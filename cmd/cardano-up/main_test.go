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
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/blinklabs-io/cardano-up/internal/consolelog"
	"github.com/blinklabs-io/cardano-up/internal/version"
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
	// actually running the command tree, so these cases are checked
	// directly against the paths cobra's own exported constants would
	// produce. "__completeNoDesc" is an alias, not a separate command, so a
	// real invocation's CommandPath() is always "... __complete" regardless
	// of which name the shell invoked it as - shouldCheckForNewVersion only
	// ever sees that one form in production. The second case below still
	// pins down real, independent coverage: it confirms the skip is a
	// prefix match broad enough to also catch "__completeNoDesc" directly,
	// not narrowed to an exact match on "__complete" alone.
	hiddenCompletionCases := []struct {
		name       string
		commandArg string
	}{
		{
			name:       "__complete is skipped",
			commandArg: cobra.ShellCompRequestCmd,
		},
		{
			name:       "__completeNoDesc is skipped",
			commandArg: cobra.ShellCompNoDescRequestCmd,
		},
	}
	for _, test := range hiddenCompletionCases {
		t.Run(test.name, func(t *testing.T) {
			commandPath := programName + " " + test.commandArg
			if got := shouldCheckForNewVersion(commandPath); got != false {
				t.Fatalf("unexpected check decision: got %t, want false", got)
			}
		})
	}
}

// TestCheckForNewVersionRespectsOptOutEnvVar checks that setting
// NO_UPDATE_CHECK skips the check entirely - before any network call would
// be made - even for a command and version that would otherwise trigger it.
// checkForNewVersion takes an *http.Client explicitly, so this passes one
// backed by a RoundTripper that fails the test outright if it's ever
// invoked, rather than mutating the shared http.DefaultClient.Transport
// (which would be fragile under parallel tests or any other code path
// using the default client during this window). Only checking for absent
// log output wouldn't prove this on its own: fetch errors are logged at
// Debug, which the buffer-backed logger below never surfaces anyway, so a
// regressed opt-out guard could silently let a real (successful or failed)
// network call through and this test would still pass.
func TestCheckForNewVersionRespectsOptOutEnvVar(t *testing.T) {
	t.Setenv(noUpdateCheckEnvVar, "1")

	// Isolate os.UserCacheDir() to an empty temp dir on every OS. Without
	// this, a real pre-existing cache file on the machine running the test
	// (e.g. left over from manually exercising the real binary) would let
	// checkForNewVersion answer from disk and never reach the network
	// layer at all, making the injected client below moot.
	cacheDir := t.TempDir()
	t.Setenv("HOME", cacheDir)
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("LocalAppData", cacheDir)

	originalVersion := version.Version
	version.Version = "v0.0.1"
	t.Cleanup(func() { version.Version = originalVersion })

	failingClient := &http.Client{
		Transport: roundTripFunc(
			func(req *http.Request) (*http.Response, error) {
				t.Fatal("unexpected network call made while NO_UPDATE_CHECK is set")
				return nil, nil
			},
		),
	}

	var buf bytes.Buffer
	logger := slog.New(consolelog.NewHandler(&buf, nil))

	rootCmd := newRootCommand(&rootCommandDeps{})
	cmd, _, err := rootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("failed to resolve command: %s", err)
	}

	checkForNewVersion(cmd, logger, failingClient)

	if buf.Len() != 0 {
		t.Fatalf("expected no output when opt-out env var is set, got: %q", buf.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
