package bot

import (
	"fmt"
	"html"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"bus_predictor/internal/engine"
)

const (
	stateAwaitStop = iota
	stateAwaitLine
	stateAwaitRichtung
)

type chatState struct {
	step int
	stop *engine.GeocodeResult
	sts  []engine.StopTime // departures cached at stop selection, for the keyboards
	line string
}

// trackQuery is the last completed request of a chat, the target for /track.
type trackQuery struct {
	stop     *engine.GeocodeResult
	line     string
	richtung string
}

func (b *bot) handle(m *tgMessage) {
	chatID := m.Chat.ID
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}

	switch text {
	case "/start":
		b.states[chatID] = &chatState{step: stateAwaitStop}
		b.send(chatID, "🚌 <b>Bus Predictor</b>\nEnter the stop name (e.g. Westerwaldstrasse):",
			tgRemoveKeyboard{RemoveKeyboard: true})
		return
	case "/track":
		b.startTracking(chatID)
		return
	case "/untrack":
		if b.stopTracking(chatID) {
			b.send(chatID, "🔕 Stopped tracking.", nil)
		} else {
			b.send(chatID, "You are not tracking any bus. Run /start, get a report, then /track.", nil)
		}
		return
	}

	st, ok := b.states[chatID]
	if !ok {
		b.send(chatID, "Send /start to begin.", nil)
		return
	}

	switch st.step {
	case stateAwaitStop:
		stop, err := engine.FindStop(text)
		if err != nil {
			b.send(chatID, "❌ "+html.EscapeString(err.Error())+"\nTry another stop name:", nil)
			return
		}
		sts, err := engine.FetchStopTimes(stop.ID)
		if err != nil {
			b.send(chatID, "❌ "+html.EscapeString(err.Error()), nil)
			return
		}
		lines := engine.DistinctLines(sts)
		if len(lines) == 0 {
			b.send(chatID, "No upcoming departures at this stop right now. Try another stop name:", nil)
			return
		}
		st.stop, st.sts, st.step = stop, sts, stateAwaitLine
		b.send(chatID, fmt.Sprintf("Stop: <b>%s</b>\nChoose the bus number:", html.EscapeString(stop.Name)),
			keyboardOf(lines, 3))

	case stateAwaitLine:
		lineName, err := engine.ValidateLine(st.sts, text, st.stop.Name)
		if err != nil {
			b.send(chatID, "❌ "+html.EscapeString(err.Error()), nil)
			return
		}
		st.line, st.step = lineName, stateAwaitRichtung
		markup := keyboardOf(engine.DirectionsOf(st.sts, lineName), 1)
		markup.Keyboard = append([][]string{{btnStartAll}}, markup.Keyboard...)
		b.send(chatID, fmt.Sprintf("Line <b>%s</b>. Choose the richtung, or start with all directions:",
			html.EscapeString(lineName)), markup)

	case stateAwaitRichtung:
		richtung := text
		if text == btnStartAll {
			richtung = ""
		}
		now := time.Now()
		rep, err := engine.BuildReport(st.stop, st.line, richtung, now)
		if err != nil {
			b.send(chatID, "❌ "+html.EscapeString(err.Error()), nil)
			return
		}
		b.lastQuery[chatID] = &trackQuery{stop: st.stop, line: st.line, richtung: richtung}
		b.send(chatID, renderReportHTML(rep, now), tgRemoveKeyboard{RemoveKeyboard: true})
		delete(b.states, chatID)
	}
}

// --- /track: live position updates for the last requested bus ---

func (b *bot) startTracking(chatID int64) {
	q, ok := b.lastQuery[chatID]
	if !ok {
		b.send(chatID, "Nothing to track yet — run /start, get a report, then /track.", nil)
		return
	}
	b.stopTracking(chatID) // restart cleanly if already tracking

	cancel := make(chan struct{})
	b.mu.Lock()
	b.trackers[chatID] = cancel
	b.mu.Unlock()

	b.send(chatID, fmt.Sprintf("🔔 Tracking <b>%s</b> at <b>%s</b> — I will message you when the bus moves. /untrack to stop.",
		html.EscapeString(q.line), html.EscapeString(q.stop.Name)), nil)
	go b.trackLoop(chatID, *q, cancel)
}

// stopTracking cancels the chat's tracker and reports whether there was one.
func (b *bot) stopTracking(chatID int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	cancel, ok := b.trackers[chatID]
	if ok {
		close(cancel)
		delete(b.trackers, chatID)
	}
	return ok
}

// removeTracker deletes the tracker entry if it still is this loop's own
// (a newer /track may have replaced it).
func (b *bot) removeTracker(chatID int64, own chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.trackers[chatID] == own {
		delete(b.trackers, chatID)
	}
}

// trackedDir is the per-direction tracking state: which concrete bus (trip)
// is followed, its last known ETA, and the last reported status.
type trackedDir struct {
	tripID  string
	eta     time.Time
	lastKey string
}

func (b *bot) trackLoop(chatID int64, q trackQuery, cancel chan struct{}) {
	deadline := time.After(b.trackDuration)
	tick := time.NewTicker(b.trackInterval)
	defer tick.Stop()

	tracked := map[string]*trackedDir{}
	if done := b.trackPoll(chatID, q, tracked, true); done {
		b.removeTracker(chatID, cancel)
		return
	}
	for {
		select {
		case <-cancel:
			return
		case <-deadline:
			b.removeTracker(chatID, cancel)
			b.send(chatID, "⏱ Tracking stopped automatically after 30 minutes. Send /track to resume.", nil)
			return
		case <-tick.C:
			if done := b.trackPoll(chatID, q, tracked, false); done {
				b.removeTracker(chatID, cancel)
				return
			}
		}
	}
}

