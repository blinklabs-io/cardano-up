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
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

const (
	// The default network used when installing into an empty context
	defaultNetwork = "preprod"
)

var installFlags = struct {
	network string
}{}

func installCommand() *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install package",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("no package provided")
			}
			if len(args) > 1 {
				return errors.New("only one package may be specified at a time")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			activeContextName, activeContext := pm.ActiveContext()
			// Update context network if specified
			if installFlags.network != "" {
				activeContext.Network = installFlags.network
				if err := pm.UpdateContext(activeContextName, activeContext); err != nil {
					slog.Error(err.Error())
					os.Exit(1)
				}
				slog.Debug(
					fmt.Sprintf(
						"set active context network to %q",
						installFlags.network,
					),
				)
			}
			// Check that context network is set
			if activeContext.Network == "" {
				activeContext.Network = defaultNetwork
				if err := pm.UpdateContext(activeContextName, activeContext); err != nil {
					slog.Error(err.Error())
					os.Exit(1)
				}
				slog.Warn(
					fmt.Sprintf(
						"defaulting to network %q for context %q",
						defaultNetwork,
						activeContextName,
					),
				)
			}
			// Install requested package
			if err := pm.Install(args[0]); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
		},
	}
	installCmd.Flags().StringVarP(&installFlags.network, "network", "n", "", fmt.Sprintf("specifies network for package (defaults to %q for empty context)", defaultNetwork))
	return installCmd
}
