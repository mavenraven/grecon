package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ResumeEntry struct {
	SessionID  string
	CWD        string
	Branch     string
	Model      string
	Tokens     uint64
	LastActive string // RFC3339
	ProjectDir string
}

func findResumableSessions() []ResumeEntry {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	liveIDs := getLiveSessionIDs()
	projectsDir := filepath.Join(home, ".claude", "projects")
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}

	var entries []ResumeEntry
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, dir.Name())
		cwd := decodeProjectPath(dirPath)
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, file := range files {
			if filepath.Ext(file.Name()) != ".jsonl" || file.IsDir() {
				continue
			}
			path := filepath.Join(dirPath, file.Name())
			sessionID := strings.TrimSuffix(file.Name(), ".jsonl")

			if liveIDs[sessionID] {
				continue
			}

			mtimeMs := fileMtimeMs(path)
			summary := readJSONLSummary(path)
			if summary.tokens == 0 {
				continue
			}

			entries = append(entries, ResumeEntry{
				SessionID:  sessionID,
				CWD:        cwd,
				Branch:     summary.branch,
				Model:      summary.model,
				Tokens:     summary.tokens,
				LastActive: formatEpochMs(mtimeMs),
				ProjectDir: dirPath,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastActive > entries[j].LastActive
	})
	return entries
}

func getLiveSessionIDs() map[string]bool {
	sessions := tryFetch()
	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.SessionID] = true
	}
	return ids
}

func fileMtimeMs(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return uint64(info.ModTime().UnixMilli())
}

func formatEpochMs(ms uint64) string {
	t := time.UnixMilli(int64(ms))
	return t.UTC().Format(time.RFC3339)
}

type jsonlSummary struct {
	model  string
	branch string
	tokens uint64
}

func readJSONLSummary(path string) jsonlSummary {
	f, err := os.Open(path)
	if err != nil {
		return jsonlSummary{}
	}
	defer f.Close()

	stat, _ := f.Stat()
	size := stat.Size()
	const tailBytes int64 = 1024 * 1024
	if size > tailBytes {
		f.Seek(size-tailBytes, io.SeekStart)
		// Discard partial first line
		reader := bufio.NewReader(f)
		reader.ReadString('\n')
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	const tailLines = 50
	var ring []string

	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			if len(ring) >= tailLines {
				ring = ring[1:]
			}
			ring = append(ring, line)
		}
		if err != nil {
			break
		}
	}

	var model, branch string
	var inputTokens, outputTokens uint64

	for i := len(ring) - 1; i >= 0; i-- {
		line := ring[i]

		if branch == "" && strings.Contains(line, `"gitBranch"`) {
			var v map[string]interface{}
			if json.Unmarshal([]byte(line), &v) == nil {
				if b, ok := v["gitBranch"].(string); ok {
					branch = b
				}
			}
		}

		if strings.Contains(line, `"type":"assistant"`) {
			var v map[string]interface{}
			if json.Unmarshal([]byte(line), &v) != nil {
				continue
			}
			msg, _ := v["message"].(map[string]interface{})
			if msg == nil {
				continue
			}
			if model == "" {
				if m, ok := msg["model"].(string); ok {
					model = m
				}
			}
			if inputTokens == 0 {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					it, _ := usage["input_tokens"].(float64)
					cc, _ := usage["cache_creation_input_tokens"].(float64)
					cr, _ := usage["cache_read_input_tokens"].(float64)
					ot, _ := usage["output_tokens"].(float64)
					inputTokens = uint64(it) + uint64(cc) + uint64(cr)
					outputTokens = uint64(ot)
				}
			}
			if model != "" && inputTokens > 0 && branch != "" {
				break
			}
		}
	}

	return jsonlSummary{
		model:  model,
		branch: branch,
		tokens: inputTokens + outputTokens,
	}
}

func dirName(path string) string {
	return filepath.Base(path)
}

