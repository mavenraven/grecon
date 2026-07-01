package server

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func writeFrame(conn net.Conn, data []byte) {
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	conn.Write(buf)
}

func writeRawLength(conn net.Conn, length uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], length)
	conn.Write(buf[:])
}

// --- readFrame ---

func TestReadFrame_ValidFrame(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sessions := []*Session{{SessionID: "abc", Status: StatusIdle}}
	data, _ := json.Marshal(sessions)

	go writeFrame(server, data)

	result := readFrame(client, time.Second)
	if result == nil {
		t.Fatal("valid frame should decode")
	}
	if len(result) != 1 || result[0].SessionID != "abc" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReadFrame_ZeroLength(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go writeRawLength(server, 0)

	result := readFrame(client, time.Second)
	if result != nil {
		t.Fatal("zero length should return nil")
	}
}

func TestReadFrame_OversizedLength(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go writeRawLength(server, 10_000_001)

	result := readFrame(client, time.Second)
	if result != nil {
		t.Fatal("oversized length should return nil")
	}
}

func TestReadFrame_TruncatedData(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	go func() {
		writeRawLength(server, 1000)
		server.Write([]byte("short"))
		server.Close()
	}()

	result := readFrame(client, time.Second)
	if result != nil {
		t.Fatal("truncated data should return nil")
	}
}

func TestReadFrame_InvalidJSON(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go writeFrame(server, []byte("not json"))

	result := readFrame(client, time.Second)
	if result != nil {
		t.Fatal("invalid JSON should return nil")
	}
}

func TestReadFrame_EmptyArray(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go writeFrame(server, []byte("[]"))

	result := readFrame(client, time.Second)
	if result == nil {
		t.Fatal("empty array should decode, not nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(result))
	}
}

func TestReadFrame_ConnectionClosed(t *testing.T) {
	server, client := net.Pipe()
	server.Close()

	result := readFrame(client, time.Second)
	client.Close()
	if result != nil {
		t.Fatal("closed connection should return nil")
	}
}

// --- readFramesLoop ---

func TestReadFramesLoop_StopChannelExits(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ch := make(chan []*Session, 1)
	stop := make(chan struct{})

	done := make(chan bool)
	go func() {
		readFramesLoop(client, ch, stop)
		done <- true
	}()

	close(stop)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readFramesLoop should exit when stop channel is closed")
	}
}

func TestReadFramesLoop_SendsFramesToChannel(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ch := make(chan []*Session, 1)
	stop := make(chan struct{})

	go readFramesLoop(client, ch, stop)

	sessions := []*Session{{SessionID: "test"}}
	data, _ := json.Marshal(sessions)
	writeFrame(server, data)

	select {
	case result := <-ch:
		if len(result) != 1 || result[0].SessionID != "test" {
			t.Fatalf("unexpected: %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("should receive frame on channel")
	}

	close(stop)
}

func TestReadFramesLoop_DropsOldFrameWhenFull(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ch := make(chan []*Session, 1)
	stop := make(chan struct{})

	go readFramesLoop(client, ch, stop)

	// Send two frames quickly — channel buffer is 1
	data1, _ := json.Marshal([]*Session{{SessionID: "old"}})
	data2, _ := json.Marshal([]*Session{{SessionID: "new"}})
	writeFrame(server, data1)
	time.Sleep(50 * time.Millisecond)
	writeFrame(server, data2)
	time.Sleep(50 * time.Millisecond)

	// Should get the latest frame
	var last []*Session
	for {
		select {
		case s := <-ch:
			last = s
		default:
			goto done
		}
	}
done:
	if last == nil {
		t.Fatal("should have received at least one frame")
	}

	close(stop)
}

// --- Subscribe ---

func TestSubscribe_ClosesCleanly(t *testing.T) {
	stop := make(chan struct{})
	ch := Subscribe(stop)

	close(stop)

	select {
	case _, ok := <-ch:
		if ok {
			// got a frame before close, that's fine, drain
			for range ch {
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Subscribe channel should close when stop is closed")
	}
}
