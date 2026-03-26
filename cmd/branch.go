package cmd

import "github.com/spf13/cobra"

var branchCmd = &cobra.Command{
	Use:     "branch",
	Aliases: []string{"b"},
	Short:   "Branch management helpers",
}

func init() {
	rootCmd.AddCommand(branchCmd)
}
