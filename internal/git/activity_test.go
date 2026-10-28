package git

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func gitIn(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, nil, "init", "-q", "-b", "main")
	gitIn(t, dir, nil, "config", "user.name", "Test")
	gitIn(t, dir, nil, "config", "user.email", "me@example.com")
	gitIn(t, dir, nil, "config", "commit.gpgsign", "false")
	return dir
}

func commitAt(t *testing.T, dir, msg, author, date string) {
	t.Helper()
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	gitIn(t, dir, env, "commit", "-q", "--allow-empty", "--author="+author, "-m", msg)
}

func TestCommitLogFiltersAuthorAndSince(t *testing.T) {
	dir := newRepo(t)
	commitAt(t, dir, "old work #1", "Me <me@example.com>", "2026-10-01T09:00:00+02:00")
	commitAt(t, dir, "today work #2\n\nmore body #3", "Me <me@example.com>", "2026-10-20T09:00:00+02:00")
	commitAt(t, dir, "someone else #4", "Other <o@example.com>", "2026-10-20T10:00:00+02:00")
	// An unpushed side branch is part of --all.
	gitIn(t, dir, nil, "checkout", "-q", "-b", "side")
	commitAt(t, dir, "side branch #5", "Me <ME@Example.com>", "2026-10-20T11:00:00+02:00")

	since := time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("x", 2*3600))
	commits, err := CommitLog(context.Background(), dir, "me@example.com", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(commits), commits)
	}
	var all string
	for _, c := range commits {
		all += c.Message + "|"
		if c.AuthorDate.IsZero() || c.CommitDate.IsZero() {
			t.Errorf("dates were not parsed: %+v", c)
		}
	}
	for _, want := range []string{"today work #2", "more body #3", "side branch #5"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in %q", want, all)
		}
	}
}

func TestHeadReflogParsesCheckouts(t *testing.T) {
	dir := newRepo(t)
	commitAt(t, dir, "init", "Me <me@example.com>", "2026-10-20T09:00:00+02:00")
	gitIn(t, dir, nil, "checkout", "-q", "-b", "feature/1234-x")

	entries, err := HeadReflog(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if e.Subject == "checkout: moving from main to feature/1234-x" {
			found = true
			if time.Since(e.Time) > time.Hour || e.Time.IsZero() {
				t.Errorf("entry time = %v", e.Time)
			}
		}
	}
	if !found {
		t.Errorf("checkout entry not found in %+v", entries)
	}
}

func TestCommitLogReportsBadRepo(t *testing.T) {
	_, err := CommitLog(context.Background(), t.TempDir(), "", time.Now())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("err = %v", err)
	}
}
