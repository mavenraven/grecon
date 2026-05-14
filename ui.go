package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	colorCyan     = lipgloss.Color("6")
	colorGreen    = lipgloss.Color("2")
	colorYellow   = lipgloss.Color("3")
	colorBlue     = lipgloss.Color("4")
	colorRed      = lipgloss.Color("1")
	colorDarkGray = lipgloss.Color("8")

	headerStyle = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(colorDarkGray)
	cyanStyle   = lipgloss.NewStyle().Foreground(colorCyan)
	greenStyle  = lipgloss.NewStyle().Foreground(colorGreen)

	selectedBg      = lipgloss.Color("240")
	inputBg         = lipgloss.Color("#322800")
	inputSelectedBg = lipgloss.Color("#504100")
)

type tickMsg struct{}

type tuiModel struct {
	app    *App
	width  int
	height int
}

func newTUIModel() tuiModel {
	app := NewApp()
	app.Refresh()
	app.StartBackgroundRefresh()
	return tuiModel{app: app, width: 80, height: 24}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(_ time.Time) tea.Msg { return tickMsg{} })
}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(_ time.Time) tea.Msg { return tickMsg{} })
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.app.TryReceive()
		m.app.AdvanceTick()
		return m, tickCmd()

	case tea.KeyMsg:
		code, ctrl := translateKey(msg)
		m.app.HandleKey(code, ctrl)
		if m.app.ShouldQuit {
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func translateKey(msg tea.KeyMsg) (string, bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return "esc", false
	case tea.KeyEnter:
		return "enter", false
	case tea.KeyTab:
		return "tab", false
	case tea.KeyUp:
		return "up", false
	case tea.KeyDown:
		return "down", false
	case tea.KeyLeft:
		return "left", false
	case tea.KeyRight:
		return "right", false
	case tea.KeyBackspace:
		return "backspace", false
	case tea.KeyDelete:
		return "delete", false
	case tea.KeyHome:
		return "home", false
	case tea.KeyEnd:
		return "end", false
	case tea.KeyCtrlA:
		return "a", true
	case tea.KeyCtrlE:
		return "e", true
	case tea.KeyCtrlU:
		return "u", true
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			return string(msg.Runes), false
		}
	}
	return msg.String(), false
}

func (m tuiModel) View() string {
	var b strings.Builder

	showSearch := m.app.FilterActive || m.app.FilterText != ""
	// Total height: border top + header + rows + border bottom + (search?) + footer
	tableContentHeight := m.height - 2 - 1 // -2 for borders, -1 for footer
	if showSearch {
		tableContentHeight-- // search bar
	}
	if tableContentHeight < 2 {
		tableContentHeight = 2
	}

	renderTable(&b, m.app, m.width, tableContentHeight)
	if showSearch {
		renderSearchBar(&b, m.app, m.width)
	}
	renderFooter(&b, m.app, m.width)

	return b.String()
}

func renderTable(b *strings.Builder, app *App, width, contentHeight int) {
	innerW := width - 2 // subtract left/right border chars

	// Column widths matching Rust exactly
	colNum := 4
	colSession := 24
	colDir := 20
	colStatus := 10
	colModel := 12
	colContext := 14
	colActivity := 14
	colProject := innerW - colNum - colSession - colDir - colStatus - colModel - colContext - colActivity
	if colProject < 20 {
		colProject = 20
	}

	// Top border with title
	title := " grecon — Claude Code Sessions "
	topBorder := "┌" + title
	remaining := innerW - lipgloss.Width(title)
	if remaining > 0 {
		topBorder += strings.Repeat("─", remaining)
	}
	topBorder += "┐"
	b.WriteString(topBorder)
	b.WriteString("\n")

	// Header row
	header := buildRow([]colSpec{
		{colNum, " # "},
		{colSession, "Session"},
		{colProject, "Project"},
		{colDir, "Directory"},
		{colStatus, "Status"},
		{colModel, "Model"},
		{colContext, "Context"},
		{colActivity, "Last Activity"},
	})
	b.WriteString("│")
	b.WriteString(headerStyle.Render(fitToWidth(header, innerW)))
	b.WriteString("│\n")

	filtered := app.FilteredIndices()
	rowsAvail := contentHeight - 1 // -1 for header

	for di, realIdx := range filtered {
		if di >= rowsAvail {
			break
		}
		s := app.Sessions[realIdx]

		needBg := di == app.Selected || s.Status == StatusInput

		num := fmt.Sprintf(" %d ", realIdx+1)
		tmuxName := s.TmuxSession
		if tmuxName == "" {
			tmuxName = "—"
		}

		sessionCol := tmuxName
		if s.SubagentCount > 0 {
			sessionCol = truncPlain(tmuxName, colSession-5) + ansiColor("36", fmt.Sprintf(" [%d]", s.SubagentCount))
		}

		projectCol := s.ProjectName
		if s.RelativeDir != "" {
			projectCol += ansiColor("90", "::") + ansiColor("36", s.RelativeDir)
		}
		if s.Branch != "" {
			projectCol += ansiColor("90", "::") + ansiColor("32", s.Branch)
		}

		dirCol := ansiColor("90", truncPlain(shortenHome(s.CWD), colDir))

		var statusDot, statusLabel, statusAnsi string
		switch s.Status {
		case StatusNew:
			statusDot, statusLabel, statusAnsi = "●", "New", "34"
		case StatusWorking:
			statusDot, statusLabel, statusAnsi = "●", "Working", "32"
		case StatusIdle:
			statusDot, statusLabel, statusAnsi = "●", "Idle", "90"
		case StatusInput:
			statusDot, statusLabel, statusAnsi = "●", "Input", "33"
		}
		statusCol := ansiColor(statusAnsi, statusDot+" "+statusLabel)

		modelCol := s.ModelDisplay()

		tokenCol := s.TokenDisplay()
		tokenRatio := s.TokenRatio()
		if tokenRatio > 0.9 {
			tokenCol = ansiColor("31", tokenCol)
		} else if tokenRatio > 0.75 {
			tokenCol = ansiColor("33", tokenCol)
		}

		activity := "—"
		if s.LastActivity != "" {
			activity = formatTimestamp(s.LastActivity)
		}

		row := padCol(num, colNum) +
			padCol(sessionCol, colSession) +
			padCol(projectCol, colProject) +
			padCol(dirCol, colDir) +
			padCol(statusCol, colStatus) +
			padCol(modelCol, colModel) +
			padCol(tokenCol, colContext) +
			padCol(activity, colActivity)

		plainLen := visibleWidth(row)
		if plainLen < innerW {
			row += strings.Repeat(" ", innerW-plainLen)
		}

		if needBg {
			var bgCode string
			if s.Status == StatusInput && di == app.Selected {
				bgCode = "\x1b[48;2;80;65;0m"
			} else if s.Status == StatusInput {
				bgCode = "\x1b[48;2;50;40;0m"
			} else {
				bgCode = "\x1b[48;5;240m"
			}
			row = applyRowBg(row, bgCode)
		}

		b.WriteString("│")
		b.WriteString(row)
		b.WriteString("│\n")
	}

	// Fill empty rows
	emptyRow := strings.Repeat(" ", innerW)
	for i := len(filtered); i < rowsAvail; i++ {
		b.WriteString("│")
		b.WriteString(emptyRow)
		b.WriteString("│\n")
	}

	// Bottom border
	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerW))
	b.WriteString("┘\n")
}

