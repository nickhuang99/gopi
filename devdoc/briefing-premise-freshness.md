<!-- doc-class: normative -->
# Briefing premise freshness — "X is not on master" re-measured at dispatch

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class**: normative (the behaviour it describes is standing: the spawn seam of
every `gopi worker` / `task run` re-measures this class of sentence).
**Task**: 01a12c87-81b6-7942-85de-a8d5b382ee5f, 2026-10-12.

## The measured case (row 01a12c7b)

An epic briefing carried the sentence

> "row 01a12c4a is completed but its fix never landed on master — fix the read
> path in this run's worktree"

rendered from a **snapshot** of the integration branch taken when the row was
queued (base `305ae03d0`, 03:37:55). The identical fix `9867ed461` became an
ancestor of `origin/master` at 03:44:05 (merge `a69601a22`); the run was handed
a 03:41:45 snapshot at 03:58. The run spent its whole budget re-implementing
landed work and then had to deduplicate against a branch nobody had told it
about. Nothing in the pipeline disagreed with the sentence, because the
briefing's text was the only thing asserting anything.

## Root cause (5-why)

1. Why did the run duplicate landed work? — The briefing told it the work was
   missing, and the briefing is the model's only instruction.
2. Why did the briefing say that? — It was rendered from a snapshot of the
   integration branch, taken at **submit/queue** time.
3. Why is that wrong? — The snapshot is a *measurement with a timestamp*, but it
   is carried as *prose with no timestamp*: nothing in the text can go stale,
   because nothing in the text claims to be current.
4. Why did nothing catch it? — No seam re-measured the class of sentence that
   can go stale in this direction. (`internal/tools/master_claim.go` already
   measured the OTHER direction — "master already implements X" — at report/seal
   time.)
5. Why is the seam the right place? — Every executor face funnels through one
   spawn seam (`cmd/agent/cli_tasks.go`, `runTaskRunOnce`), and the briefing
   string is mutated there, immediately before the ONE `cmd.Stdin` binding.

## The fix

Two files, both new; no new tool, no second git prober, no new dependency.

- `internal/taskreport/unlandedclaim.go` — the **mirror** of `implclaim.go`:
  `UnlandedClaimsIn(body, branch)` finds the sentences that assert the
  integration branch does **not** have something, and yields the commit-shaped
  token each names. Same three-way section choice (Evidence → Verification →
  whole body), same sentence unit (`claimSentences`), same commit shape
  (`implClaimShaShape`) — the two readers cannot disagree about where a result
  lives or what a revision looks like.
- `internal/tools/task_briefing_premise.go` — `TaskBriefingPremiseRefresh(repo,
  briefing)` measures each named revision through the **existing**
  `MasterClaimEvidenceAt` (whose verdict is the exit status of
  `git merge-base --is-ancestor <c> master`), and returns a clause to append
  plus one record line per claim examined.
- `cmd/agent/cli_tasks.go` — one block at the spawn seam, inside the
  ground-resolution arm, **after** every existing clause and **before** the
  single `cmd.Stdin` binding.

## Four decisions a later reader should not have to rediscover

1. **Reuse, not re-implement** (Rules 8/13). The ancestry question is already
   owned by `MasterClaimEvidenceAt`. The refresh calls it; it runs no git of its
   own. The branch name is `IntegrationBranchName` (never typed), and the stamp
   renders the same measurement the record reports (measure once, read twice).
2. **Additive, never a refusal** (the `task_claim_ground.go` shape). A premise
   that cannot be measured leaves the briefing untouched and goes to the record
   as a **STATED** line (Rule 15) — `briefing premise → UNMEASURED (…) — the
   premise was NOT checked, which is not a pass`. The run proceeds.
3. **The discriminator is the ENCLOSING punctuation, and that is a real limit.**
   The motivating sentence names a row id and a sha side by side — "The row
   `01a12c4a` … its fix (`9867ed461`)" — and the two have the **same shape**
   (eight hex characters carrying a letter and a digit, which is what
   `implClaimShaShape` accepts). So the reader prefers the **enclosed** token
   (parenthesised or backticked) and falls back to the first shaped token. A
   sentence that names a row id unenclosed and *no* revision measures that row
   id, finds nothing, and is reported UNMEASURED. Stated, not hidden: this is
   the boundary of a text-level detector, and the UNMEASURED arm is exactly
   where it heals (never a false "landed" stamp).
4. **Two claims need two bullets.** `claimSentences` joins a wrapped paragraph
   into ONE sentence, so two premises written as one paragraph are one claim and
   one claim names one revision. This was measured while writing the E2E walk:
   the first version's second premise was silently never examined.

## The contract (invariant)

- The spawn seam (`cmd/agent/cli_tasks.go`, `runTaskRunOnce`) **MUST** re-measure
  every briefing sentence of this class against the run's own ground before the
  briefing is bound to the child's stdin. A briefing handed to a child is a
  measurement taken at the moment of receipt, never a snapshot.
- A claim that **cannot** be measured **MUST NOT** be rendered as a pass: it is
  **STATED** per claim in the seam's record (`UNMEASURED (…) — the premise was
  NOT checked, which is not a pass`), and the run proceeds. The refresh is
  additive by contract: it **SHOULD NOT** refuse a claim, block a run, or fail a
  spawn.
- The verdict is the exit status of `git merge-base --is-ancestor <c> master`,
  taken through `MasterClaimEvidenceAt` (`internal/tools/master_claim.go`) —
  the single source of truth for this question, which **MUST NOT** be
  re-implemented as a second git prober.
- The stamp **MUST** name the revision, the branch, the measured exit status and
  the instruction (`VERIFY ONLY: do NOT re-implement`); a revision that is
  **NOT** an ancestor **MUST NOT** receive a "landed" stamp, because a guard that
  stamps everything is as useless as one that stamps nothing.

This invariant is enforced by tests rather than prose — the red proof, the
positive control and the stated-unmeasurable arm live in
`internal/tools/task_briefing_premise_test.go` and
`internal/taskreport/unlandedclaim_test.go`, and the surface itself is measured
by `cmd/agent/task_briefing_premise_e2e_test.go` reading the child's first model
request.

## What this does NOT do

- It does not detect a **genuinely open** premise as anything but open: a
  revision that is not an ancestor is recorded and **nothing is stamped** (the
  positive control — a guard that stamps everything is as useless as one that
  stamps nothing).
- It does not fix the briefing's *rendering* side (the submit-time snapshot);
  it makes the stale premise **correct at the moment of receipt** instead.
- It does not cover a row dispatched with **no ground** (`ground.Dir == ""`):
  the refresh lives inside that arm, because that is where the run's own repo is
  resolved. A repo-less row's briefing is left as it was — stated in the report,
  not papered over.
- It does not measure a premise naming a revision the repo does not have (a row
  from a peer's ledger, a pruned commit): that is the UNMEASURED arm.

## Evidence

- `go build ./...` → rc=0.
- `make test-budget PKGS='./internal/taskreport ./internal/tools ./cmd/agent'` →
  see `## Verification` of the task report.
- Red proof + positive control + unmeasurable arm:
  `go test ./internal/tools/ -run TestBriefingPremise` and
  `go test ./internal/taskreport/ -run UnlandedClaim`.
- The surface, run for real (Rule 31):
  `go test ./cmd/agent/ -run TestWorkerStampsAStaleBriefingPremiseForTheChild`,
  which reads the stamp out of the CHILD's first model request and the two
  verdicts out of the run's `output.log`.
