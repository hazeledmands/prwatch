// Package editor turns `$EDITOR` into an argv.
//
// It exists so the two things PROMPT.md's "opening an editor" section
// specifies — how each editor takes a line number, and whether it runs in the
// terminal — live in one table that can be exercised without a Model, a
// viewport, or a subprocess. The result is argv, never a shell string: the
// command package takes a program and arguments directly, so no quoting rule
// has to be guessed and no shell sits between prwatch and the editor.
package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// LineStyle names how an editor is told which line to open at. The values are
// strings so a failing table test prints the style rather than an integer.
type LineStyle string

const (
	// LinePlus passes `+N` ahead of the path: `vim +42 file.go`.
	LinePlus LineStyle = "+N"
	// LineColon appends the line to the path: `zed file.go:42`.
	LineColon LineStyle = "file:N"
	// LineGoto passes `--goto` and a colon-suffixed path: `code --goto file.go:42`.
	LineGoto LineStyle = "--goto file:N"
	// LineFlag passes `--line N` ahead of the path: `goland --line 42 file.go`.
	LineFlag LineStyle = "--line N"
)

// Preset is what prwatch knows about one editor.
type Preset struct {
	// Line is how this editor takes a line number.
	Line LineStyle
	// Terminal reports whether the editor runs in the terminal, which is what
	// decides whether the TUI suspends for it. A GUI launcher returns as soon
	// as the running application has been signalled, so suspending for one
	// would blank and redraw the screen for nothing.
	Terminal bool
	// Project passes the repository root ahead of the file, so the file opens
	// inside the project rather than attaching to whichever window the editor
	// happened to have focused last.
	//
	// Only set it for an editor whose CLI takes a list of paths where one may
	// be a directory and another a file with a position — zed's argument is
	// literally `[PATHS_WITH_POSITION]...`. It is meaningless for terminal
	// editors, which would simply open the root as a second file, and it is
	// deliberately off for the JetBrains launchers: they take a single path,
	// and handing them the repo root makes them write an `.idea` directory
	// into the user's repo.
	//
	// It composes only with the styles that put the path in positional
	// arguments (LineColon, and the no-line case). LineGoto and LineFlag put
	// a flag between the program and the path, so a leading root would land
	// on the wrong side of it; TestPresetTableProjectIsPositionalOnly holds
	// that line.
	Project bool
	// NoGuard omits the `--` end-of-options guard before the path.
	//
	// The guard is the default because without it a path beginning with `-`
	// is read as a flag, and because lazygit ships `--` on every preset it
	// has — including `code --goto --` — across a large user base. Only the
	// JetBrains launchers set NoGuard: they forward argv to a JVM argument
	// parser lazygit has no preset for and whose `--` handling is unverified.
	NoGuard bool
}

// presets is the editor table.
//
// Adapted from lazygit's editor presets, which are MIT licensed:
// https://github.com/jesseduffield/lazygit/blob/master/pkg/config/editor_presets.go
var presets = map[string]Preset{
	// Terminal editors: `+N` ahead of the path.
	"vi":    {Line: LinePlus, Terminal: true},
	"vim":   {Line: LinePlus, Terminal: true},
	"nvim":  {Line: LinePlus, Terminal: true},
	"nano":  {Line: LinePlus, Terminal: true},
	"emacs": {Line: LinePlus, Terminal: true},
	"micro": {Line: LinePlus, Terminal: true},
	"kak":   {Line: LinePlus, Terminal: true},

	// Terminal editors: line appended to the path.
	"hx":    {Line: LineColon, Terminal: true},
	"helix": {Line: LineColon, Terminal: true},

	// GUI editors.
	"zed":    {Line: LineColon, Project: true},
	"subl":   {Line: LineColon},
	"code":   {Line: LineGoto},
	"bbedit": {Line: LinePlus},
	"xed":    {Line: LineFlag},

	// JetBrains launchers, all `--line N`.
	"idea":      {Line: LineFlag, NoGuard: true},
	"goland":    {Line: LineFlag, NoGuard: true},
	"pycharm":   {Line: LineFlag, NoGuard: true},
	"webstorm":  {Line: LineFlag, NoGuard: true},
	"clion":     {Line: LineFlag, NoGuard: true},
	"rubymine":  {Line: LineFlag, NoGuard: true},
	"phpstorm":  {Line: LineFlag, NoGuard: true},
	"rider":     {Line: LineFlag, NoGuard: true},
	"datagrip":  {Line: LineFlag, NoGuard: true},
	"rustrover": {Line: LineFlag, NoGuard: true},
	"studio":    {Line: LineFlag, NoGuard: true},
}

// fallback is what an unrecognized editor gets: assumed to be a terminal
// editor taking `+N`, with the default `--` guard.
var fallback = Preset{Line: LinePlus, Terminal: true}

