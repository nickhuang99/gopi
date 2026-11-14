package uiclient

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// compare.go — the one-call check "is this line ON THE PAINTED GRID, and is it
// in the AUTHORITATIVE transcript?".
//
// THE PROBLEM IT CLOSES. gopi's self-observation has two ends: the dump (what
// the surface painted, as a cell grid) and the session log (what the model
// actually said, byte-exact). Reading one of them tells an agent nothing about
// whether the other agrees — so "the answer looks truncated on screen" cannot be
// distinguished from "the dump was re-interpreted at the wrong width", and a
// wrap cannot be distinguished from a loss. The check that separates them is a
// two-sided containment test on ONE line: present in the transcript (the
// authority) and present in the grid (the paint) means it arrived intact; present
// in one and not the other localizes the loss to a LAYER, which is the diagnosis
// an agent needs.
//
// WHY IT IS PURE. The grid comes from a Candidate (a parsed artifact) and the
// transcript comes in as LINES — no file access, no process probing, no clock.
// The caller (cmd/agent's `gopi ui_state --compare`) owns discovery and reading,
// so this function can be unit-tested on hand-built inputs and cannot become a
// second implementation of the reader.

// CompareStatus is the verdict of one transcript-vs-painted check. It is a
// closed vocabulary (never a bool) because "both sides have it", "only the log
// has it" and "I could not answer" are three different facts, and collapsing
// them is how a wrong answer looks like a confident one.
type CompareStatus string

const (
	// CompareMatch: the line is byte-exact on the grid AND in the transcript —
	// the paint is faithful for this line, and the observation channel is intact.
	CompareMatch CompareStatus = "match"
	// CompareMismatch: the line is on exactly ONE of the two sides. Which side
	// says where the loss is: transcript-only = the paint lost (or wrapped) it;
	// grid-only = the grid carries text the authoritative transcript does not
	// (a stale frame, or the wrong session's surface).
	CompareMismatch CompareStatus = "mismatch"
	// CompareRejoined: the WHOLE line is on the grid, reassembled from
	// CONSECUTIVE rows — the grid WRAPPED it, so no single row is the line and
	// the row-exact match could not fire. It is an ARRIVAL, and it is its own
	// status rather than a flavour of CompareMismatch because the ledger derives
	// its counts from these statuses: a status that reads "mismatch" beside a
	// counting rule that says "arrived" is how the reported ledger came to list
	// one line as a mismatch AND as a rejoin at the same time, with its own
	// sentence saying the text had arrived (task 01a0e4f5, measured on a real
	// chat dump: `disagreed: 1` with two mismatch entries, `rejoined: 1` naming
	// no row, and line 1 both `verdict.status="mismatch"` and "the text
	// arrived"). ONE line, ONE status, and the counters follow from the statuses.
	//
	// MERGE NOTE 01a0e54c — DELIBERATE DROP of the pre-merge branch's spelling of
	// this arrival. The branch named it CompareRejoined here and had
	// CompareTranscriptLine set it; master names the SAME fact CompareMatch plus the
	// boolean Rejoined, and sets it in the one place the join is asked
	// (CompareTranscriptLine, case v.InTranscript). The constant is KEPT — it is the
	// name the branch's own ledger-agreement test inspects the JSON for
	// (`verdict.status=="rejoined"`, cmd/agent/cli_uistate_ledger_agreement_test.go)
	// and the branch's report is written in it — but nothing assigns it in this
	// merge, because a wrap that is a CompareMatch carrying Rejoined and a wrap that
	// is a CompareRejoined describe ONE arrival and must not become two statuses the
	// counters could read differently. Any counter rule that needs this arrival asks
	// CompareVerdict.Rejoined (see AuditLedger.auditCountsFromEntries).
	CompareRejoined CompareStatus = "rejoined"
	// CompareNoTranscript: the grid was examined but there was no transcript to
	// compare against. It is NOT a pass: reporting it as one would let a caller
	// claim a comparison that never happened.
	CompareNoTranscript CompareStatus = "no-transcript"
	// CompareRefused: the grid may not be read as the screen's layout at all —
	// it was never painted, or its geometry is a fallback (see
	// GeometryFallbackNotice). A containment answer at an unknown geometry would
	// report a wrap the layout never drew as a missing line.
	CompareRefused CompareStatus = "refused"
	// CompareOffGrid: the line is NOT on the visible grid, and the grid's own
	// SAVED HISTORY (ScreenDump.Scrollback) carries it — so the surface painted
	// it and the row has since been pushed off the top. It is a THIRD answer,
	// beside "agreed" and "disagreed", and it exists because those two were the
	// only ones a caller could get: an answer that scrolled out of a 22-row
	// viewport reported `mismatch`, which reads as "the paint lost it" and sent
	// the reader looking for a truncation that never happened (task 01a1c05c).
	//
	// IT IS NOT A PASS. The visible window does not contain the line, so nothing
	// about the CURRENT frame was verified: reporting it as CompareMatch would
	// claim a comparison the evidence does not support. Nor is it a disagreement:
	// the two sides do not contradict each other, the observation window is just
	// smaller than the question. A caller that branches on the exit code gets its
	// own code (4) instead of the disagreement code (3).
	CompareOffGrid CompareStatus = "off-grid"
	// CompareNoAuthority: the grid IS readable and was examined, but no
	// authoritative text record exists for this artifact — the surface that wrote
	// the dump keeps no transcript this reader can derive beside it, and the
	// caller named none. It is a SEPARATE status from CompareNoTranscript, and the
	// distinction is the point: `no-transcript` means the authority was READ and
	// held no lines, `no-authoritative-side` means there was no authority to read
	// AT ALL for this surface — a reader who sees the two spelled the same way
	// cannot tell "the log is empty" from "this surface has no log", and the
	// measured complaint (task 01a0e62d) is exactly that both printed as an
	// undifferentiated `transcript_lines=0`.
	CompareNoAuthority CompareStatus = "no-authoritative-side"
)

