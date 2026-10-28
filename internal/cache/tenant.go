package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type tenantFile struct {
	Tenant     string    `json:"tenant"`
	DetectedAt time.Time `json:"detectedAt"`
}

// LoadTenant returns the Entra tenant remembered for org, or "" when none is
// stored or the file is unreadable. A missing or broken cache only costs a
// fresh detection, so errors are not worth surfacing.
func LoadTenant(org string) string {
	dir, err := cacheDir(org)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "tenant.json"))
	if err != nil {
		return ""
	}
	var tf tenantFile
	if json.Unmarshal(data, &tf) != nil {
		return ""
	}
	return strings.TrimSpace(tf.Tenant)
}

// SaveTenant remembers the tenant that org accepts tokens from.
func SaveTenant(org, tenant string) error {
	dir, err := cacheDir(org)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tenantFile{Tenant: tenant, DetectedAt: time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "tenant.json"), data, 0644)
}
