// Package bot is the Telegram GUI. Workflow per chat:
//
//	/start -> enter stop name -> choose bus number (keyboard of lines at the
//	stop) -> choose richtung or start (keyboard of the line's directions plus
//	a start-all button) -> engine report with the bus position highlighted.
//
// /track then follows the last requested bus: the report is re-fetched every
// trackInterval and re-sent whenever the bus position, arrival minute, or
// delay changes (useful while waiting at the stop). /untrack stops it; a
// trackDuration deadline stops it automatically.
//
// Plain Bot API over HTTP long polling, no external dependencies. This file
// holds the bot's configuration and the Telegram transport plumbing
// (construction, the poll loop, and the raw call/send helpers); handler.go
// has the per-chat command/text/tracking logic and keyboard.go builds the
// reply keyboards.
package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// overridable in tests
var tgBaseURL = "https://api.telegram.org"

// --- Telegram API types (only the fields we use) ---

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	Chat tgChat `json:"chat"`
	Text string `json:"text"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgSendMessage struct {
	ChatID      int64  `json:"chat_id"`
	Text        string `json:"text"`
	ParseMode   string `json:"parse_mode,omitempty"`
	ReplyMarkup any    `json:"reply_markup,omitempty"`
}

type tgResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

// --- bot ---

type bot struct {
	token      string
	client     *http.Client
	pollClient *http.Client // long polling needs a longer timeout
	states     map[int64]*chatState
	lastQuery  map[int64]*trackQuery

	mu       sync.Mutex // guards trackers (removed from tracker goroutines on timeout)
	trackers map[int64]chan struct{}

	trackInterval time.Duration
	trackDuration time.Duration // auto-stop so a forgotten /track doesn't poll forever
}

func newBot(token string) *bot {
	return &bot{
		token:         token,
		client:        &http.Client{Timeout: 20 * time.Second},
		pollClient:    &http.Client{Timeout: 70 * time.Second},
		states:        map[int64]*chatState{},
		lastQuery:     map[int64]*trackQuery{},
		trackers:      map[int64]chan struct{}{},
		trackInterval: 30 * time.Second,
		trackDuration: 30 * time.Minute,
	}
}

// Run starts the bot and blocks, polling for updates.
func Run(token string) error {
	b := newBot(token)

	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(b.client, "getMe", map[string]any{}, &me); err != nil {
		return fmt.Errorf("telegram auth failed: %w", err)
	}
	fmt.Printf("bot @%s is running — press Ctrl+C to stop\n", me.Username)

	var offset int64
	for {
		var updates []tgUpdate
		err := b.call(b.pollClient, "getUpdates", map[string]any{
			"offset":          offset,
			"timeout":         50,
			"allowed_updates": []string{"message"},
		}, &updates)
		if err != nil {
			fmt.Fprintln(os.Stderr, "getUpdates:", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for i := range updates {
			offset = updates[i].UpdateID + 1
			if updates[i].Message != nil {
				b.handle(updates[i].Message)
			}
		}
	}
}

func (b *bot) call(client *http.Client, method string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := client.Post(tgBaseURL+"/bot"+b.token+"/"+method, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var envelope tgResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if !envelope.OK {
		return fmt.Errorf("%s: telegram error: %s", method, envelope.Description)
	}
	if result != nil {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

func (b *bot) send(chatID int64, text string, markup any) {
	err := b.call(b.client, "sendMessage", tgSendMessage{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sendMessage:", err)
	}
}
