<!-- doc-class: normative -->
# Task Report: daemon-cgroup-detach — the resident daemons leave the login-session cgroup (or say why not)

**Branch:** `fix/daemon-cgroup-detach` (worktree `~/workspace/gopi-daemon-cgroup`)
**Date:** 2026-09-13
**Epic:** daemon-cgroup-detach (incidents 2026-09-12 04:12 and 2026-09-13 06:40)
**Verdict:** DOABLE — implemented as a mechanism ladder with a *measured*, *announced* degradation.

---

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

## 1. The problem, restated precisely

`gopi worker --daemon` and `gopi merger --daemon` detach with fork/exec-self + `setsid`.
`setsid` makes the child a session leader with no controlling terminal — **and does not
touch its cgroup**. The daemon therefore keeps living in
`/user.slice/user-1000.slice/session-N.scope`, the login session's scope, and when systemd
tears that scope down every member receives `SIGTERM` — the daemon and its children with it.

`setsid` is a REAL protection, but a PARTIAL one, and the two must never be conflated:

| property | `setsid` gives it? | consequence |
|---|---|---|
| no controlling terminal | yes | the session teardown's SIGHUP-to-foreground-group never reaches it (Linux clears the SIGHUP disposition at `setsid`) |
| survives the session scope's teardown | **no** | a member of `session-N.scope` dies with it — the incident |

So: the daemon must leave the cgroup, and `setsid` was never going to do that. Every verdict
in this report is read from `/proc/<pid>/cgroup`, never from a mechanism's return value
(§4 explains why that distinction is the whole ballgame here).

---

## 2. What was measured on this host (kernel 5.15.0-163-generic, cgroup v2, uid 1000)

Every row below is a command that was RUN, with its observed result. Nothing here is
inferred from documentation.

| mechanism | command | result |
|---|---|---|
| declarative scope, user bus | `systemd-run --user --scope --quiet --unit=… /bin/true` | **FAIL** `Failed to connect to bus: No such file or directory` |
| — | `ls /run/user/1000/bus /run/user/1000/systemd/private` | **absent**; `/run/user/1000/systemd/` (0755 the operator) holds only `inaccessible/` |
| — | `systemctl is-active user@1000.service` / `loginctl show-user the operator` | **active** / `Linger=yes` — the manager runs, its **private socket does not exist** |
| — | same probe re-run *from inside a real ssh session* | **FAIL**, same message (the socket is not created by a new session either) |
| declarative scope, system bus | `systemd-run --scope …` (system bus) | **FAIL** polkit `Interactive authentication required` |
| — | `systemctl stop session-18982.scope` | **FAIL** polkit `Interactive authentication required` |
| — | `systemctl --user stop session-18982.scope` | **FAIL** `Failed to connect to bus` |
| cgroupfs self-migration | `mkdir /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/<dir>` | **SUCCEEDS** (delegation present: `cgroup.controllers` = `memory pids`) |
| — | `echo $$ > …/<dir>/cgroup.procs` | **EACCES** — the dir is `the operator the operator 0664` and the write still fails |
| — | `/sys/fs/cgroup/user.slice/user-1000.slice/` | `root:root 0755` — the kernel checks write permission on the **common ancestor** cgroup |

**5-why of the EACCES:** mkdir succeeded → the subtree is delegated → but a *write into
`cgroup.procs`* needs write permission on the cgroup's **parent** → the parent chain back to
the migration point runs through `user-1000.slice` (`root:root 0755`) → no unprivileged
process can migrate **itself** out of `session-N.scope`. `unshare(CLONE_NEWCGROUP)` = `EPERM`;
userns-root does not help; the session scope itself is `root:root`.

**Conclusion (unchanged from the earlier probe, now re-confirmed):** on THIS host there is no
unprivileged mechanism that moves an existing process out of the login session scope. That is
a fact about the host, not about the code.

### 2b. The trap that shapes the entire implementation

During probing, a `cgroup.procs` **write returned success while `/proc/self/cgroup` was
unchanged**. A write's return value is therefore *not* evidence of anything. Consequences
baked into the code:

