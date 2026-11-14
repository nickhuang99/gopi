<!-- doc-class: normative -->
# 任务依赖：LLM 提交的顺序就是逻辑依赖顺序 — 默认串行成链

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**The ruling (2026-10-05, the operator): a sequence of tasks an LLM submits IS a
dependency order, so it is chained serially by submit order — losing parallelism
costs only speed; losing a dependency costs correctness.**

Every file:line in this document was re-measured at commit
**`da32f3b0`** (this run's HEAD, on which `master` is an ancestor — see
*Provenance* at the end). It documents a PRINCIPLE and the gap between it and
the current tree; it changes no code. The code change is task
`01a10a24-7a8e-7a8b-b420-b90f814ca089`.

---

## 1. The ruling (verbatim — this is the only authority)

> **"我们已经说了损失并行只是快慢，而准确性是要命的事情。不要考虑并行的损失，不用考虑！！！默认顺序就是依赖，懂吗？你具体怎么做我不管，我要的是准确，而且是防止万一都是有依赖的话。LLM 不能表达那么多，它给的顺序就是逻辑的依赖。我们要这么翻译。"**

> **"我说的是 LLM 的提交，命令行是人类和单独的一次，不需要。"**

Translated into the one rule a future agent must implement:

```
submit A   →  parent_task_id = ""     (the chain's head)
submit B   →  parent_task_id = A
submit C   →  parent_task_id = B
… and so on, position N's parent is position N-1.
```

**⇒ 宁可损失并行（只是快慢），不能丧失依赖（要命）。**

**Why the ORDER is the right encoding — not a dependency graph.** An LLM cannot
faithfully express a DAG: asked to fill `parent_task_id` per item it omits
edges and invents others. What it *can* express is which item it thinks comes
first — the order of its own output. So the harness translates the order into
the graph, and the model is never asked for the graph. The operator named the
failure mode this guards against: *"防止万一都是有依赖的话"* — assume a
dependency unless proven otherwise, because the cost of a wrong assumption is
asymmetric.

---

## 2. The boundary — where the rule applies, and where it deliberately does not

| channel | rule | why |
|---|---|---|
| **The LLM's submit path** (`task_submit` tool / `task(action=reframe)` roadmap) | ✅ **chain by submit order by default** | the ruling's subject |
| **The command line** `gopi task submit …` | ❌ **unchanged** | *"命令行是人类和单独的一次，不需要"* — a human submits one row, once, and states the edge if they want one |
| **The command line's `--parent-task-id`** | ✅ **already exists — nothing new to add** | `cmd/agent/cli_tasks.go:639` (usage), `:865` (field), `:1309` (parse; update arm `:1242`–`:1249`), `:5359` (`TaskSubmitOptions.ParentTaskID`) |

**A half-rule read as a whole rule is the failure to avoid**: "chain by order"
is an instruction about the *harness translating a model's output*, not a
license to rewrite a human's command line into a chain. A human typing one
`gopi task submit` asked for one row.

---

## 3. The mechanism it leans on — Rule 33's parent gate (as it stands today)

The chain is only a chain because `parent_task_id` is a **gate**, not a note.
Measured at this head, `internal/tools/tasks.go`:

| piece | location | what it says |
|---|---|---|
| the release set | `internal/tools/tasks.go:6471` `taskParentReleaseStatuses` | exactly `{completed, verified}` — every other parent status parks the child |
| the one predicate | `internal/tools/tasks.go:6498` `taskParentGateDenied(parentID, parentStatus)` | the single decision point (verdict **and** reason) |
| the claim funnel | `internal/tools/tasks.go:6616` `taskRowClaimable(...)` | its only caller — the funnel and the pending view cannot disagree |
| the rendered reasons | `internal/tools/tasks.go:6530`–`6532` | the `[queue-skip]` text an operator reads |
| the marker | `internal/tools/tasks.go:6645` `TaskQueueSkipMarker = " [queue-skip]"` | legible, never a silent unclaimable |

