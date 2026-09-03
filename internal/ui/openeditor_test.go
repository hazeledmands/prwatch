package ui

import (
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
)

// TestOpenEditor_SkipsDirectories pins that $EDITOR is never launched on a
// directory. The sidebar's own Enter path (handleSidebarRight) treats a
// directory as expand/collapse and never reaches the editor, but the
// main-pane Enter path in files mode called openEditor against whatever the
// sidebar had selected — which can be a directory (a mode switch restores a
// main-pane focus over a directory selection, and a collapse can shift a
// directory under the selected index). yankPath already declines the same
// way, via SelectedIsDir.
func TestOpenEditor_SkipsDirectories(t *testing.T) {
	tests := []struct {
		name    string
		item    sidebarItem
		wantCmd bool
	}{
		{
			name:    "file opens the editor",
			item:    sidebarItem{label: "a.go", filePath: "a.go"},
			wantCmd: true,
		},
		{
			name:    "directory does not",
			item:    sidebarItem{label: "pkg", filePath: "pkg", isDir: true, collapseKey: dirCollapseKey(sectionAllFiles, "pkg")},
			wantCmd: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var launched []string
			m := NewModel("/tmp", testGit())
			m.interactiveFactory = func(name string, args ...string) command.Command {
				launched = append(launched, name)
				return command.StubCommand("", nil)
			}
			m.sidebar.SetItems([]sidebarItem{tc.item})

			cmd := m.openEditor()

			if got := cmd != nil; got != tc.wantCmd {
				t.Errorf("openEditor returned cmd=%v, want %v", got, tc.wantCmd)
			}
			if got := len(launched) > 0; got != tc.wantCmd {
				t.Errorf("editor launched=%v (%v), want %v", got, launched, tc.wantCmd)
			}
		})
	}
}

// TestEnterOnDirectory_MainFocus is the end-to-end shape of the same bug:
// Enter with the main pane focused in files mode while a directory is
// selected must not launch anything.
func TestEnterOnDirectory_MainFocus(t *testing.T) {
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

	_, cmd := m.handleEnter()

	if cmd != nil {
		t.Error("Enter on a directory with main focus should not return a command")
	}
	if len(launched) > 0 {
		t.Errorf("Enter on a directory launched %v", launched)
	}
}
