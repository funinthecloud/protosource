package main

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/template"
	"unicode"

	pgs "github.com/lyft/protoc-gen-star/v2"
	pgsgo "github.com/lyft/protoc-gen-star/v2/lang/go"
	optionsv1 "github.com/funinthecloud/protosource/options/v1"
)

// ProtosourceModule generates TypeScript client code for protosource domains.
type ProtosourceModule struct {
	*pgs.ModuleBase
	ctx    pgsgo.Context
	tpls   []*template.Template
	params pgs.Parameters
}

func Protosourceify() *ProtosourceModule { return &ProtosourceModule{ModuleBase: &pgs.ModuleBase{}} }

func (p *ProtosourceModule) InitContext(c pgs.BuildContext) {
	p.ModuleBase.InitContext(c)
	p.params = c.Parameters()
	p.ctx = pgsgo.InitContext(p.params)

	if err := fs.WalkDir(embeddedTemplates, "content", p.WalkTemplates); err != nil {
		panic(err)
	}
}

func (p *ProtosourceModule) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"isAggregate":        p.isAggregate,
		"isCommand":          p.isCommand,
		"isEvent":            p.isEvent,
		"isProjection":       p.isProjection,
		"isSnapshot":         p.isSnapshot,
		"commandLifecycle":   p.commandLifecycle,
		"clientCommandFields": clientCommandFields,
		"aggregateForFile":   p.aggregateForFile,
		"routePrefix":        p.routePrefix,
		"opaqueUsedGSIs":     p.opaqueUsedGSIs,
		"opaquePKFields":     p.opaquePKFields,
		"opaqueGSISKFields":  p.opaqueGSISKFields,
		"opaqueFieldNameLower": opaqueFieldNameLower,
		"queryRoutePath":     queryRoutePath,
		"unexport":           unexport,
		"lower":              strings.ToLower,
		"tsType":             tsType,
		"tsFieldName":        tsFieldName,
		"tsQueryFormatExpr":  tsQueryFormatExpr,
		"protoFileName":      protoFileName,
		"name":               p.ctx.Name,
		"commandEmbeddedTypes": func(f pgs.File) []string { return commandEmbeddedTypes(f, p.isCommand) },
	}
}

func (p *ProtosourceModule) WalkTemplates(path string, d fs.DirEntry, err error) error {
	if d.IsDir() {
		return nil
	}
	tpl := template.New(d.Name()).Funcs(p.templateFuncs())
	body, err := fs.ReadFile(embeddedTemplates, path)
	if err != nil {
		return err
	}
	tpl2, err := tpl.Parse(string(body))
	if err != nil {
		return err
	}
	p.tpls = append(p.tpls, tpl2)
	return nil
}

func (p *ProtosourceModule) Name() string { return "protosource-ts" }

func (p *ProtosourceModule) Execute(targets map[string]pgs.File, _ map[string]pgs.Package) []pgs.Artifact {
	for _, t := range targets {
		p.generate(t)
	}
	return p.Artifacts()
}

func (p *ProtosourceModule) generate(f pgs.File) {
	if len(f.Messages()) == 0 {
		return
	}
	var fileOpts optionsv1.FileOptions
	ok, err := f.Extension(optionsv1.E_ProtosourceFile, &fileOpts)
	if err != nil || !ok || !fileOpts.GetEnabled() {
		return
	}

	for _, v := range p.tpls {
		outPath := p.outputPath(f, v)
		p.AddGeneratorTemplateFile(outPath, v, f)
	}
}

// outputPath computes the output file path for a proto file.
// Produces {proto_file_stem}.protosource.client.ts with module prefix stripping.
func (p *ProtosourceModule) outputPath(f pgs.File, _ *template.Template) string {
	base := strings.TrimSuffix(f.InputPath().Base(), ".proto") + ".protosource.client.ts"
	importPath := p.ctx.ImportPath(f).String()
	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		rel = strings.TrimPrefix(rel, "/")
		return rel + "/" + base
	}
	return base
}

// ── Annotation reading (copied subset from protoc-gen-protosource) ────────

func (p *ProtosourceModule) messageOptions(m pgs.Message) *optionsv1.MessageOptions {
	var opts optionsv1.MessageOptions
	ok, err := m.Extension(optionsv1.E_ProtosourceMessageType, &opts)
	if err != nil || !ok {
		return nil
	}
	return &opts
}

