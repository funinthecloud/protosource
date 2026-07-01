package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	optionsv1 "github.com/funinthecloud/protosource/gen/options/v1"
	pgs "github.com/lyft/protoc-gen-star/v2"
	pgsgo "github.com/lyft/protoc-gen-star/v2/lang/go"
	"github.com/lyft/protoc-gen-star/v2/testutils"
)

// repoRoot returns the absolute path to the repository root, computed relative
// to this test file so the tests work regardless of working directory.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// loadTestProto compiles a proto in testdata/ via protoc and returns the
// target pgs.File. The proto directory is added as an import path so
// the options proto resolves.
func loadTestProto(t *testing.T, name string) pgs.File {
	t.Helper()
	root := repoRoot()

	loader := testutils.Loader{
		ImportPaths: []string{
			filepath.Join(root, "proto"),
			filepath.Join(root, "cmd", "protoc-gen-protosource"),
		},
	}

	ast := loader.LoadProtos(t, filepath.Join(root, "cmd", "protoc-gen-protosource", "testdata", name))

	for _, f := range ast.Targets() {
		if strings.Contains(f.InputPath().String(), name) {
			return f
		}
	}
	// Fallback: search all packages.
	for _, pkg := range ast.Packages() {
		for _, f := range pkg.Files() {
			if strings.Contains(f.InputPath().String(), name) {
				return f
			}
		}
	}
	t.Fatalf("target file %q not found in AST; targets: %v", name, ast.Targets())
	return nil
}

// newModule creates a ProtosourceModule suitable for testing validation.
func newModule() *ProtosourceModule {
	p := &ProtosourceModule{ModuleBase: &pgs.ModuleBase{}}
	p.ctx = pgsgo.InitContext(pgs.Parameters{})
	return p
}

// findMessage returns the first message in f whose name matches.
func findMessage(f pgs.File, name string) pgs.Message {
	for _, m := range f.Messages() {
		if m.Name().String() == name {
			return m
		}
	}
	return nil
}

func TestRoutePrefix(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()

	got := p.routePrefix(f)
	want := "test/valid"
	if got != want {
		t.Errorf("routePrefix() = %q, want %q (must derive from proto package, not Go import path)", got, want)
	}
}

func TestBuildEnumValueIndex(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	idx := buildEnumValueIndex(f)

	want := map[string]string{
		"STATE_UNSPECIFIED": "State_STATE_UNSPECIFIED",
		"STATE_ACTIVE":      "State_STATE_ACTIVE",
		"STATE_LOCKED":      "State_STATE_LOCKED",
	}
	for k, v := range want {
		got, ok := idx[k]
		if !ok {
			t.Errorf("missing key %q in index", k)
		} else if got != v {
			t.Errorf("index[%q] = %q, want %q", k, got, v)
		}
	}
	if len(idx) != len(want) {
		t.Errorf("index has %d entries, want %d", len(idx), len(want))
	}
}

func TestValidateSetsState_Valid(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	for _, name := range []string{"Activated", "Locked"} {
		m := findMessage(f, name)
		if m == nil {
			t.Fatalf("message %q not found", name)
		}
		if err := p.validateSetsState(m); err != nil {
			t.Errorf("validateSetsState(%s) unexpected error: %v", name, err)
		}
	}
}

func TestValidateSetsState_NoAnnotation(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	m := findMessage(f, "Created")
	if m == nil {
		t.Fatal("message Created not found")
	}
	if err := p.validateSetsState(m); err != nil {
		t.Errorf("validateSetsState(Created) unexpected error: %v", err)
	}
}

