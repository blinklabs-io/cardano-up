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
	"os"

	"github.com/spf13/cobra"
)

const (
	programName = "cardano-up"
)

func main() {
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
	}

	// Add subcommands
	rootCmd.AddCommand(
		versionCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		// NOTE: we purposely don't display the error, since cobra will have already displayed it
		os.Exit(1)
	}
}
