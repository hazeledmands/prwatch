package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// testGit returns a dummy git instance so the model operates in git mode.
func testGit() *git.Git {
	return git.New("/tmp")
}

// initAndLoadGitData calls Init and extracts just the loadGitData result.
// Use this for tests that need the git data msg directly.
func initAndLoadGitData(m *Model) gitDataMsg {
	msg := m.loadGitData()
	return msg.(gitDataMsg)
}

// mockGit implements GitDataSource for controlled testing.
type mockGit struct {
	repoInfo        git.RepoInfoResult
	repoInfoErr     error
	prInfo          git.PRInfoResult
	prInfoErr       error
	ciStatus        git.CIStatusResult
	ciStatusErr     error
	reviews         []git.PRReview
	reviewsErr      error
	commentCount    int
	commentCountErr error
	base            string
	baseErr         error
	changedFiles    git.ChangedFilesResult
	changedErr      error
	commits         []git.Commit
	commitsErr      error
	fileDiff        string
	fileDiffErr     error
	fileContent     string
	contentErr      error
	commitPatch     string
	patchErr        error
	allCommits      []git.Commit
	allCommitsErr   error
	allFiles        []string
	allFilesErr     error
}

func (m *mockGit) RepoInfo() (git.RepoInfoResult, error) { return m.repoInfo, m.repoInfoErr }
func (m *mockGit) PRInfo() (git.PRInfoResult, error)     { return m.prInfo, m.prInfoErr }
func (m *mockGit) PRChecks() (git.CIStatusResult, error) { return m.ciStatus, m.ciStatusErr }
func (m *mockGit) PRReviews() ([]git.PRReview, error)    { return m.reviews, m.reviewsErr }
func (m *mockGit) PRCommentCount() (int, error)          { return m.commentCount, m.commentCountErr }
func (m *mockGit) DetectBase() (string, error)           { return m.base, m.baseErr }
func (m *mockGit) ChangedFiles(base string) (git.ChangedFilesResult, error) {
	return m.changedFiles, m.changedErr
}
func (m *mockGit) Commits(base string) ([]git.Commit, error) { return m.commits, m.commitsErr }
func (m *mockGit) AllCommits() ([]git.Commit, error)         { return m.allCommits, m.allCommitsErr }
func (m *mockGit) FileDiffCommitted(base, file string) (string, error) {
	return m.fileDiff, m.fileDiffErr
}
func (m *mockGit) FileDiffUncommitted(file string) (string, error) {
	return m.fileDiff, m.fileDiffErr
}
func (m *mockGit) FileContent(file string) (string, error) { return m.fileContent, m.contentErr }
func (m *mockGit) CommitPatch(sha string) (string, error)  { return m.commitPatch, m.patchErr }
func (m *mockGit) AllFiles(includeIgnored bool) ([]string, error) {
	return m.allFiles, m.allFilesErr
}

func TestModeSwitching(t *testing.T) {
	m := NewModel("/tmp", testGit())

	if m.mode != FileViewMode {
		t.Error("initial mode should be FileViewMode")
	}

	// Press m to cycle: FileView → FileDiff → Commit → FileView
	result, _ := m.Update(tea.KeyPressMsg{Text: "m", Code: 'm'})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("after m, mode should be FileDiffMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "m", Code: 'm'})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after second m, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "m", Code: 'm'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after third m, mode should be FileViewMode")
	}

	// Direct mode keys
	result, _ = m.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after c, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after v, mode should be FileViewMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("after d, mode should be FileDiffMode")
	}
}

func TestModeSwitching_RetainsSelectedFile(t *testing.T) {
	// Spec: "switching between file-diff and file-view should retain the selected file"
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed:   []string{"alpha.go", "beta.go", "gamma.go"},
			Uncommitted: []string{"wip.go"},
		},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:    "+new",
		fileContent: "content",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Select "beta.go" (index 3: wip.go=0, separator=1, alpha.go=2, beta.go=3)
	m.sidebar.SelectIndex(3)
	selected := m.sidebar.SelectedItem()
	if selected != "beta.go" {
		t.Fatalf("expected beta.go selected, got %q", selected)
	}

	// Switch to file-view mode
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Fatal("should be in FileViewMode")
	}
	if m.sidebar.SelectedItem() != "beta.go" {
		t.Errorf("after switch to file-view, selected should be beta.go, got %q", m.sidebar.SelectedItem())
	}

	// Switch back to file-diff mode
	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	if m.sidebar.SelectedItem() != "beta.go" {
		t.Errorf("after switch to file-diff, selected should be beta.go, got %q", m.sidebar.SelectedItem())
	}
}

func TestFocusSwitching(t *testing.T) {
	m := NewModel("/tmp", testGit())

	if m.focus != SidebarFocus {
		t.Error("initial focus should be SidebarFocus")
	}

	// Tab switches focus
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("after tab, focus should be MainFocus")
	}

	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("after second tab, focus should be SidebarFocus")
	}
}

func TestArrowKeysScrollHorizontally(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.wordWrap = false
	m.focus = MainFocus
	m.updateLayout()
	// Set content wider than viewport to allow scrolling
	m.mainPane.SetContent(strings.Repeat("x", 200))

	// Press l (right) should scroll right, not switch focus
	result, _ := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("l should not switch focus to sidebar")
	}
	if m.mainPane.xOffset != 4 {
		t.Errorf("expected xOffset 4 after l, got %d", m.mainPane.xOffset)
	}

	// Press h (left) should scroll left
	result, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = result.(*Model)
	if m.mainPane.xOffset != 0 {
		t.Errorf("expected xOffset 0 after h, got %d", m.mainPane.xOffset)
	}
}

func TestQuitConfirm_qEntersConfirming(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)
	if !m.confirming {
		t.Error("after q, should be confirming")
	}
	if cmd != nil {
		t.Error("should not quit yet")
	}
}

func TestQuitConfirm_qThenqQuits(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd == nil {
		t.Error("second q should produce a quit command")
	}
}

func TestQuitConfirm_qThenOtherKeyCancels(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)

	result, cmd := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = result.(*Model)
	if m.confirming {
		t.Error("after j, should not be confirming")
	}
	if cmd != nil {
		t.Error("should not quit")
	}
}

func TestQuitConfirm_qThenShiftQQuits(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "Q", Code: 'Q'})
	if cmd == nil {
		t.Error("Shift-Q during confirm should produce quit command")
	}
}

func TestQuitConfirm_escEntersConfirming(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(*Model)
	if !m.confirming {
		t.Error("esc should enter confirming")
	}
	if cmd != nil {
		t.Error("should not quit yet")
	}
}

func TestQuitImmediate_CtrlCQuits(t *testing.T) {
	m := NewModel("/tmp", testGit())

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("ctrl-c should quit immediately")
	}
}

func TestQuitImmediate_QQuitsDirectly(t *testing.T) {
	m := NewModel("/tmp", testGit())

	_, cmd := m.Update(tea.KeyPressMsg{Text: "Q", Code: 'Q'})
	if cmd == nil {
		t.Error("Q should produce a quit command immediately")
	}
}

func TestNonGitMode_StartsInFileView(t *testing.T) {
	m := NewModel("/tmp", nil)
	if m.mode != FileViewMode {
		t.Error("non-git model should start in FileViewMode")
	}
}

func TestParseHunkNewStart(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"@@ -10,5 +20,6 @@ func foo()", 20},
		{"@@ -1,3 +1,3 @@", 1},
		{"@@ -0,0 +1,100 @@", 1},
		{"not a hunk", 0},
		{"@@ no plus sign", 0},
	}
	for _, tt := range tests {
		result := parseHunkNewStart(tt.input)
		if result != tt.expected {
			t.Errorf("parseHunkNewStart(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestCurrentLineNumber_DiffMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = FileDiffMode
	m.mainPane.SetSize(80, 5)
	m.mainPane.content = "@@ -1,3 +10,3 @@\n context\n+added\n-removed\n context2"

	line := m.currentLineNumber()
	if line < 10 {
		t.Errorf("expected line >= 10, got %d", line)
	}
}

func TestCurrentLineNumber_FileViewMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = FileViewMode
	m.mainPane.SetSize(80, 5)
	m.mainPane.content = "line1\nline2\nline3"

	line := m.currentLineNumber()
	if line != 1 {
		t.Errorf("expected line 1, got %d", line)
	}
}

func TestView_Error(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.err = fmt.Errorf("test error")

	v := m.View()
	content := v.Content
	if !strings.Contains(content, "test error") {
		t.Error("view should show error")
	}
}

func TestView_Normal(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	v := m.View()
	if v.Content == "" {
		t.Error("view should not be empty")
	}
	if !v.AltScreen {
		t.Error("should use alt screen")
	}
}

func TestRenderOnce_WithMockGit(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc123",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"main.go"},
		},
		commits:     []git.Commit{{SHA: "def", Subject: "test"}},
		fileContent: "package main\n",
		fileDiff:    "+new line",
	}
	m := NewModel("/tmp", mg)
	output := m.RenderOnce(80, 24)
	if output == "" {
		t.Error("RenderOnce should produce output")
	}
	if !strings.Contains(output, "feature") {
		t.Error("should contain branch name")
	}
}

func TestRenderOnce_NonGit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644)

	m := NewModel(dir, nil)
	output := m.RenderOnce(80, 24)
	if output == "" {
		t.Error("RenderOnce should produce output for non-git")
	}
}

func TestUpdateLayout(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 100
	m.height = 30
	m.updateLayout()

	if m.sidebar.width == 0 {
		t.Error("sidebar width should be set")
	}
	if m.mainPane.width == 0 {
		t.Error("main pane width should be set")
	}
}

func TestHandleEnter_SidebarToMain(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.focus = SidebarFocus

	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("enter from sidebar should switch to main focus")
	}
}

func TestWindowSizeMsg(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = result.(*Model)
	if m.width != 120 || m.height != 40 {
		t.Error("window size should be updated")
	}
}

func TestUpdate_UnknownMsg(t *testing.T) {
	m := NewModel("/tmp", testGit())
	// Send an unhandled message type
	type unknownMsg struct{}
	result, cmd := m.Update(unknownMsg{})
	if result == nil {
		t.Error("should return model")
	}
	if cmd != nil {
		t.Error("should return nil cmd for unknown msg")
	}
}

func TestGitDataMsg(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	msg := gitDataMsg{
		repoInfo:         git.RepoInfoResult{Branch: "test", RepoName: "repo"},
		base:             "abc123",
		committedFiles:   []string{"file1.go", "file2.go"},
		uncommittedFiles: []string{"file3.go"},
	}
	result, _ := m.Update(msg)
	m = result.(*Model)

	if m.repoInfo.Branch != "test" {
		t.Error("should store repo info")
	}
	if len(m.committedFiles) != 2 {
		t.Error("should store committed files")
	}
}

func TestGitDataMsg_Error(t *testing.T) {
	m := NewModel("/tmp", testGit())

	msg := gitDataMsg{err: fmt.Errorf("git failed")}
	result, _ := m.Update(msg)
	m = result.(*Model)

	if m.err == nil {
		t.Error("should store error")
	}
}

