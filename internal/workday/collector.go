package workday

import (
	"sort"
	"sync"
	"time"
)

// Entry is everything the signals found out about one work item.
type Entry struct {
	ID      int
	Reasons []string  // sorted, unique
	First   time.Time // zero when no signal carried a timestamp
	Last    time.Time
}

// Collector gathers signals from concurrent sources.
type Collector struct {
	mu sync.Mutex
	m  map[int]*entryState
}

type entryState struct {
	reasons     map[string]struct{}
	first, last time.Time
}

func NewCollector() *Collector {
	return &Collector{m: map[int]*entryState{}}
}

// Add records that id was touched for reason. times are the moments the
// activity happened; any of them may be omitted.
func (c *Collector) Add(id int, reason string, times ...time.Time) {
	if id <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[id]
	if e == nil {
		e = &entryState{reasons: map[string]struct{}{}}
		c.m[id] = e
	}
	e.reasons[reason] = struct{}{}
	for _, t := range times {
		if t.IsZero() {
			continue
		}
		if e.first.IsZero() || t.Before(e.first) {
			e.first = t
		}
		if e.last.IsZero() || t.After(e.last) {
			e.last = t
		}
	}
}

// Has reports whether any signal has mentioned id.
func (c *Collector) Has(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.m[id]
	return ok
}

// Entries returns all items sorted by ID.
func (c *Collector) Entries() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entry, 0, len(c.m))
	for id, e := range c.m {
		reasons := make([]string, 0, len(e.reasons))
		for r := range e.reasons {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
		out = append(out, Entry{ID: id, Reasons: reasons, First: e.first, Last: e.last})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
