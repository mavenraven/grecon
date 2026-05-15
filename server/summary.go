package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxTextForSummary = 8000
	summaryTimeout    = 30 * time.Second
)

type summaryState struct {
	mu        sync.Mutex
	summaries map[string]string
	hashes    map[string]string
	pending   map[string]bool
}

var globalSummary = &summaryState{
	summaries: make(map[string]string),
	hashes:    make(map[string]string),
	pending:   make(map[string]bool),
}

func AttachSummaries(sessions []*Session) {
	for _, s := range sessions {
		if s.JSONLPath != "" {
			attachSummary(s.SessionID, s.JSONLPath, &s.Summary)
		}

		for _, sa := range s.Subagents {
			if sa.JSONLPath != "" {
				attachSummary("sa:"+sa.AgentID, sa.JSONLPath, &sa.Summary)
			}
		}
	}
}

func attachSummary(key, jsonlPath string, target *string) {
	activity := extractRecentActivity(jsonlPath)
	hash := hashEntry(activity)

	globalSummary.mu.Lock()
	oldHash := globalSummary.hashes[key]
	isPending := globalSummary.pending[key]
	*target = globalSummary.summaries[key]

	if hash != oldHash && !isPending {
		globalSummary.hashes[key] = hash

		if activity == "" {
			globalSummary.summaries[key] = ""
			*target = ""
			globalSummary.mu.Unlock()
		} else {
			globalSummary.pending[key] = true
			globalSummary.mu.Unlock()
			go generateSummary(key, activity)
		}
	} else {
		globalSummary.mu.Unlock()
	}
}

type summaryEntry struct {
	Type    string      `json:"type"`
	Message *summaryMsg `json:"message,omitempty"`
}

type summaryMsg struct {
	Content []contentBlock `json:"content,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type toolInput struct {
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

func extractRecentActivity(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.Size() == 0 {
		return ""
	}

	readSize := int64(512 * 1024)
	offset := stat.Size() - readSize
	if offset < 0 {
		offset = 0
		readSize = stat.Size()
	}

	buf := make([]byte, readSize)
	n, _ := f.ReadAt(buf, offset)
	buf = buf[:n]

	lines := strings.Split(string(buf), "\n")

	lastUserIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, `"type":"user"`) {
			continue
		}
		if strings.Contains(line, `"tool_result"`) {
			continue
		}
		lastUserIdx = i
		break
	}

	if lastUserIdx < 0 {
		lastUserIdx = 0
	}

	var parts []string
	for i := lastUserIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, `"type":"assistant"`) {
			continue
		}

		var entry summaryEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Message == nil {
			continue
		}

		for _, block := range entry.Message.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			case "tool_use":
				desc := describeToolUse(block.Name, block.Input)
				if desc != "" {
					parts = append(parts, desc)
				}
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	result := strings.Join(parts, "\n")
	if len(result) > maxTextForSummary {
		result = result[len(result)-maxTextForSummary:]
	}
	return result
}

func describeToolUse(name string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return name
	}
	var input toolInput
	json.Unmarshal(raw, &input)

	switch name {
	case "Bash":
		if input.Description != "" {
			return fmt.Sprintf("Ran: %s", input.Description)
		}
		if input.Command != "" {
			cmd := input.Command
			if len(cmd) > 80 {
				cmd = cmd[:80]
			}
			return fmt.Sprintf("Ran: %s", cmd)
		}
	case "Read":
		if input.FilePath != "" {
			return fmt.Sprintf("Read %s", input.FilePath)
		}
	case "Edit":
		if input.FilePath != "" {
			return fmt.Sprintf("Edited %s", input.FilePath)
		}
	case "Write":
		if input.FilePath != "" {
			return fmt.Sprintf("Wrote %s", input.FilePath)
		}
	case "Agent":
		if input.Description != "" {
			return fmt.Sprintf("Spawned agent: %s", input.Description)
		}
		if input.Prompt != "" {
			p := input.Prompt
			if len(p) > 80 {
				p = p[:80]
			}
			return fmt.Sprintf("Spawned agent: %s", p)
		}
	}
	return name
}

var urlRe = regexp.MustCompile(`https?://\S+`)

func stripURLs(s string) string {
	return urlRe.ReplaceAllString(s, "(url)")
}

func hashEntry(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func generateSummary(sessionID, text string) {
	defer func() {
		globalSummary.mu.Lock()
		globalSummary.pending[sessionID] = false
		globalSummary.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p",
		"--model", "haiku",
		"--no-session-persistence",
		"--system-prompt", "You are a summarizer. Output one line under 80 chars. Never use tools. Never ask questions.",
	)
	cmd.Dir = os.TempDir()

	prompt := fmt.Sprintf("Reply with ONLY a one-line summary (under 80 chars) of this AI assistant activity. No tools, no questions, no commentary.\n\nActivity:\n%s", stripURLs(text))
	cmd.Stdin = strings.NewReader(prompt)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "summary for %s: %v\n", sessionID[:8], err)
		return
	}

	summary := strings.TrimSpace(out.String())
	if summary == "" {
		return
	}

	globalSummary.mu.Lock()
	globalSummary.summaries[sessionID] = summary
	globalSummary.mu.Unlock()

	fmt.Fprintf(os.Stderr, "summary for %s: %s\n", sessionID[:8], summary)
}
