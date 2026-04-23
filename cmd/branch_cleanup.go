package cmd

import (
	"fmt"

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
squash-merged (where git cannot tell). Those are shown as "(PR merged)" and
pre-checked. Use --offline to skip the API call and rely on git alone.

Branches that are not merged by any means are annotated with "(unmerged)" and
are not pre-checked. Selecting them will force-delete them.`,
	RunE: runBranchCleanup,
}

func init() {
	branchCmd.AddCommand(branchCleanupCmd)
	branchCleanupCmd.Flags().BoolVar(&branchCleanupOffline, "offline", false, "skip Azure DevOps PR lookup and use git merge check only")
}

func runBranchCleanup(_ *cobra.Command, _ []string) error {
	// 1. Fetch & prune
	if err := ui.RunSpinner("Fetching remote info (prune)…", git.FetchPrune); err != nil {
		ui.Warning.Printf("git fetch --prune failed: %v\n", err)
		// Non-fatal — continue with cached ref info.
	}

	// 2. Optionally query Azure DevOps for completed PRs (detects squash merges)
	var prMergedBranches map[string]bool
	if !branchCleanupOffline {
		ctx, ctxErr := devops.FromCurrentRepo()
		if ctxErr != nil {
			ui.Warning.Printf("Could not determine Azure DevOps context: %v — skipping PR check.\n", ctxErr)
		} else {
			var branches map[string]bool
			if err := ui.RunSpinner("Checking completed PRs…", func() error {
				var e error
				branches, e = ctx.CompletedPRBranches(500)
				return e
			}); err != nil {
				ui.Warning.Printf("PR lookup failed: %v — skipping PR check.\n", err)
			} else {
				prMergedBranches = branches
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

	type candidate struct {
		name      string
		merged    bool // controls pre-check in the picker
		gitMerged bool // true when git -d is safe (no force needed)
		label     string
	}

	var candidates []candidate
	for _, b := range allBranches {
		if b.Name == currentBranch {
			continue
		}
		if git.RemoteBranchExists(b.Name) {
			continue
		}
		gitMerged := git.IsBranchFullyMerged(b.Name)
		prMerged := prMergedBranches[b.Name]
		merged := gitMerged || prMerged

		dateTag := ""
		if b.LastCommit != "" {
			dateTag = "  [" + b.LastCommit + "]"
		}

		var label string
		switch {
		case gitMerged:
			label = b.Name + dateTag + "  (merged)"
		case prMerged:
			label = b.Name + dateTag + "  (PR merged)"
		default:
			label = b.Name + dateTag + "  (unmerged)"
		}

		candidates = append(candidates, candidate{name: b.Name, merged: merged, gitMerged: gitMerged, label: label})
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
		preChecked[i] = c.merged
	}

	ui.Info.Printf("%d stale local branch(es) found (merged/PR merged=pre-checked, unmerged=unchecked):\n\n", len(candidates))

	selectedIdx, err := ui.MultiSelect("Select local branches to delete (only deletes local copies):", labels, preChecked)
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
		if err := git.DeleteBranch(c.name, !c.gitMerged); err != nil {
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
