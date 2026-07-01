package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"grecon/server"
)

func SwitchToPane(target string) {
	if os.Getenv("TMUX") != "" {
		exec.Command("tmux", "switch-client", "-t", target).Run()
	} else {
		cmd := exec.Command("tmux", "attach-session", "-t", target)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}

func CreateSession(name, cwd, claudeName string, command *string, tags []string, worktree bool) (string, error) {
	if !server.ValidateCWD(cwd) {
		return "", fmt.Errorf("invalid working directory: %s", cwd)
	}

	var customCmd string
	if command != nil {
		customCmd = *command
	}

	var tagsStr string
	if len(tags) > 0 {
		tagsStr = strings.Join(tags, ",")
	}

	resp, err := server.SendCommand(server.Command{
		Type:       "create-session",
		Name:       sanitizeSessionName(name),
		CWD:        cwd,
		ClaudeName: claudeName,
		Worktree:   worktree,
		CustomCmd:  customCmd,
		Tags:       tagsStr,
	})
	if err != nil {
		return "", fmt.Errorf("server error: %w", err)
	}
	if !resp.OK {
		return "", fmt.Errorf("create failed: %s", resp.Error)
	}
	return resp.TmuxSession, nil
}

func DeleteSession(sessionID string) {
	server.SendCommand(server.Command{
		Type:      "delete-session",
		SessionID: sessionID,
	})
}

func ReactivateSession(sessionID, tmuxSession string) error {
	resp, err := server.SendCommand(server.Command{
		Type:        "reactivate-session",
		SessionID:   sessionID,
		TmuxSession: tmuxSession,
	})
	if err != nil {
		return fmt.Errorf("reactivate: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("reactivate: %s", resp.Error)
	}
	return nil
}

func DefaultNewSessionInfo() (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	name := filepath.Base(cwd)
	if name == "" || name == "." {
		name = "claude"
	}
	return name, cwd
}

func KillSession(name string) bool {
	return exec.Command("tmux", "kill-session", "-t", name).Run() == nil
}

func sanitizeSessionName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.TrimLeft(b.String(), "-")
	if result == "" {
		return "claude"
	}
	return result
}
