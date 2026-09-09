package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/command"
)

// selectSidebarFileRow moves the sidebar highlight onto a file row.
func selectSidebarFileRow(t *testing.T, m *Model, path string) {
	t.Helper()
	for i, it := range m.sidebar.items {
		if it.filePath == path && !it.isDir {
			m.sidebar.SelectIndex(i)
			m.updateMainContent()
			return
		}
	}
	t.Fatalf("no sidebar row for %q; items: %v", path, sidebarLabels(m))
}

// paneOwnerModel leaves the sidebar on a directory while the pane still shows
// pkg/a.go — exactly the disagreement `e` resolves by focus. Main focus takes
// the displayed file, and carries the viewport's line.
func TestEditorTarget_MainFocusUsesTheDisplayedFile(t *testing.T) {
	m, wantFile := paneOwnerModel(t)
	m.mainPane.viewport.SetYOffset(4)

	target, ok := m.editorTargetForFocus()
	if !ok {
		t.Fatal("main focus went inert while a real file filled the pane")
	}
	if target.file != wantFile {
		t.Errorf("target file = %q, want the displayed %q", target.file, wantFile)
	}
	if target.line <= 1 {
		t.Errorf("target line = %d, want the scrolled viewport's line", target.line)
	}
}

// Sidebar focus on the same model: the highlight is a directory, so there is
// nothing to open. Inert, the way yank-path is.
func TestEditorTarget_SidebarFocusIsInertOnADirectory(t *testing.T) {
	m, _ := paneOwnerModel(t)
	m.focus = SidebarFocus

	if !m.sidebar.SelectedIsDir() {
		t.Fatal("fixture no longer leaves the sidebar on a directory")
	}
	if target, ok := m.editorTargetForFocus(); ok {
		t.Errorf("sidebar focus produced target %+v for a directory; want inert", target)
	}
}

// Sidebar focus on a file targets that file, with no line: a sidebar-focused
// launch is not tied to the viewport, so the viewport's line means nothing.
func TestEditorTarget_SidebarFocusUsesTheSelectionWithoutALine(t *testing.T) {
	m, _ := paneOwnerModel(t)
	selectSidebarFileRow(t, m, "pkg/b.go")
	m.focus = SidebarFocus
	m.mainPane.viewport.SetYOffset(4)

	target, ok := m.editorTargetForFocus()
	if !ok {
		t.Fatal("no target with a file selected")
	}
	if target.file != "pkg/b.go" {
		t.Errorf("target file = %q, want the selected pkg/b.go", target.file)
	}
	if target.line != 0 {
		t.Errorf("target line = %d, want 0 — a sidebar-focused launch carries no line", target.line)
	}
}

// `e` is a files-mode action. In another mode the sidebar's selection is a
// commit or a PR item, neither of which is a file.
func TestEditorTarget_OtherModesHaveNoTarget(t *testing.T) {
	m, _ := paneOwnerModel(t)

	for _, mode := range []Mode{CommitsMode, PRMode} {
		m.mode = mode
		for _, focus := range []Focus{SidebarFocus, MainFocus} {
			m.focus = focus
			if target, ok := m.editorTargetForFocus(); ok {
				t.Errorf("mode %v focus %v produced target %+v", mode, focus, target)
			}
		}
	}
}

func TestEditorPicker_EKeyOpensTheOverlay(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	m, _ := paneOwnerModel(t)

	if m.overlayIsOpen() {
		t.Fatal("an overlay was already open")
	}
	m = applyAction(m, tea.KeyPressMsg{Text: "e", Code: 'e'})

	if !m.editorPicker.IsOpen() {
		t.Fatal("`e` did not open the editor picker")
	}
	if m.activeOverlay() == nil {
		t.Error("the picker is open but activeOverlay reports nothing")
	}
}

// Inert must mean "no overlay", not "an empty overlay".
func TestEditorPicker_EKeyIsInertOnADirectory(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	m, _ := paneOwnerModel(t)
	m.focus = SidebarFocus

	m = applyAction(m, tea.KeyPressMsg{Text: "e", Code: 'e'})

	if m.editorPicker.IsOpen() {
		t.Error("`e` opened the picker while a directory was selected")
	}
}

// The overlay seam: while the picker is up, a click must not reach the panes
// underneath, exactly as it does not for help.
func TestEditorPicker_SwallowsClicksLikeHelp(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	m, _ := paneOwnerModel(t)
	m = applyAction(m, tea.KeyPressMsg{Text: "e", Code: 'e'})
	if !m.editorPicker.IsOpen() {
		t.Fatal("picker did not open")
	}

	before := m.sidebar.SelectedIndex()
	m = applyAction(m, tea.MouseClickMsg{X: 5, Y: 6, Button: tea.MouseLeft})

	if got := m.sidebar.SelectedIndex(); got != before {
		t.Errorf("a click under the picker moved the sidebar selection from %d to %d", before, got)
	}
}

// End to end: `e` then a digit launches the chosen editor on the pane's file,
// with the picker already closed when the command is built.
func TestEditorPicker_LaunchesTheChosenEditorOnTheTarget(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	m, wantFile := paneOwnerModel(t)

	m = applyAction(m, tea.KeyPressMsg{Text: "e", Code: 'e'})
	if !m.editorPicker.IsOpen() {
		t.Fatal("picker did not open")
	}

	// Pick whichever row `vim` landed on, so the assertion does not depend on
	// which editors happen to be installed on the machine running the test.
	// `vim` is always offered: it is $EDITOR here, and Available exempts that
	// one from the PATH filter.
	idx := -1
	for i, e := range m.editorPicker.entries {
		if e.Name == "vim" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("vim is not in the list even though it is $EDITOR: %v", m.editorPicker.entries)
	}
	if idx > 8 {
		t.Skipf("vim is row %d, past the digit-addressable nine", idx+1)
	}

	var gotArgs []string
	openWhenLaunched := true
	m.interactiveFactory = func(name string, args ...string) command.Command {
		gotArgs = append([]string{name}, args...)
		openWhenLaunched = m.editorPicker.IsOpen()
		return command.StubCommand("", nil)
	}

	digit := string(rune('1' + idx))
	m = applyAction(m, tea.KeyPressMsg{Text: digit, Code: rune(digit[0])})

	if m.editorPicker.IsOpen() {
		t.Error("picker still open after a launch")
	}
	if openWhenLaunched {
		t.Error("the picker was still open when the editor command was built; " +
			"tea.Exec would redraw the list over the editor's exit")
	}
	if len(gotArgs) == 0 {
		t.Fatal("no editor was launched")
	}
	if gotArgs[0] != "vim" {
		t.Errorf("launched %q, want vim", gotArgs[0])
	}
	if gotArgs[len(gotArgs)-1] != wantFile {
		t.Errorf("opened %q, want the displayed %q (args %v)",
			gotArgs[len(gotArgs)-1], wantFile, gotArgs)
	}
}
