package uiclient

// damage.go — the classification of a line the transcript holds and no single grid
// row carries, and the two classes that exist because the width WAS the wrong
// question to ask.
//
// WHY THIS FILE EXISTS (task 01a0e49e, re-measuring task 01a0e313's report). The
// audit could emit a verdict that contradicted its own evidence: on a row that opens
// with the WHOLE line and then continues with something else, `grid_prefix_runes`
// equalled the line's entire rune count — the paint carried all of it — while the
// verdict read `in_grid=false, damage=paint_loss` ("the paint lost your text"). The
// reader is sent to the wrong layer: what is damaged is the ROW (it still holds an
// older paint), not the line's text.
//
// Two classes are added, and NOTHING is widened into agreement:
//
//   - the WRAP stopped being a class at all, and that is the same fix one step
//     further: the whole line continued across consecutive rows ARRIVED, and the grid
//     can be ASKED (compareSequence) rather than assigned a name that says "no single
//     row carries it". Both faces now ask and report it as a match carrying
//     Rejoined/WrapRows (CompareVerdict / AuditLine). The class it replaces
//     (text_wrapped, still declared in audit.go) was reached through the verdict, and
//     arriving there while a MISSING row exact match was possible is exactly the
//     `--compare` defect of task 01a0e4f5: a loss reported beside a ledger that
//     counted the same line rejoined.
//
//   - damage_to_row: the whole line sits verbatim at the START of a grid row that
//     then continues with text the line does not have. The text is on the screen and
//     is not exactly alone: a re-paint that did not clear the row, or a second line
//     drawn over it. It stays a DISAGREEMENT (exit code unchanged) — a stale row is a
//     real, visible paint defect — but the damage it names is the row's, never the
//     line's text's.
//
// THE TAXONOMY DEFECT THIS ALSO FIXES. The wrap classes were reached by a WIDTH test
// (`displayWidth(line) > grid.Cols`) and `cjk_wrap` additionally by "wider in cells
// than in runes". Both are wrong questions where the observable is available: whether
// the grid actually reassembled the line is a FACT about the grid. Measured (this
// task, on master): a 180-cell ASCII line the grid carried wrapped across two rows
// came back `damage=wrap_artifact_or_loss` with rejoined=0 — the join instrument was
// never asked — and a 95-column ASCII line in a 117-column grid was reported
// `paint_loss` with `prefix == expected_runes`. So the wrap question is asked FIRST,
// of the grid, for every line at least as wide as the grid; the width classes remain
// as the fallback for a line the grid did NOT carry, where the width is genuinely the
// only evidence one frame offers.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// The two classes this file adds are DECLARED in audit.go (the vocabulary a reader
// greps for) and decided here.

// DamageArrived reports whether a class means "the text is on the grid", rather than
// "some layer lost it". It is the ONE predicate the ledger's counting and the
// rendering's wording both ask, so a class can never be arrival for one and a loss
// for the other.
//
// The two classes that used to be omitted from it are listed here, and each omission
// was the same defect: the ledger's repainted count treated DamageToRow as an arrival
// (the row is damaged, no text was lost) while this predicate, the one place that says
// what a class MEANS, called it a loss; and DamageEmphasisUnpainted is the class whose
// whole definition is "the text arrived and its emphasis did not". A class the
// predicate and the counters read differently is exactly the shape of bug this family
// is about, so both are fixed here rather than worked around at the call site.
func (d AuditDamage) DamageArrived() bool {
	switch d {
	case DamageNone, DamageTextWrapped, DamageRepaintTail, DamageToRow, DamageEmphasisUnpainted:
		return true
	default:
		return false
	}
}

