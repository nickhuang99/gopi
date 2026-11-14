<!-- doc-class: historical -->
# Task Report: the merger's single-instance flock — foreground owner, and the fd that let the lock escape the daemon

**Task**: `01a092ab` — "gopi merger should setup flock even it is running in front" (epic
`merger-foreground-flock`, 2026-09-12). Branch: `fix/merger-foreground-flock`
(worktree `~/workspace/gopi-mergerflock`).

## The report, answered

> "I run a gopi merger in non-daemon mode, and it sleeps when resolve a conflict, then I can run
> another gopi merger and this is wrong, because merger should prevent from running concurrent
> instance by flock locking. I suspect this is not done in foreground mode."

**The foreground face DOES take the flock, and it holds it across the conflict-resolve pass.**
Verified three ways on this host and in a `GOPI_HOME` sandbox:

| probe | result |
|---|---|
| foreground `gopi merger` (fresh binary, sandbox) | lock file names it, `/proc/locks` shows its `FLOCK` row **and** `/proc/<pid>/fd/3 -> serve.lock` |
| second `gopi merger` while it holds the lock | exit 1 — `another merge daemon is already running … refusing double-serve` |
| same, **while the holder is inside a conflict resolve** (stubbed resolver sleeping in the pass: `RESOLVER-START 07:34:31` → pass continued at 07:34:56) | second `merger`, `merger --once`, `merger --daemon` and the `task merger` alias **all refused**, naming the live holder |
| second merger 25 s later, right after the resolve finished | refused for as long as the holder lives (the lock is process-lifetime, not pass-lifetime) |

So the specific suspicion (foreground = no flock) is **false on master** — the guarantee predates this
epic. What the operator actually observed is explained in the next section: a process that *looked*
like a foreground merger and did its best work through an LLM session, while a real `--daemon`
merger held the lock.

## Root cause (5-why) — including the two traps that made the evidence misleading

**Why did two mergers appear to run at once?** The process the operator saw as their "foreground
merger" was **not** a merger:

- pid 950367, argv `gopi merger`, tty `pts/1`, session leader, started **06:49:19**;
- per `/proc/locks` it holds **no flock at all** — no fd on `~/.gopi/serve.lock` in
  `/proc/950367/fd`;
- its session dir `~/.gopi/sessions/ed7dea14-…/session.jsonl` opens with
  `{"pid": 950367, "type":"session_start", …}` and contains a full **agent** trajectory
  (169 `bash`, 20 `read`, 16 `edit`, 33 `job` calls) plus an auto-registered ledger row
  `01a092a9-3fc2` `[auto] merger [in_progress]` — the word `merger` was finally dispatched as an
  ordinary **prompt** rather than a verb.

