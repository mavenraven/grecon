package main

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

func socketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/.recon/grecon.sock"
	}
	return filepath.Join(home, ".recon", "grecon.sock")
}

func serializeSessions(sessions []*Session) []byte {
	data, err := json.Marshal(sessions)
	if err != nil {
		data = []byte("[]")
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	return buf
}

func runServer() {
	path := socketPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.Remove(path)

	// First poll synchronously so data is ready before accepting connections
	prevSessions := make(map[string]*Session)
	allSessions := discoverSessions(prevSessions)
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

	var mu sync.Mutex
	data := serializeSessions(sessions)

	listener, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to bind %s: %v\n", path, err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Fprintf(os.Stderr, "grecon server listening on %s\n", path)

	// Polling thread
	go func() {
		prev := prevSessions
		pollCount := uint64(0)
		for {
			sleepStart := time.Now()
			time.Sleep(2 * time.Second)
			actualSleep := time.Since(sleepStart)

			pollCount++
			pollStart := time.Now()
			allSessions := discoverSessions(prev)
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

			mu.Lock()
			data = serializeSessions(sessions)
			mu.Unlock()

			fmt.Printf("poll #%d: sleep=%v discover=%dms sessions=%d\n",
				pollCount, actualSleep.Round(100*time.Millisecond), pollMs, len(sessions))
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

func tryFetch() []*Session {
	path := socketPath()
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

func requireFetch() []*Session {
	sessions := tryFetch()
	if sessions != nil {
		return sessions
	}
	fmt.Fprintln(os.Stderr, "grecon server is not running. Start it with: grecon server")
	os.Exit(1)
	return nil
}
