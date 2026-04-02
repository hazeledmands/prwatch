# Spec Inconsistencies / Ambiguities

## PR description markdown formatting

The spec says: "PR description with markdown formatting". The charmbracelet/glamour
library (the standard Go terminal markdown renderer) has dependency conflicts with
the bubbletea/v2 ecosystem used by this project. PR descriptions are displayed as
plain text until the dependency conflict is resolved upstream.

## PR deployments

The spec mentions "deployments" in the PR view description panel. The GitHub CLI
`gh pr view` doesn't expose deployment data in its JSON output. This would require
using the GitHub REST/GraphQL API directly. Flagging for future implementation.

## Copy selection with word wrap (RESOLVED)

Previously used a heuristic to detect wrap-continuation lines. Now uses an explicit
`wrapContinuation` boolean map built during the wrapping process, which correctly
identifies continuation lines regardless of mode or gutter width.
