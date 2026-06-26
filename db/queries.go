package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
)

type LiveSession struct {
	SessionID   string
	TmuxSession string
	ClaudeName  string
	JSONLPath   string
	Summary     string
}

type WorkstreamInfo struct {
	WorkstreamID int64
	TmuxID       string
	DisplayName  string
	Active       bool
	Sessions     []ClaudeSessionInfo
}

type ClaudeSessionInfo struct {
	SessionID   string
	DisplayName string
}

func SyncLiveSessions(d *sql.DB, live []LiveSession) {
	tx, err := d.Begin()
	if err != nil {
		return
	}

	liveSessionIDs := make(map[string]bool)
	for _, s := range live {
		liveSessionIDs[s.SessionID] = true
		syncOne(tx, s)
	}

	rows, err := tx.Query(`
		SELECT w.id, c.session_id FROM workstreams w
		JOIN claude_sessions c ON c.workstream_id = w.id
	`)
	if err == nil {
		type wsSession struct {
			wsID      int64
			sessionID string
		}
		var all []wsSession
		for rows.Next() {
			var ws wsSession
			rows.Scan(&ws.wsID, &ws.sessionID)
			all = append(all, ws)
		}
		rows.Close()

		activeWSIDs := make(map[int64]bool)
		for _, ws := range all {
			if liveSessionIDs[ws.sessionID] {
				activeWSIDs[ws.wsID] = true
			}
		}

		tx.Exec(`UPDATE workstreams SET active = 0`)
		for wsID := range activeWSIDs {
			tx.Exec(`UPDATE workstreams SET active = 1 WHERE id = ?`, wsID)
		}
	}

	tx.Commit()
}

func syncOne(tx *sql.Tx, s LiveSession) {
	var wsID int64
	err := tx.QueryRow(
		`SELECT workstream_id FROM claude_sessions WHERE session_id = ?`,
		s.SessionID,
	).Scan(&wsID)

	if err == sql.ErrNoRows {
		var existingWSID int64
		tmuxID := "ws-" + s.TmuxSession
		err := tx.QueryRow(
			`SELECT workstream_id FROM tmux_sessions WHERE tmux_id = ?`,
			tmuxID,
		).Scan(&existingWSID)

		if err == sql.ErrNoRows {
			result, err := tx.Exec(`INSERT INTO workstreams (active) VALUES (1)`)
			if err != nil {
				return
			}
			wsID, _ = result.LastInsertId()
			tx.Exec(
				`INSERT INTO tmux_sessions (workstream_id, tmux_id, display_name) VALUES (?, ?, ?)`,
				wsID, tmuxID, s.TmuxSession,
			)
		} else if err == nil {
			wsID = existingWSID
		} else {
			return
		}

		tx.Exec(
			`INSERT OR IGNORE INTO claude_sessions (workstream_id, session_id, display_name) VALUES (?, ?, ?)`,
			wsID, s.SessionID, s.ClaudeName,
		)
	}

	if s.ClaudeName != "" {
		tx.Exec(
			`UPDATE claude_sessions SET display_name = ? WHERE session_id = ? AND display_name = ''`,
			s.ClaudeName, s.SessionID,
		)
	}
	if s.Summary != "" {
		tx.Exec(
			`UPDATE claude_sessions SET summary = ? WHERE session_id = ?`,
			s.Summary, s.SessionID,
		)
	}
}

func PruneDeadSessions(d *sql.DB) {
	rows, err := d.Query(`
		SELECT c.id, c.session_id, w.id as ws_id
		FROM claude_sessions c
		JOIN workstreams w ON w.id = c.workstream_id
		WHERE w.active = 0
	`)
	if err != nil {
		return
	}

	type deadCandidate struct {
		claudeID  int64
		sessionID string
		wsID      int64
	}
	var candidates []deadCandidate
	for rows.Next() {
		var c deadCandidate
		rows.Scan(&c.claudeID, &c.sessionID, &c.wsID)
		candidates = append(candidates, c)
	}
	rows.Close()

	for _, c := range candidates {
		if !jsonlExists(c.sessionID) {
			d.Exec(`DELETE FROM claude_sessions WHERE id = ?`, c.claudeID)
		}
	}

	d.Exec(`
		DELETE FROM workstreams WHERE id IN (
			SELECT w.id FROM workstreams w
			LEFT JOIN claude_sessions c ON c.workstream_id = w.id
			WHERE c.id IS NULL
		)
	`)
	d.Exec(`
		DELETE FROM tmux_sessions WHERE workstream_id NOT IN (
			SELECT id FROM workstreams
		)
	`)
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

func ActiveWorkstreams(d *sql.DB) []WorkstreamInfo {
	return queryWorkstreams(d, true)
}

func InactiveWorkstreams(d *sql.DB) []WorkstreamInfo {
	return queryWorkstreams(d, false)
}

func queryWorkstreams(d *sql.DB, active bool) []WorkstreamInfo {
	activeInt := 0
	if active {
		activeInt = 1
	}

	rows, err := d.Query(`
		SELECT w.id, t.tmux_id, t.display_name, w.active
		FROM workstreams w
		JOIN tmux_sessions t ON t.workstream_id = w.id
		WHERE w.active = ?
		ORDER BY w.id
	`, activeInt)
	if err != nil {
		return nil
	}

	var workstreams []WorkstreamInfo
	for rows.Next() {
		var ws WorkstreamInfo
		var activeVal int
		rows.Scan(&ws.WorkstreamID, &ws.TmuxID, &ws.DisplayName, &activeVal)
		ws.Active = activeVal == 1
		workstreams = append(workstreams, ws)
	}
	rows.Close()

	for i := range workstreams {
		ws := &workstreams[i]
		crows, err := d.Query(`
			SELECT session_id, display_name FROM claude_sessions
			WHERE workstream_id = ?
		`, ws.WorkstreamID)
		if err != nil {
			continue
		}
		for crows.Next() {
			var cs ClaudeSessionInfo
			crows.Scan(&cs.SessionID, &cs.DisplayName)
			ws.Sessions = append(ws.Sessions, cs)
		}
		crows.Close()
	}

	return workstreams
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

func SaveTmuxNameDB(d *sql.DB, sessionID, tmuxName string) {
	if strings.HasPrefix(sessionID, "tmux-") {
		return
	}
	d.Exec(`
		UPDATE tmux_sessions SET display_name = ?
		WHERE workstream_id = (
			SELECT workstream_id FROM claude_sessions WHERE session_id = ?
		)
	`, tmuxName, sessionID)
}

func SaveClaudeNameDB(d *sql.DB, sessionID, claudeName string) {
	if strings.HasPrefix(sessionID, "tmux-") {
		return
	}
	d.Exec(`
		UPDATE claude_sessions SET display_name = ?
		WHERE session_id = ? AND display_name = ''
	`, claudeName, sessionID)
}

func SaveSummaryDB(d *sql.DB, sessionID, summary string) {
	d.Exec(
		`UPDATE claude_sessions SET summary = ? WHERE session_id = ?`,
		summary, sessionID,
	)
}
