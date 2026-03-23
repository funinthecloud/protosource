package main

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/template"

	pgs "github.com/lyft/protoc-gen-star/v2"
	pgsgo "github.com/lyft/protoc-gen-star/v2/lang/go"
	optionsv1 "github.com/funinthecloud/protosource/options/v1"
)

// ProtosourceModule generates boilerplate for protosource domains.
type ProtosourceModule struct {
	*pgs.ModuleBase
	ctx            pgsgo.Context
	tpls           []*template.Template
	params         pgs.Parameters
	enumValueIndex map[string]string // built once per file in generate()
}

// Protosourceify returns an initialized ProtosourceModule.
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
		"package":                p.ctx.PackageName,
		"name":                   p.ctx.Name,
		"dump":                   p.dump,
		"gotype":                 p.ctx.Type,
		"isAggregate":            p.isAggregate,
		"isCommand":              p.isCommand,
		"isEvent":                p.isEvent,
		"isProjection":           p.isProjection,
		"isSnapshot":             p.isSnapshot,
		"isCreationEvent":        p.isCreationEvent,
		"aggregateHasField":      p.aggregateHasField,
		"excludeInternal":        ExcludeInternal,
		"excludeCommandInternal": ExcludeCommandInternal,
		"commandEvents":          p.commandEvents,
		"commandLifecycle":       p.commandLifecycle,
		"commandAllowedStates":   p.commandAllowedStates,
		"stateEnumName":          p.stateEnumName,
		"snapshotEveryN":         p.snapshotEveryN,
		"eventMessage":           p.eventMessage,
		"eventSetsState":         p.eventSetsState,
		"hasOpaqueAnnotations":   p.hasOpaqueAnnotations,
		"opaqueKeyMappings":      p.opaqueKeyMappings,
		"opaqueKeyPrefix":        p.opaqueKeyPrefix,
		"opaqueAllKeySlots":      opaqueAllKeySlots,
		"opaqueUsedGSIs":         p.opaqueUsedGSIs,
		"opaqueGSISKFields":      p.opaqueGSISKFields,
		"opaquePKFields":         p.opaquePKFields,
		"opaqueFieldNameLower":   opaqueFieldNameLower,
		"opaqueKeySlotName":      opaqueKeySlotName,
		"opaqueKeySlotGSINum":    opaqueKeySlotGSINum,
		"opaqueKeySlotIsSK":      opaqueKeySlotIsSK,
		"routePrefix":            p.routePrefix,
		"lower":                  strings.ToLower,
		"importPath":             p.importPath,
		"cliCommandFields":       CLICommandFields,
		"add":                    func(a, b int) int { return a + b },
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

// isCreationEvent returns true if the given event message is exclusively
// produced by CREATION lifecycle commands. If any MUTATION command also
// produces this event, it returns false — we don't want setCreated called
// on every state transition.
func (p *ProtosourceModule) isCreationEvent(m pgs.Message, f pgs.File) bool {
	eventName := m.Name().String()
	producedByCreation := false
	for _, msg := range f.AllMessages() {
		if !p.isCommand(msg) {
			continue
		}
		for _, producedEvent := range p.commandEvents(msg) {
			if producedEvent != eventName {
				continue
			}
			if p.commandLifecycle(msg) == "CREATION" {
				producedByCreation = true
			} else {
				// A non-creation command also produces this event
				return false
			}
		}
	}
	return producedByCreation
}

// aggregateHasField returns true if the aggregate message has a field with the
// same name as the given event field. This is used to safely map event fields
// to aggregate fields in the generated On method.
func (p *ProtosourceModule) aggregateHasField(eventField pgs.Field, aggregate pgs.Message) bool {
	for _, f := range aggregate.Fields() {
		if f.Name() == eventField.Name() {
			return true
		}
	}
	return false
}

func (p *ProtosourceModule) messageOptions(m pgs.Message) *optionsv1.MessageOptions {
	var opts optionsv1.MessageOptions
	ok, err := m.Extension(optionsv1.E_ProtosourceMessageType, &opts)
	if err != nil || !ok {
		return nil
	}
	return &opts
}

// commandLifecycle returns the lifecycle enum value for a command message.
// Returns "UNSPECIFIED", "CREATION", or "MUTATION".
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

// commandEvents returns the produces_events names for a command message.
func (p *ProtosourceModule) commandEvents(m pgs.Message) []string {
	opts := p.messageOptions(m)
	if opts == nil || opts.GetCommand() == nil {
		return nil
	}
	return opts.GetCommand().GetProducesEvents()
}

