// Package taskverbs is the single source of truth for the `gopi task` FAMILY
// verb vocabulary shared across packages (Rule 14: a value two packages must
// agree on lives in ONE small exported-constant leaf package — never mirrored,
// never typed inline, Rule 1b/13).
//
// The package is a leaf: it imports NOTHING but the standard library, so no
// consumer can create an import cycle (internal/config imports it, which is why
// the constants cannot live in cmd/agent).
//
// WHY IT EXISTS: the family surface is documented in three places that
// physically cannot import each other — the top-level `gopi -h` command index
// (internal/config/help.go), the family's own help (cmd/agent/cli_tasks.go
// tasksUsage, a raw string its test scans), and the `gopi task <TAB>`
// completion candidate set (cmd/agent/cli_completion.go). Each was maintained
// by hand, so a verb added to dispatch + completion could silently miss the
// top-level index: the 2026-09-13 `status` incident (added by d7474fb4 with
// its completion entry and its tasksUsage block, absent from help.go's
// enumeration) left TestTopLevelUsageTaskVerbEnumMatchesCompletion and
// TestTopLevelUsageShowsTaskLedger red on master. The top-level row now
// ENUMERATES this list instead of restating it, so the surface is derived,
// not re-typed — the drift class is closed rather than this one instance
// patched.
//
// SCOPE: the family verbs only. The group verb (`task`) and the top-level
// resident services (worker, merger — peers of the group, documented in their
// own index rows) are NOT family verbs and live in cmd/agent/cli_verbs.go.
//
// tasktags / taskverdict are the sibling precedents (persisted tag and
// reframe-verdict vocabularies) and follow the same leaf shape: constants
// only, no imports of project packages, so no consumer can create an import
// cycle — internal/config imports this package, which is exactly why the
// constants cannot live in cmd/agent.
package taskverbs

import "sort"

