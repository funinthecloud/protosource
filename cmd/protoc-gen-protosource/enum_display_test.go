package main

import "testing"

func TestScreamingSnake(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"State", "STATE"},
		{"LogLevel", "LOG_LEVEL"},
		{"HTTPHeader", "HTTP_HEADER"},
		{"Kind", "KIND"},
		{"OAuth2Token", "O_AUTH2_TOKEN"},
		{"", ""},
	}
	for _, c := range cases {
		got := screamingSnake(c.in)
		if got != c.want {
			t.Errorf("screamingSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnumDisplayLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"UNSPECIFIED", "Unspecified"},
		{"ACTIVE", "Active"},
		{"NOT_FOUND", "Not Found"},
		{"OK", "Ok"},
		{"FAIL", "Fail"},
		{"SELF", "Self"},
		{"WARN", "Warn"},
		{"", ""},
		{"A_B_C", "A B C"},
	}
	for _, c := range cases {
		got := enumDisplayLabel(c.in)
		if got != c.want {
			t.Errorf("enumDisplayLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
