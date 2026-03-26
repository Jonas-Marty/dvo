package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	copilotWorkItemFlag   string
	copilotModel          string
	copilotAllowAll       bool
	copilotNonInteractive bool
)

var copilotCmd = &cobra.Command{
	Use:   "copilot",
	Short: "Start a Copilot session pre-loaded with the current work item",
	Long: `Extracts the work item ID from the current branch name, fetches the work
item details from Azure DevOps, and launches the Copilot CLI with a prompt
that includes the full work item context.

Branch name must follow the convention:
  fix/<id>-<slug>   feat/<id>-<slug>   task/<id>-<slug>

Use -w to override the work item ID when not on a work-item branch.

By default, opens Copilot in interactive mode (-i) so you can continue
chatting after the initial prompt is executed. Use --non-interactive (-p)
to run in non-interactive mode (exits after completion).`,
	Example: `  adg copilot
  adg copilot -w 1234
  adg copilot --allow-all
  adg copilot --non-interactive`,
	RunE: runCopilot,
}

func init() {
	rootCmd.AddCommand(copilotCmd)
	copilotCmd.Flags().StringVarP(&copilotWorkItemFlag, "work-item", "w", "", "work item ID (default: extracted from branch name)")
	copilotCmd.Flags().StringVarP(&copilotModel, "model", "m", "claude-sonnet-4.6", "model to use (passed as --model to copilot)")
	copilotCmd.Flags().BoolVar(&copilotAllowAll, "allow-all", false, "pass --allow-all to copilot (auto-approve all tool use)")
	copilotCmd.Flags().BoolVarP(&copilotNonInteractive, "non-interactive", "p", false, "run in non-interactive mode (-p), exits after completion")
}

// copilotWI is the az boards work-item show response with all fields we need.
type copilotWI struct {
	ID     int             `json:"id"`
	Fields copilotWIFields `json:"fields"`
}

type copilotWIFields struct {
	Title              string `json:"System.Title"`
	WorkType           string `json:"System.WorkItemType"`
	Description        string `json:"System.Description"`
	AcceptanceCriteria string `json:"Microsoft.VSTS.Common.AcceptanceCriteria"`
	ReproSteps         string `json:"Microsoft.VSTS.TCM.ReproSteps"`
}

// branchWorkItemID tries to extract a numeric work item ID from the current
// branch name. Expects format: <prefix>/<id>-<slug> (e.g. fix/1234-my-feature).
var branchWIRe = regexp.MustCompile(`^(?:fix|feat|task)/(\d+)`)

func extractWorkItemFromBranch(branch string) string {
	if m := branchWIRe.FindStringSubmatch(branch); m != nil {
		return m[1]
	}
	return ""
}

// stripHTML removes HTML tags and decodes common entities.
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var htmlEntityRe = regexp.MustCompile(`&[a-zA-Z]+;|&#\d+;`)

func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = htmlEntityRe.ReplaceAllStringFunc(s, func(e string) string {
		switch e {
		case "&amp;":
			return "&"
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&nbsp;":
			return " "
		case "&quot;":
			return "\""
		default:
			return " "
		}
	})
	// Collapse whitespace
	s = regexp.MustCompile(`[ \t]+`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func runCopilot(_ *cobra.Command, _ []string) error {
	// Resolve work item ID
	wiID := copilotWorkItemFlag
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
					"Or use: adg copilot -w <id>",
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

	ui.Info.Printf("Work item #%s [%s]: %s\n", wiID, wi.Fields.WorkType, wi.Fields.Title)
	fmt.Println()

	// Build copilot command args
	copilotArgs := []string{"--model", copilotModel}
	if copilotNonInteractive {
		copilotArgs = append(copilotArgs, "-p", prompt)
		if copilotAllowAll {
			copilotArgs = append(copilotArgs, "--allow-all")
		}
	} else {
		copilotArgs = append(copilotArgs, "-i", prompt)
		if copilotAllowAll {
			copilotArgs = append(copilotArgs, "--allow-all")
		}
	}

	ui.Info.Printf("Running: copilot %s\n", strings.Join(copilotArgs[:2], " ")) // show model arg only
	fmt.Println()

	cmd := exec.Command("copilot", copilotArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start copilot: %w\nMake sure 'copilot' is installed and in PATH", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("copilot exited with error: %w\nCommand: copilot %s", err, strings.Join(copilotArgs, " "))
	}
	return nil
}

func buildCopilotPrompt(wi copilotWI, branch, repo string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(
		"Implement the following Azure DevOps work item on the current branch.\n\n"+
			"Work Item #%d [%s]: %s\n",
		wi.ID, wi.Fields.WorkType, wi.Fields.Title,
	))

	if desc := stripHTML(wi.Fields.Description); desc != "" {
		sb.WriteString("\nDescription:\n")
		sb.WriteString(desc)
		sb.WriteString("\n")
	}

	if wi.Fields.WorkType == "Bug" {
		if repro := stripHTML(wi.Fields.ReproSteps); repro != "" {
			sb.WriteString("\nRepro Steps:\n")
			sb.WriteString(repro)
			sb.WriteString("\n")
		}
	}

	if ac := stripHTML(wi.Fields.AcceptanceCriteria); ac != "" {
		sb.WriteString("\nAcceptance Criteria:\n")
		sb.WriteString(ac)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf(
		"\nContext:\n"+
			"  Branch:     %s\n"+
			"  Repository: %s\n\n",
		branch, repo,
	))

	sb.WriteString(
		"Analyse the codebase and implement the required changes. " +
			"Follow existing code patterns, naming conventions, and project structure. " +
			"Keep changes minimal and focused on the scope of this work item. " +
			"Cover all acceptance criteria if provided. " +
			"Add unit tests where applicable.",
	)

	return sb.String()
}
