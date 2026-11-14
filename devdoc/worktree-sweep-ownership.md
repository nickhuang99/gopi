<!-- doc-class: historical -->
# An ended run's worktree becomes reclaimable — ownership needs evidence, not a record

Follow-up to the worktree-residue closeout (row `01a0a36a`), which fixed two live
leak causes and deliberately left three. This is the repair for those three,
plus one it found on the way.

Branch: `fix/worktree-sweep-ownership` · Worktree: `/home/user/workspace/gopi-wtsweep-own`
Base: `1c320ddc` · **Fix commit: `b65e7e0b`** (`fix(worktree-sweep): a finished run's
worktree is reclaimable — ownership needs evidence, not a record`) — the code, the
tests and this report all ship in that one commit. Recover it with
`git show b65e7e0b --stat`; the current branch tip is one doc-only commit later
(this line was added so the report names an immutable hash instead of pointing at
itself).

## The measured starting point

**`fix/worktree-residue-closeout` is NOT merged** — `git merge-base --is-ancestor
fix/worktree-residue-closeout master` → NOT-MERGED; `git log master..` shows only
`44605f00`. The stamping-seam binding fix is therefore *not* in effect on master,
so whatever the sweep does today it does with master's code, and the sweep-seam
distrust below is the only protection that exists until that branch lands.

**The epic's acceptance fixture is already gone.** `/home/user/workspace/gopi-bgatefix-blocked`
does not exist, its branch `fix/mergebuildgate-module-download-abstain` no longer
exists, and the row `01a0a36a` that named it now reads `verified`/terminal
(owner ended `1789452840`). So acceptance (a) — "the dry-run no longer reports
the false-ownership keep" — holds **trivially**: there is no such registration
left to report. That is a fixture artefact, not a fix, and it is recorded here
rather than claimed. The defects were therefore reproduced as (i) controlled
probes on a real repo + a real ledger and (ii) red proofs, and a *live*
false-ownership case was found and fixed on the real host (see "Real host"
below).

**Ledger measurements** (`~/.gopi/memory.db`, the tasks table — note the ledger
is `memory.db`, not `tasks.db`, which is an empty file on this host):

| measurement | value |
|---|---|
| rows total | 1313 |
| rows carrying a deliverable (the evidence set) | 675 |
| **NON-TERMINAL** rows carrying a deliverable | **4** |
| NON-TERMINAL rows carrying a branch stamp | 1 |
| rows carrying a branch and **no** worktree stamp (any status) | **81** |
| terminal rows with a populated `ended_at` | 665 (of 671 with a deliverable) |

Two of these are load-bearing: guard ③'s evidence base is **4 rows**, so a
branch-name collision is a *thin* but real veto on this host; and 81 rows are
invisible to a `%worktree%` needle (defect 4 below).

## The four defects

1. **Guard ③'s branch arm trusted a name, not ownership.** `row.Branch == reg.Branch`
   kept a registration whenever ANY non-terminal row named that branch — including
   a row recording a *different* worktree. Branch names are minted per task
   (`fix/<name>`), so two tasks minting one name vetoed reclamation for as long as
   either stayed open.
2. **A persisted false `deliverable.worktree` stamp was unquestionable.** The path
   arm protected the registration on the strength of the stamp alone — exactly how
   a stamp gets written wrongly (a run that merely *inspected* another task's
   worktree re-stamped its own row; that is the loose `-C` binding `01a0a36a`
   fixed). A wrong stamp outlived the run that wrote it.
3. **The 24 h `MinAge` floor was measured on the registration's creation record.**
   A same-day orphan reported `age=0h` and was unreachable for the rest of that
   day no matter how conclusively its owner was gone.
4. **The loader silently dropped an ownership axis.** `worktreeSweepTaskRows`
   scanned `deliverable LIKE '%worktree%'`, so a row whose deliverable names
   **only a branch** never reached rung ③ at all — the branch arm could not
   protect evidence the loader never delivered. Measured: 81 such rows exist on
   this ledger, and on a probe ledger an open branch-only row was simply absent
   while the branch arm's own tests (which construct rows directly) kept passing.
   Found while testing defect 1; named as a defect rather than folded in silently.

