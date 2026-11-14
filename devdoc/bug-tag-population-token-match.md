<!-- doc-class: historical -->
# Task Report: one definition of the bug-tag population (token rule, not `%bug%` LIKE)

Task `01a09248`. Branch `fix/bug-tag-population-token-match`, worktree
`/home/user/workspace/gopi-bugtag`, rebased onto master `36fe9404`.

## The defect

`TaskBugAdoptionStats` — the bug-flow adoption **measurement** surface (Rule 11
proof, the `[bug-flow] tasks with tag bug: N …` line echoed on every bug-tagged
submit) — selected its population with a raw substring match:

```go
SELECT status, COUNT(*) FROM tasks WHERE tags LIKE ? GROUP BY status   -- "%bug%"
```

Every other consumer of the tag set works at **token** level:

| reader | rule | `debug-ui` | `wbug` | `bug` | `bug033` | `bugflow` |
|---|---|---|---|---|---|---|
| `TaskBugAdoptionStats` (before) | substring `LIKE '%bug%'` | counted | counted | counted | counted | counted |
| `taskTagPriority` (queue tier) | token prefix | — | — | bug tier | bug tier | bug tier |
| `hasBugTag` (submit echo / reissue tag) | exact token | — | — | yes | — | — |

So the population the tally **counts** could diverge from the population the
queue **recognizes**. Complicating factor found while fixing: SQLite `LIKE` is
**case-insensitive** while the Go token rule is case-sensitive (`BUG` was
counted by the tally, never recognized as bug work).

## The fix — one definition, two faces (Rule 14)

`internal/tools/tasks.go`:

1. `bugTag = "bug"` / `bugFamilyPrefix = bugTag` — the token namespace is
   declared once; the Go predicates and the SQL fragment reference it (no bare
   `"bug"` literal left in any query or comparison).
2. `hasBugTagFamily(tags string) bool` — **THE** definition of "this row is bug
   work": a token equal to `bug` or *starting* with it (`bug`, `bug033`,
   `bugflow`). A token merely containing the word mid-token (`debug`,
   `debug-ui`, `wbug`) is not bug work; matching is case-sensitive.
3. `taskTagPriority` now resolves its bug tier by calling `hasBugTagFamily` —
   the priority backfill (`ensureSchema`, the `priority IS NULL` re-derivation)
   and the tally therefore share one predicate, with **zero SQL/Go parity risk**.
4. `taskBugPopulationWhere` — the SQL twin of that predicate:

   ```sql
   (','||replace(COALESCE(tags,''),' ','')||',') GLOB '*,bug*'
   ```

   Comma-anchored **GLOB**, not `LIKE`. GLOB (not the substring LIKE, and not
   the case-insensitive LIKE) preserves token boundary *and* case. The
   space-strip mirrors `strings.TrimSpace` per token in the Go twin.
