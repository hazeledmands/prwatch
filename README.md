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

Run `prwatch` from any git branch (including worktrees). It automatically detects the base branch (`gh pr` base, then `main`, then `master`, then `HEAD~1`) and shows the delta.

The status bar shows your branch, repo name, worktree status, and GitHub PR info (if any).

## Modes

**File mode** (default) — sidebar lists changed files, main pane shows the diff for the selected file. Committed and uncommitted files are visually separated.

**Commit mode** — sidebar lists commits since the base, main pane shows the patch for the selected commit.

## Keys

| Key | Action |
|-----|--------|
| `space` | Toggle between file/commit mode |
| `f` / `c` | Switch to file/commit mode directly |
| `h` `l` / arrows | Move focus between sidebar and main pane |
| `j` `k` / arrows | Navigate sidebar selection or scroll main pane |
| `u` `d` | Half-page scroll (main pane) |
| `pgup` `pgdn` | Page scroll (main pane) |
| `enter` | Sidebar: focus main pane. Main pane (file mode): open `$EDITOR` at current line |
| `q` `esc` | Quit (with confirmation) |
| `Q` `ctrl+c` | Quit immediately |

## Live refresh

prwatch watches the working directory and `.git` for changes via fsnotify, refreshing automatically when you make commits or edit files.
