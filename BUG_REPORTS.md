## New Bugs

## Fixed Bugs

- Tests were hitting the real GitHub API and causing rate limits — fixed by converting `TestPRInfo_NoPR` and `TestDefaultCmdRunner_Error` to use mock runners.
- CRITICAL: App thought there was no active PR even when one existed — fixed by making `PRInfo()` return errors instead of swallowing them; callers now preserve existing PR data on transient failures (rate limits, network errors).
- Mouse hover over view mode labels didn't highlight — fixed by adding `modeHoverStyle` and `modeActiveHoverStyle` with underline, tracking hover position in statusBarData.
- View mode highlighting bled into adjacent labels ("file diff" highlighted in diff mode) — fixed by applying explicit `modeInactiveStyle` to non-active modes, preventing ANSI code bleeding.
- Directory name had different background color from rest of status bar — same root cause as mode bleeding: inactive mode labels now use explicit styling so the outer `statusBarStyle` applies uniformly.
- Drag-to-copy with word wrap wasn't implemented — fixed by building an explicit `wrapContinuation` boolean map during word wrapping, replacing the heuristic gutter-space detection.
- Sidebar hover highlight was off by one line — fixed by using dynamic status bar height instead of hardcoded 2.
- Drag-to-copy was copying gutter content — fixed by excluding gutter area from highlight and stripping gutter from copied text.
- Jump to next/previous diff was broken with word wrapping — fixed by mapping source lines through formatted content to viewport lines.
- Horizontal scroll was dropping ANSI styling — fixed by always emitting ANSI escape codes.
- Shift+space wasn't paging up — fixed by adding explicit handler for shift+space key combo.
- "Uncommitted changes" in commit mode was slow — fixed by using single `git diff HEAD` instead of per-file diffs.
