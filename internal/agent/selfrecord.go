// selfrecord.go — SELF-RECORDING (第一期): the model writes its own round
// record, and the harness keeps that record alive.
//
// THE DESIGN, in one paragraph. At the end of every response the model appends
// a one-line [IMPORTANT] block — the round's summary, optionally followed by
// " | " and the single most dangerous-to-forget finding. The harness EXTRACTS
// that block from the SAME response that carries it: the answer body AND the
// model's reasoning (the two search spaces are not the same; a run that writes
// the block inside its thinking has still recorded it). Each extracted note is
// APPENDED to a per-session side-channel file, so the record outlives the
// transcript. The block is NEVER removed from, rewritten in, or moved out of
// its original message — the transcript is the evidence, and a transcript the
// harness edits is no longer evidence. What the harness does instead, at
// COMPACTION time, is twofold: a memory compaction may never consume a note
// block (the deletion pass skips it — context.go), and the notes that survived
// in the file are re-injected as ONE carrier message at the head of the
// compacted context, so the distilled record is still in front of the model
// after the history it came from has been folded away.
//
// NOT HERMES' SHAPE. The sibling implementation (a sibling agent implementation/
// hot_notes.py) STRIPS the block out of the visible text (strip_notes) and
// keeps only the side-channel file. gopi does the opposite, deliberately: here
// the block stays in the message, and the file is a durable COPY rather than a
// replacement — the step-1 constraint of this epic.
//
// WHAT THE SECOND PHASE OWNS (explicitly NOT here — the notes are recorded,
// nothing more): turning a note into a memory_store row (the note → memory
// funnel), keyword filtering of notes (Hermes' PITFALL_KEYWORDS /
// cross-session pitfalls.md index), and any cross-session aggregation. This
// file writes ONE file per session and reads nothing else.

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

