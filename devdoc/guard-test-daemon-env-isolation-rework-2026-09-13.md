<!-- doc-class: historical -->
# Task Report: forward rework — feat/guard-test-daemon-env-isolation (reverted merge d45d90e2)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns `gopi worker` / `gopi task run`; an agent must never run it (submit the work instead: `gopi task submit`). The `gopi worker --once` / `gopi task run` shapes quoted below are the merger's own sweep record and in-process test invocations, never a recipe for an agent to follow.

**Rework task**: `01a0998c-b7f2-7543-834e-ab4d7c87a978` (filed by the merge-verifier after the FAIL in §3)
**Branch**: `feat/guard-test-daemon-env-isolation` — the commit carrying this report is the branch tip
**Worktree**: `/home/user/workspace/gopi-guard-rework` (Rule 6a) — released at the end (§9)
**Type**: forward rework of a REVERTED merge — the original fix's intent is kept; none of the original work is discarded.

---

## 1. The commits that define this rework

| Role | Hash | Subject |
|---|---|---|
| original merge (reverted) | `d45d90e2857c4cb7941461158e5b1fb0133e0192` | Merge branch 'feat/guard-test-daemon-env-isolation' |
| 2nd parent of that merge = **the ORIGINAL branch tip** | `9f77e133747865d5e31f29d3ffdddff14ea71713` | docs(guard-test-isolation): correct the TestMain no-restore rationale |
| the revert | `d07e0d317fc148fc7aab5d9de8e7cea46c83e329` | Revert "Merge branch 'feat/guard-test-daemon-env-isolation'" |
| master when this rework started | `e8e94ca0688b5daadeb821ad2167392aa2c1f79f` | Merge branch 'fix/cmd-agent-inherited-contract-reds' |
| **rework code commit** | `65d068424fe54f1cec308f378e0861ec3a70fcc7` | fix(cmd/agent): re-apply the daemon-handshake test isolation, with the worker-ownership pointer |
| master at the end (already merged this branch) | `30d1a6f7d9d7573f63d577e1d7175ed0cef657b5` | Merge branch 'feat/guard-test-daemon-env-isolation' |

The branch ref had been **deleted** by the revert round (only the merge commit survived on
master). Recovery used the merge's **second parent**, which is the branch's original tip:

```
git branch feat/guard-test-daemon-env-isolation d45d90e2^2   # → 9f77e133
```

`9f77e133` IS the tip the merge took, and `90f44071` (the code commit) is its ancestor, so
the recovered ref is the branch as it was, not a reconstruction:

```
d333382e ── 90f44071 ── 574e9e00 ── 9f77e133 = d45d90e2^2   (the original branch)
```

## 2. How the reverted content was restored (and the conflict count)

Not by hand — **the revert of the revert**. `d07e0d31`'s parent is the merge `d45d90e2`, so
re-applying it on top of the current master restores exactly the diff the branch carried and
lets git do the three-way merge:

```
git worktree add ~/workspace/gopi-guard-rework feat/guard-test-daemon-env-isolation
git merge master          # e8e94ca0 in — the branch's own delta untouched
git revert -n d07e0d31    # restore the reverted content
```

**Conflict resolutions: NONE (0 conflicts).** `git revert -n d07e0d31` applied clean; the 4
files came back exactly as the original branch had them
(`cmd/agent/agent_spawn_guard_env_isolation_test.go` +137, `cmd/agent/agent_spawn_guard_test.go`
23±, `cmd/agent/testmain_test.go` +49, `devdoc/tasks/guard-test-daemon-env-isolation.md` +174).
Why clean: master's `e8e94ca0` round did not touch those paths, so neither side of the
three-way merge disagreed.

Byte-identity of the restored code with the original branch — measured, not assumed:

```
$ git diff --stat 9f77e133 65d06842 -- cmd/agent/agent_spawn_guard_env_isolation_test.go \
                                        cmd/agent/agent_spawn_guard_test.go cmd/agent/testmain_test.go
(empty — identical)
$ diff <(git diff d45d90e2^1 d45d90e2^2 -- cmd/agent) <(git diff e8e94ca0 65d06842 -- cmd/agent)
IDENTICAL
```

## 3. The post-merge red, its root cause and its fix

**The recorded DELTA FAIL**: `internal/testutil` `TestWorkerOwnershipPointerPresent` FAILed on
the merged state (master alone: `ok`). The verifier's attribution: adding *only*
`devdoc/tasks/guard-test-daemon-env-isolation.md` to a green control tree turns the gate red;
removing it returns green.

**Root cause** (mechanism, not symptom): the branch's report quotes `gopi worker --once` in
its A/B table (lines 38 and 120 — the operator-shaped child the regression case spawns). The
worker-ownership gate (`internal/testutil/worker_ownership_gate_test.go`) walks `devdoc/`,
`briefings/`, `scripts/` for the launch-shape needles (`gopi worker`, `gopi task run`,
`gopi-worker-up.sh`) and requires every trigger file to carry ONE canonical pointer line
(marker + canonical path on the SAME line) — or a reasoned `workerOwnershipDismissed` entry.
The branch's report is a **teach** (it presents the command shape as a runnable case), not a
**cite**; per the gate's own rule the answer is the pointer, **not** a dismiss entry (a
dismiss would un-gate a file that should be annotated).

**The fix** — one blockquote line, directly under the H1:

```
> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns
> `gopi worker` / `gopi task run`; an agent must never run it ... The launch shapes quoted
> below are in-process test invocations inside the suite, not a recipe for an agent to follow.
```

Delta against the original branch is **only this line**:

```
$ git diff 9f77e133 65d06842 -- devdoc/tasks/guard-test-daemon-env-isolation.md
→ +2 lines (the pointer above) and nothing else
```

## 4. What the branch actually changed (intent preserved)

TEST-ONLY. The production spawn guard is untouched — no assertion removed, no predicate
relaxed.

- `cmd/agent/agent_spawn_guard_test.go` — `guardTestEnv` strips the inherited daemon handshake
  pair (`workerDaemonEnv` / `daemonReadyEnv`) for the child-process cases.
- `cmd/agent/agent_spawn_guard_env_isolation_test.go` (new) — the A1 in-process and A2 E2E
  regression cases + `envHas`.
