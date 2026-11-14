<!-- doc-class: normative -->
# The window leg's transport keys — every expression column of `harvestRemoteSelect` names its wire field

**Task**: `01a116847adf76c18ae1cc8ac7830550` (rework of
`01a116533b91772dacc9c1aa4f7139ca`, whose branch was archived and deleted —
Rule 23: the intent was recovered and re-implemented on current master, never
continued on the stale branch).

## The invariant (this document is the standing claim)

Every expression column of a `sqlite3 -json` transport **MUST** carry an
explicit `AS <wire field>` alias, and the alias **MUST** equal the receiving
struct's `json` tag. This is a criterion about the transport, not a note about
one run: the gate below fails the build when a column is dropped, unaliased or
misnamed, and it is enforced by
`internal/tools/memory_sync_harvest_wire_test.go`.

`sqlite3 -json` names a result column after SQLite's own **result name**: a bare
column keeps its name, an **expression** is named by its WHOLE SOURCE TEXT. The
window leg's SELECT therefore has to alias every expression column:

```sql
SELECT task_id, summary, …, COALESCE(deliverable,'') AS deliverable,
       COALESCE(run_method,'') AS run_method, COALESCE(run_cmd,'') AS run_cmd,
       COALESCE(reason,'') AS reason, COALESCE(parent_task_id,'') AS parent_task_id
  FROM tasks WHERE updated_at > %.6f ORDER BY updated_at ASC, id ASC
```

Re-measured on this host, two throwaway ledgers, the same expression both ways:

| SELECT sends | `sqlite3 -json` key | `encoding/json` binds to the tag |
|---|---|---|
| `COALESCE(deliverable,'')` | `COALESCE(deliverable,'')` | nothing — **the field arrives empty** |
| `COALESCE(deliverable,'') AS deliverable` | `deliverable` | `harvestRemoteRow.Deliverable` |

`encoding/json`'s tag match is case-insensitive but **does not skip
punctuation**, so the unaliased key bound nothing: every row the window leg ever
landed carried an empty `deliverable`, `run_method`, `run_cmd`, `reason` and
`parent_task_id` — the dependency edge (Rule 33), the branch provenance the
adopt gate pairs on, and the run provenance, all dropped **in transport**,
silently: no error, no counter, no log (Rule 15). The pull leg
(`syncRowRemoteSelect`) carried the identical defect and was fixed first; this
is its window-leg copy.

No unit test could see it: every test of this leg injects `Fetch`
(`harvestRows`, `internal/tools/memory_sync_harvest_test.go`), so the ssh+sqlite3
surface never ran (Rule 31). That is why the instrument below reads the SQL
text itself.

## What current master already carried, and what this change adds (Rule 26)

**Master (`e9fcc5778`, via `e9e1b2963`) already carried the aliases** — the
fix landed as `syncRemoteCoalescedProjection` in `internal/tools/memory_sync_row.go`,
`harvestRemoteSelect` builds on it, and `sqlite3 -json` keys are therefore
already correct on master. Re-fixing the expression columns would have been a
no-op diff. What master did **not** carry is this change:

1. **A positional gate.** Master's `TestRemoteProjectionCarriesAliases`
   (`memory_sync_remote_projection_test.go`) is a *substring* instrument: it
   asserts each projection item *contains* ` AS ` and that each query *contains*
   the shared constant. Both checks stay green on a **wrong** alias — measured
   in this run: aliasing `deliverable` to `deliverable_x` left master's gate
   GREEN while the wire field silently stopped binding (`column 11 … arrives as
   "deliverable_x", but the wire struct binds "deliverable"`). A reorder is
   invisible to it for the same reason.
2. **The edge's alias, spelled once.** `syncParentColumnNamed` did not exist:
   `memory_sync_row.go` re-spelled ``syncParentColumnExpr + ` AS parent_task_id` ``,
   i.e. the wire field name was a bare literal at the point of use (Rule 1b),
   and a transport could take the expression without the name that makes it
   bind.
3. **This document** and its `DOC-INDEX.tsv` row (Rule 36).

## The change

- `internal/tools/memory_sync_delta.go` — three shared names beside the existing
  `syncParentColumnExpr`: `syncParentColumnAlias` (the wire field
  `parent_task_id`), `syncColumnAliasKeyword` (`" AS "`, the ONE spelling of the
  alias introducer, referenced by the projection and by the gate that reads it),
  and `syncParentColumnNamed = syncParentColumnExpr + syncColumnAliasKeyword +
  syncParentColumnAlias` — **the expression AS the wire field**, so a transport
  cannot take one without the other.
- `internal/tools/memory_sync_row.go` — `syncRemoteCoalescedProjection` now
  references `syncColumnAliasKeyword` and `syncParentColumnNamed`; the
  `parent_task_id` literal is no longer typed inline. The two legs share this
  one projection (Rule 14): the window SELECT is `… , ` + that constant.
- `internal/tools/memory_sync_task.go` — `harvestRemoteSelect` carries the
  invariant above and the measured reason it is load-bearing (the same-wire
  transcript, with its date), so the next reader cannot "simplify" the aliases
  away. `syncParentColumnNamed` is the ONE spelling of the edge AS its field.
- `internal/tools/memory_sync_harvest_wire_test.go` (new) — the positional gate.

## The gate, and why it is positional

`TestHarvestRemoteSelectNamesEveryColumnForTheWireStruct` parses the SELECT's
column list, derives each column's **result name** (the token after ` AS `, else
the bare column — exactly what `sqlite3 -json` keys by), and compares that list,
**index by index**, with `harvestRemoteRow`'s json tags read by reflection. Three
red arms are asserted **inside the same test function**, each feeding the same
parser a mutilated query and naming the field it must blame:

- `red/dropped-column` — one column deleted → the count disagrees;
- `red/unaliased-expression` — the SHIPPED bug (the alias removed) → the name
  disagrees, the field `deliverable` is blamed;
- the live arm above (a WRONG alias) was observed by hand in this run against
  the real constant, side by side with master's substring gate staying green.

The wire-level instrument stays where master put it:
`TestRemoteProjectionDecodesThroughTheRealCLI` runs the REAL `sqlite3` binary on
a fixture ledger and decodes into the very struct the transport decodes into,
and it covers both legs through the shared projection.

## The test pattern this gate generalises

A **substring** check over SQL cannot see a rename or a reorder: the shipped,
broken query still *contains* every field name it drops (inside its own
COALESCE), and an alias renamed to `deliverable_x` still contains ` AS `. So a
SQL-shape gate **MUST** parse the column list and compare it **positionally**
with the receiver's own keys — never `strings.Contains`. The same rule applies to
`syncRemoteCoalescedProjection` (the one-row leg's tail), which is read by the
same parser here and by the shared projection test.

## Verification recipes worth reusing

- Reproduce the naming rule with no gopi in the loop:
  `sqlite3 -json :memory: "SELECT COALESCE('x','') , COALESCE('x','') AS reason"`
  → the first key is the source text, the second is `reason`.
- A substring gate on SQL cannot see a renaming; parse the column list and
  compare **positionally** with the struct's tags.
