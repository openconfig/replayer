package replay

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	lpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

func TestFromTextprotoFile(t *testing.T) {
	tests := []struct {
		desc         string
		inFilename   string
		wantMessages []*lpb.GrpcLogEntry
		wantErr      bool
	}{{
		desc:       "valid textproto",
		inFilename: "testdata/simple-example.txtpb",
		wantMessages: []*lpb.GrpcLogEntry{{
			Timestamp: &tspb.Timestamp{
				Seconds: 1665546126,
				Nanos:   981745000,
			},
		}, {
			Timestamp: &tspb.Timestamp{
				Seconds: 1665546126,
				Nanos:   981745000,
			},
		}, {
			Timestamp: &tspb.Timestamp{
				Seconds: 1665546126,
				Nanos:   981745000,
			},
		}},
	}, {
		desc:       "invalid file",
		inFilename: "testdata/INVALID",
		wantErr:    true,
	}, {
		desc:       "invalid separator",
		inFilename: "testdata/invalid-separator.txtpb",
		wantErr:    true,
	}, {
		desc:       "invalid included proto",
		inFilename: "testdata/invalid-proto.txtpb",
		wantErr:    true,
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := FromTextprotoFile(tt.inFilename)
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, wantErr? %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(got, tt.wantMessages, protocmp.Transform()); diff != "" {
				t.Fatalf("did not get expected messages, diff(-got,+want):\n%s", diff)
			}
		})
	}
}

func TestFromLogProto(t *testing.T) {
	tests := []struct {
		desc         string
		inFilename   string
		wantMessages []*lpb.GrpcLogEntry
		wantErr      bool
	}{{
		desc:       "valid proto",
		inFilename: "testdata/simple-example.pb",
		wantMessages: []*lpb.GrpcLogEntry{{
			Timestamp: &tspb.Timestamp{
				Seconds: 1665546126,
				Nanos:   981745000,
			},
		}, {
			Timestamp: &tspb.Timestamp{
				Seconds: 1665546126,
				Nanos:   981745000,
			},
		}, {
			Timestamp: &tspb.Timestamp{
				Seconds: 1665546126,
				Nanos:   981745000,
			},
		}},
	}, {
		desc:       "invalid file",
		inFilename: "testdata/INVALID",
		wantErr:    true,
	}, {
		desc:       "invalid included proto",
		inFilename: "testdata/invalid-proto.pb",
		wantErr:    true,
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := FromLogProto(tt.inFilename)
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, wantErr? %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(got, tt.wantMessages, protocmp.Transform()); diff != "" {
				t.Fatalf("did not get expected messages, diff(-got,+want):\n%s", diff)
			}
		})
	}
}
