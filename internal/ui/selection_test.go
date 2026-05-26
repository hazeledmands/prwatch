package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// TestSelection_BeginStream_AnchorsAtCursor verifies that `v` from
// MainFocus anchors at the current cursor position.
func TestSelection_BeginStream_AnchorsAtCursor(t *testing.T) {
	m := newVisualModeTestModel(t)
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 2, Column: 3})

	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)

	if !m.selection.IsActive() {
		t.Fatal("v should activate selection")
	}
	if m.selection.mode != selectionStream {
		t.Errorf("v should enter stream mode, got %d", m.selection.mode)
	}
	if m.selection.anchor.Pos != (Position{SourceLine: 2, Column: 3}) {
		t.Errorf("anchor should be at cursor; got %+v", m.selection.anchor.Pos)
	}
	if m.selection.active != m.selection.anchor {
		t.Errorf("active should equal anchor at start, got %+v vs %+v", m.selection.active, m.selection.anchor)
	}
}

func TestSelection_BeginLine_OnCapitalV(t *testing.T) {
	m := newVisualModeTestModel(t)
	res, _ := m.Update(tea.KeyPressMsg{Text: "V", Code: 'V'})
	m = res.(*Model)

	if !m.selection.IsActive() {
		t.Fatal("V should activate selection")
	}
	if m.selection.mode != selectionLine {
		t.Errorf("V should enter line mode, got %d", m.selection.mode)
	}
}

// TestSelection_DownExtendsSelection verifies cursor motion (j) updates
// selection.active while in visual mode.
func TestSelection_DownExtendsSelection(t *testing.T) {
	m := newVisualModeTestModel(t)
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 1, Column: 0})

	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	anchorBefore := m.selection.anchor

	res, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)

	if m.selection.anchor != anchorBefore {
		t.Errorf("anchor should not move on j, got %+v (was %+v)", m.selection.anchor, anchorBefore)
	}
	if m.selection.active == anchorBefore {
		t.Errorf("active should follow cursor after j; still at anchor %+v", m.selection.active)
	}
	if cursorEp := m.cursor.Endpoint(m.mainPane); m.selection.active != cursorEp {
		t.Errorf("active should equal cursor endpoint; active=%+v, cursor=%+v", m.selection.active, cursorEp)
	}
}

// TestSelection_HighlightCoversDecorationRowsBetweenEnds is the
// regression test for "visual selection over red+green diff does
// weird things". The old buildHighlightClips iterated source lines
// and skipped any row that didn't have a sourceToFormatLine entry,
// so removed-line decoration rows (red) between the selection's
// upper and lower ends were left un-painted — selection appeared
// to "skip" them. Now buildHighlightClips iterates vp rows directly
// and emits a clip for every row between upper.VpRow and lower.VpRow,
// including decoration rows.
func TestSelection_HighlightCoversDecorationRowsBetweenEnds(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 20)
	mp.SetWordWrap(false)
	mp.diffAnnotations = map[int]diffAnnotation{
		2: {kind: diffLineChanged, removedLines: []string{"old1", "old2"}},
	}
	mp.SetPlainContent("context1\nnew\ncontext3")

	// Build a selection spanning the full content (context1 ... context3).
	// Upper at row 0 col 0 (context1); lower at the last viewport row.
	rowCount := viewportContentRowCount(mp)
	if rowCount < 4 {
		t.Fatalf("expected at least 4 rows (context+removed×2+new+context); got %d", rowCount)
	}
	upper := orderedEnd{Position: Position{SourceLine: 1, Column: 0}, VpRow: 0}
	lastVp := rowCount - 1
	_, lastEnd := mp.wrapRowSourceColRange(lastVp)
	lower := orderedEnd{Position: Position{SourceLine: 3, Column: lastEnd}, VpRow: lastVp}

	clips := buildHighlightClips(mp, upper, lower)
	if len(clips) != rowCount {
		t.Fatalf("expected one clip per vp row (%d), got %d", rowCount, len(clips))
	}
	// Every viewport row in [0, lastVp] must have a clip — including
	// decoration rows (the removed lines).
	seen := make(map[int]bool, len(clips))
	for _, c := range clips {
		seen[c.vpRow] = true
	}
	for vp := 0; vp <= lastVp; vp++ {
		if !seen[vp] {
			t.Errorf("row %d not in highlight clips; decoration row was skipped", vp)
		}
	}
}

// TestSelection_StreamLineToggle_PreservesEnds verifies that toggling
// between stream and line mode (V from v, v from V) keeps the original
// anchor and active positions so the character range survives the
// round-trip. Vim semantics: `v` in line mode and `V` in stream mode
// just flip the mode flag, they don't reset the range.
func TestSelection_StreamLineToggle_PreservesEnds(t *testing.T) {
	m := newVisualModeTestModel(t)
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 1, Column: 0})

	// Enter stream mode and extend.
	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	res, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	res, _ = m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	m = res.(*Model)
	anchorAfterExtend := m.selection.anchor
	activeAfterExtend := m.selection.active
	if anchorAfterExtend == activeAfterExtend {
		t.Fatal("expected non-trivial range after j+l")
	}

	// V: switch to line mode. Anchor/active must not change.
	res, _ = m.Update(tea.KeyPressMsg{Text: "V", Code: 'V'})
	m = res.(*Model)
	if m.selection.mode != selectionLine {
		t.Errorf("V should switch to line mode, got %d", m.selection.mode)
	}
	if m.selection.anchor != anchorAfterExtend || m.selection.active != activeAfterExtend {
		t.Errorf("V should preserve anchor/active; got %+v/%+v, want %+v/%+v",
			m.selection.anchor, m.selection.active, anchorAfterExtend, activeAfterExtend)
	}

	// v: switch back to stream mode. Anchor/active still preserved.
	res, _ = m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	if m.selection.mode != selectionStream {
		t.Errorf("v should switch back to stream mode, got %d", m.selection.mode)
	}
	if m.selection.anchor != anchorAfterExtend || m.selection.active != activeAfterExtend {
		t.Errorf("v should preserve anchor/active across toggle; got %+v/%+v, want %+v/%+v",
			m.selection.anchor, m.selection.active, anchorAfterExtend, activeAfterExtend)
	}
}

