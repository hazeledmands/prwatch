# Refactor Ideas

Standing list of refactor opportunities noticed in passing but deferred so
the work in flight stays focused. New entries go at the top; once a
refactor is taken on or rejected, prune the entry.

## Drop pixel storage from dragSelection (slice 5)

After the source-space rendering migration, `dragSelection` still keeps
`startX, startY, endX, endY` for three narrow reasons:

- `HasRange()` checks `startX != endX || startY != endY`.
- `AdvanceAutoScroll` mutates `startY` (kept anchor pixel-pinned across
  viewport scrolls). With source-space anchoring this mutation is
  unnecessary — the anchor's source line is naturally stable across
  scrolls.
- `resolveSelectionEnds` reads `d.endY` to derive "active is currently
  above vs below content" when `sel.Active` is nil.

Slice 5 plan:
1. Replace the active-direction signal — either a richer endpoint type
   (`Endpoint { Pos *Position; OutsideAbove bool }`) or carry the
   direction in `dragSelection` alongside `sel.Active`.
2. Rewrite `HasRange` against `Selection`.
3. Drop the `d.startY -= delta` line in `AdvanceAutoScroll`. Update the
   `TestDragAutoScroll_PastBottomEdgeStartsScrolling` invariant — it
   currently asserts the mutation; rewrite to assert anchor stability in
   source space (selection still spans the original click line after
   scrolling).
4. Remove the pixel fields from the struct and `Begin/MoveEnd/Release`
   signatures. Tests setting pixel fields directly (`setDragRect` in
   `drag_test.go`) become `setDragSelection(m, anchor, anchorRow,
   active, activeRow)` or similar.

The forcing function is visual-mode (PLAN.md step 5): keyboard cursors
are source-native, with no pixel coords to fall back on. Doing slice 5
before step 5 makes step 5's wiring cleaner.
