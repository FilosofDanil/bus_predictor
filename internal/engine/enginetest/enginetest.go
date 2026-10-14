// Package enginetest provides a fake Transitous (MOTIS) API server with
// fixtures for the Westerwaldstr. / M37 example, shared by the engine and bot
// tests. It deliberately does not import the engine package.
package enginetest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// NewMockAPI serves geocode, stoptimes, and trip responses shaped like
// api.transitous.org, with all times relative to now.
func NewMockAPI(t *testing.T, now time.Time) *httptest.Server {
	ts := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/geocode", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("text"); q != "Westerwaldstrasse" {
			t.Errorf("unexpected geocode text %q", q)
		}
		// a same-named non-VBB stop ranks first: the VBB one must still win
		fmt.Fprintf(w, `[
			{"type":"STOP","id":"de-DELFI_000011605205","name":"Moerlen Westerwaldstrasse"},
			{"type":"STOP","id":"de-VBB_de:11000:900027453::1","name":"Berlin, Westerwaldstr."}
		]`)
	})
	mux.HandleFunc("/api/v5/stoptimes", func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("stopId"); id != "de-VBB_de:11000:900027453::1" {
			t.Errorf("unexpected stopId %q", id)
		}
		stopTime := func(tripID, headsign, line string, dep time.Duration, delay time.Duration, realTime, cancelled bool) string {
			return fmt.Sprintf(`{"place":{"name":"Berlin, Westerwaldstr.","stopId":"x",
				"departure":%q,"scheduledDeparture":%q,"cancelled":false},
				"mode":"BUS","realTime":%t,"headsign":%q,"routeShortName":%q,
				"tripId":%q,"cancelled":%t,"tripCancelled":%t}`,
				ts(dep), ts(dep-delay), realTime, headsign, line, tripID, cancelled, cancelled)
		}
		fmt.Fprintf(w, `{"stopTimes":[%s,%s,%s,%s,%s]}`,
			stopTime("trip-cancelled", "Staaken, Hahneberg", "M37", 2*time.Minute, 0, true, true),
			stopTime("trip-other-line", "Reimerweg", "137", 3*time.Minute, 0, true, false),
			stopTime("trip-m37", "Staaken, Hahneberg", "M37", 6*time.Minute, 2*time.Minute, true, false),
			stopTime("trip-m37-later", "Staaken, Hahneberg", "M37", 16*time.Minute, 0, true, false),
			stopTime("trip-m37-back", "Spandau, Waldkrankenhaus", "M37", 9*time.Minute, 0, false, false))
	})
	mux.HandleFunc("/api/v5/trip", func(w http.ResponseWriter, r *http.Request) {
		switch id := r.URL.Query().Get("tripId"); id {
		case "trip-m37": // underway: departed Ziegelhof 3 minutes ago
			fmt.Fprintf(w, `{"legs":[{"mode":"BUS",
				"from":{"name":"Staaken, Hahneberg","departure":%q,"scheduledDeparture":%q},
				"intermediateStops":[
					{"name":"Berlin, Ziegelhof","departure":%q,"scheduledDeparture":%q},
					{"name":"Berlin, Westerwaldstr.","departure":%q,"scheduledDeparture":%q}
				],
				"to":{"name":"Spandau, Waldkrankenhaus","arrival":%q,"scheduledArrival":%q}}]}`,
				ts(-10*time.Minute), ts(-12*time.Minute),
				ts(-3*time.Minute), ts(-5*time.Minute),
				ts(6*time.Minute), ts(4*time.Minute),
				ts(20*time.Minute), ts(18*time.Minute))
		case "trip-m37-later": // the following bus, not started yet
			fmt.Fprintf(w, `{"legs":[{"mode":"BUS",
				"from":{"name":"Staaken, Hahneberg","departure":%q,"scheduledDeparture":%q},
				"intermediateStops":[],
				"to":{"name":"Spandau, Waldkrankenhaus","arrival":%q,"scheduledArrival":%q}}]}`,
				ts(16*time.Minute), ts(16*time.Minute),
				ts(50*time.Minute), ts(50*time.Minute))
		case "trip-m37-back": // not started yet: everything in the future
			fmt.Fprintf(w, `{"legs":[{"mode":"BUS",
				"from":{"name":"Spandau, Waldkrankenhaus","departure":%q,"scheduledDeparture":%q},
				"intermediateStops":[],
				"to":{"name":"Staaken, Hahneberg","arrival":%q,"scheduledArrival":%q}}]}`,
				ts(9*time.Minute), ts(9*time.Minute),
				ts(40*time.Minute), ts(40*time.Minute))
		default:
			t.Errorf("unexpected tripId %q", id)
		}
	})
	return httptest.NewServer(mux)
}
