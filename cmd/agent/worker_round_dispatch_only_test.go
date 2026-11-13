package main

// worker_round_dispatch_only_test.go — the WORKER ROUND BOUNDARY, enforced.
//
// THE RULE (operator ruling, 2026-10-26):
//
//	"主线的任务就是说由你来掌握 … 大的这个代码的重构 … 必须由你来盯梢"
//	— and, for this file's subject: a WORKER's round is CLAIM → RUN → REPORT.
//	Ledger reconciliation, ground cleanup, scratch GC, ref/code delivery and
//	notify scanning are NOT a worker's business.
//
// WHY THIS FILE EXISTS AT ALL. The boundary was a COMMENT before it was a test,
// and a comment cannot enforce itself. The measured history is the argument: the
// worker's round accreted `runTaskLifecycleTick` (three ledger legs),
// `scratchGCSweepAsync` (temp-dir GC) and `serveGitSyncSweep` (ref advancement),
// each added by a REACHABILITY argument ("something resident must run it") rather
// than a SCOPE argument ("this duty is a task dispatcher's") — and every one of
// them was written with a comment explaining why it belonged. A reader coming
// next has the same argument available and no instrument to answer it, so the
// boundary must be a SCAN.
//
// WHAT IT DOES. It parses this package's OWN source, finds the worker round loop
// (`for round := 1; maxRounds == 0 || round <= maxRounds; round++` in
// cli_tasks_worker.go), extracts the span between the round's DISPATCH marker
// (①) and its claim call, and asserts that no call in that span is a
// sweep/tick/GC/reap — the naming convention those legs are all required to
// follow (Rule 1b: a duty named for what it does is what makes a scan possible).
//
// WHAT IT DOES NOT DO, honestly: it reads the round region and the call shapes it
// knows how to see. A leg reached indirectly (a helper the round calls, which
// calls a sweep) is NOT caught — the scan judges the round's own statements, and
// that limit is stated here rather than papered over. The red proof below is what
// shows the scan is not vacuous: plant a fixture and it must fail.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// workerRoundFile is the ONE source file that holds the round loop. Named
	// rather than globbed so the scan cannot silently cover zero files.
	workerRoundFile = "cli_tasks_worker.go"

	// workerRoundLoopHead identifies the loop. It is the literal header of
	// runTaskWorkerLoop's round; if the loop is rewritten so this no longer
	// matches, the test FAILS LOUDLY instead of scanning nothing (a scan that
	// looks at no code and passes is the defect this class of gate exists to
	// avoid — Rule 15).
	workerRoundLoopHead = "for round := 1; maxRounds == 0 || round <= maxRounds; round++ {"

	// workerRoundBoundaryMarker is the round's own "this is dispatch" marker.
	// The span scanned runs from it to the claim call below.
	workerRoundBoundaryMarker = "① THE ROUND IS DISPATCH AND NOTHING ELSE"

	// workerRoundClaimCall is where the round stops being prologue and starts
	// doing the one thing it is for. Everything between the marker and this call
	// is the region the rule governs.
	workerRoundClaimCall = "workerRoundClaiming("

	// workerImageRoundDecl and workerImageRoundCall are the round check's own
	// anchors (row 01a18301): the declaration must precede the loop (so the
	// once-per-change memory survives rounds) and the call must be inside it (so
	// a stale image is reported at every round boundary, not only at the
	// prologue that was clean hours before the artifact was replaced).
	workerImageRoundDecl = "imageAlarm := newWorkerImageRoundCheck()"
	workerImageRoundCall = "imageAlarm.Report(workerProgTopLevel)"
)

// workerBannedDutyPrefixes are the naming conventions the removed legs follow.
// A call whose name ends in one of these, inside the dispatch-only region, is a
// resident duty riding a claim loop — the exact shape the ruling removed.
//
// The list is deliberately NAME-BASED rather than an exhaustive list of the
// functions that were removed: a future author adding `serveFooSweep` or
// `barGCTick` should be stopped by the naming they would naturally use, not by
// having to remember a list. An author who deliberately names a resident duty
// outside the convention is doing so knowingly, and no text gate can decide
// intent — that limit is stated in this file's header rather than pretended
// away.
var workerBannedDutyTokens = []string{
	"Tick", "Sweep", "GC", "Reap",
}

