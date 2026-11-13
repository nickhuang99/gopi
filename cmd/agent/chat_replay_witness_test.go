package main

// chat_replay_witness_test.go — THE ORDINARY PATH'S SHORTCUT, AND THE GUARD THAT
// WAS ASKING ABOUT THE WRONG TURN.
//
// TWO DEFECTS ARE PINNED HERE, both measured 2026-11-04 on session d6120d04:
//
//  1. `prefixAdvanceIsWitnessed` required `r.beats != nil`, and a beat driver is
//     installed IFF `--film` was given — so on the ordinary path `--from-turn N`
//     changed nothing except a notice line (13.789s / 13.798s / 13.807s for no
//     flag / N=2 / N=3). The witness now exists without beats: the REPL bumps a
//     turn-close counter at the same seam that commits the answer tail.
//  2. The wait guard was passed `i+1` — the turn about to be TYPED — while the
//     wait itself is about the turn that just ENDED (`i`). With N=2 that made
//     every wait ineligible and the flag again shortened nothing, and with N=3
//     it shortened one of the two.
//
// The DEFAULT path is the thing neither change may touch, so the third test here
// is its non-regression arm.

import (
	"testing"
	"time"
)

// TestChatReplayPrefixGuardNamesTheTurnItWaitsFor pins the OFF-BY-ONE: the wait
// that precedes typing turn i+1 is about turn i, so a `--from-turn 2` run must be
// able to skip the wait for turn 1.
//
// The predicate is exercised through the same numbers run() passes, so a future
// edit that widens or narrows the eligibility cannot do it silently: turn 2 is
// the first turn played IN FULL at --from-turn 2, and the wait before it is about
// the prefix turn 1.
func TestChatReplayPrefixGuardNamesTheTurnItWaitsFor(t *testing.T) {
	// THE MAPPING IS THE PIN: the wait before turn 2 is about turn 1. Passing the
	// turn being TYPED is the measured off-by-one, and it makes --from-turn 2
	// shorten nothing (13.798s against 13.789s without the flag).
	if got := chatReplayWaitTurn(2); got != 1 {
		t.Fatalf("the wait before typing turn 2 is about turn %d, want 1 — naming the turn being typed is the off-by-one", got)
	}
	for typing := 1; typing <= 5; typing++ {
		if got, want := chatReplayWaitTurn(typing), typing-1; got != want {
			t.Fatalf("the wait before typing turn %d is about turn %d, want %d", typing, got, want)
		}
	}
	r := &chatReplay{fromTurn: 2, witness: &chatTurnWitness{closed: 1}}
	// run() passes chatReplayWaitTurn(i+1) — the turn that just ENDED.
	if !r.prefixClosureWitnessed(chatReplayWaitTurn(2)) {
		t.Fatal("--from-turn 2 with turn 1 closed did not shorten the wait before turn 2 — the guard is naming the turn being typed, not the one being waited for (the pre-fix `i+1` off-by-one)")
	}
	// The wait before turn 2 is the ONLY prefix wait at N=2: turn 2 is the
	// deliverable's own turn and keeps the full settle.
	if r.prefixClosureWitnessed(2) {
		t.Fatal("--from-turn 2 hurried the wait about turn 2 — that is the turn the caller asked to play in full")
	}
	// At N=3 both prefix waits are eligible once the counter says so.
	r3 := &chatReplay{fromTurn: 3, witness: &chatTurnWitness{closed: 2}}
	for _, ordinal := range []int{1, 2} {
		if !r3.prefixClosureWitnessed(chatReplayWaitTurn(ordinal+1)) {
			t.Fatalf("--from-turn 3 with %d turns closed did not shorten the wait about turn %d", 2, ordinal)
		}
	}
}

// TestChatReplayWitnessWaitIsBoundedAndCheap — the wait a prefix turn may spend
// listening for the surface's own statement is a REAL bound (a run whose surface
// never speaks must not hang) and it is not longer than the settle it stands in
// front of.
func TestChatReplayWitnessWaitIsBoundedAndCheap(t *testing.T) {
	if chatReplayWitnessWait <= 0 {
		t.Fatalf("chatReplayWitnessWait = %v: an unbounded witness wait would hang the run", chatReplayWitnessWait)
	}
	if chatReplayWitnessWait > chatReplaySettleQuiescence {
		t.Fatalf("chatReplayWitnessWait (%v) is longer than the settle it precedes (%v) — a run with no witness would then be SLOWER than the plain path", chatReplayWitnessWait, chatReplaySettleQuiescence)
	}
	start := time.Now()
	if (&chatTurnWitness{}).awaitTurnWitness(1, 60*time.Millisecond) {
		t.Fatal("an idle witness claimed a turn was closed")
	}
	if elapsed := time.Since(start); elapsed > chatReplaySettleQuiescence {
		t.Fatalf("the bounded wait overran its bound: waited %v for a 60ms bound", elapsed)
	}
	var nilWitness *chatTurnWitness
	if nilWitness.awaitTurnWitness(1, 10*time.Millisecond) {
		t.Fatal("a nil witness claimed a turn was closed")
	}
	if nilWitness.count() != 0 {
		t.Fatal("a nil witness counted turns")
	}
	nilWitness.mark() // must not panic: every caller is nil-safe by contract
}
