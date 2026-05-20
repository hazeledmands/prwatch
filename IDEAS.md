# HUNKS & THE FILES VIEW

1. I would prefer that shift+J or shift+down navigated between *hunks* instead of *diff lines*. given that the number of hunks are shown in the main pane's title bar, I imagine that there's a divergence in the definition of "hunk" and maybe something can be extracted here
2. It would be very nice to be able to click on "hunk 1/2" to open a little UI popover that lets me navigate the hunks by mouse

# SCOPE & THE FILES VIEW

1. most of the time when I am looking at changes, I want to see the diff for the ENTIRE selected scope, rather than only the change associated with the sub-scope that the file happens to be sitting in, at that point in the lifecycle. this is hard to explain, but say I'm in a branch that has committed changes to foo.rb, and there are ALSO recent uncommitted changes to foo.rb. I want to see the WHOLE diff, including both the committed changes and the recent uncommitted changes. it might also be nice to be able to change that, but I'm not sure exactly how. this needs design.

# ACTIONS/SHORTCUTS

1. `o` opens the active PR in the browser; if there is no PR but a pushed branch, it should open the branch. ideally it would also open the currently-visible line / commit etc if you're in one of those views. if there is no branch but a remote, it should just open the repo (on whatever relevant file)

# KEYBOARD SELECTION

1. We have mouse drag-to-copy, but I'd love to be able to make selections by keyboard too — vim-style visual mode. `v` for stream selection, `V` for line selection, cursor movement extends the selection, `y` copies. Line-mode is probably the most useful in a diff context — "select N lines, copy without gutter prefixes". Block-mode (vim's Ctrl-V) is interesting but has weird edge cases with diff gutters and mixed +/- lines; probably defer.
2. A selection (a pair of line positions) has the same shape as a PR comment range, so there's room to unify — the same primitive that powers "select to copy" could also power "select to leave a comment".

# SESSION / PR COMMENTS

1. I would love to be able to leave comments inline in the code and have that be visible to a claude agent running in a session in the same directory.
2. I would also love to be able to view PR comments inline, and leave PR comments for others inline.

# SEMANTIC BROWSING

1. It would be very nice if prwatch had some kind of LSP-derived understanding of the code syntax, so the user could click on identifiers to go-to-definition, find usages, look at documentation, etc.
