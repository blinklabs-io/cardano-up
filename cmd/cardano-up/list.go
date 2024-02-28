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

	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

var listFlags = struct {
	all bool
}{}

func listAvailableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list-available",
		Short: "List available packages",
		Run: func(cmd *cobra.Command, args []string) {
			pm, err := pkgmgr.NewDefaultPackageManager()
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
			}
			packages := pm.AvailablePackages()
			slog.Info("Available packages:\n")
			slog.Info(
				fmt.Sprintf(
					"%-20s %-12s %s",
					"Name",
					"Version",
					"Description",
				),
			)
			for _, tmpPackage := range packages {
				slog.Info(
					fmt.Sprintf(
						"%-20s %-12s %s",
						tmpPackage.Name,
						tmpPackage.Version,
						tmpPackage.Description,
					),
				)
			}
		},
	}
}

func listCommand() *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Run: func(cmd *cobra.Command, args []string) {
			pm, err := pkgmgr.NewDefaultPackageManager()
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
			}
			activeContextName, _ := pm.ActiveContext()
			var packages []pkgmgr.InstalledPackage
			if listFlags.all {
				packages = pm.InstalledPackagesAllContexts()
				slog.Info("Installed packages (all contexts):\n")
			} else {
				packages = pm.InstalledPackages()
				slog.Info(fmt.Sprintf("Installed packages (from context %q):\n", activeContextName))
			}
			if len(packages) > 0 {
				slog.Info(
					fmt.Sprintf(
						"%-20s %-12s %-15s %s",
						"Name",
						"Version",
						"Context",
						"Description",
					),
				)
				for _, tmpPackage := range packages {
					slog.Info(
						fmt.Sprintf(
							"%-20s %-12s %-15s %s",
							tmpPackage.Package.Name,
							tmpPackage.Package.Version,
							tmpPackage.Context,
							tmpPackage.Package.Description,
						),
					)
				}
			} else {
				slog.Info(`No packages installed`)
			}
		},
	}
	listCmd.Flags().BoolVarP(&listFlags.all, "all", "A", false, "show packages from all contexts (defaults to only active context)")
	return listCmd
}
