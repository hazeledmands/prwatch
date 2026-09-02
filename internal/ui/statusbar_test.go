package ui

import (
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
)

// TestRenderStatusBar_ScopeHandleIndicator verifies that when scopeHandle
// is set, line 2 prefixes the indicator (`@<sha7> HEAD~N`); when nil, no
// such prefix appears.
func TestRenderStatusBar_ScopeHandleIndicator(t *testing.T) {
	base := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "p", DirName: "p", Upstream: "origin/main"},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, base)
	if strings.Contains(bar, "@a3f7d21") || strings.Contains(bar, "HEAD~") {
		t.Error("at default scope, status bar should NOT contain handle indicator")
	}

	scrubbed := base
	scrubbed.scopeHandle = &scopeHandleInfo{sha7: "a3f7d21", headOffset: 7}
	bar, _, _, _ = renderStatusBar(120, scrubbed)
	if !strings.Contains(bar, "@a3f7d21") {
		t.Errorf("scrubbed: status bar should contain @<sha7>; got: %q", bar)
	}
	if !strings.Contains(bar, "HEAD~7") {
		t.Errorf("scrubbed: status bar should contain HEAD~N; got: %q", bar)
	}
}

func TestRenderStatusBar_Basic(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "main",
			RepoName: "prwatch",
			DirName:  "prwatch",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(80, data)

	if !strings.Contains(bar, "main") {
		t.Error("status bar should contain branch name")
	}
	if !strings.Contains(bar, "prwatch") {
		t.Error("status bar should contain dir/repo name")
	}
	if !strings.Contains(bar, "files") {
		t.Error("status bar should show files mode indicator")
	}
}

func TestRenderStatusBar_FilesMode(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "test", DirName: "test"},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "files") {
		t.Error("status bar should show files mode indicator")
	}
}

func TestRenderStatusBar_CommitsMode(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "test", DirName: "test"},
		mode: CommitsMode,
	}
	bar, _, _, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "commits") {
		t.Error("status bar should show commit mode indicator")
	}
}

func TestRenderStatusBar_Confirming(t *testing.T) {
	data := statusBarData{
		info:       git.RepoInfoResult{Branch: "main"},
		mode:       FilesMode,
		confirming: true,
	}
	bar, _, _, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "Quit?") {
		t.Error("confirming status bar should show quit prompt")
	}
}

func TestRenderStatusBar_WithPR(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr: git.PRInfoResult{
			Number: 42,
			Title:  "My PR",
			URL:    "https://github.com/org/repo/pull/42",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "PR #42") {
		t.Error("should show PR number")
	}
	if !strings.Contains(bar, "My PR") {
		t.Error("should show PR title")
	}
}

func TestRenderStatusBar_DetachedHead(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:         "HEAD",
			IsDetachedHead: true,
			HeadSHA:        "abc1234",
			DirName:        "repo",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "detached @ abc1234") {
		t.Error("should show detached HEAD with SHA")
	}
}

func TestRenderStatusBar_Worktree(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "feature",
			RepoName: "repo",
			DirName:  "worktree-dir",
			Worktree: "/some/path",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "in repo") {
		t.Error("should indicate parent repo name for worktree")
	}
}

func TestRenderStatusBar_NoPR(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "repo", DirName: "repo"},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(200, data)
	if !strings.Contains(bar, "No PR") {
		t.Errorf("should show 'No PR', got: %q", bar)
	}
}

func TestRenderStatusBar_NarrowWidth(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "hazel/very-long-feature-branch-name",
			RepoName: "my-really-long-repository-name",
			DirName:  "my-really-long-repository-name",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(20, data)
	if bar == "" {
		t.Error("should still render even when narrow")
	}
}

func TestRenderStatusBar_ConfirmNarrow(t *testing.T) {
	data := statusBarData{confirming: true}
	bar, _, _, _ := renderStatusBar(10, data)
	if !strings.Contains(bar, "Quit?") {
		t.Error("confirming bar should show quit prompt even when narrow")
	}
}

func TestRenderStatusBar_WithUpstream(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "feature",
			Upstream: "origin/main",
			RepoName: "repo",
			DirName:  "repo",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	// Should show "feature → main"
	if !strings.Contains(bar, "feature") {
		t.Error("should show branch name")
	}
	if !strings.Contains(bar, "→") {
		t.Error("should show arrow to base")
	}
}