const (
	// selfRecordOpenMarker / selfRecordCloseMarker delimit the block. They are
	// the WIRE CONTRACT the model is taught (selfRecordRule) and the parser
	// reads — one definition, two readers (Rule 14). The delimiters are on
	// their own lines so a block is findable in prose without a fragile
	// position rule.
	selfRecordOpenMarker  = "[IMPORTANT]"
	selfRecordCloseMarker = "[/IMPORTANT]"

	// selfRecordLineIndent is EVERYTHING that may precede a marker on its own
	// line and still leave the marker ANCHORED. Horizontal whitespace only: a
	// marker after any other character is the model NAMING the contract, not
	// writing a block (see selfRecordMarkerAt for the measured cost of the
	// position-blind scan this replaced).
	selfRecordLineIndent = " \t\r"

	// selfRecordFindingSeparator separates the mandatory round summary from the
	// optional finding inside the block, exactly as documented to the model.
	selfRecordFindingSeparator = " | "

	// selfRecordMarker heads the RE-INJECTED carrier message: the note block
	// that survived a compaction. THE VALUE IS NOT DEFINED HERE (Rule 14): it is
	// sessionevent.HarnessSelfRecordMarker, in the leaf package the session-log
	// readers also import — so a reader and the replay answer "whose row is
	// this?" with the same list the harness writes from. It is a member of the
	// anchor policy's marker set (harnessMessageMarkers, context.go —
	// participate, do not merge: see the rationale there).
	selfRecordMarker = sessionevent.HarnessSelfRecordMarker

	// selfRecordFileName is the side-channel file inside the run's session
	// directory. A sibling of the session JSONL and the raw log, in the SAME
	// directory sessionDirForID resolves — so the record's lifetime is exactly
	// the run directory's lifetime (no new tree, no new reclaimer: Rule 35).
	selfRecordFileName = "self-record.md"

	// selfRecordDirPerm / selfRecordFilePerm are the modes of a freshly created
	// record directory and file.
	selfRecordDirPerm  = 0o755
	selfRecordFilePerm = 0o644

	// selfRecordTurnStampFmt is the TURN stamp every newly recorded line opens
	// with: `t<turn> <note>`.
	//
	// WHY A TURN AND NOT THE ROUND IT USED TO BE (operator's ruling, 2026-11-10).
	// A round is one HTTP request/response; a TURN is one COMPLETE INPUT — the
	// person typed a line, `gopi notify` delivered one, or the surface typed one
	// for itself. The record's unit was wrong for its own reader: a turn that ran
	// twelve tool rounds produced twelve notes, all stamped with twelve different
	// rounds that no surface addresses. `gopi session replay --turn N` and
	// `gopi session log <id> --turns` number the axis a reader actually uses, so
	// the file says THAT number.
	//
	// THE BOUNDARIES ARE NOT RE-DERIVED HERE (Rule 14). "Which turn is a note
	// written in?" is sessionevent.IsTurnOpeningRow — the same predicate
	// internal/gopilog's reader turns into TurnBoundary, the same one
	// SessionTurnBounds numbers, the same one the chat and web surfaces print.
	// The file's writer reaches it through SessionLogger.TurnsSoFar (the writer
	// half of that count) plus the run's own baseline (see noteTurnNumber,
	// selfrecord_noteindex.go), so no second counter exists anywhere.
	//
	// The number is the SAME value the note index stores in
	// chat_notes.turn_index — ONE evaluation of noteTurnNumber, two writers — so
	// a line read off the file and a row read out of the index compare by eye and
	// by test.
	//
	// HISTORICAL LINES ARE NEVER REWRITTEN (the record is append-only evidence),
	// so the file holds THREE shapes — `t<n> `, the round stamp `r<n> ` every
	// line written between 2026-11-08 and this change carries, and the original
	// `- ` — and every reader goes through ONE parser, selfRecordNoteFromLine
	// (Rule 14).
	selfRecordTurnStampFmt = "t%d "

	// selfRecordNoTurnStamp is what a note recorded with NO turn carries — the
	// explicit ABSENCE, never a fabricated turn. It is the file's counterpart of
	// sessionevent.RoundIndexUnknown ("no number could be resolved"), so the two
	// layers degrade in the same way and neither invents a turn 1.
	selfRecordNoTurnStamp = "t? "

	// selfRecordRoundStampFmt is the RETIRED round stamp — the shape every line
	// written during the round-stamp epic opens with: `r<round> <note>`.
	//
	// IT IS NEVER WRITTEN ANY MORE (selfRecordTurnStampFmt replaced it as the
	// stamp of a new line) and it is still a constant because the PARSER reads
	// it: the record is append-only evidence, so those lines are read forever.
	// Keeping the spelling here rather than typing "r" inside the parser is
	// Rule 1 (a meaningful string literal has one home) and Rule 13 (a constant
	// exists to be REFERENCED).
	selfRecordRoundStampFmt = "r%d "

	// selfRecordNoRoundStamp is the round stamp's absence form, read-only for
	// the same reason (`r? <note>`): it is never written now, and lines carrying
	// it are history.
	selfRecordNoRoundStamp = "r? "

	// selfRecordLegacyLineMark is what EVERY line written before any stamp
	// existed opens with. It is not written any more — only read (the historical
	// half of the same append-only file).
	selfRecordLegacyLineMark = "- "

	// maxSelfRecordNoteChars caps ONE note. The block is meant to be a line;
	// a model that pastes an essay into it must not be able to spend the whole
	// re-injection budget on one round. The cap is applied to the recorded
	// copy ONLY — the message keeps whatever the model wrote (never rewrite
	// the transcript).
	maxSelfRecordNoteChars = 500

	// selfRecordTruncationMarkFormat is the tail a CUT note ends with, and the
	// reason it is not an ellipsis is measured: the previous tail was a bare
	// "…", which is EXACTLY what an author types to mean "and so on". On this
	// host's 5,918 recorded note lines only ONE was actually cut, while 1,373
	// END with an author-written "…" — a 1:1,373 ratio, i.e. a mark no reader
	// could tell apart from the text's own punctuation. The replacement states
	// the LOSS and its size, so "the author stopped here" and "the harness cut
	// this" are different strings. %d is the number of runes DROPPED.
	selfRecordTruncationMarkFormat = " …[truncated %d chars]"

	// selfRecordTruncationSettleRounds bounds the ONE fixed-point loop in
	// selfRecordTruncatedNote: the tail's width depends on how many digits the
	// loss count has, so the kept width and the count are solved together. Two
	// rounds settle every reachable case (the count grows a digit at 10 and 100,
	// both far below the cap); the bound exists so a future constant change
	// cannot turn a settle into an infinite loop, and a run that exhausts it
	// keeps the LAST answer rather than looping.
	selfRecordTruncationSettleRounds = 4

	// selfRecordTruncationMinKeptRunes is the floor on what survives a cut. It
	// is 1 because a note reduced to its tail alone would say "truncated N
	// chars" and nothing else — the record would keep the marker and lose the
	// finding, which is the failure the note exists to prevent. Unreachable
	// while maxSelfRecordNoteChars is 500 (the tail is ~20 runes wide), stated
	// so the arithmetic below cannot silently keep nothing.
	selfRecordTruncationMinKeptRunes = 1

	// selfRecordPlaceholder is the SHAPE the contract message shows the model.
	// It must not be recorded as a note: the rule text quotes the delimiters, so
	// a model that echoes the shape (or the harness's own rule message, if it is
	// ever scanned) would otherwise write the placeholder into the record file.
	// One definition, used by both the rule text and the recorder.
	selfRecordPlaceholderShape = "<round summary>"

	// maxSelfRecordNotes is the CEILING on how many notes the run holds for
	// re-injection. It is NOT a retention policy any more, and that change is
	// the point (operator's ruling, 2026-11-08): the old value of 50 silently cut
	// every note older than the last 50 rounds, so a session whose conversation
	// had been reduced to its notes lost the first half of its own record —
	// "compaction" reappearing under a different name. Nothing realistic is
	// truncated now.
	//
	// THE CAPACITY DERIVATION, because a ceiling without one is a magic number:
	//
	//   - ONE note is capped at maxSelfRecordNoteChars = 500 chars (above), i.e.
	//     ≈ 125 tokens on this package's own chars/4 basis — that is the WORST
	//     case per note, not the typical one.
	//   - MEASURED on this host (622 session records under ~/.gopi/sessions/*/
	//     self-record.md): 5686 notes total, mean 336 chars, p90 503, and the
	//     largest single run holds 805 notes = 349,593 chars ≈ 87,398 tokens —
	//     i.e. 44% of the default 200,000 window, which is exactly the state the
	//     context-pressure warning exists to surface.
	//   - 2000 is 2.5× that largest measured record: at the measured MEAN it is
	//     ≈ 672,000 chars ≈ 168,000 tokens, and at the WORST case (every note at
	//     the 500-char cap) ≈ 1,000,000 chars ≈ 250,000 tokens — which EXCEEDS
	//     one window. That is deliberate and it is why the numbers are written
	//     out here: past a certain size, cutting the OLDEST notes on a silent
	//     threshold is the very defect this epic removes. What bounds the growth
	//     instead is (a) the context-pressure notice, which tells the operator
	//     the window is filling and to start a new session (context_pressure.go),
	//     and (b) for a run that opts into automatic reduction
	//     (GOPI_CONTEXT_AUTOREDUCE=1), the trim/compaction path itself.
	//   - The value therefore stops ONE thing only: an unbounded in-memory slice
	//     on a pathological run (a bug that records a note per tool round), which
	//     would otherwise grow without any ceiling at all.
	maxSelfRecordNotes = 2000
)

