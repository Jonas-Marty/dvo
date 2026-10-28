package workday

import (
	"reflect"
	"testing"
	"time"
)

func TestCollectorMergesReasonsAndTimes(t *testing.T) {
	c := NewCollector()
	t1 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC)

	c.Add(7, "edited 2x", t2, t1)
	c.Add(7, "edited 2x") // same reason again: still one
	c.Add(7, "PR !1 created", time.Time{})
	c.Add(3, "pushed branch x")
	c.Add(0, "ignored")

	got := c.Entries()
	if len(got) != 2 || got[0].ID != 3 || got[1].ID != 7 {
		t.Fatalf("entries = %+v", got)
	}
	e := got[1]
	if !reflect.DeepEqual(e.Reasons, []string{"PR !1 created", "edited 2x"}) {
		t.Errorf("reasons = %v", e.Reasons)
	}
	if !e.First.Equal(t1) || !e.Last.Equal(t2) {
		t.Errorf("first/last = %v / %v", e.First, e.Last)
	}
	if !got[0].First.IsZero() {
		t.Errorf("an item without timestamps got First = %v", got[0].First)
	}
	if !c.Has(7) || c.Has(8) {
		t.Error("Has is wrong")
	}
}
