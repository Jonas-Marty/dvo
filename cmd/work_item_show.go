package cmd

import (
	"fmt"
	"strconv"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var workItemShowCmd = &cobra.Command{
	Use:   "work-item-show <id>",
	Short: "Open a work item in the browser",
	Long:  `Opens the Azure DevOps work item edit view in your default browser.`,
	Example: `  adg work-item-show 12345`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkItemShow,
}

func init() {
	rootCmd.AddCommand(workItemShowCmd)
}

func runWorkItemShow(_ *cobra.Command, args []string) error {
	id, err := parseWorkItemID(args[0])
	if err != nil {
		return err
	}

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	url := ctx.WorkItemURL(id)
	ui.Info.Printf("Opening work item #%s\n", id)
	ui.Info.Printf("%s\n", url)

	if err := ui.OpenBrowser(url); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}

	ui.Success.Println("✓ Opened in browser")
	return nil
}

// parseWorkItemID validates and returns the work item ID string.
func parseWorkItemID(s string) (string, error) {
	if _, err := strconv.Atoi(s); err != nil {
		return "", fmt.Errorf("invalid work item ID %q: must be a number", s)
	}
	return s, nil
}
