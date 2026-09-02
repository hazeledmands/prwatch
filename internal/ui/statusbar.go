package ui

import (
	"fmt"
	"strings"

	"github.com/hazeledmands/prwatch/internal/git"
)

// statusBarData holds all the data needed to render the status bar.
type statusBarData struct {
	info             git.RepoInfoResult
	pr               git.PRInfoResult
	ciStatus         git.CIStatusResult
	reviews          []git.PRReview
	reviewRequests   []git.PRReviewRequest
	commentCount     int
	prError          string // error message for GitHub API issues
	mode             Mode
	confirming       bool
	uncommitCount    int
	commitCount      int
	behindCount      int  // commits behind base branch
	behindKnown      bool // behindCount was measured; when false it is unknown and not rendered
	changedFileCount int  // total changed files (committed + uncommitted)
	prLoading        bool // true if PR data is still being fetched
	showHelp         bool
	hoverX           int // mouse hover position for highlighting
	hoverY           int
	// scopeHandle is non-nil when the user has scrubbed the commit-range
	// scope away from its default position. renderLine2 prefixes the line
	// with the handle indicator so it's visible across all modes.
	scopeHandle *scopeHandleInfo
}

// modeLabel tracks the position and mode of a clickable mode label.
type modeLabel struct {
	mode  Mode
	start int // x offset within the rendered line (after padding)
	end   int // exclusive x offset
}

// line2Target identifies what a click on line 2 should do.
type line2Target int

const (
	line2CommitsMode line2Target = iota
	line2FilesMode
)

