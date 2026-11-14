<!-- doc-class: historical -->
# A briefing's declared base revision is re-measured at SUBMIT — the authoring-time half of the base-staleness family

**Scope:** the submit seam (`internal/tools/task_briefing_base_submit.go`, wired into
`TaskSubmitReturningID`'s confirmation). **Verdict of the measurement: the dispatch seam was
already correct and stays unchanged; what was missing was the SAME measurement one step
earlier, where the author can still fix the text.** This document is the record of the
authoring-time half; the dispatch-time half — and the whole chain of the measured case — is
`devdoc/briefing-base-revision-staleness-at-dispatch.md`.

## The gap this closes, in one sentence

The dispatch seam (`task_briefing_premise.go`, commit `b0890a420`, row `01a136e8`) re-measures
a briefing's **assertion** ("X is not on the integration branch") and stamps the child's
prompt. A **base declaration** asserts nothing: `base 2f697e5` is a statement about where the
evidence was taken, not a claim about master — so the assertion scanner is structurally silent
on it (measured: 0 freshness stamps for that briefing's text), and nothing in the pipeline
disagreed with the row while the AUTHOR was still the one who could fix it.

## The shape of the change

```
payload ──► taskreport.BaseClaimsIn        (leaf scanner: a base WORD beside a COMMIT-SHAPED token)
            │
            ├─ declared revision ──► MasterClaimEvidenceAt   (is it an ancestor of the integration branch? exit status is the verdict)
            │                        └─ resolves the REF to ask (mirror when demonstrably ahead, else refs/heads/master)
            │
            └─ ancestor ──► integrationClaimCountBetween (`git rev-list --count <base>..<ref>`)
                            │
                            ├─ count ≥ 1  ──► WARNING appended to the confirmation (base, tip, commits-ahead, the command)
                            └─ count == 0 ──► SILENT  ← the positive control, structural, not an extra branch
```

Nothing in this lane is a second prober: detection is the dispatch seam's own leaf scanner,
the ancestry verdict is `master_claim.go`'s exit status, the ref resolution is
`integration_claim_target.go`'s, and the count is the existing `integrationClaimCountBetween`
(the `rev-list --count` helper the epic's diagnosis already used). The new file adds a
renderer and a criterion, not a mechanism (Rule 35: no fetch, no timer, no state file, no
ref).

## Why a warning, and why that is not a weakness

A base strictly behind the integration tip is a fact about the briefing's **AGE**, never
about the work: a payload may legitimately cite an old base — a diagnosis taken there, a row
whose premise is exactly "at `base X` the surface was red". A submit that refused on it would
let a stale-able measurement block a dispatch, so the row always queues; the author learns
before the row leaves, where the correction costs one edit. The same reasoning the
attachment-claim and hermes-host warnings beside it already state.

## Measured (this run, 2026-10-14, worktree `gopi-01a1371525237e0ca7dc42f192ba25ae`, master tip `95f4de712`)

| arm | payload declares | result |
|---|---|---|
| stale | `base 0ae7e7011ceec754c4cd5f7b5a73f1f63c9f2677` (master~6) | warning: `base 0ae7e701 is 6 commit(s) behind 95f4de71 (git rev-list --count 0ae7e7011cee…..refs/heads/master → 6)`, **row queued** (`01a1373b-27b2`, status=pending) |
| control | `base 95f4de712` (the tip itself) | **silent** — no `[Base revision …]` block in the confirmation, row queued (`01a1373b-4437`) |
| no declaration | prose only ("the base case", "codebase") | silent — the scanner yields no claim |

The red proof: setting `taskSubmitBaseStaleMinAhead` back to `0` (i.e. accepting a base that
IS the tip) turns the control arm red — `FAIL: TestSubmitBaseStalenessIsSilentWhenTheBaseIsTheTip`,
the warning printing `is 0 commit(s) behind` — and restoring the constant returns it green
(`git hash-object` before/after the mutation identical).

## The honest limits

- The detection window is the leaf scanner's, so a base declaration written as a bare
  40-hex sha is read as a 31-hex token (the look-ahead window is applied before tokenising).
  That is the scanner's behaviour, it is inherited, and it is filed with evidence as its own
  bug row (`01a1373b-6853`) rather than worked around here.
- A base that is NOT an ancestor of the integration branch is silence, by design: a base on
  a side branch is a different question, and "not on master" is the dispatch seam's subject,
  not this one.
- The warning measures the repo the row is filed against, at submit time. It says nothing
  about what the base means for the work — that judgement stays with the reader.
