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
	"sort"

	"github.com/blinklabs-io/cardano-up/pkgmgr"
	"github.com/spf13/cobra"
)

var contextFlags = struct {
	description string
	network     string
	force       bool
}{}

func contextCommand() *cobra.Command {
	contextCommand := &cobra.Command{
		Use:   "context",
		Short: "Manage the current context",
	}
	contextCommand.AddCommand(
		contextListCommand(),
		contextSelectCommand(),
		contextCreateCommand(),
		contextDeleteCommand(),
		contextEnvCommand(),
	)

	return contextCommand
}

func contextListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available contexts",
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			activeContext, _ := pm.ActiveContext()
			contexts := pm.Contexts()
			slog.Info("Contexts (* is active):\n")
			slog.Info(
				fmt.Sprintf(
					"  %-15s %-15s %s",
					"Name",
					"Network",
					"Description",
				),
			)
			var tmpContextNames []string
			for contextName := range contexts {
				tmpContextNames = append(tmpContextNames, contextName)
			}
			sort.Strings(tmpContextNames)
			// for contextName, context := range contexts {
			for _, contextName := range tmpContextNames {
				context := contexts[contextName]
				activeMarker := " "
				if contextName == activeContext {
					activeMarker = "*"
				}
				slog.Info(
					fmt.Sprintf(
						"%s %-15s %-15s %s",
						activeMarker,
						contextName,
						context.Network,
						context.Description,
					),
				)
			}
		},
	}
}

func contextSelectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "select <context name>",
		Short: "Select the active context",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("no context name provided")
			}
			if len(args) > 1 {
				return errors.New("only one context name may be specified")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			if err := pm.SetActiveContext(args[0]); err != nil {
				slog.Error(fmt.Sprintf("failed to set active context: %s", err))
				os.Exit(1)
			}
			slog.Info(
				fmt.Sprintf(
					"Selected context %q",
					args[0],
				),
			)
		},
	}
}

func contextCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <context name>",
		Short: "Create a new context",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("no context name provided")
			}
			if len(args) > 1 {
				return errors.New("only one context name may be specified")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			tmpContextName := args[0]
			tmpContext := pkgmgr.Context{
				Description: contextFlags.description,
				Network:     contextFlags.network,
			}
			if err := pm.AddContext(tmpContextName, tmpContext); err != nil {
				slog.Error(fmt.Sprintf("failed to add context: %s", err))
				os.Exit(1)
			}
		},
	}
	cmd.Flags().
		StringVarP(&contextFlags.description, "description", "d", "", "specifies description for context")
	cmd.Flags().
		StringVarP(&contextFlags.network, "network", "n", "", "specifies network for context. if not specified, it's set automatically on the first package install")
	return cmd
}

func contextDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <context name>",
		Short: "Delete a context",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("no context name provided")
			}
			if len(args) > 1 {
				return errors.New("only one context name may be specified")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			// Store original context name
			origContextName, _ := pm.ActiveContext()
			// Make sure we're not deleting the active context
			if args[0] == origContextName {
				slog.Error(pkgmgr.ErrContextNoDeleteActive.Error())
				os.Exit(1)
			}
			// Temporarily switch to selected context
			if err := pm.SetActiveContext(args[0]); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			installedPackages := pm.InstalledPackages()
			if len(installedPackages) > 0 {
				if !contextFlags.force {
					// Switch back to original context
					if err := pm.SetActiveContext(origContextName); err != nil {
						slog.Warn(err.Error())
					}
					slog.Error(
						"cannot delete context with packages installed. Uninstall packages or run with -f/--force",
					)
					os.Exit(1)
				}
				for _, installedPkg := range installedPackages {
					// Uninstall package
					if err := pm.Uninstall(installedPkg.Package.Name, false, true); err != nil {
						slog.Warn(err.Error())
					}
				}
			}
			// Switch back to original context
			if err := pm.SetActiveContext(origContextName); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			if err := pm.DeleteContext(args[0]); err != nil {
				slog.Error(fmt.Sprintf("failed to delete context: %s", err))
				os.Exit(1)
			}
			slog.Info(
				fmt.Sprintf(
					"Deleted context %q",
					args[0],
				),
			)
		},
	}
	cmd.Flags().
		BoolVarP(&contextFlags.force, "force", "f", false, "force removal of context with packages installed")
	return cmd
}

func contextEnvCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Generate environment vars for current context",
		Run: func(cmd *cobra.Command, args []string) {
			pm := createPackageManager()
			contextEnv := pm.ContextEnv()
			var tmpKeys []string
			for k := range contextEnv {
				tmpKeys = append(tmpKeys, k)
			}
			sort.Strings(tmpKeys)
			for _, key := range tmpKeys {
				slog.Info(
					fmt.Sprintf(
						"export %s=%s",
						key,
						contextEnv[key],
					),
				)
			}
		},
	}
	return cmd
}
