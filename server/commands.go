package server

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"grecon/db"
)

type Command struct {
	Type        string `json:"type"`
	TmuxSession string `json:"tmux_session,omitempty"`
	Name        string `json:"name,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	ClaudeName  string `json:"claude_name,omitempty"`
	Worktree    bool   `json:"worktree,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	CustomCmd   string `json:"custom_cmd,omitempty"`
	Tags        string `json:"tags,omitempty"`
}

type CommandResponse struct {
	OK          bool   `json:"ok"`
	TmuxSession string `json:"tmux_session,omitempty"`
	Error       string `json:"error,omitempty"`
}

func CommandSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/.grecon/" + db.Instance() + "-cmd.sock"
	}
	return filepath.Join(home, ".grecon", db.Instance()+"-cmd.sock")
}

func SendCommand(cmd Command) (*CommandResponse, error) {
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout("unix", CommandSocketPath(), 500*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("server not running: %w", err)
	}
	defer conn.Close()

	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(buf); err != nil {
		return nil, err
	}

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	var respLenBuf [4]byte
	if _, err := io.ReadFull(conn, respLenBuf[:]); err != nil {
		return nil, err
	}
	respLen := binary.BigEndian.Uint32(respLenBuf[:])
	if respLen == 0 || respLen > 1_000_000 {
		return nil, fmt.Errorf("invalid response length")
	}
	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, err
	}
	var resp CommandResponse
	if err := json.Unmarshal(respBuf, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func listenCommands() {
	path := CommandSocketPath()
	os.Remove(path)

	listener, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to bind command socket %s: %v\n", path, err)
		return
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleCommand(conn)
	}
}

func handleCommand(conn net.Conn) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(time.Second))
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 || length > 1_000_000 {
		return
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return
	}

	var cmd Command
	if json.Unmarshal(buf, &cmd) != nil {
		return
	}

	var resp CommandResponse
	switch cmd.Type {
	case "fix-default-path":
		go fixDefaultPath(cmd.TmuxSession)
		resp = CommandResponse{OK: true}
	case "create-session":
		resp = handleCreateSession(cmd)
	case "reactivate-session":
		resp = handleReactivateSession(cmd)
	case "delete-session":
		resp = handleDeleteSession(cmd)
	default:
		resp = CommandResponse{OK: false, Error: "unknown command"}
	}

	sendResponse(conn, resp)
}

func sendResponse(conn net.Conn, resp CommandResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	conn.Write(buf)
}

func handleCreateSession(cmd Command) CommandResponse {
	d := db.Get()
	if d == nil {
		return CommandResponse{OK: false, Error: "no database"}
	}

	if !ValidateCWD(cmd.CWD) {
		return CommandResponse{OK: false, Error: "invalid cwd"}
	}

	displayName := sanitizeSessionName(cmd.Name)

	tmuxID, err := db.CreateWorkstreamDB(d, displayName, time.Now)
	if err != nil {
		return CommandResponse{OK: false, Error: fmt.Sprintf("create workstream: %v", err)}
	}

	args := []string{"new-session", "-d", "-s", tmuxID, "-c", cmd.CWD}

	if cmd.Tags != "" {
		args = append(args, "-e", fmt.Sprintf("RECON_TAGS=%s", cmd.Tags))
	}

	if cmd.CustomCmd != "" {
		parts := strings.Fields(cmd.CustomCmd)
		args = append(args, parts...)
	} else {
		claudePath := whichClaudeBinary()
		args = append(args, claudePath)
		if cmd.ClaudeName != "" {
			args = append(args, "-n", cmd.ClaudeName)
		}
		if cmd.Worktree {
			args = append(args, "--worktree")
		}
	}

	tmuxCmd := exec.Command("tmux", args...)
	if err := tmuxCmd.Run(); err != nil {
		return CommandResponse{OK: false, Error: fmt.Sprintf("tmux: %v", err)}
	}

	exec.Command("tmux", "set-option", "-t", tmuxID, "@display_name", displayName).Run()
	if cmd.Worktree {
		go fixDefaultPath(tmuxID)
	}

	return CommandResponse{OK: true, TmuxSession: tmuxID}
}

func handleReactivateSession(cmd Command) CommandResponse {
	d := db.Get()
	if d == nil {
		return CommandResponse{OK: false, Error: "no database"}
	}

	db.SetSessionActive(d, cmd.SessionID, true)

	tmuxSession := cmd.TmuxSession
	if !tmuxSessionExists(tmuxSession) {
		cwd := FindSessionCWD(cmd.SessionID)
		if cwd == "" || !ValidateCWD(cwd) {
			return CommandResponse{OK: false, Error: "bad cwd"}
		}
		claudePath := whichClaudeBinary()
		exec.Command("tmux",
			"new-session", "-d", "-s", tmuxSession, "-c", cwd,
			claudePath, "--resume", cmd.SessionID,
		).Run()
	} else {
		cwd := FindSessionCWD(cmd.SessionID)
		if cwd == "" {
			cwd = "."
		}
		claudePath := whichClaudeBinary()
		exec.Command("tmux",
			"new-window", "-t", tmuxSession, "-c", cwd,
			claudePath, "--resume", cmd.SessionID,
		).Run()
	}

	return CommandResponse{OK: true, TmuxSession: tmuxSession}
}

func handleDeleteSession(cmd Command) CommandResponse {
	d := db.Get()
	if d == nil {
		return CommandResponse{OK: false, Error: "no database"}
	}
	if cmd.SessionID == "" {
		return CommandResponse{OK: false, Error: "no session_id"}
	}
	db.SoftDeleteSession(d, cmd.SessionID, time.Now)
	return CommandResponse{OK: true}
}

func sanitizeSessionName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.TrimLeft(b.String(), "-")
	if result == "" {
		return "claude"
	}
	return result
}

func fixDefaultPath(tmuxSession string) {
	fixDefaultPathWith(RealEnv().Cmd, tmuxSession, 500*time.Millisecond)
}

func fixDefaultPathWith(cmd CommandRunner, tmuxSession string, pollInterval time.Duration) {
	for i := 0; i < 30; i++ {
		time.Sleep(pollInterval)
		out, err := cmd.Output("tmux", "display-message", "-t", tmuxSession+":0.0", "-p", "#{pane_current_path}")
		if err != nil {
			continue
		}
		panePath := strings.TrimSpace(string(out))
		if strings.Contains(panePath, "/.claude/worktrees/") {
			cmd.Run("tmux", "attach-session", "-t", tmuxSession, "-c", panePath)
			return
		}
	}
}
