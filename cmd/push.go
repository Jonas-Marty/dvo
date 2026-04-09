package cmd

import (
	"fmt"

	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:     "push",
	Aliases: []string{"p"},
	Short:   "Push current branch and set upstream",
	Long: `Pushes the current branch to the first configured remote (usually 'origin')
and sets the upstream tracking branch. Equivalent to:
  git push -u <remote> <current-branch>`,
	RunE: runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)
}

func runPush(_ *cobra.Command, _ []string) error {
	branch, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	remote, err := git.GetFirstRemote()
	if err != nil {
		return err
	}

	ui.Info.Printf("Pushing %s → %s\n", branch, remote)

	label := fmt.Sprintf("Pushing %s → %s", branch, remote)
	if err := ui.RunStreaming(label, git.PushBranchCmd(remote, branch)); err != nil {
		ui.Error.Printf("Push failed: %v\n", err)
		return err
	}

	ui.Success.Println("✓ Pushed successfully")
	return nil
}
