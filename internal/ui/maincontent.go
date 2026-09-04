package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// itemSetter is the small interface updateMainContent uses to record the
// (mode, item) currently displayed and restore per-item scroll state.
type itemSetter func(key mainItemKey)

// updateNonGitFilesMode populates the main pane in non-git mode: just reads
// the selected file directly from disk. Returns true if it handled the call;
// false means setItem was not called and the caller should fall through.
func (m *Model) updateNonGitFilesMode(setItem itemSetter) {
	if m.mode != FilesMode {
		return
	}
	file := m.sidebar.SelectedItem()
	if file == "" {
		m.mainPane.ShowItem(paneContent{})
		setItem(mainItemKey{m.mode, ""})
		return
	}
	if m.sidebar.SelectedIsDir() {
		return // preserve current main panel content (and lastMainItem)
	}
	content, err := os.ReadFile(filepath.Join(m.dir, file))
	if err != nil {
		m.mainPane.ShowItem(paneContent{body: fmt.Sprintf("Error: %v", err)})
		setItem(mainItemKey{m.mode, file})
		return
	}
	if isBinaryContent(string(content)) {
		m.mainPane.ShowItem(paneContent{body: "[binary content]"})
		setItem(mainItemKey{m.mode, file})
		return
	}
	m.mainPane.ShowItem(paneContent{body: string(content), filename: file})
	setItem(mainItemKey{m.mode, file})
}

// updateFilesModeContent populates the main pane for files mode in a git repo.
func (m *Model) updateFilesModeContent(setItem itemSetter) {
	file := m.sidebar.SelectedItem()
	if file == "" {
		m.mainPane.ShowItem(paneContent{})
		setItem(mainItemKey{m.mode, ""})
		return
	}
	if m.sidebar.SelectedIsDir() {
		return // preserve current main panel content (and lastMainItem)
	}
	content, err := m.git.FileContent(file)
	if err != nil {
		m.mainPane.ShowItem(paneContent{
			body:       fmt.Sprintf("Error: %v", err),
			titleLeft:  sanitizeDisplayText(file),
			titleRight: "error",
		})
		setItem(mainItemKey{m.mode, file})
		return
	}
	if isBinaryContent(content) {
		m.mainPane.ShowItem(paneContent{
			body:         "[binary content]",
			noHunkRight:  m.fileContextRight(file, true),
			titleLeft:    m.fileTitleLeft(file),
			titleDynamic: true,
		})
		setItem(mainItemKey{m.mode, file})
		return
	}
	var diff string
	if m.isPureRename(file) {
		// Pure rename: skip diff entirely so the title bar's no-diff
		// "renamed · …" right side fires. FileDiff* would otherwise return
		// either a header-only diff (committed/staged rename) or a
		// synthetic /dev/null diff (untracked working-tree rename), and the
		// diff branch below would render the file as all-additions or "no
		// changes" instead.
	} else if m.isUncommittedFile(file) {
		// Same left endpoint as the committed branch: a file with both
		// committed and uncommitted changes lands in the uncommitted section,
		// and diffing it against HEAD hid its committed base..HEAD layer.
		diff, _ = m.git.FileDiffUncommitted(m.scope.OldBase(), file)
	} else if m.isCommittedFile(file) {
		diff, _ = m.git.FileDiffCommitted(m.scope.OldBase(), file)
	}
	spec := paneContent{
		body:         content,
		filename:     file,
		titleLeft:    m.fileTitleLeft(file),
		titleDynamic: true,
	}
	if diff != "" {
		annotations := parseDiffAnnotations(diff)
		// For completely deleted files, mark all lines as removed.
		if m.isDeletedFile(file) {
			contentLines := strings.Split(content, "\n")
			annotations = make(map[int]diffAnnotation, len(contentLines))
			for i := range contentLines {
				annotations[i+1] = diffAnnotation{kind: diffLineRemoved}
			}
		}
		spec.annotations = annotations
		spec.hunks = parseDiffHunks(diff)
		spec.diffPrefix = m.fileDiffPrefix(file)
	} else {
		spec.noHunkRight = m.fileContextRight(file, false)
	}
	m.mainPane.ShowItem(spec)
	setItem(mainItemKey{m.mode, file})
}

