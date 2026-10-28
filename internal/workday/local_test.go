package workday

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
)

func gitIn(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// The reflog entries get the real current time, so the window is built around
// "now"; the commits get explicit dates.
func TestLocalReposSignal(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, nil, "init", "-q", "-b", "main")
	gitIn(t, dir, nil, "config", "user.name", "Test")
	gitIn(t, dir, nil, "config", "user.email", "me@example.com")
	gitIn(t, dir, nil, "config", "commit.gpgsign", "false")

	now := time.Now()
	day := NewWindow(now)
	inDay := day.Start.Add(time.Minute).Format(time.RFC3339)
	before := day.Start.Add(-time.Hour).Format(time.RFC3339)
	commit := func(msg, date string) {
		env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
		gitIn(t, dir, env, "commit", "-q", "--allow-empty", "-m", msg)
	}
	commit("yesterday #111", before)
	commit("unpushed #222", inDay)
	gitIn(t, dir, nil, "checkout", "-q", "-b", "feature/3333-thing")

	rep := &fakeReporter{}
	f := NewFinder(nil, Options{
		Day: day, Me: devops.Identity{UniqueName: "me@example.com"},
		RepoPaths: []string{dir, t.TempDir()}, // second path is not a repo
		Author:    "me@example.com",
	}, rep)
	if err := f.localRepos(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := f.Collector().Entries()
	reasons := map[int]string{}
	for _, e := range got {
		reasons[e.ID] = strings.Join(e.Reasons, ";")
	}
	if !strings.Contains(reasons[222], "local commit message") {
		t.Errorf("unpushed commit not found: %v", reasons)
	}
	if _, ok := reasons[111]; ok {
		t.Errorf("commit from before the day was included: %v", reasons)
	}
	if !strings.Contains(reasons[3333], "checked out feature/3333-thing") {
		t.Errorf("checkout not found: %v", reasons)
	}
	if len(rep.warnings) != 1 || !strings.Contains(rep.warnings[0], "local repo") {
		t.Errorf("a non-repo path should warn once and not abort: %v", rep.warnings)
	}
}
