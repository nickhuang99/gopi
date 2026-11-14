package main

// chat_answer_ellipsis.go — THE RENDER-TIME BACKSTOP FOR THE ELLIPSIS RULE.
//
// THE MEASURED HISTORY, so a later reader does not delete this as noise. The
// chat system prompt already forbids eliding an answer body — the
// `[CHAT REPLY GLYPHS — screen cells are the deliverable]` paragraph
// (chat_reply_glyphs.go), mounted on both chat paths by chatContextRulesFor. It
// shipped on 2026-10-14 (commit 82605ae57, "a reply body may not be elided with
// an ellipsis (the screen cell is the deliverable)"). Measured in the session
// that filed THIS work (session 8616b46c, artifacts/bug-ellipsis-rule-not-
// enforced.md): the marker was in the system prompt of 43 of 43 requests, and
// the model still emitted U+2026 in the answer body — the raw.log `assistant`
// content for that round is `...`COMMANDS.…\n\nmd` 被拆开...`, i.e. the glyph
// came out of the model, after `COMMANDS.`, with the paragraph sitting in the
// very same request.
//
// WHY A PROMPT CANNOT BE THE WHOLE ANSWER: the paragraph is an INSTRUCTION to a
// probabilistic writer about a PER-CHARACTER property of its output. Instruction
// adherence is statistical over characters, so the requirement is lost at the
// rate the model slips, and the slip is invisible to it (nothing in the model's
// own loop measures its text against the rule). A prohibition that is checked by
// nobody who can stop the bytes is a preference, not a constraint.
//
// SO THE CHECK MOVES INTO THE RENDER PATH, and its contract is REPORT ONLY:
//
//	the bytes the renderer is handed are the bytes drawn — the detector NEVER
//	substitutes, rewrites, truncates or filters. A substitution would make the
//	artifact disagree with the model's own record and with the session JSONL,
//	and the disagreement would be undiagnosable (Rule 15: report, never silent).
//
// WHAT IT REPORTS: which TURN, the RUNE OFFSET inside that turn's answer body,
// a bounded excerpt, and — the correction this revision is about — WHICH COPY
// the character is in. The report is drawn on the same channel the REPL's other
// notices use (chatInput.WriteNotice), so it lands on the answer band in front of
// an operator and on stderr for a scripted run; it is not a log line nobody reads.
//
// THE MISATTRIBUTION THIS FILE WAS REWRITTEN TO REMOVE (task 01a1603f, measured).
// The first revision reported a hit as "the body was NOT altered; the prompt
// forbids it" — i.e. it asserted the MODEL violated the rule, on the strength of
// a character it had seen in the rendered body. That reading is wrong for one of
// the two copies by construction: the band's right-edge cut DRAWS the product's
// cut label `…` into a row's last cell while leaving the answer's own text
// untouched (internal/lineedit/answer.go cutLabelFor/writeCutLabel, recorded by
// internal/lineedit/answer_cut_labels.go). The drawn row and the wire text are
// therefore two copies that legitimately disagree, and a `…` in the drawn row
// can be EITHER the model's text OR that label.
//
//	Report it as "rendered U+2026" — the fact — and answer the next question
//	explicitly: does this location ALSO exist in the model's output text?
//
//	- in the text this seam was handed → model output; the wire copy carries it.
//	- not in that text, with cut labels recorded for the turn → renderer cut
//	  label; a DISPLAY-LAYER difference, NOT a model violation.
//	- not in that text, with NO cut labels recorded → unattributable. The seam
//	  cannot see the wire copy (that is the transport's record), so it says so
//	  instead of assuming. An honest "cannot attribute" is the fix; the old
//	  wording's "the prompt forbids it" was the defect.
//
// WHAT IT DELIBERATELY DOES NOT DETECT: the ASCII spelling `...`. It is not a
// // forbidden character but an ordinary token — `go test ./...`, `args...`, an
// ASCII arrow in a diagram — and a detector that fired on every one of those
// would be the console noise that gets a real notice ignored. U+2026 is the
// character measured leaking, and it has no legitimate use in an answer body.
//
// RULE 3/35: no new tool, no new config key, no new dependency, no state file.
// The detector lives in the render path and its constants live here.

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/nick/gopi/internal/lineedit"
)

const (
	// chatAnswerEllipsisRune is the character this file reports wherever it
	// appears in the RENDERED body — whether the answer wrote it or the renderer
	// drew it (Rule 1/13: the one value the whole mechanism compares against).
	chatAnswerEllipsisRune = '…'

	// chatAnswerEllipsisReportPrefix opens every report line. It names the
	// artifact (the RENDERED answer body) and the finding, and deliberately NOT a
	// verdict: the previous wording said "the prompt forbids it", which attributed
	// to the model a character the render path itself draws (see the header).
	chatAnswerEllipsisReportPrefix = "[gopi] chat answer body: rendered U+2026"

	// chatAnswerEllipsisExcerptRunes is how much of the body around the hit is
	// quoted, per side. Bounded so a report stays ONE line however long the
	// paragraph is (a report a terminal wraps is a report a reader skims past).
	chatAnswerEllipsisExcerptRunes = 16

	// chatAnswerEllipsisExcerptOpen/Close mark where the excerpt was cut, so a
	// reader can tell a quote from the whole sentence.
	chatAnswerEllipsisExcerptOpen  = "«"
	chatAnswerEllipsisExcerptClose = "»"
)