### The root shape

A ledger row is an **ownership record**, and a record outlives the process that
wrote it. The guard ladder was reading a *record* as if it were *ground*: a row's
mere presence, a row's mere branch string, and a registration's mere age each
stood in for the question the ladder is actually asking — **is anyone still
there?**

## The fix (`internal/tools/worktree_sweep.go`)

### Guard ③ (+ ①D) — ownership requires evidence, not presence

- New predicate `worktreeSweepRowHoldsGround(row, opts)`, called by BOTH
  consuming rungs (③ and the dead-registration rung ①D), so a row cannot protect
  ground through one rung's evidence and not the other's. True when the row is a
  live holder status AND its runner is not provably gone AND its life has not
  been declared over.
- New predicate `worktreeSweepRowNamesOtherDir(row, regPath)`: the branch arm now
  fires only when the row names **no other directory**. A row recording a
  DIFFERENT worktree is working elsewhere — a name collision, not ownership.
  `""` (the submit-time `{"repo":...}` placeholder, or a tilde spelling that
  never compared equal) is the ABSENCE of evidence, not evidence of "elsewhere",
  and keeps protecting.
- **The branch arm stays, on measured evidence.** It is kept deliberately for the
  case the collision predicate carves out: a live row naming this branch records
  **no** directory — the owner whose deliverable was never backfilled. Reclaiming
  on that row would be freeing ground whose proof is merely *missing*, and the
  arm is the only thing standing between the sweep and exactly that. It costs
  protection only when the row demonstrably works elsewhere, and it can no longer
  be bypassed by a foreign row because the same question is asked of the same
  evidence at both rungs. (Evidence base: 1 non-terminal row on this host carries
  a branch stamp — the arm is nearly dead *on this ledger*, but the untied case
  is unreachable when it is dead and dangerous when it is not.)
- **Safety direction preserved**: the new field is `HolderProvenDead` — the
  *proof*, not its negation — on purpose, so the struct's zero value is the
  FAIL-CLOSED direction: any caller or future loader that does not carry a holder
  verdict protects the registration instead of freeing it. ("Unknowable" is never
  "dead".) This was not theoretical: the first cut spelled it `HolderLive` and two
  existing tests correctly caught that a zero-valued hand-built row then read as
  dead.

### Defect 2 — the false stamp is distrusted at the sweep seam

The stamp can no longer carry a registration on its own: the path arm runs behind
the same `worktreeSweepRowHoldsGround`, which the real loader feeds with
`finishCleanupDeadLocalHolder(...)` — the *shared* classifier the finish-cleanup
arms already use (Rule 14: one definition of "this row's runner is gone"), fed the
row's own recorded `proc_pid`/`proc_start_ticks`/`proc_cmd`. Distrust is
re-measured from `/proc` on every sweep, never a ledger hand-edit.

### Guard ⑤ — the age floor does not shield a finished run

`worktreeSweepRowLifeEnded(row, opts)` is the ledger's own declaration that a
row's life is over, in either of two shapes: a **terminal status**, or a live
status beside an `ended_at` older than `worktreeSweepEndedGrace`. A row can only
speak for a registration it is **tied to**: its recorded worktree must BE this
registration, or — naming no other directory — its branch must be this one. That
tie is the *same* predicate rung ③ uses, so the two seams cannot disagree about
who owns what; and because terminal rows now count as ended, the tie is
load-bearing rather than cosmetic — without it ANY finished task elsewhere could
license the bypass by branch name alone.

`worktreeSweepEndedGrace = time.Hour` is a safety floor, not a convenience:
**measured** on this ledger, the gap between a row's `ended_at` and its child's
`created_at` is under two minutes (10 samples), so a run that submits a child as
it closes out — where the child could inherit the same branch and collide with
its parent's registration — stays protected inside the window.

