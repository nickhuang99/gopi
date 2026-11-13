package sessionevent

// The session round numbering (rounds.go). What these pin, in the order the
// design states them:
//
//  1. the base: the session's FIRST API call (JSONL `request` iteration 0) is
//     round 1 — the numbering `gopi session log <id> <round>` takes;
//  2. absence is a VALUE, not a round: RoundIndexUnknown is below the base, so
//     no reader can mistake "no round recorded" for round 0 or for a real round;
//  3. the inverse agrees with the forward map, and says so when there is no
//     index to give back.

import "testing"

// TestRoundNumberingBaseIsTheFirstCall: iteration 0 → round 1, and the sequence
// is contiguous — this is the offset every consumer shares.
func TestRoundNumberingBaseIsTheFirstCall(t *testing.T) {
	if got := RoundNumberOf(0); got != RoundIndexBase {
		t.Fatalf("RoundNumberOf(0) = %d, want the base %d (the session's first call IS round 1)", got, RoundIndexBase)
	}
	for iter := 0; iter < 5; iter++ {
		if got, want := RoundNumberOf(iter), iter+RoundIndexBase; got != want {
			t.Errorf("RoundNumberOf(%d) = %d, want %d", iter, got, want)
		}
	}
}

// TestRoundNumberOfRefusesToInventARound: a caller with no round to report (a
// negative iteration) gets the absence value, NOT round 0 and not round 1. This
// is the half that stops "absence silently becomes a real round" (Rule 15).
func TestRoundNumberOfRefusesToInventARound(t *testing.T) {
	for _, iter := range []int{-1, -7} {
		got := RoundNumberOf(iter)
		if got != RoundIndexUnknown {
			t.Errorf("RoundNumberOf(%d) = %d, want the absence value %d — never a fabricated round", iter, got, RoundIndexUnknown)
		}
		if RoundIndexRecorded(got) {
			t.Errorf("an unknown round (%d) reports as RECORDED", got)
		}
	}
}

// TestRoundIndexUnknownIsNotARound: the absence value sits BELOW the base, so
// it can never collide with a real round number.
func TestRoundIndexUnknownIsNotARecordedRound(t *testing.T) {
	if RoundIndexUnknown >= RoundIndexBase {
		t.Fatalf("RoundIndexUnknown (%d) must be below RoundIndexBase (%d), or absence would read as a round",
			RoundIndexUnknown, RoundIndexBase)
	}
	if RoundIndexRecorded(RoundIndexUnknown) {
		t.Error("RoundIndexRecorded(RoundIndexUnknown) = true — absence must not pass as a round")
	}
	if !RoundIndexRecorded(RoundIndexBase) {
		t.Error("RoundIndexRecorded(base) = false — the first round must count as recorded")
	}
}

// TestRoundIndexOfInverts: the reader that needs the JSONL iteration back gets
// it, and is TOLD (ok=false) when the value is not a recorded round instead of
// silently receiving -1.
func TestRoundIndexOfInvertsTheForwardMap(t *testing.T) {
	for iter := 0; iter < 5; iter++ {
		round := RoundNumberOf(iter)
		back, ok := RoundIndexOf(round)
		if !ok {
			t.Fatalf("RoundIndexOf(%d) reported 'not a round' for a value RoundNumberOf produced", round)
		}
		if back != iter {
			t.Errorf("RoundIndexOf(RoundNumberOf(%d)) = %d, want %d", iter, back, iter)
		}
	}
	if _, ok := RoundIndexOf(RoundIndexUnknown); ok {
		t.Error("RoundIndexOf(absence) reported ok=true — absence has no index to give back")
	}
	if _, ok := RoundIndexOf(0); ok {
		t.Error("RoundIndexOf(0) reported ok=true — 0 is the absence value, not a round")
	}
}
