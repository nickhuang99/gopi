package agent

// selfnotecapture.go — AUTO-MEMORY CAPTURE (epic 自动记忆, item 3/5): the round's
// own [IMPORTANT] notes become MEMORY CANDIDATES.
//
// WHERE THIS SITS. 第一期 (selfrecord.go) records every note the model writes
// into the per-session side-channel file `self-record.md` — all of them, unfiltered,
// because that file is the EVIDENCE of what the model recorded. This file is the
// funnel that runs beside that recording, on the response-end path (loop.go,
// immediately after recordSelfNotes), and turns the notes that carry CONTENT SHAPE
// into rows in distilled_qa, each with the session/round pointer item 2/5 added
// (memory_origin_stamp.go). The side channel keeps its phase-1 semantics
// untouched: the filter applies to the MEMORY step only, never to the record.
//
// THE OPERATOR'S CRITERION, and why the gate is a shape gate. The upstream ruling
// (devdoc/auto-memory-candidate.md, item 1/5) measured what a captured round looks
// like and ruled that a note is worth remembering by its FORM: a table, a topology
// diagram, or a substantial conclusion. The measured distribution of the round-level
// criterion over 220,106 local rounds (same document §1) is 1.31% — the shape gate
// is what keeps the ledger from becoming a log of status reports.
//
// NOT AN LLM JUDGMENT (a hard constraint of this epic). Every arm below is a
// property of the TEXT — a rune count, a glyph set, a pipe count — so the funnel
// costs no API call, cannot be prompt-injected, and is testable without a network.
//
// RELATION TO HERMES' GATE (devdoc/auto-memory-capture-quality.md carries the
// measured comparison and the ruling: our shape arms are a SUPERSET of
// `_has_structure()` and our length floor is BELOW `MIN_RESPONSE_LENGTH`; the
// schema here — the tier ladder, the two refusal arms hermes has no analogue for
// — is the extension). This file states the mechanism; the document carries the
// measurement.
//
// RULE 35: nothing here creates a process, a timer, a script, a lock file or a
// config key. It is a function call on a path the loop already walks, writing
// through the store the loop already calls.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nick/gopi/internal/tools"
)

