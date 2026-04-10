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

// noGH creates a Git instance that stubs out gh/rwx commands so tests
// never hit the real GitHub API. Use instead of noGH(dir) for tests
// that don't need to test gh interaction.
func noGH(dir string) *git.Git {
	return git.NewWithRunner(dir, func(d string, name string, args ...string) (string, error) {
		if name == "gh" || name == "rwx" {
			return "", fmt.Errorf("stubbed out in tests")
		}
		cmd := exec.Command(name, args...)
		cmd.Dir = d
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	})
}

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	commits, err := g.Commits(base, 0, 1000)
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
	g := noGH(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	commits, err := g.Commits(base, 0, 1000)
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
	g := noGH(dir)
	if !g.IsRepo() {
		t.Error("expected IsRepo=true for git repo")
	}

	nonGitDir := t.TempDir()
	g2 := noGH(nonGitDir)
	if g2.IsRepo() {
		t.Error("expected IsRepo=false for non-git dir")
	}
}

func TestRepoInfo_DetachedHead(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "--detach")
	g := noGH(dir)

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

func TestPRAll_NoPR(t *testing.T) {
	dir := setupTestRepo(t)
	// Mock gh to return "no pull requests found" — avoids hitting real GitHub API
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no pull requests found for branch")))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Info.Number != 0 {
		t.Errorf("expected no PR, got #%d", result.Info.Number)
	}
}

func TestFileContent(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

	base, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}

	// On main, range is empty so should fallback to last 10 commits
	commits, err := g.Commits(base, 0, 1000)
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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

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

	g := noGH(dir)
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

	g := noGH(dir)
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

	g := noGH(cloneDir)
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

	g := noGH(cloneDir)
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
	g := noGH(dir)

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
	g := noGH(dir)

	// Use HEAD as base — range HEAD..HEAD is empty
	sha, _ := g.DetectBase()
	commits, err := g.Commits(sha, 0, 1000)
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

	g := noGH(wtDir)
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

func TestPRAll_WithMockGH(t *testing.T) {
	dir := setupTestRepo(t)
	jsonResp := `{"number":42,"title":"Test PR","url":"https://github.com/test/repo/pull/42","state":"OPEN","baseRefName":"main","reviews":[{"author":{"login":"alice"},"state":"APPROVED"}],"reviewRequests":[{"__typename":"User","login":"bob"}],"comments":[{"author":{"login":"carol"},"body":"lgtm"}]}`
	g := git.NewWithRunner(dir, mockGHRunner(jsonResp, nil))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Info.Number != 42 {
		t.Errorf("expected PR #42, got #%d", result.Info.Number)
	}
	if result.Info.Title != "Test PR" {
		t.Errorf("title = %q, want 'Test PR'", result.Info.Title)
	}
	if result.Info.URL != "https://github.com/test/repo/pull/42" {
		t.Errorf("url = %q", result.Info.URL)
	}
	if result.Info.BaseRef != "main" {
		t.Errorf("baseRef = %q, want 'main'", result.Info.BaseRef)
	}
	if len(result.Reviews) != 1 || result.Reviews[0].Author != "alice" || result.Reviews[0].State != "APPROVED" {
		t.Errorf("reviews = %+v, want [{alice APPROVED}]", result.Reviews)
	}
	if len(result.ReviewRequests) != 1 || result.ReviewRequests[0].Name != "bob" {
		t.Errorf("reviewRequests = %+v, want [{bob false}]", result.ReviewRequests)
	}
	if result.CommentCount != 1 {
		t.Errorf("commentCount = %d, want 1", result.CommentCount)
	}
	if len(result.Comments) != 1 || result.Comments[0].Author != "carol" || result.Comments[0].Body != "lgtm" {
		t.Errorf("comments = %+v, want [{carol lgtm}]", result.Comments)
	}
}

func TestPRAll_InvalidJSON(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("not json", nil))

	_, err := g.PRAll()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parsing PR info") {
		t.Errorf("expected parsing error, got: %v", err)
	}
}

func TestPRAll_GHError(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("gh not found")))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Info.Number != 0 {
		t.Errorf("expected 0, got #%d", result.Info.Number)
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
	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Info.Number != 0 {
		t.Error("expected empty PR info")
	}
}

