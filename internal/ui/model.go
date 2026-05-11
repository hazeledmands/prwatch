package ui

import (
	"fmt"
	"github.com/hazeledmands/prwatch/internal/command"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

const (
	prRefreshActive  = 30 * time.Second // refresh interval when user is active
	prRefreshIdle    = 10 * time.Minute // refresh interval when idle
	prRefreshMax     = 15 * time.Minute // max backoff on rate limit
	prIdleThreshold  = 10 * time.Minute // no UI events for this long = idle
	prStaleThreshold = 24 * time.Hour   // no server changes for this long = stale
	gitRefreshActive = 5 * time.Second  // local git poll interval when active
	gitRefreshIdle   = 1 * time.Minute  // local git poll interval when idle
	gitActiveWindow  = 2 * time.Minute  // fs event within this window keeps poll fast
)

type searchMatch struct {
	pane string // "sidebar" or "main"
	line int    // line index in the respective pane
}

type Mode int

const (
	FilesMode Mode = iota
	CommitsMode
	PRMode
	HelpMode // not a real mode — used for clickable label in mode bar
)

type Focus int

const (
	SidebarFocus Focus = iota
	MainFocus
)

const commitPageSize = 100

// GitDataSource provides the git operations needed by the UI model.
// Implemented by *git.Git; mockable for testing.
type GitDataSource interface {
	RepoInfo() (gitpkg.RepoInfoResult, error)
	PRAll() (gitpkg.PRAllResult, error)
	PRChecksAll() (gitpkg.PRChecksResult, error)
	DetectBaseLocal() (string, error)
	DetectBaseFromPR(baseRefName string) (string, error)
	ChangedFiles(base string) (gitpkg.ChangedFilesResult, error)
	Commits(base string, skip, limit int) ([]gitpkg.Commit, error)
	AllCommits(skip, limit int) ([]gitpkg.Commit, error)
	CommitCount() (int, error)
	CommitCountRange(base string) (int, error)
	FileDiffCommitted(base, file string) (string, error)
	FileDiffUncommitted(file string) (string, error)
	FileContent(file string) (string, error)
	LastCommitForFile(file string) (gitpkg.Commit, error)
	CommitPatch(sha string) (string, error)
	AllFiles() ([]string, error)
	IgnoredEntries() ([]gitpkg.IgnoredEntry, error)
	IgnoredFilesInDir(dir string) ([]string, error)
	BaseCommits(base string, limit int) ([]gitpkg.Commit, error)
	BehindCount(baseRef string) int
	Parent(sha string) (string, error)
	FirstChildToward(base, head string) (string, error)
	RWXResults(runID string) (*gitpkg.RWXResult, error)
	RWXTaskLog(taskID string) (string, error)
	RWXTestResults(taskID string) ([]gitpkg.RWXFailedTest, error)
}

type Model struct {
	debugLog   *log.Logger
	git        GitDataSource
	cmdFactory command.Factory
	mode       Mode
	focus      Focus
	width      int
	height     int
	base       string
	// naturalBase is the default outer endpoint for the commit-range scope:
	// what detectBase() resolves to at load time. m.base equals naturalBase at
	// the default scope position; scope-extend / scope-contract walk m.base
	// away from naturalBase, and scope-reset snaps it back.
	naturalBase         string
	repoInfo            gitpkg.RepoInfoResult
	prInfo              gitpkg.PRInfoResult
	ciStatus            gitpkg.CIStatusResult
	prReviews           []gitpkg.PRReview
	prReviewRequests    []gitpkg.PRReviewRequest
	prError             string // error message for PR/GitHub API issues
	prCommentCount      int
	committedFiles      []string
	uncommittedFiles    []string        // unstaged/untracked (new changes)
	stagedFiles         []string        // staged but uncommitted
	deletedFiles        []string        // files deleted in base..HEAD
	addedFiles          []string        // files that are entirely new additions
	allFiles            []string        // all files in the repo (for files mode)
	ignoredFiles        map[string]bool // gitignored files (for dimming in all-files view)
	ignoredDirs         map[string]bool // ignored entries that are directories — render as expandable
	loadedIgnoredDirs   map[string]bool // ignored dirs whose contents have been lazy-loaded
	commits             []gitpkg.Commit
	commitCount         int                   // true total commit count (from rev-list --count)
	commitsLoaded       int                   // how many commits have been loaded so far
	behindCount         int                   // how many commits behind base
	baseCommits         []gitpkg.Commit       // commits from the base branch (for commit mode category 4)
	prComments          []gitpkg.PRComment    // PR comments for PR-view mode
	prDeployments       []gitpkg.PRDeployment // PR deployments for PR-view mode
	ciChecks            []gitpkg.CICheck      // CI checks for PR-view mode
	rwxFetcher          *rwxFetcher           // RWX log fetch/cache state
	mainScrollLines     map[mainItemKey]int   // last source line at top of main pane, per (mode, item)
	lastMainItem        mainItemKey           // (mode, item) currently displayed in main pane
	sidebar             *sidebar
	mainPane            *mainPane
	sidebarPct          int // sidebar width as percentage of total width (10-50)
	dir                 string
	confirming          bool
	showHelp            bool
	helpScrollOffset    int             // scroll offset within help overlay
	helpSearching       bool            // search active within help
	helpSearchConfirmed bool            // help search confirmed, n/p navigation
	helpSearchQuery     string          // search query within help
	helpSearchMatches   []int           // line indices of matches in help
	helpSearchIdx       int             // current match index
	showIgnored         bool            // whether to show gitignored files in all-files section
	collapsedDirs       map[string]bool // tracks collapsed directory paths
	sidebarHidden       bool            // [f] toggles sidebar visibility
	wordWrap            bool            // [w] toggles word wrapping in main pane
	lineNumbers         bool            // [n] toggles line numbers in files mode
	searching           bool            // search input is active
	searchConfirmed     bool            // enter pressed, n/p navigation active
	searchQuery         string
	searchMatches       []searchMatch    // matches across both panes
	searchMatchIdx      int              // current match index
	hoverX, hoverY      int              // last mouse position for hover highlighting
	activity            *activityTracker // adaptive refresh-interval bookkeeping
	dragStartX          int              // drag start position (-1 = not dragging)
	dragStartY          int
	dragEndX            int
	dragEndY            int
	dragging            bool
	// dragScrollDir is +1 to auto-scroll the main pane down (drag past the
	// bottom edge), -1 to auto-scroll up (drag past the top edge), 0 when
	// the drag end is inside the viewport. While non-zero, a recurring
	// dragScrollTickMsg keeps shifting the viewport so the user can extend
	// a selection beyond what fits on screen.
	dragScrollDir      int
	notification       string       // transient notification text (bottom-left)
	notificationExpiry time.Time    // when the notification should disappear
	loading            bool         // true until first local data load completes
	prLoadedOnce       bool         // true after first successful PR data fetch
	modeLabels         []modeLabel  // clickable mode label positions from last render
	line2Labels        []line2Label // clickable positions on git status line
	line3Labels        []line3Label // clickable positions on PR status line
	// modeStates preserves per-mode view state (sidebar selection, scroll
	// positions) so switching back to a mode restores the last view.
	// Keys are Mode values that represent real modes (FilesMode, CommitsMode,
	// PRMode); HelpMode is not stored here.
	modeStates map[Mode]modeViewState
	err        error
}

// modeViewState records the view state for a single mode, so that switching
// away and back restores what the user was looking at. Main-pane scroll
// position is tracked separately, per (mode, item), via Model.mainScrollLines.
type modeViewState struct {
	sidebarSelected string // selected sidebar item label (by content, not index)
	sidebarOffset   int    // sidebar vertical scroll offset
	focus           Focus
}

// mainItemKey identifies a single piece of content the main pane can show:
// a (mode, sidebar item) pair. Used as the lookup key for per-item scroll
// memory in Model.mainScrollLines. The item string is m.sidebar.SelectedItem()
// for that mode (file path, "abc1234 subject", "Description", "comment #N @x",
// "review #N @x", "CI · check-name", or one of the pseudo-entries like
// "new changes" / "staged changes" / "load more").
type mainItemKey struct {
	mode Mode
	item string
}

// Messages
type gitDataMsg struct {
	repoInfo         gitpkg.RepoInfoResult
	prInfo           gitpkg.PRInfoResult
	ciStatus         gitpkg.CIStatusResult
	prReviews        []gitpkg.PRReview
	prCommentCount   int
	base             string // base used for queries — equals naturalBase except when the user has scrubbed the scope handle
	naturalBase      string // base detected fresh this load — what scope-reset would snap to
	committedFiles   []string
	uncommittedFiles []string
	stagedFiles      []string
	deletedFiles     []string
	addedFiles       []string
	allFiles         []string
	ignoredFiles     map[string]bool
	ignoredDirs      map[string]bool // subset of ignoredFiles whose entries are directories
	commits          []gitpkg.Commit
	commitCount      int
	baseCommits      []gitpkg.Commit
	prComments       []gitpkg.PRComment
	prDeployments    []gitpkg.PRDeployment
	ciChecks         []gitpkg.CICheck
	reviewRequests   []gitpkg.PRReviewRequest
	behindCount      int
	prFetchFailed    bool // true if PR fetch errored (e.g. rate limit) — preserve old PR data
	localOnly        bool // true if this was a local-only refresh (no API calls attempted)
	err              error
}

type RefreshMsg struct{}

type moreCommitsMsg struct {
	commits []gitpkg.Commit
}

type prRefreshMsg struct {
	prInfo         gitpkg.PRInfoResult
	ciStatus       gitpkg.CIStatusResult
	reviews        []gitpkg.PRReview
	reviewRequests []gitpkg.PRReviewRequest
	commentCount   int
	ciChecks       []gitpkg.CICheck
	prComments     []gitpkg.PRComment
	prDeployments  []gitpkg.PRDeployment
	rateLimited    bool
}

type prTickMsg struct{}
type gitTickMsg struct{}

// dragScrollTickMsg drives auto-scroll while a drag selection is held past
// the top or bottom edge of the main pane viewport. Only delivered while
// m.dragging is true and m.dragScrollDir != 0.
type dragScrollTickMsg struct{}

const dragScrollInterval = 60 * time.Millisecond

type notificationExpiredMsg struct{}

// maybeFetchRWXLog returns a tea.Cmd to fetch RWX logs if there's a pending
// check staged by the previous render. Forwards to rwxFetcher.
func (m *Model) maybeFetchRWXLog() tea.Cmd {
	return m.rwxFetcher.Cmd(m.git)
}

// defaultCmdFactory is the command factory used by NewModel. Tests in the
// same package can override this to prevent accidental exec calls.
var defaultCmdFactory command.Factory = command.DefaultFactory

func NewModel(dir string, g GitDataSource) *Model {
	var debugLog *log.Logger
	if path := os.Getenv("PRWATCH_DEBUG_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			debugLog = log.New(f, "", log.Ltime|log.Lmicroseconds)
			debugLog.Println("=== prwatch debug log started ===")
		}
	}

	return &Model{
		debugLog:      debugLog,
		git:           g,
		cmdFactory:    defaultCmdFactory,
		dir:           dir,
		mode:          FilesMode,
		focus:         SidebarFocus,
		sidebar:       newSidebar(),
		mainPane:      newMainPane(),
		sidebarPct:    30, // default 30% of width
		showIgnored:   true,
		collapsedDirs: make(map[string]bool),
		rwxFetcher:    newRWXFetcher(),
		modeStates:    make(map[Mode]modeViewState),
		wordWrap:      true,
		lineNumbers:   true,
		activity:      newActivityTracker(time.Now()),
		loading:       g != nil,
		dragStartX:    -1,
		dragStartY:    -1,
	}
}

func (m *Model) Init() tea.Cmd {
	if m.git == nil {
		return m.loadNonGitFiles
	}
	return tea.Batch(m.loadLocalGitData, m.loadPRStatus, schedulePRTick(m.activity.PRInterval()), scheduleGitTick(m.activity.GitInterval()))
}

func schedulePRTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return prTickMsg{}
	})
}

func scheduleGitTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return gitTickMsg{}
	})
}

func (m *Model) loadNonGitFiles() tea.Msg {
	var files []string
	err := filepath.WalkDir(m.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == m.dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(m.dir, path)
			if err != nil {
				return nil
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return gitDataMsg{err: err}
	}
	return gitDataMsg{
		uncommittedFiles: files,
	}
}

func (m *Model) loadPRStatus() tea.Msg {
	prAll, err := m.git.PRAll()
	if err != nil {
		// Any PR fetch error (rate limit, network, auth) — signal to preserve old data
		return prRefreshMsg{rateLimited: true}
	}
	var checksResult gitpkg.PRChecksResult
	if prAll.Info.Number > 0 {
		checksResult, _ = m.git.PRChecksAll()
	}
	return prRefreshMsg{
		prInfo:         prAll.Info,
		ciStatus:       checksResult.Status,
		reviews:        prAll.Reviews,
		reviewRequests: prAll.ReviewRequests,
		commentCount:   prAll.CommentCount,
		ciChecks:       checksResult.Checks,
		prComments:     prAll.Comments,
		prDeployments:  prAll.Deployments,
	}
}

// isRateLimited checks if an error from the gh CLI indicates rate limiting.
func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "403") || strings.Contains(msg, "secondary rate")
}

func (m *Model) fileDiffPrefix(file string) string {
	return fileDiffPrefix(file, m.isUncommittedFile(file), m.isCommittedFile(file), m.statMtime, m.lastCommitForFile)
}

func (m *Model) fileContextRight(file string, binary bool) string {
	return fileContextRight(file, binary, m.statMtime, m.lastCommitForFile)
}

