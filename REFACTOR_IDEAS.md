# Refactor Ideas

Standing list of refactor opportunities noticed in passing but deferred so
the work in flight stays focused. New entries go at the top; once a
refactor is taken on or rejected, prune the entry.

## Drag rendering still uses pixel coordinates

`ApplyHighlight` and `SelectedText` in `internal/ui/drag.go` iterate the
viewport's rendered output by screen Y and clip by screen X. The
Position-based refactor (PLAN.md step 2) replaces the *storage* of the
drag's anchor/active with `Selection { Anchor, Active *Position }`, but
the rendering still works in pixel space — derived from the Selection
at call time today.

A fuller migration would make ApplyHighlight / SelectedText iterate by
source line + column directly. That eliminates the pixel↔source
round-trip per call and removes a class of bugs where the viewport's
formatted rows don't line up with the Selection (wrap toggle mid-drag,
viewport resize during auto-scroll, etc.). It's a meaningful rewrite of
both methods, not a drop-in change, and worth doing when there's a
forcing function (e.g., visual mode in step 5 making the source-space
operation more obviously natural).

## `SelectedText` is doing too many jobs

`drag.go:SelectedText` mixes swap-normalization, off-content clamping,
absolute-row computation, gutter stripping, wrap-continuation handling,
and text extraction in a single ~130-line function. Splitting along
those seams would make it easier to test the column-clipping logic in
isolation (which is where most drag-property failures have lived) and
would simplify the Position-based rewrite mentioned above. Defer until
that rewrite happens — splitting first and then rewriting would churn
the same lines twice.
