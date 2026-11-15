<!-- doc-class: normative -->
# Worktree removal forensics — the grep-first rule, the record, and the arms

**Audience**: whoever meets a worktree or branch that is **GONE** and does not
know which code path deleted it; and whoever is told *"worktree in use by live
process"* while the directory is empty.

**Standing claim about master** (normative): every removal gopi performs on a
worktree or a branch is recorded by `internal/removalaudit` into ONE append-only
JSONL sink, and that sink answers "who removed this, and why" in one command.
This doc is the map: where the sink is, what one line says, which arm writes
which line, and where `--fast` stops. Read §1 and run §2 **before** reading any
shell history.

---

## 1. The grep-first rule

> **An unexplained worktree/branch removal is answered by
> `grep <path-or-branch> <data-root>/logs/removal-audit.log` — never by reading
> shell history.** The audit exists precisely for this.

Three corollaries, each learned from a real investigation:

1. **Grep the PATH or the BRANCH, not a task id.** The record's `target` is the
   absolute worktree path, and a branch-delete record carries the ref name in
   `branch` (and also in `target`). A path is what the operator has in hand; a
   task id may be in no record at all (§7).
2. **Two lines, not one.** A grounded run writes `kind=worktree-ground` when it
   TAKES a directory and `kind=worktree-removal` when one is released. Reading
   both is what turns "the directory vanished" into "taken at T1 by pid P, freed
   at T2 by arm A".
3. **The sentence after `arm` is the verdict.** The `evidence` field names the
   veto channels the deciding arm consulted and did not trip. Never re-derive
   liveness from `/proc` when the record already says which check refused to
   refuse.

## 2. Where the record lives — the path spelling matters

The sink is resolved in exactly ONE place, from two constants:

| constant | value | meaning |
|---|---|---|
| `LogDirName` (`internal/removalaudit/audit.go:92`) | `"logs"` | the component-log directory under the gopi data root — **plural**; `log/` is a different, wrong directory |
| `FileName` (`internal/removalaudit/audit.go:95`) | `"removal-audit.log"` | the audit file inside it |

So the path is `<data-root>/logs/removal-audit.log` — **`logs`, plural** — with
`<data-root>` the gopi dir (`gopi config show` prints it as *gopi dir*; on this
host `/home/user/.gopi`). `DefaultPath` (`internal/removalaudit/audit.go:614`)
joins the two constants to the resolved data root; `GOPI_REMOVAL_AUDIT_LOG`
(`EnvSink`, `internal/removalaudit/audit.go:89`) overrides it wholesale, and
`sinkPath` (`internal/removalaudit/audit.go:606`) is the single reader of that
override. A run-local env pin is sufficient — the sink is **not** named in
settings.json or `CONFIG.md`, so there is no config key to look for.

### 2a. The `logs/` directory is the SINK's own obligation (fixed 2026-10-15)

