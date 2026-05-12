# Catalog: factoring candidates in `internal/ui/model.go`

## Status

18/18 catalog items have landed (see commits `4920d80…2221658`). `model.go`
shrank 4265 → 2264 lines. Build and tests pass.

§6 drag was originally shallower than the catalog called for; the follow-up
in **TODO §A** below has now landed (`dragSelection` owns the six fields
plus the four geometry-using methods). Three smaller follow-ups remain:
§B (property tests for the encapsulated state machines), §C (deeper
main-content extraction — optional), and §D (minor cleanups). The original
catalog (all marked `[x]`) remains below as a reference for the
cluster/dependency analysis.

---

## TODO

### A. Encapsulate drag selection state (was §6) — DONE

Landed: `dragSelection` type in `drag.go` owns startX/Y, endX/Y, active,
scrollDir. `Model` holds a `drag *dragSelection`. Methods that need screen
geometry take a `dragGeometry` struct built by `Model.dragGeom()` at the
call site (matches `panelayout.go`'s pure-function style — chosen over an
interface to avoid four trivial wrapper methods on `Model`). Clipboard
methods (`copySelection`, `yankPath`, `copyToClipboard`) stay on `Model`
since they touch `cmdFactory` and `notification`.

Dedicated drag tests still pending in §B below.

### B. Add property tests for the encapsulated state machines

**Problem.** The catalog spelled out concrete invariants for §6 (drag), §7
(help), §8 (search), and §9 (viewMemory). Of those, only `search.go` has
internal tests (via `prdescription_test.go`-style rapid tests landing in
other files). The state machines are now encapsulated and trivially
unit-testable, but the dedicated tests haven't been written. They're
covered transitively by `invariant_test.go` end-to-end renders, not
directly.

**What needs doing.** Create the four missing test files (`drag_test.go`
is done — see below):

`internal/ui/help_test.go` — invariants from §7:
- `Open` then `Close` is a no-op (idempotent).
- After `Open`, `searchQuery == ""` and `searchMatches == nil`.
- `searchIdx` always in `[0, len(searchMatches))` while non-empty.
- `n`/`p` navigation wraps in both directions.
- Scrolling never goes past `len(lines) - visibleHeight` nor below 0.

`internal/ui/search_test.go` — invariants from §8:
- After `Clear`, all five fields are zero-valued.
- `matchIdx` always wraps in `[0, len(matches))`.
- `Open` followed by `Enter` on empty query closes the overlay.
- Backspace to empty exits search (same as Enter on empty).
- `Confirmed` is true iff `searching` is false AND `len(matches) > 0`.

`internal/ui/viewmemory_test.go` — invariants from §9:
- `SaveSidebar` then `RestoreSidebar` is identity for sidebar selection
  when the saved item is still present in `sb.items`.
- `RestoreSidebar` is a no-op for a mode with no saved state.
- `RememberMainScroll` with empty `key.item` is a no-op.
- `RecallMainScroll` returns the most recently remembered value for that key.

`internal/ui/drag_test.go` — DONE. Seven property tests landed:
- State machine: `Begin` initializes (active, scrollDir=0, no range);
  `MoveEnd` updates only the end; `Release` returns true iff active and
  always deactivates + zeroes scrollDir; `Cancel` is idempotent.
- `ApplyHighlight` round-trip: `stripANSIForWidth(highlight(v)) ==
  stripANSIForWidth(v)` — the highlight only adds ANSI codes.
- Gutter clipping: when drag starts left of the gutter, no `\x1b[7m`
  ever appears at a display column inside the gutter region.
- `AdvanceAutoScroll` termination: each call either advances the
  viewport by some delta (and re-anchors `startY` by the same delta) or
  sets `scrollDir = 0`; repeated calls always terminate.

The catalog's "selectedText/highlight coincide" invariant is already
covered by `TestProperty_DragSelectsCorrectText` in
`invariant_test.go` (its Invariant 0, plus seven sibling invariants),
so the dedicated `drag_test.go` skips it.

**Pattern.** Use `pgregory.net/rapid` with property tests, matching the
existing `activity_test.go` / `scope_test.go` / `prdescription_test.go`
style.

### C. Deepen the main-content extraction (was §12)

