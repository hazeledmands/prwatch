## New Bugs

- `isRateLimited` (`model.go`) classifies on substrings and treats *any*
  `"403"` in the error text as a rate limit — so a SAML-enforcement or
  permission 403 (`gh` on an SSO-protected org, a token missing `repo`
  scope) is reported as "GitHub API rate limited" and, since the A3 fix,
  now also drives the backoff: the poll doubles out to the 15m cap for a
  condition no amount of waiting fixes, and the status bar names the wrong
  cause. The classifier predates A3; A3 made it load-bearing, which is why
  it's worth fixing now rather than later. A fix wants the real signal
  rather than the substring: `x-ratelimit-remaining: 0` / the
  `RATE_LIMITED` GraphQL error type / "secondary rate limit" text, with a
  bare 403 treated as an auth-or-permission error (message, no backoff).
  Deliberately not fixed in the A3 pass — out of the reviewed scope, and it
  wants a decision about how much of the `gh` error surface to parse.

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

### CODE_REVIEW.md A3 — error paths collapsed into adjacent-but-wrong states

- `m.err` was set by a failed git load and never cleared anywhere in the
  codebase, so the error screen was terminal: one transient failure (an
  `index.lock` collision during a concurrent rebase, a momentary
  permission error) wedged the UI on "Error: … Press q to quit." for the
  rest of the session while the 5s git tick kept loading good data behind
  it. Fixed by clearing `m.err` on every non-error `gitDataMsg`. All of
  them qualify: the msg only exists because `RepoInfo`/`ChangedFiles`/
  `Commits` all succeeded, so a local-only refresh, a load whose *PR* half
  failed (that failure has its own display, `prError`), and a load the
  stale-scope guard discards each prove the local half recovered.
  Regression: `TestErr_ClearedBySuccessfulLoad` (table over all four
  follow-up msg shapes; asserts both the field and that `View()` stops
  rendering the error screen).

- Every PR-fetch error was reported as a rate limit: `fetchPRStatus`
  mapped *any* `PRAll()` error to `prRefreshMsg{rateLimited: true}` while
  `isRateLimited()` — which exists precisely to classify — was referenced
  only from its own unit test. Expired `gh` auth, a DNS failure or a
  branch with no PR all displayed "GitHub API rate limited" forever, and
  each one also doubled the poll interval for a condition that backing off
  can't fix. Fixed by classifying through `isRateLimited()`: rate limits
  keep the rate-limit path (message + backoff), everything else takes the
  generic path, which sets the same `"GitHub API error"` the PR-inclusive
  git load already used and leaves the interval alone. Both paths still
  preserve the PR data already on screen. **Recurrence:** this is the same
  conflation class logged as fixed in "CRITICAL: App thought there was no
  active PR even when one existed" (below) — that fix made `PRInfo()`
  return errors instead of swallowing them, but the UI layer then flattened
  every returned error back into one state, re-losing the distinction one
  level up. Regression: `TestFetchPRStatus_ClassifiesErrors` (table over
  primary/secondary rate limit, 403, expired auth, network, no-PR),
  `TestPRRefresh_NonRateLimitErrorTakesGenericPath`.

- Rate-limit backoff was a no-op end to end, so under sustained rate
  limiting the app kept hammering GitHub every 30s — violating PROMPT.md:21
  ("respond to rate limits appropriately, backing off as needed"). Two
  independent causes: (1) `BumpRateLimited` doubled `prInterval`, but the
  next `prTickMsg` called `ResetPRInterval`, which recomputed the interval
  from activity state and overwrote the bump; (2) the tick that delivered
  the 403 had *already* scheduled the next tick, at the un-bumped interval,
  before the result existed — so even a surviving bump governed nothing.
  Both halves are fixed inside the state machine: `activityTracker` latches
  `rateLimitBackoff` as a *floor* on the interval —
  `effectivePRInterval = max(ComputePRInterval(now), rateLimitBackoff)`,
  which `ResetPRInterval`/`PRFetchDue`/`PRTickDelay` all read, so going idle
  mid-backoff still slows to the idle rate instead of speeding a
  rate-limited app back up to 60s (found in review; the latch had been
  *replacing* the computed interval) — and only
  `MarkPRSuccess` (a fetch that actually returned) clears it and decays the
  interval back to the activity-derived value; `MarkPRFetch` + `PRFetchDue`
  hold back a tick that was armed before the bump existed and comes due
  sooner than the backoff allows, and `PRTickDelay` re-arms it for the
  remainder of the backoff so the held-back fetch goes out as soon as the
  backoff permits rather than a whole cycle later. (Making the poll
  self-clocking — arming the next tick from the `prRefreshMsg` handler —
  was tried first and rejected: `invariant_test.go`'s `execSafeCmd`
  executes returned commands, so a handler returning a real `tea.Tick`
  blocks the property suite on a live 30s timer.) The old
  `TestRateLimitBackoff` asserted the doubling without ever interleaving a
  tick, which is why it stayed green across both bugs; it is kept and
  joined by `TestRateLimitBackoff_TickLoop`, which walks the real loop
  (tick → fetch → 403 → bump to 60s → the already-armed tick fires and is
  refused a fetch, re-armed for the remainder → backoff elapses → fetch,
  next tick at 60s → second 403 → 120s → success → back to 30s) through a
  stubbed `prTickScheduler`, and by `TestActivityRateLimitBackoffProperties`, a
  rapid property over arbitrary bump/tick/success/ui/fetch/advance/goIdle
  interleavings — time is an explicit op, so idle-vs-backoff is reachable
  (monotonic under consecutive failures, cap respected, never below the
  activity interval while backed off, cleared only by success).
  `TestActivityBackoffNeverBelowActivityInterval` pins the idle case
  directly. The PR-inclusive git load participates in the same state
  machine: it classifies its own `PRAll` error (`gitDataMsg.prRateLimited`)
  and bumps, and a success on that path clears the latch — otherwise a
  manual refresh that proved GitHub had recovered still left the tick loop
  backed off for up to 15 minutes, and a rate limit on that path rendered
  as a generic error with no backoff. Regression:
  `TestGitLoadPRPath_ParticipatesInBackoff` (success / rate limit / other
  error), `TestGitLoad_ClassifiesPRError`.