func renderSearchBar(b *strings.Builder, app *App, width int) {
	line := cyanStyle.Render("/") + app.FilterText
	if !app.FilterActive && app.FilterText != "" {
		count := len(app.FilteredIndices())
		suffix := "es"
		if count == 1 {
			suffix = ""
		}
		line += dimStyle.Render(fmt.Sprintf("  (%d match%s)", count, suffix))
	}
	b.WriteString(fitToWidth(line, width))
	b.WriteString("\n")
}

func renderFooter(b *strings.Builder, app *App, width int) {
	var line string
	if app.FilterActive {
		line = cyanStyle.Render("Esc") + " clear  " +
			cyanStyle.Render("Enter") + " keep filter  " +
			cyanStyle.Render("j/k") + " navigate"
	} else {
		line = cyanStyle.Render("j/k") + " navigate  " +
			cyanStyle.Render("Enter") + " switch  " +
			cyanStyle.Render("x") + " kill  " +
			cyanStyle.Render("/") + " search  " +
			cyanStyle.Render("i") + " next input  " +
			cyanStyle.Render("q") + " quit"
	}
	b.WriteString(fitToWidth(line, width))
}

type colSpec struct {
	width int
	text  string
}

func buildRow(cols []colSpec) string {
	var b strings.Builder
	for _, c := range cols {
		b.WriteString(padCol(c.text, c.width))
	}
	return b.String()
}

// padCol truncates content to exactly width visible characters, then pads with spaces.
// Handles ANSI escape sequences correctly.
func padCol(s string, width int) string {
	s = truncAnsi(s, width)
	visLen := lipgloss.Width(s)
	if visLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visLen)
}

func fitToWidth(s string, width int) string {
	visLen := lipgloss.Width(s)
	if visLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visLen)
}

// truncAnsi truncates a string that may contain ANSI escapes to maxWidth visible characters.
// Ensures any open ANSI sequences are properly closed with a reset.
func truncAnsi(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	visCount := 0
	inEscape := false
	var result strings.Builder
	hadEscape := false

	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			hadEscape = true
			result.WriteRune(r)
			continue
		}
		if inEscape {
			result.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if visCount >= maxWidth {
			break
		}
		result.WriteRune(r)
		visCount++
	}

	out := result.String()
	if hadEscape {
		out += "\x1b[0m"
	}
	return out
}

// ansiColor wraps text with a foreground color using raw ANSI codes.
// Only sets/resets foreground — never touches background.
func ansiColor(code, text string) string {
	return "\x1b[" + code + "m" + text + "\x1b[39m"
}

// applyRowBg applies a background color to an entire row, re-applying it after
// any embedded ANSI resets so the background persists across styled columns.
func applyRowBg(row, bgCode string) string {
	// Replace all forms of reset/background-clear that lipgloss might emit
	row = strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+bgCode)
	row = strings.ReplaceAll(row, "\x1b[m", "\x1b[m"+bgCode)
	row = strings.ReplaceAll(row, "\x1b[49m", bgCode)
	row = strings.ReplaceAll(row, "\x1b[39;49m", "\x1b[39m"+bgCode)
	row = strings.ReplaceAll(row, "\x1b[49;39m", "\x1b[39m"+bgCode)
	return bgCode + row + "\x1b[0m"
}

// visibleWidth counts visible characters, skipping ANSI escape sequences.
func visibleWidth(s string) int {
	count := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		count++
	}
	return count
}

func truncPlain(s string, maxWidth int) string {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth <= 0 {
		return ""
	}
	return string(runes[:maxWidth])
}
