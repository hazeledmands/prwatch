package ui

import (
	"strings"
	"testing"
)

func items(labels ...string) []sidebarItem {
	result := make([]sidebarItem, len(labels))
	for i, l := range labels {
		result[i] = sidebarItem{label: l, kind: itemNormal}
	}
	return result
}

func TestSidebar_SelectNext(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("file1.go", "file2.go", "file3.go"))

	if s.SelectedIndex() != 0 {
		t.Errorf("initial selection = %d, want 0", s.SelectedIndex())
	}

	s.SelectNext()
	if s.SelectedIndex() != 1 {
		t.Errorf("after next, selection = %d, want 1", s.SelectedIndex())
	}

	s.SelectNext()
	s.SelectNext() // should clamp at last item
	if s.SelectedIndex() != 2 {
		t.Errorf("after clamping, selection = %d, want 2", s.SelectedIndex())
	}
}

func TestSidebar_SelectPrev(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("file1.go", "file2.go"))

	s.SelectPrev() // should stay at 0
	if s.SelectedIndex() != 0 {
		t.Errorf("selection = %d, want 0", s.SelectedIndex())
	}

	s.SelectNext()
	s.SelectPrev()
	if s.SelectedIndex() != 0 {
		t.Errorf("selection = %d, want 0", s.SelectedIndex())
	}
}

func TestSidebar_SelectedItem(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("a", "b", "c"))

	if s.SelectedItem() != "a" {
		t.Errorf("selected = %q, want %q", s.SelectedItem(), "a")
	}

	s.SelectNext()
	if s.SelectedItem() != "b" {
		t.Errorf("selected = %q, want %q", s.SelectedItem(), "b")
	}
}

func TestSidebar_EmptyItems(t *testing.T) {
	s := newSidebar()
	if s.SelectedItem() != "" {
		t.Errorf("empty sidebar should return empty string, got %q", s.SelectedItem())
	}
}

func TestSidebar_SetItems_ClampsSelection(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("a", "b", "c"))
	s.SelectNext()
	s.SelectNext() // index = 2

	s.SetItems(items("x")) // shrink list
	if s.SelectedIndex() != 0 {
		t.Errorf("selection should clamp to 0, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SkipsSeparators(t *testing.T) {
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{label: "committed.go", kind: itemNormal},
		{label: "", kind: itemSeparator},
		{label: "wip.go", kind: itemDim},
	})

	if s.SelectedIndex() != 0 {
		t.Errorf("initial selection = %d, want 0", s.SelectedIndex())
	}

	s.SelectNext() // should skip separator, land on index 2
	if s.SelectedIndex() != 2 {
		t.Errorf("after next, selection = %d, want 2 (should skip separator)", s.SelectedIndex())
	}
	if s.SelectedItem() != "wip.go" {
		t.Errorf("selected = %q, want %q", s.SelectedItem(), "wip.go")
	}

	s.SelectPrev() // should skip separator, land on index 0
	if s.SelectedIndex() != 0 {
		t.Errorf("after prev, selection = %d, want 0 (should skip separator)", s.SelectedIndex())
	}
}

func TestSidebar_SelectFirst(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("a", "b", "c"))
	s.SelectNext()
	s.SelectNext() // index 2

	s.SelectFirst()
	if s.SelectedIndex() != 0 {
		t.Errorf("SelectFirst: got %d, want 0", s.SelectedIndex())
	}
}

