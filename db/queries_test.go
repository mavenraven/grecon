package db

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func testClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func writeJSONL(fs afero.Fs, home, projectDir, sessionID string, lines ...string) {
	dir := filepath.Join(home, ".claude", "projects", projectDir)
	fs.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, sessionID+".jsonl")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	afero.WriteFile(fs, path, []byte(content), 0o644)
}

func worktreeStateEntry(worktreePath string) string {
	if worktreePath == "" {
		return `{"type":"worktree-state","worktreeSession":null}`
	}
	entry := map[string]any{
		"type": "worktree-state",
		"worktreeSession": map[string]any{
			"worktreePath": worktreePath,
		},
	}
	b, _ := json.Marshal(entry)
	return string(b)
}

func agentNameEntry(name string) string {
	entry := map[string]any{
		"type":      "agent-name",
		"agentName": name,
	}
	b, _ := json.Marshal(entry)
	return string(b)
}

func TestPruneSkipsRecentClaudeSessions(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	now := time.Now()

	tmuxID, _ := CreateWorkstreamDB(d, "test-session", testClock(now))
	AddClaudeSession(d, 1, "sess-1", testClock(now.Add(-2*time.Minute)))

	PruneDeadSessions(d, map[string]bool{tmuxID: true}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) == 0 {
		t.Fatal("workstream should still exist")
	}
	if len(ws[0].Sessions) != 1 {
		t.Fatalf("session should not be pruned (created 2 min ago), got %d sessions", len(ws[0].Sessions))
	}
}

func TestPrunePrunesOldMissingJSONL(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	fs.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755)
	now := time.Now()

	CreateWorkstreamDB(d, "test-session", testClock(now.Add(-20*time.Minute)))
	AddClaudeSession(d, 1, "sess-old", testClock(now.Add(-15*time.Minute)))

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) != 0 {
		t.Fatalf("workstream should be pruned (empty, old), got %d", len(ws))
	}
}

func TestPruneKeepsSessionWithExistingJSONL(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	now := time.Now()

	CreateWorkstreamDB(d, "test-session", testClock(now.Add(-20*time.Minute)))
	AddClaudeSession(d, 1, "sess-alive", testClock(now.Add(-15*time.Minute)))

	writeJSONL(fs, home, "-test-project", "sess-alive", `{"type":"user","cwd":"/tmp"}`)

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) == 0 || len(ws[0].Sessions) != 1 {
		t.Fatal("session with existing JSONL should not be pruned")
	}
}

func TestPruneSoftDeletesWorktreeGone(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	now := time.Now()

	CreateWorkstreamDB(d, "test-session", testClock(now.Add(-20*time.Minute)))
	AddClaudeSession(d, 1, "sess-wt", testClock(now.Add(-15*time.Minute)))

	writeJSONL(fs, home, "-test-project", "sess-wt",
		worktreeStateEntry("/tmp/worktree-that-exists"),
		worktreeStateEntry(""),
	)

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) != 0 {
		sessions := 0
		for _, w := range ws {
			sessions += len(w.Sessions)
		}
		t.Fatalf("session with deleted worktree should be pruned, got %d workstreams with %d sessions", len(ws), sessions)
	}
}

func TestPruneKeepsExistingWorktree(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	now := time.Now()

	wtPath := "/tmp/my-worktree"
	fs.MkdirAll(wtPath, 0o755)

	CreateWorkstreamDB(d, "test-session", testClock(now.Add(-20*time.Minute)))
	AddClaudeSession(d, 1, "sess-wt-ok", testClock(now.Add(-15*time.Minute)))

	writeJSONL(fs, home, "-test-project", "sess-wt-ok",
		worktreeStateEntry(wtPath),
	)

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) == 0 || len(ws[0].Sessions) != 1 {
		t.Fatal("session with existing worktree should not be pruned")
	}
}

func TestPruneSoftDeletesEmptyOldWorkstream(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	fs.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755)
	now := time.Now()

	CreateWorkstreamDB(d, "empty-ws", testClock(now.Add(-20*time.Minute)))

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) != 0 {
		t.Fatalf("empty old workstream should be pruned, got %d", len(ws))
	}
}

func TestPruneSkipsNewEmptyWorkstream(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	now := time.Now()

	CreateWorkstreamDB(d, "new-ws", testClock(now.Add(-5*time.Minute)))

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) != 1 {
		t.Fatalf("new empty workstream should NOT be pruned (grace period), got %d", len(ws))
	}
}

func TestPruneSkipsLiveWorkstream(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	fs.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755)
	now := time.Now()

	tmuxID, _ := CreateWorkstreamDB(d, "live-ws", testClock(now.Add(-20*time.Minute)))

	PruneDeadSessions(d, map[string]bool{tmuxID: true}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	if len(ws) != 1 {
		t.Fatalf("live workstream should NOT be pruned, got %d", len(ws))
	}
}

