package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestBoundNoticeCountIsReadPerPeriodNotPerPaint is the minimal, DETERMINISTIC
// reproduction of the whole-package flake this file's subject was filed for:
//
//	chat_surface_bound_e2e_test.go:446: the console announced the self-driven turn
//	1/8 2 time(s), want exactly 1 — the ACCEPTED count is the loop's own, read where
//	the loop numbers it (bound 8)
//
// WHAT IS BEING PINNED, in one sentence: "turn 1/8 of this period" is announced
// TWICE IN A COMPLETE RUN BY DESIGN — once by period 1, and once by period 2 after
// the operator's own line renews the budget — so a count taken over the whole run
// reports 2 for a console that is behaving correctly, and the old claim could only
// pass by reading the stream BEFORE the renewal started, i.e. by winning a race.
// The fix reads each announcement in the window its period produced, which is what
// the loop's own accounting is.
//
// IT IS DETERMINISTIC AND PACKAGE-CHEAP: it drives the announcement seam directly
// (chatInput.WriteNotice + the counting ledger), with no pty, no HTTP mock, no
// model and no timing — the e2e case is the ACCEPTANCE (it found the defect), and
// this is the regression guard, which must be able to fail for exactly one reason
// (Rule 5).
func TestBoundNoticeCountIsReadPerPeriodNotPerPaint(t *testing.T) {
	const bound = 8
	turnNotice := func(n int) string { return fmt.Sprintf(chatSelfDrivenTurnNotice, n, bound) }
	closing := fmt.Sprintf(chatSelfDrivenClosingNotice, bound)
	renewal := fmt.Sprintf(chatSelfDrivenRenewalNotice, bound)

	// The run, in the order the REPL announces it: period 1 numbers every turn
	// 1..bound and closes; the operator's line renews; period 2 numbers its first
	// turn 1 of the bound AGAIN and runs to its own closing notice.
	ledger := &countingWriter{}
	in := &chatInput{announce: ledger, errOut: io.Discard}
	for n := 1; n <= bound; n++ {
		in.WriteNotice(turnNotice(n))
	}
	in.WriteNotice(closing)
	in.WriteNotice(renewal)
	in.WriteNotice(turnNotice(1))
	for n := 2; n <= bound; n++ {
		in.WriteNotice(turnNotice(n))
	}
	in.WriteNotice(closing)

	// THE OLD CLAIM'S IMPOSSIBILITY, stated as an assertion: over the whole run the
	// words "turn 1/8 of this period" appear TWICE — the console announced them
	// once per period, correctly. A run-scoped exact count is therefore not
	// expressible over this record at all, which is why the fix scopes the count to
	// the period (below) rather than loosening the number.
	if got := ledger.count(flattened(turnNotice(1))); got != 2 {
		t.Fatalf("turn 1/%d was announced %d time(s) in the whole run, want exactly 2 (once per period — the renewed period numbers its "+
			"first turn 1 of the bound again): the premise this regression guard rests on no longer holds, so its window assertions below "+
			"are no longer evidence about the defect", bound, got)
	}

	// PERIOD 1: every number announced EXACTLY ONCE, read up to and including the
	// closing notice — the console's own statement that period 1 is complete.
	for n := 1; n <= bound; n++ {
		if got := ledger.countBefore(flattened(closing), flattened(turnNotice(n))); got != 1 {
			t.Fatalf("in period 1 (entries up to and including the closing notice), self-driven turn %d/%d was announced %d time(s), "+
				"want exactly 1 — the loop numbers each accepted turn once", n, bound, got)
		}
	}
	// …and nothing past the ceiling: a turn bound+1 would be a turn the budget does
	// not have, and it would be invisible to the per-number loop above.
	if got := ledger.countBefore(flattened(closing), flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, bound+1, bound))); got != 0 {
		t.Fatalf("in period 1 the console announced a self-driven turn %d/%d %d time(s), want exactly 0 — the numbering must stop at the "+
			"ceiling", bound+1, bound, got)
	}

	// PERIOD 2: the renewal's own first turn, read after the renewal notice — the
	// one place that turn can be told from period 1's identical text.
	if got := ledger.turnNoticesAfter(flattened(renewal), 1, bound); got != 1 {
		t.Fatalf("after the renewal the console announced self-driven turn 1/%d %d time(s), want exactly 1 — the operator's line buys a "+
			"fresh period whose first turn is numbered 1 of the bound again", bound, got)
	}
	// …and the RENEWED period's own numbering is read over ITS window (the entries
	// after the renewal notice), exactly as period 1's was read over period 1's: every
	// number 1..bound announced once, the same arithmetic the first period obeyed.
	for n := 1; n <= bound; n++ {
		if got := ledger.turnNoticesAfter(flattened(renewal), n, bound); got != 1 {
			t.Fatalf("in the renewed period (entries after the renewal notice), self-driven turn %d/%d was announced %d time(s), want exactly 1 "+
				"— the renewal buys a period numbered 1..%d once each", n, bound, got, bound)
		}
	}
	// THE WINDOW IS WHAT MAKES THE COUNT POSSIBLE, stated as the negative it is: the
	// whole-run count of turn 2/8 is 2 (one per period), so a claim of "exactly 1" over
	// the whole run would be red on a console that is behaving perfectly. This is the
	// reason the fix scopes the count rather than adjusting the number.
	if got := ledger.count(flattened(turnNotice(2))); got != 2 {
		t.Fatalf("turn 2/%d was announced %d time(s) in the whole run, want 2 (once per period)", bound, got)
	}
}