func TestValidateSetsState_Invalid(t *testing.T) {
	f := loadTestProto(t, "invalid_sets_state.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	m := findMessage(f, "Created")
	if m == nil {
		t.Fatal("message Created not found")
	}
	err := p.validateSetsState(m)
	if err == nil {
		t.Fatal("expected error for invalid sets_state, got nil")
	}
	if !strings.Contains(err.Error(), "STATE_BOGUS") {
		t.Errorf("error should mention STATE_BOGUS, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Created") {
		t.Errorf("error should mention message name Created, got: %v", err)
	}
}

func TestValidateAllowedStates_Valid(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	m := findMessage(f, "Lock")
	if m == nil {
		t.Fatal("message Lock not found")
	}
	if err := p.validateAllowedStates(m); err != nil {
		t.Errorf("validateAllowedStates(Lock) unexpected error: %v", err)
	}
}

func TestValidateAllowedStates_NoAnnotation(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	if err := p.validateAllowedStates(m); err != nil {
		t.Errorf("validateAllowedStates(Create) unexpected error: %v", err)
	}
}

func TestValidateAllowedStates_Invalid(t *testing.T) {
	f := loadTestProto(t, "invalid_allowed_states.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	m := findMessage(f, "Update")
	if m == nil {
		t.Fatal("message Update not found")
	}
	err := p.validateAllowedStates(m)
	if err == nil {
		t.Fatal("expected error for invalid allowed_states, got nil")
	}
	if !strings.Contains(err.Error(), "STATE_NONEXISTENT") {
		t.Errorf("error should mention STATE_NONEXISTENT, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Update") {
		t.Errorf("error should mention message name Update, got: %v", err)
	}
}

func TestStateEnumName_UsesIndex(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()
	p.enumValueIndex = buildEnumValueIndex(f)

	// stateEnumName should return the Go-qualified name from the index.
	got := p.stateEnumName("STATE_LOCKED", f)
	if got != "State_STATE_LOCKED" {
		t.Errorf("stateEnumName(STATE_LOCKED) = %q, want %q", got, "State_STATE_LOCKED")
	}

	// Non-existent value should return empty string.
	got = p.stateEnumName("STATE_BOGUS", f)
	if got != "" {
		t.Errorf("stateEnumName(STATE_BOGUS) = %q, want empty", got)
	}
}

func TestValidateMessageNamesAgainstOpaque(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		f := loadTestProto(t, "conflicting_opaque_name.proto")
		p := newModule()

		err := p.validateMessageNamesAgainstOpaque(f)
		if err == nil {
			t.Fatal("expected error for conflicting message name GSI1PK, got nil")
		}
		if !strings.Contains(err.Error(), "GSI1PK") {
			t.Errorf("error should mention GSI1PK, got: %v", err)
		}
	})

	t.Run("no_conflict", func(t *testing.T) {
		f := loadTestProto(t, "valid.proto")
		p := newModule()

		err := p.validateMessageNamesAgainstOpaque(f)
		if err != nil {
			t.Errorf("unexpected error for valid proto: %v", err)
		}
	})
}

func TestOpaqueReservedNames(t *testing.T) {
	names := opaqueReservedNames()
	// 42 key slots + Hydrate = 43
	if len(names) != 43 {
		t.Errorf("expected 43 reserved names, got %d", len(names))
	}
	for _, want := range []string{"PK", "SK", "GSI1PK", "GSI1SK", "GSI20PK", "GSI20SK", "Hydrate"} {
		if !names[want] {
			t.Errorf("expected %q in reserved names", want)
		}
	}
}


// ── Collection validation tests ──

func TestValidateCollectionMapping_ValidAdd(t *testing.T) {
	f := loadTestProto(t, "collection_valid.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	if agg == nil {
		t.Fatal("aggregate Basket not found")
	}

	evt := findMessage(f, "WidgetAdded")
	if evt == nil {
		t.Fatal("event WidgetAdded not found")
	}

	if err := p.validateCollectionMapping(evt, agg, f); err != nil {
		t.Errorf("validateCollectionMapping(WidgetAdded) unexpected error: %v", err)
	}
}

func TestValidateCollectionMapping_ValidRemove(t *testing.T) {
	f := loadTestProto(t, "collection_valid.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	if agg == nil {
		t.Fatal("aggregate Basket not found")
	}

	evt := findMessage(f, "WidgetRemoved")
	if evt == nil {
		t.Fatal("event WidgetRemoved not found")
	}

	if err := p.validateCollectionMapping(evt, agg, f); err != nil {
		t.Errorf("validateCollectionMapping(WidgetRemoved) unexpected error: %v", err)
	}
}

func TestValidateCollectionMapping_NoCollection(t *testing.T) {
	f := loadTestProto(t, "collection_valid.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	evt := findMessage(f, "Created")
	if agg == nil || evt == nil {
		t.Fatal("messages not found")
	}

	// Events without collection annotations should pass validation.
	if err := p.validateCollectionMapping(evt, agg, f); err != nil {
		t.Errorf("validateCollectionMapping(Created) unexpected error: %v", err)
	}
}

func TestValidateCollectionMapping_BadTarget(t *testing.T) {
	f := loadTestProto(t, "collection_bad_target.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	evt := findMessage(f, "WidgetAdded")
	if agg == nil || evt == nil {
		t.Fatal("messages not found")
	}

	err := p.validateCollectionMapping(evt, agg, f)
	if err == nil {
		t.Fatal("expected error for bad target, got nil")
	}
	if !strings.Contains(err.Error(), "widgets") {
		t.Errorf("error should mention target field 'widgets', got: %v", err)
	}
}

func TestValidateCollectionMapping_MissingKeyField(t *testing.T) {
	f := loadTestProto(t, "collection_missing_key.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	evt := findMessage(f, "WidgetAdded")
	if agg == nil || evt == nil {
		t.Fatal("messages not found")
	}

	err := p.validateCollectionMapping(evt, agg, f)
	if err == nil {
		t.Fatal("expected error for missing key_field, got nil")
	}
	if !strings.Contains(err.Error(), "key_field") {
		t.Errorf("error should mention key_field, got: %v", err)
	}
}


// ── Projection map validation tests ──

func TestValidateProjectionFields_MapValid(t *testing.T) {
	f := loadTestProto(t, "projection_map_valid.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	proj := findMessage(f, "BasketSummary")
	if agg == nil || proj == nil {
		t.Fatal("messages not found")
	}

	if err := p.validateProjectionFields(proj, agg); err != nil {
		t.Errorf("validateProjectionFields unexpected error: %v", err)
	}
}

func TestValidateProjectionFields_MapValueMessageMismatch(t *testing.T) {
	f := loadTestProto(t, "projection_map_value_mismatch.proto")
	p := newModule()

	agg := findMessage(f, "Basket")
	proj := findMessage(f, "BasketView")
	if agg == nil || proj == nil {
		t.Fatal("messages not found")
	}

	err := p.validateProjectionFields(proj, agg)
	if err == nil {
		t.Fatal("expected error for map value message mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "map value message mismatch") {
		t.Errorf("error should mention map value message mismatch, got: %v", err)
	}
}

// --- Singular embedded message convention (by-name) ---

func TestValidateSingularEmbed_NameMatch(t *testing.T) {
	f := loadTestProto(t, "singular_embed_valid.proto")
	p := newModule()
	agg := findMessage(f, "Account")
	if agg == nil {
		t.Fatal("aggregate Account not found")
	}
	// Both set and clear events name their embed field to match the aggregate
	// field (profile), so On()'s by-name copy applies them — no error.
	for _, name := range []string{"ProfileSet", "ProfileCleared"} {
		evt := findMessage(f, name)
		if evt == nil {
			t.Fatalf("event %s not found", name)
		}
		if err := p.validateSingularEmbed(evt, agg); err != nil {
			t.Errorf("validateSingularEmbed(%s) unexpected error: %v", name, err)
		}
	}
}

func TestValidateSingularEmbed_NameMismatch(t *testing.T) {
	f := loadTestProto(t, "singular_embed_mismatch.proto")
	p := newModule()
	agg := findMessage(f, "Account")
	evt := findMessage(f, "ProfileSet")
	if agg == nil || evt == nil {
		t.Fatal("messages not found")
	}
	// Event carries a Profile under the wrong name ("config"); the aggregate
	// field is "profile". The by-name copy would silently skip it, so codegen
	// must fail with a rename hint.
	err := p.validateSingularEmbed(evt, agg)
	if err == nil {
		t.Fatal("expected error for name mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "config") || !strings.Contains(err.Error(), "profile") {
		t.Errorf("error should name the offending field 'config' and the target 'profile', got: %v", err)
	}
}

func TestValidateSingularEmbed_NameMismatch_MultipleCandidates(t *testing.T) {
	f := loadTestProto(t, "singular_embed_dual.proto")
	p := newModule()
	agg := findMessage(f, "Account")
	evt := findMessage(f, "ProfileSet")
	if agg == nil || evt == nil {
		t.Fatal("messages not found")
	}
	// The aggregate has two Profile fields (primary, backup); the rename hint
	// must list both, not an arbitrary one.
	err := p.validateSingularEmbed(evt, agg)
	if err == nil {
		t.Fatal("expected error for name mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "primary") || !strings.Contains(err.Error(), "backup") {
		t.Errorf("error should list both candidate fields 'primary' and 'backup', got: %v", err)
	}
}

func fieldByName(t *testing.T, m pgs.Message, name string) pgs.Field {
	t.Helper()
	for _, f := range m.Fields() {
		if f.Name().String() == name {
			return f
		}
	}
	t.Fatalf("field %q not found on %s", name, m.Name())
	return nil
}

func TestCommandEventArg(t *testing.T) {
	f := loadTestProto(t, "command_event_arg.proto")
	p := newModule()
	cmd := findMessage(f, "DoThing")
	evt := findMessage(f, "ThingDone")
	if cmd == nil || evt == nil {
		t.Fatal("messages not found")
	}

	// Field present on the command -> forwarded getter.
	if got := p.commandEventArg(fieldByName(t, evt, "actor"), cmd); got != "m.GetActor()" {
		t.Errorf("actor (present): got %q, want m.GetActor()", got)
	}
	// Embedded message absent on the command -> nil (the clear pattern).
	if got := p.commandEventArg(fieldByName(t, evt, "profile"), cmd); got != "nil" {
		t.Errorf("profile (absent embed): got %q, want nil", got)
	}
	// Scalar absent on the command -> a command getter that does not exist, so
	// codegen fails to compile rather than silently emitting a zero value.
	if got := p.commandEventArg(fieldByName(t, evt, "note"), cmd); got != "m.GetNote()" {
		t.Errorf("note (absent scalar): got %q, want m.GetNote() (fail-fast)", got)
	}
}

// Ensure the optionsv1 import is used (extensions must be registered).
var _ = optionsv1.E_ProtosourceMessageType
