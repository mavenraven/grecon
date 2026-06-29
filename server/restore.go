package server

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grecon/db"
)

func Reconcile(d *sql.DB) {
	workstreams := db.AllWorkstreams(d)
	if len(workstreams) == 0 {
		return
	}

	claudePath := whichClaudeBinary()

	for _, ws := range workstreams {
		if len(ws.Sessions) == 0 {
			continue
		}

		if tmuxSessionExists(ws.DisplayName) {
			continue
		}

		first := ws.Sessions[0]
		cwd := ws.Worktree
		if cwd == "" {
			cwd = FindSessionCWD(first.SessionID)
		}
		if cwd == "" || !ValidateCWD(cwd) {
			fmt.Fprintf(os.Stderr, "reconcile: skip %s: bad cwd %q\n", ws.DisplayName, cwd)
			continue
		}

		cmd := exec.Command("tmux",
			"new-session", "-d", "-s", ws.DisplayName, "-c", cwd,
			claudePath, "--resume", first.SessionID,
		)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "reconcile: fail %s: %v\n", ws.DisplayName, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "reconcile: restored %s\n", ws.DisplayName)

		for _, cs := range ws.Sessions[1:] {
			csCwd := FindSessionCWD(cs.SessionID)
			if csCwd == "" || !ValidateCWD(csCwd) {
				csCwd = cwd
			}
			exec.Command("tmux",
				"new-window", "-t", ws.DisplayName, "-c", csCwd,
				claudePath, "--resume", cs.SessionID,
			).Run()
		}
	}
}

func whichClaudeBinary() string {
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

func tmuxSessionExists(name string) bool {
	return exec.Command("tmux", "has-session", "-t", name).Run() == nil
}
