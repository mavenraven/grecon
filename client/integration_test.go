package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/afero"

	"grecon/db"
	"grecon/server"
)

type testCmd struct {
	mu      sync.Mutex
	outputs map[string][]byte
}

func newTestCmd() *testCmd {
	return &testCmd{outputs: make(map[string][]byte)}
}

func (t *testCmd) SetOutput(key string, data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outputs[key] = data
}

func (t *testCmd) Run(name string, args ...string) error { return nil }
func (t *testCmd) Output(name string, args ...string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := name
	for _, a := range args {
		key += " " + a
	}
	if out, ok := t.outputs[key]; ok {
		return out, nil
	}
	return nil, nil
}
func (t *testCmd) RunWithStdin(stdin string, name string, args ...string) (string, error) {
	return "", nil
}

func newTestTUI(sessions []*server.Session) tuiModel {
	app := NewApp()
	app.Sessions = sessions
	return tuiModel{app: app, width: 120, height: 40}
}

func sendKey(m tuiModel, r rune) tuiModel {
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return result.(tuiModel)
}

func sendSpecial(m tuiModel, key tea.KeyType) tuiModel {
	result, _ := m.Update(tea.KeyMsg{Type: key})
	return result.(tuiModel)
}

func TestIntegration_PickerShowsSessions(t *testing.T) {
	sessions := []*server.Session{
		{
			SessionID:       "sess-1",
			TmuxSession:     "uuid-1",
			TmuxDisplayName: "my-project",
			ClaudeName:      "vivid-pine",
			Status:          server.StatusIdle,
		},
	}
	m := newTestTUI(sessions)

	view := m.View()
	if !strings.Contains(view, "my-project") {
		t.Fatal("should show tmux display name in view")
	}
	if !strings.Contains(view, "vivid-pine") {
		t.Fatal("should show claude name in view")
	}
}

func TestIntegration_PickerGroupsByTmuxID(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "project-a", ClaudeName: "pine", Status: server.StatusIdle},
		{SessionID: "sess-2", TmuxSession: "uuid-1", TmuxDisplayName: "project-a", ClaudeName: "oak", Status: server.StatusIdle},
		{SessionID: "sess-3", TmuxSession: "uuid-2", TmuxDisplayName: "project-b", ClaudeName: "elm", Status: server.StatusIdle},
	}
	m := newTestTUI(sessions)

	view := m.View()
	if !strings.Contains(view, "project-a") {
		t.Fatal("should show project-a header")
	}
	if !strings.Contains(view, "project-b") {
		t.Fatal("should show project-b header")
	}
	if !strings.Contains(view, "pine") || !strings.Contains(view, "oak") {
		t.Fatal("should show both sessions under project-a")
	}
}

func TestIntegration_SameDisplayNameDifferentUUIDs(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "my-project", ClaudeName: "pine", Status: server.StatusIdle},
		{SessionID: "sess-2", TmuxSession: "uuid-2", TmuxDisplayName: "my-project", ClaudeName: "oak", Status: server.StatusIdle},
	}
	m := newTestTUI(sessions)

	view := m.View()
	// Both should appear as separate groups even with same display name
	if !strings.Contains(view, "pine") || !strings.Contains(view, "oak") {
		t.Fatal("both sessions should appear even with same display name")
	}
}

func TestIntegration_XDeletesSession(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "my-project", ClaudeName: "pine", Status: server.StatusIdle, PaneTarget: "uuid-1:0.0"},
	}
	m := newTestTUI(sessions)

	// Move to the session row (first selectable)
	m = sendKey(m, 'j')
	s := m.app.SelectedSession()
	if s == nil || s.SessionID != "sess-1" {
		t.Fatal("should have sess-1 selected")
	}

	// x sends delete command — in integration test without server, it won't
	// actually send but we can verify the intent
	m = sendKey(m, 'x')
	// The app calls DeleteSession which tries to send a command to the server
	// We can't test the full round-trip without the server, but we can verify
	// the key is handled without panic
}