func TestRenderStatusBar_AheadCount(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:     "feature",
			RepoName:   "repo",
			DirName:    "repo",
			AheadCount: 3,
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "3 unpushed") {
		t.Error("should show unpushed count")
	}
}

func TestRenderStatusBar_GitStatusSummary(t *testing.T) {
	data := statusBarData{
		info:          git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		mode:          FilesMode,
		uncommitCount: 2,
		commitCount:   5,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "2 uncommitted") {
		t.Error("should show uncommitted count")
	}
	if !strings.Contains(bar, "5 commits") {
		t.Error("should show commit count")
	}
}

func TestRenderStatusBar_DirName(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "feature",
			RepoName: "repo",
			DirName:  "worktree-dir",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "worktree-dir") {
		t.Error("should show dir name")
	}
}

func TestRenderStatusBar_DirNameSameAsRepo(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "feature",
			RepoName: "repo",
			DirName:  "repo",
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	// Dir name should still appear as the directory identifier
	if !strings.Contains(bar, "repo") {
		t.Error("should show dir/repo name")
	}
}

func TestRenderStatusBar_PRWithDraft(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr: git.PRInfoResult{
			Number:  1,
			Title:   "WIP",
			IsDraft: true,
		},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "[DRAFT]") {
		t.Error("should show [DRAFT] indicator")
	}
}

func TestRenderCIStatus(t *testing.T) {
	tests := []struct {
		name     string
		ci       git.CIStatusResult
		contains string
	}{
		{"success", git.CIStatusResult{State: "SUCCESS"}, "CI ✓"},
		{"failure", git.CIStatusResult{State: "FAILURE"}, "CI ✗"},
		{"pending", git.CIStatusResult{State: "PENDING"}, "CI ⟳"},
		{"empty", git.CIStatusResult{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderCIStatus(tt.ci)
			if tt.contains == "" {
				if result != "" {
					t.Errorf("expected empty, got %q", result)
				}
			} else if !strings.Contains(result, tt.contains) {
				t.Errorf("expected %q in %q", tt.contains, result)
			}
		})
	}
}

func TestRenderCIStatus_WithURL(t *testing.T) {
	states := []string{"SUCCESS", "FAILURE", "PENDING"}
	for _, state := range states {
		ci := git.CIStatusResult{State: state, URL: "https://ci.example.com"}
		result := renderCIStatus(ci)
		if !strings.Contains(result, "\033]8;;") {
			t.Errorf("state %s: should contain hyperlink escape", state)
		}
	}
}

func TestRenderCIStatusEmoji(t *testing.T) {
	tests := []struct {
		state    string
		contains string
	}{
		{"SUCCESS", "✅"},
		{"FAILURE", "❌"},
		{"PENDING", "⏳"},
		{"", ""},
	}
	for _, tt := range tests {
		result := renderCIStatusEmoji(git.CIStatusResult{State: tt.state})
		if tt.contains == "" {
			if result != "" {
				t.Errorf("state %q: expected empty, got %q", tt.state, result)
			}
		} else if !strings.Contains(result, tt.contains) {
			t.Errorf("state %q: expected %q in %q", tt.state, tt.contains, result)
		}
	}
}

func TestRenderReviews(t *testing.T) {
	reviews := []git.PRReview{
		{Author: "alice", State: "APPROVED"},
		{Author: "bob", State: "CHANGES_REQUESTED"},
		{Author: "charlie", State: "COMMENTED"},
	}
	result := renderReviews(reviews, nil, "")
	if !strings.Contains(result, "1✓") {
		t.Error("should show approved count")
	}
	if !strings.Contains(result, "1✗") {
		t.Error("should show rejected count")
	}
	if !strings.Contains(result, "1 pending") {
		t.Error("should show pending count")
	}
}

func TestRenderReviews_WithRequests(t *testing.T) {
	requests := []git.PRReviewRequest{
		{Name: "alice", IsTeam: false},
		{Name: "Storage Reviewers", IsTeam: true},
	}
	result := renderReviews(nil, requests, "")
	if !strings.Contains(result, "2👀") {
		t.Errorf("should show review request count, got %q", result)
	}
}

