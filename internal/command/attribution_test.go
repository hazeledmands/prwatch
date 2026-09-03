package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

// TestIsDeadlineKill pins the attribution rule for every combination that can
// reach it. The interesting cases are the two that must NOT be called a
// timeout: a command that succeeded (its output is complete and must not be
// discarded) and a failure that never started a process (an exec-start error,
// which Cmd.Start returns before it consults the context at all, so it can
// arrive with an expired deadline attached and no relation to it).
func TestIsDeadlineKill(t *testing.T) {
	execErr := errors.New(`exec: "git": executable file not found in $PATH`)
	exitErr := fmt.Errorf("exit status 1")
	killErr := fmt.Errorf("signal: killed")

	tests := []struct {
		name       string
		runErr     error
		ranProcess bool
		ctxErr     error
		want       bool
	}{
		{
			name:       "success with deadline expired at the boundary",
			runErr:     nil,
			ranProcess: true,
			ctxErr:     context.DeadlineExceeded,
			want:       false, // output is complete; do not fabricate a failure
		},
		{
			name:       "success, no deadline",
			runErr:     nil,
			ranProcess: true,
			ctxErr:     nil,
			want:       false,
		},
		{
			name:       "process killed by the deadline",
			runErr:     killErr,
			ranProcess: true,
			ctxErr:     context.DeadlineExceeded,
			want:       true,
		},
		{
			name:       "exec-start failure with the deadline also expired",
			runErr:     execErr,
			ranProcess: false,
			ctxErr:     context.DeadlineExceeded,
			want:       false, // the missing binary is the story, not the clock
		},
		{
			name:       "exec-start failure, no deadline",
			runErr:     execErr,
			ranProcess: false,
			ctxErr:     nil,
			want:       false,
		},
		{
			name:       "ordinary non-zero exit, no deadline",
			runErr:     exitErr,
			ranProcess: true,
			ctxErr:     nil,
			want:       false,
		},
		{
			name:       "Start refused an already-expired context",
			runErr:     context.DeadlineExceeded,
			ranProcess: false,
			ctxErr:     context.DeadlineExceeded,
			want:       true, // never ran, but the clock is genuinely why
		},
		{
			name:       "WaitDelay expiry with no deadline",
			runErr:     exec.ErrWaitDelay,
			ranProcess: true,
			ctxErr:     nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeadlineKill(tt.runErr, tt.ranProcess, tt.ctxErr); got != tt.want {
				t.Errorf("isDeadlineKill(%v, %t, %v) = %t, want %t",
					tt.runErr, tt.ranProcess, tt.ctxErr, got, tt.want)
			}
		})
	}
}