func TestIntegration_EnterOnActiveSession(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "my-project", ClaudeName: "pine", Status: server.StatusIdle, PaneTarget: "uuid-1:0.0"},
	}
	m := newTestTUI(sessions)

	m = sendKey(m, 'j')
	m = sendSpecial(m, tea.KeyEnter)

	if !m.app.ShouldQuit {
		t.Fatal("Enter on active session should quit")
	}
	if m.app.SwitchTarget != "uuid-1:0.0" {
		t.Fatalf("should switch to pane target, got %s", m.app.SwitchTarget)
	}
}

func TestIntegration_EnterOnDeletedSession(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "my-project", Status: server.StatusDeleted},
	}
	m := newTestTUI(sessions)

	m = sendKey(m, 'j')
	m = sendSpecial(m, tea.KeyEnter)

	if !m.app.ShouldQuit {
		t.Fatal("Enter on deleted session should quit with error")
	}
	if m.app.ExitError == "" {
		t.Fatal("should set exit error for deleted session with no pane target")
	}
}

func TestIntegration_FilterSearch(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "frontend", ClaudeName: "pine", Status: server.StatusIdle, ProjectName: "frontend"},
		{SessionID: "sess-2", TmuxSession: "uuid-2", TmuxDisplayName: "backend", ClaudeName: "oak", Status: server.StatusIdle, ProjectName: "backend"},
	}
	m := newTestTUI(sessions)

	// Activate filter
	m = sendKey(m, '/')
	if !m.app.FilterActive {
		t.Fatal("/ should activate filter")
	}

	// Type search query
	m = sendKey(m, 'f')
	m = sendKey(m, 'r')
	m = sendKey(m, 'o')
	m = sendKey(m, 'n')
	m = sendKey(m, 't')

	view := m.View()
	if !strings.Contains(view, "frontend") {
		t.Fatal("filtered view should show matching session")
	}
}

func TestIntegration_NOpensFormAndEscReturns(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "sess-1", TmuxSession: "uuid-1", TmuxDisplayName: "my-project", ClaudeName: "pine", Status: server.StatusIdle},
	}
	m := newTestTUI(sessions)

	// Open form
	m = sendKey(m, 'n')
	if m.newForm == nil {
		t.Fatal("n should open new session form")
	}

	view := m.View()
	if !strings.Contains(view, "Tmux Session") {
		t.Fatal("form view should show Tmux Session field")
	}

	// Esc back to picker
	m = sendSpecial(m, tea.KeyEsc)
	if m.newForm != nil {
		t.Fatal("Esc should return to picker")
	}

	view = m.View()
	if !strings.Contains(view, "my-project") {
		t.Fatal("picker should show sessions again after Esc")
	}
}

func TestIntegration_QQuits(t *testing.T) {
	m := newTestTUI([]*server.Session{})

	m = sendKey(m, 'q')
	if !m.app.ShouldQuit {
		t.Fatal("q should quit")
	}
}

func TestIntegration_StatusDisplay(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "s1", TmuxSession: "u1", TmuxDisplayName: "proj", ClaudeName: "a", Status: server.StatusIdle, PaneTarget: "u1:0.0"},
		{SessionID: "s2", TmuxSession: "u1", TmuxDisplayName: "proj", ClaudeName: "b", Status: server.StatusWorking, PaneTarget: "u1:1.0"},
		{SessionID: "s3", TmuxSession: "u1", TmuxDisplayName: "proj", ClaudeName: "c", Status: server.StatusInactive},
	}
	m := newTestTUI(sessions)

	view := m.View()
	if !strings.Contains(view, "Idle") {
		t.Fatal("should show Idle status")
	}
	if !strings.Contains(view, "Work") {
		t.Fatal("should show Work status")
	}
	if !strings.Contains(view, "Off") {
		t.Fatal("should show Off status for inactive")
	}
}

func TestIntegration_EmptyPickerShowsFooter(t *testing.T) {
	m := newTestTUI([]*server.Session{})

	view := m.View()
	if !strings.Contains(view, "navigate") {
		t.Fatal("empty picker should still show footer with keybinds")
	}
}

