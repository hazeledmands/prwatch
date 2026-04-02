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

The status bar shows your branch, repo name, worktree status, and GitHub PR info (refreshed periodically with adaptive rate limiting).

## Modes

There are three modes, switchable with `m` or by clicking the mode indicator in the status bar:

- **File View** (`v` / `1`, default) -- sidebar lists all files (uncommitted, committed, and all repo files), main pane shows the full file with line numbers and a diff gutter highlighting added/changed/removed lines.
- **File Diff** (`d` / `2`) -- sidebar lists changed files, main pane shows the unified diff for the selected file.
- **Commit** (`c` / `3`) -- sidebar lists commits (uncommitted changes, unpushed, pushed, base branch) with category dividers, main pane shows the patch.

## Keys

| Key | Action |
|-----|--------|
| `m` | Cycle between modes |
| `v`/`1` `d`/`2` `c`/`3` | Jump to a specific mode |
| `tab` | Toggle focus between sidebar and main pane |
| `,` `.` | Focus sidebar / main pane directly |
| `j`/`k` / up/down | Navigate sidebar or scroll main pane |
| `h`/`l` / left/right | Scroll horizontally (when word wrap is off) |
| `space`/`pgdn` | Page down |
| `shift+space`/`pgup` | Page up |
| `gg` `G` | Go to top / bottom |
| `enter` | Sidebar: focus main. Main (file mode): open `$EDITOR` |
| `/` | Search (incremental, then `n`/`p`/`shift+N` to navigate matches) |
| `?` | Help (scrollable, searchable) |
| `w` | Toggle word wrap |
| `n` | Toggle line numbers (file view) |
| `i` | Toggle gitignored files (file view) |
| `D` | Toggle removed lines in diff gutter (file view) |
| `J`/`K`/`shift+up/down` | Jump to next/previous diff hunk (file view) |
| `t` | Toggle tree view (file modes) |
| `r` | Manual refresh |
| `f` | Toggle sidebar visibility |
| `+`/`-` | Resize sidebar |
| `q`/`esc` | Quit (with confirmation) |
| `Q`/`ctrl+c` | Quit immediately |

## Mouse

- Click sidebar items to select them
- Scroll wheel for vertical scrolling (horizontal when wrap is off)
- Hover highlights clickable elements
- Drag to select text (copies to clipboard on release)

## Live refresh

prwatch watches the working directory and `.git` for changes via fsnotify, refreshing automatically when you make commits or edit files. GitHub PR status refreshes periodically with adaptive rate limiting.

## Non-git directories

When run outside a git repo, prwatch shows file-view mode only with the directory contents.
