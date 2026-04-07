package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Scenario generators
// ---------------------------------------------------------------------------

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

// genScenario generates a broader range of UI states than genMockGit alone.
// It can produce non-git directories, repos with PRs, reviews, CI, base
// commits, etc.
func genScenario(t *rapid.T) (*mockGit, Mode) {
	isGit := rapid.Float64Range(0, 1).Draw(t, "isGit") > 0.1 // 90% git, 10% non-git
	if !isGit {
		return nil, FileViewMode
	}

	mock := genMockGit(t)

	// Optionally add PR reviews
	if mock.prInfo.Number > 0 && rapid.Bool().Draw(t, "hasReviews") {
		nReviews := rapid.IntRange(1, 3).Draw(t, "nReviews")
		for i := range nReviews {
			state := rapid.SampledFrom([]string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED"}).Draw(t, fmt.Sprintf("reviewState%d", i))
			mock.reviews = append(mock.reviews, git.PRReview{
				Author: fmt.Sprintf("reviewer%d", i),
				State:  state,
			})
		}
	}

	// Optionally add PR comments
	if mock.prInfo.Number > 0 && rapid.Bool().Draw(t, "hasComments") {
		nComments := rapid.IntRange(1, 5).Draw(t, "nComments")
		mock.commentCount = nComments
		for i := range nComments {
			mock.prComments = append(mock.prComments, git.PRComment{
				Author: fmt.Sprintf("commenter%d", i),
				Body:   fmt.Sprintf("comment body %d", i),
			})
		}
	}

	// Optionally simulate GitHub API error (triggers 3-line status bar)
	if rapid.Bool().Draw(t, "hasAPIError") {
		mock.prInfoErr = fmt.Errorf("API rate limit exceeded")
	}

	// Optionally add base commits (category 4 in commit mode)
	if rapid.Bool().Draw(t, "hasBaseCommits") {
		nBase := rapid.IntRange(1, 10).Draw(t, "nBaseCommits")
		for i := range nBase {
			mock.baseCommits = append(mock.baseCommits, git.Commit{
				SHA:     fmt.Sprintf("base%04d", i),
				Subject: fmt.Sprintf("base commit %d", i),
			})
		}
	}

	// Pick a mode that makes sense for the scenario
	maxMode := 2 // FileView, FileDiff, Commit
	if mock.prInfo.Number > 0 {
		maxMode = 3 // also PRView
	}
	mode := Mode(rapid.IntRange(0, maxMode).Draw(t, "mode"))

	return mock, mode
}

// ---------------------------------------------------------------------------
// Action generator
// ---------------------------------------------------------------------------

// genAction generates a random user interaction (key press, mouse click,
// mouse scroll, or terminal resize) appropriate for the current model state.
func genAction(t *rapid.T, m *Model, step int) tea.Msg {
	tag := fmt.Sprintf("action%d", step)

	// Weight categories: keys are most common, mouse less so, resize rare
	category := rapid.SampledFrom([]string{
		"key", "key", "key", "key", "key",
		"mouse_click", "mouse_click",
		"mouse_scroll",
		"resize",
	}).Draw(t, tag+"_cat")

	switch category {
	case "key":
		return genKeyPress(t, tag)
	case "mouse_click":
		return genMouseClick(t, tag, m)
	case "mouse_scroll":
		return genMouseScroll(t, tag, m)
	case "resize":
		return genResize(t, tag)
	}
	return genKeyPress(t, tag) // fallback
}

func genKeyPress(t *rapid.T, tag string) tea.KeyPressMsg {
	type keyDef struct {
		text string
		code rune
	}

	// All the interesting keys, excluding quit (we don't want to exit)
	allKeys := []keyDef{
		// Mode switching
		{"m", 'm'}, {"v", 'v'}, {"d", 'd'}, {"c", 'c'}, {"b", 'b'},
		{"1", '1'}, {"2", '2'}, {"3", '3'}, {"4", '4'},
		// Navigation
		{"j", 'j'}, {"k", 'k'}, {"g", 'g'}, {"G", 'G'},
		{"h", 'h'}, {"l", 'l'},
		// Focus
		{",", ','}, {".", '.'},
		// Toggles
		{"t", 't'}, {"f", 'f'}, {"w", 'w'}, {"i", 'i'},
		{"D", 'D'},
		// Sidebar resize
		{"+", '+'}, {"-", '-'},
		// Diff navigation
		{"J", 'J'}, {"K", 'K'},
		// Leaf navigation
		{"N", 'N'}, {"P", 'P'},
		// Refresh
		{"r", 'r'},
		// Help
		{"?", '?'},
	}

	idx := rapid.IntRange(0, len(allKeys)-1).Draw(t, tag+"_key")
	k := allKeys[idx]
	return tea.KeyPressMsg{Text: k.text, Code: k.code}
}

func genSpecialKey(t *rapid.T, tag string) tea.KeyPressMsg {
	specials := []rune{
		tea.KeyEnter,
		tea.KeyTab,
		tea.KeyUp,
		tea.KeyDown,
		tea.KeyLeft,
		tea.KeyRight,
		tea.KeyPgUp,
		tea.KeyPgDown,
	}
	idx := rapid.IntRange(0, len(specials)-1).Draw(t, tag+"_special")
	return tea.KeyPressMsg{Code: specials[idx]}
}

func genMouseClick(t *rapid.T, tag string, m *Model) tea.MouseClickMsg {
	x := rapid.IntRange(0, max(1, m.width-1)).Draw(t, tag+"_x")
	y := rapid.IntRange(0, max(1, m.height-1)).Draw(t, tag+"_y")
	return tea.MouseClickMsg{
		X:      x,
		Y:      y,
		Button: tea.MouseLeft,
	}
}

func genMouseScroll(t *rapid.T, tag string, m *Model) tea.MouseWheelMsg {
	x := rapid.IntRange(0, max(1, m.width-1)).Draw(t, tag+"_x")
	y := rapid.IntRange(0, max(1, m.height-1)).Draw(t, tag+"_y")
	btn := rapid.SampledFrom([]tea.MouseButton{tea.MouseWheelUp, tea.MouseWheelDown}).Draw(t, tag+"_dir")
	return tea.MouseWheelMsg{
		X:      x,
		Y:      y,
		Button: btn,
	}
}

func genResize(t *rapid.T, tag string) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{
		Width:  rapid.IntRange(40, 200).Draw(t, tag+"_w"),
		Height: rapid.IntRange(10, 60).Draw(t, tag+"_h"),
	}
}

