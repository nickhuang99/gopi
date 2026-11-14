<!-- doc-class: historical -->
# Task Report: a live session's worktree was deleted mid-run — root cause + fix

**Incident:** 2026-09-14, task `01a09d67` (sibling `01a09d07`). The linked worktree
`~/workspace/gopi-nopriv-daemon` (branch `fix/daemon-no-privilege-remedy`) was deleted
**twice** while its owner session was running `go test` inside it — ~30 phantom failures
(`getwd` / relative-path `open`) that were initially misread as code regressions. The owner's
report (`.gopi/sessions/01a09d07-…/task-report.md`) records the reaper also deleting the
relocated scratch checkout twice more, once mid-`go test`.

**Deliverable:** branch `fix/liveclaim-worktree-veto`.

## Root cause — two independent defects, one shared blind spot

Both were confirmed with primary evidence, not inferred.

### RC-A — the sweep's ownership evidence was truncated to the OLDEST rows

`worktreeSweepTaskRows` (the evidence the ownership veto reads) loaded rows with

```sql
... WHERE deliverable LIKE '%worktree%' ORDER BY updated_at ASC LIMIT 200
```

Measured on the incident host at the incident time: **286** rows named a worktree, and the live
owner's row (`01a09d07`, `in_progress`, updated 08:16:57, worktree claimed until ~09:19) sat at
**position 275** by `updated_at ASC`. The sweep therefore never loaded it, the ownership veto
(rung ③ of `worktreeSweepJudge`) found no live owner, and the registration looked aged + clean +
unowned.

**Recency and liveness correlate**, so an age-`ASC` `LIMIT` drops exactly the rows that protect
live ground. This is the load-bearing half of the incident: dropping the row is silent, and no
downstream guard can veto a row it never saw.

### RC-B — "the claim pid is dead" was read as "the holder is dead"

Two holders of that verdict: the dead-holder reclaim arm (`reclaimDeadHolderCandidate`) had
**no live-row veto at all**, and the shared classifier chain
(`finishCleanupOwnerRowLive` → `finishCleanupDeadLocalHolder`) released a live-status row as soon
as its claim pid did not resolve locally.

The only channel that can see *another session's* process was `/proc/<pid>/cwd`, and this host
blanks it out:

```
$ cat /proc/sys/kernel/yama/ptrace_scope
1
$ readlink /proc/3328571/cwd          # a foreign session's process, same uid
Permission denied
$ ls -l /proc/3328571/cwd
lrwxrwxrwx 1 root root 0 … '/proc/3328571/cwd' -> ...   (readable: cmdline, stat, status)
```

Measured 185 of ~360 `/proc` entries unreadable — i.e. the probe answered "no live holder" for
**exactly the processes a sweep must not disturb**, and failed **open**. `cmdline`, `stat` and
`cgroup` stayed readable across sessions; `cwd`, `fdinfo`, `maps` and `environ` did not.

### Which arm deleted the directory

Worth stating precisely, because the two sweeps do different things:

| arm | effect on the directory |
|---|---|
| `worktreeSweepRun` (`gopi task sweep-worktrees`) | `git worktree prune` — drops the **registration**, never the directory |
| finish-cleanup ③ | `git worktree remove <wt> --force` — **deletes the directory** |
| `reclaimGitRemove` (dead-holder reclaim) | `git worktree remove` (no `--force`) — **deletes the directory** |

Both *directory*-deleting arms are gated by the two verdicts fixed here, so the fix covers the
deletion path itself — not only registration sweeps. (RC-A additionally explains why the
ownership veto could not save the tree: the shielding row was never loaded.)

## Fix

1. **Exhaustive evidence (RC-A).** Both ledger evidence loaders (sweep rows + the finish-cleanup
   live rows) dropped `ORDER BY … LIMIT ?`; the SQL `LIMIT` is gone with a comment explaining why
   the truncation was the bug (the bound is no longer a silent partial universe).
2. **A live runner keeps the veto (RC-B).** The row's **own recorded runner identity** —
   `proc_pid` + `proc_start_ticks` + `proc_cmd`, stamped by the runner at run start, all readable
   cross-session — is compared against the live process table by the new
   `finishCleanupProcessHolderLive` (start ticks must match, so a recycled pid cannot pass; a
   zombie does not count, matching `pidAlive`). The reclaim arm gained the same veto, so *both*
   directory-deleting arms are covered.
3. **Conservative in one direction only.** No recorded identity ⇒ the older, narrower rule still
   applies (the 2026-09-12 orphan release is preserved); an unreadable `/proc` or a foreign claim
   stamp is never evidence of death. `t-rig-dead` below pins that the guard is not a blanket
   refusal.

