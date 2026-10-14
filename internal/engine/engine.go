// Package engine talks to Transitous (api.transitous.org), a community-run
// MOTIS instance serving the official VBB GTFS + GTFS-RT feeds as JSON, and
// builds the bus report (next departure, delay, last departed stop per
// direction). See CHAT.md at the repo root for background.
//
// The package is split into api.go (HTTP/JSON access to Transitous and the
// raw MOTIS types), persist.go (the report data model and the lookup that
// derives a tracked bus's current position), and this file (the business
// logic that ties them together for the console and bot frontends).
package engine

import (
	"fmt"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Windows has no system tz database; embed it for Europe/Berlin
)

var berlin = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.Local
	}
	return loc
}()

// Clock formats a timestamp as local Berlin wall-clock time.
func Clock(t time.Time) string {
	return t.In(berlin).Format("15:04")
}

func MinutesBetween(from, to time.Time) int {
	return int(to.Sub(from).Round(time.Minute).Minutes())
}

// ValidateLine checks the configured line is served at the stop, based on the
// upcoming departures, and returns its canonical name.
func ValidateLine(sts []StopTime, want, stopName string) (string, error) {
	for _, st := range sts {
		if strings.EqualFold(st.RouteShortName, want) {
			return st.RouteShortName, nil
		}
	}
	return "", fmt.Errorf("line %q not found among upcoming departures at stop %q (lines seen: %s)",
		want, stopName, strings.Join(DistinctLines(sts), ", "))
}

// DistinctLines lists the lines seen among the stop's upcoming departures.
func DistinctLines(sts []StopTime) []string {
	var lines []string
	for _, st := range sts {
		if st.RouteShortName != "" && !slices.Contains(lines, st.RouteShortName) {
			lines = append(lines, st.RouteShortName)
		}
	}
	slices.Sort(lines)
	return lines
}

// DirectionsOf lists the distinct headsigns of a line at the stop.
func DirectionsOf(sts []StopTime, lineName string) []string {
	var dirs []string
	for i := range sts {
		if strings.EqualFold(sts[i].RouteShortName, lineName) && !slices.Contains(dirs, sts[i].Headsign) {
			dirs = append(dirs, sts[i].Headsign)
		}
	}
	return dirs
}

// nextPerDirection picks, per direction (headsign) of the line, the earliest
// upcoming non-cancelled departure, in order of first appearance. With a
// non-empty richtung it keeps only directions matching it (case-insensitive
// substring).
func nextPerDirection(sts []StopTime, lineName, richtung string, now time.Time) []*StopTime {
	var next []*StopTime
	seen := map[string]bool{}
	for i := range sts {
		st := &sts[i]
		if !strings.EqualFold(st.RouteShortName, lineName) || seen[st.Headsign] ||
			st.Cancelled || st.TripCancelled || st.Place.Cancelled {
			continue
		}
		if richtung != "" && !strings.Contains(strings.ToLower(st.Headsign), strings.ToLower(richtung)) {
			continue
		}
		if t, ok := parseTime(st.Place.Departure); ok && t.After(now) {
			seen[st.Headsign] = true
			next = append(next, st)
		}
	}
	return next
}

func formatDelay(st StopTime) string {
	// MOTIS states explicitly whether an entry is backed by GTFS-RT data;
	// without it the times are pure schedule (see CHAT.md on HAFAS masking
	// this — here it's honest).
	if !st.RealTime {
		return "unknown (no real-time data, time is schedule-only)"
	}
	dep, okD := parseTime(st.Place.Departure)
	sched, okS := parseTime(st.Place.ScheduledDeparture)
	if !okD || !okS {
		return "unknown"
	}
	delay := int(dep.Sub(sched).Seconds())
	mins := delay / 60
	secs := delay % 60
	switch {
	case delay == 0:
		return "on time (real-time)"
	case secs == 0:
		return fmt.Sprintf("%+d min (real-time)", mins)
	default:
		return fmt.Sprintf("%+d min %d s (real-time)", mins, abs(secs))
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// BuildReport runs the whole pipeline for an already-resolved stop: fresh
// departures, line validation, one entry per direction with the live position.
func BuildReport(stop *GeocodeResult, lineQuery, richtung string, now time.Time) (*Report, error) {
	sts, err := FetchStopTimes(stop.ID)
	if err != nil {
		return nil, err
	}
	lineName, err := ValidateLine(sts, lineQuery, stop.Name)
	if err != nil {
		return nil, err
	}

	next := nextPerDirection(sts, lineName, richtung, now)
	if len(next) == 0 && richtung != "" {
		if dirs := DirectionsOf(sts, lineName); len(dirs) > 0 {
			return nil, fmt.Errorf("no upcoming %s departures match richtung %q (directions here: %s)",
				lineName, richtung, strings.Join(dirs, ", "))
		}
	}

	rep := &Report{StopName: stop.Name, Line: lineName}
	for _, st := range next {
		d := DirectionReport{
			Richtung:  st.Headsign,
			TripID:    st.TripID,
			DelayText: formatDelay(*st),
			RealTime:  st.RealTime,
		}
		d.Departure, _ = parseTime(st.Place.Departure)
		name, departed, err := lastDepartedStop(st.TripID, now)
		if err != nil {
			d.LastStopErr = err.Error()
		} else {
			d.LastStopName = name
			d.LastStopTime = departed
		}
		rep.Directions = append(rep.Directions, d)
	}
	return rep, nil
}
