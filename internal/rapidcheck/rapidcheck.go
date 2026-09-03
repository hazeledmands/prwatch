// Package rapidcheck centralizes how many iterations rapid property tests run.
//
// It exists so the count has one definition. The plumbing started as an init()
// inside the ui package's test files, which meant scripts/rapid could only
// meaningfully sweep that one package: a property test anywhere else ignored
// PRWATCH_RAPID_CHECKS and ran at rapid's own default, so a sweep asking for
// 1000 iterations silently gave some packages 100. Every package holding
// property tests calls Apply from a test init instead, and a sweep means the
// same thing everywhere.
package rapidcheck

import (
	"flag"
	"os"
	"strconv"
)

// DefaultChecks is the iteration count used when PRWATCH_RAPID_CHECKS is unset.
//
// Deliberately low. Property tests run on every plain `go test ./...`, where
// their job is to catch an obvious regression quickly; the thorough sweeps that
// actually explore the space are what scripts/rapid is for.
const DefaultChecks = 5

// Apply sets rapid's iteration count from PRWATCH_RAPID_CHECKS, falling back to
// DefaultChecks. Call it from an init() in a test file of any package with
// property tests.
//
// An explicit -rapid.checks on the command line still wins: this runs during
// package initialization, and flag.Parse happens afterwards, overwriting it.
func Apply() {
	if n := os.Getenv("PRWATCH_RAPID_CHECKS"); n != "" {
		// Ignore a non-numeric value rather than failing the run: rapid would
		// reject it at parse time with a clearer message than anything this
		// could produce.
		if _, err := strconv.Atoi(n); err == nil {
			flag.Set("rapid.checks", n)
			return
		}
	}
	flag.Set("rapid.checks", strconv.Itoa(DefaultChecks))
}
