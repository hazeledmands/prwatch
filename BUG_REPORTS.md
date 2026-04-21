## New Bugs

- in file view mode, if there is more than one removed line in a row, I am only seeing one of the removed lines

## Fixed Bugs

- Jump to previous hunk and jump-to-hunk wrapping weren't working — fixed by using `ViewportToSourceLine()` to convert viewport scroll position to source line number before comparing against diff annotation line numbers.
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
- CI checks not showing up properly — fixed by adding `ciChecks` and `prComments` fields to `prRefreshMsg`, fetching them in `loadPRStatus()`, and updating model + UI in the refresh handler.
- CI checks not showing at all — root cause was `gh pr checks --json` using wrong field names (`conclusion`/`detailsUrl` don't exist). Fixed by using correct fields: `bucket` (pass/fail/pending/skipping/cancel) and `link`.
- Mode tab brackets caused text jumping when switching modes — fixed by removing brackets, using bold/white styling for active mode instead.
- GitHub API rate limiting — increased default refresh to 2min/15min max, now shows "GitHub API rate limited" or "GitHub API error" in status bar instead of "No PR".
- Sidebar emoji alignment — replaced emoji CI check prefixes (✅❌⏳⏭️) with fixed-width text ([✓][✗][…][-]) and 💬 with "c" for consistent column alignment.
- Header inactive mode colors too dim — changed inactive mode color from #B0A0D0 to #D0C8E8 for better contrast on purple background.
