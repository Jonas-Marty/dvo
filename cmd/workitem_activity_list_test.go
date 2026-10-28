package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/workday"
)

func TestFormatIDs(t *testing.T) {
	rows := []workday.Row{{ID: 123}, {ID: 223}, {ID: 323}}
	if got := formatIDs(rows, []int{0, 2}); got != "#123;#323" {
		t.Errorf("got %q", got)
	}
	if got := formatIDs(rows, []int{1}); got != "#223" {
		t.Errorf("got %q", got)
	}
}

func TestListItemsShowReasonsTimesAndParent(t *testing.T) {
	first := time.Date(2026, 10, 20, 9, 5, 0, 0, time.UTC)
	rows := []workday.Row{
		{ID: 1, Type: "Task", Title: "Do it", State: "Active", ParentID: 9, ParentTitle: "Story",
			First: first, Last: first.Add(2 * time.Hour),
			Reasons: []string{"edited 2x: comment", "my activity: edited"}},
		{ID: 2, Type: "Bug", Title: "Open", Reasons: []string{"assigned to you, open (no activity found)"}},
	}
	items := listItems(rows)

	if items[0].Title != "#1  Task  Do it  [Active]" {
		t.Errorf("title = %q", items[0].Title)
	}
	want := []string{"09:05–11:05  ·  parent: 9 Story", "• edited 2x: comment", "• my activity: edited"}
	if strings.Join(items[0].Details, "|") != strings.Join(want, "|") {
		t.Errorf("details = %q", items[0].Details)
	}
	if items[1].Details[0] != "no activity time" {
		t.Errorf("details = %q", items[1].Details)
	}
}
