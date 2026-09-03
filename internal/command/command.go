package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
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

// DefaultTimeout bounds every background subprocess.
//
// 45s is chosen against the two things that constrain it. Below, the slowest
// legitimate background call: a cold `gh api graphql` or an `rwx logs`
// download over a bad link, which can take tens of seconds and must be
// allowed to finish rather than be killed and retried forever. Above, the 30s
// refresh tick: a hung process must be reaped on the order of one tick, not
// left to accumulate one wedged goroutine and one wedged process per tick
// indefinitely. 45s clears the first and stays within two of the second.
const DefaultTimeout = 45 * time.Second

// killGraceDelay is how long Run waits, after the deadline fires and the
// process is signalled, for the I/O goroutines to drain before it gives up on
// them. Without it a grandchild holding the stdout pipe open (xclip forking to
// own the selection, a `gh` child) can block Wait forever — the same hang the
// deadline exists to prevent.
const killGraceDelay = 5 * time.Second

// execAdapter builds an *exec.Cmd at Run() time and satisfies Command.
//
// Construction is deferred because a timed command's clock must start when the
// process starts, not when the Command value is made: call sites build a
// Command inside Update and may hand it to bubbletea to run later.
type execAdapter struct {
	name    string
	args    []string
	timeout time.Duration // 0 means untimed

	dir    string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// Timeout reports the deadline this command will be run under; 0 means the
// command is untimed (the interactive lane).
func (a *execAdapter) Timeout() time.Duration { return a.timeout }

func (a *execAdapter) Run() error {
	ctx := context.Background()
	if a.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, a.name, a.args...)
	cmd.Dir = a.dir
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	if a.timeout > 0 {
		cmd.WaitDelay = killGraceDelay
	}

	err := cmd.Run()
	if isDeadlineKill(err, cmd.ProcessState != nil, ctx.Err()) {
		// The deadline, not the program, is the story: report it as one, and
		// keep context.DeadlineExceeded in the chain so errors.Is works.
		return fmt.Errorf("%s timed out after %s: %w", a.name, a.timeout, ctx.Err())
	}
	return err
}

// isDeadlineKill reports whether a finished command's outcome should be told as
// a timeout rather than on its own terms. runErr is what Run returned,
// ranProcess whether a process was actually started and reaped, and ctxErr the
// state of the deadline afterwards.
//
// An expired deadline alone does not make a timeout, and the two cases where it
// lies are why this is a function rather than a bare `ctx.Err() != nil`:
//
//   - The command succeeded. Its output is fully captured; the deadline merely
//     expired in the window between Run returning and the check. Reporting a
//     timeout would invent a failure and throw that output away.
//   - The command never started — a binary missing from PATH, a fork that hit
//     EAGAIN. Cmd.Start returns those before it consults the context at all, so
//     they arrive with an expired deadline attached and no causal relation to
//     it. `exec: "git": executable file not found in $PATH` is the actionable
//     sentence; replacing it with the deadline story sends the reader looking
//     for a network problem that isn't there.
//
// A process the deadline actually killed comes back as an ExitError ("signal:
// killed"), not as a context error — hence ranProcess plus an expired ctxErr.
// The one exception is a context that had already expired before Start, which
// Start itself reports as the context error: no process ran, but the clock
// genuinely is the reason.
func isDeadlineKill(runErr error, ranProcess bool, ctxErr error) bool {
	if runErr == nil {
		return false
	}
	if errors.Is(runErr, context.DeadlineExceeded) {
		return true
	}
	return ranProcess && ctxErr != nil
}

func (a *execAdapter) SetDir(dir string)     { a.dir = dir }
func (a *execAdapter) SetStdin(r io.Reader)  { a.stdin = r }
func (a *execAdapter) SetStdout(w io.Writer) { a.stdout = w }
func (a *execAdapter) SetStderr(w io.Writer) { a.stderr = w }

// blockedInTests lists commands that must never be executed during tests.
// gh/rwx: external API calls. pbcopy/xclip: system clipboard side effects.
var blockedInTests = map[string]bool{
	"gh":     true,
	"rwx":    true,
	"pbcopy": true,
	"xclip":  true,
}

// DefaultFactory creates commands via os/exec, bounded by DefaultTimeout.
// This is the background lane and the default for every call site: a new
// caller that reaches for a factory without thinking about it gets a
// subprocess that cannot hang the app forever.
//
// When running under go test, it panics if asked to create a blocked command
// to prevent accidental API calls or system side effects.
func DefaultFactory(name string, args ...string) Command {
	return TimeoutFactory(DefaultTimeout)(name, args...)
}

// TimeoutFactory returns a Factory whose commands are killed once d elapses.
// A d of zero or less produces untimed commands.
func TimeoutFactory(d time.Duration) Factory {
	// Clamped out here, not inside the closure: the closure is shared by every
	// caller of the returned Factory, and those callers are tea.Cmd
	// goroutines, so assigning to the captured d would be a data race.
	if d < 0 {
		d = 0
	}
	return func(name string, args ...string) Command {
		if testing.Testing() && blockedInTests[name] {
			panic(fmt.Sprintf("test called real %s command (use a stub factory): %s %s", name, name, strings.Join(args, " ")))
		}
		return &execAdapter{name: name, args: args, timeout: d}
	}
}

// InteractiveFactory creates untimed commands, for the one lane where a
// deadline would be a bug: a process the user is sitting in front of. An
// $EDITOR session handed to tea.Exec can legitimately stay open for hours, and
// a browser opener may not return until the browser does.
//
// Use it only for programs run in the foreground with the TUI suspended.
// Everything else — anything dispatched from a tea.Cmd or a refresh tick —
// belongs on DefaultFactory.
func InteractiveFactory(name string, args ...string) Command {
	return TimeoutFactory(0)(name, args...)
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
