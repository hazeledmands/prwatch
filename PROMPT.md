create a simple TUI, in the vein of lazygit. it is meant to be run in a directory that's a git branch, possibly in a worktree dir.

the UI should show the delta between the merge-base of the current branch and the origin's base branch (like GitHub's three-dot diff). for committed files, diff against HEAD. for uncommitted files, diff against the working tree. the tool should use origin/<base> rather than the local base branch ref to stay consistent with GitHub's view.

the UI should stay up-to-date as the git status changes, ideally refreshing its state from the filesystem unobtrusively and performantly.

there should be three modes: a "file-diff" mode, a "file-view" mode, and a "commit" mode. [m] should switch between the three modes.
[d] or [1] should jump to file-diff mode
[v] or [2] should jump to file-view mode
[c] or [3] should jump to commit mode

the UI should have a "status bar" at the top, with two panes arranged horizontally taking up the rest of the available space. the left pane should be a sidebar -- smaller than the "main" pane on the right. the sidebar should display a list (of either files or commits) and the main pane should display content.

the "status bar" should show the name of the branch and the repo and the worktree, as well as details and a link to the github PR (if there is one).
details in the status bar:
- current branch, and current upstream
- name of directory
- name of git repo (should be a TUI-compatible link to the git repo)
- current mode (clicking this should switch modes, like the space bar)
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

checking against the github server:
- we should do this often enough to get fresh data, but not so often that we run into rate limits.
- respond to rate limits appropriately, backing off as needed

in the "file-diff" mode, the left pane should be a list of the files that have been changed, and the right pane should be the content of the diff for the currently-selected file.

in the "file-view" mode, the left pane should be a list of all files in the directory, and the right pane should be the full file, that highlights the diff for the current changeset.
this mode should have a "gutter":
  - [n] should toggle on/off line numbers when displaying full files (defaulting to on)
  - if there is a diff for the current file, there should be a "diff gutter" that flags new lines (+), removed lines (-), and changed lines (~). if the file being viewed was COMPLETELY removed or is totally new, then the gutter should indicate that too.
  - wrapped text should not wrap into the gutter, instead, the gutter should just be empty for that line
- any new content (via the diff) should show as "green" in file view mode, and removed content should show as red
- [shift]+[d] should show/hide removed content from the diff, in its own line (defaulting to showing)
- [shift]+[j]/[k]/[up]/[down] should jump directly to the next or previous diff. this should wrap around, just like search results.
- entering into this view should jump immediately to the first diff
- both modes should not attempt to show binary content -- instead it should just say [binary content]

in both file modes, the sidebar should be separated into categories, with a horizontal line between each:
  1. uncommitted files (rendered in a dimmer style)
  2. committed files
  3. all files (file-view mode only)
order within these categories should be alphabetical.
deleted files should still show up in this view, but they should be red.
[i] should toggle on/off view of gitignored files in all files mode. it should be on by default.
tree view (enabled by default): files should be grouped under directories, and subsequently indented.
  - directories should be prefixed with a triangle glyph that is facing to the right if the directory is closed, and down if the directory is open.
  - [t] should toggle this mode on/off
  - files and subdirectories in directories can be hidden/shown by clicking on them or selecting them by keyboard and pressing [enter].
  - for uncommitted files and committed files in the current PR, trees should start out open. in the "all files" section, trees should start out closed.

in "commits" mode:
the left pane should be a list of commmits (also selectable via keyboard) and the right pane should be the patch associated with the commit.
the list of commits should be separated into categories, separated by a dividing horizontal line:
- unpushed changes (not technically a commit, if there are any they should all be grouped together under one line)
- commits that have not yet been pushed to the origin (should be a dimmed color).
- commits in the current branch / PR that have been pushed to the origin
- commits after the stuff that's already in the base branch

switching between file-diff and file-view should retain the selected file.

[tab] should switch focus between the sidebar and the main panel
[,] should focus the sidebar and [.] should focus the main panel
if the sidebar has focus, the up/down/j/k keys should control which item in the sidebar is selected. if the main pane has focus, the up/down/j/k keys should scroll the view.
[pgdn]/[space] should scroll the currently focused view down
[pgup]/[shift]+space should scroll the currently focused view up
the left/right arrow keys and h/l keys (vim style) should scroll the view that is currently in focus left/right if any content is truncated.
if the sidebar has focus, pressing [enter] should switch to the main pane.
if the main pane has focus, pressing [enter] should do a contextually-relevant thing: in file mode it should open $EDITOR to the given file, to whatever line is currently in view. in "commit" mode it should.... maybe do nothing for now.

[q] and [esc] show a confirmation prompt in the status bar. press [q] again to confirm, or any other key to cancel. [shift-Q] and [ctrl-c] quit immediately.

when running in a non-git directory, file-view mode should be the only mode.

running in a branch without a base branch (i.e. directly in main, or a detached head):
- file modes should show uncommitted changes
- commit mode should list the full commit history

detached HEAD works normally, status bar shows `detached @ <short sha>` instead of a branch name.

searching:
the [/] key should open a search input at the bottom of the screen.
cancel the active search with [esc].
searching should match against the content in either pane, even content that is scrolled offscreen.
searching should match as you type, and scroll to put the results of the search in view.
the number of matches, and the index of the current match, should display at the bottom of the screen.
results should be highlighted (text background should be a contrasting color).
pressing [enter] during a search allows [n] to jump to next result and [p] to jump to previous result. jumping between results should wrap around, so the next result after the last one should be the first one.

mouse behavior:
- the user should be able to click on files or commits in the sidebar to open them in the main view.
- scrolling should independently scroll the view but keep the selections the same, kind of like a scroll box in a windowed GUI.
- when text is not wrapped, it should be possible to scroll left/right, too
- hovering the mouse over clickable elements should cause them to highlight
- dragging the mouse over text should highlight the text, and finishing a drag should cause a copy to the system's paste buffer

help mode:
[?] should open a "help" page which should show all the keybindings,
help should goe away when you hit [esc] or [q]
[/] within help should open a search that applies only to the content in the help mode. this search should work the same way as search in the regular view -- pressing enter should go into a "search" mode where [n] and [p] switch between highlighted results
help should be scrollable by mouse.

other keybindings:
[gg] and [G] to go to the top and bottom of whatever view is in focus
[+] and [-] should change the size of the sidebar
[f] should hide/show the sidebar
[w] should toggle on/off word wrapping in the main pane (defaulting to on)

startup sequence:
while still loading, the display should say "loading..." rather than displaying inaccurate information

---

TESTS:
- aim for 90% code coverage. use --race in your tests to avoid race conditions.
- there should be a set of UI snapshot tests, that compare rendered output to a set of "golden files" in a variety of scenarios derived from this prompt.
- there should be a list of UI invariants encoded in a property-based test suite. these invariants should be tested by rendering given a set of inputs (including the state of a git repo and a mocked result from github), and then automatically checking the rendered output against the invariants, including things like:
  - no unexpected line wrapping (if an element is meant to fit on one line, it should)
  - when ANSI codes are stripped out, every line should be the width of the terminal
  - total line count exactly equals the terminal height
  - clicking on an element (x-y coordinates based on the render) should do the thing it's supposed to

DEVELOPING:
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

DOCUMENTATION:
- the readme file should be up-to-date and provide a relatively concise overview of what this tool is meant to do.