- `PRChecksAll` swallowed every failure, returning a zero `PRChecksResult`
  with a nil error, and both callers applied the zeros — so a transient
  failure on the checks call silently blanked the CI panel and reset the
  CI status to empty. Worse, `gh pr checks` exits nonzero precisely when
  checks are *failing or pending* while still writing the requested JSON,
  so the swallow was also discarding perfectly good data for the case
  users care most about. Fixed by parsing the output first (nonzero exit
  with parseable JSON is data, not an error) and returning the error when
  nothing parseable came back. UI decision: a checks-fetch failure keeps
  the checks already on screen (`checksFailed` on both `prRefreshMsg` and
  `gitDataMsg` suppresses the CI assignments) rather than blanking or
  raising a banner — stale CI beats no CI, and the display policy for
  GitHub errors is still open in INCONSISTENCIES.md. Preservation is gated
  on the PR number being unchanged: across a branch switch or a recreated
  PR the old checks would otherwise render beneath the new PR's header, so
  those are cleared and the panel waits for a fetch that succeeds
  (`TestChecksError_ClearedWhenPRChanged`). The failed fetch also
  no longer counts as a server-side CI-state change for the adaptive
  refresh. `TestPRChecksAll_Error` asserted the swallowing as desired
  behavior — the test encoded the bug — and now asserts the error;
  `TestPRChecksAll_InvalidJSON` likewise. New:
  `TestPRChecksAll_NonZeroExitWithOutput`,
  `TestChecksError_PreservesPreviousChecks` (prRefreshMsg + gitDataMsg),
  `TestGitLoad_ReportsChecksFailure`.

- `BehindCount` returned 0 on any error, conflating "up to date" with "we
  couldn't tell" — a base ref that isn't fetched locally reported the
  branch as caught up. Fixed by returning `(int, error)`; `runGitLoad`
  records `behindKnown`, and the status bar renders the "N behind" segment
  only for a count that was actually measured, so an unknown count is
  hidden rather than shown as a wrong number. Regression:
  `TestBehindCount_UnknownRef` (git), `TestBehindCount_UnknownIsHidden`
  (model + `renderStatusBar`).

### CODE_REVIEW.md A2 — async `tea.Cmd` plumbing patched one-off instead of by convention

- Data race: the git loads were dispatched as bound method values
  (`m.loadLocalGitData`, `m.loadGitData`, `m.loadMoreCommits`), so their
  bodies ran on bubbletea Cmd goroutines while reading `m.scope.IsScrubbed()`
  / `m.scope.OldBase()`, `m.commitsLoaded` and `m.prInfo.BaseRef` — all of
  which `Update` mutates on the main goroutine. `go test -race` reports it:
  read at `scope.IsScrubbed()` (`scope.go:62`) from
  `(*Model).loadLocalGitData` vs. previous write at `scope.ExtendBack()`
  (`scope.go:88`) from `(*Model).handleKey` — i.e. a periodic git tick
  overlapping a `[` / `]` keypress, which is the ordinary interactive case,
  not a corner. Fixed by establishing the convention CLAUDE.md already
  describes: `gitLoadRequest` is a dispatch-time snapshot of every field a
  load reads, `m.gitLoadCmd(withPR)` / `m.loadMoreCommitsCmd()` build it on
  the Update goroutine, and the closure captures the snapshot only. The
  other Cmd-producing paths (`loadPRStatusCmd`, `loadNonGitFilesCmd`,
  `expandIgnoredDirCmd`) were converted from
  `m.`-reading methods/closures to free functions over explicit parameters
  so the convention is uniform rather than a special case for the three
  racy ones. Regression: `TestGitLoadCmd_NoModelStateRace`,
  `TestGitLoadCmd_PRVariantNoModelStateRace`,
  `TestLoadMoreCommitsCmd_NoModelStateRace` (each drives the real Cmd on a
  goroutine while `Update` scrubs the scope for 30ms, so `-race` observes
  the overlap).

