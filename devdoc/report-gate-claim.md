<!-- doc-class: normative -->
# The gate-claim contract: a report speaks only to what it observed

Epic: *a report may not assert a gate result it did not measure*
(`briefings/brief-report-must-not-assert-unmeasured-gates.md`).
Branch: `fix/report-gate-claim-evidence`, off `master` `9cae9d59`.

## The measured case (both sentences, verbatim)

The run's own close-out summary (task `01a0af50-5029-75ba-8bac-368ae86a3351`):

> **Gate status:** red proof reproduces the operator's verbatim
> `permission denied` and turns green with the fix; … `go vet` exit 0, gofmt
> clean, targeted tests PASS; **`git status --porcelain` empty in both
> worktrees (Rule 30)**; live `gopi merger --status` reports the stale lock
> **FREE** so nothing blocks a start.

The close-out machinery, **in the same run, about the same worktree**:

```
worktree is DIRTY (uncommitted work) — left for a human — entries:
  M cmd/agent/merger_takeover.go
  M cmd/agent/merger_tick_no_child_gate_test.go
?? cmd/agent/merger_takeover_scope.go
?? cmd/agent/merger_takeover_scope_test.go
?? devdoc/merger-takeover-scope-escape.md
```

Both statements are about one worktree in one run. The report asserted a
measurement its own process had just contradicted — and the reviewer has no
instrument for that, because the claim is doing the work of a measurement
nobody performed. `git status --porcelain` prints nothing when clean and one
line per file when dirty; there is no reading under which five entries are
"empty".

## Chosen shape: A and B **combined**, adjudicated at the seal, recorded never rewritten

