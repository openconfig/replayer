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

// Package view is a command for viewing the contents of a binary log file in a human-readable format.
package view

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/openconfig/replayer"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	lpb "github.com/openconfig/replayer/proto/log"
)

var (
	file string
)

// New creates a new view command.
func New() *cobra.Command {
	viewCmd := &cobra.Command{
		Use:  "view",
		RunE: view,
	}

	viewCmd.Flags().StringVar(&file, "file", "", "File path of log to be viewed. Files can be stored in CNS or locally.")
	viewCmd.MarkFlagRequired("file")

	return viewCmd
}

func view(cmd *cobra.Command, args []string) error {
	l, err := parseLog(file)
	if err != nil {
		return err
	}

	return runTUI(l)
}

func parseLog(path string) (*binaryLog, error) {
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
		return nil, errors.New("no log entries found")
	}

	l := &binaryLog{}

	for i, entry := range entries {
		data := entry.GetMessage().GetData()
		timestamp := entry.GetTimestamp().AsTime()

		m, err := replayer.UnmarshalLogEntry(data)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal log entry %v: %w", i, err)
		}

		l.entries = append(l.entries, &logEntry{
			t: timestamp,
			m: m,
		})

	}
	return l, nil
}

type binaryLog struct {
	entries []*logEntry
}

type logEntry struct {
	t time.Time
	m proto.Message
}