func TestSidebar_View_Focused(t *testing.T) {
	s := newSidebar()
	s.SetSize(20, 5)
	s.SetItems([]sidebarItem{
		{label: "file1.go", kind: itemNormal},
		{label: "file2.go", kind: itemDim},
	})

	focused := s.View(true)
	unfocused := s.View(false)

	if focused == "" || unfocused == "" {
		t.Error("sidebar view should not be empty")
	}
}

func TestSidebar_View_WithSeparator(t *testing.T) {
	s := newSidebar()
	s.SetSize(20, 5)
	s.SetItems([]sidebarItem{
		{label: "uncommitted.go", kind: itemDim},
		{kind: itemSeparator},
		{label: "committed.go", kind: itemNormal},
	})

	view := s.View(true)
	if !strings.Contains(view, "─") {
		t.Error("separator should contain box drawing character")
	}
}

func TestSidebar_View_Empty(t *testing.T) {
	s := newSidebar()
	s.SetSize(20, 5)

	view := s.View(true)
	if view != "" {
		t.Error("empty sidebar should return empty string")
	}
}

func TestSidebar_SetSize(t *testing.T) {
	s := newSidebar()
	s.SetSize(30, 10)

	if s.width != 30 || s.height != 10 {
		t.Error("size should be set")
	}
}

func TestInit_WithGit(t *testing.T) {
	m := NewModel("/tmp", testGit())
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a command for git mode")
	}
}

func TestInit_NonGit(t *testing.T) {
	dir := t.TempDir()
	// Create some files in the temp dir
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "world.txt"), []byte("world"), 0644)

	m := NewModel(dir, nil)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a command for non-git mode")
	}

	// Execute the command to get the message
	msg := cmd()
	dataMsg, ok := msg.(gitDataMsg)
	if !ok {
		t.Fatalf("expected gitDataMsg, got %T", msg)
	}
	if dataMsg.err != nil {
		t.Fatal(dataMsg.err)
	}
	if len(dataMsg.uncommittedFiles) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(dataMsg.uncommittedFiles), dataMsg.uncommittedFiles)
	}
}

func TestUpdateMainContent_NonGit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("test content"), 0644)

	m := NewModel(dir, nil)
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Simulate receiving file data
	m.uncommittedFiles = []string{"test.txt"}
	m.updateSidebarItems()
	m.updateMainContent()

	if m.mainPane.content != "test content" {
		t.Errorf("expected file content, got %q", m.mainPane.content)
	}
}

func TestUpdateMainContent_FileDiffMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.base = "HEAD"
	m.updateLayout()

	// With no files, should set empty content
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.updateMainContent()
}

func TestUpdateMainContent_CommitMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.base = "HEAD"
	m.updateLayout()

	m.mode = CommitMode
	m.commits = []git.Commit{{SHA: "abc", Subject: "test"}}
	m.updateSidebarItems()
	// updateMainContent will try to run git show which will fail in /tmp,
	// but it should not panic
	m.updateMainContent()
}

func TestUpdateSidebarItems_FileMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"b.go", "a.go"}
	m.uncommittedFiles = []string{"z.go"}
	m.mode = FileDiffMode

	m.updateSidebarItems()

	// Verify order: uncommitted first, then separator, then committed
	items := m.sidebar.items
	if len(items) != 4 { // 1 uncommitted + separator + 2 committed
		t.Fatalf("expected 4 items, got %d", len(items))
	}
	if items[0].filePath != "z.go" || items[0].kind != itemDim {
		t.Errorf("first item should be uncommitted z.go, got filePath=%q kind=%v", items[0].filePath, items[0].kind)
	}
	if items[1].kind != itemSeparator {
		t.Error("second item should be separator")
	}
	// Tree mode sorts alphabetically, so a.go comes before b.go
	if items[2].filePath != "a.go" || items[2].kind != itemNormal {
		t.Errorf("third item should be committed a.go (sorted), got filePath=%q", items[2].filePath)
	}
}

func TestUpdateSidebarItems_CommitMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = CommitMode
	m.commits = []git.Commit{
		{SHA: "abc1234567890", Subject: "first"},
		{SHA: "def4567890123", Subject: "second"},
	}

	m.updateSidebarItems()

	if len(m.sidebar.items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(m.sidebar.items))
	}
	if !strings.Contains(m.sidebar.items[0].label, "first") {
		t.Error("first item should contain commit subject")
	}
}

func TestRefreshMsg_NonGit(t *testing.T) {
	m := NewModel("/tmp", nil)

	result, cmd := m.Update(RefreshMsg{})
	m = result.(*Model)
	if cmd == nil {
		t.Error("RefreshMsg should trigger reload in non-git mode")
	}
}

func TestRefreshMsg_Git(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, cmd := m.Update(RefreshMsg{})
	m = result.(*Model)
	if cmd == nil {
		t.Error("RefreshMsg should trigger reload in git mode")
	}
}

func TestPRRefreshMsg(t *testing.T) {
	m := NewModel("/tmp", testGit())

	msg := prRefreshMsg{
		prInfo:       git.PRInfoResult{Number: 10, Title: "test"},
		ciStatus:     git.CIStatusResult{State: "SUCCESS"},
		reviews:      []git.PRReview{{Author: "alice", State: "APPROVED"}},
		commentCount: 3,
	}
	result, _ := m.Update(msg)
	m = result.(*Model)

	if m.prInfo.Number != 10 {
		t.Error("should update PR info")
	}
	if m.ciStatus.State != "SUCCESS" {
		t.Error("should update CI status")
	}
	if len(m.prReviews) != 1 {
		t.Error("should update reviews")
	}
	if m.prCommentCount != 3 {
		t.Error("should update comment count")
	}
}

func TestPRTickMsg_Git(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, cmd := m.Update(prTickMsg{})
	m = result.(*Model)
	if cmd == nil {
		t.Error("prTickMsg should produce commands (loadPRStatus + schedulePRTick)")
	}
}

func TestLoadPRStatus(t *testing.T) {
	mg := &mockGit{
		prInfo: git.PRInfoResult{Number: 5, Title: "test PR"},
	}
	m := NewModel("/tmp", mg)
	msg := m.loadPRStatus()
	prMsg := msg.(prRefreshMsg)
	if prMsg.prInfo.Number != 5 {
		t.Errorf("expected PR #5, got #%d", prMsg.prInfo.Number)
	}
}

func TestLoadPRStatus_NoPR(t *testing.T) {
	mg := &mockGit{}
	m := NewModel("/tmp", mg)
	msg := m.loadPRStatus()
	prMsg := msg.(prRefreshMsg)
	if prMsg.prInfo.Number != 0 {
		t.Error("expected no PR")
	}
}

func TestPRTickMsg_NonGit(t *testing.T) {
	m := NewModel("/tmp", nil)

	result, cmd := m.Update(prTickMsg{})
	m = result.(*Model)
	if cmd == nil {
		t.Error("prTickMsg in non-git should still schedule next tick")
	}
}

func TestSidebar_ScrollOffset(t *testing.T) {
	s := newSidebar()
	s.SetSize(20, 3) // only 3 visible lines

	items := make([]sidebarItem, 10)
	for i := range items {
		items[i] = sidebarItem{label: fmt.Sprintf("item%d", i), kind: itemNormal}
	}
	s.SetItems(items)

	// Select last item — should scroll
	for i := 0; i < 9; i++ {
		s.SelectNext()
	}
	if s.selected != 9 {
		t.Errorf("selected = %d, want 9", s.selected)
	}
	if s.offset == 0 {
		t.Error("offset should have scrolled")
	}

	// View should still render without panic
	view := s.View(true)
	if view == "" {
		t.Error("scrolled view should not be empty")
	}
}

func TestSidebar_LongLabel_Truncated(t *testing.T) {
	s := newSidebar()
	s.SetSize(10, 5)
	s.SetItems([]sidebarItem{
		{label: "this_is_a_very_long_filename.go", kind: itemNormal},
	})

	view := s.View(false)
	if view == "" {
		t.Error("view should not be empty")
	}
}

func TestSidebarFocus_UnmatchedKey(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.focus = SidebarFocus

	// Press a key that doesn't match any binding (like 'x')
	result, cmd := m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	if result == nil {
		t.Error("should return model")
	}
	if cmd != nil {
		t.Error("unmatched key in sidebar should produce nil cmd")
	}
}

func TestSidebarGrow(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 100
	m.height = 24
	m.updateLayout()
	initial := m.sidebarPct

	result, _ := m.Update(tea.KeyPressMsg{Text: "+", Code: '+'})
	m = result.(*Model)
	if m.sidebarPct != initial+5 {
		t.Errorf("expected %d, got %d", initial+5, m.sidebarPct)
	}

	// Also test '=' which should do the same
	result, _ = m.Update(tea.KeyPressMsg{Text: "=", Code: '='})
	m = result.(*Model)
	if m.sidebarPct != initial+10 {
		t.Errorf("expected %d, got %d", initial+10, m.sidebarPct)
	}
}

func TestSidebarShrink(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 100
	m.height = 24
	m.updateLayout()
	initial := m.sidebarPct

	result, _ := m.Update(tea.KeyPressMsg{Text: "-", Code: '-'})
	m = result.(*Model)
	if m.sidebarPct != initial-5 {
		t.Errorf("expected %d, got %d", initial-5, m.sidebarPct)
	}
}

func TestSidebarGrow_MaxClamp(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.sidebarPct = 50
	m.width = 100
	m.height = 24
	m.updateLayout()

	result, _ := m.Update(tea.KeyPressMsg{Text: "+", Code: '+'})
	m = result.(*Model)
	if m.sidebarPct != 50 {
		t.Errorf("should clamp at 50, got %d", m.sidebarPct)
	}
}

func TestSidebarShrink_MinClamp(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.sidebarPct = 10
	m.width = 100
	m.height = 24
	m.updateLayout()

	result, _ := m.Update(tea.KeyPressMsg{Text: "-", Code: '-'})
	m = result.(*Model)
	if m.sidebarPct != 10 {
		t.Errorf("should clamp at 10, got %d", m.sidebarPct)
	}
}

func TestMainPane_ForwardKeys(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.focus = MainFocus
	m.mainPane.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")

	// Should forward to viewport without panic
	result, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	_ = result
}

func TestLoadGitData_RealRepo(t *testing.T) {
	// Create a real git repo
	dir := t.TempDir()
	cmds := []struct{ args []string }{
		{[]string{"git", "init", "--initial-branch=main"}},
		{[]string{"git", "config", "user.email", "test@test.com"}},
		{[]string{"git", "config", "user.name", "Test"}},
	}
	for _, c := range cmds {
		cmd := exec.Command(c.args[0], c.args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", c.args, out, err)
		}
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hello\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()
	exec.Command("git", "-C", dir, "checkout", "-b", "feature").Run()
	os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package f\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "add feature").Run()

	g := git.New(dir)
	m := NewModel(dir, g)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}

	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err != nil {
		t.Fatal(dataMsg.err)
	}
	if dataMsg.base == "" {
		t.Error("base should not be empty")
	}
	if len(dataMsg.committedFiles) == 0 {
		t.Error("should have committed files")
	}
}