// updateCommitsModeContent populates the main pane for commits mode.
func (m *Model) updateCommitsModeContent(setItem itemSetter) {
	selected := m.sidebar.SelectedItem()
	if selected == "" {
		m.mainPane.ShowItem(paneContent{isDiff: true})
		setItem(mainItemKey{m.mode, ""})
		return
	}
	role := m.sidebar.SelectedRole()
	if role == roleLoadMore {
		m.mainPane.ShowItem(paneContent{body: "Loading more commits...", titleLeft: selected})
		setItem(mainItemKey{m.mode, selected})
		return
	}
	if role == rolePseudoNewChanges || role == rolePseudoStaged {
		// Each pseudo-entry has its own diff source (PROMPT.md, commits mode).
		// Sharing one `git diff HEAD` between them conflated staged with
		// unstaged and dropped untracked content entirely.
		fetch := m.git.NewChangesDiff
		if role == rolePseudoStaged {
			fetch = m.git.StagedDiff
		}
		// Memoized per git-load cycle: this runs on every updateMainContent,
		// and the untracked half spawns one subprocess per untracked file.
		content, err := m.pseudoDiffs.Get(selected, fetch)
		if err != nil {
			m.mainPane.ShowItem(paneContent{body: fmt.Sprintf("Error: %v", err), titleLeft: selected})
			setItem(mainItemKey{m.mode, selected})
			return
		}
		m.mainPane.ShowItem(paneContent{
			body:       content.body,
			isDiff:     content.asDiff,
			titleLeft:  selected,
			titleRight: content.titleRight,
		})
		setItem(mainItemKey{m.mode, selected})
		return
	}
	// The selected row carries the commit it was built from (sidebarItem.commit).
	// Resolving it here rather than searching a commit list by sha is what lets
	// the Base section — built from m.baseCommits — render at all.
	selectedCommit := m.sidebar.SelectedCommit()
	if selectedCommit == nil {
		m.mainPane.ShowItem(paneContent{isDiff: true})
		setItem(mainItemKey{m.mode, ""})
		return
	}
	commit := *selectedCommit
	patch, err := m.git.CommitPatch(commit.SHA)
	titleLeft := commitTitleLeft(commit)
	titleRight := formatAuthorAndTime(commit.Author, commit.AuthorDate)
	if err != nil {
		m.mainPane.ShowItem(paneContent{
			body:       fmt.Sprintf("Error: %v", err),
			isDiff:     true,
			titleLeft:  titleLeft,
			titleRight: titleRight,
		})
		setItem(mainItemKey{m.mode, selected})
		return
	}
	if isBinaryContent(patch) {
		m.mainPane.ShowItem(paneContent{body: "[binary content]", titleLeft: titleLeft, titleRight: titleRight})
		setItem(mainItemKey{m.mode, selected})
		return
	}
	m.mainPane.ShowItem(paneContent{body: patch, isDiff: true, titleLeft: titleLeft, titleRight: titleRight})
	setItem(mainItemKey{m.mode, selected})
}