- `cmd/agent/testmain_test.go` — `unpinAmbientDaemonHandshake`, the suite-level pin for the
  **in-process** worker tests (no child env exists there to strip), with
  `TestSuiteProcessIsNotADaemonChild` as its guard. Deliberately no restore: `TestMain`
  `os.Exit()`s right after `m.Run`, so a restore is unreachable code (`// env-leak-ok:` reason
  recorded at the site).

## 5. Gate table

| # | Gate | Result | Evidence |
|---|---|---|---|
| A | `CGO_ENABLED=0 go build ./...` on the merged state | ✅ OK | exit 0; `go vet ./cmd/agent/` exit 0 |
| B | `internal/testutil` worker-ownership gates | ✅ PASS | `-run 'TestWorkerOwnership' -count=1` → `ok 1.768s` (red before the pointer, green after; control green) |
| C | `GuardTestEnvStripsDaemonHandshakeVars`, `SuiteProcessIsNotADaemonChild`, `TestAgentSpawnGuard*` — pair **PRESENT** | ✅ PASS | merged `30d1a6f7`: `ok cmd/agent 8.031s`, exit 0 |
| D | same set — pair **ABSENT** | ✅ PASS | merged: `ok cmd/agent 7.036s`, exit 0 |
| E | remaining FAILs identical on pristine-master control | ✅ empty differential both directions | §7 |
| F | `git merge-base --is-ancestor master <branch>` | ✅ exit 0 | master `30d1a6f7` is an ancestor of the branch |
| G | `bash scripts/safe-merge.sh <branch>` | ✅ exit 0 | §9 |
| H | no force-push / no history rewrite | ✅ | only ordinary commits (`9f77e133` → `e8e94ca0` → `65d06842` → `30d1a6f7` → report); nothing rebased or amended |
| I | Rule 8 (`git add -A` never used) | ✅ | files added one by one |

### 5.1 Re-verification at the CURRENT tip (forward rework, 2nd pass — §10)

Rows A–D were re-measured against `cec425cc` (the tip after master `cec425cc` was merged in);
the numbers above are kept as the original pass's record and §10 carries the current run.
The reference-identity rows F–I were re-checked at `cec425cc` → master `48e623a2` (§10.4).

## 6. A/B — the discriminating measurement (the original fix's own claim)

Every row is a real `go test ./cmd/agent/` run of
`-run 'GuardTestEnvStripsDaemonHandshakeVars|SuiteProcessIsNotADaemonChild|TestAgentSpawnGuard'`.

| Tree | daemon pair | Result | What it proves |
|---|---|---|---|
| control master `8f4042b3` | ABSENT | ✅ `ok cmd/agent 8.866s` (exit 0) | — |
| control master `8f4042b3` | PRESENT | ❌ exit 1 — FAIL `TestAgentSpawnGuardAllowsOperatorStyleLaunch`, `TestAgentSpawnGuardWorkerTaskRunChainIsAllowed`, `TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession` | the defect is real and pre-existing on master |
| merged `30d1a6f7` | PRESENT | ✅ `ok cmd/agent 8.031s` (exit 0) | **the fix closes it** |
| merged `30d1a6f7` | ABSENT | ✅ `ok cmd/agent 7.036s` (exit 0) | the fix does not depend on the pair being present |

The pair was stripped for every *control* run
(`env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u GOPI_MERGER_DAEMON_CHILD`), since
the runner itself is a worker-daemon child; the PRESENT rows set it deliberately.

## 7. Control-tree differential (delta, not "green")

Two full `go test ./... -count=1 -timeout 1500s` pairings, identical flags, both with the
daemon pair stripped — the merged state `30d1a6f7` (master + this branch) against a
**pristine-master control** `8f4042b3` (master immediately before the branch merge).

### 7.1 Run 2 (quiet host — the clean pairing, both directions empty)

| Tree | Exit | FAILs |
|---|---|---|
| merged `30d1a6f7` | **0** | **none** — 31 packages `ok` (`cmd/agent` 463s, `internal/shot` 42.5s, `internal/tools` 271.8s, `internal/testutil` 154.6s …) |
| control `8f4042b3` | **0** | **none** — 31 packages `ok` |

```
$ comm -23 <(grep -E '^FAIL' merged2 | sort) <(grep -E '^FAIL' ctl2 | sort)   → (empty)
$ comm -13 <(grep -E '^FAIL' merged2 | sort) <(grep -E '^FAIL' ctl2 | sort)   → (empty)
```

So on the clean pairing the sorted FAIL sets differ in **neither** direction, and the merged
state is green outright.

### 7.2 Run 1 (host under load ≈ load 27 — three reds, all proven ambient)

| Red (package / test) | merged `30d1a6f7` | control `8f4042b3` | Verdict |
|---|---|---|---|
| `cmd/agent` `TestMergerDaemon_DetachesAndSecondIsRefused` | ❌ FAIL 30.15s | ❌ FAIL 30.10s | **pre-existing** — identical name AND duration on both trees |
| `cmd/agent` `TestMergerDaemon_NoFlagStaysForeground` | ❌ FAIL 23.02s | ❌ FAIL 23.02s | **pre-existing** — identical name and duration (`merger_daemon_flag_e2e_test.go:372: foreground merger pid … did not exit on SIGTERM`) |
| `internal/shot` `TestLiveDisplayFocusChangesWithFocus` | ❌ FAIL (`GetInputFocus was answered by the wrong bytes`) | ✅ ok | **ambient, not branch-attributable** — see §7.2.1 |

#### 7.2.1 Why the `internal/shot` red is ambient (three independent proofs)

1. **The branch touches nothing in that package.** `git diff --stat 8f4042b3 30d1a6f7 --
   internal/shot/` is **empty**; the whole `8f4042b3 → 30d1a6f7` diff is 3 files, all under
   `cmd/agent/` plus the devdoc.
2. **It is green on both trees in isolation.** `go test ./internal/shot/` full package:
   merged **3/3 ok** (42.991s, then 42.483s, then one more) and control **3/3 ok** (42.578s …);
   the single-test name run: merged **5/5 ok**, control **5/5 ok**. 0 failures in 16 runs.
3. **Mechanism**: the test starts a private Xvfb plus an `xev` window, moves the focus, then
   compares our parsed focus id against an *independent* client (`xdotool`). Under load it
   reads a neighbouring top-level (`Focus() = 0x400001, … says 0x200001`) — the two ids are
   the two windows of the very test that spawned them, not a branch behaviour. Only the
   full-suite run under load raced; nothing about the branch changes X authority, window ids
   or focus handling.

