package docker

import (
	"context"
	"io"
	"os/exec"
	"time"
)

type dockerCmd interface {
	SetDir(string)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
	Run() error
}

type realCmd struct {
	*exec.Cmd
}

func (c *realCmd) SetDir(dir string)           { c.Dir = dir }
func (c *realCmd) SetStdout(w io.Writer)       { c.Stdout = w }
func (c *realCmd) SetStderr(w io.Writer)       { c.Stderr = w }
func (c *realCmd) Output() ([]byte, error)     { return c.Cmd.Output() }
func (c *realCmd) CombinedOutput() ([]byte, error) { return c.Cmd.CombinedOutput() }
func (c *realCmd) Run() error                  { return c.Cmd.Run() }

var (
	newDockerCmd        = func(name string, arg ...string) dockerCmd { return &realCmd{exec.Command(name, arg...)} }
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd { return &realCmd{exec.CommandContext(ctx, name, arg...)} }
	buildTimeout        = 30 * time.Minute
	composeTimeout      = 5 * time.Minute
	listTimeout         = 15 * time.Second
	tagTimeout          = 30 * time.Second
	pullTimeout         = 10 * time.Minute
	containerTimeout    = 60 * time.Second
	execTimeout         = 60 * time.Second
	statusTimeout       = 10 * time.Second
	runOutputTimeout    = 30 * time.Second
)
