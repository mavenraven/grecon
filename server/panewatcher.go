package server

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const paneBufferSize = 4096

type PaneWatcher struct {
	mu       sync.RWMutex
	buffers  map[string]*ringBuffer
	statuses map[string]SessionStatus
	clients  map[string]*exec.Cmd
	notify   chan struct{}
}

type ringBuffer struct {
	data []byte
	pos  int
	full bool
}

func NewPaneWatcher() *PaneWatcher {
	return &PaneWatcher{
		buffers:  make(map[string]*ringBuffer),
		statuses: make(map[string]SessionStatus),
		clients:  make(map[string]*exec.Cmd),
		notify:   make(chan struct{}, 1),
	}
}

func (pw *PaneWatcher) Notify() <-chan struct{} {
	return pw.notify
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{data: make([]byte, size)}
}

func (rb *ringBuffer) Write(p []byte) {
	for _, b := range p {
		rb.data[rb.pos] = b
		rb.pos = (rb.pos + 1) % len(rb.data)
		if rb.pos == 0 {
			rb.full = true
		}
	}
}

func (rb *ringBuffer) String() string {
	if !rb.full {
		return string(rb.data[:rb.pos])
	}
	var buf []byte
	buf = append(buf, rb.data[rb.pos:]...)
	buf = append(buf, rb.data[:rb.pos]...)
	return string(buf)
}

func (pw *PaneWatcher) GetStatus(sessionName string) (SessionStatus, bool) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	s, ok := pw.statuses[sessionName]
	return s, ok
}

func (pw *PaneWatcher) Sync(sessionNames []string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	wanted := make(map[string]bool, len(sessionNames))
	for _, name := range sessionNames {
		wanted[name] = true
	}

	for _, name := range sessionNames {
		if _, ok := pw.clients[name]; !ok {
			pw.startClientLocked(name)
		}
	}

	for name, cmd := range pw.clients {
		if !wanted[name] {
			cmd.Process.Kill()
			cmd.Wait()
			delete(pw.clients, name)
			delete(pw.buffers, name)
			delete(pw.statuses, name)
		}
	}
}

func (pw *PaneWatcher) startClientLocked(sessionName string) {
	cmd := exec.Command("tmux", "-C", "attach-session", "-t", sessionName)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "panewatcher: stdin pipe for %s: %v\n", sessionName, err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "panewatcher: stdout pipe for %s: %v\n", sessionName, err)
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "panewatcher: start for %s: %v\n", sessionName, err)
		return
	}

	pw.clients[sessionName] = cmd

	go func() {
		defer stdin.Close()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "%output ") {
				continue
			}
			pw.handleOutput(sessionName, line)
		}
		cmd.Wait()
		pw.mu.Lock()
		delete(pw.clients, sessionName)
		pw.mu.Unlock()
	}()
}

func (pw *PaneWatcher) handleOutput(sessionName, line string) {
	rest := line[len("%output "):]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return
	}
	escaped := rest[spaceIdx+1:]
	data := unescapeControlMode(escaped)

	pw.mu.Lock()
	buf, ok := pw.buffers[sessionName]
	if !ok {
		buf = newRingBuffer(paneBufferSize)
		pw.buffers[sessionName] = buf
	}
	buf.Write([]byte(data))
	content := buf.String()
	newStatus := paneStatusFromContent(content)
	changed := pw.statuses[sessionName] != newStatus
	pw.statuses[sessionName] = newStatus
	pw.mu.Unlock()

	if changed {
		select {
		case pw.notify <- struct{}{}:
		default:
		}
	}
}

func unescapeControlMode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+3 < len(s) {
			c1 := s[i+1]
			c2 := s[i+2]
			c3 := s[i+3]
			if isOctal(c1) && isOctal(c2) && isOctal(c3) {
				val := (c1-'0')*64 + (c2-'0')*8 + (c3 - '0')
				b.WriteByte(val)
				i += 4
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isOctal(c byte) bool {
	return c >= '0' && c <= '7'
}

func (pw *PaneWatcher) Stop() {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	for name, cmd := range pw.clients {
		cmd.Process.Kill()
		cmd.Wait()
		delete(pw.clients, name)
	}
}
