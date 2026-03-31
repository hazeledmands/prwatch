# prwatch — Design Spec

A lazygit-style TUI for reviewing the delta between a feature branch and its base branch. Runs in a git repo (including worktrees), stays up-to-date as git state changes.

## Stack

- **Go** with **bubbletea** (Elm architecture: Model → Update → View)
- **lipgloss** for styling
- **fsnotify** for filesystem watching
- Git CLI for all git operations (not go-git)
- `gh` CLI for PR info

## Components

### Git Data Layer

Shells out to `git` CLI. Key operations:

- **Base branch detection**: `gh pr view --json baseRefName` → merge-base with `main` → `master` → `HEAD~1`
- **Changed files**: `git diff --name-only <base>..HEAD` (committed) + `git diff --name-only` (uncommitted)
- **File diff**: `git diff <base>..HEAD -- <file>` (includes uncommitted changes)
- **Commit list**: `git log --oneline <base>..HEAD`
- **Commit patch**: `git show <sha>`

### File Watcher

- `fsnotify` on `.git/` directory and worktree root
- Debounce: coalesce events within 200ms, then send refresh message to bubbletea program
- Catches commits, rebases, ref changes, file edits

### TUI Layout

```
┌─────────────────────────────────────────────────────┐
│ StatusBar: branch | repo | worktree | PR #123 (url) │
├──────────────┬──────────────────────────────────────┤
│  Sidebar     │  Main Pane                           │
│  (files or   │  (diff or patch, scrollable)         │
│   commits)   │                                      │
│              │                                      │
│  [selectable]│  [scrollable]                        │
└──────────────┴──────────────────────────────────────┘
```

Sidebar is ~30% width. Main pane is ~70%.

### Modes

- **File mode**: sidebar lists changed files, main pane shows diff for selected file
- **Commit mode**: sidebar lists commits, main pane shows patch for selected commit

### Key Bindings

| Key | Action |
|---|---|
| `space` | Toggle file/commit mode |
| `f` | File mode |
| `c` | Commit mode |
| `h` / `←` | Focus sidebar |
| `l` / `→` | Focus main pane |
| `j` / `k` / `↑` / `↓` | Navigate (sidebar: select item; main: scroll) |
| `PgUp` / `PgDn` | Scroll main pane |
| `Enter` | Sidebar: switch focus to main. Main: open $EDITOR (file mode only) |
| `q` / `Ctrl+C` | Quit |

### Status Bar

Shows: branch name, repo name, worktree path (if in worktree), PR info.

PR info fetched via `gh pr view --json number,title,url,state,baseRefName`. Non-blocking (goroutine). Shows "No PR" if none exists.

### Diff Coloring

- Green: `+` lines (additions)
- Red: `-` lines (deletions)
- Cyan: `@@` hunk headers
- Bold: `diff --git` file headers

### Editor Integration

On Enter in main pane (file mode): parse the diff hunk headers to determine the source line number at the current viewport position. Exec `$EDITOR +<line> <file>`, suspending the TUI during editing.

### Base Branch Detection

Heuristic chain (first success wins):
1. `gh pr view --json baseRefName` — if PR exists, use its base
2. `git merge-base HEAD main` — common case
3. `git merge-base HEAD master` — legacy repos
4. `HEAD~1` — fallback
