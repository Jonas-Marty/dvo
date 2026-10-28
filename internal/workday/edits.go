package workday

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// revision is one entry of a work item's history. Every revision carries the
// complete set of fields, which is what makes it possible to tell afterwards
// what an edit changed.
type revision struct {
	Fields map[string]json.RawMessage `json:"fields"`
}

func (r revision) changedBy() identityRef {
	var id identityRef
	_ = json.Unmarshal(r.Fields["System.ChangedBy"], &id)
	return id
}

func (r revision) changedDate() time.Time {
	var t time.Time
	_ = json.Unmarshal(r.Fields["System.ChangedDate"], &t)
	return t
}

// fieldText returns a field as plain text: strings unquoted, anything else as
// its JSON form, a missing field as "".
func fieldText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
}

// editKinds says in a few words what cur changed compared to prev. prev is nil
// for the first revision, which is the creation of the item.
//
// Known fields get a plain name; any other changed field is listed by its own
// name so nothing is hidden, apart from bookkeeping fields that change with
// every edit (revision number, dates, board columns and the like).
func editKinds(prev, cur map[string]json.RawMessage) []string {
	if prev == nil {
		return []string{"created"}
	}
	var kinds []string
	add := func(k string) {
		for _, have := range kinds {
			if have == k {
				return
			}
		}
		kinds = append(kinds, k)
	}

	for name, raw := range cur {
		old := prev[name]
		if string(old) == string(raw) {
			continue
		}
		switch {
		case name == "System.State":
			add("state " + fieldText(old) + "→" + fieldText(raw))
		case name == "System.History":
			// Set only on the revision that added the comment.
			if strings.TrimSpace(fieldText(raw)) != "" {
				add("comment")
			}
		case isBookkeeping(name):
		default:
			add(friendlyField(name))
		}
	}
	// A field that was cleared disappears from the revision entirely.
	for name, old := range prev {
		if _, ok := cur[name]; !ok && !isBookkeeping(name) && name != "System.History" && string(old) != "null" {
			add(friendlyField(name))
		}
	}
	sortKinds(kinds)
	return kinds
}

// sortKinds keeps the output stable (map iteration order is random) and puts
// the state change and comments first.
func sortKinds(kinds []string) {
	rank := func(k string) int {
		switch {
		case strings.HasPrefix(k, "state "):
			return 0
		case k == "comment":
			return 1
		}
		return 2
	}
	sort.SliceStable(kinds, func(a, b int) bool {
		if ra, rb := rank(kinds[a]), rank(kinds[b]); ra != rb {
			return ra < rb
		}
		return kinds[a] < kinds[b]
	})
}

var friendlyNames = map[string]string{
	"System.Title":                               "title",
	"System.Description":                         "description",
	"System.AssignedTo":                          "assignee",
	"System.AreaPath":                            "area",
	"System.IterationPath":                       "iteration",
	"System.Tags":                                "tags",
	"System.Parent":                              "parent",
	"System.RelatedLinkCount":                    "links",
	"System.ExternalLinkCount":                   "links",
	"System.HyperLinkCount":                      "links",
	"System.AttachedFileCount":                   "attachments",
	"Microsoft.VSTS.Scheduling.RemainingWork":    "remaining work",
	"Microsoft.VSTS.Scheduling.CompletedWork":    "completed work",
	"Microsoft.VSTS.Scheduling.OriginalEstimate": "estimate",
	"Microsoft.VSTS.Scheduling.StoryPoints":      "story points",
	"Microsoft.VSTS.Scheduling.Effort":           "effort",
	"Microsoft.VSTS.Common.Priority":             "priority",
	"Microsoft.VSTS.Common.Severity":             "severity",
	"Microsoft.VSTS.TCM.ReproSteps":              "repro steps",
	"Microsoft.VSTS.Common.AcceptanceCriteria":   "acceptance criteria",
}

func friendlyField(name string) string {
	if n, ok := friendlyNames[name]; ok {
		return n
	}
	// "Custom.MyField" -> "MyField"
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// isBookkeeping reports fields the service rewrites with every edit or as a
// side effect of another one, which say nothing about what the person did.
func isBookkeeping(name string) bool {
	switch name {
	case "System.Rev", "System.ChangedDate", "System.ChangedBy", "System.RevisedDate",
		"System.AuthorizedDate", "System.AuthorizedAs", "System.Watermark", "System.PersonId",
		"System.CommentCount", "System.Reason", "System.IsDeleted",
		"Microsoft.VSTS.Common.StateChangeDate", "Microsoft.VSTS.Common.StackRank",
		"Microsoft.VSTS.Common.ActivatedDate", "Microsoft.VSTS.Common.ActivatedBy",
		"Microsoft.VSTS.Common.ResolvedDate", "Microsoft.VSTS.Common.ResolvedBy",
		"Microsoft.VSTS.Common.ClosedDate", "Microsoft.VSTS.Common.ClosedBy",
		"Microsoft.VSTS.Common.ResolvedReason":
		return true
	}
	return strings.HasPrefix(name, "System.Board") || strings.Contains(name, "_Kanban.")
}

// describeEdits summarizes the revisions that mine selects, as the text
// "edited 3x: state Active→Resolved, comment", together with their times.
func describeEdits(revs []revision, mine func(revision) bool) (string, []time.Time) {
	var times []time.Time
	var kinds []string
	seen := map[string]bool{}
	for i, r := range revs {
		if !mine(r) {
			continue
		}
		times = append(times, r.changedDate())
		var prev map[string]json.RawMessage
		if i > 0 {
			prev = revs[i-1].Fields
		}
		for _, k := range editKinds(prev, r.Fields) {
			if !seen[k] {
				seen[k] = true
				kinds = append(kinds, k)
			}
		}
	}
	if len(times) == 0 {
		return "", nil
	}
	reason := "edited " + strconv.Itoa(len(times)) + "x"
	if len(kinds) > 0 {
		reason += ": " + strings.Join(kinds, ", ")
	}
	return reason, times
}
