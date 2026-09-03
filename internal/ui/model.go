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
	// StagedDiff and NewChangesDiff back the two commits-mode pseudo-entries.
	// They are deliberately separate calls: sharing one diff between them is
	// the bug they exist to prevent.
	StagedDiff() (string, error)
	NewChangesDiff() (string, error)
	FileContent(file string) (string, error)
	LastCommitForFile(file string) (gitpkg.Commit, error)
	CommitPatch(sha string) (string, error)
	AllFiles() ([]string, error)
	IgnoredEntries() ([]gitpkg.IgnoredEntry, error)
	IgnoredFilesInDir(dir string) ([]string, error)
	BaseCommits(base string, limit int) ([]gitpkg.Commit, error)
	// BehindCount returns how many commits baseRef has that HEAD doesn't. An
	// error means unknown — callers must not treat it as 0 ("up to date").
	BehindCount(baseRef string) (int, error)
	Parent(sha string) (string, error)
	FirstChildToward(base, head string) (string, error)
	RWXResults(runID string) (*gitpkg.RWXResult, error)
	RWXTaskLog(taskID string) (string, error)
	RWXTestResults(taskID string) ([]gitpkg.RWXFailedTest, error)
}

type Model struct {
	debugLog *log.Logger
	git      GitDataSource
	// cmdFactory is the background lane: every subprocess the app runs on its
	// own initiative comes from here and is killed if it overruns
	// command.DefaultTimeout. New call sites belong on this one.
	cmdFactory command.Factory
	// interactiveFactory is the untimed lane, for foreground programs handed to
	// tea.Exec with the TUI suspended — an $EDITOR session the user may keep
	// open for an hour, or a browser opener that blocks until the browser does.
	interactiveFactory command.Factory
	mode               Mode
	focus              Focus
	width              int
	height             int
	// scope describes the commit range currently in view: (oldBase, newBase].
	// It owns the scope-extend / scope-contract / scope-reset state and feeds
	// the in-scope commit/file queries below. See scope.go.
	scope    *scope
	repoInfo gitpkg.RepoInfoResult
	// gitDispatchSeq is the last dispatch number handed to a git load;
	// gitAdoptedSeq is the highest one whose repo identity has been adopted.
	// Together they let the handler recognise a load that finished out of
	// order and refuse to let its stale answer overwrite a newer one. See
	// gitLoadRequest.seq.
	gitDispatchSeq int
	gitAdoptedSeq  int
	// gitLoads single-flights async git-load dispatch: N near-simultaneous
	// triggers become one load plus at most one trailing load. It composes with
	// the seq protocol above rather than replacing it — seq discards stale
	// results, the gate stops redundant loads from being started. See
	// loadgate.go.
	gitLoads           loadGate
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
	moreCommitsPending bool                  // a load-more page is dispatched and hasn't landed yet
	behindCount        int                   // how many commits behind base
	behindKnown        bool                  // behindCount was measured; false = unknown, don't render
	baseCommits        []gitpkg.Commit       // commits from the base branch (for commit mode category 4)
	prComments         []gitpkg.PRComment    // PR comments for PR-view mode
	prDeployments      []gitpkg.PRDeployment // PR deployments for PR-view mode
	ciChecks           []gitpkg.CICheck      // CI checks for PR-view mode
	rwxFetcher         *rwxFetcher           // RWX log fetch/cache state
	viewMemory         *viewMemory           // per-mode sidebar + per-item main-pane scroll
	pseudoDiffs        pseudoDiffCache       // commits-mode pseudo-entry bodies, per git-load cycle
	lastMainItem       mainItemKey           // (mode, item) currently displayed in main pane
	sidebar            *sidebar
	mainPane           *mainPane
	sidebarPct         int // sidebar width as percentage of total width (10-50)
	dir                string
	confirming         bool
	help               *helpOverlay      // help overlay subsystem
	showIgnored        bool              // whether to show gitignored files in all-files section
	collapsedDirs      map[string]bool   // tracks collapsed directory paths
	sidebarHidden      bool              // [f] toggles sidebar visibility
	wordWrap           bool              // [w] toggles word wrapping in main pane
	lineNumbers        bool              // [n] toggles line numbers in files mode
	search             *searchOverlay    // cross-pane search overlay
	hoverX, hoverY     int               // last mouse position for hover highlighting
	activity           *activityTracker  // adaptive refresh-interval bookkeeping
	drag               *dragSelection    // click-drag-release selection state
	cursor             *cursor           // persistent pointing position in the main pane
	selection          *selection        // vim-style visual-mode selection state
	notifications      notificationState // transient toast (bottom-left)
	loading            bool              // true until first local data load completes
	prLoadedOnce       bool              // true after first successful PR data fetch
	modeLabels         []modeLabel       // clickable mode label positions from last render
	line2Labels        []line2Label      // clickable positions on git status line
	line3Labels        []line3Label      // clickable positions on PR status line
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
	// queryOldBase is the base SHA the load actually queried against: the
	// user's scrubbed endpoint when scrubbed, otherwise the natural base the
	// load itself just detected. Informational — the guard uses
	// reqScrubbedBase, not this.
	queryOldBase string
	// reqScrubbedBase is the dispatch-time snapshot of the *user's* scope pin:
	// scope.OldBase() when the scope was scrubbed, "" when it sat at its
	// natural position. It is the stale-load guard's key. On receipt the
	// handler recomputes the same value from current scope state and discards
	// the load only when they differ — i.e. only when the user moved the scope
	// underneath the load. A load that merely observes the *natural* base
	// moving (rebase, base branch advanced) still answers the question that
	// was asked ("give me the natural range"), so it is applied: it is the
	// source of the base movement and its data is the freshest available.
	reqScrubbedBase string
	// natural{Old,New}Base / natural{Old,New}Offset describe the freshly-detected
	// natural endpoints at load time. Passed to scope.SyncFromLoad, which either
	// adopts them (when not scrubbed) or only updates the natural fields (when
	// scrubbed, preserving the user's scrub).
	naturalOldBase   string
	naturalNewBase   string
	naturalOldOffset int
	naturalNewOffset int
	// pinnedOldOffset is this load's measurement of how far the user's pinned
	// outer endpoint (reqScrubbedBase) sits from HEAD, or -1 when the load
	// carried no pin. The scrubbed-ness is anchored to the SHA, so the
	// `HEAD~N` indicator needs a fresh distance on every load — otherwise it
	// goes stale the moment a new commit lands.
	pinnedOldOffset int
	// seq echoes gitLoadRequest.seq — the dispatch this msg is answering.
	seq            int
	changes        *gitpkg.ChangedFiles
	allFiles       []string
	ignoredFiles   map[string]bool
	ignoredDirs    map[string]bool // subset of ignoredFiles whose entries are directories
	commits        []gitpkg.Commit
	baseCommits    []gitpkg.Commit
	prComments     []gitpkg.PRComment
	prDeployments  []gitpkg.PRDeployment
	ciChecks       []gitpkg.CICheck
	reviewRequests []gitpkg.PRReviewRequest
	behindCount    int
	// behindKnown: behindCount was actually measured. False means the count is
	// unknown (rev-list failed, base ref missing) and must not be rendered —
	// an unknown count is not "0 behind".
	behindKnown   bool
	prFetchFailed bool // true if PR fetch errored — preserve old PR data
	// prErrKind: how the PR fetch failure classified, so this load feeds the
	// backoff and the status line exactly like a PR-tick fetch would. One
	// field rather than a set of booleans, so the outcomes cannot disagree
	// (nothing can be both a rate limit and an auth error).
	prErrKind githubErrorKind
	// prErrText: the raw error text the PR fetch failed with, snapshotted on
	// the msg like every other input (never read back off Model state).
	// Rendered only for the unclassified bucket — see statusMessageWith.
	prErrText string
	// checksFailed: PR fetch succeeded but the checks fetch didn't — preserve
	// the CI data we already have instead of applying zeros.
	checksFailed bool
	localOnly    bool // true if this was a local-only refresh (no API calls attempted)
	err          error
}

