<!-- doc-class: historical -->
# The Seam's Appended Clauses Died in a Shadowed Variable

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class: normative** — this describes a standing property of the dispatch seam on
master, and the check that keeps it. Measured 2026-10-16 on task
`01a143b13407791abe72d7d1e391ac43`, tip `1921b4a`; the defect was first observed
by task `01a14242` at `3db1fd211`.

## The symptom, as reported

A dispatched child's FIRST model request appeared to lose the TAIL of its
briefing. On branch `task/01a14242` the wired seam produced `bodyLen=52830` with
the pre-existing cut-point and `Which master` clauses **ABSENT**, while base
`3db1fd211` produced `bodyLen=55587` with them **present** at idx 8179/9434 —
that is, the wired body was **2757 bytes SHORTER despite appending MORE text**.

That reading is what a **bounded prompt** looks like: the seam appends its
clauses (ground announcement, premise, base revision, landed/unlanded, gate
assertion, figure, pinned path set, cut point, master reading) at the END of the
briefing, and a capped body loses its tail first — with a truncated clause
INVISIBLE to the run.

**The hypothesis was a byte bound. There is no bound.** This document records
what the cause actually was, because the shape of the misdiagnosis is the
transferable part.

## The cause: one `:=`

`cmd/agent/cli_tasks.go` appends the freshness clauses to the child's briefing
inside the **ground-resolution block**. The FIRST of those appends was written:

```go
briefing, premiseAppended := tools.BriefingWithClause(briefing, premiseClause)
```

Inside a block, `:=` **declares a new variable** rather than assigning to the one
in the enclosing scope. So a scope-local `briefing` was born, holding the
first-appended value, and **every later append on the seam** — base, gate
assertion, figure, pinned path set, cut point, master reading — assigned into
that variable, which died when the block ended:

```go
var baseAppended bool
briefing, baseAppended = tools.BriefingWithClause(briefing, baseClause)   // same block → the shadow
```

The child therefore received the briefing **as the seam read it before any
clause**, while the seam printed every `…NAMED at dispatch` record line for
clauses the child never got.

### Why every gate stayed green

The clause RECORDS are printed from `tools.BriefingWithClause`'s return value
**outside** the block, through `execRunFacts`. The announced text therefore
reached the run-log while the padded text did not reach the child — so the log
line that exists precisely to prove "a clause was stamped into the child's
briefing" **cannot distinguish stamped from delivered**. That is the whole
reason this hid: the instrument was reading the wrong side of the boundary.

## The measurement (this tree)

| | before the fix | after the fix |
|---|---|---|
| body (first request) | 52830 | 55585 |
| piped briefing (`cmd.Stdin`) | 3262 B, `hasCutPoint=false hasMasterReading=false` | 5959 B, `hasCutPoint=true hasMasterReading=true` |
| cut-point label idx | `-1` | `8178` |
| master-reading label | absent | present |
| E2E | FAIL | PASS |

The piped-briefing row is the decisive one: a `fmt.Fprintf(execRunFacts, …)`
probe on `len(briefing)` **at the `cmd.Stdin = strings.NewReader(briefing)`
line** — the bytes the wrapper actually hands the child — showed 3262 bytes. The
briefing the seam *read* was 3262 bytes; the clauses existed only in the shadow.

## The bound hypothesis, falsified

The delivery walk's **over-limit** arm ships a **64 KiB** payload — four times
the seam's own inline spill threshold (`taskPayloadInlineMaxBytes`, 16 KiB) — so
the briefing reaches the child through the SPILL path, which is exactly the path
a byte cap would corrupt first. **The last clause still arrives.** There is no
byte bound on this path, at any size this walk exercises.

The arm also asserts that the full payload is NOT inlined, so an arm named
"over-limit" cannot silently be testing the ordinary inline path.

## The check: `TestBriefingLastClauseReachesTheChildsFirstRequest`

`cmd/agent/task_briefing_delivery_e2e_test.go`. Two arms (`near-limit` 5 KiB,
`over-limit` 64 KiB), each submitting a row, spawning a real `gopi worker`, and
reading the **first model request off the mock**:

1. the **LAST** clause the seam appends (master reading) must be in the served
   request — the first casualty of a dropped tail;
2. the **FIRST** clause (ground announcement) too, so head-truncation is caught
   by the same walk;
3. the previously-lost sibling (cut point) by name — the clause the sibling run
   measured absent at `idx=-1` while base carried it at `8178`;
4. the over-limit arm's control (spill, not inline).

**Asserted on the request the mock SERVED, never on the seam's record line** —
that line is printed just as happily by the broken tree.

```bash
go test ./cmd/agent/ -run TestBriefingLastClauseReachesTheChildsFirstRequest -count=1
go test ./cmd/agent/ -run TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree -count=3
```

Red proof: restoring the `:=` reddens **both** arms; the two-line fix greens them.

## The transferable lesson

**A log line printed about a value is not evidence the value was delivered.** If
a mechanism's job is "put X in front of the consumer", the gate must read X from
the **consumer's** side of the boundary — here, the request the mock served.
Reading it from the producer's side (the seam's own record) is reading the
producer's INTENT, and a producer can hold a correct intent about bytes it never
passed on.

Corollary for diagnosis: **a body that is SHORTER than a reference while the code
appends MORE must not be explained by a bound before the plumbing is read.** A
cap explains a truncated tail; it does not explain "the appends are gone while
the announcements remain" — that asymmetry points at the data path, not the
limit. The obvious suspect was the last thing it was.