func TestRepoInfo_NonGitDir(t *testing.T) {
	g := noGH(t.TempDir())
	_, err := g.RepoInfo()
	if err == nil {
		t.Error("expected error for non-git dir")
	}
}

func TestFileDiffUncommitted_NonExistentFile(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)
	_, err := g.FileDiffUncommitted("nonexistent_file.xyz")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestCommits_Error(t *testing.T) {
	g := noGH(t.TempDir()) // not a git repo
	_, err := g.Commits("fakebase", 0, 100)
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

func TestDetectBase_CachesGHResult(t *testing.T) {
	dir := setupTestRepo(t)
	ghCalls := 0
	runner := func(d string, name string, args ...string) (string, error) {
		if name == "gh" {
			ghCalls++
			return "main", nil
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

	// First call should invoke gh
	base1, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if ghCalls != 1 {
		t.Fatalf("expected 1 gh call after first DetectBase, got %d", ghCalls)
	}

	// Second call should use cached result, not call gh again
	base2, err := g.DetectBase()
	if err != nil {
		t.Fatal(err)
	}
	if ghCalls != 1 {
		t.Fatalf("expected 1 gh call after second DetectBase (cached), got %d", ghCalls)
	}
	if base1 != base2 {
		t.Fatalf("cached result differs: %q vs %q", base1, base2)
	}
}

func TestPRChecksAll_Success(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://ci.example.com/1"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "SUCCESS" {
		t.Errorf("expected SUCCESS, got %q", result.Status.State)
	}
	if len(result.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(result.Checks))
	}
	if result.Checks[0].Name != "build" {
		t.Errorf("check name = %q, want build", result.Checks[0].Name)
	}
}

func TestPRChecksAll_Failure(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://ci.example.com/2"},{"name":"lint","state":"SUCCESS","bucket":"pass","link":""}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "FAILURE" {
		t.Errorf("expected FAILURE, got %q", result.Status.State)
	}
	if result.Status.URL != "https://ci.example.com/2" {
		t.Errorf("expected failure URL, got %q", result.Status.URL)
	}
	if len(result.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(result.Checks))
	}
}

func TestPRChecksAll_Pending(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"build","state":"IN_PROGRESS","bucket":"pending","link":"https://ci.example.com/3"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "PENDING" {
		t.Errorf("expected PENDING, got %q", result.Status.State)
	}
}

func TestPRChecksAll_Error(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no PR")))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "" {
		t.Errorf("expected empty state, got %q", result.Status.State)
	}
}

func TestPRChecksAll_InvalidJSON(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("not json", nil))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "" {
		t.Errorf("expected empty state for invalid json, got %q", result.Status.State)
	}
}

func TestPRChecksAll_EmptyArray(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("[]", nil))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "SUCCESS" {
		t.Errorf("empty checks should be SUCCESS, got %q", result.Status.State)
	}
}

func TestPRAll_Reviews(t *testing.T) {
	dir := setupTestRepo(t)
	jsonResp := `{"number":1,"reviews":[{"author":{"login":"alice"},"state":"APPROVED","body":"looks great"},{"author":{"login":"bob"},"state":"CHANGES_REQUESTED","body":"needs fixes"}]}`
	g := git.NewWithRunner(dir, mockGHRunner(jsonResp, nil))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(result.Reviews))
	}
	if result.Reviews[0].Author != "alice" {
		t.Errorf("expected alice, got %q", result.Reviews[0].Author)
	}
	if result.Reviews[0].Body != "looks great" {
		t.Errorf("expected body 'looks great', got %q", result.Reviews[0].Body)
	}
	if result.Reviews[1].Body != "needs fixes" {
		t.Errorf("expected body 'needs fixes', got %q", result.Reviews[1].Body)
	}
}

