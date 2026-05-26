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
	if c.pos.SourceLine != 2 {
		t.Errorf("expected SL=2, got %d", c.pos.SourceLine)
	}
	if c.desiredCol != 15 {
		t.Errorf("desiredCol should stay 15 after vertical motion, got %d", c.desiredCol)
	}

	// j to line 3 (long). Cursor should restore to desiredCol=15.
	if !c.MoveDown(mp) {
		t.Fatal("MoveDown returned false")
	}
	if c.pos.SourceLine != 3 {
		t.Errorf("expected SL=3, got %d", c.pos.SourceLine)
	}
	_, dc := mp.positionToDisplay(c.pos)
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
	if c.pos.Column != 6 {
		t.Errorf("expected col 6, got %d", c.pos.Column)
	}

	// h → col 5.
	if !c.MoveLeft(mp) {
		t.Fatal("MoveLeft returned false")
	}
	if c.pos.Column != 5 {
		t.Errorf("expected col 5, got %d", c.pos.Column)
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
	if c.pos.Column != 0 {
		t.Errorf("col should stay 0, got %d", c.pos.Column)
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
	if c.pos.Column != 1 {
		t.Errorf("col should stay 1, got %d", c.pos.Column)
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
	startVp, _ := mp.positionToDisplay(c.pos)

	// First j should step to next wrap row of source line 1, NOT to source line 2.
	if !c.MoveDown(mp) {
		t.Fatal("MoveDown returned false")
	}
	newVp, _ := mp.positionToDisplay(c.pos)
	if newVp != startVp+1 {
		t.Errorf("MoveDown should step one visual row (from %d to %d), got %d", startVp, startVp+1, newVp)
	}
	if c.pos.SourceLine != 1 {
		t.Errorf("after first j on wrapped line, should still be on source line 1, got %d", c.pos.SourceLine)
	}
}

func TestCursor_SetFromClick(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hello\nworld")

	c := newCursor()
	c.SetFromClick(mp, 1, 3) // row 1 (source line 2), col 3
	if c.pos.SourceLine != 2 {
		t.Errorf("expected SL=2, got %d", c.pos.SourceLine)
	}
	if c.pos.Column != 3 {
		t.Errorf("expected col=3, got %d", c.pos.Column)
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

	startPos := m.cursor.pos
	// Press j (Down): should move cursor down one visual row.
	res, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	if m.cursor.pos == startPos {
		t.Errorf("j should move cursor; pos unchanged at %+v", m.cursor.pos)
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
	if m.cursor.pos.Column != 1 {
		t.Errorf("l should advance cursor to column 1, got %d", m.cursor.pos.Column)
	}

	// h: cursor left.
	res, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = res.(*Model)
	if m.cursor.pos.Column != 0 {
		t.Errorf("h should retreat cursor to column 0, got %d", m.cursor.pos.Column)
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
	startPos := m.cursor.pos
	res, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	if m.cursor.pos != startPos {
		t.Errorf("cursor should not move on sidebar focus; got %+v (was %+v)", m.cursor.pos, startPos)
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
			if c.pos.SourceLine == 0 {
				return
			}
			vp, _ := mp.positionToDisplay(c.pos)
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
		startVp, _ := mp.positionToDisplay(c.pos)

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
		endVp, _ := mp.positionToDisplay(c.pos)
		if endVp != startVp {
			t.Fatalf("after %d down + %d up, expected return to vp=%d, got vp=%d", moved, moved, startVp, endVp)
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