// selfRecordRule is the model-facing contract: HOW to record, and WHAT the
// harness does with the block.
//
// IT IS A HARNESS-INJECTED MESSAGE, NOT A KERNEL PROMPT SECTION — and that
// placement is measured, not stylistic. The kernel system prompt sits at
// 4488/4500 characters (the density gate, prompt_density_budget_test.go) with
// 12 characters of headroom, so ANY new rule appended there either breaks the
// gate or forces the removal of a rule somebody else's task depends on. This
// rule is exactly the shape the repo already solves this way: a one-shot
// harness user message (the taskStartSearchMessage precedent), which costs zero
// kernel budget, reaches the model before its first response, and — because it
// is injected through the SAME harness channel — is already covered by the
// anchor policy that keeps framework messages from becoming compaction floors.
//
// The wording states the two properties the epic's acceptance rests on: the
// block survives compaction (so writing it is not wasted work), and it must be
// written EVERY round (a record kept only when the run feels like it is not a
// record).
var selfRecordRule = harnessMessagePrefix + "SELF-RECORDING: end EVERY response (from this one on) with a one-line block:\n" +
	selfRecordOpenMarker + "\n" +
	selfRecordPlaceholderShape + selfRecordFindingSeparator + "<significant finding — optional>\n" +
	selfRecordCloseMarker + "\n" +
	"One line: what this round accomplished and the current status, plain prose. After \" | \" add a finding ONLY if losing it would be dangerous or expensive — a constraint, a failure diagnosis, an environment fact, a decision. Nothing significant → the summary alone. Keep it under " +
	fmt.Sprint(maxSelfRecordNoteChars) + " characters; no other formatting inside the block; do not repeat a finding you already recorded.\n" +
	"The harness copies the block into this session's own record file every round and REPLAYS it after any context compaction, so the block survives the history being folded away — it is the one place your own summary is guaranteed to outlive the round. Write it every round; a round without it records nothing."

// chatSelfRecordRule is the SAME contract addressed to the CHAT surface, and it
// is appended to the chat system prompt's tail rather than injected as a
// harness user message.
//
// WHY A DIFFERENT CARRIER (measured, not stylistic). The task path could inject
// (selfRecordRule) as a second user message because the task path REBUILDS its
// context from the row; the chat path cannot — `gopi chat` sends the operator's
// turn as the FIRST user message of every request, and consecutive user text
// messages are COALESCED into one (internal/deepseek/claude_client.go), so an
// injected message would arrive as an extra text block of the operator's own
// turn — the boundary `gopi chat` promises would be gone, exactly the reason
// maybeInjectSelfRecordRule refuses a plain prompt. The system prompt has no
// such coupling: it is the run's STABLE prefix, byte-identical turn to turn.
//
// WHY THE TAIL. The chat system prompt is assembled once per turn from the same
// parts (loop.go chatSystemPromptServed / chat_ops.go chatOpsSystemPrompt); a
// block appended AFTER every existing part cannot move any earlier block's
// bytes, so the prompt cache's prefix (system + tools, ~31k tokens) keeps its
// stability for every block the run already had. That claim is measured, not
// assumed: the A/B comparison lives in
// devdoc/chat-selfrecord-injection-cache-cost.md (per-request deltas from the
// session JSONL, first-token latency from the raw-log byte arrival sequence).
//
// The WORDING differs from the task rule in exactly one place — it says the
// record is written beside this run's transcript, because a chat run's
// session directory is the only place it keeps state (Rule 37: a chat has no
// branch and no worktree).
var chatSelfRecordRule = harnessMessagePrefix + "SELF-RECORDING: end EVERY response (from this one on) with a one-line block:\n" +
	selfRecordOpenMarker + "\n" +
	selfRecordPlaceholderShape + selfRecordFindingSeparator + "<significant finding — optional>\n" +
	selfRecordCloseMarker + "\n" +
	"One line: what this round accomplished and the current status, plain prose. After \" | \" add a finding ONLY if losing it would be dangerous or expensive — a constraint, a failure diagnosis, an environment fact, a decision. Nothing significant → the summary alone. Keep it under " +
	fmt.Sprint(maxSelfRecordNoteChars) + " characters; no other formatting inside the block; do not repeat a finding you already recorded.\n" +
	"The harness copies the block into this session's own record file (`" + selfRecordFileName + "`, beside this run's transcript) every round, so the block outlives the turn that wrote it. Write it every round; a round without it records nothing."

// maybeAppendChatSelfRecordRule appends the chat contract to a rendered chat
// system prompt. It is the ONE injection point, called by BOTH chat assemblies
// (assembleChatSystemPrompt and chatOpsSystemPrompt) and by nothing else, so
// "the chat prompt carries the record contract" has one definition.
//
// It APPENDS, never prepends: the operator's reply-language directive is the
// HEAD of the chat prompt (systemPromptWithOutputLanguage) and the persona body
// must start at the very next block (TestChatPromptCarriesDirectiveAtHead). The
// tail is also the half whose bytes no earlier block can depend on, which is
// what keeps the cache prefix of every pre-existing part unchanged.
//
// An empty prompt stays empty (no stray separator), mirroring the section
// contract the block assemblies already honour.
func maybeAppendChatSelfRecordRule(served string) string {
	if served == "" {
		return served
	}
	if strings.Contains(served, chatSelfRecordRule) {
		return served
	}
	return served + promptSectionSeparator + chatSelfRecordRule
}

