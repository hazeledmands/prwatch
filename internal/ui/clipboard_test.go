package ui

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// clipFactory returns a factory whose clipboard commands fail with err (or
// succeed when err is nil), delegating everything else to the test default.
func clipFactory(err error) command.Factory {
	return func(name string, args ...string) command.Command {
		if name == "pbcopy" || name == "xclip" {
			return command.StubCommand("", err)
		}
		return noGHFactory(name, args...)
	}
}

// blockingCommand stands in for a wedged pbcopy: Run does not return until
// release is closed.
type blockingCommand struct{ release <-chan struct{} }

func (b *blockingCommand) Run() error {
	<-b.release
	return nil
}

func (b *blockingCommand) SetDir(string)       {}
func (b *blockingCommand) SetStdin(io.Reader)  {}
func (b *blockingCommand) SetStdout(io.Writer) {}
func (b *blockingCommand) SetStderr(io.Writer) {}

// settleClipboard runs a Cmd returned by Update, feeds any clipboardCopyMsg it
// produces back through Update, and returns the resulting model. This is the
// round-trip the real event loop performs: the copy runs off the Update
// goroutine, and the toast is decided when its result lands.
func settleClipboard(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	ccMsg, ok := msg.(clipboardCopyMsg)
	if !ok {
		t.Fatalf("expected the copy path to return a clipboardCopyMsg, got %T", msg)
	}
	res, _ := m.Update(ccMsg)
	return res.(*Model)
}

// clipboardTestModel builds a model showing a file, with the clipboard lane
// wired to fail or succeed as err says.
func clipboardTestModel(t *testing.T, err error) *Model {
	t.Helper()
	mg := &mockGit{
		repoInfo:     git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:         "abc",
		changedFiles: git.ChangedFilesResult{Committed: []string{"config.go"}},
		commits:      []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent:  "short\nthis is a longer line of content\nthird line here",
	}
	m := NewModel("/tmp", mg)
	m.cmdFactory = clipFactory(err)
	m.width, m.height = 80, 24
	m.updateLayout()
	m.Update(m.loadGitData())
	m.focus = SidebarFocus
	m.updateSidebarItems()
	return m
}

// The toast used to be set unconditionally before the command ran, so a copy
// that failed — no pbcopy on PATH, a wedged xclip, a full pipe — still told the
// user it had been copied. Nothing about the copy was checked at all.
func TestClipboard_FailedCopyReportsFailure(t *testing.T) {
	m := clipboardTestModel(t, fmt.Errorf("broken pipe"))

	res, cmd := m.Update(keyMsg("y"))
	m = res.(*Model)
	m = settleClipboard(t, m, cmd)

	got := m.notifications.Text()
	if strings.HasPrefix(got, "copied") {
		t.Errorf("notification = %q, want a failure message — the copy failed", got)
	}
	if !strings.Contains(got, "broken pipe") {
		t.Errorf("notification = %q, want it to name the failure", got)
	}
}

// The dispatch itself must say nothing: until the result lands there is no
// news, and announcing success at dispatch is exactly the bug.
func TestClipboard_NoToastBeforeResult(t *testing.T) {
	m := clipboardTestModel(t, nil)

	res, cmd := m.Update(keyMsg("y"))
	m = res.(*Model)

	if got := m.notifications.Text(); got != "" {
		t.Errorf("notification = %q immediately after the keypress, want none until the copy reports back", got)
	}
	if cmd == nil {
		t.Fatal("the yank must dispatch a copy Cmd")
	}
}

