<!-- doc-class: normative -->
# No privilege for daemons — the cgroup escape is a DEAD END, not a missing password

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Date:** 2026-09-14
**Epic:** `merger cannot escape cgroup with sudo` (task 01a09d07)
**Branch:** `fix/daemon-no-privilege-remedy` (worktree `~/workspace/gopi-nopriv-daemon`)
**Verdict:** DOABLE — and the deliverable is mostly a DELETION of a wrong idea, not a feature.

---

## 1. The report, and what it actually was

The operator ran `sudo gopi merger --daemon` and read:

```
gopi merger: cgroup escape: setsid-only (Failed to connect to bus: … ; in-process escape:
  cgroup scope escape unavailable: user manager cgroup: read …/user@0.service/cgroup.controllers: … no such file)
merger daemon started: pid 2528490, log /root/.gopi/logs/merger.log
gopi merger: note: could not leave the login session scope — this daemon runs in
  /user.slice/user-1000.slice/session-18855.scope and will end with that scope.
```

Two conclusions follow, and both are measured below:

1. the privilege did **not** obtain the escape (the note says the daemon is in the
   *unprivileged launcher's* session scope, while running as root); and
2. the missing ingredient the project started to look for — a privilege — does not exist.
   So the bug is **the search itself**, not the escape.

## 2. Measurements (uid 1000, cgroup v2, kernel 5.15; every row was RUN, nothing inferred)

| # | what was asked | command | result |
|---|---|---|---|
| 1 | is the escape available unprivileged? | `systemd-run --user --scope --quiet /bin/true` | **FAIL** `Failed to connect to bus: No such file or directory` |
| 2 | is there a user bus at all? | `ls /run/user/1000/bus /run/user/1000/systemd/` | **absent** — `/run/user/1000/systemd/` holds only `inaccessible/` |
| 3 | is the user manager's cgroup there? | `ls -d …/user@1000.service` | **yes** — but its `cgroup.controllers` is `root:root 0644` |
| 4 | can mkdir in the delegated subtree? | `mkdir …/user@1000.service/gopi-manual-probe` | **yes** (rc 0) |
| 5 | can a pid be MIGRATED into it? | `echo $pid > …/gopi-manual-probe/cgroup.procs` | **FAIL** (rc 1) — and `/proc/<pid>/cgroup` still reads `session-18855.scope` |
| 6 | why? the kernel's gate | `ls -ld /sys/fs/cgroup/user.slice/user-1000.slice` | `root:root 0755` — the kernel checks write permission on the subtree the source and the target SHARE |
| 7 | does ROOT get the escape? | `sudo gopi merger --daemon`, then `cat /proc/2528490/cgroup` | `0::/user.slice/user-1000.slice/session-18855.scope` — **the SAME cgroup as the unprivileged launcher** |
| 8 | does root even have a manager subtree to migrate into? | `ls /sys/fs/cgroup/user.slice/user-0.slice/user@0.service/cgroup.controllers` | **no such file or directory** |

**5-why #7 — why root is in the USER's session scope:** sudo changes the *uid*, and nothing
else about the process. A process **inherits its cgroup from its launcher**; the sudo'd gopi
is forked by the unprivileged shell, so it is a member of that shell's `session-N.scope`. Root
membership is not a cgroup operation at all.

**5-why #5 — why no unprivileged migration works here:** cgroup v2 gates a `cgroup.procs`
write on the **common ancestor** of the source and the target, not on the target directory
(measured: `user@1000.service/cgroup.procs` is `the operator:the operator 0644`, `mkdir` in the same subtree
succeeds, the write still returns `EACCES`). The common ancestor of
`session-18855.scope` and `user@1000.service/<dir>` is `user-1000.slice`, owned `root:root 0755`.

**So: no unprivileged mechanism exists on this host, and no privileged one substitutes for it.**
Mechanism ① needs a user bus (absent); mechanism ② needs a writable shared ancestor (root-only);
`sudo` supplies neither — it only moves the uid while leaving the cgroup exactly where it was.

## 3. What was wrong (the actual defect)

Not the escape. The project **asked for the privilege**:

- task `01a09d00` ("escape cgroup requires super user", tags=bug) was dispatched, and its
  deliverable branch `fix/daemon-scope-privilege-hint` prescribes exactly
  the dead end: `daemonScopePrivilegeHintFmt` printers "re-run it as root inside a SYSTEM scope:
  `systemd-run --scope --unit=gopi-daemon-<c> <the gopi argv>`", the worker and merger usage
  heads grew a "Privileges (which mode needs them)" section, and `70575713` added the same root
  step to the `--require-scope` refusal.
- master itself was **clean** of the hint in its first reading of this session, and already carried the correct zero-privilege posture
  (setsid-only + an announced, non-error degradation note) for BOTH faces through ONE seam
  (`daemonSpawnOrExit` → `daemonScopeMeasureNote`).

**MID-SESSION, MASTER MOVED UNDER THIS BRANCH.** While this branch was being verified, the merger
(pid 2575613, sweeping on its own clock) **landed the offending branch**: `f4f761c5` = "adoption:
merge fix/daemon-scope-privilege-hint" became master's tip. The fork point this branch was
verified against (`0d2c5657` = master~1) was therefore no longer master, and "master is clean of
the hint" stopped being true at 08:33. Two consequences, both handled here (merge `b05597f8`):

- the correction is no longer a pre-emptive deletion of an unmerged branch — it is now the
  **revert of text that is in master**, and its e2e-visible surface (a second post-start line) is
  exercised against master's own bundle;
- master's arrival half `cmd/agent/daemon_scope_privilege_test.go` was **DELETED**, not merged:
  it asserts the prescription arrives (`daemonScopePrivilegeHintFor` names "root" and `--scope`,
  the section reads "Privileges (which mode needs them)") — the exact text
  `daemon_scope_no_privilege_test.go` refuses. Its `--once`-needs-no-privilege assertions were
  folded into the gated usage sections instead. Keeping both files is impossible by construction
  (one requires what the other forbids), and that impossibility IS the merge conflict's meaning —
  it must be decided by measurement, not by a textual resolution.

Left alone, that branch would have taught every future reader (and agent) that gopi's daemons
need root — a remedy that costs the whole process tree to root and buys nothing.

## 4. What this change does

Deliberately small, and it is a *correction*, not a new mechanism:

1. **`cmd/agent/cli_daemon_scope.go`** — `daemonScopeNoPrivilegeNote`: the measured answer to
   the question the misleading line raises ("…so I need a password?"), printed BESIDE the note
   (never inside it: the note stays the one-line measurement the previous epic pinned).
   Rendering moved to `daemonScopeMeasureLines`, the ONE place that decides what an operator
   reads about a started daemon's cgroup.
2. **`cmd/agent/cli_daemon_spawn.go`** — the same correction is printed on the
   `--require-scope` refusal, which is where the operator most wants a remedy; it names
   `--require-scope` as the honest lever instead.
3. **`cmd/agent/main.go`** — a "Privileges (--daemon)" section in BOTH usage heads (worker and
   merger): a daemon needs NO privilege; running it as root does not obtain the escape either;
   the escape's absence is announced and `--require-scope` is the hard guarantee; a host's own
   service unit is the operator's lever for a start that must outlive the login session.
   Nothing prescribes a password.
4. **`cmd/agent/daemon_scope_no_privilege_test.go`** — the regression gate (below).
5. **`git apply 6cafd57f`** (salvaged from the offending branch) — the e2e parsers
   (`wdParseStart` / `mdParseDaemonStart`) must slice the SUCCESS LINE before tokenizing,
   because the parent prints post-start cgroup notes AFTER it. This was already broken on
   master ("scope." was read as the log path); the correction adds a second such line, so the
   salvage is **load-bearing** here, and its root cause is proven by the run below.

### 4.1 The regression gate, and why it is a SOURCE gate

The defect is an operator-facing STRING, and a deleted string comes back the next time an agent
reads the same log line and reaches the same wrong conclusion. `daemon_scope_no_privilege_test.go`:

- **red proof** — the predicate must FLAG the text that actually shipped (all six fragments,
  including the bare `sudo` spelling); a green sweep proves nothing otherwise;
- **exhaustive sweep** — every production `.go` file of the package is parsed and its string
  LITERALS searched for the prescription shape; the scan asserts a floor on how many files it
  read, so a broken glob cannot pass vacuously;
- **presence half** — the correction MUST be emitted beside a degraded note (both lines), and
  MUST NOT be emitted for an escaped daemon;
- **both faces** — both usage heads carry "needs NO privilege", name no prescription, and name
  `--require-scope`.

Literals, not raw bytes: the criterion is what gopi PRINTS. A comment recording the history
("the operator asked for passwordless sudo, which does not work") is documentation, and a byte
scan would force the explanation out of the code that needs it.

### 4.2 Dead end — explicitly recorded

Privilege is NOT the solution and must not become one:

- `sudo gopi worker --daemon` / `sudo gopi merger --daemon` → measured in the LAUNCHER's session
  scope (no effect on the cgroup, everything moved to root);
- a root **system** scope (`systemd-run --scope`, no `--user`) does escape — but it is a
  privileged, host-level, operator-owned act outside gopi, and adopting it as gopi's remedy makes
  every daemon a root process for a property toggled at boot;
- therefore: gopi does not ask for it, and the gate forbids asking.

The zero-privilege answer is: start the daemon as the ordinary user; if the escape is
unavailable, the start SAYS SO (never silently), `setsid` already removes the
controlling-terminal exposure, and the residue is session-scope teardown membership — for which
the operator's lever is the host's service unit (`--require-scope` when the guarantee must be
hard), not a password.

## 5. Verification

- **Build:** `go build ./...` → OK.
- **Gate red proof:** re-inserting "re-run it as root inside a SYSTEM scope" **FAILS** the sweep
  (literal named); re-inserting the bare `sudo` spelling **FAILS** too; removing it passes again.
- **Rule 11 — real unprivileged adoption (both faces, one isolated `GOPI_HOME`):**
  `gopi worker --daemon` → rc 0, pid 2738339; `gopi merger --daemon` → rc 0, pid 2738385. Both
  printed the measured note AND the correction:
  `gopi merger: that is not a privilege problem: no privilege is needed to run this daemon, and
  none obtains the escape (measured) — the start stays unprivileged.`
- **Measurement (Rule 24):** `/proc/2738339/cgroup` and `/proc/2738385/cgroup` both
  `0::/user.slice/user-1000.slice/session-18855.scope` — the daemons run, unprivileged, exactly
  as designed; the note and reality agree.
- **Salvage root-cause proof:** `TestMergerDaemon_DetachesAndSecondIsRefused` and
  `TestWorkerDaemon_DetachesAndKeepsRunning` PASS with the salvage and **FAIL 54s** without it
  ("merger daemon log unprivileged. never showed a pass" — the parse read the next sentence's
  first word as the log path). The bug is real, and the fix is the salvaged one.
- Tests: `go test ./cmd/agent/ ./internal/cgroupscope/ ./internal/standards/ ./internal/stylegate/`.

### 5.1 Whole-tree triage at HEAD (2026-09-14, inside the peak window)

`go test ./...` in the scratch root went red; EVERY failure was triaged to a pre-existing,
master-reproducible cause, none to this branch:

- **Peak-hour cost guard (environmental, dominant).** The guard refuses to start the binary
  inside DeepSeek's peak window (09:00-12:00), so `gopi version`, `gopi worker --daemon`,
  `gopi task submit` and `gopi run …` exit 4 at 09:0x. Two measured sub-cases:
  (a) env-inheriting probes DO recover — six of them re-run with `GOPI_PEAK_HOURS=00:00-01:00`
  → ok 8.718s (same tree, same binary); (b) env-scrubbing probes do NOT — `cliContractEnv`
  drops every `GOPI_*` var from the child, so `TestCLIContractUnknownVerbIsFastUsageError`,
  `TestTaskRunSingleRowE2E`, `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`
  and their kin fail regardless of the documented override. Reproduced IDENTICALLY at master tip
  `f4f761c5` in a clean detached worktree. Filed as bug task
  `01a09d73-709a-704b-b5b6-a94df2ab7e5c`.
- **This branch's OWN gates PASS at HEAD:** `TestWorkerDaemon_LeavesTheLoginSessionScope` and
  `TestMergerDaemon_LeavesTheLoginSessionScope` (live-cgroup detach), plus all four
  `TestDaemonScopeNoPrivilege_*` cases (`PrescriptionsAreFlagged`,
  `NoProductionSourcePrescribesOne`, `TheCorrectionIsEmitted`,
  `BothFacesTellTheOperatorTheSameAnswer`).
- **`internal/testutil` (2 scratch-pin gates + 1 ownership-pointer gate).** Reproduced at master
  tip `f4f761c5` in a clean detached worktree (`git worktree add --detach … master`):
  `internal/cgroupscope` has no `TestMain` pin, and
  `devdoc/worker-daemon-acceptance/closing-run-1806.txt` (master commit `42d43f2f`) teaches a
  worker-launch transcript without the pointer. Neither path appears in
  `git diff --name-only master..HEAD`. Filed as bug task
  `01a09d73-74d4-7eb8-9dce-a11ea762029b`.
- `internal/standards` → ok; `internal/cgroupscope` → ok; every other package → ok.
  This branch's OWN gates are green at HEAD.

## 6. Not done / handed over

- **`fix/daemon-scope-privilege-hint` is not cleaned up here** (archive tag + branch deletion +
  worktree removal are operator-only per `devdoc/worker-ownership.md`). Its commits:
  **salvaged** `6cafd57f` (the e2e parse; applied here), **superseded** `41c90940` /
  `70575713` / `bab95419` (the privilege prescription), `c124b636` (a docs pointer).
  **The branch was archived-eligible by landing, not by luck:** the merger landed it as
  master's `f4f761c5` at 08:31 BEFORE this branch could be reviewed, so the prescription is in
  master and this branch reverts it (merge `b05597f8`). Archiving the branch is now a cleanup
  of history only — master no longer teaches the privilege (that is what this branch removes),
  and `cmd/agent/daemon_scope_privilege_test.go` (the arrival test asserting the prescription)
  is DELETED here, so the two cannot coexist: one requires exactly what the other forbids.
- **The merger cannot be tested on this host as an escaping daemon** — the escape is unavailable
  here by construction (§2). The gate and the wording are what can be verified, and they are.
- A **root-owned, stray merger daemon (pid 2528490, `--every 5m`, started by the operator under
  sudo from `~/work/GoPi`)** is still running with a `/root/.gopi` ledger. It is the artifact of
  the dead-end attempt; killing it is operator-only (reported, not improvised).

## 7. Deliverable declaration (Rule 6d)

- **Branch:** `fix/daemon-no-privilege-remedy` — **HEAD `fba5c87a`**, with master's tip
  `f4f761c5` merged in as an ancestor (merge `b05597f8`). Branch ref lives in `~/work/GoPi`;
  no worktree, checkout, or push is required to read it.
- **The worktree the branch was created in (`~/workspace/gopi-nopriv-daemon`) was deleted twice
  by an external reaper while the suite was running in it** (task 01a09d67); `.git/worktrees/`
  entry gone with it, the branch ref and all commits intact. Verification continued in the
  declared scratch root **`/dev/shm/gopi-test-scratch/nopriv-verify`** (same branch, same
  commits) — off the `/` partition per Rule 22.
- **Recovery:** `git show fix/daemon-no-privilege-remedy:devdoc/no-privilege-daemon.md`
  (report: `git show fix/daemon-no-privilege-remedy:tasks/...` — session report path is
  `$GOPI_TASK_REPORT_PATH`).
- **Commit is in the shared main repo** (`~/work/GoPi`) via the linked-worktree refs — the commit
  is on the branch there; no push performed and none needed.
- **Files:** new — `cmd/agent/daemon_scope_no_privilege_test.go`, this devdoc, the `tasks/` report;
  modified — `cmd/agent/cli_daemon_scope.go`, `cmd/agent/cli_daemon_spawn.go`, `cmd/agent/main.go`,
  `cmd/agent/merger_daemon_flag_e2e_test.go`, `cmd/agent/worker_daemon_flag_e2e_test.go`,
  `internal/standards/baseline.go` (permit re-keys `0ae9a1d1`, `ed67a2b5`), `GOPI.md` (Rule 25),
  `devdoc/daemon-scope-privilege-hint-20260914.md` + `tasks/daemon-scope-privilege-hint-report.md`
  (SUPERSEDED headers); **deleted** — `cmd/agent/daemon_scope_privilege_test.go` (master's arrival
  test; it asserts the hint names `root`/`--scope`, the exact text the gate forbids).
