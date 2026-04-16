# Command Factory: Centralized exec abstraction

## Problem

`os/exec` is called directly in multiple places across the codebase: `internal/git/git.go`, `internal/ui/model.go`, and their test files. This makes it impossible to mock external commands in tests, meaning tests can accidentally touch the system clipboard (`pbcopy`), open browsers (`open`), or hit external APIs (`gh`, `rwx`).

## Design

### New package: `internal/command`

A small package that owns all interaction with `os/exec`. No other file in the codebase (production or test) may import `os/exec`.

**`Command` interface:**

```go
type Command interface {
    Run() error
    SetDir(string)
    SetStdin(io.Reader)
    SetStdout(io.Writer)
    SetStderr(io.Writer)
}
```

This is a superset of bubbletea's `tea.ExecCommand` (which has Run, SetStdin, SetStdout, SetStderr). Our `Command` satisfies `tea.ExecCommand`, so it can be passed directly to `tea.Exec()`.

**`Factory` type:**

```go
type Factory func(name string, args ...string) Command
```

**`DefaultFactory`** wraps `exec.Command` in an adapter struct — the only place `os/exec` is used.

**`StubCommand` test helper:**

```go
func StubCommand(stdout string, err error) Command
```

Returns a `Command` that writes `stdout` to whatever writer is set via `SetStdout`, and returns `err` from `Run()`. All setter methods record their arguments for test assertions.

### Changes to `internal/git`

- Remove the `CmdRunner` type and `NewWithRunner` constructor.
- Add `cmdFactory command.Factory` field to `Git` struct.
- `New(dir)` wires in `command.DefaultFactory`.
- New constructor `NewWithFactory(dir string, factory command.Factory)` for test injection.
- `run(args ...string)` uses `g.cmdFactory("git", args...)`, sets Dir/Stdout/Stderr, calls Run, returns captured stdout.
- The `runCmd` field (used for `gh`/`rwx` commands) is replaced: methods that call non-git commands use `g.cmdFactory(name, args...)` directly with the same capture pattern.
- The `testing.Testing()` panic guard for `gh`/`rwx` moves into `DefaultFactory` (or a wrapper): when running under `go test`, `DefaultFactory` panics if asked to create a command for `gh` or `rwx`.
- The inline `exec.Command` at the untracked-file diff site (currently ~line 477) uses `g.cmdFactory` instead.
- Remove `os/exec` import from `git.go`.

### Changes to `internal/ui`

- Add `cmdFactory command.Factory` field to `Model` struct.
- `NewModel(dir, g)` wires in `command.DefaultFactory`.
- `openEditor()`: `m.cmdFactory(editor, args...)` + `SetDir` + pass to `tea.Exec()` (replaces `tea.ExecProcess`).
- `openInBrowser(url)`: `m.cmdFactory("open", url)` + pass to `tea.Exec()`.
- `copyToClipboard(text)`: becomes a method on `Model`. Uses `m.cmdFactory("pbcopy")` + `SetStdin` + `Run()`.
- Remove `os/exec` import from `model.go`.

### Changes to `main.go`

- `main.go` does not call `exec.Command` directly today, so no changes needed. It constructs `git.New(dir)` and `ui.NewModel(dir, g)`, both of which wire in `DefaultFactory` internally.

### Test migration

**`internal/git/git_test.go`:**

- `noGH(dir)` becomes a function returning `command.Factory` that delegates to `command.DefaultFactory` for non-gh/rwx commands and returns `command.StubCommand("", error)` for gh/rwx.
- `mockGHRunner(response, err)` becomes a factory that returns `StubCommand(response, err)` for `gh` and delegates to `DefaultFactory` for everything else.
- `mockCmdRunner(responses)` becomes a factory that looks up the command name in the response map, returns the appropriate `StubCommand`, and delegates to `DefaultFactory` for unmatched commands.
- Test setup code (`setupTestRepo`, etc.) that creates temp git repos uses `command.DefaultFactory` explicitly instead of importing `os/exec`.
- Remove `os/exec` import from `git_test.go`.

**`internal/ui/model_test.go`:**

- Tests that construct `Model` get `command.DefaultFactory` automatically (or a stub factory if they need to mock exec).
- Tests that construct `git.NewWithRunner` switch to `git.NewWithFactory`.
- Test setup code uses `command.DefaultFactory` instead of `exec.Command`.
- Remove `os/exec` import from `model_test.go`.

### Enforcement: import lint test

A test in `internal/command/` (e.g., `lint_test.go`) that uses `go/parser` to walk all `.go` files in the module. It parses each file and checks that `"os/exec"` appears in the imports only for files within `internal/command/`. If any other file imports `os/exec`, the test fails with the offending file path.

No exemptions for test files — the whole point is that tests must go through the Factory.

## Out of scope

- Changing how `GitDataSource` is mocked in UI tests.
- Adding a Factory injection parameter to `NewModel`'s public signature (it can be set via a package-level `SetFactory` or an unexported field for now, since UI tests are in the same package).
