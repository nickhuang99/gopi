package main

// daemon_retired.go — the RETIREMENT of the merger daemon (2026-09-19), and the
// only surface of it that remains in this package.
//
// (This file was `merger_retired.go` until 8cf39fb3 renamed it `-M` with no
// content change; the old name survives in comments that mean this code.)
//
// WHY IT IS RETIRED, NOT FIXED: `gopi merger --stop` answers at the TICK BOUNDARY
// and therefore cannot interrupt an in-flight tick. The operator report is one
// line and carries the whole argument:
//
//	$ gopi merger --stop
//	[merger] gopi merger: stop requested — pid 552530, terminated
//	(answered at the tick boundary; the in-flight tick is not interrupted)
//
// A resident service the operator cannot stop is not a service to patch in place
// — it is the ground for a RE-DEVELOPMENT. devdoc/merger-retired.md carries the
// report, the two live entry points and the re-development notes.
//
// WHAT IS NOT RETIRED: the ONE-SHOT merge face `gopi merge` (merge_run.go,
// the dispatch file, merge_build_gate*.go, merge_ceiling_rework.go,
// merge_contract_gate.go, merge_revert.go, merge_union_gate.go, task_merge*.go)
// and the merge WORK LOCK (merger_work_lock.go) are the KEPT merge pipeline and
// keep working unchanged. `gopi task merge` is where a merge happens now; NOTHING
// in this retirement touches it.
//
// THE IMPLEMENTATION IS DELETED, THE ENTRY POINTS STAY CLOSED (stage 2, epic
// merger-delete-dead-impl, 2026-09-19). Two live entries reached the resident
// merger:
//
//  1. isMergerSubcommand (this file) — the top-level `gopi merger` verb.
//  2. the taskverbs.Serve / taskverbs.Merger case (cli_tasks.go) — the
//     DEPRECATED `gopi task merger` and `gopi task serve` aliases.
//
// Stage 1 CLOSED both and kept the implementation in the tree, unreachable, so
// the re-development could start from working code. Stage 2 DELETED it — the
// loop, the tick, the take-over launcher, the control faces and the status
// surface — because unreachable code that still compiles is not a re-development
// head start: it is a face nobody can reach, kept alive by nothing, which the
// next reader has to prove is dead all over again. The design's record is
// devdoc/merger-retired.md plus `git show <stage-1 commit>` of this package,
// which is exactly the evidence Rule 23 wants kept (the path walked, readable,
// out of the way).
//
// THE COMPILER IS THE GUARANTEE, and it is a real one: with the implementation
// gone, a future edit cannot quietly re-wire the daemon — it has to restore the
// code and the entry, deliberately. What remains here is the REFUSAL, which must
// keep happening for every spelling of the verb.

import (
	"fmt"
	"os"
)

// mergerProgTopLevel is the top-level program name the merger family's messages
// print (`gopi merger`). Re-homed here from the dispatch that was deleted: the
// shared work lock's admission messages (merger_work_lock.go) still quote the
// process they are talking about, and a message that named the wrong program
// would send an operator to a verb that no longer exists (Rule 1: the spelling is
// declared once).
const mergerProgTopLevel = "gopi merger"

// mergerRetiredCause is the ONE statement of WHY (Rule 1b: no re-typed prose at
// the call sites). It names the observed defect and where the record lives.
const mergerRetiredCause = "the resident merger daemon was retired for re-development: `--stop` is only answered at a TICK BOUNDARY, so an in-flight tick cannot be interrupted — a merge the operator cannot stop is not patched in place (see devdoc/merger-retired.md)"

// mergerRetiredPointer names what remains, so an operator who reached for the
// retired verb learns the truth about merging instead of concluding that this
// refusal is about their word alone.
//
// IT USED TO SAY `gopi merge` and that address is gone (epic merge-removal,
// item 2), so printing it would send an old script from one refusal into
// another — the dead-end hint checkVerbAliasRows refuses. The surviving
// spelling is the FAMILY one, and it is not a guess: `gopi task merge` is the
// git atom the post-receive hook types, it shares the same engine and the same
// single-instance lock, and it was MEASURED landing a real branch before this
// sentence was written (2026-10-31: `gopi task merge feat-x --repo <probe>`
// printed "merge feat-x merged clean merge, branch deleted"). "Merging is gone"
// would have been a false statement about this binary, which is a worse hint
// than none.
const mergerRetiredPointer = "merging itself is NOT retired: it is reached through the family spelling `gopi task merge` (same engine, same lock)"

// retireMergerFace closes ONE entry point of the retired merger daemon: it
// reports the retirement (cause + what to use instead) and exits non-zero. It
// never falls through — a caller that kept going would be the silent no-op the
// retirement exists to prevent (Rule 15: no silent failure).
//
// invoked is the spelling the operator typed, from the verb constants
// (CliVerbMerger, taskverbs.Merger, taskverbs.Serve) — never a literal.
func retireMergerFace(invoked string) {
	fmt.Fprintf(os.Stderr, "%s: retired — %s\n  %s\n", invoked, mergerRetiredCause, mergerRetiredPointer)
	os.Exit(1)
}

// isMergerSubcommand reports whether argv begins with the `merger` verb — the
// merge component's top-level CLI face (`gopi merger`), promoted out of the
// `gopi task` group (epic merger-daemon-flag, 2026-09-12).
//
// IT REPORTS A RETIREMENT RATHER THAN DISPATCHING A FACE (2026-09-19). The verb
// is still RECOGNISED — every spelling of it, `-h` and an unknown flag included,
// lands on the same sentence — because falling through to the top-level parser
// would answer an operator's `gopi merger` with "unknown command", which reads as
// a regression instead of a decision (Rule 15).
//
// The refusal sits FIRST, before any help scan, any config resolution and any
// parser, so no spelling of the verb can take a lock, spawn a child or write
// state on its way to the message.
func isMergerSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbMerger {
		return false
	}
	retireMergerFace(CliVerbMerger)
	return false // unreachable: retireMergerFace exits
}
