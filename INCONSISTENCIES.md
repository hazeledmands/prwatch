# Spec Inconsistencies / Ambiguities

## PR description markdown formatting (RESOLVED)

The spec says: "PR description with markdown formatting". Implemented using goldmark
for parsing with a custom ANSI renderer. Supports headings, bold, italic, code blocks,
inline code, lists, links, blockquotes, and horizontal rules.

## PR deployments (RESOLVED)

Implemented using GitHub GraphQL API via `gh api graphql`. Fetches deployment statuses
from the PR's head commit and displays them in the PR description panel. The GraphQL
call is made as part of the existing PR refresh cycle (best-effort, won't fail if
deployments aren't available).

## Copy selection with word wrap (RESOLVED)

Previously used a heuristic to detect wrap-continuation lines. Now uses an explicit
`wrapContinuation` boolean map built during the wrapping process, which correctly
identifies continuation lines regardless of mode or gutter width.
