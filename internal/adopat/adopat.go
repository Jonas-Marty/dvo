// Package adopat resolves the Azure DevOps Personal Access Token for direct
// REST calls.
//
// dvo does not store the token itself. It reads the credential that
// `az devops login` already writes into the operating system's credential
// store, so one credential serves `az` and dvo's own HTTP calls.
//
// A PAT is preferred over the Entra token from `az account get-access-token`
// because that token belongs to the default `az` tenant, which is often not
// the tenant the organization is connected to; Azure DevOps then answers every
// call except connectionData with TF400813 (401).
package adopat

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	// EnvExt is the variable the azure-devops CLI extension itself honours,
	// so setting it configures `az` and dvo in one go.
	EnvExt = "AZURE_DEVOPS_EXT_PAT"

	// credentialUser mirrors CredentialStore._USERNAME in the azure-devops
	// extension. It is a fixed literal, not a real account name.
	credentialUser = "Personal Access Token"
	// defaultServiceKey is the extension's org-independent fallback entry.
	defaultServiceKey = "azdevops-cli: default"
)

// ErrNotFound reports that no token could be located in any source.
var ErrNotFound = errors.New("no Azure DevOps credential found")

// Lookup returns the PAT for org (a bare name or an organization URL).
func Lookup(org string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvExt)); v != "" {
		return v, nil
	}
	// Mirror the extension's own lookup: organization entry first, then the
	// org-independent default entry.
	if v, err := lookupStore(ServiceKey(org)); err == nil && v != "" {
		return v, nil
	}
	if v, err := lookupStore(defaultServiceKey); err == nil && v != "" {
		return v, nil
	}
	return "", ErrNotFound
}

// ServiceKey builds the credential-store key used by the azure-devops CLI
// extension: "azdevops-cli:" + scheme://host/org, lowercased.
func ServiceKey(org string) string {
	return "azdevops-cli:" + normalizeOrgURL(org)
}

func normalizeOrgURL(org string) string {
	org = strings.TrimSpace(strings.TrimSuffix(org, "/"))
	if org == "" {
		return ""
	}
	if !strings.Contains(org, "://") {
		org = "https://dev.azure.com/" + org
	}
	u, err := url.Parse(org)
	if err != nil {
		return strings.ToLower(org)
	}
	base := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	// Legacy *.visualstudio.com URLs carry the organization in the host.
	path := strings.ToLower(strings.Trim(u.Path, "/"))
	if path == "" || strings.Contains(strings.ToLower(u.Host), "visualstudio.com") {
		return base
	}
	return base + "/" + strings.Split(path, "/")[0]
}

// LoginHint returns the command that stores a credential for org.
func LoginHint(org string) string {
	if !strings.Contains(org, "://") {
		org = "https://dev.azure.com/" + org
	}
	return fmt.Sprintf("az devops login --organization %s", strings.TrimSuffix(org, "/"))
}
