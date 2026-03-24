package devops

import (
	"fmt"
	"net/url"
	"regexp"

	"github.com/Jonas-Marty/ad-cli/internal/git"
)

// Context holds the three coordinates of an Azure DevOps repository.
type Context struct {
	Org     string
	Project string
	Repo    string
}

var (
	httpsRe = regexp.MustCompile(`https://dev\.azure\.com/([^/]+)/([^/]+)/_git/([^/]+)`)
	sshRe   = regexp.MustCompile(`git@ssh\.dev\.azure\.com:v3/([^/]+)/([^/]+)/([^/]+)`)
)

// FromCurrentRepo parses the Azure DevOps context from the current repo's origin remote.
func FromCurrentRepo() (*Context, error) {
	remoteURL, err := git.GetRemoteURL()
	if err != nil {
		return nil, err
	}

	if m := httpsRe.FindStringSubmatch(remoteURL); m != nil {
		project, _ := url.PathUnescape(m[2])
		return &Context{Org: m[1], Project: project, Repo: m[3]}, nil
	}
	if m := sshRe.FindStringSubmatch(remoteURL); m != nil {
		return &Context{Org: m[1], Project: m[2], Repo: m[3]}, nil
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