The gate's own words (`internal/tools/tasks.go:6465`–`6470`):

> *"a cancelled or failed parent parks its children FOREVER, by design: the
> parked rows are visible in the pending view as `[queue-skip]` with the parent's
> status named, and the operator's resolution is to finish the parent or cancel
> the child."*

**The four arms of the predicate:**

| parent state | claimable? | rendered reason |
|---|---|---|
| `parent_task_id` empty | ✅ yes | — |
| parent row absent (dangling id) | 🔴 parked | ` parent <id> missing from the ledger` |
| parent status ∈ `{completed, verified}` | ✅ yes | — |
| parent status ∉ that set | 🔴 parked | ` parent <id> not released (status=<st>, releases on completed\|verified)` |

**Live, measured in this session** (`gopi task list --status pending`):

```
[queue-skip] parent 01a109bc-47e0-7aef-b240-7865d12a1342 not released (status=pending; releases on completed|verified)
[queue-skip] parent 01a109bc-46c4-7ec0-9a94-9c391a0e25f8 not released (status=pending; releases on completed|verified)
```

**Three design decisions, so nobody "fixes" them later.** There is deliberately
**no override**, **no cascade cancel** and **no timeout/staleness release**. A
`failed` or `cancelled` parent parks its children forever — on purpose. GOPI.md
Rule 33 (line 800) carries the same rule; this document does not restate it as
a second policy, it *uses* it.

