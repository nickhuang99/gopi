package agent

// selfnotecaptureresponse.go — the SAME auto-memory shape gate, applied to the
// round's VISIBLE RESPONSE BODY instead of to its [IMPORTANT] note.
//
// WHY THIS FILE EXISTS (measured, 2026-10-10). The note gate
// (selfnotecapture.go) classifies the round's [IMPORTANT] block, and the block
// is capped at maxSelfRecordNoteChars = 500 characters by contract — so a
// response whose value IS a table or a diagram can never carry one through the
// note: the shape does not fit. The measured case is the operator's own billing
// round: a four-column markdown table (`| 产品 | 计费单元 | 单价 | 用量 |`) in
// the body, and a note about it written as `；`-joined prose with ONE pipe. The
// note gate answered `no-structure` and the table died with the round — the
// capture object was the wrong one. This file classifies the OBJECT THAT CARRIES
// THE SHAPE: the response body the console displayed.
//
// IT IS A SECOND CALIBRATION, NOT A SECOND POLICY. Same three arms (topology /
// table / conclusion), same verdict type, same store path, same tier names —
// every difference below is a MEASUREMENT, and each constant says which corpus
// produced it and why the note's value would be wrong here.
//
// THE CORPORA ARE NOT COMPARABLE, WHICH IS THE WHOLE POINT:
//
//	                    note corpus (110 notes)      response corpus (268 bodies)
//	length              capped at 500 runes          p25 601 / p50 1279 / p90 2558 runes
//	table               `|` >= 3 occurrences         ONE markdown separator row
//	topology            ANY box/arrow glyph          >= 2 BOX-drawing glyphs (no arrows)
//	conclusion floor    300 runes                    1200 runes
//
// The note constants are LEFT ALONE (this file references the note's marker
// vocabulary rather than re-typing it, Rule 14, and never reads the note's
// thresholds) — the note path keeps the behaviour its own measurements produced.
//
// THE THREE ARM DIFFERENCES, EACH WITH THE MEASUREMENT THAT FORCED IT:
//
//   - TABLE: `|` >= 3 counts a response that merely QUOTES three pipes. On the
//     response corpus 5 of 268 bodies carry >= 3 pipes and no table at all (the
//     longest is 350 runes: a prose line about a table). The strict form — one
//     markdown SEPARATOR row (`|---|`, the row that makes a table a table) —
//     measured 122 bodies and 0 false positives at or above the length floor.
//   - TOPOLOGY: on RESPONSES the glyph set cannot include the arrows. 155 of 268
//     bodies contain `→` (prose arrows: "A → B means…"), vs 14 containing box
//     drawing; the presence form would have made the arm swallow 58% of the
//     corpus and starve the other two arms. The arm is BOX DRAWING ONLY, and
//     requires >= 2 glyphs (measured: every body carrying a box glyph carries at
//     least 3, and NO body carries exactly 1 — so 2 is a floor with margin, not
//     a fitted value).
//   - CONCLUSION: the note floor (300) sits at the response corpus' p23, so it
//     would capture a quarter of all rounds; 1200 sits at p43 of the bodies and
//     is where a round has written its finding OUT (the note's own doc calls
//     150/300 "the floor" and "twice the floor"; 600/1200 keeps the same 2x
//     relation on a corpus an order of magnitude longer per item).
//
// WHAT THIS FILE DOES NOT DO: it does not touch the note path, does not add a
// tool (Rule 3), does not spawn anything and writes no file of its own (Rule 35;
// it rides the same response-end call and the same store). The gate stays a PURE
// FUNCTION of the text — no API call, no clock, no state — so it is testable
// offline and cannot be prompt-injected (the epic's hard constraint).

