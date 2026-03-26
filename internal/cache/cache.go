package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// User represents a cached Azure DevOps user entry.
type User struct {
	Alias       string `json:"alias"`       // local part of email, e.g. "alice.smith"
	Email       string `json:"email"`       // full mail address
	DisplayName string `json:"displayName"` // from Azure DevOps
}

type cacheFile struct {
	RefreshedAt time.Time `json:"refreshedAt"`
	Users       []User    `json:"users"`
}

// cacheDir returns ~/.config/adg/cache/<org>/
func cacheDir(org string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "adg", "cache", org), nil
}

func cacheFilePath(org string) (string, error) {
	dir, err := cacheDir(org)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "users.json"), nil
}

// Load reads the cached users for the given org.
// Returns an empty slice (not an error) when the cache does not exist yet.
func Load(org string) ([]User, error) {
	path, err := cacheFilePath(org)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, err
	}
	return cf.Users, nil
}

// Save writes users to the cache for the given org.
func Save(org string, users []User) error {
	dir, err := cacheDir(org)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "users.json")
	cf := cacheFile{RefreshedAt: time.Now().UTC(), Users: users}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ResolveReviewer resolves a reviewer input to a full email address.
//
//   - If the input already contains "@", it is returned as-is.
//   - Otherwise, users whose alias starts with the input are searched.
//     Exact match wins over prefix match. If multiple prefix matches exist,
//     an error listing the candidates is returned.
func ResolveReviewer(org, input string) (string, error) {
	if strings.Contains(input, "@") {
		return input, nil
	}
	users, err := Load(org)
	if err != nil {
		return "", fmt.Errorf("could not read reviewer cache: %w", err)
	}
	if len(users) == 0 {
		return "", fmt.Errorf(
			"reviewer cache is empty for org %q — run: adg cache refresh", org)
	}

	lower := strings.ToLower(input)
	var exact *User
	var prefix []User
	for i := range users {
		u := &users[i]
		if strings.ToLower(u.Alias) == lower {
			exact = u
			break
		}
		if strings.HasPrefix(strings.ToLower(u.Alias), lower) {
			prefix = append(prefix, *u)
		}
	}
	if exact != nil {
		return exact.Email, nil
	}
	if len(prefix) == 1 {
		return prefix[0].Email, nil
	}
	if len(prefix) > 1 {
		names := make([]string, len(prefix))
		for i, p := range prefix {
			names[i] = p.Alias
		}
		return "", fmt.Errorf(
			"ambiguous reviewer %q — matches: %s\nBe more specific or use the full email",
			input, strings.Join(names, ", "))
	}
	return "", fmt.Errorf(
		"reviewer %q not found in cache — run: adg cache refresh\nOr pass a full email address",
		input)
}

// Aliases returns all cached alias strings for the given org (used by completion).
func Aliases(org string) []string {
	users, err := Load(org)
	if err != nil || len(users) == 0 {
		return nil
	}
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Alias
	}
	return out
}

// AliasFromEmail derives the alias (local part) from an email address.
func AliasFromEmail(email string) string {
	if idx := strings.Index(email, "@"); idx >= 0 {
		return strings.ToLower(email[:idx])
	}
	return strings.ToLower(email)
}