// selfRecordState is the per-agent self-recording state: where the file is,
// which notes have already been recorded, and whether the carrier was injected.
//
// A field on Agent (like every other harness-injection latch in this package:
// memorySearched, consultArmed, hotResident...) rather than a global, so two
// agents in one process can never share a record file.
type selfRecordState struct {
	// dir is the run's session directory — resolved ONCE, lazily, on the first
	// note. Empty until then (an agent that never records never resolves or
	// creates anything).
	dir string
	// notes are the notes ALREADY recorded this run, in order, for the
	// re-injection carrier. They are held in memory because the carrier must be
	// built without reading the file back (the file is the durable copy, the
	// slice is the live one).
	notes []string
	// noteStored is the phase-2 funnel's run-local dedup set (selfnotecapture.go):
	// the note texts this run has already turned into memory candidates. It lives
	// in THIS struct because it is state ABOUT the same record — one per run, one
	// owner — and adding a second field to Agent would have moved every line of
	// loop.go below the call site (the capture is wired in with zero net line
	// change; see the report). That argument is now churn, not a gate: that check
	// (the devdoc line-anchor gate) went with internal/docclass in 5a35f61, so no
	// `loop.go:NNN` anchor is resolved, and the census that replaced it is gone too
	// (78bdf05a, 2026-11-05: the documentation-citation family was deleted whole). No file, no
	// lock: the whole set dies with the run (Rule 35).
	noteStored map[string]bool
	// replayCaptureRecorded is the run-local "the replay capture refusal is
	// already on record" flag (memory_replay_guard.go: replayCaptureSkipped).
	// It lives here for the same reason noteStored does — one struct per run,
	// already owned by the capture path — so wiring the gate in adds no Agent
	// field and moves no line of loop.go (Rule 4).
	replayCaptureRecorded bool
}

// recordSelfNotes extracts every [IMPORTANT] block from one round's response
// and records it twice in ONE call: appended to the session's record file (the
// EVIDENCE layer, unchanged since 第一期) and inserted into the round-level
// retrieval index chat_notes (selfrecord_noteindex.go — the SEARCHABLE layer
// over the same notes). The file keeps its exact semantics: a failed index
// write never removes a line, reorders one, or delays its append. It returns
// the notes it recorded (nil when the round carried none).
//
// The two search spaces are the answer body and the model's reasoning, tried in
// that order: the block usually appears in the body, but a run that reasons its
// way to the block and then answers briefly has still recorded it, and losing
// those notes would make the contract depend on WHERE the model happened to
// write. A block found in the body is not counted twice when the same text also
// appears in the reasoning — the body is authoritative and the reasoning is the
// fallback.
//
// Best-effort end to end: extraction never mutates the round's own message, and
// a failed write is logged and dropped, never fatal (Rule 15b — the run's answer
// is worth more than its side-channel note).
func (a *Agent) recordSelfNotes(content, reasoning string) []string {
	// A REPLAY RECORDS NOTHING (2026-10-25). This function is the ONE write point
	// for a round's notes, and BOTH of its halves are writes: the append to the
	// session's record file and the INSERT into the chat_notes retrieval index. A
	// replay is re-running a conversation that already happened, so it must reach
	// neither — and it DID reach both before this gate: the downstream auto-memory
	// refusal (selfnotecapture.go) fired one line per note, which was the visible
	// symptom, while the record file and the index were written regardless.
	//
	// The record is the evidence file (recordReplayMemoryRefusal) — one line per
	// round at most, so a long recording cannot flood it — and the return is nil,
	// which is also what tells captureSelfNotes there is nothing to consider.
	if replaymode.Active() {
		a.recordReplayMemoryRefusal("record a round's [IMPORTANT] notes into this run's record file and note index")
		return nil
	}
	notes := extractSelfNotes(content)
	if len(notes) == 0 {
		notes = extractSelfNotes(reasoning)
	}
	if len(notes) == 0 {
		return nil
	}
	var recorded []string
	for _, note := range notes {
		capped, dropped := collapseSelfNoteCount(note)
		if capped == "" || isSelfRecordPlaceholder(capped) {
			continue
		}
		// THE LOSS IS NO LONGER SILENT (Rule 15). The tail inside the note says
		// a cut happened; this line says HOW MUCH, in the run's own log, so an
		// operator reading the log does not have to open the record file and do
		// arithmetic against the model's original text — which the log does not
		// even carry.
		if dropped > 0 {
			a.logSelfRecordTruncation(dropped, utf8.RuneCountInString(capped))
		}
		recorded = append(recorded, capped)
	}
	if len(recorded) == 0 {
		return nil
	}
	// THE TURN IS RESOLVED ONCE, HERE, and handed to BOTH writers. The file line
	// (`t<turn> <note>`) and the index row (chat_notes.turn_index) are two copies
	// of ONE fact, so a second evaluation would let them disagree — the session's
	// turn count does move between two calls in the same turn (a delivery lands,
	// the surface types a line).
	turn := a.noteTurnNumber()
	if err := a.appendSelfRecordFile(turn, recorded); err != nil {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ self-record: could not append %d note(s): %v", len(recorded), err)
		}
	}
	// The SAME call indexes what it just recorded (selfrecord_noteindex.go):
	// the file is the evidence, the table is the retrieval layer over it. The
	// index write is best-effort — it is reported and never fatal, and it never
	// changes what the file holds. The directory is the one the append already
	// resolved: this is ONE write point, so the stored path is by construction
	// the file a reader would open on this machine.
	if dir, err := a.selfRecordDir(); err != nil {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ self-record: notes recorded but not indexed: %v", err)
		}
	} else if stored, err := a.indexSelfNotes(turn, recorded, dir); err != nil {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ self-record: indexed %d of %d note(s): %v", stored, len(recorded), err)
		}
	} else if a.Logger != nil {
		a.Logger.Printf("🔎 self-record: %d note(s) indexed for search (session %s)", stored, a.storeSessionID())
	}
	a.selfRecord.notes = append(a.selfRecord.notes, recorded...)
	if len(a.selfRecord.notes) > maxSelfRecordNotes {
		a.selfRecord.notes = a.selfRecord.notes[len(a.selfRecord.notes)-maxSelfRecordNotes:]
	}
	if a.Logger != nil {
		a.Logger.Printf("📝 self-record: %d note(s) appended (%d held for replay)", len(recorded), len(a.selfRecord.notes))
	}
	return recorded
}

