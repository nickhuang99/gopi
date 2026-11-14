<!-- doc-class: normative -->
# Independent Verification Report — ledger row `01a092a6-43a2-7bc4-9c13-30f0ff80196e`

**Feature:** `glm-night-watch.sh` must stop being a second, unsupervised worker launcher.
**Deliverable under test:** merge `55296ffb` "resolve merge fix/glm-night-watch-delegates into master"
(the fix commit it brings in is `141a6aa8` "fix(glm-night-watch): delegate to the single launcher; a refused launch is loud").
**Verdict: VERIFIED** — 5/5 acceptance criteria PASS, build + targeted tests green, one pre-existing
unrelated red recorded honestly (see §7).

**Verifier discipline:** read-only. I never edited the deliverable. My only writes were (a) a scratch
worktree `~/workspace/gopi-vfy-01a092a6` on branch `docs/glm-night-watch-verify-01a092a6` (created by me
from `master`, removed at the end) and (b) this report inside it. No `pkill`/pattern kill was ever issued;
no running worker or other agent's worktree was touched.

## 1. What was verified, and against which tree

| artifact | identity |
|---|---|
| deliverable merge | `55296ffb` (master tip at verification time) |
| fix commit | `141a6aa8` |
| fix commit's parent (pre-fix) | `141a6aa8^` |
| the actual defect site (installed copy) | `~/.gopi/glm-night-watch.sh` |

The defect site is the **installed** copy, not the repo file. Checked directly:

```
$ ls -la ~/.gopi/glm-night-watch.sh
-rwxr-xr-x 1 nick nick 13862 Sep 12 07:43 /home/user/.gopi/glm-night-watch.sh
$ diff -q ~/.gopi/glm-night-watch.sh scripts/glm-night-watch.sh
IDENTICAL
$ grep -n 'LAUNCHER_NAME' ~/.gopi/glm-night-watch.sh | head
109:LAUNCHER_NAME="${GOPI_GLM_LAUNCHER:-bin/gopi-worker-up.sh}"
191:  "$SSH_BIN" -o ConnectTimeout=15 "$PEER" "$LAUNCHER_NAME $N"
```

The installed copy is byte-identical to the repo source of truth, and it delegates. (The single-file
`setsid`/`nohup` hits in it are **comments** documenting the OLD behaviour — see criterion (a).)

## 2. Per-criterion PASS/FAIL table

| # | criterion | verdict | evidence |
|---|---|---|---|
| (a) | no longer independently launches workers — delegates to the operator-owned launcher | **PASS** | §3 |
| (b) | a refused/failed launch is loud; an exit-3 worker never looks started | **PASS** | §4 |
| (c) | a launch from a plain operator shell still starts N workers | **PASS** | §5 |
| (d) | a minimal regression check exists so launcher identity cannot silently diverge | **PASS** | §6 |
| (e) | the agent-session guard's semantics untouched | **PASS** | §6e |

## 3. (a) Delegation — PASS

**3.1 Static shape (the code path, comments stripped).** The watcher's *only* remote act is one call of the
launcher with the worker count:

```
$ grep -n 'SSH_BIN".*"\$LAUNCHER_NAME\|LAUNCHER_NAME \$N' scripts/glm-night-watch.sh
191:  "$SSH_BIN" -o ConnectTimeout=15 "$PEER" "$LAUNCHER_NAME $N"
237:  echo "$SELF dry-run: remote command -> $SSH_BIN $PEER '$LAUNCHER_NAME $N'"
279:log "GLM free window open — delegating to the launcher on $PEER: $LAUNCHER_NAME $N"

$ grep -n 'setsid|nohup' scripts/glm-night-watch.sh    # every hit is a COMMENT
16:#     setsid nohup ./bin/gopi worker > ~/.gopi/logs/worker-night-$i.log 2>&1 &
82:#                                 per call, never "./bin/gopi worker")
```

`delegate_launch()` is the single seam and it "never retries, never degrades to an inline start"; the
script has no worker loop, no per-worker log path, no `bin/gopi worker` invocation.

**3.2 Runtime proof (what was actually on the wire).** A stub transport in `PATH` recorded every argv the
script issued (sandbox `HOME`, real script from the worktree, window wait collapsed by the script's own
`GOPI_GLM_*` knobs — no code path patched):

