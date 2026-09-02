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

**Resolved — spec updated.** The alias removal was intentional; PROMPT.md's
mode-switching table now lists only `1`/`2`/`3` and a new `### visual mode`
section documents `v`/`V`/`esc` and the yank semantics. No code change.

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

**Resolved — both. Implemented.** PROMPT.md:83 is taken at face value: an
active API error is surfaced on line 3 even when PR data already exists,
while the last-known-good PR data stays rendered elsewhere (the PR pane).
`renderStatusBar`'s line-3 chain is now a switch with the error first, so an
active error *replaces* line 3's content until the next successful fetch
clears it — precedence, not composition, since the spec asks for the message
on this line and says nothing about combining it with the PR summary. Landed
with the `isRateLimited` 403 fix; see BUG_REPORTS.md, "GitHub error paths:
classification and visibility".

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

**Resolved — preserved.** Read as a copy-fidelity requirement ("the same as
the text from the file"), so the copy path now restores the spaces a wrap
break consumed, without changing what is rendered. See BUG_REPORTS.md, "Word
wrap dropped the space it broke on". The related question below — a line's
*own* trailing spaces — is still open.

## Whole-line yank drops the line's own trailing spaces

Spec: PROMPT.md:365 — "copied text should be the same as the text from the
file (or diff) that is being copied", against PROMPT.md:364 — the highlight
"should only cover the relevant content that will be copied — not TUI
glyphs, border characters, or gutter content."

Code: `stripGutterText` (mainpane.go) trims trailing blanks off every
rendered row before the copy path slices it, so a source line ending in
spaces is copied without them. This is independent of wrapping — it applies
to unwrapped lines the same way — and it is the same trim that keeps
rendered padding out of copied text.

Open question: is a line's trailing whitespace "text from the file" that a
whole-line yank must reproduce, or invisible padding that a user never meant
to select and that :364 arguably wants excluded? A fix means a trailing-space
policy for `stripGutterText` that distinguishes *content* spaces from
*render padding* spaces, which the rendered row does not currently
distinguish. Needs a decision before changing anything.

**Resolved — split by selection kind.** Line-wise `V` selections are
source-text operations and reproduce lines exactly, trailing whitespace
included; cell-wise selections (drag, `v`) are screen operations and keep
the trim. Now specified in PROMPT.md (`### visual mode` and the mouse
copy bullets). **Implemented** — same bookkeeping pattern as the
wrap-break-space fix: `wrapLinesWithBreaks` now also returns each source
line's own trailing-space count (recorded on the line's last wrap row), and
`extractSourceRange` re-appends it at the join for line-wise selections
only. See BUG_REPORTS.md, "Whole-line yank dropped the line's own trailing
spaces".

## Line-wise yank is a visible-window operation when word wrap is off

**OPEN — needs a decision.**

Spec: PROMPT.md `### visual mode` — "line-wise selections (`V`) are source-text
operations — the copied text reproduces each selected source line exactly,
including its trailing whitespace."

Code: with wrap off, `refreshViewport` (mainpane.go:678-688) populates
`lineTrailingSpaces` from the rows *after*
`truncateLinesWithOffset` has applied the horizontal scroll and clipped
each line at the pane's right edge. Two consequences for a `V` yank of a
line wider than the pane:

- the copy is the visible window of the line, not the line — everything
  past the right edge is missing, so it is not reproduced "exactly";
- whatever spaces happen to end that visible window get re-appended as if
  they were the line's own trailing run, even when they are interior spaces
  with more text after them.

This is not new behavior introduced by the trailing-space fix. Copy in
no-wrap mode has always been visible-only — `extractSourceRange` documents
it (drag.go:485-488: "In no-wrap mode each source line has exactly one wrap
row in `viewport.GetContent()` (post horizontal-truncation), so the copy
preserves only visible chars"). The fix is internally consistent with that
existing rule; the fix's own round-trip property is scoped to wrap mode for
exactly this reason. But the rule and the new `V` sentence in the spec now
strictly contradict each other, and only one of them can be right.

Two ways to resolve, both small:

(a) **Adjudicate no-wrap copies as visible-window operations** (spec
adjustment). Say in PROMPT.md that with word wrap off, every copy — `V`
included — is bounded by what is on screen, since the pane is showing a
window onto the line and there is no un-truncated row to read from. Costs
nothing in code; makes `V`'s "exactly" conditional on wrap being on.

(b) **Make `V` source-text in both modes** (code change). Have the
line-wise path read from `pane.formattedContent` — the pre-truncation,
pre-wrap rows — via `sourceToFormatLine`, instead of from the truncated
viewport rows. Well-contained: `formattedContent` is already stored for
line mapping and already has the gutter applied, so `stripGutterBody` reads
it the same way. It would also make the trailing count for no-wrap mode
come from the same place the wrap path gets it, rather than from the
render.

Not implemented pending a decision — (b) changes what `V` copies in no-wrap
mode, which is a product call, not a refactor.

## Hover highlight only implemented for one element

Spec: PROMPT.md:361 — "hovering over clickable elements highlights them with
a different background color."

Code: only line-1 mode labels get a hover highlight, and it's rendered as an
underline rather than a background color. No other clickable elements
(line-3 status items, sidebar entries, etc.) have hover highlighting at all.

Open question: is background-color hover highlighting meant to be rolled out
to all clickable elements, or was line-1-only, underline-style hover a
deliberate simplification?

**Resolved — simplification blessed.** PROMPT.md's hover bullet now
describes the actual behavior (underline on line-1 mode labels only) and
marks it deliberate. No code change.

## Commits-mode pseudo-entry body content (staged/unstaged/untracked)

Spec: doesn't specify what each commits-mode pseudo-entry's *body* should
show for staged changes, unstaged changes, and untracked files respectively.

Code: current behavior renders identical bodies for all three pseudo-entries
— clearly wrong, since they're meant to represent different diffs/content.

Open question: what should each pseudo-entry's body actually show (e.g.
staged diff, unstaged diff, untracked file listing/contents)? Needs a
decision before this can be fixed.

**Resolved — semantics defined.** Staged → the staged diff
(`git diff --cached`); new changes → the working-tree diff against the
index; untracked → each file's contents rendered as a new-file diff. Now
specified in PROMPT.md's commits-mode section.

**Implemented.** `internal/git/pseudodiff.go` supplies the three sources
(`StagedDiff`, `UnstagedDiff` + `UntrackedDiff`, composed by
`NewChangesDiff`); `internal/ui/commitspseudo.go` turns each into a body and
its own shortstat. Untracked content lands in the "new changes" body, since
PROMPT.md groups untracked and unstaged under that one sidebar entry rather
than giving untracked an entry of its own. See BUG_REPORTS.md, "Commits-mode
pseudo-entry bodies were all the same diff".
