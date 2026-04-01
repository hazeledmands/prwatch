package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if len(base) < 7 {
		t.Errorf("base should be a commit SHA, got %q", base)
	}
}

func TestChangedFiles_CommittedOnly(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	result, err := g.ChangedFiles(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Committed) != 1 {
		t.Fatalf("expected 1 committed file, got %d: %v", len(result.Committed), result.Committed)
	}
	if result.Committed[0] != "feature.go" {
		t.Errorf("expected feature.go, got %q", result.Committed[0])
	}
	if len(result.Uncommitted) != 0 {
		t.Errorf("expected 0 uncommitted files, got %d: %v", len(result.Uncommitted), result.Uncommitted)
	}
}

func TestChangedFiles_UncommittedOnly(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	// Add an uncommitted file
	writeFile(t, dir, "wip.go", "package wip\n")

	result, err := g.ChangedFiles(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Committed) != 1 {
		t.Fatalf("expected 1 committed file, got %d: %v", len(result.Committed), result.Committed)
	}
	if len(result.Uncommitted) != 1 {
		t.Fatalf("expected 1 uncommitted file, got %d: %v", len(result.Uncommitted), result.Uncommitted)
	}
	if result.Uncommitted[0] != "wip.go" {
		t.Errorf("expected wip.go, got %q", result.Uncommitted[0])
	}
}

func TestChangedFiles_FileInBothGoesToUncommitted(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	// Modify the already-committed feature.go in the working tree
	writeFile(t, dir, "feature.go", "package feature\n\nvar x = 1\n")

	result, err := g.ChangedFiles(base)
	if err != nil {
		t.Fatal(err)
	}
	// feature.go should be in uncommitted only, not committed
	for _, f := range result.Committed {
		if f == "feature.go" {
			t.Error("feature.go should not be in committed list when also modified in working tree")
		}
	}
	found := false
	for _, f := range result.Uncommitted {
		if f == "feature.go" {
			found = true
		}
	}
	if !found {
		t.Error("feature.go should be in uncommitted list")
	}
}

func TestFileDiffCommitted(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	diff, err := g.FileDiffCommitted(base, "feature.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+package feature") {
		t.Errorf("diff should contain added line, got:\n%s", diff)
	}
}

func TestFileDiffUncommitted(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Modify a tracked file
	writeFile(t, dir, "feature.go", "package feature\n\nvar x = 1\n")

	diff, err := g.FileDiffUncommitted("feature.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+var x = 1") {
		t.Errorf("diff should contain uncommitted change, got:\n%s", diff)
	}
}

func TestFileDiffUncommitted_UntrackedFile(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	writeFile(t, dir, "newfile.go", "package newfile\n")

	diff, err := g.FileDiffUncommitted("newfile.go")
	if err != nil {
		// --no-index exits 1 on diff, which is expected
		if diff == "" {
			t.Fatal(err)
		}
	}
	if !strings.Contains(diff, "+package newfile") {
		t.Errorf("diff should contain new file content, got:\n%s", diff)
	}
}

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

func TestIsRepo(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)
	if !g.IsRepo() {
		t.Error("expected IsRepo=true for git repo")
	}

	nonGitDir := t.TempDir()
	g2 := git.New(nonGitDir)
	if g2.IsRepo() {
		t.Error("expected IsRepo=false for non-git dir")
	}
}

func TestRepoInfo_DetachedHead(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "--detach")
	g := git.New(dir)

	info, err := g.RepoInfo()
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDetachedHead {
		t.Error("expected IsDetachedHead=true")
	}
	if info.HeadSHA == "" {
		t.Error("HeadSHA should be populated")
	}
}

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

func TestFileContent(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Read an existing committed file
	content, err := g.FileContent("feature.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "package feature") {
		t.Errorf("expected file content, got: %q", content)
	}
}

func TestFileContent_WorkingTreeChanges(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Modify the file in the working tree
	writeFile(t, dir, "feature.go", "package feature\n\nvar modified = true\n")

	content, err := g.FileContent("feature.go")
	if err != nil {
		t.Fatal(err)
	}
	// Should read from working tree, not HEAD
	if !strings.Contains(content, "modified") {
		t.Errorf("expected working tree content, got: %q", content)
	}
}

func TestFileContent_UntrackedFile(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	writeFile(t, dir, "newfile.go", "package newfile\n")

	content, err := g.FileContent("newfile.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "package newfile") {
		t.Errorf("expected new file content, got: %q", content)
	}
}

func TestCommits_OnMainBranch(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "main")
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	// On main, range is empty so should fallback to last 10 commits
	commits, err := g.Commits(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Error("should have fallback commits when on main branch")
	}
}

func TestDetectBase_OnMainBranch(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "main")
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should still find a base even on main")
	}
}

func TestDetectBase_DetachedHead(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "--detach")
	g := git.New(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should find a base in detached HEAD")
	}
}

func TestChangedFiles_Sorted(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Add multiple files out of order
	writeFile(t, dir, "zebra.go", "package z\n")
	writeFile(t, dir, "alpha.go", "package a\n")
	writeFile(t, dir, "middle.go", "package m\n")

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	result, err := g.ChangedFiles(base)
	if err != nil {
		t.Fatal(err)
	}

	// Uncommitted files should be sorted
	for i := 1; i < len(result.Uncommitted); i++ {
		if result.Uncommitted[i] < result.Uncommitted[i-1] {
			t.Errorf("uncommitted files not sorted: %v", result.Uncommitted)
			break
		}
	}
}

func TestDetectBase_NoMainBranch(t *testing.T) {
	// Create a repo with "master" instead of "main"
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "--initial-branch=master"},
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
	writeFile(t, dir, "README.md", "# hello\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "file.go", "package f\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feature commit")

	g := git.New(dir)
	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should find master as base when main doesn't exist")
	}
}

func TestDetectBase_FallbackToHEAD(t *testing.T) {
	// Create a repo with only one branch and no remote
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "--initial-branch=only"},
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
	writeFile(t, dir, "README.md", "# hello\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	writeFile(t, dir, "second.go", "package s\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "second")

	g := git.New(dir)
	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should fall back to HEAD~1")
	}
}

func TestRepoInfo_Worktree(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "main")

	// Create a worktree
	wtDir := filepath.Join(t.TempDir(), "wt")
	runGit(t, dir, "worktree", "add", wtDir, "hazel/test/feature")

	g := git.New(wtDir)
	info, err := g.RepoInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Worktree == "" {
		t.Error("expected Worktree to be set in a worktree")
	}
	if info.Branch != "hazel/test/feature" {
		t.Errorf("branch = %q, want %q", info.Branch, "hazel/test/feature")
	}
}
