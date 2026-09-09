package editor

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestIdentify pins the name-normalization rule from PROMPT.md's "opening an
// editor": basename of the first word, with a `.sh` or `.exe` suffix stripped.
func TestIdentify(t *testing.T) {
	tests := []struct {
		word string
		want string
	}{
		{"nvim", "nvim"},
		{"/opt/homebrew/bin/nvim", "nvim"},
		{"goland.sh", "goland"},
		{"/Applications/GoLand.app/Contents/MacOS/goland.sh", "goland"},
		{"code.exe", "code"},
		{`C:\Program Files\Microsoft VS Code\code.exe`, "code"},
		{"./vim", "vim"},
		{"emacs", "emacs"},
	}
	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			if got := Identify(tt.word); got != tt.want {
				t.Errorf("Identify(%q) = %q, want %q", tt.word, got, tt.want)
			}
		})
	}
}

// TestPresetMatrix walks every preset named in PROMPT.md and asserts the argv
// its line-number form produces, plus whether it suspends the TUI.
func TestPresetMatrix(t *testing.T) {
	const file = "pkg/thing.go"
	const line = 42

	tests := []struct {
		editor   string
		want     []string // argv after the program name
		terminal bool
	}{
		// +N ahead of the path, terminal.
		{"vi", []string{"+42", "--", file}, true},
		{"vim", []string{"+42", "--", file}, true},
		{"nvim", []string{"+42", "--", file}, true},
		{"nano", []string{"+42", "--", file}, true},
		{"emacs", []string{"+42", "--", file}, true},
		{"micro", []string{"+42", "--", file}, true},
		{"kak", []string{"+42", "--", file}, true},

		// appended to the path, terminal.
		{"hx", []string{"--", file + ":42"}, true},
		{"helix", []string{"--", file + ":42"}, true},

		// appended to the path, GUI.
		{"zed", []string{"--", file + ":42"}, false},
		{"subl", []string{"--", file + ":42"}, false},

		// --goto, GUI.
		{"code", []string{"--goto", "--", file + ":42"}, false},

		// +N, GUI.
		{"bbedit", []string{"+42", "--", file}, false},

		// --line N ahead of the path, GUI. No `--`: the jetbrains launchers
		// forward argv to a JVM parser not known to accept it.
		{"xed", []string{"--line", "42", "--", file}, false},
		{"idea", []string{"--line", "42", file}, false},
		{"goland", []string{"--line", "42", file}, false},
		{"pycharm", []string{"--line", "42", file}, false},
		{"webstorm", []string{"--line", "42", file}, false},
		{"clion", []string{"--line", "42", file}, false},
		{"rubymine", []string{"--line", "42", file}, false},
		{"phpstorm", []string{"--line", "42", file}, false},
		{"rider", []string{"--line", "42", file}, false},
		{"datagrip", []string{"--line", "42", file}, false},
		{"rustrover", []string{"--line", "42", file}, false},
		{"studio", []string{"--line", "42", file}, false},
	}

	for _, tt := range tests {
		t.Run(tt.editor, func(t *testing.T) {
			if _, ok := Lookup(tt.editor); !ok {
				t.Fatalf("%s has no preset", tt.editor)
			}
			got := Resolve(tt.editor, "", file, line)
			if got.Name != tt.editor {
				t.Errorf("Name = %q, want %q", got.Name, tt.editor)
			}
			if !slices.Equal(got.Args, tt.want) {
				t.Errorf("Args = %v, want %v", got.Args, tt.want)
			}
			if got.Terminal != tt.terminal {
				t.Errorf("Terminal = %v, want %v", got.Terminal, tt.terminal)
			}
		})
	}
}

