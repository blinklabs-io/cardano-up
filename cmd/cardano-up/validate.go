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
	"path/filepath"

	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

func validateCommand() *cobra.Command {
	validateCmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate package file(s) in the given directory",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("only one package directory may be specified at a time")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			packagesDir := "."
			if len(args) > 0 {
				packagesDir = args[0]
			}
			absPackagesDir, err := filepath.Abs(packagesDir)
			if err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			cfg, err := pkgmgr.NewDefaultConfig()
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
			}
			// Point at provided registry dir
			cfg.RegistryDir = absPackagesDir
			// Disable preloading of registry to prevent errors before we explicitly start validation
			cfg.RegistryPreload = false
			pm, err := pkgmgr.NewPackageManager(cfg)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to create package manager: %s", err))
				os.Exit(1)
			}
			slog.Info(
				fmt.Sprintf(
					"Validating packages in path %s",
					absPackagesDir,
				),
			)
			if err := pm.ValidatePackages(); err != nil {
				slog.Error("problems were found")
				os.Exit(1)
			}
			slog.Info("No problems found!")
		},
	}
	return validateCmd
}