**What the gate does NOT govern** (Rule 33's own honest boundary):
`gopi worker <explicit task-id>` still runs a parked row — the gate governs the
DISPATCH funnel, and an operator who typed a row id has made the decision the
gate refuses to a queue.

---

## 4. The clarification that makes the whole thing worth writing down

**Siblings have NO dependency on each other.** `parent_task_id` expresses
exactly ONE edge: child → parent ("the producer runs first"). It says nothing
about the child's relationship to another row that happens to share the same
parent.

**⇒ "N items, all with the same parent" is not serial — it is FULLY PARALLEL.**
The instant that parent reaches `completed`/`verified`, all N children become
claimable in the same dispatch round, in whatever order the dispatcher's
priority sort happens to produce.

**⇒ A chain requires a shape the fan-out does not have:**

```
FAN (siblings — all released at once)        CHAIN (serial — released one at a time)
        root                                     A → B → C → D → E
      /  |  \                                    (B.parent=A, C.parent=B, …)
     A   B   C                                   released: A, then B, then C, …
```

This is the sentence that gets lost. A reader who hears "give every child a
parent" gets the fan and thinks they built a chain.

---

## 5. The gap in this tree — the roadmap fan-out loses the order

`internal/agent/reframe.go:533` `dispatchReframeRoadmap(in reframeInput, rowID string)`
auto-enqueues a TOO_BIG roadmap, one child per item. The loop
(`internal/agent/reframe.go:552`–`556`):

```go
for i, item := range in.Roadmap {
    out, err := tools.TaskSubmit(reframeSubmitOptions(
        reframeChildSummary(item.Goal, i+1, len(in.Roadmap)),
        reframeChildPayload(a.reframeParentSummary(), item),
        rowID, tools.ReframeRoadmapTag, a.sessionIDForTasks(), a.Model, repo))
```

**`rowID` is the same value on every iteration** — the argument comes from
`internal/agent/reframe.go:523` (`a.dispatchReframeRoadmap(in, a.autoTaskRowID)`), never from
the previous child. So every roadmap item is a **sibling of the decomposer**,
and the roadmap's own order — which is exactly the order the model produced —
is discarded by the time the rows are written. The children carry
`tools.ReframeRoadmapTag = "reframe-roadmap"` (`internal/tools/reframe.go:51`),
so this family is identifiable in the ledger.

**The shape it should have produced, per §1:** item 1's parent is the
decomposer row; item N's parent is item N−1.

**What the author of that path *did* get right, and must not be broken by the
fix** — `internal/agent/reframe.go:533`–`551` resolves the children's repo ONCE
(`tools.DurableRepoForSubmit`), because the run's own cwd is its ephemeral
worktree. A chain rewrite must keep passing that durable repo.

**Field evidence that a fan is the wrong shape here.** A worker decomposed one
epic into 5 items; every one landed as a sibling; all five became claimable in
one instant; and the five items edited **the same files** (`cmd/agent/main.go`,
`cmd/agent/cli_verbs.go`, `internal/config/cli_verb_guard.go`) — so parallel
execution could only conflict. The remedy that was actually used was manual
ledger surgery (`UPDATE tasks SET parent_task_id …`) to rebuild the order the
model had already expressed. *(This incident is reported by the reframe
briefing of task `01a10a26`; the directly measured corroboration in this
session is the live `01a109bc` family below — the same 5-item shape, now
holding a real chain.)*

**Measured in this session — the same shape, already chained** (`gopi task
show <id> | grep parent_task_id`, five rows, this head):

```
01a109bc-44b2 …  parent_task_id: 01a109b5-3ec6-…   ← [1/5], parent = the decomposer
01a109bc-46c4 …  parent_task_id: 01a109bc-44b2-…   ← [2/5], parent = [1/5]
01a109bc-4750 …  parent_task_id: 01a109bc-46c4-…   ← [3/5], parent = [2/5]
01a109bc-47e0 …  parent_task_id: 01a109bc-4750-…   ← [4/5], parent = [3/5]
01a109bc-4af2 …  parent_task_id: 01a109bc-47e0-…   ← [5/5], parent = [4/5]
```

and in the pending view exactly **one** of the five carries no `[queue-skip]`
marker while the other four print `[queue-skip] parent … not released`. That is
what the principle looks like when it holds — and it is what the reframe
fan-out does not produce by itself. *(It is also the shape a fix must be
compatible with: these rows were chained by hand, so any automatic chaining
must agree with them rather than re-fan them.)*

---

## 6. The contradiction to name: the `task_submit` tool description teaches the opposite

`internal/agent/loop.go:1540`, the `task_submit` tool's own description, verbatim:

> `"2) the current task is too large — split it: submit each subtask with`
> `parent_task_id=<current task id> and a self-contained payload briefing`
> `(divide-and-conquer; siblings execute serially via gopi task serve); "`

**Two independent defects, both quoted from the same sentence:**

1. **It instructs the opposite of §1.** *"submit each subtask with
   `parent_task_id=<current task id>`"* is precisely the fan shape §4 shows to
   be fully parallel — and then it asserts *"siblings execute serially"*, which
   §4 shows to be false. The model is told to build a fan and promised a chain.
2. **It cites a retired command.** `gopi task serve` is retired; measured in
   this session it prints
   `gopi task serve is a deprecated alias — the merger daemon is RETIRED; a merge pass is the one-shot 'gopi merge'`
   and **exits 1** — never reaching any parser. *(The quoted sentence is the
   record of what it printed when this was measured. It reads differently today:
   the refusal now names `gopi task merge`, because the `gopi merge` it used to
   point at was itself removed by the merge-removal epic. Quoted output is not
   edited — re-measure before quoting it as current.)* The retirement is recorded in
   the tree at `cmd/agent/cli_tasks_worker.go:7`, `cmd/agent/merger_retired.go:31`,
   `cmd/agent/cli_tasks_sync_pass.go:11` and `internal/taskverbs/verbs.go:167`
   (*"reachable ONLY from `gopi task serve`, which the retirement turned into a
   hard refusal"*), and in `devdoc/merger-retired.md`.

So the tool description **contradicts the ruling and points at a command that
cannot run** — the argument for fixing description and behaviour together, in
one task (`01a10a24`). The description's `parent_task_id` parameter itself is
**kept**: an explicit edge is still meaningful; what changes is that *omitting*
it must mean "the harness chains by submit order", not "you are on your own".

---

## 7. The edge is written at INSERT — and, since 2026-10-05, it is movable

At the base this document was drafted against, `parent_task_id` could be set
**only at INSERT**; `gopi task update <id> --parent-task-id X` was refused
(`unknown flag`, submit-only). A wrong edge's only remedy was cancel-and-resubmit,
which mints a new id and breaks every reference to it.

**That has since changed on `master`, and this document was measured at the
merge tip that carries it** — `6e105845 feat(task): `gopi task update
--parent-task-id / --clear-parent` — Rule 33's edge becomes movable`, task
`01a10a18-aff0-73e5-8b2b-25fe3e091256`. Measured here:

```bash
$ gopi task update <id> --parent-task-id zzz-nonexistent-xyz --reason "probe"
gopi task update error: task update: parent_task_id "zzz-nonexistent-xyz" does not exist          # exit 1

$ gopi task update <id> --parent-task-id <itself> --reason probe
gopi task update error: task …: a reparent must carry --reason …                                  # exit 1  (reason arm)
```

Both refusals wrote nothing (the probed row still reads `parent_task_id: —`).
The flags are named once at `cmd/agent/cli_tasks.go:1035`–`1036`
(`parent-task-id` / `clear-parent`), validated at `:1242`–`1252`, and listed in
the verb's flag table at `:2018`; the engine is `TaskReparent`
(`internal/tools/tasks.go:4271`), documented from `:4190`.

**Why it belongs in this document:** it changes *when* the order can be
expressed. Chaining **at INSERT** stays the primary mechanism — it is the only
one that needs nobody's intervention — and reparent is the **repair arm** for
an edge that was decided wrongly, which is exactly the manual `sqlite3 UPDATE`
of §5 turned into a reviewed path. Neither of them is a reason to skip the
chain at submit time.

---

## 8. The price of a chain, stated plainly

Chaining trades parallelism for order, and it is not free. Under Rule 33:

- **A failed or cancelled link parks everything downstream, forever.** If
  `T2` fails, `T3…TN` are unreachable by every dispatcher — deliberately, with
  no override, no cascade and no timeout. The resolutions are the operator's:
  finish/release the parent, cancel the children, or repair the edge (reparent,
  §7).
- **The ruling accepts this explicitly.** *"损失并行只是快慢，而准确性是要命的事情"* —
  latency is tolerable, a wrong premise is not.

A reader who wants parallelism back should be told the honest lever: **express
the items that are genuinely independent as siblings of nothing** (`parent_task_id`
empty on each), rather than weakening the chain.

---

## 9. Cross-references

| task | what it is | state at this head |
|---|---|---|
| `01a10a24-7a8e-7a8b-b420-b90f814ca089` | **the implementation of this principle** — chain the reframe roadmap by order, and rewrite the `task_submit` description (§5, §6) | in_progress |
| `01a10a18-aff0-73e5-8b2b-25fe3e091256` | `task update --parent-task-id / --clear-parent` — the repair arm (§7) | completed, merged (`6e105845`) |
| this task `01a10a26-858e-74c0-8670-9ca05f59aa69` | this document | — |

Related documents, not superseded by this one:

- `GOPI.md` **Rule 33** (line 800) — the gate itself.
- `devdoc/parent-task-claim-gate.md` — the historical record of how the gate
  was built, why a status map, and the `TaskVerifyToReopen` interaction.
- `devdoc/task-reparent.md` — the reparent engine, its six refusals and the
  CAS/transaction shape.

---

## Provenance

- Measured commit: **`da32f3b0`** (this run's HEAD; `master` = `da32f3b0`
  after the delivery-point sync, so every line above resolves on the
  integration branch as well as here).
- Every `file:line` above was re-read at that commit **in this session**; the
  reframe briefing's own line numbers were treated as claims, not facts.
- Commands behind the measured claims:
  `gopi task list --status pending`, `gopi task show <id>`,
  `gopi task update <id> --parent-task-id … --reason …`,
  `gopi task serve` (refusal, exit 1), `gopi doccheck`.
