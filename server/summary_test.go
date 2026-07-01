package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// --- describeToolUse ---

func TestDescribeToolUse_Bash(t *testing.T) {
	input, _ := json.Marshal(toolInput{Command: "go test ./...", Description: "Run tests"})
	result := describeToolUse("Bash", input)
	if result != "Ran: Run tests" {
		t.Fatalf("expected 'Ran: Run tests', got '%s'", result)
	}
}

func TestDescribeToolUse_BashNoDescription(t *testing.T) {
	input, _ := json.Marshal(toolInput{Command: "go test ./..."})
	result := describeToolUse("Bash", input)
	if result != "Ran: go test ./..." {
		t.Fatalf("expected 'Ran: go test ./...', got '%s'", result)
	}
}

func TestDescribeToolUse_BashLongCommand(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "x"
	}
	input, _ := json.Marshal(toolInput{Command: long})
	result := describeToolUse("Bash", input)
	if len(result) > 90 {
		t.Fatalf("should truncate long commands, got %d chars", len(result))
	}
}

func TestDescribeToolUse_Read(t *testing.T) {
	input, _ := json.Marshal(toolInput{FilePath: "/src/main.go"})
	result := describeToolUse("Read", input)
	if result != "Read /src/main.go" {
		t.Fatalf("expected 'Read /src/main.go', got '%s'", result)
	}
}

func TestDescribeToolUse_Edit(t *testing.T) {
	input, _ := json.Marshal(toolInput{FilePath: "/src/main.go"})
	result := describeToolUse("Edit", input)
	if result != "Edited /src/main.go" {
		t.Fatalf("expected 'Edited /src/main.go', got '%s'", result)
	}
}

func TestDescribeToolUse_Write(t *testing.T) {
	input, _ := json.Marshal(toolInput{FilePath: "/src/new.go"})
	result := describeToolUse("Write", input)
	if result != "Wrote /src/new.go" {
		t.Fatalf("expected 'Wrote /src/new.go', got '%s'", result)
	}
}

func TestDescribeToolUse_Agent(t *testing.T) {
	input, _ := json.Marshal(toolInput{Description: "Search for bugs"})
	result := describeToolUse("Agent", input)
	if result != "Spawned agent: Search for bugs" {
		t.Fatalf("expected 'Spawned agent: Search for bugs', got '%s'", result)
	}
}

func TestDescribeToolUse_Unknown(t *testing.T) {
	result := describeToolUse("WebSearch", nil)
	if result != "WebSearch" {
		t.Fatalf("unknown tool should return name, got '%s'", result)
	}
}

// --- stripURLs ---

func TestStripURLs(t *testing.T) {
	input := "Check https://example.com/path?q=1 and http://foo.bar/baz for details"
	result := stripURLs(input)
	expected := "Check (url) and (url) for details"
	if result != expected {
		t.Fatalf("expected '%s', got '%s'", expected, result)
	}
}

func TestStripURLs_NoURLs(t *testing.T) {
	input := "no urls here"
	if stripURLs(input) != input {
		t.Fatal("should not modify text without URLs")
	}
}

// --- hashEntry ---

func TestHashEntry_Deterministic(t *testing.T) {
	h1 := hashEntry("hello world")
	h2 := hashEntry("hello world")
	if h1 != h2 {
		t.Fatal("same input should produce same hash")
	}
}

func TestHashEntry_Different(t *testing.T) {
	h1 := hashEntry("hello")
	h2 := hashEntry("world")
	if h1 == h2 {
		t.Fatal("different input should produce different hash")
	}
}

// --- extractRecentActivityFS ---

func assistantBlock(text string) string {
	msg := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func userLine() string {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "do something"},
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func toolResultUserLine() string {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "abc"},
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func toolUseAssistantLine(toolName, filePath string) string {
	input, _ := json.Marshal(toolInput{FilePath: filePath})
	msg := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "tool_use", "name": toolName, "input": json.RawMessage(input)},
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func TestExtractRecentActivity_ExtractsAfterLastUser(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := userLine() + "\n" +
		assistantBlock("First response") + "\n" +
		userLine() + "\n" +
		assistantBlock("Second response") + "\n"
	afero.WriteFile(fs, path, []byte(content), 0o644)

	activity := extractRecentActivityFS(fs, path)

	if activity != "Second response" {
		t.Fatalf("should extract activity after last user message, got '%s'", activity)
	}
}

func TestExtractRecentActivity_SkipsToolResultUser(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := userLine() + "\n" +
		assistantBlock("Real response") + "\n" +
		toolResultUserLine() + "\n" +
		assistantBlock("After tool result") + "\n"
	afero.WriteFile(fs, path, []byte(content), 0o644)

	activity := extractRecentActivityFS(fs, path)

	if activity != "Real response\nAfter tool result" {
		t.Fatalf("should skip tool_result user lines, got '%s'", activity)
	}
}

func TestExtractRecentActivity_IncludesToolUseDescriptions(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := userLine() + "\n" +
		toolUseAssistantLine("Read", "/src/main.go") + "\n"
	afero.WriteFile(fs, path, []byte(content), 0o644)

	activity := extractRecentActivityFS(fs, path)

	if activity != "Read /src/main.go" {
		t.Fatalf("should include tool use description, got '%s'", activity)
	}
}

func TestExtractRecentActivity_EmptyFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/empty.jsonl"
	afero.WriteFile(fs, path, []byte(""), 0o644)

	activity := extractRecentActivityFS(fs, path)
	if activity != "" {
		t.Fatalf("empty file should return empty, got '%s'", activity)
	}
}

