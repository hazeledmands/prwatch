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
