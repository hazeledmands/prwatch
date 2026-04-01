package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

func renderStatusBar(width int, info git.RepoInfoResult, pr git.PRInfoResult, mode Mode) string {
	// Left: branch and repo info
	left := fmt.Sprintf(" %s", info.Branch)
	if info.RepoName != "" {
		left = fmt.Sprintf(" %s @ %s", info.Branch, info.RepoName)
	}
	if info.Worktree != "" {
		left += " (worktree)"
	}

	// Middle: mode indicator
	var modeStr string
	switch mode {
	case FileMode:
		modeStr = modeFileStyle.Render("[files]")
	case CommitMode:
		modeStr = modeCommitStyle.Render("[commits]")
	}

	// Right: PR info
	var right string
	if pr.Number > 0 {
		right = fmt.Sprintf("PR #%d: %s %s ", pr.Number, pr.Title, pr.URL)
	} else {
		right = "No PR "
	}

	// Calculate padding
	padding := width - lipgloss.Width(left) - lipgloss.Width(modeStr) - lipgloss.Width(right)
	if padding < 0 {
		padding = 0
	}
	leftPad := padding / 2
	rightPad := padding - leftPad

	bar := left
	for i := 0; i < leftPad; i++ {
		bar += " "
	}
	bar += modeStr
	for i := 0; i < rightPad; i++ {
		bar += " "
	}
	bar += right

	return statusBarStyle.Width(width).Render(bar)
}
