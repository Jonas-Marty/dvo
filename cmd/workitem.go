package cmd

import "github.com/spf13/cobra"

var workitemCmd = &cobra.Command{
	Use:     "workitem",
	Aliases: []string{"wi", "w"},
	Short:   "Manage work items",
}

func init() {
	rootCmd.AddCommand(workitemCmd)
}