5. `hasBugTag(tags) bool` stays the **narrow** question ("does this row carry
   the reserved marker itself") and is deliberately separate from
   `hasBugTagFamily` ("is this row bug work") — those are the two questions the
   code actually asks (submit echo, reissue tag append).

`TaskBugAdoptionStats` now queries `… WHERE ` + `taskBugPopulationWhere`.

The SQL uses no parameters for the fragment: it is a compile-time constant, so
there is no injection surface and nothing to bind (the previous `?`+`"%bug%"`
pair is gone).

## Verification

Build / vet / format:

```
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → build OK
go vet ./internal/tools/                          → clean
gofmt -l internal/tools/                          → clean (no output)
```

**Red-proof (the regression guard fails on the OLD query).** With
`TaskBugAdoptionStats` reverted to `tags LIKE '%bug%'`, the new test fails:

```
--- FAIL: TestBugPopulationMatchesTokenRule (0.12s)
    task_verify_test.go:370: tally must count exactly the 6 bug-family rows, got:
        [bug-flow] tasks with tag bug: 9 (verified 0, reopened 0, other 9)
--- FAIL
```

9 counted vs 6 recognized: the three swallowed rows are the mid-token / wrong-case
tags (`debug-ui`, `wbug`, `BUG`). Restored to the shared fragment → PASS.

**`TestBugPopulationMatchesTokenRule`** pins BOTH faces over one fixture of 11
rows: for every tag it asserts `hasBugTagFamily` == `taskTagPriority` == bug tier
== the SQL population fragment, and then asserts the tally text reads exactly
that population.

```
go test ./internal/tools/ -run 'TestBugPopulation|TestTaskBugAdoptionStats|TestTaskTagPriority|TestTaskSubmitDerivesPriority|TestTaskPriorityBackfill' -count=1
ok  github.com/nick/gopi/internal/tools   6.354s
  --- PASS: TestTaskBugAdoptionStatsAndTallyEcho
  --- PASS: TestBugPopulationMatchesTokenRule
  --- PASS: TestTaskTagPriorityMapping
  --- PASS: TestTaskSubmitDerivesPriorityFromTags
  --- PASS: TestTaskPriorityBackfillLegacyBugRows
```

**Live end-to-end repro on a throwaway ledger** (the rebuilt `bin/gopi`, the
exact sequence the briefing described):

```
GOPI_HOME=$SB ./bin/gopi task submit "mid-token tag probe" --tags debug-ui   # NO [bug-flow] echo
GOPI_HOME=$SB ./bin/gopi task submit "real bug probe"      --tags bug
  → [bug-flow] tasks with tag bug: 1 (verified 0, reopened 0, other 1)

sqlite3 $SB/memory.db "SELECT task_id, tags, priority FROM tasks;"
  …|debug-ui|5      ← priority 5 (not bug tier), and not counted
  …|bug     |1      ← priority 1 (bug tier), counted once

WHERE tags LIKE '%bug%'                                            → 2   (the OLD population: counted debug-ui)
WHERE (','||replace(COALESCE(tags,''),' ','')||',') GLOB '*,bug*'  → 1   (the fix)
```

The `debug-ui` row is visibly *outside* the bug population on all three faces
(no echo, priority 5, not counted) — the divergence is closed in the shipped
binary, not only in the unit fixture.

**Live ledger measurement** (`~/.gopi/memory.db`, 2026-09-12):

```
old LIKE '%bug%'                        : 221
shared fragment (token rule)            : 221
counted-but-not-recognized (divergence) : 0
recognized-but-not-counted              : 0
```

re-measured at commit time as tasks piled up (217 → 220 → 221 → 224) — **both
faces equal at every reading**. Zero live divergence matches the briefing's
2026-09-11 measurement: this was a **latent** inconsistency, and the fix keeps
it latent instead of one compound tag away from a wrong number. The count itself
is never the evidence; the two populations being *equal* is. All bug-family rows
on disk start with `bug`, and no live row carries `BUG`/`Bug` in its bug token
(the case-fold divergence is also latent).

The comparison is one statement, runnable by a reviewer at any time:

```bash
DB=~/.gopi/memory.db
sqlite3 -readonly "$DB" "SELECT COUNT(*) FROM tasks WHERE tags LIKE '%bug%';"
sqlite3 -readonly "$DB" "SELECT COUNT(*) FROM tasks WHERE (','||replace(COALESCE(tags,''),' ','')||',') GLOB '*,bug*';"
# divergence (must be 0) — rows the old query counted and the token rule rejects:
sqlite3 -readonly "$DB" "SELECT COUNT(*) FROM tasks WHERE tags LIKE '%bug%'
  AND NOT ((','||replace(COALESCE(tags,''),' ','')||',') GLOB '*,bug*');"
```

**Suite result on the rebased branch** (`go test ./internal/tools/ ./cmd/agent/
./internal/standards/ -count=1`):

```
ok   github.com/nick/gopi/cmd/agent         134.706s
ok   github.com/nick/gopi/internal/standards  8.751s
FAIL github.com/nick/gopi/internal/tools    440.438s   ← TestMemorySearchNonBlocking
```

The single red is `TestMemorySearchNonBlocking` — an already-filed, unrelated
flake (task `01a0920b`, "memory store races the first-open schema build"), not a
tag reader. Re-run in isolation **3× → ok (0.216s / 0.124s / 0.122s)**, and the
master baseline run of the same package (`460666fa`) is **ok (528.8s)**. No test
in this change touches the memory store.

**Stale-base red, absorbed by the rebase.** On the original base `3b51fc00`,
`cmd/agent` failed at `TestCLIContractInventoryMatchesDispatch` — a master fix
(`1cb6b05b`, "track the voice_input dispatcher the voice commit left untested")
landed *after* my base and is unrelated to this change. The rebase onto
`36fe9404` absorbs it; the test passes on the rebased branch. The change touches
only `internal/tools/` + `devdoc/`, so it cannot influence that test.

## Not done / observed (honest list)

- **`awaitingVerifyCount` (`internal/tools/task_verify_closure.go`) still
  carries the same coarse `tags LIKE '%bug%'`** — but it lives on the UNMERGED
  `feat/bug-verify-default` (tip `b51c8e0e`, proven NOT an ancestor of master),
  so it is outside this change's blast radius. When that branch lands, that one
  call site should be swapped to `taskBugPopulationWhere` (same package, so no
  export needed). Calling it out rather than reaching into an unmerged branch.
- **The sync import/export pull face stays case-insensitive by construction**
  (`(',' || COALESCE(tags,'') || ',') LIKE ?` in `memory_sync_task.go` — SQLite
  LIKE folds case), so a peer's `BUG`-tagged row is pulled into the ledger while
  every population reader (correctly) ignores it. No live instance; filed as
  separate bug task **`01a09253`** rather than widened here — making the import
  gate case-sensitive is a sync-face decision with its own blast radius.
- **Same coarse-match class, DIFFERENT tag: `adoptionConflictTaskLive`
  (`internal/tools/task_adoption.go:294`) dedups on `tags LIKE
  '%merge-conflict%'`.** That is the same-branch-once guard, not a measurement
  surface, and `taskTagMergeConflict` is a hyphenated token with no
  family/prefix semantics — it needs its own token-anchored decision, not this
  fragment. Zero live divergence today. Filed as separate bug task
  **`01a0925a`**.
- **The docs-side comment in `cmd/agent/cli_tasks.go` ("tags=[\"bug\"] is the
  bug-flow entry")** is accurate and untouched.
- **Timing, not just wording.** `TaskBugAdoptionStats` runs on the submit path
  (the `[bug-flow]` echo). SQLite `GLOB` cannot use an index either (`tags` is
  free text with no index), so the token match costs the same full scan `LIKE`
  did — the fix buys correctness at no query-plan cost.

## Commit

```
fix(tasks): one definition of the bug-tag population — token rule, not '%bug%' LIKE
branch fix/bug-tag-population-token-match @ c4ebfd08  (base: master 36fe9404)
```

Recovery: `git show fix/bug-tag-population-token-match:devdoc/bug-tag-population-token-match.md`.
The worktree shares the main repo's refs, so the branch IS the deliverable — no
push, no merge by this session.

## Files

- `internal/tools/tasks.go` — `bugTag`/`bugFamilyPrefix` consts, `hasBugTagFamily`,
  `taskBugPopulationWhere`, `taskTagPriority` rewired, `TaskBugAdoptionStats` query.
- `internal/tools/task_verify_test.go` — `TestBugPopulationMatchesTokenRule`
  (new), `TestTaskBugAdoptionStatsAndTallyEcho` (mid-token row now exercises the
  echo gate).
- `internal/tools/tasks_priority_test.go` — `TestTaskPriorityBackfillLegacyBugRows`
  now adds a `debug-ui` row (must stay out of the bug tier) and derives its
  backfill assertion from `taskBugPopulationWhere`, so the priority-backfill
  face is pinned by the same single definition.
- `devdoc/bug-tag-population-token-match.md` — this report.
