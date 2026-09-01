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
// Endpoints are the same shape as drag's: Position + VpRow. VpRow
// disambiguates wrap-row-boundary anchors AND decoration rows (red
// removed-line rows whose Position.SourceLine is most-recent-before),
// so visual-mode selection on red rows renders correctly.
//
// 5c will collapse selection + dragSelection into one Mode-tagged type;
// for now they live in parallel so the cursor/visual work can land
// without touching drag's existing behavior.
type selection struct {
	mode   selectionMode
	anchor endpoint
	active endpoint
}

func newSelection() *selection {
	return &selection{}
}

// IsActive reports whether a selection is in progress.
func (s *selection) IsActive() bool { return s.mode != selectionNone }

// HasRange reports whether the selection actually covers content.
// Line mode covers at least one full line whenever active; stream mode
// needs anchor != active (by Position; VpRow ties don't count as a
// range).
func (s *selection) HasRange() bool {
	switch s.mode {
	case selectionStream:
		return s.anchor.Pos != s.active.Pos
	case selectionLine:
		return true
	default:
		return false
	}
}

// BeginStream starts a stream-mode selection anchored at ep.
func (s *selection) BeginStream(ep endpoint) {
	s.mode = selectionStream
	s.anchor = ep
	s.active = ep
}

// BeginLine starts a line-mode selection anchored at ep.
func (s *selection) BeginLine(ep endpoint) {
	s.mode = selectionLine
	s.anchor = ep
	s.active = ep
}

// SetActive updates the live end (cursor endpoint) as the user moves.
func (s *selection) SetActive(ep endpoint) {
	s.active = ep
}

// Reflow re-derives the endpoints' viewport rows after a change to the
// row↔source mapping (content refresh, wrap/line-number/removed toggle,
// resize). The endpoints' source-space Positions are the durable part; the
// VpRow side-channel is display state and goes stale with the mapping,
// which would leave the highlight painting over unrelated rows.
func (s *selection) Reflow(pane *mainPane) {
	if s.mode == selectionNone {
		return
	}
	reflow := func(ep endpoint) endpoint {
		if ep.OutsideDir != 0 {
			return ep
		}
		vp, _ := pane.positionToDisplay(ep.Pos)
		ep.VpRow = vp
		return ep
	}
	s.anchor = reflow(s.anchor)
	s.active = reflow(s.active)
}

// Cancel dismisses any active selection.
func (s *selection) Cancel() {
	s.mode = selectionNone
	s.anchor = endpoint{}
	s.active = endpoint{}
}

// resolveEnds returns the source-ordered (upper, lower) ends ready for
// buildHighlightClips. In line mode, ends are extended to cover full
// source lines.
func (s *selection) resolveEnds(pane *mainPane) (upper, lower orderedEnd, ok bool) {
	if !s.HasRange() {
		return orderedEnd{}, orderedEnd{}, false
	}
	a := orderedEnd{Position: s.anchor.Pos, VpRow: s.anchor.VpRow}
	b := orderedEnd{Position: s.active.Pos, VpRow: s.active.VpRow}
	if !positionLess(a.Position, b.Position) {
		a, b = b, a
	}
	upper = a
	lower = b

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
