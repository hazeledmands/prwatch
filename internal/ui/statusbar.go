package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// statusBarData holds all the data needed to render the status bar.
type statusBarData struct {
	info          git.RepoInfoResult
	pr            git.PRInfoResult
	ciStatus      git.CIStatusResult
	reviews       []git.PRReview
	commentCount  int
	mode          Mode
	confirming    bool
	uncommitCount int
	commitCount   int
}

func renderStatusBar(width int, data statusBarData) string {
	if data.confirming {
		msg := " Quit? Press q/Q to confirm, any other key to cancel"
		pad := width - lipgloss.Width(msg)
		if pad > 0 {
			msg += strings.Repeat(" ", pad)
		}
		return statusBarConfirmStyle.Width(width).Render(msg)
	}

	// Line 1: branch info, mode, git status summary
	line1 := renderLine1(width, data)
	// Line 2: PR info
	line2 := renderLine2(width, data)
	return line1 + "\n" + line2
}

func renderLine1(width int, data statusBarData) string {
	info := data.info

	// Branch display
	var branchDisplay string
	if info.IsDetachedHead {
		branchDisplay = fmt.Sprintf("detached @ %s", info.HeadSHA)
	} else {
		branchDisplay = info.Branch
	}
	if info.Upstream != "" {
		branchDisplay += " → " + info.Upstream
	}

	// Left: branch @ repo (dir)
	left := " " + branchDisplay
	if info.RepoName != "" {
		repoDisplay := info.RepoName
		if info.RepoURL != "" {
			repoDisplay = makeHyperlink(info.RepoURL, info.RepoName)
		}
		left = fmt.Sprintf(" %s @ %s", branchDisplay, repoDisplay)
	}
	if info.DirName != "" && info.DirName != info.RepoName {
		left += fmt.Sprintf(" (%s)", info.DirName)
	}
	if info.Worktree != "" {
		left += " [wt]"
	}

	// Mode indicator
	var modeStr string
	switch data.mode {
	case FileDiffMode:
		modeStr = modeFileStyle.Render("[diff]")
	case FileViewMode:
		modeStr = modeFileStyle.Render("[file]")
	case CommitMode:
		modeStr = modeCommitStyle.Render("[commits]")
	}

	// Right: git status summary
	var statusParts []string
	if info.AheadCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("↑%d", info.AheadCount))
	}
	if data.uncommitCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("%d uncommitted", data.uncommitCount))
	}
	if data.commitCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("%d commits", data.commitCount))
	}
	right := strings.Join(statusParts, " · ")
	if right != "" {
		right += " "
	}

	// contentWidth accounts for lipgloss padding (0,1) on each side
	contentWidth := width - 2

	// Progressively truncate sections to fit within contentWidth.
	// Priority: keep mode visible, then right status, then truncate left.
	modeW := lipgloss.Width(modeStr)
	rightW := lipgloss.Width(right)
	leftW := lipgloss.Width(left)

	available := contentWidth - modeW
	if available < 0 {
		available = 0
	}

	// Truncate right if it doesn't fit alongside mode
	if rightW > available/2 {
		right = truncateToWidth(right, available/2)
		rightW = lipgloss.Width(right)
	}

	// Truncate left to whatever remains
	leftMax := available - rightW
	if leftW > leftMax {
		left = truncateToWidth(left, leftMax)
		leftW = lipgloss.Width(left)
	}

	padding := contentWidth - leftW - modeW - rightW
	if padding < 0 {
		padding = 0
	}
	leftPad := padding / 2
	rightPad := padding - leftPad

	bar := left + strings.Repeat(" ", leftPad) + modeStr + strings.Repeat(" ", rightPad) + right
	return statusBarStyle.Width(width).Render(bar)
}

func renderLine2(width int, data statusBarData) string {
	if data.pr.Number == 0 {
		return statusBarDimStyle.Width(width).Render(" No PR")
	}

	// PR link with OSC 8 hyperlink
	prLink := fmt.Sprintf("PR #%d: %s", data.pr.Number, data.pr.Title)
	if data.pr.URL != "" {
		prLink = makeHyperlink(data.pr.URL, prLink)
	}

	var parts []string
	parts = append(parts, " "+prLink)

	// Draft indicator
	if data.pr.IsDraft {
		parts = append(parts, "draft")
	}

	// CI status
	ciStr := renderCIStatus(data.ciStatus)
	if ciStr != "" {
		parts = append(parts, ciStr)
	}

	// Reviews
	reviewStr := renderReviews(data.reviews, data.pr.ReviewDecision)
	if reviewStr != "" {
		parts = append(parts, reviewStr)
	}

	// Comments
	if data.commentCount > 0 {
		parts = append(parts, fmt.Sprintf("%d comments", data.commentCount))
	}

	bar := strings.Join(parts, " · ")
	return statusBarPRStyle.Width(width).Render(bar)
}

func renderCIStatus(ci git.CIStatusResult) string {
	switch ci.State {
	case "SUCCESS":
		text := "CI ✓"
		if ci.URL != "" {
			text = makeHyperlink(ci.URL, text)
		}
		return ciPassStyle.Render(text)
	case "FAILURE":
		text := "CI ✗"
		if ci.URL != "" {
			text = makeHyperlink(ci.URL, text)
		}
		return ciFailStyle.Render(text)
	case "PENDING":
		text := "CI ⟳"
		if ci.URL != "" {
			text = makeHyperlink(ci.URL, text)
		}
		return ciPendingStyle.Render(text)
	}
	return ""
}

func renderReviews(reviews []git.PRReview, decision string) string {
	if len(reviews) == 0 && decision == "" {
		return ""
	}

	var approved, rejected, pending int
	for _, r := range reviews {
		switch r.State {
		case "APPROVED":
			approved++
		case "CHANGES_REQUESTED":
			rejected++
		default:
			pending++
		}
	}

	var parts []string
	if approved > 0 {
		parts = append(parts, fmt.Sprintf("%d✓", approved))
	}
	if rejected > 0 {
		parts = append(parts, fmt.Sprintf("%d✗", rejected))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}

	if len(parts) == 0 {
		switch decision {
		case "APPROVED":
			return "approved"
		case "CHANGES_REQUESTED":
			return "changes requested"
		case "REVIEW_REQUIRED":
			return "review required"
		}
		return ""
	}
	return strings.Join(parts, "/")
}

// truncateToWidth truncates a string to the given display width, appending "…"
// if truncation occurs. Uses lipgloss.Width for accurate display width.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	// Reserve space for ellipsis
	target := maxWidth - 1
	if target < 0 {
		target = 0
	}
	runes := []rune(s)
	for end := len(runes); end > 0; end-- {
		candidate := string(runes[:end])
		if lipgloss.Width(candidate) <= target {
			return candidate + "…"
		}
	}
	if maxWidth >= 1 {
		return "…"
	}
	return ""
}

// makeHyperlink creates an OSC 8 terminal hyperlink.
func makeHyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}
