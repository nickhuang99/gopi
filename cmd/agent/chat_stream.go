package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_stream.go — the A5 scrollback-safe streaming path plus the one-line
// status.
//
// Two renderers exist, and which one runs is decided by the terminal, not by a
// flag:
//
//   - **The reserved-layout renderer** (lineedit.AnswerRenderer) runs when the
//     editor reserved the lower screen rows. An answer line is written to the
//     live row and then committed ONCE into the scrolling answer band; the
//     deltas never repaint what is already committed, and the status line gets
//     a row of its own.
//   - **The buffered renderer** (the pre-A5 chatRenderer) runs everywhere else —
//     piped stdin, --no-editor, a terminal too small for the layout. It keeps
//     the historic "pop-in in whole words/lines" behaviour byte-for-byte.
//
// The status line is assembled from counters the REPL already keeps (the model
// name, the turn counter, the running token total) — it adds no state, only a
// place to show what exists.

// Chat renderer bounds and cadence. Every window/bound is named (Rule 1).
const (
	// chatWordFlushWindow is how long a completed word (trailing space) may
	// sit buffered before it is flushed — the "pop-in" cadence. Long enough
	// for short words to stream in a readable right-to-left rhythm, short
	// enough that the answer never lags the model.
	chatWordFlushWindow = 30 * time.Millisecond
	// chatMaxBufferedBytes caps the pending buffer — beyond this the renderer
	// flushes immediately so a very long uninterrupted answer never stalls
	// visible progress.
	chatMaxBufferedBytes = 400
	// chatSignalBuf is the buffering for the graceful-closeout channel: 1 is
	// enough — a second identical signal while the first is being handled is
	// dropped (the first already started the graceful closeout).
	chatSignalBuf = 1
	// statusTokensHeavy is the token total at which the status line says the
	// context is large. It is a NAMED threshold (Rule 1) because it is a
	// judgement the reader will want to change in one place.
	statusTokensHeavy = 60000
	// statusCtxHeavyMarker is RETIRED (bug F; re-applied by task 01a0e394 onto a
	// master that had already spent the last cell on the cache pair). This note is
	// the record of WHY, so a later session does not re-add a mark by taste.
	//
	// WHAT IT WAS: one "!" appended to the aligned context sub-cell once ctxUsed
	// reached statusTokensHeavy (60000), to mark "heavy territory". It was a GLYPH
	// rather than a second colour because the row is already GOLD — every status
	// field carries its table's own gold (internal/lineedit/palette.go's statusFg /
	// palette256StatusFg / statusGold, commit 4265324c) — so a warn spelled as a hue
	// would have had to compete with the gold the row already spends, and a warn that
	// must survive with colour OFF has to be a character. The threshold was
	// statusTokensHeavy — the SAME one the historic token field used — so the row
	// warned at one place, not two.
	//
	// WHY IT GOES (each reason measured, none of them aesthetic):
	//   · ITS THRESHOLD IS ON THE WRONG SCALE. 60000 is 30% of the deployed window
	//     (internal/agent/context.go: DefaultContextWindow = 200000) and 26% of the
	//     level at which trimming actually fires (DefaultCompactTriggerRatio 0.75 —
	//     the same file — i.e. 150000). The constant is an ABSOLUTE inherited from
	//     the token field this sub-cell replaced; the row is a ratio read-out now,
	//     and a ratio warns as a FRACTION of its denominator or not at all. A mark
	//     that fires at 30% teaches its reader to ignore it — the opposite of what
	//     the accent on statusCtxCol was for, and why the field read as dead weight.
	//   · IT SAYS "ERROR" AND HAS NO CELLS TO SAY LESS. Ten cells to its left the
	//     same glyph is the shell's non-zero-exit marker (and this chat's per-command
	//     failure glyph); with no legend "ctx 92.1k/200.0k!" reads as a fault in the
	//     NUMBER. A one-cell mark whose nearest documented meaning is "something is
	//     wrong" cannot be taught a weaker meaning within its budget.
	//   · ITS CELL IS CONTESTED, and the contest went to evidence. The sub-cell's
	//     reservation is spent to its last cell by the pair's honest worst case PLUS
	//     the quantifier the same bug requires (label 3 + gap 1 + pair 13 + quant 1 =
	//     18; statusCtxBarCells derives to 0), so no cell is free. Between a mark
	//     that repeats what the pair already shows and one that supplies what the
	//     digits cannot carry, the informative one keeps the cell.
	//   · RE-THRESHOLDING WAS THE ONLY ALTERNATIVE, and it is worse: at the real
	//     pressure point (0.75 × window) the mark becomes a compaction PREDICTION,
	//     which needs more than one cell to be honest (a word on WHICH limit is
	//     warning — exactly the identity statusCtxQuantGlyph now carries).
	//
	// WHAT REPLACES ITS FUNCTION: the denominator's own identity
	// (statusCtxQuantGlyph) states the fact the mark gestured at — "this number is
	// counted against gopi's working budget" — so a reader who sees 92.1k/200.0k≡
	// can weigh the pressure himself against a denominator whose KIND is on the row.
	//
	// The UNALIGNED fallback keeps its historical word form
	// (statusTokensHeavySuffix, " (large)") — see its note for why a word survives
	// where a glyph did not.
	statusCtxHeavyMarker = ""
	// statusModelShortMax caps the model name shown on the status line, so a
	// long provider string cannot push the live hint off the row.
	statusModelShortMax = 24
	// statusLiveSent / statusLiveThinking / statusLiveResponding are the live
	// hint's IN-FLIGHT states, in the order a turn walks them. They are distinct
	// words because they are distinct waits, and telling them apart is the whole
	// assurance the operator asked for: "I sent it and the model has not answered
	// yet" (sent) is not "the model is reasoning" (thinking) and neither is "the
	// model is writing the answer" (responding).
	//
	// Every word is bounded by the tick constants below, which is what lets all
	// three live in ONE reserved column (statusLiveCol) without the row moving
	// when the state changes (Rule 1: the widths are tied back to the words by
	// TestStatusLiveWordsFitTheirColumn).
	statusLiveSent       = "sent"
	statusLiveThinking   = "thinking"
	statusLiveResponding = "responding"
	// statusLiveIdle is the live hint at the prompt.
	statusLiveIdle = "ready"
	// statusLiveKeepalive is how often an in-flight hint is REPAINTED while its
	// state lasts. It exists because a state painted only when it changes is what
	// the operator's screen showed: the row then sits on one value for the whole
	// wait, and a single lost wake leaves it on the PREVIOUS one — measured, the
	// idle "ready" while the turn was in flight. The period is a compromise: much
	// longer and the assurance is not continuous, much shorter and the read loop
	// wakes the terminal for no new information.
	statusLiveKeepalive = 2 * time.Second
	// statusLiveAnimationFrames is the animation a keepalive frame advances, a
	// dot per frame. Moving ONE character is the cheapest possible "still alive"
	// signal for the human, and — because a repaint is deduplicated on the row's
	// composed TEXT — it is what makes the keepalive's frame a REAL repaint
	// instead of a wake that draws nothing (AnswerRenderer.Tick drops an
	// unchanged row). TWO frames, not three: the word on its first frame and the
	// dot on the next, which is exactly what the widest state word plus its
	// animation fits into the reserved column (TestStatusLiveWordsFitTheirColumn
	// — "responding" plus one cell IS the column).
	statusLiveAnimationFrames = 2
	// statusLiveAnimationChar is the character the animation advances. It is a
	// ONE-CELL character: the row's geometry is arithmetic on cell widths, so a
	// multi-cell frame would shear the column it lives in.
	statusLiveAnimationChar = "."
	// statusLiveInFlightGap separates the state word from its animation, and it
	// is a NO-BREAK SPACE (U+00A0): the row's own separator (StatusSep) is the
	// field boundary the aligned layout and the narrow-terminal field drop both
	// split on, so a field value that carried one would be cut in half. The
	// no-break spelling is one cell wide and never splits a field.
	statusLiveInFlightGap = "\u00a0"
	// statusLiveWordSent / statusLiveWordThinking / statusLiveWordResponding are
	// the in-flight words AS THEY APPEAR ON THE ROW (word + gap), i.e. the state
	// names the surfaces outside this file address. They are built HERE from the
	// words and the gap, so a caller names a state rather than assembling a
	// spelling (Rule 1b/14).
	statusLiveWordSent       = statusLiveSent + statusLiveInFlightGap
	statusLiveWordThinking   = statusLiveThinking + statusLiveInFlightGap
	statusLiveWordResponding = statusLiveResponding + statusLiveInFlightGap
	// statusLiveThinkingShort / statusLiveRespondingShort are the SHORT spellings of
	// the two states whose full word is too wide to share the reserved block with the
	// compaction counter ("⧉30 · responding" is 15 cells against statusLiveCol's 13,
	// so the whole combination was dropped and the counter — the read-out the operator
	// filed the defect for — vanished exactly while a turn was in flight).
	//
	// THE SHORT WORD IS A CONSTANT, not a truncation: "resp"/"think" are the words
	// themselves, spelled short (Rule 1b — the live block never CUTS a state word, it
	// chooses a spelling). "sent" needs no short form: it is already inside the budget
	// with the counter ("⧉30 · sent" is 9 cells), so its short spelling would be the
	// same string and has no definition of its own (statusLiveStateWords).
	statusLiveThinkingShort   = "think"
	statusLiveRespondingShort = "resp"
	// statusLiveWord*Short are those words AS THEY APPEAR ON THE ROW (word + gap),
	// built from the same constants as the full pair so a caller names a state rather
	// than assembling a spelling (Rule 1b/14).
	statusLiveWordThinkingShort   = statusLiveThinkingShort + statusLiveInFlightGap
	statusLiveWordRespondingShort = statusLiveRespondingShort + statusLiveInFlightGap
	// statusLiveAnimationChars is every character the animation can put at the
	// end of the live field (the gap is stripped with the dots): the set a
	// reader uses to strip the animation and read the STATE back off the row.
	statusLiveAnimationChars = statusLiveAnimationChar + statusLiveInFlightGap
	// toolGlyph marks a tool-activity row on the status line. It is the same
	// glyph the web UI puts on its `event: tool` rows, so the two surfaces read
	// alike. Single-width, so it cannot shear the status row. Its VALUE lives in
	// internal/lineedit — the package that composes and emphasises the row — so
	// the marker the REPL writes and the marker the palette styles are one
	// definition (Rule 14); this alias is the local spelling.
	toolGlyph = lineedit.ToolGlyph
	// statusToolRowMax caps the tool-activity row ("⚙ bash head -3 go.mod") in
	// RUNES. It is also what statusToolCol reserves (plus the ellipsis the cut
	// may add), so the two cannot disagree: the column is exactly the widest
	// field the formatter can produce. The row is the newest and least load-bearing part of the status
	// line, and an uncapped digest wraps the line to a second terminal row —
	// which costs the editing line a row and, on a short terminal, pushes the
	// input off-screen (the "I cannot type" report). Chosen so that the whole
	// row still fits beside a short model name and the counters on an 80-column
	// terminal; a longer digest is ellipsized (truncateRunes), never wrapped.
	statusToolRowMax = 32
)

