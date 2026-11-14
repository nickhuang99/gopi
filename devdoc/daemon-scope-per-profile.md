<!-- doc-class: normative -->
# Devdoc: one daemon per PROFILE — the scope unit name as the profile's identity
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: `daemon-scope-per-profile` (2026-09-16). Branch: `daemon-scope-unit-per-profile`.

## The defect, stated precisely

Every `gopi worker --daemon` / `gopi merger --daemon` start runs the child inside a
TRANSIENT systemd scope: `systemd-run --user --scope --unit=<name> …`. **The name was
not a function of the profile the daemon serves**, so two profiles on one host could
ask systemd for the same unit — and a long-lived production daemon could occupy the
name a test profile's launcher asked for.

Two spellings were measured wrong before this change:

| spelling | what it got right | what it got wrong (measured) |
|---|---|---|
| `gopi-daemon-<face>` | one name per face | systemd is **not** idempotent about a taken name: while the unit is alive, `systemd-run --user --scope --unit=<taken>` fails with `Failed to start transient scope unit: Unit <taken>.scope already exists.` A resident `gopi merger --daemon` therefore killed every test-started merger (two 10-minute suite timeouts, `panic: test timed out`). |
| `gopi-daemon-<face>-<pid>` (bec6301b) | no collision with a resident daemon | the pid is the **launcher's**, and `go test` runs every case of one package inside ONE process — all of that package's daemons shared a name. An operator also cannot NAME the unit: it changed on every start. |

## What systemd actually does (measured, 2026-09-16, this host, uid 1000)

| question | command | measured answer |
|---|---|---|
| is a name taken while the unit lives? | `systemd-run --user --scope --quiet --unit=$N /bin/sleep 3` then re-run with `/bin/true` | `Failed to start transient scope unit: Unit $N.scope already exists.` — repeated on **every** attempt for the unit's whole life (~2.9s of 50ms polls) |
| when is it released? | same, until the sleep exits | first attempt after the process exit succeeds; `systemctl --user show -p LoadState --value $N.scope` flips `loaded` → `not-found`. Startup-to-release 2.81s for a 3s scope ⇒ **release within ~10–20ms** of the process exit |
| does a dead unit linger? | `systemd-run --user --scope --quiet --unit=$V /bin/true` | `LoadState=not-found`, `ActiveState=inactive` — **nothing** is left loaded, `--collect` is unnecessary |
| cost of the availability question | `systemd-run --user --scope --quiet /bin/true` | **7.6ms** (n=3, 7.53/7.58/7.78ms) |
| does `list-unit-files` answer "is this name free?" | `systemctl --user list-unit-files $U.scope` | **No** — it prints a HEADER plus `0 unit files listed`, so a line-count test reads a MISSING unit as taken (this exact mistake refused every start in the first iteration of this branch) |
| does `show -p X <unit>` answer it? | `systemctl --user show -p X $U.scope` (no `--property`) | **No** — `X` is parsed as a UNIT NAME: the real unit's full property dump comes back |
| the correct question | `systemctl --user show --property=LoadState --value $U.scope` | `not-found` when systemd does not know the name, `loaded` while the unit lives (exit 0 either way) |

### Other measurements taken while diagnosing

* `systemd-run --user --scope /bin/sleep 30 >/dev/null` → the scope was measured DEAD
  within 0.2s (`LoadState=not-found`) while the sleep had been alive for a second
  earlier — connection to the user bus can fail EAGERLY, and an exited unit releases
  its name at once. (Prompted a `journalctl --user-unit` follow-up; not load-bearing
  for this change.)
* `systemctl --user list-units --type=scope` on this host also lists the operator's
  long-lived `gopi-daemon-merger.scope` (the pre-existing, pre-rename unit of a
  resident merger) — the reason the acceptance run asserts on the NEW names.

## The fix

### 1. One identity: the PROFILE's data root (`cmd/agent/daemon_scope_profile.go`, new)

```
profile identity  = canonical($GOPI_HOME/.gopi | ~/.gopi)     ← config.GopiDir + EvalSymlinks on the deepest existing ancestor
profile tag       = <readable tail>-<first 6 bytes of sha256(identity), hex>
unit name         = gopi-daemon-<component>-<profile tag>     ("gopi-daemon-worker-prod-a-cd332614dd36")
```

