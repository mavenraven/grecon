package server

import (
	"bufio"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"grecon/db"

	"github.com/spf13/afero"
)

func SocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/.grecon/" + db.Instance() + ".sock"
	}
	return filepath.Join(home, ".grecon", db.Instance()+".sock")
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

func lockPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/.grecon/" + db.Instance() + ".lock"
	}
	return filepath.Join(home, ".grecon", db.Instance()+".lock")
}

func acquireLock() {
	lp := lockPath()
	data, err := os.ReadFile(lp)
	if err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pidStr != "" {
			pid, err := strconv.Atoi(pidStr)
			if err == nil {
				proc, err := os.FindProcess(pid)
				if err == nil && proc.Signal(syscall.Signal(0)) == nil {
					fmt.Fprintf(os.Stderr, "grecon server already running (pid %d)\n", pid)
					os.Exit(1)
				}
			}
		}
		os.Remove(lp)
	}
	os.MkdirAll(filepath.Dir(lp), 0o755)
	os.WriteFile(lp, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func releaseLock() {
	os.Remove(lockPath())
}

func RunServer(envs ...*Env) {
	var env *Env
	if len(envs) > 0 && envs[0] != nil {
		env = envs[0]
	} else {
		env = RealEnv()
	}
	runServer(env)
}

func runServer(env *Env) {
	acquireLock()
	defer releaseLock()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		releaseLock()
		os.Exit(0)
	}()

	d, err := db.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer d.Close()

	reconcileWithEnv(env, d)

	path := SocketPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.Remove(path)

	prev := make(map[string]*Session)
	initial := seedFromDB(env, d)
	for _, s := range initial {
		prev[s.SessionID] = s
	}

	pw := NewPaneWatcher()
	defer pw.Stop()
	syncPaneWatcher(pw, initial)

	var mu sync.Mutex
	data := SerializeSessions(initial)
	var subs []net.Conn

	listener, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to bind %s: %v\n", path, err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Fprintf(os.Stderr, "grecon server listening on %s\n", path)

	go listenCommands(env)

	broadcast := func(sessions []*Session) {
		for _, s := range sessions {
			if s.Status == StatusInactive || s.Status == StatusDeleted {
				continue
			}
			if status, ok := pw.GetStatus(s.TmuxSession); ok {
				s.Status = debounceStatus(s.SessionID, status)
			}
		}

		newData := SerializeSessions(sessions)

		mu.Lock()
		data = newData
		var alive []net.Conn
		for _, conn := range subs {
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err := conn.Write(newData); err != nil {
				conn.Close()
			} else {
				alive = append(alive, conn)
			}
		}
		subs = alive
		mu.Unlock()
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "PANIC in poll goroutine: %v\n", r)
			}
		}()
		sessions := initial
		pollCount := uint64(0)
		discoverTicker := time.NewTicker(500 * time.Millisecond)
		defer discoverTicker.Stop()

		for {
			select {
			case <-pw.Notify():
				broadcast(sessions)

			case <-discoverTicker.C:
				pollCount++
				pollStart := time.Now()
				sessions = discoverTmuxSessions(env, prev)
				pollMs := time.Since(pollStart).Milliseconds()

				sessions = reconcileDBWithLive(env, d, sessions)
				cleanupSoftDeleted(env, d, sessions)

				prev = make(map[string]*Session)
				for _, s := range sessions {
					prev[s.SessionID] = s
				}

				AttachSummaries(sessions)
				syncPaneWatcher(pw, sessions)

				fmt.Printf("poll #%d: discover=%dms sessions=%d\n",
					pollCount, pollMs, len(sessions))

				broadcast(sessions)

				if pollCount%10 == 0 {
					liveTmux := make(map[string]bool)
					for _, s := range sessions {
						if s.TmuxSession != "" {
							liveTmux[s.TmuxSession] = true
						}
					}
					go db.PruneDeadSessions(d, liveTmux, env.Fs, env.Home, env.Clock)
				}
			}
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		mu.Lock()
		snapshot := make([]byte, len(data))
		copy(snapshot, data)
		mu.Unlock()

		conn.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write(snapshot); err != nil {
			conn.Close()
			continue
		}

		mu.Lock()
		subs = append(subs, conn)
		mu.Unlock()
	}
}

func TryFetch() []*Session {
	path := SocketPath()
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return nil
	}
	defer conn.Close()
	return readFrame(conn, 500*time.Millisecond)
}

func RequireFetch() ([]*Session, error) {
	sessions := TryFetch()
	if sessions != nil {
		return sessions, nil
	}
	return nil, fmt.Errorf("grecon server is not running. Start it with: grecon server")
}

func Subscribe(stop <-chan struct{}) <-chan []*Session {
	ch := make(chan []*Session, 1)
	go func() {
		defer close(ch)
		for {
			select {
			case <-stop:
				return
			default:
			}
			conn, err := net.DialTimeout("unix", SocketPath(), 500*time.Millisecond)
			if err != nil {
				select {
				case <-stop:
					return
				case <-time.After(time.Second):
					continue
				}
			}
			readFramesLoop(conn, ch, stop)
			conn.Close()
		}
	}()
	return ch
}

func readFramesLoop(conn net.Conn, ch chan []*Session, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		sessions := readFrame(conn, 5*time.Second)
		if sessions == nil {
			return
		}
		select {
		case ch <- sessions:
		default:
			<-ch
			ch <- sessions
		}
	}
}