// THE REPORTING BUDGET — one aggregated page per turn, at most.
//
// THE MEASUREMENT THIS BUDGET REPLACES (2026-10-22, the epic that produced it).
// The previous report wrote ONE LINE PER HIT, capped at eight, plus a remainder
// line and (on the display-layer path) a summary line — nine lines for the round
// the operator pasted into the ledger, which pushed the answer off the console
// completely. The defect the guard exists to report is ONE defect per turn, and a
// guard that buries the answer to say so has traded one unreadable artifact for
// another. So the per-turn budget is now:
//
//	severity HIGH (a pasted-unit token was elided)  ≤ chatAnswerEllipsisTruncatedLinesPerTurn line(s)
//	severity LOW  (prose — nothing was cut)            0 lines, and no line at all
//
// The LOW budget is ZERO LINES, and that absence is a decision rather than an
// omission. It used to be read as "no DETAIL line, but the summary still prints",
// and the summary is a line: measured 2026-11-09 (this epic's own report), a turn
// whose only characters were rhetorical ellipses printed
//
//	[gopi] chat answer body: rendered U+2026 — turn 17: 2 hit(s): 0 HIGH …, 2 LOW prose …
//
// on every turn that wrote one — no switch, no silence condition — which is the
// console noise that gets a real notice skimmed past. So the criterion is now
// stated as ONE predicate (reportChatAnswerEllipses): a turn is reported only
// when it carries a pasted unit that was actually cut (HIGH), a token whose shape
// the seam cannot decide (the body ended inside it), or a cut label the RENDERER
// drew (alone: a display-layer fact; beside a hit: the turn is unattributable).
// Everything else — every prose ellipsis — is recorded on the hits and prints
// NOTHING.
//
// Why that is not a loss (Rule 15). The detector still records every hit with its
// turn, offset, body length and classification, so the record survives in the
// turn's hits for any caller that asks (`chatTurnRenderer.Ellipses`), a test can
// assert on it, and a reader who wants the count can be given it by whatever owns
// that question. What this seam does not do is spend the OPERATOR'S console — the
// band the answer itself is drawn on — on a character that cost nothing to read
// past. A later change that wants a line back must add a NAMED constant for it
// (Rule 1), not an unnamed loop, and not a comment claiming a constant that no
// longer exists (Rule 13: a constant is referenced, or it does not exist).
//
// Where a line IS written, every count that did not get its own line is stated as
// a NUMBER in the summary — aggregated, never dropped (Rule 15: a report may
// compress, it may not lose). The cap on REPORTED tokens is separate from the
// count of HITS: the detector always records every hit.
const (
	// chatAnswerEllipsisTruncatedLinesPerTurn is how many ELIDED-UNIT findings get
	// their own line per turn: exactly one, and the summary line carries the count
	// of the rest. The epic's phrasing is the budget — one main line plus a count
	// line — and a second example line would be the start of the noise again.
	chatAnswerEllipsisTruncatedLinesPerTurn = 1

	// chatAnswerEllipsisUnitScanRunes bounds how far the token under the ellipsis
	// is followed on either side when deciding whether it is a pasted unit. A
	// token longer than this is still reported, its length stated rather than its
	// whole text (a 400-rune token would otherwise be the whole report).
	chatAnswerEllipsisUnitScanRunes = 64
)

// THE SHAPES OF A PASTED UNIT — the criteria that separate a defect the operator
// CANNOT work around from one they can read past.
//
// An answer body may legitimately carry a rhetorical ellipsis: "the net is slow
// … anyway", "before … after". Nothing was lost there — the reader gets the
// sentence, and nothing had to be pasted. But `task/01a16033-dd15-7ba0-…` and
// `/home/user/workspace/01a15bb9-…-r1` are NOT prose: they are identifiers whose
// exact bytes are the whole point, and an ellipsis inside one produces a token
// that no shell will accept and no reader can complete. That difference — not
// the presence of the character — is what the escalation now keys on.
//
// The classification reads the TOKEN the ellipsis sits in (the maximal run of
// non-space runes around it) and asks whether its shape is one an operator is
// expected to paste. The shapes are constants (Rule 1/13) so the vocabulary has
// one definition site and a caller cannot invent a fifth kind.
const (
	// chatAnswerEllipsisUnitPath — an absolute or home-relative path, or a
	// relative path that still looks like one (`a/b`, `./x`). The operator pastes
	// these into a shell; a cut inside one is unusable.
	chatAnswerEllipsisUnitPath = "path"

	// chatAnswerEllipsisUnitBranch — a git ref shape: it contains `/` and every
	// segment is a ref-name character run (`task/<id>`, `feat/<name>`), or it is
	// prefixed by the ref vocabulary itself.
	chatAnswerEllipsisUnitBranch = "branch"

	// chatAnswerEllipsisUnitCommand — the token is a program name followed by a
	// flag or an argument run (`gopi`, `go`, `make`, `--flag=value`): wrapping is
	// not involved, the SHAPE is executable text.
	chatAnswerEllipsisUnitCommand = "command/flag"

	// chatAnswerEllipsisUnitURL — `scheme://…`.
	chatAnswerEllipsisUnitURL = "URL"

	// chatAnswerEllipsisUnitIdentifier — a long opaque id: a 32+ hex rune run (a
	// task id, a content hash), short-hash style, or a code span written in
	// backticks. These are the tokens whose ONLY value is exactness.
	chatAnswerEllipsisUnitIdentifier = "identifier"
)

