package engine

import (
	"fmt"
	"net/url"
	"time"
)

// --- report: the engine's structured result, rendered by console and bot ---

type Report struct {
	StopName   string
	Line       string
	Directions []DirectionReport
}

type DirectionReport struct {
	Richtung     string
	TripID       string // identifies the concrete bus, e.g. for arrival detection
	Departure    time.Time
	DelayText    string
	RealTime     bool
	LastStopName string
	LastStopTime time.Time
	LastStopErr  string // non-empty when the last departed stop is unavailable
}

// lastDepartedStop finds the last stop of the trip whose (departure) time is
// already in the past — i.e. the stop the bus most recently left. This is
// how the tracked bus's current position is derived: MOTIS gives discrete
// stop events, not a continuous position, so "last departed stop" is what we
// persist and report as the bus's location.
func lastDepartedStop(tripID string, now time.Time) (name string, departed time.Time, err error) {
	var resp tripResponse
	err = getJSON("/api/v5/trip", url.Values{"tripId": {tripID}}, &resp)
	if err != nil {
		return "", time.Time{}, err
	}
	for _, l := range resp.Legs {
		if l.Mode == "WALK" {
			continue
		}
		stops := append([]place{l.From}, l.IntermediateStops...)
		stops = append(stops, l.To)
		for _, p := range stops {
			t, ok := parseTime(p.Departure)
			if !ok {
				t, ok = parseTime(p.Arrival)
			}
			if ok && t.Before(now) {
				name = p.Name
				departed = t
			}
		}
	}
	if name == "" {
		return "", time.Time{}, fmt.Errorf("trip has not departed from any stop yet")
	}
	return name, departed, nil
}