func deleteSession(sessionID, projectDir, cwd string) {
	os.Remove(filepath.Join(projectDir, sessionID+".jsonl"))
	os.RemoveAll(filepath.Join(projectDir, sessionID))

	if home, err := os.UserHomeDir(); err == nil {
		claude := filepath.Join(home, ".claude")
		for _, subdir := range []string{"file-history", "tasks", "debug", "plans"} {
			os.RemoveAll(filepath.Join(claude, subdir, sessionID))
		}
		os.Remove(filepath.Join(home, ".recon", "sessions", sessionID))
	}

	if idx := strings.Index(cwd, "/.claude/worktrees/"); idx >= 0 {
		repoRoot := cwd[:idx]
		execRun("git", "-C", repoRoot, "worktree", "remove", "--force", cwd)
		execRun("git", "-C", repoRoot, "worktree", "prune")
	}
}

func execRun(name string, args ...string) {
	exec.Command(name, args...).Run()
}

func formatRelative(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return ts
		}
	}
	diff := time.Since(t)
	if diff.Minutes() < 1 {
		return "just now"
	} else if diff.Minutes() < 60 {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	} else if diff.Hours() < 24 {
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
}

// --- Resume picker TUI ---

type resumeModel struct {
	entries       []ResumeEntry
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
		entries: findResumableSessions(),
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
					deleteSession(e.SessionID, e.ProjectDir, e.CWD)
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
				m.result = &resumeResult{
					sessionID: e.SessionID,
					name:      dirName(e.CWD),
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

	innerW := w - 2 // left/right border

	colNum := 4
	colName := 28
	colModel := 12
	colContext := 14
	colActivity := 14

	// Dynamic git column width based on actual content (matching Rust)
	gitColWidth := 10
	for _, e := range m.entries {
		project := dirName(e.CWD)
		gitLen := len(project)
		if e.Branch != "" {
			gitLen += 2 + len(e.Branch) // "project::branch"
		}
		if gitLen > gitColWidth {
			gitColWidth = gitLen
		}
	}
	gitColWidth += 2 // padding
	remaining := innerW - colNum - colName - colModel - colContext - colActivity
	if gitColWidth > remaining {
		gitColWidth = remaining
	}
	if gitColWidth < 20 {
		gitColWidth = 20
	}

	// Top border with title
	title := " Resume Session "
	topBorder := "┌" + title
	rem := innerW - len(title)
	if rem > 0 {
		topBorder += strings.Repeat("─", rem)
	}
	topBorder += "┐"
	b.WriteString(topBorder)
	b.WriteString("\n")

	// Content height: total - top border - bottom border - footer
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
		// Header row
		header := buildRow([]colSpec{
			{colNum, " # "},
			{colName, "Session"},
			{gitColWidth, "Git(Project::Branch)"},
			{colModel, "Model"},
			{colContext, "Context"},
			{colActivity, "Last Active"},
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
			name := loadSessionName(e.SessionID)
			if name == "" {
				if len(e.SessionID) > 8 {
					name = e.SessionID[:8]
				} else {
					name = e.SessionID
				}
			}

			project := dirName(e.CWD)
			gitCol := project
			if e.Branch != "" {
				gitCol = project + dimStyle.Render("::") + greenStyle.Render(e.Branch)
			}

			modelDisplay := "—"
			if e.Model != "" {
				modelDisplay = modelDisplayName(e.Model)
			}

			window := uint64(200_000)
			if e.Model != "" {
				window = modelContextWindow(e.Model)
			}
			tokens := fmt.Sprintf("%dk / %s", e.Tokens/1000, formatWindow(window))
			lastActive := formatRelative(e.LastActive)

			row := padCol(fmt.Sprintf(" %d ", i+1), colNum) +
				padCol(name, colName) +
				padCol(gitCol, gitColWidth) +
				padCol(modelDisplay, colModel) +
				padCol(tokens, colContext) +
				padCol(lastActive, colActivity)

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

	// Fill empty rows
	emptyRow := strings.Repeat(" ", innerW)
	for i := 0; i < contentHeight; i++ {
		b.WriteString("│")
		b.WriteString(emptyRow)
		b.WriteString("│\n")
	}

	// Bottom border
	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerW))
	b.WriteString("┘\n")

	// Footer
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

func runResumePicker() (string, string, bool) {
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