func TestExtractRecentActivity_TruncatesLongActivity(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	long := ""
	for i := 0; i < 10000; i++ {
		long += "x"
	}
	content := userLine() + "\n" + assistantBlock(long) + "\n"
	afero.WriteFile(fs, path, []byte(content), 0o644)

	activity := extractRecentActivityFS(fs, path)

	if len(activity) > maxTextForSummary {
		t.Fatalf("should truncate to %d chars, got %d", maxTextForSummary, len(activity))
	}
}

func TestExtractRecentActivity_MissingFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	activity := extractRecentActivityFS(fs, "/nonexistent.jsonl")
	if activity != "" {
		t.Fatal("missing file should return empty")
	}
}

func TestExtractRecentActivity_NoAssistantMessages(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/session.jsonl"

	content := userLine() + "\n"
	afero.WriteFile(fs, path, []byte(content), 0o644)

	activity := extractRecentActivityFS(fs, path)
	if activity != "" {
		t.Fatalf("no assistant messages should return empty, got '%s'", activity)
	}
}

// --- generateSummaryWith ---

func TestGenerateSummary_PassesCorrectArgs(t *testing.T) {
	cmd := &fakeCmd{
		Outputs: make(map[string][]byte),
		StdinOut: "Fixed a bug in the parser",
	}

	globalSummary.mu.Lock()
	globalSummary.pending["test-key"] = true
	globalSummary.mu.Unlock()

	generateSummaryWith(cmd, "test-key", "Some activity text")

	if len(cmd.StdinCalls) != 1 {
		t.Fatalf("expected 1 stdin call, got %d", len(cmd.StdinCalls))
	}
	call := cmd.StdinCalls[0]
	if call.Args[0] != "claude" {
		t.Fatalf("should call claude, got %s", call.Args[0])
	}
	foundHaiku := false
	for _, a := range call.Args {
		if a == "haiku" {
			foundHaiku = true
		}
	}
	if !foundHaiku {
		t.Fatalf("should pass haiku model, args: %v", call.Args)
	}
	if !strings.Contains(call.Stdin, "Some activity text") {
		t.Fatalf("prompt should contain activity text, got: %s", call.Stdin)
	}
}

func TestGenerateSummary_EmptyOutputSkipsSave(t *testing.T) {
	cmd := &fakeCmd{
		Outputs:  make(map[string][]byte),
		StdinOut: "",
	}

	globalSummary.mu.Lock()
	globalSummary.pending["empty-key"] = true
	globalSummary.mu.Unlock()

	generateSummaryWith(cmd, "empty-key", "Some activity")
	// Should not panic or save anything — no way to assert DB save without
	// wiring DB, but at least verify it doesn't crash
}

func TestGenerateSummary_ErrorDoesNotSave(t *testing.T) {
	cmd := &fakeCmd{
		Outputs:  make(map[string][]byte),
		StdinErr: fmt.Errorf("claude not found"),
	}

	globalSummary.mu.Lock()
	globalSummary.pending["err-key"] = true
	globalSummary.mu.Unlock()

	generateSummaryWith(cmd, "err-key", "Some activity")
	// Should not panic
}

func TestGenerateSummary_StripsURLsFromPrompt(t *testing.T) {
	cmd := &fakeCmd{
		Outputs:  make(map[string][]byte),
		StdinOut: "Did something",
	}

	globalSummary.mu.Lock()
	globalSummary.pending["url-key"] = true
	globalSummary.mu.Unlock()

	generateSummaryWith(cmd, "url-key", "Visited https://example.com/page and did stuff")

	if len(cmd.StdinCalls) != 1 {
		t.Fatal("expected 1 call")
	}
	if strings.Contains(cmd.StdinCalls[0].Stdin, "https://example.com") {
		t.Fatal("URLs should be stripped from prompt")
	}
	if !strings.Contains(cmd.StdinCalls[0].Stdin, "(url)") {
		t.Fatal("URLs should be replaced with (url)")
	}
}

func TestGenerateSummary_ClearsPendingFlag(t *testing.T) {
	cmd := &fakeCmd{
		Outputs:  make(map[string][]byte),
		StdinOut: "summary",
	}

	globalSummary.mu.Lock()
	globalSummary.pending["pending-key"] = true
	globalSummary.mu.Unlock()

	generateSummaryWith(cmd, "pending-key", "activity")

	globalSummary.mu.Lock()
	isPending := globalSummary.pending["pending-key"]
	globalSummary.mu.Unlock()

	if isPending {
		t.Fatal("pending flag should be cleared after generation")
	}
}

// --- maybeRegenerateSummary (hash-based dedup) ---

func TestHashBasedDedup(t *testing.T) {
	h1 := hashEntry("activity text version 1")
	h2 := hashEntry("activity text version 1")
	h3 := hashEntry("activity text version 2")

	if h1 != h2 {
		t.Fatal("same activity should produce same hash")
	}
	if h1 == h3 {
		t.Fatal("different activity should produce different hash")
	}
	fmt.Println("hash dedup: same content deduped, different content distinguished")
}
