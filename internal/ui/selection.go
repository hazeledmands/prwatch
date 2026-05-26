package ui

// selectionMode names the vim-style visual mode of a keyboard
// selection. Block mode (Ctrl-V) is deferred per PLAN.md — it has weird
// interactions with diff gutters and mixed +/- lines.
type selectionMode int

const (
	selectionNone selectionMode = iota
	selectionStream
	selectionLine
)

// selection is the keyboard-visual-mode peer to dragSelection: anchor
// captured at v/V, active follows the cursor as motions extend the
// range. Highlight rendering goes through the same clip pipeline as
// drag (buildHighlightClips + paintHighlightClips) so both selection
// kinds paint identically.
//
// In stream mode, the selection covers character-grained spans bounded
// by anchor.Column / active.Column. In line mode, both ends extend to
// cover full source lines regardless of column delta — anchor's column
// snaps to 0 and active's column extends past the last wrap row's end.
//
// 5c will collapse selection + dragSelection into one Mode-tagged type;
// for now they live in parallel so the cursor/visual work can land
// without touching drag's existing behavior.
type selection struct {
	mode   selectionMode
	anchor Position
	active Position
}

func newSelection() *selection {
	return &selection{}
}

// IsActive reports whether a selection is in progress.
func (s *selection) IsActive() bool { return s.mode != selectionNone }

// HasRange reports whether the selection actually covers content.
// Line mode covers at least one full line whenever active; stream mode
// needs anchor != active.
func (s *selection) HasRange() bool {
	switch s.mode {
	case selectionStream:
		return s.anchor != s.active
	case selectionLine:
		return true
	default:
		return false
	}
}

// BeginStream starts a stream-mode selection anchored at pos.
func (s *selection) BeginStream(pos Position) {
	s.mode = selectionStream
	s.anchor = pos
	s.active = pos
}

// BeginLine starts a line-mode selection anchored at pos.
func (s *selection) BeginLine(pos Position) {
	s.mode = selectionLine
	s.anchor = pos
	s.active = pos
}

// SetActive updates the live end (cursor position) as the user moves.
func (s *selection) SetActive(pos Position) {
	s.active = pos
}

// Cancel dismisses any active selection.
func (s *selection) Cancel() {
	s.mode = selectionNone
	s.anchor = Position{}
	s.active = Position{}
}

// resolveEnds returns the source-ordered (upper, lower) ends of the
// selection paired with their viewport rows, ready for
// buildHighlightClips. In line mode, ends are extended to cover full
// source lines.
func (s *selection) resolveEnds(pane *mainPane) (upper, lower orderedEnd, ok bool) {
	if !s.HasRange() {
		return orderedEnd{}, orderedEnd{}, false
	}
	a := s.anchor
	b := s.active
	if !positionLess(a, b) {
		a, b = b, a
	}
	aVp, _ := pane.positionToDisplay(a)
	bVp, _ := pane.positionToDisplay(b)
	upper = orderedEnd{Position: a, VpRow: aVp}
	lower = orderedEnd{Position: b, VpRow: bVp}

	if s.mode == selectionLine {
		// Extend upper to column 0 on the first wrap row of its source line,
		// and lower to past the last wrap row's content end.
		upperFirstRow := pane.sourceLineToViewportOffset(upper.SourceLine)
		upper.Column = 0
		upper.VpRow = upperFirstRow

		lowerFirstRow := pane.sourceLineToViewportOffset(lower.SourceLine)
		lowerCount := pane.wrapRowCountAtVpRow(lowerFirstRow)
		lastVp := lowerFirstRow + lowerCount - 1
		_, lastEnd := pane.wrapRowSourceColRange(lastVp)
		lower.Column = lastEnd
		lower.VpRow = lastVp
	}
	return upper, lower, true
}

// ApplyHighlight paints reverse-video over the selection's display
// span. Returns content unchanged when no selection is active.
func (s *selection) ApplyHighlight(content string, g dragGeometry) string {
	upper, lower, ok := s.resolveEnds(g.pane)
	if !ok {
		return content
	}
	return paintHighlightClips(content, g, buildHighlightClips(g.pane, upper, lower))
}

// SelectedText extracts the selection's source text — the same path
// drag.SelectedText uses, just sourced from anchor/active instead of
// drag endpoints.
func (s *selection) SelectedText(g dragGeometry) string {
	upper, lower, ok := s.resolveEnds(g.pane)
	if !ok {
		return ""
	}
	return extractSourceRange(g.pane, upper, lower)
}
