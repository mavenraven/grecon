package server

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func SocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/.recon/grecon.sock"
	}
	return filepath.Join(home, ".recon", "grecon.sock")
}

func SerializeSessions(sessions []*Session) []byte {
	if sessions == nil {
		sessions = []*Session{}
	}
	data, err := json.Marshal(sessions)
	if err != nil {
		data = []byte("[]")
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	return buf
}

func RunServer() {
	path := SocketPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.Remove(path)

	prevSessions := make(map[string]*Session)
	allSessions := DiscoverSessions(prevSessions)
	var sessions []*Session
	for _, s := range allSessions {
		if s.TmuxSession != "" {
			sessions = append(sessions, s)
		}
	}
	prevSessions = make(map[string]*Session)
	for _, s := range sessions {
		prevSessions[s.SessionID] = s
	}

	AttachSummaries(sessions)

	var mu sync.Mutex
	data := SerializeSessions(sessions)

	listener, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to bind %s: %v\n", path, err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Fprintf(os.Stderr, "grecon server listening on %s\n", path)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "PANIC in poll goroutine: %v\n", r)
			}
		}()
		prev := prevSessions
		pollCount := uint64(0)
		for {
			pollCount++
			pollStart := time.Now()
			allSessions := DiscoverSessions(prev)
			var sessions []*Session
			for _, s := range allSessions {
				if s.TmuxSession != "" {
					sessions = append(sessions, s)
				}
			}
			pollMs := time.Since(pollStart).Milliseconds()

			prev = make(map[string]*Session)
			for _, s := range sessions {
				prev[s.SessionID] = s
			}

			AttachSummaries(sessions)

			mu.Lock()
			data = SerializeSessions(sessions)
			mu.Unlock()

			fmt.Printf("poll #%d: discover=%dms sessions=%d\n",
				pollCount, pollMs, len(sessions))

			time.Sleep(100 * time.Millisecond)
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		t := time.Now()
		mu.Lock()
		snapshot := make([]byte, len(data))
		copy(snapshot, data)
		mu.Unlock()
		lockUs := time.Since(t).Microseconds()

		conn.Write(snapshot)
		conn.Close()
		totalUs := time.Since(t).Microseconds()
		fmt.Printf("conn: lock=%dµs write=%dµs bytes=%d\n", lockUs, totalUs, len(snapshot))
	}
}

func TryFetch() []*Session {
	path := SocketPath()
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return nil
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 || length > 10_000_000 {
		return nil
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil
	}

	var sessions []*Session
	if json.Unmarshal(buf, &sessions) != nil {
		return nil
	}
	return sessions
}

func RequireFetch() ([]*Session, error) {
	sessions := TryFetch()
	if sessions != nil {
		return sessions, nil
	}
	return nil, fmt.Errorf("grecon server is not running. Start it with: grecon server")
}
