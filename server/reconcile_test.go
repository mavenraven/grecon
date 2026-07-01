package server

import (
	"testing"
	"time"

	"grecon/db"
)

func TestReconcile_AddsNewLiveSessions(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	tmuxID, _ := db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)

	live := []*Session{
		{SessionID: "sess-A", TmuxSession: tmuxID},
		{SessionID: "sess-B", TmuxSession: tmuxID},
	}

	result := reconcileDBWithLive(env, d, live)

	ws := db.AllWorkstreams(d)
	if len(ws[0].Sessions) != 2 {
		t.Fatalf("expected 2 sessions in DB, got %d", len(ws[0].Sessions))
	}

	found := false
	for _, s := range ws[0].Sessions {
		if s.SessionID == "sess-B" {
			found = true
		}
	}
	if !found {
		t.Fatal("sess-B should have been added to DB")
	}
	_ = result
}

func TestReconcile_DoesNotDuplicateExisting(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	tmuxID, _ := db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)

	live := []*Session{
		{SessionID: "sess-A", TmuxSession: tmuxID},
	}

	reconcileDBWithLive(env, d, live)
	reconcileDBWithLive(env, d, live)

	ws := db.AllWorkstreams(d)
	if len(ws[0].Sessions) != 1 {
		t.Fatalf("should still have 1 session, got %d", len(ws[0].Sessions))
	}
}

func TestReconcile_MarksInactiveWhenNotLive(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.SetSessionActive(d, "sess-A", true)

	live := []*Session{}

	reconcileDBWithLive(env, d, live)

	ws := db.AllWorkstreams(d)
	for _, cs := range ws[0].Sessions {
		if cs.SessionID == "sess-A" && cs.Active {
			t.Fatal("sess-A should be marked inactive")
		}
	}
}

func TestReconcile_ReactivatesWhenLiveAgain(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	tmuxID, _ := db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.SetSessionActive(d, "sess-A", false)

	live := []*Session{
		{SessionID: "sess-A", TmuxSession: tmuxID},
	}

	reconcileDBWithLive(env, d, live)

	ws := db.AllWorkstreams(d)
	for _, cs := range ws[0].Sessions {
		if cs.SessionID == "sess-A" && !cs.Active {
			t.Fatal("sess-A should be marked active again")
		}
	}
}

func TestReconcile_AppendsInactiveSessionsToResult(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.SetSessionActive(d, "sess-A", false)

	writeTestJSONL(env.Fs, env.Home, "-test-proj", "sess-A", `{"type":"user"}`)

	live := []*Session{}

	result := reconcileDBWithLive(env, d, live)

	found := false
	for _, s := range result {
		if s.SessionID == "sess-A" {
			found = true
			if s.Status != StatusInactive {
				t.Fatalf("expected StatusInactive, got %d", s.Status)
			}
		}
	}
	if !found {
		t.Fatal("inactive sess-A should appear in result")
	}
}

func TestReconcile_DeletedStatusWhenJSONLMissing(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	env.Fs.MkdirAll("/fakehome/.claude/projects", 0o755)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-gone", env.Clock)
	db.SetSessionActive(d, "sess-gone", false)

	result := reconcileDBWithLive(env, d, []*Session{})

	for _, s := range result {
		if s.SessionID == "sess-gone" && s.Status != StatusDeleted {
			t.Fatalf("expected StatusDeleted for missing JSONL, got %d", s.Status)
		}
	}
}

func TestReconcile_DisplayNamePropagation(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	tmuxID, _ := db.CreateWorkstreamDB(d, "my-cool-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)

	live := []*Session{
		{SessionID: "sess-A", TmuxSession: tmuxID},
	}

	result := reconcileDBWithLive(env, d, live)

	for _, s := range result {
		if s.SessionID == "sess-A" && s.TmuxDisplayName != "my-cool-project" {
			t.Fatalf("expected TmuxDisplayName 'my-cool-project', got '%s'", s.TmuxDisplayName)
		}
	}
}

func TestReconcile_ReadsClaudeNameFromJSONL(t *testing.T) {
	now := time.Now()
	env, _ := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.SetSessionActive(d, "sess-A", false)

	writeTestJSONL(env.Fs, env.Home, "-test-proj", "sess-A",
		`{"type":"last-prompt"}`,
		agentNameLine("vivid-pine"),
	)

	result := reconcileDBWithLive(env, d, []*Session{})

	for _, s := range result {
		if s.SessionID == "sess-A" && s.ClaudeName != "vivid-pine" {
			t.Fatalf("expected ClaudeName 'vivid-pine', got '%s'", s.ClaudeName)
		}
	}
}