const (
	// selfNoteMinRunes is the floor EVERY note must clear before any tier is
	// even considered: a note shorter than this is a status line ("merged, all
	// green"), not an experience.
	//
	// The value is measured, not tasted. On the operator's own record corpus
	// (67 sessions, 110 recorded notes — the whole of `~/.gopi/sessions/*/
	// self-record.md` at 2026-10-09) the note distribution is min 3 / p25 159 /
	// p50 237 / p90 407 / max 501 runes, and 24 of the 110 (22%) fall below this
	// floor. The captured candidate set is the upper part of that curve, never
	// the thin tail.
	selfNoteMinRunes = 150

	// selfNoteConclusionRunes is the LENGTH half of the "大段总结" arm: a written-out
	// conclusion is long. It sits at TWICE the floor on purpose — the floor
	// answers "is this a sentence about anything", this answer asks "did the
	// round write its finding out". 39 of the 113 measured notes clear it.
	selfNoteConclusionRunes = 300

	// selfNoteSummaryMarkers is the OTHER half of that arm, and it is what keeps
	// the arm from swallowing the shell reports. Length alone does not
	// distinguish a conclusion from a long status line — measured: of the 39
	// notes that clear the length, 11 are "everything ran, all green" with no
	// reasoning in them at all, and a length-only arm stored every one of them.
	//
	// A conclusion says HOW SOMETHING WAS ESTABLISHED: a cause, a consequence, a
	// decision, or the evidence for one. The list is the marker vocabulary the
	// notes actually use, counted over the same corpus (判定 21, 证据 10, 实测 9,
	// 根因 8, 证明 7, … — each of these appears in a REAL note, none is
	// invented), in both languages the corpus is written in.
	selfNoteSummaryMarkers = "因为,原因,根因,修法,修了,所以,因此,结论,判定,决定,证据,证明,实测,because,the fix,therefore,conclusion,decided,evidence"

	// selfNoteTablePipeMin is the "markdown table" arm: a markdown row needs one
	// leading and one trailing pipe plus one inner separator, so a cell divider
	// makes three. Counting pipes rather than matching `^\|.*\|$` is deliberate —
	// the notes are ONE LINE by contract (collapseSelfNote), so a multi-line
	// regex would be a check that cannot fire. Cost of the looser shape: a note
	// quoting three pipe characters is read as tabular; the notes measured carry
	// none, so the arm is conservative in the direction that loses nothing.
	selfNoteTablePipeMin = 3

	// selfNoteTopologyGlyphs is the "topology diagram" arm — box drawing and
	// arrows, the characters a hand-drawn graph is made of. The set is measured
	// against the corpus the same way the arms above are: 22 of the 113 notes
	// carry at least one of these, and they are the notes that DRAW a shape (a
	// directed edge, a tree branch) rather than merely talk about one.
	selfNoteTopologyGlyphs = "├└┌┐┘│─┴┬┼┃━┏┓┗┛→←↑↓↔⇄⇒⇐"

	// The tier values. They name the ARM that captured the note, so a reader of
	// the stored row's answer tail can see WHY the row exists; and the store's
	// own tag is carried in the source value below.
	selfNoteTierTopology   = "topology"
	selfNoteTierTable      = "table"
	selfNoteTierConclusion = "conclusion"

	// The refusal reasons. A refusal is REPORTED, never silent (Rule 15): the
	// caller logs the reason, so "why was my note not remembered" is answerable
	// from the run's own log rather than by re-deriving the gate.
	selfNoteRejectPlaceholder = "placeholder"
	selfNoteRejectMarker      = "marker-literal"
	selfNoteRejectTooShort    = "too-short"
	selfNoteRejectNoStructure = "no-structure"

	// selfNoteMarkerOpen / selfNoteMarkerClose are the delimiters the model
	// QUOTES when it talks about the contract. A note carrying one is a
	// narrative ABOUT the block, not the block's content — and the phase-1
	// extractor was measured to record exactly this shape (4 of 80 sampled notes
	// carried a marker literal). One spelling each, read by both phases: these
	// alias selfrecord.go's constants rather than re-typing them (Rule 14).
	selfNoteMarkerOpen  = selfRecordOpenMarker
	selfNoteMarkerClose = selfRecordCloseMarker

	// selfNoteMemoryTopic is the distilled_qa topic every captured note is
	// stored under. It is the searchable handle of the class ("what did my own
	// rounds record"), and the note itself is the body.
	selfNoteMemoryTopic = "self-record"

	// selfNoteMemorySource is the `sources` value: the row's provenance CLASS.
	// It is deliberately not a second spelling of the session/round pointer —
	// that pointer lives in the three origin columns (memory_origin.go), and two
	// copies of one fact is how they drift. This field answers a different
	// question: "was this row written by the model, the operator, or the funnel?"
	selfNoteMemorySource = "self-record(auto)"

	// selfNoteLogHeadRunes is how much of a note a log line quotes — enough to
	// recognize WHICH note is being discussed, short enough that a 500-rune note
	// does not dominate the run's log.
	selfNoteLogHeadRunes = 60
)

// selfNoteVerdict is the funnel's answer for ONE note: whether it becomes a
// memory candidate, which arm captured it, and — when it did not — why.
type selfNoteVerdict struct {
	// Capture reports whether this note is a memory candidate.
	Capture bool
	// Tier names the capturing arm ("" for a refusal).
	Tier string
	// Reason names the refusing arm ("" for a capture).
	Reason string
}

