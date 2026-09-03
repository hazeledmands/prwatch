package git

import "github.com/hazeledmands/prwatch/internal/rapidcheck"

func init() {
	// This package holds property tests too, so it honours the same iteration
	// count as everywhere else; see internal/rapidcheck. Without this,
	// ./scripts/rapid would sweep the ui package at the requested count and
	// leave this one at rapid's built-in default.
	rapidcheck.Apply()
}
