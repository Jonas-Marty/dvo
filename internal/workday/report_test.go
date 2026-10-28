package workday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

func TestSortRows(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, time.UTC) }
	rows := []Row{
		{ID: 50},
		{ID: 3, First: at(11, 0)},
		{ID: 40},
		{ID: 9, First: at(9, 0)},
		{ID: 2, First: at(11, 0)},
	}
	sortRows(rows)
	var ids []int
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if got := fmt.Sprint(ids); got != "[9 2 3 40 50]" {
		t.Errorf("order = %s, want [9 2 3 40 50]", got)
	}
}

func TestWriteCSVIsExcelFriendly(t *testing.T) {
	rows := []Row{{
		ID: 1, Type: "Task", Title: `Say "hi"; then leave`, State: "Active", Project: "P",
		ParentID: 100, ParentTitle: "Story",
		First:   time.Date(2026, 10, 20, 9, 5, 0, 0, time.UTC),
		Last:    time.Date(2026, 10, 20, 17, 45, 0, 0, time.UTC),
		Reasons: []string{"edited 2x", "pushed branch x"},
	}}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, rows, ';'); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\xEF\xBB\xBF") {
		t.Error("missing UTF-8 byte order mark")
	}
	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(out, "\xEF\xBB\xBF"), "\r\n"), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if lines[0] != "Id;Type;Title;State;Project;Parent;First;Last;Reasons" {
		t.Errorf("header = %q", lines[0])
	}
	want := `1;Task;"Say ""hi""; then leave";Active;P;100 Story;09:05;17:45;edited 2x; pushed branch x`
	// The reasons contain "; " so they must be quoted too.
	want = strings.Replace(want, "edited 2x; pushed branch x", `"edited 2x; pushed branch x"`, 1)
	if lines[1] != want {
		t.Errorf("row = %q\nwant  %q", lines[1], want)
	}
}

func TestWriteTableKeepsReasonsComplete(t *testing.T) {
	long := strings.Repeat("x", 80)
	rows := []Row{{ID: 1, Title: long, Reasons: []string{"a reason that must not be cut off"}}}

	var buf bytes.Buffer
	WriteTable(&buf, rows)
	out := buf.String()
	if strings.Contains(out, long) || !strings.Contains(out, "…") {
		t.Error("long titles should be shortened")
	}
	if !strings.Contains(out, "a reason that must not be cut off") {
		t.Error("reasons must stay complete")
	}
}

func batchServer(t *testing.T, handler func(ids []int) (int, any)) *devops.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IDs []int }
		json.NewDecoder(r.Body).Decode(&body)
		status, v := handler(body.IDs)
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)
	c := devops.NewClient(srv.URL)
	c.TokenSource = func(context.Context) (string, error) { return "t", nil }
	return c
}

func TestBuildReportBatchesInChunksOf200(t *testing.T) {
	var sizes []int
	c := batchServer(t, func(ids []int) (int, any) {
		sizes = append(sizes, len(ids))
		return 200, map[string]any{"value": []any{}}
	})
	col := NewCollector()
	for id := 1; id <= 450; id++ {
		col.Add(id, "x")
	}
	report, err := BuildReport(context.Background(), c, col, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[200 200 50]" {
		t.Errorf("chunk sizes = %v", sizes)
	}
	if report.Dropped != 450 || len(report.Rows) != 0 {
		t.Errorf("dropped = %d, rows = %d", report.Dropped, len(report.Rows))
	}
}

func TestBuildReportKeepsItemsWhenDetailsFail(t *testing.T) {
	c := batchServer(t, func([]int) (int, any) {
		return 500, map[string]string{"message": "boom"}
	})
	col := NewCollector()
	col.Add(42, "edited 1x")
	rep := &fakeReporter{}

	report, err := BuildReport(context.Background(), c, col, time.UTC, rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 1 || !report.Rows[0].Unresolved || report.Dropped != 0 {
		t.Errorf("report = %+v", report)
	}
	if len(rep.warnings) == 0 {
		t.Error("a failed detail lookup should warn")
	}
	if got := report.Rows[0].Fields(); got[0] != "42" || got[2] != "(details unavailable)" || got[8] != "edited 1x" {
		t.Errorf("fields = %q", got)
	}
}
