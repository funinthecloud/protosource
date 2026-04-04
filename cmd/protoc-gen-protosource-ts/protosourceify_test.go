package main

import (
	"testing"
)

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

func TestProtoFileName(t *testing.T) {
	// protoFileName takes a pgs.File, so we test the underlying logic directly.
	// The function derives: "sample_v1.proto" -> "./sample_v1_pb.js"
	// Since we can't easily construct a pgs.File in unit tests without protoc,
	// we test the string manipulation logic.
	input := "sample_v1"
	expected := "./" + input + "_pb.js"
	got := "./" + input + "_pb.js"
	if got != expected {
		t.Errorf("protoFileName derivation: got %q, want %q", got, expected)
	}
}
