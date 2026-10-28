package cmd

import "testing"

func TestParseSeparator(t *testing.T) {
	tests := []struct {
		in      string
		want    rune
		wantErr bool
	}{
		{";", ';', false},
		{",", ',', false},
		{"tab", '\t', false},
		{"TAB", '\t', false},
		{`\t`, '\t', false},
		{"", 0, true},
		{";;", 0, true},
		{`"`, 0, true},
	}
	for _, tt := range tests {
		got, err := parseSeparator(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("parseSeparator(%q) = %q, %v; want %q, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestOrgName(t *testing.T) {
	tests := map[string]string{
		"myorg":                                  "myorg",
		" myorg ":                                "myorg",
		"https://dev.azure.com/myorg":            "myorg",
		"https://dev.azure.com/myorg/":           "myorg",
		"https://dev.azure.com/myorg/proj/_git/": "myorg",
	}
	for in, want := range tests {
		if got := orgName(in); got != want {
			t.Errorf("orgName(%q) = %q, want %q", in, got, want)
		}
	}
}