// statMtime returns the working-tree mtime of file (relative to m.dir).
// Adapts os.Stat into the statMtimeFn shape.
func (m *Model) statMtime(file string) (time.Time, bool) {
	if m.dir == "" {
		return time.Time{}, false
	}
	info, err := os.Stat(filepath.Join(m.dir, file))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// lastCommitForFile delegates to the git source if present. Returns the zero
// Commit with no error when git is unavailable; fileDiffPrefix and
// fileContextRight both already treat empty SHA as "no data".
func (m *Model) lastCommitForFile(file string) (gitpkg.Commit, error) {
	if m.git == nil {
		return gitpkg.Commit{}, nil
	}
	return m.git.LastCommitForFile(file)
}

func (m *Model) sortPRData() {
	sortPRData(m.prComments, m.prReviews, m.ciChecks)
}

type allFilesMsg struct {
	files []string
}

// ignoredDirLoadedMsg is the result of a lazy-load fired when the user
// expands a previously unloaded ignored directory.
type ignoredDirLoadedMsg struct {
	dir   string   // the dir that was expanded
	files []string // recursive ignored contents under dir
}

// expandIgnoredDir returns a Cmd that fetches the contents of an ignored
// directory in the background and posts an ignoredDirLoadedMsg.
func (m *Model) expandIgnoredDir(dir string) tea.Cmd {
	return func() tea.Msg {
		files, _ := m.git.IgnoredFilesInDir(dir)
		return ignoredDirLoadedMsg{dir: dir, files: files}
	}
}

func (m *Model) reloadAllFiles() tea.Msg {
	files, _ := m.git.AllFiles()
	return allFilesMsg{files: files}
}

// loadIgnoredSet fetches gitignored top-level entries via IgnoredEntries
// and returns them as two sets: all entry paths, and the subset that are
// directories (eligible for lazy expansion). Returns nil maps on error.
func loadIgnoredSet(g GitDataSource) (files map[string]bool, dirs map[string]bool) {
	entries, err := g.IgnoredEntries()
	if err != nil {
		return nil, nil
	}
	files = make(map[string]bool, len(entries))
	dirs = make(map[string]bool)
	for _, e := range entries {
		files[e.Path] = true
		if e.IsDir {
			dirs[e.Path] = true
		}
	}
	return files, dirs
}

func (m *Model) loadGitData() tea.Msg {
	info, err := m.git.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Empty repo (no commits yet): skip diff/commit operations that require HEAD
	if info.IsEmpty {
		allFiles, _ := m.git.AllFiles()
		return gitDataMsg{
			repoInfo:         info,
			uncommittedFiles: allFiles,
			allFiles:         allFiles,
		}
	}

	prAll, prErr := m.git.PRAll()
	prFetchFailed := prErr != nil
	prInfo := prAll.Info

	// Fetch CI checks if a PR exists (and fetch succeeded)
	var ciStatus gitpkg.CIStatusResult
	var ciChecks []gitpkg.CICheck
	if prInfo.Number > 0 {
		checksResult, _ := m.git.PRChecksAll()
		ciStatus = checksResult.Status
		ciChecks = checksResult.Checks
	}

	// Prefer the PR-reported base when available; fall back to local detection.
	var base string
	if prInfo.BaseRef != "" {
		if sha, berr := m.git.DetectBaseFromPR(prInfo.BaseRef); berr == nil {
			base = sha
		}
	}
	if base == "" {
		var berr error
		base, berr = m.git.DetectBaseLocal()
		if berr != nil {
			return gitDataMsg{err: berr}
		}
	}

	files, err := m.git.ChangedFiles(base)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Fetch commits and total count, preserving pagination state
	pageSize := max(commitPageSize, m.commitsLoaded)
	var commits []gitpkg.Commit
	var commitCount int
	if info.IsDetachedHead || info.Branch == "main" || info.Branch == "master" {
		commits, err = m.git.AllCommits(0, pageSize)
		if err != nil {
			return gitDataMsg{err: err}
		}
		commitCount, _ = m.git.CommitCount()
	} else {
		commits, err = m.git.Commits(base, 0, pageSize)
		if err != nil {
			return gitDataMsg{err: err}
		}
		commitCount, _ = m.git.CommitCountRange(base)
	}

	// Compute behind count: how many commits on the base branch we don't have
	var behindCount int
	if !info.IsDetachedHead && info.Branch != "main" && info.Branch != "master" {
		// Use PR base ref if available, otherwise infer from upstream
		baseRef := "origin/main"
		if prInfo.BaseRef != "" {
			baseRef = "origin/" + prInfo.BaseRef
		} else if info.Upstream != "" {
			baseRef = info.Upstream
		}
		behindCount = m.git.BehindCount(baseRef)
	}

	// Fetch base branch commits for commit mode category 4
	var baseCommits []gitpkg.Commit
	if !info.IsDetachedHead && info.Branch != "main" && info.Branch != "master" {
		baseCommits, _ = m.git.BaseCommits(base, 50)
	}

	// Fetch tracked + untracked files (no ignored — those come from the
	// dedicated --directory query so giant ignored trees like node_modules/
	// don't blow up the file list).
	allFiles, _ := m.git.AllFiles()
	ignoredSet, ignoredDirSet := loadIgnoredSet(m.git)

	return gitDataMsg{
		repoInfo:         info,
		prInfo:           prInfo,
		ciStatus:         ciStatus,
		prReviews:        prAll.Reviews,
		prCommentCount:   prAll.CommentCount,
		base:             base,
		committedFiles:   files.Committed,
		uncommittedFiles: files.Uncommitted,
		stagedFiles:      files.Staged,
		deletedFiles:     files.Deleted,
		addedFiles:       files.Added,
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
		ignoredDirs:      ignoredDirSet,
		commits:          commits,
		commitCount:      commitCount,
		baseCommits:      baseCommits,
		behindCount:      behindCount,
		prComments:       prAll.Comments,
		prDeployments:    prAll.Deployments,
		ciChecks:         ciChecks,
		reviewRequests:   prAll.ReviewRequests,
		prFetchFailed:    prFetchFailed,
	}
}

// detectBase returns the merge-base SHA to use for diff computations,
// preferring the PR-reported base when available and falling back to
// local detection. Never invokes gh.
func (m *Model) detectBase() (string, error) {
	if m.prInfo.BaseRef != "" {
		if sha, err := m.git.DetectBaseFromPR(m.prInfo.BaseRef); err == nil {
			return sha, nil
		}
	}
	return m.git.DetectBaseLocal()
}

// loadLocalGitData refreshes only local git state (no GitHub API calls).
// Existing PR data in the model is preserved via prFetchFailed.
func (m *Model) loadLocalGitData() tea.Msg {
	info, err := m.git.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Empty repo (no commits yet): skip diff/commit operations that require HEAD
	if info.IsEmpty {
		allFiles, _ := m.git.AllFiles()
		return gitDataMsg{
			repoInfo:         info,
			uncommittedFiles: allFiles,
			allFiles:         allFiles,
			localOnly:        true,
		}
	}

	// Prefer the PR-reported base if PR data has loaded; otherwise use
	// local detection (no gh shell-out). When PR data arrives later, the
	// prRefreshMsg handler re-dispatches loadLocalGitData so the base
	// upgrades to match the PR's baseRefName.
	naturalBase, err := m.detectBase()
	if err != nil {
		return gitDataMsg{err: err}
	}

	// If the user has scrubbed the scope handle, queries use the scrubbed
	// outer endpoint instead of the natural base. The handler preserves
	// m.base when the load returns.
	base := naturalBase
	if m.base != "" && m.base != naturalBase {
		base = m.base
	}

	files, err := m.git.ChangedFiles(base)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Preserve pagination: reload at least as many commits as the user has already seen
	pageSize := max(commitPageSize, m.commitsLoaded)

	// Main / master / detached HEAD have no PR delta; the historical default
	// is to list the whole repo history. When the user scrubs the scope
	// handle on those branches we want HEAD~N and the displayed commits to
	// reflect the scrubbed range — otherwise scope-extend-back would show
	// total-commits-in-repo no matter how far back the handle moved.
	onMainLike := info.IsDetachedHead || info.Branch == "main" || info.Branch == "master"
	scopedOnMainLike := onMainLike && base != naturalBase
	var commits []gitpkg.Commit
	var commitCount int
	if onMainLike && !scopedOnMainLike {
		commits, err = m.git.AllCommits(0, pageSize)
		if err != nil {
			return gitDataMsg{err: err}
		}
		commitCount, _ = m.git.CommitCount()
	} else {
		commits, err = m.git.Commits(base, 0, pageSize)
		if err != nil {
			return gitDataMsg{err: err}
		}
		commitCount, _ = m.git.CommitCountRange(base)
	}

	var behindCount int
	if !info.IsDetachedHead && info.Branch != "main" && info.Branch != "master" {
		// Prefer the PR's base branch when available. Without a PR, fall back
		// to origin/main; BehindCount returns 0 cleanly if that ref doesn't
		// exist (e.g. master-default repos or no remote).
		baseRef := "origin/main"
		if m.prInfo.BaseRef != "" {
			baseRef = "origin/" + m.prInfo.BaseRef
		}
		behindCount = m.git.BehindCount(baseRef)
	}

	var baseCommits []gitpkg.Commit
	if !info.IsDetachedHead && (!onMainLike || scopedOnMainLike) {
		baseCommits, _ = m.git.BaseCommits(base, 50)
	}

	allFiles, _ := m.git.AllFiles()
	ignoredSet, ignoredDirSet := loadIgnoredSet(m.git)

	return gitDataMsg{
		repoInfo:         info,
		base:             base,
		naturalBase:      naturalBase,
		committedFiles:   files.Committed,
		uncommittedFiles: files.Uncommitted,
		stagedFiles:      files.Staged,
		deletedFiles:     files.Deleted,
		addedFiles:       files.Added,
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
		ignoredDirs:      ignoredDirSet,
		commits:          commits,
		commitCount:      commitCount,
		baseCommits:      baseCommits,
		behindCount:      behindCount,
		localOnly:        true, // preserve existing PR data
	}
}

func (m *Model) loadMoreCommits() tea.Msg {
	skip := m.commitsLoaded
	info := m.repoInfo
	onMainLike := info.IsDetachedHead || info.Branch == "main" || info.Branch == "master"
	scopedOnMainLike := onMainLike && m.base != m.naturalBase && m.naturalBase != ""
	var commits []gitpkg.Commit
	var err error
	if onMainLike && !scopedOnMainLike {
		commits, err = m.git.AllCommits(skip, commitPageSize)
	} else {
		commits, err = m.git.Commits(m.base, skip, commitPageSize)
	}
	if err != nil {
		return nil
	}
	return moreCommitsMsg{commits: commits}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	result, cmd := m.update(msg)
	rm := result.(*Model)
	if rwxCmd := rm.maybeFetchRWXLog(); rwxCmd != nil {
		if cmd != nil {
			return result, tea.Batch(cmd, rwxCmd)
		}
		return result, rwxCmd
	}
	return result, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Track user activity for adaptive refresh
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.WindowSizeMsg:
		m.activity.MarkUIEvent(time.Now())
	}

	if m.debugLog != nil {
		switch msg := msg.(type) {
		// UI actions
		case tea.KeyPressMsg:
			m.debugLog.Printf("[key] code=%d text=%q", msg.Code, msg.Text)
		case tea.MouseClickMsg:
			m.debugLog.Printf("[mouse-click] x=%d y=%d button=%d", msg.X, msg.Y, msg.Button)
		case tea.MouseWheelMsg:
			m.debugLog.Printf("[mouse-wheel] x=%d y=%d button=%d", msg.X, msg.Y, msg.Button)
		case tea.MouseMotionMsg:
			m.debugLog.Printf("[mouse-motion] x=%d y=%d", msg.X, msg.Y)
		case tea.MouseReleaseMsg:
			m.debugLog.Printf("[mouse-release] x=%d y=%d", msg.X, msg.Y)
		case tea.WindowSizeMsg:
			m.debugLog.Printf("[resize] width=%d height=%d", msg.Width, msg.Height)
		// Timer fires
		case gitTickMsg:
			m.debugLog.Printf("[timer] gitTick")
		case prTickMsg:
			m.debugLog.Printf("[timer] prTick")
		case notificationExpiredMsg:
			m.debugLog.Printf("[timer] notificationExpired")
		// Filesystem changes
		case RefreshMsg:
			m.debugLog.Printf("[fs] RefreshMsg (file watcher)")
		// Data loads
		case gitDataMsg:
			m.debugLog.Printf("[data] gitDataMsg localOnly=%v committed=%d uncommitted=%d allFiles=%d",
				msg.localOnly, len(msg.committedFiles), len(msg.uncommittedFiles), len(msg.allFiles))
		case allFilesMsg:
			m.debugLog.Printf("[data] allFilesMsg files=%d", len(msg.files))
		case ignoredDirLoadedMsg:
			m.debugLog.Printf("[data] ignoredDirLoadedMsg dir=%q files=%d", msg.dir, len(msg.files))
		case moreCommitsMsg:
			m.debugLog.Printf("[data] moreCommitsMsg commits=%d", len(msg.commits))
		case prRefreshMsg:
			m.debugLog.Printf("[data] prRefreshMsg rateLimited=%v", msg.rateLimited)
		case rwxLogMsg:
			m.debugLog.Printf("[data] rwxLogMsg")
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case gitDataMsg:
		wasLoading := m.loading
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.repoInfo = msg.repoInfo
		// Local-only refresh: preserve all existing PR data and error state
		// PR fetch failed: preserve PR data but flag the error
		// Otherwise: update PR data normally
		if !msg.localOnly {
			m.prLoadedOnce = true
			if msg.prFetchFailed {
				m.prError = "GitHub API error"
			} else {
				m.prError = ""
				m.prInfo = msg.prInfo
				m.ciStatus = msg.ciStatus
				m.prReviews = msg.prReviews
				m.prReviewRequests = msg.reviewRequests
				m.prCommentCount = msg.prCommentCount
				m.prComments = msg.prComments
				m.prDeployments = msg.prDeployments
				m.ciChecks = msg.ciChecks
				m.sortPRData()
			}
		}
		// Stale-load guard. If a periodic tick's loadLocalGitData was already
		// in flight when the user scrubbed the scope handle, the tick's msg
		// carries the pre-scrub base. Applying it would overwrite m.base and
		// the file/commit lists with data for the wrong scope. Discard the
		// scope-dependent fields; the next load (re-dispatched by the scope
		// command) is authoritative.
		if m.base != "" && msg.base != "" && msg.base != m.base {
			if msg.naturalBase != "" {
				m.naturalBase = msg.naturalBase
			}
			m.updateLayout()
			m.updateSidebarItems()
			m.updateMainContent()
			return m, nil
		}

		m.base = msg.base
		if msg.naturalBase != "" {
			m.naturalBase = msg.naturalBase
		} else {
			// Empty-repo / pre-natural-base loads carry no naturalBase.
			m.naturalBase = msg.base
		}
		m.committedFiles = msg.committedFiles
		m.uncommittedFiles = msg.uncommittedFiles
		m.stagedFiles = msg.stagedFiles
		m.deletedFiles = msg.deletedFiles
		m.addedFiles = msg.addedFiles
		m.allFiles = msg.allFiles
		// Snapshot deep paths from the previous ignoredFiles before we
		// overwrite — we need to reattach them after the new top-level set
		// is in place, so refresh ticks don't lose the lazy-loaded contents
		// the user has been navigating inside.
		var preservedDeep []string
		for f := range m.ignoredFiles {
			if strings.Contains(f, "/") {
				preservedDeep = append(preservedDeep, f)
			}
		}
		m.ignoredFiles = msg.ignoredFiles
		m.ignoredDirs = msg.ignoredDirs
		if m.loadedIgnoredDirs == nil {
			m.loadedIgnoredDirs = make(map[string]bool)
		}
		for d := range m.loadedIgnoredDirs {
			if !m.ignoredDirs[d] {
				delete(m.loadedIgnoredDirs, d)
			}
		}
		// Default-collapse ignored dirs that we haven't seen yet, regardless
		// of mode. updateSidebarItems' FilesMode branch does this too, but
		// it doesn't run in CommitsMode/PRMode — without this, a freshly-
		// loaded ignored dir would be in the {unloaded, expanded} state,
		// which is fine for those modes (it isn't rendered) but trips the
		// state invariant and would render as an empty ▼ if the user
		// switched to FilesMode.
		for d := range m.ignoredDirs {
			key := dirCollapseKey(sectionAllFiles, d)
			if _, exists := m.collapsedDirs[key]; !exists {
				m.collapsedDirs[key] = true
			}
		}
		// Reattach deep paths whose top-level dir is still loaded.
		for _, p := range preservedDeep {
			for d := range m.loadedIgnoredDirs {
				if strings.HasPrefix(p, d+"/") {
					m.ignoredFiles[p] = true
					break
				}
			}
		}
		m.commits = msg.commits
		m.commitCount = msg.commitCount
		m.commitsLoaded = len(msg.commits)
		m.baseCommits = msg.baseCommits
		m.behindCount = msg.behindCount
		// On first load, default to PR mode if a PR exists and mode hasn't been changed
		if wasLoading && m.prInfo.Number > 0 && m.mode == FilesMode {
			m.mode = PRMode
		}
		// Recalculate layout — status bar height may have changed
		m.updateLayout()
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case moreCommitsMsg:
		m.commits = append(m.commits, msg.commits...)
		m.commitsLoaded = len(m.commits)
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case allFilesMsg:
		m.allFiles = msg.files
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case ignoredDirLoadedMsg:
		if m.loadedIgnoredDirs == nil {
			m.loadedIgnoredDirs = make(map[string]bool)
		}
		m.loadedIgnoredDirs[msg.dir] = true
		for _, f := range msg.files {
			m.ignoredFiles[f] = true
		}
		// Auto-expand the dir now that its contents are loaded — the user
		// asked for them, so don't make them press right a second time.
		// Ignored dirs only ever appear in the All Files section.
		m.collapsedDirs[dirCollapseKey(sectionAllFiles, msg.dir)] = false
		m.updateSidebarItems()
		// updateSidebarItems can change which item lives at the selected
		// index — single-leaf compaction can swallow a dir entry whose
		// only child is a file, leaving SelectedItem() pointing at the
		// child path instead of the dir we just expanded. Refresh the
		// main pane so it matches whatever's now under the cursor.
		m.updateMainContent()
		return m, nil

	case prRefreshMsg:
		if msg.rateLimited {
			m.activity.BumpRateLimited()
			m.prError = "GitHub API rate limited"
			m.updateLayout()
			return m, nil
		}
		// Track whether the server data actually changed
		if msg.prInfo.Number != m.prInfo.Number ||
			msg.prInfo.Title != m.prInfo.Title ||
			msg.prInfo.State != m.prInfo.State ||
			msg.prInfo.ReviewDecision != m.prInfo.ReviewDecision ||
			msg.ciStatus.State != m.ciStatus.State ||
			msg.commentCount != m.prCommentCount ||
			len(msg.reviews) != len(m.prReviews) {
			m.activity.MarkServerChange(time.Now())
		}
		// Detect base-branch change (either newly-learned from PR data, or the
		// PR's base ref was edited server-side). If so, re-dispatch the local
		// git refresh so diffs/commits/changed-files recompute against the new
		// base.
		baseRefChanged := msg.prInfo.BaseRef != m.prInfo.BaseRef && msg.prInfo.BaseRef != ""
		// Successful fetch — reset interval based on activity and clear error
		m.activity.ResetPRInterval(time.Now())
		m.prError = ""
		m.prInfo = msg.prInfo
		m.ciStatus = msg.ciStatus
		m.prReviews = msg.reviews
		m.prReviewRequests = msg.reviewRequests
		m.prCommentCount = msg.commentCount
		m.ciChecks = msg.ciChecks
		m.prComments = msg.prComments
		m.prDeployments = msg.prDeployments
		m.sortPRData()
		// On first PR data arrival, switch to PR mode if user hasn't changed modes
		if !m.prLoadedOnce && m.prInfo.Number > 0 && m.mode == FilesMode {
			m.mode = PRMode
		}
		m.prLoadedOnce = true
		m.updateLayout()
		m.updateSidebarItems()
		m.updateMainContent()
		if baseRefChanged && m.git != nil {
			return m, m.loadLocalGitData
		}
		return m, nil

	case rwxLogMsg:
		m.rwxFetcher.Apply(msg)
		m.updateMainContent()
		return m, nil

	case prTickMsg:
		// Recompute interval on each tick based on current activity state
		m.activity.ResetPRInterval(time.Now())
		if m.git == nil {
			return m, schedulePRTick(m.activity.PRInterval())
		}
		return m, tea.Batch(m.loadPRStatus, schedulePRTick(m.activity.PRInterval()))

	case notificationExpiredMsg:
		m.notification = ""
		return m, nil

	case gitTickMsg:
		m.activity.ResetGitInterval(time.Now())
		if m.git == nil {
			return m, scheduleGitTick(m.activity.GitInterval())
		}
		return m, tea.Batch(m.loadLocalGitData, scheduleGitTick(m.activity.GitInterval()))

	case RefreshMsg:
		// Any fs-watcher event means the working directory is active;
		// stamp lastGitChange so computeGitInterval keeps us on the fast poll.
		m.activity.MarkFSEvent(time.Now())
		if m.git == nil {
			return m, m.loadNonGitFiles
		}
		// Use local-only refresh (no GitHub API calls). File watcher
		// events fire frequently and should not hit the network.
		// Full PR data is refreshed on the PR tick cycle instead.
		return m, m.loadLocalGitData

	case ipcMsg:
		return m.handleIPC(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseWheelMsg:
		if m.showHelp {
			helpLines := m.helpContentLines()
			visibleHeight := max(1, m.height-m.statusBarLines()-2)
			if msg.Button == tea.MouseWheelUp && m.helpScrollOffset > 0 {
				m.helpScrollOffset--
			} else if msg.Button == tea.MouseWheelDown && m.helpScrollOffset < len(helpLines)-visibleHeight {
				m.helpScrollOffset++
			}
			return m, nil
		}
		return m.handleMouseWheel(msg)

	case tea.MouseMotionMsg:
		m.hoverX = msg.X
		m.hoverY = msg.Y
		var autoScrollCmd tea.Cmd
		if m.dragging {
			m.dragEndX = msg.X
			m.dragEndY = msg.Y
			autoScrollCmd = m.updateDragAutoScroll(msg.Y)
		}
		// Update sidebar hover index
		sidebarW := m.sidebarPixelWidth()
		sbLines := m.statusBarLines()
		if !m.sidebarHidden && msg.X < sidebarW && msg.Y >= sbLines {
			contentY := msg.Y - sbLines
			itemIdx := contentY - 1 + m.sidebar.offset
			m.sidebar.SetHoverIndex(itemIdx)
		} else {
			m.sidebar.SetHoverIndex(-1)
		}
		return m, autoScrollCmd

	case tea.MouseReleaseMsg:
		if m.dragging {
			m.dragging = false
			m.dragScrollDir = 0
			m.dragEndX = msg.X
			m.dragEndY = msg.Y
			return m, m.copySelection()
		}
		return m, nil

	case dragScrollTickMsg:
		if !m.dragging || m.dragScrollDir == 0 {
			return m, nil
		}
		return m, m.advanceDragAutoScroll()
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Handle shift+space as page up (may not be caught by key.Matches)
	if msg.Code == tea.KeySpace && msg.Mod&tea.ModShift != 0 {
		if m.showHelp {
			helpLines := m.helpContentLines()
			visibleHeight := max(1, m.height-m.statusBarLines()-2)
			m.helpScrollOffset = max(0, m.helpScrollOffset-visibleHeight)
			_ = helpLines
			return m, nil
		}
		if m.focus == SidebarFocus {
			for range m.sidebar.visibleLines() {
				m.sidebar.SelectPrev()
			}
			m.updateMainContent()
			return m, nil
		}
		// Forward to viewport for page up
		return m, m.mainPane.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}

	// Search input mode
	if m.searching {
		return m.handleSearchKey(msg)
	}

	// Search confirmed mode (n/p navigation)
	if m.searchConfirmed {
		return m.handleSearchNavKey(msg)
	}

	// Help overlay — supports scrolling and search
	if m.showHelp {
		return m.handleHelpKey(msg)
	}

	// Quit confirmation handling
	if m.confirming {
		if msg.Code == tea.KeyEscape {
			m.confirming = false
			return m, nil
		}
		if key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.QuitImmediate) {
			return m, tea.Quit
		}
		m.confirming = false
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.QuitImmediate):
		return m, tea.Quit

	case key.Matches(msg, keys.QuitConfirm):
		m.confirming = true
		return m, nil

	case key.Matches(msg, keys.Help):
		m.showHelp = true
		return m, nil

	case key.Matches(msg, keys.Search):
		m.searching = true
		m.searchQuery = ""
		return m, nil

	case key.Matches(msg, keys.ToggleMode):
		if m.git == nil {
			return m, nil // non-git: files mode only
		}
		// Cycle: files -> commits -> pr -> files.
		// If there's no active PR, skip the pr step.
		next := m.mode
		switch m.mode {
		case FilesMode:
			next = CommitsMode
		case CommitsMode:
			if m.prInfo.Number > 0 {
				next = PRMode
			} else {
				next = FilesMode
			}
		case PRMode:
			next = FilesMode
		}
		m.setMode(next)
		return m, nil

	case key.Matches(msg, keys.FilesMode):
		m.setMode(FilesMode)
		return m, nil

	case key.Matches(msg, keys.CommitsMode):
		if m.git == nil {
			return m, nil
		}
		m.setMode(CommitsMode)
		return m, nil

	case key.Matches(msg, keys.PRMode):
		if m.git == nil || m.prInfo.Number == 0 {
			return m, nil
		}
		m.setMode(PRMode)
		return m, nil

	case key.Matches(msg, keys.FocusLeft):
		if m.focus == SidebarFocus {
			return m.handleSidebarLeft()
		}
		// Main pane: scroll left, or switch to sidebar if already at left edge
		if !m.wordWrap && m.mainPane.xOffset > 0 {
			m.mainPane.ScrollLeft(4)
		} else {
			m.focus = SidebarFocus
		}
		return m, nil

	case key.Matches(msg, keys.FocusRight):
		if m.focus == SidebarFocus {
			return m.handleSidebarRight()
		}
		if !m.wordWrap {
			m.mainPane.ScrollRight(4)
		}
		return m, nil

	case key.Matches(msg, keys.FocusSidebar):
		m.focus = SidebarFocus
		return m, nil

	case key.Matches(msg, keys.FocusMain):
		m.focus = MainFocus
		return m, nil

	case key.Matches(msg, keys.FocusToggle):
		if m.focus == SidebarFocus {
			m.focus = MainFocus
		} else {
			m.focus = SidebarFocus
		}
		return m, nil

	case key.Matches(msg, keys.GoTop):
		if m.focus == SidebarFocus {
			m.sidebar.SelectFirst()
			m.updateMainContent()
		} else {
			m.mainPane.GoToTop()
		}
		return m, nil

	case key.Matches(msg, keys.GoBottom):
		if m.focus == SidebarFocus {
			m.sidebar.SelectLast()
			m.updateMainContent()
		} else {
			m.mainPane.GoToBottom()
		}
		return m, nil

	case key.Matches(msg, keys.SidebarGrow):
		if m.sidebarPct < 50 {
			m.sidebarPct += 5
			m.updateLayout()
		}
		return m, nil

	case key.Matches(msg, keys.SidebarShrink):
		if m.sidebarPct > 10 {
			m.sidebarPct -= 5
			m.updateLayout()
		}
		return m, nil

	case key.Matches(msg, keys.ToggleIgnored):
		if m.mode == FilesMode {
			m.showIgnored = !m.showIgnored
			m.updateSidebarItems()
		}
		return m, nil

	case key.Matches(msg, keys.NextLeaf):
		m.jumpToNextLeaf(1)
		return m, nil

	case key.Matches(msg, keys.PrevLeaf):
		m.jumpToNextLeaf(-1)
		return m, nil

	case key.Matches(msg, keys.YankPath):
		return m, m.yankPath()

	case key.Matches(msg, keys.Refresh):
		if m.git == nil {
			return m, m.loadNonGitFiles
		}
		return m, tea.Batch(m.loadLocalGitData, m.loadPRStatus)

	case key.Matches(msg, keys.ScopeReset):
		if m.naturalBase == "" || m.base == m.naturalBase {
			return m, nil
		}
		m.base = m.naturalBase
		if m.git == nil {
			return m, nil
		}
		return m, m.loadLocalGitData

	case key.Matches(msg, keys.ScopeExtendBack):
		if m.git == nil || m.base == "" {
			return m, nil
		}
		parent, err := m.git.Parent(m.base)
		if err != nil {
			// At root commit (or other failure) — no-op.
			return m, nil
		}
		m.base = parent
		return m, m.loadLocalGitData

	case key.Matches(msg, keys.ScopeContractForward):
		if m.git == nil || m.base == "" {
			return m, nil
		}
		// Walk one commit toward HEAD via first-parent. Done via git
		// rather than reading m.commits[len-1] because on main / master /
		// detached HEAD, m.commits lists the full repo history (oldest
		// entry would be the root commit, not the child of m.base).
		child, err := m.git.FirstChildToward(m.base, "HEAD")
		if err != nil {
			// No child — we're at the workdir limit (m.base == HEAD).
			return m, nil
		}
		m.base = child
		return m, m.loadLocalGitData

	case key.Matches(msg, keys.PRBrowse):
		if m.prInfo.Number == 0 || m.prInfo.URL == "" {
			return m, nil
		}
		return m, m.openInBrowser(m.prInfo.URL)

	case key.Matches(msg, keys.ToggleSidebar):
		m.sidebarHidden = !m.sidebarHidden
		m.updateLayout()
		return m, nil

	case key.Matches(msg, keys.ToggleWrap):
		m.wordWrap = !m.wordWrap
		if m.wordWrap {
			m.mainPane.xOffset = 0 // reset horizontal scroll when enabling wrap
		}
		m.mainPane.SetWordWrap(m.wordWrap)
		return m, nil

	case key.Matches(msg, keys.ToggleLineNums):
		m.lineNumbers = !m.lineNumbers
		m.mainPane.SetLineNumbers(m.lineNumbers)
		return m, nil

	case key.Matches(msg, keys.ToggleRemoved):
		if m.mode == FilesMode {
			m.mainPane.ToggleShowRemoved()
		}
		return m, nil

	case key.Matches(msg, keys.NextDiff):
		if m.mode == FilesMode {
			m.jumpToNextDiff(1)
		}
		return m, nil

	case key.Matches(msg, keys.PrevDiff):
		if m.mode == FilesMode {
			m.jumpToNextDiff(-1)
		}
		return m, nil

	case key.Matches(msg, keys.Enter):
		return m.handleEnter()

	case key.Matches(msg, keys.Up):
		if m.focus == SidebarFocus {
			m.sidebar.SelectPrev()
			m.updateMainContent()
			return m, nil
		}

	case key.Matches(msg, keys.Down):
		if m.focus == SidebarFocus {
			m.sidebar.SelectNext()
			m.updateMainContent()
			return m, nil
		}

	case key.Matches(msg, keys.PageDown):
		if m.focus == SidebarFocus {
			// Page down in sidebar
			for range m.sidebar.visibleLines() {
				m.sidebar.SelectNext()
			}
			m.updateMainContent()
			return m, nil
		}

	case key.Matches(msg, keys.PageUp):
		if m.focus == SidebarFocus {
			// Page up in sidebar
			for range m.sidebar.visibleLines() {
				m.sidebar.SelectPrev()
			}
			m.updateMainContent()
			return m, nil
		}
	}

	// Forward unhandled keys to main pane when it has focus (scrolling, half-page, etc.)
	if m.focus == MainFocus {
		cmd := m.mainPane.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) updateHelpSearchMatches() {
	m.helpSearchMatches = nil
	if m.helpSearchQuery == "" {
		return
	}
	q := strings.ToLower(m.helpSearchQuery)
	for i, line := range m.helpContentLines() {
		if strings.Contains(strings.ToLower(line), q) {
			m.helpSearchMatches = append(m.helpSearchMatches, i)
		}
	}
	m.helpSearchIdx = 0
	if len(m.helpSearchMatches) > 0 {
		m.helpScrollOffset = m.helpSearchMatches[0]
	}
}

func (m *Model) handleHelpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.helpSearching {
		switch {
		case msg.Code == tea.KeyEscape:
			m.helpSearching = false
			m.helpSearchQuery = ""
			m.helpSearchMatches = nil
			return m, nil
		case msg.Code == tea.KeyEnter:
			m.helpSearching = false
			if len(m.helpSearchMatches) > 0 {
				m.helpSearchConfirmed = true
			}
			return m, nil
		case msg.Code == tea.KeyBackspace:
			if len(m.helpSearchQuery) > 0 {
				m.helpSearchQuery = m.helpSearchQuery[:len(m.helpSearchQuery)-1]
			}
			if m.helpSearchQuery == "" {
				m.helpSearching = false
				m.helpSearchConfirmed = false
				m.helpSearchMatches = nil
				return m, nil
			}
			m.updateHelpSearchMatches()
			return m, nil
		default:
			if msg.Text != "" {
				m.helpSearchQuery += msg.Text
			}
			m.updateHelpSearchMatches()
			return m, nil
		}
	}

	// n/p navigation in help search confirmed mode
	if m.helpSearchConfirmed {
		switch {
		case key.Matches(msg, keys.SearchNext):
			if len(m.helpSearchMatches) > 0 {
				m.helpSearchIdx = (m.helpSearchIdx + 1) % len(m.helpSearchMatches)
				m.helpScrollOffset = m.helpSearchMatches[m.helpSearchIdx]
			}
			return m, nil
		case key.Matches(msg, keys.SearchPrev):
			if len(m.helpSearchMatches) > 0 {
				m.helpSearchIdx = (m.helpSearchIdx - 1 + len(m.helpSearchMatches)) % len(m.helpSearchMatches)
				m.helpScrollOffset = m.helpSearchMatches[m.helpSearchIdx]
			}
			return m, nil
		case msg.Code == tea.KeyEscape, key.Matches(msg, keys.QuitConfirm):
			m.helpSearchConfirmed = false
			m.helpSearchQuery = ""
			m.helpSearchMatches = nil
			return m, nil
		default:
			m.helpSearchConfirmed = false
			m.helpSearchQuery = ""
			m.helpSearchMatches = nil
			return m.handleHelpKey(msg)
		}
	}

	helpLines := m.helpContentLines()
	visibleHeight := max(1, m.height-m.statusBarLines()-2) // status bar + borders

	switch {
	case key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.Help):
		m.showHelp = false
		m.helpScrollOffset = 0
		m.helpSearchQuery = ""
		m.helpSearchMatches = nil
		return m, nil
	case key.Matches(msg, keys.Search):
		m.helpSearching = true
		m.helpSearchQuery = ""
		m.helpSearchMatches = nil
		return m, nil
	case key.Matches(msg, keys.Down):
		if m.helpScrollOffset < len(helpLines)-visibleHeight {
			m.helpScrollOffset++
		}
		return m, nil
	case key.Matches(msg, keys.Up):
		if m.helpScrollOffset > 0 {
			m.helpScrollOffset--
		}
		return m, nil
	case key.Matches(msg, keys.PageDown):
		maxOffset := max(0, len(helpLines)-visibleHeight)
		m.helpScrollOffset = min(m.helpScrollOffset+visibleHeight, maxOffset)
		return m, nil
	case key.Matches(msg, keys.PageUp):
		m.helpScrollOffset = max(0, m.helpScrollOffset-visibleHeight)
		return m, nil
	case key.Matches(msg, keys.GoBottom):
		m.helpScrollOffset = max(0, len(helpLines)-visibleHeight)
		return m, nil
	case key.Matches(msg, keys.QuitImmediate):
		return m, tea.Quit
	default:
		// Any other key dismisses help
		m.showHelp = false
		m.helpScrollOffset = 0
		m.helpSearchQuery = ""
		m.helpSearchMatches = nil
		return m, nil
	}
}