func (p *ProtosourceModule) isAggregate(m pgs.Message) bool {
	opts := p.messageOptions(m)
	return opts != nil && opts.GetAggregate() != nil
}

func (p *ProtosourceModule) isCommand(m pgs.Message) bool {
	opts := p.messageOptions(m)
	return opts != nil && opts.GetCommand() != nil
}

func (p *ProtosourceModule) isEvent(m pgs.Message) bool {
	opts := p.messageOptions(m)
	return opts != nil && opts.GetEvent() != nil
}

func (p *ProtosourceModule) isProjection(m pgs.Message) bool {
	opts := p.messageOptions(m)
	return opts != nil && opts.GetProjection() != nil
}

func (p *ProtosourceModule) isSnapshot(m pgs.Message) bool {
	opts := p.messageOptions(m)
	return opts != nil && opts.GetSnapshot() != nil
}

func (p *ProtosourceModule) commandLifecycle(m pgs.Message) string {
	opts := p.messageOptions(m)
	if opts == nil || opts.GetCommand() == nil {
		return "UNSPECIFIED"
	}
	switch opts.GetCommand().GetLifecycle() {
	case optionsv1.CommandLifecycle_COMMAND_LIFECYCLE_CREATION:
		return "CREATION"
	case optionsv1.CommandLifecycle_COMMAND_LIFECYCLE_MUTATION:
		return "MUTATION"
	default:
		return "UNSPECIFIED"
	}
}

func (p *ProtosourceModule) aggregateForFile(f pgs.File) pgs.Message {
	for _, m := range f.Messages() {
		if p.isAggregate(m) {
			return m
		}
	}
	return nil
}

func (p *ProtosourceModule) routePrefix(f pgs.File) string {
	importPath := p.ctx.ImportPath(f).String()
	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		return strings.TrimPrefix(rel, "/")
	}
	return importPath
}

// clientCommandFields returns command fields excluding id and actor.
func clientCommandFields(fields []pgs.Field) []pgs.Field {
	results := make([]pgs.Field, 0)
	for _, field := range fields {
		if field.Name() == "id" || field.Name() == "actor" {
			continue
		}
		results = append(results, field)
	}
	return results
}

// ── OpaqueData field annotation support ──────────────────────────────────

type opaqueFieldMapping struct {
	Field pgs.Field
	Order int32
}

type opaqueUsedGSI struct {
	Num      int
	HasPK    bool
	HasSK    bool
	PKType   optionsv1.OpaqueKeyType
	SKType   optionsv1.OpaqueKeyType
	PKFields []opaqueFieldMapping
	SKFields []opaqueFieldMapping
}

func fieldOpaqueOptions(f pgs.Field) *optionsv1.OpaqueFieldOptions {
	var opts optionsv1.OpaqueFieldOptions
	ok, err := f.Extension(optionsv1.E_ProtosourceOpaqueField, &opts)
	if err != nil || !ok {
		return nil
	}
	return &opts
}

func (p *ProtosourceModule) opaqueKeyMappings(m pgs.Message) map[optionsv1.OpaqueKeyType][]opaqueFieldMapping {
	result := make(map[optionsv1.OpaqueKeyType][]opaqueFieldMapping)
	for _, f := range m.Fields() {
		opts := fieldOpaqueOptions(f)
		if opts == nil {
			continue
		}
		for _, attr := range opts.GetAttributes() {
			kt := attr.GetType()
			if kt == optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_UNSPECIFIED {
				continue
			}
			result[kt] = append(result[kt], opaqueFieldMapping{
				Field: f,
				Order: attr.GetOrder(),
			})
		}
	}
	for kt := range result {
		sort.Slice(result[kt], func(i, j int) bool {
			return result[kt][i].Order < result[kt][j].Order
		})
	}
	return result
}

func (p *ProtosourceModule) opaqueUsedGSIs(m pgs.Message) []opaqueUsedGSI {
	mappings := p.opaqueKeyMappings(m)
	var result []opaqueUsedGSI

	for i := 1; i <= 20; i++ {
		pkType := optionsv1.OpaqueKeyType(3 + (i-1)*2)
		skType := optionsv1.OpaqueKeyType(4 + (i-1)*2)
		pkFields := mappings[pkType]
		skFields := mappings[skType]
		if len(pkFields) == 0 {
			continue
		}
		result = append(result, opaqueUsedGSI{
			Num:      i,
			HasPK:    len(pkFields) > 0,
			HasSK:    len(skFields) > 0,
			PKType:   pkType,
			SKType:   skType,
			PKFields: pkFields,
			SKFields: skFields,
		})
	}
	return result
}

