package workday

import (
	"reflect"
	"testing"
)

func TestIDsInText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []int
	}{
		{"plain", "Fix crash #1234", []int{1234}},
		{"AB prefix", "AB#5678: add field", []int{5678}},
		{"several, deduplicated", "#12 and AB#34 and #12", []int{12, 34}},
		{"line start", "#99 first", []int{99}},
		{"too short", "issue #1", nil},
		{"too long", "#12345678", nil},
		{"glued to word", "see abc#123", nil},
		{"glued AB to word", "xAB#123", nil},
		{"url fragment", "https://x/y/#123", nil},
		{"after paren", "(#456)", []int{456}},
		{"followed by letters", "#123abc", nil},
		{"unicode letter before", "über#123", nil},
		{"none", "refactor", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IDsInText(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IDsInText(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBranchIDs(t *testing.T) {
	tests := []struct {
		in   string
		want []int
	}{
		{"refs/heads/feature/12345-add-thing", []int{12345}},
		{"task/-16281", []int{16281}},
		{"fix/my-bug-1234", []int{1234}},
		{"refs/heads/release/2026.10", []int{2026}}, // false positive by design
		{"feature/12-short", nil},
		{"feature/12345678-long", nil},
		{"a/123/b/456", []int{123, 456}},
		{"main", nil},
	}
	for _, tt := range tests {
		if got := BranchIDs(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("BranchIDs(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestCheckoutTarget(t *testing.T) {
	tests := []struct {
		subject string
		want    string
		ok      bool
	}{
		{"checkout: moving from main to feature/1234-x", "feature/1234-x", true},
		{"checkout: moving from a to b", "b", true},
		{"checkout: moving from main to 1a2b3c4d5e6f", "", false}, // detached SHA
		{"commit: fix #123", "", false},
		{"pull: Fast-forward", "", false},
	}
	for _, tt := range tests {
		got, ok := checkoutTarget(tt.subject)
		if got != tt.want || ok != tt.ok {
			t.Errorf("checkoutTarget(%q) = %q, %v; want %q, %v", tt.subject, got, ok, tt.want, tt.ok)
		}
	}
}
