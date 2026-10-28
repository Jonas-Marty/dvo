package workday

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/config"
	"github.com/Jonas-Marty/ad-cli/internal/git"
)

var checkoutRe = regexp.MustCompile(`^checkout: moving from .+ to (.+)$`)

var hexRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// checkoutTarget extracts the branch a reflog subject switched to. Switching
// to a bare commit (detached HEAD) yields nothing: the digits inside a SHA
// would read as work item numbers.
func checkoutTarget(subject string) (string, bool) {
	m := checkoutRe.FindStringSubmatch(strings.TrimSpace(subject))
	if m == nil || hexRe.MatchString(m[1]) {
		return "", false
	}
	return m[1], true
}

// localRepos is signal 5: commits the user wrote and branches the user
// checked out in local repositories. This sees work that was never pushed.
// A repository that cannot be read is reported and skipped.
func (f *Finder) localRepos(ctx context.Context) error {
	for _, path := range f.opt.RepoPaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := config.NormalizePath(path)
		f.rep.Label("Reading local repo " + filepath.Base(dir) + "...")
		if err := f.scanRepo(ctx, dir); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			f.warnf("local repos", "local repo %s: %v", dir, err)
		}
	}
	return nil
}

func (f *Finder) scanRepo(ctx context.Context, dir string) error {
	name := filepath.Base(filepath.Clean(dir))

	commits, err := git.CommitLog(ctx, dir, f.opt.Author, f.opt.Day.Start)
	if err != nil {
		return err
	}
	for _, c := range commits {
		// Either date can place the commit in the day: a rebase leaves the
		// author date alone but moves the commit date.
		at := c.AuthorDate
		if !f.opt.Day.Contains(at) {
			at = c.CommitDate
		}
		if !f.opt.Day.Contains(at) {
			continue
		}
		for _, id := range IDsInText(c.Message) {
			f.col.Add(id, "local commit message ("+name+")", at)
		}
	}

	entries, err := git.HeadReflog(ctx, dir)
	if err != nil {
		return fmt.Errorf("reflog: %w", err)
	}
	for _, e := range entries {
		if !f.opt.Day.Contains(e.Time) {
			continue
		}
		branch, ok := checkoutTarget(e.Subject)
		if !ok {
			continue
		}
		for _, id := range BranchIDs(branch) {
			f.col.Add(id, fmt.Sprintf("checked out %s (%s)", branch, name), e.Time)
		}
	}
	return nil
}
