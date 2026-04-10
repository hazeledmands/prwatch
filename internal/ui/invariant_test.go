package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	runewidth "github.com/mattn/go-runewidth"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Scenario generators
// ---------------------------------------------------------------------------

// emojiPool contains emoji that have various display widths to stress-test rendering.
var emojiPool = []string{"🔥", "✅", "🚀", "💬", "⚡", "🐛", "📦", "🎉", "✨", "🤖", "❌", "⏳"}

// maybeEmoji returns the input string with an emoji occasionally appended (~30% chance).
func maybeEmoji(t *rapid.T, tag string, s string) string {
	if rapid.Float64Range(0, 1).Draw(t, tag+"_emoji") < 0.3 {
		e := rapid.SampledFrom(emojiPool).Draw(t, tag+"_emojiVal")
		return s + e
	}
	return s
}

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
			Subject: maybeEmoji(t, fmt.Sprintf("commit%d", i), fmt.Sprintf("commit message %d", i)),
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
			Title:   maybeEmoji(t, "prTitle", "Test PR"),
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
		fileDiff:    maybeEmoji(t, "fileDiff", "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new"),
		fileContent: maybeEmoji(t, "fileContent", "line1\nline2\nline3"),
		commitPatch: maybeEmoji(t, "commitPatch", "commit 0000000\n\n    msg\n\ndiff\n+added"),
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
				Author: maybeEmoji(t, fmt.Sprintf("reviewer%d", i), fmt.Sprintf("reviewer%d", i)),
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
				Author: maybeEmoji(t, fmt.Sprintf("commenter%d", i), fmt.Sprintf("commenter%d", i)),
				Body:   maybeEmoji(t, fmt.Sprintf("commentBody%d", i), fmt.Sprintf("comment body %d", i)),
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
				Subject: maybeEmoji(t, fmt.Sprintf("base%d", i), fmt.Sprintf("base commit %d", i)),
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

// viewWithTimeout calls m.View() but fails the test if rendering takes longer
// than 1 second, which indicates a hang in layout/width calculation.
func viewWithTimeout(t *rapid.T, m *Model, context string) tea.View {
	t.Helper()
	type result struct{ v tea.View }
	ch := make(chan result, 1)
	go func() {
		ch <- result{m.View()}
	}()
	select {
	case r := <-ch:
		return r.v
	case <-time.After(1 * time.Second):
		t.Fatalf("%s: View() hung for >1s (mode=%d, focus=%d, sidebarWidth=%d, width=%d, height=%d, files=%d, commits=%d)",
			context, m.mode, m.focus, m.sidebar.width, m.width, m.height,
			len(m.committedFiles)+len(m.uncommittedFiles), len(m.commits))
		return tea.View{} // unreachable
	}
}

// checkRenderInvariants renders the model and verifies that the output has
// exactly height lines, each exactly width display-cells wide, with no panics.
func checkRenderInvariants(t *rapid.T, m *Model, context string) {
	t.Helper()
	v := viewWithTimeout(t, m, context)
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
	v := viewWithTimeout(t, m, context)
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
// multi-byte UTF-8 characters, emoji, and East Asian wide characters.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\t' {
			w += 8 // tab stop
		} else {
			w += runewidth.RuneWidth(r)
		}
	}
	return w
}

// splitLineAtCols splits a line (which may contain ANSI escape codes) into
// three parts at display column boundaries: before fromCol, between fromCol
// and toCol, and after toCol. ANSI escape codes are preserved in whichever
// segment they appear in.
func splitLineAtCols(line string, fromCol, toCol int) (before, middle, after string) {
	col := 0
	fromByte := -1
	toByte := -1
	inEscape := false
	for i, r := range line {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			if fromByte < 0 && col >= fromCol {
				fromByte = i
			}
			if toByte < 0 && col >= toCol {
				toByte = i
			}
			continue
		}
		if fromByte < 0 && col >= fromCol {
			fromByte = i
		}
		if toByte < 0 && col >= toCol {
			toByte = i
		}
		col += runewidth.RuneWidth(r)
	}
	if fromByte < 0 {
		fromByte = len(line)
	}
	if toByte < 0 {
		toByte = len(line)
	}
	if fromByte > toByte {
		fromByte = toByte
	}
	return line[:fromByte], line[fromByte:toByte], line[toByte:]
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
		execSafeCmd(m, cmd)
	}
	return m
}

