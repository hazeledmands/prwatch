package ui

import (
	"runtime"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/command"
)

// recordingFactory records the names it was asked to build.
func recordingFactory(names *[]string) command.Factory {
	return func(name string, args ...string) command.Command {
		*names = append(*names, name)
		return command.StubCommand("", nil)
	}
}

// TestCommandLaneClassification pins which factory each subprocess entry point
// draws from. Everything the app runs on its own initiative must come from the
// timed background lane; only the foreground programs the user is sitting in
// front of may come from the untimed interactive lane.
func TestCommandLaneClassification(t *testing.T) {
	tests := []struct {
		name        string
		invoke      func(m *Model)
		wantLane    string // "background" or "interactive"
		wantAtLeast int
	}{
		{
			name: "editor is interactive",
			invoke: func(m *Model) {
				m.sidebar.SetItems([]sidebarItem{{label: "a.go", filePath: "a.go"}})
				// openEditor targets the displayed file, so this fixture has
				// to say what the pane is showing.
				m.lastMainItem = mainItemKey{FilesMode, "a.go"}
				m.openEditor()
			},
			wantLane:    "interactive",
			wantAtLeast: 1,
		},
		{
			name: "browser opener is interactive",
			invoke: func(m *Model) {
				if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
					t.Skipf("no browser opener on %s", runtime.GOOS)
				}
				m.openInBrowser("https://example.com/pr/1")
			},
			wantLane:    "interactive",
			wantAtLeast: 1,
		},
		{
			name: "clipboard copy is background",
			invoke: func(m *Model) {
				if clipboardToolName() == "" {
					t.Skipf("no clipboard tool on %s", runtime.GOOS)
				}
				// Driven through the production entry point, not by handing
				// copyToClipboardCmd the factory this test then asserts on —
				// that form supplied its own answer and would have passed just
				// as happily against m.interactiveFactory. yankPath picks the
				// lane; running the Cmd it returns is what reveals the choice.
				m.sidebar.SetItems([]sidebarItem{{label: "a.go", filePath: "a.go"}})
				m.focus = SidebarFocus
				cmd := m.yankPath()
				if cmd == nil {
					t.Fatal("yankPath returned nil with a file selected")
				}
				cmd()
			},
			wantLane:    "background",
			wantAtLeast: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var background, interactive []string
			m := NewModel("/tmp", testGit())
			m.cmdFactory = recordingFactory(&background)
			m.interactiveFactory = recordingFactory(&interactive)

			tt.invoke(m)

			got, other := background, interactive
			gotName, otherName := "background", "interactive"
			if tt.wantLane == "interactive" {
				got, other = interactive, background
				gotName, otherName = "interactive", "background"
			}
			if len(got) < tt.wantAtLeast {
				t.Errorf("%s lane saw %v, want at least %d command(s)", gotName, got, tt.wantAtLeast)
			}
			if len(other) != 0 {
				t.Errorf("%s lane saw %v, want none", otherName, other)
			}
		})
	}
}

// TestProductionLanesAreWired guards the structural default: NewModel's
// interactive lane must be untimed, and the background lane it hands every
// other caller must carry the default timeout.
func TestProductionLanesAreWired(t *testing.T) {
	type timeouter interface{ Timeout() time.Duration }

	m := NewModel("/tmp", testGit())
	// The interactive lane is the production one — UI tests do not override it,
	// because it is only ever constructed, never run, in tests.
	icmd, ok := m.interactiveFactory("vi", "a.go").(timeouter)
	if !ok {
		t.Fatal("interactive factory produced a command that reports no timeout")
	}
	if got := icmd.Timeout(); got != 0 {
		t.Errorf("interactive lane timeout = %s, want 0", got)
	}

	// The background lane's production value: NewModel takes it from
	// defaultCmdFactory, which UI tests stub, so assert on the real default.
	bcmd, ok := command.DefaultFactory("git", "status").(timeouter)
	if !ok {
		t.Fatal("background factory produced a command that reports no timeout")
	}
	if got := bcmd.Timeout(); got != command.DefaultTimeout {
		t.Errorf("background lane timeout = %s, want %s", got, command.DefaultTimeout)
	}
}