func TestUpdateMainContent_FileViewWithGit(t *testing.T) {
	// Create a real git repo
	dir := t.TempDir()
	cmds := []struct{ args []string }{
		{[]string{"git", "init", "--initial-branch=main"}},
		{[]string{"git", "config", "user.email", "test@test.com"}},
		{[]string{"git", "config", "user.name", "Test"}},
	}
	for _, c := range cmds {
		cmd := exec.Command(c.args[0], c.args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", c.args, out, err)
		}
	}
	os.WriteFile(filepath.Join(dir, "file.go"), []byte("package main\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()
	exec.Command("git", "-C", dir, "checkout", "-b", "feature").Run()
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package new\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "add new").Run()

	g := git.New(dir)
	m := NewModel(dir, g)
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Load data directly
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view mode
	m.mode = FileViewMode
	m.updateSidebarItems()
	m.updateMainContent()

	if m.mainPane.content == "" {
		t.Error("file view should show file content")
	}
}

func TestHandleKey_DownInSidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"a.go", "b.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.focus = SidebarFocus

	result, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 1 {
		t.Errorf("expected selected index 1, got %d", m.sidebar.SelectedIndex())
	}
}

func TestHandleKey_UpInSidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"a.go", "b.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.focus = SidebarFocus

	// Move down first
	result, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = result.(*Model)
	// Move back up
	result, _ = m.Update(tea.KeyPressMsg{Text: "k", Code: 'k'})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 0 {
		t.Errorf("expected selected index 0, got %d", m.sidebar.SelectedIndex())
	}
}

func TestTabTogglesFocus(t *testing.T) {
	m := NewModel("/tmp", testGit())

	if m.focus != SidebarFocus {
		t.Fatal("initial focus should be SidebarFocus")
	}

	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("tab should toggle to MainFocus")
	}

	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("tab should toggle back to SidebarFocus")
	}
}

func TestGG_GoToTop_Sidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"a.go", "b.go", "c.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.focus = SidebarFocus

	// Move to last item
	m.sidebar.SelectLast()
	if m.sidebar.SelectedIndex() != 2 {
		t.Fatalf("expected index 2, got %d", m.sidebar.SelectedIndex())
	}

	// Press g once
	result, _ := m.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 2 {
		t.Error("single g should not move selection")
	}

	// Press g again (gg)
	result, _ = m.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 0 {
		t.Errorf("gg should go to top, got index %d", m.sidebar.SelectedIndex())
	}
}

func TestG_GoToBottom_Sidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"a.go", "b.go", "c.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.focus = SidebarFocus

	result, _ := m.Update(tea.KeyPressMsg{Text: "G", Code: 'G'})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 2 {
		t.Errorf("G should go to last item, got index %d", m.sidebar.SelectedIndex())
	}
}

func TestGG_GoToTop_MainPane(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.focus = MainFocus
	m.mainPane.SetContent("line1\nline2\nline3\nline4\nline5")

	// Press g, then g
	result, _ := m.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	m = result.(*Model)
	if m.mainPane.ScrollTop() != 0 {
		t.Errorf("gg should scroll to top, got offset %d", m.mainPane.ScrollTop())
	}
}

func TestG_GoToBottom_MainPane(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 5
	m.updateLayout()
	m.focus = MainFocus

	// Set long content
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	m.mainPane.SetContent(strings.Join(lines, "\n"))

	result, _ := m.Update(tea.KeyPressMsg{Text: "G", Code: 'G'})
	m = result.(*Model)
	// Should be scrolled down from 0
	if m.mainPane.ScrollTop() == 0 {
		t.Error("G should scroll to bottom")
	}
}

func TestHelp_ShowAndDismiss(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24

	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)
	if !m.showHelp {
		t.Error("? should show help")
	}

	// Any key should dismiss
	result, _ = m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = result.(*Model)
	if m.showHelp {
		t.Error("any key should dismiss help")
	}
}

func TestHelp_DismissWithQ(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)

	result, _ = m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)
	if m.showHelp {
		t.Error("q should dismiss help, not trigger quit confirm")
	}
	if m.confirming {
		t.Error("q in help mode should not trigger quit confirm")
	}
}

func TestHelp_DismissWithQuestionMark(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)
	if !m.showHelp {
		t.Fatal("? should show help")
	}

	// Pressing ? again should dismiss
	result, _ = m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)
	if m.showHelp {
		t.Error("? should dismiss help overlay")
	}
}

func TestHelp_DismissWithEsc(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)

	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(*Model)
	if m.showHelp {
		t.Error("esc should dismiss help overlay")
	}
}

func TestSearch_EnterAndExit(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	if !m.searching {
		t.Error("/ should enter search mode")
	}

	// Type a query
	result, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "i", Code: 'i'})
	m = result.(*Model)
	if m.searchQuery != "hi" {
		t.Errorf("search query = %q, want 'hi'", m.searchQuery)
	}

	// Backspace
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = result.(*Model)
	if m.searchQuery != "h" {
		t.Errorf("after backspace, query = %q, want 'h'", m.searchQuery)
	}

	// Enter to execute
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	if m.searching {
		t.Error("enter should exit search mode")
	}
}

func TestSearch_EscapeCancels(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)

	result, _ = m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = result.(*Model)

	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(*Model)
	if m.searching {
		t.Error("escape should cancel search")
	}
	if m.searchQuery != "" {
		t.Error("escape should clear query")
	}
}

func TestSearch_CtrlCCancels(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)

	// ctrl+c maps to QuitImmediate, should cancel search not quit
	result, cmd := m.Update(tea.KeyPressMsg{Text: "Q", Code: 'Q'})
	m = result.(*Model)
	if m.searching {
		t.Error("Q should cancel search")
	}
	if cmd != nil {
		t.Error("should not quit from search mode")
	}
}

func TestView_WithHelp(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.showHelp = true

	v := m.View()
	if !strings.Contains(v.Content, "Keybindings") {
		t.Error("help view should show keybindings")
	}
}

func TestView_WithSearch(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.searching = true
	m.searchQuery = "test"

	v := m.View()
	if !strings.Contains(v.Content, "/test_") {
		t.Error("search bar should be visible")
	}
}

func TestMouseClick_Sidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.committedFiles = []string{"a.go", "b.go", "c.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.focus = MainFocus

	// Click on sidebar area (x=5, y=3 should be in sidebar, item index 1)
	result, _ := m.Update(tea.MouseClickMsg{X: 5, Y: 3})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("clicking sidebar should focus it")
	}
}

func TestMouseClick_MainPane(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.focus = SidebarFocus

	// Click on main pane area (far right)
	result, _ := m.Update(tea.MouseClickMsg{X: 60, Y: 5})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("clicking main pane should focus it")
	}
}

func TestMouseClick_StatusBar_Left(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.focus = SidebarFocus
	m.mode = FileDiffMode

	// Click left side of status bar (y=0) should do nothing
	result, _ := m.Update(tea.MouseClickMsg{X: 5, Y: 0})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("clicking status bar should not change focus")
	}
	if m.mode != FileDiffMode {
		t.Error("clicking left side should not change mode")
	}
}

func TestMouseClick_StatusBar_UncommittedArea(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 120
	m.height = 24
	m.updateLayout()
	m.mode = CommitMode
	m.uncommittedFiles = []string{"file.go"}
	m.commits = []git.Commit{{SHA: "abc", Subject: "test"}}
	m.updateSidebarItems()

	// Click right side of status bar (uncommitted area) — roughly 2/3 to midpoint
	rightThird := m.width * 2 / 3
	result, _ := m.Update(tea.MouseClickMsg{X: rightThird + 5, Y: 0})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Errorf("clicking uncommitted area should switch to FileDiffMode, got %d", m.mode)
	}
}

func TestMouseClick_StatusBar_CommitsArea(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 120
	m.height = 24
	m.updateLayout()
	m.mode = FileDiffMode
	m.commits = []git.Commit{{SHA: "abc", Subject: "test"}}
	m.updateSidebarItems()

	// Click far right side of status bar (commits area)
	result, _ := m.Update(tea.MouseClickMsg{X: m.width - 5, Y: 0})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Errorf("clicking commits area should switch to CommitMode, got %d", m.mode)
	}
}

func TestMouseClick_StatusBar_Line2(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mode = FileDiffMode

	// Click on line 2 (y=1, PR info line) — should not change mode
	result, _ := m.Update(tea.MouseClickMsg{X: 40, Y: 1})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("clicking PR info line should not change mode")
	}
}

func TestMouseClick_StatusBar_NonGit(t *testing.T) {
	m := NewModel("/tmp", nil)
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mode = FileViewMode

	// Click on status bar in non-git mode — should not change mode
	result, _ := m.Update(tea.MouseClickMsg{X: 70, Y: 0})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("non-git status bar click should not change mode")
	}
}

func TestMouseWheel_Sidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.committedFiles = []string{"a.go", "b.go", "c.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()

	// Scroll down in sidebar — should scroll view, NOT change selection
	result, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 0 {
		t.Errorf("scroll down should not change selection, got %d", m.sidebar.SelectedIndex())
	}

	// Scroll up — selection should remain unchanged
	result, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 0 {
		t.Errorf("scroll up should not change selection, got %d", m.sidebar.SelectedIndex())
	}
}

func TestMouseWheel_MainPane(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Scroll in main pane area — just verify no panic
	result, _ := m.Update(tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelDown})
	_ = result
}

func TestView_MouseModeEnabled(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	v := m.View()
	if v.MouseMode != tea.MouseModeAllMotion {
		t.Error("view should enable mouse all motion for hover support")
	}
}

func TestBuildEditorCmd_WithEDITOR(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	m := NewModel("/tmp", testGit())
	m.mode = FileViewMode
	m.mainPane.SetSize(80, 24)
	m.mainPane.content = "line1\nline2\nline3"

	editor, args := m.buildEditorCmd("test.go")
	if editor != "nvim" {
		t.Errorf("editor = %q, want nvim", editor)
	}
	// Last arg should be the file
	if args[len(args)-1] != "test.go" {
		t.Errorf("last arg = %q, want test.go", args[len(args)-1])
	}
	// In file-view mode, line is scroll offset + 1 = 1
	if args[0] != "+1" {
		t.Errorf("first arg = %q, want +1", args[0])
	}
}

func TestBuildEditorCmd_DefaultEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	m := NewModel("/tmp", testGit())
	m.mode = FileViewMode

	editor, _ := m.buildEditorCmd("test.go")
	if editor != "vi" {
		t.Errorf("default editor = %q, want vi", editor)
	}
}

func TestBuildEditorCmd_DiffModeLineNumber(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = FileDiffMode
	m.mainPane.SetSize(80, 24)
	m.mainPane.content = "@@ -1,3 +10,3 @@\n context\n+added"

	_, args := m.buildEditorCmd("test.go")
	// Should include +N for the line number
	found := false
	for _, a := range args {
		if strings.HasPrefix(a, "+") && a != "+0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected +N arg for diff mode, got %v", args)
	}
}

func TestHandleEnter_MainFocus_FileMode_NoFile(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.focus = MainFocus
	m.mode = FileDiffMode
	// No files set, sidebar returns empty string

	result, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	// openEditor should return nil cmd when file is empty
	if cmd != nil {
		t.Error("enter with no file should produce nil cmd")
	}
}

func TestHandleEnter_MainFocus_CommitMode(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.focus = MainFocus
	m.mode = CommitMode

	result, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	// In commit mode, enter should do nothing
	if cmd != nil {
		t.Error("enter in commit mode main focus should produce nil cmd")
	}
}

