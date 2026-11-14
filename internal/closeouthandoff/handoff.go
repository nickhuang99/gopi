// Package closeouthandoff carries the ONE decision `gopi session resume` and the
// agent's end hook must agree on WITHIN one process: WHO reports the client
// finish-cleanup chain for a row. One requested close-out used to grow three
// report writers — the end hook, the resume tail, and (for
// `gopi worker --resume`) the resuming parent — so one refusal was printed
// three times and the operator's count of it became unusable as evidence
// (epic worker-resume-report-once, measured 2026-09-26 on peer1:
// /tmp/strange_python.txt, 66 lines, the same `skipped:` line three times).
//
// THE RULE THIS PACKAGE ENCODES: a suppressed writer must have a NAMED
// COVERING writer, and it says so. Silence is only honest when somebody else is
// known to make the report — otherwise it is a dropped close-out, the defect
// devdoc/client-closeout-delivery.md exists for. So each suppression prints a
// note that names its coverer and carries NO part of the report's own spelling,
// and the count of that spelling is exactly one per run in every shape:
//
//	`gopi session resume <task-id>` (standalone)
//	    the resume tail reports; the end hook defers to it.
//	`gopi worker --resume` (a CHILD resumed for a parent one level up)
//	    the PARENT reports; the tail AND the end hook defer to it.
//	everything else (bare prompt, chat, `task run` child, panic paths)
//	    nothing is declared; the end hook reports exactly as it always has.
//
// The child→parent half cannot use this latch (two processes share no memory),
// so it is declared through the ENVIRONMENT by the parent before it spawns the
// child — the same cross-process convention `GOPI_TASK_WRAPPER_ID` uses, and
// deterministic in the same way: the declaration exists before the child's
// first instruction, so no writer has to guess what another will do later.
//
// WHY A PACKAGE AND NOT A VARIABLE IN EITHER CALLER. The two ends live in
// different packages and `internal/agent` is imported BY `cmd/agent`: neither
// can import the other, so the shared fact needs a leaf that imports nothing
// (PROJECT.md Rule 14 — shared values live in a small exported-constant/leaf
// package, so no consumer creates a cycle). One process, one latch: this is the
// only instance, and its zero value means "nothing was declared", so a run that
// never comes through a resume tail behaves exactly as it did before this
// package existed.
package closeouthandoff

import (
	"os"
	"strconv"
	"strings"
)

// ParentReportsEnv is the declaration a resuming PARENT exports for the child
// it spawns: the child stands for a `gopi worker --resume` (or any future
// caller) that will itself report the child's close-out chain, so the child's
// own writers must defer AND say to whom.
//
// It is exported as a NAME (Rule 1: one spelling, one grep) and read only
// through ParentReports below, so the knob has exactly one reader.
const ParentReportsEnv = "GOPI_RESUME_CLOSEOUT_PARENT_REPORTS"

// parentReportsTrue is the truthy vocabulary this declaration accepts. It is
// deliberately ONE spelling ("1"): the value is set by gopi itself, never typed
// by an operator, so a permissive parser would only widen what a stray
// environment can silently claim.
const parentReportsTrue = "1"

// ParentReports reports whether this process was spawned by a parent that has
// declared it will make the close-out report itself.
//
// The reader is here, beside the name, for the same reason the name is
// exported: a knob whose only reader lives three packages away is a knob whose
// semantics drift (PROJECT.md Rule 2 — one place reads the value).
func ParentReports() bool {
	return strings.TrimSpace(os.Getenv(ParentReportsEnv)) == parentReportsTrue
}

// ParentReportsValue is the value ParentReports recognizes, for the exporting
// side: a caller that spells the value inline would be a second writer for one
// wire format.
func ParentReportsValue() string {
	return strconv.Itoa(1)
}

// State is a one-shot within-process latch, keyed by the ROW the declaration
// names, with the three verbs the hand-off needs. The zero value is
// undeclared; a State is not safe for concurrent use and does not need to be
// (one run's declaration, one run's end hook, in order).
type State struct {
	declared bool
	row      string
	// detail is what the declaring writer said its report carried, quoted by a
	// consumer that must render a self-contained note about it rather than
	// re-render the report (a second writer for one measurement is the defect
	// this package removes — PROJECT.md Rule 2).
	detail string
}

// Declare records that the declaring writer WILL report the close-out chain for
// row (detail is what its report carries, when it already knows). Idempotent: a
// second declaration replaces the first, because the newest is the one that
// stands.
func (s *State) Declare(row, detail string) {
	s.declared = true
	s.row = row
	s.detail = detail
}

// Take consumes the declaration IF it is about row, reporting whether it was
// there and what it carried.
//
// The row is checked INSIDE the take rather than by the caller beforehand, so a
// consumer asking about a different row neither consumes nor is misled by a
// declaration made about somebody else's — and "was it declared" and "was it
// consumed" can never be answered by two different reads (Rule 13). A consumer
// that declines the work must still Take: a chain it did not run would
// otherwise leave the far side believing the report was made.
func (s *State) Take(row string) (bool, string) {
	if !s.declared || s.row != row {
		return false, ""
	}
	detail := s.detail
	s.declared, s.row, s.detail = false, "", ""
	return true, detail
}

// Withdraw clears a declaration whose owner never reached its report (a return
// before the print, a panic, a failed run). Idempotent, and unconditional: it
// is the declaring writer withdrawing its OWN claim, never a consumer's read.
func (s *State) Withdraw() { s.declared, s.row, s.detail = false, "", "" }

// Declared reports whether a declaration is held, and for which row. It is an
// OBSERVATION, never a decision input — a consumer deciding whether to report
// calls Take (which consumes) — and it exists for tests and diagnostics that
// must see the latch without changing it.
func (s *State) Declared() (bool, string) { return s.declared, s.row }

// resumeTail is THE latch — one instance per PROCESS, and the reason this
// package exists rather than a variable in either caller (see the package
// comment: cmd/agent and internal/agent cannot both read one variable unless it
// lives in a leaf they both import). Its zero value is undeclared.
var resumeTail State

// Declare is the arming surface's verb (cmd/agent's resume tail): "I will
// report the close-out chain for this row."
func Declare(row, detail string) { resumeTail.Declare(row, detail) }

// Take consumes the declaration if it is about row — consuming is the point: a
// consumer that declines the work must still take it, or a chain it did not run
// would leave the far side believing the report was made.
func Take(row string) (bool, string) { return resumeTail.Take(row) }

// Withdraw retracts the declaration when its owner never reached its report.
func Withdraw() { resumeTail.Withdraw() }

// Declared observes the latch without changing it (tests and diagnostics only —
// a consumer decides with Take).
func Declared() (bool, string) { return resumeTail.Declared() }
