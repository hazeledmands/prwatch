package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pgregory.net/rapid"

	git "github.com/hazeledmands/prwatch/internal/git"
)

// genNavAction generates a user interaction that can move the main pane's
// scroll position or the cursor. Unlike genAction it deliberately includes
// the paths the A5 review flagged: hunk-grain nav (J/K), search entry +
// n/p navigation, page keys, wheel, mode switches and resize.
func genNavAction(t *rapid.T, m *Model, step int) []tea.Msg {
	tag := fmt.Sprintf("nav%d", step)
	kind := rapid.SampledFrom([]string{
		"motion", "motion", "motion",
		"hunknav", "hunknav",
		"page",
		"wheel",
		"search",
		"mode",
		"toggle",
		"resize",
		"visual",
		"click",
	}).Draw(t, tag+"_kind")

	key := func(text string, code rune) tea.Msg {
		return tea.KeyPressMsg{Text: text, Code: code}
	}

	switch kind {
	case "motion":
		k := rapid.SampledFrom([]string{"j", "k", "h", "l", "g", "G"}).Draw(t, tag+"_motion")
		return []tea.Msg{key(k, rune(k[0]))}
	case "hunknav":
		k := rapid.SampledFrom([]string{"J", "K"}).Draw(t, tag+"_hunk")
		return []tea.Msg{key(k, rune(k[0]))}
	case "page":
		c := rapid.SampledFrom([]rune{tea.KeyPgUp, tea.KeyPgDown}).Draw(t, tag+"_page")
		return []tea.Msg{tea.KeyPressMsg{Code: c}}
	case "wheel":
		return []tea.Msg{genMouseScroll(t, tag, m)}
	case "search":
		// "/" + a letter + Enter, then a couple of n/p navigations.
		q := rapid.SampledFrom([]string{"a", "e", "l", "1", "-"}).Draw(t, tag+"_q")
		msgs := []tea.Msg{
			key("/", '/'),
			key(q, rune(q[0])),
			tea.KeyPressMsg{Code: tea.KeyEnter},
		}
		for range rapid.IntRange(0, 3).Draw(t, tag+"_navs") {
			nk := rapid.SampledFrom([]string{"n", "p"}).Draw(t, tag+"_nav")
			msgs = append(msgs, key(nk, rune(nk[0])))
		}
		msgs = append(msgs, tea.KeyPressMsg{Code: tea.KeyEscape})
		return msgs
	case "mode":
		k := rapid.SampledFrom([]string{"1", "2", "3", "m"}).Draw(t, tag+"_mode")
		return []tea.Msg{key(k, rune(k[0]))}
	case "toggle":
		k := rapid.SampledFrom([]string{"w", "n", "D", "N", "P"}).Draw(t, tag+"_toggle")
		return []tea.Msg{key(k, rune(k[0]))}
	case "resize":
		return []tea.Msg{genResize(t, tag)}
	case "visual":
		k := rapid.SampledFrom([]string{"v", "V", "y"}).Draw(t, tag+"_visual")
		return []tea.Msg{key(k, rune(k[0]))}
	case "click":
		return []tea.Msg{genMouseClick(t, tag, m)}
	}
	return []tea.Msg{key("j", 'j')}
}

// cursorVisibilityFailure returns a non-empty explanation when the cursor is
// outside the viewport (or outside the content), else "".
func cursorVisibilityFailure(m *Model) string {
	c := m.cursor
	if !c.IsPlaced() {
		return ""
	}
	pane := m.mainPane
	rows := viewportContentRowCount(pane)
	if rows == 0 {
		return ""
	}
	if c.vpRow >= rows {
		return fmt.Sprintf("cursor vpRow=%d is past content (rows=%d)", c.vpRow, rows)
	}
	off := pane.viewport.YOffset()
	h := pane.viewport.Height()
	if h <= 0 {
		return ""
	}
	if c.vpRow < off || c.vpRow >= off+h {
		return fmt.Sprintf("cursor vpRow=%d outside viewport [%d,%d)", c.vpRow, off, off+h)
	}
	return ""
}

