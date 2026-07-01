package server

import (
	"encoding/json"
	"testing"
)

func toolResultLine(toolUseID string) string {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": toolUseID},
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func TestCleanupPendingCalls_RemovesMatchingToolResult(t *testing.T) {
	pending := map[string]*BackgroundTask{
		"call-1": {Kind: BgShell, Command: "npm test"},
		"call-2": {Kind: BgShell, Command: "go build"},
	}

	cleanupPendingCalls(toolResultLine("call-1"), pending)

	if _, ok := pending["call-1"]; ok {
		t.Fatal("call-1 should be removed")
	}
	if _, ok := pending["call-2"]; !ok {
		t.Fatal("call-2 should remain")
	}
}

func TestCleanupPendingCalls_NoMatchDoesNothing(t *testing.T) {
	pending := map[string]*BackgroundTask{
		"call-1": {Kind: BgShell, Command: "npm test"},
	}

	cleanupPendingCalls(toolResultLine("unknown-id"), pending)

	if len(pending) != 1 {
		t.Fatalf("should still have 1 pending, got %d", len(pending))
	}
}

func TestCleanupPendingCalls_InvalidJSON(t *testing.T) {
	pending := map[string]*BackgroundTask{
		"call-1": {Kind: BgShell, Command: "npm test"},
	}

	cleanupPendingCalls("not json at all", pending)

	if len(pending) != 1 {
		t.Fatal("invalid JSON should leave pending unchanged")
	}
}

func TestCleanupPendingCalls_EmptyPending(t *testing.T) {
	pending := map[string]*BackgroundTask{}
	cleanupPendingCalls(toolResultLine("whatever"), pending)
	// should not panic
}

func TestIsSpinner(t *testing.T) {
	cases := []struct {
		r    rune
		want bool
	}{
		{'✦', true},
		{'✧', true},
		{'⏺', true},
		{'·', true},
		{'a', false},
		{'>', false},
		{' ', false},
	}
	for _, tc := range cases {
		got := isSpinner(tc.r)
		if got != tc.want {
			t.Errorf("isSpinner(%q) = %v, want %v", tc.r, got, tc.want)
		}
	}
}
