package main

import (
	"strings"
	"unicode"

	optionsv1 "github.com/funinthecloud/protosource/options/v1"
	pgs "github.com/lyft/protoc-gen-star/v2"
)

// TSEnumDisplayValue is one row in the generated TS Record.
type TSEnumDisplayValue struct {
	MemberName string // TS enum member, e.g. "STATE_ACTIVE" (protoc-gen-es preserves proto value names)
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
		prefix := screamingSnake(e.Name().String()) + "_"
		stripPrefix := true
		for _, v := range e.Values() {
			if !strings.HasPrefix(v.Name().String(), prefix) {
				stripPrefix = false
				break
			}
		}

		d := TSEnumDisplay{TypeName: p.ctx.Name(e).String()}
		seen := map[int32]bool{}
		for _, v := range e.Values() {
			if seen[v.Value()] {
				continue
			}
			seen[v.Value()] = true
			raw := v.Name().String()
			if stripPrefix {
				raw = strings.TrimPrefix(raw, prefix)
			}
			d.Values = append(d.Values, TSEnumDisplayValue{
				MemberName: v.Name().String(),
				Display:    enumDisplayLabel(raw),
			})
		}
		out = append(out, d)
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