// A successful copy keeps the wording PROMPT.md specifies.
func TestClipboard_SuccessKeepsSpecWording(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(m *Model) (tea.Model, tea.Cmd)
		want   func(string) bool
		desc   string
	}{
		{
			name: "yank-path names what was copied",
			invoke: func(m *Model) (tea.Model, tea.Cmd) {
				m.focus = SidebarFocus
				return m.Update(keyMsg("y"))
			},
			want: func(s string) bool { return strings.Contains(s, "config.go") },
			desc: "a toast naming the copied path",
		},
		{
			name: "drag copy uses the selection form",
			invoke: func(m *Model) (tea.Model, tea.Cmd) {
				setDragRect(m, 30, 5, 50, 7)
				return m, m.copySelection()
			},
			want: copySelectionNotificationRE.MatchString,
			desc: "copied selection (N lines, M bytes)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := clipboardTestModel(t, nil)
			res, cmd := tt.invoke(m)
			m = res.(*Model)
			m = settleClipboard(t, m, cmd)

			if got := m.notifications.Text(); !tt.want(got) {
				t.Errorf("notification = %q, want %s", got, tt.desc)
			}
		})
	}
}

// A missing clipboard binary is the one failure with an obvious remedy, so it
// gets fixed text naming it rather than exec's raw $PATH sentence — the same
// call the gh-missing status message makes.
func TestClipboard_MissingToolNamesTheTool(t *testing.T) {
	m := clipboardTestModel(t, fmt.Errorf("exec: %w", command.ErrNotFound))

	res, cmd := m.Update(keyMsg("y"))
	m = res.(*Model)
	m = settleClipboard(t, m, cmd)

	got := m.notifications.Text()
	if !strings.Contains(got, "not installed") {
		t.Errorf("notification = %q, want it to say the clipboard tool is not installed", got)
	}
	if !strings.Contains(got, clipboardToolName()) {
		t.Errorf("notification = %q, want it to name %q", got, clipboardToolName())
	}
}

// Platforms with no clipboard tool must not claim a copy happened either.
func TestClipboard_UnsupportedPlatformReportsFailure(t *testing.T) {
	if _, _, ok := clipboardTool("plan9"); ok {
		t.Fatal("clipboardTool claims a tool for plan9")
	}

	msg := clipboardResult("plan9", noGHFactory, "text", "copied text")
	if msg.err == nil {
		t.Fatal("a platform with no clipboard tool must report a failure, not success")
	}
	if !strings.Contains(clipboardToastFor(msg), "plan9") {
		t.Errorf("toast = %q, want it to name the unsupported platform", clipboardToastFor(msg))
	}
}

// The wedged-clipboard case that motivated moving the copy off the event loop:
// a pbcopy that never returns froze the whole TUI for the command timeout plus
// its grace. Update must hand back a Cmd promptly and let bubbletea run it.
func TestClipboard_WedgedCopyDoesNotBlockUpdate(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	m := clipboardTestModel(t, nil)
	m.cmdFactory = func(name string, args ...string) command.Command {
		if name == "pbcopy" || name == "xclip" {
			return &blockingCommand{release: release}
		}
		return noGHFactory(name, args...)
	}

	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := m.Update(keyMsg("y"))
		done <- cmd
	}()

	select {
	case cmd := <-done:
		if cmd == nil {
			t.Fatal("the yank must dispatch a copy Cmd")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked on the clipboard command; the copy must run in a Cmd, not inline")
	}
}

// House rule: a Cmd closure must not read Model state. The copy Cmd is
// therefore built from locals and must still work — and still carry the toast
// it was computed for — after the Model has moved on.
func TestClipboard_CmdIsIndependentOfModelState(t *testing.T) {
	m := clipboardTestModel(t, nil)

	res, cmd := m.Update(keyMsg("y"))
	m = res.(*Model)
	if cmd == nil {
		t.Fatal("the yank must dispatch a copy Cmd")
	}

	// Everything the closure might have been tempted to read, moved on.
	m.focus = MainFocus
	m.sidebar.SetItems(nil)
	m.lastMainItem = mainItemKey{}
	m.notifications.Show("something else")

	msg := cmd().(clipboardCopyMsg)
	if !strings.Contains(msg.okText, "config.go") {
		t.Errorf("msg.okText = %q, want the toast computed at dispatch for config.go", msg.okText)
	}
	if msg.err != nil {
		t.Errorf("msg.err = %v, want the copy to have succeeded", msg.err)
	}
}
