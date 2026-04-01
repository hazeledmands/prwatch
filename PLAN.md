# prwatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lazygit-style TUI that shows the delta between a feature branch and its base branch, with file and commit modes.

**Architecture:** Bubbletea v2 Elm architecture (Model → Update → View). Git CLI for data, fsnotify for live updates. Three visual components (status bar, sidebar, main pane) composed in a root model.

**Tech Stack:** Go, bubbletea v2, bubbles v2 (viewport, key), lipgloss v2, fsnotify

---

## File Structure

```
prwatch/
├── main.go                    # Entry point, tea.NewProgram setup
├── go.mod / go.sum
├── internal/
│   ├── git/
│   │   ├── git.go             # Git CLI wrapper: base branch, files, diffs, commits, patches
│   │   └── git_test.go        # Tests using a temp git repo
│   ├── watcher/
│   │   ├── watcher.go         # fsnotify watcher with debounce, sends tea.Msg
│   │   └── watcher_test.go
│   └── ui/
│       ├── model.go           # Root bubbletea model, mode/focus state, key dispatch
│       ├── model_test.go      # Unit tests for Update logic
│       ├── statusbar.go       # Status bar rendering
│       ├── sidebar.go         # Sidebar: selectable list of strings
│       ├── sidebar_test.go    # Sidebar selection/navigation tests
│       ├── mainpane.go        # Viewport wrapper with diff coloring
│       ├── styles.go          # All lipgloss style definitions
│       └── keys.go            # Key binding definitions
├── PLAN.md
├── PROMPT.md
└── docs/
```

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `main.go`

- [ ] **Step 1: Initialize Go module and install dependencies**

```bash
cd /Users/hazel/Projects/prwatch
go mod init github.com/hazeledmands/prwatch
go get github.com/charmbracelet/bubbletea/v2@latest
go get github.com/charmbracelet/bubbles/v2@latest
go get github.com/charmbracelet/lipgloss/v2@latest
go get github.com/fsnotify/fsnotify@latest
```

- [ ] **Step 2: Create minimal main.go**

```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea/v2"
)

type model struct{}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	return "prwatch - press q to quit\n"
}

func main() {
	p := tea.NewProgram(model{})
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Verify it builds and runs**

```bash
go build -o prwatch . && echo "Build OK"
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum main.go
git commit -m "Scaffold: minimal bubbletea program"
```

---

### Task 2: Git Data Layer

**Files:**
- Create: `internal/git/git.go`
- Create: `internal/git/git_test.go`

- [ ] **Step 1: Write failing test for RepoInfo**

`internal/git/git_test.go`:
```go
package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
)

// helper to create a temp git repo with a main branch, a feature branch, and a commit on each.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", "--initial-branch=main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", args, out, err)
		}
	}

	// Create initial commit on main
	writeFile(t, dir, "README.md", "# hello\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Create feature branch with a change
	runGit(t, dir, "checkout", "-b", "hazel/test/feature")
	writeFile(t, dir, "feature.go", "package feature\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add feature")

	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
}