func (m *Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.QuitImmediate):
		m.clearSearch()
		return m, nil
	case msg.Code == tea.KeyEscape:
		m.clearSearch()
		return m, nil
	case msg.Code == tea.KeyEnter:
		if m.searchQuery == "" {
			m.clearSearch()
			return m, nil
		}
		m.searching = false
		if len(m.searchMatches) > 0 {
			m.searchConfirmed = true
		}
		return m, nil
	case msg.Code == tea.KeyBackspace:
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
		if m.searchQuery == "" {
			m.clearSearch()
			return m, nil
		}
		m.updateSearchMatches()
		return m, nil
	default:
		if msg.Text != "" {
			m.searchQuery += msg.Text
		}
		m.updateSearchMatches()
		return m, nil
	}
}

func (m *Model) handleSearchNavKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.SearchNext):
		if len(m.searchMatches) > 0 {
			m.searchMatchIdx = (m.searchMatchIdx + 1) % len(m.searchMatches)
			m.navigateToCurrentMatch()
		}
		return m, nil
	case key.Matches(msg, keys.SearchPrev):
		if len(m.searchMatches) > 0 {
			m.searchMatchIdx = (m.searchMatchIdx - 1 + len(m.searchMatches)) % len(m.searchMatches)
			m.navigateToCurrentMatch()
		}
		return m, nil
	case msg.Code == tea.KeyEscape, key.Matches(msg, keys.QuitConfirm):
		// Esc/q just exits search mode, doesn't trigger quit
		m.clearSearch()
		return m, nil
	default:
		// Any other key exits search navigation mode and re-processes
		m.clearSearch()
		return m.handleKey(msg)
	}
}

