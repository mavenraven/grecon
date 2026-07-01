package server

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

type fakeCmd struct {
	Runs       [][]string
	Outputs    map[string][]byte
	StdinCalls []stdinCall
	StdinOut   string
	StdinErr   error
}

type stdinCall struct {
	Stdin string
	Args  []string
}

func (f *fakeCmd) Run(name string, args ...string) error {
	f.Runs = append(f.Runs, append([]string{name}, args...))
	return nil
}

func (f *fakeCmd) RunWithStdin(stdin string, name string, args ...string) (string, error) {
	f.StdinCalls = append(f.StdinCalls, stdinCall{Stdin: stdin, Args: append([]string{name}, args...)})
	return f.StdinOut, f.StdinErr
}

func (f *fakeCmd) Output(name string, args ...string) ([]byte, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	if out, ok := f.Outputs[key]; ok {
		return out, nil
	}
	return nil, nil
}

func testEnv(t time.Time) (*Env, *fakeCmd) {
	fs := afero.NewMemMapFs()
	cmd := &fakeCmd{Outputs: make(map[string][]byte)}
	return &Env{
		Fs:    fs,
		Cmd:   cmd,
		Clock: func() time.Time { return t },
		Home:  "/fakehome",
	}, cmd
}

func writeTestJSONL(fs afero.Fs, home, projectDir, sessionID string, lines ...string) {
	dir := filepath.Join(home, ".claude", "projects", projectDir)
	fs.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, sessionID+".jsonl")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	afero.WriteFile(fs, path, []byte(content), 0o644)
}

func agentNameLine(name string) string {
	b, _ := json.Marshal(map[string]any{"type": "agent-name", "agentName": name})
	return string(b)
}
