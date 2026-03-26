package cmd

import "github.com/spf13/cobra"

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Repository helpers",
}

func init() {
	rootCmd.AddCommand(repoCmd)
}
