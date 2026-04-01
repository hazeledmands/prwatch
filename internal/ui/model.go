package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

type Mode int

const (
	FileMode Mode = iota
	CommitMode
)

type Focus int

const (
	SidebarFocus Focus = iota
	MainFocus
)

type Model struct {
	git              *gitpkg.Git
	mode             Mode
	focus            Focus
	width            int
	height           int
	base             string
	repoInfo         gitpkg.RepoInfoResult
	prInfo           gitpkg.PRInfoResult
	committedFiles   []string
	uncommittedFiles []string
	commits          []gitpkg.Commit
	sidebar          *sidebar
	mainPane         *mainPane
	dir              string
	confirming       bool
	err              error
}

// Messages
type gitDataMsg struct {
	repoInfo         gitpkg.RepoInfoResult
	prInfo           gitpkg.PRInfoResult
	base             string
	committedFiles   []string
	uncommittedFiles []string
	commits          []gitpkg.Commit
	err              error
}

type RefreshMsg struct{}

func NewModel(dir string, g *gitpkg.Git) *Model {
	return &Model{
		git:      g,
		dir:      dir,
		mode:     FileMode,
		focus:    SidebarFocus,
		sidebar:  newSidebar(),
		mainPane: newMainPane(),
	}
}

func (m *Model) Init() tea.Cmd {
	return m.loadGitData
}

func (m *Model) loadGitData() tea.Msg {
	if m.git == nil {
		return gitDataMsg{err: fmt.Errorf("no git instance")}
	}

	info, err := m.git.RepoInfo()
	if err != nil {
		return gitDataMsg{err: err}
	}

	prInfo, _ := m.git.PRInfo()

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
		m.base = msg.base
		m.committedFiles = msg.committedFiles
		m.uncommittedFiles = msg.uncommittedFiles
		m.commits = msg.commits
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case RefreshMsg:
		return m, m.loadGitData

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Quit confirmation handling
	if m.confirming {
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

	case key.Matches(msg, keys.ToggleMode):
		if m.mode == FileMode {
			m.mode = CommitMode
		} else {
			m.mode = FileMode
		}
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.FileMode):
		m.mode = FileMode
		m.updateSidebarItems()
		m.updateMainContent()
		return m, nil

	case key.Matches(msg, keys.CommitMode):
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

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.focus == SidebarFocus {
		m.focus = MainFocus
		return m, nil
	}

	// Main pane focused
	if m.mode == FileMode {
		return m, m.openEditor()
	}
	return m, nil
}

func (m *Model) openEditor() tea.Cmd {
	file := m.sidebar.SelectedItem()
	if file == "" {
		return nil
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	line := m.currentLineNumber()
	args := []string{}
	if line > 0 {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, file)

	cmd := exec.Command(editor, args...)
	cmd.Dir = m.dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return RefreshMsg{}
	})
}

// currentLineNumber parses the diff to find the source line at the viewport top.
func (m *Model) currentLineNumber() int {
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

func (m *Model) updateSidebarItems() {
	switch m.mode {
	case FileMode:
		var items []sidebarItem
		for _, f := range m.committedFiles {
			items = append(items, sidebarItem{label: f, kind: itemNormal})
		}
		if len(m.committedFiles) > 0 && len(m.uncommittedFiles) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		for _, f := range m.uncommittedFiles {
			items = append(items, sidebarItem{label: f, kind: itemDim})
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
	if m.git == nil || m.base == "" {
		return
	}

	switch m.mode {
	case FileMode:
		file := m.sidebar.SelectedItem()
		if file == "" {
			m.mainPane.SetContent("")
			return
		}
		diff, err := m.git.FileDiff(m.base, file)
		if err != nil {
			m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
			return
		}
		m.mainPane.SetContent(diff)

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
	statusBarHeight := 1
	contentHeight := max(0, m.height-statusBarHeight-2) // borders
	sidebarWidth := max(0, m.width*3/10)
	mainWidth := max(0, m.width-sidebarWidth-4) // borders

	m.sidebar.SetSize(sidebarWidth, contentHeight)
	m.mainPane.SetSize(mainWidth, contentHeight)
}

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true

	if m.err != nil {
		v.SetContent(fmt.Sprintf("Error: %v\nPress q to quit.\n", m.err))
		return v
	}

	bar := renderStatusBar(m.width, m.repoInfo, m.prInfo, m.mode, m.confirming)
	sidebarView := m.sidebar.View(m.focus == SidebarFocus)
	mainView := m.mainPane.View(m.focus == MainFocus)

	content := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, mainView)

	v.SetContent(bar + "\n" + content)
	return v
}
