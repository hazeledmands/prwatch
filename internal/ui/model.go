package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

const prRefreshInterval = 1 * time.Minute

type Mode int

const (
	FileDiffMode Mode = iota
	FileViewMode
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
	FileDiffCommitted(base, file string) (string, error)
	FileDiffUncommitted(file string) (string, error)
	FileContent(file string) (string, error)
	CommitPatch(sha string) (string, error)
}

type Model struct {
	git              GitDataSource
	mode             Mode
	focus            Focus
	width            int
	height           int
	base             string
	repoInfo         gitpkg.RepoInfoResult
	prInfo           gitpkg.PRInfoResult
	ciStatus         gitpkg.CIStatusResult
	prReviews        []gitpkg.PRReview
	prCommentCount   int
	committedFiles   []string
	uncommittedFiles []string
	commits          []gitpkg.Commit
	sidebar          *sidebar
	mainPane         *mainPane
	dir              string
	confirming       bool
	lastKeyG         bool // tracks whether last key was 'g' for gg binding
	showHelp         bool
	searching        bool
	searchQuery      string
	err              error
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
	commits          []gitpkg.Commit
	err              error
}

type RefreshMsg struct{}

type prRefreshMsg struct {
	prInfo       gitpkg.PRInfoResult
	ciStatus     gitpkg.CIStatusResult
	reviews      []gitpkg.PRReview
	commentCount int
}

type prTickMsg struct{}

func NewModel(dir string, g GitDataSource) *Model {
	mode := FileDiffMode
	if g == nil {
		mode = FileViewMode
	}
	return &Model{
		git:      g,
		dir:      dir,
		mode:     mode,
		focus:    SidebarFocus,
		sidebar:  newSidebar(),
		mainPane: newMainPane(),
	}
}

func (m *Model) Init() tea.Cmd {
	if m.git == nil {
		return m.loadNonGitFiles
	}
	return tea.Batch(m.loadGitData, schedulePRTick())
}

