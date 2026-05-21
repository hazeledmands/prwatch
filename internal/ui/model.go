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
	// scope describes the commit range currently in view: (oldBase, newBase].
	// It owns the scope-extend / scope-contract / scope-reset state and feeds
	// the in-scope commit/file queries below. See scope.go.
	scope              *scope
	repoInfo           gitpkg.RepoInfoResult
	prInfo             gitpkg.PRInfoResult
	ciStatus           gitpkg.CIStatusResult
	prReviews          []gitpkg.PRReview
	prReviewRequests   []gitpkg.PRReviewRequest
	prError            string // error message for PR/GitHub API issues
	prCommentCount     int
	changes            *gitpkg.ChangedFiles // unified per-file view of base..HEAD + index + working tree
	allFiles           []string             // all files in the repo (for files mode)
	ignoredFiles       map[string]bool      // gitignored files (for dimming in all-files view)
	ignoredDirs        map[string]bool      // ignored entries that are directories — render as expandable
	loadedIgnoredDirs  map[string]bool      // ignored dirs whose contents have been lazy-loaded
	commits            []gitpkg.Commit
	commitsLoaded      int                   // how many commits have been loaded so far
	behindCount        int                   // how many commits behind base
	baseCommits        []gitpkg.Commit       // commits from the base branch (for commit mode category 4)
	prComments         []gitpkg.PRComment    // PR comments for PR-view mode
	prDeployments      []gitpkg.PRDeployment // PR deployments for PR-view mode
	ciChecks           []gitpkg.CICheck      // CI checks for PR-view mode
	rwxFetcher         *rwxFetcher           // RWX log fetch/cache state
	viewMemory         *viewMemory           // per-mode sidebar + per-item main-pane scroll
	lastMainItem       mainItemKey           // (mode, item) currently displayed in main pane
	sidebar            *sidebar
	mainPane           *mainPane
	sidebarPct         int // sidebar width as percentage of total width (10-50)
	dir                string
	confirming         bool
	help               *helpOverlay     // help overlay subsystem
	showIgnored        bool             // whether to show gitignored files in all-files section
	collapsedDirs      map[string]bool  // tracks collapsed directory paths
	sidebarHidden      bool             // [f] toggles sidebar visibility
	wordWrap           bool             // [w] toggles word wrapping in main pane
	lineNumbers        bool             // [n] toggles line numbers in files mode
	search             *searchOverlay   // cross-pane search overlay
	hoverX, hoverY     int              // last mouse position for hover highlighting
	activity           *activityTracker // adaptive refresh-interval bookkeeping
	drag               *dragSelection   // click-drag-release selection state
	notification       string           // transient notification text (bottom-left)
	notificationExpiry time.Time        // when the notification should disappear
	loading            bool             // true until first local data load completes
	prLoadedOnce       bool             // true after first successful PR data fetch
	modeLabels         []modeLabel      // clickable mode label positions from last render
	line2Labels        []line2Label     // clickable positions on git status line
	line3Labels        []line3Label     // clickable positions on PR status line
	err                error
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
	repoInfo       gitpkg.RepoInfoResult
	prInfo         gitpkg.PRInfoResult
	ciStatus       gitpkg.CIStatusResult
	prReviews      []gitpkg.PRReview
	prCommentCount int
	// queryOldBase is the scope.OldBase() the load actually queried against.
	// Used by the stale-load guard: if the user scrubbed between dispatch and
	// return, this won't match the model's current scope and the scope-dependent
	// fields (commits, baseCommits, changed files) get discarded.
	queryOldBase string
	// natural{Old,New}Base / natural{Old,New}Offset describe the freshly-detected
	// natural endpoints at load time. Passed to scope.SyncFromLoad, which either
	// adopts them (when not scrubbed) or only updates the natural fields (when
	// scrubbed, preserving the user's scrub).
	naturalOldBase   string
	naturalNewBase   string
	naturalOldOffset int
	naturalNewOffset int
	changes          *gitpkg.ChangedFiles
	allFiles         []string
	ignoredFiles     map[string]bool
	ignoredDirs      map[string]bool // subset of ignoredFiles whose entries are directories
	commits          []gitpkg.Commit
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
		scope:         &scope{},
		sidebar:       newSidebar(),
		mainPane:      newMainPane(),
		sidebarPct:    30, // default 30% of width
		showIgnored:   true,
		collapsedDirs: make(map[string]bool),
		rwxFetcher:    newRWXFetcher(),
		viewMemory:    newViewMemory(),
		wordWrap:      true,
		lineNumbers:   true,
		activity:      newActivityTracker(time.Now()),
		help:          newHelpOverlay(),
		search:        newSearchOverlay(),
		drag:          newDragSelection(),
		loading:       g != nil,
		changes:       gitpkg.NewChangedFiles(),
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
	changes := gitpkg.NewChangedFiles()
	for _, p := range files {
		changes.Add(gitpkg.ChangedFile{Path: p, Section: gitpkg.SectionUncommitted, Class: gitpkg.ClassAdded})
	}
	return gitDataMsg{
		changes: changes,
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
	return fileContextRight(file, binary, m.isRenamedFile(file), m.statMtime, m.lastCommitForFile)
}