type RefreshMsg struct{}

type moreCommitsMsg struct {
	commits []gitpkg.Commit
	// base and skip are the dispatch-time snapshot the page was computed
	// from: the scope endpoint it was queried against and the number of
	// commits already loaded when it was requested. The handler appends only
	// when both still match current state — which discards a page computed
	// against a base the user has since scrubbed away from, and makes a
	// duplicate dispatch (click + enter) idempotent rather than
	// double-appending.
	base string
	skip int
	err  error
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
	// fetchFailed: the PR fetch itself failed, so every PR field on this msg is
	// zero and the handler must preserve what it already has.
	fetchFailed bool
	// errKind: how the failure classified. Only ghErrRateLimited backs the
	// poll interval off; the rest are reported at the normal cadence.
	errKind githubErrorKind
	// errText: the raw error text the fetch failed with, computed in the
	// fetch function and carried on the msg. Rendered only for the
	// unclassified bucket — see statusMessageWith.
	errText string
	// checksFailed: the PR fetch succeeded but the checks fetch didn't, so
	// ciStatus/ciChecks are zero and the handler must keep the previous CI data
	// rather than blanking the panel.
	checksFailed bool
}

type prTickMsg struct{}
type gitTickMsg struct{}

// notificationExpiredMsg is a toast's expiry timer firing. gen names the
// notification it was armed for, so a timer belonging to a toast that has since
// been replaced is recognized and ignored rather than clearing its successor.
type notificationExpiredMsg struct{ gen uint64 }

// maybeFetchRWXLog returns a tea.Cmd to fetch RWX logs if there's a pending
// check staged by the previous render. Forwards to rwxFetcher.
func (m *Model) maybeFetchRWXLog() tea.Cmd {
	return m.rwxFetcher.Cmd(m.git)
}

// defaultCmdFactory is the command factory used by NewModel. Tests in the
// same package can override this to prevent accidental exec calls.
var defaultCmdFactory command.Factory = command.DefaultFactory

// defaultInteractiveFactory is the untimed factory used by NewModel for
// foreground programs. Tests do not need to stub it: interactive commands are
// only ever constructed in tests, never run.
var defaultInteractiveFactory command.Factory = command.InteractiveFactory

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
		debugLog:           debugLog,
		git:                g,
		cmdFactory:         defaultCmdFactory,
		interactiveFactory: defaultInteractiveFactory,
		dir:                dir,
		mode:               FilesMode,
		focus:              SidebarFocus,
		scope:              &scope{},
		sidebar:            newSidebar(),
		mainPane:           newMainPane(),
		sidebarPct:         30, // default 30% of width
		showIgnored:        true,
		collapsedDirs:      make(map[string]bool),
		rwxFetcher:         newRWXFetcher(),
		viewMemory:         newViewMemory(),
		wordWrap:           true,
		lineNumbers:        true,
		activity:           newActivityTracker(time.Now()),
		help:               newHelpOverlay(),
		search:             newSearchOverlay(),
		drag:               newDragSelection(),
		cursor:             newCursor(),
		selection:          newSelection(),
		loading:            g != nil,
		changes:            gitpkg.NewChangedFiles(),
	}
}

func (m *Model) Init() tea.Cmd {
	if m.git == nil {
		return loadNonGitFilesCmd(m.dir)
	}
	m.activity.MarkPRFetch(time.Now())
	return tea.Batch(m.gitLoadCmd(false), loadPRStatusCmd(m.git), schedulePRTick(m.activity.PRInterval()), scheduleGitTick(m.activity.GitInterval()))
}

// prTickScheduler builds the delayed prTickMsg command. Indirected through a
// var so tests can observe the interval the loop actually schedules at.
// A test that stubs this must NOT call t.Parallel(): the var is process-wide,
// and Go only defers parallel tests past serial ones, so a parallel stubber
// would race every other test's ticks.
var prTickScheduler = func(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return prTickMsg{}
	})
}

func schedulePRTick(interval time.Duration) tea.Cmd {
	return prTickScheduler(interval)
}

func scheduleGitTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return gitTickMsg{}
	})
}

// loadNonGitFilesCmd walks dir in the background. Closes over dir only.
func loadNonGitFilesCmd(dir string) tea.Cmd {
	return func() tea.Msg { return walkNonGitFiles(dir) }
}

// loadNonGitFiles runs the walk synchronously against the current model.
// Update-goroutine callers only (RenderOnce, tests).
func (m *Model) loadNonGitFiles() tea.Msg { return walkNonGitFiles(m.dir) }

