package server

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"grecon/db"

	"github.com/spf13/afero"
)

type CommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
	RunWithStdin(stdin string, name string, args ...string) (string, error)
}

type Env struct {
	Fs    afero.Fs
	Cmd   CommandRunner
	Clock func() time.Time
	Home  string
	DB    *sql.DB
}

func RealEnv() *Env {
	home, _ := os.UserHomeDir()
	d, err := db.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	return &Env{
		Fs:    afero.NewOsFs(),
		Cmd:   &realCmd{},
		Clock: time.Now,
		Home:  home,
		DB:    d,
	}
}

type realCmd struct{}

func (r *realCmd) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (r *realCmd) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (r *realCmd) RunWithStdin(stdin string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}
