// Package testcompile is the ONE definition of the command that compiles a Go
// module's TEST TREES without running a single test — and of the budget that
// command gets.
//
// ── WHY A LEAFLESS SECOND SPELLING WAS THE DEFECT, NOT THE FIX ──────────────
//
// Two independent guards need this command, and they live in packages that
// cannot import one another:
//
//	internal/tools        the merge-time build gate and union gate
//	                      (mergeBuildGateLegs, mergeUnionGateResolveSpec)
//	internal/devworkspace the clone channel's delivery loop (AttemptDelivery)
//
// `internal/tools` imports `internal/devworkspace` in 46 files (measured:
// `grep -rln 'the operator/gopi/internal/devworkspace' internal/tools/*.go | wc -l`),
// while `internal/devworkspace` imports `internal/tools` in NONE (measured:
// `grep -rn 'the operator/gopi/internal/tools' internal/devworkspace/*.go` → no hits).
// So the direction of that edge is fixed: devworkspace can never read a
// constant out of tools (Rule 14 — the value shared across packages lives in a
// small exported-constant package every consumer can import; the alternative is
// the SECOND copy, which drifts the moment either side learns a new flag, and
// the drift is invisible because both copies keep compiling).
//
// ── WHY THE COMMAND EXISTS AT ALL ───────────────────────────────────────────
//
// `go build ./...` DOES NOT COMPILE `_test.go`. That is not a tuning issue, it
// is what the toolchain does, so a branch whose TEST TREE does not compile is
// green to every build-shaped measurement, lands, and takes the test package
// down for everyone after it. Measured twice on this repository:
//
//	2026-09-16  a `funcBody` redeclared between a production file and a test
//	            file in one package — the union gate's incident.
//	2026-10-12  internal/tools lost NotifyEventLedgerDivergence, which a
//	            cmd/agent redproof test still referenced; `go build ./...`
//	            exit 0, `go vet ./cmd/agent/` reported `undefined:`.
//
// ── WHY THE FILTER IS `^$` AND NOT A TEST NAME ──────────────────────────────
//
// `-run '^$'` matches no test, so every test BINARY is BUILT and nothing is RUN:
// no ledger is opened, no daemon started, no fixture executed. Rule 27's ban on
// manufacturing load is respected BY CONSTRUCTION rather than by discipline —
// the command cannot saturate anything, because no test body ever executes.
//
// ── AND WHY NOT `go vet` (the incident's own repro command) ─────────────────
//
// A VET LEG WAS DELIBERATELY REJECTED, by the union gate, with the measured
// reason recorded there: a vet leg buys a class of findings that are NOT merge
// hazards. In Go 1.24 `unsafe.Pointer`→`uintptr` conversions in test files are
// reported (a field offset is inherently unsafe), so vet would refuse branches
// whose failing package is byte-identical to the base. The refusal must stay as
// narrow as the defect it closes.
package testcompile

import (
	"fmt"
	"time"
)

const (
	// GoBinary is the toolchain word the command opens with. It is spelled
	// `go` rather than an absolute path because the premise each caller checks
	// before running it is "this tree is a Go module", never "this host has the
	// toolchain at a known path": a host without it gets the child's own
	// "executable file not found", which every caller reports as its own
	// ABSTENTION rather than as a defect of the branch (Rule 15 — a leg that
	// cannot run says so; it never passes silently).
	GoBinary = "go"

	// RunNothingFilter is the `-run` value that matches no test. It is the
	// whole reason this command is cheap: the toolchain builds and links every
	// test binary and then runs nothing.
	RunNothingFilter = "^$"

	// CommandFormat is the ONE spelling of the test-compile pass, with two %s
	// slots: the toolchain word and the run-nothing filter. It is the format
	// because one caller (the union gate) composes the command from its own leg
	// struct rather than taking a finished string, and a finished string beside
	// a format would be the second spelling this package exists to prevent.
	//
	// Every fragment of it occurs exactly ONCE in the tree — the property a
	// grep can check (Rule 13's exhaustive-literal discipline, applied to a
	// command line): `Command` below is DERIVED from this, never re-typed.
	CommandFormat = "%s test -run '%s' -count=1 ./..."

	// TimeoutSec bounds the command, in seconds, for the callers that express a
	// budget as an integer (the merge gates' leg specs).
	TimeoutSec = 300

	// Timeout is the same budget for callers that take a time.Duration. It is
	// the SAME number, derived, never a second decision about "how long may
	// this take" (Rule 2/14).
	Timeout = time.Duration(TimeoutSec) * time.Second
)

// Command is the finished command, for the callers that execute it verbatim.
// Both merge gates and the clone channel's delivery loop run THIS string;
// nobody re-types it.
//
// A var and not a const because `fmt.Sprintf` is not a constant expression — and
// the derivation is the point: this is the ONE place the format is filled in, so
// the finished spelling cannot drift from the format the union gate composes
// with.
var Command = fmt.Sprintf(CommandFormat, GoBinary, RunNothingFilter)
