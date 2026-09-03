package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hazeledmands/prwatch/internal/command"
)

// Git wraps git CLI operations for a specific working directory.
type Git struct {
	dir        string
	cmdFactory command.Factory
}

func New(dir string) *Git {
	return &Git{dir: dir, cmdFactory: command.DefaultFactory}
}

// NewWithFactory creates a Git instance with a custom command factory for testing.
func NewWithFactory(dir string, factory command.Factory) *Git {
	return &Git{dir: dir, cmdFactory: factory}
}

// runExternal runs an arbitrary external command and returns stdout, using the
// same capture pattern as run(). Used for gh, rwx, etc.
func (g *Git) runExternal(name string, args ...string) (string, error) {
	cmd := g.cmdFactory(name, args...)
	cmd.SetDir(g.dir)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		out := strings.TrimSpace(stdout.String())
		if errMsg != "" {
			// %w, not %s: the message is unchanged, but callers can still ask
			// what kind of failure this was (errors.Is(err,
			// context.DeadlineExceeded) for a killed subprocess).
			return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(errMsg))
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
	IsEmpty        bool // true if repo has no commits yet (unborn HEAD)
	AheadCount     int  // commits ahead of upstream
}

type Commit struct {
	SHA        string
	Subject    string
	Author     string
	AuthorDate time.Time
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
	// CommentsTotal is how many inline comments GitHub says this review has.
	// Equal to len(Comments) in the ordinary case; larger when the review has
	// more inline comments than one page carries, which is the signal the UI
	// renders as "showing N of M". Never smaller.
	CommentsTotal int `json:"commentsTotal"`
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
	// ReviewsErr is set when the reviews fetch failed partway through
	// pagination and Reviews holds the pages gathered before that. The PR
	// fetch as a whole succeeded, so the caller must apply the data *and*
	// report this: dropping the partial made a truncated list render as a
	// complete one with no error anywhere.
	ReviewsErr error
	// ReviewsTotal is how many reviews GitHub says the PR has, which can
	// exceed len(Reviews) when the paginated fetch stopped at its page cap.
	// The fallback path can't know a total, so it reports len(Reviews) —
	// every path leaves ReviewsTotal >= len(Reviews), and equality means
	// "nothing was dropped".
	ReviewsTotal int
}

// PRChecksResult holds both the raw CI checks and the aggregated status.
type PRChecksResult struct {
	Checks []CICheck
	Status CIStatusResult
}

// noOptionalLocks is prefixed to every git invocation this package makes.
//
// Every call here is a read, but several of them are not read-*only* to git:
// `status` and `diff` refresh `.git/index` when they find stat-dirty entries,
// recording what they learned by opening the files. prwatch watches `.git`, so
// a load's own status call wrote the index, the watcher saw the write, and that
// fired another load — a self-sustaining refresh loop driven by nothing but
// prwatch reading the repo. `--no-optional-locks` (git's own switch for exactly
// this, equivalent to GIT_OPTIONAL_LOCKS=0) makes the reads leave no trace.
//
// Applied at the two run primitives rather than per-subcommand: the whole
// package is read-only, so the invariant "prwatch never takes an optional git
// lock" is both true and cheap to assert. If a mutating git call is ever added
// here it must not go through run/runZ.
const noOptionalLocks = "--no-optional-locks"

func (g *Git) run(args ...string) (string, error) {
	cmd := g.cmdFactory("git", append([]string{noOptionalLocks}, args...)...)
	cmd.SetDir(g.dir)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s %w", strings.Join(args, " "), stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runDiff runs a git command whose *output text* contains paths — a diff or a
// patch — with path quoting disabled.
//
// This is the display-side counterpart to runZ. runZ fixes the listing side:
// paths git hands us as data. But a diff body carries paths inside its own
// headers (`diff --git a/x b/x`, `--- a/x`, `+++ b/x`), and those are subject
// to core.quotePath too — so a file named ünï.go renders on screen as
// `diff --git "a/\303\274n\303\257.go" ...`. There is no -z for a diff body;
// -c core.quotePath=false is the switch, and it must be set per-invocation
// because a user's global config is what turns the quoting on.
//
// Not folded into run(): run()'s other callers parse paths out of
// newline-delimited output, where quoting is load-bearing for disambiguation.
// Only the diff/patch producers want this.
func (g *Git) runDiff(args ...string) (string, error) {
	return g.run(append([]string{"-c", "core.quotePath=false"}, args...)...)
}

// splitNUL splits NUL-delimited (`-z`) git output into records, dropping every
// empty record — which in practice means the one left after the final
// terminator, since git emits no empty fields in the formats used here. The
// parsers rely on that: an empty record would otherwise be read as a status
// token or a path.
//
// Records are returned verbatim. Unlike the newline-delimited forms, `-z`
// output must not be trimmed: a path may legitimately begin or end with
// whitespace, and with -z git no longer quotes it to make that visible.
func splitNUL(out string) []string {
	var recs []string
	for _, r := range strings.Split(out, "\x00") {
		if r == "" {
			continue
		}
		recs = append(recs, r)
	}
	return recs
}

// runZ runs a git command that was passed -z and returns its NUL-delimited
// records.
//
// Every path-producing git call must go through here rather than run(). Git
// quotes any path containing non-ASCII bytes, a tab, or a quote by default
// (core.quotePath), emitting `"caf\303\251.txt"` for café.txt — which the
// newline-splitting callers passed through verbatim, so the escaped form
// reached the sidebar and then failed to resolve as a real filename. -z both
// suppresses the quoting and removes the ambiguity of a path that contains the
// delimiter.
func (g *Git) runZ(args ...string) ([]string, error) {
	cmd := g.cmdFactory("git", append([]string{noOptionalLocks}, args...)...)
	cmd.SetDir(g.dir)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %s %w", strings.Join(args, " "), stderr.String(), err)
	}
	return splitNUL(stdout.String()), nil
}

