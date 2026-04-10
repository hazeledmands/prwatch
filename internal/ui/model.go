package ui

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	runewidth "github.com/mattn/go-runewidth"
)

// ansiStripRE matches ANSI escape sequences (SGR and OSC 8 hyperlinks).
var ansiStripRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\]8;;[^\x1b]*\x1b\\`)

const (
	prRefreshActive  = 30 * time.Second // refresh interval when user is active
	prRefreshIdle    = 10 * time.Minute // refresh interval when idle
	prRefreshMax     = 15 * time.Minute // max backoff on rate limit
	prIdleThreshold  = 10 * time.Minute // no UI events for this long = idle
	prStaleThreshold = 24 * time.Hour   // no server changes for this long = stale
	gitPollInterval  = 5 * time.Second
)

type searchMatch struct {
	pane string // "sidebar" or "main"
	line int    // line index in the respective pane
}

type Mode int

const (
	FileViewMode Mode = iota
	FileDiffMode
	CommitMode
	PRViewMode
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
	DetectBase() (string, error)
	ChangedFiles(base string) (gitpkg.ChangedFilesResult, error)
	Commits(base string, skip, limit int) ([]gitpkg.Commit, error)
	AllCommits(skip, limit int) ([]gitpkg.Commit, error)
	CommitCount() (int, error)
	CommitCountRange(base string) (int, error)
	FileDiffCommitted(base, file string) (string, error)
	FileDiffUncommitted(file string) (string, error)
	FileContent(file string) (string, error)
	CommitPatch(sha string) (string, error)
	AllFiles(includeIgnored bool) ([]string, error)
	BaseCommits(base string, limit int) ([]gitpkg.Commit, error)
	BehindCount(baseRef string) int
	RWXResults(runID string) (*gitpkg.RWXResult, error)
	RWXTaskLog(taskID string) (string, error)
	RWXTestResults(taskID string) ([]gitpkg.RWXFailedTest, error)
}

type Model struct {
	git                 GitDataSource
	mode                Mode
	focus               Focus
	width               int
	height              int
	base                string
	repoInfo            gitpkg.RepoInfoResult
	prInfo              gitpkg.PRInfoResult
	ciStatus            gitpkg.CIStatusResult
	prReviews           []gitpkg.PRReview
	prReviewRequests    []gitpkg.PRReviewRequest
	prError             string // error message for PR/GitHub API issues
	prCommentCount      int
	committedFiles      []string
	uncommittedFiles    []string
	deletedFiles        []string        // files deleted in base..HEAD
	allFiles            []string        // all files in the repo (for file-view mode)
	ignoredFiles        map[string]bool // gitignored files (for dimming in all-files view)
	commits             []gitpkg.Commit
	commitCount         int                   // true total commit count (from rev-list --count)
	commitsLoaded       int                   // how many commits have been loaded so far
	behindCount         int                   // how many commits behind base
	baseCommits         []gitpkg.Commit       // commits from the base branch (for commit mode category 4)
	prComments          []gitpkg.PRComment    // PR comments for PR-view mode
	prDeployments       []gitpkg.PRDeployment // PR deployments for PR-view mode
	ciChecks            []gitpkg.CICheck      // CI checks for PR-view mode
	pendingRWXCheck     *gitpkg.CICheck       // CI check awaiting RWX log fetch
	rwxLogCache         map[string]string     // cache of RWX logs by check URL
	lastViewedFile      string                // track the last file shown in file-view for auto-jump
	sidebar             *sidebar
	mainPane            *mainPane
	sidebarPct          int // sidebar width as percentage of total width (10-50)
	dir                 string
	confirming          bool
	lastKeyG            bool // tracks whether last key was 'g' for gg binding
	showHelp            bool
	helpScrollOffset    int             // scroll offset within help overlay
	helpSearching       bool            // search active within help
	helpSearchConfirmed bool            // help search confirmed, n/p navigation
	helpSearchQuery     string          // search query within help
	helpSearchMatches   []int           // line indices of matches in help
	helpSearchIdx       int             // current match index
	showIgnored         bool            // whether to show gitignored files in all-files section
	treeMode            bool            // [t] toggles tree view in file modes
	collapsedDirs       map[string]bool // tracks collapsed directory paths
	sidebarHidden       bool            // [f] toggles sidebar visibility
	wordWrap            bool            // [w] toggles word wrapping in main pane
	lineNumbers         bool            // [n] toggles line numbers in file-view mode
	searching           bool            // search input is active
	searchConfirmed     bool            // enter pressed, n/p navigation active
	searchQuery         string
	searchMatches       []searchMatch // matches across both panes
	searchMatchIdx      int           // current match index
	hoverX, hoverY      int           // last mouse position for hover highlighting
	prInterval          time.Duration // adaptive PR refresh interval
	lastUIEvent         time.Time     // last user interaction (keys, mouse, resize)
	lastServerChange    time.Time     // last time server data actually changed
	dragStartX          int           // drag start position (-1 = not dragging)
	dragStartY          int
	dragEndX            int
	dragEndY            int
	dragging            bool
	loading             bool         // true until first data load completes
	modeLabels          []modeLabel  // clickable mode label positions from last render
	line3Labels         []line3Label // clickable positions on PR status line
	err                 error
}

// Messages
type gitDataMsg struct {
	repoInfo         gitpkg.RepoInfoResult
	prInfo           gitpkg.PRInfoResult
	ciStatus         gitpkg.CIStatusResult
	prReviews        []gitpkg.PRReview
	prCommentCount   int
	base             string
	committedFiles   []string
	uncommittedFiles []string
	deletedFiles     []string
	allFiles         []string
	ignoredFiles     map[string]bool
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

// maybeFetchRWXLog returns a tea.Cmd to fetch RWX logs if there's a pending check.
func (m *Model) maybeFetchRWXLog() tea.Cmd {
	if m.pendingRWXCheck == nil || m.git == nil {
		return nil
	}
	check := *m.pendingRWXCheck
	m.pendingRWXCheck = nil
	return func() tea.Msg {
		runID := gitpkg.ExtractRWXRunID(check.URL)
		if runID == "" {
			return rwxLogMsg{checkURL: check.URL, err: fmt.Errorf("could not extract run ID from URL")}
		}

		// First get the results to find failed tasks
		results, err := m.git.RWXResults(runID)
		if err != nil {
			return rwxLogMsg{checkURL: check.URL, err: err}
		}

		var content strings.Builder
		content.WriteString(fmt.Sprintf("RWX Run: %s\nStatus: %s\n", runID, results.Status))

		if len(results.FailedTasks) > 0 {
			content.WriteString(fmt.Sprintf("\nFailed tasks: %d\n", len(results.FailedTasks)))
			for _, task := range results.FailedTasks {
				content.WriteString(fmt.Sprintf("\n--- %s ---\n\n", task.Key))

				// Try test-results artifacts first for structured failure output
				if task.HasArtifacts {
					failedTests, err := m.git.RWXTestResults(task.TaskID)
					if err == nil && len(failedTests) > 0 {
						for _, ft := range failedTests {
							content.WriteString(fmt.Sprintf("FAIL: %s (%s)\n\n", ft.Name, ft.Scope))
							if ft.Stdout != "" {
								content.WriteString(ft.Stdout)
								content.WriteString("\n")
							}
						}
						continue
					}
				}

				// Fall back to raw logs
				log, err := m.git.RWXTaskLog(task.TaskID)
				if err != nil {
					content.WriteString(fmt.Sprintf("Error fetching log: %v\n", err))
				} else {
					content.WriteString(log)
				}
			}
		} else {
			content.WriteString("\nNo failed tasks.")
		}

		return rwxLogMsg{checkURL: check.URL, log: content.String()}
	}
}

type rwxLogMsg struct {
	checkURL string
	log      string
	err      error
}

func NewModel(dir string, g GitDataSource) *Model {
	mode := FileViewMode
	if g == nil {
		mode = FileViewMode
	}
	return &Model{
		git:              g,
		dir:              dir,
		mode:             mode,
		focus:            SidebarFocus,
		sidebar:          newSidebar(),
		mainPane:         newMainPane(),
		sidebarPct:       30, // default 30% of width
		showIgnored:      true,
		treeMode:         true,
		collapsedDirs:    make(map[string]bool),
		rwxLogCache:      make(map[string]string),
		wordWrap:         true,
		lineNumbers:      true,
		prInterval:       prRefreshActive,
		lastUIEvent:      time.Now(),
		lastServerChange: time.Now(),
		loading:          g != nil,
		dragStartX:       -1,
		dragStartY:       -1,
	}
}

func (m *Model) Init() tea.Cmd {
	if m.git == nil {
		return m.loadNonGitFiles
	}
	return tea.Batch(m.loadGitData, schedulePRTick(m.prInterval), scheduleGitTick())
}

func schedulePRTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return prTickMsg{}
	})
}

func scheduleGitTick() tea.Cmd {
	return tea.Tick(gitPollInterval, func(t time.Time) tea.Msg {
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

// relativeTime returns a short human-readable relative timestamp like "2h ago" or "3d ago".
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// matchNumberedItem checks if selected matches any item's expected label (built by labelFn).
// Returns (true, index) on match, (false, 0) otherwise.
func matchNumberedItem[T any](selected string, items []T, labelFn func(int, T) string) (bool, int) {
	for i, item := range items {
		if selected == labelFn(i, item) {
			return true, i
		}
	}
	return false, 0
}

// sortPRData sorts comments, reviews, and CI checks for display.
// Comments and reviews: most recent first. CI checks: failures first, then pending, then passing.
func (m *Model) sortPRData() {
	slices.SortFunc(m.prComments, func(a, b gitpkg.PRComment) int {
		return b.CreatedAt.Compare(a.CreatedAt) // descending
	})
	slices.SortFunc(m.prReviews, func(a, b gitpkg.PRReview) int {
		return b.SubmittedAt.Compare(a.SubmittedAt) // descending
	})
	slices.SortStableFunc(m.ciChecks, func(a, b gitpkg.CICheck) int {
		return cmp.Compare(ciBucketOrder(a.Bucket), ciBucketOrder(b.Bucket))
	})
}

// ciBucketOrder returns a sort key for CI check buckets: failures first, then pending, then passing.
func ciBucketOrder(bucket string) int {
	switch bucket {
	case "fail", "cancel":
		return 0
	case "pending":
		return 1
	case "pass":
		return 2
	case "skipping":
		return 3
	default:
		return 4
	}
}

type allFilesMsg struct {
	files []string
}

func (m *Model) reloadAllFiles() tea.Msg {
	files, _ := m.git.AllFiles(m.showIgnored)
	return allFilesMsg{files: files}
}

func (m *Model) loadGitData() tea.Msg {
	info, err := m.git.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
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

	base, err := m.git.DetectBase()
	if err != nil {
		return gitDataMsg{err: err}
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

	// Fetch all files for file-view mode sidebar
	allFiles, _ := m.git.AllFiles(m.showIgnored)

	// Compute ignored files set
	var ignoredSet map[string]bool
	if m.showIgnored {
		nonIgnored, _ := m.git.AllFiles(false)
		nonIgnoredSet := make(map[string]bool, len(nonIgnored))
		for _, f := range nonIgnored {
			nonIgnoredSet[f] = true
		}
		ignoredSet = make(map[string]bool)
		for _, f := range allFiles {
			if !nonIgnoredSet[f] {
				ignoredSet[f] = true
			}
		}
	}

	return gitDataMsg{
		repoInfo:         info,
		prInfo:           prInfo,
		ciStatus:         ciStatus,
		prReviews:        prAll.Reviews,
		prCommentCount:   prAll.CommentCount,
		base:             base,
		committedFiles:   files.Committed,
		uncommittedFiles: files.Uncommitted,
		deletedFiles:     files.Deleted,
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
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

// loadLocalGitData refreshes only local git state (no GitHub API calls).
// Existing PR data in the model is preserved via prFetchFailed.
func (m *Model) loadLocalGitData() tea.Msg {
	info, err := m.git.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
	}

	base, err := m.git.DetectBase()
	if err != nil {
		return gitDataMsg{err: err}
	}

	files, err := m.git.ChangedFiles(base)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Preserve pagination: reload at least as many commits as the user has already seen
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

	var behindCount int
	if !info.IsDetachedHead && info.Branch != "main" && info.Branch != "master" {
		baseRef := "origin/main"
		if m.prInfo.BaseRef != "" {
			baseRef = "origin/" + m.prInfo.BaseRef
		} else if info.Upstream != "" {
			baseRef = info.Upstream
		}
		behindCount = m.git.BehindCount(baseRef)
	}

	var baseCommits []gitpkg.Commit
	if !info.IsDetachedHead && info.Branch != "main" && info.Branch != "master" {
		baseCommits, _ = m.git.BaseCommits(base, 50)
	}

	allFiles, _ := m.git.AllFiles(m.showIgnored)

	var ignoredSet map[string]bool
	if m.showIgnored {
		nonIgnored, _ := m.git.AllFiles(false)
		nonIgnoredSet := make(map[string]bool, len(nonIgnored))
		for _, f := range nonIgnored {
			nonIgnoredSet[f] = true
		}
		ignoredSet = make(map[string]bool)
		for _, f := range allFiles {
			if !nonIgnoredSet[f] {
				ignoredSet[f] = true
			}
		}
	}

	return gitDataMsg{
		repoInfo:         info,
		base:             base,
		committedFiles:   files.Committed,
		uncommittedFiles: files.Uncommitted,
		deletedFiles:     files.Deleted,
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
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
	var commits []gitpkg.Commit
	var err error
	if info.IsDetachedHead || info.Branch == "main" || info.Branch == "master" {
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
		m.lastUIEvent = time.Now()
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
		m.base = msg.base
		m.committedFiles = msg.committedFiles
		m.uncommittedFiles = msg.uncommittedFiles
		m.deletedFiles = msg.deletedFiles
		m.allFiles = msg.allFiles
		m.ignoredFiles = msg.ignoredFiles
		m.commits = msg.commits
		m.commitCount = msg.commitCount
		m.commitsLoaded = len(msg.commits)
		m.baseCommits = msg.baseCommits
		m.behindCount = msg.behindCount
		// On first load, default to PR mode if a PR exists and mode hasn't been changed
		if wasLoading && m.prInfo.Number > 0 && m.mode == FileViewMode {
			m.mode = PRViewMode
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

	case prRefreshMsg:
		if msg.rateLimited {
			// Double the interval on rate limit, up to max
			m.prInterval = min(m.prInterval*2, prRefreshMax)
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
			m.lastServerChange = time.Now()
		}
		// Successful fetch — reset interval based on activity and clear error
		m.prInterval = m.computePRInterval()
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
		m.updateLayout()
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case rwxLogMsg:
		m.rwxLogCache[msg.checkURL] = msg.log
		if msg.err != nil {
			m.rwxLogCache[msg.checkURL] = fmt.Sprintf("Error fetching RWX logs: %v", msg.err)
		}
		m.pendingRWXCheck = nil
		m.updateMainContent()
		return m, nil

	case prTickMsg:
		// Recompute interval on each tick based on current activity state
		m.prInterval = m.computePRInterval()
		if m.git == nil {
			return m, schedulePRTick(m.prInterval)
		}
		return m, tea.Batch(m.loadPRStatus, schedulePRTick(m.prInterval))

	case gitTickMsg:
		if m.git == nil {
			return m, scheduleGitTick()
		}
		return m, tea.Batch(m.loadLocalGitData, scheduleGitTick())

	case RefreshMsg:
		if m.git == nil {
			return m, m.loadNonGitFiles
		}
		// Use local-only refresh (no GitHub API calls). File watcher
		// events fire frequently and should not hit the network.
		// Full PR data is refreshed on the PR tick cycle instead.
		return m, m.loadLocalGitData

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
		if m.dragging {
			m.dragEndX = msg.X
			m.dragEndY = msg.Y
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
		return m, nil

	case tea.MouseReleaseMsg:
		if m.dragging {
			m.dragging = false
			m.dragEndX = msg.X
			m.dragEndY = msg.Y
			m.copySelection()
		}
		return m, nil
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
		if key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.QuitImmediate) {
			return m, tea.Quit
		}
		m.confirming = false
		return m, nil
	}

	// Handle gg (go to top) — second g in sequence
	if m.lastKeyG && key.Matches(msg, keys.GoTop) {
		m.lastKeyG = false
		if m.focus == SidebarFocus {
			m.sidebar.SelectFirst()
			m.updateMainContent()
		} else {
			m.mainPane.GoToTop()
		}
		return m, nil
	}
	m.lastKeyG = false

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
			return m, nil // non-git: file-view only
		}
		switch m.mode {
		case FileViewMode:
			m.mode = FileDiffMode
		case FileDiffMode:
			m.mode = CommitMode
		case CommitMode:
			if m.prInfo.Number > 0 {
				m.mode = PRViewMode
			} else {
				m.mode = FileViewMode
			}
		case PRViewMode:
			m.mode = FileViewMode
		}
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.FileDiffMode):
		if m.git == nil {
			return m, nil
		}
		m.mode = FileDiffMode
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.FileViewMode):
		m.mode = FileViewMode
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.CommitMode):
		if m.git == nil {
			return m, nil
		}
		m.mode = CommitMode
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.PRMode):
		if m.git == nil || m.prInfo.Number == 0 {
			return m, nil
		}
		m.mode = PRViewMode
		m.updateSidebarItems()
		m.updateMainContent()
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
		// First 'g' — wait for second
		m.lastKeyG = true
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
		if m.mode == FileViewMode {
			m.showIgnored = !m.showIgnored
			return m, m.reloadAllFiles
		}
		return m, nil

	case key.Matches(msg, keys.ToggleTree):
		if m.mode == FileDiffMode || m.mode == FileViewMode {
			m.treeMode = !m.treeMode
			m.updateSidebarItems()
		}
		return m, nil

	case key.Matches(msg, keys.NextLeaf):
		m.jumpToNextLeaf(1)
		return m, nil

	case key.Matches(msg, keys.PrevLeaf):
		m.jumpToNextLeaf(-1)
		return m, nil

	case key.Matches(msg, keys.Refresh):
		if m.git == nil {
			return m, m.loadNonGitFiles
		}
		return m, m.loadGitData

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
		if m.mode == FileViewMode {
			m.mainPane.ToggleShowRemoved()
		}
		return m, nil

	case key.Matches(msg, keys.NextDiff):
		if m.mode == FileViewMode {
			m.jumpToNextDiff(1)
		}
		return m, nil

	case key.Matches(msg, keys.PrevDiff):
		if m.mode == FileViewMode {
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
	return statusBarLineCount(statusBarData{info: m.repoInfo, pr: m.prInfo})
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
					m.mode = label.mode
					m.updateSidebarItems()
					m.updateMainContent()
				}
				return m, nil
			}
		}
	case 1:
		// Line 2: local git status — clicking commits area switches to commit mode
		if len(m.commits) > 0 {
			m.mode = CommitMode
			m.updateSidebarItems()
			m.updateMainContent()
		}
	case 2:
		// Line 3: PR status — click on specific elements
		if m.prInfo.Number > 0 {
			for _, label := range m.line3Labels {
				if x >= label.start && x < label.end {
					m.mode = PRViewMode
					m.updateSidebarItems()
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
		m.sidebar.SelectIndex(itemIdx)
		// "Load more" in commit mode triggers pagination
		if m.mode == CommitMode && strings.HasPrefix(m.sidebar.SelectedItem(), "load more") {
			return m, m.loadMoreCommits
		}
		// If a directory was clicked in tree mode, toggle collapse
		if m.treeMode && m.sidebar.SelectedIsDir() {
			dir := m.sidebar.SelectedItem()
			m.collapsedDirs[dir] = !m.collapsedDirs[dir]
			m.updateSidebarItems()
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
		if m.mode == CommitMode && strings.HasPrefix(m.sidebar.SelectedItem(), "load more") {
			return m, m.loadMoreCommits
		}
		// Enter behaves like Right in tree mode
		return m.handleSidebarRight()
	}

	// Main pane focused
	if m.mode == FileDiffMode || m.mode == FileViewMode {
		return m, m.openEditor()
	}
	return m, nil
}

// handleSidebarLeft handles left/h key when sidebar is focused.
// Tree mode: collapse directory or go to parent. Non-tree: no-op.
func (m *Model) handleSidebarLeft() (tea.Model, tea.Cmd) {
	if !m.treeMode {
		return m, nil
	}

	if m.sidebar.SelectedIsDir() {
		// Collapse the directory if it's open
		dir := m.sidebar.SelectedItem()
		if !m.collapsedDirs[dir] {
			m.collapsedDirs[dir] = true
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
// Tree mode: expand directory or go to first child. Leaf: switch to main.
// Non-tree: switch to main pane.
func (m *Model) handleSidebarRight() (tea.Model, tea.Cmd) {
	if !m.treeMode {
		m.focus = MainFocus
		return m, nil
	}

	if m.sidebar.SelectedIsDir() {
		dir := m.sidebar.SelectedItem()
		if m.collapsedDirs[dir] {
			// Expand the directory
			m.collapsedDirs[dir] = false
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
	cmd := exec.Command(editor, args...)
	cmd.Dir = m.dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
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

// currentLineNumber finds the source line at the viewport top.
// In file-view mode, it's just the scroll offset + 1.
// In file-diff mode, it parses diff hunks to find the real line number.
func (m *Model) currentLineNumber() int {
	if m.mode == FileViewMode {
		return m.mainPane.ScrollTop() + 1
	}

	lines := strings.Split(m.mainPane.content, "\n")
	scrollTop := m.mainPane.ScrollTop()

	currentLine := 1
	for i := 0; i <= scrollTop && i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "@@") {
			if n := parseHunkNewStart(line); n > 0 {
				currentLine = n
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			currentLine++
		} else if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "\\") &&
			!strings.HasPrefix(line, "diff") && !strings.HasPrefix(line, "index") &&
			!strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "+++") {
			currentLine++
		}
	}
	return currentLine
}

func parseHunkNewStart(hunkLine string) int {
	plusIdx := strings.Index(hunkLine, "+")
	if plusIdx < 0 {
		return 0
	}
	rest := hunkLine[plusIdx+1:]
	commaIdx := strings.IndexAny(rest, ", ")
	if commaIdx < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:commaIdx])
	if err != nil {
		return 0
	}
	return n
}

// isBinaryContent checks if content appears to be binary by looking for null bytes
// or a high ratio of non-printable characters.
func isBinaryContent(content string) bool {
	if len(content) == 0 {
		return false
	}
	// Check first 8KB for null bytes or high ratio of non-text characters
	sample := content
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	nonPrintable := 0
	for _, b := range []byte(sample) {
		if b == 0 {
			return true
		}
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			nonPrintable++
		}
	}
	// If more than 10% non-printable, consider it binary
	return len(sample) > 0 && nonPrintable*10 > len(sample)
}

func (m *Model) isUncommittedFile(file string) bool {
	for _, f := range m.uncommittedFiles {
		if f == file {
			return true
		}
	}
	return false
}

// jumpToFirstDiff scrolls to the first diff line in the current file.
// selectFirstComment selects the first comment in the PR mode sidebar.
func (m *Model) selectFirstComment() {
	for i, item := range m.sidebar.items {
		if strings.HasPrefix(item.label, "@") {
			m.sidebar.SelectIndex(i)
			return
		}
	}
}

// selectFirstReview selects the first review in the PR mode sidebar.
func (m *Model) selectFirstReview() {
	for i, item := range m.sidebar.items {
		if strings.HasPrefix(item.label, "@") && (strings.Contains(item.prefix, "✓") ||
			strings.Contains(item.prefix, "✗") ||
			strings.Contains(item.prefix, "c ") ||
			strings.Contains(item.prefix, "…")) {
			m.sidebar.SelectIndex(i)
			return
		}
	}
}

// selectFirstCIFailure selects the first failing CI check in the PR mode sidebar.
// If no failures, selects the first CI check item.
func (m *Model) selectFirstCIFailure() {
	// Find the first failure in ciChecks
	targetName := ""
	for _, check := range m.ciChecks {
		if check.Bucket == "fail" || check.Bucket == "cancel" {
			targetName = check.Name
			break
		}
	}
	// If no failure, use the first CI check
	if targetName == "" && len(m.ciChecks) > 0 {
		targetName = m.ciChecks[0].Name
	}
	if targetName == "" {
		return
	}
	// Find the sidebar item matching this CI check
	for i, item := range m.sidebar.items {
		if strings.Contains(item.label, targetName) {
			m.sidebar.SelectIndex(i)
			return
		}
	}
}

func (m *Model) jumpToFirstDiff() {
	diffLines := m.mainPane.DiffLineNumbers()
	if len(diffLines) > 0 {
		m.mainPane.ScrollToSourceLine(diffLines[0])
	}
}

// jumpToNextDiff scrolls the main pane to the next (direction=1) or previous
// (direction=-1) diff hunk. Wraps around.
func (m *Model) jumpToNextDiff(direction int) {
	diffLines := m.mainPane.DiffLineNumbers()
	if len(diffLines) == 0 {
		return
	}

	currentLine := m.mainPane.ViewportToSourceLine()
	if direction > 0 {
		// Find next diff line after current
		for _, l := range diffLines {
			if l > currentLine {
				m.mainPane.ScrollToSourceLine(l)
				return
			}
		}
		// Wrap around to first
		m.mainPane.ScrollToSourceLine(diffLines[0])
	} else {
		// Find previous diff line before current
		for i := len(diffLines) - 1; i >= 0; i-- {
			if diffLines[i] < currentLine {
				m.mainPane.ScrollToSourceLine(diffLines[i])
				return
			}
		}
		// Wrap around to last
		m.mainPane.ScrollToSourceLine(diffLines[len(diffLines)-1])
	}
}

// jumpToNextLeaf moves sidebar selection to the next (direction=1) or previous
// (direction=-1) non-directory, non-separator item. Works from any focus.
func (m *Model) jumpToNextLeaf(direction int) {
	start := m.sidebar.SelectedIndex()
	items := m.sidebar.items
	n := len(items)
	if n == 0 {
		return
	}
	for i := 1; i < n; i++ {
		idx := (start + i*direction + n) % n
		if items[idx].kind != itemSeparator && !items[idx].isDir {
			m.sidebar.SelectIndex(idx)
			m.updateMainContent()
			return
		}
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

// extractDirs returns the unique directory paths from a list of file paths.
func extractDirs(files []string) []string {
	dirs := make(map[string]bool)
	for _, f := range files {
		parts := strings.Split(f, "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
	}
	var result []string
	for d := range dirs {
		result = append(result, d)
	}
	return result
}

func (m *Model) isDeletedFile(file string) bool {
	for _, f := range m.deletedFiles {
		if f == file {
			return true
		}
	}
	return false
}

func (m *Model) fileItemKind(file string, defaultKind sidebarItemKind) sidebarItemKind {
	if m.isDeletedFile(file) {
		return itemDeleted
	}
	return defaultKind
}

func (m *Model) isCommittedFile(file string) bool {
	for _, f := range m.committedFiles {
		if f == file {
			return true
		}
	}
	return false
}

func (m *Model) updateSidebarItems() {
	switch m.mode {
	case FileDiffMode:
		var items []sidebarItem
		if m.treeMode {
			// Auto-collapse hidden (dot-prefixed) directories by default
			allDirs := extractDirs(m.uncommittedFiles)
			allDirs = append(allDirs, extractDirs(m.committedFiles)...)
			for _, d := range allDirs {
				if _, exists := m.collapsedDirs[d]; !exists {
					base := d
					if i := strings.LastIndex(d, "/"); i >= 0 {
						base = d[i+1:]
					}
					if strings.HasPrefix(base, ".") {
						m.collapsedDirs[d] = true
					}
				}
			}
			if len(m.uncommittedFiles) > 0 {
				items = append(items, sidebarItem{label: fmt.Sprintf("Uncommitted (%d)", len(m.uncommittedFiles)), kind: itemHeader})
				items = append(items, buildTreeItems(m.uncommittedFiles, itemNormal, m.collapsedDirs)...)
			}
			if len(m.committedFiles) > 0 {
				if len(m.uncommittedFiles) > 0 {
					items = append(items, sidebarItem{kind: itemSeparator})
				}
				items = append(items, sidebarItem{label: fmt.Sprintf("Committed (%d)", len(m.committedFiles)), kind: itemHeader})
				items = append(items, buildTreeItems(m.committedFiles, itemNormal, m.collapsedDirs, func(f string) sidebarItemKind { return m.fileItemKind(f, itemNormal) })...)
			}
		} else {
			if len(m.uncommittedFiles) > 0 {
				items = append(items, sidebarItem{label: fmt.Sprintf("Uncommitted (%d)", len(m.uncommittedFiles)), kind: itemHeader})
				for _, f := range m.uncommittedFiles {
					items = append(items, sidebarItem{label: f, filePath: f, kind: itemNormal})
				}
			}
			if len(m.committedFiles) > 0 {
				if len(m.uncommittedFiles) > 0 {
					items = append(items, sidebarItem{kind: itemSeparator})
				}
				items = append(items, sidebarItem{label: fmt.Sprintf("Committed (%d)", len(m.committedFiles)), kind: itemHeader})
				for _, f := range m.committedFiles {
					items = append(items, sidebarItem{label: f, filePath: f, kind: m.fileItemKind(f, itemNormal)})
				}
			}
		}
		m.sidebar.SetItems(items)

	case FileViewMode:
		var items []sidebarItem
		// Compute other files (not in committed or uncommitted)
		changedSet := make(map[string]bool)
		for _, f := range m.uncommittedFiles {
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

		if m.treeMode {
			// Auto-collapse directories by default
			dotDirs := extractDirs(m.uncommittedFiles)
			dotDirs = append(dotDirs, extractDirs(m.committedFiles)...)
			for _, d := range dotDirs {
				if _, exists := m.collapsedDirs[d]; !exists {
					if m.git == nil {
						// Non-git: collapse all dirs by default (no concept of "changed" files)
						m.collapsedDirs[d] = true
					} else {
						// Git: only auto-collapse hidden (dot-prefixed) directories
						base := d
						if i := strings.LastIndex(d, "/"); i >= 0 {
							base = d[i+1:]
						}
						if strings.HasPrefix(base, ".") {
							m.collapsedDirs[d] = true
						}
					}
				}
			}
			if len(m.uncommittedFiles) > 0 {
				items = append(items, sidebarItem{label: fmt.Sprintf("Uncommitted (%d)", len(m.uncommittedFiles)), kind: itemHeader})
				items = append(items, buildTreeItems(m.uncommittedFiles, itemNormal, m.collapsedDirs)...)
			}
			if len(m.committedFiles) > 0 {
				if len(m.uncommittedFiles) > 0 {
					items = append(items, sidebarItem{kind: itemSeparator})
				}
				items = append(items, sidebarItem{label: fmt.Sprintf("Committed (%d)", len(m.committedFiles)), kind: itemHeader})
				items = append(items, buildTreeItems(m.committedFiles, itemNormal, m.collapsedDirs, func(f string) sidebarItemKind { return m.fileItemKind(f, itemNormal) })...)
			}
			if len(otherFiles) > 0 {
				if len(m.uncommittedFiles) > 0 || len(m.committedFiles) > 0 {
					items = append(items, sidebarItem{kind: itemSeparator})
				}
				items = append(items, sidebarItem{label: fmt.Sprintf("All Files (%d)", len(otherFiles)), kind: itemHeader})
				// All-files trees default to collapsed (spec: "trees should start out closed")
				// Use collapsedDirs but auto-collapse dirs not already tracked
				allFilesDirs := extractDirs(otherFiles)
				for _, d := range allFilesDirs {
					if _, exists := m.collapsedDirs[d]; !exists {
						m.collapsedDirs[d] = true // default closed for all-files
					}
				}
				items = append(items, buildTreeItems(otherFiles, itemNormal, m.collapsedDirs, func(f string) sidebarItemKind {
					if m.ignoredFiles[f] {
						return itemDim
					}
					return itemNormal
				})...)
			}
		} else {
			if len(m.uncommittedFiles) > 0 {
				items = append(items, sidebarItem{label: fmt.Sprintf("Uncommitted (%d)", len(m.uncommittedFiles)), kind: itemHeader})
				for _, f := range m.uncommittedFiles {
					items = append(items, sidebarItem{label: f, filePath: f, kind: itemNormal})
				}
			}
			if len(m.committedFiles) > 0 {
				if len(m.uncommittedFiles) > 0 {
					items = append(items, sidebarItem{kind: itemSeparator})
				}
				items = append(items, sidebarItem{label: fmt.Sprintf("Committed (%d)", len(m.committedFiles)), kind: itemHeader})
				for _, f := range m.committedFiles {
					items = append(items, sidebarItem{label: f, filePath: f, kind: m.fileItemKind(f, itemNormal)})
				}
			}
			if len(otherFiles) > 0 {
				if len(m.uncommittedFiles) > 0 || len(m.committedFiles) > 0 {
					items = append(items, sidebarItem{kind: itemSeparator})
				}
				items = append(items, sidebarItem{label: fmt.Sprintf("All Files (%d)", len(otherFiles)), kind: itemHeader})
				for _, f := range otherFiles {
					kind := m.fileItemKind(f, itemNormal)
					if kind == itemNormal && m.ignoredFiles[f] {
						kind = itemDim
					}
					items = append(items, sidebarItem{label: f, filePath: f, kind: kind})
				}
			}
		}
		m.sidebar.SetItems(items)
	case CommitMode:
		var items []sidebarItem
		unpushed := m.repoInfo.AheadCount
		pushedCount := len(m.commits) - unpushed
		if pushedCount < 0 {
			pushedCount = 0
		}

		// Category 1: Uncommitted changes (if any)
		if len(m.uncommittedFiles) > 0 {
			items = append(items, sidebarItem{label: fmt.Sprintf("Uncommitted (%d files)", len(m.uncommittedFiles)), kind: itemHeader})
			items = append(items, sidebarItem{label: "uncommitted changes", kind: itemDim})
		}

		// Category 2: Unpushed commits (dimmed)
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

		// Category 3: Pushed branch commits
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

		// Category 4: Base branch commits (already in base, before the feature branch)
		if len(m.baseCommits) > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
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

	case PRViewMode:
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

func (m *Model) updateMainContent() {
	if m.git == nil {
		// Non-git: file-view only, read from disk
		if m.mode == FileViewMode {
			file := m.sidebar.SelectedItem()
			if file == "" || m.sidebar.SelectedIsDir() {
				m.mainPane.SetPlainContent("")
				return
			}
			content, err := os.ReadFile(filepath.Join(m.dir, file))
			if err != nil {
				m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
				return
			}
			if isBinaryContent(string(content)) {
				m.mainPane.SetPlainContent("[binary content]")
				return
			}
			m.mainPane.SetPlainContent(string(content))
		}
		return
	}
	if m.base == "" {
		return
	}

	switch m.mode {
	case FileDiffMode:
		file := m.sidebar.SelectedItem()
		if file == "" || m.sidebar.SelectedIsDir() {
			m.mainPane.SetContent("")
			return
		}
		var diff string
		var err error
		if m.isUncommittedFile(file) {
			diff, err = m.git.FileDiffUncommitted(file)
		} else {
			diff, err = m.git.FileDiffCommitted(m.base, file)
		}
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			return
		}
		if isBinaryContent(diff) {
			m.mainPane.SetPlainContent("[binary content]")
			return
		}
		m.mainPane.SetContent(diff)

	case FileViewMode:
		file := m.sidebar.SelectedItem()
		if file == "" || m.sidebar.SelectedIsDir() {
			m.mainPane.SetPlainContent("")
			m.mainPane.ClearDiffAnnotations()
			return
		}
		content, err := m.git.FileContent(file)
		if err != nil {
			m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
			m.mainPane.ClearDiffAnnotations()
			return
		}
		if isBinaryContent(content) {
			m.mainPane.SetPlainContent("[binary content]")
			m.mainPane.ClearDiffAnnotations()
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
		} else {
			m.mainPane.ClearDiffAnnotations()
		}
		m.mainPane.SetPlainContent(content)
		// Auto-jump to first diff only when the file changes
		if file != m.lastViewedFile {
			m.lastViewedFile = file
			m.jumpToFirstDiff()
		}

	case CommitMode:
		selected := m.sidebar.SelectedItem()
		if selected == "" {
			m.mainPane.SetContent("")
			return
		}
		// Check if this is the "load more" entry
		if strings.HasPrefix(selected, "load more") {
			m.mainPane.SetPlainContent("Loading more commits...")
			return
		}
		// Check if this is the "uncommitted changes" entry
		if strings.HasPrefix(selected, "uncommitted changes") {
			// Show combined diff of all uncommitted files in a single git call
			diff, _ := m.git.FileDiffUncommitted("")
			m.mainPane.SetContent(diff)
			return
		}
		// Otherwise it's a commit — extract SHA from "abcdef0 subject"
		commitIdx := m.commitIndexFromSidebarItem(selected)
		if commitIdx < 0 || commitIdx >= len(m.commits) {
			m.mainPane.SetContent("")
			return
		}
		patch, err := m.git.CommitPatch(m.commits[commitIdx].SHA)
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			return
		}
		if isBinaryContent(patch) {
			m.mainPane.SetPlainContent("[binary content]")
			return
		}
		m.mainPane.SetContent(patch)

	case PRViewMode:
		selected := m.sidebar.SelectedItem()
		if selected == "Description" {
			m.mainPane.SetPlainContent(m.renderPRDescription())
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
		} else {
			// CI check — find the matching check
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
					if gitpkg.IsRWXURL(check.URL) {
						if cached, ok := m.rwxLogCache[check.URL]; ok {
							content += "\n\n" + cached
						} else {
							content += "\n\nLoading RWX logs..."
							m.pendingRWXCheck = &check
						}
					}
					m.mainPane.SetPlainContent(content)
					break
				}
			}
		}
	}
}

// computePRInterval returns the appropriate PR refresh interval based on
// user activity and server data freshness.
func (m *Model) computePRInterval() time.Duration {
	now := time.Now()
	idle := now.Sub(m.lastUIEvent) >= prIdleThreshold
	stale := now.Sub(m.lastServerChange) >= prStaleThreshold
	if idle || stale {
		return prRefreshIdle
	}
	return prRefreshActive
}

func (m *Model) updateLayout() {
	statusBarHeight := statusBarLineCount(statusBarData{
		info:    m.repoInfo,
		pr:      m.prInfo,
		prError: m.prError,
	})
	contentHeight := max(0, m.height-statusBarHeight-2) // borders

	if m.sidebarHidden {
		mainWidth := max(0, m.width-2) // just main pane borders
		m.sidebar.SetSize(0, contentHeight)
		m.mainPane.SetSize(mainWidth, contentHeight)
	} else {
		sidebarWidth := max(0, m.width*m.sidebarPct/100)
		mainWidth := max(0, m.width-sidebarWidth-4) // borders
		m.sidebar.SetSize(sidebarWidth, contentHeight)
		m.mainPane.SetSize(mainWidth, contentHeight)
	}
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

func (m *Model) View() tea.View {
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

	bar, labels, l3Labels := renderStatusBar(m.width, statusBarData{
		info:           m.repoInfo,
		pr:             m.prInfo,
		ciStatus:       m.ciStatus,
		reviews:        m.prReviews,
		reviewRequests: m.prReviewRequests,
		prError:        m.prError,
		commentCount:   m.prCommentCount,
		mode:           m.mode,
		confirming:     m.confirming,
		uncommitCount:  len(m.uncommittedFiles),
		commitCount:    m.commitCount,
		behindCount:    m.behindCount,
		showHelp:       m.showHelp,
		hoverX:         m.hoverX,
		hoverY:         m.hoverY,
	})
	m.modeLabels = labels
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
	// Content starts after sidebar + main pane border + gutter
	gutterOffset := sidebarW + 1 + m.mainPane.gutterWidth // +1 for border
	if startX < gutterOffset {
		startX = gutterOffset
	}
	if endX >= m.width {
		endX = m.width - 1
	}

	lines := strings.Split(content, "\n")

	for y := startY; y <= endY && y < len(lines); y++ {
		fromCol := gutterOffset
		toCol := displayWidthOf(stripANSIForWidth(lines[y]))
		if y == startY {
			fromCol = startX
		}
		if y == endY {
			toCol = endX + 1
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

// padToHeight ensures the output has exactly the target number of lines,
// padding with empty lines or truncating as needed. Each line is also padded
// to the target width.
func padToHeight(content string, width, height int) string {
	if height <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")

	// Truncate if too many lines
	if len(lines) > height {
		lines = lines[:height]
	}

	// Pad short lines to width and add missing lines
	emptyLine := strings.Repeat(" ", width)
	for i := range lines {
		stripped := stripANSIForWidth(lines[i])
		w := displayWidthOf(stripped)
		if w < width {
			lines[i] += strings.Repeat(" ", width-w)
		}
	}
	for len(lines) < height {
		lines = append(lines, emptyLine)
	}

	return strings.Join(lines, "\n")
}

// splitAtDisplayCols splits a line (which may contain ANSI escape codes) into
// three parts at display column boundaries: before fromCol, between fromCol
// and toCol, and after toCol. ANSI escape codes are preserved in whichever
// segment they fall in.
func splitAtDisplayCols(line string, fromCol, toCol int) (before, middle, after string) {
	col := 0
	fromByte := -1
	toByte := -1
	inEscape := false
	for i, r := range line {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			if fromByte < 0 && col >= fromCol {
				fromByte = i
			}
			if toByte < 0 && col >= toCol {
				toByte = i
			}
			inEscape = true
			continue
		}
		if fromByte < 0 && col >= fromCol {
			fromByte = i
		}
		if toByte < 0 && col >= toCol {
			toByte = i
		}
		col += runewidth.RuneWidth(r)
	}
	if fromByte < 0 {
		fromByte = len(line)
	}
	if toByte < 0 {
		toByte = len(line)
	}
	if fromByte > toByte {
		fromByte = toByte
	}
	return line[:fromByte], line[fromByte:toByte], line[toByte:]
}

// stripANSIForWidth removes ANSI escape sequences for width calculation.
func stripANSIForWidth(s string) string {
	return ansiStripRE.ReplaceAllString(s, "")
}

// displayWidthOf returns the display width of a string, accounting for
// wide characters (CJK, emoji) and tab stops.
func displayWidthOf(s string) int {
	return runewidth.StringWidth(s)
}

// sliceByDisplayCol extracts a substring from s between display columns
// [fromCol, toCol). This correctly handles multi-byte and double-width
// characters, avoiding mid-character byte slicing.
func sliceByDisplayCol(s string, fromCol, toCol int) string {
	if fromCol >= toCol {
		return ""
	}
	col := 0
	startByte := len(s)
	endByte := len(s)
	foundStart := false
	for i, r := range s {
		if !foundStart && col >= fromCol {
			startByte = i
			foundStart = true
		}
		w := runewidth.RuneWidth(r)
		col += w
		if foundStart && col >= toCol {
			endByte = i + utf8.RuneLen(r)
			return s[startByte:endByte]
		}
	}
	if !foundStart {
		return ""
	}
	return s[startByte:endByte]
}

// copySelection extracts text from the main pane's content (stripping ANSI,
// gutter, and TUI glyphs) and copies to the system clipboard.
// Coordinates are screen-relative; we convert to main-pane-content-relative.
// selectedText extracts the plain text from the current drag selection,
// stripping ANSI codes, gutter prefixes, and joining word-wrap continuations.
// Returns empty string if the drag start and end are the same point.
func (m *Model) selectedText() string {
	if m.dragStartX == m.dragEndX && m.dragStartY == m.dragEndY {
		return "" // No actual drag
	}

	// Main pane content area starts after:
	// - 3 rows of status bar
	// - 1 row of top border
	// And the x offset is sidebarPixelWidth() + 1 (left border of main pane)
	statusRows := m.statusBarLines()
	topBorder := 1
	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	mainLeftBorder := 1
	contentStartY := statusRows + topBorder
	contentStartX := sidebarW + mainLeftBorder

	// Get the main pane's raw content (pre-rendered, with ANSI)
	// Use the viewport's content which is what's displayed
	viewportContent := m.mainPane.viewport.View()
	contentLines := strings.Split(viewportContent, "\n")

	// Normalize drag coordinates
	startY, endY := m.dragStartY, m.dragEndY
	startX, endX := m.dragStartX, m.dragEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	// Convert screen coordinates to content-relative
	startY -= contentStartY
	endY -= contentStartY
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
	// The viewport may be scrolled, so viewport line 0 corresponds to
	// the viewport's scroll offset in the full content.
	vpOffset := m.mainPane.viewport.YOffset()
	var selected strings.Builder
	for y := startY; y <= endY && y < len(contentLines); y++ {
		// Strip ANSI codes to get clean text
		line := stripANSIForWidth(contentLines[y])
		line = strings.TrimRight(line, " ") // remove trailing padding

		// For continuation lines (word-wrapped), strip the indent prefix
		// For original lines, strip the gutter prefix
		absY := y + vpOffset
		isCont := contMap != nil && absY < len(contMap) && contMap[absY]
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
			nextAbsY := (y + 1) + vpOffset
			if contMap != nil && nextAbsY < len(contMap) && contMap[nextAbsY] {
				continue // join continuation lines without newline
			}
			selected.WriteString("\n")
		}
	}

	return selected.String()
}

func (m *Model) copySelection() {
	text := m.selectedText()
	if text == "" {
		return
	}

	// Copy to system clipboard
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	default:
		return
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Run() //nolint: ignore clipboard errors
}

// renderPRDescription builds the full PR description panel content:
// title, status, metadata, and markdown-rendered body.
func (m *Model) renderPRDescription() string {
	pr := m.prInfo
	var b strings.Builder

	// Title and status
	b.WriteString(fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title))
	if pr.IsDraft {
		b.WriteString(" [DRAFT]")
	}
	if pr.State == "MERGED" {
		b.WriteString(" [MERGED]")
	} else if pr.State == "CLOSED" {
		b.WriteString(" [CLOSED]")
	}
	b.WriteString("\n")

	// Dates
	if !pr.CreatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Created: %s (%s)\n", pr.CreatedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.CreatedAt)))
	}
	if !pr.UpdatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Updated: %s (%s)\n", pr.UpdatedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.UpdatedAt)))
	}
	if pr.State == "MERGED" && !pr.MergedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Merged: %s (%s)\n", pr.MergedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.MergedAt)))
	}
	if pr.State == "CLOSED" && !pr.ClosedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Closed: %s (%s)\n", pr.ClosedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.ClosedAt)))
	}

	// Labels
	if len(pr.Labels) > 0 {
		var names []string
		for _, l := range pr.Labels {
			names = append(names, l.Name)
		}
		b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(names, ", ")))
	}

	// Assignees
	if len(pr.Assignees) > 0 {
		var names []string
		for _, a := range pr.Assignees {
			names = append(names, "@"+a.Login)
		}
		b.WriteString(fmt.Sprintf("Assignees: %s\n", strings.Join(names, ", ")))
	}

	// Reviewers (from prReviews)
	if len(m.prReviews) > 0 {
		var items []string
		for _, r := range m.prReviews {
			status := ""
			switch r.State {
			case "APPROVED":
				status = "✓"
			case "CHANGES_REQUESTED":
				status = "✗"
			default:
				status = "…"
			}
			items = append(items, fmt.Sprintf("@%s %s", r.Author, status))
		}
		b.WriteString(fmt.Sprintf("Reviewers: %s\n", strings.Join(items, ", ")))
	}

	// Milestone
	if pr.Milestone.Title != "" {
		b.WriteString(fmt.Sprintf("Milestone: %s\n", pr.Milestone.Title))
	}

	// Deployments
	if len(m.prDeployments) > 0 {
		b.WriteString("\nDeployments:\n")
		for _, d := range m.prDeployments {
			line := fmt.Sprintf("  %s: %s", d.Environment, d.State)
			if d.URL != "" {
				line += fmt.Sprintf(" (%s)", d.URL)
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")

	// Render body as markdown
	if pr.Body != "" {
		rendered, err := renderMarkdown(pr.Body, m.mainPane.width)
		if err != nil {
			b.WriteString(pr.Body)
		} else {
			b.WriteString(rendered)
		}
	}

	return b.String()
}

func (m *Model) helpContentLines() []string {
	return []string{
		"Keybindings:",
		"",
		"  [m]          Cycle mode (file -> diff -> commit -> pr)",
		"  [v] [1]      File view mode",
		"  [d] [2]      File diff mode",
		"  [c] [3]      Commit mode",
		"  [b] [4]      PR view mode (when PR exists)",
		"",
		"  [h] [left]   Scroll left (when wrap off)",
		"  [l] [right]  Scroll right (when wrap off)",
		"  [,]          Focus sidebar",
		"  [.]          Focus main pane",
		"  [tab]        Toggle focus (sidebar / main pane)",
		"",
		"  [j] [down]   Move down / scroll down",
		"  [k] [up]     Move up / scroll up",
		"  [pgup/pgdn]  Page up / page down",
		"  [gg]         Go to top",
		"  [G]          Go to bottom",
		"",
		"  [+] [=]      Grow sidebar",
		"  [-]          Shrink sidebar",
		"  [f]          Toggle sidebar visibility",
		"",
		"  [w]          Toggle word wrap",
		"  [n]          Toggle line numbers (file view)",
		"  [i]          Toggle gitignored files (file view)",
		"  [t]          Toggle tree mode (file modes, default: on)",
		"  [D]          Toggle removed lines in diff gutter (file view)",
		"  [J] [S-down] Jump to next diff hunk (file view)",
		"  [K] [S-up]   Jump to previous diff hunk (file view)",
		"",
		"  [enter]      Open file in $EDITOR / switch to main pane",
		"  [/]          Search (type to match, enter to confirm)",
		"  [n]          Next search result (after search)",
		"  [p]          Previous search result (after search)",
		"  [?]          Show this help (scroll with j/k/mouse)",
		"",
		"  [q] [esc]    Quit (confirm)",
		"  [Q] [ctrl-c] Quit immediately",
		"",
		"Press q/esc to dismiss. Use j/k or mouse to scroll. / to search.",
	}
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
