package git_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// stderrStub is a Command that writes to stderr before failing, which
// command.StubCommand cannot do. runExternal folds stderr into the error it
// returns, so this is the shape that exercises that folding.
type stderrStub struct {
	stderrText string
	err        error
	errW       io.Writer
}

func (s *stderrStub) Run() error {
	if s.errW != nil && s.stderrText != "" {
		fmt.Fprint(s.errW, s.stderrText)
	}
	return s.err
}

func (s *stderrStub) SetDir(string)         {}
func (s *stderrStub) SetStdin(io.Reader)    {}
func (s *stderrStub) SetStdout(io.Writer)   {}
func (s *stderrStub) SetStderr(w io.Writer) { s.errW = w }

// TestExternalTimeoutErrorSurfaces checks that a subprocess killed by its
// deadline reaches the caller as a real, readable error — not swallowed as
// "no PR here", and not stripped of context.DeadlineExceeded.
func TestExternalTimeoutErrorSurfaces(t *testing.T) {
	timeoutErr := fmt.Errorf("gh timed out after 45s: %w", context.DeadlineExceeded)

	tests := []struct {
		name   string
		stderr string
	}{
		{name: "no stderr", stderr: ""},
		{name: "with stderr", stderr: "some gh chatter\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := git.NewWithFactory("/tmp", func(name string, args ...string) command.Command {
				if name == "gh" {
					return &stderrStub{stderrText: tt.stderr, err: timeoutErr}
				}
				return command.StubCommand("", fmt.Errorf("unexpected command: %s", name))
			})

			_, err := g.PRAll()
			if err == nil {
				t.Fatal("PRAll() = nil error; a timeout must not be swallowed")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("PRAll() error %v does not wrap context.DeadlineExceeded", err)
			}
			if !strings.Contains(err.Error(), "timed out") {
				t.Errorf("PRAll() error = %q, want it to say it timed out", err)
			}
		})
	}
}
