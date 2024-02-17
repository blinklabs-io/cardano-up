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

func installCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install package",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				slog.Error("no package provided")
				os.Exit(1)
			}
			if len(args) > 1 {
				slog.Error("only one package may be specified a a time")
				os.Exit(1)
			}
			pm, err := pkgmgr.NewDefaultPackageManager()
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
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
		},
	}
}