// The REASON TOKENS: the first word of every CompareVerdict.Reason, so a caller
// can branch on WHICH SIDE the answer is about without parsing an English
// sentence. They are one closed vocabulary in one place (Rule 1b/13), and each
// names the side the finding is attributed to:
//
//	transcript-side  the authoritative log has the line and the grid does not —
//	                 the PAINT lost it (or drew it somewhere this reader cannot
//	                 see, which is why off-grid is asked first)
//	grid-side        the grid carries the line and the transcript does not — the
//	                 frame is not this session's text (a stale frame, or another
//	                 surface's screen)
//	neither-side     neither side has it — it is not text this session produced,
//	                 so no layer can be blamed
//	off-grid         the line is outside the observed window (in the scrollback,
//	                 not in the visible grid) — UNDECIDABLE, not a disagreement
//	no-transcript / no-authoritative-side
//	                 the authority was empty / does not exist for this surface —
//	                 also undecidable, spelled as its own word
//
// The token is a PREFIX of the sentence rather than a replacement for it: the
// prose still carries the measurements a reader re-does the arithmetic with
// (prefix runes, widths, wrap rows), and the first word is what a shell or an
// agent greps. Before this the only machine-readable discriminator was the
// exit code, which collapsed "lost" and "cannot tell" onto 3.
const (
	CompareReasonSideTranscript = "transcript-side"
	CompareReasonSideGrid       = "grid-side"
	CompareReasonSideNeither    = "neither-side"
	CompareReasonSideOffGrid    = "off-grid"
)

// reasonWithSide prefixes a sentence with its side token. It is the ONE place
// the two are joined, so a reason can never be rendered with the token of one
// side and the prose of another.
func reasonWithSide(side, sentence string) string {
	if sentence == "" {
		return side
	}
	return side + ": " + sentence
}

// NoAuthoritativeSideNotice is the sentence a check carries when the dump's
// surface keeps no derivable authoritative record and the caller named none. It
// is built FROM the status word, so the sentence a reader greps for and the word
// the verdict carries can never name different facts (Rule 13's shape).
const NoAuthoritativeSideNotice = string(CompareNoAuthority) + ": no authoritative text record exists for this artifact — the surface that wrote this dump keeps no transcript beside it, and the caller named none with --session. The grid was read and the OTHER side does not exist here, so nothing was compared. This is NOT a paint loss and NOT an agreement"

