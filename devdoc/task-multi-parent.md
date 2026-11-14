<!-- doc-class: normative -->
# Multi-parent dependencies — a row waits for a SET of producers, stored so an older reader still sees one

> Class: design note (the wire shape, and why parent 1 keeps its own column).
> Related: `devdoc/parent-task-claim-gate.md` (Rule 33's gate itself),
> `devdoc/task-parent-release-statuses.md` (the release set),
> `devdoc/rework-parent-gate-edge-semantics.md` (the second edge criterion),
> `devdoc/task-reparent.md` (the update face).

## 1. What was broken

A row could depend on exactly ONE producer. `parent_task_id` is a single column,
`TaskSubmitOptions.ParentTaskID` a single string, and the claim gate ANDed over
one id. There was no way to express "this row needs both A and B" — the only
available approximations were both wrong:

- **chain them** (child depends on B, B depends on A): that makes B *wait* for A,
  which is a claim about B's inputs that the caller never made, and it serialises
  two producers that could have run in parallel;
- **depend on one and hope**: the child gets dispatched while its second input is
  still being produced.

And the CLI made it worse in a way that took a measurement to see (see §5): the
flag was not repeatable, so `--parent-task-id A --parent-task-id B` exited 0 and
stored only **B**.

## 2. The wire shape, and the one thing it optimises for

There is **no new message type and no new table**. The set is split across two
columns of the existing row:

| column | holds |
|---|---|
| `parent_task_id` (unchanged) | **parent 1** — the first parent typed |
| `parent_task_ids` (new, `TEXT DEFAULT ''`) | **parents 2..N**, comma-joined, in typed order |

The split is the whole design. A single-parent row is **byte-identical to
what every release before this change wrote** — `taskParentsForWrite` puts the
only parent in the old column and leaves the new one empty — so:

- an **older gopi on another node** (a peer ledger, a sync leg, a `task show` from
  a stale binary) still sees a dependency instead of a free row;
- a **newer gopi reading a historical row** sees exactly one parent, because the
  reader is one function that takes the old field first and then the list
  (`taskParentIDsOf`);
- no migration has to rewrite data: the new column is added idempotently
  (`tasksP2Columns`) and defaults to the empty string, which reads as "no
  additional parents".

The alternative — a join table, or JSON in one column — was rejected for the
same reason: a join table makes every existing reader wrong *at once* (it reads a
row with no `parent_task_id` and treats a parked row as claimable), and JSON in
one column makes the single-parent case stop being byte-identical. The cost of
this shape is that the set is stored in two places, which is paid by having
**exactly one reader** (`taskParentIDsOf`) and **exactly one writer**
(`taskParentsForWrite` / `TaskParentIDsJoin`) — Rule 13's discipline, and the
reason the tests assert on the two RAW cells rather than on the parsed list.

## 3. What the gate does now

Rule 33's gate is unchanged in every respect except one: it ANDs over the set.

```go
denied, reason := taskParentGateDenied(rowTags, parents, parentStatus)
```

- **The release set is untouched** — `completed | superseded | verified`, exactly
  as before. Widening it to make multi-parent rows flow more easily would have
  changed the single-parent semantics, which is the thing this design protects.
- **No override, no cascade, no timeout** — still. A cancelled or failed parent
  parks its children forever, by design, and rule 33's operative answer (finish
  the parent, or cancel the child) is unchanged.
- **The verdict names EVERY blocking parent**, so a row that waits on three
  producers and has one released says which two remain. `task show` renders one
  `parent_task_id:` line as always, and — only when there is more than one
  parent — a `parent_task_ids:` line plus a per-parent status list, so the
  single-parent rendering is unchanged down to the line count.

Consequences worth stating, because they are the reason to do this at all:

- a **dangling SECOND parent is refused by name** on both faces (submit and
  reparent). A silently dropped parent is a row that claims to wait for one
  producer while it actually waits for two, and Rule 33 has no repair path for
  the row it would un-park;
- the row is claimable **iff every parent is released**, which is what
  `[queue-skip]` reports, and the marker and the claim funnel read the same
  predicate (`taskRowClaimable`).

## 4. The cycle check had to become a graph search

The reparent path refuses a move that would close a ring, and its walk followed
`parent_task_id` **only**. With a set, a ring can close through parent 2 just as
easily — and that walk would not have seen it, so the move would have been
written and then parked forever (no override, no cascade, no timeout: the ring
would be unrepairable through the ledger).

So the walk is now a search over **every parent of every row it visits**, through
the same `taskParentIDsOf` the gate uses, so the check and the gate cannot
disagree about what a row's parents are. Two properties are preserved
deliberately:

- the **refusal renders ONE chain** (each search root carries its own path), so
  the message never prints an edge between two branches that do not exist;
- the **already-broken-ledger arm is unchanged** — a ledger whose graph already
  contains a cycle is still a refusal, not an attempted repair.

