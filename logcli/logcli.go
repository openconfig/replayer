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

// logcli is a CLI for interacting with dvt replay logs.
package main

import (
	"fmt"
	"os"

	"github.com/openconfig/replayer/logcli/cmds/interfaces"
	"github.com/openconfig/replayer/logcli/cmds/merge"
	"github.com/openconfig/replayer/logcli/cmds/view"
	"github.com/spf13/cobra"
)

func main() {

	rootCmd := &cobra.Command{
		Use:   "logcli",
		Short: "Command line interface for DVT replay logs",
	}

	rootCmd.AddCommand(merge.New())
	rootCmd.AddCommand(interfaces.New())
	rootCmd.AddCommand(view.New())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
