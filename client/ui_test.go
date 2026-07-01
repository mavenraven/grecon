package client

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func escMsg() tea.Msg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

func TestPickerN_OpensNewForm(t *testing.T) {
	m := tuiModel{app: NewApp(), width: 80, height: 24}

	result, _ := m.Update(keyMsg('n'))
	tm := result.(tuiModel)

	if tm.newForm == nil {
		t.Fatal("pressing n should open the new session form")
	}
}

func TestPickerN_EscReturnsToPickerFromForm(t *testing.T) {
	m := tuiModel{app: NewApp(), width: 80, height: 24}

	result, _ := m.Update(keyMsg('n'))
	tm := result.(tuiModel)
	if tm.newForm == nil {
		t.Fatal("form should be open")
	}

	result, _ = tm.Update(escMsg())
	tm = result.(tuiModel)

	if tm.newForm != nil {
		t.Fatal("Esc should close the form and return to picker")
	}
	if tm.app.ShouldQuit {
		t.Fatal("Esc from form should not quit the app")
	}
}

func TestPickerN_IgnoredDuringFilter(t *testing.T) {
	m := tuiModel{app: NewApp(), width: 80, height: 24}
	m.app.FilterActive = true

	result, _ := m.Update(keyMsg('n'))
	tm := result.(tuiModel)

	if tm.newForm != nil {
		t.Fatal("n should be ignored when filter is active")
	}
}

func TestNewForm_ViewRendersWithoutPanic(t *testing.T) {
	m := tuiModel{app: NewApp(), width: 80, height: 24}

	result, _ := m.Update(keyMsg('n'))
	tm := result.(tuiModel)

	view := tm.View()
	if view == "" {
		t.Fatal("form view should not be empty")
	}
}
