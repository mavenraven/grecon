package server

import (
	"testing"
	"time"

	"grecon/db"
)

func TestDiscoverTmuxSessions_OnlyShowsDBSessions(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()
	db.SetGlobal(d)
	defer db.SetGlobal(nil)

	tmuxID, _ := db.CreateWorkstreamDB(d, "known-project", env.Clock)

	allSessions := []*Session{
		{SessionID: "sess-known", TmuxSession: tmuxID},
		{SessionID: "sess-unknown", TmuxSession: "random-uuid-not-in-db"},
	}

	// Mock DiscoverSessions by testing the filter logic directly
	knownTmux := make(map[string]bool)
	for _, ws := range db.AllWorkstreams(d) {
		knownTmux[ws.TmuxID] = true
	}

	var filtered []*Session
	for _, s := range allSessions {
		if s.TmuxSession != "" && knownTmux[s.TmuxSession] {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered session, got %d", len(filtered))
	}
	if filtered[0].SessionID != "sess-known" {
		t.Fatalf("expected sess-known, got %s", filtered[0].SessionID)
	}
}

func TestDiscoverTmuxSessions_ExcludesSoftDeletedWorkstreams(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()
	db.SetGlobal(d)
	defer db.SetGlobal(nil)

	tmuxID, _ := db.CreateWorkstreamDB(d, "deleted-project", env.Clock)
	db.DeleteWorkstream(d, 1, env.Clock)

	knownTmux := make(map[string]bool)
	for _, ws := range db.AllWorkstreams(d) {
		knownTmux[ws.TmuxID] = true
	}

	allSessions := []*Session{
		{SessionID: "sess-deleted", TmuxSession: tmuxID},
	}

	var filtered []*Session
	for _, s := range allSessions {
		if s.TmuxSession != "" && knownTmux[s.TmuxSession] {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) != 0 {
		t.Fatalf("soft-deleted workstream sessions should be filtered out, got %d", len(filtered))
	}
}
