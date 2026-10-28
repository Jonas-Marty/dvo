package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jonas-Marty/ad-cli/internal/adopat"
	"github.com/Jonas-Marty/ad-cli/internal/cache"
	"github.com/Jonas-Marty/ad-cli/internal/config"
	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/workday"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var workItemActivityCmd = &cobra.Command{
	Use:     "activity [date]",
	Aliases: []string{"a"},
	Short:   "List the work items you potentially worked on during a day",
	Long: `Lists every work item you may have worked on during one day, for filling in
an hour report. The list is a deliberate superset: it is better to show too
many items than too few, so skip the irrelevant ones yourself.

Every item shows why it was included and the first and last time of activity.
The day runs from local midnight to the next local midnight. The day is given
as YYYY-MM-DD, today or yesterday, either as an argument or with --date.

Signals, each adding items together with a reason:
  - edits you made (field changes, comments, state changes, links, attachments)
  - the "My activity" list of items you viewed or edited (best run the same day)
  - pull requests you created, completed, abandoned, commented on or voted on
  - pushes you made: branch names, their pull requests, commit messages
  - local repositories (--repo): your commits and the branches you checked out
  - items assigned to you that are still open, as a last resort

IDs are read from "#123" / "AB#123" in texts and from numbers in branch names.
Numbers that are not work items are dropped.`,
	Example: `  dvo workitem activity
  dvo w a yesterday
  dvo workitem a 2026-10-20 -o hours.csv
  dvo workitem a -p Alpha -p Beta --repo /d/Git/alpha --repo /d/Git/beta`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeDay,
	RunE:              runWorkItemActivity,
}

func init() {
	f := workItemActivityCmd.Flags()
	f.String("org", "", "Azure DevOps organization, name or URL (default: from the current repo)")
	f.String("tenant", "", "Entra tenant ID to sign in with (default: discovered from the organization)")
	f.StringSliceP("project", "p", nil, "project to search, repeatable (default: from the current repo)")
	f.StringP("date", "d", "today", "day to report: YYYY-MM-DD, today or yesterday (local time)")
	f.StringSliceP("repo", "r", nil, "local git repository to scan, repeatable")
	f.String("author", "", "git author to look for in local repos (default: your Azure DevOps e-mail)")
	f.StringSlice("closed-states", workday.DefaultClosedStates, "states that do not count as open for the assigned-items signal")
	f.StringP("output", "o", "", "also write the result to this CSV file")
	f.Bool("plain", false, "print a plain table instead of the interactive list")
	f.String("separator", ";", `CSV field separator ("tab" for a tab)`)
	workitemCmd.AddCommand(workItemActivityCmd)
}

// completeDay suggests the day keywords for the positional date.
func completeDay(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return []string{"today", "yesterday"}, cobra.ShellCompDirectiveNoFileComp
}

