package uiclient

// audit_test.go — the WHOLE-ANSWER audit's own claims, on hand-built inputs (no
// files, no processes, no clock): what it counts, how it CLASSIFIES a lost line,
// that a wrap is not a loss, and the two things it refuses to guess.
//
// The emphasis cases are here rather than in the CLI test because the measurement
// is a per-CELL question and a hand-built ATTRIBUTE GRID states it exactly: a
// fixture that painted real bytes could only reach the same shapes through a
// terminal, which would make the assertion about the terminal.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// auditCandidate builds a painted candidate at a known geometry, with the given
// grid rows. It starts from the compare fixture's shape and adds the attribute
// runs the emphasis half reads.
func auditCandidate(cols int, rows ...string) Candidate {
	return Candidate{
		Path:    "/fixture/chat-screen.dump",
		Surface: SurfaceChat,
		State:   StateExited,
		Frames:  9,
		Record:  DumpRecord{Fields: map[string]string{dumpRecordGeometryField: string(GeometrySourceLayout)}},
		Dump: lineedit.ScreenDump{
			Cols: cols, Rows: len(rows), Lines: append([]string(nil), rows...),
			Grid: append([]string(nil), rows...), AttrsRecorded: true,
		},
	}
}

// TestAuditAnswer_CountsEveryLineAndClassifiesTheLoss is the core claim: an answer
// is audited as a UNIT, the counts close, and each disagreeing line is named by
// its damage class — derived from the line's own width against the grid's, which is
// the only evidence one frame offers.
func TestAuditAnswer_CountsEveryLineAndClassifiesTheLoss(t *testing.T) {
	const cols = 40
	painted := "a line that is on the grid"
	// Fits the grid and is absent: no wrap can explain it -> paint_loss.
	fits := "a line that fits but is not painted"
	// Wider (in cells) than the grid, and wider than it is long in runes -> the
	// layout's double-width arithmetic is the candidate explanation.
	wide := strings.Repeat("中文与 latin ", 8)
	// Over-wide in RUNES as well: no CJK arithmetic is needed to explain it, so it
	// is the weaker claim (a wrap OR a loss).
	narrowWide := strings.Repeat("abc ", 20)

	cand := auditCandidate(cols, painted)
	led := AuditAnswer(cand, []string{painted, fits, wide, narrowWide})

	if led.Matched != 1 || led.Disagreed != 3 {
		t.Fatalf("matched=%d disagreed=%d, want 1 and 3", led.Matched, led.Disagreed)
	}
	if led.Status != AuditDisagreed {
		t.Fatalf("status = %q, want %q", led.Status, AuditDisagreed)
	}
	if led.TranscriptLines != 4 || led.Audited != 4 {
		t.Fatalf("transcript_lines=%d audited=%d, want 4 and 4", led.TranscriptLines, led.Audited)
	}
	if led.Geometry != GeometrySourceLayout || led.Cols != cols {
		t.Fatalf("the ledger does not carry the geometry the verdicts were computed at: geometry=%q cols=%d", led.Geometry, led.Cols)
	}
	got := map[string]AuditDamage{}
	for _, l := range led.Lines {
		got[l.Verdict.Line] = l.Damage
	}
	if got[fits] != DamagePaintLoss {
		t.Errorf("the line that FITS the grid but is absent = %q, want %q (no wrap can explain it)", got[fits], DamagePaintLoss)
	}
	if got[wide] != DamageCJKWrap {
		t.Errorf("the wide-rune line over the grid = %q, want %q", got[wide], DamageCJKWrap)
	}
	if got[narrowWide] != DamageWrapArtifactOrLoss {
		t.Errorf("the over-wide line = %q, want %q", got[narrowWide], DamageWrapArtifactOrLoss)
	}
	// The direction is carried per line: the transcript has it, the grid does not.
	for _, l := range led.Lines {
		if !l.Verdict.InTranscript || l.Verdict.InGrid {
			t.Errorf("line %q direction = in_grid=%t in_transcript=%t, want false and true",
				l.Verdict.Line, l.Verdict.InGrid, l.Verdict.InTranscript)
		}
		if l.ExpectedCols == 0 || l.ExpectedRunes == 0 || l.GridCols != cols {
			t.Errorf("line %q does not carry its own arithmetic: cols=%d runes=%d grid_cols=%d",
				l.Verdict.Line, l.ExpectedCols, l.ExpectedRunes, l.GridCols)
		}
	}
}

