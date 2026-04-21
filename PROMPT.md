create a simple TUI, in the vein of lazygit. it is meant to be run against a directory that's a git branch, possibly in a worktree dir.

it can be started with:

	prwatch [dir]

if [dir] is provided, then it should run against that directory; if not, it should run against the current working directory.

the UI should show the delta between the merge-base of the current branch and the origin's base branch (like GitHub's three-dot diff). for committed files, diff against HEAD. for uncommitted files, diff against the working tree. the tool should use origin/<base> rather than the local base branch ref to stay consistent with GitHub's view.

## layout

the UI should have a "status bar" at the top, with two panes arranged horizontally taking up the rest of the available space. the left pane should be a sidebar -- smaller than the "main" pane on the right.

the sidebar will be a list of selectable items, separated into groups.
each group should be separated by a horizontal rule (non-selectable), and given a heading that includes a parenthesized count of items in the section.

the main pane will display content.
binary content should never be shown -- instead display [binary content].

while loading, data (such as data from github or a CI system), the display should indicate this rather than displaying inaccurate information. however, it should also display the data it _does_ have immediately, to keep the UI snappy and useful.
  - for example, if the program is still downloading results from the GitHub API, it should render the file and diff mode and say "loading from github" on the github header

the UI should update when the size of its bounding box changes. e.g. if the terminal window it is in is resized. wrapped content should re-wrap when the bounding box changes.

## status bar

the "status bar" should be divided into three sections, one line per each.

line 1: overall status
  - name of current directory
  - if in a worktree, the name of the main git tree
  - name of view modes, with the current mode highlighted (sort of like tabs)
    each mode should be clickable
  - if not a git repo, "Not a git repo"
line 2: local git status (not shown if this is not a git repo)
  - name of current branch and merge base, if any (eg: `foo -> main`, or just `main`)
  - number of uncommitted files — both new/unstaged and staged (12 uncommitted)
  - number of unpushed commits (2 unpushed)
  - number of commits after base (3 commits) -- or just the number of commits if we're in the main branch.
    this should always be the true total count (e.g. via `git rev-list --count`), not the number of commits currently loaded.
    clicking this should switch to [commits] mode
  - number of commits that this branch is behind base, if any (4 behind)
    clicking this should switch to [commits] mode
  - number of changed files in this branch, if any (16 changed files)
    clicking this should switch to file view mode
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

## modes

main modes: "file-view", "file-diff", "commit", "pr".
PR mode should be the default mode we start up to, if there is an active PR.
otherwise, default to file-view mode.
switching between file-diff and file-view should retain the selected file.

### file-view mode

the sidebar should be a list of all files in the directory, and the right pane should be the full file, that highlights the diff for the current changeset.

this mode should have a "gutter":
- [n] should toggle on/off line numbers when displaying full files (defaulting to on)
- if there is a diff for the current file, there should be a "diff gutter" that flags new lines (+), removed lines (-), and changed lines (~). if the file being viewed was COMPLETELY removed or is totally new, then the gutter should indicate that too.
- changed lines should have ~ in the gutter. if the diff is less than 1/4 of the width of the active pane, show both the deleted content (in red) and the new content (in green) inline on the same line. if the diff is bigger than that, duplicate the line with the deleted version on top (red) and the new version on bottom (green). retained (unchanged) text within a changed line should be yellow, deleted text red, new text green.
- wrapped text should not wrap into the gutter, instead, the gutter should just be empty for that line
- [shift]+[d] should show/hide removed content from the diff, in its own line (defaulting to showing)
- [shift]+[j]/[k]/[up]/[down] should jump directly to the next or previous diff hunk. this should wrap around, just like search results.
- entering into this view should jump immediately to the first diff
- gutter should stick even when the user scrolls horizontally

### file-diff mode

the left pane should be a list of the files that have been changed, and the right pane should be the content of the diff for the currently-selected file.

### pr-view mode

