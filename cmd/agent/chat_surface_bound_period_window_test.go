package main

// chat_surface_bound_period_window_test.go — THE DETERMINISTIC GUARD for the defect
// the bound case's own whole-package run kept finding, in two different instruments.
//
// NO PTY, NO MOCK SERVER, NO TIMING. The defect was never a race in the product: it
// was a CLAIM READ OVER THE WRONG WINDOW, which is arithmetic over a record and can be
// tested as arithmetic. That is what this file does — it hands the case's own per-period
// readers a synthetic record of a complete TWO-PERIOD run and states the claim the case
// makes ("each period accepts exactly the bound and is refused exactly once") over that
// period's OWN window.
//
// WHY IT EXISTS AT ALL (Rule 5): the case's assertion is only reachable through a
// live `gopi chat` on a pty, driving a mock model to a spent ceiling — the expensive,
// timing-coupled road that let the defect survive. The arithmetic that went wrong is not
// live: it needs one ordered record and the question "which slice does a period's claim
// range over?". A guard that can ask it in microseconds is the difference between a
// defect that is caught when someone happens to run the package and one that is caught
// by `go test`.

import (
	"fmt"
	"strings"
	"testing"
)

// boundTestPeriod is the bound the synthetic record is built against: the ceiling of a
// period, small enough that the whole two-period run reads in the assertion itself.
const boundTestPeriod = 8

// syntheticWireRecord builds the wire record of a COMPLETE two-period run: the model
// offering a self-driven turn until the tool refuses, the operator's own line renewing
// the budget, and the renewed period running out exactly as the first did.
//
// The shape is the MEASURED one (sibling tree, whole-package run): 18 asks in total —
// bound accepted + one refusal per period, twice. That total is the reason the old
// assertion could not be satisfied by a correct console: `16 accepted, 2 refused` is a
// statement about the RUN, and the claim it was compared against said "8 accepted and
// exactly 1 refused".
//
// The turn indices run CONTINUOUSLY across the run (the mock's own numbering), and each
// period spends bound+1 asks — the bound the tool ACCEPTS plus the one ask it REFUSES at
// the ceiling (the measured shape: 9 asks for a bound of 8). So the renewed period's first
// accepted turn carries index bound+2, which is the case's `boundPeriodFirstIndex`, while
// the NOTICE it announces renumbers from 1.
func syntheticWireRecord(bound int) []periodTurnDecision {
	var rec []periodTurnDecision
	for period := 1; period <= 2; period++ {
		first := boundPeriodFirstIndex(period, bound)
		for i := 0; i < bound; i++ {
			rec = append(rec, periodTurnDecision{turnIndex: first + i, accepted: true})
		}
		// The refusal that closes the period: the ask that follows the closing turn, and it
		// carries that ask's own index (the case's mock records the outcome against the ask
		// it refused). `accepted=false` is what the ledger counts.
		rec = append(rec, periodTurnDecision{turnIndex: first + bound, accepted: false})
	}
	return rec
}