// extractSelfNotes pulls the inner text of every [IMPORTANT]…[/IMPORTANT] block
// out of one text, in order.
//
// BOTH DELIMITERS ARE LINE-ANCHORED — the one position rule this extractor has,
// and it is not cosmetic. The scan used to be a bare strings.Index on the open
// marker paired with the next occurrence of the closer ANYWHERE after it, so the
// START of a "block" could be the moment the model MENTIONS the contract inside a
// sentence. Measured on session 9adf61bc-c3a9-40be-9367-13dba0ef71b2: lines 1–3
// of that run's self-record.md are mid-sentence fragments of the answer body
// (the first 434 and the first 1,126 runes of the round's real note cut away),
// because the model's prose discussed `[IMPORTANT]` before writing its own block.
// The durable file kept the prose and threw the record away.
//
// The taught shape puts each delimiter alone on its line (selfRecordRule), so a
// REAL block always satisfies the anchor and a MENTION almost never does. What
// may precede a marker on its line is horizontal whitespace only
// (selfRecordLineIndent) — an indented block is still a block.
//
// A block whose closer is MISSING is not extracted — a half-written block is far
// more likely to be the model narrating the instructions than a real record, and
// inventing one would put the harness's guess into the durable file. The anchor
// rule makes that refusal stricter in the honest direction: a mention-only
// answer now records NOTHING instead of a fragment.
func extractSelfNotes(text string) []string {
	if text == "" || !strings.Contains(text, selfRecordOpenMarker) {
		return nil
	}
	var notes []string
	from := 0
	for {
		open := selfRecordMarkerAt(text, selfRecordOpenMarker, from)
		if open < 0 {
			return notes
		}
		bodyAt := open + len(selfRecordOpenMarker)
		closeAt := selfRecordMarkerAt(text, selfRecordCloseMarker, bodyAt)
		if closeAt < 0 {
			return notes
		}
		// The extracted body. A block is opened on its own line in the taught
		// form, so the text between the delimiters starts with that line's
		// newline — trimmed along with any indentation.
		if body := strings.TrimSpace(text[bodyAt:closeAt]); body != "" {
			notes = append(notes, body)
		}
		from = closeAt + len(selfRecordCloseMarker)
	}
}

// selfRecordMarkerAt returns the offset of the first occurrence of marker at or
// after from that is LINE-ANCHORED — i.e. only selfRecordLineIndent precedes it on
// its line — or -1 when there is none.
//
// It is the ONE position rule (Rule 14): extractSelfNotes reads it for both
// delimiters, so "what counts as a block" cannot drift between the open scan and
// the close scan. It is deliberately not shared with
// selfnotecaptureresponse.go's visibleResponseBody, which pairs markers by
// presence to decide what to STRIP from a stored row (a strip that misses an
// inline mention only leaves prose in a memory row; an extract that invents one
// writes garbage into the durable record).
func selfRecordMarkerAt(text, marker string, from int) int {
	for from <= len(text) {
		i := strings.Index(text[from:], marker)
		if i < 0 {
			return -1
		}
		at := from + i
		lineStart := strings.LastIndexByte(text[:at], '\n') + 1
		if strings.TrimLeft(text[lineStart:at], selfRecordLineIndent) == "" {
			return at
		}
		from = at + 1
	}
	return -1
}

// collapseSelfNote flattens a block body to ONE line and caps it: the file and
// the carrier are both line-oriented, and a multi-line note would break the
// "one round, one line" contract the reader (and any future phase-2 keyword
// pass) relies on. The cap is a rune-boundary truncation, so a CJK note is
// never cut mid-character.
//
// It is the ONE flattener; the cut itself lives in collapseSelfNoteCount, which
// also says HOW MUCH was dropped so the caller can report it (Rule 15: a silent
// cut is the defect this epic closes).
func collapseSelfNote(note string) string {
	capped, _ := collapseSelfNoteCount(note)
	return capped
}

// collapseSelfNoteCount is collapseSelfNote plus the loss count: it returns the
// recorded form and the number of RUNES the cap dropped (0 when nothing was cut,
// in which case the text is returned exactly as it was flattened).
//
// THE TAIL IS EXPLICIT AND BUDGETED. Two constraints decide its shape, and both
// are measured elsewhere in this file:
//
//   - It must not look like the author's own ellipsis. The previous tail was a
//     bare "…", and on this host 1,373 of 5,918 recorded lines end with an
//     author-written "…" while exactly ONE was actually cut — a mark a reader
//     cannot tell apart from the text's punctuation is no mark.
//   - It must fit INSIDE maxSelfRecordNoteChars, not beside it. turnfold.go
//     derives turnFoldNoteBodyBudget = maxSelfRecordNoteChars × turnFoldBodyNotes,
//     so a note that ran to 500 runes PLUS a tail would silently spend budget the
//     fold's arithmetic believes is reserved.
//
// The two are solved together because the tail's own width depends on the number
// it states ("9 chars" is narrower than "10 chars"): the kept width is
// recomputed from the current tail until it stops moving (bounded by
// selfRecordTruncationSettleRounds, which keeps the LAST answer rather than
// looping).
func collapseSelfNoteCount(note string) (string, int) {
	flat := strings.Join(strings.Fields(note), " ")
	if flat == "" {
		return "", 0
	}
	total := utf8.RuneCountInString(flat)
	if total <= maxSelfRecordNoteChars {
		return flat, 0
	}
	runes := []rune(flat)
	kept := maxSelfRecordNoteChars
	for round := 0; round < selfRecordTruncationSettleRounds; round++ {
		want := maxSelfRecordNoteChars - utf8.RuneCountInString(selfRecordTruncationMark(total-kept))
		if want < selfRecordTruncationMinKeptRunes {
			want = selfRecordTruncationMinKeptRunes
		}
		if want == kept {
			break
		}
		kept = want
	}
	dropped := total - kept
	return string(runes[:kept]) + selfRecordTruncationMark(dropped), dropped
}