func readFrame(conn net.Conn, deadline time.Duration) []*Session {
	conn.SetReadDeadline(time.Now().Add(deadline))
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

func seedFromDB(env *Env, d *sql.DB) []*Session {
	workstreams := db.AllWorkstreams(d)
	var sessions []*Session
	for _, ws := range workstreams {
		for _, cs := range ws.Sessions {
			status := StatusInactive
			if cs.Active {
				status = StatusNew
			}
			sessions = append(sessions, &Session{
				SessionID:   cs.SessionID,
				TmuxSession: ws.TmuxID,
				ClaudeName:  ReadAgentNameFromJSONL(env.Fs, env.Home, cs.SessionID),
				Summary:     db.LoadSummaryDB(d, cs.SessionID),
				Status:      status,
			})
		}
	}
	return sessions
}

func reconcileDBWithLive(env *Env, d *sql.DB, liveSessions []*Session) []*Session {
	liveByTmux := make(map[string]map[string]bool)
	for _, s := range liveSessions {
		if s.TmuxSession == "" {
			continue
		}
		if liveByTmux[s.TmuxSession] == nil {
			liveByTmux[s.TmuxSession] = make(map[string]bool)
		}
		liveByTmux[s.TmuxSession][s.SessionID] = true
	}

	workstreams := db.AllWorkstreams(d)
	for _, ws := range workstreams {
		liveIDs := liveByTmux[ws.TmuxID]

		// Add new live sessions not yet in DB
		if liveIDs != nil {
			knownIDs := make(map[string]bool)
			for _, cs := range ws.Sessions {
				knownIDs[cs.SessionID] = true
			}
			for sid := range liveIDs {
				if !knownIDs[sid] {
					db.AddClaudeSession(d, ws.WorkstreamID, sid, env.Clock)
				}
			}
		}

		for _, cs := range ws.Sessions {
			if cs.Active && (liveIDs == nil || !liveIDs[cs.SessionID]) {
				db.SetSessionActive(d, cs.SessionID, false)
			}
			if !cs.Active && liveIDs != nil && liveIDs[cs.SessionID] {
				db.SetSessionActive(d, cs.SessionID, true)
			}
		}
	}

	// Re-read workstreams to get updated state and append non-live sessions
	workstreams = db.AllWorkstreams(d)
	displayNames := make(map[string]string)
	for _, ws := range workstreams {
		displayNames[ws.TmuxID] = ws.DisplayName
		liveIDs := liveByTmux[ws.TmuxID]

		for _, cs := range ws.Sessions {
			if liveIDs != nil && liveIDs[cs.SessionID] {
				continue
			}
			status := StatusInactive
			if cs.SessionID != "" && !jsonlExistsForSessionFS(env.Fs, env.Home, cs.SessionID) {
				status = StatusDeleted
			}
			liveSessions = append(liveSessions, &Session{
				SessionID:       cs.SessionID,
				TmuxSession:     ws.TmuxID,
				TmuxDisplayName: ws.DisplayName,
				ClaudeName:      ReadAgentNameFromJSONL(env.Fs, env.Home, cs.SessionID),
				Summary:         db.LoadSummaryDB(d, cs.SessionID),
				Status:          status,
			})
		}
	}

	for _, s := range liveSessions {
		if name, ok := displayNames[s.TmuxSession]; ok {
			s.TmuxDisplayName = name
		}
	}

	return liveSessions
}

func cleanupSoftDeleted(env *Env, d *sql.DB, liveSessions []*Session) {
	deletedIDs := make(map[string]bool)
	for _, id := range db.SoftDeletedSessionIDs(d) {
		deletedIDs[id] = true
	}

	for _, s := range liveSessions {
		if s.PaneTarget != "" && deletedIDs[s.SessionID] {
			env.Cmd.Run("tmux", "kill-pane", "-t", s.PaneTarget)
		}
	}
}

func readAgentNameFromJSONL(sessionID string) string {
	return ReadAgentNameFromJSONL(afero.NewOsFs(), homeDir(), sessionID)
}

func ReadAgentNameFromJSONL(fs afero.Fs, home, sessionID string) string {
	path := db.FindJSONLPath(fs, home, sessionID)
	if path == "" {
		return ""
	}
	f, err := fs.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for i := 0; i < 10 && scanner.Scan(); i++ {
		line := scanner.Text()
		if !strings.Contains(line, `"agent-name"`) {
			continue
		}
		var entry struct {
			Type      string `json:"type"`
			AgentName string `json:"agentName"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Type == "agent-name" && entry.AgentName != "" {
			return entry.AgentName
		}
	}
	return ""
}

func jsonlExistsForSession(sessionID string) bool {
	return FindSessionCWD(sessionID) != "" || findJSONLBySessionID(sessionID) != ""
}

func jsonlExistsForSessionFS(fs afero.Fs, home, sessionID string) bool {
	return db.FindJSONLPath(fs, home, sessionID) != ""
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func discoverTmuxSessions(env *Env, prev map[string]*Session) []*Session {
	d := db.Get()
	knownTmux := make(map[string]bool)
	if d != nil {
		for _, ws := range db.AllWorkstreams(d) {
			knownTmux[ws.TmuxID] = true
		}
	}

	all := DiscoverSessions(env, prev)
	var sessions []*Session
	for _, s := range all {
		if s.TmuxSession != "" && knownTmux[s.TmuxSession] {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

func syncPaneWatcher(pw *PaneWatcher, sessions []*Session) {
	var names []string
	for _, s := range sessions {
		if s.TmuxSession != "" {
			names = append(names, s.TmuxSession)
		}
	}
	pw.Sync(names)
}
