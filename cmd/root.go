package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version and Commit are injected at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
)

var rootCmd = &cobra.Command{
	Use:          "dvo",
	Short:        "Azure DevOps CLI",
	SilenceUsage: true,
	Long: `dvo - Azure DevOps CLI

A fast, native CLI tool for everyday Azure DevOps workflows:
Pull requests, work items, branch management, and more.

All commands require a Git repository with an Azure DevOps remote.`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")
		if short {
			fmt.Println(versionString())
			return
		}
		fmt.Printf("dvo %s (%s)\n", Version, Commit)
	},
}

// versionString returns the canonical single-token version used to stamp the
// completion files and to compare against at shell-init time. Prefers the
// semantic version tag and falls back to the commit hash.
func versionString() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	return Commit
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().Bool("short", false, "print only the version token (used by the completion auto-refresh check)")
}