const (
	// Adopt is `gopi task adopt` — take over an existing branch as a task row.
	Adopt = "adopt"
	// Archive is `gopi task archive` — retire a row from the default views.
	Archive = "archive"
	// AuditMerges is `gopi task audit-merges` — the merge-trail audit report.
	AuditMerges = "audit-merges"
	// BranchRefs is the RETIRED `gopi task branch-refs` spelling (retired
	// 2026-10-30, task 01a18799). It is declared in RemovedVerbs: the
	// dispatcher keeps answering it with ONE sentence naming what it did and
	// where the reader goes instead, and nothing advertises it.
	//
	// WHAT IT WAS: the READ-ONLY peer-ref audit:
	// which TERMINAL ledger rows have a `deliverable.branch` the peer does not
	// advertise (`git ls-remote --heads`, the channel the delivery itself wrote
	// through). It is the answer to the state epic 01a12d5b could only make
	// VISIBLE for rows stranded from then on: every row stranded before that
	// fix carries no trail event, so this verb asks the peer directly instead
	// of asking the row's own history.
	//
	// A LEDGER verb: it reads the ledger and one peer and writes NOTHING — no
	// push, no delete, no status move, no event (the same claim `for-branch`
	// makes one question over). It names rows; it never repairs them.
	BranchRefs = "branch-refs"
	// DedupScan is `gopi task dedup-scan` — the twin-TASK-ROW audit (exact /
	// canonical-hash / trigram-similar pairs), report-only. It is a
	// task-LEDGER maintenance read, NOT a sync action: it was first spelled
	// `gopi memory memsync --dedup-scan`, which filed a read-only audit under a verb
	// whose whole job is transferring rows (epic cli-help-dedup re-homed it
	// here; the memsync spelling keeps dispatching and points here).
	DedupScan = "dedup-scan"
	// Diff is `gopi task diff` — compare two rows / a row against its branch.
	// It is a SYNC verb (cross-node reconciliation), advertised under the
	// sync surface, not in the ledger group: see SyncVerbs.
	Diff = "diff"
	// RetryDelivery is the RETIRED `gopi task retry-delivery` spelling (retired
	// 2026-10-30, task 01a18799). It is declared in RemovedVerbs: the
	// dispatcher keeps answering it with ONE sentence naming where the
	// capability went, and nothing advertises it.
	//
	// WHAT IT WAS: the LEDGER-KEYED repair path for a row whose delivery was
	// BLOCKED (the alignment / pre-merge gate refused it) and never revisited —
	// ask the peer (read-only `ls-remote`) whether it already holds the branch,
	// and when delivery is genuinely owed re-run the close-out chain and verify
	// the result from the peer.
	//
	// WHY IT IS RETIRED: it was a REPAIR for a delivery channel this repository
	// no longer runs (the worktree+branch hand-off). Its whole job was to ask a
	// PEER about a BRANCH; the surviving shape publishes a CLONE's `master` at
	// the run's own close-out, where there is no branch to hand over and no
	// blocked attempt waiting for a second visit. The resident pass keeps the
	// `delivery-blocked` scan (`finish-cleanup`'s retry leg) and the operator's
	// own door for a stranded terminal row is `task finish-cleanup --task <id>`
	// (dry run first, `--apply` to act).
	RetryDelivery = "retry-delivery"
	// Reconcile is the RETIRED `gopi task reconcile [--json] [--db PATH]`
	// spelling (retired 2026-10-30, task 01a18799). It is declared in
	// RemovedVerbs: the dispatcher keeps answering it with ONE sentence naming
	// where each of the pass's legs went, and nothing advertises it.
	//
	// WHAT IT WAS: the one-shot face of the ledger-reconciliation pass — the
	// stale-report leg, the terminal-row close-out leg, the
	// ownership-independent worktree reap, and the failed-needs-reissue census.
	//
	// WHY IT IS RETIRED: it was a SECOND face of duty that already has named
	// per-leg commands, and the second face carried the retired worktree
	// semantics of "one pass owns the whole ledger". Each leg is reachable by
	// the command that names it: the terminal-row close-out by
	// `task finish-cleanup`, the worktree reap by `task sweep-worktrees
	// --apply`, and the reissue census by `task list --status failed`. The
	// stale-report leg has NO successor and needs none: the sweep that produced
	// it was deleted 2026-11-05, and the reading it reported is taken by
	// `gopi task list` / `gopi task show` on demand, per row. Nothing writes to
	// the ledger from a verb whose only content is "run several other verbs".
	Reconcile     = "reconcile"
	FinishCleanup = "finish-cleanup"
	// ForBranch is the RETIRED `gopi task for-branch <branch>` spelling
	// (retired 2026-10-30, task 01a18799). It is declared in RemovedVerbs: the
	// dispatcher keeps answering it with ONE sentence naming where the
	// capability went, and nothing advertises it.
	//
	// WHAT IT WAS, AND WHERE THE CAPABILITY LIVES NOW: the READ-ONLY
	// branch→task lookup — which ledger row(s) own this branch, one row per
	// owner, each with its status / session / worktree / branch. Ledger verb:
	// it reads rows and writes NOTHING (no claim, no status move, no notify
	// event), which is why it sits in the ledger group beside `show` rather
	// than with the merger's git atoms. It replaces the task/xxx.md SIDECAR
	// the operator and the merger had to maintain by hand to link a branch
	// back to its row.
	//
	// The answer is PLURAL on purpose: a branch is commonly named by more than
	// one row over its life (the in-flight run plus the completed row the
	// completion-time backfill stamped), so the verb answers with every owner
	// and lets the reader pick. And an unanswerable query is NOT an empty
	// answer: an unknown branch exits TaskForBranchExitNoOwner (4, distinct
	// from the generic 1) with a named "no owner" line (Rule 15).
	ForBranch = "for-branch"
	// Harvest is `gopi task harvest` — pull a peer's ledger onto this node
	// (the PULL half of SyncML) and push our branches back. A SYNC verb: see
	// SyncVerbs.
	Harvest = "harvest"
	// Improvements is `gopi task improvements` — the PA self-improvement list.
	Improvements = "improvements"
	// List is `gopi task list` — the ledger listing (newest first).
	List = "list"
	// Orphans is the RETIRED `gopi task orphans` spelling (retired 2026-10-30,
	// task 01a18799). It is declared in RemovedVerbs: the dispatcher keeps
	// answering it with ONE sentence naming what it did and where the reader
	// goes instead, and nothing advertises it.
	//
	// WHAT IT WAS: the READ-ONLY orphaned-deliverable
	// readout: which TERMINAL rows' deliverable branches resolve to NO ref at
	// all (not on this host, not advertised by the delivery point, named by no
	// reachable commit), with the removal-audit arm that deleted each one and
	// the tip it recorded. Ledger verb: it reads rows and writes NOTHING — no
	// claim, no status move, no restore, no push.
	//
	// WHY IT IS A VERB AND NOT A FLAG ON branch-refs (measured 2026-10-16).
	// The reader behind it (`tools.TaskRefReachability`) shipped complete and
	// with ZERO production callers: its measurement existed, was documented, and
	// was reachable from nothing, so the question it answers was asked by no
	// surface at all. Its cousin branch-refs answers `unresolved` for both
	// "landed, ref consumed" and "ref deleted after a delivery that never
	// happened" — the two states this verb tells apart, and the reason the two
	// are separate verbs rather than one sentence in a parenthesis.
	Orphans = "orphans"
	// Stranded is the RETIRED `gopi task stranded` spelling (retired 2026-10-30,
	// task 01a18799). It is declared in RemovedVerbs: the dispatcher keeps
	// answering it with ONE sentence naming what it did and where the reader
	// goes instead, and nothing advertises it.
	//
	// WHAT IT WAS: the READ-ONLY contradiction readout —
	// the LIVE rows whose own record says the run is over (its recorded ground
	// directory is gone, or its newest run log carries a session_end) while the
	// ledger still calls the row pending/claimed/in_progress. Ledger verb: it
	// reads rows and writes NOTHING — no claim, no status move, no event, no
	// file. Its second face prints WHY a parked pending row is parked, from the
	// same gate the claim funnel uses.
	//
	// WHY IT IS A VERB (measured 2026-10-17, task 01a1492f). The two facts
	// existed on two different surfaces and were never joined: `task list`
	// showed the status, `task show` showed the recorded ground with an advisory
	// "NOT FOUND now — a stale reading", and neither related them. An operator
	// holding a ledger of parked chains had to do the join by hand, one row at a
	// time — and the parent gate's own reason line only ever appeared on a
	// `--status pending` view.
	Stranded = "stranded"
	// Merge is `gopi task merge` — merge an accepted branch.
	Merge = "merge"
	// MergeExecute is `gopi task merge-execute` — the merge executor step.
	MergeExecute = "merge-execute"
	// MergedCleanup is `gopi task merged-cleanup` — post-merge branch/worktree
	// cleanup.
	MergedCleanup = "merged-cleanup"
	// Merger is the family alias of the top-level `gopi merger` daemon.
	// DISPATCH-ONLY: it is a SERVICE spelling (ServiceAliases), never taught
	// in the ledger group — 'gopi merger -h' owns its contract. It keeps
	// dispatching because deployments and service units type it.
	Merger = "merger"
	// New is the REMOVED (M4.5) `gopi task new` spelling. It has no dispatch
	// case and is advertised nowhere: the dispatcher recognizes the word only
	// to print the pointed removal message instead of the generic usage dump.
	// It is deliberately NOT in FamilyVerbs and NOT in DispatchVerbs.
	New = "new"
	// Next is the REMOVED (M4.5) `gopi task next` claiming spelling — the
	// same treatment as New. The replacement entry point is `gopi task run
	// next`, which is why the run path also compares its positional against
	// this constant rather than a bare "next".
	Next = "next"
	// Push is `gopi task push` — publish a branch for cross-node pickup.
	// A SYNC verb (the git channel of the same fleet reconciliation): see
	// SyncVerbs.
	Push = "push"
	// Pull is `gopi task pull` — bring ONE ROW (or a comma list of rows) off
	// a peer's ledger into this one, and REPORT, per row, what this ledger did
	// with it (stored / already current / migrated / refused). It is the
	// operator-facing spelling of the row leg's PULL half; `Receive` is the
	// same leg (Rule 14: ONE implementation — pull dispatches to
	// SyncRowReceive for every id, so there is no second reading of the merge
	// authority and no second anchor decision). The two spellings exist for one
	// reason: `pull` is the word an operator reaches for when a row is waiting
	// on a LOADED peer and they want it HERE to run (the on-demand HP←peer1
	// fetch), and a row leg is idempotent (a second pull of a row this ledger
	// already holds answers `already current`, never a duplicate), so the
	// friendlier spelling costs nothing in safety. A SYNC verb: see SyncVerbs.
	Pull = "pull"
	// Receive is `gopi task receive` — pull ONE row off a peer's ledger into
	// this one. The PULL half of the row legs (see Send): it SELECTs the one
	// row by id and applies it through the same merge authority every sync
	// path uses, so "newer wins / never regress a terminal row / never
	// overwrite content" hold without being re-stated. A SYNC verb: see
	// SyncVerbs.
	Receive = "receive"
	// Clone is `gopi task clone <id>` — copy a row into a brand-new CLEAN
	// pending row (`<id>-c1`, its own session dir, its own ground).
	//
	// IT IS THE SECOND RUNG OF THE RESUME LADDER, above `resume`, and the ONLY
	// reason it exists is the one `resume` cannot serve: "the original
	// workspace is no longer trustworthy, give me a clean room". `resume`
	// re-enters the SAME recorded ground (same task id, same session, same
	// directory); `clone` mints a successor because the ROOM is suspect, so it
	// carries nothing run-allocated and takes no verdict as input.
	//
	// THE GATE IS THE WHOLE DESIGN, and it follows from that single reason:
	//
	//   - `in_progress` / `claimed` → REFUSED. A row that may be RUNNING has a
	//     ground somebody is standing in, and a second run in a second room is
	//     the exact double-execution the whole runner exists to prevent.
	//   - `pending` → REFUSED. Nothing has run, so nothing can be distrusted;
	//     the room is not suspect, it is UNUSED. The remedy is to run the row.
	//   - `failed` / `completed` / `verified` → ADMITTED. Its ground was used
	//     and its run is over, which is precisely the state "the room may now
	//     be dirty" describes.
	//   - `cancelled` → REFUSED by default, admitted only with the operator's
	//     explicit confirmation: a cancelled row was abandoned on purpose, and
	//     minting work from it silently would resurrect a decision somebody
	//     already made (Rule 15 — a refusal that explains itself beats a
	//     silent yes).
	//
	// WHY THERE ARE EXACTLY TWO RUNGS, AND WHY A THIRD ONE WAS DELETED. A
	// `failed` row has two honest ways forward: `resume` (same task id, same
	// session, same directory — continue the attempt) and `clone` (a brand-new
	// clean workspace, the successor its own row). The retired `reopen` minted
	// a successor row under a THIRD name for the SAME two situations — it
	// continued nothing (a fresh row, a fresh session) and cleaned nothing (a
	// new row, no new room) — so an operator had to choose between two verbs
	// that differed in no fact they could observe. One name for one act: the
	// work that must be continued is `resume`d, the work whose room is spent is
	// `clone`d. Deleted in full (engine, verb constant, CLI branch, help entry
	// and every string that pointed at it) rather than left as a deprecated
	// spelling — a spelling kept "for compatibility" is a second answer to a
	// question nobody asks twice.
	Clone = "clone"
	// Send is `gopi task send` — write ONE row into a peer's ledger. The PUSH
	// half of the row legs, and the honest replacement for the shape that
	// looked like it did this: `task sync --push-only --task-id T` rides the
	// CLAIM BROADCAST, whose peer handler mirrors the row's LOCK and answers a
	// verdict — measured 2026-09-21 (and again by the task that added this
	// verb): the peer answered `accepted (1 row(s))` and its own
	// `gopi task show T` denied all knowledge of the row. A SYNC verb: see
	// SyncVerbs.
	Send = "send"
	// Run is the RETIRED (2026-09-16, epic worker-run-entry) `gopi task run`
	// sub-verb. Running a task row is `gopi worker`'s job and ONLY its job:
	// the row is selected implicitly (no argument: claim the next eligible row)
	// or explicitly (`gopi worker <task-id>`, positional), and both faces go through
	// the same round body, so there is one runner, one help page, one exit
	// contract. The spelling keeps a dispatch case — a script that types it
	// must hear WHERE the function went (a pointed removal message) instead
	// of a generic usage dump — but it is NOT advertised: it lives in
	// RemovedVerbs, never in FamilyVerbs.
	Run = "run"
	// Search is `gopi task search` — full-text search over the ledger.
	Search = "search"
	// Serve is the DEPRECATED alias of `gopi task merger` (born `gopi task
	// serve`, renamed by the serve/worker split 8ef21fda). It KEEPS a dispatch
	// case for existing deployments and scripts, so it is a dispatcher verb —
	// but it is deliberately absent from FamilyVerbs: the advertised surface
	// follows help (`merger` is canonical), and the top-level usage test
	// asserts the enumeration never carries `serve`.
	// Serve is the pre-split spelling of the merger, now an alias of
	// Merger (the epic cli-help-dedup gave it ONE pointer line: the `-h`
	// route below must name THIS constant, not re-type it — Rule 1/13).
	Serve = "serve"
	// Show is `gopi task show` — one row's full record.
	Show = "show"
	// Status is `gopi task status` — is the row's holder process ACTUALLY
	// running? (procfs verdict; the zombie question — added by d7474fb4).
	Status = "status"
	// Submit is `gopi task submit` — enqueue a new task.
	Submit = "submit"
	// SweepWorktrees is `gopi task sweep-worktrees [--dry-run|--apply]
	// [--backfill-anchors] [--min-age-h N] [--max N] [--json]` — reclaim stale
	// git worktree REGISTRATIONS (inside a declared root, unheld, unowned,
	// clean, older than the threshold). Ranked guard 5 of the /dev/shm
	// assessment: the registrations a finished task leaves behind are invisible
	// to every other GC (a worktree path is not a task-scratch name), which is
	// how 19 linked worktrees came to sit in a tmpfs nobody chose.
	//
	// `--backfill-anchors` runs the run-ground anchor axis' BACKFILL leg
	// (internal/tools/run_ground_adopt.go) under the SAME plan/apply gate: it
	// adopts the CLONE grounds no producer ever anchored — the population the
	// forward-only producer (commit 3feaf4a) could never record — by writing a
	// measurement, never a taker. It removes nothing; the grounds it adopts
	// become nameable by the collect leg that already runs here.
	SweepWorktrees = "sweep-worktrees"
	// SweepZombies is `gopi task sweep-zombies` — RETIRED 2026-10-26
	// (devdoc/zombie-sweep-removed.md). The spelling is kept so the dispatcher
	// can answer it with a sentence instead of the generic usage dump; it is
	// declared in RemovedVerbs, so nothing advertises it.
	SweepZombies = "sweep-zombies"
	// SyncPass is `gopi task sync-pass --once` — REMOVED 2026-11-05 with the
	// cross-node sync family (measured: the verb exits 1 with "removed — the
	// cross-node sync surface is DELETED"). It USED to be the ONE-SHOT face of
	// the cross-node ledger passes (the git/ref pass, the adoption sweep, the
	// delta tick) the retired resident merger ran per tick
	// (devdoc/merger-retired.md), and it existed because those passes were
	// reachable ONLY from `gopi task serve`, which the retirement turned into a
	// hard refusal. The spelling is KEPT here — like SweepZombies above — so a
	// script or shell history that types it hears the removal sentence instead
	// of the generic usage dump; it is declared in RemovedVerbs, so nothing
	// advertises it.
	SyncPass = "sync-pass"
	// Sync is `gopi task sync` — cross-node ledger reconciliation
	// (bidirectional delta sessions, either side of the ssh protocol, and the
	// reconciliation self-check). A SYNC verb: advertised under the sync
	// surface, never in the ledger group — see SyncVerbs.
	Sync = "sync"
	// Update is `gopi task update` — change a row's status/summary.
	Update = "update"
	// Verify is the RETIRED `gopi task verify <task_id>` spelling (retired
	// 2026-10-30, task 01a18799). It is declared in RemovedVerbs: the
	// dispatcher keeps answering it with ONE sentence naming where the
	// capability went, and nothing advertises it.
	//
	// WHAT IT WAS: the independent-verification pass (`internal/tools`.
	// TaskVerify) — a fresh child gopi that located the delivered fix, built
	// and tested it, and wrote a verdict back onto the row, with its own
	// exit contract (0 judged / 4 the cap expired / 5 no verdict) and a
	// `GOPI_TASK_VERIFY_TIMEOUT_SEC` cap.
	//
	// WHY IT IS RETIRED: a program may not manufacture an authority over
	// itself (the operator's ruling of 2026-10-25). Verification belongs to a
	// TEST PROGRAM — a test run, a measured command — so the verdict is written
	// by the run that measured it, through `gopi task update <id> --status
	// verified --result "evidence: <command> + its output tail"`, which still
	// refuses an empty evidence field. A second spawn that judged the first
	// spawn's work was the walk-the-worktree machinery this epic removes.
	Verify = "verify"
)

