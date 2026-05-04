package cmd

import (
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/git"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var gitAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Show the authentication protocol used for origin",
	Long: `Detects whether the current repository's origin remote uses HTTPS or SSH
for Azure DevOps.`,
	Example: `  dvo git auth`,
	RunE:    runGitAuth,
}

func init() {
	gitCmd.AddCommand(gitAuthCmd)
}

type authProtocol string

const (
	protoHTTPS authProtocol = "https"
	protoSSH   authProtocol = "ssh"
)

func detectProtocol(remoteURL string) authProtocol {
	if strings.HasPrefix(remoteURL, "https://") {
		return protoHTTPS
	}
	return protoSSH
}

func runGitAuth(_ *cobra.Command, _ []string) error {
	remoteURL, err := git.GetRemoteURL()
	if err != nil {
		return err
	}

	proto := detectProtocol(remoteURL)
	ui.Info.Printf("Origin uses %s (%s)\n", string(proto), remoteURL)
	return nil
}