// TestProperty_Model_CursorAlwaysVisible is the model-level statement of
// PLAN.md's cursor invariant ("the cursor is always inside the viewport").
// cursor_test.go's TestProperty_Cursor_AlwaysVisible only proves it when the
// struct is driven with the correct paired calls; this one drives the real
// dispatcher, so any call site that scrolls without moving the cursor (or
// vice versa) shows up here.
func TestProperty_Model_CursorAlwaysVisible(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mock, mode := genScenario(t)
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(16, 44).Draw(t, "height")
		m := initModel(mock, mode, width, height)

		// Start with focus on the main pane so cursor motions apply.
		m = applyAction(m, tea.KeyPressMsg{Text: ".", Code: '.'})

		if fail := cursorVisibilityFailure(m); fail != "" {
			t.Fatalf("initial: %s", fail)
		}

		steps := rapid.IntRange(3, 20).Draw(t, "steps")
		for i := range steps {
			for _, msg := range genNavAction(t, m, i) {
				m = applyAction(m, msg)
			}
			if fail := cursorVisibilityFailure(m); fail != "" {
				t.Fatalf("after step %d: %s (mode=%d focus=%d)", i, fail, m.mode, m.focus)
			}
		}
	})
}

// TestProperty_Model_VisualSelectionTracksCursor states sub-item 2's
// invariant: while visual mode is active, the selection's active end is
// exactly the cursor's endpoint — no matter which path moved the cursor
// (j/k/h/l, g/G, page keys, wheel, hunk nav, search).
func TestProperty_Model_VisualSelectionTracksCursor(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mock, mode := genScenario(t)
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(16, 44).Draw(t, "height")
		m := initModel(mock, mode, width, height)
		m = applyAction(m, tea.KeyPressMsg{Text: ".", Code: '.'})

		// Enter visual mode.
		enter := rapid.SampledFrom([]string{"v", "V"}).Draw(t, "enter")
		m = applyAction(m, tea.KeyPressMsg{Text: enter, Code: rune(enter[0])})

		steps := rapid.IntRange(3, 15).Draw(t, "steps")
		for i := range steps {
			for _, msg := range genNavAction(t, m, i) {
				m = applyAction(m, msg)
			}
			if !m.selection.IsActive() {
				return // dismissed (Esc/y/mode switch) — nothing left to check
			}
			want := m.cursor.Endpoint(m.mainPane)
			if got := m.selection.active; got != want {
				t.Fatalf("after step %d: selection.active=%+v, cursor endpoint=%+v", i, got, want)
			}
		}
	})
}

// TestProperty_Model_VisualYankMatchesHighlight states the second half of
// sub-item 2: what `y` copies is exactly the range the highlight painted.
//
// Both visual modes are exercised, and the containment check below is
// mode-aware: a cell-wise (`v`) yank is a screen operation, so its lines
// are compared with trailing padding trimmed, while a line-wise (`V`) yank
// is a source-text operation and its lines must appear in the rendered rows
// verbatim — trailing whitespace and all (PROMPT.md `### visual mode`).
func TestProperty_Model_VisualYankMatchesHighlight(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mock, mode := genScenario(t)
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(16, 44).Draw(t, "height")
		m := initModel(mock, mode, width, height)
		m = applyAction(m, tea.KeyPressMsg{Text: ".", Code: '.'})
		enter := rapid.SampledFrom([]string{"v", "V"}).Draw(t, "enter")
		m = applyAction(m, tea.KeyPressMsg{Text: enter, Code: rune(enter[0])})

		steps := rapid.IntRange(1, 10).Draw(t, "steps")
		for i := range steps {
			for _, msg := range genNavAction(t, m, i) {
				m = applyAction(m, msg)
			}
			if !m.selection.IsActive() {
				return
			}
		}
		if !m.selection.IsActive() || !m.selection.HasRange() {
			return
		}
		g := m.dragGeom()
		want := m.selection.SelectedText(g)
		// The highlight renders from the same resolved ends; assert the
		// yank text is derived from those same ends by re-resolving after
		// a no-op render.
		_ = viewWithTimeout(t, m, "visual yank")
		if got := m.selection.SelectedText(g); got != want {
			t.Fatalf("selection text changed across render: %q vs %q", got, want)
		}
		// Every non-empty yanked line must appear in what the pane actually
		// renders — which is the property this test is named for, and is what
		// makes the assertion sound for decorated rows.
		//
		// The comparison target is `formattedContent`, the pane's own
		// pre-wrap rendered rows (gutter + diff decoration applied, wrapping
		// not yet). `content` is the wrong target: it holds only the new-file
		// text, while PROMPT.md:162 has a `~` row render the old text inline
		// beside the new and `D` shows removed-only content as its own row, so
		// a copied row legitimately contains text that is not in `content`.
		// (Observed: content "line1\nline2\nline3" with a `-old/+new` hunk
		// renders row 1 as "1 ~ " + "old" + "line1" and copies "oldline1" —
		// exactly what is on screen.) Pre-wrap is what makes this work for
		// wrapped lines too: a yanked line is one logical row, so it matches a
		// whole `formattedContent` row even when the screen split it across
		// several.
		//
		// For a line-wise selection the needle is the yanked line as-is: the
		// trailing whitespace a `V` yank re-appends came from the source line,
		// so it is present in `formattedContent` too (which is pre-wrap, and
		// so untouched by the wrap-time trimming that motivated the fix).
		lineWise := m.selection.mode == selectionLine
		rendered := stripANSIForWidth(m.mainPane.formattedContent)
		for _, line := range strings.Split(want, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			needle := line
			if !lineWise {
				needle = strings.TrimRight(line, " ")
			}
			if !strings.Contains(rendered, needle) {
				t.Fatalf("yanked line %q is not in the pane's rendered rows (mode=%v)", line, m.selection.mode)
			}
		}
	})
}

