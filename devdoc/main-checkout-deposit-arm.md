<!-- doc-class: normative -->
# The main-checkout deposit arm — a row's writes into the SHARED checkout stop being invisible

Epic `01a0db25` (follow-up to `01a0db1e`'s main-checkout pollution on HP).
Standing claim about master: a run whose OWN briefing names the repo's **main
checkout** can no longer deposit there *silently* — the close-out state gate
measures that checkout, attributes what it can prove, and records it on the
row's trail — and the briefing hands the run's linked worktree top billing as
the ONLY tree it writes.

## The measured defect (2026-09-26)

Live row `01a0dabe-3fa1-7cc8-b601-66f1456b8ffd` was handed a linked worktree
(the ground announcement said so) and *still* wrote into the repo's main
checkout, because its payload was the operative statement about where work goes:

```
Repo: /home/user/work/GoPi (branch master at 4c802614). Re-verify every line
number before relying on it.
```

That payload contains exactly **one** absolute path, and it is the shared
checkout. The run honoured it for the two things a run must write — the pixel
evidence (`devdoc/evidence/tui-two-defects/`) and a new regression test
(`internal/uiclient/live_emphasis_attr_test.go`) — and close-out noticed
nothing, for two structural reasons:

1. **`reclaimRunResidueCheck` measures the row's OWN recorded worktree.** That
   is Rule 30's ground, and it was clean: the deposit was in a *different* tree.
2. **`reclaimRunUnattributableGround` excludes the main checkout BY
   CONSTRUCTION** — correctly. The shared tree has the operator's and every
   sibling run's files in it; a close-out that blamed whatever it found there
   on whichever row happened to finish next would be evidence nobody has.

Root cause (5-why): the briefing said **WHERE** the ground is, and never said
which tree has **AUTHORITY** over the run's writes — so a single payload
sentence ("Repo: <shared checkout>") outranked it. No producer renders that
"Repo: …" line (it is submitter text), so there is nothing to fix at the
producer: the seam that already resolves the main checkout
(`TaskGroundResolution.MainCheckout`, used for the placement assertion) is the
one place that can notice it.

## What landed

### (a) The briefing clause — `TaskBriefingAuthorityClause`

`internal/tools/task_ground_advisory.go` renders the clause;
`cmd/agent/cli_tasks.go` appends it **after** the ground announcement (ordering
is the requirement: the last word on the subject is the authority) and into the
same string bound to the child's stdin.

```
[Your tree — the ONLY one you write]
Your worktree is <ground>. That is the ONLY tree this run writes: every file …
Any OTHER absolute path in this briefing is a REFERENCE for reading, never a
place to write — in particular <main checkout> is the repo's MAIN checkout,
shared with the operator and with other running tasks; a write there is
invisible to this run's branch and cannot be merged.
If this briefing's text (or Rule 6a) tells you to create a worktree or to write
into the repo, THIS clause wins: the worktree named above already IS your
ground …
```

Two spellings and no third: `taskBriefingAuthorityFmt` (the shared checkout was
resolved) and `taskBriefingAuthorityNoMainFmt` (it was not — the authority
statement does not depend on naming the other tree, and a clause naming a
directory we could not resolve is worse than no clause).

### (b) The close-out arm — `TaskMainCheckoutDepositRecord`

`internal/tools/task_main_checkout_deposit.go`, called once from `taskUpdate`'s
state gate (the switch that already owns "a delivery claim" semantics, so every
door means the same thing). It is a **RECORD, never a refusal**, and the reason
is structural, not stylistic:

- **Precondition = a measurement, not a convention.** The arm runs ONLY for a
  row whose own briefing text NAMES the shared checkout (`TaskBriefingNamesDir`:
  a token-bound absolute-path scan, so a sibling `…-GoPi-old` is never read as
  `…/GoPi`, and a path *inside* the checkout counts as naming it). Measured:
  `01a0dabe` named `/home/user/work/GoPi`; that is what separates "this run was
  pointed at the shared tree" from "the operator has scratch in it".
- **Candidates are the UNTRACKED entries** of `git -C <main> status --porcelain`.
  A tracked change in the shared tree is somebody's uncommitted work, and the
  epic's prohibition on touching that guard family is absolute.
- **Two proofs, each quoted in the note**: (i) the entry's absolute path appears
  in the row's briefing, or (ii) the entry's newest mtime falls inside the row's
  window (`in_progress_at` → `claimed_at` → `created_at`). Proof (ii) exists
  because the measured payload names the checkout and NONE of the files the run
  would write — a path-only arm would have been silent on the very case that
  motivated it. A directory entry is walked: git reports `devdoc/evidence/…/`
  once, and the proof of a hand is a FILE inside it.
- **Honest residual, stated in the code**: the window proof is a TIME bound, not
  a lineage claim. A live sibling writing into the shared tree during this row's
  window is indistinguishable by mtime alone. The record therefore states its
  basis, so an operator who knows better can falsify it from the sentence.
- **It holds no mutating call**: no delete, move, git write or chmod. The
  red/green test asserts the checkout is byte-identical across the close-out
  (`git status --porcelain` before == after), so a "cleaning" extension cannot
  be added by accident — it would fail that assertion.
- **Idempotent** (`reclaimMainCheckoutDepositFiled`, the family's own event
  stem) and **silent** for the ordinary row: no repo, no resolvable checkout, or
  a briefing that never names it → no measurement, no record.

Event: `completed-unclean-main-checkout-deposit` (the family stem plus its own
suffix, so the existing `event LIKE 'completed-unclean%'` probes cover it).

### (c) A family bug the new event exposed (fixed here)

`reclaimRunResidueFiled` matched the family by `event LIKE 'completed-unclean%'`
with no exclusion — so a record written at the **state gate** (this arm) made
the run's own-worktree residue record look "already filed" and suppressed it.
`LIKE 'completed-unclean%' AND event <> ?` (the sibling, named) now excludes it,
documented at `reclaimRunResidueEvent`. Without this, landing an arm that says
*more* would have made the arm that says *less* stop saying anything.

## The red-then-green proof

`internal/tools/zz_redproof_main_checkout_deposit_test.go` is written against the
**pre-fix API only** and defines its own fixture helpers, so the SAME file
compiles on master and on this branch and the two runs differ by the fix alone:

The scratch directory comes from the scratch QUEUE (`scripts/lib/tmp-ban.sh`,
off `/` and age-GC'd by the Go sweeps — Rule 22), never from a path typed into
the shell: a placeholder like `<scratch>` is not a directory, and the run that
read this recipe on 2026-09-26 substituted `~/workspace/gopi-maindep-red` for
it and left a REGISTERED linked worktree of the shared repo standing there —
clutter no sweep collects (see `devdoc/scratch-repo-residue-01a0db54.md`).

```
$ SCRATCH="$(bash -c 'source scripts/lib/tmp-ban.sh && gopi_scratch_mkdir maindep-red')"
$ git worktree add --detach "$SCRATCH" 92d5d492    # master
$ cp internal/tools/zz_redproof_main_checkout_deposit_test.go "$SCRATCH/internal/tools/"
$ cd "$SCRATCH" && go test -count=1 -run TestZZRedProof_MainCheckoutDeposit ./internal/tools/
--- FAIL: TestZZRedProof_MainCheckoutDepositIsRecordedForTheRowThatNamedIt
    UNATTRIBUTED DEPOSIT: the run closed out `completed` after writing
    devdoc/evidence/tui-two-defects/ and internal/uiclient/live_emphasis_attr_test.go
    into the repo's MAIN checkout … and NOTHING on the row's trail says so
    (reproduction: row 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd)

$ cd <this branch> && go test -count=1 -run TestZZRedProof_MainCheckoutDeposit ./internal/tools/
ok  	github.com/the operator/gopi/internal/tools

$ git worktree remove --force "$SCRATCH"    # the measurement is reproducible; leave nothing registered
ok  	git worktree list shows only the repo and this run's own worktree
```

The reproduction row and the two deposits are named in the test's failure
message, so the red is traceable from the test to the ledger to the deposits.

## What is deliberately NOT here

- **No clean-up, ever.** The arm cannot delete another run's or the operator's
  file, and no spelling of "probably scratch, safe to delete" was added.
- **No refusal of a writeback.** An arm that REFUSED `completed` on a shared
  checkout it merely *suspects* would let a sibling's file block an unrelated
  run.
- **No arm for a row that neither named the shared tree nor recorded it as the
  run's own launch directory**, which would have converted coincidental mtime
  overlap into blame for every live run on the host.
- **Superseded in one respect (2026-09-26, epic row `01a0dbe4`):** the shape where
  the row's `deliverable.repo` IS the shared checkout but its briefing is SILENT
  used to record nothing here — the predicate `TaskBriefingNamesDir` was false by
  construction, which is why the run that invented `<repo>/scratch/` went
  unrecorded. `reclaimMainCheckoutDoor` in `internal/tools/task_main_checkout_deposit.go`
  now reaches that shape by a SECOND door whose proof is LINEAGE (the run's own
  session recorded that checkout as its launch directory), never a bare timestamp.
  See `devdoc/main-checkout-deposit-repo-door.md`.

## References

- `internal/tools/task_main_checkout_deposit.go` — the arm
- `internal/tools/task_ground_advisory.go` — the authority clause + `TaskBriefingNamesDir`
- `cmd/agent/cli_tasks.go` — where the clause is appended (after the announcement)
- `internal/tools/zz_redproof_main_checkout_deposit_test.go` — the red-then-green driver
- `devdoc/repo-hygiene-main-checkout-pollution.md` (historical) — the 2026-09 pollution this closes the remaining door on
- `devdoc/merge-main-checkout-master.md` — the other main-checkout family (a landing must restore master first)
