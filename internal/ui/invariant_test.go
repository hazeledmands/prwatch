package ui

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// genMockGit generates random but valid mockGit instances for property testing.
func genMockGit(t *rapid.T) *mockGit {
	nCommitted := rapid.IntRange(0, 20).Draw(t, "nCommitted")
	nUncommitted := rapid.IntRange(0, 10).Draw(t, "nUncommitted")
	nCommits := rapid.IntRange(0, 20).Draw(t, "nCommits")

	committed := make([]string, nCommitted)
	for i := range committed {
		committed[i] = fmt.Sprintf("file%d.go", i)
	}
	uncommitted := make([]string, nUncommitted)
	for i := range uncommitted {
		uncommitted[i] = fmt.Sprintf("new%d.go", i)
	}
	commits := make([]git.Commit, nCommits)
	for i := range commits {
		commits[i] = git.Commit{
			SHA:     fmt.Sprintf("%07d", i),
			Subject: fmt.Sprintf("commit message %d", i),
		}
	}

	branch := rapid.SampledFrom([]string{"main", "feature/auth", "hazel/fix-bug"}).Draw(t, "branch")
	isDetached := rapid.Bool().Draw(t, "detached")
	if isDetached {
		branch = ""
	}

	prNum := rapid.SampledFrom([]int{0, 42, 100}).Draw(t, "prNum")
	var prInfo git.PRInfoResult
	if prNum > 0 {
		prInfo = git.PRInfoResult{
			Number:  prNum,
			Title:   "Test PR",
			URL:     "https://github.com/org/repo/pull/42",
			IsDraft: rapid.Bool().Draw(t, "isDraft"),
		}
	}

	aheadCount := rapid.IntRange(0, 10).Draw(t, "aheadCount")

	return &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:         branch,
			Upstream:       "origin/main",
			RepoName:       "testrepo",
			DirName:        "testrepo",
			HeadSHA:        "abc1234",
			IsDetachedHead: isDetached,
			AheadCount:     aheadCount,
		},
		prInfo: prInfo,
		base:   "origin/main",
		changedFiles: git.ChangedFilesResult{
			Committed:   committed,
			Uncommitted: uncommitted,
		},
		commits:     commits,
		allCommits:  commits,
		fileDiff:    "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n",
		fileContent: "line1\nline2\nline3\n",
		commitPatch: "commit 0000000\n\n    msg\n\ndiff\n+added\n",
	}
}

// displayWidth returns the display width of a string, accounting for
// multi-byte UTF-8 characters. East Asian wide characters are counted as 2.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == '\t':
			w += 8 // tab stop
		case utf8.RuneLen(r) > 1 && isWide(r):
			w += 2
		default:
			w++
		}
	}
	return w
}

// isWide returns true for East Asian wide characters.
func isWide(r rune) bool {
	// CJK Unified Ideographs and common wide ranges
	return (r >= 0x1100 && r <= 0x115F) ||
		(r >= 0x2E80 && r <= 0xA4CF) ||
		(r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE6F) ||
		(r >= 0xFF01 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x20000 && r <= 0x2FA1F)
}

func renderModel(mock *mockGit, mode Mode, width, height int) string {
	m := NewModel("/tmp/test-repo", mock)
	m.mode = mode
	return m.RenderOnce(width, height)
}

func TestProperty_LineCountEqualsTerminalHeight(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(40, 200).Draw(t, "width")
		height := rapid.IntRange(10, 60).Draw(t, "height")
		mode := Mode(rapid.IntRange(0, 2).Draw(t, "mode"))
		mock := genMockGit(t)

		output := renderModel(mock, mode, width, height)
		lines := strings.Split(output, "\n")

		if len(lines) != height {
			t.Fatalf("expected %d lines, got %d (width=%d, mode=%d)",
				height, len(lines), width, mode)
		}
	})
}

