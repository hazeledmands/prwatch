create a simple TUI, in the vein of lazygit. it is meant to be run in a directory that's a git branch, possibly in a worktree dir.

the UI should show the delta between the merge-base of the current branch and the origin's base branch (like GitHub's three-dot diff). for committed files, diff against HEAD. for uncommitted files, diff against the working tree. the tool should use origin/<base> rather than the local base branch ref to stay consistent with GitHub's view.

## live refresh

the UI should stay up-to-date as the git status changes, ideally refreshing its state from the filesystem unobtrusively and performantly.
- if the user has interacted with the app, and there is an update, the app should endeavor to keep the current view as stable as possible (so the currently highlighted file should stay highlighted, and scrolled to the same-ish spot, even while the surrounding content changes)

checking against the github server:
- we should do this often enough to get fresh data, but not so often that we run into rate limits.
- respond to rate limits appropriately, backing off as needed

## layout

the UI should have a "status bar" at the top, with two panes arranged horizontally taking up the rest of the available space. the left pane should be a sidebar -- smaller than the "main" pane on the right. the sidebar should display a list (of either files or commits) and the main pane should display content.

binary content should never be shown -- instead display [binary content].

startup sequence: while still loading, the display should say "loading..." rather than displaying inaccurate information.

## status bar

the "status bar" should show the name of the branch and the repo and the worktree, as well as details and a link to the github PR (if there is one).
details in the status bar:
- current branch, and current upstream
- name of directory
- name of git repo (should be a TUI-compatible link to the git repo)
- current mode (clicking this should switch modes, like [m])
- current PR (should be a TUI-compatible link)
- high level overview of "git status":
  - ahead of upstream by ? commits (? unpushed commits)
  - number uncommitted files (clicking this should switch to "file diff" mode)
  - number of commits in branch (clicking this should switch to "commit" mode)
- high level overview of "github PR status":
  - draft mode?
  - CI status (with link)
  - review requests and approvals / rejections
  - number of comments

## modes

there should be three modes: a "file-view" mode, a "file-diff" mode, and a "commit" mode.
file-view mode should be the default mode we start up to.
switching between file-diff and file-view should retain the selected file.

### file-view mode

the left pane should be a list of all files in the directory, and the right pane should be the full file, that highlights the diff for the current changeset.

this mode should have a "gutter":
- [n] should toggle on/off line numbers when displaying full files (defaulting to on)
- if there is a diff for the current file, there should be a "diff gutter" that flags new lines (+), removed lines (-), and changed lines (~). if the file being viewed was COMPLETELY removed or is totally new, then the gutter should indicate that too.
- changed lines should have ~ in the gutter. if the diff is less than 1/4 of the width of the active pane, show both the deleted content (in red) and the new content (in green) inline on the same line. if the diff is bigger than that, duplicate the line with the deleted version on top (red) and the new version on bottom (green). retained (unchanged) text within a changed line should be yellow, deleted text red, new text green.
- wrapped text should not wrap into the gutter, instead, the gutter should just be empty for that line
- [shift]+[d] should show/hide removed content from the diff, in its own line (defaulting to showing)
- [shift]+[j]/[k]/[up]/[down] should jump directly to the next or previous diff. this should wrap around, just like search results.
- entering into this view should jump immediately to the first diff

### file-diff mode

the left pane should be a list of the files that have been changed, and the right pane should be the content of the diff for the currently-selected file.

### sidebar (both file modes)

the sidebar should be separated into categories, with a horizontal line between each:
  1. uncommitted files (rendered in a dimmer style)
  2. committed files
  3. all files (file-view mode only)

order within these categories should be alphabetical.
deleted files should still show up in this view, but they should be red.
[i] should toggle on/off view of gitignored files in all files mode. it should be on by default. ignored files should show up in a dimmed color.

tree view (enabled by default): files should be grouped under directories, and subsequently indented.
- directories should be prefixed with a triangle glyph that is facing to the right if the directory is closed, and down if the directory is open.
- [t] should toggle this mode on/off
- files and subdirectories in directories can be hidden/shown by clicking on them or selecting them by keyboard and pressing [enter].
- for uncommitted files and committed files in the current PR, trees should start out open. in the "all files" section, trees should start out closed.
- special case: if there is only one leaf node in the tree, display the whole relevant subtree on the same line, kind of like when tree mode is disabled.

### commit mode

the left pane should be a list of commits (also selectable via keyboard) and the right pane should be the patch associated with the commit.
the list of commits should be separated into categories, separated by a dividing horizontal line:
1. unpushed changes (not technically a commit, if there are any they should all be grouped together under one line)
2. commits that have not yet been pushed to the origin (should be a dimmed color).
3. commits in the current branch / PR that have been pushed to the origin
4. commits after the stuff that's already in the base branch (even before the feature branch began)

if this list is very long, we should do something to limit memory usage here. for now, probably it's okay to cap this list at 1000 entries.

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
| [left]/[h] | close branch, or go to nearest parent |
| [right]/[l] or [enter] | open branch, go to nearest child, or (leaf node) switch to main pane |

when not in tree mode, [enter]/[right]/[l] on a sidebar entry switches to the main pane.

### main pane actions
| key | action |
|-----|--------|
| [enter] | file modes: open $EDITOR at current line. commit mode: no-op for now. |

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
help should be scrollable by mouse.

## mouse behavior

- clicking on files or commits in the sidebar opens them in the main view.
- scrolling independently scrolls the focused view, keeping selections the same.
- when text is not wrapped, horizontal mouse scroll works too.
- hovering over clickable elements highlights them.
- dragging over text highlights it, and finishing a drag copies to the system paste buffer.
  - selecting stays within the boundaries of the pane being dragged in.
  - the highlight should only cover the relevant content that will be copied — not TUI glyphs, border characters, or gutter content.
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

## DEVELOPING
- when starting, run git status; if there are any changes to the PROMPT.md commit those first
- check BUG_REPORTS.md, if there are bugs reported there: add a regression test that shows the existence of the bug, and then fix them, and then remove the bug report.
- this PROMPT.md is the "spec" for this program. it should not be edited; it is the source of truth. if you're looking for a task, check to make sure that this spec has been properly implemented, and if not add running notes to PLAN.md to keep track of your progress.
- use test-driven development.
- make small, iterative commits to keep your work trackable.
- before starting work on any new feature or bug fix, create a new git branch. when work is complete on that branch, merge it back into main.
- re-build the binary after every commit.
- push to github after every commit.
- there should be continuous integration with GHA
- after each commit, run `PRWATCH_RENDER_ONCE=1 go run .` to see the current state of the TUI rendered as text. review the output yourself to verify the UI looks correct before moving on.
- if everything looks good, audit the code for things that could possibly be refactored for clarity, consistency, maintainability or other forms of code quality.
- there should be tests that cover every behavior listed in this prompt file. if a behavior is described here, there should be a test asserting it works.
- if anything in this spec is ambiguous, contradictory, or impossible to implement as written, make a reasonable choice and then flag it in INCONSISTENCIES.md so the human-in-the-loop can clarify.

## DOCUMENTATION
- the readme file should be up-to-date and provide a relatively concise overview of what this tool is meant to do.