// execSafeCmd runs a command and feeds safe follow-up messages back into the
// model. It handles tea.BatchMsg by recursing into each sub-command. Commands
// that would do real I/O (or panic) are silently skipped.
func execSafeCmd(m *Model, cmd tea.Cmd) {
	func() {
		defer func() { recover() }()
		followUp := cmd()
		if followUp == nil {
			return
		}
		switch msg := followUp.(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				if sub != nil {
					execSafeCmd(m, sub)
				}
			}
		case moreCommitsMsg, gitDataMsg, prRefreshMsg, allFilesMsg:
			result, cmd2 := m.Update(msg)
			*m = *(result.(*Model))
			if cmd2 != nil {
				execSafeCmd(m, cmd2)
			}
		}
	}()
}

// applyTicks simulates periodic timer ticks (PR refresh + git refresh) firing
// and processes their results through the model. This exercises the same code
// path that runs between user interactions in the real UI.
//
// Rather than sending prTickMsg/gitTickMsg (which produce tea.Tick commands
// that block on real timers), we call the load functions directly and feed
// their results into Update.
//
// hasGit must be passed explicitly because m.git may hold a nil *mockGit inside
// a non-nil interface, which would pass an m.git == nil check.
func applyTicks(m *Model, hasGit bool) {
	if !hasGit {
		return
	}
	for _, msg := range []tea.Msg{m.loadPRStatus(), m.loadLocalGitData()} {
		result, cmd := m.Update(msg)
		*m = *(result.(*Model))
		if cmd != nil {
			execSafeCmd(m, cmd)
		}
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

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
			col := rapid.IntRange(1, max(1, m.sidebar.width)).Draw(t, fmt.Sprintf("col%d", i))

			clickMsg := tea.MouseClickMsg{
				X:      col,
				Y:      row,
				Button: tea.MouseLeft,
			}
			result, _ := m.Update(clickMsg)
			m = result.(*Model)

			selected := m.sidebar.SelectedItem()
			if selected != expectedFiles[i] {
				t.Fatalf("clicked row %d col %d (item %d), expected %q, got %q",
					row, col, i, expectedFiles[i], selected)
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
			expected = append(expected, expectedItem{isSeparator: true}) // header (non-selectable)
			expected = append(expected, expectedItem{
				label: "uncommitted changes",
			})
		}

		// Category 2: unpushed commits (dimmed)
		unpushedVisible := unpushed
		if unpushedVisible > len(mock.commits) {
			unpushedVisible = len(mock.commits)
		}
		if unpushedVisible > 0 {
			if len(expected) > 0 {
				expected = append(expected, expectedItem{isSeparator: true})
			}
			expected = append(expected, expectedItem{isSeparator: true}) // header
			for i := 0; i < unpushedVisible; i++ {
				c := mock.commits[i]
				expected = append(expected, expectedItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				})
			}
		}

		// Category 3: pushed commits
		pushedCount := len(mock.commits) - unpushed
		if pushedCount < 0 {
			pushedCount = 0
		}
		if pushedCount > 0 {
			if len(expected) > 0 {
				expected = append(expected, expectedItem{isSeparator: true})
			}
			expected = append(expected, expectedItem{isSeparator: true}) // header
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
			col := rapid.IntRange(1, max(1, m.sidebar.width)).Draw(t, fmt.Sprintf("col%d", i))
			clickMsg := tea.MouseClickMsg{
				X:      col,
				Y:      row,
				Button: tea.MouseLeft,
			}
			result, _ := m.Update(clickMsg)
			m = result.(*Model)

			selected := m.sidebar.SelectedItem()
			if selected != expected[i].label {
				t.Fatalf("clicked commit row %d col %d (item %d), expected %q, got %q",
					row, col, i, expected[i].label, selected)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Drag-to-copy property tests
// ---------------------------------------------------------------------------

// TestProperty_DragSelectsCorrectText verifies three invariants for drag
// selection in FileViewMode with plain content:
//  1. The copied text is a contiguous substring of the source content.
//  2. The first character copied matches the character at the drag start position.
//  3. The last character copied matches the character at the drag end position.
//
// Randomizes terminal size, line numbers on/off, word wrap on/off, and the
// drag start/end positions within the main pane content area.
func TestProperty_DragSelectsCorrectText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 160).Draw(t, "width")
		height := rapid.IntRange(15, 50).Draw(t, "height")
		lineNumbers := rapid.Bool().Draw(t, "lineNumbers")
		wordWrap := rapid.Bool().Draw(t, "wordWrap")

		nLines := rapid.IntRange(3, 20).Draw(t, "nLines")
		var srcLines []string
		for i := range nLines {
			srcLines = append(srcLines, fmt.Sprintf("source line %d with some content for testing", i+1))
		}
		srcContent := strings.Join(srcLines, "\n")

		mock := genMockGit(t)
		mock.fileContent = srcContent
		if len(mock.changedFiles.Committed) == 0 && len(mock.changedFiles.Uncommitted) == 0 {
			mock.changedFiles.Committed = []string{"file.go"}
		}

		m := initModel(mock, FileViewMode, width, height)
		m.mainPane.ClearDiffAnnotations()
		m.mainPane.SetLineNumbers(lineNumbers)
		m.mainPane.SetWordWrap(wordWrap)
		m.mainPane.SetPlainContent(srcContent)

		// Compute the screen region that contains actual content (after
		// status bar, borders, sidebar, and gutter).
		statusRows := m.statusBarLines()
		topBorder := 1
		sidebarW := m.sidebarPixelWidth()
		contentStartY := statusRows + topBorder
		contentStartX := sidebarW + 1 // +1 for main pane left border
		gw := m.mainPane.gutterWidth

		// Get the viewport lines (ANSI-stripped, gutter-stripped, trimmed)
		// to determine the valid drag area and expected characters.
		vpContent := m.mainPane.viewport.View()
		vpLines := strings.Split(vpContent, "\n")
		var contentRows int
		for _, vl := range vpLines {
			stripped := stripANSIForWidth(vl)
			stripped = strings.TrimRight(stripped, " ")
			if gw > 0 && len(stripped) > gw {
				stripped = stripped[gw:]
			}
			if stripped != "" {
				contentRows++
			}
		}
		if contentRows == 0 {
			return
		}

		// Visible content area bounds (screen coords, after gutter)
		minX := contentStartX + gw
		maxX := width - 2 // right border
		if maxX <= minX {
			return
		}
		visibleRows := min(len(vpLines), height-statusRows-2)
		if visibleRows <= 0 {
			return
		}
		minY := contentStartY
		maxY := contentStartY + visibleRows - 1

		// Pick random drag start and end within the content area
		y1 := rapid.IntRange(minY, maxY).Draw(t, "y1")
		y2 := rapid.IntRange(y1, maxY).Draw(t, "y2")
		x1 := rapid.IntRange(minX, maxX).Draw(t, "x1")
		x2 := rapid.IntRange(minX, maxX).Draw(t, "x2")
		// For single-line selection, ensure we drag left-to-right
		if y1 == y2 && x1 > x2 {
			x1, x2 = x2, x1
		}

		// Render without drag to capture the baseline view.
		m.dragging = false
		baseView := viewWithTimeout(t, m, "baseline")

		m.dragStartX = x1
		m.dragStartY = y1
		m.dragEndX = x2
		m.dragEndY = y2
		m.dragging = true

		got := m.selectedText()
		if got == "" {
			return // drag over empty/padding area
		}

		// Invariant 0: the characters that applyDragHighlight would visually
		// select match what selectedText() returns. We replicate the
		// highlight's coordinate logic against the full rendered view to
		// compute which content characters would be highlighted, then
		// compare against selectedText(). This verifies that the highlight
		// and copy paths agree on the selection boundaries.
		v := viewWithTimeout(t, m, "drag highlight")
		renderedLines := strings.Split(v.Content, "\n")
		gutterOffset := contentStartX + gw // screen column where content starts

		hlStartY, hlEndY := y1, y2
		hlStartX, hlEndX := x1, x2
		if hlStartY > hlEndY || (hlStartY == hlEndY && hlStartX > hlEndX) {
			hlStartY, hlEndY = hlEndY, hlStartY
			hlStartX, hlEndX = hlEndX, hlStartX
		}
		if hlStartX < gutterOffset {
			hlStartX = gutterOffset
		}

		contMap := m.mainPane.wrapContinuation
		vpOffset := m.mainPane.viewport.YOffset()
		var hlText strings.Builder
		for row := hlStartY; row <= hlEndY && row < len(renderedLines); row++ {
			// Use viewport content (not the full rendered line) to avoid
			// multibyte sidebar border characters.
			vpRow := row - contentStartY
			if vpRow < 0 || vpRow >= len(vpLines) {
				continue
			}
			line := stripANSIForWidth(vpLines[vpRow])
			line = strings.TrimRight(line, " ")
			if gw > 0 && len(line) > gw {
				line = line[gw:]
			}

			fromX := 0
			toX := len(line)
			if row == hlStartY {
				fromX = max(0, hlStartX-gutterOffset)
			}
			if row == hlEndY {
				toX = min(len(line), hlEndX+1-gutterOffset)
			}
			if fromX > len(line) {
				fromX = len(line)
			}
			if fromX < toX {
				hlText.WriteString(line[fromX:toX])
			}
			if row < hlEndY {
				absY := vpRow + vpOffset
				nextAbsY := absY + 1
				if contMap != nil && nextAbsY < len(contMap) && contMap[nextAbsY] {
					continue
				}
				hlText.WriteString("\n")
			}
		}

		// Compare with trailing spaces stripped
		var hlLines []string
		for _, hl := range strings.Split(hlText.String(), "\n") {
			hlLines = append(hlLines, strings.TrimRight(hl, " "))
		}
		var gotStripped []string
		for _, gl := range strings.Split(got, "\n") {
			gotStripped = append(gotStripped, strings.TrimRight(gl, " "))
		}
		hlJoined := strings.Join(hlLines, "\n")
		gotJoined := strings.Join(gotStripped, "\n")
		if hlJoined != gotJoined {
			t.Fatalf("highlight/selection mismatch:\n  highlight: %q\n  selectedText: %q\n  wrap=%v lineNums=%v drag=(%d,%d)->(%d,%d)",
				hlJoined, gotJoined, wordWrap, lineNumbers, x1, y1, x2, y2)
		}

		// Helper: get the gutter-stripped, ANSI-stripped character at a
		// screen position from the viewport content.
		charAt := func(screenX, screenY int) (rune, bool) {
			row := screenY - contentStartY
			if row < 0 || row >= len(vpLines) {
				return 0, false
			}
			line := stripANSIForWidth(vpLines[row])
			line = strings.TrimRight(line, " ")
			col := screenX - contentStartX
			if col < 0 || col >= len(line) {
				return 0, false
			}
			runes := []rune(line[gw:])
			charCol := col - gw
			if charCol < 0 || charCol >= len(runes) {
				return 0, false
			}
			return runes[charCol], true
		}

		// Invariant 1: every logical line in the copied text is a subsequence
		// of some source line. selectedText() only emits \n at true
		// source-line boundaries, so each piece between \n's originates
		// from one source line. Word wrap can consume whitespace at break
		// points (e.g. "for " + "testing" displays as "for" / "testing",
		// and selecting across the break gives "fort" with the space lost).
		// Truncation can shorten lines. So we check that each character of
		// the selected line appears in order in some source line (i.e. it's
		// a subsequence), allowing for dropped whitespace at wrap points.
		isSubseq := func(haystack, needle string) bool {
			h := []rune(haystack)
			hi := 0
			for _, r := range needle {
				found := false
				for hi < len(h) {
					if h[hi] == r {
						hi++
						found = true
						break
					}
					hi++
				}
				if !found {
					return false
				}
			}
			return true
		}
		for _, gotLine := range strings.Split(got, "\n") {
			gotLine = strings.TrimRight(gotLine, " ")
			if gotLine == "" {
				continue
			}
			found := false
			for _, sl := range srcLines {
				if isSubseq(sl, gotLine) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("selectedText() line %q is not a subsequence of any source line\nfull: %q\nwrap=%v lineNums=%v gw=%d drag=(%d,%d)->(%d,%d)",
					gotLine, got, wordWrap, lineNumbers, gw, x1, y1, x2, y2)
			}
		}

		// Invariant 2: first character matches drag start position
		gotRunes := []rune(got)
		if startChar, ok := charAt(x1, y1); ok {
			if gotRunes[0] != startChar {
				t.Fatalf("first char: selectedText() starts with %q, but screen position (%d,%d) has %q",
					string(gotRunes[0]), x1, y1, string(startChar))
			}
		}

		// Invariant 3: last character matches drag end position
		if endChar, ok := charAt(x2, y2); ok {
			lastGot := gotRunes[len(gotRunes)-1]
			// The last rune might be a newline if we selected to end of line;
			// in that case compare against the last non-newline rune.
			if lastGot == '\n' && len(gotRunes) > 1 {
				lastGot = gotRunes[len(gotRunes)-2]
			}
			if lastGot != endChar {
				t.Fatalf("last char: selectedText() ends with %q, but screen position (%d,%d) has %q",
					string(lastGot), x2, y2, string(endChar))
			}
		}

		// Invariant 4: the drag highlight must not produce invalid UTF-8.
		// applyDragHighlight does byte-level slicing which can split
		// multibyte characters (like box-drawing │) when ANSI escapes are
		// inserted mid-character. Check that every non-ANSI segment in the
		// rendered output is valid UTF-8.
		dragView := viewWithTimeout(t, m, "drag view")
		for row, line := range strings.Split(dragView.Content, "\n") {
			segments := ansiRE.Split(line, -1)
			for _, seg := range segments {
				if !utf8.ValidString(seg) {
					t.Fatalf("drag highlight produced invalid UTF-8 on line %d: segment %q\n  full line: %q\n  wrap=%v lineNums=%v drag=(%d,%d)->(%d,%d)",
						row, seg, line, wordWrap, lineNumbers, x1, y1, x2, y2)
				}
			}
		}

		// Invariant 5: the rest of the UI is unchanged by the highlight,
		// including ANSI styling (colors, bold, etc.). Compare the raw
		// rendered output (with ANSI codes) for regions outside the
		// highlighted columns. This catches applyDragHighlight stripping
		// ANSI codes from non-highlighted portions of highlighted lines.
		baseRawLines := strings.Split(baseView.Content, "\n")
		dragRawLines := strings.Split(dragView.Content, "\n")

		if len(baseRawLines) != len(dragRawLines) {
			t.Fatalf("drag changed line count: %d vs %d", len(baseRawLines), len(dragRawLines))
		}
		for row := range baseRawLines {
			if row < hlStartY || row > hlEndY {
				if baseRawLines[row] != dragRawLines[row] {
					t.Fatalf("drag changed non-highlighted line %d:\n  base: %q\n  drag: %q",
						row, baseRawLines[row], dragRawLines[row])
				}
			} else {
				// Line overlaps the highlight — split at the highlight
				// column boundaries and compare the before/after portions
				// which should retain their original ANSI codes.
				fromCol := 0
				toCol := m.width
				if row == hlStartY {
					fromCol = hlStartX
				}
				if row == hlEndY {
					toCol = hlEndX + 1
				}
				baseBefore, _, baseAfter := splitLineAtCols(baseRawLines[row], fromCol, toCol)
				dragBefore, _, dragAfter := splitLineAtCols(dragRawLines[row], fromCol, toCol)
				if baseBefore != dragBefore {
					t.Fatalf("drag changed styling before highlight on line %d:\n  base: %q\n  drag: %q",
						row, baseBefore, dragBefore)
				}
				// The drag's "after" may have a leading \x1b[27m
				// (reverse-off) from the highlight — strip it before
				// comparing since it's visually neutral.
				dragAfter = strings.TrimPrefix(dragAfter, "\x1b[27m")
				if baseAfter != dragAfter {
					t.Fatalf("drag changed styling after highlight on line %d:\n  base: %q\n  drag: %q",
						row, baseAfter, dragAfter)
				}
			}
		}
	})
}

