package ui

import (
	"fmt"
	"strings"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// pathsInSection returns the sorted paths in cf for section s as a plain
// []string, for callers (sidebar/commits-mode pseudo-entry detection, status
// bar counts) that still want a flat list rather than the per-file view.
func pathsInSection(cf *gitpkg.ChangedFiles, s gitpkg.Section) []string {
	entries := cf.InSection(s)
	out := make([]string, len(entries))
	for i, f := range entries {
		out[i] = f.Path
	}
	return out
}

// buildFilesSidebar constructs the sidebar item list for files mode. It also
// lazily initializes collapse state for newly-seen directories — the collapsed
// map is mutated in place.
//
// The Uncommitted/Staged/Committed sections come from cf.InSection; the All
// Files section is built from allFiles minus paths already in cf, plus
// ignored files when showIgnored is true. Change-type badges ([→]/[-]/[+]/
// [±]) are applied via applyChangeBadges after the tree is assembled.
func buildFilesSidebar(
	cf *gitpkg.ChangedFiles,
	allFiles []string,
	ignoredFiles map[string]bool,
	ignoredDirs map[string]bool,
	collapsed map[string]bool,
	showIgnored, isGit bool,
) []sidebarItem {
	pathsIn := func(s gitpkg.Section) []string {
		entries := cf.InSection(s)
		out := make([]string, len(entries))
		for i, f := range entries {
			out[i] = f.Path
		}
		return out
	}
	uncommitted := pathsIn(gitpkg.SectionUncommitted)
	staged := pathsIn(gitpkg.SectionStaged)
	committed := pathsIn(gitpkg.SectionCommitted)

	changedSet := make(map[string]bool, len(uncommitted)+len(staged)+len(committed))
	for _, f := range uncommitted {
		changedSet[f] = true
	}
	for _, f := range staged {
		changedSet[f] = true
	}
	for _, f := range committed {
		changedSet[f] = true
	}

	var otherFiles []string
	for _, f := range allFiles {
		if !changedSet[f] {
			otherFiles = append(otherFiles, f)
		}
	}
	if showIgnored {
		for path := range ignoredFiles {
			if !changedSet[path] {
				otherFiles = append(otherFiles, path)
			}
		}
	}

	// Auto-collapse rules for the changed sections: unseen dirs default to
	// expanded for git repos (except dot-prefixed which collapse), and
	// uniformly collapsed for non-git directories.
	autoCollapse := func(section string, dirs []string) {
		for _, d := range dirs {
			key := dirCollapseKey(section, d)
			if _, exists := collapsed[key]; exists {
				continue
			}
			if !isGit {
				collapsed[key] = true
				continue
			}
			base := d
			if i := strings.LastIndex(d, "/"); i >= 0 {
				base = d[i+1:]
			}
			collapsed[key] = strings.HasPrefix(base, ".")
		}
	}
	autoCollapse(sectionUncommitted, extractDirs(uncommitted))
	autoCollapse(sectionStaged, extractDirs(staged))
	autoCollapse(sectionCommitted, extractDirs(committed))

	itemKind := func(f string) sidebarItemKind {
		if entry, ok := cf.Get(f); ok && entry.Class == gitpkg.ClassDeleted {
			return itemDeleted
		}
		return itemNormal
	}
	otherKind := func(f string) sidebarItemKind {
		if ignoredFiles[f] {
			return itemDim
		}
		return itemNormal
	}

	var items []sidebarItem
	if len(uncommitted) > 0 {
		items = append(items, sidebarItem{label: fmt.Sprintf("New Changes (%d)", len(uncommitted)), kind: itemHeader})
		items = append(items, buildTreeItems(uncommitted, itemNormal, sectionUncommitted, collapsed, nil)...)
	}
	if len(staged) > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("Staged (%d)", len(staged)), kind: itemHeader})
		items = append(items, buildTreeItems(staged, itemNormal, sectionStaged, collapsed, nil)...)
	}
	if len(committed) > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("Committed (%d)", len(committed)), kind: itemHeader})
		items = append(items, buildTreeItems(committed, itemNormal, sectionCommitted, collapsed, nil, itemKind)...)
	}
	if len(otherFiles) > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("All Files (%d)", len(otherFiles)), kind: itemHeader})
		// All-files trees default to collapsed (per spec).
		for _, d := range extractDirs(otherFiles) {
			key := dirCollapseKey(sectionAllFiles, d)
			if _, exists := collapsed[key]; !exists {
				collapsed[key] = true
			}
		}
		for d := range ignoredDirs {
			key := dirCollapseKey(sectionAllFiles, d)
			if _, exists := collapsed[key]; !exists {
				collapsed[key] = true
			}
		}
		items = append(items, buildTreeItems(otherFiles, itemNormal, sectionAllFiles, collapsed, ignoredDirs, otherKind)...)
	}

	return applyChangeBadges(items, cf)
}

