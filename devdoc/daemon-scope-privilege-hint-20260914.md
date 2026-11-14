<!-- doc-class: historical -->
# Devdoc: foresee the daemon cgroup privilege requirement (+ the `--once` answer)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
> The launch commands this devdoc shows (`gopi worker --daemon`) are the
> OPERATOR's to run — an agent must never run them (submit the work instead:
> `gopi task submit`). They appear here as the text the operator reads.

> **SUPERSEDED (2026-09-14, epic `merger cannot escape cgroup with sudo`).**
> This branch landed in master as `f4f761c5` and its central remedy was WRONG:
> the project asked the OPERATOR FOR A PRIVILEGE (a root SYSTEM scope) to obtain
> the cgroup escape. Privilege is a dead end, and the operator's standing
> principle is that daemon running gets no privileged solution at all — a daemon
> started under root was measured in the cgroup of its UNPRIVILEGED launcher,
> and root has no user manager of its own to migrate into. The prescription was
> removed, the regression gate that refuses its return is
> `cmd/agent/daemon_scope_no_privilege_test.go`, and the measurements are in
> `devdoc/no-privilege-daemon.md`. What SURVIVES from this work: the `--once`
> answer (a non-detaching mode needs no privilege), and the e2e success-line
> parser fix below (`6cafd57f`), which is load-bearing.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
> The launch commands this devdoc shows (`gopi worker --daemon`) are the
> OPERATOR's to run — an agent must never run them (submit the work instead:
> `gopi task submit`). They appear here as the text the operator reads.

**Branch:** `fix/daemon-scope-privilege-hint` (worktree `~/workspace/gopi-daemon-privilege-hint`)
**Date:** 2026-09-14
**Epic:** "escape cgroup requires super user" — the operator's report of the
degraded `gopi worker --daemon` start (attachment `tmp/cgroup.txt`)
**Verdict:** DOABLE

---

## 1. The operator's report, and what was wrong with its premise

The operator ran `gopi worker --daemon` and read:

```
gopi worker: cgroup escape: setsid-only (Failed to connect to bus: No such file or directory;
  in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs:
  /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied))
worker daemon started: pid 2527977, log /home/user/.gopi/logs/worker.log
gopi worker: note: could not leave the login session scope — this daemon runs in
  /user.slice/user-1000.slice/session-18855.scope and will end with that scope.
```

and concluded: *"escape cgroup requires super user — gopi should foresee this
privilege requirement, simply return help to prompt sudo running instead of
giving failure message"*, plus: *"does this mean `worker --once` also requires
super user? if yes, help should remind user."*

**The premise was measured to be wrong, and that changes the fix.** All rows
below are a real detached worker daemon with its cgroup read LIVE from
`/proc/<pid>/cgroup` (the seam's own rule: the measurement decides, the probe's
prediction never does):

| start | measured cgroup | escaped? |
|---|---|---|
| unprivileged `gopi worker --daemon` | `/user.slice/user-1000.slice/session-18855.scope` | no (announced) |
| **root, no wrapper** | `/user.slice/user-1000.slice/session-18855.scope` | **no — root alone changes nothing** |
| root inside `systemd-run --scope` | `/system.slice/gopi-daemon-ph-c.scope` | **yes** |

Why root alone fails (5-why, and it is the same mechanism as the unprivileged
EACCES — see `devdoc/daemon-cgroup-detach-20260913.md` §2): a process
**inherits** its cgroup from its launcher, so `sudo gopi worker --daemon`
starts in the same session scope and its detach is the same detach. Mechanism ②
does not rescue it either: `cgroupscope` resolves the manager from
`os.Getuid()`, and uid 0 has no `user@0.service`. Only a **root SYSTEM scope**
moves the daemon (`systemd-run --scope` without `--user`).

So a hint that said "re-run with sudo" would be **the wrong fix** — a hint that
sends the operator to a command that provably does not work is worse than the
silence it replaces.

## 2. What was implemented

### 2a. The next step is printed beside the degraded note

`cmd/agent/cli_daemon_scope.go`:

- `daemonScopePrivilegeHintFmt` / `daemonScopePrivilegeHintFor(prog, component,
  scope)` render the operator's next step (the `systemd-run --scope --unit=…`
  wrapper + the fact that root membership alone is not enough).
- `daemonScopeMeasureNote` prints the note, then the hint — a **separate print**,
  because `daemon_scope_note_test.go` pins the degraded note itself as a plain
  single-line measurement with no alarm wording.
- Silent when there is no step: the escape succeeded (`scope.scopeEscaped()`), or
  the operator's own `GOPI_CGROUP_ESCAPE_OFF` disabled it (that degradation is a
  decision they made; "re-run as root" would be telling them to undo it).
- Keyed on the **decision** the parent measured, never on parsing the two
  mechanism error strings — a second textual classifier would drift from the
  code that produced them.

Live output on this unprivileged host:

```
gopi worker: note: could not leave the login session scope — this daemon runs in /user.slice/user-1000.slice/session-18855.scope and will end with that scope.
gopi worker: to make the next start independent of this login session, re-run it as root inside a SYSTEM scope:
  /usr/bin/systemd-run --scope --unit=gopi-daemon-worker <the gopi argv>
  Root membership alone is not enough — a process inherits its cgroup from its launcher (measured on this host); the scope wrapper is what moves it.
```

### 2b. The `--require-scope` refusal carries the same next step

