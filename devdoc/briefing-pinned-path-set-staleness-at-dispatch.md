<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The dispatch seam's fourth clause: a briefing that pins a PATH SET

**Row**: `01a144f80-afb7-73d3-a846-ccb074e40bd7` (branch `task/01a144f80afb73d3a846ccb074e40bd7`),
filed from row `01a1447c-253e-74a2-8329-1c37447c9e59`.
**Class**: system layer — the spawn seam. Rule 28 attributes this surface to **peerbox**;
this run is on a client (`hostb-Laptop`) and changed it, so the ownership is stated here
and in the report rather than left implicit.
**Anchor**: base `8cd9906`, tip of this work `aacb8fa59`.

## The incident, measured

Row `01a1447c`'s briefing pinned **twelve paths** as the `devdoc/DOC-INDEX.tsv`
stored-cell drift population and wrote an **acceptance criterion against that set**:
"`git diff --stat -- devdoc/DOC-INDEX.tsv` 的变化行数与 12 行一致". Two measurements
on the row's own ground say the criterion was unreachable as written:

- **The set was a snapshot, not a state.** The twelve paths came from a snapshot the
  *previous* row took at 19:11. Sibling commit `cc9c1fee8` (authored 18:16, merged
  19:34) had already re-derived **every one of them**.
- **The live set was a different one.** On that ground the real drift population was a
  *different* twelve, of which **two were class moves** — a path whose CLASS cell has
  to change, which no cell re-derivation clears (flipping the class cell alone reddens
  the banner gate).

A measurement repeated on **this** work's tip confirms the shape is still live here:
`python3 tools/docclass/classify.py . --check` reports **23 STALE lines**, of which
exactly one (`devdoc/main-checkout-deposit-arm.md`) was in the pinned twelve. The pinned
set and the live set are not merely different sizes — they are different sets.

## Why three shipped scanners are blind to it (measured on the incident's own text)

Probe: `go run ./tmpfreshness <ground> <briefing>` (throwaway, removed before the run
ended; output in this run's session artifacts, `bugrow-text-BEFORE.out`).

| scanner | result on that text | why |
|---|---|---|
| `CountClaimsIn` (figure clause) | **0** | it requires a count DIGIT-ADJACENT to an **English** counted unit (`rows`/`lines`/`permits`); the text counts in Chinese (`12 行`, `12 条`) and its path list is a multi-line `for` loop |
| `GateAssertionsIn` | **0** | it fires on a RED verdict word beside the integration branch's name; the text asserts no verdict |
| `UnlandedClaimsIn` | **0** | nothing asserts the branch lacks a revision |
| `BaseClaimsIn` (base clause) | **1 — it DID stamp** | the text declares `base 1c5b086` |

The base clause stamping is the interesting half, and it is **not** sufficient: its
instruction is generic ("run THAT surface again") because a base behind the tip is all a
base can be. It never says *which* surface, never says the population is a **set**, and
never warns that the **set itself** is what moved. The child that reads only the base
clause repairs the twelve paths it was handed — ten of which need nothing.

## What was built

**`internal/taskreport/driftclaim.go`** — the leaf. A **count** standing above a **LIST
BLOCK**: consecutive lines carrying exactly one path-shaped token each, ending with the
shell's own list punctuation. Two vocabularies, one of them reused: the English figure
reader (`countClaimFigureIn`), the delta exclusions, the digit parser and the path
predicate are the package's own; only the CJK counters (`条 行 个 项 路径 处`) and the
block shape are new.

**`internal/tools/task_briefing_path_set.go`** — the clause, the fourth sibling of the
unlanded / base / figure clauses, on the SAME spawn seam. It:

- does **not** re-derive the set — that costs a full classifier pass, paid by **every**
  dispatch to serve a rare shape — and **says so in its own words**
  ("THIS SEAM DID NOT RE-MEASURE IT FOR YOU"), naming the command shape instead
  (report-only mode, quote the output, diff against the pinned count);
- states that a **moved set is not a set that is fine** and is not a set that will still
  be there, and names the two consequences the acceptance criterion cannot survive:
  a per-path re-derivation is valid only for a path **still** drifted, and a **class
  move** cannot be cleared by re-deriving cells at all;
- explicitly **denies the completion reading** ("This clause is NOT evidence that the
  work is done"), the base clause's doctrine one level down;
- takes the ONE measurement that is cheap and exact — **which pinned paths this ground
  can answer for at all** — through the figure clause's ONE resolver, so the two clauses
  cannot disagree about what "inside this ground" means.

**`cmd/agent/cli_tasks.go`** — wired after the figure clause, with its own record line
(`taskBriefingPathSetLineFmt`, `cmd/agent/task_child_env.go`). Additive: best-effort,
never errors, never blocks a claim.

## Two implementation failures, kept as tests

**1. A line-scoped rule was tried FIRST and measured wrong.** The first
`PathSetClaimsIn` fired on a single line, and on the incident's own text it fired on a
**parenthesis** naming a command and a file (`（该 12 条与 \`./bin/gopi tool doccheck\` …
evidence/doccheck-before.txt）`) and reported a one-path "set" that is not the pin at
all. The rule was rebuilt around the **block**, and the prose line is now a fixture:
`TestPathSetClaims_RefusesTheProseLineThatFooledTheLineScopedRule`.

**2. The loop's BODY was read as a member.** With the block rule in place, the
classifier invocation inside the `for` body
(`python3 tools/docclass/classify.py . --only "$p"`) was collected as a thirteenth
entry, so the clause would have handed the child a four-path set at the wrong end. The
discriminator is now what stands **left of the path**: an entry is the path (optionally
with a continuation or a separator after it), while a command carries arguments
**before** it; the `for <var> in` header is the ONE prefix allowed, and only on the
block's first line. Fixture: `a command that names a file is not a list entry`.

## The acceptance that was missing before

`devdoc/claim-truncation-at-dispatch.md` records that a previous attempt at this seam
was **reverted** because its wiring reddened a ground-announcement E2E, leaving the
honest open question "does a clause appended here reach the child at all?". This run
asserted that question instead of arguing it
(`cmd/agent/task_briefing_path_set_e2e_test.go`): the real binary is driven, the child
is held inside its first model round, and the child's **first request body** must carry
the label, the instruction, the author's figure (`12 行`) and a pinned path. RED PROOF:
with the append disabled at the seam the walk fails on the label and the instruction;
with it restored (`git rev-parse HEAD` re-checked, `git hash-object` equal to
`HEAD:cmd/agent/cli_tasks.go`) it passes.

## Not done

- The clause **does not measure the live set**. Doing so at dispatch means running the
  classifier on every dispatch; that trade was declined deliberately and is stated in
  the clause's own text. A follow-up could measure a **bounded** prefix of a pinned set
  (the first N paths), but nothing here claims it does.
- The 23 STALE rows measured on this tip are **not** repaired — that is another task's
  deliverable, and repairing them here would have put an unrelated diff in this branch.
- The base clause's generic wording was left as it is: with this clause beside it the
  gap it leaves is covered, and rewriting a shipped clause is its own change.

## Verification

See the run's report (`$GOPI_TASK_REPORT_PATH`) for the Build / Tests / Ran / Result /
Evidence block, and `devdoc/DOC-INDEX.tsv` for this file's classification row.
