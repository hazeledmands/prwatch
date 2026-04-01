package ui

import (
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
)

func TestRenderStatusBar_Basic(t *testing.T) {
	info := git.RepoInfoResult{
		Branch:   "main",
		RepoName: "prwatch",
	}
	bar := renderStatusBar(80, info, git.PRInfoResult{}, FileDiffMode, false)

	if !strings.Contains(bar, "main") {
		t.Error("status bar should contain branch name")
	}
	if !strings.Contains(bar, "prwatch") {
		t.Error("status bar should contain repo name")
	}
	if !strings.Contains(bar, "[diff]") {
		t.Error("status bar should show diff mode indicator")
	}
}

func TestRenderStatusBar_FileViewMode(t *testing.T) {
	info := git.RepoInfoResult{Branch: "main", RepoName: "test"}
	bar := renderStatusBar(80, info, git.PRInfoResult{}, FileViewMode, false)

	if !strings.Contains(bar, "[file]") {
		t.Error("status bar should show file mode indicator")
	}
}

func TestRenderStatusBar_CommitMode(t *testing.T) {
	info := git.RepoInfoResult{Branch: "main", RepoName: "test"}
	bar := renderStatusBar(80, info, git.PRInfoResult{}, CommitMode, false)

	if !strings.Contains(bar, "[commits]") {
		t.Error("status bar should show commit mode indicator")
	}
}

func TestRenderStatusBar_Confirming(t *testing.T) {
	info := git.RepoInfoResult{Branch: "main"}
	bar := renderStatusBar(80, info, git.PRInfoResult{}, FileDiffMode, true)

	if !strings.Contains(bar, "Quit?") {
		t.Error("confirming status bar should show quit prompt")
	}
}

func TestRenderStatusBar_WithPR(t *testing.T) {
	info := git.RepoInfoResult{Branch: "feature", RepoName: "repo"}
	pr := git.PRInfoResult{
		Number: 42,
		Title:  "My PR",
		URL:    "https://github.com/org/repo/pull/42",
	}
	bar := renderStatusBar(120, info, pr, FileDiffMode, false)

	if !strings.Contains(bar, "PR #42") {
		t.Error("should show PR number")
	}
	if !strings.Contains(bar, "My PR") {
		t.Error("should show PR title")
	}
}

func TestRenderStatusBar_DetachedHead(t *testing.T) {
	info := git.RepoInfoResult{
		Branch:         "HEAD",
		IsDetachedHead: true,
		HeadSHA:        "abc1234",
	}
	bar := renderStatusBar(80, info, git.PRInfoResult{}, FileDiffMode, false)

	if !strings.Contains(bar, "detached @ abc1234") {
		t.Error("should show detached HEAD with SHA")
	}
}

func TestRenderStatusBar_Worktree(t *testing.T) {
	info := git.RepoInfoResult{
		Branch:   "feature",
		RepoName: "repo",
		Worktree: "/some/path",
	}
	bar := renderStatusBar(80, info, git.PRInfoResult{}, FileDiffMode, false)

	if !strings.Contains(bar, "(worktree)") {
		t.Error("should indicate worktree")
	}
}

func TestRenderStatusBar_NarrowWidth(t *testing.T) {
	info := git.RepoInfoResult{
		Branch:   "hazel/very-long-feature-branch-name/with-lots-of-detail",
		RepoName: "my-really-long-repository-name",
	}
	pr := git.PRInfoResult{
		Number: 999,
		Title:  "Very long PR title that overflows",
		URL:    "https://github.com/org/repo/pull/999",
	}
	// Narrow width forces padding < 0
	bar := renderStatusBar(20, info, pr, FileDiffMode, false)
	if bar == "" {
		t.Error("should still render even when narrow")
	}
}

func TestRenderStatusBar_ConfirmNarrow(t *testing.T) {
	info := git.RepoInfoResult{}
	bar := renderStatusBar(10, info, git.PRInfoResult{}, FileDiffMode, true)
	if !strings.Contains(bar, "Quit?") {
		t.Error("confirming bar should show quit prompt even when narrow")
	}
}

func TestRenderStatusBar_NoPR(t *testing.T) {
	info := git.RepoInfoResult{Branch: "main", RepoName: "repo"}
	// Use a wide bar so "No PR" isn't wrapped
	bar := renderStatusBar(200, info, git.PRInfoResult{}, FileDiffMode, false)

	if !strings.Contains(bar, "No PR") {
		t.Errorf("should show 'No PR' when no PR exists, got: %q", bar)
	}
}