// workerCallRe finds a call expression statement in the round region. Go source
// in this file is gofmt'd, so a call at statement position begins a line (after
// whitespace) with an identifier or a package selector.
var workerCallRe = regexp.MustCompile(`(?m)^\s*(?:if\s+!?\w+\s*\{\s*)?([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)\s*\(`)

// TestWorkerRoundIsDispatchOnly is the boundary check. It is the guard the
// removed legs' own comments never had.
func TestWorkerRoundIsDispatchOnly(t *testing.T) {
	src, err := os.ReadFile(workerRoundFile)
	if err != nil {
		t.Fatalf("read %s: %v — the scan has no subject, so it cannot pass", workerRoundFile, err)
	}
	region, ok := workerDispatchOnlyRegion(string(src))
	if !ok {
		t.Fatalf("%s: could not locate the round's dispatch-only region (loop head %q, marker %q, claim call %q) — the loop was rewritten, so this gate would have judged nothing and passed. Re-point the scan at the new shape instead of deleting it.",
			workerRoundFile, workerRoundLoopHead, workerRoundBoundaryMarker, workerRoundClaimCall)
	}

	calls := workerCallsIn(region)
	// AN EMPTY REGION IS THE PASSING STATE HERE, not a vacuous one: the ruling's
	// whole point is that nothing but dispatch sits between the round boundary
	// and the claim. The non-vacuity that matters — "would this scan catch a leg
	// if one came back?" — is proved by TestWorkerRoundScanIsNotVacuous, which
	// plants the real retired calls and requires them caught. A gate that failed
	// on an empty region would make the CORRECT state look like a defect, and the
	// first person to hit that would delete the gate.
	var offences []string
	for _, c := range calls {
		if name := workerCallBaseName(c); workerIsBannedDuty(name) {
			offences = append(offences, c)
		}
	}
	if len(offences) > 0 {
		t.Errorf(`the worker's round is CLAIM -> RUN -> REPORT (operator ruling, 2026-10-26), and these calls put a resident duty back on it:

  %s

Each is a duty that has an EXPLICIT one-shot face an operator invokes by name:
  the failed-needs-reissue census                     -> gopi task list --status failed
  ownership-independent registration reap             -> gopi task sweep-worktrees --apply
  scratch / temp directory GC                         -> gopi tool scratch-gc tick
  ref / code delivery                                 -> gopi task sync-pass --once

Two rows stood here and no longer do, because the spelling they named answers a
removal sentence and exits 1 — a dead spelling is not a one-shot face, and
keeping it here taught a reader a command that cannot work:
  - the stale report (gopi task list --sweep): deleted end to end 2026-11-05,
    commit e4521fad, on the operator's ruling that no process reports another
    process's health. What replaced it is a READ, not a duty: gopi task list
    resolves each row's holder per row and reports it, never acts on it.
  - terminal-row close-out alone (gopi task finish-cleanup --apply): retired
    2026-10-30, commit 58b5618b. What replaced it is an ordinary state write:
    gopi task update <id> --status completed|failed.
Neither replacement is a resident duty on the round, so neither belongs in the
list above.
Add the call to the ONE-SHOT verb, not to this loop: a worker that stops claiming
for a fetch or a sweep is no longer the thing that runs tasks.`,
			strings.Join(offences, "\n  "))
	}
}

// workerDispatchOnlyRegion extracts the round span the rule governs: from the
// dispatch marker to the round's claim call. It returns ok=false when any anchor
// is missing, so a rewritten loop FAILS rather than silently scanning nothing.
func workerDispatchOnlyRegion(src string) (string, bool) {
	loop := strings.Index(src, workerRoundLoopHead)
	if loop < 0 {
		return "", false
	}
	marker := strings.Index(src[loop:], workerRoundBoundaryMarker)
	if marker < 0 {
		return "", false
	}
	start := loop + marker
	claim := strings.Index(src[start:], workerRoundClaimCall)
	if claim < 0 {
		return "", false
	}
	return src[start : start+claim], true
}

// workerCallsIn returns every statement-position call in the region, as its
// source spelling (e.g. "serveGitSyncSweep" or "tools.SweepFailedNeedsReissue").
//
// Comment lines are skipped FIRST: the region is mostly prose explaining what
// used to be here, and a scan that read its own history as code would fail on
// every retired leg's name — the precise false positive that makes a gate get
// deleted instead of fixed.
func workerCallsIn(region string) []string {
	var calls []string
	for _, line := range strings.Split(region, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		m := workerCallRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		calls = append(calls, m[1])
	}
	return calls
}

