<!-- doc-class: historical -->
# The advertised transition the storage refused — `superseded` vs a stale status CHECK

**Epic**: `01a12f76-4917-7e38-bcda-275baf52466b`
**Branch**: `task/01a12f7649177e38bcda275baf52466b`
**Fix commit**: `6105c376d`
**Status**: fixed, gated, reproduced on the real surface

## 1. The reported defect

`gopi task update <pending-row> --status superseded --reason x` answered:

```
gopi task update error: task: update: constraint failed:
  CHECK constraint failed: status IN
  ('pending','claimed','in_progress','completed','failed','cancelled','verified','reopened') (275)
```

— while the SAME command's other error message advertises that very value:

```
gopi task update error: task: illegal status transition pending → completed
  (allowed from pending: claimed|in_progress|cancelled|superseded)
```

So the remedy the tool prints for a refused transition is itself refused. The
caller has to guess a second status (`cancelled`), which is what the reporting
run did — the outcome was right, the advertised path was a dead end.

## 2. Root cause (5-why)

1. **Why was the write refused?** The persisted `tasks.status` CHECK enumerated
   eight statuses; `superseded` was not among them.
2. **Why was it not among them?** The live ledger's table was created when the
   vocabulary held eight values, and SQLite cannot `ALTER` a `CHECK` — the only
   remedy is the rename→recreate→copy→drop rebuild.
3. **Why did the rebuild not run?** `tasksNeedsRebuild` — the ONE arming
   predicate for that rebuild — asked the DDL only about `pending` and
   `verified` (plus a legacy four-value list). Both were PRESENT on this
   ledger, so the probe answered "no rebuild needed" and the stale CHECK
   survived the open.
4. **Why was the probe a hand-picked pair?** It was written for the two
   migrations that existed when it was authored (P2 added `pending`, P2.5 added
   `verified`) — a probe list that enumerates the migrations somebody remembered
   rather than the set the Go vocabulary can write.
