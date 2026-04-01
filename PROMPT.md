create a simple TUI, in the vein of lazygit. it is meant to be run in a directory that's a git branch, possibly in a worktree dir.

the UI should show the delta between the current branch and the base branch that the current branch diverged from. it should stay up-to-date as the git status changes, ideally refreshing its state from the filesystem unobtrusively and performantly.

there should be three modes: a "file-diff" mode, a "file-view" mode, and a "commit" mode. the [space bar] should switch between the three modes.

the UI should have a "status bar" at the top, with two panes arranged horizontally taking up the rest of the available space. the left pane should be a sidebar -- smaller than the "main" pane on the right. the sidebar should display a list (of either files or commits) and the main pane should display content.

the "status bar" should show the name of the branch and the repo and the worktree, as well as details and a link to the github PR (if there is one).

in the "file-diff" mode, the left pane should be a list of the files that have been changed, and the right pane should be the content of the diff for the currently-selected file.

in the "file-view" mode, the left pane should be a list of the files that have been changed, and the right pane should be the full file.

in both file modes, the sidebar separates committed files from uncommitted files with a horizontal line. uncommitted files are rendered in a dimmer style. uncommitted files should be first. secondary order should be alphabetical.

in the "commit" mode, the left pane should be a list of commmits (also selectable via keyboard) and the right pane should be the patch associated with the commit.

[d] should jump right to file-diff mode.
[f] or [v] should jump right to file-view mode
[c] should jump right to commit mode
switching between file-diff and file-view should retain the selected file.

the left/right arrow keys and h/l keys (vim style) should control whether the sidebar or the main pane have focus. if the sidebar has focus, the up/down/j/k keys should control which item in the sidebar is selected. if the main pane has focus, the up/down/j/k/page-up/page-down keys should scroll the view.

if the sidebar has focus, pressing [enter] should switch to the main pane.
if the main pane has focus, pressing [enter] should do a contextually-relevant thing: in file mode it should open $EDITOR to the given file, to whatever line is currently in view. in "commit" mode it should.... maybe do nothing for now.

[q] and [esc] show a confirmation prompt in the status bar. press [q] again to confirm, or any other key to cancel. [shift-Q] and [ctrl-c] quit immediately.

when running in a non-git directory, file-view mode should be the only mode.

running in a branch without a base branch (i.e. directly in main, or a detached head): file modes should show uncommitted changes, and commit mode should just list the commit history (limit 10 for now)

detached HEAD works normally, status bar shows `detached @ <short sha>` instead of a branch name.

---

DEVELOPING:
- this PROMPT.md is the "spec" for this program. it should not be edited; it is the source of truth. if you're looking for a task, check to make sure that this spec has been properly implemented, and if not add running notes to PLAN.md to keep track of your progress.

