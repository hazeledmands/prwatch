package ui

import "testing"

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
