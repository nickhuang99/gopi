<!-- doc-class: historical -->
# The Orphaned-Deliverable Family: a Reader With No Caller, an Exit Code That Stayed Silent, and the Bare `git branch -d` Behind a Vanished Ref

Task: `01a136fe53df7a26ae5440668c04150b` (client ground, branch `task/01a136fe53df7a26ae5440668c04150b`, baseline `38081ae`).
Original bug row: `01a13605` (its evidence directory is named in that row's briefing).

Three measured gaps were filed together because they are the same defect seen
from three angles: **a completed delivery whose ref nothing carries renders
exactly like a delivered one.** Two of them were code-shaped and are fixed here;
the third was a deletion channel and is diagnosed here with first-hand evidence.

---

## F1 — the reader existed and nothing called it

`internal/tools/task_ref_reachability.go` shipped the whole measurement: four
questions per row (local ref / delivery point / does a reachable commit name the
task id / what did the removal-audit sink record), the `orphaned` verdict, and
`TaskRefReport.OrphanedRows()`. Measured before the fix:

```
$ grep -rn 'TaskRefReachability\|OrphanedRows' internal/ cmd/ | grep -v _test.go
internal/tools/task_ref_reachability.go:125:// OrphanedRows returns the rows judged orphaned, sorted by task id.
internal/tools/task_ref_reachability.go:126:func (r *TaskRefReport) OrphanedRows() []TaskRefRow {
internal/tools/task_ref_reachability.go:156:// TaskRefReachability judges every terminal ledger row that records a branch
internal/tools/task_ref_reachability.go:163:func TaskRefReachability(ctx context.Context, dbPath, repoDir, delivery string) (*TaskRefReport, error) {
```

Two definition lines and its own doc comments — **no hit under `cmd/` at all**.
Its cousin `branch-refs` answers `unresolved` for both "the branch landed and was
cleaned up" and "the ref was deleted after a delivery that never happened", which
is the two states the reader exists to tell apart. So the question was asked by
no surface.

**The fix**: a new read-only verb, `gopi task orphans` —
`cmd/agent/cli_tasks_orphans.go` (argv/render/exit), the tool-side render and
exit arithmetic in `task_ref_reachability.go`, `Orphans` in
`internal/taskverbs/verbs.go` (family help, tab completion and the dispatcher all
derive from that one list), plus the usage block and the flag whitelist in
`cmd/agent/cli_tasks.go`.

Run for real, against this host's own ledger (`Ran:` is in the report):

```
$ ./bin/gopi task orphans
repo: /home/user/work/GoPi
delivery point: (none given — --delivery skips the peer half of the question)
judged 514 terminal row(s) whose deliverable branch no longer resolves here; 6 not judged
removal-audit sink: /home/user/.gopi/logs/removal-audit.log
ORPHANED (…): 285
settled: 223   (of which 6 could NOT be checked: …)
01a083c9-bb7e-78e8-9406-fada14a79865  completed  feat/fix-stats-bare-display-id  [ORPHANED]
    ORPHANED — this completed row's branch resolves to no ref on this host, …
exit=5
```

**One wording defect this run caught, and fixed in the same change.** The
reader's orphan note was a fixed string, "this **completed** row's branch …",
while the candidate SQL deliberately does not filter on `completed` — it takes
every TERMINAL status (the epic's "completed（或其它终态）") and reports the
status per row. The live run therefore printed `cancelled` rows described as
"completed": a false sentence produced by the renderer, not by the measurement.
The note now spells `row.Status`, and the two section/digest headers that made
the same claim in prose (`orphans`' own `ORPHANED (…)` line and `branch-refs`'
count line) now say TERMINAL — three renderer sites, one defect class: a
hard-coded status word over a population the query never filtered.

## F2 — the exit code answered for one population and stayed silent about the other

`TaskBranchRefsReport.Exit()` (the `branch-refs` verb) voiced `PeerReadable` and
`withheld` only. Measured live on this host before the fix:

```
by_verdict={on-peer:4, orphaned:169, unresolved:316, withheld:1}   exit=2
```

The exit status came from the SINGLE withheld row while 169 orphaned rows — the
defect class the readout was built for — reached it nowhere. A script gating on
the verb saw the repairable population with its eyes closed to the unrecoverable
one.

**The fix**: `TaskBranchRefsExitOrphaned = 5`, and the precedence is **severity,
not discovery order** — orphaned outranks withheld, because a withheld row is one
push away from repair while an orphan has nothing left to push. A second code,
`TaskBranchRefsExitUnchecked = 6`, is the `orphans` verb's "I could not look"
arm (an unreadable object store, or a row whose recorded repository this run
cannot open): it is never folded into 0, because "I did not look" is not "it is
fine". 4 is left alone — it is `for-branch`'s "no owner", and a collision would
make a script unable to tell two facts with different remedies apart.

`--json` carries `by_verdict` / `unchecked`, so no single code has to carry a
count (Rule 15).

## F3 — which channel deleted the ref? ANSWERED, with the command that did it

`task/01a132edbfcb7190a8d9a030a96bcc4e`'s ref is gone, merge `41679b36e` is in
master, and the removal-audit sink on this host held **one `worktree-removal` and
zero `branch-delete` records** for it.

The cross-host-sink hypothesis is **DISPROVED**. Both other hosts were asked:

| host | `worktree-removal` for the branch | `branch-delete` for the branch |
|---|---|---|
| hosta (this run's host) | 1 (`arm=task-self-own-sweep`) | **0** |
| peerbox | 4 (`arm=merge-run-probe-ground`, its own scratch checkouts) | **0** |
| hp | 0 | 0 |

**The deletion channel is the merge run's own LLM child, running a bare
`git branch -d`.** The evidence is the run's own transcript on peerbox
(`01a12ed1-f0be-79ac-a7fa-770b01a5307f/04b1e817-97ba-4fac-9076-28941c25ed9b.jsonl`),
which contains the command it actually executed — a loop that prunes every
ancestor-merged branch — and its output line for this very row:

```
DEL 01a132edbfcb7190a8d9a030a96bcc4e tip=593995b7a anc=YES rc=0 :: Deleted branch …
```

with peerbox's `master` reflog the other half of the proof:

```
41679b36e master@{2026-10-14 00:55:38 +0800}: merge task/01a132edbfcb7190a8d9a030a96bcc4e: Merge made by the 'ort' strategy.
```

The channel is not a mystery and not an unrecorded production arm: it is
`cmd/agent/merge_prompt.md`, whose Merger handling table tells the merge run's
child to run `git branch -d <branch>` directly. That file is **explicitly
declared out of scope** by the removal-audit census gate
(`internal/standards/removal_audit_shell_gate.go`, the "NOT covered" note), so
the gap was known and tracked — it is a *filing* gap, not an unknown deletion
path. Every PRODUCTION Go deletion site was checked and is audited (9/9 sites:
`task_merge_executor.go:320,368`, `task_merge.go:779,802`,
`task_merged_cleanup.go:630`, `task_adoption.go:424`, and
`merge_run.go:2190,2207,2309`). The sibling row in the same family
(`01a132ed8752…`) DOES have two sha-bearing `branch-delete` records — both with
`delivery_point: peerbox:~/work/GoPi`, i.e. it went through a production close-out
arm, which is why its deletion was recorded and this one was not.

**Boundary, stated honestly (Rule 41).** F3 is a DIAGNOSIS; the fix belongs to
the system layer and is NOT in this branch:

- The prompt-side fix is to route the merge run's branch disposal through the
  recorded writer (`removalaudit.BranchDelete` / a gopi verb) instead of a bare
  `git branch -d`, or to bring `merge_prompt.md` inside the census gate's scan.
- That is a change to the merger's own layer (`cmd/agent/merge_*.go` prompt
  plumbing + `internal/standards`), which Rule 28 assigns to peerbox, and it
  deserves its own row rather than a rider on this one — so it was FILED as a
  bug row the moment it was measured (Rule 41), carrying the invocations, the
  raw transcript excerpt and the acceptance:
  **`01a142a7-9243-7c36-86d5-8f3107d9f9d1`**.

Everything above is measured (commands + raw output are in this run's session
artifacts); nothing in F3 is inference, except where the text says "the fix
belongs elsewhere".

---

## Files changed

| file | what |
|---|---|
| `internal/tools/task_ref_reachability.go` | `TaskRefReachabilityProfile` (the profile-ledger delegation), `--repo` default resolution, `DeliveryRead` reported, `TaskRefReport.Exit()`, `TaskRefReportText`, `TaskRefReportJSON`, and the status-truthful orphan note |
| `internal/tools/task_branch_refs.go` | `TaskBranchRefsExitOrphaned = 5`, `TaskBranchRefsExitUnchecked = 6`, and `Exit()`'s severity precedence |
| `internal/tools/task_orphans_exit_test.go` | new: the exit-code algebra, arm for arm, including the arm that must never be silent |
| `internal/taskverbs/verbs.go` | `Orphans` verb + its family-group membership |
| `cmd/agent/cli_tasks_orphans.go` | new: the verb's runner (argv → read → render → exit) |
| `cmd/agent/cli_tasks_orphans_test.go` | new: the verb driven end to end (real ledger, real repo, real exit codes) |
| `cmd/agent/cli_tasks.go` | the usage block, the flag whitelist entry, the parse arm, the dispatch arm, the options field |
| `man/gopi-task.md` | the exit-code contract (2/3/5) and the `orphans` page |

## Verification

See the task report (`## Verification`) for the exact commands and their output:
build, the branch-scoped budget run, the two red proofs (with their restore
readings), and the real invocations of both changed surfaces.
