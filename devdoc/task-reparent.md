<!-- doc-class: normative -->
# Task Report: `gopi task update --parent-task-id / --clear-parent` — Rule 33's edge becomes movable

Task: `01a10a18aff073e58b2b25fe3e091256` · worktree
`/home/user/workspace/gopi-01a10a18aff073e58b2b25fe3e091256` · branch
`task/01a10a18aff073e58b2b25fe3e091256`

## Completed

**The gap this closes.** `parent_task_id` could be set **only at INSERT**.
`taskCreateFields` was its single writer and the generic `taskUpdateStampedFrom`
set-list carries no parent column, so an edge decided at submit time was
permanent. The only remedy for a wrong edge was *cancel and re-submit*, which
mints a new task id and breaks every reference to the old one. Rule 33's gate
was therefore a dependency gate whose edge no operator could repair.

1. **Engine — `TaskReparent(taskID, parentID, reason)`** (`internal/tools/tasks.go`).
   Six refusals, all with zero writes, in this order: an empty row id; a missing
   row; a missing `--reason`; a self-reference; a **ghost parent**; a **cycle**.
   Then an INSERT into `task_events` (`event=reparented`, carrying the reason and
   both ends) plus ONE conditional `UPDATE` — `WHERE task_id = ? AND
   IFNULL(parent_task_id,'') = ?`, pinning the edge every validation was
   computed against — both inside ONE transaction. `RowsAffected == 0` is a
   refusal, never swallowed (Rule 15).

2. **Rule 33 honoured to the letter.** The gate's RELEASE SET
   (`{completed, verified}`), the no-override / no-cascade / no-timeout trio,
   `TaskNext`'s selection and the row's status/summary are untouched. The move
   writes ONE column plus `updated_at` and is deliberately **not** routed through
   `taskUpdateStampedFrom` — so it **consumes no state transition**. That is what
   makes `--parent-task-id X --status Y` mean "set the edge, then transition"
   (measured in acceptance step 8 below).

