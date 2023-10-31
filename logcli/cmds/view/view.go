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

// Package view is a command for viewing the contents of a binary log file in a
// human-readable format.
package view

import (
	"errors"
	"fmt"
	"os"

	"github.com/openconfig/replayer/internal"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	lpb "github.com/openconfig/replayer/proto/log"
)

var (
	file      string
	transform bool
)

// New creates a new view command.
func New() *cobra.Command {
	viewCmd := &cobra.Command{
		Use:  "view",
		RunE: view,
	}

	viewCmd.Flags().StringVar(&file, "file", "", "File path of log to be viewed. Files can be stored in CNS or locally.")
	viewCmd.MarkFlagRequired("file")

	viewCmd.Flags().BoolVar(&transform, "transform", false, "Whether or not to transform the log according to the replayer transformation logic. Transformed logs more accurately represent the events that will get sent by the replayer tool.")

	return viewCmd
}

func view(cmd *cobra.Command, args []string) error {
	bytes, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	var l []*internal.Event
	if transform {
		l, err = parseAndTransformLog(bytes)
	} else {
		l, err = parseLog(bytes)
	}
	if err != nil {
		return err
	}

	return runTUI(l)
}

func parseLog(bytes []byte) ([]*internal.Event, error) {
	events := new(lpb.Events)
	if err := proto.Unmarshal(bytes, events); err != nil {
		return nil, err
	}

	entries := events.GetGrpcEvents()
	if len(entries) == 0 {
		return nil, errors.New("no log entries found")
	}

	var l []*internal.Event

	for i, entry := range entries {
		data := entry.GetMessage().GetData()
		timestamp := entry.GetTimestamp().AsTime()

		m, err := internal.UnmarshalLogEntry(data)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal log entry %v: %w", i, err)
		}

		l = append(l, &internal.Event{
			Timestamp: timestamp,
			Message:   m,
		})

	}
	return l, nil
}

func parseAndTransformLog(bytes []byte) ([]*internal.Event, error) {
	r, err := internal.ParseBytes(bytes)
	if err != nil {
		return nil, err
	}
	intfs, err := r.Interfaces()
	if err != nil {
		return nil, err
	}

	// Map gRIBI interfaces to placeholder values so they appear in the transformed log.
	portNum := 0
	newPlaceholderPort := func() string {
		portNum++
		return fmt.Sprintf("[Port %d]", portNum)
	}

	intfMap := map[string]string{}
	for bundle, members := range intfs {
		if len(members) == 0 {
			// The "bundle" is not a bundle, so replace directly.
			intfMap[bundle] = newPlaceholderPort()
		} else {
			// The "bundle" is a bundle, so replace all members of the bundle.
			for _, member := range members {
				intfMap[member.Name] = newPlaceholderPort()
			}
		}
	}

	if err := r.SetInterfaceMap(intfMap); err != nil {
		return nil, err
	}

	return internal.GenerateReplayEvents(r)
}