func TestSidebar_SelectFirst_SkipsSeparator(t *testing.T) {
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{kind: itemSeparator},
		{label: "a.go", kind: itemNormal},
		{label: "b.go", kind: itemNormal},
	})
	s.SelectNext() // index 2

	s.SelectFirst()
	if s.SelectedIndex() != 1 {
		t.Errorf("SelectFirst should skip separator, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SelectLast(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("a", "b", "c"))

	s.SelectLast()
	if s.SelectedIndex() != 2 {
		t.Errorf("SelectLast: got %d, want 2", s.SelectedIndex())
	}
}

func TestSidebar_SelectLast_SkipsSeparator(t *testing.T) {
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{label: "a.go", kind: itemNormal},
		{label: "b.go", kind: itemNormal},
		{kind: itemSeparator},
	})

	s.SelectLast()
	if s.SelectedIndex() != 1 {
		t.Errorf("SelectLast should skip separator, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SelectIndex(t *testing.T) {
	s := newSidebar()
	s.SetItems(items("a", "b", "c"))

	s.SelectIndex(2)
	if s.SelectedIndex() != 2 {
		t.Errorf("SelectIndex(2): got %d", s.SelectedIndex())
	}

	// Out of bounds
	s.SelectIndex(10)
	if s.SelectedIndex() != 2 {
		t.Error("out of bounds SelectIndex should not change selection")
	}

	// Negative
	s.SelectIndex(-1)
	if s.SelectedIndex() != 2 {
		t.Error("negative SelectIndex should not change selection")
	}
}

func TestSidebar_SelectIndex_SkipsSeparator(t *testing.T) {
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{label: "a.go", kind: itemNormal},
		{kind: itemSeparator},
		{label: "b.go", kind: itemNormal},
	})

	s.SelectIndex(1) // separator
	if s.SelectedIndex() != 0 {
		t.Errorf("selecting separator should not change selection, got %d", s.SelectedIndex())
	}
}

// Regression: when files are added or removed from the changeset (e.g. a new
// uncommitted file appears, a file gets deleted), the user's view should not
// suddenly jump to a different file. Selection by index would shift the user
// onto whatever happens to share the old slot in the new list; selection
// must follow the file by identity (filePath / label).
func TestSidebar_SetItems_PreservesSelectionByIdentity(t *testing.T) {
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{label: "  bar.go", kind: itemNormal, filePath: "bar.go"},
		{label: "  foo.go", kind: itemNormal, filePath: "foo.go"},
	})
	s.SelectNext() // foo.go, index 1
	if s.SelectedItem() != "foo.go" {
		t.Fatalf("setup: expected foo.go selected, got %q", s.SelectedItem())
	}

	// A new file appears alphabetically before foo.go. The user's selection
	// must follow foo.go to its new index, not stay on index 1 (which is now
	// baz.go).
	s.SetItems([]sidebarItem{
		{label: "  bar.go", kind: itemNormal, filePath: "bar.go"},
		{label: "  baz.go", kind: itemNormal, filePath: "baz.go"},
		{label: "  foo.go", kind: itemNormal, filePath: "foo.go"},
	})
	if got := s.SelectedItem(); got != "foo.go" {
		t.Errorf("selection should follow foo.go, got %q at index %d", got, s.SelectedIndex())
	}
}

func TestSidebar_SetItems_PreservesSelectionByLabel(t *testing.T) {
	// Some sidebar items don't have a filePath (e.g. directory headers,
	// pseudo-entries like "new changes"). Selection must still follow them
	// by their displayed label.
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{label: "Section A", kind: itemHeader},
		{label: "  one", kind: itemNormal},
		{label: "  two", kind: itemNormal},
	})
	s.SelectIndex(2) // "  two"
	if s.SelectedItem() != "  two" {
		t.Fatalf("setup: expected '  two' selected, got %q", s.SelectedItem())
	}

	// A new item is inserted before; selection should follow "  two".
	s.SetItems([]sidebarItem{
		{label: "Section A", kind: itemHeader},
		{label: "  one", kind: itemNormal},
		{label: "  one-and-a-half", kind: itemNormal},
		{label: "  two", kind: itemNormal},
	})
	if got := s.SelectedItem(); got != "  two" {
		t.Errorf("selection should follow '  two', got %q at index %d", got, s.SelectedIndex())
	}
}

func TestSidebar_SetItems_FallsBackWhenSelectionGone(t *testing.T) {
	// If the selected file disappears (e.g. it was deleted), the selection
	// should clamp to a sensible nearby index rather than blowing up.
	s := newSidebar()
	s.SetItems([]sidebarItem{
		{label: "  a.go", kind: itemNormal, filePath: "a.go"},
		{label: "  b.go", kind: itemNormal, filePath: "b.go"},
		{label: "  c.go", kind: itemNormal, filePath: "c.go"},
	})
	s.SelectIndex(1) // b.go

	s.SetItems([]sidebarItem{
		{label: "  a.go", kind: itemNormal, filePath: "a.go"},
		{label: "  c.go", kind: itemNormal, filePath: "c.go"},
	})
	// b.go is gone; selection should fall back to a valid item.
	if s.SelectedIndex() < 0 || s.SelectedIndex() >= len(s.items) {
		t.Errorf("selection out of range: %d", s.SelectedIndex())
	}
}

