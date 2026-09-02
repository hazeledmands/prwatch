package git

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// The three commits-mode pseudo-entries each have their own diff source
// (PROMPT.md, "commits mode"):
//
//	staged      → the staged diff (`git diff --cached`)
//	new changes → the working-tree diff against the index, plus each untracked
//	              file rendered as a new-file diff
//	untracked   → each untracked file's contents as a new-file diff
//
// They used to share one `git diff HEAD`, which conflated staged with unstaged
// and showed no untracked content at all.

// StagedDiff returns the diff of the index against HEAD — what `git commit`
// would record right now.
func (g *Git) StagedDiff() (string, error) {
	return g.runDiff("diff", "--cached")
}

// UnstagedDiff returns the diff of the working tree against the index. It
// covers tracked files only; untracked content comes from UntrackedDiff.
func (g *Git) UnstagedDiff() (string, error) {
	return g.runDiff("diff")
}

// noIndexDiff renders one path as a new-file diff against /dev/null.
//
// `git diff --no-index` signals "differences found" with exit status 1, which
// is the normal outcome here, so the exit code carries no usable error
// information and is ignored. What separates success from failure is the
// output: a readable file produces stdout and no stderr; an unreadable one
// (deleted between listing and diffing, or permission-denied) produces exit 1
// with empty stdout and a message on stderr — identical exit code, so stderr
// is the only signal. Callers get ("", err) for that case rather than an empty
// string that would silently drop the file from the body.
func (g *Git) noIndexDiff(path string) (string, error) {
	cmd := g.cmdFactory("git", "-c", "core.quotePath=false", "diff", "--no-index", "--", "/dev/null", path)
	cmd.SetDir(g.dir)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	cmd.Run() // exit 1 means "differences found", which is expected
	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git diff --no-index %s: %s", path, msg)
		}
	}
	return out, nil
}

// UntrackedFiles returns the repo-relative paths of untracked, non-ignored
// files, sorted. Goes through runZ: these are paths, so core.quotePath would
// otherwise mangle anything non-ASCII.
func (g *Git) UntrackedFiles() ([]string, error) {
	files, err := g.runZ("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// UntrackedDiff renders every untracked file as a new-file diff against
// /dev/null, concatenated in path order. Returns "" when nothing is untracked.
//
// Binary files need no special casing: git emits its own
// "Binary files /dev/null and b/<path> differ" line and never the content,
// which is what PROMPT.md requires.
//
// A file that cannot be read gets a visible placeholder rather than being
// dropped. The listing and the diffs are separate git invocations, so a file
// can vanish in between; silently omitting it would leave it counted in the
// sidebar's "New Changes (N files)" header while being absent from the body,
// which reads as a rendering bug rather than as the race it is.
func (g *Git) UntrackedDiff() (string, error) {
	files, err := g.UntrackedFiles()
	if err != nil {
		return "", err
	}
	var parts []string
	for _, f := range files {
		out, err := g.noIndexDiff(f)
		switch {
		case err != nil:
			parts = append(parts, "[could not read "+f+"]")
		case out != "":
			parts = append(parts, out)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// NewChangesDiff is the body of the "new changes" pseudo-entry: the
// working-tree diff against the index followed by each untracked file as a
// new-file diff. PROMPT.md groups untracked and unstaged changes under that one
// sidebar entry, so both belong here.
func (g *Git) NewChangesDiff() (string, error) {
	unstaged, err := g.UnstagedDiff()
	if err != nil {
		return "", err
	}
	untracked, uerr := g.UntrackedDiff()
	if uerr != nil {
		// A listing failure must not pass for "no untracked files": the
		// sidebar header counts them, so a silently unstaged-only body would
		// under-report without saying so.
		if unstaged == "" {
			// Nothing to show at all — propagate, so the caller renders the
			// error instead of the "no new changes" empty state.
			return "", uerr
		}
		// There is real unstaged content worth showing; keep it and say what
		// is missing. The trailer is not a diff line, so diffScanner
		// classifies it as rowMeta and it renders as plain text.
		return unstaged + "\n[error listing untracked files: " + uerr.Error() + "]", nil
	}
	switch {
	case unstaged == "":
		return untracked, nil
	case untracked == "":
		return unstaged, nil
	}
	return unstaged + "\n" + untracked, nil
}
