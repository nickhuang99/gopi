package uiclient

// audit.go — the WHOLE-ANSWER audit: `gopi ui_state --compare` rules on ONE line
// the caller already has in hand, and that is the wrong unit for the question the
// operator actually asks ("did the answer arrive on screen, or is something
// missing?"). A human would otherwise run the check once per line and read the
// verdicts by eye — which is exactly the "a human must point at the screen" the
// self-observation loop exists to remove.
//
// WHAT THIS ADDS, and what it deliberately does NOT. It does not re-implement the
// screen model, the dump reader, the geometry authority or the one-line
// containment core: every line goes through CompareTranscriptLine unchanged, so
// the refusals (unpainted grid, fallback geometry), the gutter handling and the
// exact-match rule stay in ONE place. What it adds is the UNIT, the LEDGER, the
// CLASSIFICATION, the DIRECTION and the EMPHASIS half.
//
// THE DAMAGE CLASSES it names, each derived from a verdict's own evidence rather
// than from a second look at the grid:
//
//   - paint_loss — in the transcript, not on the grid, and the line FITS the grid
//     width: no wrap can explain that, which is what makes it the real-loss class.
//   - wrap_artifact_or_loss — the same, except the line is WIDER than the grid, so
//     a correct wrap explains it as well as a loss does. One frame cannot tell the
//     two apart, and saying so is the honest answer: a reader told "the paint lost
//     it" would chase a renderer defect a narrow window explains.
//   - cjk_wrap — as above, and the line is wider in CELLS than in RUNES, so the
//     layout's double-width arithmetic is the candidate explanation: the CJK
//     continuation column the operator saw.
//   - repaint_tail — the line ARRIVED, on one row, and the row continues past it
//     with the line's own head re-drawn: a re-paint that did not clear the row's
//     tail. It is NOT a loss (the text is on the screen) and it is NOT folded into
//     matched: a ledger that called it paint_loss reported a faithful CJK paint as
//     lost (measured 2026-09-27, task 01a0e305), and a ledger that called it a plain
//     match would hide a real, visible paint defect. So it gets its own class and
//     its own count (AuditLedger.Repainted).
//   - damage_to_row — the line ARRIVED and the row it arrived on is NOT only it: the
//     row opens with the WHOLE line and continues with text the line does not have
//     (a re-paint that was not cleared, or a second line drawn over the tail). The
//     text is on the screen, so this is an arrival too — recorded, and counted as
//     Repainted — and the damage it names is the ROW's. It is the class the reported
//     contradiction (task 01a0e313: `in_grid=false, damage=paint_loss` beside
//     `prefix=95` of 95 runes) should have named; the one-line core reports it as
//     DamageToRow, and the ledger mirrors it rather than naming the same row a second
//     way. A row still carrying a stale paint IS a paint defect, which is why the
//     class exists rather than the row being folded into a plain match.
//   - (the WRAP is not a damage class: a line the grid reassembled across
//     consecutive rows ARRIVED, and both faces now ASK the join instrument
//     (compareSequence) and report it as a match carrying Rejoined/WrapRows —
//     CompareVerdict for the one-line check, AuditLine for the ledger. The ledger
//     has always counted it (Rejoined); the one-line core used to report a loss on a
//     line the grid reassembled, which is the defect this closed.)
//   - emphasis_unpainted — the line arrived, and the format marked part of it bold,
//     and the grid's per-cell attributes are IDENTICAL to the row's plain-text
//     style across that span: the emphasis was removed (or never applied) before
//     the paint.
//
// A line that is not on one grid row because the grid WRAPPED it is re-joined
// (compareSequence) and reported as arrived: the re-wrap is not a loss, and
// reporting it as one is the false signal this family of changes exists to remove.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/lineedit"
)

// AuditBound is how many transcript lines one audit checks. Unlike --compare's
// bound (a caller error to exceed: that flag checks a line the caller holds), this
// bound is on the TRANSCRIPT side — a unit the caller does not spell out — so
// exceeding it is not a usage error. The audit stops at the bound and SAYS it
// stopped (AuditLedger.Truncated), because a set quietly cut short reads exactly
// like a shorter answer.
const AuditBound = 400

// AuditStatus is the whole-answer verdict. It is a closed vocabulary rather than a
// bool for the same reason CompareStatus is: "every line arrived", "some lines did
// not" and "I could not check any of it" are three different facts, and the
// exit-code contract is derived from them (see uiStateExitCode in cmd/agent).
type AuditStatus string

const (
	// AuditAgreed: the check RAN and every audited line is on the grid.
	AuditAgreed AuditStatus = "agreed"
	// AuditDisagreed: the check ran and at least one line is not (or lost its
	// emphasis).
	AuditDisagreed AuditStatus = "disagreed"
	// AuditRefused: the check COULD NOT RUN — the grid may not be read as the
	// screen's layout (unpainted, or a fallback geometry), so nothing was verified
	// in EITHER direction.
	AuditRefused AuditStatus = "refused"
	// AuditNoTranscript: the grid was readable but there was no answer to audit.
	AuditNoTranscript AuditStatus = "no-transcript"
	// AuditNoAuthority: the grid was readable, but no authoritative text record
	// exists for this artifact at all — the surface that wrote it keeps no
	// transcript beside it and the caller named none. It is a SEPARATE word from
	// AuditNoTranscript on purpose: `no-transcript` says an authority was READ and
	// held nothing, this says there was no authority to read, and a reader who
	// cannot tell those apart cannot tell "the log is empty" from "this surface
	// has no log" (the measured complaint, task 01a0e62d).
	AuditNoAuthority AuditStatus = "no-authoritative-side"
	// AuditOffGrid: at least one line is OUTSIDE the observed window — the grid's
	// saved history holds it and the visible grid does not. It is the whole-answer
	// twin of CompareOffGrid, and its own word for the same reason: "every line I
	// could see agreed" and "some lines were not in the window at all" are
	// different findings, and folding the second into `agreed` is how a reader
	// came to believe an unchecked answer had been verified (task 01a1c05c).
	AuditOffGrid AuditStatus = "off-grid"
)

