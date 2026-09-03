package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// filesModeStateModel builds a files-mode model whose sidebar sits on a
// non-default selection, so a mode switch that forgets to save state is
// visible when we come back.
func filesModeStateModel(t *testing.T) (m *Model, wantSelected string) {
	t.Helper()
	m = NewModel("/tmp", testGit())
	m.loading = true
	m.width = 80
	m.height = 40
	m.updateLayout()
	putChanges(m, git.SectionCommitted, git.ClassModified, "a.go", "b.go", "c.go", "d.go")
	m.mode = FilesMode
	m.updateSidebarItems()
	// Move off the default (first selectable) item.
	m.sidebar.SelectNext()
	m.sidebar.SelectNext()
	wantSelected = m.sidebar.SelectedItem()
	if wantSelected == "" {
		t.Fatal("test setup: no sidebar selection to preserve")
	}
	return m, wantSelected
}

// TestAutoSwitchToPRMode_PreservesFilesState covers both auto-switch sites
// (the gitDataMsg first-load default and the first prRefreshMsg arrival).
// Both used to assign m.mode directly, skipping saveModeState, so the
// files-mode selection the user had was gone by the time they pressed the
// files-mode key again.
func TestAutoSwitchToPRMode_PreservesFilesState(t *testing.T) {
	tests := []struct {
		name    string
		trigger func(m *Model) tea.Msg
	}{
		{
			name: "gitDataMsg first load",
			trigger: func(m *Model) tea.Msg {
				return gitDataMsg{
					repoInfo: m.repoInfo,
					prInfo:   git.PRInfoResult{Number: 7, Title: "a pr", BaseRef: "main"},
					changes:  m.changes,
				}
			},
		},
		{
			name: "first prRefreshMsg",
			trigger: func(m *Model) tea.Msg {
				m.loading = false
				m.prLoadedOnce = false
				return prRefreshMsg{prInfo: git.PRInfoResult{Number: 7, Title: "a pr", BaseRef: "main"}}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, wantSelected := filesModeStateModel(t)
			msg := tc.trigger(m)

			result, _ := m.Update(msg)
			m = result.(*Model)

			if m.mode != PRMode {
				t.Fatalf("expected auto-switch to PRMode, got mode %v", m.mode)
			}

			// The user browses around in PR mode. This is what makes the lost
			// files-mode state observable: without a saved state to restore,
			// switching back leaves the sidebar on whatever raw index PR mode
			// happened to end on.
			for range 5 {
				m.sidebar.SelectNext()
			}

			// Switching back must land on the item the user had, which only
			// works if the auto-switch saved the outgoing mode's state.
			m.setMode(FilesMode)
			if got := m.sidebar.SelectedItem(); got != wantSelected {
				t.Errorf("after switching back to files mode, selection = %q, want %q", got, wantSelected)
			}
		})
	}
}

// TestModeHasSingleWriter guards the class of bug fixed above: every mode
// transition must go through setMode, which is what saves and restores
// per-mode view state. A direct `m.mode = …` in production code silently
// drops the user's view state, and the two sites that did it were found by
// review rather than by a test — so this scan is the cheap structural
// backstop. Test files are exempt: they build fixtures, not transitions.
// modeAssignRE matches an assignment to m.mode — plain `=` and `:=`, but not
// the `==` / `!=` comparisons that appear all over the mode dispatch.
var modeAssignRE = regexp.MustCompile(`\bm\.mode\s*:?=[^=]`)

func TestModeHasSingleWriter(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		inSetMode := false
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "func (m *Model) setMode(") {
				inSetMode = true
				continue
			}
			// setMode's body ends at the first column-0 closing brace.
			if inSetMode && line == "}" {
				inSetMode = false
				continue
			}
			if !modeAssignRE.MatchString(trimmed) || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if inSetMode {
				continue
			}
			t.Errorf("%s:%d assigns m.mode outside setMode: %s\n"+
				"Mode transitions must go through setMode so per-mode view state is saved and restored.",
				name, i+1, trimmed)
		}
	}
}
