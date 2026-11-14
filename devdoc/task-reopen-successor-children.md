<!-- doc-class: normative -->
# A successor mint carries the judged row's children — the EDGE moves, the release set never does

> **THE VERB THIS PAGE WAS WRITTEN AGAINST IS RETIRED (2026-10-30).** `gopi task
> reopen` and its engine `TaskReopenWithOptions` were DELETED; the MECHANISM this
> page documents is live and unchanged, and it is now reached through the
> successor door `gopi task clone` (`TaskCloneWithOptions`). Read every
> `gopi task reopen <id>` below as `gopi task clone <id>`, `TaskReopenWithOptions`
> as `TaskCloneWithOptions`, `<id>-rN` as `<id>-cN`, `task_reopen_children_test.go`
> as the rework/clone suites — the `-r1`-minting engine and its four test files
> are gone, so the CRITERIA below are pinned by the suites named here rather than
> by the deleted file. Continuing the SAME row instead of copying it is
> `gopi session resume <id>`.

> Class: normative (mechanism). Related: `devdoc/rework-parent-gate-edge-semantics.md`
> (Rule 33's SECOND criterion — why a `rework` row waits for the parent's VERDICT),
> `devdoc/task-rework-reparent-children.md` (the shared implementation),
> `internal/tools/rework_reparent_children.go` ("THE FIX IS THE EDGE, NEVER THE RELEASE SET").

## The defect, in the operator's words

> "Reopen 有没有去把所有依赖他的任务指向他？如果没有做这件事的话，这个 Reopen 就是不合格的."

`TaskReopenWithOptions` minted `<id>-r1` and left the judged row in its terminal
status — and did nothing about that row's children. Every ordinary child then
carried `parent_task_id = <judged id>`, pointing at a row that can never advance,
so Rule 33 parked it **forever** (no override, no cascade cancel, no timeout):

```
[queue-skip] parent 01a161f3 not released (status=failed)
```

The successor itself was already rescued earlier by the `rework` lineage tag
(commit `be65b35`, `devdoc/rework-parent-gate-edge-semantics.md`) — that patch
saved the successor and no one else, because a *sibling* is not marked.

## What this change is, and what it deliberately is NOT

**The EDGE moves, never the release set.** `parent_task_id: <judged id> →
<successor id>`, through the ONE existing implementation of "the row I just
superseded had children" (`ReworkReparentChildren`) and the ONE writer of that
column (`TaskReparent`: CAS + append-only trail + ghost/self/cycle refusals).
`taskParentReleaseStatuses` is **byte-identical** — verified, see below.

Rejected alternative, already written down in `rework_reparent_children.go`:
adding `failed` to the release set "would release every child of every failed
parent at once" — including the ones whose parent's work genuinely is not done,
which is the ordinary dependency case Rule 33 exists to express.

### The mechanism, end to end

```
gopi task clone <judged> [--reason R] [--repo P]
  └─ TaskCloneWithOptions
       ├─ mint <judged>-c1 (pending, parent=<judged>, tags include `rework`)
       │     the `rework` mark makes the SUCCESSOR claimable although its
       │     parent is failed/cancelled (taskParentTerminalStatuses arm)
       └─ ReworkReparentChildren(<judged>, <successor>-c1, R)
            └─ for each child of <judged>, one TaskReparent:
                 UPDATE tasks SET parent_task_id=<succ> WHERE task_id=<child>
                   AND IFNULL(parent_task_id,'')=<judged>          (CAS)
                 INSERT INTO task_events (child, 'reparented',
                    "reason: R\nold parent_task_id: <judged>
                     \nnew parent_task_id: <succ>")                 (trail)
```

The trail is **per child row**, not one summary line on the judged row: "which
child, from which edge, to which edge, why" is answerable from the ledger alone.

### What does NOT move, by name

| child | why it stays |
|---|---|
| terminal (`completed`/`failed`/`cancelled`/`verified`/`superseded`) | a finished row's dependency record must not be rewritten |
| carrying `reworkHandoffTag` | its work lives on a peer's ledger; that ledger is where its dependencies are read |
| one refusing child (cycle, concurrent move, unreadable row) | the refusal is recorded and the other children still move |

