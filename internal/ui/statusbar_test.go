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
	result, _ := renderLine3(80, data)
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
