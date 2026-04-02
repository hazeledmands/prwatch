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
│   │   ├── git.go             # Git CLI wrapper: base branch, files, diffs, commits, patches, PR/CI via gh
│   │   └── git_test.go        # Tests using temp git repos + mock gh runner
│   ├── watcher/
│   │   ├── watcher.go         # fsnotify watcher with debounce, sends tea.Msg
│   │   └── watcher_test.go
│   └── ui/
│       ├── model.go           # Root bubbletea model, mode/focus state, key dispatch
│       ├── model_test.go      # Unit tests for Update logic
│       ├── statusbar.go       # Status bar rendering (3 lines: status, git, PR)
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

## Completed Tasks

All original implementation tasks are done:

1. Project Scaffold
2. Git Data Layer
3. Styles and Key Bindings
4. Sidebar Component
5. Main Pane
6. Status Bar
7. Root Model
8. File Watcher
9. Integrate Watcher
10. Smoke Test and Polish

Additional features implemented after initial plan:
- PR-view mode (sidebar: description, comments, reviews, CI checks)
- Mode bar with clickable labels
- Shift+N/P leaf navigation
- Help overlay with scoped search
- Mouse drag-to-copy with word wrap support
- Behind count display
- Tree view with collapse/expand, single-leaf optimization

## Known Limitations

See INCONSISTENCIES.md for details:
- PR description shown as plain text (glamour/bubbletea v2 dependency conflict)
- Deployments not shown (gh CLI doesn't expose deployment data)

## Test Coverage

Target: 90%+ across all packages.
- `internal/ui`: ~91%
- `internal/git`: ~92%
- `internal/watcher`: ~86%

Includes property-based invariant tests (line count, line width, sidebar click) and golden file snapshot tests.
