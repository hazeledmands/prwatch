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
	// DoubleDash puts `--` before the path. Only the unrecognized-editor
	// fallback sets it: with no preset to go on, the end-of-options guard is
	// the one thing that can be assumed safe, whereas adding it to a known
	// editor risks an argument its CLI does not accept.
	DoubleDash bool
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
	"zed":    {Line: LineColon},
	"subl":   {Line: LineColon},
	"code":   {Line: LineGoto},
	"bbedit": {Line: LinePlus},
	"xed":    {Line: LineFlag},

	// JetBrains launchers, all `--line N`.
	"idea":      {Line: LineFlag},
	"goland":    {Line: LineFlag},
	"pycharm":   {Line: LineFlag},
	"webstorm":  {Line: LineFlag},
	"clion":     {Line: LineFlag},
	"rubymine":  {Line: LineFlag},
	"phpstorm":  {Line: LineFlag},
	"rider":     {Line: LineFlag},
	"datagrip":  {Line: LineFlag},
	"rustrover": {Line: LineFlag},
	"studio":    {Line: LineFlag},
}

// fallback is what an unrecognized editor gets: assumed to be a terminal
// editor invoked as `<editor> +<line> -- <file>`.
var fallback = Preset{Line: LinePlus, Terminal: true, DoubleDash: true}

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
// `$EDITOR` is split on whitespace: the first word is the program (kept
// verbatim, so an absolute path still works), and the rest are passed through
// as leading arguments ahead of anything the preset adds, which is what makes
// `EDITOR="code -w"` work. No wait flag is ever synthesized here; one the user
// put in `$EDITOR` themselves rides through in those leading arguments.
func Resolve(editorEnv, file string, line int) Invocation {
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
	args = append(args, preset.fileArgs(file, line)...)
	return Invocation{Name: name, Args: args, Terminal: preset.Terminal}
}

// fileArgs is the preset-specific tail of the argv: the file, and the line
// number in whatever form this editor takes it.
func (p Preset) fileArgs(file string, line int) []string {
	// path renders the positional part of the argv, guarded by `--` when the
	// preset asks for it.
	path := func(f string) []string {
		if p.DoubleDash {
			return []string{"--", f}
		}
		return []string{f}
	}
	if line <= 0 {
		return path(file)
	}
	withLine := fmt.Sprintf("%s:%d", file, line)
	switch p.Line {
	case LineColon:
		return path(withLine)
	case LineGoto:
		return append([]string{"--goto"}, path(withLine)...)
	case LineFlag:
		return append([]string{"--line", strconv.Itoa(line)}, path(file)...)
	default: // LinePlus
		return append([]string{"+" + strconv.Itoa(line)}, path(file)...)
	}
}
