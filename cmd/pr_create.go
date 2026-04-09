package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/Jonas-Marty/ad-cli/internal/cache"
	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	createPRTarget            string
	createPRReviewers         []string
	createPROptionalReviewers []string
	createPRWorkItems         []string
	createPRNoBrowser         bool
)

var createPRCmd = &cobra.Command{
	Use:     "create <title>",
	Aliases: []string{"c"},
	Short:   "Create a pull request for the current branch",
	Long: `Creates a pull request from the current branch to the target branch
(defaults to the repository's default branch).

The PR is opened in your browser after creation. Source branch deletion
on merge is enabled by default.`,
	Example: `  adg pr create "Add login feature"
  adg pr create "Fix parser bug" --target develop
  adg pr create "Update docs" -r alice@example.com -r bob@example.com
  adg pr create "Update docs" -r alice@example.com -o bob@example.com
  adg pr create "My feature" -w 111 -w 222
  adg pr create "Silent create" --no-browser`,
	Args: cobra.ExactArgs(1),
	RunE: runCreatePR,
}

func init() {
	prCmd.AddCommand(createPRCmd)
	createPRCmd.Flags().StringVarP(&createPRTarget, "target", "t", "", "target branch (default: repo default branch)")
	createPRCmd.Flags().StringArrayVarP(&createPRReviewers, "reviewer", "r", nil, "required reviewer (email/alias); repeatable")
	createPRCmd.Flags().StringArrayVarP(&createPROptionalReviewers, "optional-reviewer", "o", nil, "optional reviewer (email/alias); repeatable")
	createPRCmd.Flags().StringArrayVarP(&createPRWorkItems, "work-item", "w", nil, "linked work item ID; repeatable")
	createPRCmd.Flags().BoolVar(&createPRNoBrowser, "no-browser", false, "skip opening the PR in browser after creation")

	// Tab-complete reviewer aliases from the local cache.
	_ = createPRCmd.RegisterFlagCompletionFunc("reviewer", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		ctx, err := devops.FromCurrentRepo()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return cache.Aliases(ctx.Org), cobra.ShellCompDirectiveNoFileComp
	})
	_ = createPRCmd.RegisterFlagCompletionFunc("optional-reviewer", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		ctx, err := devops.FromCurrentRepo()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return cache.Aliases(ctx.Org), cobra.ShellCompDirectiveNoFileComp
	})
}

type prCreateResult struct {
	PullRequestID int    `json:"pullRequestId"`
	Title         string `json:"title"`
}

func runCreatePR(cmd *cobra.Command, args []string) error {
	title := args[0]

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	branch, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	// Ensure the branch is on the remote before creating a PR.
	remote, err := git.GetFirstRemote()
	if err != nil {
		return err
	}
	exists, err := git.BranchExistsOnRemote(remote, branch)
	if err != nil {
		return err
	}
	if !exists {
		ui.Info.Printf("Branch %q not found on remote — pushing first...\n", branch)
		label := fmt.Sprintf("Pushing %s → %s", branch, remote)
		if err := ui.RunStreaming(label, git.PushBranchCmd(remote, branch)); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
		fmt.Println()
	}

	// Resolve target branch.
	target := createPRTarget
	if target == "" {
		ui.Info.Println("Detecting default branch...")
		target, err = git.GetDefaultBranch()
		if err != nil {
			return fmt.Errorf("could not determine target branch: %w\nUse --target to specify one", err)
		}
	}

	ui.Info.Printf("Creating PR: '%s'\n", title)
	ui.Info.Printf("  %s → %s\n", branch, target)
	if len(createPRReviewers) > 0 {
		ui.Info.Printf("  Reviewers: %v\n", createPRReviewers)
	}
	if len(createPROptionalReviewers) > 0 {
		ui.Info.Printf("  Optional reviewers: %v\n", createPROptionalReviewers)
	}
	if len(createPRWorkItems) > 0 {
		ui.Info.Printf("  Work items: %v\n", createPRWorkItems)
	}
	fmt.Println()

	// Resolve reviewer aliases → full emails.
	resolvedReviewers := make([]string, 0, len(createPRReviewers))
	for _, r := range createPRReviewers {
		email, err := cache.ResolveReviewer(ctx.Org, r)
		if err != nil {
			return err
		}
		resolvedReviewers = append(resolvedReviewers, email)
	}

	resolvedOptionalReviewers := make([]string, 0, len(createPROptionalReviewers))
	for _, r := range createPROptionalReviewers {
		email, err := cache.ResolveReviewer(ctx.Org, r)
		if err != nil {
			return err
		}
		resolvedOptionalReviewers = append(resolvedOptionalReviewers, email)
	}

	// Build az command args.
	azArgs := []string{
		"repos", "pr", "create",
		"--source-branch", branch,
		"--target-branch", target,
		"--title", title,
		"--delete-source-branch", "true",
		"--org", ctx.OrgURL(),
		"--project", ctx.Project,
		"--repository", ctx.Repo,
		"--output", "json",
	}
	for _, r := range resolvedReviewers {
		azArgs = append(azArgs, "--required-reviewers", r)
	}
	for _, r := range resolvedOptionalReviewers {
		azArgs = append(azArgs, "--optional-reviewers", r)
	}
	for _, w := range createPRWorkItems {
		// Validate that work item IDs are numeric before sending to az.
		if _, err := strconv.Atoi(w); err != nil {
			return fmt.Errorf("invalid work item ID %q: must be a number", w)
		}
		azArgs = append(azArgs, "--work-items", w)
	}

	var result prCreateResult
	spinErr := ui.RunSpinner("Creating pull request...", func() error {
		out, err := exec.Command("az", azArgs...).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("az repos pr create failed:\n%s", string(ee.Stderr))
			}
			return err
		}
		return json.Unmarshal(out, &result)
	})
	if spinErr != nil {
		return spinErr
	}

	prURL := ctx.PRURL(result.PullRequestID)
	ui.Success.Printf("✓ Created PR #%d: %s\n", result.PullRequestID, result.Title)
	ui.Info.Printf("  %s\n", prURL)

	if !createPRNoBrowser {
		fmt.Println()
		if err := ui.OpenBrowser(prURL); err != nil {
			ui.Warning.Printf("Could not open browser: %v\n", err)
		}
	}

	return nil
}
