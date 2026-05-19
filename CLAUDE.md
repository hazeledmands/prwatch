# CLAUDE.md

## Never delete rapid `.fail` files to make tests pass

`internal/ui/testdata/rapid/**/*.fail` files are seed-bound regression
replays for property-test failures. Each one captures a specific seed that
triggered an invariant violation. Deleting one destroys the ability to
reproduce the failure.

**Only delete a `.fail` file when:**
- The test signature has changed and rapid itself reports the file as
  "no longer valid", OR
- The underlying bug is fixed AND running the test no longer triggers a
  failure on that seed.

If a stress run (`./scripts/rapid` or a higher `PRWATCH_RAPID_CHECKS`)
surfaces a new failure, **commit the `.fail` file**. Then either fix the
bug or log it in `BUG_REPORTS.md`. Hiding a discovered failure by removing
its seed file means the bug will keep biting and we lose the seed we'd
need to reproduce it.

## All rapid runs go through `./scripts/rapid`

Any time you'd set `PRWATCH_RAPID_CHECKS`, use `./scripts/rapid` instead —
this applies to focused single-test runs at low counts, not just big
sweeps. The inline `PRWATCH_RAPID_CHECKS=N go test ...` form triggers a
permission prompt; the script does not.

The script forwards `-run`, `-v`, `-timeout`, etc. to `go test`:
- `./scripts/rapid` — default 200 iterations, all rapid tests
- `./scripts/rapid 50 -run TestProperty_TreeModeNavigation -v` — focused
- `./scripts/rapid 1000 -run TestRenderTitleRow` — heavy, single test

## Don't chain Bash commands with `;` or `&&`

Issue one command per Bash tool call. Even when each chained command would
be auto-allowed individually (`git log`, `git status`, `echo`, `cat`), the
permission validator evaluates the whole compound line and asks because no
allowlist pattern covers the chained form. Two consecutive Bash calls each
matching their own pattern is silent; one compound call prompts. Reach for
chaining only when the commands genuinely need to share shell state (e.g.
`cd dir && test ...`).

## Spec is in PROMPT.md

`PROMPT.md` is the product spec — source of truth, do not edit. Use:
- `INCONSISTENCIES.md` for spec ambiguities
- `BUG_REPORTS.md` for bugs (add a regression test before fixing)
- `PLAN.md` for in-progress work

## Encapsulate sub-feature state in its own type

When a feature has its own state — a search input, fetch cache, overlay,
state machine — put it in a peer file (`internal/ui/<feature>.go`) with
its own struct. Don't tack fields onto `Model`.

Rule of thumb: ≥3 fields moving together → struct. `Model` is a coordinator
of subsystems, not a bag of every piece of UI state. Its ~50-field shape
is what `REFACTORING.md` is working back from; don't re-grow it.

## Per-mode logic returns data; only the dispatcher mutates

A `switch m.mode { ... }` where each arm reads shared state and mutates
other shared state is a god-object pattern. Refactor so each arm is a pure
function returning what to show; one dispatcher applies the result.

This is how `updateSidebarItems` and `updateMainContent` grew to 300+-line
switches. Per-mode builders are testable in isolation; switch arms aren't.

## Pure methods belong as free functions

A method whose body only reads a handful of fields and returns a value
should be a free function taking those values as parameters. Receivers
signal "this mutates state" — if a method doesn't mutate, the receiver
lies and the function gets harder to test and harder to move.

## Extracted state machines get their own property tests

After encapsulating a state machine (overlay, tracker, cache, etc.) into a
type, write rapid property tests for its invariants in `<type>_test.go`.
End-to-end coverage in `invariant_test.go` is not a substitute — the whole
point of the encapsulation is unit-level testability.

Invariants worth looking for: index bounds (`idx ∈ [0, len)`), idempotence
(`f(f(x)) == f(x)`), reversibility (save→restore is identity), every state
has a dismiss path.