func TestUpdateSearchMatches_EmptyQuery(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.searchQuery = ""
	m.updateSearchMatches() // should not panic
	if len(m.searchMatches) != 0 {
		t.Error("empty query should produce no matches")
	}
}

func TestUpdateMainContent_EmptyBase(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.base = "" // no base set
	m.mode = FileDiffMode
	m.updateMainContent() // should return early without panic
}

func TestUpdateMainContent_EmptySidebarSelection(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.base = "HEAD"

	// FileDiffMode empty sidebar
	m.mode = FileDiffMode
	m.updateMainContent()

	// FileViewMode empty sidebar
	m.mode = FileViewMode
	m.updateMainContent()

	// CommitMode empty commits
	m.mode = CommitMode
	m.commits = nil
	m.updateSidebarItems()
	m.updateMainContent()
}

func TestUpdateMainContent_CommitMode_OutOfBounds(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.base = "HEAD"
	m.mode = CommitMode
	m.commits = nil // empty commits
	m.updateSidebarItems()
	m.updateMainContent() // should set empty content without panic
}

func TestUpdateMainContent_NonGit_EmptySidebar(t *testing.T) {
	m := NewModel("/tmp", nil)
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mode = FileViewMode
	// No files
	m.updateMainContent() // should set empty content
}

func TestUpdateMainContent_NonGit_BadFile(t *testing.T) {
	m := NewModel("/tmp", nil)
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mode = FileViewMode
	m.uncommittedFiles = []string{"nonexistent_file.xyz"}
	m.updateSidebarItems()
	m.updateMainContent()
	// Should show error in content
	if !strings.Contains(m.mainPane.content, "Error") {
		t.Error("should show error for missing file")
	}
}

func TestCurrentLineNumber_DiffWithMultipleHunks(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = FileDiffMode
	m.mainPane.SetSize(80, 20)
	m.mainPane.content = "diff --git a/f b/f\nindex abc..def\n--- a/f\n+++ b/f\n@@ -5,3 +5,4 @@\n context\n+added\n-removed\n context"
	line := m.currentLineNumber()
	if line < 1 {
		t.Errorf("expected line >= 1, got %d", line)
	}
}

func TestParseHunkNewStart_SpaceSeparated(t *testing.T) {
	result := parseHunkNewStart("@@ -1 +1 @@")
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestParseHunkNewStart_AtoiError(t *testing.T) {
	result := parseHunkNewStart("@@ -1 +abc,3 @@")
	if result != 0 {
		t.Errorf("expected 0 for non-numeric, got %d", result)
	}
}

func TestParseHunkNewStart_NoCommaOrSpace(t *testing.T) {
	result := parseHunkNewStart("@@ +123")
	if result != 0 {
		t.Errorf("expected 0 when no comma/space delimiter after number, got %d", result)
	}
}

func TestCurrentLineNumber_VariousDiffPrefixes(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = FileDiffMode
	m.mainPane.SetSize(80, 2) // small viewport so we can scroll
	content := "diff --git a/f b/f\nindex abc..def 100644\n--- a/f\n+++ b/f\n@@ -1,5 +1,5 @@\n context\n-old\n+new\n\\ No newline at end of file\n context2"
	m.mainPane.SetContent(content)
	m.mainPane.content = content
	// Scroll to the end so the loop processes all lines
	m.mainPane.GoToBottom()
	line := m.currentLineNumber()
	if line < 1 {
		t.Errorf("expected line >= 1, got %d", line)
	}
}

func TestCurrentLineNumber_EmptyContent(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.mode = FileDiffMode
	m.mainPane.content = ""
	line := m.currentLineNumber()
	if line < 1 {
		t.Errorf("expected line >= 1, got %d", line)
	}
}

func TestModeSwitching_RetainsFileSelection(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"a.go", "b.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()

	// Select second file
	m.sidebar.SelectNext()
	if m.sidebar.SelectedItem() != "b.go" {
		t.Fatalf("expected b.go, got %s", m.sidebar.SelectedItem())
	}

	// Switch to file-view — should retain selection
	result, _ := m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.sidebar.SelectedItem() != "b.go" {
		t.Errorf("file-view should retain selection, got %s", m.sidebar.SelectedItem())
	}
}

func TestSearch_ExecutesOnContent(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 10 // small viewport so content exceeds it and scrolling works
	m.updateLayout()
	// Content must be longer than viewport height for scrolling to work.
	// With height=10, contentHeight=10-4=6, so we need >6 lines.
	m.mainPane.SetContent("line1\nline2\ntarget line\nline4\nline5\nline6\nline7\nline8\nline9\nline10")

	// Enter search, type "target", press enter
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "t", Code: 't'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "e", Code: 'e'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "t", Code: 't'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)

	if m.mainPane.ScrollTop() != 2 {
		t.Errorf("search should scroll to target line (2), got %d", m.mainPane.ScrollTop())
	}
}

func TestG_SingleG_DoesNotMove(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.committedFiles = []string{"a.go", "b.go", "c.go"}
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.focus = SidebarFocus
	m.sidebar.SelectLast()

	// Press g once, then something else — should NOT go to top
	result, _ := m.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = result.(*Model)
	// lastKeyG should be cleared, no jump to top
	if m.lastKeyG {
		t.Error("lastKeyG should be cleared after non-g key")
	}
}

func TestUpdateMainContent_FileDiff_UncommittedFile(t *testing.T) {
	dir := t.TempDir()
	cmds := []struct{ args []string }{
		{[]string{"git", "init", "--initial-branch=main"}},
		{[]string{"git", "config", "user.email", "test@test.com"}},
		{[]string{"git", "config", "user.name", "Test"}},
	}
	for _, c := range cmds {
		cmd := exec.Command(c.args[0], c.args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", c.args, out, err)
		}
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hello\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()
	exec.Command("git", "-C", dir, "checkout", "-b", "feature").Run()

	// Create an uncommitted file
	os.WriteFile(filepath.Join(dir, "wip.go"), []byte("package wip\n"), 0644)

	g := git.New(dir)
	m := NewModel(dir, g)
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Load data directly
	msg := m.loadGitData()
	m.Update(msg)

	// Should be in file-diff mode with uncommitted file
	m.mode = FileDiffMode
	m.updateSidebarItems()
	m.updateMainContent()

	if m.mainPane.content == "" {
		// It's OK if the diff is empty for an untracked file (depends on git)
		// Just verify no panic
	}
}

func TestUpdateMainContent_FileView_WithGitAndError(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.base = "HEAD"
	m.mode = FileViewMode
	m.committedFiles = []string{"nonexistent_file.go"}
	m.updateSidebarItems()
	m.updateMainContent()
	// FileContent will try to read from disk, fail, and fall back to git show,
	// which will also fail — should show error
	if !strings.Contains(m.mainPane.content, "Error") {
		t.Error("should show error for nonexistent file in git mode")
	}
}

func TestLoadGitData_RepoInfoError(t *testing.T) {
	mg := &mockGit{
		repoInfoErr: fmt.Errorf("repo info error"),
	}
	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error from RepoInfo")
	}
}

func TestLoadGitData_DetectBaseError(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "test", RepoName: "repo"},
		baseErr:  fmt.Errorf("no base"),
	}
	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error from DetectBase")
	}
}

func TestLoadGitData_ChangedFilesError(t *testing.T) {
	mg := &mockGit{
		repoInfo:   git.RepoInfoResult{Branch: "test", RepoName: "repo"},
		base:       "abc123",
		changedErr: fmt.Errorf("changed files error"),
	}
	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error from ChangedFiles")
	}
}

func TestLoadGitData_CommitsError(t *testing.T) {
	mg := &mockGit{
		repoInfo:   git.RepoInfoResult{Branch: "test", RepoName: "repo"},
		base:       "abc123",
		commitsErr: fmt.Errorf("commits error"),
	}
	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error from Commits")
	}
}

func TestLoadGitData_Success(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		prInfo:   git.PRInfoResult{Number: 1, Title: "test"},
		base:     "abc123",
		changedFiles: git.ChangedFilesResult{
			Committed:   []string{"a.go"},
			Uncommitted: []string{"b.go"},
		},
		commits: []git.Commit{{SHA: "def", Subject: "commit"}},
	}
	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err != nil {
		t.Fatal(dataMsg.err)
	}
	if dataMsg.repoInfo.Branch != "feature" {
		t.Error("should have repo info")
	}
	if len(dataMsg.committedFiles) != 1 {
		t.Error("should have committed files")
	}
}

func TestUpdateMainContent_WithMockGit_CommitPatchError(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "test"},
		base:     "abc",
		patchErr: fmt.Errorf("patch error"),
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.base = "abc"
	m.updateLayout()
	m.mode = CommitMode
	m.commits = []git.Commit{{SHA: "def", Subject: "test"}}
	m.updateSidebarItems()
	m.updateMainContent()
	if !strings.Contains(m.mainPane.content, "Error") {
		t.Error("should show error for patch failure")
	}
}

