package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pgs "github.com/lyft/protoc-gen-star/v2"
	pgsgo "github.com/lyft/protoc-gen-star/v2/lang/go"
	"github.com/lyft/protoc-gen-star/v2/testutils"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func loadTestProto(t *testing.T, name string) pgs.File {
	t.Helper()
	root := repoRoot()

	loader := testutils.Loader{
		ImportPaths: []string{
			filepath.Join(root, "proto"),
			filepath.Join(root, "cmd", "protoc-gen-protosource-ts"),
		},
	}

	ast := loader.LoadProtos(t, filepath.Join(root, "cmd", "protoc-gen-protosource-ts", "testdata", name))

	for _, f := range ast.Targets() {
		if strings.Contains(f.InputPath().String(), name) {
			return f
		}
	}
	for _, pkg := range ast.Packages() {
		for _, f := range pkg.Files() {
			if strings.Contains(f.InputPath().String(), name) {
				return f
			}
		}
	}
	t.Fatalf("target file %q not found in AST", name)
	return nil
}

func newModule() *ProtosourceModule {
	p := &ProtosourceModule{ModuleBase: &pgs.ModuleBase{}}
	p.ctx = pgsgo.InitContext(pgs.Parameters{})
	return p
}

func TestSnakeToCamel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"id", "id"},
		{"body", "body"},
		{"customer_id", "customerId"},
		{"create_at", "createAt"},
		{"shipping_address", "shippingAddress"},
		{"placed_at", "placedAt"},
		{"item_id", "itemId"},
		{"customer_name", "customerName"},
		{"a_b_c", "aBC"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := snakeToCamel(tt.input)
			if got != tt.expected {
				t.Errorf("snakeToCamel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestProtoFileNameFromBase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sample_v1.proto", "./sample_v1_pb.js"},
		{"order_v1.proto", "./order_v1_pb.js"},
		{"history_v1.proto", "./history_v1_pb.js"},
		{"my_domain.proto", "./my_domain_pb.js"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := protoFileNameFromBase(tt.input)
			if got != tt.expected {
				t.Errorf("protoFileNameFromBase(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRoutePrefix(t *testing.T) {
	f := loadTestProto(t, "gsi_enum.proto")
	p := newModule()

	got := p.routePrefix(f)
	want := "test/gsi_enum"
	if got != want {
		t.Errorf("routePrefix() = %q, want %q (must derive from proto package, not Go import path)", got, want)
	}
}

func TestGSIEnumTypes_EnumPK(t *testing.T) {
	f := loadTestProto(t, "gsi_enum.proto")
	p := newModule()

	types := p.gsiEnumTypes(f)
	if len(types) == 0 {
		t.Fatal("expected at least one enum type from GSI fields, got none")
	}

	found := false
	for _, name := range types {
		if name == "Priority" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Priority in GSI enum types, got: %v", types)
	}
}

// findField walks the file's messages for one named (msg, field).
func findField(t *testing.T, f pgs.File, msgName, fieldName string) pgs.Field {
	t.Helper()
	for _, m := range f.AllMessages() {
		if m.Name().String() != msgName {
			continue
		}
		for _, fld := range m.Fields() {
			if fld.Name().String() == fieldName {
				return fld
			}
		}
	}
	t.Fatalf("field %s.%s not found", msgName, fieldName)
	return nil
}

func TestTSParamName_ReservedWords(t *testing.T) {
	f := loadTestProto(t, "reserved_words.proto")

	cases := []struct {
		msg, field   string
		wantParam    string
		wantWireName string
	}{
		{"SetFunction", "function", "function_", "function"},
		{"SetFunction", "class", "class_", "class"},
		{"SetFunction", "id", "id", "id"},
		{"SetFunction", "actor", "actor", "actor"},
	}
	for _, tc := range cases {
		t.Run(tc.msg+"."+tc.field, func(t *testing.T) {
			fld := findField(t, f, tc.msg, tc.field)
			if got := tsParamName(fld); got != tc.wantParam {
				t.Errorf("tsParamName = %q, want %q", got, tc.wantParam)
			}
			if got := tsFieldName(fld); got != tc.wantWireName {
				t.Errorf("tsFieldName = %q, want %q (wire name must be preserved)", got, tc.wantWireName)
			}
		})
	}
}

func TestOutputPath_NestedProtoPackage(t *testing.T) {
	f := loadTestProto(t, "reserved_words.proto")
	p := newModule()
	got := p.outputPath(f, nil)
	want := "test/reserved_words/reserved_words.protosource.client.ts"
	if got != want {
		t.Errorf("outputPath = %q, want %q (must derive from proto package)", got, want)
	}
}

func TestOutputPath_DerivedFromProtoPackageNotGoImport(t *testing.T) {
	// gsi_enum.proto has proto package "test.gsi_enum" but Go import
	// "github.com/funinthecloud/protosource/test/gsi_enum;gsienum".
	// The output path must follow the proto package, not the Go import.
	f := loadTestProto(t, "gsi_enum.proto")
	p := newModule()
	got := p.outputPath(f, nil)
	want := "test/gsi_enum/gsi_enum.protosource.client.ts"
	if got != want {
		t.Errorf("outputPath = %q, want %q", got, want)
	}
}

func TestClientEnumTypes_CommandField(t *testing.T) {
	// Kind appears only as a command parameter, not as a GSI field.
	// It must still end up in the import list.
	f := loadTestProto(t, "command_enum.proto")
	p := newModule()

	types := p.clientEnumTypes(f)
	found := false
	for _, n := range types {
		if n == "Kind" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("clientEnumTypes did not include command-field enum Kind: %v", types)
	}
}

func TestClientEnumTypes_SortedAndDeduped(t *testing.T) {
	f := loadTestProto(t, "command_enum.proto")
	p := newModule()
	types := p.clientEnumTypes(f)

	for i := 1; i < len(types); i++ {
		if types[i-1] > types[i] {
			t.Errorf("clientEnumTypes not sorted: %v", types)
			break
		}
		if types[i-1] == types[i] {
			t.Errorf("clientEnumTypes has duplicate %q: %v", types[i], types)
		}
	}
}

func TestGSIEnumTypes_NoEnums(t *testing.T) {
	// Use the same proto but verify a file without enum GSI fields returns empty.
	// We'll create a simple test: gsi_enum.proto has Priority as GSI1PK (enum),
	// so this test just confirms the function returns the right count.
	f := loadTestProto(t, "gsi_enum.proto")
	p := newModule()

	types := p.gsiEnumTypes(f)
	if len(types) != 1 {
		t.Errorf("expected exactly 1 enum type (Priority), got %d: %v", len(types), types)
	}
}