only available if there is an active PR.
main panel should show the content associated with the currently-selected sidebar item.
sidebar should show:
- description
  - main panel should show:
      - full PR title and status (DRAFT/MERGED)
      - relevant dates: created, updated, and (if applicable) merged or closed — shown as absolute timestamp with relative time
      - tags, assignees, reviewers (and review status for each), projects, milestone
      - PR description with markdown formatting
      - deployments
      - pressing [enter] when the main panel is highlighted should open a browser to the PR url
- Comments section header
  - one line per comment: dim index, author name, dim relative timestamp
  - sorted by date descending (most recent first)
  - main panel shows author with timestamp, then the comment body
    - pressing [enter] when the main panel is highlighted should open a browser to the comment URL
- horizontal rule
- Reviews section header
  - one line per review: dim index, state indicator (✓ ✗ c …), author name, dim relative timestamp
  - sorted by date descending (most recent first)
  - main panel shows author with timestamp, review state, body, and inline code-level comments (file:line plus body for each)
    - inline review comments are fetched via GitHub GraphQL API (gh pr view --json doesn't include them)
    - pressing [enter] when the main panel is highlighted should open a browser to the review URL
- horizontal rule
- CI section header
  - one line per CI check: state indicator, check name, dim relative last updated time
  - sorted by: failures first, then pending, then passing; secondary order preserves GitHub's canonical order
  - main panel shows check name, status, start/completion timestamps, URL, and (for RWX) fetched logs
    - pressing [enter] when the main panel is highlighted should open a browser to the CI URL

### CI logs
- support RWX as a CI provider. if the github CI status points to RWX and there are failures, use the rwx CLI tool to display details about the failures (including failing test results).

### sidebar (both file modes)

the sidebar should be separated into categories:
  1. new changes — untracked or unstaged files
  2. staged — staged but uncommitted files
  3. committed files
  4. all files (file-view mode only)

order within these categories should be alphabetical.
deleted files should still show up in this view, but they should be red.

change-type indicators: in the new changes, staged, and committed sections, each changed file should display a right-aligned badge indicating the nature of the change:
  - `[-]` in red for files that are entirely deletions (file was removed or diff is all removals)
  - `[+]` in green for entirely new files (file was added or diff is all additions)
  - `[±]` in the default color for files with a mix of additions and deletions

[i] should toggle on/off view of gitignored files in all files mode. it should be on by default. ignored files should show up in a dimmed color.

tree view (enabled by default): files should be grouped under directories, and subsequently indented.
- directories should be prefixed with a triangle glyph that is facing to the right if the directory is closed, and down if the directory is open.
- [t] should toggle this mode on/off
- files and subdirectories in directories can be hidden/shown by clicking on them or selecting them by keyboard and pressing [enter].
- for new changes, staged files, and committed files in the current PR, trees should start out open. in the "all files" section, trees should start out closed.
- compact directories: when a chain of directories each have only one child (e.g. `foo/bar/baz/`), collapse them into a single line showing the combined path. this applies even if the leafmost directory contains multiple files — the combined directory entry is expandable/collapsible as a single unit. if the entire chain leads to a single file with no sibling directories, display the whole path including the filename on one line (no directory entry).
- cursor vs. pinned file: the sidebar cursor moves freely over files and directories, but the main panel only updates when the cursor lands on a file. navigating over directories (keys or click) keeps the previous file's content visible. the sidebar should visually distinguish the cursor position from the pinned (currently viewing) file when they differ.

### commit mode

the left pane should be a list of commits (also selectable via keyboard) and the right pane should be the patch associated with the commit.
the list of commits should be separated into categories, each with a section header and horizontal rule separator:
1. New Changes — untracked or unstaged changes (not technically a commit, if there are any they should all be grouped together under one line)
2. Staged — staged but uncommitted changes (grouped together under one line, like new changes)
3. Unpushed — commits that have not yet been pushed to the origin (should be a dimmed color).
4. Pushed — commits in the current branch / PR that have been pushed to the origin
5. Base — commits after the stuff that's already in the base branch (even before the feature branch began)

if this list is very long, we should paginate it. load the first 100 commits initially, then load the next 100 when the user scrolls to the end of the list. show a "load more" entry at the bottom of the list while more commits are available.

## live refresh

the UI should stay up-to-date as the git status changes, ideally refreshing its state from the filesystem unobtrusively and performantly.
- avoid repainting the UI unless the state has changed in some way.
- the view should refresh not only when files change on disk, but also when git state changes in ways that don't modify working tree files — for example, pushing commits, fetching, editing the global gitignore, or garbage collection repacking refs. a periodic background poll can serve as a fallback to catch state changes that filesystem watchers miss.
- if the user has interacted with the app, and there is an update, the app should endeavor to keep the current view as stable as possible (so the currently highlighted file should stay highlighted, and scrolled to the same-ish spot, even while the surrounding content changes)

checking against the github server:
- state updates from the server should happen at most every 30s.
- this automatic refresh interval should decrease to every 10m if there have been no UI events in the last 10m (including mouse movements or window size changes), or if there have been no updates in the state from the remote server in over 24 hours.
- respond to rate limits appropriately, backing off as needed


## edge cases

when running in a non-git directory, file-view mode should be the only mode.

running in a branch without a base branch (i.e. directly in main, or a detached head):
- file modes should show uncommitted changes
- commit mode should list the full commit history

detached HEAD works normally, status bar shows `detached @ <short sha>` instead of a branch name.

## keybindings

### mode switching
| key | action |
|-----|--------|
| [m] | cycle modes (file-view -> file-diff -> commit) |
| [v] or [1] | jump to file-view mode |
| [d] or [2] | jump to file-diff mode |
| [c] or [3] | jump to commit mode |
| [b] or [4] | jump to pr mode |

### focus & navigation
| key | action |
|-----|--------|
| [tab] | toggle focus between sidebar and main panel |
| [,] | focus the sidebar |
| [.] | focus the main panel |
| [j]/[k] or [up]/[down] | sidebar: select item. main pane: scroll vertically |
| [space]/[pgdn] | page down the focused view |
| [shift]+[space]/[pgup] | page up the focused view |
| [gg] | go to the top of the focused view |
| [G] | go to the bottom of the focused view |

### horizontal scrolling (main pane focused, word wrap off)
| key | action |
|-----|--------|
| [h]/[l] or [left]/[right] | scroll left/right. left at scroll=0 switches focus to sidebar. cannot scroll past the last visible character. |

### sidebar tree navigation (sidebar focused, tree mode on)
| key | action |
|-----|--------|
| [left]/[h] | collapse directory (if expanded), or go to nearest parent directory |
| [right]/[l] or [enter] | expand directory (if collapsed), go to first child (if expanded), or (leaf file) switch to main pane |

navigating over directories with any of these keys does not change the main panel content — only landing on a file does.

when not in tree mode, [enter]/[right]/[l] on a sidebar entry switches to the main pane.

### main pane actions
| key | action |
|-----|--------|
| [enter] | file modes: open $EDITOR at current line. commit mode: no-op for now. |
| [shift]+[n]/[p] | jump to next/previous leaf node in the sidebar, even if the sidebar is not selected |
| [y] | sidebar focused: copy the relative path of the selected file to the system clipboard. main pane focused: copy the file path plus the line number range currently in view (e.g. `path/to/file.go:42-87`). |

### file-view specific
| key | action |
|-----|--------|
| [n] | toggle line numbers |
| [shift]+[d] | toggle showing removed diff lines inline |
| [shift]+[j]/[k]/[up]/[down] | jump to next/previous diff (wraps around) |
| [i] | toggle gitignored files in all-files section |
| [t] | toggle tree view |

### display
| key | action |
|-----|--------|
| [+]/[-] | resize sidebar |
| [f] | hide/show sidebar |
| [w] | toggle word wrapping (default: on). word-wrap should break at word boundaries, except words longer than 1/8 of the screen width should be broken mid-word. |
| [r] | manual refresh |

### search
| key | action |
|-----|--------|
| [/] | open search input at bottom of screen |
| [esc] | cancel search |
| [backspace] | if search text is empty, cancel search |
| [enter] | if search text is empty, cancel search. otherwise confirm search and enter n/p navigation mode |
| [n] | next search result (wraps around) |
| [p] or [shift]+[n] | previous search result (wraps around) |

searching should match against the content in the main pane only (not the sidebar), including content that is scrolled offscreen.
searching should match as you type, and scroll to put the results of the search in view.
the number of matches, and the index of the current match, should display at the bottom of the screen.
results should be highlighted (text background should be a contrasting color).

### quit
| key | action |
|-----|--------|
| [q]/[esc] | show confirmation prompt. press [q] again to confirm, or any other key to cancel. |
| [Q]/[ctrl-c] | quit immediately |

### help
| key | action |
|-----|--------|
| [?] | open help page showing all keybindings |

help goes away when you hit [esc] or [q].
[/] within help opens a search scoped to help content, with the same n/p navigation as regular search.
help should be scrollable by mouse and also by all the same scrolling keys as in other views (page up/down, etc)

## mouse behavior

- clicking on files or commits in the sidebar opens them in the main view. clicking a directory toggles its expand/collapse state without changing the main panel.
- scrolling independently scrolls the focused view, keeping selections the same.
- when text is not wrapped, horizontal mouse scroll works too.
- hovering over clickable elements highlights them with a different background color.
- dragging over text highlights it, and finishing a drag copies to the system paste buffer.
  - selecting stays within the boundaries of the pane being dragged in.
  - the highlight should only cover the relevant content that will be copied — not TUI glyphs, border characters, or gutter content.
  - copied text should be the same as the text from the file (or diff) that is being copied - it should not carry over extra newlines when the text in the UI wraps
  - copied text should not include TUI glyphs, gutter characters, or ANSI codes.

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
- when starting, run git status; if there are any changes to the PROMPT.md commit those first
- check BUG_REPORTS.md, if there are bugs reported there: add a regression test that shows the existence of the bug, and then fix them, and then put the bug report plus a little one-liner about how it was fixed in a log at the bottom of the doc.
- this PROMPT.md is the "spec" for this program. it should not be edited; it is the source of truth. if you're looking for a task, check to make sure that this spec has been properly implemented, and if not add running notes to PLAN.md to keep track of your progress. If PLAN.md seems outdated -- clean it up so that it doesn't take up unnecessary context for future agents.
- re-check this file occasionally to see if the user has made changes to it. if there are uncommitted changes to this file, commit them and follow the newly updated instructions
- use test-driven development.
- make small, iterative commits to keep your work trackable.
- before starting work on any new feature or bug fix, create a new git branch. when work is complete on that branch, merge it back into main.
- re-build the binary after every commit.
- push to github after every commit.
- there should be continuous integration with GHA
- after each commit, run `PRWATCH_RENDER_ONCE=1 go run .` to see the current state of the TUI rendered as text. review the output yourself to verify the UI looks correct before moving on.
- if everything looks good from the outside, see if you can explore the app yourself, as a user might, to verify things that way. use EXAMPLES.md to find some local directories to explore in to try out various features.
- if everything still looks good, audit the code for things that could possibly be refactored for clarity, consistency, maintainability or other forms of code quality.
- there should be tests that cover every behavior listed in this prompt file. if a behavior is described here, there should be a test asserting it works.
- if anything in this spec is ambiguous, contradictory, or impossible to implement as written, make a reasonable choice and then flag it in INCONSISTENCIES.md so the human-in-the-loop can clarify.
  - for each inconsistency, provide a short list of proposed paths forward to address them

## DOCUMENTATION
- the readme file should be up-to-date and provide a relatively concise overview of what this tool is meant to do.

## EXAMPLES
Take a look at EXAMPLES.md (should be in .gitignore since these examples may contain sensitive data) for some links to PRs and CI logs that you can use as example cases.

