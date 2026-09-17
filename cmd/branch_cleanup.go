package cmd

import (
	"fmt"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var branchCleanupOffline bool

var branchCleanupCmd = &cobra.Command{
	Use:     "cleanup",
	Aliases: []string{"cu"},
	Short:   "Interactively delete local branches no longer on the remote",
	Long: `Fetches and prunes the remote, then presents a checkbox list of local
branches that no longer exist on origin. Select which ones to delete and
confirm — nothing is deleted without your explicit selection.

By default the Azure DevOps PR API is queried to detect branches that were
squash-merged (where git cannot tell). The commit a completed PR actually merged
is compared against the local branch tip, so commits added after the PR
completed are not mistaken for merged work. Use --offline to skip the API call
and rely on git alone.

Each branch is annotated:

  (merged)                            fully merged into HEAD; deleted with -d
  (PR merged)                         a PR merged exactly this tip
  (PR merged — local commits on top!) merged once, but has moved on since
  (PR merged — could not verify tip)  merged commit no longer available locally
  (unmerged)                          no evidence it was ever merged

Only the first two are pre-checked. The rest can still be selected, and are then
force-deleted.

Press "p" in the picker to preview the highlighted branch — an overlay listing the
commits deleting it would discard.`,
	RunE: runBranchCleanup,
}

func init() {
	branchCmd.AddCommand(branchCleanupCmd)
	branchCleanupCmd.Flags().BoolVar(&branchCleanupOffline, "offline", false, "skip Azure DevOps PR lookup and use git merge check only")
}

// mergeState describes how confident we are that a stale local branch carries no
// work that would be lost by deleting it.
type mergeState int

const (
	// stateUnmerged — no evidence the branch was ever merged.
	stateUnmerged mergeState = iota
	// stateGitMerged — every commit is reachable from HEAD; `git branch -d` accepts it.
	stateGitMerged
	// statePRMerged — a completed PR merged exactly the commits the branch still points at.
	statePRMerged
	// statePRMergedAhead — a completed PR merged this branch, but it has since moved on.
	statePRMergedAhead
	// statePRMergedUnverified — a completed PR merged this branch, but the merged commit
	// is no longer available locally, so the tip cannot be compared.
	statePRMergedUnverified
)

// annotation is the suffix shown after the branch name in the picker.
func (s mergeState) annotation() string {
	switch s {
	case stateGitMerged:
		return "(merged)"
	case statePRMerged:
		return "(PR merged)"
	case statePRMergedAhead:
		return "(PR merged — local commits on top!)"
	case statePRMergedUnverified:
		return "(PR merged — could not verify tip)"
	default:
		return "(unmerged)"
	}
}

// safeToPreCheck reports whether the branch may be pre-selected for deletion.
func (s mergeState) safeToPreCheck() bool {
	return s == stateGitMerged || s == statePRMerged
}

// branchMerge is the verdict on a stale branch: its merge state, plus the revision its
// commits should be measured against when showing what deletion would discard.
type branchMerge struct {
	state mergeState
	// base is the revision to diff the branch against: the commit a completed PR
	// merged, or HEAD when no merged commit applies.
	base string
}

// classifyBranch determines how safely branch can be deleted. prTips are the source-branch
// commits merged by completed PRs whose source ref name matches the branch.
//
// A name match alone is not enough: a branch keeps its name after its PR completes, so
// commits pushed afterwards would otherwise be force-deleted without warning. The local tip
// must actually be contained in one of the merged commits.
func classifyBranch(branch string, prTips []string) branchMerge {
	if git.IsBranchFullyMerged(branch) {
		return branchMerge{state: stateGitMerged, base: "HEAD"}
	}
	comparable := false
	bestBase, bestAhead := "", -1
	for _, tip := range prTips {
		if !git.CommitExists(tip) {
			continue // merged commit was pruned locally — cannot compare against this PR
		}
		comparable = true
		if git.IsAncestor(branch, tip) {
			return branchMerge{state: statePRMerged, base: tip}
		}
		// The branch moved on since this PR. Of the PRs it grew out of, prefer the one
		// leaving the fewest commits unaccounted for — that is the most recent merge.
		if git.IsAncestor(tip, branch) {
			if ahead := git.CountCommitsNotIn(tip, branch); bestAhead < 0 || ahead < bestAhead {
				bestBase, bestAhead = tip, ahead
			}
		}
	}
	switch {
	case comparable:
		if bestBase == "" {
			bestBase = "HEAD" // branch diverged from every merged tip (rebased, amended, …)
		}
		return branchMerge{state: statePRMergedAhead, base: bestBase}
	case len(prTips) > 0:
		return branchMerge{state: statePRMergedUnverified, base: "HEAD"}
	default:
		return branchMerge{state: stateUnmerged, base: "HEAD"}
	}
}

// candidate is one stale local branch offered for deletion.
type candidate struct {
	name  string
	merge branchMerge
	label string
}

// previewMaxCommits caps the overlay height so it stays readable in a small terminal.
const previewMaxCommits = 12

// shortRev abbreviates a full SHA for display, leaving symbolic revs like HEAD alone.
func shortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

// previewUnmergedCommits renders the overlay body for a candidate: the commits it
// carries that its merge state does not account for — exactly what deleting it discards.
func previewUnmergedCommits(c candidate) string {
	var sb strings.Builder
	sb.WriteString(c.name + "\n")
	sb.WriteString(c.merge.state.annotation() + "\n\n")

	switch c.merge.state {
	case stateGitMerged:
		sb.WriteString("Every commit is already reachable from HEAD.\nDeleting this branch discards nothing.")
		return sb.String()
	case statePRMerged:
		sb.WriteString("A completed PR merged exactly this tip (" + shortRev(c.merge.base) + ").\nDeleting this branch discards nothing.")
		return sb.String()
	}

	commits, err := git.CommitsNotIn(c.merge.base, c.name)
	if err != nil {
		sb.WriteString("Could not list commits: " + err.Error())
		return sb.String()
	}
	if len(commits) == 0 {
		sb.WriteString("No commits beyond " + shortRev(c.merge.base) + ".")
		return sb.String()
	}

	switch c.merge.state {
	case statePRMergedAhead:
		sb.WriteString(fmt.Sprintf("%d commit(s) added after the PR merged %s:\n\n", len(commits), shortRev(c.merge.base)))
	default:
		sb.WriteString(fmt.Sprintf("%d commit(s) not reachable from HEAD:\n\n", len(commits)))
	}

	shown := commits
	if len(shown) > previewMaxCommits {
		shown = shown[:previewMaxCommits]
	}
	for _, line := range shown {
		sb.WriteString("  " + line + "\n")
	}
	if len(commits) > len(shown) {
		sb.WriteString(fmt.Sprintf("  … and %d more\n", len(commits)-len(shown)))
	}
	sb.WriteString("\nDeleting the branch drops these refs (the reflog keeps them recoverable\nuntil it expires).")
	return sb.String()
}

func runBranchCleanup(_ *cobra.Command, _ []string) error {
	// 1. Fetch & prune
	if err := ui.RunSpinner("Fetching remote info (prune)…", git.FetchPrune); err != nil {
		ui.Warning.Printf("git fetch --prune failed: %v\n", err)
		// Non-fatal — continue with cached ref info.
	}

	// 2. Optionally query Azure DevOps for completed PRs (detects squash merges)
	var prMergeTips map[string][]string
	if !branchCleanupOffline {
		ctx, ctxErr := devops.FromCurrentRepo()
		if ctxErr != nil {
			ui.Warning.Printf("Could not determine Azure DevOps context: %v — skipping PR check.\n", ctxErr)
		} else {
			var tips map[string][]string
			if err := ui.RunSpinner("Checking completed PRs…", func() error {
				var e error
				tips, e = ctx.CompletedPRMergeTips(500)
				return e
			}); err != nil {
				ui.Warning.Printf("PR lookup failed: %v — skipping PR check.\n", err)
			} else {
				prMergeTips = tips
			}
		}
	}

	// 3. Gather candidates
	currentBranch, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	allBranches, err := git.LocalBranchesInfo()
	if err != nil {
		return err
	}

	var candidates []candidate
	for _, b := range allBranches {
		if b.Name == currentBranch {
			continue
		}
		if git.RemoteBranchExists(b.Name) {
			continue
		}
		merge := classifyBranch(b.Name, prMergeTips[b.Name])

		dateTag := ""
		if b.LastCommit != "" {
			dateTag = "  [" + b.LastCommit + "]"
		}

		label := b.Name + dateTag + "  " + merge.state.annotation()
		candidates = append(candidates, candidate{name: b.Name, merge: merge, label: label})
	}

	if len(candidates) == 0 {
		ui.Success.Println("No stale local branches found. Nothing to clean up.")
		return nil
	}

	// 4. Build picker data — pre-check merged/PR-merged branches, leave unmerged unchecked
	labels := make([]string, len(candidates))
	preChecked := make([]bool, len(candidates))
	for i, c := range candidates {
		labels[i] = c.label
		preChecked[i] = c.merge.state.safeToPreCheck()
	}

	ui.Info.Printf("%d stale local branch(es) found (only branches whose merged state is confirmed are pre-checked):\n\n", len(candidates))

	selectedIdx, err := ui.MultiSelectWithPreview(
		"Select local branches to delete (only deletes local copies):",
		labels, preChecked,
		func(i int) string { return previewUnmergedCommits(candidates[i]) },
	)
	if err != nil {
		return err
	}
	if selectedIdx == nil {
		ui.Info.Println("Cancelled. No local branches deleted.")
		return nil
	}
	if len(selectedIdx) == 0 {
		ui.Info.Println("Nothing selected. No local branches deleted.")
		return nil
	}

	// 5. Delete selected branches
	fmt.Println()
	deleted, skipped := 0, 0
	for _, idx := range selectedIdx {
		c := candidates[idx]
		if err := git.DeleteBranch(c.name, c.merge.state != stateGitMerged); err != nil {
			ui.Error.Printf("Failed to delete %q: %v\n", c.name, err)
			skipped++
		} else {
			ui.Success.Printf("Deleted %q\n", c.name)
			deleted++
		}
	}

	// 6. Summary
	fmt.Println()
	ui.Info.Printf("Done — %d deleted, %d skipped.\n", deleted, skipped)
	return nil
}