// CompareVerdict is the whole answer of one check: the verdict, the evidence
// that decided it, and the reason a reader needs to act on it. Every field is
// one an agent would otherwise have to reconstruct by hand.
type CompareVerdict struct {
	// Status is the verdict.
	Status CompareStatus `json:"status"`
	// Line is the searched line, verbatim.
	Line string `json:"line"`
	// InGrid reports an exact, cell-for-cell hit on a grid row.
	InGrid bool `json:"in_grid"`
	// GridRow is the 0-based row the line was found on (or -1 when absent).
	GridRow int `json:"grid_row"`
	// (MERGE NOTE 01a0e54c — DELIBERATE DROP of the pre-merge branch's
	// `RejoinRows []int json:"rejoin_rows"` field. It was the branch's name for the
	// rows a wrap was reassembled from, and in this merge NOTHING would have set it:
	// master records the same fact in `WrapRows` below, filled by the one place the
	// join is asked, so keeping a second field would have meant either a dead key in
	// the JSON or two spellings of one fact. WrapRows is kept because it is the one
	// the live wrap path fills and the one the rendered sentence NAMES.)
	// RowTailRunes is how many runes followed the line on the row that carried it
	// and had to be explained as the line's own head re-drawn (0 for a row that is
	// exactly the line, -1 when the line is not on a row). It is reported because
	// the evidence for a "carried" verdict IS that tail: a reader can re-do the
	// arithmetic from the numbers rather than trust the sentence.
	RowTailRunes int `json:"row_tail_runes"`
	// GridPrefixRunes is the longest prefix of the LINE that the grid actually
	// carries, counted in the LINE'S OWN rune space: every grid row the scan read,
	// taken as the wrap join reads it (auditWrapRow), matched against the line —
	// one row's own opening, or the longest consecutive run of rows whose joined
	// text is a prefix of the line.
	//
	// It is the diagnostic that separates "the band truncated it" (a large prefix)
	// from "it never arrived" (0). IT IS NOT A ROW LENGTH: the number used to be
	// the first grid row's own text length, which is a length in the ROW's space
	// and is a prefix of the line only by coincidence — the row carries a marker,
	// and the ROW the line OPENS is not always the row carrying its first
	// character. Measured (task 01a0e4f5, this host, a 64-column grid): a 102-rune
	// line the grid painted verbatim across a mid-word wrap was reported
	// `prefix=36`, i.e. the 36 runes of the first row, while the grid carried all
	// 102.
	GridPrefixRunes int `json:"grid_prefix_runes"`
	// WrapRows is every grid row the line's whole text was reassembled from (see
	// compareSequence, the wrap instrument), in order — empty unless Rejoined.
	// GridRow is its first entry, and the field exists because "the wrap is not a
	// loss" is a fact a reader has to be able to SEE: the rows are the evidence,
	// and a reader who is given the row numbers can re-do the join.
	WrapRows []int `json:"wrap_rows,omitempty"`
	// Rejoined reports that the line was NOT on one grid row because the grid
	// WRAPPED it: the join instrument reassembled its whole text from consecutive
	// rows, so the text arrived and Damage is empty. It is the one-line compare's
	// twin of AuditLine.Rejoined — the two faces name the same fact with the same
	// word, which is what stops one of them reporting a loss on a line the other
	// counts as arrived.
	Rejoined bool `json:"rejoined"`
	// OffGrid reports that the line was NOT on the visible grid and WAS
	// reassembled from the grid's SAVED HISTORY (ScreenDump.Scrollback): the
	// surface painted it and it has since been pushed off the top of the window
	// this artifact observes. It is deliberately separate from Rejoined —
	// "wrapped, with both rows on the screen" is a VERIFIED arrival, while "off
	// the observed window" is an observation that cannot decide the question.
	OffGrid bool `json:"off_grid"`
	// ScrollbackRow is the first row of the saved history the line was
	// reassembled from (-1 when the line is not in the history). It is an index
	// in Scrollback's OWN space, never a grid row, and it is reported so a reader
	// can go and re-do the join rather than trust the sentence.
	ScrollbackRow int `json:"scrollback_row"`
	// DisplayHits is how many times the line appears on grid rows that it OPENS,
	// counted as DISPLAYS: a row that opens with the line twice (a re-paint whose
	// two copies are both on screen) counts two. It is the number behind the
	// DamageToRow class: "the line is on the grid but not exactly once" is a
	// measurement, and this is it. 0 = the line is not at the start of any row.
	DisplayHits int `json:"display_hits"`
	// Damage is the classification of a line the transcript holds and no single row
	// carries, decided by the ONE shared classifier (see classifyLostLine) so the
	// one-line compare and the whole-answer ledger cannot name the same row two
	// different ways. Empty for a match, a refusal, or a line that arrived wrapped.
	Damage AuditDamage `json:"damage,omitempty"`
	// InTranscript reports an exact hit in the authoritative transcript.
	InTranscript bool `json:"in_transcript"`
	// TranscriptHits is how many transcript lines matched (0 when none).
	TranscriptHits int `json:"transcript_hits"`
	// Geometry is the authority behind the grid's size (never empty: a
	// "refused" verdict names the fallback it refused).
	Geometry GeometrySource `json:"geometry"`
	// Cols / Rows are the grid's geometry, so a reader can see the width the
	// containment answer was computed at.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// Frames is the sink's frame count — 0 means nothing was ever painted.
	Frames int `json:"frames"`
	// Reason explains a refusal or a mismatch in one sentence ("" for a match).
	Reason string `json:"reason"`
}

