package workday

import (
	"testing"
	"time"

	_ "time/tzdata"
)

func TestWindowAcrossDST(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		day       string
		wantHours float64
		wantStart string // UTC
		wantFloor string
	}{
		{"normal day", "2026-06-10", 24, "2026-06-09T22:00:00Z", "2026-06-09"},
		{"spring forward", "2026-03-29", 23, "2026-03-28T23:00:00Z", "2026-03-28"},
		{"fall back", "2026-10-25", 25, "2026-10-24T22:00:00Z", "2026-10-24"},
		{"floor crosses month", "2026-03-01", 24, "2026-02-28T23:00:00Z", "2026-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := ParseDay(tt.day, time.Date(2026, 12, 1, 9, 0, 0, 0, zurich))
			if err != nil {
				t.Fatal(err)
			}
			if got := w.End.Sub(w.Start).Hours(); got != tt.wantHours {
				t.Errorf("length = %vh, want %vh", got, tt.wantHours)
			}
			if got := w.StartUTC(); got != tt.wantStart {
				t.Errorf("StartUTC = %s, want %s", got, tt.wantStart)
			}
			if got := w.WIQLFloor(); got != tt.wantFloor {
				t.Errorf("WIQLFloor = %s, want %s", got, tt.wantFloor)
			}
		})
	}
}

func TestWindowContainsIsHalfOpenAndZoneAgnostic(t *testing.T) {
	zone := time.FixedZone("x", 2*3600)
	w := NewWindow(time.Date(2026, 10, 20, 15, 0, 0, 0, zone))

	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"start is inside", time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC), true},
		{"one second before", time.Date(2026, 10, 19, 21, 59, 59, 0, time.UTC), false},
		{"end is outside", time.Date(2026, 10, 20, 22, 0, 0, 0, time.UTC), false},
		{"late evening local", time.Date(2026, 10, 20, 21, 59, 0, 0, time.UTC), true},
		{"zero time", time.Time{}, false},
	}
	for _, tt := range tests {
		if got := w.Contains(tt.at); got != tt.want {
			t.Errorf("%s: Contains = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseDay(t *testing.T) {
	now := time.Date(2026, 10, 21, 8, 30, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"":           "2026-10-21",
		"today":      "2026-10-21",
		"Yesterday":  "2026-10-20",
		"2026-01-05": "2026-01-05",
	} {
		w, err := ParseDay(in, now)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got := w.Start.Format("2006-01-02"); got != want {
			t.Errorf("%q: got %s, want %s", in, got, want)
		}
	}
	for _, bad := range []string{"21.10.2026", "tomorrow", "2026-13-01"} {
		if _, err := ParseDay(bad, now); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}
