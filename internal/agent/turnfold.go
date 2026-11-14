// turnfold.go — THE NOTE AS A SUBSTITUTE, NOT AN ADDITION.
//
// WHAT THIS CHANGES, in one paragraph. 第一期 (selfrecord.go) recorded a note
// and left the turn it described in the conversation, so the note was an
// EXTRAS on top of the raw history. From this file on, a COMPLETED turn is
// REPLACED by its note: the turn's own input row, every round's tool_use /
// tool_result and the assistant text all leave Ctx.Messages, and what remains
// in their place is ONE harness note that states what the turn did and carries
// the exact anchors (task ids, paths, commands, error text) a later round can
// search for. The raw transcript is NOT lost — it is on disk in the session
// JSONL, which is the layer `session_search` reads. The conversation is
// therefore O(turns) notes rather than O(rounds × results) raw history.
//
// WHY IT IS SAFE TO REMOVE A WHOLE TURN, and the only reason it is: a turn is
// removed as a UNIT (its input message, every assistant message it produced and
// every tool result answering them). The Claude wire contract this package is
// gated on is tool_use ↔ tool_result PAIRING, not their presence — removing
// both sides of a pair leaves the invariant `verifyToolUsePairs` asserts
// untouched, and a note takes the turn's place as a USER message so no two
// consecutive assistant messages are created either.
//
// WHERE THE BOUNDARY COMES FROM (Rule 14 — no second opinion). This file does
// NOT invent a predicate for "where does a turn begin". It asks
// sessionevent.IsTurnOpeningRow about the very message it is looking at, so the
// conversation's fold boundaries and `gopi session log --turns`' turn axis are
// ONE judgement, not two that can drift.
//
// THE PREDICATE ALREADY ANSWERS BOTH HALVES, which is why nothing here re-checks
// "is this a harness row". A framework user row that is not an INPUT — a
// `[system] ` checkpoint, a job notice, an inbox restatement — is not a turn
// opening either, so it rides inside the turn that already exists and is folded
// with it. The rows the predicate DOES count are the ones a person or a peer put
// on the boundary: a briefing on stdin, a `gopi message boundary` row, and a new
// `you>` input. A local `!isHarnessInjectedUser` guard beside that question would
// be a second opinion that can disagree with `session log --turns` — and it did:
// it made a boundary row invisible to the fold, which is how the arms below
// caught it.
//
// WHAT IS DELIBERATELY NEVER FOLDED.
//   - the LAST turn (still running — the model is in it),
//   - INDEX 0 ON A TASK RUN — the premise (a plain prompt or a dispatched
//     briefing). NOT on a chat: there message 0 is one of the operator's inputs
//     among many and sits at 0 forever, so exempting it made every chat's first
//     turn permanently unfoldable (measured through the real binary, see
//     foldableTurnRanges).
//   - a range that IS already a note (isTurnFoldNoteMessage) — there is nothing
//     left to replace, and folding one into another would nest a note in a note,
//   - any message carrying a self-record note or a memory summary
//     (isNonCompressibleMessage): those are the record layers this whole epic
//     exists to preserve,
//   - image-bearing turns (the images cannot be re-derived from text, and the
//     fold's note is text).
//
// THE ANCHORS ARE MECHANICAL, NOT PARAPHRASED. extractTurnAnchors runs the
// SAME anchorCategories the compaction's Anchor Index uses (compact.go, Rule
// 14) over the turn's text, so a SHA, path, error string, URL or version in a
// turn survives at the same fidelity whether the turn was folded here or by a
// memory compaction. The syntheses on top add the two anchors a generic regex
// does not see: the harness's own record ids (#N) and the literal command of
// every bash call.

package agent

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