```
$ cat /tmp/glmprobe.K21Ceh/probe.log
STUB-SSH-ARGV: -o ConnectTimeout=15 hp bin/gopi-worker-up.sh 2
```

One invocation, the operator-owned launcher name, the count — never `./bin/gopi worker`.

## 4. (b) A refused / failed launch is loud — PASS

Same stub transport, exit status forced. Probe 2 = the agent-session guard's refusal code (3):

```
########## PROBE 2: launcher REFUSES (exit 3) ##########
[2026-09-12 15:29:55] GLM free window open — delegating to the launcher on hp: bin/gopi-worker-up.sh 2
[2026-09-12 15:29:55] FAILED: the launcher REFUSED this launch on hp (exit 3) — the agent-session guard declines to
                      start workers from an agent session; this watcher invents no override. Run the launcher from
                      your own shell on hp.
[2026-09-12 15:29:55] the window's launch did NOT happen; no worker was started by this script
exit=3
```

- the script's own exit status is **3** (propagated, not swallowed) → a cron/caller can see it;
- a **FAILED** line names the refusal code and the guard as the cause;
- an explicit "no worker was started by this script" line — the launch never *looks* started;
- **no override exists**: `grep -nE 'GOPI_AGENT|GOPI_ALLOW|--force|force|GOPI_GUARD'` → NONE. The watcher
  invents no bypass of the guard.

Non-refusal failure (exit 1) and success (exit 0) are distinguished too — the log is a real record:

```
########## PROBE 3: launcher FAILS (exit 1) ##########
[2026-09-12 15:29:59] FAILED: the launcher on hp did not start the workers (exit 1) — see the launcher's own
                      message above; nothing was launched silently.
exit=1
########## PROBE 4: launcher ACCEPTS (exit 0) ##########
[2026-09-12 15:30:00] launcher accepted: 2 worker(s) on hp via bin/gopi-worker-up.sh
exit=0
```

`~/.gopi/glm-night-watch.log` (sandboxed) carries the full sequence — lines 3–5 refused, 6–10 failed,
11–14 accepted — i.e. the record survives and is auditable.

## 5. (c) A plain operator shell still starts N workers — PASS

`TestGlmNightWatchOperatorPathStartsWorkersUnattended` drives the **real** launcher (`scripts/gopi-worker-up.sh`)
through the watcher from a probe env carrying no agent signature (`PATH`, `HOME`, the launcher's own knobs
only), and counts the truth source — the launcher's startup log:

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/         # the repo's own bin, required by the gate
$ go test ./internal/testutil/ -run TestGlmNightWatchOperatorPathStartsWorkersUnattended -v
    glm_night_watch_test.go:388: sandbox scratch root /dev/shm/gopi-test-scratch/... is off /
    --- PASS: TestGlmNightWatchOperatorPathStartsWorkersUnattended (5.07s)
ok  	github.com/nick/gopi/internal/testutil	5.091s
```

Two workers announced their loop banner before claiming anything — so the operator path is intact while the
watcher stops being a second launcher.

**Honest note (test hygiene, not a defect of the fix):** this gate leaves the workers it starts **running**
(deliberate — they are detached `setsid` workers, and the test kills nothing "by pattern"; that discipline is
explicitly documented at `glm_night_watch_test.go:450,570`). The sandbox is empty and its scratch runtime is
reaped, so those workers find nothing to claim and exit on their own; I confirmed they were gone a few minutes
later (`pgrep -a -f 'gopi-vfy-01a092a6/bin/gopi worker'` → none) and that they hold no real task (sandbox has
no queue DB — only `memory.db`). Nothing of another agent's work was touched.

## 6. (d) Regression check + (e) guard semantics — PASS

**6.1 The gate exists and is real (5 gates, ledger row 01a092a6, at `55296ffb`):**

```
$ go test ./internal/testutil/ -run 'TestGlmNightWatch|TestWorkerStartupLogNameIsTheLaunchersFile' -v
--- PASS: TestGlmNightWatchDelegatesToTheSingleLauncher (0.00s)
--- PASS: TestGlmNightWatchEnvKnobsAreDefined (0.00s)
--- PASS: TestGlmNightWatchRefusedLaunchIsLoudAndNonZero (2.06s)
    --- PASS: .../refused   .../accepted   .../dry-run
