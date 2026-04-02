# Spec Inconsistencies / Ambiguities

## Copy selection with word wrap

The spec says: "copied text should be the same as the text from the file (or
diff) that is being copied - it should not carry over extra newlines when the
text in the UI wraps."

Current implementation copies from the viewport's rendered content, which includes
wrap-induced newlines. Properly mapping viewport coordinates back to source content
requires significant complexity due to ANSI codes, gutter formatting, and diff
annotations. Flagging for future improvement.
