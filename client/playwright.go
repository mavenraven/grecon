package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type PlaywrightSetup struct {
	Port        int
	UserDataDir string
	MCPConfig   string
	SourceFile  string
}

func SetupPlaywright(sessionName string) (*PlaywrightSetup, error) {
	port, err := findFreePort()
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}

	baseDir := filepath.Join(os.TempDir(), "grecon-playwright", sessionName)
	os.MkdirAll(baseDir, 0o755)

	userDataDir := filepath.Join(baseDir, "chrome-data")
	if err := cloneChromeProfile(userDataDir); err != nil {
		return nil, fmt.Errorf("clone chrome profile: %w", err)
	}

	mcpConfig := filepath.Join(baseDir, "mcp.json")
	if err := writeMCPConfig(mcpConfig, port); err != nil {
		return nil, fmt.Errorf("write mcp config: %w", err)
	}

	sourceFile := filepath.Join(baseDir, "helpers.sh")
	if err := writeHelperScript(sourceFile, port, userDataDir); err != nil {
		return nil, fmt.Errorf("write helper script: %w", err)
	}

	return &PlaywrightSetup{
		Port:        port,
		UserDataDir: userDataDir,
		MCPConfig:   mcpConfig,
		SourceFile:  sourceFile,
	}, nil
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func chromeProfileDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	}
	return filepath.Join(home, ".config", "google-chrome")
}

func cloneChromeProfile(dest string) error {
	src := chromeProfileDir()
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("chrome profile not found at %s", src)
	}

	if runtime.GOOS == "darwin" {
		err := exec.Command("cp", "-c", "-r", src, dest).Run()
		if err == nil {
			return nil
		}
	}
	return exec.Command("cp", "-r", src, dest).Run()
}

func writeMCPConfig(path string, port int) error {
	config := map[string]any{
		"mcpServers": map[string]any{
			"playwright": map[string]any{
				"command": "npx",
				"args": []string{
					"@playwright/mcp@latest",
					"--cdp-endpoint",
					fmt.Sprintf("http://localhost:%d", port),
				},
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeHelperScript(path string, port int, userDataDir string) error {
	script := fmt.Sprintf(`playwright-mcp-chrome() {
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    --remote-debugging-port=%d \
    --user-data-dir="%s" \
    "$@"
}
`, port, userDataDir)
	return os.WriteFile(path, []byte(script), 0o644)
}