// IsRepo returns true if the directory is inside a git repository.
func (g *Git) IsRepo() bool {
	_, err := g.run("rev-parse", "--git-dir")
	return err == nil
}

func (g *Git) RepoInfo() (RepoInfoResult, error) {
	var isEmpty bool
	branch, err := g.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		// Empty repo (no commits): rev-parse fails but symbolic-ref works
		branch, err = g.run("symbolic-ref", "--short", "HEAD")
		if err != nil {
			return RepoInfoResult{}, err
		}
		isEmpty = true
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
		IsEmpty:        isEmpty,
		AheadCount:     aheadCount,
	}, nil
}

// DetectBaseLocal finds the merge-base commit between HEAD and a base branch,
// using only local git state (no GitHub API calls). Intended as the synchronous
// startup path; callers can upgrade to a PR-reported base via DetectBaseFromPR
// once PR data has been fetched asynchronously.
//
// Tries in order: origin/main → origin/master → local main → local master →
// HEAD~1. Returns the merge-base SHA of the first ref that succeeds.
func (g *Git) DetectBaseLocal() (string, error) {
	refs := []string{"origin/main", "origin/master", "main", "master"}
	for _, ref := range refs {
		if sha, err := g.run("merge-base", "HEAD", ref); err == nil {
			return sha, nil
		}
	}
	// Fallback to HEAD~1 — the "delta" is just the latest commit.
	sha, err := g.run("rev-parse", "HEAD~1")
	if err != nil {
		return "", fmt.Errorf("cannot detect base branch: %w", err)
	}
	return sha, nil
}

// DetectBaseFromPR computes the merge-base for a specific base branch name
// (typically the baseRefName returned by gh pr view). Prefers origin/<base> to
// stay consistent with GitHub's three-dot diff; falls back to the local <base>
// ref if origin isn't available.
//
// Returns an error if neither ref produces a valid merge-base. Caller should
// fall back to DetectBaseLocal in that case.
func (g *Git) DetectBaseFromPR(baseRefName string) (string, error) {
	if baseRefName == "" {
		return "", fmt.Errorf("empty base ref name")
	}
	if sha, err := g.run("merge-base", "HEAD", "origin/"+baseRefName); err == nil {
		return sha, nil
	}
	if sha, err := g.run("merge-base", "HEAD", baseRefName); err == nil {
		return sha, nil
	}
	return "", fmt.Errorf("no merge-base for %q (tried origin/%s and %s)", baseRefName, baseRefName, baseRefName)
}

// BehindCount returns the number of commits the current branch is behind the
// given base ref (e.g. "origin/main"). Returns 0 if not applicable.
// BehindCount reports how many commits baseRef has that HEAD does not.
// An error means the count is *unknown* (the ref doesn't exist, the fetch
// hasn't happened yet, git failed) — distinct from a known 0, which means
// "up to date". Callers must not render an unknown count as 0.
func (g *Git) BehindCount(baseRef string) (int, error) {
	out, err := g.run("rev-list", "--count", "HEAD.."+baseRef)
	if err != nil {
		return 0, err
	}
	var count int
	if _, err := fmt.Sscanf(out, "%d", &count); err != nil {
		return 0, fmt.Errorf("parsing rev-list count %q: %w", out, err)
	}
	return count, nil
}

// Rename describes a file that was renamed from Old to New. The New path
// appears in exactly one of Committed/Uncommitted/Staged; the Old path does
// not appear in Deleted (it isn't a deletion) and the New path does not
// appear in Added (it isn't a new file). Pure is true when git's similarity
// score is 100 — the file moved without any content edits.
type Rename struct {
	Old  string
	New  string
	Pure bool
}

// ChangedFilesResult is the raw shape returned by git.ChangedFiles, with one
// slice per section/class bucket. Production callers should immediately
// convert to the unified per-file view via ToChangedFiles; the slice shape
// remains primarily because it's convenient for mocks and tests to construct.
type ChangedFilesResult struct {
	Committed   []string // files changed in base..HEAD only
	Uncommitted []string // unstaged or untracked files (new changes)
	Staged      []string // staged but uncommitted files
	Deleted     []string // files deleted in base..HEAD (subset of Committed)
	Added       []string // files that are entirely new additions (untracked, newly added in staged, or pure-add in base..HEAD)
	Renamed     []Rename // files renamed in base..HEAD, staged index, or working tree
}

// ToChangedFiles builds the unified per-file view from the slice fields.
// Section priority for files appearing in multiple buckets: Uncommitted >
// Staged > Committed. Class priority: Renamed > Deleted > Added > Modified.
func (r ChangedFilesResult) ToChangedFiles() *ChangedFiles {
	return buildChangedFiles(r.Committed, r.Uncommitted, r.Staged, r.Deleted, r.Added, r.Renamed)
}