func TestPRAll_ReviewsWithGraphQLComments(t *testing.T) {
	dir := setupTestRepo(t)
	prViewResp := `{"number":1,"reviews":[{"author":{"login":"alice"},"state":"COMMENTED","body":"see comments"}]}`
	graphQLResp := `{"data":{"repository":{"pullRequest":{"reviews":{"nodes":[{"author":{"login":"alice"},"state":"COMMENTED","body":"see comments","comments":{"nodes":[{"path":"main.go","line":42,"body":"nit: rename this"},{"path":"main.go","line":100,"body":"consider error handling"}]}}]}}}}}`

	g := git.NewWithRunner(dir, func(d string, name string, args ...string) (string, error) {
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return graphQLResp, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "repo" {
			return "testowner/testrepo", nil
		}
		if name == "gh" {
			return prViewResp, nil
		}
		return "", fmt.Errorf("unexpected command: %s", name)
	})

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(result.Reviews))
	}
	r := result.Reviews[0]
	if r.Author != "alice" {
		t.Errorf("expected alice, got %q", r.Author)
	}
	if r.Body != "see comments" {
		t.Errorf("expected body 'see comments', got %q", r.Body)
	}
	if len(r.Comments) != 2 {
		t.Fatalf("expected 2 review comments, got %d", len(r.Comments))
	}
	if r.Comments[0].Path != "main.go" || r.Comments[0].Line != 42 {
		t.Errorf("unexpected first comment: %+v", r.Comments[0])
	}
	if r.Comments[1].Body != "consider error handling" {
		t.Errorf("unexpected second comment body: %q", r.Comments[1].Body)
	}
}

func TestPRAll_ReviewRequests(t *testing.T) {
	dir := setupTestRepo(t)
	jsonResp := `{"number":1,"reviewRequests":[{"__typename":"User","login":"alice"},{"__typename":"Team","name":"Storage Reviewers"}]}`
	g := git.NewWithRunner(dir, mockGHRunner(jsonResp, nil))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReviewRequests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(result.ReviewRequests))
	}
	if result.ReviewRequests[0].Name != "alice" || result.ReviewRequests[0].IsTeam {
		t.Errorf("first request: got %+v, want user alice", result.ReviewRequests[0])
	}
	if result.ReviewRequests[1].Name != "Storage Reviewers" || !result.ReviewRequests[1].IsTeam {
		t.Errorf("second request: got %+v, want team Storage Reviewers", result.ReviewRequests[1])
	}
}

func TestPRAll_Comments(t *testing.T) {
	dir := setupTestRepo(t)
	jsonResp := `{"number":1,"comments":[{"author":{"login":"alice"},"body":"looks good"},{"author":{"login":"bob"},"body":"needs work"}]}`
	g := git.NewWithRunner(dir, mockGHRunner(jsonResp, nil))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.CommentCount != 2 {
		t.Errorf("expected 2 comments, got %d", result.CommentCount)
	}
	if len(result.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(result.Comments))
	}
	if result.Comments[0].Author != "alice" {
		t.Errorf("first comment author = %q, want %q", result.Comments[0].Author, "alice")
	}
	if result.Comments[1].Body != "needs work" {
		t.Errorf("second comment body = %q, want %q", result.Comments[1].Body, "needs work")
	}
}

func TestPRAll_ErrorReturnsNil(t *testing.T) {
	dir := setupTestRepo(t)
	g := git.NewWithRunner(dir, mockGHRunner("", fmt.Errorf("no pull requests found")))

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Info.Number != 0 {
		t.Error("expected empty PR info on 'no PR' error")
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

	g := noGH(cloneDir)
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
	g := noGH(dir)

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
	g := noGH(dir)

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
	g := noGH(dir)

	commits, err := g.AllCommits(0, 100)
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

func TestAllCommits_Pagination(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)

	// First page: limit 1
	page1, err := g.AllCommits(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(page1))
	}
	if page1[0].Subject != "add feature" {
		t.Errorf("page1[0] = %q, want %q", page1[0].Subject, "add feature")
	}

	// Second page: skip 1, limit 1
	page2, err := g.AllCommits(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(page2))
	}
	if page2[0].Subject != "initial commit" {
		t.Errorf("page2[0] = %q, want %q", page2[0].Subject, "initial commit")
	}

	// Past the end: skip 2, limit 1
	page3, err := g.AllCommits(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 0 {
		t.Fatalf("expected 0 commits, got %d", len(page3))
	}
}

func TestCommitCount(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)

	count, err := g.CommitCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 commits, got %d", count)
	}
}

