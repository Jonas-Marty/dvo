package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/config"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var openVsCmd = &cobra.Command{
	Use:     "open",
	Aliases: []string{"o"},
	Short:   "Open a solution file in Visual Studio or VS Code",
	Long: `Searches for .sln/.slnx files and opens them in Visual Studio (or VS Code with --code).

Use --path to search for solutions under a directory (default: current directory).
Use --file to search for a file by name; the command will open the git repository
that contains the file. Only one of --path or --file may be provided.

If a .slnx exists alongside a .sln with the same base name, only the .slnx is shown.
When multiple solutions or repositories are found, an interactive picker is shown.`,
	Args: cobra.NoArgs,
	RunE: runOpenVs,
}

var (
	openVs2022     bool
	openVsInsiders bool
	openCode       bool
	openPath       string
	openFile       string
)

func init() {
	vsCmd.AddCommand(openVsCmd)
	openVsCmd.Flags().BoolVar(&openVs2022, "2022", false, "Use Visual Studio 2022 Professional")
	openVsCmd.Flags().BoolVar(&openVsInsiders, "insiders", false, "Use Visual Studio Insiders")
	openVsCmd.Flags().BoolVar(&openCode, "code", false, "Open in VS Code instead of Visual Studio")
	openVsCmd.Flags().StringVar(&openPath, "path", "", "Search for solutions under this directory (default: current directory)")
	openVsCmd.Flags().StringVar(&openFile, "file", "", "Search for a file by name and open the containing repository")
	openVsCmd.MarkFlagsMutuallyExclusive("path", "file")
}

const (
	vs2026Path    = `C:\Program Files\Microsoft Visual Studio\18\Professional\Common7\IDE\devenv.exe`
	vs2022Path    = `C:\Program Files\Microsoft Visual Studio\2022\Professional\Common7\IDE\devenv.exe`
	vsInsiderPath = `C:\Program Files\Microsoft Visual Studio\18\Insiders\Common7\IDE\devenv.exe`
	codeExePath   = `C:\Program Files\Microsoft VS Code\bin\code.cmd`
)

func runOpenVs(cmd *cobra.Command, args []string) error {
	cfg, _ := config.Load()

	defaultRoot := config.NormalizePath(cfg.RepoRoot)
	if defaultRoot == "" {
		defaultRoot = "."
	}

	if openFile != "" {
		// File-search mode: find the git repo that contains the named file.
		searchRoot := config.NormalizePath(openPath)
		if searchRoot == "" {
			searchRoot = defaultRoot
		}
		info, err := os.Stat(searchRoot)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("path %q does not exist or is not a directory", searchRoot)
		}
		repoRoot, cancelled, findErr := findRepoRootByFile(searchRoot, openFile)
		if findErr != nil {
			return findErr
		}
		if cancelled {
			ui.Info.Println("Cancelled.")
			return nil
		}
		return openInEditor(repoRoot)
	}

	// Path mode: search for solution files.
	searchPath := config.NormalizePath(openPath)
	if searchPath == "" {
		searchPath = defaultRoot
	}
	info, err := os.Stat(searchPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("path %q does not exist or is not a directory", searchPath)
	}
	return openInEditor(searchPath)
}

// openInEditor opens VS or VS Code for the given directory.
func openInEditor(targetDir string) error {
	if openCode {
		if _, _, err := resolveCodePath(); err != nil {
			return err
		}
		ui.Success.Printf("Opening: %s\n", targetDir)
		ui.Info.Println("Using:   VS Code")
		c := exec.Command("code", ".")
		c.Dir = targetDir
		return c.Start()
	}

	devenvExe, vsLabel, err := resolveDevenv()
	if err != nil {
		return err
	}

	slnFiles, err := findSolutions(targetDir)
	if err != nil {
		return err
	}
	if len(slnFiles) == 0 {
		return fmt.Errorf("no solution files (*.sln or *.slnx) found in %q", targetDir)
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

// findRepoRootByFile searches root for any file matching filename (by base name,
// case-insensitive). For each match the git repository root is located by
// walking up from the file's directory. Distinct repo roots are deduplicated;
// if more than one is found an interactive picker is shown.
// Returns the chosen repo root, a cancelled flag, and any error.
func findRepoRootByFile(root, filename string) (string, bool, error) {
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), filename) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	if len(matches) == 0 {
		return "", false, fmt.Errorf("no file named %q found under %q", filename, root)
	}

	// Collect distinct git repo roots.
	seen := make(map[string]bool)
	var repoRoots []string
	for _, m := range matches {
		repoRoot := findGitRoot(filepath.Dir(m), root)
		if !seen[repoRoot] {
			seen[repoRoot] = true
			repoRoots = append(repoRoots, repoRoot)
		}
	}

	if len(repoRoots) == 1 {
		return repoRoots[0], false, nil
	}

	idx, err := ui.PickOne(fmt.Sprintf("Found %q in multiple repositories — select one:", filename), repoRoots)
	if err != nil {
		return "", false, err
	}
	if idx < 0 {
		return "", true, nil
	}
	return repoRoots[idx], false, nil
}

// findGitRoot walks up from dir toward stopAt looking for a .git directory.
// Returns dir if no .git is found before reaching stopAt or the filesystem root.
func findGitRoot(dir, stopAt string) string {
	stopAtAbs, _ := filepath.Abs(stopAt)
	current, _ := filepath.Abs(dir)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		if current == stopAtAbs {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break // filesystem root
		}
		current = parent
	}
	return dir
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

func resolveCodePath() (string, string, error) {
	if _, err := os.Stat(codeExePath); err != nil {
		return "", "", fmt.Errorf("VS Code not found\n  expected: %s", codeExePath)
	}
	return codeExePath, "VS Code", nil
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
