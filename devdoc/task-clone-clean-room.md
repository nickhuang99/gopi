<!-- doc-class: normative -->
# `gopi task clone` — the clean-room copy, and why it is not `reopen`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## What problem it solves

A row's run goes wrong and the operator has three distinct questions. Before
this verb only two of them had an answer:

| the question | the answer before | the answer now |
|---|---|---|
| "the run died — re-enter the same ground" | `gopi worker <id>`, `gopi task status <id>` | unchanged |
| **"the original workspace is no longer trustworthy — give me a clean room with the same work in it"** | **none** (`reopen` was the nearest spelling and answers something else) | `gopi task clone <id>` |
| "the VERDICT said redo it" | `gopi task reopen <id> --reason …` | **`gopi task clone <id>`** — `reopen` was RETIRED 2026-10-30; its copy mechanics are clone's, and continuing the failure is `gopi session resume` |

`clone` is therefore a **second rung**, added beside `reopen`, never in place of
it (PROJECT.md Rule 43: a mechanism is not deleted to make room for its
neighbour).

## The status gate is the design

The verb's whole reason is "this room is suspect", and that sentence is only
true for some statuses. `taskCloneStatusAdmits` (`internal/tools/task_clone.go`)
is the one predicate, and the CLI, the engine and the tests all read it:

| status | verdict | why |
|---|---|---|
| `in_progress`, `claimed` | **refused** | the row may be RUNNING: its ground has an owner, and a clone would put a second run beside the first |
| `pending` | **refused** | nothing has run, so nothing can be distrusted — the ground is UNUSED, not dirty; the remedy is to run the row |
| `completed`, `failed`, `verified` | admitted | its run is over — exactly the state "the room may now be dirty" describes |
| `cancelled` | refused unless `--confirm-cancelled` | the row was abandoned on purpose; minting work from it silently would resurrect a decision somebody already made |
| anything else (`superseded`, …) | refused **by name** | a status the gate cannot judge is not silently admitted |

Every refusal is decided **before the first write**, so a refused clone leaves
the ledger exactly as it found it — asserted, not assumed: each refusal test
counts the ledger's rows on both sides.

## What the successor carries, and what it must not

**Carries** — the source's summary and payload **verbatim** behind a provenance
header, its tags (+ the lineage token), its `deliverable.repo` (inherited, or
`--repo PATH` resolved to its durable main checkout), its attachment manifest
with every replica re-materialised in the **new row's own session directory**,
and the lineage edge (`parent_task_id` + `tasktags.ReworkLineageTag`, without
which Rule 33 parks it forever behind a `failed` parent).

**Does not carry** — `worktree`, `branch`, `grounds`. Those are RUN-allocated,
and "the source's directory" is precisely what the caller declared
untrustworthy. The successor's ground is derived from its **own** id
(`devworkspace.Dir(root, newID)` under the operator's configured workspace
root), and the derivation refuses — fatally, not with a warning — if the result
could equal the source's directory. The line the verb prints **states** the
ground it promises, because "a clean room" is a claim about a path.

A source that recorded no worktree has no ground to replace; that absence is
**stated** (`TaskCloneGroundUntracked`), never reported as "the grounds differ".

## The live-successor gate

A second clone of the same row is a second run of the same work. The gate
matches on the **shape** (`<src>-c<ordinal>`) as well as on `parent_task_id`,
so an ordinary child that merely names the source as its parent is never read
as a clone (`taskCloneIsSuccessorID`; pinned by
`TestTaskCloneSuccessorIDShape`, including the `-r1` reopen successor, which
must not match).

## `--reason` is optional here — and that is the second difference from `reopen`

A clean-room copy repeats **the same work**, so there is no failure to carry and
no payload requirement. `reopen` requires `--reason` because a rework briefing
is built from the failure. When `clone` is given a reason it rides the payload
as a provenance note.

## Rejected alternative: teaching `reopen` a `--clean-room` flag

Considered and rejected: the two verbs differ in their **gate** (a verdict-judged
row versus a room suspected dirty), their **required input** (`--reason`
mandatory versus optional) and in whether an abandoned row is admissible at all.
One flag switching three behaviours is how "which row can I hand this" becomes a
question nobody can answer from the synopsis. Two verbs, two gates, each
nameable in one line — and the family's advertised surface is the ceiling that
keeps that from becoming three (`taskverbs.MaxTaskVerbs`).

## Evidence

See the task report for the run's `Ran:`/`Result:` lines: the dry run, the
minted row (`pending`, `parent=<source id>`, and a FRESH uuid — the id carries no
suffix of its source; the lineage is the row's own `clone` + `rework-of:<source
id>` tags, per operator ruling 2026-11-05, memory #27273), the payload-copy audit
row, the
four refusals (`pending`, `cancelled`, live successor, and `in_progress`), and
the confirmation path that admits the abandoned row.