const (
	// chatAnswerEllipsisUnitMinHexRunes is the shortest hex rune run treated as an
	// opaque id. A 7-rune git short hash is the shortest thing one pastes and
	// expects to resolve; anything shorter reads as a word.
	chatAnswerEllipsisUnitMinHexRunes = 7

	// chatAnswerEllipsisUnitMinPathSlashes / ...PathMinRunes — a path or ref
	// needs at least one separator and enough runes to be worth completing.
	chatAnswerEllipsisUnitMinPathSlashes = 1
	chatAnswerEllipsisUnitMinPathRunes   = 4

	// chatAnswerEllipsisUnitWrappers are the characters a token is quoted with in
	// prose. They are stripped before classification so "`cmd/agent/x.go`" is
	// judged as the path it is, and re-attached only in the excerpt.
	chatAnswerEllipsisUnitWrappers = "`\"'()[]{}<>*,;:!?"

	// chatAnswerEllipsisRefPrefixes are the segments that make a slash-joined
	// token a GIT REF specifically rather than a filesystem path. Both are pasted
	// the same way; the distinction exists because the word printed with the
	// finding should be the thing the reader has to complete.
	chatAnswerEllipsisRefTaskPrefix = "task/"
	chatAnswerEllipsisRefFeatPrefix = "feat/"
	chatAnswerEllipsisRefFixPrefix  = "fix/"
	chatAnswerEllipsisRefChatPrefix = "chat/"
	// chatAnswerEllipsisRefDisjoint is the ref segment separator.
	chatAnswerEllipsisRefSeparator = '/'

	// chatAnswerEllipsisURLSeparator is what makes a token a URL rather than a
	// path with a colon in it.
	chatAnswerEllipsisURLAutority = "://"

	// chatAnswerEllipsisFlagPrefix opens a command flag, the shape that is
	// executable text even when it is a single token.
	chatAnswerEllipsisFlagPrefix = "--"

	// chatAnswerEllipsisWordSeparators are the runes that END a token: whitespace
	// and the punctuation prose puts BETWEEN tokens. They are what the excerpt
	// reader walks out to, so the unit is the maximal separator-free run.
	chatAnswerEllipsisWordSeparators = " \t\n\r\f\v"

	// chatAnswerEllipsisUnitMaxRune / ...UnitMinRune bound the CHARACTER CLASS a
	// pasted unit is made of: ASCII printable runes, plus U+2026 itself (it is
	// often the cut, and the shape must still classify). Any rune outside that
	// class — CJK prose, a full-width comma — ENDS the token.
	//
	// The measured reason this bound exists (2026-10-22, the operator's own turn-5
	// body): written Chinese has no spaces, so a token walker bounded only by
	// whitespace returned `origin/task/01a15b2c-…`。这些分支是在` — the identifier
	// the finding is about, fused with the sentence after it. The finding would
	// then quote a token nobody could complete, which is precisely the defect this
	// report exists to name.
	chatAnswerEllipsisUnitMinRune = ' ' + 1 // '!'
	chatAnswerEllipsisUnitMaxRune = '~'
)

// THE TWO SEVERITIES. The epic's criterion, spelled as data: a hit is SEVERE when
// the ellipsis falls inside a pasted unit (the operator cannot use what they were
// given), and ORDINARY otherwise (they can read past it).
const (
	// chatAnswerEllipsisSeverityTruncated — the ellipsis is INSIDE a pasted unit.
	// This is the class that gets its own line, because it names something the
	// operator has to go and find.
	chatAnswerEllipsisSeverityTruncated = "HIGH"
	// chatAnswerEllipsisSeverityOrdinary — a prose ellipsis. Reported as a count.
	chatAnswerEllipsisSeverityOrdinary = "LOW"
)

const (
	// chatAnswerEllipsisTruncatedWord names the finding on a severe line.
	chatAnswerEllipsisTruncatedWord = "truncated a pasted unit"
	// chatAnswerEllipsisOrdinaryWord names the low-severity class in the summary.
	chatAnswerEllipsisOrdinaryWord = "prose (no pasted unit cut)"
	// chatAnswerEllipsisUnknownUnit is what a severe line prints when the token
	// cannot be recovered at all (the body ended inside it, so there is no shape
	// to report). It names the gap instead of printing an empty quote.
	chatAnswerEllipsisUnknownUnit = "a token this seam cannot recover (the body ends inside it)"

	// chatAnswerEllipsisPartialTailMax is the longest incomplete UTF-8 sequence
	// this detector may hold back between two deltas: a rune is at most
	// utf8.UTFMax bytes, so at most UTFMax-1 bytes of one can be undecided.
	chatAnswerEllipsisPartialTailMax = utf8.UTFMax - 1
)

// chatAnswerEllipsisHit is ONE located occurrence: the turn, the rune offset of
// the character inside that turn's answer body, and the bounded excerpt around
// it. It is a record, not an instruction — nothing in this package acts on it.
type chatAnswerEllipsisHit struct {
	Turn    int
	Offset  int    // rune offset in the turn's answer body, 0-based
	Excerpt string // one line, bounded, with the character shown as `…`
	// BodyRunes is the length, in runes, of the WHOLE answer body Offset is an
	// index into, measured when the report was taken. It is the anchor that makes
	// the offset checkable from outside this process, and it exists because a turn
	// is NOT one message: with tools a turn carries several ReAct rounds, and this
	// seam is handed all of their text as one stream (cli_chat.go's OnText) while a
	// reader comparing against the wire has only the LAST round in front of them.
	// Measured (session b9936006, 2026-10-22): turn 8 = a 19-rune round + a
	// 1355-rune round, so the guard's offsets ran exactly 19 ahead of the same
	// character's index in the last round's text — a correct offset against a
	// basis the report never named. "rune offset 543 of 1374" is refutable;
	// "rune offset 543" invites a comparison with the wrong body.
	BodyRunes int
	// Messages is how many assistant messages this turn streamed into the body
	// above. One is the single-round case; more than one is the case a reader must
	// know about before measuring an offset against "the" wire text, because with
	// several messages there is no single wire text at all.
	Messages int
	// Source is which COPY the character is in — the model's streamed text or the
	// renderer's drawn cells. It is filled per hit by the watch (answer) and by
	// the report's turn-level attribution (renderer / unattributable).
	Source chatAnswerEllipsisSource

	// byteAt is where in the accumulated body this hit sits. The shapes below are
	// computed from it AT THE TURN'S END (flush), not at detection: the token the
	// ellipsis cuts is very often still streaming when the character arrives —
	// `task/01a16033-…` reaches the seam one fragment before `-r1` — so a shape
	// read at detection time would describe half a token.
	byteAt int

	// Unit / UnitKind / Severity are the turn-end reading of that token, filled by
	// the watch's flush (chatAnswerEllipsisClassify): the KIND of pasted unit the
	// ellipsis cut, the token's own text as it stands, and which of the two
	// severities the report puts it in. Unit is empty and Severity ordinary when
	// no pasted unit was cut.
	Unit     string
	UnitKind string
	Severity string

	// unitKnown distinguishes "the token is an ordinary prose one" (unitKnown and
	// Unit empty: the ellipsis stood alone) from "the body ended inside the token
	// so its shape cannot be read" (not unitKnown). The two print differently.
	unitKnown bool
}

