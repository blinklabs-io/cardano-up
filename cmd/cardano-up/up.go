package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

func upCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Starts all Docker containers",
		Long:  `Starts all stopped Docker containers for installed packages in the current context.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pm := createPackageManager()
			if err := pm.Up(); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			return nil
		},
	}
	return cmd
}
