package server

import (
	"testing"
	"time"
)

// --- determineStatus ---

func TestDetermineStatus_NewWhenZeroTokensAndIdle(t *testing.T) {
	paneContents := map[string]string{"s:0.0": "❯ "}
	status := determineStatus(0, 0, "s:0.0", paneContents)
	if status != StatusNew {
		t.Fatalf("expected StatusNew, got %d", status)
	}
}

func TestDetermineStatus_IdleWhenTokensExist(t *testing.T) {
	paneContents := map[string]string{"s:0.0": "❯ "}
	status := determineStatus(100, 50, "s:0.0", paneContents)
	if status != StatusIdle {
		t.Fatalf("expected StatusIdle, got %d", status)
	}
}

func TestDetermineStatus_WorkingWhenSpinner(t *testing.T) {
	paneContents := map[string]string{"s:0.0": "✦ Processing…"}
	status := determineStatus(100, 50, "s:0.0", paneContents)
	if status != StatusWorking {
		t.Fatalf("expected StatusWorking, got %d", status)
	}
}

func TestDetermineStatus_NewWhenNoPaneTarget(t *testing.T) {
	status := determineStatus(0, 0, "", nil)
	if status != StatusNew {
		t.Fatalf("expected StatusNew when no pane target, got %d", status)
	}
}

func TestDetermineStatus_IdleWhenNoPaneTargetButTokens(t *testing.T) {
	status := determineStatus(100, 0, "", nil)
	if status != StatusIdle {
		t.Fatalf("expected StatusIdle, got %d", status)
	}
}

func TestDetermineStatus_InputWhenEscToCancel(t *testing.T) {
	paneContents := map[string]string{"s:0.0": "  Esc to cancel\n"}
	status := determineStatus(100, 50, "s:0.0", paneContents)
	if status != StatusInput {
		t.Fatalf("expected StatusInput, got %d", status)
	}
}

// --- paneStatusFromContent ---

func TestPaneStatus_Empty(t *testing.T) {
	if paneStatusFromContent("") != StatusIdle {
		t.Fatal("empty content should be Idle")
	}
}

func TestPaneStatus_Spinner(t *testing.T) {
	if paneStatusFromContent("✦ Working…") != StatusWorking {
		t.Fatal("spinner should be Working")
	}
}

func TestPaneStatus_Prompt(t *testing.T) {
	if paneStatusFromContent("❯ ") != StatusIdle {
		t.Fatal("prompt should be Idle")
	}
}

func TestPaneStatus_NumberedInput(t *testing.T) {
	if paneStatusFromContent("❯ 1. Option one") != StatusInput {
		t.Fatal("numbered prompt should be Input")
	}
}

func TestPaneStatus_EscToCancel(t *testing.T) {
	if paneStatusFromContent("Press Esc to cancel") != StatusInput {
		t.Fatal("Esc to cancel should be Input")
	}
}

// --- debounceStatus ---

func TestDebounceStatus_HoldsWorkingBriefly(t *testing.T) {
	id := "test-debounce-hold"
	statusDebounceMu.Lock()
	statusDebounceMap[id] = statusHold{status: StatusWorking, since: time.Now()}
	statusDebounceMu.Unlock()

	result := debounceStatus(id, StatusIdle)
	if result != StatusWorking {
		t.Fatalf("should hold Working during debounce period, got %d", result)
	}
}

func TestDebounceStatus_ReleasesAfterTimeout(t *testing.T) {
	id := "test-debounce-release"
	statusDebounceMu.Lock()
	statusDebounceMap[id] = statusHold{status: StatusWorking, since: time.Now().Add(-1 * time.Second)}
	statusDebounceMu.Unlock()

	result := debounceStatus(id, StatusIdle)
	if result != StatusIdle {
		t.Fatalf("should release to Idle after debounce period, got %d", result)
	}
}

func TestDebounceStatus_PassthroughNonWorkingToIdle(t *testing.T) {
	id := "test-debounce-passthrough"
	statusDebounceMu.Lock()
	statusDebounceMap[id] = statusHold{status: StatusIdle, since: time.Now()}
	statusDebounceMu.Unlock()

	result := debounceStatus(id, StatusWorking)
	if result != StatusWorking {
		t.Fatalf("non-Working->Idle transitions should pass through, got %d", result)
	}
}

// --- buildLiveMapFromPanes ---

func TestBuildLiveMap_MatchesPIDsToSessions(t *testing.T) {
	claudePanes := [][4]string{
		{"100", "my-session", "my-session:0.0", "/home/user"},
		{"200", "other", "other:0.0", "/tmp"},
	}
	pidMap := map[int]sessionFileInfo{
		100: {sessionID: "sess-abc", startedAt: 1000},
	}

	m := buildLiveMapFromPanes(claudePanes, pidMap)

	if len(m) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m))
	}
	if m["sess-abc"] == nil {
		t.Fatal("sess-abc should be in map")
	}
	if m["sess-abc"].pid != 100 {
		t.Fatalf("expected pid 100, got %d", m["sess-abc"].pid)
	}
	if m["sess-abc"].tmuxSession != "my-session" {
		t.Fatalf("expected tmuxSession 'my-session', got %s", m["sess-abc"].tmuxSession)
	}
}

