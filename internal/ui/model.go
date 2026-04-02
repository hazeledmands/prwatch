package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	runewidth "github.com/mattn/go-runewidth"
)

// ansiStripRE matches ANSI escape sequences (SGR and OSC 8 hyperlinks).
var ansiStripRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\]8;;[^\x1b]*\x1b\\`)

const (
	prRefreshDefault = 1 * time.Minute
	prRefreshMax     = 10 * time.Minute
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
)

type Focus int

const (
	SidebarFocus Focus = iota
	MainFocus
)

// GitDataSource provides the git operations needed by the UI model.
// Implemented by *git.Git; mockable for testing.
type GitDataSource interface {
	RepoInfo() (gitpkg.RepoInfoResult, error)
	PRInfo() (gitpkg.PRInfoResult, error)
	PRChecks() (gitpkg.CIStatusResult, error)
	PRReviews() ([]gitpkg.PRReview, error)
	PRCommentCount() (int, error)
	DetectBase() (string, error)
	ChangedFiles(base string) (gitpkg.ChangedFilesResult, error)
	Commits(base string) ([]gitpkg.Commit, error)
	AllCommits() ([]gitpkg.Commit, error)
	FileDiffCommitted(base, file string) (string, error)
	FileDiffUncommitted(file string) (string, error)
	FileContent(file string) (string, error)
	CommitPatch(sha string) (string, error)
	AllFiles(includeIgnored bool) ([]string, error)
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
	prCommentCount      int
	committedFiles      []string
	uncommittedFiles    []string
	deletedFiles        []string        // files deleted in base..HEAD
	allFiles            []string        // all files in the repo (for file-view mode)
	ignoredFiles        map[string]bool // gitignored files (for dimming in all-files view)
	commits             []gitpkg.Commit
	lastViewedFile      string // track the last file shown in file-view for auto-jump
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
	dragStartX          int           // drag start position (-1 = not dragging)
	dragStartY          int
	dragEndX            int
	dragEndY            int
	dragging            bool
	loading             bool // true until first data load completes
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
	err              error
}

type RefreshMsg struct{}

type prRefreshMsg struct {
	prInfo       gitpkg.PRInfoResult
	ciStatus     gitpkg.CIStatusResult
	reviews      []gitpkg.PRReview
	commentCount int
	rateLimited  bool
}

type prTickMsg struct{}

func NewModel(dir string, g GitDataSource) *Model {
	mode := FileViewMode
	if g == nil {
		mode = FileViewMode
	}
	return &Model{
		git:           g,
		dir:           dir,
		mode:          mode,
		focus:         SidebarFocus,
		sidebar:       newSidebar(),
		mainPane:      newMainPane(),
		sidebarPct:    30, // default 30% of width
		showIgnored:   true,
		treeMode:      true,
		collapsedDirs: make(map[string]bool),
		wordWrap:      true,
		lineNumbers:   true,
		prInterval:    prRefreshDefault,
		loading:       g != nil,
		dragStartX:    -1,
		dragStartY:    -1,
	}
}

func (m *Model) Init() tea.Cmd {
	if m.git == nil {
		return m.loadNonGitFiles
	}
	return tea.Batch(m.loadGitData, schedulePRTick(m.prInterval))
}

func schedulePRTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return prTickMsg{}
	})
}

func (m *Model) loadNonGitFiles() tea.Msg {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return gitDataMsg{err: err}
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			files = append(files, e.Name())
		}
	}
	return gitDataMsg{
		uncommittedFiles: files,
	}
}

func (m *Model) loadPRStatus() tea.Msg {
	prInfo, err := m.git.PRInfo()
	if err != nil && isRateLimited(err) {
		return prRefreshMsg{rateLimited: true}
	}
	var ciStatus gitpkg.CIStatusResult
	var reviews []gitpkg.PRReview
	var commentCount int
	if prInfo.Number > 0 {
		ciStatus, _ = m.git.PRChecks()
		reviews, _ = m.git.PRReviews()
		commentCount, _ = m.git.PRCommentCount()
	}
	return prRefreshMsg{
		prInfo:       prInfo,
		ciStatus:     ciStatus,
		reviews:      reviews,
		commentCount: commentCount,
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

	prInfo, _ := m.git.PRInfo()

	// Fetch PR details if a PR exists
	var ciStatus gitpkg.CIStatusResult
	var reviews []gitpkg.PRReview
	var commentCount int
	if prInfo.Number > 0 {
		ciStatus, _ = m.git.PRChecks()
		reviews, _ = m.git.PRReviews()
		commentCount, _ = m.git.PRCommentCount()
	}

	base, err := m.git.DetectBase()
	if err != nil {
		return gitDataMsg{err: err}
	}

	files, err := m.git.ChangedFiles(base)
	if err != nil {
		return gitDataMsg{err: err}
	}

	// Spec: on the base branch or detached HEAD, show full commit history
	var commits []gitpkg.Commit
	if info.IsDetachedHead || info.Branch == "main" || info.Branch == "master" {
		commits, err = m.git.AllCommits()
	} else {
		commits, err = m.git.Commits(base)
	}
	if err != nil {
		return gitDataMsg{err: err}
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
		prReviews:        reviews,
		prCommentCount:   commentCount,
		base:             base,
		committedFiles:   files.Committed,
		uncommittedFiles: files.Uncommitted,
		deletedFiles:     files.Deleted,
		allFiles:         allFiles,
		ignoredFiles:     ignoredSet,
		commits:          commits,
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case gitDataMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.repoInfo = msg.repoInfo
		m.prInfo = msg.prInfo
		m.ciStatus = msg.ciStatus
		m.prReviews = msg.prReviews
		m.prCommentCount = msg.prCommentCount
		m.base = msg.base
		m.committedFiles = msg.committedFiles
		m.uncommittedFiles = msg.uncommittedFiles
		m.deletedFiles = msg.deletedFiles
		m.allFiles = msg.allFiles
		m.ignoredFiles = msg.ignoredFiles
		m.commits = msg.commits
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
			return m, nil
		}
		// Successful fetch — reset to default interval
		m.prInterval = prRefreshDefault
		m.prInfo = msg.prInfo
		m.ciStatus = msg.ciStatus
		m.prReviews = msg.reviews
		m.prCommentCount = msg.commentCount
		return m, nil

	case prTickMsg:
		if m.git == nil {
			return m, schedulePRTick(m.prInterval)
		}
		return m, tea.Batch(m.loadPRStatus, schedulePRTick(m.prInterval))

	case RefreshMsg:
		if m.git == nil {
			return m, m.loadNonGitFiles
		}
		return m, m.loadGitData

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseWheelMsg:
		if m.showHelp {
			helpLines := m.helpContentLines()
			visibleHeight := max(1, m.height-4)
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
		if !m.sidebarHidden && msg.X < sidebarW && msg.Y >= 2 {
			contentY := msg.Y - 2
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
	visibleHeight := max(1, m.height-4) // status bar + borders

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

func (m *Model) sidebarPixelWidth() int {
	// sidebar width + 2 for border
	return m.sidebar.width + 2
}

func (m *Model) handleStatusBarClick(x, y int) (tea.Model, tea.Cmd) {
	if y == 0 && m.git != nil {
		// Check if click is on the mode indicator (center region of the bar)
		leftThird := m.width / 3
		rightThird := m.width * 2 / 3
		if x >= leftThird && x < rightThird {
			// Cycle mode like [m] key
			switch m.mode {
			case FileViewMode:
				m.mode = FileDiffMode
			case FileDiffMode:
				m.mode = CommitMode
			case CommitMode:
				m.mode = FileViewMode
			}
			m.updateSidebarItems()
			m.updateMainContent()
			return m, nil
		}

		// Right third: git status summary area
		if x >= rightThird {
			midRight := rightThird + (m.width-rightThird)/2
			if x < midRight && len(m.uncommittedFiles) > 0 {
				m.mode = FileDiffMode
				m.updateSidebarItems()
				m.updateMainContent()
			} else if len(m.commits) > 0 {
				m.mode = CommitMode
				m.updateSidebarItems()
				m.updateMainContent()
			}
		}
	}
	return m, nil
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y

	// Status bar is rows 0-1
	if y <= 1 {
		m.dragging = false
		return m.handleStatusBarClick(x, y)
	}

	// Adjust y for the 2-line status bar
	contentY := y - 2
	sidebarW := m.sidebarPixelWidth()
	if !m.sidebarHidden && x < sidebarW {
		// Clicked in sidebar — no drag tracking
		m.dragging = false
		m.focus = SidebarFocus
		// Content starts after status bar (2 lines) + top border (1 line) = row 3
		itemIdx := contentY - 1 + m.sidebar.offset
		m.sidebar.SelectIndex(itemIdx)
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
		if !m.wordWrap && (msg.Button == tea.MouseWheelLeft || msg.Button == tea.MouseWheelRight) {
			if msg.Button == tea.MouseWheelLeft {
				m.mainPane.ScrollLeft(4)
			} else {
				m.mainPane.ScrollRight(4)
			}
			return m, nil
		}
		// Vertical scrolling — forward to main pane viewport
		cmd := m.mainPane.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.focus == SidebarFocus {
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
func (m *Model) jumpToFirstDiff() {
	diffLines := m.mainPane.DiffLineNumbers()
	if len(diffLines) > 0 {
		m.mainPane.ScrollToLine(diffLines[0] - 1)
	}
}

// jumpToNextDiff scrolls the main pane to the next (direction=1) or previous
// (direction=-1) diff hunk. Wraps around.
func (m *Model) jumpToNextDiff(direction int) {
	diffLines := m.mainPane.DiffLineNumbers()
	if len(diffLines) == 0 {
		return
	}

	currentLine := m.mainPane.ScrollTop() + 1
	if direction > 0 {
		// Find next diff line after current
		for _, l := range diffLines {
			if l > currentLine {
				m.mainPane.ScrollToLine(l - 1) // 0-indexed
				return
			}
		}
		// Wrap around to first
		m.mainPane.ScrollToLine(diffLines[0] - 1)
	} else {
		// Find previous diff line before current
		for i := len(diffLines) - 1; i >= 0; i-- {
			if diffLines[i] < currentLine {
				m.mainPane.ScrollToLine(diffLines[i] - 1)
				return
			}
		}
		// Wrap around to last
		m.mainPane.ScrollToLine(diffLines[len(diffLines)-1] - 1)
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
			items = append(items, buildTreeItems(m.uncommittedFiles, itemDim, m.collapsedDirs)...)
			if len(m.uncommittedFiles) > 0 && len(m.committedFiles) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, buildTreeItems(m.committedFiles, itemNormal, m.collapsedDirs, func(f string) sidebarItemKind { return m.fileItemKind(f, itemNormal) })...)
		} else {
			for _, f := range m.uncommittedFiles {
				items = append(items, sidebarItem{label: f, filePath: f, kind: itemDim})
			}
			if len(m.uncommittedFiles) > 0 && len(m.committedFiles) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			for _, f := range m.committedFiles {
				items = append(items, sidebarItem{label: f, filePath: f, kind: m.fileItemKind(f, itemNormal)})
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
			items = append(items, buildTreeItems(m.uncommittedFiles, itemDim, m.collapsedDirs)...)
			if len(m.uncommittedFiles) > 0 && len(m.committedFiles) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			items = append(items, buildTreeItems(m.committedFiles, itemNormal, m.collapsedDirs, func(f string) sidebarItemKind { return m.fileItemKind(f, itemNormal) })...)
			if len(otherFiles) > 0 && (len(m.uncommittedFiles) > 0 || len(m.committedFiles) > 0) {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
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
		} else {
			for _, f := range m.uncommittedFiles {
				items = append(items, sidebarItem{label: f, filePath: f, kind: itemDim})
			}
			if len(m.uncommittedFiles) > 0 && len(m.committedFiles) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			for _, f := range m.committedFiles {
				items = append(items, sidebarItem{label: f, filePath: f, kind: m.fileItemKind(f, itemNormal)})
			}
			if len(otherFiles) > 0 && (len(m.uncommittedFiles) > 0 || len(m.committedFiles) > 0) {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			for _, f := range otherFiles {
				kind := m.fileItemKind(f, itemNormal)
				if kind == itemNormal && m.ignoredFiles[f] {
					kind = itemDim
				}
				items = append(items, sidebarItem{label: f, filePath: f, kind: kind})
			}
		}
		m.sidebar.SetItems(items)
	case CommitMode:
		var items []sidebarItem
		unpushed := m.repoInfo.AheadCount

		// Category 1: Uncommitted changes (if any)
		if len(m.uncommittedFiles) > 0 {
			label := fmt.Sprintf("uncommitted changes (%d files)", len(m.uncommittedFiles))
			items = append(items, sidebarItem{label: label, kind: itemDim})
		}

		// Category 2: Unpushed commits (dimmed)
		if unpushed > 0 {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			for i := 0; i < unpushed && i < len(m.commits); i++ {
				c := m.commits[i]
				items = append(items, sidebarItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
					kind:  itemDim,
				})
			}
		}

		// Category 3: Pushed branch commits
		if unpushed < len(m.commits) {
			if len(items) > 0 {
				items = append(items, sidebarItem{kind: itemSeparator})
			}
			for i := unpushed; i < len(m.commits); i++ {
				c := m.commits[i]
				items = append(items, sidebarItem{
					label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
					kind:  itemNormal,
				})
			}
		}

		m.sidebar.SetItems(items)
	}
}

func (m *Model) updateMainContent() {
	if m.git == nil {
		// Non-git: file-view only, read from disk
		if m.mode == FileViewMode {
			file := m.sidebar.SelectedItem()
			if file == "" {
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
			m.mainPane.SetDiffAnnotations(parseDiffAnnotations(diff))
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
		// Check if this is the "uncommitted changes" entry
		if strings.HasPrefix(selected, "uncommitted changes") {
			// Show combined diff of all uncommitted files
			var diffs []string
			for _, f := range m.uncommittedFiles {
				d, err := m.git.FileDiffUncommitted(f)
				if err == nil && d != "" {
					diffs = append(diffs, d)
				}
			}
			m.mainPane.SetContent(strings.Join(diffs, "\n"))
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
	}
}

func (m *Model) updateLayout() {
	statusBarHeight := 2                                // line 1: branch/mode/status, line 2: PR info
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

	bar := renderStatusBar(m.width, statusBarData{
		info:          m.repoInfo,
		pr:            m.prInfo,
		ciStatus:      m.ciStatus,
		reviews:       m.prReviews,
		commentCount:  m.prCommentCount,
		mode:          m.mode,
		confirming:    m.confirming,
		uncommitCount: len(m.uncommittedFiles),
		commitCount:   len(m.commits),
	})

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

	// Clamp to main pane area
	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	if startX < sidebarW {
		startX = sidebarW
	}
	if endX >= m.width {
		endX = m.width - 1
	}

	lines := strings.Split(content, "\n")
	selectStyle := lipgloss.NewStyle().Reverse(true)

	for y := startY; y <= endY && y < len(lines); y++ {
		stripped := stripANSIForWidth(lines[y])
		fromX := sidebarW
		toX := len(stripped)
		if y == startY {
			fromX = startX
		}
		if y == endY {
			toX = endX + 1
		}
		if fromX >= len(stripped) {
			continue
		}
		if toX > len(stripped) {
			toX = len(stripped)
		}
		if fromX >= toX {
			continue
		}
		before := stripped[:fromX]
		selected := stripped[fromX:toX]
		after := stripped[toX:]
		lines[y] = before + selectStyle.Render(selected) + after
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

// stripANSIForWidth removes ANSI escape sequences for width calculation.
func stripANSIForWidth(s string) string {
	return ansiStripRE.ReplaceAllString(s, "")
}

// displayWidthOf returns the display width of a string, accounting for
// wide characters (CJK, emoji) and tab stops.
func displayWidthOf(s string) int {
	return runewidth.StringWidth(s)
}

// copySelection extracts text from the main pane's content (stripping ANSI,
// gutter, and TUI glyphs) and copies to the system clipboard.
// Coordinates are screen-relative; we convert to main-pane-content-relative.
func (m *Model) copySelection() {
	if m.dragStartX == m.dragEndX && m.dragStartY == m.dragEndY {
		return // No actual drag
	}

	// Main pane content area starts after:
	// - 2 rows of status bar
	// - 1 row of top border
	// And the x offset is sidebarPixelWidth() + 1 (left border of main pane)
	statusRows := 2
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

	var selected strings.Builder
	for y := startY; y <= endY && y < len(contentLines); y++ {
		// Strip ANSI codes to get clean text
		line := stripANSIForWidth(contentLines[y])
		line = strings.TrimRight(line, " ") // remove trailing padding

		fromX := 0
		toX := len(line)
		if y == startY {
			fromX = startX
		}
		if y == endY {
			toX = endX + 1
		}
		if fromX > len(line) {
			fromX = len(line)
		}
		if toX > len(line) {
			toX = len(line)
		}
		if fromX < toX {
			selected.WriteString(line[fromX:toX])
		}
		if y < endY {
			selected.WriteString("\n")
		}
	}

	text := selected.String()
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

func (m *Model) helpContentLines() []string {
	return []string{
		"Keybindings:",
		"",
		"  [m]          Cycle mode (file -> diff -> commit)",
		"  [v] [1]      File view mode",
		"  [d] [2]      File diff mode",
		"  [c] [3]      Commit mode",
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
		"  [J]          Jump to next diff hunk (file view)",
		"  [K]          Jump to previous diff hunk (file view)",
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
	visibleHeight := max(1, m.height-4) // status bar + borders

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