func TestRenderReviews_MixedReviewsAndRequests(t *testing.T) {
	reviews := []git.PRReview{
		{Author: "alice", State: "APPROVED"},
	}
	requests := []git.PRReviewRequest{
		{Name: "bob", IsTeam: false},
	}
	result := renderReviews(reviews, requests, "")
	if !strings.Contains(result, "1✓") {
		t.Error("should show approved count")
	}
	if !strings.Contains(result, "1👀") {
		t.Error("should show review request count")
	}
}

func TestRenderReviews_Empty(t *testing.T) {
	result := renderReviews(nil, nil, "")
	if result != "" {
		t.Errorf("empty reviews should return empty, got %q", result)
	}
}

func TestRenderReviews_UnknownDecision(t *testing.T) {
	result := renderReviews(nil, nil, "UNKNOWN_STATE")
	if result != "" {
		t.Errorf("unknown decision should return empty, got %q", result)
	}
}

func TestRenderReviews_DecisionOnly(t *testing.T) {
	tests := []struct {
		decision string
		expected string
	}{
		{"APPROVED", "approved"},
		{"CHANGES_REQUESTED", "changes requested"},
		{"REVIEW_REQUIRED", "review required"},
		{"", ""},
	}
	for _, tt := range tests {
		result := renderReviews(nil, nil, tt.decision)
		if result != tt.expected {
			t.Errorf("decision %q: got %q, want %q", tt.decision, result, tt.expected)
		}
	}
}

func TestRenderStatusBar_WithComments(t *testing.T) {
	data := statusBarData{
		info:         git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr:           git.PRInfoResult{Number: 1, Title: "test"},
		mode:         FilesMode,
		commentCount: 5,
	}
	bar, _, _, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "5 comments") {
		t.Error("should show comment count")
	}
}

func TestRenderStatusBar_FullPRDetails(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr: git.PRInfoResult{
			Number:         42,
			Title:          "My PR",
			URL:            "https://github.com/org/repo/pull/42",
			IsDraft:        true,
			ReviewDecision: "CHANGES_REQUESTED",
		},
		ciStatus:     git.CIStatusResult{State: "FAILURE", URL: "https://ci.example.com"},
		reviews:      []git.PRReview{{Author: "alice", State: "APPROVED"}, {Author: "bob", State: "CHANGES_REQUESTED"}},
		commentCount: 7,
		mode:         FilesMode,
	}
	bar, _, _, _ := renderStatusBar(200, data)
	if !strings.Contains(bar, "[DRAFT]") {
		t.Error("should show draft")
	}
	if !strings.Contains(bar, "❌") {
		t.Error("should show CI status emoji")
	}
	if !strings.Contains(bar, "7 comments") {
		t.Error("should show comments")
	}
}

func TestRenderLine3_PRWithNoURL(t *testing.T) {
	data := statusBarData{
		pr: git.PRInfoResult{Number: 1, Title: "no url"},
	}
	result, _ := renderLine3(80, data, statusBarRows(data).line3)
	if !strings.Contains(result, "PR #1") {
		t.Error("should show PR without URL")
	}
}

func TestRenderStatusBar_ThreeLines(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr:   git.PRInfoResult{Number: 1, Title: "test"},
		mode: FilesMode,
	}
	bar, _, _, _ := renderStatusBar(80, data)
	stripped := stripANSIForWidth(bar)
	lines := strings.Split(stripped, "\n")
	if len(lines) != 3 {
		t.Errorf("status bar should be 3 lines, got %d", len(lines))
	}
}

func TestMakeHyperlink(t *testing.T) {
	link := makeHyperlink("https://example.com", "click me")
	if !strings.Contains(link, "\033]8;;https://example.com\033\\") {
		t.Error("should contain OSC 8 open sequence")
	}
	if !strings.Contains(link, "click me") {
		t.Error("should contain link text")
	}
	if !strings.HasSuffix(link, "\033]8;;\033\\") {
		t.Error("should end with OSC 8 close sequence")
	}
}