// ---------------------------------------------------------------------------
// Invariant checks
// ---------------------------------------------------------------------------

// checkRenderInvariants renders the model and verifies that the output has
// exactly height lines, each exactly width display-cells wide, with no panics.
func checkRenderInvariants(t *rapid.T, m *Model, context string) {
	t.Helper()
	v := m.View()
	output := v.Content
	width := m.width
	height := m.height

	lines := strings.Split(output, "\n")
	if len(lines) != height {
		t.Fatalf("%s: expected %d lines, got %d (width=%d, mode=%d)",
			context, height, len(lines), width, m.mode)
	}

	stripped := stripANSI(output)
	strippedLines := strings.Split(stripped, "\n")
	for i, line := range strippedLines {
		w := displayWidth(line)
		if w != width {
			t.Fatalf("%s: line %d has display width %d, expected %d\nline: %q",
				context, i+1, w, width, line)
		}
	}
}

// checkSidebarInvariants verifies the sidebar selection is valid.
func checkSidebarInvariants(t *rapid.T, m *Model, context string) {
	t.Helper()
	items := m.sidebar.items
	if len(items) == 0 {
		return
	}
	sel := m.sidebar.SelectedIndex()
	if sel < 0 || sel >= len(items) {
		t.Fatalf("%s: sidebar selection %d out of range [0, %d)",
			context, sel, len(items))
	}
	if !items[sel].kind.selectable() {
		t.Fatalf("%s: sidebar selection %d is on non-selectable item (kind=%d, label=%q)",
			context, sel, items[sel].kind, items[sel].label)
	}
}

// checkBottomBorder verifies the last line of rendered output contains
// box-drawing bottom-border characters (╰). Skipped for help overlay,
// confirm dialog, loading state, error state, and hidden sidebar.
func checkBottomBorder(t *rapid.T, m *Model, context string) {
	t.Helper()
	if m.showHelp || m.confirming || m.loading || m.err != nil || m.sidebarHidden {
		return
	}
	v := m.View()
	stripped := stripANSI(v.Content)
	lines := strings.Split(stripped, "\n")
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "╰") {
		t.Fatalf("%s: last line should contain bottom border (╰) but got: %q",
			context, lastLine)
	}
}

