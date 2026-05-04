package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var branchCreateWorkItem string

var branchCreateCmd = &cobra.Command{
	Use:     "create [<commitish>]",
	Aliases: []string{"c"},
	Short:   "Create a branch from a work item",
	Long: `Fetches the work item title and type from Azure DevOps and creates a local
branch with a name derived from the work item:

  Bug          →  fix/<slug>-<id>
  User Story / Feature →  feat/<slug>-<id>
  anything else        →  task/<slug>-<id>

The slug is the first 30 characters of the title, lowercase, with runs of
non-alphanumeric characters collapsed to a single dash and trailing dashes
removed.

The branch is created from the current HEAD, or from <commitish> if provided.
Errors out if the branch already exists.`,
	Example: `  dvo branch create -w 1234
  dvo b c -w 1234
  dvo branch create -w 1234 main
  dvo branch create -w 1234 v2.3.0`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Offer all local + remote branches (git branch -a, strip leading "  " / "* " / "remotes/")
		out, err := exec.Command("git", "branch", "-a", "--format=%(refname:short)").Output()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var refs []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			refs = append(refs, line)
		}
		// Also add tags
		tagOut, err := exec.Command("git", "tag").Output()
		if err == nil {
			for _, t := range strings.Split(strings.TrimSpace(string(tagOut)), "\n") {
				t = strings.TrimSpace(t)
				if t != "" {
					refs = append(refs, t)
				}
			}
		}
		return refs, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: runBranchCreate,
}

func init() {
	branchCmd.AddCommand(branchCreateCmd)
	branchCreateCmd.Flags().StringVarP(&branchCreateWorkItem, "work-item", "w", "", "work item ID (required)")
	_ = branchCreateCmd.MarkFlagRequired("work-item")
}

// workItemDetail is the az boards work-item show response.
type workItemDetail struct {
	ID     int                  `json:"id"`
	Fields workItemDetailFields `json:"fields"`
}

type workItemDetailFields struct {
	Title    string `json:"System.Title"`
	WorkType string `json:"System.WorkItemType"`
}

func runBranchCreate(_ *cobra.Command, args []string) error {
	id, err := parseWorkItemID(branchCreateWorkItem)
	if err != nil {
		return err
	}

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	// Fetch the work item.
	var wi workItemDetail
	if err := ui.RunSpinner(fmt.Sprintf("Fetching work item #%s…", id), func() error {
		out, err := exec.Command("az", "boards", "work-item", "show",
			"--id", id,
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

	prefix := workItemPrefix(wi.Fields.WorkType)
	slug := titleSlug(wi.Fields.Title, 50)
	branchName := fmt.Sprintf("%s/%s-%s", prefix, slug, id)

	// Check branch doesn't already exist.
	existing, _ := git.LocalBranches()
	for _, b := range existing {
		if b == branchName {
			return fmt.Errorf("branch %q already exists", branchName)
		}
	}

	ui.Info.Printf("Work item #%s: %s\n", id, wi.Fields.Title)
	ui.Info.Printf("Type:   %s  →  prefix: %s/\n", wi.Fields.WorkType, prefix)
	ui.Success.Printf("Branch: %s\n", branchName)
	fmt.Println()

	// Build git checkout -b command.
	gitArgs := []string{"checkout", "-b", branchName}
	if len(args) == 1 {
		gitArgs = append(gitArgs, args[0])
	}

	return git.RunInteractive(gitArgs...)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// titleSlug converts a title to a URL-safe lowercase slug of at most maxLen chars.
func titleSlug(title string, maxLen int) string {
	s := strings.ToLower(title)
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// workItemPrefix maps work item types to branch prefixes.
func workItemPrefix(workType string) string {
	switch strings.ToLower(workType) {
	case "bug":
		return "fix"
	case "user story", "feature":
		return "feat"
	default:
		return "task"
	}
}
