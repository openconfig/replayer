package replay

import (
	"bufio"
	"fmt"
	"os"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

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