func (m *Model) updateSearchMatches() {
	var matches []searchMatch

	// Spec: "searching should match against the content in the main pane only (not the sidebar)"
	for _, line := range m.mainPane.FindMatches(m.searchQuery) {
		matches = append(matches, searchMatch{pane: "main", line: line})
	}

	m.searchMatches = matches
	m.searchMatchIdx = 0
	m.mainPane.SetSearchQuery(m.searchQuery)
	m.navigateToCurrentMatch()
}

func (m *Model) navigateToCurrentMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	match := m.searchMatches[m.searchMatchIdx]
	switch match.pane {
	case "sidebar":
		m.sidebar.SelectIndex(match.line)
		m.updateMainContent()
	case "main":
		m.mainPane.ScrollToLine(match.line)
	}
}

func (m *Model) clearSearch() {
	m.searching = false
	m.searchConfirmed = false
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchMatchIdx = 0
	m.mainPane.SetSearchQuery("")
}

func (m *Model) statusBarLines() int {
	return statusBarLineCount(statusBarData{info: m.repoInfo, pr: m.prInfo, prLoading: (m.loading || !m.prLoadedOnce) && m.git != nil})
}

func (m *Model) sidebarPixelWidth() int {
	// sidebar width + 2 for border
	return m.sidebar.width + 2
}