func TestCommitCountRange(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)

	count, err := g.CommitCountRange("main")
	if err != nil {
		t.Fatal(err)
	}
	// Feature branch has 1 commit ahead of main
	if count != 1 {
		t.Errorf("expected 1 commit in range, got %d", count)
	}
}

func TestCommitCountRange_OnMain(t *testing.T) {
	dir := setupTestRepo(t)
	runGit(t, dir, "checkout", "main")
	g := noGH(dir)

	count, err := g.CommitCountRange("main")
	if err != nil {
		t.Fatal(err)
	}
	// On main itself, range is empty so falls back to total count (1 commit on main)
	if count != 1 {
		t.Errorf("expected 1 commit (fallback to total), got %d", count)
	}
}

func TestBaseCommits(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)

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

func TestPRChecksAll_ActionRequired(t *testing.T) {
	dir := setupTestRepo(t)
	checksJSON := `[{"name":"deploy","state":"COMPLETED","bucket":"cancel","link":"https://ci.example.com/4"}]`
	g := git.NewWithRunner(dir, mockGHRunner(checksJSON, nil))

	result, err := g.PRChecksAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != "FAILURE" {
		t.Errorf("action_required should be FAILURE, got %q", result.Status.State)
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
	rwxOutput := "Run result status: failed\n\n# Failed task:\n\n- ci.lint-go (task-id: c60819ffe21693dda97241c55b0a8f2e)\n"

	t.Run("parses task ID with has-artifacts suffix", func(t *testing.T) {
		dir := setupTestRepo(t)
		rwxWithArtifacts := "Run result status: failed\n\n# Failed task:\n\n- ci.test-go (task-id: 0b4cbfc6edc27d1c94ef44e2f916c639) (has artifacts)\n"
		g := git.NewWithRunner(dir, mockCmdRunner(map[string]struct {
			output string
			err    error
		}{
			"rwx": {output: rwxWithArtifacts, err: fmt.Errorf("exit status 1")},
		}))

		result, err := g.RWXResults("testrun123")
		if err != nil {
			t.Fatal(err)
		}
		if len(result.FailedTasks) != 1 {
			t.Fatalf("expected 1 failed task, got %d", len(result.FailedTasks))
		}
		if result.FailedTasks[0].TaskID != "0b4cbfc6edc27d1c94ef44e2f916c639" {
			t.Errorf("expected clean task ID, got %q", result.FailedTasks[0].TaskID)
		}
		if !result.FailedTasks[0].HasArtifacts {
			t.Error("expected HasArtifacts to be true")
		}
	})

	t.Run("parses failed tasks from output", func(t *testing.T) {
		dir := setupTestRepo(t)
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
		if result.FailedTasks[0].HasArtifacts {
			t.Error("expected HasArtifacts to be false for task without artifacts")
		}
	})

	t.Run("calls rwx runs with correct args", func(t *testing.T) {
		dir := setupTestRepo(t)
		var capturedName string
		var capturedArgs []string
		g := git.NewWithRunner(dir, func(d string, name string, args ...string) (string, error) {
			capturedName = name
			capturedArgs = args
			return rwxOutput, fmt.Errorf("exit status 1")
		})

		_, _ = g.RWXResults("testrun123")

		if capturedName != "rwx" {
			t.Errorf("expected command 'rwx', got %q", capturedName)
		}
		// The rwx CLI subcommand for viewing run results is "runs", not "results"
		if len(capturedArgs) < 1 || capturedArgs[0] != "runs" {
			t.Errorf("expected first arg 'runs', got %v", capturedArgs)
		}
	})

	t.Run("parses output even when runner returns error alongside stdout", func(t *testing.T) {
		// rwx exits 1 for failed runs but still writes parseable output to stdout.
		// defaultCmdRunner must return that stdout alongside the error so
		// RWXResults can parse the failed tasks. If stdout is discarded on
		// error, the UI shows "No failed tasks" for a failed run.
		dir := setupTestRepo(t)

		// Simulate what defaultCmdRunner SHOULD do: return stdout + error
		g := git.NewWithRunner(dir, mockCmdRunner(map[string]struct {
			output string
			err    error
		}{
			"rwx": {output: rwxOutput, err: fmt.Errorf("exit status 1")},
		}))

		result, err := g.RWXResults("testrun123")
		if err != nil {
			t.Fatalf("RWXResults should parse output even on exit 1, got error: %v", err)
		}
		if len(result.FailedTasks) == 0 {
			t.Fatal("expected failed tasks to be parsed from output, got none")
		}
	})
}

func TestDefaultCmdRunner_ReturnsStdoutOnError(t *testing.T) {
	// defaultCmdRunner must return stdout even when the command exits non-zero.
	// rwx writes parseable output to stdout and exits 1 for failed runs;
	// if defaultCmdRunner discards stdout on error, RWXResults can't parse
	// the failed tasks and the UI misleadingly shows "No failed tasks."
	//
	// git.New() uses defaultCmdRunner, and it only panics for "gh"/"rwx",
	// so we can test its behavior with "bash -c".
	dir := setupTestRepo(t)
	g := git.New(dir)

	out, err := g.RunCmd("bash", "-c", "echo hello; exit 1")
	if err == nil {
		t.Fatal("expected error from exit 1")
	}
	if out != "hello" {
		t.Errorf("expected stdout 'hello' even on non-zero exit, got %q", out)
	}
}

func TestRWXTestResults(t *testing.T) {
	t.Run("downloads and parses failed tests from artifacts", func(t *testing.T) {
		dir := setupTestRepo(t)

		// Create a fake test-results JSON file to be "downloaded"
		artifactDir := filepath.Join(dir, "fake-artifacts")
		os.MkdirAll(artifactDir, 0o755)
		testResultsJSON := `{
			"tests": [
				{"name": "TestPassing", "scope": "pkg/foo", "attempt": {"status": {"kind": "successful"}, "stdout": "ok"}},
				{"name": "TestFailing", "scope": "pkg/bar", "attempt": {"status": {"kind": "failed"}, "stdout": "=== RUN TestFailing\n    bar_test.go:10: expected 1, got 2\n--- FAIL: TestFailing"}}
			]
		}`
		os.WriteFile(filepath.Join(artifactDir, "test-results.json"), []byte(testResultsJSON), 0o644)

		artifactListJSON := `{"Artifacts":[{"Key":"test-data","Kind":"directory"}]}`

		g := git.NewWithRunner(dir, func(d string, name string, args ...string) (string, error) {
			if name != "rwx" {
				return "", fmt.Errorf("unexpected command: %s", name)
			}
			// rwx artifacts list <task-id> --output json
			if len(args) >= 2 && args[0] == "artifacts" && args[1] == "list" {
				return artifactListJSON, nil
			}
			// rwx artifacts download <task-id> <key> --auto-extract --output-dir <dir>
			if len(args) >= 2 && args[0] == "artifacts" && args[1] == "download" {
				// Copy our fake test-results.json into the output dir
				outputDir := args[len(args)-1]
				os.MkdirAll(outputDir, 0o755)
				data, _ := os.ReadFile(filepath.Join(artifactDir, "test-results.json"))
				os.WriteFile(filepath.Join(outputDir, "test-results.json"), data, 0o644)
				return "", nil
			}
			return "", fmt.Errorf("unexpected rwx args: %v", args)
		})

		results, err := g.RWXTestResults("task123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 failed test, got %d", len(results))
		}
		if results[0].Name != "TestFailing" {
			t.Errorf("expected test name 'TestFailing', got %q", results[0].Name)
		}
		if results[0].Scope != "pkg/bar" {
			t.Errorf("expected scope 'pkg/bar', got %q", results[0].Scope)
		}
		if !strings.Contains(results[0].Stdout, "expected 1, got 2") {
			t.Errorf("expected stdout to contain failure message, got %q", results[0].Stdout)
		}
	})

	t.Run("returns empty when no artifacts", func(t *testing.T) {
		dir := setupTestRepo(t)
		g := git.NewWithRunner(dir, func(d string, name string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "artifacts" && args[1] == "list" {
				return `{"Artifacts":[]}`, nil
			}
			return "", fmt.Errorf("unexpected: %v", args)
		})

		results, err := g.RWXTestResults("task123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})
}
