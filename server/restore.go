package server

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grecon/db"
)

func RestoreSessions() {
	d := db.Get()
	if d == nil {
		fmt.Fprintf(os.Stderr, "restore: no database\n")
		return
	}

	workstreams := db.ActiveWorkstreams(d)
	if len(workstreams) == 0 {
		fmt.Fprintf(os.Stderr, "restore: no active workstreams to restore\n")
		return
	}

	fmt.Fprintf(os.Stderr, "restore: restoring %d workstream(s)\n", len(workstreams))

	globalSummary.mu.Lock()
	for _, ws := range workstreams {
		for _, cs := range ws.Sessions {
			summary := db.LoadSummaryDB(d, cs.SessionID)
			if summary != "" {
				globalSummary.summaries[cs.SessionID] = summary
			}
		}
	}
	globalSummary.mu.Unlock()

	claudePath := whichClaudeBinary()

	for _, ws := range workstreams {
		if tmuxSessionExists(ws.DisplayName) {
			fmt.Fprintf(os.Stderr, "  skip %s: already exists\n", ws.DisplayName)
			continue
		}

		if len(ws.Sessions) == 0 {
			continue
		}

		first := ws.Sessions[0]
		cwd := FindSessionCWD(first.SessionID)
		if cwd == "" || !ValidateCWD(cwd) {
			fmt.Fprintf(os.Stderr, "  skip %s (%s): bad cwd\n", ws.DisplayName, first.SessionID[:min(8, len(first.SessionID))])
			continue
		}

		envVar := fmt.Sprintf("RECON_RESUMED_FROM=%s", first.SessionID)

		cmd := exec.Command("tmux",
			"new-session", "-d", "-s", ws.DisplayName, "-c", cwd,
			"-e", envVar,
			claudePath, "--resume", first.SessionID,
		)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  fail %s: %v\n", ws.DisplayName, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  restored %s (%s)\n", ws.DisplayName, first.SessionID[:min(8, len(first.SessionID))])

		for _, cs := range ws.Sessions[1:] {
			csCwd := FindSessionCWD(cs.SessionID)
			if csCwd == "" || !ValidateCWD(csCwd) {
				csCwd = cwd
			}
			csEnv := fmt.Sprintf("RECON_RESUMED_FROM=%s", cs.SessionID)
			exec.Command("tmux",
				"new-window", "-t", ws.DisplayName, "-c", csCwd,
				"-e", csEnv,
				claudePath, "--resume", cs.SessionID,
			).Run()
			fmt.Fprintf(os.Stderr, "  restored %s window (%s)\n", ws.DisplayName, cs.SessionID[:min(8, len(cs.SessionID))])
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
