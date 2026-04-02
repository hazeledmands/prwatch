# Spec Inconsistencies / Ambiguities

## Commit mode category 4: "commits after the stuff that's already in the base branch"

The spec lists 4 commit sidebar categories:

1. unpushed changes (uncommitted files grouped together)
2. commits that have not yet been pushed to the origin (dimmed)
3. commits in the current branch / PR that have been pushed to the origin
4. commits after the stuff that's already in the base branch

Category 4 is ambiguous. The commit list from `git log base..HEAD` only contains
commits that are in the feature branch and NOT in the base branch. So there are
no "commits after the stuff that's already in the base branch" in that range.

Possible interpretations:
- Show additional commits from the base branch for context (could be very long)
- Label the end of the PR commits with "base: <sha>" as a divider
- This may refer to a scenario where the base branch has advanced since the PR
  was created, and those new base commits should be shown

Currently implementing categories 1-3 only. Clarification needed from spec author.
