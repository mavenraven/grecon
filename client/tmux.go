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

	baseName := sanitizeSessionName(name)
	sessionName := uniqueSessionName(baseName)

	args := []string{"new-session", "-d", "-s", sessionName, "-c", cwd}

	if len(tags) > 0 {
		tagsVal := strings.Join(tags, ",")
		args = append(args, "-e", fmt.Sprintf("RECON_TAGS=%s", tagsVal))
	}

	if claudeName != "" {
		args = append(args, "-e", fmt.Sprintf("RECON_CLAUDE_NAME=%s", claudeName))
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

	cmd := exec.Command("tmux", args...)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create tmux session: %w", err)
	}

	if worktree {
		server.SendCommand(server.Command{
			Type:        "fix-default-path",
			TmuxSession: sessionName,
		})
	}

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

