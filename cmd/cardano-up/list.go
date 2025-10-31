// Copyright 2025 Blink Labs Software
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
	"strings"

	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/hashicorp/go-version"
	"github.com/spf13/cobra"
)

var listFlags = struct {
	all bool
}{}

func listAvailableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-available",
		Short: "List available packages",
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			packages := pm.AvailablePackages()
			verbose, _ := cmd.Flags().GetBool("verbose")

			slog.Info("Available packages:\n")
			slog.Info(
				fmt.Sprintf(
					"%-20s %-12s %s",
					"Name",
					"Version",
					"Description",
				),
			)
			if verbose {
				// show all versions of packages
				for _, tmpPackage := range packages {
					printPackageInfo(tmpPackage)
				}
			} else {
				// Shows only latest version of each package
				latestPackages := make(map[string]int)
				order := make([]string, 0)
				for index, pkg := range packages {
					packageName := pkg.Name
					packageVersion := pkg.Version
					existingIndex, exists := latestPackages[packageName]
					if !exists || compareVersions(packageVersion, packages[existingIndex].Version) {
						if !exists {
							order = append(order, packageName)
						}
						latestPackages[packageName] = index
					}
				}
				for _, name := range order {
					printPackageInfo(packages[latestPackages[name]])
				}
			}
		},
	}
	// Added a verbose flag
	cmd.Flags().BoolP("verbose", "v", false, "Show all versions of packages")
	return cmd
}

func listCommand() *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
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
	listCmd.Flags().
		BoolVarP(&listFlags.all, "all", "A", false, "show packages from all contexts (defaults to only active context)")
	return listCmd
}

// Prints packge details
func printPackageInfo(pkg pkgmgr.Package) {
	slog.Info(
		fmt.Sprintf(
			"%-20s %-12s %s",
			pkg.Name,
			pkg.Version,
			pkg.Description,
		),
	)
	if len(pkg.Dependencies) > 0 {
		var sb strings.Builder
		sb.WriteString("    Requires: ")
		for idx, dep := range pkg.Dependencies {
			sb.WriteString(dep)
			if idx < len(pkg.Dependencies)-1 {
				sb.WriteString(` | `)
			}
		}
		slog.Info(sb.String())
	}
}

// Compare semantic version of packages
func compareVersions(v1 string, v2 string) bool {
	ver1, err1 := version.NewVersion(v1)
	ver2, err2 := version.NewVersion(v2)

	if err1 != nil || err2 != nil {
		return v1 > v2
	}
	return ver1.GreaterThan(ver2)
}