// TestRenderLine3_ActiveErrorWithPRData is the regression test for
// INCONSISTENCIES.md "GitHub API errors hidden once PR data exists":
// PROMPT.md:83 says line 3 carries the GitHub API error message, but
// renderStatusBar only reached the error branch when no PR had been fetched
// yet. An active error must be surfaced on line 3 even with PR data on hand
// (the PR data itself stays rendered elsewhere), and the error text goes
// through the display-text sanitizer like any other display string.
func TestRenderLine3_ActiveErrorWithPRData(t *testing.T) {
	base := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "p", DirName: "p"},
		pr:   git.PRInfoResult{Number: 7, Title: "a pr"},
		mode: FilesMode,
	}

	t.Run("error replaces line 3 content", func(t *testing.T) {
		data := base
		data.prError = "GitHub API rate limited"
		bar, _, _, line3Labels := renderStatusBar(120, data)
		lines := strings.Split(bar, "\n")
		if len(lines) != 3 {
			t.Fatalf("status bar has %d rows, want 3: %q", len(lines), bar)
		}
		if !strings.Contains(lines[2], "GitHub API rate limited") {
			t.Errorf("line 3 = %q, want the active error message", lines[2])
		}
		if strings.Contains(lines[2], "PR #7") {
			t.Errorf("line 3 = %q, want the error to replace the PR summary", lines[2])
		}
		if len(line3Labels) != 0 {
			t.Errorf("line 3 labels = %v, want none while the error occupies the line", line3Labels)
		}
	})

	t.Run("no error keeps the PR summary", func(t *testing.T) {
		bar, _, _, _ := renderStatusBar(120, base)
		lines := strings.Split(bar, "\n")
		if len(lines) != 3 || !strings.Contains(lines[2], "PR #7") {
			t.Errorf("line 3 = %q, want the PR summary when no error is active", bar)
		}
	})

	t.Run("error text is sanitized", func(t *testing.T) {
		data := base
		data.prError = "GitHub API error\nsecond row\tand a tab"
		bar, _, _, _ := renderStatusBar(120, data)
		if got := len(strings.Split(bar, "\n")); got != 3 {
			t.Fatalf("status bar has %d rows, want 3 — control characters must not add rows: %q", got, bar)
		}
		if !strings.Contains(bar, `\n`) || !strings.Contains(bar, `\t`) {
			t.Errorf("status bar = %q, want the control characters rendered as escapes", bar)
		}
	})
}

// ---------------------------------------------------------------------------
// Status-bar hover
//
// PROMPT.md, "mouse behavior": "every clickable element has a hover state …
// **status bar** — an underline on the hovered label. this covers the line-1
// mode labels and every clickable label on lines 2 and 3 … a label whose
// click target was truncated away is not hoverable either — hover regions and
// click regions are the same regions."
// ---------------------------------------------------------------------------

// underlinedRuns returns the visible text of each underlined run in a rendered
// status-bar row, in order. Underline is applied with the bare SGR pair
// (ansiUlOn/ansiUlOff) rather than lipgloss.Render — see the comment above
// ansiWhiteFg — so the runs are exactly the text bracketed by those two
// sequences.
func underlinedRuns(row string) []string {
	var out []string
	rest := row
	for {
		i := strings.Index(rest, ansiUlOn)
		if i < 0 {
			return out
		}
		rest = rest[i+len(ansiUlOn):]
		j := strings.Index(rest, ansiUlOff)
		if j < 0 {
			// Unterminated underline: report the remainder so a leaked
			// attribute shows up as a failure rather than vanishing.
			out = append(out, stripANSI(rest))
			return out
		}
		out = append(out, stripANSI(rest[:j]))
		rest = rest[j+len(ansiUlOff):]
	}
}

// underlinedRun returns the single underlined run on a row, "" for none, and
// fails the test if there is more than one — at most one label is hovered.
func underlinedRun(t *testing.T, row string) string {
	t.Helper()
	runs := underlinedRuns(row)
	switch len(runs) {
	case 0:
		return ""
	case 1:
		return runs[0]
	default:
		t.Fatalf("row carries %d underlined runs %q, want at most 1", len(runs), runs)
		return ""
	}
}

