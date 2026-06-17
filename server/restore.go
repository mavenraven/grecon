package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type SavedSession struct {
	SessionID   string `json:"session_id"`
	TmuxSession string `json:"tmux_session"`
}

func stateFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".recon", "session-state.json")
}

func WriteSessionState(sessions []*Session) {
	path := stateFilePath()
	if path == "" {
		return
	}

	var saved []SavedSession
	for _, s := range sessions {
		if s.TmuxSession == "" || s.JSONLPath == "" {
			continue
		}
		sid := strings.TrimSuffix(filepath.Base(s.JSONLPath), ".jsonl")
		saved = append(saved, SavedSession{
			SessionID:   sid,
			TmuxSession: s.TmuxSession,
		})
	}

	if saved == nil {
		saved = []SavedSession{}
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return
	}
	next := path + ".next"
	if err := os.WriteFile(next, data, 0o644); err != nil {
		return
	}
	os.Rename(next, path)
}

func RestoreSessions() {
	path := stateFilePath()
	if path == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: no saved state\n")
		return
	}

	var saved []SavedSession
	if json.Unmarshal(data, &saved) != nil {
		fmt.Fprintf(os.Stderr, "restore: corrupt state file\n")
		return
	}

	if len(saved) == 0 {
		fmt.Fprintf(os.Stderr, "restore: no sessions to restore\n")
		return
	}

	fmt.Fprintf(os.Stderr, "restore: restoring %d session(s)\n", len(saved))

	claudePath := whichClaudeBinary()

	for _, s := range saved {
		cwd := FindSessionCWD(s.SessionID)
		if cwd == "" || !ValidateCWD(cwd) {
			fmt.Fprintf(os.Stderr, "  skip %s (%s): bad cwd\n", s.TmuxSession, s.SessionID[:min(8, len(s.SessionID))])
			continue
		}

		sessionName := uniqueTmuxName(s.TmuxSession)
		envVar := fmt.Sprintf("RECON_RESUMED_FROM=%s", s.SessionID)

		cmd := exec.Command("tmux",
			"new-session", "-d", "-s", sessionName, "-c", cwd,
			"-e", envVar,
			claudePath, "--resume", s.SessionID,
		)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  fail %s: %v\n", s.TmuxSession, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  restored %s (%s)\n", sessionName, s.SessionID[:min(8, len(s.SessionID))])
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

func uniqueTmuxName(base string) string {
	if exec.Command("tmux", "has-session", "-t", base).Run() != nil {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if exec.Command("tmux", "has-session", "-t", candidate).Run() != nil {
			return candidate
		}
	}
}