// AuditDamage is one disagreeing line's classification: which layer lost it, to
// the degree a single frame can tell.
type AuditDamage string

const (
	// DamageNone: nothing was lost for this line.
	DamageNone AuditDamage = ""
	// DamagePaintLoss: in the log, not on the grid, and narrow enough that no wrap
	// explains it.
	DamagePaintLoss AuditDamage = "paint_loss"
	// DamageWrapArtifactOrLoss: in the log, not on the grid, and wider than the
	// grid — a correct wrap explains it as well as a loss does.
	DamageWrapArtifactOrLoss AuditDamage = "wrap_artifact_or_loss"
	// DamageCJKWrap: as above, and wider in CELLS than in RUNES (a wide-rune line),
	// so the layout's own double-width arithmetic is the candidate explanation.
	DamageCJKWrap AuditDamage = "cjk_wrap"
	// DamageGridOnly: on the painted grid, not in the log — a stale frame, or
	// another surface's screen.
	DamageGridOnly AuditDamage = "grid_only"
	// DamageNeither: in neither side, so no layer can be blamed (a typo, or the
	// wrong transcript).
	DamageNeither AuditDamage = "neither"
	// DamageEmphasisUnpainted: the text arrived and its emphasis did not.
	DamageEmphasisUnpainted AuditDamage = "emphasis_unpainted"
	// DamageRepaintTail: the line arrived on ONE grid row, and the row continues
	// past it with the line's own head re-drawn — a re-paint that never cleared the
	// row's tail. The text is there (the whole line, on one row), so this is NOT a
	// loss: it is the class the byte-exact row match used to misname paint_loss.
	DamageRepaintTail AuditDamage = "repaint_tail"
	// DamageTextWrapped: the whole line is on the grid CONTINUED across consecutive
	// rows — it arrived, and no single row is the line. It is the one-line core's
	// answer for the case the ledger records as `rejoined`; the arrival classes and
	// the ONE classifier that decides them are in damage.go.
	DamageTextWrapped AuditDamage = "text_wrapped"
	// DamageToRow: the whole line is verbatim at the START of a grid row that then
	// continues with text the line does not have — a stale re-paint that was not
	// cleared, or another line drawn over its tail. The text ARRIVED and is not
	// exactly alone: the damage is the ROW's, never the line's text's. It is the
	// class the reported verdict (task 01a0e313: `in_grid=false, damage=paint_loss`
	// beside `prefix=95` of 95 runes) should have named.
	DamageToRow AuditDamage = "damage_to_row"
)

// AuditEmphasisVerdict is the emphasis half's own closed vocabulary, kept
// three-valued on purpose: "it was painted", "it was NOT painted" and "this
// artifact cannot say" are three facts, and collapsing the third into either of
// the others would report a measurement that never happened.
type AuditEmphasisVerdict string

const (
	AuditEmphasisPainted   AuditEmphasisVerdict = "painted"
	AuditEmphasisUnpainted AuditEmphasisVerdict = "unpainted"
	AuditEmphasisUnknown   AuditEmphasisVerdict = "unknown"
)

// AuditEmphasis is the emphasis half of ONE line: the runs the FORMAT marked (the
// tokenizer's own answer about the raw transcript line — the marker's spelling is
// never re-typed here) and whether the GRID's per-cell attributes carry a distinct
// emphasis across them.
type AuditEmphasis struct {
	// Verdict: painted / unpainted / unknown (see AuditEmphasisVerdict).
	Verdict AuditEmphasisVerdict `json:"verdict"`
	// Spans are the bold runs the format marked, with their measurement.
	Spans []AuditEmphasisSpan `json:"spans"`
	// Row is the grid row the spans were measured on (-1 when the line has no row).
	Row int `json:"row"`
	// Baseline is the row's PLAIN-text style, the one the emphasised cells are
	// compared against: the emphasis is visible in a cell grid only as a style
	// DISTINCT from the text around it.
	Baseline string `json:"baseline"`
	// Reason says why the verdict is not "painted" ("" when it is).
	Reason string `json:"reason"`
}

// AuditEmphasisSpan is one expected bold run and where it was looked for.
type AuditEmphasisSpan struct {
	Text string `json:"text"`
	// Cells is the span's width in CELLS (a wide rune is two).
	Cells int `json:"cells"`
	// PlainCells is how many of those cells carried the row's PLAIN style — i.e.
	// how much of the run shows no emphasis at all. 0 = fully emphasised.
	PlainCells int `json:"plain_cells"`
	// Col is the rune column the span was located at, or -1 when its text is not
	// on the row at all (a missing run, reported as fully plain).
	Col int `json:"col"`
}

// AuditLine is the audit's per-line record for a line that did NOT arrive as a plain
// row-exact hit: the one-line verdict, the classification, and the line's own geometry
// — so a reader sees why the class came out that way and can redo the arithmetic.
//
// ONE ENTRY PER SUCH LINE, each carrying ONE verdict about it. The ledger's per-line
// counters are DERIVED from these entries (see AuditLedger.auditCountsFromEntries), so
// the list and the counts cannot disagree about how many lines disagreed or rejoined —
// and an arrival record (a wrap, a re-paint tail, a row carrying text past the line)
// can never sit in the same list as a disagreement count without being counted as the
// arrival it is.
type AuditLine struct {
	// Index is the line's 0-based position in the audited answer. The line's own
	// text is Verdict.Line, so a ledger is readable without the transcript.
	Index int `json:"index"`
	// Verdict is the one-line containment answer, unchanged (the authority).
	Verdict CompareVerdict `json:"verdict"`
	// Damage is the classification.
	Damage AuditDamage `json:"damage"`
	// ExpectedCols is the line's width in CELLS (a CJK rune is two) and
	// ExpectedRunes its width in runes; GridCols is the width the verdict was
	// computed at. A wide line is wider in cells than in runes, which is what makes
	// the continuation column visible in the numbers.
	ExpectedCols  int `json:"expected_cols"`
	ExpectedRunes int `json:"expected_runes"`
	GridCols      int `json:"grid_cols"`
	// GridRow is the grid row the line was found on, or -1.
	GridRow int `json:"grid_row"`
	// Rejoined reports that the line was NOT on one grid row because the grid
	// WRAPPED it: its text was reassembled from consecutive rows, so the answer
	// arrived and Damage is empty. A ledger keeps it because "the wrap is not a
	// loss" is a fact a reader has to be able to SEE, not infer from an absence —
	// and it is the fact the ledger's `rejoined` counter is DERIVED from, so the
	// count and the entry that justifies it cannot disagree.
	Rejoined bool `json:"rejoined"`
	// RowTailRunes is the tail the row carried past the line — the evidence of a
	// RE-PAINT that did not clear the row (see DamageRepaintTail). 0 for a row that
	// is exactly the line, -1 when the line is not on a row at all.
	RowTailRunes int `json:"row_tail_runes"`
	// WrapRows are the grid rows the line was reassembled from (empty unless
	// Rejoined). They are what makes the counter checkable rather than a number to
	// trust: the verdict's sentence NAMES them, so a reader can re-do the join.
	WrapRows []int `json:"wrap_rows,omitempty"`
	// Emphasis is present when the format marked this line and the emphasis could
	// not be confirmed (see AuditEmphasis).
	Emphasis *AuditEmphasis `json:"emphasis,omitempty"`
}