// The status row's COLUMN layout. Every field's reserved width is NAMED here
// (Rule 1) and there is exactly ONE per field (Rule 14): the row is composed in
// chatStatusSource.columns from these, so a field cannot be laid out at one
// width and measured at another. The reservation is what fixes the alignment —
// with the row's fields concatenated, a field appearing (the tool row, while a
// tool runs) or a counter growing a digit (iter9 → iter10, 999 → 1.0k tokens)
// slid every field to its right, which is what made the row look like it was
// jumping while the agent worked.
//
// The widths are chosen from the values the fields actually take:
//   - the model name is capped at statusModelShortMax by modelShort for the
//     UNALIGNED path, and the COLUMN is that cap or narrower: no model string
//     can widen the row, and a short one is padded to the column;
//   - the turn field is the word plus the digits statusTurnCol reserves
//     (statusTurnWordCells + statusTurnDigitsMax) — the reservation is arithmetic
//     on turnLabel's own label, not measured off one rendering;
//   - the mode hint ("tools") is a fixed word;
//   - the token field's formatCount output is at most five cells ("999.9k")
//     plus the word " tokens" and the heavy-context suffix, which ride the
//     column's own budget so a heavy context cannot widen the row either.
//
// The WHOLE geometry is additionally bounded by statusLayoutBudgetCols: the
// aligned form only runs on a terminal at least as wide as the pinned layout, so
// a geometry wider than an ordinary terminal would mean the alignment exists
// only on paper. That bound is asserted here, at compile time, so a future field
// cannot be added by pushing the row past the terminals it is meant for.
const (
	// statusLayoutBudgetCols is the narrowest terminal the aligned status row
	// must fit: 100 columns is an ordinary width and the one the layout's own
	// grid evidence is captured at. It is the budget statusColumnsAtCols is
	// constrained by (below), not a description of any particular terminal.
	//
	// It is a CEILING (statusPinCols ≤ statusLayoutBudgetCols), and since task
	// 01a0e2e9 the two are EQUAL: the row shown at statusPinCols occupies exactly
	// 100 cells, and statusPinSlackCells is 0. The equality was reached by
	// correcting a reservation (the cache pair's separator was counted twice — see
	// statusCachePairSepCells), not by spending air, so the geometry draws in every
	// cell it claims. A future field therefore has to find its cells by re-deriving
	// a reservation, which is the same discipline that fixed this one.
	statusLayoutBudgetCols = 100
	// statusPinCols is the row's DRAWN width: the cell count the composed row
	// occupies at the pin, and the narrowest terminal that hosts the aligned
	// layout. It is the pin the ladder steps down from and the width the
	// caller's column assertion renders at (Status.Line(statusPinCols)) — ONE
	// definition, so the assertion and the production row cannot drift.
	//
	// Why it EQUALS the budget (task 01a0e2e9, unchanged by 01a0e394): the cache-pair
	// correction spent the budget to its last cell — pin 100, slack 0, and every one of
	// those cells DRAWN — and that equality came from fixing a double-counted
	// reservation, not from spending air. Bug F then needed a cell for the context
	// denominator's qualifier, and this pin DID NOT MOVE to give it one: the cell was
	// paid INSIDE the context sub-cell, out of that sub-cell's own label yield (the
	// pinned row composes "ctx 92.1k/200.0k≡" from the same 17 cells that composed
	// "ctx 92.1k/200.0k" — see statusCtxCol's arithmetic and statusCtxField's
	// composition). No top-level column gave a cell, and no column was left reserving
	// space nothing draws in, which is the constraint a slack of 0 imposes on every
	// future field too: this pin is the sum of what the row DRAWS. It is spelled so
	// that it cannot be typed wrong, and the compile-time assertions below tie the
	// composition's fit check and the drawn row to the same number.
	statusPinCols = statusModelCol + statusTurnCol +
		statusToolCol + statusMetricsFormCol +
		// the separators between the columns above come from the layout's own
		// count (statusColumnSepTotalN), never from a literal: a form which
		// SHEDS a column also sheds its separator (see that constant).
		statusColumnSepTotalN*lineedit.StatusColumnSepCells +
		lineedit.StatusLineGapCells + statusLiveCol

	// statusModelCol is the model field's reserved width: wide enough for the
	// model names this UI displays ("deepseek-flash" is 14 cells; the column
	// leaves a cell of slack and a shorter name is padded), and a longer
	// provider string is elided inside the column by the row's own field cut
	// (statusEllipsis) rather than widening the row — the column IS the row's
	// geometry. modelShort's own cap (statusModelShortMax) stays the policy for
	// the UNALIGNED label path, which has no columns to spend.
	statusModelCol = 14
	// statusTurnCol reserves the iteration field. THE RESERVATION IS ARITHMETIC ON
	// THE FORMATTER (task 01a10dd0), because a width measured off ONE rendering is
	// a claim about that rendering only:
	//
	//	statusTurnWordCells 4  (statusColumnNameTurn's own literal)
	//	+ statusTurnDigitsMax 3 ("999" — the widest count this column reserves for)
	//	= 7 = statusTurnCol
	//
	// SO THE COLUMN HOLDS THE WORD PLUS UP TO THREE DIGITS. What the formatter had
	// to stop printing to make that true is the SPACE it used to put between them:
	// "iter 100" is 8 cells, so the FIRST three-digit count this row met was cut by
	// padStatusColumn into "iter 1…" — a truncated NUMBER, the one thing this row
	// must not print. The space is gone ("iter100" is 7 cells, exactly the
	// reservation), and the withdrawn reading that a wider count was "a different
	// defect" is withdrawn with it: the defect WAS this cell and the label's shape,
	// and the bound above is now stated here instead of left implied.
	//
	// THIS COLUMN WAS THE FIRST CANDIDATE DONOR FOR BUG F'S QUALIFIER AND WAS
	// REFUSED, and the refusal is a measurement rather than a taste: task 01a0e2e9's
	// cache-pair change tried exactly this (7 → 6) and its suites reverted it (see
	// that commit's message), and the count it would cut is a REAL value — "iter12"
	// is an ordinary mid-session count, not air. So the qualifier is funded by the
	// context sub-cell's own label yield instead (see statusCtxCol) — no column on
	// this row was left with a cell nothing draws in.
	//
	// (UNCHANGED by task 01a0e2e9: the cache pair's two extra cells were paid for
	// out of statusLiveCol, not here.)
	//
	// statusTurnWordCells is the label word's own width, spelled once so the
	// reservation can be arithmetic on it rather than a hand-counted literal.
	statusTurnWordCells = 4
	// statusTurnDigitsMax is the digits this column reserves — three, i.e. counts
	// below 1000. It is the bound the reservation STATES, and the row's own test
	// renders the widest count it admits (statusTurnDigitsMax's own extreme) at the
	// pin, so the formatter and this number cannot drift apart silently.
	statusTurnDigitsMax = 3
	statusTurnCol       = statusTurnWordCells + statusTurnDigitsMax
	// statusToolCol reserves the tool-activity column. It is NARROWER than the
	// row cap statusToolRowMax on purpose: the cap bounds the UNALIGNED label
	// (which has no columns and just avoids wrapping), while this bounds the
	// column, whose width is what the whole row's geometry is built on. A digest
	// that does not fit the column is elided inside it (statusEllipsis), so the
	// columns to its right never move; the digest's full text is still in the
	// tool's own output in the answer band.
	//
	// THE COLUMN IS THE ROW'S ONE TOOL STATE (operator bug #5, task 01a0e4c7), and
	// it is WIDER because it ABSORBED what used to say the same thing twice. The
	// retired mode column's whole content was "tools" and this column's idle
	// content was "no tool" — the SAME field stated twice, negatively, nine cells
	// apart, which reads as a contradiction in left-to-right order. What the row
	// spent on the second statement comes here: the retired column's own
	// statusModeColRetired cells, and the top-level separator that joined it
	// (statusColumnSepTotalN 4 → 3). The absorption is SPELLED as arithmetic rather
	// than as a hand-totalled literal, so a reader can see that the pin did not
	// move: the cells the row already had are the cells it still spends, now on ONE
	// statement of the fact instead of two halves of it.
	//
	// THE /VOICE FIELD DID NOT TOUCH THIS COLUMN (task 01a116b0), and the reason is
	// a measurement worth keeping: the field was first budgeted as a wider COLUMN or
	// a wider live block, and both were refused by what the row already measures.
	// The block's reservation must equal the widest form it DRAWS — a reservation
	// wider than that pulls the tail left while the mode is on and pushes it back
	// when the mode ends, i.e. the row visibly jumps — and this column's cells are
	// all drawn by the digest. So the field was sized to the room the block already
	// had instead (voiceChatLiveWordCells, chat_voice.go: a three-cell word, no
	// glyph), and NO geometry constant in this file moves for it — statusPinCols
	// stays 100, statusPinSlackCells stays 0, and every reservation below is the
	// number it was.
	statusToolCol = statusToolColOwn + statusModeColRetired + statusToolColWayBreadth
	// statusToolColOwn is the tool-activity column's own width before bug #5's
	// absorption: the digest column's historical spec (a glyph, a tool name and as
	// much of the digest as the 16 cells allow).
	statusToolColOwn = 14
	// statusModeColRetired is the width of the RETIRED mode column: the cells the
	// tool column absorbed. The name says RETIRED because the column is gone from
	// statusRowColumns — this constant is not a column's reservation any more, it
	// is the measurement of what the absorber inherited, and it is referenced by
	// the arithmetic above (Rule 13: a constant kept alive only by a comment would
	// be the defect this rule names).
	statusModeColRetired = 5
	// statusToolColWayBreadth is the room the tool field needs to name WHAT ITS
	// DIGEST IS ABOUT: the two-segment way marker the row learned in task
	// 01a0e3db ("⚙ bash · retrieval: …") is the shape that made the operator's row
	// readable, and the 16-cell column it used to live in was sized before that
	// marker existed. Measured, not guessed: at 16 cells the aligned row cut the
	// way marker away entirely, because the field is composed INSIDE the column and
	// the cut reached the marker before the digest — i.e. the column was reserving
	// cells for a field it could not draw as designed. Four cells carry the marker
	// and a couple of digest characters.
	//
	// The absorbed 5 and this 4 are the whole of the widening; the fifth cell is
	// NOT spent, it is RETURNED to the row, so reading the absorption as "the
	// separator's 4 went in too" would put the pin at 101 (> statusLayoutBudgetCols,
	// which the compile-time guard below refuses) and 102 would wrap the row. The
	// pin does not move: statusPinCols stays 100 and statusPinSlackCells stays 0.
	statusToolColWayBreadth = 0
	// statusCtxCol reserves the context sub-cell: the fact NAME (statusCtxLabel),
	// the ABSOLUTE used/max pair — the PRIMARY fact — and the window's
	// percentage (see statusCtxField). This column SUBSUMES the bare token field
	// the row used to carry (statusTokensCol): the old field showed the session's
	// cumulative token total, and the context read-out is that same quantity
	// measured against the window it is spent from — the total is still reported,
	// as this sub-cell's whole content when no window is known.
	//
	// RE-PARTITION (task 01a0ddab, the label / percentage decision): the sub-cell
	// is now the fact NAME, its gap, and the ABSOLUTE pair. The percentage and its
	// gap are gone (see the FOLD note below), and the cells they held pay for the
	// label and for the pair's HONEST worst case. The arithmetic, derived and
	// two-sided (TestStatusMetricsGroupArithmetic and TestStatusLadder_FormsNarrowStrictly
	// re-derive every number here from the constants, never from this prose):
	//
	//	sub-cell   statusCtxCol     = label 3 + gap 1 + pair 13          = 17
	//	group      statusMetricsCol = 12 + 1 seam x2 + cache 8             = 22
	//	           statusMetricsFormCol = statusMetricsCol + rate 10       = 32
	//	pin        statusPinCols    = 69 + 4 seps ×4 + gap 2 + live 13    = 100
	//	slack      statusPinSlackCells = 100 − 100                       =  0
	//
	// (69 is the column reservations summed — model 14 + iter 7 + mode 5 + tool 16
	// + the group 27; the group is ONE column of the row and its own 2-cell seam is
	// inside that 27, which is why the 4 top-level separators below are counted
	// separately. The sum is the same however it is bracketed, and it is asserted
	// from the constants rather than from this prose.)
	//
	// RE-PARTITION (task 01a0e394, bug F's qualifier — and it is a re-partition WITHIN
	// this sub-cell, not across the row). Bug F adds the qualifier — the cell that says
	// WHAT the denominator IS (statusCtxQuantGlyph / statusCtxQuantCells) — and the
	// budget had nothing to give: statusPinSlackCells is 0 since task 01a0e2e9, so the
	// pin was already the budget exactly. The cell is therefore paid out of this
	// sub-cell's own widest form. The pinned row composed
	//
	//	ctx 92.1k/200.0k        (before: label 3 + gap 1 + pair 13 = 17 cells)
	//	ctx 92.1k/200.0k≡       (after: the same 17 — the qualifier is the cell the
	//	                         LABEL's 4 cells yield to it, not a new cell)
	//
	// so the sub-cell's reservation is UNCHANGED at 17 cells and the row's arithmetic
	// does not move: 14 + 7 + 5 + 16 + 27 + 4×4 + 2 + 13 = 100, slack 0.
	//
	// WHY THE LABEL IS WHAT YIELDS. This sub-cell already makes that argument for the
	// wide-pair case (see the note below the composition): the label is a decoration
	// NAMING the fact, while the pair is the fact itself and the qualifier is the
	// fact's IDENTITY — the one field that closes the ambiguity that was reported (a
	// 200.0k denominator read as the model's limit, when the backend advertises 1M and
	// gopi trims at the window it states). A form that kept a 3-cell word and dropped
	// the identity would spend its cells on the part a reader can infer from the row's
	// fixed columns and drop the part he cannot. The PAIR is never the yielder: a cut
	// NUMBER is the one thing this row must not print.
	//
	// WHY NO CELL CAME FROM ANOTHER COLUMN — MEASURED, not preferred (the probes are
	// in this task's evidence): every top-level column is exactly its widest drawn
	// value — model 14 = the longest model name this UI displays, mode 5 = "tools",
	// tool 16 = the digest column's own spec, iter 7 = the word plus its reserved
	// digits (statusTurnWordCells + statusTurnDigitsMax), live 13 =
	// "1h02m · ready" — and the cache sub-cell's 8 is the BOUNDED formatter's own
	// derived width, i.e. the correction task 01a0e2e9 landed, which this change does
	// not reopen. Taking a cell from statusTurnCol is the one that LOOKS free and is
	// not: task 01a0e2e9 tried exactly that (7 → 6) and its suites reverted it, because
	// the count the column holds is drawn. Re-partitioning a column
	// that draws nothing was therefore not available, and inventing a cell would break
	// the 100-cell budget the geometry is pinned to — so the cell came from the ONE
	// place this sub-cell already documents as its yielder.
	//
	// RE-PARTITION (task 01a0e2e9, the cache pair's BOUNDED width): the cache sub-cell
	// went 7 → 8 and the pin 99 → 100, so the named slack is now ZERO. The cell came
	// from the reservation's own error: the pair's separator was counted TWICE (each
	// side already carries its arrow, so the second glyph IS the boundary —
	// statusCachePairSepCells is 0), which is a cell reserved over nothing. The
	// reservation is now the composed string's own width, and the pin spends the budget
	// exactly — that is what the budget guard's own note means by the intended
	// state being EQUALITY. See statusPinSlackCells for why zero is honest here and not
	// the retired-column defect.
	//
	// The pair's 13 is what pays for the label without inventing cells: the old
	// reservation claimed 10 for a pair whose widest REAL form is 13 — measured,
	// formatCount prints "200.0k" (6 cells) for the production window
	// (internal/agent/context.go's DefaultContextWindow, 200000), so the sub-cell
	// that claimed 10 CUT the operator's own row ("ctx 13.1k/200.0k" is 16 cells
	// against a 14-cell sub-cell). The mis-measure survived because the fixture's
	// window (64000 → "64.0k", 5 cells) is not the deployed one; the ceiling is
	// pinned by TestStatusMetricsGroupArithmetic now.
	//
	// RE-PARTITION (task 01a0dcd1, the money sub-cell's retirement): the pair is
	// primary here — the Hermes order. It was the other way round before, because a
	// 9-cell sub-cell inside a geometry that also paid 7 cells for a money read-out
	// had no room for a denominator. The operator's criticism of that form is why
	// this column exists: "the '6%' is a percentage such that you know the backend
	// LLM spec, say 1M for context window, what if the model changes?" — a
	// percentage is meaningless without the window it is a percentage OF, and
	// ContextWindow is CONFIGURATION (internal/agent/loop.go, DefaultContextWindow),
	// so the denominator has to be ON the row. The two extra cells the pair needs
	// were paid for out of the money sub-cell, which is GONE (see the RETIRED note
	// on statusCostCol).
	//
	// The reservation is that sum and NOTHING ELSE: there is no cell for a bar (see
	// statusCtxBarCells) and none for a percentage — the FOLD decision below. The
	// QUALIFIER (bug F) is composed inside these cells rather than reserved beside
	// them: the pinned row draws "ctx 92.1k/200.0k≡" in the same 17, with the label
	// yielding the cell (statusCtxField), which is why no term was added here.
	statusCtxCol = statusCtxLabelCells + statusCtxBarGapCells + statusCtxPairCells +
		statusCtxQuantCells
	// statusCtxPairCells / statusCtxLabelCells / statusCtxPctCells /
	// statusCtxBarGapCells / statusCtxBarCells compose that sub-cell, and they are
	// DERIVED rather than hand-summed so the budget guard cannot be satisfied by a
	// typo.
	//
	// FOLD (task 01a0ddab): the window PERCENTAGE is not on this row any more, and
	// the cells it and its gap held are the label's. What the code can measure
	// decides it, and the measurement is negative:
	//
	//	· The row ALREADY carries three "%"-form read-outs' worth of vocabulary it
	//	  does not draw — statusCtxPct's clamp, the bar's glyphs — and the ONE
	//	  quantity on the row that is a PERCENTAGE is a cache ratio, which
	//	  statusCacheField does not draw either. So a "6%" on this row is a
	//	  percentage with no label, sitting beside a second, differently-derived
	//	  pair — the operator read exactly that as a cache hit rate ("13.1k/200.0k?
	//	  what is this? cache-hit/cache-miss? '6%'?").
	//	· The pair states used AND max: both operands of the ratio are on the row,
	//	  so the percentage is a DERIVED restatement of two numbers a reader already
	//	  has — a lower-precision copy of the pair's own first operand over its own
	//	  second. That is the whole of the argument against it, and it is a fact
	//	  about what is drawn, not a preference.
	//	· Its denominator is CONFIGURATION (internal/agent/context.go's
	//	  DefaultContextWindow, 200000) rather than a measurement, which is what
	//	  demoted it behind the pair in the first place (task 01a0dcd1).
	//
	// The alternative — keeping it and labelling it "6% ctx" — was rejected on
	// CELLS rather than on taste: 5 + 1 + 10 = 16 for one fact's worth of
	// confirmation against 3 + 10 = 13 for naming the fact, and the label answers
	// the ambiguity the operator actually reported while the percentage answers
	// the one he had already ruled on twice.
	//
	// statusCtxBarCells is STILL what the pair and the label leave over — and it
	// derives to ZERO, which is a MEASURED fact about the budget rather than a
	// stylistic choice: a bar beside the labelled absolute pair costs more cells
	// than the 100-column row has left after the model, tool and live contracts.
	// statusCtxField only composes a bar when this constant says there is room, so
	// a future column that affords one turns it back on by arithmetic alone, and
	// the cell it would need is never reserved-but-undrawn in the meantime (the
	// retired cache column's defect, in the opposite direction).
	//
	// Measure before changing any of them: formatCount prints at most five cells
	// plus a unit ("999.9k", "999.9M"), and the label is a fixed word.
	statusCtxLabelCells = statusCtxLabelCellsPinned
	statusCtxPairCells  = 2*statusCtxPairSideCellsPinned + statusCtxPairSepCells
	statusCtxQuantCells = statusCtxQuantCellsPinned
	// statusHitRateLabelCells is the cumulative reading's MARKER in cells — the
	// one-cell Σ (statusHitRateCumulativeGlyph). It used to be a whole word
	// ("hit", 3): the epic's second reading (this turn's, statusHitRateDeltaGlyph)
	// is funded by the cells the word gave back, which is why the two readings fit
	// inside the reservation the one word occupied. It is spelled from the string
	// by TestStatusMetricsGroupArithmetic, so a marker renamed without re-measuring
	// fails the suite rather than the row.
	statusHitRateLabelCells = statusHitRateCumulativeGlyphCells
	statusCtxPctCells       = statusCtxPctCellsWidest
	statusCtxBarGapCells    = statusCtxBarGapCellsWidest
	statusCtxBarCells       = statusCtxCol - statusCtxLabelCells - statusCtxBarGapCells -
		statusCtxPairCells - statusCtxQuantCells
	// statusCtxLabelCellsPinned is the context sub-cell's fact name in cells: the
	// "ctx" in "ctx 42.1k/64.0k". It is spelled from statusCtxLabel rather than
	// counted by hand — TestStatusMetricsGroupArithmetic measures the string
	// against this number, so the two cannot drift (the same tie
	// statusCtxBarGapCellsWidest has to statusCtxBarGap).
	//
	// WHY A NAME IS SPENT HERE AT ALL: the row carried TWO token pairs with the
	// same unit and no words — "13.1k/200.0k" (context used/window) and
	// "↑12k↓4k" (cache served/written) — and the operator, reading his own live
	// row, could not tell which was which. The cache pair carries glyphs
	// (↑/↓, which no context number can produce); the context pair carried
	// nothing. A label is the smallest cell count that makes the distinction
	// visible WITHOUT COLOUR (the row is already GOLD — every status field carries
	// its table's own gold (internal/lineedit/palette.go's statusFg /
	// palette256StatusFg / statusGold, commit 4265324c), and
	// TestPaletteCarriesNoInformationAlone forbids a distinction riding on that
	// colour alone), and it is 3 cells against the 4 the
	// percentage it replaces needed, so the geometry gains a cell.
	statusCtxLabelCellsPinned = 3
	// statusHitRateLabelCellsPinned is the cumulative reading's MARKER in cells
	// (the Σ, one cell), pinned exactly as statusCtxLabelCellsPinned is: the
	// arithmetic test measures the STRING against this number, so a marker renamed
	// or widened cannot drift away from the reservation (Rule 1 / Rule 13).
	statusHitRateLabelCellsPinned = statusHitRateCumulativeGlyphCells
	// statusCacheHitRateBoundaryCells is the cell the readings cost BEYOND their own
	// text: the gap that joins the reading to the pair it rides ("↑12k↓4k" then
	// "Σ74.5%"). It is ZERO, and it is a named term rather than an omission for the
	// same reason statusCachePairSepCells is: the reading now OPENS WITH A GLYPH
	// (Σ), so the marker itself detaches it from the pair's last character and a
	// gap would be a cell no character occupies — the reserved-over-nothing defect
	// this row has already paid for once. It is the pair's own precedent: there the
	// second side's ↓ was the boundary.
	statusCacheHitRateBoundaryCells = 0
	// statusHitRateDeltaCells is the DELTA reading's whole width — its own marker
	// plus the widest percentage it states — summed here from the two named terms
	// so statusCacheHitRateCells counts it the way it counts the cumulative
	// reading's (Rule 1/Rule 13: a term assembled at the call site is a number no
	// reader can check against the strings it names).
	statusHitRateDeltaCells = statusHitRateDeltaGlyphCells + statusHitRateDeltaDigitsCells
	// statusCtxPairCellsWidest is formatCount's own worst case per side of the
	// pair — SIX cells, not five: "%.1fk" prints "200.0k" for the production
	// window (internal/agent/context.go's DefaultContextWindow, 200000) and
	// "999.9k" up to a million, against "1000.0k" (7) above it. The previous
	// reservation said 10 = 5+1+4 and was MEASURED WRONG on the deployed window:
	// "ctx 13.1k/200.0k" is 16 cells against the 14-cell sub-cell that 10 bought,
	// so the row cut the operator's own pair. A pair is two sides of that class
	// plus the separator: 6+1+6 = 13. What a SEVEN-cell side does (a 1M window:
	// "999.9k/1000.0k", 14) is not a wider reservation but a narrower FORM — the
	// label yields, the fact stays (statusCtxField), because the budget has no
	// cells for a 19-cell sub-cell.
	//
	// statusCtxPctCellsWidest (a clamped percentage, "100%", 4) and
	// statusCtxBarGapCellsWidest (the one space that separates a form's parts) are
	// the measured widths the FOLD's arithmetic is stated against; they are no
	// longer SPENT on a percentage (there is none on the aligned row) but the gap
	// still separates the label from the number it names.
	statusCtxPairCellsWidest   = 2*statusCtxPairSideCellsWidest + statusCtxPairSepCells
	statusCtxPctCellsWidest    = 4
	statusCtxBarGapCellsWidest = 1
	// statusHitRatePctCellsWidest is the widest rate the hit-rate composer can
	// print — the CLAMPED percentage with all its fraction digits, "100.0%", six
	// cells — and it is DERIVED from terms that already exist rather than counted
	// by hand: the percentage ceiling's own measured width
	// (statusCtxPctCellsWidest, "100%", 4), the decimal separator the formatter
	// emits (statusHitRatePctPointCells), and the named fraction-digit count. A
	// format change that the derivation cannot follow is what the arithmetic test's
	// measurement of statusCacheHitRate's own widest output catches (Rule 1/Rule 13).
	//
	// It is a MEASUREMENT of the composer's output, not a term in this
	// reservation: the hit-rate read-out is a RE-COMPOSITION inside statusCtxCol
	// (17), and the assertion that it fits is the one that walks the counters'
	// extremes — the label is 3, the gap 1 and this 6, so the widest labelled form
	// is 10 cells of the 17, which is why the fraction costs no geometry.
	statusHitRatePctCellsWidest = statusCtxPctCellsWidest + statusHitRatePctPointCells + statusHitRateDecimals
	// statusCtxPairSideCellsWidest is ONE side of the context pair in cells, and it
	// is the MAX of two DERIVED terms — the shape statusCountSideTextWidest already
	// has, and for the same reason: the pair is drawn by the ladder's formatter, and
	// the sub-cell it sits in is a cell budget that must not move.
	//
	//	· statusCountSideTextWidest is the FORMATTER's ceiling — the widest one side
	//	  of ANY count can be, for every int (the ladder is total: MaxInt is "92G").
	//	  This is the term that answers the epic's question, and it is why the pair
	//	  can never state a magnitude in a unit the ladder would not have chosen.
	//	· statusCtxPairSideCellsPinned is the SUB-CELL's ceiling — what the budget
	//	  re-partitioned for this pair when the label and the qualifier were paid for
	//	  (statusCtxCol's arithmetic). It is kept, as a named pin rather than a
	//	  forgotten literal, because the pin statusPinCols = statusLayoutBudgetCols
	//	  with slack 0 is a hard contract: shrinking this term would leave six cells
	//	  of every top-level column reserving space the drawn row does not occupy,
	//	  which is the retired-column defect at the row's scale.
	//
	// A pair whose side is 3 or fewer cells (the ladder's own worst case) is
	// therefore drawn whole inside a reservation that also holds a 6-cell side — the
	// room the pinned geometry already carries, and room a pair really uses: a
	// 999-token window renders "999" (3) beside a "42k" (3), the full 13-cell pair.
	// What the ceiling is NOT is a second formatter's spelling: the retired
	// formatCount's "200.0k"/"1000.0k" (6 and 7 cells) is gone, and with it the
	// ".0" tail and the k unit on a megabyte-scale count. The assertion that ties
	// the form to the ceiling is TestStatusMetricsGroupArithmetic, which walks the
	// formatter's rung boundaries and the extremes of the int type against THIS
	// constant.
	statusCtxPairSideCellsWidest = max(statusCountSideTextWidest, statusCtxPairSideCellsPinned)
	// statusCtxPairSideCellsPinned is the context sub-cell's own per-side ceiling,
	// named because it is the term that holds the geometry still: 6 is what
	// statusCtxCol's re-partition (task 01a0e394, the qualifier's cell) sized the
	// pair for, and statusCtxCol = label 3 + gap 1 + 13 is the sub-cell the group
	// and the pin are summed from. Derived, not typed, is the RULE for a formatter's
	// width — this is not a formatter's width but a column's, and a column's width
	// is what the budget decides.
	statusCtxPairSideCellsPinned = 3
	// statusCountCtxDigitsWidest / statusCtxPairSideCellsDrawn / statusCtxPairCellsDrawn
	// are the CONTEXT PAIR's own digit budget, and the cell bound derived from it.
	//
	// THE DEFECT THEY CLOSE (the operator's `ctx 13k/1M≡` on a 200000-token budget): the
	// pair was drawn by formatCountBounded, whose allowance is the CACHE pair's
	// (statusCountWholeDigitsWidest = 2), and 200000 needs THREE whole digits to be
	// stated in the k rung ("200k"). Two digits sent it up a rung, the tenth of a
	// megabyte rounded away, the bare-unit clamp printed the smallest mantissa, and the
	// row stated a BUDGET FIVE TIMES larger than it is — a denominator lying about the
	// number every percentage beside it is a percentage of. The same collapse drew two
	// DIFFERENT counters as one figure ("1M/1M") over four fifths of the window's range.
	//
	// WHY THE PAIR GETS ITS OWN ALLOWANCE RATHER THAN THE CACHE PAIR'S: the two families
	// are drawn in different cells, by different glyphs, against different reservations
	// (statusCtxPairCellsDrawn here, statusCacheCol there). The cache sides carry an
	// arrow each, so the same cell count buys the context sides one more digit. Handing
	// the context pair three digits is therefore a REPAIR of its spelling, not a
	// re-partition of the row: statusCtxCol, statusMetricsCol, statusPinCols and
	// statusPinSlackCells do not move, and statusCtxPairCells (the pre-fold reservation)
	// is untouched.
	//
	// THE BUDGET IS A SINGLE ONE, FOR BOTH SIDES. A used/max pair is a ratio, so its two
	// sides must be stated in ONE unit system: drawing the denominator at three digits
	// while the numerator stayed at two would print "1M/200k" for 199999 of 200000 — a
	// numerator whose unit is LARGER than its denominator's, on a session that is inside
	// its budget. One allowance decides both sides.
	//
	// WHY A WIDER ALLOWANCE FITS WITH NO GEOMETRY CHANGE, which is the measurement this
	// recovery rests on: the sub-cell reserves statusCtxPairCells (7) but the widest
	// pair it must host is statusCtxPairCellsWidest (13), and the FOLD that spends those
	// extra cells is what statusCtxField documents — where the labelled form does not
	// fit, the LABEL yields and the pair is shown whole. A pair of two 4-cell sides plus
	// the separator is 9 cells, so it composes to 9 + statusCtxQuantCells = 10 cells
	// inside the sub-cell's 12 — i.e. the drawn form rides the SAME yield path the
	// composer already had (Rule 13: the reservation is not re-partitioned, it is
	// reached from the other side).
	//
	// The allowance is THREE digits because that is what the cells buy: a side is the
	// digits plus the unit, and 3 + 1 = 4 is inside the 4 the quantifier leaves. It is
	// still a BOUND, not a truncation — a value the allowance cannot state is rounded at
	// it and carries its unit (boundedCountRectDigits), so the drawn form is bounded by
	// construction for every int, exactly as the cache form is. A FOURTH digit buys
	// nothing on the windows this row receives: 64000, 200000 and 1000000 each state
	// their whole part in three or fewer digits ("64k", "200k", "1M"), and a fourth
	// would only widen every side to 5 cells and spend the label at every magnitude.
	statusCountCtxDigitsWidest = 3
	// statusCtxPairSideCellsDrawn is one side of the drawn pair, derived from the
	// formatter's own terms (base digits for the whole-number class, allowance plus unit
	// for a rung) rather than sampled from a string — the same shape as
	// statusCountSideTextWidest, at this pair's allowance (Rule 13).
	statusCtxPairSideCellsDrawn = max(statusCountDigitsBase,
		statusCountCtxDigitsWidest+statusCacheCountUnitWidest)
	statusCtxPairCellsDrawn = 2*statusCtxPairSideCellsDrawn + statusCtxPairSepCells
	// statusCtxPairSepCells is statusCtxPairSep's width in cells. It is the
	// separator that makes the two counts a RATIO; TestStatusMetricsGroupArithmetic
	// measures the string against it (Rule 13).
	statusCtxPairSepCells = 1
	// statusCacheCol is NOT a top-level column (see the RETIRED note master's
	// cache-column decision carries): the arithmetic has no cell for a seventh
	// top-level column. It IS a sub-cell of the metrics group, where it costs the
	// light field separator instead of a column boundary.
	//
	// RE-PARTITION (task 01a0e2e9, the cache pair's UNBOUNDED WIDTH). 7 was the
	// compact pair's width — and it was measured WRONG, in exactly the way
	// statusCtxPairCellsWidest's note describes for the context pair: the string it
	// was measured against ("↑12k↓4k") is the pair at a FOUR-DIGIT count, while the
	// formatter behind it was unbounded. The operator's own session carries ~1.66M
	// cached tokens, and measured on the composed row:
	//
	//	cacheRead = cacheWrite = 1662000  →  "↑1662k↓1662k"  (12 cells)
	//	row  deepseek-flash · iter9 · tools ·  ⚙ keyword_search ·  ctx 42.1k/200.0k ·↑1662k↓…  4m00s · ready
	//
	// The sub-cell's 7 cells are what the GROUP's reservation counted, so the
	// group's width was honest and the row did not widen — the layout's own field
	// cut took the 5 cells that were missing from the TAIL, and the tail is where
	// the miss-billed side lives: the ↓ arrow and the tokens WRITTEN into the cache
	// were swallowed, leaving a paired read-out that claimed to be the whole story.
	// The reservation was not the only thing wrong; the FORM was — a formatter whose
	// output width is a function of the magnitude of its input cannot be covered by
	// any reservation short of ten digits.
	//
	// So the sub-cell's width is derived from the BOUNDED formatter's own declared
	// terms (statusCacheCountUnitWidest, statusCacheCountDigitsWidest) instead of a
	// sampled string, and
	// the formatter is bounded BY CONSTRUCTION (formatCountBounded): every count it
	// can ever be handed renders in at most statusCacheCountSideCellsWidest cells,
	// both sides whole, at every magnitude. The cells the wider pair costs are paid
	// for by a correction to the reservation itself: the pair's separator was counted
	// twice, and each side already carries its own arrow (statusCachePairSepCells is
	// 0). So no column gave cells up — the sub-cell simply stops reserving a cell no
	// character occupied (statusPinSlackCells, which is now 0).
	statusCacheCol = 2*statusCacheCountSideCellsWidest + statusCachePairSepCells
	// statusCacheCountSideCellsWidest is ONE side of the cache pair in cells, at
	// the WORST case the bounded formatter can produce — a property of that function
	// rather than a measurement of a sample. Its arithmetic is stated once, below,
	// in terms of the formatter's own scale table (statusCacheCountUnitWidest); the
	// number is here so a caller reads the reservation in one place.
	statusCacheCountSideCellsWidest = statusCountSideTextWidest + statusCacheCountGlyphCells
	// statusCachePairSepCells is the width BETWEEN the pair's two sides, and it is
	// ZERO — the second side's own arrow IS the boundary, because each side carries its
	// glyph ("↑12k" then "↓4k": the ↓ that separates them is counted inside the second
	// side's statusCacheCountGlyphCells). Counting a separator here as well is the
	// off-by-one this constant exists to make visible: it cost the pair a cell that no
	// character of the composed string occupied — the retired-reservation defect in
	// miniature, a cell reserved over nothing — and it is a named term rather than an
	// omission so a future form that DOES draw something between the sides has a place
	// to say so.
	statusCachePairSepCells = 0
	// statusCacheCountUnitWidest is the WIDTH of the widest unit the formatter can print
	// — the single character it appends to a scaled value. Measured from the formatter's
	// own ladder (TestStatusCacheFieldWidthIsBounded walks every rung and its
	// boundaries), never asserted by eye: the ladder's units are "" (the whole numbers),
	// "k", "M" and "G", and a single ASCII letter is one cell.
	//
	// WHY A UNIT IS COUNTED AT ALL: the previous reservation named a STRING ("↑12k↓4k"),
	// and a string is one input's output — so a formatter that varies with its input's
	// magnitude was covered by the one sample it was measured on, and grew past it. The
	// reservation is now a statement about the FUNCTION: statusCountSideTextWidest bounds
	// the text, this bounds the unit, and statusCacheCountGlyphCells bounds the arrow.
	statusCacheCountUnitWidest = 1
	// statusCacheCountGlyphCells is one direction arrow's width in cells. The arrows are
	// ASCII-adjacent single-width characters (statusCacheReadGlyph's note), and the
	// constant exists so the reservation COUNTS them rather than assuming them.
	statusCacheCountGlyphCells = 1
	// statusCountDigitsBase is what the formatter prints below the first scale: an integer
	// under statusCountDigitsPerRung has at most this many digits. It is DERIVED from the
	// base rather than written as a 3 — the base is a power of ten
	// (statusCountCompactKilo = 10 × 10 × 10), so its digit count is the number of tens it
	// is built from, spelled out of the SAME constant the scale uses. Raising the base to
	// another power of ten is then an edit to statusCountCompactKilo that moves this bound
	// with it (Rule 13: the reservation is re-derived, never re-literalled).
	statusCountDigitsBase = statusCountKiloTens
	// statusCountKiloTens is statusCountCompactKilo's decade count (1000 → 3): the ONE
	// place the base's own scale is spelled as a count of tens.
	statusCountKiloTens = 3
	// statusCountDigitsPerRung is how many digits of a value fit on one rung before the
	// scale advances — the decimal base, and the same number statusCountCompactKilo is a
	// power of, so rung zero and rung one meet exactly. It is the ladder's own step, and
	// the formatter advances by nothing else.
	statusCountDigitsPerRung = statusCountCompactKilo
	// statusCountFractionDigits / statusCountFractionScale are the formatter's PRECISION:
	// ZERO decimals, i.e. the count rounded to the whole unit it is printed in.
	//
	// Zero is the precision the row's BUDGET buys, and it is a trade the epic's arithmetic
	// forces rather than a preference. A decimal costs the separator plus
	// statusCountFractionDigits on top of the whole part — FOUR cells in the row, because
	// the side appears twice beside the arrow — and the pair must fit the sub-cell the
	// budget has. What the reader LOSES is the tenth; what he KEEPS is the order of
	// magnitude, stated by the unit — and, decisively, BOTH SIDES WHOLE. A pair showing
	// "↑1.7M↓…" would be a decimal bought with the miss-billed half of the read-out,
	// which is the defect this file exists to close.
	//
	// They are named because the RESERVATION is derived from them (Rule 1/13): raising the
	// precision must widen statusCountSideTextWidest — and therefore statusCacheCol — in
	// the same change, or the formatter would outgrow the column again.
	statusCountFractionDigits = 0
	statusCountFractionScale  = 1
	// statusCountFractionSepCells is the width statusCountFractionSep occupies on a side:
	// ZERO at this precision, because no separator is drawn when there is no fraction. It
	// is a named term rather than an omission so that raising the precision has ONE place
	// to say what the mark costs (statusCountFractionDigits' note).
	statusCountFractionSepCells = 0
	// statusCountWholeDigitsWidest is the widest WHOLE PART a rung may print before the
	// formatter advances the scale. TWO, and it is not a taste: a side's text is this plus
	// the unit, the pair is that doubled plus the arrow, and the row's budget is what
	// fixes the ceiling. Two digits keeps the pair inside the cells the sub-cell has
	// (measured, "↑12k↓12k" is 8 against an 8-cell sub-cell), which is what makes this
	// change a REPAIR of the reservation rather than a re-partition of the budget.
	statusCountWholeDigitsWidest = 2
	// statusCountMaxDigits is the width of a whole part that can still carry a fraction: a
	// whole part with more digits than this is printed as the integer it rounds to, in the
	// unit that states the magnitude. Its job is to keep a side's TEXT inside
	// statusCountSideTextWidest — whole part, separator and fraction together — which is
	// why it is derived from the rung's ceiling rather than chosen independently of it.
	statusCountMaxDigits = statusCountWholeDigitsWidest
	// statusCountSideTextWidest is the widest TEXT a single side can reach — the number
	// the reservation's text term is stated against, and a max() of the formatter's TWO
	// classes because they are bounded by different things:
	//
	//	whole numbers   an integer below the first scale prints EXACTLY, with no unit and
	//	                no cap: its digits are statusCountDigitsBase ("999" is three).
	//	rung values     the value is scaled to the rung's unit and capped on the whole
	//	                part: statusCountWholeDigitsWidest plus the unit the class always
	//	                carries ("12k" is three — the same width as "999", a digit traded
	//	                for the unit that states the magnitude).
	//
	// Stating it as the max of two DERIVED terms rather than as a literal ties the
	// reservation to the precision policy and to the base at once (Rule 13): raising
	// statusCountFractionDigits raises the rung term, and changing the scale moves the
	// whole-number term, so a form that outgrows the reservation cannot be written
	// without this number moving too.
	statusCountSideTextWidest = max(statusCountDigitsBase,
		statusCountWholeDigitsWidest+statusCacheCountUnitWidest+
			statusCountFractionSepCells+statusCountFractionDigits)
	// statusCostCol does NOT exist. The money sub-cell it used to reserve was
	// REMOVED (task 01a0dcd1), and this note is the record of WHY, so that a later
	// reader does not re-add it:
	//
	//	· The amount was never MEASURED — the transports gopi speaks report token
	//	  volumes, never a price, so the number was DERIVED here: a function of
	//	  (price card generation × Beijing civil time × a scraped price page).
	//	  internal/pricing carries peak/off-peak regimes (dsFlashIdle 1/0.02/4 vs
	//	  dsFlashPeak 2/0.04/8, weekdays 09-12/14-18 Beijing, off-peak HALF the
	//	  peak) and dated per-generation cards (dsV4FlashOld/dsV4Pro are already
	//	  retired), and pricing.CostOf(model, hit, miss, out, at) takes a
	//	  TIMESTAMP to resolve the regime.
	//	· What reached the screen was therefore "~0.00元": a hedged, sub-precision
	//	  figure — zero information spending seven cells. The operator's verdict on
	//	  that cell is the design rule this row now follows: "You cannot just
	//	  display a '·~0.00元'. this is a fooling, and user is not a fool!" The row
	//	  shows facts it can measure directly.
	//	· internal/pricing is NOT deleted and its counters are NOT dropped: it has
	//	  live callers outside this row (cmd/agent/cli_chat.go's statusOpsFor prices
	//	  the session, and cli_stats.go's `gopi session stats --cost` is the surface where a
	//	  bill belongs, computed once, off the repaint path). What is retired is the
	//	  ROW's money CELL, not money. This is about the row.
	//	· The cells it freed are spent: the context pair's two cells (10 rather than
	//	  9) and the arithmetic's own slack (statusPinCols). Nothing is reserved and
	//	  left undrawn — the defect the retired cache column was.
	//
	// statusMetricsSep is the separator that joins the metrics GROUP's sub-cells
	// (context, cache). It is StatusSepGroup — lineedit's light, field-level
	// separator — rather than the heavier StatusColumnSep, because that pair IS
	// the distinction the row draws: a TOP-LEVEL column boundary is a column
	// boundary (4 cells), a boundary inside a column is a field boundary (2).
	// Spending the column separator inside the group would cost two cells the
	// budget does not have, and would make the group read as two columns
	// pretending to be one.
	//
	// Both the string and its CELL width are definitions in internal/lineedit
	// (Rule 14: a shared value has one definition, in the package that owns the
	// composition), and TestStatusMetricsGroupArithmetic ties the two together —
	// so an edit to either cannot leave this geometry summing a stale number.
	statusMetricsSep      = lineedit.StatusSepGroup
	statusMetricsSepCells = lineedit.StatusSepGroupCells
	// statusMetricsSubCells is how many read-outs the group joins internally
	// (context, cache). It is NAMED because statusMetricsCol's width is derived
	// from it — AND because it is what pins the derivation to the sub-cells the
	// row really draws (statusMetricsSubCellWidths), so a read-out added or
	// removed cannot leave this geometry summing a number the drawn row no longer
	// matches.
	statusMetricsSubCells = 2
	// statusMetricsCol is the group's reserved width: its sub-cells plus the
	// separators that join them, all from NAMED widths (a constant expression, so
	// the compile-time guard below really is a build-time check — a computed
	// value would silently move that check to run time). Rule 1: the n-1 seams
	// are derived from statusMetricsSubCells, never typed.
	//
	// The runtime list the group draws from (statusMetricsSubCellWidths) is tied
	// to BOTH numbers by TestStatusMetricsGroupArithmetic: its length must be
	// statusMetricsSubCells and its derived width must be this constant, so a
	// read-out added, removed or re-widened cannot leave the row summing a number
	// the drawn group no longer matches.
	//
	// Re-partition arithmetic, measured (tasks 01a0dcd1, 01a0ddab): ctx 13 (label
	// 3 + pair 10) + one field separator 2 + cache 7 = 22 reserved. The group's
	// CONTENT can spend one cell more than that when the context pair needs its
	// whole reservation, and that single cell is why the drawn pin
	// (statusPinCols, 95) is five cells under the budget (100) while the composed
	// row occupies exactly 100. Both numbers are stated rather than netted out,
	// because a geometry that hid the difference inside a total could not be
	// checked by hand.
	// RE-PARTITION (task 01a0f8c9, the ctx pair leads — the cell the rate is paid
	// with). The hit rate moved out of the context sub-cell (where it EVICTED the
	// pair) and onto the cache sub-cell's own value, beside the two counters it is
	// computed from — so the cache sub-cell's cell is now the pair PLUS the labelled
	// rate, and that cell has to come from somewhere. It comes from the context
	// sub-cell's pair reservation, WHICH WAS BOUNDED BY A FORMATTER THE ROW NO LONGER
	// HAS: statusCtxPairSideCellsPinned is 6 because the retired formatCount printed
	// "200.0k" (6 cells a side), while the ladder the row actually draws tops out at
	// statusCountSideTextWidest ("1M"/"92G", 3 cells) — so the sub-cell held six
	// cells nothing could ever draw in. The ctx sub-cell's reservation now follows the
	// LIVE formatter (12 = label 3 + gap 1 + pair 7 + qualifier 1), and the six cells
	// it stops claiming are the rate's home.
	//
	// The group's width is therefore UNCHANGED at 27 — no row cell was invented and no
	// other column gave one up: the same 27 cells are partitioned differently between
	// the two sub-cells, which is what keeps statusPinCols = statusLayoutBudgetCols =
	// 100 with statusPinSlackCells 0.
	//
	// The group's CONTENT at the pinned state is "ctx 13k/1M≡" + seam 2 +
	// "↑12k↓4k hit 74.5%" = 28, ONE cell past the reservation — the same overhang
	// statusMetricsCol carried before this task and the reason statusPinCols is the
	// drawn width rather than the reservation sum (statusColumnsAtCols' note).
	// statusMetricsFieldBudget is that form budget, named and asserted, so the
	// overhang is a stated number rather than an accident.
	statusMetricsCol = statusCtxCol + statusCacheCol +
		(statusMetricsSubCells-1)*statusMetricsSepCells
	// statusMetricsFormCol is the group's own drawn width at its FULL form — the
	// CONTENT the column can carry, as opposed to the width the column RESERVES.
	//
	// THE TWO DIFFER, and they always have: statusCacheCol is the cache PAIR's width
	// (the pair is what that sub-cell's reservation is sized for) while the value the
	// sub-cell DRAWS includes the hit rate riding it (statusCacheField), so the
	// group's full form is wider than the sum of its sub-cell reservations. Before
	// task 01a0f8c9 the same kind of gap existed in the other direction (the context
	// sub-cell drew one cell past its own reservation by yielding its label); the gap
	// is a property of a composition that must be bounded, not hidden — so it is a
	// NAMED term here, and the pin (statusPinCols) is the width the row really
	// OCCUPIES: the reservation sum plus this overhang.
	//
	// THE OVERHANG IS NOT SLACK. A geometry that reserved the full form would leave a
	// column reserving cells the drawn row does not occupy at the states where the
	// rate is absent (the retired-cache-column defect), so the row spends the
	// overhang on this one column, at a FIXED cell, by taking it from statusToolCol —
	// whose WAY-MARKER breadth was measured against the wider context pair this task
	// replaced (see statusToolColWayBreadth). The alternative reading — leave the
	// group's reservation alone and let the rate push the row past 100 — would wrap
	// the row on the terminal it is designed for, which the compile-time guard
	// refuses by construction.
	statusMetricsFormCol = statusMetricsCol + statusCacheHitRateCells
	// statusLiveCol reserves the live hint's right-hand block, so "ready" and
	// "thinking…" occupy the same cells (the hint's left edge is a column). It is
	// wider than the two state words alone because the session's elapsed time and
	// its compaction count ride in the same block, prefixed to the state (see
	// statusLiveField) — Hermes' `duration` segment, in the one place the row has
	// left: the tail.
	//
	// THE `/VOICE` FIELD RIDES THIS RESERVATION AND DID NOT WIDEN IT (task
	// 01a116b0). While the mode is on the block shows "<field> · <state word>"
	// instead of the counter — the field takes the counter's room by rank (see
	// statusLiveField's voiced branch) — and the field's own width was chosen so
	// that sentence fits HERE, in these 13 cells:
	//
	//	voiceChatLiveWordCells  3      the field ("rec" / "stt")
	//	lineedit.StatusSepCells 3      the separator between the field and the word
	//	the state word          4..11  ("sent" 4, "think"+gap 6, "resp"+gap 5,
	//	                               "responding"+gap 11)
	//	                        --
	//	                        10..17, and the block's composer picks the form that
	//	                        fits 13 — the SHORT spelling does it for the two long
	//	                        states, which is the mechanism the compaction counter
	//	                        already uses (statusLiveStateWords)
	//
	// THE RESERVATION MUST EQUAL THE WIDEST FORM THE BLOCK DRAWS, which is why the
	// field was sized DOWN rather than the block sized UP: a reservation wider than
	// the widest form it draws pulls the row's tail left while the mode is on and
	// pushes it back when the mode ends — the row visibly jumps, and the pin (and
	// therefore which terminal gets the aligned layout) would move with it.
	// TestStatusReservationsAreFullyDrawn measures the block's widest drawn form
	// against this number, and that measurement is what refused both a wider block
	// and the glyph-plus-word spelling this field started as (chat_voice.go's
	// vocabulary note records both arithmetic results).
	//
	// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): 13 is the value
	// this tree already reserved; what the group buys is the cell that PAYS for it.
	// The group joins its sub-cells on field separators instead of spending a
	// column separator per top-level column, and the elapsed label master's own
	// ladder test composes with ("1h02m · ready", 12 cells) fits the reservation
	// with a cell to spare. RE-PARTITION (task 01a0dcd1): the group now carries
	// TWO read-outs (context, cache) — the money sub-cell is gone — and this
	// reservation is unchanged, because it is sized for the HINT ("thinking…" plus
	// the clock), not for the group.
	// THE `/VOICE` FIELD RIDES THIS RESERVATION AND DID NOT WIDEN IT (task
	// 01a116b0). While the mode is on the block shows "<field> · <state word>"
	// instead of the counter — the field takes the counter's room by rank (see
	// statusLiveField's voiced branch) — and the field's own width was chosen so
	// that sentence fits HERE, in these 13 cells:
	//
	//	voiceChatLiveWordCells  3      the field ("rec" / "stt")
	//	lineedit.StatusSepCells 3      the separator between the field and the word
	//	the state word          4..11  ("sent" 4, "think"+gap 6, "resp"+gap 5,
	//	                               "responding"+gap 11)
	//	                        --
	//	                        10..17, and the block's composer picks the form that
	//	                        fits 13 — the SHORT spelling does it for the two long
	//	                        states, which is the mechanism the compaction counter
	//	                        already uses (statusLiveStateWords)
	//
	// THE RESERVATION MUST EQUAL THE WIDEST FORM THE BLOCK DRAWS, and that is why
	// the field was sized DOWN rather than the block sized UP: a reservation wider
	// than the widest form it draws pulls the row's tail left while the mode is on
	// and pushes it back when the mode ends — the row visibly jumps.
	// TestStatusReservationsAreFullyDrawn measures the block's widest drawn form
	// against this number, and that measurement is what refused both a wider block
	// and the glyph-plus-word spelling this field started as (chat_voice.go's
	// vocabulary note records the two arithmetic results).
	statusLiveCol = 13
	// statusColumnsAtCols is the row WIDTH the aligned layout is composed for:
	// the layout is a single fixed geometry (the sums of the reserved widths
	// above), so the fields' columns are the same cells on every terminal at
	// least this wide. It is also the pinned width a caller's column assertion
	// renders the row at (Status.Line(statusColumnsAtCols)) — one number, one
	// definition, so the assertion and the production row cannot drift.
	//
	// The separator term is measured in CELLS (lineedit.StatusColumnSepCells),
	// never with len(): StatusColumnSep carries a multi-byte "·", so len()
	// over-states it by one cell per separator. That is not a rounding detail —
	// measured, it put the pinned geometry three cells past a 100-column
	// terminal, and since the aligned form only runs on a terminal at least as
	// wide as the pin, the alignment silently never appeared on the width it was
	// designed for. StatusColumnSepCells is tied back to the strings by
	// TestStatusSeparatorCellWidths.
	//
	// The columns below are spelled as NAMED widths rather than as one sum, and
	// that is a deliberate structural change from the five-field row this
	// replaces. The counter fields whose width is proportional to the CONTEXT
	// WINDOW (the context column's absolute used/max pair, and the cache pair) are
	// the ones the reservation exists for, so they are stated as columns and the
	// compile-time budget below is what keeps the total inside
	// statusLayoutBudgetCols. Stating them as columns is what makes the
	// degradation order expressible: a form that sheds a field drops its column
	// AND its separator, which is a statement about the geometry rather than
	// about a string.
	//
	// It is the DRAWN pin (statusPinCols), which is the geometry's own sum for
	// every column except the metrics group: that group's content spends one cell
	// past its sub-cell reservations (statusMetricsCol), so the row's composed
	// width and the geometry's sum are one cell apart — and the row shown at this
	// width occupies EXACTLY statusLayoutBudgetCols cells. The alternative (pin
	// the geometry's sum, draw 101 cells) would wrap the row on the terminal it
	// is designed for, which costs the editing line a row.
	statusColumnsAtCols = statusPinCols
)

