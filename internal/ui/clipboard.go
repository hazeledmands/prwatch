package ui

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/command"
)

// clipboardCopyMsg is the outcome of an async clipboard write.
//
// It carries okText — the toast computed at dispatch, from the text that was
// copied — rather than leaving the handler to recompute it. That is the same
// discipline rwxFetcher.Cmd follows: the closure captures locals only, and the
// result msg carries the inputs it was computed from, so the toast describes
// the copy that actually happened even if the selection has moved on since.
type clipboardCopyMsg struct {
	// tool is the program that was run ("pbcopy", "xclip"), or "" when the
	// platform has none.
	tool string
	// okText is the toast to show if err is nil.
	okText string
	err    error
}

// clipboardTool names the program that writes the system clipboard on goos,
// and reports whether there is one at all.
func clipboardTool(goos string) (name string, args []string, ok bool) {
	switch goos {
	case "darwin":
		return "pbcopy", nil, true
	case "linux":
		return "xclip", []string{"-selection", "clipboard"}, true
	default:
		return "", nil, false
	}
}

// clipboardToolName is the clipboard program on this platform, or "" if there
// is none. Exists so callers and tests can name the tool without repeating the
// GOOS switch.
func clipboardToolName() string {
	name, _, _ := clipboardTool(runtime.GOOS)
	return name
}

// copyToClipboardCmd returns a Cmd that writes text to the system clipboard
// and reports the outcome, so the toast can be gated on it.
//
// The copy used to run inline on the Update goroutine, which meant a wedged
// pbcopy — or an xclip fork holding the pipe — froze the entire TUI for the
// command timeout plus its grace, with no way to redraw or quit. It is
// background work, so it stays on the timed lane; the deadline is what bounds
// the wedge now, and it no longer bounds anything the user is waiting on.
//
// factory and okText are snapshotted by the caller inside Update; the closure
// below reads neither Model nor anything else that Update can mutate.
func copyToClipboardCmd(factory command.Factory, text, okText string) tea.Cmd {
	goos := runtime.GOOS
	return func() tea.Msg {
		return clipboardResult(goos, factory, text, okText)
	}
}

// clipboardResult performs the copy and describes the outcome. Split from the
// closure so the result can be exercised without a goroutine, and so the
// platform is a parameter rather than a build-time fact.
func clipboardResult(goos string, factory command.Factory, text, okText string) clipboardCopyMsg {
	name, args, ok := clipboardTool(goos)
	if !ok {
		return clipboardCopyMsg{okText: okText, err: fmt.Errorf("no clipboard tool for %s", goos)}
	}
	cmd := factory(name, args...)
	cmd.SetStdin(strings.NewReader(text))
	return clipboardCopyMsg{tool: name, okText: okText, err: cmd.Run()}
}

// clipboardToastFor is the toast text for a finished copy.
//
// The failure wording follows the conventions githubErrorKind already sets: a
// missing binary gets fixed text naming the tool and the remedy, because
// exec's `executable file not found in $PATH` names the cause but not the
// consequence; everything else carries the raw error, which is the only thing
// that distinguishes a broken pipe from a permission failure.
func clipboardToastFor(msg clipboardCopyMsg) string {
	if msg.err == nil {
		return msg.okText
	}
	if errors.Is(msg.err, command.ErrNotFound) && msg.tool != "" {
		return msg.tool + " not installed — nothing copied"
	}
	return "copy failed: " + msg.err.Error()
}
