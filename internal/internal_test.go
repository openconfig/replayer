// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openconfig/gribigo/client"
	"github.com/openconfig/gribigo/constants"
	"github.com/openconfig/gribigo/fluent"
	"github.com/openconfig/gribigo/rib"
	"github.com/openconfig/gribigo/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	gribipb "github.com/openconfig/gribi/v1/proto/service"
	p4infopb "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4pb "github.com/p4lang/p4runtime/go/p4/v1"
)

const (
	testdataPath = "../testdata"
)

func TestParse(t *testing.T) {
	tests := []struct {
		desc     string
		filename string
		want     *Recording
		wantErr  error
	}{
		{
			desc:     "success",
			filename: "parse_success.pb",
			want: &Recording{
				snapshot: &snapshot{
					gribi: &gribipb.GetResponse{
						Entry: []*gribipb.AFTEntry{
							entryProto(t, fluent.IPv4Entry().WithNetworkInstance("DEFAULT").WithNextHopGroup(42).WithPrefix("prefix1").EntryProto),
						},
					},
					gnmiSet: &gnmipb.SetRequest{
						Replace: []*gnmipb.Update{
							&gnmipb.Update{
								Path: &gnmipb.Path{
									Origin: "first",
									Target: "baba",
								},
							},
						},
					},
					gnmiGet: &gnmipb.GetResponse{
						Notification: []*gnmipb.Notification{
							{
								Timestamp: 123,
								Update: []*gnmipb.Update{
									&gnmipb.Update{
										Path: &gnmipb.Path{
											Origin: "googoo",
											Target: "gaga",
										},
										Val: &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 7}},
									},
									&gnmipb.Update{
										Path: &gnmipb.Path{
											Origin: "googoo",
											Target: "gaga/gaagaa",
										},
										Val: &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 9}},
									},
								},
							},
						},
					},
					p4rt: &p4pb.ReadResponse{
						Entities: []*p4pb.Entity{
							{Entity: &p4pb.Entity_CounterEntry{CounterEntry: &p4pb.CounterEntry{
								CounterId: 123,
								Data: &p4pb.CounterData{
									ByteCount:   456,
									PacketCount: 789,
								},
							}}},
						},
					},
				},
				events: []*Event{
					{
						Message: &gribipb.ModifyRequest{
							Operation: []*gribipb.AFTOperation{
								opProto(t, gribipb.AFTOperation_ADD, 0, fluent.IPv4Entry().WithNetworkInstance("DEFAULT").WithNextHopGroup(43).WithPrefix("prefix2").OpProto),
							},
						},
						Timestamp: time.Unix(5, 0),
					},
					{
						Message: &gribipb.ModifyRequest{
							Operation: []*gribipb.AFTOperation{
								opProto(t, gribipb.AFTOperation_ADD, 1, fluent.IPv4Entry().WithNetworkInstance("DEFAULT").WithNextHopGroup(44).WithPrefix("prefix3").OpProto),
							},
						},
						Timestamp: time.Unix(6, 0),
					},
					{
						Message: &gnmipb.SetRequest{
							Prefix: &gnmipb.Path{
								Origin: "foo",
								Target: "bar",
							},
							Update: []*gnmipb.Update{
								&gnmipb.Update{
									Path: &gnmipb.Path{
										Origin: "foo",
										Target: "bar",
									},
								},
							},
						},
						Timestamp: time.Unix(7, 0),
					},
					{
						Message: &p4pb.WriteRequest{
							DeviceId: 1234,
							Role:     "test_role",
						},
						Timestamp: time.Unix(9, 0),
					},
					{
						Message: &p4pb.PacketOut{
							Payload: []byte("test payload"),
							Metadata: []*p4pb.PacketMetadata{
								{
									MetadataId: 321,
									Value:      []byte("abc"),
								},
							},
						},
						Timestamp: time.Unix(10, 0),
					},
				},
				finalGRIBI: &gribipb.GetResponse{
					Entry: []*gribipb.AFTEntry{
						entryProto(t, fluent.IPv4Entry().WithNetworkInstance("DEFAULT").WithNextHopGroup(43).WithPrefix("final state").EntryProto),
					},
				},
			},
		},
		{
			desc:     "invalid message in log",
			filename: "parse_invalid_message.pb",
			wantErr:  errBadUnmarshal,
		},
		{
			desc:     "file not binary log",
			filename: "parse_non_log.pb",
			wantErr:  errNoEntries,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			b, err := os.ReadFile(path.Join(testdataPath, test.filename))
			if err != nil {
				t.Fatalf("os.ReadFile(%q) failed: %v", test.filename, err)
			}
			got, err := ParseBytes(b)
			au := cmp.AllowUnexported(Recording{}, snapshot{})
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), au); diff != "" {
				t.Errorf("Parse() got unexpected recording (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("Parse() got error %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestReplayGRIBI(t *testing.T) {
	tests := []struct {
		desc      string
		recording *Recording
		want      []*client.OpResult
	}{
		{
			desc: "sends initial state from get response",
			recording: &Recording{
				snapshot: &snapshot{
					gribi: &gribipb.GetResponse{
						Entry: []*gribipb.AFTEntry{
							entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
						},
					},
				},
			},
			want: []*client.OpResult{
				{
					SessionParameters: &gribipb.SessionParametersResult{
						Status: gribipb.SessionParametersResult_OK,
					},
				},
				{
					CurrentServerElectionID: electionID,
				},
				{
					OperationID:       1,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
					},
				},
				{
					OperationID:       1,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
					},
				},
			},
		},
		{
			desc: "modifies initial state",
			recording: &Recording{
				snapshot: &snapshot{
					gribi: &gribipb.GetResponse{
						Entry: []*gribipb.AFTEntry{
							entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
						},
					},
				},
				events: []*Event{
					{
						Message: &gribipb.ModifyRequest{
							Operation: []*gribipb.AFTOperation{
								opProto(t, gribipb.AFTOperation_ADD, 1, fluent.NextHopGroupEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithID(42).AddNextHop(1, 1).OpProto),
								opProto(t, gribipb.AFTOperation_ADD, 2, fluent.IPv4Entry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithNextHopGroup(42).WithPrefix("1.1.1.1/32").OpProto),
							},
						},
					},
				},
			},
			want: []*client.OpResult{
				{
					SessionParameters: &gribipb.SessionParametersResult{
						Status: gribipb.SessionParametersResult_OK,
					},
				},
				{
					CurrentServerElectionID: electionID,
				},
				{
					OperationID:       1,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
					},
				},
				{
					OperationID:       1,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
					},
				},
				{
					OperationID:       2,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 42,
					},
				},
				{
					OperationID:       2,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 42,
					},
				},
				{
					OperationID:       3,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:       constants.Add,
						IPv4Prefix: "1.1.1.1/32",
					},
				},
				{
					OperationID:       3,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:       constants.Add,
						IPv4Prefix: "1.1.1.1/32",
					},
				},
			},
		},
		{
			desc: "reorders GetResponse entries",
			recording: &Recording{
				snapshot: &snapshot{
					gribi: &gribipb.GetResponse{
						Entry: []*gribipb.AFTEntry{
							entryProto(t, fluent.IPv4Entry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithNextHopGroup(42).WithPrefix("1.1.1.1/32").EntryProto),
							entryProto(t, fluent.NextHopGroupEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithID(42).AddNextHop(1, 1).WithBackupNHG(2).EntryProto),
							entryProto(t, fluent.NextHopGroupEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithID(2).EntryProto),
							entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
						},
					},
				},
			},
			want: []*client.OpResult{
				{
					SessionParameters: &gribipb.SessionParametersResult{
						Status: gribipb.SessionParametersResult_OK,
					},
				},
				{
					CurrentServerElectionID: electionID,
				},
				{
					OperationID:       1,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
					},
				},
				{
					OperationID:       1,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
					},
				},
				{
					OperationID:       2,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 2,
					},
				},
				{
					OperationID:       2,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 2,
					},
				},
				{
					OperationID:       3,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 42,
					},
				},
				{
					OperationID:       3,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 42,
					},
				},
				{
					OperationID:       4,
					ProgrammingResult: gribipb.AFTResult_RIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:       constants.Add,
						IPv4Prefix: "1.1.1.1/32",
					},
				},
				{
					OperationID:       4,
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:       constants.Add,
						IPv4Prefix: "1.1.1.1/32",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx := context.Background()

			cfg, _ := newTestClients(ctx, t)
			cfg.P4RT = nil
			results, err := Replay(ctx, test.recording, cfg)
			if err != nil {
				t.Errorf("Replay() got unexpected error %v", err)
			}
			if diff := cmp.Diff(test.want, results.GRIBI(), cmpopts.IgnoreFields(client.OpResult{}, "Timestamp", "Latency"), protocmp.Transform()); diff != "" {
				t.Errorf("Replay() got unexpected ops (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReplayGRIBIErrors(t *testing.T) {
	tests := []struct {
		desc      string
		recording *Recording
		wantErr   string
	}{
		{
			desc: "invalid modify request",
			recording: &Recording{
				snapshot: &snapshot{
					gribi: &gribipb.GetResponse{
						Entry: []*gribipb.AFTEntry{
							entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
						},
					},
				},
				events: []*Event{
					{
						Message: &gribipb.ModifyRequest{
							Operation: []*gribipb.AFTOperation{
								opProto(t, gribipb.AFTOperation_INVALID, 1234, fluent.NextHopEntry().OpProto),
							},
						},
					},
				},
			},
			wantErr: "converge",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx := context.Background()

			cfg, _ := newTestClients(ctx, t)
			cfg.P4RT = nil
			_, err := Replay(ctx, test.recording, cfg)
			if err == nil {
				t.Fatalf("Replay() got no error, want error %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("Replay() got unexpected error %v, want error %q", err, test.wantErr)
			}
		})
	}
}

func TestReplayGNMI(t *testing.T) {
	tests := []struct {
		desc      string
		recording *Recording
	}{
		{
			desc: "sends initial state",
			recording: &Recording{
				snapshot: &snapshot{
					gnmiSet: &gnmipb.SetRequest{
						Update: []*gnmipb.Update{
							{
								Path: &gnmipb.Path{Origin: "foo", Target: "bar"},
								Val:  &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 1}},
							},
						},
						Delete: []*gnmipb.Path{
							{
								Origin: "foo",
								Target: "baz",
							},
						},
					},
				},
			},
		},
		{
			desc: "sends events",
			recording: &Recording{
				snapshot: &snapshot{
					gnmiSet: &gnmipb.SetRequest{
						Update: []*gnmipb.Update{
							{
								Path: &gnmipb.Path{Origin: "foo", Target: "bar"},
								Val:  &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 1}},
							},
						},
					},
				},
				events: []*Event{
					{
						Message: &gnmipb.SetRequest{
							Update: []*gnmipb.Update{
								&gnmipb.Update{
									Path: &gnmipb.Path{
										Origin: "foo",
										Target: "bar",
									},
								},
							},
						},
					},
					{
						Message: &gnmipb.SetRequest{
							Update: []*gnmipb.Update{
								&gnmipb.Update{
									Path: &gnmipb.Path{
										Origin: "foo",
										Target: "baz",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx := context.Background()

			cfg, _ := newTestClients(ctx, t)
			cfg.P4RT = nil
			results, err := Replay(ctx, test.recording, cfg)
			if err != nil {
				t.Errorf("Replay() got unexpected error %v", err)
			}
			got := len(results.GNMI())
			want := len(test.recording.events) + 1
			if got != want {
				t.Errorf("Replay() got %d results, want %d", got, want)
			}
		})
	}
}

func TestReplayGNMIError(t *testing.T) {
	ctx := context.Background()

	rec := &Recording{
		snapshot: &snapshot{},
		events:   []*Event{{Message: &gnmipb.SetRequest{Prefix: &gnmipb.Path{Origin: "error"}}}},
	}

	cfg, _ := newTestClients(ctx, t)
	cfg.P4RT = nil
	_, err := Replay(ctx, rec, cfg)
	if err == nil {
		t.Errorf("Replay() want error, got nil")
	}
}

func TestReplayP4RT(t *testing.T) {
	ctx := context.Background()

	p4Info := &p4infopb.P4Info{
		PkgInfo: &p4infopb.PkgInfo{
			Name:    "test p4info",
			Version: "1234",
		},
	}
	rec := &Recording{
		snapshot: &snapshot{
			p4rt: &p4pb.ReadResponse{
				Entities: []*p4pb.Entity{
					{
						Entity: &p4pb.Entity_CounterEntry{
							CounterEntry: &p4pb.CounterEntry{
								CounterId: 123,
								Index: &p4pb.Index{
									Index: 456,
								},
							},
						},
					},
				},
			},
		},
		events: []*Event{
			{Message: &p4pb.PacketOut{Payload: []byte("first")}, Timestamp: time.Unix(123, 0)},
			{Message: &p4pb.WriteRequest{
				DeviceId: 2,
				Updates: []*p4pb.Update{
					{
						Entity: &p4pb.Entity{
							Entity: &p4pb.Entity_CounterEntry{
								CounterEntry: &p4pb.CounterEntry{
									CounterId: 555,
									Index: &p4pb.Index{
										Index: 777,
									},
								},
							},
						},
					},
				},
			},
			},
			{Message: &p4pb.PacketOut{Payload: []byte("second")}, Timestamp: time.Unix(456, 0)},
		},
	}

	cfg, fakes := newTestClients(ctx, t)
	cfg.P4Info = p4Info
	_, err := Replay(ctx, rec, cfg)
	if err != nil {
		t.Errorf("Replay() got error: %v", err)
	}

	if diff := cmp.Diff(p4Info, fakes.p4rt.gotP4Info, protocmp.Transform()); diff != "" {
		t.Errorf("Replay() got unexpected P4Info: (-want,+got): %v", diff)
	}

	wantWrite := []*p4pb.WriteRequest{
		{
			DeviceId:   1,
			ElectionId: &p4pb.Uint128{Low: 1},
			Updates: []*p4pb.Update{
				{
					Type: p4pb.Update_INSERT,
					Entity: &p4pb.Entity{
						Entity: &p4pb.Entity_CounterEntry{
							CounterEntry: &p4pb.CounterEntry{
								CounterId: 123,
								Index: &p4pb.Index{
									Index: 456,
								},
							},
						},
					},
				},
			},
		},
		{
			DeviceId:   1,
			ElectionId: &p4pb.Uint128{Low: 1},
			Updates: []*p4pb.Update{
				{
					Entity: &p4pb.Entity{
						Entity: &p4pb.Entity_CounterEntry{
							CounterEntry: &p4pb.CounterEntry{
								CounterId: 555,
								Index: &p4pb.Index{
									Index: 777,
								},
							},
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(wantWrite, fakes.p4rt.gotWrite, protocmp.Transform()); diff != "" {
		t.Errorf("Replay() got unexpected P4 writes: (-want,+got): %v", diff)
	}

	wantPacketOut := []*p4pb.PacketOut{
		{
			Payload: []byte("first"),
		},
		{
			Payload: []byte("second"),
		},
	}
	if diff := cmp.Diff(wantPacketOut, fakes.p4rt.gotPacketOut, protocmp.Transform()); diff != "" {
		t.Errorf("Replay() got unexpected P4 packet out: (-want,+got): %v", diff)
	}
}

func TestSetInterfaceMap(t *testing.T) {
	tests := []struct {
		desc      string
		intfs     map[string]string
		recording *Recording
		wantIntfs map[string]string
		wantErr   bool
	}{
		{
			desc:      "no snapshot",
			recording: &Recording{},
			wantErr:   true,
		},
		{
			desc: "remap a bundle",
			recording: &Recording{
				snapshot: &snapshot{
					gnmiGet: gnmiGetFromJSONString(`
					{"openconfig-interfaces:interfaces": {
						"interface": [
						{
							"name": "foo1",
							"openconfig-if-ethernet:ethernet": {
								"config": {
									"openconfig-if-aggregate:aggregate-id": "foo-bundle",
									"port-speed": "100G"
								}
							}
						},
						{
							"name": "foo2",
							"openconfig-if-ethernet:ethernet": {
								"config": {
									"openconfig-if-aggregate:aggregate-id": "foo-bundle",
									"port-speed": "200G"
								}
							}
						},
						{
							"name": "not-relevant",
							"openconfig-if-ethernet:ethernet": {
								"config": {
									"openconfig-if-aggregate:aggregate-id": "some-other-bundle",
									"port-speed": "200G"
								}
							}
						}
						]
					}
				}
				`),
					gribi: &gribipb.GetResponse{
						Entry: []*gribipb.AFTEntry{
							entryProto(t, fluent.NextHopEntry().WithInterfaceRef("foo-bundle").WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
						},
					},
				},
			},
			intfs: map[string]string{
				"foo1": "newfoo1",
				"foo2": "newfoo2",
			},
			wantIntfs: map[string]string{
				"foo1": "newfoo1",
				"foo2": "newfoo2",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			err := test.recording.SetInterfaceMap(test.intfs)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("SetInterfaceMap() got unexpected error %v, want error %t", err, test.wantErr)
			}
			if err != nil {
				return
			}
			au := cmp.AllowUnexported(Recording{}, snapshot{})
			if diff := cmp.Diff(test.wantIntfs, test.recording.intfMap, au, protocmp.Transform()); diff != "" {
				t.Errorf("SetInterfaceMap() got unexpected recording (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInterfaces(t *testing.T) {
	rec := &Recording{
		snapshot: &snapshot{
			gribi: &gribipb.GetResponse{
				Entry: []*gribipb.AFTEntry{
					entryProto(t, fluent.NextHopEntry().WithInterfaceRef("foo-bundle").WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
					entryProto(t, fluent.NextHopEntry().WithInterfaceRef("not-bundle").WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
				},
			},
			gnmiGet: gnmiGetFromJSONString(`
			{"openconfig-interfaces:interfaces": {
				"interface": [
				{
					"name": "foo1",
					"openconfig-if-ethernet:ethernet": {
						"config": {
							"openconfig-if-aggregate:aggregate-id": "foo-bundle",
							"port-speed": "100G"
						}
					}
				},
				{
					"name": "foo2",
					"openconfig-if-ethernet:ethernet": {
						"config": {
							"openconfig-if-aggregate:aggregate-id": "foo-bundle",
							"port-speed": "200G"
						}
					}
				},
				{
					"name": "not-relevant",
					"openconfig-if-ethernet:ethernet": {
						"config": {
							"openconfig-if-aggregate:aggregate-id": "some-other-bundle",
							"port-speed": "200G"
						}
					}
				}
				]
			}
		}
		`),
		},
	}

	want := map[string][]Interface{
		"not-bundle": nil,
		"foo-bundle": []Interface{
			{Name: "foo1", Speed: "100G"},
			{Name: "foo2", Speed: "200G"},
		},
	}

	got, err := rec.Interfaces()
	if err != nil {
		t.Errorf("Interfaces(): unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Interfaces() got unexpected mapping (-want +got):\n%s", diff)
	}
}

func TestInterfacesErrors(t *testing.T) {
	tests := []struct {
		desc string
		rec  *Recording
	}{
		{
			desc: "no snapshot",
			rec:  &Recording{},
		},
		{
			desc: "bad json unmarshal",
			rec: &Recording{
				snapshot: &snapshot{
					gnmiGet: gnmiGetFromJSONString(`{"invalid json}`),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			_, err := test.rec.Interfaces()
			if err == nil {
				t.Errorf("Interfaces(): want error, got none.")
			}
		})
	}
}

func gnmiGetFromJSONString(json string) *gnmipb.GetResponse {
	return &gnmipb.GetResponse{
		Notification: []*gnmipb.Notification{
			{
				Update: []*gnmipb.Update{
					{
						Val: &gnmipb.TypedValue{
							Value: &gnmipb.TypedValue_JsonIetfVal{[]byte(json)},
						},
					},
				},
			},
		},
	}
}

// newTestClients starts a server with fake grpc protocol handlers and returns clients to that server.
func newTestClients(ctx context.Context, t *testing.T) (*Config, *fakes) {
	t.Helper()
	srv := grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
	s, err := server.NewFake(
		server.DisableRIBCheckFn(),
	)
	if err != nil {
		t.Fatalf("cannot create server, error: %v", err)
	}
	s.InjectRIB(rib.New(server.DefaultNetworkInstanceName))

	f := &fakes{
		gnmi:  &fakeGNMI{},
		gribi: s,
		p4rt:  &fakeP4RT{},
	}

	gribipb.RegisterGRIBIServer(srv, f.gribi)
	gnmipb.RegisterGNMIServer(srv, f.gnmi)
	p4pb.RegisterP4RuntimeServer(srv, f.p4rt)

	addr := startServer(t, srv)

	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial: error dialing test server: %v", err)
	}

	t.Cleanup(func() {
		err := conn.Close()
		if err != nil {
			t.Logf("Error closing test clients: %v", err)
		}
	})

	return &Config{
		GNMI:  gnmipb.NewGNMIClient(conn),
		GRIBI: gribipb.NewGRIBIClient(conn),
		P4RT:  p4pb.NewP4RuntimeClient(conn),
	}, f
}

type fakes struct {
	gnmi  *fakeGNMI
	gribi *server.FakeServer
	p4rt  *fakeP4RT
}

type fakeGNMI struct {
	gnmipb.UnimplementedGNMIServer

	gotSetReq []*gnmipb.SetRequest
}

func (f *fakeGNMI) Set(ctx context.Context, req *gnmipb.SetRequest) (*gnmipb.SetResponse, error) {
	if req.GetPrefix().GetOrigin() == "error" {
		return nil, errors.New("gNMI error")
	}
	f.gotSetReq = append(f.gotSetReq, req)
	resp := &gnmipb.SetResponse{
		Prefix: req.GetPrefix(),
	}
	return resp, nil
}

type fakeP4RT struct {
	p4pb.UnimplementedP4RuntimeServer

	gotP4Info    *p4infopb.P4Info
	gotWrite     []*p4pb.WriteRequest
	gotPacketOut []*p4pb.PacketOut
}

func (f *fakeP4RT) StreamChannel(stream p4pb.P4Runtime_StreamChannelServer) error {
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil // client is done
		}
		if err != nil {
			return err
		}

		resp := &p4pb.StreamMessageResponse{}
		switch v := in.GetUpdate().(type) {
		case *p4pb.StreamMessageRequest_Arbitration:
			resp.Update = &p4pb.StreamMessageResponse_Arbitration{
				Arbitration: &p4pb.MasterArbitrationUpdate{
					DeviceId:   1,
					ElectionId: &p4pb.Uint128{High: 0, Low: 1},
				},
			}
		case *p4pb.StreamMessageRequest_Packet:
			f.gotPacketOut = append(f.gotPacketOut, v.Packet)
		default:
			log.Errorf("Got unhandled stream message type %T: %v", v, v)
		}

		err = stream.Send(resp)
		if err != nil {
			return err
		}
	}
}

func (f *fakeP4RT) SetForwardingPipelineConfig(ctx context.Context, req *p4pb.SetForwardingPipelineConfigRequest) (*p4pb.SetForwardingPipelineConfigResponse, error) {
	f.gotP4Info = req.GetConfig().GetP4Info()
	return &p4pb.SetForwardingPipelineConfigResponse{}, nil
}

func (f *fakeP4RT) Write(ctx context.Context, req *p4pb.WriteRequest) (*p4pb.WriteResponse, error) {
	f.gotWrite = append(f.gotWrite, req)
	return &p4pb.WriteResponse{}, nil
}

func entryProto(t *testing.T, f func() (*gribipb.AFTEntry, error)) *gribipb.AFTEntry {
	t.Helper()
	m, err := f()
	if err != nil {
		t.Fatalf("Error building AFT proto: %v", err)
	}
	return m
}

func opProto(t *testing.T, op gribipb.AFTOperation_Operation, id uint64, f func() (*gribipb.AFTOperation, error)) *gribipb.AFTOperation {
	t.Helper()
	m, err := f()
	if err != nil {
		t.Fatalf("Error building AFT proto: %v", err)
	}
	m.Op = op
	m.Id = id
	return m
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	ret, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("Error marshalling proto: %v", err)
	}
	return ret
}

func startServer(t *testing.T, server *grpc.Server) string {
	t.Helper()
	lis, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatalf("Creating TCP listener: %v", err)
	}
	// server.Stop() should close this, but do it anyway to avoid resource leak.
	t.Cleanup(func() { lis.Close() })

	go server.Serve(lis)
	t.Cleanup(server.Stop)

	return lis.Addr().String()
}