// statusColumnSepTotalN is how many separators join the columns named in
// statusColumnsAtCols: one between each pair. It is a named constant rather than
// an inline n-1 because a form which SHEDS a column also sheds its separator,
// and the degradation forms below do that arithmetic — a bare literal here would
// be a number no added column could update (Rule 1).
// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): the count is 4,
// not 5, and the COLUMN LIST is why — the top-level columns are model, iter, mode,
// tool and the metrics GROUP. Master carried ctx and cost as two top-level
// columns (5 separators); the group is ONE column carrying them plus cache, so the
// row pays 4. That saved separator is where the group's cells come from: the same
// five columns and the same 100-cell budget, with statusLiveCol at master's 13.
// RE-PARTITION (task 01a0dcd1, the money sub-cell's retirement): the count is
// unchanged at 4 — the group is still ONE column, and a sub-cell leaving it does
// not add or remove a top-level boundary. That is the whole point of a grouped
// column, stated as arithmetic.
//
// RE-PARTITION (operator bug #5, task 01a0e4c7): the count is 3, because the row
// carries FOUR columns — model, iter, tool and the metrics group. The retired
// mode column is absorbed into the tool column (see statusToolCol), so its
// top-level boundary goes with it: a row that no longer states the mode beside a
// tool state has no boundary to pay for. The pin does not move — the 4-cell
// separator's cost and the 5 retired cells both land inside the absorbed column,
// which is why statusToolCol is 21 and the arithmetic still sums to the budget.
const statusColumnSepTotalN = 3

// A geometry that cannot fit its own budget does not build (Rule 12: the compile
// error IS the instrument, and it is the one assertion no edit can leave stale).
//
// The guard IS the inequality it enforces, written as its arithmetic:
//
//	statusColumnsAtCols <= statusLayoutBudgetCols
//	  ⇔  statusLayoutBudgetCols - statusColumnsAtCols >= 0
//
// and the uint conversion is what turns the second form into a compile-time
// REFUSAL: converting a negative constant to uint is a compile error ("constant
// -1 overflows uint"), while a ZERO difference converts to a legal uint(0). So
// EQUALITY passes — and equality is the intended state of this geometry (the pin
// spends statusLayoutBudgetCols exactly, so pin == budget by construction) — and
// an overshoot of ONE cell does not.
//
// The difference carries NO offset term, and both halves of that are deliberate.
//
//  1. No offset is NEEDED. A previous revision added a named shift to the sum
//     first and "took it back out" again on a second `const _` line, on the
//     premise that the bare subtraction "also stops compiling when the two are
//     EQUAL". That premise is false: uint(0) is a legal constant, so equality
//     passes the bare form and the offset bought nothing it claimed to.
//  2. An offset is HARMFUL. Because the value the compiler actually converts was
//     the overshoot MINUS the shift, the guard refused only an overshoot larger
//     than the shift itself: with the shift at 2, a two-cell overshoot measured
//     `CGO_ENABLED=0 go build ./cmd/agent/` exit 0 — the geometry built green
//     while its own comment claimed a zero-slack boundary (Rule 13: a constant
//     that absorbs the violation is prose, not a gate). The single line below is
//     the whole guard, so there is no second arithmetic for the first to disagree
//     with.
//
// A compile-time bound is VERIFIED by compiling the violating value, never by
// reasoning about it.
//
// RE-PORT NOTE (task 01a0e394, bug F re-applied onto this master): the guard below
// is MASTER'S (commit 2268f52b) and this port KEEPS it verbatim. The archived bug F
// branch was cut before that commit and carried the shift form, so re-applying its
// diff whole would have silently reverted a landed fix — the shape Rule 23 warns
// about, caught here by `git diff master -- cmd/agent/chat_stream.go` showing the
// guard block as a whole-file minus. The two requirements meet at this line: the
// qualifier's cell must be paid INSIDE the ctx sub-cell, and "one cell of overshoot
// fails the build" is only checkable while the difference carries no offset.
const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)

// statusContextColumnCells is the cell count the row really OCCUPIES at the pin
// — the geometry's reservation summed for every column — MEASURED on the
// composed row this geometry produces (task 01a0ddab, on the operator's own
// numbers, ctx 13.1k of a 200.0k window):
//
//	deepseek-flash │ iter12 │ tools │ ⚙ keyword_search │ ctx 13.1k/200.0k≡ · ↑12k↓4k │ 4m00s · ready
//	 14              7         5       16                 27 (ctx 17 + 2 + cache 8)          13
//
// = 14+7+5+16+27 = 69 cells of content, 4 column separators (4 cells each = 16),
// plus the 2-cell gap before the live block = 100 cells, of which the live block
// gives its own trailing pad back on the right edge.
//
// (The row above is the pinned form AFTER bug F: the context sub-cell still
// occupies 17 cells (label 3 + gap 1 + pair 13) and now composes
// "ctx 13.1k/200.0k≡" — the qualifier is the cell the LABEL yields to it, so the
// group's 27 and the geometry's 100 are unchanged from the cache-pair
// correction. The cache sub-cell's 8, not the 7 this sample carried before that
// correction, is what the group's 27 counts.)
//
// It IS statusColumnsAtCols (asserted at compile time, below): the pinned width
// the row is composed FOR and the width the geometry sums to are one number, so
// the composition's own fit check (left + live + gap <= cols) cannot refuse the
// aligned form on the terminal the pin describes.
const statusContextColumnCells = statusColumnsAtCols

// statusPinSlackCells is what the budget reserves and the geometry does not
// spend: the headroom a future field would take (Rule 1 — a named constant, not
// a footnote). It is ZERO since task 01a0e2e9, and that is a fact about the
// geometry rather than a shortage: the pin is 100 and every one of those cells is
// DRAWN (the equality was reached by correcting a double-counted reservation —
// see statusCachePairSepCells — not by spending air). Zero is honest here and is
// NOT the retired-cache-column defect: that defect was a reservation with nothing
// in it, while this is an absence of reservation. It is stated as arithmetic so a
// geometry change moves it visibly.
const statusPinSlackCells = statusLayoutBudgetCols - statusColumnsAtCols

// statusMetricsSubCellWidths is the row's list of the metrics GROUP's sub-cell
// reservations, in the group's left-to-right order (context, then cache). It is
// the ONE place the group's content is enumerated, and it exists because the
// count and the sum must not be able to disagree: statusMetricsSubCells is
// defined as its length and statusMetricsCol as its derived width, so a read-out
// added to the group, removed from it, or re-widened moves BOTH with it — and the
// compile-time guard below then fails the BUILD if the row no longer fits.
//
// Removing the money sub-cell (task 01a0dcd1) is exactly the edit this shape is
// for: the read-out left this list, and the count, the column and the row width
// followed in the same change with no cell left reserved-but-undrawn — the defect
// the retired cache column was, in the opposite direction.
func statusMetricsSubCellWidths() []int {
	return []int{statusCtxCol, statusCacheCol}
}

// statusMetricsGroupWidth is the reserved width of a group of sub-cells joined by
// the LIGHT separator: the sub-cells plus one seam between each pair. It is a
// function rather than an inline sum so the count of seams cannot drift from the
// count of sub-cells (Rule 1: the n-1 is derived, never typed).
func statusMetricsGroupWidth(subCells []int) int {
	if len(subCells) == 0 {
		return 0
	}
	total := (len(subCells) - 1) * statusMetricsSepCells
	for _, w := range subCells {
		total += w
	}
	return total
}

// The banner's ONE line is built here from two halves, so the Ctrl+C sentence and
// the exit sentence cannot drift apart: chatBannerCtrlCTail states what the key
// does now (interrupt the turn) while chatBannerExit keeps what it has always
// done (leave the session altogether).
//
// THE SENTENCES ARE THE SINGLE SOURCE, AND THE STARTUP SCREEN READS THEM TOO
// (chat_banner.go prints exactly these two strings in its side panel). It matters
// for more than tidiness: testsuite/chat_pty_interrupt_test.go asserts on the
// banner's own fragments — "Ctrl+D or /quit to exit." WITH its period — so a panel
// that re-spelled the sentence one character differently would satisfy the eye and
// fail the contract. One text, one owner.
const (
	chatBannerCtrlCTail = "Ctrl+C interrupts the running turn"
	chatBannerExit      = "Ctrl+D or /quit to exit."
)

// chatBannerCtrlC is the interrupt half as the ONE-LINE banner states it: the
// product name and the surface, then the sentence itself.
const chatBannerCtrlC = chatProductName + " chat — " + chatBannerCtrlCTail

// chatBannerLine is the one line the REPL prints at startup.
const chatBannerLine = chatBannerCtrlC + ", " + chatBannerExit

// chatPrompt is the REPL's prompt. It is drawn by the line editor (on a
// terminal) and by the cooked reader's own echo-free path; it is written to
// stderr, where the rest of the chat chrome goes, so stdout carries only the
// model's answer.
const chatPrompt = "you> "

// chatStatusSource assembles the one-line status from the REPL's own counters.
//
// It is READ by the editor's read loop (through SetStatus, installed once before
// the loop starts) and WRITTEN by the REPL's own goroutine (the turn loop). The
// two goroutines are the whole point of the publish path: the writer holds the
// mutex for the duration of an update and then PUBLISHES (a flag + a wake, never
// a terminal write), so the read loop always composes a status line from one
// consistent set of counters — no torn field, and no second writer on the fd.
type chatStatusSource struct {
	// mu guards every field below. It is held around the EDITOR's composition
	// too (status() takes it), so a read never sees half an update.
	mu sync.Mutex

	model string
	// pal is the run's ONE emphasis table (resolved once by the caller and handed
	// along — cli_chat.go's chatPal). The source needs it because the row's
	// measured-quality tiers are a PRODUCER decision: which TIER the hit rate falls
	// in is measured here, and which HUE that tier is comes from this table. It is
	// never resolved inside the source (Rule 14: one capability decision per run, or
	// the downgrade notice prints twice into the terminal whose reader is reading
	// it).
	//
	// Its ZERO VALUE IS MonoPalette, so a source nobody handed a table to (every
	// test that composes a row directly) wears no tier and renders the bytes it
	// always did — the channel is inert until a caller resolves a table.
	pal lineedit.Palette
	// iter is the completed-turn count; live is the turn currently running
	// (0 = at the prompt). They are separate fields because "turn 3 running"
	// and "3 turns done" are different facts.
	iter   int
	live   int
	tokens int
	queued string
	// toolsOn is the TOOLS-ENABLED fact of the surface that owns this row: the
	// `gopi chat --tools` path sets it, the plain chat path leaves it false. It is
	// the row's ONE tool-state switch (operator bug #5): it used to be spelled as
	// the mode column's word "tools" AND the tool column's "no tool", i.e. the same
	// fact stated twice with opposite signs, and it is now a single field the tool
	// column's composer reads (see statusToolField).
	toolsOn bool
	// phase is the in-flight turn's live STATE (chatTurnPhase) and frame is the
	// keepalive's animation counter. Both are guarded by mu like every other
	// field, and both are meaningless at the prompt (phase zero, frame zero) —
	// which is what makes the idle value the state a reader sees when no turn is
	// running.
	phase chatTurnPhase
	frame int
	// viewport, when set, is asked for the terminal width at COMPOSE time —
	// the ONE hook that keeps the degradation ladder's input from going stale
	// (see setViewportFunc). Guarded by mu like every other field.
	viewportFn func() int
	// voice, when set, answers the LIVE PHASE of the `/voice` mode
	// (voiceChatLive, chat_voice.go) — the ONE read-out this row takes from a
	// feature that is not itself part of the row's counters.
	//
	// IT IS A SEAM, NOT A COPY OF THE MODE'S STATE (Rule 3: one state source).
	// The mode's phase lives on voiceChatMode's own mutex-guarded state
	// (chatVoiceState.live) and is read through this FUNCTION, so the row can
	// never hold a second, drifting answer to "is the microphone open?"; the
	// mode hands the source one closure at wiring time and the mode stays the
	// only owner of the fact. It is called from status(), i.e. from the editor's
	// read loop, and it is guaranteed to be cheap and non-blocking because the
	// reader is the same shape the mode uses for its own state() readers (a
	// mutex-held field read, never a callout).
	//
	// NIL MEANS "NO VOICE FEATURE ON THIS SURFACE" — the cooked path (which
	// builds no status row at all) and every direct-composition test. The zero
	// value therefore renders the row exactly as it was before this field
	// existed, which is the property that keeps the geometry change auditable.
	voice func() voiceChatLive
	// availCols is the terminal width the aligned layout may spend, as last
	// reported by the REPL's publish path (0 = not reported, and the full pinned
	// geometry is used). It exists because the DEGRADATION LADDER has to be driven
	// by the terminal the user is actually on: internal/lineedit refuses the
	// aligned form outright on a terminal narrower than the pinned layout
	// (alignedFits), so without this the row could only ever be all-or-nothing and
	// the ladder would be unreachable on a narrow screen.
	//
	// The value is a HINT and its staleness is safe: it decides only how MANY
	// columns the row asks for, and internal/lineedit then verifies the answer
	// against the real terminal width before drawing (and falls back to the
	// unaligned composition when the answer does not fit). A stale-WIDE hint
	// therefore costs the aligned form, never a wrapped row; a stale-NARROW one
	// costs a column until the next resize repaint. Every value written here comes
	// from the live band width in the same round that paints the row.
	availCols int
	// viewport, when set, is asked for the terminal width at COMPOSE time — the
	// ONE hook that keeps the degradation ladder's input from going stale (see
	// setViewportFunc). It is the LIVE REPL's input, because a width pushed by a
	// publisher can only reach the composer if that publisher is still called:
	// the branch wired it to publishChatStatus, which the live loop stopped
	// calling when chatTurnStatus took over the publish path, so the composer read
	// 0 on every terminal and the ladder below the pin was unreachable. Guarded by
	// mu like every other field, and CALLED outside the lock (the hook reaches
	// into the editor's own geometry).
	viewport func() int
	// MERGE NOTE (rework run 01a0db73, resolving 4c4b64a3 x master): the branch and
	// master each added field(s) here and BOTH are kept, re-expressed in the
	// survivor's vocabulary. The state word (sent / thinking / responding) is
	// master's: chatTurnPhase + frame are an in-flight turn's LIVE state and the
	// animation counter that makes each keepalive repaint a real one. The
	// branch's statusLiveField had collapsed that state into one word
	// (statusLiveThinking) because the branch's base predates master's phase
	// machine; keeping the branch's word would have DELETED master's three-state
	// read-out and the keepalive's animation, so master's state machine is what
	// survives and the branch's field READS it (see statusLiveField below).
	// availCols is the branch's own contribution, kept verbatim: it is what makes
	// the degradation ladder reachable on a narrow terminal.
	// toolName/toolSummary describe tool activity inside the running turn:
	// the tool currently executing (or last executed) and its one-line
	// digest. Both are cleared when the turn ends. Guarded by mu like every
	// other field — the status is composed from one consistent snapshot.
	toolName    string
	toolSummary string
	toolRunning bool

	// ops carries the OPERATIONAL read-outs — the numbers that describe what
	// the session has spent rather than which turn it is on. They are held as
	// ONE value so a publish installs a coherent set (a ctx read-out from one
	// turn beside a cache count from the next would be a torn snapshot of a
	// different kind), and so the zero value is the honest "nothing measured
	// yet" state every formatter below is written against.
	ops statusOps
	// opsFn, when set, supplies the operational read-outs afresh on every publish.
	// It is a FUNCTION rather than a value because the fields it reads are
	// CLOCK-driven (the session's elapsed time, the context size as a turn grows):
	// a value installed once would freeze them at the moment of construction.
	//
	// It is called from update() — i.e. from the REPL's own goroutine, the same
	// one that owns the agent's counters — and never from status(), which runs on
	// the editor's read loop and would race the turn's mutations. Like model and
	// toolsOn it is installed once, before the loop starts. Guarded by mu like
	// every other field.
	opsFn func() statusOps

	// cacheTurnRead / cacheTurnWrite are THIS TURN's baseline: the two cache
	// counters as they stood when the turn's request was issued. They live here,
	// on the source, rather than on statusOps because statusOps is REBUILT from
	// the runtime on every publish (opsFn) while a baseline must survive a
	// repaint — a snapshot stored on the freshly-built struct would be re-taken
	// (against the current counters, i.e. always zero) or lost at every frame.
	//
	// The row's second reading is the difference against them
	// (statusCacheTurnHitRate), and cacheTurnKnown is its honesty flag: false
	// before the first turn's request, i.e. while there is no baseline to
	// subtract — the row then prints the cumulative reading alone (Rule 15b).
	cacheTurnRead  int
	cacheTurnWrite int
	// cacheTurnPrompt is the SAME-INSTANT third reading (the prompt-side total)
	// taken in the very same snapshot call as the two above. It is what makes the
	// turn's reading a SAME-口径 measurement instead of the difference of two
	// counters that may have landed at different instants (statusCacheTurnRatio
	// states the defect and the arithmetic).
	cacheTurnPrompt int
	cacheTurnKnown  bool
}

// statusOps is the operational half of the status row: every field here is a
// counter the runtime really maintains, together with the flag that says whether
// it was ever MEASURED. The flags are not decoration — they are what lets a
// formatter report "not measured" instead of a fabricated zero (the epic's
// honesty clause): a zero token count and an unmeasured one are different facts,
// and the row says which it has.
type statusOps struct {
	// ctxUsed / ctxMax are the context's estimated token count and the window it
	// is spent from. ctxMeasured reports whether ctxUsed came from a real
	// measurement (the last API response's prompt tokens) rather than the local
	// estimate — and the row MARKS the estimated case: the context sub-cell's
	// qualifier spells statusCtxQuantGlyph ("≡") when this flag is true and
	// statusCtxEstimateGlyph ("≈") when it is false, so a reader can always tell
	// which of the two numbers he is looking at (statusCtxQuantGlyphFor is the one
	// decision; the promise this field's comment made for a long time was kept by
	// no renderer until that glyph existed). The mark costs NO cell — it replaces
	// the qualifier rather than joining it — because the sub-cell renders 12 of its
	// 12 cells in the pinned form. ctxMax == 0 means the window is unknown, and
	// then the field reports the token total alone.
	ctxUsed     int
	ctxMax      int
	ctxMeasured bool

	// cacheRead / cacheWrite are the tokens served from the prompt cache and the
	// tokens written into it. The two counters are genuinely different on this
	// transport — a read is a cache hit, a write is the miss that creates the
	// entry, and they are priced ~30x apart (internal/pricing) — so they are kept
	// apart here rather than summed. They reach the screen as the metrics group's
	// second sub-cell (statusCacheField) — a top-level column of their own was
	// RETIRED as unaffordable (see the RETIRED note on statusCacheCol).
	cacheRead  int
	cacheWrite int
	// cachePrompt is the PROMPT-SIDE TOTAL the same counters sum to
	// (input + cache_creation + cache_read, deepseek.ClaudeUsage.PromptSideTokens),
	// and it is the third field of the row's turn reading. It exists because the
	// cache pair alone cannot answer "what did this turn send" at ONE instant: the
	// prompt side is what the transport reports per response as a whole, so it is
	// the quantity both the numerator and the denominator of Δ are derived from
	// (statusCacheTurnRatio). 0 means "not measured here" — a statusOps built by a
	// caller that does not set it — and the turn reading then falls back to the
	// miss counter's own delta, which is a NAMED degradation, never a silent one.
	cachePrompt int

	// cacheReadAtTurn / cacheWriteAtTurn are the SAME two counters as they stood
	// when the CURRENT turn's request was ISSUED, and cacheTurnKnown says whether
	// such a snapshot was ever taken. They are the denominator of the row's
	// SECOND reading — this turn's own rate, cacheRead-cacheReadAtTurn over
	// (cacheRead-cacheReadAtTurn)+(cacheWrite-cacheWriteAtTurn) — which is the
	// difference the cumulative pair above cannot state: a session that has served
	// 98% of its prompt tokens says nothing about whether THIS turn was served or
	// written, and the turn is what the operator is waiting on.
	//
	// THE SNAPSHOT IS TAKEN AT THE TURN BOUNDARY, never here (chatStatusSource.
	// beginTurn): this struct is rebuilt from the runtime's counters on every
	// publish (statusOpsFor), so a baseline stored in it could not survive a
	// repaint. cacheTurnKnown is the honesty flag (Rule 15b): with no snapshot
	// there is no delta, and the row prints the cumulative reading alone rather
	// than a difference against a baseline nobody took.
	cacheReadAtTurn  int
	cacheWriteAtTurn int
	// cachePromptAtTurn is the THIRD reading of the same snapshot instant: the
	// prompt-side total as it stood when the turn's request was issued. It is
	// carried beside the two counters above because the turn's rate is taken over
	// the prompt side itself, not over a sum of two counters whose updates can
	// land at different instants (statusCacheTurnRatio states why).
	cachePromptAtTurn int
	cacheTurnKnown    bool

	// There is NO money field here, and its absence is deliberate (task
	// 01a0dcd1): the amount was never measured — it was DERIVED from a price
	// card, a Beijing civil-time regime and a scraped price page — and the row
	// carries only facts it can measure directly. See the RETIRED note on
	// statusCostCol for the full reasoning, and note that internal/pricing and
	// its counters are NOT gone: `gopi session stats --cost` is where a bill belongs
	// (computed once, off the repaint path).
	//
	// The cache pair IS carried: it is reported by the transport itself
	// (deepseek.CostTracker's own counters), which is exactly the class of
	// read-out that stayed.
	// elapsed is the session's wall time, and elapsedKnown says whether the
	// source was given a clock (the cooked/test path may not have one). A row
	// that has no clock says nothing about time rather than printing 00:00.
	elapsed      time.Duration
	elapsedKnown bool

	// compressions is how many context compactions this session has performed,
	// and compressionsKnown whether the count is available. Hermes shows it only
	// when non-zero; so does this row, which is also why it has no column of its
	// own (see the degradation order).
	compressions      int
	compressionsKnown bool
}

// newChatStatusSource builds the source with the fields that never change after
// construction (the model name and whether this surface has tools at all).
func newChatStatusSource(model string, toolsOn bool) *chatStatusSource {
	return &chatStatusSource{model: model, toolsOn: toolsOn}
}

// setPalette hands the source the run's ONE emphasis table, so the row's
// measured-quality tier can be resolved where the measurement is (see the pal
// field). It is called by the REPL with the table it already resolved for the
// banner and the editor — never resolved a second time here.
func (s *chatStatusSource) setPalette(pal lineedit.Palette) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pal = pal
}

// update replaces the mutable counters under one lock. It is the ONLY way the
// REPL's goroutine touches the source — no field is assigned directly outside
// this function, so an update can never be half-applied.
func (s *chatStatusSource) update(iter, live, tokens int, queued string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iter, s.live, s.tokens, s.queued = iter, live, tokens, queued
	s.refreshOpsLocked()
}