const (
	// turnFoldNoteHead heads the note that REPLACES a folded turn. It is a
	// member of the harness marker family (it opens a user row the framework
	// wrote), so it is spelled on the same convention as the other injections
	// and named here rather than typed at the render site (Rule 1b).
	//
	// It deliberately does NOT reuse selfRecordOpenMarker: that delimiter is
	// the WIRE CONTRACT the model is taught to write, and a note this file
	// composed must never be confused with one the model recorded (nor be
	// re-extracted by recordSelfNotes as if the model had just written it).
	//
	// IT IS MINIMAL ON PURPOSE (operator, 2026-11-10). The head used to carry a
	// full sentence — "the history this note replaces is on disk in this
	// session's transcript" — and that sentence was the note's first line in
	// every request for the rest of the run: a sentence the model already knows
	// (it is IN the transcript it is reading) paid for out of the note's budget
	// on every single turn. What the head has to do is say ONE thing to the fold
	// machinery and to a reader: this user row is a substitute for a turn, not
	// something a person typed. "turn folded" says exactly that, and every
	// reader of this value (isTurnFoldNoteMessage, turnFoldConclusionClaim, the
	// wire tests, the chat round-decision fixture) tests it as a PREFIX or as a
	// substring of this spelling, so the identification survives the cut.
	turnFoldNoteHead = harnessMessagePrefix + "turn folded"

	// turnFoldNothingToFoldToken is the ONE spelling an operator greps for when
	// asking "was there nothing to fold, or is the fold broken?".
	//
	// WHY IT EXISTS (task 01a1d723): foldCompletedTurns has TWO early exits and
	// BOTH used to return without a word — `len(splitTurnRanges(msgs)) < 2` (the
	// whole conversation is one range) and `len(complete) <= window` (every
	// complete turn sits inside the operator's raw window). From the console
	// they were indistinguishable from a broken fold: the log carried the 📦
	// success line and NOTHING else. Measured 2026-11-14 on session
	// 6f982833-6291-45cd-b420-ed9df0ce1e36: 443 requests, 7 turn-fold lines,
	// `grep -c refused` = 0, and the whole session never folded once because it
	// cut into a single turn range.
	//
	// The two lines SHARE this token on purpose — a reader wants "did it fold,
	// and if not, why", and two unrelated spellings would make that a search
	// over a vocabulary rather than over one word.
	turnFoldNothingToFoldToken = "nothing to fold"

	// turnFoldRecentTurnsDefault is the size of the RAW WINDOW: the number of
	// newest COMPLETE turns that stay in the request verbatim. It is a
	// DELIBERATE ESCAPE HATCH for an operator who wants the newest turns' exact
	// wording, and its DEFAULT IS ZERO — the substitute form.
	//
	// ZERO IS THE CONTRACT, AND IT WAS MEASURED BACK OFF 8. The note is a
	// SUBSTITUTE for a turn, not a summary beside it (task
	// 01a1bafd-d5f6-7c1e-b88e-42373f79ae1c): every COMPLETED turn leaves the
	// request and ONE note takes its place. A default window of 8 says the
	// opposite — the newest eight turns stay raw and are never replaced — so a
	// conversation of nine turns folds nothing at all until it is nine turns
	// deep, and a 39-turn session whose request is still below the compaction
	// trigger folds NOTHING (measured 2026-11-12 on session
	// a596a78e-9594-4b4f-8768-3135e42f4835: `session context --last --user`
	// carried 285 messages, 143 raw user rows and 135 raw tool_result blocks,
	// and ZERO `turn folded` notes).
	//
	// The knob itself survives because "keep the last N turns verbatim" is a
	// real preference (it is what DefaultCompactKeepRecent expresses for the
	// summariser); only its DEFAULT moved, and it moved to the value the epic's
	// contract names.
	turnFoldRecentTurnsDefault = 0

	// turnFoldRecentTurnsEnv overrides turnFoldRecentTurnsDefault
	// (GOPI_TURN_FOLD_KEEP_TURNS). It is read in exactly ONE place —
	// turnFoldRecentTurnsFromEnv below — so the width of the raw window can never
	// be decided by two readers disagreeing (PROJECT.md Rule 2). ZERO IS A VALID
	// SETTING and it means "no raw window: every completed turn is replaced by
	// its note". A NEGATIVE or unparseable value is IGNORED with a warning (the
	// default stands), because a negative width names no window anyone could
	// mean (Rule 15 — the warning names it).
	turnFoldRecentTurnsEnv = "GOPI_TURN_FOLD_KEEP_TURNS"

	// maxTurnFoldNoteChars caps one turn note. The epic's ceiling was 1000 and
	// it is a HARD one: a note is the whole turn in one budget, so a turn's
	// value has to survive being compressed to it rather than being allowed to
	// grow into a second transcript.
	//
	// IT WAS RAISED FROM 1000, and the reason is the note this file now COPIES
	// rather than composes. A turn that ends with a self-record note carries
	// that note's text VERBATIM (the whole point of the copy path below), and
	// maxSelfRecordNoteChars is 500 runes per note, while a turn may hold MORE
	// THAN ONE (the contract asks for one per round, and a turn can hold several
	// rounds). The measured case that forced the number: one recorded note of
	// 1206 runes was cut to 1000 the moment it became the body of a fold note —
	// i.e. the fold truncated the very thing it exists to preserve. 1600 keeps
	// two worst-case recorded notes (2 × 500) plus the head, the ask and the
	// anchors inside one note, and it is still bounded by construction: the copy
	// path caps the copied body at turnFoldNoteBodyBudget and the mechanical
	// rendering is capped by everything else, so nothing here can grow with the
	// turn.
	//
	// IT IS DERIVED, NOT TYPED (2026-11-10). The number used to be a literal
	// 1600 with a prose argument for why that was enough, and the argument was
	// wrong for the ordinary case: a turn's recorded notes are bounded by the
	// RECORD layer (maxSelfRecordNoteChars each) and by nothing this file
	// controls, so any hand-chosen total is a second guess at a quantity the
	// note already has an arithmetic for. The ceiling is now the SUM of the
	// slots it has to hold — the copied body, the ask, the anchors, the
	// conclusion and the scaffolding (head, labels, bullets and the truncation
	// sentence) — so raising one slot can never silently eat another, and the
	// only thing that changes when the record layer's own cap moves is this
	// expression.
	maxTurnFoldNoteChars = turnFoldNoteBodyBudget + turnFoldAskChars + turnFoldAnchorsBudget + turnFoldConclusionChars + turnFoldNoteScaffoldRoom

	// turnFoldBodyNotes is how many WORST-CASE recorded notes the copied body is
	// sized for — maxSelfRecordNoteChars each, so the body budget is
	// turnFoldBodyNotes × the record layer's own per-note cap.
	//
	// THE MEASURED DEFECT this replaces: the body budget was a flat 1000 runes,
	// so a turn holding ONE recorded note of 1206 runes had it CUT the moment it
	// became the note body — the fold truncated the very text it exists to
	// preserve, and the "not kept here" line fired on a turn that had recorded
	// nothing unusual. Four worst-case notes cover the ordinary turn (a note per
	// round, a handful of rounds) with room to spare; beyond that the counted
	// truncation line below still fires, because an unbounded copy is a second
	// transcript and Rule 15 only asks that the loss be STATED, never that it be
	// impossible.
	turnFoldBodyNotes = 4

	// turnFoldNoteBodyBudget caps the COPIED body — the self-record notes a turn
	// already wrote — inside the note. It is a share of maxTurnFoldNoteChars, not
	// an independent ceiling: the head, the ask and the anchors all come out of
	// the same budget, so a body allowed to run to its own limit would push the
	// anchors out of the note they belong to. Derived from the record layer's own
	// per-note cap so the two cannot drift (Rule 14).
	turnFoldNoteBodyBudget = maxSelfRecordNoteChars * turnFoldBodyNotes

	// turnFoldNoteScaffoldRoom is the room the NON-content scaffolding needs:
	// the head line, the "asked: " / "recorded: (<source>)" labels, one bullet
	// per note, the conclusion's label and the truncation sentence — which
	// carries a PATH, so it is the largest of them. It is reserved rather than
	// measured because reserving it is what keeps the sum above honest: without
	// it the body could fill the ceiling and the truncation sentence the Rule 15
	// promise rides on would be the thing cut off the end.
	turnFoldNoteScaffoldRoom = 300

	// turnFoldAnchorsBudget caps the anchor section of a note. It is a SHARE
	// of maxTurnFoldNoteChars, not an independent ceiling: the summary, the
	// counts and the head all come out of the same budget, and an anchor block
	// allowed to run to its own limit would push the summary out of the note it
	// belongs to.
	turnFoldAnchorsBudget = 400

	// turnFoldParseCostBounds how many anchors are kept. Bounded for the same
	// reason the note is: eight anchors are searchable, eighty are a dump.
	turnFoldAnchorCount = 8

	// turnFoldAskChars caps the ONE line that says what the turn was ASKED for.
	// The briefing's note spec has four slots and the ask is the first; a note
	// that cannot say what the turn was about is not a substitute for the turn,
	// it is an inventory of its tool calls.
	//
	// IT IS COUNTED IN RUNES AND THAT IS WHY IT WAS TOO SMALL (measured
	// 2026-11-09). At 180 runes a CJK conversation got ~60 Chinese characters —
	// three short clauses — while the anchor block beside it has its own 400-rune
	// budget, so the note could say MORE about the ids it saw than about what it
	// was asked to do. The ratio was inverted, not the ceiling: the ask is the
	// line a later round reads to know what this turn was FOR, so it is raised to
	// sit at the SAME share as the anchors (400) rather than a third of it. It is
	// still a share of maxTurnFoldNoteChars, never an independent ceiling.
	//
	// IT WAS RAISED AGAIN (measured, session 26ffcd9f, 2026-11-10) once the
	// dictation carrier came off the ask: the stripped input of that turn was
	// 573 runes of ONE paragraph, and at 400 it closed with "1 line(s) not kept
	// here" — on the ordinary case, not an outlier. A note whose ask line says
	// "this turn's input was longer" on nearly every CJK turn is a note that
	// spends its Rule 15 sentence on noise, which is how a real truncation stops
	// being visible. 800 holds a dictated paragraph with room to spare; past it
	// the counted line still fires, because the ask is a share of the ceiling and
	// not an exception to it.
	turnFoldAskChars = 2 * turnFoldAnchorsBudget

	// turnFoldAskLineSep joins a multi-line ask's kept lines, and
	// turnFoldAskDroppedFmt closes it with the COUNT of lines not kept. Both are
	// constants because turnFoldAsk's truncation has to be READABLE (Rule 15): a
	// note that silently shortens what the operator asked reads as a complete
	// request, and its reader has no transcript in front of them to notice.
	turnFoldAskLineSep    = "\n  "
	turnFoldAskDroppedFmt = "\n  … (this turn's input was longer; %d line(s) not kept here — the full text is in the transcript)"
	turnFoldAskCutTail    = "… (cut; the full text is in the transcript)"

	// turnFoldConclusionChars caps the turn's own CONCLUSION — the last
	// substantial thing the model said in it. That is the slot a later round
	// reads to know where the work stopped without re-reading the transcript.
	turnFoldConclusionChars = 240

	// turnFoldRecapChars is how much of a tool's own first line is quoted as a
	// RECAP. One line is enough to say "this passed" / "this failed", which is
	// what the next round needs; the body is in the transcript.
	turnFoldRecapChars = 80

	// The record id shape (#N) this file harvests as an anchor. It is the
	// memory/ledger cross-reference the model itself votes on, so it must
	// survive a fold. `recordIDRe` in loop.go looks for the same shape in ONE
	// direction (counting which ids a round cited); this one harvests them for
	// the note.
	turnFoldRecordIDPrefix = "#"

	// The three conversation roles a fold has to tell apart. They are named
	// here because this file SWITCHES on them (Rule 1b: a value compared on is
	// a constant, not an inline literal); the wire spelling is unchanged.
	roleUserMessage      = "user"
	roleAssistantMessage = "assistant"
	roleToolMessage      = "tool"

	// turnFoldCommandArg is the bash tool's own argument name, spelled once
	// here rather than at the harvest site (Rule 1b).
	turnFoldCommandArg = "command"

	// turnFoldConclusionLabel heads the note's CONCLUSION slot. It is a constant
	// because the renderer writes it and turnFoldConclusionClaim reads it back —
	// two sites, one spelling (Rule 1b/14).
	turnFoldConclusionLabel = "conclusion: "

	// turnFoldAskLabel heads the ONE line that says what the turn was ASKED.
	// Named (Rule 1b) because the renderer writes it and the tests read it.
	turnFoldAskLabel = "asked: "

	// turnFoldCountsLabel heads the mechanical counts line — round(s) and tool
	// call(s). It is a const because the render site and the fact that the line
	// is now CONDITIONAL (turnFoldNote renders it only when the copy path could
	// not answer) both need one spelling to reason about.
	turnFoldCountsLabel = "what this turn did: "

	// turnFoldNoRecordLine is what a four-slot note says about its own
	// provenance: the record file had no line for this turn, so the note was
	// COMPOSED from the mechanical slots instead of copied. Named because a
	// reader greps for it (a test asserts on it, and an operator reading a
	// session needs to tell the two kinds of note apart), and because it is
	// charged to turnFoldNoteScaffoldRoom like every other fixed line.
	turnFoldNoRecordLine = turnFoldRecordLabel + "该 turn 无自记录 (no self-record line for this turn — this note is composed from the mechanical slots below)"

	// turnFoldAnchorsLabel heads the anchor section of a note.
	turnFoldAnchorsLabel = "anchors: "

	// turnFoldRecordLabel heads the note's RECORDED-KNOWLEDGE slot: the turn's
	// own `[IMPORTANT]` notes, copied verbatim out of the record file
	// (turnFoldRecordSource). It is
	// spelled once because a reader (a human, or a later round's grep) looks for
	// it, and it must not be confused with the conclusion line beside it.
	turnFoldRecordLabel = "recorded: "

	// turnFoldNoteBullet is the mark the fold renders above every RECAP line —
	// the per-call first-lines section at the end of a note, which is the fold's
	// own rendering and not a copy of anything.
	//
	// IT IS NOT THE RECORDED SLOT'S PREFIX ANY MORE (measured 2026-11-14): the
	// `recorded:` slot claims to copy the record file verbatim, and the file
	// writes `t<turn> <note>` (selfRecordTurnStampFmt, selfrecord.go) — no dash.
	// The renderer prepending "- " put a character between the label and the
	// file's bytes that the file does not have, so the copy was one substring
	// test away from being false and no reader could compare the two. The slot
	// now copies untouched; this constant survives only where the fold really is
	// the author (the recap), which is why the name still says "note".
	turnFoldNoteBullet = "- "

	// turnFoldTruncatedMark is what a note carries when the body budget cut it.
	// A cut WITHOUT a mark is a silent loss of the very text the slot exists to
	// carry (Rule 15): the reader must be able to tell "the model wrote this and
	// it was longer" from "the model wrote exactly this".
	turnFoldTruncatedMark = " [truncated]"

	// turnFoldTruncatedLabel heads the line that reports what the body budget
	// left out, and where the whole text can be read instead. It is a constant
	// because a reader looks for it (the same reason turnFoldRecordLabel is).
	turnFoldTruncatedLabel = "recorded-truncated: "

	// turnFoldRecordFileName is NOT re-spelled here: the record layer owns the
	// name (selfRecordFileName, selfrecord.go) and this file joins it into the
	// directory THAT layer resolves, so the fold reads the exact file the record
	// layer writes (Rule 14 — one definition, two readers).

	// turnFoldRecordTailLines is how many UNCONSUMED record lines the LAST turn
	// folded in one call may claim. It is a LINE bound, not the note's character
	// budget: the tail is handed to the latest completed turn so that one turn's
	// notes are all copied (turnFoldRecordSource states the allocation rule and
	// its residual). Bounded so a session that recorded for days cannot pull its
	// whole history into one note.
	turnFoldRecordTailLines = 64

	// turnFoldRoundWord / turnFoldToolWord are the note's own units.
	turnFoldRoundWord = "round(s)"
	turnFoldToolWord  = "tool call(s)"

	// turnFoldPostmarkMaxChars bounds how far the head-strip will look for the
	// `]` that closes a postmark before deciding the text is not wearing one.
	// A postmark is a handful of characters ("[t+58s]", "[t+~31m]",
	// "[tool_use id: call_x]"); anything longer is prose that happens to open
	// with the same characters, and the strip must leave it alone rather than
	// eat a paragraph looking for a bracket that is not a postmark's.
	turnFoldPostmarkMaxChars = 64
)