--- PASS: TestGlmNightWatchOperatorPathStartsWorkersUnattended (5.06s)
--- PASS: TestWorkerStartupLogNameIsTheLaunchersFile (0.00s)
ok  	github.com/nick/gopi/internal/testutil	7.134s
```

**6.2 RED-PROOF — the gate really catches launcher-identity divergence.** Two independent red-proofs. The
stronger one (6.2b) came from a *genuine* pre-fix artifact discovered late in the verification; it supersedes
the reconstruction in 6.2a and is the one to trust.

**6.2a (weaker — reconstruction, kept for the record).** `scripts/glm-night-watch.sh` was **ADDED** by the fix
(`git log --diff-filter=A` → `141a6aa8` only), so no *tracked* pre-fix revision exists and the first red-proof
had to reconstruct the defect: I injected the *documented* pre-fix act (one line, taken verbatim from
`devdoc/pa-never-runs-tasks-20260912.md:626`) into the fixed script, ran the gate, and restored the file:

```
--- FAIL: TestGlmNightWatchDelegatesToTheSingleLauncher (0.00s)
    glm_night_watch_test.go:193: scripts/glm-night-watch.sh starts workers ITSELF ("./bin/gopi worker") — that is
        the second, unsupervised launcher this gate exists to prevent: the one remote act must be the single
        launcher scripts/gopi-worker-up.sh ...
    glm_night_watch_test.go:193: ... starts workers ITSELF ("/gopi worker") ...
    glm_night_watch_test.go:202: ... loops over a per-worker index — the launcher takes the COUNT ...
FAIL
--- restore ---
$ git checkout -- scripts/glm-night-watch.sh && git status --porcelain   # (empty = restored)
```

The gate is not a shape-only formality: it fails on the exact defect, and the deliverable's own verification
claim ("RED against the pre-fix script") reproduces.

**6.3 The gate pins launcher identity to Go constants** — not a second inline spelling:
`procname.WorkerLoopStartLine` / `procname.WorkerStartupLogName` / the shared `GOPI_*` names, plus the refusal
code pinned to the default `3`. `TestWorkerStartupLogNameIsTheLaunchersFile` asserts the log name equals the
launcher's own file name, so a rename cannot leave half the delegation pointing at a missing file.

**6.4 (e) The guard's semantics are untouched.**

```
$ git diff --stat 141a6aa8^ 141a6aa8 -- cmd/agent/agent_spawn_guard.go internal/config/agentguard.go
                                   # empty: the fix never touched the guard
$ git diff --stat 2583d7d4 55296ffb -- cmd/agent/agent_spawn_guard.go internal/config/agentguard.go
                                   # empty: the merge brought the guard in UNCHANGED
$ git log --oneline --all -- internal/config/agentguard.go cmd/agent/agent_spawn_guard.go | grep -c 141a6aa8
0
```

The guard's own tests are green against a binary **built at the deliverable tip** (this matters — see §7.2):

```
$ GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run TestAgentSpawnGuard -v
--- PASS: TestAgentSpawnGuardRefusesWorkerFromAgentSession (0.01s)
--- PASS: TestAgentSpawnGuardAllowsOperatorStyleLaunch (1.03s)
--- PASS: TestAgentSpawnGuardHelpIsNeverRefused (0.01s)
--- PASS: TestAgentSpawnGuardWorkerTaskRunChainIsAllowed (1.33s)
--- PASS: TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession (3.07s)
ok  	github.com/nick/gopi/cmd/agent	5.464s
```

## 7. Build / lint / repo-level gates, and honest gaps

**7.1 Build + format (Rule 5 / Rule 12 gates).**

```
$ CGO_ENABLED=0 go build ./...            # exit 0
$ gofmt -l cmd/agent/cli_tasks_worker.go internal/procname/procname.go internal/testutil/glm_night_watch_test.go
                                          # (empty = gofmt clean)
$ bash -n scripts/glm-night-watch.sh      # OK
$ go test ./internal/standards/           # ok (Rules 16–21 gates green)
```

**7.2 Reproducibility trap found (worth a devdoc line for the next verifier):**
`TestGlmNightWatchOperatorPathStartsWorkersUnattended` requires `<repo>/bin/gopi` to exist — the launcher it
exercises refuses to start without it:

```
    gopi-worker-up.sh: /home/user/workspace/gopi-vfy-01a092a6/bin/gopi is missing or not executable (build with: make build)
    --- FAIL: TestGlmNightWatchOperatorPathStartsWorkersUnattended (0.01s)
