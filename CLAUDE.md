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

## Heavy property runs use `./scripts/rapid`

Run `./scripts/rapid` for thorough property-test sweeps. Do not invoke
`PRWATCH_RAPID_CHECKS=N go test ...` directly.

## Spec is in PROMPT.md

`PROMPT.md` is the product spec — source of truth, do not edit. Use:
- `INCONSISTENCIES.md` for spec ambiguities
- `BUG_REPORTS.md` for bugs (add a regression test before fixing)
- `PLAN.md` for in-progress work
