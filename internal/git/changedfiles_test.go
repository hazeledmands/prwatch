package git

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// Add then Get round-trips: the value returned is the value added (same path,
// same fields).
func TestChangedFiles_AddGetRoundtrip(t *testing.T) {
	c := NewChangedFiles()
	f := ChangedFile{Path: "a.go", Section: SectionStaged, Class: ClassRenamed, OldPath: "old.go", PureRename: true}
	c.Add(f)
	got, ok := c.Get("a.go")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if got != f {
		t.Errorf("Get round-trip: got %+v, want %+v", got, f)
	}
}

// Add of the same path replaces the prior entry rather than duplicating it.
func TestChangedFiles_AddReplaces(t *testing.T) {
	c := NewChangedFiles()
	c.Add(ChangedFile{Path: "a.go", Section: SectionCommitted, Class: ClassAdded})
	c.Add(ChangedFile{Path: "a.go", Section: SectionUncommitted, Class: ClassModified})
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
	got, _ := c.Get("a.go")
	if got.Section != SectionUncommitted || got.Class != ClassModified {
		t.Errorf("after replace: got %+v, want Section=Uncommitted Class=Modified", got)
	}
}

// All returns every added file once, alphabetic by path.
func TestChangedFiles_AllOrder(t *testing.T) {
	c := NewChangedFiles()
	c.Add(ChangedFile{Path: "c.go"})
	c.Add(ChangedFile{Path: "a.go"})
	c.Add(ChangedFile{Path: "b.go"})
	all := c.All()
	if len(all) != 3 {
		t.Fatalf("All len = %d, want 3", len(all))
	}
	want := []string{"a.go", "b.go", "c.go"}
	for i, f := range all {
		if f.Path != want[i] {
			t.Errorf("All[%d].Path = %q, want %q", i, f.Path, want[i])
		}
	}
}

// InSection returns only files in that section, alphabetic.
func TestChangedFiles_InSectionFilter(t *testing.T) {
	c := NewChangedFiles()
	c.Add(ChangedFile{Path: "a.go", Section: SectionUncommitted})
	c.Add(ChangedFile{Path: "b.go", Section: SectionStaged})
	c.Add(ChangedFile{Path: "c.go", Section: SectionUncommitted})
	c.Add(ChangedFile{Path: "d.go", Section: SectionCommitted})
	unc := c.InSection(SectionUncommitted)
	if len(unc) != 2 || unc[0].Path != "a.go" || unc[1].Path != "c.go" {
		t.Errorf("Uncommitted section: got %+v", unc)
	}
	if len(c.InSection(SectionStaged)) != 1 {
		t.Errorf("Staged section len = %d, want 1", len(c.InSection(SectionStaged)))
	}
}

// Renames returns every renamed file and no others.
func TestChangedFiles_Renames(t *testing.T) {
	c := NewChangedFiles()
	c.Add(ChangedFile{Path: "a.go", Class: ClassModified})
	c.Add(ChangedFile{Path: "b.go", Class: ClassRenamed, OldPath: "old_b.go", PureRename: true})
	c.Add(ChangedFile{Path: "c.go", Class: ClassAdded})
	c.Add(ChangedFile{Path: "d.go", Class: ClassRenamed, OldPath: "old_d.go"})
	rens := c.Renames()
	if len(rens) != 2 {
		t.Fatalf("Renames len = %d, want 2", len(rens))
	}
	if rens[0].Path != "b.go" || rens[1].Path != "d.go" {
		t.Errorf("Renames order: got %v, want [b.go d.go]", []string{rens[0].Path, rens[1].Path})
	}
}

func TestChangedFiles_HasRenameOldPath(t *testing.T) {
	c := NewChangedFiles()
	c.Add(ChangedFile{Path: "new.go", Class: ClassRenamed, OldPath: "old.go"})
	c.Add(ChangedFile{Path: "other.go", Class: ClassModified, OldPath: "stale"}) // OldPath only honored when ClassRenamed
	if !c.HasRenameOldPath("old.go") {
		t.Error("HasRenameOldPath(old.go) = false, want true")
	}
	if c.HasRenameOldPath("stale") {
		t.Error("HasRenameOldPath(stale) = true; OldPath on non-rename should not match")
	}
	if c.HasRenameOldPath("missing.go") {
		t.Error("HasRenameOldPath(missing.go) = true, want false")
	}
}

// Property: All() returns one entry per unique input path, in sorted order.
func TestProperty_ChangedFiles_AllUniqueSorted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 50).Draw(t, "n")
		c := NewChangedFiles()
		seen := make(map[string]bool)
		for i := 0; i < n; i++ {
			path := fmt.Sprintf("f%d.go", rapid.IntRange(0, n).Draw(t, fmt.Sprintf("p%d", i)))
			c.Add(ChangedFile{Path: path})
			seen[path] = true
		}
		all := c.All()
		if len(all) != len(seen) {
			t.Fatalf("All len = %d, unique paths = %d", len(all), len(seen))
		}
		for i := 1; i < len(all); i++ {
			if all[i-1].Path >= all[i].Path {
				t.Fatalf("All not sorted: [%d]=%q >= [%d]=%q", i-1, all[i-1].Path, i, all[i].Path)
			}
		}
	})
}

