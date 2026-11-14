<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: run-singleton guard — a task row cannot be executed twice

**Epic**: task-run-singleton-guard (2026-09-15) · **Branch**: `fix/task-run-singleton-guard`
**Verdict**: DOABLE → delivered

## The defect (root cause)

`gopi task run <id>` had NO pre-spawn check for an already-live runner. The claim
path's conditional UPDATE looked like sufficient arbitration, but it does not
arbitrate the **claimed window**:

```go
// before — crash-recovery branch, TaskClaimExplicit / TaskResumeClaimed
UPDATE tasks SET claimed_by=?, claimed_at=?, updated_at=?
 WHERE task_id=? AND status='claimed'          -- BOTH racers see 'claimed'
```

Two runners racing the same claimed row both see `claimed` **before either
writes**, so both conditional UPDATEs legitimately match — both re-stamp — then
both advance the row to `in_progress` and both execute the briefing. The status
CAS is honest and still useless: it has no notion of *who* holds the row.

The ledger already carried the tools to tell them apart — the three-factor
process identity from epic **task-process-identity** (`internal/tools/taskproc.go`:
pid + `/proc` start ticks + command line) — but only for the
claimed→in_progress transition. A row in the **claimed** window carried a bare
`hostname:pid` stamp that cannot be verified and cannot distinguish "the claimer
died" (recoverable) from "the claimer is alive and about to execute" (the
double-run).

## Completed

1. **`internal/tools/taskrunguard.go`** — the guard, one decision function:
   - `taskRunHolderVerdict` → `free | self | live-other | unknown`, judged from
     the recorded identity via the ledger's own process-state ladder (one
     implementation, so `task run` and `task status` cannot disagree).
   - `TaskRunGuardFor` — the run door's pre-spawn check (never writes: a refused
     run leaves the ledger and the session dir untouched).
   - `taskRunHolderPin` + `taskRunRestampClaim` — the recovery rewrite is pinned
     to the holder state just judged, so of N racers on an unjudgeable claim
     **exactly one wins**; the losers are refused, not silently retried.
   - `taskRunRefusalText` — names the holder (pid + start time when known), the
     reason, `gopi task status <id>`, and `--takeover`.
2. **Identity recorded at CLAIM time** — the pending→claimed CAS
   (`TaskClaimExplicit` shape ①, `TaskNextFiltered`) and the claimed re-stamp
   (`TaskClaimExplicit` shape ②, `TaskResumeClaimed`) now write pid/start/cmd in
   the SAME statement that stamps the holder (`taskRunClaimIdentitySet`), with
   the Rule-15 degrade rule (unreadable → NULL, never a zero-valued
   pseudo-identity).
3. **Claimed→in_progress is now a CAS** — `taskUpdateStampedFrom` /
   `TaskStartForce` pin the advance to `status='claimed'`. A runner that lost
   the race is told the row "moved on" instead of quietly starting a second
   execution.
4. **CLI door** — `gopi task run <id>` calls the guard before any write and exits
   non-zero on `live-other`; the worker round refuses when its own claim was
   taken over by a live other process. New flag **`--takeover`** (the deliberate
   operator override, threaded to `TaskClaimExplicitForce` / `TaskStartForce`).
   Help text documents the guard and the flag.
