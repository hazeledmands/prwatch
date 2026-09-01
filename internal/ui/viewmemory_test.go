package ui

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// genSidebarItems produces a mixed list of headers, separators, tree-indented
// file items (label != filePath) and prefixed non-file items (label != prefix
// +label) — i.e. exactly the shapes where "compare the label" and "compare
// the identity" disagree.
func genSidebarItems(t *rapid.T, tag string) []sidebarItem {
	n := rapid.IntRange(1, 12).Draw(t, tag+"_n")
	items := make([]sidebarItem, 0, n)
	for i := range n {
		switch rapid.SampledFrom([]string{"file", "file", "pr", "header", "sep"}).Draw(t, fmt.Sprintf("%s_kind%d", tag, i)) {
		case "file":
			depth := rapid.IntRange(0, 3).Draw(t, fmt.Sprintf("%s_depth%d", tag, i))
			base := fmt.Sprintf("f%d.go", i)
			path := base
			for d := range depth {
				path = fmt.Sprintf("d%d/%s", d, path)
			}
			items = append(items, sidebarItem{
				label:    base, // tree label: no directory prefix
				indent:   depth,
				kind:     itemNormal,
				filePath: path,
			})
		case "pr":
			items = append(items, sidebarItem{
				label:  fmt.Sprintf("item %d", i),
				prefix: rapid.SampledFrom([]string{"", "  ", "· ", "✓ "}).Draw(t, fmt.Sprintf("%s_pfx%d", tag, i)),
				kind:   itemNormal,
			})
		case "header":
			items = append(items, sidebarItem{label: fmt.Sprintf("Section %d", i), kind: itemHeader})
		case "sep":
			items = append(items, sidebarItem{kind: itemSeparator})
		}
	}
	return items
}

func selectableIndices(items []sidebarItem) []int {
	var out []int
	for i, it := range items {
		if it.kind.selectable() {
			out = append(out, i)
		}
	}
	return out
}

// Property: SaveSidebar → RestoreSidebar is the identity on the selection,
// even when the item list is shuffled/grown/shrunk in between, as long as the
// item is still present. The saved key and the restore comparison must be the
// same canonical identity — A5 had SaveSidebar storing itemID while
// RestoreSidebar compared item.label, so restore silently fell back to the
// stored raw index.
func TestProperty_ViewMemory_SidebarRestoreByIdentity(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		items := genSidebarItems(t, "items")
		sel := selectableIndices(items)
		if len(sel) == 0 {
			return
		}
		sb := newSidebar()
		sb.SetItems(items)
		sb.height = 10
		idx := rapid.SampledFrom(sel).Draw(t, "selected")
		sb.SelectIndex(idx)
		wantID := sb.SelectedItem()

		vm := newViewMemory()
		vm.SaveSidebar(FilesMode, sb, MainFocus)

		// Between save and restore the list is rebuilt: prepend some
		// unrelated items so every index shifts. Identity must still win.
		shift := rapid.IntRange(0, 5).Draw(t, "shift")
		rebuilt := make([]sidebarItem, 0, len(items)+shift)
		for i := range shift {
			rebuilt = append(rebuilt, sidebarItem{label: fmt.Sprintf("extra%d", i), kind: itemHeader})
		}
		rebuilt = append(rebuilt, items...)

		sb2 := newSidebar()
		sb2.SetItems(rebuilt)
		sb2.height = 10
		sb2.SelectIndex(selectableIndices(rebuilt)[0])
		vm.RestoreSidebar(FilesMode, sb2, SidebarFocus)

		if got := sb2.SelectedItem(); got != wantID {
			t.Fatalf("restore selected %q, want %q (shift=%d)", got, wantID, shift)
		}
	})
}

// Property: the focus stored by SaveSidebar comes back out of
// RestoreSidebar, and a mode never saved returns the caller's current focus.
func TestProperty_ViewMemory_FocusRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		items := genSidebarItems(t, "items")
		if len(selectableIndices(items)) == 0 {
			return
		}
		sb := newSidebar()
		sb.SetItems(items)
		sb.height = 10

		saved := rapid.SampledFrom([]Focus{SidebarFocus, MainFocus}).Draw(t, "saved")
		current := rapid.SampledFrom([]Focus{SidebarFocus, MainFocus}).Draw(t, "current")

		vm := newViewMemory()
		if got := vm.RestoreSidebar(CommitsMode, sb, current); got != current {
			t.Fatalf("unsaved mode returned focus %v, want current %v", got, current)
		}
		vm.SaveSidebar(CommitsMode, sb, saved)
		if got := vm.RestoreSidebar(CommitsMode, sb, current); got != saved {
			t.Fatalf("restored focus %v, want saved %v", got, saved)
		}
	})
}

// Property: RememberMainScroll / RecallMainScroll round-trips, and an empty
// item key is never recorded.
func TestProperty_ViewMemory_MainScrollRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		vm := newViewMemory()
		mode := Mode(rapid.IntRange(0, 2).Draw(t, "mode"))
		item := rapid.SampledFrom([]string{"", "a.go", "src/b.go", "· comment 1"}).Draw(t, "item")
		line := rapid.IntRange(1, 10000).Draw(t, "line")
		key := mainItemKey{mode: mode, item: item}

		vm.RememberMainScroll(key, line)
		got, ok := vm.RecallMainScroll(key)
		if item == "" {
			if ok {
				t.Fatalf("empty item key was recorded (line=%d)", got)
			}
			return
		}
		if !ok || got != line {
			t.Fatalf("recall = (%d, %v), want (%d, true)", got, ok, line)
		}
	})
}
