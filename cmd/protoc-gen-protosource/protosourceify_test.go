package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pgs "github.com/lyft/protoc-gen-star/v2"
	"github.com/lyft/protoc-gen-star/v2/testutils"

	optionsv1 "github.com/funinthecloud/protosource/options/v1"
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
	return &ProtosourceModule{ModuleBase: &pgs.ModuleBase{}}
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

func TestValidateCLICommandFields_StringOnly(t *testing.T) {
	f := loadTestProto(t, "cli_valid.proto")

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	if err := validateCLICommandFields(m); err != nil {
		t.Errorf("expected no error for string-only fields, got: %v", err)
	}
}

func TestValidateCLICommandFields_NoExtraFields(t *testing.T) {
	f := loadTestProto(t, "valid.proto")

	// Create only has id + actor, so zero CLI fields — should pass.
	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	if err := validateCLICommandFields(m); err != nil {
		t.Errorf("expected no error for id-and-actor-only command, got: %v", err)
	}
}

func TestValidateCLICommandFields_Int64Accepted(t *testing.T) {
	f := loadTestProto(t, "cli_invalid_int.proto") // has int64 "count" field

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	if err := validateCLICommandFields(m); err != nil {
		t.Errorf("expected no error for int64 field, got: %v", err)
	}
}

func TestValidateCLICommandFields_EnumRejected(t *testing.T) {
	f := loadTestProto(t, "cli_invalid_enum.proto")

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	err := validateCLICommandFields(m)
	if err == nil {
		t.Fatal("expected error for enum field, got nil")
	}
	if !strings.Contains(err.Error(), "priority") {
		t.Errorf("error should mention field name 'priority', got: %v", err)
	}
	if !strings.Contains(err.Error(), "enum") {
		t.Errorf("error should mention 'enum', got: %v", err)
	}
}

func TestValidateCLICommandFields_RepeatedRejected(t *testing.T) {
	f := loadTestProto(t, "cli_invalid_repeated.proto")

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	err := validateCLICommandFields(m)
	if err == nil {
		t.Fatal("expected error for repeated field, got nil")
	}
	if !strings.Contains(err.Error(), "tags") {
		t.Errorf("error should mention field name 'tags', got: %v", err)
	}
}

func TestValidateCLICommandFields_MessageRejected(t *testing.T) {
	f := loadTestProto(t, "cli_invalid_message.proto")

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}
	err := validateCLICommandFields(m)
	if err == nil {
		t.Fatal("expected error for message field, got nil")
	}
	if !strings.Contains(err.Error(), "metadata") {
		t.Errorf("error should mention field name 'metadata', got: %v", err)
	}
	if !strings.Contains(err.Error(), "message type") {
		t.Errorf("error should mention 'message type', got: %v", err)
	}
}

func TestCLIOutputPath_WithModule(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()
	p.params = pgs.Parameters{"module": "github.com/funinthecloud/protosource"}

	// valid.proto has aggregate "Thing", so directory should be "thingmgr".
	got := p.cliOutputPath(f, "github.com/funinthecloud/protosource/test/valid")
	want := "test/valid/thingmgr/main.go"
	if got != want {
		t.Errorf("cliOutputPath = %q, want %q", got, want)
	}
}

func TestCLIOutputPath_WithModule_CLIValid(t *testing.T) {
	f := loadTestProto(t, "cli_valid.proto")
	p := newModule()
	p.params = pgs.Parameters{"module": "github.com/funinthecloud/protosource"}

	// cli_valid.proto has aggregate "Widget".
	got := p.cliOutputPath(f, "github.com/funinthecloud/protosource/test/cli_valid")
	want := "test/cli_valid/widgetmgr/main.go"
	if got != want {
		t.Errorf("cliOutputPath = %q, want %q", got, want)
	}
}

func TestCLIOutputPath_NoAggregate(t *testing.T) {
	f := loadTestProto(t, "cli_invalid_int.proto")
	p := newModule()
	p.params = pgs.Parameters{"module": "github.com/funinthecloud/protosource"}

	// cli_invalid_int.proto has aggregate "Gadget".
	got := p.cliOutputPath(f, "github.com/funinthecloud/protosource/test/cli_invalid_int")
	want := "test/cli_invalid_int/gadgetmgr/main.go"
	if got != want {
		t.Errorf("cliOutputPath = %q, want %q", got, want)
	}
}

func TestCLIParseExpr_Types(t *testing.T) {
	f := loadTestProto(t, "cli_invalid_int.proto") // has int64 "count" field

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}

	// Find the "count" field (int64, field index 0 after filtering id+actor).
	fields := CLICommandFields(m.Fields())
	if len(fields) != 1 {
		t.Fatalf("expected 1 CLI field, got %d", len(fields))
	}

	got := cliParseExpr(fields[0], 3)
	if !strings.Contains(got, "mustParseInt64") {
		t.Errorf("expected mustParseInt64 for int64 field, got: %s", got)
	}
	if !strings.Contains(got, "os.Args[3]") {
		t.Errorf("expected os.Args[3] in expression, got: %s", got)
	}
}

func TestCLIParseExpr_String(t *testing.T) {
	f := loadTestProto(t, "cli_valid.proto")

	m := findMessage(f, "Create")
	if m == nil {
		t.Fatal("message Create not found")
	}

	// First CLI field is "body" (string).
	fields := CLICommandFields(m.Fields())
	if len(fields) < 1 {
		t.Fatal("expected at least 1 CLI field")
	}

	got := cliParseExpr(fields[0], 3)
	if got != "os.Args[3]" {
		t.Errorf("expected os.Args[3] for string field, got: %s", got)
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

	if err := p.validateCollectionMapping(evt, agg); err != nil {
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

	if err := p.validateCollectionMapping(evt, agg); err != nil {
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
	if err := p.validateCollectionMapping(evt, agg); err != nil {
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

	err := p.validateCollectionMapping(evt, agg)
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
	evt := findMessage(f, "WidgetRemoved")
	if agg == nil || evt == nil {
		t.Fatal("messages not found")
	}

	err := p.validateCollectionMapping(evt, agg)
	if err == nil {
		t.Fatal("expected error for missing key_field, got nil")
	}
	if !strings.Contains(err.Error(), "key_field") {
		t.Errorf("error should mention key_field, got: %v", err)
	}
}

func TestFileSupportsCLI_WithMessageField(t *testing.T) {
	// The order proto now has AddItem with a message field — CLI should not be supported.
	f := loadTestProto(t, "collection_valid.proto")
	p := newModule()

	if p.fileSupportsCLI(f) {
		t.Error("expected fileSupportsCLI to return false for file with message-typed command field")
	}
}

func TestFileSupportsCLI_AllScalar(t *testing.T) {
	f := loadTestProto(t, "valid.proto")
	p := newModule()

	if !p.fileSupportsCLI(f) {
		t.Error("expected fileSupportsCLI to return true for file with only scalar command fields")
	}
}

// Ensure the optionsv1 import is used (extensions must be registered).
var _ = optionsv1.E_ProtosourceMessageType
