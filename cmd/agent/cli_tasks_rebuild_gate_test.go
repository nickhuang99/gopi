package main

// The DURABLE close loop's cmd/agent half (epic 2026-09-16, branch
// fix/merge-rebuild-loop): master advancing and `./bin/gopi` following it are two
// STEPS of ONE tick, and nothing guarantees the one process performs both. The
// LANDING happens in the tick's decision tree (internal/tools/repo_sweep.go) and
// the rebuild is a LATER step of the same tick (mergerSweepRepo →
// serveRebuildAfterMerge), so the gap between them is a window a pass can die in.
//
// WHAT THESE TESTS PIN (each is a red-proof: every one of them fails when its
// half of the wiring is REMOVED — the removal is named in each case and was run):
//
//	① THE GATE'S LANDING BOTH MARKS AND REBUILDS (source-level): the landing arm
//	   must leave the durable mark AND the landed path must reach the close
//	   loop's rebuild, with the mark inside the landing arm (after the fast
//	   forward, before the not-ff fall-through). The mark is the half that survives
//	   the process — the 07:29:34 evidence shows a pass dying with master
//	   advanced and `bin/gopi` left at a PREVIOUS position, and no later round
//	   came back for that position: the branch is gone, so `merged=0` and the
//	   per-round rebuild hook cannot fire.
//	② THE FULL WIRING (source-level): the pass's arming of the close loop, its
//	   START-of-pass drain, the close loop's own drain+rebuild, the mark's
//	   clear-on-success, the runner chain the daemon's loop really calls, and the
//	   pass-totals record — so a future refactor cannot delete one arm and leave
//	   the others green.
//
// THE MECHANISM (a mark is written, drains a stale binary, and is cleared) is
// covered where the primitives live: internal/tools/rebuild_pending_test.go,
// internal/tools/repo_sweep_notff_test.go (the landing itself writes the mark) and
// cmd/agent/cli_tasks_rebuild_pass_test.go (the drain inside the close loop).
//
// RE-ANCHORED 2026-09-17 (task cmd-agent-red-reanchor). These two cases were
// red-proofs against the POST-MERGE VERIFY CHILD, and that machinery was
// deliberately deleted whole by 126fad74 ("refactor(merger): ff-only or
// park-and-file — no child, no wait, no state"): merger_verify_child.go is gone,
// mergerVerifyGate is gone, mergerRunPass is gone with the per-pass state file.
// The cases kept reading those symbols, so on a clean checkout they failed with
// "no such file or directory" / "declaration not found" — a stale anchor, never
// a regression, and zero signal. The INVARIANTS they were written for SURVIVED
// that refactor and are re-pinned here onto the symbols that replaced the
// deleted ones, at the same strength:
//
//	old anchor                        → new anchor, same invariant
//	──────────────────────────────────────────────────────────────────────────────
//	merger_verify_child.go:           → internal/tools/repo_sweep.go:
//	  mergerVerifyGate's attempt+mark,    repoSweepDecideBranch's landing arm
//	  both BEFORE the judge                 (mark + the rebuild it protects),
//	                                        both inside the landing arm and
//	                                        before the not-ff fall-through
//	mergerRunPass +                   → mergerPassOnce + the runner chain
//	  mergerPunchPassEnd                  runTaskMerger → runTaskMergerWithLock,
//	                                      which reaches the pass body.
//	                                      (The tick-boundary record this row
//	                                      used to name went with the resident
//	                                      loop — epic daemon-machinery-removal,
//	                                      2026-09-17; see the anchors below.)
//
// The RED-PROOF for each case is a REMOVAL, not an assertion about the past:
//
//	# ① delete the MarkRebuildPending(...) line from repoSweepDecideBranch's
//	#    landing arm (internal/tools/repo_sweep.go) → case ① FAILS;
//	# ② delete the `defer func() { mergerPassCloseLoop(repos) }()` line or the
//	#    mergerPassCloseLoop(nil) START drain from mergerPassOnce → case ② FAILS.
//	go test -run 'TestVerifyGateLandingCarriesTheDurableCloseLoop|TestCloseLoopWiringIsComplete' ./cmd/agent/

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The anchors are NAMED values (Rules 1b/13): a test whose subject IS a symbol
// must spell that symbol once, where a reader looks for it. The old anchors
// (srcFile = "merger_verify_child.go", gateFunc = "func mergerVerifyGate(",
// judge = "verdict := runMergeVerifyChild(rec, beforeTip)", the assembly
// `mergerRunPass` + `mergerPunchPassEnd`) were deleted with the machinery they
// named; each is replaced by the symbol carrying the same invariant today.
const (
	// landingDecideDecl locates the tick's landing decision — the function whose
	// body holds the single place a branch becomes master.
	landingDecideDecl = "func repoSweepDecideBranch("
	// landingCall is the fast-forward that IS the landing. Everything the mark
	// protects is measured from here: master advances at this statement.
	landingCall = `repoSweepGit(repo, "merge", "--ff-only", branch)`
	// landingMark is the durable mark the landing must leave. It is compared
	// against the production text, so the constant's NAME (not its value) is what
	// keeps the two in step: renaming the constant fails here by design, because
	// the mark must name a reason a reader can look up (Rules 1b/13).
	landingMark = "MarkRebuildPending(repo, MarkRebuildAfterLandingReason)"
	// landingNotFF is the non-ff fall-through: a branch that did NOT land must
	// reach the report arm, and the mark must not sit after it (a reported branch
	// is not on master, so there is nothing there for a mark to rebuild).
	// Renamed from landingPark when the park arm was replaced by the report
	// inlining (epic merger-remove-park, 2026-09-17): the arm still exists, its
	// name for a non-ff branch is now the `not-ff` action.
	landingNotFF = "repoSweepNotFFOutcome(branch)"
	// sweepCall / sweepRebuild are the LATER step the mark protects: the pass
	// runs the sweep and, when it landed or pruned work, reaches the rebuild —
	// which is exactly the step a pass can be killed before.
	sweepSrcFile  = "cli_tasks.go"
	sweepCall     = "tools.MergerRepoSweep(repo, 0)"
	sweepRebuild  = "serveRebuildAfterMerge(repo, rvt)"
	sweepLandedAt = "if res.Merged > 0 || res.Deleted > 0 {"
	// passDecl / closeLoopDecl locate the pass body (the ONE function both the
	// pre-loop tick and the poll-loop tick call) and the close loop itself.
	passSrcFile      = "cli_tasks.go"
	passDecl         = "func mergerPassOnce("
	closeLoopSrcFile = "cli_tasks_rebuild.go"
	closeLoopDecl    = "func mergerPassCloseLoop("
	// runChainDecl / runChainWithLock / runChainHandoff are the runner chain the
	// merger's entry point actually calls. The old anchor (mergerRunPass) died
	// with the state file; the surviving chain has TWO links, so the case
	// asserts the whole chain — every link is a place a future refactor can cut
	// the tick off from the pass, and cutting any one of them leaves both drain
	// arms dead in production. Both links live in ONE file since the cron form
	// extracted the tick (cmd/agent/merger_tick.go, epic
	// daemon-machinery-removal, 2026-09-17): the runner used to be split across
	// cli_tasks.go and cli_merger_daemon.go, and the daemon half is gone with
	// the resident form, so the chain is read from its one remaining home.
	//
	// THE TICK-BOUNDARY STAMP IS NO LONGER ASSERTED HERE (same epic). It was the
	// resident loop's own line — `tools.MergerTickMarker` + `MergerClockLayout`
	// printed at the TOP of each poll iteration, the record an interrupted
	// resident tick was recovered from, and what --status read its schedule
	// from. A one-tick cron process has no iterations and no idle phase to
	// recover: its boundary IS the process, and the stamp that remains is the
	// "--once: single tick done" completion line the E2E suites wait for. Asking
	// this gate for the stamp now would demand a line belonging to a deleted
	// form, which is the opposite of a red-proof.
	// THE RUNNER CHAIN WENT WITH THE DAEMON (epic merger-delete-dead-impl,
	// 2026-09-19). The two links asserted here used to be `runTaskMerger` →
	// `runTaskMergerWithLock` → `mergerPassOnce(opts)`, declared in
	// merger_tick.go. Both declarations were deleted with the retired resident
	// merger, so the chain this case asserted no longer has a subject — and the
	// pass body itself (mergerPassOnce, kept in cli_tasks.go for the
	// re-development, still carrying the Rule-12 mark/drain arms) has no
	// production caller today.
	//
	// WHAT REPLACES IT IS A LIVE CARRIER, NOT A WEAKER ASSERTION. The drain has
	// another reader that survived the retirement — the one-shot `gopi task
	// merge-execute` landing path, which reaches mergerPassCloseLoop through
	// mergerCloseLoopAfterLanding. That link is asserted below, in the file that
	// declares it, so "the drain is still reached by shipped code" stays a
	// claim this test can fail on. Re-instating the merger's own chain is a duty
	// of the re-development, and this constant block is where it goes back.
	liveDrainSrcFile = "cli_tasks.go"
	liveDrainDecl    = "func runTaskMergeExecute("
	liveDrainCall    = "mergerCloseLoopAfterLanding(rep.Repo)"
)