// classifySelfNote is the PURE predicate: note text in, verdict out. No state, no
// clock, no store — so the table-driven test in selfnotecapture_test.go can drive
// every arm with a literal and nothing else.
//
// The order of the arms is the order of their COST and their certainty:
//
//  1. a placeholder or a quoted marker never reaches the ledger (contamination);
//  2. below the floor there is nothing to classify;
//  3. then the three shape arms, strongest first — a diagram outranks a table
//     outranks a conclusion. The note is stored ONCE; the tier only names why.
//
// A note that clears the floor but carries no shape at all is REFUSED, and that
// is the operator's criterion working as written ("含图表 / 含拓扑图 / 大段总结"):
// the arms are a whitelist of forms, not a length gate with decoration.
//
// The conclusion arm is the ONLY one that needs two conditions, because it is the
// only one whose single condition (length) is also true of a shell report. That
// is measured, not assumed: see selfNoteSummaryMarkers.
func classifySelfNote(note string) selfNoteVerdict {
	trimmed := strings.TrimSpace(note)
	switch {
	case isSelfRecordPlaceholder(trimmed):
		return selfNoteVerdict{Reason: selfNoteRejectPlaceholder}
	case strings.Contains(trimmed, selfNoteMarkerOpen) || strings.Contains(trimmed, selfNoteMarkerClose):
		return selfNoteVerdict{Reason: selfNoteRejectMarker}
	case utf8.RuneCountInString(trimmed) < selfNoteMinRunes:
		return selfNoteVerdict{Reason: selfNoteRejectTooShort}
	case strings.ContainsAny(trimmed, selfNoteTopologyGlyphs):
		return selfNoteVerdict{Capture: true, Tier: selfNoteTierTopology}
	case strings.Count(trimmed, "|") >= selfNoteTablePipeMin:
		return selfNoteVerdict{Capture: true, Tier: selfNoteTierTable}
	case utf8.RuneCountInString(trimmed) >= selfNoteConclusionRunes && selfNoteCarriesSummary(trimmed):
		return selfNoteVerdict{Capture: true, Tier: selfNoteTierConclusion}
	}
	return selfNoteVerdict{Reason: selfNoteRejectNoStructure}
}

// selfNoteCarriesSummary reports whether a note REASONS — whether the length it
// carries is spent on how something was established (a cause, a consequence, a
// decision, the evidence for one) rather than on enumerating what ran. It is the
// second condition of the conclusion arm, and it is what separates a finding from
// a long status line; the marker vocabulary lives in selfNoteSummaryMarkers.
//
// It is a substring test over the note, lowercased: the corpus is bilingual, so
// both vocabularies are scanned in one pass, and the gate must stay a pure
// function of the text (no LLM, no state — an epic constraint).
func selfNoteCarriesSummary(note string) bool {
	low := strings.ToLower(note)
	for _, m := range strings.Split(selfNoteSummaryMarkers, ",") {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// selfNoteStoreArgs renders the store arguments for one captured note. The note
// IS the answer (scheme C, item 1/5: self-contained, thinking excluded) and the
// topic is the class handle.
//
// THE QUESTION IS A RETRIEVAL KEY, NOT A SECOND COPY OF THE BODY (defect fixed
// 2026-11-02, measured on the HP ledger). This used to return `note` for
// question AND answer, so every captured row carried question == answer — 2284
// of 2284 `self-record` rows and 8041 of 8043 `response` rows on that ledger.
// A question that IS the answer destroys the one field memory search matches
// against: the match surface becomes "your query versus two thousand characters
// of prose", so precision collapses and two records with unrelated content rank
// as near-duplicates the moment they share an opening line ("[TASK_LIST_BEGIN]"
// being the measured worst case). Deriving the key from the body costs nothing
// and loses nothing — the answer still holds every byte.
func selfNoteStoreArgs(note string) (topic, question, answer, source string) {
	return selfNoteMemoryTopic, memoryQuestionKey(note), note, selfNoteMemorySource
}

// memoryQuestionKeyRunes bounds the derived retrieval key. A memory question is
// a sentence a reader scans in a list, so the cap is well under a paragraph and
// comfortably over one clause; it is a named constant because it is a limit
// (Rule 1), and because the store's dedup and the search ranking both key on it.
const memoryQuestionKeyRunes = 160

// memoryQuestionKey derives the retrieval key from a captured body.
//
// THE RULE, and why it is not an LLM call: the epic's hard constraint is that
// every capture decision is a pure function of the TEXT (see this file's head),
// so the key is extracted mechanically. The body's first非-empty line that
// carries real content is taken, markdown/box-drawing decoration is stripped,
// and the result is cut to memoryQuestionKeyRunes on a rune boundary.
//
// WHY THE FIRST SUBSTANTIVE LINE AND NOT A SUMMARY: a captured note and a
// captured response both OPEN with their point — the note because the model
// writes it that way, the response because that is what a reader sees first.
// Paraphrasing would need a model; taking the opening IS the key.
//
// A body whose every line strips to nothing still has to yield a usable key, so
// the fallback is the collapsed body capped to the same length: a bad key beats
// an empty one, which the store would refuse outright (memory_store requires a
// non-empty question) and the capture would then lose the whole row.
//
// A TABLE'S HEADER ROW IS NOT CONTENT (measured, this file's own red test). The
// first arm to walk the lines took `结论 | 说明` from a table whose actual finding
// sat in the row below it: a header names the COLUMNS, never the claim. So a line
// that is a table row whose next line is the separator (`|---|---|`) is skipped —
// the separator is what identifies it as a header rather than a data row.
func memoryQuestionKey(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		k := strings.TrimSpace(stripKeyDecoration(line))
		if k == "" {
			continue
		}
		// A separator row ("|---|---|") or a fence survives stripping as pure
		// punctuation; it is not a key, so skip it and keep looking.
		if !carriesWordRune(k) {
			continue
		}
		// A table HEADER: this line is a row, and the line beneath it is the
		// column separator. The header names the columns, not the finding.
		if isTableRow(line) && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			continue
		}
		return truncateRunes(k, memoryQuestionKeyRunes)
	}
	if k := truncateRunes(collapseSelfNote(body), memoryQuestionKeyRunes); k != "" {
		return k
	}
	// Last resort: nothing word-bearing survived. Return whatever non-space text
	// the body has, capped — still better than the empty key the store refuses.
	// (Only reachable for a body of pure punctuation; the caller's gate has
	// already required real content before this runs.)
	return truncateRunes(strings.Join(strings.Fields(body), " "), memoryQuestionKeyRunes)
}

