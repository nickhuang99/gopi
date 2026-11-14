<!-- doc-class: historical -->
> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** The
> post-pass verification and its withdrawal (`internal/tools/merge_pass_verify.go`:
> `CaptureMergePassScope` / `VerifyMergePass` / `WithdrawMergePass`, plus the
> `merge-withdrawn` notify alarm) were DELETED on 2026-11-01 with the last slice
> of the retired top-level `gopi merge` verb — its last production caller,
> `cmd/agent/merge_run.go`, was already gone. **`gopi task merge` does not verify
> the merged tree and does not withdraw a landing.** Everything below that speaks
> of the post-pass in the present tense is the record of a capability that
> existed, measured on the day each page states; it is not a promise about the
> shipped engine. `man/gopi-merge.md` carries the corrected contract.
# `gopi merge`: two rounds, one batch measurement

**Status**: normative — this is how `gopi merge` behaves on master.

## Why

`gopi merge` used to decide branches in ONE loop, each branch measured on its own,
in candidate order. Two costs followed from that shape:

1. **A resolve could destroy a fast-forward.** The conflict ladder (union commit,
   resolver child, branch rework) rewrites code and moves the integration branch.
   Run before a branch that would have fast-forwarded cleanly, it changes the very
   tips that fast-forward depended on: a landing that was free becomes a conflict,
   or stops being possible at all.
2. **One measurement per branch.** The toolchain cost is per *build*, and the
   builds are over the same tree at the same moment: measuring the tree after
   landing A and again after landing B buys a second build and answers nothing the
   first did not.

So the pass is now **two rounds per repo**, in this order.

## Round 1 — fast lane (`mergeDecideFastLane`)

Pure git decisions over the whole candidate list:

| verdict | what happens |
|---|---|
| already in the integration branch | ref pruned (`mergeLandIfSpent`) |
| pure fast-forward | `git merge --ff-only` → landing recorded, **ref prune deferred** (`mergeLandIfFastForward`) |
| anything else | **declined, untouched**, reported as `fast-lane-left` (or `diverged` under `--fast`, see below) |

The lane never writes code. That is not a limitation, it is the reason it may run
FIRST: it shrinks the candidate set without destroying what the slow lane needs.

After the lane, the pass measures its batch **once** through the machinery that
already existed — `tools.CaptureMergePassScope` (pre-pass tip snapshot, taken
before the sweep) and `tools.VerifyMergePass` — never per branch.

* **green / abstained / `--no-verify`** → the prune round 1 owes runs now
  (`mergePruneRoundBatches` → `mergePruneLandedRound` → `mergePruneSpentRef`).
* **red** → `tools.WithdrawMergePass` resets to the captured pre-pass tip and
  **re-creates the refs the landings deleted** (its `BranchTips` snapshot). No
  prune runs. The per-branch report line carries
  `— UNSURVIVED: the batch was withdrawn, master is back at the pre-pass tip, the
  ref survives`.

### Why the prune is deferred, not done at landing time

A red batch is *undone*: `git reset --hard <pre-pass tip>` plus ref re-creation. A
prune performed at landing time would therefore be undone too — the refs come
back — while a prune performed after a green verdict is final. Between the landing
and the verdict the ref also has to exist: it is what a human needs to rework the
withdrawn batch.

## Round 2 — slow lane (`mergeDecideBranch`)

Exactly the ladder this command ran before the split, over exactly the branches
round 1 left: measure → mechanical union → hand over / file. Behaviour unchanged,
including pruning at landing (a slow-lane landing is never part of a withdrawable
batch) and the hand-over cap + branch-rework liveness gate, whose ledger read now
happens just before round 2 because only round 2 hands anything over.

## `--fast`

Unchanged in MEANING: round 1 only, no child, nothing filed, and its non-landable
branches keep the word they always had (`diverged`), because there is no later
lane in that run. It does still measure its batch — that is what makes a
fast-forward of a non-compiling branch withdrawable — and the prune follows the
verdict exactly as above.

## Report shape

```
gopi merge: round 1 (fast lane) — prune spent refs, fast-forward what git alone can land, never write code
gopi merge: round 2 (slow lane) — the ordinary conflict ladder over the branches round 1 left (measure, union, hand over, or file)
gopi merge: <repo> post-pass verification verified — … PASSED (go build ./... + go vet ./... green)
gopi merge: <repo> — round 1's batch stands: 1 spent ref(s) pruned
  <repo> feat/ff        landed          fast-forwarded into master, ref pruned
  <repo> feat/conflict  fast-lane-left  not a pure fast-forward — left untouched for the slow lane in this same run
  <repo> feat/conflict  cap-exceeded    conflict over the run ceiling — filed as a branch-rework task …
```

The branch report is printed **after** the prune, so each line states what the
repository holds now.

## Measured (2026-10-18, `/home/user/.gopi/sessions/two-rounds-evidence/`)

Real `./bin/gopi merge`, fixture repos under `GOPI_SETTINGS`, master pre-tip noted:

* **ff + conflict** (`fixture/repo2`, PRE `2c21188`): round 1 landed `feat/ff`
  (master → `527282b`), one batch verification `PASSED`, `1 spent ref(s) pruned`,
  then round 2 filed `feat/conflict`. 3 decisions, one build.
* **red batch** (`fixture/repo-red`, PRE `58f8e34`): `feat/broken` fast-forwarded
  (master → `085151c`), `go build ./...` FAILED → `WITHDRAWN 085151c → 58f8e34`,
  master back at PRE, `feat/broken` ref back at its own tip, exit 1, no child.
* **`--fast`** (`fixture/repo-fast`, PRE `008d223`): `feat/ff` landed + pruned,
  `feat/conflict` reported `diverged`, round 2 never announced, one build.

Logs: `run-full.log`, `run-red.log`, `run-fast.log` beside each repo.

## Gates

`go test ./cmd/agent/ -run 'TestMergeFastLaneLandsBeforeAnySlowLaneWork|TestMergeFastArmRunsRoundOneOnly|TestMergeRedBatchWithdrawsRoundOneAndRestoresTheTip'`

* `…LandsBeforeAnySlowLaneWork` reads the repository **inside** the batch
  verification: master already contains the fast-lane tip, the fast-lane ref still
  exists (prune deferred), and after the green verdict it is gone. Exactly ONE
  verification call. Slow-lane branch ref/tip untouched.
* `…FastArmRunsRoundOneOnly` pins `--fast`: no round 2 line, no hand-off word, the
  `diverged` word kept, landing stands and its ref reclaimed, one measurement.
* `…RedBatchWithdrawsRoundOneAndRestoresTheTip` runs the REAL verification against
  a fixture module that does not compile (heavy: device exec, stubbed verdicts
  cannot show a withdrawal): exit status `mergeVerifyWithdrawnExitStatus`, master
  back at the pre-pass tip, ref re-created at its landed tip, the withdrawal loud,
  no hand-over.

## Not done / known edges

* The ref `feat/broken` in the red run is re-created by the withdrawal, so
  `mergePruneRoundBatches` deliberately returns without pruning when
  `verification.withdrawn` — a later run decides it again (by design: its work is
  red and a human owes it a rework).
* Round 1's batch verification covers the SLOW lane's landings too (the sweep is
  one pass over the repo), so a red batch withdraws them as well. Withdrawing the
  whole pass is the conservative reading and the existing machinery's semantic;
  narrowing it to round 1 only would need a second snapshot mechanism (rejected —
  Rule 35).