// CompareGridUnusable reports WHY a candidate's grid cannot be read as the
// screen's layout at all, or "" when it can. It is the ONE predicate for that
// refusal — the containment core and any caller that wants to check the grid
// before fetching a transcript both ask it, so the two cannot disagree about
// which grids are readable.
func CompareGridUnusable(cand Candidate) string {
	if cand.Unpainted() {
		// frames=0: the grid is an all-blank snapshot, not a screen (see
		// UnpaintedNotice). Answering "not in the grid" here would report the
		// observer's own counter as a property of the surface.
		return UnpaintedNotice
	}
	if cand.BlankUnpainted() {
		// frames>0 and every cell blank: the grid has no content to be checked
		// against (see BlankUnpaintedNotice — measured `frames=3 dumps=1` on an
		// idle chat). Answering "not in the grid" here reads a screen that was
		// never painted, or a painted blank one, as a screen that lost the text.
		return BlankUnpaintedNotice
	}
	if GeometryOf(cand) == GeometrySourceFallback {
		// The grid was built at a geometry nothing measured: a containment
		// answer would read the default width's wraps as the screen's (see
		// GeometryFallbackNotice).
		return GeometryFallbackNotice
	}
	return ""
}

// CompareGutters are the marker columns a surface's own layout writes to the
// LEFT of a line's text. A grid row is the line the operator saw, so it carries
// that column; the transcript is the model's TEXT, which never does. A
// comparison that required the two to be equal byte-for-byte as whole ROWS would
// therefore report every single answer line as a mismatch — the diagnosis
// inverted, and the flag useless for the case it exists for.
//
// THE GRAMMAR IS PER SURFACE, and that is the fix for a MEASURED defect
// (2026-09-28, real 110x40 pty, task 01a0e62d): the two surfaces label their
// rows differently, and the chat surface's marker set was applied to the ui
// surface's rows. On a `gopi ui` screen whose grid carried
//
//	003|gopi> UI-AUTH-OK-8842 qwerty
//
// the row's text after the chat markers' strip is still `gopi> …`, so no row
// carried the transcript line `UI-AUTH-OK-8842 qwerty` — and the verb reported
// `outcome=disagreed damage=paint_loss prefix=0` on a screen that had painted
// the line FAITHFULLY. The transcript side held it (in_transcript=true), the
// grid had it (visible on the row), and the reader was told the paint lost it.
//
// The marker lists are read from the code that WRITES them (never re-typed): the
// chat surface's from internal/lineedit's answer band, the ui surface's from the
// entry prefixes internal/uiclient/model.go's wrapEntry draws.
func CompareGutters(surface Surface) []string {
	// The chat surface: the answer band's turn bullet, its wrap/continuation
	// marker and the reasoning gutter.
	band := lineedit.AnswerGutters()
	if surface != SurfaceUI {
		return band
	}
	// The ui surface draws its own row labels (model.go's wrapEntry selects one
	// per entry kind) and NO band marker, so the two sets are peers rather than
	// one containing the other. The union is returned rather than a replacement:
	// a dump read at the wrong surface marker must never be answered with a
	// false paint loss, and a strip that removes a prefix the row does not have
	// is a no-op.
	return append(band, uiRowPrefixes()...)
}

// uiRowPrefixes are the row labels `gopi ui` draws to the left of a line's text
// (model.go's wrapEntry). They are listed in one place because the reader needs
// the whole grammar and the renderer's own `switch` is where the values live —
// the two are pinned against each other by TestUIRowPrefixesMatchTheRenderer.
func uiRowPrefixes() []string {
	return []string{userPrefix, assistantPrefix, toolPrefix, errorPrefix, noticePrefix, reasoningPrefix}
}

// uiSharedRowPrefixes are the row labels `gopi ui` draws with a marker the CHAT
// surface also uses — i.e. the entries of uiRowPrefixes that are NOT ui-only.
//
// WHY THIS LIST EXISTS, and why it is not a loophole. The grammar pin
// (TestCompareGuttersIsPerSurface) asserts that the chat grammar does not carry
// the ui labels, because a chat row whose own text began with `gopi> ` would
// otherwise lose it. The reasoning gutter is the deliberate exception: it is ONE
// glyph on both carriers (reasoningPrefix IS lineedit.ThinkingGutter — Rule 14),
// so "the chat grammar carries it" is not drift, it is the sharing.
//
// The list is declared rather than inferred so the exception is a STATEMENT a
// reader can audit: TestCompareGuttersIsPerSurface requires each entry to be a
// label the chat grammar genuinely carries (a stale declaration is red), and the
// ui-only labels stay under the original ban.
func uiSharedRowPrefixes() []string {
	return []string{reasoningPrefix}
}