// refreshOps re-reads the operational read-outs (the agent's own counters) and
// leaves the viewport as it is. It is the entry point for a call site that only
// wants the counters refreshed around a REPAINT.
func (s *chatStatusSource) refreshOps() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshOpsLocked()
}

// setViewportFunc installs the ONE hook the composer asks for the terminal width
// the aligned layout may spend. It is read at COMPOSE time rather than pushed by
// a publisher, and that is the whole point (defect fix, task 01a0dbc7): the
// ladder's form is a function of the width the row is being drawn at, so asking
// the width where the row is composed makes the answer impossible to be stale.
//
// The defect it closes: the width used to reach this source ONLY through
// publishChatStatus -> setAvailCols, and when the live loop's publish path moved
// to the chatTurnStatus tracker that helper stopped being called at all. The
// composer therefore read availCols == 0 on every terminal, fell back to the
// pinned geometry, and the degradation ladder below the pin was unreachable —
// measured on the real pty at 100/95/80 columns (availCols=0 in all three, and
// below the pin no shed form survived the aligned-fit check).
//
// A nil function (the zero value) leaves the stored availCols in force, which is
// what the cooked path and the direct-composition tests want.
func (s *chatStatusSource) setViewportFunc(fn func() int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewport = fn
}

// refreshOpsLocked re-reads the operational read-outs from their source. The
// caller holds mu, and the call happens on the REPL's goroutine (via update), so
// the agent's counters are read from the one goroutine that mutates them — the
// editor's read loop only ever reads the SNAPSHOT this leaves behind.
func (s *chatStatusSource) refreshOpsLocked() {
	if s.opsFn == nil {
		return
	}
	// The source function is called OUTSIDE the lock's protection of the agent,
	// but it runs on the owning goroutine either way; holding mu here only means
	// a concurrent status() waits for the snapshot rather than seeing a torn one.
	s.ops = s.opsFn()
	// THE TURN BASELINE IS MERGED INTO THE SNAPSHOT HERE, and this is the ONE place
	// it is: statusOps is REBUILT from the runtime on every publish (opsFn), so a
	// snapshot stored on it would be re-taken every frame (a delta of zero by
	// construction). The baseline lives on the SOURCE — taken once per turn
	// (beginTurn → snapshotTurnCountersLocked) — and is copied onto each fresh
	// snapshot, so the composer sees one coherent value (Rule 15: the reading and
	// the baseline it subtracts cannot come from two different moments).
	s.ops.cacheReadAtTurn, s.ops.cacheWriteAtTurn = s.cacheTurnRead, s.cacheTurnWrite
	// The prompt-side baseline rides the same merge: one snapshot, three fields —
	// the row's Δ is derived from them together (statusCacheTurnRatio).
	s.ops.cachePromptAtTurn = s.cacheTurnPrompt
	s.ops.cacheTurnKnown = s.cacheTurnKnown
}

// setOpsFn installs the operational read-out source. It is called ONCE, from the
// REPL's own goroutine, before the editor's read loop starts — the same
// discipline the model name and the mode hint follow.
func (s *chatStatusSource) setOpsFn(fn func() statusOps) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opsFn = fn
	if fn != nil {
		// Through refreshOpsLocked rather than fn() directly: the snapshot it leaves
		// behind must carry the turn baseline like every other one (see that
		// function's note), or the row's first paint would compose the delta against
		// a zero baseline it invented.
		s.refreshOpsLocked()
	}
}

// setAvailCols records the terminal width the aligned layout may spend, so the
// (legacy name kept for the cooked/test path: updateViewport is the live REPL's
// entry point and the ONE writer there).
// degradation ladder composes for the terminal the user is on rather than for the
// pinned geometry alone (see availCols). A non-positive width is "not reported"
// and restores the full pinned form.
//
// It is the ONLY writer of availCols, and it is a separate call from update so
// the two facts it carries — the counters and the viewport — can be refreshed
// from their own moments without either overwriting the other.
func (s *chatStatusSource) setAvailCols(cols int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols < 0 {
		cols = 0
	}
	s.availCols = cols
}

// setVoiceFn installs the read-out of the `/voice` mode's live phase — the ONE
// hook the row asks that question through (see the voice field). It is called
// ONCE, from the REPL's own goroutine, before the editor's read loop starts, and
// the mode it closes over is the SESSION's mode (built once in cli_chat.go), so
// the answer follows the mode for the whole session rather than for the line that
// started it.
//
// A nil function restores the "no voice feature here" state, which is what every
// caller that composes a row directly already has.
func (s *chatStatusSource) setVoiceFn(fn func() voiceChatLive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voice = fn
}

// setLive drops the in-flight marker without touching the completed counters
// (the prompt has come back).
func (s *chatStatusSource) setLive(live int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live = live
}

// setTool records that a tool call of name has started executing, with its
// one-line argument digest. The status reads "⚙ name summary" while it runs.
//
// It re-reads the operational snapshot too: the callback runs INSIDE ChatOps — on
// the REPL's own goroutine, the one that owns the agent's counters — and a tool
// starting is the first moment the row has something to say about what the session
// is spending. Refreshing only at the publish is what let a live turn carry a
// frozen snapshot (see chatTurnStatus.begin).
func (s *chatStatusSource) setTool(name, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolName, s.toolSummary, s.toolRunning = name, summary, true
	s.refreshOpsLocked()
}

// setToolDone marks the last tool as finished, keeping its name/summary on the
// row so the completed activity stays visible until the next event replaces it,
// and re-reads the operational snapshot for the same reason setTool does.
func (s *chatStatusSource) setToolDone(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolName == "" {
		s.toolName = name
	}
	s.toolRunning = false
	s.refreshOpsLocked()
}

// clearTool drops the tool row (called when a turn ends and the prompt returns,
// so a stale "⚙ bash" never outlives the activity it described).
func (s *chatStatusSource) clearTool() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolName, s.toolSummary, s.toolRunning = "", "", false
}

// liveHintText composes the live field's text for the current state: the state
// word at the prompt, and — while a turn is in flight — the state word plus the
// keepalive's animation frame, so a frame is a NEW row (the renderer deduplicates
// an unchanged one, so an animation that did not move would be a wake that draws
// nothing). Caller holds mu.
func (s *chatStatusSource) liveHintText() string {
	if s.live <= 0 {
		return statusLiveIdle
	}
	word := statusLiveWordSent
	switch s.phase {
	case chatTurnThinking:
		word = statusLiveWordThinking
	case chatTurnResponding:
		word = statusLiveWordResponding
	}
	dots := s.frame % liveHintFrameCount
	if dots == 0 {
		// The first frame carries no dots (i % 3 == 0 is frames 0, 3, 6 …):
		// a phase's FIRST paint is the bare word, so "the row says sent" is what
		// the operator reads at the moment of the phase change, and the animation
		// only starts moving on the keepalive behind it.
		return word
	}
	return word + strings.Repeat(statusLiveAnimationChar, dots)
}

// liveHintFrameCount is the number of frames the animation cycles through,
// derived from the frames constant so the two cannot disagree.
const liveHintFrameCount = statusLiveAnimationFrames

// beginTurn marks turn as the turn in flight: the phase is sent (the request has
// been issued and nothing has come back yet) and the animation starts over, so
// the first frame drawn for this turn is a phase's bare word.
func (s *chatStatusSource) beginTurn(turn int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live, s.phase, s.frame = turn, chatTurnSent, 0
	// THIS TURN'S CACHE BASELINE is taken HERE, and here alone (task 01a10f4e): the
	// row's second hit-rate reading is the DIFFERENCE of the cache counters since
	// the turn's request was issued, so the snapshot must be taken at exactly the
	// moment a turn starts and nowhere else. Taking it at every publish would make
	// it the current counters (a delta of zero); taking it at the turn's END would
	// never see the turn's own traffic (the transport's counters arrive with the
	// response, i.e. before the end).
	//
	// snapshotTurnCounters reads them from the SAME source the row composes from
	// (opsFn → statusOpsFor), so the baseline and the reading cannot come from two
	// different views of the counters.
	s.snapshotTurnCountersLocked()
}

// snapshotTurnCountersLocked records this turn's cache baseline. The caller holds
// mu. It is a method rather than inline code in beginTurn because a test that wants
// to drive a turn must be able to take a baseline the same way the REPL does (Rule
// 31: the surface under test is the one that runs).
//
// A source with no opsFn (the cooked path, a direct-composition test) takes no
// baseline: cacheTurnKnown stays false and the row prints the cumulative reading
// alone, which is the honest state — there are no counters to build a delta from.
func (s *chatStatusSource) snapshotTurnCountersLocked() {
	if s.opsFn == nil {
		s.cacheTurnKnown = false
		return
	}
	// ONE call, THREE fields: the read side, the miss side and the prompt-side
	// total are read from the same snapshot value, so no baselines can come from
	// two different instants (statusCacheTurnRatio is the reader that depends on
	// this being one instant, and Rule 15: a reading and the baseline it subtracts
	// cannot come from two different moments).
	ops := s.opsFn()
	s.cacheTurnRead, s.cacheTurnWrite = ops.cacheRead, ops.cacheWrite
	s.cacheTurnPrompt = ops.cachePrompt
	s.cacheTurnKnown = true
}

// setTools installs the surface's tools-enabled fact. It is the ONE writer of
// that field after construction: the `/tools` toggle calls it so the row follows
// the runner, and it takes the lock like every other mutation.
func (s *chatStatusSource) setTools(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolsOn = on
}

// setPhase moves the in-flight turn to phase. It is a no-op at the prompt (no
// turn is in flight, so there is no phase to move) — which is what keeps a late
// callback from an ended turn from resurrecting a phase.
func (s *chatStatusSource) setPhase(phase chatTurnPhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live > 0 {
		s.phase = phase
	}
}

// endTurn clears the in-flight state: the live count drops to the prompt's and
// the phase and animation reset with it, so the row composes the idle value and
// nothing of the turn's survives into the next one.
func (s *chatStatusSource) endTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live, s.phase, s.frame = statusLiveIdleTurn, chatTurnIdle, 0
}

// advanceFrame moves the keepalive's animation on by one frame and reports
// whether the turn was still in flight — the tick's own decision to keep going.
func (s *chatStatusSource) advanceFrame() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live <= 0 {
		return false
	}
	s.frame++
	return true
}

// status renders the source as the single status line. It is called by the
// editor's read loop, from inside a repaint.
func (s *chatStatusSource) status() lineedit.Status {
	s.mu.Lock()
	model, iter, live, tokens, queued, toolsOn := s.model, s.iter, s.live, s.tokens, s.queued, s.toolsOn
	toolName, toolSummary := s.toolName, s.toolSummary
	// The live hint is composed INSIDE the lock, together with the counters it
	// belongs to: the state word and the animation frame are two fields of one
	// fact, and reading them against a later `live` would let a repaint show the
	// previous state's animation under the next state's word.
	liveHint := s.liveHintText()
	ops := s.ops
	// The voice read-out is read UNDER the lock like every other snapshot field
	// (it is a field, not a call — the call happens below, outside, for the same
	// reason the viewport hook's does).
	voiceFn := s.voice
	availCols := s.availCols
	// The emphasis table is read UNDER the lock with the other snapshot fields: the
	// tier the row wears is chosen from THIS table and THIS ops reading, and a pair
	// read across a repaint boundary would let one repaint's measurement wear the
	// next table's hue.
	pal := s.pal
	// The viewport hook is read UNDER the lock like every other field ...
	viewport := s.viewport
	// MERGE NOTE (rework run 01a0db73): the branch composed the live hint OUTSIDE
	// the lock from a single 'thinking' word; master composes it INSIDE the lock
	// from phase + frame (two fields of one fact). Master's composition WINS — the
	// lock discipline is the reason (the word read against a later `live` would
	// show the previous state's animation under the next state's word) — and
	// master's three-state word is the information, not the lock. The branch's
	// ops and availCols reads are additional snapshot fields and are kept.
	s.mu.Unlock()

	// ... and CALLED outside it: the hook reaches into the editor's own geometry
	// (its region), and holding this lock across that call would make the
	// composer's lock order depend on a second component. The stored availCols
	// stays the fallback for a source with no hook (see setViewportFunc).
	if viewport != nil {
		if c := viewport(); c > 0 {
			availCols = c
		}
	}

	// THE VOICE MODE'S PHASE, asked through its seam and composed by the MODE's
	// own composer (voiceChatIndicatorFor) — the status code names no voice word
	// (Rule 1/13: the vocabulary lives in chat_voice.go, beside voiceChatUsage).
	// The read-out is taken here, outside the lock, for the same reason the
	// viewport hook is: the mode holds its OWN mutex for the field read, and
	// holding this source's lock across that call would make the composer's lock
	// order depend on a second component. A nil seam is "no voice feature on this
	// surface" and yields the idle phase, which composes to nothing.
	voiceLive := voiceChatLiveIdle
	if voiceFn != nil {
		voiceLive = voiceFn()
	}

	// The fields are assembled at their RESERVED widths (one definition per
	// field, above): the row's columns then hold still while the values in them
	// change, which is the whole point of the aligned layout.
	// The LAYOUT pin (Rule 1 / the epic's "columns hold still" clause): the
	// composition happens at a FIXED width, not at the terminal's current width,
	// so a field's START CELL really is a constant and the row can only move when
	// the terminal itself resizes — or when a degradation step crosses. That is
	// the property the caller's COLUMN pin asserts, and the property that makes
	// the row stable while the agent works.
	//
	// The width the ladder composes for is the terminal as last REPORTED
	// (availCols), and the pin stays the full geometry when nothing was reported
	// (the cooked path, a test composing the row directly). Using the reported
	// width rather than the full pin is what makes the ladder reachable: the row
	// asks for as much as the terminal can hold, and internal/lineedit verifies
	// the answer against the real width before drawing it.
	layoutCols := statusColumnsAtCols
	if availCols > 0 && availCols < layoutCols {
		layoutCols = availCols
	}
	// The column LIST is a constant of the layout, not a function of the
	// values: every field the geometry reserves gets its entry on EVERY
	// repaint, and an absent field is an empty Text in its reserved width
	// (statusLineColumns renders it as padding). Building the list conditionally
	// was measured to defeat the alignment entirely — the token field appearing
	// at a turn's end added a column, which pushed the row past its own pinned
	// width and flipped the whole row to the unaligned composition mid-session.
	// A column the row has no value for is still a column.
	//
	// ALL SIX columns are named once, in the row's left-to-right order, by
	// statusRowColumns — the ONE list the ladder below resolves its names
	// against. That is what makes the degradation order a property of the
	// geometry rather than of a second, hand-kept copy of it: a form which sheds
	// a column drops it from THIS list, and the separators fall out of len() — so
	// the arithmetic cannot disagree with the columns actually drawn. The list is
	// built by a function rather than inline so that a TEST can resolve the
	// ladder's names against the very geometry the row is composed from — the
	// guard that catches a name with no column behind it (Rule 31's point: a
	// guard asserting on a fixture would pass while the row drifted).
	full := statusRowColumns()
	full[statusColModel].Text = modelShort(model)
	full[statusColTurn].Text = turnLabel(iter, live)
	full[statusColTool].Text = statusToolColumnFieldFor(toolName, toolSummary, toolsOn)
	// The group's three read-outs are rendered by statusMetricsField into ONE
	// cell of the list: the row carries one operational column, and the ladder
	// sheds it as one step.
	full[statusColMetrics].Text = statusMetricsField(ops, tokens)
	// THE COLUMN'S OWN MEASURED-QUALITY SPAN (the prompt cache's hit rate): the
	// producer names WHICH cells of the field it just composed are a measurement,
	// and the tier that measurement falls in — resolved against the SAME table the
	// run decided once. Both facts ride the COLUMN, never the field's text matched
	// afterwards by the RENDERER: internal/lineedit wraps a field by its identity
	// (the label, or tool activity), so an emphasis chosen from a VALUE has no other
	// way to reach the screen, and the renderer must never parse a row it did not
	// compose (memory #5707).
	//
	// The span is taken from THIS column's own text (statusHitRateSpan), so the
	// two halves are one composition: a column that does not carry the reading —
	// unmeasured cache, or a geometry that shed the sub-cell — wears no emphasis at
	// all rather than an emphasis with no span (Rule 15: an emphasis nobody can see
	// is the silent non-effect this whole channel exists to remove).
	full[statusColMetrics].EmphasisSpan, full[statusColMetrics].Emphasis =
		statusHitRateSpan(ops, pal, full[statusColMetrics].Text)
	// The ALIGNED path wears BOTH readings, each from its own tier
	// (statusHitRateSpans): the row now carries two measurements in this column —
	// the session's cumulative rate and this turn's — and one span slot could
	// colour only one of them. The list is the producer's answer to "which cells of
	// this field are measurements", asked once, for both.
	full[statusColMetrics].EmphasisSpans =
		statusHitRateSpans(ops, pal, full[statusColMetrics].Text)
	// The UNALIGNED label stays the historic concatenation (model · iter · mode
	// · tool · ctx): it is the form a narrow terminal falls back to, and its job
	// is to keep the fields that carry the most meaning in the fewest cells. It
	// deliberately does NOT mirror the aligned columns — the two have different
	// budgets and different jobs (see statusLine's fallback).
	//
	// RE-PARTITION (task 01a0dcd1): the tail of the label is the CONTEXT read-out
	// rather than the historic bare token total. The operator's criterion — the
	// percentage is meaningless without its denominator, and a read-out must
	// survive a model/window change — cannot be met on the fallback by a bare
	// count, which is what the old tail showed (and it was the same quantity at a
	// different moment, so nothing is lost by showing it measured against its
	// window). statusCtxBase composes it from the SAME pair the aligned sub-cell
	// uses, so the two forms cannot disagree — and where there is no window the
	// pair prints the total alone, which is exactly the old behaviour.
	label := modelShort(model) + lineedit.StatusSep + turnLabel(iter, live)
	// task 01a0ddab: the FALLBACK keeps its ratio — statusCtxBase — and does NOT
	// mirror the aligned sub-cell's shape (label + pair, no percentage). The two
	// have different budgets and different jobs: the label costs a cell and the
	// fallback is the form that has no cells to spend, while the ratio costs no
	// words and states the one thing a narrow row cannot otherwise show (how much
	// of the window is spent). The sub-cell's own note (statusCtxCol's FOLD) is
	// about the ALIGNED row, where both operands of that ratio are already drawn.
	ctxField := statusCtxBase(ops, tokens)
	if ctxField != "" {
		// THE PROVENANCE MARK, on the fallback too (statusCtxEstimateGlyph). The
		// aligned sub-cell spends its qualifier cell on this distinction, and the
		// fallback has no reserved cells at all — so it appends the SAME one-cell
		// spelling after the reading rather than inventing a second vocabulary. A
		// bare "" (nothing measured) carries no mark: there is no number to grade,
		// and a glyph beside no number would be a claim about nothing.
		ctxField += statusCtxQuantGlyphFor(ops)
	}
	if tokens >= statusTokensHeavy {
		// The heavy suffix is the historic token field's warn, and it is the
		// ONLY place a heavy context is marked now: the aligned sub-cell's own
		// glyph was RETIRED (bug F — see the note on statusCtxHeavyMarker).
		// The fallback keeps the word because it has no reserved cells to
		// spend and no denominator to qualify: it prints a token total, so a
		// word stating the judgement is the honest form (see the note on
		// statusTokensHeavySuffix).
		ctxField += statusTokensHeavySuffix
	}
	if field := statusToolField(toolName, toolSummary, toolsOn); field != "" {
		label += lineedit.StatusSep + field
	}
	if ctxField != "" {
		label += lineedit.StatusSep + ctxField
	}
	// MERGE NOTE (rework run 01a0db73): the branch had already folded the queued
	// text INTO statusLiveField (live, queued, ops); master appended it here. Both
	// are kept by ONE path: the call below passes `queued` to the branch's field,
	// so appending it here as well would print the queued message twice. Nothing
	// master-side is dropped — publishChatStatus still calls update(..., "").
	columns := statusColumnsForWidth(layoutCols, full)

	liveHint = statusLiveFieldVoice(liveHint, queued, ops, voiceLive)
	// Thinking is carried as a FACT, not inferred from the hint's wording: the
	// status row's emphasis for "working" is chosen by the source that knows a
	// turn is running, so a reworded hint cannot silently lose it (and the
	// palette never has to match on a string). It rides the SAME Status value as
	// the aligned column layout, which is where the two intents join: which state
	// the row is in is the source's fact (Thinking), HOW the row is laid out is
	// the source's geometry (Columns/LiveWidth/LayoutCols, composed by
	// lineedit.statusLineColumns).
	// THE UNALIGNED LABEL'S OWN MEASURED-QUALITY SPAN. The aligned row carries the
	// emphasis on the COLUMN (StatusColumn.Emphasis); the unaligned label carries it
	// on the label, and both ask the SAME producer function the same question — so
	// the two paths cannot grade the same number differently. The span is whatever
	// the MEASUREMENT looks like in the label this call actually composed, and the
	// label's tail is currently the context pair rather than the hit rate (the
	// fallback keeps its ratio — see the note where ctxField is composed), so the
	// span is ordinarily empty here and the label is styled exactly as before. It is
	// NOT an assumption about the label's text: a label carrying the reading wears
	// its tier, and one that does not wears nothing (an emphasis with no span is the
	// silent non-effect this channel exists to remove).
	labelSpan, labelStyle := statusHitRateSpan(ops, pal, label)
	return lineedit.Status{
		Label: label, Columns: columns, Live: liveHint, LiveWidth: statusLiveCol,
		LayoutCols: layoutCols, Thinking: live > 0,
		LabelEmphasis: labelSpan, Emphasis: labelStyle,
	}
}

// ── the in-flight turn's state machine ──────────────────────────────────────

// chatTurnPhase is the in-flight turn's state. The zero value means "no turn is
// running", which is the SAME condition the source's live counter carries (0) —
// so a phase and a live count cannot disagree about whether a turn is in flight.
type chatTurnPhase int

const (
	// chatTurnIdle is the prompt: the live hint is the idle value.
	chatTurnIdle chatTurnPhase = iota
	// chatTurnSent is the request ISSUED and no token received yet — the wait the
	// operator described as having no assurance at all.
	chatTurnSent
	// chatTurnThinking is the model's reasoning arriving.
	chatTurnThinking
	// chatTurnResponding is the answer's text arriving.
	chatTurnResponding
)

// chatTurnStatus owns the turn's state AND the keepalive that repaints it.
//
// It exists because "the row is painted once at turn start" is the defect: a
// state that is painted only when it changes sits on one value for the whole
// wait, and one lost wake leaves it on the PREVIOUS one. Here the state is a
// fact (phase) and the assurance is the cadence — a ticker repaints the row every
// statusLiveKeepalive while a turn is in flight, and the animation frame it
// advances is what makes each of those repaints a REAL one (an unchanged row is
// deduplicated by the renderer, so a keepalive that did not move would be a wake
// that draws nothing).
//
// Every write is a PUBLISH through the editor's own seam. Nothing here touches
// the terminal: the read loop is the only writer, which is the invariant
// TestChatTurnPhasesWriteNothingFromTheCaller pins.
type chatTurnStatus struct {
	// source is the counters the row is composed from (model, iter, tokens) plus
	// the phase/frame this type drives.
	source *chatStatusSource
	// notify announces a change through the input layer (a flag + a wake, never
	// a terminal write).
	notify func()
	// liveTick is the keepalive period. It is a FIELD so a test can drive the
	// cadence at its own scale instead of sleeping the production period; the
	// zero value means the production period, so a caller that does not set it
	// gets the shipped policy.
	liveTick time.Duration
	// stop ends the keepalive goroutine. Closed by end() and never reused: a turn
	// owns one tracker call at a time, and a stopped ticker's loop returns.
	stop chan struct{}
	// done is closed when the keepalive goroutine has returned, so end() can wait
	// for it: a ticker still running after the turn would be the "stale working
	// indicator" the fix must not introduce.
	done chan struct{}
}

// newChatTurnStatus builds the tracker over the counters and the publish seam.
func newChatTurnStatus(source *chatStatusSource, notify func()) *chatTurnStatus {
	return &chatTurnStatus{source: source, notify: notify}
}

// tick is the keepalive period in force.
func (t *chatTurnStatus) tick() time.Duration {
	if t.liveTick > 0 {
		return t.liveTick
	}
	return statusLiveKeepalive
}

// begin records that a turn's request has been ISSUED: the named turn is the one
// in flight, the phase is sent, the animation frame restarts, and the keepalive
// starts. The state change and the publish are ONE step, so the row can never be
// left showing the phase with nothing scheduled to redraw it.
func (t *chatTurnStatus) begin(turn int) lineedit.Status {
	t.source.beginTurn(turn)
	// The operational read-outs are re-read HERE as well as at the publish: this is
	// the REPL's own goroutine (the one that owns the agent's counters), and it is
	// the moment the row stops describing an idle session and starts describing a
	// turn. Refreshing only at the publish left the LIVE row frozen on whatever the
	// snapshot held when opsFn was installed — measured: an ops source installed at
	// REPL setup still read elapsed=0s / empty cache on a turn minutes later, so
	// the row printed a zero-looking duration it had never measured.
	t.source.refreshOps()
	t.startKeepalive()
	t.publishNow()
	return t.source.status()
}

// repaint re-reads the live counters (the agent's own, which the ops read-out is
// composed from) and the viewport, then publishes. It is the entry point for a
// caller that has nothing new to SET but must still redraw — the terminal
// resized, or the instrumentation was just installed. The width is applied
// BEFORE the status is composed, so the repaint it triggers draws the form the
// new width asks for rather than the previous one.
func (t *chatTurnStatus) repaint() {
	t.source.refreshOps()
	t.publishNow()
}

// setPhase moves the turn to phase and repaints immediately: the phase change is
// a real EVENT (the first thinking delta, the first answer delta), so the row
// must not wait for the next keepalive frame to show it.
func (t *chatTurnStatus) setPhase(phase chatTurnPhase) lineedit.Status {
	// The frame is NOT reset here: the animation's only job is to differ from the
	// last frame, and restarting it at every phase change would collapse the
	// phase change and the animation into one visual event.
	t.source.setPhase(phase)
	t.publishNow()
	return t.source.status()
}

// publish applies the REPL's completed counters and the IDLE state, and repaints:
// the turn is over, the prompt is back, and the row must say so. It is the ONE
// place the in-flight state is cleared, so no path can leave a stale "working".
func (t *chatTurnStatus) publish(s *chatStatusSource, iterCount, live, tokens int) lineedit.Status {
	t.end()
	// The ops are re-read by update() below, which is the whole reason the read-out
	// is a SOURCE (opsFn) rather than a value: a publish is where the session's
	// numbers are allowed to move, so the counters it installs arrive together with
	// a fresh operational snapshot instead of beside a frozen one.
	s.update(iterCount, live, tokens, "")
	if live <= 0 {
		s.endTurn()
	} else {
		s.setPhase(chatTurnSent)
	}
	t.publishNow()
	return s.status()
}

// end stops the keepalive and clears the in-flight state. It is idempotent (a
// turn can end through several paths) and it WAITS for the ticker goroutine, so
// "the animation stopped" is a fact by the time the caller returns rather than a
// hope.
func (t *chatTurnStatus) end() {
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
		<-t.done
		t.done = nil
	}
	t.source.endTurn()
}

// startKeepalive launches the ticker that repaints the in-flight row. A second
// call for the same turn is a no-op: the tracker owns at most ONE ticker, so a
// repeated event cannot leave two goroutines animating one row.
func (t *chatTurnStatus) startKeepalive() {
	if t.stop != nil {
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	t.stop, t.done = stop, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(t.tick())
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// The frame advances UNDER the lock together with the state it
				// animates, so a repaint can never read a frame that belongs to
				// the previous phase. The turn may have ended between the tick
				// and this lock: then live is 0 and the frame is left alone —
				// the ticker is on its way out, and a frame advanced here would
				// be one the idle row never shows.
				if !t.source.advanceFrame() {
					return
				}
				t.publishNow()
			}
		}
	}()
}

// publishNow raises the publish through the input layer's own seam: a flag plus a
// wake, never a terminal write (the read loop is the only writer).
func (t *chatTurnStatus) publishNow() {
	if t.notify != nil {
		t.notify()
	}
}

// status renders the current status from the counters — the tracker's own
// accessor, so a caller never reaches through to the source.
func (t *chatTurnStatus) status() lineedit.Status { return t.source.status() }