// AuditLedger is the whole-answer answer: what was checked, how it came out, and
// what the audit could not decide.
type AuditLedger struct {
	// Status is the whole-answer verdict; the process exit code is derived from it.
	Status AuditStatus `json:"status"`
	// Reason explains a refusal or an empty transcript ("" otherwise).
	Reason string `json:"reason"`
	// Geometry / Cols / Rows / Frames are the grid's provenance, so the ledger
	// carries the geometry every verdict in it was computed at.
	Geometry GeometrySource `json:"geometry"`
	Cols     int            `json:"cols"`
	Rows     int            `json:"rows"`
	Frames   int            `json:"frames"`
	// TranscriptLines is how many lines the answer had; Audited is how many were
	// checked, Skipped how many were left out because there was nothing to check
	// (a blank line appears inside every grid row, so a verdict on it would be
	// vacuously true — it is excluded and COUNTED, never silently dropped).
	TranscriptLines int `json:"transcript_lines"`
	Audited         int `json:"audited"`
	Skipped         int `json:"skipped"`
	// Truncated reports that the bound stopped the audit before the answer ended.
	Truncated bool `json:"truncated"`
	// Matched / Disagreed are the counts the operator asks for first. Rejoined is
	// how many lines arrived only AFTER the grid's wrap was joined — counted
	// separately because it is the number that says how much of the answer arrived
	// wrapped rather than on one row, and every line it counts has an entry naming
	// the rows it was reassembled from (WrapRows). Repainted is how many arrived
	// on one row that a re-paint had left a tail on (DamageRepaintTail /
	// DamageToRow) — also counted separately, and NOT added to Disagreed: its text
	// is on the screen, so a ledger that moved this number into "disagreed" would
	// keep reporting the false loss this class was added to end.
	//
	// None of these four is incremented where the auditing happens: they are all
	// derived from Lines in ONE place (auditCountsFromEntries), so the counters and
	// the entries that justify them are the same facts read twice.
	Matched   int `json:"matched"`
	Disagreed int `json:"disagreed"`
	Rejoined  int `json:"rejoined"`
	Repainted int `json:"repainted"`
	// LostText is how many audited lines the transcript holds and the grid does not —
	// lines whose damage is a LOSS of text (paint_loss / wrap_artifact_or_loss /
	// cjk_wrap) rather than damage to a row that carried them. It is the number the
	// whole-answer STATUS is derived from (see auditStatus), and it is separate from
	// Disagreed because a disagreement also counts a line whose damage is the ROW's
	// (damage_to_row): that one is recorded, and it must not decide the status as if
	// the audit had lost text.
	LostText int `json:"lost_text"`
	// OffGrid is how many audited lines were OUTSIDE the observed window: the grid's
	// saved history holds them, the visible grid does not (see CompareOffGrid). They
	// are neither matched nor disagreed — there was nothing on the visible grid to
	// match them against — which is why they are their own count, and why the residue
	// arithmetic states them explicitly (Matched = Audited − Disagreed − OffGrid). A
	// line nobody could see must never inflate `matched`.
	OffGrid int `json:"off_grid"`
	// Lines holds one entry per line that did NOT arrive as a plain row-exact hit:
	// a disagreement, a line the grid WRAPPED (Verdict.Status=CompareRejoined — it
	// arrived, and the entry is evidence that the wrap was recognized rather than
	// counted as a loss, naming the rows it was reassembled from), a line outside the
	// observed window (CompareOffGrid), or a line a re-paint left a tail on
	// (DamageRepaintTail / DamageToRow — it arrived too, on one row). An answer is
	// thousands of lines and the question is what went wrong, so a clean run prints a
	// summary rather than handing the whole text back.
	// EVERY counter above that classifies a line is derived from this list: the
	// arithmetic closes by construction (Matched = Audited − Disagreed − OffGrid).
	Lines []AuditLine `json:"lines"`
	// EmphasisChecked is how many audited lines the format marked AND whose
	// emphasis was measurable; EmphasisUnpainted is how many of those showed no
	// emphasis on the grid; EmphasisUnknown is how many could not be measured from
	// this artifact (attrs not recorded, no plain baseline) — reported rather than
	// folded into either count.
	EmphasisChecked   int `json:"emphasis_checked"`
	EmphasisUnpainted int `json:"emphasis_unpainted"`
	EmphasisUnknown   int `json:"emphasis_unknown"`
}

