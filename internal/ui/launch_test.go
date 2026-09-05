package ui

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"io"

	"github.com/hazeledmands/prwatch/internal/command"
)

// launchProbe is a recording factory that also captures the argv and the dir.
type launchProbe struct {
	argv []string
	dir  string
	err  error
}

func (p *launchProbe) factory() command.Factory {
	return func(name string, args ...string) command.Command {
		p.argv = append([]string{name}, args...)
		return &dirRecordingCommand{probe: p, err: p.err}
	}
}

type dirRecordingCommand struct {
	probe *launchProbe
	err   error
}

func (c *dirRecordingCommand) Run() error            { return c.err }
func (c *dirRecordingCommand) SetDir(dir string)     { c.probe.dir = dir }
func (c *dirRecordingCommand) SetStdin(_ io.Reader)  {}
func (c *dirRecordingCommand) SetStdout(_ io.Writer) {}
func (c *dirRecordingCommand) SetStderr(_ io.Writer) {}

// editorModel builds a files-mode model with a file on screen.
func editorModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel("/tmp/repo", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.mode = FilesMode
	m.focus = MainFocus
	m.updateLayout()
	// Enough lines that the viewport can actually scroll: SetYOffset clamps to
	// the content, so a three-line fixture silently pins the line number at 1.
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	m.mainPane.SetPlainContent(strings.Join(lines, "\n"))
	m.lastMainItem = mainItemKey{FilesMode, "pkg/a.go"}
	return m
}

// TestOpenEditor_TerminalEditorSuspends pins PROMPT.md's "terminal vs. GUI":
// a terminal editor runs in the foreground with the TUI suspended, which here
// means it is built inside openEditor and handed to tea.Exec — bubbletea runs
// it, so the command exists before any Cmd does.
func TestOpenEditor_TerminalEditorSuspends(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	m := editorModel(t)
	var background []string
	probe := &launchProbe{}
	m.cmdFactory = recordingFactory(&background)
	m.interactiveFactory = probe.factory()

	cmd := m.openEditor()
	if cmd == nil {
		t.Fatal("openEditor returned nil with a file on screen")
	}
	want := []string{"nvim", "+1", "--", "pkg/a.go"}
	if !slices.Equal(probe.argv, want) {
		t.Errorf("argv built during openEditor = %v, want %v — a terminal editor must reach tea.Exec", probe.argv, want)
	}
	if probe.dir != "/tmp/repo" {
		t.Errorf("dir = %q, want %q", probe.dir, "/tmp/repo")
	}
	if len(background) != 0 {
		t.Errorf("terminal editor used the timed background lane: %v", background)
	}
}

// TestOpenEditor_GUIEditorDoesNotSuspend is the other half: a GUI editor is
// spawned from a Cmd instead of tea.Exec, so the TUI is not blanked and
// redrawn for a launcher that returns as soon as the app is signalled.
//
// The observable difference from the terminal path is *when* the command is
// built: tea.Exec needs it up front, a background spawn builds it on the Cmd's
// own goroutine.
func TestOpenEditor_GUIEditorDoesNotSuspend(t *testing.T) {
	t.Setenv("EDITOR", "code")
	m := editorModel(t)
	var background []string
	probe := &launchProbe{}
	m.cmdFactory = recordingFactory(&background)
	m.interactiveFactory = probe.factory()

	cmd := m.openEditor()
	if cmd == nil {
		t.Fatal("openEditor returned nil with a file on screen")
	}
	if probe.argv != nil {
		t.Errorf("GUI editor was built on the Update goroutine (%v) — that is the tea.Exec path", probe.argv)
	}

	msg := cmd()
	want := []string{"code", "--goto", "--", "pkg/a.go:1"}
	if !slices.Equal(probe.argv, want) {
		t.Errorf("argv = %v, want %v", probe.argv, want)
	}
	if probe.dir != "/tmp/repo" {
		t.Errorf("dir = %q, want %q", probe.dir, "/tmp/repo")
	}
	if len(background) != 0 {
		// $EDITOR is the user's: a `-w` they put there makes the launcher block
		// for as long as the window is open, so the spawn must be untimed.
		t.Errorf("GUI editor used the timed background lane: %v", background)
	}
	lm, ok := msg.(launchMsg)
	if !ok {
		t.Fatalf("Cmd returned %T, want launchMsg", msg)
	}
	if lm.err != nil {
		t.Errorf("launchMsg.err = %v, want nil", lm.err)
	}
	if lm.refresh {
		t.Error("a GUI editor spawn should not ask for a refresh: nothing has finished")
	}
}

