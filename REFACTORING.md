# Catalog: factoring candidates in `internal/ui/model.go`

## Context

`internal/ui/model.go` is 4265 lines and defines a `Model` struct with ~55 fields
and ~73 methods, plus ~25 free functions. The struct mixes at least a dozen
distinct concerns (git data, modes, sidebar/main-pane state, search, help,
drag selection, refresh cadence, RWX log cache, layout, notifications, etc.).
Most logic in the file is dispatched through `Model` even when its actual
dependencies are narrow.

The goal of this catalog is to enumerate self-contained, idempotent chunks
that could be extracted — each entry names the cohesive cluster of fields +
methods that moves together, the dependencies that stay narrow, and what a
property-based invariant test for the extracted piece would look like.

Ranking heuristic per candidate: **value** = how much surface area this
removes from `Model` × how testable-in-isolation the extracted piece is.
**ease** = how narrow the dependencies on the rest of `Model` are and how
mechanical the extraction is.

The catalog does not propose an order or attempt to bundle these — pick
whichever cluster to peel off next.

---

## Tier 1 — high value, low risk (pure free functions hiding as methods)

These are the cheapest extractions — small, already pure or nearly pure,
have narrow inputs and obvious property tests.

### 1. [x] PR description rendering — `renderPRDescription`
- **Location**: `model.go:4024-4120` (~100 lines).
- **Surface area**: 1 method.
- **Dependencies on Model**: `prInfo`, `prReviews`, `prDeployments`, `mainPane.width`.
- **Extraction**: free function `renderPRDescription(prInfo, reviews, deployments, width int) string` in a new `prdescription.go`.
- **Invariants worth testing**: idempotent for same input; output always contains the PR number and title; reviewer line absent iff `prReviews` empty; markdown rendering errors fall back to raw body.

### 2. [x] Scope handle info — `scopeHandleInfo`
- **Location**: `model.go:3440-3449` (~10 lines).
- **Surface area**: 1 method.
- **Dependencies on Model**: `base`, `naturalBase`, `commitCount`.
- **Extraction**: free function `scopeHandleFromBase(base, naturalBase string, commitCount int) *scopeHandleInfo`. The `scopeHandleInfo` type already lives in `statusbar.go`.
- **Invariants worth testing**: returns nil iff `base == naturalBase`; sha7 is always 7 chars when base is longer; `headOffset` equals `commitCount`.

### 3. [x] Refresh cadence — `computePRInterval` / `computeGitInterval`
- **Location**: `model.go:3251-3272` (~22 lines).
- **Surface area**: 2 methods + 5 fields (`prInterval`, `gitInterval`, `lastUIEvent`, `lastServerChange`, `lastGitChange`) + 3 mutation sites that set timestamps.
- **Extraction**: a small `activityTracker` type with `MarkUIEvent`, `MarkServerChange`, `MarkFSEvent`, `PRInterval(now)`, `GitInterval(now)`.
- **Invariants worth testing**: any recent UI event keeps both intervals at "active"; `GitInterval` returns active when EITHER UI or FS is recent; intervals are monotonic w.r.t. quiescence.

### 4. [x] Pure parsing helpers (already free, just scattered)
- `parseHunkNewStart` (model.go:2240), `parseHunkHeader` (model.go:2259), `isBinaryContent` (model.go:2293), `shortstatFromDiff` (model.go:465), `relativeTime` (model.go:597), `pluralize`, `formatAuthorAndTime`, `commitTitleLeft`, `matchNumberedItem`, `reviewStateLabel`, `ciBucketOrder`, `extractDirs`.
- **Extraction**: move to a `format.go` / `diffparse.go` / `gitformat.go` peer file. Already pure — just lives in the wrong file.
- **Why it matters**: it's not the lines that hurt, it's that `model.go` is currently the keyword search target for everything. Moving these reduces noise in the file that actually mutates state.

