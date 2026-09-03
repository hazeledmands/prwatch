package command_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/command"
)

// TestFactoryTimeouts covers the two lanes: background commands are killed
// once their deadline passes, interactive ones run as long as the user wants.
func TestFactoryTimeouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		factory     command.Factory
		sleep       string // seconds, passed to sh -c "sleep N"
		wantTimeout bool
	}{
		{
			name:        "background command past deadline is killed",
			factory:     command.TimeoutFactory(50 * time.Millisecond),
			sleep:       "30",
			wantTimeout: true,
		},
		{
			name:        "background command inside deadline succeeds",
			factory:     command.TimeoutFactory(10 * time.Second),
			sleep:       "0",
			wantTimeout: false,
		},
		{
			name:        "interactive command is never timed out",
			factory:     command.InteractiveFactory,
			sleep:       "0.3",
			wantTimeout: false,
		},
		{
			name:        "non-positive timeout means no timeout",
			factory:     command.TimeoutFactory(0),
			sleep:       "0.3",
			wantTimeout: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.factory("sh", "-c", "sleep "+tt.sleep)
			start := time.Now()
			err := cmd.Run()
			elapsed := time.Since(start)

			if !tt.wantTimeout {
				if err != nil {
					t.Fatalf("Run() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Run() = nil after %s, want a timeout error", elapsed)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Run() = %v, want an error wrapping context.DeadlineExceeded", err)
			}
			if !strings.Contains(err.Error(), "timed out") {
				t.Errorf("Run() = %q, want the message to say it timed out", err)
			}
			if elapsed > 5*time.Second {
				t.Errorf("Run() took %s; the process was not killed at the deadline", elapsed)
			}
		})
	}
}

// TestExecFailureKeepsItsOwnMessage covers the attribution trap: an exec-start
// failure (binary not on PATH) is returned by Cmd.Start before the context is
// ever consulted, so it can surface with an already-expired deadline. The
// caller must be told the binary is missing — reporting a timeout would send
// them looking for a network problem that does not exist.
func TestExecFailureKeepsItsOwnMessage(t *testing.T) {
	t.Parallel()

	// 1ns: the deadline is guaranteed to have expired by the time Run looks.
	cmd := command.TimeoutFactory(time.Nanosecond)("prwatch-no-such-binary-xyz")
	err := cmd.Run()
	if err == nil {
		t.Fatal("Run() = nil, want an exec failure")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() = %v, want the exec failure, not a fabricated timeout", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Run() = %q, want the message to name the missing executable", err)
	}
}

// TestTimeoutKillsProcess proves the subprocess is actually killed rather than
// merely abandoned: the command would create a file after its sleep, and that
// file must never appear.
func TestTimeoutKillsProcess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")

	cmd := command.TimeoutFactory(50*time.Millisecond)("sh", "-c", "sleep 1; touch "+marker)
	if err := cmd.Run(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("subprocess kept running past its deadline: marker file was created")
	}
}

// TestDefaultFactoryIsTimed pins the structural default: anything built through
// DefaultFactory inherits the background timeout.
func TestDefaultFactoryIsTimed(t *testing.T) {
	t.Parallel()

	if command.DefaultTimeout < 30*time.Second || command.DefaultTimeout > 60*time.Second {
		t.Errorf("DefaultTimeout = %s, want something in the 30s–60s band", command.DefaultTimeout)
	}

	type timeouter interface{ Timeout() time.Duration }

	cmd := command.DefaultFactory("sh", "-c", "true")
	tc, ok := cmd.(timeouter)
	if !ok {
		t.Fatalf("DefaultFactory returned %T, which reports no timeout", cmd)
	}
	if got := tc.Timeout(); got != command.DefaultTimeout {
		t.Errorf("DefaultFactory timeout = %s, want %s", got, command.DefaultTimeout)
	}

	icmd := command.InteractiveFactory("sh", "-c", "true")
	itc, ok := icmd.(timeouter)
	if !ok {
		t.Fatalf("InteractiveFactory returned %T, which reports no timeout", icmd)
	}
	if got := itc.Timeout(); got != 0 {
		t.Errorf("InteractiveFactory timeout = %s, want 0 (untimed)", got)
	}
}

// TestTimedCommandPlumbing checks the lazily-built exec.Cmd still honours every
// setter on the Command interface.
func TestTimedCommandPlumbing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	cmd := command.DefaultFactory("sh", "-c", "cat; ls; echo oops >&2")
	cmd.SetDir(dir)
	cmd.SetStdin(strings.NewReader("stdin-line\n"))
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(stdout.String(), "stdin-line") {
		t.Errorf("stdout = %q, want it to contain the stdin passthrough", stdout.String())
	}
	if !strings.Contains(stdout.String(), "hello.txt") {
		t.Errorf("stdout = %q, want it to list the working directory", stdout.String())
	}
	if !strings.Contains(stderr.String(), "oops") {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), "oops")
	}
}

// TestBlockedCommandsPanicInBothLanes keeps the test-safety guard on the
// interactive lane too.
func TestBlockedCommandsPanicInBothLanes(t *testing.T) {
	t.Parallel()

	lanes := map[string]command.Factory{
		"default":     command.DefaultFactory,
		"interactive": command.InteractiveFactory,
		"timeout":     command.TimeoutFactory(time.Second),
	}
	for name, factory := range lanes {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic when constructing a blocked command under test")
				}
			}()
			factory("gh", "pr", "view")
		})
	}
}
