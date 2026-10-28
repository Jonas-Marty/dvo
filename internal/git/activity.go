package git

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Field and record separators used in the log formats below. Commit messages
// are free text, so a newline cannot delimit records.
const (
	unitSep   = "\x1f"
	recordSep = "\x1e"
)

// LogCommit is one commit returned by CommitLog.
type LogCommit struct {
	AuthorDate time.Time
	CommitDate time.Time
	Message    string
}

// CommitLog returns the commits of every ref in the repository at dir whose
// author matches author (a case-insensitive pattern, as for git log --author)
// and that were committed at or after since.
//
// There is deliberately no upper bound: an amend or rebase moves the committer
// date forward, and such a commit still belongs to the day it was authored.
// Callers filter on the dates they care about.
func CommitLog(ctx context.Context, dir, author string, since time.Time) ([]LogCommit, error) {
	args := []string{"-C", dir, "log", "--all", "-i",
		"--since=" + since.Format(time.RFC3339),
		"--format=%aI%x1f%cI%x1f%B%x1e",
	}
	if author != "" {
		args = append(args, "--author="+author)
	}
	out, err := runGit(ctx, args...)
	if err != nil {
		return nil, err
	}
	var commits []LogCommit
	for _, rec := range strings.Split(out, recordSep) {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, unitSep, 3)
		if len(f) != 3 {
			continue
		}
		a, errA := time.Parse(time.RFC3339, f[0])
		c, errC := time.Parse(time.RFC3339, f[1])
		if errA != nil || errC != nil {
			continue
		}
		commits = append(commits, LogCommit{AuthorDate: a, CommitDate: c, Message: f[2]})
	}
	return commits, nil
}

// ReflogEntry is one line of the HEAD reflog.
type ReflogEntry struct {
	Time    time.Time
	Subject string // e.g. "checkout: moving from main to feature/123-x"
}

var reflogSelectorRe = regexp.MustCompile(`@\{(.+)\}$`)

// HeadReflog returns the HEAD reflog of the repository at dir, newest first.
func HeadReflog(ctx context.Context, dir string) ([]ReflogEntry, error) {
	out, err := runGit(ctx, "-C", dir, "log", "-g", "--date=iso-strict",
		"--format=%gd%x1f%gs")
	if err != nil {
		return nil, err
	}
	var entries []ReflogEntry
	for _, line := range strings.Split(out, "\n") {
		sel, subject, ok := strings.Cut(strings.TrimSpace(line), unitSep)
		if !ok {
			continue
		}
		m := reflogSelectorRe.FindStringSubmatch(sel)
		if m == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, m[1])
		if err != nil {
			continue
		}
		entries = append(entries, ReflogEntry{Time: t, Subject: subject})
	}
	return entries, nil
}

// runGit runs git and returns stdout. On failure the error carries git's
// stderr so a bad repo path is easy to diagnose.
func runGit(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", args[2], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[2], err)
	}
	return string(out), nil
}