// statusColumnsForWidth picks the widest FORM of the status row that fits cols
// and returns its columns. It is the ONE definition of the degradation order
// (Rule 14) — the order in which fields are shed as the terminal narrows, each
// shedding its column AND its separator, so the row's own arithmetic falls out of
// the column list rather than being restated anywhere.
//
// The order, widest form first, follows one rule: the least load-bearing read-out
// yields first, and the one the operator is steering by never yields.
//
//	cost   — a session total and a lagging indicator: it only grows, and it is
//	         read at the END of a session, not while steering through one.
//	tool   — what ran last. Useful, but the answer band above already carries the
//	         tool's own output, so the row's copy is a reminder, not the record.
//	ctx    — the context read-out, and the LAST counter to go: it is the number
//	         that predicts a compaction, which is the event that changes what the
//	         model still remembers.
//
// A cache column used to head this list and it was a LIE the row could not
// honour: the row carries no cache column, so the widest form was already the
// first step that could drop anything and the "cache sheds first" step was a
// silent no-op at every width. It is gone — with its dead geometry and renderer
// (see the RETIRED note on statusCacheCol) and with the claim it rested on.
//
// The first three columns (model, iteration, mode) are never shed: they are the
// row's identity, and a status row with none of them is not a status row. A
// terminal too narrow for even those refuses the aligned layout entirely and
// statusLine falls back to the historic unaligned composition, which is the
// honest answer — dropping a padded column would keep its padding's cost while
// losing its information.
//
// THE CACHE GAP IS CLOSED (task 01a0dbe2). Until that task the row carried SIX
// columns while statusCacheCol reserved a tenth-of-the-row width for a cache
// column and the order below named it FIRST — so the first shed an operator could
// SEE was cost and the cache step was a no-op at every width. Reserved-but-undrawn
// is not a small defect: it is a claim about the screen that the screen does not
// make, and nothing failed when it drifted, because statusFormFor reports ok=false
// for a name with no column behind it and the loop below SKIPS such a form.
//
// The column is now RETIRED rather than added, and the arithmetic is why (see the
// RETIRED note on statusCacheCol): the six columns + five separators + gap + live
// block come to EXACTLY statusLayoutBudgetCols (100, zero slack, a compile-time
// check), and the composed width is only ever capped DOWN to statusColumnsAtCols —
// so there is no terminal, however wide, at which a seventh column could be drawn.
// The order below is therefore exactly the columns the row carries, and it is
// CHECKED rather than trusted: TestStatusLadder_EveryNameIsAColumnOfTheRow
// resolves each name against the row's own geometry (statusRowColumns) and fails
// on a name with no column, and TestStatusLadder_FormsNarrowStrictly fails on a
// step no terminal can select.
//
// The ladder is a list of NAMES rather than of counts precisely because the
// priority order is NOT the row's left-to-right order: cost sits to the RIGHT of
// ctx on the row but is shed BEFORE it, so a prefix of the row's columns cannot
// express the order. Naming the columns keeps the order in one place that reads
// like the documented list, and statusFormFor resolves each name against the
// row's real order — so a name here that the row does not carry cannot silently
// disappear.
func statusColumnsForWidth(cols int, full []lineedit.StatusColumn) []lineedit.StatusColumn {
	for _, form := range statusDegradationOrder {
		columns, ok := statusFormFor(form, full)
		if !ok {
			continue
		}
		if statusFormWidth(columns) <= cols {
			return columns
		}
	}
	// No form fits: hand back the identity columns and let statusLineColumns
	// refuse the aligned layout (it compares the reservation against the terminal
	// itself). Never a partial form — the row does not show half a column.
	return columnsNamed(statusKeepAlways, full)
}

// statusDegradationOrder lists the forms from WIDEST to NARROWEST, each the set
// of columns kept at that step — the documented order, written the way it reads.
//
// Each step must be STRICTLY narrower than the one before it. That is a property
// of the ladder, not a style note: statusColumnsForWidth takes the FIRST form that
// fits, so two forms of equal width make the second one dead — invisible, and
// untestable through the composer (measured: a form dropping ctx while keeping
// tool measured 69 cells, exactly like the form dropping tool while keeping ctx,
// so the ctx-last step the doc comment argues for could never be selected and ctx
// actually shed BEFORE tool — the opposite of the stated priority).
//
// TestStatusLadder_FormsNarrowStrictly is the guard: it sums every form and fails
// on a repeat, which is how a duplicate step is caught by arithmetic instead of
// being found by someone reading a terminal.
// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): master's order was
// cost, tool, ctx — FOUR steps, because ctx and cost were separate top-level
// columns. The union's row carries ONE operational column, so the ladder drops it
// as ONE step, and the three steps below are the whole order: the group (ctx,
// cache and money together), then tool, then the identity columns. Inside the
// group the documented ORDER survives as the group's own left-to-right policy —
// a tail cut takes cache first, then cost, and keeps the context read-out longest
// (statusMetricsField) — which is the same priority master's ladder expressed at
// top level, now expressed one level down.
var statusDegradationOrder = [][]statusColumn{
	{statusColModel, statusColTurn, statusColTool, statusColMetrics},
	{statusColModel, statusColTurn, statusColTool},
	{statusColModel, statusColTurn},
}

// statusShedOrder is the documented shed order written as NAMES — the list the
// doc comment above states in prose (cost, then tool, then ctx), in the code, so
// a test can hold it against the row's real columns.
//
// It is the guard's SUBJECT rather than a duplicate of statusDegradationOrder:
// the forms above are the geometry (which cells survive at which step, and they
// are written as column KEYS), while this is the ORDER the reasoning is stated
// in, and the two are allowed to be checked against each other. Before this
// existed the order lived only in a doc comment, which is how the row came to
// document a cache column it never carried — prose cannot fail a build.
// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): the shed order is
// ONE operational step, not three. The names still have to resolve — that is the
// guard's whole point — and the operational read-outs now leave TOGETHER, because
// they are one column: naming ctx and cost as separate steps would be the same
// class of claim master retired the cache column for, a ladder step with no
// geometry behind it that a bare `continue` would skip in silence.
var statusShedOrder = []string{
	statusColumnNameMetrics,
	statusColumnNameTool,
}

// statusColumn names one column of the status row: the key that lets the
// degradation order refer to a column by NAME (so the order reads like the
// documented list and can be reordered) rather than by a position in a slice.
// The keys ARE the row's left-to-right order (statusColModel == 0, …), which is
// what lets one list describe both the order and the geometry.
type statusColumn int

const (
	statusColModel statusColumn = iota
	statusColTurn
	statusColTool
	// statusColMetrics is the grouped column: context, cache and money share it,
	// so the ladder sheds them as ONE step (and their own left-to-right order in
	// the group is what decides which of the three a tail cut keeps — ctx last).
	statusColMetrics
)

// statusColumnNames maps each column key to the name the documented shed order
// uses for it. It is the OTHER half of statusRowColumns' geometry and it exists
// so a reader (and a test) can talk about the ladder in the words the doc
// comment uses.
//
// The pair is also the guard: a name in this map that has no entry in
// statusRowColumns is a name the ladder can step over silently, and
// TestStatusLadder_EveryNameIsAColumnOfTheRow fails on exactly that — as it does
// on a ladder form whose name is in neither (which is what a retired column
// becomes if only its geometry is deleted).
var statusColumnNames = map[statusColumn]string{
	statusColModel:   statusColumnNameModel,
	statusColTurn:    statusColumnNameTurn,
	statusColTool:    statusColumnNameTool,
	statusColMetrics: statusColumnNameMetrics,
}

// The column names as they appear in the documented shed order. They are
// constants rather than inline strings because the test that resolves the ladder
// against the geometry (TestStatusLadder_EveryNameIsAColumnOfTheRow) matches on
// the SAME spelling the doc uses — a typo in either would otherwise read as a
// name that resolves to nothing (Rule 1).
const (
	statusColumnNameModel = "model"
	statusColumnNameTurn  = "turn"
	statusColumnNameTool  = "tool"
	// statusColumnNameMetrics is the group's name in the documented order. It is
	// ONE name for three read-outs on purpose: they are one column, and a ladder
	// step is a statement about the geometry.
	statusColumnNameMetrics = "metrics"
)

// statusRowColumns is the row's column list — the ONE definition of which
// columns the status row CARRIES, in the row's left-to-right order, with the
// geometry each one is reserved. status() copies it and fills in the Texts; the
// ladder resolves its names against it.
//
// A column belongs here only if the row actually draws it. That is the property
// the epic's defect violated: statusCacheCol was reserved and named by the shed
// order while this list (then inline in status()) carried six columns, so the
// ladder's first step dropped nothing and its name was a claim the row could not
// honour. Anything removed from here must leave the ladder too — enforced by a
// test, not by this comment.
//
// Widths, not Texts, are what make it the geometry: the Texts are per-repaint
// values and are empty here on purpose (an empty field is padded, never dropped
// — see status()'s note on conditional list building).
func statusRowColumns() []lineedit.StatusColumn {
	return []lineedit.StatusColumn{
		{Width: statusModelCol},
		{Width: statusTurnCol},
		{Width: statusToolCol},
		// The metrics group is ONE column of this list: ctx, cache and money share
		// its cells, so the guard above (and the ladder behind it) sees the column
		// the row really draws — a name with no column behind it is exactly the
		// claim the retired cache column was, and the group must not reopen it by
		// being described as three columns here.
		// The column's width is statusMetricsFormCol — the group's own FULL form
		// (reservations + the rate's cells), not the sum of its sub-cell reservations:
		// the value statusCacheField draws is the pair PLUS the hit rate riding it
		// (task 01a0f8c9), so the column the row really draws is that form's width, and
		// a column declared narrower than its own content is what the width ladder
		// reads (the row would elide a group that fits).
		{Width: statusMetricsFormCol},
	}
}

// statusKeepAlways is the identity set: the columns no form sheds.
var statusKeepAlways = []statusColumn{statusColModel, statusColTurn}

// statusFormFor resolves a named form against the row's real column list,
// preserving the ROW's order (not the form's) so the drawn row always reads
// model → iter → mode → tool → ctx → cost however the form was written.
// ok is false when the form names a column the row does not carry — a ladder that
// has drifted from the geometry must not silently drop the field it misnamed.
// The caller SKIPS an unresolvable form (statusColumnsForWidth's `continue`),
// which is why the drift this predicate detects is caught by a test rather than
// by the row: a dropped form is invisible on screen. See
// TestStatusLadder_EveryNameIsAColumnOfTheRow for that guard.
func statusFormFor(form []statusColumn, full []lineedit.StatusColumn) ([]lineedit.StatusColumn, bool) {
	if len(form) == 0 {
		return nil, false
	}
	keep := make(map[statusColumn]bool, len(form))
	for _, name := range form {
		if int(name) >= len(full) {
			return nil, false
		}
		keep[name] = true
	}
	out := make([]lineedit.StatusColumn, 0, len(form))
	for i, c := range full {
		if keep[statusColumn(i)] {
			out = append(out, c)
		}
	}
	return out, true
}

// columnsNamed is statusFormFor's sibling for the always-kept set.
func columnsNamed(names []statusColumn, full []lineedit.StatusColumn) []lineedit.StatusColumn {
	out, _ := statusFormFor(names, full)
	return out
}

// statusFormWidth is the width a form of the row occupies: the sum of its
// columns, the separators between them, the gap before the live block, and the
// live block itself. It performs the SAME arithmetic statusColumnsAtCols does for
// the full form, and it counts separators from the column list rather than from a
// constant — a form of n columns is joined by n-1 separators, so a form that
// sheds a field sheds its separator with it.
func statusFormWidth(cols []lineedit.StatusColumn) int {
	total := statusLiveCol + lineedit.StatusLineGapCells
	for i, c := range cols {
		total += c.Width
		if i > 0 {
			total += lineedit.StatusColumnSepCells
		}
	}
	return total
}

// MERGE NOTE (rework run 01a0db73): master's in-flight state machine
// (chatTurnPhase / chatTurnStatus / liveHintText) and the branch's degradation
// ladder (statusColumnsForWidth and its named forms) are two DISJOINT
// contributions that both sit at this point in the file; both are KEPT in full,
// neither replaced nor paraphrased. The ladder is a pure function of (cols,
// full) and touches none of the state machine's fields.

// statusToolColumnFieldFor is the tool column's content at the ALIGNED row's
// budget: whatever statusToolField answers, composed to the COLUMN's width
// instead of the unaligned row cap. It is the arm the aligned composer calls, so
// the two forms of the row cannot disagree about WHICH state the tool fact is in
// (the disagreement is exactly what bug #5 was: one form said "tools · no tool",
// the other said "·  no tool" with a blank first half).
//
// The composition to the column, rather than the cap, is structural: a value
// wider than its column is cut from the TAIL by the layout's own field cut, where
// the tool's NAME sits — so composing to the column is what keeps the name whole.
func statusToolColumnFieldFor(toolName, toolSummary string, toolsOn bool) string {
	field := statusToolField(toolName, toolSummary, toolsOn)
	// The two state labels are the column's own facts and are never cut: they are
	// shorter than the column by construction (TestStatusToolStateLabelsFitTheirColumn).
	if toolName == "" {
		return field
	}
	// The COLUMN's own budget, not the unaligned row cap: see
	// statusToolColumnField for why the two must differ.
	return statusToolColumnField(toolName, toolSummary)
}

// ── the operational fields ──────────────────────────────────────────────────
//
// Each formatter below renders ONE field of the status row from a counter the
// runtime really maintains. The provenance is stated at the formatter rather
// than in a table that could drift from it:
//
//	statusCtxField   ← agent.Context.CalibratedTokens()  (internal/agent/context_tokens.go)
//	                   agent.Agent.ContextWindow        (internal/agent/loop.go)
//	statusCacheField ← deepseek.CostTracker.TotalCacheHitTokens /
//	                   TotalCacheMissTokens            (internal/deepseek/types.go)
//
// Every one of them has an UNMEASURED branch. That is the honesty clause: a
// counter that was never populated renders as "—" (or an empty field), never as
// a zero, because a zero is a CLAIM ("this cost nothing") and the truth is an
// absence ("nothing was priced"). The distinction survives into the drawn row.
//
// There is NO money formatter here any more, and that absence is deliberate
// (task 01a0dcd1): a price was never measured on this stack — it was derived
// from (price card generation × Beijing civil time × a scraped price page) — and
// the row is only allowed facts it can measure directly. See the RETIRED note on
// statusCostCol for the full reasoning and for where a bill DOES belong
// (`gopi session stats --cost`, computed once, off the repaint path).

// statusCtxPairSep is the glyph between the context read-out's numerator and
// denominator — the separator that makes the pair a RATIO the reader can weigh
// rather than two adjacent counts. It is named (Rule 1) because statusCtxPair
// spells it and the width arithmetic (statusCtxPairCells) counts it.
const statusCtxPairSep = "/"

// statusUnmeasured is the marker for a read-out that exists on the row but has
// no value yet. It is a named string (Rule 1) because two fields use it and a
// third-grader's dash is exactly the kind of literal that gets edited in one
// place and missed in the other.
const statusUnmeasured = "—"

// statusCtxLabel names the context read-out on the row: the "ctx" in
// "ctx 42.1k/64.0k". It is the ONE string that tells a reader what the pair
// beside it counts — the row carries a SECOND, differently-derived pair of the
// same unit (the cache pair, "↑12k↓4k"), and the operator reading his own live
// row guessed cache-hit/cache-miss for the context one.
//
// It is a WORD rather than a glyph, and the reason is colour: the row's
// distinctions must survive the palette being off (TestPaletteCarriesNoInformationAlone),
// so the fact name is spelled in cells a reader can see, not implied by an
// attribute. It is short because the sub-cell is 13 cells and the pair needs 10
// of them: "context" (7) would cost 4 more and push the geometry past the
// budget, which the compile-time guard would refuse.
//
// Its CELL width is statusCtxLabelCells, declared with the geometry (Rule 1) —
// never hand-counted here or there.
const statusCtxLabel = "ctx"

// statusCtxQuantGlyph is the sub-cell's third field: ONE cell that states WHAT the
// denominator is — gopi's own working/trimming budget, NOT the model's advertised
// window. It rides the pair's tail ("ctx 92.1k/200.0k≡"), so it is the cell a
// reader weighing the denominator meets. One cell because a WORD does not fit: the
// sub-cell is spent to its last cell by the pair's honest worst case plus this
// glyph (see statusCtxCol's arithmetic), and every other column is either a display
// contract (model 14, tool 16) or already carrying a fact.
//
// WHY "≡" AND NOT "⌫": "≡" says the pair is measured AGAINST the working budget
// — the "of" in "used of budget" — and claims no event. "⌫" names trimming, and
// the row does not say a trim happened: a number being large is not the statement
// "the history was compacted just now" (the occasional "⧉" counter is the field
// that reports an EVENT). A glyph meaning an event that did not occur is a lie the
// reader cannot check.
//
// WHY 200.0k IS A BUDGET AND NOT THE MODEL'S WINDOW (the defect this field closes):
// internal/agent/context.go's DefaultContextWindow is 200000, chosen for a MEASURED
// reason — prefill cost is the binding constraint on this stack (a 290K-token
// prefill measured ~16s) — while the backend model advertises a 1M context. So the
// number was RIGHT and its IDENTITY was missing, which is exactly what a reader
// cannot derive from the digits: the operator read 200.0k as a wrong model limit.
//
// The glyph is not self-evident BY DESIGN: its meaning is stated in the row's own
// help (`gopi chat -h`, THE STATUS ROW), in the field documentation above, and in
// this note. Leaving the denominator bare is the defect — a reader who does not
// know the glyph can look it up, while a reader of "ctx 92.1k/200.0k" has no way to
// learn that 200.0k is not the model's limit.
const statusCtxQuantGlyph = "≡"

// statusCtxEstimateGlyph is the qualifier's OTHER spelling: the same claim about
// the denominator, carried by a row whose NUMERATOR is a local estimate rather
// than the server's own prompt count.
//
// THE PROMISE THIS KEEPS (Rule 15, the defect this constant closes).
// statusOps.ctxMeasured has always carried the measured/estimated distinction, and
// its field comment promised "the row marks the estimated case" — while NOTHING
// read it: grep found the field declared, assigned in cli_chat.go's statusOpsFor and
// set in tests, and not one renderer. A flag computed and never spent is the silent
// failure Rule 15 names, and the row said the same thing whether its number was the
// API's or a BPE guess.
//
// WHY IT IS THIS CELL AND NOT A NEW ONE (measured, not chosen by taste). The
// context sub-cell is statusCtxCol = 12 cells and its pinned form renders 12 —
// "ctx 42k/64k≡" is exactly 12, slack 0 — because the row's budget is already spent
// to equality (statusPinSlackCells is 0 since task 01a0e2e9). A second mark would
// need a 13th cell, i.e. a re-partition of a row whose arithmetic is a compile-time
// guard. The qualifier is the ONE cell on this row that speaks about the number's
// PROVENANCE (the denominator's identity today), so the marked/estimated
// distinction rides it: same width in every case, in every form, at every terminal
// width — the 1-cell glyph is replaced, never joined by another.
//
// WHY "≈" AND NOT A SECOND "≡": "≈" is the plain reading of "approximately", which
// is exactly the estimated case, and it is one display cell (measured:
// lineedit.DisplayWidth("≈") == 1, the same as "≡"). It is a GLYPH rather than a
// word for the reason the qualifier itself is one: the row is already gold, so a
// distinction that must survive the palette being off has to be a character.
const statusCtxEstimateGlyph = "≈"

// statusCtxQuantGlyphFor is the qualifier a row of THIS ops carries: the measured
// spelling when the numerator came from the API's own prompt count, the estimate
// spelling when it did not. ONE decision function, so the composition below cannot
// mark one form and not another (Rule 14).
func statusCtxQuantGlyphFor(ops statusOps) string {
	if ops.ctxMeasured {
		return statusCtxQuantGlyph
	}
	return statusCtxEstimateGlyph
}

// statusCtxQuantCellsPinned is the glyph's width in cells, spelled from the string
// by the geometry test (Rule 13: a constant defined but bypassed is the defect). It
// is 1 because the glyph is a single display cell — the same class of measurement
// statusCtxLabelCellsPinned and statusCtxPairSepCells are, never a hand count.
const statusCtxQuantCellsPinned = 1

// statusCtxField renders the context sub-cell in the row's shape: the fact NAME,
// the ABSOLUTE used/max pair — the primary fact — the QUANTIFIER that says WHAT the
// denominator is, and, where the cells allow, the bar.
// bar.
//
//	"ctx 42.1k/64.0k≡"   (the pinned form: label + pair + quantifier)
//	"42.1k/64.0k≡"       (the 1M-window form: the pair needs 14 cells, so the label
//	                      yields and the quantifier stays — the denominator never
//	                      appears unqualified)
//	"ctx 42.1k"         (tokens known, window unknown — no denominator, so
//	                     nothing to be a percentage of; statusCtxPair)
//	""                  (nothing measured; the column holds its cells)
//
// WHY THE PAIR IS PRIMARY (task 01a0dcd1, the operator's own criterion): "the
// '6%' is a percentage such that you know the backend LLM spec. say 1M for
// context window, what if the model changes?" The window is configuration
// (Agent.ContextWindow, internal/agent/loop.go), so the same "6%" means 12k on a
// 200k window and 439k on a 1M one. A percentage without its denominator is an
// unreadable number; the pair IS the read-out. The order is Hermes'.
//
// THE DENOMINATOR'S IDENTITY (bug F): the pair alone still does not say WHOSE limit
// the denominator is, and among THIS ROW's two pairs a bare slash already means
// opposite things — the cache pair's separator sits between a numerator that is an
// ADDEND of the denominator and a denominator that is the TRANSPORT TOTAL actually
// observed (the operator read it as hit/miss), while the context pair's separator
// sits between a measured occupancy and a BUDGET whose only claim is that gopi stops
// growing the history there. So the sub-cell ends with statusCtxQuantGlyph, the one
// cell that names the denominator's kind (see that constant for why a glyph, and why
// this one). It is composed LAST and shed NEVER: it is the field that closes the
// ambiguity the operator reported, so a form that dropped it would reinstate it.
//
// WHY NO PERCENTAGE (task 01a0ddab, the fold): both operands of the percentage
// are ALREADY on the row, so it was a lower-precision restatement of the pair's
// own first number over its own second — and unlabelled it sat beside a second
// pair, which is the confusion the operator reported. The derivation itself is
// ALIVE (statusCtxPct, used by the unaligned composition below, where the cells
// are not reserved and the pair is often absent); what left the ROW is the third
// column of digits. See statusCtxCol's FOLD note for the cell arithmetic.
//
// The bar rides the label and the pair when statusCtxBarCells says there is room;
// in the pinned geometry that constant derives to ZERO, so the pinned row is the
// label, the pair and the quantifier, and a future column that affords a bar turns
// it on by
// arithmetic alone.
//
// THE HEAVY MARKER IS GONE FROM THIS SUB-CELL (bug F — see the RETIRED note on
// statusCtxHeavyMarker): it marked a context at 30% of the budget with the glyph a
// shell uses for an ERROR, one cell right of the number it marked, in a sub-cell
// spent to its last cell. The ambiguity report and the marker's own complaint share
// a root — a cell that says something about the denominator without saying it — and
// the cell that survives carries information (the identity) rather than emphasis.
//
// (Its own history: the marker rode the pair, not a percentage, because the pair is
// what the reader weighs: a context near its window is heavy regardless of how any
// percentage would have been drawn. It sits BEFORE the trailing padding, never
// after it — a marker inside a padded cell is swallowed by the cell's own cut
// (padStatusColumn pads when short and elides when long, so trailing spaces would
// become the elided cells and delete the marker).
func statusCtxField(ops statusOps, tokens int) string {
	// RE-PARTITION (task 01a0f8c9 — THE PAIR LEADS). This sub-cell used to return
	// the HIT RATE whenever the cache counters were measured (statusCacheHitRateField
	// first, the pair only as its fallback), and the measured consequence is in the
	// task's own briefing: on any session with two round-trips the row painted
	//
	//	hit 75.4% ·↑2M↓1M            (the pair gone, the rate leading)
	//
	// — the "number that predicts a compaction" this file's own rationale names as
	// primary, evicted from the row by a read-out that landed on top of it. The
	// order below is the invariant the task states: the ctx pair LEADS whenever a
	// window is measured, and the rate keeps a visible home (invariant I2 is the
	// caller's: statusMetricsField composes it into the CACHE sub-cell's value — see
	// statusCacheField — so the rate can never be shed at any width, because the
	// width ladder sheds a WHOLE sub-cell and the rate rides inside one).
	//
	// The pair's own composition is unchanged, including its yield rule: where the
	// widest pair a budget of this size can host does not fit beside the label, the
	// LABEL yields and the pair is shown whole (never a cut NUMBER, never a wider
	// row — the compile-time guard forbids the latter).
	pair := statusCtxPair(ops, tokens)
	if pair == "" {
		return ""
	}
	// The label is composed only where it FITS the sub-cell TOGETHER WITH the
	// quantifier — the cell that says what the denominator IS is not shed to keep a
	// word that only names the numerator's quantity.
	//
	// The pair's drawn form is bounded by its OWN allowance
	// (statusCtxPairCellsDrawn: two sides of statusCtxPairSideCellsDrawn plus the
	// separator), so the widths here are arithmetic rather than a sample of what the
	// formatter can print. Three rungs carry what the sub-cell's 12 cells can hold,
	// and each is stated below in cells; the last one fires only at a pair of two
	// four-cell sides ("200k/200k" at a window a hundred tokens from full), where the
	// label genuinely cannot ride along. Never a cut NUMBER — that is the one thing
	// this row must not print — and never a wider row, which the compile-time guard
	// forbids. The label is the right thing to yield LAST: it is the decoration the
	// reservation was not sized for, and the pair is the fact.
	label := statusCtxLabel + statusCtxBarGap
	// THE QUALIFIER'S SPELLING CARRIES THE NUMBER'S PROVENANCE (statusCtxEstimateGlyph):
	// one cell, decided by statusCtxQuantGlyphFor from ops.ctxMeasured, so the
	// measured and estimated rows differ by exactly one glyph and by no width.
	quant := statusCtxQuantGlyphFor(ops)
	base := label + pair + quant
	if lineedit.DisplayWidth(base) > statusCtxCol {
		// THREE RUNGS, NOT TWO, and the middle one is what the honest denominator
		// bought (the epic that gave this pair its own three-digit allowance: a 200000
		// budget is stated "200k", four cells a side, where the two-digit form stated
		// "1M" — a budget five times the real one).
		//
		// The sub-cell is 12 cells; a pair at the deployed window is 8 ("13k/200k"),
		// the qualifier 1, and the label WITH its gap 4 — 13, one over. The wording
		// this FOLD has to protect is the label ITSELF (it says which of the two pairs
		// of the same unit this number is), so the GAP is what yields first: the label
		// closes onto its number ("ctx13k/200k≡", exactly 12) and the reader keeps both
		// the name and the honest denominator. Losing the gap costs one cell of air;
		// losing the label costs the fact's identity; losing the denominator's third
		// digit costs the truth (statusCountCtxDigitsWidest).
		base = statusCtxLabel + pair + quant
	}
	if lineedit.DisplayWidth(base) > statusCtxCol {
		// THE PAIR MAY NOT YIELD — it IS this sub-cell's fact, and since task
		// 01a0f8c9 nothing above it competes for the cells (the rate moved to the
		// cache sub-cell). This rung is reached only where even the gapless label does
		// not fit — a pair of two four-cell sides ("200k/200k" at a nearly-full
		// window) — and yielding the LABEL is the right trade rather than dropping
		// the fact: the word names the quantity, the pair IS the quantity, and the
		// quantifier that closes the denominator's ambiguity stays with it.
		base = pair + quant
	}
	if pct := statusCtxPct(ops.ctxUsed, ops.ctxMax); statusCtxBarCells > 0 && pct != "" {
		// A WIDER geometry's form: the bar derived from the same pair, drawn
		// between the label and the pair. Unreachable in the pinned geometry (the
		// constant is 0 and TestStatusMetricsGroupArithmetic pins that), kept
		// because the composition is arithmetic rather than a hardcoded pinned
		// shape.
		if barred := label + statusBar(pct, statusCtxBarCells) + statusCtxBarGap + pair + quant; lineedit.DisplayWidth(barred) <= statusCtxCol {
			return barred
		}
	}
	return base
}

// statusCtxPair renders the context's absolute used/max pair — the row's primary
// context fact. It is the ONLY place the pair is spelled, so the aligned sub-cell
// and the unaligned label cannot drift apart in their numbers.
//
//	tokens known, window known → "123.4k/200k"
//	tokens known, no window    → "123.4k"
//	nothing measured           → ""
func statusCtxPair(ops statusOps, tokens int) string {
	used := ops.ctxUsed
	if used <= 0 && tokens > 0 {
		return formatCtxCount(tokens)
	}
	if used <= 0 {
		return ""
	}
	if ops.ctxMax <= 0 {
		return formatCtxCount(used)
	}
	// BOTH SIDES ARE DRAWN AT THIS PAIR'S OWN ALLOWANCE — one budget, applied once,
	// to both (statusCountCtxDigitsWidest says why the pair may not mix two
	// allowances). The window therefore states the budget it really is ("64k",
	// "200k", "1M") instead of climbing a rung and losing its tenth, and the
	// numerator is stated in the same unit system, so the pair is a ratio a reader
	// can act on rather than two figures in two precisions.
	//
	// WHERE THE SIDES LAND ON DIFFERENT RUNGS they stay in the two units their
	// MAGNITUDES select ("13k/1M"): the rung is a function of magnitude, and
	// re-spelling the numerator in the denominator's unit to force one unit on the
	// pair would print "0M" for a session that has measured something.
	return formatCtxCount(used) + statusCtxPairSep + formatCtxCount(ops.ctxMax)
}

// formatCtxCount renders one count of the CONTEXT pair — numerator, denominator, or
// the lone numerator of a pair whose window has not been measured yet — at that pair's
// own digit allowance (statusCountCtxDigitsWidest).
//
// IT IS THE ONE SPELLING OF THE CONTEXT FIELD'S NUMBERS (Rule 14), and it exists for
// the reason the pair's first defect existed: three call sites in this function spelled
// their counts through the CACHE pair's formatter, so a value with no window printed in
// one unit system while the same value beside a window printed in another. A single
// count on this row is one quantity however many cells it is drawn with, and this is the
// function that decides how it is spelled in all three shapes.
func formatCtxCount(n int) string {
	return boundedCountRectDigits(n, statusCountCtxDigitsWidest).text
}

