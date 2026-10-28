package workday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

type fakeReporter struct {
	mu       sync.Mutex
	warnings []string
}

func (r *fakeReporter) Label(string) {}
func (r *fakeReporter) Warnf(format string, a ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, fmt.Sprintf(format, a...))
}

var (
	// 2026-10-19T22:00Z .. 2026-10-20T22:00Z
	testZone = time.FixedZone("UTC+2", 2*3600)
	testDay  = NewWindow(time.Date(2026, 10, 20, 12, 0, 0, 0, testZone))
	testMe   = devops.Identity{ID: "me-id", UniqueName: "me@example.com", DisplayName: "Me"}
)

func me() map[string]string { return map[string]string{"id": "me-id", "uniqueName": "me@example.com"} }
func other() map[string]string {
	return map[string]string{"id": "other", "uniqueName": "o@example.com"}
}

// fakeADO answers just the endpoints the finder uses. failActivity makes the
// "My activity" call return a server error.
func fakeADO(t *testing.T, failActivity bool) *devops.Client {
	t.Helper()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, q := r.URL.Path, r.URL.Query()
		switch {
		case p == "/_apis/wit/wiql":
			var body struct{ Query string }
			json.NewDecoder(r.Body).Decode(&body)
			ids := []int{1, 2}
			if strings.Contains(body.Query, "[System.AssignedTo] = @Me") {
				ids = []int{1, 9} // 1 already has a reason, 9 does not
			}
			var items []map[string]int
			for _, id := range ids {
				items = append(items, map[string]int{"id": id})
			}
			write(w, map[string]any{"workItems": items})

		case p == "/_apis/wit/workItems/1/revisions":
			// 250 revisions force a second page. Only the first and the last
			// are by me inside the day.
			skip, _ := strconv.Atoi(q.Get("$skip"))
			top, _ := strconv.Atoi(q.Get("$top"))
			var revs []map[string]any
			for i := skip; i < min(skip+top, 250); i++ {
				by, at := other(), "2026-10-20T08:00:00Z"
				switch i {
				case 0:
					by = me()
				case 249:
					by, at = me(), "2026-10-20T15:00:00Z"
				case 5:
					by, at = me(), "2026-10-19T21:59:00Z" // me, but just before the day
				}
				revs = append(revs, map[string]any{"fields": map[string]any{
					"System.ChangedBy": by, "System.ChangedDate": at}})
			}
			write(w, map[string]any{"value": revs})
		case p == "/_apis/wit/workItems/2/revisions":
			// Edited by someone else only.
			write(w, map[string]any{"value": []map[string]any{{"fields": map[string]any{
				"System.ChangedBy": other(), "System.ChangedDate": "2026-10-20T08:00:00Z"}}}})

		case p == "/_apis/work/accountmyworkrecentactivity":
			if failActivity {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			write(w, map[string]any{"value": []map[string]any{
				{"id": 4, "activityType": "visited", "activityDate": "2026-10-20T12:00:00Z"},
				{"id": 5, "activityType": "edited", "activityDate": "2026-10-21T12:00:00Z"}, // next day
			}})

		case p == "/P/_apis/git/pullrequests":
			if q.Get("searchCriteria.status") != "active" {
				write(w, map[string]any{"value": []any{}})
				return
			}
			write(w, map[string]any{"value": []map[string]any{{
				"pullRequestId": 10, "title": "Fix crash #11", "status": "active",
				"creationDate":  "2026-10-10T08:00:00Z",
				"createdBy":     other(),
				"sourceRefName": "refs/heads/feature/777-x",
				"repository":    map[string]string{"id": "r1"},
			}}})
		case p == "/P/_apis/git/repositories/r1/pullRequests/10/threads":
			write(w, map[string]any{"value": []map[string]any{{"comments": []map[string]any{
				{"author": other(), "publishedDate": "2026-10-20T08:00:00Z", "lastUpdatedDate": "2026-10-20T08:00:00Z"},
				{"author": me(), "publishedDate": "2026-10-20T09:00:00Z", "lastUpdatedDate": "2026-10-20T09:30:00Z"},
			}}}})
		case p == "/P/_apis/git/repositories/r1/pullRequests/10/workitems":
			write(w, map[string]any{"value": []map[string]string{{"id": "12"}}})

		case p == "/P/_apis/git/repositories":
			write(w, map[string]any{"value": []map[string]any{
				{"id": "r1", "name": "Repo", "isDisabled": false},
				{"id": "r2", "name": "Old", "isDisabled": true},
			}})
		case p == "/P/_apis/git/repositories/r1/pushes":
			if q.Get("searchCriteria.pusherId") != "me-id" {
				t.Errorf("pusherId = %q", q.Get("searchCriteria.pusherId"))
			}
			write(w, map[string]any{"value": []map[string]any{{
				"pushId": 55, "date": "2026-10-20T10:00:00Z",
				"refUpdates": []map[string]string{{"name": "refs/heads/feature/777-x"}},
			}}})
		case p == "/P/_apis/git/repositories/r1/pushes/55":
			write(w, map[string]any{"commits": []map[string]string{{"comment": "wip, closes #13"}}})
		case strings.HasPrefix(p, "/P/_apis/git/repositories/r2/"):
			t.Errorf("disabled repo was queried: %s", p)

		case p == "/_apis/wit/workitemsbatch":
			var body struct{ IDs []int }
			json.NewDecoder(r.Body).Decode(&body)
			known := map[int]map[string]any{
				1:   {"System.WorkItemType": "Task", "System.Title": "Implement", "System.State": "Active", "System.TeamProject": "P", "System.Parent": 100},
				4:   {"System.WorkItemType": "Bug", "System.Title": "Viewed", "System.State": "New", "System.TeamProject": "P"},
				9:   {"System.WorkItemType": "Task", "System.Title": "Assigned", "System.State": "Active", "System.TeamProject": "P"},
				11:  {"System.WorkItemType": "Bug", "System.Title": "Crash", "System.State": "Active", "System.TeamProject": "P"},
				12:  {"System.WorkItemType": "Bug", "System.Title": "Linked", "System.State": "Active", "System.TeamProject": "P"},
				13:  {"System.WorkItemType": "Bug", "System.Title": "Commit", "System.State": "Active", "System.TeamProject": "P"},
				100: {"System.WorkItemType": "User Story", "System.Title": "The story", "System.State": "Active", "System.TeamProject": "P"},
			}
			var out []any
			for _, id := range body.IDs {
				if f, ok := known[id]; ok {
					out = append(out, map[string]any{"id": id, "fields": f})
				} else {
					out = append(out, nil) // errorPolicy=omit
				}
			}
			write(w, map[string]any{"value": out})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := devops.NewClient(srv.URL)
	c.TokenSource = func(context.Context) (string, error) { return "t", nil }
	return c
}

func runFinder(t *testing.T, client *devops.Client) (*Report, *fakeReporter) {
	t.Helper()
	rep := &fakeReporter{}
	f := NewFinder(client, Options{Projects: []string{"P"}, Day: testDay, Me: testMe}, rep)
	if err := f.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	report, err := BuildReport(context.Background(), client, f.Collector(), testZone, rep)
	if err != nil {
		t.Fatal(err)
	}
	return report, rep
}

func rowByID(r *Report, id int) *Row {
	for i := range r.Rows {
		if r.Rows[i].ID == id {
			return &r.Rows[i]
		}
	}
	return nil
}

func TestFinderEndToEnd(t *testing.T) {
	report, rep := runFinder(t, fakeADO(t, false))
	if len(rep.warnings) != 0 {
		t.Errorf("unexpected warnings: %v", rep.warnings)
	}

	// 2 was a WIQL hit but only somebody else edited it: not listed. 777 is
	// the number in the branch name and no work item: dropped.
	if got := rowByID(report, 2); got != nil {
		t.Errorf("item 2 should not be listed: %+v", got)
	}
	if report.Dropped != 1 {
		t.Errorf("Dropped = %d, want 1 (branch number 777)", report.Dropped)
	}

	r1 := rowByID(report, 1)
	if r1 == nil {
		t.Fatal("item 1 missing")
	}
	// The assigned reason is dropped, edits by others and outside the day are
	// ignored.
	if got := strings.Join(r1.Reasons, "; "); got != "edited 2x: created" {
		t.Errorf("item 1 reasons = %q", got)
	}
	if clock(r1.First) != "10:00" || clock(r1.Last) != "17:00" {
		t.Errorf("item 1 first/last = %s/%s, want 10:00/17:00 local", clock(r1.First), clock(r1.Last))
	}
	if r1.ParentID != 100 || r1.ParentTitle != "The story" {
		t.Errorf("item 1 parent = %d %q", r1.ParentID, r1.ParentTitle)
	}

	r4 := rowByID(report, 4)
	if r4 == nil || strings.Join(r4.Reasons, ";") != "my activity: visited" || clock(r4.First) != "14:00" {
		t.Errorf("item 4 = %+v", r4)
	}
	if rowByID(report, 5) != nil {
		t.Error("item 5 was visited on the next day and must not be listed")
	}

	r9 := rowByID(report, 9)
	if r9 == nil || strings.Join(r9.Reasons, ";") != "assigned to you, open (no activity found)" || !r9.First.IsZero() {
		t.Errorf("item 9 = %+v", r9)
	}

	// 11 comes from the PR title, 12 from the PR link; both with the time of
	// my comment, and both again through the push to the PR's branch.
	for _, id := range []int{11, 12} {
		r := rowByID(report, id)
		if r == nil {
			t.Errorf("item %d missing", id)
			continue
		}
		reasons := strings.Join(r.Reasons, ";")
		if !strings.Contains(reasons, "PR !10 commented/voted") || !strings.Contains(reasons, "pushed to branch of PR !10") {
			t.Errorf("item %d reasons = %v", id, r.Reasons)
		}
		if clock(r.First) != "11:00" {
			t.Errorf("item %d first = %s, want 11:00", id, clock(r.First))
		}
	}

	r13 := rowByID(report, 13)
	if r13 == nil || !strings.Contains(strings.Join(r13.Reasons, ";"), "commit message in push to Repo") || clock(r13.First) != "12:00" {
		t.Errorf("item 13 = %+v", r13)
	}

	// Timestamped rows first by First (1 at 10:00, 11 and 12 at 11:00, 13 at
	// 12:00, 4 at 14:00), then the rest by ID.
	var order []int
	for _, r := range report.Rows {
		order = append(order, r.ID)
	}
	if got := fmt.Sprint(order); got != "[1 11 12 13 4 9]" {
		t.Errorf("order = %v", order)
	}
}

func TestFailingSourceOnlyWarns(t *testing.T) {
	report, rep := runFinder(t, fakeADO(t, true))
	if len(rep.warnings) != 1 || !strings.Contains(rep.warnings[0], "my activity") {
		t.Errorf("warnings = %v", rep.warnings)
	}
	if rowByID(report, 1) == nil || rowByID(report, 13) == nil {
		t.Error("other sources must still contribute")
	}
	if rowByID(report, 4) != nil {
		t.Error("item 4 only came from the failed source")
	}
}

func TestCollectStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := NewFinder(fakeADO(t, false), Options{Projects: []string{"P"}, Day: testDay, Me: testMe}, nil)
	if err := f.Collect(ctx); err == nil {
		t.Fatal("a cancelled run must return an error, not partial results")
	}
}

func TestPRLinkedItemsAreCached(t *testing.T) {
	var calls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Write([]byte(`{"value":[{"id":"5"}]}`))
	}))
	defer srv.Close()
	c := devops.NewClient(srv.URL)
	c.TokenSource = func(context.Context) (string, error) { return "t", nil }
	f := NewFinder(c, Options{Day: testDay, Me: testMe}, nil)

	pr := prInfo{pullRequest: pullRequest{ID: 1}, project: "P"}
	for i := 0; i < 3; i++ {
		if _, err := f.prLinkedItems(context.Background(), pr); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Errorf("API called %d times, want 1", calls)
	}
}

func TestPREventsCountsCompletionAndEditedComments(t *testing.T) {
	pr := pullRequest{
		ID: 7, Status: "completed",
		CreationDate: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC),
		ClosedDate:   time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		ClosedBy:     identityRef{ID: "ME-ID"}, // IDs compare case-insensitively
	}
	var th prThread
	if err := json.Unmarshal([]byte(`{"comments":[{
		"author":{"uniqueName":"ME@example.com"},
		"publishedDate":"2026-10-18T09:00:00Z",
		"lastUpdatedDate":"2026-10-20T07:00:00Z"}]}`), &th); err != nil {
		t.Fatal(err)
	}

	var reasons []string
	for _, e := range prEvents(pr, []prThread{th}, testMe, testDay) {
		reasons = append(reasons, e.reason)
	}
	if got := fmt.Sprint(reasons); got != "[PR !7 completed PR !7 commented/voted]" {
		t.Errorf("reasons = %v", reasons)
	}
}