// commandAllowedStates returns the allowed_states for a command message.
func (p *ProtosourceModule) commandAllowedStates(m pgs.Message) []string {
	opts := p.messageOptions(m)
	if opts == nil || opts.GetCommand() == nil {
		return nil
	}
	return opts.GetCommand().GetAllowedStates()
}

// buildEnumValueIndex builds a map from enum value name to Go-qualified name
// (e.g., "STATE_LOCKED" → "State_STATE_LOCKED") for all enums in the file.
func buildEnumValueIndex(f pgs.File) map[string]string {
	idx := make(map[string]string)
	for _, e := range f.AllEnums() {
		for _, v := range e.Values() {
			idx[v.Name().String()] = e.Name().String() + "_" + v.Name().String()
		}
	}
	return idx
}

// stateEnumName looks up the Go-qualified name for an enum value using the
// precomputed index. The pgs.File parameter is accepted for template
// compatibility but ignored (the index is built once per file in generate).
func (p *ProtosourceModule) stateEnumName(valueName string, _ pgs.File) string {
	return p.enumValueIndex[valueName]
}

// validateAllowedStates checks that every entry in a command's allowed_states
// refers to an enum value that exists in the file.
func (p *ProtosourceModule) validateAllowedStates(cmd pgs.Message) error {
	for _, s := range p.commandAllowedStates(cmd) {
		if _, ok := p.enumValueIndex[s]; !ok {
			return fmt.Errorf("command %s: allowed_states references %q, but no such enum value exists in the file", cmd.Name(), s)
		}
	}
	return nil
}

// validateSetsState checks that an event's sets_state annotation, if present,
// refers to an enum value that exists in the file.
func (p *ProtosourceModule) validateSetsState(evt pgs.Message) error {
	state := p.eventSetsState(evt)
	if state == "" {
		return nil
	}
	if _, ok := p.enumValueIndex[state]; !ok {
		return fmt.Errorf("event %s: sets_state references %q, but no such enum value exists in the file", evt.Name(), state)
	}
	return nil
}

// snapshotEveryN returns the every_n_events value from the Snapshot message
// in the file, or 0 if no snapshot is configured or disabled.
func (p *ProtosourceModule) snapshotEveryN(f pgs.File) uint32 {
	for _, m := range f.Messages() {
		opts := p.messageOptions(m)
		if opts == nil || opts.GetSnapshot() == nil {
			continue
		}
		snap := opts.GetSnapshot()
		if snap.GetDisabled() {
			return 0
		}
		return snap.GetEveryNEvents()
	}
	return 0
}

// eventSetsState returns the sets_state annotation value for an event message,
// or an empty string if no state transition is declared.
func (p *ProtosourceModule) eventSetsState(m pgs.Message) string {
	opts := p.messageOptions(m)
	if opts == nil || opts.GetEvent() == nil {
		return ""
	}
	return opts.GetEvent().GetSetsState()
}

// eventMessage looks up a message by name within the same file.
func (p *ProtosourceModule) eventMessage(name string, f pgs.File) pgs.Message {
	for _, m := range f.Messages() {
		if m.Name().String() == name {
			return m
		}
	}
	return nil
}

// ExcludeCommandInternal filters out command-internal fields (just "id"),
// leaving actor and domain fields for passing to event builder methods.
func ExcludeCommandInternal(fields []pgs.Field) interface{} {
	results := make([]pgs.Field, 0)
	for _, field := range fields {
		if field.Name() == "id" {
			continue
		}
		results = append(results, field)
	}
	return results
}

// CLICommandFields returns command fields excluding id and actor (both are
// handled automatically by the CLI: id from args, actor from OS user+hostname).
func CLICommandFields(fields []pgs.Field) []pgs.Field {
	results := make([]pgs.Field, 0)
	for _, field := range fields {
		if field.Name() == "id" || field.Name() == "actor" {
			continue
		}
		results = append(results, field)
	}
	return results
}

func ExcludeInternal(fields []pgs.Field) interface{} {
	results := make([]pgs.Field, 0)

	for _, field := range fields {
		switch n := field.Name(); n {
		case "id", "version", "at":
			continue
		}
		results = append(results, field)
	}
	return results
}

// requiredField describes an expected field with a name, number, and proto type.
type requiredField struct {
	name   string
	number int32
	ptype  pgs.ProtoType
}

var commandFields = []requiredField{
	{"id", 1, pgs.StringT},
	{"actor", 2, pgs.StringT},
}

