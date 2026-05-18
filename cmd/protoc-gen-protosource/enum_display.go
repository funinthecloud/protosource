package main

import (
	"strings"
	"unicode"

	optionsv1 "github.com/funinthecloud/protosource/options/v1"
	pgs "github.com/lyft/protoc-gen-star/v2"
)

// EnumDisplayValue is one row in a generated display map.
type EnumDisplayValue struct {
	GoName  string // Go constant name, e.g. "State_STATE_ACTIVE"
	Display string // Human-readable label, e.g. "Active"
}

// EnumDisplay is the data the protosource.gotext template needs to emit one
// `var X_Display = map[X]string{…}` block.
type EnumDisplay struct {
	GoTypeName string // Go type name, e.g. "State" (or "Order_LineKind" for nested)
	Values     []EnumDisplayValue
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

// enumDisplays returns one EnumDisplay per enum in the file (top-level and
// nested) for which display-name generation is enabled. Aliased values keep
// the first declared name and drop duplicates.
func (p *ProtosourceModule) enumDisplays(f pgs.File) []EnumDisplay {
	var out []EnumDisplay
	for _, e := range f.AllEnums() {
		if !p.enumDisplayEnabled(e) {
			continue
		}
		out = append(out, p.buildEnumDisplay(e))
	}
	return out
}

func (p *ProtosourceModule) buildEnumDisplay(e pgs.Enum) EnumDisplay {
	prefix := screamingSnake(e.Name().String()) + "_"
	stripPrefix := true
	for _, v := range e.Values() {
		if !strings.HasPrefix(v.Name().String(), prefix) {
			stripPrefix = false
			break
		}
	}

	d := EnumDisplay{GoTypeName: p.ctx.Name(e).String()}
	seen := map[int32]bool{}
	for _, v := range e.Values() {
		if seen[v.Value()] {
			continue // aliased: first declared name wins
		}
		seen[v.Value()] = true
		raw := v.Name().String()
		if stripPrefix {
			raw = strings.TrimPrefix(raw, prefix)
		}
		d.Values = append(d.Values, EnumDisplayValue{
			GoName:  p.ctx.Name(v).String(),
			Display: enumDisplayLabel(raw),
		})
	}
	return d
}

// TSEnumDisplay is the same data shaped for the TS template (which keys the
// Record by short TS enum constant — e.g. "ACTIVE", not "State_STATE_ACTIVE").
type TSEnumDisplay struct {
	TSTypeName string             // TS enum type name, e.g. "State"
	Values     []TSEnumDisplayValue
}

type TSEnumDisplayValue struct {
	TSName  string // TS enum member name, e.g. "STATE_ACTIVE" (protoc-gen-es preserves proto value names)
	Display string
}

// screamingSnake converts a PascalCase / camelCase identifier to
// SCREAMING_SNAKE_CASE. "LogLevel" → "LOG_LEVEL"; "State" → "STATE";
// "HTTPHeader" → "HTTP_HEADER".
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
			// boundary before an uppercase that follows a lowercase/digit,
			// or before an uppercase that begins a lowercase run (HTTPHeader → HTTP_Header).
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
// stripping) to a Title-Cased display label. "NOT_FOUND" → "Not Found";
// "OK" → "Ok"; "" (degenerate) → "".
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
