package ui

import "charm.land/lipgloss/v2"

var (
	// Common colors
	statusBarBg = lipgloss.Color("#7D56F4")

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(statusBarBg).
			Foreground(lipgloss.Color("#FAFAFA")).
			Padding(0, 1)

	// Sidebar
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555"))

	sidebarFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4"))

	sidebarItemStyle         = lipgloss.NewStyle()
	sidebarSelectedItemStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("#333")).
					Foreground(lipgloss.Color("#FAFAFA")).
					Bold(true)

	// Main pane
	mainPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555"))

	mainPaneFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4"))

	// Main pane sticky title bar (above viewport, inside border).
	mainPaneTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888")).
				Bold(true)

	// Sidebar uncommitted files
	sidebarUncommittedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888"))
	sidebarUncommittedSelectedStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("#333")).
					Foreground(lipgloss.Color("#AAA"))
	sidebarSeparatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#555"))
	sidebarHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888")).
				Bold(true)
	sidebarDeletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F38BA8")) // red for deleted files
	sidebarDeletedSelectedStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("#333")).
					Foreground(lipgloss.Color("#F38BA8"))
	sidebarDeletedHoverStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A2A")).
					Foreground(lipgloss.Color("#F38BA8"))

	// Diff coloring (foreground only — used for inline ~ segments and the
	// gutter mark; layered with chroma syntax highlighting on the body).
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	diffRemoveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
	diffChangeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF")) // yellow for changed
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#89DCEB"))
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)

	// Files-mode diff backgrounds. Subtle tints that read as "added/removed/
	// changed line" while leaving chroma's per-token foreground colors
	// visible. Combined fg+bg variants are used for the gutter mark and for
	// flat (un-highlighted) rows like pre-context removed lines.
	diffAddBg    = lipgloss.Color("#1F2D24")
	diffRemoveBg = lipgloss.Color("#3A1F26")
	diffChangeBg = lipgloss.Color("#33301F")

	diffAddLineStyle    = diffAddStyle.Background(diffAddBg)
	diffRemoveLineStyle = diffRemoveStyle.Background(diffRemoveBg)
	diffChangeLineStyle = diffChangeStyle.Background(diffChangeBg)

	// Status bar confirm
	statusBarConfirmStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#F9E2AF")).
				Foreground(lipgloss.Color("#1E1E2E")).
				Padding(0, 1)

	// Status bar PR line
	statusBarPRStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#45475A")).
				Foreground(lipgloss.Color("#CDD6F4")).
				Padding(0, 1)

	statusBarDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#45475A")).
				Foreground(lipgloss.Color("#888")).
				Padding(0, 1)

	// CI status
	ciPassStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	ciFailStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
	ciPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF"))

	// Inline diff: retained (unchanged) text within a changed line
	diffRetainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF")) // yellow

	// Search highlight
	searchHighlightStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#F9E2AF")).
				Foreground(lipgloss.Color("#1E1E2E"))

	// Hover styles
	sidebarHoverStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A2A2A")).
				Foreground(lipgloss.Color("#FAFAFA"))
	sidebarUncommittedHoverStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A2A")).
					Foreground(lipgloss.Color("#AAA"))

	// Pinned-but-not-cursor styles. When the sidebar cursor moves off the
	// file currently being shown in the main pane (e.g. cursor is on a
	// directory), the file the user is looking at is rendered with this
	// style so they can locate it without scanning.
	sidebarPinnedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#89DCEB")).
				Bold(true)
	sidebarPinnedDimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#89DCEB"))
	sidebarPinnedUncommittedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#89DCEB")).
					Bold(true)
	sidebarPinnedDeletedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#F38BA8")).
					Bold(true).Underline(true)

	// Dim styles for prefix/suffix within sidebar items
	sidebarDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888"))
	sidebarSelectedDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#333")).
				Foreground(lipgloss.Color("#AAA"))
	sidebarHoverDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A2A2A")).
				Foreground(lipgloss.Color("#AAA"))
)
