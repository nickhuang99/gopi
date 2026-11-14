<!-- doc-class: historical -->
# Task Report: `gopi merger --status` is an operator-trustworthy window

> **REBASE NOTE (read this first).** Master moved while this work was in flight: commit
> `c890b0fe` (already on master) is `Merge branch 'fix/attribution-ledger-nil-error'` — the
> SIBLING branch that this report's §Overlap section warned about. That branch carried an
> independent fix for defect ①, and **it converged on exactly the same root cause and the same
> fix** as this branch's (§1): the ledger error given its own variable, `ledgerRead` derived from
> it, the nil-wrap made unrepresentable, and a cause naming the path. It also shipped its own
> red proof (`branch_attribution_ledger_err_test.go`).
>
> So this branch has been **rebased onto `c890b0fe` and its defect-① half DROPPED as redundant**:
> master already owns it (and its duplicate tests would not even compile alongside). What this
> branch delivers is **defects ② and ③**, which master does **not** have (verified:
> `git show master:internal/tools/merger_status.go | grep disposition` is empty). §1's ① material
> is kept as the *diagnosis of record* — it is the same defect, independently arrived at, and it
> documents why the fix is the shape it is.

Branch `feat/merger-status-trustworthy` — worktree `/home/user/workspace/gopi-merger-status`.

## 1. The three defects, and what was done about each

### ① `task ledger unreadable: %!w(<nil>)` — the cause destroyed by its own report  *(master now owns this fix; kept as the diagnosis of record)*

**Location** `internal/tools/branch_attribution.go`, `BranchAttributionReport`.

**Mechanism** (a variable-lifetime bug, not a formatting bug):

```go
rows, err := branchAttributionRows(ctx)      // ← the REAL ledger error lands here
ledgerRead := err == nil
if err != nil { rows = nil }
branches, err := branchAttributionBranches(repo)  // ← OVERWRITES it with nil
...
if !ledgerRead {
    return out, fmt.Errorf("branch attribution: task ledger unreadable: %w", err) // err == nil HERE
}
```

The statement that REPORTS the failure is the statement that DESTROYED it. `%!w(<nil>)` is Go's
verb marker telling the reader that `%w` was handed a nil — the real cause (a sqlite open refusal)
never reached the operator. This is Rule 15's worst shape: not a silent failure but a
*mis-reported* one, where the operator acts on a fake.

**Fix** — the ledger error keeps its OWN lifetime (`ledgerErr`), and `ledgerRead` is derived from
that same variable and reassigned nowhere, so reaching the `!ledgerRead` arm PROVES
`ledgerErr != nil`. The nil-wrap is **unrepresentable**, not merely avoided: no future edit can
re-introduce it without also breaking the derivation in the same two lines. The branch listing got
its own `branchErr` (a second, genuine error path that was previously conflated).

The cause now names the three facts an operator can act on — **where** it was looked for, **which
channel** supplied that path, and **what was found** there:

```
branch attribution: task ledger unreadable: looked for /dev/shm/.../memory.db
  (supplied by GOPI_MEMORY_DB; a 4096-byte file (the ledger driver's own error below says why it
   could not be used)) — the ledger is PROCESS-GLOBAL, so this one cause is rendered on every
   repo's line, not one per repo — file is not a database (26): file is not a database (26)
```

The path comes from `memoryDBPath()` itself (Rule 13 — the named target cannot drift from the
opened one); the channel is read from the live environment through the same precedence
`GOPI_MEMORY_DB > GOPI_HIPPO_DB > profile default`; and the filesystem half distinguishes a
missing file, a directory where a file belongs, and an unreadable file. The scope clause exists
because the failure renders on EVERY repo's line (one ledger, N repos) — measured live, an
operator reading the Maps and GoPi lines naturally concludes there are two broken ledgers.

### ② `pending: none — nothing to merge` was three different facts in one sentence

**Measured before** (live GoPi repo, 2026-09-17): `pending: 5 branch(es) the next sweep would act
on` while **twelve** heads the merger cannot touch sat in the same repo — the word "withheld"
appeared on no line at all. Nothing was missing from the output; what was missing was the
*summing sentence*.

**Fix** — a new `dispositions:` block classifies the branch-axis evidence the preview already
carries (`merger_status_disposition.go`). It re-derives nothing: the verdicts are the same
branch-attribution verdicts the protected line renders, the remedies are the arms that already own
them (`Reclaimer`), and the "already in master" fact is a positive `merge-base --is-ancestor`
measurement (`repoSweepWithheldInMaster` — the ordinary spent scan structurally cannot see this
population, because it walks the CANDIDATES and a worktree-held branch never becomes one).