// TestProperty_DragAcrossModesNoPanic verifies that drag selection never panics
// regardless of mode, scroll position, or drag coordinates. Also checks that
// selectedText() only contains characters present in the viewport content.
func TestProperty_DragAcrossModesNoPanic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mock, mode := genScenario(t)
		width := rapid.IntRange(40, 200).Draw(t, "width")
		height := rapid.IntRange(10, 60).Draw(t, "height")

		m := initModel(mock, mode, width, height)

		// Random drag coordinates anywhere on screen
		y1 := rapid.IntRange(0, height-1).Draw(t, "y1")
		y2 := rapid.IntRange(0, height-1).Draw(t, "y2")
		x1 := rapid.IntRange(0, width-1).Draw(t, "x1")
		x2 := rapid.IntRange(0, width-1).Draw(t, "x2")

		m.dragStartX = x1
		m.dragStartY = y1
		m.dragEndX = x2
		m.dragEndY = y2
		m.dragging = true

		// Should not panic
		text := m.selectedText()

		// If we got text, every line should be a substring of some viewport line
		if text != "" {
			v := viewWithTimeout(t, m, "drag")
			stripped := stripANSI(v.Content)
			viewLines := strings.Split(stripped, "\n")
			// Build a set of all characters in the viewport
			viewChars := make(map[rune]bool)
			for _, vl := range viewLines {
				for _, r := range vl {
					viewChars[r] = true
				}
			}
			for _, r := range text {
				if r != '\n' && !viewChars[r] {
					t.Fatalf("selectedText() contains character %q not in viewport (mode=%d)",
						string(r), mode)
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Multi-step interaction property test
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
			checkAllInvariants(t, m, context+" after action")

			// Capture sidebar scroll state before ticks
			offsetBefore := m.sidebar.offset
			selectedBefore := m.sidebar.selected

			// Simulate periodic refresh ticks firing between user interactions
			applyTicks(m, mock != nil)
			checkAllInvariants(t, m, context+" after ticks")

			// Tick refreshes should not move the sidebar scroll position
			// when the selection hasn't changed. This is the "jump to
			// selected" bug: periodic refreshes call updateSidebarItems →
			// SetItems → clampOffset, which snaps the viewport back to the
			// selected item even though the user scrolled away.
			if m.sidebar.selected == selectedBefore && m.sidebar.offset != offsetBefore {
				t.Fatalf("%s: sidebar offset changed from %d to %d after tick (selected=%d, items=%d)",
					context, offsetBefore, m.sidebar.offset, m.sidebar.selected, len(m.sidebar.items))
			}
		}
	})
}
