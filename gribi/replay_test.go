package replay

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	spb "github.com/openconfig/gribi/v1/proto/service"
	"google.golang.org/protobuf/proto"
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

func TestTimeseries(t *testing.T) {

	mustMarshal := func(t *testing.T, p proto.Message) []byte {
		b, err := proto.Marshal(p)
		if err != nil {
			t.Fatalf("cannot marshal protobuf message, %v", err)
		}
		return b
	}

	modifyData := mustMarshal(t, &spb.ModifyRequest{
		ElectionId: &spb.Uint128{Low: 42},
	})

	makeStream := func(baseTime, numMsg int) []*lpb.GrpcLogEntry {
		modifyStream := []*lpb.GrpcLogEntry{}
		for i := 1; i <= numMsg; i++ {
			d := mustMarshal(t, &spb.ModifyRequest{
				ElectionId: &spb.Uint128{Low: uint64(i)},
			})

			modifyStream = append(modifyStream, &lpb.GrpcLogEntry{
				Timestamp: &tspb.Timestamp{Seconds: int64(baseTime + i)},
				Type:      lpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE,
				Payload: &lpb.GrpcLogEntry_Message{
					Message: &lpb.Message{
						Data: d,
					},
				},
			})
		}
		return modifyStream
	}

	tests := []struct {
		desc      string
		inProtos  []*lpb.GrpcLogEntry
		inQuantum time.Duration
		want      timeseries
		wantErr   bool
	}{{
		desc: "ignored non-client message",
		inProtos: []*lpb.GrpcLogEntry{{
			Type: lpb.GrpcLogEntry_EVENT_TYPE_CANCEL,
		}},
	}, {
		desc: "message with no timestamp",
		inProtos: []*lpb.GrpcLogEntry{{
			Type: lpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE,
		}},
		wantErr: true,
	}, {
		desc: "single event with correct type",
		inProtos: []*lpb.GrpcLogEntry{{
			Type:      lpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE,
			Timestamp: &tspb.Timestamp{Nanos: 42},
			Payload: &lpb.GrpcLogEntry_Message{
				Message: &lpb.Message{Data: modifyData},
			},
		}},
		want: timeseries{
			time.Unix(0, 42): []*spb.ModifyRequest{{
				ElectionId: &spb.Uint128{Low: 42},
			}},
		},
	}, {
		desc:      "time quantum of 1 second",
		inProtos:  makeStream(0, 2),
		inQuantum: 1 * time.Second,
		want: timeseries{
			time.Unix(1, 0): []*spb.ModifyRequest{{
				ElectionId: &spb.Uint128{Low: 1},
			}},
			time.Unix(2, 0): []*spb.ModifyRequest{{
				ElectionId: &spb.Uint128{Low: 2},
			}},
		},
	}, {
		desc:      "time quantum of 2 seconds",
		inProtos:  makeStream(0, 4),
		inQuantum: 2 * time.Second,
		want: timeseries{
			time.Unix(0, 0): []*spb.ModifyRequest{
				{ElectionId: &spb.Uint128{Low: 1}},
			},
			time.Unix(2, 0): []*spb.ModifyRequest{
				{ElectionId: &spb.Uint128{Low: 2}},
				{ElectionId: &spb.Uint128{Low: 3}},
			},
			time.Unix(4, 0): []*spb.ModifyRequest{
				{ElectionId: &spb.Uint128{Low: 4}},
			},
		},
	}, {
		desc:      "time quantum of 10 seconds, starting at higher time",
		inProtos:  makeStream(100, 20),
		inQuantum: 10 * time.Second,
		want: timeseries{
			time.Unix(101, 0): []*spb.ModifyRequest{
				{ElectionId: &spb.Uint128{Low: 1}},
				{ElectionId: &spb.Uint128{Low: 2}},
				{ElectionId: &spb.Uint128{Low: 3}},
				{ElectionId: &spb.Uint128{Low: 4}},
				{ElectionId: &spb.Uint128{Low: 5}},
				{ElectionId: &spb.Uint128{Low: 6}},
				{ElectionId: &spb.Uint128{Low: 7}},
				{ElectionId: &spb.Uint128{Low: 8}},
				{ElectionId: &spb.Uint128{Low: 9}},
				{ElectionId: &spb.Uint128{Low: 10}},
			},
			time.Unix(111, 0): []*spb.ModifyRequest{
				{ElectionId: &spb.Uint128{Low: 11}},
				{ElectionId: &spb.Uint128{Low: 12}},
				{ElectionId: &spb.Uint128{Low: 13}},
				{ElectionId: &spb.Uint128{Low: 14}},
				{ElectionId: &spb.Uint128{Low: 15}},
				{ElectionId: &spb.Uint128{Low: 16}},
				{ElectionId: &spb.Uint128{Low: 17}},
				{ElectionId: &spb.Uint128{Low: 18}},
				{ElectionId: &spb.Uint128{Low: 19}},
				{ElectionId: &spb.Uint128{Low: 20}},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := Timeseries(tt.inProtos, tt.inQuantum)
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, want: %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(got, tt.want, cmpopts.EquateEmpty(), protocmp.Transform()); diff != "" {
				t.Fatalf("did not get expected timeseries, diff(-got,+want):\n%s", diff)
			}
		})
	}
}