A refusal does **not** fail the mint: the successor is already created and the
rework is wanted either way — but it is **said on the returned line**
(`— rework-child-reparent: <judged> → <succ>: N child(ren) read — moved …, …`),
so "no children" and "children that refused" never render alike (Rule 15).

## The two halves, and the test that pins each

1. **The successor is not parked by the verdict that asked for it.** The
   `rework` lineage tag + `taskParentTerminalStatuses` (Rule 33's second
   criterion). Guard test: `TestReworkParentGateCloneChildIsClaimable`
   (`internal/tools/task_rework_parent_gate_test.go`) — asserts the mark is on
   the row at birth, the predicate releases it, the funnel offers it, the
   operator's `task list` renders it without `[queue-skip]`, **and** that an
   UNMARKED child of the same failed parent stays parked by all three.
2. **The judged row's children follow the successor.** Regression tests
   (`internal/tools/rework_reparent_children_test.go` + the clone suite; the four
   `task_reopen_children_test.go` arms died with the verb, and these are the
   criteria they pinned):
   - "moves every pending child and trails each" — the epic's minimal
     repro: one failed parent, **two** pending children → both point at the
     successor read back off the ledger, each with exactly one `reparented`
     entry naming old and new edge.
   - "leaves terminal children alone" — the half that must not move.
   - "a second successor does not steal moved children" — a **second** mint
     produces `-c2`; the already-moved child stays on the first successor
     (a later successor is a sibling, not the child's new parent), while the
     stale live first successor itself is moved onto `-c2` so two successors of one judged
     row are not left racing.

**Never spell `-c1` in a test.** A second mint produces `-c2`, so the successor
is read back off the ledger (`LIKE '<judged>-%'`) — the suffix is an
implementation detail, the row id is the fact.

## Verification (this change's own numbers)

```
Build:   go build ./...                                                    -> exit 0
Tests:   make test-budget PKGS='./internal/tools'                          -> see report
         go test ./internal/tools -run 'TestTaskReopen…|TestReworkParentGate…'
                                                                          -> all PASS
RED:     the same two regression tests, transplanted onto the base tree
         (git archive c3148def3)                                          -> FAIL
           child t-rpm-a parent after reopen = "t-rpm", want the successor …
           child parent after the SECOND reopen = "t-rp2", want the first successor …
RELEASE SET: diff <(git show c3148def3:internal/tools/tasks.go | sed -n
                   '/^var taskParentReleaseStatuses/,/^}/p')
                  <(sed -n '/^var taskParentReleaseStatuses/,/^}/p' internal/tools/tasks.go)
                                                                          -> empty
             sha256 of the block, both sides
               628d9280a5efc75ad460323d6162dc037466b7ee6f59a514890f41aae9784bb5
GREEN e2e: two private sandboxes, one per binary, same CLI script.
         base binary   : children still on the failed row, 0 `reparented` rows,
                         both children `[queue-skip] … (status=failed)`
         patched binary: `moved 2`, both children re-pointed, 2 trail rows,
                         `[queue-skip] … (status=pending)` — and once the
                         successor reaches `completed`, the marker disappears
                         from both children entirely.
```

## Operating notes (what an operator should know)

- **A successor that dies needs its OWN successor.** Minting from the judged row
  again does not re-parent children of a *failed successor* — they are children
  of the successor, not of the judged row. The composed remedy is
  `gopi task clone <successor-failed> --repo <live work tree>`, which moves the
  successor's own children onto *its* successor.
- **`--repo` still matters.** A successor that inherits a reclaimed
  clone ground mints a row that cannot start; the reissue line says so
  (`carries no deliverable.repo` / `deliverable.repo=…`). This change did not
  touch that axis (`devdoc/task-reopen-repo-axis.md`).
- **`[queue-skip] … (status=pending)` is not the deadlock.** A child parked behind
  a *live* successor is correct — the work really is owed. The deadlock was
  `(status=failed)` with no successor in front of it.
