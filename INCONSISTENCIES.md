# Spec Inconsistencies / Ambiguities

## Copy selection with word wrap

The spec says: "copied text should be the same as the text from the file (or
diff) that is being copied - it should not carry over extra newlines when the
text in the UI wraps."

Current implementation attempts to detect wrap-continuation lines by checking
for gutter-width indentation, but this heuristic doesn't work perfectly for
all modes (especially diff mode where gutter width is 0). A fully correct
implementation requires mapping viewport coordinates back through the wrapping
transformation, which is complex. Flagging for future improvement.

## PR-view mode

The spec defines a PR-view mode that shows PR description, comments, and CI
status. This mode requires additional GitHub API calls (PR body, individual
comments) that are not yet implemented in the git layer. The mode structure
and keybinding are ready but the data fetching and rendering are pending.