// MaxTaskVerbs is the CEILING on the ADVERTISED `gopi task` verb surface —
// the one gate on this vocabulary. It is not a target and not a count: the
// surface may shrink freely beneath it, and
// TestTaskVerbCeiling is what refuses growth past it.
//
// IT WAS SET FROM A REAL MEASUREMENT, at
// sha 3d355184b4713efe35beb1256824840bdee9b0fd (the peer's master tip this
// branch is cut from, measured by `go test ./internal/taskverbs/ -run
// TestZZProbe` — a throwaway probe that printed the three numbers — and
// cross-read against `gopi task -h` from a real ./bin/gopi build at the same
// tree):
//
//	len(FamilyVerbs) = 17
//	len(SyncVerbs)   = 8
//	sum              = 25
//
// the two retirements that landed between this branch's cut and its delivery
// (master's `reopen` deletion, 545b5b2, and this branch's four read-only
// audits) leave the surface SHORTER than either side measured alone, so the
// ceiling is re-measured HERE rather than carried from either. It is not a
// licence to grow: whoever lands a further deletion LOWERS it in the same
// commit, to that tree's own sum + the same 2 headroom and with its own
// measurement — a stale-high ceiling is exactly the drift
// this gate exists to catch.
//
// LOWERED 28 → 25 (2026-10-30, task 01a18799, on the REBASED tip), in the
// commit that retired `for-branch`, `branch-refs`, `orphans` and `stranded`
// from the advertised surface (they moved to RemovedVerbs) AND rebased onto
// master's own `reopen` deletion (545b5b2). Two independent retirements landed,
// so neither side's count is the tree's: master carries FamilyVerbs = 19 (its
// own `reopen` gone, the four audits still advertised) and this branch carried
// FamilyVerbs = 16 before the rebase. Measured HERE, both lists counted element
// by element — not carried from either side:
//
//	len(FamilyVerbs) = 15
//	len(SyncVerbs)   = 8
//	sum              = 23
//
// and 23 + the same 2 headroom = 25. The retirements are advertise-and-dispatch
// removals, never a ceiling adjustment made to get a red test green — the
// surface SHRANK, so the ceiling had to follow it down.
//
// LOWERED 25 → 22 (2026-10-30, task 01a18799, slice 2/4), in the commit that
// retired `verify`, `retry-delivery` and `reconcile` from the advertised surface
// (they moved to RemovedVerbs). Measured HERE, element by element:
//
//	len(FamilyVerbs) = 12
//	len(SyncVerbs)   = 8
//	sum              = 20
//
// and 20 + the same 2 headroom = 22.
//
// RAISING IT IS A DELIBERATE ACT: it may only ever be raised in a commit that
// states WHY the advertised surface grew and what the new verbs do — the
// ceiling is a decision, never an adjustment made to get a red test green.
// Check: TestTaskVerbCeiling (count <= ceiling, and every advertised verb
// reachable from the help page).
const MaxTaskVerbs = 22