import (
	"strings"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// respCaptureMinRunes is the floor EVERY response body must clear before any
	// tier is considered.
	//
	// MEASURED on the response corpus (123 local session JSONL files, 268 visible
	// assistant bodies, 2026-10-10): min 34 / p10 106 / p25 601 / p50 1279 /
	// p75 1917 / p90 2558 / max 4384 runes. 600 sits at that corpus' p26 — the
	// same QUANTILE POSITION the note's 150 (p25 of 110 notes) was chosen at, not
	// the same number. 70 of 268 bodies (26%) fall below it; they are the short
	// answers ("收到", a one-line status, a two-line acknowledgement) that carry
	// no finding to remember.
	respCaptureMinRunes = 600

	// respCaptureConclusionRunes is the LENGTH half of the written-out-conclusion
	// arm, at TWICE the floor (the relation the note gate uses 150/300 for).
	// MEASURED: it sits at p43 of the response corpus; 92 of 268 bodies clear it.
	// The 46 that ALSO carry a reasoning marker are captured by this arm; the
	// other 46 are captured by the table or topology arm above it (the arms are
	// exclusive and ordered, so no body is stored twice).
	respCaptureConclusionRunes = 1200

	// respCaptureTopologyMin is how many BOX-DRAWING glyphs a body needs to be
	// read as a diagram. It is a COUNT, not a presence test, and the value is
	// measured: of the 268 bodies, 14 carry box glyphs — every one of them 3 or
	// more — and NO body carries exactly 1 or exactly 2. So 2 is a floor with a
	// margin on the corpus, not a value fitted to it.
	respCaptureTopologyMin = 2

	// respCaptureTableRowMin is how many markdown SEPARATOR rows (`|---|`) make a
	// body tabular. One, and it is the strict form: on responses the note's
	// `|` >= 3 test is a false-positive machine (5 bodies carry 3+ pipes with no
	// table; the longest such body is 350 runes, which the length floor already
	// refuses). A separator row is the row that makes a table a table, so this
	// arm is the shape the operator's own billing table has.
	respCaptureTableRowMin = 1

	// respCaptureNoteOnly is the refusal when a round's ONLY text was its own
	// [IMPORTANT] block: stripping the block leaves nothing to classify, and the
	// note path already owns that text. Refusing it here is what keeps ONE
	// capture per body — the body is not a second copy of the note.
	respCaptureNoteOnly = "note-only"

	// respStoredKeyPrefix keys the run-local dedup set with the response's own
	// spelling. It REUSES selfRecordState.noteStored (one map, one owner, Rule
	// 14) rather than adding a field: a note and a body cannot collide because
	// the key names which object it is, and no struct in the tree moves a line
	// for this feature — a churn argument only: that gate (the devdoc line-anchor
	// gate) went with internal/docclass in 5a35f61; live: nothing. AND NOTHING REPLACES IT: the config-anchor census that was the executable anchor check (`internal/standards/testdata/config-anchor-census.go --check`, driven by the TestMergeConfigAnchorGate* arms in internal/tools) was deleted with the whole documentation-citation family in 78bdf05a (2026-11-05, operator ruling), so no executable anchor check runs over CONFIG.md's rows either.
	respStoredKeyPrefix = "response\x00"
)

// respCaptureTierTable / respCaptureTierTopology / respCaptureTierConclusion are
// the response arms' tier values. They are NOT new vocabulary: a captured row's
// tier has to mean the same thing whichever object produced it, so they alias
// the note gate's constants (Rule 14 — one definition, referenced).
const (
	respCaptureTierTopology   = selfNoteTierTopology
	respCaptureTierTable      = selfNoteTierTable
	respCaptureTierConclusion = selfNoteTierConclusion

	// respCaptureMemorySource is the `sources` class of a row captured from a
	// response body. It is deliberately distinct from the note gate's
	// "self-record(auto)": a reader asking "was this the model's own note, or the
	// answer the console showed?" gets an answer from the row itself. It is not a
	// second copy of the session/round pointer — that lives in the origin columns.
	respCaptureMemorySource = "response(auto)"

	// respCaptureMemoryTopic is the distilled_qa topic, beside the note gate's
	// "self-record" handle: the same searchable class, named for the object.
	respCaptureMemoryTopic = "response"
)