At 06:49 the running binary was `bin/gopi` built from a tree **without** the top-level
`merger` predicate — that predicate landed at **07:07** (`393916b7`, epic
merger-daemon-flag) and the accompanying test even names the trap: *"(Direct precedent: `gopi
worker -h` used to start a REAL worker)"*. The merge daemon that legitimately held the lock the
whole time was pid 950387's successor **1070633** (`./bin/gopi task merger --every 5m`, started
07:14, `/proc/locks` owner, re-armed after each merge/reload — the "another gopi merger" the
operator could still start was that daemon plus the new session, never two *flock-holding*
mergers). The agent process held no lock **by design** — it is not a merger.

**Second trap — the lock file's pid is not evidence.** `serve.lock` said `1070633` while the
operator's pgrep pointed at `950367`; the number inside the file is only a diagnostic *stamp*, and
the authoritative holder is the kernel (`/proc/locks` row, or any pid with an fd on the file).

**A real defect did fall out of the same investigation** — on the *daemon* side, in the flock
ownership across the fork/exec:

> `--daemon` hands the flock to its child as an `ExtraFiles` entry. `os/exec` `dup2`s that entry
> into the child **without `O_CLOEXEC`**, so the child — and every process it execs — carries a copy
> of the lock fd. The first such process is the merge-resolve child (`mergerResolveConflict`), which
> by design blocks for minutes (20-minute cap).

Reproduced in a sandbox: after `kill -9` of the daemon, `/proc/<orphan resolver>/fd/3 ->
…/serve.lock` was still there, `grep flock /proc/locks` still showed the entry, and a fresh
merger was **refused with the dead daemon's pid** until the orphan exited. A flock lives on the
open file description and is released only when the **last** fd copy closes — so one killed merger
during a conflict resolve locks the service out for as long as the resolve child lingers. The
foreground face never had this problem: its fd comes from `os.OpenFile` and carries `O_CLOEXEC`
automatically — which is exactly why the symptom showed up around the daemon/`task merger` paths
and not in a pure foreground run.

## Completed

- **Forensic root cause** of the reported incident, from kernel ground truth (not from the pid
  stamped in the lock file): `/proc/locks`, `/proc/<pid>/fd`, `/proc/<pid>/environ`, the session
  transcript, and the ledger row that the offending process minted.
- **Empirical re-verification of the epic's own requirement** (foreground holder + refusal while
  resolving) in a `GOPI_HOME` sandbox with a genuinely conflicting repo and a stubbed resolver that
  sleeps inside the pass — the exact window the operator described.
- **Fix**: `syscall.CloseOnExec(fd)` the moment the daemon child re-takes the inherited flock
  (`mergerDaemonInheritLock`), so no process the daemon execs can keep the single-instance lock
  after its owner dies. Comment documents the incident, the one-line fix, and why the foreground
  face's fd is already fenced.
- **Regression E2E** `cmd/agent/merger_lock_ownership_e2e_test.go` (`TestMergerFlock_*`): real
  binary, private `GOPI_HOME`, real conflict repo, mechanical resolver stub (never a live LLM).
  - `TestMergerFlock_ForegroundHoldsItWhileWorking` — the epic's requirement, pinned: the
    foreground holder owns the lock *while resolving*, all three other faces are refused naming it,
    and its own children do **not** carry the fd.
  - `TestMergerFlock_DaemonLockStopsWithTheDaemon` — the fix, pinned: daemon holds the fd, its
    resolve child and grandchild do not, and `kill -9` of the daemon frees the lock immediately
    (the same `/proc` scan runs inside a **live refusing loop** before opening any fd, and must
    return empty — so a probe that cannot see fds fails loudly instead of passing vacuously).

## Not Done (honest boundaries)

- **The 06:49 agent process itself is not "fixed" here.** Its root cause is a build/rollout window
  (the top-level verb did not exist in the binary that ran), and the outstanding row for the
  generic "a verb the binary does not know silently becomes an LLM prompt" problem is
  `01a092ba` — deliberately **not** duplicated by this branch.
- **No change to the foreground locking path** — there was no defect there. Adding a redundant
  re-lock would have been motion, not progress; the property is now pinned by test instead.
- **`GOPI_MERGER_LOCK_FENCE_OFF=1` style kill switches were not added** (the test drives the real
  path instead, so no production knob is needed to keep the test honest).
- The `--daemon` start's readiness handshake, stdio redirect and pid restamp were reviewed but not
  modified.

## Verification

### Final re-verification at the delivered tip (branch `fix/merger-foreground-flock`, master `a0bc3e11` merged in, `git rev-list --count HEAD..master` = 0)

The commands below were re-run on the last CODE tree of this branch; later commits on the branch are
this report only (docs-only). The merge gate (build + gofmt + gate packages + merger suite) was
re-run once more at the then-tip: build OK, gofmt clean, `internal/standards` ok 7.7 s,
`internal/stylegate` ok 5.4 s, merger suite `ok github.com/nick/gopi/cmd/agent 10.462s` (both
`TestMergerFlock_*` PASS), `pgrep -af "gopi-merger merger|gopi-merger serve"` empty.

All of the below was re-run on the FINAL tree, not the earlier intermediate one:

- Build: `CGO_ENABLED=0 go build ./...` — OK (`BUILD_OK`); `gofmt -l cmd internal` — empty (`GOFMT_DONE`).
- Gate packages: `go test ./internal/standards/ ./internal/stylegate/` — `ok internal/standards 7.659s`, `ok internal/stylegate 5.432s`.
- Green at the tip (`go test -count=1 ./cmd/agent/ -run 'TestAcquireServeLock|TestMergerDaemon|TestMergerFlock|TestMergerSingleton'`):
  ```
  --- PASS: TestAcquireServeLock (0.00s)
  --- PASS: TestMergerDaemon_HelpIsZeroSideEffect (1.95s)
  --- PASS: TestMergerDaemon_DetachesAndSecondIsRefused (0.28s)
  --- PASS: TestMergerDaemon_NoFlagStaysForeground (3.00s)
  --- PASS: TestMergerDaemonFlagParsing_TranscribesLoopFlagsOnly (0.00s)
  --- PASS: TestMergerDaemonRequested_RecognizesBothFlagForms (0.00s)
  --- PASS: TestMergerDaemonChild_RequiresTheEnvMarker (0.00s)
  --- PASS: TestMergerFlock_ForegroundHoldsItWhileWorking (0.69s)
  --- PASS: TestMergerFlock_DaemonLockStopsWithTheDaemon (0.97s)
  --- PASS: TestMergerSingleton_TwoConcurrentInvocations (3.47s)
  ok  github.com/nick/gopi/cmd/agent  10.462s
  ```
  Note `TestMergerFlock_ForegroundHoldsItWhileWorking` is the epic's literal requirement and it
  passes on BOTH the pre-fix and the post-fix tree — the foreground face was already correct; the
  fencing defect was in the daemon face.
- `git status --short` in the worktree — empty (nothing uncommitted).
- **`TestMergeVerifyGatePassKeepsMerge`: pre-existing failure — RE-CONFIRMED as not caused by this change.**
  It fails IDENTICALLY on the branch tip `3faf5ba9` and on pristine master `a0bc3e11` (`--- FAIL:
  TestMergeVerifyGatePassKeepsMerge` at `merger_verify_e2e_test.go:152` on both; the harness's verify
  child records no verdict — "verify child … recorded no verdict (row now \"completed\")"). Recorded here
  because the claim is what a reviewer would otherwise distrust; it is A/B evidence, not an assurance.
- **FULL-SUITE A/B (`go test -count=1 -timeout 1500s ./...`), run side by side on the delivered tree and on pristine master:**
  ```
  branch fce13964  → /tmp/mflock-full-branch.log : 1 × "--- FAIL" (TestMergeVerifyGatePassKeepsMerge 7.03s), 19 × "ok", FAIL github.com/nick/gopi/cmd/agent 176.310s
  master a0bc3e11  → /tmp/mflock-full-master.log : 1 × "--- FAIL" (TestMergeVerifyGatePassKeepsMerge 13.65s), 19 × "ok", FAIL github.com/nick/gopi/cmd/agent 204.894s
  ```
  Same *single* failing test on both trees (all other packages `ok`) — the whole-repo suite carries no
  regression from this change beyond the known pre-existing failure. Note the two logs are not
  line-identical by design: `cmd/agent` is the only red package, and the `cmd/agent` directory is where
  this change adds a test, so a per-line diff is not the evidence — the identical FAIL set is.

### Re-run at the final DOC tip (`cbd769da`, 2026-09-12, no code changed since `13f00090`)

The last round above pinned the code tree; this one is the literal delivered tip, so a reviewer can
reproduce it verbatim:

```
== HEAD ==            cbd769da
== BUILD ==           CGO_ENABLED=0 go build ./... → BUILD_OK
== GOFMT ==           gofmt -l cmd/agent/cli_merger_daemon.go cmd/agent/merger_lock_ownership_e2e_test.go → empty
== MERGER E2E ==      --- PASS: TestMergerFlock_ForegroundHoldsItWhileWorking (12.22s)
                      --- PASS: TestMergerFlock_DaemonLockStopsWithTheDaemon (2.94s)
                      ok github.com/nick/gopi/cmd/agent  15.374s
