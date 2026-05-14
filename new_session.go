package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type field int

const (
	fieldName field = iota
	fieldCWD
	fieldWorktree
)

type newSessionModel struct {
	name      string
	cwd       string
	worktree  bool
	cursorPos int
	active    field
	result    *string // nil = still running, empty = cancelled, non-empty = session name
	width     int
	height    int
}

func newNewSessionModel() newSessionModel {
	name, cwd := defaultNewSessionInfo()
	return newSessionModel{
		name:      name,
		cwd:       cwd,
		cursorPos: len(name),
		active:    fieldName,
	}
}

func (m newSessionModel) activeText() string {
	switch m.active {
	case fieldName:
		return m.name
	case fieldCWD:
		return m.cwd
	default:
		return m.name
	}
}

func (m *newSessionModel) submit() {
	if strings.TrimSpace(m.name) == "" {
		return
	}
	cwd := strings.TrimSpace(m.cwd)
	if cwd == "" {
		cwd = "."
	}
	if strings.HasPrefix(cwd, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = home + cwd[1:]
		}
	}
	name, err := createSession(strings.TrimSpace(m.name), cwd, nil, nil, m.worktree)
	if err != nil {
		empty := ""
		m.result = &empty
	} else {
		m.result = &name
	}
}

func (m newSessionModel) Init() tea.Cmd {
	return nil
}

func (m newSessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.active == fieldWorktree {
			switch msg.String() {
			case "esc":
				empty := ""
				m.result = &empty
				return m, tea.Quit
			case " ":
				m.worktree = !m.worktree
			case "enter":
				m.submit()
				return m, tea.Quit
			case "tab", "down":
				m.active = fieldName
				m.cursorPos = len(m.name)
			case "shift+tab", "up":
				m.active = fieldCWD
				m.cursorPos = len(m.cwd)
			}
			return m, nil
		}

		switch msg.String() {
		case "esc":
			empty := ""
			m.result = &empty
			return m, tea.Quit
		case "enter":
			if m.active == fieldName {
				if strings.TrimSpace(m.name) == "" {
					return m, nil
				}
				m.active = fieldCWD
				m.cursorPos = len(m.cwd)
				return m, nil
			}
			m.submit()
			return m, tea.Quit
		case "tab", "down":
			switch m.active {
			case fieldName:
				m.active = fieldCWD
				m.cursorPos = len(m.cwd)
			case fieldCWD:
				m.active = fieldWorktree
			}
		case "shift+tab", "up":
			switch m.active {
			case fieldName:
				m.active = fieldWorktree
			case fieldCWD:
				m.active = fieldName
				m.cursorPos = len(m.name)
			}
		case "backspace":
			if m.cursorPos > 0 {
				switch m.active {
				case fieldName:
					m.name = m.name[:m.cursorPos-1] + m.name[m.cursorPos:]
				case fieldCWD:
					m.cwd = m.cwd[:m.cursorPos-1] + m.cwd[m.cursorPos:]
				}
				m.cursorPos--
			}
		case "delete":
			text := m.activeText()
			if m.cursorPos < len(text) {
				switch m.active {
				case fieldName:
					m.name = m.name[:m.cursorPos] + m.name[m.cursorPos+1:]
				case fieldCWD:
					m.cwd = m.cwd[:m.cursorPos] + m.cwd[m.cursorPos+1:]
				}
			}
		case "left":
			if m.cursorPos > 0 {
				m.cursorPos--
			}
		case "right":
			if m.cursorPos < len(m.activeText()) {
				m.cursorPos++
			}
		case "home", "ctrl+a":
			m.cursorPos = 0
		case "end", "ctrl+e":
			m.cursorPos = len(m.activeText())
		case "ctrl+u":
			switch m.active {
			case fieldName:
				m.name = ""
			case fieldCWD:
				m.cwd = ""
			}
			m.cursorPos = 0
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				ch := string(msg.Runes[0])
				switch m.active {
				case fieldName:
					m.name = m.name[:m.cursorPos] + ch + m.name[m.cursorPos:]
				case fieldCWD:
					m.cwd = m.cwd[:m.cursorPos] + ch + m.cwd[m.cursorPos:]
				}
				m.cursorPos++
			}
		}
	}
	return m, nil
}

func (m newSessionModel) View() string {
	var b strings.Builder

	nameBorder := dimStyle
	if m.active == fieldName {
		nameBorder = cyanStyle
	}
	cwdBorder := dimStyle
	if m.active == fieldCWD {
		cwdBorder = cyanStyle
	}

	// Name field
	b.WriteString(nameBorder.Render("┌─ Name ─" + strings.Repeat("─", max(0, m.width-10)) + "┐"))
	b.WriteString("\n")
	b.WriteString(nameBorder.Render("│") + " " + m.name + strings.Repeat(" ", max(0, m.width-len(m.name)-4)) + " " + nameBorder.Render("│"))
	b.WriteString("\n")
	b.WriteString(nameBorder.Render("└" + strings.Repeat("─", max(0, m.width-2)) + "┘"))
	b.WriteString("\n")

	// CWD field
	b.WriteString(cwdBorder.Render("┌─ Directory ─" + strings.Repeat("─", max(0, m.width-15)) + "┐"))
	b.WriteString("\n")
	b.WriteString(cwdBorder.Render("│") + " " + m.cwd + strings.Repeat(" ", max(0, m.width-len(m.cwd)-4)) + " " + cwdBorder.Render("│"))
	b.WriteString("\n")
	b.WriteString(cwdBorder.Render("└" + strings.Repeat("─", max(0, m.width-2)) + "┘"))
	b.WriteString("\n")

	// Worktree toggle
	marker := " "
	if m.worktree {
		marker = "x"
	}
	wtStyle := dimStyle
	if m.active == fieldWorktree {
		wtStyle = cyanStyle
	}
	b.WriteString(wtStyle.Render(fmt.Sprintf(" [%s] Worktree", marker)))
	b.WriteString("\n")

	// Hints
	switch m.active {
	case fieldName:
		b.WriteString(" " + cyanStyle.Render("Enter") + " next  " +
			cyanStyle.Render("Tab") + " switch  " +
			cyanStyle.Render("Esc") + " cancel")
	case fieldCWD:
		b.WriteString(" " + cyanStyle.Render("Enter") + " create  " +
			cyanStyle.Render("Tab") + " switch  " +
			cyanStyle.Render("Esc") + " cancel")
	case fieldWorktree:
		b.WriteString(" " + cyanStyle.Render("Space") + " toggle  " +
			cyanStyle.Render("Enter") + " create  " +
			cyanStyle.Render("Tab") + " switch  " +
			cyanStyle.Render("Esc") + " cancel")
	}

	return b.String()
}

func runNewSessionForm() (string, bool) {
	m := newNewSessionModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return "", false
	}
	rm := result.(newSessionModel)
	if rm.result != nil && *rm.result != "" {
		return *rm.result, true
	}
	return "", false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
