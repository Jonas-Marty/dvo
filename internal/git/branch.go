package git

import "regexp"

// branchWIRe matches branch names following the convention:
//
//	<prefix>/<slug>-<id>   (e.g. fix/my-bug-1234, feat/add-login-5678)
//
// The ID is always the last numeric segment at the end of the branch name.
var branchWIRe = regexp.MustCompile(`^(?:fix|feat|task)/.+-(\d+)$`)

// ExtractWorkItemFromBranch extracts the numeric work item ID from a branch
// name that follows the convention: fix/<slug>-<id> / feat/<slug>-<id> / task/<slug>-<id>.
// Returns an empty string if the branch name does not match.
func ExtractWorkItemFromBranch(branch string) string {
	if m := branchWIRe.FindStringSubmatch(branch); m != nil {
		return m[1]
	}
	return ""
}
