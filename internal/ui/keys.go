package ui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	QuitConfirm    key.Binding
	QuitImmediate  key.Binding
	ToggleMode     key.Binding
	FileDiffMode   key.Binding
	FileViewMode   key.Binding
	CommitMode     key.Binding
	FocusLeft      key.Binding
	FocusRight     key.Binding
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
}

var keys = keyMap{
	QuitConfirm: key.NewBinding(
		key.WithKeys("q", "esc"),
	),
	QuitImmediate: key.NewBinding(
		key.WithKeys("Q", "ctrl+c"),
	),
	ToggleMode: key.NewBinding(
		key.WithKeys("space"),
	),
	FileDiffMode: key.NewBinding(
		key.WithKeys("d"),
	),
	FileViewMode: key.NewBinding(
		key.WithKeys("v"),
	),
	CommitMode: key.NewBinding(
		key.WithKeys("c"),
	),
	FocusLeft: key.NewBinding(
		key.WithKeys("h", "left"),
	),
	FocusRight: key.NewBinding(
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
		key.WithKeys("pgup"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
	),
	GoTop: key.NewBinding(
		key.WithKeys("g"),
	),
	GoBottom: key.NewBinding(
		key.WithKeys("G"),
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
		key.WithKeys("p"),
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
		key.WithKeys("J"),
	),
	PrevDiff: key.NewBinding(
		key.WithKeys("K"),
	),
}
