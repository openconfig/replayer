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

package replayer

import (
	"context"
	"errors"
	"net"
	"os"
	"path"
	"strings"
	"testing"
	"time"

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
	p4pb "github.com/p4lang/p4runtime/go/p4/v1"
)

const (
	testdataPath = "testdata"
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
				},
				events: []*event{
					{
						message: &gribipb.ModifyRequest{
							Operation: []*gribipb.AFTOperation{
								opProto(t, gribipb.AFTOperation_ADD, 0, fluent.IPv4Entry().WithNetworkInstance("DEFAULT").WithNextHopGroup(43).WithPrefix("prefix2").OpProto),
							},
						},
						timestamp: time.Unix(5, 0),
					},
					{
						message: &gribipb.ModifyRequest{
							Operation: []*gribipb.AFTOperation{
								opProto(t, gribipb.AFTOperation_ADD, 1, fluent.IPv4Entry().WithNetworkInstance("DEFAULT").WithNextHopGroup(44).WithPrefix("prefix3").OpProto),
							},
						},
						timestamp: time.Unix(6, 0),
					},
					{
						message: &gnmipb.SetRequest{
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
						timestamp: time.Unix(7, 0),
					},
					{
						message: &p4pb.WriteRequest{
							DeviceId: 1234,
							Role:     "test_role",
						},
						timestamp: time.Unix(9, 0),
					},
					{
						message: &p4pb.PacketOut{
							Payload: []byte("test payload"),
							Metadata: []*p4pb.PacketMetadata{
								{
									MetadataId: 321,
									Value:      []byte("abc"),
								},
							},
						},
						timestamp: time.Unix(10, 0),
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
			desc:     "file not found",
			filename: "i_dont_exist.pb",
			wantErr:  os.ErrNotExist,
		},
		{
			desc:     "file not binary log",
			filename: "parse_non_log.pb",
			wantErr:  errNoEntries,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := Parse(path.Join(testdataPath, test.filename))
			au := cmp.AllowUnexported(Recording{}, event{}, snapshot{})
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), au); diff != "" {
				t.Errorf("Parse() got unexpected recording (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("Parse() got error %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestReplayiGRIBI(t *testing.T) {
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
				events: []*event{
					&event{
						message: &gribipb.ModifyRequest{
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
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
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
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:         constants.Add,
						NextHopIndex: 1,
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
					ProgrammingResult: gribipb.AFTResult_FIB_PROGRAMMED,
					Details: &client.OpDetailsResults{
						Type:           constants.Add,
						NextHopGroupID: 42,
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

			results, err := Replay(ctx, test.recording, newTestClients(ctx, t))
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
				events: []*event{
					&event{
						message: &gribipb.ModifyRequest{
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

			_, err := Replay(ctx, test.recording, newTestClients(ctx, t))
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
				events: []*event{
					{
						message: &gnmipb.SetRequest{
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
						message: &gnmipb.SetRequest{
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

			results, err := Replay(ctx, test.recording, newTestClients(ctx, t))
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
		events:   []*event{&event{message: &gnmipb.SetRequest{Prefix: &gnmipb.Path{Origin: "error"}}}},
	}

	_, err := Replay(ctx, rec, newTestClients(ctx, t))
	if err == nil {
		t.Errorf("Replay() want error, got nil")
	}
}

func TestReplayGRIBIDiff(t *testing.T) {
	rec := &Recording{
		snapshot: &snapshot{
			gribi: &gribipb.GetResponse{
				Entry: []*gribipb.AFTEntry{
					entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
				},
			},
		},
		events: []*event{
			{
				timestamp: time.Unix(0, 0),
				message: &gribipb.ModifyRequest{
					Operation: []*gribipb.AFTOperation{
						opProto(t, gribipb.AFTOperation_ADD, 1, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(2).WithIPAddress("5.6.7.8").OpProto),
					},
				},
			},
		},
		finalGRIBI: &gribipb.GetResponse{
			Entry: []*gribipb.AFTEntry{
				entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(1).WithIPAddress("1.2.3.4").EntryProto),
				entryProto(t, fluent.NextHopEntry().WithNetworkInstance(server.DefaultNetworkInstanceName).WithIndex(2).WithIPAddress("5.6.7.8").EntryProto),
			},
		},
	}
	ctx := context.Background()

	results, err := Replay(ctx, rec, newTestClients(ctx, t))
	if err != nil {
		t.Fatalf("Replay() got unexpected error %v", err)
	}

	if diff := results.GRIBIDiff(rec); diff != "" {
		t.Errorf("Replay() got unexpected final gRIBI result (-want +got):\n%s", diff)
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
			au := cmp.AllowUnexported(Recording{}, event{}, snapshot{})
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

// newTestClients starts a fake gRIBI server and dials a client to  that server.
func newTestClients(ctx context.Context, t *testing.T) *Clients {
	t.Helper()
	srv := grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
	s, err := server.NewFake(
		server.DisableRIBCheckFn(),
	)
	if err != nil {
		t.Fatalf("cannot create server, error: %v", err)
	}
	s.InjectRIB(rib.New(server.DefaultNetworkInstanceName))

	f := &fakeGNMI{}

	gribipb.RegisterGRIBIServer(srv, s)
	gnmipb.RegisterGNMIServer(srv, f)

	addr := startServer(t, srv)

	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial: error dialing test server: %v", err)
	}

	t.Cleanup(func() {
		err := conn.Close()
		if err != nil {
			t.Logf("Error closing gRIBI test client: %v", err)
		}
	})

	return &Clients{
		GNMI:  gnmipb.NewGNMIClient(conn),
		GRIBI: gribipb.NewGRIBIClient(conn),
	}
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
