package main

import (
	"strings"
	"testing"
	"time"
)

// TestChatParseFromTurnArg — `--from-turn N` is the ONLY way a run states where
// in the recording it begins, so its refusals are the whole contract of the
// flag: a typo that fell back to "play from the start" would re-run the whole
// recording and look as if it had obeyed (Rule 15).
func TestChatParseFromTurnArg(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		next     string
		hasNext  bool
		wantN    int
		wantCon  bool
		wantErr  bool
		errMatch string
	}{
		{name: "spaced value", arg: chatFlagFromTurn, next: "3", hasNext: true, wantN: 3, wantCon: true},
		{name: "joined value", arg: chatFlagFromTurn + "=7", wantN: 7, wantCon: true},
		{name: "first turn", arg: chatFlagFromTurn + "=1", wantN: 1, wantCon: true},
		{name: "not this flag", arg: chatFlagReplay, next: "abc", hasNext: true, wantCon: false},
		{name: "prefix of another word", arg: chatFlagFromTurn + "s=3", wantCon: false},
		{name: "no value", arg: chatFlagFromTurn, hasNext: false, wantCon: true, wantErr: true,
			errMatch: chatReplayFromTurnNeedsValue},
		{name: "value is the next flag", arg: chatFlagFromTurn, next: chatFlagReplay, hasNext: true, wantCon: true, wantErr: true,
			errMatch: chatReplayFromTurnNeedsValue},
		{name: "empty joined value", arg: chatFlagFromTurn + "=", wantCon: true, wantErr: true,
			errMatch: chatReplayFromTurnNeedsValue},
		{name: "not a number", arg: chatFlagFromTurn + "=two", wantCon: true, wantErr: true, errMatch: "is not one"},
		{name: "zero names no turn", arg: chatFlagFromTurn + "=0", wantCon: true, wantErr: true, errMatch: "1-based"},
		{name: "negative names no turn", arg: chatFlagFromTurn + "=-2", wantCon: true, wantErr: true, errMatch: "1-based"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, consumed, err := chatParseFromTurnArg(c.arg, c.next, c.hasNext)
			if consumed != c.wantCon {
				t.Fatalf("consumed = %v, want %v", consumed, c.wantCon)
			}
			if c.wantErr {
				if err == nil {
					t.Fatalf("%q was accepted as turn %d — a value the flag cannot mean must be REFUSED by name, not coerced", c.arg, n)
				}
				if !strings.Contains(err.Error(), c.errMatch) {
					t.Fatalf("refusal %q does not name the reason (%q)", err.Error(), c.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if n != c.wantN {
				t.Fatalf("turn = %d, want %d", n, c.wantN)
			}
		})
	}
}

// TestChatReplayDefaultPathWaitIsUnchanged is the DEFAULT-PATH non-regression
// arm: with no --from-turn, every turn's closure is established by the SAME
// predicate and the SAME bound a plain replay used before this flag existed —
// the only thing `--from-turn` may remove is a wait whose condition a beat has
// already reported (prefixAdvanceIsWitnessed), and that guard is unreachable
// when fromTurn is 0.
func TestChatReplayDefaultPathWaitIsUnchanged(t *testing.T) {
	if got := chatReplaySettleQuiescence; got != 3*time.Second {
		t.Fatalf("the plain inter-turn quiet is %v; the default path's timing (and therefore its frames) is defined by this number", got)
	}
	// A plain replay waits: the witnessed-skip guard must be false for every
	// ordinal, including when a beat driver exists (--film runs have one).
	plain := &chatReplay{beats: &chatReplayBeatDriver{}}
	for _, ordinal := range []int{0, 1, 2, 3, 40} {
		if plain.prefixAdvanceIsWitnessed(ordinal) {
			t.Fatalf("with no --from-turn, turn %d skipped its wait — the default path's behaviour changed", ordinal)
		}
	}
}

// TestChatReplayPrefixShortcutNeedsAWitness pins the CONDITION guard: the
// prefix may skip its quiet wait ONLY when the surface itself reported the turn
// closed (a beat). A run with no beat driver keeps the wait, and no turn at or
// after the start point is ever eligible — so the shortcut can never reach the
// screen the caller asked for.
func TestChatReplayPrefixShortcutNeedsAWitness(t *testing.T) {
	if (&chatReplay{fromTurn: 3}).prefixAdvanceIsWitnessed(1) {
		t.Fatal("a run with NO beat driver skipped a prefix turn's wait — the wait is the only evidence that turn closed")
	}
	// THE BEAT IS ASKED, NOT ASSUMED (2026-11-04): a driver that EXISTS has not
	// thereby closed a turn, and the pre-fix predicate read `beats != nil` as
	// the fact. Two turns are closed here, so turns 1 and 2 are behind it.
	w := &chatReplay{fromTurn: 3, beats: &chatReplayBeatDriver{}}
	if w.prefixAdvanceIsWitnessed(1) {
		t.Fatal("a beat driver that has closed NO turn was read as a witness — its existence is not the fact the wait exists to establish")
	}
	w.beats.mu.Lock()
	w.beats.turnOrdinal = 2
	w.beats.mu.Unlock()
	if !w.prefixAdvanceIsWitnessed(1) {
		t.Fatal("prefix turn 1 with the beat of turn 1 on record still waited")
	}
	if !w.prefixAdvanceIsWitnessed(2) {
		t.Fatal("prefix turn 2 with the beat of turn 2 on record still waited")
	}
	for _, ordinal := range []int{3, 4, 5} {
		if w.prefixAdvanceIsWitnessed(ordinal) {
			t.Fatalf("turn %d is the DELIVERABLE's own turn and skipped its wait — only the prefix may be hurried", ordinal)
		}
	}
	if (&chatReplay{beats: &chatReplayBeatDriver{}}).prefixAdvanceIsWitnessed(1) {
		t.Fatal("no --from-turn: the shortcut was reachable on the DEFAULT path")
	}
	var nilRun *chatReplay
	if nilRun.prefixAdvanceIsWitnessed(1) {
		t.Fatal("a nil driver claimed a witness")
	}
}
