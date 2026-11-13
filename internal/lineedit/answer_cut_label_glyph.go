package lineedit

// answer_cut_label_glyph.go — THE ANSWER BAND'S OWN CUT GLYPH, and why it is not
// the status row's ellipsis.
//
// THE MEASURED MISREADING THIS EXISTS FOR (task 01a1a325, the operator's own
// investigation). The band's right-edge cut used to spend the row's last cell on
// `…` — the SAME glyph the status row uses for a field whose tail was DROPPED
// (fitField), and the glyph a reader has learned, everywhere else in this
// product and in every editor they use, to mean "content was omitted here". The
// two facts the mark stands for are OPPOSITE:
//
//	status row (`…`)      the tail is GONE — a status field is one screen row
//	                      and its overflow is discarded (fitField).
//	answer band (`…`)     the tail is on the ROW BELOW, one character displaced
//	                      onto that row's first column. NOTHING is missing.
//
// So the operator, reading `only w…` above `here the TEMPLATE`, looked for the
// text the ellipsis claimed was omitted, compared the screen against raw.log,
// found no difference at all, and filed it as "the renderer altered the model's
// own words". They were right to: a glyph that means "omitted" was standing where
// nothing had been omitted. `…` also carries East Asian Width AMBIGUOUS (A), so in
// a CJK environment it can spend TWO cells — a second reason the band, whose
// arithmetic is cell arithmetic, may not wear it (see runeCells/isWideRune).
//
// THE GLYPH. `↵` (U+21B5 DOWNWARDS ARROW WITH CORNER LEFTWARDS) is the carriage-
// return/enter sign: it says "the line continues BELOW", which is exactly what
// the band did, and it says nothing about omission. Measured properties, all
// three of them requirements rather than preferences:
//
//   - one display cell in EVERY environment: East Asian Width is N (neutral),
//     not A, so runeCells answers 1 whatever internal/config's ambiguous-width
//     knob says — the band's width arithmetic is cell arithmetic and a label that
//     changed width with the locale would break every row it is drawn on;
//   - it is NOT the status row's elision idiom, so the two vocabularies stop
//     sharing a spelling whose meanings are opposite;
//   - it is present in the monospace fonts this product's terminals use
//     (measured on this host: DejaVu Sans Mono and FreeMono both carry U+21B5).
//
// WHY THE FIX IS A CONSTANT AND NOT A DELETION. `answer_cut_labels.go` records
// every label the renderer DRAWS (`writeCutLabel` → add(turn,row)), and
// cmd/agent/chat_answer_ellipsis.go uses that record to tell a character the
// MODEL wrote from a cell the RENDERER drew. The record's semantics are exactly
// what this file leaves alone: the same call, at the same place, still records
// turn + row + total. Only the glyph changes, so the record keeps answering "the
// renderer drew something here the answer's own text does not contain" — and the
// report's wording is corrected separately (chat_answer_ellipsis.go), because a
// sentence that called those drawn cells "U+2026" would now be false.
//
// WHAT IT IS NOT. It is not NoteContinuation (`↳ `, two cells, a note row's
// gutter) — a different path with a different contract — and it is not
// statusEllipsis/NoteCutMarker/StatusEllipsis, which keep their meaning (a field
// whose tail was dropped, and the note legend's reference to that meaning). The
// status row's truncation vocabulary and the note rows' are out of this file's
// scope on purpose: only the ANSWER band's cut label changes here.

// answerCutLabel is the glyph the answer band draws in a row's last cell when the
// row's text was cut between two word characters and continues on the row below.
// It is one display cell by construction (see this file's head) and it NEVER
// enters the row's source text: writeCutLabel draws it into the cell and liveText
// stays the answer's own characters, so the emphasis span walk (row.skip) and
// every committed-row comparison keep their byte arithmetic.
const answerCutLabel = "↵"

// AnswerCutLabel is answerCutLabel for callers OUTSIDE this package — the chat
// face's grid readers, which fold the band's layout glyphs out before comparing
// the screen against the answer's text. They must read the glyph from the one
// place that draws it rather than spelling "↵" again (Rule 14: a second spelling
// is a second definition, and the one that drifts is the one nobody re-read).
const AnswerCutLabel = answerCutLabel
