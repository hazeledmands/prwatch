package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// wheelModel builds a model with enough sidebar items and main-pane content
// that both panes can actually scroll, so "nothing moved" in a wheel test
// means the event was inert rather than clamped.
func wheelModel(t *testing.T, sidebarHidden bool) *Model {
	t.Helper()
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.sidebarHidden = sidebarHidden
	m.updateLayout()
	names := make([]string, 0, 60)
	for i := range 60 {
		names = append(names, string(rune('a'+i%26))+string(rune('a'+i/26))+".go")
	}
	putChanges(m, git.SectionCommitted, git.ClassModified, names...)
	m.mode = FilesMode
	m.updateSidebarItems()
	m.wordWrap = false
	m.mainPane.SetWordWrap(false)
	m.mainPane.SetPlainContent(strings.Repeat("line of content\n", 200))
	// The wheel cases below derive their y from statusBarLines() — the same
	// helper the oracle consumes — so pin it against a literal once here.
	// Otherwise a change in the bar's height would silently move every
	// coordinate in this file and the tests would keep passing while
	// testing different rows than they name.
	if got := m.statusBarLines(); got != wheelFixtureStatusRows {
		t.Fatalf("fixture expects a %d-row status bar, got %d — retune the wheel coordinates",
			wheelFixtureStatusRows, got)
	}
	return m
}

// wheelFixtureStatusRows is the status-bar height wheelModel is built around.
// Not a source of truth for anything — just the literal that anchors the
// derivation in this file.
const wheelFixtureStatusRows = 2

func TestResolveMouseRegion(t *testing.T) {
	tests := []struct {
		name       string
		statusRows int
		sidebarW   int
		x, y       int
		want       screenRegion
	}{
		{"status bar row 0", 3, 20, 5, 0, regionStatusBar},
		{"status bar last row", 3, 20, 60, 2, regionStatusBar},
		{"sidebar first column", 3, 20, 0, 3, regionSidebar},
		{"sidebar last column", 3, 20, 19, 10, regionSidebar},
		{"main pane at sidebar edge", 3, 20, 20, 10, regionMainPane},
		{"main pane far right", 3, 20, 79, 20, regionMainPane},
		// sidebarW == 0 is how dragGeometry encodes a hidden sidebar: the
		// main pane owns the leftmost columns, exactly as click and drag
		// already treat them.
		{"hidden sidebar column 0", 3, 0, 0, 10, regionMainPane},
		{"hidden sidebar column 1", 3, 0, 1, 10, regionMainPane},
		{"hidden sidebar status bar still wins", 3, 0, 1, 1, regionStatusBar},
		// The title row and the pane borders belong to the main pane box, so
		// they resolve to it — the same ownership click and drag use (a click
		// on the title row starts a main-pane gesture).
		{"main pane top border", 3, 20, 60, 3, regionMainPane},
		{"main pane title row", 3, 20, 60, 4, regionMainPane},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := dragGeometry{statusRows: tc.statusRows, sidebarW: tc.sidebarW, screenW: 80, screenH: 24}
			if got := g.regionAt(tc.x, tc.y); got != tc.want {
				t.Errorf("regionAt(%d, %d) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// TestMouseWheel_RegionTargets asserts the wheel scrolls the pane that owns
// the coordinate and nothing else — in particular that a wheel over the
// status bar is inert, and that a hidden sidebar's former columns scroll the
// main pane instead of the invisible sidebar.
func TestMouseWheel_RegionTargets(t *testing.T) {
	const (
		targetNone = iota
		targetSidebar
		targetMain
	)
	tests := []struct {
		name          string
		sidebarHidden bool
		x             int
		yStatusOffset int // y = statusBarLines() + this
		want          int
	}{
		{"sidebar visible, over sidebar", false, 5, 5, targetSidebar},
		{"sidebar visible, over main", false, 60, 5, targetMain},
		{"sidebar visible, over status bar", false, 5, -1, targetNone},
		{"sidebar hidden, column 0", true, 0, 5, targetMain},
		{"sidebar hidden, column 1", true, 1, 5, targetMain},
		{"sidebar hidden, over main", true, 60, 5, targetMain},
		{"sidebar hidden, over status bar", true, 1, -1, targetNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := wheelModel(t, tc.sidebarHidden)
			y := m.statusBarLines() + tc.yStatusOffset
			sidebarBefore := m.sidebar.offset
			mainBefore := m.mainPane.viewport.YOffset()

			result, _ := m.Update(tea.MouseWheelMsg{X: tc.x, Y: y, Button: tea.MouseWheelDown})
			m = result.(*Model)

			sidebarMoved := m.sidebar.offset != sidebarBefore
			mainMoved := m.mainPane.viewport.YOffset() != mainBefore

			switch tc.want {
			case targetNone:
				if sidebarMoved || mainMoved {
					t.Errorf("wheel at (%d,%d) should be inert; sidebar moved=%v main moved=%v",
						tc.x, y, sidebarMoved, mainMoved)
				}
			case targetSidebar:
				if !sidebarMoved {
					t.Errorf("wheel at (%d,%d) should scroll the sidebar", tc.x, y)
				}
				if mainMoved {
					t.Errorf("wheel at (%d,%d) should not scroll the main pane", tc.x, y)
				}
			case targetMain:
				if !mainMoved {
					t.Errorf("wheel at (%d,%d) should scroll the main pane", tc.x, y)
				}
				if sidebarMoved {
					t.Errorf("wheel at (%d,%d) should not scroll the sidebar", tc.x, y)
				}
			}
		})
	}
}

// TestMouseWheel_HorizontalRespectsRegion checks the horizontal (shift+wheel)
// branch is gated on the same region resolution as the vertical one.
func TestMouseWheel_HorizontalRespectsRegion(t *testing.T) {
	tests := []struct {
		name          string
		sidebarHidden bool
		x             int
		yStatusOffset int
		wantScroll    bool
	}{
		{"over main", false, 60, 5, true},
		{"over sidebar", false, 5, 5, false},
		{"over status bar", false, 60, -1, false},
		{"hidden sidebar column 0", true, 0, 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := wheelModel(t, tc.sidebarHidden)
			m.mainPane.SetPlainContent(strings.Repeat("x", 200))
			y := m.statusBarLines() + tc.yStatusOffset

			result, _ := m.Update(tea.MouseWheelMsg{X: tc.x, Y: y, Button: tea.MouseWheelDown, Mod: tea.ModShift})
			m = result.(*Model)

			if got := m.mainPane.xOffset != 0; got != tc.wantScroll {
				t.Errorf("shift+wheel at (%d,%d): horizontal scrolled=%v, want %v", tc.x, y, got, tc.wantScroll)
			}
		})
	}
}
