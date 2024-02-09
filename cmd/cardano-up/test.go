// Copyright 2023 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

func testCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Test subcommand for development work (TODO: remove me)",
		Run: func(cmd *cobra.Command, args []string) {
			pm, err := pkgmgr.NewDefaultPackageManager()
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
			}
			slog.Debug(fmt.Sprintf("pm = %#v", pm))
		},
	}
}
