package workday

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

type prKey struct {
	repoID string
	id     int
}

type pullRequest struct {
	ID            int         `json:"pullRequestId"`
	Title         string      `json:"title"`
	Status        string      `json:"status"`
	CreationDate  time.Time   `json:"creationDate"`
	ClosedDate    time.Time   `json:"closedDate"`
	CreatedBy     identityRef `json:"createdBy"`
	ClosedBy      identityRef `json:"closedBy"`
	SourceRefName string      `json:"sourceRefName"`
	Repository    struct {
		ID string `json:"id"`
	} `json:"repository"`
}

// prInfo is a pull request plus the project it was found in, which the
// project-less fields of the API response do not reliably carry.
type prInfo struct {
	pullRequest
	project string
}

func (p prInfo) key() prKey { return prKey{p.Repository.ID, p.ID} }

type prThread struct {
	Comments []struct {
		Author        identityRef `json:"author"`
		PublishedDate time.Time   `json:"publishedDate"`
		LastUpdated   time.Time   `json:"lastUpdatedDate"`
	} `json:"comments"`
}

type event struct {
	reason string
	times  []time.Time
}

// prEvents lists how the user took part in a pull request during the day: by
// creating it, by completing or abandoning it, or by writing a thread comment.
// Votes show up as system comments authored by the voter, so counting every
// comment of the user covers them too.
func prEvents(pr pullRequest, threads []prThread, me devops.Identity, day Window) []event {
	var events []event
	if isMe(pr.CreatedBy, me) && day.Contains(pr.CreationDate) {
		events = append(events, event{fmt.Sprintf("PR !%d created", pr.ID), []time.Time{pr.CreationDate}})
	}
	if isMe(pr.ClosedBy, me) && day.Contains(pr.ClosedDate) {
		events = append(events, event{fmt.Sprintf("PR !%d %s", pr.ID, pr.Status), []time.Time{pr.ClosedDate}})
	}
	var commented []time.Time
	for _, t := range threads {
		for _, c := range t.Comments {
			if !isMe(c.Author, me) {
				continue
			}
			for _, at := range []time.Time{c.PublishedDate, c.LastUpdated} {
				if day.Contains(at) {
					commented = append(commented, at)
				}
			}
		}
	}
	if len(commented) > 0 {
		events = append(events, event{fmt.Sprintf("PR !%d commented/voted", pr.ID), commented})
	}
	return events
}

// pullRequests is signal 3. Active PRs plus those completed or abandoned since
// the start of the day are listed per project; the PRs the user took part in
// contribute their linked work items, the IDs in their title and the numbers
// in their source branch. Every listed PR is also indexed by source branch for
// the push signal.
func (f *Finder) pullRequests(ctx context.Context) error {
	closedSince := url.Values{
		"searchCriteria.queryTimeRangeType": {"closed"},
		"searchCriteria.minTime":            {f.opt.Day.StartUTC()},
	}
	queries := []url.Values{{"searchCriteria.status": {"active"}}}
	for _, status := range []string{"completed", "abandoned"} {
		q := url.Values{"searchCriteria.status": {status}}
		for k, v := range closedSince {
			q[k] = v
		}
		queries = append(queries, q)
	}

	var prs []prInfo
	seen := map[prKey]bool{}
	for _, project := range f.opt.Projects {
		for _, base := range queries {
			status := base.Get("searchCriteria.status")
			err := eachPage(ctx, pageSize, func(skip int) (int, error) {
				q := url.Values{"$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa(skip)}}
				for k, v := range base {
					q[k] = v
				}
				var res struct {
					Value []pullRequest `json:"value"`
				}
				if err := f.client.Get(ctx, "/"+pathSeg(project)+"/_apis/git/pullrequests", q, &res); err != nil {
					return 0, err
				}
				for _, pr := range res.Value {
					p := prInfo{pullRequest: pr, project: project}
					if !seen[p.key()] {
						seen[p.key()] = true
						prs = append(prs, p)
					}
				}
				return len(res.Value), nil
			})
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				f.warnf("pull requests", "%s %s pull requests: %v", project, status, err)
			}
		}
	}

	f.prMu.Lock()
	for _, p := range prs {
		k := p.Repository.ID + "|" + p.SourceRefName
		f.prByBranch[k] = append(f.prByBranch[k], p)
	}
	f.prMu.Unlock()

	f.forEach(ctx, "Checking PR threads", len(prs), func(i int) { f.checkPR(ctx, prs[i]) })
	return ctx.Err()
}

