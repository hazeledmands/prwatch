# prwatch Implementation Plan

**Goal:** Build a lazygit-style TUI that shows the delta between a feature branch and its base branch, with file, commit, and PR modes.

**Architecture:** Bubbletea v2 Elm architecture (Model → Update → View). Git CLI for data, fsnotify for live updates. Three visual components (status bar, sidebar, main pane) composed in a root model.

**Tech Stack:** Go, bubbletea v2, bubbles v2 (viewport, key), lipgloss v2, fsnotify

---

## File Structure

```
prwatch/
├── main.go                    # Entry point, tea.NewProgram setup
├── go.mod / go.sum
├── internal/
│   ├── git/
│   │   ├── git.go             # Git CLI wrapper: branch, files, diffs, commits, PR/CI via gh, RWX via rwx
│   │   └── git_test.go        # Tests using temp git repos + mock runners
│   ├── watcher/
│   │   ├── watcher.go         # fsnotify watcher with debounce, sends tea.Msg
│   │   └── watcher_test.go
│   └── ui/
│       ├── model.go           # Root bubbletea model, mode/focus state, key dispatch
│       ├── model_test.go      # Unit tests for Update logic
│       ├── statusbar.go       # Status bar rendering (3 lines: status, git, PR) with clickable regions
│       ├── sidebar.go         # Sidebar: tree view, file/commit/PR item lists
│       ├── sidebar_test.go    # Sidebar selection/navigation tests
│       ├── mainpane.go        # Viewport with diff coloring, word wrap, gutter
│       ├── styles.go          # All lipgloss style definitions
│       ├── keys.go            # Key binding definitions
│       ├── snapshot_test.go   # Golden file snapshot tests
│       ├── invariant_test.go  # Property-based tests (rapid)
│       └── testdata/golden/   # Golden files for snapshot tests
├── PLAN.md
├── PROMPT.md
├── BUG_REPORTS.md
├── INCONSISTENCIES.md
└── README.md
```

---

## Completed Features

Core features (all original tasks complete):
- File-view, file-diff, commit, PR, help modes
- Status bar with 3 lines: mode bar, git status, PR/GitHub status
- Sidebar with tree view, collapse/expand, category separators
- Main pane with diff coloring, word wrap, gutter, search
- File watcher with debounced live refresh
- Mouse support: clicks, scrolling, drag-to-copy, hover

Recent additions:
- Clickable status bar: mode labels (line 1), commit count (line 2), PR elements (line 3)
- Review requests displayed in status bar (👀 emoji)
- CI status with text labels and clickable jump to CI results
- RWX CI log integration: async-fetches run results and failed task logs
- GitHub API error display on status bar line 3

## Known Limitations

See INCONSISTENCIES.md for details:
- PR description shown as plain text (glamour/bubbletea v2 dependency conflict)
- Deployments not shown (gh CLI doesn't expose deployment data)

## Test Coverage

Target: 90%+ for UI and git packages.
- `internal/ui`: ~90%
- `internal/git`: ~87%
- `internal/watcher`: ~82%

Includes property-based invariant tests (line count, line width, sidebar click) and golden file snapshot tests.
