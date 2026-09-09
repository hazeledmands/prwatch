package editor

import (
	"os/exec"
	"slices"
	"strings"
)

// Entry is one editor as the picker shows it: PROMPT.md's "choosing an editor".
type Entry struct {
	// Name is the command, and the key the preset table is keyed by.
	Name string
	// Preset is how this editor takes a line number and whether it runs in
	// the terminal — the same preset Resolve would apply.
	Preset Preset
	// Default reports whether this is the editor `$EDITOR` names, the one
	// `confirm` would launch. Only Available sets it; List cannot, because
	// nothing in the table knows what `$EDITOR` holds.
	Default bool
}

// List returns every editor with a preset, alphabetically.
//
// The preset table is a map, so it has no order of its own and cannot be
// rendered directly. A fresh slice is built per call: the picker sorts and
// annotates what it gets back, and a shared backing array would carry those
// edits into the next caller.
func List() []Entry {
	entries := make([]Entry, 0, len(presets))
	for name, preset := range presets {
		entries = append(entries, Entry{Name: name, Preset: preset})
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return entries
}

// Available returns the editors the picker should offer for a given `$EDITOR`,
// per PROMPT.md's "choosing an editor": the presets whose command resolves,
// plus `$EDITOR`'s own editor whether or not it does.
//
// lookPath reports whether a command resolves; a nil one means exec.LookPath.
// That is the same lookup exec.CommandContext performs when internal/command
// spawns the editor, so an entry this filter drops is one whose launch would
// have failed — the filter is the launch's own answer, not a guess about it.
//
// If nothing resolves, the full table comes back instead of an empty screen.
// A launch may then fail, which PROMPT.md's "failures" already covers.
func Available(editorEnv string, lookPath func(string) (string, error)) []Entry {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	def := DefaultEditor
	if words := strings.Fields(editorEnv); len(words) > 0 {
		def = Identify(words[0])
	}

	all := List()
	// `$EDITOR` may name an editor with no preset. It is still the editor the
	// app would launch, so the list would be lying if it left it out; it
	// carries the fallback preset Resolve would give it.
	if _, known := Lookup(def); !known {
		all = append(all, Entry{Name: def, Preset: fallback})
		slices.SortFunc(all, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	}

	entries := make([]Entry, 0, len(all))
	for _, e := range all {
		e.Default = e.Name == def
		if !e.Default {
			if _, err := lookPath(e.Name); err != nil {
				continue
			}
		}
		entries = append(entries, e)
	}

	// Nothing but the exempt default survived: offer everything rather than a
	// one-row list that only looks like a choice.
	if len(entries) <= 1 {
		for i := range all {
			all[i].Default = all[i].Name == def
		}
		return all
	}
	return entries
}