// TestBoundPeriodArithmeticIsReadOverThePeriodsOwnWindow is the guard for the MOCK-side
// half of the defect — the half this task exists for.
//
// It states, for BOTH periods, the claim the case makes, over the window the case reads
// — and then states the same claim over the WHOLE RUN, where it must FAIL. The second
// half is the point: it pins the defect's signature, so a future edit that reads a
// per-period claim off a run-scoped record cannot pass by accident.
func TestBoundPeriodArithmeticIsReadOverThePeriodsOwnWindow(t *testing.T) {
	rec := syntheticWireRecord(boundTestPeriod)

	for _, tc := range []struct {
		period int
		what   string
	}{
		{boundFirstPeriod, "the first period"},
		{boundRenewedPeriod, "the renewed period"},
	} {
		got := periodFrom(rec, boundPeriodFirstIndex(tc.period, boundTestPeriod), boundTestPeriod)
		if got.accepted != boundTestPeriod {
			t.Fatalf("%s: accepted %d self-driven turn(s), want exactly %d — a period's ceiling is a PER-PERIOD budget, "+
				"read over that period's own window", tc.what, got.accepted, boundTestPeriod)
		}
		if got.refused != 1 {
			t.Fatalf("%s: refused %d time(s), want exactly 1 — the refusal that closes the period is charged to the period it closes, "+
				"not to the run", tc.what, got.refused)
		}
		if got.first != boundPeriodFirstTurn || got.last != boundTestPeriod {
			t.Fatalf("%s: numbered %d..%d, want %d..%d — the renewal re-opens the numbering, which is what makes the "+
				"period's own arithmetic expressible at all",
				tc.what, got.first, got.last, boundPeriodFirstTurn, boundTestPeriod)
		}
	}

	// THE PRE-FIX READ, stated over the SAME record: the whole run. It must NOT satisfy
	// the per-period claim — this is the measured failure `16 accepted and 2 refused,
	// want 8 accepted and exactly 1 refused`, reduced to arithmetic.
	whole := periodArithmetic{}
	for _, ev := range rec {
		if ev.accepted {
			whole.accepted++
			continue
		}
		whole.refused++
	}
	if whole.accepted == boundTestPeriod || whole.refused == 1 {
		t.Fatalf("the RUN-scoped read of this record reported %d accepted and %d refused, and a per-period claim would be satisfied by it — "+
			"the record is meant to be two spent periods, which a run-scoped read cannot express (two periods of %d accepted + 1 refusal)",
			whole.accepted, whole.refused, boundTestPeriod)
	}
	if want := 2 * boundTestPeriod; whole.accepted != want {
		t.Fatalf("the synthetic run should carry %d accepted decisions (two periods of %d), got %d — the guard's own fixture is wrong",
			want, boundTestPeriod, whole.accepted)
	}
}

