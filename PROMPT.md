create a simple TUI, in the vein of lazygit. it is meant to be run against a directory that's a git branch, possibly in a worktree dir.

it can be started with:

	prwatch [dir]

if [dir] is provided, then it should run against that directory; if not, it should run against the current working directory.

the UI should show the delta between the merge-base of the current branch and the origin's base branch (like GitHub's three-dot diff). the left endpoint of every file's diff is that merge-base; only the right endpoint varies with the file's state: HEAD for files whose changes are all committed, the working tree for files with uncommitted changes. a file with both committed and uncommitted changes therefore shows one blended merge-base → working-tree diff (the uncommitted layer on its own is viewable via the commits-mode pseudo-entries). the tool should use origin/<base> rather than the local base branch ref to stay consistent with GitHub's view.

## live refresh

the UI should stay up-to-date as the git status changes, ideally refreshing its state from the filesystem unobtrusively and performantly.
- avoid repainting the UI unless the state has changed in some way.
- the view should refresh not only when files change on disk, but also when git state changes in ways that don't modify working tree files — for example, pushing commits, fetching, editing the global gitignore, or garbage collection repacking refs. a periodic background poll can serve as a fallback to catch state changes that filesystem watchers miss.
- if the user has interacted with the app, and there is an update, the app should endeavor to keep the current view as stable as possible (so the currently highlighted file should stay highlighted, and scrolled to the same-ish spot, even while the surrounding content changes)

checking against the github server:
- state updates from the server should happen at most every 30s.
- this automatic refresh interval should decrease to every 10m if there have been no UI events in the last 10m (including mouse movements or window size changes), or if there have been no updates in the state from the remote server in over 24 hours.
- respond to rate limits appropriately, backing off as needed

local git polling (fallback for filesystem watcher misses):
- state polls from the local filesystem should happen at most every 5s while the directory is active.
- this interval should decrease to every 1m if there have been no UI events in the last 10m AND no filesystem events in the last 2m.
- any event from the filesystem watcher (working tree, .git/, or refs) is a signal that the directory is active and should immediately reset the poll to the fast interval, even if the user hasn't interacted with the app recently.


## base branch detection

the base branch is determined using the first of the following that yields a valid merge-base with HEAD. this information should be regularly refreshed, in case something in the remote or filesystem state has changed:

1. **`gh pr view --json baseRefName`** — if there's a GitHub PR for the current branch, use its base branch. prefer `origin/<base>` for the merge-base, falling back to the local `<base>` ref if `origin` isn't available. this call to the github API should be non-blocking -- if we don't have the information yet, we should fall through to the next option and update again when we have the API response.
2. **`origin/main`**
3. **`origin/master`**
4. local **`main`** (for repos with no remote configured)
5. local **`master`**
6. **`HEAD~1`** — final fallback. the resulting "delta" is just the latest commit. on a single-commit repo with no remote, even this fails and detection returns an error; the app should then behave as if there is no base (see `## edge cases`).

## commands and keybindings

everything the user can do in the app is a named *command*. commands are context-aware: the same command may be bound in multiple places (sidebar vs. main pane, search input vs. normal mode, help overlay) and do the right thing for that context. the rest of this spec refers to behaviors by command name (e.g. `toggle-ignored`, `next-diff`); the `## keybindings` section at the bottom is the single source of truth for which keys trigger which command. rebinding a key in one place rebinds it everywhere.

## layout

the UI should have a "status bar" at the top, with two panes arranged horizontally taking up the rest of the available space. the left pane should be a sidebar - smaller than the "main" pane on the right.

the sidebar will be a list of selectable items, separated into groups.
each group should be separated by a horizontal rule (non-selectable), and given a heading that includes a parenthesized count of items in the section.

the main pane will display content.
binary content should never be shown - instead display [binary content].

while loading, data (such as data from github or a CI system), the display should indicate this rather than displaying inaccurate information. however, it should also display the data it _does_ have immediately, to keep the UI snappy and useful.
  - for example, if the program is still downloading results from the GitHub API, it should render the file and diff mode and say "loading from github" on the github header

