package git_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
)

// mockGHRunner returns a CmdRunner that intercepts "gh" calls with mock responses
// and delegates everything else to the real exec.Command.
func mockGHRunner(ghResponse string, ghErr error) git.CmdRunner {
	return func(dir string, name string, args ...string) (string, error) {
		if name == "gh" {
			return ghResponse, ghErr
		}
		// Fall back to real execution for non-gh commands
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
}

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
	// Mock gh to return "no pull requests found" — avoids hitting real GitHub API
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no pull requests found for branch")))

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

func TestDetectBase_WithOrigin(t *testing.T) {
	// Create a "bare" origin repo, then clone it so we have origin/main
	originDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "--initial-branch=main", "--bare"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = originDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup origin %v: %s %v", args, out, err)
		}
	}

	// Clone it
	cloneDir := t.TempDir()
	cloneCmd := exec.Command("git", "clone", originDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s %v", out, err)
	}

	// Set up user config
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	// Create initial commit on main
	writeFile(t, cloneDir, "README.md", "# hello\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "initial")
	runGit(t, cloneDir, "push", "origin", "main")

	// Create feature branch
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.go", "package f\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature")

	g := git.New(cloneDir)
	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should detect base via origin/main")
	}
}

func TestDetectBase_WithOriginMaster(t *testing.T) {
	// Create a "bare" origin repo with master branch
	originDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "--initial-branch=master", "--bare"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = originDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup origin %v: %s %v", args, out, err)
		}
	}

	cloneDir := t.TempDir()
	cloneCmd := exec.Command("git", "clone", originDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s %v", out, err)
	}

	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")
	writeFile(t, cloneDir, "README.md", "# hello\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "initial")
	runGit(t, cloneDir, "push", "origin", "master")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.go", "package f\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature")

	g := git.New(cloneDir)
	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should detect base via origin/master")
	}
}

func TestFileContent_FallbackToHEAD(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Delete the file from working tree but it exists in HEAD
	os.Remove(filepath.Join(dir, "feature.go"))

	content, err := g.FileContent("feature.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "package feature") {
		t.Errorf("should fall back to HEAD, got: %q", content)
	}
}

func TestCommits_FallbackToRecentHistory(t *testing.T) {
	// When on the same branch as base (base..HEAD is empty), should show recent commits
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "main")
	g := git.New(dir)

	// Use HEAD as base — range HEAD..HEAD is empty
	sha, _ := g.DetectBase()
	commits, err := g.Commits(sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Error("should show recent commits as fallback")
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

func TestPRInfo_WithMockGH(t *testing.T) {
	dir := setupTestRepo(t)
	jsonResp := `{"number":42,"title":"Test PR","url":"https://github.com/test/repo/pull/42","state":"OPEN","baseRefName":"main"}`
	g := git.NewWithRunner(dir, mockGHRunner(jsonResp, nil))

	info, err := g.PRInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != 42 {
		t.Errorf("expected PR #42, got #%d", info.Number)
	}
	if info.Title != "Test PR" {
		t.Errorf("title = %q, want 'Test PR'", info.Title)
	}
	if info.URL != "https://github.com/test/repo/pull/42" {
		t.Errorf("url = %q", info.URL)
	}
	if info.BaseRef != "main" {
		t.Errorf("baseRef = %q, want 'main'", info.BaseRef)
	}
}

func TestPRInfo_InvalidJSON(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("not json", nil))

	_, err := g.PRInfo()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parsing PR info") {
		t.Errorf("expected parsing error, got: %v", err)
	}
}

func TestPRInfo_GHError(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("gh not found")))

	info, err := g.PRInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != 0 {
		t.Errorf("expected 0, got #%d", info.Number)
	}
}

func TestDetectBase_WithGHPRBase(t *testing.T) {
	dir := setupTestRepo(t)
	// Mock gh to return "main" as PR base
	g := git.NewWithRunner(dir, mockGHRunner("main", nil))

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should detect base via gh PR base")
	}
}

func TestDetectBase_GHReturnsNonExistentBranch(t *testing.T) {
	dir := setupTestRepo(t)
	// Mock gh to return a branch that doesn't exist as origin ref or local ref
	runner := func(d string, name string, args ...string) (string, error) {
		if name == "gh" {
			return "nonexistent-branch-xyz", nil
		}
		cmd := exec.Command(name, args...)
		cmd.Dir = d
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	g := git.NewWithRunner(dir, runner)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	// Should fall through to main/master/HEAD~1 fallback
	if base == "" {
		t.Error("should still find a base via fallback")
	}
}

func TestDefaultCmdRunner_Error(t *testing.T) {
	// Mock gh to simulate "not a git repository" — avoids hitting real GitHub API
	dir := t.TempDir()
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("not a git repository")))
	info, err := g.PRInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != 0 {
		t.Error("expected empty PR info")
	}
}

