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
		m.mainPane.SetFilename("")
		m.mainPane.SetPlainContent("")
		setItem(mainItemKey{m.mode, ""})
		return
	}
	if m.sidebar.SelectedIsDir() {
		return // preserve current main panel content (and lastMainItem)
	}
	content, err := os.ReadFile(filepath.Join(m.dir, file))
	if err != nil {
		m.mainPane.SetFilename("")
		m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
		setItem(mainItemKey{m.mode, file})
		return
	}
	if isBinaryContent(string(content)) {
		m.mainPane.SetFilename("")
		m.mainPane.SetPlainContent("[binary content]")
		setItem(mainItemKey{m.mode, file})
		return
	}
	m.mainPane.SetFilename(file)
	m.mainPane.SetPlainContent(string(content))
	setItem(mainItemKey{m.mode, file})
}

// updateFilesModeContent populates the main pane for files mode in a git repo.
func (m *Model) updateFilesModeContent(setItem itemSetter) {
	file := m.sidebar.SelectedItem()
	if file == "" {
		m.mainPane.SetFilename("")
		m.mainPane.SetPlainContent("")
		m.mainPane.ClearDiffAnnotations()
		m.mainPane.ClearDiffHunks()
		m.mainPane.SetTitle("", "")
		setItem(mainItemKey{m.mode, ""})
		return
	}
	if m.sidebar.SelectedIsDir() {
		return // preserve current main panel content (and lastMainItem)
	}
	content, err := m.git.FileContent(file)
	if err != nil {
		m.mainPane.SetFilename("")
		m.mainPane.SetPlainContent(fmt.Sprintf("Error: %v", err))
		m.mainPane.ClearDiffAnnotations()
		m.mainPane.ClearDiffHunks()
		m.mainPane.SetTitle(file, "error")
		setItem(mainItemKey{m.mode, file})
		return
	}
	if isBinaryContent(content) {
		m.mainPane.SetFilename("")
		m.mainPane.SetPlainContent("[binary content]")
		m.mainPane.ClearDiffAnnotations()
		m.mainPane.ClearDiffHunks()
		m.mainPane.SetNoHunkRight(m.fileContextRight(file, true))
		m.mainPane.SetDiffPrefix("")
		m.mainPane.SetTitleWithHunks(m.fileTitleLeft(file))
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
		diff, _ = m.git.FileDiffUncommitted(file)
	} else if m.isCommittedFile(file) {
		diff, _ = m.git.FileDiffCommitted(m.scope.OldBase(), file)
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
		m.mainPane.SetDiffAnnotations(annotations)
		m.mainPane.SetDiffHunks(parseDiffHunks(diff))
		m.mainPane.SetNoHunkRight("")
		m.mainPane.SetDiffPrefix(m.fileDiffPrefix(file))
	} else {
		m.mainPane.ClearDiffAnnotations()
		m.mainPane.ClearDiffHunks()
		m.mainPane.SetNoHunkRight(m.fileContextRight(file, false))
		m.mainPane.SetDiffPrefix("")
	}
	m.mainPane.SetFilename(file)
	m.mainPane.SetPlainContent(content)
	m.mainPane.SetTitleWithHunks(m.fileTitleLeft(file))
	setItem(mainItemKey{m.mode, file})
}