* The identity is **hashed**, not spelled: the readable tail alone is not unique (two
  profiles can both be named `gopi` — pinned by a test), and a raw path carries
  characters systemd refuses in a unit name.
* Canonicalisation tolerates a not-yet-existing profile directory (`EvalSymlinks`
  on the whole path would simply fail at start time) — two SPELLINGS of one profile
  must not become two units.
* One definition each (`scopeUnitNameFor`, `scopeDirName = scopeUnitName`), so the
  unit name and the cgroupfs scope directory can never drift (Rules 13/14).
* An UNRESOLVABLE profile (no HOME) yields the explicit `nohome-<pid>` mark rather
  than an invented identity — a name that two profiles could share is the defect.

### 2. The existing-unit case is DECIDED, not discovered (`daemonScopeResolveExistingUnit`)

Run in the parent, BEFORE `child.Start()`, only when the child would be wrapped:

| systemd says | decision |
|---|---|
| `LoadState=not-found` | start normally |
| the name is taken AND the holder's `/proc/<pid>/environ` carries THIS profile's identity (`GOPI_DAEMON_PROFILE`, stamped by every wrap) | **reuse**: this profile's daemon already holds the unit, so the requested end state holds — reported in one line, nothing is detached, and the start is not a failure |
| the name is taken by anything else | **refuse**, naming the unit, the profile and why it is not retryable (Rule 15) |
| the question cannot be asked | **refuse** with the question's own failure — never "assume free" |

The holder is identified by the daemon's OWN statement of its profile
(`GOPI_DAEMON_PROFILE`), not by reconstructing a path from the holder's `GOPI_HOME`:
the comparison is exact string equality, and it works for the DEFAULT profile, whose
processes have no `GOPI_HOME` at all. An unreadable holder environment is NOT a
"yes" — claiming a foreign daemon as one's own would silently turn a start into a
no-op.

#### Three silent defects in that decision (found after the first commit, measured, fixed)

The decision above was committed in `217762af` and **did not work** — it was inert
in the reuse direction and never reached in the taken-name direction. Each defect
was found by making its test a REAL one instead of a skipping one (commit
`9851dc3e`), which is the general lesson: a test that SKIPS cannot discriminate.

