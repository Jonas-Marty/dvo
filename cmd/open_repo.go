package cmd

import (
	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var openRepoCmd = &cobra.Command{
	Use:   "open-repo",
	Short: "Open the current repository in Azure DevOps",
	Long: `Extracts the organization, project, and repository name from the
current Git remote URL and opens the repository page in your default browser.`,
	Example: `  adg open-repo`,
	RunE:    runOpenRepo,
}

func init() {
	rootCmd.AddCommand(openRepoCmd)
}

func runOpenRepo(_ *cobra.Command, _ []string) error {
	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	url := ctx.RepoURL()
	ui.Info.Printf("Opening %s\n", url)

	if err := ui.OpenBrowser(url); err != nil {
		return err
	}

	ui.Success.Println("✓ Opened in browser")
	return nil
}
