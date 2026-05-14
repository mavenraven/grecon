package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type App struct {
	Sessions     []*Session
	Selected     int
	ShouldQuit   bool
	Tick         uint64
	FilterActive bool
	FilterText   string
	FilterCursor int

	mu       sync.Mutex
	latest   []*Session
	hasNew   bool
	stopChan chan struct{}
}

func NewApp() *App {
	return &App{
		stopChan: make(chan struct{}),
	}
}

func (a *App) StartBackgroundRefresh() {
	go func() {
		for {
			sessions := tryFetch()
			if sessions != nil {
				a.mu.Lock()
				a.latest = sessions
				a.hasNew = true
				a.mu.Unlock()
			}
			select {
			case <-a.stopChan:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

func (a *App) StopBackgroundRefresh() {
	select {
	case <-a.stopChan:
	default:
		close(a.stopChan)
	}
}

func (a *App) TryReceive() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hasNew {
		a.Sessions = a.latest
		a.hasNew = false
		count := len(a.FilteredIndices())
		if count == 0 {
			a.Selected = 0
		} else if a.Selected >= count {
			a.Selected = count - 1
		}
	}
}

func (a *App) Refresh() {
	a.Sessions = requireFetch()
	count := len(a.FilteredIndices())
	if count == 0 {
		a.Selected = 0
	} else if a.Selected >= count {
		a.Selected = count - 1
	}
}

func (a *App) AdvanceTick() {
	a.Tick++
}

func (a *App) FilteredIndices() []int {
	if a.FilterText == "" {
		indices := make([]int, len(a.Sessions))
		for i := range indices {
			indices[i] = i
		}
		return indices
	}
	query := strings.ToLower(a.FilterText)
	var indices []int
	for i, s := range a.Sessions {
		if strings.Contains(strings.ToLower(s.ProjectName), query) ||
			strings.Contains(strings.ToLower(s.TmuxSession), query) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (a *App) clampSelection() {
	count := len(a.FilteredIndices())
	if count == 0 {
		a.Selected = 0
	} else if a.Selected >= count {
		a.Selected = count - 1
	}
}

func (a *App) resolveSelected() int {
	indices := a.FilteredIndices()
	if a.Selected >= 0 && a.Selected < len(indices) {
		return indices[a.Selected]
	}
	return -1
}

func (a *App) HandleKey(code string, ctrl bool) {
	if a.FilterActive {
		a.handleKeyFilter(code, ctrl)
		return
	}
	if code == "tab" || code == "i" {
		a.jumpToNextInput()
		return
	}
	a.handleKeyTable(code, ctrl)
}

func (a *App) jumpToNextInput() {
	for _, s := range a.Sessions {
		if s.Status == StatusInput && s.PaneTarget != "" {
			switchToPane(s.PaneTarget)
			a.ShouldQuit = true
			return
		}
	}
}

func (a *App) handleKeyTable(code string, ctrl bool) {
	switch code {
	case "q":
		a.ShouldQuit = true
	case "esc":
		if a.FilterText != "" {
			a.FilterText = ""
			a.Selected = 0
		} else {
			a.ShouldQuit = true
		}
	case "/":
		a.FilterActive = true
		a.FilterText = ""
		a.FilterCursor = 0
		a.Selected = 0
	case "j", "down":
		count := len(a.FilteredIndices())
		if count > 0 && a.Selected+1 < count {
			a.Selected++
		}
	case "k", "up":
		if a.Selected > 0 {
			a.Selected--
		}
	case "enter":
		if idx := a.resolveSelected(); idx >= 0 {
			s := a.Sessions[idx]
			if s.PaneTarget != "" {
				switchToPane(s.PaneTarget)
				a.ShouldQuit = true
			}
		}
	case "x":
		if idx := a.resolveSelected(); idx >= 0 {
			s := a.Sessions[idx]
			if s.TmuxSession != "" {
				killSession(s.TmuxSession)
			}
		}
	}
}

func (a *App) handleKeyFilter(code string, ctrl bool) {
	switch {
	case code == "esc":
		a.FilterActive = false
		a.FilterText = ""
		a.FilterCursor = 0
		a.Selected = 0
	case code == "enter":
		indices := a.FilteredIndices()
		if len(indices) == 1 {
			s := a.Sessions[indices[0]]
			if s.PaneTarget != "" {
				switchToPane(s.PaneTarget)
				a.ShouldQuit = true
				return
			}
		}
		a.FilterActive = false
	case code == "backspace":
		runes := []rune(a.FilterText)
		if a.FilterCursor > 0 && a.FilterCursor <= len(runes) {
			runes = append(runes[:a.FilterCursor-1], runes[a.FilterCursor:]...)
			a.FilterText = string(runes)
			a.FilterCursor--
			a.clampSelection()
		}
	case code == "delete":
		runes := []rune(a.FilterText)
		if a.FilterCursor < len(runes) {
			runes = append(runes[:a.FilterCursor], runes[a.FilterCursor+1:]...)
			a.FilterText = string(runes)
			a.clampSelection()
		}
	case code == "left":
		if a.FilterCursor > 0 {
			a.FilterCursor--
		}
	case code == "right":
		runes := []rune(a.FilterText)
		if a.FilterCursor < len(runes) {
			a.FilterCursor++
		}
	case code == "home" || (ctrl && code == "a"):
		a.FilterCursor = 0
	case code == "end" || (ctrl && code == "e"):
		a.FilterCursor = len([]rune(a.FilterText))
	case ctrl && code == "u":
		a.FilterText = ""
		a.FilterCursor = 0
		a.clampSelection()
	case code == "down" || code == "j":
		count := len(a.FilteredIndices())
		if count > 0 && a.Selected+1 < count {
			a.Selected++
		}
	case code == "up" || code == "k":
		if a.Selected > 0 {
			a.Selected--
		}
	case code == "tab" || code == "i":
		a.jumpToNextInput()
	case len(code) == 1 && !ctrl:
		runes := []rune(a.FilterText)
		ch := []rune(code)[0]
		newRunes := make([]rune, 0, len(runes)+1)
		newRunes = append(newRunes, runes[:a.FilterCursor]...)
		newRunes = append(newRunes, ch)
		newRunes = append(newRunes, runes[a.FilterCursor:]...)
		a.FilterText = string(newRunes)
		a.FilterCursor++
		a.clampSelection()
	}
}

func (a *App) ToJSON(tagFilters []string) string {
	type filter struct{ k, v string }
	var filters []filter
	for _, t := range tagFilters {
		if k, v, ok := strings.Cut(t, ":"); ok {
			filters = append(filters, filter{k, v})
		}
	}

	var result []map[string]interface{}
	for i, s := range a.Sessions {
		match := true
		for _, f := range filters {
			if v, ok := s.Tags[f.k]; !ok || v != f.v {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		result = append(result, map[string]interface{}{
			"index":               i + 1,
			"session_id":          s.SessionID,
			"project_name":        s.ProjectName,
			"branch":              s.Branch,
			"cwd":                 s.CWD,
			"room_id":             s.RoomID(),
			"relative_dir":        s.RelativeDir,
			"tmux_session":        s.TmuxSession,
			"pane_target":         s.PaneTarget,
			"model":               s.Model,
			"model_display":       s.ModelDisplay(),
			"total_input_tokens":  s.TotalInputTokens,
			"total_output_tokens": s.TotalOutputTokens,
			"context_display":     s.TokenDisplay(),
			"token_ratio":         s.TokenRatio(),
			"status":              s.Status.Label(),
			"pid":                 s.PID,
			"last_activity":       s.LastActivity,
			"started_at":          s.StartedAt,
			"tags":                s.Tags,
			"subagent_count":      s.SubagentCount,
		})
	}

	out, err := json.MarshalIndent(map[string]interface{}{"sessions": result}, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(out)
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func formatTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return ts
		}
	}
	diff := time.Since(t)

	if diff.Seconds() < 60 {
		return "< 1m"
	} else if diff.Minutes() < 60 {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	} else if diff.Hours() < 24 {
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	}
	return t.Local().Format("Jan 02 15:04")
}