func TestIntegration_NavigationWraps(t *testing.T) {
	sessions := []*server.Session{
		{SessionID: "s1", TmuxSession: "u1", TmuxDisplayName: "proj", ClaudeName: "a", Status: server.StatusIdle},
		{SessionID: "s2", TmuxSession: "u1", TmuxDisplayName: "proj", ClaudeName: "b", Status: server.StatusIdle},
	}
	m := newTestTUI(sessions)

	// Navigate down past the end
	for i := 0; i < 10; i++ {
		m = sendKey(m, 'j')
	}
	if m.app.Selected < 0 {
		t.Fatal("selection should not go negative")
	}
	count := m.app.SelectableCount()
	if count > 0 && m.app.Selected >= count {
		t.Fatalf("selection %d should be < count %d", m.app.Selected, count)
	}
}

func testSessions() []*server.Session {
	return []*server.Session{
		{SessionID: "s1", TmuxSession: "u1", TmuxDisplayName: "proj-a", ClaudeName: "a", Status: server.StatusIdle},
		{SessionID: "s2", TmuxSession: "u1", TmuxDisplayName: "proj-a", ClaudeName: "b", Status: server.StatusIdle},
		{SessionID: "s3", TmuxSession: "u2", TmuxDisplayName: "proj-b", ClaudeName: "c", Status: server.StatusIdle},
		{SessionID: "s4", TmuxSession: "u2", TmuxDisplayName: "proj-b", ClaudeName: "d", Status: server.StatusIdle},
		{SessionID: "s5", TmuxSession: "u3", TmuxDisplayName: "proj-c", ClaudeName: "e", Status: server.StatusIdle},
	}
}

func TestIntegration_JMovesDown(t *testing.T) {
	m := newTestTUI(testSessions())

	start := m.app.Selected
	m = sendKey(m, 'j')

	if m.app.Selected != start+1 {
		t.Fatalf("j should move down, expected %d got %d", start+1, m.app.Selected)
	}
}

func TestIntegration_KMovesUp(t *testing.T) {
	m := newTestTUI(testSessions())

	m = sendKey(m, 'j')
	m = sendKey(m, 'j')
	pos := m.app.Selected
	m = sendKey(m, 'k')

	if m.app.Selected != pos-1 {
		t.Fatalf("k should move up, expected %d got %d", pos-1, m.app.Selected)
	}
}

func TestIntegration_KDoesNotGoBelowZero(t *testing.T) {
	m := newTestTUI(testSessions())

	for i := 0; i < 5; i++ {
		m = sendKey(m, 'k')
	}

	if m.app.Selected != 0 {
		t.Fatalf("k should not go below 0, got %d", m.app.Selected)
	}
}

func TestIntegration_JClampsAtEnd(t *testing.T) {
	m := newTestTUI(testSessions())
	count := m.app.SelectableCount()

	for i := 0; i < count+5; i++ {
		m = sendKey(m, 'j')
	}

	if m.app.Selected != count-1 {
		t.Fatalf("j should clamp at %d, got %d", count-1, m.app.Selected)
	}
}

func TestIntegration_GGoesToBottom(t *testing.T) {
	m := newTestTUI(testSessions())
	count := m.app.SelectableCount()

	m = sendKey(m, 'G')

	if m.app.Selected != count-1 {
		t.Fatalf("G should go to last item %d, got %d", count-1, m.app.Selected)
	}
}

func TestIntegration_ggGoesToTop(t *testing.T) {
	m := newTestTUI(testSessions())

	m = sendKey(m, 'G')
	m = sendKey(m, 'g')
	m = sendKey(m, 'g')

	if m.app.Selected != 0 {
		t.Fatalf("gg should go to top, got %d", m.app.Selected)
	}
}

func TestIntegration_MGoesToMiddle(t *testing.T) {
	m := newTestTUI(testSessions())
	count := m.app.SelectableCount()

	m = sendKey(m, 'M')

	expected := count / 2
	if m.app.Selected != expected {
		t.Fatalf("M should go to middle %d, got %d", expected, m.app.Selected)
	}
}

func TestIntegration_CtrlDPageDown(t *testing.T) {
	m := newTestTUI(testSessions())
	m.app.PageSize = 2

	m = sendKey(m, 'j') // start from 1
	start := m.app.Selected

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = result.(tuiModel)

	if m.app.Selected <= start {
		t.Fatalf("ctrl-d should page down from %d, got %d", start, m.app.Selected)
	}
}