func TestRepoInfo(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	info, err := g.RepoInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "hazel/test/feature" {
		t.Errorf("branch = %q, want %q", info.Branch, "hazel/test/feature")
	}
	if info.RepoName == "" {
		t.Error("repo name should not be empty")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

```bash
go test -race -v ./internal/git/
```

Expected: compilation error, package doesn't exist.

- [ ] **Step 3: Implement Git struct, RepoInfo**

`internal/git/git.go`:
```go
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Git wraps git CLI operations for a specific working directory.
type Git struct {
	dir string
}

func New(dir string) *Git {
	return &Git{dir: dir}
}

type RepoInfoResult struct {
	Branch   string
	RepoName string
	Worktree string // empty if not in a worktree
}

func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s %w", strings.Join(args, " "), stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (g *Git) RepoInfo() (RepoInfoResult, error) {
	branch, err := g.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return RepoInfoResult{}, err
	}

	toplevel, err := g.run("rev-parse", "--show-toplevel")
	if err != nil {
		return RepoInfoResult{}, err
	}
	repoName := filepath.Base(toplevel)

	// Detect worktree: if .git is a file (not a dir), we're in a worktree
	var worktree string
	gitDir, err := g.run("rev-parse", "--git-dir")
	if err == nil && strings.Contains(gitDir, "worktrees") {
		worktree = toplevel
	}

	return RepoInfoResult{
		Branch:   branch,
		RepoName: repoName,
		Worktree: worktree,
	}, nil
}
```

- [ ] **Step 4: Run test, verify it passes**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 5: Write failing test for DetectBase**

Append to `internal/git/git_test.go`:
```go
func TestDetectBase(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("base should not be empty")
	}
	// Should find main as the base
	// The merge-base should be a valid commit SHA
	if len(base) < 7 {
		t.Errorf("base should be a commit SHA, got %q", base)
	}
}
```

- [ ] **Step 6: Run test, verify it fails**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 7: Implement DetectBase**

Add to `internal/git/git.go`:
```go
// DetectBase finds the merge-base commit between HEAD and the base branch.
// Tries: gh pr base → main → master → HEAD~1.
func (g *Git) DetectBase() (string, error) {
	// Try gh pr view first
	if base, err := g.ghPRBase(); err == nil && base != "" {
		if sha, err := g.run("merge-base", "HEAD", base); err == nil {
			return sha, nil
		}
	}

	// Try main
	if sha, err := g.run("merge-base", "HEAD", "main"); err == nil {
		return sha, nil
	}

	// Try master
	if sha, err := g.run("merge-base", "HEAD", "master"); err == nil {
		return sha, nil
	}

	// Fallback to HEAD~1
	sha, err := g.run("rev-parse", "HEAD~1")
	if err != nil {
		return "", fmt.Errorf("cannot detect base branch: %w", err)
	}
	return sha, nil
}

func (g *Git) ghPRBase() (string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "baseRefName", "-q", ".baseRefName")
	cmd.Dir = g.dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
```

- [ ] **Step 8: Run test, verify it passes**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 9: Write failing test for ChangedFiles**

Append to `internal/git/git_test.go`:
```go
func TestChangedFiles(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	files, err := g.ChangedFiles(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 changed file, got %d: %v", len(files), files)
	}
	if files[0] != "feature.go" {
		t.Errorf("expected feature.go, got %q", files[0])
	}
}
```

- [ ] **Step 10: Implement ChangedFiles**

Add to `internal/git/git.go`:
```go
// ChangedFiles returns files changed between base and HEAD, plus uncommitted changes.
func (g *Git) ChangedFiles(base string) ([]string, error) {
	// Committed changes
	out, err := g.run("diff", "--name-only", base+"..HEAD")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var files []string
	for _, f := range strings.Split(out, "\n") {
		f = strings.TrimSpace(f)
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	// Uncommitted changes (staged + unstaged)
	out, err = g.run("diff", "--name-only", "HEAD")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}

	return files, nil
}
```

- [ ] **Step 11: Run test, verify it passes**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 12: Write failing test for FileDiff**

Append to `internal/git/git_test.go`:
```go
func TestFileDiff(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	diff, err := g.FileDiff(base, "feature.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+package feature") {
		t.Errorf("diff should contain added line, got:\n%s", diff)
	}
}
```

Add `"strings"` to the test imports if not already present.

- [ ] **Step 13: Implement FileDiff**

Add to `internal/git/git.go`:
```go
// FileDiff returns the diff for a single file between base and HEAD (including uncommitted).
func (g *Git) FileDiff(base, file string) (string, error) {
	// Show full diff from base to working tree
	diff, err := g.run("diff", base, "--", file)
	if err != nil {
		return "", err
	}
	return diff, nil
}
```

- [ ] **Step 14: Run test, verify it passes**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 15: Write failing test for Commits and CommitPatch**

Append to `internal/git/git_test.go`:
```go
func TestCommits(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	commits, err := g.Commits(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].Subject != "add feature" {
		t.Errorf("subject = %q, want %q", commits[0].Subject, "add feature")
	}
	if commits[0].SHA == "" {
		t.Error("SHA should not be empty")
	}
}

func TestCommitPatch(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	commits, err := g.Commits(base)
	if err != nil {
		t.Fatal(err)
	}

	patch, err := g.CommitPatch(commits[0].SHA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "feature") {
		t.Errorf("patch should mention feature, got:\n%s", patch)
	}
}
```

- [ ] **Step 16: Implement Commits and CommitPatch**

Add to `internal/git/git.go`:
```go
type Commit struct {
	SHA     string
	Subject string
}

// Commits returns the list of commits between base and HEAD, newest first.
func (g *Git) Commits(base string) ([]Commit, error) {
	out, err := g.run("log", "--format=%H %s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		subject := ""
		if len(parts) > 1 {
			subject = parts[1]
		}
		commits = append(commits, Commit{SHA: parts[0], Subject: subject})
	}
	return commits, nil
}

// CommitPatch returns the full patch for a single commit.
func (g *Git) CommitPatch(sha string) (string, error) {
	return g.run("show", sha)
}
```

- [ ] **Step 17: Run all git tests, verify they pass**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 18: Write failing test for PRInfo**

Append to `internal/git/git_test.go`:
```go
func TestPRInfo_NoPR(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// In a local-only repo, PRInfo should return empty/no-error
	info, err := g.PRInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != 0 {
		t.Errorf("expected no PR, got #%d", info.Number)
	}
}
```

- [ ] **Step 19: Implement PRInfo**

Add to `internal/git/git.go`:
```go
import "encoding/json"

type PRInfoResult struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	State   string `json:"state"`
	BaseRef string `json:"baseRefName"`
}

// PRInfo fetches PR info via gh CLI. Returns zero-value PRInfoResult if no PR exists.
func (g *Git) PRInfo() (PRInfoResult, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,title,url,state,baseRefName")
	cmd.Dir = g.dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// No PR exists or gh not available
		return PRInfoResult{}, nil
	}
	var result PRInfoResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return PRInfoResult{}, nil
	}
	return result, nil
}
```

- [ ] **Step 20: Run all git tests**

```bash
go test -race -v ./internal/git/
```

- [ ] **Step 21: Commit**

```bash
git add internal/git/
git commit -m "Add git data layer: repo info, base detection, diffs, commits, PR info"
```

---

### Task 3: Styles and Key Bindings

**Files:**
- Create: `internal/ui/styles.go`
- Create: `internal/ui/keys.go`

- [ ] **Step 1: Create styles.go**

`internal/ui/styles.go`:
```go
package ui

import "github.com/charmbracelet/lipgloss/v2"

var (
	// Status bar
	statusBarStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#7D56F4")).
		Foreground(lipgloss.Color("#FAFAFA")).
		Padding(0, 1)

	// Sidebar
	sidebarStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#555"))

	sidebarFocusedStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4"))

	sidebarItemStyle         = lipgloss.NewStyle()
	sidebarSelectedItemStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#333")).
		Foreground(lipgloss.Color("#FAFAFA")).
		Bold(true)

	// Main pane
	mainPaneStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#555"))

	mainPaneFocusedStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4"))

	// Diff coloring
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	diffRemoveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#89DCEB"))
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)

	// Mode indicator
	modeFileStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A6E3A1"))
	modeCommitStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89DCEB"))
)
```

- [ ] **Step 2: Create keys.go**

`internal/ui/keys.go`:
```go
package ui

import "github.com/charmbracelet/bubbles/v2/key"

type keyMap struct {
	Quit        key.Binding
	ToggleMode  key.Binding
	FileMode    key.Binding
	CommitMode  key.Binding
	FocusLeft   key.Binding
	FocusRight  key.Binding
	Up          key.Binding
	Down        key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Enter       key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
	),
	ToggleMode: key.NewBinding(
		key.WithKeys(" "),
	),
	FileMode: key.NewBinding(
		key.WithKeys("f"),
	),
	CommitMode: key.NewBinding(
		key.WithKeys("c"),
	),
	FocusLeft: key.NewBinding(
		key.WithKeys("h", "left"),
	),
	FocusRight: key.NewBinding(
		key.WithKeys("l", "right"),
	),
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
	),
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/ui/
```

- [ ] **Step 4: Commit**

```bash
git add internal/ui/styles.go internal/ui/keys.go
git commit -m "Add UI styles and key bindings"
```

---

### Task 4: Sidebar Component

**Files:**
- Create: `internal/ui/sidebar.go`
- Create: `internal/ui/sidebar_test.go`

- [ ] **Step 1: Write failing tests for sidebar**

`internal/ui/sidebar_test.go`:
```go
package ui

import "testing"

func TestSidebar_SelectNext(t *testing.T) {
	s := newSidebar()
	s.SetItems([]string{"file1.go", "file2.go", "file3.go"})

	if s.SelectedIndex() != 0 {
		t.Errorf("initial selection = %d, want 0", s.SelectedIndex())
	}

	s.SelectNext()
	if s.SelectedIndex() != 1 {
		t.Errorf("after next, selection = %d, want 1", s.SelectedIndex())
	}

	s.SelectNext()
	s.SelectNext() // should clamp at last item
	if s.SelectedIndex() != 2 {
		t.Errorf("after clamping, selection = %d, want 2", s.SelectedIndex())
	}
}

func TestSidebar_SelectPrev(t *testing.T) {
	s := newSidebar()
	s.SetItems([]string{"file1.go", "file2.go"})

	s.SelectPrev() // should stay at 0
	if s.SelectedIndex() != 0 {
		t.Errorf("selection = %d, want 0", s.SelectedIndex())
	}

	s.SelectNext()
	s.SelectPrev()
	if s.SelectedIndex() != 0 {
		t.Errorf("selection = %d, want 0", s.SelectedIndex())
	}
}

func TestSidebar_SelectedItem(t *testing.T) {
	s := newSidebar()
	s.SetItems([]string{"a", "b", "c"})

	if s.SelectedItem() != "a" {
		t.Errorf("selected = %q, want %q", s.SelectedItem(), "a")
	}

	s.SelectNext()
	if s.SelectedItem() != "b" {
		t.Errorf("selected = %q, want %q", s.SelectedItem(), "b")
	}
}

func TestSidebar_EmptyItems(t *testing.T) {
	s := newSidebar()
	if s.SelectedItem() != "" {
		t.Errorf("empty sidebar should return empty string, got %q", s.SelectedItem())
	}
}

func TestSidebar_SetItems_ClampsSelection(t *testing.T) {
	s := newSidebar()
	s.SetItems([]string{"a", "b", "c"})
	s.SelectNext()
	s.SelectNext() // index = 2

	s.SetItems([]string{"x"}) // shrink list
	if s.SelectedIndex() != 0 {
		t.Errorf("selection should clamp to 0, got %d", s.SelectedIndex())
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test -race -v ./internal/ui/
```

- [ ] **Step 3: Implement sidebar**

`internal/ui/sidebar.go`:
```go
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
)

type sidebar struct {
	items    []string
	selected int
	width    int
	height   int
	offset   int // scroll offset for long lists
}

func newSidebar() *sidebar {
	return &sidebar{}
}

func (s *sidebar) SetItems(items []string) {
	s.items = items
	if s.selected >= len(items) {
		s.selected = max(0, len(items)-1)
	}
	s.clampOffset()
}

func (s *sidebar) SetSize(w, h int) {
	s.width = w
	s.height = h
	s.clampOffset()
}

func (s *sidebar) SelectedIndex() int {
	return s.selected
}

func (s *sidebar) SelectedItem() string {
	if len(s.items) == 0 {
		return ""
	}
	return s.items[s.selected]
}

func (s *sidebar) SelectNext() {
	if s.selected < len(s.items)-1 {
		s.selected++
		s.clampOffset()
	}
}

func (s *sidebar) SelectPrev() {
	if s.selected > 0 {
		s.selected--
		s.clampOffset()
	}
}

func (s *sidebar) clampOffset() {
	visible := s.visibleLines()
	if visible <= 0 {
		return
	}
	if s.selected < s.offset {
		s.offset = s.selected
	}
	if s.selected >= s.offset+visible {
		s.offset = s.selected - visible + 1
	}
}

func (s *sidebar) visibleLines() int {
	if s.height <= 0 {
		return len(s.items)
	}
	return s.height
}

func (s *sidebar) View(focused bool) string {
	if len(s.items) == 0 {
		return ""
	}

	visible := s.visibleLines()
	end := s.offset + visible
	if end > len(s.items) {
		end = len(s.items)
	}

	var b strings.Builder
	for i := s.offset; i < end; i++ {
		if i > s.offset {
			b.WriteString("\n")
		}
		label := s.items[i]
		if s.width > 0 && len(label) > s.width {
			label = label[:s.width]
		}
		if s.width > 0 {
			label = fmt.Sprintf("%-*s", s.width, label)
		}
		if i == s.selected {
			b.WriteString(sidebarSelectedItemStyle.Render(label))
		} else {
			b.WriteString(sidebarItemStyle.Render(label))
		}
	}

	style := sidebarStyle
	if focused {
		style = sidebarFocusedStyle
	}
	content := b.String()

	// Pad to fill height
	lines := strings.Count(content, "\n") + 1
	for lines < s.height {
		content += "\n" + strings.Repeat(" ", s.width)
		lines++
	}

	return style.Width(s.width).Render(content)
}

func (s *sidebar) Render(focused bool) string {
	return s.View(focused)
}
```

Also add to `internal/ui/styles.go` if not already there (it should be from Task 3).

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -v ./internal/ui/
```

- [ ] **Step 5: Commit**

```bash
git add internal/ui/sidebar.go internal/ui/sidebar_test.go
git commit -m "Add sidebar component with selection and scrolling"
```

---

### Task 5: Main Pane (Viewport with Diff Coloring)

**Files:**
- Create: `internal/ui/mainpane.go`

- [ ] **Step 1: Create mainpane.go with diff coloring**

`internal/ui/mainpane.go`:
```go
package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/v2/viewport"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

type mainPane struct {
	viewport viewport.Model
	content  string
	width    int
	height   int
}

func newMainPane() *mainPane {
	vp := viewport.New()
	return &mainPane{viewport: vp}
}

func (m *mainPane) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(h)
}

func (m *mainPane) SetContent(content string) {
	m.content = content
	m.viewport.SetContent(colorDiff(content))
}

func (m *mainPane) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return cmd
}

