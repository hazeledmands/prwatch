# prwatch Implementation Plan

**Goal:** Build a lazygit-style TUI that shows the delta between a feature branch and its base branch, with file, commit, and PR modes.

**Architecture:** Bubbletea v2 Elm architecture (Model → Update → View). Git CLI for data, fsnotify for live updates. Three visual components (status bar, sidebar, main pane) composed in a root model.

**Tech Stack:** Go, bubbletea v2, bubbles v2 (viewport, key), lipgloss v2, fsnotify, goldmark

---

## File Structure

```
prwatch/
├── main.go                    # Entry point, tea.NewProgram setup
├── go.mod / go.sum
├── internal/
│   ├── git/
│   │   ├── git.go             # Git CLI wrapper: branch, files, diffs, commits, PR/CI via gh, RWX via rwx
│   │   └── git_test.go        # Tests using temp git repos + mock runners
│   ├── watcher/
│   │   ├── watcher.go         # fsnotify watcher with debounce, sends tea.Msg
│   │   └── watcher_test.go
│   └── ui/
│       ├── model.go           # Root bubbletea model, mode/focus state, key dispatch
│       ├── model_test.go      # Unit tests for Update logic
│       ├── markdown.go        # Goldmark-based markdown → ANSI renderer
│       ├── markdown_test.go   # Markdown rendering tests
│       ├── statusbar.go       # Status bar rendering (3 lines: status, git, PR) with clickable regions
│       ├── sidebar.go         # Sidebar: tree view, file/commit/PR item lists
│       ├── sidebar_test.go    # Sidebar selection/navigation tests
│       ├── mainpane.go        # Viewport with diff coloring, word wrap, gutter
│       ├── styles.go          # All lipgloss style definitions
│       ├── keys.go            # Key binding definitions
│       ├── snapshot_test.go   # Golden file snapshot tests
│       ├── invariant_test.go  # Property-based tests (rapid)
│       └── testdata/golden/   # Golden files for snapshot tests
├── PLAN.md
├── PROMPT.md
├── BUG_REPORTS.md
├── INCONSISTENCIES.md
└── README.md
```

---

## In Progress: Code-review remediation (CODE_REVIEW.md A1–A6)

Working through the six systemic antipatterns from `CODE_REVIEW.md` (2026-06-12,
findings verified against `main` @ c6c9260) in order, one commit per
antipattern. Method per antipattern: regression test observed failing first,
then the fix, then full-suite + rapid verification; each fixed bug logged in
`BUG_REPORTS.md`.

**A1 — paired computations that drift** (done, committed 4902d15). Fix = one source of
truth per value, all call sites converted, per the "Layout geometry comes from
one function" CLAUDE.md rule:
1. Status-bar height: `statusBarLines()` is the sole row-count authority;
   `View()` and `updateLayout()` consume it; the loading-line predicate is one
   shared expression (also resolves the dead "Loading from GitHub…" render
   path — revive or delete deliberately).
2. Quit-confirm height: `statusBarLineCount` accounts for `confirming`.
   Guard for 1+2: property test that rendered status-bar row count ==
   layout row count across the model state space (loading × prLoadedOnce ×
   git nil × confirming × error × pr states).
3. Wrap-aware line mapping: search uses `ScrollToSourceLine` (not raw
   `ScrollToLine`); `currentLineNumber` uses `viewportToSourceLine`.
4. Behind-count base: one function for the base-ref fallback chain, used by
   `loadGitData`, `loadLocalGitData`, `renderLine2`.
5. Diff header detection: one shared predicate for `+++`/`---` file headers
   used by `parseDiffHunks`, `parseDiffAnnotations`, `shortstatFromDiff`,
   `colorDiff`. Trailing-space check alone is insufficient (`--- comment`
   removing a `-- comment` line still has the space) — headers must be
   recognized positionally (outside hunks only).
6. PR item identity: `openPRItemURL` uses `matchNumberedItem` like
   `updatePRModeContent` does.