// landingSrcFile locates the tick's decision file, which lives in the sibling
// package (a path, so it is a var: filepath.Join is not a constant expression).
var landingSrcFile = filepath.Join("..", "..", "internal", "tools", "repo_sweep.go")

// readDeclBody reads one source file and returns the body of the function whose
// declaration is decl — the shared prologue of every source-level case here.
func readDeclBody(t *testing.T, file, decl string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	return braceFuncBody(t, string(b), decl)
}

// TestVerifyGateLandingCarriesTheDurableCloseLoop is the ① red-proof, in two
// halves that must hold TOGETHER: the landing leaves the mark (the durable half)
// and the landed path reaches the rebuild (the half that makes the mark
// meaningful). Either alone is a false green — a mark nothing rebuilds, or a
// rebuild a killed pass steps over.
//
// The assertion is on the landing function's own body, because that is where the
// merge happens: a mark written anywhere else is written at a moment that is not
// the landing, which is the window the mark exists to cover.
func TestVerifyGateLandingCarriesTheDurableCloseLoop(t *testing.T) {
	body := readDeclBody(t, landingSrcFile, landingDecideDecl)

	landAt := strings.Index(body, landingCall)
	if landAt < 0 {
		t.Fatalf("%s no longer contains %q — the landing this test anchors on moved; update it deliberately",
			landingSrcFile, landingCall)
	}
	markAt := strings.Index(body, landingMark)
	if markAt < 0 {
		t.Fatalf("%s does not leave the durable mark (%q) — master advances and the rebuild is a LATER step of the same tick, so a pass killed in between leaves the artifact behind FOREVER: the branch is gone, `merged=0`, and no later round comes back for that position (measured: the 07:29:34 landing with bin/gopi left at 07:17:53)",
			landingSrcFile, landingMark)
	}
	if markAt < landAt {
		t.Fatalf("%s marks the repo BEFORE the landing at body offset %d — the mark must be written the moment master advances, not before there is anything to rebuild", landingSrcFile, landAt)
	}
	// The mark must belong to the LANDING ARM, not to the function's tail: past
	// the not-ff fall-through the branch provably did NOT land, so a mark there
	// would be written for a branch that is not on master.
	if notFFAt := strings.Index(body, landingNotFF); notFFAt >= 0 && markAt > notFFAt {
		t.Fatalf("%s marks the repo AFTER the not-ff fall-through (mark at %d, fall-through at %d) — a branch the tick could not land is not on master, so there is nothing for that mark to rebuild",
			landingSrcFile, markAt, notFFAt)
	}

	// ①b the OTHER half: the landed path must really reach the rebuild. A mark
	// whose drain rebuilds nothing would discharge the obligation on the floor.
	sweep := readDeclBody(t, sweepSrcFile, sweepDeclFor(sweepCall))
	sweepAt := strings.Index(sweep, sweepCall)
	if sweepAt < 0 {
		t.Fatalf("%s no longer runs %q — the landing path this close loop belongs to moved", sweepSrcFile, sweepCall)
	}
	if !strings.Contains(sweep, sweepLandedAt) {
		t.Fatalf("%s no longer branches on the landed/pruned condition (%q) — the rebuild would run for a tick that landed nothing, or never at all", sweepSrcFile, sweepLandedAt)
	}
	if !strings.Contains(sweep, sweepRebuild) {
		t.Fatalf("%s does not reach %q after its sweep — a landing whose rebuild is a LATER step of the tick is exactly the window the durable mark covers, and without this arm the mark can only ever be drained by a LATER pass",
			sweepSrcFile, sweepRebuild)
	}
}