// AuditAnswer checks EVERY line of the authoritative answer against the grid.
//
// raw is the transcript as the SESSION LOG holds it: the model's own text, one
// entry per line, markers INCLUDED — the same text `gopi session log <id> --response
// --text` prints. It is the only single input this needs, and that is deliberate:
// the alternative (a second, "drawn" slice passed in beside it) is two slices that
// can be mis-paired by a caller, and the pairing IS the correctness of the
// comparison. The drawn text is derived HERE, through the format layer's own
// decision (see auditDrawnLines), never re-derived by the caller.
//
// WHY THE DRAWN TEXT, NOT THE RAW ONE. An answer line's `**markers**` occupy no
// cell: the band removes them, so the grid carries the DRAWN text. A containment
// test on the raw line can therefore never match an emphasised line — the very
// case this audit exists for (the operator's "`**bold**` silently not painted") —
// and would report it as a missing line instead. `gopi ui_state --compare` (the
// pre-existing per-line check) compares the line the CALLER typed, which is the
// caller's business; the audit compares what the surface was supposed to draw.
//
// The order is the one the one-line compare established: the grid's own
// qualification comes first, and a grid that may not be read as a screen refuses
// the WHOLE audit — a per-line "refused" would say the same sentence N times and
// bury the fact that nothing was checked.
func AuditAnswer(cand Candidate, raw []string) AuditLedger {
	led := AuditLedger{
		Geometry:        GeometryOf(cand),
		Cols:            cand.Dump.Cols,
		Rows:            cand.Dump.Rows,
		Frames:          cand.Frames,
		TranscriptLines: len(raw),
	}
	if refusal := CompareGridUnusable(cand); refusal != "" {
		led.Status = AuditRefused
		led.Reason = refusal
		return led
	}
	if len(raw) == 0 {
		led.Status = AuditNoTranscript
		led.Reason = "the transcript has no answer lines: there is nothing to check against the grid, so no comparison can be claimed"
		return led
	}
	led.Status = AuditAgreed
	led.Truncated = len(raw) > AuditBound
	lines, styles := auditDrawnLines(raw)
	for i, line := range lines {
		if i >= AuditBound {
			break
		}
		if strings.TrimSpace(line) == "" {
			// A blank line is inside every row, so an "it matched" answer would be
			// vacuous. Counted, so the ledger's arithmetic closes.
			led.Skipped++
			continue
		}
		led.Audited++
		v := CompareTranscriptLine(line, cand, lines)
		if v.Status == CompareMatch {
			led.Matched++
			if v.Rejoined {
				// ARRIVED, WRAPPED: the one-line core ASKED the join instrument and it
				// reassembled the line's whole text from consecutive rows (see
				// CompareVerdict.Rejoined/WrapRows). The ledger has counted this since
				// it existed (Rejoined: "matched only AFTER the grid's wrap was
				// joined"), and it is derived from the verdict rather than re-run here,
				// so the two audit faces report ONE measurement — the defect this
				// closes is a `--compare` that printed a loss beside an `--all` that
				// counted the same line rejoined (task 01a0e4f5).
				//
				// THE WRAP KEEPS ITS OWN LEDGER ENTRY: a reader has to be able to SEE
				// that the row-exact match failed because the grid wrapped the line —
				// "the wrap is not a loss" is a fact, and inferring it from an absence
				// of lines is what the Rejoined count exists to prevent.
				//
				// THE LEDGER NAMES THE ARRIVAL `rejoined`, while the ONE-LINE face
				// keeps master's `match`+Rejoined: the two faces answer different
				// questions, and the status word is the answer to each. `--compare`
				// answers "is this line contained on the grid?" — yes, CompareMatch,
				// with Rejoined recording the SHAPE. The ledger answers "what happened
				// to this line?" — the row-exact rule did not match it and the join
				// reassembled it, which in the ledger's vocabulary is CompareRejoined,
				// the constant the counters read. One fact (this line was wrapped and
				// arrived), named in each face's own word, with AuditLine.Rejoined and
				// CompareVerdict.Rejoined carrying it identically across the boundary.
				wrapVerdict := v
				wrapVerdict.Status = CompareRejoined
				led.Lines = append(led.Lines, AuditLine{
					Index:         i,
					Verdict:       wrapVerdict,
					Damage:        DamageNone,
					ExpectedCols:  lineedit.DisplayWidth(line),
					ExpectedRunes: len([]rune(line)),
					GridCols:      cand.Dump.Cols,
					GridRow:       v.GridRow,
					RowTailRunes:  v.RowTailRunes,
					Rejoined:      true,
					WrapRows:      v.WrapRows,
				})
				led.auditEmphasisOf(cand, i, line, raw[i], styles[i])
				continue
			}
			if v.RowTailRunes > 0 {
				// ARRIVED ON A ROW THAT CONTINUES PAST IT: the row opens with the
				// whole line and then re-draws the line's own head (see
				// CompareVerdict.RowTailRunes). The text is on the screen, so this is
				// an ARRIVAL — the ledger stays agreed and the line counts as matched
				// (see auditCountsFromEntries) — and it is recorded as its own class
				// because the row-exact match failed and a reader must be able to see
				// that, rather than infer it from a line that no longer appears
				// anywhere. Measured (task 01a0e305, 2026-09-27, real 117x26 pty): a
				// faithful 33-rune CJK answer line whose row carried a 29-rune tail,
				// reported as damage=paint_loss before this class existed.
				entry := AuditLine{
					Index:         i,
					Verdict:       v,
					Damage:        DamageRepaintTail,
					ExpectedCols:  lineedit.DisplayWidth(line),
					ExpectedRunes: len([]rune(line)),
					GridCols:      cand.Dump.Cols,
					GridRow:       v.GridRow,
					RowTailRunes:  v.RowTailRunes,
				}
				entry.Verdict.Reason = fmt.Sprintf(
					"the row carries the whole line and continues with the line's own head re-drawn (%d runes): the text arrived, and the row-exact match failed only because a re-paint did not clear the row's tail",
					v.RowTailRunes)
				led.Lines = append(led.Lines, entry)
			}
			// A row-exact hit with nothing to report leaves NO entry: the ledger says
			// what went wrong, and the counters reconcile it (a clean line is
			// Audited-and-not-Disagreed).
			led.auditEmphasisOf(cand, i, line, raw[i], styles[i])
			continue
		}
		entry := AuditLine{
			Index:         i,
			Verdict:       v,
			ExpectedCols:  lineedit.DisplayWidth(line),
			ExpectedRunes: len([]rune(line)),
			GridCols:      cand.Dump.Cols,
			GridRow:       v.GridRow,
			RowTailRunes:  v.RowTailRunes,
			WrapRows:      v.WrapRows,
			Rejoined:      v.Rejoined,
		}
		// OFF THE WINDOW, NOT LOST: the one-line core asked the grid's SAVED HISTORY
		// and reassembled the line there (see CompareOffGrid). The ledger records it
		// and — this is the half that matters — COUNTS it, because the whole-answer
		// status must not say "agreed" over a line the visible grid never showed: an
		// unchecked line is not a verified one, and the measured defect is exactly a
		// definite-sounding status standing in for "I could not see it" (task
		// 01a1c05c). The line is NOT a disagreement either: nothing contradicts
		// anything, and calling it one would send the reader to look for a loss.
		if v.Status == CompareOffGrid {
			entry.Damage = DamageNone
			entry.Verdict.Reason = v.Reason
			led.Lines = append(led.Lines, entry)
			continue
		}
		// THE CLASS COMES FROM THE VERDICT, not from a second look at the grid: the
		// one-line core ran the ONE classifier (see CompareVerdict.Damage /
		// classifyLostLine), so the two audit faces cannot name the same row two
		// different ways.
		entry.Damage = v.Damage
		// (MERGE NOTE 01a0e54c — DELIBERATE DROP of the pre-merge branch's
		// `switch v.Status { case CompareRejoined: …append entry… continue }`.
		// Master's wrap path is taken above: the arrival is caught by the
		// `v.Status == CompareMatch && v.Rejoined` branch, which appends the entry
		// and labels it CompareRejoined. So a wrap never reaches this point, and the
		// branch's arm here was UNREACHABLE dead code — an entry producer that could
		// never run. It is dropped rather than kept, because two producers of one
		// arrival is how the counters and the list drifted apart in the first place
		// (the defect this task reports).
		//)
		// A DISAGREEMENT: the verdict says the line is on exactly one side and the
		// class says which side lost it. Its counters are NOT incremented here — the
		// whole loop's counters are derived from this list after it closes (see
		// auditCountsFromEntries), so an entry and the count that summarises it are
		// one fact read twice instead of two facts that happen to agree. That is the
		// reported defect (task 01a0e4f5): five increments at five points of this
		// loop versus six appends to this list is how ONE line came to be counted as
		// a disagreement AND as a rejoin, with `rejoined: 1` naming no entry at all.
		entry.Emphasis = auditExpectedEmphasis(cand, raw[i], styles[i])
		led.Lines = append(led.Lines, entry)
	}
	led.auditCountsFromEntries()
	led.Status = auditStatus(led)
	return led
}

