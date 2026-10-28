//go:build !windows

package adopat

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// lookupStore reads the credential the azure-devops extension stored on
// Linux/macOS.
//
// On Linux the extension falls back to a plaintext INI file whenever keyring
// has no working backend — which is the normal case under WSL, where no
// Secret Service daemon runs. That file is checked first because it is the
// likely location, then secret-tool for desktop sessions that do have one.
func lookupStore(service string) (string, error) {
	if service == "" {
		return "", ErrNotFound
	}
	if v, err := patFromFile(service); err == nil && v != "" {
		return v, nil
	}
	if v, err := patFromSecretTool(service); err == nil && v != "" {
		return v, nil
	}
	return "", ErrNotFound
}

// azDevOpsConfigDir mirrors AZ_DEVOPS_GLOBAL_CONFIG_DIR in the extension.
func azDevOpsConfigDir() string {
	if d := os.Getenv("AZURE_DEVOPS_EXT_CONFIG_DIR"); d != "" {
		return d
	}
	base := os.Getenv("AZURE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".azure")
	}
	return filepath.Join(base, "azuredevops")
}

// patFromFile parses ~/.azure/azuredevops/personalAccessTokens, a Python
// ConfigParser file whose section names are credential keys. ConfigParser
// lower-cases option names, so the fixed username is matched case-insensitively.
func patFromFile(service string) (string, error) {
	dir := azDevOpsConfigDir()
	if dir == "" {
		return "", ErrNotFound
	}
	f, err := os.Open(filepath.Join(dir, "personalAccessTokens"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	wantOption := strings.ToLower(credentialUser)
	inSection := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]") == service
			continue
		}
		if !inSection {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if strings.ToLower(strings.TrimSpace(key)) == wantOption {
			return strings.TrimSpace(value), nil
		}
	}
	return "", sc.Err()
}

// patFromSecretTool queries the Secret Service, matching the attributes
// Python's keyring SecretService backend writes.
func patFromSecretTool(service string) (string, error) {
	out, err := exec.Command("secret-tool", "lookup",
		"service", service, "username", credentialUser).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
