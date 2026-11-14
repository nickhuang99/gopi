<!-- doc-class: historical -->
# Task Report: the probe refusal's verdict — `settled` stops disagreeing with `would-run`

**Epic task**: `01a134f8-05d5-76a4-9b70-e7bad4e1592c` (branch
`task/01a134f805d576a49b70e7bad4e1592c`, worktree
`/home/user/workspace/gopi-01a134f805d576a49b70e7bad4e1592c`).
**Baseline for this diff**: `6ef428c`.

## Completed

- **The defect, measured before the change** (real binary, fixture ledger,
  three host shapes — see `## Verification`): a TERMINAL row whose recorded
  worktree directory is **gone** reaches the chain's terminal-state probe
  (`git rev-parse --verify HEAD` inside that directory → `chdir …: no such file
  or directory`). That refusal composes **no delivery state**, so the plan face
  read the row as neither settled nor owed:

  | host | probe refusal reading (BEFORE) |
  |---|---|
  | client + peer | `candidates=1 would-run=1 settled=0 refused=0` ✓ |
  | **server node + peer** | `candidates=1 would-run=0 settled=0 refused=1` ✗ |
  | no peer declared | `candidates=1 would-run=0 settled=1 refused=0` ✓ |

  The server row is the shape that lived: the two plan fields **disagree about
  one row** (`Settled=False WouldRun=False`), and the row is counted as
  `refused` by every sweep for ever — while its branch sat local and in **no**
  peer ref (measured: the peer ref was empty before AND after `--apply`).

