package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var workItemLinkCmd = &cobra.Command{
	Use:   "work-item-link <id>",
	Short: "Copy a formatted HTML work item link to the clipboard",
	Long: `Fetches the work item title from Azure DevOps and builds an HTML anchor tag:
  <a href="...">#{id}: {title}</a>

Both the HTML link and plain text are placed on the clipboard so you can
paste into Outlook, Word, Teams, etc. as a clickable hyperlink.`,
	Example: `  adg work-item-link 12345`,
	Args:    cobra.ExactArgs(1),
	RunE:    runWorkItemLink,
}

func init() {
	rootCmd.AddCommand(workItemLinkCmd)
}

type workItemFields struct {
	Title string `json:"System.Title"`
}

type workItemResponse struct {
	ID     int            `json:"id"`
	Fields workItemFields `json:"fields"`
}

func runWorkItemLink(_ *cobra.Command, args []string) error {
	id, err := parseWorkItemID(args[0])
	if err != nil {
		return err
	}

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	// Fetch work item with spinner.
	var wi workItemResponse
	spinErr := ui.RunSpinner(fmt.Sprintf("Fetching work item #%s...", id), func() error {
		out, err := exec.Command("az", "boards", "work-item", "show",
			"--id", id,
			"--org", ctx.OrgURL(),
			"--output", "json",
		).Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("az boards work-item show failed:\n%s", strings.TrimSpace(string(ee.Stderr)))
			}
			return err
		}
		return json.Unmarshal(out, &wi)
	})
	if spinErr != nil {
		return spinErr
	}

	if wi.Fields.Title == "" {
		return fmt.Errorf("could not read title for work item #%s", id)
	}

	workItemURL := ctx.WorkItemURL(id)
	htmlLink := fmt.Sprintf(`<a href="%s">#%s: %s</a>`, workItemURL, id, wi.Fields.Title)
	plainText := fmt.Sprintf("#%s: %s", id, wi.Fields.Title)

	if err := copyToClipboard(htmlLink, plainText); err != nil {
		// Fallback: just print it.
		ui.Warning.Printf("Could not copy to clipboard: %v\n", err)
		ui.Info.Printf("HTML:  %s\n", htmlLink)
		ui.Info.Printf("Plain: %s\n", plainText)
		return nil
	}

	ui.Success.Println("✓ Copied to clipboard!")
	ui.Info.Printf("  %s\n", plainText)
	ui.Info.Printf("  %s\n", workItemURL)
	return nil
}

// copyToClipboard sets both the HTML and plain-text clipboard formats via a
// small inline PowerShell script (Windows-only).
func copyToClipboard(htmlContent, plainText string) error {
	psScript := "Add-Type -AssemblyName System.Windows.Forms\n" +
		"$html  = $args[0]\n" +
		"$plain = $args[1]\n" +
		"$enc = [System.Text.Encoding]::UTF8\n" +
		"$htmlStart = \"<html><body><!--StartFragment-->\"\n" +
		"$htmlEnd   = \"<!--EndFragment--></body></html>\"\n" +
		"$hdr = \"Version:0.9`r`nStartHTML:0000000000`r`nEndHTML:0000000000`r`nStartFragment:0000000000`r`nEndFragment:0000000000`r`n\"\n" +
		"$hdrBytes      = $enc.GetByteCount($hdr)\n" +
		"$startHTML     = $hdrBytes\n" +
		"$startFragment = $startHTML     + $enc.GetByteCount($htmlStart)\n" +
		"$endFragment   = $startFragment + $enc.GetByteCount($html)\n" +
		"$endHTML       = $endFragment   + $enc.GetByteCount($htmlEnd)\n" +
		"$hdr = \"Version:0.9`r`n\" +\n" +
		"       \"StartHTML:\"     + $startHTML.ToString(\"0000000000\")     + \"`r`n\" +\n" +
		"       \"EndHTML:\"       + $endHTML.ToString(\"0000000000\")       + \"`r`n\" +\n" +
		"       \"StartFragment:\" + $startFragment.ToString(\"0000000000\") + \"`r`n\" +\n" +
		"       \"EndFragment:\"   + $endFragment.ToString(\"0000000000\")   + \"`r`n\"\n" +
		"$full = $hdr + $htmlStart + $html + $htmlEnd\n" +
		"$data = New-Object System.Windows.Forms.DataObject\n" +
		"$data.SetData([System.Windows.Forms.DataFormats]::Html,  $full)\n" +
		"$data.SetData([System.Windows.Forms.DataFormats]::Text,  $plain)\n" +
		"[System.Windows.Forms.Clipboard]::SetDataObject($data, $true)\n"
	// Write script to a temp file to avoid shell-escaping issues with quotes.
	tmp, err := os.CreateTemp("", "adg-clip-*.ps1")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(psScript); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", tmp.Name(), htmlContent, plainText)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("powershell: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
