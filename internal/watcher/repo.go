package watcher

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hazeledmands/prwatch/internal/command"
)

// TrackedFileBudget caps how many tracked files a repo may hold before
// subdirectory watching is dropped wholesale.
//
// The budget counts files rather than directories because files are roughly
// what the watch costs. fsnotify is not recursive on any platform, but its
// backends price a directory watch very differently: inotify takes one watch
// per directory, while kqueue — the macOS backend — calls ReadDir on each
// watched directory and opens a descriptor for every entry it finds
// (backend_kqueue.go's watchDirectoryFiles). So on the platform prwatch is
// mostly used, watching every directory that holds tracked files costs on the
// order of one descriptor per tracked file.
//
// The headroom that makes 2048 safe is not the inherited soft limit, which on
// macOS is small by default (256 in a stock configuration). It is that Go
// raises RLIMIT_NOFILE to the hard limit during startup (Go 1.19 and later),
// which measures 122880 here. 2048 sits far enough under that to leave room
// for everything else the process opens, while covering essentially every
// single-project repo.
//
// Residual exposure: the budget counts *tracked* files, while kqueue charges
// for every directory entry — ignored files included, since it reads the
// directory itself and knows nothing about gitignore. A repo under the budget
// whose tracked directories are also full of ignored build output therefore
// costs more descriptors than the count suggests. Past the budget,
// subdirectory events fall back to the poll.
const TrackedFileBudget = 2048

// RepoDirs returns the directories worth watching for a working tree at dir,
// which may be the tree root or any path inside it.
//
// It exists because "the repo's git state" is not one fixed place. In an
// ordinary clone the tree root holds `.git/`, and watching `.git` plus
// `.git/refs/heads` catches HEAD moves and commits. In a *linked* worktree
// `.git` is a file holding a `gitdir:` pointer, and the state is split in two:
// the worktree's HEAD and index live in a private gitdir under the main
// repository's `.git/worktrees/<name>/`, while its branch ref lives in the
// common dir's `refs/heads`. Code that tests `.git` with IsDir and gives up
// therefore watches no refs at all in exactly the case a developer is most
// likely to be in.
//
// Rather than parse the pointer file and its commondir indirection by hand,
// this asks git, which knows the layout including whatever future variations
// of it exist. The cost is one subprocess, and watcher setup runs once at
// startup.
//
// Only existing directories are returned, so every entry is addable.
func RepoDirs(dir string, factory command.Factory) []string {
	var dirs []string

	// The tree itself, always — prwatch is also pointed at directories that
	// are not repos at all.
	dirs = append(dirs, dir)

	gitDir, commonDir, topLevel, ok := gitLocations(dir, factory)
	if !ok {
		return existingDirs(dirs)
	}

	// The tree root, which is what git considers the working tree, may differ
	// from the directory prwatch was pointed at.
	dirs = append(dirs, topLevel)

	// The private gitdir carries this worktree's HEAD; the common dir carries
	// packed-refs and the shared config. For an ordinary clone the two are the
	// same path and the dedup below collapses them.
	dirs = append(dirs, gitDir, commonDir)

	// Loose branch refs — the file a commit rewrites — live in the common
	// dir's refs/heads for every worktree.
	dirs = append(dirs, filepath.Join(commonDir, "refs", "heads"))
	if gitDir != commonDir {
		// A linked worktree also keeps per-worktree refs of its own.
		dirs = append(dirs, filepath.Join(gitDir, "refs"))
	}

	dirs = append(dirs, trackedSubdirs(topLevel, factory)...)

	return existingDirs(dirs)
}

// gitLocations resolves the git-dir, common-dir, and tree root for dir. The
// paths git reports may be relative to the directory the command ran in, so
// each is resolved back to an absolute path.
func gitLocations(dir string, factory command.Factory) (gitDir, commonDir, topLevel string, ok bool) {
	out, err := runGit(dir, factory, "rev-parse", "--git-dir", "--git-common-dir", "--show-toplevel")
	if err != nil {
		return "", "", "", false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		return "", "", "", false
	}
	abs := func(p string) string {
		p = strings.TrimSpace(p)
		if p == "" {
			return ""
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return filepath.Clean(p)
	}
	gitDir, commonDir, topLevel = abs(lines[0]), abs(lines[1]), abs(lines[2])
	if gitDir == "" || commonDir == "" || topLevel == "" {
		return "", "", "", false
	}
	return gitDir, commonDir, topLevel, true
}

// trackedSubdirs returns every directory below root that holds a tracked file,
// or nil if the repo carries more tracked files than TrackedFileBudget.
//
// Tracked files, not a filesystem walk: git already knows which paths matter
// and excludes ignored ones, so this never descends into node_modules or a
// build directory. On a large monorepo that alone is a ~40x reduction, but
// still far past what the descriptor budget allows — hence the cap, and hence
// all-or-nothing rather than a partial frontier, so the coverage a given repo
// gets is something a reader can state in one sentence.
func trackedSubdirs(root string, factory command.Factory) []string {
	out, err := runGit(root, factory, "ls-files", "-z")
	if err != nil {
		return nil
	}
	var seen map[string]bool
	var dirs []string
	count := 0
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		count++
		if count > TrackedFileBudget {
			return nil
		}
		rel := filepath.Dir(p)
		if rel == "." {
			continue // the root is watched already
		}
		if seen == nil {
			seen = map[string]bool{}
		}
		// Every ancestor needs its own watch: a non-recursive backend gives no
		// events for a grandchild directory.
		for rel != "." && rel != string(filepath.Separator) {
			if seen[rel] {
				break
			}
			seen[rel] = true
			dirs = append(dirs, filepath.Join(root, rel))
			rel = filepath.Dir(rel)
		}
	}
	return dirs
}

// existingDirs deduplicates paths and keeps only those that are directories
// right now, so every returned path can actually be added to a watcher.
//
// Deduplication is by resolved path. The same directory routinely arrives
// under two names — on macOS git reports /private/var/... where the caller
// passed /var/..., since /var is a symlink — and a name-based dedup would then
// watch it twice, doubling its descriptor cost for nothing.
func existingDirs(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, d := range in {
		if d == "" {
			continue
		}
		real, err := filepath.EvalSymlinks(d)
		if err != nil {
			continue
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		if info, err := os.Stat(real); err == nil && info.IsDir() {
			out = append(out, real)
		}
	}
	slices.Sort(out)
	return out
}

// runGit runs a read-only git command in dir and returns its stdout verbatim
// (untrimmed, so NUL-delimited output survives).
func runGit(dir string, factory command.Factory, args ...string) (string, error) {
	cmd := factory("git", append([]string{"--no-optional-locks"}, args...)...)
	cmd.SetDir(dir)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// WatchRepo watches every location RepoDirs finds for dir, through a single
// watcher and a single debounce timer.
func WatchRepo(dir string, factory command.Factory, onRefresh func()) (*Watcher, error) {
	return NewMulti(RepoDirs(dir, factory), onRefresh)
}
