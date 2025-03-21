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

	"github.com/blinklabs-io/cardano-up/internal/consolelog"
	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

const (
	programName = "cardano-up"
)

func main() {
	globalFlags := struct {
		debug bool
	}{}

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
		},
	}

	// Global flags
	rootCmd.PersistentFlags().
		BoolVarP(&globalFlags.debug, "debug", "D", false, "enable debug logging")

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
		downCommand(),
		updateCommand(),
		upgradeCommand(),
		validateCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
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
	return pm
}
