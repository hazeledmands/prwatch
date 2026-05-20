# prwatch Implementation Plan

**Goal:** Build a lazygit-style TUI that shows the delta between a feature branch and its base branch, with file, commit, and PR modes.

**Architecture:** Bubbletea v2 Elm architecture (Model → Update → View). Git CLI for data, fsnotify for live updates. Three visual components (status bar, sidebar, main pane) composed in a root model.

**Tech Stack:** Go, bubbletea v2, bubbles v2 (viewport, key), lipgloss v2, fsnotify, goldmark

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
│       ├── markdown.go        # Goldmark-based markdown → ANSI renderer
│       ├── markdown_test.go   # Markdown rendering tests
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

## In Progress: Position-based diff addressing

**Goal:** Introduce explicit `Position` and `Range` values naming "where the user is pointing in a diff" and "what window is currently visible." Several planned features (hunk-grain navigation, line-aware deep-links, inline comments, keyboard selection, LSP) all need to ask "what am I looking at right now?" Today this is derived ad-hoc from viewport state each time. Naming it unblocks the rest.

**Common thread.** Most items in `IDEAS.md` need finer granularity than "this file" — hunk, line, or (LSP + stream-mode selection) sub-line.

**Current state — addressing is anonymous:**
- `Model.currentLineNumber()` at `internal/ui/model.go:1751` derives current line from viewport-top on demand.
- `diffHunk{StartLine, EndLine}` at `internal/ui/mainpane.go:76` is hunk-indexed by new-file line range.
- `hunkPositionForLine` at `internal/ui/mainpane.go:150` classifies a line against the hunk list.
- `nextDiffLine` at `internal/ui/navigate.go:7` walks line-by-line, not hunk-by-hunk (related to the removal-only-hunk bug in `BUG_REPORTS.md`).
- `dragSelection` at `internal/ui/drag.go:33` stores selection in *pixel coordinates*, so it doesn't survive viewport changes and can't share state with keyboard-driven selection without translation.
- The **visible-window-as-a-range** pattern is *already* implicit in the codebase: `hunkTitleRight` at `mainpane.go:401-431` calls `visibleHunkRange(hunks, topLine, bottomLine)`; `progressPercent` at `mainpane.go:438-457` uses viewport bottom; drag uses both top + bottom (`drag.go:357-358`). Range as a peer type would just *name* what's there.

**Proposed shapes:**

```go
type Position struct {
    File       *ChangedFile  // which changed file
    SourceLine int           // 1-indexed line in the displayed version
    Column     int           // 0-indexed column; populated for stream selection, LSP, and mouse drag
    Side       Side          // add | remove | context — needed for PR comments + deep-links
    Ref        string        // which ref this line lives in (interacts with scope-diff)
}

type Range struct {
    Start, End Position
}
```

Position is a singular point (cursor / focus / anchor / active). Range is a pair (selection, visible window, hunk extent, comment range). Today the cursor doesn't exist as a distinct concept — `Position` references derive from viewport-top until visual mode / click-to-place lands.

