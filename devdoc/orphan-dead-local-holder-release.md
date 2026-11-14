<!-- doc-class: historical -->
# Devdoc: the orphaned live-status row — releasing a directory its dead holder no longer needs

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Epic: task `01a095b5-5004-74ec-961e-83d4e6b2447d` (2026-09-12).
Branch `fix/dead-local-holder-release`. Report: `tasks/01a095b5-…-report.md`.

## The shape that had no disposition

Three arms in `internal/tools` + `cmd/agent` all decided a live-status row
"holds" its directory, and none of them asked whether the holder still existed:

* `taskLiveHolderStatus` (task_finish_cleanup.go) — `pending/claimed/in_progress`
  is a holder *by design*, documented as such;
* `reclaimDeadHolderRowsFrom` (task_reclaim.go) — `status IN (completed, failed,
  cancelled, verified)`; a live-status row was never even a candidate;
* `serveFinishCleanupWorktreeQuery` (cli_tasks.go) — the same three terminal
  statuses, so the stranded scan never selected the row either.

The join of the three: a row whose holder was killed (`SIGKILL` → the wrapper is
never reaped → the `in_progress` row is never closed out) vetoed the cleanup of
**its own** directory and was invisible to the reclaim arm — forever, until a
human ran `gopi task sweep-zombies --kill`. The cost was the parked worktree and
`scripts/safe-merge.sh` gate [c] refusing that branch: the deliverable could not
reach the merger.

## The decision (and what was deliberately NOT used)

**Evidence = the pid layer alone.** `claimHolder(claimedBy)` yields an `ok`
verdict (blank / malformed / foreign stamp ⇒ `ok=false`) and a local pid;
`pidAlive` already treats `Z`-state as dead, which is the killed-wrapper case.
That evidence has **zero false positives** on the question "is a runner working
here?". So: `live status ∧ ok ∧ pid dead ⇒ the status overstates the holder`.

**The heartbeat chain was deliberately not consulted** — no `zombieVerdict`, no
`claimStampFreshness`. A stale claim stamp proves neither life nor death (that
is its contract), and freeing a directory on "probably stale" is exactly the
false positive the conservative direction exists to prevent. Keep it that way:
if a future change wants the stamp back in, it needs a proof of death, not a
proof of silence.

**A foreign host is unknowable, never dead.** `ok=true` is required, so a stamp
another host wrote keeps its veto on a live status. This is what makes the change
safe on a fleet where a pid means nothing outside its own host — and it is
pinned by `TestForeignHostHolderIsNeverCalledDead` (extended with the
live-status case) and by the live-twin acceptance test.

**The ROW stays open.** The release is about the directory, and the ledger still
says `in_progress` afterwards. Auto-closing would need the heartbeat chain this
design just rejected, and it would duplicate `sweep-zombies`. So the release is
loud instead: a notify line, a disposition suffix on the reclaim line, and an
exported census (`FinishCleanupDeadLocalHolderCount`) re-reported every pass
while non-zero.

## Why ONE predicate was not enough (the easy thing to miss)

Fixing only the *holder gate* would have changed nothing on a real host: the
ledger's scan window is what decides whether a row is ever *looked at*, and
every scan was keyed on TERMINAL statuses. So the release needed a second edit —
but the load-bearing one is `reclaimDeadLocalHolderRowsFrom` (the live-status
scan **inside** `ReclaimDeadHolderWorktrees`, behind its own disposition guards
and the `GOPI_RECLAIM_DEAD_HOLDER` kill switch), NOT a widening of the
`cmd/agent` stranded scan. The mutant run shows the shape: with the pre-fix
predicate, the pass printed **no lines at all** for the orphan's repo and the
directory stayed put.

**What I got wrong, and what caught it.** A first draft ALSO widened
`serveFinishCleanupWorktreeQuery` (cli_tasks.go) to the live statuses. That
looked load-bearing and was not: it broke `TestStrandedScanIgnoresLiveRows`,
the same-day invariant that the stranded scan must not even *propose* a
live-status row, and it put the release on a path outside the reclaim arm's kill
switch. The full `./cmd/agent/` suite failed on exactly that test — reasoning
did not catch it, the suite did (and a master baseline run proved the test green
without my change, so the failure was mine and not pre-existing). Reverted; the
release now has exactly ONE owner.