Also note one invalidated pairing (kept for honesty, not as evidence): the first attempt at
the branch-side full suite ran while the merger's dead-holder sweep **removed the worktree
under the running tests**, so the test binaries' `cwd` vanished — that is what produced
`getwd: getwd: no such file or directory` and `read main.go: no such file or directory` in
`cmd/agent`. All four of those reds passed individually on a live tree (PASS 0.11s / 0.00s /
0.12s / 0.02s) and do not reappear in §7.1/§7.2, which were run on stable worktrees.

## 8. Merge-forward state (what the merger did, what this rework did)

- master advanced `e8e94ca0` → `30d1a6f7` **during** this rework, and `30d1a6f7` IS the
  merger's own merge of this branch (2nd parent `65d06842`). The merger's build gate then
  rebuilt the binary: `bin/gopi` reports `v20260913-1421-30d1a6f7`, matching the new master tip.
- This rework then ran `git merge master` inside the branch worktree: **fast-forward**,
  `HEAD = 30d1a6f7` — no new tree, nothing lost, nothing rewritten.
- `git merge-base --is-ancestor master feat/guard-test-daemon-env-isolation` → **exit 0**
  (ff-mergeable; merging is a no-op).
- The report commit on top of that is a normal, non-rewriting commit — the branch stays
  ff-mergeable because master is still an ancestor.

## 9. Deliverable declaration (Rule 6d) + close-out

0. **Final re-sync**: after this report was first written, master advanced again
   (`30d1a6f7` → `791d81cc`, the version-tag round), so the branch merged master a second time
   (`git merge master` → `5eb8524d`): a clean merge, the branch's own delta still being only
   the 4 restored files + this report. `git merge-base --is-ancestor master HEAD` → exit 0
   again, so the branch remains ff-mergeable.

1. **Branch**: `feat/guard-test-daemon-env-isolation` (tip = the commit carrying this report;
   the rework's code commit is `65d068424fe54f1cec308f378e0861ec3a70fcc7`, already on master
   via `30d1a6f7`).
2. **Recovery**:
   `git show feat/guard-test-daemon-env-isolation:devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md`
   (and `git show feat/guard-test-daemon-env-isolation:devdoc/tasks/guard-test-daemon-env-isolation.md`).
3. **In the main repo**: yes — the branch ref lives in `/home/user/work/GoPi` and master
   already carries `65d06842` through `30d1a6f7`; no push was needed (a worktree shares refs).
4. **Worktree released**: `git worktree remove /home/user/workspace/gopi-guard-rework`, then
   `bash scripts/safe-merge.sh feat/guard-test-daemon-env-isolation` → **exit 0** (all four
   guards pass; `[d]` reports the branch already merged into master).

## 10. Second forward-rework pass — re-verification at the CURRENT master

This section is the record of the re-verification pass performed after master moved on from
`30d1a6f7`. Nothing in §1–§9 was rewritten; this pass re-measures, and it repairs two
presentational defects a later docs commit (`828ba83a`) had introduced into this very file
(§5 lost its row `E`, §9 item 1 lost its list marker) — the branch's *content* was already
correct on master.

### 10.1 The moving reference — master advanced twice, and it already carries this branch

| Point in time | master | Note |
|---|---|---|
| rework started | `e8e94ca0` | §1 |
| first landing | `30d1a6f7` | the merger's own merge of this branch |
| §9 close-out | `791d81cc` | version-tag round |
| **this pass** | `cec425cc` → `48e623a2` (still moving) | `cec425cc` = merge of `feat/worker-once-purpose`, `48e623a2` = merge of `docs/worker-graceful-drain-merge-verification` |

`git merge-base --is-ancestor cec425cc master` → **exit 0**: the branch tip reached in §9 is
*already* an ancestor of current master, so the recovery work of §1–§2 is landed, not pending.

### 10.2 Gate table at the current tip

| # | Gate | Result | Evidence |
|---|---|---|---|
| A | `CGO_ENABLED=0 go build ./...` (merged tree) | ✅ OK | exit 0 |
| B | `internal/testutil` `-run TestWorkerOwnership` | ⚠️ red — **not branch-attributable** | §10.3; identical file list on the pristine control |
| C | guard set, daemon pair **PRESENT** | ✅ PASS | `cec425cc`: `ok cmd/agent 18.766s`, 8/8 PASS — `GuardTestEnvStripsDaemonHandshakeVars`, `SuiteProcessIsNotADaemonChild`, `TestAgentSpawnGuard*` (4), `...SurvivesInheritedDaemonHandshake` |
| D | guard set, daemon pair **ABSENT** | ✅ PASS | `cec425cc`: `ok cmd/agent 13.924s` |
| E | control-tree differential, both directions | ✅ class-identical (see §10.3) | merged vs pristine-master control, sorted named FAIL sets |
| F | `git merge-base --is-ancestor master <branch>` | ✅ exit 0 | after `git merge master` → `48e623a2` (§10.4) |
| G | `bash scripts/safe-merge.sh <branch>` | ✅ exit 0 | §10.5 |
| H | no force-push / no history rewrite | ✅ | `git merge master` only; every commit ordinary |
| I | Rule 8 (`git add -A` never used) | ✅ | files added one by one |

### 10.3 Control-tree differential — the residual, class by class

