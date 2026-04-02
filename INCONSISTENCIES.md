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