3. **CLI** (`cmd/agent/cli_tasks.go`). Two flag constants
   (`taskParentTaskIDFlagName`, `taskClearParentFlagName`; Rule 1/13 — the
   admission table, the parse arm, the dispatch arm and the help page all
   reference them, never a literal), both admitted by `taskFlagMember` for
   `update`; the parse arm refuses the mutually-exclusive pair **and** a
   valueless `--parent-task-id` (naming `--clear-parent` rather than guessing);
   the dispatch arm runs after `--repo` and `--payload` and **before `--status`**
   (the briefing's arm order). `--reason` is required by the engine — one
   definition, so the CLI does not re-spell it.

4. **`taskFlagMember`'s `Submit` arm no longer carries a bare literal** for the
   flag it shares with `update`: it was `"parent-task-id"` inline and is now the
   constant. Before this change the constant could not exist because the value
   had one user; now it has two, which is the Rule 14 boundary.

5. **One refusal sentence, two doors.** The ghost-parent message is now
   `taskParentMissingFormat` (`parent_task_id %q does not exist`), shared
   verbatim by `TaskSubmit` (`task_submit: ` prefix) and `TaskReparent`
   (`task update: ` prefix). Asserted by a test that drives **both** faces on one
   ledger, so they cannot drift.

6. **Help surface.** The `task update` synopsis and prose document both flags;
   `devdoc/gopi-task-help-surface-inventory.md` row 7 gains the two flags and the
   two counts (`update` 5 → 7 pairs, total 55 → 57); `COMMANDS.md`'s generated
   block was regenerated **with its own generator** (Rule 38:

   ```bash
   go run internal/standards/testdata/commands-census.go . COMMANDS.md
   ```

   ) and its two stale prose line-references were re-pointed (`cli_tasks.go:734`
   → `:312`, the flag became stale because *my* insertion moved the anchor;
   `cli_session.go:194` → `:193`, which was **already** stale at pristine
   `HEAD` — see Not Done 1).

7. **Tests** — `internal/tools/tasks_reparent_test.go` (11 functions),
   `cmd/agent/cli_tasks_reparent_cli_test.go` (2, through the real binary),
   `internal/tools/tasks_reparent_sync_boundary_test.go` (1 — since repurposed:
   it now pins that the edge TRAVELS, see below), and
   `internal/tools/memory_sync_task_export_wire_test.go` (4, the v1 file
   channel's edge). Ledgers come from
   the scaffold (`testutil.NewTestDBPath`) and the CLI tests from `g1NewSandbox`
   — never a hand-rolled path, never the operator's profile (Rule 32).

## Not Done (honest)

1. **`COMMANDS.md`'s line-reference gate was ALREADY RED at pristine `HEAD`**,
   on a reference my change does not touch:
   `TestCommandsDocAnchorsResolve` reported `cmd/agent/cli_session.go:178
   (points at a BLANK line)`. Measured by stashing my whole diff and re-running
   that one test (it failed identically), then reading the line in the untouched
   main repo. I re-pointed it to `:193` so the gate goes green in this branch —
   that is a **repair of a pre-existing defect**, not a consequence of this work,
   and the fix belongs on `master` independently of this branch's merge.

2. **`internal/stylegate` is red on this tree, on sites my diff does not
   touch**: 6 `test-sleep` sites (`cmd/agent/chat_notice_turn_busy_test.go`,
   `internal/lineedit/keysource_test.go`, `internal/scratch/declared_session_legacy_test.go`,
   `testsuite/chat_notice_turn_pty_test.go`) and 2 `test-setenv` sites
   (`cmd/agent/cli_checkpoint_root_channel_test.go`,
   `internal/tools/memory_sync_row_test.go`). Every one is inherited: each file
   is unmodified in my diff (`git diff --name-only`), which a reader can check
   directly. My diff introduces neither shape.

3. **`internal/tools/tasks.go` is not `gofmt`-clean, and was not before this
   change either** — the file carries 11 pre-existing `gofmt -d` hunks, and the
   count is **identical at pristine `HEAD`** (measured: 11 → 11). Reformatting
   the file would be the whole-file rewrite Rule 4 forbids, and would bury this
   diff; the reformat is a task of its own.

4. **The pinned CAS's `RowsAffected == 0` arm is not observed firing.** See
   "the CAS: what is measured and what is argued" below. The arm is a
   statement-level guarantee that stays correct if the DSN changes or a foreign
   writer that does not take the transaction's write lock touches the row; on
   this DSN it is a backstop behind a measured serialization, and the test says
   so rather than claiming the arm ran.

5. **The `reparented` trail event does not render on `task show`'s `result:`
   line.** It is a member of `contentRevisionEventNames`, rendering its detail
   there — old edge, new edge, reason — would bury the run digest the close-out
   chain and the report seam read, exactly as the payload revision's own entry
   does. The fact is untouched: the trail is queryable (`task_events`, and the
   tests read it) and the edge that is *in force* is the `parent_task_id` column.
   This is a read-side selection, never a deletion (Rule 15).

6. **`TaskReparent`'s `task update: ` prefix is doubled on the parse-time
   refusals** — `gopi task update: task update: --parent-task-id and
   --clear-parent are mutually exclusive`. The doubling is the **file's existing
   convention** (measured on an untouched arm: `gopi task update t-a` prints
   `gopi task update: task update: nothing to update (… --parent-task-id or
   --clear-parent)`), so my two new messages match their neighbours rather than
   diverging alone. Cosmetic; the fix is a sweep of every message in the file,
   not a second style introduced by this change.

7. **The cross-machine boundary WAS measured and is no longer a gap** — see
   below. `parent_task_id` / `parent_task_ids` now travel through the v1 file
   channel (fixed 2026-11-04, `internal/tools/memory_sync_task.go`): the
   receiving node materialises the imported row WITH its edge, and its own
   `taskRowClaimable` parks the child.

8. **The whole-package sweep was run to completion and reported ONE failure —
   not mine, and not a regression.** `make test-budget PKGS='./internal/tools
   ./cmd/agent'` (job-1) ended `exit=1`, `1150s`, summary `2 finished, 1 with a
   failure → FAILED: github.com/nick/gopi/cmd/agent`; the single failing
   case is `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--tools`
   (`chat_argv_test.go:627: runChat([--tools -h]) = 1, want 0`, stderr
   `write |1: bad file descriptor`) — the long-standing argv-fixture
   descriptor flake, **under whole-package load only**: re-run alone it is
   `ok`, and `-count=5` alone it is `ok 2.115s`. `cmd/agent/chat_argv_test.go`
   is **not in this diff** (`git diff master...HEAD --name-only` does not list
   it), and the defect already has its own live row —
   `01a1077a-dcc6-78c1-a119-aab23e0141ab` (`in_progress`), which is exactly this
   symptom. So: **every surface this change touches is green under the
   load-serialized run; the one red is an inherited flake with its own owner**,
   and the honest boundary is that the two-package sweep does not read all-green
   on this tree for a reason this branch did not cause.

9. **A SECOND whole-package sweep, this one at the final `HEAD` (`b59e6b84`),
   came back with a DIFFERENT single red — also not mine, and now dispatched.**
   `make test-budget PKGS='./internal/tools ./cmd/agent'` (job-2) → `exit=1`,
   `910s`, load `15.18 14.81 10.74 → 8.50 11.04 11.40` (4 cores), `2 finished,
   1 with a failure → FAILED: github.com/nick/gopi/cmd/agent`, whose single red
   is `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (22.06s)`.
   The argv flake of item 8 did **not** appear this time, and item 8's red did
   not recur — the two measurements are separate observations, not one story.
   The failing arm is the `ctrl-c closeout` wait
   (`chat_surface_bound_e2e_test.go:1274`): "the REPL did not finish within 20s
   after the injected ctrl-c; the goroutine is still running and the mock served
   45 model round(s)" — and the session then exits normally a moment later
   (`bye.` / `ended (normal)`), which is the point: the run is **progressing**,
   the fixed `boundWait = 20 * time.Second` (line 73) simply expires first under
   contention. Alone the case is green and stable — `-count=3` → **3/3 PASS**
   (`ok 2.934s`). `cmd/agent/chat_surface_bound_e2e_test.go` is **not in this
   diff** (`git log 6e105845~1..HEAD -- <file>` is empty; the file is byte-identical
   to this branch's merge-base `3675bb44`). This is the **same family** as the
   already-`completed` rows `01a0f7d6` / `01a0f810` (whose "ctrl-c closeout
   21.59s" is this same 20s overshoot) / `01a0f871` / `01a0f86c` / `01a0f8ae` —
   i.e. it survived those fixes, so the wait itself was arguably never the thing
   they changed. Rather than smooth it over, it is filed as a **new row**,
   `01a10a45-f3b1-7cdb-9d5d-b237dab80dc2` (tags `bug`, pending), carrying the run
   stream path, the load line, the case's own log lines and the 3×-alone green
   result as an attachment — with "do not widen the constant to make the probe
   succeed" written into its acceptance. **Not Done** remains the honest status of
   the sweep on this tree: both two-package runs are red, on two different
   inherited cases, neither in this diff.

---

## The CAS: what is measured and what is argued

This is the honest split, because "we used a CAS" is easy to say and harder to
observe.

**Measured.** `_txlock=immediate` on the tasks DSN means the transaction takes
the write lock at `BEGIN`, so a second mover queues on `busy_timeout` before it
can read the chain. `TestTaskReparentConcurrentMoversKeepTheLedgerConsistent`
starts four movers together on one row's edge and asserts two facts about the
LEDGER (not about which goroutine won): every successful move left exactly one
trail entry, and the trail count equals the number of successful calls — so the
column and the trail never diverge. It also asserts that the only failure it
tolerates is the named CAS arm; any other error fails the test. **On this DSN
the CAS arm never fires**, and the test's comment says so.

**Argued, not measured.** The CAS's `WHERE task_id = ? AND
IFNULL(parent_task_id,'') = ?` is what keeps the move correct if that
serialization is ever removed — a changed DSN, a direct `database/sql` writer,
another process on a shared file. Under those conditions a loser's stale cycle
check would otherwise certify an edge derived from a tree that no longer exists.
That is a **measured fact about a different question**: I am claiming the
statement is correct, not that I watched it refuse.

**Bounded walk.** The cycle walk's visited-set is the primary termination guard;
`taskReparentWalkMax` (512) is the backstop that refuses a chain deeper than any
tree this ledger can hold, rather than reading an unbounded walk as "no cycle".

## The cross-machine boundary — MEASURED, and since 2026-11-04 the edge TRAVELS

**The boundary this section used to describe was a defect, not a design.** Until
2026-11-04 the v1 file channel lost the row's whole dependency edge, in two
halves that each looked complete on its own:

- the export's `SELECT` named no parent column and the scan had no destination
  for one, so a patch row carried **no parent key at all** (`SyncTaskRow`'s
  json tags are `omitempty`); and
- `importTaskInsertTx` was called without the parent fields, though its own
  `INSERT` already names both columns.

The observable result: a child imported onto a receiving ledger with **both
parent columns empty** — a row that looks parentless and is claimable at once
while its parent sits pending beside it — with `exit=0`, `refused=0` and an
empty stderr. No counter, no note, no log.

**What is measured now** (`TestFileChannelCarriesTheDependencyEdge`,
`TestFileChannelAdoptsTheEdgeOntoAnExistingRow`,
`TestFileChannelRefusesAnUnhonourableEdge`, and the repurposed
`TestTaskReparentEdgeTravelsThroughTheFileTransport`): the export's projection
is the named constant `syncTaskExportColumns`, which carries the parent
expressions **from the shared spellings** the delta export and the peer-side
projection already use (`syncParentColumnExpr` / `syncParentIDsColumnExpr`,
Rule 14 — one edge, three transports); the import hands the parents to
`importTaskInsertTx` through `taskParentsForWrite` (parent 1 → `parent_task_id`,
parents 2..N → `parent_task_ids`); and the receiving ledger's **own**
claimability predicate (`taskRowClaimable`) reads the edge, which is the fact
that matters — a value in a column is not a dependency.

**The repair arm, in BOTH shapes.** A row that arrived while the channel carried
no parent column already exists on the receiver, so the insert path never runs
for it. `importTaskAdoptParentsTx` repairs it in the two shapes a parent SET can
take — and "already has a parent" is not "is current":

| local row | peer row | action | counter |
|---|---|---|---|
| no parent | has parents | adopt the whole set | `RowsParented` |
| `{P1}` | `{P1, P2}` (superset) | append the missing edges, local ORDER kept | `RowsParentsWidened` |
| `{P1}` | `{P1}` / subset | write nothing — a stale peer never takes an edge away | — |
| `{P1}` | `{P2}` (disjoint) | count and REPORT, never silent-union | `RowsParentsConflict` |

The second row is the same defect one level down and it was live until
2026-11-04: a child carrying `P1` whose peer copy carries `{P1, P2}` released the
moment `P1` finished and stayed **claimable while `P2` was still live** — with no
error, no counter, and no peer to re-send it, because the row already looked
parented. The classification is not re-derived here: it is
`syncDeltaParentSetRelation` / `syncParentSetUnion`, the pair the DELTA channel
uses (it closed its own version of this hole in `de9b7f2d`), so the two
transports cannot disagree about what "new edge" means (Rule 14). A disjoint pair
is reported rather than unioned because neither node declared that pair, and
landing a union would be this leg inventing a dependency graph.

Both counters print on the import's counted line, as a SUFFIX present only when
they fire — `… refused=0 parented=1 widened=1` — because a repair that prints
identically to a no-op import is a silent failure wearing a success.

Without this arm the fix would only help rows the receiver had never seen —
precisely not the ones the defect corrupted.

**The refusal arm.** An edge this ledger cannot honour is refused **by name**
through `syncParentVerdict` (dangling parent, or a verdict that does not
release), and the row is not written at all: Rule 33 parks such a row forever,
so "the edge lands" and "the row is refused" are the only two honest outcomes.

**What this still does not say**: nothing here claims the LOCAL write is
unsynced — that was never true. `updated_at` moves, so the row's ANCHOR travels
and the remote replica adopts the row; the point of the fix is that the EDGE now
travels with it.

Verified end to end on the real CLI (two sandbox profiles, `--export-tasks` →
`--import-tasks`): the patch rows carry `parent_task_id` / `parent_task_ids`, the
receiving ledger reads both columns back identically, and
`task list --status pending` marks both children `[queue-skip]` naming every
unreleased parent.

## Verification

**Build**: `go build ./...` → OK (exit 0). `CGO_ENABLED=0 go build -o bin/gopi
./cmd/agent/` → `bin/gopi`, 30,522,091 bytes.

**Tests**:

```bash
go test -count=1 -run 'TestTaskReparent' ./internal/tools/ ./cmd/agent/     # ok, 0.620s / 2.301s
go test -count=1 -run 'TestTaskReparent|TestParentGate|TestTaskUpdate|TestSetPayload|TestTaskVerb' ./internal/tools/   # ok
go test -count=1 -run 'TestTaskReparent|TestTaskUpdate|TestTaskHelpSurface|TestTaskVerb' ./cmd/agent/                  # ok, 8.070s
go test -race -count=1 -run 'TestTaskReparent|TestParentGate' ./internal/tools/                                        # ok, 35.505s
go test -count=1 -run 'TestCommandsDoc' ./internal/standards/                                                          # ok, 16.812s
```

**Red → green proof** (Rule 10.5). Disabling the cycle arm and re-running:

```
--- FAIL: TestTaskReparentRefusesCycles (0.21s)
    tasks_reparent_test.go:193: reparenting the root onto its own descendant must be refused
```

Restoring it: `--- PASS: TestTaskReparentRefusesCycles (0.27s)`.

**Ran** — the real CLI on a **sandbox ledger** (Rule 34; `gopi sandbox create
--label reparent`, `GOPI_HOME` into it, so no probe touches a real ledger):

```
$ ./bin/gopi sandbox create --label reparent --task-id 01a10a18aff073e58b2b25fe3e091256
sandbox …/gopi-sandbox/sandbox-reparent-92ac50fc created=true … ledger=…/sandbox-reparent-92ac50fc/.gopi/memory.db
```

1. chain built:
   ```
   Task queued: t-a (status=pending, source=agent, tags=—)
   Task queued: t-b (status=pending, source=agent, tags=—)
   Task queued: t-c (status=pending, source=agent, tags=—)
   ```
2. the gate reads the edge:
   ```
   #t-c  [pending]  consumer c … [queue-skip] parent t-b not released (status=pending; releases on completed|verified)
   #t-b  [pending]  producer b … [queue-skip] parent t-a not released (status=pending; releases on completed|verified)
   #t-a  [pending]  producer a … [next]
   ```
3. reparent:
   ```
   $ ./bin/gopi task update t-c --parent-task-id t-a --reason "t-b is not the producer any more"
   Task parent updated: t-c (parent_task_id t-b -> t-a)      exit=0
   ```
   `task show t-c` → `parent_task_id: t-a`; the gate line becomes
   `#t-c … [queue-skip] parent t-a not released`.
4. the four refusals, each `exit=1`:
   ```
   task: t-a: refusing to reparent onto t-c — it would close a dependency cycle: t-c -> t-a. Rule 33's gate requires a DAG: every row on a ring would park forever. …
   task: t-b: refusing to make the row its own parent — a self-referencing edge parks the row forever (Rule 33 reads the parent's status from the row itself), which is a cycle of one.
   task update: parent_task_id "t-doesnotexist" does not exist
   gopi task update: task update: --parent-task-id and --clear-parent are mutually exclusive — …
   task: t-c: a reparent must carry --reason — the trail records WHY the dependency edge moved, …
   ```
   **Nothing was written**: `t-a parent=—  t-b parent=t-a  t-c parent=t-a` afterwards.
5. `--clear-parent` restores `[next]`:
   ```
   Task parent updated: t-c (parent_task_id t-a -> (none))     exit=0
   #t-c  [pending]  consumer c …            ← no [queue-skip]
   ```
6. **edge + transition in ONE invocation, in arm order**:
   ```
   $ ./bin/gopi task update t-c --parent-task-id t-a --reason "…" --status claimed
   Task parent updated: t-c (parent_task_id (none) -> t-a)
   Task updated: t-c (status=claimed, reason=combined: edge then transition)     exit=0
   ```
   and the REFUSAL arm:
   ```
   $ ./bin/gopi task update t-a --parent-task-id t-c --reason "cycle + status" --status claimed
   exit=1 … refusing to reparent onto t-c — it would close a dependency cycle …
   # task show t-a → status: pending, parent_task_id: —    ← the transition was NOT consumed
   ```
7. FTS follows the edge: after `t-b --parent-task-id t-a`,
   `./bin/gopi task search t-a` → `2 task(s) matching "t-a"` (t-a, **t-b**).

**Result**: every arm behaves as claimed; all four refusals exit 1 with the
ledger unchanged; the combined invocation runs the edge first and still lands
the transition, and a refused edge leaves the status alone.

**Commit**: `6e105845` — the engine (`internal/tools/tasks.go`), the CLI
(`cmd/agent/cli_tasks.go`) and the three new test files, in one change.
`05ef67f5` — this report. `b6fe417c` — its `devdoc/DOC-INDEX.tsv` row
(Rule 36: the `write` tool timed out after the bytes landed on this file, so
the hook never ran and the classification is landed by hand, in its own commit
naming that cause).

### Re-run on a second, fresh sandbox (closing checkpoint)

The acceptance above was re-executed end to end on a **second** sandbox
(`sandbox-reparent-verify-f49ec597`) against the committed binary, so the
record does not rest on one environment:

```
$ ./bin/gopi task update B --parent-task-id A --reason "b consumes a"   exit=0
Task parent updated: B (parent_task_id (none) -> A)
$ ./bin/gopi task list --status pending
#C  [pending]  consumer c  [queue-skip] parent B not released (status=pending; …)
#B  [pending]  producer b  [queue-skip] parent A not released (status=pending; …)
#A  [pending]  producer a  [next]
$ ./bin/gopi task update C --clear-parent --reason "c no longer depends on anything"   exit=0
Task parent updated: C (parent_task_id A -> (none))     # and C's [queue-skip] is gone
$ ./bin/gopi task update C --parent-task-id B --reason "c consumes b again" --status claimed   exit=0
Task parent updated: C (parent_task_id (none) -> B)
Task updated: C (status=claimed, reason=c consumes b again)     # → task show: claimed / parent B
```

The refusals, and the zero-write property measured against them:

```
$ ./bin/gopi task update A --parent-task-id C --reason "cycle attempt"      exit=1
gopi task update error: task: A: refusing to reparent onto C — it would close a dependency cycle:
C -> B -> A. Rule 33's gate requires a DAG: every row on a ring would park forever. …
$ ./bin/gopi task update B --parent-task-id B --reason "self"               exit=1
gopi task update error: task: B: refusing to make the row its own parent — …
$ ./bin/gopi task update B --parent-task-id t-doesnotexist --reason "ghost" exit=1
gopi task update error: task update: parent_task_id "t-doesnotexist" does not exist
$ ./bin/gopi task update B --parent-task-id A --clear-parent --reason "both" exit=1
gopi task update: task update: --parent-task-id and --clear-parent are mutually exclusive — …
$ ./bin/gopi task update B --parent-task-id "" --reason "empty"             exit=1
gopi task update: task update: --parent-task-id needs a task id; use --clear-parent to remove the edge
$ ./bin/gopi task update B --parent-task-id A                             exit=1
gopi task update error: task: B: a reparent must carry --reason — …
```

`task show` on all three afterwards: `A pending —`, `B pending A`,
`C pending A` — **nothing moved**, and the refused combined invocation left
`A status: pending, parent_task_id: —` (the transition was not consumed).
**Two things this re-run added to the record**: the empty-value arm (the flag
present with no id is refused at parse, not silently treated as `--clear-parent`),
and the FTS arm re-measured — `task search <B>` returns **2** rows (B and its
child C), i.e. the external-content index does follow the rewritten edge.

## Evidence (self-filed)

- `go build ./...` → exit 0; `gofmt -l` on my three new test files → empty.
- `go test -run 'TestTaskReparent' ./internal/tools/ ./cmd/agent/` → `ok` both.
- Cycle arm disabled → `--- FAIL: TestTaskReparentRefusesCycles (0.21s)`; restored
  → `--- PASS (0.27s)`. Both runs pasted above.
- `go test -race -run 'TestTaskReparent|TestParentGate' ./internal/tools/` → `ok 35.505s`.
- `go test -run TestCommandsDoc ./internal/standards/` → `ok 16.812s` (was red
  before the `COMMANDS.md` re-point).
- `git diff --name-only` → `COMMANDS.md`, `cmd/agent/cli_tasks.go`,
  `devdoc/gopi-task-help-surface-inventory.md`, `internal/tools/tasks.go` (+3
  untracked test files). Every stylegate-flagged site is in a file that list
  does not contain: checked per-file with
  `git diff --quiet -- <file> && echo UNMODIFIED-INHERITED`.
- `gofmt -d internal/tools/tasks.go | grep -c '^+'` → 11; the same command on
  `git show HEAD:internal/tools/tasks.go` → 11. Pre-existing, unchanged.
- The sandboxes created for the acceptance runs are private and off `/`:
  `/home/user/workspace/gopi-sandbox/sandbox-reparent-92ac50fc/.gopi/memory.db`
  and `…/sandbox-reparent-verify-f49ec597/.gopi/memory.db` (mode `0600`, 0 bytes
  before their first open).