// TestResolve covers the $EDITOR parsing rules and the unrecognized-editor
// fallback.
func TestResolve(t *testing.T) {
	const file = "a/b.go"

	tests := []struct {
		name     string
		env      string
		line     int
		wantName string
		wantArgs []string
		terminal bool
	}{
		{
			name:     "unset EDITOR falls back to vi",
			env:      "",
			line:     7,
			wantName: "vi",
			wantArgs: []string{"+7", "--", file},
			terminal: true,
		},
		{
			name:     "whitespace-only EDITOR falls back to vi",
			env:      "   \t ",
			line:     7,
			wantName: "vi",
			wantArgs: []string{"+7", "--", file},
			terminal: true,
		},
		{
			name:     "GUI wait flag is stripped",
			env:      "code -w",
			line:     3,
			wantName: "code",
			wantArgs: []string{"--goto", "--", file + ":3"},
			terminal: false,
		},
		{
			name:     "non-wait extra words keep their order",
			env:      "code -w --new-window",
			line:     3,
			wantName: "code",
			wantArgs: []string{"--new-window", "--goto", "--", file + ":3"},
			terminal: false,
		},
		{
			name:     "absolute path keeps the path but resolves the preset",
			env:      "/opt/homebrew/bin/nvim",
			line:     11,
			wantName: "/opt/homebrew/bin/nvim",
			wantArgs: []string{"+11", "--", file},
			terminal: true,
		},
		{
			name:     ".sh suffix resolves the jetbrains preset",
			env:      "/usr/local/bin/goland.sh",
			line:     11,
			wantName: "/usr/local/bin/goland.sh",
			wantArgs: []string{"--line", "11", file},
			terminal: false,
		},
		{
			name:     ".exe suffix resolves the preset",
			env:      "code.exe",
			line:     11,
			wantName: "code.exe",
			wantArgs: []string{"--goto", "--", file + ":11"},
			terminal: false,
		},
		{
			name:     "unknown editor gets the terminal fallback",
			env:      "acme",
			line:     5,
			wantName: "acme",
			wantArgs: []string{"+5", "--", file},
			terminal: true,
		},
		{
			name:     "unknown editor with extra args",
			env:      "myed --frob",
			line:     5,
			wantName: "myed",
			wantArgs: []string{"--frob", "+5", "--", file},
			terminal: true,
		},
		{
			name:     "unknown editor with no line still guards the path",
			env:      "acme",
			line:     0,
			wantName: "acme",
			wantArgs: []string{"--", file},
			terminal: true,
		},
		{
			name:     "line 0 drops the line argument entirely",
			env:      "vim",
			line:     0,
			wantName: "vim",
			wantArgs: []string{"--", file},
			terminal: true,
		},
		{
			name:     "negative line drops the line argument entirely",
			env:      "code",
			line:     -1,
			wantName: "code",
			wantArgs: []string{"--", file},
			terminal: false,
		},
		{
			name:     "line 0 on a colon-suffix preset leaves the path bare",
			env:      "zed",
			line:     0,
			wantName: "zed",
			wantArgs: []string{"--", file},
			terminal: false,
		},
		{
			name:     "line 0 on a --line preset leaves the path bare",
			env:      "goland",
			line:     0,
			wantName: "goland",
			wantArgs: []string{file},
			terminal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.env, "", file, tt.line)
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if !slices.Equal(got.Args, tt.wantArgs) {
				t.Errorf("Args = %v, want %v", got.Args, tt.wantArgs)
			}
			if got.Terminal != tt.terminal {
				t.Errorf("Terminal = %v, want %v", got.Terminal, tt.terminal)
			}
		})
	}
}

// TestResolveNeverAddsWaitFlag pins PROMPT.md's "waiting" rule: prwatch never
// synthesizes a wait flag, whatever the preset.
func TestResolveNeverAddsWaitFlag(t *testing.T) {
	for name := range presets {
		got := Resolve(name, "", "f.go", 9)
		for _, a := range got.Args {
			if a == "-w" || a == "--wait" {
				t.Errorf("%s: Resolve added a wait flag: %v", name, got.Args)
			}
		}
	}
}

// TestResolveMentionsFileExactlyOnce is the argv invariant that matters for
// every preset shape: the path appears in exactly one argument (bare or with a
// `:N` suffix), and never as a second positional that would open a stray file.
func TestResolveMentionsFileExactlyOnce(t *testing.T) {
	const file = "some/dir/file.go"
	names := []string{"nope-not-an-editor"}
	for name := range presets {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		for _, line := range []int{0, 1, 12345} {
			t.Run(name+"/"+strconv.Itoa(line), func(t *testing.T) {
				got := Resolve(name, "", file, line)
				n := 0
				for _, a := range got.Args {
					if strings.Contains(a, file) {
						n++
					}
				}
				if n != 1 {
					t.Errorf("file appears in %d args, want 1: %v", n, got.Args)
				}
			})
		}
	}
}