### 5. [x] ANSI / display-width helpers
- `padToHeight` (3635), `splitAtDisplayCols` (3666), `stripANSIForWidth` (3709), `displayWidthOf` (3715), `sliceByDisplayCol` (3722).
- **Extraction**: peer file `ansiwidth.go` (or fold into existing `mainpane.go` neighbors if they're already used there).
- **Invariants**: `splitAtDisplayCols`'s three parts reconcat to the original; `sliceByDisplayCol` never splits a rune mid-byte; `stripANSIForWidth ∘ ansi-wrap = identity` on the unstyled content.

---

## Tier 2 — encapsulable state machines (more value, more work)

These are the highest-value extractions in terms of removing fields and
making the remaining `Model` legible. Each is a coherent sub-feature whose
state never escapes.

### 6. [x] Drag selection / clipboard subsystem
- **Cluster**:
  - Fields: `dragStartX`, `dragStartY`, `dragEndX`, `dragEndY`, `dragging`, `dragScrollDir`.
  - Methods: `applyDragHighlight` (3552), `dragMainPaneBounds` (3750), `updateDragAutoScroll` (3770), `advanceDragAutoScroll` (3792), `scheduleDragScrollTick`, `selectedText` (3833), `copySelection` (3985), `copyToClipboard` (4008).
  - Adjacent: `yankPath` (3961) — clipboard-touching, not drag.
  - Pure helpers it relies on: the ANSI/width helpers from Tier 1 §5.
- **Dependencies on Model that stay narrow**: status bar height (a layout function), sidebar pixel width, the main pane's viewport `GetContent()` / `YOffset()` / `gutterWidth` / `wrapContinuation`, and `cmdFactory`.
- **Extraction shape**: a `dragSelection` type that holds the four coordinates and `scrollDir`, and takes a "pane geometry" interface for the narrow dependency. Could live in a peer `drag.go` or its own sub-package — favors **peer file**, since the geometry interface ties tightly to `Model`.
- **Invariants worth testing**: applying highlight then stripping ANSI for width yields the unselected text; `selectedText` returns the same characters that `applyDragHighlight` renders in reverse video; auto-scroll always reduces drag distance to viewport edge; clipping to the gutter never produces a selection that crosses the gutter boundary.

### 7. [x] Help overlay subsystem
- **Cluster**:
  - Fields: `showHelp`, `helpScrollOffset`, `helpSearching`, `helpSearchConfirmed`, `helpSearchQuery`, `helpSearchMatches`, `helpSearchIdx`.
  - Methods: `helpContentLines` (4140), `renderHelp` (4226), `handleHelpKey` (1669), `updateHelpSearchMatches` (1652). Plus the helpEntry/keyList helpers (4123-4138).
  - The dispatch hook in `handleKey` at model.go:1357-1360.
- **Dependencies on Model that stay narrow**: `width`, `height`, `statusBarLines()`. That's all.
- **Extraction shape**: a `helpOverlay` type that owns its state, takes width/height/statusBarLines per render, and returns either `(model, cmd, handled)` from key dispatch. Peer file `help.go`. Could become a sub-package since the surface is so narrow, but the package boundary doesn't earn its keep for ~200 lines.
- **Invariants worth testing**: search query empty ⇔ matches empty; search idx always in `[0, len(matches))`; toggling help is idempotent; n/p navigation wraps; scrolling never goes past `len(lines) - visibleHeight`.

### 8. [x] Cross-pane search subsystem
- **Cluster**:
  - Fields: `searching`, `searchConfirmed`, `searchQuery`, `searchMatches`, `searchMatchIdx`.
  - Methods: `handleSearchKey` (1779), `handleSearchNavKey` (1816), `updateSearchMatches` (1841), `navigateToCurrentMatch` (1855), `clearSearch` (1869).
  - Render hook in `View()` at model.go:3511-3529 (the bottom search bar).
- **Dependencies on Model that stay narrow**: `mainPane.FindMatches()`, `mainPane.SetSearchQuery()`, `mainPane.ScrollToLine()`, and `sidebar.SelectIndex()` + `updateMainContent()` for the "sidebar" match pane (which currently is never produced — spec says search is main-pane only, so the sidebar branch in `navigateToCurrentMatch` is dead).
- **Extraction shape**: a `searchOverlay` type that owns its state and accepts a small "searchable view" interface. Peer file `search.go`. Note: the `searchMatch.pane == "sidebar"` branch is dead — extracting forces deciding whether to drop it.
- **Invariants worth testing**: after `clearSearch`, all five fields are zero-valued; `searchMatchIdx` always wraps in `[0, len(matches))`; setting an empty query never confirms; backspace to empty exits search.

### 9. [x] Mode view state persistence
- **Cluster**:
  - Fields: `mode`, `focus`, `modeStates`, `lastMainItem`, `mainScrollLines`.
  - Methods: `setMode` (2883), `saveModeState` (2904), `restoreModeState` (2918), partial of `updateMainContent` (the `setItem` closure at 2956-2973 that remembers the per-item scroll line).
  - Types `modeViewState` (192) and `mainItemKey` (204) — already named.
- **Dependencies on Model that stay narrow**: `sidebar.SelectedItem()`, `sidebar.SelectIndex()`, `sidebar.offset`, `mainPane.ViewportToSourceLine()`, `mainPane.ScrollToSourceLine()`.
- **Extraction shape**: a `viewMemory` type holding `modeStates` and `mainScrollLines`, with `Save(mode, sidebar) ` / `Restore(mode, sidebar, mainPane)` methods plus per-`mainItemKey` scroll memo.
- **Invariants worth testing**: save-then-restore is identity for sidebar selection (when selectable item still present); switching mode and back preserves selection iff the item still exists; per-item scroll memo is preserved across mode round-trips.

### 10. [x] RWX log fetcher
- **Cluster**:
  - Fields: `pendingRWXCheck`, `rwxLogCache`.
  - Methods: `maybeFetchRWXLog` (270), and the inline `Loading RWX logs...` / cache check inside `updateMainContent`'s PR branch (model.go:3209-3217).
  - Type: `rwxLogMsg` (327).
- **Dependencies on Model**: `git` (only the three `RWX*` methods on it).
- **Extraction shape**: an `rwxFetcher` type owning the cache and pending state, with `Get(check) (string, tea.Cmd)` that returns cached content or returns a `tea.Cmd` plus a "loading" sentinel.
- **Invariants worth testing**: cache hits never produce a cmd; consecutive `Get` calls for the same URL produce at most one cmd; `Apply(msg)` is idempotent for the same `rwxLogMsg`.

---

## Tier 3 — pull-apart per-mode dispatchers (high value, medium-large work)

These two giant switch-on-`Mode` methods are where the god object actually
manifests. Each arm could be a pure(-ish) builder that takes a small data
record and returns a result.

### 11. [x] Sidebar item builder — `updateSidebarItems`
- **Location**: `model.go:2552-2877` (~325 lines).
- **Shape today**: one method with three big arms (`FilesMode`, `CommitsMode`, `PRMode`) plus a debug-logging defer at the top.
- **Per-arm extractability**:
  - `buildFilesSidebar(committed, uncommitted, staged, deleted, all []string, ignored, ignoredDirs, collapsed map[string]bool, showIgnored bool, isGit bool) []sidebarItem`
  - `buildCommitsSidebar(commits, baseCommits []Commit, uncommittedFiles, stagedFiles []string, ahead, loaded, total int) []sidebarItem`
  - `buildPRSidebar(comments []PRComment, reviews []PRReview, checks []CICheck) []sidebarItem`
- **Why this is high-value**: the three arms only share the `sidebarItem` slice type; their data dependencies don't overlap. Splitting them frees three independent pure functions and one tiny method that just dispatches and calls `m.sidebar.SetItems(...)`.
- **Invariants worth testing**: header counts always match item counts in the section; ordering matches the spec (alphabetical for files; descending date for comments/reviews; failures-first for CI); cutline appears iff base commits and other commits both present.

### 12. [ ] Main content builder — `updateMainContent`
- **Location**: `model.go:2940-3230` (~290 lines).
- **Shape today**: one method, three arms again. Inside the arms there's also the `setItem` closure (scroll memory, see candidate §9) which can be lifted out.
- **Per-arm extractability**: harder than §11 because each arm calls multiple `mainPane.Set*` mutators and reads many more `Model` fields. But pure "compute what to show" functions are still feasible:
  - `filesModeContent(...)` → returns `(filename, content, diff, hunks, title, dirty bool)`-shaped struct, then a small method applies that to `mainPane`.
  - `commitsModeContent(...)` → returns the title + content for the selected commit / pseudo-entry.
  - `prModeContent(...)` → returns title + content for the selected PR sidebar item.
- **Why it's worth it anyway**: each arm has its own complicated logic (deleted-file annotations, RWX cache fallthrough, markdown rendering) and currently they all read whatever they want off `Model`. Decomposing forces the dependencies to be named.

---

## Tier 4 — smaller cohesive groupings

### 13. [x] File classification
- **Cluster**: `isDeletedFile` (2474), `isCommittedFile` (2543), `isUncommittedFile` (2315), `fileItemKind` (2483), `changeBadge` (2493), `applyChangeBadges` (2525), plus the seven `*Files` slices on `Model`.
- **Extraction shape**: a `fileSets` value holding the slices (or hashed sets) with `IsCommitted/IsUncommitted/IsStaged/IsDeleted/IsAdded` predicates plus `ChangeBadge` and `ApplyBadges`. Today the linear scans through `committedFiles` for membership are O(n) per call inside loops in `updateSidebarItems` — a set-backed version would be measurably faster on large repos.
- **Invariants worth testing**: `IsDeleted` and `IsCommitted` are not mutually exclusive (deleted files are tracked in both lists); `ChangeBadge` is empty iff none of the sets contains the file.

### 14. [x] File title-right strings (no-diff and with-diff)
- **Cluster**: `fileDiffPrefix` (516), `fileContextRight` (549).
- **Dependencies**: `dir`, `git`, `os.Stat`, and the classification predicates from §13.
- **Extraction shape**: free functions that take an explicit `filesystemStat` callback + git source + the file classification. Peer file `filetitle.go`.
- **Invariants worth testing**: binary prefix only present when `binary=true`; `untracked` appears iff no commit found; the result has at most 3 ` · `-separated segments.

### 15. [x] Diff navigation helpers
- **Cluster**: `jumpToFirstDiff` (2380), `jumpToNextDiff` (2389), `jumpToNextLeaf` (2421).
- **Dependencies on Model**: just `mainPane.DiffLineNumbers()`, `mainPane.ViewportToSourceLine()`, `mainPane.ScrollToSourceLine()`, `sidebar.items`, `sidebar.SelectedIndex()`.
- **Extraction shape**: free functions taking the small interface they need, or a single `Navigator` type bundling mainPane and sidebar pointers.
- **Invariants**: wraps; never moves past last or before first; `jumpToNextLeaf` skips separators and cutlines.

### 16. [x] Pane geometry helpers
- **Cluster**: `statusBarLines` (1878), `sidebarPixelWidth` (1882), `updateLayout` (3274), `dragMainPaneBounds` (3750).
- **Extraction shape**: a `paneLayout` snapshot/struct that takes the relevant model fields and produces the bounds queried by drag, drag-highlight, search-bar overlay, etc. Currently each function recomputes them inline.

### 17. [x] Selection helpers for click-targeted lists
- **Cluster**: `selectFirstComment` (2331), `selectFirstReview` (2341), `selectFirstCIFailure` (2355).
- **Extraction shape**: small free functions taking the sidebar items + the data slice they're "first-of"-ing into. Could merge into a single `selectFirst(items, predicate)`.

### 18. [x] PR data sort & ordering
- **Cluster**: `sortPRData` (631), `ciBucketOrder` (644).
- **Extraction shape**: already pure — move alongside the rest of the format/sort helpers in a `prdata.go` peer file.

---

## Notes & callouts

- **Already factored, don't disturb**: `sidebar.go` (sidebar type), `mainpane.go` (mainPane type), `statusbar.go` (statusbar rendering), `keys.go`, `markdown.go`, `ipc.go`, `styles.go`. The pattern of "type + peer file" is already idiomatic here; new extractions should match.

- **Testing pattern observed**: `invariant_test.go` (2502 lines) already uses `pgregory.net/rapid` for property-based testing with a `genMockGit` generator. New extractions can plug into this same generator (or a narrower one for the specific subsystem) and assert their invariants there. Use `./scripts/rapid` for heavy property runs.

- **`Model.lastMainItem` and `Model.modeStates` together encode "what the user was looking at"** — they're tightly coupled and should move together (candidate §9), not separately.

- **`dragScrollTickMsg` and `notificationExpiredMsg`** are messages that belong to their respective subsystems (drag §6, notifications). If you extract those subsystems, the message types should travel with them — currently they sit alongside `prTickMsg`/`gitTickMsg` in a flat list at the top of `model.go`.

- **Dead branch found** during this survey: `navigateToCurrentMatch` has a `match.pane == "sidebar"` case, but `updateSearchMatches` only ever produces `pane: "main"` matches. The sidebar branch is unreachable. Worth dropping when extracting §8.

- The `searchMatch` struct (model.go:40) has a `pane` field that exists only to support that dead branch. If §8 is extracted and the dead branch is removed, `searchMatch` becomes just an int.

- **`extractDirs` (model.go:2459)** is called four times in `updateSidebarItems` to recompute the same directory list. If §11 is extracted, the builders can do it once.

---

## Verification

This is a catalog, not an implementation. There's nothing to test directly.

When any individual extraction from this catalog is actually executed:
1. Run `go test -race -v ./internal/ui` and `./scripts/rapid` to confirm
   existing invariants still hold.
2. Run `PRWATCH_RENDER_ONCE=1 go run .` from a repo with a PR to confirm the
   overall UI still renders identically.
3. Add property-based invariant tests for the extracted piece (see each
   candidate's "Invariants worth testing" subsection for starting points).
