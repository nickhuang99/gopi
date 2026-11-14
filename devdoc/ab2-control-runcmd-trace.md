<!-- doc-class: historical -->
# Task Report: AB2-CONTROL — auto task missing run_cmd field (trace + closure)

Branch: `feat/ab2-control-runcmd-trace` (HP node /home/user/work/GoPi, base 1c407f4 = peer1/master aligned)

## Goal
Trace the reported bug — an auto-submitted task row missing the `run_cmd`
field that manually-submitted tasks carry — and make sure auto-submitted
tasks carry every field a manual task does. This task is itself an A/B
control run (clone of peer1 t-1788526902; HP ran the baseline bin without
the feature at first, same DS model; A/B #2 vs peer1 feature build).

## Completed

### 1. Bug reproduced with live ledger evidence (two shapes)
| row | spawn | bin | run_method | run_cmd |
|---|---|---|---|---|
| t-1788528730 (wrapper, human) | 21:32 | — | manual | `--settings … gopi task run t-1788528730` ✓ |
| **t-1788528735 (child-1 mirror, auto)** | **21:32** | **pre-fix 67d56d3** | **'' (empty)** | **'' (empty)** ← the bug |
| t-1788530475 (child-2 mirror, auto, this session) | 22:01 | fixed (21:53) | manual | `--settings … gopi task run t-1788528730` ✓ |
| t-1788531029 (isolated sandbox mirror, auto) | 22:10 | canonical 1c407f4 | **child** | `--settings … gopi task run t-1788531028` ✓ |

Root cause (matching the canonical peer1 report devdoc/auto-mirror-runcmd.md,
324b54b): `runTaskRunOnce` (cmd/agent/cli_tasks.go) stamps only the WRAPPER
row right before spawn; the CHILD's auto-register hook
(`maybeRegisterSessionTask`, internal/agent/taskregister.go) tagged
`wrapper:<id>` but never stamped the mirror row's own provenance — so the
[auto] mirror row stayed run_method='' / run_cmd='' forever.

### 2. Two parallel fixes existed; HP aligned to the canonical one
- **peer1 canonical (merged, master 324b54b → 1c407f4)**: new
  `TaskRunProvenance(taskID)` helper + `maybeRegisterSessionTask` stamps the
  mirror row `method=child` with the wrapper's exact launch command at
  auto-registration (post-insert read-back). Ships regression tests
  (`TestAutoTaskRegisterMirrorRunProvenance`, `TestTaskStampRun`,
  `TestSyncDeltaRunProvenanceTravels`). A/B-verified: tests fail on pre-fix
  tree, pass on fix.
- **HP parallel (uncommitted working tree, now `stash@{0}`)**: a different
  shape — insert-time provenance via `TaskNewReturningIDProvenance` (extends
  the INSERT with run_method/run_cmd columns) + env passthrough
  (`GOPI_RUN_METHOD`/`GOPI_RUN_CMD`, cmd layer → spawned child → NewAgent
  seams) + Agent.runMethod/runCmd fields. This is what child-1 (session
  0cf87580) implemented live at 21:38–21:52 while running the baseline bin,
  and what my own auto row t-1788530475 was born under (method=manual).
  Functionally equivalent, structurally divergent. Stashed (recoverable)
  with a descriptive message; NOT merged — canonical 324b54b is the keeper.

### 3. HP repo-state closure (Rule 6 / ff-only hygiene)
HP master was at pre-fix 67d56d3 with child-1's uncommitted work on top.
Action: `git stash push` the parallel fix → `git pull --ff-only peer1
master` → HP master now at 1c407f4 (canonical fix + docs) →
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → bin/gopi 22:10:13
(1c407f4). Stale-bin trap avoided (the A/B #1 report 7ee2301 documents the
same stale-bin pattern — bin not rebuilt after merge).

### 4. Live verification (canonical path, isolated sandbox HOME)
`HOME=sandbox/abverify gopi task run t-1788531028` on the aligned build
logged: `task ledger: t-1788531029 mirror run stamp child: --settings …
gopi task run t-1788531028`. DB confirms the mirror auto row was BORN
complete: run_method=child + full re-runnable run_cmd — the exact field
parity the bug demanded. Sandbox removed after verification.

## Not Done
- No backfill of historical empty auto rows (canonical report's stance:
  closed/terminal rows, stamping history would be noise; provenance is
  recorded at run start going forward).
- No merge of this report branch / no code change — the code fix is
  canonical on peer1/master; this run was a trace + closure task.
- Orphan rows left in_progress: t-1788528735 (child-1 control session
  killed 22:05 mid-run — harness orphan, see memory #1229 recipe) and the
  still-live wrapper chain t-1788528730 / t-1788530475 (this session).
- stash@{0} (HP parallel fix) kept for reference, not applied.

## Verification
- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` OK → bin/gopi 1c407f4 (22:10:13)
- Tests (canonical regression guards, on aligned HP master):
  `go test ./internal/agent/ -run TestAutoTaskRegisterMirrorRunProvenance` → ok
  `go test ./internal/tools/ -run 'TestTaskStampRun|TestSyncDeltaRunProvenanceTravels'` → ok
- Live: sandbox task-run child auto row born with run_method=child + run_cmd
  (evidence above); real-ledger child-2 row t-1788530475 carries run_cmd
  (born under the parallel fix — proof the gap was closing on both shapes)
- Commit: on `feat/ab2-control-runcmd-trace` (this report)
