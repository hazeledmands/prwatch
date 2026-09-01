package ui

import "strings"

// baseRefForBehind returns the single ref the current branch is measured
// against: the target of the behind-count and the base shown in the status
// bar's `feature → main`. One chain, one definition — see CLAUDE.md,
// "Layout geometry comes from one function"; the same reasoning applies to
// any value that render and data-loading both need.
//
// The chain mirrors PROMPT.md's base-branch detection, restricted to what
// the model already knows without extra git calls:
//
//  1. the PR's baseRefName, when PR data has loaded. Prefer origin/<base>
//     to stay consistent with GitHub's three-dot diff.
//  2. the tracked upstream, when it names a branch *other* than the
//     current one. A branch that tracks origin/main is based on main; one
//     tracking its own origin/<self> says nothing about its base, and
//     using it (as loadGitData used to) counts how far behind the branch
//     is from its own remote copy.
//  3. origin/main — the head of git's local detection chain
//     (origin/main → origin/master → main → master). BehindCount reports
//     0 cleanly when the ref doesn't exist.
func baseRefForBehind(prBaseRef, branch, upstream string) string {
	if prBaseRef != "" {
		return "origin/" + prBaseRef
	}
	if upstream != "" && refBranchName(upstream) != branch {
		return upstream
	}
	return "origin/main"
}

// baseBranchName returns the display name of the base branch — the same
// chain as baseRefForBehind, with the remote prefix stripped.
func baseBranchName(prBaseRef, branch, upstream string) string {
	return refBranchName(baseRefForBehind(prBaseRef, branch, upstream))
}

// refBranchName strips the remote segment from a remote-tracking ref,
// yielding the branch name. Only the FIRST segment is the remote —
// `git rev-parse --abbrev-ref <branch>@{upstream}` returns
// "<remote>/<branch>", and branch names routinely contain slashes
// (`hazel/ui/foo`, `release/1.2`). Stripping the last segment instead
// would turn "origin/hazel/ui/foo" into "foo", which matches neither the
// branch it names nor anything displayable.
func refBranchName(ref string) string {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}
