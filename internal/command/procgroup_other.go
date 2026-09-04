//go:build !unix

package command

import "os/exec"

// superviseProcessGroup is a no-op on platforms without POSIX process groups.
// Windows has job objects, which are a different enough mechanism to be worth
// its own change if prwatch is ever run there; until then the timed lane keeps
// exec.CommandContext's default behaviour of killing the direct child, bounded
// by WaitDelay.
func superviseProcessGroup(cmd *exec.Cmd) {}