| # | defect (measured 2026-09-16, systemd 249, user bus) | how it stayed invisible | fix |
|---|---|---|---|
| 1 | `systemctl show --property=LoadState --value <unit>` was run **without `--user`** at all three call sites — it asked the SYSTEM manager, which answers `not-found` for **every transient USER scope, a live one included**. | `not-found` reads as the perfectly ordinary "the name is free", so the start simply proceeded and discovered the taken name only inside `systemd-run`; the REUSE half could never fire. | the bus is named through the SAME constant the mechanism uses (`systemdRunFlagUser`); a test asserts the user and system buses are distinguishable. |
| 2 | the holder of a transient SCOPE is **not** reachable via `--property=MainPID` — measured EMPTY for a live scope (`--property=PIDs` empty too). | `pid <= 0` read as "not ours", so `daemonScopeSameProfileDaemon` was dead code answering only ever "no". | the holder is read from the unit's CGROUP (`--property=ControlGroup` → `<cgroup>/cgroup.procs`, measured 18ms — the very unit whose cgroup held the pid answered empty to MainPID). `MainPID` stays as DIAGNOSTIC text only (`daemonScopeUnitMainPIDText`, printed by `--status`). |
| 3 | `systemd-run --scope` **stays in the FOREGROUND** for the child's whole lifetime and exits with the CHILD's status. | the two measured tests wrapped it in a `10s` context around a `60s` holder, so the creation was killed (`signal: killed`) and the tests **SKIPPED** — both `t.Skip`-shaped failures looked like "the host cannot do this". | `daemonProbeStartScope` (disowned launcher, `Setsid`, its own log) + `daemonProbeAwaitUnit` (fails with systemd's own words, **never** skips). Both tests now PASS in 0.04s where they used to SKIP after 10s. |

Defect 1 is the sharpest instance of this repo's Rule 15: an unanswerable question
whose answer happened to be spelled like the expected one. The system manager did
answer — with the truth about a namespace that does not contain the unit.

`daemonSpawn` now returns `(reused bool, err error)`: a no-op start must not print
the start-failure line, and the worker's `--restart` must not treat it as a failure
(the restart still records a state file for the daemon it did not start).

### 3. The false idempotence claim is corrected

`cli_daemon_scope.go`'s constant block now carries the MEASURED truth in place of
"systemd is idempotent about an existing unit name": the name is a handle, taken
while the unit lives, released ~10–20ms after its process exits, plus the two
earlier spellings and why each failed. The stopgap's test header was rewritten the
same way.

## Files

| file | change |
|---|---|
| `cmd/agent/daemon_scope_profile.go` | NEW — the profile identity, the tag, the name sanitiser, canonicalisation |
| `cmd/agent/cli_daemon_scope.go` | corrected constant block; `scopeUnitName`/`scopeUnitNameFor`/`scopeDirName` now profile-derived; NEW `daemonScopeUnitExists` / `daemonScopeUnitLoadState` / `daemonScopeSameProfileDaemon` / `daemonScopeUnitMainPID` / `daemonProcessEnvVar` / `daemonScopeResolveExistingUnit` / `daemonScopeReuseReport`; wraps stamp `GOPI_DAEMON_PROFILE` |
| `cmd/agent/cli_daemon_spawn.go` | `daemonSpawn` returns the reuse outcome; step ③d calls the decision before `child.Start()` |
| `cmd/agent/cli_worker_control.go`, `cmd/agent/cli_worker_daemon.go` | the reuse outcome travels to `--restart` |
| `cmd/agent/daemon_userbus_probe.go` | systemctl/unit-name/procfs constants one place (`--property`, `--value`, `.scope`, `not-found`, `/proc/%d/environ`) |
| `cmd/agent/daemon_scope_unit_test.go` | per-profile uniqueness, no-launcher-pid red proof, tag/canonicalisation/identity contracts, and the two MEASURED decision tests (real systemd) |
| `cmd/agent/daemon_cgroup_e2e_test.go` | merge damage fixed: the verbatim-argv assertion compared against the stable argv[0] instead of the real executable, and had been red since cb536ada |
| `cmd/agent/cli_tasks_rebuild_pass_test.go` | merge damage fixed: `funcBody` collided with `merger_resume_arm_wiring_test.go`'s helper → renamed to `braceFuncBody` (master's `cmd/agent` TEST package did not compile, which made all four target tests unobservable) |
| `scripts/accept-daemon-scope.sh` | the acceptance rig (two production profiles + the four tests in one window) |

## Acceptance evidence

`bash scripts/accept-daemon-scope.sh` (branch binary `bin/gopi`, scratch under
`/dev/shm` per Rule 22) — ONE window, run 2026-09-16 05:43, output verbatim in the
task report:

```
=== [1] two profiles, two daemons ===
[prod-a] worker daemon started: pid 2330009, log /dev/shm/gopi-daemon-accept-2329992/prod-a/.gopi/logs/worker.log
[prod-b] worker daemon started: pid 2330032, log /dev/shm/gopi-daemon-accept-2329992/prod-b/.gopi/logs/worker.log
--- gopi scope units while BOTH production daemons run:
  gopi-daemon-merger.scope                     loaded active running /home/user/work/GoPi/bin/gopi merger
  gopi-daemon-worker-prod-a-510eca152b43.scope loaded active running …/bin/gopi worker
  gopi-daemon-worker-prod-b-c93bd54a8cde.scope loaded active running …/bin/gopi worker

=== [2] a second --daemon for the SAME profile is still refused ===
gopi worker: daemon start failed: another worker daemon is already running (lock …/prod-a/.gopi/worker.lock
held by pid 2330009) — refusing a second resident worker: …      [exit 1]

=== [3] the four worker-daemon tests, WITH the production daemons up ===
ok  github.com/the operator/gopi/cmd/agent  10.344s
--- units immediately after the suite: (the same three units, still running)

=== [4] cleanup: both production daemons are stopped (no residue) ===
production daemons still alive after cleanup: 0 (want 0)
both rig units are gone — the rig leaves no resident daemon
```

Two distinct `gopi-daemon-worker-<profile>.scope` units, one per profile, both alive
while the four named tests pass — the property the pid suffix could not deliver,
since a test package's daemons shared one launcher pid.

