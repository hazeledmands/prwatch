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
	"testing"
	"time"
)

// CmdRunner executes an external command and returns its stdout.
// The default implementation uses exec.Command.
type CmdRunner func(dir string, name string, args ...string) (string, error)

// Git wraps git CLI operations for a specific working directory.
type Git struct {
	dir          string
	runCmd       CmdRunner // for running non-git commands (e.g. gh)
	cachedGHBase string    // cached result from gh pr view --baseRefName
	hasGHBase    bool      // true once we've queried gh for the base ref
}

func New(dir string) *Git {
	return &Git{dir: dir, runCmd: defaultCmdRunner}
}

// NewWithRunner creates a Git instance with a custom command runner for testing.
func NewWithRunner(dir string, runner CmdRunner) *Git {
	return &Git{dir: dir, runCmd: runner}
}

// RunCmd exposes the command runner for testing.
func (g *Git) RunCmd(name string, args ...string) (string, error) {
	return g.runCmd(g.dir, name, args...)
}

func defaultCmdRunner(dir string, name string, args ...string) (string, error) {
	if testing.Testing() && (name == "gh" || name == "rwx") {
		panic(fmt.Sprintf("test called real %s command (use NewWithRunner to stub): %s %s", name, name, strings.Join(args, " ")))
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		out := strings.TrimSpace(stdout.String())
		if errMsg != "" {
			return out, fmt.Errorf("%s: %s", err, strings.TrimSpace(errMsg))
		}
		return out, err
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
	Number         int         `json:"number"`
	Title          string      `json:"title"`
	URL            string      `json:"url"`
	State          string      `json:"state"`
	BaseRef        string      `json:"baseRefName"`
	IsDraft        bool        `json:"isDraft"`
	ReviewDecision string      `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	CommentsCount  int         `json:"comments"`
	Body           string      `json:"body"`
	Labels         []PRLabel   `json:"labels"`
	Assignees      []PRUser    `json:"assignees"`
	Milestone      PRMilestone `json:"milestone"`
	MergedBy       *PRUser     `json:"mergedBy"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	MergedAt       time.Time   `json:"mergedAt"`
	ClosedAt       time.Time   `json:"closedAt"`
}

type PRLabel struct {
	Name string `json:"name"`
}

type PRUser struct {
	Login string `json:"login"`
}

type PRMilestone struct {
	Title string `json:"title"`
}

type PRComment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	URL       string    `json:"url"`
}

type PRDeployment struct {
	Environment string `json:"environment"`
	State       string `json:"state"` // ACTIVE, INACTIVE, ERROR, QUEUED, IN_PROGRESS, etc.
	URL         string `json:"url"`   // deployment URL (logUrl in GraphQL)
}

type CICheck struct {
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Bucket      string    `json:"bucket"` // pass, fail, pending, skipping, cancel
	URL         string    `json:"link"`
	CompletedAt time.Time `json:"completedAt"`
	StartedAt   time.Time `json:"startedAt"`
}

type CIStatusResult struct {
	State string // SUCCESS, FAILURE, PENDING, ""
	URL   string // link to the CI run
}

// RWXResult represents the result of an RWX CI run.
type RWXResult struct {
	RunID       string
	Status      string // passed, failed
	FailedTasks []RWXFailedTask
}

// RWXFailedTask represents a failed task in an RWX run.
type RWXFailedTask struct {
	Key          string
	TaskID       string
	HasArtifacts bool
}

// RWXFailedTest represents a single failed test extracted from RWX test-results artifacts.
type RWXFailedTest struct {
	Name   string
	Scope  string
	Stdout string
}