`<data-root>/logs/` is created by **nobody else**: every other component log is
opened on a path a daemon spawner has already made (`cmd/agent/cli_daemon_spawn.go`
> **Retired mechanism (merge verb removal, 2026-11-01).** Every `gopi merge …`
> below is the RECORD of the 2026-09 measurement; the top-level verb was removed
> afterwards and answers exit **1** today. The file/page discipline this document
> is about is unchanged, and the engine is reached now as `gopi task merge`
> (`man/gopi-merge.md` is its contract page).

MkdirAll's `logs/` before it starts a resident process). So on a FRESH profile —
a sandbox, a new machine, a new operator — the directory does not exist, the
append's `os.OpenFile` answered ENOENT, and **every record on that profile was
lost**: one stderr refusal per record, and no file to read back. Measured on the
real verb (Rule 31): a fresh `GOPI_HOME` driven through `gopi merge --fast`
pruned the spent ref and printed

```
removal-audit: branch-delete record for task/e2e not written: open …/.gopi/logs/removal-audit.log: no such file or directory
```

with the branch gone and the sink absent — the incident shape (a mutation with
no attributable record) reproducing itself on every new profile.

**The rule that now holds:**

- **The DEFAULT sink's directory is the emitter's to create** — `emit` calls
  `os.MkdirAll(<data-root>/logs, sinkDirMode)` at append time (one `stat` when it
  already exists). A failure there takes the SAME loud `refusef` path as every
  other write failure: the directory is a step of the append, never a separate
  error path, and recording stays best-effort (a hole in the audit trail must
  not leak a worktree).
- **An explicit `GOPI_REMOVAL_AUDIT_LOG` names a FILE, and the directory it
  implies is the CALLER's contract** — deliberately NOT created. A caller that
  names a path names where the record goes; inventing directories on its behalf
  would turn a typo into an audit trail nobody looks at. Such an override is
  refused LOUDLY by the open, and that refusal is pinned by a test
  (`TestExplicitOverrideWithAMissingDirectoryIsRefusedNotInvented`).

The red half is permanent, not a claim: `SetSinkDirEnsureDisabledForTest(true)`
drives the emitter back to its pre-fix behaviour, and
`TestFreshProfileWithoutTheDirectoryCreationLosesTheRecord` must then observe the
lost record AND the refusal on stderr.

Records are written **before** the git verb, never after: a record whose removal
then failed is still a record (`emit` at `internal/removalaudit/audit.go:543`
writes one line; a write failure is reported on stderr by
`refusef` at `internal/removalaudit/audit.go:687` and **never fails the
caller** — a hole in the audit trail must not turn into a leaked worktree).
The same sink serves the anchors: `TaskGroundAnchorPath`
(`internal/tools/run_ground_anchor.go:100`) resolves the same file through the
same constants (`internal/tools/run_ground_anchor.go:104`), so ground-taken and
ground-released are two entries of ONE history, in file order.

## 3. One record = one line

A line is a `removalaudit.Record` marshalled with `omitempty` on the OPTIONAL
fields only. The attributing core is ALWAYS on the wire.

```json
{"version":1,"ts":"2026-09-25T22:48:14.978987382Z","kind":"worktree-removal","arm":"task-self-own-sweep","op":"worktree:remove","pid":1334633,"repo":"/home/user/work/GoPi","target":"/home/user/workspace/gopi-01a0d9ab7f5a75f49be4f842fa1449cf","branch":"task/01a0d9ab7f5a75f49be4f842fa1449cf","evidence":"unowned registration of this run's own repo; live-session-veto and the live-holder scan did not refuse; checklist own-ground + integration-lineage (reclaim_ground_checklist.go)"}
```

| field | always? | meaning |
|---|---|---|
| `version` | yes | record schema version — `Version = 1` (`internal/removalaudit/audit.go:108`); a field of every line so a future shape change is readable in the file itself |
| `ts` | yes | RFC3339Nano, UTC, the write instant (`TS` field tag, `internal/removalaudit/audit.go:341`) |
| `kind` | yes | WHICH CLASS of mutation: `worktree-removal` (`KindWorktreeRemoval`, `internal/removalaudit/audit.go:114`), `worktree-ground`, `branch-delete` |
| `arm` | yes | the **calling arm** — the code path that decided to delete (`type Arm string`, `internal/removalaudit/audit.go:169`). The single most valuable field: the 2026-09-17/18 post-mortems had the destruction and no arm |
| `op` | yes | the git operation about to run — `OpWorktreeRemove = "worktree:remove"` (`internal/removalaudit/audit.go:129`), `worktree:ground`, `branch:delete` |
| `pid` | yes | the process that decided; the pid to read back out of `worker.log` / `merger.log` |
| `repo` | no | the repository the mutation ran in |
| `target` | yes | the absolute worktree path for a worktree op, the ref name for a branch op — **this is what you grep** |
| `branch` | no | the ref the removed directory had checked out (`FieldBranch`, `internal/removalaudit/audit.go:148`) |
| `task_id` | no | the ledger row, when the arm knew one (`FieldTaskID`, `internal/removalaudit/audit.go:149`); a sweep finding an unowned registration has none |
| `evidence` | no | WHY it was safe to delete: the veto channels consulted and not tripped (§4). Field name is `FieldEvidence` (`internal/removalaudit/audit.go:150`) |
| `holder_pid`, `holder_start`, `holder_sid` | ground only | the TAKER's identity on a `worktree-ground` line — a (pid, start-ticks) pair plus a session id, kept as VALUES so liveness is decided from numbers and not by parsing an English sentence |

Field names are constants (`FieldVersion`/`FieldTS`/… at
`internal/removalaudit/audit.go:140`, `internal/removalaudit/audit.go:141`,
`internal/removalaudit/audit.go:144`, `internal/removalaudit/audit.go:146`) so a
consumer references the key instead of retyping it (Rule 1b/14). The `evidence`
vocabulary is itself a constant set: the three VETO CHANNELS are
`live-session-veto`, `live-holder-scan` and `ledger-live-claim`.

## 4. The arm census — who deletes, and what each arm means

Every arm is a constant in `internal/removalaudit/audit.go`; the structural gate
requires each one to be referenced by a real removal site and each site to name
one of them. 21 live arms:

| arm (value) | what it means |
|---|---|
| `worktree-sweep-reclaim` | the worktree sweep recycling a registration nobody lives in (`internal/tools/worktree_sweep.go:2037`) |
| `worktree-sweep-dead-registration-branch` | the sweep dropping the branch of a DEAD registration after its git bookkeeping was reaped (`internal/tools/worktree_sweep.go:2343`) |
| `task-row-reclaim` | a task row's own reclaim path — the row's worktree was released, the record says by which reclaim (`internal/tools/task_reclaim.go:496`) |
| `task-self-reclaim` | the worker's own FINISHED rows released at the end of its round (`internal/tools/task_reclaim_self.go:250`) |
| `task-self-own-sweep` | a registration **of this run's own repo** that no ledger row owned, freed by the worker's own post-round pass (`internal/tools/task_reclaim_self.go:731`) — the arm that removed the fixture worktree in §6 |
| `finish-cleanup-worktree` | the delivery chain's worktree step, branch preserved (server / single-node shape) (`internal/tools/task_finish_cleanup.go:1697`) |
| `finish-cleanup-worktree-force` | the delivery chain's `--force` removal — **the one step that deletes uncommitted work** |
| `finish-cleanup-branch` | the delivery chain's branch step, plain `-d` (`internal/tools/task_finish_cleanup.go:3875`) |
| `finish-cleanup-branch-force` | the same chain's `-D` after git refused the plain delete of a ref its own push had already landed (`internal/tools/task_finish_cleanup.go:3910`) |
| `merged-cleanup-worktree` | the merged-cleanup sweep removing a merged branch's checkout (`internal/tools/task_merged_cleanup.go:605`) |
| `merged-cleanup-branch` | the merged-cleanup sweep's branch step (`internal/tools/task_merged_cleanup.go:611`) |
| `merged-cleanup-orphan-branch` | the ref-only lane: a merged branch with NO checkout left to cross-check (`internal/tools/task_merged_cleanup.go:709`) |
| `task-merge-branch` | the one-shot merge path pruning a ref it just proved spent (`internal/tools/task_merge.go:648`) |
| `merge-exec-branch` | the merge-executor path pruning a spent branch (`internal/tools/task_merge_executor.go:304`) |
| `adoption-branch-force` | the adoption chain force-deleting a ref whose work it proved already integrated (`internal/tools/task_adoption.go:419`) |
| `adoption-branch-clean` | the adoption chain deleting a ref with `-d` after its own merge of that ref was committed (`internal/tools/task_adoption.go:440`) |
| `merge-build-gate-ground` | the merge-time BUILD gate dropping the detached scratch checkout it materialised for one build (`internal/tools/merge_build_gate.go:498`) |
| `task-ground-anchor` | not a removal: the producer's RUN-GROUND ANCHOR, written the moment a run TAKES a directory (`ArmTaskGroundAnchor`, `internal/removalaudit/audit.go:223`) — this is the line that makes a grounded run attributable at the moment of grounding |
| `merge-run-spent-branch` | **RETIRED 2026-11-01** — it was the foreground one-shot merge run pruning a ref it just proved SPENT (`cmd/agent/merge_run.go:2023`); the arm's constant was deleted with its emit (`internal/removalaudit/audit.go`, epic merge-verb-removal item 3), so a record naming it is evidence of an OLD binary. The surviving spent-branch prunes are `task-merge-branch` and `merge-exec-branch` above |
| `merge-run-probe-ground` | **RETIRED 2026-11-01** — the same run dropping the DETACHED scratch worktree it materialised to measure one branch's conflict (`ArmMergeRunProbeGround`); deleted with its emit alongside the arm above, and nothing materialises a merge-run probe ground today |

**Retired arms are deleted with their emit**, so a declared-but-unemitted arm
cannot survive: `repos-sweep-branch` retired 2026-09-19 (the repo sweep no longer
prunes a spent branch — it REPORTS the branch, its tip, the verdict and the owner
evidence, and mutates nothing; see the comment at
`internal/removalaudit/audit.go:238`), and `task-verify-ground`
(`ArmVerifyGround`) retired 2026-10-30 with slice 2/4 of epic 01a18799 — the
`verify` verb and the `taskVerifyGround` function that created *and* freed that
private scratch checkout were deleted together, so no path creates or reclaims a
verifier ground any more. A record naming a retired arm is
therefore evidence of an OLD binary, not of a live path — worth knowing before
you search for the call site.

## 5. `--fast` — what it does and does not touch

`gopi merge --fast` (flag constant `mergeFastFlag`, `cmd/agent/merge_run.go:460`;
parsed to `opts.fast` at `cmd/agent/merge_run.go:2731`) was the deterministic-only
arm: pure git decisions, **no measurement, no child, nothing filed**. *(That flag
went with the verb on 2026-11-01: the surviving spelling `gopi task merge` has no
`--fast` — measured, `gopi task merge -h` lists `--all`, `--repo`, `--submit`,
`--db` and nothing else — so what follows is how to READ an old binary's records,
not an instruction to run anything. Every `cmd/agent/merge_run.go:` anchor below
names a deleted file.)* The flag's
own comment marks the boundary: *"--fast STOPS HERE, and stopping here is the
whole point of the flag"* (`cmd/agent/merge_run.go:2080`), and a diverged branch
is reported with `mergeDetailFastLeft` (`cmd/agent/merge_run.go:800`) and left
EXACTLY as it is — same ref, same tip, no archive tag, no ledger row.

For the audit trail this means:

- **`--fast` DOES still delete**: ① a branch already contained in the
  integration branch (`merge-run-spent-branch`, `cmd/agent/merge_run.go:2023`)
  — that lane runs before the stop, because `git merge-base --is-ancestor` is a
  git fact, not judgement.
- **`--fast` does NOT**: materialise a conflict-probe worktree (so no
  `merge-run-probe-ground` line), spend a hand-over child, file branch rework,
  or move the integration branch.
- **A `merge-run-probe-ground` line therefore proves the run was NOT `--fast`**,
  and a `merge-run-probe-ground` line with an UNRELATED-HISTORY verdict is a
  full run that reached the probe; the retire of that case is a tag, never
  `branch -d` (`cmd/agent/merge_run.go:2076`), so no `branch-delete` line
  accompanies it.
- `--fast` NEVER bypasses the worktree-held exclusion: a branch checked out in a
  worktree is reported as `mergeDetailWorktreeHeld` (`cmd/agent/merge_run.go:794`)
  and left alone — the ref stays, and no record is written because nothing was
  removed.

## 6. A worked example

The task's brief chose `/home/user/workspace/gopi-01a0d68746ed7ea090e56bff794e1b9f`
as its example path. Run the recipe on it and **this host's sink is silent** —
`grep -F` returns nothing, and no line carries `pid=2183284`. That is the second
half of the lesson (§7): a quoted removal is a claim about the host that ran it,
and the sink in front of you is the only authority. The same arm doing the same
job on a path this sink DOES hold is
`/home/user/workspace/gopi-01a0d9ab7f5a75f49be4f842fa1449cf`:

```bash
grep -F '/home/user/workspace/gopi-01a0d9ab7f5a75f49be4f842fa1449cf' \
  /home/user/.gopi/logs/removal-audit.log
```

Two lines come back, in file order:

```
2026-09-25T19:08:11.645933418Z  kind=worktree-ground   arm=task-ground-anchor  pid=1326684
2026-09-25T22:48:14.978987382Z  kind=worktree-removal  arm=task-self-own-sweep  pid=1334633
  branch=task/01a0d9ab7f5a75f49be4f842fa1449cf
  evidence="unowned registration of this run's own repo; live-session-veto and the
            live-holder scan did not refuse; checklist own-ground + integration-lineage
            (reclaim_ground_checklist.go)"
```

**Reading it**: pid 1326684 took the ground at 19:08 (holder session 1247531),
and pid 1334633 — the long-lived worker — freed it at 22:48 as an unowned
registration of its own repo, after the live-session veto and the live-holder
scan both declined to refuse. The three hours between the two lines are the veto
window: the directory was unremovable while a fixture process still stood in it,
and the same worker's next pass over its own repo found it unowned and free. No
merge, no `finish-cleanup` — a run's own sweep.

Project the whole history of a path in one command (`jq` is present on this host;
`grep -o` is the zero-dependency fallback):

```bash
grep -F '<path-or-branch>' <data-root>/logs/removal-audit.log \
  | jq -r '"\(.ts) \(.kind) \(.arm) pid=\(.pid) \(.evidence // "")"'
```

## 7. Limits of the record — what it cannot tell you

- **Absence is not innocence, and it is not proof either.** The sink is a plain
  file: if it is rotated, truncated or (on a peer) has never seen the record,
  `grep` returning nothing means *no line here* — read the sink's own mtime and
  the run's host before concluding "nothing removed it".
- **Host/peer provenance.** A line is written by the process that *decided* the
  removal, on the host it ran on. A worktree whose row was driven on another
  peer may be accounted for only in THAT peer's sink.
- **The arm is authoritative; a prose report quoting an arm is not.** Measured
  2026-09-25: `devdoc/handoff-20260926-restart-verification.md` §6.4 quotes a
  `task-self-own-sweep` removal of `…/gopi-01a0d68746ed7ea090e56bff794e1b9f`,
  and this host's sink contains **no line** for that path or that pid — that
  record lives on the peer that ran it. So: always re-run the grep against
  **your** sink before repeating a quoted line, and say which host answered.

## 8. Cross-references

- `internal/removalaudit/` — the whole record: constants, arms, `Records`
  (read-back), `DefaultPath`. Start at `internal/removalaudit/audit.go`.
- `cmd/agent/merge_run.go` — `--fast` semantics, the worktree-held exclusion,
  and the two arms that belong to a merge run (§5).
- `devdoc/handoff-20260926-restart-verification.md` §6.4 — the investigation
  that produced this doc; it states the grep-first rule and points here for the
  record's shape and the arm census.
- `internal/tools/task_finish_cleanup.go` — the delivery chain, and every guard
  that refuses it; `internal/tools/task_reclaim_self.go` — the self-reclaim and
  self-own-sweep arms.
