## Fixed Bugs

- Sidebar hover highlight was off by one line — fixed by using dynamic status bar height instead of hardcoded 2.
- Drag-to-copy was copying gutter content — fixed by excluding gutter area from highlight and stripping gutter from copied text.
- Jump to next/previous diff was broken with word wrapping — fixed by mapping source lines through formatted content to viewport lines.
- Horizontal scroll was dropping ANSI styling — fixed by always emitting ANSI escape codes.
- Shift+space wasn't paging up — fixed by adding explicit handler for shift+space key combo.
- "Uncommitted changes" in commit mode was slow — fixed by using single `git diff HEAD` instead of per-file diffs.
