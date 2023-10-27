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

// Package interfaces defines the interfaces analysis command for the log CLI.
package interfaces

import (
	"fmt"
	"os"

	"github.com/kylelemons/godebug/pretty"
	"github.com/openconfig/replayer/internal"
	"github.com/spf13/cobra"
)

// New creates a new interfaces command.
func New() *cobra.Command {
	intfCmd := &cobra.Command{
		Use:   "interfaces",
		Short: "Analyze interfaces present in a replay binary log.",
		RunE:  interfaces,
	}

	intfCmd.Flags().StringVar(&logPath, "log_path", "", "Path to the binary log to be analyzed.")
	intfCmd.MarkFlagRequired("log_path")
	return intfCmd
}

var (
	logPath string
)

func interfaces(cmd *cobra.Command, args []string) error {
	b, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	r, err := internal.ParseBytes(b)
	if err != nil {
		return err
	}
	intfs, err := r.Interfaces()
	if err != nil {
		return err
	}

	total := 0
	for _, members := range intfs {
		if len(members) == 0 {
			total++
		} else {
			total += len(members)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "INTERFACES (%v needed): %v\n", total, pretty.Sprint(intfs))
	return nil
}
