<!-- doc-class: normative -->
# Task Report: serve-lock refusal diagnosis — the stamped pid's liveness + the kernel's record for the refused inode

Branch `fix/serve-lock-refusal-diagnosis`, base `d5ba59f2` (the
`fix/cmd-agent-suite-red` commit carrying `serveLockErrnoIsContention` /
`serveLockHeldError`, cherry-picked as the epic requires — that mapping is
UNTOUCHED by this task). The worktree this was developed in
(`~/workspace/gopi-lock-diag`) was removed underneath the session by a concurrent
agent's worktree sweep; the branch lives in the main repo `~/work/GoPi`, which is
the deliverable's persistent location (`git show
fix/serve-lock-refusal-diagnosis:devdoc/serve-lock-refusal-diagnosis.md`).

## The defect

A CONTENTION refusal named the pid STAMPED into `serve.lock` without saying
whether that pid was alive, and without reporting the inode the acquirer was
actually refused on. A flock lives on the **open file description**: it survives
its owner's death (a duplicated fd inherited by a child — `os/exec ExtraFiles`,
a fork — keeps it held) and is not invalidated by unlink/replace. So the bare
stamp can name a corpse: measured in `c50e7f2a`'s E2E ("held by pid 1630650"
while that pid was gone) and in the operator's own session ("held by pid
1653050", also gone).

## Completed

1. **Liveness pairing** (`cmd/agent/cli_tasks.go`): `serveLockHolder(lock *os.File)`
   takes the descriptor the acquirer was refused on (not a path string) and pairs
   the stamp with `tools.PidAlive`:
   - **alive** → today's wording verbatim, via the shared
     `tools.LockHolderNote(true)` (empty today, so the message is byte-identical
     to the pre-change one for a live holder);
   - **dead** → `pid N` + `serveLockHolderDeadNote` =
     `" (no such process: the flock outlived its owner — an inherited descriptor
     copy, or a replaced path)"`.
   The dead half deliberately does **not** use `tools.LockHolderNote(false)`: that
   text asserts "the lock is FREE", which the refusal it is printed inside
   contradicts — the kernel just refused the flock, so a live descriptor copy is
   holding it. The stamp is dead; the holder is not necessarily gone.
   `acquireWorkerLock` (`cli_worker_control.go`) now passes the descriptor too, so
   the worker face gets the same diagnosis.

2. **Kernel-record evidence** for the CONTENTION arm: `serveLockKernelEvidence(lock)`
   prints `kernel lock table (inode <ino>): <records> | live pids holding an fd on
   that inode: <pids>`, where the records are the kernel's own `/proc/locks` FLOCK
   rows matched on the **full `<maj>:<min>:<ino>`** of the refused descriptor
   (`serveLockDeviceField`, Linux kdev_t decode; the table spells major/minor in
   hex and inode in decimal — measured, not guessed) and the holders come from a
   `/proc/<pid>/fd` stat join. Same shape as the merger-lock E2E diagnostics
   (`mlLockKernelRecords` / `mlAllFlockRecords` / `mlPidsHoldingFdOn`), lifted into
   production because the refusal IS the moment an operator needs it.
   Three distinct answers, never conflated (Rule 15): a readable table with no
   record for the inode = **a replaced lock path, not a live holder**; an
   unreadable table = **"cannot say"**, explicitly not "nobody holds it"; a scan
   that found holders but could not judge every process is marked
   `(scan incomplete)`.

3. **A production bug the E2E found and this task fixed**:
   `lockAndVerifyIdentity` closed the descriptor **before** building the refusal,
   so every real contention rendered
   `kernel lock table: cannot stat the refused descriptor — its inode is unknown`
   — the evidence was dead at the one moment it exists for. The errno mapping now
   lives in one named function (`serveLockRefusalForErrno`) so the refusal is built
   **while the descriptor is open** and the close follows. `serveLockHeldError`'s
   signature and `serveLockErrnoIsContention` are unchanged.

