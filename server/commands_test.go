package server

import (
	"testing"
	"time"
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

type countingCmd struct {
	fakeCmd
	outputCount *int
}

func (c *countingCmd) Output(name string, args ...string) ([]byte, error) {
	*c.outputCount++
	return c.fakeCmd.Output(name, args...)
}
