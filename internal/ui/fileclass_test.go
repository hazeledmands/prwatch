package ui

import (
	"testing"

	"pgregory.net/rapid"
)

// Property: changeBadgeFor returns the correct symbol based on which set
// contains the file. Priority: deleted > added > committed/uncommitted/staged.
func TestChangeBadgePriority(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		file := rapid.SampledFrom([]string{"a.go", "b.go", "c.go", "x.go"}).Draw(t, "file")
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
		got := changeBadgeFor(file, set(inDel), set(inAdd), set(inCom), set(inUnc), set(inSta))

		var want string
		switch {
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
			t.Fatalf("inputs=(del=%v add=%v com=%v unc=%v sta=%v): got=%q want=%q", inDel, inAdd, inCom, inUnc, inSta, got, want)
		}
	})
}

// Property: a file in none of the sets gets the empty badge.
func TestChangeBadgeAbsent(t *testing.T) {
	if got := changeBadgeFor("nonesuch.go", nil, nil, nil, nil, nil); got != "" {
		t.Errorf("got %q want \"\"", got)
	}
}

// Property: applyChangeBadges leaves non-changed-section items alone.
func TestApplyChangeBadgesSkipsAllFiles(t *testing.T) {
	items := []sidebarItem{
		{label: "All Files (5)", kind: itemHeader},
		{label: "foo.go", kind: itemNormal, filePath: "foo.go"},
	}
	out := applyChangeBadges(items, nil, nil, []string{"foo.go"}, nil, nil)
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
	out := applyChangeBadges(items, nil, nil, []string{"foo.go"}, nil, nil)
	if out[1].suffix != "[±]" {
		t.Errorf("Committed entry got %q, want [±]", out[1].suffix)
	}
}