// FamilyVerbs is the ADVERTISED family surface, in the order the help renders
// it (alphabetical): every verb the `gopi task <TAB>` completion offers, every
// verb the top-level `gopi -h` task row enumerates, and every verb `gopi task
// -h` documents a usage block for.
//
// It is the LEDGER surface — the verbs an operator uses to read or move a row
// in the task ledger. It deliberately does NOT carry, and must never
// re-advertise:
//
//   - SyncVerbs — cross-node reconciliation. Those are dispatchable family
//     spellings with their own advertised home (see SyncVerbs and
//     cmd/agent/cli_tasks_sync.go's syncUsageHead); teaching them in the
//     ledger group is the drift TestNoServiceOrSyncVerbInLedgerGroup exists
//     to catch.
//   - the SERVICE spellings (Merger/Serve — ServiceAliases) and the
//     git-infrastructure atoms (MergerVerbs): a daemon and a merge atom are not
//     ledger verbs. The services are advertised by their own top-level pages
//     (`gopi merger -h` / `gopi worker -h`), the git atoms by the merger's
//     page; the ledger group documents neither, it POINTS at them. The
//     anti-readd gate keys on a re-taught SYNOPSIS line ("  gopi task merger
//     …"), not on a mention: a sentence saying "see 'gopi merger -h'" is the
//     pointer working, and is required to stay.
//   - RemovedVerbs — the RETIRED spellings (`run`, `sweep-zombies`, and the
//     four read-only audits retired by task 01a18799: `for-branch`,
//     `branch-refs`, `orphans`, `stranded`). The group has no home here
//     either: each retired spelling is answered by ONE sentence naming where
//     its function went, and the ledger group neither teaches the spelling nor
//     carries a second page for it.
//
// READ-ONLY: consumers derive presentation from it and must never mutate it
// (the slice is shared, not copied — a copy would be the second definition
// this package exists to prevent).
var FamilyVerbs = []string{
	// The LEDGER verbs. DedupScan (merge resolution 2026-09-15) is the ONE verb
	// master contributed that has no other group to live in: it audits task
	// ROWS (twin-row report), so it reads the ledger — a ledger verb, its home
	// is the family page. Every other verb master's side listed is NOT ledger:
	// Adopt / AuditMerges / Merge / MergeExecute / MergedCleanup / Merger are
	// MergerVerbs (`gopi merger -h`) and Diff / Harvest / Push / Sync are
	// SyncVerbs (`gopi task sync -h`) — advertising them here is the exact
	// double-home this restructure removes (TestNoServiceOrSyncVerbInLedgerGroup).
	Archive, Clone, DedupScan, Improvements, List,
	Search, Show, Status, Submit, SweepWorktrees,
	Update,
}

