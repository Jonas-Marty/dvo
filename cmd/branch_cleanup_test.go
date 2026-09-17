package cmd

import (
	"os/exec"
	"strings"
	"testing"
)

// runGit runs git in dir and fails the test if it errors.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit creates an empty commit with the given message and returns its SHA.
func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	runGit(t, dir, "commit", "--allow-empty", "-m", msg)
	return runGit(t, dir, "rev-parse", "HEAD")
}

// newRepo builds a repo whose main branch has one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	commit(t, dir, "initial")
	return dir
}

func TestClassifyBranch(t *testing.T) {
	tests := []struct {
		name string
		// setup prepares the repo and returns the branch to classify plus the PR
		// merge tips the Azure DevOps lookup would have reported for it.
		setup func(t *testing.T, dir string) (branch string, prTips []string)
		want  mergeState
	}{
		{
			name: "fully merged into HEAD",
			setup: func(t *testing.T, dir string) (string, []string) {
				runGit(t, dir, "checkout", "-b", "feature")
				commit(t, dir, "work")
				runGit(t, dir, "checkout", "main")
				runGit(t, dir, "merge", "--ff-only", "feature")
				return "feature", nil
			},
			want: stateGitMerged,
		},
		{
			name: "squash-merged, tip unchanged since the PR",
			setup: func(t *testing.T, dir string) (string, []string) {
				runGit(t, dir, "checkout", "-b", "feature")
				tip := commit(t, dir, "work")
				runGit(t, dir, "checkout", "main")
				commit(t, dir, "squashed work") // main gets the content under a new SHA
				return "feature", []string{tip}
			},
			want: statePRMerged,
		},
		{
			name: "commit pushed after the PR completed",
			setup: func(t *testing.T, dir string) (string, []string) {
				runGit(t, dir, "checkout", "-b", "feature")
				tip := commit(t, dir, "work")
				runGit(t, dir, "checkout", "main")
				commit(t, dir, "squashed work")
				runGit(t, dir, "checkout", "feature")
				commit(t, dir, "oops, committed on the merged branch")
				runGit(t, dir, "checkout", "main")
				return "feature", []string{tip} // PR still reports the old tip
			},
			want: statePRMergedAhead,
		},
		{
			name: "merged commit no longer available locally",
			setup: func(t *testing.T, dir string) (string, []string) {
				runGit(t, dir, "checkout", "-b", "feature")
				commit(t, dir, "work")
				runGit(t, dir, "checkout", "main")
				commit(t, dir, "squashed work")
				return "feature", []string{"0123456789012345678901234567890123456789"}
			},
			want: statePRMergedUnverified,
		},
		{
			name: "no PR and not merged",
			setup: func(t *testing.T, dir string) (string, []string) {
				runGit(t, dir, "checkout", "-b", "feature")
				commit(t, dir, "work")
				runGit(t, dir, "checkout", "main")
				return "feature", nil
			},
			want: stateUnmerged,
		},
		{
			name: "several PRs, the later one matches",
			setup: func(t *testing.T, dir string) (string, []string) {
				runGit(t, dir, "checkout", "-b", "feature")
				first := commit(t, dir, "work")
				second := commit(t, dir, "follow-up work")
				runGit(t, dir, "checkout", "main")
				commit(t, dir, "squashed work")
				return "feature", []string{first, second}
			},
			want: statePRMerged,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			branch, prTips := tc.setup(t, dir)
			t.Chdir(dir) // classifyBranch shells out to git in the working directory

			if got := classifyBranch(branch, prTips); got.state != tc.want {
				t.Errorf("classifyBranch(%q, %v) = %v (%s), want %v (%s)",
					branch, prTips, got.state, got.state.annotation(), tc.want, tc.want.annotation())
			}
		})
	}
}

// TestPreviewListsCommitsAddedAfterMerge covers the overlay for the case it exists for:
// a branch whose PR completed and that was then committed to. The preview must list the
// commits added since, and nothing that the PR already merged.
func TestPreviewListsCommitsAddedAfterMerge(t *testing.T) {
	dir := newRepo(t)
	runGit(t, dir, "checkout", "-b", "feature")
	commit(t, dir, "merged by the PR")
	prTip := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "main")
	commit(t, dir, "squashed work")
	runGit(t, dir, "checkout", "feature")
	commit(t, dir, "accidental commit after the merge")
	runGit(t, dir, "checkout", "main")
	t.Chdir(dir)

	merge := classifyBranch("feature", []string{prTip})
	if merge.state != statePRMergedAhead {
		t.Fatalf("state = %s, want statePRMergedAhead", merge.state.annotation())
	}
	if merge.base != prTip {
		t.Errorf("base = %s, want the PR tip %s", merge.base, prTip)
	}

	got := previewUnmergedCommits(candidate{name: "feature", merge: merge})
	if !strings.Contains(got, "accidental commit after the merge") {
		t.Errorf("preview omits the unmerged commit:\n%s", got)
	}
	if strings.Contains(got, "merged by the PR") {
		t.Errorf("preview lists an already-merged commit:\n%s", got)
	}
	if !strings.Contains(got, "1 commit(s) added after the PR merged") {
		t.Errorf("preview missing the count header:\n%s", got)
	}
}

// TestPreviewWhenNothingUnmerged checks the overlay says so plainly for safe branches,
// rather than rendering an empty box.
func TestPreviewWhenNothingUnmerged(t *testing.T) {
	dir := newRepo(t)
	runGit(t, dir, "checkout", "-b", "feature")
	commit(t, dir, "work")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "merge", "--ff-only", "feature")
	t.Chdir(dir)

	merge := classifyBranch("feature", nil)
	got := previewUnmergedCommits(candidate{name: "feature", merge: merge})
	if !strings.Contains(got, "discards nothing") {
		t.Errorf("preview should state nothing is lost:\n%s", got)
	}
}

// TestPreCheckOnlyWhenConfirmed guards the property that matters: a branch is never
// pre-selected for deletion unless its merged state was actually confirmed.
func TestPreCheckOnlyWhenConfirmed(t *testing.T) {
	for state, want := range map[mergeState]bool{
		stateGitMerged:          true,
		statePRMerged:           true,
		statePRMergedAhead:      false,
		statePRMergedUnverified: false,
		stateUnmerged:           false,
	} {
		if got := state.safeToPreCheck(); got != want {
			t.Errorf("%s: safeToPreCheck() = %v, want %v", state.annotation(), got, want)
		}
	}
}