4. **Tests** (`cmd/agent/serve_lock_refusal_diagnosis_test.go`, new):
   - `TestServeLockRefusalPairsTheStampedPidWithItsLiveness` — a REAL held flock in
     the test process + a REAL reaped child pid. (a) dead stamp → "no such
     process", and the message must not claim the lock is free while the kernel is
     refusing it; (b) live stamp → today's refusal verbatim (the exact pre-change
     message pinned as a prefix); (c) the kernel record + `inode <ino>` + the
     `FLOCK` column + this process as an fd holder.
   - `TestServeLockKernelEvidenceReportsAnAbsentRecord` — an unheld inode is
     reported as carrying no record, labelled as the kernel's table.
   - `TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses` — the guard for
     item 3: a RE-EXEC'd participant holds the flock in its own process (reported
     over a stdout pipe), the test contends for real through
     `lockAndVerifyIdentity`, and the refusal must carry the held inode. The helper
     is released by closing its stdin — an event the test controls, no timed sleep.

5. **E2E evidence** (`.evid/serve-lock-refusal-e2e.txt`, script
   `.evid/demo-serve-lock-refusal.sh`, `bin/gopi` rebuilt before the run): a real
   `gopi merger --daemon` against a held `serve.lock` stamped with a reaped pid:

   ```
   gopi merger: daemon start failed: serve lock held by a live daemon: another merge
   daemon is already running (lock …/.gopi/serve.lock held by pid 2330654 (no such
   process: the flock outlived its owner — an inherited descriptor copy, or a
   replaced path)) — refusing double-serve: …
   kernel lock table (inode 29117447): 10: FLOCK  ADVISORY  WRITE 2330653
   103:05:29117447 0 EOF | live pids holding an fd on that inode: 2330653, 2330655 (scan incomplete)
   ```
   with the ground truth recorded alongside: the same inode, the same `/proc/locks`
   row, and the live fd holder. The kernel's record names pid 2330653 where the
   stamp named 2330654 — exactly the distinction the refusal previously could not
   make.

## Not Done (honest)

- **No new constant-bypass gate for the new literals.** Every new string is a
  named constant referenced from one place (and the audit driven by the diff —
  `git diff | grep '^+' | grep -oE '"[^"]*"'` — shows no bare literal outside the
  const block except the pre-existing `serveLockHolder` on the base commit), but
  no new mechanical check was added to `internal/standards`; the existing gates
  pass.
- **`TestMergerFlock_DaemonLockStopsWithTheDaemon`** (the c50e7f2a-era E2E that
  surfaced the original confusion) fails in this environment at
  `merger_lock_ownership_e2e_test.go:591` — *"daemon pid N is not a session
  leader"* — a cgroup/setsid premise (Rules 24/25), unrelated to the refusal text.
  It reproduces on the **base commit with my changes stashed**, together with 11
  other E2E failures (baseline below); none is touched by this change.

## Verification

- Commit: `c98bb85b` on `fix/serve-lock-refusal-diagnosis`
  (final tip; its parent `d5ba59f2` is the cherry-picked `fix/cmd-agent-suite-red`
  head. An earlier revision of this same change was `0532bb9b` — cited in an
  intermediate report — whose content differs from this commit by one line of this
  file alone, verified with `git diff 0532bb9b:devdoc/serve-lock-refusal-diagnosis.md
  c98bb85b:devdoc/serve-lock-refusal-diagnosis.md`; a concurrent agent's worktree
  sweep removed the worktree that held it, so it is reachable only from the reflog
  — cite `c98bb85b`).
- Build: `go build ./...` → OK. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (rebuilt before every E2E run).
- `gofmt -l cmd/agent/cli_tasks.go cmd/agent/cli_worker_control.go cmd/agent/serve_lock_refusal_diagnosis_test.go` → empty.
- `go vet ./cmd/agent/` → OK.
- `go test ./internal/standards/ -count=1` → `ok` (12.9s) — Rules 16–21 gates green, no new baseline entries.
- `go test ./internal/stylegate/ -count=1` → pre-existing hits only; **zero in the files this task touched** (`grep -E "serve_lock_refusal|cli_worker_control\.go|cli_tasks\.go"` over its output → empty).
- Target tests `go test ./cmd/agent/ -count=1 -run TestServeLock -v`:
  ```
  --- PASS: TestServeLockRefusalNamesTheActualErrnor (0.00s)
  --- PASS: TestServeLockContentionRefusalIsOneDefinition (0.00s)
  --- PASS: TestServeLockRefusalPairsTheStampedPidWithItsLiveness (1.76s)
  --- PASS: TestServeLockKernelEvidenceReportsAnAbsentRecord (0.05s)
  --- PASS: TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses (1.95s)
  ok  github.com/the operator/gopi/cmd/agent  5.550s
  ```
  (The three SKIPs are the re-exec helper process bodies — each names the test that drives it.)
