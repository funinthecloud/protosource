package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"unicode"

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
		"eventTTLSeconds":        p.eventTTLSeconds,
		"eventMessage":           p.eventMessage,
		"eventSetsState":              p.eventSetsState,
		"eventCollectionMapping":      p.eventCollectionMapping,
		"eventCollectionAction":       p.eventCollectionAction,
		"eventCollectionTarget":       p.eventCollectionTarget,
		"eventCollectionKeyField":     p.eventCollectionKeyField,
		"eventCollectionSourceField":  p.eventCollectionSourceField,
		"collectionElementTypeName":   p.collectionElementTypeName,
		"collectionKeyFieldGoName":    p.collectionKeyFieldGoName,
		"aggregateFieldGoName":        p.aggregateFieldGoName,
		"eventFieldGoName":            p.eventFieldGoName,
		"hasOpaqueAnnotations":        p.hasOpaqueAnnotations,
		"opaqueKeyMappings":      p.opaqueKeyMappings,
		"opaqueKeyPrefix":        p.opaqueKeyPrefix,
		"opaqueAllKeySlots":      opaqueAllKeySlots,
		"opaqueUsedGSIs":         p.opaqueUsedGSIs,
		"opaqueGSISKFields":      p.opaqueGSISKFields,
		"opaquePKFields":         p.opaquePKFields,
		"opaqueFieldNameLower":   opaqueFieldNameLower,
		"opaqueKeySlotName":      opaqueKeySlotName,
		"opaqueKeySlotGSINum":    opaqueKeySlotGSINum,
		"opaqueKeySlotIsSK":          opaqueKeySlotIsSK,
		"projectionSKValue":          p.projectionSKValue,
		"projectionMatchingFields":   p.projectionMatchingFields,
		"projectionsForFile":         p.projectionsForFile,
		"aggregateForFile":           p.aggregateForFile,
		"routePrefix":                p.routePrefix,
		"lower":                  strings.ToLower,
		"importPath":             p.importPath,
		"cliCommandFields":       CLICommandFields,
		"cliParseExpr":           cliParseExpr,
		"fileSupportsCLI":        p.fileSupportsCLI,
		"add":                    func(a, b int) int { return a + b },
		"lastPathComponent":      lastPathComponent,
		"unexport":               unexport,
		"queryRoutePath":         queryRoutePath,
		"queryParseExpr":         queryParseExpr,
		"queryFormatExpr":        queryFormatExpr,
		"cliQueryParseExpr":      cliQueryParseExpr,
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

