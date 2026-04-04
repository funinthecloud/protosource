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
