package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var updateDescriptionYes bool

var updateDescriptionCmd = &cobra.Command{
	Use:     "update-description",
	Aliases: []string{"u"},
	Short:   "Update the PR description with commit messages since divergence",
	Long: `Finds the active pull request for the current branch, collects all commit
messages (full body, no merge commits) since the branch diverged from the
PR target branch, and updates the PR description with them as a bullet list.

A preview of the new description is shown before applying. Use --yes to skip
the confirmation prompt.`,
	Example: `  dvo pr update-description
  dvo pr update-description --yes`,
	RunE: runUpdateDescription,
}

func init() {
	prCmd.AddCommand(updateDescriptionCmd)
	updateDescriptionCmd.Flags().BoolVarP(&updateDescriptionYes, "yes", "y", false, "skip confirmation prompt")
}

type prDetail struct {
	PullRequestID int    `json:"pullRequestId"`
	Title         string `json:"title"`
	TargetRefName string `json:"targetRefName"` // e.g. "refs/heads/main"
}

func runUpdateDescription(_ *cobra.Command, _ []string) error {
	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	branch, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	ui.Info.Printf("Current branch: %s\n", branch)

	// ── Step 1: find the active PR ──────────────────────────────────────────
	var prs []prDetail
	spinErr := ui.RunSpinner("Looking up active PR...", func() error {
		out, err := exec.Command("az", "repos", "pr", "list",
			"--source-branch", branch,
			"--status", "active",
			"--org", ctx.OrgURL(),
			"--project", ctx.Project,
			"--repository", ctx.Repo,
			"--query", "[].{pullRequestId:pullRequestId,title:title,targetRefName:targetRefName}",
			"--output", "json",
		).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("az repos pr list failed:\n%s", strings.TrimSpace(string(ee.Stderr)))
			}
			return err
		}
		return json.Unmarshal(out, &prs)
	})
	if spinErr != nil {
		return spinErr
	}

	switch len(prs) {
	case 0:
		return fmt.Errorf("no active pull request found for branch '%s'", branch)
	case 1:
		// fine
	default:
		return fmt.Errorf("multiple active PRs found for branch '%s' — close the extras first", branch)
	}

	pr := prs[0]
	// "refs/heads/main" → "main"
	targetBranch := strings.TrimPrefix(pr.TargetRefName, "refs/heads/")

	ui.Success.Printf("Found PR #%d: %s\n", pr.PullRequestID, pr.Title)
	ui.Info.Printf("Target branch: %s\n", targetBranch)

	// ── Step 2: collect commit messages ────────────────────────────────────
	base, err := git.MergeBase(targetBranch, branch)
	if err != nil {
		return fmt.Errorf("could not find common ancestor between '%s' and '%s': %w", targetBranch, branch, err)
	}

	messages, err := git.CommitMessagesSince(base, branch)
	if err != nil {
		return err
	}
	if messages == "" {
		return fmt.Errorf("no commits found since '%s' diverged from '%s'", branch, targetBranch)
	}

	// ── Step 3: preview and confirm ────────────────────────────────────────
	fmt.Println()
	ui.Info.Println("New description preview:")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(messages)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println()

	if !updateDescriptionYes {
		if !promptYesNo("Update PR description with the above?", true) {
			ui.Info.Println("Aborted.")
			return nil
		}
	}

	// ── Step 4: update the PR ──────────────────────────────────────────────
	spinErr = ui.RunSpinner(fmt.Sprintf("Updating PR #%d...", pr.PullRequestID), func() error {
		// Split messages into lines and pass each as a separate argument after --description
		// (per Azure CLI docs: --description "First Line" "Second Line")
		args := []string{"repos", "pr", "update",
			"--id", fmt.Sprintf("%d", pr.PullRequestID),
			"--org", ctx.OrgURL(),
			"--description",
		}
		args = append(args, strings.Split(messages, "\n")...)
		args = append(args, "--output", "none")

		out, err := exec.Command("az", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("az repos pr update failed:\n%s", strings.TrimSpace(string(out)))
		}
		return nil
	})
	if spinErr != nil {
		return spinErr
	}

	ui.Success.Printf("✓ PR #%d description updated\n", pr.PullRequestID)
	return nil
}
