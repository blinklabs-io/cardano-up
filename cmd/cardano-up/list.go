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
			pkgOutput := "Available packages:\n\n"
			for _, tmpPackage := range packages {
				pkgOutput += fmt.Sprintf("%s (%s)    %s\n", tmpPackage.Name, tmpPackage.Version, tmpPackage.Description)
			}
			slog.Info(pkgOutput)
		},
	}
}
