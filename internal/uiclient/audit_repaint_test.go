package uiclient

// audit_repaint_test.go — the whole-answer ledger's side of the re-paint defect
// (task 01a0e305): the class `repaint_tail`, that it does NOT move the ledger off
// agreed, and that the genuinely lost line is STILL disagreed.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// TestAuditAnswer_RepaintTailIsArrivalNotPaintLoss is the defect's own fixture: a
// faithful CJK answer line on a row that re-draws the line's head, exactly the
// shape a real 117x26 pty produced. Before this class existed the ledger reported
// damage=paint_loss / matched=0 / disagreed=1 / exit 3 for this frame.
func TestAuditAnswer_RepaintTailIsArrivalNotPaintLoss(t *testing.T) {
	head := []rune(cjkAnswerLine)
	const tailRunes = 29
	row := lineedit.TurnMarker + cjkAnswerLine + string(head[:tailRunes])
	const cols = 117
	cand := auditCandidate(cols, "──── turn 1 ────", row, "you>")

	led := AuditAnswer(cand, []string{cjkAnswerLine})

	if led.Matched != 1 || led.Disagreed != 0 {
		t.Fatalf("matched=%d disagreed=%d, want 1 and 0 — the whole line is on the row", led.Matched, led.Disagreed)
	}
	if led.Status != AuditAgreed {
		t.Fatalf("status=%q, want %q: a faithful paint must not be reported as a loss", led.Status, AuditAgreed)
	}
	if led.Repainted != 1 {
		t.Fatalf("repainted=%d, want 1 — the class exists so the row-exact miss is still VISIBLE", led.Repainted)
	}
	if len(led.Lines) != 1 {
		t.Fatalf("lines=%d, want 1: a re-painted row is recorded, not silently folded into the match count", len(led.Lines))
	}
	entry := led.Lines[0]
	if entry.Damage != DamageRepaintTail {
		t.Fatalf("damage=%q, want %q", entry.Damage, DamageRepaintTail)
	}
	if entry.Verdict.InGrid != true || entry.Verdict.InTranscript != true {
		t.Fatalf("a re-painted row is on BOTH sides: in_grid=%t in_transcript=%t", entry.Verdict.InGrid, entry.Verdict.InTranscript)
	}
	if entry.RowTailRunes != tailRunes {
		t.Fatalf("row_tail_runes=%d, want %d (the evidence, not just the verdict)", entry.RowTailRunes, tailRunes)
	}
	if !strings.Contains(entry.Verdict.Reason, "re-drawn") {
		t.Fatalf("the reason does not name what the row carried past the line: %q", entry.Verdict.Reason)
	}
}

// TestAuditAnswer_LostLineIsStillPaintLoss is the anti-blanket-excuse direction at
// the LEDGER layer: the same fixture with the line genuinely absent from the grid
// must still be a paint_loss disagreement, with the status and the count to match.
// Without it, a rule that accepted any row would turn every lost line into a pass.
func TestAuditAnswer_LostLineIsStillPaintLoss(t *testing.T) {
	const cols = 117
	cand := auditCandidate(cols, "──── turn 1 ────", "a different row entirely", "you>")
	led := AuditAnswer(cand, []string{cjkAnswerLine})

	if led.Disagreed != 1 || led.Matched != 0 {
		t.Fatalf("matched=%d disagreed=%d, want 0 and 1 — a lost line must keep being reported", led.Matched, led.Disagreed)
	}
	if led.Status != AuditDisagreed {
		t.Fatalf("status=%q, want %q", led.Status, AuditDisagreed)
	}
	if led.Repainted != 0 {
		t.Fatalf("repainted=%d, want 0: nothing re-painted this row", led.Repainted)
	}
	if len(led.Lines) != 1 || led.Lines[0].Damage != DamagePaintLoss {
		t.Fatalf("damage=%v, want %q", led.Lines, DamagePaintLoss)
	}
}

// TestAuditAnswer_RowCarryingAnotherLineIsNotArrival is the ledger's version of the
// tail rule: a row that carries this line and then DIFFERENT text is not a plain
// arrival, and the ledger must keep saying so. It is the case that separates "a
// re-paint of this line" from "some row that happens to start with it".
//
// THE CONTRACT MOVED HERE, and this case is the record of it. It used to require
// damage=paint_loss / disagreed=1. That verdict was the reported CONTRADICTION (task
// 01a0e313) once the row was measured: the row opens with the WHOLE line — the
// transcript's own `prefix` diagnostic equals the line's rune count — so "the paint
// lost your text" blamed the paint for text the paint carried, and sent the reader to
// the wrong layer. The row IS damaged (a re-paint that was not cleared, or a second
// line over its tail), and the class that says so is damage_to_row: recorded, counted
// as Repainted, and NOT a disagreement about the text. The loss direction is
// unchanged and is pinned by TestAuditAnswer_LostLineIsStillPaintLoss.
func TestAuditAnswer_RowCarryingAnotherLineIsNotArrival(t *testing.T) {
	const cols = 117
	row := lineedit.TurnMarker + cjkAnswerLine + "另一行完全不同的内容"
	cand := auditCandidate(cols, row)
	led := AuditAnswer(cand, []string{cjkAnswerLine})
	if led.Disagreed != 0 || led.Repainted != 1 {
		t.Fatalf("matched=%d disagreed=%d repainted=%d, want 0/0/1 — the text arrived, the ROW is damaged", led.Matched, led.Disagreed, led.Repainted)
	}
	if led.Lines[0].Damage != DamageToRow {
		t.Fatalf("damage=%q, want %q (never %q: the whole line is on that row)", led.Lines[0].Damage, DamageToRow, DamagePaintLoss)
	}
	if led.Status != AuditAgreed {
		t.Fatalf("status=%q, want %q: no text was lost", led.Status, AuditAgreed)
	}
	if led.Lines[0].Verdict.GridPrefixRunes < len([]rune(cjkAnswerLine)) {
		t.Fatalf("the verdict's own prefix diagnostic (%d) does not cover the line (%d): the fixture no longer reproduces the reported contradiction",
			led.Lines[0].Verdict.GridPrefixRunes, len([]rune(cjkAnswerLine)))
	}
}