func (p *ProtosourceModule) opaqueGSISKFields(m pgs.Message, gsiNum int) []opaqueFieldMapping {
	mappings := p.opaqueKeyMappings(m)
	skType := optionsv1.OpaqueKeyType(4 + (gsiNum-1)*2)
	return mappings[skType]
}

func (p *ProtosourceModule) opaquePKFields(m pgs.Message) []opaqueFieldMapping {
	if p.isAggregate(m) || p.isProjection(m) {
		for _, f := range m.Fields() {
			if f.Name().String() == "id" {
				return []opaqueFieldMapping{{Field: f, Order: 1}}
			}
		}
		return nil
	}
	mappings := p.opaqueKeyMappings(m)
	return mappings[optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_PK]
}

func opaqueFieldNameLower(f pgs.Field) string {
	return strings.ToLower(f.Name().String())
}

func queryRoutePath(fields []opaqueFieldMapping) string {
	parts := make([]string, len(fields))
	for i, fm := range fields {
		parts[i] = strings.ReplaceAll(strings.ToLower(fm.Field.Name().String()), "_", "-")
	}
	return "by-" + strings.Join(parts, "-and-")
}

func unexport(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// commandEmbeddedTypes returns the names of embedded message types used as
// command fields (excluding id and actor). These need to be imported in the
// generated TS client.
func commandEmbeddedTypes(f pgs.File, isCmd func(pgs.Message) bool) []string {
	seen := make(map[string]bool)
	var result []string
	for _, m := range f.Messages() {
		if !isCmd(m) {
			continue
		}
		for _, field := range clientCommandFields(m.Fields()) {
			if field.Type().IsEmbed() {
				name := field.Type().Embed().Name().String()
				if !seen[name] {
					seen[name] = true
					result = append(result, name)
				}
			}
		}
	}
	return result
}

// ── TypeScript-specific functions ─────────────────────────────────────────

// tsType maps a proto field type to a TypeScript type string.
func tsType(f pgs.Field) string {
	if f.Type().IsMap() {
		return "unknown" // maps not expected in command fields
	}
	if f.Type().IsRepeated() {
		return tsScalarType(f) + "[]"
	}
	if f.Type().IsEmbed() {
		return f.Type().Embed().Name().String()
	}
	if f.Type().IsEnum() {
		return f.Type().Enum().Name().String()
	}
	return tsScalarType(f)
}

func tsScalarType(f pgs.Field) string {
	switch f.Type().ProtoType() {
	case pgs.StringT:
		return "string"
	case pgs.BoolT:
		return "boolean"
	case pgs.Int32T, pgs.SInt32, pgs.SFixed32, pgs.UInt32T, pgs.Fixed32T,
		pgs.FloatT, pgs.DoubleT:
		return "number"
	case pgs.Int64T, pgs.SInt64, pgs.SFixed64, pgs.UInt64T, pgs.Fixed64T:
		return "bigint"
	case pgs.BytesT:
		return "Uint8Array"
	default:
		return "unknown"
	}
}

// tsFieldName converts a proto snake_case field name to camelCase,
// matching protoc-gen-es v2 output.
func tsFieldName(f pgs.Field) string {
	return snakeToCamel(f.Name().String())
}

// snakeToCamel converts a snake_case string to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			r := []rune(parts[i])
			r[0] = unicode.ToUpper(r[0])
			parts[i] = string(r)
		}
	}
	return strings.Join(parts, "")
}

// tsQueryFormatExpr returns a TypeScript expression to convert a typed value
// to a string for use in URL query parameters.
func tsQueryFormatExpr(f pgs.Field, varName string) string {
	switch f.Type().ProtoType() {
	case pgs.StringT:
		return varName
	default:
		return fmt.Sprintf("String(%s)", varName)
	}
}

// protoFileName derives the protoc-gen-es import path from the proto file name.
// e.g., "sample_v1.proto" -> "./sample_v1_pb.js"
func protoFileName(f pgs.File) string {
	base := strings.TrimSuffix(f.InputPath().Base(), ".proto")
	return "./" + base + "_pb.js"
}
