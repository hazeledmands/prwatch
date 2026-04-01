package ui

import "testing"

func TestSidebar_SelectNext(t *testing.T) {
	s := newSidebar()
	s.SetItems([]string{"file1.go", "file2.go", "file3.go"})

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
	s.SetItems([]string{"file1.go", "file2.go"})

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
	s.SetItems([]string{"a", "b", "c"})

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
	s.SetItems([]string{"a", "b", "c"})
	s.SelectNext()
	s.SelectNext() // index = 2

	s.SetItems([]string{"x"}) // shrink list
	if s.SelectedIndex() != 0 {
		t.Errorf("selection should clamp to 0, got %d", s.SelectedIndex())
	}
}