// rowCarriesLine decides whether one grid row CARRIES the transcript line, and
// how much of the row that is not the line had to be explained. carry=true means
// the line arrived on this row.
//
// THE MEASURED CASE it exists for (task 01a0e305, 2026-09-27, real 117x26 pty):
// the band re-drew the answer line while the end of the PREVIOUS paint of that
// same line was still on the row, so the row carried the line followed by the
// line's own head again —
//
//	row  = "▌ 自观测闭环：…两面对照。" + "自观测闭环：…自己会把两"
//	line =           "自观测闭环：…两面对照。"
//
// and the byte-exact row match therefore never fired. The whole line was painted,
// the audit said damage=paint_loss, and the stderr of `gopi ui_state` told the
// operator "the paint lost it" — the opposite of the truth. The row's tail is the
// line's OWN head, so the rule is: the row opens with the line, and what follows
// is a PREFIX of the line again. That is a re-paint, not a second line.
//
// WHY NOT "the row merely STARTS with the line". A rule that accepted any tail
// would call a row carrying two different lines "arrival" for the first one, and a
// lost second line would stop being reported at all — the blanket excuse this
// function must not be. Here the tail has to be the line's own text, re-drawn from
// its own start; a row whose tail is ANY OTHER text is not the line's row, and the
// containment answer falls through to the mismatch path exactly as before.
func rowCarriesLine(row, line string) (bool, int) {
	if !strings.HasPrefix(row, line) {
		return false, -1
	}
	tail := []rune(row[len(line):])
	if len(tail) == 0 {
		// The ordinary frame: the row IS the line. A re-paint's zero-length tail is
		// the exact hit, so the two are one rule with one arithmetic.
		return true, 0
	}
	head := []rune(line)
	if len(tail) > len(head) || string(head[:len(tail)]) != string(tail) {
		// The row carries the line and then something that is NOT the line's own
		// beginning: a second, different line drew on this row (a wrap of the next
		// transcript line), so this row does not vouch for THIS line's arrival.
		return false, -1
	}
	return true, len(tail)
}

// WholeLineAtRowStart reports the measurement that must never be presented as a
// LOSS. It is true when the line is verbatim at the start of a grid row that
// continues with text the line does not have — i.e. the whole line arrived, and the
// row it arrived on carries something else as well (a re-paint that was not cleared,
// or a second line drawn over the tail).
//
// WHY IT IS A PREDICATE ON THE VERDICT rather than only a damage class: the class
// names the damage, and this says what the damage is NOT — the line's text was not
// lost. Keeping it here means the containment answer and the prefix can no longer
// contradict each other in the reader's hands, which is the defect this whole file's
// family exists to remove.
func (v CompareVerdict) WholeLineAtRowStart() bool {
	return v.Status == CompareMismatch && !v.InGrid && v.InTranscript &&
		v.GridRow >= 0 && v.DisplayHits > 0
}

// (MERGE NOTE 01a0e54c — DELIBERATE DROP of the pre-merge branch's `rejoinReason`.
// Master already renders this sentence, and renders it ONCE for both faces:
// `wrapArrivalReason` (damage.go), called by CompareTranscriptLine and quoted by the
// ledger entry, so the two audit faces cannot hand-write two versions of "the text
// arrived" (Rule 13's shape). The branch's copy was byte-identical in intent but
// became dead in this merge — the unwrapped call site below is master's. Two
// renderers of one sentence is exactly the drift this family of changes removes.)

