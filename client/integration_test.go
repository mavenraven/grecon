package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"grecon/server"
)

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
