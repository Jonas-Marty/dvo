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
	Use:          "adg",
	Short:        "Azure DevOps CLI",
	SilenceUsage: true,
	Long: `adg - Azure DevOps CLI

A fast, native CLI tool for everyday Azure DevOps workflows:
Pull requests, work items, branch management, and more.

All commands require a Git repository with an Azure DevOps remote.`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("adg %s (%s)\n", Version, Commit)
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
