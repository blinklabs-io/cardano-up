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

	"github.com/blinklabs-io/cardano-up/internal/consolelog"
	"github.com/blinklabs-io/cardano-up/internal/version"
	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

const (
	programName = "cardano-up"
)

var globalFlags = struct {
	debug   bool
	context string
}{}

func main() {
	// versionCheckDone is nil unless PersistentPreRun ran (e.g. it doesn't
	// for --help), in which case it's closed once the background version
	// check finishes.
	var versionCheckDone chan struct{}

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
			versionCheckDone = make(chan struct{})
			go func() {
				defer close(versionCheckDone)
				checkForNewVersion(cmd)
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

	cmdErr := rootCmd.Execute()
	if versionCheckDone != nil {
		<-versionCheckDone
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
// so, logs a warning naming the current and latest versions. It is run in a
// goroutine from the root command's PersistentPreRun (see main), which waits
// for it to finish before the process exits; any error here is only logged
// at debug level and never blocks or fails the command.
func checkForNewVersion(cmd *cobra.Command) {
	if version.Version == "" || !shouldCheckForNewVersion(cmd) {
		return
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		slog.Debug("failed to determine cache directory for version check: " + err.Error())
		return
	}
	update, err := version.CheckForUpdate(
		filepath.Join(userCacheDir, programName),
	)
	if err != nil {
		slog.Debug("failed to check for a newer version: " + err.Error())
		return
	}
	if update == nil {
		return
	}
	slog.Warn(
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
// the given command. It is skipped for "context env" (whose output is
// meant to be eval'd), "version" (redundant), "help", any "completion"
// subcommand (whose output is a shell script), and cobra's hidden
// "__complete"/"__completeNoDesc" commands (invoked by shells on every TAB
// keypress during interactive completion, so they must never block on a
// network call or print a warning).
func shouldCheckForNewVersion(cmd *cobra.Command) bool {
	commandPath := cmd.CommandPath()
	if commandPath == programName+" context env" ||
		commandPath == programName+" version" ||
		commandPath == programName+" help" {
		return false
	}
	return !strings.HasPrefix(commandPath, programName+" completion") &&
		!strings.HasPrefix(commandPath, programName+" __complete")
}