// isTableRow reports whether a line is a markdown table row: it opens and closes
// with a pipe after trimming.
func isTableRow(line string) bool {
	s := strings.TrimSpace(line)
	return len(s) >= 2 && strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|")
}

// isTableSeparator reports whether a line is markdown's column separator row,
// i.e. a table row whose every cell is only dashes and alignment colons.
func isTableSeparator(line string) bool {
	if !isTableRow(line) {
		return false
	}
	return strings.Trim(strings.TrimSpace(line), "|-: ") == ""
}

// stripKeyDecoration removes the leading decoration a line may carry before its
// content: ATX heading marks, list bullets, blockquote markers, table pipes and
// the box-drawing runes the topology arm keys on. It is a trimmer, never a
// rewriter — anything that is not leading decoration is left exactly as written.
func stripKeyDecoration(line string) string {
	s := strings.TrimSpace(line)
	// Leading punctuation/decoration run, peeled from the left. Both ASCII and
	// the CJK box-drawing range are covered because the corpus is bilingual.
	for {
		trimmed := strings.TrimLeft(s, "#>*-+ \t|`~=_")
		trimmed = strings.TrimLeftFunc(trimmed, func(r rune) bool {
			return (r >= 0x2500 && r <= 0x257F) || r == 0xFE0F || r == 0x200B
		})
		if trimmed == s {
			return s
		}
		// A separator row strips to nothing; keep going until the line is
		// stable so a heading under a fence is still reached.
		if trimmed == "" {
			return ""
		}
		s = trimmed
	}
}

// carriesWordRune reports whether s holds at least one letter or digit — the
// difference between a table separator and an actual line of text.
func carriesWordRune(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// truncateRunes caps s at n runes on a boundary that never splits a CJK
// character.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimSpace(string(runes[:n]))
}

