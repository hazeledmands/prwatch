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

// CmdRunner executes an external command and returns its stdout.
// The default implementation uses exec.Command.
type CmdRunner func(dir string, name string, args ...string) (string, error)

// Git wraps git CLI operations for a specific working directory.
type Git struct {
	dir    string
	runCmd CmdRunner // for running non-git commands (e.g. gh)
}

func New(dir string) *Git {
	return &Git{dir: dir, runCmd: defaultCmdRunner}
}

// NewWithRunner creates a Git instance with a custom command runner for testing.
func NewWithRunner(dir string, runner CmdRunner) *Git {
	return &Git{dir: dir, runCmd: runner}
}

func defaultCmdRunner(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg != "" {
			return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(errMsg))
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

type RepoInfoResult struct {
	Branch         string
	Upstream       string // e.g. "origin/main"
	RepoName       string
	RepoURL        string // HTTPS URL of the repo (from origin remote)
	DirName        string // basename of the working directory
	Worktree       string // empty if not in a worktree
	HeadSHA        string
	IsDetachedHead bool
	AheadCount     int // commits ahead of upstream
}

type Commit struct {
	SHA     string
	Subject string
}

type PRInfoResult struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	State          string `json:"state"`
	BaseRef        string `json:"baseRefName"`
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	CommentsCount  int    `json:"comments"`
	Body           string `json:"body"`
}

type PRComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

