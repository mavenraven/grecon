package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var execCommand = exec.Command

func main() {
	rootCmd := &cobra.Command{
		Use:     "grecon",
		Short:   "Monitor and manage Claude Code sessions running in tmux",
		Version: "0.6.1",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
		SilenceUsage: true,
	}

	newCmd := &cobra.Command{
		Use:   "new",
		Short: "Interactive form to create a new tmux session",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, ok := runNewSessionForm()
			if ok && name != "" {
				switchToPane(name)
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
			defName, defCWD := defaultNewSessionInfo()
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
			sessName, err := createSession(name, cwd, cmdPtr, launchTags, launchWorktree)
			if err != nil {
				return err
			}
			if launchAttach {
				switchToPane(sessName)
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

	var resumeID, resumeName string
	var resumeNoAttach bool
	resumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a past session (interactive picker, or by ID)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if resumeID != "" {
				var namePtr *string
				if resumeName != "" {
					namePtr = &resumeName
				}
				sess, err := resumeSession(resumeID, namePtr)
				if err != nil {
					return err
				}
				if !resumeNoAttach {
					switchToPane(sess)
				}
				fmt.Fprintf(os.Stderr, "Resumed in session: %s\n", sess)
				return nil
			}
			sessionID, sessName, ok := runResumePicker()
			if !ok {
				return nil
			}
			sess, err := resumeSession(sessionID, &sessName)
			if err != nil {
				return err
			}
			switchToPane(sess)
			fmt.Fprintf(os.Stderr, "Resumed in session: %s\n", sess)
			return nil
		},
	}
	resumeCmd.Flags().StringVar(&resumeID, "id", "", "Session ID to resume directly")
	resumeCmd.Flags().StringVar(&resumeName, "name", "", "Custom tmux session name")
	resumeCmd.Flags().BoolVar(&resumeNoAttach, "no-attach", false, "Don't attach after resuming")

	nextCmd := &cobra.Command{
		Use:   "next",
		Short: "Jump to the next agent waiting for input",
		Run: func(cmd *cobra.Command, args []string) {
			app := NewApp()
			app.Refresh()
			for _, s := range app.Sessions {
				if s.Status == StatusInput && s.PaneTarget != "" {
					switchToPane(s.PaneTarget)
					return
				}
			}
		},
	}

	var jsonTags []string
	jsonCmd := &cobra.Command{
		Use:   "json",
		Short: "Print all session state as JSON",
		Run: func(cmd *cobra.Command, args []string) {
			app := NewApp()
			app.Refresh()
			fmt.Println(app.ToJSON(jsonTags))
		},
	}
	jsonCmd.Flags().StringSliceVar(&jsonTags, "tag", nil, "Filter by tag (key:value)")

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Run a background server that caches session data",
		Run: func(cmd *cobra.Command, args []string) {
			runServer()
		},
	}

	parkCmd := &cobra.Command{
		Use:   "park",
		Short: "Save all live sessions to disk for restoring later",
		Run: func(cmd *cobra.Command, args []string) {
			park()
		},
	}

	unparkCmd := &cobra.Command{
		Use:   "unpark",
		Short: "Restore previously parked sessions",
		Run: func(cmd *cobra.Command, args []string) {
			unpark()
		},
	}

	rootCmd.AddCommand(newCmd, launchCmd, resumeCmd, nextCmd, jsonCmd, serverCmd, parkCmd, unparkCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI() error {
	m := newTUIModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
