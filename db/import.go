package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type importedResumeEntry struct {
	SessionID  string `json:"session_id"`
	CWD        string `json:"cwd"`
	Branch     string `json:"branch,omitempty"`
	Model      string `json:"model,omitempty"`
	Tokens     uint64 `json:"tokens"`
	LastActive string `json:"last_active"`
	ProjectDir string `json:"project_dir"`
}

type importedSavedSession struct {
	SessionID   string `json:"session_id"`
	TmuxSession string `json:"tmux_session"`
	Summary     string `json:"summary,omitempty"`
	Playwright  bool   `json:"playwright,omitempty"`
}

func ImportExistingState(d *sql.DB) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	reconDir := filepath.Join(home, ".recon")

	var count int
	d.QueryRow(`SELECT COUNT(*) FROM workstreams`).Scan(&count)
	if count > 0 {
		fmt.Println("db: import skipped, database already has data")
		return nil
	}

	tmuxNames := readNameDir(filepath.Join(reconDir, "tmux-names"))
	claudeNames := readNameDir(filepath.Join(reconDir, "claude-names"))

	activeState := readSavedSessions(filepath.Join(reconDir, "session-state.json"))
	resumeCache := readResumeCache(filepath.Join(reconDir, "resume-cache.json"))

	type sessionInfo struct {
		sessionID   string
		tmuxName    string
		claudeName  string
		active      bool
		summary     string
		playwright  bool
	}

	seen := make(map[string]*sessionInfo)

	for _, s := range activeState {
		info := &sessionInfo{
			sessionID:  s.SessionID,
			tmuxName:   s.TmuxSession,
			active:     true,
			summary:    s.Summary,
			playwright: s.Playwright,
		}
		if cn, ok := claudeNames[s.SessionID]; ok {
			info.claudeName = cn
		}
		seen[s.SessionID] = info
	}

	for _, r := range resumeCache {
		if _, exists := seen[r.SessionID]; exists {
			continue
		}
		tmuxName := r.SessionID[:min(8, len(r.SessionID))]
		if tn, ok := tmuxNames[r.SessionID]; ok {
			tmuxName = tn
		}
		info := &sessionInfo{
			sessionID:  r.SessionID,
			tmuxName:   tmuxName,
			claudeName: "",
			active:     false,
		}
		if cn, ok := claudeNames[r.SessionID]; ok {
			info.claudeName = cn
		}
		seen[r.SessionID] = info
	}

	for sid, tn := range tmuxNames {
		if _, exists := seen[sid]; exists {
			continue
		}
		info := &sessionInfo{
			sessionID: sid,
			tmuxName:  tn,
			active:    false,
		}
		if cn, ok := claudeNames[sid]; ok {
			info.claudeName = cn
		}
		seen[sid] = info
	}

	groups := make(map[string][]*sessionInfo)
	for _, info := range seen {
		groups[info.tmuxName] = append(groups[info.tmuxName], info)
	}

	tx, err := d.Begin()
	if err != nil {
		return err
	}

	imported := 0
	for tmuxName, sessions := range groups {
		active := false
		for _, s := range sessions {
			if s.active {
				active = true
				break
			}
		}

		result, err := tx.Exec(
			`INSERT INTO workstreams (active) VALUES (?)`,
			boolToInt(active),
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("insert workstream for %s: %w", tmuxName, err)
		}
		wsID, _ := result.LastInsertId()

		tmuxID := sanitizeTmuxID(tmuxName)
		_, err = tx.Exec(
			`INSERT INTO tmux_sessions (workstream_id, tmux_id, display_name) VALUES (?, ?, ?)`,
			wsID, tmuxID, tmuxName,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("insert tmux_session for %s: %w", tmuxName, err)
		}

		for _, info := range sessions {
			_, err = tx.Exec(
				`INSERT INTO claude_sessions (workstream_id, session_id, display_name) VALUES (?, ?, ?)`,
				wsID, info.sessionID, info.claudeName,
			)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("insert claude_session for %s: %w", info.sessionID, err)
			}
			imported++
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Printf("db: imported %d sessions into %d workstreams\n", imported, len(groups))
	return nil
}

func readNameDir(dir string) map[string]string {
	m := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return m
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(data))
		if name != "" {
			m[e.Name()] = name
		}
	}
	return m
}

func readSavedSessions(path string) []importedSavedSession {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var sessions []importedSavedSession
	json.Unmarshal(data, &sessions)
	return sessions
}

func readResumeCache(path string) []importedResumeEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []importedResumeEntry
	json.Unmarshal(data, &entries)
	return entries
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sanitizeTmuxID(name string) string {
	return "ws-" + name
}