func schedulePRTick() tea.Cmd {
	return tea.Tick(prRefreshInterval, func(t time.Time) tea.Msg {
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
	prInfo, _ := m.git.PRInfo()
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

	commits, err := m.git.Commits(base)
	if err != nil {
		return gitDataMsg{err: err}
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
		m.commits = msg.commits
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case prRefreshMsg:
		m.prInfo = msg.prInfo
		m.ciStatus = msg.ciStatus
		m.prReviews = msg.reviews
		m.prCommentCount = msg.commentCount
		return m, nil

	case prTickMsg:
		if m.git == nil {
			return m, schedulePRTick()
		}
		return m, tea.Batch(m.loadPRStatus, schedulePRTick())

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
		return m.handleMouseWheel(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Search input mode
	if m.searching {
		return m.handleSearchKey(msg)
	}

	// Help overlay — any key dismisses
	if m.showHelp {
		if key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.Help) {
			m.showHelp = false
			return m, nil
		}
		m.showHelp = false
		return m, nil
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
		case FileDiffMode:
			m.mode = FileViewMode
		case FileViewMode:
			m.mode = CommitMode
		case CommitMode:
			m.mode = FileDiffMode
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
		m.focus = SidebarFocus
		return m, nil

	case key.Matches(msg, keys.FocusRight):
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
	}

	// Forward unhandled keys to main pane when it has focus (scrolling, half-page, etc.)
	if m.focus == MainFocus {
		cmd := m.mainPane.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.QuitImmediate):
		m.searching = false
		m.searchQuery = ""
		return m, nil
	case msg.Code == tea.KeyEscape:
		m.searching = false
		m.searchQuery = ""
		return m, nil
	case msg.Code == tea.KeyEnter:
		m.searching = false
		m.executeSearch()
		return m, nil
	case msg.Code == tea.KeyBackspace:
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
		return m, nil
	default:
		if msg.Text != "" {
			m.searchQuery += msg.Text
		}
		return m, nil
	}
}

func (m *Model) executeSearch() {
	if m.searchQuery == "" {
		return
	}
	m.mainPane.SearchAndHighlight(m.searchQuery)
}

func (m *Model) sidebarPixelWidth() int {
	// sidebar width + 2 for border
	return m.sidebar.width + 2
}

func (m *Model) handleStatusBarClick(x, y int) (tea.Model, tea.Cmd) {
	if y == 0 && m.git != nil {
		// Line 0: check if click is on the right side (git status summary area)
		// Right third of the bar roughly corresponds to the status summary
		rightThird := m.width * 2 / 3
		if x >= rightThird {
			// Determine if clicking "uncommitted" or "commits" based on position
			// Simple heuristic: uncommitted info comes before commits info
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
		return m.handleStatusBarClick(x, y)
	}

	// Adjust y for the 2-line status bar
	contentY := y - 2
	sidebarW := m.sidebarPixelWidth()
	if x < sidebarW {
		// Clicked in sidebar
		m.focus = SidebarFocus
		// Content starts after status bar (2 lines) + top border (1 line) = row 3
		itemIdx := contentY - 1 + m.sidebar.offset
		m.sidebar.SelectIndex(itemIdx)
		m.updateMainContent()
	} else {
		m.focus = MainFocus
	}
	return m, nil
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	x := msg.X
	sidebarW := m.sidebarPixelWidth()

	if x < sidebarW {
		// Scroll sidebar
		if msg.Button == tea.MouseWheelUp {
			m.sidebar.SelectPrev()
		} else {
			m.sidebar.SelectNext()
		}
		m.updateMainContent()
	} else {
		// Forward to main pane viewport
		cmd := m.mainPane.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.focus == SidebarFocus {
		m.focus = MainFocus
		return m, nil
	}

	// Main pane focused
	if m.mode == FileDiffMode || m.mode == FileViewMode {
		return m, m.openEditor()
	}
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

func (m *Model) isUncommittedFile(file string) bool {
	for _, f := range m.uncommittedFiles {
		if f == file {
			return true
		}
	}
	return false
}

func (m *Model) updateSidebarItems() {
	switch m.mode {
	case FileDiffMode, FileViewMode:
		var items []sidebarItem
		// Uncommitted files first (dimmed), then separator, then committed
		for _, f := range m.uncommittedFiles {
			items = append(items, sidebarItem{label: f, kind: itemDim})
		}
		if len(m.uncommittedFiles) > 0 && len(m.committedFiles) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		for _, f := range m.committedFiles {
			items = append(items, sidebarItem{label: f, kind: itemNormal})
		}
		m.sidebar.SetItems(items)
	case CommitMode:
		items := make([]sidebarItem, len(m.commits))
		for i, c := range m.commits {
			items[i] = sidebarItem{
				label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				kind:  itemNormal,
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
		if file == "" {
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
		m.mainPane.SetContent(diff)

	case FileViewMode:
		file := m.sidebar.SelectedItem()
		if file == "" {
			m.mainPane.SetPlainContent("")
			return
		}
		content, err := m.git.FileContent(file)
		if err != nil {
			m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
			return
		}
		m.mainPane.SetPlainContent(content)

	case CommitMode:
		idx := m.sidebar.SelectedIndex()
		if idx >= len(m.commits) {
			m.mainPane.SetContent("")
			return
		}
		patch, err := m.git.CommitPatch(m.commits[idx].SHA)
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			return
		}
		m.mainPane.SetContent(patch)
	}
}

func (m *Model) updateLayout() {
	statusBarHeight := 2                                // line 1: branch/mode/status, line 2: PR info
	contentHeight := max(0, m.height-statusBarHeight-2) // borders
	sidebarWidth := max(0, m.width*3/10)
	mainWidth := max(0, m.width-sidebarWidth-4) // borders

	m.sidebar.SetSize(sidebarWidth, contentHeight)
	m.mainPane.SetSize(mainWidth, contentHeight)
}

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	if m.err != nil {
		v.SetContent(fmt.Sprintf("Error: %v\nPress q to quit.\n", m.err))
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
	sidebarView := m.sidebar.View(m.focus == SidebarFocus)
	mainView := m.mainPane.View(m.focus == MainFocus)

	content := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, mainView)

	result := bar + "\n" + content

	if m.searching {
		searchBar := fmt.Sprintf("/%s_", m.searchQuery)
		result += "\n" + searchBar
	}

	if m.showHelp {
		result = bar + "\n" + m.renderHelp()
	}

	v.SetContent(result)
	return v
}

func (m *Model) renderHelp() string {
	help := []string{
		"Keybindings:",
		"",
		"  [space]      Cycle mode (diff -> file -> commit)",
		"  [d]          File diff mode",
		"  [f] [v]      File view mode",
		"  [c]          Commit mode",
		"",
		"  [h] [left]   Focus sidebar",
		"  [l] [right]  Focus main pane",
		"  [tab]        Toggle focus",
		"",
		"  [j] [down]   Move down / scroll down",
		"  [k] [up]     Move up / scroll up",
		"  [pgup/pgdn]  Page up / page down",
		"  [gg]         Go to top",
		"  [G]          Go to bottom",
		"",
		"  [enter]      Open file in $EDITOR / switch to main pane",
		"  [/]          Search",
		"  [?]          Show this help",
		"",
		"  [q] [esc]    Quit (confirm)",
		"  [Q] [ctrl-c] Quit immediately",
		"",
		"Press any key to dismiss.",
	}
	return strings.Join(help, "\n")
}
