package main

import "testing"

func TestScreamingSnakeTS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"State", "STATE"},
		{"LogLevel", "LOG_LEVEL"},
		{"HTTPHeader", "HTTP_HEADER"},
		{"Kind", "KIND"},
		{"", ""},
	}
	for _, c := range cases {
		got := screamingSnake(c.in)
		if got != c.want {
			t.Errorf("screamingSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripEnumValuePrefixTS(t *testing.T) {
	cases := []struct {
		name   string
		enum   string
		values []string
		want   []string
	}{
		{
			name:   "common prefix stripped",
			enum:   "State",
			values: []string{"STATE_UNSPECIFIED", "STATE_ACTIVE", "STATE_DELETED"},
			want:   []string{"UNSPECIFIED", "ACTIVE", "DELETED"},
		},
		{
			name:   "no common prefix left alone",
			enum:   "Status",
			values: []string{"OK", "FAIL"},
			want:   []string{"OK", "FAIL"},
		},
		{
			name:   "camel-case enum name converted to screaming snake",
			enum:   "LogLevel",
			values: []string{"LOG_LEVEL_DEBUG", "LOG_LEVEL_INFO"},
			want:   []string{"DEBUG", "INFO"},
		},
		{
			name:   "partial prefix coverage disables stripping",
			enum:   "State",
			values: []string{"STATE_ACTIVE", "DELETED"},
			want:   []string{"STATE_ACTIVE", "DELETED"},
		},
		{
			name:   "empty input",
			enum:   "State",
			values: nil,
			want:   []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripEnumValuePrefix(c.enum, c.values)
			if len(got) != len(c.want) {
				t.Fatalf("stripEnumValuePrefix(%q, %v) length = %d, want %d", c.enum, c.values, len(got), len(c.want))
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("stripEnumValuePrefix(%q, %v)[%d] = %q, want %q", c.enum, c.values, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestEnumDisplayLabelTS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"UNSPECIFIED", "Unspecified"},
		{"NOT_FOUND", "Not Found"},
		{"OK", "Ok"},
		{"", ""},
	}
	for _, c := range cases {
		got := enumDisplayLabel(c.in)
		if got != c.want {
			t.Errorf("enumDisplayLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
