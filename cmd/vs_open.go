package cmd

import (
	"context"
	"fmt"
	"io/fs"
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
	openVerbose    bool
)

func init() {
	vsCmd.AddCommand(openVsCmd)
	openVsCmd.Flags().BoolVar(&openVs2022, "2022", false, "Use Visual Studio 2022 Professional")
	openVsCmd.Flags().BoolVarP(&openVsInsiders, "insiders", "i", false, "Use Visual Studio Insiders")
	openVsCmd.Flags().BoolVarP(&openCode, "code", "c", false, "Open in VS Code instead of Visual Studio")
	openVsCmd.Flags().StringVarP(&openPath, "path", "p", "", "Search for solutions under this directory (default: current directory)")
	openVsCmd.Flags().StringVarP(&openFile, "file", "f", "", "Search for a file by name and open the containing repository")
	openVsCmd.Flags().BoolVarP(&openVerbose, "verbose", "v", false, "Print search commands as they are executed")
	openVsCmd.MarkFlagsMutuallyExclusive("path", "file")
}

const (
	vs2022Path    = `C:\Program Files\Microsoft Visual Studio\2022\Professional\Common7\IDE\devenv.exe`
	vs2026Path    = `C:\Program Files\Microsoft Visual Studio\18\Professional\Common7\IDE\devenv.exe`
	vsInsiderPath = `C:\Program Files\Microsoft Visual Studio\18\Insiders\Common7\IDE\devenv.exe`
	codeExePath   = `C:\Program Files\Microsoft VS Code\bin\code.cmd`
)

func runOpenVs(cmd *cobra.Command, args []string) error {
	cfg, _ := config.Load()

	configuredRoot := config.NormalizePath(cfg.RepoRoot)

	if openFile != "" {
		// File-search mode: default to repo-root from config, then ".".
		searchRoot := config.NormalizePath(openPath)
		if searchRoot == "" {
			searchRoot = configuredRoot
		}
		if searchRoot == "" {
			searchRoot = "."
		}
		info, err := os.Stat(searchRoot)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("path %q does not exist or is not a directory", searchRoot)
		}
		var found fileMatches
		if err := ui.RunSpinner(cmd.Context(), fmt.Sprintf("Searching for %q ...", openFile), func(c context.Context) error {
			var err error
			found, err = findRepoRootsByFile(c, searchRoot, openFile)
			return err
		}); err != nil {
			return err
		}

		// The picker runs after the spinner has released the terminal — two
		// bubbletea programs cannot share it.
		repoRoot := found.roots[0]
		if len(found.roots) > 1 {
			idx, err := ui.PickOne(fmt.Sprintf("Found %q in multiple repositories — select one:", openFile), found.roots)
			if err != nil {
				return err
			}
			repoRoot = found.roots[idx]
		}
		return openInEditor(cmd.Context(), repoRoot, found.fileByRoot[repoRoot])
	}

	// Path mode: always default to current directory, ignore repo-root.
	searchPath := config.NormalizePath(openPath)
	if searchPath == "" {
		searchPath = "."
	}
	info, err := os.Stat(searchPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("path %q does not exist or is not a directory", searchPath)
	}
	return openInEditor(cmd.Context(), searchPath, "")
}

// openInEditor opens VS or VS Code for the given directory.
// filePath may be empty; when set the editor is asked to navigate directly to it.
func openInEditor(ctx context.Context, targetDir, filePath string) error {
	if openCode {
		if _, _, err := resolveCodePath(); err != nil {
			return err
		}
		ui.Success.Printf("Opening: %s\n", targetDir)
		ui.Info.Println("Using:   VS Code")
		var c *exec.Cmd
		if filePath != "" {
			// code -r <repoRoot> <file> — reuses the existing window and opens the file
			c = exec.Command("code", "-r", targetDir, filePath)
		} else {
			c = exec.Command("code", ".")
			c.Dir = targetDir
		}
		return c.Start()
	}

	devenvExe, vsLabel, err := resolveDevenv()
	if err != nil {
		return err
	}

	var slnFiles []string
	if err := ui.RunSpinner(ctx, fmt.Sprintf("Searching for solutions in %q ...", targetDir), func(c context.Context) error {
		var err error
		slnFiles, err = findSolutions(c, targetDir)
		return err
	}); err != nil {
		return err
	}
	if len(slnFiles) == 0 {
		return fmt.Errorf("no solution files (*.sln or *.slnx) found in %q", targetDir)
	}

	selectedSlnFile := slnFiles[0]
	if len(slnFiles) > 1 {
		idx, err := ui.PickOne("Select a solution to open:", slnFiles)
		if err != nil {
			return err
		}
		selectedSlnFile = slnFiles[idx]
	}

	ui.Success.Printf("Opening: %s\n", selectedSlnFile)
	ui.Info.Printf("Using:   %s\n", vsLabel)

	if filePath != "" {
		return exec.Command(devenvExe, selectedSlnFile, filePath).Start()
	}
	return exec.Command(devenvExe, selectedSlnFile).Start()
}

