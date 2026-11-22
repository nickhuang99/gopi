package tools

// PROSE-DEPENDENCY warning for the submit seam: "the briefing says it waits on
// another row, but the ledger says it waits on nothing."
//
// WHY THIS EXISTS (measured, 2026-11-22, rows 01a20059-* + the defect row
// 01a20076-96f6): a four-step epic was submitted as a chain, and step 4's
// briefing carried the line
//
//	前置依赖：第 1 步（01a20059-6103）completed 即可开工
//
// while its parent_task_id column was EMPTY. `gopi task list --status pending`
// therefore offered it `[next]` — claimable at once — while step 1 was still
// `claimed`, so the next free dispatcher would have started a worker that
// rewrote the same files as the running one, and the collision would only
// surface at merge time.
//
// The important half of that measurement: NOT passing the id was not the whole
// story. Step 4 waited on step 1, not on the chain tail (step 3), and the
// ledger's parent edge can express only "my predecessor is X". A submitter with
// a perfectly clear dependency in its head can still get the EDGE wrong, or
// omit it, and nothing at the seam said so. A worker chain whose order is the
// only dependency signal a model can express (Rule 33b) needs this warning
// most.
//
// THE SHAPE IS THE PRECEDENTS' (Rules 14/15/35), not a new mechanism:
//   - internal/tools/task_hermes_host_warning.go (Rule 40) — a submit-time
//     warning raised where the submitter still has the command in front of them.
//   - internal/tools/task_attachment_claim.go — "prose asserts X, the row
//     carries no X": the same defect class, one level down (a claim with no
//     artifact behind it).
//   - internal/tools/task_briefing_base_submit.go — a payload declaration
//     measured against the ledger at the moment of the write.
//
// IT IS A WARNING, NEVER A REFUSAL. A dependency stated in prose and not in the
// ledger is a DIAGNOSABILITY defect: the row is still dispatchable, and on this
// system it is often legitimately unparented — a consumer that waits on a SET
// expresses that in prose because parent_task_id cannot (Rule 33b), and a
// briefing may quote another row's dependency while being about something else.
// A refusal would let a wording nit block a dispatch, and the submit seam is the
// funnel every internal auto-submit path passes through too.
//
// WHAT THE PREDICATE DELIBERATELY DOES NOT DO:
//   - It never fires when the row DOES declare an edge. `parent_task_id`,
//     `parent_task_ids` and a `task_group:` line are all read through their
//     existing single readers (taskParentIDsOf / taskGroupDependenciesOf), and
//     ANY of them being non-empty silences the warning — including an EMPTY
//     `task_group:` line, which is a declaration that parks the row (Rule 33b).
//   - It does not infer the dependency from prose. It matches asserting PHRASES;
//     "the driver depends on the config value" is ordinary prose about code, so
//     the phrase table holds only shapes that name work waiting on other work,
//     and a NEGATED phrase ("not blocked by", "不依赖于") is not an assertion.
//   - It is not a gate and it writes nothing: the row submits unchanged.

import (
	"fmt"
	"strings"
)

// Prose dependency PHRASES. Each one asserts that this work waits for other
// work. Named constants, never inline literals (Rules 1b/13); the match set
// below is built from them so the two halves cannot drift.
const (
	// English: the shapes that name a blocking edge.
	proseDepPhrasePrerequisite = "prerequisite"
	proseDepPhraseBlockedBy    = "blocked by"
	proseDepPhraseBlockedOn    = "blocked on"
	proseDepPhraseBlockedUntil = "blocked until"
	proseDepPhraseDependsOn    = "depends on"
	proseDepPhraseDependingOn  = "depending on"
	proseDepPhraseDependentOn  = "dependent on"
	proseDepPhraseWaitingOn    = "waiting on"

	// Chinese: the measured shape (the defect's own wording) plus the two
	// spellings a Chinese brief most often uses for the same edge. Word
	// boundaries are satisfied automatically on the CJK side — see
	// taskAttachmentClaimWordByte, which counts only ASCII identifier bytes as
	// word bytes, so a phrase adjacent to a Han character is bounded.
	proseDepPhraseZhQianzhiYilai = "前置依赖"
	proseDepPhraseZhQianzhiRenwu = "前置任务"
	proseDepPhraseZhYilaiRenwu   = "依赖任务"
	proseDepPhraseZhYilaiYu      = "依赖于"
	proseDepPhraseZhZuseYu       = "阻塞于"
)