- **The fix** — `internal/tools/task_finish_cleanup.go`:
  1. `finishCleanupClientArmApplies()` — ONE named predicate for *"which arm does
     this host's chain take for a row"* (`!IsServerNode() &&
     finishCleanupServerTarget() != ""`), read by **both** the dispatcher
     `taskFinishCleanup` and the settled classifier (Rules 13/14: the value is
     defined once and referenced, never mirrored).
  2. `FinishCleanupReportSettled`'s probe arm gained the second half of *"this row
     has no delivery channel"*, asked **by role**: a **server node** preserves the
     branch as the merger's input and pushes nothing, so its probe refusal leaves
     the row with exactly as little to hand over as the no-peer host's — settled.
     The no-peer clause above it is unchanged, and the client host that declares a
     peer **keeps the unset verdict** (its branch is still owed) — the control.

- **Tests**:
  - `internal/tools/task_finish_cleanup_probe_delivery_plan_test.go` (new) — the
    **missing coverage that let this live**: the shape was pinned at the CHAIN
    level (`single_node_settled_test.go`) but never at the **PLAN** level, which
    is the face `--dry-run` prints. Three arms: client+peer (`would-run`), no
    peer (`settled`), **server+peer** (the measured defect). Fixture = a real
    repo + real branch via the scaffold (Rule 32), the directory deleted for real.
  - `internal/tools/task_finish_cleanup_single_node_settled_test.go` — ARM B
    rewritten to pin the **role**-aware reading (its old assertion is the pre-fix
    behaviour), ARM C added as the client control.

## Not Done (honest)

- **The first shape of the fix was wrong, and the red proof is what caught it.**
  It delegated the refusal into the CLIENT delivery arm from inside
  `finishCleanupServerRecycle`, on the belief that a peer-declaring client host
  could reach that body. It cannot: the dispatcher enters the client chain
  whenever `finishCleanupClientArmApplies()` is true, so every call arriving at
  that body already has no client arm — the delegation was **unreachable**, and
  the arm stayed **red with the delegation reverted**. The delegation was
  removed; the code comment at the refusal records this so the next reader does
  not add it back.
- **The row's `--apply` on the server arm still removes nothing** (`removed=0`),
  correctly: the branch is preserved for the merger and the directory is gone.
  What the fix changes is the *verdict*, not the action — a row whose plan said
  `refused` was invisible to every sweep; it now reads `settled`.
- No change was made to the peer-hand-off machinery or to any push path. A row
  on a client host that declares a peer is untouched (`would-run=1`), and
  `--apply` on that arm still delivers (peer ref appears).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → BUILD_OK
- Tests: `go test ./internal/tools/ -run 'FinishCleanup' -count=1` → `ok
  github.com/nick/gopi/internal/tools 33.801s` (the whole finish-cleanup family)
  and `-run 'TestFinishCleanupPlanStrandedProbeBucketOwesTheDelivery|TestFinishCleanupGoneWorktreeProbeRefusalSingleNodeIsSettled'` → both PASS.
  `make test-budget PKGS='-run TestFinishCleanup ./internal/tools/'` — the
  declared entry point, branch-scoped to the family this diff touches → `exit=0
  outcome=pass`, `elapsed=94s`, both new arms listed as PASS
  (`TestFinishCleanupPlanStrandedProbeBucketOwesTheDelivery` 0.97s,
  `TestFinishCleanupGoneWorktreeProbeRefusalSingleNodeIsSettled` 0.81s).
  A whole-package run of `./internal/tools ./cmd/agent` was started and
  **killed at 467s** still running (it was one of two `go test` sets in this
  run, and Rule 5's merge-path scope is SMOKE, BRANCH-SCOPED — the scoped run
  above is that scope, and it is the one whose result is claimed here).
- Ran: the surface is the **verb**, over its own fixture:

  ```
  BIN=/dev/shm/redproof/gopi-final bash /dev/shm/redproof/cmp.sh final
    git -C <repo> worktree remove --force <recorded ground>      # the directory really goes
    $BIN task finish-cleanup --dry-run --task <row>              # the plan face
    $BIN task finish-cleanup --apply   --task <row>              # the chain
  ```
  All three host shapes are exercised in ONE fixture (repo + bare peer + the
  `ssh` shim that maps `fixturepeer:~/work/GoPi` → the fixture's bare repo, so
  the production addressing shape is what runs):
  `sync_peers:["fixturepeer"]` + `role:client` / `role:server` / no peers.

- Red then green (the SAME binary pair, the same fixture generator):

  ```
  BIN=…/gopi-baseline (HEAD, pre-fix) bash cmp.sh baseline
    [baseline] client  would-run=1 settled=0 refused=0   PEER-after=6771d6b0… (delivered)
    [baseline] server  would-run=0 settled=0 refused=1   PEER-after=''          ✗ THE DEFECT
    [baseline] nopeer  would-run=0 settled=1 refused=0

  BIN=…/gopi-final (this diff) bash cmp.sh final
    [final] client  would-run=1 settled=0 refused=0   PEER-after=6fb361da… (still delivers) ✓ control
    [final] server  would-run=0 settled=1 refused=0   PEER-after=''                       ✓ fixed
    [final] nopeer  would-run=0 settled=1 refused=0                                       ✓ unchanged
  ```

  Rule 22: the fixture root is `/dev/shm/redproof/…` — `stat -c %d` reads **27**
  against `/`'s **66307**, i.e. provably off the `/` partition. Each binary gets
  its OWN fixture root, so block-device work is small, one-shot and sequential
  (Rule 27).

- Red proof + restore (Rule 5's criterion — measured by **content hash**, not by
  an index-relative diff):
  - removed the role arm from `FinishCleanupReportSettled` → **both** tests FAIL
    with the defect's own shape printed — the plan row's struct dump carries
    `WouldRun:false Settled:false` and its verdict is
    `worktree unreadable: git rev-parse --verify HEAD: chdir … no such file or directory`.
  - restoring → `git hash-object internal/tools/task_finish_cleanup.go` =
    `5e00fc096ced001536a2cfcf7279ca39943fb354`, the identical hash taken **before**
    the mutation (and `git rev-parse HEAD:<path>` still names the unmodified
    baseline `a57fd5f1…`, because the deliverable was uncommitted in this worktree
    at that moment).
- Result: as measured above; the plan/chain disagreement is gone on the server
  role, and both controls read unchanged.
- Commit: `231ad0de8` (the report, both tests and the fix).
- Raw evidence kept beside this run's session directory:
  `~/.gopi/sessions/01a134f8-05d5-76a4-9b70-e7bad4e1592c/evidence/` —
  `redgreen-binary-run.log` (both binaries, all six arms, verbatim),
  `cmp.sh` (the generator) and `gopi-e2e-run.sh` (the earlier one-row- per-arm
  variant that first reproduced the shape).

## Evidence (self-filed)

- `device / :66307   device root:27` — the fixture root is off the `/` partition
  (Rule 22), printed by `cmp.sh` itself.
- `[baseline] server  … would-run=0 settled=0 refused=1` and
  `[final] server  … would-run=0 settled=1 refused=0` — the defect and its fix,
  from two binaries over the same generator (both lines are verbatim output).
- `[baseline] client PEER-after='6771d6b0…'` / `[final] client
  PEER-after='6fb361da…'` — the client arm still delivers in both, so the fix did
  not touch the hand-off.
- `BEFORE=5e00fc096ced001536a2cfcf7279ca39943fb354` / `AFTER=5e00fc096ced001536a2cfcf7279ca39943fb354`
  — the restore, measured by content hash.
- `stamp: … host=hostb-Laptop pkg=-run TestFinishCleanup ./internal/tools/
  budget=25m elapsed=94s … cores=8 exit=0 outcome=pass` — the scoped run's own
  stamp line, from `make test-budget` (the declared entry point).
- `--- FAIL: TestFinishCleanupPlanStrandedProbeBucketOwesTheDelivery` with
  `Settled:false` in the printed plan row — the red proof, from the suite.
- The whole-package `make test-budget PKGS='./internal/tools ./cmd/agent'`
  invocation was **killed at 467s** without a verdict and is NOT claimed as a
  result anywhere in this report.
