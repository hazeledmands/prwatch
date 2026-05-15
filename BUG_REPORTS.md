## New Bugs

- renaming a file in git doesn't reflect properly in the sidebar.

- `TestProperty_DragAcrossModesNoPanic` fails on a specific rapid seed
  (`testdata/rapid/TestProperty_DragAcrossModesNoPanic/...-20260513172000-22131.fail`):
  `selectedText() contains character "+" not in viewport (mode=1)` —
  drag-selection returns content not visible on screen. Seed: width=40
  height=11 y1=0 y2=10 x1=0 x2=0 (column drag covering most of a narrow
  viewport) in CommitsMode with `hasAPIError=true`, `hasBaseCommits=false`.
  Surfaced by `./scripts/rapid 100` during the Reading B / scope-encapsulation
  refactor; underlying mismatch is pre-existing — `SelectedText` reads
  `viewport.GetContent()` (full content, including scrolled-off lines) while
  the test's viewport-membership check looks only at rendered output, so any
  vpOffset > 0 lets a stray "+" through. Seed file is committed; bug needs
  investigation separately from scope work.

- `TestProperty_DragAcrossModesNoPanic` fails on a second seed
  (`testdata/rapid/TestProperty_DragAcrossModesNoPanic/...-20260515122534-4472.fail`):
  `selectedText() contains character "x" not in viewport (mode=0)`. Same
  family of bug as the previous entry — `selectedText` reads scrolled-off
  content while the viewport-membership check only sees rendered output.
  Surfaced by widening `genMockGit` to emit diverse unified-diff shapes
  (the new `genUnifiedDiff` generator), which shifted the rapid RNG stream
  and uncovered another drag clamp/coordinate gap. Seed config: FilesMode,
  width=41 height=12, drag (1,7)→(2,2), longer-than-default fileContent
  produced by the new diff generator. Pre-existing; fix lives with the
  earlier entry.

- `TestProperty_DragSelectsCorrectText` fails on a new rapid seed
  (`testdata/rapid/TestProperty_DragSelectsCorrectText/...-20260515122504-4472.fail`):
  highlight/selection mismatch when drag starts at `y=0` (the status-bar
  row, above the content area). Model's `selectedText` includes the first
  source line; the visual highlight only covers lines 2 and 3. Same
  family as the `DragAcrossModesNoPanic` failures — model and renderer
  disagree on how to clamp a drag that begins outside the content area.
  Seed: width=60 height=15 wrap=true lineNums=false drag=(24,0)→(25,7),
  fileContent of 6 long source lines that wrap to multiple visible rows.
  Surfaced by the same `genUnifiedDiff` wiring as above. Pre-existing.

## Fixed Bugs

- Scope handle on main/master/detached HEAD jumped to HEAD~362 on the first press of either `[` or `]`, then refused to advance: `loadLocalGitData` (and `loadMoreCommits`) used `AllCommits`/`CommitCount` whenever on a main-like branch, ignoring the scrubbed base, so `commitCount` always reported total-commits-in-repo. Compounding bug: `scope-contract-forward` used `m.commits[len-1]` to find the "next commit toward HEAD," but on main `m.commits` is the full history, so its oldest entry is the root commit — pressing `[` from any scope state jumped the handle straight to the root. Fixed by (a) switching on-main-like loads to base..HEAD when scrubbed (so `commitCount` reflects the scrub), and (b) replacing the `m.commits[len-1]` lookup with a new `git.FirstChildToward(base, head)` helper that walks one first-parent step toward HEAD. Regression tests cover all three observed symptoms.

- Scope handle could briefly snap back to a stale base if a periodic git tick's `loadLocalGitData` was already in flight when the user pressed `[`, `]`, or `\`. The in-flight load captured the older `m.base`, so when its `gitDataMsg` arrived after the keypress it overwrote the new scrub state with stale counts/file lists. Fixed by adding a stale-load guard to the `gitDataMsg` handler: when `msg.base != m.base` (and both are non-empty), discard the scope-dependent fields; `m.naturalBase` is still tracked. Regression test (`TestScope_StaleLoadDoesNotOverwriteScrubbedBase`) injects a stale msg after a scrub and asserts neither `m.base` nor `committedFiles` is overwritten.

- Expanding/collapsing a directory in one sidebar section also flipped the same directory in other sections — root cause was `m.collapsedDirs` keyed by raw path, so e.g. "pkg/" under Committed and "pkg/" under All Files shared a single open/closed flag. Fixed by section-qualifying the key (`section|path`) and recording the key on each `sidebarItem`; click/h/l toggle handlers now use `SelectedCollapseKey` so the toggle is scoped to the row the user actually interacted with.

- Drag-to-copy highlighted (and would have copied) the main pane's sticky title row — `applyDragHighlight` and `selectedText` both used `contentStartY = statusRows + topBorder`, which is exactly the row containing the sticky title. Fixed by accounting for the title row (+1) when clamping drag coordinates so dragging onto/past the title falls back to the first content line. Added a property-test invariant that the title row never gets reverse-videoed by drag.

- View switched to a different file when a file was added or removed from the diff — root cause was `sidebar.SetItems` preserving the selected *index* across refreshes, so any shift in the item list put a different file under the cursor. Fixed by tracking the selected item's identity (filePath, falling back to prefix+label) and re-finding it in the new list, picking the closest match by index when the same identifier appears in multiple sections.

- Slow startup: loading screen persisted for multiple seconds — root cause was `loadGitData` being monolithic (GitHub API calls blocked local git data from rendering). Fixed by splitting Init() to load local git data and PR data as separate concurrent commands; local files/diffs/commits now appear within milliseconds while PR data fills in asynchronously.

- Multiple consecutive removed lines only showing one in file-view changed-line rendering — fixed by showing all removed lines (extras as pure deletions) before the inline/split diff comparison with the last removed line.

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
