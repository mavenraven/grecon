package client

import (
	"os"
	"os/exec"
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

func KillPane(target string) bool {
	return exec.Command("tmux", "kill-pane", "-t", target).Run() == nil
}