func (f *Finder) checkPR(ctx context.Context, pr prInfo) {
	// A PR created after the day cannot have been touched during it.
	if !pr.CreationDate.IsZero() && !pr.CreationDate.Before(f.opt.Day.End) {
		return
	}

	var threads []prThread
	var res struct {
		Value []prThread `json:"value"`
	}
	path := fmt.Sprintf("/%s/_apis/git/repositories/%s/pullRequests/%d/threads",
		pathSeg(pr.project), pathSeg(pr.Repository.ID), pr.ID)
	if err := f.client.Get(ctx, path, nil, &res); err != nil {
		if ctx.Err() != nil {
			return
		}
		// Creation and completion are still known without the threads.
		f.warnf("pull requests", "threads of PR !%d: %v", pr.ID, err)
	} else {
		threads = res.Value
	}

	events := prEvents(pr.pullRequest, threads, f.opt.Me, f.opt.Day)
	if len(events) == 0 {
		return
	}
	ids, err := f.prItemIDs(ctx, pr)
	if err != nil && ctx.Err() == nil {
		f.warnf("pull requests", "work items of PR !%d: %v", pr.ID, err)
	}
	for _, e := range events {
		for _, id := range ids {
			f.col.Add(id, e.reason, e.times...)
		}
	}
}

// prItemIDs returns the work items a pull request refers to: those linked to
// it, IDs in its title and numbers in its source branch. When the link lookup
// fails the other two are still returned together with the error.
func (f *Finder) prItemIDs(ctx context.Context, pr prInfo) ([]int, error) {
	ids := append(IDsInText(pr.Title), BranchIDs(pr.SourceRefName)...)
	linked, err := f.prLinkedItems(ctx, pr)
	return dedupe(append(ids, linked...)), err
}

// flexInt reads a number that the API sends either as a JSON number or string.
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	v, err := strconv.Atoi(strings.Trim(string(b), `"`))
	if err != nil {
		return err
	}
	*n = flexInt(v)
	return nil
}

// prLinkedItems returns the work items linked to a PR, asking the API only once
// per PR.
func (f *Finder) prLinkedItems(ctx context.Context, pr prInfo) ([]int, error) {
	f.linkMu.Lock()
	cached, ok := f.prLinks[pr.key()]
	f.linkMu.Unlock()
	if ok {
		return cached, nil
	}

	var res struct {
		Value []struct {
			ID flexInt `json:"id"`
		} `json:"value"`
	}
	path := fmt.Sprintf("/%s/_apis/git/repositories/%s/pullRequests/%d/workitems",
		pathSeg(pr.project), pathSeg(pr.Repository.ID), pr.ID)
	if err := f.client.Get(ctx, path, nil, &res); err != nil {
		return nil, err
	}
	ids := make([]int, len(res.Value))
	for i, v := range res.Value {
		ids[i] = int(v.ID)
	}

	f.linkMu.Lock()
	f.prLinks[pr.key()] = ids
	f.linkMu.Unlock()
	return ids, nil
}

func (f *Finder) prsForBranch(repoID, ref string) []prInfo {
	f.prMu.Lock()
	defer f.prMu.Unlock()
	return f.prByBranch[repoID+"|"+ref]
}

type repoRef struct {
	project, id, name string
}

