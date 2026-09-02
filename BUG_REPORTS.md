## CODE_REVIEW.md A6 — ungenerated inputs

Fixed in the A6 pass. Each entry names the regression test and the failure
observed *before* the fix.

- **Non-ASCII paths came back octal-escaped from every path-producing git
  call.** git quotes any path with non-ASCII bytes, a tab, or a quote by
  default (`core.quotePath`), and every call site split newline-delimited
  output and passed the quoted form through — so `café.txt` reached the
  sidebar as the literal 14-character string `"caf\303\251.txt"` and then
  failed to resolve as a filename for diffs or file content. `IgnoredEntries`
  was doubly broken: the closing quote defeated its `HasSuffix(line, "/")`
  directory check too.
  *Root cause:* no `-z` on any of the 15 path-producing calls — `diff
  --name-only` (×7, counting the D/A/cached filters), `diff --name-status`
  (×2), `status --porcelain=v2`, `ls-files` (×5).
  *Fix:* new `Git.runZ` + `splitNUL` (`internal/git/git.go`) as the single
  NUL-delimited boundary; all 15 call sites converted. `runZ` also
  deliberately does not trim, since with `-z` a path may legitimately begin or
  end with whitespace. `parseRenameNameStatus` and `parsePorcelainV2Renames`
  now consume records instead of splitting lines on tabs, which also retires
  the fragile 9-field space-walk in the latter (`strings.SplitN(header, " ",
  10)` instead).
  *Record shapes were verified against real git, not inferred:* `diff
  --name-status -z` emits `R100\0old\0new\0`; `status --porcelain=v2 -z`
  emits a rename entry's original path as a separate following record.
  *Regression tests:* `TestChangedFiles_NonASCIIPaths`,
  `TestChangedFiles_NonASCIIDeletedPath`,
  `TestChangedFiles_Rename_NonASCIIPaths`,
  `TestChangedFiles_Rename_WorkingTree_NonASCIIPaths`,
  `TestAllFiles_NonASCIIPaths`, `TestIgnoredEntries_NonASCIIPaths` (all drive
  real git in a temp repo), plus the `parsers_test.go` tables.
  *Observed pre-fix:* `Committed = ["\"caf\\303\\251.txt\"" "feature.go"],
  want it to contain "café.txt"` and `rename = {Old:original.go
  New:"ren\303\240med.go" Pure:true}`.

- **Three disagreeing tab widths.** Wrap math and `ansiAwareIterate` assumed
  8-column tab stops, `runewidth.RuneWidth('\t')` reports 0, and lipgloss
  renders a tab as 4 spaces. On tab-indented files — i.e. every Go file —
  wrap points, gutter alignment, cursor columns and drag-copy slicing
  disagreed with the render and with each other.
  *Fix:* `expandTabs` (`internal/ui/mainpane.go`), applied once where content
  enters the pane, and the downstream `'\t'` special cases deleted. Two test
  helpers that carried a *fourth* and *fifth* tab width (a flat `w += 8`, and
  `8 - (w%8)`) were converted to plain rune widths.
  *Regression tests:* `TestMainPane_TabRendersIdenticallyToFourSpaces` (a tab
  and four spaces must render identically — an assertion that does not depend
  on the expansion width itself), `TestMainPane_NoTabsSurviveTheContentBoundary`,
  `TestExpandTabs`.
  *Observed pre-fix:* at width 24, `tab: "1       return          \n    someValue +..."`
  vs `space: "1       return someValue\n    + otherValue..."` — the tab form
  wrapped a column early and broke at a different word.

- **`SetDiffAnnotations` was an unguarded second content boundary.** An
  annotation's `removedLines` render as pane rows (Shift+D) but never went
  through tab normalization, so a removed line carried a raw tab into the
  render, where lipgloss expanded it to 4 columns while the pane's own width
  math counted it as 0.
  *Fix:* `expandTabsInAnnotations`, applied in `SetDiffAnnotations`.
  *Regression test:* `TestProperty_FileViewRender_PreservesAllRemovedLines`,
  which also stopped bypassing the setter by assigning the field directly.
  *Observed pre-fix:* `removed line "\tremoved_h0o5" missing from rendered
  output`. Seed committed.

- **Search highlighting corrupted ANSI and missed matches.**
  `highlightMatchInLine` ran `strings.Index` against the *styled* string.
  Truecolor sequences are nothing but digits, semicolons and a terminating
  letter, so searching `2`, `;` or `m` spliced the highlight into the middle
  of an escape sequence and dumped escape bytes on screen as visible text;
  matches straddling a chroma token boundary were silently missed for the
  same reason.
  *Fix:* `indexVisible` builds the lowercased visible text alongside a
  per-byte map back to the styled line, so the search runs on visible text
  and the highlight is applied to whole styled spans. Lowercasing per rune
  (not over the whole string) keeps the map valid when a rune's lowercase
  form has a different byte length. The matched span is wrapped with the
  existing `applyDiffBg` re-establish-after-reset technique so inner chroma
  resets do not cancel the highlight.
  *Regression tests:* `TestHighlightMatchInLine_ANSISafe`,
  `TestHighlightSearch_ANSISafe`, and `TestProperty_HighlightMatchIsANSISafe`
  — the package's first generator that emits ANSI at all.
  *Observed pre-fix:* querying `2` on a styled `hello` produced visible text
  `"\x1b[38;2;227;194;161mhello"` instead of `"hello"`, and `ooba` across a
  style boundary in `foobar` was not highlighted at all.

- **Mouse events passed through the help overlay.** Only the wheel handler
  checked `help.IsOpen()`; click, motion and release fell through to the
  panes underneath, so clicking help text selected hidden sidebar items, set
  sidebar hover, started invisible drags and placed the main-pane cursor.
  *Fix:* the click, motion and release arms of the `Update` dispatcher return
  early while the overlay is open.
  *Regression test:* `TestMouse_HelpOverlaySwallowsClickMotionAndRelease`.
  *Observed pre-fix:* `focus changed to 0 through the help overlay`,
  `sidebar hover set to 1 through the help overlay`, `cursor placed (seq 1 →
  2) by a release on the help overlay`.

- **Mouse release moved the main-pane cursor with no drag behind it.**
  Cursor placement on release was gated only on the release y landing in the
  content band, so a release over the sidebar (whose click had cancelled the
  drag) and a bare release with no preceding click both moved the cursor.
  *Fix:* gate on `m.drag.IsActive()` captured before `Release`, plus a new
  `endpoint.OutsideSidebar`. `endpoint` also gained `DisplayCol` so the
  release and click handlers stop re-deriving the click geometry inline —
  both now consume what `clickAt` already computed, per the
  "layout geometry comes from one function" rule.
  *Regression test:* `TestMouse_ReleaseDoesNotMoveCursorWithoutADrag`, whose
  third case pins that a real main-pane click+release *does* still place the
  cursor, so the gate cannot be over-tightened.
  *Observed pre-fix:* `release in the sidebar placed the main-pane cursor
  (seq 1 → 2)` and `bare release placed the cursor with no drag in progress`.

- **Blank lines + drag-copy** (the fifth A6 bullet) was already fixed by A4:
  `stripGutterText` strips the gutter before trimming and is shared by
  `extractLineFragment` and `stripGutterDisplayWidth`. Verified, not
  re-fixed. `CODE_REVIEW.md`'s A6 text and its "verified clean" section are
  both stale on this point.

### Review-round fixes (same A6 pass)

- **`indexVisible` panicked on invalid UTF-8.** `for i, r := range line` yields
  `RuneError` for a single bad byte, but `utf8.RuneLen(RuneError)` is 3, so the
  recorded span overshot the byte the rune actually occupied — corrupting spans
  mid-string and running past the end of the line at the tail.
  *Reachable in normal use:* raw file bytes reach `SetPlainContent`, a Latin-1
  file passes binary detection and renders `U+FFFD`, and the user searches for
  the `�` they can see.
  *Fix:* manual `utf8.DecodeRuneInString` loop using the real `size` for both
  the span end and the loop increment.
  *Regression test:* invalid-UTF-8 bodies and a `�` query added to
  `TestProperty_HighlightMatchIsANSISafe`'s pools; seed committed. Verified
  non-vacuous by restoring `utf8.RuneLen` and watching the property fail.
  *Observed pre-fix:* `panic: runtime error: slice bounds out of range [:6]
  with length 4` from `highlightMatchInLine("abc\xff", "\uFFFD")`.