### What this evidence DOES and DOES NOT discriminate (measured, not assumed)

* **The positive property is fully evidenced**: two profiles → two units
  simultaneously, and the four tests green inside that same window.
* **The pid-suffix attribution alone is NOT discriminated by that window**: the 4
  tests were ALSO run on PRISTINE master (`/home/user/workspace/gopi-master-baseline`,
  `664b3784`, binary rebuilt) with the two production branch daemons of the SAME
  face alive — they **passed there too** (`ok … 9.885s`). So on this host, the
  4 tests were not currently blocked by unit-name collision; what blocked them was
  the **non-compiling test package** (`funcBody` redeclared), which is fixed here.
  The collision mechanism stays evidenced the other way and is pinned by test:
  `TestScopeUnitNameCarriesNoLauncherPid` fails on the pid-suffix spelling, and
  bec6301b's own commit message records the two 10-minute `panic: test timed out`
  suites the name collision produced.
* The honest summary: **the four tests are unblocked (compile fix) and green while
  a same-face production daemon runs; the per-profile property is pinned by unit
  test and demonstrated live by the two-unit listing** — not by a red→green
  transition of the 4 tests on this host.

### A wrong first attribution, and the REAL cause (measured)

The rig's first run (`05:37`) left two daemons (pids 2220937/2220960) ALIVE after
`cleanup`, and I first attributed it to `kill -TERM` not stopping a detached
daemon. Two follow-ups were needed to get it right:

1. A probe with a SHORT idle interval (`GOPI_WORKER_IDLE_SEC=5`) showed SIGTERM
   stopping the daemon within 1s, and a second probe showed the same for
   `worker --stop` — so the signal is not generally broken.
2. A probe with the RIG's OWN shape (`GOPI_WORKER_IDLE_SEC=3600
   GOPI_WORKER_IDLE_EXITS=0`) reproduced it exactly: the daemon log's last line is
   `worker: queue drained — waiting 1h0m0s (new tasks picked up next tick; kill to
   stop)` and it survived **SIGTERM (20s), SIGUSR1, and `worker --stop --wait 20s`**
   — SIGKILL was the only thing that stopped it.

ROOT CAUSE, in master's worker loop (NOT in this epic's files — filed as task
`01a0a715-1ae0-74ec-aa57-ae7593173d1e`): with `idleExits == 0` the drained wait is
a raw `time.Sleep(workerIdleInterval())` (`cmd/agent/cli_tasks_worker.go`), the only
stop-aware wait (`waitWorkerIdleOrStop`) is used by the OTHER branch, and every stop
signal only sets an in-memory flag — so a daemon whose interval exceeds an
operator's patience is unstoppable except by SIGKILL, and `--stop` reaches it only at
the next claim boundary. The log line's tail ("kill to stop") is false for that
shape.

CONSEQUENCE FOR THIS RIG (fixed here, since a rig that leaves resident daemons on the
host is the very thing this epic must not do): the production daemons now use a 5s
idle interval (still never self-exiting — resident for the whole window, which is what
the rig demonstrates; one unit name per PROFILE does not depend on the interval), and
`stop_daemon` tries the documented `worker --stop --wait 20s`, then SIGTERM, then
NAMES the escalation to SIGKILL if 10s of both were ignored. Step [4] measures the
outcome: `production daemons still alive after cleanup: 0` and both rig units gone.
The interval choice carries the reason in the script itself, so the next reader does
not "restore" 3600 and re-create a resident-daemon leak.


## Final acceptance run (after the test-hardening commit `1bc4247e`)

Re-run of the SAME rig, branch binary rebuilt, 2026-09-16 06:31:

```
=== [1] two profiles, two daemons ===
--- gopi scope units while BOTH production daemons run:
  gopi-daemon-merger.scope                     loaded active running /home/user/work/GoPi/bin/gopi merger
  gopi-daemon-worker-prod-a-67e16f78fe2d.scope loaded active running …/bin/gopi worker
  gopi-daemon-worker-prod-b-a0e576951b13.scope loaded active running …/bin/gopi worker

=== [2] a second --daemon for the SAME profile is still refused ===
gopi worker: daemon start failed: another worker daemon is already running
(lock /dev/shm/gopi-daemon-accept-2431763/prod-a/.gopi/worker.lock held by pid 2431779) [exit 1]