var eventFields = []requiredField{
	{"id", 1, pgs.StringT},
	{"version", 2, pgs.Int64T},
	{"at", 3, pgs.Int64T},
	{"actor", 4, pgs.StringT},
}

// validateFields checks that a message has the required fields with correct numbers and types.
// Returns an error describing the first violation, or nil if valid.
func validateFields(m pgs.Message, required []requiredField) error {
	byName := make(map[string]pgs.Field, len(m.Fields()))
	for _, f := range m.Fields() {
		byName[f.Name().String()] = f
	}

	for _, req := range required {
		f, ok := byName[req.name]
		if !ok {
			return fmt.Errorf("message %s: missing required field %q (field number %d)", m.Name(), req.name, req.number)
		}
		if got := f.Descriptor().GetNumber(); got != req.number {
			return fmt.Errorf("message %s: field %q must be field number %d, got %d", m.Name(), req.name, req.number, got)
		}
		if got := f.Type().ProtoType(); got != req.ptype {
			return fmt.Errorf("message %s: field %q must be type %s, got %s", m.Name(), req.name, req.ptype, got)
		}
	}
	return nil
}

// validateProducesEvents checks that every entry in a command's produces_events
// refers to a message that exists in the file and is annotated as an event.
func (p *ProtosourceModule) validateProducesEvents(cmd pgs.Message, f pgs.File) error {
	events := p.commandEvents(cmd)
	if len(events) == 0 {
		return fmt.Errorf("command %s: produces_events must list at least one event", cmd.Name())
	}
	for _, name := range events {
		em := p.eventMessage(name, f)
		if em == nil {
			return fmt.Errorf("command %s: produces_events references %q, but no such message exists", cmd.Name(), name)
		}
		if !p.isEvent(em) {
			return fmt.Errorf("command %s: produces_events references %q, but it is not annotated as an event", cmd.Name(), name)
		}
	}
	return nil
}

// outputPathForTemplate computes the output file path for a proto file and template.
// The default template ("protosource.gotext") produces ".protosource.pb.go" (backward compatible).
// The cli template ("cli.gotext") produces a subdirectory: "<aggregate_lower>mgr/main.go".
// Other templates produce ".protosource.<name>.pb.go" where <name> is the template name
// without the ".gotext" extension.
func (p *ProtosourceModule) outputPathForTemplate(f pgs.File, tpl *template.Template) string {
	importPath := p.ctx.ImportPath(f).String()

	// CLI template goes into a <aggregate>mgr/ subdirectory as package main.
	if tpl.Name() == "cli.gotext" {
		return p.cliOutputPath(f, importPath)
	}

	suffix := ".protosource.pb.go"
	if tplName := tpl.Name(); tplName != "protosource.gotext" {
		name := strings.TrimSuffix(tplName, ".gotext")
		suffix = ".protosource." + name + ".pb.go"
	}

	base := strings.TrimSuffix(f.InputPath().Base(), ".proto") + suffix

	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		rel = strings.TrimPrefix(rel, "/")
		return rel + "/" + base
	}

	// Fallback: use OutputPath from pgsgo context
	out := p.ctx.OutputPath(f).String()
	return strings.TrimSuffix(out, ".pb.go") + suffix
}

// cliOutputPath returns the output path for the CLI template, placing it in
// a <aggregate_lower>mgr/ subdirectory (e.g., "example/app/test/v1/testmgr/main.go").
func (p *ProtosourceModule) cliOutputPath(f pgs.File, importPath string) string {
	aggregateName := ""
	for _, m := range f.Messages() {
		if p.isAggregate(m) {
			aggregateName = strings.ToLower(m.Name().String())
			break
		}
	}
	if aggregateName == "" {
		aggregateName = "cli"
	}

	dir := aggregateName + "mgr"

	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		rel = strings.TrimPrefix(rel, "/")
		return rel + "/" + dir + "/main.go"
	}

	out := p.ctx.OutputPath(f).String()
	parent := out[:strings.LastIndex(out, "/")]
	return parent + "/" + dir + "/main.go"
}

// importPath returns the full Go import path for the proto file's package.
func (p *ProtosourceModule) importPath(f pgs.File) string {
	return p.ctx.ImportPath(f).String()
}

// routePrefix returns the module-stripped import path for a proto file,
// used to derive HTTP route prefixes (e.g., "example/app/sample/v1").
func (p *ProtosourceModule) routePrefix(f pgs.File) string {
	importPath := p.ctx.ImportPath(f).String()
	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		return strings.TrimPrefix(rel, "/")
	}
	return importPath
}