- `moreCommitsMsg` had no stale guard: the handler appended
  unconditionally. A scrub landing mid-flight appended a page computed
  against a different base, and the two "load more" trigger paths (click,
  `model.go` mouse handler; enter, `handleEnter`) could both fire before
  either result landed, appending the same page twice (observed: 200
  commits where 150 exist). Fixed by carrying the dispatch-time `base` and
  `skip` in the msg and appending only when both still match current state,
  plus a `moreCommitsPending` marker so the second dispatch returns a nil
  Cmd instead of running a duplicate git query. The marker is cleared on
  every `moreCommitsMsg` including the error case (the msg now carries
  `err` rather than the Cmd returning nil), so one failed page can't wedge
  pagination for the session. Regression:
  `TestMoreCommits_DiscardedWhenScopeMovedMidFlight`,
  `TestMoreCommits_DuplicateDispatchAppendsOnce`,
  `TestMoreCommits_ErrorClearsInFlightMarker`.

- Stale-load guard misfired on legitimate base movement: it compared
  `msg.queryOldBase` against `m.scope.OldBase()`, so when the natural base
  moved (rebase, base branch advanced) the load that *detected* the new
  base — and queried against it — had its own results discarded. `scope`
  then adopted the new natural base via `SyncFromLoad` while the commit
  list, changed files and base commits stayed at the old base, so counts
  and sidebar disagreed until the next poll. Fixed by making the guard ask
  "is this answer still an answer to a question I'm asking?" instead of
  "does this base match?": the msg carries `reqScrubbedBase`, the
  dispatcher's snapshot of the *user's* pin (`scope.OldBase()` when
  scrubbed, `""` when natural), and the handler discards only when the
  current pin differs. Natural-base movement leaves the pin at `""` on both
  sides, so the fresh data is applied; a scrub, un-scrub, or re-scrub
  mid-flight still discards. Regression:
  `TestGitData_AcceptedWhenNaturalBaseMoves` (failed pre-fix with
  "commits = 100, want 7"), `TestGitData_DiscardedWhenUserScrubsMidFlight`,
  `TestGitData_DiscardedWhenUserUnscrubsMidFlight`.

### CODE_REVIEW.md A1 — paired computations maintained in N places

