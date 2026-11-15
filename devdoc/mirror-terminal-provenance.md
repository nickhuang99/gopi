<!-- doc-class: normative -->
# The mirrored terminal row's provenance: `'sync'` was not a value at all

`internal/tools/memory_sync_task.go`'s mirror-history INSERT
(`importTaskMirrorTx`) hardcoded the literal `'sync'` in the `source` column of
every terminal row it copied from a peer. `'sync'` is not ledger vocabulary:
`tasksource.Known("sync")` = **false**, `tasksource.Normalize("sync")` =
`("", false)`. So the sync layer's own writer stamped a provenance that the
ledger's own validator (`validTaskSource` = `tasksource.Known`) refuses, and
`gopi task show` rendered it through `taskSubmittedBy`'s `orDash` fallback —
the raw spelling, narrated by nothing.

Measured before the fix (real CLI, real ledger, see `## Verification`):

```
source:         sync
submitted_by:   sync
```

## The decision (two shapes, two values — not one)

The sync layer writes provenance at TWO shapes, and they answer different
questions. Naming them one value would misdescribe one of them, which is the
defect the retired `auto` alias already taught this repository (see
`devdoc/forwarded-row-provenance.md`, commit `0bbfcffd`).

| shape | writer | what the row IS | value |
|---|---|---|---|
| fresh replication | `importTaskFreshTx` | PENDING work THIS node will run — the row *is* this node's enqueue | `tasksource.Server` (unchanged) |
| mirror insert | `importTaskMirrorTx` | a TERMINAL row copied here, never enqueued, never run on this node | `tasksource.MirrorTerminal` (**new**) |

Why the mirror row is not `server`: `server` narrates "the resident node
machinery enqueued this" (`taskSubmittedBy`'s "resident node machinery"); a
mirrored terminal row is the record of a run that happened ELSEWHERE and is
never in this node's runnable set. Why it is not `forward` either: `forward` is
the opposite direction — a row THIS node filed to a peer, whose local copy the
transport produced. Arrival of somebody else's finished work and departure of
this node's own row are two sentences.

The decision is recorded in two places so neither has to be re-derived: the
constant's own comment (`internal/tasksource/source.go`, `MirrorTerminal`) and
the package doc's vocabulary table.

## What changed

1. `internal/tasksource/source.go` — the `MirrorTerminal` constant; wired into
   `lexical` (the acceptance list), `Normalize`'s current-value case and
   `Policy()` — a constant missing from any of the three is refused by the
   ledger's own validator or refused in an error message; plus the vocabulary
   table entry in the package doc (the decision).
2. `internal/tools/memory_sync_task.go` — the INSERT binds
   `tasksource.MirrorTerminal` in place of the literal.
3. `internal/tools/tasks.go` — `taskSubmittedBy` gained the
   `tasksource.MirrorTerminal` arm, so the operator-facing line narrates the
   value instead of falling through `orDash`.
4. `internal/tools/task_sync_mirror_provenance_test.go` — the pin, driving the
   REAL import path (`SyncTaskImport`, the function `gopi memsync
   --import-tasks` calls) against a real mirror DB, and asserting both halves
   of the decision (mirror → `MirrorTerminal`; fresh → `Server`).

## The audit (what else in this family was checked, not assumed)

The `'sync'` literal was the LAST bare spelling in the `tasks.source` write
path. Audited, with the grep each claim rests on:

- `grep -rn "'sync'" --include=*.go internal/ cmd/` → the only hit left is the
  test's own comment. No other writer spells it.
- `internal/tools/memory_sync_delta.go`'s `syncDeltaInsertRow` (the delta
  transport's insert, reached when the client applies a delta patch) also
  inserts a terminal row this node has never seen — and it is CORRECT as it
  stands: it forwards `r.Source`, the peer's own recorded value, verbatim. It
  never invents a spelling, so it cannot reintroduce this defect; and an empty
  `r.Source` there (a legacy peer row) lands as `''`, which `taskSubmittedBy`
  renders as the `—` "no claim" placeholder, not as a fake provenance. That is
  the honest reading of "the peer recorded nothing", so the asymmetry with the
  mirror path (which writes its OWN sentence about a row that arrives with no
  local counterpart) is deliberate and named here rather than "fixed" into a
  second default.
