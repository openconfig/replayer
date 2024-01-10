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

// Package replayer provides methods for parsing and replaying logs of g*
// protocol messanges.
package replayer

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/replayer/internal"
	"google.golang.org/protobuf/testing/protocmp"

	gribipb "github.com/openconfig/gribi/v1/proto/service"
)

//go:generate ./compile_protos.sh

// Recording is a parsed gRIBI binary log record.
type Recording = internal.Recording

// Results contains the results of replayed requests.
type Results = internal.Results

// Config holds configuration for the replay, including RPC clients for g* protocols.
type Config = internal.Config

// ParseFile parses a binary log at the specified file path.
func ParseFile(t *testing.T, path string) *Recording {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ParseFile(): failed to read log file: %v", err)
	}
	rec, err := internal.ParseBytes(bytes)
	if err != nil {
		t.Fatalf("ParseFile(): failed to parse log: %v", err)
	}
	return rec
}

// ParseURL parses a binary log at the specified URL.
func ParseURL(t *testing.T, path string) *Recording {
	t.Helper()
	resp, err := http.Get(path)
	if err != nil {
		t.Fatalf("ParseURL(): failed to get log: %v", err)
	}
	defer resp.Body.Close()
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ParseURL(): failed to read log body: %v", err)
	}
	rec, err := internal.ParseBytes(bytes)
	if err != nil {
		t.Fatalf("ParseURL(): failed to parse log: %v", err)
	}
	return rec
}

// ParseBytes parses a binary log from bytes.
func ParseBytes(t *testing.T, bytes []byte) *Recording {
	t.Helper()
	rec, err := internal.ParseBytes(bytes)
	if err != nil {
		t.Fatalf("ParseBytes(): failed to parse log: %v", err)
	}
	return rec
}

// Replay sends the parsed record over the given gRIBI client.
func Replay(ctx context.Context, t *testing.T, r *Recording, config *Config) *Results {
	t.Helper()
	res, err := internal.Replay(ctx, r, config)
	if err != nil {
		t.Errorf("Replay(): failed to replay log: %v", err)
	}
	return res
}

// GRIBIDiff returns the diff of the final gRIBI state in the replay results
// with that of the given recording.
func GRIBIDiff(rec *Recording, res *Results) string {
	return cmp.Diff(res.FinalGRIBI(), rec.FinalGRIBI(),
		protocmp.Transform(),
		protocmp.SortRepeatedFields(&gribipb.GetResponse{}, "entry"),
		protocmp.IgnoreFields(&gribipb.AFTEntry{}, "fib_status"),
	)
}