func (m *Model) handleStatusBarClick(x, y int) (tea.Model, tea.Cmd) {
	if m.git == nil {
		return m, nil
	}
	switch y {
	case 0:
		// Line 1: click on specific mode label to switch to that mode
		for _, label := range m.modeLabels {
			if x >= label.start && x < label.end {
				if label.mode == HelpMode {
					m.showHelp = !m.showHelp
				} else {
					m.showHelp = false
					m.setMode(label.mode)
				}
				return m, nil
			}
		}
	case 1:
		// Line 2: local git status — click on specific elements
		for _, label := range m.line2Labels {
			if x >= label.start && x < label.end {
				switch label.target {
				case line2CommitsMode:
					if len(m.commits) > 0 {
						m.setMode(CommitsMode)
					}
				case line2FilesMode:
					m.setMode(FilesMode)
				}
				return m, nil
			}
		}
		// Fallback: anywhere on line 2 goes to commits mode
		if len(m.commits) > 0 {
			m.setMode(CommitsMode)
		}
	case 2:
		// Line 3: PR status — click on specific elements. Clicking any
		// label jumps to a specific item, overriding the restored state.
		if m.prInfo.Number > 0 {
			for _, label := range m.line3Labels {
				if x >= label.start && x < label.end {
					m.setMode(PRMode)
					switch label.target {
					case line3Description:
						m.sidebar.SelectFirst()
					case line3Reviews:
						m.selectFirstReview()
					case line3Comments:
						m.selectFirstComment()
					case line3CI:
						m.selectFirstCIFailure()
					}
					m.updateMainContent()
					return m, nil
				}
			}
		}
	}
	return m, nil
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y

	// Status bar is rows 0-2
	if y < m.statusBarLines() {
		m.dragging = false
		return m.handleStatusBarClick(x, y)
	}

	// Adjust y for the 3-line status bar
	contentY := y - m.statusBarLines()
	sidebarW := m.sidebarPixelWidth()
	if !m.sidebarHidden && x < sidebarW {
		// Clicked in sidebar — no drag tracking
		m.dragging = false
		m.focus = SidebarFocus
		// Content starts after status bar (2 lines) + top border (1 line) = row 3
		itemIdx := contentY - 1 + m.sidebar.offset
		// If the user clicked the topmost row while a sticky header is being
		// overlaid there, route the click to the header itself (which is
		// non-selectable, so this becomes a no-op rather than selecting the
		// hidden item underneath).
		if contentY == 1 {
			if sticky := m.sidebar.stickyHeaderIndex(); sticky >= 0 {
				itemIdx = sticky
			}
		}
		m.sidebar.SelectIndex(itemIdx)
		// "Load more" in commit mode triggers pagination
		if m.mode == CommitsMode && strings.HasPrefix(m.sidebar.SelectedItem(), "load more") {
			return m, m.loadMoreCommits
		}
		// If a directory was clicked, toggle collapse — except for an
		// unloaded ignored dir, where clicking should fire the lazy-load
		// cmd just like pressing right does. Without this branch, the
		// dir would flip to expanded with no children visible (forever).
		if m.sidebar.SelectedIsDir() {
			dir := m.sidebar.SelectedItem()
			key := m.sidebar.SelectedCollapseKey()
			if m.ignoredDirs[dir] && !m.loadedIgnoredDirs[dir] {
				return m, m.expandIgnoredDir(dir)
			}
			if key != "" {
				m.collapsedDirs[key] = !m.collapsedDirs[key]
			}
			selectedBefore := m.sidebar.SelectedItem()
			m.updateSidebarItems()
			// Collapsing may shift which item is at the selected index;
			// update the main panel if the effective selection changed.
			if m.sidebar.SelectedItem() != selectedBefore {
				m.updateMainContent()
			}
			return m, nil
		}
		m.updateMainContent()
	} else {
		// Clicked in main pane — start drag tracking for copy
		m.focus = MainFocus
		m.dragging = true
		m.dragStartX = x
		m.dragStartY = y
		m.dragEndX = x
		m.dragEndY = y
	}
	return m, nil
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	x := msg.X
	sidebarW := m.sidebarPixelWidth()

	if x < sidebarW {
		// Scroll sidebar view without changing selection
		if msg.Button == tea.MouseWheelUp {
			m.sidebar.ScrollUp()
		} else {
			m.sidebar.ScrollDown()
		}
	} else {
		// Horizontal scrolling (when word wrap is off)
		// Support both native horizontal wheel events and Shift+vertical wheel
		if !m.wordWrap {
			isHorizScroll := msg.Button == tea.MouseWheelLeft || msg.Button == tea.MouseWheelRight
			isShiftVertScroll := (msg.Button == tea.MouseWheelUp || msg.Button == tea.MouseWheelDown) && msg.Mod&tea.ModShift != 0
			if isHorizScroll || isShiftVertScroll {
				scrollLeft := msg.Button == tea.MouseWheelLeft || (isShiftVertScroll && msg.Button == tea.MouseWheelUp)
				if scrollLeft {
					m.mainPane.ScrollLeft(4)
				} else {
					m.mainPane.ScrollRight(4)
				}
				return m, nil
			}
		}
		// Vertical scrolling — forward to main pane viewport
		cmd := m.mainPane.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.focus == SidebarFocus {
		// "Load more" in commit mode triggers pagination
		if m.mode == CommitsMode && strings.HasPrefix(m.sidebar.SelectedItem(), "load more") {
			return m, m.loadMoreCommits
		}
		// Enter behaves like Right
		return m.handleSidebarRight()
	}

	// Main pane focused
	if m.mode == FilesMode {
		return m, m.openEditor()
	}
	if m.mode == PRMode {
		return m, m.openPRItemURL()
	}
	return m, nil
}

// handleSidebarLeft handles left/h key when sidebar is focused.
// Collapses the selected directory or jumps to its parent.
func (m *Model) handleSidebarLeft() (tea.Model, tea.Cmd) {
	if m.sidebar.SelectedIsDir() {
		// Collapse the directory if it's open
		key := m.sidebar.SelectedCollapseKey()
		if key != "" && !m.collapsedDirs[key] {
			m.collapsedDirs[key] = true
			m.updateSidebarItems()
			return m, nil
		}
	}

	// Go to nearest parent directory
	idx := m.sidebar.SelectedIndex()
	currentIndent := -1
	if idx < len(m.sidebar.items) {
		currentIndent = m.sidebar.items[idx].indent
	}
	for i := idx - 1; i >= 0; i-- {
		item := m.sidebar.items[i]
		if item.isDir && item.indent < currentIndent {
			m.sidebar.SelectIndex(i)
			m.updateMainContent()
			return m, nil
		}
	}
	return m, nil
}

// handleSidebarRight handles right/l/enter key when sidebar is focused.
// Expands a collapsed directory, jumps to its first child, or (on a leaf
// file) switches focus to the main pane.
//
// Special case: an unloaded ignored directory (in m.ignoredDirs but not
// m.loadedIgnoredDirs) fires a Cmd to lazy-load its contents. The dir
// transitions from collapsed-empty to expanded-with-contents once the
// resulting ignoredDirLoadedMsg arrives.
func (m *Model) handleSidebarRight() (tea.Model, tea.Cmd) {
	if m.sidebar.SelectedIsDir() {
		dir := m.sidebar.SelectedItem()
		key := m.sidebar.SelectedCollapseKey()
		if m.ignoredDirs[dir] && !m.loadedIgnoredDirs[dir] {
			return m, m.expandIgnoredDir(dir)
		}
		if key != "" && m.collapsedDirs[key] {
			// Expand the directory
			m.collapsedDirs[key] = false
			m.updateSidebarItems()
		} else {
			// Already expanded — move to first child
			m.sidebar.SelectNext()
			m.updateMainContent()
		}
		return m, nil
	}

	// Leaf node — switch to main pane
	m.focus = MainFocus
	return m, nil
}

func (m *Model) openEditor() tea.Cmd {
	file := m.sidebar.SelectedItem()
	if file == "" {
		return nil
	}

	editor, args := m.buildEditorCmd(file)
	cmd := m.cmdFactory(editor, args...)
	cmd.SetDir(m.dir)
	return tea.Exec(cmd, func(err error) tea.Msg {
		return RefreshMsg{}
	})
}

// openPRItemURL opens the URL for the currently selected PR sidebar item in the browser.
// Handles: PR description, comments, reviews, and CI checks.
func (m *Model) openPRItemURL() tea.Cmd {
	selected := m.sidebar.SelectedItem()
	if selected == "" {
		return nil
	}

	var url string

	// Check if it's the Description item
	if selected == "Description" {
		url = m.prInfo.URL
	}

	// Check comments
	if url == "" {
		for _, c := range m.prComments {
			if strings.Contains(selected, c.Author) && c.URL != "" {
				url = c.URL
				break
			}
		}
	}

	// Check reviews
	if url == "" {
		for _, r := range m.prReviews {
			if strings.Contains(selected, r.Author) && r.URL != "" {
				url = r.URL
				break
			}
		}
	}

	// Check CI checks
	if url == "" {
		for _, check := range m.ciChecks {
			if strings.Contains(selected, check.Name) && check.URL != "" {
				url = check.URL
				break
			}
		}
	}

	if url == "" {
		return nil
	}

	return m.openInBrowser(url)
}

// openInBrowser opens a URL in the default system browser.
func (m *Model) openInBrowser(url string) tea.Cmd {
	var cmd command.Command
	switch runtime.GOOS {
	case "darwin":
		cmd = m.cmdFactory("open", url)
	case "linux":
		cmd = m.cmdFactory("xdg-open", url)
	default:
		return nil
	}
	return tea.Exec(cmd, func(err error) tea.Msg {
		return RefreshMsg{}
	})
}

// buildEditorCmd returns the editor command and arguments for opening a file.
// Exported for testing.
func (m *Model) buildEditorCmd(file string) (string, []string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	var args []string
	line := m.currentLineNumber()
	if line > 0 {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, file)
	return editor, args
}

// currentLineNumber finds the source line at the viewport top. Files mode
// displays raw file content, so the line at the viewport top is just
// scroll offset + 1. This is only meaningful in files mode — callers in
// other modes should gate accordingly.
func (m *Model) currentLineNumber() int {
	return m.mainPane.ScrollTop() + 1
}

func (m *Model) isUncommittedFile(file string) bool {
	return containsString(m.uncommittedFiles, file) || containsString(m.stagedFiles, file)
}

func (m *Model) selectFirstComment() {
	if i := firstCommentIndex(m.sidebar.items); i >= 0 {
		m.sidebar.SelectIndex(i)
	}
}

func (m *Model) selectFirstReview() {
	if i := firstReviewIndex(m.sidebar.items); i >= 0 {
		m.sidebar.SelectIndex(i)
	}
}

func (m *Model) selectFirstCIFailure() {
	if i := firstCIFailureIndex(m.sidebar.items, m.ciChecks); i >= 0 {
		m.sidebar.SelectIndex(i)
	}
}

func (m *Model) jumpToFirstDiff() {
	if line, ok := nextDiffLine(m.mainPane.DiffLineNumbers(), -1, +1); ok {
		m.mainPane.ScrollToSourceLine(line)
	}
}

func (m *Model) jumpToNextDiff(direction int) {
	if line, ok := nextDiffLine(m.mainPane.DiffLineNumbers(), m.mainPane.ViewportToSourceLine(), direction); ok {
		m.mainPane.ScrollToSourceLine(line)
	}
}

func (m *Model) jumpToNextLeaf(direction int) {
	if idx := nextLeafIndex(m.sidebar.items, m.sidebar.SelectedIndex(), direction); idx >= 0 {
		m.sidebar.SelectIndex(idx)
		m.updateMainContent()
	}
}

// commitIndexFromSidebarItem extracts the commit index from a sidebar label
// of the form "abcdef0 subject".
func (m *Model) commitIndexFromSidebarItem(label string) int {
	parts := strings.SplitN(label, " ", 2)
	if len(parts) == 0 {
		return -1
	}
	sha := parts[0]
	for i, c := range m.commits {
		short := c.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		if short == sha {
			return i
		}
	}
	return -1
}

func (m *Model) isDeletedFile(file string) bool {
	return containsString(m.deletedFiles, file)
}

func (m *Model) fileItemKind(file string, defaultKind sidebarItemKind) sidebarItemKind {
	if m.isDeletedFile(file) {
		return itemDeleted
	}
	return defaultKind
}

func (m *Model) changeBadge(file string) string {
	return changeBadgeFor(file, m.deletedFiles, m.addedFiles, m.committedFiles, m.uncommittedFiles, m.stagedFiles)
}

func (m *Model) applyChangeBadges(items []sidebarItem) []sidebarItem {
	return applyChangeBadges(items, m.deletedFiles, m.addedFiles, m.committedFiles, m.uncommittedFiles, m.stagedFiles)
}

func (m *Model) isCommittedFile(file string) bool {
	return containsString(m.committedFiles, file)
}

func (m *Model) updateSidebarItems() {
	// Snapshot collapse state and sidebar before the update for debug diffing.
	var collapseBefore map[string]bool
	var selectedBefore int
	var selectedItemBefore string
	var offsetBefore int
	if m.debugLog != nil {
		collapseBefore = make(map[string]bool, len(m.collapsedDirs))
		for d, v := range m.collapsedDirs {
			collapseBefore[d] = v
		}
		selectedBefore = m.sidebar.SelectedIndex()
		selectedItemBefore = m.sidebar.SelectedItem()
		offsetBefore = m.sidebar.offset
	}
	defer func() {
		if m.debugLog == nil {
			return
		}
		_, file, line, _ := runtime.Caller(1)
		caller := fmt.Sprintf("%s:%d", filepath.Base(file), line)

		// Log collapse state changes
		for d, v := range m.collapsedDirs {
			if collapseBefore[d] != v {
				m.debugLog.Printf("[collapse-change] %s: %q: %v -> %v (caller=%s mode=%d)",
					"after", d, collapseBefore[d], v, caller, m.mode)
			}
		}
		// Log dirs removed from collapse map (shouldn't happen, but just in case)
		for d, v := range collapseBefore {
			if _, exists := m.collapsedDirs[d]; !exists {
				m.debugLog.Printf("[collapse-removed] %s: %q was %v (caller=%s mode=%d)",
					"after", d, v, caller, m.mode)
			}
		}
		// Log selection/scroll changes
		selectedAfter := m.sidebar.SelectedIndex()
		selectedItemAfter := m.sidebar.SelectedItem()
		offsetAfter := m.sidebar.offset
		if selectedAfter != selectedBefore || selectedItemAfter != selectedItemBefore {
			m.debugLog.Printf("[selection-change] %d(%q) -> %d(%q) (caller=%s mode=%d)",
				selectedBefore, selectedItemBefore, selectedAfter, selectedItemAfter, caller, m.mode)
		}
		if offsetAfter != offsetBefore {
			m.debugLog.Printf("[scroll-change] offset %d -> %d (caller=%s mode=%d)",
				offsetBefore, offsetAfter, caller, m.mode)
		}
	}()

	switch m.mode {
	case FilesMode:
		var items []sidebarItem
		// Compute other files (not in committed, staged, or uncommitted)
		changedSet := make(map[string]bool)
		for _, f := range m.uncommittedFiles {
			changedSet[f] = true
		}
		for _, f := range m.stagedFiles {
			changedSet[f] = true
		}
		for _, f := range m.committedFiles {
			changedSet[f] = true
		}
		var otherFiles []string
		for _, f := range m.allFiles {
			if !changedSet[f] {
				otherFiles = append(otherFiles, f)
			}
		}
		// Add gitignored top-level entries to the All Files section. These
		// come from IgnoredEntries (--directory), so an entire ignored
		// subtree like node_modules/ shows up as a single dim leaf.
		if m.showIgnored {
			for path := range m.ignoredFiles {
				if !changedSet[path] {
					otherFiles = append(otherFiles, path)
				}
			}
		}

		// Auto-collapse directories by default. Each section maintains its
		// own collapse state (so "pkg/" under Committed and "pkg/" under
		// All Files don't share a single open/closed flag).
		autoCollapseChanged := func(section string, dirs []string) {
			for _, d := range dirs {
				key := dirCollapseKey(section, d)
				if _, exists := m.collapsedDirs[key]; exists {
					continue
				}
				if m.git == nil {
					// Non-git: collapse all dirs by default (no concept of "changed" files)
					m.collapsedDirs[key] = true
				} else {
					// Git: auto-collapse hidden (dot-prefixed) directories,
					// explicitly expand others so the "All Files" section's
					// default-closed logic doesn't override them.
					base := d
					if i := strings.LastIndex(d, "/"); i >= 0 {
						base = d[i+1:]
					}
					m.collapsedDirs[key] = strings.HasPrefix(base, ".")
				}
			}
		}
		autoCollapseChanged(sectionUncommitted, extractDirs(m.uncommittedFiles))
		autoCollapseChanged(sectionStaged, extractDirs(m.stagedFiles))
		autoCollapseChanged(sectionCommitted, extractDirs(m.committedFiles))
		if len(m.uncommittedFiles) > 0 {
			items = append(items, sidebarItem{label: fmt.Sprintf("New Changes (%d)", len(m.uncommittedFiles)), kind: itemHeader})
			items = append(items, buildTreeItems(m.uncommittedFiles, itemNormal, sectionUncommitted, m.collapsedDirs, nil)...)
		}
		if len(m.stagedFiles) > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("Staged (%d)", len(m.stagedFiles)), kind: itemHeader})
			items = append(items, buildTreeItems(m.stagedFiles, itemNormal, sectionStaged, m.collapsedDirs, nil)...)
		}
		if len(m.committedFiles) > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("Committed (%d)", len(m.committedFiles)), kind: itemHeader})
			items = append(items, buildTreeItems(m.committedFiles, itemNormal, sectionCommitted, m.collapsedDirs, nil, func(f string) sidebarItemKind { return m.fileItemKind(f, itemNormal) })...)
		}
		if len(otherFiles) > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("All Files (%d)", len(otherFiles)), kind: itemHeader})
			// All-files trees default to collapsed (spec: "trees should start out closed").
			allFilesDirs := extractDirs(otherFiles)
			for _, d := range allFilesDirs {
				key := dirCollapseKey(sectionAllFiles, d)
				if _, exists := m.collapsedDirs[key]; !exists {
					m.collapsedDirs[key] = true // default closed for all-files
				}
			}
			// Ignored top-level dirs are single-segment paths in the file
			// list. Default-collapse them, and pass them as forceDirs so
			// buildTreeItems classifies them in the dir bucket from the
			// start — without this, an unloaded ignored dir sorts as a
			// leaf and jumps when the user expands it.
			for d := range m.ignoredDirs {
				key := dirCollapseKey(sectionAllFiles, d)
				if _, exists := m.collapsedDirs[key]; !exists {
					m.collapsedDirs[key] = true
				}
			}
			items = append(items, buildTreeItems(otherFiles, itemNormal, sectionAllFiles, m.collapsedDirs, m.ignoredDirs, func(f string) sidebarItemKind {
				if m.ignoredFiles[f] {
					return itemDim
				}
				return itemNormal
			})...)
		}
		m.sidebar.SetItems(m.applyChangeBadges(items))
	case CommitsMode:
		var items []sidebarItem
		unpushed := m.repoInfo.AheadCount
		pushedCount := len(m.commits) - unpushed
		if pushedCount < 0 {
			pushedCount = 0
		}

		// Category 1: New changes (unstaged/untracked)
		if len(m.uncommittedFiles) > 0 {
			items = append(items, sidebarItem{label: fmt.Sprintf("New Changes (%d files)", len(m.uncommittedFiles)), kind: itemHeader})
			items = append(items, sidebarItem{label: "new changes", kind: itemDim})
		}

		// Category 2: Staged changes
		if len(m.stagedFiles) > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("Staged (%d files)", len(m.stagedFiles)), kind: itemHeader})
			items = append(items, sidebarItem{label: "staged changes", kind: itemDim})
		}

		// Category 3: Unpushed commits (dimmed)
		unpushedVisible := unpushed
		if unpushedVisible > len(m.commits) {
			unpushedVisible = len(m.commits)
		}
		if unpushedVisible > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("Unpushed (%d)", unpushedVisible), kind: itemHeader})
			for i := 0; i < unpushedVisible; i++ {
				c := m.commits[i]
				items = append(items, sidebarItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
					kind:  itemDim,
				})
			}
		}

		// Category 4: Pushed branch commits
		if pushedCount > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("Pushed (%d)", pushedCount), kind: itemHeader})
			for i := unpushed; i < len(m.commits); i++ {
				c := m.commits[i]
				items = append(items, sidebarItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
					kind:  itemNormal,
				})
			}
		}

		// "Load more" entry if there are more commits to load
		if m.commitsLoaded < m.commitCount {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			remaining := m.commitCount - m.commitsLoaded
			items = append(items, sidebarItem{
				label: fmt.Sprintf("load more (%d remaining)", remaining),
				kind:  itemDim,
			})
		}

		// Category 5: Base branch commits (already in base, before the feature branch).
		// A scope cutline marks the boundary between in-scope (above) and
		// out-of-scope (Base, below) commits.
		if len(m.baseCommits) > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemCutline})
			}
			items = append(items, sidebarItem{label: fmt.Sprintf("Base (%d)", len(m.baseCommits)), kind: itemHeader})
			for _, c := range m.baseCommits {
				items = append(items, sidebarItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
					kind:  itemDim,
				})
			}
		}

		m.sidebar.SetItems(items)

	case PRMode:
		var items []sidebarItem
		// PR description
		items = append(items, sidebarItem{label: "Description", kind: itemNormal})
		items = append(items, sidebarItem{kind: itemSeparator})

		// Comments
		items = append(items, sidebarItem{label: fmt.Sprintf("Comments (%d)", len(m.prComments)), kind: itemHeader})
		for i, c := range m.prComments {
			items = append(items, sidebarItem{
				prefix: fmt.Sprintf("#%d ", len(m.prComments)-i),
				label:  fmt.Sprintf("@%s", c.Author),
				suffix: " " + relativeTime(c.CreatedAt),
				kind:   itemNormal,
			})
		}
		if len(m.prComments) == 0 {
			items = append(items, sidebarItem{label: "(no comments)", kind: itemDim})
		}

		// Reviews
		items = append(items, sidebarItem{kind: itemSeparator})
		items = append(items, sidebarItem{label: fmt.Sprintf("Reviews (%d)", len(m.prReviews)), kind: itemHeader})
		for i, r := range m.prReviews {
			var emoji string
			switch r.State {
			case "APPROVED":
				emoji = "✓ "
			case "CHANGES_REQUESTED":
				emoji = "✗ "
			case "COMMENTED":
				emoji = "c "
			default:
				emoji = "… "
			}
			items = append(items, sidebarItem{
				prefix: fmt.Sprintf("#%d %s", len(m.prReviews)-i, emoji),
				label:  fmt.Sprintf("@%s", r.Author),
				suffix: " " + relativeTime(r.SubmittedAt),
				kind:   itemNormal,
			})
		}
		if len(m.prReviews) == 0 {
			items = append(items, sidebarItem{label: "(no reviews)", kind: itemDim})
		}

		// CI checks
		items = append(items, sidebarItem{kind: itemSeparator})
		items = append(items, sidebarItem{label: fmt.Sprintf("CI (%d)", len(m.ciChecks)), kind: itemHeader})
		for _, check := range m.ciChecks {
			var indicator string
			switch check.Bucket {
			case "pass":
				indicator = "[✓] "
			case "fail", "cancel":
				indicator = "[✗] "
			case "pending":
				indicator = "[…] "
			case "skipping":
				indicator = "[-] "
			default:
				indicator = "    "
			}
			ts := check.CompletedAt
			if ts.IsZero() {
				ts = check.StartedAt
			}
			items = append(items, sidebarItem{
				prefix: indicator,
				label:  check.Name,
				suffix: " " + relativeTime(ts),
				kind:   itemNormal,
			})
		}
		if len(m.ciChecks) == 0 {
			items = append(items, sidebarItem{label: "(no CI checks)", kind: itemDim})
		}

		m.sidebar.SetItems(items)
	}
}