// eventTTLSeconds returns the event_ttl_seconds value from the aggregate message
// in the file, or 0 if no TTL is configured.
func (p *ProtosourceModule) eventTTLSeconds(f pgs.File) int64 {
	for _, m := range f.Messages() {
		opts := p.messageOptions(m)
		if opts == nil || opts.GetAggregate() == nil {
			continue
		}
		return opts.GetAggregate().GetEventTtlSeconds()
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

// eventCollectionMapping returns the CollectionMapping annotation for an event,
// or nil if the event does not have a collection annotation at all.
// A non-nil return means the annotation is present (possibly misconfigured) —
// call validateCollectionMapping to check required fields like target and action.
func (p *ProtosourceModule) eventCollectionMapping(m pgs.Message) *optionsv1.CollectionMapping {
	opts := p.messageOptions(m)
	if opts == nil || opts.GetEvent() == nil {
		return nil
	}
	return opts.GetEvent().GetCollection()
}

// eventCollectionSourceField finds the event field whose embedded message type
// matches the value type of the target map field on the aggregate.
// Returns nil if no matching field is found.
func (p *ProtosourceModule) eventCollectionSourceField(evt pgs.Message, agg pgs.Message) pgs.Field {
	cm := p.eventCollectionMapping(evt)
	if cm == nil {
		return nil
	}
	// Find the aggregate's map field.
	var targetField pgs.Field
	for _, f := range agg.Fields() {
		if f.Name().String() == cm.GetTarget() {
			targetField = f
			break
		}
	}
	if targetField == nil || !targetField.Type().IsMap() || !targetField.Type().Element().IsEmbed() {
		return nil
	}
	elemFQN := targetField.Type().Element().Embed().FullyQualifiedName()

	// Find the event field whose message type matches.
	for _, ef := range evt.Fields() {
		if ef.Type().IsEmbed() && ef.Type().Embed().FullyQualifiedName() == elemFQN {
			return ef
		}
	}
	return nil
}

// collectionElementTypeName returns the Go type name of the value type of a
// map<string, Message> field on the aggregate.
func (p *ProtosourceModule) collectionElementTypeName(target string, agg pgs.Message) string {
	for _, f := range agg.Fields() {
		if f.Name().String() == target && f.Type().IsMap() && f.Type().Element().IsEmbed() {
			return p.ctx.Name(f.Type().Element().Embed()).String()
		}
	}
	return ""
}

// collectionKeyFieldGoName returns the Go (PascalCase) name of the key_field
// on the collection element message.
func (p *ProtosourceModule) collectionKeyFieldGoName(keyField string, target string, agg pgs.Message) string {
	for _, f := range agg.Fields() {
		if f.Name().String() == target && f.Type().IsMap() && f.Type().Element().IsEmbed() {
			for _, ef := range f.Type().Element().Embed().Fields() {
				if ef.Name().String() == keyField {
					return p.ctx.Name(ef).String()
				}
			}
		}
	}
	return ""
}

// aggregateFieldGoName returns the Go (PascalCase) name of a field on a message,
// looked up by its proto name. Used in templates where we have a string field name.
func (p *ProtosourceModule) aggregateFieldGoName(protoName string, msg pgs.Message) string {
	for _, f := range msg.Fields() {
		if f.Name().String() == protoName {
			return p.ctx.Name(f).String()
		}
	}
	return ""
}

// eventFieldGoName returns the Go (PascalCase) name of a field on an event message,
// looked up by its proto name.
func (p *ProtosourceModule) eventFieldGoName(protoName string, evt pgs.Message) string {
	for _, f := range evt.Fields() {
		if f.Name().String() == protoName {
			return p.ctx.Name(f).String()
		}
	}
	return ""
}

// eventCollectionAction returns "ADD", "REMOVE", or "" for an event's collection annotation.
func (p *ProtosourceModule) eventCollectionAction(m pgs.Message) string {
	cm := p.eventCollectionMapping(m)
	if cm == nil {
		return ""
	}
	switch cm.GetAction() {
	case optionsv1.CollectionAction_COLLECTION_ACTION_ADD:
		return "ADD"
	case optionsv1.CollectionAction_COLLECTION_ACTION_REMOVE:
		return "REMOVE"
	default:
		return ""
	}
}

// eventCollectionTarget returns the target field name from the collection annotation.
func (p *ProtosourceModule) eventCollectionTarget(m pgs.Message) string {
	cm := p.eventCollectionMapping(m)
	if cm == nil {
		return ""
	}
	return cm.GetTarget()
}

// eventCollectionKeyField returns the key_field from the collection annotation.
func (p *ProtosourceModule) eventCollectionKeyField(m pgs.Message) string {
	cm := p.eventCollectionMapping(m)
	if cm == nil {
		return ""
	}
	return cm.GetKeyField()
}

// validateCollectionMapping validates the collection annotation on an event message.
// Collections use map<string, Message> fields on the aggregate.
func (p *ProtosourceModule) validateCollectionMapping(evt pgs.Message, agg pgs.Message, f pgs.File) error {
	cm := p.eventCollectionMapping(evt)
	if cm == nil {
		return nil
	}

	// Target is required whenever a collection annotation is present.
	if cm.GetTarget() == "" {
		return fmt.Errorf("event %s: collection annotation present but target is empty",
			evt.Name())
	}

	// Action is required.
	if cm.GetAction() == optionsv1.CollectionAction_COLLECTION_ACTION_UNSPECIFIED {
		return fmt.Errorf("event %s: collection annotation present but action is unspecified (must be ADD or REMOVE)",
			evt.Name())
	}

	// key_field is required for both ADD and REMOVE.
	if cm.GetKeyField() == "" {
		return fmt.Errorf("event %s: collection annotation requires key_field to be set",
			evt.Name())
	}

	// Target field must exist on aggregate.
	var targetField pgs.Field
	for _, af := range agg.Fields() {
		if af.Name().String() == cm.GetTarget() {
			targetField = af
			break
		}
	}
	if targetField == nil {
		return fmt.Errorf("event %s: collection target %q not found on aggregate %s",
			evt.Name(), cm.GetTarget(), agg.Name())
	}

	// Target must be a map<string, Message> field.
	if !targetField.Type().IsMap() {
		return fmt.Errorf("event %s: collection target %q on aggregate %s must be a map field",
			evt.Name(), cm.GetTarget(), agg.Name())
	}
	if targetField.Type().Key().ProtoType() != pgs.StringT {
		return fmt.Errorf("event %s: collection target %q on aggregate %s must have string keys",
			evt.Name(), cm.GetTarget(), agg.Name())
	}
	if !targetField.Type().Element().IsEmbed() {
		return fmt.Errorf("event %s: collection target %q on aggregate %s must have message values",
			evt.Name(), cm.GetTarget(), agg.Name())
	}

	elemMsg := targetField.Type().Element().Embed()

	// key_field must exist on the element message and be a string.
	var elemKeyField pgs.Field
	for _, ef := range elemMsg.Fields() {
		if ef.Name().String() == cm.GetKeyField() {
			elemKeyField = ef
			break
		}
	}
	if elemKeyField == nil {
		return fmt.Errorf("event %s: key_field %q not found on element %s",
			evt.Name(), cm.GetKeyField(), elemMsg.Name())
	}
	if elemKeyField.Type().IsRepeated() || elemKeyField.Type().IsMap() {
		return fmt.Errorf("event %s: key_field %q on element %s must be a scalar string, not repeated/map",
			evt.Name(), cm.GetKeyField(), elemMsg.Name())
	}
	if elemKeyField.Type().ProtoType() != pgs.StringT {
		return fmt.Errorf("event %s: key_field %q on element %s must be a string, got %s",
			evt.Name(), cm.GetKeyField(), elemMsg.Name(), elemKeyField.Type().ProtoType())
	}

	// REMOVE is not valid on creation events — the collection is empty at creation time.
	if cm.GetAction() == optionsv1.CollectionAction_COLLECTION_ACTION_REMOVE {
		if p.isCreationEvent(evt, f) {
			return fmt.Errorf("event %s: collection REMOVE is not valid on a creation event",
				evt.Name())
		}
	}

	// Collect domain fields (everything except id, version, at, actor).
	var domainFields []pgs.Field
	for _, ef := range evt.Fields() {
		switch ef.Name().String() {
		case "id", "version", "at", "actor":
			continue
		}
		domainFields = append(domainFields, ef)
	}

	switch cm.GetAction() {
	case optionsv1.CollectionAction_COLLECTION_ACTION_ADD:
		// Event must have exactly one embedded field matching the element type.
		elemFQN := elemMsg.FullyQualifiedName()
		var matchCount int
		for _, ef := range evt.Fields() {
			if ef.Type().IsEmbed() && ef.Type().Embed().FullyQualifiedName() == elemFQN {
				matchCount++
			}
		}
		if matchCount == 0 {
			return fmt.Errorf("event %s: collection ADD requires a field of type %s, but none found",
				evt.Name(), elemMsg.Name())
		}
		if matchCount > 1 {
			return fmt.Errorf("event %s: collection ADD requires exactly one field of type %s, but found %d",
				evt.Name(), elemMsg.Name(), matchCount)
		}
		// Collection events must not have extra domain fields.
		if len(domainFields) != 1 {
			return fmt.Errorf("event %s: collection ADD must have exactly one domain field (the element), but has %d",
				evt.Name(), len(domainFields))
		}

	case optionsv1.CollectionAction_COLLECTION_ACTION_REMOVE:
		// Event must have a string field matching key_field.
		var evtKeyField pgs.Field
		for _, ef := range evt.Fields() {
			if ef.Name().String() == cm.GetKeyField() {
				evtKeyField = ef
				break
			}
		}
		if evtKeyField == nil {
			return fmt.Errorf("event %s: collection REMOVE requires a field named %q matching the key_field",
				evt.Name(), cm.GetKeyField())
		}
		if evtKeyField.Type().IsRepeated() || evtKeyField.Type().IsMap() {
			return fmt.Errorf("event %s: collection REMOVE field %q must be a scalar string, not repeated/map",
				evt.Name(), cm.GetKeyField())
		}
		if evtKeyField.Type().ProtoType() != pgs.StringT {
			return fmt.Errorf("event %s: collection REMOVE field %q must be a string, got %s",
				evt.Name(), cm.GetKeyField(), evtKeyField.Type().ProtoType())
		}
		// Collection events must not have extra domain fields.
		if len(domainFields) != 1 {
			return fmt.Errorf("event %s: collection REMOVE must have exactly one domain field (the key), but has %d",
				evt.Name(), len(domainFields))
		}

	default:
		return fmt.Errorf("event %s: collection action must be ADD or REMOVE", evt.Name())
	}

	return nil
}

// fileSupportsCLI returns true if all commands in the file have only scalar
// fields (no repeated, map, message, or enum). Files with complex command
// fields skip CLI generation instead of failing.
func (p *ProtosourceModule) fileSupportsCLI(f pgs.File) bool {
	for _, m := range f.Messages() {
		if p.isCommand(m) {
			if err := validateCLICommandFields(m); err != nil {
				return false
			}
		}
	}
	return true
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

// validateCLICommandFields checks that all non-id/actor command fields are
// scalar types the generated CLI can parse from os.Args: string, integer,
// float, bool, and bytes (read from a file path). Repeated, map, message,
// and enum fields are rejected because they cannot be meaningfully parsed
// from a single positional argument.
func validateCLICommandFields(m pgs.Message) error {
	for _, field := range CLICommandFields(m.Fields()) {
		if field.Type().IsRepeated() || field.Type().IsMap() {
			return fmt.Errorf(
				"command %s: field %q is repeated/map — the generated CLI only supports scalar fields; "+
					"use a hand-written CLI for complex types",
				m.Name(), field.Name())
		}
		if field.Type().IsEmbed() {
			return fmt.Errorf(
				"command %s: field %q is a message type — the generated CLI only supports scalar fields; "+
					"use a hand-written CLI for complex types",
				m.Name(), field.Name())
		}
		if field.Type().IsEnum() {
			return fmt.Errorf(
				"command %s: field %q is an enum — the generated CLI cannot parse enum values; "+
					"use a hand-written CLI for enum fields",
				m.Name(), field.Name())
		}
	}
	return nil
}

// cliParseExpr returns the Go expression to parse an os.Args value into the
// correct type for a command field. For strings it returns the arg directly;
// for numeric/bool types it wraps with a mustParseXxx helper; for bytes it
// reads the contents from the file path given as the argument.
//
// Note: pgs names the signed/fixed variants without the T suffix (SInt32,
// SFixed64) while the primary types use it (Int32T, UInt64T). This is a
// naming inconsistency in protoc-gen-star, not a typo.
func cliParseExpr(f pgs.Field, argIdx int) string {
	arg := fmt.Sprintf("os.Args[%d]", argIdx)
	name := strings.ToLower(f.Name().String())
	switch f.Type().ProtoType() {
	case pgs.StringT:
		return arg
	case pgs.Int32T, pgs.SInt32, pgs.SFixed32:
		return fmt.Sprintf("mustParseInt32(%s, %q)", arg, name)
	case pgs.Int64T, pgs.SInt64, pgs.SFixed64:
		return fmt.Sprintf("mustParseInt64(%s, %q)", arg, name)
	case pgs.UInt32T, pgs.Fixed32T:
		return fmt.Sprintf("mustParseUint32(%s, %q)", arg, name)
	case pgs.UInt64T, pgs.Fixed64T:
		return fmt.Sprintf("mustParseUint64(%s, %q)", arg, name)
	case pgs.FloatT:
		return fmt.Sprintf("float32(mustParseFloat(%s, 32, %q))", arg, name)
	case pgs.DoubleT:
		return fmt.Sprintf("mustParseFloat(%s, 64, %q)", arg, name)
	case pgs.BoolT:
		return fmt.Sprintf("mustParseBool(%s, %q)", arg, name)
	case pgs.BytesT:
		return fmt.Sprintf("mustReadFile(%s, %q)", arg, name)
	default:
		return arg
	}
}

// cliQueryParseExpr returns a Go expression to parse a string variable into
// the correct type for a query parameter. Like cliParseExpr but takes a
// variable name instead of an os.Args index.
func cliQueryParseExpr(f pgs.Field, varName string) string {
	name := strings.ToLower(f.Name().String())
	switch f.Type().ProtoType() {
	case pgs.StringT:
		return varName
	case pgs.Int32T, pgs.SInt32, pgs.SFixed32:
		return fmt.Sprintf("mustParseInt32(%s, %q)", varName, name)
	case pgs.Int64T, pgs.SInt64, pgs.SFixed64:
		return fmt.Sprintf("mustParseInt64(%s, %q)", varName, name)
	case pgs.UInt32T, pgs.Fixed32T:
		return fmt.Sprintf("mustParseUint32(%s, %q)", varName, name)
	case pgs.UInt64T, pgs.Fixed64T:
		return fmt.Sprintf("mustParseUint64(%s, %q)", varName, name)
	case pgs.FloatT:
		return fmt.Sprintf("float32(mustParseFloat(%s, 32, %q))", varName, name)
	case pgs.DoubleT:
		return fmt.Sprintf("mustParseFloat(%s, 64, %q)", varName, name)
	case pgs.BoolT:
		return fmt.Sprintf("mustParseBool(%s, %q)", varName, name)
	default:
		return varName
	}
}

// queryParseExpr returns a Go expression that parses a string variable into
// the field's Go type, returning (value, error). Used in generated query handlers.
// Panics at generation time for unsupported field types.
func queryParseExpr(f pgs.Field, varName string) string {
	switch f.Type().ProtoType() {
	case pgs.StringT:
		return fmt.Sprintf("parseQueryParamString(%s)", varName)
	case pgs.Int32T, pgs.SInt32, pgs.SFixed32:
		return fmt.Sprintf("parseQueryParamInt32(%s)", varName)
	case pgs.Int64T, pgs.SInt64, pgs.SFixed64:
		return fmt.Sprintf("parseQueryParamInt64(%s)", varName)
	case pgs.UInt32T, pgs.Fixed32T:
		return fmt.Sprintf("parseQueryParamUint32(%s)", varName)
	case pgs.UInt64T, pgs.Fixed64T:
		return fmt.Sprintf("parseQueryParamUint64(%s)", varName)
	case pgs.BoolT:
		return fmt.Sprintf("parseQueryParamBool(%s)", varName)
	case pgs.FloatT:
		return fmt.Sprintf("parseQueryParamFloat32(%s)", varName)
	case pgs.DoubleT:
		return fmt.Sprintf("parseQueryParamFloat64(%s)", varName)
	default:
		panic(fmt.Sprintf("queryParseExpr: unsupported field type %s for field %s — GSI key fields must be scalar types", f.Type().ProtoType(), f.Name()))
	}
}

// queryFormatExpr returns a Go expression that formats a typed value as a string
// for use in HTTP query parameters. Used in generated HTTP client methods.
func queryFormatExpr(f pgs.Field, varName string) string {
	switch f.Type().ProtoType() {
	case pgs.StringT:
		return varName
	case pgs.Int32T, pgs.SInt32, pgs.SFixed32:
		return fmt.Sprintf("strconv.FormatInt(int64(%s), 10)", varName)
	case pgs.Int64T, pgs.SInt64, pgs.SFixed64:
		return fmt.Sprintf("strconv.FormatInt(%s, 10)", varName)
	case pgs.UInt32T, pgs.Fixed32T:
		return fmt.Sprintf("strconv.FormatUint(uint64(%s), 10)", varName)
	case pgs.UInt64T, pgs.Fixed64T:
		return fmt.Sprintf("strconv.FormatUint(%s, 10)", varName)
	case pgs.BoolT:
		return fmt.Sprintf("strconv.FormatBool(%s)", varName)
	case pgs.FloatT:
		return fmt.Sprintf("strconv.FormatFloat(float64(%s), 'f', -1, 32)", varName)
	case pgs.DoubleT:
		return fmt.Sprintf("strconv.FormatFloat(%s, 'f', -1, 64)", varName)
	default:
		panic(fmt.Sprintf("queryFormatExpr: unsupported field type %s for field %s", f.Type().ProtoType(), f.Name()))
	}
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

// validateFileStructure enforces structural guardrails on the proto file:
//   - Exactly one aggregate message
//   - Aggregate must be the first message
//   - Exactly one CREATION command
//   - Non-first events in a CREATION command must also be produced by a MUTATION command
//   - At most one snapshot message
//   - Projection messages must have an "id" field
func (p *ProtosourceModule) validateFileStructure(f pgs.File) error {
	// Count aggregates and verify the first message is the aggregate.
	aggregateCount := 0
	for _, m := range f.Messages() {
		if p.isAggregate(m) {
			aggregateCount++
		}
	}
	if aggregateCount == 0 {
		return fmt.Errorf("file %s: no aggregate message defined", f.Name())
	}
	if aggregateCount > 1 {
		return fmt.Errorf("file %s: exactly one aggregate per file, found %d", f.Name(), aggregateCount)
	}
	if !p.isAggregate(f.Messages()[0]) {
		return fmt.Errorf("file %s: aggregate must be the first message, but %s is not an aggregate", f.Name(), f.Messages()[0].Name())
	}

	// Exactly one CREATION command.
	creationCount := 0
	var creationCmd pgs.Message
	for _, m := range f.Messages() {
		if p.isCommand(m) && p.commandLifecycle(m) == "CREATION" {
			creationCount++
			creationCmd = m
		}
	}
	if creationCount != 1 {
		return fmt.Errorf("file %s: exactly one CREATION command required, found %d", f.Name(), creationCount)
	}

	// Non-first events in a CREATION command must also be produced by a MUTATION command.
	if creationCmd != nil {
		events := p.commandEvents(creationCmd)
		if len(events) > 1 {
			// Build set of events produced by MUTATION commands.
			mutationEvents := make(map[string]bool)
			for _, m := range f.Messages() {
				if p.isCommand(m) && p.commandLifecycle(m) != "CREATION" {
					for _, e := range p.commandEvents(m) {
						mutationEvents[e] = true
					}
				}
			}
			for _, eventName := range events[1:] {
				if !mutationEvents[eventName] {
					return fmt.Errorf("file %s: CREATION command %s produces event %q which is not produced by any MUTATION command — every non-first creation event must also be a standalone command",
						f.Name(), creationCmd.Name(), eventName)
				}
			}
		}
	}

	// At most one snapshot.
	snapshotCount := 0
	for _, m := range f.Messages() {
		if p.isSnapshot(m) {
			snapshotCount++
		}
	}
	if snapshotCount > 1 {
		return fmt.Errorf("file %s: at most one snapshot per file, found %d", f.Name(), snapshotCount)
	}

	// Projection messages must have an "id" field.
	for _, m := range f.Messages() {
		if p.isProjection(m) {
			hasID := false
			for _, field := range m.Fields() {
				if field.Name().String() == "id" {
					hasID = true
					break
				}
			}
			if !hasID {
				return fmt.Errorf("file %s: projection %s must have an \"id\" field (required for PK derivation)", f.Name(), m.Name())
			}
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

	// Wire templates go into <aggregate><version><store>/ subdirectories.
	if strings.HasPrefix(tpl.Name(), "wire_") {
		name := strings.TrimSuffix(strings.TrimPrefix(tpl.Name(), "wire_"), ".gotext")
		return p.wireOutputPath(f, importPath, name, "providers.go")
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
	parent := filepath.Dir(out)
	return filepath.Join(parent, dir, "main.go")
}

// wireOutputPath returns the output path for a wire template, placing it in
// a <aggregate><version><store>/ subdirectory (e.g., "example/app/test/v1/testv1memory/providers.go").
func (p *ProtosourceModule) wireOutputPath(f pgs.File, importPath, store, filename string) string {
	aggregateName := ""
	for _, m := range f.Messages() {
		if p.isAggregate(m) {
			aggregateName = strings.ToLower(m.Name().String())
			break
		}
	}
	if aggregateName == "" {
		aggregateName = "wire"
	}

	version := lastPathComponent(p.routePrefix(f))
	dir := aggregateName + version + store

	if mod := p.params.Str("module"); mod != "" {
		rel := strings.TrimPrefix(importPath, mod)
		rel = strings.TrimPrefix(rel, "/")
		return rel + "/" + dir + "/" + filename
	}

	out := p.ctx.OutputPath(f).String()
	parent := filepath.Dir(out)
	return filepath.Join(parent, dir, filename)
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

// lastPathComponent returns the last component of a path string (e.g., "v1" from "example/app/test/v1")
func lastPathComponent(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
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

	// ── File-level structural guardrails ──
	if err := p.validateFileStructure(f); err != nil {
		p.Fail(err.Error())
		return
	}

	p.enumValueIndex = buildEnumValueIndex(f)

	agg := p.aggregateForFile(f)

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
			if agg != nil {
				if err := p.validateCollectionMapping(m, agg, f); err != nil {
					p.Fail(err.Error())
					return
				}
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

	// Validate projection fields match aggregate fields by name and type.
	if agg != nil {
		for _, proj := range p.projectionsForFile(f) {
			if err := p.validateProjectionFields(proj, agg); err != nil {
				p.Fail(err.Error())
				return
			}
		}
	}

	// Validate that {Aggregate}List message exists with repeated {Aggregate} items = 1.
	if agg != nil {
		if err := p.validateAggregateList(f, agg); err != nil {
			p.Fail(err.Error())
			return
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

// hasOpaqueAnnotations returns true if the message should get AutoPKSK methods.
// Aggregates and projections get PK/SK automatically; others need explicit field annotations.
func (p *ProtosourceModule) hasOpaqueAnnotations(m pgs.Message) bool {
	if p.isAggregate(m) || p.isProjection(m) {
		return true
	}
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

	// Aggregates and projections get PK/SK automatically — reject explicit PK/SK annotations.
	if p.isAggregate(m) || p.isProjection(m) {
		kind := "aggregate"
		if p.isProjection(m) {
			kind = "projection"
		}
		if _, ok := mappings[optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_PK]; ok {
			return fmt.Errorf("message %s: %s PK is automatic (derived from package + id) — remove OPAQUE_KEY_TYPE_PK annotations", m.Name(), kind)
		}
		if _, ok := mappings[optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_SK]; ok {
			skVal := "AGG"
			if p.isProjection(m) {
				skVal = "PROJ#" + m.Name().String()
			}
			return fmt.Errorf("message %s: %s SK is automatic (%q) — remove OPAQUE_KEY_TYPE_SK annotations", m.Name(), kind, skVal)
		}
		if len(mappings) == 0 {
			return nil
		}
	}

	if len(mappings) == 0 {
		return nil
	}

	// Non-aggregates/projections must have at least PK
	if !p.isAggregate(m) && !p.isProjection(m) {
		if _, ok := mappings[optionsv1.OpaqueKeyType_OPAQUE_KEY_TYPE_PK]; !ok {
			return fmt.Errorf("message %s: opaque annotations present but no PK field defined", m.Name())
		}
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

// opaquePKFields returns the fields that compose the table PK.
// For aggregates and projections, PK is always derived from the id field.
// For other messages, PK comes from explicit OPAQUE_KEY_TYPE_PK annotations.
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

// unexport lowercases the first character of a Go identifier, preserving
// internal casing: "CustomerId" -> "customerId", "ID" -> "iD".
func unexport(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// queryRoutePath builds a URL path segment from GSI PK field names.
// e.g., fields [CustomerId] -> "by-customer-id", fields [TenantId, RegionId] -> "by-tenant-id-and-region-id"
func queryRoutePath(fields []opaqueFieldMapping) string {
	parts := make([]string, len(fields))
	for i, fm := range fields {
		parts[i] = strings.ReplaceAll(strings.ToLower(fm.Field.Name().String()), "_", "-")
	}
	return "by-" + strings.Join(parts, "-and-")
}

// aggregateForFile returns the aggregate message in the same file as the given message.
func (p *ProtosourceModule) aggregateForFile(f pgs.File) pgs.Message {
	for _, m := range f.Messages() {
		if p.isAggregate(m) {
			return m
		}
	}
	return nil
}

// projectionSKValue returns the SK value for a projection: "PROJ#MessageName".
func (p *ProtosourceModule) projectionSKValue(m pgs.Message) string {
	return "PROJ#" + m.Name().String()
}

// projectionMatchingFields returns the projection fields whose names match aggregate fields.
// Used to generate the Project() method that copies fields from aggregate to projection.
type projectionFieldPair struct {
	ProjectionField pgs.Field
	AggregateField  pgs.Field
}

func (p *ProtosourceModule) projectionMatchingFields(proj pgs.Message, agg pgs.Message) []projectionFieldPair {
	aggFields := make(map[string]pgs.Field)
	for _, f := range agg.Fields() {
		aggFields[f.Name().String()] = f
	}
	var pairs []projectionFieldPair
	for _, pf := range proj.Fields() {
		if af, ok := aggFields[pf.Name().String()]; ok {
			pairs = append(pairs, projectionFieldPair{
				ProjectionField: pf,
				AggregateField:  af,
			})
		}
	}
	return pairs
}

// validateProjectionFields checks that all projection fields match aggregate fields
// by name AND proto type. Returns an error listing any mismatches.
func (p *ProtosourceModule) validateProjectionFields(proj pgs.Message, agg pgs.Message) error {
	aggFields := make(map[string]pgs.Field)
	for _, f := range agg.Fields() {
		aggFields[f.Name().String()] = f
	}
	var errs []string
	for _, pf := range proj.Fields() {
		af, ok := aggFields[pf.Name().String()]
		if !ok {
			errs = append(errs, fmt.Sprintf("field %q: not found on aggregate %s", pf.Name(), agg.Name()))
			continue
		}
		if pf.Type().ProtoType() != af.Type().ProtoType() {
			errs = append(errs, fmt.Sprintf("field %q: type mismatch — projection has %s, aggregate %s has %s",
				pf.Name(), pf.Type().ProtoType(), agg.Name(), af.Type().ProtoType()))
			continue
		}
		if pf.Type().IsRepeated() != af.Type().IsRepeated() {
			errs = append(errs, fmt.Sprintf("field %q: repeated mismatch — projection repeated=%v, aggregate %s repeated=%v",
				pf.Name(), pf.Type().IsRepeated(), agg.Name(), af.Type().IsRepeated()))
			continue
		}
		if pf.Type().IsMap() != af.Type().IsMap() {
			errs = append(errs, fmt.Sprintf("field %q: map mismatch — projection map=%v, aggregate %s map=%v",
				pf.Name(), pf.Type().IsMap(), agg.Name(), af.Type().IsMap()))
			continue
		}
		// For map types, verify key and value types match.
		if pf.Type().IsMap() && af.Type().IsMap() {
			if pf.Type().Key().ProtoType() != af.Type().Key().ProtoType() {
				errs = append(errs, fmt.Sprintf("field %q: map key type mismatch — projection has %s, aggregate %s has %s",
					pf.Name(), pf.Type().Key().ProtoType(), agg.Name(), af.Type().Key().ProtoType()))
			}
			if pf.Type().Element().ProtoType() != af.Type().Element().ProtoType() {
				errs = append(errs, fmt.Sprintf("field %q: map value type mismatch — projection has %s, aggregate %s has %s",
					pf.Name(), pf.Type().Element().ProtoType(), agg.Name(), af.Type().Element().ProtoType()))
			} else if pf.Type().Element().IsEmbed() && af.Type().Element().IsEmbed() {
				if pf.Type().Element().Embed().FullyQualifiedName() != af.Type().Element().Embed().FullyQualifiedName() {
					errs = append(errs, fmt.Sprintf("field %q: map value message mismatch — projection has %s, aggregate has %s",
						pf.Name(), pf.Type().Element().Embed().FullyQualifiedName(), af.Type().Element().Embed().FullyQualifiedName()))
				}
			}
			continue
		}
		// For message/enum types, verify the underlying type name matches.
		if pf.Type().IsEmbed() && af.Type().IsEmbed() {
			if pf.Type().Embed().FullyQualifiedName() != af.Type().Embed().FullyQualifiedName() {
				errs = append(errs, fmt.Sprintf("field %q: message type mismatch — projection has %s, aggregate has %s",
					pf.Name(), pf.Type().Embed().FullyQualifiedName(), af.Type().Embed().FullyQualifiedName()))
			}
		}
		if pf.Type().IsEnum() && af.Type().IsEnum() {
			if pf.Type().Enum().FullyQualifiedName() != af.Type().Enum().FullyQualifiedName() {
				errs = append(errs, fmt.Sprintf("field %q: enum type mismatch — projection has %s, aggregate has %s",
					pf.Name(), pf.Type().Enum().FullyQualifiedName(), af.Type().Enum().FullyQualifiedName()))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("projection %s: %s", proj.Name(), strings.Join(errs, "; "))
	}
	return nil
}

// validateAggregateList checks that a {Aggregate}List message exists with
// a single field: repeated {Aggregate} items = 1.
func (p *ProtosourceModule) validateAggregateList(f pgs.File, agg pgs.Message) error {
	listName := agg.Name().String() + "List"
	var listMsg pgs.Message
	for _, m := range f.Messages() {
		if m.Name().String() == listName {
			listMsg = m
			break
		}
	}
	if listMsg == nil {
		return fmt.Errorf("missing required message %s: aggregates must have a corresponding "+
			"%s message with `repeated %s items = 1` for query result serialization",
			listName, listName, agg.Name())
	}
	fields := listMsg.Fields()
	if len(fields) != 1 {
		return fmt.Errorf("%s must have exactly 1 field, got %d", listName, len(fields))
	}
	f0 := fields[0]
	if f0.Name().String() != "items" {
		return fmt.Errorf("%s field must be named \"items\", got %q", listName, f0.Name())
	}
	if !f0.Type().IsRepeated() {
		return fmt.Errorf("%s.items must be repeated", listName)
	}
	if f0.Descriptor().GetNumber() != 1 {
		return fmt.Errorf("%s.items must be field number 1, got %d", listName, f0.Descriptor().GetNumber())
	}
	elem := f0.Type().Element()
	if elem == nil || !elem.IsEmbed() {
		return fmt.Errorf("%s.items must be a message type (repeated %s)", listName, agg.Name())
	}
	if elem.Embed().FullyQualifiedName() != agg.FullyQualifiedName() {
		return fmt.Errorf("%s.items must be repeated %s, got %s",
			listName, agg.Name(), f0.Type().Element().Embed().Name())
	}
	return nil
}

// projectionsForFile returns all projection messages in the file.
func (p *ProtosourceModule) projectionsForFile(f pgs.File) []pgs.Message {
	var projs []pgs.Message
	for _, m := range f.Messages() {
		if p.isProjection(m) {
			projs = append(projs, m)
		}
	}
	return projs
}
