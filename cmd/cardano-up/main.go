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
	"log/slog"
	"os"

	"github.com/blinklabs-io/cardano-up/internal/consolelog"

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
		/*
			Short: "A brief description of your application",
			Long: `A longer description that spans multiple lines and likely contains
			examples and usage of using your application. For example:

			Cobra is a CLI library for Go that empowers applications.
			This application is a tool to generate the needed files
			to quickly create a Cobra application.`,
		*/
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
	rootCmd.PersistentFlags().BoolVarP(&globalFlags.debug, "debug", "D", false, "enable debug logging")

	// Add subcommands
	rootCmd.AddCommand(
		contextCommand(),
		versionCommand(),
		listCommand(),
		listAvailableCommand(),
		installCommand(),
		uninstallCommand(),
		upCommand(),
		downCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		// NOTE: we purposely don't display the error, since cobra will have already displayed it
		os.Exit(1)
	}
}
