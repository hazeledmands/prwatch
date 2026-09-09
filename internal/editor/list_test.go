package editor

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// resolveOnly builds a lookPath that succeeds for exactly the given names.
func resolveOnly(names ...string) func(string) (string, error) {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func resolveNothing(string) (string, error) { return "", exec.ErrNotFound }

func entryNames(entries []Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func TestList_CoversThePresetTable(t *testing.T) {
	got := entryNames(List())

	want := make([]string, 0, len(presets))
	for name := range presets {
		want = append(want, name)
	}
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("List() names = %v, want %v", got, want)
	}
}

func TestList_IsSortedAndFreeOfDuplicates(t *testing.T) {
	names := entryNames(List())
	if !slices.IsSorted(names) {
		t.Errorf("List() is not sorted: %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Errorf("List() repeats %q", names[i])
		}
	}
}

func TestList_PresetsAgreeWithLookup(t *testing.T) {
	for _, e := range List() {
		want, ok := Lookup(e.Name)
		if !ok {
			t.Errorf("List() offered %q, which Lookup does not know", e.Name)
			continue
		}
		if e.Preset != want {
			t.Errorf("%s: List() preset %+v, Lookup preset %+v", e.Name, e.Preset, want)
		}
	}
}

// The listing is rebuilt per call rather than shared, so a caller that sorts
// or annotates the slice cannot corrupt the next caller's copy.
func TestList_CallersGetIndependentSlices(t *testing.T) {
	first := List()
	if len(first) == 0 {
		t.Fatal("List() is empty")
	}
	first[0].Name = "clobbered"

	if second := List(); second[0].Name == "clobbered" {
		t.Error("List() handed two callers the same backing array")
	}
}

func TestList_MarksNothingAsDefault(t *testing.T) {
	// Which editor is the default depends on $EDITOR, which List knows
	// nothing about; only Available can answer that.
	for _, e := range List() {
		if e.Default {
			t.Errorf("List() marked %q as the default", e.Name)
		}
	}
}

func TestAvailable_FiltersToResolvableCommands(t *testing.T) {
	got := entryNames(Available("vim", resolveOnly("vim", "code", "hx")))
	want := []string{"code", "hx", "vim"}
	if !slices.Equal(got, want) {
		t.Errorf("Available() = %v, want %v", got, want)
	}
}

func TestAvailable_KeepsTheEditorEnvEvenWhenItDoesNotResolve(t *testing.T) {
	got := entryNames(Available("zed", resolveOnly("vim")))
	want := []string{"vim", "zed"}
	if !slices.Equal(got, want) {
		t.Errorf("Available() = %v, want %v (the $EDITOR editor must survive the filter)", got, want)
	}
}

// A `$EDITOR` with no preset is still the editor the app would launch, so the
// list would be lying if it left it out. It carries the same fallback preset
// Resolve would apply.
func TestAvailable_KeepsAnUnknownEditorEnv(t *testing.T) {
	entries := Available("/opt/bin/myeditor -w", resolveOnly("vim"))

	idx := slices.IndexFunc(entries, func(e Entry) bool { return e.Name == "myeditor" })
	if idx < 0 {
		t.Fatalf("Available() = %v, want it to include the unrecognized $EDITOR", entryNames(entries))
	}
	if got := entries[idx].Preset; got != fallback {
		t.Errorf("unrecognized $EDITOR preset = %+v, want the fallback %+v", got, fallback)
	}
	if !entries[idx].Default {
		t.Error("the unrecognized $EDITOR should still be marked as the default")
	}
}

func TestAvailable_FallsBackToTheFullTableWhenNothingResolves(t *testing.T) {
	got := entryNames(Available("vim", resolveNothing))

	// "vim" resolves via the $EDITOR exemption, so the interesting case is an
	// $EDITOR that is itself unresolvable: the list must not collapse to one
	// row that happens to be exempt.
	if len(got) <= 1 {
		t.Fatalf("Available() = %v, want the full table when nothing resolves", got)
	}
	if want := entryNames(List()); !slices.Equal(got, want) {
		t.Errorf("Available() = %v, want the full table %v", got, want)
	}
}

func TestAvailable_UnsetEditorEnvDefaultsToTheFallbackEditor(t *testing.T) {
	for _, env := range []string{"", "   ", "\t"} {
		entries := Available(env, resolveOnly("vim", "code"))
		def := defaultEntry(t, entries)
		if def.Name != DefaultEditor {
			t.Errorf("Available(%q) default = %q, want %q", env, def.Name, DefaultEditor)
		}
	}
}

func TestAvailable_MarksExactlyOneDefault(t *testing.T) {
	entries := Available("code --new-window", resolveOnly("vim", "code", "zed"))
	if def := defaultEntry(t, entries); def.Name != "code" {
		t.Errorf("default = %q, want code", def.Name)
	}
}

func defaultEntry(t *testing.T, entries []Entry) Entry {
	t.Helper()
	var found []Entry
	for _, e := range entries {
		if e.Default {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one default, got %d: %v", len(found), entryNames(found))
	}
	return found[0]
}

// resolvableGen draws a lookPath that resolves an arbitrary subset of the
// preset table, including the empty and complete subsets.
func resolvableGen(t *rapid.T) func(string) (string, error) {
	all := knownEditors()
	keep := make([]string, 0, len(all))
	for _, name := range all {
		if rapid.Bool().Draw(t, "resolves-"+name) {
			keep = append(keep, name)
		}
	}
	return resolveOnly(keep...)
}

func TestProperty_AvailableInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		env := editorEnvGen().Draw(t, "editor")
		entries := Available(env, resolvableGen(t))
		names := entryNames(entries)

		if len(entries) == 0 {
			t.Fatalf("Available(%q) returned nothing; the list is never empty", env)
		}
		if !slices.IsSorted(names) {
			t.Fatalf("Available(%q) = %v, not sorted", env, names)
		}
		for i := 1; i < len(names); i++ {
			if names[i] == names[i-1] {
				t.Fatalf("Available(%q) = %v, repeats %q", env, names, names[i])
			}
		}

		// Exactly one default, and it is the editor Resolve would launch.
		var defaults []string
		for _, e := range entries {
			if e.Default {
				defaults = append(defaults, e.Name)
			}
		}
		wantDefault := Identify(firstWord(env))
		if wantDefault == "" {
			wantDefault = DefaultEditor
		}
		if len(defaults) != 1 || defaults[0] != wantDefault {
			t.Fatalf("Available(%q) defaults = %v, want exactly [%s]", env, defaults, wantDefault)
		}

		// Every row is launchable: its preset is the one Resolve would use.
		for _, e := range entries {
			want, ok := Lookup(e.Name)
			if !ok {
				want = fallback
			}
			if e.Preset != want {
				t.Fatalf("Available(%q): %s carries preset %+v, want %+v", env, e.Name, e.Preset, want)
			}
		}
	})
}

func firstWord(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// A nil lookPath means exec.LookPath, so the UI does not have to wire it up.
func TestAvailable_NilLookPathUsesExecLookPath(t *testing.T) {
	got := entryNames(Available("vim", nil))
	want := entryNames(Available("vim", exec.LookPath))
	if !slices.Equal(got, want) {
		t.Errorf("Available with a nil lookPath = %v, want the exec.LookPath result %v", got, want)
	}
	if len(got) == 0 {
		t.Error("Available returned nothing; the list is never empty")
	}
}
