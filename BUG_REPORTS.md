## New Bugs

- Flaky `viewWithTimeout` assertion in `TestProperty_DragSelectsCorrectText`:
  occasional "View() hung for >1s" under stress (300+ iter), but the
  captured seed replays cleanly at default check count. Seed at
  `testdata/rapid/TestProperty_DragSelectsCorrectText/...-20260520145748-58114.fail`.
  Per-property timing was identical with/without the SelectedText split,
  so this looks like a pre-existing performance edge under load rather
  than a regression. Worth profiling View() on the captured configuration
  (13 files, FilesMode, 66×38 viewport with sidebar 19) when there's
  bandwidth.

## Fixed Bugs

- Scrolling to next/prev hunk skipped removal-only hunks. Root cause:
  `parseDiffHunks` dropped hunks with `count == 0` (the `+A,0` headers
  for pure deletions), so `diffHunks` had no entry to jump to and
  `DiffLineNumbers` only listed added/changed lines. Fixed by anchoring
  removal-only hunks to their newStart (clamped to 1 for `+0,0`) with
  `EndLine == StartLine`, and switching `jumpToFirstDiff` /
  `jumpToNextDiff` to hunk-grain nav (`nextHunkStart` over the hunk
  list). Regression: `TestJumpToNextDiff_RemovalOnlyHunk`.

- Selecting a PR-mode pseudo-entry ("(no comments)", "(no reviews)",
  "(no CI checks)") left the previous item's content showing in the main
  pane — `applyCICheckContent`'s no-match branch only updated the title.
  That stale-content state also poisoned scroll memory: capturing
  `visible.Start.SourceLine` while showing some other item's content
  produced a value that couldn't round-trip when navigating back, because
  the content under foot when restoring differed from when remembering.
  Surfaced by a 300-iter stress run during the position-refactor work
  (`testdata/rapid/TestProperty_InteractionInvariants/...-20260520144058-13094.fail`);
  fixed by clearing the main pane content in the no-match fallback so
  the pseudo-entries have predictable empty bodies and scroll memory
  round-trips cleanly.

- Renaming a file in git showed up as two unrelated entries (delete + add)
  in the sidebar instead of a single rename. Fixed by detecting renames at
  the git layer via `git diff -M` (committed + staged), `git status
  --porcelain=v2 -M` (working tree), and a content-hash fallback for the
  pure-mv-without-add case that porcelain v2 can't cross. The new
  `git.Rename{Old, New, Pure}` flows through `ChangedFilesResult.Renamed`
  into the model; sidebar shows the new path with a `[→]` badge that takes
  precedence over `[+]`/`[-]`/`[±]`, title bar shows `<old> → <new>` on
  the left, and pure renames get a `renamed · …` no-diff right side.
  Property invariants (`checkRenameInvariants`,
  `TestApplyChangeBadgesRenameWins`) and unit tests in `title_test.go`
  guard against regressions.

- `ViewportToSourceLine` (wrap branch) fell back to `i + 1` when the
  viewport top landed on a formatted row with no direct source mapping
  (a removed-line prefix above a source line, or a "tail" annotation
  past the last source line). That returned the formatted index as if
  it were a source line, which happened to round-trip with
  `ScrollToSourceLine` for some shapes by coincidence, but broke when
  the post-loop tail-row pass in `applyFileViewFormatting` (a755699)
  shifted clamping by one row — saved at fallback-i+1, restored to a
  position where `reverseMap` had a different direct hit. Fixed by
  mirroring the non-wrap branch fallback ("closest source line with
  formattedIdx ≤ i") so VTS returns the source line whose section
  contains the viewport top, regardless of which row inside that
  section we landed on. The
  `testdata/rapid/TestProperty_InteractionInvariants/...-20260518112037-70712.fail`
  seed now replays cleanly; 500-iter sweep across `InteractionInvariants`
  + the drag property tests is green.

- Drag-selection clamp diverged between the model's `selectedText` and the
  visual `ApplyHighlight` when a drag started above the content area
  (status bar / top border / title row) AND the viewport was scrolled
  (`vpOffset > 0`). `ApplyHighlight` clamped startY to the first visible
  row, but `selectedText` translated to `vpOffset + (startY - contentStartY)`
  which could land on an absolute content row that was scrolled off above
  the viewport — pulling content the user never saw into the copy. Fixed
  by recording `originStartY` on `Begin()` (the un-adjusted screen Y at
  click time, which `AdvanceAutoScroll` does NOT mutate) and using it in
  `selectedText` to detect the "click began above content" case directly;
  in that case clamp to `vpOffset` so the selection starts at the first
  visible row, matching the visual highlight. The auto-scroll case
  (original click was in content, viewport then moved out from under it)
  is preserved — `originStartY ≥ contentStartY` still translates to the
  absolute content row via the existing path. Three seeds (one each for
  `TestProperty_DragAcrossModesNoPanic` from May 13 and May 15, and one
  for `TestProperty_DragSelectsCorrectText`) replay cleanly with the fix.

- `Model.statusBarLines()` diverged from `Model.updateLayout()` on how
  many status-bar rows to count when there was a PR error but no PR
  loading state — `statusBarLines` only passed `prLoading`, while
  `updateLayout` passed `prError` too. Layout sized panes based on the
  actual 3-row status bar while `dragGeom` reported 2 rows, so all
  downstream callers (drag math, hit-testing) used a `contentStartY`
  that was one row too high. Fixed by passing `prError` through
  `statusBarLines` to match the rendering path.

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