// updatePRModeContent populates the main pane for PR mode.
func (m *Model) updatePRModeContent(setItem itemSetter) {
	selected := m.sidebar.SelectedItem()
	it := m.sidebar.SelectedRow()
	// Each arm asks the row for a referent rather than trusting the role to
	// imply one, so a row whose role and payload disagree lands in the default
	// arm and clears the pane instead of panicking mid-render.
	switch {
	case it.role == rolePRDescription:
		m.mainPane.ShowItem(paneContent{body: m.renderPRDescription(), titleLeft: "Description"})
	case it.prComment() != nil:
		c := *it.prComment()
		m.mainPane.ShowItem(paneContent{
			body:       buildCommentContent(c, m.mainPane.width),
			titleLeft:  fmt.Sprintf("comment #%d", it.pr.number),
			titleRight: formatAuthorAndTime(c.Author, c.CreatedAt),
		})
	case it.prReview() != nil:
		r := *it.prReview()
		m.mainPane.ShowItem(paneContent{
			body:       buildReviewContent(r, m.mainPane.width),
			titleLeft:  fmt.Sprintf("review #%d · %s", it.pr.number, reviewStateLabel(r.State)),
			titleRight: formatAuthorAndTime(r.Author, r.SubmittedAt),
		})
	case it.ciCheck() != nil:
		m.applyCICheckContent(*it.ciCheck())
	default:
		// Rows that stand for nothing — the "(no comments)" style
		// pseudo-entries — clear the body so we don't leave stale content
		// from the previously-selected item on screen. Stale content also
		// poisons scroll memory: saving visible.Start.SourceLine while
		// showing a different item's content gives a value that can't
		// round-trip on return.
		m.mainPane.ShowItem(paneContent{titleLeft: selected})
	}
	setItem(mainItemKey{m.mode, selected})
}

// applyCICheckContent renders the CI check the selected row stands for.
func (m *Model) applyCICheckContent(check gitpkg.CICheck) {
	status := check.Bucket
	if status == "" {
		status = check.State
	}
	extra, _ := m.rwxFetcher.Lookup(check)
	m.mainPane.ShowItem(paneContent{
		body:       buildCIContent(check, status, extra),
		titleLeft:  "CI · " + check.Name,
		titleRight: status,
	})
}

// reviewStateEmoji returns the prefix emoji for a PR review state used in
// numbered sidebar labels.
func reviewStateEmoji(state string) string {
	switch state {
	case "APPROVED":
		return "✓ "
	case "CHANGES_REQUESTED":
		return "✗ "
	case "COMMENTED":
		return "c "
	default:
		return "… "
	}
}

// buildCommentContent assembles the main-pane body for a PR comment.
func buildCommentContent(c gitpkg.PRComment, width int) string {
	header := fmt.Sprintf("@%s", c.Author)
	if !c.CreatedAt.IsZero() {
		header += fmt.Sprintf("  •  %s (%s)", c.CreatedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(c.CreatedAt))
	}
	return fmt.Sprintf("%s\n\n%s", header, renderMarkdown(c.Body, width))
}

// buildReviewContent assembles the main-pane body for a PR review (header,
// state, body, inline code comments).
func buildReviewContent(r gitpkg.PRReview, width int) string {
	content := fmt.Sprintf("Review by @%s", r.Author)
	if !r.SubmittedAt.IsZero() {
		content += fmt.Sprintf("  •  %s (%s)", r.SubmittedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(r.SubmittedAt))
	}
	content += fmt.Sprintf("\nState: %s", r.State)
	if r.Body != "" {
		content += "\n\n" + renderMarkdown(r.Body, width)
	}
	for _, c := range r.Comments {
		content += fmt.Sprintf("\n\n--- %s:%d ---\n%s", c.Path, c.Line, renderMarkdown(c.Body, width))
	}
	// Inline comments are fetched one page deep (they hang off the paginated
	// reviews collection, so paging them would be a query per review). Say so
	// when GitHub reported more than we hold — the sidebar row counts reviews
	// and cannot carry this.
	if r.CommentsTotal > len(r.Comments) {
		content += fmt.Sprintf("\n\n(showing %d of %d inline comments)", len(r.Comments), r.CommentsTotal)
	}
	return content
}

// buildCIContent assembles the main-pane body for a CI check.
func buildCIContent(check gitpkg.CICheck, status, extra string) string {
	content := fmt.Sprintf("Check: %s\nStatus: %s", check.Name, status)
	if !check.StartedAt.IsZero() {
		content += fmt.Sprintf("\nStarted: %s (%s)", check.StartedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(check.StartedAt))
	}
	if !check.CompletedAt.IsZero() {
		content += fmt.Sprintf("\nCompleted: %s (%s)", check.CompletedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(check.CompletedAt))
	}
	if check.URL != "" {
		content += fmt.Sprintf("\nURL: %s", check.URL)
	}
	if extra != "" {
		content += "\n\n" + extra
	}
	return content
}