`daemonScopeRefusalNote` (`cli_daemon_scope.go`) + its call site in
`cli_daemon_spawn.go`: under `--require-scope` the daemon does not start at all,
so the operator is stuck **harder** than on the degraded path. Printing the two
failed mechanisms without the fix was the exact dead end the operator reported,
in its hardest form (Rule 15). Both exits of the privilege seam now tell the
same measured thing, and both stay silent for the off-switch.

### 2c. Both help texts answer the operator's two questions

`cmd/agent/main.go`, a new **"Privileges (which mode needs them)"** section in
`workerUsageHead` and `mergerUsageHead`:

- only the **detached** form can need a privilege, and only for the cgroup;
- root membership **alone** does not help (with the measured reason);
- `--require-scope` turns the degradation into a refusal;
- **explicitly**: neither the foreground loop nor `--once` needs any privilege —
  `--once` never detaches, spawns nothing, touches no cgroup;
- `--once` is explained against `gopi task run next`: the ONE task that command
  claims, applied repeatedly, plus the round/idle bookkeeping — "reach for
  `--once` where you would otherwise loop `gopi task run next` yourself".

## 3. Verification

| check | result |
|---|---|
| `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| `go test ./cmd/agent/ -run 'TestDaemonScope\|TestWorkerUsageHead\|TestMergerUsageHead'` | PASS (16 cases, incl. the pre-existing wording pins) |
| real `gopi worker --daemon` (unprivileged, hermetic `GOPI_HOME`) | note + hint printed; daemon started; self-exited after its idle round |
| real `gopi worker --daemon --require-scope` | refusal + the same next step, exit 1, no orphan |
| real `gopi worker --once` | drains the queue, exit 0, no escape path |
| daemon e2e (`TestWorkerDaemon_*`, `TestMergerDaemon_*`) | PASS, run ALONE (RC=0) |
| root-system-scope escape | measured at `/system.slice/gopi-daemon-ph-c.scope` (by hand, see §4) |
| `gofmt -l cmd/agent/`, `go vet ./cmd/agent/` | clean |

## 4. Not done / honest limits

- **No self-escalation.** gopi never re-execs itself under sudo: the hint
  instructs the operator. The daemon still starts (degraded) — that behaviour is
  unchanged and deliberate.
- The privileged escape was verified **by hand** with a probe script, not as a
  test case: a test needing a privileged `systemd-run` cannot run in the standard
  suite, and a silently-skipping test is worse than the recorded evidence. The
  recipe is in memory (#2592).
- The hint is **general** (it does not name which mechanism failed). On this
  host the reason is the same privilege; a host where the bus exists but the
  delegation does not would still be told the scope wrapper — which is the fix
  there too (`cgroupscope` still needs write permission on the slice).

## 4b. The daemon e2e failures this work first blamed on concurrency (corrected)

The first verification run of this branch showed `TestWorkerDaemon_*` failing
with `success line named log "it."` / `never showed the loop banner` while a
full-suite run was in flight, and that was recorded as *run-concurrency
contamination*. **The hypothesis was wrong, and running the same tests on a
clean MASTER worktree proved it** (job-10, no other test running):

```
--- FAIL: TestWorkerDaemon_DetachesAndKeepsRunning
    worker_daemon_flag_e2e_test.go:339: success line named log "scope.", want a "worker.log" file
--- FAIL: TestWorkerDaemon_OnceDrainsAndExits
    the --once mode did not survive the re-exec: (log unreadable: open scope.: no such file or directory)
```

Master fails identically (`"scope."` = the last word of the *pre-existing*
degraded note), so this is a **pre-existing defect in the test parser**, dated to
the commit that introduced the seam (`9ab8cf17`), not contamination and not this
branch. Mechanism: the parent prints the success line (`worker daemon started:
pid N, log <path>`) and THEN the post-start cgroup note; both parsers took the
log path as **the last whitespace token of the whole remaining output**, so the
"path" became the note's final word — `"scope."` on master, `"it."` once my
hint's last sentence followed it. The test then waited on a path that never
existed. (The wording-sensitivity is the tell: the parse agreed with reality only
while the note's last word happened to be the path.)

Fixed in **both** parsers (`wdParseStart`, `mdParseDaemonStart` — the merger had
the identical shape): slice the output to the success line's OWN line before
tokenizing. With that, `go test ./cmd/agent/ -run 'TestWorkerDaemon_|TestMergerDaemon_'`
is **green (RC=0)**.

Lesson recorded (memory #2596): when two e2e tests fail with "log unreadable",
read the failure's *parsed value* before blaming the runner — a parse that
returns prose is a parsing bug, and the cheapest refutation of a concurrency
hypothesis is the same test on a clean master worktree.

## 5. Deliverable declaration (Rule 6d)

- **Branch:** `fix/daemon-scope-privilege-hint` (worktree `~/workspace/gopi-daemon-privilege-hint`)
- **Recovery:** `git show fix/daemon-scope-privilege-hint:devdoc/daemon-scope-privilege-hint-20260914.md`
- **In the shared main repo** (`~/work/GoPi`) via the linked worktree — no push needed.
- **Files:** `cmd/agent/cli_daemon_scope.go`, `cmd/agent/cli_daemon_spawn.go`,
  `cmd/agent/main.go`, `cmd/agent/daemon_scope_privilege_test.go`,
  `devdoc/daemon-scope-privilege-hint-20260914.md`,
  `tasks/daemon-scope-privilege-hint-report.md`.