// selfRecordTruncationMark renders the cut tail for ONE loss count — the single
// place the format constant is filled in (Rule 13: a constant bypassed by a
// literal is a defect).
func selfRecordTruncationMark(dropped int) string {
	return fmt.Sprintf(selfRecordTruncationMarkFormat, dropped)
}

// logSelfRecordTruncation reports ONE cut note on the run's log. It is a method
// rather than an inline printer so the nil-Logger case (a minimal test agent, a
// headless run) is decided in ONE place: a run with no logger still records the
// note — the tail inside the line is the durable half of the report — and this
// is the volatile half.
func (a *Agent) logSelfRecordTruncation(dropped, kept int) {
	if a == nil || a.Logger == nil {
		return
	}
	a.Logger.Printf("✂️ self-record: note cut to the %d-char cap — %d char(s) dropped (recorded %d, tail %q)",
		maxSelfRecordNoteChars, dropped, kept, selfRecordTruncationMark(dropped))
}

// selfRecordDir resolves (once) and creates the run's record directory: the
// SAME per-run session directory the session JSONL and the raw log live in
// (sessionDirForID — one resolution, Rule 14), so the record is swept by
// whatever already owns that tree and adds no new reclaimer (Rule 35).
//
// A session with no SessionLog (a minimal test agent) has no run directory to
// hang the file on, and that is reported rather than guessed at: the record is
// skipped, the notes stay in memory for the re-injection carrier, and the
// caller logs it.
func (a *Agent) selfRecordDir() (string, error) {
	if a.selfRecord.dir != "" {
		return a.selfRecord.dir, nil
	}
	if a.SessionLog == nil {
		return "", fmt.Errorf("self-record: no session logger — nowhere to keep a per-session record")
	}
	dir, err := sessionDirForID(a.SessionLog.SessionID(), a.SessionLog.TaskID())
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, selfRecordDirPerm); err != nil {
		return "", fmt.Errorf("self-record: cannot create %s: %w", dir, err)
	}
	a.selfRecord.dir = dir
	return dir, nil
}

