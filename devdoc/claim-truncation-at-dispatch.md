<!-- doc-class: normative -->
# A dispatch-seam clause can be TRUNCATED AWAY — measured while adding the gate-assertion clause (row 01a136be's epic)

**Scope:** the dispatch seam's briefing clauses (`cmd/agent/cli_tasks.go`,
`runTaskRunOnce`) and the bound on the child's first model request.

## The measurement (this is the finding; the bound itself is NOT yet named)

Adding the gate-assertion clause (+ the `briefing_clause.go` refactor) to the
seam reddened `cmd/agent/TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree`:

| tree | runs | result |
|---|---|---|
| my branch (clauses wired) | 9 | **FAIL 9/9** |
| base `3db1fd211` (same test, same machine) | 6 | **PASS 6/6** |
| fresh worktree detached at my commit | 3 | FAIL 3/3 (not a stale build cache) |

The failure is not an assertion about my clause — it is the pre-existing
ground-announcement/cut-point assertions failing because **the child's first
model request is TRUNCATED BEFORE those clauses**:

```
DEBUG label "[Your worktree — ALREADY CREATED for you]" present=true  idx=4951
DEBUG label "This branch's cut point — …"              present=false idx=-1
DEBUG label "Which `master` — …"                       present=false idx=-1
DEBUG label "GATE ASSERTION"                           present=false idx=-1
DEBUG bodyLen=52830            # base: 55587
```

Base carries both clauses (`cut point` at idx **8179**, `Which master` at **9434**);
the wired tree carries NEITHER, and its request body is **2757 bytes SHORTER**
than base's — even though the wired tree appends MORE text. So the added clauses
do not merely land last; they push the tail past whatever bounds that prompt, and
the pre-existing clauses fall off with them.

## Isolation performed (each reverted to base while the rest stayed)

- **both `cmd/agent` files reverted** (no wiring at all, new `internal/tools`
  files still present) → still FAIL 3/3.
- **`devdoc/DOC-INDEX.tsv` reverted** → still FAIL.
- a **fresh detached worktree at my commit** → FAIL 3/3.

The first line is the one that matters and it is the one that makes this
obvious in hindsight: with the wiring REMOVED the test still failed — so the
trigger is not the call site but something else my commit changed that reaches
the child's prompt. `git checkout 3db1fd211 -- cmd/agent/cli_tasks.go` writes the
file but the **new `internal/tools` package symbols still exist** and `cmd/agent`
still builds against them, so "no wiring" was not a clean isolation — the honest
statement is that the experiments above did NOT isolate a single cause.

## What is NOT established

- **WHICH bound truncates** is unknown. It is NOT the mock
  (`MockLLMRequestBody` stores the whole body; base's 55587 proves that).
- Whether the bound is a byte cap, a tool-count/description-assembly order, or
  something in the prompt builder — **unverified**.

## Reproduction

```bash
cd <worktree at 1aa9b855b^>
go test ./cmd/agent/ -run TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree -count=1
# on this tree:  FAIL (9/9)
cd <worktree at 3db1fd211>
go test ./cmd/agent/ -run TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree -count=1
# base:          ok  (6/6)
```

## Why this matters beyond the one test

Every clause this seam adds (base revision, landed/unlanded, gate assertion,
count, cut point, master reading) is written to the END of the briefing, which is
exactly where a bounded prompt loses its tail first. If a bound exists and is
near, then **the seam's newest clauses are the first to silently disappear** —
and the failure mode is invisible: the run simply never receives the freshness
clause, and nothing in the record says so. That is the same class of defect the
clauses exist to prevent, one level up.

**A clause that is truncated away is a clause that does not exist** — so the
question "is the briefing delivered WHOLE?" is a precondition of every clause on
this seam, not a separate concern.

## What a follow-up must settle (acceptance)

1. **Name the bound** (byte cap? tool-array assembly? something else) and cite
   the code line — or prove there is none and the divergence has another cause.
2. **Assert delivery**: a test that the child's first request carries the LAST
   clause the seam appended, for a briefing long enough to be near the bound.
3. Only then does the gate-assertion wiring ship.