type line2Label struct {
	target line2Target
	start  int
	end    int
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
// Every branch here must mirror a branch in renderStatusBar — the two are
// the layout and render halves of the same geometry.
func statusBarLineCount(data statusBarData) int {
	if data.confirming {
		// renderStatusBar replaces the whole bar with the single-line
		// quit prompt.
		return 1
	}
	count := 1 // line 1 always shown
	if data.info.RepoName != "" || data.info.Branch != "" {
		count++ // line 2: git status
	}
	if data.pr.Number > 0 || data.prError != "" || data.prLoading {
		count++ // line 3: PR status, error, or loading
	}
	return count
}

func renderStatusBar(width int, data statusBarData) (string, []modeLabel, []line2Label, []line3Label) {
	if data.confirming {
		msg := " Quit? Press q/Q to confirm, any other key to cancel"
		// padToWidth rather than a counted shortfall, for the same reason as
		// padToHeight and renderTitleRow: width is not additive across
		// concatenation. This message is ASCII today, but the rule is the rule.
		msg = padToWidth(msg, width)
		return statusBarConfirmStyle.Width(width).Render(msg), nil, nil, nil
	}

	line1, labels := renderLine1(width, data)
	result := line1

	// Line 2: only show for git repos
	var line2Labels []line2Label
	if data.info.RepoName != "" || data.info.Branch != "" {
		l2, l2Labels := renderLine2(width, data)
		line2Labels = l2Labels
		result += "\n" + l2
	}

	// Line 3: the GitHub row — an active API error, else the PR summary, else
	// the loading indicator. The error comes first because PROMPT.md:83 puts
	// the API error message on this line unconditionally ("if the github API
	// is returning errors, then put the error message here!"); it used to be
	// reachable only before the first successful PR fetch, so every later
	// failure was invisible while stale PR data sat on the line looking
	// current. The PR data itself is still rendered — in the PR pane — so
	// both spec clauses hold. prError is cleared by the next successful fetch,
	// so "active" is exactly "prError != \"\"". See INCONSISTENCIES.md,
	// "GitHub API errors hidden once PR data exists".
	//
	// Every branch here occupies exactly one row, matching the single
	// increment in statusBarLineCount.
	var line3Labels []line3Label
	switch {
	case data.prError != "":
		// Sanitized like any other display text: an error string is not
		// under our control (it can carry gh's stderr), and a raw newline
		// would split this "row" in two and desync the layout.
		errText := " " + sanitizeDisplayText(data.prError)
		errText = ellipsize(errText, width-2)
		result += "\n" + statusBarDimStyle.Width(width).Render(errText)
	case data.pr.Number > 0:
		l3, l3Labels := renderLine3(width, data)
		line3Labels = l3Labels
		result += "\n" + l3
	case data.prLoading:
		// Show loading indicator while PR data is being fetched
		loadText := " Loading from GitHub…"
		loadText = ellipsize(loadText, width-2)
		result += "\n" + statusBarDimStyle.Width(width).Render(loadText)
	}

	return result, labels, line2Labels, line3Labels
}

// ANSI SGR sequences for inline mode styling. We use these instead of
// lipgloss.Render on individual mode labels because Render appends \e[0m
// (full reset) which clears the background color set by the outer
// statusBarStyle, leaving dark gaps between mode names.
const (
	ansiWhiteFg = "\x1b[38;2;250;250;250m" // #FAFAFA
	ansiDimFg   = "\x1b[38;2;208;200;232m" // #D0C8E8
	ansiBoldOn  = "\x1b[1m"
	ansiBoldOff = "\x1b[22m"
	ansiUlOn    = "\x1b[4m"
	ansiUlOff   = "\x1b[24m"
)

// styleModeInline applies foreground/bold/underline attributes to a mode label
// without emitting a full ANSI reset, so the outer statusBarStyle background
// is preserved.
func styleModeInline(text string, active, hovered bool) string {
	var b strings.Builder
	if active {
		b.WriteString(ansiBoldOn)
		b.WriteString(ansiWhiteFg)
	} else {
		b.WriteString(ansiDimFg)
	}
	if hovered {
		b.WriteString(ansiUlOn)
	}
	b.WriteString(text)
	// Reset to baseline state: no bold, no underline, white foreground.
	// No \x1b[0m — that would kill the background.
	b.WriteString(ansiBoldOff + ansiUlOff + ansiWhiteFg)
	return b.String()
}

// renderLine1: overall status — mode, directory, worktree
// Returns the rendered line and the clickable mode label positions.
func renderLine1(width int, data statusBarData) (string, []modeLabel) {
	// Build mode bar: show all modes, highlight the active one
	modes := []struct {
		mode Mode
		name string
	}{
		{FilesMode, "files"},
		{CommitsMode, "commits"},
		{PRMode, "pr"},
		{HelpMode, "help"},
	}

	var modeItems []string
	var labels []modeLabel
	// Account for statusBarStyle padding (1 char) + leading space in parts[0]
	pos := 2 // 1 for Padding(0,1) + 1 for " " prefix
	hoverMode := Mode(-1)

	for _, m := range modes {
		// Skip pr mode if no PR
		if m.mode == PRMode && data.pr.Number == 0 {
			continue
		}

		displayText := m.name
		// Oracle, not len(): this is a hit region in display columns, and a
		// byte count is only right by accident for ASCII mode names. Also
		// avoids shadowing displayWidth in a scope that computes geometry.
		textW := displayWidth(displayText)

		label := modeLabel{mode: m.mode, start: pos, end: pos + textW}
		labels = append(labels, label)

		// Check if hover is on this label
		isHovered := data.hoverY == 0 && data.hoverX >= label.start && data.hoverX < label.end

		// Help mode is "active" when help overlay is shown
		isActive := m.mode == data.mode || (m.mode == HelpMode && data.showHelp)
		if !isActive && isHovered {
			hoverMode = m.mode
		}
		// Style mode text using inline ANSI attributes (bold, underline,
		// foreground) instead of lipgloss Render, which emits a full \e[0m
		// reset that kills the outer statusBarStyle background between items.
		modeItems = append(modeItems, styleModeInline(displayText, isActive, isHovered))

		pos += textW + 1 // +1 for space separator
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
	bar = ellipsize(bar, width-2)
	return statusBarStyle.Width(width).Render(bar), labels
}

// renderLine2: local git status — branch, uncommitted, unpushed, commits
func renderLine2(width int, data statusBarData) (string, []line2Label) {
	info := data.info

	// Branch and merge base
	var branchDisplay string
	if info.IsDetachedHead {
		branchDisplay = fmt.Sprintf("detached @ %s", info.HeadSHA)
	} else {
		branchDisplay = info.Branch
	}

	// Show "branch -> base" if not on main/master. The base comes from the
	// same chain the behind-count is measured against.
	if info.Branch != "main" && info.Branch != "master" && !info.IsDetachedHead {
		base := baseBranchName(data.pr.BaseRef, info.Branch, info.Upstream)
		if base != info.Branch {
			branchDisplay = info.Branch + " → " + base
		}
	}

	type part struct {
		text   string
		target line2Target
	}
	var parts []part
	var labels []line2Label

	// When the scope handle is scrubbed, prefix the line so the user can
	// always see where the outer endpoint sits, regardless of mode.
	if data.scopeHandle != nil {
		parts = append(parts, part{
			text:   fmt.Sprintf(" @%s HEAD~%d", data.scopeHandle.sha7, data.scopeHandle.headOffset),
			target: line2CommitsMode,
		})
	}

	parts = append(parts, part{" " + branchDisplay, line2CommitsMode})

	if data.uncommitCount > 0 {
		parts = append(parts, part{fmt.Sprintf("%d uncommitted", data.uncommitCount), line2FilesMode})
	}
	if info.AheadCount > 0 {
		parts = append(parts, part{fmt.Sprintf("%d unpushed", info.AheadCount), line2CommitsMode})
	}
	if data.commitCount > 0 {
		parts = append(parts, part{fmt.Sprintf("%d commits", data.commitCount), line2CommitsMode})
	}
	if data.changedFileCount > 0 {
		parts = append(parts, part{fmt.Sprintf("%d changed files", data.changedFileCount), line2FilesMode})
	}
	if data.behindKnown && data.behindCount > 0 {
		parts = append(parts, part{fmt.Sprintf("%d behind", data.behindCount), line2CommitsMode})
	}
	if data.pr.Number == 0 && data.prError == "" {
		parts = append(parts, part{"No PR", line2CommitsMode})
	}

	// Build bar and track positions (statusBarPRStyle has Padding(0,1), pos starts at 1)
	pos := 1
	var textParts []string
	for i, p := range parts {
		textW := displayWidth(p.text)
		labels = append(labels, line2Label{target: p.target, start: pos, end: pos + textW})
		textParts = append(textParts, p.text)
		pos += textW
		if i < len(parts)-1 {
			pos += 3 // " · " separator
		}
	}

	bar := strings.Join(textParts, " · ")
	bar = ellipsize(bar, width-2)
	return statusBarPRStyle.Width(width).Render(bar), labels
}

// renderLine3: github status — PR, draft, reviews, comments, CI
func renderLine3(width int, data statusBarData) (string, []line3Label) {
	type part struct {
		text   string
		target line3Target
	}

	var parts []part
	var labels []line3Label

	// Draft/Merged status first (bright, bold, obvious)
	if data.pr.IsDraft {
		parts = append(parts, part{" \x1b[1;38;2;249;226;175m[DRAFT]\x1b[0;38;2;205;214;244;48;2;69;71;90m", line3Description})
	}
	if data.pr.State == "MERGED" {
		parts = append(parts, part{" \x1b[1;38;2;166;227;161m[MERGED]\x1b[0;38;2;205;214;244;48;2;69;71;90m", line3Description})
	}

	// PR link
	prLink := fmt.Sprintf("PR #%d: %s", data.pr.Number, data.pr.Title)
	if data.pr.URL != "" {
		prLink = makeHyperlink(data.pr.URL, prLink)
	}
	prefix := " "
	if len(parts) > 0 {
		prefix = ""
	}
	parts = append(parts, part{prefix + prLink, line3Description})

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
		textW := displayWidth(p.text)
		labels = append(labels, line3Label{target: p.target, start: pos, end: pos + textW})
		textParts = append(textParts, p.text)
		pos += textW
		if i < len(parts)-1 {
			pos += 3 // " · " separator
		}
	}

	bar := strings.Join(textParts, " · ")
	// Truncate if too wide for the content area (width - 2 padding)
	bar = ellipsize(bar, width-2)
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

// ellipsize truncates a status-bar string to the given width, appending "…" if
// truncation occurs.
//
// It fits the RENDERER's measurement, not the oracle's: every string it returns
// is handed to `style.Width(n).Render(...)`, and lipgloss is what decides to
// wrap. Trimming only under the oracle left divergence-class input measuring
// wider to lipgloss, which then hard-wrapped the bar onto an extra row and
// desynchronised statusBarLineCount from renderStatusBar. See
// fitToRendererWidth (width.go) and TestStatusBarRowCountMatchesLayout.
func ellipsize(s string, maxWidth int) string {
	return fitToRendererWidth(s, maxWidth, "…")
}

// makeHyperlink creates an OSC 8 terminal hyperlink.
func makeHyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}