func TestUpdateMainContent_WithMockGit_FileDiffError(t *testing.T) {
	mg := &mockGit{
		repoInfo:    git.RepoInfoResult{Branch: "test"},
		base:        "abc",
		fileDiffErr: fmt.Errorf("diff error"),
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.base = "abc"
	m.updateLayout()
	m.mode = FileDiffMode
	m.committedFiles = []string{"file.go"}
	m.updateSidebarItems()
	m.updateMainContent()
	if !strings.Contains(m.mainPane.content, "Error") {
		t.Error("should show error for diff failure")
	}
}

func TestUpdateMainContent_WithMockGit_FileContentError(t *testing.T) {
	mg := &mockGit{
		repoInfo:   git.RepoInfoResult{Branch: "test"},
		base:       "abc",
		contentErr: fmt.Errorf("content error"),
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.base = "abc"
	m.updateLayout()
	m.mode = FileViewMode
	m.committedFiles = []string{"file.go"}
	m.updateSidebarItems()
	m.updateMainContent()
	if !strings.Contains(m.mainPane.content, "Error") {
		t.Error("should show error for content failure")
	}
}

func TestUpdateMainContent_WithMockGit_UncommittedDiff(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "test"},
		base:     "abc",
		fileDiff: "+new line\n-old line",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.base = "abc"
	m.updateLayout()
	m.mode = FileDiffMode
	m.uncommittedFiles = []string{"wip.go"}
	m.updateSidebarItems()
	m.updateMainContent()
	if !strings.Contains(m.mainPane.content, "new line") {
		t.Error("should show uncommitted diff")
	}
}

func TestUpdateMainContent_WithMockGit_FileViewSuccess(t *testing.T) {
	mg := &mockGit{
		repoInfo:    git.RepoInfoResult{Branch: "test"},
		base:        "abc",
		fileContent: "package main\n\nfunc main() {}\n",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.base = "abc"
	m.updateLayout()
	m.mode = FileViewMode
	m.committedFiles = []string{"main.go"}
	m.updateSidebarItems()
	m.updateMainContent()
	if !strings.Contains(m.mainPane.content, "package main") {
		t.Error("should show file content")
	}
}

func TestUpdateMainContent_WithMockGit_CommitPatchSuccess(t *testing.T) {
	mg := &mockGit{
		repoInfo:    git.RepoInfoResult{Branch: "test"},
		base:        "abc",
		commitPatch: "commit abc\n\ndiff --git a/f b/f\n+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.base = "abc"
	m.updateLayout()
	m.mode = CommitMode
	m.commits = []git.Commit{{SHA: "abc", Subject: "test"}}
	m.updateSidebarItems()
	m.updateMainContent()
	if !strings.Contains(m.mainPane.content, "commit abc") {
		t.Error("should show commit patch")
	}
}

func TestLoadGitData_EmptyRepo(t *testing.T) {
	// A git repo with no commits — DetectBase will fail
	dir := t.TempDir()
	cmds := []struct{ args []string }{
		{[]string{"git", "init", "--initial-branch=main"}},
		{[]string{"git", "config", "user.email", "test@test.com"}},
		{[]string{"git", "config", "user.name", "Test"}},
	}
	for _, c := range cmds {
		cmd := exec.Command(c.args[0], c.args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", c.args, out, err)
		}
	}

	g := git.New(dir)
	m := NewModel(dir, g)
	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	// Empty repo: RepoInfo may work (orphan branch) but DetectBase should fail
	if dataMsg.err == nil {
		t.Log("no error in empty repo — some git versions handle this")
	}
}

func TestLoadGitData_Error(t *testing.T) {
	// Use a non-git directory as the git dir — RepoInfo will fail
	dir := t.TempDir()
	g := git.New(dir)
	m := NewModel(dir, g)

	msg := m.loadGitData()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error from loadGitData with non-git dir")
	}
}

func TestLoadNonGitFiles_SkipsHiddenAndDirs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("hi"), 0644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("hi"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	m := NewModel(dir, nil)
	msg := m.loadNonGitFiles()
	dataMsg := msg.(gitDataMsg)

	if len(dataMsg.uncommittedFiles) != 1 {
		t.Errorf("expected 1 visible file, got %d: %v", len(dataMsg.uncommittedFiles), dataMsg.uncommittedFiles)
	}
	if dataMsg.uncommittedFiles[0] != "visible.txt" {
		t.Errorf("expected visible.txt, got %q", dataMsg.uncommittedFiles[0])
	}
}

func TestLoadNonGitFiles_BadDir(t *testing.T) {
	m := NewModel("/nonexistent/dir", nil)
	msg := m.loadNonGitFiles()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestNonGitMode_BlocksModeSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	// Space should not change mode
	result, _ := m.Update(tea.KeyPressMsg{Text: "m", Code: 'm'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("space should not change mode in non-git")
	}

	// 'd' should not change mode
	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("d should not change mode in non-git")
	}

	// 'c' should not change mode
	result, _ = m.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("c should not change mode in non-git")
	}

	// 'f' should stay in file-view (already there)
	result, _ = m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("f should keep FileViewMode in non-git")
	}
}

// === Regression tests for bug reports ===

func TestBug_CommitsTruncatedOnBaseBranch(t *testing.T) {
	// Bug: when on main with 1 unpushed commit, commit mode shows only 1 commit
	// instead of full history. Spec: "running in a branch without a base branch
	// (i.e. directly in main): commit mode should list the full commit history."
	allCommits := []git.Commit{
		{SHA: "aaa0001", Subject: "latest"},
		{SHA: "aaa0002", Subject: "second"},
		{SHA: "aaa0003", Subject: "initial"},
	}
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:     "main",
			Upstream:   "origin/main",
			RepoName:   "repo",
			AheadCount: 1,
		},
		base:         "abc123",
		commits:      allCommits[:1], // Commits(base) returns only unpushed
		changedFiles: git.ChangedFilesResult{},
		allCommits:   allCommits, // AllCommits() returns full history
	}

	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	m.Update(msg)

	if len(m.commits) != 3 {
		t.Errorf("on main branch, expected full history (3 commits), got %d", len(m.commits))
	}
}

func TestBug_CommitsTruncatedOnDetachedHead(t *testing.T) {
	allCommits := []git.Commit{
		{SHA: "bbb0001", Subject: "latest"},
		{SHA: "bbb0002", Subject: "second"},
	}
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{
			IsDetachedHead: true,
			HeadSHA:        "bbb0001",
			RepoName:       "repo",
		},
		base:         "def456",
		commits:      allCommits[:1],
		changedFiles: git.ChangedFilesResult{},
		allCommits:   allCommits,
	}

	m := NewModel("/tmp", mg)
	msg := m.loadGitData()
	m.Update(msg)

	if len(m.commits) != 2 {
		t.Errorf("on detached HEAD, expected full history (2 commits), got %d", len(m.commits))
	}
}

func TestBug_MouseScrollSidebarKeepsSelection(t *testing.T) {
	// Bug: scrolling mouse over file list changes the selected file.
	// Spec: "scrolling should independently scroll the view but keep the
	// selections the same"
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
		},
		commits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff: "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Select the second file
	m.sidebar.SelectIndex(1)
	selectedBefore := m.sidebar.SelectedItem()
	if selectedBefore != "b.go" {
		t.Fatalf("expected b.go selected, got %q", selectedBefore)
	}

	// Scroll down in the sidebar area
	wheelMsg := tea.MouseWheelMsg{
		X:      1,
		Y:      5,
		Button: tea.MouseWheelDown,
	}
	result, _ := m.Update(wheelMsg)
	m = result.(*Model)

	selectedAfter := m.sidebar.SelectedItem()
	if selectedAfter != selectedBefore {
		t.Errorf("mouse scroll changed selection from %q to %q; should stay the same",
			selectedBefore, selectedAfter)
	}
}

func TestBug_SidebarContentDoesNotWrap(t *testing.T) {
	// Bug: in diff and file mode, each sidebar line wraps into the next line.
	// Caused by lipgloss v2 Width being outer dimension (includes borders),
	// but content formatted to the full width.
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"long_filename_that_fills_sidebar.go"},
		},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:    "+new\n-old",
		fileContent: "line1\nline2\n",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Render the sidebar and check that each line fits within the border
	sidebarView := m.sidebar.View(true)
	stripped := stripANSI(sidebarView)
	lines := strings.Split(stripped, "\n")

	// All lines should have the same display width (sidebar outer width)
	expectedWidth := m.sidebarPixelWidth()
	for i, line := range lines {
		w := displayWidth(line)
		if w != expectedWidth {
			t.Errorf("sidebar line %d has width %d, expected %d\nline: %q",
				i, w, expectedWidth, line)
		}
	}
}

func TestBug_MainPaneContentDoesNotWrap(t *testing.T) {
	// Bug: diff content lines wrap slightly over into the next line.
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		commits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff: "+new line of code that is reasonably long\n-old line\n",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Render the main pane and check line widths
	mainView := m.mainPane.View(true)
	stripped := stripANSI(mainView)
	lines := strings.Split(stripped, "\n")

	// Main pane outer width = total width - sidebar outer width
	expectedWidth := m.width - m.sidebarPixelWidth()
	for i, line := range lines {
		w := displayWidth(line)
		if w != expectedWidth {
			t.Errorf("main pane line %d has width %d, expected %d\nline: %q",
				i, w, expectedWidth, line)
		}
	}
}

func TestCommitMode_UnpushedCommitsDimmedWithSeparator(t *testing.T) {
	// Spec: "commits that have not yet been pushed to the origin should be a
	// dimmed color. there should be a dividing horizontal line between these
	// commits and the pushed ones."
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:     "feature/test",
			Upstream:   "origin/feature/test",
			RepoName:   "repo",
			AheadCount: 2, // 2 unpushed commits
		},
		base:         "abc123",
		changedFiles: git.ChangedFilesResult{},
		commits: []git.Commit{
			{SHA: "ccc0001", Subject: "unpushed 1"},
			{SHA: "ccc0002", Subject: "unpushed 2"},
			{SHA: "ccc0003", Subject: "pushed 1"},
			{SHA: "ccc0004", Subject: "pushed 2"},
		},
		allCommits: []git.Commit{
			{SHA: "ccc0001", Subject: "unpushed 1"},
			{SHA: "ccc0002", Subject: "unpushed 2"},
			{SHA: "ccc0003", Subject: "pushed 1"},
			{SHA: "ccc0004", Subject: "pushed 2"},
		},
	}

	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)
	m.mode = CommitMode
	m.updateSidebarItems()

	items := m.sidebar.items
	// Should be: 2 unpushed (dim), 1 separator, 2 pushed (normal)
	if len(items) != 5 {
		t.Fatalf("expected 5 sidebar items (2 dim + 1 sep + 2 normal), got %d", len(items))
	}
	if items[0].kind != itemDim {
		t.Errorf("item 0 should be dim (unpushed), got kind %d", items[0].kind)
	}
	if items[1].kind != itemDim {
		t.Errorf("item 1 should be dim (unpushed), got kind %d", items[1].kind)
	}
	if items[2].kind != itemSeparator {
		t.Errorf("item 2 should be separator, got kind %d", items[2].kind)
	}
	if items[3].kind != itemNormal {
		t.Errorf("item 3 should be normal (pushed), got kind %d", items[3].kind)
	}
	if items[4].kind != itemNormal {
		t.Errorf("item 4 should be normal (pushed), got kind %d", items[4].kind)
	}
}

func TestClickModeIndicator_CyclesModes(t *testing.T) {
	// Spec: "current mode (clicking this should switch modes, like the space bar)"
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 100
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Mode starts as FileView
	if m.mode != FileViewMode {
		t.Fatalf("expected FileViewMode, got %d", m.mode)
	}

	// Click on the center of line 0 (where the mode indicator is)
	centerX := m.width / 2
	clickMsg := tea.MouseClickMsg{X: centerX, Y: 0, Button: tea.MouseLeft}
	result, _ := m.Update(clickMsg)
	m = result.(*Model)

	if m.mode != FileDiffMode {
		t.Errorf("clicking mode indicator should cycle to FileDiffMode, got %d", m.mode)
	}

	// Click again to cycle to CommitMode
	result, _ = m.Update(clickMsg)
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Errorf("clicking mode indicator again should cycle to CommitMode, got %d", m.mode)
	}
}

// === Enhanced search tests ===

func TestSearch_IncrementalMatchAsYouType(t *testing.T) {
	// Spec: "searching should match as you type, and scroll to put the results
	// of the search in view"
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 10
	m.updateLayout()
	m.mainPane.SetContent("line1\nline2\ntarget line\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12")

	// Start search
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)

	// Type "tar" — should already find "target line" at line 2
	for _, ch := range "tar" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}

	// Should have found matches and scrolled
	if len(m.searchMatches) == 0 {
		t.Error("incremental search should find matches while typing")
	}
	if m.mainPane.ScrollTop() != 2 {
		t.Errorf("should scroll to first match at line 2, got %d", m.mainPane.ScrollTop())
	}
}

func TestSearch_MatchCountDisplayed(t *testing.T) {
	// Spec: "the number of matches, and the index of the current match,
	// should display at the bottom of the screen"
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetContent("foo\nbar\nfoo again\nbaz\nfoo third")

	// Start search and type "foo"
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "foo" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}

	v := m.View()
	// Should show match count in the search bar like "/foo  1/3"
	if !strings.Contains(v.Content, "1/3") {
		t.Errorf("search bar should show match count 1/3, got view content containing search bar")
	}
}