// workerCallBaseName strips a package selector: "tools.SweepFailedNeedsReissue"
// -> "SweepFailedNeedsReissue".
func workerCallBaseName(call string) string {
	if i := strings.LastIndex(call, "."); i >= 0 {
		return call[i+1:]
	}
	return call
}

// workerIsBannedDuty reports whether a call name declares a resident duty by the
// convention those legs follow.
//
// CONTAINS, not suffix: the measured legs are `runTaskLifecycleTick`,
// `scratchGCSweepAsync`, `serveGitSyncSweep` and `serveWorktreeReapSweep` — one of
// them ends in `Async`, so a suffix rule would have waved the GC sweep through.
// This was caught by the red proof below, which is why that proof exists.
func workerIsBannedDuty(name string) bool {
	for _, token := range workerBannedDutyTokens {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

// TestWorkerRoundScanIsNotVacuous plants known-bad shapes through the SAME
// predicates the real scan uses, beside clean controls. Without this, a scan that
// reached no code would read green forever — the failure mode this repository has
// already paid for twice (a named check that matched no test printed
// "no tests to run" and exited 0).
func TestWorkerRoundScanIsNotVacuous(t *testing.T) {
	fixture := workerRoundBoundaryMarker + `
		// a COMMENT naming a retired leg must NOT trip the scan:
		// runTaskLifecycleTick(workerRoleLabel)
		markWorkerRoundStarted()
		serveGitSyncSweep(workerRoleLabel)
		if !once {
			scratchGCSweepAsync(workerRoleLabel)
		}
		runTaskLifecycleTick(workerRoleLabel)
		n := workerRoundClaiming()
`
	calls := workerCallsIn(fixture)
	var banned, clean []string
	for _, c := range calls {
		if workerIsBannedDuty(workerCallBaseName(c)) {
			banned = append(banned, c)
		} else {
			clean = append(clean, c)
		}
	}

	// RED: the three planted resident duties are all caught, including the one
	// behind an `if` guard (a conditionally-fired tick is still a tick).
	wantBanned := []string{"serveGitSyncSweep", "scratchGCSweepAsync", "runTaskLifecycleTick"}
	for _, want := range wantBanned {
		found := false
		for _, b := range banned {
			if b == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the scan did NOT catch the planted resident duty %q; it caught %v", want, banned)
		}
	}
	// CLEAN: the dispatch calls and the comment's mention are not offences. This
	// is the half that keeps the gate from being deleted for crying wolf.
	for _, c := range clean {
		if c == "markWorkerRoundStarted" || c == "workerRoundClaiming" {
			continue
		}
		if c == "runTaskLifecycleTick" || c == "serveGitSyncSweep" || c == "scratchGCSweepAsync" {
			t.Errorf("%q was classified clean — the classifier is inverted", c)
		}
	}
	if len(calls) != 4 {
		t.Errorf("the fixture has 4 code calls and 1 commented-out mention; the scan saw %d (%v) — a comment must not be read as code", len(calls), calls)
	}
}

// TestWorkerRoundRegionIsFoundInTheRealTree pins the anchors on the REAL source,
// so an anchor that rots fails HERE with a clear name rather than turning the
// boundary check into a silent no-op.
func TestWorkerRoundRegionIsFoundInTheRealTree(t *testing.T) {
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, workerRoundFile, nil, parser.SkipObjectResolution); err != nil {
		t.Fatalf("parse %s: %v", workerRoundFile, err)
	}
	src, err := os.ReadFile(workerRoundFile)
	if err != nil {
		t.Fatalf("read %s: %v", workerRoundFile, err)
	}
	region, ok := workerDispatchOnlyRegion(string(src))
	if !ok {
		t.Fatalf("the round's dispatch-only region was not found in %s — anchors: loop %q / marker %q / claim %q",
			workerRoundFile, workerRoundLoopHead, workerRoundBoundaryMarker, workerRoundClaimCall)
	}
	// The region must not be trivially short: a marker and claim call adjacent by
	// accident would make every scan above pass on nothing.
	if len(strings.TrimSpace(region)) < 200 {
		t.Errorf("the dispatch-only region is only %d bytes — too short to be the real boundary, so the scan would judge nothing:\n%s", len(region), region)
	}
	_ = ast.Print // keep the ast import honest: the parse above is the syntax gate
}