- **Red proof 1 — liveness pairing reverted to the bare stamp**:
  ```
  --- FAIL: TestServeLockRefusalPairsTheStampedPidWithItsLiveness
      serve_lock_refusal_diagnosis_test.go:124: a dead stamped pid must be reported as no such process, got:
  FAIL github.com/the operator/gopi/cmd/agent 1.934s
  ```
- **Red proof 2 — kernel evidence removed from the contention arm**:
  ```
  --- FAIL: TestServeLockRefusalPairsTheStampedPidWithItsLiveness
      serve_lock_refusal_diagnosis_test.go:159: the contention refusal must report "kernel lock table", got:
      …: the contention refusal must report "inode 29118541", got:
      …: the contention refusal must report "FLOCK", got:
  ```
- **Red proof 3 — the close-before-refusal ordering** (the bug the E2E found):
  ```
  --- FAIL: TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses
      serve_lock_refusal_diagnosis_test.go:329: the refusal stat'd the descriptor AFTER closing it — the evidence is dead:
          kernel lock table: cannot stat the refused descriptor — its inode is unknown
  ```
- **Suite attribution (like-for-like)**: the same command
  (`go test ./cmd/agent/ -count=1`, `bin/gopi` present in both runs) fails the
  **same 12 E2E tests** on the base commit with my changes stashed
  (`/tmp/lockdiag-baseline.log`) and on this branch (`/tmp/lockdiag-branch.log`);
  a new pkg-count/mkfifo race (`TestFrontRunConsoleOutputLandsInItsSessionDir`)
  appears on both sides of the comparison across runs. No test was deleted,
  skipped or weakened: the only new SKIPs are this task's re-exec helper bodies.
- Rule 27: no load generation. The test instrument is a re-exec'd helper holding a
  real flock (released by closing its stdin — an event, not a timeout); the E2E's
  dead pid comes from a fork + `waitpid` in python. Nothing touches the block
  device.

## Independent verification of this tip (`c98bb85b`)

The worktree this change was developed in was swept away mid-session, so this tip
was re-verified from a fresh detached worktree at the commit
(`git worktree add --detach /home/user/workspace/gopi-lock-diag-verify c98bb85b`), and both
red proofs were re-run there/alongside rather than quoted:

- `CGO_ENABLED=0 go build ./...` → exit 0; `go vet ./cmd/agent/` → exit 0.
- `gofmt -l cmd/agent/cli_tasks.go cmd/agent/cli_worker_control.go cmd/agent/serve_lock_refusal_diagnosis_test.go` → empty.
- **Red proof 1 re-run — liveness pairing reverted to the bare stamp** (a separate
  worktree at the same commit, `serveLockHolder` reduced to
  `pid + tools.LockHolderNote(true)`):
  ```
  --- FAIL: TestServeLockRefusalPairsTheStampedPidWithItsLiveness (0.74s)
      serve_lock_refusal_diagnosis_test.go:124: a dead stamped pid must be reported as no such process, got:
          … (lock …/001/.gopi/serve.lock held by pid 2482389) — refusing double-serve: …
          kernel lock table (inode 29795571): 15: FLOCK  ADVISORY  WRITE 2482187 103:05:29795571 0 EOF | …
  ```
  Note what the mutant's own output shows: the kernel's record names **2482187**
  while the bare stamp named **2482389** — the corpse, printed with no liveness.
