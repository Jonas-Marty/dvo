package cmd

import (
	"fmt"

	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var branchCleanupCmd = &cobra.Command{
	Use:   "branch-cleanup",
	Short: "Interactively delete local branches no longer on the remote",
	Long: `Fetches and prunes the remote, then presents a checkbox list of local
branches that no longer exist on origin. Select which ones to delete and
confirm — nothing is deleted without your explicit selection.

Branches that are not fully merged are annotated with "(unmerged)" and
are not pre-checked. Selecting them will force-delete them.`,
	RunE: runBranchCleanup,
}

func init() {
	rootCmd.AddCommand(branchCleanupCmd)
}

func runBranchCleanup(_ *cobra.Command, _ []string) error {
	// 1. Fetch & prune
	if err := ui.RunSpinner("Fetching remote info (prune)…", git.FetchPrune); err != nil {
		ui.Warning.Printf("git fetch --prune failed: %v\n", err)
		// Non-fatal — continue with cached ref info.
	}

	// 2. Gather candidates
	currentBranch, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	allBranches, err := git.LocalBranches()
	if err != nil {
		return err
	}

	type candidate struct {
		name     string
		merged   bool
		label    string
	}

	var candidates []candidate
	for _, b := range allBranches {
		if b == currentBranch {
			continue
		}
		if git.RemoteBranchExists(b) {
			continue
		}
		merged := git.IsBranchFullyMerged(b)
		label := b
		if !merged {
			label = b + "  (unmerged)"
		}
		candidates = append(candidates, candidate{name: b, merged: merged, label: label})
	}

	if len(candidates) == 0 {
		ui.Success.Println("No stale local branches found. Nothing to clean up.")
		return nil
	}

	// 3. Build picker data — pre-check merged branches, leave unmerged unchecked
	labels := make([]string, len(candidates))
	preChecked := make([]bool, len(candidates))
	for i, c := range candidates {
		labels[i] = c.label
		preChecked[i] = c.merged
	}

	ui.Info.Printf("%d stale branch(es) found (merged=pre-checked, unmerged=unchecked):\n\n", len(candidates))

	selectedIdx, err := ui.MultiSelect("Select branches to delete:", labels, preChecked)
	if err != nil {
		return err
	}
	if selectedIdx == nil {
		ui.Info.Println("Cancelled. No branches deleted.")
		return nil
	}
	if len(selectedIdx) == 0 {
		ui.Info.Println("Nothing selected. No branches deleted.")
		return nil
	}

	// 4. Delete selected branches
	fmt.Println()
	deleted, skipped := 0, 0
	for _, idx := range selectedIdx {
		c := candidates[idx]
		if err := git.DeleteBranch(c.name, !c.merged); err != nil {
			ui.Error.Printf("Failed to delete %q: %v\n", c.name, err)
			skipped++
		} else {
			ui.Success.Printf("Deleted %q\n", c.name)
			deleted++
		}
	}

	// 5. Summary
	fmt.Println()
	ui.Info.Printf("Done — %d deleted, %d skipped.\n", deleted, skipped)
	return nil
}