Two full `go test ./... -count=1 -timeout 1500s` runs, identical flags, daemon triple stripped,
**both pinned off `/`** (`GOPI_TEST_TMPDIR` — see §10.6): the merged tree `cec425cc` against a
control worktree at the same commit with the branch's four files reverted
(`git checkout cec425cc -- <paths>` keeps the files; the control DELETES them and restores
master's `agent_spawn_guard_test.go` / `testmain_test.go`).

| Residual red | merged `cec425cc` | control (branch reverted) | pristine master `903fa1bf` | Verdict |
|---|---|---|---|---|
| `TestWorkerOwnershipPointerPresent` | ❌ (2 files: `devdoc/worker-once-vs-task-run-next.md`, `devdoc/worker-status-overreport-20260913.md`) | ❌ **same 2 files** | ❌ **same 2 files** | **master-preexisting** — the branch's own devdoc carries the pointer (1 marker line), so it is not listed |
| `TestStyleGate` | ❌ 184 hits / ceiling 182 | ❌ 184 / 182 | (same gate) | **master-preexisting** — identical ceiling breach on both trees, identical in isolation |
| `TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses` | ❌ `SQLITE_BUSY` | ❌ `SQLITE_BUSY` | — | **ambient** — identical on both; the runner is a merger-daemon child |
| `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` | ❌ `SQLITE_BUSY` | ❌ `SQLITE_BUSY` | — | **ambient** — identical |
| `internal/gopilog` `TestParseHTTPRounds*` | ❌ 3 names | ❌ 4 names (**disjoint**) | — | **ambient, load-raced** — asymmetric *within* one test family, and the whole package is `ok` in isolation on **both** trees |
| `TestMergerRestart_StopReleasesTheFlockForASuccessor` | ✅ ok | ❌ | — | **ambient** — green on the merged tree, and green isolated on control too (4.68s / 3.76s) |

The named-FAIL sets are therefore *not literally equal*, and saying so would be dishonest.
What is proven is stricter in the direction that matters: **every red on the merged tree is
reproduced on the control tree**, and every red that appears on the control alone is green on
the merged tree *and* green in isolation on both — i.e. no red is attributable to the branch.
The `internal/gopilog` family is the clearest case: one run picks up
`AnnotatedMarkers`+`BaseStreaming`, the other `HTTPError`+`MarkerInsideBody`+`RequestBytes`,
both red, from the same load-sensitive streaming parser; isolated, both trees are `ok`.

`TestWorkerOwnershipPointerPresent` — the DELTA FAIL this rework exists to clear — is now
green with respect to the branch: at `cec425cc` (and at `903fa1bf`) the branch's
`devdoc/tasks/guard-test-daemon-env-isolation.md` carries the canonical pointer, and the gate's
remaining complaint names two files that arrived on master through *other* merges
(`3d6785e6` via `feat/worker-once-purpose`, `6c2ad527` via `fix/worker-status-attested`) and
that are **absent** at the pre-branch master `8f4042b3`. They are master's to fix; the epic's
own rule (delta, not "green") puts them out of scope.

### 10.4 ff-mergeability, re-established at the current master

```
git merge master            # in the branch worktree → fast-forward to 48e623a2
git merge-base --is-ancestor master feat/guard-test-daemon-env-isolation   → exit 0
git diff --stat master..feat/guard-test-daemon-env-isolation              → empty
```

The branch delta against master is **empty** — the branch is master plus the report commit
that carries this section. Landed state is therefore a no-op merge (nothing to resolve, no
second parent needed); the ephemeral merge hashes `30d1a6f7`/`5eb8524d` are the merger's own
throwaway merge commits, kept as a record of *when* the merge happened, not as an anchor
(`git cat-file -t` confirms they still exist, but they are not referenced by the branch).

### 10.5 Deliverable declaration (Rule 6d), current

1. **Branch**: `feat/guard-test-daemon-env-isolation`, tip = master `48e623a2` + this report
   commit. Code commit on master: `65d068424fe54f1cec308f378e0861ec3a70fcc7`.
2. **Recovery**: `git show feat/guard-test-daemon-env-isolation:devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md`
   (and `...:devdoc/tasks/guard-test-daemon-env-isolation.md`).
3. **In the main repo**: yes — the ref lives in `/home/user/work/GoPi`; master already carries
   all of it through `cec425cc`; no push needed (a worktree shares refs).
4. **Worktree released** after the report commit, then
   `bash scripts/safe-merge.sh feat/guard-test-daemon-env-isolation` → **exit 0** (recorded
   in the task ledger; the earlier `exit 1` was *only* the `[c]` worktree-occupancy guard and
   cleared the moment the worktree was removed).

### 10.6 One environment hazard worth recording (Rule 22, observed live)

`/dev/shm` on this host is a 7.8G tmpfs sitting at **100% full** (`6.3G/7.8G`, and the suite's
default scratch home on this host resolves onto it). A suite run against that root fails with
`no space left on device` (14 hits) and `memory schema init: begin: disk I/O error (4874)`
(13 hits) — a red that *looks* like a branch regression and is nothing of the kind. Both
differential runs were therefore pinned with `GOPI_TEST_TMPDIR` onto the (much larger,
non-`/`) home partition; the one unpinned attempt (whose 13 `disk I/O error` hits are on
record) was discarded as env-poisoned rather than reported as a FAIL. `internal/testutil`'s
scratch-root refusal (exit code 97) exists for exactly this class; the practical lesson is to
pin the suite root before starting a full run on this host.



- The **ambient** reds are not "fixed" — `TestMergerDaemon_*` (present identically on both
  trees, because the runner is a merger-daemon child) and the load-raced `internal/shot`
  focus oracle are master/ambient residue, out of scope by the epic's own rule (delta, not
  "green"). Both are green in the quiet pairing (§7.1).
- No force-push, no rebase, no history rewrite: the branch is append-only.
- `internal/shot` was not touched — its red needed attribution, not a change.

---

## 11. Third pass — the handover verification (branch tip 9b167b8c, master 48e623a2)

This pass re-measures every acceptance row against the **current** master and adds the
differential run that was still in flight when §10 was written. Nothing in §1–§10 is rewritten.

### 11.1 Reference identity (re-checked, measured)

| Role | Hash |
|---|---|
| original (reverted) merge | `d45d90e2857c4cb7941461158e5b1fb0133e0192` |
| 2nd parent of that merge = original branch tip | `9f77e133747865d5e31f29d3ffdddff14ea71713` |
| the revert | `d07e0d317fc148fc7aab5d9de8e7cea46c83e329` |
| rework code (revert-of-revert + pointer) | `65d068424fe54f1cec308f378e0861ec3a70fcc7` |
| master at handover | `48e623a2` |
| **branch tip (this report)** | `9b167b8c` |

```
$ git merge-base --is-ancestor master feat/guard-test-daemon-env-isolation ; echo $? → 0
$ git diff --stat master feat/guard-test-daemon-env-isolation
   ...-test-daemon-env-isolation-rework-2026-09-13.md | 118 ++++++++++++++++++++-
$ git diff --stat 9f77e133 HEAD -- cmd/agent/agent_spawn_guard_test.go \
        cmd/agent/agent_spawn_guard_env_isolation_test.go cmd/agent/testmain_test.go
   (empty)
$ git log --oneline master..feat/guard-test-daemon-env-isolation   → 1 commit (this report)
```

