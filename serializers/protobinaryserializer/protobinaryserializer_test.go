package protobinaryserializer

import (
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/funinthecloud/protosource"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestSerializer_MarshalEvent(t *testing.T) {
	type args struct {
		event protosource.Event
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "04e16729-9b9d-4b51-9f16-5e6aa6b9be86",
			args: args{
				event: &testv1.Created{
					Id:      "7f95adab-e89e-404f-a253-bb04b8d571de",
					Version: 1,
					At:      8675309,
					Actor:   "Me",
					Body:    "LOOK AT MY BODY",
				},
			},
		},
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Serializer{}
			got, err := s.MarshalEvent(tt.args.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Validate the Record has the correct version
			if got.Version != tt.args.event.GetVersion() {
				t.Errorf("MarshalEvent() Record.Version = %d, want %d", got.Version, tt.args.event.GetVersion())
			}

			// Validate the Record has non-empty data
			if len(got.Data) == 0 {
				t.Fatal("MarshalEvent() Record.Data is empty")
			}

			// Validate the data contains a valid anypb.Any with the correct type URL
			var a anypb.Any
			if err := proto.Unmarshal(got.Data, &a); err != nil {
				t.Fatalf("MarshalEvent() Record.Data is not a valid anypb.Any: %v", err)
			}
			wantTypeURL := "type.googleapis.com/example.app.test.v1.Created"
			if a.TypeUrl != wantTypeURL {
				t.Errorf("MarshalEvent() anypb.Any.TypeUrl = %q, want %q", a.TypeUrl, wantTypeURL)
			}

			// Round-trip: unmarshal and compare to original
			cmpOpts := []cmp.Option{
				cmp.FilterPath(func(p cmp.Path) bool {
					sf, ok := p.Index(-1).(cmp.StructField)
					if !ok {
						return false
					}
					r, _ := utf8.DecodeRuneInString(sf.Name())
					return !unicode.IsUpper(r)
				}, cmp.Ignore()),
			}

			got2, err := s.UnmarshalEvent(got)
			if err != nil {
				t.Fatalf("UnmarshalEvent() error = %v", err)
			}
			if diff := cmp.Diff(tt.args.event, got2, cmpOpts...); diff != "" {
				t.Errorf("Round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSerializer_MarshalEventAsData(t *testing.T) {
	type args struct {
		event protosource.Event
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "04e16729-9b9d-4b51-9f16-5e6aa6b9be86",
			args: args{
				event: &testv1.Created{
					Id:      "7f95adab-e89e-404f-a253-bb04b8d571de",
					Version: 1,
					At:      8675309,
					Actor:   "Me",
					Body:    "LOOK AT MY BODY",
				},
			},
		},
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Serializer{}
			got, err := s.MarshalEventAsData(tt.args.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalEventAsData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Validate non-empty bytes
			if len(got) == 0 {
				t.Fatal("MarshalEventAsData() returned empty bytes")
			}

			// Validate the bytes contain a valid anypb.Any with the correct type URL
			var a anypb.Any
			if err := proto.Unmarshal(got, &a); err != nil {
				t.Fatalf("MarshalEventAsData() result is not a valid anypb.Any: %v", err)
			}
			wantTypeURL := "type.googleapis.com/example.app.test.v1.Created"
			if a.TypeUrl != wantTypeURL {
				t.Errorf("MarshalEventAsData() anypb.Any.TypeUrl = %q, want %q", a.TypeUrl, wantTypeURL)
			}

			// MarshalEventAsData should produce the same bytes as MarshalEvent's Record.Data
			record, err := s.MarshalEvent(tt.args.event)
			if err != nil {
				t.Fatalf("MarshalEvent() error = %v", err)
			}
			if diff := cmp.Diff(record.Data, got); diff != "" {
				t.Errorf("MarshalEventAsData() bytes differ from MarshalEvent().Data (-want +got):\n%s", diff)
			}

			// Round-trip: unmarshal and compare to original
			cmpOpts := []cmp.Option{
				cmp.FilterPath(func(p cmp.Path) bool {
					sf, ok := p.Index(-1).(cmp.StructField)
					if !ok {
						return false
					}
					r, _ := utf8.DecodeRuneInString(sf.Name())
					return !unicode.IsUpper(r)
				}, cmp.Ignore()),
			}

			got2, err := s.UnmarshalEventFromData(got)
			if err != nil {
				t.Fatalf("UnmarshalEventFromData() error = %v", err)
			}
			if diff := cmp.Diff(tt.args.event, got2, cmpOpts...); diff != "" {
				t.Errorf("Round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}