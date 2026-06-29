package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"grecon/db"
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

	baseName := sanitizeSessionName(name)
	sessionName := uniqueSessionName(baseName)

	args := []string{"new-session", "-d", "-s", sessionName, "-c", cwd}

	if len(tags) > 0 {
		tagsVal := strings.Join(tags, ",")
		args = append(args, "-e", fmt.Sprintf("RECON_TAGS=%s", tagsVal))
	}

	if command != nil {
		parts := strings.Fields(*command)
		args = append(args, parts...)
	} else {
		claudePath := whichClaude()
		args = append(args, claudePath)
		if claudeName != "" {
			args = append(args, "-n", claudeName)
		}
		if worktree {
			args = append(args, "--worktree")
		}
	}

	tmuxCmd := exec.Command("tmux", args...)
	if err := tmuxCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create tmux session: %w", err)
	}

	var worktreePath string
	if worktree {
		worktreePath = cwd
		server.SendCommand(server.Command{
			Type:        "fix-default-path",
			TmuxSession: sessionName,
		})
	}

	db.CreateWorkstream(sessionName, claudeName, worktreePath)

	return sessionName, nil
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

func uniqueSessionName(baseName string) string {
	if !tmuxSessionExists(baseName) {
		return baseName
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", baseName, n)
		if !tmuxSessionExists(candidate) {
			return candidate
		}
	}
}

func tmuxSessionExists(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", name).Run()
	return err == nil
}

func whichClaude() string {
	out, err := exec.Command("which", "claude").Output()
	if err != nil {
		return "claude"
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "claude"
	}
	return path
}

func ReactivateSession(sessionID, tmuxSession string) {
	d := db.OpenClient()
	if d == nil {
		return
	}
	defer d.Close()
	db.SetSessionActive(d, sessionID, true)

	if !tmuxSessionExists(tmuxSession) {
		cwd := server.FindSessionCWD(sessionID)
		if cwd == "" || !server.ValidateCWD(cwd) {
			return
		}
		claudePath := whichClaude()
		exec.Command("tmux",
			"new-session", "-d", "-s", tmuxSession, "-c", cwd,
			claudePath, "--resume", sessionID,
		).Run()
	} else {
		cwd := server.FindSessionCWD(sessionID)
		if cwd == "" {
			cwd = "."
		}
		claudePath := whichClaude()
		exec.Command("tmux",
			"new-window", "-t", tmuxSession, "-c", cwd,
			claudePath, "--resume", sessionID,
		).Run()
	}
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