func TestSoftDeleteAndQuery(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	now := time.Now()

	CreateWorkstreamDB(d, "test", testClock(now))
	AddClaudeSession(d, 1, "sess-1", testClock(now))

	ws := AllWorkstreams(d)
	if len(ws[0].Sessions) != 1 {
		t.Fatal("should have 1 session")
	}

	SoftDeleteSession(d, "sess-1", testClock(now))

	ws = AllWorkstreams(d)
	if len(ws[0].Sessions) != 0 {
		t.Fatal("soft-deleted session should not appear")
	}

	ids := SoftDeletedSessionIDs(d)
	if len(ids) != 1 || ids[0] != "sess-1" {
		t.Fatalf("SoftDeletedSessionIDs should return sess-1, got %v", ids)
	}
}

func TestReadAgentNameFromJSONL(t *testing.T) {
	fs := afero.NewMemMapFs()
	home := "/fakehome"

	writeJSONL(fs, home, "-test-project", "sess-1",
		`{"type":"last-prompt"}`,
		`{"type":"custom-title"}`,
		agentNameEntry("vivid-pine"),
		`{"type":"mode"}`,
	)

	name := FindJSONLPath(fs, home, "sess-1")
	if name == "" {
		t.Fatal("should find JSONL path")
	}

	f, err := fs.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestJSONLExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	fs.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755)

	if JSONLExists(fs, home, "nonexistent") {
		t.Fatal("should not exist")
	}

	writeJSONL(fs, home, "-test-project", "sess-1", `{"type":"user"}`)

	if !JSONLExists(fs, home, "sess-1") {
		t.Fatal("should exist")
	}
}

func TestWorktreeGone_NoWorktree(t *testing.T) {
	fs := afero.NewMemMapFs()
	home := "/fakehome"

	writeJSONL(fs, home, "-proj", "sess-1", `{"type":"user","cwd":"/tmp"}`)

	if WorktreeGone(fs, home, "sess-1") {
		t.Fatal("session without worktree should return false")
	}
}

func TestWorktreeGone_ExplicitlyDeleted(t *testing.T) {
	fs := afero.NewMemMapFs()
	home := "/fakehome"

	writeJSONL(fs, home, "-proj", "sess-1",
		worktreeStateEntry("/tmp/wt-1"),
		worktreeStateEntry(""),
	)

	if !WorktreeGone(fs, home, "sess-1") {
		t.Fatal("explicitly deleted worktree should return true")
	}
}

func TestWorktreeGone_PathMissing(t *testing.T) {
	fs := afero.NewMemMapFs()
	home := "/fakehome"

	writeJSONL(fs, home, "-proj", "sess-1",
		worktreeStateEntry("/tmp/nonexistent-worktree"),
	)

	if !WorktreeGone(fs, home, "sess-1") {
		t.Fatal("missing worktree path should return true")
	}
}

func TestWorktreeGone_PathExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	home := "/fakehome"

	wtPath := "/tmp/real-worktree"
	fs.MkdirAll(wtPath, 0o755)

	writeJSONL(fs, home, "-proj", "sess-1",
		worktreeStateEntry(wtPath),
	)

	if WorktreeGone(fs, home, "sess-1") {
		t.Fatal("existing worktree path should return false")
	}
}

func TestCreateWorkstreamDB_UniqueUUIDs(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	now := time.Now()

	id1, _ := CreateWorkstreamDB(d, "same-name", testClock(now))
	id2, _ := CreateWorkstreamDB(d, "same-name", testClock(now))

	if id1 == id2 {
		t.Fatal("two workstreams with the same display name should get different UUIDs")
	}

	ws := AllWorkstreams(d)
	if len(ws) != 2 {
		t.Fatalf("should have 2 workstreams, got %d", len(ws))
	}
}

func TestGracePeriodBoundary(t *testing.T) {
	d := OpenTestDB()
	defer d.Close()
	fs := afero.NewMemMapFs()
	home := "/fakehome"
	fs.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755)
	now := time.Now()

	CreateWorkstreamDB(d, "boundary", testClock(now.Add(-10*time.Minute)))
	AddClaudeSession(d, 1, "sess-boundary", testClock(now.Add(-10*time.Minute)))

	PruneDeadSessions(d, map[string]bool{}, fs, home, testClock(now))

	ws := AllWorkstreams(d)
	for _, w := range ws {
		for _, s := range w.Sessions {
			if s.SessionID == "sess-boundary" {
				t.Fatal("session at exactly 10 minutes should be pruned")
			}
		}
	}
	fmt.Println("boundary test passed")
}
