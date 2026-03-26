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

var copilotPromptWorkItemFlag string

var copilotPromptCmd = &cobra.Command{
	Use:     "prompt",
	Aliases: []string{"p"},
	Short:   "Print the Copilot prompt for the current work item and copy it to the clipboard",
	Example: `  adg copilot prompt
  adg copilot prompt -w 1234`,
	RunE: runCopilotPrompt,
}

func init() {
	copilotCmd.AddCommand(copilotPromptCmd)
	copilotPromptCmd.Flags().StringVarP(&copilotPromptWorkItemFlag, "work-item", "w", "", "work item ID (default: extracted from branch name)")
}

func runCopilotPrompt(_ *cobra.Command, _ []string) error {
	// Resolve work item ID
	wiID := copilotPromptWorkItemFlag
	if wiID == "" {
		branch, err := git.GetCurrentBranch()
		if err != nil {
			return err
		}
		wiID = extractWorkItemFromBranch(branch)
		if wiID == "" {
			return fmt.Errorf(
				"could not extract work item ID from branch %q\n"+
					"Branch must follow the convention: fix/<id>-<slug> / feat/<id>-<slug> / task/<id>-<slug>\n"+
					"Or use: adg copilot prompt -w <id>",
				branch,
			)
		}
		ui.Info.Printf("Detected work item #%s from branch\n", wiID)
	}

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	branch, _ := git.GetCurrentBranch()

	// Fetch work item
	var wi copilotWI
	if err := ui.RunSpinner(fmt.Sprintf("Fetching work item #%s…", wiID), func() error {
		out, err := exec.Command("az", "boards", "work-item", "show",
			"--id", wiID,
			"--org", ctx.OrgURL(),
			"--output", "json",
		).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("az boards work-item show failed:\n%s", string(ee.Stderr))
			}
			return err
		}
		return json.Unmarshal(out, &wi)
	}); err != nil {
		return err
	}

	prompt := buildCopilotPrompt(wi, branch, ctx.Repo)

	// Print the prompt
	fmt.Println(prompt)
	fmt.Println()

	// Copy to clipboard via clip.exe (reads from stdin)
	clip := exec.Command("clip.exe")
	clip.Stdin = strings.NewReader(prompt)
	if err := clip.Run(); err != nil {
		ui.Warning.Printf("Could not copy to clipboard: %v\n", err)
		return nil
	}

	ui.Success.Println("✓ Prompt copied to clipboard!")
	return nil
}
