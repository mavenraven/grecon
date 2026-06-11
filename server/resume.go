package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ResumeEntry struct {
	SessionID  string `json:"session_id"`
	CWD        string `json:"cwd"`
	Branch     string `json:"branch,omitempty"`
	Model      string `json:"model,omitempty"`
	Tokens     uint64 `json:"tokens"`
	LastActive string `json:"last_active"`
	ProjectDir string `json:"project_dir"`
}

func ResumeCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".recon", "resume-cache.json")
}

func ReadResumeCache() []ResumeEntry {
	path := ResumeCachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []ResumeEntry
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	return entries
}

func discoverResumableSessions(liveIDs map[string]bool) []ResumeEntry {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	projectsDir := filepath.Join(home, ".claude", "projects")
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}

	var entries []ResumeEntry
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, dir.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, file := range files {
			if filepath.Ext(file.Name()) != ".jsonl" || file.IsDir() {
				continue
			}
			path := filepath.Join(dirPath, file.Name())
			sessionID := strings.TrimSuffix(file.Name(), ".jsonl")

			if liveIDs[sessionID] {
				continue
			}

			if isTaskSession(path) {
				continue
			}

			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			mtimeMs := uint64(info.ModTime().UnixMilli())

			summary := resumeJSONLSummary(path)
			if summary.tokens == 0 {
				continue
			}

			cwd := resumeSessionCWD(path)
			if cwd == "" {
				cwd = DecodeProjectPath(dirPath)
			}

			entries = append(entries, ResumeEntry{
				SessionID:  sessionID,
				CWD:        cwd,
				Branch:     summary.branch,
				Model:      summary.model,
				Tokens:     summary.tokens,
				LastActive: time.UnixMilli(int64(mtimeMs)).UTC().Format(time.RFC3339),
				ProjectDir: dirPath,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastActive > entries[j].LastActive
	})
	return entries
}

func writeResumeCache(entries []ResumeEntry) {
	path := ResumeCachePath()
	if path == "" {
		return
	}
	if entries == nil {
		entries = []ResumeEntry{}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

type resumeSummary struct {
	model  string
	branch string
	tokens uint64
}

func resumeJSONLSummary(path string) resumeSummary {
	f, err := os.Open(path)
	if err != nil {
		return resumeSummary{}
	}
	defer f.Close()

	stat, _ := f.Stat()
	size := stat.Size()
	const tailBytes int64 = 1024 * 1024
	if size > tailBytes {
		f.Seek(size-tailBytes, io.SeekStart)
		reader := bufio.NewReader(f)
		reader.ReadString('\n')
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	const tailLines = 50
	var ring []string

	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			if len(ring) >= tailLines {
				ring = ring[1:]
			}
			ring = append(ring, line)
		}
		if err != nil {
			break
		}
	}

	var model, branch string
	var inputTokens, outputTokens uint64

	for i := len(ring) - 1; i >= 0; i-- {
		line := ring[i]

		if branch == "" && strings.Contains(line, `"gitBranch"`) {
			var v map[string]interface{}
			if json.Unmarshal([]byte(line), &v) == nil {
				if b, ok := v["gitBranch"].(string); ok {
					branch = b
				}
			}
		}

		if strings.Contains(line, `"type":"assistant"`) {
			var v map[string]interface{}
			if json.Unmarshal([]byte(line), &v) != nil {
				continue
			}
			msg, _ := v["message"].(map[string]interface{})
			if msg == nil {
				continue
			}
			if model == "" {
				if m, ok := msg["model"].(string); ok {
					model = m
				}
			}
			if inputTokens == 0 {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					it, _ := usage["input_tokens"].(float64)
					cc, _ := usage["cache_creation_input_tokens"].(float64)
					cr, _ := usage["cache_read_input_tokens"].(float64)
					ot, _ := usage["output_tokens"].(float64)
					inputTokens = uint64(it) + uint64(cc) + uint64(cr)
					outputTokens = uint64(ot)
				}
			}
			if model != "" && inputTokens > 0 && branch != "" {
				break
			}
		}
	}

	return resumeSummary{
		model:  model,
		branch: branch,
		tokens: inputTokens + outputTokens,
	}
}

func resumeSessionCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		line := scanner.Text()
		if !strings.Contains(line, `"cwd"`) {
			continue
		}
		var v map[string]interface{}
		if json.Unmarshal([]byte(line), &v) == nil {
			if cwd, ok := v["cwd"].(string); ok {
				return cwd
			}
		}
	}
	return ""
}

func isTaskSession(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), `"queue-operation"`)
}

func RefreshResumeCache(liveIDs map[string]bool) {
	start := time.Now()
	entries := discoverResumableSessions(liveIDs)
	writeResumeCache(entries)
	fmt.Printf("resume cache: %d entries in %dms\n", len(entries), time.Since(start).Milliseconds())
}