// chatAnswerEllipsisSource says WHERE the character under a report came from.
// It is the whole point of this file's second revision: the body the renderer is
// handed and the row the operator sees are TWO copies, and a `…` in the drawn
// row may belong to the model's text or to the band's right-edge cut label
// (internal/lineedit/answer_cut_labels.go). Reporting the first as the second is
// what cost a real investigation its first day.
type chatAnswerEllipsisSource int

const (
	// chatAnswerEllipsisSourceAnswer — the character is IN the model's streamed
	// text, at this rune offset. The wire copy carries it too. This is the one
	// shape in which "the model wrote it" is a fact rather than a guess.
	chatAnswerEllipsisSourceAnswer chatAnswerEllipsisSource = iota

	// chatAnswerEllipsisSourceRenderer — the character is NOT in the text this
	// seam was handed for that location: it is a cell the RENDERER drew on a row
	// it had to shorten (the cut label). The wire copy does not carry it; the
	// display does. A display-layer difference, not a model violation.
	chatAnswerEllipsisSourceRenderer

	// chatAnswerEllipsisSourceUnattributable — the body carries U+2026 and this
	// renderer recorded NO cut labels, so the watch's observation and the paint
	// disagree: the report cannot say which copy the character is in. Reported as
	// unattributable rather than assumed to be the model's, because an assumption
	// is what the old wording made (Rule 15: an unknown is named, never filled in).
	chatAnswerEllipsisSourceUnattributable
)

// String is the word the report line prints for a source. It is a method so the
// three spellings live with the three values (Rule 14) and a caller cannot invent
// a fourth vocabulary for the same facts.
func (s chatAnswerEllipsisSource) String() string {
	switch s {
	case chatAnswerEllipsisSourceAnswer:
		return chatAnswerEllipsisSourceAnswerWord
	case chatAnswerEllipsisSourceRenderer:
		return chatAnswerEllipsisSourceRendererWord
	default:
		return chatAnswerEllipsisSourceUnattributableWord
	}
}

const (
	// chatAnswerEllipsisBannedVerdict is the OLD line's closing clause, spelled
	// once here so a test can assert its absence (Rule 1/13). It is the wording
	// that turned a display fact into a verdict on the model — see the header.
	chatAnswerEllipsisBannedVerdict = "the prompt forbids it"

	// chatAnswerEllipsisSourceAnswerWord — the wire copy carries the character.
	chatAnswerEllipsisSourceAnswerWord = "model output (the wire copy carries it)"
	// chatAnswerEllipsisSourceRendererWord — the wire copy does not; the display does.
	chatAnswerEllipsisSourceRendererWord = "renderer cut label (DISPLAY-LAYER ONLY — the wire copy does NOT carry it)"
	// chatAnswerEllipsisSourceUnattributableWord — cannot be decided from here.
	chatAnswerEllipsisSourceUnattributableWord = "unattributable (no cut label was recorded; the wire copy was not read)"
)

// chatAnswerEllipsisWatch accumulates one TURN's answer body as the renderer is
// handed it, and records every U+2026 it sees.
//
// It accumulates because a locatable record needs the bytes AROUND the hit (the
// excerpt) and because an offset must count from the start of the turn, not from
// the start of a delta. It scans INCREMENTALLY from the last scanned byte so the
// cost stays linear in the body, and it HOLDS an incomplete trailing rune
// between deltas: a stream may split U+2026 across two fragments, and a detector
// that decoded each delta alone would miss exactly the case that matters — the
// character is three bytes, and one byte per fragment is the cadence the chat
// path is measured at.
type chatAnswerEllipsisWatch struct {
	turn    int
	body    []byte
	scanned int // body[:scanned] has been scanned
	runeOff int // rune offset of body[scanned]
	hits    []chatAnswerEllipsisHit
	// messages counts the assistant messages that streamed text into body, and
	// pendingMessage latches that the NEXT text belongs to a new one — so an empty
	// message contributes nothing while a boundary that arrives before its first
	// delta still opens a new one (see chatAnswerEllipsisHit.Messages).
	messages       int
	pendingMessage bool
}

// messageBoundary records that ONE assistant message just ended. The next delta
// belongs to the next message, so the count moves when that text actually
// arrives — a message that streamed nothing is not a message this body spans. The
// text itself is untouched: the seam draws one continuous answer for the turn, and
// a boundary is a fact ABOUT that answer, not a cut in it.
func (w *chatAnswerEllipsisWatch) messageBoundary() {
	if w == nil || w.pendingMessage {
		return
	}
	w.pendingMessage = true
}

// newChatAnswerEllipsisWatch starts a watch for one turn.
func newChatAnswerEllipsisWatch(turn int) *chatAnswerEllipsisWatch {
	return &chatAnswerEllipsisWatch{turn: turn}
}