// turnFoldPostmarkPrefix is the OPENING every tick postmark shares, DERIVED from
// the clock's two format strings rather than typed again (Rule 1/13/14):
// tickExactFmt is "[t+%s]" and tickApproxFmt is "[t+~%s]", so their common
// prefix is exactly the part before a rendered duration can differ.
var turnFoldPostmarkPrefix = func() string {
	i := 0
	for i < len(tickExactFmt) && i < len(tickApproxFmt) && tickExactFmt[i] == tickApproxFmt[i] {
		i++
	}
	return tickExactFmt[:i]
}()

// The carriers fall into TWO CLASSES, and the class decides WHERE the strip
// looks — not because the classes mean different things, but because the two are
// written in different places by different producers.
//
//   - turnFoldAskLeadingMarkers are ROW POSTMARKS: the harness opens a row with
//     them (`[system] `, `[gopi message boundary] `, the stdin briefing marker,
//     the re-injected self-record carrier). They are postmarks by CONTRACT — the
//     writer puts them first — so they are stripped from the HEAD only. Stripping
//     them anywhere would eat an operator's own sentence, and one of them
//     (`harnessMessagePrefix`) is a spelling a person can legitimately type.
//
//   - turnFoldAskProvenanceSentences are PROVENANCE: a SENTENCE the harness says
//     about where the text came from (the dictation carrier). It is not a
//     postmark and no producer promises it opens the message — the boundary
//     framing's own `the operator's line, verbatim: ` clause puts it after
//     itself, and an assistant that quotes the input back puts it wherever the
//     quote begins. So it is stripped WHEREVER IT OCCURS, which is the fix the
//     measured defect needs (see stripTurnFoldAskCarriers).
//
// EVERY ENTRY IS DERIVED FROM ITS OWN DEFINITION (Rule 1/13/14) — none is typed
// here: all five come from sessionevent (the package that writes them) or from
// the same alias this package already reads (`harnessMessagePrefix`), so a
// re-worded carrier cannot leave these lists matching the old spelling, and
// neither list can become a second, drifting copy of the vocabulary.
var turnFoldAskLeadingMarkers = []string{
	sessionevent.HarnessMessageBoundaryPrefix,
	sessionevent.HarnessBriefingOnStdinMarker,
	sessionevent.HarnessSelfRecordMarker,
	harnessMessagePrefix,
}

// turnFoldAskProvenanceSentences are the sentences that say WHERE the text came
// from rather than WHAT it says, and they are position-free (see the class note
// above). VoiceInputPrefix is the sessionevent layer's — the same value
// `chatVoiceInputPrefix` prepends (sessionevent.VoiceInputPrefixFor).
var turnFoldAskProvenanceSentences = []string{
	sessionevent.VoiceInputPrefix,
}

// stripHarnessPostmarks removes the POSTMARKS the harness wraps a tool result in
// before it enters the conversation, leaving the payload the tool actually
// returned.
//
// WHAT IT STRIPS, AND WHY IT IS NOT A BLACKLIST. A tool result reaches
// Ctx.Messages as `<tick> <call header>\n<payload>`: clock.go's withTick prefixes
// the tick and loop.go / chat_ops.go stamp `[tool_use id: <id>]` in front of the
// body. Both are ADDRESSES — the tick says when the call ran, the header says
// which call this answers — and neither is something the tool returned. The fold
// used to read that raw string as the result, so the recap slot quoted
// `[t+58s] [tool_use id: call_x]` instead of `exit=0`, and the call id took an
// anchor slot the turn's real identifiers needed (measured on session 592ae637).
//
// The tokens are the harness's OWN format constants (tickExactFmt /
// tickApproxFmt through turnFoldPostmarkPrefix, and toolResultHeaderMarker), so
// this is a HEAD-STRIP built from the definitions that WRITE the postmarks, not a
// list of strings to keep out: nothing here can drift from the writer, and a new
// postmark needs a change in the writer before it can need one here.
//
// IT ONLY STRIPS FROM THE HEAD, repeatedly, and it stops at the first thing that
// is not a postmark — so a payload whose own text legitimately contains
// `[tool_use id:` a hundred lines down is untouched, and a result with neither
// postmark passes through byte for byte.
func stripHarnessPostmarks(s string) string {
	for {
		trimmed := strings.TrimLeft(s, " \t\r\n")
		if v, ok := cutTickPostmark(trimmed); ok {
			s = v
			continue
		}
		if v, ok := cutToolHeaderPostmark(trimmed); ok {
			s = v
			continue
		}
		return trimmed
	}
}

// cutTickPostmark removes ONE leading tick postmark (`<tick> `) and reports
// whether there was one. The bracket must close inside
// turnFoldPostmarkMaxChars and the interior must carry no whitespace — a tick is
// one token, so a `[t+ … ]` spanning words is prose and is left alone.
func cutTickPostmark(s string) (string, bool) {
	rest, ok := strings.CutPrefix(s, turnFoldPostmarkPrefix)
	if !ok {
		return s, false
	}
	end := strings.IndexByte(rest, ']')
	if end < 0 || end > turnFoldPostmarkMaxChars {
		return s, false
	}
	if strings.ContainsAny(rest[:end], " \t\r\n") {
		return s, false
	}
	return rest[end+1:], true
}

// cutToolHeaderPostmark removes ONE leading call header
// (`[tool_use id: X]` plus the newline that ends it) and reports whether there
// was one. The marker is the harness's own constant, so the strip cannot drift
// from the stamp that writes it.
func cutToolHeaderPostmark(s string) (string, bool) {
	rest, ok := strings.CutPrefix(s, toolResultHeaderMarker)
	if !ok {
		return s, false
	}
	end := strings.IndexByte(rest, ']')
	if end < 0 || end > turnFoldPostmarkMaxChars {
		return s, false
	}
	return rest[end+1:], true
}

// turnFoldStats reports what one fold did, so the caller can log it and a test
// can assert on a NUMBER instead of on prose (Rule 15: a fold that silently
// removed nothing must be distinguishable from one that worked).
type turnFoldStats struct {
	// Folded is the number of turns replaced by a note in this call.
	Folded int
	// Removed is the number of raw messages that left Ctx.Messages.
	Removed int
	// Notes are the notes, in order, that replaced them.
	Notes []string
	// KeptRaw is how many complete turns this call deliberately left in the
	// request verbatim because they were inside the recent-turn window
	// (turnFoldRecentTurns). It is a NUMBER a test can assert on, so "the
	// window is what stopped this" is never inferred from prose.
	KeptRaw int
}

// turnFoldRecentTurnsFromEnv is the ONE reader of turnFoldRecentTurnsEnv
// (Rule 2). It is called once, at Agent construction, beside the other GOPI_*
// parses — never at a round boundary, because a window that could change in the
// middle of a conversation would make "which turns are raw" depend on when the
// environment was read rather than on the conversation itself.
//
// ZERO IS VALID AND IT IS THE DEFAULT: "no raw window" — every completed turn is
// replaced by its note, which is the substitute contract. An unset/empty value, a
// non-integer, or a value BELOW zero is IGNORED (the default stands) WITH a
// warning naming the var and the default, exactly like every other envInt parse in
// this package.
func turnFoldRecentTurnsFromEnv() int {
	return envInt(turnFoldRecentTurnsEnv, turnFoldRecentTurnsDefault, turnFoldRecentTurnsDefault, func(n int) bool { return n >= 0 })
}

// turnFoldRecentTurns reports the size of the RAW WINDOW: how many of the newest
// COMPLETE turns stay in the request verbatim. Zero (the default) means no window
// at all: nothing is protected from the fold, and a completed turn becomes its
// note the moment a later turn begins — task 01a1bafd's substitute form.
func (a *Agent) turnFoldRecentTurns() int {
	if a == nil {
		return turnFoldRecentTurnsDefault
	}
	// A NEGATIVE field is not a window either: it falls through to the default
	// like every other unset knob, so an Agent{} literal and an explicitly
	// negative one resolve the same way.
	if a.TurnFoldRecentTurns >= 0 {
		return a.TurnFoldRecentTurns
	}
	return turnFoldRecentTurnsDefault
}