func TestProperty_EveryLineIsTerminalWidth(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(40, 200).Draw(t, "width")
		height := rapid.IntRange(10, 60).Draw(t, "height")
		mode := Mode(rapid.IntRange(0, 2).Draw(t, "mode"))
		mock := genMockGit(t)

		output := renderModel(mock, mode, width, height)
		stripped := stripANSI(output)
		lines := strings.Split(stripped, "\n")

		for i, line := range lines {
			w := displayWidth(line)
			if w != width {
				t.Fatalf("line %d has display width %d, expected %d\nline: %q",
					i+1, w, width, line)
			}
		}
	})
}

func TestProperty_NoUnexpectedLineWrapping(t *testing.T) {
	// The status bar lines (line 1 and 2) should each be a single line —
	// no embedded newlines within a status bar render call.
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(40, 200).Draw(t, "width")
		mode := Mode(rapid.IntRange(0, 2).Draw(t, "mode"))
		mock := genMockGit(t)

		// Render the status bar directly and verify no wrapping
		data := statusBarData{
			info:          mock.repoInfo,
			pr:            mock.prInfo,
			mode:          mode,
			uncommitCount: len(mock.changedFiles.Uncommitted),
			commitCount:   len(mock.commits),
		}
		bar, _ := renderStatusBar(width, data)
		stripped := stripANSI(bar)
		barLines := strings.Split(stripped, "\n")

		// Status bar should be 1-3 lines depending on git/PR state
		expectedLines := statusBarLineCount(data)
		if len(barLines) != expectedLines {
			t.Fatalf("status bar should be %d lines, got %d (width=%d)\nbar: %q",
				expectedLines, len(barLines), width, stripped)
		}

		for i, line := range barLines {
			w := displayWidth(line)
			if w != width {
				t.Fatalf("status bar line %d has width %d, expected %d\nline: %q",
					i+1, w, width, line)
			}
		}
	})
}

func TestProperty_ClickSidebarSelectsItem(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 160).Draw(t, "width")
		height := rapid.IntRange(15, 50).Draw(t, "height")
		mock := genMockGit(t)

		// Only test if there are files to click
		totalFiles := len(mock.changedFiles.Committed) + len(mock.changedFiles.Uncommitted)
		if totalFiles == 0 {
			return
		}

		m := NewModel("/tmp/test-repo", mock)
		m.mode = FileDiffMode
		m.width = width
		m.height = height
		m.updateLayout()

		// Load data
		msg := m.loadGitData()
		m.Update(msg)

		// The sidebar starts at row 2 (after 2-line status bar), inside border at row 3
		// and column 1 (inside the left border of the sidebar)
		statusBarHeight := statusBarLineCount(statusBarData{info: mock.repoInfo, pr: mock.prInfo})
		sidebarContentRow := statusBarHeight + 1 // first row inside sidebar border
		sidebarContentCol := 1                   // first col inside sidebar border

		// Build expected file list (tree mode sorts alphabetically)
		uncommittedSorted := make([]string, len(mock.changedFiles.Uncommitted))
		copy(uncommittedSorted, mock.changedFiles.Uncommitted)
		sort.Strings(uncommittedSorted)
		committedSorted := make([]string, len(mock.changedFiles.Committed))
		copy(committedSorted, mock.changedFiles.Committed)
		sort.Strings(committedSorted)

		var expectedFiles []string
		hasSeparator := len(uncommittedSorted) > 0 && len(committedSorted) > 0
		expectedFiles = append(expectedFiles, uncommittedSorted...)
		if hasSeparator {
			expectedFiles = append(expectedFiles, "") // placeholder for separator
		}
		expectedFiles = append(expectedFiles, committedSorted...)

		// Click on each visible file
		visibleCount := min(len(expectedFiles), height-statusBarHeight-2) // minus borders
		for i := 0; i < visibleCount; i++ {
			if expectedFiles[i] == "" {
				continue // skip separator
			}
			row := sidebarContentRow + i
			col := sidebarContentCol

			clickMsg := tea.MouseClickMsg{
				X:      col,
				Y:      row,
				Button: tea.MouseLeft,
			}
			result, _ := m.Update(clickMsg)
			m = result.(*Model)

			selected := m.sidebar.SelectedItem()
			if selected != expectedFiles[i] {
				t.Fatalf("clicked row %d (item %d), expected %q, got %q",
					row, i, expectedFiles[i], selected)
			}
		}
	})
}