== STANDARDS ==       ok github.com/nick/gopi/internal/standards  32.250s
== STYLEGATE ==       ok github.com/nick/gopi/internal/stylegate  15.425s
== ORPHANS ==         ps -eo pid,args | grep "gopi-merger (merger|serve)" → none
```

(`git diff --stat master..HEAD` at this tip = 3 files, +753: `cmd/agent/cli_merger_daemon.go`,
`cmd/agent/merger_lock_ownership_e2e_test.go`, this report.)

### Earlier verification rounds (kept for the red-proof history)

- Build: `CGO_ENABLED=0 go build ./...` — OK; `gofmt -l cmd internal` — empty.
- GREEN: `go test -count=1 -run TestMergerFlock ./cmd/agent/ -v`
  ```
  --- PASS: TestMergerFlock_ForegroundHoldsItWhileWorking (5.80s)
  --- PASS: TestMergerFlock_DaemonLockStopsWithTheDaemon (0.37s)
  ok  github.com/nick/gopi/cmd/agent  6.249s
  ```
  plus the neighbouring merger E2Es (`-run 'TestMergerDaemon|TestMergerSingleton|TestAcquireServeLock|TestMergerFlock'`) — `ok github.com/nick/gopi/cmd/agent 25.113s`.
- RED PROOF (the test has teeth): the same test file copied into a **detached worktree of master
  `09402611`** (pre-fix tree) — `TestMergerFlock_DaemonLockStopsWithTheDaemon` **FAILS**:
  ```
  merger_lock_ownership_e2e_test.go:433: the daemon's resolve child (pid 1223428) inherited the lock fd
  merger_lock_ownership_e2e_test.go:437: the resolve child's grandchild (pid 1223429) inherited the lock fd
  merger_lock_ownership_e2e_test.go:445: fd-ownership scan says [1223304 1223428 1223429] holds serve.lock, want exactly the daemon 1223304
  --- FAIL: TestMergerFlock_DaemonLockStopsWithTheDaemon (0.76s)
  ```
  while `TestMergerFlock_ForegroundHoldsItWhileWorking` **passes on both trees** — the epic's literal
  requirement was already met; the fd fence was the missing half.
- Full package: `go test -count=1 ./cmd/agent/` on the branch and on pristine master `09402611` show
  the **same single** pre-existing failure (`TestMergeVerifyGatePassKeepsMerge`) — no regression
  from this change. `internal/stylegate` ok. *(Earlier round, merge-base-era masters; the same A/B was
  re-confirmed at `3faf5ba9` vs `a0bc3e11` in the final round above — the master SHAs `09402611` /
  `58946e5f` below were the then-tips.)*
- **CORRECTION (re-verification, 2026-09-12 08:0x, master `58946e5f` / branch merge `cb381b5a`).** This
  section previously reported that `internal/standards` fails *identically on both trees*. It does
  not: `./internal/standards/` **PASSES on current master `58946e5f`** and FAILED on this branch only
  because the branch's merge-base (`09402611`) predated the Rule-16/Rule-20 baseline refresh
  (`fix/rule16-baseline-stale`). The failure was therefore **branch staleness, not a conflicting fix** —
  merging master into the branch resolved it, and the merged tree is green:
  ```
  ok  github.com/nick/gopi/internal/stylegate  23.684s
  ok  github.com/nick/gopi/internal/standards  14.053s
  ```
  (cmd/agent/main.go did not overlap: master's `fix/merger-top-level-verb` is the branch's own
  merge-base ancestor.) Independent re-verification of the fix itself on the merged tree:
  ```
  --- PASS: TestMergerFlock_ForegroundHoldsItWhileWorking
  --- PASS: TestMergerFlock_DaemonLockStopsWithTheDaemon
  ```
  and the RED proof re-obtained by copying THIS test file into a fresh `git worktree` of current
  master `58946e5f` and building a pre-fix binary (`CloseOnExec` count 0 in
  `cmd/agent/cli_merger_daemon.go`) — the daemon half fails exactly on the inherited-fd assertions
  while the foreground half passes:
  ```
  merger_lock_ownership_e2e_test.go:433: the daemon's resolve child (pid 1309000) inherited the lock fd
  merger_lock_ownership_e2e_test.go:437: the resolve child's grandchild (pid 1309001) inherited the lock fd
  merger_lock_ownership_e2e_test.go:445: fd-ownership scan says [1308973 1309000 1309001] holds serve.lock, want exactly the daemon 1308973
  --- FAIL: TestMergerFlock_DaemonLockStopsWithTheDaemon (1.15s)
  ```
- **Test-hygiene fix (found while re-verifying).** Two earlier runs of this very probe left orphan
  `gopi-merger merger` daemons alive on the host (found via `/proc/<pid>/environ` carrying
  `GOPI_MERGER_DAEMON_CHILD`; they held a *deleted* sandbox `serve.lock`). `testutil`'s reaper owns
  only the DIRECT child and deliberately refuses to force `Setpgid` onto a `Setsid` command, so a
  daemon's descendants survived it. The probe now registers `mlStopDaemon` in `t.Cleanup` (runs on
  the `t.Fatalf` paths too) and stops the daemon by its **process group** (`kill(-pid)`, valid
  because pgid == pid — asserted in the test), SIGKILL on purpose. The two orphans were killed
  during this session; `pgrep -af "gopi-merger merger"` is empty after a run.
- Sandbox transcript (pre-fix, hand-driven, live host, private `GOPI_HOME`):
  daemon `1198577` killed with `-9` while its resolver slept → `/proc/locks` still listed the lock,
  owner pid `1198564` (already gone), holders `1198726 /bin/sh resolver.sh` + `1198731 sleep 40`;
  `gopi merger --once` → `already running (… held by pid 1198577)` with a dead pid.
- Commit: `13f00090` — `fix(merger): fence the inherited flock fd — a daemon's children must not keep the single-instance lock`
  (re-applied on top of master during the master merge; `6d0e40cf` was the pre-merge application of the
  identical patch and is *not* reachable from the branch tip — `git show 13f00090` is the authoritative diff).