// foldCompletedTurns replaces every completed turn in a.Ctx.Messages with one
// note each, and returns what it did.
//
// THE NOTE IS A SUBSTITUTE, NOT AN ADDITION (task 01a1bafd-d5f6-7c1e-b88e-42373f79ae1c,
// and the operator's ruling that reinstated it). A completed turn's raw messages
// LEAVE the request and ONE note takes their place, so a conversation of T turns
// costs O(T) notes instead of O(rounds × results) raw history. The raw history is
// not destroyed: it is on disk in the session JSONL, which is what
// `keyword_search scope=session` reads.
//
// IT IS UNCONDITIONAL, AND THE THRESHOLD THAT USED TO GATE IT IS GONE. Commit
// 4c8c44df made this function run only when the context had crossed the
// compaction trigger; the effect was MEASURED (2026-11-12, session
// a596a78e-9594-4b4f-8768-3135e42f4835, `gopi session context --last --user`):
// 39 turns, 285 messages and 93,059 estimated tokens against a
// 0.75 × 200,000 = 150,000 threshold — so the fold never fired ONCE, and the
// request carried 143 raw user rows and 135 raw tool_result blocks with ZERO
// `turn folded` notes. A substitute that engages only past a line most sessions
// never cross is not the substitute form; it is the addition form plus a note
// nobody sees. `trimContext` already owns the size-triggered reduction, and
// there the trigger is correct because compaction SUMMARISES; the fold does not
// summarise — it copies the turn's own recorded words — so it needs no threshold
// to be safe.
//
// WHAT STILL GATES A FOLD is what would make it LOSE information, and nothing
// else: index 0 on a task run (the run's premise), a turn that is already a note,
// a text-only turn whose wording IS its value, a turn holding an image, and a
// turn holding a compaction block or a re-injected record carrier. See
// foldableTurnRanges and turnRangeFoldable — each exclusion names its own reason.
//
// turnFoldRecentTurns IS AN ESCAPE HATCH, NOT THE POLICY: it is 0 by default
// (nothing is protected: every completed turn folds), and an operator who wants
// the newest N turns' exact wording sets GOPI_TURN_FOLD_KEEP_TURNS. When it is
// set, the newest N complete turns stay verbatim and only the older ones become
// notes.
//
// A turn is COMPLETE when a LATER turn has begun — the conversation itself says
// so, because there is content after it (a new boundary of input, or anything at
// all the turn did not produce). The turn containing the FINAL message is the
// one in flight and is never folded: the fold runs at a round boundary, so the
// final turn is the round that is about to be sent.
//
// It is idempotent by construction: a folded turn is one harness note, which is
// not a raw turn any more, so a second call finds nothing new to remove. That
// matters because the boundary it is called from runs once per ROUND.
func (a *Agent) foldCompletedTurns() turnFoldStats {
	var stats turnFoldStats
	if a == nil || a.Ctx == nil || len(a.Ctx.Messages) == 0 {
		return stats
	}
	turns := splitTurnRanges(a.Ctx.Messages)
	if len(turns) < 2 {
		// SAY IT ONCE, NAME THE CRITERION (task 01a1d723). This exit used to
		// return without a word, which made "the conversation has nothing to
		// fold" indistinguishable from "the fold is broken": measured
		// 2026-11-14 on session 6f982833, 443 requests, `grep -c refused` = 0
		// while the whole session never folded once. The latch keeps it to one
		// line per session — the boundary is per ROUND.
		if !a.turnFoldRangeAnnounced && a.Logger != nil {
			a.turnFoldRangeAnnounced = true
			a.Logger.Printf("📦 turn-fold: %s — the conversation cuts into %d turn range(s) and a fold needs at least 2 (one complete, one in flight); nothing from the request was replaced, and this is reported once per session",
				turnFoldNothingToFoldToken, len(turns))
		}
		return stats
	}
	// The LAST turn is the in-flight one. Everything before it is complete.
	complete := turns[:len(turns)-1]
	// THE RECENT WINDOW (an operator's escape hatch, 0 by default): the newest
	// turnFoldRecentTurns complete turns stay verbatim. Only the turns older than
	// that are replaced by their notes. At the default of 0 there is no protected
	// turn at all, so EVERY completed turn folds — the substitute form.
	window := a.turnFoldRecentTurns()
	if len(complete) <= window {
		stats.KeptRaw = len(complete)
		// SAY IT ONCE, NAME THE CRITERION (task 01a1d723). The operator set a raw
		// window wide enough to hold every complete turn, so the fold is not
		// broken — it is doing exactly what the knob says. Without this line an
		// A/B of `GOPI_TURN_FOLD_KEEP_TURNS` reads as "the fold stopped working".
		// It names the knob and the actual width, because those are what the
		// operator has to change to see a fold.
		if !a.turnFoldWindowAnnounced && a.Logger != nil {
			a.turnFoldWindowAnnounced = true
			a.Logger.Printf("📦 turn-fold: %s — all %d complete turn(s) are inside the raw window (%s=%d), so no turn is old enough to replace; nothing from the request was replaced, and this is reported once per session",
				turnFoldNothingToFoldToken, len(complete), turnFoldRecentTurnsEnv, window)
		}
		return stats
	}
	older := complete[:len(complete)-window]
	stats.KeptRaw = window
	// bodyFrom starts at the HEAD OF THE ARRAY — index 0, not the first turn
	// range. foldableTurnRanges can only return a SUBSET of `older`, and `older`
	// can only start at a range boundary, so NOTHING guarantees index 0 is the
	// start of a foldable range: a resumed conversation's message 0 is the note
	// the PREVIOUS run folded its last completed turn into (splitTurnRanges does
	// not cut a range at index 0, so that note and the reply after it ride
	// BEFORE every range). Starting at complete[0].start dropped
	// `Messages[0:bodyFrom]` — the previous run's knowledge and its anchors —
	// with no error and no log line (`folded=N` counts turns folded, never
	// messages lost); measured IN=8 OUT=3 with removed=4. Everything before the
	// first foldable turn is carried verbatim by construction, which holds only
	// when the copy starts at 0.
	bodyFrom := 0
	// recordCursor is this CALL's position in the run's record file, shared by
	// every turn folded here so one turn's note lines are never offered to the
	// next (turnFoldRecordSource states the allocation rule).
	recordCursor := 0
	foldable := a.foldableTurnRanges(a.Ctx.Messages, older)
	// TURN ORDINALS COME FROM THE POSITION IN THE CONVERSATION, never from the
	// position in the folded list. With a window, the first turn a call folds is
	// no longer turn 1 — numbering it by its index in `foldable` would read some
	// OTHER turn's `t<N>` lines out of self-record.md and paste them into this
	// turn's note, i.e. it would attribute one turn's knowledge to another.
	turnAt := make(map[int]int, len(turns))
	for i, r := range turns {
		turnAt[r.start] = i + 1
	}
	var kept []deepseek.Message
	for i, r := range foldable {
		kept = append(kept, a.Ctx.Messages[bodyFrom:r.start]...)
		note := a.turnFoldNote(a.Ctx.Messages[r.start:r.end], &recordCursor, i == len(foldable)-1, a.foldTurnFor(turnAt[r.start]))
		kept = append(kept, deepseek.Message{Role: "user", Content: note})
		stats.Folded++
		stats.Removed += r.end - r.start
		stats.Notes = append(stats.Notes, note)
		bodyFrom = r.end
	}
	if stats.Folded == 0 {
		return stats
	}
	kept = append(kept, a.Ctx.Messages[bodyFrom:]...)

	// FAIL CLOSED, NEVER SEND A DAMAGED HISTORY. The fold removes whole turns,
	// so it cannot orphan a pair by construction — but "cannot by construction"
	// is an argument, and the argument was wrong once already in this package
	// (context.go's pairing epic). The rebuilt array is therefore PROVEN with
	// the same verifier the resume path and the outbound request use, and a
	// violation leaves the conversation EXACTLY as it was while reporting why.
	if err := verifyToolUsePairs(kept); err != nil {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ turn-fold: refused — the rebuilt history violates the tool_use/tool_result pairing invariant, keeping the raw turns: %v", err)
		}
		return turnFoldStats{}
	}
	a.Ctx.Messages = kept
	if a.Logger != nil {
		a.Logger.Printf("📦 turn-fold: %d completed turn(s) replaced by their note (%d raw message(s) left the request)",
			stats.Folded, stats.Removed)
	}
	return stats
}

// turnRange is a half-open span of Ctx.Messages that one turn occupies.
type turnRange struct{ start, end int }

// splitTurnRanges cuts the conversation into turns using the ONE boundary
// predicate the session readers use (sessionevent.IsTurnOpeningRow, through its
// package alias). A user row that opens a turn starts a new range; every other
// message — an assistant message, a tool result, and a harness injection that
// rides INSIDE the current turn — extends the range that is already open.
//
// A message before the first boundary (an assistant message at index 0 in a
// resumed history, say) belongs to the first range: it is part of what the first
// turn did, and dropping it into a range of its own would invent a turn.
func splitTurnRanges(msgs []deepseek.Message) []turnRange {
	var out []turnRange
	for i, m := range msgs {
		if i > 0 && isTurnFoldNoteMessage(m) {
			// AN ALREADY-FOLDED TURN IS ITS OWN RANGE. Without this cut, every
			// note is absorbed into the range that began before it, so the whole
			// folded head becomes ONE range whose start is index 0 — the range
			// the fold exempts — and a session resumed with several completed
			// turns in its history could never fold ANY of them (measured: a
			// three-complete-turn fixture folded 0). Cutting here keeps each
			// turn's raw span separate, so `start == 0` exempts the PREMISE
			// alone instead of the entire head.
			if len(out) > 0 {
				out[len(out)-1].end = i
			}
			out = append(out, turnRange{start: i, end: len(msgs)})
			continue
		}
		if m.Role == roleUserMessage && isTurnOpeningRow(m.Content) {
			if len(out) > 0 {
				out[len(out)-1].end = i
			}
			out = append(out, turnRange{start: i, end: len(msgs)})
		}
	}
	if len(out) == 0 && len(msgs) > 0 {
		out = append(out, turnRange{start: 0, end: len(msgs)})
	}
	return out
}