**Problem.** `internal/ui/maincontent.go` split `updateMainContent` into
per-mode methods (`updateFilesModeContent`, etc.), but those methods still
read freely from `Model` and mutate `m.mainPane` directly. The catalog
called for "return a struct, apply separately" — pure compute functions
that produce a result record, then a small applier that writes it to
`mainPane`. Only the leaf helpers `buildCommentContent`,
`buildReviewContent`, `buildCIContent` got that treatment.

**What needs doing (optional — lower value than §A or §B).** Decide whether
the catalog's framing is worth the churn. Two paths:

1. **Drop the framing.** Update §12 in the catalog to "split into smaller
   methods", and stop. The current state is already much more readable
   than the 290-line switch.

2. **Finish the extraction.** Convert each per-mode arm into a pure
   function returning a `mainPaneState` record:
   ```go
   type mainPaneState struct {
       filename, content, titleLeft, titleRight, diffPrefix string
       diffAnnotations map[int]diffAnnotation
       diffHunks       []diffHunk
       plain           bool
   }
   ```
   Then a single applier method writes that to `m.mainPane`. This makes
   per-mode content trivially testable (no `mainPane` mock needed), at
   the cost of one more layer.

Recommend (1) unless you find yourself needing to mock `mainPane` for
content tests.

### D. Minor cleanups noted in the catalog but skipped

1. **`extractDirs` is still called three times in
   `internal/ui/sidebarbuild.go:70-72`** — the catalog called this out as
   "called four times… do it once" but it survived the §11 extraction.
   Compute once at the top of `buildFilesSidebar` and pass the slice in.

2. **`internal/ui/fileclass.go` kept the linear-scan `containsString`**
   rather than building map-backed sets. Catalog §13 mentioned this as a
   perf win on large repos. Optional — purely a performance optimization,
   not a structural correctness issue.

---

## Original catalog (reference)

All 18 items are marked `[x]` per the commits. See the TODO section above
for follow-up work on the items that landed shallower than recommended.

### Context

`internal/ui/model.go` was 4265 lines and defined a `Model` struct with ~55
fields and ~73 methods, plus ~25 free functions. The struct mixed at least
a dozen distinct concerns (git data, modes, sidebar/main-pane state,
search, help, drag selection, refresh cadence, RWX log cache, layout,
notifications, etc.). Most logic in the file was dispatched through
`Model` even when its actual dependencies were narrow.

Ranking heuristic per candidate: **value** = how much surface area this
removes from `Model` × how testable-in-isolation the extracted piece is.
**ease** = how narrow the dependencies on the rest of `Model` are and how
mechanical the extraction is.

---

### Tier 1 — high value, low risk (pure free functions hiding as methods)

#### 1. [x] PR description rendering — `renderPRDescription`
- **Location**: `model.go:4024-4120` (~100 lines).
- **Surface area**: 1 method.
- **Dependencies on Model**: `prInfo`, `prReviews`, `prDeployments`, `mainPane.width`.
- **Extraction**: free function `renderPRDescription(prInfo, reviews, deployments, width int) string` in a new `prdescription.go`.
- **Invariants worth testing**: idempotent for same input; output always contains the PR number and title; reviewer line absent iff `prReviews` empty; markdown rendering errors fall back to raw body.
- **Result**: clean extraction; 5 rapid property tests in `prdescription_test.go`.

#### 2. [x] Scope handle info — `scopeHandleInfo`
- **Location**: `model.go:3440-3449` (~10 lines).
- **Surface area**: 1 method.
- **Dependencies on Model**: `base`, `naturalBase`, `commitCount`.
- **Extraction**: free function `scopeHandleFromBase(base, naturalBase string, commitCount int) *scopeHandleInfo`. The `scopeHandleInfo` type already lives in `statusbar.go`.
- **Invariants worth testing**: returns nil iff `base == naturalBase`; sha7 is always 7 chars when base is longer; `headOffset` equals `commitCount`.
- **Result**: clean extraction; 3 rapid property tests in `scope_test.go`.