`65d06842` is already an ancestor of master (`30d1a6f7`), so the recovered fix is **landed**;
the branch ref's remaining delta is this report alone. `safe-merge.sh`'s `[d]` line ("尚未
merge 进 master") speaks about that report commit's ancestry, not about the fix.

### 11.2 Gate table, final

| # | Gate | Result | Evidence (this pass) |
|---|---|---|---|
| A | `CGO_ENABLED=0 go build ./...` | ✅ OK | exit 0 |
| B | ownership gate vs the branch | ✅ delta cleared | merged tree names only `devdoc/worker-once-vs-task-run-next.md` + `devdoc/worker-status-overreport-20260913.md`; the **same two** on the revert-state control; the branch's own devdoc is named on neither (it carries the pointer at line 3) |
| C | guard set, pair **PRESENT** | ✅ PASS 8/8 | `ok cmd/agent 14.901s`, exit 0 — `GuardTestEnvStripsDaemonHandshakeVars`, `SuiteProcessIsNotADaemonChild`, `TestAgentSpawnGuard*` ×4, `…SurvivesInheritedDaemonHandshake` |
| D | guard set, pair **ABSENT** | ✅ PASS 8/8 | `ok cmd/agent 14.630s`, exit 0 |
| E | revert-state control, pair **PRESENT** | ❌ FAIL ×3 (expected) | `AllowsOperatorStyleLaunch` / `WorkerTaskRunChainIsAllowed` / `AllowsOperatorTerminalInsideAgentSession`, exit 1 — the fix's discriminating claim |
| F | full-suite differential | ✅ no branch-attributable red | §11.3 |
| G | `git merge-base --is-ancestor master <branch>` | ✅ exit 0 | §11.1 |
| H | `bash scripts/safe-merge.sh <branch>` | ✅ exit 0 | §11.4 |
| I | no force-push / no history rewrite | ✅ | ordinary commits only; `git merge master` fast-forwards |
| J | Rule 8 (`git add -A` never used) | ✅ | files added one by one |

### 11.3 Differential run 3 — master `48e623a2` on BOTH sides

Both trees: full `go test ./... -count=1 -timeout 1800s`, daemon triple stripped,
`GOPI_TEST_TMPDIR` off `/`; merged tree `9b167b8c`, control tree `48e623a2` (pristine master).
`MERGED_EXIT=1`, `CONTROL_EXIT=1`, **25 `ok` packages each**.

| Residual red | merged | control | Attribution |
|---|---|---|---|
| `TestWorkerOwnershipPointerPresent` | ❌ same 2 files | ❌ same 2 files | master-preexisting (both docs absent at pre-branch master `8f4042b3`) |
| `TestStyleGate` | ❌ 184/182 | ❌ 184/182 | master-preexisting — identical breach |
| `TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses` | ❌ | ❌ | ambient — runner is a merger-daemon child |
| `TestTask_BarePrompt_SIGTERM_WritesSessionEnd` | ❌ | ❌ | pre-existing — identical on both |
| `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` | ❌ | ❌ | **ambient** — isolated this pass it FAILs *identically* on **both** trees: `task_lifecycle_tick_test.go:557: … database is locked (5) (SQLITE_BUSY)` (branch 11.9s / control 8.8s) |
| `internal/gopilog` `TestParseHTTPRounds*` | ❌ `BaseStreaming` | ❌ `AnnotatedMarkers` | **ambient, load-raced** — disjoint members of one family; the package is `ok` in isolation on **both** trees (branch `0.184s`, control `0.281s`) |

Sorted named-FAIL set delta: **only-merged = {`TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound`, `TestParseHTTPRoundsBaseStreaming`}**, **only-control = {`TestParseHTTPRoundsAnnotatedMarkers`}**. Not literally empty, and saying otherwise would be dishonest — but **every** member of the delta is a red that is reproducible on the *control* tree as well (row 5: same test, same assertion, same SQLITE_BUSY, same second; row 6: the other tree reddens a member of the same family because both runs share this host), and the package-level `ok` counts are equal at 25. No red is branch-attributable; the branch's own delta is test-only wiring under `cmd/agent/`.

### 11.4 Close-out

- Worktrees released: `gopi-guard-final` (branch), `gopi-guard-ctl` and `gopi-ab-revert` (controls).
- `bash scripts/safe-merge.sh feat/guard-test-daemon-env-isolation` → **exit 0** (recorded in
  the task ledger; the earlier `exit 1` was *only* the `[c]` worktree-occupancy guard).
- **Branch**: `feat/guard-test-daemon-env-isolation`, tip `9b167b8c`.
  **Recovery**: `git show feat/guard-test-daemon-env-isolation:devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md`.
  **In the main repo**: yes — the ref lives in `/home/user/work/GoPi`; no push needed
  (worktree shares refs), no force-push, no rewrite.

**Not done (honest)**: the two master-owned devdocs that keep `TestWorkerOwnershipPointerPresent`
red (`worker-once-vs-task-run-next.md`, `worker-status-overreport-20260913.md`) are *not*
touched here — they are master's own debt, absent at pre-branch master `8f4042b3`, and the
epic's rule is delta, not "green" (a drive-by fix would also collide with their own branches).

---

## 12. Fourth pass — final differential at master `48e623a2` (delta empty in BOTH directions)

Master had not moved (`48e623a2`, the tip §11 was measured against), so this pass re-ran the
paired full suite to settle the one thing §11 could not: §11's run had a *non-empty* named-FAIL
delta (three load-raced members). Run 4 was taken on a quiet host (load 6.1 vs ≈27) and the
delta is **empty in both directions** — the epic's literal acceptance row.

### 12.1 Run 4 — identical trees, identical flags

Both trees: full `go test ./...`, daemon triple stripped
(`env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u GOPI_MERGER_DAEMON_CHILD`),
`GOPI_TEST_TMPDIR` pinned off `/` (`/home/user/scratch/gopi-diff-{merged,ctl}`, both on
`/dev/sda4 /home` — Rule 22). Merged tree = worktree at branch tip `22432abe`; control tree =
worktree at pristine master `48e623a2` with the same 4 branch files absent.

| Tree | Exit | `ok` packages | FAIL packages |
|---|---|---|---|
| merged (branch tip `22432abe`) | 1 | 26 | 5 |
| control (pristine master `48e623a2`) | 1 | 26 | 5 |

```
$ comm -23 <(grep -oE '^--- FAIL: [A-Za-z0-9_/]+' merged.log | sort -u) \
           <(grep -oE '^--- FAIL: [A-Za-z0-9_/]+' ctl.log    | sort -u)   → (empty)
$ comm -13 … (control only)                                            → (empty)
```

**Sorted named-FAIL set delta = empty, both directions.** Package-level FAIL sets are equal
(`cmd/agent`, `internal/stylegate`, `internal/testutil`, `internal/tools`, `testsuite`).

Shared named FAILs (3, identical on both trees):

| Named FAIL | Class | Why it is not branch-attributable |
|---|---|---|
| `TestWorkerOwnershipPointerPresent` | master-preexisting | names **only** `devdoc/worker-once-vs-task-run-next.md` (added by `3d6785e6`, 2026-09-13 23:15) and `devdoc/worker-status-overreport-20260913.md` (added by `6c2ad527`, 23:03) — both **absent at the pre-branch master `8f4042b3` and at the original branch tip `9f77e133`** (`git cat-file -e` → 128 on both). The branch's own devdoc is *not* named. |
| `TestStyleGate` | master-preexisting | `ignored-error: 184 test hit(s), ceiling 182` + `test-sleep: 1 hit(s), ceiling 0`, byte-identical breach on both trees |
| `TestTask_BarePrompt_SIGTERM_WritesSessionEnd` | master-preexisting | identical on both trees |

Two package-level timeouts (`panic: test timed out after 10m0s`) appear on **both** trees —
`cmd/agent` (600.188s merged / 600.018s control) and `internal/tools` (600.147s / 600.071s) —
and redden *different* members of the same family on each run (merged: `TestWebChat_R2_…` /
`TestTaskRunWritebackTaggedStampsWorkerProvenance`; control: `TestVoiceSubcommandHelpSmoke` +
`TestTaskRunWritebackTagsExplicitAndEmpty`), with the default 10m per-package budget in a host
that was still draining the previous 20-minute suite runs. Same class as §11's load-raced
`internal/gopilog` pair: asymmetric *within* a family, equal package counts, no branch signal.

`TestStyleGate`'s **sub-rule asymmetry** (merged reported `test-sleep`, control did not) was run
down rather than waved off: the two trees' `cmd/agent/cli_worker_status_test.go` are
byte-identical (`diff` → identical), and in **isolation each tree reports BOTH sub-rules**
(branch 2/2 runs: `test-sleep` + `ignored-error`; control 2/2 runs: the same two). The
difference is emission order under load inside `TestStyleGate`, not a tree difference.

### 12.2 The pointer line's causal A/B (the delta fix, measured directly)

The epic's delta FAIL is about ONE file. Toggling the branch's pointer line proves the causal
link, in place, on the merged tree:

```
$ sed -i '3d' devdoc/tasks/guard-test-daemon-env-isolation.md     # drop the pointer line
$ go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1
 → FAIL, files listed: devdoc/tasks/guard-test-daemon-env-isolation.md   ← the branch's devdoc RETURNS
                       devdoc/worker-once-vs-task-run-next.md
                       devdoc/worker-status-overreport-20260913.md
$ (restore line 3)
$ go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1
 → FAIL, files listed: devdoc/worker-once-vs-task-run-next.md           ← branch's devdoc GONE
                       devdoc/worker-status-overreport-20260913.md
```

So the pointer at line 3 of `devdoc/tasks/guard-test-daemon-env-isolation.md` is exactly what
clears the file the verify child recorded, and the two remaining names are master's own debt.
`git status --short` after the toggle is **empty** (the file is restored byte-for-byte; nothing
about this was committed).

### 12.3 Gate table, fourth pass (final)

| # | Gate | Result | Evidence |
|---|---|---|---|
| A | `CGO_ENABLED=0 go build ./...` (merged tree `22432abe`) | ✅ OK | exit 0 |
| B | guard set, pair **PRESENT** | ✅ 8/8 PASS | `ok cmd/agent 77.986s` — `GuardTestEnvStripsDaemonHandshakeVars`, `SuiteProcessIsNotADaemonChild`, `TestAgentSpawnGuard*` ×4, `…SurvivesInheritedDaemonHandshake` |
| C | guard set, pair **ABSENT** | ✅ 8/8 PASS | `ok cmd/agent 16.724s` |
| D | revert-state control, pair **PRESENT** | ❌ FAIL ×3 (expected) | `AllowsOperatorStyleLaunch` / `WorkerTaskRunChainIsAllowed` / `AllowsOperatorTerminalInsideAgentSession`, exit 1 — the fix's discriminating claim, reproduced on the current master |
| E | revert-state control, pair **ABSENT** | ✅ all PASS | `ok cmd/agent 14.750s` |
| F | full-suite differential, both directions | ✅ **empty** | §12.1 |
| G | pointer causal A/B (branch devdoc) | ✅ cleared by the fix | §12.2 |
| H | `git merge-base --is-ancestor master <branch>` | ✅ exit 0 | §12.4 |
| I | branch code identity vs the original tip `9f77e133` | ✅ empty diff | `git diff --stat 9f77e133 HEAD -- cmd/agent/{agent_spawn_guard_test.go,agent_spawn_guard_env_isolation_test.go,testmain_test.go}` → empty |
| J | `bash scripts/safe-merge.sh <branch>` | ✅ exit 0 | §12.5 |
| K | no force-push / no history rewrite | ✅ | ordinary commits; `git merge master` fast-forwards |

### 12.4 ff-mergeability and the landed fix

```
$ git merge-base --is-ancestor master feat/guard-test-daemon-env-isolation ; echo $?  → 0
$ git merge-base --is-ancestor 65d06842 master ; echo $?                              → 0
$ git diff --stat master...feat/guard-test-daemon-env-isolation
   devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md | 197 ++++--
```

Master `48e623a2` is an ancestor of the branch, and the rework code commit `65d06842` is
already an ancestor of master — the recovered fix is **landed**; the branch's remaining delta
is this report. Merging the branch is therefore a fast-forward (no conflicts, no second parent).

### 12.5 Close-out

- Worktrees released: `gopi-rework-verify` (branch), `gopi-rework-ctl` and `gopi-rework-ab`
  (controls); earlier `gopi-guard-final` / `gopi-ab-revert` / `gopi-guard-rework` were released
  in §9/§11.4.
- `bash scripts/safe-merge.sh feat/guard-test-daemon-env-isolation` → **exit 0**.
- **Branch**: `feat/guard-test-daemon-env-isolation`.
  **Recovery**: `git show feat/guard-test-daemon-env-isolation:devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md`.
  **In the main repo**: yes — the ref lives in `/home/user/work/GoPi`; no push needed (a
  worktree shares refs), no force-push, no rewrite.

**Not done (honest)**: `internal/stylegate`'s two ceiling breaches (184/182 `ignored-error`,
1/0 `test-sleep`), the `cmd/agent` + `internal/tools` per-package 10-minute timeouts on the
loaded host, and the two master-owned devdocs behind `TestWorkerOwnershipPointerPresent` are
**not** touched — all three classes are byte-identical on the pristine-master control, i.e.
master-preexisting or ambient, and the epic's rule is delta, not "green".