## Pitfalls hit while building this

* **A verification world must be non-claimable, or it measures the runner.**
  The first real-binary E2E "passed" for BOTH the fixed and the mutant binary —
  because `gopi worker --once` claimed my seeded `in_progress` row (its repo was
  reachable), ran it, and closed it out `failed`; the ordinary terminal path
  freed the worktree either way. The tell: the row's holder stamp became the
  worker's own pid. Fix: settings declares a DIFFERENT `repos` path with no
  peers, so nothing but the code under test can touch the row.
* **A "refusal gate" and a "release" cannot share one return value.**
  `finishCleanupOwnerRowGate` returning non-empty means *refuse*; my first draft
  returned the release reason from it, which would have refused exactly the
  directory the epic exists to free. Resolution: the release is not a gate
  verdict at all — the predicate simply stops calling the row a holder, and the
  *reporting* happens in `finishCleanupDeadLocalHolderReason` (owner-gate
  wording) and in the reclaim line. If you add an arm here, ask first whether you
  are deciding or reporting.
* **`~/.gopi/memory.db` is shared and alive.** Other sessions closed rows while
  this one ran (`01a09573` went `failed` at 20:57 and its directory vanished), so
  any "current population" number is a snapshot with a short half-life. Use a
  sandbox ledger for anything that mutates.
* **The fleet guard bites the harness, not the code.** `gopi worker --once`
  refuses to start inside an agent session (`GOPI_ALLOW_AGENT_SPAWN=1` is the
  one-off override; it also prints the operator ruling against it). For a
  verification pass that only needs the lifecycle leg, that override is the
  surgical way in — a test rather than an agent would use `serveFinishCleanupSweep`
  directly (`cmd/agent/cli_tasks_dead_holder_accept_test.go`).
* **A `sleep` used as the "live holder" must not be a child of the tool call.**
  A backgrounded child keeps the call's stdout open, so the call times out even
  though the command finished. `setsid nohup sleep N &` detaches properly — and
  it must be killed afterwards, or it pollutes the next run's census.
* **`sqlite3 <schema> | sqlite3 new.db` fails on `sqlite_sequence`** ("object
  name reserved for internal use"). Filter it out (and `sqlite_stat*`) when
  cloning a schema for a fixture.
* **`git worktree add` after a `git worktree remove` leaves no trace, but
  re-adding the same path needs the path gone first** — resets for an A/B run are
  cheapest by re-creating the whole sandbox.

## Verification that actually proved it

* Pre-fix mutation of `finishCleanupDeadLocalHolder` ⇒ the new tools tests and
  the cmd acceptance test fail; reverted, green (`go test` outputs in the report).
* Real binary, real `scripts/safe-merge.sh`, sandboxed ledger: gate [c] **exit 1
  → exit 0** after ONE lifecycle pass, branch tip byte-identical, deliverable
  file still on the branch; the same world under the **mutant** binary stays
  exit 1 with the directory intact.
* Live twin with a REAL live process as holder: directory survives the pass,
  gate [c] keeps refusing.
* `go test ./internal/standards/` green (Rules 16–21 untouched: no `%v`-wrap,
  no `os.Setenv`, no `else`-after-return, no new panic, no accessor prefix, no
  reader-less env constant — the new constants are strings, not env names).

## Follow-ups (not done here, deliberately)

1. `01a09573`-class rows: nothing closes the ROW. If the operator wants the
   ledger cleaned automatically, that belongs to `sweep-zombies`, whose evidence
   chain (heartbeat + session mtime) is the right tool for "abandoned", as
   opposed to this epic's "provably dead".
2. Pre-existing failure reported by the voice review branch
   (`internal/testutil`) is unrelated to this change — it fails on master too.
