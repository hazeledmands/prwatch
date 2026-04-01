package ui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	QuitConfirm   key.Binding
	QuitImmediate key.Binding
	ToggleMode    key.Binding
	FileMode      key.Binding
	CommitMode    key.Binding
	FocusLeft     key.Binding
	FocusRight    key.Binding
	Up            key.Binding
	Down          key.Binding
	PageUp        key.Binding
	PageDown      key.Binding
	Enter         key.Binding
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
	FileMode: key.NewBinding(
		key.WithKeys("f"),
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
}
