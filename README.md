# prwatch

A terminal UI for reviewing the diff between your feature branch and its base branch. Think of it as a read-only lazygit focused on PR review.

## Install

```
go install github.com/hazeledmands/prwatch@latest
```

Or build from source:

```
git clone https://github.com/hazeledmands/prwatch
cd prwatch
go build -o prwatch .
```

## Usage

Run `prwatch` from any git branch (including worktrees). It automatically detects the base branch (`gh pr` base, then `origin/main`, then `origin/master`, then `HEAD~1`) and shows the delta.

The 3-line status bar shows: current mode + directory, branch + git status, and GitHub PR info (refreshed periodically with adaptive rate limiting).

## Modes

Three modes, switchable with `m` or by clicking the mode bar in the status bar. Switching between modes retains the per-mode view state (selection + scroll position).

- **Files** (`v` / `1`) — sidebar lists new/staged/committed files and the rest of the repo, main pane shows the full file with line numbers and a diff gutter highlighting added/changed/removed lines.
- **Commits** (`c` / `2`) — sidebar lists commits (new/staged/unpushed/pushed/base) with category dividers, main pane shows the patch.
- **PR** (`b` / `3`) — when a PR exists, shows PR description (with markdown rendering), comments, reviews with inline code comments, CI checks (with RWX log support), and deployments. Default mode when a PR is active.

## Keys

| Key | Action |
|-----|--------|
| `m` | Cycle between modes (files → commits → pr → files) |
| `v`/`1` `c`/`2` `b`/`3` | Jump to a specific mode |
| `tab` | Toggle focus between sidebar and main pane |
| `,` `.` | Focus sidebar / main pane directly |
| `j`/`k` / up/down | Navigate sidebar or scroll main pane |
| `h`/`l` / left/right | Scroll horizontally (when word wrap is off) |
| `space`/`pgdn` | Page down |
| `shift+space`/`pgup` | Page up |
| `g` `G` | Go to top / bottom |
| `enter` | Sidebar: focus main. Main (files mode): open `$EDITOR` |
| `N`/`P` | Jump to next/previous file (leaf) in sidebar |
| `/` | Search (incremental, then `n`/`p`/`shift+N` to navigate matches) |
| `?` | Help (scrollable, searchable) |
| `w` | Toggle word wrap |
| `n` | Toggle line numbers (files mode) |
| `i` | Toggle gitignored files (files mode) |
| `D` | Toggle removed lines in diff gutter (files mode) |
| `J`/`K`/`shift+up/down` | Jump to next/previous diff hunk (files mode) |
| `t` | Toggle tree view (files mode) |
| `r` | Manual refresh |
| `f` | Toggle sidebar visibility |
| `+`/`-` | Resize sidebar |
| `q`/`esc` | Quit (with confirmation) |
| `Q`/`ctrl+c` | Quit immediately |

## Mouse

- Click sidebar items to select them
- Scroll wheel for vertical scrolling (horizontal with shift+wheel when wrap is off)
- Hover highlights clickable elements
- Drag to select text (copies to clipboard on release, excludes gutter)

## Live refresh

prwatch watches the working directory and `.git` for changes via fsnotify, refreshing automatically when you make commits or edit files. GitHub PR status refreshes every 30s when you're actively using the tool, slowing to every 10m when idle (no interactions for 10 minutes) or when server data hasn't changed in 24 hours.

## Non-git directories

When run outside a git repo, prwatch shows files mode only with the directory contents.

## Non-interactive modes

Render the TUI as text and exit (useful for CI or automated review):

```
PRWATCH_RENDER_ONCE=1 prwatch [dir]
PRWATCH_RENDER_ONCE=1 PRWATCH_KEYS="c,j,j" prwatch [dir]
```

Run in headless IPC mode for programmatic control:

```
prwatch --ipc [dir] &
prwatch-ctl --render           # get current screen
prwatch-ctl "c,j,j"           # send keys, get result
prwatch-ctl --quit             # stop
```

## Debug logging

Set `PRWATCH_DEBUG_LOG` to a file path to enable verbose debug logging:

```
PRWATCH_DEBUG_LOG=/tmp/prwatch-debug.log prwatch
```

Logs incoming message types (data refreshes, file watcher events, ticks), collapse state changes in the sidebar tree, and selection/scroll position changes after sidebar rebuilds. No-op when unset.