// renameOldPath returns the pre-rename path for file if it's the new side of
// a known rename, plus true. When the file isn't a rename target, returns
// ("", false).
func (m *Model) renameOldPath(file string) (string, bool) {
	if f, ok := m.changes.Get(file); ok && f.Class == gitpkg.ClassRenamed {
		return f.OldPath, true
	}
	return "", false
}

// isRenamedFile reports whether file is the new side of a rename.
func (m *Model) isRenamedFile(file string) bool {
	_, ok := m.renameOldPath(file)
	return ok
}

// isPureRename reports whether file is a rename target with no content
// changes (git similarity = 100). The title bar uses this to choose between
// the "renamed · ..." no-diff right side and the regular hunk-position one.
func (m *Model) isPureRename(file string) bool {
	f, ok := m.changes.Get(file)
	return ok && f.Class == gitpkg.ClassRenamed && f.PureRename
}

// fileTitleLeft returns the title-bar's left side for file. For renamed files
// it returns "<old> → <new>"; otherwise just the file path.
func (m *Model) fileTitleLeft(file string) string {
	if old, ok := m.renameOldPath(file); ok {
		return old + " → " + file
	}
	return file
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
		changes := gitpkg.NewChangedFiles()
		for _, p := range allFiles {
			changes.Add(gitpkg.ChangedFile{Path: p, Section: gitpkg.SectionUncommitted, Class: gitpkg.ClassAdded})
		}
		return gitDataMsg{
			repoInfo: info,
			allFiles: allFiles,
			changes:  changes,
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

	// Detect the natural scope base. Prefer the PR-reported base when
	// available; fall back to local detection.
	naturalOldBase, berr := detectNaturalBase(m.git, prInfo.BaseRef)
	if berr != nil {
		return gitDataMsg{err: berr}
	}

	// Pick the base used for queries: if the user has scrubbed the scope
	// handle, that's the scrubbed outer endpoint; otherwise the natural one.
	queryOldBase := naturalOldBase
	if m.scope.IsScrubbed() && m.scope.OldBase() != "" {
		queryOldBase = m.scope.OldBase()
	}

	files, err := m.git.ChangedFiles(queryOldBase)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// In-scope commits + count are always range-relative now. On main / detached
	// HEAD the range is empty (queryOldBase == HEAD), so commits = []; the full
	// repo history is rendered below the cutline via BaseCommits.
	pageSize := max(commitPageSize, m.commitsLoaded)
	commits, err := m.git.Commits(queryOldBase, 0, pageSize)
	if err != nil {
		return gitDataMsg{err: err}
	}
	naturalOldOffset, _ := m.git.CommitCountRange(naturalOldBase)

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

	// Below-cutline commits (out-of-scope context). On non-detached-HEAD this
	// is what the commits-mode "Base" section renders.
	var baseCommits []gitpkg.Commit
	if !info.IsDetachedHead {
		baseCommits, _ = m.git.BaseCommits(queryOldBase, 50)
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
		queryOldBase:     queryOldBase,
		naturalOldBase:   naturalOldBase,
		naturalOldOffset: naturalOldOffset,
		changes:          files.ToChangedFiles(),
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
		ignoredDirs:      ignoredDirSet,
		commits:          commits,
		baseCommits:      baseCommits,
		behindCount:      behindCount,
		prComments:       prAll.Comments,
		prDeployments:    prAll.Deployments,
		ciChecks:         ciChecks,
		reviewRequests:   prAll.ReviewRequests,
		prFetchFailed:    prFetchFailed,
	}
}

// detectNaturalBase resolves the natural outer endpoint of the scope range
// for the current branch. Prefers a PR-reported base when available; falls
// back to local merge-base detection (which on main/detached HEAD returns
// HEAD itself, yielding an empty natural scope per Reading B).
func detectNaturalBase(g GitDataSource, prBaseRef string) (string, error) {
	if prBaseRef != "" {
		if sha, err := g.DetectBaseFromPR(prBaseRef); err == nil {
			return sha, nil
		}
	}
	return g.DetectBaseLocal()
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
		changes := gitpkg.NewChangedFiles()
		for _, p := range allFiles {
			changes.Add(gitpkg.ChangedFile{Path: p, Section: gitpkg.SectionUncommitted, Class: gitpkg.ClassAdded})
		}
		return gitDataMsg{
			repoInfo:  info,
			allFiles:  allFiles,
			changes:   changes,
			localOnly: true,
		}
	}

	// Detect the natural scope base. Prefer the PR-reported base if PR data
	// has loaded; fall back to local detection. When PR data arrives later,
	// prRefreshMsg re-dispatches loadLocalGitData so the natural base upgrades
	// to match the PR's baseRefName.
	naturalOldBase, err := m.detectBase()
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Pick the base used for queries: scrubbed outer endpoint when scrubbed,
	// natural base otherwise.
	queryOldBase := naturalOldBase
	if m.scope.IsScrubbed() && m.scope.OldBase() != "" {
		queryOldBase = m.scope.OldBase()
	}

	files, err := m.git.ChangedFiles(queryOldBase)
	if err != nil {
		return gitDataMsg{err: err}
	}

	pageSize := max(commitPageSize, m.commitsLoaded)
	commits, err := m.git.Commits(queryOldBase, 0, pageSize)
	if err != nil {
		return gitDataMsg{err: err}
	}
	naturalOldOffset, _ := m.git.CommitCountRange(naturalOldBase)

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
	if !info.IsDetachedHead {
		baseCommits, _ = m.git.BaseCommits(queryOldBase, 50)
	}

	allFiles, _ := m.git.AllFiles()
	ignoredSet, ignoredDirSet := loadIgnoredSet(m.git)

	return gitDataMsg{
		repoInfo:         info,
		queryOldBase:     queryOldBase,
		naturalOldBase:   naturalOldBase,
		naturalOldOffset: naturalOldOffset,
		changes:          files.ToChangedFiles(),
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
		ignoredDirs:      ignoredDirSet,
		commits:          commits,
		baseCommits:      baseCommits,
		behindCount:      behindCount,
		localOnly:        true, // preserve existing PR data
	}
}