- **`OutsideSidebar` treated the main pane's own gutter as outside.** A click
  on the gutter clamps to column 0 and places the cursor; the release path
  declined, so the cursor moved on click and then refused to move on release.
  *Fix:* one semantics, expressed once in `clickAt` — `OutsideSidebar` is now
  `g.sidebarW > 0 && x < g.sidebarW`, the sidebar proper. The gutter belongs to
  the pane.
  *Regression test:* `TestMouse_ReleaseOnGutterPlacesCursorLikeClick`.
  *Observed pre-fix:* `release on the gutter should place the cursor, as a
  gutter click does`.

- **A release swallowed by the help overlay stranded an in-flight drag.** Help
  can be opened with `?` mid-drag; the release was then dropped without
  clearing `m.drag`, so motion kept extending the selection once the overlay
  closed.
  *Fix:* the release arm's overlay gate calls `m.drag.Cancel()` before
  returning — the gesture was interrupted, so no cursor placement and no copy.
  *Regression test:* `TestMouse_HelpOverlayReleaseDoesNotStrandDrag`.
  *Observed pre-fix:* `drag still active after the overlay swallowed its
  release`.

- **Control characters in filenames reached display text unescaped.** This one
  is *created* by the `-z` conversion: `core.quotePath` used to escape control
  characters in filenames before we ever saw them. Reading raw NUL-delimited
  output is correct — it is what lets `café.txt` display as `café.txt` — but a
  literal tab or newline in a filename now flows straight to display text. A
  raw tab hits the same runewidth-0 / lipgloss-4 disagreement `expandTabs`
  exists to prevent; a raw newline in a sidebar label breaks row math and mouse
  hit-testing outright, since a label is assumed to occupy exactly one row.
  *Fix:* one boundary function, `sanitizeDisplayText` (`internal/ui/format.go`),
  applied at the three sidebar label-construction sites in `buildTreeItems`,
  at `fileTitleLeft` (covering the rename `old → new` form), and at
  `maincontent.go`'s error-path `SetTitle`. **Representation is git's own:**
  `\t`, `\n`, `\r` by name and `\xNN` for the rest — what these filenames
  displayed as before the `-z` change, pure ASCII so width math stays trivially
  correct, and renderable in every terminal (unlike the Control Pictures block
  `␉`/`␊`, whose font coverage is patchy). A literal backslash is deliberately
  *not* escaped: doing so would rewrite every path containing one to resolve an
  ambiguity that is vanishingly rare here.
  *Identity stays raw.* `sidebarItem.filePath`, map keys and every git argument
  keep the original bytes — an escaped path stops naming a file. An initial
  attempt to sanitize at the path-split point in `buildTreeItems` was backed
  out for exactly this reason: `treeNode.path` is rebuilt from those segments.
  *Regression tests:* `TestSanitizeDisplayText`,
  `TestProperty_SanitizedFilenameIsSafeForDisplay` (no control char survives,
  single row, idempotent, control-free input untouched), and
  `TestControlCharFilenames_NeverReachDisplayText`, which asserts the boundary
  is actually *wired up* end-to-end and that `filePath` stays raw. `genFileName`
  now emits a tab-bearing filename so the whole property suite exercises it.
  *Observed pre-fix* (with the sanitizer stubbed out): `sidebar item 2 label
  "  dir/new\nline.go" spans multiple rows`, plus control chars surviving into
  five more labels and every `fileTitleLeft` result.

### Generator widenings (A6's actual subject)

The bugs above were survivable because no property test ever generated the
triggering input. Widened, all *without adding a rapid draw* (the variation is
keyed on the loop index) so every existing `.fail` seed stays replayable:

- `TestProperty_DragSelectsCorrectText` line bodies: tab-indented, deep tab
  indent with an interior tab, and mixed tabs/spaces.
- `genFileName` (new): generated filenames now cycle through non-ASCII
  letters, wide runes and spaces, feeding `genMockGit`'s committed /
  uncommitted / staged / other buckets and `genNestedFiles`.
- `genNestedFiles` directory pool gained `café`, `my docs`, `internal/日本`
  (appended, so existing seeds sample the same earlier entries).
- `genDiffLineBody` (new): diff bodies now carry tab indentation, interior
  tabs and non-ASCII text instead of being uniformly `kind_hNoM`.
- `TestProperty_HighlightMatchIsANSISafe` (new) is the first generator in the
  package to emit ANSI escape sequences, with queries drawn from the escape
  bytes themselves.

Two tests held their *own* pre-boundary copy of the content and so reported
the tab expansion as a mismatch; both now measure against the post-boundary
text the pane actually holds, which is the honest reference:
`TestProperty_DragSelectsCorrectText` and
`TestProperty_FileViewRender_PreservesAllRemovedLines`.

### Found by the widening, since fixed

Both entries that lived here — the `renderTitleRow` zero-width mis-padding
and the wide-glyph drag asymmetry — are fixed by the unified width oracle.
See "Unicode width accounting unified on one oracle" under Fixed Bugs.

**The suite is fully green.** There is no deliberate red left. The only
expected failure is `TestStartIPCListener_RoundTrip`, which fails solely
under a sandbox that forbids `bind`; it is environmental and unrelated.

## New Bugs

- **A whole-line yank still drops the source line's *own* trailing
  spaces.** Not a wrap bug — `stripGutterText` trims trailing blanks off
  every rendered row before the copy path slices it, so a line ending in
  spaces copies without them whether or not it wrapped. (In the wrapper,
  the same thing shows up as `pending` being discarded after the final
  `emit`: there is no following row for those spaces to belong to.) A
  strict reading of PROMPT.md:365 — "copied text should be the same as the
  text from the file" — calls this a copy-fidelity gap; the counter-reading
  is that trailing whitespace is invisible padding a user never meant to
  select, and the trim is what keeps rendered padding out of the copy in
  the first place.
  *Deliberately not fixed with the wrap/copy fix:* that fix restores spaces
  *between* wrap rows, which are unambiguously interior to the line. This
  one is a `stripGutterText` policy question affecting unwrapped lines
  equally, so it wants a decision rather than a patch. See INCONSISTENCIES.md.

- **Resolved as a stale test expectation, not a product bug:
  `TestProperty_InteractionInvariants`' rename title-bar check.** With a
  control character in the name it expected `titleLeft` to be the *raw*
  `"old_new4\tcontrol.go → new4\tcontrol.go"`, while production renders the
  sanitized `"old_new4\\tcontrol.go → new4\\tcontrol.go"`. Production is
  right: `TestControlCharFilenames_NeverReachDisplayText`
  (`model_test.go`) pins that no control character may survive
  `fileTitleLeft`, and a raw tab is precisely the runewidth-0 /
  lipgloss-renders-4 hazard A6's `sanitizeDisplayText` exists to remove.
  The two committed tests contradicted each other; this one was wrong.
  *Fix:* `checkRenameInvariants` now expects
  `sanitizeDisplayText(old) + " → " + sanitizeDisplayText(selected)` —
  identical byte-for-byte to sanitizing the joined string, since the helper
  is per-rune and `" → "` has no control characters. The invariant's real
  subject (the `old → new` structure and the old/new pairing) is still
  asserted independently, and the helper itself is pinned by its own table
  and idempotence tests, so the expectation is not circular.
  *Found by a fresh seed during the wrap/copy fix and verified
  pre-existing* (reproduced with that fix stashed, i.e. at `adf90a1`).
  *Regression replay:* seed `...-20260901134448-50533.fail`, green after
  the fix.

- ~~`isRateLimited` (`model.go`) classifies on substrings and treats *any*
  `"403"` in the error text as a rate limit~~ **FIXED** — see "GitHub error
  paths: classification and visibility" under Fixed Bugs. Original report
  kept for the reasoning: `isRateLimited` classified on substrings and
  treated *any*
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
  captured seed replayed cleanly at default check count. Per-property timing
  was identical with/without the SelectedText split, so this looks like a
  pre-existing performance edge under load rather than a regression.
  *Seed deleted* — rapid reported it "no longer valid" after the generator
  widening changed the draw sequence (see "Invalidated seeds" below), so it can
  no longer be replayed. `View()` has since gotten ~14x faster on an empty
  render (see the padding-complexity entry above), which may or may not cover
  this configuration; `BenchmarkViewEmpty` is now the standing guard.

## Fixed Bugs

### GitHub error paths: classification and visibility

