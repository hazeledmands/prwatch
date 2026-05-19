package ui

import (
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// Property: changeBadgeFor returns the correct symbol based on which set
// contains the file. Priority: renamed > deleted > added > committed/uncommitted/staged.
func TestChangeBadgePriority(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		file := rapid.SampledFrom([]string{"a.go", "b.go", "c.go", "x.go"}).Draw(t, "file")
		inRen := rapid.Bool().Draw(t, "inRen")
		inDel := rapid.Bool().Draw(t, "inDel")
		inAdd := rapid.Bool().Draw(t, "inAdd")
		inCom := rapid.Bool().Draw(t, "inCom")
		inUnc := rapid.Bool().Draw(t, "inUnc")
		inSta := rapid.Bool().Draw(t, "inSta")

		set := func(b bool) []string {
			if b {
				return []string{file}
			}
			return nil
		}
		var renames []gitpkg.Rename
		if inRen {
			renames = []gitpkg.Rename{{Old: "old_" + file, New: file}}
		}
		got := changeBadgeFor(file, set(inDel), set(inAdd), set(inCom), set(inUnc), set(inSta), renames)

		var want string
		switch {
		case inRen:
			want = "[→]"
		case inDel:
			want = "[-]"
		case inAdd:
			want = "[+]"
		case inCom || inUnc || inSta:
			want = "[±]"
		default:
			want = ""
		}
		if got != want {
			t.Fatalf("inputs=(ren=%v del=%v add=%v com=%v unc=%v sta=%v): got=%q want=%q", inRen, inDel, inAdd, inCom, inUnc, inSta, got, want)
		}
	})
}

// Property: a file in none of the sets gets the empty badge.
func TestChangeBadgeAbsent(t *testing.T) {
	if got := changeBadgeFor("nonesuch.go", nil, nil, nil, nil, nil, nil); got != "" {
		t.Errorf("got %q want \"\"", got)
	}
}

// Property: applyChangeBadges leaves non-changed-section items alone.
func TestApplyChangeBadgesSkipsAllFiles(t *testing.T) {
	items := []sidebarItem{
		{label: "All Files (5)", kind: itemHeader},
		{label: "foo.go", kind: itemNormal, filePath: "foo.go"},
	}
	out := applyChangeBadges(items, nil, nil, []string{"foo.go"}, nil, nil, nil)
	if out[1].suffix != "" {
		t.Errorf("All Files entry got badge %q, want none", out[1].suffix)
	}
}

// Property: applyChangeBadges sets the suffix on leaf items in a changed section.
func TestApplyChangeBadgesSetsChanged(t *testing.T) {
	items := []sidebarItem{
		{label: "Committed (1)", kind: itemHeader},
		{label: "foo.go", kind: itemNormal, filePath: "foo.go"},
	}
	out := applyChangeBadges(items, nil, nil, []string{"foo.go"}, nil, nil, nil)
	if out[1].suffix != "[±]" {
		t.Errorf("Committed entry got %q, want [±]", out[1].suffix)
	}
}

// Rename takes precedence over the other badges, even when the file is also
// in the uncommitted/staged/committed buckets (which is normal — the new path
// lives in one of those).
func TestApplyChangeBadgesRenameWins(t *testing.T) {
	items := []sidebarItem{
		{label: "New Changes (1)", kind: itemHeader},
		{label: "renamed.go", kind: itemNormal, filePath: "renamed.go"},
	}
	out := applyChangeBadges(items, nil, []string{"renamed.go"}, nil, []string{"renamed.go"}, nil,
		[]gitpkg.Rename{{Old: "original.go", New: "renamed.go"}})
	if out[1].suffix != "[→]" {
		t.Errorf("rename entry got %q, want [→]", out[1].suffix)
	}
}
