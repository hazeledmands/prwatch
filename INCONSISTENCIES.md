# Spec Inconsistencies / Ambiguities

## Mode-switch key aliases (`v`/`c`/`b`) removed

Spec: PROMPT.md:287-289 lists `files-mode` on keys `v`, `1`; `commits-mode` on
`c`, `2`; `pr-mode` on `b`, `3`.

Code: `keys.go` comment notes `v`/`c`/`b` were removed as mode-switch aliases
once `v` became the visual-mode entry key. Only `1`/`2`/`3` remain for mode
switching. Visual mode itself is not in PROMPT.md at all.

Open question: is this alias removal intentional and should PROMPT.md be
updated to drop `v`/`c`/`b` and add visual mode, or should the aliases be
restored under different keys? Deliberate change, but never logged against
the spec.

## GitHub API errors hidden once PR data exists

Spec: PROMPT.md:83 — "if the github API is returning errors, then put the
error message here!" (line 3 of the status bar).

Code: `renderLine3` (statusbar.go:108-127) never reads `prError` once
`pr.Number > 0` — once PR data has been fetched successfully at least once,
subsequent API errors are invisible on line 3.

Open question: is silently keeping stale-but-last-known-good PR data on
screen (instead of surfacing the new error) the intended behavior, or should
errors always take priority on line 3 regardless of whether PR data is
already loaded?

## Drag-copy drops the space at word-wrap breaks

Spec: PROMPT.md:365 — "copied text should be the same as the text from the
file (or diff) that is being copied - it should not carry over extra
newlines when the text in the UI wraps."

Code: drag.go:467-468 drops the space character that was consumed at a
word-wrap break, so copied text loses a space that exists in the source
file/diff.

Open question: the spec only calls out not adding extra newlines at wrap
points; it doesn't say whether the wrap-consumed space should be preserved
or dropped. Need a decision on whether dropping that space is correct or a
bug.

## Hover highlight only implemented for one element

Spec: PROMPT.md:361 — "hovering over clickable elements highlights them with
a different background color."

Code: only line-1 mode labels get a hover highlight, and it's rendered as an
underline rather than a background color. No other clickable elements
(line-3 status items, sidebar entries, etc.) have hover highlighting at all.

Open question: is background-color hover highlighting meant to be rolled out
to all clickable elements, or was line-1-only, underline-style hover a
deliberate simplification?

## Commits-mode pseudo-entry body content (staged/unstaged/untracked)

Spec: doesn't specify what each commits-mode pseudo-entry's *body* should
show for staged changes, unstaged changes, and untracked files respectively.

Code: current behavior renders identical bodies for all three pseudo-entries
— clearly wrong, since they're meant to represent different diffs/content.

Open question: what should each pseudo-entry's body actually show (e.g.
staged diff, unstaged diff, untracked file listing/contents)? Needs a
decision before this can be fixed.