func TestStatusBarRows_LineToRowMapping(t *testing.T) {
	gitRepo := git.RepoInfoResult{Branch: "main", RepoName: "repo", DirName: "repo"}
	tests := []struct {
		name                     string
		data                     statusBarData
		line1, line2, line3, cnt int
	}{
		{"not a git repo, no PR", statusBarData{}, 0, -1, -1, 1},
		{"git repo, no PR", statusBarData{info: gitRepo}, 0, 1, -1, 2},
		{"git repo with PR", statusBarData{info: gitRepo, pr: git.PRInfoResult{Number: 1}}, 0, 1, 2, 3},
		{"git repo, loading", statusBarData{info: gitRepo, prLoading: true}, 0, 1, 2, 3},
		{"git repo, error", statusBarData{info: gitRepo, prError: "boom"}, 0, 1, 2, 3},
		// No line 2 at all: line 3 slides up to row 1. Hover and click
		// targeting must follow it there.
		{"no git repo but a PR", statusBarData{pr: git.PRInfoResult{Number: 1}}, 0, -1, 1, 2},
		{"no git repo, loading", statusBarData{prLoading: true}, 0, -1, 1, 2},
		{"confirming replaces the bar", statusBarData{info: gitRepo, confirming: true}, -1, -1, -1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := statusBarRows(tc.data)
			want := statusBarRowLayout{line1: tc.line1, line2: tc.line2, line3: tc.line3, rows: tc.cnt}
			if got != want {
				t.Errorf("statusBarRows = %+v, want %+v", got, want)
			}
			if n := statusBarLineCount(tc.data); n != tc.cnt {
				t.Errorf("statusBarLineCount = %d, want %d", n, tc.cnt)
			}
			bar, _, _, _ := renderStatusBar(120, tc.data)
			if n := len(strings.Split(bar, "\n")); n != tc.cnt {
				t.Errorf("renderStatusBar produced %d rows, want %d: %q", n, tc.cnt, bar)
			}
		})
	}
}

// line2HoverData is the fixture for the line-2 hover table. Its parts are all
// ASCII, so the column arithmetic in the table is exact:
//
//	col: 1         6  9             22 25        34 37
//	     " main" + " · " + "3 uncommitted" + " · " + "5 commits" + " · " + "No PR"
func line2HoverData() statusBarData {
	return statusBarData{
		info:          git.RepoInfoResult{Branch: "main", RepoName: "repo", DirName: "repo"},
		mode:          FilesMode,
		uncommitCount: 3,
		commitCount:   5,
	}
}

func TestStatusBarHover_Line2(t *testing.T) {
	tests := []struct {
		name   string
		hoverX int
		hoverY int
		want   string
	}{
		{"first column of branch label", 1, 1, " main"},
		{"last column of branch label", 5, 1, " main"},
		{"padding column left of the bar", 0, 1, ""},
		{"separator after branch", 6, 1, ""},
		{"separator column before uncommitted", 8, 1, ""},
		{"start of uncommitted", 9, 1, "3 uncommitted"},
		{"end of uncommitted", 21, 1, "3 uncommitted"},
		{"separator after uncommitted", 22, 1, ""},
		{"start of commits", 25, 1, "5 commits"},
		{"end of commits", 33, 1, "5 commits"},
		{"start of no-PR", 37, 1, "No PR"},
		{"end of no-PR", 41, 1, "No PR"},
		{"one past the end of the bar", 42, 1, ""},
		{"far past the end of the bar", 200, 1, ""},
		{"hover on line 1 leaves line 2 alone", 9, 0, ""},
		{"hover below the bar", 9, 2, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := line2HoverData()
			data.hoverX, data.hoverY = tc.hoverX, tc.hoverY
			bar, _, _, _ := renderStatusBar(120, data)
			lines := strings.Split(bar, "\n")
			if len(lines) != 2 {
				t.Fatalf("want 2 rows, got %d: %q", len(lines), bar)
			}
			if got := underlinedRun(t, lines[1]); got != tc.want {
				t.Errorf("hover (%d,%d): underlined %q, want %q", tc.hoverX, tc.hoverY, got, tc.want)
			}
		})
	}
}

// line3HoverData exercises the parts that embed raw SGR sequences (the
// [DRAFT] marker, which ends in its own `0` reset) and OSC 8 hyperlinks (the
// PR link and the CI status), all of which the underline pair has to bracket
// without being clobbered and without changing measured width.
func line3HoverData() statusBarData {
	return statusBarData{
		info:         git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		mode:         FilesMode,
		pr:           git.PRInfoResult{Number: 42, Title: "My PR", URL: "https://example.com/pull/42", IsDraft: true},
		reviews:      []git.PRReview{{Author: "alice", State: "APPROVED"}},
		commentCount: 7,
		ciStatus:     git.CIStatusResult{State: "SUCCESS", URL: "https://ci.example.com"},
	}
}

