package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"bus_predictor/internal/engine"
	"bus_predictor/internal/engine/enginetest"
)

// recorder is a fake Telegram server capturing sendMessage calls; the mutex
// matters because tracker goroutines send concurrently with the test.
type recorder struct {
	mu   sync.Mutex
	sent []tgSendMessage
}

func newRecorder(t *testing.T) (*recorder, *httptest.Server) {
	rec := &recorder{}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, m := range rec.sent {
			t.Logf("bot sent: %s", m.Text)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			var m tgSendMessage
			if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
				t.Errorf("bad sendMessage payload: %v", err)
			}
			rec.mu.Lock()
			rec.sent = append(rec.sent, m)
			rec.mu.Unlock()
		}
		fmt.Fprint(w, `{"ok":true,"result":{}}`)
	}))
	return rec, server
}

func (r *recorder) count(substr string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.sent {
		if strings.Contains(m.Text, substr) {
			n++
		}
	}
	return n
}

func (r *recorder) last() tgSendMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sent) == 0 {
		return tgSendMessage{}
	}
	return r.sent[len(r.sent)-1]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBotFlow(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	api := enginetest.NewMockAPI(t, now)
	defer api.Close()
	engine.BaseURL = api.URL

	rec, tg := newRecorder(t)
	defer tg.Close()
	tgBaseURL = tg.URL

	b := newBot("TESTTOKEN")
	msg := func(text string) *tgMessage { return &tgMessage{Chat: tgChat{ID: 1}, Text: text} }

	b.handle(msg("/start"))
	b.handle(msg("Westerwaldstrasse"))
	b.handle(msg("M37"))
	b.handle(msg(btnStartAll))

	sent := rec.sent
	if len(sent) != 4 {
		t.Fatalf("expected 4 bot messages, got %d: %+v", len(sent), sent)
	}
	if !strings.Contains(sent[0].Text, "Enter the stop name") {
		t.Errorf("start message wrong: %q", sent[0].Text)
	}
	if !strings.Contains(sent[1].Text, "Choose the bus number") ||
		!strings.Contains(fmt.Sprint(sent[1].ReplyMarkup), "M37") {
		t.Errorf("line keyboard wrong: %+v", sent[1])
	}
	if !strings.Contains(sent[2].Text, "Choose the richtung") ||
		!strings.Contains(fmt.Sprint(sent[2].ReplyMarkup), "Hahneberg") ||
		!strings.Contains(fmt.Sprint(sent[2].ReplyMarkup), btnStartAll) {
		t.Errorf("richtung keyboard wrong: %+v", sent[2])
	}
	final := sent[3].Text
	for _, want := range []string{"<b>Berlin, Ziegelhof</b>", "richtung : Staaken, Hahneberg",
		"next departure : <b>", "+2 min (real-time)"} {
		if !strings.Contains(final, want) {
			t.Errorf("final report missing %q:\n%s", want, final)
		}
	}
	if _, stillTracked := b.states[1]; stillTracked {
		t.Error("chat state should be cleared after the report")
	}

	// after the flow finished, plain text prompts /start again
	b.handle(msg("hello"))
	if got := rec.last(); !strings.Contains(got.Text, "/start") {
		t.Errorf("expected /start hint, got %+v", got)
	}
}