// auditCountsFromEntries DERIVES the ledger's per-line counters from its own entry
// list, so a counter and the list it summarises cannot disagree. It is the fix for the
// reported defect (task 01a0e4f5), and the shape of the fix is the point: the counters
// used to be incremented at five separate points of the auditing loop, while the entry
// that DEscribed the same line was appended at six — so ONE line could be counted as a
// disagreement (the branch it entered) and as a rejoin (the classification it carried)
// at the same time, and the ledger printed `disagreed: 1` beside two mismatch entries
// with `rejoined: 1` naming no line at all. Nothing in the loop could notice, because
// the numbers and the list were never asked to agree.
//
// The rules, one per counter, all read off AuditLine.Verdict.Status — the verdict the
// per-line face printed, so the two faces cannot name one line two ways:
//
//   - CompareMismatch   → Disagreed (so `disagreed` counts every mismatch entry, and
//     a mismatch entry is the only thing it counts);
//   - CompareRejoined   → Rejoined (and the entry names the rows, see WrapRows);
//   - every other entry → an ARRIVAL (a row-exact hit a re-paint left a tail on, a
//     row carrying the whole line plus something else, a line whose emphasis
//     vanished) — recorded, and never counted as a disagreement;
//   - Repainted is the arrival entries whose damage is the ROW's (repaint_tail /
//     damage_to_row), the two classes that exist so a paint defect stays visible;
//   - LostText is the disagreeing entries whose class means the text was NOT on the
//     grid (see Damage.DamageArrived) — the count the whole-answer status is derived
//     from (see auditStatus);
//   - Matched is the residue, Audited − Disagreed: every audited line is either a
//     disagreement or an arrival, so the two counts close the arithmetic BY
//     CONSTRUCTION rather than by two increments that happen to agree.
//
// ONE ENTRY PER LINE is what makes this sound, and it holds by construction: an
// audited line yields exactly one entry (a clean row-exact hit yields none, and a
// matching line's emphasis is measured at most once), and every other path `continue`s
// after appending.
func (led *AuditLedger) auditCountsFromEntries() {
	var rejoined, repainted, disagreed, lost, offGrid int
	for _, e := range led.Lines {
		if e.Verdict.Status == CompareOffGrid {
			// OUTSIDE THE WINDOW: counted on its own, never as an arrival and never as
			// a loss. Asked FIRST so no later rule can reclassify it.
			offGrid++
			continue
		}
		// ARRIVED is the class's own answer (Damage.DamageArrived — ONE predicate for
		// "the text is on the grid"), asked FIRST so the class and the counting rule
		// cannot disagree about it.
		arrived := e.Damage.DamageArrived()
		switch {
		case e.Damage == DamageRepaintTail || e.Damage == DamageToRow:
			// The text arrived and the ROW is what is damaged — the two classes that
			// exist so a paint defect stays visible WITHOUT being reported as a loss.
			repainted++
		case arrived:
			// ARRIVED. Whatever else the entry records has its own counter: a wrap
			// names the rows it was reassembled from (WrapRows), an emphasis that
			// vanished is counted by EmphasisUnpainted. Neither is a disagreement —
			// `disagreed` counts the entries whose class says the text was LOST, and
			// calling an arrival a disagreement is the contradiction this whole
			// change removes (a status and a count derived from different facts).
			//
			// The REJOIN is read off the entry's own Rejoined flag, which is the SAME
			// field master's wrap path sets and the one the `rejoined` sentence is
			// about — not off a status word. Both faces name this arrival the same
			// way (CompareVerdict.Rejoined / AuditLine.Rejoined: "the grid wrapped
			// it"), so a counter keyed on a status would have to keep a second
			// vocabulary of statuses in step with it, which is exactly the two-facts
			// drift this derivation exists to remove.
			if e.Verdict.Status == CompareRejoined {
				rejoined++
			}
		default:
			// The class says the text is NOT on the grid: the entry and the count are
			// the same fact, and LostText is the number the whole-answer status is
			// derived from (see auditStatus).
			disagreed++
			lost++
		}
	}
	led.Rejoined = rejoined
	led.Repainted = repainted
	led.Disagreed = disagreed
	led.LostText = lost
	// Matched is the residue over what was AUDITED, and an off-grid line is
	// neither a match nor a disagreement — it was not visible to be matched. So it
	// is subtracted here explicitly rather than silently inflating `matched` with a
	// line nobody verified (the arithmetic still closes: audited = matched +
	// disagreed + offGrid).
	led.Matched = led.Audited - disagreed - offGrid
	led.OffGrid = offGrid
}

