package server

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func extractJSON(frame []byte) []byte {
	if len(frame) < 4 {
		return nil
	}
	length := binary.BigEndian.Uint32(frame[:4])
	return frame[4 : 4+length]
}

func TestSerializeSessions_NilReturnsEmptyArray(t *testing.T) {
	data := extractJSON(SerializeSessions(nil))
	if string(data) != "[]" {
		t.Fatalf("nil sessions should serialize to [], got %s", string(data))
	}
}

func TestSerializeSessions_EmptySliceReturnsEmptyArray(t *testing.T) {
	data := extractJSON(SerializeSessions([]*Session{}))
	if string(data) != "[]" {
		t.Fatalf("empty slice should serialize to [], got %s", string(data))
	}
}

func TestSerializeSessions_RoundTrip(t *testing.T) {
	sessions := []*Session{
		{SessionID: "abc", TmuxSession: "uuid-1", Status: StatusIdle},
		{SessionID: "def", TmuxSession: "uuid-2", Status: StatusWorking},
	}
	data := extractJSON(SerializeSessions(sessions))

	var result []*Session
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("should unmarshal cleanly: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(result))
	}
	if result[0].SessionID != "abc" {
		t.Fatalf("expected abc, got %s", result[0].SessionID)
	}
}

func TestSerializeSessions_FrameLength(t *testing.T) {
	frame := SerializeSessions([]*Session{{SessionID: "test"}})
	length := binary.BigEndian.Uint32(frame[:4])
	if int(length) != len(frame)-4 {
		t.Fatalf("frame length mismatch: header says %d, actual payload %d", length, len(frame)-4)
	}
}
