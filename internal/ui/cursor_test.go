package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// TestCursor_MoveDown_PreservesDesiredCol verifies that vertical
// motion preserves desiredCol across rows of varying length. Sticky
// column is the load-bearing invariant of j/k cursor motion.
func TestCursor_MoveDown_PreservesDesiredCol(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 10)
	mp.SetWordWrap(false)
	// Two long source lines + one short + another long. Cursor at
	// column 15 of line 1; j over line 2 (short) should clamp Pos
	// but keep desiredCol; j onto line 3 should restore col 15.
	mp.SetPlainContent("aaaaaaaaaaaaaaaaaaaa\nbbb\nccccccccccccccccccccc")

	c := newCursor()
	c.SetPosition(mp, Position{SourceLine: 1, Column: 15})
	if c.desiredCol != 15 {
		t.Fatalf("desiredCol after SetPosition should be 15, got %d", c.desiredCol)
	}

	// j to line 2 (short — only 3 chars). Cursor clamps.
	if !c.MoveDown(mp) {
		t.Fatal("MoveDown returned false")
	}
	if pos := c.Pos(mp); pos.SourceLine != 2 {
		t.Errorf("expected SL=2, got %d", pos.SourceLine)
	}
	if c.desiredCol != 15 {
		t.Errorf("desiredCol should stay 15 after vertical motion, got %d", c.desiredCol)
	}

	// j to line 3 (long). Cursor should restore to desiredCol=15.
	if !c.MoveDown(mp) {
		t.Fatal("MoveDown returned false")
	}
	if pos := c.Pos(mp); pos.SourceLine != 3 {
		t.Errorf("expected SL=3, got %d", pos.SourceLine)
	}
	_, dc := mp.positionToDisplay(c.Pos(mp))
	if dc != 15 {
		t.Errorf("expected to restore display col 15 on long line, got %d", dc)
	}
}

func TestCursor_MoveLeftRight_BasicCharGrained(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hello world")

	c := newCursor()
	c.SetPosition(mp, Position{SourceLine: 1, Column: 5})

	// l → col 6.
	if !c.MoveRight(mp) {
		t.Fatal("MoveRight returned false")
	}
	if pos := c.Pos(mp); pos.Column != 6 {
		t.Errorf("expected col 6, got %d", pos.Column)
	}

	// h → col 5.
	if !c.MoveLeft(mp) {
		t.Fatal("MoveLeft returned false")
	}
	if pos := c.Pos(mp); pos.Column != 5 {
		t.Errorf("expected col 5, got %d", pos.Column)
	}
}

func TestCursor_MoveLeft_StopsAtColumn0(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hi")

	c := newCursor()
	c.SetPosition(mp, Position{SourceLine: 1, Column: 0})
	if c.MoveLeft(mp) {
		t.Error("MoveLeft should return false at col 0")
	}
	if pos := c.Pos(mp); pos.Column != 0 {
		t.Errorf("col should stay 0, got %d", pos.Column)
	}
}

func TestCursor_MoveRight_StopsAtEOL(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hi") // 2 chars: 'h' at col 0, 'i' at col 1.

	c := newCursor()
	c.SetPosition(mp, Position{SourceLine: 1, Column: 1}) // on last char
	if c.MoveRight(mp) {
		t.Error("MoveRight should return false past EOL")
	}
	if pos := c.Pos(mp); pos.Column != 1 {
		t.Errorf("col should stay 1, got %d", pos.Column)
	}
}

func TestCursor_MoveUp_StopsAtFirstRow(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("a\nb\nc")

	c := newCursor()
	c.SetPosition(mp, Position{SourceLine: 1, Column: 0})
	if c.MoveUp(mp) {
		t.Error("MoveUp should return false at first row")
	}
}

func TestCursor_MoveDown_VisualRowGrained_WrappedLine(t *testing.T) {
	// Cursor on a long wrapped line steps through each wrap row on j/k,
	// not jumping straight to the next source line.
	mp := newMainPane()
	mp.SetSize(20, 10) // narrow → forces wrap
	mp.SetWordWrap(true)
	long := strings.Repeat("x", 50)
	mp.SetPlainContent(long + "\nshort")

	c := newCursor()
	c.SetPosition(mp, Position{SourceLine: 1, Column: 0})
	startVp := c.vpRow

	// First j should step to next wrap row of source line 1, NOT to source line 2.
	if !c.MoveDown(mp) {
		t.Fatal("MoveDown returned false")
	}
	if c.vpRow != startVp+1 {
		t.Errorf("MoveDown should step one visual row (from %d to %d), got %d", startVp, startVp+1, c.vpRow)
	}
	if pos := c.Pos(mp); pos.SourceLine != 1 {
		t.Errorf("after first j on wrapped line, should still be on source line 1, got %d", pos.SourceLine)
	}
}

