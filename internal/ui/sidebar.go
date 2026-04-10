package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	runewidth "github.com/mattn/go-runewidth"
)

type sidebarItemKind int

const (
	itemNormal    sidebarItemKind = iota
	itemDim                       // hidden/ignored files — rendered dimmer
	itemSeparator                 // horizontal line, not selectable
	itemDeleted                   // deleted files — rendered in red
	itemHeader                    // section title, not selectable
)

func (k sidebarItemKind) selectable() bool {
	return k != itemSeparator && k != itemHeader
}

type sidebarItem struct {
	label    string
	prefix   string // rendered dim, before the label
	suffix   string // rendered dim, after the label (right-aligned if space)
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
	// Keep offset in valid range without snapping to the selected item.
	// clampOffset would scroll-to-selected, which is wrong here: the user
	// may have scrolled away from the selection and a periodic refresh
	// shouldn't jump them back.
	s.clampOffsetBounds()
}

func (s *sidebar) SetSize(w, h int) {
	s.width = w
	s.height = h
	s.clampOffsetBounds()
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
	return item.prefix + item.label
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
		if s.items[i].kind.selectable() {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

func (s *sidebar) SelectPrev() {
	for i := s.selected - 1; i >= 0; i-- {
		if s.items[i].kind.selectable() {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

func (s *sidebar) SelectFirst() {
	for i := 0; i < len(s.items); i++ {
		if s.items[i].kind.selectable() {
			s.selected = i
			s.clampOffset()
			return
		}
	}
}

func (s *sidebar) SelectLast() {
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].kind.selectable() {
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
	if !s.items[idx].kind.selectable() {
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
	if s.items[s.selected].kind.selectable() {
		return
	}
	// Try forward then backward
	for i := s.selected; i < len(s.items); i++ {
		if s.items[i].kind.selectable() {
			s.selected = i
			return
		}
	}
	for i := s.selected; i >= 0; i-- {
		if s.items[i].kind.selectable() {
			s.selected = i
			return
		}
	}
}

// clampOffset adjusts the scroll offset so the selected item is visible.
// Use after user navigation (arrow keys, mouse click on item, etc.).
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

// clampOffsetBounds keeps the offset within valid range [0, len-visible]
// without forcing the selected item to be visible. Use after item list
// updates where we want to preserve the user's scroll position.
func (s *sidebar) clampOffsetBounds() {
	if s.offset < 0 {
		s.offset = 0
	}
	visible := s.visibleLines()
	if visible <= 0 {
		return
	}
	maxOffset := len(s.items) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.offset > maxOffset {
		s.offset = maxOffset
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

		if item.kind == itemHeader {
			label := item.label
			if s.width > 0 {
				label = fmt.Sprintf("%-*s", s.width, label)
			}
			b.WriteString(sidebarHeaderStyle.Render(label))
			continue
		}

		// Pick styles based on selection/hover state and item kind.
		var labelStyle, dimStyle lipgloss.Style
		if i == s.selected {
			switch item.kind {
			case itemDim:
				labelStyle = sidebarUncommittedSelectedStyle
			case itemDeleted:
				labelStyle = sidebarDeletedSelectedStyle
			default:
				labelStyle = sidebarSelectedItemStyle
			}
			dimStyle = sidebarSelectedDimStyle
		} else if i == s.hoverIndex {
			switch item.kind {
			case itemDim:
				labelStyle = sidebarUncommittedHoverStyle
			case itemDeleted:
				labelStyle = sidebarDeletedHoverStyle
			default:
				labelStyle = sidebarHoverStyle
			}
			dimStyle = sidebarHoverDimStyle
		} else {
			switch item.kind {
			case itemDim:
				labelStyle = sidebarUncommittedStyle
			case itemDeleted:
				labelStyle = sidebarDeletedStyle
			default:
				labelStyle = sidebarItemStyle
			}
			dimStyle = sidebarDimStyle
		}

		if item.prefix == "" && item.suffix == "" {
			// Simple path: single-styled label (unchanged behavior).
			label := item.label
			if s.width > 0 && len(label) > s.width {
				label = label[:s.width]
			}
			if s.width > 0 {
				label = fmt.Sprintf("%-*s", s.width, label)
			}
			b.WriteString(labelStyle.Render(label))
		} else {
			// Composite label: dim prefix + styled label + dim suffix, padded to width.
			prefix := item.prefix
			label := item.label
			suffix := item.suffix

			prefixW := runewidth.StringWidth(prefix)
			labelW := runewidth.StringWidth(label)
			suffixW := runewidth.StringWidth(suffix)
			contentW := prefixW + labelW + suffixW
			if s.width > 0 && contentW > s.width {
				// Truncate: prefix stays, suffix stays if it fits, label shrinks.
				avail := s.width - prefixW - suffixW
				if avail < 1 {
					// Not enough room for suffix; drop it.
					suffix = ""
					suffixW = 0
					avail = s.width - prefixW
				}
				if avail < 1 {
					avail = 1
				}
				if labelW > avail {
					label = runewidth.Truncate(label, avail, "")
					labelW = runewidth.StringWidth(label)
				}
				contentW = prefixW + labelW + suffixW
			}

			// Build the line with padding between label and suffix.
			var line strings.Builder
			line.WriteString(dimStyle.Render(prefix))
			pad := 0
			if s.width > 0 {
				pad = s.width - contentW
			}
			line.WriteString(labelStyle.Render(label))
			if pad > 0 {
				line.WriteString(labelStyle.Render(strings.Repeat(" ", pad)))
			}
			line.WriteString(dimStyle.Render(suffix))
			b.WriteString(line.String())
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
