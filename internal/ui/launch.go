package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/command"
)

// launchMsg is the outcome of launching a program on the user's behalf — a GUI
// editor, a browser opener, or a terminal editor the user has just exited.
//
// It carries name so the failure toast can say which program could not be
// started: by the time this lands the launch site is long gone, and exec's
// error text alone ("executable file not found in $PATH") names the cause but
// not the thing the user asked for.
type launchMsg struct {
	// name is the program that was launched.
	name string
	// refresh asks for a working-tree reload once the program finishes. Only a
	// terminal editor sets it — a GUI launcher returning means the application
	// has been signalled, not that anything was edited.
	refresh bool
	err     error
}

// spawnCmd returns a Cmd that runs a program off the Update goroutine and
// reports the outcome, without suspending the TUI.
//
// This is the non-suspending half of PROMPT.md's "terminal vs. GUI": a GUI
// editor's CLI returns as soon as the running application has been signalled,
// so handing it to tea.Exec blanks and redraws the screen for nothing.
//
// The factory is the caller's choice, because the two callers differ on
// whether a deadline is safe: a browser opener has argv this code controls and
// always returns promptly, so it is bounded; a GUI editor is the user's
// $EDITOR, and a `-w` they put in it blocks for as long as the window is open,
// so a deadline there would kill a live edit.
//
// Every input is a parameter, never read from Model inside the closure: the
// Cmd runs on bubbletea's goroutine, where m.* is a data race.
func spawnCmd(factory command.Factory, dir, name string, args []string, refresh bool) tea.Cmd {
	// Cloned because the caller's slice may be reused or appended to after
	// this returns, and the closure outlives the call.
	spawnArgs := append([]string(nil), args...)
	return func() tea.Msg {
		cmd := factory(name, spawnArgs...)
		cmd.SetDir(dir)
		return launchMsg{name: name, refresh: refresh, err: cmd.Run()}
	}
}

// launchToastFor is the status-bar text for a finished launch, or "" when
// there is nothing worth saying.
//
// The wording follows clipboardToastFor: a missing binary gets fixed text
// naming the program, because that is the one failure whose remedy the user
// can act on directly; everything else carries the raw error, which is the
// only thing that distinguishes a crash from a permission failure.
func launchToastFor(msg launchMsg) string {
	if msg.err == nil {
		return ""
	}
	if errors.Is(msg.err, command.ErrNotFound) {
		return msg.name + " not found — is it installed and on $PATH?"
	}
	return "could not launch " + msg.name + ": " + msg.err.Error()
}