Two bugs on the same path — the classifier that decides what a failed `gh`
call *means*, and the status line that decides whether the user ever hears
about it.

What `gh` actually emits, since both fixes turn on it: `internal/git`'s
`runExternal` (git.go:34-49) folds gh's stderr into the returned error as
`"<exit status>: <stderr>"`, so gh's own text is what reaches the classifier.
gh formats API failures as `HTTP %d: %s (%s)` for REST and `GraphQL: %s` for
GraphQL (both format strings confirmed in the gh 2.92.0 binary), where `%s` is
GitHub's own `message` field. gh does **not** print response headers, so
`x-ratelimit-remaining: 0` — the canonical signal, and the one gh itself
checks internally (`pkg/cmd/skills/search.isRateLimitError`, also in the
binary) — only reaches us if a caller captured headers (`gh api -i`). The
signals actually available in the captured text are therefore the message
texts: `API rate limit exceeded …`, `You have exceeded a secondary rate
limit …`, `Resource protected by organization SAML enforcement …`, `your
authentication token is missing required scopes […]`, `Bad credentials`,
`Resource not accessible by …`. No widening of capture was needed; stderr was
already being captured in full.

- **`isRateLimited` called any error containing "403" a rate limit.** FIXED.
  *Symptom:* a SAML/SSO 403, a token missing scopes, or any error text that
  happened to contain the digits 403 was reported on the status bar as
  "GitHub API rate limited" and — since the A3 backoff fix made the
  classification load-bearing — drove `BumpRateLimited`, doubling the poll
  interval out to the 15-minute cap for a condition no amount of waiting
  fixes. Five consecutive SSO 403s took the poll from 30s to 16m.
  *Root cause:* `isRateLimited` (model.go) was three substring tests —
  `"rate limit"`, `"403"`, `"secondary rate"` — on the lowercased error. The
  status code is not evidence of throttling: GitHub returns 403 for
  permission failures too, and returns rate-limit *evidence* separately.
  *Fix:* `isRateLimited` is replaced by `classifyGitHubError`
  (`internal/ui/gherror.go`), which returns a three-valued
  `githubErrorKind` — `ghErrRateLimited`, `ghErrAuth`, `ghErrOther` (plus
  `ghErrNone` for a nil error). Rate limit requires actual throttle
  evidence: `x-ratelimit-remaining: 0` (digit-bounded regex), the texts
  `rate limit exceeded` / `secondary rate limit`, or the GraphQL error type
  `RATE_LIMITED`. Auth-or-permission is SAML/SSO, missing-scope, bad or
  expired credential, and inaccessible-resource text, plus a bare 401/403
  matched *in status-code position* (`bareAuthStatusRe`: `HTTP 40[13]`, or
  `40[13]: forbidden|unauthorized`). Position, not digit-bounding, is what
  makes it evidence: gh appends the request URL, and that URL carries the PR
  number, so `HTTP 502: Bad gateway (…/pulls/403)` is a gateway failure on PR
  #403 — under a merely digit-bounded match it read as an auth error and told
  the user to check `gh auth status` for a 502. For the same reason a bare
  `"forbidden"` is deliberately *not* an auth signal (a branch named
  "forbidden-fix" in `no pull requests found for branch …` would trip it);
  gh's own `HTTP 403: Forbidden` is already covered by the anchored status
  match, so anchoring loses no real shape. A SHA or byte count containing
  "403" is likewise not a status code. Only
  `githubErrorKind.backsOff()` (true for `ghErrRateLimited` alone) drives
  `BumpRateLimited`; everything else takes `ResetPRInterval`, i.e. the
  normal cadence. **Cadence decision for an unrecognized 403:** normal
  cadence, error shown. A permanent condition must not spin the backoff, and
  the user sees the message on line 3 either way. "Normal cadence" means
  *unless a rate-limit backoff is already latched*: `ResetPRInterval`
  recomputes `max(activity-derived, rateLimitBackoff)` and only
  `MarkPRSuccess` clears the latch, so a rate limit followed by SSO failures
  holds the latched floor while the message flips to the auth text. That is
  deliberate — a 403 about a missing scope is not evidence the throttle
  lifted — and is now pinned by `TestAuthErrorDuringRateLimitBackoff`. The message is per-kind
  (`statusMessage()`), so an auth failure now names its own cause:
  "GitHub API auth/permission error — check: gh auth status".
  The classification travels as one field (`prRefreshMsg.errKind`,
  `gitDataMsg.prErrKind`) rather than a pair of booleans, so "backs off" and
  "is an auth error" cannot disagree by construction.
  *Regression tests:* `TestSSO403_NotRateLimited` (errorpaths_test.go) walks
  the real loop — `fetchPRStatus` classifies, `Update` applies — for five
  consecutive identical failures, asserting an SSO 403 and a missing-scope
  error leave `PRInterval` untouched while a genuine rate limit doubles it
  five times, and that each surfaces its own message on the rendered status
  bar. `TestAuthErrorDuringRateLimitBackoff` pins the latch interaction
  above, on both the PR-tick and git-load paths, through to the success that
  clears it. `TestClassifyGitHubError` (gherror_test.go) is the table, one
  row per real gh error shape, including the 502-on-PR-#403 and
  branch-named-"forbidden-fix" false positives.
  `TestProperty_ClassificationNeverBacksOffNonRateLimit` recombines labelled
  fragments of those shapes and asserts the *exact* kind against the
  generator's own ground truth (which fragments were drawn), with
  `backsOff()` following from it. Its first form asserted only "backs off ⇒
  rate limit", which is unsatisfiable by construction — `backsOff()` is
  *defined* as `k == ghErrRateLimited`, so it would have passed with SAML
  text bucketed as a rate limit. Liveness was then verified by mutation:
  temporarily adding `"saml"` to `rateLimitSignals` failed the property in 30
  draws (`classifyGitHubError("Resource protected by organization SAML
  enforcement") = rate-limited, want auth`). That run's seed
  (`TestProperty_ClassificationNeverBacksOffNonRateLimit-20260901181031-41837.fail`)
  is committed and replays green — it records a deliberate mutation check,
  not a product bug, and is kept because rapid has not reported it invalid.
  `TestFetchPRStatus_ClassifiesErrors` and `TestGitLoad_ClassifiesPRError`
  now assert kinds instead of a bool; `TestIsRateLimited` (model_test.go) is
  gone — its `{"403", …, true}` row *was* the bug.
  *Pre-fix failure lines:* `errorpaths_test.go:526: after 1 SSO 403s, PR
  interval = 1m0s, want it unchanged at 30s`; and for the URL false positive,
  `gherror_test.go:129: classifyGitHubError(exit status 1: gh: HTTP 502: Bad
  gateway (https://api.github.com/repos/o/r/pulls/403)) = auth, want other`.

