package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

func downCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stops all Docker containers",
		Long:  `Stops all running Docker containers for installed packages in the current context.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pm := createPackageManager()
			if err := pm.Down(); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			return nil
		},
	}
	return cmd
}
