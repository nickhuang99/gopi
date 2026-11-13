// Session round numbering — the ONE definition of "which round is this".
//
// WHY IT LIVES HERE, and not in the package that happens to need it first
// (PROJECT.md Rule 14): the same number is produced by internal/agent (the loop's
// own round stamp), derived by internal/gopilog (from the session JSONL's
// `request` events, for `gopi session log <id> <round>` and its `--rounds`
// table) and STORED by internal/tools (a memory row's provenance pointer,
// distilled_qa.round_index). internal/agent and internal/gopilog import each
// other (gopilog/formatter.go -> agent), so no consumer of that pair can hold a
// definition the others reach; internal/tools may import neither. This package
// is the leaf with zero project imports those three already share for exactly
// this reason (its header's own argument for the event names), so the round
// numbering has one spelling instead of one per consumer.
//
// THE NUMBERING (measured, not chosen): a session's rounds are its API calls.
// The session JSONL numbers them from ZERO — its `request` event carries
// iteration=0 for the session's first send — while every user-facing surface
// (`gopi session log <id> <round>`, the `--rounds` table's ROUND column) counts
// them from ONE. RoundIndexBase is that offset, named once.
//
// A value is not a round: RoundIndexUnknown (0) is what a column or a wire
// field carries when NO round was recorded, and it is deliberately below the
// base so a reader can never mistake "no round" for "round 0" or for a real
// round somebody forgot to stamp (Rule 15: absence is stated, never a fake
// round 1).
package sessionevent

// RoundIndexBase is the first round number a session's own numbering uses:
// round 1 IS the session's first API call (JSONL `request` iteration 0). Every
// conversion between the JSONL's 0-based iteration and a round number is this
// base, and it is the only place the offset is spelled.
const RoundIndexBase = 1

// RoundIndexUnknown is the round value carried when no round was recorded —
// never a round that exists. It is 0, i.e. below RoundIndexBase, so
// RoundIndexRecorded refuses it and a reader cannot mistake absence for a
// round.
const RoundIndexUnknown = 0

// RoundNumberOf converts a session's 0-based loop iteration (the value the
// session JSONL's `request` event carries) into the session's own 1-based
// round number — the numbering `gopi session log <id> <round>` takes and the
// `--rounds` table prints. Negative input is returned as RoundIndexUnknown:
// a caller with no round to report says so instead of inventing round 0.
func RoundNumberOf(iteration int) int {
	if iteration < 0 {
		return RoundIndexUnknown
	}
	return iteration + RoundIndexBase
}

// RoundIndexRecorded reports whether n is a round that exists. Absence
// (RoundIndexUnknown) is a value, not an error: a memory row distilled by a
// caller that knew no round (a CLI store, an import) carries it.
func RoundIndexRecorded(n int) bool {
	return n >= RoundIndexBase
}

// RoundIndexOf is the inverse of RoundNumberOf for a RECORDED round: the
// 0-based iteration a round number came from. It returns ok=false for a value
// that is not a recorded round (RoundIndexUnknown and below), so a reader that
// needs the index is told the value is absent instead of silently getting -1.
func RoundIndexOf(n int) (int, bool) {
	if !RoundIndexRecorded(n) {
		return 0, false
	}
	return n - RoundIndexBase, true
}