// CompareTranscriptLine decides whether line arrived intact, by containment on
// both sides. transcript is the authoritative text, one entry per line, in
// order; an EMPTY transcript is reported as CompareNoTranscript rather than as
// a one-sided mismatch — "I could not check" must never be dressed up as "the
// paint lost it".
//
// The grid match is EXACT once the row's marker column is removed: the claim the
// observation channel makes is that the model's text is on screen verbatim, and
// the marker is the BAND's decoration rather than the text. A prefix hit is
// reported separately as the diagnostic for a truncated or wrapped line, and
// never counts as a match.
func CompareTranscriptLine(line string, cand Candidate, transcript []string) CompareVerdict {
	v := CompareVerdict{
		Line:          line,
		GridRow:       -1,
		RowTailRunes:  -1,
		ScrollbackRow: -1,
		Cols:          cand.Dump.Cols,
		Rows:          cand.Dump.Rows,
		Frames:        cand.Frames,
		Geometry:      GeometryOf(cand),
	}
	if refusal := CompareGridUnusable(cand); refusal != "" {
		v.Status = CompareRefused
		v.Reason = refusal
		return v
	}
	if line == "" {
		v.Status = CompareRefused
		v.Reason = "an empty line cannot be compared: every row contains it, so the answer would be trivially true"
		return v
	}
	// The row that carries the line is the row whose text OPENS with it and whose
	// remainder is the line's own head re-drawn — see rowCarriesLine, which owns
	// that arithmetic including the byte-exact case (a zero tail), so the exact
	// hit is the rule's simplest case rather than a special case beside it.
	for i, row := range cand.Dump.Lines {
		text := stripCompareGutters(row, cand.Surface)
		// THE WHOLE LINE AT A ROW'S START, with other text after it: the containment
		// core does not call this arrival (the row is not the line, and the rule that
		// accepts a tail accepts only the line's OWN head re-drawn — see
		// rowCarriesLine), but it is the measurement that stops a reader being told
		// the paint lost text that is on the screen. The count is how many copies the
		// row DISPLAYS (see displayedWholeLine) and the tail is what follows the FIRST
		// copy; the damage class is derived from both. The scan continues rather than
		// breaking: an exact row later on the grid is still the better answer.
		if n := displayedWholeLine(text, line); n > 0 {
			if v.GridRow < 0 {
				v.GridRow = i
			}
			v.DisplayHits += n
		}
		if carry, tail := rowCarriesLine(text, line); carry {
			v.InGrid = true
			v.GridRow = i
			v.RowTailRunes = tail
			break
		}
		// THE PREFIX IS THE DIAGNOSTIC THAT ANSWERS "HOW MUCH OF THE LINE IS ON THE
		// GRID?" — a question in the LINE'S OWN RUNE SPACE, and the one number that
		// separates "the band truncated it" from "it never arrived". It is the BEST of
		// two readings of the same grid: how much a single row shares with the line
		// from its start, and — for a line the grid WRAPPED — how much a run of
		// consecutive rows carries when they are read the way the wrap join reads them
		// (auditWrapRow, the ONE rendering both faces use). The second reading is what
		// the number was missing: on a mid-word wrap the row the line OPENS on carries
		// the line's head and stops where the terminal wrapped, so a number in the
		// ROW's space reports a 102-rune line as a 36-rune prefix (task 01a0e4f5,
		// measured on this host). Neither reading can exceed the line — each is a
		// prefix of it — so the diagnostic cannot over-claim.
		if n := joinedPrefixRunes(cand.Dump.Lines, i, line, cand.Surface); n > v.GridPrefixRunes {
			v.GridPrefixRunes = n
		}
		if n := sharedRunePrefix(text, line); n > v.GridPrefixRunes {
			v.GridPrefixRunes = n
		}
	}
	for _, t := range transcript {
		if t == line {
			v.InTranscript = true
			v.TranscriptHits++
		}
	}
	// OFF THE VISIBLE GRID, BUT NOT NECESSARILY LOST: the saved history is the
	// same kind of row list the visible grid is (the terminal pushed those rows
	// off the top), so the SAME join instrument is asked of it — BEFORE the
	// verdict is decided, because a line the history holds lands in a DIFFERENT
	// branch than one neither side has, and deciding first would be deciding
	// wrong. A hit here means "this line was painted and has since scrolled out
	// of the window being observed": neither agreement (the current frame does
	// not contain it, so nothing about the frame was verified) nor disagreement
	// (the two sides do not contradict each other — the window is smaller than
	// the question).
	//
	// WHY IT IS ASKED AT ALL, measured (task 01a1c05c): `ui_state`'s grid is the
	// VIEWPORT (22 rows on a real chat) and a 2,518-row scrollback sits beside it
	// in the SAME artifact. A correct answer that had scrolled out therefore
	// reported `mismatch` — the code that means "the paint lost it" — and sent
	// the reader looking for a truncation that never happened. Under Rule 15
	// that is the failure a definite-sounding answer makes when the observation
	// cannot decide: it is asked, and it says so in its own word and its own
	// exit code.
	var offGridRow int = -1
	var offGridWrap []int
	if !v.InGrid {
		if ok, row, rows := compareSequenceInRows(cand.Dump.Scrollback, line, cand.Surface); ok {
			offGridRow, offGridWrap = row, rows
		}
	}
	switch {
	case len(transcript) == 0:
		v.Status = CompareNoTranscript
		v.Reason = "no transcript lines were available: the grid was examined, the authoritative side was not, so no comparison can be claimed"
	case v.InGrid && v.InTranscript:
		v.Status = CompareMatch
	case v.InGrid:
		v.Status = CompareMismatch
		v.Reason = reasonWithSide(CompareReasonSideGrid,
			"the grid carries this line but the transcript does not: the painted frame is not this session's text (a stale frame, or another surface's screen)")
	case v.InTranscript:
		// THE WRAP IS ASKED OF THE GRID, NOT INFERRED FROM THE WIDTH — the same
		// instrument, and the same verdict, the whole-answer audit reaches: no single
		// row carries the line, and consecutive rows reassemble its WHOLE text
		// (compareSequence), so the grid drew it on more than one row and the text
		// arrived. The reported defect (task 01a0e4f5) is exactly the shape this
		// branch exists for: a mid-word wrap whose first row carries only the line's
		// head, which the row-exact rule cannot see and which used to fall through to
		// the loss classes with `prefix` in the first ROW's space.
		//
		// THE JOIN MUST REPRODUCE THE TEXT OR THIS BRANCH DOES NOT FIRE: a join that
		// cannot reconstruct the line exactly leaves the verdict a mismatch, so a line
		// the grid did NOT reassemble is still reported — the anti-widening control
		// this family of changes must never lose.
		if ok, row, rows := compareSequence(cand, line); ok {
			v.Status = CompareMatch
			v.InGrid = true
			v.GridRow = row
			v.WrapRows = rows
			v.Rejoined = true
			v.GridPrefixRunes = len([]rune(line))
			v.Reason = wrapArrivalReason(rows)
			break
		}
		// THE SAVED HISTORY HAS IT, THE VISIBLE WINDOW DOES NOT: the third answer.
		// Asked AFTER the visible grid's own join so a line on the CURRENT frame
		// can never be reported as merely off-grid — a verified arrival outranks an
		// undecidable one.
		if offGridRow >= 0 {
			v.Status = CompareOffGrid
			v.OffGrid = true
			v.ScrollbackRow = offGridRow
			v.WrapRows = offGridWrap
			v.Damage = DamageNone
			v.GridPrefixRunes = len([]rune(line))
			v.Reason = offGridReason(offGridRow, len(cand.Dump.Scrollback), len(cand.Dump.Lines))
			break
		}
		v.Status = CompareMismatch
		// ONE classifier for the two audit faces (see classifyLostLine): the class
		// and the reason sentence are derived from the SAME measurement, so they can
		// never disagree — which is exactly what the reported verdict did, saying
		// "the paint lost it" beside a prefix covering the whole line.
		v.Damage = classifyLostLine(cand, line, v)
		// (MERGE NOTE 01a0e54c — DELIBERATE DROP of the pre-merge branch's SECOND,
		// width-gated wrap probe that stood here: `if wrapWouldFit(cand, line) { if
		// ok, row, rows := compareSequence(...); ok { v.Status = CompareRejoined;
		// v.Damage = DamageTextWrapped; ... } }`. It is dead on arrival after the
		// union, and leaving it would have been worse than dead: the UNCONDITIONAL
		// `compareSequence` call above already asks the join for every line, so this
		// block could only be reached when the join has JUST answered "no" — the
		// `wrapWouldFit` width gate in front of a query whose answer is already known
		// false. Dropped, so master's one-oracle rule holds: the join is asked once,
		// its answer decides the arrival, and a line the grid did NOT reassemble is a
		// mismatch whose class carries the loss — the anti-widening control.)
		v.Reason = lostReason(cand, line, v, v.Damage)
	default:
		// NEITHER side has it. This is a different answer from "one side lost
		// it", and reporting the transcript-only diagnosis here would send a
		// reader looking for a paint defect when the line simply is not the text
		// under discussion (a typo, or the wrong session).
		v.Status = CompareMismatch
		if offGridRow >= 0 {
			// The grid's SAVED HISTORY does carry it while the transcript does
			// not, which is the grid-side finding at a larger unit: the artifact
			// painted this text and the authority has no record of producing it
			// (a stale frame from another session, or a hand-typed line). Naming
			// the side is the whole point of the token — "neither side" would be
			// factually wrong about a line the history holds.
			v.OffGrid = true
			v.ScrollbackRow = offGridRow
			v.WrapRows = offGridWrap
			v.Reason = reasonWithSide(CompareReasonSideGrid, fmt.Sprintf(
				"the grid's SAVED HISTORY carries this line (history row %d) and the transcript does not: the painted frame is not this session's text (a stale frame, or a line drawn on the surface without the log recording it)",
				offGridRow))
			break
		}
		v.Reason = reasonWithSide(CompareReasonSideNeither, "neither the transcript nor the grid (visible window or saved history) has this line verbatim: it is not text this session produced, so no layer can be blamed (check the session, and that the line is exact)")
	}
	return v
}

