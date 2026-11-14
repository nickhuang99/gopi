<!-- doc-class: historical -->
# Task Report: verify the `feat/worker-graceful-drain` merge into master is complete

Task: `01a099e3-8f46-714a-b9fc-c20c8ac65549` (epic reframed from "resolve the
conflict" to "verify the merged state" — the resolution had already landed).

Repo: `/home/user/work/GoPi`. Branch carrying this report:
`docs/worker-graceful-drain-merge-verification` (worktree
`/dev/shm/gopi-test-scratch/wt-verify-wgd`).

**Verdict: the merge is complete and correct.** `feat/worker-graceful-drain`
(tip `753caeda`) is an ancestor of master via the real two-parent merge commit
`f7fbaa4b`; the branch's drain code, its 12 unit tests and its devdoc are all
on master; `go build ./...` is green and every drain test passes on master.
No merge defect was found, so nothing was fixed and no fresh branch was needed
beyond the documentation branch below.

## Completed

### 1. The merge commit and the ancestry proof

| Fact | Evidence |
| --- | --- |
| Branch tip | `753caeda` `feat(worker): graceful drain for --stop/--restart, consumed at the claim boundary` |
| Merge commit | `f7fbaa4b` — `resolve merge feat/worker-graceful-drain into master`, `2026-09-13 17:06:38 +0800` |
| Two parents | `git log -1 --pretty='%h %p'` → `f7fbaa4b 753caeda c7ce3f16` (parent 1 = the branch tip, parent 2 = master `c7ce3f16`) |
| Ancestry | `git merge-base --is-ancestor 753caeda master` → **exit 0** |
| Nothing left behind | `git rev-list --count master..753caeda` → **0** |

`753caeda` also survived on `refs/tags/version/v20260913-0906-f7fbaa4b` and on
several later branches (`docs/tmpdir-merge-resolution-report`,
`fix/peak-hour-build-gaps`, …), so the branch's history is not merely reachable
through master but independently preserved.

### 2. The branch's artifacts are on master

All 18 files the branch tip added/changed exist on master. The three named in
the acceptance brief:

| Artifact | On master |
| --- | --- |
| `cmd/agent/worker_drain_unit_test.go` | present, **byte-identical to the branch's copy** (`git diff 753caeda master -- <file>` is empty), 386 lines, 12 `Test…` functions |
| `cmd/agent/cli_worker_control.go` drain code | present — 78 `drain`/`Drain` mentions (branch: 77) and every branch-side function kept: `workerForeignDrainBinary`, `workerDrainAgeNote`, `workerControlState.DrainReport`, `workerStateWriter.MarkDraining`, `workerLiveDrainRequests`, `workerDrainRequestedFor`, `workerDrainRequestAge`, `workerDaemonCtx.consumeDrainRequests`, `markDraining`, `drainRequestPending`, plus `workerMarkDrainRequested`/`workerDrainRequestedByFile`/`workerDrainLine`/`awaitWorkerStopChild(drain bool)`/`awaitWorkerDrainChild` in `cli_tasks_worker_stop.go` |
| `devdoc/worker-graceful-drain.md` | present, 151 lines (149 added by the branch + the 2-line ownership-pointer header backfilled later by `7697e1aa`) |
| `devdoc/worker-graceful-drain-evidence/` (9 files) | present |

`comm -23` between the branch's function list and master's for
`cli_worker_control.go` is **empty** — the resolution dropped no branch-side
function. `go vet ./cmd/agent/` exits 0, so no symbol is declared twice (the
classic "both sides added the same helper" merge trap) and nothing became
unreachable.

### 3. The resolution did not damage the drain work

The merge's own delta relative to the branch side (`git diff f7fbaa4b^2
f7fbaa4b`) is additive: 386 lines for the unit test, 149 for the devdoc, +422/−23
in `cli_worker_control.go`, with every branch-side symbol still defined and
referenced. The deletions are the branch's own lines being re-expressed by
master's newer call sites (e.g. the old inline stop block replaced by
`workerControlStopOne`), not lost behaviour — `grep` shows `workerControlStopOne`
defined once and referenced 4×, `workerControlStop` 12×, etc.

No residual conflict markers anywhere in tracked Go sources:
`git grep -n -E '^(<<<<<<<|>>>>>>>)' -- '*.go'` → empty.

### 4. Build

```
$ CGO_ENABLED=0 go build ./...
BUILD_EXIT=0                      # repo /home/user/work/GoPi @ master db1ac4aa
$ CGO_ENABLED=0 go build ./...    # worktree @ master cec425cc (tree pin for the tests)
BUILD_EXIT=0
$ CGO_ENABLED=0 go build -o /tmp/gopi-verify ./cmd/agent/
BUILD_EXIT=0
$ go vet ./cmd/agent/
VET_EXIT=0
```
go 1.24.5 linux/amd64.

### 5. Tests

**Drain unit tests on master — all PASS.** Two independent runs in
`/home/user/work/GoPi` (`go test ./cmd/agent/ -count=1` with a topical `-run`):

```
--- PASS: TestStopChildGracefullySignalsTermFirst (0.15s)
--- PASS: TestStopChildGracefullyEscalatesOnlyAfterTheGrace (0.17s)
--- PASS: TestStopChildGracefullyToleratesAnAlreadyDeadChild (0.15s)
--- PASS: TestWorkerDrainedExitSemantics (0.02s)
--- PASS: TestWorkerDaemon_OnceDrainsAndExits (2.22s)
--- PASS: TestWorkerDrainConsumesRequestAtBoundary (0.00s)
--- PASS: TestWorkerDrainConsumeIsIdempotent (0.00s)
--- PASS: TestWorkerDrainRequestDetectedForPid (0.00s)
--- PASS: TestWorkerDrainRenderNamesTheDrain (0.00s)
--- PASS: TestWorkerRoundRefusesClaimWhileDraining (0.01s)
--- PASS: TestWorkerForeignDrainBinaryDecidesTheSignal (0.00s)
--- PASS: TestWorkerStopReleasesOwnWorktree (0.13s)
PASS
ok  github.com/nick/gopi/cmd/agent  3.306s
```
A second, wider run of the same topical set on the master tree `cec425cc`
(`ok … 14.472s`) reproduced it green; a third run on the same `cec425cc` tree
with the test scratch tree moved off the full `/dev/shm`
(`TMPDIR=/home/user/scratch/verify-wgd/tmp GOPI_TEST_SCRATCH_HOME=…/scratch`)
was green too: **`ok github.com/nick/gopi/cmd/agent 12.937s`**. So the drain set
is green on the current master tip as well, not only on the tip the merge
landed on.

**Whole `cmd/agent` package: no code failure, but it does NOT pass inside Go's
default 10-minute package timeout — proven environmental, not merge damage.**
The run ends `panic: test timed out after 10m0s`, `running tests:
TestRunChat_UnknownSeedErrors (9m53s)` — a chat-seed test that scans
`~/.gopi/sessions`, stuck in `os.ReadDir` via
`internal/sessionlayout.DirHasRecord` / `gopilog.FindSession`.

Root cause, measured:

```
$ ls -f ~/.gopi/sessions | wc -l     → 68221   (68k+ session directories)
$ time find ~/.gopi/sessions -maxdepth 1 | wc -l
real 0m15.317s                        (15s for ONE raw directory listing)
```
The test binary runs plain `go test` locally, so `HOME=/home/user` and the real
68k-entry sessions root is scanned; the listing alone costs ~15s and the test
walks it repeatedly. Under concurrent agent load (load average was 20 during
these runs) that exceeds 10 minutes.

Proof that this is pre-existing environment cost rather than this merge:

| Run | Tree | HOME | Result |
| --- | --- | --- | --- |
| A | `master db1ac4aa` | real | `timeout 400` → **exit 124** (killed at the cap), no failure output |
| B | `master db1ac4aa` | real | same test **PASS (484.83s)**, `ok … 485.561s`, exit 0 |
| C | pristine **pre-merge** `c7ce3f16` (scratch worktree, same real HOME) | real | same test **PASS (458.36s)**, `ok … 458.859s`, exit 0 |
| D | master, `GOPI_HOME` pointed at an empty scratch home | synthetic | no hang at all (`ok 0.622s` for the subset; full package 211s) — the cost disappears exactly when the giant sessions root is not scanned |

Runs A/B isolate it to *budget*: the same binary on the same tree passes with
enough time. Run C is the decisive control — the identical test on the
**pre-merge** parent needs the same ~460s, so the merge neither introduced nor
worsened it. Run D confirms the 68k-dir scan is the cost.

Consistently, none of the involved files were touched by either side of the
merge (`git diff --name-only f7fbaa4b^1 f7fbaa4b -- <file>` and
`… f7fbaa4b^2 -- <file>` are both **empty** for `cmd/agent/cli_chat_test.go`,
`cmd/agent/cli_resume.go`, `internal/gopilog/reader.go`,
`internal/sessionlayout/sessionlayout.go`, `internal/tools/tasks.go`).

Run D's nine extra failures are an artefact of *my* `GOPI_HOME` override, not
defects: pointing `GOPI_HOME` at an empty dir removes the very fixtures those
tests read (`--last … "No sessions found in …"`, `cli_resume_test.go`
"neither a raw log path nor an existing session id"). That run is cited only
for "no hang without the 68k dirs", never as a package verdict.

**One further failure was disk exhaustion, not code.** A first topical run in
the scratch worktree reported

```
--- FAIL: TestWorkerStopReleasesOwnWorktree
    worker_stop_accept_test.go:115: open ledger: database or disk is full (13)
--- FAIL: TestWorkerDrainedExitSemantics
    memory schema init: commit: database or disk is full (13)
    git init …: error: copy-fd: write returned: No space left on device
```
`df -h /dev/shm` → **100% used, 36K free** (7.8G tmpfs, shared with other
concurrent agent sessions). Re-running the identical set with the test scratch
tree on `/home/user/scratch/verify-wgd` (off the `/` partition, Rule 22) →
**`ok … 12.937s`**, all drain tests PASS.

## Not Done

- **`go test ./...` was not run and is not claimed green.** The epic explicitly
  forbids it as an exit gate (known pre-existing full-suite hang, documented in
  `devdoc/suite-hang-fix-t1788361069.md` and eight other devdocs). Only
  `./cmd/agent` (the merge's package) plus build/vet were exercised.
- **The full `cmd/agent` package was never observed `ok` inside one command on
  the real HOME.** It is not green-by-default here: it needs a
  `-timeout` above ~10m (measured 484s for the slow test alone, and that was on
  an idle-ish compare point; run A shows the same test exceeding 400s under
  load). I ran it with 10m and with 25m; the 25m variant failed only for the
  `GOPI_HOME`-override fixtures described above. A genuinely clean full-package
  run needs `-timeout 30m` on the real HOME — **not attempted**, out of the
  bounded-verification budget. This is recorded as an environment/tooling
  finding, not as a merge defect.
- **The 68k-directory `~/.gopi/sessions` growth was not fixed** (it is the
  underlying cause and belongs to a session-GC task, not to a merge
  verification). No bug task was filed for it in this session.
- **The other agents' concurrent masters were not chased.** Master moved twice
  while verifying (`db1ac4aa` → `cec425cc`, other tasks' merges). Both tips were
  checked; no post-merge commit modified the drain code. If master moves again,
  the ancestry test alone re-establishes the deliverable.
- **The `/dev/shm/gopi-ctl-premerge` scratch worktree (48M) was left behind.**
  I created it outside the declared worktree roots, so both the cleanup gate and
  `git worktree remove` refuse it; it needs a manual `git worktree prune` after
  deleting the directory. It is a control artefact only — no deliverable is in
  it.
- **`cmd/agent/out.txt`** (4 bytes, `done`, untracked, dated 2025-09-13 19:33)
  was found pre-existing in the main checkout — the known leaked-write symptom
  its test `TestTaskRunNormalExitSingleRowE2E` guards. It is not from this
  task and was not committed or deleted.

## Verification

- Ancestry: `git merge-base --is-ancestor 753caeda master` → **exit 0**
  (`git rev-list --count master..753caeda` → 0).
- Merge commit: `git log -1 --pretty='%h %p' f7fbaa4b` → `f7fbaa4b 753caeda c7ce3f16`.
- Build: `CGO_ENABLED=0 go build ./...` → **OK** (exit 0) on both `db1ac4aa` and
  `cec425cc`; `go vet ./cmd/agent/` → **exit 0**.
- Tests: drain topical set → **12/12 PASS** (`ok … 3.306s`, re-confirmed
  `ok … 14.472s` on `db1ac4aa`/`cec425cc`, and `ok … 12.937s` on `cec425cc`
  with scratch off the full tmpfs). `-run TestRunChat_UnknownSeedErrors` on
  master HOME=real → **PASS 484.83s**; same test on pristine pre-merge
  `c7ce3f16` → **PASS 458.36s** (pre-existing environment cost, merge delta 0).
- Markers: `git grep -n -E '^(<<<<<<<|>>>>>>>)' -- '*.go'` → empty.
- Report commit: this file, on
  `docs/worker-graceful-drain-merge-verification` (see the final declaration in
  the task's closing message for the hash and recovery command).
