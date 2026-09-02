package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
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

// statusBarRowLayout maps the status bar's logical lines onto terminal row
// indices, with -1 for a line that is not rendered.
//
// Line 3's row index is NOT a constant: line 2 only exists for a git repo, so
// with no repo the GitHub line slides up to row 1. Render, hit-testing, hover
// and layout all read this one value, so a line that isn't drawn can never be
// clicked or hovered at some other line's row. See CLAUDE.md, "Layout
// geometry comes from one function".
type statusBarRowLayout struct {
	line1 int
	line2 int
	line3 int
	rows  int
}

// statusBarRows is the sole authority for the status bar's row geometry. Every
// branch here must mirror a branch in renderStatusBar — the two are the layout
// and render halves of the same geometry.
func statusBarRows(data statusBarData) statusBarRowLayout {
	if data.confirming {
		// renderStatusBar replaces the whole bar with the single-line
		// quit prompt, which carries no clickable labels.
		return statusBarRowLayout{line1: -1, line2: -1, line3: -1, rows: 1}
	}
	l := statusBarRowLayout{line1: 0, line2: -1, line3: -1, rows: 1}
	if data.info.RepoName != "" || data.info.Branch != "" {
		l.line2 = l.rows // line 2: git status
		l.rows++
	}
	if data.pr.Number > 0 || data.prError != "" || data.prLoading {
		l.line3 = l.rows // line 3: PR status, error, or loading
		l.rows++
	}
	return l
}

// statusBarLineCount returns how many lines the status bar will occupy.
func statusBarLineCount(data statusBarData) int {
	return statusBarRows(data).rows
}

