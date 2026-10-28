package workday

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

// Row is one line of the final report.
type Row struct {
	ID          int
	Type        string
	Title       string
	State       string
	Project     string
	ParentID    int
	ParentTitle string
	First, Last time.Time
	Reasons     []string

	// Unresolved marks an item whose details could not be fetched. It is kept
	// with its ID and reasons rather than dropped.
	Unresolved bool
}

// Report is the result of a run.
type Report struct {
	Rows []Row
	// Dropped counts candidate IDs that are not work items (typically digits
	// in a branch name). They are left out silently.
	Dropped int
}

type workItem struct {
	ID     int `json:"id"`
	Fields struct {
		Type    string  `json:"System.WorkItemType"`
		Title   string  `json:"System.Title"`
		State   string  `json:"System.State"`
		Project string  `json:"System.TeamProject"`
		Parent  flexInt `json:"System.Parent"`
	} `json:"fields"`
}

const batchSize = 200

// fetchItems resolves ids through the batch endpoint. IDs that do not exist
// are omitted by the service. IDs of a chunk that failed are returned in
// failed so the caller can keep them instead of mistaking them for missing.
func fetchItems(ctx context.Context, c *devops.Client, ids []int, rep Reporter) (found map[int]workItem, failed []int, err error) {
	found = map[int]workItem{}
	for start := 0; start < len(ids); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		chunk := ids[start:min(start+batchSize, len(ids))]
		body := map[string]any{
			"ids": chunk,
			"fields": []string{
				"System.Id", "System.WorkItemType", "System.Title",
				"System.State", "System.TeamProject", "System.Parent",
			},
			"errorPolicy": "omit",
		}
		var res struct {
			Value []*workItem `json:"value"`
		}
		if err := c.Post(ctx, "/_apis/wit/workitemsbatch", body, &res); err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			rep.Warnf("work item details (%d items): %v", len(chunk), err)
			failed = append(failed, chunk...)
			continue
		}
		for _, w := range res.Value {
			if w != nil {
				found[w.ID] = *w
			}
		}
	}
	return found, failed, nil
}

// BuildReport resolves the collected IDs, adds parent titles and sorts.
func BuildReport(ctx context.Context, c *devops.Client, col *Collector, loc *time.Location, rep Reporter) (*Report, error) {
	if rep == nil {
		rep = nopReporter{}
	}
	entries := col.Entries()
	ids := make([]int, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}

	rep.Label(fmt.Sprintf("Loading details of %d work items...", len(ids)))
	items, failed, err := fetchItems(ctx, c, ids, rep)
	if err != nil {
		return nil, err
	}
	failedSet := make(map[int]bool, len(failed))
	for _, id := range failed {
		failedSet[id] = true
	}

	// Customer reports are often booked on the parent story or feature.
	var parentIDs []int
	for _, w := range items {
		if p := int(w.Fields.Parent); p > 0 {
			if _, known := items[p]; !known {
				parentIDs = append(parentIDs, p)
			}
		}
	}
	sort.Ints(parentIDs)
	parentIDs = dedupe(parentIDs)
	rep.Label("Loading parent items...")
	parents, _, err := fetchItems(ctx, c, parentIDs, rep)
	if err != nil {
		return nil, err
	}

	report := &Report{}
	for _, e := range entries {
		row := Row{ID: e.ID, First: e.First.In(loc), Last: e.Last.In(loc), Reasons: e.Reasons}
		if e.First.IsZero() {
			row.First, row.Last = time.Time{}, time.Time{}
		}
		w, ok := items[e.ID]
		switch {
		case ok:
			row.Type, row.Title, row.State, row.Project =
				w.Fields.Type, w.Fields.Title, w.Fields.State, w.Fields.Project
			if p := int(w.Fields.Parent); p > 0 {
				row.ParentID = p
				if pw, ok := items[p]; ok {
					row.ParentTitle = pw.Fields.Title
				} else if pw, ok := parents[p]; ok {
					row.ParentTitle = pw.Fields.Title
				}
			}
		case failedSet[e.ID]:
			row.Unresolved = true
		default:
			report.Dropped++
			continue
		}
		report.Rows = append(report.Rows, row)
	}
	sortRows(report.Rows)
	return report, nil
}

// sortRows puts items with timestamps first, ordered by first activity, then
// the rest by ID.
func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch {
		case a.First.IsZero() != b.First.IsZero():
			return !a.First.IsZero()
		case !a.First.IsZero() && !a.First.Equal(b.First):
			return a.First.Before(b.First)
		}
		return a.ID < b.ID
	})
}

// Header is the column order of both the table and the CSV.
var Header = []string{"Id", "Type", "Title", "State", "Project", "Parent", "First", "Last", "Reasons"}

func clock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("15:04")
}

// Fields renders a row in Header order.
func (r Row) Fields() []string {
	parent := ""
	if r.ParentID > 0 {
		parent = strings.TrimSpace(fmt.Sprintf("%d %s", r.ParentID, r.ParentTitle))
	}
	title := r.Title
	if r.Unresolved {
		title = "(details unavailable)"
	}
	return []string{
		fmt.Sprint(r.ID), r.Type, title, r.State, r.Project, parent,
		clock(r.First), clock(r.Last), strings.Join(r.Reasons, "; "),
	}
}

// WriteCSV writes the rows in a form Excel opens correctly: UTF-8 with a byte
// order mark, CRLF line ends and the given field separator.
func WriteCSV(w io.Writer, rows []Row, sep rune) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = sep
	cw.UseCRLF = true
	if err := cw.Write(Header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write(r.Fields()); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteTable prints the rows as an aligned text table. Title and parent are
// shortened to keep lines readable; the reasons stay complete because they are
// the point of the report.
func WriteTable(w io.Writer, rows []Row) {
	cells := make([][]string, 0, len(rows)+1)
	cells = append(cells, Header)
	for _, r := range rows {
		f := r.Fields()
		f[2] = truncate(f[2], 50)
		f[5] = truncate(f[5], 40)
		cells = append(cells, f)
	}

	widths := make([]int, len(Header)-1) // the last column is not padded
	for _, row := range cells {
		for i := range widths {
			widths[i] = max(widths[i], len([]rune(row[i])))
		}
	}
	for _, row := range cells {
		var b strings.Builder
		for i, c := range row {
			if i == len(row)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(c)
			b.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))+2))
		}
		fmt.Fprintln(w, b.String())
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
