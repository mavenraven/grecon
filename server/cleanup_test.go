package server

import (
	"testing"
	"time"

	"grecon/db"
)

func TestCleanup_KillsPanesForSoftDeletedSessions(t *testing.T) {
	now := time.Now()
	env, cmd := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "test", env.Clock)
	db.AddClaudeSession(d, 1, "sess-D", env.Clock)
	db.SoftDeleteSession(d, "sess-D", env.Clock)

	live := []*Session{
		{SessionID: "sess-D", PaneTarget: "uuid-1:0.0"},
	}

	cleanupSoftDeleted(env, d, live)

	found := false
	for _, r := range cmd.Runs {
		if len(r) >= 4 && r[0] == "tmux" && r[1] == "kill-pane" && r[3] == "uuid-1:0.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("should have killed pane uuid-1:0.0, runs: %v", cmd.Runs)
	}
}

func TestCleanup_IgnoresNonDeletedSessions(t *testing.T) {
	now := time.Now()
	env, cmd := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "test", env.Clock)
	db.AddClaudeSession(d, 1, "sess-alive", env.Clock)

	live := []*Session{
		{SessionID: "sess-alive", PaneTarget: "uuid-1:0.0"},
	}

	cleanupSoftDeleted(env, d, live)

	for _, r := range cmd.Runs {
		if len(r) >= 2 && r[0] == "tmux" && r[1] == "kill-pane" {
			t.Fatalf("should NOT have killed any pane, but killed: %v", r)
		}
	}
}

func TestCleanup_IgnoresDeletedSessionsWithoutPaneTarget(t *testing.T) {
	now := time.Now()
	env, cmd := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "test", env.Clock)
	db.AddClaudeSession(d, 1, "sess-dead", env.Clock)
	db.SoftDeleteSession(d, "sess-dead", env.Clock)

	live := []*Session{
		{SessionID: "sess-dead", PaneTarget: ""},
	}

	cleanupSoftDeleted(env, d, live)

	for _, r := range cmd.Runs {
		if len(r) >= 2 && r[0] == "tmux" && r[1] == "kill-pane" {
			t.Fatalf("should NOT have killed any pane (no target), but ran: %v", r)
		}
	}
}

func TestCleanup_MultipleDeletedSessions(t *testing.T) {
	now := time.Now()
	env, cmd := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "test", env.Clock)
	db.AddClaudeSession(d, 1, "sess-1", env.Clock)
	db.AddClaudeSession(d, 1, "sess-2", env.Clock)
	db.AddClaudeSession(d, 1, "sess-3", env.Clock)
	db.SoftDeleteSession(d, "sess-1", env.Clock)
	db.SoftDeleteSession(d, "sess-3", env.Clock)

	live := []*Session{
		{SessionID: "sess-1", PaneTarget: "uuid-1:0.0"},
		{SessionID: "sess-2", PaneTarget: "uuid-1:1.0"},
		{SessionID: "sess-3", PaneTarget: "uuid-1:2.0"},
	}

	cleanupSoftDeleted(env, d, live)

	killCount := 0
	killedTargets := make(map[string]bool)
	for _, r := range cmd.Runs {
		if len(r) >= 4 && r[0] == "tmux" && r[1] == "kill-pane" {
			killCount++
			killedTargets[r[3]] = true
		}
	}

	if killCount != 2 {
		t.Fatalf("should have killed 2 panes, killed %d", killCount)
	}
	if !killedTargets["uuid-1:0.0"] {
		t.Fatal("should have killed uuid-1:0.0")
	}
	if !killedTargets["uuid-1:2.0"] {
		t.Fatal("should have killed uuid-1:2.0")
	}
	if killedTargets["uuid-1:1.0"] {
		t.Fatal("should NOT have killed uuid-1:1.0 (not deleted)")
	}
}
