package ui

// layoutDimensions returns the sidebar pixel width, main pane pixel width, and
// shared content height given the outer dimensions and current configuration.
// When sidebarHidden, sidebar gets width 0 and main pane gets the full width
// minus its own 2-column border. Otherwise sidebar takes sidebarPct of total
// width and main pane takes the remainder minus 4 columns of borders.
func layoutDimensions(width, height, statusRows, sidebarPct int, sidebarHidden bool) (sidebarW, mainW, contentH int) {
	contentH = max(0, height-statusRows-2) // top + bottom border rows
	if sidebarHidden {
		return 0, max(0, width-2), contentH
	}
	sidebarW = max(0, width*sidebarPct/100)
	mainW = max(0, width-sidebarW-4)
	return sidebarW, mainW, contentH
}

// contentHeight returns the number of terminal rows available to content
// below the status bar and inside the pane borders — the same contentH the
// panes are sized with in updateLayout.
//
// This is the only way to ask the question. It exists because the help
// overlay, which occupies the region the panes would have, needs the panes'
// content height to scroll in step with them, and four call sites used to
// spell that out as max(1, m.height-m.statusBarLines()-2). That floor of 1
// disagreed with layoutDimensions' floor of 0 on a terminal too short for its
// own chrome, which is the class of divergence that keeps producing off-by-one
// click targeting.
//
// Zero is the honest answer there: a terminal whose status bar and borders
// already fill it has no content rows, the panes are sized 0, and the help
// overlay renders nothing rather than one row with nowhere to go. Its scroll
// entry points are all total at 0 — Render slices an empty range, paging and
// wheel clamp to no-ops.
func (m *Model) contentHeight() int {
	_, _, contentH := layoutDimensions(m.width, m.height, m.statusBarLines(), m.sidebarPct, m.sidebarHidden)
	return contentH
}

// mainPaneContentRows returns the inclusive screen-row range of the main pane
// content area (between the title row and the bottom border). top is the
// first content row; bottom is the last content row before the border.
// When the available area is empty, top == bottom.
func mainPaneContentRows(statusRows, height int) (top, bottom int) {
	const topBorder, titleRow = 1, 1
	top = statusRows + topBorder + titleRow
	bottom = height - 2
	if bottom < top {
		bottom = top
	}
	return top, bottom
}
