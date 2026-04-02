package ui

import (
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
)

func TestRenderStatusBar_Basic(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{
			Branch:   "main",
			RepoName: "prwatch",
			DirName:  "prwatch",
		},
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(80, data)

	if !strings.Contains(bar, "main") {
		t.Error("status bar should contain branch name")
	}
	if !strings.Contains(bar, "prwatch") {
		t.Error("status bar should contain dir/repo name")
	}
	if !strings.Contains(bar, "[diff]") {
		t.Error("status bar should show diff mode indicator")
	}
}

func TestRenderStatusBar_FileViewMode(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "test", DirName: "test"},
		mode: FileViewMode,
	}
	bar, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "[file]") {
		t.Error("status bar should show file mode indicator")
	}
}

func TestRenderStatusBar_CommitMode(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "test", DirName: "test"},
		mode: CommitMode,
	}
	bar, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "[commits]") {
		t.Error("status bar should show commit mode indicator")
	}
}

func TestRenderStatusBar_Confirming(t *testing.T) {
	data := statusBarData{
		info:       git.RepoInfoResult{Branch: "main"},
		mode:       FileDiffMode,
		confirming: true,
	}
	bar, _ := renderStatusBar(80, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(120, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(80, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(80, data)
	if !strings.Contains(bar, "in repo") {
		t.Error("should indicate parent repo name for worktree")
	}
}

func TestRenderStatusBar_NoPR(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "main", RepoName: "repo", DirName: "repo"},
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(200, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(20, data)
	if bar == "" {
		t.Error("should still render even when narrow")
	}
}

func TestRenderStatusBar_ConfirmNarrow(t *testing.T) {
	data := statusBarData{confirming: true}
	bar, _ := renderStatusBar(10, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(120, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(120, data)
	if !strings.Contains(bar, "3 unpushed") {
		t.Error("should show unpushed count")
	}
}

func TestRenderStatusBar_GitStatusSummary(t *testing.T) {
	data := statusBarData{
		info:          git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		mode:          FileDiffMode,
		uncommitCount: 2,
		commitCount:   5,
	}
	bar, _ := renderStatusBar(120, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(120, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(120, data)
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
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(120, data)
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
	result := renderReviews(reviews, "")
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

func TestRenderReviews_Empty(t *testing.T) {
	result := renderReviews(nil, "")
	if result != "" {
		t.Errorf("empty reviews should return empty, got %q", result)
	}
}

func TestRenderReviews_UnknownDecision(t *testing.T) {
	result := renderReviews(nil, "UNKNOWN_STATE")
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
		result := renderReviews(nil, tt.decision)
		if result != tt.expected {
			t.Errorf("decision %q: got %q, want %q", tt.decision, result, tt.expected)
		}
	}
}

func TestRenderStatusBar_WithComments(t *testing.T) {
	data := statusBarData{
		info:         git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr:           git.PRInfoResult{Number: 1, Title: "test"},
		mode:         FileDiffMode,
		commentCount: 5,
	}
	bar, _ := renderStatusBar(120, data)
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
		mode:         FileDiffMode,
	}
	bar, _ := renderStatusBar(200, data)
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
	result := renderLine3(80, data)
	if !strings.Contains(result, "PR #1") {
		t.Error("should show PR without URL")
	}
}

func TestRenderStatusBar_ThreeLines(t *testing.T) {
	data := statusBarData{
		info: git.RepoInfoResult{Branch: "feature", RepoName: "repo", DirName: "repo"},
		pr:   git.PRInfoResult{Number: 1, Title: "test"},
		mode: FileDiffMode,
	}
	bar, _ := renderStatusBar(80, data)
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
