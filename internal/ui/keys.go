package ui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	QuitConfirm    key.Binding
	QuitImmediate  key.Binding
	ToggleMode     key.Binding
	FilesMode      key.Binding
	CommitsMode    key.Binding
	PRMode         key.Binding
	FocusLeft      key.Binding
	FocusRight     key.Binding
	CursorLeft     key.Binding
	CursorRight    key.Binding
	FocusSidebar   key.Binding
	FocusMain      key.Binding
	FocusToggle    key.Binding
	Up             key.Binding
	Down           key.Binding
	PageUp         key.Binding
	PageDown       key.Binding
	Enter          key.Binding
	GoTop          key.Binding
	GoBottom       key.Binding
	Search         key.Binding
	Help           key.Binding
	SidebarGrow    key.Binding
	SidebarShrink  key.Binding
	SearchNext     key.Binding
	SearchPrev     key.Binding
	ToggleIgnored  key.Binding
	ToggleSidebar  key.Binding
	ToggleWrap     key.Binding
	ToggleLineNums key.Binding
	ToggleRemoved  key.Binding
	NextDiff       key.Binding
	PrevDiff       key.Binding
	Refresh        key.Binding
	NextLeaf       key.Binding
	PrevLeaf       key.Binding
	YankPath       key.Binding
	PRBrowse       key.Binding
	VisualStream   key.Binding
	VisualLine     key.Binding
	VisualDismiss  key.Binding

	ScopeExtendBack      key.Binding
	ScopeContractForward key.Binding
	ScopeReset           key.Binding
}

var keys = keyMap{
	QuitConfirm: key.NewBinding(
		key.WithKeys("q", "esc"),
	),
	QuitImmediate: key.NewBinding(
		key.WithKeys("Q", "ctrl+c"),
	),
	ToggleMode: key.NewBinding(
		key.WithKeys("m"),
	),
	// Mode switches: numeric keys only. The letter aliases (v/c/b) were
	// removed once v became visual-mode entry; c and b followed for
	// consistency. Cycle via [m].
	FilesMode: key.NewBinding(
		key.WithKeys("1"),
	),
	CommitsMode: key.NewBinding(
		key.WithKeys("2"),
	),
	PRMode: key.NewBinding(
		key.WithKeys("3"),
	),
	// FocusLeft / FocusRight (shift+h, shift+l, shift+arrows): the
	// pre-cursor bindings for h/l. Promoted to capital-letter keys so
	// h/l/left/right are free for cursor motion in MainFocus.
	FocusLeft: key.NewBinding(
		key.WithKeys("H", "shift+left"),
	),
	FocusRight: key.NewBinding(
		key.WithKeys("L", "shift+right"),
	),
	// CursorLeft / CursorRight: character-grained cursor motion in
	// MainFocus. Falls back to FocusLeft semantics (focus toggle or
	// horizontal scroll) when in sidebar focus — see handleKey.
	CursorLeft: key.NewBinding(
		key.WithKeys("h", "left"),
	),
	CursorRight: key.NewBinding(
		key.WithKeys("l", "right"),
	),
	FocusSidebar: key.NewBinding(
		key.WithKeys(","),
	),
	FocusMain: key.NewBinding(
		key.WithKeys("."),
	),
	FocusToggle: key.NewBinding(
		key.WithKeys("tab"),
	),
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "shift+space"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "space"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
	),
	GoTop: key.NewBinding(
		key.WithKeys("g", "home"),
	),
	GoBottom: key.NewBinding(
		key.WithKeys("G", "end"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
	),
	SidebarGrow: key.NewBinding(
		key.WithKeys("+", "="),
	),
	SidebarShrink: key.NewBinding(
		key.WithKeys("-"),
	),
	SearchNext: key.NewBinding(
		key.WithKeys("n"),
	),
	SearchPrev: key.NewBinding(
		key.WithKeys("p", "N"),
	),
	ToggleIgnored: key.NewBinding(
		key.WithKeys("i"),
	),
	ToggleSidebar: key.NewBinding(
		key.WithKeys("f"),
	),
	ToggleWrap: key.NewBinding(
		key.WithKeys("w"),
	),
	ToggleLineNums: key.NewBinding(
		key.WithKeys("n"),
	),
	ToggleRemoved: key.NewBinding(
		key.WithKeys("D"),
	),
	NextDiff: key.NewBinding(
		key.WithKeys("J", "shift+down"),
	),
	PrevDiff: key.NewBinding(
		key.WithKeys("K", "shift+up"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
	),
	NextLeaf: key.NewBinding(
		key.WithKeys("N"), // Shift+N
	),
	PrevLeaf: key.NewBinding(
		key.WithKeys("P"), // Shift+P
	),
	YankPath: key.NewBinding(
		key.WithKeys("y"),
	),
	PRBrowse: key.NewBinding(
		key.WithKeys("o"),
	),
	// Visual mode: v for stream (char-grained) selection, V for line
	// selection, Esc dismisses. Vim convention. y (YankPath) does
	// double duty — yanks the selection text in visual mode and the
	// path otherwise.
	VisualStream: key.NewBinding(
		key.WithKeys("v"),
	),
	VisualLine: key.NewBinding(
		key.WithKeys("V"),
	),
	VisualDismiss: key.NewBinding(
		key.WithKeys("esc"),
	),
	ScopeExtendBack: key.NewBinding(
		key.WithKeys("]"),
	),
	ScopeContractForward: key.NewBinding(
		key.WithKeys("["),
	),
	ScopeReset: key.NewBinding(
		key.WithKeys("\\"),
	),
}
