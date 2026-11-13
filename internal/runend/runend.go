// Package runend is the SINGLE VOCABULARY for why an agent run's engine
// stopped the loop, plus the ONE rule that maps an engine-forced end to the
// terminal ledger status the run may claim.
//
// Why it exists (Rule 14 — a value shared across packages gets one home):
// the loop knows why it stopped (the wallclock cap fired, the stuck detector
// tripped, the iteration budget ran out) and the close-out hook is what
// writes the ledger row, but those two facts used to have no shared name. The
// close-out decision was one bit — `exitCode != 0` — and an engine-forced stop
// exits ZERO, so a run cut short after hours of half-finished work was
// recorded `completed`. A false success is worse than a failure: a failure
// gets handled, a false completion is built upon (sync, verify and the merger
// all read that status as "the work is done").
//
// The reason is NOT a new status (the ledger's status set is shared by sync,
// verify and the claim gate — see internal/tools taskAllowedTransitions): it
// is carried in the existing vocabulary — the row's task_events detail and
// its reason column — so nothing downstream has to learn a new word.
package runend

import "strings"

// Reason is why the run's loop stopped.
//
// The ZERO VALUE is the ordinary case on purpose: a run the MODEL finished
// carries no reason, so every caller that has nothing to say keeps saying
// nothing, and the truncation vocabulary is opt-in at the exit sites that
// know the engine (not the model) ended the run.
type Reason string

const (
	// Finished is the model's own completion (the bug022 consecutive end_turn
	// exit, including the R3 all-completed fast path): the run ended because
	// the model produced its final answer. The zero value — a finished run
	// carries no marker, and its status stays the plain exit-code mapping.
	Finished Reason = ""

	// WallclockCap is the session wallclock ceiling
	// (GOPI_SESSION_MAX_WALLCLOCK, loop.go sessionWallclockCap): the loop
	// stopped issuing rounds at the deadline and returned the last substantive
	// text with a nil error. The cap is an environment boundary, not a model
	// decision — the work in flight was NOT finished.
	WallclockCap Reason = "wallclock-cap"

	// StuckLoop is the no-tool detector (maxStuckRounds consecutive rounds
	// with no tool_use and the same text prefix — incident t-1788173679).
	StuckLoop Reason = "stuck-loop"

	// StuckToolLoop is the toolStreak detector (identical tool_use / frozen
	// poll results — incident Bond 718319).
	StuckToolLoop Reason = "stuck-tool-loop"

	// StuckObservation is the observation bound (§6.3 of
	// devdoc/cli-as-eyes-and-hands-feasibility.md): the run looked at live
	// state round after round and never acted — the one loop shape both
	// sibling detectors are structurally blind to (see observationState).
	StuckObservation Reason = "stuck-observation"

	// MaxIterations is the iteration budget (a.MaxIterations, 0 = unlimited
	// in production): the loop ran out of rounds with the task still running.
	MaxIterations Reason = "max-iterations"

	// RunContextCancelled is the round-boundary fallback for a run whose
	// CONTEXT was cancelled from outside the loop (ctx.Err() ==
	// context.Canceled): the caller/owner — a parent run, a supervising
	// surface, a harness stop — ended the run while it still had work to do.
	// The loop cancels nothing itself, so this reason is always an EXTERNAL
	// stop, and that is what makes it distinct from MaxIterations: nobody ran
	// out of anything.
	//
	// WHY IT EXISTS (measured, 2026-10-16): that exit site used to report
	// every cancellation as a timeout "agent timed out after %d iterations" —
	// an attribution to a budget that does not exist in production
	// (a.MaxIterations == 0 means unlimited), with the loop counter as the
	// number, so each occurrence printed a different figure (43, 48, 49, 52 …)
	// and 81 merge hand-over failures shared that one misleading sentence. A
	// reader could neither tell an external cancel from an expired limit nor
	// find a budget that was never reached.
	//
	// ENGINE-FORCED (Truncated): the model never finished, so the row must not
	// close `completed` — the same rule the cap and the stuck nets obey.
	RunContextCancelled Reason = "run-context-cancelled"

	// DeadlineExceeded is its sibling: the run's OWN deadline expired
	// (ctx.Err() == context.DeadlineExceeded — GOPI_TIMEOUT_SEC and/or the
	// session wallclock cap, folded into the same run context). It stays a
	// SEPARATE value because the two demand different responses: an expired
	// limit is a budget/configuration fact (raise it, or accept the
	// truncation), while a cancellation is somebody's decision — merging them
	// would put the caller back where the single misleading sentence was.
	//
	// ENGINE-FORCED (Truncated) for the same reason as its sibling.
	DeadlineExceeded Reason = "deadline-exceeded"
)