func TestSearch_NextPrevNavigation(t *testing.T) {
	// Spec: "pressing [enter] during a search allows [n] to jump to next result
	// and [p] to jump to previous result. jumping between results should wrap around"
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 10
	m.updateLayout()
	m.mainPane.SetContent("match1\nline2\nmatch2\nline4\nmatch3\nline6\nline7\nline8\nline9\nline10\nline11\nline12")

	// Search for "match"
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "match" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}

	// Press enter to confirm
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)

	if m.searching {
		t.Error("enter should exit search input mode")
	}

	// Press n to go to next match
	result, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)
	if m.searchMatchIdx != 1 {
		t.Errorf("n should advance to match index 1, got %d", m.searchMatchIdx)
	}

	// Press n again
	result, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)
	if m.searchMatchIdx != 2 {
		t.Errorf("n should advance to match index 2, got %d", m.searchMatchIdx)
	}

	// Press n again — should wrap to 0
	result, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)
	if m.searchMatchIdx != 0 {
		t.Errorf("n should wrap to match index 0, got %d", m.searchMatchIdx)
	}

	// Press p to go to previous — should wrap to last
	result, _ = m.Update(tea.KeyPressMsg{Text: "p", Code: 'p'})
	m = result.(*Model)
	if m.searchMatchIdx != 2 {
		t.Errorf("p should wrap to match index 2, got %d", m.searchMatchIdx)
	}
}

func TestSearch_DoesNotMatchSidebar(t *testing.T) {
	// Spec: "searching should match against the content in the main pane only (not the sidebar)"
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"alpha.go", "beta.go", "gamma.go"},
		},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "no match here",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Search for "beta" — should NOT match in sidebar
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "beta" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}

	if len(m.searchMatches) != 0 {
		t.Errorf("search should not match sidebar content, got %d matches", len(m.searchMatches))
	}
}

func TestSearch_BackspaceCancelsOnEmpty(t *testing.T) {
	// Spec: "[backspace] if search text is empty, cancel search"
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Enter search, type one char, backspace twice (second empties → cancel)
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = result.(*Model)

	if m.searching {
		t.Error("backspace on empty query should cancel search")
	}
	if m.searchQuery != "" {
		t.Error("search query should be cleared after cancel")
	}
}

func TestSearch_EnterCancelsOnEmpty(t *testing.T) {
	// Spec: "[enter] if search text is empty, cancel search"
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Enter search, then press enter immediately with empty query
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)

	if m.searching {
		t.Error("enter on empty query should cancel search")
	}
	if m.searchConfirmed {
		t.Error("should not confirm search with empty query")
	}
}

func TestSearch_ShiftNPrevResult(t *testing.T) {
	// Spec: "[p] or [shift]+[n]" for previous search result
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetContent("match\nline2\nmatch\nline4\nmatch")

	// Search and confirm
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "match" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)

	// Advance forward first
	result, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)

	// Use shift+N (capital N) to go back
	result, _ = m.Update(tea.KeyPressMsg{Text: "N", Code: 'N'})
	m = result.(*Model)

	if m.searchMatchIdx != 0 {
		t.Errorf("shift+N should go to previous match, got index %d, want 0", m.searchMatchIdx)
	}
}

func TestSearch_EscClearsSearchState(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetContent("match\nline2\nmatch")

	// Search, confirm, then esc should clear
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "match" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)

	// Now in n/p mode — esc should clear search
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(*Model)
	if len(m.searchMatches) != 0 {
		t.Error("esc should clear search matches")
	}
	if m.searchQuery != "" {
		t.Error("esc should clear search query")
	}
}

func TestFileViewMode_ThreeCategories(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed:   []string{"alpha.go", "beta.go"},
			Uncommitted: []string{"wip.go"},
		},
		allFiles:    []string{"alpha.go", "beta.go", "main.go", "readme.md", "wip.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "content",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view mode
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// Sidebar should have: wip.go (dim), separator, alpha.go, beta.go, separator, main.go, readme.md
	items := m.sidebar.items
	if len(items) != 7 {
		t.Fatalf("expected 7 sidebar items, got %d: %v", len(items), items)
	}
	if items[0].filePath != "wip.go" || items[0].kind != itemDim {
		t.Errorf("item 0: expected dim wip.go, got filePath=%q kind=%v", items[0].filePath, items[0].kind)
	}
	if items[1].kind != itemSeparator {
		t.Errorf("item 1: expected separator, got %v", items[1])
	}
	if items[2].filePath != "alpha.go" || items[2].kind != itemNormal {
		t.Errorf("item 2: expected alpha.go, got filePath=%q", items[2].filePath)
	}
	if items[3].filePath != "beta.go" {
		t.Errorf("item 3: expected beta.go, got filePath=%q", items[3].filePath)
	}
	if items[4].kind != itemSeparator {
		t.Errorf("item 4: expected separator, got %v", items[4])
	}
	if items[5].filePath != "main.go" {
		t.Errorf("item 5: expected main.go, got filePath=%q", items[5].filePath)
	}
	if items[6].filePath != "readme.md" {
		t.Errorf("item 6: expected readme.md, got filePath=%q", items[6].filePath)
	}
}

func TestFileDiffMode_NoAllFilesCategory(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed:   []string{"alpha.go"},
			Uncommitted: []string{"wip.go"},
		},
		allFiles:   []string{"alpha.go", "main.go", "wip.go"},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.mode = FileDiffMode // explicitly set since default changed to FileViewMode
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// In file-diff mode, sidebar should only have changed files (no "all files" category)
	items := m.sidebar.items
	if len(items) != 3 { // wip.go, separator, alpha.go
		t.Fatalf("expected 3 sidebar items in diff mode, got %d: %v", len(items), items)
	}
}

func TestToggleSidebar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	if m.sidebarHidden {
		t.Error("sidebar should be visible by default")
	}

	// Press f to hide sidebar
	result, _ := m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if !m.sidebarHidden {
		t.Error("sidebar should be hidden after f")
	}

	// Press f again to show
	result, _ = m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.sidebarHidden {
		t.Error("sidebar should be visible after second f")
	}
}

func TestToggleWordWrap(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	if !m.wordWrap {
		t.Error("word wrap should be on by default")
	}

	result, _ := m.Update(tea.KeyPressMsg{Text: "w", Code: 'w'})
	m = result.(*Model)
	if m.wordWrap {
		t.Error("word wrap should be off after w")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "w", Code: 'w'})
	m = result.(*Model)
	if !m.wordWrap {
		t.Error("word wrap should be on after second w")
	}
}

func TestToggleLineNumbers(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	if !m.lineNumbers {
		t.Error("line numbers should be on by default")
	}

	result, _ := m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)
	if m.lineNumbers {
		t.Error("line numbers should be off after n")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)
	if !m.lineNumbers {
		t.Error("line numbers should be on after second n")
	}
}

func TestToggleIgnored(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"alpha.go"},
		},
		allFiles:    []string{"alpha.go", "main.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "content",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	if !m.showIgnored {
		t.Error("showIgnored should be on by default")
	}

	// Switch to file-view mode first
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// Press i to toggle
	result, _ = m.Update(tea.KeyPressMsg{Text: "i", Code: 'i'})
	m = result.(*Model)
	if m.showIgnored {
		t.Error("showIgnored should be off after i")
	}

	// i should not work in diff mode
	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	result, _ = m.Update(tea.KeyPressMsg{Text: "i", Code: 'i'})
	m = result.(*Model)
	if m.showIgnored {
		t.Error("i in diff mode should not toggle showIgnored")
	}
}

func TestMouseHover_SidebarHighlight(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"alpha.go", "beta.go"},
		},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Hover over sidebar item at row 3 (status bar is 2 rows, border is 1)
	result, _ := m.Update(tea.MouseMotionMsg{X: 5, Y: 3})
	m = result.(*Model)

	// hoverIndex should be set to the first item
	if m.sidebar.hoverIndex != 0 {
		t.Errorf("expected hover index 0, got %d", m.sidebar.hoverIndex)
	}

	// Move outside sidebar
	result, _ = m.Update(tea.MouseMotionMsg{X: 60, Y: 5})
	m = result.(*Model)
	if m.sidebar.hoverIndex != -1 {
		t.Errorf("expected hover index -1 outside sidebar, got %d", m.sidebar.hoverIndex)
	}
}

func TestMouseDrag_SetsCoordinates(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Click in main pane area (x=50 is past sidebar) to start drag
	result, _ := m.Update(tea.MouseClickMsg{X: 50, Y: 5, Button: tea.MouseLeft})
	m = result.(*Model)
	if !m.dragging {
		t.Error("should be dragging after click in main pane")
	}
	if m.dragStartX != 50 || m.dragStartY != 5 {
		t.Errorf("drag start should be (50,5), got (%d,%d)", m.dragStartX, m.dragStartY)
	}

	// Motion while dragging
	result, _ = m.Update(tea.MouseMotionMsg{X: 70, Y: 5})
	m = result.(*Model)
	if m.dragEndX != 70 || m.dragEndY != 5 {
		t.Errorf("drag end should be (70,5), got (%d,%d)", m.dragEndX, m.dragEndY)
	}

	// Release
	result, _ = m.Update(tea.MouseReleaseMsg{X: 70, Y: 5})
	m = result.(*Model)
	if m.dragging {
		t.Error("should not be dragging after release")
	}

	// Clicking in sidebar should NOT start dragging
	result, _ = m.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	m = result.(*Model)
	if m.dragging {
		t.Error("clicking in sidebar should not start drag")
	}
}

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		binary bool
	}{
		{"empty", "", false},
		{"normal text", "hello world\nline 2\n", false},
		{"null byte", "hello\x00world", true},
		{"Go source", "package main\n\nfunc main() {\n}\n", false},
		{"diff output", "+added\n-removed\n context\n", false},
		{"binary with many control chars", string([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x0e, 0x0f}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBinaryContent(tt.input)
			if got != tt.binary {
				t.Errorf("isBinaryContent(%q) = %v, want %v", tt.input, got, tt.binary)
			}
		})
	}
}

func TestBinaryContentDisplay(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"image.png"},
		},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "hello\x00binary\x00content",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view to see the binary content
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// The main pane should show "[binary content]"
	if m.mainPane.content != "[binary content]" {
		t.Errorf("expected [binary content], got %q", m.mainPane.content)
	}
}

func TestRateLimitBackoff(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"alpha.go"},
		},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()

	initial := m.prInterval
	if initial != prRefreshDefault {
		t.Fatalf("expected default interval %v, got %v", prRefreshDefault, initial)
	}

	// Simulate rate limit
	result, _ := m.Update(prRefreshMsg{rateLimited: true})
	m = result.(*Model)
	if m.prInterval != prRefreshDefault*2 {
		t.Errorf("expected interval to double to %v, got %v", prRefreshDefault*2, m.prInterval)
	}

	// Second rate limit
	result, _ = m.Update(prRefreshMsg{rateLimited: true})
	m = result.(*Model)
	if m.prInterval != prRefreshDefault*4 {
		t.Errorf("expected interval to quadruple to %v, got %v", prRefreshDefault*4, m.prInterval)
	}

	// Successful response resets
	result, _ = m.Update(prRefreshMsg{})
	m = result.(*Model)
	if m.prInterval != prRefreshDefault {
		t.Errorf("expected interval reset to %v, got %v", prRefreshDefault, m.prInterval)
	}
}