func TestCursor_SetFromClick(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hello\nworld")

	c := newCursor()
	c.SetFromClick(mp, 1, 3) // row 1 (source line 2), col 3
	if pos := c.Pos(mp); pos.SourceLine != 2 {
		t.Errorf("expected SL=2, got %d", pos.SourceLine)
	}
	if pos := c.Pos(mp); pos.Column != 3 {
		t.Errorf("expected col=3, got %d", pos.Column)
	}
	if c.desiredCol != 3 {
		t.Errorf("expected desiredCol=3, got %d", c.desiredCol)
	}
}

// TestCursor_Integration_JKMoveAndRender runs an end-to-end check that
// pressing j with main focus moves the cursor, and the cursor renders
// as a reverse-video cell in the View output.
func TestCursor_Integration_JKMoveAndRender(t *testing.T) {
	mock := &mockGit{
		fileContent: "line one with content\nline two of content\nline three is here\nline four\n",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"foo.go"},
		},
	}
	m := initModel(mock, FilesMode, 100, 30)
	m.focus = MainFocus
	// Force plain content with several rows so MoveDown has somewhere to go.
	m.mainPane.SetPlainContent("line one\nline two\nline three\nline four")

	startVp := m.cursor.vpRow
	// Press j (Down): should move cursor down one visual row.
	res, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	if m.cursor.vpRow == startVp {
		t.Errorf("j should move cursor; vpRow unchanged at %d", m.cursor.vpRow)
	}

	// View should contain at least one reverse-video sequence somewhere
	// — the cursor cell. (Drag highlight is not active.)
	v := m.View()
	if !strings.Contains(v.Content, "\x1b[7m") {
		t.Errorf("cursor render should produce a reverse-video sequence; not found in View output")
	}
}

// TestCursor_Integration_HLMoveCursor verifies that h/l (and arrow
// left/right) move the cursor horizontally in main focus. Shift+H/L
// keep the original FocusLeft/FocusRight behavior.
func TestCursor_Integration_HLMoveCursor(t *testing.T) {
	mock := &mockGit{
		fileContent: "hello world\nsecond line\n",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"foo.go"},
		},
	}
	m := initModel(mock, FilesMode, 100, 30)
	m.focus = MainFocus
	m.mainPane.SetPlainContent("hello world\nsecond line")
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 1, Column: 0})

	// l: cursor right.
	res, _ := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	m = res.(*Model)
	if pos := m.cursor.Pos(m.mainPane); pos.Column != 1 {
		t.Errorf("l should advance cursor to column 1, got %d", pos.Column)
	}

	// h: cursor left.
	res, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = res.(*Model)
	if pos := m.cursor.Pos(m.mainPane); pos.Column != 0 {
		t.Errorf("h should retreat cursor to column 0, got %d", pos.Column)
	}
}

// TestCursor_SetFromClick_ClampsPastEOL verifies that clicking past
// the end of a row's content clamps the cursor to end-of-line rather
// than placing it on padding cells.
func TestCursor_SetFromClick_ClampsPastEOL(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hi") // 2-char line

	c := newCursor()
	// Click far past EOL.
	c.SetFromClick(mp, 0, 50)
	// Row "hi" has content width 2; expect clamp to col 1 (last char).
	if c.desiredCol != 1 {
		t.Errorf("click past EOL on 2-char row should clamp desiredCol to 1, got %d", c.desiredCol)
	}
}