- The column DEFAULT is `''` in the CREATE, the ALTER and the FTS-rebuild
  projection (`colExpr("source", "''")`) — unlike the retired alias, the
  DEFAULT is not itself a writer here.
- `tasksource.AgencyProposal` is deliberately KEPT in the `task_submit` flag
  help (a submitter is allowed to say what kind of a row it is); a mirror
  terminal row is written by machinery, so the flag help correctly does not
  enumerate it. No rule requires the two lists to move together.

`internal/tasksource` was checked for a renderer of its own: it is a leaf
package with no code-table, so its doc table + `Policy()` are the whole surface
a new value has to reach.

## Out of scope (named, not silently absorbed)

Historical rows already written with `source='sync'` are NOT rewritten by this
change — the fix is on the write side, exactly as the `auto` retirement was. A
backfill (`UPDATE tasks SET source='mirror-terminal' WHERE source='sync'`) is a
data migration and a separate decision; it is deliberately not in this diff
(`Normalize("sync")` still returns `("", false)`, and making the reader accept a
spelling never written again would be the wrong direction).

## Verification

- Build: `go build ./...` → OK.
- Tests: `go test ./internal/tools/ ./internal/tasksource/ ./internal/standards/`
  → green. Branch-scoped (the diff's packages), not a whole-tree run.
  ```
  ok  github.com/the operator/gopi/internal/tools        4.172s
  ok  github.com/the operator/gopi/internal/tasksource   0.002s
  ok  github.com/the operator/gopi/internal/standards   37.331s
  ```
- Ran (the changed surface, for real — a real ledger, the real CLI):
  ```
  CGO_ENABLED=0 go build -o $T/gopi ./cmd/agent/
  ./gopi memsync --import-tasks $T/patch.json --db $T/mirror.db
  ./gopi task show t-real-mirror-1 --db $T/mirror.db
  ```
- Result: the patch (one TERMINAL row, no local counterpart → the mirror-insert
  branch) printed `imported tasks: queued=1 duped=0 already=0 migrated=0
  tagged=0 refused=0` (exit 0), and `gopi task show` read the row back as:
  ```
  source:         mirror-terminal
  submitted_by:   mirrored terminal history (mirror-terminal; this ledger never ran it)
  status:         completed
  ended_at:       2025-09-26 23:20:00
  ```
  Before the change the same two lines read `source: sync` /
  `submitted_by: sync` — the raw spelling off the orDash fallback.
- Red-before/green-after: reverting the INSERT to `'sync'` fails the new pin
  with `mirrored terminal row source = "sync" — not ledger vocabulary (want
  human|agent (llm)|worker|merger (auto)|server|forward|agency-proposal|
  mirror-terminal)`; restoring it is green.

## Files

- `internal/tasksource/source.go`
- `internal/tools/memory_sync_task.go`
- `internal/tools/tasks.go`
- `internal/tools/task_sync_mirror_provenance_test.go` (new)
- `devdoc/mirror-terminal-provenance.md` (this file) + its `DOC-INDEX.tsv` row

## Not Done (honest)

- No backfill of `source='sync'` rows (above — a data migration, own decision).
- `syncDeltaInsertRow` (the delta transport's insert) does not stamp
  `MirrorTerminal` for a terminal row it has never seen; it forwards the peer's
  own `source`, which is a different (and defensible) rule — named here so the
  asymmetry is not mistaken for an omission later.
- The two pre-existing manifest gaps this run had to repair to get
  `internal/standards` green (`devdoc/chat-delta-cadence.md`,
  `devdoc/task-sync-banner-source.md` had no `DOC-INDEX.tsv` row on master) were
  fixed as rows only — the docs themselves were not audited.
