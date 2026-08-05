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
// It resolves real subcommand paths from the production command tree built
// by newRootCommand, rather than a hand-built replica, so the coverage
// tracks the actual command names rather than a copy that could drift from
// them.
func TestShouldCheckForNewVersion(t *testing.T) {
	rootCmd := newRootCommand(&rootCommandDeps{})
	// cobra only registers the default "completion" command inside
	// Execute(); call the exported initializer directly so Find can resolve
	// it without actually executing the command tree.
	rootCmd.InitDefaultCompletionCmd()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "context env is skipped",
			args: []string{"context", "env"},
			want: false,
		},
		{
			name: "completion is skipped",
			args: []string{"completion", "bash"},
			want: false,
		},
		{
			name: "version is skipped",
			args: []string{"version"},
			want: false,
		},
		{
			name: "context list is checked",
			args: []string{"context", "list"},
			want: true,
		},
		{
			name: "up is checked",
			args: []string{"up"},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, _, err := rootCmd.Find(test.args)
			if err != nil {
				t.Fatalf("failed to resolve command %v: %s", test.args, err)
			}
			if got := shouldCheckForNewVersion(cmd.CommandPath()); got != test.want {
				t.Fatalf("unexpected check decision: got %t, want %t", got, test.want)
			}
		})
	}

	// cobra only registers "__complete" (aliased as "__completeNoDesc")
	// inside Execute() itself, with no exported way to add it without
	// actually running the command tree, so this case is checked directly
	// against the path cobra's own exported constant would produce.
	t.Run("hidden shell completion request is skipped", func(t *testing.T) {
		commandPath := programName + " " + cobra.ShellCompRequestCmd
		if got := shouldCheckForNewVersion(commandPath); got != false {
			t.Fatalf("unexpected check decision: got %t, want false", got)
		}
	})
}
