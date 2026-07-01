package db

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type WorkstreamInfo struct {
	WorkstreamID int64
	TmuxID       string
	DisplayName  string
	CreatedAt    string
	Sessions     []ClaudeSessionInfo
}

type ClaudeSessionInfo struct {
	SessionID   string
	DisplayName string
	Active      bool
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func CreateWorkstreamDB(d *sql.DB, displayName string) (string, error) {
	tx, err := d.Begin()
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}

	ts := now()
	result, err := tx.Exec(`INSERT INTO workstreams (created_at) VALUES (?)`, ts)
	if err != nil {
		tx.Rollback()
		return "", fmt.Errorf("insert workstream: %w", err)
	}
	wsID, _ := result.LastInsertId()

	tmuxID := uuid.New().String()
	_, err = tx.Exec(
		`INSERT INTO tmux_sessions (workstream_id, tmux_id, display_name, created_at) VALUES (?, ?, ?, ?)`,
		wsID, tmuxID, displayName, ts,
	)
	if err != nil {
		tx.Rollback()
		return "", fmt.Errorf("insert tmux_session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return tmuxID, nil
}

func AllWorkstreams(d *sql.DB) []WorkstreamInfo {
	rows, err := d.Query(`
		SELECT w.id, t.tmux_id, t.display_name, COALESCE(w.created_at, '')
		FROM workstreams w
		JOIN tmux_sessions t ON t.workstream_id = w.id
		WHERE w.deleted_at = '' AND t.deleted_at = ''
		ORDER BY w.id
	`)
	if err != nil {
		return nil
	}

	var workstreams []WorkstreamInfo
	for rows.Next() {
		var ws WorkstreamInfo
		rows.Scan(&ws.WorkstreamID, &ws.TmuxID, &ws.DisplayName, &ws.CreatedAt)
		workstreams = append(workstreams, ws)
	}
	rows.Close()

	for i := range workstreams {
		ws := &workstreams[i]
		crows, err := d.Query(`
			SELECT session_id, display_name, active FROM claude_sessions
			WHERE workstream_id = ? AND deleted_at = ''
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
	ts := now()
	cutoff := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)

	rows, err := d.Query(`SELECT id, session_id, workstream_id, created_at FROM claude_sessions WHERE deleted_at = ''`)
	if err != nil {
		return
	}

	type candidate struct {
		id        int64
		sessionID string
		wsID      int64
		createdAt string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		rows.Scan(&c.id, &c.sessionID, &c.wsID, &c.createdAt)
		candidates = append(candidates, c)
	}
	rows.Close()

	for _, c := range candidates {
		if c.createdAt > cutoff {
			continue
		}
		if c.sessionID == "" || !jsonlExists(c.sessionID) {
			d.Exec(`UPDATE claude_sessions SET deleted_at = ? WHERE id = ?`, ts, c.id)
		} else if worktreeGone(c.sessionID) {
			d.Exec(`UPDATE claude_sessions SET deleted_at = ? WHERE id = ?`, ts, c.id)
		}
	}

	liveWSIDs := make(map[int64]bool)
	tmuxRows, err := d.Query(`SELECT workstream_id, tmux_id FROM tmux_sessions WHERE deleted_at = ''`)
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
		if ws.CreatedAt > cutoff {
			continue
		}
		if len(ws.Sessions) == 0 {
			d.Exec(`UPDATE tmux_sessions SET deleted_at = ? WHERE workstream_id = ? AND deleted_at = ''`, ts, ws.WorkstreamID)
			d.Exec(`UPDATE workstreams SET deleted_at = ? WHERE id = ? AND deleted_at = ''`, ts, ws.WorkstreamID)
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

func worktreeGone(sessionID string) bool {
	path := findJSONLPath(sessionID)
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hadWorktree := false
	lastWorktreePath := ""
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"worktree-state"`) {
			continue
		}
		var entry struct {
			Type            string `json:"type"`
			WorktreeSession *struct {
				WorktreePath string `json:"worktreePath"`
			} `json:"worktreeSession"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Type == "worktree-state" {
			if entry.WorktreeSession == nil {
				lastWorktreePath = ""
			} else {
				hadWorktree = true
				lastWorktreePath = entry.WorktreeSession.WorktreePath
			}
		}
	}

	if !hadWorktree {
		return false
	}
	if lastWorktreePath == "" {
		return true
	}
	_, err = os.Stat(lastWorktreePath)
	return err != nil
}

func findJSONLPath(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	projectsDir := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		path := filepath.Join(projectsDir, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func LoadTmuxNameDB(d *sql.DB, sessionID string) string {
	var name string
	d.QueryRow(`
		SELECT t.tmux_id FROM tmux_sessions t
		JOIN claude_sessions c ON c.workstream_id = t.workstream_id
		WHERE c.session_id = ? AND c.deleted_at = '' AND t.deleted_at = ''
	`, sessionID).Scan(&name)
	return name
}

func LoadClaudeNameDB(d *sql.DB, sessionID string) string {
	var name string
	d.QueryRow(`SELECT display_name FROM claude_sessions WHERE session_id = ? AND deleted_at = ''`,
		sessionID).Scan(&name)
	return name
}

func SaveSummaryDB(d *sql.DB, sessionID, summary string) {
	d.Exec(
		`UPDATE claude_sessions SET summary = ? WHERE session_id = ? AND deleted_at = ''`,
		summary, sessionID,
	)
}

func DeleteWorkstream(d *sql.DB, wsID int64) {
	ts := now()
	d.Exec(`UPDATE claude_sessions SET deleted_at = ? WHERE workstream_id = ? AND deleted_at = ''`, ts, wsID)
	d.Exec(`UPDATE tmux_sessions SET deleted_at = ? WHERE workstream_id = ? AND deleted_at = ''`, ts, wsID)
	d.Exec(`UPDATE workstreams SET deleted_at = ? WHERE id = ? AND deleted_at = ''`, ts, wsID)
}

func SoftDeleteSession(d *sql.DB, sessionID string) {
	d.Exec(`UPDATE claude_sessions SET deleted_at = ? WHERE session_id = ? AND deleted_at = ''`,
		now(), sessionID)
}

func SoftDeletedSessionIDs(d *sql.DB) []string {
	rows, err := d.Query(`SELECT session_id FROM claude_sessions WHERE deleted_at != '' AND session_id != ''`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func SetClaudeName(d *sql.DB, sessionID, name string) {
	d.Exec(`UPDATE claude_sessions SET display_name = ? WHERE session_id = ? AND deleted_at = ''`,
		name, sessionID)
}

func SetSessionActive(d *sql.DB, sessionID string, active bool) {
	val := 0
	if active {
		val = 1
	}
	d.Exec(`UPDATE claude_sessions SET active = ? WHERE session_id = ? AND deleted_at = ''`, val, sessionID)
}

func LoadSummaryDB(d *sql.DB, sessionID string) string {
	var summary string
	d.QueryRow(`SELECT summary FROM claude_sessions WHERE session_id = ? AND deleted_at = ''`,
		sessionID).Scan(&summary)
	return summary
}

func AddClaudeSession(d *sql.DB, workstreamID int64, sessionID, claudeName string) {
	d.Exec(
		`INSERT OR IGNORE INTO claude_sessions (workstream_id, session_id, display_name, created_at) VALUES (?, ?, ?, ?)`,
		workstreamID, sessionID, claudeName, now(),
	)
}


func UpdateSessionID(d *sql.DB, workstreamID int64, sessionID string) {
	d.Exec(
		`UPDATE claude_sessions SET session_id = ? WHERE workstream_id = ? AND session_id = '' AND deleted_at = '' LIMIT 1`,
		sessionID, workstreamID,
	)
}

func AllWorkstreamDisplayNames(d *sql.DB) map[string]string {
	result := make(map[string]string)
	rows, err := d.Query(`
		SELECT t.tmux_id, c.display_name
		FROM tmux_sessions t
		JOIN claude_sessions c ON c.workstream_id = t.workstream_id
		WHERE c.display_name != '' AND c.deleted_at = '' AND t.deleted_at = ''
	`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var tmuxID, claudeName string
		rows.Scan(&tmuxID, &claudeName)
		if _, exists := result[tmuxID]; !exists {
			result[tmuxID] = claudeName
		}
	}
	return result
}
