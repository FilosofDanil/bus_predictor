package engine

import (
	"strings"
	"testing"
	"time"

	"bus_predictor/internal/engine/enginetest"
)

func TestPipeline(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	server := enginetest.NewMockAPI(t, now)
	defer server.Close()
	BaseURL = server.URL

	stop, err := FindStop("Westerwaldstrasse")
	if err != nil {
		t.Fatalf("FindStop: %v", err)
	}
	if stop.Name != "Berlin, Westerwaldstr." || stop.ID != "de-VBB_de:11000:900027453::1" {
		t.Fatalf("VBB stop not preferred, got: %+v", stop)
	}

	sts, err := FetchStopTimes(stop.ID)
	if err != nil {
		t.Fatalf("FetchStopTimes: %v", err)
	}
	if len(sts) != 5 {
		t.Fatalf("expected 5 stoptimes, got %d", len(sts))
	}

	if name, err := ValidateLine(sts, "m37", stop.Name); err != nil || name != "M37" {
		t.Errorf("case-insensitive line match failed: %q, %v", name, err)
	}
	if _, err := ValidateLine(sts, "X99", stop.Name); err == nil {
		t.Error("expected error for line that does not serve the stop")
	}

	// no richtung: one departure per direction, cancelled and later ones skipped
	both := nextPerDirection(sts, "M37", "", now)
	if len(both) != 2 || both[0].TripID != "trip-m37" || both[1].TripID != "trip-m37-back" {
		t.Fatalf("expected [trip-m37 trip-m37-back], got %+v", both)
	}

	// richtung filters by case-insensitive substring of the headsign
	if got := nextPerDirection(sts, "M37", "hahneberg", now); len(got) != 1 || got[0].TripID != "trip-m37" {
		t.Fatalf("richtung=hahneberg: expected [trip-m37], got %+v", got)
	}
	if got := nextPerDirection(sts, "M37", "alexanderplatz", now); len(got) != 0 {
		t.Fatalf("richtung=alexanderplatz: expected no matches, got %+v", got)
	}

	if got := formatDelay(*both[0]); got != "+2 min (real-time)" {
		t.Errorf("formatDelay = %q", got)
	}
	// realTime=false must be reported as schedule-only regardless of times
	if got := formatDelay(*both[1]); got != "unknown (no real-time data, time is schedule-only)" {
		t.Errorf("formatDelay(no realtime) = %q", got)
	}
}

func TestBuildReport(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	server := enginetest.NewMockAPI(t, now)
	defer server.Close()
	BaseURL = server.URL

	stop := &GeocodeResult{Type: "STOP", ID: "de-VBB_de:11000:900027453::1", Name: "Berlin, Westerwaldstr."}
	rep, err := BuildReport(stop, "M37", "", now)
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if len(rep.Directions) != 2 {
		t.Fatalf("expected 2 directions, got %+v", rep.Directions)
	}
	underway := rep.Directions[0]
	if underway.LastStopName != "Berlin, Ziegelhof" || underway.LastStopErr != "" || !underway.RealTime ||
		underway.TripID != "trip-m37" {
		t.Errorf("underway direction wrong: %+v", underway)
	}
	notStarted := rep.Directions[1]
	if notStarted.LastStopErr == "" || notStarted.LastStopName != "" {
		t.Errorf("not-started direction should report unavailable: %+v", notStarted)
	}

	if _, err := BuildReport(stop, "M37", "alexanderplatz", now); err == nil ||
		!strings.Contains(err.Error(), "directions here") {
		t.Errorf("expected helpful richtung error, got %v", err)
	}
}