// isTurnFoldNoteMessage reports whether a message is a note this file already
// wrote — the substitute for a turn that has been folded. Such a range is
// skipped by the fold (there is nothing left to replace) and is what makes the
// rewrite idempotent.
func isTurnFoldNoteMessage(m deepseek.Message) bool {
	return m.Role == roleUserMessage && strings.HasPrefix(m.Content, turnFoldNoteHead)
}

// isTurnOpening asks the shared predicate whether this message opens a turn.
//
// IT IS A THIN FORWARD, NOT A SECOND RULE, and it exists only so the call site
// reads as one question. The recorded FACE is derived by the same function the
// writer and the reader use (sessionevent.TurnRowSource), so a row recorded
// without a face is judged exactly as `gopi session log --turns` judges it.
func isTurnOpeningRow(content string) bool {
	return sessionevent.IsTurnOpeningRow(content, sessionevent.TurnRowSource(""))
}

// foldableTurnRanges filters the complete turns down to the ones this file may
// replace.
//
// ON A TASK RUN, INDEX 0 IS NEVER FOLDED — it is the run's PREMISE: a plain
// prompt (`gopi "<prompt>"`, not a harness row at all) or a dispatched briefing
// (`[gopi briefing] `, which is), and a run whose own instruction was folded into
// a note ABOUT it has lost the one text it can check its goal against. The
// exemption is bounded by construction: index 0 alone, so even a hundred-turn
// session keeps exactly one raw turn and pays a note for every other completed
// one.
//
// ON THE CHAT FACES THERE IS NO PREMISE, so nothing is exempt (a.ChatMode).
// A chat's message 0 is one of the operator's inputs among many, and exempting
// it was MEASURED wrong: the first turn of every chat sits at index 0 for the
// life of the conversation, so it could never fold, and the e2e walk
// (testsuite/turnfold_budget_test.go) found the last request still carrying turn
// 1's raw tool_use/tool_result pair with a complete turn behind it.
func (a *Agent) foldableTurnRanges(msgs []deepseek.Message, complete []turnRange) []turnRange {
	var out []turnRange
	for _, r := range complete {
		// INDEX 0 IS EXEMPT ONLY WHEN IT IS A TASK RUN'S PREMISE — never on the
		// chat faces. A dispatched run has no operator: message 0 IS the
		// briefing, and folding it would compress the run's own goal and
		// acceptance criteria into a note the run then works from. A chat's
		// message 0 is one of the operator's inputs among many, all equally
		// replaceable — and exempting it there was MEASURED wrong: the first
		// turn of every conversation sits at index 0 forever, so a chat could
		// never fold it, and the e2e walk found the last request still carrying
		// turn 1's tool_use/tool_result pair with a complete turn behind it.
		if r.start == 0 && !a.ChatMode {
			continue
		}
		if !turnRangeFoldable(msgs[r.start:r.end]) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// turnRangeFoldable reports whether a turn's messages can leave the request
// without losing something the request cannot re-derive.
//
// FOUR exclusions, each with its own reason and none of them a taste call:
// a range that IS a note (folding it nests a note in a note), a turn with no
// tool traffic (its wording IS its value — see below), a turn holding an image
// block (the pixels are the message), and a turn holding a COMPACTION block or a
// RE-INJECTED record carrier — neither of which is this turn's own writing.
//
// A TURN'S OWN `[IMPORTANT]` NOTE IS NO LONGER AN EXCLUSION, and that is the fix
// this file's copy path exists for. It used to be: the record layer was exempt
// wholesale, so a turn that BOTH recorded a note and called a tool could not be
// folded at all — the exact shape a working round has — and when such a turn was
// folded the recorded knowledge was discarded (the operator's verdict, measured
// on a 1206-rune note that never reached the conversation). The exemption was
// standing in for the real requirement: the record must SURVIVE. turnFoldNote
// now copies the note text into the substitute verbatim, so the knowledge leaves
// the transcript as it always did — through its own words, not through a
// summariser.
func turnRangeFoldable(msgs []deepseek.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	// A range that IS a note is already a substitute: folding it would put a
	// note inside a note and lose the anchors it carries.
	if isTurnFoldNoteMessage(msgs[0]) {
		return false
	}
	// A TURN WITH NO TOOL TRAFFIC IS NOT WORTH FOLDING, and folding it is not
	// free. The growth this file exists to stop is Σ(turns × their tool
	// traffic): a tool result is a whole file's contents or a command's output,
	// while a text turn is one message whose VALUE IS ITS OWN WORDING. A note
	// caps the conclusion at turnFoldConclusionChars and the ask at
	// turnFoldAskChars — so folding a text-only turn keeps a paraphrase of
	// the answer and discards the answer to save one message. Measured against
	// the shipped arms: the seed replay of a plain chat
	// (TestChatOps_TextTurn_UnchangedShape) and the plain notify turn
	// (TestSessionInboxIsDeliveredOncePerTurn) both ride on a text turn staying
	// intact, and both are CORRECT to: nothing there was large.
	if !turnCarriesToolTraffic(msgs) {
		return false
	}
	for _, m := range msgs {
		if len(m.Images) > 0 {
			return false
		}
		if m.Role == roleUserMessage && isTurnFoldPinnedMessage(m) {
			return false
		}
	}
	return true
}

// isTurnFoldPinnedMessage reports whether a USER row must stay in the request
// verbatim.
//
// It is narrower than isNonCompressibleMessage ON PURPOSE, and the narrowing is
// the whole fix: that predicate answers "is a summarizer call wasted on this?"
// and it says yes to a turn's own `[IMPORTANT]` note as well — which was read
// here as "this turn may not be folded", so the ordinary working round (a note
// AND a tool call) could never fold, and the fold that did run around it threw
// the note away. A compaction MEMORY SUMMARY is different in kind: it is not this
// turn's writing at all, it is a previous reduction's output, and a note about a
// note has nothing left to be right about. The RE-INJECTED carrier
// ([SELF-RECORD — …]) is a framework restatement of notes that live in the record
// file; folding it into a turn note would nest a carrier inside a note.
func isTurnFoldPinnedMessage(m deepseek.Message) bool {
	return isMemorySummaryMessage(m) || isSelfRecordCarrierMessage(m)
}

// isSelfRecordCarrierMessage reports whether a user row is the harness's
// RE-INJECTION of the run's recorded notes (selfrecord.go's carrier) rather than
// a turn input. Matched by its marker prefix — the same one isSelfRecordMessage
// uses for its first arm — because the carrier is the only user row the harness
// writes that opens with it.
func isSelfRecordCarrierMessage(m deepseek.Message) bool {
	return strings.HasPrefix(strings.TrimLeft(m.Content, " \t\r\n"), selfRecordMarker)
}

// turnCarriesToolTraffic reports whether any message in the range holds a tool
// call or a tool result — the traffic whose size is worth replacing.
func turnCarriesToolTraffic(msgs []deepseek.Message) bool {
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 || m.Role == roleToolMessage {
			return true
		}
	}
	return false
}

// turnFoldNote renders the ONE note that replaces a turn. Its four slots are the
// briefing's spec, in the order a reader needs them:
//
//  1. what this turn was ASKED for (the turn's own input, one line);
//  2. what it DID and the conclusion it reached (counts + the last thing it said);
//  3. the ANCHORS a later round can search for (ids, paths, commands, errors);
//  4. the per-call RECAP — first lines only, and the first thing cut when the
//     budget binds (it is the most recoverable: the transcript has every result).
//
// THE FOUR SLOTS ARE A FALLBACK, NOT THE PREFERRED BODY. When the turn already
// recorded its own knowledge — selfrecord.go's notes, durable in the run's
// `self-record.md` — that note IS the distilled statement of the turn, and the
// four mechanical slots are a paraphrase of it: an inventory of tool calls that
// cannot say what was LEARNED. Copying the recorded note and demoting the slots
// to a fallback is what makes the fold keep a turn's knowledge instead of only
// its shape.
//
// THERE IS ONE DATA SOURCE (operator, 2026-11-10): the run's record FILE
// (`<session-dir>/self-record.md`) — every note the model wrote, verbatim,
// stamped `t<turn> ` and appended at the moment it knew the fact. It is the
// EVIDENCE layer, and it is the source that cannot lose a note to the
// archiving of a message (turnFoldRecordSource).
//
// THE TURN'S OWN MESSAGES ARE NOT A SECOND SOURCE and there is no fallback
// path: re-deriving a note out of archived assistant text is a worse copy of
// something the file already holds, and a copy that can disagree with the
// evidence is worse than no copy at all. What the file does not hold is not
// reported as recorded — the mechanical slots carry the turn's shape.
//
// THE ORDER IS THE PRIORITY. Everything comes out of one maxTurnFoldNoteChars
// budget, so the slots are filled in that order and the recap takes what is left;
// what a note may never lose is the ask and the anchors, because those are what
// make it a substitute rather than a deletion.
//
// IT IS A METHOD because its FIRST data source is the run's record FILE
// (turnFoldRecordSource), which lives in this run's session directory — a path
// only the agent can resolve. The cursor argument is the SAME fold call's
// position in that file: the turns folded in one call are consumed
// earliest-first, so one turn's lines are never offered to the next.
// foldTurnFor is the TURN ORDINAL a folded turn is labelled with — the same
// number the record file stamps on a note (noteTurnNumber, selfrecord_noteindex.go),
// counted from the turn the fold is about to remove.
//
// It is resolved from the FOLD, never invented here: foldCompletedTurns walks the
// COMPLETE turns in order, so the i-th one is turn (i+1) of this run. A run with
// no logger has no ordinals to compare, and the fold then falls back to the
// pre-stamp rule (claimable by position) rather than fabricating a number.
func (a *Agent) foldTurnFor(idx int) int {
	if a == nil || a.SessionLog == nil {
		return sessionevent.RoundIndexUnknown
	}
	return a.TurnBaseline + idx
}

func (a *Agent) turnFoldNote(msgs []deepseek.Message, consumed *int, last bool, turn int) string {
	rounds, calls := 0, 0
	var anchors []string
	seen := make(map[string]bool)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] || len(anchors) >= turnFoldAnchorCount {
			return
		}
		seen[v] = true
		anchors = append(anchors, v)
	}
	var recap []string
	conclusion := ""
	for _, m := range msgs {
		if m.Role == roleAssistantMessage {
			rounds++
			// THE CONCLUSION IS THE NON-NOTE TEXT. Before the copy path existed
			// this slot took the last assistant line whatever it was, so a turn
			// whose final assistant message WAS a self-record block recorded
			// "conclusion: [IMPORTANT]" — the marker, not the conclusion. The
			// record layer is copied into its own slot now, so this one is
			// filled from the text the record was stripped out of.
			if t := oneLineRecap(stripTurnFoldProvenance(selfRecordStrippedContent(m.Content))); t != "" {
				conclusion = t
			}
		}
		for _, tc := range m.ToolCalls {
			calls++
			add(tc.Name)
		}
		if m.Role == roleToolMessage && len(recap) < turnFoldAnchorCount {
			// THE POSTMARKS COME OFF BEFORE THE RECAP IS TAKEN. A tool result in
			// the conversation wears `<tick> <call header>\n<payload>` — the
			// harness's two addresses, neither of which the tool returned — and
			// taking the recap first quoted `[t+58s] [tool_use id: call_x]` as if
			// it were the call's own first line (measured on session 592ae637).
			recap = append(recap, oneLineRecap(stripTurnFoldProvenance(stripHarnessPostmarks(m.Content))))
		}
	}
	// The mechanical anchors FIRST — they are the exact identifiers the generic
	// regex layer also keeps (compact.go's anchorCategories), so a note and a
	// memory compaction agree about what is load-bearing.
	for _, text := range turnTextParts(msgs) {
		for _, v := range extractTurnAnchors(text) {
			add(v)
		}
	}
	// Then the two anchors a regex cannot do: the harness's own record ids
	// (#N) wherever they are cited, and the literal command of every bash call.
	// The command is the anchor a later round searches for when it needs to
	// know WHAT was run, and it lives in the tool call's arguments.
	for _, text := range turnTextParts(msgs) {
		for _, v := range harvestRecordIDs(text) {
			add(v)
		}
	}
	for _, v := range harvestBashCommands(msgs) {
		add(v)
	}

	// THE RECORD IS RESOLVED HERE, ONCE — file first, messages second, and a
	// logged fallback when neither answers (turnFoldRecordSource).
	rec := a.turnFoldRecordSource(msgs, rounds, consumed, turn)

	var b strings.Builder
	b.WriteString(turnFoldNoteHead)
	b.WriteString("\n")
	// WHAT THE TURN WAS ASKED — always kept, because a note that cannot say what
	// the turn was about is not a substitute for the turn.
	if ask := turnFoldAsk(msgs); ask != "" {
		// turnFoldAsk already caps ITSELF (capTurnFoldAsk: on a line boundary,
		// naming how many lines it did not keep). Capping again here would cut
		// that very record off — and it is the only place the loss is stated.
		fmt.Fprintf(&b, "%s%s\n", turnFoldAskLabel, ask)
	}
	// THE TURN'S OWN RECORDED KNOWLEDGE, COPIED — the slot this function is
	// ordered around. It goes ABOVE the mechanical counts because it is the
	// turn's own statement of itself, and it is quoted rather than re-derived
	// because the model already did the distilling (Rule 14: one definition of
	// what the turn learned, written once, at the moment the model knew it).
	if len(rec.notes) > 0 {
		b.WriteString(turnFoldRecordLabel)
		b.WriteString("\n")
		body, cut := 0, 0
		for _, note := range rec.notes {
			// A NOTE IS COPIED VERBATIM — with ONE stated exception.
			//
			// The record file holds ONE note per line by contract, and the
			// MESSAGE source has already been flattened by the record layer's own
			// extractor (collapseSelfNote, selfrecord.go) before it gets here, so
			// there is nothing left for this slot to tidy. It used to run every
			// note through strings.Fields, which silently re-spaced a note
			// containing a double space or a tab: the copy claimed to be verbatim
			// and was not, and no reader could tell which of the two they held.
			//
			// THE EXCEPTION IS PROVENANCE, and it is a CLASS, not a list of
			// strings to keep out (stripTurnFoldProvenance): a note that quotes
			// the turn's input back at it carries the harness's sentence about
			// where those bytes came from, and that sentence is not knowledge —
			// it is the same bytes the ask slot refuses. Removing them keeps the
			// copy faithful to what the turn LEARNED; keeping them would spend
			// this slot's budget on a claim about the other slot.
			note = stripTurnFoldProvenance(note)
			if strings.TrimSpace(note) == "" {
				continue
			}
			room := turnFoldNoteBodyBudget - body
			if room <= 0 {
				// NOTHING IS SILENTLY DROPPED: what did not fit is counted and
				// reported on its own line below, with where the whole text is
				// (Rule 15 — a cut the reader cannot see is a loss).
				cut++
				continue
			}
			if utf8.RuneCountInString(note) > room {
				note = capOneLine(note, room)
				cut++
			}
			// THE COPY CARRIES NO MARK OF ITS OWN. This slot used to prepend
			// turnFoldNoteBullet ("- ") to every note, and the record file has no
			// such prefix — so the slot claimed to be a verbatim copy while its
			// first character was the renderer's. The contract in the comment
			// above is now the whole truth: what stands between the label and the
			// newline is the file's own bytes (minus the stated provenance
			// exception), which also makes the copy CHECKABLE — a reader compares
			// the line against `self-record.md` and it matches character for
			// character (testsuite/turnfold_recordfile_wire_test.go, and the
			// four-criterion verifier's C3).
			b.WriteString(note)
			b.WriteString("\n")
			body += utf8.RuneCountInString(note)
		}
		if cut > 0 {
			fmt.Fprintf(&b, "%s%d of %d recorded note(s) did not fit turnFoldNoteBodyBudget (%d chars)%s\n",
				turnFoldTruncatedLabel, cut, len(rec.notes), turnFoldNoteBodyBudget, turnFoldTruncatedWhere(rec.path))
		}
	}
	// THE MECHANICAL COUNTS ARE A FALLBACK, NOT AN INVENTORY. When the record
	// file answered, the note already says what the turn LEARNED and the counts
	// say only how busy it was — "5 round(s), 6 tool call(s)" is the line an
	// operator reads as scaffolding, and it is the one slot whose whole content
	// is recoverable from nothing (it names no task, no file, no decision).
	// So it is rendered ONLY when the copy path could not answer, which is the
	// same condition the four slots exist for (see this function's header: the
	// slots are the FALLBACK). The count is never lost, it is simply not spent
	// when there is something better to spend the budget on.
	// THE FOUR-SLOT FALLBACK, AND IT SAYS SO. When the record file holds no line
	// for this turn, the note is composed from the four mechanical slots (ask,
	// counts, conclusion, anchors) — and the FIRST line says which of the two
	// things the reader is holding. Without that sentence an operator cannot tell
	// "this turn recorded nothing" from "the fold lost the note", which is the
	// ambiguity the copy path exists to remove (Rule 15: a loss the reader cannot
	// see is a loss).
	if len(rec.notes) == 0 {
		fmt.Fprintf(&b, "%s%d %s, %d %s.\n",
			turnFoldCountsLabel, rounds, turnFoldRoundWord, calls, turnFoldToolWord)
		b.WriteString(turnFoldNoRecordLine)
		b.WriteString("\n")
	}
	if conclusion != "" {
		fmt.Fprintf(&b, "%s%s\n", turnFoldConclusionLabel, capOneLine(conclusion, turnFoldConclusionChars))
	}
	if len(anchors) > 0 {
		b.WriteString(turnFoldAnchorsLabel)
		b.WriteString(strings.Join(anchors, ", "))
		b.WriteString("\n")
	}
	for _, r := range recap {
		if r == "" {
			continue
		}
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(r)+utf8.RuneCountInString(turnFoldNoteBullet) > maxTurnFoldNoteChars {
			break
		}
		b.WriteString(turnFoldNoteBullet)
		b.WriteString(r)
		b.WriteString("\n")
	}
	return capTurnFoldNote(b.String())
}

