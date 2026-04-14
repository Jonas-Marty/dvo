package devops

import (
	"fmt"
	"net/url"
	"regexp"
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
	if strings.HasPrefix(remoteURL, "https://dev.azure.com/") {
		u, err := url.Parse(remoteURL)
		if err != nil {
			return nil, fmt.Errorf("could not parse remote URL: %w", err)
		}
		// Path: /<org>/<project>/_git/<repo>
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 4)
		if len(parts) < 4 || parts[2] != "_git" {
			return nil, fmt.Errorf("unexpected Azure DevOps URL path: %s", u.Path)
		}
		project, _ := url.PathUnescape(parts[1])
		repo, _ := url.PathUnescape(parts[3])
		return &Context{Org: parts[0], Project: project, Repo: repo}, nil
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
	return "https://dev.azure.com/" + c.Org
}

// RepoURL returns the web URL for the repository.
func (c *Context) RepoURL() string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", c.Org, c.Project, c.Repo)
}

// WorkItemURL returns the edit URL for a work item.
func (c *Context) WorkItemURL(id string) string {
	return fmt.Sprintf("https://dev.azure.com/%s/_workitems/edit/%s", c.Org, id)
}

// PRURL returns the web URL for a specific pull request.
func (c *Context) PRURL(prID int) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d", c.Org, c.Project, c.Repo, prID)
}

// CommitDiffURL returns the Azure DevOps branchCompare URL for two full SHAs.
func (c *Context) CommitDiffURL(baseSHA, compareSHA string) string {
	return fmt.Sprintf(
		"https://dev.azure.com/%s/%s/_git/%s/branchCompare?baseVersion=GC%s&targetVersion=GC%s",
		c.Org, c.Project, c.Repo, baseSHA, compareSHA,
	)
}