Each feature becomes a pure function of these:
- `o` deep-link: `Position → URL` or `Range → range URL`
- Hunk nav: `(Position, []diffHunk, direction) → Position`
- Hunk title display: `(Range visible, []diffHunk) → string` (already shaped this way)
- Comment lookup: `Position → []Comment`
- LSP query: `Position → LspRequest` (uses Column)
- `progressPercent`: `Range visible → int` (uses `visible.End`, stays viewport-based even when cursor exists — "how much of the file have I seen" is a more useful semantics for diff review than vim's cursor-position convention)

**Selection = a Range with mode.** `Selection { Anchor Position; Active Position; Mode SelectionMode }` covers:
- Existing mouse drag-to-copy (anchor = click, active = mouse, mode = stream)
- Keyboard visual mode (anchor = where `v` was pressed, active = cursor)
- PR comment ranges (GitHub anchors on start/end line + side)
- Range deep-links (`#L12-L15`)

Stream-mode keyboard selection needs Column for the active end — extending with `h`/`l` is character-grained.

**Implementation order:**

1. **Name `Position` and `Range`; introduce `Model.visibleRange()`.** Route existing "where am I" and "what's on screen" call-sites through them. Behavior unchanged — this is a setup refactor whose payoff appears in steps 2+. The leverage comes from downstream callers taking typed values instead of bare ints.
2. **Convert `dragSelection` to Position-based.** Mouse handler translates `(x, y) → Position` at event time. Eliminates the `originStartY` workaround (`drag.go:26-32`) since a Position anchor survives viewport scroll natively. Existing rapid tests guard regressions.
3. **Hunk-grain navigation.** `shift+J`/`shift+down` move active Position to next hunk start instead of next diff line. Should also fix the removal-only-hunk bug — hunk-indexed nav doesn't depend on a new-file line existing.
4. **Hunk popover.** Clickable "hunk N/M" in the title bar opens a list; click navigates active Position.
5. **Keyboard visual mode.** `v` (stream), `V` (line), cursor movement extends, `y` copies, `Esc` dismisses. Stream mode advances Column. Block-mode (`Ctrl-V`) deferred — weird interactions with diff gutters + mixed +/- lines.
6. **`o` deep-link cascade.** Single-line URL when no selection, line-range URL when selection active. Falls back PR → branch → repo per `IDEAS.md`.

**Open UX questions** (don't block the refactor but need answers as features land):

- Once cursor diverges from viewport-top, what defines "the current hunk" for the title-bar display — cursor, viewport, or visible range? Current code uses visible range; cursor-based might feel more natural with visual mode active.
- Whether `progressPercent` ever switches to cursor-based. Tentative answer: no, viewport-based stays — "% seen" is more useful for diff review than "where is the cursor."

**Deferred (need design before implementation):**

- **Whole-scope diff** (`IDEAS.md: SCOPE & THE FILES VIEW`). Orthogonal in code structure (it determines viewport content), but interacts via `Position.Ref` needing per-line provenance from the scope-diff renderer so deep-links and comments anchor to the right ref. Open: what's the base when the scope spans multiple refs? UI for switching modes?
- **Inline session comments** (`IDEAS.md: SESSION / PR COMMENTS #1`). Anchoring strategy is the dominant question — blob-hash (canonical, breaks on edits) vs surrounding-line-hash (fuzzy, survives small edits). Choice depends on whether comments are ephemeral per session or persistent.
- **PR comments** (`IDEAS.md: SESSION / PR COMMENTS #2`). GitHub already solves anchoring; mostly fetch + display + post. Worth designing alongside local session comments so they share a data model.
- **LSP semantic browsing** (`IDEAS.md: SEMANTIC BROWSING`). Heaviest data-layer extension (process management, indexing). Position's `Column` field is forward-compatible; full implementation deferred.

**Testing:**
- Property tests for `Position` and `Range` invariants (round-trip through viewport scroll, scope change, etc.) belong in `internal/ui/position_test.go` per the CLAUDE.md guidance on extracted state machines.
- Existing rapid drag-selection tests guard step 2.
- New rapid tests for visual-mode selection mirror the drag tests in shape.

---

## Completed Features

Core features (all original tasks complete):
- Files, commits, pr modes (plus help overlay)
- Switching between modes retains per-mode view state (sidebar selection, scroll positions, focus)
- Status bar with 3 lines: mode bar, git status, PR/GitHub status
- Sidebar with tree view, collapse/expand, category separators
- Main pane with diff coloring, word wrap, gutter, search
- File watcher with debounced live refresh
- Mouse support: clicks, scrolling, drag-to-copy, hover
- Clickable status bar: mode labels (line 1), commit count (line 2), PR elements (line 3)
- Review requests displayed in status bar
- CI status with text labels and clickable jump to CI results
- RWX CI log integration: async-fetches run results and failed task logs
- Adaptive PR refresh: 30s when active, 10m when idle or stale
- PR description with dates, markdown rendering (goldmark), and deployments
- Comments and reviews show author with timestamp + markdown rendering
- CI checks show start/completion timestamps and URL
- PR deployments fetched via GitHub GraphQL API

## Previously Known Limitations (now resolved)

- PR description markdown: resolved by using goldmark + custom ANSI renderer
- PR deployments: resolved by using GitHub GraphQL API via `gh api graphql`
- Sidebar emoji truncation: resolved by switching to runewidth-aware truncation

## Performance

- Startup performance tests verify UI renders immediately with loading state before data loads
- Init() splits loading into local git data (fast) + PR API data (slow) so the UI is usable within milliseconds
- Manual refresh ([r]) also uses split loading to avoid blocking on GitHub API
- Benchmarks track View() and RenderOnce performance over time
- Full test suite completes in ~19s by default (5 rapid iterations, 3-20 steps)
- For thorough verification: `PRWATCH_RAPID_CHECKS=100 go test -race ./...`

## Test Coverage

Target: 90%+ for UI and git packages.
- `internal/ui`: ~87.7%
- `internal/git`: ~86.1%
- `internal/watcher`: ~86.4%

Includes property-based invariant tests (line count, line width, sidebar click, drag-copy, tree navigation, interaction invariants) and 16 golden file snapshot tests.