`TestCycleThroughTheSecondParentIsRefused` is the arm the old walk could not see:
the ring closes only via a second parent, and the move must be refused with the
row untouched.

## 5. Two defects the FIRST real invocation found (Rule 31's whole point)

Build and unit tests were green, and both of these were found by running the
binary on a real ledger with real arguments:

### 5a. `slice bounds out of range [1:0]` — a panic on the ordinary case

The dispatch arm passed `opts.parentTaskIDs[1:]` to `TaskSubmit`, which is the
`parents 2..N` slice. On a **zero-length** slice that is a panic — and a
zero-length list is the ordinary `gopi task submit "..."` with no parent flag at
all:

```
$ ./bin/gopi task submit "parent A" --payload "briefing A" --task-id 0199bbbb-…000a --repo … --db …
panic: runtime error: slice bounds out of range [1:0]
        main.runTaskSubmit(0xc000342a88)
                /home/user/workspace/01a1a60e-7c67-791f-aa81-6c7a1b0e095e/cmd/agent/cli_tasks.go:6280 +0x7fd
```

The fix is `multiParentsRest`, which returns nil for a list shorter than two
parents; `TestSubmitWithoutParentsDoesNotSliceAPanic` pins both ends and the case
the panic was measured on. **Every `gopi task submit` in the project would have
crashed**, and no unit test in this tree covered the dispatch arm's slice — the
tests exercised `TaskSubmit`, not the CLI that calls it.

### 5b. The sync window dropped parents 2..N

`syncRemoteCoalescedProjection` (the harvest window's column list) named the old
parent column and not the new one, so a row reconstructed from a peer lost every
parent after the first: the edge silently shrank to one parent, which is exactly
the loss the split exists to avoid. Caught by
`TestHarvestRemoteSelectNamesEveryColumnForTheWireStruct`, which compares the
select's column NAMES against the wire struct's fields — the guard that makes a
column addition impossible to forget. Fixed by naming both columns through their
paired constants (`syncParentColumnNamed`, `syncParentIDsColumnNamed`).

## 6. The CLI face

`--parent-task-id` is **repeatable** on `submit` and `update`, and the order typed
is the order stored (parent 1 is the first occurrence, which is the column an
older reader sees). `update`'s list **replaces** the row's whole parent set, and
`--clear-parent` clears every parent. `--parent-task-id` and `--clear-parent`
remain mutually exclusive.

The measured defect this replaced: the flag was not declared repeatable, so the
flag set's last-wins rule dropped the first occurrence — `submit --parent-task-id
A --parent-task-id B` exited 0 and stored only B, with no error anywhere.
`TestSubmitKeepsEveryParentTaskIDFlag` and `TestUpdateKeepsEveryParentTaskIDFlag`
are that regression guard, and `TestFlagTableAdmitsTheParentFlagOnBothVerbs`
covers the other half (an advertised flag the admission table refuses exits with
"unknown flag").

## 7. Verification

The tests are the evidence, and they are all on the real surfaces (Rule 32: every
ledger comes from the scaffold):

| file | pins |
|---|---|
| `internal/tools/task_multi_parent_test.go` | the reader/writer split, the two raw cells, the AND gate, the dangling second parent on both faces, the reparent-before-write ordering, the cycle through parent 2 |
| `cmd/agent/cli_tasks_multi_parent_test.go` | repeatable flag on both verbs, typed order, empty value refused, mutual exclusion, the flag table, and the no-parents panic |

The green run on a real ledger (`0199bbbb-…` ids, a `--db` outside the operator's
profile):

```
$ ./bin/gopi task submit "child of both" --payload … --task-id …c --repo /home/user/work/gopi \
    --parent-task-id …a --parent-task-id …b --db "$D"
Task queued: 0199bbbb-0000-7000-8000-00000000000c (status=pending, source=agent, tags=—)

$ ./bin/gopi task list --status pending --db "$D"
#…c  [pending]  child of both  … [queue-skip] parent …a not released (status=pending; …); parent …b not released (…)
#…b  [pending]  parent B  …
#…a  [pending]  parent A  … [next]

# release ONLY parent A
… parent A -> status: completed
$ ./bin/gopi task list --status pending --db "$D"
#…c  [pending]  child of both  … [queue-skip] parent …b not released (status=pending; …)

# release parent B too
$ ./bin/gopi task list --status pending --db "$D"
#…c  [pending]  child of both  … [next]
```

`[next]` moving onto the child only when the LAST parent is released is the
whole epic in one line of output. A dangling second parent is refused by name:

```
$ ./bin/gopi task submit "bad child" … --parent-task-id …a --parent-task-id …009 --db "$D"
exit=1
[stderr] gopi task submit error: task_submit: parent_task_id "0199bbbb-0000-7000-8000-000000000009" does not exist
```