// turnFoldRecord is ONE turn's recorded knowledge, copied out of the run's
// record file so the renderer can put it where the turn used to be.
//
// THERE IS ONE SOURCE AND NO DEGRADED ONE (operator, 2026-11-10): the file
// `<session-dir>/self-record.md` is the ONLY thing a fold reads. The turn's own
// assistant messages are not a second source, and a note never carries a
// `(source)` mark: the model already distilled the turn as it happened, the file
// is append-only evidence of that, and a re-derivation out of archived messages
// is a worse copy of a thing we already hold. Copying is the job; composing a
// substitute is not.
type turnFoldRecord struct {
	// notes are the turn's recorded notes, in the order they were written.
	notes []string
	// path is the record file the notes came from, so a truncation line can name
	// where the whole text lives.
	path string
}

// turnFoldRecordSource resolves ONE turn's recorded knowledge FROM THE RECORD
// FILE — the only source there is.
//
// WHY THE FILE — the measured defect. The fold used to read notes out of the
// turn's own assistant messages and from nowhere else, so a turn whose
// `[IMPORTANT]` block did not survive in the archived message folded with NO
// notes at all: `recorded:` was skipped, silently, and four mechanical slots
// took over — an inventory of tool calls standing in for what the turn learned.
// The file had every note, verbatim, and nothing ever opened it.
//
// ALLOCATION — THE TURN STAMP, not a round count. Every line is written as
// `<turn> <note>` (selfrecord.go), so "which lines belong to THIS turn" is a
// comparison, never an inference. The turn is asked of the FOLD itself
// (a.foldTurnFor), so the number in the note and the number in the file are the
// same ordinal by construction. What is still allocated by position is only the
// LEGACY shape: a line written before the stamp epic carries no turn at all
// (`- <note>`) and none of them can be attributed — they are consumed
// earliest-first, exactly as before, so an old file still folds and no line is
// offered to two turns.
func (a *Agent) turnFoldRecordSource(msgs []deepseek.Message, rounds int, consumed *int, turn int) turnFoldRecord {
	limit := turnFoldRecordTailLines
	if turn > 0 {
		// A STAMPED LINE IS CLAIMED EXACTLY ONCE, by its own turn; the count is
		// only a safety bound (a malformed file cannot grow this without limit).
		limit = len(msgs) + turnFoldRecordTailLines
	}
	notes, path := a.turnFoldFileNotes(limit, consumed, turn)
	if len(notes) == 0 && a.Logger != nil {
		// THE PATH IS THE FILE'S OWN, never a join with a file name treated as a
		// directory. This line used to read recordPathIn("self-record.md"),
		// which joins the file name to itself and printed
		// `self-record.md/self-record.md` — a path that names nothing, in the one
		// message a reader follows to find out why a turn folded with no notes.
		// turnFoldFileNotes already resolved the real path; the constant stands in
		// only when it could not (no session logger), and then it is the file name
		// alone rather than an invented directory (Rule 15: name what was measured).
		where := path
		if where == "" {
			where = selfRecordFileName
		}
		a.Logger.Printf("⚠️ turn-fold: %s holds no note line for turn %d; the folded turn carries its ask and anchors only", where, turn)
	}
	return turnFoldRecord{notes: notes, path: path}
}

