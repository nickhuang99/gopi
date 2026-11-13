package main

// cli_session_replay_turn_test.go — THE USER SURFACE HAS ONE UNIT: the TURN.
//
// The operator's ruling (2026-10-25): `steps` and the operator-facing `round` are
// gone from this sub-verb. `--turns` lists the turns, `--turn N` prints ONE turn
// in FULL (its request, its reasoning, its prose and EVERY tool call with its
// result), `--steps` is RETIRED BY NAME, and the round views survive only as
// INTERNAL/DEBUG addresses, marked as such in -h and in their own output.
//
// Every arm below runs the REAL sub-verb through runGopiCapture — the same path
// an operator's terminal takes — so a page or a listing that drifts away from the
// ruling fails here rather than in a hand-copied string.

import (
	"strings"
	"testing"
)

// TestSessionReplayTurnPrintsEveryToolCallOfTheTurn is the core assertion of
// `--turn N`: the static view of a turn carries EVERY tool call that turn ran,
// each with its result — the whole reason a turn is addressable and a round is
// not. The fixture's turn 1 holds one tool call (bash → "hi from the tool") and
// turn 2 holds none, so the two arms cannot be satisfied by one accident.
func TestSessionReplayTurnPrintsEveryToolCallOfTheTurn(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "1")
	if err != nil {
		t.Fatalf("--turn 1 failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"turn 1/",
		"PROMPT:",
		"a recorded question",
		"bash",                 // the tool call's name
		"hi from the tool",     // its RESULT
		"a recorded answer",    // the prose of that turn
		"weighing the options", // its reasoning, in full
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("--turn 1 does not print %q — a turn's view carries its request, its reasoning, its prose and its tool calls WITH RESULTS:\n%s", want, out)
		}
	}
	// The retired vocabulary is absent from a user-surface view, and so is a
	// round count: this is the turn address space.
	if strings.Contains(out, "steps") {
		t.Fatalf("`--turn N` prints the retired word `steps`:\n%s", out)
	}
	if strings.Contains(out, "round") {
		t.Fatalf("`--turn N` mentions a round — this is the user surface's address space:\n%s", out)
	}

	// Out of range is REFUSED BY NAME, never clamped to the last turn.
	if _, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "99"); err == nil {
		t.Fatalf("--turn 99 was accepted on a %d-turn session: an address out of range must be refused", replayTestTurnCount)
	}
	// The from-the-end spelling resolves inside the turn sequence.
	last, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "last")
	if err != nil {
		t.Fatalf("--turn last failed: %v\n%s", err, last)
	}
	if !strings.Contains(last, "turn 2/") {
		t.Fatalf("--turn last did not resolve to the LAST of %d turns:\n%s", replayTestTurnCount, last)
	}
}

// TestSessionReplayTurnsCountAgreesWithThePlayer pins that the listing and the
// playback report ONE length: `--turns`' headline number is what the player
// walks through (Material.PlayableUnits), read through the verb itself rather
// than typed here.
func TestSessionReplayTurnsCountAgreesWithThePlayer(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turns")
	if err != nil {
		t.Fatalf("--turns failed: %v\n%s", err, out)
	}
	if got := replayField(t, out, "turns"); got != replayTestTurnCount {
		t.Fatalf("--turns reports turns=%d, want %d:\n%s", got, replayTestTurnCount, out)
	}
	// The listing prints one row per turn — and the count of those rows IS the
	// number the headline states. A listing whose rows disagreed with its own
	// header would send a reader to a `--turn N` that does not exist.
	rows := strings.Count(out, "turn ")
	if rows < replayTestTurnCount {
		t.Fatalf("--turns lists %d turn row(s), want at least %d:\n%s", rows, replayTestTurnCount, out)
	}
	if playerTotal := replayStepTotal(t, home, id); playerTotal != replayTestTurnCount {
		t.Fatalf("the player walks %d unit(s) but the session has %d turn(s) — the listing and the playback must report one length",
			playerTotal, replayTestTurnCount)
	}
}

// TestSessionReplayStepsIsRetiredByName pins the retirement: `--steps` is
// REFUSED, with the new spelling in the message. It is not an alias.
func TestSessionReplayStepsIsRetiredByName(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--steps")
	if err == nil {
		t.Fatalf("`--steps` still works — the user surface has ONE unit and the retired spelling must be refused:\n%s", out)
	}
	if !strings.Contains(out, "--turns") || !strings.Contains(out, "--turn") {
		t.Fatalf("the refusal does not name the replacements (%q / %q):\n%s", "--turns", "--turn", out)
	}
}

// TestSessionReplayUserSurfaceNeverSaysSteps pins the vocabulary on every
// user-facing output: the -h page, the turn listing, a turn's full view and the
// internal round views. `steps` must appear nowhere, and a round may appear ONLY
// under the internal/debug wording.
func TestSessionReplayUserSurfaceNeverSaysSteps(t *testing.T) {
	home, id := replayTestSession(t)
	page, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, "-h")
	if err != nil {
		t.Fatalf("-h failed: %v\n%s", err, page)
	}
	if strings.Contains(page, "steps") {
		t.Fatalf("the -h page still says `steps`:\n%s", page)
	}
	// Every PARAGRAPH of the page that mentions a ROUND must be the internal/debug
	// wording — a reader must never meet the transport's unit unexplained. The
	// unit of the check is the paragraph, not the line: an option and its
	// indented continuation are one statement, and the marker may sit on either.
	if !strings.Contains(page, sessionReplayRoundsInternalMark) {
		t.Fatalf("the -h page carries no %q section — the round views must be introduced as internal/debug:\n%s",
			sessionReplayRoundsInternalMark, page)
	}
	for _, para := range strings.Split(page, "\n\n") {
		if !strings.Contains(strings.ToLower(para), "round") {
			continue
		}
		if !strings.Contains(strings.ToLower(para), "internal") && !strings.Contains(strings.ToLower(para), "debug") {
			t.Fatalf("this -h paragraph mentions a round without marking it internal/debug:\n%s", para)
		}
	}
	for _, args := range [][]string{
		{"--turns"},
		{"--turn", "1"},
		{"--rounds"},
		{"--round", "1"},
	} {
		argv := append([]string{CliVerbSession, sessionSubcmdReplay, id}, args...)
		out, err := runGopiCapture(t, home, argv...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		if strings.Contains(out, "steps") {
			t.Fatalf("%v prints the retired word `steps`:\n%s", args, out)
		}
	}
}
