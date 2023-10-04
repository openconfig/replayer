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

// Package replayer provides methods for parsing and replaying logs of g* protocol messanges.
package replayer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	log "github.com/golang/glog"
	"github.com/google/go-cmp/cmp"
	"github.com/kylelemons/godebug/pretty"
	"github.com/openconfig/gribigo/client"
	"github.com/openconfig/ygot/util"
	"github.com/openconfig/ygot/ygot"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	gribipb "github.com/openconfig/gribi/v1/proto/service"
	lpb "github.com/openconfig/replayer/proto/log"
)

//go:generate ./compile_protos.sh

// Recording is a parsed gRIBI binary log record.
type Recording struct {
	snapshot   *snapshot
	events     []*event
	intfMap    map[string]string
	finalGRIBI *gribipb.GetResponse
}

func (r *Recording) gribiEvents() []*event {
	var ret []*event
	for _, event := range r.events {
		switch event.message.(type) {
		case *gribipb.ModifyRequest:
			ret = append(ret, event)
		}
	}
	return ret
}

func (r *Recording) String() string {
	var strRep struct {
		Snapshot string
		Events   []string
	}
	strRep.Snapshot = r.snapshot.String()
	for _, event := range r.events {
		strRep.Events = append(strRep.Events, event.String())
	}
	return fmt.Sprintf("%+v", strRep)
}

// SetInterfaceMap sets the interfaces to be used during replay according to the input map.
// When replaying, any un-mapped interfaces and bundles containing unmapped interfaces will be
// ignored.
func (r *Recording) SetInterfaceMap(oldToNew map[string]string) error {
	bundleMapping, err := r.Interfaces()
	if err != nil {
		return fmt.Errorf("cannot get bundle interface mapping: %w", err)
	}

	log.Infof("Bundle interface mapping: %v", bundleMapping)

	r.intfMap = oldToNew
	log.Infof("Set interface mapping: %v", oldToNew)

	return nil
}

// Interface is an interface present in a replay log.
type Interface struct {
	Name, Speed string
}

var (
	errNoSnapshot = errors.New("no snapshot found")
)

// Interfaces returns a map where the keys are interface names found in the gRIBI events and
// the values are the members of the bundle interface specified in the gNMI snapshot.
// If the key is not a bundle interface, the length of the slice will be zero.
func (r *Recording) Interfaces() (map[string][]Interface, error) {
	// Get interface references from the gRIBI snapshot.
	gribiIntfs := map[string][]Interface{}
	if r.snapshot == nil {
		return nil, errNoSnapshot
	}
	for _, aftOp := range r.snapshot.gribi.GetEntry() {
		switch v := aftOp.GetEntry().(type) {
		case *gribipb.AFTEntry_NextHop:
			intf := v.NextHop.GetNextHop().GetInterfaceRef().GetInterface()
			if intf == nil || intf.Value == "" {
				continue
			}
			gribiIntfs[intf.Value] = nil
		}
	}
	for _, event := range r.gribiEvents() {
		req := event.message.(*gribipb.ModifyRequest)
		for _, aftOp := range req.GetOperation() {
			switch v := aftOp.GetEntry().(type) {
			case *gribipb.AFTOperation_NextHop:
				intf := v.NextHop.GetNextHop().GetInterfaceRef().GetInterface()
				if intf == nil || intf.Value == "" {
					continue
				}
				gribiIntfs[intf.Value] = nil
			}
		}
	}

	// Now get the bundle members of those referenced interfaces from the gNMI snapshot.
	req, err := initSetReq(r.snapshot.gnmiGet)
	if err != nil {
		return nil, err
	}
	for _, update := range req.GetUpdate() {
		jv := update.GetVal().GetJsonIetfVal()
		if jv == nil {
			continue
		}

		jm := map[string]any{}
		err := json.Unmarshal(jv, &jm)
		if err != nil {
			return nil, fmt.Errorf("cannot unmarshal json value: %w", err)
		}

		intfs, ok := jm["openconfig-interfaces:interfaces"].(map[string]any)
		if !ok {
			continue
		}
		intfList, ok := intfs["interface"].([]any)
		if !ok {
			continue
		}
		for _, intf := range intfList {
			intf := intf.(map[string]any)
			iName, ok := intf["name"].(string)
			if !ok {
				continue
			}
			eth, ok := intf["openconfig-if-ethernet:ethernet"].(map[string]any)
			if !ok {
				continue
			}
			cfg, ok := eth["config"].(map[string]any)
			if !ok {
				continue
			}
			aID, ok := cfg["openconfig-if-aggregate:aggregate-id"].(string)
			if !ok {
				continue
			}
			bundleMembers, ok := gribiIntfs[aID]
			if !ok {
				continue
			}
			speed, ok := cfg["port-speed"].(string)
			if !ok {
				continue
			}

			i := Interface{Name: iName, Speed: speed}
			gribiIntfs[aID] = append(bundleMembers, i)

		}
	}
	return gribiIntfs, nil
}