// RemovedVerbs is the RETIRED family surface: spellings that keep a DISPATCH
// case (so a script or a deployment that types one hears where the function
// went) but are advertised nowhere — not in the family help, not in the
// top-level index, not in `gopi task <TAB>`.
//
// WHY IT IS A GROUP AND NOT A HAND-TYPED `case`: the dispatcher's verb set is
// DERIVED (DispatchVerbs == the union of these groups), and the completion
// test asserts that union equals the dispatcher's case labels. A removed
// spelling that is still dispatchable but declared in no group would make the
// derivation a lie; a spelling that was simply deleted from every group would
// silently lose its pointed removal message and fall back to the generic
// "unknown subcommand" dump (Rule 15: a removal must explain itself).
//
// TestRemovedVerbsAreNotAdvertised pins the split: a member of this list must
// appear in no advertised group, and every retired spelling in the dispatcher
// belongs here.
//
// A REMOVED VERB KEEPS ITS CONSTANT AND ITS DISPATCH CASE, and that is the
// whole point of the group: deleting the spelling outright would make an old
// script — or a command recorded in a task row, a briefing or a shell history —
// fall back to the generic "unknown subcommand" dump, which sends its reader
// hunting a typo instead of reading the replacement path (Rule 15: a removal
// must explain itself). What is deleted is the IMPLEMENTATION and every
// advertisement; what survives is one dispatch case printing one sentence.
//
// THE FOUR READ-ONLY AUDITS (2026-10-30, task 01a18799) joined this list
// together because they share one reason: they were facets of the retired
// worktree+branch delivery semantics — asking the ledger which branch a row
// owns, and which refs of that shape still exist — while the surviving surface
// answers the same questions from the ROW (`task show --branch/-h`), from the
// ledger (`task list`), and from the machinery that actually owns refs
// (`gopi merge`, `gopi task finish-cleanup`). Two of the four made peer/ref
// claims about a delivery channel this repository no longer runs, which is the
// defect that made removing them a repair rather than a tidy-up.
//
// THE THREE IMPLEMENTATION-SHAPED VERBS (2026-10-30, task 01a18799, slice 2/4:
// `verify`, `retry-delivery`, `reconcile`) joined it for the same underlying
// reason, and each carries its own replacement path in its constant's comment:
// they were the last three verbs whose BODY was the retired machinery rather
// than a ledger operation — a child spawned to judge a run, a repair pass for a
// blocked branch hand-off, and a verb whose only content was "run these legs".
// What is left is the simple shape: success / failed / in_progress, `resume`,
// and `clone`.
var RemovedVerbs = []string{
	BranchRefs, FinishCleanup, ForBranch, Orphans, Reconcile, RetryDelivery, Run,
	Stranded, SweepZombies, Verify,
	// THE CROSS-NODE SYNC FAMILY (2026-11-05). Eight spellings, one reason:
	// every one of them reached ANOTHER node's ledger, and this binary no longer
	// does that. They are here rather than deleted from the vocabulary because a
	// script, a task row or a shell history that types one must hear a removal
	// sentence, not the generic "unknown subcommand" dump. See SyncVerbs (now
	// empty) for what replaced the capability.
	Diff, Harvest, Pull, Push, Receive, Send, Sync, SyncPass,
}