// TestAuditAnswer_ARwrapIsNotALoss is the instrument that keeps the audit honest:
// a line the grid WRAPPED across consecutive rows ARRIVED, and reporting it as a
// loss is the false signal this family of changes exists to remove.
func TestAuditAnswer_WrapIsNotALoss(t *testing.T) {
	const cols = 20
	line := "中文与 latin 混排的句子 —— dump 必须逐字符还原。"
	// The grid drew it on two rows (the band's marker column on the first), which
	// is exactly what a terminal does with a line wider than the window.
	half := []rune(line)
	cut := len(half) / 2
	cand := auditCandidate(cols, lineedit.TurnMarker+string(half[:cut]), "  "+string(half[cut:]))
	led := AuditAnswer(cand, []string{line})
	if led.Rejoined != 1 {
		t.Fatalf("the wrapped line was not reported as arrived: status=%q matched=%d disagreed=%d lines=%d",
			led.Status, led.Matched, led.Disagreed, len(led.Lines))
	}
	if led.Status != AuditAgreed || led.Disagreed != 0 {
		t.Fatalf("status=%q disagreed=%d, want %q and 0 — a wrap is not a loss", led.Status, led.Disagreed, AuditAgreed)
	}
	if !strings.Contains(led.Lines[0].Verdict.Reason, "WRAPPED across rows") {
		t.Fatalf("the wrap verdict does not name the rows it was reassembled from: %q", led.Lines[0].Verdict.Reason)
	}
	// The RED direction of this case: the two rows are not one row-exact row — the
	// arrival is reachable ONLY through the join instrument, and it is asserted as
	// an arrival on the ONE-LINE face too (both faces ask the same oracle, which is
	// what the reported defect — `--compare` calling a wrap a loss — was missing).
	v := CompareTranscriptLine(line, cand, []string{line})
	if !v.Rejoined || v.Status != CompareMatch {
		t.Fatalf("the one-line face did not ask the join for a line the grid wrapped: status=%s rejoined=%t reason=%q",
			v.Status, v.Rejoined, v.Reason)
	}
	if v.GridPrefixRunes != len([]rune(line)) {
		t.Fatalf("the one-line face's prefix=%d, want the line's own %d runes: the number is measured in the LINE's space",
			v.GridPrefixRunes, len([]rune(line)))
	}
	if v.RowTailRunes == 0 {
		t.Fatalf("the fixture is not exercising the join: the row-exact compare already matched with no tail (row_tail_runes=%d)", v.RowTailRunes)
	}
}

// TestAuditAnswer_DrawnTextIsWhatTheGridCarries is the emphasis/marker bridge: an
// answer line carrying the format's intra-line markers must be compared as the
// text the SURFACE DRAWS (markers removed), or every emphasised line would be
// reported as lost — the exact case the audit exists for.
func TestAuditAnswer_DrawnTextIsWhatTheGridCarries(t *testing.T) {
	const raw = "the **row shear** comes from counting a marker as a cell"
	drawn := lineedit.DisplayText(raw)
	if drawn == raw {
		t.Fatalf("the format layer did not remove the marker: %q", drawn)
	}
	cand := auditCandidate(117, drawn)
	led := AuditAnswer(cand, []string{raw})
	if led.Status != AuditAgreed || led.Matched != 1 {
		t.Fatalf("the emphasised line was not counted as arrived: status=%q matched=%d disagreed=%d lines=%+v",
			led.Status, led.Matched, led.Disagreed, led.Lines)
	}
}

// TestAuditAnswer_FenceLineKeepsItsMarkers is the same bridge's other half: inside
// a fence the marker is CODE, so the drawn text keeps it and the comparison must
// not strip it (the rowFor gate, asked through the composer rather than re-decided).
func TestAuditAnswer_FenceLineKeepsItsMarkers(t *testing.T) {
	inFence := "code = \"**not bold**\""
	cand := auditCandidate(117, "```", inFence)
	led := AuditAnswer(cand, []string{"```", inFence})
	if led.Matched != 2 || led.Disagreed != 0 {
		t.Fatalf("a fenced line was compared as prose: matched=%d disagreed=%d lines=%+v", led.Matched, led.Disagreed, led.Lines)
	}
	// The RED direction: the same input compared through a marker-stripping pass
	// (what a caller who derived the drawn text itself would do) does not match.
	if lineedit.DisplayText(inFence) == inFence {
		t.Skip("the format layer strips markers on a code line: the RED direction is not observable here")
	}
	if v := CompareTranscriptLine(lineedit.DisplayText(inFence), cand, []string{inFence}); v.Status == CompareMatch {
		t.Fatalf("the fixture does not exercise the fence gate: the stripped text still matched (status=%s)", v.Status)
	}
}