func TestRepoInfo_NonGitDir(t *testing.T) {
	g := git.New(t.TempDir())
	_, err := g.RepoInfo()
	if err == nil {
		t.Error("expected error for non-git dir")
	}
}

func TestFileDiffUncommitted_NonExistentFile(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)
	_, err := g.FileDiffUncommitted("nonexistent_file.xyz")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestCommits_Error(t *testing.T) {
	g := git.New(t.TempDir()) // not a git repo
	_, err := g.Commits("fakebase")
	if err == nil {
		t.Error("expected error for non-git dir")
	}
}

func TestDetectBase_GHWithOriginRef(t *testing.T) {
	// Create a repo with an origin remote so origin/<base> works
	originDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "--initial-branch=main", "--bare"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = originDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup origin %v: %s %v", args, out, err)
		}
	}
	cloneDir := t.TempDir()
	cloneCmd := exec.Command("git", "clone", originDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s %v", out, err)
	}
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")
	writeFile(t, cloneDir, "README.md", "# hello\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "initial")
	runGit(t, cloneDir, "push", "origin", "main")
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.go", "package f\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature")

	// Mock gh to return "main" — origin/main exists in this repo
	g := git.NewWithRunner(cloneDir, mockGHRunner("main", nil))
	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should detect base via origin/main from gh PR base")
	}
}

func TestDetectBase_GHReturnsEmpty(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", nil))

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Error("should fall through to main when gh returns empty")
	}
}

func TestPRChecks_Success(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://ci.example.com/1"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "SUCCESS" {
		t.Errorf("expected SUCCESS, got %q", ci.State)
	}
}

func TestPRChecks_Failure(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://ci.example.com/2"},{"name":"lint","state":"SUCCESS","bucket":"pass","link":""}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "FAILURE" {
		t.Errorf("expected FAILURE, got %q", ci.State)
	}
	if ci.URL != "https://ci.example.com/2" {
		t.Errorf("expected failure URL, got %q", ci.URL)
	}
}

func TestPRChecks_Pending(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"IN_PROGRESS","bucket":"pending","link":"https://ci.example.com/3"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "PENDING" {
		t.Errorf("expected PENDING, got %q", ci.State)
	}
}

func TestPRChecks_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no PR")))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "" {
		t.Errorf("expected empty state, got %q", ci.State)
	}
}

func TestPRChecks_InvalidJSON(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("not json", nil))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "" {
		t.Errorf("expected empty state for invalid json, got %q", ci.State)
	}
}

func TestPRChecks_EmptyArray(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("[]", nil))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "SUCCESS" {
		t.Errorf("empty checks should be SUCCESS, got %q", ci.State)
	}
}

func TestPRReviews_Success(t *testing.T) {
	dir := setupTestRepo(t)
	reviewJSON := "{\"author\":\"alice\",\"state\":\"APPROVED\"}\n{\"author\":\"bob\",\"state\":\"CHANGES_REQUESTED\"}"
	g := git.NewWithRunner(dir, mockGHRunner(reviewJSON, nil))

	reviews, err := g.PRReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(reviews))
	}
	if reviews[0].Author != "alice" {
		t.Errorf("expected alice, got %q", reviews[0].Author)
	}
}

func TestPRReviews_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no PR")))

	reviews, err := g.PRReviews()
	if err != nil {
		t.Fatal(err)
	}
	if reviews != nil {
		t.Error("expected nil reviews on error")
	}
}

func TestPRReviews_InvalidJSON(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("not json\nalso not json", nil))

	reviews, err := g.PRReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 0 {
		t.Errorf("expected 0 reviews for invalid json, got %d", len(reviews))
	}
}

func TestPRCommentCount_Success(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("5", nil))

	count, err := g.PRCommentCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("expected 5 comments, got %d", count)
	}
}

func TestPRCommentCount_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no PR")))

	count, err := g.PRCommentCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 comments on error, got %d", count)
	}
}