// genReflowMock builds a files-mode scenario whose content actually re-wraps.
// genScenario's diffs are a handful of ~20-column lines, which fit at every
// generated pane width — a resize then leaves the row↔source mapping
// untouched and the invariance below is vacuously true. Here the line widths
// straddle the whole 40..200 range of generated widths, and removed lines are
// present so `D` moves rows too.
func genReflowMock(t *rapid.T) *mockGit {
	nOps := rapid.IntRange(20, 60).Draw(t, "reflow_nOps")
	var body strings.Builder
	var content []string
	oldCount, newCount := 0, 0

	for i := range nOps {
		w := rapid.SampledFrom([]int{0, 4, 25, 55, 110, 190, 320}).
			Draw(t, fmt.Sprintf("reflow_w%d", i))
		text := fmt.Sprintf("l%d_%s", i, strings.Repeat("x", w))
		switch rapid.SampledFrom([]string{"context", "context", "added", "removed"}).
			Draw(t, fmt.Sprintf("reflow_k%d", i)) {
		case "removed":
			body.WriteString("-" + text + "\n")
			oldCount++
		case "added":
			body.WriteString("+" + text + "\n")
			content = append(content, text)
			newCount++
		default:
			body.WriteString(" " + text + "\n")
			content = append(content, text)
			oldCount++
			newCount++
		}
	}

	diff := fmt.Sprintf("@@ -1,%d +1,%d @@\n%s", oldCount, newCount, body.String())
	return &mockGit{
		repoInfo:     git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:         "abc",
		changedFiles: git.ChangedFilesResult{Committed: []string{"file.go"}},
		allFiles:     []string{"file.go"},
		commits:      []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent:  strings.Join(content, "\n") + "\n",
		fileDiff:     diff,
	}
}

// TestProperty_Model_CursorSurvivesReflow is the resize-invariance property
// PLAN.md's step-5 list asks for ("the cursor's source-space Position is
// invariant under terminal resize; only its display position changes"),
// generalized to every mutation that changes the row↔source mapping: a
// resize, a wrap/line-number/removed-line toggle, and a content refresh.
//
// The cursor's canonical state is a viewport *row*, so all of those silently
// move it to a different source line — or off the end of shrunken content,
// where MoveDown is a permanent no-op and the highlight paints into the pane's
// padding.
func TestProperty_Model_CursorSurvivesReflow(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(16, 44).Draw(t, "height")
		m := initModel(genReflowMock(t), FilesMode, width, height)
		m = applyAction(m, tea.KeyPressMsg{Text: ".", Code: '.'})

		// Put the cursor somewhere non-trivial: a run of cursor-driven motions
		// only (nothing that reflows), so `before` is a settled position.
		for i := range rapid.IntRange(0, 12).Draw(t, "moves") {
			k := rapid.SampledFrom([]string{"j", "k", "l", "h", "G", "g"}).
				Draw(t, fmt.Sprintf("move%d", i))
			m = applyAction(m, tea.KeyPressMsg{Text: k, Code: rune(k[0])})
		}
		if !m.cursor.IsPlaced() {
			return
		}
		before := m.cursor.Pos(m.mainPane)

		// Reflow-only actions. Mode switches and item changes are excluded:
		// those legitimately re-place the cursor (scroll memory, first hunk),
		// which Reflow honours via cursor.seq.
		steps := rapid.IntRange(1, 6).Draw(t, "steps")
		for i := range steps {
			tag := fmt.Sprintf("reflow%d", i)
			var msg tea.Msg
			switch rapid.SampledFrom([]string{"resize", "toggle"}).Draw(t, tag+"_kind") {
			case "resize":
				msg = genResize(t, tag)
			default:
				k := rapid.SampledFrom([]string{"w", "n", "D"}).Draw(t, tag+"_toggle")
				msg = tea.KeyPressMsg{Text: k, Code: rune(k[0])}
			}
			m = applyAction(m, msg)

			if fail := cursorVisibilityFailure(m); fail != "" {
				t.Fatalf("after %s step %d: %s", tag, i, fail)
			}
			got := m.cursor.Pos(m.mainPane)
			if got.SourceLine != before.SourceLine {
				t.Fatalf("%s step %d moved the cursor's source line: %d -> %d (w=%d h=%d wrap=%v nums=%v)",
					tag, i, before.SourceLine, got.SourceLine, m.width, m.height, m.wordWrap, m.lineNumbers)
			}
		}
	})
}