// respCaptureTopologyGlyphs is the box-drawing half of this gate's glyph set,
// and respCaptureArrowGlyphs is the other half. Their UNION is
// selfNoteTopologyGlyphs, and that is asserted rather than assumed
// (TestResponseTopologyGlyphsPartitionTheNoteSet): the two sets are written out
// once each here, and the test refuses a change that breaks the partition, so
// neither side can drift into a stale copy of the note vocabulary (Rule 14 —
// one definition, referenced; a set that PARTITIONS a shared constant is
// referenced by the test that proves the partition).
//
// WHY THE ARROWS ARE NOT AN ARM HERE (the measured reason): on the note corpus
// the arrow set is a diagram signal; on responses `→` is ordinary prose
// punctuation ("client → server", "A 修好了 → B 还可以用") and appears in 155 of
// the 268 measured bodies. Keeping it in the arm would make topology capture 58%
// of everything, and — since the arms are ordered — it would starve the table arm
// of the very bodies it was written for. The note gate keeps its own set
// untouched.
const (
	respCaptureTopologyGlyphs = "├└┌┐┘│─┴┬┼┃━┏┓┗┛"
	respCaptureArrowGlyphs    = "→←↑↓↔⇄⇒⇐"
)

// classifyResponseText is the PURE predicate for one visible response body:
// text in, verdict out. Same shape and same arm ORDER as classifySelfNote
// (strongest form first — a diagram outranks a table outranks a conclusion),
// over an object with different measurements.
//
// It takes the body WITH any [IMPORTANT] block still in it and strips the block
// itself, so a caller cannot forget to: the block is the harness' own protocol
// text, and the note gate is already classifying it from the note side. What
// remains is the text the operator actually read.
func classifyResponseText(text string) selfNoteVerdict {
	body := visibleResponseBody(text)
	switch {
	case body == "":
		// Nothing but the block: either the round recorded only a note (the note
		// path owns it), or it said nothing at all. Both are refusals, and the
		// reason names the first so a log line is answerable.
		return selfNoteVerdict{Reason: respCaptureNoteOnly}
	case utf8.RuneCountInString(body) < respCaptureMinRunes:
		return selfNoteVerdict{Reason: selfNoteRejectTooShort}
	case respCaptureGlyphCount(body) >= respCaptureTopologyMin:
		return selfNoteVerdict{Capture: true, Tier: respCaptureTierTopology}
	case respCaptureTableRows(body) >= respCaptureTableRowMin:
		return selfNoteVerdict{Capture: true, Tier: respCaptureTierTable}
	case utf8.RuneCountInString(body) >= respCaptureConclusionRunes && selfNoteCarriesSummary(body):
		return selfNoteVerdict{Capture: true, Tier: respCaptureTierConclusion}
	}
	return selfNoteVerdict{Reason: selfNoteRejectNoStructure}
}

// visibleResponseBody removes every [IMPORTANT]…[/IMPORTANT] block from a
// response and returns what a reader saw, trimmed.
//
// THE BLOCK IS PROTOCOL, NOT CONTENT (the ruling this gate obeys): the note is
// the model's own summary FOR the harness, and a body that is nothing but a note
// must not be captured a second time from the response side. Stripping it here —
// at the ONE place the response is classified — is what makes the two gates
// disjoint rather than nested.
//
// The block delimiters come from selfrecord.go's constants (Rule 14), and the
// scan is the same open/close pairing extractSelfNotes uses: an UNCLOSED marker
// is not a block, so a round talking ABOUT the contract keeps its text.
func visibleResponseBody(text string) string {
	if text == "" {
		return ""
	}
	if !strings.Contains(text, selfNoteMarkerOpen) {
		return strings.TrimSpace(text)
	}
	var kept strings.Builder
	rest := text
	for {
		open := strings.Index(rest, selfNoteMarkerOpen)
		if open < 0 {
			kept.WriteString(rest)
			break
		}
		closeAt := strings.Index(rest[open+len(selfNoteMarkerOpen):], selfNoteMarkerClose)
		if closeAt < 0 {
			// An unclosed delimiter is prose about the contract, not a block.
			kept.WriteString(rest)
			break
		}
		kept.WriteString(rest[:open])
		kept.WriteString("\n")
		rest = rest[open+len(selfNoteMarkerOpen)+closeAt+len(selfNoteMarkerClose):]
	}
	return strings.TrimSpace(kept.String())
}

