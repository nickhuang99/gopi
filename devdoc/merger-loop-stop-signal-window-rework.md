<!-- doc-class: historical -->
# Rework: relanding `fix/merger-loop-exit-bounded` onto post-retirement master

Date: 2026-09-19. Task: merge take-over rework item (branch
`fix/merger-loop-exit-bounded`, tip `d5cbb76c`, 5 commits / 9 files; archived
series `~/workspace/merge-run-diffs/fix_merger-loop-exit-bounded.patch`).
Result branch: `fix/merger-loop-exit-bounded-rework`, rebased on master
`a9a96cd7` (hash-agnostic: see "Recovery" — the branch is the deliverable).

## The conflict, and why it was not a text conflict

The take-over named one conflicted file: `cmd/agent/merger_restart_e2e_test.go`.
It is not a two-sides-edited-one-hunk conflict. Master's `f9ff1a89`
("feat(merger): retire the resident merger daemon by entry-point closure",
2026-09-18 21:07) **deleted 888 lines of that file** — one of 12
`cmd/agent/merger*_test.go` files and 4405 lines removed in that single commit
(13 files changed, all deletions) — as part of the retirement. The branch (cut
from `9ce8e159`, before the retirement) edits the same file. A file the other
side removed cannot be text-merged: hence `git apply -3` reports
`does not exist in index` rather than leaving `<<<<<<<` markers.

The retirement is real and still in force on master: `retireMergerFace` sits
FIRST in `isMergerSubcommand` (main.go) and in the `taskverbs.Serve` /
`taskverbs.Merger` arm (cli_tasks.go). The whole dispatch is kept verbatim as
`runMergerVerb`, unreachable in production, for a re-development.

So the REWORK DECISION is per-file, not per-hunk:

| branch file | disposition | reason |
|---|---|---|
| `cmd/agent/merger_loop.go` | **RELANDED** | production fix: `mergerArmStopSignal()` is the one place the stop channel is created and `signal.Notify`'d, and `mergerResidentLoop` now RECEIVES the channel instead of installing it. Applies cleanly to master. |
| `cmd/agent/merger_tick.go` | **RELANDED** | production fix: the arm runs BEFORE `acquireServeLock()` (resident form only, released by `defer`). Applies cleanly. |
| `cmd/agent/merger_restart_e2e_test.go` | **SUPERSEDED → RE-DERIVED** | the file does not exist on master (retired). Its behavioural guard `TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid` spawns `gopi merger --every D`, which now exits 1 with a retirement sentence and never takes the lock — it cannot pass on this master for any implementation. The INVARIANT is re-derived as a source-level gate (below). |
| `devdoc/merger-loop-stop-signal-window.md` | **RELANDED** (as-is) | the root cause, the 0.095-0.117s window measurement, the 5-why, the rig table, the honest Not Done. Its `Branch:` header is the ORIGINAL 2026-09-18 provenance line and is kept — it is a dated record, not a claim about this branch. |
| 5 `scripts/merger-loop-*.sh` rig scripts | **RELANDED** (as-is) | observation-only rigs. Audited for Rule 27: no `dd`, no `oflag=direct`, no device writer; both `while :; do` loops are poll/sample loops over `kill -0` + `/proc` + a log's mtime, bounded by a wall-clock budget. Their subject **still exists** — `runMergerVerb` and `mergerResidentLoop` are in the tree — so "revive the entry point, re-run the rig" is a real path for the re-development. |

Nothing is silently dropped: every branch file is either applied or accounted
for above.

## Why the behavioural guard could not be re-derived, and what replaced it

`TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid` is a **black-box
behavioural** guard: spawn the loop, wait for the pid STAMP, `syscall.Kill`,
require the process to die inside the bound AND to have printed its stop line.
Every one of those steps needs a signallable loop, and master has none: both
entry points call `retireMergerFace` before any dispatch.

Re-deriving it as-is would have produced a test that can only ever fail (or a
`t.Skip` — a deleted test wearing a passing green). What replaced it is
`cmd/agent/merger_stop_arm_order_gate_test.go`, two source-level cases:

1. `TestMergerStopSignalIsArmedBeforeTheLoopIsSignallable` — in
   `runTaskMerger`'s body, `mergerArmStopSignal()` must appear BEFORE
   `acquireServeLock()`, the arm must be gated on the resident form
   (`if interval > 0 {`), and the release must be deferred.
2. `TestTheStopInstallLivesInExactlyOnePlace` — `signal.Notify(` belongs to
   `mergerArmStopSignal` alone; `mergerResidentLoop` must NOT install its own
   handler (an install there runs after the lock — the measured window).

This is not a weaker proxy for the behavioural case: the fix IS an ordering, and
source-level is the only level at which the ordering is still observable on this
master. The gate follows the established pattern of
`cmd/agent/cli_tasks_rebuild_gate_test.go` (source read + the shared
`readDeclBody` / `braceFuncBody` helper), and it is a red-proof by REMOVAL, not
an assertion about the past.

**Red-proof, observed (2026-09-19):** the arm block was moved back below
`acquireServeLock()` in `merger_tick.go` (i.e. the pre-fix order restored); the
case failed naming both offsets —
`"mergerArmStopSignal()" at body offset 1255, "acquireServeLock()" at 987` —
and passed again once the order was restored (step 1 green, step 3 `FAIL …
RED_EXIT=1`, step 5 green again, in one run). The transition is recorded in the
task report.

## What a re-development should take from this

- The fix is already in the tree and is preserved by the retirement ("THE
  IMPLEMENTATION IS KEPT, THE ENTRY POINTS ARE CLOSED", merger_retired.go). When
  the merger is revived, the arm order is already correct; the source-level gate
  is what stops a later refactor from re-inlining the install at the loop's head
  and re-opening the window.
- The behavioural guard is the thing to restore WITH the entry point (it needs
  `mre*` fixture helpers, which went with `merger_restart_e2e_test.go`).
- The window's second half — `internal/signalrec`'s
  `modesThatOwnTheirSignalHandlers` policy, which turns a consumed signal into a
  lost one for any mode that owns its handlers but arms them late — remains
  unaddressed. It is enumerated in the relanded devdoc's `## Not Done
  (honest)`; the fix removes the merger's exposure, not the shape.

## Recovery

Branch `fix/merger-loop-exit-bounded-rework` (this commit is its HEAD;
`git log --oneline -3`). The commit lives in the shared object store of the main
repo, so no push is needed:

```bash
git show fix/merger-loop-exit-bounded-rework:devdoc/merger-loop-stop-signal-window-rework.md
```

The archived take-over series is kept at
`~/workspace/merge-run-diffs/fix_merger-loop-exit-bounded.patch` (no archive tag
was ever created for the branch, and the branch ref itself is gone).