// turnFoldFileNotes reads the run's record file and returns the notes this turn
// may claim, plus the file's path and — when it could not answer — WHY.
//
// The caller limits the claim with `rounds`: see turnFoldRecordSource for the
// allocation rule and its known approximation. A missing file is not an error
// (a run that recorded nothing has none); a file that EXISTS and cannot be read
// is reported, because that is a real failure with a real consequence.
func (a *Agent) turnFoldFileNotes(limit int, consumed *int, turn int) ([]string, string) {
	if a == nil || a.SessionLog == nil {
		return nil, ""
	}
	dir, err := sessionDirForID(a.SessionLog.SessionID(), a.SessionLog.TaskID())
	if err != nil {
		return nil, ""
	}
	path := recordPathIn(dir)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, path
	}
	// THE CURSOR IS THE LINES, not the notes: every line this call walks past is
	// consumed, so a line is never offered to two turns, and a non-note line
	// (the file's blank tail) can never stall the walk.
	lines := strings.Split(string(body), "\n")
	start := 0
	if consumed != nil {
		start = *consumed
	}
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	if limit < 1 {
		limit = 1
	}
	var out []string
	at := start
	for i := start; i < len(lines); i++ {
		n, note, ok := selfRecordNoteFromLine(lines[i])
		if !ok {
			continue
		}
		note = strings.TrimSpace(note)
		if note == "" || isSelfRecordPlaceholder(note) {
			continue
		}
		// THE TURN STAMP DECIDES. A line that carries another turn's number is
		// NOT this turn's and is left where it is (the cursor does not advance
		// past it, so a later turn can still claim it); a line with no number at
		// all predates the stamp and is claimable by position, earliest-first,
		// which is the only rule the legacy shape admits.
		if n != sessionevent.RoundIndexUnknown && turn > 0 && n != turn {
			continue
		}
		out = append(out, note)
		if turn <= 0 || n == sessionevent.RoundIndexUnknown {
			at = i + 1
		}
		if len(out) >= limit {
			if consumed != nil && at < i+1 {
				at = i + 1
			}
			break
		}
		at = i + 1
	}
	if consumed != nil {
		*consumed = at
	}
	return out, path
}

// turnFoldTruncatedWhere names where a reader can find the text the body budget
// cut — the record file when the file was the source, and a statement of the
// alternative when it was not (a note that was cut must say where the whole one
// lives; Rule 15).
func turnFoldTruncatedWhere(path string) string {
	if path == "" {
		return " — the whole note is in this session's transcript"
	}
	return " — the whole note is in " + path
}

// selfRecordStrippedContent drops every self-record block from one assistant
// text, keeping the prose around it. It is what turns "the last thing the model
// said" into "the last thing it said OTHER THAN its own record", so the
// conclusion slot never degenerates to the marker.
func selfRecordStrippedContent(content string) string {
	if content == "" || !strings.Contains(content, selfRecordOpenMarker) {
		return content
	}
	var b strings.Builder
	rest := content
	for {
		open := strings.Index(rest, selfRecordOpenMarker)
		if open < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:open])
		after := rest[open+len(selfRecordOpenMarker):]
		closeAt := strings.Index(after, selfRecordCloseMarker)
		if closeAt < 0 {
			// An UNCLOSED block is kept: it is not a record this layer can
			// remove without guessing where it ended (Rule 15 — never silently
			// drop what could not be parsed).
			b.WriteString(rest[open:])
			return b.String()
		}
		rest = after[closeAt+len(selfRecordCloseMarker):]
	}
}

// turnFoldAsk returns the turn's own INPUT — the first user row in the range,
// which is the message that opened it. The harness markers that make it a
// boundary are stripped first: a note that reads "asked: [gopi message
// boundary] …" spends its budget on the framework's own framing.
//
// IT KEEPS THE INPUT'S OWN MULTI-LINE TEXT, and that IS the fix (measured
// 2026-11-09 on session 592ae637). This function used to end in oneLineRecap —
// a RECAP OF A TOOL RESULT, which drops every line but the first and caps at
// turnFoldRecapChars (80). So an operator's paragraph reached the note as its
// first 80 runes plus "…", and the capOneLine(turnFoldAskChars = 400) applied at
// the render site could not give back what those 80 had already dropped: a
// truncated request READS as a complete one, which is the silent failure Rule 15
// forbids. The note then reported the turn as if the rest had never been asked.
func turnFoldAsk(msgs []deepseek.Message) string {
	for _, m := range msgs {
		if m.Role != roleUserMessage || m.Content == "" {
			continue
		}
		return capTurnFoldAsk(stripTurnFoldAskCarriers(m.Content))
	}
	return ""
}

// stripTurnFoldAskCarriers removes every HARNESS CARRIER from a turn's input
// before it is rendered as the note's ask, and it LOOPS because the carriers
// stack.
//
// THE MEASURED DEFECT (operator, 2026-11-10, session 26ffcd9f): the ask line read
// `asked: [voice input] The text below was dictated: it is a speech-recognition
// transcript of what the user said, not characters they typed. Read it as the
// user's own words.` — 191 characters of PROVENANCE, rendered as if it were what
// the operator asked, and paid for out of the ask's own 400-rune budget. The
// words that followed were truncated to make room for a sentence ABOUT them.
//
// WHY IT WAS NOT ALREADY STRIPPED: the old loop trimmed a FIXED three markers in
// ONE pass, and a dictated input carries the voice carrier ON TOP of the input
// the boundary framed — so whichever marker sat outermost was trimmed and the
// one under it survived. A one-pass strip cannot answer a stack.
//
// THE CRITERION IS PROVENANCE, NOT POSITION (the correction this function
// carries). It used to ask "does this text OPEN WITH a carrier" —
// strings.TrimPrefix, once per carrier per pass — and that question is answered
// by the wrong fact. A carrier is not a POSTMARK on the text; it is a sentence
// the HARNESS wrote about where the text came from, and the harness writes it
// wherever its own producer happens to put it: in front of the operator's words
// on the voice route, and INSIDE the boundary framing on the spilled route
// (cmd/agent/chat_boundary.go's `… the operator's line, verbatim: ` clause puts
// it after the framing, so the input opens with the boundary carrier and the
// dictation carrier sits deeper). Position answers "is it the first thing?";
// provenance answers "whose sentence is this?", which is the question the note
// needs — and it is the one that stays true for every producer, present and
// future, without this function having to know their order.
//
// SO every PROVENANCE SENTENCE goes, wherever it sits, while every ROW POSTMARK
// goes from the head.
//
// The loop runs to a fixed point because removing one carrier can expose another
// (a postmark behind a provenance sentence), and because a removal can leave the
// separator that followed it. The result is re-trimmed and its blank runs are
// bounded: a carrier taken out from between two paragraphs must not leave the
// reader a break nobody wrote.
//
// EVERY VALUE COMES FROM ITS OWN DEFINITION (Rule 14), and the two classes are
// separated by WHERE THE PRODUCER PUTS THEM, never by taste — see the class note
// on turnFoldAskLeadingMarkers / turnFoldAskProvenanceSentences. The lists stay
// CLOSED and short on purpose: they hold carriers that are PROVENANCE ("where
// this text came from"), never anything a person typed — a strip that guessed
// would eat the operator's first line.
func stripTurnFoldAskCarriers(text string) string {
	for {
		before := text
		text = strings.TrimSpace(text)
		for _, marker := range turnFoldAskLeadingMarkers {
			text = strings.TrimSpace(strings.TrimPrefix(text, marker))
		}
		for _, sentence := range turnFoldAskProvenanceSentences {
			text = strings.ReplaceAll(text, sentence, "")
		}
		text = collapseTurnFoldBlankRuns(strings.TrimSpace(text))
		if text == before {
			return text
		}
	}
}

