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

// withDesc attaches a binding's help-listing description, the text the help
// overlay shows to the right of the key column.
//
// key.Help's Key field is deliberately left empty: the key column is generated
// from the binding's own Keys() by keyList, so there is no second copy of the
// key text that could drift from WithKeys above it. Any context a command is
// limited to ("(files mode)", "(main pane)", "(sidebar)") belongs in the
// description — it is the only place the help screen conveys it.
func withDesc(desc string) key.BindingOpt { return key.WithHelp("", desc) }

var keys = keyMap{
	QuitConfirm: key.NewBinding(
		key.WithKeys("q", "esc"),
		withDesc("Quit (confirm)"),
	),
	QuitImmediate: key.NewBinding(
		key.WithKeys("Q", "ctrl+c"),
		withDesc("Quit immediately"),
	),
	ToggleMode: key.NewBinding(
		key.WithKeys("m"),
		withDesc("Cycle mode (files → commits → pr)"),
	),
	// Mode switches: numeric keys only. The letter aliases (v/c/b) were
	// removed once v became visual-mode entry; c and b followed for
	// consistency. Cycle via [m].
	FilesMode: key.NewBinding(
		key.WithKeys("1"),
		withDesc("Files mode"),
	),
	CommitsMode: key.NewBinding(
		key.WithKeys("2"),
		withDesc("Commits mode"),
	),
	PRMode: key.NewBinding(
		key.WithKeys("3"),
		withDesc("PR mode (when PR exists)"),
	),
	// FocusLeft / FocusRight (shift+h, shift+l, shift+arrows): the
	// pre-cursor bindings for h/l. Promoted to capital-letter keys so
	// h/l/left/right are free for cursor motion in MainFocus.
	FocusLeft: key.NewBinding(
		key.WithKeys("H", "shift+left"),
		withDesc("Scroll left / switch to sidebar (when wrap off)"),
	),
	FocusRight: key.NewBinding(
		key.WithKeys("L", "shift+right"),
		withDesc("Scroll right (when wrap off)"),
	),
	// CursorLeft / CursorRight: character-grained cursor motion in
	// MainFocus. Falls back to FocusLeft semantics (focus toggle or
	// horizontal scroll) when in sidebar focus — see handleKey.
	CursorLeft: key.NewBinding(
		key.WithKeys("h", "left"),
		withDesc("Move cursor left (main pane)"),
	),
	CursorRight: key.NewBinding(
		key.WithKeys("l", "right"),
		withDesc("Move cursor right (main pane)"),
	),
	FocusSidebar: key.NewBinding(
		key.WithKeys(","),
		withDesc("Focus sidebar"),
	),
	FocusMain: key.NewBinding(
		key.WithKeys("."),
		withDesc("Focus main pane"),
	),
	FocusToggle: key.NewBinding(
		key.WithKeys("tab"),
		withDesc("Toggle focus (sidebar / main pane)"),
	),
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		withDesc("Move cursor up / select prev (sidebar)"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		withDesc("Move cursor down / select next (sidebar)"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "shift+space"),
		withDesc("Page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "space"),
		withDesc("Page down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		withDesc("Open file in $EDITOR / switch to main pane"),
	),
	GoTop: key.NewBinding(
		key.WithKeys("g", "home"),
		withDesc("Go to top"),
	),
	GoBottom: key.NewBinding(
		key.WithKeys("G", "end"),
		withDesc("Go to bottom"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		withDesc("Search (type to match, enter to confirm)"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		withDesc("Show this help (scroll with j/k/mouse)"),
	),
	SidebarGrow: key.NewBinding(
		key.WithKeys("+", "="),
		withDesc("Grow sidebar"),
	),
	SidebarShrink: key.NewBinding(
		key.WithKeys("-"),
		withDesc("Shrink sidebar"),
	),
	SearchNext: key.NewBinding(
		key.WithKeys("n"),
		withDesc("Next search result (after search confirmed)"),
	),
	SearchPrev: key.NewBinding(
		key.WithKeys("p", "N"),
		withDesc("Previous search result (after search confirmed)"),
	),
	ToggleIgnored: key.NewBinding(
		key.WithKeys("i"),
		withDesc("Toggle gitignored files (files mode)"),
	),
	ToggleSidebar: key.NewBinding(
		key.WithKeys("f"),
		withDesc("Toggle sidebar visibility"),
	),
	ToggleWrap: key.NewBinding(
		key.WithKeys("w"),
		withDesc("Toggle word wrap"),
	),
	ToggleLineNums: key.NewBinding(
		key.WithKeys("n"),
		withDesc("Toggle line numbers (files mode)"),
	),
	ToggleRemoved: key.NewBinding(
		key.WithKeys("D"),
		withDesc("Toggle removed lines in diff gutter (files mode)"),
	),
	NextDiff: key.NewBinding(
		key.WithKeys("J", "shift+down"),
		withDesc("Jump to next diff hunk (files mode)"),
	),
	PrevDiff: key.NewBinding(
		key.WithKeys("K", "shift+up"),
		withDesc("Jump to previous diff hunk (files mode)"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		withDesc("Refresh git state"),
	),
	NextLeaf: key.NewBinding(
		key.WithKeys("N"), // Shift+N
		withDesc("Jump to next leaf"),
	),
	PrevLeaf: key.NewBinding(
		key.WithKeys("P"), // Shift+P
		withDesc("Jump to previous leaf"),
	),
	YankPath: key.NewBinding(
		key.WithKeys("y"),
		withDesc("Yank selection (visual mode) / copy path (otherwise)"),
	),
	PRBrowse: key.NewBinding(
		key.WithKeys("o"),
		withDesc("Open the active PR in the browser"),
	),
	// Visual mode: v for stream (char-grained) selection, V for line
	// selection, Esc dismisses. Vim convention. y (YankPath) does
	// double duty — yanks the selection text in visual mode and the
	// path otherwise.
	VisualStream: key.NewBinding(
		key.WithKeys("v"),
		withDesc("Visual mode (character selection, main pane)"),
	),
	VisualLine: key.NewBinding(
		key.WithKeys("V"),
		withDesc("Visual mode (line selection, main pane)"),
	),
	VisualDismiss: key.NewBinding(
		key.WithKeys("esc"),
		withDesc("Dismiss visual selection"),
	),
	ScopeExtendBack: key.NewBinding(
		key.WithKeys("]"),
		withDesc("Extend commit-range scope backward"),
	),
	ScopeContractForward: key.NewBinding(
		key.WithKeys("["),
		withDesc("Contract commit-range scope toward working tree"),
	),
	ScopeReset: key.NewBinding(
		key.WithKeys("\\"),
		withDesc("Reset commit-range scope to default"),
	),
}

// helpSections is the help overlay's listing: which commands it shows, in what
// order, and how they group. Each inner slice renders as one block of rows,
// separated from its neighbours by a blank line; helpContentLines does the
// formatting and nothing else.
//
// This is the listing's only definition. A binding named here is documented
// with its own keys and its own description, so the three facts the help
// screen states about a command — what it is bound to, what it does, and that
// it exists at all — cannot disagree with the keymap.
//
// Every field of keyMap appears here exactly once;
// TestHelpListing_MatchesKeyMap enforces both directions and names the
// exclusion list a deliberate omission would have to join.
var helpSections = [][]key.Binding{
	{
		keys.ToggleMode,
		keys.FilesMode,
		keys.CommitsMode,
		keys.PRMode,
	},
	{
		keys.FocusLeft,
		keys.FocusRight,
		keys.FocusSidebar,
		keys.FocusMain,
		keys.FocusToggle,
	},
	{
		keys.Down,
		keys.Up,
		keys.CursorLeft,
		keys.CursorRight,
		keys.PageDown,
		keys.PageUp,
		keys.GoTop,
		keys.GoBottom,
	},
	{
		keys.SidebarGrow,
		keys.SidebarShrink,
		keys.ToggleSidebar,
	},
	{
		keys.ToggleWrap,
		keys.ToggleLineNums,
		keys.ToggleIgnored,
		keys.ToggleRemoved,
	},
	{
		keys.NextDiff,
		keys.PrevDiff,
		keys.NextLeaf,
		keys.PrevLeaf,
	},
	{
		keys.VisualStream,
		keys.VisualLine,
		keys.VisualDismiss,
		keys.YankPath,
	},
	{
		keys.Enter,
		keys.Search,
		keys.SearchNext,
		keys.SearchPrev,
	},
	{
		keys.Refresh,
		keys.PRBrowse,
		keys.Help,
	},
	{
		keys.ScopeExtendBack,
		keys.ScopeContractForward,
		keys.ScopeReset,
	},
	{
		keys.QuitConfirm,
		keys.QuitImmediate,
	},
}