type push struct {
	PushID     int       `json:"pushId"`
	Date       time.Time `json:"date"`
	RefUpdates []struct {
		Name string `json:"name"`
	} `json:"refUpdates"`
}

// pushes is signal 4: what the user pushed during the day. Branch names, the
// pull requests of the pushed branches and the commit messages of each push
// all point at work items.
func (f *Finder) pushes(ctx context.Context) error {
	var repos []repoRef
	for _, project := range f.opt.Projects {
		var res struct {
			Value []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				IsDisabled bool   `json:"isDisabled"`
			} `json:"value"`
		}
		if err := f.client.Get(ctx, "/"+pathSeg(project)+"/_apis/git/repositories", nil, &res); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			f.warnf("pushes", "repositories of %s: %v", project, err)
			continue
		}
		for _, r := range res.Value {
			if !r.IsDisabled {
				repos = append(repos, repoRef{project: project, id: r.ID, name: r.Name})
			}
		}
	}

	f.forEach(ctx, "Checking pushes", len(repos), func(i int) {
		if err := f.repoPushes(ctx, repos[i]); err != nil && ctx.Err() == nil {
			f.warnf("pushes", "pushes to %s/%s: %v", repos[i].project, repos[i].name, err)
		}
	})
	return ctx.Err()
}

func (f *Finder) repoPushes(ctx context.Context, repo repoRef) error {
	base := fmt.Sprintf("/%s/_apis/git/repositories/%s/pushes", pathSeg(repo.project), pathSeg(repo.id))

	var pushes []push
	err := eachPage(ctx, pageSize, func(skip int) (int, error) {
		q := url.Values{
			"searchCriteria.pusherId":          {f.opt.Me.ID},
			"searchCriteria.fromDate":          {f.opt.Day.StartUTC()},
			"searchCriteria.toDate":            {f.opt.Day.EndUTC()},
			"searchCriteria.includeRefUpdates": {"true"},
			"$top":                             {strconv.Itoa(pageSize)},
			"$skip":                            {strconv.Itoa(skip)},
		}
		var res struct {
			Value []push `json:"value"`
		}
		if err := f.client.Get(ctx, base, q, &res); err != nil {
			return 0, err
		}
		pushes = append(pushes, res.Value...)
		return len(res.Value), nil
	})
	if err != nil {
		return err
	}

	for _, p := range pushes {
		if !f.opt.Day.Contains(p.Date) {
			continue
		}
		f.addPush(ctx, repo, base, p)
	}
	return nil
}

func (f *Finder) addPush(ctx context.Context, repo repoRef, base string, p push) {
	for _, ru := range p.RefUpdates {
		if !strings.HasPrefix(ru.Name, "refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(ru.Name, "refs/heads/")
		for _, id := range BranchIDs(ru.Name) {
			f.col.Add(id, "pushed branch "+branch, p.Date)
		}
		for _, pr := range f.prsForBranch(repo.id, ru.Name) {
			ids, err := f.prItemIDs(ctx, pr)
			if err != nil && ctx.Err() == nil {
				f.warnf("pushes", "work items of PR !%d: %v", pr.ID, err)
			}
			for _, id := range ids {
				f.col.Add(id, fmt.Sprintf("pushed to branch of PR !%d", pr.ID), p.Date)
			}
		}
	}

	var res struct {
		Commits []struct {
			Comment string `json:"comment"`
		} `json:"commits"`
	}
	q := url.Values{"includeCommits": {"100"}}
	if err := f.client.Get(ctx, fmt.Sprintf("%s/%d", base, p.PushID), q, &res); err != nil {
		if ctx.Err() == nil {
			f.warnf("pushes", "commits of push %d to %s: %v", p.PushID, repo.name, err)
		}
		return
	}
	for _, c := range res.Commits {
		for _, id := range IDsInText(c.Comment) {
			f.col.Add(id, "commit message in push to "+repo.name, p.Date)
		}
	}
}