// collapseTurnFoldBlankRuns bounds a run of blank lines at ONE, which is what a
// removed carrier may leave behind (the dictation carrier is followed by a blank
// paragraph, sessionevent.VoiceInputSeparator, so taking the sentence out of the
// middle of a message leaves the break its separator made).
//
// It bounds runs and never touches a single blank line: an operator's own
// paragraph break is still exactly one, so the note cannot be accused of
// re-flowing text it did not level. Nothing else about the spacing changes.
func collapseTurnFoldBlankRuns(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// stripTurnFoldProvenance is the SAME judgement applied to the note's OTHER
// text slots — the copied record lines, the conclusion and the tool recaps.
//
// THEY ARE SEPARATE FIELDS BUT NOT SEPARATE CLASSES: a note states what the turn
// was asked, what it learned, what it concluded and what it observed, and no one
// of those four is the place a provenance sentence belongs. A model that quotes
// its own input back (a self-record line saying `asked: [voice input] …`, a
// conclusion restating it) would otherwise move the carrier from the ask slot
// into the body, where it costs the body's budget instead — the same defect,
// relocated. The record's own note is still copied verbatim EXCEPT for these
// bytes, and that exception is stated here rather than left for a reader to
// discover: provenance is never knowledge, so removing it does not make the copy
// unfaithful.
func stripTurnFoldProvenance(text string) string {
	if text == "" {
		return text
	}
	return stripTurnFoldAskCarriers(text)
}

// capTurnFoldAsk shortens the turn's input to turnFoldAskChars, preferring a
// LINE boundary and ALWAYS SAYING what it left out.
//
// Two shapes, and the difference is the INPUT's, not ours: a one-line input is
// capped on a character boundary (the only boundary it has); a multi-line input
// keeps whole lines while they fit and closes with turnFoldAskDroppedFmt naming
// how many were not kept. The dropped-line count is a RECORD, not decoration —
// Rule 15: a truncated ask that reads as a complete one is a silent failure, and
// the reader of a note has no transcript in front of them to notice the gap.
func capTurnFoldAsk(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		// ONE line is flattened (a one-liner stays a one-liner) but capped at
		// turnFoldAskChars — NOT at the tool-recap's 80, which is where this
		// path used to die — and, being cut, it says where the rest is.
		flat := strings.Join(strings.Fields(s), " ")
		if utf8.RuneCountInString(flat) <= turnFoldAskChars {
			return flat
		}
		return string([]rune(flat)[:turnFoldAskChars]) + turnFoldAskCutTail
	}
	kept, used := 0, 0
	sepWidth := utf8.RuneCountInString(turnFoldAskLineSep)
	for i, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		w := sepWidth + utf8.RuneCountInString(ln)
		if used+w > turnFoldAskChars {
			if kept == 0 {
				// A FIRST line too long to fit on its own is cut on a character
				// boundary — and every line after it goes too, which is the part
				// the reader cannot otherwise recover (Rule 15).
				return capOneLine(strings.TrimRight(lines[0], "\r"), turnFoldAskChars) +
					fmt.Sprintf(turnFoldAskDroppedFmt, len(lines)-i)
			}
			return strings.Join(lines[:kept], turnFoldAskLineSep) +
				fmt.Sprintf(turnFoldAskDroppedFmt, len(lines)-i)
		}
		used += w
		kept = i + 1
	}
	return strings.Join(lines[:kept], turnFoldAskLineSep)
}

// capOneLine shortens one line to n runes on a character boundary.
func capOneLine(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// capTurnFoldNote enforces maxTurnFoldNoteChars on a rune boundary, so a CJK
// note is never cut mid-character. The head is always kept: a note whose first
// line was truncated would not say what it is.
func capTurnFoldNote(note string) string {
	note = strings.TrimRight(note, "\n")
	if utf8.RuneCountInString(note) <= maxTurnFoldNoteChars {
		return note
	}
	runes := []rune(note)
	return string(runes[:maxTurnFoldNoteChars-1]) + "…"
}

// turnTextParts is every text a turn's messages carry, INCLUDING each tool
// call's arguments and name — the argument map is where a command, a path or a
// task id actually lives, and a scan that read only `Content` would miss all of
// the tool_use half.
func turnTextParts(msgs []deepseek.Message) []string {
	out := make([]string, 0, len(msgs)*2)
	for _, m := range msgs {
		if m.Content != "" {
			// THE POSTMARKS ARE NOT PART OF THE TURN'S TEXT. A tool result's
			// content opens with `<tick> <call header>`, and both are the
			// harness's addresses — the tick says when the call ran, the header
			// says which call this answers. Reading them as text let the tick be
			// harvested as if it were a value the turn produced, and it is the
			// reason a call id could reach an anchor slot a real identifier
			// needed (measured on session 592ae637). Stripping at the ONE
			// funnel every anchor scan reads from is what makes "no postmark is
			// an anchor" a property of the collection, not a rule each scan
			// remembers (Rule 14).
			out = append(out, stripHarnessPostmarks(m.Content))
		}
		for _, tc := range m.ToolCalls {
			out = append(out, tc.Name)
			if tc.RawArgs != "" {
				out = append(out, tc.RawArgs)
			}
			// THE ARGUMENTS ARE WHERE THE ANCHORS ARE. A parsed tool call keeps
			// its arguments in a MAP and leaves RawArgs empty (types.go: the map
			// is the flat representation the run loop dispatches on), so a scan
			// that read only RawArgs lost EVERY path, id and command in the
			// tool_use half — measured: turn 3's note came out with `read` and the
			// error text but not the path the read was about.
			for _, v := range tc.Arguments {
				if str, ok := v.(string); ok && str != "" {
					out = append(out, str)
				}
			}
		}
	}
	return out
}

// extractTurnAnchors runs the compaction's OWN anchor categories over one text
// and returns the identifiers in category order (Rule 14: one definition of
// "what is load-bearing", read by both layers). The per-category caps it uses
// are the compaction's, minus the sequencing: a note wants the values, not the
// frequency ranking a 10:1 summary needs.
func extractTurnAnchors(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, cat := range anchorCategories {
		vals := cat.re.FindAllString(text, -1)
		if len(vals) > 0 {
			out = append(out, vals...)
		}
	}
	return out
}

// harvestBashCommands pulls the `command` argument of every bash call in a turn
// and flattens it to one line — the anchor a later round needs in order to know
// what was actually run on this machine, which no regex over the prose recovers
// (the command is a JSON argument, not a sentence).
func harvestBashCommands(msgs []deepseek.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.Name != toolnames.ToolBash {
				continue
			}
			raw, ok := tc.Arguments[turnFoldCommandArg].(string)
			if !ok || raw == "" {
				continue
			}
			cmd := strings.Join(strings.Fields(raw), " ")
			if utf8.RuneCountInString(cmd) > turnFoldRecapChars {
				cmd = string([]rune(cmd)[:turnFoldRecapChars]) + "…"
			}
			out = append(out, cmd)
		}
	}
	return out
}

// harvestRecordIDs pulls every `#N` record id out of one text. The digit test
// is what keeps a markdown heading (`# `) from becoming an anchor.
func harvestRecordIDs(text string) []string {
	var out []string
	for i := 0; i+1 < len(text); i++ {
		if text[i] != turnFoldRecordIDPrefix[0] {
			continue
		}
		j := i + 1
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		if j > i+1 {
			out = append(out, text[i:j])
			i = j - 1
		}
	}
	return out
}

// oneLineRecap reduces a tool result to its first line, capped: enough to say
// whether the call passed or failed, and deliberately not more (the body is in
// the transcript the head names).
func oneLineRecap(content string) string {
	first := strings.TrimSpace(content)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	first = strings.Join(strings.Fields(first), " ")
	if utf8.RuneCountInString(first) > turnFoldRecapChars {
		first = string([]rune(first)[:turnFoldRecapChars]) + "…"
	}
	return first
}

// turnFoldConclusionClaim returns the CONCLUSION a folded turn's note recorded,
// or "" when the message is not such a note.
//
// It exists because the fold changes WHAT IS IN THE CONTEXT: a reader that
// looked for "the most recent non-trivial assistant text" (lastSubstantiveText,
// the wallclock cap's graceful finish) finds none in a conversation whose whole
// head has been folded — not because the work said nothing, but because what it
// said now lives in a note. This is the ONE accessor for that value, so the
// note's rendered shape and its reader cannot drift (Rule 14).
func turnFoldConclusionClaim(m deepseek.Message) string {
	if m.Role != roleUserMessage || !strings.HasPrefix(m.Content, turnFoldNoteHead) {
		return ""
	}
	for _, line := range strings.Split(m.Content, "\n") {
		if v, ok := strings.CutPrefix(line, turnFoldConclusionLabel); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