#### 3. [x] Refresh cadence — `computePRInterval` / `computeGitInterval`
- **Location**: `model.go:3251-3272` (~22 lines).
- **Surface area**: 2 methods + 5 fields (`prInterval`, `gitInterval`, `lastUIEvent`, `lastServerChange`, `lastGitChange`) + 3 mutation sites that set timestamps.
- **Extraction**: a small `activityTracker` type with `MarkUIEvent`, `MarkServerChange`, `MarkFSEvent`, `PRInterval(now)`, `GitInterval(now)`.
- **Invariants worth testing**: any recent UI event keeps both intervals at "active"; `GitInterval` returns active when EITHER UI or FS is recent; intervals are monotonic w.r.t. quiescence.
- **Result**: clean encapsulation; 5 rapid property tests in `activity_test.go`.

#### 4. [x] Pure parsing helpers (already free, just scattered)
- `parseHunkNewStart`, `parseHunkHeader`, `isBinaryContent`, `shortstatFromDiff`, `relativeTime`, `pluralize`, `formatAuthorAndTime`, `commitTitleLeft`, `matchNumberedItem`, `reviewStateLabel`, `ciBucketOrder`, `extractDirs`.
- **Extraction**: moved to `format.go` / `diffparse.go` peer files.

#### 5. [x] ANSI / display-width helpers
- `padToHeight`, `splitAtDisplayCols`, `stripANSIForWidth`, `displayWidthOf`, `sliceByDisplayCol`.
- **Extraction**: moved to `ansiwidth.go`.

---

### Tier 2 — encapsulable state machines (more value, more work)

#### 6. [x] Drag selection / clipboard subsystem
- **Cluster**:
  - Fields: `dragStartX`, `dragStartY`, `dragEndX`, `dragEndY`, `dragging`, `dragScrollDir`.
  - Methods: `applyDragHighlight`, `dragMainPaneBounds`, `updateDragAutoScroll`, `advanceDragAutoScroll`, `scheduleDragScrollTick`, `selectedText`, `copySelection`, `copyToClipboard`.
  - Adjacent: `yankPath` — clipboard-touching, not drag.
- **Extraction shape**: a `dragSelection` type that holds the four coordinates and `scrollDir`, with a `dragGeometry` struct param for the narrow layout dependency.
- **Invariants worth testing**: applying highlight then stripping ANSI for width yields the unselected text; `selectedText` returns the same characters that `applyDragHighlight` renders in reverse video; auto-scroll always reduces drag distance to viewport edge; clipping to the gutter never produces a selection that crosses the gutter boundary.
- **Result**: encapsulation and tests both done — `dragSelection` owns all six fields and the four geometry-using methods; seven rapid property tests in `drag_test.go` cover state-machine transitions, highlight round-trip, gutter clipping, and auto-scroll termination.

#### 7. [x] Help overlay subsystem
- **Cluster**:
  - Fields: `showHelp`, `helpScrollOffset`, `helpSearching`, `helpSearchConfirmed`, `helpSearchQuery`, `helpSearchMatches`, `helpSearchIdx`.
  - Methods: `helpContentLines`, `renderHelp`, `handleHelpKey`, `updateHelpSearchMatches`.
- **Extraction shape**: a `helpOverlay` type that owns its state.
- **Result**: clean encapsulation in `help.go`. ⚠️ No dedicated tests — see **TODO §B**.

#### 8. [x] Cross-pane search subsystem
- **Cluster**:
  - Fields: `searching`, `searchConfirmed`, `searchQuery`, `searchMatches`, `searchMatchIdx`.
  - Methods: `handleSearchKey`, `handleSearchNavKey`, `updateSearchMatches`, `navigateToCurrentMatch`, `clearSearch`.
- **Extraction shape**: a `searchOverlay` type owning its state.
- **Dead branch flagged in survey**: `match.pane == "sidebar"` in `navigateToCurrentMatch`. Confirmed removed in `search.go` extraction; matches are now `[]int`.
- **Result**: clean encapsulation in `search.go`, dead branch dropped. ⚠️ No dedicated tests — see **TODO §B**.

#### 9. [x] Mode view state persistence
- **Cluster**:
  - Fields: `mode`, `focus`, `modeStates`, `lastMainItem`, `mainScrollLines`.
  - Methods: `setMode`, `saveModeState`, `restoreModeState`.
- **Extraction shape**: a `viewMemory` type with `SaveSidebar` / `RestoreSidebar` / `RememberMainScroll` / `RecallMainScroll`.
- **Result**: clean encapsulation in `viewmemory.go`. ⚠️ No dedicated tests — see **TODO §B**.