// setMode changes the active mode, saving the current mode's view state
// (sidebar selection, scroll positions, focus) and restoring the new mode's
// previously-saved state if any. After swapping state, it refreshes sidebar
// items and main pane content to reflect the new mode.
func (m *Model) setMode(next Mode) {
	if next == m.mode {
		// No mode change, just refresh.
		m.updateSidebarItems()
		m.updateMainContent()
		return
	}
	// Save current mode's view state before switching.
	m.saveModeState()
	m.mode = next
	m.updateSidebarItems()
	// Restore the saved state for the new mode, if any. restoreModeState
	// must run after updateSidebarItems so the sidebar contains the items
	// we're going to match against.
	m.restoreModeState()
	m.updateMainContent()
}

// saveModeState captures the current sidebar selection/scroll/focus so
// setMode can restore them later. Main-pane scroll is tracked per-item via
// Model.mainScrollLines and applied by updateMainContent.
func (m *Model) saveModeState() {
	if m.modeStates == nil {
		m.modeStates = make(map[Mode]modeViewState)
	}
	m.modeStates[m.mode] = modeViewState{
		sidebarSelected: m.sidebar.SelectedItem(),
		sidebarOffset:   m.sidebar.offset,
		focus:           m.focus,
	}
}

// restoreModeState applies the previously-saved sidebar/focus state for the
// current mode. Safe to call when there is no saved state (no-op in that
// case). updateMainContent restores the main-pane scroll position separately.
func (m *Model) restoreModeState() {
	if m.modeStates == nil {
		return
	}
	state, ok := m.modeStates[m.mode]
	if !ok {
		return
	}
	// Restore sidebar selection by matching the item label.
	if state.sidebarSelected != "" {
		for i, item := range m.sidebar.items {
			if item.kind.selectable() && item.label == state.sidebarSelected {
				m.sidebar.SelectIndex(i)
				break
			}
		}
	}
	m.sidebar.offset = state.sidebarOffset
	m.sidebar.clampOffset()
	m.focus = state.focus
}

func (m *Model) updateMainContent() {
	// Save the source line at the top of the main pane under the item we
	// were just showing, so the next time the user navigates to it we can
	// drop them back at the same line.
	if m.lastMainItem.item != "" {
		if m.mainScrollLines == nil {
			m.mainScrollLines = make(map[mainItemKey]int)
		}
		m.mainScrollLines[m.lastMainItem] = m.mainPane.ViewportToSourceLine()
	}

	prevKey := m.lastMainItem
	// setItem records the (mode, item) currently displayed and, if it
	// differs from prevKey, restores the saved scroll for it (or applies a
	// per-mode default for first visits — currently jumpToFirstDiff for
	// files mode with a diff).
	setItem := func(key mainItemKey) {
		m.lastMainItem = key
		// Tell the sidebar which item the main pane is showing so it can
		// render the "pinned" file with a distinct style when the cursor
		// has moved off it (spec: "the sidebar should visually distinguish
		// the cursor position from the pinned file when they differ").
		m.sidebar.SetPinnedID(key.item)
		if key == prevKey || key.item == "" {
			return
		}
		if line, ok := m.mainScrollLines[key]; ok {
			m.mainPane.ScrollToSourceLine(line)
			return
		}
		if key.mode == FilesMode && len(m.mainPane.DiffLineNumbers()) > 0 {
			m.jumpToFirstDiff()
		}
	}

	if m.git == nil {
		// Non-git: files mode only, read from disk
		if m.mode == FilesMode {
			file := m.sidebar.SelectedItem()
			if file == "" {
				m.mainPane.SetFilename("")
				m.mainPane.SetPlainContent("")
				setItem(mainItemKey{m.mode, ""})
				return
			}
			if m.sidebar.SelectedIsDir() {
				return // preserve current main panel content (and lastMainItem)
			}
			content, err := os.ReadFile(filepath.Join(m.dir, file))
			if err != nil {
				m.mainPane.SetFilename("")
				m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
				setItem(mainItemKey{m.mode, file})
				return
			}
			if isBinaryContent(string(content)) {
				m.mainPane.SetFilename("")
				m.mainPane.SetPlainContent("[binary content]")
				setItem(mainItemKey{m.mode, file})
				return
			}
			m.mainPane.SetFilename(file)
			m.mainPane.SetPlainContent(string(content))
			setItem(mainItemKey{m.mode, file})
		}
		return
	}
	if m.base == "" {
		return // preserve current main panel content (and lastMainItem)
	}

	switch m.mode {
	case FilesMode:
		file := m.sidebar.SelectedItem()
		if file == "" {
			m.mainPane.SetFilename("")
			m.mainPane.SetPlainContent("")
			m.mainPane.ClearDiffAnnotations()
			m.mainPane.ClearDiffHunks()
			m.mainPane.SetTitle("", "")
			setItem(mainItemKey{m.mode, ""})
			return
		}
		if m.sidebar.SelectedIsDir() {
			return // preserve current main panel content (and lastMainItem)
		}
		content, err := m.git.FileContent(file)
		if err != nil {
			m.mainPane.SetFilename("")
			m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
			m.mainPane.ClearDiffAnnotations()
			m.mainPane.ClearDiffHunks()
			m.mainPane.SetTitle(file, "error")
			setItem(mainItemKey{m.mode, file})
			return
		}
		if isBinaryContent(content) {
			m.mainPane.SetFilename("")
			m.mainPane.SetPlainContent("[binary content]")
			m.mainPane.ClearDiffAnnotations()
			m.mainPane.ClearDiffHunks()
			m.mainPane.SetNoHunkRight(m.fileContextRight(file, true))
			m.mainPane.SetDiffPrefix("")
			m.mainPane.SetTitleWithHunks(file)
			setItem(mainItemKey{m.mode, file})
			return
		}
		// Compute diff annotations for the gutter
		var diff string
		if m.isUncommittedFile(file) {
			diff, _ = m.git.FileDiffUncommitted(file)
		} else if m.isCommittedFile(file) {
			diff, _ = m.git.FileDiffCommitted(m.base, file)
		}
		if diff != "" {
			annotations := parseDiffAnnotations(diff)
			// For completely deleted files, mark all lines as removed
			if m.isDeletedFile(file) {
				contentLines := strings.Split(content, "\n")
				annotations = make(map[int]diffAnnotation, len(contentLines))
				for i := range contentLines {
					annotations[i+1] = diffAnnotation{kind: diffLineRemoved}
				}
			}
			m.mainPane.SetDiffAnnotations(annotations)
			m.mainPane.SetDiffHunks(parseDiffHunks(diff))
			m.mainPane.SetNoHunkRight("")
			m.mainPane.SetDiffPrefix(m.fileDiffPrefix(file))
		} else {
			m.mainPane.ClearDiffAnnotations()
			m.mainPane.ClearDiffHunks()
			m.mainPane.SetNoHunkRight(m.fileContextRight(file, false))
			m.mainPane.SetDiffPrefix("")
		}
		m.mainPane.SetFilename(file)
		m.mainPane.SetPlainContent(content)
		m.mainPane.SetTitleWithHunks(file)
		setItem(mainItemKey{m.mode, file})

	case CommitsMode:
		selected := m.sidebar.SelectedItem()
		if selected == "" {
			m.mainPane.SetContent("")
			m.mainPane.SetTitle("", "")
			setItem(mainItemKey{m.mode, ""})
			return
		}
		// Check if this is the "load more" entry
		if strings.HasPrefix(selected, "load more") {
			m.mainPane.SetPlainContent("Loading more commits...")
			m.mainPane.SetTitle(selected, "")
			setItem(mainItemKey{m.mode, selected})
			return
		}
		// Check if this is the "new changes" or "staged changes" entry
		if selected == "new changes" || selected == "staged changes" {
			// Show combined diff of all uncommitted/staged files
			diff, _ := m.git.FileDiffUncommitted("")
			m.mainPane.SetContent(diff)
			m.mainPane.SetTitle(selected, shortstatFromDiff(diff))
			setItem(mainItemKey{m.mode, selected})
			return
		}
		// Otherwise it's a commit — extract SHA from "abcdef0 subject"
		commitIdx := m.commitIndexFromSidebarItem(selected)
		if commitIdx < 0 || commitIdx >= len(m.commits) {
			m.mainPane.SetContent("")
			m.mainPane.SetTitle("", "")
			setItem(mainItemKey{m.mode, ""})
			return
		}
		commit := m.commits[commitIdx]
		patch, err := m.git.CommitPatch(commit.SHA)
		titleLeft := commitTitleLeft(commit)
		titleRight := formatAuthorAndTime(commit.Author, commit.AuthorDate)
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			m.mainPane.SetTitle(titleLeft, titleRight)
			setItem(mainItemKey{m.mode, selected})
			return
		}
		if isBinaryContent(patch) {
			m.mainPane.SetPlainContent("[binary content]")
			m.mainPane.SetTitle(titleLeft, titleRight)
			setItem(mainItemKey{m.mode, selected})
			return
		}
		m.mainPane.SetContent(patch)
		m.mainPane.SetTitle(titleLeft, titleRight)
		setItem(mainItemKey{m.mode, selected})

	case PRMode:
		selected := m.sidebar.SelectedItem()
		if selected == "Description" {
			m.mainPane.SetPlainContent(m.renderPRDescription())
			m.mainPane.SetTitle("Description", "")
		} else if matched, i := matchNumberedItem(selected, m.prComments, func(j int, c gitpkg.PRComment) string {
			return fmt.Sprintf("#%d @%s", len(m.prComments)-j, c.Author)
		}); matched {
			// Comment
			c := m.prComments[i]
			header := fmt.Sprintf("@%s", c.Author)
			if !c.CreatedAt.IsZero() {
				header += fmt.Sprintf("  •  %s (%s)", c.CreatedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(c.CreatedAt))
			}
			body := c.Body
			if rendered, err := renderMarkdown(body, m.mainPane.width); err == nil {
				body = rendered
			}
			m.mainPane.SetPlainContent(fmt.Sprintf("%s\n\n%s", header, body))
			m.mainPane.SetTitle(
				fmt.Sprintf("comment #%d", len(m.prComments)-i),
				formatAuthorAndTime(c.Author, c.CreatedAt),
			)
		} else if matched, i := matchNumberedItem(selected, m.prReviews, func(j int, r gitpkg.PRReview) string {
			var emoji string
			switch r.State {
			case "APPROVED":
				emoji = "✓ "
			case "CHANGES_REQUESTED":
				emoji = "✗ "
			case "COMMENTED":
				emoji = "c "
			default:
				emoji = "… "
			}
			return fmt.Sprintf("#%d %s@%s", len(m.prReviews)-j, emoji, r.Author)
		}); matched {
			// Review
			r := m.prReviews[i]
			content := fmt.Sprintf("Review by @%s", r.Author)
			if !r.SubmittedAt.IsZero() {
				content += fmt.Sprintf("  •  %s (%s)", r.SubmittedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(r.SubmittedAt))
			}
			content += fmt.Sprintf("\nState: %s", r.State)
			if r.Body != "" {
				body := r.Body
				if rendered, err := renderMarkdown(body, m.mainPane.width); err == nil {
					body = rendered
				}
				content += "\n\n" + body
			}
			for _, c := range r.Comments {
				content += fmt.Sprintf("\n\n--- %s:%d ---\n%s", c.Path, c.Line, c.Body)
			}
			m.mainPane.SetPlainContent(content)
			m.mainPane.SetTitle(
				fmt.Sprintf("review #%d · %s", len(m.prReviews)-i, reviewStateLabel(r.State)),
				formatAuthorAndTime(r.Author, r.SubmittedAt),
			)
		} else {
			// CI check — find the matching check
			matched := false
			for _, check := range m.ciChecks {
				if strings.Contains(selected, check.Name) {
					status := check.Bucket
					if status == "" {
						status = check.State
					}
					content := fmt.Sprintf("Check: %s\nStatus: %s", check.Name, status)
					if !check.StartedAt.IsZero() {
						content += fmt.Sprintf("\nStarted: %s (%s)", check.StartedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(check.StartedAt))
					}
					if !check.CompletedAt.IsZero() {
						content += fmt.Sprintf("\nCompleted: %s (%s)", check.CompletedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(check.CompletedAt))
					}
					if check.URL != "" {
						content += fmt.Sprintf("\nURL: %s", check.URL)
					}
					// If RWX, check cache or trigger async fetch
					if extra, _ := m.rwxFetcher.Lookup(check); extra != "" {
						content += "\n\n" + extra
					}
					m.mainPane.SetPlainContent(content)
					m.mainPane.SetTitle("CI · "+check.Name, status)
					matched = true
					break
				}
			}
			if !matched {
				m.mainPane.SetTitle(selected, "")
			}
		}
		setItem(mainItemKey{m.mode, selected})
	}
}