### Defect 4 — the evidence set carries every ownership axis

The scan is now `COALESCE(deliverable,'') <> ''` and lets the parsers decide, so
no spelling can truncate an axis out of the evidence (Rule 15: the miss was
silent). The same query now also loads `ended_at`, `claimed_by` and the recorded
process identity, which is what feeds the holder verdict.

## Verification

### Red proofs (fails before, passes after — shown by running against pre-change code)

Pre-change tree: a detached worktree at master (`/home/user/workspace/gopi-ab-base`)
carrying `internal/tools/worktree_sweep_redproof_base_test.go` — the SAME
scenarios, asserted at the post-fix expectation, expressed with only the evidence
the pre-change struct can carry.

| Red proof (master @ `1c320ddc`) | Result on master | On the fix |
|---|---|---|
| `TestRedProofBranchNameCollisionProtectsAForeignTree` | **FAIL** — `keep (branch fix/shared-name belongs to live task T-collide (in_progress))` | PASS |
| `TestRedProofPersistedStampWithNoRunnerProtects` | **FAIL** — `keep (owned by live task T-false-stamp (in_progress))` | PASS |
| `TestRedProofAgeFloorShieldsAnEndedOwner` | **FAIL** — `keep (owned by live task T-ended (in_progress))` | PASS |
| `TestRedProofAgeFloorShieldsATerminalOwner` | **FAIL** — `keep (younger than the threshold (10m0s < 24h0m0s))` | PASS |
| `TestRedProofLoaderDropsABranchOnlyRow` | **FAIL** — `RED: the loader dropped a branch-only row (0 rows)` | PASS |
| `TestRedProofLiveRowStillProtects`, `…AgeFloorKeepsAnOwnerlessSameDayRegistration` | PASS (no regression, both sides) | PASS |

Post-fix twins in `internal/tools/worktree_sweep_ownership_test.go`:
`TestSweepBranchNameCollisionDoesNotProtectAForeignTree`,
`TestSweepDistrustsAStampWhoseRunnerIsGone`,
`TestSweepAgeFloorDoesNotShieldAnEndedOwner`,
`TestSweepAgeFloorDoesNotShieldATerminalOwner`,
`TestSweepLoaderCarriesABranchOnlyRow`, plus the safety twins
`TestSweepKeepsRegistrationOfAGenuinelyLiveRun`,
`TestSweepUnstampedRowOfThisBranchStillProtects`,
`TestSweepEndedRowElsewhereDoesNotLicenseTheBypass`,
`TestSweepYoungRegistrationWithoutOwnerEvidenceIsStillKept`.

### Runtime acceptance on real git + a real ledger (A/B, identical evidence)

Probe built by `/dev/shm/gopi-test-scratch/wtsweep-probe/mkprobe.sh` (a real repo
under the scratch root + a **copy** of the real ledger; never production state,
never a ledger hand-edit):

```
probe-collide-0001  in_progress  ended_at=-6h  branch fix/collided-name
                    worktree=<elsewhere-not-this-tree>   ← false stamp + collision
probe-live-0002     in_progress  no ended_at   branch fix/live-name  worktree=<wt-live>
probe-live-0003     in_progress  no ended_at   branch fix/collided-name, NO worktree
probe-dirty-0004    in_progress  ended_at=-6h  worktree=<wt-dirty>   ← orphan-B shape
probe-ended-0005    completed    ended_at=-10m worktree=<wt-orphan>  ← guard ⑤, tied by path
```

**Defect 1+2 (collision + false stamp)** — `gopi task sweep-worktrees --dry-run`:

- **master** → `keep … wt-orphan (branch fix/collided-name belongs to live task probe-collide-0001 (in_progress))`
- **fix** → `reclaim … wt-orphan (… no live holder, clean, registration age …)`; `wt-live` kept.

**Defect 3 (age floor)** — isolated to a single terminal, path-tied owner:

- **master** → `keep … wt-orphan (younger than the threshold (1m0s < 24h0m0s))`
- **fix** → `reclaim … wt-orphan (task probe-ended-0005 is completed, no live holder, clean, age 1m0s)`

**The safety half (orphan-B shape — staged work existing in no branch)**: a real
worktree with `M f.txt; A g.txt` and an *ended* owner, on BOTH binaries and with
`--apply`:

```
report  wt-dirty (dirty (uncommitted work) — left in place: M f.txt; A  g.txt)
```

The tree survives, its staged file survives, `--apply` reclaims only the clean
orphan, and `git for-each-ref refs/heads` is **byte-identical before/after**
(REFS_IDENTICAL) — branches are never force-deleted and no uncommitted work is
destroyed. `git worktree remove` is the only removal verb used; never `--force`.

### Real host (`/home/user/work/GoPi`, real ledger, both binaries, identical evidence)

The live counterpart of the probe, on the actual host and the actual ledger —
`gopi-wt-residue` is a **sibling task's** worktree (the residue-closeout run,
which finished), so this is exactly the shape the epic is about:

| binary | line |
|---|---|
| master `1c320ddc` (base) | `keep  gopi-wt-residue (younger than the threshold (1h39m0s < 24h0m0s))` |
| fix (this branch) | `reclaim gopi-wt-residue (task 01a09d0a-277e-7c8e-b412-859b754a09a8 is completed, no live holder, clean, age 1h39m0s)` |

The owning row `01a09d0a` is **terminal** and tied to the registration **by
path**; the branch it carries (`fix/worktree-residue-occupancy`) is not even the
branch checked out there (`fix/worktree-residue-closeout`), which is why the path
tie — not the branch arm — is what licenses the reclaim.

Re-measured independently of the sweep, on the same host and ledger (this is the
runtime acceptance the epic asks for, against real git + the real ledger):

```
row 01a09d0a  status=completed  ended_at=2026-09-14 13:18:39
              deliverable: branch fix/worktree-residue-occupancy
                           worktree /home/user/workspace/gopi-wt-residue
              → the ONLY row on this ledger stamping that directory
live /proc holders of gopi-wt-residue: NONE      (cwd/exe/fd scan over /proc/[0-9]*)
git -C /home/user/workspace/gopi-wt-residue status --porcelain: empty (clean)
git -C … log -1: 44605f00 (HEAD -> fix/worktree-residue-closeout)  ← branch preserved
```

So the four facts the guard ladder needs are each measured, not assumed: the
owner's life is over (its own row says so), nothing holds the directory, the tree
is clean, and the branch — the actual deliverable — is untouched by a
registration removal. `reclaim` is the only verdict those four facts support;
master's `keep` rests solely on the age floor, which the owner's `ended_at`
refutes. **No ledger hand-edit was made at any point** — the row is exactly as
the run left it.

Full dry-run with the fixed binary:

```
sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=1 …
  report  gopi-ab-base (dirty (uncommitted work) — left in place: ?? …redproof_base_test.go)
  keep    gopi-voice-out (held by a live process (cwd/fd/exe/maps))
  keep    gopi-wtsweep-own (held by a live process (cwd/fd/exe/maps))
  reclaim gopi-wt-residue (task 01a09d0a-… is completed, no live holder, clean, age 1h39m0s)
```

Every remaining `keep` is explained by real ground: two live `/proc` holders (the
session working in this very worktree is one of them), two main checkouts, one
registration inside the floor with no ended owner, and a dirty tree (reported,
never touched). So acceptance (a) holds in its **substantive** form — the sweep
no longer reports a false-ownership `keep` for a finished run's worktree — even
though the epic's named fixture had already been reaped (see above).

#### The one `keep` that is NOT a live holder, measured (acceptance (a), second form)

`/home/user/workspace/gopi-verify-01a0a387` is the only unowned registration this
host still keeps, and the epic's acceptance allows exactly this outcome *"if the
code explains in-code why the remaining `keep` is correct (verified against the
real ledger + real git)"*. It is verified, and the `keep` is correct:

