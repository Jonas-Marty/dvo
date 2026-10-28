package workday

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Go's regexp has no lookbehind, so the "not preceded by a word character or
// slash" part of the text pattern is checked by hand in IDsInText.
var textIDRe = regexp.MustCompile(`(?:AB)?#(\d{2,7})\b`)

// IDsInText returns the work item numbers referenced as "#123" or "AB#123" in
// a commit message or pull request title. Matches glued to a preceding word
// character or slash (URLs, "abc#123") are ignored.
func IDsInText(s string) []int {
	var ids []int
	for _, m := range textIDRe.FindAllStringSubmatchIndex(s, -1) {
		if m[0] > 0 {
			r, _ := utf8.DecodeLastRuneInString(s[:m[0]])
			if r == '/' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
		}
		if n, err := strconv.Atoi(s[m[2]:m[3]]); err == nil {
			ids = append(ids, n)
		}
	}
	return dedupe(ids)
}

// BranchIDs returns every run of 3 to 7 digits in a branch name. It will find
// plenty of numbers that are not work items; the detail lookup discards those.
func BranchIDs(ref string) []int {
	name := strings.TrimPrefix(ref, "refs/heads/")
	var ids []int
	for i := 0; i < len(name); {
		if name[i] < '0' || name[i] > '9' {
			i++
			continue
		}
		j := i
		for j < len(name) && name[j] >= '0' && name[j] <= '9' {
			j++
		}
		if n := j - i; n >= 3 && n <= 7 {
			v, _ := strconv.Atoi(name[i:j])
			ids = append(ids, v)
		}
		i = j
	}
	return dedupe(ids)
}

func dedupe(ids []int) []int {
	seen := make(map[int]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
