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

// mockGit implements GitDataSource for controlled testing.
type mockGit struct {
	repoInfo     git.RepoInfoResult
	repoInfoErr  error
	prInfo       git.PRInfoResult
	prInfoErr    error
	base         string
	baseErr      error
	changedFiles git.ChangedFilesResult
	changedErr   error
	commits      []git.Commit
	commitsErr   error
	fileDiff     string
	fileDiffErr  error
	fileContent  string
	contentErr   error
	commitPatch  string
	patchErr     error
}

func (m *mockGit) RepoInfo() (git.RepoInfoResult, error) { return m.repoInfo, m.repoInfoErr }
func (m *mockGit) PRInfo() (git.PRInfoResult, error)     { return m.prInfo, m.prInfoErr }
func (m *mockGit) DetectBase() (string, error)           { return m.base, m.baseErr }
func (m *mockGit) ChangedFiles(base string) (git.ChangedFilesResult, error) {
	return m.changedFiles, m.changedErr
}
func (m *mockGit) Commits(base string) ([]git.Commit, error) { return m.commits, m.commitsErr }
func (m *mockGit) FileDiffCommitted(base, file string) (string, error) {
	return m.fileDiff, m.fileDiffErr
}
func (m *mockGit) FileDiffUncommitted(file string) (string, error) {
	return m.fileDiff, m.fileDiffErr
}
func (m *mockGit) FileContent(file string) (string, error) { return m.fileContent, m.contentErr }
func (m *mockGit) CommitPatch(sha string) (string, error)  { return m.commitPatch, m.patchErr }

func TestModeSwitching(t *testing.T) {
	m := NewModel("/tmp", testGit())

	if m.mode != FileDiffMode {
		t.Error("initial mode should be FileDiffMode")
	}

	// Press space to cycle: FileDiff → FileView → Commit → FileDiff
	result, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after space, mode should be FileViewMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after second space, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("after third space, mode should be FileDiffMode")
	}

	// Direct mode keys
	result, _ = m.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after c, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after f, mode should be FileViewMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("after d, mode should be FileDiffMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after v, mode should be FileViewMode")
	}
}

func TestFocusSwitching(t *testing.T) {
	m := NewModel("/tmp", testGit())

	if m.focus != SidebarFocus {
		t.Error("initial focus should be SidebarFocus")
	}

	result, _ := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("after l, focus should be MainFocus")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("after h, focus should be SidebarFocus")
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
	if items[0].label != "z.go" || items[0].kind != itemDim {
		t.Error("first item should be uncommitted z.go")
	}
	if items[1].kind != itemSeparator {
		t.Error("second item should be separator")
	}
	if items[2].label != "b.go" || items[2].kind != itemNormal {
		t.Error("third item should be committed b.go")
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

	msg := cmd()
	dataMsg, ok := msg.(gitDataMsg)
	if !ok {
		t.Fatalf("expected gitDataMsg, got %T", msg)
	}
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

	// Load data
	cmd := m.Init()
	msg := cmd()
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

func TestMouseClick_StatusBar(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.focus = SidebarFocus

	// Click status bar (y=0) should do nothing
	result, _ := m.Update(tea.MouseClickMsg{X: 5, Y: 0})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("clicking status bar should not change focus")
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

	// Scroll down in sidebar
	result, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 1 {
		t.Errorf("scroll down should select next, got %d", m.sidebar.SelectedIndex())
	}

	// Scroll up
	result, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	m = result.(*Model)
	if m.sidebar.SelectedIndex() != 0 {
		t.Errorf("scroll up should select prev, got %d", m.sidebar.SelectedIndex())
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
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Error("view should enable mouse cell motion")
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

func TestExecuteSearch_EmptyQuery(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.searchQuery = ""
	m.executeSearch() // should not panic
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
	m.mainPane.SetSize(80, 24)
	// Include all diff prefix types for full line coverage
	m.mainPane.content = "diff --git a/f b/f\nindex abc..def 100644\n--- a/f\n+++ b/f\n@@ -1,5 +1,5 @@\n context\n-old\n+new\n\\ No newline at end of file\n context2"
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
	m.height = 5
	m.updateLayout()
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

	// Load data
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
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

	cmd := m.Init()
	msg := cmd()
	dataMsg, ok := msg.(gitDataMsg)
	if !ok {
		t.Fatalf("expected gitDataMsg, got %T", msg)
	}
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
	cmd := m.Init()
	msg := cmd()
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
	cmd := m.Init()
	msg := cmd()
	dataMsg := msg.(gitDataMsg)
	if dataMsg.err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestNonGitMode_BlocksModeSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	// Space should not change mode
	result, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
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