// TestBoundNoticeCountIsReadPerPeriodNotPerRun is the guard for the ANNOUNCEMENT half:
// the numbering notice is announced ONCE PER PERIOD with IDENTICAL wording, so a
// whole-run count of it reads 2 for a console behaving exactly as designed.
//
// This is the measured pre-fix red, reduced to the ledger: the same wording, twice, in
// two windows, and a claim that each window carries it exactly once.
func TestBoundNoticeCountIsReadPerPeriodNotPerRun(t *testing.T) {
	ledger := &countingWriter{}
	turnNotice := func(n int) string {
		return flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, n, boundTestPeriod))
	}
	// An announcement is recorded through the seam's own Write, so the guard exercises
	// the instrument the case reads rather than a copy of it.
	announce := func(format string, args ...interface{}) {
		if _, err := ledger.Write([]byte(fmt.Sprintf(format, args...))); err != nil {
			t.Fatalf("the ledger's Write failed: %v", err)
		}
	}
	// PERIOD 1: the full numbering, closed by its own two closing notices.
	for n := 1; n <= boundTestPeriod; n++ {
		announce(chatSelfDrivenTurnNotice, n, boundTestPeriod)
	}
	announce(chatSelfDrivenClosingNotice, boundTestPeriod)
	closing := flattened(fmt.Sprintf(chatSelfDrivenClosingNotice, boundTestPeriod))
	firstClose := ledger.entriesContaining(closing)
	if len(firstClose) != 1 {
		t.Fatalf("the ledger holds %d closing announcement(s) after period 1, want 1", len(firstClose))
	}
	// The renewal — the boundary the operator's own line publishes — then the renewed
	// period's IDENTICAL numbering.
	announce(chatSelfDrivenRenewalNotice, boundTestPeriod)
	renewal := flattened(fmt.Sprintf(chatSelfDrivenRenewalNotice, boundTestPeriod))
	renewals := ledger.entriesContaining(renewal)
	if len(renewals) != 1 {
		t.Fatalf("the ledger holds %d renewal announcement(s), want 1", len(renewals))
	}
	for n := 1; n <= boundTestPeriod; n++ {
		announce(chatSelfDrivenTurnNotice, n, boundTestPeriod)
	}
	announce(chatSelfDrivenClosingNotice, boundTestPeriod)

	// EACH PERIOD'S OWN WINDOW: exactly once per number, in that period.
	for n := 1; n <= boundTestPeriod; n++ {
		if got := ledger.countInWindow(0, firstClose[0], turnNotice(n)); got != 1 {
			t.Fatalf("self-driven turn %d/%d was announced %d time(s) in period 1 (the window up to and including its own "+
				"closing notice), want exactly 1", n, boundTestPeriod, got)
		}
		if got := ledger.countInWindow(renewals[0]+1, len(ledger.entries())-1, turnNotice(n)); got != 1 {
			t.Fatalf("self-driven turn %d/%d was announced %d time(s) in the RENEWED period (the window the renewal opened), "+
				"want exactly 1", n, boundTestPeriod, got)
		}
	}
	// …AND THE WHOLE RUN CARRIES IT TWICE, which is the fact a run-scoped count cannot
	// tell from a defect. Stated explicitly so the guard documents WHY the window is not
	// an optimisation: for n = 1 the two announcements are IDENTICAL text.
	if got := ledger.count(turnNotice(boundPeriodFirstTurn)); got != 2 {
		t.Fatalf("the whole-run count of self-driven turn %d/%d is %d, want 2 (one per period) — the guard's fixture must "+
			"reproduce the shape that made a run-scoped count red", boundPeriodFirstTurn, boundTestPeriod, got)
	}
	// A DUPLICATE INSIDE A PERIOD IS STILL A DEFECT, and the window says so: the claim
	// the case makes is not weakened by reading a window — the window is what makes it
	// expressible. (The pre-fix form READ the whole run and so could not distinguish the
	// two periods' legitimate renumbering from a double announcement.)
	announce(chatSelfDrivenTurnNotice, boundPeriodFirstTurn, boundTestPeriod)
	if got := ledger.countInWindow(renewals[0]+1, len(ledger.entries())-1, turnNotice(boundPeriodFirstTurn)); got != 2 {
		t.Fatalf("after a SECOND announcement of self-driven turn %d/%d inside the renewed period the window counted %d, want 2 — "+
			"a genuine in-period duplicate must stay visible", boundPeriodFirstTurn, boundTestPeriod, got)
	}
	if !strings.Contains(strings.Join(ledger.entries(), ""), "self-driventurn1/8") {
		t.Fatalf("the ledger's entries do not carry the numbering this guard asserts about: %v", ledger.entries())
	}
}

// TestBoundWireRecordChargesTheClosingRefusalToThePeriodItClosed is the guard for the
// one boundary decision the per-period read makes: the ask that is refused AFTER a
// period's closing turn is charged to the period that was SPENT when it arrived.
//
// It is asserted rather than left implicit because the alternative reading is
// defensible — and because both periods are read over the SAME rule, so neither
// period is privileged by the choice. What the guard pins is that the reading is
// deliberate: change the rule and this fails.
func TestBoundWireRecordChargesTheClosingRefusalToThePeriodItClosed(t *testing.T) {
	rec := syntheticWireRecord(boundTestPeriod)
	// Cut the run short the way a mid-assertion read sees it: period 1 complete, the
	// renewal written, the renewed period still running.
	partial := rec[:boundTestPeriod+1+3]
	p1 := periodFrom(partial, boundPeriodFirstIndex(boundFirstPeriod, boundTestPeriod), boundTestPeriod)
	if p1.accepted != boundTestPeriod || p1.refused != 1 {
		t.Fatalf("period 1 read over a record that continues into the renewed period: %d accepted, %d refused — want %d and 1 "+
			"(the read must be scoped to its own window, not to how far the run has got)",
			p1.accepted, p1.refused, boundTestPeriod)
	}
	// The renewed period, read over the SAME continued record, is its own arithmetic.
	p2 := periodFrom(partial, boundPeriodFirstIndex(boundRenewedPeriod, boundTestPeriod), boundTestPeriod)
	if p2.accepted != 3 {
		t.Fatalf("the renewed period read over a three-decision partial record: %d accepted, want 3 — the slice must take exactly "+
			"what the renewed period has produced so far", p2.accepted)
	}
	// And the period that has NOT started reads as nothing at all, never as "the run's
	// totals so far" (the pre-fix failure mode).
	if never := periodFrom(rec[:boundTestPeriod+1], boundPeriodFirstIndex(boundRenewedPeriod, boundTestPeriod), boundTestPeriod); never.accepted != 0 || never.refused != 0 {
		t.Fatalf("a period that has not started yet read as %d accepted / %d refused, want 0/0 — a period's window must be empty "+
			"before the period exists", never.accepted, never.refused)
	}
}