**Interlude — parallelize rapid property tests** (done, committed c639927): add `t.Parallel()` to the `TestProperty_*` tests (after
checking for shared mutable globals, e.g. lipgloss profile/styles) so heavy
sweeps use all cores. Also from A1 on, verification regimen changes: new/
affected properties at high count + full suite at moderate count as the gate,
with the full `./scripts/rapid 200` sweep run in the background overlapping
review rather than blocking the implementing agent.

**A2 — async tea.Cmd convention** (done, uncommitted): snapshot inputs at
dispatch, carry in result msg, compare on receipt. `gitLoadRequest` +
`m.gitLoadCmd(withPR)` replace the `m.loadLocalGitData` / `m.loadGitData`
bound-method Cmds and merge the two 110-line near-duplicate loaders into one
`runGitLoad(req)`; `m.loadMoreCommitsCmd()` snapshots base+skip and holds a
`moreCommitsPending` marker; `loadPRStatusCmd` / `loadNonGitFilesCmd` /
`expandIgnoredDirCmd` converted to free functions over
explicit parameters so no Cmd closure reads `m.*`. `gitDataMsg` gained
`reqScrubbedBase` (the user's scope pin at dispatch) and the stale-load guard
now compares pins rather than bases, so a load that detects legitimate natural-
base movement is applied instead of discarded. The synchronous
`m.loadGitData()` / `m.loadLocalGitData()` / `m.loadPRStatus()` /
`m.loadNonGitFiles()` / `m.loadMoreCommits()` methods remain for
Update-goroutine callers (RenderOnce, RenderWithKeys, tests) and are documented
as never-dispatch.

**A3 — error-path conflation** (done, uncommitted): `m.err` clears on every
non-error `gitDataMsg` (a local-only refresh and a PR-half failure both prove
the local half recovered). `fetchPRStatus` classifies via `isRateLimited()`:
`prRefreshMsg` now carries `fetchFailed` (preserve PR data, generic
"GitHub API error") separately from `rateLimited` (also back off). Backoff is
latched in `activityTracker.rateLimitBackoff` as a *floor* on the interval
(`effectivePRInterval = max(activity, backoff)`, so going idle mid-backoff
still slows down instead of speeding up), which `ResetPRInterval` reads and
only `MarkPRSuccess` clears; `MarkPRFetch`/`PRFetchDue`/`PRTickDelay`
refuse a tick that was armed before the bump and re-arm it for the remainder of
the backoff, so the bump governs the next fetch rather than taking effect a
cycle late. (Self-clocking from the `prRefreshMsg` handler was tried and
rejected — `invariant_test.go`'s `execSafeCmd` runs returned commands, so a
handler returning a real `tea.Tick` hangs the property suite.)
`PRChecksAll` returns its errors (parsing output first, since `gh pr checks`
exits nonzero on failing/pending checks while still emitting JSON) and a
`checksFailed` msg flag makes both handlers keep the CI data already on screen
while the PR number is unchanged (cleared across a PR switch). The PR-inclusive
git load feeds the same state machine as the tick: it classifies its own PR
error (`prRateLimited`) and bumps, and a success on that path clears the latch.
`BehindCount` returns `(int, error)`; `behindKnown` flows to the status bar,
which hides an unmeasured count rather than rendering it as 0.
Display policy for GitHub errors is deliberately unchanged — the
INCONSISTENCIES.md adjudication about API errors being invisible once PR data
exists stays open.

**A4 — tests that encode bugs** (done, uncommitted): `ScrollRight`'s clamp
subtracted the gutter from `maxContentWidth()`, which measures the unformatted
source and so never had one — the last `gutterWidth` columns of the widest line
were unreachable, and `TestScrollRight_Clamping`'s `+10 // allow some tolerance
for gutter` admitted both answers. Clamp fixed, assertion now exact, and
`TestScrollRight_RightmostColumnReachable` states it observably.
`extractLineFragment`/`stripGutterDisplayWidth` trimmed before stripping the
gutter, so a blank line's own gutter survived the length guard and was copied as
content; both now go through one `stripGutterText` helper that strips first.
The dead `wrapLinesWordBoundary` copy (plus `wrapLines`/`wrapLinesWithIndent`)
is deleted and the `TestWrapLines_*` tests retargeted at the live
`wrapLinesWithContinuationMap` — the two copies had not actually diverged yet.
`TestProperty_DragSelectsCorrectText` now generates blank, whitespace-only,
short, long, leading/trailing-whitespace and CJK-width line bodies, calls the
production `stripGutterText` instead of re-deriving it, and adds invariant 1a
(the i-th copied line maps to a *specific* source line, so a blank source line
must copy as empty). `gh` fixtures are complete relative to the `--json` lists,
with a tripwire that reads the recorded argv and fails on any omitted field.
`parseRenameNameStatus`/`parsePorcelainV2Renames`/`parseCommitLog` get
table-driven behavior tests as the safety net for A6's `-z` conversion, with
today's `core.quotePath` mishandling recorded as `CURRENT BEHAVIOR:` rather
than fixed. Two A4 bullets are deliberately left to A5
(`TestScope_IsScrubbedConditions`, `TestModeSwitching_RetainsPerModeViewState`);
one new wide-glyph drag bug found and logged in BUG_REPORTS.md.

**A5 — broken seams** (done, uncommitted): all six sub-items fixed; details
per-bug in BUG_REPORTS.md. The structural change is `internal/ui/mainnav.go`,
a `mainNav` seam owning every Model-level main-pane scroll, cursor move, and
change to the row↔source mapping, so cursor visibility, visual-mode selection
and cursor re-derivation are restored at one choke point rather than remembered
at each call site. Hunk nav, search nav and `setItem`'s fallback now pair their
scroll with a cursor placement; `syncSelection` in the shared fixups covers
g/G, the forwarded page keys and the wheel. `sidebar.SelectedItem()` and
`viewMemory.RestoreSidebar` share one canonical key (`itemID`), the same one
`SetItems` uses. `scope` gained an explicit `pinned` flag re-evaluated against
endpoint *SHAs*, and `SyncFromLoad` takes the load's own measurement of the
pinned commit's distance from HEAD (`gitDataMsg.pinnedOldOffset`, snapshotted
at dispatch per A2), so a new commit neither un-pins the scope nor staleens the
`HEAD~N` indicator. The `gitDataMsg` handler resets the scope on branch switch
(PROMPT.md:232) via `branchIdentity`, keyed on branch rather than HeadSHA so a
commit is not mistaken for a checkout, and re-dispatches — running before the
A2 pin guard, which then discards the payload computed against the stale pin.
`mainNav.Reflow` re-derives the cursor (new `cursor.seq` to honour deliberate
re-placements, new `cursor.ClampToContent`) and the selection (new
`selection.Reflow`) across content refreshes, the `w`/`n`/`D` toggles and
resize; `mainPane.SetSize` now re-wraps on a width change, which it never did.
`TestSeam_MainPaneNavigationGoesThroughNav` is the drift guard for the seam.
Step 5's resize-invariance property is `TestProperty_Model_CursorSurvivesReflow`
— it needed its own generator, since `genScenario`'s ~20-column diff lines fit
at every generated width and made the property vacuous. Both A4-deferred tests
(`TestScope_IsScrubbedConditions`, `TestModeSwitching_RetainsPerModeViewState`)
are resolved here. One unsound assertion in
`TestProperty_Model_VisualYankMatchesHighlight` corrected against PROMPT.md:162
(inline `~` rows deliberately render the pre-image, so a copied row can contain
text that is not in the new-file content) — it now asserts against the pane's
pre-wrap rendered rows (`formattedContent`) rather than skipping decorated
files.

Review round on A5 turned up two further bugs in the new code, both fixed:
`SyncFromLoad` applied a pinned-distance measurement without checking which pin
it was measured against (wrong `HEAD~N` on a rapid double-scrub), now keyed on
the SHA via the existing `reqScrubbedBase`; and the branch-switch reset could
be triggered by an out-of-order load, wiping a fresh scrub on the new branch
and adopting stale `repoInfo` — the A2 snapshot convention now carries a
monotonic `seq` (`gitLoadRequest` → `gitDataMsg`) compared against
`Model.gitAdoptedSeq`. The seam's scope is documented in mainnav.go: it owns
the vertical axis only, and horizontal scroll stays deliberately outside the
contract this round.

**A6 — ungenerated inputs** (done, uncommitted): five of the six bullets fixed;
the sixth was already done. Details per-bug in BUG_REPORTS.md.

`Git.runZ` + `splitNUL` (`internal/git/git.go`) is the new NUL-delimited
boundary, and all 15 path-producing call sites pass `-z`, so
`core.quotePath`'s octal-escaped `"caf\303\251.txt"` no longer reaches the UI.
Both rename parsers consume records rather than splitting lines on tabs, which
also retires `parsePorcelainV2Renames`' 9-field space-walk. The `-z` record
shapes were verified against real git rather than inferred. `IgnoredEntries`
was doubly broken (the closing quote also defeated its `/` directory check).

`expandTabs` normalizes tabs once at the pane's content boundary and the three
disagreeing downstream tab widths are deleted; the widening turned up a
*second* content boundary — `SetDiffAnnotations`, whose `removedLines` render
as pane rows — which now normalizes too. `highlightMatchInLine` searches the
visible text via a new `indexVisible` byte map instead of the styled bytes, so
querying `2`/`;`/`m` no longer splices a highlight mid-escape and boundary-
spanning matches are no longer missed. Click, motion and release now return
early while the help overlay is open, and cursor-on-release is gated on a drag
having actually been in progress; `endpoint` gained `DisplayCol` so the click
and release handlers consume `clickAt`'s geometry instead of re-deriving it.
The blank-line drag-copy bullet was already fixed by A4 (`stripGutterText`) —
verified, not re-fixed; `CODE_REVIEW.md` is stale on that point.

Generators were widened for tabs, non-ASCII/wide/spaced filenames, non-ASCII
diff bodies, and (newly) ANSI escape sequences, all keyed on the loop index
rather than a new rapid draw so existing `.fail` seeds stay replayable.

A review round added five more fixes: `indexVisible` panicked on invalid UTF-8
(`utf8.RuneLen(RuneError)` is 3, so spans overshot the byte a bad byte actually
occupied); `OutsideSidebar` wrongly counted the pane's own gutter as outside,
so a gutter release refused a cursor placement a gutter click performs; a
release swallowed by the help overlay stranded an in-flight drag; and — a gap
the `-z` conversion itself opened — control characters in filenames now reach
display text, since `core.quotePath` used to escape them for us. That last one
gets one boundary function, `sanitizeDisplayText`, applied at the sidebar label
sites and `fileTitleLeft`, using git's own `\t`/`\n`/`\xNN` representation;
`filePath` and every git argument keep the raw bytes, because an escaped path
stops naming a file.

Two bugs the widening surfaced are **left open with their seeds committed**,
both needing a decision rather than a patch, and both reproducing at HEAD:
word wrap discards the space it breaks on (so yank/copy loses one space per
wrap point, and the wrap-relative column model cannot reconstruct it), and
`renderTitleRow` mis-pads strings of zero-width runes. Both are the same
family as the already-open wide-glyph half-cell question.

**The suite is therefore deliberately red on exactly those two tests**
(`TestProperty_Model_VisualYankMatchesHighlight`,
`TestRenderTitleRow_AlwaysExactWidth`) — per Hazel's decision, they are neither
fixed nor narrowed nor skipped, because the failing replay *is* the record of
the bug. Any third failure is a real regression. See BUG_REPORTS.md's "Found by
the widening, NOT fixed" section.

---

## In Progress: Position-based diff addressing

**Goal:** Introduce explicit `Position` and `Range` values naming "where the user is pointing in a diff" and "what window is currently visible." Several planned features (hunk-grain navigation, line-aware deep-links, inline comments, keyboard selection, LSP) all need to ask "what am I looking at right now?" Today this is derived ad-hoc from viewport state each time. Naming it unblocks the rest.

**Common thread.** Most items in `IDEAS.md` need finer granularity than "this file" — hunk, line, or (LSP + stream-mode selection) sub-line.

**Current state — addressing is anonymous:**
- `Model.currentLineNumber()` at `internal/ui/model.go:1751` derives current line from viewport-top on demand.
- `diffHunk{StartLine, EndLine}` at `internal/ui/mainpane.go:76` is hunk-indexed by new-file line range.
- `hunkPositionForLine` at `internal/ui/mainpane.go:150` classifies a line against the hunk list.
- `nextDiffLine` at `internal/ui/navigate.go:7` walks line-by-line, not hunk-by-hunk (related to the removal-only-hunk bug in `BUG_REPORTS.md`).
- `dragSelection` at `internal/ui/drag.go:33` stores selection in *pixel coordinates*, so it doesn't survive viewport changes and can't share state with keyboard-driven selection without translation.
- The **visible-window-as-a-range** pattern is *already* implicit in the codebase: `hunkTitleRight` at `mainpane.go:401-431` calls `visibleHunkRange(hunks, topLine, bottomLine)`; `progressPercent` at `mainpane.go:438-457` uses viewport bottom; drag uses both top + bottom (`drag.go:357-358`). Range as a peer type would just *name* what's there.

**Proposed shapes:**

```go
type Position struct {
    SourceLine int           // 1-indexed line in the displayed version
    Column     int           // 0-indexed column past the gutter; populated for mouse drag (step 2), visual mode (step 5), LSP (deferred)
    Side       Side          // add | remove | context — needed for PR comments + deep-links (added in step 6)
}

type Range struct {
    Start, End Position
}

type Selection struct {
    Anchor *Position // nil = clicked outside content at Begin time
    Active *Position // nil = currently outside content
    // Mode SelectionMode — added in step 5 for vim-style v/V/Ctrl-V
}
```

`Selection` projects to `Range` via a normalize method; `Range` doesn't recover Selection's direction.

Position is line-and-column only; file/document identity is paired with it at the call site rather than embedded, following the convention used by LSP, VS Code, tree-sitter, and other editor APIs. This keeps `mainPane` (which owns source-line data) independent of `Model` (which owns file identity via sidebar selection). `Ref` for the whole-scope-diff work is similarly paired externally.

Position is a singular point (cursor / focus / anchor / active). Range is a pair (selection, visible window, hunk extent, comment range). Today the cursor doesn't exist as a distinct concept — `Position` references derive from viewport-top until visual mode / click-to-place lands.

Each feature becomes a pure function of these:
- `o` deep-link: `(file, Position) → URL` or `(file, Range) → range URL`
- Hunk nav: `(Position, []diffHunk, direction) → Position`
- Hunk title display: `(Range visible, []diffHunk) → string` (already shaped this way)
- Comment lookup: `(file, Position) → []Comment`
- LSP query: `(file, Position) → LspRequest` (uses Column)
- `progressPercent`: `Range visible → int` (uses `visible.End`, stays viewport-based even when cursor exists — "how much of the file have I seen" is a more useful semantics for diff review than vim's cursor-position convention)

**Selection = a Range with mode.** `Selection { Anchor Position; Active Position; Mode SelectionMode }` covers:
- Existing mouse drag-to-copy (anchor = click, active = mouse, mode = stream)
- Keyboard visual mode (anchor = where `v` was pressed, active = cursor)
- PR comment ranges (GitHub anchors on start/end line + side)
- Range deep-links (`#L12-L15`)

Stream-mode keyboard selection needs Column for the active end — extending with `h`/`l` is character-grained.

**Implementation order:**

1. **Name `Position` and `Range`; introduce `mainPane.visibleRange()`.** (done) Routed existing "where am I" and "what's on screen" call-sites through them. `visibleRange` lives on `mainPane` because source-line data does; callers that also need file identity pair it externally.
2. **Convert `dragSelection` to Position-based.** (done) Mouse handler translates `(x, y) → Position` at event time via `dragGeometry.clickAt`. `originStartY` workaround eliminated; out-of-content clicks now carry an explicit `OutsideDir` on `endpoint` instead.
3. **Hunk-grain navigation.** (done) `nextHunkStart` in `navigate.go`; removal-only-hunk bug fixed (see `BUG_REPORTS.md`).
4. **Hunk popover.** (deprioritized) Clickable "hunk N/M" in the title bar opens a list; click navigates active Position. Independent of steps 5+; revisit after the cursor work lands.

---

5. **Persistent cursor and keyboard visual mode.** Introduces a `Position` cursor that exists outside any selection mode, motivated by LSP "symbol under cursor" and inline comments — both need "where am I pointing" without a modal opt-in. On top, `v`/`V`/`y`/`Esc` give vim-style selection that anchors at the cursor.

   The cursor's defining invariant: **the cursor is always inside the viewport.** Cursor-driven motion (`j`/`k`/`h`/`l`, click, hunk-nav) scrolls the viewport only when the cursor would otherwise leave the visible area. Viewport-driven motion (mouse wheel, `space`/`b`) drags the cursor along the edge when scrolling would otherwise push it off-screen. One rule; LSP/comments/highlights never have to handle "cursor off-screen."

   `j`/`k` move by **visual rows** (vim's `gj`/`gk` semantics), not logical source lines — wrapped lines step through each wrap row. Feels right for column behavior on wrapped lines but requires the source↔display translation in Phase A.

   **Phase A (precursor) — `positionToDisplay` helper.** Add `(m *mainPane) positionToDisplay(pos Position) → (vpRow, displayCol)` to `mainpane.go`. Inverse of the existing `sourceLineAtViewportOffset` + `absoluteColumnFromDisplay`; composes existing wrap-row helpers (`sourceLineToViewportOffset`, `wrapRowSourceColRange`, `wrapRowCountAtVpRow`). Documents the wrap-boundary convention: when `pos.Column` lands exactly at a wrap-row boundary, render at the leftward row's right edge (vim convention). Required for cursor rendering; valuable wherever else "given a Position, where on screen?" is asked.

   Phase A intentionally does **not** retire `endpoint.VpRow` (`drag.go:53-57`). Click context still disambiguates drag at wrap-row boundaries; we keep it until visual mode is in use and we have real signal on whether the convention is acceptable as lossy.

   Tests for Phase A: rapid round-trip property `(absoluteColumnFromDisplay ∘ positionToDisplay) == id` over Positions derived from displayed content; snapshot of cursor at a wrap-row boundary as the convention's regression guard.

   **5a — persistent cursor, no selection.** Own peer file `internal/ui/cursor.go` with its own struct (per CLAUDE.md: "encapsulate sub-feature state in its own type"). Fields: `pos Position`, `desiredCol int` (sticky display-column for vim-style `j`/`k` column preservation across rows of varying length). `j`/`k`/`h`/`l` move the cursor (`j`/`k` visual-row-grained); `space`/`b`/wheel scroll the viewport with cursor drag-along; click places cursor at release point. Cursor renders as a single reverse-video cell via `positionToDisplay`. Cursor `pos` persists per-file in `viewmemory` alongside scroll. Initial cursor at first hunk start (same target as `jumpToFirstDiff`).

   **5b — visual mode.** `v` (stream) and `V` (line) snapshot the cursor as `Anchor`; further cursor motion updates `Active`. `y` extracts via the existing `extractSourceRange` and copies; `Esc` / file switch / mode switch / mouse-drag start all dismiss. Highlight reuses the drag renderer (already column-aware).

   Cursor can land on **removed-side displayed rows** (commenting on a deleted line is a real PR gesture). That requires Position to carry `Side` (add/remove/context) so the cursor unambiguously identifies which displayed row it's on when multiple rows map to the same new-file source line. Step 6's `Side` field is pulled forward into 5b as a prerequisite.

   **5c — selection unification.** Two flavors, both reasonable:
   - *Shared interfaces* (landed). `paintHighlightClips`/`buildHighlightClips` paint either selection kind; `copyAndNotify` is the shared yank pipeline; `extractSourceRange` does text extraction the same way for both. The user-facing concept "a selection" is unified at the consumer boundaries even though `dragSelection` and `selection` remain separate structs.
   - *Structural collapse into one Mode-tagged type* (deferred). On reflection, this is best deferred alongside Phase B: `dragSelection` carries click-time fields (`VpRow`, `OutsideDir`, `scrollDir` for auto-scroll) that don't apply to keyboard modes, and Phase B's retirement of VpRow would clean those up first. Forcing a unified shape now would mean Mode-conditional fields that the Phase B work would remove anyway.

   **Phase B (deferred follow-up to 5c) — retire `endpoint.VpRow`.** Once `positionToDisplay`'s convention has been exercised by visual mode and we've seen drag behavior at wrap-row boundaries in practice, decide whether drag can drop its `VpRow` side-channel and rely on the same convention. Corner case is rare (clicks exactly at `displayCol == width`); likely acceptable as lossy but should be confirmed with real data.

   **Non-diff panes (PR description, CI logs, RWX logs).** Cursor exists and visual mode works the same way — selection extracts displayed text via `extractSourceRange`. `Side` is undefined there (no add/remove dimension); cursor stays line+column. This confirms `Position` belongs to displayed text, not to git semantics.

   **Tests for step 5:**
   - Property: cursor is always inside the viewport after any sequence of motion + scroll + wheel events.
   - Property: `desiredCol` survives vertical motion across rows of varying length — landing on a short row clamps `cursor.pos.Column`, but the next jump to a longer row restores the desired column.
   - Property: cursor's source-space `Position` is invariant under terminal resize (only its display position changes).
   - Property: every entry into visual mode has a dismiss path (`Esc`, `y`, mode switch, file switch, mouse drag start). [CLAUDE.md "every state has a dismiss path"]
   - Property: visual-mode `y` copies the same text the highlight rendered (analogous to `TestProperty_DragSelectsCorrectText`).
   - Snapshot: cursor at a wrap-row boundary (Phase A convention).
   - Snapshot: cursor in PR description and CI logs (non-diff-pane behavior).

6. **`o` deep-link cascade.** Single-line URL when no selection, line-range URL when selection active. Falls back PR → branch → repo per `IDEAS.md`. `Side` is already in place from 5b.

**Open UX questions** (don't block the refactor but need answers as features land):

- Once cursor diverges from viewport-top, what defines "the current hunk" for the title-bar display — cursor, viewport, or visible range? Current code uses visible range; cursor-based might feel more natural with visual mode active.
- Whether `progressPercent` ever switches to cursor-based. Tentative answer: no, viewport-based stays — "% seen" is more useful for diff review than "where is the cursor."
- What the cursor does when the diff content refreshes underneath it (file watcher fires mid-cursor). Tentative answer: snap to nearest valid line, same pattern as remembered scroll position.
- Jumplist (`Ctrl-O`/`Ctrl-I`) for "wheel-scroll dragged my cursor away from where I was reading." Defer; revisit if the drag-along behavior becomes annoying in practice.
- Phase B: whether `endpoint.VpRow` can be retired after 5c without observable regressions to drag at wrap-row boundaries. Decide with real data, not in advance.

**Deferred (need design before implementation):**

- **Whole-scope diff** (`IDEAS.md: SCOPE & THE FILES VIEW`). Orthogonal in code structure (it determines viewport content), but interacts via `Position.Ref` needing per-line provenance from the scope-diff renderer so deep-links and comments anchor to the right ref. Open: what's the base when the scope spans multiple refs? UI for switching modes?
- **Inline session comments** (`IDEAS.md: SESSION / PR COMMENTS #1`). Anchoring strategy is the dominant question — blob-hash (canonical, breaks on edits) vs surrounding-line-hash (fuzzy, survives small edits). Choice depends on whether comments are ephemeral per session or persistent.
- **PR comments** (`IDEAS.md: SESSION / PR COMMENTS #2`). GitHub already solves anchoring; mostly fetch + display + post. Worth designing alongside local session comments so they share a data model.
- **LSP semantic browsing** (`IDEAS.md: SEMANTIC BROWSING`). Heaviest data-layer extension (process management, indexing). Position's `Column` field is forward-compatible; full implementation deferred.

**Testing:**

Step 1 is a pure internal refactor with no behavior change, so guards should test observable behavior, not internal helpers. The existing rapid suite already does this — keypress sequences in, rendered output / model state asserted out — and survives internal renaming because it doesn't name the internals.

- **Existing observable-behavior coverage is the primary guard for step 1.** Strong: `TestProperty_InteractionInvariants` (viewport↔source round-trip), `TestProperty_DragSelectsCorrectText` + `TestProperty_DragAcrossModesNoPanic`, the three `TestProperty_ProgressPercent_*` properties. Moderate: `title_test.go` + snapshots for hunk-title rendering.
- **Add one golden snapshot before step 1** capturing a representative complex state (multi-hunk file, mid-scroll, drag-selected, progress percent showing) as a concrete before/after artifact. Snapshot tests survive internal renames because they assert on rendered strings.
- **Defer property tests for `Position` and `Range`** until those types are doing something observable — step 2 when drag stores them, step 5 when visual mode populates `Column`. At that point the tests guard features, not abstractions, so they survive future rearrangement.
- New rapid tests for visual-mode selection (step 5) mirror the drag tests in shape.

---

## Completed Features

Core features (all original tasks complete):
- Files, commits, pr modes (plus help overlay)
- Switching between modes retains per-mode view state (sidebar selection, scroll positions, focus)
- Status bar with 3 lines: mode bar, git status, PR/GitHub status
- Sidebar with tree view, collapse/expand, category separators
- Main pane with diff coloring, word wrap, gutter, search
- File watcher with debounced live refresh
- Mouse support: clicks, scrolling, drag-to-copy, hover
- Clickable status bar: mode labels (line 1), commit count (line 2), PR elements (line 3)
- Review requests displayed in status bar
- CI status with text labels and clickable jump to CI results
- RWX CI log integration: async-fetches run results and failed task logs
- Adaptive PR refresh: 30s when active, 10m when idle or stale
- PR description with dates, markdown rendering (goldmark), and deployments
- Comments and reviews show author with timestamp + markdown rendering
- CI checks show start/completion timestamps and URL
- PR deployments fetched via GitHub GraphQL API

## Previously Known Limitations (now resolved)

- PR description markdown: resolved by using goldmark + custom ANSI renderer
- PR deployments: resolved by using GitHub GraphQL API via `gh api graphql`
- Sidebar emoji truncation: resolved by switching to runewidth-aware truncation

## Performance

- Startup performance tests verify UI renders immediately with loading state before data loads
- Init() splits loading into local git data (fast) + PR API data (slow) so the UI is usable within milliseconds
- Manual refresh ([r]) also uses split loading to avoid blocking on GitHub API
- Benchmarks track View() and RenderOnce performance over time
- Full test suite completes in ~19s by default (5 rapid iterations, 3-20 steps)
- For thorough verification: `PRWATCH_RAPID_CHECKS=100 go test -race ./...`

## Test Coverage

Target: 90%+ for UI and git packages.
- `internal/ui`: ~87.7%
- `internal/git`: ~86.1%
- `internal/watcher`: ~86.4%

Includes property-based invariant tests (line count, line width, sidebar click, drag-copy, tree navigation, interaction invariants) and 16 golden file snapshot tests.