type snapshot struct {
	gribi   *gribipb.GetResponse
	gnmiGet *gnmipb.GetResponse
	gnmiSet *gnmipb.SetRequest
}

func (s *snapshot) String() string {
	strRep := struct {
		GRIBI   string
		GNMI    string
		GNMISet string
	}{
		GRIBI:   prototext.Format(s.gribi),
		GNMI:    prototext.Format(s.gnmiGet),
		GNMISet: prototext.Format(s.gnmiSet),
	}
	return fmt.Sprintf("%+v", strRep)
}

// event is one logged event from a binarylog.
type event struct {
	timestamp time.Time
	message   proto.Message
}

func (e *event) String() string {
	return fmt.Sprintf("[%v: %v]", e.timestamp, prototext.Format(e.message))
}

var (
	errNoEntries    = errors.New("no entries found")
	errBadUnmarshal = errors.New("could not unmarshal into known message type")
)

// Parse parses an SFE binary log at the specified path.
func Parse(path string) (*Recording, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	events := new(lpb.Events)
	if err := proto.Unmarshal(bytes, events); err != nil {
		return nil, err
	}

	entries := events.GetGrpcEvents()
	if len(entries) == 0 {
		return nil, errNoEntries
	}

	r := &Recording{snapshot: &snapshot{}}

	counts := map[string]int{}
	for i, entry := range entries {
		data := entry.GetMessage().GetData()
		timestamp := entry.GetTimestamp().AsTime()

		m, err := unmarshal(data)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal log entry %v: %w", i, err)
		}

		switch v := m.(type) {
		case *gnmipb.GetResponse:
			counts["gNMI GetResponse"]++
			if r.snapshot.gnmiGet == nil {
				r.snapshot.gnmiGet = v
			}
		case *gribipb.GetResponse:
			counts["gRIBI GetResponse"]++
			if r.snapshot.gribi == nil {
				r.snapshot.gribi = v
			}
			r.finalGRIBI = v
		case *gnmipb.SetRequest:
			counts["gNMI SetRequest"]++
			if r.snapshot.gnmiSet == nil {
				r.snapshot.gnmiSet = v
			} else {
				r.events = append(r.events, &event{message: v, timestamp: timestamp})
			}
		case *gribipb.ModifyRequest:
			counts["gRIBI ModifyRequest"]++
			r.events = append(r.events, &event{message: v, timestamp: timestamp})
		default:
			counts["unsupported"]++
			log.Errorf("Unsupported message type: %T, %v", v, v)
		}

	}
	log.Infof("Parsed binary log with request counts:\n%v", pretty.Sprint(counts))
	return r, nil
}

