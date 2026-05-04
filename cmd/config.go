package cmd

import (
	"fmt"
	"os"

	"github.com/Jonas-Marty/ad-cli/internal/config"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage dvo configuration",
	Long: `Get and set dvo configuration values.

Usage:
  dvo config get <key>
  dvo config set <key> <value>

Keys:
  repo-root    Default root directory used when searching for repositories or solutions`,
}

func keyCompleter(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return config.KnownKeys(), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

var configGetCmd = &cobra.Command{
	Use:               "get <key>",
	Short:             "Get a configuration value",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: keyCompleter,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		switch args[0] {
		case "repo-root":
			v := config.NormalizePath(cfg.RepoRoot)
			if v == "" {
				ui.Info.Println("(not set)")
			} else {
				fmt.Println(v)
			}
		default:
			return fmt.Errorf("unknown config key %q — run `dvo config --help` for available keys", args[0])
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:               "set <key> <value>",
	Short:             "Set a configuration value",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: keyCompleter,
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		switch key {
		case "repo-root":
			value = config.NormalizePath(value)
			info, err := os.Stat(value)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("path %q does not exist or is not a directory", value)
			}
			cfg.RepoRoot = value
		default:
			return fmt.Errorf("unknown config key %q — run `dvo config --help` for available keys", key)
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		ui.Success.Printf("%s = %s\n", key, value)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}