// computePRInterval returns the appropriate PR refresh interval based on
// user activity and server data freshness.
func (m *Model) computePRInterval() time.Duration {
	return m.activity.ComputePRInterval(time.Now())
}

func (m *Model) computeGitInterval() time.Duration {
	return m.activity.ComputeGitInterval(time.Now())
}

func (m *Model) updateLayout() {
	statusBarHeight := statusBarLineCount(statusBarData{
		info:      m.repoInfo,
		pr:        m.prInfo,
		prError:   m.prError,
		prLoading: (m.loading || !m.prLoadedOnce) && m.git != nil,
	})
	sidebarW, mainW, contentH := layoutDimensions(m.width, m.height, statusBarHeight, m.sidebarPct, m.sidebarHidden)
	m.sidebar.SetSize(sidebarW, contentH)
	m.mainPane.SetSize(mainW, contentH)
}

// RenderOnce synchronously loads data, applies the given terminal size,
// and returns the rendered view as a plain string. Useful for non-interactive
// inspection (e.g. CI, automated review loops).
func (m *Model) RenderOnce(width, height int) string {
	m.width = width
	m.height = height
	m.updateLayout()

	// Synchronously load data and apply it via Update
	var msg tea.Msg
	if m.git != nil {
		msg = m.loadGitData()
	} else {
		msg = m.loadNonGitFiles()
	}
	m.Update(msg)

	v := m.View()
	return v.Content
}

// RenderWithKeys loads data, applies a sequence of key events, and returns the
// rendered output. Used by PRWATCH_KEYS for non-interactive exploration.
//
// The keys string is a comma-separated list of key names. Special keys:
// tab, enter, esc, space, up, down, left, right, pgup, pgdn, backspace.
// Shift modifier: shift+j, shift+n, etc. Single characters: j, k, v, d, etc.
func (m *Model) RenderWithKeys(width, height int, keys string) string {
	m.width = width
	m.height = height
	m.updateLayout()

	// Synchronously load data
	var msg tea.Msg
	if m.git != nil {
		msg = m.loadGitData()
	} else {
		msg = m.loadNonGitFiles()
	}
	result, cmd := m.Update(msg)
	m = result.(*Model)
	// Execute safe follow-up commands (like PR load, all-files load)
	m.execFollowUps(cmd)

	// Apply each key
	for _, k := range strings.Split(keys, ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		keyMsg := parseKeyName(k)
		result, cmd := m.Update(keyMsg)
		m = result.(*Model)
		m.execFollowUps(cmd)
	}

	v := m.View()
	return v.Content
}

// execFollowUps executes safe follow-up commands from Update, recursively
// processing batched commands. Used by RenderWithKeys to resolve async loads.
func (m *Model) execFollowUps(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	func() {
		defer func() { recover() }()
		msg := cmd()
		if msg == nil {
			return
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				if sub != nil {
					m.execFollowUps(sub)
				}
			}
		case gitDataMsg, prRefreshMsg, allFilesMsg, moreCommitsMsg, ignoredDirLoadedMsg:
			result, cmd2 := m.Update(msg)
			*m = *(result.(*Model))
			m.execFollowUps(cmd2)
		}
	}()
}

// parseKeyName converts a key name string to a tea.KeyPressMsg.
func parseKeyName(name string) tea.KeyPressMsg {
	// Handle shift+X
	if strings.HasPrefix(name, "shift+") {
		ch := name[6:]
		if len(ch) == 1 {
			upper := strings.ToUpper(ch)
			return tea.KeyPressMsg{Text: upper, Code: rune(upper[0]), Mod: tea.ModShift}
		}
		// shift+up, shift+down, shift+space, etc.
		base := parseKeyName(ch)
		base.Mod |= tea.ModShift
		return base
	}

	switch name {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Text: " ", Code: ' '}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdn":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "?":
		return tea.KeyPressMsg{Text: "?", Code: '?'}
	case "/":
		return tea.KeyPressMsg{Text: "/", Code: '/'}
	case "+":
		return tea.KeyPressMsg{Text: "+", Code: '+'}
	case "-":
		return tea.KeyPressMsg{Text: "-", Code: '-'}
	default:
		if len(name) == 1 {
			ch := rune(name[0])
			return tea.KeyPressMsg{Text: name, Code: ch}
		}
		return tea.KeyPressMsg{Text: name}
	}
}

// scopeHandleInfo returns a snapshot of the scope handle for the status bar,
// or nil at the default scope position. m.commitCount already counts commits
// in m.base..HEAD, which equals N for HEAD~N.
func (m *Model) scopeHandleInfo() *scopeHandleInfo {
	return scopeHandleFromBase(m.base, m.naturalBase, m.commitCount)
}

func (m *Model) View() tea.View {
	if m.debugLog != nil {
		m.debugLog.Printf("[render] mode=%d focus=%d items=%d selected=%d offset=%d",
			m.mode, m.focus, len(m.sidebar.items), m.sidebar.selected, m.sidebar.offset)
	}

	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion

	if m.err != nil {
		v.SetContent(fmt.Sprintf("Error: %v\nPress q to quit.\n", m.err))
		return v
	}

	if m.loading {
		v.SetContent(padToHeight("loading...", m.width, m.height))
		return v
	}

	bar, labels, l2Labels, l3Labels := renderStatusBar(m.width, statusBarData{
		info:             m.repoInfo,
		pr:               m.prInfo,
		ciStatus:         m.ciStatus,
		reviews:          m.prReviews,
		reviewRequests:   m.prReviewRequests,
		prError:          m.prError,
		commentCount:     m.prCommentCount,
		mode:             m.mode,
		confirming:       m.confirming,
		uncommitCount:    len(m.uncommittedFiles) + len(m.stagedFiles),
		commitCount:      m.commitCount,
		behindCount:      m.behindCount,
		changedFileCount: len(m.committedFiles) + len(m.uncommittedFiles) + len(m.stagedFiles),
		prLoading:        m.loading && m.git != nil,
		showHelp:         m.showHelp,
		hoverX:           m.hoverX,
		hoverY:           m.hoverY,
		scopeHandle:      m.scopeHandleInfo(),
	})
	m.modeLabels = labels
	m.line2Labels = l2Labels
	m.line3Labels = l3Labels

	var result string
	if m.showHelp {
		result = bar + "\n" + m.renderHelp()
	} else if m.sidebarHidden {
		mainView := m.mainPane.View(m.focus == MainFocus)
		result = bar + "\n" + mainView
	} else {
		sidebarView := m.sidebar.View(m.focus == SidebarFocus)
		mainView := m.mainPane.View(m.focus == MainFocus)
		content := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, mainView)
		result = bar + "\n" + content
	}

	padded := padToHeight(result, m.width, m.height)

	// Replace the last line with the search bar when searching or in nav mode
	if m.searching || m.searchConfirmed {
		var searchBar string
		if m.searching {
			searchBar = fmt.Sprintf("/%s_", m.searchQuery)
		} else {
			searchBar = fmt.Sprintf("/%s", m.searchQuery)
		}
		if len(m.searchMatches) > 0 {
			searchBar += fmt.Sprintf("  %d/%d", m.searchMatchIdx+1, len(m.searchMatches))
		} else if m.searchQuery != "" {
			searchBar += "  0/0"
		}
		lines := strings.Split(padded, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = searchBar
			padded = strings.Join(lines, "\n")
			padded = padToHeight(padded, m.width, m.height)
		}
	}

	// Show notification on the last line (unless search bar is active)
	if m.notification != "" && !m.searching && !m.searchConfirmed {
		lines := strings.Split(padded, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = sidebarDimStyle.Render(m.notification)
			padded = strings.Join(lines, "\n")
			padded = padToHeight(padded, m.width, m.height)
		}
	}

	// Apply drag selection highlighting
	if m.dragging && (m.dragStartX != m.dragEndX || m.dragStartY != m.dragEndY) {
		padded = m.applyDragHighlight(padded)
	}

	v.SetContent(padded)
	return v
}

// applyDragHighlight applies reverse-video highlighting to the drag-selected region.
// Constrains highlighting to the main pane area only.
func (m *Model) applyDragHighlight(content string) string {
	startY, endY := m.dragStartY, m.dragEndY
	startX, endX := m.dragStartX, m.dragEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	// Clamp to main pane area, excluding the gutter
	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	// Content starts after sidebar + main pane border + sticky title row.
	// The title row sits at statusRows + topBorder; the user's selection
	// must skip it so dragging onto / past the title doesn't reverse-video
	// the title text (it is not part of the file content being read).
	statusRows := m.statusBarLines()
	topBorder := 1
	titleRow := 1
	contentStartY := statusRows + topBorder + titleRow
	gutterOffset := sidebarW + 1 + m.mainPane.gutterWidth // +1 for border
	if startX < gutterOffset {
		startX = gutterOffset
	}
	if endX >= m.width {
		endX = m.width - 1
	}
	// Clamp Y range to the main pane content area (between borders)
	contentEndY := m.height - 2 // last row before bottom border
	if startY < contentStartY {
		startY = contentStartY
		startX = gutterOffset
	}
	if endY > contentEndY {
		endY = contentEndY
	}

	lines := strings.Split(content, "\n")

	// Right border of the main pane is the last column before the edge.
	rightBorderCol := m.width - 1

	for y := startY; y <= endY && y < len(lines); y++ {
		fromCol := gutterOffset
		// Clamp the highlight to the actual content on this line,
		// excluding trailing padding spaces inside the main pane border.
		// Extract just the main pane content area (between gutter and
		// right border), strip ANSI, trim trailing spaces, and use the
		// resulting width to compute the content end column.
		stripped := stripANSIForWidth(lines[y])
		mainContent := sliceByDisplayCol(stripped, gutterOffset, rightBorderCol)
		trimmed := strings.TrimRight(mainContent, " ")
		contentEndCol := gutterOffset + displayWidthOf(trimmed)
		toCol := contentEndCol
		if y == startY {
			fromCol = startX
		}
		if y == endY {
			toCol = min(endX+1, contentEndCol)
		}
		if fromCol >= toCol {
			continue
		}

		// Split the original line (preserving ANSI codes) at the
		// highlight column boundaries, so that styling outside the
		// selection is not disturbed.
		before, middle, after := splitAtDisplayCols(lines[y], fromCol, toCol)
		selected := stripANSIForWidth(middle)
		if selected == "" {
			continue
		}
		// Use raw ANSI escapes: \x1b[7m enables reverse-video,
		// \x1b[27m disables only reverse-video (preserving other styles).
		lines[y] = before + "\x1b[7m" + selected + "\x1b[27m" + after
	}
	return strings.Join(lines, "\n")
}

func (m *Model) dragMainPaneBounds() (top, bottom int) {
	return mainPaneContentRows(m.statusBarLines(), m.height)
}

// updateDragAutoScroll inspects the current drag-end Y in screen coords and
// sets m.dragScrollDir accordingly: -1 if the user has dragged above the
// viewport top, +1 if below the bottom, 0 if back inside. Returns a tick
// command when scrolling needs to (re)start, nil otherwise.
//
// Spec: "when dragging past the top line or past the bottom line, the view
// should scroll, making it possible to copy content larger than the view on
// the screen." The actual scrolling happens in advanceDragAutoScroll on
// each tick — this only sets the direction.
func (m *Model) updateDragAutoScroll(y int) tea.Cmd {
	top, bottom := m.dragMainPaneBounds()
	prev := m.dragScrollDir
	switch {
	case y < top:
		m.dragScrollDir = -1
	case y > bottom:
		m.dragScrollDir = +1
	default:
		m.dragScrollDir = 0
	}
	if m.dragScrollDir != 0 && prev == 0 {
		return scheduleDragScrollTick()
	}
	return nil
}