// statusHitRateLabel names the cache hit-rate read-out on the row. It used to be
// the word "hit"; it is now the GLYPH that marks the reading as the CUMULATIVE
// one — the rate over every prompt token this SESSION has sent, the share the
// provider's cache already held (hits / (hits + writes)). The word was 4 cells
// (the label and the gap that joined it) and the row's own geometry has exactly
// 10 cells for the reading once the two cache counters and the group's seam are
// paid for (statusCacheHitRateReadingCells, derived below), so the second
// reading the epic adds — THIS TURN's rate (statusHitRateDeltaGlyph) — is funded
// by the word's four cells: the two readings coexist INSIDE the same 11-cell
// reservation, and statusCacheHitRateCells does not move.
//
// A GLYPH RATHER THAN A WORD IS ALSO WHAT MAKES THE TWO DISTINGUISHABLE: two
// percentages side by side with no markers would be one value whose parts a
// reader cannot attribute. Σ (sum over the session) and Δ (the difference this
// turn) are a pair, they are one cell each, and BOTH are defined in the legend
// (`gopi chat -h`, THE STATUS ROW) with the denominator each one is over.
const statusHitRateLabel = statusHitRateCumulativeGlyph

// statusHitRateCumulativeGlyph marks the cumulative reading, and
// statusHitRateDeltaGlyph marks this turn's. They are separate constants because
// they are two different markers with two different meanings, and the legend
// resolves both BY NAME (Rule 13/14: the reader's vocabulary and the row's are
// one definition, so a marker cannot be renamed on one side only).
const (
	statusHitRateCumulativeGlyph = "Σ"
	statusHitRateDeltaGlyph      = "Δ"
)

// statusHitRateCumulativeGlyphCells / statusHitRateDeltaGlyphCells are the two
// markers' widths in cells — one each, measured for the same reason every other
// width in this file is (Rule 1): the reservation below counts them, and a marker
// widened to a word would have to move the geometry rather than quietly overrun
// its cells.
const (
	statusHitRateCumulativeGlyphCells = 1
	statusHitRateDeltaGlyphCells      = 1
)

// statusHitRateDeltaDigitsCells is the widest number of digits the DELTA reading
// prints — the cells its own value can occupy.
//
// WHY A WHOLE PERCENT WITH NO SUFFIX (the epic's second reading). Two reasons,
// and both are measurements:
//
//  1. IT IS THE CELLS. The cumulative reading spends its percentage's full width
//     (statusHitRatePctCellsWidest, "100.0%") and the delta's own percentage form
//     would cost six more, i.e. 1 + 6 + 1 + 6 = 14 cells against the 10 the row
//     has (statusCacheHitRateReadingCells). A whole percent is 3 digits at most
//     and lands the two-reading form on EXACTLY 10 ("Σ98.8%Δ10"), which is why
//     the second reading costs the geometry nothing.
//  2. IT IS THE HONEST RESOLUTION. A turn's cache behaviour is ONE fresh sample
//     (a handful of thousand tokens); a tenth of a percent of it is noise. The
//     SESSION's rate keeps the fraction (statusHitRateDecimals — the operator's
//     own request, task 01a0f658) because it is an average over everything; the
//     turn's is stated to the percent, which is the precision its own sample
//     supports.
//
// The value is BOUNDED like every other number on this row: both readings are
// clamped to the same 0..100 ceiling (statusPctMax, one definition), so a
// difference in percent points can never be wider than three digits ("100") —
// derived here from the clamp that already exists rather than sampled from a
// rendering (Rule 1/Rule 13; the T5 lesson).
const statusHitRateDeltaDigitsCells = 3

// statusHitRatePctSep is the gap statusCacheHitRateField's LEGACY labelled form
// separates the reading from the word that names it with. It is kept because the
// form it belongs to is kept (a caller driving that composer with a geometry of
// its own still gets the word), and because statusHitRateReading trims the
// suffix the composer appended rather than a second spelling of it (Rule 14).
const statusHitRatePctSep = " "

// statusHitRateDecimals is the number of FRACTION digits the hit rate is printed
// with — the format parameter BOTH halves of this field are built from (Rule 1: a
// format parameter is a named constant, never a literal at the call site and
// never a second convention on the reading side).
//
// WHY A FRACTION AT ALL (the operator's own report, task 01a0f658): a whole
// percent is the RESOLUTION of the read-out, and this row carries the number
// precisely so it can be watched — "98%" threw away the part of the measurement
// the counters behind it do support. The measured session that filed it served
// 111104 of 112473 prompt tokens (98.75%, printed "98%").
//
// ONE DIGIT ALSO FITS THE SUB-CELL'S RESERVATION, and that is arithmetic rather
// than taste: every cell of the reservation is accounted for below
// (statusCacheHitRateCells) and the two readings the row draws land on exactly the
// sum the one word occupied, so no geometry constant moves
// (TestStatusHitRateFormIsBoundedByConstruction walks the extremes).
const statusHitRateDecimals = 1

// statusHitRatePctSuffix turns the number into a PERCENTAGE, and it is read by
// BOTH halves of the field (Rule 14): statusHitRatePct appends it and
// statusHitRateReading trims it. A second spelling on either side would be a
// format the other side cannot read back — the defect that leaves the row
// printing a number that wears no tier while the emphasis channel is silently
// inert (Rule 15).
const statusHitRatePctSuffix = "%"

// statusHitRatePctPointCells is the DECIMAL POINT the formatter writes between the
// whole part and the fraction — one cell, named because
// statusHitRatePctCellsWidest (the geometry's own statement of this form's widest
// width) is derived from it and a bare "100.0%" typed in the arithmetic would be a
// second, unchecked opinion about what this form looks like (Rule 1 / Rule 13).
const statusHitRatePctPointCells = 1

// statusHitRatePctSepCells is the width of the gap between the fact name and the
// percentage it names in statusCacheHitRateField's LEGACY labelled form
// (statusHitRatePctSep, one space). The row itself no longer spends it — the
// structured readings carry a glyph marker and no gap
// (statusCacheHitRateBoundaryCells is 0) — but the form it belongs to is still
// composed by that function for a caller with a geometry of its own, and the
// geometry's own note is where a reader looks for it.
const statusHitRatePctSepCells = 1

// statusCacheHitRateCells is the cells the cache hit-rate READINGS cost when they
// ride the cache sub-cell's value, and it is the term statusMetricsFormCol adds to
// the group's reservation. It is an EQUATION over named widths (Rule 1/13), every
// term of which is a string the row really draws:
//
//	statusHitRateLabelCells     1  the cumulative reading's marker (Σ)
//	statusHitRatePctCellsWidest 6  its percentage at its widest ("100.0%")
//	statusHitRateDeltaCells     4  this turn's reading: its marker (Δ) + "100"
//	statusCacheHitRateBoundaryCells 0  the gap before the first marker
//	                            ---
//	                           11  what the ONE labelled word ("hit 74.5%") cost
//
// THE TWO READINGS THEREFORE FIT THE ONE-WORD RESERVATION EXACTLY (task 01a10f4e):
// the word gave back three cells and the Δ marker plus its three digits take
// exactly those three, so statusCtxCol, statusCacheCol, statusMetricsCol,
// statusMetricsFormCol, statusPinCols and statusPinSlackCells are all UNMOVED —
// the pin stays 100 with slack 0 and the row's drawn width does not change by a
// cell. A geometry that had to widen for the second reading would be a different
// epic; this one is funded from inside.
const statusCacheHitRateCells = statusHitRateLabelCells + statusHitRatePctCellsWidest +
	statusHitRateDeltaCells + statusCacheHitRateBoundaryCells

// statusHitRateSpanBudget is the geometry the EMPHASIS channel asks the rate's
// composer about (statusHitRateSpan), and it is stated separately from
// statusCacheCol because the two answer different questions:
//
//	statusCacheCol   the cells the cache sub-cell RESERVES — the pair, and nothing else
//	                 (it is what the group's own arithmetic sums).
//	statusHitRateSpanBudget   the cells the row can spend on the RATE when it rides
//	                 that pair's value — the reservation plus the group's form
//	                 overhang (statusMetricsFormCol - statusMetricsCol), which is
//	                 the same number statusCacheHitRateCells names.
//
// The composer is asked with the cells the ROW can give it, so the span it answers
// is the form the row DREW; a caller that asked with the tighter reservation would
// get the empty read-out and the emphasis would be inert at exactly the states this
// channel exists for — the silent non-effect (Rule 15).
const statusHitRateSpanBudget = statusCacheCol + statusCacheHitRateCells

// statusHitRateRatio is the ONE ratio behind both of the row's hit-rate readings:
// served / (served + written), or ok=false when neither counter was exercised (the
// zero case IS the unmeasured case on this transport, Rule 15b).
//
// It is float64 for BOTH reasons the cumulative reading's note gives, and now for a
// third: a DELTA is a subtraction of two counters, so `read*100` on either side
// overflows the int type for magnitudes this field must still answer for.
func statusHitRateRatio(read, write int) (float64, bool) {
	if read <= 0 && write <= 0 {
		return 0, false
	}
	r, wr := float64(read), float64(write)
	total := r + wr
	if total <= 0 {
		// Unreachable (both counters are positive here) and kept as the guard
		// against dividing by it: no total, no rate.
		return 0, false
	}
	return r / total, true
}

// statusHitRatePct renders a hit-rate RATIO (a quotient in 0..1) as the row's
// percentage with statusHitRateDecimals fraction digits and the shared suffix.
//
// It is the ONE renderer of this form: statusCacheHitRate calls it, and
// statusHitRateReading reads its output back, so the drawn number and the number
// the tier is decided from cannot hold two different conventions (Rule 14).
func statusHitRatePct(ratio float64) string {
	return statusHitRatePctDigits(ratio, statusHitRateDecimals)
}

// statusHitRatePctWhole renders the same ratio with NO fraction digits — the form
// the TURN's reading wears (statusHitRateDeltaDigitsCells carries why: one turn is
// one sample, and a tenth of a percent of it is noise). It shares the clamp and the
// suffix with the fraction form through statusHitRatePctDigits, so the two readings
// cannot disagree about the ceiling or about how a percentage is spelled.
func statusHitRatePctWhole(ratio float64) string {
	return statusHitRatePctDigits(ratio, 0)
}

// statusHitRatePctDigits is the ONE place a ratio is turned into the row's
// percentage text: the clamp to statusPctMax, the fraction-digit count, and the
// suffix. Both readings' renderers are this function with a different digits
// argument — a second spelling of "how a percentage is written" is exactly the
// drift statusHitRatePctSuffix's note forbids (Rule 14).
func statusHitRatePctDigits(ratio float64, digits int) string {
	pct := ratio * 100
	// The clamp is the same one-definition ceiling statusCtxPct clamps to (Rule
	// 14). It is applied to the SCALED value so the digits printed are the clamped
	// number's: the transport can report a write with no read in the same response,
	// and a rate outside 0..100 is not a measurement a reader can act on.
	if pct > float64(statusPctMax) {
		pct = float64(statusPctMax)
	}
	if pct < 0 {
		pct = 0
	}
	return strconv.FormatFloat(pct, 'f', digits, 64) + statusHitRatePctSuffix
}

// statusHitRateReading is the READER to statusHitRatePct's writer: the percentage
// the row DRAWS, back as a number, with the suffix trimmed from the ONE constant
// the composer appended. ok=false is a form this field cannot read, and the
// caller's answer to that is to wear no tier rather than to guess one (Rule 15) —
// guessing would put the wrong hue on a measurement nobody can parse.
func statusHitRateReading(reading string) (float64, bool) {
	// THE MARKER IS PART OF THE FORM THE ROW DRAWS, so the reader strips it the same
	// way it strips the suffix: the two readings are told apart by exactly these
	// glyphs, and a reader that accepted only the bare percentage would be reading a
	// string the row never prints (Rule 14 — the writer's form and the reader's are
	// one convention). The glyphs come from the composer's own constants, and an
	// unprefixed percentage (the pre-epic form, still composed by the legacy labelled
	// path) keeps parsing unchanged.
	for _, marker := range []string{statusHitRateCumulativeGlyph, statusHitRateDeltaGlyph} {
		reading = strings.TrimPrefix(reading, marker)
	}
	pct, err := strconv.ParseFloat(strings.TrimSuffix(reading, statusHitRatePctSuffix), 64)
	if err != nil {
		return 0, false
	}
	return pct, true
}

// statusCacheHitRate returns the prompt cache's hit rate over the SESSION's own
// counters: hits / (hits + misses), clamped to 0..100 and printed with
// statusHitRateDecimals fraction digits ("98.8%"). It is the same
// one-definition policy statusCtxPct keeps for the context ratio, and the same
// clamp reason: the transport can report a write with no read in the same
// response, and a row that printed a negative or above-100% rate would be
// reporting a number the operator cannot act on.
//
// Nothing measured → "" (Rule 15b): the caller falls back to the absolute pair,
// so a row never claims a 0% hit rate for a cache that was never exercised. The
// zero case IS the unmeasured case on this transport, and it is stated as the
// absence rather than as a measurement of zero.
func statusCacheHitRate(ops statusOps) string {
	ratio, ok := statusHitRateRatio(ops.cacheRead, ops.cacheWrite)
	if !ok {
		return ""
	}
	return statusHitRatePct(ratio)
}

// statusCacheTurnHitRate returns THIS TURN's own cache hit rate — a SAME-INSTANT,
// SAME-口径 measurement of the turn (statusCacheTurnRatio does the arithmetic and
// states the defect it removes), printed to the whole percent
// (statusHitRatePctWhole).
//
// NOTHING DRAWN IS NOTHING MEASURED (Rule 15b). The empty answer has three states,
// and all three mean the same thing to a reader — there is no turn reading to show:
//
//	no baseline    the first request has not been issued yet (cacheTurnKnown false)
//	no traffic     the turn has produced no prompt-cache counters at all
//	already shown  both sides are zero, i.e. nothing changed since the snapshot
//
// A counter that went BACKWARDS is clamped rather than reported: the counters are
// monotonic on this transport (deepseek.CostTracker only adds), so a negative would
// mean the snapshot belongs to a different run — and a row that printed a negative
// percentage would be reporting a number the operator cannot act on, which is the
// same refusal statusHitRatePctDigits' clamp states for the ratio itself.
func statusCacheTurnHitRate(ops statusOps) string {
	ratio, ok := statusCacheTurnRatio(ops)
	if !ok {
		return ""
	}
	return statusHitRatePctWhole(ratio)
}

// statusCacheTurnRatio is the ONE arithmetic behind the turn reading: the share of
// THIS TURN's prompt tokens the cache served, taken over two increments that come
// from the SAME SNAPSHOT INSTANT and from the SAME QUANTITY.
//
// THE DEFECT IT REMOVES (task 01a1c312; the row read `Δ100%` on a turn whose real
// rate was 98.4%). The two increments used to be read off two DIFFERENT counters,
// each against its own baseline:
//
//	read Δ  = cacheRead  − cacheReadAtTurn     (CacheReadInputTokens)
//	write Δ = cacheWrite − cacheWriteAtTurn    (CacheCreationInputTokens + InputTokens)
//
// A sum of counters is only as old as its LAST arriving term, so any instant the
// two sides were not credited together produced a numerator with no denominator
// (write Δ == 0 ⇒ ratio 1) — the reading degraded to a binary indicator of "has
// this counter moved since the snapshot", which is exactly what the operator
// observed. It is one counter reading against one baseline or it is not a ratio.
//
// THE ARITHMETIC, and why it is same-口径. The transport defines the prompt side
// per response as input + cache_creation + cache_read (ClaudeUsage.
// PromptSideTokens), and it credits ALL THREE in one call (CostTracker.
// AddClaudeUsage), so the prompt-side total and the served count always arrive at
// the same instant from the same response. The write side is therefore DERIVED as
// the remainder of that same instant:
//
//	prompt Δ = cachePrompt − cachePromptAtTurn
//	read Δ   = cacheRead   − cacheReadAtTurn
//	write Δ  = prompt Δ − read Δ   (== input Δ + cache_creation Δ)
//
// — one snapshot, one quantity, no counter whose own arrival can lag its partner.
// When the prompt side carried no measurement this turn (a caller that never set
// cachePrompt, i.e. a statusOps built outside the row's own source), the miss
// counter's own delta is the only write side there is; that is a NAMED degradation
// rather than a silent one, and it is the same value the pre-fix code reported.
//
// ok=false is the genuine zero sample (both increments are zero/absent), never a
// clamped 100%: a turn that drew nothing has no rate, and printing one would claim
// a measurement nobody took (Rule 15b).
func statusCacheTurnRatio(ops statusOps) (float64, bool) {
	if !ops.cacheTurnKnown {
		return 0, false
	}
	read := ops.cacheRead - ops.cacheReadAtTurn
	write := ops.cacheWrite - ops.cacheWriteAtTurn
	if prompt := ops.cachePrompt - ops.cachePromptAtTurn; prompt > 0 {
		// The same-instant definition: the write side is what the prompt side holds
		// beyond what was served, measured against the SAME snapshot as the read.
		write = prompt - read
	}
	read, write = max(read, 0), max(write, 0)
	return statusHitRateRatio(read, write)
}

// statusHitRateTurnReading trims the turn reading's own form back to a number, the
// way statusHitRateReading does for the cumulative one (Rule 14: the reader takes
// the suffix from the ONE constant the composer appended). ok=false is a form this
// field cannot read, and the caller wears no tier rather than guessing one.
func statusHitRateTurnReading(reading string) (float64, bool) {
	return statusHitRateReading(reading)
}

// statusCacheHitRateField renders the hit-rate read-out at a geometry given by
// the caller: the reading's MARKER and the percentage.
//
//	"Σ74.5%" (measured: the cumulative marker, then the rate to
//	          statusHitRateDecimals fraction digits)
//	""       (a geometry too narrow for the marker plus its rate — and, before
//	          that, nothing measured: the honest empty, Rule 15b)
//
// THE MARKER IS THE NAME (task 01a10f4e). This composer used to lead with the word
// "hit"; the epic's second reading (this turn's, Δ) is funded by the three cells
// that word gave back, and the pair of markers does the naming job better than the
// one word did: Σ says the rate is over the whole session and Δ that it is over
// this turn, where "hit" said neither. Both markers are defined in the legend
// (`gopi chat -h`, THE STATUS ROW) with the denominator each one is over.
//
// IT IS NO LONGER THE CONTEXT SUB-CELL'S COMPOSER (task 01a0f8c9). It used to BE
// statusCtxField's answer — the rate replacing the pair outright — and the measured
// consequence was the row the task's own briefing quotes: "hit 75.4% ·↑2M↓1M", the
// pair that predicts a compaction gone from the row. The pair leads that sub-cell
// again, and the rate rides the CACHE sub-cell's value beside the two counters it
// is computed from (statusCacheField → statusCacheHitRateAppend, which is where the
// cells decision now lives). What remains here is the FORM — the one place the
// CUMULATIVE reading is spelled alone — kept as the definition the emphasis channel
// reads back through the same constants (statusHitRateCumulativeSpan) and as the
// narrower-geometry composer a caller with a budget of its own drives.
//
// WHY IT IS NOT COMPOSED ON THE ROW ANY MORE, and why nothing is lost by that: the
// rate's operands — the served and written counters — are drawn in the cache
// sub-cell, so the composition that states the ratio BESIDE them is one read-out
// rather than two cells apart from what it measures.
//
// THE RE-COMPOSITION COSTS NO GEOMETRY: no column, no reservation and no arithmetic
// term was added for either reading — the cells they spend are the ones the cache
// sub-cell's reservation already holds (statusCacheCol), and the group's seam count
// is unchanged (the readings ride INSIDE a value, which is why the width ladder can
// never shed them while keeping the pair — see statusMetricsField).
//
// The composition is built from NAMED constants only (statusHitRateLabel /
// statusCacheHitRateBoundaryCells, the latter zero because the marker detaches the
// reading from the pair) and its width is MEASURED with lineedit.DisplayWidth before
// it is returned, so no width here is sampled from a rendered string (the T5
// lesson: a reservation sampled from a sample string is not a reservation).
// cells is the geometry the form must fit, passed IN rather than read from
// statusCtxCol: the composition is arithmetic, and a test can then drive a
// narrower budget through the SAME function the row calls instead of arguing
// about a shape it cannot reach (Rule 31 — the surface under test is the one that
// runs).
func statusCacheHitRateField(ops statusOps, cells int) string {
	pct := statusCacheHitRate(ops)
	if pct == "" {
		return ""
	}
	reading := statusHitRateLabel + pct
	if lineedit.DisplayWidth(reading) <= cells {
		return reading
	}
	// THE UNMARKED FORM IS REFUSED ON THE ROW (task 01a0f8c9). It used to answer here
	// for a geometry narrower than the name, and the row must not print it: a bare
	// percentage TWO CELLS from "↑<read>↓<write>" — the pair it is computed FROM and
	// the shape the operator once read as a hit/miss ratio — is exactly the ambiguity
	// the marker exists to close (the "6%" beside "13.1k/200.0k" report). A
	// composition that cannot mark which reading the number is does not print it: it
	// yields ENTIRELY, and the caller's arm — the cache pair alone
	// (statusCacheHitRateReadings) — is what the cells show.
	//
	// The measurement is not lost by this arm: the percentage is a derived
	// restatement of the two operands the pair already carries (statusCacheField),
	// so a reader who cannot be told the ratio still has both counters. What the
	// marker buys is the READ-OUT, and a read-out without it is the defect.
	return ""
}

// statusCtxBase renders the context read-out as the pair and the percentage
// derived from it, with no bar. It is the UNALIGNED composition's form — where
// there is no column whose cells decide which of the two a value gets — and it is
// what a caller with no geometry should print.
//
//	tokens known, window known → "123.4k/200k 61%"
//	tokens known, no window    → "123.4k"   (a pair with no denominator)
//	nothing measured           → ""         (the column holds its cells)
func statusCtxBase(ops statusOps, tokens int) string {
	pair := statusCtxPair(ops, tokens)
	if pair == "" {
		return ""
	}
	pct := statusCtxPct(ops.ctxUsed, ops.ctxMax)
	if pct == "" {
		return pair
	}
	return pair + statusCtxBarGap + pct
}

// The cache read-out is the group's SECOND sub-cell: a directly measured
// transport fact (the tokens the prompt cache served and the tokens written into
// it), which is the class of read-out this row carries. It sits to the RIGHT of
// the context pair because a tail cut must reach it first: the context read-out —
// the number that predicts a compaction — is the one the row keeps longest.
// statusCtxField renders the context sub-cell: the ABSOLUTE used/max pair, the bar
// derived from it, and the percentage beside it — see the form's own note above
// for the order and the reasoning.

// statusCtxBarGap separates the context numbers from their bar.
const statusCtxBarGap = " "

// statusPctMax is the ceiling BOTH percentage read-outs on the row clamp to — the
// context's share of its window and the prompt cache's hit rate. One definition
// (Rule 14) because the two are the same class of measurement, and a second
// literal 100 in either clamp would be the kind of drift a reader cannot see.
const statusPctMax = 100

// statusCtxPct is the context's percentage of its window, clamped to 0..100. The
// clamp is not cosmetic: a compaction can leave the measured conversation larger
// than the window for the moment before it trims, and a row that printed 118%
// would be reporting a number the operator cannot act on.
func statusCtxPct(used, max int) string {
	if max <= 0 {
		return ""
	}
	pct := used * 100 / max
	if pct > statusPctMax {
		pct = statusPctMax
	}
	if pct < 0 {
		pct = 0
	}
	return fmt.Sprintf("%d%%", pct)
}

