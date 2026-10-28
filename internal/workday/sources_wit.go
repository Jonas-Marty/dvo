package workday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

// identityRef is a user reference as the REST API returns it. Work item
// revisions on older API versions give a plain "Name <mail>" string instead of
// an object, so both shapes are accepted.
type identityRef struct {
	ID         string `json:"id"`
	UniqueName string `json:"uniqueName"`
	text       string
}

func (r *identityRef) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &r.text)
	}
	type plain identityRef
	return json.Unmarshal(b, (*plain)(r))
}

func isMe(r identityRef, me devops.Identity) bool {
	switch {
	case r.ID != "" && strings.EqualFold(r.ID, me.ID):
		return true
	case r.UniqueName != "" && strings.EqualFold(r.UniqueName, me.UniqueName):
		return true
	case r.text != "" && me.UniqueName != "":
		return strings.Contains(strings.ToLower(r.text), strings.ToLower(me.UniqueName))
	}
	return false
}

// revisions is signal 1: the exact edits. A WIQL query with generous bounds
// finds candidates, then each candidate's revision history is checked for
// revisions made by the user inside the day. That covers field edits,
// comments, state changes, links and attachments, even when someone else
// changed the item afterwards.
func (f *Finder) revisions(ctx context.Context) error {
	query := fmt.Sprintf(
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] IN (%s) "+
			"AND [System.ChangedDate] >= %s "+
			"AND ([System.ChangedBy] EVER @Me OR [System.AssignedTo] EVER @Me)",
		wiqlList(f.opt.Projects), wiqlQuote(f.opt.Day.WIQLFloor()))
	ids, err := f.wiql(ctx, query)
	if err != nil {
		return fmt.Errorf("candidate query failed: %w", err)
	}

	f.forEach(ctx, "Checking revisions", len(ids), func(i int) {
		revs, err := f.revisionsOf(ctx, ids[i])
		if err != nil {
			if ctx.Err() == nil {
				f.warnf("revisions", "revisions of #%d: %v", ids[i], err)
			}
			return
		}
		reason, times := describeEdits(revs, func(r revision) bool {
			return isMe(r.changedBy(), f.opt.Me) && f.opt.Day.Contains(r.changedDate())
		})
		if len(times) > 0 {
			f.col.Add(ids[i], reason, times...)
		}
	})
	return ctx.Err()
}

// revisionsOf pages through the complete history of one item. The whole
// history is needed, not just the part inside the day: what an edit changed
// is only visible against the revision before it.
func (f *Finder) revisionsOf(ctx context.Context, id int) ([]revision, error) {
	const top = 200
	var revs []revision
	err := eachPage(ctx, top, func(skip int) (int, error) {
		var res struct {
			Value []revision `json:"value"`
		}
		q := url.Values{"$top": {strconv.Itoa(top)}, "$skip": {strconv.Itoa(skip)}}
		if err := f.client.Get(ctx, fmt.Sprintf("/_apis/wit/workItems/%d/revisions", id), q, &res); err != nil {
			return 0, err
		}
		revs = append(revs, res.Value...)
		return len(res.Value), nil
	})
	return revs, err
}

// myActivity is signal 2: the "My activity" list of recently viewed and edited
// items. The service keeps only the latest activity per item, so it is most
// useful when run on the same day.
func (f *Finder) myActivity(ctx context.Context) error {
	var res struct {
		Value []struct {
			ID           int       `json:"id"`
			ActivityType string    `json:"activityType"`
			ActivityDate time.Time `json:"activityDate"`
		} `json:"value"`
	}
	if err := f.client.Get(ctx, "/_apis/work/accountmyworkrecentactivity", nil, &res); err != nil {
		return err
	}
	for _, a := range res.Value {
		if f.opt.Day.Contains(a.ActivityDate) {
			f.col.Add(a.ID, "my activity: "+strings.ToLower(a.ActivityType), a.ActivityDate)
		}
	}
	return nil
}

// assigned is signal 6, the last resort: open items assigned to the user. They
// carry no timestamp and are only listed when no other signal found them, so
// they never dilute a real reason.
func (f *Finder) assigned(ctx context.Context) error {
	query := fmt.Sprintf(
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] IN (%s) "+
			"AND [System.AssignedTo] = @Me AND [System.State] NOT IN (%s)",
		wiqlList(f.opt.Projects), wiqlList(f.opt.ClosedStates))
	ids, err := f.wiql(ctx, query)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !f.col.Has(id) {
			f.col.Add(id, "assigned to you, open (no activity found)")
		}
	}
	return nil
}