// taskProseDependencyPhrases is the ordered match set — LONGEST FIRST, so a
// payload carrying both "blocked until" and "blocked on" reports one
// deterministic diagnostic. Derived from the constants above at init: a
// hand-maintained order is drift waiting to happen (Rule 13).
var taskProseDependencyPhrases = taskAttachmentClaimSortPhrases([]string{
	proseDepPhrasePrerequisite,
	proseDepPhraseBlockedBy,
	proseDepPhraseBlockedOn,
	proseDepPhraseBlockedUntil,
	proseDepPhraseDependsOn,
	proseDepPhraseDependingOn,
	proseDepPhraseDependentOn,
	proseDepPhraseWaitingOn,
	proseDepPhraseZhQianzhiYilai,
	proseDepPhraseZhQianzhiRenwu,
	proseDepPhraseZhYilaiRenwu,
	proseDepPhraseZhYilaiYu,
	proseDepPhraseZhZuseYu,
})

// Negation tokens: a phrase PRECEDED by one of these is not an assertion. It is
// the one guard the ASCII word-boundary matcher cannot give us, and both
// languages produce the shape ("this row is NOT blocked by the other one",
// "本行不依赖于第 2 步"). Without it the warning would fire on exactly the
// payloads that took the trouble to rule an edge OUT — the fastest way to teach
// a reader to ignore a warning.
// proseDepNegationWords are the English tokens that cancel the phrase after
// them, compared case-insensitively.
var proseDepNegationWords = []string{"not", "no", "never", "without"}

// proseDepNegationBytes are the single Han characters that cancel the phrase
// after them: 不 (not) / 无 (without) / 非 (not) / 勿 (do not) / 没 (not).
var proseDepNegationBytes = []string{"不", "无", "非", "勿", "没"}

// proseDepNegationObjects are the English OBJECT tokens that cancel the phrase
// BEFORE them — the second negation shape, and the one a prefix scan cannot
// see: "depends on nothing here", "blocked by none of the above". The object is
// what the dependency would name, and a phrase whose object is "nothing" asserts
// no edge at all. Compared case-insensitively against the single token that
// follows the phrase.
var proseDepNegationObjects = []string{"nothing", "none", "nobody"}

// TaskProseDependencyLedgerState is the LEDGER half of the predicate: what the
// row's own columns say about dependencies. Both fields are the output of an
// existing single reader (taskParentIDsOf / taskGroupDependenciesOf), never a
// re-parse, so the warning and the dispatch gate can never disagree about
// whether an edge exists (Rule 14).
type TaskProseDependencyLedgerState struct {
	// ParentIDs is parent_task_id + parent_task_ids, folded.
	ParentIDs []string
	// GroupPrefixes is the payload's `task_group:` declarations. An entry may
	// be the EMPTY prefix: a `task_group:` line naming nothing still declares
	// a dependency (it parks the row, Rule 33b), so it still silences this
	// warning.
	GroupPrefixes []string
}

// Declares reports whether the row carries ANY ledger dependency. One predicate,
// its own name, so the read face and the warning cannot drift.
func (s TaskProseDependencyLedgerState) Declares() bool {
	return len(s.ParentIDs) > 0 || len(s.GroupPrefixes) > 0
}

// TaskSubmitProseDependencyWarningFmt is the submit-time warning TEMPLATE
// appended to TaskSubmit's confirmation when the prose asserts a dependency and
// the row declares none. The %q slot takes the matched phrase; callers use
// TaskSubmitProseDependencyWarningText, never Sprintf directly (Rules 1/13 —
// one spelling, one renderer).
//
// The wording NAMES the cause (the matched phrase, and that all three columns
// are empty) and NAMES the repair (--parent-task-id, or a task_group: line) —
// Rule 15: a warning that does not say what to do about it is noise.
const TaskSubmitProseDependencyWarningFmt = "warning: the prose asserts a dependency (%q) but the row declares NO ledger edge (parent_task_id empty, parent_task_ids empty, no task_group: line) — the dispatcher will offer this row as [next] at once, in parallel with the work the prose says it waits for; pass --parent-task-id <id> (or a task_group: <prefix> line) if that edge is real, or reword the prose if it is not"

// TaskSubmitProseDependencyWarningText renders the write-face warning. One
// helper owns the Sprintf so the template has a single consumer (Rule 13).
func TaskSubmitProseDependencyWarningText(phrase string) string {
	return fmt.Sprintf(TaskSubmitProseDependencyWarningFmt, phrase)
}

// TaskProseDependencyWarning is the ONE predicate: the warning for a row whose
// prose asserts a dependency while `state` declares none, or "" when there is
// nothing to say. PURE — the ledger half is a parameter — so every arm of the
// four-way matrix (prose±  ×  edge±) is testable without a ledger, and the
// submit seam stays one line (TaskSubmitProseDependencyWarning below is that
// one line).
func TaskProseDependencyWarning(summary, payload string, state TaskProseDependencyLedgerState) string {
	if state.Declares() {
		return ""
	}
	phrase, ok := TaskProseDependencyScan(summary, payload)
	if !ok {
		return ""
	}
	return TaskSubmitProseDependencyWarningText(phrase)
}