## 13. Fifth pass — close-out verification at master `48e623a2` (fresh worktree, fresh runs)

This pass re-measures every acceptance clause from scratch in a NEW worktree
(`/home/user/workspace/gopi-guard-close`; every measurement below was taken at tip `e9970b9a`,
and commit `988773c0` afterwards only appends THIS section — a doc-only delta) against a fresh pristine-master
control (`/home/user/workspace/gopi-guard-ctl`, detached at `48e623a2`) and a fresh revert-state
control (`/home/user/workspace/gopi-guard-revstate`, `48e623a2` with `git revert -n 65d06842`).

### 13.1 The reference chain (hashes, as the epic asks for them)

| role | commit | parents / note |
|---|---|---|
| original merge (reverted) | `d45d90e2` | `571c3d30` + **`9f77e133`** (2nd parent = the branch tip at merge time) |
| the revert | `d07e0d31` | `d45d90e2` — "Revert \"Merge branch 'feat/guard-test-daemon-env-isolation'\"" |
| rework restore commit | `65d06842` | `e8e94ca0` — `git revert -n d07e0d31`, **0 conflicts** (§2) |
| branch tip | the report commits (this file) | `22432abe` is the last pre-report commit; `git log --oneline master..<branch>` lists ONLY report commits |
| master | `48e623a2` | `git merge-base --is-ancestor 65d06842 master` → **exit 0** (the fix is landed) |

