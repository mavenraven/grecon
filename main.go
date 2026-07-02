package main

import (
	"os"

	"grecon/client"
	"grecon/server"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "grecon",
		Short:   "Pick and switch between Claude Code sessions running in tmux",
		Version: "0.7.0",
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := client.RunTUI()
			if err != nil {
				return err
			}
			if target != "" {
				client.SwitchToPane(target)
			}
			return nil
		},
		SilenceUsage: true,
	}

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Run a background server that discovers and streams session data",
		Run: func(cmd *cobra.Command, args []string) {
			server.RunServer()
		},
	}

	rootCmd.AddCommand(serverCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
