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

	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
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
				return errors.New("only one package may be specified a a time")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			pm, err := pkgmgr.NewDefaultPackageManager()
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
			}
			if installFlags.network != "" {
				activeContextName, activeContext := pm.ActiveContext()
				if activeContext.Network == "" {
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
				} else {
					if activeContext.Network != installFlags.network {
						slog.Error(
							fmt.Sprintf(
								"active context already has network %q, cannot set to %q",
								activeContext.Network,
								installFlags.network,
							),
						)
						os.Exit(1)
					}
				}
			}
			packages := pm.AvailablePackages()
			foundPackage := false
			for _, tmpPackage := range packages {
				if tmpPackage.Name == args[0] {
					foundPackage = true
					if err := pm.Install(tmpPackage); err != nil {
						slog.Error(err.Error())
						os.Exit(1)
					}
					break
				}
			}
			if !foundPackage {
				slog.Error(fmt.Sprintf("no such package: %s", args[0]))
				os.Exit(1)
			}
			slog.Info(fmt.Sprintf("Successfully installed package %s", args[0]))
		},
	}
	installCmd.Flags().StringVarP(&installFlags.network, "network", "n", "preprod", "specifies network for package")
	return installCmd
}