// displayedWholeLine counts how many times the WHOLE line appears at the start of
// text, each occurrence occupying the following len(line) runes — i.e. how many
// copies of the line the row DISPLAYS, not how many times its bytes appear inside
// it. Non-adjacent repetition is not a display: `line + other + line` is other text
// that happens to contain the line twice, and counting it would turn an unrelated
// row into this line's row.
//
// It returns 0 when text does not open with the line — the caller's own prefix
// diagnostic covers "the text is a partial head of the line", which is the real-loss
// shape and must not be counted here.
func displayedWholeLine(text, line string) int {
	if line == "" || !strings.HasPrefix(text, line) {
		return 0
	}
	runes, want := []rune(text), []rune(line)
	n := 0
	for n*len(want)+len(want) <= len(runes) {
		got := runes[n*len(want) : n*len(want)+len(want)]
		if string(got) != line {
			break
		}
		n++
	}
	return n
}

// lostReason renders the sentence a reader acts on for a line the transcript holds
// and no single row carries. It is derived from the CLASS rather than written beside
// it (Rule 13's shape): the reported defect was a verdict whose class and whose
// sentence both said "lost" about a line whose own measurement said it was there, and
// a hand-written pair of strings is exactly how those two drift apart. `prefix` and
// the display count are quoted as the evidence a reader can re-do the arithmetic on.
func lostReason(cand Candidate, line string, v CompareVerdict, d AuditDamage) string {
	switch d {
	case DamageToRow:
		return reasonWithSide(CompareReasonSideGrid, fmt.Sprintf("the row opens with the WHOLE line and then continues with text the line does not have (%d runes past it, displayed %d time(s)): the line's text is on the grid — what is damaged is the ROW, not the line (a re-paint that did not clear the row, or a second line drawn over its tail)",
			tailPastFirstCopy(cand, line, v.GridRow), v.DisplayHits))
	case DamageCJKWrap:
		return reasonWithSide(CompareReasonSideTranscript, fmt.Sprintf("the transcript has this line and no row carries it (longest verbatim prefix on the grid: %d runes): the line is wider in CELLS (%d) than in RUNES (%d) and wider than the grid (%d), so the layout's double-width arithmetic is the candidate explanation",
			v.GridPrefixRunes, lineedit.DisplayWidth(line), len([]rune(line)), cand.Dump.Cols))
	case DamageWrapArtifactOrLoss:
		return reasonWithSide(CompareReasonSideTranscript, fmt.Sprintf("the transcript has this line and no row carries it (longest verbatim prefix on the grid: %d runes): the line is wider than the grid (%d cells at %d), so a correct wrap explains its absence as well as a loss does — and the grid's own wrap join did not reassemble it, and the saved history does not hold it either",
			v.GridPrefixRunes, lineedit.DisplayWidth(line), cand.Dump.Cols))
	default:
		return reasonWithSide(CompareReasonSideTranscript, fmt.Sprintf("the transcript has this line and no row carries it, and it FITS the grid (%d cells at %d, longest verbatim prefix on the grid: %d runes): no wrap can explain that, and the grid's saved history does not hold it either — the paint lost it",
			lineedit.DisplayWidth(line), cand.Dump.Cols, v.GridPrefixRunes))
	}
}

