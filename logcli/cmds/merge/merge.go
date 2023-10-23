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

// Package merge is a command for merging binary logs into a single log, ordered chronologically.
package merge

import (
	"os"

	"golang.org/x/exp/slices"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	lpb "github.com/openconfig/replayer/proto/log"
	bpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
)

var (
	files  []string
	output string
)

// New creates a new merge command.
func New() *cobra.Command {
	mergeCmd := &cobra.Command{
		Use:  "merge",
		RunE: merge,
	}

	mergeCmd.Flags().StringSliceVar(&files, "files", nil, "List of file paths of logs to be merged. Files can be stored in CNS or locally.")
	mergeCmd.MarkFlagRequired("files")

	mergeCmd.Flags().StringVar(&output, "output", "/tmp/dvtreplay_log", "Output path for merged log.")
	mergeCmd.MarkFlagRequired("output")

	return mergeCmd
}

func merge(cmd *cobra.Command, args []string) error {
	merged := &lpb.Events{}
	for _, path := range files {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		events := new(lpb.Events)
		if err := proto.Unmarshal(bytes, events); err != nil {
			return err
		}
		merged.GrpcEvents = append(merged.GetGrpcEvents(), events.GetGrpcEvents()...)
	}

	slices.SortFunc(merged.GrpcEvents, func(a, b *bpb.GrpcLogEntry) int {
		return a.GetTimestamp().AsTime().Compare(b.GetTimestamp().AsTime())
	})

	out, err := proto.Marshal(merged)
	if err != nil {
		return err
	}

	return os.WriteFile(output, out, 0666)
}
