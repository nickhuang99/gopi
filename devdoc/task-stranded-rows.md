<!-- doc-class: historical -->
# `gopi task stranded` — the contradiction readout, and the refusal reason made legible

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a1492f-5485-7442-9d50-01e6348ccc95` (epic: "ground reclaimed / run
finished, row still non-terminal").
**Class**: historical — a past run's record. The standing contract it describes
lives in the code it names (`internal/tools/task_stranded_rows.go`'s header and
the verb's usage text), which is where a reader who needs it as authority
should look; this file is the account of the measurement that produced it.

## 1. What the briefing claimed, and what the host said

The epic opened with a premise: worker runs finish (ground taken, worktree
removed) yet their rows never reach `completed`, parking whole chains behind
Rule 33's parent gate.

Measured on this host's own ledger, 2026-10-17:

```
$ gopi task show 01a14712-27bf-7f3f-82ee-81778c6ac5f8 | grep -E '^(status|reason|run:)'
status:         completed
reason:         task finished
run:            2b2ea004-…  2026-10-17 15:10:25  completed exit=0  110 loop passes  ← resume target

$ gopi task show 01a14731-e777-72c8-897b-87857e74d1a8 | grep -E '^(status|reason|run:)'
status:         completed
reason:         task finished
run:            31b57e41-…  2026-10-17 15:58:24  completed exit=0  153 loop passes  ← resume target

$ gopi task list --status pending | grep 01a14732
#01a14732-0e00-7305-93d3-80468c30110c  [pending] …  [next]
```

Both rows are terminal with their own run recorded complete, and the chain they
were said to be blocking is **released** — `01a14732` is the claimable `[next]`
row. The premise did not hold, and this change does not manufacture a defect to
match it. (The wrapper's silent false success on the *refused* writeback was
already closed on 2026-10-07 by `7a40331f0`; that is on master, ancestor of
this branch.)

## 2. What WAS real — two measured blind spots

### 2a. Nobody joined the two halves of the contradiction

`gopi task list` prints a live row's status. `gopi task show` prints the
recorded deliverable plus an advisory line that says the recorded worktree is
`(NOT FOUND now — a stale reading)` — and stops. Neither related the two, so a
row whose ground is gone (or whose run log already carries a `session_end`)
while the ledger still calls it `pending`/`claimed`/`in_progress` printed the
contradiction **nowhere**. An operator had to do the join by hand, one row at a
time, over the whole ledger.

The chain effect was equally silent: such a row holds every pending child of it
behind Rule 33's gate, and the gate's own reason sentence is rendered only on a
`--status pending` view (`[queue-skip] parent … not released`). `task show` on
the parked child said nothing about being parked.

### 2b. A REFUSED `completed` writeback left the row with no reason at all

`reclaimRunTrackedDirtRefusal` / `reclaimRunResidueRefusal`
(`internal/tools/task_reclaim.go`) write their refusal to the row's trail under
their own event names — `completed-unclean-tracked-work-refused` and
`completed-unclean-refused` — and hand the sentence to the wrapper, whose only
consumer prints it on **stderr** (`writeback failed (continuing): …`). That
stderr belongs to a process which has already exited.

`gopi task show` renders a `verdict:` line from `verdictTrailEventNames`, and
**neither event name was in it**. Measured on this host's ledger: of the
`task_events` rows carrying `completed-unclean*`, exactly one was a
`…-tracked-work-refused` (row `01a143b1`), i.e. a row whose close-out the gate
refused — and its `show` carried no refusal line at all.

### 2c. The parked-row question (`task show` said nothing)

```
$ gopi task show 01a1478f-e681-76d4-b3be-9f36543d91c2 | grep -iE 'park|queue|skip'
parent_task_id: 01a14776-4b23-7154-a690-61fd355895dc
(no other line)
```

The child knows its parent; the row does not say the parent is unreleased, nor
which statuses would release it.

## 3. What this change delivers

### 3.1 `gopi task stranded` — a new READ-ONLY ledger verb (Rule 3: no new tool)

`internal/tools/task_stranded_rows.go` (measurement + render) +
`cmd/agent/cli_tasks_stranded.go` (argv/exit code). Two independent arms,
because either alone is a real contradiction:

| arm | the fact | why it contradicts a live status |
|---|---|---|
| **ground gone** | the row's recorded `deliverable.worktree` does not exist | nobody can still be working in it |
| **run ended** | the row's newest run log carries `session_end` | the run that owns the row is over |

Requiring both would be the false negative the verb exists to end (run
directories are pruned independently of the ledger).

It prints, per row: the id, the status, the arm(s) that fired, the run's exit
code (or "no exit code recorded: ended by a signal"), and **how many PENDING
rows name it as their parent** — the chain it is holding. Exit `0` = judged,
every live row agrees with its record (and **nothing is printed**: silence is a
verdict, distinguished from "never ran" by the code); exit `5` = at least one
contradiction.

**It never writes.** No status move, no event, no file, no config key (Rule 35 —
zero dependency, no resident process, no new state). The risk symmetric to Rule
30's dirty refusal is a surface that "fixes" a row by promoting
`pending → completed` on an inference; the remedy printed is the operator's own
(`gopi task update <id> --status completed`, or `gopi worker --resume <id>`).
Pinned by `TestStrandedRows_ReadsAndNeverWrites` and
`TestTaskStrandedNeverWritesAStatus`.

### 3.2 The refusal reason is readable on the row

`reclaimRunTrackedDirtRefusalEvent` and `reclaimRunResidueRefusedEvent` joined
`verdictTrailEventNames` in `internal/tools/tasks.go` — the list's own
membership test is "a JUDGMENT ON the row", and a gate declining to close the
row is the most literal case of one. `task show` now renders the gate's own
sentence on `verdict:`, naming the worktree, the dirty entries and the two
remedies.

No new event, no new column, no new file: the fact was already durable on the
trail; this makes it visible where the status is read.

### 3.3 What was deliberately NOT done

* **No auto-promotion** of `pending → completed` (Rule 30's inverse accident).
* **The Rule 33 parent gate is untouched** — its status was not changed, its
  release set was not changed, and `TestParentGate` stays green unchanged.
* **No second surface for "why is this row parked" beyond what the verb now
  prints.** The parked-child question is answered by the gate's own reason
  string, reused (Rule 14), not re-decided.

## 4. The minimal repro (red → green)

`internal/tools/task_show_writeback_refusal_test.go` — the repro for 2b. Proven
red by mutating the fix away (removing the two names from
`verdictTrailEventNames`):

```
--- FAIL: TestTaskShowRendersTheWritebackRefusalReason
    `task show` does not carry "verdict:" — the refusal reason is stderr-only again
    `task show` does not carry "completed-unclean-tracked-work-refused"
