package workday

import (
	"encoding/json"
	"testing"
	"time"
)

func rev(fields string) revision {
	var r revision
	if err := json.Unmarshal([]byte(`{"fields":`+fields+`}`), &r); err != nil {
		panic(err)
	}
	return r
}

func TestDescribeEdits(t *testing.T) {
	revs := []revision{
		rev(`{"System.Rev":1,"System.State":"New","System.ChangedBy":{"id":"me-id"},"System.ChangedDate":"2026-10-20T08:00:00Z"}`),
		// someone else: not mine, but the baseline for the next revision
		rev(`{"System.Rev":2,"System.State":"New","System.AssignedTo":"A","System.ChangedBy":{"id":"other"},"System.ChangedDate":"2026-10-20T09:00:00Z"}`),
		// mine: state change, assignee cleared, effort set, plus bookkeeping noise
		rev(`{"System.Rev":3,"System.State":"Active","System.ChangedBy":{"id":"me-id"},"System.ChangedDate":"2026-10-20T10:00:00Z",
			"Microsoft.VSTS.Common.StateChangeDate":"2026-10-20T10:00:00Z","System.BoardColumn":"Doing",
			"Microsoft.VSTS.Scheduling.RemainingWork":4,"System.CommentCount":0}`),
		// mine: a comment and a link
		rev(`{"System.Rev":4,"System.State":"Active","System.History":"<div>done</div>","System.RelatedLinkCount":1,
			"Microsoft.VSTS.Scheduling.RemainingWork":4,"System.ChangedBy":{"id":"me-id"},"System.ChangedDate":"2026-10-20T11:00:00Z"}`),
		// mine again but outside the selection (next day)
		rev(`{"System.Rev":5,"System.State":"Resolved","System.ChangedBy":{"id":"me-id"},"System.ChangedDate":"2026-10-21T11:00:00Z"}`),
	}
	day := NewWindow(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	got, times := describeEdits(revs, func(r revision) bool {
		return r.changedBy().ID == "me-id" && day.Contains(r.changedDate())
	})

	// Chronological across revisions; within one revision the state change
	// and comment come first.
	want := "edited 3x: created, state New→Active, assignee, remaining work, comment, links"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if len(times) != 3 {
		t.Errorf("times = %v", times)
	}
}

func TestEditKindsIgnoresBookkeeping(t *testing.T) {
	a := rev(`{"System.Rev":1,"System.ChangedDate":"x","System.Watermark":1,"System.BoardColumn":"A"}`).Fields
	b := rev(`{"System.Rev":2,"System.ChangedDate":"y","System.Watermark":2,"System.BoardColumn":"B"}`).Fields
	if got := editKinds(a, b); len(got) != 0 {
		t.Errorf("kinds = %v, want none", got)
	}
}

func TestEditKindsNamesUnknownFields(t *testing.T) {
	a := rev(`{"Custom.CustomerRef":"x"}`).Fields
	b := rev(`{"Custom.CustomerRef":"y"}`).Fields
	if got := editKinds(a, b); len(got) != 1 || got[0] != "CustomerRef" {
		t.Errorf("kinds = %v", got)
	}
}
