package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// TestPRMode_NoStaleFilesModeStateLeaks is the regression test for the pair of
// symptoms caused by updatePRModeContent installing content without resetting
// the pane's per-item state from the previously shown item:
//
//  1. A stale chroma lexer (from viewing a .go file in files mode) re-highlighted
//     the rendered comment text. Chroma tokenizes the ESC byte of each SGR
//     sequence emitted by renderMarkdown as its own Error token and wraps it in
//     chroma's own colors, separating the ESC from its `[48;2;...m` body — so
//     the terminal printed the sequence bodies as literal text.
//  2. Stale diffAnnotations (same origin) injected the old file's removed diff
//     lines into the comment view — annotations keyed past the comment's last
//     line render via the tail-annotation pass.
func TestPRMode_NoStaleFilesModeStateLeaks(t *testing.T) {
	goSource := "package main\n\nfunc main() {\n\tfeatureFlags := load()\n}\n"
	diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,5 +1,5 @@
 package main

 func main() {
-	staleRemovedLine := old()
+	featureFlags := load()
 }
`
	mg := &mockGit{
		repoInfo:     git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:         "abc",
		prInfo:       git.PRInfoResult{Number: 42, Title: "Test PR", Body: "PR body"},
		changedFiles: git.ChangedFilesResult{Committed: []string{"file.go"}},
		commits:      []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent:  goSource,
		fileDiff:     diff,
		prComments: []git.PRComment{{
			Author: "alice",
			Body:   "Look at `api.ExpHistogram` for the type switch on error.",
		}},
	}
	m := NewModel("/tmp", mg)
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.Update(m.loadGitData())

	// View file.go in files mode. This is what installs the lexer and the
	// diff annotations that must not survive into PR mode.
	m.setMode(FilesMode)
	if m.sidebar.SelectedItem() != "file.go" {
		t.Fatalf("setup: files-mode selection = %q, want file.go", m.sidebar.SelectedItem())
	}
	if m.mainPane.lexer == nil {
		t.Fatal("setup: viewing file.go should install a chroma lexer")
	}
	if len(m.mainPane.diffAnnotations) == 0 {
		t.Fatal("setup: viewing file.go with a diff should install annotations")
	}

	// Switch to PR mode and select alice's comment.
	m.setMode(PRMode)
	for i := 0; m.sidebar.SelectedRow().prComment() == nil; i++ {
		if i > len(m.sidebar.items) {
			t.Fatal("setup: no comment row found in PR sidebar")
		}
		m.sidebar.SelectNext()
		m.updateMainContent()
	}

	fc := m.mainPane.formattedContent

	// Symptom 2: the old file's removed diff line must not be in the comment.
	if strings.Contains(fc, "staleRemovedLine") {
		t.Errorf("stale removed diff line from files mode rendered in PR comment view:\n%s", fc)
	}

	// Symptom 1: renderMarkdown's inline-code styling must survive intact.
	// Chroma mangling separates each ESC from its sequence body, leaving the
	// body visible; stripping the (valid) escapes must therefore leave no
	// SGR-looking fragments behind.
	if !strings.Contains(m.mainPane.content, "\x1b[48;2;40;40;40m") {
		t.Error("inline-code SGR open sequence missing or corrupted in comment content")
	}
	visible := stripANSIForWidth(fc)
	for _, fragment := range []string{"[48;2;", "[38;2;", "[0m"} {
		if strings.Contains(visible, fragment) {
			t.Errorf("escape-sequence fragment %q visible as literal text in PR comment view:\n%s", fragment, visible)
		}
	}
}

// genPaneContent draws an arbitrary paneContent spec: bodies with tabs and
// ANSI-ish text, a filename that may or may not pick a lexer, annotations,
// hunks, and both title forms.
func genPaneContent() *rapid.Generator[paneContent] {
	genAnnotation := rapid.Custom(func(t *rapid.T) diffAnnotation {
		return diffAnnotation{
			kind:         rapid.SampledFrom([]diffLineKind{diffLineUnchanged, diffLineAdded, diffLineRemoved, diffLineChanged}).Draw(t, "kind"),
			removedLines: rapid.SliceOfN(rapid.StringN(0, 30, -1), 0, 3).Draw(t, "removedLines"),
		}
	})
	genHunk := rapid.Custom(func(t *rapid.T) diffHunk {
		start := rapid.IntRange(1, 40).Draw(t, "start")
		return diffHunk{StartLine: start, EndLine: start + rapid.IntRange(0, 10).Draw(t, "len")}
	})
	return rapid.Custom(func(t *rapid.T) paneContent {
		return paneContent{
			body:         rapid.StringN(0, 200, -1).Draw(t, "body"),
			isDiff:       rapid.Bool().Draw(t, "isDiff"),
			filename:     rapid.SampledFrom([]string{"", "a.go", "b.md", "c.py"}).Draw(t, "filename"),
			annotations:  rapid.MapOfN(rapid.IntRange(1, 40), genAnnotation, 0, 4).Draw(t, "annotations"),
			hunks:        rapid.SliceOfN(genHunk, 0, 3).Draw(t, "hunks"),
			noHunkRight:  rapid.StringN(0, 20, -1).Draw(t, "noHunkRight"),
			diffPrefix:   rapid.StringN(0, 20, -1).Draw(t, "diffPrefix"),
			titleLeft:    rapid.StringN(0, 30, -1).Draw(t, "titleLeft"),
			titleRight:   rapid.StringN(0, 30, -1).Draw(t, "titleRight"),
			titleDynamic: rapid.Bool().Draw(t, "titleDynamic"),
		}
	})
}

// TestProperty_ShowItemIsHistoryIndependent pins the invariant whose violation
// was the stale-state bug: what the pane shows after ShowItem(b) must not
// depend on what it showed before. A pane that showed some other item first
// must render byte-identically to a fresh pane showing only b.
func TestProperty_ShowItemIsHistoryIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genPaneContent().Draw(t, "a")
		b := genPaneContent().Draw(t, "b")

		withHistory := newMainPane()
		withHistory.SetSize(60, 20)
		withHistory.ShowItem(a)
		withHistory.ShowItem(b)

		fresh := newMainPane()
		fresh.SetSize(60, 20)
		fresh.ShowItem(b)

		if withHistory.formattedContent != fresh.formattedContent {
			t.Fatalf("formattedContent depends on pane history:\nwith history: %q\nfresh:        %q",
				withHistory.formattedContent, fresh.formattedContent)
		}
		if got, want := withHistory.View(true), fresh.View(true); got != want {
			t.Fatalf("View depends on pane history:\nwith history: %q\nfresh:        %q", got, want)
		}
	})
}

// TestShowItemIsTheOnlyContentWriter guards the class of bug fixed by ShowItem:
// installing an item's content through the piecemeal per-item setters makes
// every caller responsible for resetting the fields it doesn't use, and a
// forgotten one leaks the previous item's state (stale lexer, stale diff
// annotations). Production code must go through ShowItem, which resets all of
// them atomically; the setters remain only as test fixtures and internals.
// Same structural-backstop approach as TestModeHasSingleWriter.
func TestShowItemIsTheOnlyContentWriter(t *testing.T) {
	perItemSetters := map[string]bool{
		"SetContent":           true,
		"SetPlainContent":      true,
		"SetFilename":          true,
		"SetDiffAnnotations":   true,
		"ClearDiffAnnotations": true,
		"SetDiffHunks":         true,
		"ClearDiffHunks":       true,
		"SetNoHunkRight":       true,
		"SetDiffPrefix":        true,
		"SetTitle":             true,
		"SetTitleWithHunks":    true,
	}

	// isMainPaneExpr reports whether the AST expression plausibly evaluates to
	// the model's main pane: a `….mainPane` field selector, or an identifier
	// bound to mainPane by the enclosing function (receiver or parameter).
	// Type-resolution-free on purpose, matching the package's other AST guards;
	// this covers every realistic call shape (`m.mainPane.SetX`, and pane
	// helpers taking a *mainPane), while a local viewport variable's
	// `v.SetContent` has neither shape and is correctly ignored.
	isMainPaneExpr := func(e ast.Expr, paneIdents map[string]bool) bool {
		if sel, ok := e.(*ast.SelectorExpr); ok {
			return sel.Sel.Name == "mainPane"
		}
		id, ok := e.(*ast.Ident)
		return ok && paneIdents[id.Name]
	}
	isMainPaneType := func(e ast.Expr) bool {
		if star, ok := e.(*ast.StarExpr); ok {
			e = star.X
		}
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "mainPane"
	}
	paneBoundIdents := func(fn *ast.FuncDecl) map[string]bool {
		out := map[string]bool{}
		add := func(fl *ast.FieldList) {
			if fl == nil {
				return
			}
			for _, f := range fl.List {
				if !isMainPaneType(f.Type) {
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

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	showItemCalls := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// mainpane.go owns the pane: ShowItem's implementation and the setters
		// themselves live there.
		if name == "mainpane.go" {
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
			paneIdents := paneBoundIdents(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !isMainPaneExpr(sel.X, paneIdents) {
					return true
				}
				if sel.Sel.Name == "ShowItem" {
					showItemCalls++
					return true
				}
				if perItemSetters[sel.Sel.Name] {
					t.Errorf("%s calls mainPane.%s outside mainpane.go, in %s\n"+
						"Install item content atomically via mainPane.ShowItem so no stale per-item state survives.",
						fset.Position(sel.Pos()), sel.Sel.Name, fn.Name.Name)
				}
				return true
			})
		}
	}
	// Anchors the scan against silently matching nothing — a broken receiver
	// heuristic would otherwise "pass" forever.
	if showItemCalls == 0 {
		t.Error("scan found no mainPane.ShowItem calls in production code; " +
			"if the content pipeline was refactored, update this guard rather than deleting it")
	}
}
