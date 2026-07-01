package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func writeFile(fs afero.Fs, path, content string) {
	afero.WriteFile(fs, path, []byte(content), 0o644)
}

func assistantMsg(model string, inputTokens, outputTokens, cacheCreate, cacheRead uint64) string {
	msg := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-07-01T12:00:00Z",
		"message": map[string]any{
			"model": model,
			"usage": map[string]any{
				"input_tokens":                inputTokens,
				"output_tokens":               outputTokens,
				"cache_creation_input_tokens":  cacheCreate,
				"cache_read_input_tokens":      cacheRead,
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func userMsg(cwd string) string {
	msg := map[string]any{
		"type":      "user",
		"timestamp": "2026-07-01T12:00:01Z",
		"cwd":       cwd,
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func modelCommandMsg(modelDisplay, effort string) string {
	stdout := fmt.Sprintf("Set model to %s with %s effort", modelDisplay, effort)
	return `{"type":"user","timestamp":"2026-07-01T12:00:02Z","message":{"content":[{"type":"text","text":"<local-command-stdout>` + stdout + `</local-command-stdout>"}]}}`
}

func TestParseJSONL_TokenAccumulation(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := assistantMsg("claude-sonnet-4-6", 100, 50, 200, 300) + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	expectedInput := uint64(100 + 200 + 300)
	if info.inputTokens != expectedInput {
		t.Fatalf("expected input tokens %d (100+200+300), got %d", expectedInput, info.inputTokens)
	}
	if info.outputTokens != 50 {
		t.Fatalf("expected output tokens 50, got %d", info.outputTokens)
	}
}

func TestParseJSONL_ModelExtraction(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := assistantMsg("claude-sonnet-4-6", 100, 50, 0, 0) + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.model != "claude-sonnet-4-6" {
		t.Fatalf("expected model claude-sonnet-4-6, got %s", info.model)
	}
}

func TestParseJSONL_FileSizeCacheSkip(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := assistantMsg("claude-sonnet-4-6", 100, 50, 0, 0) + "\n"
	writeFile(fs, path, content)

	info1 := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	info2 := parseJSONL(fs, path, info1.fileSize, 999, 999, "cached-model", "high", "cached-activity", nil, nil, nil)

	if info2.inputTokens != 999 {
		t.Fatalf("should return cached input tokens when file size unchanged, got %d", info2.inputTokens)
	}
	if info2.model != "cached-model" {
		t.Fatalf("should return cached model when file size unchanged, got %s", info2.model)
	}
	if info2.fileSize != info1.fileSize {
		t.Fatalf("file size should match, got %d vs %d", info2.fileSize, info1.fileSize)
	}
}

func TestParseJSONL_IncrementalParse(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	line1 := assistantMsg("claude-sonnet-4-6", 100, 50, 0, 0)
	writeFile(fs, path, line1+"\n")

	info1 := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	line2 := assistantMsg("claude-sonnet-4-6", 200, 80, 0, 0)
	writeFile(fs, path, line1+"\n"+line2+"\n")

	info2 := parseJSONL(fs, path, info1.fileSize, info1.inputTokens, info1.outputTokens, info1.model, info1.effort, info1.lastActivity, nil, nil, nil)

	if info2.inputTokens != 200 {
		t.Fatalf("incremental parse should update to latest tokens, got %d", info2.inputTokens)
	}
}

func TestParseJSONL_CWDExtraction(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := userMsg("/home/user/project") + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.cwd != "/home/user/project" {
		t.Fatalf("expected cwd /home/user/project, got %s", info.cwd)
	}
}

func TestParseJSONL_LastActivityTimestamp(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := userMsg("/tmp") + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.lastActivity != "2026-07-01T12:00:01Z" {
		t.Fatalf("expected timestamp 2026-07-01T12:00:01Z, got %s", info.lastActivity)
	}
}

func TestParseJSONL_LineOverflow(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	normalLine := userMsg("/tmp")
	bigLine := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 11*1024*1024) + `"}}`
	afterLine := assistantMsg("claude-sonnet-4-6", 42, 10, 0, 0)

	content := normalLine + "\n" + bigLine + "\n" + afterLine + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.inputTokens != 42 {
		t.Fatalf("should skip oversized line and parse subsequent lines, got input=%d", info.inputTokens)
	}
}

func TestParseJSONL_EmptyFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/empty.jsonl"
	writeFile(fs, path, "")

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.inputTokens != 0 || info.outputTokens != 0 {
		t.Fatal("empty file should have zero tokens")
	}
}

func TestParseJSONL_MissingFile(t *testing.T) {
	fs := afero.NewMemMapFs()

	info := parseJSONL(fs, "/nonexistent.jsonl", 0, 0, 0, "", "", "prev-activity", nil, nil, nil)

	if info.lastActivity != "prev-activity" {
		t.Fatalf("missing file should return prev values, got %s", info.lastActivity)
	}
}

func TestParseJSONL_SkipsSyntheticAssistant(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	synthetic := `{"type":"assistant","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":999,"output_tokens":999}},"<synthetic>":true}`
	real := assistantMsg("claude-opus-4-6", 42, 10, 0, 0)
	content := synthetic + "\n" + real + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.inputTokens != 42 {
		t.Fatalf("should skip synthetic, got input=%d", info.inputTokens)
	}
	if info.model != "claude-opus-4-6" {
		t.Fatalf("model should be from real entry, got %s", info.model)
	}
}

func TestParseJSONL_ModelCommandFromLocalStdout(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := modelCommandMsg("Sonnet 4.6", "high") + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "", "", "", nil, nil, nil)

	if info.effort != "high" {
		t.Fatalf("expected effort 'high', got '%s'", info.effort)
	}
}

func TestParseJSONL_ModelCommandNotFromToolResult(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	// Model command inside a toolUseResult should be ignored
	msg := map[string]any{
		"type":      "user",
		"timestamp": "2026-07-01T12:00:02Z",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "toolUseResult", "text": "<local-command-stdout>Set model to Opus 4.6 with max effort</local-command-stdout>"},
			},
		},
	}
	b, _ := json.Marshal(msg)
	content := string(b) + "\n"
	writeFile(fs, path, content)

	info := parseJSONL(fs, path, 0, 0, 0, "original-model", "low", "", nil, nil, nil)

	if info.effort != "" {
		t.Fatalf("model command in toolUseResult should be ignored, effort=%s", info.effort)
	}
}
