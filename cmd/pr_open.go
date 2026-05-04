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

var openPRCmd = &cobra.Command{
	Use:     "open",
	Aliases: []string{"o"},
	Short:   "Open the active pull request for the current branch",
	Long: `Looks up the active pull request where the current branch is the source,
prints its title and URL, then opens it in your default browser.`,
	Example: `  dvo pr open`,
	RunE:    runOpenPR,
}

func init() {
	prCmd.AddCommand(openPRCmd)
}

type prListEntry struct {
	PullRequestID int    `json:"pullRequestId"`
	Title         string `json:"title"`
}

func runOpenPR(_ *cobra.Command, _ []string) error {
	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	branch, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	// Query active PRs for the current branch as source, with a spinner.
	var prs []prListEntry
	spinErr := ui.RunSpinner(
		fmt.Sprintf("Looking up PR for branch '%s'...", branch),
		func() error {
			out, err := exec.Command("az", "repos", "pr", "list",
				"--source-branch", branch,
				"--status", "active",
				"--org", ctx.OrgURL(),
				"--project", ctx.Project,
				"--repository", ctx.Repo,
				"--query", "[].{pullRequestId:pullRequestId,title:title}",
				"--output", "json",
			).Output()
			if err != nil {
				return fmt.Errorf("az repos pr list failed: %w", err)
			}
			return json.Unmarshal(out, &prs)
		},
	)
	if spinErr != nil {
		return spinErr
	}

	if len(prs) == 0 {
		return fmt.Errorf("no active pull request found for branch '%s'", branch)
	}

	pr := prs[0]
	prURL := ctx.PRURL(pr.PullRequestID)

	ui.Info.Printf("PR #%d: %s\n", pr.PullRequestID, pr.Title)
	ui.Info.Printf("%s\n", prURL)

	if err := ui.OpenBrowser(prURL); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}

	ui.Success.Println("✓ Opened in browser")
	return nil
}

// azJSON runs an az CLI command and unmarshals the JSON output into dst.
// Exported for reuse by other commands in this package.
func azJSON(dst any, args ...string) error {
	args = append(args, "--output", "json")
	out, err := exec.Command("az", args...).Output()
	if err != nil {
		// az writes errors to stderr; include them if available.
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("az %s: %s", strings.Join(args[:2], " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return err
	}
	return json.Unmarshal(out, dst)
}