// appendSelfRecordFile appends one line per note to the session's record file.
// The file is opened O_APPEND for every write: the record is an append-only log
// (a note already on disk is history — nothing rewrites it), and a crash
// between rounds can lose at most the current round's note.
//
// turn is the number the caller resolved ONCE for this turn (recordSelfNotes
// evaluates noteTurnNumber a single time and hands the same value to the index
// writer), so a line reads `t<turn> <note>` — the same number the searchable row
// carries, and the same number `gopi session log --turns` / `session replay
// --turn N` give the turn this note was written in. A turn that could not be
// resolved (no session log) is stored as selfRecordNoTurnStamp, the explicit
// absence, and never as a made-up turn.
func (a *Agent) appendSelfRecordFile(turn int, notes []string) error {
	dir, err := a.selfRecordDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, selfRecordFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, selfRecordFilePerm)
	if err != nil {
		return fmt.Errorf("self-record: cannot open %s: %w", path, err)
	}
	var b strings.Builder
	for _, note := range notes {
		b.WriteString(selfRecordLinePrefix(turn))
		b.WriteString(note)
		b.WriteString("\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close() //stylegate:ignore the write already failed and is reported below; a close error here has no separate meaning
		return fmt.Errorf("self-record: cannot append to %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("self-record: cannot close %s: %w", path, err)
	}
	return nil
}

// selfRecordLinePrefix renders the stamp a newly recorded line OPENS with:
// `t<turn> ` — the same turn the index row stores (selfRecordTurnStampFmt is the
// one spelling, Rule 1/13).
func selfRecordLinePrefix(turn int) string {
	if turn < sessionevent.RoundIndexBase {
		return selfRecordNoTurnStamp
	}
	return fmt.Sprintf(selfRecordTurnStampFmt, turn)
}

// SelfRecordNoteFromLine is selfRecordNoteFromLine for the ONE reader outside
// this package — the end-to-end wire test (testsuite), which compares the file
// the run wrote against what the fold put on the wire. It is exported because a
// SECOND definition of "what a record line is" over there is exactly the drift
// Rule 14 forbids: the shape would be parsed twice and disagree the day the
// stamp changes.
func SelfRecordNoteFromLine(line string) (int, string, bool) {
	return selfRecordNoteFromLine(line)
}

// selfRecordNoteFromLine is the ONE parser of a record-file line, and it is why
// this epic costs no migration: the file is append-only evidence, so it holds
// THREE shapes and every reader goes through this function.
//
//	`t7 a note`      → 7,   "a note"      (written from the TURN epic on)
//	`t? a note`      → 0,   "a note"      (no turn could be resolved)
//	`r9 a note`      → 9,   "a note"      (the RETIRED round stamp — history)
//	`r? a note`      → 0,   "a note"      (no round could be resolved — history)
//	`- a note`       → 0,   "a note"      (every line written before any stamp)
//	anything else    → 0,   "", false     (a blank tail, prose — not a note)
//
// The number is returned as sessionevent.RoundIndexUnknown when the line says
// nothing about one — the SAME absence value the two stamp families share, so a
// caller never has to ask which family it read. THE UNITS ARE NOT THE SAME and
// the parser cannot pretend otherwise: a returned number out of an `r` line is a
// ROUND and out of a `t` line is a TURN. A caller that needs the distinction asks
// selfRecordStampUnitOf (below) — one parse, two questions.
//
// The historical mark is tried LAST so a note that legitimately begins with
// "- " keeps its dash rather than being mangled, and the round stamp is tried
// after the turn stamp so a note whose text begins "r…" cannot shadow a turn
// line (the first letter decides, and both are one character wide).
func selfRecordNoteFromLine(line string) (int, string, bool) {
	line = strings.TrimRight(line, "\r")
	if n, rest, ok := selfRecordStampedNumber(line, selfRecordTurnStampFmt); ok {
		return n, rest, true
	}
	if n, rest, ok := selfRecordStampedNumber(line, selfRecordRoundStampFmt); ok {
		return n, rest, true
	}
	if rest, ok := strings.CutPrefix(line, selfRecordLegacyLineMark); ok {
		return sessionevent.RoundIndexUnknown, rest, true
	}
	return sessionevent.RoundIndexUnknown, "", false
}

// selfRecordStampedNumber reads ONE numbered stamp family off a line.
//
// format is the family's spelling (`t%d ` or `r%d `, Rule 1/13), and it supplies
// everything the match needs: its FIRST byte is the one-character mark, and the
// space it ends with is the separator. A line either carries that mark followed
// by a number (or the family's `?`) and that space, or it is not this family's
// line — "round 7 is prose" and "t7a note" are refused here and fall through to
// the next family rather than being read as a stamp.
//
// IT EXISTS SO THE TWO FAMILIES CANNOT DRIFT: the marks and the separator are
// derived from the format constants, so a change to a stamp's spelling moves the
// parser with it. One function, two formats, three shapes read.
func selfRecordStampedNumber(line, format string) (int, string, bool) {
	mark := format[0]
	sep := format[len(format)-1]
	if len(line) < 3 || line[0] != mark {
		return 0, "", false
	}
	rest := line[1:]
	sp := strings.IndexByte(rest, sep)
	if sp <= 0 {
		return 0, "", false
	}
	digits := rest[:sp]
	if digits == selfRecordAbsentNumberMark {
		return sessionevent.RoundIndexUnknown, rest[sp+1:], true
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < sessionevent.RoundIndexBase {
		return 0, "", false
	}
	return n, rest[sp+1:], true
}

// selfRecordAbsentNumberMark is what a stamp carries instead of a number when
// the writer could not resolve one (`t? ` / `r? `). Named once because BOTH the
// turn stamp and the round stamp spell the absence the same way and the parser
// compares against it (Rule 1b: a comparison target is never typed inline).
const selfRecordAbsentNumberMark = "?"

// selfRecordStampUnitRound is the unit a number parsed out of a RETIRED round
// stamp carries. The turn stamp's unit is the record's own unit and needs no
// constant: it is what every line says now.
const selfRecordStampUnitRound = "round"

// SelfRecordStampUnitOf / SelfRecordStampUnitTurnWord are the EXPORTED face of
// the unit question, for the end-to-end wire test (testsuite) which reads a real
// run's file through the record layer's own parser rather than through a second
// definition of "what a stamp is" (Rule 14).
func SelfRecordStampUnitOf(line string) string { return selfRecordStampUnitOf(line) }

// SelfRecordStampUnitTurnWord is the unit word a line written from the turn epic
// on carries.
func SelfRecordStampUnitTurnWord() string { return selfRecordStampUnitTurn }

// selfRecordStampUnitOf reports which UNIT the number selfRecordNoteFromLine
// returned is in — "turn" for the shape written now, "round" for the stamp the
// epic retired, "" for a line that carries no number at all.
//
// IT IS A SECOND QUESTION ABOUT THE SAME PARSE, not a second parser: it reads
// the same mark off the same line and never touches the note. A reader that
// presents a historical number (a report, a rendered row) must be able to say
// WHICH axis it is on, or it would print a round as a turn — the exact silent
// substitution Rule 15 forbids.
func selfRecordStampUnitOf(line string) string {
	line = strings.TrimRight(line, "\r")
	if _, _, ok := selfRecordStampedNumber(line, selfRecordTurnStampFmt); ok {
		return selfRecordStampUnitTurn
	}
	if _, _, ok := selfRecordStampedNumber(line, selfRecordRoundStampFmt); ok {
		return selfRecordStampUnitRound
	}
	return ""
}

// selfRecordStampUnitTurn is the record's own unit, named for the callers that
// compare against selfRecordStampUnitOf's answer.
const selfRecordStampUnitTurn = "turn"

// maybeInjectSelfRecordRule injects the model-facing contract ONCE, before the
// first round. One-shot (the selfRecordRulePrompted latch), so it cannot spam a
// long run; best-effort — a run with no context is a no-op.
//
// plainPrompt is Agent.plainPrompt: a run whose first user message is the
// caller's text and nothing else (`gopi "<prompt>"`, loop.go RunPrompt). Such a
// run is NOT a task this harness dispatched, so the contract is not injected —
// and the reason is not tidiness, it is the wire: consecutive user text
// messages are COALESCED into one message
// (internal/deepseek/claude_client.go:560), so an injected second user message
// does not stay a second message. It arrives as an extra TEXT BLOCK of the
// prompt's own first message, and the prompt stops being the first user message
// "as typed" — the boundary `gopi -h` promises and
// TestRunPathPlainPromptIsVerbatim asserts (cli_runpath_boundary_rp1_test.go:246).
//
// The scope is the SAME predicate the EPIC framing already uses (!plainPrompt),
// so the two decisions cannot drift apart: one definition of "this run is a
// task", read in both places. The feature is untouched everywhere it applies —
// a `task run` child, a merger run, every in-process task caller.
func (a *Agent) maybeInjectSelfRecordRule(plainPrompt bool) bool {
	if plainPrompt {
		return false
	}
	if a.selfRecordRulePrompted || a.Ctx == nil {
		return false
	}
	a.selfRecordRulePrompted = true
	a.Ctx.AddUserMessage(selfRecordRule)
	// Auditability, identical to maybeInjectTaskStartSearch: an injected wire
	// message absent from the session JSONL is indistinguishable from one that
	// was never sent.
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), selfRecordRule)
	}
	if a.Logger != nil {
		a.Logger.Printf("📝 self-record: contract injected (one line per round, survives compaction)")
	}
	return true
}