// observe takes one streamed fragment of the answer. It is nil-safe so a caller
// that built a renderer by hand cannot panic on the detector.
func (w *chatAnswerEllipsisWatch) observe(delta string) {
	if w == nil || delta == "" {
		return
	}
	if w.messages == 0 || w.pendingMessage {
		w.messages++
		w.pendingMessage = false
	}
	w.body = append(w.body, delta...)
	w.scan(len(w.body) - chatAnswerEllipsisPartialTail(w.body))
}

// flush scans whatever is left, including a trailing sequence that never became
// a complete rune (a stream can end mid-character), and then reads each hit's
// TOKEN SHAPE against the completed body. It is called at the turn's end so
// nothing is left unscanned and no shape is read from a half-streamed token
// (Rule 15: a detector that stops early is a detector that lies).
func (w *chatAnswerEllipsisWatch) flush() {
	if w == nil {
		return
	}
	w.scan(len(w.body))
	// The two ANCHORS (chatAnswerEllipsisHit.BodyRunes / .Messages) are stamped
	// here, at the same moment the token shapes are read, and for the same reason:
	// a hit's offset was recorded as it was scanned, and the body length / message
	// count that offset is relative to is only final once the body is. Stamping
	// them earlier would name a shorter body than the offsets inside it (Rule 15).
	// They are separate facts from the shape, so a later change to either one must
	// not be able to drop the other (the merge that produced this line had exactly
	// that shape: master rewrote the loop, the branch added the anchor).
	bodyRunes := w.runeOff
	messages := w.messages
	for i := range w.hits {
		chatAnswerEllipsisClassify(&w.hits[i], w.body)
		w.hits[i].BodyRunes = bodyRunes
		w.hits[i].Messages = messages
	}
}

// scan walks body[scanned:limit] rune by rune and records every ellipsis.
func (w *chatAnswerEllipsisWatch) scan(limit int) {
	for w.scanned < limit {
		r, size := utf8.DecodeRune(w.body[w.scanned:])
		if r == chatAnswerEllipsisRune {
			w.hits = append(w.hits, chatAnswerEllipsisHit{
				Turn:    w.turn,
				Offset:  w.runeOff,
				Excerpt: chatAnswerEllipsisExcerpt(w.body, w.scanned),
				// The default source for a watch hit is the model's own text: the
				// watch was HANDED these bytes, and a hit that came from it is
				// literally present in the copy the model produced. The turn-level
				// attribution in reportChatAnswerEllipses may still downgrade the
				// turn to unattributable when the renderer drew labels beside it —
				// it does so by construction, not by rewriting this field.
				Source: chatAnswerEllipsisSourceAnswer,
				byteAt: w.scanned,
			})
		}
		w.scanned += size
		w.runeOff++
	}
}

// chatAnswerEllipsisPartialTail reports how many trailing bytes of b form an
// INCOMPLETE UTF-8 sequence (0 when the tail is decidable). Holding them is what
// makes a rune split across two deltas one hit instead of none.
func chatAnswerEllipsisPartialTail(b []byte) int {
	for n := 1; n <= chatAnswerEllipsisPartialTailMax && n <= len(b); n++ {
		if !utf8.RuneStart(b[len(b)-n]) {
			continue
		}
		if utf8.FullRune(b[len(b)-n:]) {
			return 0
		}
		return n
	}
	return 0
}

// chatAnswerEllipsisExcerpt quotes the body around the byte offset at, bounded
// per side and flattened to ONE line: a report the terminal wraps is a report
// whose turn/offset fields a reader loses.
func chatAnswerEllipsisExcerpt(body []byte, at int) string {
	start, end := at, at
	for n := 0; n < chatAnswerEllipsisExcerptRunes; n++ {
		if start == 0 {
			break
		}
		_, size := utf8.DecodeLastRune(body[:start])
		start -= size
	}
	for n := 0; n < chatAnswerEllipsisExcerptRunes; n++ {
		if end >= len(body) {
			break
		}
		_, size := utf8.DecodeRune(body[end:])
		end += size
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString(chatAnswerEllipsisExcerptOpen)
	}
	b.WriteString(chatAnswerEllipsisFlatten(string(body[start:end])))
	if end < len(body) {
		b.WriteString(chatAnswerEllipsisExcerptClose)
	}
	return b.String()
}

