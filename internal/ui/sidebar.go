package ui

import (
	"fmt"
	"strings"
)

type sidebarItemKind int

const (
	itemNormal    sidebarItemKind = iota
	itemDim                       // uncommitted files — rendered dimmer
	itemSeparator                 // horizontal line, not selectable
)

type sidebarItem struct {
	label string
	kind  sidebarItemKind
}

type sidebar struct {
	items    []sidebarItem
	selected int
	width    int
	height   int
	offset   int // scroll offset for long lists
}

func newSidebar() *sidebar {
	return &sidebar{}
}

func (s *sidebar) SetItems(items []sidebarItem) {
	s.items = items
	if s.selected >= len(items) {
		s.selected = max(0, len(items)-1)
	}
	// Ensure selection isn't on a separator
	s.skipToSelectable()
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
	return s.items[s.selected].label
}

func (s *sidebar) SelectNext() {
	for i := s.selected + 1; i < len(s.items); i++ {
		if s.items[i].kind != itemSeparator {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

func (s *sidebar) SelectPrev() {
	for i := s.selected - 1; i >= 0; i-- {
		if s.items[i].kind != itemSeparator {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

// skipToSelectable moves selection to the nearest selectable item.
func (s *sidebar) skipToSelectable() {
	if len(s.items) == 0 {
		return
	}
	if s.selected >= len(s.items) {
		s.selected = len(s.items) - 1
	}
	if s.items[s.selected].kind != itemSeparator {
		return
	}
	// Try forward then backward
	for i := s.selected; i < len(s.items); i++ {
		if s.items[i].kind != itemSeparator {
			s.selected = i
			return
		}
	}
	for i := s.selected; i >= 0; i-- {
		if s.items[i].kind != itemSeparator {
			s.selected = i
			return
		}
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
		item := s.items[i]

		if item.kind == itemSeparator {
			sep := strings.Repeat("─", s.width)
			b.WriteString(sidebarSeparatorStyle.Render(sep))
			continue
		}

		label := item.label
		if s.width > 0 && len(label) > s.width {
			label = label[:s.width]
		}
		if s.width > 0 {
			label = fmt.Sprintf("%-*s", s.width, label)
		}

		if i == s.selected {
			switch item.kind {
			case itemDim:
				b.WriteString(sidebarUncommittedSelectedStyle.Render(label))
			default:
				b.WriteString(sidebarSelectedItemStyle.Render(label))
			}
		} else {
			switch item.kind {
			case itemDim:
				b.WriteString(sidebarUncommittedStyle.Render(label))
			default:
				b.WriteString(sidebarItemStyle.Render(label))
			}
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
