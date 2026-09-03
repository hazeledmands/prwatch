package ui

import (
	"testing"

	"pgregory.net/rapid"
)

// A single trigger dispatches; the gate is then in flight with nothing pending.
func TestLoadGate_FirstTriggerDispatches(t *testing.T) {
	var g loadGate
	if !g.Begin(false) {
		t.Fatal("first Begin should dispatch")
	}
	if !g.InFlight() {
		t.Fatal("gate should be in flight after Begin")
	}
	if g.Pending() {
		t.Fatal("nothing should be pending after a single Begin")
	}
}

// N triggers arriving while a load is in flight coalesce into exactly one
// trailing dispatch — this is the whole point of the gate. Three fs watchers
// plus a tick used to mean four concurrent `git` process trees.
func TestLoadGate_CoalescesTriggersWhileInFlight(t *testing.T) {
	var g loadGate
	if !g.Begin(false) {
		t.Fatal("first Begin should dispatch")
	}
	for i := 0; i < 5; i++ {
		if g.Begin(false) {
			t.Fatalf("Begin %d while in flight should not dispatch", i)
		}
	}
	// First result: releases the one trailing load.
	withPR, again := g.Done()
	if !again {
		t.Fatal("Done should release a trailing load")
	}
	if withPR {
		t.Fatal("trailing load should be local-only")
	}
	if !g.InFlight() {
		t.Fatal("trailing dispatch leaves the gate in flight")
	}
	// Second result: nothing left.
	if _, again := g.Done(); again {
		t.Fatal("Done should not release a second trailing load")
	}
	if g.InFlight() || g.Pending() {
		t.Fatal("gate should be fully drained")
	}
}

// A suppressed PR-inclusive trigger must not be downgraded to a local-only
// trailing load: the PR half would silently never be fetched.
func TestLoadGate_TrailingLoadKeepsStrongestFlavor(t *testing.T) {
	var g loadGate
	g.Begin(false)
	g.Begin(false)
	g.Begin(true)
	g.Begin(false)
	withPR, again := g.Done()
	if !again {
		t.Fatal("expected a trailing load")
	}
	if !withPR {
		t.Fatal("trailing load must be PR-inclusive when any suppressed trigger was")
	}
}

// Done on an idle gate is a no-op. Tests hand-build gitDataMsg values that
// never came from a tracked dispatch, and those must not conjure a load.
func TestLoadGate_DoneWhenIdleIsNoop(t *testing.T) {
	var g loadGate
	if _, again := g.Done(); again {
		t.Fatal("idle Done should not dispatch")
	}
	if g.InFlight() || g.Pending() {
		t.Fatal("idle Done should leave the gate idle")
	}
}

// Property: never two loads in flight, the pending bit is idempotent under
// repeated triggers, and any trigger sequence drains to a fully idle gate once
// results stop arriving.
func TestProperty_LoadGate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var g loadGate
		// dispatched counts loads actually handed to bubbletea; results counts
		// gitDataMsg values fed back. dispatched-results is the number of loads
		// currently in flight and must never exceed 1.
		dispatched, results := 0, 0
		wantPR := false

		n := rapid.IntRange(1, 40).Draw(t, "steps")
		for i := 0; i < n; i++ {
			switch rapid.SampledFrom([]string{"trigger", "result"}).Draw(t, "op") {
			case "trigger":
				withPR := rapid.Bool().Draw(t, "withPR")
				if g.Begin(withPR) {
					dispatched++
				} else if withPR {
					wantPR = true
				}
			case "result":
				if dispatched == results {
					// No load outstanding; a stray result is a no-op.
					if _, again := g.Done(); again {
						t.Fatal("idle Done dispatched")
					}
					continue
				}
				results++
				pr, again := g.Done()
				if again {
					if wantPR && !pr {
						t.Fatal("trailing load lost the PR flavor")
					}
					wantPR = false
					dispatched++
				}
			}
			if dispatched-results < 0 || dispatched-results > 1 {
				t.Fatalf("in-flight loads = %d, want 0 or 1", dispatched-results)
			}
			if g.InFlight() != (dispatched-results == 1) {
				t.Fatalf("InFlight()=%v but %d loads outstanding", g.InFlight(), dispatched-results)
			}
		}

		// Drain: feeding results until nothing is outstanding must terminate
		// with an idle gate, never a self-sustaining trailing load.
		for guard := 0; dispatched > results; guard++ {
			if guard > n+2 {
				t.Fatal("gate failed to drain")
			}
			results++
			if _, again := g.Done(); again {
				dispatched++
			}
		}
		if g.InFlight() || g.Pending() {
			t.Fatalf("drained gate not idle: inFlight=%v pending=%v", g.InFlight(), g.Pending())
		}
	})
}

// Property: repeated triggers while in flight are idempotent — the gate's
// observable state after k>=1 suppressed triggers of the same flavor is the
// same for every k.
func TestProperty_LoadGateTriggerIdempotence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		withPR := rapid.Bool().Draw(t, "withPR")
		k := rapid.IntRange(1, 20).Draw(t, "k")

		var one, many loadGate
		one.Begin(false)
		many.Begin(false)
		one.Begin(withPR)
		for i := 0; i < k; i++ {
			many.Begin(withPR)
		}
		if one != many {
			t.Fatalf("k=%d diverged from k=1: %+v vs %+v", k, many, one)
		}
	})
}
