package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// BaseURL is overridable via TRANSITOUS_API_BASE (used by tests; also handy
// for self-hosted MOTIS instances).
var BaseURL = func() string {
	if v := os.Getenv("TRANSITOUS_API_BASE"); v != "" {
		return v
	}
	return "https://api.transitous.org"
}()

var httpClient = &http.Client{Timeout: 20 * time.Second}

// --- MOTIS API types (nullable fields are pointers) ---

type GeocodeResult struct {
	Type string `json:"type"` // "STOP" | "ADDRESS" | "PLACE"
	ID   string `json:"id"`
	Name string `json:"name"`
}

type place struct {
	Name               string  `json:"name"`
	StopID             string  `json:"stopId"`
	Arrival            *string `json:"arrival"`
	Departure          *string `json:"departure"`
	ScheduledArrival   *string `json:"scheduledArrival"`
	ScheduledDeparture *string `json:"scheduledDeparture"`
	Cancelled          bool    `json:"cancelled"`
}

type StopTime struct {
	Place          place  `json:"place"`
	RealTime       bool   `json:"realTime"` // false = schedule-only, no GTFS-RT match
	Headsign       string `json:"headsign"` // direction (destination shown on the bus)
	RouteShortName string `json:"routeShortName"`
	TripID         string `json:"tripId"`
	Cancelled      bool   `json:"cancelled"`
	TripCancelled  bool   `json:"tripCancelled"`
}

type stoptimesResponse struct {
	StopTimes []StopTime `json:"stopTimes"`
}

type leg struct {
	Mode              string  `json:"mode"`
	From              place   `json:"from"`
	To                place   `json:"to"`
	IntermediateStops []place `json:"intermediateStops"`
}

type tripResponse struct {
	Legs []leg `json:"legs"`
}

// --- helpers ---

func getJSON(path string, query url.Values, out any) error {
	u := BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	// Transitous asks clients to identify themselves
	req.Header.Set("User-Agent", "bus-predictor-pet-project")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, msg)
	}
	return json.Unmarshal(body, out)
}

func parseTime(s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, *s)
	return t, err == nil
}

// FindStop resolves a stop name via geocoding. Transitous is worldwide, so
// prefer stops from the VBB (Berlin-Brandenburg) feed over same-named stops
// elsewhere in Germany.
func FindStop(query string) (*GeocodeResult, error) {
	var results []GeocodeResult
	err := getJSON("/api/v1/geocode", url.Values{
		"text":     {query},
		"language": {"de"},
	}, &results)
	if err != nil {
		return nil, err
	}
	var firstStop *GeocodeResult
	for i := range results {
		if results[i].Type != "STOP" {
			continue
		}
		if strings.HasPrefix(results[i].ID, "de-VBB_") {
			return &results[i], nil
		}
		if firstStop == nil {
			firstStop = &results[i]
		}
	}
	if firstStop != nil {
		return firstStop, nil
	}
	return nil, fmt.Errorf("stop %q not found", query)
}

func FetchStopTimes(stopID string) ([]StopTime, error) {
	var resp stoptimesResponse
	err := getJSON("/api/v5/stoptimes", url.Values{
		"stopId": {stopID},
		"n":      {"100"},
	}, &resp)
	return resp.StopTimes, err
}
