package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	Branch         string
	RepoName       string
	Worktree       string // empty if not in a worktree
	HeadSHA        string
	IsDetachedHead bool
}

type Commit struct {
	SHA     string
	Subject string
}

type PRInfoResult struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	State   string `json:"state"`
	BaseRef string `json:"baseRefName"`
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

// IsRepo returns true if the directory is inside a git repository.
func (g *Git) IsRepo() bool {
	_, err := g.run("rev-parse", "--git-dir")
	return err == nil
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

	headSHA, _ := g.run("rev-parse", "--short", "HEAD")

	return RepoInfoResult{
		Branch:         branch,
		RepoName:       repoName,
		Worktree:       worktree,
		HeadSHA:        headSHA,
		IsDetachedHead: branch == "HEAD",
	}, nil
}

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

// ChangedFilesResult separates committed and uncommitted file changes.
type ChangedFilesResult struct {
	Committed   []string // files changed in base..HEAD only
	Uncommitted []string // files with working tree changes
}

// ChangedFiles returns files changed between base and HEAD, separated by commit status.
// Files that appear in both committed and uncommitted go to Uncommitted only.
func (g *Git) ChangedFiles(base string) (ChangedFilesResult, error) {
	// Get committed changes (base..HEAD)
	out, err := g.run("diff", "--name-only", base+"..HEAD")
	if err != nil {
		return ChangedFilesResult{}, err
	}

	committedSet := make(map[string]bool)
	for _, f := range strings.Split(out, "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			committedSet[f] = true
		}
	}

	// Get uncommitted changes (staged + unstaged + untracked)
	uncommittedSet := make(map[string]bool)
	out, err = g.run("diff", "--name-only", "HEAD")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				uncommittedSet[f] = true
			}
		}
	}
	// Also include untracked files
	out, err = g.run("ls-files", "--others", "--exclude-standard")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				uncommittedSet[f] = true
			}
		}
	}

	// Files in both go to uncommitted only
	var committed, uncommitted []string
	for f := range committedSet {
		if uncommittedSet[f] {
			continue // will be in uncommitted list
		}
		committed = append(committed, f)
	}
	for f := range uncommittedSet {
		uncommitted = append(uncommitted, f)
	}

	sort.Strings(committed)
	sort.Strings(uncommitted)

	return ChangedFilesResult{
		Committed:   committed,
		Uncommitted: uncommitted,
	}, nil
}

// FileDiff returns the diff for a single file between base and HEAD (including uncommitted).
func (g *Git) FileDiff(base, file string) (string, error) {
	diff, err := g.run("diff", base, "--", file)
	if err != nil {
		return "", err
	}
	return diff, nil
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

// FileContent returns the full content of a file from the working tree.
// Falls back to HEAD version if the working tree read fails.
func (g *Git) FileContent(file string) (string, error) {
	// Read from working tree directly (handles uncommitted/untracked files)
	content, err := os.ReadFile(filepath.Join(g.dir, file))
	if err != nil {
		// Fall back to HEAD version
		return g.run("show", "HEAD:"+file)
	}
	return string(content), nil
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
