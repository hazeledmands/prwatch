# Spec Inconsistencies / Ambiguities

## PR description markdown formatting

The spec says: "PR description with markdown formatting". The charmbracelet/glamour
library (the standard Go terminal markdown renderer) has dependency conflicts with
the bubbletea/v2 ecosystem used by this project. PR descriptions are displayed as
plain text until the dependency conflict is resolved upstream.

**Proposed paths forward:**
1. Wait for glamour to release a version compatible with bubbletea v2 / lipgloss v2, then integrate.
2. Use a lighter-weight markdown renderer (e.g. `goldmark` to parse → custom ANSI renderer) that doesn't depend on lipgloss v1.
3. Implement minimal markdown formatting manually (bold, italic, headers, code blocks, lists) using ANSI escape codes.

## PR deployments

The spec mentions "deployments" in the PR view description panel. The GitHub CLI
`gh pr view` doesn't expose deployment data in its JSON output. This would require
using the GitHub REST/GraphQL API directly. Flagging for future implementation.

**Proposed paths forward:**
1. Use `gh api` to query the GitHub Deployments API (`GET /repos/{owner}/{repo}/deployments?sha={head_sha}`) and parse the JSON response.
2. Use the GraphQL API via `gh api graphql` to fetch deployment statuses as part of the PR query.
3. Skip deployments entirely if the project doesn't use GitHub Deployments (many teams use external CD).

## Copy selection with word wrap (RESOLVED)

Previously used a heuristic to detect wrap-continuation lines. Now uses an explicit
`wrapContinuation` boolean map built during the wrapping process, which correctly
identifies continuation lines regardless of mode or gutter width.