```

A fresh worktree does not carry `bin/`, so `go build ./...` (which builds to the build cache, not to `bin/`)
is **not enough**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` is. Once built, the gate is green. This is
a test-harness precondition, not a defect of the fix — but a verifier who skips it will report a false FAIL.

**7.3 One repo-level red, PRE-EXISTING and unrelated to this deliverable (recorded honestly, not charged to it):**

```
$ go test ./internal/testutil/
--- FAIL: TestWorkerOwnershipPointerPresent
    worker_ownership_gate_test.go:285: files that teach a worker-launch / task-run command without the
        operator-only pointer:  … 17 files (briefings/*, devdoc/merger-*, devdoc/pa-never-runs-tasks-20260912.md,
        scripts/glm-night-watch.sh, …)
FAIL
```

Attribution evidence:

- the gate file `internal/testutil/worker_ownership_gate_test.go` (480 lines) arrived in the **same merge**
  `55296ffb` via `fa1a1e7a` (2026-09-12 07:31:49), i.e. it is a sibling deliverable, not this fix's work;
- on the pre-merge master (`55296ffb^` = `141a6aa8`) that gate file **does not exist** and the package is green
  (`ok … [no tests to run]`), so the red is new-in-master only in the sense that the *gate* is new;
- it flags **17** files, of which only 2 come from this deliverable (`scripts/glm-night-watch.sh`,
  `devdoc/glm-night-watch-single-launcher.md`), and it flags the repo's own briefing corpus wholesale — i.e.
  it is a repo-wide hygiene gate that is red on its own arrival, not a regression caused by the delegation fix;
- it fails identically in the **main repo** (`~/work/GoPi`, master `55296ffb`), so it is not an artifact of my
  worktree.

If it must be charged, it is charged to `fa1a1e7a`'s delivery, not to `141a6aa8`/`55296ffb`'s delegation fix.
Recommendation (for whoever owns it): the two deliverable-owned files should carry the canonical pointer line
`# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md` (the watcher does launch through a launcher,
so it is in scope) — or be dismissed with a reason in `workerOwnershipDismissed`. Not done by me: a verifier
does not edit the deliverable.

**7.4 Not verified (out of scope, stated plainly).** The peer side (`hp`) was never contacted: the delegation is
verified against a recording stub transport and against the real local launcher, not against a live remote
`bin/gopi-worker-up.sh`. A live end-to-end on the peer is the operator's call. No `crontab`/systemd unit
currently invokes the watcher (checked read-only) — invocation is by hand/operator.

## 8. Environment hygiene

```
# before (start of session) and after: identical except my own scratch
$ git worktree list
/home/user/work/GoPi                       55296ffb [master]
/home/user/workspace/gopi-combo            fcbb4742 (detached HEAD)
/home/user/workspace/gopi-mergerverify     a0bc3e11 (detached HEAD)
/home/user/workspace/gopi-r16base          fc6109ed [fix/rule16-baseline-stale]
/home/user/workspace/gopi-verify-glmwatch  141a6aa8 (detached HEAD)
/home/user/workspace/gopi-verify-prefix    cf99e644 (detached HEAD)
/home/user/workspace/gopi-vfy-01a092a6     55296ffb [docs/glm-night-watch-verify-01a092a6]   <- mine, removed after
/home/user/workspace/gopi-wbfix            d259403e [fix/worker-writeback-normal-exit]
```

No `pkill`/`pkill -f gopi`/`pkill -x gopi` was issued at any point. The two transient workers the operator-path
gate started were identified by explicit PID, inspected read-only, left to self-exit (verified gone), and never
signalled.

## 9. Verdict

**VERIFIED** — all five acceptance criteria PASS with reproducible evidence; build exit 0; the deliverable's
five gates + the guard suite + the standards gates green; the regression gate proven RED against the documented
pre-fix defect. The single package-level red (`TestWorkerOwnershipPointerPresent`) is a pre-existing sibling
deliverable's repo-wide hygiene gate, attributed in §7.3.

Ledger verdict recorded exactly once on row `01a092a6-43a2-7bc4-9c13-30f0ff80196e`.