// classifyLostLine names the damage of ONE line the transcript holds and whose
// row-exact containment failed. It is THE one place a loss class is decided, shared
// by the one-line compare (CompareTranscriptLine) and the whole-answer ledger
// (AuditAnswer reads the verdict's class), so the two faces cannot name the same row
// two different ways.
//
// The order of the questions is the whole content of this function, and it is
// deliberate — each one is asked only when the one before it could not answer:
//
//  1. Is the WHOLE line at the start of a row? Then nothing was lost, whatever the
//     rest of the row holds (damage_to_row). This is the check the reported
//     contradiction was missing: the containment answer and the prefix are two
//     measurements of the same row, and when the prefix covers the line they can no
//     longer disagree about whether the text is there.
//  2. Otherwise the width is the only evidence there is, and the classes say exactly
//     that: wider in cells than in runes (the layout's double-width arithmetic is the
//     candidate explanation), wider than the grid (a correct wrap explains it as well
//     as a loss does), or — the real-loss class — a line that FITS the grid and is on
//     no row, where no wrap can explain the absence.
//
// THE WRAP IS NOT DECIDED HERE, and that is the fix this function's own history asks
// for: a class is a name for a MEASUREMENT, and this one has none — the grid can be
// ASKED whether it reassembled the line (compareSequence, the wrap instrument), and
// the answer is an ARRIVAL rather than a loss class, so it is decided one step
// earlier by CompareTranscriptLine (case v.InTranscript), whose verdict both faces
// read. Above this point is the classifier's only window for the question.
// `DamageTextWrapped` therefore no longer leaves this function: a line the join
// reassembles is a MATCH (Rejoined), and a line it does not reassemble is a LOSS —
// which is what keeps a genuinely lost wide line reported instead of excused.
//
// (MERGE NOTE 01a0e54c, task/01a0e4f5f9cc787a9823040b8500c215: the pre-merge branch
// stated the same decision with a WIDER claim than it implemented — "asked of the
// GRID ... only when the line is wide enough that a wrap is the only way the grid
// could show it (see wrapWouldFit)". Master's form is kept: CompareTranscriptLine
// asks the join UNCONDITIONALLY and `wrapWouldFit` is deleted, so no arrival can be
// suppressed by a fact about the LINE. See the second MERGE NOTE below.)
func classifyLostLine(cand Candidate, line string, v CompareVerdict) AuditDamage {
	if v.WholeLineAtRowStart() {
		return DamageToRow
	}
	cols := lineedit.DisplayWidth(line)
	switch {
	case cols >= cand.Dump.Cols && cols > len([]rune(line)):
		return DamageCJKWrap
	case cols >= cand.Dump.Cols:
		return DamageWrapArtifactOrLoss
	default:
		return DamagePaintLoss
	}
}

// wrapArrivalReason renders the ONE sentence that describes a line the grid
// reassembled across consecutive rows — the evidence a reader re-does the join
// with. It is written ONCE because both faces report it (CompareTranscriptLine's
// verdict and AuditAnswer's ledger entry): the reported defect was a shape whose
// two faces contradicted each other, and two hand-written copies of "the text
// arrived" is exactly how that happens again (Rule 13's shape).
func wrapArrivalReason(rows []int) string {
	return fmt.Sprintf(
		"the line is on the grid WRAPPED across rows %v (band gutters removed): the text arrived, and the row-exact match failed only because the grid drew it on more than one row",
		rows)
}

// offGridReason renders the sentence for a line the grid Painted and the
// observed window no longer holds. It names the three numbers a reader needs to
// re-do the measurement (which row of the history, how long the history is, how
// tall the window is) and says the one thing that matters: nothing was lost, and
// nothing about the CURRENT frame was verified.
//
// It is written once and read once — CompareVerdict's status and this sentence
// are produced from the same branch, so the two cannot say different things
// (Rule 13's shape: the reported defect was a status and a sentence disagreeing).
func offGridReason(historyRow, historyRows, windowRows int) string {
	return reasonWithSide(CompareReasonSideOffGrid, fmt.Sprintf(
		"the line is in the grid's SAVED HISTORY (history row %d, %d row(s) of history beside a %d-row visible window): the surface painted it and the terminal has since pushed it off the top of the window this artifact observes. Nothing was lost, and nothing about the CURRENT frame was verified — this is UNDECIDABLE for the visible grid, not a disagreement",
		historyRow, historyRows, windowRows))
}

// reader re-does the arithmetic with. It is derived here rather than stored in
// CompareVerdict.RowTailRunes because that field has a narrower meaning the verdict
// already relies on (the re-paint tail of a row that CARRIES the line): -1 means "no
// row carries this line", and a damage_to_row row is by definition not a carry.
func tailPastFirstCopy(cand Candidate, line string, row int) int {
	if row < 0 || row >= len(cand.Dump.Lines) {
		return -1
	}
	text := stripCompareGutters(cand.Dump.Lines[row], cand.Surface)
	if !strings.HasPrefix(text, line) {
		return -1
	}
	return len([]rune(text)) - len([]rune(line))
}

// (The wrap question is no longer a separate helper: `classifyLostLine` asks the
// grid itself through compareSequence, and returns the rows as the class's evidence.)
//
// (MERGE NOTE 01a0e54c — DELIBERATE DROP of the pre-merge branch's `wrapWouldFit`.
// The branch kept a gate, `lineedit.DisplayWidth(line) >= cand.Dump.Cols`, in front
// of the join; master asks compareSequence unconditionally. The gate only ever
// suppressed ASKING about a line the grid had reassembled but which FIT the grid in
// cells (a wrap of exactly-grid-width text, or double-width runes counted as cells) —
// so it could only ever turn an arrival into a loss class, which is the defect this
// task reports. Dropping it strictly widens what the join is ASKED; it does not
// widen what the join ACCEPTS, because compareSequence still has to reproduce the
// line's whole text before the verdict becomes an arrival. The width test survives
// where it carries evidence: classifyLostLine names DamageCJKWrap /
// DamageWrapArtifactOrLoss from it when the join did NOT reproduce.)
