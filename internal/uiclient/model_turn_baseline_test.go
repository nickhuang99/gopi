package uiclient

// model_turn_baseline_test.go — THE BAR'S SEEDED NUMBER, ASSERTED WHERE IT IS
// RENDERED.
//
// THE DEFECT (the operator's own report, 2026-10-27: "重启之后这个数字又归零了"):
// the status bar under `gopi ui` counted the turns the SURFACE ran, from 1,
// while `gopi session log`, `gopi session show` and `chats.turn_count` all
// reported the session's whole history — 16 on the session that was measured,
// with the resume notice one screen earlier promising "the next turn is turn 17".
//
// THE FIX is Config.TurnBaseline (run.go): the caller — the only layer that can
// read the ledger — hands the count over, and Run seeds the model with it. This
// file pins the half that belongs to THIS package: the seeded value is on the
// FIRST rendered status line, before any turn ran here, and a fresh session
// (baseline 0) renders the count it always did.

import (
	"strings"
	"testing"
)

// TestSeededTurnBaselineIsOnTheFirstStatusLine: the counter the operator reads is
// the session's, not the surface's.
func TestSeededTurnBaselineIsOnTheFirstStatusLine(t *testing.T) {
	const baseline = 16
	m, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	// The production seam, exactly as Run applies it (run.go): the caller's
	// baseline becomes the model's counter.
	m.turns = baseline

	line := m.statusLine()
	if !strings.Contains(line, "turns 16") {
		t.Fatalf("the status line does not carry the session's turn count %d — the bar restarts at 1 while every other surface says %d:\n%s",
			baseline, baseline, line)
	}

	// A FRESH session is unchanged: baseline 0 keeps the numbering the surface
	// always had, so this change is invisible to a run that continues nothing.
	fresh, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	if got, want := fresh.statusLine(), "turns 0"; !strings.Contains(got, want) {
		t.Fatalf("a fresh surface's status line lost %q:\n%s", want, got)
	}
}

// TestSeededTurnBaselineAdvancesWithTheSurfacesOwnTurns: the baseline is a
// STARTING POINT, not a replacement — the turns this surface runs add on top, so
// a resumed bar reads 17 after the operator's first new turn, which is the number
// the resume notice names.
func TestSeededTurnBaselineAdvancesWithTheSurfacesOwnTurns(t *testing.T) {
	const baseline = 16
	m, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	m.turns = baseline
	m.endTurn(nil) // one turn ran in THIS process
	if got, want := m.statusLine(), "turns 17"; !strings.Contains(got, want) {
		t.Fatalf("after one turn the bar does not read %q (baseline %d + 1):\n%s", want, baseline, got)
	}
	if m.turns != baseline+1 {
		t.Fatalf("the terminal count is %d, want %d (baseline + the turns this surface ran)", m.turns, baseline+1)
	}
}
