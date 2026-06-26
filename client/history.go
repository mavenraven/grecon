package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"grecon/db"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type resumableWorkstream struct {
	WorkstreamID int64
	TmuxName     string
	Sessions     []db.ClaudeSessionInfo
}

func findResumableWorkstreams() []resumableWorkstream {
	d := db.Get()
	if d == nil {
		return nil
	}
	workstreams := db.InactiveWorkstreams(d)
	var result []resumableWorkstream
	for _, ws := range workstreams {
		if len(ws.Sessions) == 0 {
			continue
		}
		result = append(result, resumableWorkstream{
			WorkstreamID: ws.WorkstreamID,
			TmuxName:     ws.DisplayName,
			Sessions:     ws.Sessions,
		})
	}
	return result
}

func deleteSession(sessionID, projectDir, cwd string) {
	jsonlPath := filepath.Join(projectDir, sessionID+".jsonl")

	realCWD := readSessionCWD(jsonlPath)
	if realCWD == "" {
		realCWD = cwd
	}

	os.Remove(jsonlPath)
	os.RemoveAll(filepath.Join(projectDir, sessionID))

	if home, err := os.UserHomeDir(); err == nil {
		claude := filepath.Join(home, ".claude")
		for _, subdir := range []string{"file-history", "tasks", "debug", "plans"} {
			os.RemoveAll(filepath.Join(claude, subdir, sessionID))
		}
		grecon := filepath.Join(home, ".grecon")
		os.Remove(filepath.Join(grecon, "sessions", sessionID))
	}

	if d := db.Get(); d != nil {
		db.DeleteClaudeSession(d, sessionID)
	}

	if idx := strings.Index(realCWD, "/.claude/worktrees/"); idx >= 0 {
		repoRoot := realCWD[:idx]
		execRun("git", "-C", repoRoot, "worktree", "remove", "--force", realCWD)
		execRun("git", "-C", repoRoot, "worktree", "prune")
	}
}

func readSessionCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		line := scanner.Text()
		if !strings.Contains(line, `"cwd"`) {
			continue
		}
		var v map[string]interface{}
		if json.Unmarshal([]byte(line), &v) == nil {
			if cwd, ok := v["cwd"].(string); ok {
				return cwd
			}
		}
	}
	return ""
}

func execRun(name string, args ...string) {
	exec.Command(name, args...).Run()
}

// --- Resume picker TUI ---

type resumeModel struct {
	entries       []resumableWorkstream
	selected      int
	confirmDelete bool
	result        *resumeResult
	width         int
	height        int
}

type resumeResult struct {
	sessionID string
	name      string
}

func newResumeModel() resumeModel {
	return resumeModel{
		entries: findResumableWorkstreams(),
	}
}

func (m resumeModel) Init() tea.Cmd {
	return nil
}

func (m resumeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.confirmDelete {
			switch msg.String() {
			case "y":
				if m.selected < len(m.entries) {
					e := m.entries[m.selected]
					for _, cs := range e.Sessions {
						go deleteSession(cs.SessionID, "", "")
					}
					m.entries = append(m.entries[:m.selected], m.entries[m.selected+1:]...)
					if len(m.entries) == 0 {
						m.selected = 0
					} else if m.selected >= len(m.entries) {
						m.selected = len(m.entries) - 1
					}
				}
				m.confirmDelete = false
			default:
				m.confirmDelete = false
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.selected+1 < len(m.entries) {
				m.selected++
			}
		case "k", "up":
			if m.selected > 0 {
				m.selected--
			}
		case "enter":
			if len(m.entries) > 0 && m.selected < len(m.entries) {
				e := m.entries[m.selected]
				if len(e.Sessions) > 0 {
					m.result = &resumeResult{
						sessionID: e.Sessions[0].SessionID,
						name:      e.TmuxName,
					}
				}
			}
			return m, tea.Quit
		case "x":
			if len(m.entries) > 0 {
				m.confirmDelete = true
			}
		}
	}
	return m, nil
}