// auditDrawnLines derives the text the SURFACE must draw for each transcript line,
// and the line style that decision came from. Both come from the format layer: the
// composer classifies fences/diffs (its own stateful replay, in transcript order),
// and DisplayText removes the intra-line markers — a line inside a fence is CODE
// and keeps its markers as literal text, a plain line does not.
func auditDrawnLines(raw []string) ([]string, []lineedit.LineStyle) {
	lines := make([]string, len(raw))
	styles := make([]lineedit.LineStyle, len(raw))
	var composer lineedit.LineComposer
	for i, r := range raw {
		st := composer.StyleFor(r)
		styles[i] = st
		if st == lineedit.LinePlain {
			lines[i] = lineedit.DisplayText(r)
			continue
		}
		lines[i] = r
	}
	return lines, styles
}

// auditStatus derives the whole-answer verdict from the ledger's own count of LOST
// lines and unpainted emphasis, so the rule lives in one place rather than in the
// caller's head.
//
// It reads LostText, not Disagreed — the two were the same number until DamageToRow
// existed and they are not the same fact now: a row that carries the whole line plus
// something else is RECORDED (it is a real, visible paint defect) and counted in
// Disagreed, but no text was lost. Deriving the verdict from Disagreed would let a
// class that means "the ROW is damaged" decide the status as if the audit had lost
// text (measured: a damaged-but-arrived row printed `outcome=disagreed` beside
// `matched=1 disagreed=0`).
func auditStatus(led AuditLedger) AuditStatus {
	if led.LostText > 0 || led.EmphasisUnpainted > 0 {
		return AuditDisagreed
	}
	if led.OffGrid > 0 {
		// SOME LINES WERE NOT IN THE WINDOW: the audit cannot claim agreement over
		// lines it never saw. `agreed` here would be the whole-answer twin of the
		// measured defect — a definite-sounding status standing in for "I could not
		// see it" (task 01a1c05c).
		return AuditOffGrid
	}
	return AuditAgreed
}

// auditEmphasisOf is the emphasis half of a line that MATCHED: the text arrived,
// and its emphasis is a separate claim. A ledger keeps disagreeing lines only, so
// an unpainted match is appended here — the operator's second damage class is
// exactly a line that is present with its bold missing.
func (led *AuditLedger) auditEmphasisOf(cand Candidate, i int, line, raw string, style lineedit.LineStyle) {
	em := auditExpectedEmphasis(cand, raw, style)
	if em == nil {
		return
	}
	switch em.Verdict {
	case AuditEmphasisPainted:
		led.EmphasisChecked++
	case AuditEmphasisUnknown:
		led.EmphasisUnknown++
	default:
		led.EmphasisChecked++
		led.EmphasisUnpainted++
		led.Lines = append(led.Lines, AuditLine{
			Index: i,
			Verdict: CompareVerdict{
				Status: CompareMatch, Line: line, InGrid: true, InTranscript: true,
				GridRow: em.Row, Geometry: led.Geometry, Cols: led.Cols, Rows: led.Rows,
				Frames: led.Frames,
			},
			Damage:        DamageEmphasisUnpainted,
			ExpectedCols:  lineedit.DisplayWidth(line),
			ExpectedRunes: len([]rune(line)),
			GridCols:      led.Cols,
			GridRow:       em.Row,
			Emphasis:      em,
		})
	}
}