// Spec: "the sidebar should visually distinguish the cursor position from
// the pinned (currently viewing) file when they differ." The pinned file
// is the one currently displayed in the main pane, set via SetPinnedID.
// When the cursor moves over a directory or pseudo-entry, the pinned file
// must still be visible at a glance.
func TestSidebar_PinnedItem_RenderedWithDistinctStyle(t *testing.T) {
	s := newSidebar()
	s.width = 30
	s.height = 10
	s.SetItems([]sidebarItem{
		{label: "  pinned.go", kind: itemNormal, filePath: "pinned.go"},
		{label: "  cursor.go", kind: itemNormal, filePath: "cursor.go"},
	})
	s.SetPinnedID("pinned.go")
	s.SelectIndex(1) // cursor on cursor.go, pinned is pinned.go

	view := s.View(false)
	lines := strings.Split(view, "\n")
	// Find which line contains "pinned.go" and which "cursor.go"
	var pinnedLine, cursorLine string
	for _, ln := range lines {
		if strings.Contains(stripANSI(ln), "pinned.go") {
			pinnedLine = ln
		}
		if strings.Contains(stripANSI(ln), "cursor.go") {
			cursorLine = ln
		}
	}
	if pinnedLine == "" || cursorLine == "" {
		t.Fatalf("missing pinned/cursor line in view:\n%s", view)
	}
	// The two lines should have visibly different ANSI styling because
	// one carries the pinned style and the other carries the selected
	// style. A naive "same ANSI prefix" test catches the bug where the
	// pinned style isn't applied at all.
	if pinnedLine == cursorLine {
		t.Errorf("pinned and cursor lines render identically:\n  %q", pinnedLine)
	}
	// The pinned line should not look like a regular (unselected, unpinned)
	// row — verify it does carry some ANSI styling.
	if pinnedLine == stripANSI(pinnedLine) {
		t.Errorf("pinned line carries no ANSI styling: %q", pinnedLine)
	}
}

// When the cursor coincides with the pinned file, the cursor styling wins
// (it's the stronger signal) and the pinned style is suppressed.
func TestSidebar_PinnedItem_CursorOverridesPinnedStyle(t *testing.T) {
	s := newSidebar()
	s.width = 30
	s.height = 10
	s.SetItems([]sidebarItem{
		{label: "  shared.go", kind: itemNormal, filePath: "shared.go"},
		{label: "  other.go", kind: itemNormal, filePath: "other.go"},
	})
	s.SetPinnedID("shared.go")
	s.SelectIndex(0) // cursor on the same file as pinned

	pinnedAndCursor := s.View(false)

	// Now move the cursor away. The first line is no longer selected, but
	// it's still pinned.
	s.SelectIndex(1)
	pinnedNotCursor := s.View(false)

	// The two views must differ — when the cursor leaves shared.go, its
	// style switches from "selected (cursor)" to "pinned (no cursor)".
	if pinnedAndCursor == pinnedNotCursor {
		t.Errorf("pinned-also-cursor view should differ from pinned-only view; got identical output:\n%s", pinnedAndCursor)
	}
}