// --- Deterministic regression tests for the A5 seam ------------------------

// hunkNavModel builds a files-mode model on a long file with two widely
// separated hunks, sized so a jump between them scrolls the viewport by more
// than a screen.
func hunkNavModel(t *testing.T) *Model {
	t.Helper()
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		allFiles:    []string{"file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: strings.Repeat("kept line\n", 200),
		fileDiff: `@@ -10,2 +10,3 @@
 kept
+added
 kept
@@ -150,2 +150,3 @@
 kept
+added
 kept
`,
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.Update(m.loadGitData())
	return m
}

// Regression (A5 sub-item 1): hunk-grain navigation scrolled the viewport
// without moving the cursor, so J from hunk 1 to hunk 2 left the cursor
// 100+ rows above the visible window.
func TestHunkNav_KeepsCursorVisible(t *testing.T) {
	m := hunkNavModel(t)
	m.focus = MainFocus

	if fail := cursorVisibilityFailure(m); fail != "" {
		t.Fatalf("after load: %s", fail)
	}
	result, _ := m.Update(tea.KeyPressMsg{Text: "J", Code: 'J'})
	m = result.(*Model)
	if fail := cursorVisibilityFailure(m); fail != "" {
		t.Fatalf("after J: %s", fail)
	}
	result, _ = m.Update(tea.KeyPressMsg{Text: "K", Code: 'K'})
	m = result.(*Model)
	if fail := cursorVisibilityFailure(m); fail != "" {
		t.Fatalf("after K: %s", fail)
	}
}

// Regression (A5 sub-item 1): search navigation scrolled via ScrollToSourceLine
// without moving the cursor.
func TestSearchNav_KeepsCursorVisible(t *testing.T) {
	m := hunkNavModel(t)
	m.focus = MainFocus

	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Text: "/", Code: '/'},
		tea.KeyPressMsg{Text: "k", Code: 'k'},
		tea.KeyPressMsg{Code: tea.KeyEnter},
	} {
		result, _ := m.Update(msg)
		m = result.(*Model)
	}
	if fail := cursorVisibilityFailure(m); fail != "" {
		t.Fatalf("after search confirm: %s", fail)
	}
	for i := range 40 {
		result, _ := m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
		m = result.(*Model)
		if fail := cursorVisibilityFailure(m); fail != "" {
			t.Fatalf("after search-next #%d: %s", i, fail)
		}
	}
}

// Regression (A5 sub-item 2): g/G, page keys and the wheel dragged the cursor
// along but never updated the visual-mode selection's active end, so the
// highlight and what `y` copies drifted apart from the cursor.
func TestVisualMode_SelectionFollowsViewportMotion(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
	}{
		{"G", tea.KeyPressMsg{Text: "G", Code: 'G'}},
		{"g", tea.KeyPressMsg{Text: "g", Code: 'g'}},
		{"pgdn", tea.KeyPressMsg{Code: tea.KeyPgDown}},
		{"pgup", tea.KeyPressMsg{Code: tea.KeyPgUp}},
		{"wheel", tea.MouseWheelMsg{X: 60, Y: 10, Button: tea.MouseWheelDown}},
		{"J", tea.KeyPressMsg{Text: "J", Code: 'J'}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := hunkNavModel(t)
			m.focus = MainFocus
			result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
			m = result.(*Model)
			if !m.selection.IsActive() {
				t.Fatal("visual mode did not activate")
			}
			result, _ = m.Update(tc.msg)
			m = result.(*Model)
			if !m.selection.IsActive() {
				t.Skip("selection dismissed by this key")
			}
			want := m.cursor.Endpoint(m.mainPane)
			if got := m.selection.active; got != want {
				t.Fatalf("selection.active=%+v, cursor endpoint=%+v", got, want)
			}
		})
	}
}

