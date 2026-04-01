package ui

import (
	"fmt"
	"strings"
)

type sidebar struct {
	items    []string
	selected int
	width    int
	height   int
	offset   int // scroll offset for long lists
}

func newSidebar() *sidebar {
	return &sidebar{}
}

func (s *sidebar) SetItems(items []string) {
	s.items = items
	if s.selected >= len(items) {
		s.selected = max(0, len(items)-1)
	}
	s.clampOffset()
}

func (s *sidebar) SetSize(w, h int) {
	s.width = w
	s.height = h
	s.clampOffset()
}

func (s *sidebar) SelectedIndex() int {
	return s.selected
}

func (s *sidebar) SelectedItem() string {
	if len(s.items) == 0 {
		return ""
	}
	return s.items[s.selected]
}

func (s *sidebar) SelectNext() {
	if s.selected < len(s.items)-1 {
		s.selected++
		s.clampOffset()
	}
}

func (s *sidebar) SelectPrev() {
	if s.selected > 0 {
		s.selected--
		s.clampOffset()
	}
}

func (s *sidebar) clampOffset() {
	visible := s.visibleLines()
	if visible <= 0 {
		return
	}
	if s.selected < s.offset {
		s.offset = s.selected
	}
	if s.selected >= s.offset+visible {
		s.offset = s.selected - visible + 1
	}
}

func (s *sidebar) visibleLines() int {
	if s.height <= 0 {
		return len(s.items)
	}
	return s.height
}

func (s *sidebar) View(focused bool) string {
	if len(s.items) == 0 {
		return ""
	}

	visible := s.visibleLines()
	end := s.offset + visible
	if end > len(s.items) {
		end = len(s.items)
	}

	var b strings.Builder
	for i := s.offset; i < end; i++ {
		if i > s.offset {
			b.WriteString("\n")
		}
		label := s.items[i]
		if s.width > 0 && len(label) > s.width {
			label = label[:s.width]
		}
		if s.width > 0 {
			label = fmt.Sprintf("%-*s", s.width, label)
		}
		if i == s.selected {
			b.WriteString(sidebarSelectedItemStyle.Render(label))
		} else {
			b.WriteString(sidebarItemStyle.Render(label))
		}
	}

	style := sidebarStyle
	if focused {
		style = sidebarFocusedStyle
	}
	content := b.String()

	// Pad to fill height
	lines := strings.Count(content, "\n") + 1
	for lines < s.height {
		content += "\n" + strings.Repeat(" ", s.width)
		lines++
	}

	return style.Width(s.width).Render(content)
}