func runWorkItemActivity(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	org, _ := f.GetString("org")
	projects, _ := f.GetStringSlice("project")
	dateStr, _ := f.GetString("date")
	repos, _ := f.GetStringSlice("repo")
	author, _ := f.GetString("author")
	closed, _ := f.GetStringSlice("closed-states")
	output, _ := f.GetString("output")
	sepStr, _ := f.GetString("separator")
	tenant, _ := f.GetString("tenant")
	plain, _ := f.GetBool("plain")

	if len(args) == 1 {
		if f.Changed("date") {
			return fmt.Errorf("give the day either as an argument or with --date, not both")
		}
		dateStr = args[0]
	}

	sep, err := parseSeparator(sepStr)
	if err != nil {
		return err
	}
	day, err := workday.ParseDay(dateStr, time.Now())
	if err != nil {
		return err
	}

	// The repo supplies whatever was not given on the command line, but is not
	// required when both are.
	if org == "" || len(projects) == 0 {
		ctx, err := devops.FromCurrentRepo()
		if err != nil {
			return fmt.Errorf("%w\nPass --org and --project to run outside an Azure DevOps repo", err)
		}
		if org == "" {
			org = ctx.Org
		}
		if len(projects) == 0 {
			projects = []string{ctx.Project}
		}
	}
	client := devops.NewClient("https://dev.azure.com/" + orgName(org))
	switch {
	case tenant != "":
		client.Tenant = tenant
	default:
		// Remembered from an earlier run; re-detected if the org rejects it.
		client.Tenant = cache.LoadTenant(orgName(org))
		client.TenantIsHint = client.Tenant != ""
	}
	client.OnTenant = func(t string) { _ = cache.SaveTenant(orgName(org), t) }
	// Fallback only: used when the Entra token is rejected.
	if pat, err := adopat.Lookup(orgName(org)); err == nil {
		client.PAT = pat
	}

	var report *workday.Report
	err = ui.RunSpinnerProgress(cmd.Context(), "Signing in...", func(c context.Context, p *ui.Progress) error {
		me, err := client.CurrentUser(c)
		if err != nil {
			return err
		}
		if author == "" {
			author = me.UniqueName
		}
		finder := workday.NewFinder(client, workday.Options{
			Projects:     projects,
			Day:          day,
			Me:           me,
			RepoPaths:    repos,
			Author:       author,
			ClosedStates: closed,
		}, p)
		if err := finder.Collect(c); err != nil {
			return err
		}
		report, err = workday.BuildReport(c, client, finder.Collector(), day.Loc(), p)
		return err
	})
	if err != nil {
		return err
	}

	title := fmt.Sprintf("Work items you potentially worked on, %s (%d)",
		day.Start.Format("Mon 2006-01-02"), len(report.Rows))
	if report.Dropped > 0 {
		defer ui.Info.Printf("Dropped %d candidate IDs that are not work items.\n", report.Dropped)
	}

	// The CSV is written first so it is not lost if the list is cancelled.
	if output != "" {
		path := config.NormalizePath(output)
		if err := writeCSVFile(path, report.Rows, sep); err != nil {
			return err
		}
		defer ui.Success.Printf("✓ Wrote %s\n", path)
	}

	if plain || len(report.Rows) == 0 || !ui.IsInteractive() {
		ui.Info.Println(title)
		if len(report.Rows) > 0 {
			workday.WriteTable(cmd.OutOrStdout(), report.Rows)
		}
		return nil
	}

	copied := ""
	err = ui.SelectList(title, listItems(report.Rows), func(sel []int) (string, error) {
		text := formatIDs(report.Rows, sel)
		if err := ui.CopyToClipboard(text); err != nil {
			return "", err
		}
		copied = text
		return fmt.Sprintf("copied %d: %s", len(sel), text), nil
	})
	if err != nil {
		return err
	}
	if copied != "" {
		ui.Success.Printf("✓ Copied to clipboard: %s\n", copied)
	}
	return nil
}

// listItems lays the rows out for the interactive list: a headline per item,
// the time span and parent below it, then one line per reason.
func listItems(rows []workday.Row) []ui.ListItem {
	items := make([]ui.ListItem, len(rows))
	for i, r := range rows {
		f := r.Fields()
		title := fmt.Sprintf("#%d  %s  %s", r.ID, r.Type, f[2])
		if r.State != "" {
			title += "  [" + r.State + "]"
		}

		when := "no activity time"
		if !r.First.IsZero() {
			when = f[6] + "–" + f[7]
			if f[6] == f[7] {
				when = f[6]
			}
		}
		meta := when
		if f[5] != "" {
			meta += "  ·  parent: " + f[5]
		}

		details := []string{meta}
		for _, reason := range r.Reasons {
			details = append(details, "• "+reason)
		}
		items[i] = ui.ListItem{Title: title, Details: details}
	}
	return items
}

// formatIDs renders the selected rows as "#123;#223;#323".
func formatIDs(rows []workday.Row, selected []int) string {
	ids := make([]string, len(selected))
	for i, idx := range selected {
		ids[i] = fmt.Sprintf("#%d", rows[idx].ID)
	}
	return strings.Join(ids, ";")
}

func writeCSVFile(path string, rows []workday.Row, sep rune) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("could not create %s: %w", path, err)
	}
	if err := workday.WriteCSV(file, rows, sep); err != nil {
		file.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	return file.Close()
}

// parseSeparator accepts a single character or the word "tab".
func parseSeparator(s string) (rune, error) {
	if strings.EqualFold(s, "tab") || s == `\t` {
		return '\t', nil
	}
	r := []rune(s)
	if len(r) != 1 || r[0] == '"' || r[0] == '\r' || r[0] == '\n' {
		return 0, fmt.Errorf("invalid --separator %q: use a single character or \"tab\"", s)
	}
	return r[0], nil
}

// orgName accepts "myorg" or "https://dev.azure.com/myorg[/...]".
func orgName(org string) string {
	org = strings.TrimSpace(org)
	for _, prefix := range []string{"https://dev.azure.com/", "http://dev.azure.com/"} {
		org = strings.TrimPrefix(org, prefix)
	}
	if i := strings.Index(org, "/"); i >= 0 {
		org = org[:i]
	}
	return org
}
