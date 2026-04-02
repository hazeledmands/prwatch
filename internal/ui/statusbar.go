package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// statusBarData holds all the data needed to render the status bar.
type statusBarData struct {
	info           git.RepoInfoResult
	pr             git.PRInfoResult
	ciStatus       git.CIStatusResult
	reviews        []git.PRReview
	reviewRequests []git.PRReviewRequest
	commentCount   int
	prError        string // error message for GitHub API issues
	mode           Mode
	confirming     bool
	uncommitCount  int
	commitCount    int
	behindCount    int // commits behind base branch
	showHelp       bool
	hoverX         int // mouse hover position for highlighting
	hoverY         int
}

// modeLabel tracks the position and mode of a clickable mode label.
type modeLabel struct {
	mode  Mode
	start int // x offset within the rendered line (after padding)
	end   int // exclusive x offset
}

// line3Target identifies what a click on line 3 should jump to.
type line3Target int

const (
	line3Description line3Target = iota
	line3Reviews
	line3Comments
	line3CI
)

type line3Label struct {
	target line3Target
	start  int
	end    int
}

// statusBarLineCount returns how many lines the status bar will occupy.
func statusBarLineCount(data statusBarData) int {
	count := 1 // line 1 always shown
	if data.info.RepoName != "" || data.info.Branch != "" {
		count++ // line 2: git status
	}
	if data.pr.Number > 0 {
		count++ // line 3: PR status
	}
	return count
}

func renderStatusBar(width int, data statusBarData) (string, []modeLabel, []line3Label) {
	if data.confirming {
		msg := " Quit? Press q/Q to confirm, any other key to cancel"
		pad := width - lipgloss.Width(msg)
		if pad > 0 {
			msg += strings.Repeat(" ", pad)
		}
		return statusBarConfirmStyle.Width(width).Render(msg), nil, nil
	}

	line1, labels := renderLine1(width, data)
	result := line1

	// Line 2: only show for git repos
	if data.info.RepoName != "" || data.info.Branch != "" {
		result += "\n" + renderLine2(width, data)
	}

	// Line 3: only show if there's a PR
	var line3Labels []line3Label
	if data.pr.Number > 0 {
		l3, l3Labels := renderLine3(width, data)
		line3Labels = l3Labels
		result += "\n" + l3
	}

	return result, labels, line3Labels
}

// renderLine1: overall status — mode, directory, worktree
// Returns the rendered line and the clickable mode label positions.
func renderLine1(width int, data statusBarData) (string, []modeLabel) {
	// Build mode bar: show all modes, highlight the active one
	modes := []struct {
		mode Mode
		name string
	}{
		{FileViewMode, "file"},
		{FileDiffMode, "diff"},
		{CommitMode, "commits"},
		{PRViewMode, "pr"},
		{HelpMode, "help"},
	}

	var modeItems []string
	var labels []modeLabel
	// Account for statusBarStyle padding (1 char) + leading space in parts[0]
	pos := 2 // 1 for Padding(0,1) + 1 for " " prefix
	hoverMode := Mode(-1)

	for _, m := range modes {
		// Skip pr mode if no PR
		if m.mode == PRViewMode && data.pr.Number == 0 {
			continue
		}

		displayText := m.name
		displayWidth := len(displayText)

		label := modeLabel{mode: m.mode, start: pos, end: pos + displayWidth}
		labels = append(labels, label)

		// Check if hover is on this label
		isHovered := data.hoverY == 0 && data.hoverX >= label.start && data.hoverX < label.end

		// Help mode is "active" when help overlay is shown
		isActive := m.mode == data.mode || (m.mode == HelpMode && data.showHelp)
		if isActive {
			if isHovered {
				modeItems = append(modeItems, modeActiveHoverStyle.Render(displayText))
			} else {
				modeItems = append(modeItems, modeActiveStyle.Render(displayText))
			}
		} else {
			if isHovered {
				hoverMode = m.mode
				modeItems = append(modeItems, modeHoverStyle.Render(displayText))
			} else {
				modeItems = append(modeItems, modeInactiveStyle.Render(displayText))
			}
		}

		pos += displayWidth + 1 // +1 for space separator
	}
	_ = hoverMode
	modeStr := strings.Join(modeItems, " ")

	dirName := data.info.DirName
	if dirName == "" {
		dirName = data.info.RepoName
	}

	var parts []string
	parts = append(parts, " "+modeStr)
	if dirName != "" {
		parts = append(parts, dirName)
	}
	if data.info.Worktree != "" && data.info.RepoName != "" && data.info.DirName != data.info.RepoName {
		parts = append(parts, "in "+data.info.RepoName)
	}
	if data.info.RepoName == "" && data.info.Branch == "" {
		parts = append(parts, "Not a git repo")
	}

	bar := strings.Join(parts, " · ")
	// Truncate to prevent wrapping — statusBarStyle has Padding(0,1) = 2 chars
	if lipgloss.Width(bar) > width-2 {
		bar = truncateToWidth(bar, width-2)
	}
	return statusBarStyle.Width(width).Render(bar), labels
}