// statusBar renders a percentage as a fill bar. It is the graphic half of the
// number statusCtxPct prints, and it uses the SAME block glyphs the turn rule
// does so the row has one visual vocabulary.
//
// The bar is deliberately coarser than the percentage: statusBarCells cells
// means each cell is worth more than a percent, so the bar shows the ROUNDED
// truth and the digits beside it carry the precision. That is the opposite of
// the usual mistake (a bar pretending to be an instrument and disagreeing with
// the number it is drawn from).
func statusBar(pctStr string, cells int) string {
	if cells <= 0 {
		return ""
	}
	pct := 0
	if _, err := fmt.Sscanf(pctStr, "%d%%", &pct); err != nil {
		// An unparseable percentage means there is no percentage: render an empty
		// bar rather than a bar claiming zero. Rule 15b — the error is the
		// answer here, not something to swallow into a default.
		return strings.Repeat(statusBarEmptyCell, cells)
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * cells / 100
	return strings.Repeat(statusBarFullCell, filled) + strings.Repeat(statusBarEmptyCell, cells-filled)
}

// The bar's two glyphs. They are shared with the turn rule's vocabulary (a filled
// cell that reads as a solid block, a light one that reads as track), and the
// bar's WIDTH is not a constant of its own any more: it is statusCtxBarCells,
// derived from the context sub-cell's reservation (see that constant's note —
// in the pinned geometry it derives to zero, so no bar is drawn and no cells are
// reserved for one that is not).
const (
	statusBarFullCell  = "█"
	statusBarEmptyCell = "░"
)

// statusCountCompactKilo is the scale formatCountCompact rounds at: the same
// thousand its sibling divides by (one scale for the row's counts). The two are
// separate functions because they trade different things — formatCount keeps the
// decimal, formatCountCompact spends that cell on fitting a pair.
const statusCountCompactKilo = 1000

// formatCountCompact renders a count in the fewest cells that still tells its
// order of magnitude (12345 → "12k", 1234 → "1k", 999 → "999"). It is a PLAIN
// formatter with NO width bound, and that is now a measured property of the row
// rather than a defect: since the re-partition (task 01a0e2e9) the bounded form
// the row draws is formatCountBounded, and this one survives to define the cell
// threshold in boundCountUnit's note and to be measured by the arithmetic test.
//
// DO NOT put it back on the row. It is UNBOUNDED BY CONSTRUCTION — the
// representable integer spans ten magnitudes (1000 → "1k" of 2 cells, 2147483647
// → "2147483k" of 8 cells), so no small reservation can cover it and the layout's
// own cut is what the operator sees: the tail, where the cache-WRITE glyph and
// number live. That is the epic's defect (measured: "↑1662000↓1662000" →
// "↑1662k↓…", the ↓ swallowed), and it is unreachable only because the row's
// formatter (formatCountBounded) cannot produce such a string at any input.
func formatCountCompact(n int) string {
	if n < statusCountCompactKilo {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dk", n/statusCountCompactKilo)
}

// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): the cache read-out
// is a SUB-cell of the metrics group. Master retired it as a TOP-LEVEL column — the
// budget has no cells for a seventh column and its separator, and the 10-cell
// reservation did not even fit its own canonical render. Inside the group it costs a
// field separator instead of a column boundary and its COMPACT pair is what the
// group's reservation was measured against, so the read-out reaches the row again in
// a geometry that can hold it. That is a different column, not a resurrection of the
// retired one: the arithmetic above is what decides which of the two is drawn.
// The cache field's direction markers. Ascending/descending arrows are the
// conventional read/write pair, and they are ASCII-adjacent enough to be safe on
// any terminal the row runs on.
const (
	statusCacheReadGlyph  = "↑"
	statusCacheWriteGlyph = "↓"
)

// formatCountBounded renders a count in a form whose WIDTH IS BOUNDED BY
// CONSTRUCTION — at most statusCacheCountSideCellsWidest cells, for EVERY input
// the int type can hold — and it is the cache pair's formatter, the one
// statusCacheField draws with.
//
// WHY BOUNDED IS THE WHOLE POINT (epic 01a0e2e9). Its predecessor
// (formatCountCompact) is a function of the input's MAGNITUDE: a count that grows
// from thousands to millions lengthens its own rendering, and once it outgrows the
// sub-cell's reservation the row's own field cut takes the difference off the TAIL
// — where the ↓ arrow and the cache-WRITE number sit. The operator's row measured
// "↑1662k↓…": a paired read-out whose second half was silently missing, on the
// side that is billed at the MISS rate. A reservation cannot fix that (covering ten
// orders of magnitude costs cells the 100-column budget does not have); the FORM
// has to be bounded, so that the reservation can be a true statement about it.
//
// HOW (boundedCountRect, chat_status_cache_bound.go). The magnitude is drawn from a
// fixed LADDER OF UNITS, so no value can spell more of them than the ladder is long,
// and the digits are capped per rung (statusCountMaxDigits) with a per-rung
// remainder. The rounding is the same one the row's other counts use (one tenth of
// the printed unit): an approximation with a stated unit is honest, a TRUNCATED
// number is not, and no unit is ever invented — below the first rung the integer is
// printed whole.
//
// Its bound is MEASURED, not asserted: TestStatusCacheFieldWidthIsBounded walks
// every rung boundary, every rung's ceiling, the extremes of the int type and a
// sample of every magnitude in between, and fails if any rendering is wider than
// statusCacheCountSideCellsWidest or contains the row's own ellipsis.
func formatCountBounded(n int) string {
	return boundedCountRect(n).text
}

// statusCacheField renders the cache column: the tokens the prompt cache SERVED
// (a hit — the cheap path) and the tokens written INTO it (the miss that creates
// the entry, which the provider bills at the miss rate). Both come from
// deepseek.CostTracker, and both are real counters on this transport.
//
// The two are shown together, and labelled, because a single "cache" number
// would be ambiguous in a way the reader cannot resolve: a hit and a write cost
// ~30x different amounts per token on the current card (internal/pricing), so a
// total would average two facts with different meanings into one that has none.
//
// BOTH SIDES ARE ALWAYS WHOLE (epic 01a0e2e9). The formatter is bounded
// (formatCountBounded), the sub-cell's reservation is derived from that bound
// (statusCacheCol), and the pair below is built in a fixed order with fixed
// glyphs — so the string this returns can never be wider than the column that
// holds it, at any counter value, and the row's field cut has nothing left to
// take. That is the property the epic's defect violated: the pair was composed
// from an unbounded formatter inside a reservation sampled from one small string,
// and the operator's 1.66M-token session lost the miss-billed half of its own
// read-out to the cut.
//
// Nothing cached yet → empty (the column holds its cells): a session that has
// not re-sent a prompt has no cache story to tell, and "0/0" would invent one.
func statusCacheField(ops statusOps) string {
	pair := statusCachePairExpr(ops)
	if pair == "" {
		return ""
	}
	// The pair is rendered BOUNDED (formatCountBounded) and, unlike the pair it
	// replaces, the CELLS ARE THE REASON RATHER THAN A CONSTRAINT. The two facts it
	// carries — served vs written — are the row's only read-out of the prompt cache's
	// economics, and the write side is the one billed at the miss rate, so neither
	// may be the one the cut eats: the form is bounded so that BOTH arrive, whole,
	// with the precision traded instead of a fact (one tenth of a unit, never a
	// truncated number).

	// THE HIT RATE'S HOME (task 01a0f8c9). The rate used to LEAD the context
	// sub-cell and today rides here, beside the two counters it is computed FROM —
	// so the number and its operands are one read-out, two cells apart, and the
	// pair that predicts a compaction leads the group instead.
	//
	// WHY HERE AND NOT A SEVENTH SUB-CELL: the group draws one seam per boundary
	// (statusMetricsSep, 2 cells) and the row has no cells left — statusPinSlackCells
	// is 0 — so a third sub-cell would have to take 2 cells plus its own reservation
	// from a column that draws every one of them. Riding INSIDE this value costs
	// nothing for the same reason the pair itself costs nothing beyond the
	// reservation already made for it: the group's sub-cell reservation is the
	// column's own width, and the composition is measured against it rather than
	// assumed.
	//
	// ...AND THE RATE IS SHED LAST, BY CONSTRUCTION (the invariant this placement is
	// FOR). The width ladder drops a WHOLE sub-cell at a time — never a sub-cell's
	// tail — so a rate that is part of this value's own text leaves the row exactly
	// when the cache pair leaves it, and not one cell sooner. A rate in a sub-cell of
	// its own would instead be the FIRST thing a narrow row sheds while the pair it
	// is about survives. (The rate's own tail is still cut here rather than allowed
	// to widen the sub-cell, which is what statusCacheHitRateAppend decides.)
	return statusCacheHitRateAppend(ops, pair, statusHitRateSpanBudget)
}

// statusCachePairExpr composes the cache pair ALONE — the two bounded sides, each
// carrying its own direction glyph, with nothing between them — and it is split out
// of statusCacheField so a caller can ask what the PAIR costs without the hit rate
// riding its value (task 01a0f8c9): the width the cache sub-cell reserves is the
// pair's, and the pair is the part that must never be cut. Empty when nothing was
// cached, so both callers share ONE emptiness rule rather than two.
func statusCachePairExpr(ops statusOps) string {
	if ops.cacheRead <= 0 && ops.cacheWrite <= 0 {
		return ""
	}
	return fmt.Sprintf("%s%s%s%s",
		statusCacheReadGlyph, formatCountBounded(ops.cacheRead),
		statusCacheWriteGlyph, formatCountBounded(ops.cacheWrite))
}

// statusCacheHitRateAppend joins the cache pair with the hit rate it is computed
// from — "↑12k↓4k hit 74.5%" — and it is arithmetic rather than concatenation,
// because the pair must arrive WHOLE (epic 01a0e2e9, the defect this sub-cell's
// reservation exists for) and the rate is the part that yields when only one of
// the two can fit.
//
// The three forms, in order of what the cells buy:
//
//	"↑12k↓4k hit 74.5%"  both — the pinned form; the rate names itself
//	"↑12k↓4k"            the pair alone, where the rate and its name do not fit
//	""                   nothing measured (the caller's arm, not this one)
//
// WHY THE NAME YIELDS BEFORE THE PAIR AND THE PAIR BEFORE THE NAME'S NUMBER: a cut
// NUMBER is the one thing this row must not print (statusCtxPair's note), so a
// narrower geometry drops the whole labelled form rather than the digits of one. The
// row's own vocabulary already has the shape for this — statusCacheHitRateField's
// unlabelled arm — and it is NOT reused here on purpose: an unlabelled percentage
// beside a two-sided pair is exactly the ambiguity the operator reported when this
// row carried "6%" next to "13.1k/200.0k", and a sub-cell that cannot name a number
// does not print it.
//
// cells is the sub-cell's own reservation, passed IN rather than read from
// statusCacheCol, so a test can drive a narrower budget through the SAME function
// the row calls instead of arguing about a shape it cannot reach (Rule 31).
func statusCacheHitRateAppend(ops statusOps, pair string, cells int) string {
	return pair + statusCacheHitRateReadings(ops, cells-lineedit.DisplayWidth(pair))
}

// statusCacheHitRateReadings composes the row's hit-rate READINGS alone — the
// session's cumulative rate and this turn's — for a geometry of `cells` cells,
// given the cells the cache PAIR has already taken.
//
// THE FOUR FORMS, widest first. Each is dropped WHOLE, never cut into digits (a cut
// number is the one thing this row must not print — statusCtxPair's note), and the
// forms are tried in the order that keeps the MOST information in the cells:
//
//	"Σ98.8%Δ10"  both readings — the pinned form
//	"Σ98.8%"     the cumulative reading alone (no turn baseline, or a narrow geometry)
//	""           neither fits: the caller still has the pair
//
// THE DENOMINATORS, which the epic requires the reader be able to state (memory
// #5912: a percentage must carry the quantity it is a percentage OF):
//
//	Σ  cumulative — every prompt token THIS SESSION has sent: served/(served+written)
//	   over the whole session's counters (statusCacheHitRate).
//	Δ  this turn  — the prompt tokens of THE CURRENT TURN: the same formula over
//	   the DIFFERENCE of the counters since the turn's request was issued
//	   (statusCacheTurnHitRate).
//
// Both denominators are stated in the legend (`gopi chat -h`, THE STATUS ROW), where
// the reader also learns that the pair to their left is the SAME pair the rates are
// computed from — so the two readings and their operands are one read-out.
//
// cells is passed IN rather than read from the geometry, so a test can drive a
// narrower budget through the SAME function the row calls instead of arguing about a
// shape it cannot reach (Rule 31).
func statusCacheHitRateReadings(ops statusOps, cells int) string {
	cumulative, turn := statusCacheHitRate(ops), statusCacheTurnHitRate(ops)
	// The two readings are spelled ONCE, here, and the emphasis channel reads the
	// SAME constants back (statusHitRateReadings → statusHitRateSpans): a second
	// spelling on the emphasis side would let the coloured span and the drawn text
	// be different strings, which is the silent non-effect this channel exists to
	// remove (Rule 14/15).
	both := statusHitRateCumulativeGlyph + cumulative + statusHitRateDeltaGlyph + turn
	if cumulative != "" && turn != "" && lineedit.DisplayWidth(both) <= cells {
		return both
	}
	// The cumulative reading PROMOTES out of the pair form (it was the row's one
	// reading before this epic, and it is the one the operator steers by): the word
	// that used to name it is gone, so the marker alone must still fit — and it is
	// one cell plus a percentage, so a geometry the pair form fits always fits it.
	if cumulative != "" {
		labelled := statusHitRateCumulativeGlyph + cumulative
		if lineedit.DisplayWidth(labelled) <= cells {
			return labelled
		}
	}
	return ""
}

// statusHitRateCumulativeSpan and statusHitRateDeltaSpan name the two readings as
// the EMPHASIS channel must find them in the drawn text: each marker together with
// the percentage it introduces, spelled from the SAME constants the composer used.
// The turn's reading carries no suffix (it is the whole-percent form), so its span
// is its own marker and digits.
func statusHitRateCumulativeSpan(ops statusOps) string {
	rate := statusCacheHitRate(ops)
	if rate == "" {
		return ""
	}
	return statusHitRateCumulativeGlyph + rate
}

// statusHitRateDeltaSpan is the turn reading's span, in the same terms.
func statusHitRateDeltaSpan(ops statusOps) string {
	rate := statusCacheTurnHitRate(ops)
	if rate == "" {
		return ""
	}
	return statusHitRateDeltaGlyph + rate
}

// statusMetricsUnmeasured is the metrics group's content when NOT ONE of its
// read-outs has been measured — the field's state, in words, where the group
// would otherwise be a hole.
//
// THE DEFECT IT CLOSES (the group's own honesty clause, the twin of
// statusNoToolLabel's). The group is ONE reserved column of the row
// (statusMetricsFormCol), and until this constant existed its unmeasured arm
// returned "" — the column kept its cells as padding and the segment between
// the tool column and the live block was BLANK. A reader could not tell "nothing
// has been measured yet" from "this field is broken": the same sentence the tool
// column's idle indent used to say, one column to the left, and the operator read
// the row exactly that way.
//
// WHY IT IS NOT A ZERO, AND NOT AN EXIT. The refusal to print 0 for an
// unmeasured counter is the contract this field keeps (Rule 15b) and this
// constant does not weaken it: no read-out, no number — the group says NOTHING
// ABOUT ITS COUNTERS and something about ITSELF. The difference between "the
// count is 0" and "no count was taken" is the whole reason the arm exists, so the
// word states the ABSENCE of a measurement rather than a measurement of zero.
//
// WHY THE COLUMN'S OWN NAME. It reads "metrics" because that is what the column
// IS — the name the ladder's documented shed order uses for it
// (statusColumnNameMetrics) and the name a reader of this row already has — and
// because it makes the row carry ONE rule for a reserved field with no value
// rather than two: the tool column says "no tool" (statusNoToolLabel) and this
// column says "no metrics". Two idle fields, one spelling of idleness, so a
// reader who has decoded one column has decoded the other.
//
// WHY IT DOES NOT SAY "yet". A word claiming a measurement is coming would be the
// row predicting the future: whether a counter is ever taken is the session's
// business, not the row's, and a row that promised one and never showed it would
// be lying about the very thing this constant exists to stop lying about.
//
// IT FITS statusMetricsFormCol (10 cells against the column's 32 — statusCtxCol 12
// + one seam x2 + statusCacheCol 8 + the hit rate the cache value carries,
// statusCacheHitRateCells 10. It is the CONTEXT sub-cell's reservation that makes
// the column wide; the word needs less than half of it.) That is why the
// compile-time budget guard stays exact and NO column is re-partitioned: the
// group's reservation already holds this word, and a word that did not fit would
// be cut by the row's own field cut — the "the columns moved" defect wearing the
// honesty clause's clothes.
const statusMetricsUnmeasured = "no metrics"

// statusMetricsField renders the metrics GROUP: the row's operational read-outs —
// the ABSOLUTE context pair with its derived percentage, and the cache pair — as
// ONE column, joined internally by statusMetricsSep. It is the column the
// re-partition exists for (see statusMetricsFormCol): the row pays ONE top-level
// separator for the fields instead of the four each would cost as its own column.
//
// The group's order is fixed and IS the degradation policy inside the column:
// ctx · cache. A tail cut reaches the RIGHTMOST field first, so a group too
// narrow for its content sheds cache and keeps the context read-out — the number
// that predicts a compaction — longest: the documented order (cache → ctx) read
// as geometry rather than as a ladder.
//
// There were THREE sub-cells until task 01a0dcd1 retired the money one. Its
// removal is why ctx could move to the LEFT and stay primary: the cells the money
// held are now the pair's own width (see statusCtxCol), and the group's
// arithmetic, its sub-cell count and the row's pin followed in the same change.
//
// The seams are held: a sub-cell with no value is empty but STILL spends its
// separator once a later one has a value, so a cache pair appearing cannot slide
// the context pair. A LEADING empty sub-cell does not indent the ones after it
// (the group is left-anchored), which is the one movement the row can make inside
// its own column — and why the pinned cell assertions address columns, not
// sub-cells.
//
// NOTHING MEASURED is the group's other state, and it is STATED rather than
// implied: see statusMetricsUnmeasured. The word the group carries there is the
// column's own content — it is not composed from the sub-cells, and it must stay
// inside statusMetricsFormCol so that the row's fixed geometry is untouched.
func statusMetricsField(ops statusOps, tokens int) string {
	// The sub-cells are built from the SAME list the geometry derives its width
	// from (statusMetricsSubCellWidths' order and count), so a read-out added or
	// removed cannot leave the group drawing a cell the reservation never counted:
	// the context read-out is first because it is the primary fact, and the cache
	// pair is second because a tail cut must reach it first.
	// The sub-cells are asked for the width their OWN value may occupy — the context
	// sub-cell's reservation, and the cache sub-cell's reservation plus the rate's
	// cells (task 01a0f8c9) — so each composer's own bound is the geometry this row
	// gives it rather than a number it invents.
	fields := [statusMetricsSubCells]string{
		statusCtxField(ops, tokens),
		statusCacheField(ops),
	}
	last := -1
	for i := len(fields) - 1; i >= 0; i-- {
		if fields[i] != "" {
			last = i
			break
		}
	}
	if last < 0 {
		// Nothing measured yet: the column keeps its cells and STATES the fact
		// (statusMetricsUnmeasured) — never an invented zero (Rule 15b), and never
		// a hole, which is what this arm used to return. An unmeasured COUNTER
		// still prints no number: the arm returns a word about the group's own
		// state, not a measurement of any counter.
		return statusMetricsUnmeasured
	}
	var out strings.Builder
	wrote := false
	// A seam is spent only BETWEEN two read-outs the group can actually show: an
	// unmeasured sub-cell — wherever it falls — costs no seam and no cells, which
	// is the same rule the left anchor states, applied to a hole in the middle as
	// well as at the front. (The defect this closes: a middle hole was joined
	// anyway, so a row carried two adjacent separators with nothing between them —
	// two seams for one boundary, in the row's own vocabulary of group seams.)
	for i := 0; i <= last; i++ {
		if fields[i] == "" {
			continue
		}
		if wrote {
			out.WriteString(statusMetricsSep)
		}
		out.WriteString(fields[i])
		wrote = true
	}
	return out.String()
}

// statusLiveShortWord pairs a state's FULL spelling with its SHORT one — the row's
// answer to "the counter and the word do not fit together". It is data rather than
// a switch so the alternative spellings live in ONE place (Rule 13/14): a state
// added to the phase machine is added here by name, and a state with no short
// spelling is simply absent (statusLiveSent and statusLiveIdle: "sent" and "ready"
// already fit beside a compaction counter, so a shorter spelling of them would be
// the same string — a definition with nothing to define).
var statusLiveShortWord = []struct {
	full  string
	short string
}{
	{full: statusLiveWordResponding, short: statusLiveWordRespondingShort},
	{full: statusLiveWordThinking, short: statusLiveWordThinkingShort},
}

// statusLiveStateWords returns the spellings a state may be shown in, the FULL one
// first: the block tries them in order and keeps the first that fits beside the
// counter. A state with no short spelling answers with itself alone, so the caller's
// loop is the same shape for every state and the fallback (the counter is dropped)
// stays reachable exactly as it was.
//
// The hint may carry the keepalive's animation cells behind the word, so the match
// is on the word's PREFIX and the suffix is carried over unchanged: the animation is
// the same cells in either spelling, and stripping or re-adding them here would be a
// second composer of the frame.
func statusLiveStateWords(hint string) []string {
	for _, sw := range statusLiveShortWord {
		if strings.HasPrefix(hint, sw.full) {
			return []string{hint, sw.short + hint[len(sw.full):]}
		}
	}
	return []string{hint}
}

// statusLiveField renders the live block: the session's elapsed time, the state
// word, and any queued hint. Elapsed rides HERE rather than in a column of its
// own because this is the one part of the row that is not a field (it is the
// right-aligned tail), so a value that is a property of the whole session sits
// with the state it describes without spending a column's worth of separators.
//
// The time is omitted entirely when the source was not given a clock — never
// rendered as 00:00, which would claim the session started just now.
// statusLiveField renders the live block for a caller that has no voice phase to
// report — the direct-composition tests and every non-chat surface. It is the
// SAME composer the production row uses, handed the idle phase, so the row it
// draws and the row the chat draws cannot be two different strings (Rule 14).
func statusLiveField(hint, queued string, ops statusOps) string {
	return statusLiveFieldVoice(hint, queued, ops, voiceChatLiveIdle)
}

func statusLiveFieldVoice(hint, queued string, ops statusOps, voice voiceChatLive) string {
	// MERGE NOTE (rework run 01a0db73): this field is the branch's (the live hint
	// carries the elapsed time and the queued text beside the state word), but the
	// STATE WORD inside it is master's. The branch composed the word from a boolean
	// — live > 0 -> statusLiveThinking — because its base had no phase machine,
	// while master's row distinguishes sent / thinking / responding and animates
	// with a frame (chatStatusSource.liveHintText, composed under the same lock as
	// the counters it belongs to). So the word is master's, handed in already
	// composed, and this field wraps it. Keeping the branch's two-valued word would
	// have been a silent deletion of master's three-state read-out; the branch's own
	// pair of constants (statusLiveThinking = "thinking…", statusLiveIdle = "ready")
	// is REPLACED by master's richer set (statusLiveSent/Thinking/Responding plus
	// statusLiveWord*) rather than dropped — the two tip lines this tree does not
	// carry are exactly that const pair.
	//
	// THE BLOCK IS A REAL COLUMN (defect fix, task 01a0dbc7). statusLiveCol is a
	// RESERVED width: the row's arithmetic budgets it, the layout pads the hint
	// inside it, and the composer's own gap check depends on it being the width the
	// text really occupies. Appending the elapsed label to the state word
	// UNCONDITIONALLY broke that reservation — the block's real width became a
	// function of the session's age ("4m00s" is three cells wider than "0s"), so at
	// the pin the row overflowed the budget it was composed for and
	// lineedit.statusLineColumns (gap >= StatusLineGap) refused the ALIGNED layout
	// outright: the degradation ladder below was unreachable on every width,
	// because no form ever survived the composition check. Measured on the real pty
	// before this fix (cols 100/95/80): the row never aligned below the pin, and at
	// the pin a grown elapsed label cut the state word with the row's ellipsis.
	//
	// So the SUFFIXES yield to the reservation, in the block's own priority order:
	// the state word is the block's reason to exist and is never cut (a queued
	// note and the elapsed label are additions to it), the elapsed label is the
	// next most valuable (it is the session's own wall time — Hermes' `duration`
	// in the one place the row has left), and the queued note is dropped whole
	// rather than shown cut. A suffix that would push the block past
	// statusLiveCol is left OFF — never truncated — because a reserved column
	// that its content can widen is the "columns move while the agent works"
	// defect wearing a different hat.
	// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): the body below
	// is master's — every suffix is weighed against the reserved block
	// (statusLiveCol) and left OFF rather than truncated when it does not fit,
	// which is what keeps the row inside its band and the ladder reachable. ONE
	// addition rides it, and it is this branch's: the counter that prefixes the
	// state word is chosen by PRIORITY rather than always being the clock. A
	// session that has COMPACTED shows its compaction count — the event that
	// changed what the model still remembers outranks the duration, which is a
	// comfort number — and only a session with no compaction shows the elapsed
	// time. It is a strict extension: with no compaction (which is what master's
	// own ladder fixture composes) the choice falls through to the elapsed label
	// and this field behaves exactly as master's does.
	out := hint
	counter := ""
	switch {
	case ops.compressionsKnown && ops.compressions > 0:
		counter = statusCompactionLabel(ops.compressions)
	case ops.elapsedKnown && ops.elapsed > 0:
		counter = statusElapsedLabel(ops.elapsed)
	}
	// THE VOICE FIELD IS WEIGHED FIRST (task 01a116b0), and it is weighed as a
	// WHOLE field: either the block holds it and the state word, or the field is
	// left off entirely — never cut, because "r" is a word this vocabulary does
	// not contain (voiceChatIndicatorFor's contract). It outranks the counter
	// because the two facts are not comparable: the counter reports something that
	// has already happened (how long the session has run, how often it has
	// compacted), the field reports what the operator is doing RIGHT NOW — the
	// microphone is open, so the next keystroke is being recorded.
	//
	// THE COUNTER CANNOT RIDE WITH IT, and that is arithmetic rather than a choice:
	// the shortest form that carries both is field(5) + sep(3) + "⧉30"(3) + sep(3) +
	// "resp"+gap(5) = 19 cells against the 14 the block reserves. So while the mode
	// is on the clock yields and returns the moment it closes; what competes for the
	// room behind the field is only the state word and its animation, which
	// statusLiveVoiceForm resolves in the block's existing rank (full word, then the
	// SHORT spelling the compaction rule introduced, then the dots — the block's
	// lowest-value content, shed only when no form with them fits).
	prefix := voiceChatIndicatorFor(voice)
	voiced := false
	if prefix != "" {
		if form, ok := statusLiveVoiceForm(prefix, hint); ok {
			out, voiced = form, true
		}
	}
	// THE LEGACY LADDER, unchanged and unreached while the field is on: a block that
	// carries the field has already been composed above, and a block that does not
	// (idle, or a surface with no voice seam) must behave exactly as it did before
	// this field existed — which is what keeps the field's arrival auditable.
	if !voiced && counter != "" {
		// THE COUNTER OUTRANKS THE STATE WORD'S LENGTH (the operator's own priority,
		// filed as the "⧉N hidden by responding" defect): a compaction changes what the
		// model still remembers, the state word only says which wait the turn is in, so
		// the block tries the state in BOTH its spellings before it gives up the
		// counter — and the counter is the thing that is dropped, never the state.
		//
		// MEASURED WHY THE SHORT SPELLING IS THE LEVER RATHER THAN A WIDER BLOCK: every
		// cell of the 100-cell pin is claimed and DRAWN (TestStatusReservationsAreFullyDrawn
		// — ctx 12/12, cache 19/19, group 33/33, tool 19/19, live 13/13), so no reserve
		// exists to widen statusLiveCol, and the word the defect names is the one thing
		// in the block whose information survives being said shorter. "⧉30 · resp" is 10
		// cells against the 13 the block reserves; "⧉30 · responding" is 15.
		//
		// THE SAME ARITHMETIC PAYS FOR THE /VOICE FIELD (task 01a116b0): the room the
		// counter yields when the mode is on is the room the field takes, which is why
		// the field could be added WITHOUT widening the block — and why its word had to
		// be three cells (chat_voice.go's vocabulary note derives the bound).
		for _, word := range statusLiveStateWords(hint) {
			if withCounter := counter + lineedit.StatusSep + word; statusFieldCells(withCounter) <= statusLiveCol {
				out = withCounter
				break
			}
		}
	}
	if queued != "" {
		withQueued := out + lineedit.StatusSep + queued
		if statusFieldCells(withQueued) <= statusLiveCol {
			out = withQueued
		}
	}
	return out
}

// statusLiveVoiceForm composes the live block while the `/voice` mode is on: the
// mode's own field, the separator the block joins its fields with, and the state
// word as the room allows. It is the ONE place the field's RANK is implemented —
// above the counter, above the word's length, above the animation — and it answers
// ok=false when even the shortest form does not fit, which is what tells the
// caller to compose the block exactly as it would with the mode off rather than to
// cut the field (the field is never cut).
//
// The order it walks is the block's existing rank, reused rather than restated:
// statusLiveStateWords hands back the word's full spelling first and its SHORT
// spelling second (the substitution the compaction counter introduced), and the
// two spellings are tried with the animation BEFORE either is tried without it —
// the dots are shed last, because "still working" is the one signal on the row
// that the process has not hung, and a word that is merely shorter is the cheaper
// concession (the same judgement the counter's own ladder makes, one rank up).
func statusLiveVoiceForm(prefix, hint string) (string, bool) {
	for _, tail := range statusLiveAnimationForms(hint) {
		for _, word := range statusLiveStateWords(tail) {
			form := prefix + lineedit.StatusSep + word
			if statusFieldCells(form) <= statusLiveCol {
				return form, true
			}
		}
	}
	return "", false
}

// statusLiveAnimationForms lists the state word as the block may draw it, longest
// first: the hint the source composed (word plus its animation frames), then the
// same word with the animation shed WHOLE. It is whole because the frames are the
// source's own count (liveHintFrameCount): a single dot the source never drew
// would be an animation frame nothing produced, and the row's dots are how a
// reader strips the animation to read the state back off it.
//
// The hint is the block's INPUT (the state word is composed under the status
// source's lock, beside the counters it belongs to — see liveHintText), so this is
// a pure function of it and never a second source for the word.
func statusLiveAnimationForms(hint string) []string {
	bare := strings.TrimRight(hint, statusLiveAnimationChar)
	if bare == "" || bare == hint {
		return []string{hint}
	}
	return []string{hint, bare}
}

// statusFieldCells is a field's width in DISPLAY CELLS, the unit the row's
// geometry is stated in. It exists so the live block's fit check (above) measures
// the same quantity the reservation is expressed in: len() would count the
// multi-byte "·" separator as two cells, which is the exact mistake that once put
// the pinned geometry three cells past a 100-column terminal (Rule 14 — one
// measure, used everywhere).
func statusFieldCells(text string) int { return lineedit.DisplayWidth(text) }

// statusTokensHeavySuffix is the UNALIGNED composition's warn word: a token total at
// or past statusTokensHeavy is labelled " (large)" on the fallback label.
//
// WHY THIS ONE SURVIVES THE GLYPH'S RETIREMENT (bug F): the two forms carry the same
// judgement in different budgets. The aligned sub-cell is a fixed 18 cells whose last
// cell the qualifier (statusCtxQuantGlyph) holds, so it cannot also host a mark — and
// there the reader is weighing a RATIO, where the judgement is already carried by the
// pair's two numbers. The fallback label has no reserved cells at all (it is
// concatenated and then cut by the terminal), so it can afford a WORD, and a word
// states the judgement directly instead of leaving a glyph whose nearest documented
// meaning is the shell's failure marker. Retiring the glyph was not "removing the
// warn": it was choosing the form where the judgement is legible over the form where
// it was ambiguous.
//
// The suffix is bounded by design: the aligned row never carries it (that is what the
// qualifier's cell buys) and the fallback has no column to overflow — the field has a
// reserved column (statusTokensCol), and a verbose
// suffix would push the field past its own column and therefore out of the row's
// geometry.
const statusTokensHeavySuffix = " (large)"

// statusNoToolLabel is the tool column's content when this surface HAS tools and
// NO tool has run in the turn: the field's IDLE FACT, in words. It REPLACES
// statusFieldIndent ("  "), which was the indent that held the column while no
// tool had run — the column is still held (statusToolCol reserves it), but what
// it carries is now a stated fact instead of a hole, and the old constant is gone
// rather than kept as a bypassed fallback (Rule 13).
//
// The column is RESERVED on every repaint (statusToolCol) — that is the alignment
// contract, and a hole in the middle of the row would read as "the fields moved".
// But a reservation is not a licence to print nothing: a reader who sees blank
// cells cannot tell "no tool has run yet" from "this field is broken", and the
// operator's own row was read exactly that way. So the absence is STATED.
//
// THE NEGATION IS THE WHOLE POINT (operator bug #5, task 01a0e4c7). This label
// used to sit BESIDE a mode column reading "tools", so the row said the same
// thing twice with opposite signs nine cells apart:
//
//	fake-model     ·  iter 1  ·  tools ·  no tool          ·  ctx 51/200.0k≡   6s · ready
//	                              ^^^^^^   ^^^^^^^^^
//
// — which reads as a contradiction in left-to-right order ("tools… no tool") and
// is ONE bit of information spent on two columns. The pair is now ONE answer from
// ONE column (see statusToolField): "no tool" is the tools-enabled surface's own
// negative fact, and a surface with NO tools never reaches this label at all (it
// has nothing to be negative about — statusNoToolsLabel).
const statusNoToolLabel = "no tool"

// statusNoToolsLabel is the tool column's content when the surface has NO tools
// configured — the plain `gopi chat` path, where `--tools` was not passed.
//
// It exists because the row must not print a field it cannot justify: "no tool"
// is a statement ABOUT TOOL USE (there are tools and none has run), while a
// surface that carries no tools at all is in a different state and saying "no
// tool" there would invite the reader to wait for one. One word, and the two
// states are distinguishable — which is exactly what the retired mode column's
// blank-on-one-path behaviour destroyed (see statusToolColumnField).
const statusNoToolsLabel = "tools off"

// statusToolField renders the row's ONE tool-state field — the whole of what the
// row says about tools (operator bug #5). It has three arms, and together they
// are the complete set of states the fact can be in:
//
//	toolName != ""          → the tool's name and digest   ("⚙ bash head -3 go.mod")
//	toolsOn, no tool yet    → statusNoToolLabel            ("no tool")
//	no tools on the surface → statusNoToolsLabel           ("tools off")
//
// This single answer is the fix. The row used to answer the same question from
// TWO columns — a mode column reading "tools" (the configuration) and this column
// reading "no tool" (the activity) — which spent one bit of information twice,
// nine cells apart, in an order that reads as self-contradiction. The mode column
// is RETIRED and the configuration fact it carried is what the second arm states,
// once, in the same column as the activity it qualifies.
//
// The bound is statusToolRowMax runes for the UNALIGNED label arm (so the
// fallback row cannot wrap) — see statusToolColumnField for the aligned column's
// own budget. The digest is ellipsized, never wrapped.
//
// The field is present for the WHOLE turn — while a tool runs ("⚙ bash head -3
// go.mod") and after it ("⚙ bash" + " […]" marks the ordered run's earlier
// activity) — because its column is reserved either way: a field that vanished
// between events would leave its reservation as a hole, and the row would read
// as if the fields had moved.
func statusToolField(toolName, toolSummary string, toolsOn bool) string {
	if toolName == "" {
		if toolsOn {
			return statusNoToolLabel
		}
		return statusNoToolsLabel
	}
	row := toolGlyph + " " + toolName
	// The NAME is the load-bearing part of this field — it is what the reader
	// steers by, and what the row's own tool assertions anchor. So the SUMMARY
	// yields first: a digest is added only as far as the field's cap allows, and
	// the name is never what gets cut. Measured (lineedit_height_test.go:300):
	// growing the whole row to the cap let a long digest eat the name's tail
	// ("⚙ keyword_searc…"), so the row stopped naming the tool that ran — the
	// budget belongs to the name, and the digest spends only what is left.
	if toolSummary != "" {
		room := statusToolRowMax - truncateWidth(toolGlyph+" "+toolName)
		// room-1: truncateRunes appends its own ellipsis, so one cell of the
		// remaining budget belongs to that marker (the same "reserve the marker
		// BEFORE the cut" rule the layout's own field cut follows).
		if room > 1 {
			row += " " + truncateRunes(toolSummary, room-1)
		}
	}
	// truncateRunes returns at most n runes PLUS its ellipsis, so the budget is
	// capped one short: the row must not exceed statusToolRowMax runes in total,
	// or it wraps.
	return truncateRunes(row, statusToolRowMax-1)
}

// statusToolColumnField renders the tool field for the ALIGNED row's column: the
// same field, composed to the COLUMN's budget instead of the unaligned row cap.
//
// The distinction is structural, not cosmetic. The column is the row's geometry,
// and a value wider than its column is cut by the layout's own field cut
// (padStatusColumn) — which cuts from the TAIL, where the tool's NAME sits. So a
// long digest used to shear the name inside the column ("⚙ keyword_searc…") and
// the row stopped naming the tool that ran. Composing to the column instead keeps
// the name whole: the summary is included only as far as the column allows, and
// the name is elided only when it alone is wider than the column.
func statusToolColumnField(toolName, toolSummary string) string {
	if toolName == "" {
		return ""
	}
	if toolSummary == "" {
		return truncateRunes(toolGlyph+" "+toolName, statusToolCol)
	}
	head := toolGlyph + " " + toolName + " "
	// The summary's room: whatever the column has left after the name and the
	// separator space, less the marker truncateRunes appends.
	//
	// room-1, not room: truncateRunes returns at most n runes PLUS its own "…",
	// so asking it for `room` yields room+1 cells and the composed field
	// overflows its own column by exactly one — the layout's tail cut then bites
	// a character the column had already paid for ("⚙ keyword_search lineedi…"
	// at 25 cells, 26 wide). This is the same marker reservation statusToolField
	// makes above, and it is the whole of the difference between the two
	// composers.
	room := statusToolCol - truncateWidth(head)
	if room <= 1 {
		// No room for a digest at all (or only for the marker itself): the name
		// is the field, whole.
		return truncateRunes(toolGlyph+" "+toolName, statusToolCol)
	}
	return truncateRunes(head+truncateRunes(toolSummary, room-1), statusToolCol)
}

// truncateWidth is the rune count of s after the same truncation the field above
// applies — one definition of "how wide did the name come out", so the summary's
// room cannot disagree with it.
func truncateWidth(s string) int {
	return len([]rune(s))
}

// modelShort trims the model name to the status line's budget.
func modelShort(model string) string {
	if model == "" {
		return "model?"
	}
	if len(model) > statusModelShortMax {
		return model[:statusModelShortMax]
	}
	return model
}

// turnLabel names THIS ROW's counter: the running turn while a turn is in
// flight, the completed count at the prompt.
//
// IT IS A LOCAL PER-TURN COUNTER, NOT THE SESSION'S ROUND COUNT. The row counts
// the REPL's turns — cmd/agent/cli_chat.go increments it once per turn ("one turn
// = one logical iteration regardless of internal ReAct rounds"), so its number is
// the operator's own asks as the chat surface saw them live. The session's ROUND
// count is a different quantity (one JSONL `request` row = one HTTP
// request/response, sessionevent.CounterUnitRound) and is printed by
// `gopi session log <id> --rounds`, `gopi session replay --rounds` and
// `gopi session stats`. The word on this row says TURN precisely so the two are
// never read as one number (Rule 14: one meaning per word — the operator's
// 2026-10-12 ruling made round the request row, and this label used to read
// "turn", which is the round's other spelling).
//
// THE LABEL CARRIES NO SPACE BETWEEN THE WORD AND THE NUMBER (task 01a10dd0),
// and the cell it saves is not decoration: statusTurnCol reserves the cells, and
// a spaced form leaves the number too few of them, so the first three-digit count
// the row met was cut by padStatusColumn into "turn 1…" — a truncated NUMBER,
// which is the one thing this row must not print. Unspaced, the word plus digits
// fits the reservation (see statusTurnCol's own arithmetic).
func turnLabel(turn, live int) string {
	if live > 0 {
		return fmt.Sprintf("%s%d", statusColumnNameTurn, live)
	}
	return fmt.Sprintf("%s%d", statusColumnNameTurn, turn)
}

// formatCount IS RETIRED — the row has ONE count formatter now, and this note is
// the record of why it is gone rather than deprecated.
//
// WHAT THE ROW LOOKED LIKE WITH TWO FORMATTERS. The context pair was drawn by this
// function (a k-ONLY ladder: below 1000 the integer, above it "%.1fk") while the
// cache pair beside it was drawn by formatCountBounded (the k/M/G ladder). Measured
// on a real pty at 117x26, one row, one quantity family, two unit systems:
//
//	… ·  19500.0k/200.0k≡ ·↑36M↓3M    6s · ready
//
// Three defects ride in that one string, and they are one defect:
//
//	k WHERE M APPLIES   the context side states 19500.0k for a count the reader
//	                    must convert (19.5M) and CANNOT compare with the cache pair
//	                    two columns right, which says "36M". Same family, two
//	                    scales, so the row cannot be read with one rule.
//	A FORCED TAIL       "%.1fk" prints ".0" whenever the value is a whole number of
//	                    thousands — "19500.0k", "200.0k" — spending a cell on a digit
//	                    the counter does not have (the tenth is not a measurement
//	                    here; it is the format's own constant).
//	UNBOUNDED FORM      "12345" → "12.3k" is 5 cells, but the form grows with the
//	                    input's magnitude: a 4-digit thousands value prints 6, a
//	                    million-unit value "1000.0k" prints 7. A reservation derived
//	                    from it is a statement about a sample, which is the defect
//	                    epic 01a0e2e9 removed from the cache pair on the same row.
//
// WHY DELETED RATHER THAN KEPT AND UNUSED. The cells above are drawn by the pair;
// the RULE is what the reader applies. Leaving a second formatter alive — even with
// no caller — leaves a second rule for a future field to pick up, and the row's
// whole complaint is that a reader had to re-derive the scale per field. So the
// spellings are gone and the ONE rule is the ladder's: every count on this row goes
// through formatCountBounded (statusCountUnits / statusCountSideTextWidest /
// statusCountFractionDigits, chat_status_cache_bound.go) — same ladder, so k is
// never used where M applies; same decimal policy, so no trailing ".0"; same
// significant digits, so two counts of one family are comparable at a glance.
//
// The geometry follows the RULE rather than a remembered sample: the context pair's
// per-side ceiling is now DERIVED from the formatter's own constants
// (statusCtxPairSideCellsWidest), exactly as the cache pair's is, so a form that
// outgrows its reservation cannot be written in one place and missed in the other.

// statusElapsedLabel renders a session's wall time compactly: "1h02m" past an
// hour, "12m30s" past a minute, "45s" past a second, and — below a second —
// "0.4s" in tenths. Hermes shows a duration segment and so does this row — but
// only when the source HAS a clock, because a time rendered from a zero start
// would claim a session that just began.
//
// The SUB-SECOND rung is why the scale now reaches a tenth: the label used to
// floor to whole seconds, so every turn shorter than a second — a tool round, a
// short answer, i.e. exactly the turns where the row is the only thing on screen
// that could answer "is it working?" — printed "0s". That is a number no clock
// reported, about a turn that was NOT zero, and it is the failure mode this row
// is not allowed to have. A tenth is the smallest unit that keeps the label
// inside statusLiveCol ("0.4s" is 4 cells against the 13 the block reserves,
// measured by TestStatusLadder_LiveBlockIsReserved) while still stating a
// measurement.
//
// It never negative-clamps silently: a negative duration (a clock that stepped,
// or a caller that passed its start after its now) is reported as the honest
// floor, statusElapsedZero, rather than as a nonsense like "-3s" — and rounding
// is DOWN, so the label never claims more time has passed than actually has.
func statusElapsedLabel(d time.Duration) string {
	if d <= 0 {
		return statusElapsedZero
	}
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d/time.Minute)%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d/time.Second)%60)
	case d >= time.Second:
		return fmt.Sprintf("%ds", int(d/time.Second))
	default:
		// The sub-second rung, in tenths. The floor of one tenth is deliberate:
		// a zero here would be the very number this rung exists to stop printing
		// (it can only be reached when d > 0).
		tenths := int(d / statusElapsedTenth)
		if tenths < 1 {
			return statusElapsedMin
		}
		return fmt.Sprintf("0.%ds", tenths)
	}
}