func (p *ProtosourceModule) dump(input any) string {
	return fmt.Sprintf("%#v", input)
}

// Name satisfies the generator.Plugin interface.
func (p *ProtosourceModule) Name() string { return "protosource" }

func (p *ProtosourceModule) Execute(targets map[string]pgs.File, pkgs map[string]pgs.Package) []pgs.Artifact {

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

	p.enumValueIndex = buildEnumValueIndex(f)

	for _, m := range f.Messages() {
		if p.isCommand(m) {
			if err := validateFields(m, commandFields); err != nil {
				p.Fail(err.Error())
				return
			}
			if err := p.validateProducesEvents(m, f); err != nil {
				p.Fail(err.Error())
				return
			}
			if err := p.validateAllowedStates(m); err != nil {
				p.Fail(err.Error())
				return
			}
		}
		if p.isEvent(m) {
			if err := validateFields(m, eventFields); err != nil {
				p.Fail(err.Error())
				return
			}
			if err := p.validateSetsState(m); err != nil {
				p.Fail(err.Error())
				return
			}
		}
	}

	hasOpaque := false
	for _, m := range f.Messages() {
		if p.hasOpaqueAnnotations(m) {
			hasOpaque = true
			if err := p.validateOpaqueAnnotations(m); err != nil {
				p.Fail(err.Error())
				return
			}
		}
	}

	if hasOpaque {
		if err := p.validateMessageNamesAgainstOpaque(f); err != nil {
			p.Fail(err.Error())
			return
		}
	}

	for _, v := range p.tpls {
		outPath := p.outputPathForTemplate(f, v)
		p.AddGeneratorTemplateFile(outPath, v, f)
	}
}

// ── OpaqueData field annotation support ──────────────────────────────────

// opaqueFieldMapping associates a proto field with its order in a composite key.
type opaqueFieldMapping struct {
	Field pgs.Field
	Order int32
}

// fieldOpaqueOptions reads the OpaqueFieldOptions extension from a field.
func fieldOpaqueOptions(f pgs.Field) *optionsv1.OpaqueFieldOptions {
	var opts optionsv1.OpaqueFieldOptions
	ok, err := f.Extension(optionsv1.E_ProtosourceOpaqueField, &opts)
	if err != nil || !ok {
		return nil
	}
	return &opts
}

// hasOpaqueAnnotations returns true if any field in the message has opaque annotations.
func (p *ProtosourceModule) hasOpaqueAnnotations(m pgs.Message) bool {
	for _, f := range m.Fields() {
		if opts := fieldOpaqueOptions(f); opts != nil && len(opts.GetAttributes()) > 0 {
			return true
		}
	}
	return false
}

// opaqueKeyMappings collects all annotated fields for a message, grouped by key type
// and sorted by order within each group.
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
	// Sort each group by order
	for kt := range result {
		sort.Slice(result[kt], func(i, j int) bool {
			return result[kt][i].Order < result[kt][j].Order
		})
	}
	return result
}

// opaqueKeyPrefix computes the key prefix from the proto package + message name.
// Format: "package_underscored#messagename"
func (p *ProtosourceModule) opaqueKeyPrefix(m pgs.Message, f pgs.File) string {
	pkg := f.Package().ProtoName().String()
	pkg = strings.ReplaceAll(pkg, ".", "_")
	name := strings.ToLower(m.Name().String())
	return pkg + "#" + name
}

// validateOpaqueAnnotations validates the opaque field annotations on a message.
func (p *ProtosourceModule) validateOpaqueAnnotations(m pgs.Message) error {
	mappings := p.opaqueKeyMappings(m)
	if len(mappings) == 0 {
		return nil
	}

	// Must have at least PK
	if _, ok := mappings[optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_PK]; !ok {
		return fmt.Errorf("message %s: opaque annotations present but no PK field defined", m.Name())
	}

	// Validate ordering within each key type
	for kt, fields := range mappings {
		if len(fields) == 1 {
			// Single field: order 0 or 1 are both acceptable, but reject negative
			if fields[0].Order < 0 {
				return fmt.Errorf("message %s: key type %v field %s has negative order %d",
					m.Name(), kt, fields[0].Field.Name(), fields[0].Order)
			}
			continue
		}
		// Composite key: require unique positive orders
		seen := make(map[int32]bool)
		for _, fm := range fields {
			if fm.Order <= 0 {
				return fmt.Errorf("message %s: composite key %v has %d fields — all must have positive order values, but field %s has order %d",
					m.Name(), kt, len(fields), fm.Field.Name(), fm.Order)
			}
			if seen[fm.Order] {
				return fmt.Errorf("message %s: duplicate order %d for key type %v", m.Name(), fm.Order, kt)
			}
			seen[fm.Order] = true
		}
	}

	// Validate GSI completeness: if a GSI SK is annotated, require a corresponding GSI PK
	for i := 1; i <= 20; i++ {
		skType := optionsv1.OpaqueKeyType(4 + (i-1)*2)
		pkType := optionsv1.OpaqueKeyType(3 + (i-1)*2)
		if len(mappings[skType]) > 0 && len(mappings[pkType]) == 0 {
			return fmt.Errorf("message %s: GSI%d has SK fields but no PK fields — annotate a PK for this index", m.Name(), i)
		}
	}

	return nil
}