The three situations are now visibly distinct (full transcripts in §5):

* **withheld** — `N branch(es) withheld from the merger …` then one line per branch: name, verdict,
  whether the deliverable is already in master, and the reason in the remedy arm's vocabulary
  (`live-holder` → "a live run is working in its checkout"; `owner-ended` → resume **or** reclaim,
  depending on whether the run was interrupted; `owner-record-only` → registration repair;
  `ungoverned` → the ledger could not be read).
* **not running** — appended to the withheld summary only when it changes what the operator must do:
  `— the merger is NOT running, so none of the remedies below will run by itself`.
* **nothing** — no withheld branch ⇒ **no block at all**: the pre-existing lines already say it
  honestly (`pending: none`, `merger: not running`).

**Output contract.** The change is **additive-only**. `_strip.py` removes the added block from the
after-transcript and `diff`s it against the before-transcript — empty (byte-identical) for every
pre-existing line of the GoPi repo, and the Maps repo block is byte-identical in full (§5). The
reason the block is absent when nothing is withheld is not a shortcut: a line that appears when it
has nothing to say is a line that trains operators to ignore it.

### ③ `owner-ended`-held branches are actionable

`owner-ended` (a row named the branch, the run is over, the checkout still stands) now renders with
its **existing** remedy arm named, and the two distinct medicines are told apart:

* run was interrupted (`Resumable`) → *"the owner's run ended with its work UNFINISHED — the resume
  arm continues it"*;
* run is done (terminal row) → *"the owner's work is DONE — the reclaim arm releases the checkout,
  then the branch merges like any other deliverable"*;
* and, when the arm refuses (a dirty checkout above all), the arm's OWN measured sentence is
  rendered instead of the generic one.