func TestBuildLiveMap_SkipsUnmatchedPIDs(t *testing.T) {
	claudePanes := [][4]string{
		{"999", "orphan", "orphan:0.0", "/tmp"},
	}
	pidMap := map[int]sessionFileInfo{}

	m := buildLiveMapFromPanes(claudePanes, pidMap)
	if len(m) != 0 {
		t.Fatalf("unmatched PIDs should not appear in map, got %d entries", len(m))
	}
}

// --- ValidateCWD ---
// Note: uses os.Stat directly, not afero — testing with real FS

func TestValidateCWD_RejectsRelativePath(t *testing.T) {
	if ValidateCWD("relative/path") {
		t.Fatal("relative path should be rejected")
	}
}

func TestValidateCWD_RejectsEmptyString(t *testing.T) {
	if ValidateCWD("") {
		t.Fatal("empty string should be rejected")
	}
}

func TestValidateCWD_AcceptsExistingDir(t *testing.T) {
	if !ValidateCWD("/tmp") {
		t.Fatal("/tmp should be valid")
	}
}

func TestValidateCWD_RejectsNonExistentPath(t *testing.T) {
	if ValidateCWD("/nonexistent/path/that/doesnt/exist") {
		t.Fatal("nonexistent path should be rejected")
	}
}

// --- markBgTaskLiveness ---

func TestMarkBgTaskLiveness_AliveMark(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "npm test", Alive: false, SeenAlive: false},
	}
	pt := &processTree{
		children: map[int][]int{1: {2}},
		args:     map[int]string{2: "npm test"},
	}

	markBgTaskLiveness(tasks, 1, pt)

	if !tasks[0].Alive {
		t.Fatal("task should be marked alive")
	}
	if !tasks[0].SeenAlive {
		t.Fatal("task should be marked seen alive")
	}
}

func TestMarkBgTaskLiveness_DeadMark(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "npm test", Alive: false, SeenAlive: false},
	}
	pt := &processTree{
		children: map[int][]int{1: {2}},
		args:     map[int]string{2: "git status"},
	}

	markBgTaskLiveness(tasks, 1, pt)

	if tasks[0].Alive {
		t.Fatal("task should be dead")
	}
	if tasks[0].DeadSince.IsZero() {
		t.Fatal("DeadSince should be set")
	}
}

func TestMarkBgTaskLiveness_NilProcessTree(t *testing.T) {
	tasks := []*BackgroundTask{{Command: "test"}}
	markBgTaskLiveness(tasks, 1, nil)
	// should not panic
}

func TestMarkBgTaskLiveness_EmptyTasks(t *testing.T) {
	pt := &processTree{children: map[int][]int{}, args: map[int]string{}}
	markBgTaskLiveness(nil, 1, pt)
	// should not panic
}

// --- pruneStaleBgTasks ---

func TestPruneStaleBgTasks_RemovesCompletedDead(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "done", Completed: true, Alive: false},
	}
	kept := pruneStaleBgTasks(tasks)
	if len(kept) != 0 {
		t.Fatal("completed+dead task should be pruned")
	}
}

func TestPruneStaleBgTasks_RemovesNeverSeenAlive(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "ghost", Alive: false, SeenAlive: false},
	}
	kept := pruneStaleBgTasks(tasks)
	if len(kept) != 0 {
		t.Fatal("never-alive task should be pruned")
	}
}

func TestPruneStaleBgTasks_RemovesDeadTooLong(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "old", Alive: false, SeenAlive: true, DeadSince: time.Now().Add(-3 * time.Minute)},
	}
	kept := pruneStaleBgTasks(tasks)
	if len(kept) != 0 {
		t.Fatal("dead-too-long task should be pruned")
	}
}

func TestPruneStaleBgTasks_KeepsAlive(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "running", Alive: true, SeenAlive: true},
	}
	kept := pruneStaleBgTasks(tasks)
	if len(kept) != 1 {
		t.Fatal("alive task should be kept")
	}
}

func TestPruneStaleBgTasks_KeepsRecentlyDead(t *testing.T) {
	tasks := []*BackgroundTask{
		{Command: "just-died", Alive: false, SeenAlive: true, DeadSince: time.Now().Add(-30 * time.Second)},
	}
	kept := pruneStaleBgTasks(tasks)
	if len(kept) != 1 {
		t.Fatal("recently-dead task should be kept")
	}
}