type CICheck struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"detailsUrl"`
}

type CIStatusResult struct {
	State      string // SUCCESS, FAILURE, PENDING, ""
	Conclusion string // e.g. "success", "failure"
	URL        string // link to the CI run
}

type PRReview struct {
	Author string `json:"author"`
	State  string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, PENDING
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

	// Get upstream tracking branch
	var upstream string
	var aheadCount int
	if branch != "HEAD" {
		upstream, _ = g.run("rev-parse", "--abbrev-ref", branch+"@{upstream}")
		if upstream != "" {
			// Count commits ahead of upstream
			ahead, err := g.run("rev-list", "--count", upstream+"..HEAD")
			if err == nil {
				fmt.Sscanf(ahead, "%d", &aheadCount)
			}
		}
	}

	// Get repo URL from origin remote
	var repoURL string
	if remoteURL, err := g.run("remote", "get-url", "origin"); err == nil {
		repoURL = gitRemoteToHTTPS(remoteURL)
	}

	return RepoInfoResult{
		Branch:         branch,
		Upstream:       upstream,
		RepoName:       repoName,
		RepoURL:        repoURL,
		DirName:        filepath.Base(g.dir),
		Worktree:       worktree,
		HeadSHA:        headSHA,
		IsDetachedHead: branch == "HEAD",
		AheadCount:     aheadCount,
	}, nil
}

// DetectBase finds the merge-base commit between HEAD and origin's base branch.
// Uses origin/<base> refs to stay consistent with GitHub's three-dot diff view.
// Tries: gh pr base → origin/main → origin/master → local main → local master → HEAD~1.
func (g *Git) DetectBase() (string, error) {
	// Try gh pr view first — use origin/<base> for GitHub consistency
	if base, err := g.ghPRBase(); err == nil && base != "" {
		if sha, err := g.run("merge-base", "HEAD", "origin/"+base); err == nil {
			return sha, nil
		}
		// Fall back to local ref if origin not available
		if sha, err := g.run("merge-base", "HEAD", base); err == nil {
			return sha, nil
		}
	}

	// Try origin/main
	if sha, err := g.run("merge-base", "HEAD", "origin/main"); err == nil {
		return sha, nil
	}

	// Try origin/master
	if sha, err := g.run("merge-base", "HEAD", "origin/master"); err == nil {
		return sha, nil
	}

	// Fall back to local refs (no remote configured)
	if sha, err := g.run("merge-base", "HEAD", "main"); err == nil {
		return sha, nil
	}
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
	return g.runCmd(g.dir, "gh", "pr", "view", "--json", "baseRefName", "-q", ".baseRefName")
}

// BehindCount returns the number of commits the current branch is behind the
// given base ref (e.g. "origin/main"). Returns 0 if not applicable.
func (g *Git) BehindCount(baseRef string) int {
	out, err := g.run("rev-list", "--count", "HEAD.."+baseRef)
	if err != nil {
		return 0
	}
	var count int
	fmt.Sscanf(out, "%d", &count)
	return count
}

// ChangedFilesResult separates committed and uncommitted file changes.
type ChangedFilesResult struct {
	Committed   []string // files changed in base..HEAD only
	Uncommitted []string // files with working tree changes
	Deleted     []string // files deleted in base..HEAD (subset of Committed)
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

	// Detect deleted files (in base..HEAD)
	deletedSet := make(map[string]bool)
	out, err = g.run("diff", "--name-only", "--diff-filter=D", base+"..HEAD")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				deletedSet[f] = true
			}
		}
	}

	var deleted []string
	for _, f := range committed {
		if deletedSet[f] {
			deleted = append(deleted, f)
		}
	}

	sort.Strings(committed)
	sort.Strings(uncommitted)
	sort.Strings(deleted)

	return ChangedFilesResult{
		Committed:   committed,
		Uncommitted: uncommitted,
		Deleted:     deleted,
	}, nil
}

// FileDiffCommitted returns the diff for a committed file between base and HEAD.
func (g *Git) FileDiffCommitted(base, file string) (string, error) {
	return g.run("diff", base+"..HEAD", "--", file)
}

// FileDiffUncommitted returns the working tree diff for a file against HEAD.
// If file is empty, returns the diff for all files.
func (g *Git) FileDiffUncommitted(file string) (string, error) {
	// Try tracked diff first (staged + unstaged vs HEAD)
	var diff string
	var err error
	if file == "" {
		diff, err = g.run("diff", "HEAD")
	} else {
		diff, err = g.run("diff", "HEAD", "--", file)
	}
	if err == nil && diff != "" {
		return diff, nil
	}
	// For untracked files, diff against /dev/null.
	// git diff --no-index exits 1 when differences exist, so we capture output manually.
	cmd := exec.Command("git", "diff", "--no-index", "/dev/null", file)
	cmd.Dir = g.dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Run() // ignore exit code — 1 means "differences found"
	out := stdout.String()
	if out != "" {
		return out, nil
	}
	return "", fmt.Errorf("no diff available for %s", file)
}

// Commits returns the list of commits between base and HEAD, newest first.
// If no commits exist in the range (e.g. on main), falls back to last 10 commits.
// AllCommits returns the full commit history of HEAD, capped at 1000 entries.
func (g *Git) AllCommits() ([]Commit, error) {
	out, err := g.run("log", "-n", "1000", "--format=%H %s", "HEAD")
	if err != nil {
		return nil, err
	}
	return parseCommitLog(out), nil
}

func (g *Git) Commits(base string) ([]Commit, error) {
	out, err := g.run("log", "--format=%H %s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	commits := parseCommitLog(out)
	if len(commits) == 0 {
		// On the base branch itself — show full commit history (capped at 1000)
		out, err = g.run("log", "-n", "1000", "--format=%H %s", "HEAD")
		if err != nil {
			return nil, err
		}
		commits = parseCommitLog(out)
	}
	return commits, nil
}

// BaseCommits returns commits from the base branch that are already in the
// history (before the feature branch diverged). Limited to a reasonable count.
func (g *Git) BaseCommits(base string, limit int) ([]Commit, error) {
	out, err := g.run("log", "-n", fmt.Sprintf("%d", limit), "--format=%H %s", base)
	if err != nil {
		return nil, err
	}
	return parseCommitLog(out), nil
}

func parseCommitLog(out string) []Commit {
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
	return commits
}

// CommitPatch returns the full patch for a single commit.
func (g *Git) CommitPatch(sha string) (string, error) {
	return g.run("show", sha)
}

// FileContent returns the full content of a file from the working tree.
// Falls back to HEAD version if the working tree read fails.
func (g *Git) FileContent(file string) (string, error) {
	fullPath := filepath.Join(g.dir, file)
	// Check if path is a directory
	if info, err := os.Stat(fullPath); err == nil && info.IsDir() {
		return "", fmt.Errorf("%s is a directory", file)
	}
	// Read from working tree directly (handles uncommitted/untracked files)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		// Fall back to HEAD version
		return g.run("show", "HEAD:"+file)
	}
	return string(content), nil
}

// AllFiles returns all files in the repo (tracked + untracked).
// If includeIgnored is true, gitignored files are also included.
// Results are sorted alphabetically.
func (g *Git) AllFiles(includeIgnored bool) ([]string, error) {
	fileSet := make(map[string]bool)

	// Tracked files
	out, err := g.run("ls-files")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(out, "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			fileSet[f] = true
		}
	}

	// Untracked files (excluding ignored)
	out, err = g.run("ls-files", "--others", "--exclude-standard")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				fileSet[f] = true
			}
		}
	}

	// Ignored files
	if includeIgnored {
		out, err = g.run("ls-files", "--others", "--ignored", "--exclude-standard")
		if err == nil {
			for _, f := range strings.Split(out, "\n") {
				f = strings.TrimSpace(f)
				if f != "" {
					fileSet[f] = true
				}
			}
		}
	}

	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// PRInfo fetches PR info via gh CLI. Returns zero-value PRInfoResult if no PR exists.
// Returns an error if the gh command fails for reasons other than "no PR" (e.g. rate limiting, auth issues).
func (g *Git) PRInfo() (PRInfoResult, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "view", "--json", "number,title,url,state,baseRefName,isDraft,reviewDecision,body")
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		// These errors mean genuinely no PR or no remote — not a transient failure
		if strings.Contains(errMsg, "no pull requests found") ||
			strings.Contains(errMsg, "no open pull requests") ||
			strings.Contains(errMsg, "not a git repository") ||
			strings.Contains(errMsg, "none of the remotes") ||
			strings.Contains(errMsg, "no github remotes") ||
			strings.Contains(errMsg, "no git remotes") ||
			strings.Contains(errMsg, "could not determine") ||
			strings.Contains(errMsg, "gh not found") ||
			strings.Contains(errMsg, "executable file not found") {
			return PRInfoResult{}, nil
		}
		// Everything else (rate limit, auth, network) is a real error
		return PRInfoResult{}, err
	}
	var result PRInfoResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return PRInfoResult{}, fmt.Errorf("parsing PR info: %w", err)
	}
	return result, nil
}

// PRChecks fetches CI check status for the current PR.
func (g *Git) PRChecks() (CIStatusResult, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "checks", "--json", "name,state,conclusion,detailsUrl")
	if err != nil {
		return CIStatusResult{}, nil
	}

	var checks []struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		Conclusion string `json:"conclusion"`
		URL        string `json:"detailsUrl"`
	}
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		return CIStatusResult{}, nil
	}

	// Aggregate: if any failed, overall is FAILURE; if any pending, PENDING; else SUCCESS
	result := CIStatusResult{State: "SUCCESS"}
	for _, c := range checks {
		if c.Conclusion == "failure" || c.Conclusion == "action_required" {
			result.State = "FAILURE"
			result.URL = c.URL
			return result, nil
		}
		if c.State == "PENDING" || c.State == "QUEUED" || c.State == "IN_PROGRESS" {
			result.State = "PENDING"
			if result.URL == "" {
				result.URL = c.URL
			}
		}
	}
	if len(checks) > 0 && result.URL == "" {
		result.URL = checks[0].URL
	}
	return result, nil
}

// PRReviews fetches review information for the current PR.
func (g *Git) PRReviews() ([]PRReview, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "view", "--json", "reviews", "-q", ".reviews[] | {author: .author.login, state: .state}")
	if err != nil {
		return nil, nil
	}

	// gh outputs NDJSON, one object per line
	var reviews []PRReview
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r PRReview
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}

// gitRemoteToHTTPS converts a git remote URL to an HTTPS URL.
// Handles SSH (git@github.com:user/repo.git) and HTTPS formats.
func gitRemoteToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")

	// SSH format: git@github.com:user/repo
	if strings.HasPrefix(remote, "git@") {
		remote = strings.TrimPrefix(remote, "git@")
		remote = strings.Replace(remote, ":", "/", 1)
		return "https://" + remote
	}

	// Already HTTPS
	if strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://") {
		return remote
	}

	return ""
}

// PRCommentCount fetches the number of comments on the current PR.
func (g *Git) PRCommentCount() (int, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "view", "--json", "comments", "-q", ".comments | length")
	if err != nil {
		return 0, nil
	}
	var count int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &count)
	return count, nil
}

// PRComments fetches comments on the current PR.
func (g *Git) PRComments() ([]PRComment, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "view", "--json", "comments", "-q", ".comments[] | {author: .author.login, body: .body}")
	if err != nil {
		return nil, nil
	}
	var comments []PRComment
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c PRComment
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// CIChecks fetches individual CI check results for the current PR.
func (g *Git) CIChecks() ([]CICheck, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "checks", "--json", "name,state,conclusion,detailsUrl")
	if err != nil {
		return nil, nil
	}
	var checks []CICheck
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		return nil, nil
	}
	return checks, nil
}