## Recovery

- Branch `fix/merger-foreground-flock` in `/home/user/work/GoPi` (shared via the linked worktree at
  `~/workspace/gopi-mergerflock`); the FIX commit is `13f00090` (the only production-file change:
  `git show fix/merger-foreground-flock -- cmd/agent/cli_merger_daemon.go`), the regression test
  arrived with `13f00090`'s sibling `3faf5ba9`'s parent chain — read the report with
  `git show fix/merger-foreground-flock:devdoc/merger-foreground-flock-20260912.md`, the fix with
  `git show fix/merger-foreground-flock -- cmd/agent/cli_merger_daemon.go`, the regression test with
  `git show fix/merger-foreground-flock -- cmd/agent/merger_lock_ownership_e2e_test.go`.
  No push is needed: the linked worktree shares the main repo's refs, so the commits are already on
  the branch there (verified at delivery: branch tip `86960674` = `git -C /home/user/work/GoPi log --oneline -1 fix/merger-foreground-flock`;
  the tip is `docs(merger-lock): full-suite A/B …` — the last CODE commit remains `13f00090`, the
  last code-touching tree is `fce13964`'s ancestors; `git rev-list --count HEAD..master` = 0).
- The worktree must be REMOVED before merging (`scripts/safe-merge.sh` refuses a branch still
  checked out in a worktree — that is its job, not an error):
  `git -C /home/user/work/GoPi worktree remove ~/workspace/gopi-mergerflock`.