type PRReview struct {
	Author      string            `json:"author"`
	State       string            `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, PENDING
	Body        string            `json:"body"`
	SubmittedAt time.Time         `json:"submittedAt"`
	URL         string            `json:"url"`
	Comments    []PRReviewComment `json:"comments"`
}

// PRReviewComment is an inline code comment attached to a review.
type PRReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// PRReviewRequest represents a pending review request on a PR.
type PRReviewRequest struct {
	Name   string // display name (login for users, team name for teams)
	IsTeam bool
}

// PRAllResult holds everything from a single consolidated gh pr view call.
type PRAllResult struct {
	Info           PRInfoResult
	Reviews        []PRReview
	ReviewRequests []PRReviewRequest
	Comments       []PRComment
	CommentCount   int
	Deployments    []PRDeployment
}

// PRChecksResult holds both the raw CI checks and the aggregated status.
type PRChecksResult struct {
	Checks []CICheck
	Status CIStatusResult
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
	if g.hasGHBase {
		return g.cachedGHBase, nil
	}
	base, err := g.runCmd(g.dir, "gh", "pr", "view", "--json", "baseRefName", "-q", ".baseRefName")
	if err != nil {
		// Cache empty result so we don't keep calling gh when there's no PR.
		// The cache gets refreshed when PRAll succeeds.
		g.cachedGHBase = ""
		g.hasGHBase = true
		return "", err
	}
	g.cachedGHBase = base
	g.hasGHBase = true
	return base, nil
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
	Uncommitted []string // unstaged or untracked files (new changes)
	Staged      []string // staged but uncommitted files
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

	// Get staged changes (index vs HEAD)
	stagedSet := make(map[string]bool)
	out, err = g.run("diff", "--name-only", "--cached", "HEAD")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				stagedSet[f] = true
			}
		}
	}

	// Get unstaged changes (working tree vs index)
	unstagedSet := make(map[string]bool)
	out, err = g.run("diff", "--name-only")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				unstagedSet[f] = true
			}
		}
	}
	// Also include untracked files
	out, err = g.run("ls-files", "--others", "--exclude-standard")
	if err == nil {
		for _, f := range strings.Split(out, "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				unstagedSet[f] = true
			}
		}
	}

	// Files in both committed and any local change go to the local bucket only
	allLocalSet := make(map[string]bool)
	for f := range stagedSet {
		allLocalSet[f] = true
	}
	for f := range unstagedSet {
		allLocalSet[f] = true
	}

	var committed, uncommitted, staged []string
	for f := range committedSet {
		if allLocalSet[f] {
			continue // will be in staged or uncommitted list
		}
		committed = append(committed, f)
	}
	for f := range unstagedSet {
		uncommitted = append(uncommitted, f)
	}
	for f := range stagedSet {
		if unstagedSet[f] {
			continue // file is in both — show in uncommitted (new changes) only
		}
		staged = append(staged, f)
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
	sort.Strings(staged)
	sort.Strings(deleted)

	return ChangedFilesResult{
		Committed:   committed,
		Uncommitted: uncommitted,
		Staged:      staged,
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

// AllCommits returns the commit history of HEAD with pagination.
func (g *Git) AllCommits(skip, limit int) ([]Commit, error) {
	out, err := g.run("log", "--skip", fmt.Sprintf("%d", skip), "-n", fmt.Sprintf("%d", limit), "--format=%H %s", "HEAD")
	if err != nil {
		return nil, err
	}
	return parseCommitLog(out), nil
}

// Commits returns the list of commits between base and HEAD, newest first.
// If no commits exist in the range (e.g. on main), falls back to AllCommits.
func (g *Git) Commits(base string, skip, limit int) ([]Commit, error) {
	out, err := g.run("log", "--skip", fmt.Sprintf("%d", skip), "-n", fmt.Sprintf("%d", limit), "--format=%H %s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	commits := parseCommitLog(out)
	if skip == 0 && len(commits) == 0 {
		// On the base branch itself — show full commit history
		return g.AllCommits(skip, limit)
	}
	return commits, nil
}

// CommitCount returns the total number of commits reachable from HEAD.
func (g *Git) CommitCount() (int, error) {
	out, err := g.run("rev-list", "--count", "HEAD")
	if err != nil {
		return 0, err
	}
	var count int
	fmt.Sscanf(out, "%d", &count)
	return count, nil
}

// CommitCountRange returns the number of commits in base..HEAD.
// If the range is empty (on the base branch), falls back to CommitCount.
func (g *Git) CommitCountRange(base string) (int, error) {
	out, err := g.run("rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0, err
	}
	var count int
	fmt.Sscanf(out, "%d", &count)
	if count == 0 {
		return g.CommitCount()
	}
	return count, nil
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

// PRAll fetches all PR data in a single gh pr view call.
// Returns zero-value PRAllResult if no PR exists.
// Returns an error if the gh command fails for reasons other than "no PR" (e.g. rate limiting, auth issues).
func (g *Git) PRAll() (PRAllResult, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "view", "--json",
		"number,title,url,state,baseRefName,isDraft,reviewDecision,body,labels,assignees,milestone,mergedBy,reviews,reviewRequests,comments,createdAt,updatedAt,mergedAt,closedAt")
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
			return PRAllResult{}, nil
		}
		// Everything else (rate limit, auth, network) is a real error
		return PRAllResult{}, err
	}

	// Parse the combined JSON response
	var raw struct {
		PRInfoResult
		Reviews []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			State       string    `json:"state"`
			Body        string    `json:"body"`
			SubmittedAt time.Time `json:"submittedAt"`
		} `json:"reviews"`
		ReviewRequests []struct {
			TypeName string `json:"__typename"`
			Login    string `json:"login"` // for User
			Name     string `json:"name"`  // for Team
		} `json:"reviewRequests"`
		Comments []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"createdAt"`
			URL       string    `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return PRAllResult{}, fmt.Errorf("parsing PR info: %w", err)
	}

	result := PRAllResult{
		Info:         raw.PRInfoResult,
		CommentCount: len(raw.Comments),
	}

	// Try to fetch reviews with inline comments via GraphQL.
	// Falls back to the basic review data from gh pr view if this fails.
	if reviewsWithComments, err := g.fetchReviewsGraphQL(result.Info.Number); err == nil && len(reviewsWithComments) > 0 {
		result.Reviews = reviewsWithComments
	} else {
		for _, r := range raw.Reviews {
			result.Reviews = append(result.Reviews, PRReview{
				Author:      r.Author.Login,
				State:       r.State,
				Body:        r.Body,
				SubmittedAt: r.SubmittedAt,
			})
		}
	}

	for _, rr := range raw.ReviewRequests {
		name := rr.Login
		isTeam := false
		if rr.TypeName == "Team" {
			name = rr.Name
			isTeam = true
		}
		if name != "" {
			result.ReviewRequests = append(result.ReviewRequests, PRReviewRequest{Name: name, IsTeam: isTeam})
		}
	}

	for _, c := range raw.Comments {
		result.Comments = append(result.Comments, PRComment{
			Author:    c.Author.Login,
			CreatedAt: c.CreatedAt,
			Body:      c.Body,
			URL:       c.URL,
		})
	}

	// Fetch deployments for this PR (best-effort, don't fail if this errors)
	if deploys, err := g.fetchDeployments(result.Info.Number); err == nil {
		result.Deployments = deploys
	}

	// Update the cached base ref so DetectBase doesn't need to call gh
	if result.Info.BaseRef != "" {
		g.cachedGHBase = result.Info.BaseRef
		g.hasGHBase = true
	}

	return result, nil
}

// fetchDeployments fetches deployment statuses for a PR using the GitHub GraphQL API.
// This consolidates with the existing gh api pattern to minimize separate REST calls.
func (g *Git) fetchDeployments(prNumber int) ([]PRDeployment, error) {
	// Use GraphQL to get deployment statuses for the PR's head commit.
	// This avoids a separate REST call for owner/repo resolution.
	query := fmt.Sprintf(`query {
		repository(owner: "{owner}", name: "{repo}") {
			pullRequest(number: %d) {
				commits(last: 1) {
					nodes {
						commit {
							deployments(last: 10) {
								nodes {
									environment
									state
									latestStatus {
										state
										logUrl
									}
								}
							}
						}
					}
				}
			}
		}
	}`, prNumber)

	// Resolve owner/repo via gh repo view
	nwoOut, err := g.runCmd(g.dir, "gh", "repo", "view", "--json", "owner,name", "--jq", ".owner.login + \"/\" + .name")
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(nwoOut), "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected repo format: %q", nwoOut)
	}
	owner, repo := parts[0], parts[1]

	// Replace placeholders in query
	query = strings.ReplaceAll(query, "{owner}", owner)
	query = strings.ReplaceAll(query, "{repo}", repo)

	out, err := g.runCmd(g.dir, "gh", "api", "graphql", "-f", "query="+query)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					Commits struct {
						Nodes []struct {
							Commit struct {
								Deployments struct {
									Nodes []struct {
										Environment  string `json:"environment"`
										State        string `json:"state"`
										LatestStatus *struct {
											State  string `json:"state"`
											LogURL string `json:"logUrl"`
										} `json:"latestStatus"`
									} `json:"nodes"`
								} `json:"deployments"`
							} `json:"commit"`
						} `json:"nodes"`
					} `json:"commits"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, err
	}

	var deployments []PRDeployment
	for _, commitNode := range resp.Data.Repository.PullRequest.Commits.Nodes {
		for _, d := range commitNode.Commit.Deployments.Nodes {
			deploy := PRDeployment{
				Environment: d.Environment,
				State:       d.State,
			}
			if d.LatestStatus != nil {
				deploy.State = d.LatestStatus.State
				deploy.URL = d.LatestStatus.LogURL
			}
			deployments = append(deployments, deploy)
		}
	}

	return deployments, nil
}

