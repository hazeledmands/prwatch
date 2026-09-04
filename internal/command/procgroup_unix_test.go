//go:build unix

package command_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/command"
)

// TestTimeoutKillsGrandchildren is the process-tree half of the deadline
// guarantee. `git` forks a credential helper or ssh, `gh` forks a pager; killing
// only the immediate child leaves those running past the deadline, still
// holding the pipes the deadline exists to release.
//
// The command spawns a grandchild that records its pid and would create a
// marker file well after the deadline, then sleeps past the deadline itself.
// After Run returns, the grandchild must be gone and the marker absent.
func TestTimeoutKillsGrandchildren(t *testing.T) {
	t.Parallel()
	requireShell(t)

	dir := t.TempDir()
	marker := filepath.Join(dir, "grandchild-survived")
	pidFile := filepath.Join(dir, "grandchild.pid")

	// Two grandchildren, because one cannot answer both questions. The
	// long-lived one is the liveness probe: its sleep outlasts the poll window,
	// so it can only disappear by being killed — an earlier version of this test
	// slept 3s and passed spuriously when the grandchild simply finished. The
	// short-lived one is the side-effect probe: it would touch the marker file
	// after the deadline, which catches a pid that was recycled rather than
	// reaped. Both sleeps are bounded well under a minute so that a test that
	// bails out early cannot leave anything around for long.
	script := "sh -c 'sleep 30' & echo $! > " + pidFile +
		"; sh -c 'sleep 2; touch " + marker + "' &" +
		" sleep 30"
	// 1s, not the 150ms this started at: the script has to fork twice and write
	// the pid file before the deadline fires. On a loaded box a tighter deadline
	// signals the shell first, and the test then fails on a missing pid file —
	// a confusing "no such file" for what is really a timing problem. The
	// side-effect probe (2s) and the post-check (3s) still straddle 1s.
	cmd := command.TimeoutFactory(time.Second)("sh", "-c", script)
	if err := cmd.Run(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
	}

	// Registered before the pid is read, so that a failure to read it does not
	// walk out on a live grandchild. Reads the variable at cleanup time.
	var pid int
	t.Cleanup(func() {
		if pid != 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	pid = readPID(t, pidFile)

	// Generous margin: the grandchild may need a moment to be signalled and
	// reaped by init after its parent dies. A loaded CI box gets the whole
	// window; a working implementation takes single-digit milliseconds.
	deadline := time.Now().Add(10 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid %d still alive 10s after Run returned; only the direct child was reaped", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Past the short-lived grandchild's own sleep, so the absence of the marker
	// means it never got to run rather than that we looked too early.
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("grandchild kept running past the deadline: marker file was created")
	}
}

// TestInteractiveLaneKeepsCallerProcessGroup guards the other side of the
// split. An $EDITOR handed to tea.Exec needs the terminal's foreground process
// group for job control (^Z, ^C); putting it in its own group breaks that. Only
// the timed lane gets Setpgid.
func TestInteractiveLaneKeepsCallerProcessGroup(t *testing.T) {
	t.Parallel()
	requireShell(t)

	own := syscall.Getpgrp()

	tests := []struct {
		name     string
		factory  command.Factory
		wantOwn  bool
		whyNotOK string
	}{
		{
			name:     "interactive lane inherits the caller's group",
			factory:  command.InteractiveFactory,
			wantOwn:  true,
			whyNotOK: "the interactive lane must stay in the caller's process group so job control works",
		},
		{
			name:     "timed lane gets its own group",
			factory:  command.TimeoutFactory(10 * time.Second),
			wantOwn:  false,
			whyNotOK: "the timed lane needs its own process group so the deadline can signal the whole tree",
		},
		{
			name:     "untimed TimeoutFactory inherits the caller's group",
			factory:  command.TimeoutFactory(0),
			wantOwn:  true,
			whyNotOK: "an untimed command has no deadline to enforce, so it must not be moved out of the caller's group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			cmd := tt.factory("sh", "-c", "ps -o pgid= -p $$")
			cmd.SetStdout(&out)
			if err := cmd.Run(); err != nil {
				t.Fatalf("Run() = %v", err)
			}
			got, err := strconv.Atoi(strings.TrimSpace(out.String()))
			if err != nil {
				t.Fatalf("parsing pgid from %q: %v", out.String(), err)
			}
			if (got == own) != tt.wantOwn {
				t.Errorf("child pgid = %d, caller pgid = %d: %s", got, own, tt.whyNotOK)
			}
		})
	}
}

// requireShell skips rather than fails where these tests' fixtures cannot run.
// Both drive `sh` and `ps` to observe process groups from the outside; a
// stripped container has neither, and the bare failure is an unhelpful `exit
// status 126`.
func requireShell(t *testing.T) {
	t.Helper()
	for _, prog := range []string{"sh", "ps"} {
		if _, err := exec.LookPath(prog); err != nil {
			t.Skipf("%s not available: %v", prog, err)
		}
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading grandchild pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("parsing grandchild pid from %q: %v", b, err)
	}
	return pid
}

// processAlive reports whether pid still names a live (or zombie-but-unreaped)
// process. Signal 0 performs error checking without delivering a signal.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