=== [3] the four worker-daemon tests, WITH the production daemons up ===
  … three units listed immediately BEFORE the suite (the two production ones + the operator's merger)
ok  	github.com/the operator/gopi/cmd/agent	10.194s          ← suite exit 0
  … immediately AFTER the suite:
  gopi-daemon-merger.scope                     loaded active running /home/user/work/GoPi/bin/gopi merger
  gopi-daemon-worker-001-ab07c6ec2aa3.scope    loaded active running …/worker-daemon-bin/gopi worker
  gopi-daemon-worker-prod-a-67e16f78fe2d.scope loaded active running …/bin/gopi worker
  gopi-daemon-worker-prod-b-a0e576951b13.scope loaded active running …/bin/gopi worker

=== [4] cleanup ===
production daemons still alive after cleanup: 0 (want 0)
both rig units are gone — the rig leaves no resident daemon
```

Two distinct per-profile units, the flock refusal naming the holder pid, the four
named tests green in the same window, and no residue — the whole acceptance.

A NOTE ON THE PROFILE TAG CHANGING BETWEEN RUNS: `prod-a` is
`510eca152b43` in the first rig run and `67e16f78fe2d` in this one. That is
CORRECT, not instability: the rig's scratch root carries the run's pid
(`/dev/shm/gopi-daemon-accept-$$`), so the two runs are two DIFFERENT profile
paths and therefore two identities. Within one profile the name is now
permanently the same (that is what `TestScopeUnitNameIsPerProfile` pins).

### The suite-vs-unit observation (measured, and NOT this epic's mechanism)

In step [3] a fourth unit appeared and was **still listed after the suite**:
`gopi-daemon-worker-001-ab07c6ec2aa3.scope`, running
`…/suite-cmd-agent-run-3869262774/worker-daemon-bin/gopi worker`. Its tag is
`001-…`, i.e. the profile path `…/suite-…/001` — a TEST-case scratch profile.

This is deliberately reported rather than "cleaned up in the rig": it is the
SAME class of leak this epic is about (a daemon outliving its test), it is now
ATTRIBUTABLE (one profile = one named unit, so the unit names which scratch
profile was left behind), and the suite's own leak policy is a separate task.
It also shows the property working: the leaked test daemon sat in its OWN unit
and did not occupy a production profile's name, which is exactly what the old
per-face name could not do.

### The wrong-bus defect is now FAILING, not logging (`1bc4247e`)

`TestDaemonScopeExistingUnitDecisionIsMeasured` used to compare the two buses and
only `t.Logf` when they agreed — so the exact regression that made the decision
inert (`--user` dropped) would have been caught by nothing. The user manager's
answer for a LIVE unit is now asserted.

RED PROOF: `--user` was removed from `daemonScopeUnitLoadState`, the binary
rebuilt, the test run — it FAILED:

```
daemon_scope_unit_test.go:526: systemd never reported gopi-daemon-worker-probe-2428561
as taken (the launcher is still running; pid 2428591) — a LIVE transient scope must
answer as taken; this is the condition the existing-unit decision exists for
--- FAIL: TestDaemonScopeExistingUnitDecisionIsMeasured (5.04s)
```

then the flag was restored and the test is green (0.07s). The failure lands on the
probe assertion first (the wrong bus makes the unit look absent, so the probe
refuses to report it as taken) — the new fatal covers the same path a step later.

## Verification

Commands run on the branch worktree unless a baseline is named; baseline is
`/home/user/workspace/gopi-master-baseline` (detached at `664b3784`, the same
commit the branch is based on), each side driving its OWN freshly built binary.

| check | branch | pristine master |
|---|---|---|
| `go build ./...` | OK | OK (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`) |
| `go vet ./cmd/agent/` | clean | **RED** — `funcBody redeclared in this block` (measured with the working tree stashed, so genuinely pristine: the `cmd/agent` test package does not compile) |
| the four named tests | **ok** (11.094s; each case also run separately with `-v`: SecondStartIsRefused 3.95s PASS, StatusReportsTheLiveDaemon 1.38s PASS, ForegroundWorkerIgnoresTheLock 5.72s PASS, StopEndsTheDaemon 2.78s PASS, RestartReplacesTheDaemon 3.84s PASS) | ok (9.885s) — see "what this evidence does NOT discriminate" above |
| full `go test ./cmd/agent/` | 10 FAIL | 11 FAIL |
| `go test ./internal/standards/` | ok (15.7s) | — |

