package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

// ExitCancelled is the conventional shell exit code for "interrupted by SIGINT".
const ExitCancelled = 130

// Version and Commit are injected at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "dvo",
	Short: "Azure DevOps CLI",
	// Errors are rendered by Execute so a cancellation reads as "Cancelled."
	// instead of as a failure.
	SilenceUsage:  true,
	SilenceErrors: true,
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

// Execute runs the CLI and returns the process exit code.
//
// A command that was interrupted reports ui.ErrCancelled rather than a
// zero-valued success, and lands here: nothing further is attempted, the user
// gets one "Cancelled." line instead of a stack of half-finished output, and
// the shell sees 130.
//
// No SIGINT handler is installed on purpose. Outside a bubbletea program the
// terminal is not in raw mode, so Ctrl+C reaches the whole process group and
// kills dvo along with the git or az child it is waiting on — which is already
// the behaviour we want. Catching the signal here would only keep dvo running
// past the point the user asked it to stop.
func Execute() int {
	err := rootCmd.ExecuteContext(context.Background())
	switch {
	case err == nil:
		return 0
	case ui.IsCancelled(err), errors.Is(err, context.Canceled):
		ui.Warning.Println("Cancelled.")
		return ExitCancelled
	default:
		ui.Error.Printf("Error: %v\n", err)
		return 1
	}
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().Bool("short", false, "print only the version token (used by the completion auto-refresh check)")
}
