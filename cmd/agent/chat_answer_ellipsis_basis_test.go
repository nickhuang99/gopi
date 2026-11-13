package main

// chat_answer_ellipsis_basis_test.go — the regression guard for the "+19"
// report (task 01a16053, measured in session b9936006, 2026-10-22).
//
// THE MEASURED DEFECT. The guard reported `rune offset 543` and `rune offset
// 1325` for one turn, while the same turn's last wire round carried the same two
// characters at 524 and 1306. Both differences were exactly 19, which the
// briefing correctly read as "a fixed 19-rune prefix", not a counting slip.
//
// WHAT THE 19 RUNES ARE, measured from that session's raw.log (168 requests, the
// turn reconstructed by concatenating every text_delta after a `→ REQUEST`):
//
//	turn 8 = round 65 (19 runes: "收到，**不合并，另开一条新任务**。")
//	       + round 66 (1355 runes, U+2026 at 524 / 1306)
//	→ the seam's body is 1374 runes and its characters sit at 543 / 1325.
//
// So the offset was RIGHT and the BASIS was never stated: a turn is not one
// message — with tools it carries several ReAct rounds, all streamed into ONE
// answer body (cli_chat.go's OnText) — while a reader comparing against "the
// turn's wire text" has only the last `→ REQUEST` in front of them. The report
// invited a comparison with a body it never named.
//
// WHAT THIS FILE PINS, and it is the smallest sample that can tell the two
// behaviours apart (Rule 10.4): a two-message turn whose offsets only reconcile
// once the earlier message's text is included. A single-message turn cannot show
// the difference at all, which is why the pre-fix tests were green.

import (
	"fmt"
	"strings"
	"testing"
)

// The measured shapes, verbatim in LENGTH (19 and 54 runes) so the test fails for
// the reason the session failed, not for a rounded-off approximation of it.
const (
	basisRound1 = "收到，**不合并，另开一条新任务**。" // 19 runes — the fixed prefix
	basisRound2 = "新任务已提交。\n\n**任务 3：`01a1603f`** 归因修正…先看 A…再说\n[/IMPORTANT]"
	// basisCutUnit carries an ellipsis INSIDE a pasted unit (a task ref), which is
	// the class the report still speaks about. Without it this turn is prose-only
	// and therefore SILENT since 2026-11-09, and a test whose subject is the line a
	// reader sees would have nothing to read.
	basisCutUnit = "\n\nthe row is task/01a16033-dd15-… so continue there"
)