func (m *mainPane) View(focused bool) string {
	style := mainPaneStyle
	if focused {
		style = mainPaneFocusedStyle
	}
	return style.Width(m.width).Height(m.height).Render(m.viewport.View())
}

// ScrollTop returns the line number at the top of the viewport.
func (m *mainPane) ScrollTop() int {
	return m.viewport.YOffset()
}

// colorDiff applies syntax coloring to unified diff output.
func colorDiff(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			lines[i] = diffHeaderStyle.Render(line)
		case strings.HasPrefix(line, "diff "):
			lines[i] = diffHeaderStyle.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = diffHunkStyle.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = diffAddStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = diffRemoveStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/ui/
```

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mainpane.go
git commit -m "Add main pane viewport with diff coloring"
```

---

### Task 6: Status Bar

**Files:**
- Create: `internal/ui/statusbar.go`

- [ ] **Step 1: Create statusbar.go**

`internal/ui/statusbar.go`:
```go
package ui

import (
	"fmt"

	"github.com/hazeledmands/prwatch/internal/git"
	"github.com/charmbracelet/lipgloss/v2"
)

func renderStatusBar(width int, info git.RepoInfoResult, pr git.PRInfoResult, mode Mode) string {
	// Left: branch and repo info
	left := fmt.Sprintf(" %s", info.Branch)
	if info.RepoName != "" {
		left = fmt.Sprintf(" %s @ %s", info.Branch, info.RepoName)
	}
	if info.Worktree != "" {
		left += " (worktree)"
	}

	// Middle: mode indicator
	var modeStr string
	switch mode {
	case FileMode:
		modeStr = modeFileStyle.Render("[files]")
	case CommitMode:
		modeStr = modeCommitStyle.Render("[commits]")
	}

	// Right: PR info
	var right string
	if pr.Number > 0 {
		right = fmt.Sprintf("PR #%d: %s %s ", pr.Number, pr.Title, pr.URL)
	} else {
		right = "No PR "
	}

	// Calculate padding
	padding := width - lipgloss.Width(left) - lipgloss.Width(modeStr) - lipgloss.Width(right)
	if padding < 0 {
		padding = 0
	}
	leftPad := padding / 2
	rightPad := padding - leftPad

	bar := left
	for i := 0; i < leftPad; i++ {
		bar += " "
	}
	bar += modeStr
	for i := 0; i < rightPad; i++ {
		bar += " "
	}
	bar += right

	return statusBarStyle.Width(width).Render(bar)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/ui/
```

- [ ] **Step 3: Commit**

```bash
git add internal/ui/statusbar.go
git commit -m "Add status bar rendering"
```

---

### Task 7: Root Model — Wire Everything Together

**Files:**
- Create: `internal/ui/model.go`
- Create: `internal/ui/model_test.go`
- Modify: `main.go`

- [ ] **Step 1: Write failing tests for mode switching and focus**

`internal/ui/model_test.go`:
```go
package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"
)

func pressKey(m tea.Model, k string) tea.Model {
	updated, _ := m.Update(tea.KeyPressMsg{Code: -1, Text: k})
	return updated
}

func TestModeSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	if m.(*Model).mode != FileMode {
		t.Error("initial mode should be FileMode")
	}

	// Press space to toggle
	m = pressKey(m, " ")
	if m.(*Model).mode != CommitMode {
		t.Error("after space, mode should be CommitMode")
	}

	m = pressKey(m, " ")
	if m.(*Model).mode != FileMode {
		t.Error("after space again, mode should be FileMode")
	}

	// Direct mode keys
	m = pressKey(m, "c")
	if m.(*Model).mode != CommitMode {
		t.Error("after c, mode should be CommitMode")
	}

	m = pressKey(m, "f")
	if m.(*Model).mode != FileMode {
		t.Error("after f, mode should be FileMode")
	}
}

func TestFocusSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	if m.(*Model).focus != SidebarFocus {
		t.Error("initial focus should be SidebarFocus")
	}

	m = pressKey(m, "l")
	if m.(*Model).focus != MainFocus {
		t.Error("after l, focus should be MainFocus")
	}

	m = pressKey(m, "h")
	if m.(*Model).focus != SidebarFocus {
		t.Error("after h, focus should be SidebarFocus")
	}
}
```

Note: The `pressKey` helper constructs a `tea.KeyPressMsg` — we may need to adjust based on how bubbletea v2 actually constructs these. The test intent is clear; exact construction may need adjustment during implementation.

- [ ] **Step 2: Run tests, verify they fail**

```bash
go test -race -v ./internal/ui/
```

- [ ] **Step 3: Implement the root Model**

`internal/ui/model.go`:
```go
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

type Mode int

const (
	FileMode Mode = iota
	CommitMode
)

type Focus int

const (
	SidebarFocus Focus = iota
	MainFocus
)

type Model struct {
	git      *gitpkg.Git
	mode     Mode
	focus    Focus
	width    int
	height   int
	base     string
	repoInfo gitpkg.RepoInfoResult
	prInfo   gitpkg.PRInfoResult
	files    []string
	commits  []gitpkg.Commit
	sidebar  *sidebar
	mainPane *mainPane
	dir      string
	err      error
}

// Messages
type gitDataMsg struct {
	repoInfo gitpkg.RepoInfoResult
	prInfo   gitpkg.PRInfoResult
	base     string
	files    []string
	commits  []gitpkg.Commit
	err      error
}

type RefreshMsg struct{}

func NewModel(dir string, g *gitpkg.Git) tea.Model {
	return &Model{
		git:      g,
		dir:      dir,
		mode:     FileMode,
		focus:    SidebarFocus,
		sidebar:  newSidebar(),
		mainPane: newMainPane(),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadGitData, tea.WindowSize())
}

func (m *Model) loadGitData() tea.Msg {
	if m.git == nil {
		return gitDataMsg{err: fmt.Errorf("no git instance")}
	}

	info, err := m.git.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
	}

	prInfo, _ := m.git.PRInfo()

	base, err := m.git.DetectBase()
	if err != nil {
		return gitDataMsg{err: err}
	}

	files, err := m.git.ChangedFiles(base)
	if err != nil {
		return gitDataMsg{err: err}
	}

	commits, err := m.git.Commits(base)
	if err != nil {
		return gitDataMsg{err: err}
	}

	return gitDataMsg{
		repoInfo: info,
		prInfo:   prInfo,
		base:     base,
		files:    files,
		commits:  commits,
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case gitDataMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.repoInfo = msg.repoInfo
		m.prInfo = msg.prInfo
		m.base = msg.base
		m.files = msg.files
		m.commits = msg.commits
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case RefreshMsg:
		return m, m.loadGitData

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.ToggleMode):
		if m.mode == FileMode {
			m.mode = CommitMode
		} else {
			m.mode = FileMode
		}
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.FileMode):
		m.mode = FileMode
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.CommitMode):
		m.mode = CommitMode
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.FocusLeft):
		m.focus = SidebarFocus
		return m, nil

	case key.Matches(msg, keys.FocusRight):
		m.focus = MainFocus
		return m, nil

	case key.Matches(msg, keys.Enter):
		return m.handleEnter()

	case key.Matches(msg, keys.Up):
		if m.focus == SidebarFocus {
			m.sidebar.SelectPrev()
			m.updateMainContent()
		} else {
			cmd := m.mainPane.Update(msg)
			return m, cmd
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.focus == SidebarFocus {
			m.sidebar.SelectNext()
			m.updateMainContent()
		} else {
			cmd := m.mainPane.Update(msg)
			return m, cmd
		}
		return m, nil

	case key.Matches(msg, keys.PageUp), key.Matches(msg, keys.PageDown):
		if m.focus == MainFocus {
			cmd := m.mainPane.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.focus == SidebarFocus {
		m.focus = MainFocus
		return m, nil
	}

	// Main pane focused
	if m.mode == FileMode {
		return m, m.openEditor()
	}
	return m, nil
}

func (m *Model) openEditor() tea.Cmd {
	file := m.sidebar.SelectedItem()
	if file == "" {
		return nil
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	line := m.currentLineNumber()
	args := []string{}
	if line > 0 {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, file)

	cmd := exec.Command(editor, args...)
	cmd.Dir = m.dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return RefreshMsg{}
	})
}

// currentLineNumber parses the diff to find the source line at the viewport top.
func (m *Model) currentLineNumber() int {
	lines := strings.Split(m.mainPane.content, "\n")
	scrollTop := m.mainPane.ScrollTop()

	// Walk backwards from scrollTop to find the nearest @@ hunk header
	currentLine := 1
	for i := 0; i <= scrollTop && i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "@@") {
			// Parse @@ -a,b +c,d @@
			if n := parseHunkNewStart(line); n > 0 {
				currentLine = n
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			currentLine++
		} else if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "\\") &&
			!strings.HasPrefix(line, "diff") && !strings.HasPrefix(line, "index") &&
			!strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "+++") {
			// Context line
			currentLine++
		}
	}
	return currentLine
}

func parseHunkNewStart(hunkLine string) int {
	// @@ -a,b +c,d @@
	plusIdx := strings.Index(hunkLine, "+")
	if plusIdx < 0 {
		return 0
	}
	rest := hunkLine[plusIdx+1:]
	commaIdx := strings.IndexAny(rest, ", ")
	if commaIdx < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:commaIdx])
	if err != nil {
		return 0
	}
	return n
}

func (m *Model) updateSidebarItems() {
	switch m.mode {
	case FileMode:
		m.sidebar.SetItems(m.files)
	case CommitMode:
		labels := make([]string, len(m.commits))
		for i, c := range m.commits {
			labels[i] = fmt.Sprintf("%.7s %s", c.SHA, c.Subject)
		}
		m.sidebar.SetItems(labels)
	}
}

func (m *Model) updateMainContent() {
	if m.git == nil || m.base == "" {
		return
	}

	switch m.mode {
	case FileMode:
		file := m.sidebar.SelectedItem()
		if file == "" {
			m.mainPane.SetContent("")
			return
		}
		diff, err := m.git.FileDiff(m.base, file)
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			return
		}
		m.mainPane.SetContent(diff)

	case CommitMode:
		idx := m.sidebar.SelectedIndex()
		if idx >= len(m.commits) {
			m.mainPane.SetContent("")
			return
		}
		patch, err := m.git.CommitPatch(m.commits[idx].SHA)
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			return
		}
		m.mainPane.SetContent(patch)
	}
}

func (m *Model) updateLayout() {
	statusBarHeight := 1
	contentHeight := m.height - statusBarHeight - 2 // borders
	sidebarWidth := m.width * 3 / 10
	mainWidth := m.width - sidebarWidth - 4 // borders

	m.sidebar.SetSize(sidebarWidth, contentHeight)
	m.mainPane.SetSize(mainWidth, contentHeight)
}

func (m *Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\nPress q to quit.\n", m.err)
	}

	bar := renderStatusBar(m.width, m.repoInfo, m.prInfo, m.mode)
	sidebarView := m.sidebar.View(m.focus == SidebarFocus)
	mainView := m.mainPane.View(m.focus == MainFocus)

	content := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, mainView)

	return bar + "\n" + content
}
```

Note: import `lipgloss` at the top: `"github.com/charmbracelet/lipgloss/v2"`.

- [ ] **Step 4: Run tests, verify they pass**

```bash
go test -race -v ./internal/ui/
```

Adjust the `pressKey` helper if needed based on how `tea.KeyPressMsg` works in v2. The key matching uses `key.Matches` which checks `msg.String()` / key codes.

- [ ] **Step 5: Update main.go to use the real model**

`main.go`:
```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"github.com/hazeledmands/prwatch/internal/ui"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	g := git.New(dir)
	m := ui.NewModel(dir, g)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Verify it builds**

```bash
go build -o prwatch .
```

- [ ] **Step 7: Commit**

```bash
git add internal/ui/model.go internal/ui/model_test.go internal/ui/statusbar.go main.go
git commit -m "Wire up root model with mode/focus switching, layout, and editor integration"
```

---

### Task 8: File Watcher

**Files:**
- Create: `internal/watcher/watcher.go`
- Create: `internal/watcher/watcher_test.go`

- [ ] **Step 1: Write failing test for debounced watcher**

`internal/watcher/watcher_test.go`:
```go
package watcher_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/watcher"
)

