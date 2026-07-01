package server

import (
	"errors"
	"testing"
	"time"

	"grecon/db"

	"github.com/spf13/afero"
)

type fakeErrCmd struct {
	fakeCmd
	failOn map[string]bool
}

func (f *fakeErrCmd) Run(name string, args ...string) error {
	f.Runs = append(f.Runs, append([]string{name}, args...))
	key := name
	for _, a := range args {
		key += " " + a
	}
	if f.failOn != nil && f.failOn[key] {
		return errors.New("command failed")
	}
	return nil
}

func TestRestore_SkipsExistingTmuxSession(t *testing.T) {
	now := time.Now()
	env, cmd := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	tmuxID, _ := db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.SetSessionActive(d, "sess-A", true)

	cmd.Outputs["which claude"] = []byte("/usr/bin/claude")
	// has-session succeeds (session exists) — fakeCmd.Run returns nil by default

	reconcileWithEnv(env, d)

	for _, r := range cmd.Runs {
		if len(r) >= 3 && r[0] == "tmux" && r[1] == "new-session" {
			t.Fatal("should not create new session when tmux session already exists")
		}
	}
	_ = tmuxID
}

func TestRestore_RestoresFirstActiveSession(t *testing.T) {
	now := time.Now()
	fs := afero.NewMemMapFs()
	errCmd := &fakeErrCmd{
		fakeCmd: fakeCmd{Outputs: map[string][]byte{
			"which claude": []byte("/usr/bin/claude"),
		}},
		failOn: map[string]bool{},
	}
	env := &Env{Fs: fs, Cmd: errCmd, Clock: func() time.Time { return now }, Home: "/fakehome"}

	d := db.OpenTestDB()
	defer d.Close()

	tmuxID, _ := db.CreateWorkstreamDB(d, "my-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.AddClaudeSession(d, 1, "sess-B", env.Clock)
	db.SetSessionActive(d, "sess-A", true)
	db.SetSessionActive(d, "sess-B", true)

	// has-session fails (session doesn't exist)
	errCmd.failOn["tmux has-session -t "+tmuxID] = true

	// Write JSONL so FindSessionCWDFS works
	cwd := "/fakehome/projects/myapp"
	fs.MkdirAll(cwd, 0o755)
	writeTestJSONL(fs, "/fakehome", "-proj", "sess-A", `{"type":"user","cwd":"`+cwd+`"}`)
	writeTestJSONL(fs, "/fakehome", "-proj", "sess-B", `{"type":"user","cwd":"`+cwd+`"}`)

	reconcileWithEnv(env, d)

	newSessionCount := 0
	newWindowCount := 0
	for _, r := range errCmd.Runs {
		if len(r) >= 3 && r[0] == "tmux" && r[1] == "new-session" {
			newSessionCount++
			// Should contain --resume sess-A
			found := false
			for _, arg := range r {
				if arg == "sess-A" {
					found = true
				}
			}
			if !found {
				t.Fatalf("new-session should resume sess-A, got %v", r)
			}
		}
		if len(r) >= 3 && r[0] == "tmux" && r[1] == "new-window" {
			newWindowCount++
		}
	}

	if newSessionCount != 1 {
		t.Fatalf("should create exactly 1 new-session, got %d", newSessionCount)
	}
	if newWindowCount != 1 {
		t.Fatalf("should create 1 new-window for the second session, got %d", newWindowCount)
	}
}

func TestRestore_SkipsWorkstreamWithNoActiveSessions(t *testing.T) {
	now := time.Now()
	env, cmd := testEnv(now)
	d := db.OpenTestDB()
	defer d.Close()

	db.CreateWorkstreamDB(d, "inactive-project", env.Clock)
	db.AddClaudeSession(d, 1, "sess-A", env.Clock)
	db.SetSessionActive(d, "sess-A", false)

	reconcileWithEnv(env, d)

	for _, r := range cmd.Runs {
		if len(r) >= 3 && r[0] == "tmux" && r[1] == "new-session" {
			t.Fatal("should not create session for workstream with no active sessions")
		}
	}
}
