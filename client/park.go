package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type parkFile struct {
	ParkedAt string          `json:"parked_at"`
	Sessions []parkedSession `json:"sessions"`
}

type parkedSession struct {
	SessionID   string `json:"session_id"`
	TmuxSession string `json:"tmux_session"`
	CWD         string `json:"cwd"`
}

func parkFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "recon", "parked.json")
}

func Park() {
	app := NewApp()
	if err := app.Refresh(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	var parked []parkedSession
	for _, s := range app.Sessions {
		if s.TmuxSession == "" {
			continue
		}
		resumeID := s.SessionID
		if s.JSONLPath != "" {
			resumeID = strings.TrimSuffix(filepath.Base(s.JSONLPath), ".jsonl")
		}
		parked = append(parked, parkedSession{
			SessionID:   resumeID,
			TmuxSession: s.TmuxSession,
			CWD:         s.CWD,
		})
	}

	if len(parked) == 0 {
		fmt.Fprintln(os.Stderr, "No live sessions to park.")
		return
	}

	pf := parkFile{
		ParkedAt: time.Now().UTC().Format(time.RFC3339),
		Sessions: parked,
	}

	path := parkFilePath()
	if path == "" {
		fmt.Fprintln(os.Stderr, "Could not determine home directory.")
		return
	}

	os.MkdirAll(filepath.Dir(path), 0o755)

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to serialize: %v\n", err)
		return
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write park file: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "Parked %d session(s) to %s\n", len(parked), path)
	for _, s := range parked {
		id := s.SessionID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Fprintf(os.Stderr, "  %s (%s)\n", s.TmuxSession, id)
	}
}

func Unpark() {
	path := parkFilePath()
	if path == "" {
		fmt.Fprintln(os.Stderr, "Could not determine home directory.")
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Nothing parked.")
		return
	}

	var pf parkFile
	if err := json.Unmarshal(data, &pf); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read park file: %v\n", err)
		return
	}

	if len(pf.Sessions) == 0 {
		fmt.Fprintln(os.Stderr, "Park file is empty.")
		os.Remove(path)
		return
	}

	fmt.Fprintf(os.Stderr, "Restoring %d session(s) from %s...\n", len(pf.Sessions), pf.ParkedAt)

	for _, s := range pf.Sessions {
		name := s.TmuxSession
		sess, err := ResumeSession(s.SessionID, &name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Failed to restore %s: %v\n", s.TmuxSession, err)
		} else {
			id := s.SessionID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Fprintf(os.Stderr, "  Restored %s (%s)\n", sess, id)
		}
	}

	fmt.Fprintf(os.Stderr, "Done. Park file kept at %s\n", path)
}