func TestStatusBarHover_Line3(t *testing.T) {
	// Resolve the expected columns from the click regions themselves: line 3
	// carries emoji and check marks, so hard-coding columns here would encode
	// this machine's idea of their width rather than the geometry under test.
	// The point of the assertion is that the underline lands on the label a
	// click at that column would dispatch.
	data := line3HoverData()
	_, _, _, labels := renderStatusBar(120, data)
	if len(labels) != 5 {
		t.Fatalf("want 5 line-3 labels (draft, link, reviews, comments, CI), got %d: %+v", len(labels), labels)
	}
	texts := map[line3Target]string{
		line3Description: " [DRAFT]",
		line3Reviews:     "1✓",
		line3Comments:    "7 comments",
		line3CI:          "✅ CI passing",
	}

	for _, label := range labels {
		want := texts[label.target]
		if label.target == line3Description && label.start > 1 {
			want = "PR #42: My PR" // the second description label is the link
		}
		for _, x := range []int{label.start, label.end - 1} {
			d := data
			d.hoverX, d.hoverY = x, 2
			bar, _, _, _ := renderStatusBar(120, d)
			lines := strings.Split(bar, "\n")
			if len(lines) != 3 {
				t.Fatalf("want 3 rows, got %d", len(lines))
			}
			if got := underlinedRun(t, lines[2]); got != want {
				t.Errorf("hover x=%d: underlined %q, want %q", x, got, want)
			}
			// The other rows must stay untouched.
			if got := underlinedRun(t, lines[1]); got != "" {
				t.Errorf("hover x=%d on line 3 also underlined %q on line 2", x, got)
			}
		}
	}

	// Separators underline nothing. Every column between one label's end and
	// the next label's start is separator.
	for i := 0; i+1 < len(labels); i++ {
		for x := labels[i].end; x < labels[i+1].start; x++ {
			d := data
			d.hoverX, d.hoverY = x, 2
			bar, _, _, _ := renderStatusBar(120, d)
			lines := strings.Split(bar, "\n")
			if got := underlinedRun(t, lines[2]); got != "" {
				t.Errorf("hover on separator column %d underlined %q, want nothing", x, got)
			}
		}
	}
}

// TestStatusBarHover_Line3WithoutLine2 pins the row-index shift: with no git
// repo there is no line 2, so line 3 renders on row 1 and that is where it
// must be hoverable.
func TestStatusBarHover_Line3WithoutLine2(t *testing.T) {
	data := line3HoverData()
	data.info = git.RepoInfoResult{}
	rows := statusBarRows(data)
	if rows.line2 != -1 || rows.line3 != 1 {
		t.Fatalf("row layout = %+v, want line2 absent and line3 on row 1", rows)
	}

	_, _, _, labels := renderStatusBar(120, data)
	if len(labels) == 0 {
		t.Fatal("no line-3 labels")
	}
	last := labels[len(labels)-1]

	data.hoverX, data.hoverY = last.start, 1
	bar, _, _, _ := renderStatusBar(120, data)
	lines := strings.Split(bar, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(lines), bar)
	}
	if got := underlinedRun(t, lines[1]); got != "✅ CI passing" {
		t.Errorf("hover (%d,1): underlined %q, want the CI label", last.start, got)
	}

	// Row 2 does not exist; hovering it must underline nothing anywhere.
	data.hoverY = 2
	bar, _, _, _ = renderStatusBar(120, data)
	for i, row := range strings.Split(bar, "\n") {
		if got := underlinedRun(t, row); got != "" {
			t.Errorf("hover on nonexistent row 2 underlined %q on row %d", got, i)
		}
	}
}