// updateCommitsModeContent populates the main pane for commits mode.
func (m *Model) updateCommitsModeContent(setItem itemSetter) {
	selected := m.sidebar.SelectedItem()
	if selected == "" {
		m.mainPane.SetContent("")
		m.mainPane.SetTitle("", "")
		setItem(mainItemKey{m.mode, ""})
		return
	}
	if strings.HasPrefix(selected, "load more") {
		m.mainPane.SetPlainContent("Loading more commits...")
		m.mainPane.SetTitle(selected, "")
		setItem(mainItemKey{m.mode, selected})
		return
	}
	if selected == "new changes" || selected == "staged changes" {
		diff, _ := m.git.FileDiffUncommitted("")
		m.mainPane.SetContent(diff)
		m.mainPane.SetTitle(selected, shortstatFromDiff(diff))
		setItem(mainItemKey{m.mode, selected})
		return
	}
	commitIdx := m.commitIndexFromSidebarItem(selected)
	if commitIdx < 0 || commitIdx >= len(m.commits) {
		m.mainPane.SetContent("")
		m.mainPane.SetTitle("", "")
		setItem(mainItemKey{m.mode, ""})
		return
	}
	commit := m.commits[commitIdx]
	patch, err := m.git.CommitPatch(commit.SHA)
	titleLeft := commitTitleLeft(commit)
	titleRight := formatAuthorAndTime(commit.Author, commit.AuthorDate)
	if err != nil {
		m.mainPane.SetContent(fmt.Sprintf("Error: %v", err))
		m.mainPane.SetTitle(titleLeft, titleRight)
		setItem(mainItemKey{m.mode, selected})
		return
	}
	if isBinaryContent(patch) {
		m.mainPane.SetPlainContent("[binary content]")
		m.mainPane.SetTitle(titleLeft, titleRight)
		setItem(mainItemKey{m.mode, selected})
		return
	}
	m.mainPane.SetContent(patch)
	m.mainPane.SetTitle(titleLeft, titleRight)
	setItem(mainItemKey{m.mode, selected})
}

// updatePRModeContent populates the main pane for PR mode.
func (m *Model) updatePRModeContent(setItem itemSetter) {
	selected := m.sidebar.SelectedItem()
	switch {
	case selected == "Description":
		m.mainPane.SetPlainContent(m.renderPRDescription())
		m.mainPane.SetTitle("Description", "")
	default:
		if matched, i := matchNumberedItem(selected, m.prComments, func(j int, c gitpkg.PRComment) string {
			return fmt.Sprintf("#%d @%s", len(m.prComments)-j, c.Author)
		}); matched {
			c := m.prComments[i]
			m.mainPane.SetPlainContent(buildCommentContent(c, m.mainPane.width))
			m.mainPane.SetTitle(
				fmt.Sprintf("comment #%d", len(m.prComments)-i),
				formatAuthorAndTime(c.Author, c.CreatedAt),
			)
		} else if matched, i := matchNumberedItem(selected, m.prReviews, func(j int, r gitpkg.PRReview) string {
			return fmt.Sprintf("#%d %s@%s", len(m.prReviews)-j, reviewStateEmoji(r.State), r.Author)
		}); matched {
			r := m.prReviews[i]
			m.mainPane.SetPlainContent(buildReviewContent(r, m.mainPane.width))
			m.mainPane.SetTitle(
				fmt.Sprintf("review #%d · %s", len(m.prReviews)-i, reviewStateLabel(r.State)),
				formatAuthorAndTime(r.Author, r.SubmittedAt),
			)
		} else {
			m.applyCICheckContent(selected)
		}
	}
	setItem(mainItemKey{m.mode, selected})
}

// applyCICheckContent matches selected against m.ciChecks and updates the
// main pane. Falls back to setting just the title if no check matches.
func (m *Model) applyCICheckContent(selected string) {
	for _, check := range m.ciChecks {
		if !strings.Contains(selected, check.Name) {
			continue
		}
		status := check.Bucket
		if status == "" {
			status = check.State
		}
		extra, _ := m.rwxFetcher.Lookup(check)
		m.mainPane.SetPlainContent(buildCIContent(check, status, extra))
		m.mainPane.SetTitle("CI · "+check.Name, status)
		return
	}
	m.mainPane.SetTitle(selected, "")
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
	body := c.Body
	if rendered, err := renderMarkdown(body, width); err == nil {
		body = rendered
	}
	return fmt.Sprintf("%s\n\n%s", header, body)
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
		body := r.Body
		if rendered, err := renderMarkdown(body, width); err == nil {
			body = rendered
		}
		content += "\n\n" + body
	}
	for _, c := range r.Comments {
		content += fmt.Sprintf("\n\n--- %s:%d ---\n%s", c.Path, c.Line, c.Body)
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
