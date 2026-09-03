package ui

import (
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// paneOwnerModel builds a files-mode model where the main pane is showing a
// real file while the sidebar selection has moved onto a directory.
//
// That combination is not a corner case: updateFilesModeContent early-returns
// on a directory selection precisely so the previously-shown file stays on
// screen (maincontent.go), and it leaves m.lastMainItem pointing at that file.
// So the pane and the sidebar legitimately disagree, and any main-pane action
// has to follow the pane.
func paneOwnerModel(t *testing.T) (m *Model, wantFile string) {
	t.Helper()
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", Upstream: "origin/main"},
		base:     "origin/main",
		// Two files under pkg/: a single-child directory gets compacted into
		// a "pkg/a.go" leaf row, leaving no directory to select.
		changedFiles: git.ChangedFilesResult{Committed: []string{"pkg/a.go", "pkg/b.go"}},
		fileContent:  strings.Repeat("some source line\n", 200),
		allFiles:     []string{"pkg/a.go", "pkg/b.go"},
	}
	m = NewModel("/tmp/repo", mock)
	m.loading = false
	m.width = 100
	m.height = 30
	m.updateLayout()
	// updateMainContent early-returns while the scope has no old base.
	m.scope.SyncFromLoad("origin/main", "", 0, 0, "", -1)
	putChanges(m, git.SectionCommitted, git.ClassModified, "pkg/a.go", "pkg/b.go")
	m.mode = FilesMode
	m.wordWrap = false
	m.mainPane.SetWordWrap(false)
	m.updateSidebarItems()

	// Put the file on screen.
	fileIdx := -1
	dirIdx := -1
	for i, item := range m.sidebar.items {
		if item.isDir && item.filePath == "pkg" {
			dirIdx = i
		}
		if item.filePath == "pkg/a.go" && !item.isDir {
			fileIdx = i
		}
	}
	if fileIdx < 0 || dirIdx < 0 {
		t.Fatalf("test setup: need both a dir and a file row; got dirIdx=%d fileIdx=%d items=%d",
			dirIdx, fileIdx, len(m.sidebar.items))
	}
	m.sidebar.SelectIndex(fileIdx)
	m.updateMainContent()
	if m.lastMainItem.item != "pkg/a.go" {
		t.Fatalf("test setup: pane should be showing pkg/a.go, got %q", m.lastMainItem.item)
	}

	// Now move the sidebar onto the directory. The pane keeps the file.
	m.sidebar.SelectIndex(dirIdx)
	m.updateMainContent()
	if !m.sidebar.SelectedIsDir() {
		t.Fatal("test setup: sidebar should be on a directory")
	}
	if m.lastMainItem.item != "pkg/a.go" {
		t.Fatalf("test setup: pane should still show pkg/a.go, got %q", m.lastMainItem.item)
	}
	m.focus = MainFocus
	return m, "pkg/a.go"
}

// TestEnter_MainFocusFollowsPaneNotSidebar pins PROMPT.md:338: main-pane
// Enter in files mode opens $EDITOR on the file the pane is displaying, at
// the top-of-viewport line. A directory sitting under the sidebar cursor is
// not a reason to do nothing — a real file is on screen.
func TestEnter_MainFocusFollowsPaneNotSidebar(t *testing.T) {
	m, wantFile := paneOwnerModel(t)
	var gotArgs []string
	m.interactiveFactory = func(name string, args ...string) command.Command {
		gotArgs = append([]string{name}, args...)
		return command.StubCommand("", nil)
	}

	_, cmd := m.handleEnter()

	if cmd == nil {
		t.Fatal("Enter with a file on screen should open the editor")
	}
	if len(gotArgs) == 0 {
		t.Fatal("editor was not launched")
	}
	if gotArgs[len(gotArgs)-1] != wantFile {
		t.Errorf("editor opened %q, want the displayed file %q (args %v)",
			gotArgs[len(gotArgs)-1], wantFile, gotArgs)
	}
}

// TestEnter_MainFocusEditorLineIsViewportTop checks the line argument tracks
// the viewport, so the file identity fix didn't come at the cost of the
// "+N" the spec asks for.
func TestEnter_MainFocusEditorLineIsViewportTop(t *testing.T) {
	m, wantFile := paneOwnerModel(t)
	var gotArgs []string
	m.interactiveFactory = func(name string, args ...string) command.Command {
		gotArgs = append([]string{name}, args...)
		return command.StubCommand("", nil)
	}
	// Scroll the pane down; plain (non-diff) content maps viewport row N to
	// source line N+1.
	m.mainPane.viewport.SetYOffset(4)

	if cmd := m.openEditor(); cmd == nil {
		t.Fatal("openEditor returned nil with a file on screen")
	}
	if len(gotArgs) < 3 {
		t.Fatalf("expected editor, +line and file args, got %v", gotArgs)
	}
	if gotArgs[1] != "+5" {
		t.Errorf("editor line arg = %q, want %q (viewport at row 4) — args %v", gotArgs[1], "+5", gotArgs)
	}
	if gotArgs[len(gotArgs)-1] != wantFile {
		t.Errorf("editor opened %q, want %q", gotArgs[len(gotArgs)-1], wantFile)
	}
}

// TestYankPath_MainFocusFollowsPaneNotSidebar is the same ownership bug in
// yankPath: main-pane `y` copies the displayed file's visible range, so it
// must name the pane's file, not the sidebar's directory.
func TestYankPath_MainFocusFollowsPaneNotSidebar(t *testing.T) {
	m, wantFile := paneOwnerModel(t)

	if cmd := m.yankPath(); cmd == nil {
		t.Fatal("yankPath returned nil with a file on screen")
	}
	if !strings.HasPrefix(m.notification, "copied "+wantFile+":") {
		t.Errorf("notification = %q, want a %q line range", m.notification, wantFile+":N-M")
	}
}

// TestYankPath_SidebarFocusOnDirectoryStillDeclines pins the half of yankPath
// that was already right: with the sidebar focused, `y` names the sidebar's
// selection, and a directory has no path worth copying.
func TestYankPath_SidebarFocusOnDirectoryStillDeclines(t *testing.T) {
	m, _ := paneOwnerModel(t)
	m.focus = SidebarFocus
	m.notification = ""

	if cmd := m.yankPath(); cmd != nil {
		t.Error("sidebar-focused yankPath on a directory should return nil")
	}
	if m.notification != "" {
		t.Errorf("sidebar-focused yankPath on a directory set notification %q", m.notification)
	}
}

// TestEnter_MainFocusNoFileOnScreen keeps the guard that matters: when the
// pane genuinely has no file (nothing ever displayed), Enter is inert rather
// than launching $EDITOR on an empty path.
func TestEnter_MainFocusNoFileOnScreen(t *testing.T) {
	var launched []string
	m := NewModel("/tmp", testGit())
	m.interactiveFactory = func(name string, args ...string) command.Command {
		launched = append(launched, name)
		return command.StubCommand("", nil)
	}
	m.mode = FilesMode
	m.focus = MainFocus
	m.sidebar.SetItems([]sidebarItem{{
		label:       "pkg",
		filePath:    "pkg",
		isDir:       true,
		collapseKey: dirCollapseKey(sectionAllFiles, "pkg"),
	}})
	m.lastMainItem = mainItemKey{FilesMode, ""}

	_, cmd := m.handleEnter()

	if cmd != nil {
		t.Error("Enter with no file on screen should not return a command")
	}
	if len(launched) > 0 {
		t.Errorf("Enter with no file on screen launched %v", launched)
	}
}
