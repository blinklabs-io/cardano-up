// Copyright 2026 Blink Labs Software
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
	"testing"

	"github.com/spf13/cobra"
)

// TestShouldCheckForNewVersion checks that the version check is skipped for
// "context env", "version", any "completion" subcommand, and cobra's hidden
// "__complete" shell-completion command, and is run for all other commands.
func TestShouldCheckForNewVersion(t *testing.T) {
	rootCmd := &cobra.Command{Use: programName}
	contextCmd := &cobra.Command{Use: "context"}
	contextEnvCmd := &cobra.Command{Use: "env"}
	contextListCmd := &cobra.Command{Use: "list"}
	contextCmd.AddCommand(contextEnvCmd, contextListCmd)
	completionCmd := &cobra.Command{Use: "completion"}
	completionBashCmd := &cobra.Command{Use: "bash"}
	completionCmd.AddCommand(completionBashCmd)
	versionCmd := &cobra.Command{Use: "version"}
	upCmd := &cobra.Command{Use: "up"}
	// Mirrors how cobra itself registers the hidden completion-request
	// command during Execute(): a single "__complete" command aliased as
	// "__completeNoDesc", both invoked by shells on every TAB keypress.
	shellCompCmd := &cobra.Command{
		Use:     cobra.ShellCompRequestCmd,
		Aliases: []string{cobra.ShellCompNoDescRequestCmd},
	}
	rootCmd.AddCommand(contextCmd, completionCmd, versionCmd, upCmd, shellCompCmd)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{
			name: "context env is skipped",
			cmd:  contextEnvCmd,
			want: false,
		},
		{
			name: "completion is skipped",
			cmd:  completionBashCmd,
			want: false,
		},
		{
			name: "version is skipped",
			cmd:  versionCmd,
			want: false,
		},
		{
			name: "hidden shell completion request is skipped",
			cmd:  shellCompCmd,
			want: false,
		},
		{
			name: "context list is checked",
			cmd:  contextListCmd,
			want: true,
		},
		{
			name: "up is checked",
			cmd:  upCmd,
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldCheckForNewVersion(test.cmd); got != test.want {
				t.Fatalf("unexpected check decision: got %t, want %t", got, test.want)
			}
		})
	}
}