**Failure-set delta — the branch fixes exactly one and breaks none.** Master's 11
and the branch's 10 are the same set except `TestDaemonScopeWrap_OnlyWrapsTheSystemdPath`
(red on master; **fixed here**). The 10 remaining on BOTH sides are pre-existing and
out of this epic's scope:

* `TestCLIContractTaskServeAliasPrintsOneLinePointer`, `TestTasksCLIListHeaderCountMatchesRows`
  — CLI text contract
* `TestWorkerDaemon_LeavesTheLoginSessionScope` (21.4s), `TestMergerDaemon_LeavesTheLoginSessionScope` (20.4s),
  `TestWorkerDaemon_DetachesAndKeepsRunning`, `TestWorkerDaemon_RequireScopeIsHardWhenUnavailable`,
  `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis` — the cgroup-escape family
  (on this host a `systemd-run --user --scope` child lands in a `user@1000.service`-owned
  scope, so the SID/setsid assertions cannot hold; the `--require-scope` pair is recorded
  in master's own commit message 664b3784)
* `TestLedgerOpenersUseProductionDSNContract`, `TestMergerFlock_DaemonLockStopsWithTheDaemon`
  — ledger-DSN / merger-flock contracts
* `TestTmpDirValueProbeLeavesNothingBehind` — pre-existing probe residue under the SHARED
  temp root (three directories dated 04:01–04:06, i.e. before this session's first run)

So the honest statement this change can make about the seam is:
**`go build` + `go vet ./cmd/agent` are green, the four named tests are green (with a
same-face production daemon alive), and no test that passed on master fails here.**
Making the whole `cmd/agent` package green is a separate task (the cgroup-escape family
deserves its own briefing, and three of its members were red before this branch existed).

## Not done (honest)

* The four named tests pass on the branch, but **they also passed on pristine master
  under the same conditions** — the change that made them observable is the
  compile-blocker fix, not the unit-name change. The per-profile property itself is
  proven by `TestScopeUnitNameIsPerProfile` / `TestScopeUnitNameCarriesNoLauncherPid`
  and by the live two-unit listing, not by a red→green transition of those four tests
  (details in "what this evidence does NOT discriminate"). It follows that
  `bec6301b`'s pid suffix would still be the ONLY thing separating two profiles'
  units in production — which is why this change is still the right one, and why the
  two-profiles-live-demo matters more than the test colour.
* Ten `cmd/agent` tests remain RED on this branch — the same ten that are red on
  pristine master (full set and classification in the table above). None is in this
  epic's acceptance set; the cgroup-escape family (`…LeavesTheLoginSessionScope`,
  `DetachesAndKeepsRunning`, `RequireScopeIsHardWhenUnavailable`,
  `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis`) has been red since before
  this branch and should be dispatched as its own task — on this host it is a
  host-capability question (userns scopes land in `user@1000.service`), not a naming
  question.
* The reuse branch of the decision is covered by a synthesised holder (a stamped
  `sleep`) because the LIVE path is pre-empted by the flock, as designed. Making the
  live path reachable would mean racing the lock on purpose — measured and recorded
  rather than faked.
* `TestDaemonScopeExistingUnitDecisionIsMeasured`'s live half was rewritten during this
  session after it proved to be measuring the flock, not the unit-name decision (the
  end-to-end restart case is the real integration evidence).
* `scripts/probe-daemon-term.sh` is kept as the small rig whose measurements are cited
  above (the `kill -TERM` question); it is a probe, not part of the acceptance rig.
* The operator's pre-existing resident merger still sits in the OLD unit name
  (`gopi-daemon-merger.scope`, started before the rename); it will take its
  profile-derived name on its next start.
* `git stash list` in the shared repo carries an unrelated pre-existing entry
  (`WIP on (no branch): c0cd21ab …`); the baseline worktree's one modified file
  (`cli_tasks_rebuild_pass_test.go`, the compile fix) is deliberately left in place.