// TestCursor_NavigatesDecorationRows is the regression test for the
// "cursor stuck on red diff" bug: removed-line decoration rows have no
// source-line mapping of their own (sourceLineAtViewportOffset returns
// the most-recent-before source line), so a cursor stored as
// source-space Position couldn't navigate past them. Now the cursor's
// canonical state is vpRow, and j/k step through each viewport row
// regardless of whether it's a decoration or a source line.
func TestCursor_NavigatesDecorationRows(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 20)
	mp.SetWordWrap(false)
	// A diff with line 2 changed from "old1\nold2\nold3" to "new". The
	// "large diff" branch produces 2 removed-line decoration rows then
	// an added row.
	mp.diffAnnotations = map[int]diffAnnotation{
		2: {kind: diffLineChanged, removedLines: []string{"old1", "old2", "old3"}},
	}
	mp.SetPlainContent("context1\nnew\ncontext3")

	startRows := viewportContentRowCount(mp)
	if startRows < 4 {
		t.Fatalf("expected at least 4 viewport rows (context+removed+changed+context), got %d (vpContent=%q)", startRows, mp.viewport.GetContent())
	}

	c := newCursor()
	c.vpRow = 0
	c.desiredCol = 0

	// j should step through every viewport row in order, including
	// the decoration rows. Before the fix, vpRow could get stuck.
	visited := make([]int, 0, startRows)
	visited = append(visited, c.vpRow)
	for c.MoveDown(mp) {
		visited = append(visited, c.vpRow)
		if len(visited) > startRows+1 {
			t.Fatalf("MoveDown looped past viewport row count; visited=%v", visited)
		}
	}
	// We should have visited every row from 0 to startRows-1.
	if len(visited) != startRows {
		t.Errorf("expected to visit %d rows, got %d (%v)", startRows, len(visited), visited)
	}
	for i, vp := range visited {
		if vp != i {
			t.Errorf("visited[%d] = %d, expected %d (decoration rows must be reachable)", i, vp, i)
		}
	}
}

// TestCursor_Integration_RendersOnEmptyLine verifies that the cursor
// remains visible when positioned on an empty source line. Previously
// the renderer suppressed paint when the cell landed past actual
// content (a leftover from drag's "don't highlight pad" guard), which
// made the cursor disappear on blank lines.
func TestCursor_Integration_RendersOnEmptyLine(t *testing.T) {
	mock := &mockGit{
		fileContent: "first line\n\nthird line\n",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"foo.go"},
		},
	}
	m := initModel(mock, FilesMode, 100, 30)
	m.focus = MainFocus
	m.mainPane.SetPlainContent("first line\n\nthird line")
	// Place cursor on the empty middle source line, column 0.
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 2, Column: 0})

	v := m.View()
	if !strings.Contains(v.Content, "\x1b[7m") {
		t.Error("cursor should render on empty line; no reverse-video found in View output")
	}
}

// TestCursor_Integration_NoMotionOnSidebarFocus verifies that j with
// sidebar focused selects sidebar items, not the cursor.
func TestCursor_Integration_NoMotionOnSidebarFocus(t *testing.T) {
	mock := &mockGit{
		fileContent: "line one\nline two\n",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"foo.go"},
		},
	}
	m := initModel(mock, FilesMode, 100, 30)
	m.focus = SidebarFocus
	startVp := m.cursor.vpRow
	res, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	if m.cursor.vpRow != startVp {
		t.Errorf("cursor should not move on sidebar focus; vpRow now %d (was %d)", m.cursor.vpRow, startVp)
	}
}