// captureSelfNotes is the response-end wiring: one call per round, beside
// recordSelfNotes, over the notes that round recorded.
//
// PROPERTIES THIS FUNCTION OWES THE EPIC, each one deliberate:
//
//   - IT NEVER SETS a.memoryStored. The task-end memory requirement is the
//     MODEL's to satisfy (the completion checkpoint exists to make it store a
//     distilled lesson); letting the harness's own capture satisfy it would
//     silently retire that checkpoint. The store is called directly, not through
//     the memory tool path, so the flag is never in scope here.
//   - IT IS BEST-EFFORT. Every failure — no session to anchor a pointer on, a
//     store refusal, a database error — is logged with its reason and the round
//     carries on. A note we could not store is a lost record, never a failed run
//     (Rule 15b: the log line is the record; the answer is worth more).
//   - IT DEDUPES LOCALLY, per run. The store has its own two-tier dedup, but the
//     note is capped at exactly the contract length (maxSelfRecordNoteChars), so
//     several rounds can produce byte-identical text; a map here keeps a repeated
//     status phrase from paying a store round-trip. The map is per Agent and
//     per run — no file, no lock, no cross-run state (Rule 35).
func (a *Agent) captureSelfNotes(notes []string) int {
	// A REPLAY CAPTURES NOTHING — asked ONCE for the round, at the TOP.
	//
	// WHY AT THE TOP AND NOT ONLY INSIDE captureSelfNote (measured 2026-10-25).
	// The gate used to sit after classifySelfNote, so every note the classifier
	// declined ("🧠 auto-memory: note not captured (too-short)") was announced —
	// a memory sentence, drawn in the answer band, for a round this run is not
	// running. And the refusal itself was announced once PER NOTE, so a recording
	// with a dozen notes wrote a dozen lines. A replay has no round of its own, so
	// the whole capture pipeline is skipped and RECORDED once (Rule 15: the fact
	// is not lost, it is just not drawn — every time).
	if a.replayCaptureSkipped() {
		return 0
	}
	stored := 0
	for _, note := range notes {
		if a.captureSelfNote(note) != "" {
			stored++
		}
	}
	return stored
}

// captureSelfNote runs ONE note through the gate and stores it. It returns the
// tier that captured it ("") when the note was refused or the store failed —
// the two are told apart by the log line, not by a second return value.
func (a *Agent) captureSelfNote(note string) string {
	verdict := classifySelfNote(note)
	if !verdict.Capture {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: note not captured (%s)", verdict.Reason)
		}
		return ""
	}
	// A REPLAY STORES NOTHING (internal/replaymode, epic defect A). The note is
	// captured from a round the recording already ran; writing it now would put
	// a row in the operator's shared ledger for a run that is not happening.
	// The check sits BEFORE the dedup map and before the origin stamp so a
	// refused note is not remembered as "captured this run" — a replay has no
	// round of its own to remember.
	if a.memoryWriteReplayRefused("store an auto-memory note in the operator's ledger (" + selfNoteLogHead(note) + ")") {
		return ""
	}
	if _, seen := a.selfRecord.noteStored[note]; seen {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: note already captured this run — skipped")
		}
		return ""
	}

	// The pointer comes from the SAME stamp the memory tool uses (item 2/5):
	// the run's session and its current round — one definition, two callers. A
	// run with no session log stamps nothing, and a row with no pointer is not
	// stored at all: the whole point of the capture is that the row leads back
	// to the round that produced it, so a note we cannot anchor is reported
	// rather than written anonymously.
	origin := a.memoryStoreOrigin()
	if origin.None() {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: no session to stamp the pointer with — note not captured (%s)", selfNoteLogHead(note))
		}
		return ""
	}

	topic, question, answer, source := selfNoteStoreArgs(note)
	result, err := tools.MemoryStoreWithOrigin(topic, question, answer, source, nil, origin)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: store refused the note (%v)", err)
		}
		return ""
	}
	if a.selfRecord.noteStored == nil {
		a.selfRecord.noteStored = make(map[string]bool)
	}
	a.selfRecord.noteStored[note] = true
	// NO ASSET SYNC RUNS HERE (epic memory-store-sync-decouple, 2026-10-27): the
	// automatic capture is a local write like any other store, and nothing about
	// it reaches a peer at all — the verb that used to carry it
	// (`gopi memory sync`, deleted 2026-11-05 and restored 2026-11-06 as a MANUAL
	// door) is not fired by a store either: an automatic capture writes THIS
	// node's ledger and stops.
	if a.Logger != nil {
		a.Logger.Printf("🧠 auto-memory: captured a %s note — %s", verdict.Tier, result)
	}
	return verdict.Tier
}

// selfNoteLogHead renders the head of a note for a log line: enough to recognize
// WHICH note is being talked about, short enough that a 500-rune note does not
// dominate the run's log. The cut is on a rune boundary, so a CJK note is never
// split mid-character (the same rule collapseSelfNote follows at the contract cap).
func selfNoteLogHead(note string) string {
	runes := []rune(strings.TrimSpace(note))
	if len(runes) <= selfNoteLogHeadRunes {
		return string(runes)
	}
	return string(runes[:selfNoteLogHeadRunes]) + "…"
}
