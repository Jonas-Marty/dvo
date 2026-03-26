package cmd

import "github.com/spf13/cobra"

var vsCmd = &cobra.Command{
	Use:   "vs",
	Short: "Visual Studio helpers",
}

func init() {
	rootCmd.AddCommand(vsCmd)
}