// TestWaitFlagStripping pins PROMPT.md's "waiting" rule: `-w`/`--wait` are
// removed from a GUI editor's argv and left alone for a terminal one.
//
// The terminal half is the point of the rule, not an oversight. `vim -w
// <file>` records keystrokes to a file and `emacs -nw` suppresses the GUI —
// neither is a wait flag, so a blanket strip would silently change what the
// user asked for.
func TestWaitFlagStripping(t *testing.T) {
	const file = "a/b.go"

	tests := []struct {
		name string
		env  string
		want []string
	}{
		// GUI: stripped.
		{"code -w", "code -w", []string{"--goto", "--", file + ":9"}},
		{"code --wait", "code --wait", []string{"--goto", "--", file + ":9"}},
		{"zed -w", "zed -w", []string{"--", file + ":9"}},
		{"zed --wait", "zed --wait", []string{"--", file + ":9"}},
		{"subl --wait", "subl --wait", []string{"--", file + ":9"}},
		{"goland --wait", "goland --wait", []string{"--line", "9", file}},
		{"both forms at once", "code -w --wait", []string{"--goto", "--", file + ":9"}},
		{"strip keeps neighbours", "code -w -n --wait -r", []string{"-n", "-r", "--goto", "--", file + ":9"}},
		{"case-insensitive", "code -W", []string{"--goto", "--", file + ":9"}},

		// GUI: only exact matches go. A flag that merely starts with -w stays.
		{"-wait is not --wait", "code -wait", []string{"-wait", "--goto", "--", file + ":9"}},
		{"--waitfor stays", "code --waitfor", []string{"--waitfor", "--goto", "--", file + ":9"}},

		// Terminal: untouched, because -w means something else there.
		{"vim -w keeps its scriptout flag", "vim -w", []string{"-w", "+9", "--", file}},
		{"emacs -nw untouched", "emacs -nw", []string{"-nw", "+9", "--", file}},
		{"unknown editor is terminal, untouched", "myed -w", []string{"-w", "+9", "--", file}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.env, "", file, 9)
			if !slices.Equal(got.Args, tt.want) {
				t.Errorf("Resolve(%q).Args = %v, want %v", tt.env, got.Args, tt.want)
			}
		})
	}
}

// TestPresetTableProjectIsPositionalOnly holds the composition line the
// Project doc comment draws.
//
// Project prepends the repo root to the positional arguments. LineGoto and
// LineFlag put a flag between the program and the path (`--goto`, `--line N`),
// so a leading root would land on the wrong side of it and be read as the
// flag's operand. fileArgs therefore ignores Project for those two styles —
// which would make a preset that set both silently drop the root rather than
// fail. This test is what turns that into a build-time decision instead.
func TestPresetTableProjectIsPositionalOnly(t *testing.T) {
	for name, p := range presets {
		if !p.Project {
			continue
		}
		if p.Line == LineGoto || p.Line == LineFlag {
			t.Errorf("%s sets Project with line style %q; fileArgs cannot place "+
				"the root there — teach fileArgs the ordering for that style first",
				name, p.Line)
		}
		if p.Terminal {
			t.Errorf("%s is a terminal editor with Project set; the root would "+
				"just be a second file to open", name)
		}
	}
}

// TestResolveZedOpensTheProjectAndTheFile pins the invocation this whole
// preset field exists for. Without the root, `zed <file>` attaches the file to
// whichever window Zed had focused last rather than opening it in the project.
func TestResolveZedOpensTheProjectAndTheFile(t *testing.T) {
	got := Resolve("zed", "/repo", "pkg/thing.go", 42)
	want := []string{"--", "/repo", "pkg/thing.go:42"}
	if !slices.Equal(got.Args, want) {
		t.Errorf("Resolve zed = %v, want %v", got.Args, want)
	}

	// No line: still the project plus the file.
	got = Resolve("zed", "/repo", "pkg/thing.go", 0)
	want = []string{"--", "/repo", "pkg/thing.go"}
	if !slices.Equal(got.Args, want) {
		t.Errorf("Resolve zed (no line) = %v, want %v", got.Args, want)
	}

	// No root: unchanged from before this field existed.
	got = Resolve("zed", "", "pkg/thing.go", 42)
	want = []string{"--", "pkg/thing.go:42"}
	if !slices.Equal(got.Args, want) {
		t.Errorf("Resolve zed (no root) = %v, want %v", got.Args, want)
	}
}

// The root must not leak into an editor that did not ask for it — the
// JetBrains `.idea` case, and every terminal editor.
func TestResolveWithoutProjectIgnoresTheRoot(t *testing.T) {
	for _, name := range []string{"vim", "nvim", "hx", "code", "goland", "subl", "myeditor"} {
		withRoot := Resolve(name, "/repo", "pkg/thing.go", 42)
		without := Resolve(name, "", "pkg/thing.go", 42)
		if !slices.Equal(withRoot.Args, without.Args) {
			t.Errorf("%s: passing a root changed the argv from %v to %v",
				name, without.Args, withRoot.Args)
		}
	}
}