- the production escape verifies by re-reading `/proc/self/cgroup` and compares it to the
  expected path — a mismatch is reported as a **failure**, never as a success;
- the acceptance test reads `/proc/<daemon pid>/cgroup` **live**, twice, and never asserts the
  mechanism (`setsid` succeeded / the wrapper ran / the argv looks right);
- `internal/cgroupscope`'s test explicitly asserts that a reported success and a reported
  failure each agree with a fresh procfs read.

---

## 3. What was implemented

### 3.1 One mechanism, in the shared seam (§Deliverable 1)

The escape lives in the shared daemonization seam so both faces get it and cannot drift:

- **`cmd/agent/cli_daemon_scope.go`** (new) — the ladder: `daemonScopeDecide()` probes the two
  mechanisms **before anything is detached**, `daemonScopeWrap()` applies the chosen one to the
  child command, `daemonScopeChildEscape()` is the child's own half (it measures its own
  `/proc/self/cgroup` and logs what it found), `daemonScopeAbandon()` is the explicit-pid
  failed-start cleanup.
- **`cmd/agent/cli_daemon_spawn.go`** (modified) — new step ③b/③c inside `daemonSpawn`, between
  the control fd and the pipe: decide → refuse-or-warn → wrap; `setsid` is applied only on the
  direct-exec paths (`systemd-run --scope` is a process in its own right, not
  session-leader-safe to `setsid` the wrapper); the chosen mechanism is printed as part of the
  start note.
- **`internal/cgroupscope/`** (new, zero-dependency leaf package) — the cgroup v2 primitives with
  every slice/slice-suffix/dir/file name as a named constant (Rules 1/1b/13/14), the
  `ErrNoEscape` sentinel, and the documented invariant that only a fresh procfs read is a verdict.
- **`cmd/agent/child_stop.go`** (modified) — added `signalChildOnly(pid, sig)`: a cgroup-scoped
  child may not be the leader of any group we own, so the failed-start cleanup signals the BARE
  pid, never a process group, never a pattern.
- **`cmd/agent/cli_worker_daemon.go`, `cmd/agent/cli_merger_daemon.go`, `cmd/agent/main.go`,
  `cmd/agent/cli_tasks_worker.go`** — the seam's `--require-scope` word is accepted by both
  scanners (one shared predicate), refused when used **without** `--daemon` (Rule 15: a silently
  dropped option is how an operator believes in a guarantee they never bought), never forwarded
  into the child argv, and both faces call the child-side escape before writing the readiness byte.

**The ladder, in order:**

| ⓪ | mechanism | when it is chosen | what the operator sees |
|---|---|---|---|
| ① | `systemd-run --user --scope --unit=gopi-daemon-<face>` | a user manager actually answers (probed with a real `systemd-run … /bin/true`, 5 s budget) | daemon in its own transient scope unit; `systemctl --user list-units` shows it |
| ② | in-process cgroupfs scope (`internal/cgroupscope`, child migrates itself) | ① fails but the delegation probe passes (mkdir + self-migration + fresh-read verification) | daemon in its own cgroupfs cgroup under the user manager |
| ③ | `setsid` only — **degraded** | neither works | parent prints `WARNING: could not leave the login session scope (<reason>); starting with setsid only — this daemon WILL die when the session scope is torn down`; the daemon's own log repeats it as `daemon: scope DEGRADED: …` |
| — | hard refusal | `--require-scope` was passed and neither ① nor ② works | non-zero exit, a message naming the flag and the reason, no orphan process |

Ordering is deliberate: the **declarative** mechanism is preferred because systemd then owns the
unit (a `systemctl --user stop` works on it, and the unit is visible). The cgroupfs path is the
zero-dependency path that works when delegation — but not the bus — is present.

### 3.2 Tests (§Deliverable 2)

**`cmd/agent/daemon_cgroup_e2e_test.go`** — the epic's acceptance proof, run against the real
binary in a private `GOPI_HOME`:

| test | asserts |
|---|---|
| `TestWorkerDaemon_LeavesTheLoginSessionScope` | the LIVE `/proc/<daemon pid>/cgroup` of a detached **worker** is not a session scope — or, when the host cannot escape, the parent announced the degradation *and* the daemon's log carries `daemon: scope DEGRADED` and the daemon honestly *is* still session-scoped (criterion D) |
| `TestMergerDaemon_LeavesTheLoginSessionScope` | the same for the **merger** face (one mechanism, two faces, no drift) |
| `TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange` | the escape did not weaken the START PROOF contract: the caller still returns within budget, the pid is alive, **PPID=1** (still a real detach), two consecutive cgroup reads agree (no race), the launcher names the mechanism it chose, the daemon's environ carries the mode stamp, and the loop banner is in the face's log |
| `TestWorkerDaemon_RequireScopeIsHardWhenUnavailable` | `--require-scope` refuses (non-zero, names the flag, prints no success line, leaves no orphan) exactly when the host cannot escape — and still starts normally when it can |
| `TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation` | `GOPI_CGROUP_ESCAPE_OFF=1` yields the *announced* degradation, never an error and never a silent success |
| `TestWorkerDaemon_RequireScopeWithoutDaemonIsRefused` | the seam-flag grammar: `--require-scope` alone is refused, naming both words |
| `TestDaemonScopeDecide_ChosenMechanismMatchesTheHost` | the decision never claims a mechanism the host cannot deliver; a degraded decision always carries its reason |
| `TestDaemonScopeWrap_OnlyWrapsTheSystemdPath` | argv rewrite is surgical: only mechanism ① rewrites; ②/③ leave the child argv byte-identical; the mode is stamped into the child env in all three |
| `TestDaemonSeamFlag_IsSharedByBothFaces` | the seam word is accepted by both scanners, forwarded by neither face's child-argv builder, and refused without `--daemon` |

**Design note — the host-dependent case is a provenance proof, not a skip-if-absent proof.**
Whether the escape succeeds depends on the host's systemd delegation, so a case that merely
skipped on an undelegated host would leave the regression this epic exists to prevent completely
unguarded, announced by nobody. Both cases therefore run in **both** worlds and assert the
*honest* outcome of whichever world they are in.

**`internal/cgroupscope/cgroupscope_test.go`** — the host-independent half: the
`InSessionScope` classifier against the exact measured incident path, `SelfRel` agreeing with a
raw procfs read, `RelOfPID` failing rather than fabricating, `ManagerRel` not classifying as a
session scope, the off-switch, and `TestEnter_VerdictComesFromProcNotFromTheWrite` /
`TestAvailable_AgreesWithEnter` (the diagnostic may never disagree with the action).

### 3.3 Explicit behavior when systemd is unavailable (§Deliverable 3)

Three properties, all test-asserted:

1. **Announced** — the parent prints the reason on stderr *and* the daemon logs its own measured
   cgroup; the reason names the failing command and its output.
2. **Not silent, not fatal by default** — a degraded start still yields a working daemon, because
   the alternative (refusing to start) would break the PA's daily workflow on a host where the
   user manager is unhealthy. The daemon is honestly labelled; the operator decides.
3. **`--require-scope` for the operator who needs the guarantee** — the escape becomes mandatory
   and its absence a visible, non-zero start failure.

---

## 4. Verification

```
Build:  CGO_ENABLED=0 go build ./...                      → BUILD-OK
Tests:  go build ./... && go test ./...                   → 2 pre-existing failures, see below
Tests:  go test ./internal/cgroupscope/ -v                → ok (8/8 PASS)
Tests:  go test ./cmd/agent/ -run 'Daemon'                → ok  github.com/the operator/gopi/cmd/agent  16.343s
Tests:  go test ./internal/standards/                     → ok  github.com/the operator/gopi/internal/standards  3.013s
Tests:  go test ./internal/stylegate/                     → FAIL (PRE-EXISTING, proven by git stash, see below)
```

### 4.1 Observed runtime evidence (the daemon's own words)

Degraded start (this host), worker face:

