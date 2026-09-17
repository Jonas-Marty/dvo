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

// CompletedPRMergeTips queries the most recent `top` completed PRs and returns a map of
// source branch name (with the "refs/heads/" prefix stripped) to the source-branch commits
// that were actually merged. Use this to detect squash-merged branches that git cannot
// identify as merged — comparing against the merged commit rather than matching on the
// branch name alone, so commits pushed after the PR completed are not mistaken for merged
// work. A branch may be the source of several completed PRs, so every tip is kept.
func (c *Context) CompletedPRMergeTips(top int) (map[string][]string, error) {
	out, err := exec.Command("az", "repos", "pr", "list",
		"--status", "completed",
		"--top", strconv.Itoa(top),
		"--org", c.OrgURL(),
		"--project", c.Project,
		"--repository", c.Repo,
		"--query", "[].{ref:sourceRefName, tip:lastMergeSourceCommit.commitId}",
		"--output", "json",
	).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az repos pr list failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("az repos pr list failed: %w", err)
	}
	var entries []struct {
		Ref string `json:"ref"`
		Tip string `json:"tip"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("could not parse PR list response: %w", err)
	}
	result := make(map[string][]string, len(entries))
	for _, e := range entries {
		if e.Tip == "" {
			continue
		}
		// "refs/heads/fix/my-bug-1234" → "fix/my-bug-1234"
		name := strings.TrimPrefix(e.Ref, "refs/heads/")
		result[name] = append(result[name], e.Tip)
	}
	return result, nil
}
