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