// TestBoundNoticeLedgerCountsAnnouncementsNotRepaints pins the OTHER half of the
// defect, and is the reason the count has a seam of its own: the record an exact
// count is made against must be a log of ANNOUNCEMENTS, while a run's painted byte
// stream is a log of WRITES that also carries the editor's redrawing of the rows it
// tracks. Counting the painted stream therefore cannot distinguish "announced once,
// redrawn" from "announced twice" — measured in a passing run's own raw stream,
// where a row carrying a notice is re-issued by the layout as incremental redrawing
// (the artifact is quoted in devdoc/chat-surface-bound-notice-count-per-period.md).
//
// The test drives the seam with the SAME text twice and asserts the count is two —
// the ledger is not deduplicating, and a genuine duplicate announcement is still
// reported — while nothing in the EDITOR's repaint path reaches it at all (the
// repaint writes terminal bytes; the ledger is written by chatInput.WriteNotice).
func TestBoundNoticeLedgerCountsAnnouncementsNotRepaints(t *testing.T) {
	ledger := &countingWriter{}
	in := &chatInput{announce: ledger}
	text := fmt.Sprintf(chatSelfDrivenTurnNotice, 1, 8)

	in.WriteNotice(text)
	if got := ledger.count(flattened(text)); got != 1 {
		t.Fatalf("after ONE announcement the ledger counted %d, want exactly 1", got)
	}
	// A SECOND announcement of the same words is a real duplication — the loop
	// numbering one turn twice — and the ledger must show it rather than absorb it:
	// this is the assertion that a bug in the loop cannot hide behind the fix.
	in.WriteNotice(text)
	if got := ledger.count(flattened(text)); got != 2 {
		t.Fatalf("after TWO announcements the ledger counted %d, want exactly 2 — the count must report a real duplicate, never dedupe it", got)
	}
	// THE OBSERVER IS ADDITIVE: installing one must not divert the notice the
	// operator reads. Without an editor the notice goes to the error stream, and it
	// must arrive there WHOLE and UNCHANGED while the ledger counts it — an
	// instrument that replaced the console's own text would be a different console.
	var out strings.Builder
	observed := &chatInput{announce: ledger, errOut: &out}
	observed.WriteNotice(text)
	if out.String() != text {
		t.Fatalf("with an announcement observer installed the console carried %q, want %q (the observer is a TEE: the notice the operator "+
			"reads is unchanged, trailing break included — the constant already ends in one)", out.String(), text)
	}
	if got := ledger.count(flattened(text)); got != 3 {
		t.Fatalf("the observed notice took the ledger to %d, want 3 announcements (two direct + this one)", got)
	}
}
