package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/Jonas-Marty/ad-cli/internal/cache"
	"github.com/Jonas-Marty/ad-cli/internal/devops"
	"github.com/Jonas-Marty/ad-cli/pkg/ui"
	"github.com/spf13/cobra"
)

var cacheRefreshCmd = &cobra.Command{
	Use:     "refresh",
	Aliases: []string{"r"},
	Short:   "Fetch all Azure DevOps users and store them locally",
	Long: `Calls az devops user list (paginated) and saves the results to
~/.config/adg/cache/<org>/users.json

The cached users are used by 'adg pr create -r <alias>' to resolve short
names to full email addresses, and by shell completion to tab-complete
reviewer names.

The org is read from the current repository's remote URL.`,
	Example: `  adg cache refresh`,
	RunE:    runCacheRefresh,
}

func init() {
	cacheCmd.AddCommand(cacheRefreshCmd)
}

// azUserListResponse mirrors the top-level JSON returned by az devops user list.
type azUserListResponse struct {
	Items      []azUserItem `json:"items"`
	TotalCount int          `json:"totalCount"`
}

type azUserItem struct {
	User azUserDetail `json:"user"`
}

type azUserDetail struct {
	MailAddress string `json:"mailAddress"`
	DisplayName string `json:"displayName"`
}

func runCacheRefresh(_ *cobra.Command, _ []string) error {
	ctx, err := devops.FromCurrentRepo()
	if err != nil {
		return err
	}

	org := ctx.Org
	orgURL := ctx.OrgURL()

	var users []cache.User
	const pageSize = 100

	err = ui.RunSpinner(
		fmt.Sprintf("Fetching users for org '%s'…", org),
		func() error {
			skip := 0
			for {
				out, err := exec.Command("az", "devops", "user", "list",
					"--org", orgURL,
					"--top", fmt.Sprintf("%d", pageSize),
					"--skip", fmt.Sprintf("%d", skip),
					"-o", "json",
				).Output()
				if err != nil {
					if ee, ok := err.(*exec.ExitError); ok {
						return fmt.Errorf("az devops user list failed:\n%s", string(ee.Stderr))
					}
					return err
				}

				var resp azUserListResponse
				if err := json.Unmarshal(out, &resp); err != nil {
					return fmt.Errorf("could not parse response: %w", err)
				}

				for _, item := range resp.Items {
					email := item.User.MailAddress
					if email == "" {
						continue
					}
					users = append(users, cache.User{
						Alias:       cache.AliasFromEmail(email),
						Email:       email,
						DisplayName: item.User.DisplayName,
					})
				}

				skip += len(resp.Items)
				if skip >= resp.TotalCount || len(resp.Items) == 0 {
					break
				}
			}
			return nil
		},
	)
	if err != nil {
		return err
	}

	if err := cache.Save(org, users); err != nil {
		return fmt.Errorf("could not save cache: %w", err)
	}

	ui.Success.Printf("Cached %d users for org '%s'\n", len(users), org)
	ui.Info.Println("Aliases are now available for tab completion and -r shorthand in 'adg pr create'.")
	return nil
}