// trackPoll fetches a fresh report and messages the chat with short status
// lines (position + ETA) for directions that changed, and arrival notices for
// tracked buses that are gone from the departure board (their trip at the
// stop switched to a later one = the bus has been at the station). Returns
// true when tracking is finished. API hiccups are logged, not sent, so a
// flaky minute does not spam the chat.
func (b *bot) trackPoll(chatID int64, q trackQuery, tracked map[string]*trackedDir, first bool) bool {
	now := time.Now()
	rep, err := engine.BuildReport(q.stop, q.line, q.richtung, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "track:", err)
		return false
	}

	if first {
		if len(rep.Directions) == 0 {
			b.send(chatID, "Nothing upcoming to track right now — tracking stopped.", nil)
			return true
		}
		var lines []string
		for i := range rep.Directions {
			d := rep.Directions[i]
			tracked[d.Richtung] = &trackedDir{tripID: d.TripID, eta: d.Departure, lastKey: dirKey(d)}
			lines = append(lines, trackStatusLine(d, now))
		}
		b.send(chatID, trackHeader(q)+"\n"+strings.Join(lines, "\n"), nil)
		return false
	}

	current := map[string]*engine.DirectionReport{}
	for i := range rep.Directions {
		current[rep.Directions[i].Richtung] = &rep.Directions[i]
	}

	var lines []string
	for _, richtung := range slices.Sorted(maps.Keys(tracked)) {
		rec := tracked[richtung]
		cur := current[richtung]
		if cur == nil || cur.TripID != rec.tripID {
			lines = append(lines, fmt.Sprintf("🏁 %s — the bus should have arrived (~%s)",
				html.EscapeString(richtung), engine.Clock(rec.eta)))
			delete(tracked, richtung)
			continue
		}
		rec.eta = cur.Departure
		if k := dirKey(*cur); k != rec.lastKey {
			rec.lastKey = k
			lines = append(lines, trackStatusLine(*cur, now))
		}
	}
	if len(lines) > 0 {
		b.send(chatID, trackHeader(q)+"\n"+strings.Join(lines, "\n"), nil)
	}
	if len(tracked) == 0 {
		b.send(chatID, fmt.Sprintf("✅ Tracking finished — the tracked bus has arrived at <b>%s</b>. Send /track to follow the next one.",
			html.EscapeString(q.stop.Name)), nil)
		return true
	}
	return false
}

func trackHeader(q trackQuery) string {
	return fmt.Sprintf("🔄 <b>%s</b> at <b>%s</b>", html.EscapeString(q.line), html.EscapeString(q.stop.Name))
}

// trackStatusLine is the short tracking format: just the bus position and the
// estimated arrival time.
func trackStatusLine(d engine.DirectionReport, now time.Time) string {
	esc := html.EscapeString
	pos := "not departed yet"
	if d.LastStopErr == "" {
		pos = fmt.Sprintf("📍 <b>%s</b> (%d min ago)", esc(d.LastStopName), engine.MinutesBetween(d.LastStopTime, now))
	}
	s := fmt.Sprintf("➡️ %s — %s, arrives <b>%s</b> (in %d min)",
		esc(d.Richtung), pos, engine.Clock(d.Departure), engine.MinutesBetween(now, d.Departure))
	if !d.RealTime {
		s += " ⚠️ schedule-only"
	}
	return s
}

// dirKey captures what counts as "the bus moved": position, arrival minute,
// delay — but not the countdown, which changes every minute by itself.
func dirKey(d engine.DirectionReport) string {
	return strings.Join([]string{d.LastStopName, d.LastStopErr, engine.Clock(d.Departure), d.DelayText}, "|")
}

// renderReportHTML mirrors the console report, highlighting the arrival time
// and where the bus last was.
func renderReportHTML(rep *engine.Report, now time.Time) string {
	esc := html.EscapeString
	var sb strings.Builder
	fmt.Fprintf(&sb, "stop : %s\n", esc(rep.StopName))
	fmt.Fprintf(&sb, "line : %s\n", esc(rep.Line))

	if len(rep.Directions) == 0 {
		sb.WriteString("next departure : none found among the next 100 departures at this stop\n")
	}
	for _, d := range rep.Directions {
		sb.WriteString("\n")
		fmt.Fprintf(&sb, "➡️ richtung : %s\n", esc(d.Richtung))
		fmt.Fprintf(&sb, "next departure : <b>%s</b> (in %d min)\n",
			engine.Clock(d.Departure), engine.MinutesBetween(now, d.Departure))
		fmt.Fprintf(&sb, "delay : %s\n", esc(d.DelayText))
		switch {
		case d.LastStopErr != "":
			fmt.Fprintf(&sb, "last departed stop : unavailable (%s)\n", esc(d.LastStopErr))
		case d.RealTime:
			fmt.Fprintf(&sb, "last departed stop : 📍 <b>%s</b> (departed %s, %d min ago)\n",
				esc(d.LastStopName), engine.Clock(d.LastStopTime), engine.MinutesBetween(d.LastStopTime, now))
		default:
			fmt.Fprintf(&sb, "last departed stop : 📍 <b>%s</b> (departed %s per schedule — no real-time confirmation)\n",
				esc(d.LastStopName), engine.Clock(d.LastStopTime))
		}
	}
	sb.WriteString("\nSend /track for live updates on this bus, or /start to check another one.")
	return sb.String()
}