func TestWatcher_DetectsFileChange(t *testing.T) {
	dir := t.TempDir()

	// Create a file to watch
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}

	ch := make(chan struct{}, 10)
	w, err := watcher.New(dir, func() {
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Modify the file
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(testFile, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should receive a notification within 500ms (debounce is 200ms)
	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for file change notification")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

```bash
go test -race -v ./internal/watcher/
```

- [ ] **Step 3: Implement watcher**

`internal/watcher/watcher.go`:
```go
package watcher

import (
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 200 * time.Millisecond

type Watcher struct {
	fsw    *fsnotify.Watcher
	done   chan struct{}
}

// New creates a file watcher that calls onRefresh (debounced) when files change in dir.
func New(dir string, onRefresh func()) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		fsw:  fsw,
		done: make(chan struct{}),
	}

	go w.loop(onRefresh)
	return w, nil
}

func (w *Watcher) loop(onRefresh func()) {
	var timer *time.Timer
	for {
		select {
		case _, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceInterval, onRefresh)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		case <-w.done:
			return
		}
	}
}

func (w *Watcher) Close() error {
	close(w.done)
	return w.fsw.Close()
}
```

- [ ] **Step 4: Run test, verify it passes**

```bash
go test -race -v ./internal/watcher/
```

- [ ] **Step 5: Commit**

```bash
git add internal/watcher/
git commit -m "Add debounced file watcher using fsnotify"
```

---

### Task 9: Integrate Watcher into Main

**Files:**
- Modify: `main.go`
- Modify: `internal/ui/model.go`

- [ ] **Step 1: Update main.go to start watcher**

Update `main.go` to watch the `.git` directory and working tree, sending `RefreshMsg` to the bubbletea program:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"github.com/hazeledmands/prwatch/internal/ui"
	"github.com/hazeledmands/prwatch/internal/watcher"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	g := git.New(dir)
	m := ui.NewModel(dir, g)

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Start file watcher
	w, err := watcher.New(dir, func() {
		p.Send(ui.RefreshMsg{})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: file watcher failed: %v\n", err)
	} else {
		defer w.Close()
		// Also watch .git dir for ref changes
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			wGit, err := watcher.New(gitDir, func() {
				p.Send(ui.RefreshMsg{})
			})
			if err == nil {
				defer wGit.Close()
			}
		}
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify it builds**

```bash
go build -o prwatch .
```

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "Integrate file watcher for live git state updates"
```

---

### Task 10: Manual Smoke Test and Polish

- [ ] **Step 1: Test in a real git worktree**

```bash
cd /path/to/some/feature-branch
/Users/hazel/Projects/prwatch/prwatch
```

Verify:
- Status bar shows branch, repo
- File list appears in sidebar
- Selecting files shows diffs
- Space toggles to commit mode
- h/l switches focus
- j/k navigates
- Enter opens editor (file mode)
- q quits

- [ ] **Step 2: Fix any issues found during smoke test**

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "Polish: fixes from smoke testing"
```

---

## Status

- [x] Task 1: Project Scaffold
- [x] Task 2: Git Data Layer
- [x] Task 3: Styles and Key Bindings
- [x] Task 4: Sidebar Component
- [x] Task 5: Main Pane
- [x] Task 6: Status Bar
- [x] Task 7: Root Model
- [x] Task 8: File Watcher
- [x] Task 9: Integrate Watcher
- [x] Task 10: Smoke Test and Polish