the UI should update when the size of its bounding box changes. e.g. if the terminal window it is in is resized. wrapped content should re-wrap when the bounding box changes.

### unicode width accounting

the terminal cell grid is ground truth; grapheme clusters are atoms; all geometry comes from one width oracle.

- all display-width math comes from one shared grapheme-cluster-aware width function — the same measurement the renderer uses — always applied to whole strings, never computed by summing the widths of parts (cluster merging makes width non-additive across concatenation).
- grapheme clusters are indivisible: no cursor position, selection endpoint, wrap break, or string slice may land inside a cluster. a cluster includes both cells of a wide glyph and a base character together with its combining marks.
- rows that promise a width (title bars, status bar, padded rows) must measure exactly that width under the oracle for any input, zero-width and combining characters included.
- the guarantee is internal self-consistency: every subsystem agrees with the one oracle. terminal emulators disagree with each other on exotic scripts and emoji sequences; that residual variance is accepted and out of scope.

## status bar

the "status bar" should be divided into three sections, one line per each.

line 1: overall status
  - name of current directory
  - if in a worktree, the name of the main git tree
  - name of view modes, with the current mode highlighted (sort of like tabs)
    each mode should be clickable
  - if not a git repo, "Not a git repo"
line 2: local git status (not shown if this is not a git repo)
  - when the commit range is scrubbed away from its default, the line is prefixed with the handle indicator — see `## commit range scope`
  - name of current branch and merge base, if any (eg: `foo -> main`, or just `main`)
  - number of uncommitted files — both new/unstaged and staged (12 uncommitted)
  - number of unpushed commits (2 unpushed)
  - number of commits in scope (3 commits) - or just the number of commits if we're in the main branch. when the scope is scrubbed away from default, this count reflects the scrubbed scope.
    this should always be the true total count (e.g. via `git rev-list --count`), not the number of commits currently loaded.
    clicking this should invoke `commits-mode`
  - number of commits that this branch is behind base, if any (4 behind)
    clicking this should invoke `commits-mode`
  - number of changed files in scope, if any (16 changed files)
    clicking this should invoke `files-mode`
  - if no PR, "No PR"
line 3: github status (not shown if there is no PR)
  - if the github API is returning errors, then put the error message here! otherwise:
  - [DRAFT] if in draft mode, [MERGED] if merged
    - clicking this should jump to the pr mode PR description
    - this should be bright and bold and obvious
  - name of the current PR
    - clicking this should jump to the pr mode PR description
  - review requests and approvals/rejections (as emoji)
    - clicking this should jump straight to the reviews list (if any)
  - number of comments
    - clicking this should jump straight to the comments list
  - CI status as an emoji plus a simple textual indicator (CI failing)
    - clicking this should jump straight to CI results (the first failure, if any)

## title bars

each main pane view has a sticky one-row title bar inside the pane border, with a left-aligned label and a right-aligned context string. when they'd collide, the left side is truncated with an ellipsis so the right stays flush. the title bar is dim + bold; per-mode content lives with each mode.

the sidebar's section headers use the same visual treatment. when scrolled past a header, it's pinned to the topmost sidebar row, overlaying whatever item would be there. selection navigation never lands on the covered row — clamping bumps the offset one extra row when it would. clicking the overlay falls on the non-selectable header (a no-op).

## modes

main modes: "files", "commits", "pr".
pr mode should be the default mode we start up to, if there is an active pull request.
otherwise, default to files mode.
switching between modes should retain the view state from the last time we were on that mode.

### file mode

the sidebar should be a list of all files in the directory, separated into categories:
  1. new changes — untracked or unstaged files
  2. staged — staged but uncommitted files
  3. committed files
  4. all files

order within these categories should be alphabetical.
deleted files should still show up in this view, but they should be red.

the main pane should be the currently-selected file, and it should highlight the diff for the current changeset.

renames are detected via git's similarity heuristic and treated as a single change rather than a delete + add. detection runs over working-tree state too (not just staged/committed), so a `mv` shows as one entry whether or not it's been `git add`-ed. the sidebar entry sits in the section appropriate to its newest state (new changes → staged → committed) and shows the new path; the old path surfaces in the title bar. a rename combined with content edits is still one entry, with the diff displayed as usual.