// ChangedFiles returns files changed between base and HEAD, separated by commit status.
// Files that appear in both committed and uncommitted go to Uncommitted only.
// Renames are detected and reported as Rename pairs; the old path is suppressed
// from Deleted and the new path is suppressed from Added so each rename
// surfaces as exactly one entry.
func (g *Git) ChangedFiles(base string) (ChangedFilesResult, error) {
	// All diff calls below pass -M so renames collapse to the new path
	// (the old path is reclassified from D+A to R and dropped from --name-only).
	paths, err := g.runZ("diff", "-M", "--name-only", "-z", base+"..HEAD")
	if err != nil {
		return ChangedFilesResult{}, err
	}

	committedSet := make(map[string]bool)
	for _, f := range paths {
		committedSet[f] = true
	}

	// Get staged changes (index vs HEAD)
	stagedSet := make(map[string]bool)
	if paths, err := g.runZ("diff", "-M", "--name-only", "-z", "--cached", "HEAD"); err == nil {
		for _, f := range paths {
			stagedSet[f] = true
		}
	}

	// Get unstaged changes (working tree vs index)
	unstagedSet := make(map[string]bool)
	if paths, err := g.runZ("diff", "-M", "--name-only", "-z"); err == nil {
		for _, f := range paths {
			unstagedSet[f] = true
		}
	}
	// Also include untracked files (these are inherently new — all additions)
	untrackedSet := make(map[string]bool)
	if paths, err := g.runZ("ls-files", "-z", "--others", "--exclude-standard"); err == nil {
		for _, f := range paths {
			unstagedSet[f] = true
			untrackedSet[f] = true
		}
	}

	// Detect renames before partitioning so we can suppress the old paths
	// from the unstaged/untracked sets (where -M on diff can't reach them
	// because the new side of a working-tree rename may be untracked).
	var renamed []Rename
	if recs, err := g.runZ("diff", "-M", "--name-status", "-z", "--diff-filter=R", base+"..HEAD"); err == nil {
		renamed = append(renamed, parseRenameNameStatus(recs)...)
	}
	if recs, err := g.runZ("diff", "-M", "--name-status", "-z", "--diff-filter=R", "--cached", "HEAD"); err == nil {
		renamed = append(renamed, parseRenameNameStatus(recs)...)
	}
	if recs, err := g.runZ("status", "--porcelain=v2", "-z", "-M", "--untracked-files=all"); err == nil {
		renamed = append(renamed, parsePorcelainV2Renames(recs)...)
	}
	// Fallback: pure working-tree renames (mv without git add) where the new
	// path is untracked. `git diff -M` can't see across that boundary, and
	// `git status --porcelain=v2 -M` won't pair tracked-deleted with untracked
	// either. Catch the pure case (content unchanged) by matching index blob
	// hashes against the working-tree hashes of untracked files.
	renamed = append(renamed, g.detectPureMvRenames(renamed, untrackedSet)...)
	renamed = dedupRenamesByNew(renamed)

	// Strip rename old paths from working-tree-derived sets (-M doesn't cover
	// these when the new side is untracked).
	for _, r := range renamed {
		delete(unstagedSet, r.Old)
		delete(untrackedSet, r.Old)
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

	// Detect deleted files (in base..HEAD). -M reclassifies rename old paths
	// from D to R so they fall out of this list naturally.
	deletedSet := make(map[string]bool)
	if paths, err := g.runZ("diff", "-M", "--name-only", "-z", "--diff-filter=D", base+"..HEAD"); err == nil {
		for _, f := range paths {
			deletedSet[f] = true
		}
	}

	var deleted []string
	for _, f := range committed {
		if deletedSet[f] {
			deleted = append(deleted, f)
		}
	}

	// Detect "pure addition" files: committed files added in base..HEAD,
	// staged files that are newly added to the index, and all untracked files.
	// -M keeps rename new paths out of --diff-filter=A on the tracked side;
	// for working-tree renames where the new path is untracked, we strip below.
	addedSet := make(map[string]bool)
	if paths, err := g.runZ("diff", "-M", "--name-only", "-z", "--diff-filter=A", base+"..HEAD"); err == nil {
		for _, f := range paths {
			addedSet[f] = true
		}
	}
	if paths, err := g.runZ("diff", "-M", "--name-only", "-z", "--diff-filter=A", "--cached", "HEAD"); err == nil {
		for _, f := range paths {
			addedSet[f] = true
		}
	}
	for f := range untrackedSet {
		addedSet[f] = true
	}
	// Working-tree rename targets get reported as untracked above; strip.
	for _, r := range renamed {
		delete(addedSet, r.New)
	}

	var added []string
	for f := range addedSet {
		added = append(added, f)
	}

	sort.Strings(committed)
	sort.Strings(uncommitted)
	sort.Strings(staged)
	sort.Strings(deleted)
	sort.Strings(added)
	sort.Slice(renamed, func(i, j int) bool { return renamed[i].New < renamed[j].New })

	return ChangedFilesResult{
		Committed:   committed,
		Uncommitted: uncommitted,
		Staged:      staged,
		Deleted:     deleted,
		Added:       added,
		Renamed:     renamed,
	}, nil
}

// buildChangedFiles assembles the unified per-file view from the legacy
// section + class slices. Each path lands in exactly one section, with the
// class inferred from membership in the deleted/added/renamed sets.
//
// Section priority when a file is in multiple buckets (which can happen if
// the caller passes overlapping inputs): uncommitted > staged > committed.
// Class priority: renamed > deleted > added > modified.
func buildChangedFiles(committed, uncommitted, staged, deleted, added []string, renamed []Rename) *ChangedFiles {
	out := NewChangedFiles()
	renameByNew := make(map[string]Rename, len(renamed))
	for _, r := range renamed {
		renameByNew[r.New] = r
	}
	addedSet := make(map[string]bool, len(added))
	for _, a := range added {
		addedSet[a] = true
	}
	deletedSet := make(map[string]bool, len(deleted))
	for _, d := range deleted {
		deletedSet[d] = true
	}
	classify := func(path string) (Class, string, bool) {
		if r, ok := renameByNew[path]; ok {
			return ClassRenamed, r.Old, r.Pure
		}
		if deletedSet[path] {
			return ClassDeleted, "", false
		}
		if addedSet[path] {
			return ClassAdded, "", false
		}
		return ClassModified, "", false
	}
	// Iterate by section, highest priority first. The collection's Add
	// replaces any prior entry, so the latest section assignment wins for
	// any path that overlaps (shouldn't happen with the current producer,
	// but the data shape doesn't enforce uniqueness).
	for _, p := range committed {
		cls, oldPath, pure := classify(p)
		out.Add(ChangedFile{Path: p, Section: SectionCommitted, Class: cls, OldPath: oldPath, PureRename: pure})
	}
	for _, p := range staged {
		cls, oldPath, pure := classify(p)
		out.Add(ChangedFile{Path: p, Section: SectionStaged, Class: cls, OldPath: oldPath, PureRename: pure})
	}
	for _, p := range uncommitted {
		cls, oldPath, pure := classify(p)
		out.Add(ChangedFile{Path: p, Section: SectionUncommitted, Class: cls, OldPath: oldPath, PureRename: pure})
	}
	return out
}

// parseRenameNameStatus parses the NUL-delimited records of `git diff -M
// --name-status -z --diff-filter=R`.
//
// With -z each field is its own record rather than a tab-separated column, so
// a record is a status token followed by its path(s): rename and copy statuses
// carry two paths (old then new), every other status carries one. Walking
// records this way — instead of splitting a line on tabs — is what lets a path
// containing a tab, a newline, or non-ASCII bytes survive intact.
func parseRenameNameStatus(recs []string) []Rename {
	var renamed []Rename
	for i := 0; i < len(recs); {
		status := recs[i]
		// R and C statuses are followed by two paths; everything else by one.
		paths := 1
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			paths = 2
		}
		if i+paths >= len(recs) {
			break // truncated record; nothing trustworthy left to read
		}
		if strings.HasPrefix(status, "R") {
			score, _ := strconv.Atoi(status[1:])
			renamed = append(renamed, Rename{Old: recs[i+1], New: recs[i+2], Pure: score >= 100})
		}
		i += paths + 1
	}
	return renamed
}

