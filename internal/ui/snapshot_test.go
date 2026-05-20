package ui

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
)

var update = flag.Bool("update", false, "update golden files")

// stripANSI removes all ANSI escape codes from a string so golden files
// are human-readable and diff-friendly.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\]8;;[^\x1b]*\x1b\\`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// goldenPath returns the path to a golden file for the given test name.
func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".txt")
}

// assertGolden compares rendered output against a golden file.
// If -update is passed, the golden file is overwritten.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file %s not found; run with -update to create it: %v", path, err)
	}
	if got != string(expected) {
		t.Errorf("output does not match golden file %s\n\n--- want ---\n%s\n--- got ---\n%s", path, string(expected), got)
	}
}

// renderScenario creates a Model with the given mock data and returns the
// ANSI-stripped rendered output at the given terminal size.
func renderScenario(mock *mockGit, width, height int) string {
	m := NewModel("/tmp/test-repo", mock)
	return stripANSI(m.RenderOnce(width, height))
}

func standardMock() *mockGit {
	return &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:   "feature/add-auth",
			Upstream: "origin/main",
			RepoName: "myapp",
			DirName:  "myapp",
		},
		prInfo: git.PRInfoResult{
			Number: 42,
			Title:  "Add authentication",
			URL:    "https://github.com/org/myapp/pull/42",
		},
		base: "origin/main",
		changedFiles: git.ChangedFilesResult{
			Committed:   []string{"auth.go", "config.go"},
			Uncommitted: []string{"README.md"},
			Added:       []string{"README.md"},
		},
		commits: []git.Commit{
			{SHA: "abc1234", Subject: "Add auth middleware"},
			{SHA: "def5678", Subject: "Update config parsing"},
		},
		fileDiff:    "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1,3 +1,4 @@\n # My App\n+Authentication docs\n",
		fileContent: "# My App\nAuthentication docs\n",
		commitPatch: "commit abc1234\nAuthor: test\n\n    Add auth middleware\n\ndiff --git a/auth.go b/auth.go\n+++ b/auth.go\n+package auth\n",
	}
}

func TestSnapshot_FilesMode(t *testing.T) {
	mock := standardMock()
	m := NewModel("/tmp/test-repo", mock)
	m.mode = FilesMode
	out := stripANSI(m.RenderOnce(100, 30))
	assertGolden(t, "files_mode", out)
}

func TestSnapshot_CommitsMode(t *testing.T) {
	mock := standardMock()
	m := NewModel("/tmp/test-repo", mock)
	m.mode = CommitsMode
	out := stripANSI(m.RenderOnce(100, 30))
	assertGolden(t, "commits_mode", out)
}

func TestSnapshot_DetachedHead(t *testing.T) {
	mock := standardMock()
	mock.repoInfo.Branch = ""
	mock.repoInfo.IsDetachedHead = true
	mock.repoInfo.HeadSHA = "abc1234"
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "detached_head", out)
}

func TestSnapshot_NoPR(t *testing.T) {
	mock := standardMock()
	mock.prInfo = git.PRInfoResult{}
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "no_pr", out)
}

func TestSnapshot_DraftPRWithCI(t *testing.T) {
	mock := standardMock()
	mock.prInfo.IsDraft = true
	mock.ciStatus = git.CIStatusResult{State: "FAILURE", URL: "https://ci.example.com/run/1"}
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "draft_pr_ci_failure", out)
}

func TestSnapshot_WithReviews(t *testing.T) {
	mock := standardMock()
	mock.reviews = []git.PRReview{
		{Author: "alice", State: "APPROVED"},
		{Author: "bob", State: "CHANGES_REQUESTED"},
	}
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "with_reviews", out)
}

func TestSnapshot_NarrowTerminal(t *testing.T) {
	mock := standardMock()
	out := renderScenario(mock, 60, 20)
	assertGolden(t, "narrow_terminal", out)
}

func TestSnapshot_WideTerminal(t *testing.T) {
	mock := standardMock()
	out := renderScenario(mock, 160, 40)
	assertGolden(t, "wide_terminal", out)
}