#### 10. [x] RWX log fetcher
- **Cluster**: `pendingRWXCheck`, `rwxLogCache`, `maybeFetchRWXLog`, `rwxLogMsg`.
- **Extraction shape**: an `rwxFetcher` type owning the cache and pending state, with `Lookup`, `Cmd`, `Apply`.
- **Result**: clean encapsulation in `rwx.go`; property tests in `rwx_test.go`.

---

### Tier 3 — pull-apart per-mode dispatchers

#### 11. [x] Sidebar item builder — `updateSidebarItems`
- **Location**: `model.go:2552-2877` (~325 lines).
- **Per-arm extractability**:
  - `buildFilesSidebar(...) []sidebarItem`
  - `buildCommitsSidebar(...) []sidebarItem`
  - `buildPRSidebar(...) []sidebarItem`
- **Result**: clean pure-builder extraction in `sidebarbuild.go`. Minor: `extractDirs` still called 3× — see **TODO §D.1**.

#### 12. [x] Main content builder — `updateMainContent`
- **Location**: `model.go:2940-3230` (~290 lines).
- **Per-arm extractability**: catalog called for pure "compute what to show" functions returning a result struct, then a small applier.
- **Result**: ⚠️ Per-mode methods split out in `maincontent.go`, but they still mutate `m.mainPane` directly. The leaf helpers (`buildCommentContent`, `buildReviewContent`, `buildCIContent`) are pure. See **TODO §C** for whether to finish or accept.

---

### Tier 4 — smaller cohesive groupings

#### 13. [x] File classification
- **Cluster**: `isDeletedFile`, `isCommittedFile`, `isUncommittedFile`, `fileItemKind`, `changeBadge`, `applyChangeBadges`.
- **Extraction**: free functions in `fileclass.go` taking explicit slice params. Kept linear-scan `containsString` (catalog suggested map-backed sets for perf — see **TODO §D.2**).

#### 14. [x] File title-right strings
- **Cluster**: `fileDiffPrefix`, `fileContextRight`.
- **Extraction**: free functions in `filetitle.go`.

#### 15. [x] Diff navigation helpers
- **Cluster**: `jumpToFirstDiff`, `jumpToNextDiff`, `jumpToNextLeaf`.
- **Extraction**: free functions in `navigate.go`; property tests in `navigate_test.go`.

#### 16. [x] Pane geometry helpers
- **Cluster**: `statusBarLines`, `sidebarPixelWidth`, `updateLayout`, `dragMainPaneBounds`.
- **Extraction**: `panelayout.go`.

#### 17. [x] Selection helpers for click-targeted lists
- **Cluster**: `selectFirstComment`, `selectFirstReview`, `selectFirstCIFailure`.
- **Extraction**: free functions in `selectfirst.go`.

#### 18. [x] PR data sort & ordering
- **Cluster**: `sortPRData`, `ciBucketOrder`.
- **Extraction**: moved to `prdata.go`.

---

## Notes & callouts (from original survey)

- **Already factored, don't disturb**: `sidebar.go`, `mainpane.go`, `statusbar.go`, `keys.go`, `markdown.go`, `ipc.go`, `styles.go`.

- **Testing pattern**: `invariant_test.go` uses `pgregory.net/rapid` with a `genMockGit` generator. New extraction tests should match this style. Use `./scripts/rapid` for heavy property runs.

- **`dragScrollTickMsg` and `notificationExpiredMsg`** belong to their respective subsystems (drag §6, notifications). `dragScrollTickMsg` moved to `drag.go` already; `notificationExpiredMsg` still sits in `model.go`.

---

## Verification

For the TODO follow-ups:
1. After each change, run `go test -race -v ./internal/ui` to confirm
   existing invariants still hold.
2. For heavier verification, run `./scripts/rapid` (per CLAUDE.md — do not
   invoke `PRWATCH_RAPID_CHECKS=N go test` directly).
3. Run `PRWATCH_RENDER_ONCE=1 go run .` from a repo with a PR to confirm
   the overall UI still renders identically.
4. New property tests should commit any `.fail` files they produce (per
   CLAUDE.md).