// opaqueReservedNames returns the set of method names generated by opaque
// annotations (AutoPKSK + Hydrater). Message names that collide with these
// would produce duplicate Go methods in the generated file.
func opaqueReservedNames() map[string]bool {
	names := make(map[string]bool, 44)
	for _, kt := range opaqueAllKeySlots() {
		names[opaqueKeySlotName(kt)] = true
	}
	names["Hydrate"] = true
	return names
}

// validateMessageNamesAgainstOpaque checks that no command, event, or snapshot
// message name in the file collides with a generated AutoPKSK/Hydrater method
// name. This is only relevant when the file contains opaque annotations.
func (p *ProtosourceModule) validateMessageNamesAgainstOpaque(f pgs.File) error {
	reserved := opaqueReservedNames()
	for _, m := range f.Messages() {
		name := m.Name().String()
		if reserved[name] {
			return fmt.Errorf(
				"message %q in %s conflicts with generated AutoPKSK method name %q; rename the message to avoid a compilation error",
				name, f.Name(), name,
			)
		}
	}
	return nil
}

// opaqueAllKeySlots returns all 42 key slot types (PK, SK, GSI1PK..GSI20SK).
func opaqueAllKeySlots() []optionsv1.OpaqueKeyType {
	slots := make([]optionsv1.OpaqueKeyType, 0, 42)
	for i := optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_PK; i <= optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_GSI20SK; i++ {
		slots = append(slots, i)
	}
	return slots
}

// opaqueKeySlotName returns the method name for a key type.
// e.g. OPAQUE_KEY_TYPE_PK → "PK", OPAQUE_KEY_TYPE_GSI1PK → "GSI1PK"
func opaqueKeySlotName(kt optionsv1.OpaqueKeyType) string {
	s := kt.String()
	return strings.TrimPrefix(s, "OPAQUE_KEY_TYPE_")
}

// opaqueKeySlotGSINum returns the GSI number for a key type (0 for PK/SK).
func opaqueKeySlotGSINum(kt optionsv1.OpaqueKeyType) int {
	n := int(kt)
	if n <= 2 {
		return 0
	}
	return (n-3)/2 + 1
}

// opaqueKeySlotIsSK returns true if the key type is a sort key (SK or GSInSK).
func opaqueKeySlotIsSK(kt optionsv1.OpaqueKeyType) bool {
	return int(kt)%2 == 0
}

// opaqueFieldNameLower returns the lowercase version of a field name (for key formatting).
func opaqueFieldNameLower(f pgs.Field) string {
	return strings.ToLower(f.Name().String())
}

// opaqueUsedGSI represents a GSI index with its PK and SK field info.
type opaqueUsedGSI struct {
	Num     int
	HasPK   bool
	HasSK   bool
	PKType  optionsv1.OpaqueKeyType
	SKType  optionsv1.OpaqueKeyType
	PKFields []opaqueFieldMapping
	SKFields []opaqueFieldMapping
}

// opaqueUsedGSIs returns info about all GSIs that have at least a PK defined.
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

// opaqueGSISKFields returns the fields for a specific GSI SK. Used for typed SK structs.
func (p *ProtosourceModule) opaqueGSISKFields(m pgs.Message, gsiNum int) []opaqueFieldMapping {
	mappings := p.opaqueKeyMappings(m)
	skType := optionsv1.OpaqueKeyType(4 + (gsiNum-1)*2)
	return mappings[skType]
}

// opaquePKFields returns the fields mapped to the table PK.
func (p *ProtosourceModule) opaquePKFields(m pgs.Message) []opaqueFieldMapping {
	mappings := p.opaqueKeyMappings(m)
	return mappings[optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_PK]
}
