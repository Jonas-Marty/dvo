package workday

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

// DefaultClosedStates are the states for which an assigned item no longer
// counts as "open" in the last-resort signal.
var DefaultClosedStates = []string{"New", "Proposed", "To Do", "Closed", "Done", "Removed", "Resolved"}

// Reporter receives progress and warnings. ui.Progress implements it.
type Reporter interface {
	Label(s string)
	Warnf(format string, a ...any)
}

// Options describes one run.
type Options struct {
	Projects     []string
	Day          Window
	Me           devops.Identity
	RepoPaths    []string // local git repositories to scan, optional
	Author       string   // git author pattern for local repositories
	ClosedStates []string
}

// workers bounds concurrent REST calls. Azure DevOps throttles per user, and
// the client backs off on 429, so a modest pool is already fast enough.
const workers = 8

// maxWarnings is how many warnings one source prints before it goes quiet.
const maxWarnings = 5

// Finder runs every signal source and funnels the results into a Collector.
type Finder struct {
	client *devops.Client
	opt    Options
	rep    Reporter
	col    *Collector

	warnMu sync.Mutex
	warned map[string]int

	// prByBranch maps repoID+"|"+sourceRefName to the pull requests seen for
	// that branch, so a push can be tied to its PRs.
	prMu       sync.Mutex
	prByBranch map[string][]prInfo

	// prLinks caches the work items linked to a pull request. Both the PR
	// signal and the push signal ask for them.
	linkMu  sync.Mutex
	prLinks map[prKey][]int
}

func NewFinder(client *devops.Client, opt Options, rep Reporter) *Finder {
	if rep == nil {
		rep = nopReporter{}
	}
	if len(opt.ClosedStates) == 0 {
		opt.ClosedStates = DefaultClosedStates
	}
	return &Finder{
		client:     client,
		opt:        opt,
		rep:        rep,
		col:        NewCollector(),
		warned:     map[string]int{},
		prByBranch: map[string][]prInfo{},
		prLinks:    map[prKey][]int{},
	}
}

// Collector returns the signals gathered by Collect.
func (f *Finder) Collector() *Collector { return f.col }

type nopReporter struct{}

func (nopReporter) Label(string)         {}
func (nopReporter) Warnf(string, ...any) {}

// Collect runs all sources. A failing source only produces a warning; the one
// thing that stops the run is cancellation, which is returned right away so a
// half-finished result is never mistaken for a complete one.
func (f *Finder) Collect(ctx context.Context) error {
	sources := []struct {
		name string
		run  func(context.Context) error
	}{
		{"revisions", f.revisions},
		{"my activity", f.myActivity},
		{"pull requests", f.pullRequests},
		{"pushes", f.pushes},
		{"local repos", f.localRepos},
		// Last on purpose: it only fills in items nothing else mentioned.
		{"assigned items", f.assigned},
	}
	for _, s := range sources {
		f.rep.Label("Collecting " + s.name + "...")
		if err := s.run(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			f.warnf(s.name, "%s: %v", s.name, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// warnf reports a non-fatal problem. Each source prints at most maxWarnings
// lines so one broken permission does not bury the output in hundreds of
// identical errors.
func (f *Finder) warnf(source, format string, a ...any) {
	f.warnMu.Lock()
	f.warned[source]++
	n := f.warned[source]
	f.warnMu.Unlock()
	switch {
	case n <= maxWarnings:
		f.rep.Warnf(format, a...)
	case n == maxWarnings+1:
		f.rep.Warnf("%s: further warnings suppressed", source)
	}
}

// forEach calls fn for every index in [0, n) on a pool of workers and reports
// "<label> done/n" as it goes. It returns early, without starting new work,
// once ctx is cancelled; callers check ctx.Err() afterwards.
func (f *Finder) forEach(ctx context.Context, label string, n int, fn func(i int)) {
	if n == 0 {
		return
	}
	var next, done atomic.Int64
	var wg sync.WaitGroup
	f.rep.Label(fmt.Sprintf("%s 0/%d", label, n))
	for w := 0; w < min(workers, n); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
				f.rep.Label(fmt.Sprintf("%s %d/%d", label, done.Add(1), n))
			}
		}()
	}
	wg.Wait()
}

// pageSize is the page size used for every $top/$skip loop.
const pageSize = 100

// eachPage calls fetch with increasing $skip until a page comes back short.
func eachPage(ctx context.Context, size int, fetch func(skip int) (int, error)) error {
	for skip := 0; ; skip += size {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := fetch(skip)
		if err != nil {
			return err
		}
		if n < size {
			return nil
		}
	}
}

func pathSeg(s string) string { return url.PathEscape(s) }

func wiqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func wiqlList(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = wiqlQuote(v)
	}
	return strings.Join(q, ", ")
}

// wiql runs a query and returns the matching IDs.
func (f *Finder) wiql(ctx context.Context, query string) ([]int, error) {
	var res struct {
		WorkItems []struct {
			ID int `json:"id"`
		} `json:"workItems"`
	}
	if err := f.client.Post(ctx, "/_apis/wit/wiql", map[string]string{"query": query}, &res); err != nil {
		return nil, err
	}
	ids := make([]int, len(res.WorkItems))
	for i, w := range res.WorkItems {
		ids[i] = w.ID
	}
	return ids, nil
}