// auditExpectedEmphasis is the emphasis check for ONE raw transcript line under
// the line style the composer gave it: the format's own tokenizer says what should
// be emphasised, and the grid's per-cell attributes say what was painted. A line
// with no marker — or a line inside a fence, where a marker is CODE and the
// renderer draws it literally (the rowFor gate, asked through the composer's own
// verdict rather than re-decided here) — is not an emphasis case at all (nil), so
// the common line costs nothing and the fence case is never counted as a loss.
//
// WHAT IT CAN AND CANNOT SAY. A cell grid shows emphasis only as a style DISTINCT
// from the text around it, so the measurement is "across the span the format
// marked, is every cell's style different from this row's plain-text style?" — the
// same observable the live-surface emphasis test asserts on. It therefore reports a
// LOSS when the emphasis vanished, and cannot by itself name which style was
// intended. A grid that records no attributes, or a row whose whole width carries
// ONE style (no plain baseline to compare against), is reported as UNKNOWN rather
// than guessed in either direction.
func auditExpectedEmphasis(cand Candidate, raw string, style lineedit.LineStyle) *AuditEmphasis {
	if style != lineedit.LinePlain || !lineedit.HasBoldMarker(raw) {
		return nil
	}
	spans := lineedit.DisplaySpans(raw)
	em := &AuditEmphasis{Verdict: AuditEmphasisUnknown, Row: -1}
	text := lineedit.SpanText(spans)
	if row := auditRowForText(cand, text); row >= 0 {
		em.Row = row
	} else {
		em.Reason = "the line is not on the grid, so its emphasis has no row to measure: the text's own absence is the damage, not the emphasis"
		return em
	}
	if !cand.Dump.AttrsRecorded || len(cand.Dump.AttrRuns) == 0 {
		em.Reason = "this artifact records no per-cell attributes (attrs_recorded=false): the emphasis can be neither confirmed nor denied here, and this is NOT a report that it is missing"
		return em
	}
	base, ok := auditBaselineStyle(cand, em.Row)
	if !ok {
		em.Reason = "the row carries a single attribute run, so there is no plain-text baseline to compare the emphasised cells against: emphasis cannot be observed on this row"
		return em
	}
	em.Baseline = base
	gridRow := cand.Dump.Lines[em.Row]
	for _, sp := range spans {
		if sp.Emphasis != lineedit.EmphasisBold {
			continue
		}
		run := AuditEmphasisSpan{Text: sp.Text, Col: -1, Cells: lineedit.DisplayWidth(sp.Text)}
		col := auditFindText(gridRow, sp.Text)
		if col < 0 {
			// A marked run whose text is not on the row is a loss of that run;
			// reported as fully plain so the count is never silently zero.
			run.PlainCells = len([]rune(sp.Text))
			em.Spans = append(em.Spans, run)
			continue
		}
		run.Col = col
		for c := col; c < col+len([]rune(sp.Text)); c++ {
			if auditCellStyle(cand, em.Row, c) == base {
				run.PlainCells++
			}
		}
		em.Spans = append(em.Spans, run)
	}
	em.Verdict = AuditEmphasisPainted
	for _, run := range em.Spans {
		if run.PlainCells > 0 {
			em.Verdict = AuditEmphasisUnpainted
		}
	}
	if em.Verdict == AuditEmphasisUnpainted {
		em.Reason = fmt.Sprintf("the format marked this text emphasised and its cells carry the row's PLAIN style (%s) across %d of %d cells: the emphasis was removed, or never applied, before the paint",
			base, em.PlainCells(), em.SpanCells())
	}
	return em
}

// PlainCells totals how much of the emphasised run showed NO emphasis — the
// number both the ledger's counts and the reason sentence quote, defined once
// (Rule 14) so the printed total and the sentence can never disagree.
func (e *AuditEmphasis) PlainCells() int {
	n := 0
	for _, sp := range e.Spans {
		n += sp.PlainCells
	}
	return n
}

// SpanCells totals the emphasised run's width in cells.
func (e *AuditEmphasis) SpanCells() int {
	n := 0
	for _, sp := range e.Spans {
		n += sp.Cells
	}
	return n
}

// auditBaselineStyle reports the style the row's PLAIN text carries: the style
// covering the most cells of the row. It is taken from the row itself rather than
// from a palette, because the question "is this cell emphasised" is a question
// about that row's own paint. ok=false when the row has one style only — then
// there is no baseline, and the caller reports UNKNOWN instead of counting every
// emphasised cell as plain.
func auditBaselineStyle(cand Candidate, row int) (string, bool) {
	if row < 0 || row >= len(cand.Dump.AttrRuns) {
		return "", false
	}
	runs := cand.Dump.AttrRuns[row]
	if len(runs) < 2 {
		return "", false
	}
	best, bestCells := "", 0
	for _, run := range runs {
		if run.Cells > bestCells {
			best, bestCells = run.Style, run.Cells
		}
	}
	return best, best != ""
}

// auditCellStyle is the style one cell of a grid row was painted with ("" when the
// row or column is outside the recorded runs).
func auditCellStyle(cand Candidate, row, col int) string {
	if row < 0 || row >= len(cand.Dump.AttrRuns) || col < 0 {
		return ""
	}
	for _, run := range cand.Dump.AttrRuns[row] {
		if col >= run.Col && col < run.Col+run.Cells {
			return run.Style
		}
	}
	return ""
}

// auditFindText locates a run of text inside a grid row and returns its 0-based
// RUNE column, or -1. Runes are the grid's columns for a row's TEXT
// (ScreenDump.Lines renders one entry per cell, skipping a wide rune's
// continuation column), which is the unit the attr runs index.
func auditFindText(row, text string) int {
	if text == "" {
		return -1
	}
	rr, t := []rune(row), []rune(text)
	for i := 0; i+len(t) <= len(rr); i++ {
		if string(rr[i:i+len(t)]) == text {
			return i
		}
	}
	return -1
}

// auditRowForText finds the grid row whose drawn text carries want, or -1. It is
// the ONE locator the emphasis half uses: asking the row from the omission-free
// text keeps the emphasis check and the containment check pointed at the same row.
func auditRowForText(cand Candidate, want string) int {
	if want == "" {
		return -1
	}
	for i, row := range cand.Dump.Lines {
		if stripCompareGutters(row, cand.Surface) == want {
			return i
		}
	}
	for i, row := range cand.Dump.Lines {
		if auditFindText(stripCompareGutters(row, cand.Surface), want) >= 0 {
			return i
		}
	}
	return -1
}

// auditWrapRow renders a grid row as the WRAP join reads it: the band's marker
// column removed, and the blank marker column a CONTINUATION row carries removed
// too (the band writes a wrap's marker as blank cells of the marker's own width —
// internal/lineedit's writeGutter(glyph=false) — so a wrapped row starts with that
// many spaces, not with a glyph). The bound is taken from the markers themselves
// (CompareGutters) rather than typed here. Trailing blanks are NOT touched: a grid
// row is padded to the full width, but a real space can also sit at the boundary
// the terminal wrapped at, and the two are only told apart by trying both (see
// compareSequence).
func auditWrapRow(row string, surface Surface) string {
	row = stripCompareGutters(row, surface)
	limit := auditGutterCols(surface)
	for n := 0; n < limit && strings.HasPrefix(row, " "); n++ {
		row = row[1:]
	}
	return row
}

// auditGutterCols is the widest marker column the band writes, in cells.
func auditGutterCols(surface Surface) int {
	widest := 0
	for _, g := range CompareGutters(surface) {
		if w := lineedit.DisplayWidth(g); w > widest {
			widest = w
		}
	}
	return widest
}