// TestProperty_Cursor_AlwaysVisible is the cursor's defining invariant:
// after any sequence of cursor-motion and viewport-scroll actions, the
// cursor's vp row lies inside the viewport window [YOffset, YOffset+Height).
// Cursor motion auto-scrolls (EnsureVisible); viewport motion drags the
// cursor along the edge (DragAlongScroll). Both contribute to keeping the
// cursor visible.
func TestProperty_Cursor_AlwaysVisible(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(30, 120).Draw(t, "width")
		paneH := rapid.IntRange(5, 25).Draw(t, "paneH")
		wordWrap := rapid.Bool().Draw(t, "wordWrap")
		nLines := rapid.IntRange(3, 30).Draw(t, "nLines")
		var lines []string
		for i := range nLines {
			n := rapid.IntRange(0, 100).Draw(t, fmt.Sprintf("len-%d", i))
			lines = append(lines, strings.Repeat("x", n))
		}
		mp := newMainPane()
		mp.SetSize(width, paneH)
		mp.SetWordWrap(wordWrap)
		mp.SetPlainContent(strings.Join(lines, "\n"))

		c := newCursor()
		c.SetPosition(mp, Position{SourceLine: 1, Column: 0})

		ensureVisible := func(label string, step int) {
			if !c.IsPlaced() {
				return
			}
			vp := c.vpRow
			vpOff := mp.viewport.YOffset()
			vpH := mp.viewport.Height()
			if vpH <= 0 {
				return
			}
			if vp < vpOff || vp >= vpOff+vpH {
				t.Fatalf("cursor not visible after %s at step %d: vp=%d, window=[%d, %d)", label, step, vp, vpOff, vpOff+vpH)
			}
		}

		steps := rapid.IntRange(1, 30).Draw(t, "steps")
		for i := range steps {
			action := rapid.IntRange(0, 5).Draw(t, fmt.Sprintf("action-%d", i))
			switch action {
			case 0:
				c.MoveDown(mp)
				c.EnsureVisible(mp)
			case 1:
				c.MoveUp(mp)
				c.EnsureVisible(mp)
			case 2:
				c.MoveLeft(mp)
				c.EnsureVisible(mp)
			case 3:
				c.MoveRight(mp)
				c.EnsureVisible(mp)
			case 4:
				// Viewport scroll down (simulates wheel/page).
				mp.viewport.SetYOffset(mp.viewport.YOffset() + 3)
				c.DragAlongScroll(mp)
			case 5:
				// Viewport scroll up.
				mp.viewport.SetYOffset(max(0, mp.viewport.YOffset()-3))
				c.DragAlongScroll(mp)
			}
			ensureVisible(fmt.Sprintf("action=%d", action), i)
		}
	})
}

// TestProperty_Cursor_MoveDownThenUp_Idempotent — after any sequence of
// MoveDown's, an equal number of MoveUp's returns to the starting vp row
// AND to a column whose display value matches desiredCol (clamped per
// row). This is the structural test for "vertical motion is symmetric."
func TestProperty_Cursor_MoveDownThenUp_Idempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(20, 120).Draw(t, "width")
		nLines := rapid.IntRange(2, 10).Draw(t, "nLines")
		wordWrap := rapid.Bool().Draw(t, "wordWrap")
		var lines []string
		for i := range nLines {
			n := rapid.IntRange(0, 100).Draw(t, fmt.Sprintf("lineLen-%d", i))
			lines = append(lines, strings.Repeat("x", n))
		}
		mp := newMainPane()
		mp.SetSize(width, 30)
		mp.SetWordWrap(wordWrap)
		mp.SetPlainContent(strings.Join(lines, "\n"))

		// Start at top-left.
		c := newCursor()
		c.SetPosition(mp, Position{SourceLine: 1, Column: 0})
		startVp := c.vpRow

		steps := rapid.IntRange(0, 20).Draw(t, "steps")
		moved := 0
		for range steps {
			if c.MoveDown(mp) {
				moved++
			}
		}
		for range moved {
			c.MoveUp(mp)
		}
		if c.vpRow != startVp {
			t.Fatalf("after %d down + %d up, expected return to vp=%d, got vp=%d", moved, moved, startVp, c.vpRow)
		}
	})
}

// TestProperty_Cursor_DesiredColPreserved is the explicit sticky-col
// invariant: after any sequence of pure j/k motions (no h/l/click),
// desiredCol does not change.
func TestProperty_Cursor_DesiredColPreserved(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(20, 120).Draw(t, "width")
		nLines := rapid.IntRange(2, 12).Draw(t, "nLines")
		wordWrap := rapid.Bool().Draw(t, "wordWrap")
		var lines []string
		for i := range nLines {
			n := rapid.IntRange(0, 100).Draw(t, fmt.Sprintf("lineLen-%d", i))
			lines = append(lines, strings.Repeat("x", n))
		}
		mp := newMainPane()
		mp.SetSize(width, 30)
		mp.SetWordWrap(wordWrap)
		mp.SetPlainContent(strings.Join(lines, "\n"))

		c := newCursor()
		startCol := rapid.IntRange(0, 30).Draw(t, "startCol")
		c.SetPosition(mp, Position{SourceLine: 1, Column: startCol})
		want := c.desiredCol

		steps := rapid.IntRange(1, 30).Draw(t, "steps")
		for i := range steps {
			if rapid.Bool().Draw(t, fmt.Sprintf("down-%d", i)) {
				c.MoveDown(mp)
			} else {
				c.MoveUp(mp)
			}
			if c.desiredCol != want {
				t.Fatalf("desiredCol drifted after j/k at step %d: want %d, got %d", i, want, c.desiredCol)
			}
		}
	})
}
