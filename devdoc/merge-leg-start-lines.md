<!-- doc-class: historical -->
# A merge leg said nothing for 260.8 seconds — and could not be told from a wedged face

**Class**: devdoc / incident + fix
**Origin**: bug row `01a14252`, filed by task `01a136c7`; executed on branch
`task/01a1425205b77854a0d774a00eeb277b` (cut at peer master tip `f007c88`).

## The defect, as measured

Every leg of the merge gates logged **once** — when it *returned*. The two
longest legs are compiled-shape work, and on one landing they ran concurrently:

| leg | step | measured |
|---|---|---|
| `go vet ./...` | merged-result step (`MERGESET gate`) | 267.9 s |
| gate-packages test leg | merged-result step | 243.6 s |

Result: **260.8 s in which neither stdout nor stderr carried a single byte.**
A leg that is working and a leg that is wedged are indistinguishable in that
window — which is the whole defect. Nothing was broken; nothing could be
*observed*.

## The fix

One line per leg, emitted **before** the leg's process is spawned, through the
funnel that already existed (`taskNotifyLog` → stderr — no new knob, no new
process, no new file; Rule 35):

- `internal/tools/merge_build_gate.go` — the build gate's per-leg runner
  (`mergeBuildGateLegStartFmt`, `mergeBuildGateLegStartMarker`).
- `internal/tools/rebuild_vet_mergeset.go` — the merged-result step's per-leg
  runner (`mergesetGateLegStartFmt`, `mergesetGateLegStartMarker`).
- `internal/tools/merge_union_gate.go` — the union gate's leg loop.

Each line names its leg **and the budget that bounds it**. The budget is the
part that makes the line actionable: without it a reader knows a leg started,
but not how long the absence of a result line is still legitimate.

## Why the budget is not decoration

`gopi task merge` on a *fixture* repo — a pure POSIX-`sh` build, no Go compile,
no vet — still showed **three silent windows of 30.3 s, 33.3 s and 3.7 s**, and
a trailing silence of 121.5 s, sampled at 0.5 s (evidence:
`exp/ab/A.timeline`, session `01a14252-…`). The long legs are not exotic; they
are the ordinary shape of a landing.

## Second half of the same row: the refusal that named the wrong remedy

The single-instance lock has two refusal arms:

- **contention** (`EWOULDBLOCK`, a live holder) — `serveLockContentionError`;
- **stray holder / replacement** (a live process still flocks an **unlinked**
  copy of the path) — `serveLockStrayHolderRefusal`.

Since the serve-lock-diagnosis epic, the *contention* arm has appended three
labelled lines answering the questions an operator actually has — `lock-holder:`
(with liveness), `lock-stray-holder:`, `lock-kernel-records:`. The
*stray-holder* arm printed its sentence alone — and that sentence is precisely
the one the run measured as **non-actionable**: it advised "kill pid N", while
the holder was alive and unreachable because `flock -n` on the path reported
FREE.

Both arms now render through **one** builder, `serveLockStrayHolderRefusalError`,
which composes the **one** shared renderer `serveLockRefusalDiagnosis`
(Rule 2 — one renderer per question). Both call sites route through it: the
merger face (`cmd/agent/cli_tasks.go`) and the worker face
(`cmd/agent/cli_worker_control.go`), which had the same gap.

## Two measurement lessons worth keeping

**1. A red proof can be masked by a second call site.** The first mutation
removed the diagnosis from *one* of the two stray-holder call sites — and the
test stayed **green**, because the test's path went through the other one. The
mutation that proves anything is the one applied to the **shared** function body.
Take the mutation to the narrowest point every path passes through.

**2. A restore is proven by content identity anchored to a revision**, not by a
clean `git status`: `git show HEAD:<path> > <path>`, then compare
`git hash-object <path>` against `git rev-parse HEAD:<path>`. `git diff HEAD`
being empty is the second half of the same reading.

## What is deliberately NOT here

- No ordering assertion in the test. A leg's output is captured into a buffer by
  `CombinedOutput` and never reaches the stream the START line goes to, so the
  two halves are not comparable in one capture. The order is structural (the
  `taskNotifyLog` call sits immediately above the spawn); the test proves the line
  exists, names its leg, and changed no verdict.
- No new knob to switch the lines off. A silent window that can be re-opened by
  an environment variable is the same defect with a customer.

## Check

```bash
go test ./internal/tools/ -run 'TestMergeBuildGate|TestMergesetStep_EveryLeg'
go test ./cmd/agent/ -run 'TestServeLockReplacementMechanism'
```