title bar: file path on the left — for renamed files, the left side shows `<old-path> → <new-path>` instead of a single path. on the right:
- when the file has a diff, derived from the visible viewport range against the file's hunk list:
  - single hunk visible: `hunk N/M`
  - multiple hunks visible: `viewing hunks N through M`
  - no hunks visible: `between hunks (N–N+1)` / `before hunk 1` / `after hunk M`
  - "visible" tracks the hunk's *change-line* range — the new-file lines spanned by its `+`/`-` markers — not the diff header's full range (which includes leading and trailing context). So a hunk is only counted as visible when something it actually *changed* is on screen; seeing only a hunk's leading or trailing context doesn't qualify.
  - the hunk-position string above is preceded by the file's most-recently-changed metadata (joined with ` · `):
    - uncommitted changes: `uncommitted · <relative-time>` (working-tree mtime); falls back to just `uncommitted` when the mtime is unavailable
    - committed changes: `<sha7> · <relative-time>` (most recent commit touching the file)
- when the file has no diff:
  - `<sha7> · <relative-time>` for tracked files (most recent commit touching the file)
  - `untracked · <relative-time>` for untracked files (working-tree mtime)
  - `renamed · <uncommitted|sha7> · <relative-time>` for files renamed with no content changes (a pure `mv`). uncommitted form for working-tree or staged renames; sha7 form for renames that landed in a commit.
  - binary files prefix the result: `binary · <sha7> · <relative-time>` or `binary · untracked · <relative-time>`. binary files always render via this no-diff format regardless of whether they have a diff, since their content can't be hunk-displayed.
  - falls back to `no changes` if nothing is available
- the right side is followed by ` · N%`, the user's progress through the file based on the bottom-most visible source line. 100% means the last line is in view; empty content or content that fits entirely in the viewport both report 100%.

change-type indicators: in the new changes, staged, and committed sections, each changed file should display a right-aligned badge indicating the nature of the change:
  - `[-]` in red for files that are entirely deletions (file was removed or diff is all removals)
  - `[+]` in green for entirely new files (file was added or diff is all additions)
  - `[±]` in the default color for files with a mix of additions and deletions
  - `[→]` in the default color for renamed files (with or without content changes). takes precedence over `[+]`/`[-]`/`[±]` — a rename plus edits is still primarily a rename.

`toggle-ignored` should toggle on/off view of gitignored files in all files mode. the default setting should be to show ignored files. ignored files should show up in a dimmed color.

ignored directories should not be eagerly enumerated — a tree like `node_modules/` containing hundreds of thousands of files would otherwise dominate startup and every refresh. instead, gitignored entries are collected via `git ls-files --directory`, which collapses entire ignored subtrees into a single top-level entry. that entry renders as a dimmed directory in the "all files" section; pressing `focus-right` (or `confirm`) on it lazily fetches the contents on demand. once expanded, the dir behaves like any other tree node.

files are grouped under directories, and subsequently indented.
- directories should be prefixed with a triangle glyph that is facing to the right if the directory is closed, and down if the directory is open.
- files and subdirectories in directories can be hidden/shown by clicking on them or by selecting them in the sidebar and invoking `confirm`.
- for new changes, staged files, and committed files in the current PR, trees should start out open. in the "all files" section, trees should start out closed.
- compact directories: when a chain of directories each have only one child (e.g. `foo/bar/baz/`), collapse them into a single line showing the combined path. this applies even if the leafmost directory contains multiple files — the combined directory entry is expandable/collapsible as a single unit. if the entire chain leads to a single file with no sibling directories, display the whole path including the filename on one line (no directory entry).
- cursor vs. pinned file: the sidebar cursor moves freely over files and directories, but the main panel only updates when the cursor lands on a file. navigating over directories (keys or click) keeps the previous file's content visible. the sidebar should visually distinguish the cursor position from the pinned (currently viewing) file when they differ.

this mode should have a "gutter" — a narrow column to the left of each line showing line numbers and diff indicators.