// renderLine2: local git status — branch, uncommitted, unpushed, commits
func renderLine2(width int, data statusBarData) string {
	info := data.info

	// Branch and merge base
	var branchDisplay string
	if info.IsDetachedHead {
		branchDisplay = fmt.Sprintf("detached @ %s", info.HeadSHA)
	} else {
		branchDisplay = info.Branch
	}

	// Show "branch -> base" if not on main/master
	if info.Branch != "main" && info.Branch != "master" && !info.IsDetachedHead {
		// Extract base branch name from upstream or default to "main"
		base := "main"
		if info.Upstream != "" {
			parts := strings.Split(info.Upstream, "/")
			if len(parts) > 1 {
				base = parts[len(parts)-1]
			}
		}
		if base != info.Branch {
			branchDisplay = info.Branch + " → " + base
		}
	}

	var parts []string
	parts = append(parts, " "+branchDisplay)

	if data.uncommitCount > 0 {
		parts = append(parts, fmt.Sprintf("%d uncommitted", data.uncommitCount))
	}
	if info.AheadCount > 0 {
		parts = append(parts, fmt.Sprintf("%d unpushed", info.AheadCount))
	}
	if data.commitCount > 0 {
		parts = append(parts, fmt.Sprintf("%d commits", data.commitCount))
	}
	if data.behindCount > 0 {
		parts = append(parts, fmt.Sprintf("%d behind", data.behindCount))
	}
	if data.prError != "" {
		parts = append(parts, data.prError)
	} else if data.pr.Number == 0 {
		parts = append(parts, "No PR")
	}

	bar := strings.Join(parts, " · ")
	// Truncate if too wide for the content area (width - 2 padding)
	if lipgloss.Width(bar) > width-2 {
		bar = truncateToWidth(bar, width-2)
	}
	return statusBarPRStyle.Width(width).Render(bar)
}

// renderLine3: github status — PR, draft, reviews, comments, CI
func renderLine3(width int, data statusBarData) (string, []line3Label) {
	type part struct {
		text   string
		target line3Target
	}

	var parts []part
	var labels []line3Label

	// PR link
	prLink := fmt.Sprintf("PR #%d: %s", data.pr.Number, data.pr.Title)
	if data.pr.URL != "" {
		prLink = makeHyperlink(data.pr.URL, prLink)
	}
	parts = append(parts, part{" " + prLink, line3Description})

	if data.pr.IsDraft {
		parts = append(parts, part{"[DRAFT]", line3Description})
	}
	if data.pr.State == "MERGED" {
		parts = append(parts, part{"[MERGED]", line3Description})
	}

	// Reviews and review requests
	reviewStr := renderReviews(data.reviews, data.reviewRequests, data.pr.ReviewDecision)
	if reviewStr != "" {
		parts = append(parts, part{reviewStr, line3Reviews})
	}

	// Comments
	if data.commentCount > 0 {
		parts = append(parts, part{fmt.Sprintf("%d comments", data.commentCount), line3Comments})
	}

	// CI status
	ciStr := renderCIStatusEmoji(data.ciStatus)
	if ciStr != "" {
		parts = append(parts, part{ciStr, line3CI})
	}

	// Build the bar and track positions
	// statusBarDimStyle has Padding(0,1), so pos starts at 1
	pos := 1
	var textParts []string
	for i, p := range parts {
		displayWidth := lipgloss.Width(p.text)
		labels = append(labels, line3Label{target: p.target, start: pos, end: pos + displayWidth})
		textParts = append(textParts, p.text)
		pos += displayWidth
		if i < len(parts)-1 {
			pos += 3 // " · " separator
		}
	}

	bar := strings.Join(textParts, " · ")
	return statusBarDimStyle.Width(width).Render(bar), labels
}

// renderCIStatusEmoji returns CI status as an emoji plus text label.
func renderCIStatusEmoji(ci git.CIStatusResult) string {
	switch ci.State {
	case "SUCCESS":
		text := "✅ CI passing"
		if ci.URL != "" {
			text = makeHyperlink(ci.URL, text)
		}
		return text
	case "FAILURE":
		text := "❌ CI failing"
		if ci.URL != "" {
			text = makeHyperlink(ci.URL, text)
		}
		return text
	case "PENDING":
		text := "⏳ CI pending"
		if ci.URL != "" {
			text = makeHyperlink(ci.URL, text)
		}
		return text
	}
	return ""
}

// renderCIStatus returns CI status with check/cross symbols (for tests).
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

func renderReviews(reviews []git.PRReview, requests []git.PRReviewRequest, decision string) string {
	if len(reviews) == 0 && len(requests) == 0 && decision == "" {
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
	if len(requests) > 0 {
		parts = append(parts, fmt.Sprintf("%d👀", len(requests)))
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
// if truncation occurs.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
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