// advanceDragAutoScroll scrolls the main pane viewport one line in the
// current drag direction and re-anchors the drag start so the original
// click stays attached to the same content row (eventually moving off-
// screen above when scrolling down). Returns the next tick command unless
// the viewport has hit the corresponding edge — at which point we stop.
func (m *Model) advanceDragAutoScroll() tea.Cmd {
	if m.dragScrollDir == 0 {
		return nil
	}
	beforeOffset := m.mainPane.viewport.YOffset()
	if m.dragScrollDir > 0 {
		m.mainPane.viewport.ScrollDown(1)
	} else {
		m.mainPane.viewport.ScrollUp(1)
	}
	delta := m.mainPane.viewport.YOffset() - beforeOffset
	if delta == 0 {
		// Hit top or bottom; nothing more to scroll. Don't schedule another
		// tick — the loop is over. The user can still drag back inside.
		m.dragScrollDir = 0
		return nil
	}
	// Re-anchor: the original click should keep pointing at the same content
	// row even as the viewport scrolls underneath it. Scrolling down by N
	// shifts content up by N screen rows, so dragStartY moves up too.
	m.dragStartY -= delta
	return scheduleDragScrollTick()
}

func scheduleDragScrollTick() tea.Cmd {
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg {
		return dragScrollTickMsg{}
	})
}

// copySelection extracts text from the main pane's content (stripping ANSI,
// gutter, and TUI glyphs) and copies to the system clipboard.
// Coordinates are screen-relative; we convert to main-pane-content-relative.
// selectedText extracts the plain text from the current drag selection,
// stripping ANSI codes, gutter prefixes, and joining word-wrap continuations.
// Returns empty string if the drag start and end are the same point.
//
// The selection works against the viewport's full GetContent() (not just
// the visible View()). That matters when auto-scroll has moved the
// viewport during the drag: the original click line may now be off-screen
// above, but the user still expects it included in the copy.
func (m *Model) selectedText() string {
	if m.dragStartX == m.dragEndX && m.dragStartY == m.dragEndY {
		return "" // No actual drag
	}

	// Main pane content area starts after:
	// - status bar rows
	// - 1 row of top border
	// - 1 row of sticky title bar
	// And the x offset is sidebarPixelWidth() + 1 (left border of main pane).
	statusRows := m.statusBarLines()
	topBorder := 1
	titleRow := 1
	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	mainLeftBorder := 1
	contentStartY := statusRows + topBorder + titleRow
	contentStartX := sidebarW + mainLeftBorder

	// Read the full viewport content (all lines, not just visible) so we
	// can extract selections that started before the viewport scrolled.
	viewportContent := m.mainPane.viewport.GetContent()
	contentLines := strings.Split(viewportContent, "\n")

	// Normalize drag coordinates
	startY, endY := m.dragStartY, m.dragEndY
	startX, endX := m.dragStartX, m.dragEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	// Clamp the drag Y range to the visible main-pane content rows, matching
	// applyDragHighlight. Without this, a drag onto the bottom border row
	// would translate to an absolute viewport line that exists in GetContent
	// but is not actually rendered, causing selectedText to disagree with
	// the on-screen highlight. When endY is dragged past the last content
	// row, the user's intent is "select to end of last visible line" — so
	// also disable the x-clamp on the new last row by pushing endX past the
	// right edge of the pane.
	contentEndY := m.height - 2
	if endY > contentEndY {
		endY = contentEndY
		endX = m.width
	}
	if startY > contentEndY {
		return ""
	}

	// Translate from screen-Y to absolute viewport-content-Y by accounting
	// for the current scroll offset. dragStartY may sit above the visible
	// area (after auto-scroll re-anchored it); the absolute line index it
	// resolves to remains valid.
	vpOffset := m.mainPane.viewport.YOffset()
	startY = vpOffset + (startY - contentStartY)
	endY = vpOffset + (endY - contentStartY)
	startX -= contentStartX
	endX -= contentStartX

	if startY < 0 {
		startY = 0
		startX = 0
	}
	if startX < 0 {
		startX = 0
	}
	if endX < 0 {
		endX = 0
	}

	gw := m.mainPane.gutterWidth
	contMap := m.mainPane.wrapContinuation
	var selected strings.Builder
	for y := startY; y <= endY && y < len(contentLines); y++ {
		// Strip ANSI codes to get clean text
		line := stripANSIForWidth(contentLines[y])
		line = strings.TrimRight(line, " ") // remove trailing padding

		// For continuation lines (word-wrapped), strip the indent prefix
		// For original lines, strip the gutter prefix. The continuation
		// map is keyed on absolute viewport-content-Y (matches y here).
		isCont := contMap != nil && y < len(contMap) && contMap[y]
		if isCont {
			// Continuation line: strip indent (gutter-width spaces)
			if gw > 0 && len(line) > gw {
				line = line[gw:]
			}
		} else if gw > 0 && len(line) > gw {
			line = line[gw:]
		}

		lineWidth := displayWidthOf(line)
		fromCol := 0
		toCol := lineWidth
		if y == startY {
			// Adjust startX for gutter/indent removal
			fromCol = max(0, startX-gw)
		}
		if y == endY {
			toCol = max(0, endX+1-gw)
		}
		if fromCol > lineWidth {
			fromCol = lineWidth
		}
		if toCol > lineWidth {
			toCol = lineWidth
		}
		if fromCol < toCol {
			selected.WriteString(sliceByDisplayCol(line, fromCol, toCol))
		}
		if y < endY {
			// If the NEXT viewport line is a word-wrap continuation, don't add newline
			nextAbsY := y + 1
			if contMap != nil && nextAbsY < len(contMap) && contMap[nextAbsY] {
				continue // join continuation lines without newline
			}
			selected.WriteString("\n")
		}
	}

	return selected.String()
}

// yankPath copies the current file path to the clipboard.
// Sidebar focused: copies the relative path of the selected file.
// Main pane focused: copies path:startLine-endLine for the visible range.
func (m *Model) yankPath() tea.Cmd {
	file := m.sidebar.SelectedItem()
	if file == "" || m.sidebar.SelectedIsDir() {
		return nil
	}
	var text string
	if m.focus == SidebarFocus {
		text = file
	} else {
		topLine := m.mainPane.ViewportToSourceLine()
		bottomLine := m.mainPane.ViewportBottomSourceLine()
		if topLine == bottomLine {
			text = fmt.Sprintf("%s:%d", file, topLine)
		} else {
			text = fmt.Sprintf("%s:%d-%d", file, topLine, bottomLine)
		}
	}
	m.copyToClipboard(text)
	m.notification = "copied " + text
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return notificationExpiredMsg{}
	})
}

func (m *Model) copySelection() tea.Cmd {
	text := m.selectedText()
	if text == "" {
		return nil
	}
	m.copyToClipboard(text)
	lines := strings.Count(text, "\n") + 1
	lineWord := "lines"
	if lines == 1 {
		lineWord = "line"
	}
	bytes := len(text)
	byteWord := "bytes"
	if bytes == 1 {
		byteWord = "byte"
	}
	m.notification = fmt.Sprintf("copied selection (%d %s, %d %s)", lines, lineWord, bytes, byteWord)
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return notificationExpiredMsg{}
	})
}

// copyToClipboard copies the given text to the system clipboard.
func (m *Model) copyToClipboard(text string) {
	var cmd command.Command
	switch runtime.GOOS {
	case "darwin":
		cmd = m.cmdFactory("pbcopy")
	case "linux":
		cmd = m.cmdFactory("xclip", "-selection", "clipboard")
	default:
		return
	}
	cmd.SetStdin(strings.NewReader(text))
	cmd.Run() //nolint: ignore clipboard errors
}

func (m *Model) renderPRDescription() string {
	return renderPRDescription(m.prInfo, m.prReviews, m.prDeployments, m.mainPane.width)
}

// helpEntry pairs one or more key bindings with a description line.
type helpEntry struct {
	bindings []key.Binding
	desc     string
}

// keyList formats a set of bindings as "[key1] [key2] ..." so the help overlay
// always reflects the actual keymap in keys.go rather than hard-coded strings.
func keyList(bs ...key.Binding) string {
	var parts []string
	for _, b := range bs {
		for _, k := range b.Keys() {
			parts = append(parts, "["+k+"]")
		}
	}
	return strings.Join(parts, " ")
}

func (m *Model) helpContentLines() []string {
	sections := [][]helpEntry{
		{
			{bindings: []key.Binding{keys.ToggleMode}, desc: "Cycle mode (files → commits → pr)"},
			{bindings: []key.Binding{keys.FilesMode}, desc: "Files mode"},
			{bindings: []key.Binding{keys.CommitsMode}, desc: "Commits mode"},
			{bindings: []key.Binding{keys.PRMode}, desc: "PR mode (when PR exists)"},
		},
		{
			{bindings: []key.Binding{keys.FocusLeft}, desc: "Scroll left (when wrap off)"},
			{bindings: []key.Binding{keys.FocusRight}, desc: "Scroll right (when wrap off)"},
			{bindings: []key.Binding{keys.FocusSidebar}, desc: "Focus sidebar"},
			{bindings: []key.Binding{keys.FocusMain}, desc: "Focus main pane"},
			{bindings: []key.Binding{keys.FocusToggle}, desc: "Toggle focus (sidebar / main pane)"},
		},
		{
			{bindings: []key.Binding{keys.Down}, desc: "Move down / scroll down"},
			{bindings: []key.Binding{keys.Up}, desc: "Move up / scroll up"},
			{bindings: []key.Binding{keys.PageDown}, desc: "Page down"},
			{bindings: []key.Binding{keys.PageUp}, desc: "Page up"},
			{bindings: []key.Binding{keys.GoTop}, desc: "Go to top"},
			{bindings: []key.Binding{keys.GoBottom}, desc: "Go to bottom"},
		},
		{
			{bindings: []key.Binding{keys.SidebarGrow}, desc: "Grow sidebar"},
			{bindings: []key.Binding{keys.SidebarShrink}, desc: "Shrink sidebar"},
			{bindings: []key.Binding{keys.ToggleSidebar}, desc: "Toggle sidebar visibility"},
		},
		{
			{bindings: []key.Binding{keys.ToggleWrap}, desc: "Toggle word wrap"},
			{bindings: []key.Binding{keys.ToggleLineNums}, desc: "Toggle line numbers (files mode)"},
			{bindings: []key.Binding{keys.ToggleIgnored}, desc: "Toggle gitignored files (files mode)"},
			{bindings: []key.Binding{keys.ToggleRemoved}, desc: "Toggle removed lines in diff gutter (files mode)"},
		},
		{
			{bindings: []key.Binding{keys.NextDiff}, desc: "Jump to next diff hunk (files mode)"},
			{bindings: []key.Binding{keys.PrevDiff}, desc: "Jump to previous diff hunk (files mode)"},
			{bindings: []key.Binding{keys.NextLeaf}, desc: "Jump to next leaf"},
			{bindings: []key.Binding{keys.PrevLeaf}, desc: "Jump to previous leaf"},
		},
		{
			{bindings: []key.Binding{keys.Enter}, desc: "Open file in $EDITOR / switch to main pane"},
			{bindings: []key.Binding{keys.YankPath}, desc: "Copy file path (sidebar) or path:lines (main pane)"},
			{bindings: []key.Binding{keys.Search}, desc: "Search (type to match, enter to confirm)"},
			{bindings: []key.Binding{keys.SearchNext}, desc: "Next search result (after search confirmed)"},
			{bindings: []key.Binding{keys.SearchPrev}, desc: "Previous search result (after search confirmed)"},
		},
		{
			{bindings: []key.Binding{keys.Refresh}, desc: "Refresh git state"},
			{bindings: []key.Binding{keys.PRBrowse}, desc: "Open the active PR in the browser"},
			{bindings: []key.Binding{keys.Help}, desc: "Show this help (scroll with j/k/mouse)"},
		},
		{
			{bindings: []key.Binding{keys.ScopeExtendBack}, desc: "Extend commit-range scope backward"},
			{bindings: []key.Binding{keys.ScopeContractForward}, desc: "Contract commit-range scope toward working tree"},
			{bindings: []key.Binding{keys.ScopeReset}, desc: "Reset commit-range scope to default"},
		},
		{
			{bindings: []key.Binding{keys.QuitConfirm}, desc: "Quit (confirm)"},
			{bindings: []key.Binding{keys.QuitImmediate}, desc: "Quit immediately"},
		},
	}

	// Column-align descriptions: pad the key list to the widest one.
	width := 0
	for _, section := range sections {
		for _, e := range section {
			if w := len(keyList(e.bindings...)); w > width {
				width = w
			}
		}
	}

	lines := []string{"Keybindings:", ""}
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		for _, e := range section {
			lines = append(lines, fmt.Sprintf("  %-*s  %s", width, keyList(e.bindings...), e.desc))
		}
	}
	lines = append(lines, "", "Press q/esc to dismiss. Use j/k or mouse to scroll. / to search.")
	return lines
}

func (m *Model) renderHelp() string {
	lines := m.helpContentLines()
	visibleHeight := max(1, m.height-m.statusBarLines()-2) // status bar + borders

	// Apply search highlighting
	if m.helpSearchQuery != "" {
		for i, line := range lines {
			lines[i] = highlightMatchInLine(line, m.helpSearchQuery)
		}
	}

	// Apply scroll offset
	end := m.helpScrollOffset + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}
	start := m.helpScrollOffset
	if start > len(lines) {
		start = len(lines)
	}
	visible := lines[start:end]

	result := strings.Join(visible, "\n")

	// Add search bar at bottom if searching or in nav mode
	if m.helpSearching {
		searchBar := "/" + m.helpSearchQuery + "_"
		if len(m.helpSearchMatches) > 0 {
			searchBar += fmt.Sprintf("  %d/%d", m.helpSearchIdx+1, len(m.helpSearchMatches))
		} else if m.helpSearchQuery != "" {
			searchBar += "  0/0"
		}
		result += "\n" + searchBar
	} else if m.helpSearchConfirmed {
		searchBar := fmt.Sprintf("/%s  %d/%d", m.helpSearchQuery, m.helpSearchIdx+1, len(m.helpSearchMatches))
		result += "\n" + searchBar
	}

	return result
}