func TestSnapshot_NoFiles(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:   "main",
			RepoName: "empty",
			DirName:  "empty",
		},
		base:         "origin/main",
		changedFiles: git.ChangedFilesResult{},
	}
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "no_files", out)
}

func TestSnapshot_OnlyUncommittedFiles(t *testing.T) {
	mock := standardMock()
	mock.changedFiles.Committed = nil
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "only_uncommitted", out)
}

func TestSnapshot_HelpOverlay(t *testing.T) {
	mock := standardMock()
	m := NewModel("/tmp/test-repo", mock)
	m.help.Open()
	out := stripANSI(m.RenderOnce(100, 30))
	assertGolden(t, "help_overlay", out)
}

func TestSnapshot_ConfirmQuit(t *testing.T) {
	mock := standardMock()
	m := NewModel("/tmp/test-repo", mock)
	m.confirming = true
	out := stripANSI(m.RenderOnce(100, 30))
	assertGolden(t, "confirm_quit", out)
}

func TestSnapshot_ManyFiles(t *testing.T) {
	mock := standardMock()
	var committed []string
	for i := 0; i < 50; i++ {
		committed = append(committed, strings.Repeat("a", 3)+string(rune('a'+(i%26)))+".go")
	}
	mock.changedFiles.Committed = committed
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "many_files", out)
}

func TestSnapshot_Worktree(t *testing.T) {
	mock := standardMock()
	mock.repoInfo.Worktree = "/tmp/worktrees/feature"
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "worktree", out)
}

func TestSnapshot_AheadOfUpstream(t *testing.T) {
	mock := standardMock()
	mock.repoInfo.AheadCount = 5
	out := renderScenario(mock, 100, 30)
	assertGolden(t, "ahead_upstream", out)
}

// TestSnapshot_MidScrollMultiHunk captures the main pane rendering when a
// multi-hunk file is scrolled past the first hunk. It exists as a concrete
// before/after artifact for the Position/Range refactor (PLAN.md step 1):
// it exercises the joint output of hunkTitleRight, progressPercent,
// visibleHunkRange, and visibleRange — the touch points that step 1
// routes through Position/Range. A drift here means the refactor changed
// something it shouldn't have.
func TestSnapshot_MidScrollMultiHunk(t *testing.T) {
	mock := standardMock()
	mock.fileDiff = "diff --git a/auth.go b/auth.go\n" +
		"--- a/auth.go\n" +
		"+++ b/auth.go\n" +
		"@@ -3,6 +3,7 @@ package auth\n" +
		" \n" +
		" import (\n" +
		" \t\"fmt\"\n" +
		"+\t\"errors\"\n" +
		" \t\"os\"\n" +
		" )\n" +
		" \n" +
		"@@ -15,7 +16,8 @@ func init() {\n" +
		" \ta := 1\n" +
		" \tb := 2\n" +
		"-\tc := 3\n" +
		"+\tc := 3\n" +
		"+\td := 4\n" +
		" \treturn\n" +
		" }\n" +
		" \n" +
		"@@ -28,5 +30,6 @@ func cleanup() {\n" +
		" \tp := 1\n" +
		" \tq := 2\n" +
		"+\tr := 3\n" +
		" \treturn\n" +
		" }\n"
	mock.fileContent = strings.Repeat("filler line\n", 40)

	m := initModel(mock, FilesMode, 100, 30)

	// Select the first file in the sidebar (skip non-file items like
	// the Description pseudo-entry).
	for i, item := range m.sidebar.items {
		if item.filePath != "" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	// Scroll partway into the file so progressPercent is non-trivial and
	// hunkTitleRight transitions between hunks. With viewport height ~26
	// content rows after status bar / borders, an offset of 10 should put
	// us in or just past the second hunk.
	m.mainPane.viewport.SetYOffset(10)

	out := stripANSI(m.RenderOnce(100, 30))
	assertGolden(t, "midscroll_multihunk", out)
}
