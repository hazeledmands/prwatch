package ui

import "charm.land/lipgloss/v2"

var (
	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#7D56F4")).
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

	// Diff coloring
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	diffRemoveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#89DCEB"))
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)

	// Status bar confirm
	statusBarConfirmStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#F9E2AF")).
				Foreground(lipgloss.Color("#1E1E2E")).
				Padding(0, 1)

	// Mode indicator
	modeFileStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A6E3A1"))
	modeCommitStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89DCEB"))
)
