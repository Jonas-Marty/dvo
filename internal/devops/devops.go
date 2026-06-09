package devops

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Jonas-Marty/ad-cli/internal/git"
)

// Context holds the three coordinates of an Azure DevOps repository.
type Context struct {
	Org     string
	Project string
	Repo    string
}

var sshRe = regexp.MustCompile(`git@ssh\.dev\.azure\.com:v3/([^/]+)/([^/]+)/([^/]+)`)

// FromCurrentRepo parses the Azure DevOps context from the current repo's origin remote.
func FromCurrentRepo() (*Context, error) {
	remoteURL, err := git.GetRemoteURL()
	if err != nil {
		return nil, err
	}

	// HTTPS: use url.Parse so percent-encoded segments (e.g. %20) are decoded automatically.
	// Handles both https://dev.azure.com/... and https://<user>@dev.azure.com/... forms.
	if strings.HasPrefix(remoteURL, "https://") {
		u, err := url.Parse(remoteURL)
		if err != nil {
			return nil, fmt.Errorf("could not parse remote URL: %w", err)
		}
		if u.Hostname() == "dev.azure.com" {
			// Path: /<org>/<project>/_git/<repo>
			parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 4)
			if len(parts) < 4 || parts[2] != "_git" {
				return nil, fmt.Errorf("unexpected Azure DevOps URL path: %s", u.Path)
			}
			project, _ := url.PathUnescape(parts[1])
			repo, _ := url.PathUnescape(parts[3])
			return &Context{Org: parts[0], Project: project, Repo: repo}, nil
		}
	}

	if m := sshRe.FindStringSubmatch(remoteURL); m != nil {
		project, _ := url.PathUnescape(m[2])
		repo, _ := url.PathUnescape(m[3])
		return &Context{Org: m[1], Project: project, Repo: repo}, nil
	}

	return nil, fmt.Errorf("remote URL does not look like an Azure DevOps URL: %s", remoteURL)
}

// OrgURL returns https://dev.azure.com/<org>/
func (c *Context) OrgURL() string {
	return "https://dev.azure.com/" + url.PathEscape(c.Org)
}

// RepoURL returns the web URL for the repository.
func (c *Context) RepoURL() string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s",
		url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(c.Repo))
}

// WorkItemURL returns the edit URL for a work item.
func (c *Context) WorkItemURL(id string) string {
	return fmt.Sprintf("https://dev.azure.com/%s/_workitems/edit/%s",
		url.PathEscape(c.Org), url.PathEscape(id))
}

// PRURL returns the web URL for a specific pull request.
func (c *Context) PRURL(prID int) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d",
		url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(c.Repo), prID)
}

// CommitDiffURL returns the Azure DevOps branchCompare URL for two full SHAs.
func (c *Context) CommitDiffURL(baseSHA, compareSHA string) string {
	return fmt.Sprintf(
		"https://dev.azure.com/%s/%s/_git/%s/branchCompare?baseVersion=%s&targetVersion=%s",
		url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(c.Repo),
		url.QueryEscape("GC"+baseSHA), url.QueryEscape("GC"+compareSHA),
	)
}

// CompletedPRBranches queries the most recent `top` completed PRs and returns
// a set of source branch names (with the "refs/heads/" prefix stripped).
// Use this to detect squash-merged branches that git cannot identify as merged.
func (c *Context) CompletedPRBranches(top int) (map[string]bool, error) {
	out, err := exec.Command("az", "repos", "pr", "list",
		"--status", "completed",
		"--top", strconv.Itoa(top),
		"--org", c.OrgURL(),
		"--project", c.Project,
		"--repository", c.Repo,
		"--query", "[].sourceRefName",
		"--output", "json",
	).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az repos pr list failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("az repos pr list failed: %w", err)
	}
	var refs []string
	if err := json.Unmarshal(out, &refs); err != nil {
		return nil, fmt.Errorf("could not parse PR list response: %w", err)
	}
	result := make(map[string]bool, len(refs))
	for _, ref := range refs {
		// "refs/heads/fix/my-bug-1234" → "fix/my-bug-1234"
		result[strings.TrimPrefix(ref, "refs/heads/")] = true
	}
	return result, nil
}