--- FAIL: TestTaskShowRendersTheResidueRefusalReason
```

Restored, both pass. The restore was verified against the index-free revision
(`git diff HEAD -- internal/tools/tasks.go` shows the 14-line addition, nothing
else).

`internal/tools/task_stranded_rows_test.go` — the repro for 2a: a fixture ledger
carrying one ground-gone row and one run-ended row reports exactly those two and
leaves the healthy live row AND the terminal row whose worktree was reaped
alone; a healthy ledger renders nothing and exits `0` while `Scanned > 0` (so
its silence means something).

## 5. Ran for real (Rule 31)

Pre-fix binary (the installed `gopi`):

```
$ gopi task stranded
gopi task stranded: unknown subcommand "stranded" (want archive|branch-refs|…)
```

Post-fix binary, on a fixture sandbox ledger with one contradiction:

```
$ GOPI_HOME=<sandbox> ./bin/gopi task stranded
stranded: 1 live row(s) with 1 scanned — each row's own record says the run is over while the ledger still calls it live:
  01a20000-0000-7000-8000-0000000000aa  in_progress  [run ended]  fixture: a live row whose run already ended
      run:      newest run log carries session_end (exit=0) — it is over
  this reader never writes a status: close a row whose work landed with `gopi task update <id> --status completed`, or hand it back to a run with `gopi worker --resume <id>`
exit=5
```

Same ledger, the row closed out:

```
$ … ./bin/gopi task stranded
exit=0        (nothing printed)
```

On this host's own ledger, `./bin/gopi task stranded` prints nothing and exits
`0` — the two live rows here agree with their records. That is the honest
verdict: **this host does not currently exhibit the class**, and the verb is the
instrument that would have said so if it did.

## 6. Trail

* `internal/tools/task_stranded_rows.go`, `cmd/agent/cli_tasks_stranded.go` — the verb.
* `internal/tools/tasks.go` — two event names join `verdictTrailEventNames`.
* `internal/tools/verify_gate.go` — `MergeVerifySetDeliverableGroundForTest` (the
  fixture seam that stamps repo+branch+**worktree**, so a CLI test never
  hand-rolls a ledger write, Rule 32).
* `internal/taskverbs/verbs.go` — the `stranded` spelling, advertised.
* `cmd/agent/cli_tasks.go` — argv/flag/dispatch/usage.
* `devdoc/gopi-task-help-surface-inventory.md` — row 21 (the G1 gate reads it).