// TestStatusBarHover_TruncatedLabelsAreNotHoverable is the regression test for
// the pre-existing phantom-target bug: label columns were computed before
// ellipsize(bar, width-2) ran, so on a narrow terminal a click past the
// truncation point fired a target whose text was not on screen. PROMPT.md:
// "a label whose click target was truncated away is not hoverable either —
// hover regions and click regions are the same regions."
func TestStatusBarHover_TruncatedLabelsAreNotHoverable(t *testing.T) {
	// Content area is width-2 = 28 columns; the full ASCII bar wants 41, so
	// one column goes to the "…" and 27 columns of content survive. Content
	// starts at column 1 (the style's left padding), so column 28 is the
	// first column no label may claim.
	const width = 30
	const limit = 28
	data := line2HoverData()
	bar0, _, labels, _ := renderStatusBar(width, data)
	visible := stripANSI(strings.Split(bar0, "\n")[1])

	if len(labels) == 0 {
		t.Fatal("no line-2 labels at all")
	}
	for _, l := range labels {
		if l.start >= l.end {
			t.Errorf("empty label region %+v survived", l)
		}
		if l.end > limit {
			t.Errorf("label %+v extends past the rendered content (limit %d, row %q)", l, limit, visible)
		}
	}
	// "No PR" starts at column 37 and cannot survive a 28-column content
	// area, so it must be gone entirely.
	if len(labels) != 3 {
		t.Errorf("got %d labels, want 3 — the truncated-away label must be dropped: %+v", len(labels), labels)
	}

	// A column past the truncation point underlines nothing.
	for _, x := range []int{limit, limit + 1, 37, 40} {
		d := data
		d.hoverX, d.hoverY = x, 1
		bar, _, _, _ := renderStatusBar(width, d)
		lines := strings.Split(bar, "\n")
		if got := underlinedRun(t, lines[1]); got != "" {
			t.Errorf("hover x=%d past truncation underlined %q, want nothing", x, got)
		}
	}

	// A label the truncation clipped in half stays hoverable over the columns
	// that survived.
	tail := labels[len(labels)-1]
	d := data
	d.hoverX, d.hoverY = tail.end-1, 1
	bar, _, _, _ := renderStatusBar(width, d)
	lines := strings.Split(bar, "\n")
	if got := underlinedRun(t, lines[1]); got == "" {
		t.Errorf("hover x=%d on the surviving columns of %+v underlined nothing: %q", tail.end-1, tail, lines[1])
	}
}

// TestStatusBarHover_Line1ModeLabels covers the line-1 labels through the same
// harness, including the truncation clipping that line 1 shared with lines 2
// and 3.
func TestStatusBarHover_Line1ModeLabels(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "repo", DirName: "repo"},
		mode: FilesMode,
	}
	// " files commits help" — "files" at [2,7), "commits" at [8,15),
	// "help" at [16,20).
	tests := []struct {
		hoverX int
		want   string
	}{
		{0, ""},
		{1, ""},
		{2, "files"},
		{6, "files"},
		{7, ""},
		{8, "commits"},
		{14, "commits"},
		{15, ""},
		{16, "help"},
		{19, "help"},
		{20, ""},
	}
	for _, tc := range tests {
		d := data
		d.hoverX, d.hoverY = tc.hoverX, 0
		bar, _, _, _ := renderStatusBar(120, d)
		lines := strings.Split(bar, "\n")
		if got := underlinedRun(t, lines[0]); got != tc.want {
			t.Errorf("hover x=%d: underlined %q, want %q", tc.hoverX, got, tc.want)
		}
	}
}

// TestRenderStatusBar_ConfirmRowFitsNarrowTerminal is the regression test for
// a bug the hover property test turned up: the quit prompt was padded but
// never truncated, so at width 8 lipgloss hard-wrapped it onto 8 rows while
// statusBarRows promised 1 — the row-count desync that shifts every click
// target below the bar. It survived because at ordinary widths the only
// overflow was the padding lipgloss trims anyway.
func TestRenderStatusBar_ConfirmRowFitsNarrowTerminal(t *testing.T) {
	for _, width := range []int{1, 2, 3, 8, 20, 53, 54, 100, 200} {
		data := statusBarData{
			info:       git.RepoInfoResult{Branch: "main", RepoName: "repo", DirName: "repo"},
			pr:         git.PRInfoResult{Number: 1, Title: "t"},
			confirming: true,
		}
		bar, _, _, _ := renderStatusBar(width, data)
		if got, want := len(strings.Split(bar, "\n")), statusBarLineCount(data); got != want {
			t.Errorf("width %d: confirm bar rendered %d rows, statusBarLineCount promised %d: %q",
				width, got, want, bar)
		}
	}
}
