package main

import (
	"strings"
	"unicode"

	optionsv1 "github.com/funinthecloud/protosource/gen/options/v1"
	pgs "github.com/lyft/protoc-gen-star/v2"
)

// TSEnumDisplayValue is one row in the generated TS Record.
type TSEnumDisplayValue struct {
	MemberName string // TS enum member, e.g. "ACTIVE" (protoc-gen-es strips the enum-name prefix from values)
	Display    string // human-readable label, e.g. "Active"
}

// TSEnumDisplay is the data the enums.tstext template needs to emit one
// `export const XDisplay: Record<X, string> = {…}` block.
type TSEnumDisplay struct {
	TypeName string // TS enum type name, e.g. "State"
	Values   []TSEnumDisplayValue
}

func (p *ProtosourceModule) enumOptions(e pgs.Enum) *optionsv1.EnumOptions {
	var opts optionsv1.EnumOptions
	ok, err := e.Extension(optionsv1.E_ProtosourceEnum, &opts)
	if err != nil || !ok {
		return nil
	}
	return &opts
}

func (p *ProtosourceModule) enumDisplayEnabled(e pgs.Enum) bool {
	opts := p.enumOptions(e)
	if opts == nil {
		return true
	}
	return !opts.GetDisplayNamesDisabled()
}

// tsEnumDisplays returns one TSEnumDisplay per enum in the file (top-level
// and nested) for which display-name generation is enabled. Aliased values
// keep the first declared name and drop duplicates.
//
// The TS type name uses `p.ctx.Name(enum)` which produces the same
// underscore-joined name protoc-gen-es emits for nested enums.
func (p *ProtosourceModule) tsEnumDisplays(f pgs.File) []TSEnumDisplay {
	var out []TSEnumDisplay
	for _, e := range f.AllEnums() {
		if !p.enumDisplayEnabled(e) {
			continue
		}
		valueNames := make([]string, 0, len(e.Values()))
		for _, v := range e.Values() {
			valueNames = append(valueNames, v.Name().String())
		}
		stripped := stripEnumValuePrefix(e.Name().String(), valueNames)

		d := TSEnumDisplay{TypeName: p.ctx.Name(e).String()}
		seen := map[int32]bool{}
		for i, v := range e.Values() {
			if seen[v.Value()] {
				continue
			}
			seen[v.Value()] = true
			member := stripped[i]
			d.Values = append(d.Values, TSEnumDisplayValue{
				MemberName: member,
				Display:    enumDisplayLabel(member),
			})
		}
		out = append(out, d)
	}
	return out
}

// stripEnumValuePrefix returns each value name with the enum-name prefix
// removed (e.g. "STATE_ACTIVE" → "ACTIVE" when the enum is named "State").
// The prefix is only stripped if every value carries it — otherwise the
// names are returned unchanged. This mirrors what protoc-gen-es does when
// it emits enum members, so the generated TS keys index the enum
// correctly: `State.ACTIVE`, not `State.STATE_ACTIVE`.
func stripEnumValuePrefix(enumName string, valueNames []string) []string {
	prefix := screamingSnake(enumName) + "_"
	stripPrefix := len(valueNames) > 0
	for _, v := range valueNames {
		if !strings.HasPrefix(v, prefix) {
			stripPrefix = false
			break
		}
	}
	out := make([]string, len(valueNames))
	for i, v := range valueNames {
		if stripPrefix {
			out[i] = strings.TrimPrefix(v, prefix)
		} else {
			out[i] = v
		}
	}
	return out
}

// fileHasDisplayableEnum reports whether the file would emit any display map.
// Used by the template router to skip writing an empty enums.ts file.
func (p *ProtosourceModule) fileHasDisplayableEnum(f pgs.File) bool {
	for _, e := range f.AllEnums() {
		if p.enumDisplayEnabled(e) {
			return true
		}
	}
	return false
}

// screamingSnake converts a PascalCase / camelCase identifier to
// SCREAMING_SNAKE_CASE. Mirrors the Go plugin's implementation — keep both
// in sync.
func screamingSnake(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			next := rune(0)
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				(unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next)) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// enumDisplayLabel converts a SCREAMING_SNAKE value name (after prefix
// stripping) to a Title-Cased display label. Mirrors the Go plugin's
// implementation — keep both in sync.
func enumDisplayLabel(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		lower := strings.ToLower(p)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}