// TestOpenEditor_LineNumberReachesGUIPreset checks the viewport line survives
// the trip through the preset for a non-`+N` editor, and that a wait flag the
// user put in $EDITOR is dropped on the way — PROMPT.md's "waiting".
//
// goland also stands in for the one family that takes no `--` guard.
func TestOpenEditor_LineNumberReachesGUIPreset(t *testing.T) {
	t.Setenv("EDITOR", "goland.sh -w")
	m := editorModel(t)
	probe := &launchProbe{}
	m.interactiveFactory = probe.factory()
	m.mainPane.viewport.SetYOffset(2)

	cmd := m.openEditor()
	if cmd == nil {
		t.Fatal("openEditor returned nil")
	}
	cmd()
	want := "goland.sh --line 3 pkg/a.go"
	if got := strings.Join(probe.argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// TestLaunchFailureSurfacesInStatusBar pins PROMPT.md's "failures": an editor
// that cannot be launched says so in the status bar rather than failing
// silently.
func TestLaunchFailureSurfacesInStatusBar(t *testing.T) {
	tests := []struct {
		name string
		msg  launchMsg
		want string
	}{
		{
			name: "missing binary names the tool",
			msg:  launchMsg{name: "code", err: fmt.Errorf("exec: %q: %w", "code", command.ErrNotFound)},
			want: "code not found",
		},
		{
			name: "other failures carry the error",
			msg:  launchMsg{name: "vim", refresh: true, err: errors.New("exit status 1")},
			want: "exit status 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := editorModel(t)
			result, _ := m.Update(tt.msg)
			m = result.(*Model)
			if got := m.notifications.Text(); !strings.Contains(got, tt.want) {
				t.Errorf("notification = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// TestLaunchSuccessRefreshesOnlyWhenAsked: a terminal editor exiting is a
// reason to reload the working tree; a GUI launcher returning is not, and
// neither is a browser opener.
func TestLaunchSuccessRefreshesOnlyWhenAsked(t *testing.T) {
	for _, refresh := range []bool{true, false} {
		m := editorModel(t)
		_, cmd := m.Update(launchMsg{name: "vim", refresh: refresh})
		if refresh && cmd == nil {
			t.Error("a terminal editor exiting should trigger a refresh")
		}
		if !refresh && cmd != nil {
			t.Errorf("a background launch should not trigger a refresh, got %T", cmd())
		}
	}
}

// TestOpenInBrowser_DoesNotSuspend: `open`/`xdg-open` return as soon as the
// browser is signalled, so they get the same background spawn a GUI editor
// does.
func TestOpenInBrowser_DoesNotSuspend(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("no browser opener on %s", runtime.GOOS)
	}
	m := editorModel(t)
	var interactive []string
	probe := &launchProbe{}
	m.cmdFactory = probe.factory()
	m.interactiveFactory = recordingFactory(&interactive)

	cmd := m.openInBrowser("https://example.com/pr/1")
	if cmd == nil {
		t.Fatal("openInBrowser returned nil")
	}
	msg := cmd()
	if len(interactive) != 0 {
		t.Errorf("browser opener used the interactive lane: %v", interactive)
	}
	if len(probe.argv) != 2 || probe.argv[1] != "https://example.com/pr/1" {
		t.Errorf("argv = %v, want an opener and the URL", probe.argv)
	}
	if lm, ok := msg.(launchMsg); !ok || lm.refresh {
		t.Errorf("browser launch produced %#v, want a launchMsg that does not refresh", msg)
	}
}
