## New Bugs
- CRITICAL: the app consistently thinks there is no active PR, even when there is
- mouse hover over the view mode at the top of the status bar does not highlight
- view mode highlights "file" when in file mode, "file diff" when in diff mode, and "file diff commits" when in commits mode. it should just highlight the existing mode
- the name of the directory has a different background color from the rest of the top status bar for some reason
- drag to copy selection with word wrap is not implemented. we should implement it.

## Fixed Bugs

- Sidebar hover highlight was off by one line — fixed by using dynamic status bar height instead of hardcoded 2.
- Drag-to-copy was copying gutter content — fixed by excluding gutter area from highlight and stripping gutter from copied text.
- Jump to next/previous diff was broken with word wrapping — fixed by mapping source lines through formatted content to viewport lines.
- Horizontal scroll was dropping ANSI styling — fixed by always emitting ANSI escape codes.
- Shift+space wasn't paging up — fixed by adding explicit handler for shift+space key combo.
- "Uncommitted changes" in commit mode was slow — fixed by using single `git diff HEAD` instead of per-file diffs.