// TestAuditAnswer_RefusesWhenTheGridCannotBeRead is the contract that must never
// be traded for a per-line "refused": a grid nothing can be read from refuses the
// WHOLE audit, so the caller cannot mistake N refusals for N verified lines.
func TestAuditAnswer_RefusesWhenTheGridCannotBeRead(t *testing.T) {
	cand := auditCandidate(117, "a painted row")
	// Unpainted: frames=0 is not a screen.
	cand.Frames = 0
	led := AuditAnswer(cand, []string{"a painted row"})
	if led.Status != AuditRefused || led.Reason != UnpaintedNotice {
		t.Fatalf("an unpainted grid: status=%q reason=%q, want %q / the unpainted notice", led.Status, led.Reason, AuditRefused)
	}
	if led.Matched != 0 || led.Disagreed != 0 {
		t.Fatalf("a refusal reported counts: matched=%d disagreed=%d", led.Matched, led.Disagreed)
	}
	// Fallback geometry: the same refusal, by name.
	cand.Frames = 9
	cand.Record.Fields[dumpRecordGeometryField] = string(GeometrySourceFallback)
	led = AuditAnswer(cand, []string{"a painted row"})
	if led.Status != AuditRefused || led.Reason != GeometryFallbackNotice {
		t.Fatalf("a fallback geometry: status=%q reason=%q, want %q / the fallback notice", led.Status, led.Reason, AuditRefused)
	}
}

// TestAuditAnswer_EmptyTranscriptIsNotAnAgreement: "the grid was readable and there
// was nothing to check" is its own status, never a vacuous pass.
func TestAuditAnswer_EmptyTranscriptIsNotAnAgreement(t *testing.T) {
	led := AuditAnswer(auditCandidate(117, "a painted row"), nil)
	if led.Status != AuditNoTranscript {
		t.Fatalf("status=%q, want %q", led.Status, AuditNoTranscript)
	}
	if led.Matched != 0 {
		t.Fatalf("an empty transcript reported %d matches", led.Matched)
	}
}

// TestAuditAnswer_BoundIsSaidOutLoud: an answer longer than the bound is audited up
// to it and the truncation is REPORTED — a quietly cut set reads like a shorter
// answer, which is the failure mode the ledger exists to prevent.
func TestAuditAnswer_BoundIsSaidOutLoud(t *testing.T) {
	rows := make([]string, 0, AuditBound)
	transcript := make([]string, AuditBound+3)
	for i := range transcript {
		line := "line " + strings.Repeat("x", i%3) + strings.Repeat("y", 4) + string(rune('a'+i%26))
		transcript[i] = line
		if i < AuditBound {
			rows = append(rows, line)
		}
	}
	led := AuditAnswer(auditCandidate(117, rows...), transcript)
	if !led.Truncated {
		t.Fatal("the audit did not report that the bound cut it short")
	}
	if led.Audited != AuditBound || led.TranscriptLines != AuditBound+3 {
		t.Fatalf("audited=%d of transcript_lines=%d, want %d of %d", led.Audited, led.TranscriptLines, AuditBound, AuditBound+3)
	}
}