5. **Root-cause fix in the shared verdict** (`taskIdentityVerdict`,
   `internal/tools/taskproc.go`): a matching **start tick** now decides
   `running` outright. Before, a changed command line under the same pid+ticks
   was reported `recycled` — i.e. "the runner is dead" for a runner sitting
   right there. That shape is real and measured: a shell's exec optimisation
   (`/bin/bash -c "sleep 30"` → `/bin/sleep 30`) rewrites argv in place under
   the same pid and start tick (found while building this task's E2E fixture).
   `pid+starttime` is unique **per incarnation**, so the cmdline is
   corroboration, never a competing axis. The cmdline keeps its full power where
   it is the only factor (a row stamped without a readable start tick).
6. **`task status` judges the claimed window** — `TaskProcessStatusFor` now
   covers `claimed` as well as `in_progress` (it answered `n/a` for exactly the
   window that needed the warning).
7. **Tests** (~640 lines):
   - `internal/tools/taskrunguard_test.go`: the verdict table (live/self/dead/
     recycled/foreign/blank/malformed/pending), recycled-incarnation vs
     matching-incarnation, the **live-other refusal** built from a REAL spawned
     process, `TaskResumeClaimed` refusing a live other holder while a dead one
     stays recoverable, the claim-time identity actually landing on the row, and
     the **4-racer pinned single-winner** race.
   - `cmd/agent/cli_tasks_singleton_guard_test.go` (E2E, real binary + real
     ledger + real holder process): non-zero exit + refusal text + untouched
     ledger; `--takeover` proceeding to the hermetic child; `task status` and
     the guard agreeing.
   - `taskproc_test.go`: replaced "cmdline differs ⇒ recycled" with the
     incarnation rule, plus the no-start-tick case that preserves it.

### Every fix carries its repro test (Rule 10.5)

| fix | repro test |
|---|---|
| live holder no longer runs twice | `TestTaskRunRefusesLiveOtherProcess`, `…RefusesLiveClaimedRow`, `TestTaskRunRefusesLiveHolderE2E` |
| recovery is single-winner | `TestTaskRunRestampClaimIsSingleWinner` (4 racers, exactly 1 wins) |
| claim window is guarded | `TestTaskResumeClaimedRefusesLiveOtherHolder` |
| claim records identity | `TestTaskRunClaimRecordsIdentity` |
| exec-optimisation is not "dead" | `TestTaskStatusRunningWhenCmdlineDiffersUnderMatchingTicks` |
| claimed window is inspected | `TestTaskStatusAgreesWithGuard` |

## Design decisions worth keeping

- **Refusal vs proceed.** `live-other` (verified live, not me) ⇒ refuse.
  Everything unjudgeable (foreign host, unreadable procfs, no stamp) ⇒ **proceed
  and record**. Evidence may add a refusal, never fabricate one (Rule 15) — and
  proceeding is safe because the recovery write is pinned, so N racers on one
  unjudgeable claim still resolve to a single winner.
- **`--takeover` stays explicit.** No automatic path bypasses the guard: the
  override is one grep-able call site (`TaskClaimExplicitForce`/
  `TaskStartForce` with `force=true`), reserved for the operator who knows the
  holder is stale (a killed session, a foreign stamp).
- **Self-claim never refuses.** A worker round passing its OWN claimed row
  through the door is `self` — checked by stamp equality and by pid before any
  /proc probing, so a wrapper claiming under an inherited `--as` name cannot
  refuse its own claim.
- **Cross-node is out of scope, deliberately.** A foreign stamp answers
  `unknown` (proceed), not a refusal: a pid on another host is not evidence this
  host can read, and the cross-node double-run belongs to the M4.21 claim-lock
  protocol.

## Deliverable location

- **Branch**: `fix/task-run-singleton-guard` (worktree `/home/user/workspace/gopi-task-singleton`)
- **Recovery**: `git show fix/task-run-singleton-guard:devdoc/task-run-singleton-guard.md`
  (code: `git show fix/task-run-singleton-guard:internal/tools/taskrunguard.go`)
- The worktree shares the main repo's refs, so the commit is already in
  `/home/user/work/GoPi` on that branch — no push needed. Merge is PA's step
  (`git merge --ff-only fix/task-run-singleton-guard` from master).

## Not Done

- **No host-level/`systemd` lock.** The guard is per-ledger (the `tasks` table);
  a second operator with a different `--db` is out of its reach by design.
- **`--takeover` does not kill the previous holder.** It lets this run proceed
  and overwrite the identity; stopping the other process remains the operator's
  action.
- **Cross-node live-holder detection** stays with the claim-lock protocol (not
  extended here).
- The stale-sweep path (`taskzombie.go`) was left as it was: it is the
  *recovery* side, and the guard now covers the *entry* side.

## Verification

- Build: `go build ./...` → OK (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK)
- Vet/gofmt: `go vet ./internal/tools/ ./cmd/agent/` clean; `gofmt -l <the 7 changed/added files>` empty
- Targeted: `go test ./internal/tools/ -run 'TestTaskRun|TestTaskResumeClaimed|TestTaskStatus|TestTaskClaimExplicit' -count=1` → ok 7.3s
- E2E: `go test ./cmd/agent/ -count=1 -run 'TestTaskRunRefusesLiveHolderE2E|TestTaskRunTakeoverOverrideE2E|TestTaskStatusAgreesWithGuard'` → **ok (1.9s)** — note the suite's own staleness gate (`task_session_cwd_e2e_test.go`) refused two sibling tests until `bin/gopi` was rebuilt; after `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` both joined the same green run. That gate is why the rebuilt binary is part of the deliverable state.
- Full: `go test ./... -count=1` → every package ok EXCEPT two PRE-EXISTING red gates that
  are red on master too, with a **zero delta** from this branch:
  - `internal/stylegate`: master reports 187 ignored-error sites + 1 test-sleep site;
    this branch 184 (+3 phantom sites from a file master lacks). Both red, both
    above the same baseline ceiling. **None of the 184/187 sites is in this branch's
    files** (`grep -E 'taskrunguard|taskproc\.go|cli_tasks\.go|tools/tasks\.go'` → no hits):
    the two new-test-file hits the sweep would otherwise have found were fixed at
    authoring time (Rule 15b checked errors, Rule 18 guard clauses).
  - `internal/testutil` `TestWorkerOwnershipPointerPresent`: master already reports
    `devdoc/worker-once-vs-task-run-next.md` + `devdoc/worker-status-overreport-20260913.md`
    (a pre-existing hold on master). This branch's devdoc ADDED a third file, so the
    canonical pointer line was inserted into `devdoc/task-run-singleton-guard.md`
    (its mention of `gopi task run` is exactly the launch-recipe shape the gate
    guards) — the remaining two are master's, untouched here.
- Commit: on `fix/task-run-singleton-guard` (see `git log -1`)