// MergerVerbs is the GIT-INFRASTRUCTURE surface: the family spellings that are
// ATOMS OF THE MERGER'S PASS rather than ledger operations. Each one is a
// narrow, once-only face of something the merger does every tick
// (`gopi merger`, cmd/agent/cli_merger*.go) — adoption discovery, the guarded
// merge, the merge-backlog executor, merged-branch cleanup, the backlog audit.
//
// WHY THEY ARE NOT FamilyVerbs: a ledger verb reads or moves a row; these move
// GIT (branches, worktrees, merges) and only write the ledger as a side effect
// (a conflict row, a reaped branch). Their advertised home is `gopi merger -h`
// — the daemon whose pass they are the CLI face of — and the family dispatcher
// points there instead of re-teaching them (test:
// TestNoServiceOrSyncVerbInLedgerGroup). They keep dispatching at the same
// spellings, with no deprecation notice: the SPELLING is not deprecated, only
// its documentation home moved.
// It carries the SERVICE spelling `merger` too: the daemon IS the thing these
// atoms are a pass of, and `gopi task merger` is the deprecated spelling of
// that same daemon — one page (`gopi merger -h`) documents both.
var MergerVerbs = []string{Adopt, AuditMerges, Merge, MergeExecute, MergedCleanup, Merger}