- **Red proof 2 re-run — kernel evidence dropped from the contention arm**
  (`serveLockContentionError` returning `serveLockHeldError(...)` alone):
  ```
  --- FAIL: TestServeLockRefusalPairsTheStampedPidWithItsLiveness
      serve_lock_refusal_diagnosis_test.go:161: the contention refusal must report "kernel lock table", got: …
      …: the contention refusal must report "inode 29795538", got: …
      …: the contention refusal must report "FLOCK", got: …
  ```
### Third red proof, re-run by the accepting instance (2026-09-17)

The two proofs above were re-executed a second time by the instance that accepted
this work, from `~/workspace/gopi-lock-diag-redproof` (detached at `c98bb85b`, a
SEPARATE worktree — the deliverable worktree was never edited in place, Rule 23).
The mutant this time was the dead half only, so the live half stayed intact and the
failure isolates the liveness pairing:

```
--- FAIL: TestServeLockRefusalPairsTheStampedPidWithItsLiveness (0.62s)
    serve_lock_refusal_diagnosis_test.go:124: a dead stamped pid must be reported as no such process, got:
        serve lock held by a live daemon: … (lock …/001/.gopi/serve.lock held by pid 2497124) — refusing double-serve: …
        kernel lock table (inode 30027393): 1: FLOCK  ADVISORY  WRITE 2497099 103:05:30027393 0 EOF | live pids holding an fd on that inode: 2497099 (scan incomplete)
FAIL	github.com/the operator/gopi/cmd/agent	1.192s
```

Same signature as red proof 1 and for the same reason: the stamp names **2497124**
(the reaped corpse) while the kernel's record names **2497099** (the live descriptor
holder). The full transcript is committed at
`.evid/redproof-liveness-20260917.log`; `git checkout -- cmd/agent/cli_tasks.go`
restored the worktree (`git diff --stat c98bb85b` empty) and the focused test was
green again afterwards.

- Guard semantics untouched (checked against the cherry-picked parent, not
  asserted): `git diff d5ba59f2...HEAD -- cmd/agent/cli_tasks.go` leaves
  `serveLockErrnoIsContention`'s single `errors.Is(err, syscall.EWOULDBLOCK)` line
  and `serveLockHeldError(path, holder string)`'s signature byte-identical; the
  only change on the errno path is that the mapping lives in the named
  `serveLockRefusalForErrno` (`-` the inline `if !serveLockErrnoIsContention(err)`
  at the call site, `+` the same predicate inside the extracted function).
- Constant discipline (Rules 1b/13/14): every string the diff adds is a named
  constant in the `cli_tasks.go` const block and is referenced from exactly one
  site. The eight `"<maj>:<min>:<ino>"` / `"no FLOCK record…"` / `"none"` /
  `"unknown (…NOT 'none')"` / `"FLOCK"` / `"kernel lock table"` / `"pid "` /
  `"(scan incomplete)"` shapes appear as literals ONLY in that block; the added
  `"/proc"` + `"/locks"` are joined into `serveLockProcLocksPath`, so no call site
  writes a path.
- **Suite attribution re-checked independently.** `bin/gopi` is built in BOTH
  trees (`/home/user/workspace/gopi-lock-diag-base` = `17cec86b`, `/home/user/workspace/gopi-lock-diag-verify`,
  the latter with `-o bin/gopi`). The base tree is a plain master checkout, so the
  12 failing E2E names were read out of the recorded base run
  (`/tmp/lockdiag-baseline.log`, 66540 bytes, produced with `bin/gopi` present —
  `TestWorkerDaemon_*`, `TestMergerDaemon_*`, `TestMergerSingleton_TwoConcurrentInvocations`,
  `TestMergerFlock_DaemonLockStopsWithTheDaemon`, `TestVerifyGateLandingCarriesTheDurableCloseLoop`,
  `TestCloseLoopWiringIsComplete`, `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`,
  `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis`,
  `TestFrontRunConsoleOutputLandsInItsSessionDir`) — all cgroup/setsid-scope premises
  (Rules 24/25) plus the console-session race, none of them about the refusal text.
  The like-for-like *full-suite* comparison is the one recorded in this report's
  Verification section; it is cited, not re-derived here, because re-running the
  whole `cmd/agent` suite on a loaded host is exactly the device cost Rule 27
  tells us to avoid paying twice for the same number.