func TestProperty_ClickCommitSelectsCommit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 160).Draw(t, "width")
		height := rapid.IntRange(15, 50).Draw(t, "height")
		mock := genMockGit(t)

		if len(mock.commits) == 0 {
			return
		}

		m := NewModel("/tmp/test-repo", mock)
		m.mode = CommitMode
		m.width = width
		m.height = height
		m.updateLayout()

		msg := m.loadGitData()
		m.Update(msg)

		statusBarHeight := statusBarLineCount(statusBarData{info: mock.repoInfo, pr: mock.prInfo})
		sidebarContentRow := statusBarHeight + 1
		sidebarContentCol := 1

		// Build expected items list matching the new categorized commit sidebar
		unpushed := mock.repoInfo.AheadCount
		type expectedItem struct {
			label       string
			isSeparator bool
		}
		var expected []expectedItem

		// Category 1: uncommitted changes
		uncommitted := mock.changedFiles.Uncommitted
		if len(uncommitted) > 0 {
			expected = append(expected, expectedItem{
				label: fmt.Sprintf("uncommitted changes (%d files)", len(uncommitted)),
			})
		}

		// Category 2: unpushed commits (dimmed)
		if unpushed > 0 {
			if len(expected) > 0 {
				expected = append(expected, expectedItem{isSeparator: true})
			}
			for i := 0; i < unpushed && i < len(mock.commits); i++ {
				c := mock.commits[i]
				expected = append(expected, expectedItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				})
			}
		}

		// Category 3: pushed commits
		if unpushed < len(mock.commits) {
			if len(expected) > 0 {
				expected = append(expected, expectedItem{isSeparator: true})
			}
			for i := unpushed; i < len(mock.commits); i++ {
				c := mock.commits[i]
				expected = append(expected, expectedItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				})
			}
		}

		visibleCount := min(len(expected), height-statusBarHeight-2)
		for i := 0; i < visibleCount; i++ {
			if expected[i].isSeparator {
				continue // skip separator rows
			}
			row := sidebarContentRow + i
			clickMsg := tea.MouseClickMsg{
				X:      sidebarContentCol,
				Y:      row,
				Button: tea.MouseLeft,
			}
			result, _ := m.Update(clickMsg)
			m = result.(*Model)

			selected := m.sidebar.SelectedItem()
			if selected != expected[i].label {
				t.Fatalf("clicked commit row %d (item %d), expected %q, got %q",
					row, i, expected[i].label, selected)
			}
		}
	})
}

func TestProperty_HelpOverlayLineCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(40, 200).Draw(t, "width")
		height := rapid.IntRange(10, 60).Draw(t, "height")
		mock := genMockGit(t)

		m := NewModel("/tmp/test-repo", mock)
		m.showHelp = true
		output := m.RenderOnce(width, height)
		lines := strings.Split(output, "\n")

		if len(lines) != height {
			t.Fatalf("help overlay: expected %d lines, got %d", height, len(lines))
		}
	})
}

func TestProperty_ConfirmQuitLineCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(40, 200).Draw(t, "width")
		height := rapid.IntRange(10, 60).Draw(t, "height")
		mock := genMockGit(t)

		m := NewModel("/tmp/test-repo", mock)
		m.confirming = true
		output := m.RenderOnce(width, height)
		lines := strings.Split(output, "\n")

		if len(lines) != height {
			t.Fatalf("confirm quit: expected %d lines, got %d", height, len(lines))
		}
	})
}