// Regression (A5 sub-item 6): updateLayout resized the pane without re-wrapping
// its content, so after a terminal resize the rows on screen stayed wrapped at
// the *old* width until the next content-setting tick — visibly wrong wrap
// points, and a stale row↔source mapping underneath the cursor and the
// selection highlight.
func TestResize_RewrapsMainPaneContent(t *testing.T) {
	long := strings.Repeat("x", 600)
	mg := &mockGit{
		repoInfo:     git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:         "abc",
		changedFiles: git.ChangedFilesResult{Committed: []string{"file.go"}},
		allFiles:     []string{"file.go"},
		commits:      []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent:  long + "\n",
		fileDiff:     "@@ -1,0 +1,1 @@\n+" + long + "\n",
	}
	m := initModel(mg, FilesMode, 200, 40)
	if !m.wordWrap {
		m = applyAction(m, tea.KeyPressMsg{Text: "w", Code: 'w'})
	}
	wide := viewportContentRowCount(m.mainPane)

	m = applyAction(m, tea.WindowSizeMsg{Width: 60, Height: 40})
	narrow := viewportContentRowCount(m.mainPane)

	if narrow <= wide {
		t.Errorf("content rows after narrowing 200 -> 60 = %d, was %d at the wider size; "+
			"the pane never re-wrapped, so it is still laid out for the old width", narrow, wide)
	}
}

// TestSeam_MainPaneNavigationGoesThroughNav is the compile-adjacent guard for
// A5's central claim: a call site must not be able to scroll the main pane
// without the cursor following, move the cursor without the viewport
// following, or move either without visual mode's selection following.
//
// Go can't express that as a type constraint inside a single package, so the
// rule is enforced here: the dispatcher layer (model.go and its per-mode
// content builders, plus search.go) may not name the pane's scroll
// primitives, the viewport's offset setters, m.cursor, or
// selection.SetActive. Everything goes through mainNav (mainnav.go), whose
// methods restore all three invariants.
//
// SetContent/SetPlainContent are deliberately *not* forbidden in
// maincontent.go: those builders run inside updateMainContent's
// mainNav.Reflow, which re-derives the cursor across the new mapping.
func TestSeam_MainPaneNavigationGoesThroughNav(t *testing.T) {
	forbidden := []struct {
		pattern *regexp.Regexp
		why     string
	}{
		// `\.cursor\b`, not `\.cursor\.`: passing m.cursor as an argument or
		// assigning to it skips the seam just as thoroughly as calling a
		// method on it, and a pattern requiring the trailing dot let an
		// argument pass through unnoticed.
		{regexp.MustCompile(`\.cursor\b`), "cursor access outside mainnav.go (ApplyHighlight excepted)"},
		{regexp.MustCompile(`mainPane\.(GoToTop|GoToBottom|ScrollToSourceLine|scrollToHunkStart|Update|SetSize|SetWordWrap|SetLineNumbers|ToggleShowRemoved|SetSearchQuery)\(`), "unpaired main-pane scroll/reflow primitive"},
		{regexp.MustCompile(`mainPane\.viewport\.(SetYOffset|GotoTop|GotoBottom|ScrollUp|ScrollDown)\(`), "raw viewport scroll"},
		{regexp.MustCompile(`selection\.SetActive\(`), "selection active end set outside the seam"},
		// A direct field write evades the SetActive( pattern above and skips
		// the seam just as thoroughly, so name the fields too.
		{regexp.MustCompile(`selection\.(active|anchor)\s*=`), "selection endpoint field written directly outside the seam"},
		{regexp.MustCompile(`drag\.AdvanceAutoScroll\(`), "unpaired drag auto-scroll"},
	}
	// The dispatcher layer: everything that reacts to user input or rebuilds
	// main-pane content.
	files := []string{"model.go", "maincontent.go", "search.go"}

	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, f := range forbidden {
				loc := f.pattern.FindString(line)
				if loc == "" {
					continue
				}
				// The one sanctioned cursor read outside the seam: View's
				// highlight pass, which only renders. `\.cursor\b` matches
				// just ".cursor", so the exception is checked against the
				// line — and only when *every* cursor mention on it is the
				// sanctioned call, so a second access can't ride along.
				if loc == ".cursor" &&
					strings.Count(line, ".cursor") == strings.Count(line, ".cursor.ApplyHighlight") {
					continue
				}
				t.Errorf("%s:%d: %s — route it through m.nav() (mainnav.go)\n\t%s",
					name, i+1, f.why, trimmed)
			}
		}
	}
}