// parsePorcelainV2Renames parses the NUL-delimited records of `git status
// --porcelain=v2 -z -M`, returning each type-2 (rename/copy) entry where the
// rename is in the working tree or index. A type-2 record has the form:
//
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <newPath>
//
// and — uniquely among porcelain v2 entry types — is followed by a second
// record holding the original path. (Without -z the two are packed into one
// line separated by a tab, which no path containing a tab can survive.)
//
// We accept R in either X or Y position; copies (C) are skipped.
func parsePorcelainV2Renames(recs []string) []Rename {
	var renamed []Rename
	for i := 0; i < len(recs); i++ {
		header := recs[i]
		if !strings.HasPrefix(header, "2 ") {
			continue
		}
		if i+1 >= len(recs) {
			break // rename entry missing its original-path record
		}
		origPath := recs[i+1]
		i++ // consume the original-path record whether or not we accept the entry

		// SplitN rather than Fields: the header is single-space delimited and
		// the 10th field is the new path, which may itself contain spaces.
		fields := strings.SplitN(header, " ", 10)
		if len(fields) < 10 {
			continue
		}
		xy := fields[1]
		if len(xy) < 2 || (xy[0] != 'R' && xy[1] != 'R') {
			continue
		}
		xScore := fields[8]
		if !strings.HasPrefix(xScore, "R") {
			continue // ignore copies (C)
		}
		score, _ := strconv.Atoi(xScore[1:])
		renamed = append(renamed, Rename{Old: origPath, New: fields[9], Pure: score >= 100})
	}
	return renamed
}