func logSetPaths(req *gnmipb.SetRequest) {
	sb := strings.Builder{}
	sb.WriteString("Sending SetRequest with paths:\n")
	var paths []string
	for _, update := range req.GetUpdate() {
		path, err := ygot.PathToString(update.GetPath())
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	sb.WriteString(fmt.Sprintf("Update: %v\n", paths))
	paths = nil
	for _, update := range req.GetReplace() {
		path, err := ygot.PathToString(update.GetPath())
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	sb.WriteString(fmt.Sprintf("Replace: %v\n", paths))
	paths = nil
	for _, delete := range req.GetDelete() {
		path, err := ygot.PathToString(delete)
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	sb.WriteString(fmt.Sprintf("Delete: %v\n", paths))
	log.Info(sb.String())
}

// unmarshal attempts to unmarshal the given data into a supported binary log message type.
func unmarshal(data []byte) (proto.Message, error) {
	messages := []proto.Message{
		new(gnmipb.GetRequest),
		new(gnmipb.GetResponse),
		new(gnmipb.SetRequest),
		new(gnmipb.SetResponse),
		new(gnmipb.SubscribeRequest),
		new(gnmipb.SubscribeResponse),
		new(gribipb.GetResponse),
		new(gribipb.ModifyRequest),
	}

	for _, m := range messages {
		if err := unmarshalToType(data, m); err == nil {
			return m, nil
		}
	}
	return nil, errBadUnmarshal
}

// unmarshalToType tries to unmarshal a message to the given type of m. The unmarshalling returns an
// error if proto.Unmarshal returns an error or if there are unknown fields in m after unmarshalled.
func unmarshalToType(data []byte, m proto.Message) error {
	if err := proto.Unmarshal(data, m); err != nil {
		return err
	}
	newMsg := proto.Clone(m)

	uo := proto.UnmarshalOptions{DiscardUnknown: true}
	if err := uo.Unmarshal(data, newMsg); err != nil {
		return err
	}

	if !proto.Equal(newMsg, m) {
		return fmt.Errorf("unknown fields when unmarshalling to %T", m)
	}
	return nil
}

var (
	electionID      = &gribipb.Uint128{Low: 1}
	gRIBIClientOpts = []client.Opt{
		client.FIBACK(),
		client.PersistEntries(),
		client.ElectedPrimaryClient(electionID),
	}
)

// Clients holds the RPC clients for g* protocols.
type Clients struct {
	GNMI  gnmipb.GNMIClient
	GRIBI gribipb.GRIBIClient
}

// Replay sends the parsed record over the given gRIBI client.
func Replay(ctx context.Context, r *Recording, clients *Clients) (*Results, error) {
	gRIBI, err := client.New(gRIBIClientOpts...)
	if err != nil {
		return nil, fmt.Errorf("cannot create gRIBI client: %w", err)
	}
	if err := gRIBI.UseStub(clients.GRIBI); err != nil {
		return nil, fmt.Errorf("cannot use gRIBI stub: %w", err)
	}
	if err := gRIBI.Connect(ctx); err != nil {
		return nil, fmt.Errorf("cannot connect to gRIBI: %w", err)
	}
	gRIBI.StartSending()
	defer gRIBI.StopSending()

	// Log results from any exit point.
	results := &Results{}
	defer func() {
		gr, _ := gRIBI.Results()
		results.gribi = gr
		logResults(r, results)
	}()

	if r.snapshot.gnmiSet != nil {
		setReq, err := transformSet(r.snapshot.gnmiSet, r)
		if err != nil {
			return nil, fmt.Errorf("can't transform initial set request: %w", err)
		}
		logSetPaths(setReq)
		longInfof("Sending set request for initial gNMI state: %v", prettySetRequest(setReq))
		resp, err := clients.GNMI.Set(ctx, setReq)
		if err != nil {
			return nil, fmt.Errorf("can't set initial gNMI state: %w", err)
		}
		results.gnmi = append(results.gnmi, resp)
	}

	opID := uint64(1)
	if r.snapshot.gribi != nil {
		initModReq, err := initModifyReq(r.snapshot.gribi)
		if err != nil {
			return nil, err
		}
		log.Infof("Sending modify request for initial gRIBI state: %v", initModReq)
		gRIBI.Q(initModReq)
		log.Infof("Waiting for gRIBI client convergence for initial state")
		if err := gRIBI.AwaitConverged(ctx); err != nil {
			return nil, fmt.Errorf("can't converge to initial gRIBI state: %w", err)
		}
		opID += uint64(len(initModReq.GetOperation()))
	}

	var prevTime time.Time
	if len(r.events) != 0 {
		prevTime = r.events[0].timestamp
	}

	for _, event := range r.events {
		log.Infof("Waiting for gRIBI client convergence before next event")
		if err := gRIBI.AwaitConverged(ctx); err != nil {
			return nil, fmt.Errorf("can't converge gRIBI client: %w", err)
		}
		waitTime := event.timestamp.Sub(prevTime)
		if waitTime > 0 {
			log.Infof("Sleeping for %v", waitTime)
			time.Sleep(waitTime)
		}
		prevTime = event.timestamp

		switch req := event.message.(type) {
		case *gribipb.ModifyRequest:
			if req.GetParams() != nil || req.GetElectionId() != nil {
				log.Infof("Skipping gRIBI session params or election ID request: %v", req)
				continue
			}
			for _, aftOp := range req.GetOperation() {
				aftOp.ElectionId = electionID
				aftOp.Id = opID
				opID++
			}
			log.Infof("Sending gRIBI modify request: %v", req)
			gRIBI.Q(req)

		case *gnmipb.SetRequest:
			setReq, err := transformSet(req, r)
			if err != nil {
				return nil, fmt.Errorf("can't transform initial set request: %w", err)
			}
			logSetPaths(setReq)
			log.Infof("Sending gNMI set request: %v", prettySetRequest(setReq))
			resp, err := clients.GNMI.Set(ctx, setReq)
			if err != nil {
				log.Errorf("Response from gNMI error: %v", resp)
				return nil, fmt.Errorf("gNMI set error: %w", err)
			}
			results.gnmi = append(results.gnmi, resp)

		default:
			log.Errorf("Unsupported message type %T in recording: %v", req, req)
		}
	}

	log.Infof("Waiting for final gRIBI state convergence")
	if err := gRIBI.AwaitConverged(ctx); err != nil {
		return nil, fmt.Errorf("can't converge to final gRIBI state: %w", err)
	}

	grRes, err := gRIBI.Results()
	if err != nil {
		return nil, fmt.Errorf("can't get results from gRIBI client: %w", err)
	}
	results.gribi = grRes

	final, err := gRIBI.Get(ctx, &gribipb.GetRequest{Aft: gribipb.AFTType_ALL, NetworkInstance: &gribipb.GetRequest_All{All: &gribipb.Empty{}}})
	if err != nil {
		return nil, fmt.Errorf("can't get final GetResponse from gRIBI client: %w", err)
	}
	results.finalGRIBI = final
	return results, nil
}

func logResults(rec *Recording, res *Results) {
	logGRIBIResults(rec, res)

	log.Info("gNMI Results:")
	for _, r := range res.GNMI() {
		log.Info(r)
	}
}

func logGRIBIResults(rec *Recording, res *Results) {
	log.Info("gRIBI Results:")
	var initModReq *gribipb.ModifyRequest
	if rec == nil || rec.snapshot == nil || rec.snapshot.gribi == nil {
		log.Warning("Recording has nil gRIBI snapshot, can't log detailed gRIBI results")
		log.Info(res.GRIBI())
		return
	}

	initModReq, err := initModifyReq(rec.snapshot.gribi)
	if err != nil {
		log.Error(err)
		return
	}

	type sVal struct {
		op     *gribipb.AFTOperation
		result *client.OpResult
	}
	summary := map[uint64]*sVal{}

	for _, aftOp := range initModReq.GetOperation() {
		summary[aftOp.GetId()] = &sVal{op: aftOp}
	}
	for _, e := range rec.gribiEvents() {
		req := e.message.(*gribipb.ModifyRequest)
		for _, aftOp := range req.GetOperation() {
			summary[aftOp.GetId()] = &sVal{op: aftOp}
		}
	}

	for _, r := range res.GRIBI() {
		if r == nil {
			continue
		} else if r.ProgrammingResult == gribipb.AFTResult_UNSET {
			log.Infof("[Non-Op]: %v", r)
			continue
		}
		v, ok := summary[r.OperationID]
		if !ok {
			log.Errorf("Missing op ID %v in requests. Got result: %v", r.OperationID, r)
			continue
		}
		v.result = r
	}

	passed, failed, noResult := 0, 0, 0
	for i := 1; i <= len(summary); i++ {
		v, ok := summary[uint64(i)]
		if !ok {
			continue
		}
		log.Infof("[%v] %v", i, v.op)
		if v.result == nil {
			log.Infof("  * [NO RESULT]")
			noResult++
			continue
		}
		log.Infof("  * [%v] %v", v.result.ProgrammingResult, v.result)

		rs := v.result.ProgrammingResult
		if rs == gribipb.AFTResult_FAILED || rs == gribipb.AFTResult_FIB_FAILED {
			failed++
		} else {
			passed++
		}
	}
	log.Infof("Passed: %v Failed: %v No Result: %v", passed, failed, noResult)
}

func longInfof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	rs := []rune(string(msg))
	for limit := 5000; len(rs) > limit; rs = rs[limit:] {
		log.InfoDepth(1, string(rs[:limit]))
	}
	log.InfoDepth(1, string(rs))
}

// Results contains the results of replayed requests.
type Results struct {
	gribi      []*client.OpResult
	gnmi       []*gnmipb.SetResponse
	finalGRIBI *gribipb.GetResponse
}

// GRIBI returns the results from the replayed gRIBI requests.
func (r *Results) GRIBI() []*client.OpResult {
	if r == nil {
		return nil
	}
	return r.gribi
}

// GNMI returns the results from the replayed gNMI requests.
func (r *Results) GNMI() []*gnmipb.SetResponse {
	if r == nil {
		return nil
	}
	return r.gnmi
}

// GRIBIDiff returns the diff of the final gRIBI state in the replay results with that of the given
// recording.
func (r *Results) GRIBIDiff(rec *Recording) string {
	// TODO: b/293275239 - Refactor our packages so this usage of cmp is in a test-only package.
	return cmp.Diff(r.finalGRIBI, rec.finalGRIBI,
		protocmp.Transform(),
		protocmp.SortRepeatedFields(&gribipb.GetResponse{}, "entry"),
		protocmp.IgnoreFields(&gribipb.AFTEntry{}, "fib_status"),
	)
}

func initModifyReq(getResp *gribipb.GetResponse) (*gribipb.ModifyRequest, error) {
	var aftOps []*gribipb.AFTOperation

	var backupNHG []uint64
	for _, aftEntry := range getResp.GetEntry() {
		aftOp := &gribipb.AFTOperation{
			NetworkInstance: aftEntry.GetNetworkInstance(),
			Op:              gribipb.AFTOperation_ADD,
			ElectionId:      electionID,
		}
		switch v := aftEntry.GetEntry().(type) {
		case *gribipb.AFTEntry_Ipv4:
			aftOp.Entry = &gribipb.AFTOperation_Ipv4{v.Ipv4}
		case *gribipb.AFTEntry_Ipv6:
			aftOp.Entry = &gribipb.AFTOperation_Ipv6{v.Ipv6}
		case *gribipb.AFTEntry_Mpls:
			aftOp.Entry = &gribipb.AFTOperation_Mpls{v.Mpls}
		case *gribipb.AFTEntry_NextHopGroup:
			bu := v.NextHopGroup.GetNextHopGroup().GetBackupNextHopGroup().GetValue()
			if bu != 0 {
				backupNHG = append(backupNHG, bu)
			}
			aftOp.Entry = &gribipb.AFTOperation_NextHopGroup{v.NextHopGroup}
		case *gribipb.AFTEntry_NextHop:
			aftOp.Entry = &gribipb.AFTOperation_NextHop{v.NextHop}
		case *gribipb.AFTEntry_MacEntry:
			aftOp.Entry = &gribipb.AFTOperation_MacEntry{v.MacEntry}
		case *gribipb.AFTEntry_PolicyForwardingEntry:
			aftOp.Entry = &gribipb.AFTOperation_PolicyForwardingEntry{v.PolicyForwardingEntry}
		default:
			return nil, fmt.Errorf("unknown AFT entry: %v(%T)", v, v)
		}
		// TODO(gdennis): Use the RIB and FIB status of the entry?
		aftOps = append(aftOps, aftOp)
	}

	// The order for operations should be as follows to ensure dependencies are respected:
	//   1. Next Hop
	//   2. Next Hop Groups used as backup NHGs
	//   3. Other Next Hop Groups
	//   4. Everything else
	opOrder := func(op *gribipb.AFTOperation) int {
		switch v := op.GetEntry().(type) {
		case *gribipb.AFTOperation_NextHop:
			return 0
		case *gribipb.AFTOperation_NextHopGroup:
			n := v.NextHopGroup.GetId()
			if slices.Contains(backupNHG, n) {
				return 1
			}
			return 2
		default:
			return 99
		}
	}
	sort.SliceStable(aftOps, func(i, j int) bool { return opOrder(aftOps[i]) < opOrder(aftOps[j]) })

	for i, aftOp := range aftOps {
		aftOp.Id = uint64(i) + 1 // Ensure ID starts at 1.
	}

	return &gribipb.ModifyRequest{
		Operation: aftOps,
	}, nil
}

func initSetReq(getResp *gnmipb.GetResponse) (*gnmipb.SetRequest, error) {
	ret := &gnmipb.SetRequest{}

	for _, notif := range getResp.GetNotification() {
		prefix := notif.GetPrefix()

		for _, update := range notif.GetUpdate() {
			path, err := util.JoinPaths(prefix, update.GetPath())
			if err != nil {
				return nil, err
			}
			ret.Update = append(ret.Update, &gnmipb.Update{
				Path: path,
				Val:  update.GetVal(),
			})
		}

		for _, delete := range notif.GetDelete() {
			path, err := util.JoinPaths(prefix, delete)
			if err != nil {
				return nil, err
			}
			ret.Delete = append(ret.Delete, path)
		}
	}

	return ret, nil
}

// prettySetRequest returns a pretty-formatted SetRequest proto with its JSON values pretty-
// formatted. If the JSON cannot be pretty-formatted, it will remain unchanged in the output.
func prettySetRequest(req *gnmipb.SetRequest) string {
	tryPrettyJSONVal := func(u *gnmipb.Update) {
		pj := prettyJSON(u.GetVal().GetJsonIetfVal())
		if pj == nil {
			return
		}
		u.Val = &gnmipb.TypedValue{Value: &gnmipb.TypedValue_JsonIetfVal{JsonIetfVal: pj}}
	}

	for _, u := range req.GetUpdate() {
		tryPrettyJSONVal(u)
	}
	for _, r := range req.GetReplace() {
		tryPrettyJSONVal(r)
	}

	return strings.ReplaceAll(prototext.Format(req), "\\n", "\n")
}

// prettyJSON converts a given json byte slice into a properly-indented one. It returns nil if the
// given slice cannot be prettied.
func prettyJSON(js []byte) []byte {
	if js == nil {
		return nil
	}
	var jv map[string]any
	if err := json.Unmarshal(js, &jv); err != nil {
		log.Error(err)
		return nil
	}
	out, err := json.MarshalIndent(jv, "", "  ")
	if err != nil {
		log.Error(err)
		return nil
	}
	return out
}
