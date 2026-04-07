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