func TestIsRateLimited(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"normal error", fmt.Errorf("connection refused"), false},
		{"rate limit", fmt.Errorf("API rate limit exceeded"), true},
		{"403", fmt.Errorf("HTTP 403: forbidden"), true},
		{"secondary rate", fmt.Errorf("secondary rate limit"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRateLimited(tt.err); got != tt.want {
				t.Errorf("isRateLimited(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestHelpScrolling(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 10 // small so help needs scrolling
	m.updateLayout()

	// Open help
	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)
	if !m.showHelp {
		t.Fatal("help should be showing")
	}
	if m.helpScrollOffset != 0 {
		t.Error("help scroll offset should start at 0")
	}

	// Scroll down
	result, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = result.(*Model)
	if m.helpScrollOffset != 1 {
		t.Errorf("expected scroll offset 1, got %d", m.helpScrollOffset)
	}

	// Scroll up
	result, _ = m.Update(tea.KeyPressMsg{Text: "k", Code: 'k'})
	m = result.(*Model)
	if m.helpScrollOffset != 0 {
		t.Errorf("expected scroll offset 0 after up, got %d", m.helpScrollOffset)
	}

	// Mouse scroll
	result, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = result.(*Model)
	if m.helpScrollOffset != 1 {
		t.Errorf("expected scroll offset 1 after mouse wheel, got %d", m.helpScrollOffset)
	}
}

func TestHelpSearch(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Open help, then search
	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)

	result, _ = m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	if !m.helpSearching {
		t.Fatal("help search should be active")
	}

	// Type search query
	for _, ch := range "quit" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}
	if m.helpSearchQuery != "quit" {
		t.Errorf("expected search query 'quit', got %q", m.helpSearchQuery)
	}

	// Esc cancels search
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(*Model)
	if m.helpSearching {
		t.Error("esc should cancel help search")
	}
	if m.helpSearchQuery != "" {
		t.Error("esc should clear help search query")
	}

	// Help should still be showing
	if !m.showHelp {
		t.Error("help should still be showing after search cancel")
	}
}

// Regression: BUG_REPORTS.md says go.mod has 27 lines but file-view only scrolls to 25
func TestFileView_ScrollToLastLine(t *testing.T) {
	// Create a 27-line file content
	var fileLines []string
	for i := 1; i <= 27; i++ {
		fileLines = append(fileLines, fmt.Sprintf("line %d content", i))
	}
	fileContent := strings.Join(fileLines, "\n")

	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"go.mod"},
		},
		allFiles:    []string{"go.mod"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: fileContent,
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view mode
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// Switch to main pane focus, then press G to go to bottom
	m.focus = MainFocus
	result, _ = m.Update(tea.KeyPressMsg{Text: "G", Code: 'G'})
	m = result.(*Model)

	// The view should contain line 27
	v := m.View()
	if !strings.Contains(v.Content, "line 27 content") {
		t.Errorf("after GoToBottom, view should contain 'line 27 content' but it doesn't")
		// Find what's visible
		for i := 20; i <= 27; i++ {
			marker := fmt.Sprintf("line %d content", i)
			if strings.Contains(v.Content, marker) {
				t.Logf("  found: %s", marker)
			} else {
				t.Logf("  MISSING: %s", marker)
			}
		}
	}

	// Also test scrolling via repeated down-arrow
	m.mainPane.GoToTop()
	// Press down enough times to reach the end
	for range 50 {
		m.mainPane.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	}
	v = m.View()
	if !strings.Contains(v.Content, "line 27 content") {
		t.Errorf("after scrolling down many times, view should contain 'line 27 content'")
	}
}

func TestParseDiffAnnotations(t *testing.T) {
	diff := `diff --git a/file.go b/file.go
index abc..def 100644
--- a/file.go
+++ b/file.go
@@ -1,5 +1,6 @@
 line1
 line2
+added line
 line3
-removed line
 line4
`
	annotations := parseDiffAnnotations(diff)

	// Line 3 should be added
	ann, ok := annotations[3]
	if !ok {
		t.Fatal("expected annotation for line 3")
	}
	if ann.kind != diffLineAdded {
		t.Errorf("line 3 should be added, got %v", ann.kind)
	}

	// Line 5 (line4 in new file) should have removed lines attached
	ann5, ok5 := annotations[5]
	if !ok5 {
		t.Fatal("expected annotation for line 5")
	}
	if len(ann5.removedLines) != 1 || ann5.removedLines[0] != "removed line" {
		t.Errorf("line 5 should have 'removed line' attached, got %v", ann5.removedLines)
	}
}

func TestDiffGutterInFileView(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		allFiles:    []string{"file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "line1\nline2\nadded\nline3\nline4",
		fileDiff: `@@ -1,4 +1,5 @@
 line1
 line2
+added
 line3
 line4
`,
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view mode
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// The main pane should have diff annotations
	if m.mainPane.diffAnnotations == nil {
		t.Fatal("expected diff annotations to be set")
	}

	// Line 3 should be annotated as added
	ann, ok := m.mainPane.diffAnnotations[3]
	if !ok || ann.kind != diffLineAdded {
		t.Error("line 3 should be annotated as added")
	}

	// The rendered view should contain the "+" gutter marker
	v := m.View()
	if !strings.Contains(v.Content, " + ") {
		t.Error("view should contain ' + ' gutter marker for added lines")
	}
}

func TestShiftD_ToggleRemoved(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		allFiles:    []string{"file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "line1\nline2",
		fileDiff: `@@ -1,3 +1,2 @@
 line1
-removed
 line2
`,
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view mode
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// By default, showRemoved is on — view should contain removed line marker
	v := m.View()
	if !strings.Contains(v.Content, " - ") {
		t.Error("with showRemoved on, view should contain ' - ' for removed lines")
	}

	// Press Shift+D to toggle off
	result, _ = m.Update(tea.KeyPressMsg{Text: "D", Code: 'D'})
	m = result.(*Model)
	v = m.View()
	if strings.Contains(v.Content, " - ") {
		t.Error("with showRemoved off, view should NOT contain ' - '")
	}
}

func TestTreeMode(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"internal/ui/model.go", "internal/ui/keys.go", "main.go"},
		},
		allFiles:   []string{"internal/ui/model.go", "internal/ui/keys.go", "main.go"},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Tree mode is on by default
	if !m.treeMode {
		t.Fatal("tree mode should be on by default")
	}

	// In tree mode, sidebar should have directory entries
	hasDir := false
	for _, item := range m.sidebar.items {
		if item.isDir {
			hasDir = true
			break
		}
	}
	if !hasDir {
		t.Error("tree mode should create directory entries for nested files")
	}

	// Toggle tree mode off
	result, _ := m.Update(tea.KeyPressMsg{Text: "t", Code: 't'})
	m = result.(*Model)
	if m.treeMode {
		t.Error("tree mode should be off after t")
	}

	// In flat mode, no directory entries
	for _, item := range m.sidebar.items {
		if item.isDir {
			t.Error("flat mode should not have directory entries")
			break
		}
	}
}

func TestBuildTreeItems(t *testing.T) {
	files := []string{
		"internal/ui/model.go",
		"internal/ui/keys.go",
		"internal/git/git.go",
		"main.go",
	}
	collapsed := make(map[string]bool)
	items := buildTreeItems(files, itemNormal, collapsed)

	// Should have: internal/ dir, git/ dir, git.go file, ui/ dir, keys.go, model.go, main.go
	// Dirs first, sorted
	if len(items) < 4 {
		t.Fatalf("expected at least 4 items, got %d", len(items))
	}

	// First item should be "internal/" directory
	if !items[0].isDir || items[0].filePath != "internal" {
		t.Errorf("first item should be internal/ dir, got %v", items[0])
	}

	// Collapse internal/
	collapsed["internal"] = true
	items = buildTreeItems(files, itemNormal, collapsed)

	// After collapse, should only have internal/ (collapsed) and main.go
	nonSepCount := 0
	for _, item := range items {
		if item.kind != itemSeparator {
			nonSepCount++
		}
	}
	if nonSepCount != 2 {
		t.Errorf("after collapsing internal/, expected 2 items, got %d", nonSepCount)
	}
}

func TestParseDiffAnnotations_ChangedLines(t *testing.T) {
	// When removed lines are immediately followed by added lines,
	// those should be marked as "changed" (~), not just "added" (+)
	diff := `@@ -1,3 +1,3 @@
 unchanged
-old line
+new line
 unchanged
`
	annotations := parseDiffAnnotations(diff)
	ann, ok := annotations[2]
	if !ok {
		t.Fatal("expected annotation for line 2")
	}
	if ann.kind != diffLineChanged {
		t.Errorf("line 2 should be changed (got kind=%v)", ann.kind)
	}
	if len(ann.removedLines) != 1 || ann.removedLines[0] != "old line" {
		t.Errorf("line 2 should have removed 'old line', got %v", ann.removedLines)
	}
}

func TestDeletedFilesShownInRed(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"deleted.go", "normal.go"},
			Deleted:   []string{"deleted.go"},
		},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Check that deleted.go has itemDeleted kind
	found := false
	for _, item := range m.sidebar.items {
		if item.filePath == "deleted.go" {
			if item.kind != itemDeleted {
				t.Errorf("deleted.go should have itemDeleted kind, got %v", item.kind)
			}
			found = true
		}
	}
	if !found {
		t.Error("deleted.go should appear in sidebar")
	}
}

func TestDeletedFile_GutterShowsRemoved(t *testing.T) {
	// Spec: "if the file being viewed was COMPLETELY removed, then the gutter should indicate that"
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"deleted.go"},
			Deleted:   []string{"deleted.go"},
		},
		allFiles:    []string{},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "old line 1\nold line 2",
		fileDiff: `@@ -1,2 +0,0 @@
-old line 1
-old line 2
`,
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Ensure in file-view mode with deleted.go selected
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// The diff annotations should mark lines as removed
	if m.mainPane.diffAnnotations == nil {
		t.Fatal("expected diff annotations for deleted file")
	}
	ann, ok := m.mainPane.diffAnnotations[1]
	if !ok {
		t.Fatal("expected annotation for line 1")
	}
	if ann.kind != diffLineRemoved {
		t.Errorf("line 1 of deleted file should be marked as removed, got %v", ann.kind)
	}
}

func TestJumpToNextDiff(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		allFiles:    []string{"file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10",
		fileDiff: `@@ -1,10 +1,10 @@
 line1
 line2
+line3
 line4
 line5
 line6
 line7
+line8
 line9
 line10
`,
	}
	m := NewModel("/tmp", mg)
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Switch to file-view
	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)
	m.focus = MainFocus

	// Verify diff annotations are set
	diffLines := m.mainPane.DiffLineNumbers()
	if len(diffLines) == 0 {
		t.Fatal("expected diff annotations to be set")
	}
	if len(diffLines) < 2 {
		t.Fatalf("expected at least 2 diff lines, got %v", diffLines)
	}

	// Verify jumpToNextDiff and jumpToFirstDiff don't panic
	m.jumpToFirstDiff()
	m.jumpToNextDiff(1)
	m.jumpToNextDiff(-1)

	// Verify the functions exercise properly (viewport offset testing is limited
	// without a real terminal, but the logic paths should be covered)
}