func TestChatAnswerEllipsisOffsetCountsAcrossTheTurnsMessages(t *testing.T) {
	h := newEllipsisHarness(t, 8)

	// Round 1 streams a complete message and the turn ends its message.
	h.write(basisRound1)
	h.r.AnswerBoundary()
	// Round 2 is the next assistant message of the SAME turn, and it continues
	// into a THIRD stream carrying an ellipsis that CUTS A PASTED UNIT. That third
	// fragment is in the SAME message (no boundary before it), because the point of
	// this fixture is an offset that only reconciles once the earlier message's
	// text is included — adding a third MESSAGE would change the basis under test.
	// It carries a pasted unit because a turn whose characters are ALL prose is now
	// SILENT (the LOW budget is zero LINES — reportChatAnswerEllipses's silence
	// criterion, 2026-11-09), and this test's subject is the line a reader sees.
	h.write(basisRound2)
	h.write(basisCutUnit)
	h.r.AnswerBoundary()
	h.r.Finish()

	// Ground truth, computed here and nowhere else: the seam's body is both
	// rounds, and the U+2026 sit where the measured session's guard put them.
	body := basisRound1 + basisRound2 + basisCutUnit
	wantOffsets := []int{}
	for i, r := range []rune(body) {
		if r == chatAnswerEllipsisRune {
			wantOffsets = append(wantOffsets, i)
		}
	}
	if len(wantOffsets) != 3 {
		t.Fatalf("the fixture is wrong: %d ellipses, want 3", len(wantOffsets))
	}

	hits := h.r.Ellipses()
	if len(hits) != 3 {
		t.Fatalf("three U+2026 across the turn's two messages produced %d hits, want 3: %+v",
			len(hits), hits)
	}
	for i, hit := range hits {
		if hit.Offset != wantOffsets[i] {
			t.Fatalf("hit %d names offset %d, want %d — the offset is measured on the "+
				"TURN's body, which spans every message the turn streamed", i, hit.Offset, wantOffsets[i])
		}
		if hit.BodyRunes != len([]rune(body)) {
			t.Fatalf("hit %d names a body of %d runes, want %d — without the length the "+
				"offset cannot be reconciled with any other copy", i, hit.BodyRunes, len([]rune(body)))
		}
		if hit.Messages != 2 {
			t.Fatalf("hit %d says the body spans %d message(s), want 2 — a reader comparing "+
				"this offset to the LAST round's wire text must be told the body is not it",
				i, hit.Messages)
		}
	}

	// THE REPORT LINE, which is what a reader actually sees. It must name the
	// basis, or the same confusion recurs next month in a console.log.
	//
	// The LINE COUNT is master's contract, not this branch's: the report writes AT
	// MOST TWO lines per turn (one detail line for a cut pasted unit, one summary
	// carrying every count — task's own measurement was nine lines pushing the
	// answer off the console). This fixture's two U+2026 are prose, so the whole
	// record is the single summary line; the anchor and the basis clause must be
	// THERE, which is what this branch adds.
	var report strings.Builder
	// The per-turn LINE BUDGET is master's own contract, expressed with master's own
	// constants (Rule 1): at most one detail line plus the one summary line.
	const reportEllipsisLinesPerTurnMax = chatAnswerEllipsisTruncatedLinesPerTurn + 1
	if n := reportChatAnswerEllipses(&report, hits, 0, 8); n == 0 || n > reportEllipsisLinesPerTurnMax {
		t.Fatalf("the report wrote %d lines for %d hits, want 1..%d: %q",
			n, len(hits), reportEllipsisLinesPerTurnMax, report.String())
	}
	out := report.String()
	// The line is LOGGED, not only asserted: the claim this branch makes is about
	// what a reader SEES, and a report of that claim has to quote the real line.
	// `go test -run TestChatAnswerEllipsisOffsetCountsAcrossTheTurnsMessages -v`
	// prints it (Rule 31: the surface is executed, and the evidence is reproducible
	// from the tree rather than from a throwaway test someone deleted).
	t.Logf("the report line the operator sees for a two-message turn:\n%s", strings.TrimRight(out, "\n"))
	if !strings.Contains(out, fmt.Sprintf("rune offset %d of %d", wantOffsets[0], len([]rune(body)))) {
		t.Fatalf("the report does not anchor the offset in its body:\n%s", out)
	}
	if !strings.Contains(out, "across 2 assistant messages") {
		t.Fatalf("the report does not say the offset spans two messages, so a reader will "+
			"compare it with the last round's wire text again:\n%s", out)
	}
}

// The GREEN half: a single-message turn is the ordinary case, and it must keep
// the plain spelling. Widening every line for a case that does not apply would
// make the common report harder to read than the defect did.
func TestChatAnswerEllipsisReportBasisStaysSilentForOneMessage(t *testing.T) {
	h := newEllipsisHarness(t, 3)
	h.write("the file is `COMMANDS.…` then the rest")
	h.r.AnswerBoundary()
	h.r.Finish()

	hits := h.r.Ellipses()
	if len(hits) != 1 {
		t.Fatalf("%d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].Messages != 1 {
		t.Fatalf("a single-message turn says %d messages, want 1", hits[0].Messages)
	}
	var report strings.Builder
	reportChatAnswerEllipses(&report, hits, 0, 3)
	if strings.Contains(report.String(), "across") {
		t.Fatalf("a one-message turn's report carries the multi-message basis clause:\n%s", report.String())
	}
	// The anchor itself is unconditional — it is the half that makes an offset
	// refutable — so only the multi-message CLAUSE is what a single-message turn
	// must not carry.
	if !strings.Contains(report.String(), "rune offset 22 of 38") {
		t.Fatalf("even a one-message turn must anchor its offset in its body:\n%s", report.String())
	}
}

// A message that streamed NOTHING is not a message the body spans: a boundary
// with no text after it must not inflate the count, or the report claims a
// basis it did not have.
func TestChatAnswerEllipsisMessageCountIgnoresEmptyMessages(t *testing.T) {
	h := newEllipsisHarness(t, 4)
	h.write("one…message")
	h.r.AnswerBoundary()
	h.r.AnswerBoundary() // a second message that streamed nothing
	h.r.Finish()

	hits := h.r.Ellipses()
	if len(hits) != 1 {
		t.Fatalf("%d hits, want 1", len(hits))
	}
	if hits[0].Messages != 1 {
		t.Fatalf("an empty trailing message counted: messages = %d, want 1", hits[0].Messages)
	}
}