// TestBoundAskReconciliationPairsByTurnNotByRunTotal is the guard for the THIRD instrument
// the same mistake was found in (whole-package run, 2026-10-02) — one level over from the
// per-period claim: the mock's asks were reconciled against its decisions as two RUN
// TOTALS, so the check demanded `asks == decisions` with a one-ask allowance and reported
//
//	the mock offered 12 self-driven request(s) but the wire record holds 10 decision(s)
//
// for a fixture behaving EXACTLY as designed. The two totals differ by more than one
// whenever the console has raced ahead of the record, and the number in flight is a
// statement about how far the RUN has got — which is what the check had no business
// reading. Pairing the two sides by TURN INDEX asks the same question (does the fixture
// describe one wire?) without borrowing the run's progress into the answer.
func TestBoundAskReconciliationPairsByTurnNotByRunTotal(t *testing.T) {
	rec := syntheticWireRecord(boundTestPeriod)
	ledger := &countingWriter{}
	// THE MEASURED SHAPE: twelve asks made, ten decided — the two at the END have had no
	// result round served, because the console had raced ahead when the read was taken.
	// (The asks are logged one per request the mock hands out; a decided ask is marked with
	// the outcome its decision carries.)
	const inFlight = 2
	asks := rec[:boundTestPeriod+inFlight+2]
	for _, ev := range asks {
		ledger.ask(ev.turnIndex)
	}
	for _, ev := range asks[:len(asks)-inFlight] {
		ledger.record(ev)
		if !ev.accepted {
			ledger.serveAsk(ev.turnIndex)
		}
	}
	if runAsks, runDecisions := len(ledger.asks), len(ledger.decisions()); runAsks != 12 || runDecisions != 10 {
		t.Fatalf("the guard's own fixture is %d ask(s) / %d decision(s), want the measured 12 / 10", runAsks, runDecisions)
	}
	// THE POSITIONAL READ (the case's form): the two sides agree ask by ask, and an ask
	// with no result round yet is simply not part of the question.
	if mismatched, unpaired := ledger.askReconciliation(); len(mismatched) > 0 || len(unpaired) > 0 {
		t.Fatalf("the positional pairing reported %v outcome mismatch(es) and %v decision(s) belonging to no ask, want none — every "+
			"decision in this record belongs to an ask the mock made, with the same outcome", mismatched, unpaired)
	}
	// A DECISION WITH NO ASK IS STILL A BROKEN FIXTURE — the pairing does not weaken that.
	ledger.record(periodTurnDecision{turnIndex: 999, accepted: true})
	if _, unpaired := ledger.askReconciliation(); len(unpaired) != 1 || unpaired[0] != 999 {
		t.Fatalf("a decision with no ask behind it reported unpaired %v, want [999] — the positional read must still catch a decision "+
			"the mock never asked for", unpaired)
	}
	// THE PRE-FIX READ, over the SAME record: two run totals with a one-ask allowance. It
	// must be RED here — this is the measured failure reduced to arithmetic, and it is the
	// reason the check is now positional.
	runAsk, runDecision := len(ledger.asks), len(ledger.decisions())-1 // the 999 decision is not an ask
	if runAsk == runDecision || runAsk == runDecision+1 {
		t.Fatalf("the run-scoped reconciliation accepted %d ask(s) against %d decision(s) — the guard's fixture is meant to reproduce "+
			"the measured shape (%d asks, %d decisions, %d still in flight), which a totals comparison cannot describe",
			runAsk, runDecision, 12, 10, inFlight)
	}
}