```
gopi worker: WARNING: could not leave the login session scope (Failed to connect to bus: No such
  file or directory; in-process escape: cgroup scope escape unavailable: … EACCES) — starting with
  setsid only — this daemon WILL die when the session scope is torn down
gopi worker: starting a detached worker daemon (idle wait 30s between rounds, self-exits after 3 idle round(s))
gopi worker: cgroup escape: setsid-only (Failed to connect to bus: No such file or directory; …)
worker daemon started: pid 2171883, log …/worker.log
```

The daemon's own log (its measurement, not the parent's prediction):

```
worker: loop start (claim → run → … )                    ← START PROOF intact
gopi worker: daemon: scope DEGRADED: still in a login session scope
  (/user.slice/user-1000.slice/session-18200.scope) — this process dies with that scope
```

And the live truth for that pid: `cat /proc/2171883/cgroup` →
`0::/user.slice/user-1000.slice/session-18200.scope` — i.e. the report and reality agree. A
*successful* escape is asserted to produce the mirror image (a non-session-scope path) by the
same two tests, which is what they will assert on a host with a working user bus.

### 4.2 Real-machine replay (§Acceptance B) — what was achieved and where it stops

Done, with the measured transcript:

1. `ssh the operator@127.0.0.1` lands in a real login session scope —
   `/proc/self/cgroup` → `0::/user.slice/user-1000.slice/session-18981.scope`.
2. A daemon started **from inside a real ssh session** →
   `/proc/2172242/cgroup` = `0::/user.slice/user-1000.slice/session-18982.scope`, `PPID=1`,
   own `SID` — the exact incident shape (reparented, terminal-less, still a scope member).
3. Closing that session was then attempted by every unprivileged route:
   - `loginctl terminate-session 18982` → session goes `State=closing`, the scope **survives**;
     the daemon was still alive with the same cgroup 30 s later;
   - `systemctl stop session-18982.scope` → polkit `Interactive authentication required`;
   - `systemctl --user stop …` → `Failed to connect to bus`;
   - writing the scope's `cgroup.kill` → `Permission denied` (that file is `root:root --w-------`).