func (m resumeModel) View() string {
	var b strings.Builder
	w := m.width
	if w == 0 {
		w = 80
	}
	h := m.height
	if h == 0 {
		h = 24
	}

	innerW := w - 2

	colNum := 4
	colTmux := 28
	colClaude := 24
	colSummary := innerW - colNum - colTmux - colClaude
	if colSummary < 20 {
		colSummary = 20
	}

	title := " Resume Workstream "
	topBorder := "┌" + title
	rem := innerW - len(title)
	if rem > 0 {
		topBorder += strings.Repeat("─", rem)
	}
	topBorder += "┐"
	b.WriteString(topBorder)
	b.WriteString("\n")

	contentHeight := h - 3

	if len(m.entries) == 0 {
		row := dimStyle.Render("  No resumable sessions found")
		plainLen := visibleWidth(row)
		if plainLen < innerW {
			row += strings.Repeat(" ", innerW-plainLen)
		}
		b.WriteString("│")
		b.WriteString(row)
		b.WriteString("│\n")
		contentHeight--
	} else {
		header := buildRow([]colSpec{
			{colNum, " # "},
			{colTmux, "Session"},
			{colClaude, "Claude"},
			{colSummary, "Summary"},
		})
		b.WriteString("│")
		b.WriteString(headerStyle.Render(fitToWidth(header, innerW)))
		b.WriteString("│\n")
		contentHeight--

		maxRows := contentHeight
		if maxRows < 1 {
			maxRows = 20
		}
		for i, e := range m.entries {
			if i >= maxRows {
				break
			}
			tmuxName := e.TmuxName
			if tmuxName == "" {
				tmuxName = dimStyle.Render("—")
			}

			var claudeNames []string
			var summary string
			for _, cs := range e.Sessions {
				if cs.DisplayName != "" {
					claudeNames = append(claudeNames, cs.DisplayName)
				}
			}
			claudeDisplay := strings.Join(claudeNames, ", ")
			if claudeDisplay == "" {
				claudeDisplay = dimStyle.Render("—")
			}

			d := db.Get()
			if d != nil && len(e.Sessions) > 0 {
				summary = db.LoadSummaryDB(d, e.Sessions[0].SessionID)
			}

			row := padCol(fmt.Sprintf(" %d ", i+1), colNum) +
				padCol(tmuxName, colTmux) +
				padCol(claudeDisplay, colClaude) +
				padCol(summary, colSummary)

			plainLen := visibleWidth(row)
			if plainLen < innerW {
				row += strings.Repeat(" ", innerW-plainLen)
			}

			if i == m.selected {
				row = applyRowBg(row, "\x1b[48;5;240m")
			}

			b.WriteString("│")
			b.WriteString(row)
			b.WriteString("│\n")
			contentHeight--
		}
	}

	emptyRow := strings.Repeat(" ", innerW)
	for i := 0; i < contentHeight; i++ {
		b.WriteString("│")
		b.WriteString(emptyRow)
		b.WriteString("│\n")
	}

	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerW))
	b.WriteString("┘\n")

	if m.confirmDelete {
		b.WriteString(
			lipgloss.NewStyle().Foreground(colorRed).Render(" Delete session? ") +
				cyanStyle.Render("y") + " yes  " +
				cyanStyle.Render("n/Esc") + " no",
		)
	} else {
		b.WriteString(
			cyanStyle.Render("j/k") + " navigate  " +
				cyanStyle.Render("Enter") + " resume  " +
				cyanStyle.Render("x") + " delete  " +
				cyanStyle.Render("q/Esc") + " cancel",
		)
	}

	return b.String()
}

func RunResumePicker() (string, string, bool) {
	m := newResumeModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return "", "", false
	}
	rm := result.(resumeModel)
	if rm.result != nil {
		return rm.result.sessionID, rm.result.name, true
	}
	return "", "", false
}