func (m *Model) loadMoreCommits() tea.Msg {
	skip := m.commitsLoaded
	commits, err := m.git.Commits(m.scope.OldBase(), skip, commitPageSize)
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
			changedCount := 0
			if msg.changes != nil {
				changedCount = msg.changes.Len()
			}
			m.debugLog.Printf("[data] gitDataMsg localOnly=%v changed=%d allFiles=%d",
				msg.localOnly, changedCount, len(msg.allFiles))
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
		// Stale-load guard. If a periodic tick's load was already in flight
		// when the user scrubbed the scope handle, msg.queryOldBase reflects
		// the pre-scrub state and msg.commits / committedFiles / baseCommits
		// describe the wrong slice of history. Sync the natural endpoints so
		// scope-reset still snaps correctly, then discard the rest. The next
		// load (re-dispatched by the scope command) is authoritative.
		currentOld := m.scope.OldBase()
		if currentOld != "" && msg.queryOldBase != "" && msg.queryOldBase != currentOld {
			m.scope.SyncFromLoad(msg.naturalOldBase, msg.naturalNewBase, msg.naturalOldOffset, msg.naturalNewOffset)
			m.updateLayout()
			m.updateSidebarItems()
			m.updateMainContent()
			return m, nil
		}

		m.scope.SyncFromLoad(msg.naturalOldBase, msg.naturalNewBase, msg.naturalOldOffset, msg.naturalNewOffset)
		m.changes = msg.changes
		if m.changes == nil {
			m.changes = gitpkg.NewChangedFiles()
		}
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
		if m.help.IsOpen() {
			visibleHeight := max(1, m.height-m.statusBarLines()-2)
			dir := 0
			if msg.Button == tea.MouseWheelUp {
				dir = -1
			} else if msg.Button == tea.MouseWheelDown {
				dir = +1
			}
			m.help.HandleWheel(dir, visibleHeight)
			return m, nil
		}
		return m.handleMouseWheel(msg)

	case tea.MouseMotionMsg:
		m.hoverX = msg.X
		m.hoverY = msg.Y
		var autoScrollCmd tea.Cmd
		if m.drag.IsActive() {
			g := m.dragGeom()
			m.drag.MoveEnd(g.clickAt(msg.X, msg.Y))
			autoScrollCmd = m.drag.UpdateAutoScroll(msg.Y, g)
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
		g := m.dragGeom()
		if m.drag.Release(g.clickAt(msg.X, msg.Y)) {
			return m, m.copySelection()
		}
		return m, nil

	case dragScrollTickMsg:
		if !m.drag.IsActive() || m.drag.ScrollDir() == 0 {
			return m, nil
		}
		return m, m.drag.AdvanceAutoScroll(m.dragGeom())
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Handle shift+space as page up (may not be caught by key.Matches)
	if msg.Code == tea.KeySpace && msg.Mod&tea.ModShift != 0 {
		if m.help.IsOpen() {
			visibleHeight := max(1, m.height-m.statusBarLines()-2)
			m.help.PageUp(visibleHeight)
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
	if m.search.IsSearching() {
		m.search.HandleInputKey(msg, m.mainPane)
		return m, nil
	}

	// Search confirmed mode (n/p navigation)
	if m.search.IsConfirmed() {
		if m.search.HandleNavKey(msg, m.mainPane) {
			return m, nil
		}
		return m.handleKey(msg)
	}

	// Help overlay — supports scrolling and search
	if m.help.IsOpen() {
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
		m.help.Open()
		return m, nil

	case key.Matches(msg, keys.Search):
		m.search.Open()
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
		if !m.scope.IsScrubbed() {
			return m, nil
		}
		m.scope.Reset()
		if m.git == nil {
			return m, nil
		}
		return m, m.loadLocalGitData

	case key.Matches(msg, keys.ScopeExtendBack):
		if m.git == nil {
			return m, nil
		}
		if err := m.scope.ExtendBack(m.git); err != nil {
			// At root commit, unloaded, or other failure — no-op.
			return m, nil
		}
		return m, m.loadLocalGitData

	case key.Matches(msg, keys.ScopeContractForward):
		if m.git == nil {
			return m, nil
		}
		if err := m.scope.ContractForward(m.git); err != nil {
			// FirstChildToward failed unexpectedly — no-op. (The empty-range
			// case is handled inside scope.ContractForward as a silent no-op.)
			return m, nil
		}
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

func (m *Model) handleHelpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	visibleHeight := max(1, m.height-m.statusBarLines()-2)
	return m, m.help.HandleKey(msg, visibleHeight)
}

// clearSearch is a thin wrapper preserved for the few non-handleKey call sites
// that already exist (mode switches, etc.). New code should call
// m.search.Clear directly.
func (m *Model) clearSearch() {
	m.search.Clear(m.mainPane)
}

func (m *Model) statusBarLines() int {
	return statusBarLineCount(statusBarData{
		info:      m.repoInfo,
		pr:        m.prInfo,
		prError:   m.prError,
		prLoading: (m.loading || !m.prLoadedOnce) && m.git != nil,
	})
}

func (m *Model) sidebarPixelWidth() int {
	// sidebar width + 2 for border
	return m.sidebar.width + 2
}

// dragGeom snapshots the screen-layout values dragSelection needs to map
// pixel coords onto rendered content. Built fresh at each call site so
// drag methods see the current layout.
func (m *Model) dragGeom() dragGeometry {
	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	return dragGeometry{
		statusRows: m.statusBarLines(),
		sidebarW:   sidebarW,
		screenW:    m.width,
		screenH:    m.height,
		pane:       m.mainPane,
	}
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
					if m.help.IsOpen() {
						m.help.Close()
					} else {
						m.help.Open()
					}
				} else {
					m.help.Close()
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
		m.drag.Cancel()
		return m.handleStatusBarClick(x, y)
	}

	// Adjust y for the 3-line status bar
	contentY := y - m.statusBarLines()
	sidebarW := m.sidebarPixelWidth()
	if !m.sidebarHidden && x < sidebarW {
		// Clicked in sidebar — no drag tracking
		m.drag.Cancel()
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
		g := m.dragGeom()
		m.drag.Begin(g.clickAt(x, y))
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
	f, ok := m.changes.Get(file)
	if !ok {
		return false
	}
	return f.Section == gitpkg.SectionUncommitted || f.Section == gitpkg.SectionStaged
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
	if line, ok := nextDiffLine(m.mainPane.DiffLineNumbers(), m.mainPane.visibleRange().Start.SourceLine, direction); ok {
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

func (m *Model) fileItemKind(file string, defaultKind sidebarItemKind) sidebarItemKind {
	if m.isDeletedFile(file) {
		return itemDeleted
	}
	return defaultKind
}

func (m *Model) changeBadge(file string) string {
	return changeBadgeFor(file, m.changes)
}

func (m *Model) applyChangeBadges(items []sidebarItem) []sidebarItem {
	return applyChangeBadges(items, m.changes)
}

func (m *Model) isCommittedFile(file string) bool {
	f, ok := m.changes.Get(file)
	return ok && f.Section == gitpkg.SectionCommitted
}

func (m *Model) isDeletedFile(file string) bool {
	f, ok := m.changes.Get(file)
	return ok && f.Class == gitpkg.ClassDeleted
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
		items := buildFilesSidebar(
			m.changes, m.allFiles,
			m.ignoredFiles, m.ignoredDirs, m.collapsedDirs,
			m.showIgnored, m.git != nil,
		)
		m.sidebar.SetItems(items)
	case CommitsMode:
		uncommittedPaths := pathsInSection(m.changes, gitpkg.SectionUncommitted)
		stagedPaths := pathsInSection(m.changes, gitpkg.SectionStaged)
		items := buildCommitsSidebar(
			m.commits, m.baseCommits,
			uncommittedPaths, stagedPaths,
			m.repoInfo.AheadCount, m.commitsLoaded, m.scope.Len(),
		)
		m.sidebar.SetItems(items)
	case PRMode:
		items := buildPRSidebar(m.prComments, m.prReviews, m.ciChecks)
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

func (m *Model) saveModeState() {
	m.viewMemory.SaveSidebar(m.mode, m.sidebar, m.focus)
}

func (m *Model) restoreModeState() {
	m.focus = m.viewMemory.RestoreSidebar(m.mode, m.sidebar, m.focus)
}

func (m *Model) updateMainContent() {
	// Save the source line at the top of the main pane under the item we
	// were just showing, so the next time the user navigates to it we can
	// drop them back at the same line.
	m.viewMemory.RememberMainScroll(m.lastMainItem, m.mainPane.visibleRange().Start.SourceLine)

	prevKey := m.lastMainItem
	// setItem records the (mode, item) currently displayed and, if it
	// differs from prevKey, restores the saved scroll for it (or applies a
	// per-mode default for first visits — currently jumpToFirstDiff for
	// files mode with a diff).
	setItem := func(key mainItemKey) {
		m.lastMainItem = key
		m.sidebar.SetPinnedID(key.item)
		if key == prevKey || key.item == "" {
			return
		}
		if line, ok := m.viewMemory.RecallMainScroll(key); ok {
			m.mainPane.ScrollToSourceLine(line)
			return
		}
		if key.mode == FilesMode && len(m.mainPane.DiffLineNumbers()) > 0 {
			m.jumpToFirstDiff()
		}
	}

	if m.git == nil {
		m.updateNonGitFilesMode(setItem)
		return
	}
	if m.scope.OldBase() == "" {
		return // preserve current main panel content (and lastMainItem)
	}

	switch m.mode {
	case FilesMode:
		m.updateFilesModeContent(setItem)
	case CommitsMode:
		m.updateCommitsModeContent(setItem)
	case PRMode:
		m.updatePRModeContent(setItem)
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
		uncommitCount:    m.changes.Len() - len(m.changes.InSection(gitpkg.SectionCommitted)),
		commitCount:      m.scope.Len(),
		behindCount:      m.behindCount,
		changedFileCount: m.changes.Len(),
		prLoading:        m.loading && m.git != nil,
		showHelp:         m.help.IsOpen(),
		hoverX:           m.hoverX,
		hoverY:           m.hoverY,
		scopeHandle:      m.scope.Handle(),
	})
	m.modeLabels = labels
	m.line2Labels = l2Labels
	m.line3Labels = l3Labels

	var result string
	if m.help.IsOpen() {
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
	if m.search.IsActive() {
		searchBar := m.search.RenderBar()
		lines := strings.Split(padded, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = searchBar
			padded = strings.Join(lines, "\n")
			padded = padToHeight(padded, m.width, m.height)
		}
	}

	// Show notification on the last line (unless search bar is active)
	if m.notification != "" && !m.search.IsActive() {
		lines := strings.Split(padded, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = sidebarDimStyle.Render(m.notification)
			padded = strings.Join(lines, "\n")
			padded = padToHeight(padded, m.width, m.height)
		}
	}

	// Apply drag selection highlighting
	if m.drag.IsActive() && m.drag.HasRange() {
		padded = m.drag.ApplyHighlight(padded, m.dragGeom())
	}

	v.SetContent(padded)
	return v
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

func (m *Model) renderHelp() string {
	return m.help.Render(max(1, m.height-m.statusBarLines()-2))
}