```
git worktree list            → /home/user/workspace/gopi-verify-01a0a387  2cd4a8dc (detached HEAD)
git -C <it> status --porcelain → empty (clean)      git log -1 → 2cd4a8dc
dir mtime / .git created     → 2026-09-15 14:25:46 / 14:25:06   (39m before the dry-run)
ledger rows stamping that directory            → 0     (LIKE '%gopi-verify-01a0a387%' → 0 rows)
ledger rows mentioning its id at all           → 0
live holders (cwd/exe/fd/maps over /proc/*)    → 0
```

So the registration has **no owning row to consult** — there is no `ended_at`
anywhere that speaks for it, which is why guard ⑤ is the rung that stopped it and
why the reason reads "younger than the threshold". The nearest thing the ledger
offers is a *coincidence*: exactly one ended GoPi row's life window covers the
instant the registration was created (`01a0a387`, verified, `13:26:21 →
14:28:30`), and it stamps a **different** directory (`gopi-ticktock`). Treating
that as ownership is the very class of inference this epic deletes — the same
"an ended run is nearby, therefore this is free" that guard ⑤'s tie exists to
refuse (`worktreeSweepOwnerEnded` requires the path tie or an *untied* branch
name, and this row has neither).

Reclaiming it would require a rung the epic explicitly did not ask for: "any
ended run anywhere in time frees any young registration" — a licence to delete
ground on proximity. It becomes reclaimable the ordinary way (24 h, no
intervention) or immediately if an operator declares that root, and it needs
**no ledger hand-edit** either way. The floor is doing precisely its job:
protecting work whose owner is *unknown*, not proved gone.

### Gates

- Build: `CGO_ENABLED=0 go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `CGO_ENABLED=0 go test ./internal/... ./cmd/...` — the run's failures are
  **pre-existing and unrelated**, each A/B-confirmed on master at `1c320ddc` (and
  again on `2cd4a8dc`): `internal/config [build failed]` +
  `cmd/agent [build failed]` (`undefined: config.PeakHoursEnv` /
  `IgnorePeakHoursFlagSpelling` — an in-flight peak-hours refactor, identical on
  both trees; `git grep PeakHoursEnv 2cd4a8dc -- internal/config/` matches only
  the test file there, so master's own tip is red in this package too), `TestWorkerOwnershipPointerPresent` (fails on master too),
  `TestMergeBuildGate_SharedModCacheResolvesWithProxyOff` (fails on master too:
  no reachable module proxy). My change touches only
  `internal/tools/worktree_sweep.go`; `internal/tools` is otherwise green apart
  from that shared-modcache case.
- `gofmt` clean on every file touched.
- **One flaky pre-existing failure, measured:** `TestTaskFindSessionJSONLStillHonoursEveryLayout`
  (`internal/tools/taskledger_cost_test.go:246`) is red on master more often than
  on this branch — isolated repeats: base fails **7 of 8**, the fix 1 of 8 — and the
  diff touches neither that file nor its subject (`0dcb48a9` authored it). Flaky by
  order/timing; recorded, not absorbed.

## What was NOT weakened (safety record)

- `git worktree remove` only — never `--force`; a refusal is a reported `keep`, never a bypass.
- Branches are never force-deleted (`branch -d` only); the probes show refs byte-identical.
- A `keep` still wins for: the main checkout, a bare or locked registration, an
  unmeasurable age, a live process holding the path, dirty work, and any row that
  still holds ground.
- The ledger rung can only ever be *stricter* about evidence, never less careful
  about the directory: unreadable holder identity keeps protection (`unknowable ≠ dead`).

## Files

- `internal/tools/worktree_sweep.go` — the guard ladder, the loader, the new predicates + constants.
- `internal/tools/worktree_sweep_ownership_test.go` — post-fix tests incl. the loader proof.
- `devdoc/worktree-sweep-ownership.md` — this report.