5. **Why did the divergence survive the epic that created it?** Commit
   `7c32e88f4` ("`superseded` — a terminal status that releases the children it
   was blocking", 2026-10-07) updated the whole vocabulary —
   `taskStatusSet`, `taskAllowedTransitions`, `taskStatusRank`,
   `taskTerminalStatusSQLList`, `taskParentReleaseStatuses`, eleven explicit
   per-status consumer sites — and did **not** touch `tasksNeedsRebuild`
   (measured: `git show 7c32e88f4 -- internal/tools/tasks.go | grep -c
   tasksNeedsRebuild` → `0`). Nothing in the tree related the two: adding a
   status to the set is a declaration, arming the rebuild is a probe, and only a
   human reading both could notice they had drifted.

**The mechanism in one sentence**: the status vocabulary has a single source of
truth (`taskStatusSet`), the schema CHECK is DERIVED from it (`taskSchemaStatusCHECK`),
and the migration probe was NOT — so the probe's coverage was exactly the set of
statuses its last author remembered, and every later status silently inherited a
rejected write path.

**Measured scale.** This is not a single ledger: every ledger created between
P2.5 (`verified`'s arrival) and `7c32e88f4` carries the eight-value CHECK and is
in this state. A status synced IN from a peer (a migrated row carrying
`superseded`) would also be refused storage — the test
`TestParentGateMigratedSupersededParentCrossesTheLedgerBoundary` pins that
boundary precisely because it is the cross-ledger arm.

## 3. The fix

`internal/tools/tasks.go`, `tasksNeedsRebuild`:

```go
for _, s := range taskStatusSet {
        if !strings.Contains(ddl, "'"+s+"'") {
                return true, nil // constraint omits a writable status → rebuild from taskStatusSet
        }
}
return false, nil
```

The required set is now the SAME slice `taskSchemaStatusCHECK` renders the
CREATE literal from, walked PER MEMBER: any single missing value arms the
rebuild, and the rebuild re-renders the CHECK from that same slice. The
hand-picked list `tasksStatusesBeforeP2` is **deleted**, not kept: all four of
its members are members of `taskStatusSet`, so the derived walk subsumes its
"anomalous legacy value missing" arm — and keeping a second, now-unreferenced
copy of a status list is the drift hazard this change exists to remove (Rules
1/13: constants referenced, not mirrored; Rule 14: one definition).

The `!strings.Contains(ddl, "CHECK")` and empty-DDL arms are unchanged: a
constraint-free legacy table is still widened by plain `ALTER`, and a ledger
with no `tasks` table has nothing to rebuild.

## 4. Why the set is the right source of truth

`taskStatusSet` is the slice from which `taskSchemaStatusCHECK()` renders
`'pending','claimed',…` — the statement is a two-line function in the same file.
So "the set" and "the CHECK" are one declaration apart by construction, and the
property the gate asserts is exactly that relation, not a coincidence of
spelling.

**The rejected alternative** (worth recording, because it is the obvious one):
make the probe compare the DDL's stored member list against
`taskSchemaStatusCHECK()` textually. Rejected — the DDL's spacing/parens are
whatever the creating binary wrote, so a text compare would arm the rebuild on
COSMETIC drift and rebuild an operator's 245 MB ledger for nothing. Per-member
substring presence is the property that actually matters (can the column hold
this value?), and it is drift-free.

## 5. Verification

### 5.1 Real-surface reproduction, pre-fix binary (RED)

`artifacts/repro/repro.sh <binary> <outdir>` — creates a sandbox (Rule 34), opens
a fresh ledger through a real surface, ages its CHECK to the live 8-status shape,
enqueues a row through `task submit`, then runs the reported command.

```
== fresh CHECK ==
CHECK (status IN ('pending','claimed','in_progress','completed','failed','cancelled','verified','reopened','superseded'))
== aged CHECK (must NOT contain superseded) ==
CHECK (status IN ('pending','claimed','in_progress','completed','failed','cancelled','verified','reopened'))
== update --status superseded ==
gopi task update error: task: update: constraint failed: CHECK constraint failed: status IN
  ('pending','claimed','in_progress','completed','failed','cancelled','verified','reopened') (275)
exit=1
== status after ==
status:         pending
```

This is the reported defect, byte-identical to the original (exit 1, the same
`(275)` code, the row left pending). Evidence: `artifacts/repro/prefix/`.

### 5.2 Real-surface invocation, post-fix binary (GREEN)

Same script, post-fix binary — see §6 for the recorded output.

### 5.3 Tests

`internal/tools/task_status_check_rebuild_test.go`

**Red proof** (pre-fix `tasks.go` in the tree, new test files present), captured
at `artifacts/red-proof.txt`:

```
--- FAIL: TestTaskUpdateSupersededOnPreSupersededCheckLedger (0.06s)
--- FAIL: TestTasksNeedsRebuildCoversEveryWritableStatus (0.30s)
    --- FAIL: …/omitting_claimed (0.03s)
    --- FAIL: …/omitting_reopened (0.02s)
    --- FAIL: …/omitting_superseded (0.02s)
```

The gate failed on **exactly** the three members the hand-picked probe was blind
to — `claimed`, `reopened`, `superseded` — which is the divergence measured from
the other side, and it passed on the two the old list did cover. That
correspondence is the evidence that the test measures the defect and not
something adjacent.

**Green** (post-fix): both tests pass, all nine per-member subtests pass.

**Branch-scoped suite, verbatim verdict** —
`make test-budget PKGS='./internal/tools ./internal/docclass'` → **exit 1,
`outcome=fail`, budget NOT tripped** (730s of 25m, load_end 6.89 11.04 8.49):
```
--- FAIL: TestProsePathLineGateOnTheRealTree          (internal/docclass)
--- FAIL: TestProsePathLineShippedTableSurvivesHeaderChurn (internal/docclass)
```
`internal/tools` → **`pass`**; `internal/docclass`'s **anchor** arm →
`ok … 11.451s`. The two failures are the prose-path-line family, whose one
remaining unpermitted citation belongs to another task (§8.1) — this branch
reports the suite red rather than claiming a green it does not have.

### 5.4 The two hands of the gate

- `TestTaskUpdateSupersededOnPreSupersededCheckLedger` — the **repro**. A ledger
  aged to the live shape (CHECK without `superseded`), then the real write entry
  point `TaskUpdateReason`. Red before, green after, and it asserts the row
  reads back `superseded` with its reason — an accepted write, not a silent
  no-op.
- `TestTasksNeedsRebuildCoversEveryWritableStatus` — the **gate**:
  - **(a) membership**: every source AND target of `taskAllowedTransitions` must
    be a `taskStatusSet` member, and the rendered CHECK must carry every member.
    A status the CLI can name but storage cannot hold fails HERE. This is the
    half that makes a future status addition safe by construction.
  - **(b) per-member omission**: for EVERY member of `taskStatusSet`, aged out
    alone, the real open path (`openTasksDB` → `ensureTasksSchema` →
    `tasksNeedsRebuild` → rebuild) must leave the CHECK admitting it. Not the
    predicate called by hand: the path a run actually takes.

### 5.5 Fixture design — why this shape

The fixture ages the stored DDL **in place** (`PRAGMA writable_schema`,
rewriting only the CHECK member list in `sqlite_master`) rather than renaming the
table and recreating a legacy column set. Reasons, both measured:

- It is the **highest-fidelity** shape available: the table keeps every column
  and every companion object (FTS triggers, indexes) byte for byte; only the
  CHECK text moves. That is exactly how the live ledger diverges — the live
  table is not a *different* table, it is the same table whose CHECK is old.
- The rename/recreate variant couples the test to the legacy column projection,
  so a failure could mean "the fixture's column list is stale" rather than "the
  probe is blind".

**Pitfall found while writing it** (recorded because it cost two red runs and is
invisible while it happens): `PRAGMA writable_schema` is a **per-connection**
setting. Issuing it through `*sql.DB` lets database/sql's pool hand the pragma,
the `UPDATE` and the `schema_version` bump to three DIFFERENT connections — the
UPDATE then runs where the pragma is still OFF and the edit is a **silent
no-op**; the DDL reads back unchanged and the test asserts against a schema it
never built. The fixture now pins ONE connection (`db.Conn`) for the whole edit
and reads the aged text back on that same connection, failing loudly if it did
not persist (Rule 15).

## 6. Post-fix real invocation

See `## Verification` in the task report (`tasks/01a12f7649177e38bcda275baf52466b-report.md`)
for the verbatim invocation and output. Summary: the same aged-ledger fixture,
driven by the post-fix binary, transitions `pending → superseded` with exit 0 and
the row reads back `superseded` — and the probe's repair is observable as the
ledger's CHECK gaining the member on open.

## 7. What this does NOT claim

- **The live ledger is not repaired here.** `~/.gopi/memory.db`'s stale CHECK is
  repaired by the next open of a binary carrying this fix — that is the fix's
  own mechanism, not a separate migration script (Rule 35: no new artifact).
  Nothing in this branch runs against the operator's ledger.
- **A status synced in from a peer is now storable only after the receiving
  ledger opens once with a fixed binary.** Between a peer writing `superseded`
  and this ledger's next open, the sync write is still refused by the old CHECK.
  The window is one open wide, and it is the same window every status addition
  has always had; making it zero would need a migration at the storage layer
  (a persistent artifact), which the fix deliberately does not add.
- **No other consumer was audited in this change.** The last epic
  (`7c32e88f4`) inventoried eleven per-status sites and its report covers them.
  This fix closes the arming predicate only — but the new gate's membership arm
  now covers the transition table exhaustively, so a consumer that reads
  `taskAllowedTransitions` cannot drift silently any more.
- **`taskStatusWritable` is untouched.** It was already correct (it excludes the
  retired `reopened`); the defect was never in the remedy list's membership, it
  was in storage's.

## 8. The edit's own blast radius — a line-count change in `tasks.go` moved a frozen anchor

Recorded because it is the kind of self-inflicted red a branch can carry into a
merge, and because the report's first draft got it **wrong** and had to retract.

`internal/tools/tasks.go` is one of the most-cited files in `devdoc/`: several
manifest rows carry an `anchor internal/tools/tasks.go:NNNN at <text>` cell, and
`internal/docclass/testdata/anchor-number-baseline.tsv` freezes every such row
whose number no longer points at its text. **A change that shifts line numbers in
that file therefore moves the frozen population**, and this fix is +35 net lines
above `:2395` (the rewrite sits at `:921`–`:1000`).

Measured, after commit `6105c376d`:

```
--- FAIL: TestAnchorNumberGateOnTheRealTree
    anchor_number_gate_test.go:127: 1 permit(s) in
    internal/docclass/testdata/anchor-number-baseline.tsv no longer cover a
    drifting row — a stale permit is a silent permission and this table only
    ratchets DOWN. Delete the row(s) (regenerate the table) or restore the site:
        devdoc/task-notify-terminal-gap-20260912.md  number-drifted
        internal/tools/tasks.go:2395  if rawLogPath == "" {
```

**Attribution, not assumption.** The same gate run in a detached scratch worktree
of this task at the branch base (`85581e538`, my own worktree, removed
afterwards) fails differently: **2 unpermitted drifts**
(`tasks.go:2221`, `tasks.go:2825`) and **no stale permit**. HEAD fails with **1
stale permit** and only 1 unpermitted drift. Different failure *sets* ⇒ the delta
is this branch's: line `2395` was a comment at base and is blank on HEAD, so the
permit's class flipped `number-drifted` → `recorded-line-blank` and the shipped
table stopped describing the tree. The remaining master-side drifts are not mine
and are filed (`01a12f8c-cea6`, `01a12f8c-ceff`).

**Repair — the generator, never the hand** (Rule 36/38):

```bash
go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv
go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
```

Result: `237 anchor row(s) scanned, 156 incoherent (6 new permit(s), 150
pre-existing)`; `--check` → **exit 0**, `table matches the tree — the freeze is
exact`; `go test ./internal/docclass/ -run Anchor -count=1` → `ok … 6.507s`.

**`gopi tool doccheck` is exit 1, and the earlier draft of this section said
`exit 0` — that claim was WRONG (corrected here, and in the report).** It was
written from the census `--check` verdict without running doccheck. Measured:
HEAD table → exit 1, 3 × `CLAIM NOT TRUE (regenerate the table)`; the base table
(`git checkout 85581e538 -- …anchor-number-baseline.tsv`) in the same tree →
exit 1, **2** claims. The regen therefore introduced exactly one
(`task-notify-terminal-gap-20260912.md`: its permit kind moved
`number-drifted` → `recorded-line-blank` and it was re-marked `first-frozen`);
the other two are pre-existing. Its coverage arm is clean:
`0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`. **The two gates disagree
about the same table** — census `--check` freezes the row SET, doccheck
re-derives each row's `first-frozen` ASSERTION across the 0b3c1de29 population —
and that disagreement is filed as its own row rather than papered over here.

Commit `bc10e8d78` carries the regenerated table. **This is part of the fix, not
a tidy-up**: a branch that lands a stale freeze lands a red gate.

### 8.1 A second gate reads the report's own prose — and the residue is not mine

`internal/docclass`'s prose-path-line gate parses a bare `` `path:line` `` cell in a
`tasks/*.md` report as a **citation** and fails when the number is past EOF. The
first draft of this task's report quoted the two drift rows above verbatim, which
created two brand-new past-EOF citations in the report itself:

```
UNPERMITTED citation past EOF: tasks/01a12f7649177e38bcda275baf52466b-report.md:163:
  devdoc/merge-backlog-terminal-guard.md:1148 is past EOF (317 line(s), over by 831)
```

Repaired by citing those paths **without** their numbers. Re-measured, both
findings are gone. **One unpermitted citation remains and it is not this
branch's** (`tasks/01a12edb-report.md`, a file this branch does not touch — base
`85581e538` already carries it). Its repair is the briefing of a **live** sibling
row (`01a12f8c-ceff-7d4e-88f9-1670f709a0b3`), whose worktree already holds
`prose-path-line-baseline.tsv` modified — so regenerating it here would collide
with a running deliverable (Rule 29). Left to its owner, named in the report's
`## Not Done`, and the branch-scoped suite is reported red on exactly those two
tests rather than claimed green.
