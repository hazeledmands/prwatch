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

	// Diff coloring
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	diffRemoveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
	diffChangeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF")) // yellow for changed
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#89DCEB"))
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)

	// Status bar confirm
	statusBarConfirmStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#F9E2AF")).
				Foreground(lipgloss.Color("#1E1E2E")).
				Padding(0, 1)

	// Mode indicator — explicit background matches statusBarStyle to prevent color bleeding
	modeActiveStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Background(statusBarBg)
	modeActiveHoverStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Background(statusBarBg).Underline(true)
	modeInactiveStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#D0C8E8")).Background(statusBarBg)
	modeHoverStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Background(statusBarBg).Underline(true)

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
)