func TestRepoInfo_WithUpstream(t *testing.T) {
	// Create a repo with an origin so upstream tracking works
	originDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "--initial-branch=main", "--bare"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = originDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup origin %v: %s %v", args, out, err)
		}
	}
	cloneDir := t.TempDir()
	cloneCmd := exec.Command("git", "clone", originDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s %v", out, err)
	}
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")
	writeFile(t, cloneDir, "README.md", "# hello\n")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "initial")
	runGit(t, cloneDir, "push", "origin", "main")

	g := git.New(cloneDir)
	info, err := g.RepoInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", info.Upstream)
	}
	if info.DirName == "" {
		t.Error("DirName should not be empty")
	}
}

func TestAllFiles(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Add an untracked file and a gitignored file
	writeFile(t, dir, "untracked.go", "package u\n")
	writeFile(t, dir, ".gitignore", "ignored.txt\n")
	writeFile(t, dir, "ignored.txt", "secret\n")
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "commit", "-m", "add gitignore")

	t.Run("without ignored files", func(t *testing.T) {
		files, err := g.AllFiles(false)
		if err != nil {
			t.Fatal(err)
		}
		// Should include tracked files and untracked (non-ignored) files
		hasIgnored := false
		for _, f := range files {
			if f == "ignored.txt" {
				hasIgnored = true
			}
		}
		if hasIgnored {
			t.Error("should not include ignored.txt when includeIgnored=false")
		}
		// Should include untracked.go
		hasUntracked := false
		for _, f := range files {
			if f == "untracked.go" {
				hasUntracked = true
			}
		}
		if !hasUntracked {
			t.Error("should include untracked.go")
		}
	})

	t.Run("with ignored files", func(t *testing.T) {
		files, err := g.AllFiles(true)
		if err != nil {
			t.Fatal(err)
		}
		hasIgnored := false
		for _, f := range files {
			if f == "ignored.txt" {
				hasIgnored = true
			}
		}
		if !hasIgnored {
			t.Error("should include ignored.txt when includeIgnored=true")
		}
	})

	t.Run("sorted", func(t *testing.T) {
		files, err := g.AllFiles(false)
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(files); i++ {
			if files[i] < files[i-1] {
				t.Errorf("files not sorted: %v", files)
				break
			}
		}
	})
}

func TestBehindCount(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	// Feature branch is 0 commits behind main (main has only initial commit)
	count := g.BehindCount("main")
	if count != 0 {
		t.Errorf("expected 0 behind, got %d", count)
	}

	// Add a commit to main, feature branch should be 1 behind
	runGit(t, dir, "checkout", "main")
	writeFile(t, dir, "extra.txt", "extra\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "extra on main")
	runGit(t, dir, "checkout", "hazel/test/feature")

	count = g.BehindCount("main")
	if count != 1 {
		t.Errorf("expected 1 behind, got %d", count)
	}
}

func TestAllCommits(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	commits, err := g.AllCommits()
	if err != nil {
		t.Fatal(err)
	}
	// Feature branch has 2 commits: "add feature" and "initial commit"
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Subject != "add feature" {
		t.Errorf("first commit subject = %q, want %q", commits[0].Subject, "add feature")
	}
}

func TestBaseCommits(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.New(dir)

	commits, err := g.BaseCommits("main", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 base commit, got %d", len(commits))
	}
	if commits[0].Subject != "initial commit" {
		t.Errorf("base commit subject = %q, want %q", commits[0].Subject, "initial commit")
	}
}

func TestPRComments(t *testing.T) {
	dir := setupTestRepo(t)
	commentsJSON := `{"author":"alice","body":"looks good"}
{"author":"bob","body":"needs work"}`
	g := git.NewWithRunner(dir, mockGHRunner(commentsJSON, nil))

	comments, err := g.PRComments()
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].Author != "alice" {
		t.Errorf("first comment author = %q, want %q", comments[0].Author, "alice")
	}
	if comments[1].Body != "needs work" {
		t.Errorf("second comment body = %q, want %q", comments[1].Body, "needs work")
	}
}

func TestPRComments_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("gh failed")))

	comments, err := g.PRComments()
	if err != nil {
		t.Errorf("PRComments should return nil on error, got %v", err)
	}
	if comments != nil {
		t.Errorf("expected nil comments on error, got %v", comments)
	}
}

func TestCIChecks(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://ci.example.com/1"},{"name":"lint","state":"FAILURE","bucket":"fail","link":"https://ci.example.com/2"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	checks, err := g.CIChecks()
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}
	if checks[0].Name != "build" || checks[0].Bucket != "pass" {
		t.Errorf("unexpected first check: %+v", checks[0])
	}
	if checks[1].Name != "lint" || checks[1].Bucket != "fail" {
		t.Errorf("unexpected second check: %+v", checks[1])
	}
}