// Property: InSection partitions All — every file appears in exactly one section's list.
func TestProperty_ChangedFiles_SectionPartition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "n")
		c := NewChangedFiles()
		for i := 0; i < n; i++ {
			s := Section(rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("s%d", i)))
			c.Add(ChangedFile{Path: fmt.Sprintf("f%d.go", i), Section: s})
		}
		all := c.All()
		var combined []ChangedFile
		combined = append(combined, c.InSection(SectionUncommitted)...)
		combined = append(combined, c.InSection(SectionStaged)...)
		combined = append(combined, c.InSection(SectionCommitted)...)
		if len(combined) != len(all) {
			t.Fatalf("section partition: union has %d, All has %d", len(combined), len(all))
		}
		seen := make(map[string]int)
		for _, s := range []Section{SectionUncommitted, SectionStaged, SectionCommitted} {
			for _, f := range c.InSection(s) {
				if f.Section != s {
					t.Fatalf("InSection(%d) returned file with Section=%d", s, f.Section)
				}
				seen[f.Path]++
			}
		}
		for p, n := range seen {
			if n != 1 {
				t.Fatalf("path %q appeared in %d sections; partition violated", p, n)
			}
		}
	})
}

// Property: every file produced by buildChangedFiles belongs to exactly one
// section, the section assignment is consistent with the input slices, and
// rename targets are classified as ClassRenamed with non-empty OldPath.
func TestProperty_BuildChangedFiles_Invariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Disjoint section buckets — committed/staged/uncommitted use
		// non-overlapping name prefixes the way git.ChangedFiles produces
		// them in practice.
		mkPaths := func(prefix string, count int) []string {
			out := make([]string, count)
			for i := range out {
				out[i] = fmt.Sprintf("%s%d.go", prefix, i)
			}
			return out
		}
		nC := rapid.IntRange(0, 6).Draw(t, "nCommitted")
		nS := rapid.IntRange(0, 6).Draw(t, "nStaged")
		nU := rapid.IntRange(0, 6).Draw(t, "nUncommitted")
		committed := mkPaths("c", nC)
		staged := mkPaths("s", nS)
		uncommitted := mkPaths("u", nU)
		// Some classified as deleted (only meaningful for committed in current model)
		var deleted []string
		for _, p := range committed {
			if rapid.Bool().Draw(t, "del_"+p) {
				deleted = append(deleted, p)
			}
		}
		// Some classified as added (any section)
		var added []string
		all := append(append([]string(nil), committed...), staged...)
		all = append(all, uncommitted...)
		for _, p := range all {
			if rapid.Bool().Draw(t, "add_"+p) {
				added = append(added, p)
			}
		}
		// Some renames (use ONE entry in each section as the "new" path of a
		// rename; OldPath uses a prefix that won't collide with anything).
		var renamed []Rename
		for _, p := range all {
			if rapid.Float64Range(0, 1).Draw(t, "ren_"+p) < 0.3 {
				renamed = append(renamed, Rename{Old: "OLD_" + p, New: p, Pure: rapid.Bool().Draw(t, "pure_"+p)})
			}
		}

		cf := buildChangedFiles(committed, uncommitted, staged, deleted, added, renamed)

		// Every input path appears exactly once with the right section.
		pathSection := make(map[string]Section)
		for _, p := range committed {
			pathSection[p] = SectionCommitted
		}
		for _, p := range staged {
			pathSection[p] = SectionStaged
		}
		for _, p := range uncommitted {
			pathSection[p] = SectionUncommitted
		}
		if cf.Len() != len(pathSection) {
			t.Fatalf("Len = %d, want %d", cf.Len(), len(pathSection))
		}
		for p, want := range pathSection {
			got, ok := cf.Get(p)
			if !ok {
				t.Fatalf("missing %q", p)
			}
			if got.Section != want {
				t.Errorf("%q Section = %v, want %v", p, got.Section, want)
			}
		}
		// ClassRenamed iff there's an entry in renamed with this New path.
		renameByNew := make(map[string]Rename)
		for _, r := range renamed {
			renameByNew[r.New] = r
		}
		for _, f := range cf.All() {
			r, isRen := renameByNew[f.Path]
			if isRen {
				if f.Class != ClassRenamed {
					t.Errorf("%q should be ClassRenamed, got %v", f.Path, f.Class)
				}
				if f.OldPath != r.Old {
					t.Errorf("%q OldPath = %q, want %q", f.Path, f.OldPath, r.Old)
				}
				if f.PureRename != r.Pure {
					t.Errorf("%q PureRename = %v, want %v", f.Path, f.PureRename, r.Pure)
				}
			} else if f.Class == ClassRenamed {
				t.Errorf("%q has Class=Renamed but no matching rename in input", f.Path)
			}
		}
	})
}
