package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type sidebarItemKind int

const (
	itemNormal    sidebarItemKind = iota
	itemDim                       // uncommitted files — rendered dimmer
	itemSeparator                 // horizontal line, not selectable
	itemDeleted                   // deleted files — rendered in red
)

type sidebarItem struct {
	label    string
	kind     sidebarItemKind
	filePath string // actual file path (for file items)
	isDir    bool   // true for directory entries in tree mode
	indent   int    // indentation level in tree mode
}

// buildTreeItems converts a flat list of file paths into a tree-structured list
// of sidebar items with directories and indentation. Directories start expanded.
// The collapsed map tracks which directory paths are collapsed.
// kindFunc returns the item kind for a given file path. If nil, the default kind is used.
type kindFunc func(filePath string) sidebarItemKind

func buildTreeItems(files []string, kind sidebarItemKind, collapsed map[string]bool, kf ...kindFunc) []sidebarItem {
	if len(files) == 0 {
		return nil
	}

	// Build a tree structure
	type treeNode struct {
		name     string
		path     string // full path
		children map[string]*treeNode
		isFile   bool
		kind     sidebarItemKind
	}

	root := &treeNode{children: make(map[string]*treeNode)}

	for _, f := range files {
		parts := strings.Split(f, string(filepath.Separator))
		node := root
		for i, part := range parts {
			isLast := i == len(parts)-1
			child, ok := node.children[part]
			if !ok {
				path := strings.Join(parts[:i+1], string(filepath.Separator))
				child = &treeNode{
					name:     part,
					path:     path,
					children: make(map[string]*treeNode),
					isFile:   isLast,
					kind:     kind,
				}
				node.children[part] = child
			}
			if isLast {
				child.isFile = true
				if len(kf) > 0 && kf[0] != nil {
					child.kind = kf[0](f)
				} else {
					child.kind = kind
				}
			}
			node = child
		}
	}

	// leafCount returns the total number of leaf (file) nodes under a node.
	var leafCount func(n *treeNode) int
	leafCount = func(n *treeNode) int {
		count := 0
		for _, child := range n.children {
			if child.isFile && len(child.children) == 0 {
				count++
			} else {
				count += leafCount(child)
			}
		}
		return count
	}

	// Flatten tree into items
	var items []sidebarItem
	var flatten func(node *treeNode, indent int)
	flatten = func(node *treeNode, indent int) {
		// Sort children: directories first, then files, alphabetically
		var dirNames, fileNames []string
		for name, child := range node.children {
			if child.isFile && len(child.children) == 0 {
				fileNames = append(fileNames, name)
			} else {
				dirNames = append(dirNames, name)
			}
		}
		sort.Strings(dirNames)
		sort.Strings(fileNames)

		for _, name := range dirNames {
			child := node.children[name]

			// Spec: if there is only one leaf node, display the whole subtree on one line
			if leafCount(child) == 1 {
				// Find the single leaf by traversing down
				cur := child
				for {
					var nextDir *treeNode
					var leafNode *treeNode
					for _, c := range cur.children {
						if c.isFile && len(c.children) == 0 {
							leafNode = c
						} else {
							nextDir = c
						}
					}
					if leafNode != nil {
						// Found the leaf — render as flat path
						label := strings.Repeat("  ", indent) + "  " + leafNode.path
						items = append(items, sidebarItem{
							label:    label,
							kind:     leafNode.kind,
							filePath: leafNode.path,
							indent:   indent,
						})
						break
					}
					if nextDir == nil {
						break
					}
					cur = nextDir
				}
				continue
			}

			prefix := ""
			if collapsed[child.path] {
				prefix = "▶"
			} else {
				prefix = "▼"
			}
			label := strings.Repeat("  ", indent) + prefix + " " + name + "/"
			items = append(items, sidebarItem{
				label:    label,
				kind:     kind,
				filePath: child.path,
				isDir:    true,
				indent:   indent,
			})
			if !collapsed[child.path] {
				flatten(child, indent+1)
			}
		}
		for _, name := range fileNames {
			child := node.children[name]
			label := strings.Repeat("  ", indent) + "  " + name
			items = append(items, sidebarItem{
				label:    label,
				kind:     child.kind,
				filePath: child.path,
				indent:   indent,
			})
		}
	}

	flatten(root, 0)
	return items
}

type sidebar struct {
	items      []sidebarItem
	selected   int
	width      int
	height     int
	offset     int // scroll offset for long lists
	hoverIndex int // item under mouse cursor (-1 = none)
}

func newSidebar() *sidebar {
	return &sidebar{hoverIndex: -1}
}

// SetHoverIndex sets which item is being hovered by the mouse.
func (s *sidebar) SetHoverIndex(idx int) {
	s.hoverIndex = idx
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
	item := s.items[s.selected]
	if item.filePath != "" {
		return item.filePath
	}
	return item.label
}

// SelectedIsDir returns true if the selected item is a directory.
func (s *sidebar) SelectedIsDir() bool {
	if len(s.items) == 0 || s.selected >= len(s.items) {
		return false
	}
	return s.items[s.selected].isDir
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

func (s *sidebar) SelectFirst() {
	for i := 0; i < len(s.items); i++ {
		if s.items[i].kind != itemSeparator {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

func (s *sidebar) SelectLast() {
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].kind != itemSeparator {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

func (s *sidebar) SelectIndex(idx int) {
	if idx < 0 || idx >= len(s.items) {
		return
	}
	if s.items[idx].kind == itemSeparator {
		return
	}
	s.selected = idx
	s.clampOffset()
}

// ScrollUp scrolls the sidebar view up by one line without changing selection.
func (s *sidebar) ScrollUp() {
	if s.offset > 0 {
		s.offset--
	}
}

// ScrollDown scrolls the sidebar view down by one line without changing selection.
func (s *sidebar) ScrollDown() {
	maxOffset := len(s.items) - s.visibleLines()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.offset < maxOffset {
		s.offset++
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
			case itemDeleted:
				b.WriteString(sidebarDeletedSelectedStyle.Render(label))
			default:
				b.WriteString(sidebarSelectedItemStyle.Render(label))
			}
		} else if i == s.hoverIndex {
			switch item.kind {
			case itemDim:
				b.WriteString(sidebarUncommittedHoverStyle.Render(label))
			case itemDeleted:
				b.WriteString(sidebarDeletedHoverStyle.Render(label))
			default:
				b.WriteString(sidebarHoverStyle.Render(label))
			}
		} else {
			switch item.kind {
			case itemDim:
				b.WriteString(sidebarUncommittedStyle.Render(label))
			case itemDeleted:
				b.WriteString(sidebarDeletedStyle.Render(label))
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

	// lipgloss v2: Width sets the outer dimension (includes borders).
	// Add 2 for the left+right border characters.
	return style.Width(s.width + 2).Render(content)
}