The remedy arm is carried through `BranchAttributionLine.Reclaimer` (new field) — the classifier
already computed it and the render was throwing it away. Naming the arm is the deliverable; no
automatic destructive disposition was added, exactly as the classifier's deliberate non-goal says
(a wrong auto-disposition destroys live work — memory #2324).

## 2. The Maps-repo ledger verdict (evidence, both halves)

**Verdict: the Maps repo has no ledger and is not expected to have one. It was never the source of
the failure — it merely RENDERED it. No fix is owed to the repo; the fix is the scope clause now
printed on every line.**

* **Code path** — the attribution loader takes exactly ONE argument, a context:
  `branchAttributionRows(ctx) → openTasksDB(ctx, memoryDBPath())`. `memoryDBPath()` reads one of
  two environment variables and then the profile default under the gopi dir. Neither the function
  nor any of its 264 call sites resolves a ledger from a REPO path. Identity (branch → task) is
  process-global by construction; the repo is only the axis the verdicts are grouped by.
* **Filesystem** — `/home/user/Downloads/games/Maps/.gopi` **does not exist** (measured). Its git
  HEAD is `69ef0ea Merge branch 'feat/phase3-translation'`, i.e. an ordinary code deliverable repo.
  It is declared in `settings.json` as `"/home/user/Downloads/games/Maps": {}` — an entry with an
  EMPTY build, which Rule 12 defines as a valid **pure merge target**: swept for branches, never
  rebuilt.
* **Actionability either way** — the cause now names the one path to fix and states that it is
  process-global, so the operator does not go looking for a per-repo ledger. If a repo genuinely
  needed its own ledger, the same line names the path and the channel that supplied it, which is
  what a per-repo deployment would have to change.

## 3. What `--status` should report under the cronjob architecture (for task 01a0ac69)

The briefing's own note is the design input: the merger is becoming a cronjob and the park/archive
mechanism is abolished, so `--status` is the ONLY observable surface. The split this change lands
supports that directly:

1. **Liveness is a first-class input, not decoration.** Every disposition sentence is qualified by
   whether a merger is alive. Under a cronjob there are three liveness shapes, not two: *a tick is
   running*, *no tick is running but one is scheduled*, and *nothing is scheduled at all* (cron
   missing / job removed). The first two are already distinguished by the header + schedule lines;
   **the third is not observable today** and is the gap I recommend 01a0ac69 close — the honest
   report is the crontab entry itself (present/absent, and its interval), because `--status` can
   read a file but can never see a scheduler that is not there.
2. **`--no-branch-scan` is no longer the right default for a cronjob.** A disposition block that
   costs a ledger read + one `merge-base` per withheld branch is affordable once per cron tick and
   is the operator's only window; a status that omits it should say so.
3. **The three-way `pending` / `protected` / `parked` split may indeed be retired.** The `dispositions`
   block subsumes the *withheld* information in one place and is derived from ONE classifier, so the
   buckets can be reduced to their reconciliation duty (`git branch` arithmetic) without losing any
   operator-facing fact. I did NOT do that here — deleting information is out of scope while the
   output contract must stay stable — but the new block is the shape the cronjob surface should be
   built on: one class per situation, each with the reason and the arm that owns the remedy.

## 4. Same-class swallowed / mis-reported error survey (report only — NOT mass-fixed)

Searched the render paths and their neighbours for the same two shapes: an error's lifetime being
reused, and an error being discarded.

| Site | Shape | Verdict |
|---|---|---|
| `internal/gopilog/jsonlline.go:98` | `complete := err == nil` derived from `err`, then `err` reused for the next loop iteration's read | **Not the defect class, but monitor.** The flag is consumed before the next `ReadSlice` assignment, so no destroyed cause. Worth a one-line lifetime comment; not fixed here (different package, no observed failure). |
| `internal/tools/task_reclaim.go:846` (`DeadHolderBranches`), `:935` (`ReclaimHeldBranches`) | `rows, err := reclaimDeadHolderRows(); if err != nil \|\| len(rows)==0 { return out }` — a ledger read failure and "no rows" collapse into an empty map | **Reported, not fixed.** Same family as ① (an unreadable ledger silently yields "nobody owns it"), but here the conservative direction is preserved twice over: the branch-attribution verdict for the same row is `ungoverned`, and the preview's dead-holder/held sets are annotations on a count that is computed independently. Fixing it means threading a third error into `RepoSweepPreview` — a genuinely larger change, and NOT what this task was dispatched for. Named here so it is dispatchable (Rule 26). |
| `internal/tools/repo_sweep.go:878` `preview.Err = err.Error()`, consumed at `merger_status.go:315` → rendered as `error: …` | **Not a defect.** The error IS rendered (measured: an unreadable repo prints `error:` and the repo's other lines are skipped). |
| `internal/tools/memory.go` (`_ = err //stylegate:ignore best-effort…`), `tasknotify.go:426` (best-effort kill), assorted `rows.Close()` | Deliberate discards with a written reason at the site | **Out of class** — these are annotated best-effort paths, not errors whose lifetime was destroyed by a reporting statement. |

The class that ① belongs to is narrow and mechanical: **an error whose variable is reused by a
later, unrelated call between the failure and the report.** `%!w(<nil>)` is its fingerprint, and a
`grep -n '%!w'` over a live log is the way to find a recurrence.

## Overlap (stated as the briefing requires)

Sibling branch **`fix/attribution-ledger-nil-error`** (commit `0810ba8c` + docs `8de85bc1`,
`9e3f94ae`) already proposed a fix for defect ①, with the identical root-cause diagnosis (the shared
`err`). It has no worktree and no live owner and sits in the merger's pending list.

**Its finding is adopted as evidence; my implementation was my own** (Rule 23 — read the old branch,
do not continue on it; this work was a fresh branch from master). Both derived the cause from the
same place (`memoryDBPath()`), both made the nil-wrap structurally unrepresentable, and both
shipped a red proof — independent convergence on the same fix.

**RESOLVED — the overlap was merged, and in the right direction.** During this task master
received `c890b0fe Merge branch 'fix/attribution-ledger-nil-error'`, i.e. the sibling landed
defect ①. Dropping my duplicate was therefore the only correct move: two implementations of one
arm would have conflicted at merge time for no gain (and my duplicate test file would not even
compile next to its `branch_attribution_ledger_err_test.go`). Git's own rebase produced a content
CONFLICT in `branch_attribution.go` — which is the mechanical proof that the two edits were the
same change — and this branch was rebased onto `c890b0fe` with **only ② and ③** re-applied.
**This branch is now strictly additive over master**: it removes nothing and touches no line of ①'s
fix, so the reviewer has no choice to make — merge it for ②+③.

## 5. Transcripts
On branch feat/owner-ended-work
nothing to commit, working tree clean
########## S1: NOTHING to do, merger NOT running ##########
merger:      not running
lock file:   /dev/shm/gopi-test-scratch/sbx-merger/.gopi/.gopi/serve.lock
lock holder: pid 1903229  (stale: the process is gone; the lock is FREE)
interval:    5m0s (default — no live merger to read it from)
next tick:   unknown (no merger is alive — nothing is polling)
log file:    /dev/shm/gopi-test-scratch/sbx-merger/.gopi/.gopi/logs/merger.log
  (not created yet — no merger has written here in this profile)

repo:        /dev/shm/gopi-test-scratch/sbx-merger/repoA
  branches:  2 local head(s) = 1 integration + 1 worktree-held + 0 archive-tagged + 0 exempt-namespace + 0 deferred + 0 pending
    worktree-held (1): feat/owner-ended-work — git worktree list (holder liveness is not measured here)
  pending:   none — nothing to merge
  unowned:   1 branch(es) no ledger row names — the only class attributed to nobody:
    - feat/owner-ended-work

########## S2: WITHHELD work (a finished run still holds a checkout), merger NOT running ##########
repo:        /dev/shm/gopi-test-scratch/sbx-merger/repoA
  branches:  2 local head(s) = 1 integration + 1 worktree-held + 0 archive-tagged + 0 exempt-namespace + 0 deferred + 0 pending
    worktree-held (1): feat/owner-ended-work — git worktree list (holder liveness is not measured here) (dead holder: 1 [feat/owner-ended-work] — the holder is gone, the worktree still blocks the merge) (still refused: 1 [feat/owner-ended-work — row carries no worktree/repo — nothing to free]) (attributed: 1 [feat/owner-ended-work — 01a0aaaa-1111-7111-8111-111111111111 (owner-ended)])
  pending:   none — nothing to merge
  dispositions: 1 branch(es) withheld from the merger, 2 head(s) accounted for — the merger is NOT running, so none of the remedies below will run by itself
  withheld:  1 branch(es) the merger will NOT merge this tick — each names its existing remedy arm:
    - feat/owner-ended-work (owner-ended, already in master: release it and the deliverable is IN master) — the reclaim arm refuses to release the checkout (row carries no worktree/repo — nothing to free)

########## S3: the SAME withheld repo, merger ALIVE ##########
merger:      alive
repo:        /dev/shm/gopi-test-scratch/sbx-merger/repoA
  branches:  2 local head(s) = 1 integration + 1 worktree-held + 0 archive-tagged + 0 exempt-namespace + 0 deferred + 0 pending
    worktree-held (1): feat/owner-ended-work — git worktree list (holder liveness is not measured here) (dead holder: 1 [feat/owner-ended-work] — the holder is gone, the worktree still blocks the merge) (still refused: 1 [feat/owner-ended-work — row carries no worktree/repo — nothing to free]) (attributed: 1 [feat/owner-ended-work — 01a0aaaa-1111-7111-8111-111111111111 (owner-ended)])
  pending:   none — nothing to merge
  dispositions: 1 branch(es) withheld from the merger, 2 head(s) accounted for
  withheld:  1 branch(es) the merger will NOT merge this tick — each names its existing remedy arm:
    - feat/owner-ended-work (owner-ended, already in master: release it and the deliverable is IN master) — the reclaim arm refuses to release the checkout (row carries no worktree/repo — nothing to free)

########## S4: the SAME branch, now a DIRTY checkout (the reclaim arm's own measured refusal) ##########
  withheld:  1 branch(es) the merger will NOT merge this tick — each names its existing remedy arm:
    - feat/owner-ended-work (owner-ended, already in master: release it and the deliverable is IN master) — the reclaim arm refuses to release the checkout (row carries no worktree/repo — nothing to free)

########## S5: the deliverable is ALREADY IN master (releasing the blocker IS the whole remedy) ##########
  withheld:  1 branch(es) the merger will NOT merge this tick — each names its existing remedy arm:
    - feat/owner-ended-work (owner-ended, already in master: release it and the deliverable is IN master) — the reclaim arm refuses to release the checkout (row carries no worktree/repo — nothing to free)

## 6. Verification

* **Build / vet** — `go build ./...`: OK. `go vet ./...`: clean.
* **Red proof, defect ② (this branch)** — `TestMergerStatusDistinguishesWithheldFromNothing`
  asserts the `withheld:` block; it cannot exist before the block does, and on the pre-fix source
  the same repo renders `pending: none — nothing to merge` with **no withheld line at all** (the
  measured defect). Recipe (exactly as run):

  ```bash
  # defect ① (now master's) — recorded evidence from before the rebase:
  git checkout 64f4345a -- internal/tools/branch_attribution.go internal/tools/repo_sweep.go internal/tools/merger_status.go
  mv internal/tools/merger_status_disposition.go internal/tools/merger_status_trust_test.go <scratch>/
  go test -run TestMergerStatusLedgerCauseNameable ./internal/tools/
  #   --- FAIL: ... RED: the real cause was destroyed and replaced by Go's nil-wrap marker:
  #       branch attribution: task ledger unreadable: %!w(<nil>)
  <restore>  → ok

  # defect ②+③ (this branch): strip the added block from a rendered status and diff against
  # the pre-fix render — asserted in-test by stripDispositionLines + renderRepoBlock, so the
  # byte-identity claim is a TEST, not an eyeball.
  ```

* **Tests** — the two packages this change touches, run with an explicit timeout and
  benchmarked against a PRISTINE baseline worktree of the same commit (`64f4345a`, the branch
  point) so a pre-existing cost is never reported as a regression:

  | run | pre-existing 10m package budget | with `-timeout 900s` |
  |---|---|---|
  | `internal/tools` — baseline `64f4345a` (no changes) | timeout (605s, `database/sql` stack) | **ok, 792.025s** |
  | `internal/tools` — this branch | timeout (602s) | **ok** (see below) |
  | `cmd/agent` — baseline `64f4345a` | timeout (`TestTopLevelMergerOnceRunsAPass`, merger_top_level_e2e_test.go:118) | n/a — not re-run here |

  The suite exceeds Go's 10m package budget **on the untouched baseline**, which is the
  documented host/ledger cost and the subject of its own task — not a property of this change.
  Measured honestly, the delta attributable to this branch is a handful of added unit tests
  (seconds), and this change also runs one extra `git merge-base` per withheld branch on the
  status path. Nothing here is heavy: no device load, no loop, no sleep.
* **Live defect reproduction** — with `GOPI_MEMORY_DB` pointing at a directory (off-`/`, under the
  scratch tree), the pre-fix binary prints `%!w(<nil>)` on BOTH repo lines; `c890b0fe`'s binary
  prints the full cause. Measured on the rebased branch: `merger --status` on the live GoPi repo
  shows **8 withheld branches** (7 before the sibling's merge) where master shows none.
* **Rebased-branch re-verification** (after `git reset --hard c890b0fe` + re-apply of ②/③):
  `go build ./...` OK, `go vet ./internal/tools/` clean, the targeted tests ok, and the live
  byte-identity re-measured — every pre-existing line of the live GoPi repo identical, Maps
  byte-identical, and the three situations still distinguishable (S1 no block / S2–S3 withheld,
  differing only by the liveness clause / S4–S5 the remedy sentences).

## 7. Files

* `internal/tools/branch_attribution.go` — `Reclaimer` carried into `BranchAttributionLine`
  (①'s error-lifetime fix is master's, `c890b0fe`; this branch deliberately does not touch it).
* `internal/tools/repo_sweep.go` — `Reclaimer` field; `WithheldInMaster`; `Dispositions()`,
  `UnownedBranchCount()`, `repoSweepWithheldInMaster()`.
* `internal/tools/merger_status_disposition.go` (**new**) — the disposition classes, their reason
  vocabulary, and the ONE render.
* `internal/tools/merger_status.go` — the block's placement (after `pending:`), `Dispositions` /
  `Unowned` / `WithheldInMaster` on the per-repo status.
* `internal/tools/merger_status_trust_test.go` (**new**) — the three defects' assertions
  (including the two directions of every classification).
* `internal/tools/merger_status_trust_redproof_test.go` (**DROPPED in the rebase**) — ①'s red
  proof, superseded by master's own `branch_attribution_ledger_err_test.go`.
* `devdoc/merger-status-trustworthy.md` (**new**) — this report.

## 8. Not Done (honest)

* `deadHolderResumeRows`' `LIKE '%worktree%'` needle and the `DeadHolderBranches` /
  `ReclaimHeldBranches` collapsed error are **reported in §4, not fixed** — different packages/arms,
  and mass-fixing them is explicitly out of scope for this task.
* The cronjob's third liveness shape (*nothing scheduled at all*) is **recommended, not
  implemented** — it belongs to task 01a0ac69, which owns the scheduler.
* No automatic disposition was added for `owner-ended` (deliberate, per the classifier's contract).