// compareSequence is the WRAP instrument: it asks whether the grid carries a
// transcript line's WHOLE text continued across CONSECUTIVE rows (a wrap the
// terminal drew) rather than lost.
//
// It is deliberately conservative — a wrap is claimed only when the joined rows
// reconstruct the line's EXACT text (see auditWrapRow), in order, starting at a row
// whose text is a prefix of the line. Anything less is not claimed as arrival: a
// loose "some text is on some row" test would call a genuinely lost line painted,
// which is the false signal this audit exists to remove.
//
// IT IS THE ONE WRAP ORACLE, and both faces ask it: the whole-answer ledger
// (AuditAnswer) and the one-line containment core (CompareTranscriptLine) — the
// second one through classifyLostLine and wrapJoinPrefixRunes. Before it was asked
// there, a line the grid painted verbatim across a mid-word wrap came back from
// `gopi ui_state --compare` as a paint loss while the SAME dump's `--all` ledger
// counted it rejoined: one artifact, two contradicting faces (task 01a0e4f5).
//
// THE PADDING AMBIGUITY, and why it is tried rather than guessed: a grid row is
// padded with blanks to the full width, and the wrap point can land ON a real
// space — so "the head's trailing blanks" is either padding or text, and the audit
// cannot know which. Both readings are tried, and a join is claimed only if one of
// them reconstructs the line exactly; the pair never makes a claim weaker, because
// each reading must still reproduce every character.
//
// It returns the first row and every row the line was reassembled from, so the
// verdict can QUOTE the evidence instead of asserting a wrap.
func compareSequence(cand Candidate, line string) (bool, int, []int) {
	return compareSequenceInRows(cand.Dump.Lines, line, cand.Surface)
}

// compareSequenceInRows is the join CORE, over any row list — the visible grid
// (cand.Dump.Lines) or the saved history (cand.Dump.Scrollback). It is one
// function because the two lists are the SAME kind of thing: rows the terminal
// wrote, each already wrapped by the surface's own band. A second copy of this
// arithmetic for the scrollback is how the two would drift about what a wrap is,
// and the drifted answer would be the one a caller branches on.
//
// The rows are returned as INDICES IN THE LIST THAT WAS PASSED, so the caller
// names rows in the space it asked about (and the verdict can say so).
func compareSequenceInRows(rows []string, line string, surface Surface) (bool, int, []int) {
	want := []rune(line)
	if len(want) == 0 {
		return false, -1, nil
	}
	for start, row := range rows {
		head := auditWrapRow(row, surface)
		if head == "" {
			continue
		}
		for _, trimmedHead := range []string{head, strings.TrimRight(head, " ")} {
			if trimmedHead == "" || !strings.HasPrefix(string(want), trimmedHead) {
				continue
			}
			got := []rune(trimmedHead)
			span := []int{start}
			for next := start + 1; next < len(rows) && len(got) < len(want); next++ {
				cont := strings.TrimRight(auditWrapRow(rows[next], surface), " ")
				if cont == "" || !strings.HasPrefix(string(want[len(got):]), cont) {
					// The join does not continue THIS line: a longer claim would be
					// a guess about unrelated rows.
					break
				}
				got = append(got, []rune(cont)...)
				span = append(span, next)
			}
			if string(got) == string(want) {
				return true, start, span
			}
		}
	}
	return false, -1, nil
}

// joinedPrefixRunes is how many of the line's own runes the grid carries when the
// rows from `start` on are read the way the wrap join reads them (auditWrapRow) and
// joined in order — the longest run of consecutive grid rows whose text is a PREFIX
// of the line.
//
// WHY IT EXISTS. The prefix diagnostic answers "how much of the line is on the
// grid?" — a question in the LINE'S OWN RUNE SPACE. A single row's opening match
// answers it only when the line begins on a row that carries its first character
// first: on a WRAP, the row the line opens on holds the line's head and stops where
// the terminal wrapped, so that row's own text is a prefix of the line only by
// accident of the wrap point. Measured (task 01a0e4f5, 64-column grid): a 102-rune
// line the grid painted verbatim across a mid-word wrap — the join reassembles all
// 102 runes from rows 10..12 — was reported `prefix=36`, the first row's own text
// length, beside a loss class. The number sent the reader to a truncation that had
// not happened.
//
// It cannot over-claim: the walk stops at the first row whose text is not a prefix
// of what is still owed, so the number is always a prefix of the line and never
// exceeds it. It does not require the join to COMPLETE (that is compareSequence's
// job, and it is what decides arrival): it MEASURES, so a partial reassembly reports
// the partial number rather than 0 — which is the diagnostic a reader needs to tell
// "the band truncated it" from "it never arrived".
func joinedPrefixRunes(rows []string, start int, line string, surface Surface) int {
	want := []rune(line)
	if len(want) == 0 || start < 0 || start >= len(rows) {
		return 0
	}
	head := auditWrapRow(rows[start], surface)
	if head == "" {
		return 0
	}
	// The same two readings the join tries (a row's trailing blanks are either
	// padding or the text's own spaces), for the same reason: whichever the grid
	// meant, the reader is shown the number it produces.
	best := 0
	for _, trimmedHead := range []string{head, strings.TrimRight(head, " ")} {
		if trimmedHead == "" || !strings.HasPrefix(string(want), trimmedHead) {
			continue
		}
		got := []rune(trimmedHead)
		for next := start + 1; next < len(rows) && len(got) < len(want); next++ {
			cont := strings.TrimRight(auditWrapRow(rows[next], surface), " ")
			if cont == "" || !strings.HasPrefix(string(want[len(got):]), cont) {
				break
			}
			got = append(got, []rune(cont)...)
		}
		if len(got) > len(want) {
			got = got[:len(want)]
		}
		if len(got) > best {
			best = len(got)
		}
	}
	return best
}

// The damage classes are declared in audit.go (the vocabulary a reader greps for)
// and DECIDED in damage.go, by the ONE classifier both audit faces call. The width
// classes (paint_loss / wrap_artifact_or_loss / cjk_wrap) remain what they always
// were — the evidence one frame offers for a line no row carries — and they are
// reached only after the grid itself has been asked whether it reassembled the line.
