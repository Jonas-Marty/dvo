package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var openVsCmd = &cobra.Command{
	Use:   "open-vs [path]",
	Short: "Open a solution file in Visual Studio",
	Long: `Searches for .sln/.slnx files under [path] (default: current directory)
and opens the selected solution in Visual Studio.

If a .slnx exists alongside a .sln with the same base name, only the .slnx is shown.
When multiple solutions are found an interactive picker is shown.`,
	RunE: runOpenVs,
}

var (
	openVs2022     bool
	openVsInsiders bool
)

func init() {
	rootCmd.AddCommand(openVsCmd)
	openVsCmd.Flags().BoolVar(&openVs2022, "2022", false, "Use Visual Studio 2022 Professional")
	openVsCmd.Flags().BoolVar(&openVsInsiders, "insiders", false, "Use Visual Studio Insiders")
}

const (
	vs2026Path    = `C:\Program Files\Microsoft Visual Studio\18\Professional\Common7\IDE\devenv.exe`
	vs2022Path    = `C:\Program Files\Microsoft Visual Studio\2022\Professional\Common7\IDE\devenv.exe`
	vsInsiderPath = `C:\Program Files\Microsoft Visual Studio\18\Insiders\Common7\IDE\devenv.exe`
)

func runOpenVs(cmd *cobra.Command, args []string) error {
	searchPath := "."
	if len(args) > 0 {
		searchPath = args[0]
	}

	devenvExe, vsLabel, err := resolveDevenv()
	if err != nil {
		return err
	}

	info, err := os.Stat(searchPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("path %q does not exist or is not a directory", searchPath)
	}

	slnFiles, err := findSolutions(searchPath)
	if err != nil {
		return err
	}
	if len(slnFiles) == 0 {
		return fmt.Errorf("no solution files (*.sln or *.slnx) found in %q", searchPath)
	}

	var selected string
	if len(slnFiles) == 1 {
		selected = slnFiles[0]
	} else {
		idx, err := ui.PickOne("Select a solution to open:", slnFiles)
		if err != nil {
			return err
		}
		if idx < 0 {
			ui.Info.Println("Cancelled.")
			return nil
		}
		selected = slnFiles[idx]
	}

	ui.Success.Printf("Opening: %s\n", selected)
	ui.Info.Printf("Using:   %s\n", vsLabel)

	return exec.Command(devenvExe, selected).Start()
}

func resolveDevenv() (string, string, error) {
	if openVs2022 {
		if _, err := os.Stat(vs2022Path); err != nil {
			return "", "", fmt.Errorf("Visual Studio 2022 Professional not found\n  expected: %s", vs2022Path)
		}
		return vs2022Path, "Visual Studio 2022 Professional", nil
	}
	if openVsInsiders {
		if _, err := os.Stat(vsInsiderPath); err != nil {
			return "", "", fmt.Errorf("Visual Studio Insiders not found\n  expected: %s", vsInsiderPath)
		}
		return vsInsiderPath, "Visual Studio Insiders", nil
	}
	// Default: VS 2026
	if _, err := os.Stat(vs2026Path); err != nil {
		return "", "", fmt.Errorf("Visual Studio 2026 Professional not found\n  expected: %s\n  try --2022 or --insiders", vs2026Path)
	}
	return vs2026Path, "Visual Studio 2026 Professional", nil
}

// findSolutions walks root for *.sln and *.slnx files.
// When both Foo.sln and Foo.slnx exist, Foo.sln is dropped in favour of Foo.slnx.
func findSolutions(root string) ([]string, error) {
	var all []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".sln" || ext == ".slnx" {
				all = append(all, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Build set of known .slnx paths for O(1) lookup.
	slnxSet := make(map[string]bool, len(all))
	for _, p := range all {
		if strings.ToLower(filepath.Ext(p)) == ".slnx" {
			slnxSet[p] = true
		}
	}

	// Drop .sln entries that have a corresponding .slnx.
	filtered := all[:0]
	for _, p := range all {
		if strings.ToLower(filepath.Ext(p)) == ".sln" && slnxSet[p+"x"] {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered, nil
}