The brief offered Shape A (the claim carries its observation inline) and Shape B
(gate facts are the framework's to report, so the report drops them). They are
not alternatives — they are the two halves of ONE sentence: *state only what you
observed; if the framework observed it, name the framework.* The choice made
here is that combination, plus a third decision the brief left open: **what the
framework does when a claim carries no observation.**

**Shape A — the agent-owned claim carries its observation.** A gate claim is
accepted when the sentence carries any of: a pasted output (`output:`), a bash
exit marker (`exit=`, `exit code 0`), a measured transition (`→`, `->`), a
ground-truth path (`/proc/…`), an observation verb (`measures`, `reproduces`,
`printed`, `ran`, …), or the instrument that produced the result (`go test …:
ok` — PROJECT.md Rule 5's own template line).

**Shape B — a framework-owned fact is named, not restated.** The framework
measures the worktree's state at close-out and records it on the row's own
trail. A report that restates it must either carry what it actually saw or name
the framework as the source (`framework report`), which *is* an observation: it
points at the measurement that exists.

**The third decision: the framework RECORDS, it does not rewrite and does not
re-run.** Three rejected alternatives, and why:

| Alternative | Why rejected |
|---|---|
| The seal rewrites/refuses the offending sentence | It would edit the artifact of record — a door silently rewriting a run's own prose. An unchosen door may not touch the report. Also it would put the seal in the business of judging wording, which is not checkable in general. |
| The framework re-runs every gate the report claims | The brief forbids it and it is right: it doubles cost (a full suite per close-out) and measures the wrong thing (the environment now, not what the run saw). |
| A prompt rule ("be honest / state your evidence") | Explicitly rejected by the brief and measured by this repo: *a principle changes nothing; a check does.* The prompt line added here is the teaching half, never the enforcement half. |

What the seal does instead (`internal/tools/task_report.go`,
`taskReportGateClaimRecord`): adjudicates the sealed body, appends the offending
sentences — quoted verbatim — to the row's append-only result digest under
`gate-claims-unobserved: `, prints them on stderr, and leaves the file
byte-identical. The same finding is rendered in `gopi task show` under
`task_report_claim:` (`TaskShowReportClaimLabel`), so the operator meets it on
the surface where reports are actually read, not only in a digest. **The
unobserved claim becomes visible instead of trusted** — the same shape as the
Rule-30 residue record (`completed-unclean`), deliberately: one family of
close-out findings, one mechanism.

## Which text gets adjudicated (and the loophole closed)

The verdict reads, in order:

1. the `## Evidence (self-filed)` section, when the report declares one;
2. otherwise the run's `## Verification` section — **the motivating shape**: the
   measured report had no evidence section at all, its claims sat under
   Verification;
3. otherwise the whole body.

Step 2 is what makes the contract non-optional: a report cannot satisfy it by
simply not writing the section. Step 3 closes the same hole one level down.

## The predicate is conservative on purpose

A sentence is flagged only when either

- it **names a framework-measured ground** (`git status --porcelain`,
  `git status`, `framework report`), asserts a result about it (`empty`, `clean`,
  `nothing`, `no output`, or any result word), and observes nothing; or
- it asserts a result word with **neither** an observation, an observation verb,
  nor the instrument that produced the result.

That ordering is the design, and it is what keeps the motivating run's **TRUE**
claims expressible:

- `red proof reproduces the operator's verbatim `permission denied` and turns
  green with the fix` → observation verb (`reproduces`, `turns`) → accepted.
- `real-child acceptance measures `/proc/<pid>/cgroup` at
  `gopi-takeover-002-518e44ac26a2.scope`` → observation verb + `/proc/` ground
  truth → accepted.

Solving this epic by suppressing evidence would have been the worse defect;
`TestGateClaimAcceptsTheRunsTrueClaims` fails if anyone ever tries.

### The unit is the bullet, and the epic's own report proved it

The first cut split a section into **physical lines**. That is wrong for the very
text this project writes: a bullet whose arrow lands on the marker line and whose
evidence is the next indented line

```
- post-fix run (t+2019s, this branch): the same `-run` selection →
  `ok github.com/the operator/gopi/internal/tools 3.340s`.
```

was judged one line at a time — the instrument and its `ok` sat on the *next*
line — and the claim was flagged although it was measured. It was found the only
way it could be: by pointing the predicate at this epic's own report before
committing (`GateClaimVerdict` on the live `GOPI_TASK_REPORT_PATH`), which
returned exactly that one false positive. The unit is therefore the **bullet**
(marker + its wrapped lines); a separator splits *within* a bullet, and a newline
ends one only when the next line opens a new item. Pinned by
`TestGateClaimAcceptsAWrappedBulletObservation`, which also asserts the
neighbouring bare claim on the same section is still flagged — the fix must not
blind the predicate (Rule 10.5: every fix carries its regression test).

Re-measured after the fix, against this epic's own live report (a throwaway
`internal/selfcheck` package, removed again — Rule 30):

```
SELFCHECK section="## Evidence (self-filed)" missing=0 []
ok  github.com/the operator/gopi/internal/selfcheck  0.049s
```

The live report is the hardest input available (a 6 KB report written for this
very contract, by this very run), and the predicate is now silent on it — so
the false positive is measured gone on the instrument that found it, not merely
argued away.

## Files

- `internal/taskreport/gateclaim.go` (new) — the contract: the evidence section
  constant, the ground/vocabulary sets, `GateClaimVerdict` (pure text),
  `DigestRecord`, `ClaimWarning`. Leaf package: stdlib only, so the writer
  (`internal/tools`) and every future face share ONE definition (Rule 14).
- `internal/taskreport/gateclaim_test.go` (new) — the pure-face red/green pins,
  the measured sentence quoted verbatim.
- `internal/tools/task_report.go` — the seal calls the predicate; the finding
  rides the digest and stderr; `TaskReportShowBlock` renders it in `task show`.
- `internal/tools/task_report_gateclaim_test.go` (new) — the seam tests.
- `internal/tools/zz_redproof_gateclaim_test.go` (new) — the A/B proof (below).
- `internal/taskreport/taskreport.go` — `SectionEvidence`/`EvidencePlaceholder`
  live next to the contract that owns them (Rule 14); `SkeletonSections` emits
  the evidence section after `## Verification` in the harness skeleton;
  `VerificationHeading()` is the one accessor both files read.
- `GOPI.md` — Rule 5's report template gains the `## Evidence (self-filed)`
  section and the measured case, so the project-level contract (which is what a
  task's own GOPI.md-reading agent sees) states it too.
- `devdoc/report-gate-claim.md` — this document.

## The A/B red proof (the failing-then-passing case)

`internal/tools/zz_redproof_gateclaim_test.go` is written against the **pre-fix
API only** (`SealTaskReport` + the digest string it returns) and asserts the
digest prefix as a **literal**, so the identical file compiles on both trees
(the `zz_redproof_reclaim_promise` precedent — production code references the
constant, never the literal).

```
# pre-fix tree (master): FAILS — the claim reaches the reviewer unrecorded
$ AB=$(mktemp -d); git worktree add --detach "$AB" master
$ cp internal/tools/zz_redproof_gateclaim_test.go internal/tools/task_report_test.go "$AB/internal/tools/"
$ (cd "$AB" && go test ./internal/tools/ -run TestZZRedProofUnobservedGateClaim -count=1)
--- FAIL: TestZZRedProofUnobservedGateClaim (0.00s)
    zz_redproof_gateclaim_test.go:74: UNRECORDED CLAIM: the report asserts an empty
    worktree while the same run's close-out says DIRTY, and the row's digest records
    nothing about it: "task-report: …/t-zz-redproof-gateclaim/task-report.md (328 bytes)"
FAIL

# post-fix tree (this branch): PASSES — recorded verbatim, report untouched
$ go test ./internal/tools/ -run TestZZRedProofUnobservedGateClaim -count=1
ok  github.com/the operator/gopi/internal/tools
```

## Verification

See `tasks/<id>-report.md` for the pasted build/vet/suite output of this run.

## Not Done (honest)

- **The seal records; it does not refuse.** A run can still file a report with an
  unobserved gate claim — it is now recorded and shown, not blocked. A refusal
  knob (the `GOPI_TASK_RUN_RESIDUE_ENFORCE` precedent) was considered and left
  out deliberately: this contract's first job is to make the defect visible on
  every existing report, and a refusal that fires on legacy text would be turned
  off within a day. The enforcement point is the operator reading
  `task_report_claim:`.
- **The false-positive boundary is measured on this epic's own report, not proven
  in general.** The predicate is a phrase model over English report prose;
  `gateclaim_test.go` pins the vocabulary this project's templates actually emit
  (Rule 5's build/test/commit lines, the `- ` / `* ` bullets, the old-style
  `## Verification` claims), the grey variants, and — from the live measurement
  above — the wrapped-bullet shape. A report worded unusually may still be
  flagged spuriously. The cost of a false positive is one `task_report_claim:`
  line, never a lost artifact — which is why recording (not refusal) is the right
  default here.
- **Not a general truthfulness gate.** This says nothing about a claim that
  cites a fabricated observation. It closes the measured hole — *the claim's own
  run contradicted it* — and nothing wider.
- **`cmd/agent/merger_takeover_prompt.md`** was pointed at by the brief as a
  prompt surface; the takeover child's own report guidance was left alone
  because the seal adjudicates at the writeback, below every prompt surface, so
  no per-agent prompt edit is needed for the check to hold.
