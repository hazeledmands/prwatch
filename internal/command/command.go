package command

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// Command represents an external command that can be executed.
// It is a superset of bubbletea's tea.ExecCommand, so implementations
// can be passed directly to tea.Exec().
type Command interface {
	Run() error
	SetDir(string)
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

// Factory creates a Command for the given program name and arguments.
type Factory func(name string, args ...string) Command

// execAdapter wraps an *exec.Cmd to satisfy Command.
type execAdapter struct {
	cmd *exec.Cmd
}

func (a *execAdapter) Run() error            { return a.cmd.Run() }
func (a *execAdapter) SetDir(dir string)     { a.cmd.Dir = dir }
func (a *execAdapter) SetStdin(r io.Reader)  { a.cmd.Stdin = r }
func (a *execAdapter) SetStdout(w io.Writer) { a.cmd.Stdout = w }
func (a *execAdapter) SetStderr(w io.Writer) { a.cmd.Stderr = w }

// DefaultFactory creates commands via os/exec. When running under go test,
// it panics if asked to create a command for "gh" or "rwx" to prevent
// accidental API calls.
func DefaultFactory(name string, args ...string) Command {
	if testing.Testing() && (name == "gh" || name == "rwx") {
		panic(fmt.Sprintf("test called real %s command (use a stub factory): %s %s", name, name, strings.Join(args, " ")))
	}
	return &execAdapter{cmd: exec.Command(name, args...)}
}

// stubCommand is a Command that records setter calls and returns canned output.
type stubCommand struct {
	stdout string
	err    error
	dir    string
	stdin  io.Reader
	outW   io.Writer
	errW   io.Writer
}

// StubCommand returns a Command that writes stdout to whatever writer is set
// via SetStdout, and returns err from Run(). All setter methods record their
// arguments for test assertions.
func StubCommand(stdout string, err error) Command {
	return &stubCommand{stdout: stdout, err: err}
}

func (s *stubCommand) Run() error {
	if s.outW != nil && s.stdout != "" {
		io.Copy(s.outW, bytes.NewBufferString(s.stdout))
	}
	return s.err
}

func (s *stubCommand) SetDir(dir string)     { s.dir = dir }
func (s *stubCommand) SetStdin(r io.Reader)  { s.stdin = r }
func (s *stubCommand) SetStdout(w io.Writer) { s.outW = w }
func (s *stubCommand) SetStderr(w io.Writer) { s.errW = w }