So the **final teardown step needs root** (`sudo systemctl stop session-N.scope`, or the real
end of an ssh session driven by logind's own teardown). The replay is complete up to and
including "the daemon really is a session-scope member started from a real ssh session"; the
teardown itself is not performable by uid 1000 on this host — see Not Done.

**A finding that changes the incident's story (see §5).** The local logs show the 06:40 victim
was **not** the daemon at a scope teardown: `~/.gopi/logs/merger.log` records
`[merger] merge-verify: verify child for 01a095af-… exited signal: terminated` immediately before
`[merger] graceful stop (terminated received)` (line 7513) and
`final state: pid 889796, started 2026-09-13T03:25:34+08:00, passes 25, last pass: … idle since
2026-09-13T06:40:24` (7514). The daemon's in-flight **merge-verify child** was SIGTERMed during a
pass; the daemon itself survived and stopped later. `~/.gopi/logs/signal-provenance.log` holds 18
`kind:"signal"` records and **all** of them are `role=voice_input` — it contains **no** record for
the daemon incident, so the sender of that SIGTERM is still unproven from this file.

### 4.3 Pre-existing failures (NOT caused by this change)

Both were proven pre-existing by stashing the 6 modified + 3 new files and re-running on the clean
tree — identical failures, none in the touched paths:

| failure | clean-tree result |
|---|---|
| `internal/tools` `TestTaskStatusRunningForOwnRow` — "a row with a verified-live holder must NOT be resumable (that is the double-run guard)" | **FAILS on the clean tree** |
| `internal/version` `TestDescribeAndMintTagEndToEnd` — `MintTag = "version/20260913-083000-…"` vs want `"version/v20260913-0830-…"` | **FAILS on the clean tree** |
| `internal/stylegate` `TestStyleGate` — `internal/version/git.go:25` (ignored error-typed value `cmd.Process.Kill()`), ceiling 0 | **FAILS on the clean tree** |

Every daemon/worker/merger test in `cmd/agent/` passes, including all pre-existing regression
cases (W1–W6, the merger flock single-instance and second-instance-refused cases, the
`--daemon`/`--once` conflict check).

### 4.4 Standards gates

`go test ./internal/standards/` → **ok**. Two new gate hits were fixed rather than baselined, and
one stale permit was re-keyed (a stale permit is a silent permission):

- **Rule 16**: 8 new `%v`-wrapped-error sites in `internal/cgroupscope` → rewritten with `%w`
  (`errReadError.Error()`/`errWriteError.Error()` now concatenate rather than `%v`-format the
  wrapped error).
- **Rule 18**: 2 `else`-after-terminating-`if` sites in `daemonScopeDecide` → restructured to the
  early-return form.
- **Rule 16 baseline re-key**: `cmd/agent/main.go:860` → `:880`, the *same unchanged* site
  (its `%v` verbs format the `payloadFilePrefix` string constant, no error is wrapped there),
  shifted by the new `--require-scope` refusal inserted above it. The entry carries the reason.

---

## 5. Not Done (honest list)

- **The escape does not actually succeed on this host.** It cannot: no unprivileged mechanism
  exists here (§2). What is delivered is the mechanism that *will* escape on a host where either
  probe passes, plus an honest, announced, test-asserted degradation where it does not. This is
  the briefing's specified fallback, not a workaround.
- **Acceptance B — the end-to-end "close the ssh session" replay — is NOT complete.** Reached: a
  daemon started from a real ssh session is provably a member of `session-18982.scope` with
  `PPID=1` and its own SID (the full incident shape). Blocked at the teardown: every
  unprivileged route to destroy that scope (logind, systemd system bus, user bus, `cgroup.kill`)
  is denied to uid 1000 (§4.2). Completing it needs `sudo systemctl stop session-N.scope` from a
  privileged operator. Since the escape cannot succeed here anyway, the replay would demonstrate
  the *degradation* surviving/dying — which is what the automated tests already assert at the
  same level of rigor.
- **Mechanism ① (`systemd-run --scope --user`) could not be exercised on this host** — there is no
  user bus (`/run/user/1000/systemd/private` absent, and absent from inside an ssh session too).
  Its probe, its argv shape and its wiring are unit-covered (`TestDaemonScopeWrap_…`), but a live
  end-to-end run of mechanism ① needs a host with a healthy user manager.
- **The 06:40 incident's SIGTERM sender is unattributed.** The logs show the victim was a
  merge-verify child, not the daemon at teardown; `signal-provenance.log` has no non-`voice_input`
  record. The epic's stated trigger (session-scope teardown) is therefore **not proven** by the
  local evidence for 06:40, and this report does not claim it. (The *fix* is still correct for the
  stated failure mode, and `setsid` remains the honest guard for the SIGHUP half.)
- **Host hygiene found while working (not fixed here, out of scope):**
  `~/.gopi/logs/worker.log` is polluted with a previous agent's transcript — 991 of its lines
  contain `thinking`/`Hmm` prose, so its line numbers are not a trustworthy incident record.
  A separate session/agent also repeatedly reset `/home/user/.ssh` from `755` back to `700`,
  which is what made localhost ssh fail during the replay attempt (the key was fine).

---

## 6. Deliverable declaration (Rule 6d)

- **Branch:** `fix/daemon-cgroup-detach` (worktree `~/workspace/gopi-daemon-cgroup`)
- **Recovery:** `git show fix/daemon-cgroup-detach:devdoc/daemon-cgroup-detach-20260913.md`
- **Commit is in the shared main repo** (`~/work/GoPi`) via the linked worktree — no push needed.
- **Files:** new — `internal/cgroupscope/cgroupscope.go`, `internal/cgroupscope/cgroupscope_test.go`,
  `cmd/agent/cli_daemon_scope.go`, `cmd/agent/daemon_cgroup_e2e_test.go`; modified —
  `cmd/agent/cli_daemon_spawn.go`, `cmd/agent/child_stop.go`, `cmd/agent/cli_worker_daemon.go`,
  `cmd/agent/cli_merger_daemon.go`, `cmd/agent/cli_tasks_worker.go`, `cmd/agent/main.go`,
  `internal/standards/baseline.go`.
- **NOT merged** (per the constraint). The worktree was kept; the copy of this report lives on the
  branch as well as in the worktree.