// statusElapsedMin is the smallest positive duration the sub-second rung can
// state: a measurement that rounds below a tenth is still a measurement, and it
// is reported as one instead of collapsing into the zero-looking label.
const statusElapsedMin = "0.1s"

// statusElapsedTenth is the resolution of the sub-second rung above: the label's
// unit below one second. Named (Rule 1) because the arithmetic it is entered into
// is a threshold, not a formatting detail.
const statusElapsedTenth = 100 * time.Millisecond

// statusElapsedZero is the label for a clock that reports no time elapsed (or
// went backwards). One definition: it is the honest floor of the elapsed field.
const statusElapsedZero = "0s"

// statusCompactionGlyph marks the session's COMPACTION count (Hermes' pill
// segment): how many times the context has been summarised away under the
// running session. It is a count, never an amount, so the glyph is the whole
// unit — "⧉2" and not "2 compactions", because the live block is 12 cells wide
// and measured, "⧉2 · ready" is 10 of them.
//
// THE GLYPH DOES NOT CARRY THE MEANING BY ITSELF, and that is the operator's
// bug #3 in one line: a reader meets "⧉2 · ready" on every row of a long
// session and NOTHING in the product said what "⧉" counts (measured on master
// 2044ae98: `gopi chat -h` carried ZERO occurrences of the glyph). Its meaning
// lives in the row's own help (chatUsage's THE STATUS ROW section, built from
// statusCompactionRow here) and in the field documentation above — the SAME
// arrangement the context pair's qualifier ("≡", see statusCtxQuantGlyph) has,
// and the arrangement the doctrine in internal/lineedit/answer.go requires: a
// symbol a reader cannot decode is either NAMED where the reader can see it or
// not printed. TestStatusRowSymbolsHaveAReaderVisibleDefinition is the gate for
// the whole row, so a glyph added here without a legend fails the build's tests
// rather than reaching the operator.
const statusCompactionGlyph = "⧉"

// statusCompactionLabel renders the compaction count for the live block. The
// count is shown only when it is non-zero (the caller checks), because zero
// compactions and an unknown count are different facts and neither should print
// a "0" that reads like one — the row says nothing when there is nothing to say.
func statusCompactionLabel(n int) string {
	return statusCompactionGlyph + strconv.Itoa(n)
}

// chatBeginTurn opens ONE turn's header, choosing the form by what the turn's
// input WAS.
//
// It is a function rather than an if-statement at the call site so the choice it
// makes is executable BY A TEST, on the REAL renderer and the REAL band: the pty
// case (chat_turn_spill_header_pty_test.go) drives this exact call and reads the
// header off the grid, so "a spilled turn gets the spill form" is asserted against
// the dispatch production runs rather than against a sibling of it.
//
// A spilled turn's header describes the PAYLOAD — its true size and line count,
// read back from the file, and what was pasted — instead of reprinting the
// reference the input line carries: a reader who cannot tell a spilled turn from a
// typed one has been shown a file path where the turn's question should be.
func chatBeginTurn(renderer *chatTurnRenderer, index int, out chatOutbound) {
	renderer.beginTurnOut(index, out, time.Time{}, false)
}

// chatBeginTurnAt is chatBeginTurn for the caller that KNOWS when the turn
// opened, so its band states the moment (BeginTurnAt carries the why).
func chatBeginTurnAt(renderer *chatTurnRenderer, index int, out chatOutbound, openedAt time.Time) {
	renderer.beginTurnOut(index, out, openedAt, true)
}

func (r *chatTurnRenderer) beginTurnOut(index int, out chatOutbound, openedAt time.Time, stamp bool) {
	if out.Spill.Spilled {
		r.BeginSpillTurn(index, out.Spill)
		return
	}
	r.beginTurn(index, out.Display, openedAt, stamp)
}

// chatTurnRenderer is the REPL's per-turn answer sink. It hides which of the two
// renderers is active behind one small interface, so the turn loop reads the
// same whether the terminal hosts the reserved layout or not.
type chatTurnRenderer struct {
	// region is non-nil when the reserved layout is active.
	region *lineedit.AnswerRenderer
	// buffered is the historic renderer (the fallback path).
	buffered *chatRenderer
	// out is where the buffered renderer writes.
	out io.Writer
	// pal is the run's ONE emphasis table, so a committed tool row carries the
	// same style the status row's running marker does (see ToolRow).
	pal lineedit.Palette
	// ellipsis is the RENDER-TIME backstop for the prompt's ellipsis rule
	// (chat_answer_ellipsis.go): every delta the renderer is handed is ALSO
	// observed here, and a U+2026 in the body is reported with its turn and rune
	// offset. It observes — it never edits the bytes on their way to the screen.
	ellipsis *chatAnswerEllipsisWatch
	// turn is the 1-based index of the turn being rendered, taken from BeginTurn
	// (the caller's own counter). A locatable record needs it: "an ellipsis" is
	// not actionable, "turn 7, rune offset 431" is.
	turn int
	// cutLabelBase is the renderer's running cut-label count at the moment this
	// turn BEGAN (chat_answer_ellipsis.go's attribution reads the difference), so
	// a turn reports the cells its own rows drew, never the run's total.
	cutLabelBase int
}

// noteAnswer feeds one delta to the ellipsis backstop. It is the ONLY hook, so
// the detector sees exactly what the renderer draws and nothing else (a delta
// assembled a second time from elsewhere could disagree with the painted text).
func (r *chatTurnRenderer) noteAnswer(delta string) {
	if r.ellipsis == nil {
		r.ellipsis = newChatAnswerEllipsisWatch(r.turn)
	}
	r.ellipsis.observe(delta)
}

// noteAnswerMessageBoundary tells the ellipsis watch that ONE assistant message
// of this turn has ended (AnswerBoundary). The watch keeps accumulating — the
// seam draws one answer body per TURN — but the report must be able to say how
// many wire texts that body spans, because an offset across two of them is an
// index into neither (chatAnswerEllipsisHit.Messages).
func (r *chatTurnRenderer) noteAnswerMessageBoundary() {
	if r.ellipsis == nil {
		// No text was observed yet, so there is no body an offset could be
		// measured on: creating a watch here would produce a turn whose watch
		// exists only to hold a count. The first delta creates it instead.
		return
	}
	r.ellipsis.messageBoundary()
}

// Ellipses reports the located U+2026 occurrences of the turn just rendered and
// closes the watch, so a reader (the REPL's notice seam) asks once per turn for
// a complete answer body. The bytes are already on screen by then and are
// unchanged — the return value is a RECORD, never a repair.
func (r *chatTurnRenderer) Ellipses() []chatAnswerEllipsisHit {
	if r.ellipsis == nil {
		return nil
	}
	r.ellipsis.flush()
	return r.ellipsis.hits
}

// CutLabels reports how many cut labels the RENDERER drew on this turn's rows —
// the cells that are on the screen but not in the answer's own text
// (internal/lineedit's answer_cut_labels.go).
//
// It is asked separately from Ellipses(), and by the same caller, because the two
// answer DIFFERENT questions and the attribution needs both: Ellipses() counts the
// character in the text this seam was HANDED; CutLabels() counts the character the
// seam itself DREW. A `…` on the operator's screen is one of the two, and only
// these two numbers together can say which (chat_answer_ellipsis.go).
//
// The buffered path draws no cut label (it wraps at a cell boundary and inserts no
// glyph — see chatRenderer.emit), so it answers 0: "the renderer added nothing".
func (r *chatTurnRenderer) CutLabels() int {
	if r.region == nil {
		return 0
	}
	return r.drawnCutLabels() - r.cutLabelBase
}

// drawnCutLabels is the renderer's RUNNING total of drawn cut labels — the value
// CutLabels differences against the turn's own base. Split out so the two readers
// (BeginTurn's reset and CutLabels' answer) share one derivation (Rule 14) —
// the buffered path draws no label, so it answers 0 for both.
func (r *chatTurnRenderer) drawnCutLabels() int {
	if r.region == nil {
		return 0
	}
	// The ledger's kept rows are bounded and its COUNT is not (Rule 15); the
	// report needs the count, so the rows are dropped here on purpose rather than
	// re-published under a second vocabulary.
	_, total := r.region.CutLabels()
	return total
}

// Turn is the 1-based index of the turn this renderer is drawing (BeginTurn's
// own argument). It is the fallback a report needs when the turn's answer text
// carries no character of its own: a summary line about drawn cells must still
// name WHICH turn's rows it is about (chat_answer_ellipsis.go).
func (r *chatTurnRenderer) Turn() int { return r.turn }

// newChatTurnRenderer picks the renderer for this turn: the reserved-layout one
// when the editor has an answer band, the buffered one otherwise.
//
// It takes the REPL's input layer rather than a resolved band + writer, because
// the two answers must agree: `Answer()` is the live band (nil when the reserved
// layout is not on screen) and `flowWriter()` is the historic stdout the flow
// path has always used, so a turn cannot render its answer into a band while
// writing the fallback somewhere else.
func newChatTurnRenderer(input *chatInput) *chatTurnRenderer {
	r := &chatTurnRenderer{out: input.flowWriter(), pal: input.pal}
	if band := input.Answer(); band != nil {
		r.region = band
		r.region.Begin()
		return r
	}
	r.buffered = newChatRenderer(r.out)
	// No width is read here, and there is nothing to read it into: the fallback
	// renderer emits the model's bytes on every writer and the terminal folds a
	// long row itself, at zero bytes of stream. Reading the width here is the
	// defect this path used to have — it put CR LF into copied text and moved
	// the fold whenever the window was resized.
	return r
}

// BeginTurn opens one turn's chrome: the turn header is committed into the
// answer band, and the turn's body lines are marked so the block has a left
// edge. It replaces the bare Begin on the reserved path; on the cooked path
// there is no band to frame and the turn adds NO bytes at all — that path's
// contract is its byte stream (the pre-A5 output scripts and tests depend on),
// and its turn separation stays the trailing newline the loop already writes.
func (r *chatTurnRenderer) BeginTurn(index int, prompt string) {
	r.beginTurn(index, prompt, time.Time{}, false)
}

// BeginTurnAt begins a turn whose OPENING ROW MOMENT is known, so the band states
// when the turn began.
//
// IT IS A SECOND ENTRY POINT, NOT A WIDER SIGNATURE, because "this header carries
// a moment" is a different claim from "this header has an ordinal and a prompt"
// (lineedit.TurnInfo.Stamp says the same thing one layer down): a caller with no
// record behind it — a probe, a header-geometry test — keeps the two-argument
// form and gets the bytes it always got, instead of being forced to invent a
// time it does not have.
func (r *chatTurnRenderer) BeginTurnAt(index int, prompt string, openedAt time.Time) {
	r.beginTurn(index, prompt, openedAt, true)
}

func (r *chatTurnRenderer) beginTurn(index int, prompt string, openedAt time.Time, stamp bool) {
	// The turn index is recorded on BOTH paths: it is the locator in an ellipsis
	// report, and a report without it cannot be traced back to a turn (a run's
	// answer bodies are otherwise indistinguishable).
	r.turn = index
	// The cut-label count is read at a turn BOUNDARY, so the boundary is where it
	// starts over: a turn's report must count the cells ITS OWN rows drew, never
	// the run's running total (chat_stream.go CutLabels).
	r.cutLabelBase = r.drawnCutLabels()
	if r.region != nil {
		// THE MOMENT THE TURN OPENED rides the SAME struct as the ordinal and
		// the prompt. stamp is a SEPARATE flag from openedAt because "this
		// header carries a moment" is a different claim from "the ordinal and
		// the prompt are known": a caller with no record behind it (a probe, a
		// header-geometry test) must keep the bytes it always got rather than
		// printing a placeholder for a turn nobody recorded. A stamping caller
		// that has a zero openedAt prints the shared placeholder, so the band
		// and the web banner show the same three fields on every recorded turn.
		r.region.BeginTurn(lineedit.TurnInfo{Index: index, Prompt: prompt, OpenedAt: openedAt, Stamp: stamp})
	}
}

// BeginSpillTurn begins a turn whose input was a PASTE SPILL. The header it draws
// is a DESCRIPTION of the payload — size in bytes and runes, the payload's own
// line count, the marked count of lines left unshown, and the handle on the file
// — rather than a reprint of the reference that stands in for the prompt.
//
// It is a sibling of BeginTurn rather than a mode on it because the two forms
// differ in what they take: an ordinary turn has the operator's words as typed,
// and a spilled one has a file (and no words at all). Keeping them separate is
// also what guarantees the ordinary header's bytes are unchanged by this change.
func (r *chatTurnRenderer) BeginSpillTurn(index int, spill lineedit.TurnSpill) {
	r.turn = index
	r.cutLabelBase = r.drawnCutLabels()
	if r.region != nil {
		r.region.BeginTurn(lineedit.TurnInfo{Index: index, Spill: spill})
	}
}

// EndTurn closes a turn: the closing rule is committed into the band, so the
// next turn's header never lands flush against this turn's last answer line. It
// must be called AFTER Finish (the tail has to be committed first, or the rule
// would land above it).
func (r *chatTurnRenderer) EndTurn() {
	if r.region != nil {
		r.region.EndTurn()
	}
}

// Write feeds one streamed fragment of the ANSWER. The reasoning block, if one
// is open, is closed first: that is the spacing rule between thinking and answer
// — the reasoning's line is committed and one blank row separates it from the
// answer, so the two are never run together on one line.
func (r *chatTurnRenderer) Write(delta string) {
	// THE BACKSTOP RUNS HERE, on the exact bytes about to be drawn and BEFORE
	// either renderer sees them: one seam, so the two paths cannot observe
	// different text (chat_answer_ellipsis.go).
	r.noteAnswer(delta)
	if r.region != nil {
		r.region.EndThinking()
		r.region.Delta(strings.TrimRight(delta, "\r"))
		return
	}
	r.buffered.write(delta)
}

// Thinking feeds one streamed fragment of the model's REASONING. Inside the band
// the reasoning is committed under its own gutter, so the user can tell working
// from answer at a glance; on the cooked path the reasoning is drawn by the
// agent's own stderr markers (this callback is not installed there), so there is
// nothing to do.
func (r *chatTurnRenderer) Thinking(delta string) {
	if r.region == nil {
		return
	}
	r.region.BeginThinking()
	r.region.Delta(strings.TrimRight(delta, "\r"))
}

// AnswerBoundary announces that one ASSISTANT MESSAGE of the turn has ended (the
// agent's own OnMessageEnd seam): with tools, a turn carries several, and the
// band's format classifier must not carry a block those messages left open into
// the next one. On the cooked path there is no band and no classifier to reset —
// the path's byte contract is its stream (the pre-A5 output scripts depend on it),
// so this adds no byte at all.
func (r *chatTurnRenderer) AnswerBoundary() {
	// THE ELLIPSIS WATCH IS TOLD TOO (chat_answer_ellipsis.go): a boundary is the
	// one place the seam learns that this turn's answer body is being fed by
	// ANOTHER assistant message, and the report has to say so — an offset measured
	// across two messages is not an offset into either one of them.
	r.noteAnswerMessageBoundary()
	if r.region != nil {
		r.region.AnswerBoundary()
	}
}

// Finish ends the turn so the tail of the answer is never dropped.
func (r *chatTurnRenderer) Finish() {
	if r.region != nil {
		r.region.Finish()
		return
	}
	r.buffered.flush()
}

// ToolRow commits one durable tool-activity row into the answer band: the turn's
// record of the call, drawn from the SAME line text (termToolLine) and the same
// palette field the status row's running marker uses, so the transient hint and
// the committed row cannot describe one call differently.
//
// The reserved path is the ONLY one that gets it. The cooked path's contract is
// its byte stream: a row here would be a new byte in the output piped scripts
// capture, and the historic flow output on that path already carries the tool
// activity in its own form (cmd/agent/chat_once.go's own line sink).
func (r *chatTurnRenderer) ToolRow(line string) {
	if r.region == nil {
		return
	}
	r.region.Note(strings.TrimRight(line, "\r\n"), r.pal.ToolCall)
}

// Reserved reports whether the scrollback-safe path is active for this turn (the
// turn loop uses it to decide whether a trailing newline is needed).
func (r *chatTurnRenderer) Reserved() bool { return r.region != nil }

// chatRenderer buffers streamed text deltas and emits them in human-friendly
// chunks: whole lines immediately (a newline is a natural reading pause), and
// whole words once their flush window elapses or the buffer fills. Result:
// the answer appears in smooth, readable bursts rather than raw fragments.
//
// This is the FALLBACK path (A5 keeps it for every non-reserved terminal), and
// its byte contract is the SAME ON EVERY WRITER: the deltas reach the writer
// exactly as the model sent them — on a pipe, a file, a test buffer AND on the
// terminal itself, because the terminal folds a long row at zero bytes of stream.
// There is no width in this renderer and no field to hold one (emit states why).
type chatRenderer struct {
	out       io.Writer
	buf       strings.Builder
	lastFlush time.Time // last time a word was flushed (for pop-in cadence)
}

// newChatRenderer builds a renderer writing to out. It has no width and no way
// to be given one: a renderer that bounded its rows would have to write the row
// breaks, and those bytes are the defect (emit states the whole rule).
func newChatRenderer(out io.Writer) *chatRenderer {
	return &chatRenderer{out: out, lastFlush: time.Now()}
}

// write accumulates one text delta and flushes any complete reading unit.
func (r *chatRenderer) write(delta string) {
	r.buf.WriteString(delta)
	s := r.buf.String()

	// Flush up to the last newline immediately: a line break is a natural
	// pause, and the mid-line remainder continues to buffer.
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		r.emit(s[:idx+1])
		r.buf.Reset()
		r.buf.WriteString(s[idx+1:])
		r.lastFlush = time.Now()
		return
	}

	// Word boundary flush: if the buffer ends in whitespace AND the word's
	// flush window elapsed, emit the completed words (pop-in cadence).
	if strings.HasSuffix(s, " ") && time.Since(r.lastFlush) >= chatWordFlushWindow {
		r.emit(s)
		r.buf.Reset()
		r.lastFlush = time.Now()
		return
	}

	// Buffer-size flush: a very long uninterrupted answer must keep visible
	// progress even with no word boundary handy.
	if r.buf.Len() >= chatMaxBufferedBytes {
		r.emit(s)
		r.buf.Reset()
		r.lastFlush = time.Now()
	}
}

// emit writes one complete reading unit to the writer — the single write point
// of this renderer, and the place where the answer's byte stream is decided.
//
// IT WRITES THE MODEL'S BYTES AND NOTHING ELSE, ON EVERY WRITER. That is the
// whole of it: the answer stream has no line break of its own except the ones
// the answer itself carries, so a width never enters these bytes and the same
// answer copies out the same way on an 80-column terminal and a 200-column one.
//
// A terminal folds a row the application never ended, and that fold is the
// terminal's ink, not ours: it costs zero bytes of the stream. Writing the fold
// instead — which this renderer did until the operator ruled it out — put
// CR LF into the middle of copied text (a long file path arrived split, a pasted
// command carried a return the shell read as ENTER), and because the width was
// re-read once per turn the FOLD MOVED with the window, so the same answer
// copied differently at two widths. devdoc/chat-stream-no-app-wrap.md carries
// the measurement.
//
// The visual result is unchanged: the reader still sees a row and its
// continuation, because that is what the terminal does with a long row. What
// changed is who owns the break — and only the terminal's breaks are free of
// bytes. Non-TTY streams are byte-identical to the historic renderer BY
// CONSTRUCTION, because there is now one path and it was already the frozen one.
func (r *chatRenderer) emit(text string) {
	if text == "" {
		return
	}
	fmt.Fprint(r.out, text)
}

// flush emits any remaining buffered text (called once at the end of a turn
// so the tail of the answer is never dropped).
func (r *chatRenderer) flush() {
	if r.buf.Len() > 0 {
		r.emit(r.buf.String())
		r.buf.Reset()
	}
}

// There is no second constant here, and its absence is the point: a break the
// WIDTH would have written has no name because it does not exist. A reader
// searching for "where do this stream's line breaks come from" gets one answer —
// from the model, and nowhere else.