// SyncVerbs is the RETIRED CROSS-NODE SYNC surface, and it is EMPTY on purpose.
//
// WHAT IT WAS: the family spellings that spoke to a PEER — an ssh alias in
// settings.json "sync_peers" — rather than to this node's ledger: `diff`,
// `harvest`, `pull`, `push`, `receive`, `send`, `sync` and `sync-pass`. Their
// flags (--peer/--peers/--status/--serve-remote/--claim-remote/--push-only/
// --full) described a PROTOCOL, so they were advertised at exactly one page
// (syncUsageHead, printed by `gopi task sync -h`) and never in the ledger group.
//
// WHY IT IS EMPTY AND NOT DELETED (2026-11-05): the list is a VOCABULARY slot —
// DispatchVerbs is `FamilyVerbs ∪ SyncVerbs ∪ MergerVerbs ∪ ServiceAliases ∪
// RemovedVerbs`, so emptying it is how the family says "these spellings are no
// longer advertised anywhere". Deleting the identifier would break that
// derivation and every reader of it (the ceiling gate, IndexRowVerbs, the
// config enum gate) for a vocabulary that still exists.
//
// WHERE THE EIGHT SPELLINGS WENT: `RemovedVerbs` — the eight keep a dispatch
// case that prints ONE removal sentence and exits non-zero, because a script, a
// task row, a briefing or a shell history that types one must hear "this surface
// is gone and here is what replaced it", never the generic usage dump (Rule 15:
// a removal must explain itself). Their implementation — the peer sessions, the
// row legs, the git pass and its clock — is DELETED: no verb in this binary
// opens a session against another node's ledger. Work crosses hosts through the
// GIT ref a row's deliverable publishes to (the clone channel's delivery leg),
// and a row that must be worked on another host is FILED there with
// `gopi task submit` (Rule 28 / Rule 40).
//
// A new spelling must NOT be added here to "re-grow" the family: an advertised
// verb must be documented on the family page (TestTaskVerbCeiling), and the
// page no longer documents any of these.
var SyncVerbs = []string{}