// respCaptureGlyphCount counts the box-drawing characters in a body.
func respCaptureGlyphCount(body string) int {
	n := 0
	for _, g := range respCaptureTopologyGlyphs {
		n += strings.Count(body, string(g))
	}
	return n
}

// respCaptureTableRows counts the body's markdown SEPARATOR rows: a line that
// opens and closes with `|` and contains nothing but pipes, dashes, colons and
// spaces (`|---|---|`, `|:--|--:|`). This is the row that makes a markdown table
// a table; counting it is what separates a table from prose that quotes pipes.
func respCaptureTableRows(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if len(s) < 2 || !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
			continue
		}
		if strings.Trim(s, "|-: ") == "" && strings.Contains(s, "-") {
			n++
		}
	}
	return n
}

// captureResponseText runs ONE response body through the response gate and
// stores it. It returns the tier that captured it ("" when the body was refused
// or the store failed) — the two are told apart by the log line, not by a second
// return value, exactly as captureSelfNote does it.
//
// THE PROPERTIES IT INHERITS FROM THE NOTE PATH, each deliberate (see
// captureSelfNote): it never sets a.memoryStored (the task-end memory
// requirement stays the MODEL's), it is best-effort (a failure is logged and the
// round carries on), it refuses to write a row it cannot anchor with a session/
// round pointer, and it dedupes per run through the one shared map.
func (a *Agent) captureResponseText(text string) string {
	// A REPLAY CAPTURES NOTHING — asked here, FIRST (see captureSelfNotes for the
	// measurement): the classifier's own "🧠 auto-memory: response not captured
	// (too-short)" is a memory sentence, and on a replay it was drawn in the
	// answer band on top of the refusal that follows it.
	if a.replayCaptureSkipped() {
		return ""
	}
	verdict := classifyResponseText(text)
	body := visibleResponseBody(text)
	if !verdict.Capture {
		if a.Logger != nil && body != "" {
			a.Logger.Printf("🧠 auto-memory: response not captured (%s)", verdict.Reason)
		}
		return ""
	}
	key := respStoredKeyPrefix + body
	if _, seen := a.selfRecord.noteStored[key]; seen {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: response already captured this run — skipped")
		}
		return ""
	}
	// A REPLAY STORES NOTHING (internal/replaymode, epic defect A) — the same
	// gate as the note path, at the same place (before the dedup map and the
	// origin stamp), so the two capture paths cannot disagree about it.
	if a.memoryWriteReplayRefused("store a captured response in the operator's ledger (" + selfNoteLogHead(body) + ")") {
		return ""
	}
	origin := a.memoryStoreOrigin()
	if origin.None() {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: no session to stamp the pointer with — response not captured (%s)", selfNoteLogHead(body))
		}
		return ""
	}
	result, err := tools.MemoryStoreWithOrigin(respCaptureMemoryTopic, memoryQuestionKey(body), body, respCaptureMemorySource, nil, origin)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Printf("🧠 auto-memory: store refused the response (%v)", err)
		}
		return ""
	}
	if a.selfRecord.noteStored == nil {
		a.selfRecord.noteStored = make(map[string]bool)
	}
	a.selfRecord.noteStored[key] = true
	// NO ASSET SYNC RUNS HERE (epic memory-store-sync-decouple, 2026-10-27) — see
	// selfnotecapture.go: the local write is the whole success criterion.
	if a.Logger != nil {
		a.Logger.Printf("🧠 auto-memory: captured a %s response (%d runes) — %s",
			verdict.Tier, utf8.RuneCountInString(body), result)
	}
	return verdict.Tier
}

// ClassifyResponseText is the read-only, exported face of the response gate: the
// ONE classifier, reachable from a CLI without a second implementation (Rule 14
// — the survey verb and the response-end call must never be able to disagree
// about what counts as a shape). It returns the tier and the refusal reason; at
// most one of them is non-empty.
func ClassifyResponseText(text string) (tier, reason string) {
	v := classifyResponseText(text)
	return v.Tier, v.Reason
}