func TestCIChecks_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("gh failed")))

	checks, err := g.CIChecks()
	if err != nil {
		t.Errorf("CIChecks should return nil on error, got %v", err)
	}
	if checks != nil {
		t.Errorf("expected nil checks on error, got %v", checks)
	}
}

func TestCIChecks_InvalidJSON(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("not json", nil))

	checks, err := g.CIChecks()
	if err != nil {
		t.Errorf("CIChecks should return nil on invalid JSON, got %v", err)
	}
	if checks != nil {
		t.Errorf("expected nil checks on invalid JSON, got %v", checks)
	}
}

func TestPRChecks_ActionRequired(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"deploy","state":"COMPLETED","bucket":"cancel","link":"https://ci.example.com/4"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	ci, err := g.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	if ci.State != "FAILURE" {
		t.Errorf("action_required should be FAILURE, got %q", ci.State)
	}
}

func TestPRReviewRequests(t *testing.T) {
	dir := setupTestRepo(t)
	requestsJSON := `{"reviewRequests":[{"__typename":"User","login":"alice"},{"__typename":"Team","name":"Storage Reviewers","slug":"org/storage-reviewers"}]}`
	g := git.NewWithRunner(dir, mockGHRunner(requestsJSON, nil))

	requests, err := g.PRReviewRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	if requests[0].Name != "alice" || requests[0].IsTeam {
		t.Errorf("first request: got %+v, want user alice", requests[0])
	}
	if requests[1].Name != "Storage Reviewers" || !requests[1].IsTeam {
		t.Errorf("second request: got %+v, want team Storage Reviewers", requests[1])
	}
}

func TestPRReviewRequests_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("gh failed")))

	requests, err := g.PRReviewRequests()
	if err != nil {
		t.Error("should return nil on error")
	}
	if requests != nil {
		t.Error("should return nil requests on error")
	}
}

func TestPRReviewRequests_Empty(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner(`{"reviewRequests":[]}`, nil))

	requests, err := g.PRReviewRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Errorf("expected 0 requests, got %d", len(requests))
	}
}

func TestIsRWXURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://cloud.rwx.com/mint/honeycomb/runs/abc123", true},
		{"https://github.com/actions/runs/123", false},
		{"https://cloud.rwx.com/mint/org/runs/def456", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := git.IsRWXURL(tt.url); got != tt.expected {
			t.Errorf("IsRWXURL(%q) = %v, want %v", tt.url, got, tt.expected)
		}
	}
}

func TestExtractRWXRunID(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://cloud.rwx.com/mint/honeycomb/runs/7a0c90c974b442d586969dd9be7974ef", "7a0c90c974b442d586969dd9be7974ef"},
		{"https://cloud.rwx.com/mint/org/runs/abc123?foo=bar", "abc123"},
		{"https://github.com/actions/runs/123", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := git.ExtractRWXRunID(tt.url); got != tt.expected {
			t.Errorf("ExtractRWXRunID(%q) = %q, want %q", tt.url, got, tt.expected)
		}
	}
}

// mockCmdRunner returns a CmdRunner that intercepts commands by name with mock responses.
func mockCmdRunner(responses map[string]struct {
	output string
	err    error
}) git.CmdRunner {
	return func(dir string, name string, args ...string) (string, error) {
		if resp, ok := responses[name]; ok {
			return resp.output, resp.err
		}
		// Fall back to real execution for unmatched commands
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
}

func TestRWXResults(t *testing.T) {
	dir := setupTestRepo(t)
	rwxOutput := "Run result status: failed\n\n# Failed task:\n\n- ci.lint-go (task-id: c60819ffe21693dda97241c55b0a8f2e)\n"
	g := git.NewWithRunner(dir, mockCmdRunner(map[string]struct {
		output string
		err    error
	}{
		"rwx": {output: rwxOutput, err: fmt.Errorf("exit status 1")},
	}))

	result, err := g.RWXResults("testrun123")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", result.Status)
	}
	if len(result.FailedTasks) != 1 {
		t.Fatalf("expected 1 failed task, got %d", len(result.FailedTasks))
	}
	if result.FailedTasks[0].Key != "ci.lint-go" {
		t.Errorf("expected task key 'ci.lint-go', got %q", result.FailedTasks[0].Key)
	}
	if result.FailedTasks[0].TaskID != "c60819ffe21693dda97241c55b0a8f2e" {
		t.Errorf("expected task ID 'c60819ffe21693dda97241c55b0a8f2e', got %q", result.FailedTasks[0].TaskID)
	}
}