// GeometryOf reports the geometry authority recorded in a candidate's own
// record. A record that does not carry the field (an artifact written before
// the field existed) is reported as the fallback — the conservative answer: an
// artifact that cannot state its authority must not be read as if a layout had
// measured it.
func GeometryOf(cand Candidate) GeometrySource {
	raw := cand.Record.Fields[dumpRecordGeometryField]
	if raw == "" {
		return GeometrySourceFallback
	}
	return GeometrySource(raw)
}

// gridRowText was the cell-exact comparison surface; the comparison uses the
// dump's LINES instead (ScreenDump.Lines), which is the same row as a human
// reads it: trailing blanks trimmed and a wide rune's continuation column
// skipped. That is deliberate — a continuation column is a COLUMN OF a rune, not
// a character, so a byte-exact comparison of the text must not carry it, and
// ScreenDump.Lines is the grid owner's own definition of "this row's text"
// rather than a second reconstruction here.
//
// stripCompareGutters removes the leading marker COLUMNS (see CompareGutters)
// from a grid row, and nothing else: the band writes its marker as a prefix — and
// a line that both opens a turn and continues it carries more than one (measured
// 2026-09-26: the row is `▌ ▌ 中文与…`, the turn marker plus the wrap marker), so
// the strip repeats until no marker matches, bounded by the number of markers the
// band defines.
//
// Only a PREFIX is removed: a row whose text merely contains a bullet further in
// is left alone, and an answer line that itself begins with the marker glyph is
// compared as the text it is.
func stripCompareGutters(row string, surface Surface) string {
	gutters := CompareGutters(surface)
	for n := 0; n < len(gutters); n++ {
		stripped := ""
		for _, g := range gutters {
			if strings.HasPrefix(row, g) {
				stripped = strings.TrimPrefix(row, g)
				break
			}
		}
		if stripped == "" {
			break
		}
		row = stripped
	}
	return row
}

// sharedRunePrefix reports how many leading RUNES (not bytes) two strings share.
// It is counted in runes because the lines under comparison are CJK+latin: a
// byte count over a multi-byte line would report a "86% match" for a line that
// diverged after the third character.
func sharedRunePrefix(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n := 0
	for n < len(ar) && n < len(br) && ar[n] == br[n] {
		n++
	}
	return n
}