func walkNonGitFiles(dir string) tea.Msg {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(dir, path)
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

// loadPRStatusCmd fetches PR state in the background. Closes over the git
// source only — that's the whole of the Model state this load depends on.
func loadPRStatusCmd(g GitDataSource) tea.Cmd {
	return func() tea.Msg { return fetchPRStatus(g) }
}

// loadPRStatus runs the PR fetch synchronously. Update-goroutine callers only.
func (m *Model) loadPRStatus() tea.Msg { return fetchPRStatus(m.git) }

func fetchPRStatus(g GitDataSource) tea.Msg {
	prAll, err := g.PRAll()
	if err != nil {
		// Every fetch error preserves the PR data we already have, but only a
		// genuine rate limit backs the poll off — reporting expired auth or a
		// DNS failure as "rate limited" both lies to the user and slows the
		// poll down for a condition that retrying won't fix.
		return prRefreshMsg{
			fetchFailed: true,
			errKind:     classifyGitHubError(err),
			errText:     err.Error(),
		}
	}
	var checksResult gitpkg.PRChecksResult
	var checksFailed bool
	if prAll.Info.Number > 0 {
		var checksErr error
		checksResult, checksErr = g.PRChecksAll()
		checksFailed = checksErr != nil
	}
	return prRefreshMsg{
		checksFailed:   checksFailed,
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
	// Title text is display text: escape control characters here (the raw
	// path is still what every caller passes to git).
	if old, ok := m.renameOldPath(file); ok {
		return sanitizeDisplayText(old) + " → " + sanitizeDisplayText(file)
	}
	return sanitizeDisplayText(file)
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

// expandIgnoredDirCmd returns a Cmd that fetches the contents of an ignored
// directory in the background and posts an ignoredDirLoadedMsg. Closes over
// the git source and the dir name only.
func expandIgnoredDirCmd(g GitDataSource, dir string) tea.Cmd {
	return func() tea.Msg {
		files, _ := g.IgnoredFilesInDir(dir)
		return ignoredDirLoadedMsg{dir: dir, files: files}
	}
}

// reloadAllFiles runs the file-list fetch synchronously. Update-goroutine
// callers only.
func (m *Model) reloadAllFiles() tea.Msg { return fetchAllFiles(m.git) }

func fetchAllFiles(g GitDataSource) tea.Msg {
	files, _ := g.AllFiles()
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

// branchIdentity names the checkout the scope's default range is relative to.
// PROMPT.md:232 requires a scrubbed scope to reset when that changes.
//
// It is deliberately *not* keyed on HeadSHA: HeadSHA moves on every commit, and
// resetting the scope on every commit is the same distance-vs-identity mistake
// A5 removed from `scope`. A detached HEAD is therefore one bucket — moving
// between two detached commits keeps the scrub, which is the conservative
// choice (the user's pin is still reachable) and matches the spec's wording.
//
// The empty string means "no repo info observed yet", which is why the caller
// treats it as "not a switch" rather than as a distinct branch.
func branchIdentity(info gitpkg.RepoInfoResult) string {
	if info.IsDetachedHead {
		return "\x00detached"
	}
	return info.Branch
}

// gitLoadRequest is the dispatch-time snapshot of every piece of Model state an
// async git load depends on. Cmd closures capture a gitLoadRequest and nothing
// else — never the *Model — so the load runs against a frozen view of the world
// while Update stays free to keep mutating. See CLAUDE.md, "tea.Cmd closures
// must not read Model state".
type gitLoadRequest struct {
	git GitDataSource
	// withPR: also fetch GitHub PR data (PRAll / PRChecksAll). When false the
	// load is local-only and the handler preserves existing PR data.
	withPR bool
	// scrubbedBase is the scope endpoint the *user* pinned: scope.OldBase()
	// when scope.IsScrubbed(), "" when the scope sits at its natural position.
	// It is both a query input and the guard key carried back in
	// gitDataMsg.reqScrubbedBase.
	scrubbedBase string
	// commitsLoaded is m.commitsLoaded at dispatch. The load re-fetches at
	// least that many commits so a refresh tick doesn't drop pagination.
	commitsLoaded int
	// prBaseRef is m.prInfo.BaseRef at dispatch. Consulted only when !withPR;
	// a withPR load uses the BaseRef it just fetched.
	prBaseRef string
	// seq is a monotonic dispatch number, assigned on the Update goroutine and
	// echoed back in gitDataMsg.seq. Loads can finish out of order (a slow
	// cross-checkout load landing after a fast local one), and "which checkout
	// are we on" is last-writer-wins state that an older answer must not be
	// allowed to overwrite. seq 0 means "not from a tracked dispatch" — a
	// hand-built msg in a test — and is never treated as stale.
	seq int
}

// gitLoadRequest snapshots the Model state a git load reads. Must be called on
// the Update goroutine.
func (m *Model) gitLoadRequest(withPR bool) gitLoadRequest {
	m.gitDispatchSeq++
	req := gitLoadRequest{
		git:           m.git,
		withPR:        withPR,
		commitsLoaded: m.commitsLoaded,
		prBaseRef:     m.prInfo.BaseRef,
		seq:           m.gitDispatchSeq,
	}
	if m.scope.IsScrubbed() {
		req.scrubbedBase = m.scope.OldBase()
	}
	return req
}

// gitLoadCmd is the only supported way to dispatch a git load. It snapshots on
// the Update goroutine and returns a closure over the snapshot alone.
//
// It is single-flighted through m.gitLoads: while a load is in flight this
// returns nil and records a pending rerun, which the Update wrapper releases as
// one trailing load when the in-flight result lands. Callers therefore have to
// tolerate a nil cmd — tea.Batch and a bare `return m, nil` both already do.
func (m *Model) gitLoadCmd(withPR bool) tea.Cmd {
	if !m.gitLoads.Begin() {
		return nil
	}
	return m.gitLoadCmdNow(withPR)
}

// gitLoadCmdNow builds a load cmd without consulting the gate. Only two
// callers: gitLoadCmd, which has just claimed the in-flight slot, and the
// Update wrapper releasing a trailing load, for which Done has already claimed
// it. Everything else must go through gitLoadCmd.
func (m *Model) gitLoadCmdNow(withPR bool) tea.Cmd {
	req := m.gitLoadRequest(withPR)
	return func() tea.Msg { return runGitLoad(req) }
}

// loadGitData runs a full (PR-inclusive) load synchronously against the
// current model state. Update-goroutine callers only — RenderOnce,
// RenderWithKeys and tests. Interactive dispatch goes through gitLoadCmd.
func (m *Model) loadGitData() tea.Msg { return runGitLoad(m.gitLoadRequest(true)) }

// loadLocalGitData runs a local-only load synchronously. Same contract as
// loadGitData: synchronous callers only.
func (m *Model) loadLocalGitData() tea.Msg { return runGitLoad(m.gitLoadRequest(false)) }

// runGitLoad performs a git load against a frozen request. It touches no
// Model state, so it is safe to run on a Cmd goroutine.
func runGitLoad(req gitLoadRequest) tea.Msg {
	g := req.git
	info, err := g.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Empty repo (no commits yet): skip diff/commit operations that require HEAD
	if info.IsEmpty {
		allFiles, _ := g.AllFiles()
		changes := gitpkg.NewChangedFiles()
		for _, p := range allFiles {
			changes.Add(gitpkg.ChangedFile{Path: p, Section: gitpkg.SectionUncommitted, Class: gitpkg.ClassAdded})
		}
		return gitDataMsg{
			repoInfo:        info,
			allFiles:        allFiles,
			changes:         changes,
			reqScrubbedBase: req.scrubbedBase,
			pinnedOldOffset: -1,
			seq:             req.seq,
			localOnly:       !req.withPR,
		}
	}

	// PR data, when this load is the PR-inclusive variant. A failure is
	// reported via prFetchFailed rather than erroring the whole load, so the
	// local half still refreshes.
	var (
		prAll         gitpkg.PRAllResult
		prFetchFailed bool
		prErrKind     githubErrorKind
		prErrText     string
		checksFailed  bool
		ciStatus      gitpkg.CIStatusResult
		ciChecks      []gitpkg.CICheck
	)
	prBaseRef := req.prBaseRef
	if req.withPR {
		var prErr error
		prAll, prErr = g.PRAll()
		prFetchFailed = prErr != nil
		// Classified here, exactly as fetchPRStatus does, so the handler can
		// back off on a rate limit and report anything else generically.
		prErrKind = classifyGitHubError(prErr)
		if prErr != nil {
			prErrText = prErr.Error()
		}
		prBaseRef = prAll.Info.BaseRef
		if prAll.Info.Number > 0 {
			checksResult, checksErr := g.PRChecksAll()
			checksFailed = checksErr != nil
			ciStatus = checksResult.Status
			ciChecks = checksResult.Checks
		}
	}

	// Detect the natural scope base. Prefers the PR-reported base ref when
	// one is known — freshly fetched for a withPR load, the dispatch-time
	// snapshot otherwise. (When PR data arrives later, prRefreshMsg
	// re-dispatches a local load so the natural base upgrades to match.)
	naturalOldBase, err := detectNaturalBase(g, prBaseRef)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Pick the base used for queries: the user's scrubbed outer endpoint when
	// scrubbed, the freshly-detected natural one otherwise.
	queryOldBase := naturalOldBase
	if req.scrubbedBase != "" {
		queryOldBase = req.scrubbedBase
	}

	files, err := g.ChangedFiles(queryOldBase)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// In-scope commits + count are always range-relative now. On main / detached
	// HEAD the range is empty (queryOldBase == HEAD), so commits = []; the full
	// repo history is rendered below the cutline via BaseCommits.
	pageSize := max(commitPageSize, req.commitsLoaded)
	commits, err := g.Commits(queryOldBase, 0, pageSize)
	if err != nil {
		return gitDataMsg{err: err}
	}
	naturalOldOffset, _ := g.CommitCountRange(naturalOldBase)
	// Measure the pinned endpoint's own distance from HEAD in the same load.
	// -1 means "no pin in this request", which SyncFromLoad reads as "leave
	// the cached distance alone".
	pinnedOldOffset := -1
	if req.scrubbedBase != "" {
		if n, err := g.CommitCountRange(req.scrubbedBase); err == nil {
			pinnedOldOffset = n
		}
	}

	// Compute behind count: how many commits on the base branch we don't have.
	// A failure leaves it unknown rather than 0 — the base ref may simply not
	// exist locally, and rendering that as "up to date" is a wrong claim.
	var behindCount int
	var behindKnown bool
	if !info.IsDetachedHead && info.Branch != "main" && info.Branch != "master" {
		var behindErr error
		behindCount, behindErr = g.BehindCount(baseRefForBehind(prBaseRef, info.Branch, info.Upstream))
		behindKnown = behindErr == nil
	}

	// Below-cutline commits (out-of-scope context). On non-detached-HEAD this
	// is what the commits-mode "Base" section renders.
	var baseCommits []gitpkg.Commit
	if !info.IsDetachedHead {
		baseCommits, _ = g.BaseCommits(queryOldBase, 50)
	}

	// Fetch tracked + untracked files (no ignored — those come from the
	// dedicated --directory query so giant ignored trees like node_modules/
	// don't blow up the file list).
	allFiles, _ := g.AllFiles()
	ignoredSet, ignoredDirSet := loadIgnoredSet(g)

	msg := gitDataMsg{
		repoInfo:         info,
		queryOldBase:     queryOldBase,
		reqScrubbedBase:  req.scrubbedBase,
		seq:              req.seq,
		naturalOldBase:   naturalOldBase,
		naturalOldOffset: naturalOldOffset,
		pinnedOldOffset:  pinnedOldOffset,
		changes:          files.ToChangedFiles(),
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
		ignoredDirs:      ignoredDirSet,
		commits:          commits,
		baseCommits:      baseCommits,
		behindCount:      behindCount,
		behindKnown:      behindKnown,
		localOnly:        !req.withPR, // preserve existing PR data
	}
	if req.withPR {
		msg.prInfo = prAll.Info
		msg.ciStatus = ciStatus
		msg.ciChecks = ciChecks
		msg.prReviews = prAll.Reviews
		msg.prCommentCount = prAll.CommentCount
		msg.prComments = prAll.Comments
		msg.prDeployments = prAll.Deployments
		msg.reviewRequests = prAll.ReviewRequests
		msg.prFetchFailed = prFetchFailed
		msg.prErrKind = prErrKind
		msg.prErrText = prErrText
		msg.checksFailed = checksFailed
	}
	return msg
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

// loadMoreCommitsCmd dispatches the next page of in-scope commits, snapshotting
// the base and offset it is computed from. Returns nil when a page is already
// in flight, so the click and enter paths can't both fire one.
func (m *Model) loadMoreCommitsCmd() tea.Cmd {
	if m.moreCommitsPending {
		return nil
	}
	m.moreCommitsPending = true
	g := m.git
	base := m.scope.OldBase()
	skip := m.commitsLoaded
	return func() tea.Msg { return fetchMoreCommits(g, base, skip) }
}

// loadMoreCommits runs a load-more page synchronously against current state.
// Update-goroutine callers only (tests); interactive dispatch goes through
// loadMoreCommitsCmd.
func (m *Model) loadMoreCommits() tea.Msg {
	return fetchMoreCommits(m.git, m.scope.OldBase(), m.commitsLoaded)
}

func fetchMoreCommits(g GitDataSource, base string, skip int) tea.Msg {
	commits, err := g.Commits(base, skip, commitPageSize)
	return moreCommitsMsg{commits: commits, base: base, skip: skip, err: err}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Release the single-flight gate here rather than inside the gitDataMsg arm:
	// that arm has half a dozen early returns (load error, stale dispatch,
	// branch-switch reset, pin mismatch) and a gate released on only some of
	// them would wedge every later refresh for the session. Every gitDataMsg is
	// exactly one load's result, whether it was adopted or discarded, so this is
	// the one place guaranteed to see all of them.
	//
	// The release must run BEFORE m.update, because the arm can dispatch a load
	// of its own (the branch-switch path resets a stale scrub and reloads). With
	// the release afterwards, an arm that claimed the in-flight slot had it
	// un-claimed by this same message's Done, leaving a genuinely outstanding
	// load behind an idle gate — so the next trigger started a second one. The
	// arm's Begin now runs against the already-released gate: it either claims
	// the freed slot, or is coalesced into the pending rerun if a trailing load
	// took it, and either way exactly one load is in flight.
	trailing := false
	if _, isGitData := msg.(gitDataMsg); isGitData {
		trailing = m.gitLoads.Done()
	}

	result, cmd := m.update(msg)
	rm := result.(*Model)
	cmds := []tea.Cmd{cmd}
	// Built after m.update, not above: the trailing load must snapshot the state
	// this message just produced (a reset scope, a new base), not the state it
	// replaced. Done has already counted it as the in-flight load.
	if trailing {
		cmds = append(cmds, rm.gitLoadCmdNow(false))
	}
	cmds = append(cmds, rm.maybeFetchRWXLog())
	return result, batchNonNil(cmds...)
}

// batchNonNil batches the non-nil cmds, returning nil for none and the cmd
// itself for exactly one — tea.Batch tolerates nils, but returning a Batch of
// one wraps a cmd the tests compare against nil.
func batchNonNil(cmds ...tea.Cmd) tea.Cmd {
	var live []tea.Cmd
	for _, c := range cmds {
		if c != nil {
			live = append(live, c)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	default:
		return tea.Batch(live...)
	}
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
			m.debugLog.Printf("[timer] notificationExpired gen=%d", msg.gen)
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
			m.debugLog.Printf("[data] prRefreshMsg fetchFailed=%v errKind=%v checksFailed=%v",
				msg.fetchFailed, msg.errKind, msg.checksFailed)
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
		// This load's local half succeeded, so whatever previous failure is on
		// screen is over. Without this the error screen was terminal: one
		// index.lock collision during a rebase wedged the UI for the session
		// while the 5s tick kept loading good data behind it. Every non-error
		// gitDataMsg clears it — a local-only refresh and a load whose PR half
		// failed both prove the local half worked, and the PR failure has its
		// own display (prError).
		m.err = nil
		// This is the cycle that refreshes the sidebar's file counts, so it is
		// also where the pseudo-entry bodies behind those counts go stale.
		m.pseudoDiffs.Invalidate()
		// Which checkout we are on is last-writer-wins state, so an answer from
		// an older dispatch must not overwrite a newer one. Loads do finish out
		// of order: a slow cross-checkout load dispatched on branch A can land
		// after a fast load has already adopted branch B and the user has
		// scrubbed there — and the branch-switch reset below would then read
		// A-vs-B as a fresh checkout and throw away the B scrub, while adopting
		// stale branch-A repoInfo. Anything at or above the high-water mark is
		// fresh; seq 0 (a hand-built msg, never a real dispatch) always is.
		staleDispatch := msg.seq != 0 && msg.seq < m.gitAdoptedSeq
		branchSwitched := false
		if !staleDispatch {
			m.gitAdoptedSeq = msg.seq
			branchSwitched = branchIdentity(m.repoInfo) != "" &&
				branchIdentity(m.repoInfo) != branchIdentity(msg.repoInfo)
			m.repoInfo = msg.repoInfo
		}
		// Local-only refresh: preserve all existing PR data and error state
		// PR fetch failed: preserve PR data but flag the error
		// Otherwise: update PR data normally
		if !msg.localOnly {
			m.prLoadedOnce = true
			// This load talked to GitHub, so it feeds the same rate-limit state
			// machine as the PR tick: classify the failure, or let a success
			// clear a backoff the tick loop is still sitting in.
			m.activity.MarkPRFetch(time.Now())
			if msg.prFetchFailed {
				// Unlike the prRefreshMsg arm, a non-rate-limit failure here
				// does not call ResetPRInterval. The difference is deliberate
				// and inert: this path is not the poll loop, so it has no tick
				// cadence of its own to restore, and ResetPRInterval only ever
				// recomputes max(activity-derived, latched backoff) — it cannot
				// clear a latch (only MarkPRSuccess does). Both arms therefore
				// leave the same interval behind. Don't "fix" the asymmetry by
				// adding a reset here; it would only add a redundant recompute.
				kind := msg.prErrKind.reported()
				if kind.backsOff() {
					m.activity.BumpRateLimited(time.Now())
				}
				m.prError = kind.statusMessageWith(msg.prErrText)
			} else {
				m.activity.MarkPRSuccess(time.Now())
				m.prError = ""
				// Preserving CI data across a checks failure is only right while
				// it describes the same PR — under a different PR number it would
				// render the previous PR's checks beneath the new PR's header.
				keepChecks := msg.checksFailed && msg.prInfo.Number == m.prInfo.Number
				m.prInfo = msg.prInfo
				if !keepChecks {
					m.ciStatus = msg.ciStatus
					m.ciChecks = msg.ciChecks
					// Fresh CI data means the run may have moved on, so an RWX
					// log fetch that failed earlier is worth retrying. Riding
					// the poll like this bounds the retry rate without a timer.
					m.rwxFetcher.InvalidateErrors()
				}
				m.prReviews = msg.prReviews
				m.prReviewRequests = msg.reviewRequests
				m.prCommentCount = msg.prCommentCount
				m.prComments = msg.prComments
				m.prDeployments = msg.prDeployments
				m.sortPRData()
			}
		}
		// Stale-load guard. A load answers exactly one question, fixed at
		// dispatch: "the range pinned at <reqScrubbedBase>", or — for the empty
		// string — "the natural range, wherever it is now". Discard only when
		// the user's pin has moved underneath the load, i.e. when the question
		// this msg answers is no longer one we're asking. Sync the natural
		// endpoints first so scope-reset still snaps correctly, then discard
		// the rest; the load re-dispatched by the scope command is
		// authoritative.
		//
		// Crucially this does NOT discard on natural base movement (rebase,
		// base branch advanced): the load that detected the new base queried
		// against it, so its results are the freshest available. Comparing
		// against scope.OldBase() instead threw exactly those away.
		// PROMPT.md:232 — "the scope resets to default on branch switch." The
		// scrub is a pinned *commit* (see scope.pinned); it was chosen relative
		// to the branch that was checked out, so after a checkout it is either
		// absent from the new branch or names something unrelated, while every
		// later load kept passing it as the range's outer endpoint. Reset
		// before the pin guard below, which then discards this msg's local
		// payload — it was computed against the now-stale pin — and re-dispatch
		// a load for the default range, the same reset+reload pairing the
		// ScopeReset key uses.
		if branchSwitched && m.scope.IsScrubbed() {
			m.scope.Reset()
			m.scope.SyncFromLoad(msg.naturalOldBase, msg.naturalNewBase, msg.naturalOldOffset, msg.naturalNewOffset, msg.reqScrubbedBase, msg.pinnedOldOffset)
			m.updateLayout()
			m.updateSidebarItems()
			m.updateMainContent()
			if m.git == nil {
				return m, nil
			}
			return m, m.gitLoadCmd(false)
		}

		currentPin := ""
		if m.scope.IsScrubbed() {
			currentPin = m.scope.OldBase()
		}
		if msg.reqScrubbedBase != currentPin {
			m.scope.SyncFromLoad(msg.naturalOldBase, msg.naturalNewBase, msg.naturalOldOffset, msg.naturalNewOffset, msg.reqScrubbedBase, msg.pinnedOldOffset)
			m.updateLayout()
			m.updateSidebarItems()
			m.updateMainContent()
			return m, nil
		}

		m.scope.SyncFromLoad(msg.naturalOldBase, msg.naturalNewBase, msg.naturalOldOffset, msg.naturalNewOffset, msg.reqScrubbedBase, msg.pinnedOldOffset)
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
		m.behindKnown = msg.behindKnown
		// Recalculate layout — status bar height may have changed
		m.updateLayout()
		// On first load, default to PR mode if a PR exists and mode hasn't
		// been changed. Goes through setMode so the files-mode view state the
		// user was looking at is saved and comes back with them; a direct
		// assignment silently dropped it. Layout is settled first so the
		// restore inside setMode measures against the final pane size.
		if wasLoading && m.prInfo.Number > 0 && m.mode == FilesMode {
			m.setMode(PRMode)
		}
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case moreCommitsMsg:
		// Always clear the marker, including on error — otherwise one failed
		// page wedges pagination for the rest of the session.
		m.moreCommitsPending = false
		if msg.err != nil {
			return m, nil
		}
		// The page continues msg.skip commits after msg.base. If the scope has
		// moved it describes the wrong slice of history; if the commit list is
		// no longer exactly msg.skip long, a page covering this range already
		// landed and appending would duplicate it.
		if msg.base != m.scope.OldBase() || msg.skip != m.commitsLoaded {
			return m, nil
		}
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
		// A classified error implies fetchFailed; both are accepted so the two
		// fields can't disagree about whether there is any data to apply.
		if msg.fetchFailed || msg.errKind != ghErrNone {
			// Preserve the PR data we have either way; only a real rate limit
			// backs the interval off (PROMPT.md:21). The bump is latched in the
			// tracker, so neither the next tick's ResetPRInterval nor the tick
			// already in flight at the old interval can undo it — see the
			// prTickMsg arm's PRFetchDue gate. An auth-or-permission failure
			// deliberately keeps the normal cadence: waiting cannot grant a
			// scope or clear SAML enforcement, and the message is on line 3
			// either way.
			//
			// "Normal cadence" means *unless a rate-limit backoff is already
			// latched*. ResetPRInterval recomputes through
			// effectivePRInterval = max(activity-derived, rateLimitBackoff),
			// so a rate limit followed by an auth failure holds the latched
			// floor; only MarkPRSuccess — a fetch that actually came back —
			// clears the latch. That is deliberate: a 403 telling us the token
			// lacks a scope is not evidence the throttle lifted, so speeding
			// the poll back up on it would walk straight back into the rate
			// limit. See TestAuthErrorDuringRateLimitBackoff.
			kind := msg.errKind.reported()
			if kind.backsOff() {
				m.activity.BumpRateLimited(time.Now())
			} else {
				m.activity.ResetPRInterval(time.Now())
			}
			m.prError = kind.statusMessageWith(msg.errText)
			m.updateLayout()
			return m, nil
		}
		// Track whether the server data actually changed. The CI state only
		// counts when we actually fetched it — a failed checks call reports a
		// zero state, which would otherwise read as a change every time.
		ciStateChanged := !msg.checksFailed && msg.ciStatus.State != m.ciStatus.State
		if msg.prInfo.Number != m.prInfo.Number ||
			msg.prInfo.Title != m.prInfo.Title ||
			msg.prInfo.State != m.prInfo.State ||
			msg.prInfo.ReviewDecision != m.prInfo.ReviewDecision ||
			ciStateChanged ||
			msg.commentCount != m.prCommentCount ||
			len(msg.reviews) != len(m.prReviews) {
			m.activity.MarkServerChange(time.Now())
		}
		// Detect base-branch change (either newly-learned from PR data, or the
		// PR's base ref was edited server-side). If so, re-dispatch the local
		// git refresh so diffs/commits/changed-files recompute against the new
		// base.
		baseRefChanged := msg.prInfo.BaseRef != m.prInfo.BaseRef && msg.prInfo.BaseRef != ""
		// Successful fetch — clear any rate-limit backoff, return the interval
		// to whatever activity implies, and clear the error.
		m.activity.MarkPRSuccess(time.Now())
		m.prError = ""
		// A checks-fetch failure leaves ciStatus/ciChecks zero on the msg; keep
		// the CI data we have rather than blanking the panel — but only while it
		// still describes this PR. Under a new PR number the old checks would
		// render beneath the new PR's header, so clear them instead.
		keepChecks := msg.checksFailed && msg.prInfo.Number == m.prInfo.Number
		m.prInfo = msg.prInfo
		if !keepChecks {
			m.ciStatus = msg.ciStatus
			m.ciChecks = msg.ciChecks
			// See the gitDataMsg arm: fresh CI data re-opens failed RWX fetches.
			m.rwxFetcher.InvalidateErrors()
		}
		m.prReviews = msg.reviews
		m.prReviewRequests = msg.reviewRequests
		m.prCommentCount = msg.commentCount
		m.prComments = msg.prComments
		m.prDeployments = msg.prDeployments
		m.sortPRData()
		prLoadedBefore := m.prLoadedOnce
		m.prLoadedOnce = true
		m.updateLayout()
		// On first PR data arrival, switch to PR mode if user hasn't changed
		// modes. Through setMode for the same reason as the gitDataMsg arm:
		// the outgoing mode's view state has to be saved, or switching back
		// lands on whatever index PR mode left behind.
		if !prLoadedBefore && m.prInfo.Number > 0 && m.mode == FilesMode {
			m.setMode(PRMode)
		}
		m.updateSidebarItems()
		m.updateMainContent()
		if baseRefChanged && m.git != nil {
			return m, m.gitLoadCmd(false)
		}
		return m, nil

	case rwxLogMsg:
		m.rwxFetcher.Apply(msg)
		m.updateMainContent()
		return m, nil

	case prTickMsg:
		// Recompute interval on each tick based on current activity state. A
		// latched rate-limit backoff survives this (activityTracker.ResetPRInterval).
		now := time.Now()
		m.activity.ResetPRInterval(now)
		// This tick may have been scheduled before a rate limit came back, in
		// which case it is due sooner than the backoff allows: re-arm for the
		// remainder of the backoff instead of fetching. Without this the first
		// retry after a 403 still went out at the un-bumped interval.
		if m.git == nil || !m.activity.PRFetchDue(now) {
			return m, schedulePRTick(m.activity.PRTickDelay(now))
		}
		m.activity.MarkPRFetch(now)
		return m, tea.Batch(loadPRStatusCmd(m.git), schedulePRTick(m.activity.PRTickDelay(now)))

	case notificationExpiredMsg:
		m.notifications.Expire(msg)
		return m, nil

	case gitTickMsg:
		m.activity.ResetGitInterval(time.Now())
		if m.git == nil {
			return m, scheduleGitTick(m.activity.GitInterval())
		}
		return m, tea.Batch(m.gitLoadCmd(false), scheduleGitTick(m.activity.GitInterval()))

	case RefreshMsg:
		// Any fs-watcher event means the working directory is active;
		// stamp lastGitChange so computeGitInterval keeps us on the fast poll.
		m.activity.MarkFSEvent(time.Now())
		if m.git == nil {
			return m, loadNonGitFilesCmd(m.dir)
		}
		// Use local-only refresh (no GitHub API calls). File watcher
		// events fire frequently and should not hit the network.
		// Full PR data is refreshed on the PR tick cycle instead.
		return m, m.gitLoadCmd(false)

	case ipcMsg:
		return m.handleIPC(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		// The help overlay is modal: it covers both panes, so a click on it
		// must not reach the sidebar or start a drag underneath.
		if m.help.IsOpen() {
			return m, nil
		}
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
		if m.help.IsOpen() {
			// Modal: no drag extension, no sidebar hover under the overlay.
			m.sidebar.SetHoverIndex(-1)
			return m, nil
		}
		g := m.dragGeom()
		var autoScrollCmd tea.Cmd
		if m.drag.IsActive() {
			m.drag.MoveEnd(g.clickAt(msg.X, msg.Y))
			autoScrollCmd = m.drag.UpdateAutoScroll(msg.Y, g)
		}
		// Update sidebar hover index
		if g.regionAt(msg.X, msg.Y) == regionSidebar {
			contentY := msg.Y - g.statusRows
			itemIdx := contentY - 1 + m.sidebar.offset
			m.sidebar.SetHoverIndex(itemIdx)
		} else {
			m.sidebar.SetHoverIndex(-1)
		}
		return m, autoScrollCmd

	case tea.MouseReleaseMsg:
		if m.help.IsOpen() {
			// Swallowing the release must not strand an in-flight drag: help
			// can be opened with `?` mid-drag, and a dropped release would
			// leave m.drag active so motion kept extending the selection after
			// the overlay closed. Cancel it (no cursor placement, no copy —
			// the gesture was interrupted, not completed).
			m.drag.Cancel()
			return m, nil
		}
		g := m.dragGeom()
		ep := g.clickAt(msg.X, msg.Y)
		// Whether a drag was in progress decides whether this release is the
		// end of a main-pane gesture at all. A sidebar click cancels the drag,
		// so its release must not be read as concluding one.
		wasDragging := m.drag.IsActive()
		hadRange := m.drag.Release(ep)
		// Place cursor at release point when the release concludes a main-pane
		// drag and lands on real content (per PLAN.md "cursor at release
		// point"). Past-EOL release clamps to row content via SetFromClick.
		if wasDragging && ep.OutsideDir == 0 && !ep.OutsideSidebar {
			m.nav().PlaceCursorFromClick(ep.VpRow, ep.DisplayCol)
		}
		if hadRange {
			return m, m.copySelection()
		}
		return m, nil

	case dragScrollTickMsg:
		if !m.drag.IsActive() || m.drag.ScrollDir() == 0 {
			return m, nil
		}
		return m, m.nav().AdvanceDragAutoScroll(m.dragGeom())
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
		return m, m.nav().ForwardKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}

	// Search input mode
	if m.search.IsSearching() {
		m.search.HandleInputKey(msg, m.nav())
		return m, nil
	}

	// Search confirmed mode (n/p navigation)
	if m.search.IsConfirmed() {
		if m.search.HandleNavKey(msg, m.nav()) {
			return m, nil
		}
		return m.handleKey(msg)
	}

	// Help overlay — supports scrolling and search
	if m.help.IsOpen() {
		return m.handleHelpKey(msg)
	}

	// Quit confirmation handling. The confirm prompt replaces the whole
	// status bar with one line, so entering and leaving it changes the
	// bar's row count — relayout at each toggle or the panes stay sized
	// for the old bar until the next data refresh.
	if m.confirming {
		if msg.Code == tea.KeyEscape {
			m.setConfirming(false)
			return m, nil
		}
		if key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.QuitImmediate) {
			return m, tea.Quit
		}
		m.setConfirming(false)
		return m, nil
	}

	// Visual mode dismissal: Esc cancels the selection. Preempts the
	// QuitConfirm binding (which also has "esc") when a selection is
	// active.
	if m.selection.IsActive() && key.Matches(msg, keys.VisualDismiss) {
		m.selection.Cancel()
		return m, nil
	}

	// Visual mode entry / mode toggle (v/V). Ignored while a mouse
	// drag is in progress.
	if m.focus == MainFocus && !m.search.IsActive() && !m.help.IsOpen() && !m.drag.IsActive() {
		switch {
		case key.Matches(msg, keys.VisualStream):
			// v in stream mode: dismiss. v in line mode: switch to
			// stream, preserving anchor/active so the original
			// character range is recovered. Otherwise: enter stream
			// mode anchored at cursor.
			switch m.selection.mode {
			case selectionStream:
				m.selection.Cancel()
			case selectionLine:
				m.selection.mode = selectionStream
			default:
				m.nav().BeginVisualStream()
			}
			return m, nil
		case key.Matches(msg, keys.VisualLine):
			// V mirrors v's toggle semantics: V in line mode dismisses;
			// V in stream mode switches to line, preserving anchor/active.
			switch m.selection.mode {
			case selectionLine:
				m.selection.Cancel()
			case selectionStream:
				m.selection.mode = selectionLine
			default:
				m.nav().BeginVisualLine()
			}
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, keys.QuitImmediate):
		return m, tea.Quit

	case key.Matches(msg, keys.QuitConfirm):
		m.setConfirming(true)
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

	case key.Matches(msg, keys.CursorLeft):
		if m.focus == SidebarFocus {
			return m.handleSidebarLeft()
		}
		if m.focus == MainFocus {
			m.nav().CursorLeft()
			return m, nil
		}

	case key.Matches(msg, keys.CursorRight):
		if m.focus == SidebarFocus {
			return m.handleSidebarRight()
		}
		if m.focus == MainFocus {
			m.nav().CursorRight()
			return m, nil
		}

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
			m.nav().GoToTop()
		}
		return m, nil

	case key.Matches(msg, keys.GoBottom):
		if m.focus == SidebarFocus {
			m.sidebar.SelectLast()
			m.updateMainContent()
		} else {
			m.nav().GoToBottom()
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
		// In visual mode, y yanks the selection's text; otherwise y
		// copies the file path (existing behavior).
		if m.selection.IsActive() {
			cmd := m.copyVisualSelection()
			m.selection.Cancel()
			return m, cmd
		}
		return m, m.yankPath()

	case key.Matches(msg, keys.Refresh):
		// An explicit refresh is the user asking to retry everything that
		// failed, RWX log fetches included — otherwise one transient network
		// error stays on screen for the rest of the session.
		m.rwxFetcher.InvalidateErrors()
		if m.git == nil {
			return m, loadNonGitFilesCmd(m.dir)
		}
		return m, tea.Batch(m.gitLoadCmd(false), loadPRStatusCmd(m.git))

	case key.Matches(msg, keys.ScopeReset):
		if !m.scope.IsScrubbed() {
			return m, nil
		}
		m.scope.Reset()
		if m.git == nil {
			return m, nil
		}
		return m, m.gitLoadCmd(false)

	case key.Matches(msg, keys.ScopeExtendBack):
		if m.git == nil {
			return m, nil
		}
		if err := m.scope.ExtendBack(m.git); err != nil {
			// At root commit, unloaded, or other failure — no-op.
			return m, nil
		}
		return m, m.gitLoadCmd(false)

	case key.Matches(msg, keys.ScopeContractForward):
		if m.git == nil {
			return m, nil
		}
		if err := m.scope.ContractForward(m.git); err != nil {
			// FirstChildToward failed unexpectedly — no-op. (The empty-range
			// case is handled inside scope.ContractForward as a silent no-op.)
			return m, nil
		}
		return m, m.gitLoadCmd(false)

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
		m.nav().Reflow(func(p *mainPane) { p.SetWordWrap(m.wordWrap) })
		return m, nil

	case key.Matches(msg, keys.ToggleLineNums):
		m.lineNumbers = !m.lineNumbers
		m.nav().Reflow(func(p *mainPane) { p.SetLineNumbers(m.lineNumbers) })
		return m, nil

	case key.Matches(msg, keys.ToggleRemoved):
		if m.mode == FilesMode {
			m.nav().Reflow((*mainPane).ToggleShowRemoved)
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
		if m.focus == MainFocus {
			m.nav().CursorUp()
			return m, nil
		}

	case key.Matches(msg, keys.Down):
		if m.focus == SidebarFocus {
			m.sidebar.SelectNext()
			m.updateMainContent()
			return m, nil
		}
		if m.focus == MainFocus {
			m.nav().CursorDown()
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
		// The forwarded key may scroll the viewport (space/b/PgUp/PgDn,
		// Ctrl-D/U, …); the seam drags the cursor along and re-syncs any
		// visual-mode selection.
		return m, m.nav().ForwardKey(msg)
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
	m.search.Clear(m.nav())
}

// setConfirming toggles the quit-confirm prompt. It exists so the
// relayout can't be forgotten at a call site: `confirming` feeds
// statusBarLineCount, so every toggle changes the status bar's height and
// must resize the panes underneath it.
func (m *Model) setConfirming(on bool) {
	if m.confirming == on {
		return
	}
	m.confirming = on
	m.updateLayout()
}

// statusBarData assembles every input the status bar needs, from model
// state. It is the single source of truth for what the bar shows: both the
// render path (View) and the layout path (statusBarLines → updateLayout)
// build from this one value, so the rows rendered and the rows reserved
// can never disagree. See CLAUDE.md, "Layout geometry comes from one
// function".
func (m *Model) statusBarData() statusBarData {
	return statusBarData{
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
		behindKnown:      m.behindKnown,
		changedFileCount: m.changes.Len(),
		// PR data is still in flight either during the initial synchronous
		// load or in the window after local git data lands but before the
		// first PR fetch completes. Both render the "Loading from GitHub…"
		// row, and both must be reflected in the layout — see PROMPT.md's
		// "display the data it _does_ have immediately" rule.
		prLoading:   (m.loading || !m.prLoadedOnce) && m.git != nil,
		showHelp:    m.help.IsOpen(),
		hoverX:      m.hoverX,
		hoverY:      m.hoverY,
		scopeHandle: m.scope.Handle(),
	}
}

// statusBarLines returns the number of terminal rows the status bar
// occupies. Sole row-count authority: every layout and hit-testing call
// site goes through here.
func (m *Model) statusBarLines() int {
	return statusBarLineCount(m.statusBarData())
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
	// Row indices come from the same authority render and hover use, so a
	// coordinate never resolves against a line that isn't on that row —
	// line 3 sits on row 1, not row 2, when there is no line 2. See
	// CLAUDE.md, "Layout geometry comes from one function".
	rows := statusBarRows(m.statusBarData())
	switch y {
	case rows.line1:
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
	case rows.line2:
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
		// No row-wide fallback: a coordinate no label covers does nothing.
		// PROMPT.md requires hover regions and click regions to be the same
		// regions, and hover only ever highlights a published label — so a
		// row-wide target would be a large invisible one, which is exactly
		// what the hover state exists to rule out. Separators, the padding
		// past the last label, and anything beyond a truncation cut are all
		// inert. Same shape as line 3, which never had a fallback.
	case rows.line3:
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
	g := m.dragGeom()
	region := g.regionAt(x, y)

	if region == regionStatusBar {
		m.drag.Cancel()
		return m.handleStatusBarClick(x, y)
	}

	// Adjust y for the status bar above the panes
	contentY := y - g.statusRows
	if region == regionSidebar {
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
			return m, m.loadMoreCommitsCmd()
		}
		// If a directory was clicked, toggle collapse — except for an
		// unloaded ignored dir, where clicking should fire the lazy-load
		// cmd just like pressing right does. Without this branch, the
		// dir would flip to expanded with no children visible (forever).
		if m.sidebar.SelectedIsDir() {
			dir := m.sidebar.SelectedItem()
			key := m.sidebar.SelectedCollapseKey()
			if m.ignoredDirs[dir] && !m.loadedIgnoredDirs[dir] {
				return m, expandIgnoredDirCmd(m.git, dir)
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
		// Clicked in main pane — start drag tracking for copy AND place
		// the cursor at the click point. Release at zero-distance keeps
		// the cursor here; a drag will update cursor to the release
		// point in the release handler. Mouse drag dismisses any active
		// visual-mode selection (mouse expresses fresh intent).
		m.focus = MainFocus
		m.selection.Cancel()
		ep := g.clickAt(x, y)
		m.drag.Begin(ep)
		if ep.OutsideDir == 0 {
			m.nav().PlaceCursorFromClick(ep.VpRow, ep.DisplayCol)
		}
	}
	return m, nil
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch m.dragGeom().regionAt(msg.X, msg.Y) {
	case regionStatusBar:
		// The status bar doesn't scroll, so a wheel over it does nothing —
		// it must not reach the pane underneath.
		return m, nil
	case regionSidebar:
		// Scroll sidebar view without changing selection
		if msg.Button == tea.MouseWheelUp {
			m.sidebar.ScrollUp()
		} else {
			m.sidebar.ScrollDown()
		}
	case regionMainPane:
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
		return m, m.nav().ForwardKey(msg)
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.focus == SidebarFocus {
		// "Load more" in commit mode triggers pagination
		if m.mode == CommitsMode && strings.HasPrefix(m.sidebar.SelectedItem(), "load more") {
			return m, m.loadMoreCommitsCmd()
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
			return m, expandIgnoredDirCmd(m.git, dir)
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

// displayedFilesModeFile returns the file the main pane is currently
// showing, or "" when the pane is not showing a files-mode file at all.
//
// This is the target for every main-pane action, and it is deliberately not
// the sidebar selection: updateFilesModeContent early-returns on a directory
// selection so the previously-shown file stays on screen (maincontent.go),
// which leaves the pane and the sidebar legitimately disagreeing. The mode
// check matters too — switching into files mode while a directory is
// selected takes that same early return, so the key can still name the
// previous mode's item while its content sits in the pane.
func displayedFilesModeFile(key mainItemKey) string {
	if key.mode != FilesMode {
		return ""
	}
	return key.item
}

func (m *Model) openEditor() tea.Cmd {
	// Acts on what the pane is showing, per PROMPT.md's `confirm` row: "main
	// pane (files mode): open $EDITOR at the line currently at the top of
	// the viewport" — the viewport's file, at the viewport's line. Reading
	// the sidebar here opened $EDITOR on a directory; guarding on the
	// sidebar instead made Enter dead while a real file filled the pane.
	file := displayedFilesModeFile(m.lastMainItem)
	if file == "" {
		return nil
	}

	editor, args := m.buildEditorCmd(file)
	cmd := m.interactiveFactory(editor, args...)
	cmd.SetDir(m.dir)
	return tea.Exec(cmd, func(err error) tea.Msg {
		return RefreshMsg{}
	})
}

// openPRItemURL opens the URL for the currently selected PR sidebar item in the browser.
// Handles: PR description, comments, reviews, and CI checks.
func (m *Model) openPRItemURL() tea.Cmd {
	url := prItemURL(m.sidebar.SelectedItem(), m.prInfo, m.prComments, m.prReviews, m.ciChecks)
	if url == "" {
		return nil
	}
	return m.openInBrowser(url)
}

// prItemURL resolves the sidebar item label a user activated to the URL it
// points at.
func prItemURL(selected string, pr gitpkg.PRInfoResult, comments []gitpkg.PRComment, reviews []gitpkg.PRReview, checks []gitpkg.CICheck) string {
	if selected == "" {
		return ""
	}
	if selected == "Description" {
		return pr.URL
	}
	if ok, i := matchPRComment(selected, comments); ok {
		return comments[i].URL
	}
	if ok, i := matchPRReview(selected, reviews); ok {
		return reviews[i].URL
	}
	if ok, i := matchCICheck(selected, checks); ok {
		return checks[i].URL
	}
	return ""
}

// openInBrowser opens a URL in the default system browser.
func (m *Model) openInBrowser(url string) tea.Cmd {
	var cmd command.Command
	switch runtime.GOOS {
	case "darwin":
		cmd = m.interactiveFactory("open", url)
	case "linux":
		cmd = m.interactiveFactory("xdg-open", url)
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

// currentLineNumber finds the source line at the viewport top, mapping
// through the gutter/removed-line formatting and word wrapping that make
// the viewport row differ from the file line. Feeds `$EDITOR +N`, so it
// must be the file's line number, not a screen row. Only meaningful in
// files mode — callers in other modes should gate accordingly.
func (m *Model) currentLineNumber() int {
	return m.mainPane.viewportToSourceLine()
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
	if line, ok := nextHunkStart(m.mainPane.diffHunks, 0, +1); ok {
		m.nav().JumpToHunkStart(line)
	}
}

func (m *Model) jumpToNextDiff(direction int) {
	if line, ok := nextHunkStart(m.mainPane.diffHunks, hunkNavAnchor(m.cursor, m.mainPane), direction); ok {
		m.nav().JumpToHunkStart(line)
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
	// Mode switch dismisses any active visual-mode selection.
	m.selection.Cancel()
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
		// Switching to a new item dismisses any active visual-mode
		// selection — selection is per-view.
		m.selection.Cancel()
		if line, ok := m.viewMemory.RecallMainScroll(key); ok {
			m.nav().ScrollToSourceLine(line)
			return
		}
		if key.mode == FilesMode && len(m.mainPane.diffHunks) > 0 {
			// jumpToFirstDiff scrolls *and* places the cursor on the hunk.
			m.jumpToFirstDiff()
			return
		}
		// New item with no hunks and no scroll memory: cursor at top. Goes
		// through the seam so the viewport follows — the fallback used to
		// leave the cursor at row 0 while the viewport kept the previous
		// item's offset.
		m.nav().PlaceCursorAt(Position{SourceLine: 1, Column: 0})
	}

	if m.scope.OldBase() == "" && m.git != nil {
		return // preserve current main panel content (and lastMainItem)
	}

	// The per-mode builders install new content, which re-derives the
	// row↔source mapping the cursor's canonical vpRow is expressed in.
	// Reflow re-derives the cursor across that — unless a builder's setItem
	// placed it deliberately, in which case that placement wins.
	m.nav().Reflow(func(*mainPane) {
		if m.git == nil {
			m.updateNonGitFilesMode(setItem)
			return
		}
		switch m.mode {
		case FilesMode:
			m.updateFilesModeContent(setItem)
		case CommitsMode:
			m.updateCommitsModeContent(setItem)
		case PRMode:
			m.updatePRModeContent(setItem)
		}
	})

	// Every main-pane content swap funnels through the builders above, and the
	// search overlay's matches are line indices into that content — so they are
	// stale the moment it changes. Re-run the query against what is now
	// displayed. Outside Reflow: this only reads the pane.
	m.search.RecomputeMatches(m.nav())
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
	statusBarHeight := m.statusBarLines()
	sidebarW, mainW, contentH := layoutDimensions(m.width, m.height, statusBarHeight, m.sidebarPct, m.sidebarHidden)
	m.sidebar.SetSize(sidebarW, contentH)
	// SetSize re-wraps content at the new width, which moves every row —
	// go through the seam so the cursor keeps pointing at the same source
	// position (only its display position changes).
	m.nav().Reflow(func(p *mainPane) { p.SetSize(mainW, contentH) })
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

	bar, labels, l2Labels, l3Labels := renderStatusBar(m.width, m.statusBarData())
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
	if m.notifications.Text() != "" && !m.search.IsActive() {
		lines := strings.Split(padded, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = sidebarDimStyle.Render(m.notifications.Text())
			padded = strings.Join(lines, "\n")
			padded = padToHeight(padded, m.width, m.height)
		}
	}

	// Apply drag selection highlighting
	if m.drag.IsActive() && m.drag.HasRange() {
		padded = m.drag.ApplyHighlight(padded, m.dragGeom())
	}

	// Visual mode selection highlight — shares the renderer with drag
	// but lives on m.selection (anchor/active are Position values, not
	// click endpoints).
	if !m.drag.IsActive() && m.selection.HasRange() {
		padded = m.selection.ApplyHighlight(padded, m.dragGeom())
	}

	// Cursor: paint a single reverse-video cell at the cursor position.
	// Skipped while drag or visual-mode selection is active — both
	// already paint a reverse-video region covering the cursor cell,
	// and overlapping the two looks weird in most terminals.
	if !m.drag.IsActive() && !m.selection.IsActive() && m.focus == MainFocus {
		padded = m.cursor.ApplyHighlight(padded, m.dragGeom())
	}

	v.SetContent(padded)
	return v
}

func (m *Model) renderPRDescription() string {
	return renderPRDescription(m.prInfo, m.prReviews, m.prDeployments, m.mainPane.width)
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
