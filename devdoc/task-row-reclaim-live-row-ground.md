<!-- doc-class: historical -->

> **NOTE 2026-10-26 — the verb this page is about NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left where it is. The defect analysis below is kept as history
> (the Rule 15 lesson is still live in other verbs), but no command here can be re-run.
> Current behaviour: `devdoc/zombie-sweep-removed.md`.

# The dead-holder arm freed the ground of rows that were still OPEN

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a131d297ae7ad3a504b2ae2c7d0a43` · **Branch:** `task/01a131d297ae7ad3a504b2ae2c7d0a43`
**Baseline:** `17fe68d21` · **Incident:** 2026-10-12T16:28:52Z → 16:39:02Z

## The incident

The task-row-reclaim removal arm (`ReclaimDeadHolderWorktrees` →
`reclaimDisposition`, `internal/tools/task_reclaim.go`) freed the worktree ground of
ten rows. Three of them — `01a130f0`, `01a130ee`, `01a130e4` — the ledger still reads
`in_progress` **to this day**. The arm's own audit record named the disposition
`reclaimFreable past the live-session-veto channel`.

## Root cause (5-why)

1. **Why did the row stay open?** The arm freed the DIRECTORY and wrote nothing about
   the ROW. A live status means no run ever wrote a completion — the completion *is* the
   status transition.
2. **Why was that allowed?** The disposition ends at `reclaimFreable`, and the caller's
   live-status half dismissed the row with a tag: `reclaimOrphanRowNote`, whose text was
   literally *"directory freed, row still open"*. The note was the **disclosure standing
   in for the missing step** (Rule 15 — silence was not the defect; naming a stranding
   instead of preventing it was).
3. **Why was the note considered enough?** Because the orphan arm
   (`reclaimDeadLocalHolderRowsFrom`, epic of 2026-09-12) had deliberately reversed the
   earlier stance that a live-status row is never reclaimable. That epic is *correct* and
   its reason is real: these directories — and the branch that `scripts/safe-merge.sh`
   gate [c] refuses while the directory is occupied — stayed parked until a human ran
   `gopi task sweep-zombies --kill`. Freeing them is the contract
   (`TestDeadHolderReclaimFreesALiveStatusOrphan`,
   `TestAcceptance_DeadLocalHolderOrphanReleasesItsDirectory`, and the acceptance chain
   that ends with safe-merge exiting 0). **I first implemented a refusal and it went red
   against those three tests — that red is what proved refusal is the wrong remedy.**
4. **Why did nothing else close the row in time?** `SweepZombies` has a **1800s stall
   grace** (measured: `gopi task sweep-zombies --dry-run` → `no zombie rows (stall grace
   1800s, max-run 0s)`). The reclaim fired ~7 minutes after the claim, so the row was
   left live with its ground gone, and nothing on the host would have ended it.
5. **Why is "ground gone + row open" unrecoverable?** Every resume spelling needs the
   ground — the default positional target resolves the row's *recorded worktree*, and
   `--branch` / `--worktree` name it outright. And the sibling arm whose whole job is to
   *continue* an interrupted run (`ResumeDeadHolderRuns`) refuses once the directory is
   gone. The row is neither finished nor resumable nor re-dispatchable.

**The one-sentence defect:** *"the row's holder is provably DEAD" was consumed as "the
row is finished".* They are different claims — the pid layer proves the first, only the
ledger's own completion write proves the second.

## The fix: co-death, not refusal

> **The pass ENDS the attempt and frees the ground in the same act — or it does neither.**

`reclaimDeadHolderCoDeath` (`internal/tools/task_reclaim.go`) runs immediately before the
removal and **gates it**. For a row still in a live status it writes the terminal
`failed` through the ledger's ONE mutation core (`TaskUpdateReason`), then the pass frees
the directory. If the write fails — including a transition the table forbids — the
removal is **skipped** and the line says why, so the incident's state is never produced.
The row's new status is carried back into the report line and the removal-audit record.

**Why `failed` and not a resume state.** `taskAllowedTransitions` (`internal/tools/tasks.go`)
has **no path from a live status back to a resumable one**: `in_progress` leaves only for
terminal, and the ledger's own word for "this attempt ended, the work must run again" is
`failed` + a REISSUED pending row — the table's own comment is *"the rework itself is a NEW
pending row"* (`claimed → pending` is explicitly illegal). `failed` is the one terminal
state that both closes the row and makes re-dispatch reachable. The briefing's option
"write the row back to a resumable/redispatchable state first" is therefore **unreachable
as literally stated**, and `failed` + reissue is the ledger's existing expression of it.

**What is deliberately NOT changed:** the terminal half of the arm (a terminal row's
ground is still freed — that withholding is the reason this file exists), the release of
an orphaned dead-holder row's directory (the 2026-09-12 contract), the live-runner veto
(`reclaimForeignRunnerRefusal`, asked before the disposition and still naming the foreign
pid), and `guestRowOwnCheckoutRefusal` / `guestRowReleaseRefusal` /
`reclaimLiveSessionRefusal`.

## Defect ② — the audit wording, with the measurement

**Measured:** `worktreeSweepSessionLiveEvidenceFor` (`internal/tools/worktree_sweep.go:1322`)
opens with `if !taskLiveHolderStatus(status) { return "" }`, and `reclaimLiveSessionRefusal`
calls it first.

- On **this arm's own path** (terminal rows) the channel is consulted and returns `""`
  precisely because the row is terminal — its live-status arm is not applicable.
- On the **incident's rows** (`in_progress`) the channel *was* applicable and consulted by
  the same Guard ⓪; it answered `""` because the claim pid was dead and no live process
  occupied the row's session directory. It raised no objection because it **had none to
  raise**: it answers *"is a run of this row still alive?"*, and it can never answer *"is
  this row finished?"*.

So the old sentence **"past the live-session-veto channel"** was prose that read as
*"this arm BYPASSED the veto"* while meaning *"the channel was consulted and did not
refuse"* — the two are indistinguishable to an operator, which is exactly how a
post-mortem gets misled (the reporter read it as an admission of bypass). The record now
states which of the two happened, and names why the channel could not object:

```
dead-holder disposition <disp> — decided only after the live-session-veto channel was
consulted and did not refuse (<why>); row status <status> is terminal, so the channel's
live-status arm was not applicable (worktreeSweepSessionLiveEvidenceFor returns no
evidence for a terminal row); checklist <…>
```

## Verification

- **Red proof** (mutation, then restore verified by blob hash): stubbing
  `reclaimDeadHolderCoDeath`'s live-status branch makes all three named tests red —
  `TestRowReclaimCoDeathFreesTheGroundAndClosesTheRow` reporting
  *"a dead holder's live-status row must be CLOSED by the same pass that frees its ground,
  got status in_progress"*, plus `TestRowReclaimClosesEveryLiveStatusItFrees` and
  `TestRowReclaimNeverStrandsALiveStatusRow`. Restore measured with
  `git hash-object internal/tools/task_reclaim.go` == `25ef6f161dff61c0132f42801145fff4a30277e0`
  (the pre-stub blob); green afterwards.
- **Rule 31, end to end with real verbs** — not `-h`, not a unit test. A real repo, a real
  clean linked worktree, and a row put in the incident's shape through real verbs
  (`gopi task submit`, `gopi task update --status in_progress`) with a claim stamp naming a
  pid that is provably dead (`kill -0` → gone). Then the real surface that carries the leg,
  in a sandbox profile (`GOPI_HOME` + `GOPI_SETTINGS` into it):
  `gopi worker --once` — with the pre-fix binary the directory is **gone** and the row
  still reads `in_progress`; with the fixed binary the same pass prints the co-death line
  and the row is closed. The A/B is the evidence.
- `go build ./...`, `go vet`, and `make test-budget PKGS='./internal/tools ./internal/removalaudit ./cmd/agent'` — see `task-report.md` for the recorded run.

## Not done (honest)

- **No change to `SweepZombies`' grace or to `taskAllowedTransitions`.** Adding a live→
  resumable edge is a ledger-wide semantic change with no consumer asking for it, and the
  reissue-by-new-row rule is deliberate append-only design (`briefings/brief-failed-rows-are-silent-forever.md`
  records a direct instruction not to widen that table). Co-death expresses the same
  intent through the existing writers instead.
- **The three incident rows were not repaired.** They belong to another host's ledger
  (`hostb-Laptop`); this fix prevents recurrence and does not retroactively close them —
  they need a human close-out there.
- **A live-status row whose closing write is refused** (e.g. a future guard) now keeps its
  directory *forever* rather than being freed-then-stranded. That is the deliberate
  trade: a parked directory is recoverable, a stranded row is not.

## Files

- `internal/tools/task_reclaim.go` — `reclaimDeadHolderCoDeath` (new), the gated call site,
  `reclaimOrphanRowNote` retagged, the removal-audit wording.
- `internal/tools/task_row_reclaim_live_row_ground_test.go` — the regression guards.