// fileMatches holds the distinct git repository roots a file search turned up,
// in discovery order, with the first matching file found in each.
type fileMatches struct {
	roots      []string
	fileByRoot map[string]string
}

// findRepoRootsByFile searches root for any file matching filename (by base name,
// case-insensitive). If filename has no extension, any file whose base name
// (without extension) matches is included (e.g. "Foo" matches "Foo.cs").
// For each match the git repository root is located by walking up from the
// file's directory; distinct repo roots are deduplicated.
//
// It performs no UI of its own so it can run inside a spinner: a walk that is
// cancelled aborts with ctx.Err() rather than returning a partial match list
// that the caller would act on.
func findRepoRootsByFile(ctx context.Context, root, filename string) (fileMatches, error) {
	var none fileMatches
	hasExt := filepath.Ext(filename) != ""
	var matches []string
	esRan := false
	if _, lookErr := exec.LookPath("es"); lookErr == nil {
		esMatches, esErr := findFilesWithEverything(ctx, root, filename, hasExt)
		if esErr == nil {
			esRan = true
			matches = esMatches
		}
	}
	if !esRan {
		if openVerbose {
			ui.Info.Printf("es not available, falling back to WalkDir: %s\n", root)
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				return nil // skip unreadable entries
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if hasExt {
				if strings.EqualFold(name, filename) {
					matches = append(matches, path)
				}
			} else {
				stem := strings.TrimSuffix(name, filepath.Ext(name))
				if strings.EqualFold(stem, filename) {
					matches = append(matches, path)
				}
			}
			return nil
		})
		if err != nil {
			return none, err
		}
	}
	if len(matches) == 0 {
		return none, fmt.Errorf("no file named %q found under %q", filename, root)
	}

	// Collect distinct git repo roots, keeping the first matched file per root.
	seen := make(map[string]bool)
	result := fileMatches{fileByRoot: make(map[string]string)}
	for _, m := range matches {
		repoRoot := findGitRoot(filepath.Dir(m), root)
		if !seen[repoRoot] {
			seen[repoRoot] = true
			result.roots = append(result.roots, repoRoot)
			result.fileByRoot[repoRoot] = m
		}
	}
	return result, nil
}

// findFilesWithEverything uses the Everything CLI (es) to locate files matching
// filename under root. When hasExt is false the pattern matches any extension
// and results are filtered to files whose base name (without extension) equals
// filename. Returns nil, err on failure so the caller can fall back to WalkDir.
func findFilesWithEverything(ctx context.Context, root, filename string, hasExt bool) ([]string, error) {
	var pattern string
	if hasExt {
		pattern = root + `\*\` + filename
	} else {
		pattern = root + `\*\` + filename + `.*`
	}
	if openVerbose {
		ui.Info.Printf("Running: es %s\n", pattern)
	}
	out, err := exec.CommandContext(ctx, "es", pattern).Output()
	if err != nil {
		return nil, err
	}
	rootLower := strings.ToLower(filepath.Clean(root))
	var matches []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(filepath.Clean(line)), rootLower) {
			continue
		}
		name := filepath.Base(line)
		if hasExt {
			if strings.EqualFold(name, filename) {
				matches = append(matches, line)
			}
		} else {
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if strings.EqualFold(stem, filename) {
				matches = append(matches, line)
			}
		}
	}
	return matches, nil
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
func findSolutions(ctx context.Context, root string) ([]string, error) {
	var all []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return nil // skip unreadable entries
		}
		if !d.IsDir() {
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
