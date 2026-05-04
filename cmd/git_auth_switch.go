package cmd

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	switchToSSH   bool
	switchToHTTPS bool
)

var gitAuthSwitchCmd = &cobra.Command{
	Use:   "switch",
	Short: "Switch the origin remote between HTTPS and SSH",
	Long: `Switches the origin remote URL between HTTPS and SSH for Azure DevOps.
Asks for confirmation before applying the change.

Use --to-ssh or --to-https to force switch to a specific protocol without
prompting. If already on the target protocol, the command is a no-op.`,
	Example: `  dvo git auth switch
  dvo git auth switch --to-ssh
  dvo git auth switch --to-https`,
	RunE: runGitAuthSwitch,
}

func init() {
	gitAuthSwitchCmd.Flags().BoolVar(&switchToSSH, "to-ssh", false, "Switch to SSH (no-op if already SSH)")
	gitAuthSwitchCmd.Flags().BoolVar(&switchToHTTPS, "to-https", false, "Switch to HTTPS (no-op if already HTTPS)")
	gitAuthCmd.AddCommand(gitAuthSwitchCmd)
}

func runGitAuthSwitch(_ *cobra.Command, _ []string) error {
	remoteURL, err := git.GetRemoteURL()
	if err != nil {
		return err
	}

	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	current := detectProtocol(remoteURL)

	// Handle forced protocol selection via flags.
	if switchToSSH && switchToHTTPS {
		return fmt.Errorf("cannot specify both --to-ssh and --to-https")
	}

	if switchToSSH {
		if current == protoSSH {
			ui.Info.Println("Already using SSH.")
			return nil
		}
		// Switch to SSH without prompting.
		newURL := fmt.Sprintf("git@ssh.dev.azure.com:v3/%s/%s/%s", ctx.Org, ctx.Project, ctx.Repo)
		if err := exec.Command("git", "remote", "set-url", "origin", newURL).Run(); err != nil {
			return fmt.Errorf("failed to update remote URL: %w", err)
		}
		ui.Success.Printf("✓ Switched to %s: %s\n", string(protoSSH), newURL)
		return nil
	}

	if switchToHTTPS {
		if current == protoHTTPS {
			ui.Info.Println("Already using HTTPS.")
			return nil
		}
		// Switch to HTTPS without prompting.
		org := url.PathEscape(ctx.Org)
		project := url.PathEscape(ctx.Project)
		repo := url.PathEscape(ctx.Repo)
		newURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", org, project, repo)
		if err := exec.Command("git", "remote", "set-url", "origin", newURL).Run(); err != nil {
			return fmt.Errorf("failed to update remote URL: %w", err)
		}
		ui.Success.Printf("✓ Switched to %s: %s\n", string(protoHTTPS), newURL)
		return nil
	}

	// No flags: interactive toggle with confirmation.
	var target authProtocol
	var newURL string
	if current == protoHTTPS {
		target = protoSSH
		newURL = fmt.Sprintf("git@ssh.dev.azure.com:v3/%s/%s/%s", ctx.Org, ctx.Project, ctx.Repo)
	} else {
		target = protoHTTPS
		// URL-encode path segments for org, project and repo (e.g. spaces -> %20)
		org := url.PathEscape(ctx.Org)
		project := url.PathEscape(ctx.Project)
		repo := url.PathEscape(ctx.Repo)
		newURL = fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", org, project, repo)
	}

	fmt.Printf("Currently '%s' is used, switch to %s? [Y/n] ", string(current), string(target))
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" {
		ui.Warning.Println("Aborted.")
		return nil
	}

	if err := exec.Command("git", "remote", "set-url", "origin", newURL).Run(); err != nil {
		return fmt.Errorf("failed to update remote URL: %w", err)
	}

	ui.Success.Printf("✓ Switched to %s: %s\n", string(target), newURL)
	return nil
}
