package protojsonserializer

import (
	"encoding/json"
	"testing"

	"github.com/funinthecloud/protosource"
	testv1 "github.com/funinthecloud/protosource/acme/app/test/v1"
)

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	original := &testv1.Created{
		Id:      "7f95adab-e89e-404f-a253-bb04b8d571de",
		Version: 1,
		At:      8675309,
		Actor:   "alice",
		Body:    "hello world",
	}

	s := NewSerializer()
	record, err := s.MarshalEvent(original)
	if err != nil {
		t.Fatalf("MarshalEvent failed: %v", err)
	}
	if record.GetVersion() != 1 {
		t.Errorf("expected record version 1, got %d", record.GetVersion())
	}

	restored, err := s.UnmarshalEvent(record)
	if err != nil {
		t.Fatalf("UnmarshalEvent failed: %v", err)
	}

	got := restored.(*testv1.Created)
	if got.GetId() != original.GetId() {
		t.Errorf("id mismatch: got %q, want %q", got.GetId(), original.GetId())
	}
	if got.GetVersion() != original.GetVersion() {
		t.Errorf("version mismatch: got %d, want %d", got.GetVersion(), original.GetVersion())
	}
	if got.GetAt() != original.GetAt() {
		t.Errorf("at mismatch: got %d, want %d", got.GetAt(), original.GetAt())
	}
	if got.GetActor() != original.GetActor() {
		t.Errorf("actor mismatch: got %q, want %q", got.GetActor(), original.GetActor())
	}
	if got.GetBody() != original.GetBody() {
		t.Errorf("body mismatch: got %q, want %q", got.GetBody(), original.GetBody())
	}
}

func TestMarshalUnmarshalAsData_RoundTrip(t *testing.T) {
	original := &testv1.Updated{
		Id:      "abc-123",
		Version: 5,
		At:      1234567890,
		Actor:   "bob",
		Body:    "updated body",
	}

	s := NewSerializer()
	data, err := s.MarshalEventAsData(original)
	if err != nil {
		t.Fatalf("MarshalEventAsData failed: %v", err)
	}

	restored, err := s.UnmarshalEventFromData(data)
	if err != nil {
		t.Fatalf("UnmarshalEventFromData failed: %v", err)
	}

	got := restored.(*testv1.Updated)
	if got.GetId() != original.GetId() {
		t.Errorf("id mismatch: got %q, want %q", got.GetId(), original.GetId())
	}
	if got.GetBody() != original.GetBody() {
		t.Errorf("body mismatch: got %q, want %q", got.GetBody(), original.GetBody())
	}
}

func TestMarshalEvent_ProducesValidJSON(t *testing.T) {
	event := &testv1.Created{
		Id:      "test-id",
		Version: 1,
		At:      100,
		Actor:   "actor",
		Body:    "test body",
	}

	data, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent failed: %v", err)
	}

	// Verify the output is valid JSON.
	if !json.Valid(data) {
		t.Fatalf("MarshalEvent produced invalid JSON: %s", data)
	}

	// Verify the JSON contains the type URL.
	var envelope map[string]interface{}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	typeURL, ok := envelope["@type"].(string)
	if !ok || typeURL == "" {
		t.Fatalf("expected @type field in JSON, got: %s", data)
	}
	t.Logf("JSON output: %s", data)
}

func TestMarshalEvent_MultipleEventTypes(t *testing.T) {
	t.Run("Created", func(t *testing.T) {
		testRoundTrip(t, &testv1.Created{Id: "1", Version: 1, At: 1, Actor: "a", Body: "b"})
	})
	t.Run("Updated", func(t *testing.T) {
		testRoundTrip(t, &testv1.Updated{Id: "1", Version: 2, At: 2, Actor: "a", Body: "c"})
	})
	t.Run("Locked", func(t *testing.T) {
		testRoundTrip(t, &testv1.Locked{Id: "1", Version: 3, At: 3, Actor: "a"})
	})
	t.Run("Unlocked", func(t *testing.T) {
		testRoundTrip(t, &testv1.Unlocked{Id: "1", Version: 4, At: 4, Actor: "a"})
	})
}

func testRoundTrip(t *testing.T, original protosource.Event) {
	t.Helper()
	s := NewSerializer()
	data, err := s.MarshalEventAsData(original)
	if err != nil {
		t.Fatalf("MarshalEventAsData failed: %v", err)
	}
	restored, err := s.UnmarshalEventFromData(data)
	if err != nil {
		t.Fatalf("UnmarshalEventFromData failed: %v", err)
	}
	if restored.GetId() != original.GetId() {
		t.Errorf("id mismatch: got %q, want %q", restored.GetId(), original.GetId())
	}
}

func TestUnmarshalEvent_InvalidJSON(t *testing.T) {
	_, err := UnmarshalEvent([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestUnmarshalEvent_ValidJSONBadType(t *testing.T) {
	// Valid JSON but not a valid Any envelope
	_, err := UnmarshalEvent([]byte(`{"@type": "type.googleapis.com/nonexistent.Type", "id": "1"}`))
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}
