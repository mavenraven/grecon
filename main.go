package main

import (
	"encoding/json"
	"fmt"
	"os"

	"grecon/client"
	"grecon/db"
	"grecon/server"

	"github.com/spf13/cobra"
)

func main() {
	var instanceName string

	rootCmd := &cobra.Command{
		Use:     "grecon",
		Short:   "Monitor and manage Claude Code sessions running in tmux",
		Version: "0.6.1",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if instanceName != "" {
				db.SetInstance(instanceName)
			}
		},
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

	newCmd := &cobra.Command{
		Use:   "new [session-name]",
		Short: "Interactive form to create a new tmux session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := server.RequireFetch(); err != nil {
				return err
			}
			var initialName string
			if len(args) > 0 {
				initialName = args[0]
			}
			name, ok := client.RunNewSessionForm(initialName)
			if ok && name != "" {
				client.SwitchToPane(name)
			}
			return nil
		},
	}

	var launchName, launchCWD, launchCommand string
	var launchAttach, launchWorktree bool
	var launchTags []string
	launchCmd := &cobra.Command{
		Use:   "launch",
		Short: "Create a new claude session (background by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			defName, defCWD := client.DefaultNewSessionInfo()
			name := defName
			if launchName != "" {
				name = launchName
			}
			cwd := defCWD
			if launchCWD != "" {
				cwd = launchCWD
			}
			var cmdPtr *string
			if launchCommand != "" {
				cmdPtr = &launchCommand
			}
			claudeName := client.GenerateFunName()
			sessName, err := client.CreateSession(name, cwd, claudeName, cmdPtr, launchTags, launchWorktree)
			if err != nil {
				return err
			}
			if launchAttach {
				client.SwitchToPane(sessName)
			}
			fmt.Fprintf(os.Stderr, "Session: %s\n", sessName)
			return nil
		},
	}
	launchCmd.Flags().StringVar(&launchName, "name", "", "Custom session name")
	launchCmd.Flags().StringVar(&launchCWD, "cwd", "", "Working directory")
	launchCmd.Flags().StringVar(&launchCommand, "command", "", "Custom command to run")
	launchCmd.Flags().BoolVar(&launchAttach, "attach", false, "Attach after creating")
	launchCmd.Flags().StringSliceVar(&launchTags, "tag", nil, "Tag the session (key:value)")
	launchCmd.Flags().BoolVar(&launchWorktree, "worktree", false, "Create a git worktree")

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Run a background server that caches session data",
		Run: func(cmd *cobra.Command, args []string) {
			server.RunServer()
		},
	}

	jsonCmd := &cobra.Command{
		Use:   "json",
		Short: "Print current session state as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions := server.TryFetch()
			if sessions == nil {
				return fmt.Errorf("grecon server is not running")
			}
			data, err := json.MarshalIndent(sessions, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}

	nameCmd := &cobra.Command{
		Use:   "name",
		Short: "Generate a random fun name",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(client.GenerateFunName())
		},
	}

	rootCmd.PersistentFlags().StringVarP(&instanceName, "instance", "i", "", "Instance name (namespaces DB, sockets, lock file)")
	rootCmd.AddCommand(newCmd, launchCmd, serverCmd, jsonCmd, nameCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