func TestTrackFlow(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	api := enginetest.NewMockAPI(t, now)
	defer api.Close()
	engine.BaseURL = api.URL

	rec, tg := newRecorder(t)
	defer tg.Close()
	tgBaseURL = tg.URL

	b := newBot("TESTTOKEN")
	b.trackInterval = 50 * time.Millisecond
	b.trackDuration = 10 * time.Second
	msg := func(text string) *tgMessage { return &tgMessage{Chat: tgChat{ID: 1}, Text: text} }

	// /track and /untrack before any report
	b.handle(msg("/track"))
	if got := rec.last(); !strings.Contains(got.Text, "Nothing to track") {
		t.Errorf("expected nothing-to-track hint, got %+v", got)
	}
	b.handle(msg("/untrack"))
	if got := rec.last(); !strings.Contains(got.Text, "not tracking") {
		t.Errorf("expected not-tracking hint, got %+v", got)
	}

	// complete a report, then track it
	b.handle(msg("/start"))
	b.handle(msg("Westerwaldstrasse"))
	b.handle(msg("M37"))
	b.handle(msg(btnStartAll))
	b.handle(msg("/track"))

	if rec.count("🔔 Tracking") != 1 {
		t.Errorf("expected tracking confirmation, got %+v", rec.last())
	}
	// the first snapshot arrives from the tracker goroutine
	waitFor(t, "first tracking update", func() bool { return rec.count("🔄") == 1 })

	// tracking updates use the short format: position + arrival only
	rec.mu.Lock()
	var update string
	for _, m := range rec.sent {
		if strings.Contains(m.Text, "🔄") {
			update = m.Text
		}
	}
	rec.mu.Unlock()
	for _, want := range []string{"📍 <b>Berlin, Ziegelhof</b>", "arrives <b>", "➡️ Staaken, Hahneberg"} {
		if !strings.Contains(update, want) {
			t.Errorf("short update missing %q:\n%s", want, update)
		}
	}
	if strings.Contains(update, "delay :") {
		t.Errorf("tracking update should not contain the full report:\n%s", update)
	}

	// nothing changes in the fixtures, so several intervals must add no spam
	time.Sleep(6 * b.trackInterval)
	if got := rec.count("🔄"); got != 1 {
		t.Errorf("expected exactly 1 update while nothing changed, got %d", got)
	}

	b.handle(msg("/untrack"))
	if got := rec.last(); !strings.Contains(got.Text, "Stopped tracking") {
		t.Errorf("expected stop confirmation, got %+v", got)
	}
	b.mu.Lock()
	trackerCount := len(b.trackers)
	b.mu.Unlock()
	if trackerCount != 0 {
		t.Errorf("tracker map should be empty after /untrack, got %d", trackerCount)
	}
}

func TestTrackArrival(t *testing.T) {
	// Shift the fixtures so the tracked bus (trip-m37) departs the tracked
	// stop ~2s into the test: the departure board then switches to
	// trip-m37-later for the same headsign, which is the arrival signal.
	// The mock serves RFC3339 times (whole seconds), so base must sit on a
	// second boundary — a sub-second remainder would truncate the departure
	// into the past before tracking even starts.
	base := time.Now().Truncate(time.Second).Add(-6*time.Minute + 2*time.Second)
	api := enginetest.NewMockAPI(t, base)
	defer api.Close()
	engine.BaseURL = api.URL

	rec, tg := newRecorder(t)
	defer tg.Close()
	tgBaseURL = tg.URL

	b := newBot("TESTTOKEN")
	b.trackInterval = 50 * time.Millisecond
	b.trackDuration = 10 * time.Second
	msg := func(text string) *tgMessage { return &tgMessage{Chat: tgChat{ID: 1}, Text: text} }

	b.handle(msg("/start"))
	b.handle(msg("Westerwaldstrasse"))
	b.handle(msg("M37"))
	b.handle(msg("Staaken, Hahneberg")) // richtung button: track one direction
	b.handle(msg("/track"))

	waitFor(t, "arrival notice", func() bool { return rec.count("🏁") == 1 })
	waitFor(t, "tracking finished", func() bool { return rec.count("Tracking finished") == 1 })
	waitFor(t, "tracker removal", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.trackers) == 0
	})
}

func TestTrackAutoStop(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	api := enginetest.NewMockAPI(t, now)
	defer api.Close()
	engine.BaseURL = api.URL

	rec, tg := newRecorder(t)
	defer tg.Close()
	tgBaseURL = tg.URL

	b := newBot("TESTTOKEN")
	b.trackInterval = 20 * time.Millisecond
	b.trackDuration = 100 * time.Millisecond
	msg := func(text string) *tgMessage { return &tgMessage{Chat: tgChat{ID: 1}, Text: text} }

	b.handle(msg("/start"))
	b.handle(msg("Westerwaldstrasse"))
	b.handle(msg("M37"))
	b.handle(msg(btnStartAll))
	b.handle(msg("/track"))

	waitFor(t, "auto-stop message", func() bool { return rec.count("stopped automatically") == 1 })
	b.mu.Lock()
	trackerCount := len(b.trackers)
	b.mu.Unlock()
	if trackerCount != 0 {
		t.Errorf("tracker map should be empty after auto-stop, got %d", trackerCount)
	}
}
