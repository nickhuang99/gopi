package uiclient

// compare_red_proof_test.go — the RED proof for the reported contradiction, written
// against MASTER's API only (CompareTranscriptLine + AuditAnswer + DamagePaintLoss all
// exist there), so it can be dropped into an archive of the base tree and RUN. A case
// that referenced the new fields would fail to COMPILE on the base tree, and this
// repo's own precedent is explicit that a compile error is not a RED proof
// (commit ccdfb727).

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// redProofLine is the shape of the report's line: the report's own was 95 runes.
var redProofLine = "self-observation: this line must land on the canvas verbatim, and the verb compares both sides."

// TestRedProof_RowDisplayingTheWholeLineIsNotPaintLoss asserts the TWO user-visible
// properties the report asks for, both base-compatible (they read only fields master
// already has, so this file COMPILES there — a compile error would not be a RED proof):
//
//   - the ledger must not classify as paint_loss a line whose whole text is at the
//     start of a grid row;
//   - when the printed `prefix` covers the whole line, the verdict's own reason must
//     not tell the reader the paint lost it — that sentence beside that measurement is
//     the contradiction, and it is what sends a reader to the wrong layer.
//
// It does NOT require the status to become a match: the row genuinely is not the line
// (no single row carries it exactly), and widening containment into agreement is the
// one thing this task must not do. The class carries the reconciliation instead.
func TestRedProof_RowDisplayingTheWholeLineIsNotPaintLoss(t *testing.T) {
	shapes := []struct {
		name string
		row  string
	}{
		{"line+line (the report's measured row)", lineedit.TurnMarker + redProofLine + redProofLine},
		{"line+line+line", lineedit.TurnMarker + redProofLine + redProofLine + redProofLine},
		{"line+other line", lineedit.TurnMarker + redProofLine + "另一行完全不同的内容"},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			cand := auditCandidate(117, s.row)
			led := AuditAnswer(cand, []string{redProofLine})
			if len(led.Lines) > 0 && led.Lines[0].Damage == DamagePaintLoss {
				t.Fatalf("CONTRADICTION: in_grid=%t prefix=%d expected_runes=%d damage=%s — the row displays the whole line",
					led.Lines[0].Verdict.InGrid, led.Lines[0].Verdict.GridPrefixRunes, len([]rune(redProofLine)), led.Lines[0].Damage)
			}
			v := CompareTranscriptLine(redProofLine, cand, []string{redProofLine})
			if v.GridPrefixRunes >= len([]rune(redProofLine)) && strings.Contains(v.Reason, "the paint lost it") {
				t.Fatalf("CONTRADICTION: status=%s in_grid=%t prefix=%d expected_runes=%d — the printed prefix covers the whole line while the reason says the paint lost it: %q",
					v.Status, v.InGrid, v.GridPrefixRunes, len([]rune(redProofLine)), v.Reason)
			}
		})
	}
}

// TestRedProof_GridWrappedLineAtTheGridsOwnWidthIsNotPaintLoss is the taxonomy half:
// a line exactly as wide as the grid, which the grid carried WRAPPED. Master's
// classifier asks the WIDTH (`cols > grid.Cols`) and never asks the grid, so this came
// back paint_loss with the whole text on the grid.
func TestRedProof_GridWrappedLineAtTheGridsOwnWidthIsNotPaintLoss(t *testing.T) {
	const cols = 117
	line := ""
	for len(line) < cols {
		line += "abcdefghij"
	}
	line = line[:cols]
	half := []rune(line)
	cut := len(half) / 2

	cand := auditCandidate(cols, lineedit.TurnMarker+string(half[:cut]), "  "+string(half[cut:]))
	led := AuditAnswer(cand, []string{line})
	if len(led.Lines) > 0 && led.Lines[0].Damage == DamagePaintLoss {
		t.Fatalf("CONTRADICTION: damage=%s for a %d-cell line the grid carried wrapped across rows 0..1 (prefix=%d)",
			led.Lines[0].Damage, cols, led.Lines[0].Verdict.GridPrefixRunes)
	}
}