// DefaultEditor is used when `$EDITOR` is unset or holds only whitespace.
const DefaultEditor = "vi"

// Lookup returns the preset for an editor name and whether one exists. The
// name is matched after Identify-style normalization is already done — pass
// `nvim`, not `/opt/homebrew/bin/nvim`.
func Lookup(name string) (Preset, bool) {
	p, ok := presets[strings.ToLower(name)]
	return p, ok
}

// Identify reduces the first word of `$EDITOR` to the name the preset table is
// keyed by: the basename, with a `.sh` or `.exe` suffix stripped, so
// `/opt/homebrew/bin/nvim` resolves to `nvim` and JetBrains' `goland.sh` to
// `goland`.
func Identify(word string) string {
	// Windows paths reach us as `$EDITOR` text, not as OS paths, so the
	// backslash separator has to be handled explicitly: filepath.Base on a
	// unix build would hand back the whole `C:\...\code.exe` string.
	if i := strings.LastIndexByte(word, '\\'); i >= 0 {
		word = word[i+1:]
	}
	base := filepath.Base(word)
	for _, suffix := range []string{".sh", ".exe"} {
		if len(base) > len(suffix) && strings.EqualFold(base[len(base)-len(suffix):], suffix) {
			return base[:len(base)-len(suffix)]
		}
	}
	return base
}

// Invocation is a resolved editor launch: the program, its arguments, and
// whether running it means suspending the TUI.
type Invocation struct {
	Name     string
	Args     []string
	Terminal bool
}

// Resolve turns the raw `$EDITOR` value into an Invocation opening file at
// line. A line of zero or less means "no line number known", and the line
// argument is dropped entirely rather than passed as `+0`.
//
// root is the repository root, and reaches the argv only for a preset that
// sets Project; an empty root is the same as not having one. Every other
// preset gets the file and nothing else.
//
// `$EDITOR` is split on whitespace: the first word is the program (kept
// verbatim, so an absolute path still works), and the rest are passed through
// as leading arguments ahead of anything the preset adds. No wait flag is ever
// synthesized, and for a GUI editor one the user wrote is dropped — see
// stripWaitFlags.
func Resolve(editorEnv, root, file string, line int) Invocation {
	words := strings.Fields(editorEnv)
	if len(words) == 0 {
		words = []string{DefaultEditor}
	}
	name := words[0]

	preset, ok := Lookup(Identify(name))
	if !ok {
		preset = fallback
	}

	args := append([]string{}, words[1:]...)
	if !preset.Terminal {
		args = stripWaitFlags(args)
	}
	args = append(args, preset.fileArgs(root, file, line)...)
	return Invocation{Name: name, Args: args, Terminal: preset.Terminal}
}

// fileArgs is the preset-specific tail of the argv: the repo root when the
// preset asks for one, the file, and the line number in whatever form this
// editor takes it.
func (p Preset) fileArgs(root, file string, line int) []string {
	// paths renders the positional part of the argv, guarded by `--` when the
	// preset asks for it. The guard covers every positional, so the root is
	// protected from a leading `-` the same way the file is.
	paths := func(ps ...string) []string {
		if p.NoGuard {
			return ps
		}
		return append([]string{"--"}, ps...)
	}
	// positional prefixes the root for a Project preset. It goes first so the
	// editor reads the directory as the project and the file as something to
	// open inside it.
	positional := func(last string) []string {
		if p.Project && root != "" {
			return paths(root, last)
		}
		return paths(last)
	}
	if line <= 0 {
		return positional(file)
	}
	withLine := fmt.Sprintf("%s:%d", file, line)
	switch p.Line {
	case LineColon:
		return positional(withLine)
	case LineGoto:
		return append([]string{"--goto"}, paths(withLine)...)
	case LineFlag:
		return append([]string{"--line", strconv.Itoa(line)}, paths(file)...)
	default: // LinePlus
		return append([]string{"+" + strconv.Itoa(line)}, paths(file)...)
	}
}

// stripWaitFlags removes `-w` and `--wait` from a GUI editor's arguments.
//
// A GUI launcher told to wait blocks until the window is closed, which would
// pin one prwatch goroutine to a window the user may leave open for hours.
// Dropping the flag costs nothing: the launcher returns as soon as the running
// application has been signalled, and the window outlives it regardless,
// because it belongs to that application and not to prwatch.
//
// Callers must apply this only to GUI editors. In a terminal editor the same
// spelling means something else entirely — `vim -w <file>` records keystrokes
// to a file — so stripping there would silently change the invocation.
//
// Matching is exact (case-insensitive): `--waitfor` and `-wait` are somebody
// else's flags and survive.
func stripWaitFlags(args []string) []string {
	out := args[:0:0]
	for _, a := range args {
		if strings.EqualFold(a, "-w") || strings.EqualFold(a, "--wait") {
			continue
		}
		out = append(out, a)
	}
	return out
}
