package server

import (
	"testing"
	"time"

	"grecon/db"
)

func TestFixDefaultPath_SetsDefaultOnWorktreeDetected(t *testing.T) {
	cmd := &fakeCmd{Outputs: map[string][]byte{
		"tmux display-message -t uuid-1:0.0 -p #{pane_current_path}": []byte("/home/user/project/.claude/worktrees/fuzzy-cat\n"),
	}}

	fixDefaultPathWith(cmd, "uuid-1", 0)

	found := false
	for _, r := range cmd.Runs {
		if len(r) >= 5 && r[0] == "tmux" && r[1] == "attach-session" && r[3] == "uuid-1" && r[5] == "/home/user/project/.claude/worktrees/fuzzy-cat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("should call attach-session with worktree path, runs: %v", cmd.Runs)
	}
}

func TestFixDefaultPath_SkipsNonWorktreePath(t *testing.T) {
	cmd := &fakeCmd{Outputs: map[string][]byte{
		"tmux display-message -t uuid-1:0.0 -p #{pane_current_path}": []byte("/home/user/project\n"),
	}}

	fixDefaultPathWith(cmd, "uuid-1", 0)

	for _, r := range cmd.Runs {
		if len(r) >= 2 && r[0] == "tmux" && r[1] == "attach-session" {
			t.Fatal("should NOT call attach-session for non-worktree path")
		}
	}
}

func TestFixDefaultPath_HandlesDisplayMessageError(t *testing.T) {
	cmd := &fakeCmd{Outputs: map[string][]byte{}}

	fixDefaultPathWith(cmd, "uuid-1", 0)

	for _, r := range cmd.Runs {
		if len(r) >= 2 && r[0] == "tmux" && r[1] == "attach-session" {
			t.Fatal("should not call attach-session when display-message fails")
		}
	}
}

func TestFixDefaultPath_GivesUpAfter30Tries(t *testing.T) {
	outputCalls := 0
	cmd := &countingCmd{
		fakeCmd: fakeCmd{Outputs: map[string][]byte{
			"tmux display-message -t uuid-1:0.0 -p #{pane_current_path}": []byte("/home/user/not-a-worktree\n"),
		}},
		outputCount: &outputCalls,
	}

	fixDefaultPathWith(cmd, "uuid-1", time.Millisecond)

	if outputCalls != 30 {
		t.Fatalf("should try exactly 30 times, got %d", outputCalls)
	}
	for _, r := range cmd.Runs {
		if len(r) >= 2 && r[0] == "tmux" && r[1] == "attach-session" {
			t.Fatal("should not call attach-session when worktree never appears")
		}
	}
}

// --- handleCreateSession error paths ---

func TestHandleCreateSession_InvalidCWD(t *testing.T) {
	d := db.OpenTestDB()
	defer d.Close()
	db.SetGlobal(d)
	defer db.SetGlobal(nil)

	resp := handleCreateSession(Command{
		Type: "create-session",
		Name: "test",
		CWD:  "relative/path",
	})
	if resp.OK {
		t.Fatal("should fail with invalid CWD")
	}
	if resp.Error != "invalid cwd" {
		t.Fatalf("expected 'invalid cwd', got '%s'", resp.Error)
	}
}

func TestHandleCreateSession_NoDB(t *testing.T) {
	old := db.Get()
	db.SetGlobal(nil)
	defer db.SetGlobal(old)

	resp := handleCreateSession(Command{
		Type: "create-session",
		Name: "test",
		CWD:  "/tmp",
	})
	if resp.OK {
		t.Fatal("should fail with no database")
	}
	if resp.Error != "no database" {
		t.Fatalf("expected 'no database', got '%s'", resp.Error)
	}
}

func TestHandleDeleteSession_NoSessionID(t *testing.T) {
	d := db.OpenTestDB()
	defer d.Close()
	db.SetGlobal(d)
	defer db.SetGlobal(nil)

	resp := handleDeleteSession(Command{Type: "delete-session"})
	if resp.OK {
		t.Fatal("should fail with no session_id")
	}
	if resp.Error != "no session_id" {
		t.Fatalf("expected 'no session_id', got '%s'", resp.Error)
	}
}

func TestHandleDeleteSession_Success(t *testing.T) {
	d := db.OpenTestDB()
	defer d.Close()
	db.SetGlobal(d)
	defer db.SetGlobal(nil)

	db.CreateWorkstreamDB(d, "test", time.Now)
	db.AddClaudeSession(d, 1, "sess-1", time.Now)

	resp := handleDeleteSession(Command{Type: "delete-session", SessionID: "sess-1"})
	if !resp.OK {
		t.Fatalf("should succeed, got error: %s", resp.Error)
	}

	ids := db.SoftDeletedSessionIDs(d)
	found := false
	for _, id := range ids {
		if id == "sess-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("sess-1 should be soft-deleted")
	}
}

func TestHandleReactivateSession_BadCWD(t *testing.T) {
	d := db.OpenTestDB()
	defer d.Close()
	db.SetGlobal(d)
	defer db.SetGlobal(nil)

	db.CreateWorkstreamDB(d, "test", time.Now)
	db.AddClaudeSession(d, 1, "sess-nocwd", time.Now)

	resp := handleReactivateSession(Command{
		Type:        "reactivate-session",
		SessionID:   "sess-nocwd",
		TmuxSession: "nonexistent-tmux-uuid",
	})

	if resp.OK {
		t.Fatal("should fail with bad cwd when JSONL doesn't exist")
	}
	if resp.Error != "session no longer exists" {
		t.Fatalf("expected 'session no longer exists', got '%s'", resp.Error)
	}
}

type countingCmd struct {
	fakeCmd
	outputCount *int
}

func (c *countingCmd) Output(name string, args ...string) ([]byte, error) {
	*c.outputCount++
	return c.fakeCmd.Output(name, args...)
}