- No test was deleted, skipped or weakened; the only new `t.Skip` bodies are the
  two re-exec helper process bodies, each naming the test that drives it.
- **Fourth red proof — third independent re-run, by the accepting instance of
  record** (this session, own detached worktree `/home/user/workspace/gopi-lock-diag-accept`
  at `8fa412aa`, the deliverable worktree never edited in place — Rule 23). One
  mutation, `if tools.PidAlive(pid)` → `if true` (bare stamp):
  ```
  --- FAIL: TestServeLockRefusalPairsTheStampedPidWithItsLiveness (0.78s)
      serve_lock_refusal_diagnosis_test.go:124: a dead stamped pid must be reported as no such process, got:
          … (lock …/.gopi/serve.lock held by pid 2518159) …        ← the reaped corpse (the stamp)
          kernel lock table (inode 30029897): 24: FLOCK  ADVISORY  WRITE 2518050 …  ← the live
                                                                     descriptor-copy holder
  FAIL	github.com/the operator/gopi/cmd/agent	1.303s
  ```
  Transcript committed at `.evid/redproof-liveness-accept2-20260917.log`; the
  worktree was restored with `git checkout -- cmd/agent/cli_tasks.go`.

## Rework provenance (2026-09-23) — what changed when this branch was relanded

This doc is the PREDECESSOR's account, kept verbatim above as the record of the
work (it was written before master moved). The branch could not merge; it was
relanded as `rework/serve-lock-refusal-diagnosis` onto master `953a92c2`. Three
things a reader of the sections above must know, because master's tree is no
longer the tree they describe:

1. **Master landed the same epic independently** — `serve-lock-diagnosis-liveness`,
   as `c50e7f2a` (the errno arm), `9e5e94bc` (the liveness pairing) and `b18c3eea`,
   all inside `fb877170`'s cron refactor. From `9e5e94bc` onward master's tree
   already carries `serveLockRefusalDiagnosis` (the `lock-stray-holder:` and
   `lock-kernel-records:` lines), `serveLockStrayHolderRefusal` /
   `serveLockHeldRefusal` **reworded from "daemon" to "merge process"** (the
   merger is a cron job now, not a daemon), a `serve_lock_errno_test.go` of its
   own, and a master-only `serve_lock_diagnosis_liveness_test.go`.
2. **A union of the two "serveLockHolder" implementations is not expressible in
   Go** — both sides define that name, one with a `*os.File` argument. So the
   reland took master's version as the base and PORTED the parts of this branch
   that master's variant did not cover: the liveness half of the stamp (a dead
   holder now renders `(no such process: the flock outlived its owner …)` instead
   of a bare `pid N`), the `serveLockRefusalForErrno` extraction (the errno
   decision before the descriptor is closed), the full device+inode
   (`<maj>:<min>:<ino>`) join into `/proc/locks` with the fd-holder scan
   (`serveLockFdHolders`, `serveLockPidList`), and the `no FLOCK record …` case
   that tells a replaced lock path apart from a live holder.
3. **`merger_lock_ownership_e2e_test.go` is retired, not resurrected**: master
   deleted it in `fb877170` (the daemon machinery it drove is gone). The evidence
   helpers this branch added to it (`mlLockKernelRecords` / `mlAllFlockRecords` /
   `mlPidsHoldingFdOn`'s shape) survive as production code in `cli_tasks.go`, and
   the assertions they supported are re-expressed in master's
   `serve_lock_diagnosis_liveness_test.go` plus this branch's
   `serve_lock_refusal_diagnosis_test.go`. The predecessor's own report above
   already had to watch that file fail for cgroup/setsid reasons unrelated to the
   refusal — another reason not to restore a daemon-driven rig.

The precedent's regression suite was re-run against the merged tree; the numbers
are in `tasks/<task-id>-serve-lock-refusal-rework-report.md`, not here, because
this doc is a standing claim about the CODE and that report is the run's record.