func checkAllInvariants(t *rapid.T, m *Model, context string) {
	t.Helper()
	checkRenderInvariants(t, m, context)
	checkSidebarInvariants(t, m, context)
	checkBottomBorder(t, m, context)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// initModel creates a model from a scenario, loads data, and sets dimensions.
func initModel(mock *mockGit, mode Mode, width, height int) *Model {
	dir := "/tmp/test-repo"
	if mock == nil {
		// Non-git mode needs a real directory to walk
		d, err := os.MkdirTemp("", "prwatch-test-*")
		if err == nil {
			dir = d
		}
	}
	m := NewModel(dir, mock)
	m.width = width
	m.height = height
	m.updateLayout()

	// Load data synchronously
	var msg tea.Msg
	if mock != nil {
		msg = m.loadGitData()
	} else {
		msg = m.loadNonGitFiles()
	}
	m.Update(msg)
	m.mode = mode
	m.updateSidebarItems()
	m.updateMainContent()
	return m
}

// applyAction sends a message to the model and handles synchronous follow-up
// commands (like loadMoreCommits). Returns the updated model.
func applyAction(m *Model, msg tea.Msg) *Model {
	result, cmd := m.Update(msg)
	m = result.(*Model)

	// Execute synchronous commands (at most one level deep to avoid loops).
	// Only follow up on messages we know are safe in test context — skip
	// commands that would do I/O (exec editor, git refresh, etc.)
	if cmd != nil {
		func() {
			defer func() { recover() }() // guard against panics from I/O commands
			followUp := cmd()
			if followUp == nil {
				return
			}
			switch followUp.(type) {
			case moreCommitsMsg, gitDataMsg:
				result, _ = m.Update(followUp)
				m = result.(*Model)
			}
		}()
	}
	return m
}

// ---------------------------------------------------------------------------
// Existing property tests (preserved)
// ---------------------------------------------------------------------------

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
		bar, _, _ := renderStatusBar(width, data)
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

		// Build expected item list (tree mode sorts alphabetically)
		// Includes headers and separators as empty strings (non-clickable)
		uncommittedSorted := make([]string, len(mock.changedFiles.Uncommitted))
		copy(uncommittedSorted, mock.changedFiles.Uncommitted)
		sort.Strings(uncommittedSorted)
		committedSorted := make([]string, len(mock.changedFiles.Committed))
		copy(committedSorted, mock.changedFiles.Committed)
		sort.Strings(committedSorted)

		var expectedFiles []string
		if len(uncommittedSorted) > 0 {
			expectedFiles = append(expectedFiles, "") // Uncommitted header
			expectedFiles = append(expectedFiles, uncommittedSorted...)
		}
		if len(committedSorted) > 0 {
			if len(uncommittedSorted) > 0 {
				expectedFiles = append(expectedFiles, "") // separator
			}
			expectedFiles = append(expectedFiles, "") // Committed header
			expectedFiles = append(expectedFiles, committedSorted...)
		}

		// Click on each visible file
		visibleCount := min(len(expectedFiles), height-statusBarHeight-2) // minus borders
		for i := 0; i < visibleCount; i++ {
			if expectedFiles[i] == "" {
				continue // skip header/separator
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

// ---------------------------------------------------------------------------
// New: multi-step interaction property test
// ---------------------------------------------------------------------------

func TestProperty_InteractionInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mock, mode := genScenario(t)
		width := rapid.IntRange(40, 200).Draw(t, "width")
		height := rapid.IntRange(10, 60).Draw(t, "height")

		m := initModel(mock, mode, width, height)

		// Check invariants after initial load
		checkAllInvariants(t, m, "after init")

		// Run random interactions
		nSteps := rapid.IntRange(1, 30).Draw(t, "nSteps")
		for step := range nSteps {
			// 20% chance of a special key (enter, tab, arrows, pgup/dn)
			var msg tea.Msg
			if rapid.Float64Range(0, 1).Draw(t, fmt.Sprintf("special%d", step)) < 0.2 {
				msg = genSpecialKey(t, fmt.Sprintf("step%d", step))
			} else {
				msg = genAction(t, m, step)
			}

			m = applyAction(m, msg)

			// If the model entered confirming or help, exit them so we keep
			// exercising the main UI. This avoids getting stuck.
			if m.confirming {
				m.confirming = false
			}
			if m.showHelp {
				m.showHelp = false
			}

			context := fmt.Sprintf("step %d (mode=%d, focus=%d)", step, m.mode, m.focus)
			checkAllInvariants(t, m, context)
		}
	})
}