- Status-bar height computed three ways: `View()` used
  `m.loading && m.git != nil` for `prLoading` while `statusBarLines()` and
  `updateLayout()` used `(m.loading || !m.prLoadedOnce) && m.git != nil`. In
  the startup window after local git data lands but before PR data, layout
  reserved 3 rows while render emitted 2, putting every click/hover/drag one
  row off; and because `View()` early-returns on `m.loading`, the
  "Loading from GitHub…" line was unreachable. Fixed by making
  `Model.statusBarData()` the one place the bar's inputs are assembled —
  `View()` renders from it, `statusBarLines()` counts from it, and
  `updateLayout()` calls `statusBarLines()`. The loading row was revived
  rather than deleted (PROMPT.md: show what we have, say "loading from
  github" on the GitHub header), so it now renders during the
  `!prLoadedOnce` window that layout was already sizing for. Regression:
  `TestStatusBarRows_RenderMatchesLayout`,
  `TestProperty_StatusBarRenderRowsMatchLayoutRows`,
  `TestView_UsesSharedStatusBarData`.

- Quit-confirm height: `renderStatusBar` replaces the whole bar with a
  single line while `confirming`, but `statusBarLineCount` ignored
  `confirming` and still reported 2–3. The panes were sized 1–2 rows short
  (visible as dead rows at the bottom of the golden snapshot), and clicks at
  y=1/y=2 routed to status-bar handlers — the line-2 fallback could switch
  to commits mode mid-confirm. Fixed by returning 1 from
  `statusBarLineCount` when `confirming`. Covered by the same two tests;
  `testdata/golden/confirm_quit.txt` regenerated (panes now reach the
  bottom of the screen).
  Follow-up found in review: making the count depend on `confirming` also
  made every *toggle* of it a layout change, and the three toggle sites in
  `handleKey` assigned the field directly — so pressing `q` left the panes
  sized for the old 2–3-row bar until the next data-driven relayout.
  `TestSnapshot_ConfirmQuit` couldn't catch it because it sets the field
  before `RenderOnce`, which lays out fresh. Fixed by routing all three
  through `Model.setConfirming`, which relayouts. Regression:
  `TestConfirmQuit_RelayoutsOnKeypress`, driving the real key path (`q`,
  cancel, `esc` in, `esc` out).

- Search and `$EDITOR +N` used raw viewport offsets as line numbers.
  `searchOverlay.navigateToCurrent` called `ScrollToLine` (a bare
  `SetYOffset`) with a content-line index, and `currentLineNumber` returned
  `ScrollTop() + 1`. With word wrap on (the default) or removed-line rows
  present, both are off by the number of wrap rows above the target: search
  landed well above the match and the editor opened at the wrong line.
  Fixed by routing search through `ScrollToSourceLine`, `currentLineNumber`
  through `viewportToSourceLine`, and deleting `ScrollToLine` so there is no
  raw "scroll to row N" entry point left to misuse. The wrap-walk in both
  converters was extracted to `formatLineToViewportRow` /
  `viewportRowToFormatLine` and now also applies to diff content (which has
  no source→format map and so previously skipped wrap handling entirely).
  Regression: `TestSearch_NavigatesToSourceLine_Wrapped` (plain + diff
  content), `TestCurrentLineNumber_WrapAware`.

- Behind-count base resolved three different ways: `loadGitData` fell back
  to `info.Upstream` (the branch's *own* remote ref, so it counted how far
  behind the branch was from its own remote copy), `loadLocalGitData`
  hardcoded `origin/main` and ignored the upstream entirely, and
  `renderLine2` displayed a third answer derived from `Upstream`'s last path
  segment — so the status bar could read `feature → develop` while the
  behind count was measured against `origin/main`. Fixed by extracting
  `baseRefForBehind` / `baseBranchName` (`internal/ui/basebranch.go`),
  following PROMPT.md's detection chain with what the model already knows:
  PR baseRefName → tracked upstream when it names a *different* branch →
  `origin/main`. All three call sites converted. The remote-prefix strip
  takes off the FIRST path segment only: `--abbrev-ref @{upstream}` returns
  `<remote>/<branch>`, and branch names routinely contain slashes, so a
  last-segment strip read `origin/hazel/ui/foo` as branch "foo" — which
  both made step 2 fire against the branch's own remote ref (the exact case
  step 2 exists to skip) and displayed a `release/1.2` base as "1.2".
  Regression: `TestBaseRefChain_SingleSource`, including slashed-branch and
  slashed-PR-base rows.

- Diff `+++`/`---` file headers detected four ways, three of them wrong.
  `parseDiffHunks`, `parseDiffAnnotations` and `shortstatFromDiff` skipped
  any line with a `+++`/`---` prefix even inside a hunk body, so `+++i;`
  (adding `++i;`) was dropped from the insertion count and every later
  annotation in the hunk shifted; `colorDiff` used `"+++ "`, which is also
  insufficient because removing a SQL/Lua `-- comment` produces the body
  line `--- comment`, trailing space included. Fixed by recognising headers
  *positionally*: the new `diffScanner` (`internal/ui/diffparse.go`) is fed
  every line in order and treats `---`/`+++` as headers only before a file
  section's first `@@`. All four consumers classify through it. The dead
  first parse loop in `parseDiffAnnotations` (results discarded, and
  carrying yet another header-skip set) was removed — it was a drift seed
  for exactly this bug. Regression: `TestDiffHeaderDetection_BodyLines`,
  `TestDiffHeaderDetection_MultiFile`,
  `TestProperty_DiffBodyLinesAreNeverHeaders`.

- `openPRItemURL` matched PR sidebar rows by substring
  (`strings.Contains(label, author)` / check name), so two comments by one
  author both opened the newest one, a review row could resolve to a
  comment, and CI check "build" claimed the "build-arm" row.
  `applyCICheckContent` had the same substring bug for CI checks. Fixed by
  giving the PR rows one set of label builders (`internal/ui/pritems.go`)
  that `buildPRSidebar` renders from and that `matchPRComment` /
  `matchPRReview` / `matchCICheck` match against exactly; `openPRItemURL`
  is now a thin wrapper over the pure `prItemURL`. `firstCIFailureIndex`
  (`selectfirst.go`) had the same substring match — "jump to first failing
  check" selected the "build-arm" row when "build" was the failing check —
  and is now an exact label comparison. Regression:
  `TestPRItemURL_ExactIdentity`.

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
