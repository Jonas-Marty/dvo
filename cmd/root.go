package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "adg",
	Short:        "Azure DevOps CLI - Manage Azure DevOps workflows",
	SilenceUsage: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// subcommands registered in their own files via init()
}