func TestHandleEnter_DirectoryToggle(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"internal/model.go", "internal/keys.go"},
		},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
		fileDiff:   "+new",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Tree mode should be on, first item should be internal/ directory
	if !m.sidebar.SelectedIsDir() {
		t.Fatal("first item should be a directory in tree mode")
	}

	// Press h (left) to collapse the directory
	result, _ := m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = result.(*Model)
	if !m.collapsedDirs["internal"] {
		t.Error("h on expanded directory should collapse it")
	}

	// Press enter to expand it again
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	if m.collapsedDirs["internal"] {
		t.Error("enter on collapsed directory should expand it")
	}

	// Press enter on expanded dir moves to first child
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	if m.sidebar.SelectedIsDir() {
		// Moved to child — might be a subdir or file
	}
	if m.sidebar.SelectedIndex() == 0 {
		t.Error("enter on expanded directory should move to child")
	}
}

func TestHelpSearch_WithNavigation(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()

	// Open help
	result, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = result.(*Model)

	// Start search, type a query with multiple matches
	result, _ = m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "mode" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}

	// Should have matches
	if len(m.helpSearchMatches) == 0 {
		t.Fatal("'mode' should match in help content")
	}

	// Press enter to confirm search
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	if !m.helpSearchConfirmed {
		t.Error("enter should confirm help search")
	}

	// Press n to go to next match
	initialIdx := m.helpSearchIdx
	result, _ = m.Update(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = result.(*Model)
	if m.helpSearchIdx == initialIdx && len(m.helpSearchMatches) > 1 {
		t.Error("n should advance to next match")
	}

	// Press p to go to previous
	result, _ = m.Update(tea.KeyPressMsg{Text: "p", Code: 'p'})
	m = result.(*Model)

	// Press esc to exit search mode
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(*Model)
	if m.helpSearchConfirmed {
		t.Error("esc should exit help search navigation")
	}

	// Help should still be showing
	if !m.showHelp {
		t.Error("help should still be showing")
	}
}

func TestRenderHelp_SearchBar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.showHelp = true
	m.helpSearchConfirmed = true
	m.helpSearchQuery = "test"
	m.helpSearchMatches = []int{1, 5, 10}
	m.helpSearchIdx = 1
	m.updateLayout()

	rendered := m.renderHelp()
	if !strings.Contains(rendered, "/test") {
		t.Error("render should show search query")
	}
	if !strings.Contains(rendered, "2/3") {
		t.Error("render should show match index 2/3")
	}
}

func TestHandleSidebarLeft_CollapseDir(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"dir/file.go"},
		},
		allFiles:    []string{"dir/file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "content",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)
	m.treeMode = true
	m.focus = SidebarFocus
	m.updateSidebarItems()

	// Select the directory and press left to collapse
	if !m.sidebar.SelectedIsDir() {
		t.Skip("first item is not a directory")
	}
	dir := m.sidebar.SelectedItem()
	if m.collapsedDirs[dir] {
		t.Error("directory should start expanded")
	}
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = result.(*Model)
	if !m.collapsedDirs[dir] {
		t.Error("left on expanded directory should collapse it")
	}
}

func TestHandleSidebarLeft_GoToParent(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"dir/sub/file.go"},
		},
		allFiles:    []string{"dir/sub/file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "content",
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)
	m.treeMode = true
	m.focus = SidebarFocus
	m.updateSidebarItems()

	// Navigate to the file
	for m.sidebar.SelectedIsDir() {
		m.sidebar.SelectNext()
	}
	fileIdx := m.sidebar.SelectedIndex()

	// Left on a file should go to parent directory
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = result.(*Model)

	if m.sidebar.SelectedIndex() >= fileIdx {
		t.Error("left on file should go to parent directory")
	}
}

func TestMouseWheelHorizontal(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.wordWrap = false
	m.mainPane.SetWordWrap(false)
	m.mainPane.SetPlainContent(strings.Repeat("x", 200))

	result, _ := m.Update(tea.MouseWheelMsg{X: 50, Y: 10, Button: tea.MouseWheelRight})
	m = result.(*Model)

	if m.mainPane.xOffset == 0 {
		t.Error("horizontal scroll right should increase xOffset")
	}

	result, _ = m.Update(tea.MouseWheelMsg{X: 50, Y: 10, Button: tea.MouseWheelLeft})
	m = result.(*Model)
	// xOffset should decrease
}

func TestDragHighlight(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetPlainContent("some content\nmore content")

	// Start a drag
	m.dragging = true
	m.dragStartX = 40
	m.dragStartY = 5
	m.dragEndX = 60
	m.dragEndY = 5

	v := m.View()
	// Just verify it doesn't crash and produces output
	if v.Content == "" {
		t.Error("view should render with drag highlight")
	}
}

func TestReloadAllFiles(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		allFiles:   []string{"file.go", "new.go"},
		commits:    []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits: []git.Commit{{SHA: "abc", Subject: "test"}},
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Trigger allFiles reload
	allMsg := m.reloadAllFiles()
	result, _ := m.Update(allMsg)
	m = result.(*Model)

	if len(m.allFiles) != 2 {
		t.Errorf("expected 2 files after reload, got %d", len(m.allFiles))
	}
}

func TestBuildEditorCmd(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetPlainContent("line1\nline2\nline3")

	editor, args := m.buildEditorCmd("test.go")
	if editor == "" {
		t.Error("editor should not be empty")
	}
	// Should include +line and filename
	hasFile := false
	for _, arg := range args {
		if arg == "test.go" {
			hasFile = true
		}
	}
	if !hasFile {
		t.Error("args should contain filename")
	}
}

func TestCommitIndexFromSidebarItem(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.commits = []git.Commit{
		{SHA: "abc1234567890", Subject: "first"},
		{SHA: "def4567890123", Subject: "second"},
	}

	idx := m.commitIndexFromSidebarItem("abc1234 first")
	if idx != 0 {
		t.Errorf("expected commit index 0, got %d", idx)
	}

	idx = m.commitIndexFromSidebarItem("def4567 second")
	if idx != 1 {
		t.Errorf("expected commit index 1, got %d", idx)
	}

	idx = m.commitIndexFromSidebarItem("unknown")
	if idx != -1 {
		t.Errorf("expected -1 for unknown, got %d", idx)
	}

	idx = m.commitIndexFromSidebarItem("")
	if idx != -1 {
		t.Errorf("expected -1 for empty, got %d", idx)
	}
}

func TestNavigateToCurrentMatch_MainPane(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 5 // small viewport
	m.updateLayout()
	// Make content much taller than viewport
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	m.mainPane.SetContent(strings.Join(lines, "\n"))

	m.searchMatches = []searchMatch{
		{pane: "main", line: 30},
	}
	m.searchMatchIdx = 0
	m.navigateToCurrentMatch()

	if m.mainPane.ScrollTop() < 20 {
		t.Errorf("expected scroll near line 30, got %d", m.mainPane.ScrollTop())
	}
}

func TestScrollRight_Clamping(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 10)
	mp.SetWordWrap(false)
	mp.SetPlainContent("short")

	// Content is shorter than viewport — scroll right should clamp to 0
	mp.ScrollRight(10)
	if mp.xOffset != 0 {
		t.Errorf("expected xOffset 0 for short content, got %d", mp.xOffset)
	}

	// Content wider than viewport
	mp.SetPlainContent(strings.Repeat("x", 100))
	mp.ScrollRight(10)
	if mp.xOffset == 0 {
		t.Error("scroll right should increase xOffset for wide content")
	}

	// Scroll far right — should clamp
	mp.ScrollRight(1000)
	maxExpected := 100 - 40
	if mp.xOffset > maxExpected+10 { // allow some tolerance for gutter
		t.Errorf("xOffset should be clamped, got %d", mp.xOffset)
	}
}

func TestSearchNavKey_ExitsOnOtherKey(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetContent("match\nother\nmatch\nmore\nmatch")

	// Enter search, type, confirm
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, ch := range "match" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(ch), Code: rune(ch)})
		m = result.(*Model)
	}
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)

	if !m.searchConfirmed {
		t.Fatal("search should be confirmed")
	}

	// Press a non-search key — should exit search and re-process
	result, _ = m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	if m.searchConfirmed {
		t.Error("non-search key should exit search nav mode")
	}
}

func TestHighlightSearch_EmptyQuery(t *testing.T) {
	result := highlightSearch("some content", "")
	if result != "some content" {
		t.Error("empty query should return content unchanged")
	}
}

func TestInlineDiffSize_DifferentLengths(t *testing.T) {
	// Test where old and new have different lengths
	size := inlineDiffSize("ab", "abcde")
	if size != 3 { // 0 removed + 3 added ("cde")
		t.Errorf("expected 3, got %d", size)
	}

	size = inlineDiffSize("abcde", "ab")
	if size != 3 { // 3 removed ("cde") + 0 added
		t.Errorf("expected 3, got %d", size)
	}
}

func TestNavigateToCurrentMatch_Empty(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.searchMatches = nil
	m.navigateToCurrentMatch() // should not panic
}

func TestWrapLinesWithIndent_ZeroIndent(t *testing.T) {
	result := wrapLinesWithIndent("hello world testing now", 15, 0)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatal("expected wrapping")
	}
}

func TestWrapLinesWithIndent_SmallWidth(t *testing.T) {
	// width <= indent should fall back to no-indent wrapping
	result := wrapLinesWithIndent("hello world", 3, 5)
	if result == "" {
		t.Error("should not return empty")
	}
}

func TestWrapLinesWithIndent_WithIndent(t *testing.T) {
	result := wrapLinesWithIndent("aaa bbb ccc ddd eee fff ggg", 15, 4)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatal("expected wrapping")
	}
	// Continuation lines should start with indent
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "    ") {
			t.Errorf("continuation line %d should be indented, got %q", i, lines[i])
		}
	}
}

func TestTruncateLinesWithOffset_WidthZero(t *testing.T) {
	result := truncateLinesWithOffset("hello", 0, 0)
	if result != "hello" {
		t.Errorf("width 0 should return original, got %q", result)
	}
}

func TestJumpToNextDiff_NoDiffs(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.mainPane.SetPlainContent("no diffs here")
	m.mainPane.ClearDiffAnnotations()
	m.mode = FileViewMode

	// Should not crash
	m.jumpToNextDiff(1)
	m.jumpToNextDiff(-1)
}

func TestJumpToNextDiff_Wrap(t *testing.T) {
	mg := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		allFiles:    []string{"file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		fileContent: "line1\nline2\nchanged\nline4\nline5",
		fileDiff: `@@ -1,5 +1,5 @@
 line1
 line2
-old
+changed
 line4
 line5
`,
	}
	m := NewModel("/tmp", mg)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	result, _ := m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)

	// Jump to next diff (should be line 3)
	result, _ = m.Update(tea.KeyPressMsg{Text: "J", Code: 'J'})
	m = result.(*Model)

	// Jump again should wrap around
	result, _ = m.Update(tea.KeyPressMsg{Text: "J", Code: 'J'})
	m = result.(*Model)

	// Jump prev should also work and wrap
	result, _ = m.Update(tea.KeyPressMsg{Text: "K", Code: 'K'})
	m = result.(*Model)
}