// buildCommitsSidebar constructs the sidebar for commits mode.
func buildCommitsSidebar(
	commits, baseCommits []gitpkg.Commit,
	uncommitted, staged []string,
	aheadCount, commitsLoaded, commitCount int,
) []sidebarItem {
	var items []sidebarItem
	unpushed := aheadCount
	pushedCount := len(commits) - unpushed
	if pushedCount < 0 {
		pushedCount = 0
	}

	if len(uncommitted) > 0 {
		items = append(items, sidebarItem{label: fmt.Sprintf("New Changes (%d files)", len(uncommitted)), kind: itemHeader})
		items = append(items, sidebarItem{label: pseudoNewChangesLabel, kind: itemDim})
	}

	if len(staged) > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("Staged (%d files)", len(staged)), kind: itemHeader})
		items = append(items, sidebarItem{label: pseudoStagedLabel, kind: itemDim})
	}

	unpushedVisible := unpushed
	if unpushedVisible > len(commits) {
		unpushedVisible = len(commits)
	}
	if unpushedVisible > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("Unpushed (%d)", unpushedVisible), kind: itemHeader})
		for i := 0; i < unpushedVisible; i++ {
			c := commits[i]
			items = append(items, sidebarItem{
				label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				kind:  itemDim,
			})
		}
	}

	if pushedCount > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("Pushed (%d)", pushedCount), kind: itemHeader})
		for i := unpushed; i < len(commits); i++ {
			c := commits[i]
			items = append(items, sidebarItem{
				label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				kind:  itemNormal,
			})
		}
	}

	if commitsLoaded < commitCount {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemSeparator})
		}
		remaining := commitCount - commitsLoaded
		items = append(items, sidebarItem{
			label: fmt.Sprintf("load more (%d remaining)", remaining),
			kind:  itemDim,
		})
	}

	if len(baseCommits) > 0 {
		if len(items) > 0 {
			items = append(items, sidebarItem{kind: itemCutline})
		}
		items = append(items, sidebarItem{label: fmt.Sprintf("Base (%d)", len(baseCommits)), kind: itemHeader})
		for _, c := range baseCommits {
			items = append(items, sidebarItem{
				label: fmt.Sprintf("%.7s %s", c.SHA, c.Subject),
				kind:  itemDim,
			})
		}
	}

	return items
}

// buildPRSidebar constructs the sidebar for PR mode.
func buildPRSidebar(comments []gitpkg.PRComment, reviews []gitpkg.PRReview, checks []gitpkg.CICheck) []sidebarItem {
	var items []sidebarItem
	items = append(items, sidebarItem{label: "Description", kind: itemNormal})
	items = append(items, sidebarItem{kind: itemSeparator})

	items = append(items, sidebarItem{label: fmt.Sprintf("Comments (%d)", len(comments)), kind: itemHeader})
	for i, c := range comments {
		items = append(items, sidebarItem{
			prefix: prCommentPrefix(i, len(comments)),
			label:  prCommentLabel(c),
			suffix: " " + relativeTime(c.CreatedAt),
			kind:   itemNormal,
		})
	}
	if len(comments) == 0 {
		items = append(items, sidebarItem{label: "(no comments)", kind: itemDim})
	}

	items = append(items, sidebarItem{kind: itemSeparator})
	items = append(items, sidebarItem{label: fmt.Sprintf("Reviews (%d)", len(reviews)), kind: itemHeader})
	for i, r := range reviews {
		items = append(items, sidebarItem{
			prefix: prReviewPrefix(i, len(reviews), r),
			label:  prReviewLabel(r),
			suffix: " " + relativeTime(r.SubmittedAt),
			kind:   itemNormal,
		})
	}
	if len(reviews) == 0 {
		items = append(items, sidebarItem{label: "(no reviews)", kind: itemDim})
	}

	items = append(items, sidebarItem{kind: itemSeparator})
	items = append(items, sidebarItem{label: fmt.Sprintf("CI (%d)", len(checks)), kind: itemHeader})
	for _, check := range checks {
		indicator := ciCheckPrefix(check)
		ts := check.CompletedAt
		if ts.IsZero() {
			ts = check.StartedAt
		}
		items = append(items, sidebarItem{
			prefix: indicator,
			label:  ciCheckLabel(check),
			suffix: " " + relativeTime(ts),
			kind:   itemNormal,
		})
	}
	if len(checks) == 0 {
		items = append(items, sidebarItem{label: "(no CI checks)", kind: itemDim})
	}

	return items
}
