package protobinaryserializer

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/funinthecloud/protosource"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type Serializer struct{}

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

func NewSerializer() *Serializer {
	return &Serializer{}
}

func MarshalEvent(event protosource.Event) ([]byte, error) {
	a, err := anypb.New(event)
	if err != nil {
		return nil, fmt.Errorf("failed anypb.New(event): %w", err)
	}

	// marshal to bytes
	data, err := proto.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("failed proto.Marshal: %w", err)
	}

	return data, nil
}

func UnmarshalEvent(data []byte) (protosource.Event, error) {
	// unmarshal bytes to any
	var a anypb.Any
	if err := proto.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("failed proto.Unmarshal: %w", err)
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

type Encoder struct {
	w io.Writer
}

func (e *Encoder) WriteEvent(event protosource.Event) (int, error) {
	data, err := MarshalEvent(event)
	if err != nil {
		return 0, err
	}

	// Write the length of the marshaled event as uint64
	//
	buffer := make([]byte, 8)
	binary.LittleEndian.PutUint64(buffer, uint64(len(data)))
	if _, err := e.w.Write(buffer); err != nil {
		return 0, err
	}

	n, err := e.w.Write(data)
	if err != nil {
		return 0, err
	}

	return n + 8, nil
}

// This is a convenience method for working with dynamo data without deserializing.
func (e *Encoder) WriteBase64(b64 string) (int, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, fmt.Errorf("failed base64.StdEncoding.DecodeString: %w", err)
	}

	// Write the length of the marshaled event as uint64
	//
	buffer := make([]byte, 8)
	binary.LittleEndian.PutUint64(buffer, uint64(len(data)))
	if _, err := e.w.Write(buffer); err != nil {
		return 0, err
	}

	n, err := e.w.Write(data)
	if err != nil {
		return 0, err
	}

	return n + 8, nil
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		w: w,
	}
}

type Decoder struct {
	r       *bufio.Reader
	scratch *bytes.Buffer
}

func (d *Decoder) readN(n uint64) ([]byte, error) {
	d.scratch.Reset()
	for i := uint64(0); i < n; i++ {
		b, err := d.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if err := d.scratch.WriteByte(b); err != nil {
			return nil, err
		}
	}
	return d.scratch.Bytes(), nil
}

func (d *Decoder) ReadEvent() (protosource.Event, error) {
	data, err := d.readN(8)
	if err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint64(data)

	data, err = d.readN(length)
	if err != nil {
		return nil, err
	}

	event, err := UnmarshalEvent(data)
	if err != nil {
		return nil, err
	}

	return event, nil
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r:       bufio.NewReader(r),
		scratch: bytes.NewBuffer(nil),
	}
}
