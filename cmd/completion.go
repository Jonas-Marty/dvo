package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// versionHeaderLine is the comment injected at the top of every generated
// completion file. The shell-init snippets compare it against the running
// binary's version so a shell can tell whether its completions are stale
// without executing dvo on every startup.
const versionHeaderLine = "# dvo-version: "

// completionCmd replaces cobra's auto-generated completion command so that we
// can attach the custom "regen" subcommand. Registering it in init() — before
// Execute() — is what stops cobra from adding its own.
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for dvo.

To load completions in your current shell session:
  source <(dvo completion bash)

To regenerate the static completion files (with version stamp):
  dvo completion regen`,
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unknown shell %q", args[0])
		}
	},
}

// completionRegenCmd rewrites the static bash and PowerShell completion files
// and stamps them with the current version, so the shell-init checks can tell
// a new build from the one their completions were generated for.
var completionRegenCmd = &cobra.Command{
	Use:   "regen",
	Short: "Regenerate the static completion files (with version stamp)",
	Long: `Regenerate the static bash and PowerShell completion files used by the
shell-init auto-refresh checks.

The files are written next to the dvo binary by default; use --dir to
override. Each starts with a comment of the form:

  # dvo-version: <version>

which the snippets installed by 'dvo init' compare against the running
binary, so a shell only regenerates when the version actually changed.

A file whose contents already match is left untouched, so rebuilding
without changing any command does not rewrite it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		if dir == "" {
			var err error
			if dir, err = defaultCompletionDir(); err != nil {
				return err
			}
		}

		written, err := writeStampedCompletions(dir)
		if err != nil {
			return err
		}
		if len(written) == 0 {
			fmt.Fprintf(os.Stderr, "completions already up to date in %s (version: %s)\n", dir, versionString())
			return nil
		}
		for _, p := range written {
			fmt.Fprintf(os.Stderr, "completions written to: %s (version: %s)\n", p, versionString())
		}
		return nil
	},
}

// defaultCompletionDir returns the directory holding the running binary, which
// is where 'dvo init' points the shell profiles.
func defaultCompletionDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine binary path: %w", err)
	}
	return filepath.Dir(exe), nil
}

// BashCompletionPath and PowerShellCompletionPath name the generated files
// inside dir. Shared by regen and init so both agree on the layout.
func BashCompletionPath(dir string) string { return filepath.Join(dir, "dvo-completion.bash") }
func PowerShellCompletionPath(dir string) string {
	return filepath.Join(dir, "dvo-completion.ps1")
}

// writeStampedCompletions writes both completion scripts into dir and returns
// the paths it actually changed.
//
// Both 'dvo completion regen' and 'dvo init' go through here so the files on
// disk are always stamped — otherwise the first shell after init would see a
// missing stamp and needlessly regenerate.
func writeStampedCompletions(dir string) ([]string, error) {
	bash, err := stampedScript(func(buf *bytes.Buffer) error {
		return rootCmd.GenBashCompletionV2(buf, true)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate bash completion: %w", err)
	}
	ps, err := stampedScript(func(buf *bytes.Buffer) error {
		return rootCmd.GenPowerShellCompletionWithDesc(buf)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate powershell completion: %w", err)
	}

	var written []string
	for path, content := range map[string][]byte{
		BashCompletionPath(dir):       bash,
		PowerShellCompletionPath(dir): ps,
	} {
		changed, err := writeIfChanged(path, content)
		if err != nil {
			return written, err
		}
		if changed {
			written = append(written, path)
		}
	}
	return written, nil
}

// stampedScript renders a completion script prefixed with the version header.
// '#' starts a comment in both bash and PowerShell, so one form covers both.
func stampedScript(gen func(*bytes.Buffer) error) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s%s\n", versionHeaderLine, versionString())
	if err := gen(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeIfChanged writes content to path only when it differs from what is
// already there, and reports whether it wrote. Skipping the identical case
// keeps a rebuild that changed no command from touching the file at all — no
// mtime churn, and nothing for git to show.
func writeIfChanged(path string, content []byte) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return false, nil
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return false, fmt.Errorf("failed to write %s: %w", path, err)
	}
	return true, nil
}

func init() {
	// Registered before Execute() so cobra does not add its own plain
	// completion command alongside this one.
	rootCmd.AddCommand(completionCmd)
	completionCmd.AddCommand(completionRegenCmd)

	completionRegenCmd.Flags().StringP("dir", "d", "",
		"directory to write the completion files into (default: the directory holding dvo)")
}