// fetchReviewsGraphQL fetches reviews with their inline comments via the GitHub GraphQL API.
func (g *Git) fetchReviewsGraphQL(prNumber int) ([]PRReview, error) {
	// Resolve owner/repo via gh so we don't have to parse git remotes ourselves.
	nwoOut, err := g.runCmd(g.dir, "gh", "repo", "view", "--json", "owner,name", "--jq", ".owner.login + \"/\" + .name")
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(nwoOut), "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected repo format: %q", nwoOut)
	}
	owner, repo := parts[0], parts[1]

	query := fmt.Sprintf(`query {
  repository(owner: %q, name: %q) {
    pullRequest(number: %d) {
      reviews(first: 50) {
        nodes {
          author { login }
          state
          body
          submittedAt
          url
          comments(first: 100) {
            nodes {
              path
              line
              body
            }
          }
        }
      }
    }
  }
}`, owner, repo, prNumber)

	out, err := g.runCmd(g.dir, "gh", "api", "graphql", "-f", "query="+query)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					Reviews struct {
						Nodes []struct {
							Author struct {
								Login string `json:"login"`
							} `json:"author"`
							State       string    `json:"state"`
							Body        string    `json:"body"`
							SubmittedAt time.Time `json:"submittedAt"`
							URL         string    `json:"url"`
							Comments    struct {
								Nodes []struct {
									Path string `json:"path"`
									Line int    `json:"line"`
									Body string `json:"body"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviews"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, err
	}

	var reviews []PRReview
	for _, r := range resp.Data.Repository.PullRequest.Reviews.Nodes {
		review := PRReview{
			Author:      r.Author.Login,
			State:       r.State,
			Body:        r.Body,
			SubmittedAt: r.SubmittedAt,
			URL:         r.URL,
		}
		for _, c := range r.Comments.Nodes {
			review.Comments = append(review.Comments, PRReviewComment{
				Path: c.Path,
				Line: c.Line,
				Body: c.Body,
			})
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// PRChecksAll fetches CI checks in a single gh pr checks call, returning
// both the individual checks and an aggregated status summary.
func (g *Git) PRChecksAll() (PRChecksResult, error) {
	out, err := g.runCmd(g.dir, "gh", "pr", "checks", "--json", "name,state,bucket,link,completedAt,startedAt")
	if err != nil {
		return PRChecksResult{}, nil
	}

	var checks []CICheck
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		return PRChecksResult{}, nil
	}

	// Aggregate: if any failed, overall is FAILURE; if any pending, PENDING; else SUCCESS
	status := CIStatusResult{State: "SUCCESS"}
	for _, c := range checks {
		if c.Bucket == "fail" || c.Bucket == "cancel" {
			status.State = "FAILURE"
			status.URL = c.URL
			return PRChecksResult{Checks: checks, Status: status}, nil
		}
		if c.Bucket == "pending" {
			status.State = "PENDING"
			if status.URL == "" {
				status.URL = c.URL
			}
		}
	}
	if len(checks) > 0 && status.URL == "" {
		status.URL = checks[0].URL
	}
	return PRChecksResult{Checks: checks, Status: status}, nil
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

// IsRWXURL returns true if the URL points to an RWX CI run.
func IsRWXURL(url string) bool {
	return strings.Contains(url, "cloud.rwx.com/mint/")
}

// ExtractRWXRunID extracts the run ID from an RWX URL.
// URL format: https://cloud.rwx.com/mint/<org>/runs/<run-id>
func ExtractRWXRunID(url string) string {
	if !IsRWXURL(url) {
		return ""
	}
	idx := strings.Index(url, "/runs/")
	if idx < 0 {
		return ""
	}
	runID := url[idx+len("/runs/"):]
	// Remove any trailing path or query
	if i := strings.IndexAny(runID, "/?#"); i >= 0 {
		runID = runID[:i]
	}
	return runID
}

// RWXResults fetches the result of an RWX run using the rwx CLI.
func (g *Git) RWXResults(runID string) (*RWXResult, error) {
	out, err := g.runCmd(g.dir, "rwx", "runs", runID, "--output", "text")
	if err != nil {
		// rwx results exits 1 on failure, but still outputs useful data
		if out == "" {
			return nil, err
		}
	}

	result := &RWXResult{RunID: runID}

	// Parse output
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Run result status:") {
			result.Status = strings.TrimSpace(strings.TrimPrefix(line, "Run result status:"))
		}
		// Failed task lines: "- ci.lint-go (task-id: c60819ffe21693dda97241c55b0a8f2e)"
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "(task-id:") {
			taskLine := strings.TrimPrefix(line, "- ")
			parts := strings.SplitN(taskLine, " (task-id: ", 2)
			if len(parts) == 2 {
				taskID, _, _ := strings.Cut(parts[1], ")")
				result.FailedTasks = append(result.FailedTasks, RWXFailedTask{
					Key:          parts[0],
					TaskID:       taskID,
					HasArtifacts: strings.Contains(line, "(has artifacts)"),
				})
			}
		}
	}
	return result, nil
}

// RWXTaskLog fetches the log for a specific RWX task.
func (g *Git) RWXTaskLog(taskID string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "prwatch-rwx-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = g.runCmd(g.dir, "rwx", "logs", taskID, "--output-dir", tmpDir)
	if err != nil {
		return "", err
	}

	// Read all .log files from the output dir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}
	var content strings.Builder
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
			data, err := os.ReadFile(filepath.Join(tmpDir, entry.Name()))
			if err != nil {
				continue
			}
			if content.Len() > 0 {
				content.WriteString("\n\n--- " + entry.Name() + " ---\n\n")
			}
			content.Write(data)
		}
	}
	return content.String(), nil
}

// RWXTestResults downloads test-results artifacts for a task and returns the failed tests.
func (g *Git) RWXTestResults(taskID string) ([]RWXFailedTest, error) {
	// List artifacts to find test-results
	listOut, err := g.runCmd(g.dir, "rwx", "artifacts", "list", taskID, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("listing artifacts: %w", err)
	}

	var artifactList struct {
		Artifacts []struct {
			Key  string `json:"Key"`
			Kind string `json:"Kind"`
		} `json:"Artifacts"`
	}
	if err := json.Unmarshal([]byte(listOut), &artifactList); err != nil {
		return nil, fmt.Errorf("parsing artifact list: %w", err)
	}

	// Download each artifact and look for test-results JSON files
	tmpDir, err := os.MkdirTemp("", "prwatch-rwx-artifacts-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var allFailed []RWXFailedTest
	for _, artifact := range artifactList.Artifacts {
		artDir := filepath.Join(tmpDir, artifact.Key)
		_, err := g.runCmd(g.dir, "rwx", "artifacts", "download", taskID, artifact.Key,
			"--auto-extract", "--output-dir", artDir)
		if err != nil {
			continue
		}

		// Walk the download dir for JSON files that look like test results
		filepath.Walk(artDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
				return nil
			}
			failed, _ := parseTestResultsFile(path)
			allFailed = append(allFailed, failed...)
			return nil
		})
	}

	return allFailed, nil
}

// parseTestResultsFile reads an RWX test-results JSON file and extracts failed tests.
func parseTestResultsFile(path string) ([]RWXFailedTest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var results struct {
		Tests []struct {
			Name    string `json:"name"`
			Scope   string `json:"scope"`
			Attempt struct {
				Status struct {
					Kind string `json:"kind"`
				} `json:"status"`
				Stdout string `json:"stdout"`
			} `json:"attempt"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}

	var failed []RWXFailedTest
	for _, t := range results.Tests {
		if t.Attempt.Status.Kind == "failed" {
			failed = append(failed, RWXFailedTest{
				Name:   t.Name,
				Scope:  t.Scope,
				Stdout: t.Attempt.Stdout,
			})
		}
	}
	return failed, nil
}
