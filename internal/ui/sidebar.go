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
	itemCutline                   // commit-range scope boundary, not selectable
)

func (k sidebarItemKind) selectable() bool {
	return k != itemSeparator && k != itemHeader && k != itemCutline
}

type sidebarItem struct {
	label    string
	prefix   string // rendered dim, before the label
	suffix   string // rendered dim, after the label (right-aligned if space)
	kind     sidebarItemKind
	filePath string // actual file path (for file items)
	isDir    bool   // true for directory entries in tree mode
	indent   int    // indentation level in tree mode
	// collapseKey is the key used to look up directory expand/collapse state
	// in m.collapsedDirs. Empty for non-directory items. The key is
	// section-qualified ("section|path") so the same directory path appearing
	// in multiple sections (e.g. "pkg/" under both Committed and All Files)
	// has independent collapse state.
	collapseKey string
}

// dirCollapseKey returns the section-qualified collapse-state key for a
// directory path within a sidebar section.
func dirCollapseKey(section, path string) string {
	return section + "|" + path
}

// Section names used as the prefix for collapse-state keys. Each section in
// the files-mode sidebar gets its own namespace so the same directory path
// appearing in multiple sections has independent expand/collapse state.
const (
	sectionUncommitted = "uncommitted"
	sectionStaged      = "staged"
	sectionCommitted   = "committed"
	sectionAllFiles    = "all-files"
)

// buildTreeItems converts a flat list of file paths into a tree-structured list
// of sidebar items with directories and indentation. Directories start expanded.
// The collapsed map tracks which directory paths are collapsed.
// kindFunc returns the item kind for a given file path. If nil, the default kind is used.
// forceDirs is the set of paths that must be classified as directories even
// when they appear as leafmost segments with no children — used for ignored
// top-level dirs whose contents haven't been lazy-loaded yet, so they sort
// in the dir bucket alongside other directories.
type kindFunc func(filePath string) sidebarItemKind

