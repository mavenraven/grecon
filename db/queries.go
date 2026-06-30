package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type WorkstreamInfo struct {
	WorkstreamID int64
	TmuxID       string
	DisplayName  string
	Worktree     string
	Sessions     []ClaudeSessionInfo
}

type ClaudeSessionInfo struct {
	SessionID   string
	DisplayName string
	Active      bool
}

func CreateWorkstreamDB(d *sql.DB, tmuxSession, worktree string) error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	result, err := tx.Exec(`INSERT INTO workstreams (worktree) VALUES (?)`, worktree)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("insert workstream: %w", err)
	}
	wsID, _ := result.LastInsertId()

	tmuxID := "ws-" + tmuxSession
	_, err = tx.Exec(
		`INSERT INTO tmux_sessions (workstream_id, tmux_id, display_name) VALUES (?, ?, ?)`,
		wsID, tmuxID, tmuxSession,
	)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("insert tmux_session: %w", err)
	}

	return tx.Commit()
}

func AllWorkstreams(d *sql.DB) []WorkstreamInfo {
	rows, err := d.Query(`
		SELECT w.id, t.tmux_id, t.display_name, COALESCE(w.worktree, '')
		FROM workstreams w
		JOIN tmux_sessions t ON t.workstream_id = w.id
		ORDER BY w.id
	`)
	if err != nil {
		return nil
	}

	var workstreams []WorkstreamInfo
	for rows.Next() {
		var ws WorkstreamInfo
		rows.Scan(&ws.WorkstreamID, &ws.TmuxID, &ws.DisplayName, &ws.Worktree)
		workstreams = append(workstreams, ws)
	}
	rows.Close()

	for i := range workstreams {
		ws := &workstreams[i]
		crows, err := d.Query(`
			SELECT session_id, display_name, active FROM claude_sessions
			WHERE workstream_id = ?
		`, ws.WorkstreamID)
		if err != nil {
			continue
		}
		for crows.Next() {
			var cs ClaudeSessionInfo
			var activeVal int
			crows.Scan(&cs.SessionID, &cs.DisplayName, &activeVal)
			cs.Active = activeVal == 1
			ws.Sessions = append(ws.Sessions, cs)
		}
		crows.Close()
	}

	return workstreams
}

func PruneDeadSessions(d *sql.DB, liveTmuxSessions map[string]bool) {
	rows, err := d.Query(`SELECT id, session_id, workstream_id FROM claude_sessions`)
	if err != nil {
		return
	}

	type candidate struct {
		id        int64
		sessionID string
		wsID      int64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		rows.Scan(&c.id, &c.sessionID, &c.wsID)
		candidates = append(candidates, c)
	}
	rows.Close()

	for _, c := range candidates {
		if c.sessionID == "" || !jsonlExists(c.sessionID) {
			d.Exec(`DELETE FROM claude_sessions WHERE id = ?`, c.id)
		}
	}

	liveWSIDs := make(map[int64]bool)
	tmuxRows, err := d.Query(`SELECT workstream_id, display_name FROM tmux_sessions`)
	if err == nil {
		for tmuxRows.Next() {
			var wsID int64
			var name string
			tmuxRows.Scan(&wsID, &name)
			if liveTmuxSessions[name] {
				liveWSIDs[wsID] = true
			}
		}
		tmuxRows.Close()
	}

	for _, ws := range AllWorkstreams(d) {
		if liveWSIDs[ws.WorkstreamID] {
			continue
		}
		hasSession := false
		for range ws.Sessions {
			hasSession = true
			break
		}
		if !hasSession {
			d.Exec(`DELETE FROM tmux_sessions WHERE workstream_id = ?`, ws.WorkstreamID)
			d.Exec(`DELETE FROM workstreams WHERE id = ?`, ws.WorkstreamID)
		}
	}
}

func jsonlExists(sessionID string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return true
	}
	projectsDir := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		path := filepath.Join(projectsDir, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func LoadTmuxNameDB(d *sql.DB, sessionID string) string {
	var name string
	d.QueryRow(`
		SELECT t.display_name FROM tmux_sessions t
		JOIN claude_sessions c ON c.workstream_id = t.workstream_id
		WHERE c.session_id = ?
	`, sessionID).Scan(&name)
	return name
}

func LoadClaudeNameDB(d *sql.DB, sessionID string) string {
	var name string
	d.QueryRow(`SELECT display_name FROM claude_sessions WHERE session_id = ?`,
		sessionID).Scan(&name)
	return name
}

func SaveSummaryDB(d *sql.DB, sessionID, summary string) {
	d.Exec(
		`UPDATE claude_sessions SET summary = ? WHERE session_id = ?`,
		summary, sessionID,
	)
}

func DeleteWorkstream(d *sql.DB, wsID int64) {
	d.Exec(`DELETE FROM claude_sessions WHERE workstream_id = ?`, wsID)
	d.Exec(`DELETE FROM tmux_sessions WHERE workstream_id = ?`, wsID)
	d.Exec(`DELETE FROM workstreams WHERE id = ?`, wsID)
}

func SetSessionActive(d *sql.DB, sessionID string, active bool) {
	val := 0
	if active {
		val = 1
	}
	d.Exec(`UPDATE claude_sessions SET active = ? WHERE session_id = ?`, val, sessionID)
}

func LoadSummaryDB(d *sql.DB, sessionID string) string {
	var summary string
	d.QueryRow(`SELECT summary FROM claude_sessions WHERE session_id = ?`,
		sessionID).Scan(&summary)
	return summary
}

func AddClaudeSession(d *sql.DB, workstreamID int64, sessionID, claudeName string) {
	d.Exec(
		`INSERT OR IGNORE INTO claude_sessions (workstream_id, session_id, display_name) VALUES (?, ?, ?)`,
		workstreamID, sessionID, claudeName,
	)
}

// LoadClaudeNameForTmuxSession returns the claude name for any session in the given tmux session's workstream.
func LoadClaudeNameForTmuxSession(d *sql.DB, tmuxSession string) string {
	tmuxID := "ws-" + tmuxSession
	var name string
	d.QueryRow(`
		SELECT c.display_name FROM claude_sessions c
		JOIN tmux_sessions t ON t.workstream_id = c.workstream_id
		WHERE t.tmux_id = ? AND c.display_name != ''
		LIMIT 1
	`, tmuxID).Scan(&name)
	return name
}

// UpdateSessionID updates the session_id for a claude_session that has an empty session_id in the given workstream.
func UpdateSessionID(d *sql.DB, workstreamID int64, sessionID string) {
	d.Exec(
		`UPDATE claude_sessions SET session_id = ? WHERE workstream_id = ? AND session_id = '' LIMIT 1`,
		sessionID, workstreamID,
	)
}

func AllWorkstreamDisplayNames(d *sql.DB) map[string]string {
	result := make(map[string]string)
	rows, err := d.Query(`
		SELECT t.display_name, c.display_name
		FROM tmux_sessions t
		JOIN claude_sessions c ON c.workstream_id = t.workstream_id
		WHERE c.display_name != ''
	`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var tmuxName, claudeName string
		rows.Scan(&tmuxName, &claudeName)
		if _, exists := result[tmuxName]; !exists {
			result[tmuxName] = claudeName
		}
	}
	return result
}