// sweepDeclFor is the declaration of the function the sweep call lives in: the
// caller is named by the call site, so the declaration is the caller's own
// (mergerSweepRepo today) — a constant, not a guess about where the call sits.
func sweepDeclFor(string) string { return "func mergerSweepRepo(" }

// TestCloseLoopWiringIsComplete is the ② wiring red-proof: every arm of the
// durable close loop must be present together. A refactor that keeps the mark
// writer but drops the drain would accumulate marks forever; one that keeps the
// drain but drops the landing's mark would lose the landing entirely.
func TestCloseLoopWiringIsComplete(t *testing.T) {
	rebuildSrc, err := os.ReadFile(closeLoopSrcFile)
	if err != nil {
		t.Fatalf("read %s: %v", closeLoopSrcFile, err)
	}
	loopBody := braceFuncBody(t, string(rebuildSrc), closeLoopDecl)
	if !strings.Contains(loopBody, "tools.RebuildPendingRepos()") {
		t.Fatalf("%s does not drain tools.RebuildPendingRepos() — the durable marks are never discharged, so an interrupted landing's rebuild is lost forever", closeLoopDecl)
	}
	if !strings.Contains(loopBody, "serveRebuildAfterMerge(repo, nil)") {
		t.Fatalf("%s drains a mark without reaching the rebuild — the mark would clear nothing", closeLoopDecl)
	}
	if !strings.Contains(string(rebuildSrc), "tools.ClearRebuildPending(repo)") {
		t.Fatalf("serveRebuild does not clear the durable mark on success — a mark that survives its own drain rebuilds the repo on every pass forever")
	}
	passBody := readDeclBody(t, passSrcFile, passDecl)
	if !strings.Contains(passBody, "defer func() { mergerPassCloseLoop(repos) }()") {
		t.Fatalf("%s no longer arms the pass close loop — the drain never runs on a normal pass", passDecl)
	}
	// ...and the START-of-pass drain (see cli_tasks_rebuild_pending_test.go for
	// the ordering red-proof): the pass END is exactly the step a KILLED pass
	// never reaches, because a SIGKILL runs no defer.
	if passSrc, err := os.ReadFile(passSrcFile); err != nil {
		t.Fatalf("read %s: %v", passSrcFile, err)
	} else if drainAt := passStartDrain(t, string(passSrc), passBody); drainAt < 0 {
		t.Fatalf("%s does not drain the durable marks at its start — a killed pass (measured: passes=0 state files) leaves the artifact stale", passDecl)
	}
	// THE DRAIN MUST BE REACHED BY SHIPPED CODE. The runner chain that used to
	// carry this (runTaskMerger → runTaskMergerWithLock → mergerPassOnce) went
	// with the retired daemon, so the assertion moved to the carrier that
	// survived: the one-shot merge-execute landing path's own close loop. A
	// drain reachable only from a test is a drain that runs in production
	// never, which is the exact defect this case exists to catch.
	if body := readDeclBody(t, liveDrainSrcFile, liveDrainDecl); !strings.Contains(body, liveDrainCall) {
		t.Fatalf("%s does not contain %q — the durable-mark drain lost its LAST shipped carrier: the merger daemon's chain is gone (epic merger-delete-dead-impl, 2026-09-19), so if this one goes too, Master can advance with no step that ever rebuilds it",
			liveDrainDecl, liveDrainCall)
	}
}
