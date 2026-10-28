// Package workday finds the Azure DevOps work items a person potentially
// worked on during one local calendar day.
//
// The result is meant as a superset for hour reports: every signal that hints
// at involvement adds the item together with a reason, and the person weeds
// out the rest. Nothing here may drop an item for lack of evidence.
package workday

import (
	"fmt"
	"strings"
	"time"
)

// Window is one local calendar day.
type Window struct {
	Start time.Time // local midnight
	End   time.Time // next local midnight, exclusive
}

// NewWindow returns the day containing t's calendar date in t's location.
//
// Both ends come from time.Date rather than Start+24h, so a day that is 23 or
// 25 hours long because of a DST change still ends at the next midnight.
func NewWindow(t time.Time) Window {
	y, m, d := t.Date()
	loc := t.Location()
	return Window{
		Start: time.Date(y, m, d, 0, 0, 0, 0, loc),
		End:   time.Date(y, m, d+1, 0, 0, 0, 0, loc),
	}
}

// ParseDay reads "", "today", "yesterday" or YYYY-MM-DD, relative to now's
// location.
func ParseDay(s string, now time.Time) (Window, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "today":
		return NewWindow(now), nil
	case "yesterday":
		return NewWindow(now.AddDate(0, 0, -1)), nil
	}
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), now.Location())
	if err != nil {
		return Window{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD, today or yesterday", s)
	}
	return NewWindow(t), nil
}

// Contains reports whether t lies inside the day. t may be in any location.
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.Start) && t.Before(w.End)
}

// Loc is the time zone the day is defined in.
func (w Window) Loc() *time.Location { return w.Start.Location() }

// StartUTC and EndUTC format the bounds for API query strings.
func (w Window) StartUTC() string { return w.Start.UTC().Format("2006-01-02T15:04:05Z") }
func (w Window) EndUTC() string   { return w.End.UTC().Format("2006-01-02T15:04:05Z") }

// WIQLFloor is the date to use in `[System.ChangedDate] >= '...'`.
//
// WIQL compares whole days in the profile time zone of the signed-in user,
// which can differ from the local one by up to a day in either direction. One
// extra day of margin keeps the query a superset; every hit is checked against
// the exact window afterwards.
func (w Window) WIQLFloor() string {
	y, m, d := w.Start.Date()
	return time.Date(y, m, d-1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}
