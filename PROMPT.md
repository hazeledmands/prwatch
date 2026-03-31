# prwatch — Original Prompt

I'd like to create a simple TUI, in the vein of lazygit. it should be run in a directory that's a git branch, possibly in a worktree dir. the UI should show the delta between the current branch and the base branch that the current branch diverged from. it should stay up-to-date as the git status changes, ideally refreshing its state from the filesystem unobtrusively and performantly.

there should be two modes: a "file" mode and a "commit" mode. the [space bar] should switch between the two modes, and the [f] amd [c] keys should switch to the appropriate mode directly.

the UI it should have a "status bar" at the top, and two panes arranged horizontally taking up the rest of the available space. the left pane should be a sidebar -- smaller than the "main" pane on the right. the sidebar should display a list (of either files or commits) and the main pane should display content.

the "status bar" should show the name of the branch and the repo and the worktree, as well as details and a link to the github PR (if there is one).

in the "file" mode, the left pane should be a list of the files that have been changed, and the right pane should be the content of the diff for the currently-selected file.
in the "commit" mode, the left pane should be a list of commmits (also selectable via keyboard) and the right pane should be the patch associated with the commit.

the left/right arrow keys and h/l keys (vim style) should control whether the sidebar or the main pane have focus. if the sidebar has focus, the up/down/j/k keys should control which item in the sidebar is selected. if the main pane has focus, the up/down/j/k/page-up/page-down keys should scroll the view.

if the sidebar has focus, pressing [enter] should switch to the main pane.
if the main pane has focus, pressing [enter] should do a contextually-relevant thing: in file mode it should open $EDITOR to the given file, to whatever line is currently in view. in "commit" mode it should.... maybe do nothing for now.
