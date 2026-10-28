package cache

import "testing"

func TestTenantRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := LoadTenant("Org"); got != "" {
		t.Fatalf("empty cache returned %q", got)
	}
	if err := SaveTenant("Org", "tenant-1"); err != nil {
		t.Fatal(err)
	}
	if got := LoadTenant("Org"); got != "tenant-1" {
		t.Errorf("LoadTenant = %q", got)
	}
	if got := LoadTenant("Other"); got != "" {
		t.Errorf("tenants must be kept per organization, got %q", got)
	}
}