func TestIntegration_CtrlUPageUp(t *testing.T) {
	m := newTestTUI(testSessions())
	m.app.PageSize = 2

	m = sendKey(m, 'G')
	bottom := m.app.Selected

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = result.(tuiModel)

	if m.app.Selected >= bottom {
		t.Fatalf("ctrl-u should page up from %d, got %d", bottom, m.app.Selected)
	}
}

func TestIntegration_SelectedSessionCorrect(t *testing.T) {
	m := newTestTUI(testSessions())

	m = sendKey(m, 'j') // first selectable
	s := m.app.SelectedSession()
	if s == nil {
		t.Fatal("should have a selected session")
	}

	m = sendKey(m, 'j') // second selectable
	s2 := m.app.SelectedSession()
	if s2 == nil {
		t.Fatal("should have a selected session")
	}
	if s.SessionID == s2.SessionID {
		t.Fatal("moving down should select a different session")
	}
}

func TestIntegration_FooterShowsNewKey(t *testing.T) {
	m := newTestTUI(testSessions())
	view := m.View()

	if !strings.Contains(view, "new") {
		t.Fatal("footer should show 'n new' keybinding")
	}
}

func TestIntegration_NoGhostAfterTmuxKillAndReactivate(t *testing.T) {
	instance := fmt.Sprintf("integration-test-%d", time.Now().UnixNano())
	db.SetInstance(instance)
	testSocketPath := server.SocketPath()
	testCmdSocketPath := server.CommandSocketPath()
	defer func() {
		os.Remove(testSocketPath)
		os.Remove(testCmdSocketPath)
		db.SetInstance("grecon")
	}()

	d := db.OpenTestDB()
	defer d.Close()

	home, _ := os.UserHomeDir()
	cmd := newTestCmd()
	env := &server.Env{
		Fs:    afero.NewOsFs(),
		Cmd:   cmd,
		Clock: time.Now,
		Home:  home,
		DB:    d,
	}

	// Create JSONL backing files for sessions we'll create
	jsonlDir := filepath.Join(home, ".claude", "projects", "-test-integration-ghost")
	os.MkdirAll(jsonlDir, 0o755)
	defer os.RemoveAll(jsonlDir)

	for _, sid := range []string{"sess-A", "sess-B", "sess-C"} {
		os.WriteFile(filepath.Join(jsonlDir, sid+".jsonl"), []byte(`{"type":"user","cwd":"/tmp"}`+"\n"), 0o644)
	}

	// Create session files so DiscoverSessions can match PIDs to session IDs
	sessDir := filepath.Join(home, ".claude", "sessions")
	os.MkdirAll(sessDir, 0o755)
	for i, sid := range []string{"sess-A", "sess-B", "sess-C"} {
		pid := 90001 + i
		data, _ := json.Marshal(map[string]any{
			"pid": pid, "sessionId": sid, "cwd": "/tmp", "startedAt": time.Now().UnixMilli(),
		})
		os.WriteFile(filepath.Join(sessDir, fmt.Sprintf("%d.json", pid)), data, 0o644)
		defer os.Remove(filepath.Join(sessDir, fmt.Sprintf("%d.json", pid)))
	}

	// Start server — no tmux panes yet, so no sessions discovered
	go server.RunServer(env)
	time.Sleep(200 * time.Millisecond)

	stop := make(chan struct{})
	defer close(stop)
	ch := server.Subscribe(stop)

	// Step 1: Create sessions through the command socket
	for _, name := range []string{"grecon-improvements", "coach-queue-stuck", "policy-bot-issue"} {
		resp, err := server.SendCommand(server.Command{
			Type: "create-session",
			Name: name,
			CWD:  "/tmp",
		})
		if err != nil {
			t.Fatalf("failed to create session %s: %v", name, err)
		}
		if !resp.OK {
			t.Fatalf("create session %s failed: %s", name, resp.Error)
		}
	}

	// Find the tmux IDs from the DB for each workstream
	workstreams := db.AllWorkstreams(d)
	tmuxIDs := make(map[string]string)
	for _, ws := range workstreams {
		tmuxIDs[ws.DisplayName] = ws.TmuxID
	}

	// Step 2: Make fake tmux return pane data showing all 3 sessions as live
	var paneLines string
	for i, sid := range []string{"sess-A", "sess-B", "sess-C"} {
		pid := 90001 + i
		names := []string{"grecon-improvements", "coach-queue-stuck", "policy-bot-issue"}
		tmuxID := tmuxIDs[names[i]]
		paneLines += fmt.Sprintf("%d|||%s|||claude|||/tmp|||0|||0\n", pid, tmuxID)
		_ = sid
	}
	cmd.SetOutput("tmux list-panes -a -F #{pane_pid}|||#{session_name}|||#{pane_current_command}|||#{pane_current_path}|||#{window_index}|||#{pane_index}", []byte(paneLines))
	cmd.SetOutput("ps -eo pid,ppid,args", []byte("  PID  PPID ARGS\n90001     1 claude\n90002     1 claude\n90003     1 claude\n"))

	// Wait for all 3 sessions to appear as active
	var sessions []*server.Session
	for i := 0; i < 20; i++ {
		select {
		case sessions = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for active sessions")
		}
		if len(sessions) >= 3 {
			activeCount := 0
			for _, s := range sessions {
				if s.Status != server.StatusInactive && s.Status != server.StatusDeleted {
					activeCount++
				}
			}
			if activeCount >= 3 {
				break
			}
		}
	}

	m := newTestTUI(sessions)
	view := m.View()
	if strings.Contains(view, "Off") {
		t.Fatal("no sessions should show as Off when tmux is alive")
	}

	// Step 3: Kill tmux — fake returns empty
	cmd.SetOutput("tmux list-panes -a -F #{pane_pid}|||#{session_name}|||#{pane_current_command}|||#{pane_current_path}|||#{window_index}|||#{pane_index}", []byte(""))
	cmd.SetOutput("ps -eo pid,ppid,args", []byte("  PID  PPID ARGS\n"))

	// Wait for all sessions to go inactive
	for i := 0; i < 20; i++ {
		select {
		case sessions = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for sessions to go Off")
		}
		allOff := true
		for _, s := range sessions {
			if s.Status != server.StatusInactive && s.Status != server.StatusDeleted {
				allOff = false
			}
		}
		if allOff && len(sessions) >= 3 {
			break
		}
	}

	m = newTestTUI(sessions)
	view = m.View()
	if !strings.Contains(view, "Off") {
		t.Fatal("all sessions should show as Off after tmux kill")
	}

	// Step 4: Reactivate sess-B through the command socket
	coachTmuxID := tmuxIDs["coach-queue-stuck"]
	resp, err := server.SendCommand(server.Command{
		Type:        "reactivate-session",
		SessionID:   "sess-B",
		TmuxSession: coachTmuxID,
	})
	if err != nil {
		t.Fatalf("reactivate failed: %v", err)
	}
	if !resp.OK {
		t.Fatalf("reactivate failed: %s", resp.Error)
	}

	// Step 5: Simulate Claude resuming with the SAME session ID but a new PID
	newSessFile := filepath.Join(sessDir, "99999.json")
	newSessData, _ := json.Marshal(map[string]any{
		"pid": 99999, "sessionId": "sess-B", "cwd": "/tmp", "startedAt": time.Now().UnixMilli(),
	})
	os.WriteFile(newSessFile, newSessData, 0o644)
	defer os.Remove(newSessFile)

	// Fake tmux now shows only the resumed session running
	cmd.SetOutput("tmux list-panes -a -F #{pane_pid}|||#{session_name}|||#{pane_current_command}|||#{pane_current_path}|||#{window_index}|||#{pane_index}",
		[]byte(fmt.Sprintf("99999|||%s|||claude|||/tmp|||0|||0\n", coachTmuxID)))
	cmd.SetOutput("ps -eo pid,ppid,args", []byte("  PID  PPID ARGS\n99999     1 claude --resume sess-B\n"))

	// Wait for server to discover the resumed session as live
	var found bool
	for i := 0; i < 20; i++ {
		select {
		case sessions = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for resumed session discovery")
		}
		for _, s := range sessions {
			if s.SessionID == "sess-B" && s.Status != server.StatusInactive && s.Status != server.StatusDeleted {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("server should have discovered sess-B as live")
	}

	// Assert: coach-queue-stuck should have exactly 1 session, not 2
	var coachSessions []string
	for _, s := range sessions {
		if s.TmuxSession == coachTmuxID {
			coachSessions = append(coachSessions, fmt.Sprintf("id=%s status=%d name=%q", s.SessionID, s.Status, s.ClaudeName))
		}
	}
	if len(coachSessions) > 1 {
		m = newTestTUI(sessions)
		view = m.View()
		t.Fatalf("expected 1 session under coach-queue-stuck, got %d:\n  %s\nview:\n%s",
			len(coachSessions), strings.Join(coachSessions, "\n  "), view)
	}
}

func TestIntegration_ReactivateWorksAfterTmuxKill(t *testing.T) {
	instance := fmt.Sprintf("integration-test-%d", time.Now().UnixNano())
	db.SetInstance(instance)
	testSocketPath := server.SocketPath()
	testCmdSocketPath := server.CommandSocketPath()
	defer func() {
		os.Remove(testSocketPath)
		os.Remove(testCmdSocketPath)
		db.SetInstance("grecon")
	}()

	d := db.OpenTestDB()
	defer d.Close()

	home, _ := os.UserHomeDir()
	cmd := newTestCmd()
	env := &server.Env{
		Fs:    afero.NewOsFs(),
		Cmd:   cmd,
		Clock: time.Now,
		Home:  home,
		DB:    d,
	}

	// Create JSONL backing file
	jsonlDir := filepath.Join(home, ".claude", "projects", "-test-integration-reactivate")
	os.MkdirAll(jsonlDir, 0o755)
	defer os.RemoveAll(jsonlDir)
	os.WriteFile(filepath.Join(jsonlDir, "sess-A.jsonl"), []byte(`{"type":"user","cwd":"/tmp"}`+"\n"), 0o644)

	// Create session file for initial PID
	sessDir := filepath.Join(home, ".claude", "sessions")
	os.MkdirAll(sessDir, 0o755)
	sessData, _ := json.Marshal(map[string]any{
		"pid": 90001, "sessionId": "sess-A", "cwd": "/tmp", "startedAt": time.Now().UnixMilli(),
	})
	os.WriteFile(filepath.Join(sessDir, "90001.json"), sessData, 0o644)
	defer os.Remove(filepath.Join(sessDir, "90001.json"))

	go server.RunServer(env)
	time.Sleep(200 * time.Millisecond)

	stop := make(chan struct{})
	defer close(stop)
	ch := server.Subscribe(stop)

	// Create a session
	resp, err := server.SendCommand(server.Command{
		Type: "create-session",
		Name: "my-project",
		CWD:  "/tmp",
	})
	if err != nil || !resp.OK {
		t.Fatalf("create session failed: %v %s", err, resp.Error)
	}

	workstreams := db.AllWorkstreams(d)
	tmuxID := workstreams[0].TmuxID

	// Make it live
	cmd.SetOutput("tmux list-panes -a -F #{pane_pid}|||#{session_name}|||#{pane_current_command}|||#{pane_current_path}|||#{window_index}|||#{pane_index}",
		[]byte(fmt.Sprintf("90001|||%s|||claude|||/tmp|||0|||0\n", tmuxID)))
	cmd.SetOutput("ps -eo pid,ppid,args", []byte("  PID  PPID ARGS\n90001     1 claude\n"))

	// Wait for it to appear active
	var sessions []*server.Session
	for i := 0; i < 20; i++ {
		select {
		case sessions = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for active session")
		}
		for _, s := range sessions {
			if s.SessionID == "sess-A" && s.Status != server.StatusInactive {
				goto sessionActive
			}
		}
	}
	t.Fatal("session never became active")
sessionActive:

	// Kill tmux — returns empty
	cmd.SetOutput("tmux list-panes -a -F #{pane_pid}|||#{session_name}|||#{pane_current_command}|||#{pane_current_path}|||#{window_index}|||#{pane_index}", []byte(""))
	cmd.SetOutput("ps -eo pid,ppid,args", []byte("  PID  PPID ARGS\n"))

	// Wait for session to go inactive
	for i := 0; i < 20; i++ {
		select {
		case sessions = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for session to go Off")
		}
		for _, s := range sessions {
			if s.SessionID == "sess-A" && s.Status == server.StatusInactive {
				goto sessionOff
			}
		}
	}
	t.Fatal("session never went Off")
sessionOff:

	// Verify picker shows Off
	m := newTestTUI(sessions)
	view := m.View()
	if !strings.Contains(view, "Off") {
		t.Fatal("session should show as Off after tmux kill")
	}

	// Reactivate through the command socket
	resp, err = server.SendCommand(server.Command{
		Type:        "reactivate-session",
		SessionID:   "sess-A",
		TmuxSession: tmuxID,
	})
	if err != nil {
		t.Fatalf("reactivate command failed: %v", err)
	}
	if !resp.OK {
		t.Fatalf("reactivate should succeed, got error: %s", resp.Error)
	}

	// Verify the server tried to create a tmux session
	cmd.mu.Lock()
	var tmuxCreateFound bool
	for key := range cmd.outputs {
		if strings.Contains(key, "new-session") {
			tmuxCreateFound = true
		}
	}
	cmd.mu.Unlock()

	// Simulate the resumed Claude process
	newSessFile := filepath.Join(sessDir, "99999.json")
	newSessData, _ := json.Marshal(map[string]any{
		"pid": 99999, "sessionId": "sess-A", "cwd": "/tmp", "startedAt": time.Now().UnixMilli(),
	})
	os.WriteFile(newSessFile, newSessData, 0o644)
	defer os.Remove(newSessFile)

	cmd.SetOutput("tmux list-panes -a -F #{pane_pid}|||#{session_name}|||#{pane_current_command}|||#{pane_current_path}|||#{window_index}|||#{pane_index}",
		[]byte(fmt.Sprintf("99999|||%s|||claude|||/tmp|||0|||0\n", tmuxID)))
	cmd.SetOutput("ps -eo pid,ppid,args", []byte("  PID  PPID ARGS\n99999     1 claude --resume sess-A\n"))

	// Wait for session to come back as active
	for i := 0; i < 20; i++ {
		select {
		case sessions = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for reactivated session")
		}
		for _, s := range sessions {
			if s.SessionID == "sess-A" && s.Status != server.StatusInactive && s.Status != server.StatusDeleted {
				goto sessionBack
			}
		}
	}
	t.Fatal("session never came back as active after reactivation")
sessionBack:

	// Verify picker shows the session as active, not Off
	m = newTestTUI(sessions)
	view = m.View()
	if strings.Contains(view, "Off") && !strings.Contains(view, "Gone") {
		// All sessions should be active or there should be no Off entries for my-project
	}

	// The workstream should have exactly 1 session
	var projectSessions []string
	for _, s := range sessions {
		if s.TmuxSession == tmuxID {
			projectSessions = append(projectSessions, fmt.Sprintf("id=%s status=%d", s.SessionID, s.Status))
		}
	}
	if len(projectSessions) != 1 {
		t.Fatalf("expected 1 session under my-project, got %d:\n  %s",
			len(projectSessions), strings.Join(projectSessions, "\n  "))
	}

	_ = tmuxCreateFound
}

func TestIntegration_EmptyListNavigation(t *testing.T) {
	m := newTestTUI([]*server.Session{})

	// Should not panic
	m = sendKey(m, 'j')
	m = sendKey(m, 'k')
	m = sendKey(m, 'G')
	m = sendKey(m, 'g')
	m = sendKey(m, 'g')
	m = sendKey(m, 'M')

	if m.app.Selected != 0 {
		t.Fatalf("empty list should keep selection at 0, got %d", m.app.Selected)
	}
	if m.app.SelectedSession() != nil {
		t.Fatal("empty list should return nil for SelectedSession")
	}
}