// TruncationMarker is the word an operator reads on a truncated row, in both
// the task_events digest and `gopi task show`. ONE spelling, referenced by the
// writer (the agent's end hook) and the reader (the show renderer + the
// wrapper writeback) — a mirrored literal would let a row carry a marker that
// the renderer no longer recognises (Rules 13/14).
const TruncationMarker = "TRUNCATED run"

// TruncationExplain is the operator-facing one-liner rendered beside the marker
// (`gopi task show`). It states the CONSEQUENCE, not just the fact: the reader
// must not treat the row as a finished result.
const TruncationExplain = "the engine stopped the loop before the task was reported finished — this row is NOT a completed result"

// EventName is the task_events event name that carries a run's truncation in
// the append-only trail. It is NOT a status (the ledger's status set is shared
// by sync/verify/the claim gate and stays closed) — it is a NOTE about the run
// that any later reader can consult, which is what lets the wrapper's
// writeback refuse `completed` for a run whose own record it must consult.
const EventName = "run-end"

// engineReasons is the set of engine-forced ends: the loop was still going
// when the framework stopped it.
var engineReasons = map[Reason]bool{
	WallclockCap:     true,
	StuckLoop:        true,
	StuckToolLoop:    true,
	StuckObservation: true,
	MaxIterations:    true,
	// Both context-ended exits belong here: the context ending is what stopped
	// the loop, and neither the caller's cancel nor an expired limit is the
	// model reporting its work finished.
	RunContextCancelled: true,
	DeadlineExceeded:    true,
}

// Truncated reports whether r is an ENGINE-FORCED end — the run was stopped
// while it still had work to do. A finished run is never truncated.
func (r Reason) Truncated() bool { return engineReasons[r] }

// Token is the reason's wire/ledger spelling ("" for Finished — a finished run
// records no reason token).
func (r Reason) Token() string { return string(r) }

// Detail renders the task_events digest marker for r: the operator-facing
// "why this row must not be read as a finished run". Empty for a finished run
// (a normal run's digest is unchanged by this package).
func (r Reason) Detail() string {
	if !r.Truncated() {
		return ""
	}
	return TruncationMarker + " (" + r.Token() + ")"
}

// ReasonText is the row's `reason` column value for a truncated run: the
// ledger's own reason slot (rendered by `gopi task show` as `reason:`), which
// is where a status transition's WHY belongs. It opens with the SAME marker
// the digest carries, so the row's two slots agree and one reader
// (TokenIn) answers for both. Empty for a finished run so the caller falls
// back to the status default (internal/tools taskDefaultReason).
func (r Reason) ReasonText() string {
	if !r.Truncated() {
		return ""
	}
	return r.Detail() + " — the engine stopped the loop, not the model; the task was NOT reported finished"
}

// Or returns the truncation reason to record when two writers each know part
// of the answer: the first of the two that is a truncation, or r (so a
// finished run unioned with a finished run stays finished).
//
// Used by the run close-out, which is a FALLBACK behind another writer: the
// reason passed in by the run's own END hook and the reason the row already
// records can each be present while the other is not, and the union is the
// only answer that never loses a truncation.
func (r Reason) Or(other Reason) Reason {
	if r.Truncated() {
		return r
	}
	if other.Truncated() {
		return other
	}
	return r
}

// RecoveryHint is appended to a truncated run's digest: the row is the
// canonical resume candidate (the run directory holds the checkpoint), and
// saying so is the difference between an operator who can recover the work
// and one who re-runs it from scratch. It never promises a retry happened —
// nothing auto-resumes (the operator decides).
func RecoveryHint(taskID string) string {
	return " | engine-stopped before completion — recoverable: `gopi session resume " + taskID + "` continues this run's context"
}

// TokenIn extracts the truncation token from a task_events detail written by
// Detail/TruncationMarker ("TRUNCATED run (wallclock-cap) — elapsed=…" →
// "wallclock-cap"). "" when the detail carries no truncation marker.
//
// The READER side of the shared spelling: `gopi task show` and the wrapper's
// writeback both ask this, so a row closed by any other writer still reports
// the truncation its own trail records.
func TokenIn(detail string) string {
	i := strings.Index(detail, TruncationMarker)
	if i < 0 {
		return ""
	}
	rest := detail[i+len(TruncationMarker):]
	open := strings.Index(rest, "(")
	if open < 0 {
		return ""
	}
	rest = rest[open+1:]
	close_ := strings.Index(rest, ")")
	if close_ < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:close_])
}
