package server

import (
	"os"
	"os/exec"
	"time"

	"github.com/spf13/afero"
)

type CommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type Env struct {
	Fs    afero.Fs
	Cmd   CommandRunner
	Clock func() time.Time
	Home  string
}

func RealEnv() *Env {
	home, _ := os.UserHomeDir()
	return &Env{
		Fs:    afero.NewOsFs(),
		Cmd:   &realCmd{},
		Clock: time.Now,
		Home:  home,
	}
}

type realCmd struct{}

func (r *realCmd) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (r *realCmd) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