// detectPureMvRenames pairs working-tree-deleted tracked files with untracked
// files whose content hashes match — the case where the user ran `mv` (or an
// editor) without `git add`, so both endpoints sit outside what `git diff -M`
// or `git status --porcelain=v2 -M` can pair on their own. Limited to content-
// identical pairs (mv with no edits); rename+edits requires staging.
//
// existing carries renames already found via porcelain v2 / diff so we don't
// double-pair. untrackedSet provides candidate new paths.
func (g *Git) detectPureMvRenames(existing []Rename, untrackedSet map[string]bool) []Rename {
	if len(untrackedSet) == 0 {
		return nil
	}
	pairedOld := make(map[string]bool, len(existing))
	pairedNew := make(map[string]bool, len(existing))
	for _, r := range existing {
		pairedOld[r.Old] = true
		pairedNew[r.New] = true
	}

	// Hash each untracked file once, keyed by blob sha.
	untrackedByHash := make(map[string]string, len(untrackedSet))
	for u := range untrackedSet {
		if pairedNew[u] {
			continue
		}
		h, err := g.run("hash-object", "--", u)
		if err != nil {
			continue
		}
		untrackedByHash[strings.TrimSpace(h)] = u
	}
	if len(untrackedByHash) == 0 {
		return nil
	}

	// Find tracked deletions in the working tree.
	deleted, err := g.runZ("diff", "--name-only", "-z", "--diff-filter=D")
	if err != nil {
		return nil
	}

	var matches []Rename
	for _, old := range deleted {
		if pairedOld[old] {
			continue
		}
		// Read the index blob sha for old.
		stage, err := g.run("ls-files", "--stage", "--", old)
		if err != nil {
			continue
		}
		stage = strings.TrimSpace(stage)
		fields := strings.Fields(stage)
		if len(fields) < 2 {
			continue
		}
		sha := fields[1]
		if newPath, ok := untrackedByHash[sha]; ok {
			matches = append(matches, Rename{Old: old, New: newPath, Pure: true})
			delete(untrackedByHash, sha) // one-to-one pairing
		}
	}
	return matches
}

// dedupRenamesByNew keeps the first occurrence of each new path. Used when
// the same rename appears in both --cached and porcelain-v2 status output.
func dedupRenamesByNew(in []Rename) []Rename {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, r := range in {
		if seen[r.New] {
			continue
		}
		seen[r.New] = true
		out = append(out, r)
	}
	return out
}

// FileDiffCommitted returns the diff for a committed file between base and HEAD.
func (g *Git) FileDiffCommitted(base, file string) (string, error) {
	return g.runDiff("diff", base+"..HEAD", "--", file)
}

// FileDiffUncommitted returns the working tree diff for a file against HEAD.
// If file is empty, returns the diff for all files.
func (g *Git) FileDiffUncommitted(file string) (string, error) {
	// Try tracked diff first (staged + unstaged vs HEAD)
	var diff string
	var err error
	if file == "" {
		diff, err = g.runDiff("diff", "HEAD")
	} else {
		diff, err = g.runDiff("diff", "HEAD", "--", file)
	}
	if err == nil && diff != "" {
		return diff, nil
	}
	// For untracked files, diff against /dev/null. Shares noIndexDiff with the
	// pseudo-entry bodies, which also gets this call the `--` separator it
	// previously lacked (a file named `-x` was read as a flag).
	if out, _ := g.noIndexDiff(file); out != "" {
		return out, nil
	}
	return "", fmt.Errorf("no diff available for %s", file)
}

// Commits returns the list of commits between base and HEAD, newest first.
// Returns an empty slice when base..HEAD is empty (e.g. on main where base
// resolves to HEAD itself); per Reading B in PROMPT.md the natural scope
// there is empty, and the repo history is rendered below the commits-mode
// cutline via BaseCommits instead of pretending to be "in scope".
func (g *Git) Commits(base string, skip, limit int) ([]Commit, error) {
	out, err := g.run("log", "--skip", fmt.Sprintf("%d", skip), "-n", fmt.Sprintf("%d", limit), "--format=%H%x09%an%x09%aI%x09%s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	return parseCommitLog(out), nil
}

// CommitCountRange returns the number of commits in base..HEAD. Returns 0
// when the range is empty (e.g. base == HEAD on main).
func (g *Git) CommitCountRange(base string) (int, error) {
	out, err := g.run("rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0, err
	}
	var count int
	fmt.Sscanf(out, "%d", &count)
	return count, nil
}

// Parent returns the first-parent SHA of the given commit, or an error when
// at the root commit. Used by the scope handle to walk one commit further
// back. Resolves through `git rev-parse <sha>^`.
func (g *Git) Parent(sha string) (string, error) {
	out, err := g.run("rev-parse", sha+"^")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// FirstChildToward returns the first-parent child of base on the path toward
// head — i.e., the oldest commit in base..head when walking by --first-parent.
// Used by the scope handle to walk one commit forward (toward HEAD) without
// blindly trusting m.commits, which on main lists all repo commits and would
// jump to the root commit. Returns an error when base..head is empty.
func (g *Git) FirstChildToward(base, head string) (string, error) {
	out, err := g.run("rev-list", "--first-parent", "--reverse", base+".."+head)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("no commits between %s and %s", base, head)
	}
	lines := strings.SplitN(out, "\n", 2)
	return lines[0], nil
}

// BaseCommits returns commits from the base branch that are already in the
// history (before the feature branch diverged). Limited to a reasonable count.
func (g *Git) BaseCommits(base string, limit int) ([]Commit, error) {
	out, err := g.run("log", "-n", fmt.Sprintf("%d", limit), "--format=%H%x09%an%x09%aI%x09%s", base)
	if err != nil {
		return nil, err
	}
	return parseCommitLog(out), nil
}

func parseCommitLog(out string) []Commit {
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		// Tab-separated: %H \t %an \t %aI \t %s
		parts := strings.SplitN(line, "\t", 4)
		c := Commit{SHA: parts[0]}
		if len(parts) > 1 {
			c.Author = parts[1]
		}
		if len(parts) > 2 {
			if t, err := time.Parse(time.RFC3339, parts[2]); err == nil {
				c.AuthorDate = t
			}
		}
		if len(parts) > 3 {
			c.Subject = parts[3]
		}
		commits = append(commits, c)
	}
	return commits
}