- **API errors were invisible on status line 3 once PR data existed.** FIXED.
  *Symptom:* PROMPT.md:83 puts the GitHub API error message on line 3, but
  after the first successful PR fetch the line always showed the PR summary.
  Every later failure — rate limit, expired auth, network — was silent, with
  stale PR data sitting on the line looking current.
  *Root cause:* `renderStatusBar` (statusbar.go) chained
  `if pr.Number > 0 … else if prLoading … else if prError != ""`, so the
  error branch was reachable only before any PR had been fetched.
  *Fix:* the chain is a `switch` with the error first: an active error
  replaces line 3's content while it lasts, the PR summary returns when it
  clears. Precedence rather than composition — PROMPT.md:83 asks for the
  error message on this line and says nothing about combining it with the PR
  summary, and the last-known-good PR data is still rendered in the PR pane,
  so both spec clauses hold (INCONSISTENCIES.md, "GitHub API errors hidden
  once PR data exists"). "Active" is exactly `prError != ""`: it is set on
  every failure and cleared by the next successful fetch, on both the PR-tick
  and PR-inclusive-git-load paths (verified, no lifecycle change needed).
  The error text now goes through `sanitizeDisplayText` before `ellipsize`,
  like any other display string — a raw newline in an error would otherwise
  split the "row" in two and desync the status bar's row count. Row count is
  unchanged: `statusBarLineCount` already counted one line-3 row for
  `pr.Number > 0 || prError != "" || prLoading`, and every switch arm renders
  exactly one row.
  *Regression tests:* `TestRenderLine3_ActiveErrorWithPRData`
  (statusbar_test.go) — error replaces the PR summary and emits no clickable
  line-3 labels; no error keeps the summary; control characters in the error
  render as escapes and add no rows. The existing
  `TestProperty_StatusBarRenderRowsMatchLayoutRows` already fuzzes the
  error × PR × loading state space for the row-count promise and stays green.
  *Pre-fix failure line:* `statusbar_test.go:509: line 3 = "… PR #7: a pr …",
  want the active error message`.

- **Line-3 messages are hybrid: summary for classified kinds, raw text for
  the rest.** (Adjudicated during review of the two fixes above.) The
  classified kinds keep fixed, actionable text — "GitHub API rate limited",
  "GitHub API auth/permission error — check: gh auth status" — because the
  classifier has already extracted the meaning and a one-line summary beats
  gh's sentence-and-a-URL on a single status row. `ghErrOther` has no meaning
  to state, and a fixed "GitHub API error" made a DNS failure, a missing `gh`
  binary, a 502 and "no pull requests found" all render identically — a
  genuine gap against PROMPT.md:83. It now carries gh's own words:
  `statusMessageWith(detail)` renders `"GitHub API error: " + detail`, where
  the detail is the raw error text snapshotted onto the msg in the fetch
  function (`prRefreshMsg.errText`, `gitDataMsg.prErrText`) — never read back
  off Model state, per the tea.Cmd snapshot rule. Sanitizing stays at the
  display boundary (`renderStatusBar` → `sanitizeDisplayText` → `ellipsize`)
  rather than in the message builder, so the text is escaped exactly once.
  *Tests:* `TestGenericError_CarriesRawText` (network and gh-missing carry
  their text; rate-limit and auth keep their fixed summaries) and
  `TestGenericError_RawTextIsSanitizedOnLine3` (a multi-line error carrying
  `\t`, `\r` and `\x07` renders escaped, on exactly the number of rows
  `statusBarLines()` reserved).

- *Documented, not changed:* the `gitDataMsg` failure path skips
  `ResetPRInterval` where the `prRefreshMsg` path calls it. The asymmetry is
  pre-existing and inert — that path is not the poll loop, and
  `ResetPRInterval` only recomputes `max(activity-derived, latched backoff)`,
  so it cannot clear a latch (only `MarkPRSuccess` does) and both arms leave
  the same interval behind. A comment at the `gitDataMsg` site now says so,
  so nobody "fixes" it into a redundant recompute;
  `TestAuthErrorDuringRateLimitBackoff` covers both paths.

### Unicode width accounting unified on one oracle

All display-width math now goes through a single grapheme-cluster-aware
function, `displayWidth` in `internal/ui/width.go`, with `eachDisplayCluster`
as the matching walk. `TestNoDirectRunewidthOutsideOracle` (width_test.go)
parses every Go file in the repo with `go/parser` and forbids any other file
importing a width library or calling the renderer's measurement — matching on
resolved import paths rather than text, so an alias or dot-import cannot slip
past it. The single exception is `rendererWidth` / `fitToRendererWidth`, which
live in width.go precisely so the one place that must consult lipgloss's own
measure (to predict its wrapping) is the sanctioned one. Six independent width
authorities were in play before this: `runewidth.StringWidth`, four separate
`runewidth.RuneWidth` rune-walks (`splitAtDisplayCols`, `sliceByDisplayCol`,
`expandTabs`, the wrap tokenizer and its mid-token splitter), a
`lipgloss.Width`-plus-rune-slicing truncator in `statusbar.go`, and a private
rune-sum `displayWidth` in the test package. Each disagreed with the renderer,
and with the others, on some input class.

- **`renderTitleRow` mis-padded strings of zero-width runes.** FIXED.
  *Symptom:* `renderTitleRow("", "ः\u0600", 3)` returned a row of display
  width 2 instead of 3.
  *Root cause:* the row was assembled as `left + strings.Repeat(" ", pad) +
  right` with `pad = width - leftW - rightW`, i.e. from separately measured
  parts. Display width is not additive across concatenation: U+0903 merges
  backward into the padding and U+0600 (Prepend class) swallows the first
  space appended after it, so the assembled row measured narrower than the
  parts predicted.
  *Fix:* `renderTitleRow` now measures whole candidate rows and grows the gap
  to a fixed point, never accepting a candidate that overshoots; `fitToWidth`
  / `padToWidth` (width.go) re-measure the whole string after each appended
  space. It also sanitizes control characters first, so a newline in the input
  cannot split the "row" in two and make any single width unachievable.
  *Regression tests:* `TestRenderTitleRow_AlwaysExactWidth` (all three
  committed seeds replay green), plus `TestPadToWidth_ExactForAnyInput`,
  `TestPadToWidth_AbsorbedSpaces` and `TestFitToWidth_ExactForAnyInput`.
  *Pre-fix failure line:* `mainpane_test.go:1205: renderTitleRow width=3, got
  display width 2 for left="" right="ः\u0600" result=" ः\u0600 "`.

- **Drag selection disagreed with itself on the trailing cell of a wide
  glyph.** FIXED.
  *Symptom:* clicking the second cell of `日` excluded it when it was the
  start of a selection and included it when it was the end.
  *Root cause:* `sliceByDisplayCol` rounded the start up to the next rune
  boundary but let a rune straddling the end through — the two edges rounded
  in opposite directions.
  *Fix:* rounding is now an explicit policy parameter. `roundOutward` takes
  every cluster the range touches, symmetrically at both edges (selection and
  highlighting, per PROMPT.md's mouse-behavior rule); `roundInward` takes only
  clusters wholly inside (clipping a row to the pane, where a straddling glyph
  must be dropped or the content would overflow the border). Both resolve
  through one primitive, `displayColByteRange`.
  *Regression tests:* `TestProperty_DragSelectsCorrectText`'s first/last-char
  invariants now assert on **every** column — the `onBoundary` skip that
  dodged this case is gone — plus `TestSliceByDisplayCol_OutwardIsSymmetric`,
  `TestSliceByDisplayCol_NeverSplitsAnAtom`, and explicit both-policy rows in
  `TestSliceByDisplayCol`.
  *Pre-fix failure line:* `last char: selectedText() ends with "の", but screen
  position (31,5) has "テ"`.

- **A cluster split across ANSI color spans measured several cells too
  wide.** FIXED (found while widening the generators).
  *Symptom:* the syntax highlighter emits one color span per token, so a ZWJ
  family emoji arrives as `ESC[..m👩ESC[0m ESC[..m<ZWJ>ESC[0m ...`. That line
  measured 6 cells for a 2-cell glyph, and because the drag highlight rewrites
  its region with escapes stripped, applying a highlight silently changed the
  row's width — `displayWidth(s) != displayWidth(stripANSIForWidth(s))`.
  *Root cause:* segmentation broke clusters at escape boundaries.
  *Fix:* escapes are transparent to clustering — `eachDisplayCluster` segments
  over the ANSI-stripped text and reattaches escapes, so width no longer
  depends on where the highlighter chunked its spans.
  *Regression tests:* `TestWidthIgnoresEscapePlacement` and
  `TestWidthIgnoresEscapePlacement_ZWJ`.
  *Pre-fix failure line:* `drag changed styling after highlight on line 11:
  base: "\x1b[m    " drag: ""`.

- **Wrap could break inside a grapheme cluster.** FIXED. The over-long-token
  splitter stepped rune by rune, so a base character could end one row and its
  combining mark start the next. It now steps by cluster.
  *Regression test:* `TestWrapNeverSplitsACluster`.

- **Click-placed cursors could land inside a glyph.** FIXED. `clampDisplayCol`
  now snaps to the start of the cluster the click lands on, via
  `mainPane.snapDisplayColToCluster`.
  *Regression tests:* `TestCursorSnapsToClusterStart`,
  `TestCursorSnapIsIdempotent`.

### Found in review of the width-oracle change

Three real bugs caught reviewing the unification. The first two were introduced
or left in place by it; the third it made reachable in a new way.

- **`truncateToWidth` dropped every ANSI escape past the truncation point.**
  FIXED.
  *Symptom:* a status bar carrying a `makeHyperlink` (OSC 8) cut mid-link kept
  the opening sequence and lost its terminator, so the "…" and everything
  rendered after it became part of the clickable link. The SGR form of the same
  bug leaked color past the cut. Reachable through `ellipsize`
  (`statusbar.go`) whenever the terminal was too narrow for a PR line
  containing a link.
  *Root cause:* the cluster walk returned `false` — stopping iteration — at the
  first over-budget cluster. Escape sequences come in open/close pairs, so
  stopping at the cut necessarily dropped the closing half.
  *Fix:* the walk now continues to the end of the string once the budget is
  spent, emitting escapes and dropping only printable content (`ansi.Truncate`'s
  policy). Escapes carried *inside* a dropped cluster are extracted too.
  *Regression tests:* `TestTruncateToWidth_PreservesEscapesPastTheCut` (asserts
  balanced OSC 8 open/close after cutting mid-link),
  `TestTruncateToWidth_PreservesSGRReset`, and the general property
  `TestTruncateToWidth_KeepsAllEscapes` (the escape stream is unchanged by
  truncation).
  *Pre-fix failure line:* `truncateToWidth(link, 1) left 1 OSC 8 sequences,
  want 2 (unbalanced link)  got "\x1b]8;;https://example.com/pull/1234\x1b\\…"`.

- **`padToHeight` padded by counted shortfall.** FIXED.
  *Symptom:* a row whose content ended in a Prepend-class cluster came out one
  cell short of its promised width — the exact non-additive-concatenation bug
  fixed in `renderTitleRow`, still present one function away. These rows carry
  arbitrary file content through `RenderOnce` (`model.go`), so it was reachable
  from ordinary use, and it violates PROMPT.md's exact-width clause.
  *Root cause:* `lines[i] += strings.Repeat(" ", width-w)` computes the padding
  once from a measurement taken before the padding is attached; the first
  appended space is then absorbed into the preceding cluster.
  *Fix:* `lines[i] = padToWidth(lines[i], width)`, which re-measures the whole
  string after each space. Also removes a duplicate padding implementation.
  *Regression tests:* `TestPadToHeight_ExactWidthForAnyInput` (Prepend-final
  line) and `TestPadToHeight_ExactWidthProperty`.
  *Pre-fix failure line:* `padToHeight line 0 measures 9, want exactly 10 (line
  "xy ः\u0600       ")`.

- **The status bar wrapped onto an extra row, desynchronising layout from
  render.** FIXED.
  *Symptom:* on divergence-class input in a PR title, `renderStatusBar`
  returned more rows than `statusBarLineCount` promised — 4 or 5 where 3 was
  budgeted. Every click target below the wrap point shifts by a row: the
  `statusBarLines` off-by-one family CLAUDE.md records as having recurred three
  times.
  *Root cause:* the overflow guards mixed width authorities — they measured
  with `lipgloss.Width` but truncated with `ellipsize`, which measured with the
  oracle. Switching the guards to the oracle is NOT sufficient on its own, and
  that is the interesting part: `style.Width(n).Render(...)` is what performs
  the wrap, using lipgloss's own measure, so no amount of oracle-side trimming
  prevents it.
  *Fix:* the two questions are now asked in the frame of whoever answers them.
  `fitToRendererWidth` / `rendererWidth` (width.go, the one file allowed to name
  the renderer's measurement) trim until *lipgloss* agrees the string fits, and
  `ellipsize` uses them; the redundant pre-checks are gone, so there is one call
  and one authority per site. Click hit-regions deliberately stay on
  `displayWidth`, because they model where a glyph lands on the real terminal's
  cell grid — which follows grapheme clusters — rather than lipgloss's counter.
  *Regression test:* `TestStatusBarRowCountMatchesLayout` checks
  `renderStatusBar`'s row count against `statusBarLineCount`, and each row
  against the promised width, across both divergence classes plus wide glyphs,
  ZWJ clusters and decomposed Latin.
  *Pre-fix failure line:* `width 40, title "AःAःAःAःAःAःAःAःAःAः"…:
  renderStatusBar produced 5 rows but statusBarLineCount promised 3`.

- **The width oracle made padding quadratic, and `View()` started timing out.**
  FIXED.
  *Symptom:* `TestProperty_InteractionInvariants` failed under `-race` with
  `after init: View() hung for >1s (mode=0, focus=0, sidebarWidth=57, width=192,
  height=51, files=0, commits=0)` — an EMPTY model, failing after 0 tests. It
  did not reproduce in a focused run; it needed the whole package running in
  parallel under `-race`, which is why the non-race sweeps missed it.
  *Root cause:* `padToWidth` appended ONE space at a time and re-measured the
  whole string on every iteration, to defend against a Prepend-class tail
  absorbing a space. That is O(width²) per row, and `padToHeight` runs it on
  every row of every frame — so a mostly-blank 192x51 render spent essentially
  all its time there. `renderTitleRow`'s gap-growth loop had the same shape.
  *Fix:* one-shot-then-verify. Add the whole shortfall at once and keep it only
  if a single re-measure says the result is exactly the target width — correct
  by definition when it holds, whatever the tail. When it does not hold, give
  the absorbing cluster one space and retry the one-shot; a cluster can swallow
  only one space and there are finitely many, so even the hazard path converges
  in practice on the next iteration rather than stepping cell by cell.
  *Pre-fix numbers* (Apple M1 Pro, `-benchtime 50x`):

  | benchmark | before | after | speedup |
  |---|---|---|---|
  | `BenchmarkViewEmpty` (192x51, empty) | 13,571,038 ns/op | 937,015 ns/op | 14.5x |
  | `BenchmarkPadToWidth/empty` | 15,640 ns/op | 284 ns/op | 55x |
  | `BenchmarkPadToWidth/ascii` | 15,327 ns/op | 306 ns/op | 50x |
  | `BenchmarkPadToWidth/prepend_tail` | 253,385 ns/op | 5,756 ns/op | 44x |

  *Regression guard:* `BenchmarkViewEmpty` and `BenchmarkPadToWidth`
  (width_test.go), so the next regression is a number rather than a flaky 1s
  timeout. Correctness is still pinned by `TestPadToWidth_ExactForAnyInput`,
  `TestPadToWidth_AbsorbedSpaces` and `TestRenderTitleRow_AlwaysExactWidth`,
  which the fast path had to keep passing.

- **Resolved as an imprecise test expectation, not a product bug: the drag
  property's first/last-cluster invariants.** Surfaced at 300 checks by the
  widened generator, on a line containing `\u0600本` — a Prepend character
  immediately before a wide CJK glyph.
  *Symptom:* `first char: selectedText() starts with "\u0600本 1 ", but screen
  position (27,5) has " "`.
  *Cause:* uniseg segments `\u0600本` as ONE cluster and scores it **width 0**
  (a Prepend cluster reports no width regardless of what it prepends). So two
  atoms share column 3: the invisible `"\u0600本"` and the following space.
  `charAt` skips zero-width atoms because it answers "which glyph occupies this
  cell"; `displayColByteRange` attaches zero-width atoms to the content that
  follows them, per Prepend semantics, so the selection legitimately opens with
  that invisible cluster. Both are behaving as designed — the invariant was
  comparing the selection's first *bytes* against the cell's *glyph*.
  *Fix:* the invariants now compare the first and last atoms that actually
  occupy a cell (`firstVisibleCluster`, and `lastCluster` skipping zero-width
  atoms). No production change: this is the contract stated precisely, and it is
  still asserted on every column.
  *Note:* it also shows the width model inherits uniseg's view that a Prepend
  cluster is zero-width, which is visually wrong for `\u0600本` (the glyph does
  occupy two cells). That is squarely the exotic-script residue PROMPT.md puts
  out of scope, and it is reachable only from synthetic input.

#### Invalidated seeds

Widening the generators changed the draw sequence of the shared `genMockGit`
helpers, so six committed seeds could no longer be replayed. rapid reported each
of them itself — the one condition CLAUDE.md permits deletion under — and they
were deleted, none other:

```
[rapid] fail file ".../TestProperty_DragSelectsCorrectText-20260520145748-58114.fail" is no longer valid
[rapid] fail file ".../TestProperty_DragAcrossModesNoPanic-20260515122534-4472.fail" is no longer valid
[rapid] fail file ".../TestProperty_DragAcrossModesNoPanic-20260515161924-53136.fail" is no longer valid
[rapid] fail file ".../TestProperty_TreeModeNavigation-20260509234947-38218.fail" is no longer valid
[rapid] fail file ".../TestProperty_InteractionInvariants-20260520144058-13094.fail" is no longer valid
[rapid] fail file ".../TestProperty_InteractionInvariants-20260901164422-53562.fail" is no longer valid
```

The last of those is the seed written by the `View()` timeout above. It records
a timing failure at 0 draws, and rapid cannot replay it, so `BenchmarkViewEmpty`
is that bug's durable guard instead. A full verbose package run now reports zero
invalid fail files.

#### Known, deliberate divergence from `ansi.StringWidth`

The oracle models the terminal cell grid, which PROMPT.md makes ground truth.
It agrees with `ansi.StringWidth` (what lipgloss measures with) everywhere
except two classes in which that function contradicts *its own* grapheme
segmentation, and where a real terminal follows the segmentation:

1. a cluster that begins with an ASCII byte and continues into non-ASCII bytes.
   `ansi.StringWidth`'s ASCII fast path emits the base as one cell without
   consulting grapheme segmentation, then measures the continuation as if it
   began a new cluster. So `"Aः"` (ASCII + U+0903) scores 2 and `" 🏿"` (space +
   U+1F3FF emoji modifier) scores 3, though each is one cluster of width 1; and
2. a cluster split across ANSI escape spans (above).

Reproducing either would mean splitting a cluster, which the spec forbids and
which is not exotic — decomposed accented Latin text is ordinary content, and
splitting it makes a selection begin with a floating accent. Both classes are
asserted explicitly by `TestOracleDivergesOnlyWhereRendererIsSelfInconsistent`
and `TestWidthIgnoresEscapePlacement_ZWJ` rather than tolerated silently.

### Wrap/copy space loss (was a deliberate red from the A6 widening)

- **Word wrap dropped the space it broke on, so yanked/copied text lost a
  space per wrap point.** Two mechanisms, both in
  `wrapLinesWithContinuationMap` (`mainpane.go`): a space run that does not
  fit is flushed and written to *neither* row
  (`if lineWidth+tok.displayW <= currentMax { write } else { flush() }`), and
  a space run that *does* fit survives only as the ending row's trailing
  padding, which `stripGutterText` trims out of the copy. Either way the
  spaces sit outside the pane's column model
  (`absoluteColumnFromDisplay` / `wrapRowSourceColRange` are derived from
  the *wrapped* rows and contiguous by construction), so the copy path had
  nothing to reconstruct them from. Violates PROMPT.md:365 ("copied text
  should be the same as the text from the file").
  *Observed pre-fix:* `yanked line "        added_h0o2  //café" is not in
  the pane's rendered rows` (true line `"        added_h0o2  // café"`), and
  after the fix landed, the same divergence from the other side:
  `highlight/selection mismatch: highlight "…content fortesting" vs
  selectedText "…content for testing"`.
  *Fix (copy-only — rendering, cursor math, selection endpoints and
  hit-testing are untouched):* `wrapLinesWithBreaks` returns a third
  per-viewport-row slice counting the source spaces each wrap break
  consumed — a **count**, not a flag, because the tokenizer groups a run of
  consecutive spaces into one token and drops it all-or-nothing (measured:
  `"aaa      bbb"` at width 4 eats 6). `wrapLinesWithContinuationMap` is now
  a thin wrapper over it, `mainPane.wrapBreakSpaces` stores the counts, and
  `mainPane.breakSpacesBefore` reads them. `extractSourceRange` (`drag.go`)
  re-inserts them when it joins a source line's wrap rows.
  *Boundary semantics:* the consumed spaces render in no cell, so a
  selection cannot name them; they are copied exactly when the selection
  *spans* the break — it covers the ending row's last cell and the
  continuation row's first cell. Whole-line and whole-row yanks are
  therefore byte-identical to the source, while a selection that merely
  stops at a row edge gains no phantom leading/trailing space.
  *Regression tests:* the committed seed for
  `TestProperty_Model_VisualYankMatchesHighlight` (now green), plus
  `TestWrapLines_BreakSpaceAccounting` (deterministic table pinning both
  mechanisms and the multi-space count) and
  `TestProperty_WrapLines_JoinWithBreaksRestoresSource` (reversibility:
  wrap → rejoin-with-counts is the identity on the source line).
  `TestProperty_DragSelectsCorrectText`'s expected-text model reimplements
  the wrap join, so it now mirrors the same span-the-break rule; the seed
  that caught the divergence is committed.

### CODE_REVIEW.md A5 — well-encapsulated units, broken seams between them

Six items. The structural fix is `internal/ui/mainnav.go`: a `mainNav` seam
that owns every Model-level main-pane scroll, cursor move, and change to the
row↔source mapping, so the three things that must move together (cursor
visibility, visual-mode selection, cursor re-derivation across reflow) are
restored at one choke point instead of remembered at each call site.
`TestSeam_MainPaneNavigationGoesThroughNav` is the drift guard: it fails if
`model.go`, `maincontent.go` or `search.go` names `m.cursor`, a raw viewport
scroll, an unpaired pane reflow primitive, `selection.SetActive`, or
`drag.AdvanceAutoScroll`.

- **Fixed — the cursor-always-visible invariant was violated at three
  Model-level call sites.** PLAN.md states "the cursor is always inside the
  viewport" and `TestProperty_Cursor_AlwaysVisible` proves it — but only by
  driving the `cursor` struct with the correct paired calls. Hunk nav
  (`jumpToFirstDiff` / `jumpToNextDiff`), search navigation, and `setItem`'s
  no-scroll-memory fallback scrolled the viewport (or placed the cursor)
  without the other half, so `J` across a 140-row gap left the cursor
  offscreen. Fixed by routing all three through `mainNav.JumpToHunkStart` /
  `ScrollToSourceLine` / `PlaceCursorAt`, each of which pairs the scroll with
  the placement. Regression tests: `TestHunkNav_KeepsCursorVisible`,
  `TestSearchNav_KeepsCursorVisible`, and the model-level property
  `TestProperty_Model_CursorAlwaysVisible` (which drives the real dispatcher,
  not the struct). Observed pre-fix:
  `after J: cursor vpRow=0 outside viewport [5,24)` and
  `after search-next #0: cursor vpRow=0 outside viewport [1,20)`.

- **Fixed — visual-mode selection was not updated on paging, g/G or the
  wheel.** The j/k/h/l arms called `selection.SetActive` after moving the
  cursor; `g`/`G`, the forwarded page keys and `MouseWheelMsg` called
  `cursor.DragAlongScroll` and stopped there. The highlight stayed where it
  was while the cursor moved, and `y` copied a range that did not match the
  cursor. Fixed by making `syncSelection` part of `mainNav`'s shared fixups,
  so every path that moves the cursor updates the selection's active end.
  Regression tests: `TestVisualMode_SelectionFollowsViewportMotion`
  (table-driven over G/g/pgdn/pgup/wheel/J) and the property
  `TestProperty_Model_VisualSelectionTracksCursor`. Observed pre-fix, on `G`:
  `selection.active={Pos:{SourceLine:11 …} VpRow:10}, cursor endpoint={Pos:{SourceLine:183 …} VpRow:182}`.

- **Fixed — mode-switch sidebar restore matched the wrong identity.**
  `SaveSidebar` stored `sb.SelectedItem()` (filePath, else prefix+label) while
  `RestoreSidebar` compared against `item.label`, which carries tree
  indentation and no prefix. Restore-by-identity therefore never matched a
  file or a PR item; it fell through to the saved raw *index* and selected
  whatever occupied that slot. Fixed by giving both sides one canonical key:
  `SelectedItem()` now returns `itemID(item)` — the same function `SetItems`
  uses for identity preservation — and `RestoreSidebar` compares `itemID`.
  `SelectedItem()` also gained the missing bounds check on `s.selected`.
  Regression tests: the property `TestProperty_ViewMemory_SidebarRestoreByIdentity`
  (which shifts the item list between save and restore, so index coincidence
  cannot pass it) plus `TestModeSwitching_RetainsPerModeViewState`, which A4
  deferred here because it had been passing by index coincidence (beta.go sat
  at index 5 in both fixtures). Observed pre-fix:
  `restore selected "f0.go", want "d0/f1.go" (shift=0)` and
  `files mode should restore src/deep/beta.go selection, got "gamma.go"`.

- **Fixed — scope scrubbed-ness was keyed by offset, not by SHA.**
  `IsScrubbed()` was `oldOffset != naturalOldOffset || newOffset != naturalNewOffset`.
  Scrub back one commit and then make a commit: HEAD moves, `naturalOldOffset`
  catches up with `oldOffset`, and the scope silently un-pinned itself and
  snapped back to the default range with no user action. The `HEAD~N`
  indicator went stale on any new commit for the same reason. Fixed by making
  the pin explicit: `scope.pinned` is the authority, maintained by the
  endpoint movers and re-evaluated against the *SHAs* (`repin`), and
  `SyncFromLoad` takes a `pinnedOldOffset` — the same load's measurement of
  the pinned commit's distance from HEAD — so the indicator tracks the pinned
  commit instead of a cached distance. `gitDataMsg.pinnedOldOffset` carries it,
  measured in `runGitLoad` alongside the natural offsets (`-1` = no pin), which
  keeps it inside the A2 snapshot-at-dispatch convention. A pin the natural
  endpoint catches up with (rebase, base advanced onto it) is dropped, since it
  no longer describes anything different from the default.
  `TestScope_IsScrubbedConditions` — the A4 bullet deferred here, which had
  codified the offset disjunction, i.e. the bug — now states the SHA-anchored
  contract, and `arbitraryScope` generates `pinned` via `repin` so properties
  describe reachable states. New: `TestScope_ScrubSurvivesNewCommit`,
  `TestScope_PinSurvivesCommitsAndIndicatorTracksSHA`,
  `TestScope_ContractBackToNaturalUnpins`,
  `TestScope_NaturalCatchingUpToPinUnpins`.

- **Fixed — a pinned-distance measurement was applied to the wrong pin.**
  Found in review of the fix above. `SyncFromLoad` adopted `pinnedOldOffset`
  whenever it was `>= 0`, but that number is a fact about one specific commit —
  the pin as it stood *when the load was dispatched*. Scrub twice in quick
  succession (or scrub while a load is in flight) and the earlier load's
  measurement was stamped onto the newer pin, so the `HEAD~N` indicator read a
  commit short until the next load landed. The same path is taken in the
  stale-load discard branch of the `gitDataMsg` handler, which called
  `SyncFromLoad` with the outgoing pin's offset. Fixed by carrying the SHA the
  measurement was taken against — `msg.reqScrubbedBase`, which already existed
  as the A2 guard key, so no new field was needed — and applying the offset
  only when it still equals `s.oldBase`. This is the robust option rather than
  passing `-1` at the one call site: it fixes every path that can deliver a
  measurement against a superseded pin, not just the branch the review found.
  Regression test: `TestScope_StalePinnedOffsetNotAppliedToNewerPin`; the
  property `TestScope_SyncFromLoadPreservesScrub` now draws `pinnedBase` from
  {the real pin, another SHA, ""} and `pinnedOff` from `[-1, …]` so both the
  no-pin and wrong-pin branches are covered. Observed pre-fix:
  `indicator says HEAD~1 after a load measured against the previous pin "c1"; want HEAD~2, c2's own distance`
  and, from the property,
  `oldOffset=0 changed to a distance measured against "some-other-sha", not the pin "c1"; want the cached 1`.

- **Fixed — a stale cross-checkout load could wipe a fresh scrub.** Also found
  in review. Git loads finish out of order, and the branch-switch reset below
  had no way to tell a *new* checkout from an *old* load reporting the previous
  one: a slow load dispatched on branch A, arriving after a faster load had
  adopted branch B and the user had scrubbed there, read A-vs-B as a fresh
  checkout, reset the brand-new B scrub, and adopted stale branch-A `repoInfo`.
  Fixed by extending the A2 snapshot convention with a monotonic dispatch
  number: `gitLoadRequest.seq` is assigned on the Update goroutine, echoed back
  as `gitDataMsg.seq`, and compared against the `Model.gitAdoptedSeq`
  high-water mark. A msg below the mark leaves both the repo identity and the
  branch-switch reset alone. `seq == 0` means "not from a tracked dispatch" (a
  hand-built msg in a test) and is never treated as stale, which keeps the
  existing literal-msg tests meaningful. Regression test:
  `TestScope_StaleCrossCheckoutLoadDoesNotResetNewerScrub`, which drives the
  real out-of-order arrival. Observed pre-fix, all three assertions:
  `a stale branch-a load reset the scrub made on branch-b`,
  `scope.OldBase()="a-natural" after the stale load, want the branch-b pin "b-parent"`,
  `repoInfo.Branch="branch-a"; a stale load overwrote the current checkout`.

- **Fixed — the scope did not reset on branch switch** (spec PROMPT.md:232,
  "the scope resets to default on branch switch"). The `gitDataMsg` handler
  never compared the incoming branch against the current one, so after a
  checkout a scrubbed scope kept passing the old branch's pinned SHA as the
  range's outer endpoint — a commit that is typically absent from the new
  branch. Fixed by comparing `branchIdentity(m.repoInfo)` against
  `branchIdentity(msg.repoInfo)` before adopting the new repo info and, on a
  change, resetting the scope and re-dispatching a load — the same reset+reload
  pairing the `ScopeReset` key uses. The reset runs *before* the A2 pin guard,
  which then correctly discards this msg's payload (it was computed against the
  now-stale pin). `branchIdentity` is deliberately not keyed on `HeadSHA`:
  that moves on every commit, and resetting per commit would re-introduce the
  distance-vs-identity mistake above; a detached HEAD is one bucket.
  Regression tests: `TestScope_ResetsOnBranchSwitch` and its negative half
  `TestScope_SurvivesRefreshOnSameBranch`. Observed pre-fix:
  `scope still scrubbed after branch switch (OldBase="parent-sha")` /
  `scope.OldBase()="parent-sha" after branch switch, want the new branch's natural base "other-natural"`.

- **Fixed — the vpRow-canonical cursor was never re-derived across reflow, and
  resize never reflowed the pane at all.** The cursor's canonical state is a
  viewport row (design from c4914bc), so a content refresh, a `w`/`n`/`D`
  toggle, or a resize moved it to a different source line — or off the end of
  shrunken content, where `MoveDown` is a permanent no-op and `ApplyHighlight`
  paints into the pane's padding. Separately, `updateLayout` called
  `mainPane.SetSize`, which resized the viewport without re-wrapping, so after
  a resize the rows on screen stayed wrapped at the *old* width until the next
  content-setting tick. Fixed with `mainNav.Reflow`, which snapshots the
  cursor's source-space `Position`, runs the mutation, restores the position
  (unless the mutation deliberately re-placed the cursor — tracked by the new
  `cursor.seq`), clamps to the content via the new `cursor.ClampToContent`, and
  re-derives the selection's endpoints via the new `selection.Reflow`; plus
  `SetSize` now calls `refreshViewport` when the width changes. Every reflowing
  call site (`updateMainContent`, `updateLayout`, the three toggles,
  `SetSearchQuery`) goes through it. Regression tests: the property
  `TestProperty_Model_CursorSurvivesReflow` — this is PLAN.md's step-5
  resize-invariance item ("the cursor's source-space Position is invariant
  under terminal resize; only its display position changes"), generalized to
  the toggles — and the deterministic `TestResize_RewrapsMainPaneContent`.
  Note the property needed its own generator (`genReflowMock`): `genScenario`
  produces a handful of ~20-column diff lines that fit at every generated pane
  width, so a resize left the mapping untouched and the invariance was
  vacuously true — with the fix reverted the test still passed until the
  generator produced line widths straddling the whole 40..200 width range.
  Observed pre-fix, once it did:
  `reflow0 step 0 moved the cursor's source line: 20 -> 19 (w=40 h=10 wrap=true nums=true)`
  and `content rows after narrowing 200 -> 60 = 6, was 6 at the wider size`.

One test-assertion correction, not a product bug:
`TestProperty_Model_VisualYankMatchesHighlight` closed with "every non-empty
yanked line must appear in `mainPane.content`", which contradicts
PROMPT.md:162 — a `~` row renders the old text inline beside the new when the
inline diff is small, and `D` shows removed-only content as its own row, so the
copied row legitimately contains text that is not in the new-file content.
Observed at 50 rapid checks: `yanked line "oldline1" is not in pane content`,
where the row on screen really is `1 ~ ` + `old` + `line1` and the copy matches
it exactly — i.e. the property the test is named for held.

The first attempt skipped any scenario whose file had a removed-line
annotation, which review correctly rejected: the skip was file-scoped rather
than selection-scoped, and since the `nHunks == 0` fallback diff always carries
one, it left the assertion effectively dead in FilesMode — the loosened-test
pattern CLAUDE.md forbids. The assertion now compares against
`mainPane.formattedContent`, the pane's own pre-wrap rendered rows (gutter and
diff decoration applied, wrapping not yet), which is what the property is
actually named for and holds for decorated rows too. Pre-wrap is the
load-bearing detail: a yanked line is one logical row, so it matches a whole
formatted row even when the screen split it across several — which is why
comparing against the post-wrap viewport rows was not viable. Confirmed live
rather than vacuous by pointing it back at `content`, which still fails on the
recorded `oldline1` seed.

### CODE_REVIEW.md A4 — tests that encoded or masked bugs

Five items. Two were live bugs the tests were hiding (marked **fixed**);
three were test-infrastructure gaps where production behavior is unchanged
(marked **hardened**).

- **Fixed — horizontal scroll clamped `gutterWidth` columns early.**
  `mainPane.ScrollRight` (`mainpane.go`) computed
  `maxWidth := m.maxContentWidth() - m.gutterWidth`, but `maxContentWidth()`
  measures `m.content`, which is the *unformatted* source — the gutter has
  not been added yet. Subtracting the gutter from both the content and the
  viewport term cancelled it out, so the clamp landed at
  `maxContentWidth - width` instead of `maxContentWidth - (width - gutter)`
  and the last `gutterWidth` columns (4-6 in practice) of the widest line
  could never be scrolled into view. `TestScrollRight_Clamping`
  (`model_test.go`) had been loosened around it with
  `xOffset <= maxExpected+10 // allow some tolerance for gutter`, which
  admitted both the correct and the buggy value. Fixed by dropping the
  gutter from the content term. The tolerance is gone (the assertion is now
  exact) and `TestScrollRight_RightmostColumnReachable` states the contract
  observably — with and without line numbers, the tail of the widest line
  must appear in `View()` after scrolling fully right, and scrolling
  further must not move the offset.

- **Fixed — a blank line's gutter leaked into copied text.**
  `extractLineFragment` (`drag.go`) and `stripGutterDisplayWidth`
  (`mainpane.go`) both stripped ANSI, then trimmed trailing whitespace,
  then removed the gutter prefix — guarded by `len(stripped) > gw`. For a
  blank source line the rendered row is nothing *but* gutter (`"  12   "`),
  so the trim shrank it below `gw`, the guard declined to strip, and the
  line-number digits survived as if they were content: dragging across a
  blank line copied `"12"` while the on-screen highlight correctly showed
  nothing, and the cursor could sit in gutter columns on blank lines.
  Fixed by extracting `stripGutterText` (`mainpane.go`) as the single
  source of truth for "the visible body of a rendered viewport line" and
  reversing the order — strip the gutter first, then trim. Both call sites
  now go through it. Regressions:
  `TestExtractLineFragment_GutterStrippedBeforeTrim` and
  `TestStripGutterDisplayWidth_BlankLine` (`drag_test.go`), table-driven
  over blank/whitespace-only/leading-whitespace/wide-rune bodies.

- **Hardened — `TestWrapLines_*` asserted against a dead copy of the wrap
  implementation.** `wrapLinesWordBoundary` (~130 lines) and its two
  wrappers `wrapLines` / `wrapLinesWithIndent` had no non-test callers;
  production wraps through `wrapLinesWithContinuationMap`. The tests were
  green regardless of what the live copy did. All three dead functions are
  deleted and the tests (`TestWrapLines_BreaksAtWordBoundaries`,
  `_BreaksMidWordWhenTooLong`, `_PreservesShortWords`,
  `TestWrapLinesWithIndent_ZeroIndent`, `_SmallWidth`, `_WithIndent`) now
  call `wrapLinesWithContinuationMap` directly, with the assertions
  strengthened rather than relaxed (mid-word break actually verified, every
  emitted line checked against the width, `contMap` checked alongside the
  indent). No behavioral difference was found between the two copies: the
  dead one was textually identical apart from the continuation-map
  bookkeeping and having its `width <= indent` normalization done by the
  `wrapLinesWithIndent` wrapper instead of inline. The duplication had not
  yet drifted; it was removed before it could.

- **Hardened — `TestProperty_DragSelectsCorrectText` could not see the
  blank-line bug.** Two independent reasons. Its generator emitted only
  uniform, non-blank, pure-ASCII lines, and its harness re-derived the
  expected text from the rendered viewport using the same trim-then-strip
  order as the production bug — so the test agreed with the bug instead of
  catching it. The generator now draws per line from blank, whitespace-
  only, short, long (wrap-forcing), leading-whitespace, trailing-
  whitespace, CJK-width and mixed-accent shapes. The harness's three
  inline re-derivations are replaced by calls to the production
  `stripGutterText`, and a new **invariant 1a** asserts against
  source-of-truth: `extractSourceRange` emits one logical line per mapped
  source line, so the i-th copied line is checked against
  `srcLines[upper.SourceLine-1+i]` specifically — including "a blank source
  line must copy as empty" — rather than against "some source line" or
  against text re-derived from the render. Verified by reintroducing the
  trim-then-strip order and watching the property fail with
  `blank source line 2 copied as "2" (gutter leak?)`.

  A subsequence check alone is one-sided — dropping *leading* characters
  preserves it, and invariants 2/3 route through the same
  `stripGutterText`, so a helper that ate one column too many would have
  gone unnoticed by every assertion except the drag_test.go unit literals.
  **Invariant 1b** closes that: a *middle* line of a multi-line selection
  carries no column clipping (only the first and last lines take
  `upper.Column` / `lower.Column`), and a source line whose display width
  fits the pane's content columns is neither wrapped nor horizontally
  truncated — so under those two conditions the copied line must equal the
  source line exactly. Verified by making `stripGutterText` strip `gw+1`
  columns: `copied middle line 4 = "railing space body 5", want exactly
  source line 5 = "trailing space body 5"`.

  **Deleted seeds.** Widening the generator changed the property's draw
  sequence, so five pre-existing replays no longer decode against the new
  signature — rapid reports each as `fail file "…" is no longer valid`,
  which is the one case CLAUDE.md sanctions deleting a `.fail` for.
  Removed:

  - `TestProperty_DragSelectsCorrectText-20260510113158-43696.fail`
  - `TestProperty_DragSelectsCorrectText-20260515122504-4472.fail`
  - `TestProperty_DragSelectsCorrectText-20260520161647-83995.fail`
  - `TestProperty_DragSelectsCorrectText-20260520162703-48030.fail`
  - `TestProperty_DragSelectsCorrectText-20260520200620-2229.fail`

  These guarded highlight/selection-boundary and UTF-8-slicing regressions,
  all of which the widened generator now reaches far more often than the
  old uniform-ASCII one did, backed by invariants 1a/1b and by the two
  seeds captured during this work
  (`…-20260831203552-95794.fail`, `…-20260831203634-98636.fail`). The five
  seeds that still decode (`…20260414164021`, `…20260509230432`,
  `…20260509233230`, `…20260510160329`, `…20260520145748`) were kept and
  replay green.

- **Hardened — mock `gh` JSON fixtures omitted fields the real queries
  request.** `TestPRAll_WithMockGH` supplied 8 of the 20 fields in the
  `gh pr view --json` list; timestamps, labels, assignees, milestone,
  mergedBy, body, isDraft and reviewDecision were all absent, so a field
  added to the query or a mistyped struct tag changed nothing observable.
  Added `TestPRAll_FixtureCoversEveryRequestedField` and
  `TestPRChecksAll_FixtureCoversEveryRequestedField` (`git_test.go`), which
  record the actual gh argv, parse the `--json` list out of it, and fail if
  the fixture omits any requested field — then assert the parsed value of
  every previously-missing field, including the nested review/comment
  timestamps and the `link`→`URL` remapping on `CICheck`.

- **Hardened — three zero-coverage parsers.** `parseRenameNameStatus`,
  `parsePorcelainV2Renames` and `parseCommitLog` had no dedicated tests,
  only incidental exercise through happy-path integration. Added
  `internal/git/parsers_test.go`: table-driven behavior tests (input string
  → parsed value) covering empty and malformed input, rename-score
  variants, copies-vs-renames, R in either XY position, spaces in
  filenames, CRLF, unparseable dates and truncated records. These are the
  safety net for A6's planned `-z`/NUL-delimited conversion, so cases where
  current behavior is plainly wrong for exotic input are recorded as
  `CURRENT BEHAVIOR:` with an `// A6:` note rather than fixed here — most
  notably git's default `core.quotePath` octal-escaping, which makes
  `café.txt` arrive as `"caf\303\251.txt"` and pass through the parsers
  verbatim into the UI.


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
