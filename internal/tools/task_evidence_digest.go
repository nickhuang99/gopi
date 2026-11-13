// task_evidence_digest.go — the `--status verified` evidence contract, moved out
// of task_verify_evidence.go when the `gopi task verify` VERB was retired
// (2026-10-30, task 01a18799, Rule 43: a symbol with a live reader outside the
// retired mechanism is moved BEFORE the mechanism is deleted, in the same
// commit).
//
// WHAT MOVED AND WHY IT SURVIVES ITS OLD HOME. `TaskVerifyEvidenceArgs` /
// `TaskVerifyEvidenceError` sat beside a verb that no longer exists, and the
// filename said "verify" — but the reader was never that verb: it is
// `gopi task update <id> --status verified`, the OPERATOR's own write, which
// refuses to record a verdict with an empty evidence field. Deleting the
// mechanism would have deleted the operator's refusal with it. So the predicate
// and its wording moved here, and only the verb's implementation went.
package tools

import "strings"

// taskVerifyEvidencePrefix begins an evidence digest ("evidence: …").
const taskVerifyEvidencePrefix = "evidence:"

// taskVerifyReasonPrefix is the pass-through prefix for a verdict that
// carries a stated reason instead of a measured digest.
const taskVerifyReasonPrefix = "reason:"

// TaskVerifyEvidenceArgs reports whether a `--status verified` write carries
// the evidence digest that verdict demands, returning the digest (or "" when
// there is none).
//
// WHAT THIS IS, AND WHAT IT IS NOT. It is the OPERATOR's verb refusing to
// record a verdict with an empty evidence field — the human who types
// `gopi task update <id> --status verified` has to say what was observed.
// It is NOT the removed verified-closure gate: nothing here creates a row,
// schedules a verifier or judges the program's own output. Verification
// belongs to a TEST PROGRAM (a test run, a measured command); a program may
// not manufacture an authority over itself, which is the operator's ruling of
// 2026-10-25 that deleted that gate.
//
// The two spellings it accepts are the ones a reader can ACT on: a measured
// digest ("evidence: <command> + its output tail") or a stated reason
// ("reason: …"). Anything else is a bare assertion and is refused.
func TaskVerifyEvidenceArgs(result string) string {
	v := strings.TrimSpace(result)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, taskVerifyReasonPrefix) {
		return v
	}
	if strings.HasPrefix(v, taskVerifyEvidencePrefix) {
		return v
	}
	return ""
}

// TaskVerifyEvidenceError is the message the CLI prints when a verified
// write arrives without an evidence digest. The wording lives here, beside
// the predicate it explains, so the two cannot drift.
func TaskVerifyEvidenceError() string {
	return "task update: --status " + taskStatusVerified + " requires an evidence digest (--result \"" +
		taskVerifyEvidencePrefix + "<the test command> + its output tail\", or a \"reason: …\" pass-through)"
}
