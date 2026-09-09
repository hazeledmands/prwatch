package editor

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/rapidcheck"
	"pgregory.net/rapid"
)

func init() { rapidcheck.Apply() }

// knownEditors is the preset table's key set, sorted so the generator draws
// from a stable domain.
func knownEditors() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// editorEnvGen produces plausible `$EDITOR` values: a known editor, a known
// editor behind a directory path and/or a `.sh`/`.exe` suffix, or a name that
// has no preset at all — optionally followed by pass-through words.
func editorEnvGen() *rapid.Generator[string] {
	word := rapid.SampledFrom([]string{"-w", "--wait", "--new-window", "-n", "--reuse-window"})
	return rapid.Custom(func(t *rapid.T) string {
		var name string
		if rapid.Bool().Draw(t, "known") {
			name = rapid.SampledFrom(knownEditors()).Draw(t, "editor")
		} else {
			name = rapid.SampledFrom([]string{"acme", "ed", "myeditor", "ne", "joe"}).Draw(t, "editor")
		}
		if rapid.Bool().Draw(t, "suffixed") {
			name += rapid.SampledFrom([]string{".sh", ".exe"}).Draw(t, "suffix")
		}
		if rapid.Bool().Draw(t, "absolute") {
			name = rapid.SampledFrom([]string{"/usr/bin/", "/opt/homebrew/bin/", "./"}).Draw(t, "dir") + name
		}
		words := append([]string{name}, rapid.SliceOfN(word, 0, 3).Draw(t, "extra")...)
		// The separator is what Resolve splits on, so vary it.
		sep := rapid.SampledFrom([]string{" ", "  ", "\t"}).Draw(t, "sep")
		return strings.Join(words, sep)
	})
}

// fileGen produces paths that can contain spaces — argv carries them fine, and
// a preset that colon-suffixes must not mangle them — but never a colon or a
// digit, so an assertion about where the line number shows up cannot be fooled
// by the path itself.
func fileGen() *rapid.Generator[string] {
	seg := rapid.StringMatching(`[a-z ._-]{1,8}`)
	return rapid.Custom(func(t *rapid.T) string {
		return strings.Join(rapid.SliceOfN(seg, 1, 4).Draw(t, "seg"), "/")
	})
}

// TestProperty_ResolveArgv asserts the invariants that hold for every preset,
// every `$EDITOR` spelling, and every line number. They are the properties the
// per-preset table test cannot state once: it enumerates expected argvs, so it
// can only be as complete as its rows.
func TestProperty_ResolveArgv(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		env := editorEnvGen().Draw(t, "env")
		file := fileGen().Draw(t, "file")
		line := rapid.IntRange(-3, 100000).Draw(t, "line")
		// A root that no generated file path can equal, so the "file appears
		// exactly once" count below cannot be fooled by the root matching it.
		root := rapid.SampledFrom([]string{"", "/REPO/ROOT"}).Draw(t, "root")

		got := Resolve(env, root, file, line)
		words := strings.Fields(env)

		// The program is the first word, verbatim — an absolute path stays an
		// absolute path even though the preset is looked up by basename.
		if got.Name != words[0] {
			t.Fatalf("Name = %q, want the first word %q", got.Name, words[0])
		}

		// Pass-through words keep their order and lead the argv, so a flag the
		// user put in $EDITOR is seen by the editor before the preset's args —
		// minus the wait flags, which a GUI editor does not get to keep.
		extra := words[1:]
		if !got.Terminal {
			extra = stripWaitFlags(extra)
		}
		if len(got.Args) < len(extra) || !slices.Equal(got.Args[:len(extra)], extra) {
			t.Fatalf("Args = %v, want it to start with the pass-through words %v", got.Args, extra)
		}
		preset := got.Args[len(extra):]

		p0, ok0 := Lookup(Identify(words[0]))
		if !ok0 {
			p0 = fallback
		}

		// The file is named exactly once, in the final argument. Matched
		// whole-arg (bare, or with the colon suffix a preset may add) rather
		// than by substring: a path like "-" is a substring of the "--" guard,
		// which would make a substring count lie.
		namesFile := func(a string) bool {
			return a == file || (line > 0 && a == file+":"+strconv.Itoa(line))
		}
		// The guard is an argv token, not a path, and a file can be named
		// "--". Drop the one guard the preset emits before counting, or
		// `Resolve("acme", "", "--", 0)` — a correct `[-- --]`, guard then
		// literal path — reads as the file appearing twice.
		counted := got.Args
		if !p0.NoGuard {
			if i := slices.Index(counted, "--"); i >= 0 {
				counted = slices.Concat(counted[:i], counted[i+1:])
			}
		}
		n := 0
		for _, a := range counted {
			if namesFile(a) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("file %q appears as %d args (guard excluded), want 1: %v", file, n, got.Args)
		}
		if !namesFile(got.Args[len(got.Args)-1]) {
			t.Fatalf("file %q is not the last arg: %v", file, got.Args)
		}

		// The repo root reaches the argv only for a Project preset, and only
		// when there is one. Everything else gets the file alone — the
		// JetBrains `.idea` case depends on that being airtight.
		wantRoot := p0.Project && root != ""
		gotRoot := slices.Contains(got.Args, root) && root != ""
		if gotRoot != wantRoot {
			t.Fatalf("root %q present=%v, want %v for %+v: %v", root, gotRoot, wantRoot, p0, got.Args)
		}
		// When it is there, it comes immediately before the file, so the
		// editor reads the directory as the project and the file as the thing
		// to open in it.
		if wantRoot && got.Args[len(got.Args)-2] != root {
			t.Fatalf("root %q is not immediately before the file: %v", root, got.Args)
		}

		// No wait flag is ever synthesized, and a GUI editor carries none at
		// all: waiting there would pin a goroutine to a window the user may
		// leave open for hours. A terminal editor keeps whatever the user
		// wrote, since `-w` means something else to it.
		scope := preset
		if !got.Terminal {
			scope = got.Args
		}
		for _, a := range scope {
			if a == "-w" || a == "--wait" {
				t.Fatalf("wait flag survived in a %s invocation: %v",
					map[bool]string{true: "terminal", false: "GUI"}[got.Terminal], got.Args)
			}
		}

		// The line number reaches the editor when there is one, and no `+0`
		// (or `--line 0`) is invented when there isn't.
		joined := strings.Join(preset, " ")
		if line > 0 {
			if !strings.Contains(joined, strconv.Itoa(line)) {
				t.Fatalf("line %d does not appear in %v", line, got.Args)
			}
		} else if strings.Contains(joined, strconv.Itoa(line)) {
			t.Fatalf("non-positive line %d leaked into %v", line, got.Args)
		}

		// Terminal is a property of the resolved preset, not of the spelling:
		// a path and a suffix must not change whether the TUI suspends.
		p, ok := Lookup(Identify(words[0]))
		if !ok {
			p = fallback
		}
		if got.Terminal != p.Terminal {
			t.Fatalf("Terminal = %v, want %v for preset %+v", got.Terminal, p.Terminal, p)
		}
	})
}