// ensureSelfRecordPresent is the record's SURVIVAL GUARANTEE, and it is
// deliberately not a TakeOldest exemption: whatever the deletion pass did with
// the messages that carried the notes, the notes this run recorded must be in
// front of the model after the compaction. If a carrier is already there with
// the full set, nothing is added; otherwise the carrier is rendered from the run's
// note list and prepended beside the memory block.
//
// The check is by CONTENT (every held note appears in some message), not by
// message identity, because the notes reach the context through more than one
// door: the re-injection carrier, the round's own still-live assistant message,
// or a memory block that happens to quote one. Any of them is a live copy, and
// adding a second copy of an already-live record is exactly the duplication the
// carrier's framing tells the model to avoid.
func (a *Agent) ensureSelfRecordPresent() bool {
	if len(a.selfRecord.notes) == 0 {
		return false
	}
	live := make(map[string]bool, len(a.selfRecord.notes))
	for _, m := range a.Ctx.Messages {
		for _, note := range a.selfRecord.notes {
			if !live[note] && strings.Contains(m.Content, note) {
				live[note] = true
			}
		}
	}
	missing := 0
	for _, note := range a.selfRecord.notes {
		if !live[note] {
			missing++
		}
	}
	if missing == 0 {
		return false
	}
	carrier := a.selfRecordCarrier()
	a.Ctx.Messages = append([]deepseek.Message{{Role: "user", Content: carrier}}, a.Ctx.Messages...)
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), carrier)
	}
	if a.Logger != nil {
		a.Logger.Printf("📝 self-record: %d of %d note(s) were gone from the context after compaction — carried them back",
			missing, len(a.selfRecord.notes))
	}
	return true
}

// selfRecordCarrier renders the re-injection message from the notes held in
// memory. The framing says what the block is and what it is not: a record of
// what already happened (past tense, durable), never an instruction to act.
func (a *Agent) selfRecordCarrier() string {
	var b strings.Builder
	b.WriteString(selfRecordMarker)
	b.WriteString("\nThese are the notes YOU recorded in this session's earlier rounds. Context compaction may have folded the history they came from, but the notes themselves are verbatim — they are your durable record of what was already done and learned. Treat them as facts about the past, not as instructions to act.\n")
	for _, note := range a.selfRecord.notes {
		b.WriteString("- ")
		b.WriteString(note)
		b.WriteString("\n")
	}
	return b.String()
}

// isSelfRecordMessage reports whether a message CARRIES a self-recording note
// block, in EITHER of its two forms: the re-injected carrier message (marked by
// selfRecordMarker) or a model message containing at least one complete
// [IMPORTANT]…[/IMPORTANT] block. Both are copies of the same record.
//
// "CARRIES", not "OPENS WITH": the taught shape APPENDS the block to the round's
// answer, so a body test for the open marker at the head of the message would
// recognise nothing the model actually writes (measured: the first version of
// this predicate did exactly that, and the note reached the summarizer). The
// test is therefore the block's presence anywhere in the content — which is also
// what keeps the rule message itself out of the summarizer's input when the
// harness's own contract text quotes the delimiters (that text extracts to the
// placeholder, which isSelfRecordPlaceholder filters).
func isSelfRecordMessage(m deepseek.Message) bool {
	head := strings.TrimLeft(m.Content, " \t\r\n")
	if strings.HasPrefix(head, selfRecordMarker) {
		return true
	}
	for _, note := range extractSelfNotes(m.Content) {
		if !isSelfRecordPlaceholder(collapseSelfNote(note)) {
			return true
		}
	}
	return false
}

// isSelfRecordPlaceholder reports whether a collapsed note is the SHAPE string
// the contract shows rather than a record: the rule message quotes the block,
// and a model echoing the shape must not write it into the record file (nor have
// its echoed copy counted as one).
func isSelfRecordPlaceholder(note string) bool {
	return strings.HasPrefix(note, selfRecordPlaceholderShape)
}

// selfRecordPathFor returns the record file path for a session id, for readers
// outside the agent (a test, an operator, a future phase-2 pass). Exported
// through a function rather than a constant path so the layout keeps ONE
// definition (sessionDirForID + selfRecordFileName — Rule 14).
func selfRecordPathFor(sessionID, taskID string) (string, error) {
	dir, err := sessionDirForID(sessionID, taskID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, selfRecordFileName), nil
}

// configGopiDir is referenced here so the file's dependency on the ONE
// profile-resolution function is explicit and testable: the record tree is
// <config.GopiDir()>/<config.SessionsDirName>/<session-or-task-id>/… and this
// package never spells either component itself (internal/config owns both).
var _ = config.GopiDir
