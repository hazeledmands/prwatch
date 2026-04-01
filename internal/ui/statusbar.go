package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

func renderStatusBar(width int, info git.RepoInfoResult, pr git.PRInfoResult, mode Mode, confirming bool) string {
	if confirming {
		msg := " Quit? Press q/Q to confirm, any other key to cancel"
		pad := width - lipgloss.Width(msg)
		if pad > 0 {
			msg += strings.Repeat(" ", pad)
		}
		return statusBarConfirmStyle.Width(width).Render(msg)
	}

	// Left: branch and repo info
	var branchDisplay string
	if info.IsDetachedHead {
		branchDisplay = fmt.Sprintf("detached @ %s", info.HeadSHA)
	} else {
		branchDisplay = info.Branch
	}

	left := fmt.Sprintf(" %s", branchDisplay)
	if info.RepoName != "" {
		left = fmt.Sprintf(" %s @ %s", branchDisplay, info.RepoName)
	}
	if info.Worktree != "" {
		left += " (worktree)"
	}

	// Middle: mode indicator
	var modeStr string
	switch mode {
	case FileDiffMode:
		modeStr = modeFileStyle.Render("[diff]")
	case FileViewMode:
		modeStr = modeFileStyle.Render("[file]")
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
