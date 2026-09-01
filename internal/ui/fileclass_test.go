package ui

import (
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// Property: changeBadgeFor returns the symbol matching the file's Class.
// Precedence is encoded in Class (renamed > deleted > added > modified),
// established at construction time by buildChangedFiles.
func TestChangeBadgePriority(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		file := rapid.SampledFrom([]string{"a.go", "b.go", "c.go", "x.go"}).Draw(t, "file")
		class := gitpkg.Class(rapid.IntRange(0, 3).Draw(t, "class"))

		cf := gitpkg.NewChangedFiles()
		entry := gitpkg.ChangedFile{Path: file, Section: gitpkg.SectionUncommitted, Class: class}
		if class == gitpkg.ClassRenamed {
			entry.OldPath = "old_" + file
		}
		cf.Add(entry)

		want := map[gitpkg.Class]string{
			gitpkg.ClassRenamed:  "[→]",
			gitpkg.ClassDeleted:  "[-]",
			gitpkg.ClassAdded:    "[+]",
			gitpkg.ClassModified: "[±]",
		}[class]

		if got := changeBadgeFor(file, cf); got != want {
			t.Fatalf("class=%v: got=%q want=%q", class, got, want)
		}
	})
}

// A file absent from the changed set gets the empty badge.
func TestChangeBadgeAbsent(t *testing.T) {
	cf := gitpkg.NewChangedFiles()
	if got := changeBadgeFor("nonesuch.go", cf); got != "" {
		t.Errorf("got %q want \"\"", got)
	}
}

// applyChangeBadges leaves non-changed-section items alone.
func TestApplyChangeBadgesSkipsAllFiles(t *testing.T) {
	cf := gitpkg.NewChangedFiles()
	cf.Add(gitpkg.ChangedFile{Path: "foo.go", Section: gitpkg.SectionCommitted, Class: gitpkg.ClassModified})
	items := []sidebarItem{
		{label: "All Files (5)", kind: itemHeader},
		{label: "foo.go", kind: itemNormal, filePath: "foo.go"},
	}
	out := applyChangeBadges(items, cf)
	if out[1].suffix != "" {
		t.Errorf("All Files entry got badge %q, want none", out[1].suffix)
	}
}

// applyChangeBadges sets the suffix on leaf items in a changed section.
func TestApplyChangeBadgesSetsChanged(t *testing.T) {
	cf := gitpkg.NewChangedFiles()
	cf.Add(gitpkg.ChangedFile{Path: "foo.go", Section: gitpkg.SectionCommitted, Class: gitpkg.ClassModified})
	items := []sidebarItem{
		{label: "Committed (1)", kind: itemHeader},
		{label: "foo.go", kind: itemNormal, filePath: "foo.go"},
	}
	out := applyChangeBadges(items, cf)
	if out[1].suffix != "[±]" {
		t.Errorf("Committed entry got %q, want [±]", out[1].suffix)
	}
}

// Rename takes precedence over the other badges, even when the file is also
// in the uncommitted/staged/committed buckets (which is normal — the new path
// lives in one of those).
func TestApplyChangeBadgesRenameWins(t *testing.T) {
	cf := gitpkg.NewChangedFiles()
	cf.Add(gitpkg.ChangedFile{
		Path: "renamed.go", Section: gitpkg.SectionUncommitted, Class: gitpkg.ClassRenamed,
		OldPath: "original.go", PureRename: true,
	})
	items := []sidebarItem{
		{label: "New Changes (1)", kind: itemHeader},
		{label: "renamed.go", kind: itemNormal, filePath: "renamed.go"},
	}
	out := applyChangeBadges(items, cf)
	if out[1].suffix != "[→]" {
		t.Errorf("rename entry got %q, want [→]", out[1].suffix)
	}
}
