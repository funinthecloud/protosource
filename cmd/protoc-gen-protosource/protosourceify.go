package main

import (
	"fmt"
	"io/fs"
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

// outputPath computes the output file path for a proto file.
// It supports the "module" parameter (like protoc-gen-go) to strip a Go module
// prefix from the go_package import path, producing a relative output path.
func (p *ProtosourceModule) outputPath(f pgs.File) string {
	importPath := p.ctx.ImportPath(f).String()
	base := strings.TrimSuffix(f.InputPath().Base(), ".proto") + ".protosource.pb.go"

	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		rel = strings.TrimPrefix(rel, "/")
		return rel + "/" + base
	}

	// Fallback: use OutputPath from pgsgo context
	out := p.ctx.OutputPath(f).String()
	return strings.TrimSuffix(out, ".pb.go") + ".protosource.pb.go"
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

	for _, v := range p.tpls {
		outPath := p.outputPath(f)
		p.AddGeneratorTemplateFile(outPath, v, f)
	}
}
