package replay

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/openconfig/gribigo/client"
	"github.com/openconfig/gribigo/fluent"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	spb "github.com/openconfig/gribi/v1/proto/service"
	logpb "github.com/openconfig/replayer/proto/log"
	lpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
)

// FromLogProto reads a logpb.Events message from the specified file name and
// returns the set of GrpcLogEntry gRPC binary log messags that are stored in
// it. It returns an error if one is encountered reading or unmarshalling the
// protobuf.
func FromLogProto(fn string) ([]*lpb.GrpcLogEntry, error) {
	f, err := os.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot read binary protobuf %s, error: %v", fn, err)
	}

	p := &logpb.Events{}
	if err := proto.Unmarshal(f, p); err != nil {
		return nil, fmt.Errorf("cannot unmarshal protobuf, error: %v", err)
	}
	return p.GrpcEvents, nil
}

// FromTextprotoFile reads a series of gRPC GrpcLogEntry protobufs from a
// text file named fn. The text file must consist of a series of textprotos
// separated by newlines. It returns an error if one is encountered reading the
// file or unmarshalling any of the individual messages.
func FromTextprotoFile(fn string) ([]*lpb.GrpcLogEntry, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot read file %s, error: %v", fn, err)
	}

	msgs := []*lpb.GrpcLogEntry{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		p := &lpb.GrpcLogEntry{}
		if err := prototext.Unmarshal(s.Bytes(), p); err != nil {
			return nil, fmt.Errorf("invalid entry in log, message `%s` could not be converted to GrpcLogEntry, %v", s.Bytes(), err)
		}
		msgs = append(msgs, p)
	}
	return msgs, nil
}

// timeseries is a series of gRIBI ModifyRequests bucketed by their start time.
type timeseries map[time.Time][]*spb.ModifyRequest

// Timeseries takes a slice of GrpcLogEntry binary entries, and a specified
// duration used to bucket those events, and returns a timeseries of the events
// broken down by time. The timeQuantum is used to group events that occcur
// within a specific window - e.g., if this value is set as 100 milliseconds,
// then all events that occur within 100ms of the first event are grouped into
// the same bucket.
//
// Timeseries filters events to be the client messages and expects the slice to
// contain only gRIBI ModifyRequests.
func Timeseries(pb []*lpb.GrpcLogEntry, timeQuantum time.Duration) (timeseries, error) {
	ts := timeseries{}
	timeBucket := time.Unix(0, 0)
	for _, p := range pb {
		if p.Type != lpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE {
			continue
		}

		if p.GetTimestamp() == nil {
			return nil, fmt.Errorf("invalid protobuf with nil timestamp, %s", p)
		}

		eventTime := time.Unix(p.Timestamp.Seconds, int64(p.Timestamp.Nanos))
		if eventTime.Sub(timeBucket) >= timeQuantum {
			timeBucket = eventTime
		}

		m := &spb.ModifyRequest{}
		if err := proto.Unmarshal(p.GetMessage().GetData(), m); err != nil {
			return nil, fmt.Errorf("cannot unmarshal ModifyRequest %s, %v", p, err)
		}
		ts[timeBucket] = append(ts[timeBucket], m)
	}
	return ts, nil
}

// event is a datapoint within a replay stream. When events are replayed
// the replayer implementation should initially sleep for the period
// indicated by DelayBefore and then replay out the Events specified.
type event struct {
	// DelayBefore is the duration for which the replayer should sleep
	// before replaying these events immediately after having played
	// out the prior event.
	DelayBefore time.Duration
	// Events is the set of ModifyRequests that should be played out
	// for specific event. The messages should be replayed in the
	// specified order, with no delay between them.
	Events []*spb.ModifyRequest
}

// Schedule takes an input timeseries and converts it to a slice of
// events to be replayed.
func Schedule(ts timeseries) ([]*event, error) {
	times := []time.Time{}
	for t := range ts {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	prev := times[0]
	sched := []*event{}
	for _, t := range times {
		sched = append(sched, &event{
			DelayBefore: t.Sub(prev),
			Events:      ts[t],
		})
		prev = t
	}
	return sched, nil
}

// awaitTimeout waits for the client c to converge, waiting for the specified
// timeout.
func awaitTimeout(ctx context.Context, c *fluent.GRIBIClient, t testing.TB, timeout time.Duration) error {
	subctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Await(subctx, t)
}

// Do replays the contents of the schedule (sched) provided using the specified
// gRIBI Client c. The timeMultiplier is used in order to multiply the delays
// specified in the event stream. The specified timeout is used to determine
// how long the server takes to converge following the events.
func Do(ctx context.Context, t testing.TB, c *fluent.GRIBIClient, sched []*event, timeMultiplier int, timeout time.Duration) []*client.OpResult {
	c.Start(ctx, t)
	defer c.Stop(t)
	c.StartSending(ctx, t)
	if err := awaitTimeout(ctx, c, t, timeout); err != nil {
		t.Fatalf("got unexpected error from server - session negotiation, got: %v, want: nil", err)
	}

	for _, s := range sched {
		extendedTime := s.DelayBefore * time.Duration(timeMultiplier)
		log.Infof("sleeping %s (exaggerated)\n", extendedTime)
		time.Sleep(extendedTime)
		log.Infof("sending %v\n", s.Events)
		c.Modify().Enqueue(t, s.Events...)
	}
	if err := awaitTimeout(ctx, c, t, timeout); err != nil {
		t.Fatalf("got unexpected error from server - session negotiation, got: %v, want: nil", err)
	}
	return c.Results(t)
}
