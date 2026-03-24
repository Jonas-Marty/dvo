package cmd

import (
	"fmt"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var commitDiffCmd = &cobra.Command{
	Use:   "commit-diff <base>..<compare>  |  commit-diff <base> <compare>",
	Short: "Open Azure DevOps diff viewer for two commits or branches",
	Long: `Resolves both refs to full SHAs and opens the Azure DevOps branchCompare
page in your browser.

Examples:
  adg commit-diff main..develop
  adg commit-diff main develop
  adg commit-diff abc123 def456
  adg commit-diff HEAD~5 HEAD`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runCommitDiff,
}

func init() {
	rootCmd.AddCommand(commitDiffCmd)
}

func runCommitDiff(cmd *cobra.Command, args []string) error {
	var baseRef, compareRef string

	if len(args) == 1 {
		// Must contain ".."  e.g. main..develop
		parts := strings.SplitN(args[0], "..", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("single argument must use the '..' syntax, e.g. main..develop")
		}
		baseRef, compareRef = parts[0], parts[1]
	} else {
		baseRef, compareRef = args[0], args[1]
	}

	baseSHA, err := git.ResolveSHA(baseRef)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", baseRef, err)
	}
	compareSHA, err := git.ResolveSHA(compareRef)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", compareRef, err)
	}

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	diffURL := ctx.CommitDiffURL(baseSHA, compareSHA)

	ui.Info.Printf("Base:    %s (%s)\n", baseRef, baseSHA[:8])
	ui.Info.Printf("Compare: %s (%s)\n", compareRef, compareSHA[:8])
	ui.Success.Println("Opening diff viewer...")

	return ui.OpenBrowser(diffURL)
}