func renderStatusBar(width int, data statusBarData) (string, []modeLabel, []line2Label, []line3Label) {
	rows := statusBarRows(data)

	if data.confirming {
		msg := " Quit? Press q/Q to confirm, any other key to cancel"
		// Truncate to the content area, exactly as lines 1-3 do, and let
		// statusBarConfirmStyle.Width pad the rest. Unbounded, a terminal
		// narrower than the prompt makes lipgloss hard-wrap it onto rows
		// statusBarRows never promised, and every click below the bar lands
		// that many rows off — the row-count desync class in CLAUDE.md,
		// "Layout geometry comes from one function". It survived this long
		// only because at ordinary widths the sole overflow was the padding
		// lipgloss trims anyway.
		msg = ellipsize(msg, width-2*statusBarPadding)
		return statusBarConfirmStyle.Width(width).Render(msg), nil, nil, nil
	}

	line1, labels := renderLine1(width, data, rows.line1)
	result := line1

	// Line 2: only show for git repos
	var line2Labels []line2Label
	if rows.line2 >= 0 {
		l2, l2Labels := renderLine2(width, data, rows.line2)
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
	// increment in statusBarRows.
	var line3Labels []line3Label
	if rows.line3 >= 0 {
		switch {
		case data.prError != "":
			// Sanitized like any other display text: an error string is not
			// under our control (it can carry gh's stderr), and a raw newline
			// would split this "row" in two and desync the layout.
			errText := " " + sanitizeDisplayText(data.prError)
			errText = ellipsize(errText, width-2*statusBarPadding)
			result += "\n" + statusBarDimStyle.Width(width).Render(errText)
		case data.pr.Number > 0:
			l3, l3Labels := renderLine3(width, data, rows.line3)
			line3Labels = l3Labels
			result += "\n" + l3
		case data.prLoading:
			// Show loading indicator while PR data is being fetched
			loadText := " Loading from GitHub…"
			loadText = ellipsize(loadText, width-2*statusBarPadding)
			result += "\n" + statusBarDimStyle.Width(width).Render(loadText)
		}
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

// underlineInline brackets a status-bar label in underline-on/underline-off
// without emitting a full reset (\e[0m), which would clear the outer row
// background — the same reason styleModeInline exists.
//
// Escape sequences occupy no display cells (width.go), so bracketing a label
// changes neither the row's measured width nor where any label starts. Labels
// that carry their own SGR are safe: line 3's [DRAFT]/[MERGED] markers end in
// their own `0`-reset, which lands after the marker's last visible cell, and
// ansiUlOff is harmless when the attribute is already clear. Hyperlinks are
// OSC 8 and touch no SGR attribute at all.
func underlineInline(text string) string {
	return ansiUlOn + text + ansiUlOff
}

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

// ---------------------------------------------------------------------------
// Status-bar column geometry
//
// Every clickable label's column range on every status-bar row comes from the
// functions below, and so does the hover test. PROMPT.md ("mouse behavior")
// requires that "hover regions and click regions are the same regions", and
// that "a label whose click target was truncated away is not hoverable
// either" — both hold because there is one region set per row, clipped once.
// ---------------------------------------------------------------------------

const (
	// statusBarSep joins the labels on lines 2 and 3.
	statusBarSep = " · "
	// statusBarEllipsis is the tail ellipsize appends when it truncates.
	statusBarEllipsis = "…"
	// statusBarPadding is the left inset every status-bar style applies
	// (Padding(0, 1)), i.e. the column at which row content starts.
	statusBarPadding = 1
)

// statusBarSpan is the half-open column range a clickable status-bar label
// occupies in its rendered row. start >= end means the label is not on
// screen, and is therefore neither clickable nor hoverable.
type statusBarSpan struct {
	start int
	end   int
}

// statusBarSpans returns the column range each part occupies in a row built by
// joining parts with sep and rendering it from column startCol.
//
// Positions come from measuring whole prefixes rather than accumulating part
// widths: display width is not additive across concatenation, since a
// combining mark at the head of one part merges into the tail of the previous
// one. See PROMPT.md, "unicode width accounting", and width.go.
func statusBarSpans(parts []string, sep string, startCol int) []statusBarSpan {
	spans := make([]statusBarSpan, len(parts))
	for i := range parts {
		start := startCol
		if i > 0 {
			start += displayWidth(strings.Join(parts[:i], sep) + sep)
		}
		spans[i] = statusBarSpan{
			start: start,
			end:   startCol + displayWidth(strings.Join(parts[:i+1], sep)),
		}
	}
	return spans
}

// clipStatusBarSpans drops the part of each span at or past limit — the first
// column the row's truncation removed. A span entirely past limit collapses to
// empty, which every consumer reads as "not on screen".
func clipStatusBarSpans(spans []statusBarSpan, limit int) []statusBarSpan {
	out := make([]statusBarSpan, len(spans))
	for i, sp := range spans {
		sp.start = min(sp.start, limit)
		sp.end = min(sp.end, limit)
		sp.end = max(sp.end, sp.start)
		out[i] = sp
	}
	return out
}

// hoveredStatusBarSpan returns the index of the span under (hoverX, hoverY) on
// terminal row `row`, or -1 when the cursor is on another row, on a separator,
// or past the row's content. Empty spans never match, so a label truncated
// away is not hoverable.
func hoveredStatusBarSpan(spans []statusBarSpan, row, hoverX, hoverY int) int {
	if row < 0 || hoverY != row {
		return -1
	}
	for i, sp := range spans {
		if sp.start < sp.end && hoverX >= sp.start && hoverX < sp.end {
			return i
		}
	}
	return -1
}

// fitStatusBarRow truncates a row's content to contentWidth and reports how
// many display columns of it survived, the ellipsis excluded.
//
// Geometry derived from the untruncated content must be clipped to `retained`
// or it points at content that is no longer on screen — the phantom
// click-target bug PROMPT.md's "a label whose click target was truncated away
// is not hoverable either" rules out.
func fitStatusBarRow(content string, contentWidth int) (shown string, retained int) {
	shown = ellipsize(content, contentWidth)
	if shown == content {
		return shown, displayWidth(content)
	}
	// ellipsize appends the ellipsis exactly when it dropped content, and the
	// escape sequences it deliberately preserves past the cut occupy no cells,
	// so what survived is the rendered width less the ellipsis.
	return shown, max(0, displayWidth(shown)-displayWidth(statusBarEllipsis))
}

// resolveStatusBarHover renders a status-bar row to a fixed point at which the
// underlined label is exactly the one the row's own surviving click regions
// put under the cursor.
//
// Truncation and hover are mutually dependent: which label the cursor is over
// depends on which labels survive truncation, and the underline escapes ride
// along in the very string that gets truncated. Escape sequences occupy no
// display cells, so the styled row all but always truncates at the same column
// as the unstyled one and the second pass agrees with the first. The loop is
// for the divergence class width.go documents — an escape landing inside a
// grapheme cluster measures wider to lipgloss than to the oracle — and settles
// on "nothing hovered", whose geometry is self-consistent, if the passes never
// agree.
//
// build(hovered) returns the row's content with label index `hovered`
// underlined (-1 for none); spansFor(retained) returns the click regions that
// survive a row with `retained` columns of content; hoverAt reports which of
// those regions holds the cursor.
func resolveStatusBarHover(
	contentWidth int,
	build func(hovered int) string,
	spansFor func(retained int) []statusBarSpan,
	hoverAt func([]statusBarSpan) int,
) (string, []statusBarSpan) {
	hovered := -1
	for range 4 {
		shown, retained := fitStatusBarRow(build(hovered), contentWidth)
		spans := spansFor(retained)
		h := hoverAt(spans)
		if h == hovered {
			return shown, spans
		}
		hovered = h
	}
	shown, retained := fitStatusBarRow(build(-1), contentWidth)
	return shown, spansFor(retained)
}

// renderStatusBarRow is the shared body of lines 2 and 3: it lays out the
// row's clickable parts, clips their hit regions to what survives truncation,
// underlines the part under the cursor, and renders the row. The returned
// spans are one per part, in order, and empty for a part that was truncated
// away.
func renderStatusBarRow(style lipgloss.Style, width int, parts []string, row, hoverX, hoverY int) (string, []statusBarSpan) {
	// Both padding columns come out of the content budget.
	contentWidth := width - 2*statusBarPadding
	raw := statusBarSpans(parts, statusBarSep, statusBarPadding)

	bar, spans := resolveStatusBarHover(contentWidth,
		func(hovered int) string {
			if hovered < 0 {
				return strings.Join(parts, statusBarSep)
			}
			styled := slices.Clone(parts)
			styled[hovered] = underlineInline(styled[hovered])
			return strings.Join(styled, statusBarSep)
		},
		func(retained int) []statusBarSpan {
			return clipStatusBarSpans(raw, statusBarPadding+retained)
		},
		func(spans []statusBarSpan) int {
			return hoveredStatusBarSpan(spans, row, hoverX, hoverY)
		},
	)
	return style.Width(width).Render(bar), spans
}

// renderLine1: overall status — mode, directory, worktree
// Returns the rendered line and the clickable mode label positions.
func renderLine1(width int, data statusBarData, row int) (string, []modeLabel) {
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

	var names []string
	var shownModes []Mode
	for _, m := range modes {
		// Skip pr mode if no PR
		if m.mode == PRMode && data.pr.Number == 0 {
			continue
		}
		names = append(names, m.name)
		shownModes = append(shownModes, m.mode)
	}

	dirName := data.info.DirName
	if dirName == "" {
		dirName = data.info.RepoName
	}

	// buildBar assembles the whole row around an already-rendered mode string.
	// Only the mode string varies between hover passes, and its width never
	// does: the styling is escape sequences, which occupy no cells.
	buildBar := func(modeStr string) string {
		parts := []string{" " + modeStr}
		if dirName != "" {
			parts = append(parts, dirName)
		}
		if data.info.Worktree != "" && data.info.RepoName != "" && data.info.DirName != data.info.RepoName {
			parts = append(parts, "in "+data.info.RepoName)
		}
		if data.info.RepoName == "" && data.info.Branch == "" {
			parts = append(parts, "Not a git repo")
		}
		return strings.Join(parts, statusBarSep)
	}

	// The mode labels sit inside the row's first part, one column past the
	// style padding because that part opens with a space.
	raw := statusBarSpans(names, " ", statusBarPadding+1)

	bar, spans := resolveStatusBarHover(width-2*statusBarPadding,
		func(hovered int) string {
			items := make([]string, len(names))
			for i, name := range names {
				// Help mode is "active" when the help overlay is shown.
				isActive := shownModes[i] == data.mode || (shownModes[i] == HelpMode && data.showHelp)
				// Style mode text using inline ANSI attributes (bold,
				// underline, foreground) instead of lipgloss Render, which
				// emits a full \e[0m reset that kills the outer
				// statusBarStyle background between items.
				items[i] = styleModeInline(name, isActive, i == hovered)
			}
			return buildBar(strings.Join(items, " "))
		},
		func(retained int) []statusBarSpan {
			return clipStatusBarSpans(raw, statusBarPadding+retained)
		},
		func(spans []statusBarSpan) int {
			return hoveredStatusBarSpan(spans, row, data.hoverX, data.hoverY)
		},
	)

	var labels []modeLabel
	for i, sp := range spans {
		if sp.start >= sp.end {
			continue // truncated away: neither clickable nor hoverable
		}
		labels = append(labels, modeLabel{mode: shownModes[i], start: sp.start, end: sp.end})
	}
	return statusBarStyle.Width(width).Render(bar), labels
}

// renderLine2: local git status — branch, uncommitted, unpushed, commits
func renderLine2(width int, data statusBarData, row int) (string, []line2Label) {
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

	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.text
	}
	bar, spans := renderStatusBarRow(statusBarPRStyle, width, texts, row, data.hoverX, data.hoverY)

	var labels []line2Label
	for i, sp := range spans {
		if sp.start >= sp.end {
			continue // truncated away: neither clickable nor hoverable
		}
		labels = append(labels, line2Label{target: parts[i].target, start: sp.start, end: sp.end})
	}
	return bar, labels
}

// renderLine3: github status — PR, draft, reviews, comments, CI
func renderLine3(width int, data statusBarData, row int) (string, []line3Label) {
	type part struct {
		text   string
		target line3Target
	}

	var parts []part

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

	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.text
	}
	bar, spans := renderStatusBarRow(statusBarDimStyle, width, texts, row, data.hoverX, data.hoverY)

	var labels []line3Label
	for i, sp := range spans {
		if sp.start >= sp.end {
			continue // truncated away: neither clickable nor hoverable
		}
		labels = append(labels, line3Label{target: parts[i].target, start: sp.start, end: sp.end})
	}
	return bar, labels
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
	return fitToRendererWidth(s, maxWidth, statusBarEllipsis)
}

// makeHyperlink creates an OSC 8 terminal hyperlink.
func makeHyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}