// TestSelection_SecondVDismisses verifies that pressing v while already
// in stream mode dismisses (vim convention).
func TestSelection_SecondVDismisses(t *testing.T) {
	m := newVisualModeTestModel(t)
	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	if !m.selection.IsActive() {
		t.Fatal("first v should activate")
	}
	res, _ = m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	if m.selection.IsActive() {
		t.Errorf("second v should dismiss; mode=%d", m.selection.mode)
	}
}

// TestSelection_EscDismisses verifies Esc cancels the selection.
func TestSelection_EscDismisses(t *testing.T) {
	m := newVisualModeTestModel(t)
	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	if !m.selection.IsActive() {
		t.Fatal("v should activate")
	}

	res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = res.(*Model)
	if m.selection.IsActive() {
		t.Errorf("Esc should dismiss; mode=%d", m.selection.mode)
	}
}

// TestSelection_YankCopiesAndDismisses verifies y copies the selection
// text to the clipboard (we observe the "copied selection" notification
// as a proxy) and clears the selection.
func TestSelection_YankCopiesAndDismisses(t *testing.T) {
	m := newVisualModeTestModel(t)
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 1, Column: 0})

	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	// Move down a row to make the selection non-empty.
	res, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	if !m.selection.HasRange() {
		t.Fatal("after v+j the selection should have range")
	}

	res, _ = m.Update(tea.KeyPressMsg{Text: "y", Code: 'y'})
	m = res.(*Model)

	if m.selection.IsActive() {
		t.Errorf("y should dismiss the selection")
	}
	if !copySelectionNotificationRE.MatchString(m.notification) {
		t.Errorf("expected copied-selection notification, got %q", m.notification)
	}
}

// TestSelection_ModeSwitchDismisses verifies switching modes cancels
// any visual-mode selection.
func TestSelection_ModeSwitchDismisses(t *testing.T) {
	m := newVisualModeTestModel(t)
	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	if !m.selection.IsActive() {
		t.Fatal("v should activate")
	}

	// Switch to commits mode via the numeric binding.
	res, _ = m.Update(tea.KeyPressMsg{Text: "2", Code: '2'})
	m = res.(*Model)
	if m.selection.IsActive() {
		t.Errorf("mode switch should dismiss selection; mode=%d", m.selection.mode)
	}
}

// TestSelection_MouseClickDismisses verifies a mouse click in the main
// pane dismisses any active visual-mode selection (mouse expresses
// fresh intent).
func TestSelection_MouseClickDismisses(t *testing.T) {
	m := newVisualModeTestModel(t)
	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	if !m.selection.IsActive() {
		t.Fatal("v should activate")
	}

	// Click somewhere in the main pane content area.
	statusRows := m.statusBarLines()
	contentY := statusRows + 1 + 1 // status + top border + title row
	sidebarW := m.sidebarPixelWidth()
	clickX := sidebarW + 1 + m.mainPane.gutterWidth + 1
	res, _ = m.Update(tea.MouseClickMsg{X: clickX, Y: contentY, Button: tea.MouseLeft})
	m = res.(*Model)

	if m.selection.IsActive() {
		t.Errorf("mouse click should dismiss selection; mode=%d", m.selection.mode)
	}
}

// TestSelection_RendersHighlight verifies the selection paints a
// reverse-video region in the View output.
func TestSelection_RendersHighlight(t *testing.T) {
	m := newVisualModeTestModel(t)
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 1, Column: 0})

	res, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = res.(*Model)
	res, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)
	res, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = res.(*Model)

	v := m.View()
	// At least one reverse-video sequence (selection paints it; cursor
	// also paints, but suppressed because we're not testing that here).
	if !strings.Contains(v.Content, "\x1b[7m") {
		t.Error("expected selection highlight (reverse-video) in View output")
	}
}

// newVisualModeTestModel builds a Model with multi-line plain content
// in the main pane, MainFocus, and the cursor pre-positioned at the
// top. Shared setup for the visual-mode integration tests above.
func newVisualModeTestModel(t *testing.T) *Model {
	t.Helper()
	mock := &mockGit{
		fileContent: "alpha\nbravo\ncharlie\ndelta\necho\n",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"foo.go"},
		},
	}
	m := initModel(mock, FilesMode, 80, 30)
	m.focus = MainFocus
	m.mainPane.SetPlainContent("alpha\nbravo\ncharlie\ndelta\necho")
	m.cursor.SetPosition(m.mainPane, Position{SourceLine: 1, Column: 0})
	m.selection.Cancel()
	return m
}
