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
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var uninstallFlags = struct {
	keepData bool
}{}

func uninstallCommand() *cobra.Command {
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall package",
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
			// Uninstall package
			if err := pm.Uninstall(args[0], uninstallFlags.keepData, false); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
		},
	}
	uninstallCmd.Flags().
		BoolVarP(&uninstallFlags.keepData, "keep-data", "k", false, "don't cleanup package data")
	return uninstallCmd
}