- **line numbers.** `toggle-line-numbers` toggles line numbers when displaying full files (defaulting to on).
- **diff markers.** if there is a diff for the current file, the gutter flags new lines with `+`, removed lines with `-`, and changed lines with `~`. `~` only fires for 1-to-1 changes — multi-line block changes (multiple deletions adjacent to multiple additions) render as N `-` rows followed by M `+` rows, since there's no meaningful per-line pairing. if the file being viewed was COMPLETELY removed or is totally new, the gutter should indicate that too.
- **changed line rendering.** the gutter (line number + `~`) carries a yellow background to mark the row as changed. when the inline diff is small (less than 1/4 of the pane's width), show old and new on the same line with per-segment backgrounds: deleted segments get a red background, added segments get a green background, and retained text in between has no background. when the diff is larger, split into two rows — the deleted version on top with a red background, the new version below with a green background. `toggle-removed-lines` shows/hides removed-only content as its own row above the next change (defaulting to showing).
- **syntax highlighting.** source code is highlighted using a lexer chosen from the file's extension. retained portions of an inline `~` body, plain context lines, and added / changed / removed-whole-file rows all carry the file's syntax foregrounds; the red/green/yellow diff backgrounds layer on top so the change signal stays visible without overriding the code colors. when no lexer matches the filename (or the content isn't source code), rows render without highlighting.
- **diff navigation.** entering files mode jumps immediately to the first diff. `next-diff` and `prev-diff` jump to adjacent hunks and wrap around, just like search results. The target hunk's first changed line lands ~30% down the viewport — Vim-style centering — so there's a few rows of leading context above it for orientation. Pure-deletion hunks (no new-file lines) navigate to the line their removed text is attached to in the rendered view.
- **wrapping & horizontal scroll.** wrapped text does not wrap into the gutter — continuation lines have an empty gutter. the gutter stays pinned when the user scrolls horizontally.

### commits mode

the left pane should be a list of commits (also selectable via keyboard) and the right pane should be the patch associated with the commit.
the list of commits should be separated into categories, each with a section header and horizontal rule separator:
1. New Changes — untracked or unstaged changes (not technically a commit, if there are any they should all be grouped together under one line)
2. Staged — staged but uncommitted changes (grouped together under one line, like new changes)
3. Unpushed — commits that have not yet been pushed to the origin (should be a dimmed color).
4. Pushed — commits in the current branch / PR that have been pushed to the origin
5. Base — commits after the stuff that's already in the base branch (even before the feature branch began)

the sidebar also shows a horizontal "scope cutline" indicating the commit range boundary — commits above are in scope, commits below are out of scope. the cutline is rendered as a labeled rule (e.g. `─── scope ───`) so it's visually distinct from the plain section separators. by default the cutline sits between Pushed and Base; scrubbing the scope handle moves it. see `## commit range scope`.

if this list is very long, we should paginate it. load the first 100 commits initially, then load the next 100 when the user scrolls to the end of the list. show a "load more" entry at the bottom of the list while more commits are available.

title bar: for a real commit, left shows `<sha7> · <subject>` and right shows `@<author> · <relative-time>`. for the "new changes" or "staged changes" pseudo-entries, right shows the diff shortstat (e.g. `3 files changed, 42 insertions(+), 11 deletions(-)`).

pseudo-entry bodies (main pane content) each show their own distinct diff:
- **staged** — the staged diff (`git diff --cached`).
- **new changes** — the working-tree diff against the index, followed by each untracked file's contents rendered as a new-file diff (untracked and unstaged share this one entry, matching the sidebar grouping above).


### pr mode

only available if there is an active PR.
main panel should show the content associated with the currently-selected sidebar item.

title bar: left shows the section/item label (`Description`, `comment #N`, `review #N · <state>`, `CI · <check-name>`); right shows `@<author> · <relative-time>` for comments and reviews, the check status (bucket name preferred, falling back to state) for CI, or empty for the description.
sidebar should show:
- description
  - main panel should show:
      - full PR title and status (DRAFT/MERGED)
      - relevant dates: created, updated, and (if applicable) merged or closed — shown as absolute timestamp with relative time
      - tags, assignees, reviewers (and review status for each), projects, milestone
      - PR description with markdown formatting
      - deployments
      - `confirm` in the main panel should open a browser to the PR url
- Comments section header
  - one line per comment: dim index, author name, dim relative timestamp
  - sorted by date descending (most recent first)
  - main panel shows author with timestamp, then the comment body
    - `confirm` in the main panel should open a browser to the comment URL
- horizontal rule
- Reviews section header
  - one line per review: dim index, state indicator (✓ ✗ c …), author name, dim relative timestamp
  - sorted by date descending (most recent first)
  - main panel shows author with timestamp, review state, body, and inline code-level comments (file:line plus body for each)
    - inline review comments are fetched via GitHub GraphQL API (gh pr view --json doesn't include them)
    - `confirm` in the main panel should open a browser to the review URL
- horizontal rule
- CI section header
  - one line per CI check: state indicator, check name, dim relative last updated time
  - sorted by: failures first, then pending, then passing; secondary order preserves GitHub's canonical order
  - main panel shows check name, status, start/completion timestamps, URL, and (for RWX) fetched logs
    - `confirm` in the main panel should open a browser to the CI URL

#### CI logs
- support RWX as a CI provider. if the github CI status points to RWX and there are failures, use the rwx CLI tool to display details about the failures (including failing test results).


## commit range scope

the app maintains a global "commit range" — the slice of history currently in scope for files mode, the status-bar counts, and the position of the commits-mode cutline. its inner end is pinned to the working tree; its outer end is a movable handle.

the default outer endpoint matches today's natural PR delta:
- on a branch with a merge-base, the merge-base itself (so default scope = the PR delta).
- on a branch without a merge-base (e.g. main itself, or a detached HEAD), the working tree (so default scope = uncommitted/staged only, no commits). the repository's history is still visible in commits mode — see "effects on each mode" below.

`scope-extend-back` extends the handle one commit further back. `scope-contract-forward` moves it one commit closer to the working tree. `scope-reset` snaps back to default. extension stops at the root commit; contraction stops at the working tree (zero commits in scope).

the scope resets to default on branch switch and does not persist across restarts.

### handle indicator

when the handle is scrubbed away from default, the status bar (line 2) is prefixed with `@<sha7> HEAD~N`. when the handle has crossed a named landmark, append a suffix: `past origin/<base>`, `past PR base`, etc. at the default position no indicator is shown.

### effects on each mode

- **files mode.** the file list, per-file diffs, and the status-bar counts ("N commits", "N changed files") all aggregate over the current scope. files that drop out of scope when contracting disappear; files that come into scope when extending appear.
- **commits mode.** a horizontal "scope cutline" is drawn in the sidebar between the last in-scope commit and the first out-of-scope commit. the New Changes / Staged / Unpushed / Pushed / Base section partitioning is unaffected — those reflect git state, not scope.
- **pr mode.** unaffected. pr mode describes the GitHub PR itself, not a scope-derived view.

### pagination

commits mode loads commits in pages of 100. `scope-extend-back` past the currently loaded extent triggers a fetch; the handle moves when the data arrives, with a "loading" indication in the meantime.

## edge cases

when running in a non-git directory, files mode should be the only mode.

detached HEAD works normally, status bar shows `detached @ <short sha>` instead of a branch name.

## global commands

The following commands should be available from all modes, regardless of the UI state:

### search

`search` opens the search input at the bottom of the screen. while the input is active, searching matches as you type and scrolls to put results in view. results are highlighted (text background should be a contrasting color). the number of matches and the index of the current match should display at the bottom of the screen.

`confirm` with non-empty text confirms the search and enters navigation mode, where `search-next` and `search-prev` cycle through matches (wrapping). `confirm` on empty text exits search. `quit` exits search. pressing backspace on empty text also exits search.

searching should match against the content in the main pane only (not the sidebar), including content that is scrolled offscreen.

### quit

`quit` is context-aware. when a search input is active, it cancels search. when help is open, it closes help. otherwise it shows a confirmation prompt — invoking `quit` again confirms; any other key cancels. `quit-immediate` always exits without confirmation.

### help

`help` opens a help page showing all commands and their bindings. inside help, `search` opens a search scoped to help content (same `search-next`/`search-prev` navigation). help should be scrollable by mouse and by the same scrolling commands (`page-up`, `page-down`, `go-top`, `go-bottom`, `up`, `down`) as other views.


### pr-browse

`pr-browse` opens the browser to the active PR, if there is one.

## keybindings

each command maps to one or more keys. keys listed on the same row are interchangeable.

### mode switching
| command | default key(s) | action |
|---------|----------------|--------|
| `toggle-mode` | `m` | cycle modes: files → commits → pr → files (skips pr if no PR) |
| `files-mode` | `1` | jump to files mode |
| `commits-mode` | `2` | jump to commits mode |
| `pr-mode` | `3` | jump to pr mode (no-op if no PR) |

### visual mode

vim-style keyboard selection in the main pane (only when the main pane is focused, no search input, help overlay, or mouse drag active):

| command | default key(s) | action |
|---------|----------------|--------|
| `visual-stream` | `v` | enter character-grained visual mode anchored at the cursor. in stream mode: dismiss. in line mode: switch to stream, preserving the anchor/active range. |
| `visual-line` | `V` | enter line-grained visual mode anchored at the cursor. in line mode: dismiss. in stream mode: switch to line, preserving the anchor/active range. |
| `visual-dismiss` | `esc` | cancel the selection (preempts quit-confirm while a selection is active) |

- cursor motion extends the selection; the highlight shares the drag-selection renderer and follows the same copy-hygiene rules (PROMPT.md `## mouse behavior`).
- `y` yanks the selection (see `yank-path` — the key does double duty).
- copy semantics by selection kind: **line-wise selections (`V`) are source-text operations** — the copied text reproduces each selected source line exactly, including its trailing whitespace. **cell-wise selections (`v`, mouse drag) are screen operations** — they copy what the highlight covers, and trailing render padding is excluded.

### focus & navigation
| command | default key(s) | action |
|---------|----------------|--------|
| `focus-toggle` | `tab` | toggle focus between sidebar and main panel |
| `focus-sidebar` | `,` | focus the sidebar |
| `focus-main` | `.` | focus the main panel |
| `focus-left` | `h`, `left` | sidebar: collapse dir, or go to nearest parent. main pane: scroll left (or, if word wrap is off and scroll is at 0, switch focus to sidebar). |
| `focus-right` | `l`, `right` | sidebar: expand dir, descend into first child, or (leaf file) switch focus to main pane. main pane: scroll right when word wrap is off. |
| `up` | `k`, `up` | sidebar: select previous item. main pane: scroll up one line. |
| `down` | `j`, `down` | sidebar: select next item. main pane: scroll down one line. |
| `page-up` | `pgup`, `shift+space` | page up the focused view |
| `page-down` | `pgdn`, `space` | page down the focused view |
| `go-top` | `g`, `home` | go to the top of the focused view |
| `go-bottom` | `G`, `end` | go to the bottom of the focused view |

horizontal scrolling via `focus-left` / `focus-right` only applies when the main pane is focused and word wrap is off. when word wrap is on, `focus-right` on the main pane is a no-op, and `focus-left` at the left edge still switches focus to the sidebar.

### main pane & sidebar actions
| command | default key(s) | action |
|---------|----------------|--------|
| `confirm` | `enter` | sidebar (on a dir): expand/collapse. sidebar (on a file): switch focus to main pane. main pane (files mode): open `$EDITOR` at the line currently at the top of the viewport. main pane (pr mode): open a browser to the URL of the selected item. main pane (commits mode): no-op for now. active search input: confirm (empty text cancels). |
| `next-leaf` | `shift+n` | jump to next leaf node in the sidebar, regardless of focus |
| `prev-leaf` | `shift+p` | jump to previous leaf node in the sidebar, regardless of focus |
| `yank-path` | `y` | sidebar focused: copy the selected file's relative path to the system clipboard. main pane focused (files mode): copy `path/to/file.go:N-M` where N-M is the line range currently in view. shows a transient toast in the bottom-left confirming what was copied. |

### files-mode toggles
| command | default key(s) | action |
|---------|----------------|--------|
| `toggle-line-numbers` | `n` | toggle line numbers when displaying full files |
| `toggle-removed-lines` | `shift+d` | toggle showing removed diff lines inline |
| `next-diff` | `shift+j`, `shift+down` | jump to next diff hunk (wraps) |
| `prev-diff` | `shift+k`, `shift+up` | jump to previous diff hunk (wraps) |
| `toggle-ignored` | `i` | toggle gitignored files in all-files section |

### display
| command | default key(s) | action |
|---------|----------------|--------|
| `sidebar-grow` | `+`, `=` | grow sidebar width |
| `sidebar-shrink` | `-` | shrink sidebar width |
| `toggle-sidebar` | `f` | hide/show sidebar |
| `toggle-wrap` | `w` | toggle word wrapping (default: on). word-wrap breaks at word boundaries, except words longer than 1/8 of the screen width are broken mid-word. |
| `refresh` | `r` | manual refresh |

### scope
| command | default key(s) | action |
|---------|----------------|--------|
| `scope-extend-back` | `]` | extend the commit range one commit further back |
| `scope-contract-forward` | `[` | contract the commit range one commit toward the working tree |
| `scope-reset` | `\` | reset the commit range to default |

### search
| command | default key(s) | action |
|---------|----------------|--------|
| `search` | `/` | open search input |
| `search-next` | `n` | next search match (wraps) — available after `confirm` in search mode |
| `search-prev` | `p`, `shift+n` | previous search match (wraps) |

### other global commands
| command | default key(s) | action |
|---------|----------------|--------|
| `pr-browse` | `o` | open the browser to the active PR |
| `quit` | `q`, `esc` | context-aware: cancel active search, close help overlay, or show quit confirmation |
| `quit-immediate` | `Q`, `ctrl+c` | quit without confirmation |
| `help` | `?` | open help overlay |

## mouse behavior

- clicking on files or commits in the sidebar opens them in the main view. clicking a directory toggles its expand/collapse state without changing the main panel.
- scrolling independently scrolls the focused view, keeping selections the same.
- when text is not wrapped, horizontal mouse scroll works too.
- every clickable element has a hover state, so the mouse always reveals what is actionable. the treatment differs by region, matching how each region already signals its selection:
  - **sidebar** — a background color on the hovered row (selection wins over hover, which wins over the pinned/uncommitted/deleted styling).
  - **status bar** — an underline on the hovered label. this covers the line-1 mode labels and every clickable label on lines 2 and 3 (the counts, the PR link, reviews, comments, CI). a label whose click target was truncated away is not hoverable either — hover regions and click regions are the same regions.
- dragging over text highlights it, and finishing a drag copies to the system clipboard.
  - selecting stays within the boundaries of the pane being dragged in.
  - selection endpoints round *outward* to grapheme-cluster boundaries, symmetrically at both edges: an endpoint on any cell of a wide glyph includes the whole glyph (see "unicode width accounting" under layout). a cursor placed by click snaps to the start of the cluster it lands on.
  - the highlight should only cover the relevant content that will be copied — not TUI glyphs, border characters, or gutter content.
  - copied text should be the same as the text from the file (or diff) that is being copied - it should not carry over extra newlines when the text in the UI wraps, and spaces consumed at wrap breaks are restored in the copy.
  - a drag (like a `v` visual selection) is a *screen* operation: it copies what the highlight covers, and trailing render padding is excluded. line-wise `V` selections are *source-text* operations and reproduce lines exactly, trailing whitespace included (see `### visual mode`).
  - copied text should not include TUI glyphs, gutter characters, or ANSI codes.
  - on release, a transient toast appears in the bottom-left in the form `copied selection (N lines, M bytes)` (same toast style as `yank-path`).
  - when dragging past the top line or past the bottom line, the view should scroll, making it possible to copy content larger than the view on the screen.

---

## TESTS
- aim for 90% code coverage. use --race in your tests to avoid race conditions.
- there should be a set of UI snapshot tests, that compare rendered output to a set of "golden files" in a variety of scenarios derived from this prompt.
- there should be a list of UI invariants encoded in a property-based test suite. these invariants should be tested by rendering given a set of inputs (including the state of a git repo and a mocked result from github), and then automatically checking the rendered output against the invariants, including things like:
  - no unexpected line wrapping (if an element is meant to fit on one line, it should)
  - when ANSI codes are stripped out, every line should be the width of the terminal
  - total line count exactly equals the terminal height
  - clicking on an element (x-y coordinates based on the render) should do the thing it's supposed to
- when possible if there is a bug or failure, look at ways that the property-based tests could have caught the failure, and change the generators or add a new property accordingly
- property-based test failure files (`testdata/rapid/**/*.fail`) should be committed to version control so that rapid replays them as regression cases on future runs. delete `.fail` files only if the test signature has changed and rapid reports them as "no longer valid".
- the full test suite should take less than 60s to run by default, though we should be able to crank that up for stronger verification at will.

## PERFORMANCE

- Quick app startup time is important! We should test this, to verify that even when github API or git is taking a long time to respond to requests, we still render whatever data we have quickly. We should have tests that prevent performance regressions.

## DEVELOPING
- `PRWATCH_DEBUG_LOG` enables verbose debug logging to a file. it should log all UI actions, timer fires, filesystem changes, signals from the OS, and re-renders.
- there should be continuous integration with GHA.
- there should be tests that cover every behavior listed in this prompt file. if a behavior is described here, there should be a test asserting it works.

## DOCUMENTATION
- the readme file should be up-to-date and provide a relatively concise overview of what this tool is meant to do.

## EXAMPLES
Take a look at EXAMPLES.md (should be in .gitignore since these examples may contain sensitive data) for some links to PRs and CI logs that you can use as example cases.

---

## WORKFLOW (for Claude)

Everything above describes the product. The rules below bind the agent working on this codebase, not the app itself.

### spec & planning
- this PROMPT.md is the "spec" for this program. it should not be edited; it is the source of truth. if you're looking for a task, check to make sure that this spec has been properly implemented, and if not add running notes to PLAN.md to keep track of your progress. If PLAN.md seems outdated - clean it up so that it doesn't take up unnecessary context for future agents.
- re-check this file occasionally to see if the user has made changes to it. if there are uncommitted changes to this file, commit them and follow the newly updated instructions.
- when starting, run git status; if there are any changes to the PROMPT.md commit those first.
- if anything in this spec is ambiguous, contradictory, or impossible to implement as written, make a reasonable choice and then flag it in INCONSISTENCIES.md so the human-in-the-loop can clarify.
  - for each inconsistency, provide a short list of proposed paths forward to address them.

### bug triage
- check BUG_REPORTS.md, if there are bugs reported there: add a regression test that shows the existence of the bug, and then fix them, and then put the bug report plus a little one-liner about how it was fixed in a log at the bottom of the doc.

### git & commits
- use test-driven development.
- make small, iterative commits to keep your work trackable.
- before starting work on any new feature or bug fix, create a new git branch. when work is complete on that branch, merge it back into main.
- re-build the binary after every commit.
- push to github after every commit.

### verification after commits
- after each commit, run `PRWATCH_RENDER_ONCE=1 go run .` to see the current state of the TUI rendered as text. review the output yourself to verify the UI looks correct before moving on.
- if everything looks good from the outside, see if you can explore the app yourself, as a user might, to verify things that way. use EXAMPLES.md to find some local directories to explore in to try out various features. give yourself a mechanism by which you can navigate around in the app to try out various features. it should be possible to run these commands without needing special permission from the user each time. run prwatch in the background, send it commands via a helper app that communicates with it via IPC, and gets rendered screens on command. commands you run should be one-liners without inlined env vars, since that will cause the user to be prompted to give new permissions.
- if everything still looks good, audit the code for things that could possibly be refactored for clarity, consistency, maintainability or other forms of code quality.

### persona review
- think through the app from different personas: an engineer end user, a UX designer, a product manager, a QA specialist, and a staff software engineer implementing the program. add actionable feedback, in bulled-point form, to AGENT_FEEDBACK.md. Make sure that the feedback file is in .gitignore. if anything in agent feedback seems like it would be in keeping with a reading of the prompt, please make the change proactively.

