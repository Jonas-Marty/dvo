package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const CurrentVersion = "1.0"

// Config holds persistent user-level settings for dvo.
type Config struct {
	Version  string `json:"version"`
	RepoRoot string `json:"repoRoot,omitempty"`
}

// KnownKeys returns the list of settable config keys for shell completion.
func KnownKeys() []string {
	return []string{"repo-root"}
}

// configFilePath returns ~/.config/dvo/config.json
func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dvo", "config.json"), nil
}

// Load reads the config file. Returns a default Config if the file does not exist.
func Load() (Config, error) {
	path, err := configFilePath()
	if err != nil {
		return defaultConfig(), err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultConfig(), nil
	}
	if err != nil {
		return defaultConfig(), err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig(), err
	}
	return cfg, nil
}

// Save writes the config to disk, stamping the current version.
func Save(cfg Config) error {
	cfg.Version = CurrentVersion
	path, err := configFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func defaultConfig() Config {
	return Config{Version: CurrentVersion}
}

// NormalizePath converts Unix-style Git-Bash paths to native OS paths.
// On Windows: /d/Git -> D:/Git
// On other platforms: path is returned unchanged.
func NormalizePath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	// Unix-style absolute path: /d/... or /D/...
	if len(path) >= 3 && path[0] == '/' && path[2] == '/' {
		drive := strings.ToUpper(string(path[1]))
		rest := path[3:]
		return drive + ":/" + rest
	}
	return path
}