// buildTreeItems builds the directory-tree representation for one sidebar
// section. The section name is mixed into the collapse-state key so the same
// directory appearing in multiple sections has independent expand/collapse
// state.
func buildTreeItems(files []string, kind sidebarItemKind, section string, collapsed map[string]bool, forceDirs map[string]bool, kf ...kindFunc) []sidebarItem {
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
					isFile:   isLast && !forceDirs[path],
					kind:     kind,
				}
				node.children[part] = child
			}
			if isLast {
				if !forceDirs[child.path] {
					child.isFile = true
				}
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

			// Compact single-child directory chains: if a directory's only
			// child is another directory, merge them into one entry
			// (e.g. "foo/bar/baz/" instead of separate "foo/", "bar/", "baz/").
			displayName := name
			compacted := child
			for {
				var cdirs, cfiles int
				var onlyChild *treeNode
				for _, c := range compacted.children {
					if c.isFile && len(c.children) == 0 {
						cfiles++
					} else {
						cdirs++
						onlyChild = c
					}
				}
				if cdirs == 1 && cfiles == 0 {
					displayName += "/" + onlyChild.name
					compacted = onlyChild
				} else {
					break
				}
			}

			// Single leaf: display the remaining path from compacted dir on one line, no directory entry
			if leafCount(compacted) == 1 {
				cur := compacted
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
						// Show path relative to parent: compacted dir name + remaining subdirs + filename
						relPath := strings.TrimPrefix(leafNode.path, compacted.path+"/")
						displayLabel := displayName + "/" + relPath
						// filePath below stays raw; only the label is escaped.
						label := strings.Repeat("  ", indent) + "  " + sanitizeDisplayText(displayLabel)
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

			cKey := dirCollapseKey(section, compacted.path)
			prefix := "▼"
			if collapsed[cKey] {
				prefix = "▶"
			}
			// Use compacted.kind so an ignored dir entry can render dim
			// (kindFunc was applied when the path was added as a leafmost
			// entry). For purely intermediate dirs that never appeared as
			// a leafmost path, compacted.kind defaults to the kind parameter.
			dirKind := compacted.kind
			label := strings.Repeat("  ", indent) + prefix + " " + sanitizeDisplayText(displayName) + "/"
			items = append(items, sidebarItem{
				label:       label,
				kind:        dirKind,
				filePath:    compacted.path,
				isDir:       true,
				indent:      indent,
				collapseKey: cKey,
			})
			if !collapsed[cKey] {
				flatten(compacted, indent+1)
			}
		}
		for _, name := range fileNames {
			child := node.children[name]
			label := strings.Repeat("  ", indent) + "  " + sanitizeDisplayText(name)
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
	offset     int    // scroll offset for long lists
	hoverIndex int    // item under mouse cursor (-1 = none)
	pinnedID   string // identity of the item currently shown in the main pane
}

func newSidebar() *sidebar {
	return &sidebar{hoverIndex: -1}
}

// SetHoverIndex sets which item is being hovered by the mouse.
func (s *sidebar) SetHoverIndex(idx int) {
	s.hoverIndex = idx
}

// SetPinnedID records which item is currently displayed in the main pane.
// The cursor (selected) and the pinned item often coincide, but when the
// cursor moves over a directory or pseudo-entry the main pane keeps showing
// the previously-selected file. View() renders the pinned item with a
// distinct style when it differs from the cursor so the user can see at a
// glance which file is in the main pane.
func (s *sidebar) SetPinnedID(id string) {
	s.pinnedID = id
}

func (s *sidebar) SetItems(items []sidebarItem) {
	// Capture the previously-selected item by identity. After installing
	// the new items, we look it up again so the user's selection follows
	// the file/header they were on rather than landing on whatever happens
	// to share the old slot. Without this, a refresh that adds or removes
	// a file shuffles the index → main pane suddenly switches files.
	//
	// The same identifier (e.g. a directory path) can appear in multiple
	// sections (the same dir lives under "Committed" and again under
	// "All Files"). When that happens we prefer the match closest to the
	// previous index so a no-op refresh keeps everything in place.
	prevIdx := s.selected
	var prevID string
	if prevIdx >= 0 && prevIdx < len(s.items) {
		prevID = itemID(s.items[prevIdx])
	}

	s.items = items

	if prevID != "" {
		bestIdx := -1
		bestDist := 0
		for i, it := range items {
			if !it.kind.selectable() || itemID(it) != prevID {
				continue
			}
			dist := i - prevIdx
			if dist < 0 {
				dist = -dist
			}
			if bestIdx == -1 || dist < bestDist {
				bestIdx = i
				bestDist = dist
			}
		}
		if bestIdx >= 0 {
			s.selected = bestIdx
			s.clampOffsetBounds()
			return
		}
	}

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

// itemID returns the stable identifier for a sidebar item used to track
// selection across refreshes. File items use their filePath; everything else
// uses prefix+label (matching SelectedItem so callers compare apples-to-apples).
func itemID(it sidebarItem) string {
	if it.filePath != "" {
		return it.filePath
	}
	return it.prefix + it.label
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
	if s.selected < 0 || s.selected >= len(s.items) {
		return ""
	}
	// Same canonical key as itemID — one definition, so save/restore and
	// SetItems all compare apples to apples.
	return itemID(s.items[s.selected])
}

// SelectedCollapseKey returns the section-qualified collapse-state key of
// the currently-selected directory (empty string for non-directory items).
// Use this when toggling expand/collapse so the toggle is scoped to the
// section the user clicked, not to every section that happens to contain
// the same directory path.
func (s *sidebar) SelectedCollapseKey() string {
	if len(s.items) == 0 || s.selected >= len(s.items) {
		return ""
	}
	return s.items[s.selected].collapseKey
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
	// If a sticky section header would otherwise hide the selected item under
	// the topmost visible row, scroll one extra line up so selection lands at
	// row 1 instead of row 0.
	if s.selected == s.offset && s.stickyHeaderIndex() >= 0 && s.offset > 0 {
		s.offset--
	}
}

// stickyHeaderIndex returns the index of the section header that should be
// pinned to the topmost visible row, or -1 if no overlay is needed. An overlay
// is shown when the user has scrolled past a header and the topmost visible
// item is not itself a header.
func (s *sidebar) stickyHeaderIndex() int {
	if s.offset <= 0 || s.offset >= len(s.items) {
		return -1
	}
	if s.items[s.offset].kind == itemHeader {
		return -1
	}
	for i := s.offset - 1; i >= 0; i-- {
		if s.items[i].kind == itemHeader {
			return i
		}
	}
	return -1
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

	stickyIdx := s.stickyHeaderIndex()

	var b strings.Builder
	for i := s.offset; i < end; i++ {
		if i > s.offset {
			b.WriteString("\n")
		}
		// Sticky overlay: replace the topmost visible item with the section
		// header that's logically above it. The hidden item lives at index
		// s.offset; clampOffset keeps the cursor off it.
		var item sidebarItem
		if i == s.offset && stickyIdx >= 0 {
			item = s.items[stickyIdx]
		} else {
			item = s.items[i]
		}

		if item.kind == itemSeparator {
			sep := strings.Repeat("─", s.width)
			b.WriteString(sidebarSeparatorStyle.Render(sep))
			continue
		}

		if item.kind == itemCutline {
			label := " scope "
			labelW := runewidth.StringWidth(label)
			leftFill := (s.width - labelW) / 2
			rightFill := s.width - labelW - leftFill
			if leftFill < 0 {
				leftFill = 0
			}
			if rightFill < 0 {
				rightFill = 0
			}
			cutline := strings.Repeat("─", leftFill) + label + strings.Repeat("─", rightFill)
			b.WriteString(sidebarSeparatorStyle.Render(cutline))
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

		// Pick styles based on selection/hover/pinned state and item kind.
		// Selected wins over hover wins over pinned — the cursor/hover
		// signals are stronger because they reflect immediate user input,
		// while "pinned" just identifies whatever the main pane is showing.
		isPinned := s.pinnedID != "" && itemID(item) == s.pinnedID
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
		} else if isPinned {
			switch item.kind {
			case itemDim:
				labelStyle = sidebarPinnedUncommittedStyle
			case itemDeleted:
				labelStyle = sidebarPinnedDeletedStyle
			default:
				labelStyle = sidebarPinnedStyle
			}
			dimStyle = sidebarPinnedDimStyle
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
			// Simple path: single-styled label.
			label := item.label
			if s.width > 0 && runewidth.StringWidth(label) > s.width {
				label = runewidth.Truncate(label, s.width, "")
			}
			if s.width > 0 {
				// Pad with spaces to fill sidebar width
				pad := s.width - runewidth.StringWidth(label)
				if pad > 0 {
					label += strings.Repeat(" ", pad)
				}
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
