package ui

// viewMemory holds per-mode view state so switching away and back restores
// what the user was looking at. It owns two pieces:
//
//  1. modeStates: sidebar selection + scroll offset + focus, per mode.
//  2. mainScroll: source-line at the top of the main pane, per (mode, item)
//     pair. updateMainContent consults this when re-displaying an item.
type viewMemory struct {
	modeStates map[Mode]modeViewState
	mainScroll map[mainItemKey]int
}

func newViewMemory() *viewMemory {
	return &viewMemory{
		modeStates: make(map[Mode]modeViewState),
		mainScroll: make(map[mainItemKey]int),
	}
}

// SaveSidebar captures the sidebar selection/scroll and focus for mode.
func (v *viewMemory) SaveSidebar(mode Mode, sb *sidebar, focus Focus) {
	v.modeStates[mode] = modeViewState{
		sidebarSelected: sb.SelectedItem(),
		sidebarOffset:   sb.offset,
		focus:           focus,
	}
}

// RestoreSidebar applies the previously-saved selection/scroll for mode if any.
// Returns the saved focus, or the current focus when nothing is stored.
func (v *viewMemory) RestoreSidebar(mode Mode, sb *sidebar, currentFocus Focus) Focus {
	state, ok := v.modeStates[mode]
	if !ok {
		return currentFocus
	}
	if state.sidebarSelected != "" {
		for i, item := range sb.items {
			if item.kind.selectable() && item.label == state.sidebarSelected {
				sb.SelectIndex(i)
				break
			}
		}
	}
	sb.offset = state.sidebarOffset
	sb.clampOffset()
	return state.focus
}

// RememberMainScroll records the top-source-line of the main pane against the
// given key. No-op when key.item is empty.
func (v *viewMemory) RememberMainScroll(key mainItemKey, sourceLine int) {
	if key.item == "" {
		return
	}
	v.mainScroll[key] = sourceLine
}

// RecallMainScroll returns the saved source line for key, plus a bool.
func (v *viewMemory) RecallMainScroll(key mainItemKey) (int, bool) {
	line, ok := v.mainScroll[key]
	return line, ok
}
