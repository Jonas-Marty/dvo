package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func GetRemoteURL() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL (are you in a git repo with an 'origin' remote?)")
	}
	return strings.TrimSpace(string(out)), nil
}

func GetCurrentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

func GetDefaultBranch() (string, error) {
	out, err := exec.Command("git", "remote", "show", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("failed to query remote info")
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "HEAD branch:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "HEAD branch:")), nil
		}
	}
	return "", fmt.Errorf("could not determine default branch")
}

func GetFirstRemote() (string, error) {
	out, err := exec.Command("git", "remote").Output()
	if err != nil {
		return "", fmt.Errorf("failed to list remotes")
	}
	remotes := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(remotes) == 0 || remotes[0] == "" {
		return "", fmt.Errorf("no remotes configured")
	}
	return remotes[0], nil
}

func ResolveSHA(ref string) (string, error) {
	out, err := exec.Command("git", "rev-parse", ref).Output()
	if err != nil {
		return "", fmt.Errorf("could not resolve ref %q", ref)
	}
	return strings.TrimSpace(string(out)), nil
}

func MergeBase(a, b string) (string, error) {
	out, err := exec.Command("git", "merge-base", a, b).Output()
	if err != nil {
		return "", fmt.Errorf("could not find common ancestor between %q and %q", a, b)
	}
	return strings.TrimSpace(string(out)), nil
}

// CommitMessagesSince returns all commit messages (full body) since base..head,
// formatted as a bullet list. Merge commits are excluded.
func CommitMessagesSince(base, head string) (string, error) {
	out, err := exec.Command("git", "log", "--pretty=format:- %B", "--no-merges", "--reverse", base+".."+head).Output()
	if err != nil {
		return "", fmt.Errorf("failed to read commits")
	}
	return strings.TrimSpace(string(out)), nil
}

// LocalBranches returns all local branch names.
func LocalBranches() ([]string, error) {
	out, err := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list local branches")
	}
	var branches []string
	for _, b := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if b != "" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// RemoteBranchExists checks whether origin/<branch> exists locally (after fetch).
func RemoteBranchExists(branch string) bool {
	err := exec.Command("git", "rev-parse", "--verify", "origin/"+branch).Run()
	return err == nil
}

// FetchPrune runs git fetch --prune.
func FetchPrune() error {
	return exec.Command("git", "fetch", "--prune").Run()
}

// DeleteBranch deletes a branch. force=true uses -D (force), false uses -d (safe).
func DeleteBranch(branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	out, err := exec.Command("git", "branch", flag, branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// IsBranchFullyMerged returns true if the branch can be safely deleted (no unmerged commits).
func IsBranchFullyMerged(branch string) bool {
	err := exec.Command("git", "branch", "-d", "--dry-run", branch).Run()
	return err == nil
}

// RunInteractive runs git with the given args, inheriting the caller's
// stdout, stderr, and stdin so output is shown directly in the terminal.
func RunInteractive(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
