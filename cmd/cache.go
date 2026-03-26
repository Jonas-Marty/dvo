package cmd

import "github.com/spf13/cobra"

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage local caches (users, etc.)",
}

func init() {
	rootCmd.AddCommand(cacheCmd)
}
