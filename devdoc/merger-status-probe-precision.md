<!-- doc-class: historical -->
# merger --status: state only what the probes measured

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a0b1d9-d79e-727a-bb27-a0ef55e61321` ("merger inaccurate statement"),
branch `fix/merger-status-liveness-precise`, commit `f6c6986a`.

## The complaint, reproduced

`gopi merger --status` printed, for three worktree-held branches of
`/home/user/work/GoPi`:

```
- fix/scratch-shm-role-split — NOT a candidate: a checkout holds it
  (/home/user/workspace/gopi-shm-role) and NO live process is inside that
  checkout — task 01a0b1b2-…; the merger can merge it only after that checkout
  is released
monitor:   3 of 4 head(s) sit in a checkout: 3 with no process inside
           (those runs are over — each branch's remedy is named above)
```

`ps aux | grep gopi` showed, at the same moment, a live `gopi worker` for every
one of those three task ids (`662639` // `01a0b1b2`, `662694` // `01a0b1b4`,
`659404` // `01a0b1c5`). The runs were not over.

## Root cause (measured, not inferred)

The occupancy probe is `scratch.HeldByProcesses` (`internal/scratch/holder.go:276`)
— it asks whether any process holds a path **inside** the checkout (cwd / open
fd / exe / mapped file). It is a correct probe of *the checkout*, and it is
blind to *the run*:

```
$ readlink /proc/662639/cwd
/home/user/.gopi/sessions/01a0b1b2-3995-7a9e-b390-ae9aaf839f38
```

A `gopi worker <task-id>` runs with its **session directory** as its cwd; its
tool grandchildren carry the worktree. When those children exit (between tool
calls — the normal state), no process stands in the checkout while the run is
very much alive. The renderer then welded that empty scan to the ledger's
verdict and stated the conclusion *"the run is over"* — a claim the probe never
made (Rule 15: an unmeasured fact must never render as a measured negative).

The genuine signal was already in the ledger and already printed by
`gopi task show`: the row's recorded runner identity (pid + kernel start ticks),
the same three-factor identity the reclaim/finish arms use.

## The fix — three axes, each stating only its own probe

| axis | source | renders as |
|---|---|---|
| checkout occupancy | `scratch.HeldByProcesses` | `a live process IS working in that checkout (pid 714241 (cwd …); …)` or `the scan found no process inside that checkout` or `whether a process is inside could not be measured (the /proc scan was unreadable)` |
| task-row runner | the row's recorded `pid` + `start ticks`, via the existing `finishCleanupProcessHolderLive` | `the task row's recorded runner is still alive` / `is NOT alive` / `records no runner identity (not verified)` |
| checkout age | git's own registration `.git/worktrees/<id>/gitdir` mtime | `the checkout has stood for 28m` |

All three reach the operator on the same line, and the disposition block names
**which** evidence carried the verdict (`withheldReasonLiveRunnerClaimOnly`
exists so a claim-stamp-only ownership is not restated as a live run).

The monitor line no longer infers a lifecycle:

```
monitor:   4 of 7 head(s) sit in a checkout: 0 with no process inside and 4
           being worked in right now — a checkout with no process inside is NOT
           proof its run ended (see each line's task-row clause)
```

The operator's own bottom-line instruction — *"no info is better than wrong
info; if unsure, just don't say it"* — is implemented as: an unmeasured age
prints **nothing** (never `0s`), an unrecorded runner identity prints
`(not verified)` (never "not alive"), an unreadable `/proc` prints
`could not be measured` (never "nobody inside").

### Ordering defect found in the recovered work (my only functional change)

`checkoutClause` tested `!ProbeMeasured` **before** `Held`, so a *positive* hold
— the strongest evidence there is — rendered as "could not be measured". Held is
now tested first: a positive observation is its own proof; the flag decides only
between the two negative readings.

## Provenance — this work was recovered, not written from scratch

An earlier run, task `01a0b182` ("merger status text misleading", source human),
had already written ~484 lines implementing exactly these three axes. It
**never committed them**: they sat as uncommitted tracked changes in the main
repo's `master` working tree, while its own branch
(`fix/merger-status-liveness`) pointed at an unmodified base — so the branch was
invisible to the merge queue while its row read `completed`. That is the Rule 30
failure mode in its worst shape.

This run: recovered the diff onto its own fresh branch (Rule 23 — fresh branch,
never continue on someone else's), ran the tests, fixed the one ordering defect
and the three fixture lines the prior run had left un-updated, verified against
the live repo, and committed. The prior work is credited here; the
commit is the first time it existed in any ref.

Protocol note: the recovered source was read with `git diff` from the main repo
(Read-only). Nothing in `/home/user/work/GoPi` was committed, stashed, reverted
or deleted — those uncommitted changes remain exactly where their owner left
them, and that leftover is itself evidence for the report.

## The epic's three remedies, honestly accounted

1. **Claim-time branch/worktree stamping on the task row.** NOT done here, and
   **no new task submitted**: three equivalent rows are already pending in the
   queue from the prior epic — `01a0b1b1-2a7e-7c17` ("claim path: record the
   claimed branch+worktree on the task row at creation time (closes merger's
   branch↔task gap)"), `01a0b1ab-935f-7736` ("Redo [2/5] stamp the
   branch↔worktree linkage at CLAIM time"). Submitting a fourth would run the
   same work twice (Rule 26 permits duplication for *repair*, not for an
   already-queued identical row).
2. **The merger queries the ledger for liveness.** CONFIRMED as the operator
   suspected — `gopi task show` already prints the runner identity — and now
   IMPLEMENTED: the render reads the row's recorded pid + start ticks through
   the ONE existing implementation (`finishCleanupProcessHolderLive`); no second
   `/proc` reader was added. The shared-helper rows (`01a0b1b1-2c57-7307`,
   `01a0b1ab-9130-703f`) remain pending; the consumer half is done.
3. **Concise, precise bottom line; omit rather than guess; report age.** DONE —
   see the table above; every unmeasured fact is either stated as unmeasured or
   omitted, and the checkout age is printed on every withheld line.

## Files

- `internal/scratch/holder.go` — `ProcReadable()` ("the scan could not run" is
  distinct from "the scan found nobody"), `processInside` naming the channel.
- `internal/tools/branch_attribution.go` — the row axis
  (`branchAttributionRunnerLiveness`), the checkout age
  (`branchAttributionCheckoutAge`), evidence names carried on the verdict.
- `internal/tools/merger_status_candidate.go` — the candidate line, monitor line,
  and the clause constants (no inline literals — Rule 1b).
- `internal/tools/merger_status_disposition.go` — `withheldReasonFor` now takes
  the row axis; new reason constants; age prefix.
- `internal/tools/repo_sweep.go` — `BranchAttributionLine` carries
  `ProbeMeasured` / `Inside` / `RunnerKnown` / `RunnerLive` / `Age`.
- tests: `internal/tools/merger_status_candidate_test.go`,
  `internal/tools/merger_status_trust_test.go`.

## Verification

See the task's report block and the shell evidence captured this run; the live
end-to-end check is `bin/gopi merger --status` in the main repo, whose
`GoPi` block now names, per branch, the holder pids, the row's runner liveness
and the checkout age — and whose monitor line counts probes instead of
lifecycles.