// chatAnswerEllipsisFlatten renders control characters as visible escapes so the
// excerpt is exactly one line of cells.
func chatAnswerEllipsisFlatten(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// chatAnswerEllipsisTokenAt returns the token the character at byte offset at
// (of size bytes) sits in: the maximal run of non-separator runes on both sides,
// bounded by chatAnswerEllipsisUnitScanRunes per side.
//
// It is read at the TURN'S END, against the complete body, because a token is
// routinely still streaming when its ellipsis arrives — `task/01a16033-…`
// reaches the seam one fragment before `-r1` does — and a shape read mid-stream
// would describe half a token and misclassify it as prose.
//
// bounded reports whether a separator (or the body's own edge) ended the token on
// BOTH sides. An unbounded side means the body ended inside the token, so its
// shape is unknown rather than prose — the two print differently (Rule 15: an
// unknown is named, never filled in).
func chatAnswerEllipsisTokenAt(body []byte, at, size int) (token string, bounded bool) {
	start, end := at, at+size
	leftOK, rightOK := false, false
	for n := 0; n < chatAnswerEllipsisUnitScanRunes && start > 0; n++ {
		r, s := utf8.DecodeLastRune(body[:start])
		if !chatAnswerEllipsisUnitRune(r) {
			leftOK = true
			break
		}
		start -= s
	}
	for n := 0; n < chatAnswerEllipsisUnitScanRunes && end < len(body); n++ {
		r, s := utf8.DecodeRune(body[end:])
		if !chatAnswerEllipsisUnitRune(r) {
			rightOK = true
			break
		}
		end += s
	}
	return chatAnswerEllipsisFlatten(string(body[start:end])), leftOK && rightOK
}

// chatAnswerEllipsisUnitRune reports whether a rune can be part of a pasted unit.
// The class is deliberately NARROW — printable ASCII, plus U+2026 itself — because
// a token is only useful to a reader if it is the token: a walker that also took
// the CJK sentence after an identifier returned one uncompletable run-on (see
// chatAnswerEllipsisUnitMinRune). Prose in any other script is, to this seam, the
// SEPARATOR that ends a token — which is the correct reading: Chinese puts no
// space between an identifier and the words around it.
func chatAnswerEllipsisUnitRune(r rune) bool {
	if r == chatAnswerEllipsisRune {
		return true
	}
	return r >= chatAnswerEllipsisUnitMinRune && r <= chatAnswerEllipsisUnitMaxRune
}

// chatAnswerEllipsisClassify fills a hit's Unit / UnitKind / Severity from the
// completed body. It is the epic's criterion in code: the character alone is not
// the finding — the finding is whether the character sits in a token an operator
// is expected to paste.
func chatAnswerEllipsisClassify(h *chatAnswerEllipsisHit, body []byte) {
	raw, bounded := chatAnswerEllipsisTokenAt(body, h.byteAt, len(string(chatAnswerEllipsisRune)))
	h.unitKnown = bounded
	h.Unit = raw
	if !bounded {
		// The body ended inside the token: there is no shape to read. An unknown is
		// NOT a prose ellipsis (that would be a quiet downgrade of a real cut), and
		// it is not a severe claim either — the summary counts it as unknown.
		h.Severity = chatAnswerEllipsisSeverityOrdinary
		return
	}
	h.UnitKind = chatAnswerEllipsisUnitKind(raw)
	if h.UnitKind == "" {
		h.Severity = chatAnswerEllipsisSeverityOrdinary
		return
	}
	h.Severity = chatAnswerEllipsisSeverityTruncated
}

// chatAnswerEllipsisUnitKind answers WHAT KIND of pasted unit the token is, or ""
// when it is ordinary prose. The order of the tests is the order of specificity:
// a URL is never a path, a flag is never a path, and a `/`-joined token is a ref
// when its first segment is the ref vocabulary and a path otherwise.
func chatAnswerEllipsisUnitKind(raw string) string {
	token := strings.Trim(raw, chatAnswerEllipsisUnitWrappers)
	// The character under test is part of the evidence, not part of the shape: a
	// token is classified as if the ellipsis had not cut it.
	bare := strings.ReplaceAll(token, string(chatAnswerEllipsisRune), "")
	if len([]rune(bare)) < chatAnswerEllipsisUnitMinPathRunes {
		return ""
	}
	if strings.Contains(token, chatAnswerEllipsisURLAutority) {
		return chatAnswerEllipsisUnitURL
	}
	if strings.HasPrefix(token, chatAnswerEllipsisFlagPrefix) {
		return chatAnswerEllipsisUnitCommand
	}
	if strings.ContainsRune(token, chatAnswerEllipsisRefSeparator) {
		if strings.Count(token, string(chatAnswerEllipsisRefSeparator)) >= chatAnswerEllipsisUnitMinPathSlashes {
			for _, prefix := range []string{
				chatAnswerEllipsisRefTaskPrefix,
				chatAnswerEllipsisRefFeatPrefix,
				chatAnswerEllipsisRefFixPrefix,
				chatAnswerEllipsisRefChatPrefix,
			} {
				if strings.HasPrefix(token, prefix) {
					return chatAnswerEllipsisUnitBranch
				}
			}
			return chatAnswerEllipsisUnitPath
		}
	}
	// A code span in backticks is an identifier even when it is a single word: the
	// backticks are the model's own statement that the exact bytes matter.
	if strings.HasPrefix(raw, "`") || strings.HasSuffix(raw, "`") {
		return chatAnswerEllipsisUnitIdentifier
	}
	if chatAnswerEllipsisLooksLikeID(bare) {
		return chatAnswerEllipsisUnitIdentifier
	}
	return ""
}

// chatAnswerEllipsisLooksLikeID reports whether the token carries the shape of an
// opaque id: a hyphen-separated segment spelled entirely in hex runes and long
// enough to be one (a task id, a content hash).
//
// SEGMENT-wise, not whole-token (measured, 2026-10-22, the operator's turn-5
// body): `01a15bb9-…-r1` is a task id carrying the reap suffix, so a whole-token
// hex test rejected it and the report downgraded a genuinely uncompletable token to
// "prose". The hex run is the id; what follows it is an ordinary suffix.
//
// A stated limit: an English word spelled entirely from a-f (`deadcafe`, `beefed`)
// answers true. The cost of that false positive is one extra detail line; the cost
// of the false negative was a silent downgrade of a truncated id, which is the
// failure this report exists to prevent.
func chatAnswerEllipsisLooksLikeID(bare string) bool {
	for _, segment := range strings.Split(bare, string('-')) {
		if len(segment) < chatAnswerEllipsisUnitMinHexRunes {
			continue
		}
		hex := true
		for _, r := range segment {
			switch {
			case r >= '0' && r <= '9':
			case r >= 'a' && r <= 'f':
			case r >= 'A' && r <= 'F':
			default:
				hex = false
			}
			if !hex {
				break
			}
		}
		if hex {
			return true
		}
	}
	return false
}

// reportChatAnswerEllipses writes AT MOST TWO LINES per turn: one DETAIL line for
// the first hit that cut a pasted unit (the class the operator must act on), and
// ONE summary line carrying every count this turn produced. It returns how many
// lines it wrote, which is what a test asserts on; the count of HITS is
// len(hits), and the summary states it, so aggregation never becomes loss.
//
// Why the budget is this small (measured, 2026-10-22): the previous shape wrote one
// line per hit plus a remainder plus a display-layer line — NINE lines for the
// round the operator filed, which pushed the answer itself off the console. A
// guard that buries the artifact to report a defect in the artifact is the same
// failure it was built to catch. The counts are what carries the record; a line
// each is what the console cannot carry.
//
// turn is the turn whose rows were drawn, and it is what a turn with NO hits
// (every `…` on its rows came from a cut label) names in its summary line.
//
// EVERY LINE STATES THE FACTS AND NOTHING ELSE: the character, whether a pasted
// unit was cut and which one, its location, its SOURCE (which copy it is in —
// chatAnswerEllipsisSource), and the one thing a reader must not have to guess:
// the drawn bytes were not repaired.
func reportChatAnswerEllipses(w io.Writer, hits []chatAnswerEllipsisHit, cutLabels, turn int) int {
	if w == nil || (len(hits) == 0 && cutLabels == 0) {
		return 0
	}
	// The turn's verdict is decided FIRST, so the detail line can state it too: a
	// hit only carries "the model's own text" when the turn as a whole says so.
	source := attributeChatAnswerElision(chatAnswerEllipsisTurn{Hits: hits, CutLabels: cutLabels})
	hitSource := chatAnswerEllipsisSourceAnswer
	if source != chatAnswerEllipsisSourceAnswer {
		hitSource = source
	}
	truncated, prose, undecidable := chatAnswerEllipsisSplit(hits)
	// THE SILENCE CRITERION, and it is ONE predicate (Rule 14: the view and the
	// emission cannot disagree). A turn speaks to the operator only when it carries
	// at least one of the three things he must act on:
	//
	//   - a HIGH hit — the ellipsis is INSIDE a pasted unit (a path, a ref, a flag,
	//     a URL, an opaque id), so what he was handed cannot be used;
	//   - an undecidable hit — the body ended inside the token, so the seam cannot
	//     say whether a pasted unit was cut; an unknown is named, never filled in;
	//   - a cut label the RENDERER drew — the display copy carries a cell the
	//     model's text does not, which is the display/model distinction the
	//     operator's own investigation cost a day to establish.
	//
	// Anything else is prose: a rhetorical ellipsis that cost nothing to read past.
	// It is recorded on the hits, counted here, and printed NOWHERE — the LOW
	// budget is zero LINES, not "zero detail lines with a summary". Measured
	// 2026-11-09 (this epic's report): the summary alone put one line on the
	// operator's band for every turn that wrote a prose ellipsis, with no switch
	// and no silence condition, which is exactly the noise that gets a real notice
	// skimmed past. The file header's THE REPORTING BUDGET states the same thing.
	if len(truncated) == 0 && len(undecidable) == 0 && cutLabels == 0 {
		return 0
	}
	written := 0
	// THE DETAIL LINE — only for the class that costs the operator something, and
	// only one of them: the rest of that class is counted in the summary below.
	for i, hit := range truncated {
		if i == chatAnswerEllipsisTruncatedLinesPerTurn {
			break
		}
		fmt.Fprintf(w, "%s — %s %s %q (turn %d, rune offset %d of %d%s); NOT altered (report only); source: %s\n",
			chatAnswerEllipsisReportPrefix, chatAnswerEllipsisSeverityTruncated,
			chatAnswerEllipsisTruncatedWord, hit.Unit, hit.Turn, hit.Offset, hit.BodyRunes,
			chatAnswerEllipsisBasisSuffix(hit.Messages), hitSource)
		written++
	}
	// THE ONE-LINE SUMMARY: a single truncated hit with nothing else beside it is
	// fully described by the line above, so it gets no redundant summary line.
	if len(truncated) == 1 && len(prose) == 0 && len(undecidable) == 0 && cutLabels == 0 {
		return written
	}
	fmt.Fprintf(w, "%s — turn %d: %d hit(s): %d %s %s",
		chatAnswerEllipsisReportPrefix, chatAnswerEllipsisTurnNumber(hits, turn), len(hits),
		len(truncated), chatAnswerEllipsisSeverityTruncated, chatAnswerEllipsisTruncatedWord)
	if len(truncated) > 0 {
		fmt.Fprintf(w, " (named %d)", chatAnswerEllipsisNamedTruncated(len(truncated)))
	}
	fmt.Fprintf(w, ", %d %s %s", len(prose), chatAnswerEllipsisSeverityOrdinary,
		chatAnswerEllipsisOrdinaryWord)
	if len(prose) > 0 {
		// The prose example's own location: the detail line names the severe token,
		// so the low class still gets one locator without getting a line of its own.
		// It carries the SAME anchor as the detail line — `of <body>` plus the
		// basis clause — because the confusion this report was filed for does not
		// care which severity the character sat in: an offset with no body named
		// invites a comparison against the last round's wire text (task 01a16053).
		fmt.Fprintf(w, " (first at rune offset %d of %d%s)", prose[0].Offset, prose[0].BodyRunes,
			chatAnswerEllipsisBasisSuffix(prose[0].Messages))
	}
	if len(undecidable) > 0 {
		fmt.Fprintf(w, ", %d %s", len(undecidable), chatAnswerEllipsisUnknownUnit)
	}
	if cutLabels > 0 {
		// THE GLYPH IS NAMED FROM THE ONE PLACE THAT DRAWS IT, and it is no longer
		// U+2026: the band's right-edge cut spends a row's last cell on
		// lineedit.AnswerCutLabel and displaces the character that held it onto
		// the row below (answer_cut_label_glyph.go carries why the ellipsis was
		// the wrong mark for that — it reads as "content omitted", and nothing is
		// omitted). The COUNT and the attribution are unchanged; only the cell's
		// spelling moved, so a report that still said "U+2026" would send a reader
		// looking for a glyph the renderer no longer draws.
		fmt.Fprintf(w, "; display layer: the renderer drew %d cut label(s) on this turn's rows — "+
			"cells carrying %s that the model's output does NOT contain "+
			"(the band's own continuation mark, not an elision)", cutLabels, lineedit.AnswerCutLabel)
	}
	fmt.Fprintf(w, "; NOT altered (report only); source: %s\n", source)
	written++
	return written
}

// chatAnswerEllipsisNamedTruncated is how many of the truncated hits the detail
// loop actually printed — stated as a number in the summary so a reader knows the
// difference between "one cut" and "one shown of eight".
func chatAnswerEllipsisNamedTruncated(n int) int {
	if n > chatAnswerEllipsisTruncatedLinesPerTurn {
		return chatAnswerEllipsisTruncatedLinesPerTurn
	}
	return n
}

// chatAnswerEllipsisSplit partitions a turn's hits by the criterion: the ones that
// cut a pasted unit, the ones that did not, and the ones whose token the body
// ended inside (shape unknown — counted separately, never folded into prose).
func chatAnswerEllipsisSplit(hits []chatAnswerEllipsisHit) (truncated, prose, undecidable []chatAnswerEllipsisHit) {
	for _, hit := range hits {
		switch {
		case !hit.unitKnown:
			undecidable = append(undecidable, hit)
		case hit.Severity == chatAnswerEllipsisSeverityTruncated:
			truncated = append(truncated, hit)
		default:
			prose = append(prose, hit)
		}
	}
	return truncated, prose, undecidable
}

// chatAnswerEllipsisTurn is ONE turn's worth of report input: the located hits in
// the text the seam was handed, and how many cut labels the RENDERER drew on that
// turn's rows. The two numbers are what make an attribution possible at all —
// neither alone can answer "which copy is this character in".
type chatAnswerEllipsisTurn struct {
	Hits []chatAnswerEllipsisHit
	// CutLabels is the renderer's own count (lineedit.CutLabels): cells it DREW
	// that the answer's text does not contain.
	CutLabels int
}

// attributeChatAnswerElision decides, once per turn, what the turn's report can
// honestly say. The three outcomes are mutually exclusive and each is provable:
//
//   - no character in the handed text and none drawn → nothing to report;
//   - a character in the handed text, no cut label drawn → it is the model's:
//     the seam is the only writer of that text and the only drawer of those rows,
//     and it drew no label, so the two copies agree;
//   - cut labels drawn → the DISPLAY copy carries cut-label cells the model's
//     text does not (lineedit.AnswerCutLabel — a continuation mark, U+21B5 since
//     answer_cut_label_glyph.go; it was U+2026 before). That is the difference the operator's investigation hit, and it is
//     a display-layer fact (RENDERER), never a model violation. When the same turn
//     ALSO has hits, a per-character answer ("which of the `…` on my screen is
//     the model's?") is NOT available from this seam — the grid rows and the
//     source offsets are different coordinate systems and the seam does not read
//     raw.log — so the turn is reported UNATTRIBUTABLE for that question instead
//     of being guessed at.
func attributeChatAnswerElision(t chatAnswerEllipsisTurn) chatAnswerEllipsisSource {
	switch {
	case t.CutLabels > 0 && len(t.Hits) > 0:
		return chatAnswerEllipsisSourceUnattributable
	case t.CutLabels > 0:
		return chatAnswerEllipsisSourceRenderer
	default:
		return chatAnswerEllipsisSourceAnswer
	}
}

// chatAnswerEllipsisBasisSuffix names the BASIS an offset is measured on, and
// stays silent for the ordinary one-message turn so a clean report is unchanged.
// It is the whole fix for the +19 finding: the offset was never wrong, the basis
// was never stated, and a reader compared it to the last round's wire text.
func chatAnswerEllipsisBasisSuffix(messages int) string {
	if messages <= 1 {
		return ""
	}
	return fmt.Sprintf(" (across %d assistant messages: the turn's ReAct rounds stream into ONE answer body, so this offset is NOT an index into the last round's wire text alone)", messages)
}

// chatAnswerEllipsisTurnNumber is the turn a summary line names: the hits' own
// turn when there are hits, and the CALLER's counter when there are none (a turn
// whose only U+2026 came from cut labels — the measured case, where the answer
// text carries no character at all). A record that cannot name its turn is not
// actionable, so the fallback is the caller's index rather than a zero.
func chatAnswerEllipsisTurnNumber(hits []chatAnswerEllipsisHit, turn int) int {
	if len(hits) == 0 {
		return turn
	}
	return hits[0].Turn
}

// chatReportAnswerEllipses is the REPL's emission seam: the located hits of the
// turn that just ended plus the cut labels the renderer drew during it, drawn
// through the SAME notice channel the rest of the REPL uses (on the answer band
// in front of the operator, on stderr for a scripted run), never through a bare
// stderr write while the band owns the screen — a report the operator cannot see
// is not a report (chat_diagnostics.go).
func chatReportAnswerEllipses(input *chatInput, renderer *chatTurnRenderer) int {
	if input == nil || renderer == nil {
		return 0
	}
	hits := renderer.Ellipses()
	cutLabels := renderer.CutLabels()
	if len(hits) == 0 && cutLabels == 0 {
		return 0
	}
	var b strings.Builder
	n := reportChatAnswerEllipses(&b, hits, cutLabels, renderer.Turn())
	if n == 0 {
		return 0
	}
	input.WriteNotice(b.String())
	return n
}