// TestAuditAnswer_EmphasisPaintedAndUnpainted is the operator's second damage
// class, measured per CELL: the same line, the same cells, and the only variable is
// whether the emphasised span's style differs from the row's plain text.
func TestAuditAnswer_EmphasisPaintedAndUnpainted(t *testing.T) {
	const raw = "the **row shear** comes from counting a marker"
	drawn := lineedit.DisplayText(raw)
	span := "row shear"
	col := strings.Index(drawn, span)
	if col < 0 {
		t.Fatalf("the fixture's span is not in the drawn text %q", drawn)
	}
	const plain, bold = "dim", "dim bold"

	// PAINTED: the span's cells carry the emphasis style, the rest the plain one.
	painted := auditCandidate(117, drawn)
	painted.Dump.AttrRuns = [][]lineedit.DumpAttrRun{{{
		Col: 0, Cells: col, Style: plain,
	}, {
		Col: col, Cells: len(span), Style: bold,
	}, {
		Col: col + len(span), Cells: 117 - col - len(span), Style: plain,
	}}}
	led := AuditAnswer(painted, []string{raw})
	if led.EmphasisChecked != 1 || led.EmphasisUnpainted != 0 {
		t.Fatalf("a painted emphasis: checked=%d unpainted=%d, want 1 and 0 (lines=%+v)", led.EmphasisChecked, led.EmphasisUnpainted, led.Lines)
	}
	if led.Status != AuditAgreed {
		t.Fatalf("status=%q, want %q for a faithful line", led.Status, AuditAgreed)
	}

	// UNPAINTED: the SAME grid, with the span's cells carrying the row's plain
	// style. Every other measurement is unchanged — this is the same input with one
	// variable flipped.
	unpainted := auditCandidate(117, drawn)
	unpainted.Dump.AttrRuns = [][]lineedit.DumpAttrRun{{{
		Col: 0, Cells: col, Style: plain,
	}, {
		Col: col, Cells: len(span), Style: plain,
	}, {
		Col: col + len(span), Cells: 117 - col - len(span), Style: plain,
	}}}
	led = AuditAnswer(unpainted, []string{raw})
	if led.EmphasisChecked != 1 || led.EmphasisUnpainted != 1 {
		t.Fatalf("an unpainted emphasis: checked=%d unpainted=%d, want 1 and 1", led.EmphasisChecked, led.EmphasisUnpainted)
	}
	if led.Status != AuditDisagreed || led.Disagreed != 0 {
		t.Fatalf("status=%q disagreed=%d, want %q and 0 — the TEXT arrived; only its emphasis did not",
			led.Status, led.Disagreed, AuditDisagreed)
	}
	if len(led.Lines) != 1 || led.Lines[0].Damage != DamageEmphasisUnpainted {
		t.Fatalf("the unpainted emphasis was not reported as its own damage class: %+v", led.Lines)
	}
	em := led.Lines[0].Emphasis
	if em == nil || em.Verdict != AuditEmphasisUnpainted || em.Baseline != plain {
		t.Fatalf("the emphasis record does not carry the measurement: %+v", em)
	}
	if em.PlainCells() != len(span) {
		t.Fatalf("plain cells = %d, want the whole span (%d)", em.PlainCells(), len(span))
	}
}

// TestAuditAnswer_EmphasisWithNoBaselineIsUnknownNotUnpainted: a row that carries
// ONE style has no plain text to compare against, so emphasis cannot be observed
// there. It is reported as UNKNOWN — neither counted as painted nor accused.
func TestAuditAnswer_EmphasisWithNoBaselineIsUnknownNotUnpainted(t *testing.T) {
	const raw = "one style everywhere: **still marked** here"
	drawn := lineedit.DisplayText(raw)
	cand := auditCandidate(117, drawn)
	cand.Dump.AttrRuns = [][]lineedit.DumpAttrRun{{{Col: 0, Cells: 117, Style: "default"}}}
	led := AuditAnswer(cand, []string{raw})
	if led.EmphasisUnknown != 1 || led.EmphasisUnpainted != 0 || led.EmphasisChecked != 0 {
		t.Fatalf("checked=%d unpainted=%d unknown=%d, want 0/0/1 (no baseline is not a loss)",
			led.EmphasisChecked, led.EmphasisUnpainted, led.EmphasisUnknown)
	}
	if led.Status != AuditAgreed {
		t.Fatalf("status=%q, want %q: an unmeasurable emphasis is not an accusation", led.Status, AuditAgreed)
	}
}

// TestAuditAnswer_EveryAuditedLineIsInTheTranscript is the audit's UNIT stated as a
// test: an audited line comes FROM the transcript, so the one-line core can only
// ever answer "both sides" or "the transcript only" here. The two other directions
// the core names (grid-only = a stale frame; neither = not this session's text)
// belong to `--compare`, where the line is the CALLER's and may be anything —
// which is why the audit's classifier has no branch for them: an unreachable
// branch is a claim the code cannot keep.
func TestAuditAnswer_EveryAuditedLineIsInTheTranscript(t *testing.T) {
	stale := "text the grid carries and the log does not"
	cand := auditCandidate(117, stale)
	led := AuditAnswer(cand, []string{"a transcript line the grid does not carry"})
	if led.Disagreed != 1 {
		t.Fatalf("disagreed = %d, want 1", led.Disagreed)
	}
	if led.Lines[0].Damage != DamagePaintLoss {
		t.Fatalf("damage = %q, want %q (the line is in the log by construction, and it fits the grid)", led.Lines[0].Damage, DamagePaintLoss)
	}
	if !led.Lines[0].Verdict.InTranscript {
		t.Fatal("the audited line was reported as absent from the transcript it came from")
	}
}