func TestSidebar_SetItems_SkipsSeparatorOnClamp(t *testing.T) {
	s := newSidebar()
	// If all items are separators, selected should still be 0
	// but in practice this shouldn't happen
	s.SetItems([]sidebarItem{
		{label: "", kind: itemSeparator},
		{label: "a.go", kind: itemNormal},
	})
	if s.SelectedIndex() != 1 {
		t.Errorf("selection should skip separator, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SkipToSelectable_AllSeparators(t *testing.T) {
	s := newSidebar()
	// Edge case: all items are separators (shouldn't happen in practice)
	s.items = []sidebarItem{
		{kind: itemSeparator},
		{kind: itemSeparator},
	}
	s.selected = 0
	s.skipToSelectable()
	// Should not panic, selected stays wherever it was
}

func TestSidebar_SkipToSelectable_ForwardSearch(t *testing.T) {
	s := newSidebar()
	// Separator at start, selectable after
	s.items = []sidebarItem{
		{kind: itemSeparator},
		{kind: itemSeparator},
		{label: "found.go", kind: itemNormal},
	}
	s.selected = 0
	s.skipToSelectable()
	if s.SelectedIndex() != 2 {
		t.Errorf("should skip forward to index 2, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SkipToSelectable_BackwardSearch(t *testing.T) {
	s := newSidebar()
	// Selectable before separator, separator at end
	s.items = []sidebarItem{
		{label: "found.go", kind: itemNormal},
		{kind: itemSeparator},
		{kind: itemSeparator},
	}
	s.selected = 1
	s.skipToSelectable()
	if s.SelectedIndex() != 0 {
		t.Errorf("should skip backward to index 0, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SkipToSelectable_SelectedPastEnd(t *testing.T) {
	s := newSidebar()
	s.items = []sidebarItem{
		{label: "a.go", kind: itemNormal},
	}
	s.selected = 5 // past end
	s.skipToSelectable()
	if s.SelectedIndex() != 0 {
		t.Errorf("should clamp to last, got %d", s.SelectedIndex())
	}
}

func TestSidebar_SelectFirst_Empty(t *testing.T) {
	s := newSidebar()
	s.SelectFirst() // should not panic
}

func TestSidebar_SelectLast_Empty(t *testing.T) {
	s := newSidebar()
	s.SelectLast() // should not panic
}

func TestSidebar_ClampOffset_SelectedBeforeOffset(t *testing.T) {
	s := newSidebar()
	s.SetSize(20, 3) // 3 visible lines

	items := make([]sidebarItem, 10)
	for i := range items {
		items[i] = sidebarItem{label: "item", kind: itemNormal}
	}
	s.SetItems(items)

	// Scroll down to the end
	for i := 0; i < 9; i++ {
		s.SelectNext()
	}
	// Now offset > 0 and selected = 9

	// Jump to first — offset should clamp down
	s.SelectFirst()
	if s.offset != 0 {
		t.Errorf("offset should be 0 after SelectFirst, got %d", s.offset)
	}
}

func TestSidebar_ClampOffset_ZeroVisible(t *testing.T) {
	s := newSidebar()
	// Don't set size, so height=0 -> visibleLines returns len(items)
	s.SetItems(items("a", "b"))
	s.SelectNext()
	// Should not panic with zero height
}

func TestBuildTreeItems_SingleLeafFlatPath(t *testing.T) {
	// Spec: "if there is only one leaf node in the tree, display the whole
	// relevant subtree on the same line, kind of like when tree mode is disabled."
	collapsed := make(map[string]bool)
	items := buildTreeItems([]string{"dir/sub/file.go"}, itemNormal, collapsed, nil)

	if len(items) != 1 {
		t.Fatalf("single-leaf tree should produce 1 item, got %d", len(items))
	}
	if items[0].filePath != "dir/sub/file.go" {
		t.Errorf("item should have full path, got %q", items[0].filePath)
	}
	if items[0].isDir {
		t.Error("single-leaf should be rendered as a file, not a directory")
	}
}

func TestBuildTreeItems_MultipleLeaves_NotFlattened(t *testing.T) {
	// Multiple files under a directory should still use tree structure
	collapsed := make(map[string]bool)
	items := buildTreeItems([]string{"dir/a.go", "dir/b.go"}, itemNormal, collapsed, nil)

	hasDirItem := false
	for _, item := range items {
		if item.isDir {
			hasDirItem = true
		}
	}
	if !hasDirItem {
		t.Error("multiple leaves should still show directory structure")
	}
}

func TestBuildTreeItems_SingleLeafPreservesKind(t *testing.T) {
	collapsed := make(map[string]bool)
	items := buildTreeItems([]string{"pkg/file.go"}, itemDim, collapsed, nil)

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].kind != itemDim {
		t.Errorf("single-leaf should preserve kind, got %v", items[0].kind)
	}
}

// === Sticky section header (Option A: overlay) ===
//
// Invariants:
//   I1. At offset 0, no overlay is rendered.
//   I2. When the topmost visible item is itself a header, no overlay is rendered.
//   I3. When scrolled past a header onto a non-header item, the overlay is the
//       most recent header before the offset.
//   I4. After clampOffset, selected != offset whenever an overlay would activate
//       (the cursor is never hidden under the sticky row).

func sectionedItems() []sidebarItem {
	return []sidebarItem{
		{label: "New Changes (1)", kind: itemHeader}, // 0
		{label: "  z.go", kind: itemNormal},          // 1
		{label: "Committed (3)", kind: itemHeader},   // 2
		{label: "  a.go", kind: itemNormal},          // 3
		{label: "  b.go", kind: itemNormal},          // 4
		{label: "  c.go", kind: itemNormal},          // 5
	}
}

func TestSidebar_StickyHeader_NoneAtTop(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(20, 4)
	if got := s.stickyHeaderIndex(); got != -1 {
		t.Errorf("offset 0: expected no overlay, got idx %d", got)
	}
}

func TestSidebar_StickyHeader_NoneOnHeaderRow(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(20, 4)
	s.offset = 2 // "Committed (3)" header is the topmost visible row
	if got := s.stickyHeaderIndex(); got != -1 {
		t.Errorf("topmost is header: expected no overlay, got idx %d", got)
	}
}

func TestSidebar_StickyHeader_FindsPreviousHeader(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(20, 4)
	s.offset = 4 // "  b.go" — topmost; nearest header before is "Committed (3)" at idx 2
	if got := s.stickyHeaderIndex(); got != 2 {
		t.Errorf("expected sticky idx 2, got %d", got)
	}
	s.offset = 1 // "  z.go" — first item under "New Changes" header
	if got := s.stickyHeaderIndex(); got != 0 {
		t.Errorf("expected sticky idx 0, got %d", got)
	}
}

// I4: clampOffset must never leave selection at the row that would be covered
// by a sticky header.
func TestSidebar_ClampOffset_KeepsCursorVisible(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(20, 3) // tiny visible area to stress the clamping
	s.SelectIndex(5) // select "  c.go"
	if s.selected == s.offset && s.stickyHeaderIndex() >= 0 {
		t.Fatalf("after selecting c.go: cursor hidden under sticky (selected=%d, offset=%d, sticky=%d)",
			s.selected, s.offset, s.stickyHeaderIndex())
	}
	// Manually pin the offset onto the cursor's index, simulating an external
	// update path that doesn't go through clampOffset's adjustment.
	s.SelectIndex(4)
	s.offset = 4 // selected == offset, sticky would activate
	s.clampOffset()
	if s.selected == s.offset && s.stickyHeaderIndex() >= 0 {
		t.Fatalf("clampOffset failed to bump offset: selected=%d, offset=%d, sticky=%d",
			s.selected, s.offset, s.stickyHeaderIndex())
	}
}

// View output: row 0 should contain the sticky header text rather than the
// hidden item's label when an overlay is active.
func TestSidebar_View_OverlaysStickyHeader(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(30, 4)
	s.offset = 4 // overlay "Committed (3)" hiding "  b.go"

	out := stripANSI(s.View(false))
	lines := strings.Split(out, "\n")
	// lines[0] is top border (╭...╮); lines[1] is the first content row.
	if !strings.Contains(lines[1], "Committed") {
		t.Errorf("row 1 should contain sticky header 'Committed', got %q", lines[1])
	}
	if strings.Contains(lines[1], "b.go") {
		t.Errorf("row 1 should not still show the hidden item 'b.go', got %q", lines[1])
	}
	// The hidden item is at offset, so rows 2+ should show items[5] = c.go etc.
	bodyJoined := strings.Join(lines[2:], "\n")
	if !strings.Contains(bodyJoined, "c.go") {
		t.Errorf("body rows should show c.go, got: %q", bodyJoined)
	}
}

// At offset 0 there's no overlay, so row 1 shows items[0] (the section header
// itself, naturally placed).
func TestSidebar_View_NoOverlayAtTop(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(30, 4)
	out := stripANSI(s.View(false))
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[1], "New Changes") {
		t.Errorf("row 1 at top should show first item 'New Changes', got %q", lines[1])
	}
}

// Cross-section scroll: when offset crosses from one section into the next,
// the sticky overlay swaps to the new section's header.
func TestSidebar_StickyHeader_CrossesSections(t *testing.T) {
	s := newSidebar()
	s.SetItems(sectionedItems())
	s.SetSize(30, 3)
	// In "New Changes" section: items[1] is the first non-header item.
	s.offset = 1
	if got := s.stickyHeaderIndex(); got != 0 {
		t.Errorf("inside New Changes: sticky idx = %d, want 0", got)
	}
	// Move offset into the "Committed" section.
	s.offset = 3
	if got := s.stickyHeaderIndex(); got != 2 {
		t.Errorf("inside Committed: sticky idx = %d, want 2", got)
	}
}
