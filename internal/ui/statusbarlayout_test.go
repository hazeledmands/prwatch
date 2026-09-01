package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// renderedStatusBarRows returns how many terminal rows the status bar
// actually occupies when rendered from the model's own state — the render
// side of the "layout geometry comes from one function" invariant.
func renderedStatusBarRows(m *Model) int {
	bar, _, _, _ := renderStatusBar(m.width, m.statusBarData())
	return len(strings.Split(bar, "\n"))
}

// statusBarState is one point in the model state space that feeds the
// status-bar row count.
type statusBarState struct {
	name         string
	hasGit       bool
	loading      bool
	prLoadedOnce bool
	confirming   bool
	repoInfo     gitpkg.RepoInfoResult
	prInfo       gitpkg.PRInfoResult
	prError      string
}

func modelForStatusBarState(s statusBarState) *Model {
	var g GitDataSource
	if s.hasGit {
		g = &mockGit{}
	}
	m := NewModel("/tmp/test-repo", g)
	m.width = 100
	m.height = 40
	m.loading = s.loading
	m.prLoadedOnce = s.prLoadedOnce
	m.confirming = s.confirming
	m.repoInfo = s.repoInfo
	m.prInfo = s.prInfo
	m.prError = s.prError
	m.updateLayout()
	return m
}

// TestStatusBarRows_RenderMatchesLayout is the table-driven guard for
// CODE_REVIEW A1 sub-items 1 and 2: the number of rows rendered must equal
// the number of rows the layout reserves.
func TestStatusBarRows_RenderMatchesLayout(t *testing.T) {
	repo := gitpkg.RepoInfoResult{RepoName: "prwatch", DirName: "prwatch", Branch: "feature"}
	pr := gitpkg.PRInfoResult{Number: 7, Title: "a pr"}

	cases := []statusBarState{
		{name: "non-git", hasGit: false},
		{name: "git, nothing loaded yet", hasGit: true, repoInfo: repo},
		{
			name:     "startup window: local git loaded, PR not yet",
			hasGit:   true,
			repoInfo: repo,
			// loading==false (View no longer short-circuits) but PR data
			// has not landed: the "Loading from GitHub…" row is live.
			prLoadedOnce: false,
		},
		{name: "git + PR loaded", hasGit: true, repoInfo: repo, prInfo: pr, prLoadedOnce: true},
		{name: "git + PR error", hasGit: true, repoInfo: repo, prError: "boom", prLoadedOnce: true},
		{name: "confirming, non-git", confirming: true},
		{name: "confirming, git only", hasGit: true, repoInfo: repo, confirming: true, prLoadedOnce: true},
		{name: "confirming, git + PR", hasGit: true, repoInfo: repo, prInfo: pr, prLoadedOnce: true, confirming: true},
		{name: "confirming, startup window", hasGit: true, repoInfo: repo, confirming: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := modelForStatusBarState(c)
			got := renderedStatusBarRows(m)
			want := m.statusBarLines()
			if got != want {
				t.Errorf("rendered %d status-bar rows, layout reserved %d", got, want)
			}
		})
	}
}

// TestProperty_StatusBarRenderRowsMatchLayoutRows fuzzes the whole
// loading × prLoadedOnce × git × confirming × error × PR state space.
func TestProperty_StatusBarRenderRowsMatchLayoutRows(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := statusBarState{
			hasGit:       rapid.Bool().Draw(t, "hasGit"),
			loading:      rapid.Bool().Draw(t, "loading"),
			prLoadedOnce: rapid.Bool().Draw(t, "prLoadedOnce"),
			confirming:   rapid.Bool().Draw(t, "confirming"),
		}
		if rapid.Bool().Draw(t, "hasRepoInfo") {
			s.repoInfo = gitpkg.RepoInfoResult{
				RepoName: "repo",
				DirName:  "repo",
				Branch:   rapid.SampledFrom([]string{"main", "feature", ""}).Draw(t, "branch"),
			}
		}
		if rapid.Bool().Draw(t, "hasPR") {
			s.prInfo = gitpkg.PRInfoResult{Number: rapid.IntRange(1, 999).Draw(t, "prNumber"), Title: "t"}
		}
		if rapid.Bool().Draw(t, "hasPRError") {
			s.prError = "some error"
		}

		m := modelForStatusBarState(s)
		got := renderedStatusBarRows(m)
		want := m.statusBarLines()
		if got != want {
			t.Fatalf("state %+v: rendered %d status-bar rows, layout reserved %d", s, got, want)
		}

		// The layout must also be sized from the same number: the main
		// pane + sidebar get height - statusRows - 2.
		_, _, contentH := layoutDimensions(m.width, m.height, want, m.sidebarPct, m.sidebarHidden)
		if m.mainPane.height != contentH {
			t.Fatalf("state %+v: main pane height %d, layout says %d", s, m.mainPane.height, contentH)
		}
	})
}

// assertLayoutMatchesRender checks the panes are sized for exactly the
// rows the status bar renders — the invariant that makes clicks land.
func assertLayoutMatchesRender(t *testing.T, m *Model, context string) {
	t.Helper()
	rows := m.statusBarLines()
	if got := renderedStatusBarRows(m); got != rows {
		t.Errorf("%s: rendered %d status-bar rows, layout reserved %d", context, got, rows)
	}
	_, _, contentH := layoutDimensions(m.width, m.height, rows, m.sidebarPct, m.sidebarHidden)
	if m.mainPane.height != contentH {
		t.Errorf("%s: main pane height %d, layout for %d status rows says %d",
			context, m.mainPane.height, rows, contentH)
	}
}

// TestConfirmQuit_RelayoutsOnKeypress drives the real key path: entering
// and leaving quit-confirm changes the status bar's row count, so the
// panes have to be resized then and there, not at the next data refresh.
func TestConfirmQuit_RelayoutsOnKeypress(t *testing.T) {
	m := initModel(standardMock(), FilesMode, 100, 30)
	assertLayoutMatchesRender(t, m, "before confirming")

	m = applyAction(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !m.confirming {
		t.Fatalf("q should have entered quit-confirm")
	}
	assertLayoutMatchesRender(t, m, "after pressing q")

	m = applyAction(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.confirming {
		t.Fatalf("any other key should have cancelled quit-confirm")
	}
	assertLayoutMatchesRender(t, m, "after cancelling")

	// Esc is the other entry point, and Esc again the other exit.
	m = applyAction(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	assertLayoutMatchesRender(t, m, "after esc into confirm")
	m = applyAction(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	assertLayoutMatchesRender(t, m, "after esc out of confirm")
}

// TestView_UsesSharedStatusBarData guards the render path itself: the bar
// View() emits must be byte-identical to the one built from the shared
// statusBarData accessor, so the two can't drift again.
func TestView_UsesSharedStatusBarData(t *testing.T) {
	m := modelForStatusBarState(statusBarState{
		hasGit:   true,
		repoInfo: gitpkg.RepoInfoResult{RepoName: "prwatch", DirName: "prwatch", Branch: "feature"},
	})
	bar, _, _, _ := renderStatusBar(m.width, m.statusBarData())
	content := m.View().Content
	if !strings.HasPrefix(content, bar) {
		t.Errorf("View() did not render the shared status bar.\nView starts:\n%q\nshared bar:\n%q",
			firstLines(content, 3), bar)
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