// ServiceAliases is the SERVICE spelling set: the two family spellings that
// name a resident SERVICE rather than a ledger operation.
//
//   - Serve is the older alias of the same daemon, kept for scripts.
//
// NEITHER is advertised by the ledger group: `gopi task -h` carries no
// synopsis line for a service (test: TestNoServiceOrSyncVerbInLedgerGroup),
// and its contract lives on `gopi merger -h`. Both stay DISPATCHABLE
// (DispatchVerbs is the union of the groups), so scripts, deployments and
// service units written against `gopi task merger …` / `gopi task serve …`
// keep working — the dispatcher even keeps printing the deprecation pointer
// on stderr so the operator learns the new home from the run they made.
//
// (Merger itself sits in MergerVerbs: it is the daemon those git atoms are a
// pass of, so `gopi merger -h` is its one home. This list is therefore the
// spellings with no other group to belong to.)
var ServiceAliases = []string{Serve}

// DispatchVerbs is the DISPATCH surface:
//
//	DispatchVerbs == FamilyVerbs ∪ MergerVerbs ∪ SyncVerbs ∪ ServiceAliases ∪ RemovedVerbs
//
// It exists so the dispatch-side guard is exhaustive by construction:
// cli_completion_test.go's TestTaskCompletionNamesMatchDispatch asserts
// exactly `DispatchVerbs == the case labels of runTasksSubcommand`.
// A verb added to any group with no dispatch case fails that test, and a
// dispatch case whose spelling is in no group fails it too — so the surfaces
// cannot drift apart (Rule 13/14).
//
// The UNION is why the groups can be carved without touching dispatch:
// moving a spelling between groups changes only HOW it is advertised, never
// WHETHER it dispatches. `Serve`'s dispatch case shares a label site with
// `Merger` (MergerVerbs) and `Run`'s is the retired one — another reason the
// groups are derived rather than supplemented by a hand-typed tail.
//
// TestDispatchVerbsAreDerived pins this derivation and the disjointness of
// the groups; TestNoBareTaskVerbLiteralInDispatcher is the litmus test: it
// treats a bare literal in the dispatcher as a DEFAULT (a verb with no
// constant), so the derivation above cannot quietly become a stale second
// definition.
var DispatchVerbs = func() []string {
	out := make([]string, 0,
		len(FamilyVerbs)+len(SyncVerbs)+len(MergerVerbs)+len(ServiceAliases)+len(RemovedVerbs))
	out = append(out, FamilyVerbs...)
	out = append(out, SyncVerbs...)
	out = append(out, MergerVerbs...)
	out = append(out, ServiceAliases...)
	out = append(out, RemovedVerbs...)
	return out
}()

// IndexRowVerbs is the TOP-LEVEL INDEX surface: the family spellings the
// `gopi -h` task row enumerates, as a SORTED, DISTINCT set.
//
//	IndexRowVerbs == sort(FamilyVerbs ∪ SyncVerbs)
//
// WHY IT IS NOT FamilyVerbs, and why the cross-node legs belong on the front
// door (task 01a11f70…, 2026-10-10). The row used to enumerate FamilyVerbs
// ALONE, so a reader who did not already know that `gopi task send` exists could
// not learn it from the page he opens first: `send` / `receive` / `pull` move a
// ROW to another machine, which is a thing the ledger does, and they are
// dispatchable family spellings — the row's own head is `gopi task
// <subcommand>`, so a member missing from its enumeration is not "out of
// scope", it is an omission. The row therefore names the row-moving legs too,
// with `peer` (the word the reader's intent actually contains) in its summary.
//
// IT IS THE ENUMERATION OF A LINE, NOT A SECOND HOME — the two constraints the
// 2026-09-15 restructure put around this surface, both still standing:
//
//   - FamilyVerbs stays the COMPLETION/TAB surface, which is what
//     TestTopLevelUsageTaskVerbEnumMatchesCompletion pins
//     (taskCompletionNames == taskverbs.FamilyVerbs). A sync verb offered by
//     tab-completion would advertise a group whose home is `gopi task sync -h`,
//     so the two lists are deliberately not equal.
//   - Group membership is unchanged: FamilyVerbs still holds no sync verb, and
//     this list holds no service spelling, no git-infrastructure atom and no
//     retired spelling. TestNoServiceOrSyncVerbInLedgerGroup still governs the
//     family PAGE (no re-taught synopsis, no restated sync flag semantics).
//     Widening the row's enumeration is not re-teaching: the row carries NAMES,
//     one line, no syntax and no flags — the detail stays where it was.
//
// A verb MOVES between FamilyVerbs and SyncVerbs without touching this list
// (it is the union), so the only way into it is into one of the two groups.
//
// READ-ONLY: consumers derive presentation from it and must never mutate it.
var IndexRowVerbs = func() []string {
	out := make([]string, 0, len(FamilyVerbs)+len(SyncVerbs))
	out = append(out, FamilyVerbs...)
	out = append(out, SyncVerbs...)
	sort.Strings(out)
	return out
}()