Constants: no new magic numbers or bare-string keywords; the SQL fragments and the `/proc` names
reuse the existing named constants (`taskLiveStatusSQLList`, `finishCleanupWorktreeVerb`,
`claimHolder`, `pidAlive`, `procStartTicks`, `procCmdline`).

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`.
- **Red proof (Rule 10.5)** — with the fix reverted by mutation, all three new tests FAIL; with the
  fix in place they PASS:
  ```
  --- FAIL: TestLiveClaimRunnerSurvivesDeadHolderReclaim
      a live runner's worktree must never be a reclaim candidate, got 1: […LiveRow:true]
  --- FAIL: TestLiveClaimRunnerSurvivesWhenClaimStampIsUnresolvable
      a live recorded runner must not be reported as a dead local holder
  --- FAIL: TestWorktreeSweepSeesTheNewestRowsOnALargeLedger
      the sweep's evidence must not be truncated: loaded 200 rows, seeded 251
  ```
  The last line *is* the incident mechanism, reproduced: the window loaded 200 rows and the
  newest (live) row was not among them.
- **Green suite:** `go test ./...` and `go test ./internal/standards/` with `TMPDIR` pinned to the
  scratch tree (Rule 22). The inherited reds this change recorded were the now-REMOVED peak-hour
  cost guard's refusals in `cmd/agent` (`agent_spawn_guard*_test.go`, tracked as `01a09d73`):
  the guard and its `GOPI_IGNORE_PEAK_HOURS` escape hatch were deleted outright by
  `fix/remove-peak-guard` (commit `bb0c4c1a`, devdoc/peak-guard-removal.md), so no package needs
  an override to run and no line below may ask for one.
  **`GOPI_IGNORE_PEAK_HOURS` is RETIRED: it is read by nothing** — the env reader left with the
  guard (`internal/peakhours` is absent from the tree; `cmd/agent/peak_guard_removal_test.go`
  proves the spelling is gone and that `--ignore-peak-hours` is now an unknown flag). **No
  exemption of any kind is needed to run this rig or its suite**, at any hour; a reader who
  remembers this line telling them the rig "needs `GOPI_IGNORE_PEAK_HOURS=1`" is holding a
  sentence that was true only while the guard existed. Setting it changes nothing.
- **Scratch rig (real CLI):** `scripts/repro-live-holder-worktree-veto.sh` drives
  `gopi task sweep-worktrees --apply` in an isolated HOME and prints
  `keep …/wt (owned by live task t-rig-positive (in_progress))` for the live-runner case — the
  fix's own verdict, from the real binary (the peak-hour override it used to need is gone with
  the guard) and asserts the live runner's tree survives with its registration intact.
  **The rig deliberately does not attempt the dead-holder direction through the CLI:**
  measured, the two *directory*-deleting arms have no non-resident CLI face — `reclaimGitRemove`
  is reached only from `ReclaimDeadHolderWorktrees`, which runs from `mergerPassOnce` /
  `runTaskLifecycleTick`, i.e. inside a resident daemon — so that direction is verified by the
  package test `TestDeadHolderReclaimStillFreesAProvenDeadRunner` (invoked by the rig) instead of
  by a claim the rig cannot honour.

**Measured while building the rig (worth keeping):**
- the sweep's ownership rung keeps ANY non-terminal row by design (rung ③, `row.terminal()`),
  so the registration sweep can never be the arm that frees a dead holder's tree — that is
  finish-cleanup ③ / `reclaimGitRemove`, both gated by the verdicts fixed here;
- the sweep discovers its repos from the **cwd**, so a rig must run *inside* the rig repo;
- a registration's age is measured from the `commondir` **file's** mtime inside the worktree's
  private git dir (`worktreeSweepCreatedAt`) — aging the directory does not age the registration.

## Not Done

- The **merger daemon** was not restarted against this branch, so the fix was not observed
  protecting a real multi-session takeover end to end; the unit tests and the scratch rig use the
  same predicates and the same CLI, but the incident's takeover sequence was not replayed live.
- The incident's own delete event is **not** in any retained log: `~/.gopi/logs/merger.log` covers
  a 21:12–01:16 window and holds no `nopriv` line, and nothing in `merger.log`,
  `worker.log` or `notify/task-events.jsonl` names the path. The attribution to the two
  directory-deleting arms is therefore by **mechanism + row state at the incident time**, not by a
  captured log line — stated plainly rather than asserted.
- `reclaimScanMax` is still the constant name/consumers used elsewhere; only its misuse as a
  *truncating window* over evidence was removed.

## Files

- `internal/tools/worktree_sweep.go` — exhaustive evidence loader (RC-A)
- `internal/tools/task_finish_cleanup.go` — runner-identity liveness, both verdicts (RC-B)
- `internal/tools/task_reclaim.go` — live-row veto on the reclaim arm (RC-B)
- `internal/tools/live_claim_worktree_veto_test.go` — the three regression tests
- `scripts/repro-live-holder-worktree-veto.sh` — scratch rig, real CLI
