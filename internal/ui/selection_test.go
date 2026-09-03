package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
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
	if !copySelectionNotificationRE.MatchString(m.notifications.Text()) {
		t.Errorf("expected copied-selection notification, got %q", m.notifications.Text())
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

// yankTestPane builds a bare mainPane holding plain content, sized and
// wrapped as the case asks. Shared setup for the trailing-space yank
// tests below.
func yankTestPane(t *testing.T, content string, width int, wrap bool) *mainPane {
	t.Helper()
	mp := newMainPane()
	mp.SetSize(width, 20)
	mp.SetWordWrap(wrap)
	mp.SetPlainContent(content)
	return mp
}

// lineSelection builds a line-wise (`V`) selection covering source lines
// [from, to]; streamSelection builds the cell-wise (`v`) peer over the
// same lines, anchored at column 0 and extended to the last line's final
// content column.
func lineSelection(mp *mainPane, from, to int) *selection {
	s := newSelection()
	s.BeginLine(endpointAt(mp, from, 0))
	s.SetActive(endpointAt(mp, to, 0))
	return s
}

func endpointAt(mp *mainPane, sourceLine, col int) endpoint {
	pos := Position{SourceLine: sourceLine, Column: col}
	vpRow, _ := mp.positionToDisplay(pos)
	return endpoint{Pos: pos, VpRow: vpRow}
}

// TestSelection_LineWiseYankKeepsTrailingSpaces is the regression test for
// BUG_REPORTS.md "A whole-line yank still drops the source line's own
// trailing spaces". PROMPT.md's `### visual mode` makes a line-wise (`V`)
// selection a *source-text* operation: the copy reproduces each selected
// source line exactly, trailing whitespace included. stripGutterText trims
// trailing blanks off every rendered row, so the yank used to lose them.
func TestSelection_LineWiseYankKeepsTrailingSpaces(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
		wrap    bool
		from    int
		to      int
		want    string
	}{
		{
			name:    "single line, no wrap",
			content: "alpha   \nbravo",
			width:   80,
			from:    1, to: 1,
			want: "alpha   ",
		},
		{
			name:    "single line, wrap on but line fits",
			content: "alpha   \nbravo",
			width:   80,
			wrap:    true,
			from:    1, to: 1,
			want: "alpha   ",
		},
		{
			name:    "whitespace-only line",
			content: "alpha\n   \nbravo",
			width:   80,
			from:    2, to: 2,
			want: "   ",
		},
		{
			name:    "multi-line span keeps each line's own trailing run",
			content: "alpha \nbravo   \ncharlie",
			width:   80,
			from:    1, to: 3,
			want: "alpha \nbravo   \ncharlie",
		},
		{
			name:    "wrapped line: break spaces and own trailing run both survive",
			content: "aaaa bbbb cccc dddd  \nzz",
			width:   16,
			wrap:    true,
			from:    1, to: 1,
			want: "aaaa bbbb cccc dddd  ",
		},
		{
			name:    "line with no trailing run is unchanged",
			content: "alpha\nbravo",
			width:   80,
			from:    1, to: 2,
			want: "alpha\nbravo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mp := yankTestPane(t, tt.content, tt.width, tt.wrap)
			s := lineSelection(mp, tt.from, tt.to)
			got := s.SelectedText(dragGeometry{pane: mp})
			if got != tt.want {
				t.Errorf("V-yank = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSelection_StreamYankStillTrimsTrailingSpaces is the other half of the
// adjudicated policy: a cell-wise (`v`) selection is a *screen* operation,
// so trailing render padding stays out of the copy even when the selection
// runs to the end of the row.
func TestSelection_StreamYankStillTrimsTrailingSpaces(t *testing.T) {
	mp := yankTestPane(t, "alpha   \nbravo", 80, false)
	s := newSelection()
	s.BeginStream(endpointAt(mp, 1, 0))
	_, lastEnd := mp.wrapRowSourceColRange(mp.sourceLineToViewportOffset(1))
	s.SetActive(endpointAt(mp, 1, lastEnd))
	if got, want := s.SelectedText(dragGeometry{pane: mp}), "alpha"; got != want {
		t.Errorf("v-yank = %q, want %q (trailing padding must stay excluded)", got, want)
	}
}

// genTrailingSpaceLines produces source lines that actually carry trailing
// whitespace — the shape no existing generator emitted, which is why the
// V-yank trim survived every property in the suite. Bodies cover the cases
// that interact with the trailing run: empty (so the line is whitespace
// only), wide glyphs, interior space runs a wrap break can eat, and lines
// long enough to wrap several times.
func genTrailingSpaceLines(t *rapid.T) []string {
	n := rapid.IntRange(1, 8).Draw(t, "nLines")
	lines := make([]string, n)
	for i := range lines {
		body := rapid.SampledFrom([]string{
			"",
			"a",
			"alpha bravo",
			"café 日本語 🔥",
			"   leading",
			"word " + strings.Repeat("x", 40),
			"aaa   bbb   ccc   ddd   eee   fff   ggg   hhh",
		}).Draw(t, fmt.Sprintf("body%d", i))
		trail := rapid.IntRange(0, 5).Draw(t, fmt.Sprintf("trail%d", i))
		lines[i] = body + strings.Repeat(" ", trail)
	}
	return lines
}

// TestProperty_LineWiseYankRoundTripsSourceLines is the round-trip
// invariant behind the trailing-space fix: a line-wise (`V`) yank of any
// set of whole lines equals those post-boundary source lines byte for
// byte, trailing whitespace included, joined with newlines.
//
// Scoped to wrap mode and undecorated plain content, which is where the
// equality is even well-posed. With wrap off, horizontal truncation drops
// everything past the pane's right edge, so the copy is visible-only by
// design (see extractSourceRange); with diff annotations on, a `~` row
// renders the old and new text side by side and a copied row legitimately
// holds text that is not one source line.
func TestProperty_LineWiseYankRoundTripsSourceLines(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		lines := genTrailingSpaceLines(t)
		width := rapid.IntRange(24, 120).Draw(t, "width")
		lineNumbers := rapid.Bool().Draw(t, "lineNumbers")

		mp := newMainPane()
		mp.SetSize(width, 24)
		mp.SetWordWrap(true)
		mp.SetLineNumbers(lineNumbers)
		mp.SetPlainContent(strings.Join(lines, "\n"))

		// The contract is against post-boundary source text: SetPlainContent
		// expands tabs, so that is the text a yank must reproduce.
		srcLines := strings.Split(expandTabs(strings.Join(lines, "\n")), "\n")

		from := rapid.IntRange(1, len(srcLines)).Draw(t, "from")
		to := rapid.IntRange(from, len(srcLines)).Draw(t, "to")

		s := lineSelection(mp, from, to)
		got := s.SelectedText(dragGeometry{pane: mp})
		want := strings.Join(srcLines[from-1:to], "\n")
		if got != want {
			t.Fatalf("V-yank of lines %d..%d = %q, want %q (width=%d lineNumbers=%v)",
				from, to, got, want, width, lineNumbers)
		}
	})
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
