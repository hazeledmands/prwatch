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
