// Copyright 2024 Blink Labs Software
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
	"path/filepath"
	"strings"
	"time"

	"github.com/blinklabs-io/cardano-up/internal/consolelog"
	"github.com/blinklabs-io/cardano-up/internal/version"
	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

const (
	programName = "cardano-up"

	// noUpdateCheckEnvVar disables the version check entirely when set to
	// any non-empty value, for offline, air-gapped, or automation/CI
	// environments that don't want an outbound request to GitHub.
	noUpdateCheckEnvVar = "NO_UPDATE_CHECK"

	// versionCheckMaxWait bounds how long main will wait for the version
	// check goroutine below before giving up on it and letting the process
	// exit anyway. It's set a little above the check's own internal
	// network timeout as a safety margin, not as the primary bound - it
	// exists so main's exit is never at the mercy of the check
	// implementation alone.
	versionCheckMaxWait = 2 * time.Second
)

var globalFlags = struct {
	debug   bool
	context string
}{}

// rootCommandDeps carries state out of newRootCommand that main needs after
// Execute() returns. It's a struct (rather than a bare returned channel) so
// PersistentPreRun, which runs after newRootCommand has already returned,
// has somewhere to publish the channel it creates.
type rootCommandDeps struct {
	// versionCheckDone is nil unless PersistentPreRun ran (e.g. it doesn't
	// for --help), in which case it's closed once the background version
	// check finishes.
	versionCheckDone chan struct{}
}

// newRootCommand builds the full command tree. It's factored out of main so
// tests can exercise shouldCheckForNewVersion against the real, production
// command tree via Find, instead of a hand-built replica that could drift
// from it.
func newRootCommand(deps *rootCommandDeps) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: programName,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Configure default logger
			logLevel := slog.LevelInfo
			if globalFlags.debug {
				logLevel = slog.LevelDebug
			}
			logger := slog.New(
				consolelog.NewHandler(os.Stdout, &slog.HandlerOptions{
					Level: logLevel,
				}),
			)
			slog.SetDefault(logger)

			// The version-check notice logs to stderr, not stdout: some
			// commands (e.g. "logs --follow") stream meaningful data to
			// stdout that callers may parse or pipe, and a notice landing
			// in the middle of that stream would corrupt it.
			versionCheckLogger := slog.New(
				consolelog.NewHandler(os.Stderr, &slog.HandlerOptions{
					Level: logLevel,
				}),
			)

			// Run the version check concurrently with the command itself,
			// so a slow or unreachable GitHub endpoint doesn't delay the
			// start of command execution. main waits for it below, after
			// Execute() returns, so it isn't cut off mid-request by the
			// process exiting the instant the command's own work is done —
			// Go does not wait for background goroutines to finish on
			// program exit. This wait is skipped for subcommands that call
			// os.Exit directly on failure instead of returning an error, a
			// pre-existing pattern used throughout this CLI; closing that
			// gap would mean converting every such call site to return an
			// error instead, which is a much larger, separate change.
			deps.versionCheckDone = make(chan struct{})
			go func() {
				defer close(deps.versionCheckDone)
				checkForNewVersion(cmd, versionCheckLogger)
			}()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().
		BoolVarP(&globalFlags.debug, "debug", "D", false, "enable debug logging")
	rootCmd.PersistentFlags().
		StringVarP(&globalFlags.context, "context", "c", "", "target the named context for this command instead of the active one")

	// Add subcommands
	rootCmd.AddCommand(
		contextCommand(),
		versionCommand(),
		listCommand(),
		listAvailableCommand(),
		logsCommand(),
		infoCommand(),
		installCommand(),
		uninstallCommand(),
		upCommand(),
		startCommand(),
		downCommand(),
		stopCommand(),
		updateCommand(),
		upgradeCommand(),
		validateCommand(),
	)

	return rootCmd
}

func main() {
	deps := &rootCommandDeps{}
	rootCmd := newRootCommand(deps)

	cmdErr := rootCmd.Execute()
	if deps.versionCheckDone != nil {
		select {
		case <-deps.versionCheckDone:
		case <-time.After(versionCheckMaxWait):
		}
	}
	if cmdErr != nil {
		// NOTE: we purposely don't display the error, since cobra will have already displayed it
		os.Exit(1)
	}
}

func createPackageManager() *pkgmgr.PackageManager {
	cfg, err := pkgmgr.NewDefaultConfig()
	if err != nil {
		slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
		os.Exit(1)
	}
	// Allow setting registry URL/dir via env var
	if url, ok := os.LookupEnv("REGISTRY_URL"); ok {
		cfg.RegistryUrl = url
	}
	if dir, ok := os.LookupEnv("REGISTRY_DIR"); ok {
		cfg.RegistryDir = dir
	}
	pm, err := pkgmgr.NewPackageManager(cfg)
	if err != nil {
		slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
		os.Exit(1)
	}
	// Apply per-invocation context override (--context) without mutating the
	// persisted active context
	if globalFlags.context != "" {
		if err := pm.SetActiveContextOverride(globalFlags.context); err != nil {
			slog.Error(
				fmt.Sprintf(
					"invalid --context %q: %s",
					globalFlags.context,
					err,
				),
			)
			os.Exit(1)
		}
	}
	return pm
}

// checkForNewVersion looks up whether a newer release is available and, if
// so, logs a warning naming the current and latest versions to logger. It is
// run in a goroutine from the root command's PersistentPreRun (see
// newRootCommand), which waits for it to finish before the process exits;
// any error here is only logged at debug level and never blocks or fails
// the command. Set NO_UPDATE_CHECK to any non-empty value to disable this
// entirely, e.g. for offline or CI use.
func checkForNewVersion(cmd *cobra.Command, logger *slog.Logger) {
	if version.Version == "" || !shouldCheckForNewVersion(cmd.CommandPath()) {
		return
	}
	if _, disabled := os.LookupEnv(noUpdateCheckEnvVar); disabled {
		return
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		logger.Debug("failed to determine cache directory for version check: " + err.Error())
		return
	}
	update, err := version.CheckForUpdate(
		filepath.Join(userCacheDir, programName),
	)
	if err != nil {
		logger.Debug("failed to check for a newer version: " + err.Error())
		return
	}
	if update == nil {
		return
	}
	logger.Warn(
		fmt.Sprintf(
			"A newer version of %s is available: %s (current: %s). Download it from %s",
			programName,
			update.LatestVersion,
			update.CurrentVersion,
			update.ReleaseURL,
		),
	)
}

// shouldCheckForNewVersion reports whether the version check should run for
// the given command path (as returned by cobra.Command.CommandPath()). It's
// skipped for "context env" (whose output is meant to be eval'd), "version"
// (redundant), "help", any "completion" subcommand (whose output is a shell
// script), and cobra's hidden "__complete"/"__completeNoDesc" commands
// (invoked by shells on every TAB keypress during interactive completion, so
// they must never block on a network call or print a warning). Taking a
// plain string, rather than a *cobra.Command, keeps this directly testable
// against real command paths without needing a live cobra tree.
func shouldCheckForNewVersion(commandPath string) bool {
	if commandPath == programName+" context env" ||
		commandPath == programName+" version" ||
		commandPath == programName+" help" {
		return false
	}
	return !strings.HasPrefix(commandPath, programName+" completion") &&
		!strings.HasPrefix(commandPath, programName+" __complete")
}