// TaskSubmitProseDependencyWarning is the submit-seam wrapper: it reads the
// ledger half off the row's own columns and payload through the existing
// readers, then asks the one predicate. `payload` must be the payload column AS
// IT WILL BE WRITTEN (after a spill decision), because that is the text the
// row's future reader sees.
func TaskSubmitProseDependencyWarning(summary, payload, parentTaskID, parentTaskIDs string) string {
	return TaskProseDependencyWarning(summary, payload, TaskProseDependencyLedgerState{
		ParentIDs:     taskParentIDsOf(parentTaskID, parentTaskIDs),
		GroupPrefixes: taskGroupDependenciesOf(payload),
	})
}

// TaskProseDependencyScan reports the first dependency-asserting phrase in the
// task's prose, searching the payload first and the summary second (the payload
// is the briefing the executor runs with; a claim there is the one that
// misleads). Both empty → no claim.
//
// Matching is case-insensitive and word-bounded through the SAME matcher the
// attachment-claim predicate uses (taskAttachmentClaimMatch): "depends on"
// inside a longer identifier does not match, a phrase adjacent to a Han
// character does, and a NEGATED phrase is not a claim. The returned phrase comes
// from the constant list — the caller quotes it, never raw payload bytes.
func TaskProseDependencyScan(summary, payload string) (string, bool) {
	if p, ok := taskProseDependencyScanText(payload); ok {
		return p, true
	}
	return taskProseDependencyScanText(summary)
}

// taskProseDependencyScanText is the single-text half of the scan.
func taskProseDependencyScanText(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	for _, p := range taskProseDependencyPhrases {
		if taskProseDependencyMatch(lower, p) {
			return p, true
		}
	}
	return "", false
}

// taskProseDependencyMatch reports whether phrase p occurs in the (lower-cased)
// haystack on a word boundary and is NOT negated by the token before it. It
// reuses taskAttachmentClaimMatch for the boundary rule and only adds the
// negation scan, so this package holds ONE boundary implementation (Rule 14).
func taskProseDependencyMatch(haystack, phrase string) bool {
	from := 0
	for {
		i := strings.Index(haystack[from:], phrase)
		if i < 0 {
			return false
		}
		start := from + i
		if taskProseDependencyOccurrenceCounts(haystack, start, phrase) {
			return true
		}
		from = start + 1
	}
}

// taskProseDependencyOccurrenceCounts reports whether the occurrence of phrase
// at `start` is a real assertion: word-bounded on both sides AND not negated.
func taskProseDependencyOccurrenceCounts(haystack string, start int, phrase string) bool {
	end := start + len(phrase)
	leftOK := start == 0 || !taskAttachmentClaimWordByte(haystack[start-1])
	rightOK := end == len(haystack) || !taskAttachmentClaimWordByte(haystack[end])
	if !leftOK || !rightOK {
		return false
	}
	if taskProseDependencyNegated(haystack, start) {
		return false
	}
	return !taskProseDependencyObjectIsNothing(haystack, end)
}

// taskProseDependencyObjectIsNothing reports whether the token following the
// phrase is a negation OBJECT ("depends on nothing here"): the edge names no
// work, so nothing is being waited for.
func taskProseDependencyObjectIsNothing(haystack string, end int) bool {
	rest := strings.TrimLeft(haystack[end:], " \t\r\n")
	tokenEnd := 0
	for tokenEnd < len(rest) && taskAttachmentClaimWordByte(rest[tokenEnd]) {
		tokenEnd++
	}
	if tokenEnd == 0 {
		return false
	}
	token := rest[:tokenEnd]
	for _, obj := range proseDepNegationObjects {
		if strings.EqualFold(token, obj) {
			return true
		}
	}
	return false
}

// taskProseDependencyNegated reports whether the text immediately before `start`
// is a negation token. A trailing Han negation character is checked first (it
// abuts the phrase with no separator), then the previous whitespace-delimited
// ASCII word.
func taskProseDependencyNegated(haystack string, start int) bool {
	if start == 0 {
		return false
	}
	before := haystack[:start]
	for _, neg := range proseDepNegationBytes {
		if strings.HasSuffix(before, neg) {
			return true
		}
	}
	trimmed := strings.TrimRight(before, " \t\r\n")
	wordStart := len(trimmed)
	for wordStart > 0 && taskAttachmentClaimWordByte(trimmed[wordStart-1]) {
		wordStart--
	}
	word := trimmed[wordStart:]
	for _, neg := range proseDepNegationWords {
		if strings.EqualFold(word, neg) {
			return true
		}
	}
	return false
}
