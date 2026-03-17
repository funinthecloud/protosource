package protojsonserializer

import (
	"fmt"

	"github.com/funinthecloud/protosource"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type Serializer struct{}

func NewSerializer() *Serializer {
	return &Serializer{}
}

func (s *Serializer) MarshalEvent(event protosource.Event) (*recordv1.Record, error) {
	data, err := MarshalEvent(event)
	if err != nil {
		return &recordv1.Record{}, err
	}

	return &recordv1.Record{
		Version: event.GetVersion(),
		Data:    data,
	}, nil
}

func (s *Serializer) UnmarshalEvent(record *recordv1.Record) (protosource.Event, error) {
	return UnmarshalEvent(record.Data)
}

func (s *Serializer) UnmarshalEventFromData(data []byte) (protosource.Event, error) {
	return UnmarshalEvent(data)
}

func (s *Serializer) MarshalEventAsData(event protosource.Event) ([]byte, error) {
	return MarshalEvent(event)
}

// MarshalEvent wraps the event in an anypb.Any and serializes to JSON.
// The resulting bytes are human-readable JSON containing the fully-qualified
// type URL and the event fields.
func MarshalEvent(event protosource.Event) ([]byte, error) {
	a, err := anypb.New(event)
	if err != nil {
		return nil, fmt.Errorf("failed anypb.New(event): %w", err)
	}

	data, err := protojson.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("failed protojson.Marshal: %w", err)
	}

	return data, nil
}

// UnmarshalEvent deserializes JSON bytes back into an Event.
// The JSON must contain an anypb.Any envelope with a @type field
// identifying the concrete event type.
func UnmarshalEvent(data []byte) (protosource.Event, error) {
	var a anypb.Any
	if err := protojson.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("failed protojson.Unmarshal: %w", err)
	}

	e, err := anypb.UnmarshalNew(&a, proto.UnmarshalOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed anypb.UnmarshalNew: %w", err)
	}

	event, ok := e.(protosource.Event)
	if !ok {
		return nil, fmt.Errorf("message %T does not implement protosource.Event", e)
	}

	return event, nil
}
