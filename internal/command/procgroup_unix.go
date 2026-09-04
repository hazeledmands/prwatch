//go:build unix

package command

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// killGroupGrace is how long the process group gets to act on SIGTERM before
// the SIGKILL lands.
//
// It has to fit inside killGraceDelay (the exec.Cmd WaitDelay): past that, Wait
// gives up on the I/O goroutines and Run returns, so a SIGKILL scheduled later
// would be escalating against a command nobody is waiting for any more. 2s
// inside 5s leaves Wait three seconds to observe the killed processes and
// finish normally.
const killGroupGrace = 2 * time.Second

// superviseProcessGroup makes the deadline apply to the whole process tree
// rather than just the process we forked.
//
// Setpgid puts the child in a new process group with its pid as the group id,
// so every descendant it forks — a credential helper or ssh under `git`, a
// pager under `gh` — inherits that group and can be signalled as a unit with a
// negative pid. Without it, exec.CommandContext's default Cancel kills only the
// immediate child, and the grandchildren keep running past the deadline still
// holding the stdout pipe the deadline exists to release. WaitDelay does not
// cover this: it bounds how long *we* wait, not how long they live.
//
// Only ever called for the timed lane. The interactive lane must stay in the
// caller's process group, because a foreground $EDITOR needs to be in the
// terminal's foreground group for job control to work.
func superviseProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Cancel runs on the os/exec watchdog goroutine once the context is done,
	// and only after Start has succeeded, so cmd.Process is set and stable by
	// then — this is the same field the stdlib's own default Cancel reads.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Setpgid made the child a group leader, so its pid is the group id.
		pgid := cmd.Process.Pid

		// SIGTERM before SIGKILL, for git's sake. git installs handlers that
		// unlink the lock files it holds (index.lock, a ref's .lock) when
		// terminated, and a SIGKILL mid-write leaves those behind — every later
		// git call in the repo then fails with "Another git process seems to be
		// running" until someone removes them by hand. Turning a 45s timeout
		// into a wedged working copy is a far worse outcome than waiting two
		// more seconds. gh and rwx hold no locks and lose nothing either way.
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				// Whole group is already gone.
				return os.ErrProcessDone
			}
			return err
		}

		// Escalate unconditionally rather than cancelling this when the direct
		// child exits: the case that motivates the whole change is a grandchild
		// that outlives its parent, and a credential helper sitting on a prompt
		// may well ignore SIGTERM after its parent has gone. Signalling a
		// recycled group id is the theoretical cost, and it stays theoretical —
		// pids are handed out sequentially, so reuse inside two seconds would
		// mean cycling the entire pid space.
		time.AfterFunc(killGroupGrace, func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		})
		return nil
	}
}