`git diff --stat master feat/guard-test-daemon-env-isolation` = the report file only
(1 file, 315 insertions / 3 deletions); `git diff --stat 9f77e133 <branch> -- cmd/agent/agent_spawn_guard_test.go cmd/agent/agent_spawn_guard_env_isolation_test.go cmd/agent/testmain_test.go` = **empty**
(the fix's code is byte-identical to the original, intent preserved).

### 13.2 Gate table (fifth pass — every row measured this pass)

| # | gate | result | evidence |
|---|---|---|---|
| A | `CGO_ENABLED=0 go build ./...` + `-o bin/gopi ./cmd/agent/` on the merged tree | ✅ OK | `MERGED_BUILD_OK` (and `CTL_BUILD_OK` for the control) |
| B | guard set, daemon pair **PRESENT** (`GOPI_DAEMON_READY_FD=3 GOPI_WORKER_DAEMON_CHILD=1`) | ✅ **8/8 PASS** | `ok cmd/agent 7.857s` — `GuardTestEnvStripsDaemonHandshakeVars`, `SuiteProcessIsNotADaemonChild`, `TestAgentSpawnGuard{AllowsOperatorStyleLaunch,AllowsOperatorTerminalInsideAgentSession,HelpIsNeverRefused,OperatorStyleLaunchSurvivesInheritedDaemonHandshake,RefusesWorkerFromAgentSession,WorkerTaskRunChainIsAllowed}` |
| C | guard set, daemon triple **ABSENT** (`env -u …×3`) | ✅ **8/8 PASS** | `ok cmd/agent 7.260s` |
| D | revert-state control, pair **PRESENT** | ❌ FAIL ×3 (**expected — the fix's own claim**) | `AllowsOperatorStyleLaunch` / `WorkerTaskRunChainIsAllowed` / `AllowsOperatorTerminalInsideAgentSession`, exit 1, "inherited worker lock fd 3 is not a usable file" |
| E | revert-state control, pair **ABSENT** | ✅ all PASS | `ok cmd/agent 7.068s` |
| F | full `go test ./...` differential, both directions | ✅ **empty** | §13.3 |
| G | pointer line causal A/B on the branch's own devdoc | ✅ cleared by the fix | §13.4 |
| H | `git merge-base --is-ancestor master <branch>` | ✅ exit 0 | §13.1 |
| I | fix code identity vs original tip `9f77e133` | ✅ empty diff | §13.1 |
| J | `bash scripts/safe-merge.sh <branch>` | ✅ **exit 0** | guards a–d pass; `[d]` = not yet merged into master (the branch's 1 report commit) |
| K | no force-push / no history rewrite | ✅ | `git log --first-parent` and `git reflog show <branch>` show an ordinary append-only chain |

### 13.3 Full-suite differential (fifth pass) — merged `e9970b9a` vs pristine master `48e623a2`

Both runs: full `go test ./...`, `CGO_ENABLED=0`, the daemon triple stripped, `bin/gopi`
built in-tree first, and `GOPI_TEST_TMPDIR` pinned to a home-partition scratch
(`/home/user/scratch/rework-tmp/{merged,ctl}`) so the run does not exercise the
`/dev/shm` tmpfs (Rule 22; §10.6 records why: a full tmpfs produces non-deterministic
"database or disk is full" reds that masquerade as branch-attributable).

```
MERGED_EXIT=1   CTL_EXIT=1   ok packages: 26 / 26   FAIL packages: 5 / 5 (identical set)
FAIL pkgs both: cmd/agent (10m pkg timeout), internal/stylegate, internal/testutil,
                internal/tools (10m pkg timeout), testsuite
sorted named-FAIL delta: comm -23 (merged-only) = EMPTY ; comm -13 (control-only) = EMPTY
```

The five shared reds, each attributed:

1. `TestWorkerOwnershipPointerPresent` — **master-owned, proven directly** (§13.4).
2. `TestStyleGate` — the SAME two lines on both trees, byte-identical text:
   `ignored-error: 184 hit(s), ceiling 182` and `test-sleep: 1 hit(s), ceiling 0`.
   Pre-existing; the branch touches no test source outside the three guard files.
3. `TestTask_BarePrompt_SIGTERM_WritesSessionEnd` — identical assertion on both trees
   (`gopi exit = -1 after SIGTERM, want 1`), `testsuite` is untouched by the branch.
4. `internal/tools` — 10-minute package timeout with a **different test** in flight on each
   tree (merged: `TestMemorySearchColdDemoted`; control: `TestMemoryStoreSearchKwColumn`),
   i.e. a host-load/SQLite-contention residue, not a test identity.
5. `cmd/agent` — 10-minute package timeout with a different merger test in flight on each
   tree (`TestMergerSingleton_TwoConcurrentInvocations` vs `TestTopLevelMergerOnceRunsAPass`);
   the runner is itself a merger-daemon child, the ambient class recorded in §7.2/§10.3.

The guard tests themselves are NOT implicated: the differential FAIL sets are equal in both
directions, and rows B/C above show the guard set green on the merged tree under both env
pairings.

### 13.4 The pointer red is 100% master-owned — proven by probe, not by argument

The gate's complaint on BOTH trees names exactly two files, neither of them the branch's:

```
devdoc/worker-once-vs-task-run-next.md      (arrived on master with feat/worker-once-purpose)
devdoc/worker-status-overreport-20260913.md (arrived on master with fix/worker-status-attested)
```

Two independent probes, both run on the merged tree:

- **The branch's own devdoc is clean.** Deleting line 3 (the canonical
  `> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**` line) from
  `devdoc/tasks/guard-test-daemon-env-isolation.md` **re-adds that file** to the gate's
  complaint list; restoring the line removes it again. The delta fix is real and causal.
- **The residual disappears if — and only if — the two MASTER docs are annotated.**
  Temporarily prepending the canonical pointer to just those two files turns the whole gate
  green (`ok internal/testutil 0.040s`). Both files are byte-restored afterwards
  (`git status --porcelain` empty) — the probe changed nothing committed.

So: the branch-attributable part of this gate is fixed; what is left is a master-owned
backlog item (the two docs are absent at the pre-branch master `8f4042b3` and present,
unannotated, on pristine `48e623a2`).

### 13.5 Deliverable declaration (Rule 6d) and close-out

1. **Branch**: `feat/guard-test-daemon-env-isolation` (tip `988773c0`; measured at `e9970b9a`,
   `988773c0` adds nothing but this report section), recovery via
   `git show feat/guard-test-daemon-env-isolation:devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md`.
2. **In the main repo**: yes — the ref lives in `/home/user/work/GoPi` and master already
   carries the fix (`65d06842` is an ancestor); no push was needed (a worktree shares refs).
3. **Worktrees released** this pass: `gopi-guard-close`, `gopi-guard-ctl`,
   `gopi-guard-revstate`.
4. `bash scripts/safe-merge.sh feat/guard-test-daemon-env-isolation` → **exit 0**.
5. Merging the branch is a **fast-forward** (master is an ancestor; the branch adds one doc
   commit) — no conflicts, no second parent, no force-push, no rewrite.

**Not done (honest)**: the two master-owned devdocs stay unannotated (their owner is the
epic that produced them, and touching them would be a master-wide change outside this
branch's scope); the stylegate ceilings, the two 10-minute package timeouts and the
`testsuite` SIGTERM assertion are master-preexisting/ambient — reproduced identically on the
pristine-master control, so they are not delta.

## 14. Sixth pass — final differential on a loaded host, delta EMPTY in the branch direction

Fresh worktrees, both pinned at the SAME merge base, both built in-tree, same flags:
merged `d105e6ec` (`gopi-guard-merged`) vs pristine master `48e623a2` (`gopi-guard-ctl2`),
`env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u GOPI_MERGER_DAEMON_CHILD`,
`GOPI_TEST_TMPDIR=/home/user/scratch/gtmp` (off `/`), background jobs, full `go test ./...`.

| | merged `d105e6ec` | control master `48e623a2` |
|---|---|---|
| exit | 1 | 1 |
| `ok` packages | 26 | 27 |
| FAIL packages | cmd/agent, stylegate, testutil, tools, testsuite | cmd/agent, internal/agent, internal/shot, stylegate, testutil, tools, testsuite |
| named `--- FAIL` set | TestStyleGate, TestTask_BarePrompt_SIGTERM_WritesSessionEnd, TestWorkerOwnershipPointerPresent | same three **+** TestAgent_Run_ContextTrimDeleteSlimming, TestAgent_Run_ContextTrimReplaceSlimming, TestCaptureMatchesImportOracle, TestSuiteLedgerPin_IsActive |
| 10-min package timeout, test in flight | `TestMergerRestart_BusyIsNotInterruptedByStop` (cmd/agent), `TestSyncImportVersionlessPatchRefused` (tools) | `TestMergerRestart_StopReleasesTheFlockForASuccessor` (cmd/agent), `TestSyncImportConflictHumanWins` (tools) |

`comm -23 merged.named control.named` → **empty**: no named test fails on the branch that
does not also fail on pristine master. The three shared reds:
`TestStyleGate` (ceilings 184/182 + 1/0, byte-identical complaint), `TestWorkerOwnershipPointerPresent`
(names only the two master-owned docs — §13), `TestTask_BarePrompt_SIGTERM_WritesSessionEnd`.

The four control-only reds are **ambient**, not branch-attributable — a subset of them is by
construction impossible to be branch-caused, since the branch touches no code at all:
`git diff --name-only master...feat/guard-test-daemon-env-isolation` is the report file ONLY
(the merged tree's `cmd/agent/` content is byte-identical to master's). Isolated re-runs on
the control tree: `go test -run TestAgent_Run_ContextTrim ./internal/agent/` → `ok 0.476s`;
`go test -run TestCaptureMatchesImportOracle ./internal/shot/` → `ok 0.206s` — both flake
green when not racing the 26 concurrently-running package binaries of this suite.

The two named package timeouts are *load* artifacts with a test in flight that differs per
tree (merged: `TestMergerRestart_Busy…` + `TestSyncImportVersionlessPatchRefused`; control:
`TestMergerRestart_StopReleases…` + `TestSyncImportConflictHumanWins`) — a package timeout
whose in-flight test is a *different member of the same family* on each tree carries no
branch signal, and it reproduces on the pristine control.

**Direction note (honest)**: the delta is empty in the branch direction (merged ⊆ control).
The control-only direction is not empty here (4 extra flakes on the control run of a loaded
host) — those are the ambient class above, none of which can be caused by the branch because
the branch adds no code. A quiet-host run (§7.1) had the delta empty in BOTH directions.


**Close-out order note**: §14 is the last measured artifact of this rework (it supersedes the
"Not done" paragraph above as the final statement — that paragraph still holds verbatim, and
§14 adds the sixth-pass differential run). Tip after §14: commit
`docs(guard-test-isolation): sixth pass — final differential …`.
