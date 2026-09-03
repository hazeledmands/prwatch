package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// filesModeStateModel builds a files-mode model whose sidebar sits deep in a
// long file list, both selected and scrolled.
//
// The list is deliberately long and the selection deliberately far down it.
// An earlier version of this test used four files and a selection two rows
// in, and it passed against the unfixed code: the raw sidebar index survived
// the round trip and happened to land on the same file. The state loss is
// only unambiguous when the remembered position is far from anywhere the
// other mode's index could put it, and when the scroll offset is non-zero
// too — an index coincidence cannot reproduce both.
func filesModeStateModel(t *testing.T) (m *Model, wantSelected string, wantOffset int) {
	t.Helper()
	m = NewModel("/tmp", testGit())
	m.loading = true
	m.width = 80
	// Short pane, long list: guarantees the sidebar can actually scroll, so
	// the saved offset is a real value rather than a clamped zero.
	m.height = 20
	m.updateLayout()
	names := make([]string, 0, 60)
	for i := range 60 {
		names = append(names, fmt.Sprintf("f%02d.go", i))
	}
	putChanges(m, git.SectionCommitted, git.ClassModified, names...)
	m.mode = FilesMode
	m.updateSidebarItems()

	// Land far down the list — well past any index PR mode could leave behind.
	for range 40 {
		m.sidebar.SelectNext()
	}
	for range 8 {
		m.sidebar.ScrollDown()
	}
	wantSelected = m.sidebar.SelectedItem()
	wantOffset = m.sidebar.offset
	if wantSelected == "" {
		t.Fatal("test setup: no sidebar selection to preserve")
	}
	if wantOffset == 0 {
		t.Fatal("test setup: sidebar offset should be non-zero so the saved scroll is meaningful")
	}
	return m, wantSelected, wantOffset
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
			m, wantSelected, wantOffset := filesModeStateModel(t)
			msg := tc.trigger(m)

			result, _ := m.Update(msg)
			m = result.(*Model)

			if m.mode != PRMode {
				t.Fatalf("expected auto-switch to PRMode, got mode %v", m.mode)
			}

			// Assert the mechanism directly: the outgoing mode's state must
			// be in view memory. This is the thing a direct `m.mode = PRMode`
			// skips, so checking it here cannot be satisfied by an index
			// coincidence downstream.
			saved, ok := m.viewMemory.modeStates[FilesMode]
			if !ok {
				t.Fatal("auto-switch did not save any files-mode view state")
			}
			if saved.sidebarSelected != wantSelected {
				t.Errorf("saved files-mode selection = %q, want %q", saved.sidebarSelected, wantSelected)
			}
			if saved.sidebarOffset != wantOffset {
				t.Errorf("saved files-mode scroll offset = %d, want %d", saved.sidebarOffset, wantOffset)
			}

			// The user browses around in PR mode. This is what makes the lost
			// files-mode state observable end-to-end: without a saved state
			// to restore, switching back leaves the sidebar on whatever raw
			// index PR mode happened to end on.
			for range 5 {
				m.sidebar.SelectNext()
			}

			// Switching back must land on the item and scroll the user had,
			// which only works if the auto-switch saved the outgoing state.
			m.setMode(FilesMode)
			if got := m.sidebar.offset; got != wantOffset {
				t.Errorf("after switching back to files mode, scroll offset = %d, want %d", got, wantOffset)
			}
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
// isModelType reports whether an AST type expression is Model or *Model.
func isModelType(e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "Model"
}

// modelBoundIdents returns the identifiers inside fn that are bound to a
// Model — its receiver plus any Model-typed parameter. Keying on the *type*
// rather than on the conventional name `m` is what keeps `s.mode` (the
// selection state machine's own receiver, selection.go) out of the results
// while still catching a Model method that names its receiver something else.
func modelBoundIdents(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if !isModelType(f.Type) {
				continue
			}
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	add(fn.Recv)
	if fn.Type != nil {
		add(fn.Type.Params)
	}
	return out
}

func TestModeHasSingleWriter(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	legitimate := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			models := modelBoundIdents(fn)
			if len(models) == 0 {
				continue
			}
			isSetMode := fn.Name.Name == "setMode" && fn.Recv != nil &&
				len(fn.Recv.List) == 1 && isModelType(fn.Recv.List[0].Type)

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				// Assignment targets only. Reading m.mode is what the mode
				// dispatch does everywhere and is none of our business; an
				// AssignStmt's Lhs covers `=`, `:=`, and the compound forms,
				// including the multi-assign `m.mode, m.focus = a, b` that a
				// future direct write would most naturally take (setMode
				// writes both fields).
				var targets []ast.Expr
				switch s := n.(type) {
				case *ast.AssignStmt:
					targets = s.Lhs
				case *ast.IncDecStmt:
					targets = []ast.Expr{s.X}
				default:
					return true
				}
				for _, tgt := range targets {
					sel, ok := tgt.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "mode" {
						continue
					}
					// Base must be a bare Model identifier. A nested
					// selector like m.selection.mode has a SelectorExpr
					// base and is correctly not a Model.mode write.
					base, ok := sel.X.(*ast.Ident)
					if !ok || !models[base.Name] {
						continue
					}
					if isSetMode {
						legitimate++
						continue
					}
					t.Errorf("%s assigns %s.mode outside setMode, in %s\n"+
						"Mode transitions must go through setMode so per-mode view state is saved and restored.",
						fset.Position(sel.Pos()), base.Name, fn.Name.Name)
				}
				return true
			})
		}
	}
	// Anchors the scan against silently matching nothing — a broken walk
	// would otherwise "pass" forever.
	if legitimate != 1 {
		t.Errorf("found %d mode writes inside setMode, want exactly 1 (`m.mode = next`); "+
			"if setMode was refactored, update this guard rather than deleting it", legitimate)
	}
}