// LastCommitForFile returns the most recent commit that touched the given
// file. Returns an empty Commit and an error if the file has no history (e.g.
// it's untracked or doesn't exist in HEAD).
func (g *Git) LastCommitForFile(file string) (Commit, error) {
	out, err := g.run("log", "-1", "--format=%H%x09%an%x09%aI%x09%s", "--", file)
	if err != nil {
		return Commit{}, err
	}
	commits := parseCommitLog(out)
	if len(commits) == 0 {
		return Commit{}, fmt.Errorf("no commits for %s", file)
	}
	return commits[0], nil
}

// CommitPatch returns the full patch for a single commit.
func (g *Git) CommitPatch(sha string) (string, error) {
	return g.runDiff("show", sha)
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

// IgnoredEntry is a single top-level gitignored entry. Use IgnoredEntries
// to fetch the full set; use IgnoredFilesInDir to recursively enumerate the
// contents of a specific ignored directory on demand.
type IgnoredEntry struct {
	Path  string // relative path, no trailing slash
	IsDir bool   // true if this is a directory collapsed by --directory
}

// IgnoredEntries returns top-level gitignored entries via
// `git ls-files --others --ignored --exclude-standard --directory`. Ignored
// directories collapse to a single entry; bare ignored files appear
// individually. This is dramatically cheaper than enumerating every file
// under each ignored directory (e.g. node_modules/ on a JS monorepo).
func (g *Git) IgnoredEntries() ([]IgnoredEntry, error) {
	recs, err := g.runZ("ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	var entries []IgnoredEntry
	for _, rec := range recs {
		isDir := strings.HasSuffix(rec, "/")
		path := strings.TrimSuffix(rec, "/")
		entries = append(entries, IgnoredEntry{Path: path, IsDir: isDir})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// IgnoredFilesInDir returns the recursive contents of a specific ignored
// directory using `git ls-files --others --ignored --exclude-standard <dir>/`.
// Used to lazily expand a top-level ignored directory entry when the user
// drills in.
func (g *Git) IgnoredFilesInDir(dir string) ([]string, error) {
	files, err := g.runZ("ls-files", "-z", "--others", "--ignored", "--exclude-standard", dir+"/")
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// AllFiles returns all tracked + untracked files in the repo, excluding
// gitignored content. Use IgnoredEntries to fetch ignored top-level
// entries separately (much cheaper than enumerating every ignored file).
// Results are sorted alphabetically.
func (g *Git) AllFiles() ([]string, error) {
	fileSet := make(map[string]bool)

	// Tracked files
	tracked, err := g.runZ("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	for _, f := range tracked {
		fileSet[f] = true
	}

	// Untracked files (excluding ignored)
	if untracked, err := g.runZ("ls-files", "-z", "--others", "--exclude-standard"); err == nil {
		for _, f := range untracked {
			fileSet[f] = true
		}
	}

	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// prPresence is what a failed `gh pr view` turns out to have meant. The
// three states are distinct on purpose: collapsing "the query failed" into
// "there is no PR" makes an expired token, a 502 or a rate limit look exactly
// like a branch nobody has opened a PR for, and the UI then shows an empty PR
// pane with no error at all.
type prPresence int

const (
	// prUnknown: we could not establish that the branch has no PR, so the
	// view failure stands as a failure.
	prUnknown prPresence = iota
	// prAbsent: there is genuinely no PR to fetch (or nothing to ask).
	prAbsent
	// prPresent: a PR exists, so the view failure was about fetching it.
	prPresent
)

// prPresenceAfterViewFailure decides why `gh pr view` failed, without reading
// gh's error prose. gh's messages are English UI text with no stability
// contract; matching them meant a reworded or localized message silently
// changed which of the three states above prwatch reported.
//
// The signals used instead, in order of decisiveness:
//   - a killed subprocess (context.DeadlineExceeded) is a query failure by
//     definition, whatever gh managed to print;
//   - gh not being on PATH (command.ErrNotFound) is not a GitHub failure at all
//     — there is nothing to report and nothing to fix by retrying;
//   - a repo with no GitHub remote has no PR by construction, and gh has
//     nothing to say about it either;
//   - a detached or unborn HEAD gives gh no branch to match a PR against;
//   - otherwise `gh pr list --json number` answers structurally: an empty
//     JSON array means no PR, a non-empty one means the view failure was
//     real, and a probe that itself fails leaves the question open (prUnknown
//     — reported, not swallowed).
//
// The probe costs one extra subprocess, and only on the failure path.
func (g *Git) prPresenceAfterViewFailure(viewErr error) prPresence {
	if errors.Is(viewErr, context.DeadlineExceeded) {
		return prUnknown
	}
	if errors.Is(viewErr, command.ErrNotFound) {
		return prAbsent
	}
	if !g.hasGitHubRemote() {
		return prAbsent
	}
	branch, err := g.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return prAbsent
	}

	out, err := g.runExternal("gh", "pr", "list", "--state", "all",
		"--head", branch, "--limit", "1", "--json", "number")
	if err != nil {
		return prUnknown
	}
	var prs []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		// gh promised JSON and delivered something else. That is a broken
		// query, not evidence of absence.
		return prUnknown
	}
	if len(prs) == 0 {
		return prAbsent
	}
	return prPresent
}

// hasGitHubRemote reports whether any remote URL names a host gh can speak
// to: github.com, or whatever GH_HOST points at (gh's own switch for a
// GitHub Enterprise instance). A repo with no such remote — no remotes at
// all, or a GitLab-only checkout — has no pull request to find, and saying so
// on the status bar every poll would be noise rather than news.
//
// This matches remote URLs against a host list, which is configuration, not
// error prose: nothing here depends on how gh words a failure.
func (g *Git) hasGitHubRemote() bool {
	out, err := g.run("config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil || out == "" {
		return false
	}
	hosts := []string{"github.com"}
	if h := strings.ToLower(strings.TrimSpace(os.Getenv("GH_HOST"))); h != "" {
		hosts = append(hosts, h)
	}
	for _, line := range strings.Split(out, "\n") {
		// `remote.<name>.url <url>` — the URL is the last field.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		url := strings.ToLower(gitRemoteToHTTPS(fields[len(fields)-1]))
		for _, h := range hosts {
			if strings.HasPrefix(url, "https://"+h+"/") || strings.HasPrefix(url, "http://"+h+"/") {
				return true
			}
		}
	}
	return false
}

// PRAll fetches all PR data in a single gh pr view call.
// Returns zero-value PRAllResult if no PR exists.
// Returns an error if the gh command fails for reasons other than "no PR" (e.g. rate limiting, auth issues).
func (g *Git) PRAll() (PRAllResult, error) {
	out, err := g.runExternal("gh", "pr", "view", "--json",
		"number,title,url,state,baseRefName,isDraft,reviewDecision,body,labels,assignees,milestone,mergedBy,reviews,reviewRequests,comments,createdAt,updatedAt,mergedAt,closedAt")
	if err != nil {
		if g.prPresenceAfterViewFailure(err) == prAbsent {
			return PRAllResult{}, nil
		}
		// A PR exists (or we could not establish that one doesn't): the
		// failure is real — rate limit, auth, network — and belongs to the
		// caller, which classifies it (internal/ui.classifyGitHubError).
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

	// Try to fetch reviews with inline comments via GraphQL. Falls back to
	// the basic review data from gh pr view only when GraphQL produced
	// nothing at all: a partial page set beats the fallback, which carries
	// no inline comments and no total, and the failure travels out on
	// ReviewsErr rather than being swallowed.
	reviewsWithComments, total, reviewsErr := g.fetchReviewsGraphQL(result.Info.Number)
	if len(reviewsWithComments) > 0 {
		result.Reviews = reviewsWithComments
		result.ReviewsTotal = total
		result.ReviewsErr = reviewsErr
	} else {
		for _, r := range raw.Reviews {
			result.Reviews = append(result.Reviews, PRReview{
				Author:      r.Author.Login,
				State:       r.State,
				Body:        r.Body,
				SubmittedAt: r.SubmittedAt,
			})
		}
		// `gh pr view` reports no total, so the fallback claims exactly what
		// it has: no truncation is *known*, and inventing one would put a
		// permanent "showing N of M" on every PR whose GraphQL fetch failed.
		result.ReviewsTotal = len(result.Reviews)
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
	nwoOut, err := g.runExternal("gh", "repo", "view", "--json", "owner,name", "--jq", ".owner.login + \"/\" + .name")
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

	out, err := g.runExternal("gh", "api", "graphql", "-f", "query="+query)
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

// Bounds on the reviews fetch. Every page is one `gh api graphql`
// subprocess with its own 45s deadline, and this fetch runs on the PR poll,
// so "page until GitHub runs out" is not an option: a PR with thousands of
// reviews would turn each refresh into an unbounded run of subprocesses.
//
// The outer collection pages properly, capped at reviewsMaxPages — worst
// case 5 subprocesses and 250 reviews per refresh. The inner comments
// collection hangs off a paginated parent, so paging it would mean an extra
// query per review (N+1); it is capped at one page instead. Both caps report
// GitHub's own totalCount alongside the nodes, so a truncated fetch is
// visible as "showing N of M" rather than silently short.
const (
	reviewsPageSize     = 50
	reviewsMaxPages     = 5
	reviewCommentsFirst = 100
)

// reviewsQuery is the reviews page document. Every input is a declared
// GraphQL variable rather than interpolated text: a cursor is opaque
// server-supplied data, and Go's %q emits Go string escapes, which are not
// GraphQL's grammar — `\x1b`, for one, is a Go escape GraphQL rejects
// outright. The page sizes stay literal because they are untainted local
// constants, not values from the network.
//
// $cursor is nullable and simply omitted from the variables on the first
// page: an absent GraphQL variable is null, which is what `after` wants for
// "start at the beginning".
var reviewsQuery = fmt.Sprintf(`query($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviews(first: %d, after: $cursor) {
        totalCount
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          author { login }
          state
          body
          submittedAt
          url
          comments(first: %d) {
            totalCount
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
}`, reviewsPageSize, reviewCommentsFirst)

// fetchReviewsGraphQL fetches reviews with their inline comments via the
// GitHub GraphQL API, following the reviews cursor up to reviewsMaxPages.
// Returns the reviews it gathered plus GitHub's reported total, which exceeds
// len(reviews) exactly when the cap truncated the fetch.
//
// A failure partway through pagination returns the pages already gathered
// alongside the error, so the caller can prefer honest partial data ("100 of
// 500") over the comment-less `gh pr view` fallback while still reporting
// what went wrong. Callers must therefore check the reviews slice, not just
// the error.
//
// Pages are fetched sequentially on the caller's goroutine — PRAll's — so
// this adds no concurrency of its own.
func (g *Git) fetchReviewsGraphQL(prNumber int) ([]PRReview, int, error) {
	var reviews []PRReview
	total := 0

	// Resolve owner/repo via gh so we don't have to parse git remotes ourselves.
	nwoOut, err := g.runExternal("gh", "repo", "view", "--json", "owner,name", "--jq", ".owner.login + \"/\" + .name")
	if err != nil {
		return nil, 0, err
	}
	parts := strings.SplitN(strings.TrimSpace(nwoOut), "/", 2)
	if len(parts) != 2 {
		return nil, 0, fmt.Errorf("unexpected repo format: %q", nwoOut)
	}
	owner, repo := parts[0], parts[1]

	// honest returns the gathered pages with a total that never undercounts
	// them, so every exit — clean, capped, or failed partway — reports the
	// same relationship between total and len(reviews).
	honest := func(err error) ([]PRReview, int, error) {
		if total < len(reviews) {
			total = len(reviews)
		}
		return reviews, total, err
	}

	cursor := ""
	for page := 0; page < reviewsMaxPages; page++ {
		// -f (not -F) for the string variables: -F infers types, so an owner
		// or a cursor that looks like a number would be sent as one and
		// rejected against String!. number is the one genuine Int.
		args := []string{"api", "graphql",
			"-f", "query=" + reviewsQuery,
			"-f", "owner=" + owner,
			"-f", "repo=" + repo,
			"-F", fmt.Sprintf("number=%d", prNumber),
		}
		if cursor != "" {
			args = append(args, "-f", "cursor="+cursor)
		}
		out, err := g.runExternal("gh", args...)
		if err != nil {
			return honest(err)
		}

		var resp struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						Reviews struct {
							TotalCount int `json:"totalCount"`
							PageInfo   struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								Author struct {
									Login string `json:"login"`
								} `json:"author"`
								State       string    `json:"state"`
								Body        string    `json:"body"`
								SubmittedAt time.Time `json:"submittedAt"`
								URL         string    `json:"url"`
								Comments    struct {
									TotalCount int `json:"totalCount"`
									Nodes      []struct {
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
			return honest(err)
		}

		gqlReviews := resp.Data.Repository.PullRequest.Reviews
		total = gqlReviews.TotalCount
		for _, r := range gqlReviews.Nodes {
			review := PRReview{
				Author:        r.Author.Login,
				State:         r.State,
				Body:          r.Body,
				SubmittedAt:   r.SubmittedAt,
				URL:           r.URL,
				CommentsTotal: r.Comments.TotalCount,
			}
			for _, c := range r.Comments.Nodes {
				review.Comments = append(review.Comments, PRReviewComment{
					Path: c.Path,
					Line: c.Line,
					Body: c.Body,
				})
			}
			// A totalCount GitHub didn't report (or reported low) must never
			// leave the review claiming fewer comments than it carries.
			if review.CommentsTotal < len(review.Comments) {
				review.CommentsTotal = len(review.Comments)
			}
			reviews = append(reviews, review)
		}

		if !gqlReviews.PageInfo.HasNextPage || gqlReviews.PageInfo.EndCursor == "" {
			break
		}
		cursor = gqlReviews.PageInfo.EndCursor
	}
	return honest(nil)
}

// PRChecksAll fetches CI checks in a single gh pr checks call, returning
// both the individual checks and an aggregated status summary.
func (g *Git) PRChecksAll() (PRChecksResult, error) {
	// `gh pr checks` exits nonzero when checks are failing or pending while
	// still writing the requested JSON, so the output is authoritative and the
	// exit status is only consulted when nothing parseable came back. A real
	// failure (no PR, auth, network) is reported: the caller must be able to
	// keep the checks it already has rather than blanking the CI panel.
	out, runErr := g.runExternal("gh", "pr", "checks", "--json", "name,state,bucket,link,completedAt,startedAt")

	var checks []CICheck
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		if runErr != nil {
			return PRChecksResult{}, runErr
		}
		return PRChecksResult{}, fmt.Errorf("parsing gh pr checks output: %w", err)
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
	out, err := g.runExternal("rwx", "runs", runID, "--output", "text")
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

	_, err = g.runExternal("rwx", "logs", taskID, "--output-dir", tmpDir)
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
	listOut, err := g.runExternal("rwx", "artifacts", "list", taskID, "--output", "json")
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
		_, err := g.runExternal("rwx", "artifacts", "download", taskID, artifact.Key,
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
