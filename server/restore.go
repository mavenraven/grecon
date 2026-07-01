package server

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grecon/db"
)

func reconcileWithEnv(env *Env, d *sql.DB) {
	workstreams := db.AllWorkstreams(d)
	if len(workstreams) == 0 {
		return
	}

	claudePath := whichClaudeBinaryEnv(env)

	for _, ws := range workstreams {
		var activeSessions []db.ClaudeSessionInfo
		for _, cs := range ws.Sessions {
			if cs.Active {
				activeSessions = append(activeSessions, cs)
			}
		}

		if len(activeSessions) == 0 {
			continue
		}

		if tmuxSessionExistsEnv(env, ws.TmuxID) {
			continue
		}

		first := activeSessions[0]
		cwd := FindSessionCWDFS(env.Fs, env.Home, first.SessionID)
		if cwd == "" || !ValidateCWDFS(env.Fs, cwd) {
			fmt.Fprintf(os.Stderr, "reconcile: skip %s: bad cwd %q\n", ws.TmuxID, cwd)
			continue
		}

		err := env.Cmd.Run("tmux",
			"new-session", "-d", "-s", ws.TmuxID, "-c", cwd,
			claudePath, "--resume", first.SessionID,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reconcile: fail %s: %v\n", ws.TmuxID, err)
			continue
		}
		env.Cmd.Run("tmux", "set-option", "-t", ws.TmuxID, "@display_name", ws.DisplayName)
		fmt.Fprintf(os.Stderr, "reconcile: restored %s\n", ws.TmuxID)

		for _, cs := range activeSessions[1:] {
			csCwd := FindSessionCWDFS(env.Fs, env.Home, cs.SessionID)
			if csCwd == "" || !ValidateCWDFS(env.Fs, csCwd) {
				csCwd = cwd
			}
			env.Cmd.Run("tmux",
				"new-window", "-t", ws.TmuxID, "-c", csCwd,
				claudePath, "--resume", cs.SessionID,
			)
		}
	}
}

func whichClaudeBinary() string {
	return whichClaudeBinaryEnv(RealEnv())
}

func whichClaudeBinaryEnv(env *Env) string {
	out, err := env.Cmd.Output("which", "claude")
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

func tmuxSessionExistsEnv(env *Env, name string) bool {
	return env.Cmd.Run("tmux", "has-session", "-t", name) == nil
}
