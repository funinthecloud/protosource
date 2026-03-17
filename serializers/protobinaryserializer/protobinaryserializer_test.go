package protobinaryserializer

import (
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/funinthecloud/protosource"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
	testv1 "github.com/funinthecloud/protosource/acme/app/test/v1"
	"github.com/google/go-cmp/cmp"
)

func TestSerializer_MarshalEvent(t *testing.T) {
	type args struct {
		event protosource.Event
	}
	tests := []struct {
		name    string
		args    args
		want    *recordv1.Record
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
			if !cmp.Equal(tt.args.event, got2, cmpOpts...) {
				cmp.Diff(tt.args.event, got2, cmpOpts...)
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
		want    *recordv1.Record
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
				t.Errorf("MarshalEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

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
			if !cmp.Equal(tt.args.event, got2, cmpOpts...) {
				cmp.Diff(tt.args.event, got2, cmpOpts...)
			}
		})
	}
}
