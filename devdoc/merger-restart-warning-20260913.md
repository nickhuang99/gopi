<!-- doc-class: historical -->
# Task Report: the merger-restart warning — a fabricated reason, printed as a failure

**Epic:** “merger restart giving some warning/error message” (2026-09-13).
**Branch:** `fix/cgroup-escape-available-probe` (worktree `~/workspace/gopi-merger-scope-warning`)
**Precursor:** `devdoc/daemon-cgroup-detach-20260913.md` (the epic that ADDED the cgroup escape).

---

## 1. The report

```
$ gopi merger --restart
gopi merger: asking pid 558723 to finish in-flight work and exit (SIGTERM, bound 1h0m0s)
gopi merger: pid 558723 exited 1.511s after the graceful stop request
gopi merger: WARNING: could not leave the login session scope (Failed to connect to bus: No such
             file or directory; in-process escape: cgroup scope escape unavailable: read
             /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service: open
             /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service: is a directory);
             starting with setsid only — this daemon WILL die when the session scope is torn down
gopi merger: starting a detached merger daemon (--every 5m0s)
gopi merger: cgroup escape: setsid-only (Failed to connect to bus: …; in-process escape: … is a directory)
merger daemon started: pid 901421, log /home/user/.gopi/logs/merger.log
```

Nothing failed — the daemon started and answered the handshake. Two things are wrong with what the
operator read: the alarm **shape** (WARNING / “WILL die”) and the **reason**, which is fabricated.

## 2. Root cause (two independent layers, both measured)

### 2a. The reason is a fabrication: an open(2) probe on a directory can only ever return EISDIR

`internal/cgroupscope.Available(uid)` decided whether the in-process escape works by

```go
func writable(path string) error {           // pre-fix
        f, err := os.OpenFile(path, os.O_WRONLY, 0)
        ...
}
```

called with the user manager **directory** (`/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service`).
A directory cannot be opened `O_WRONLY` — the kernel returns `EISDIR` on **every host**. So the
message `open …/user@1000.service: is a directory` was not a diagnosis at all: the probe could not
return `nil` for that path anywhere, and `mkdir` in the very same subtree **succeeds**
(`[ -w …/user@1000.service ]` = yes). The verdict “the in-process escape is unavailable” was
therefore produced without ever asking the question the escape asks.

### 2b. On this host the escape really is unavailable — for a different, real reason

Measured (kernel 5.15.0-163, cgroup v2, uid 1000):

| path | mode | owner | verdict |
|---|---|---|---|
| `/sys/fs/cgroup` | 555 | root:root | — |
| `…/user.slice/user-1000.slice` | 755 | root:root | **not writable by us** |
| `…/user-1000.slice/user@1000.service` | 755 | nick:nick | writable (mkdir works) |
| `…/user@1000.service/cgroup.procs` | 644 | nick:nick | writable as a FILE |

Migrating **this** process (a member of `…/user-1000.slice/session-N.scope`) into any target requires
write permission on the cgroup the source and the target **share** — the per-uid slice — which is
`root:root 0755`. Writing a pid succeeds for the *manager's own* processes (their slice ancestor is
the manager's parent, which they own) and fails `EACCES` for a session-scope process. Confirmed in
the kernel source, `kernel/cgroup/cgroup.c`:

```c
static int cgroup_procs_write_permission(struct cgroup *src_cgrp, struct cgroup *dst_cgrp, …)
{
        struct cgroup *com_cgrp = src_cgrp;
        /* find the common ancestor */
        while (!cgroup_is_descendant(dst_cgrp, com_cgrp))
                com_cgrp = cgroup_parent(com_cgrp);
        /* %current should be authorized to migrate to the common ancestor */
        ret = cgroup_may_write(com_cgrp, sb);
```

So `InSessionScope` is the honest, purely syntactic criterion for the gate, and the old probe
**never measured the gate**: it measured the manager dir, which *is* writable. Mechanism ①
(`systemd-run --user --scope`) is unavailable too: `/run/user/1000/bus` and
`/run/user/1000/systemd/private` do not exist; only the system bus answers and it needs polkit.

**The degradation is therefore the normal, unavoidable outcome on this host — and printing it as a
failure is what sent the operator looking for a bug.**

### 2c. Third defect: the verdict is the PARENT's prediction, never corrected by the measurement

`daemonScopeDecide` runs in the parent *before* the fork and its verdict was printed there. The
child's own measurement (`/proc/self/cgroup`, logged as `daemon: scope DEGRADED`) never reached the
operator. Consequence: on a host where the escape *does* work but the pre-fork probe is pessimistic,
the operator is told “it did not escape” about a daemon that did — the note contradicted the live
`/proc/<pid>/cgroup`. The note also printed **before** the “starting a detached merger daemon” line,
which is what made it read like a failure of the whole command.

## 3. What was implemented

### 3a. `internal/cgroupscope` — ask the kernel's real gate

- **New exported `Writable(path)`**: `syscall.Access(path, accessWrite)` (`access(2)`, `MAY_WRITE`,
  `accessWrite = 2` named because the stdlib exposes no `W_OK`). It answers for a **directory**,
  which is the thing the escape writes to (mkdir) and the thing the probe must be able to say yes
  about. Stdlib only — zero new dependencies.
- **The old `writable()` is gone** (its `errReadError` spelling “read …: is a directory” went with
  it). A refusal is now an `errAccessError` whose message is
  `<path> is not writable by this user (<errno>)` — the operation was not attempted, so it is not
  reported with a read/write verb.
- **New exported `SliceDir(uid)`** = `/sys/fs/cgroup/user.slice/user-<uid>.slice` — the cgroup every
  process of this uid lives under, i.e. the common ancestor `cgroup_procs_write_permission` checks.
- **`Available(uid)` now walks the two gates in the order the escape meets them**:
  1. stat the per-uid slice → `Writable(slice)` — *the migration gate*; on failure:
     `cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied)`;
  2. `Writable(manager)` — *the mkdir gate* → `creating the scope directory: …`.
  The verdict is the **first** gate that fails, names the concrete path, and carries no
  “is a directory”. The probe stays side-effect free (no mkdir, no stray cgroup).

### 3b. `cmd/agent` — the operator reads a MEASUREMENT, phrased as a note

- **The pre-fork warning is removed from `daemonSpawn`** (`cli_daemon_spawn.go` step ③b). The probe
  remains the *decision* (it decides `systemd-run` vs in-process vs `setsid`), but it is no longer
  printed as the verdict.
- **New `daemonScopeMeasureNote`** (step ③d, after `child.Process.Pid` exists and after the start
  proof): it prints the cgroup the daemon **actually** has, read from `/proc/<pid>/cgroup` through
  the shared reader (`cgroupscope.RelOfPID`) — the same file the incident was diagnosed from.
- **New pure `daemonScopeNoteFor(prog, rel, relErr, scope)`** renders the three outcomes:
  - measured **outside** a session scope → **no line at all** (a successful escape is already
    reported by `cgroup escape: <mechanism>` before the start);
  - measured **inside** a session scope → the plain note
    `note: could not leave the login session scope — this daemon runs in <measured cgroup> and will
    end with that scope.` (the sentence “could not leave the login session scope” is kept verbatim —
    the e2e suite asserts it);
  - the measurement itself **failed** → a line that *admits* it and labels the stored probe verdict
    as `this is the pre-start probe: …`. The prediction is never passed off as a reading.
- **`scopeNoneDetail` / `firstLine`**: the degraded reason is reduced to one line per mechanism
  (joined by `daemonScopeNoneDetailFmt`). The degraded case is the *normal* one here, so it is read
  on every restart — a multi-line systemd-run failure pasted inline is what made the original
  message unreadable. The full text stays in the daemon's log via `daemonScopeDetailEnv`.
- **`--require-scope` is unchanged**: a hard refusal naming the flag, with the honest reason.

### 3c. What the operator now sees (same host, same command)

```
gopi merger: starting a detached merger daemon (--every 5m0s)
gopi merger: cgroup escape: setsid-only (Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied))
merger daemon started: pid 1126678, log …/logs/merger.log
gopi merger: note: could not leave the login session scope — this daemon runs in /user.slice/user-1000.slice/session-18252.scope and will end with that scope.
```

No WARNING, no WILL-die, and the last line names the cgroup read from the **live daemon's**
`/proc/1126678/cgroup`. The reason is now true (`… non writable … permission denied`) and actionable
(that path is root-owned; fixing it is a host delegation change, not a GoPi bug).

## 4. Verification

| check | command | result |
|---|---|---|
| build | `CGO_ENABLED=0 go build ./...` | **OK** |
| vet | `go vet ./internal/cgroupscope/ ./cmd/agent/` | **clean** |
| unit (probe + reason) | `go test ./internal/cgroupscope/` | **ok** (incl. the new cases) |
| wording | `go test ./cmd/agent/ -run 'TestDaemonScope'` | **ok** |
| e2e (shared seam, both faces) | `go test ./cmd/agent/ -run 'Scope\|LeavesTheLoginSessionScope\|RequireScope\|StartProof' -count=1` | **ok (50.1s)** |
| e2e (the reported command) | `go test ./cmd/agent/ -run TestMergerRestart_NoAlarmWordingAndTheCgroupIsMeasured` | **PASS** |
| standards + style gates | `go test ./internal/standards/ ./internal/stylegate/` | **ok** (7.3s / 4.3s) at the merged tip |
| live | `merger --restart` with the built binary, private `GOPI_HOME` | §3c transcript, alarm-word scan empty, `/proc/<pid>/cgroup` matches |
| A/B | pre-fix binary vs fixed binary, §4b | the reported warning is gone; the reason is true |

### 4a. RED proofs — re-run at the merged tip, transcripts verbatim

Both proofs were produced in a **detached worktree of this branch's tip** by hand-restoring the
pre-fix code into the branch's file (master's `Available()` + its `os.OpenFile(path, O_WRONLY, 0)`
`writable()` helper), then deleting the scratch tree (`/dev/shm/gopi-prefix-wt`) — nothing outside
the worktree was touched.

1. **Fabricated reason** (`go test ./internal/cgroupscope/ -run 'TestWritable_…|TestAvailable_…'`):

   ```
   --- FAIL: TestWritable_AccessesInsteadOfOpeningTheDirectory
       cgroupscope_test.go:180: Writable(/tmp/…/001) = /tmp/…/001 is not writable by this user
           (open /tmp/…/001: is a directory), want nil: a writable directory must be reported writable
   --- FAIL: TestAvailable_ReasonIsNotTheFabricatedEISDIR
       cgroupscope_test.go:228: the unavailable reason carries the fabricated open-a-directory artefact:
           cgroup scope escape unavailable: read /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service:
           open /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service: is a directory
   ```

   and the live verdict with the old probe prints the operator's exact string:
   `Available() -> cgroup scope escape unavailable: read …/user@1000.service: open …: is a directory`.
2. **Wording** (flipping `daemonScopeDegradedNote` back to the pre-fix text):

   ```
   --- FAIL: TestDaemonScopeDegradedNote_HasNoAlarmWording
       daemon_scope_note_test.go:56: the degraded note carries alarm wording "WARNING" …
       daemon_scope_note_test.go:56: the degraded note carries alarm wording "WILL die" …
   --- FAIL: TestDaemonScopeNote_InSessionScopeIsAPlainNote
       daemon_scope_note_test.go:85: the note is not a plain note: it carries "WARNING": …
   ```

   Restored, both pass — the guard is bound to the constant, so a future re-inline fails CI.
3. `TestWritable_AccessesInsteadOfOpeningTheDirectory` is host-independent: it asserts
   `Writable(t.TempDir()) == nil` **and** that `os.OpenFile(the same dir, O_WRONLY)` returns `EISDIR`
   — so it pins the fix to the probe, not to this host's delegation state.

### 4b. A/B on the reported command (pre-fix binary vs fixed binary, same host, private GOPI_HOMEs)

Both binaries were run as `merger --restart` in their own `GOPI_HOME` under `/dev/shm`. The pre-fix
binary is a `--detach` build of this branch's tip **with master's code restored** (§4a scratch); the
fixed binary is this branch as committed.

| line | pre-fix (`/dev/shm/gopi-prefix`) | fixed (this branch) |
|---|---|---|
| verdict | `WARNING: …; starting with setsid only — this daemon WILL die when the session scope is torn down` (**before** the “starting a detached …” line) | **no such line** |
| reason | `cgroup scope escape unavailable: read …/user@1000.service: open …: is a directory` (fabricated) | `… migrating a pid into cgroup.procs: …/user-1000.slice is not writable by this user (permission denied)` (true) |
| measurement | absent (the parent's prediction was the only verdict) | `note: could not leave the login session scope — this daemon runs in /user.slice/user-1000.slice/session-18252.scope and will end with that scope.` (**after** the start line, read from the daemon's live `/proc/<pid>/cgroup`) |
| exit code | 0 | 0 |

Both daemons were then stopped through `merger --stop` and both exited cleanly (2.0s, rc=0), so the A/B
also confirms the start/stop lifecycle is unaffected.

## 5. Not Done / honest limits

- **This host's escape is still unavailable** (2b). The epic was about the *message*, and the message
  is now true; making the escape actually succeed here needs a host change (`/sys/fs/cgroup` mounted
  with `nsdelegate`, or a user manager that owns `user-1000.slice`) — deliberately **not** attempted
  (GoPi never fixes a host: GOPI.md Rule 22’s sibling discipline).
- **The daemon still runs in the session scope** on this host, so it will still end with that scope.
  That fact is now stated plainly instead of being shouted as a failure; `--require-scope` turns it
  into a refusal for an operator who needs the guarantee.
- Mechanism ① (systemd-run) is untested as a *success* path here because no user bus exists — the
  pre-existing e2e cases cover the branch that the host allows, exactly as before this change.
- **The fixed binary is not installed**: `~/work/GoPi/bin/gopi` still runs master's code until the
  merge + rebuild (Rule 12). The live transcript in §3c is from this branch's freshly built binary,
  not from the installed one.
- **Live-verification hygiene (disclosed):** the `merger --restart` run in §3c started a daemon under
  a `/dev/shm` `GOPI_HOME`; a later `merger --stop` against that sandbox reported success while the
  process was still alive and it had to be `kill -KILL`ed — i.e. **the sandbox stop path is itself
  suspect and is NOT claimed fixed here** (the §4b A/B runs, which used the branch binary and a fresh
  sandbox, both stopped cleanly in 2.0s). Nothing outside the sandbox was written: the daemon's only
  repo pass reports `merged=0`, and `~/work/GoPi` `master` was verified untouched (`git log -1` =
  `db1ac4aa`, working tree unchanged).

## 6. Deliverable declaration (Rule 6d)

- **Branch:** `fix/cgroup-escape-available-probe` (worktree `~/workspace/gopi-merger-scope-warning`)
- **Recovery:** `git show fix/cgroup-escape-available-probe:devdoc/merger-restart-warning-20260913.md`
- **Commit is in the shared main repo** (`~/work/GoPi`) via the linked worktree — no push needed.
- **Files:** modified — `internal/cgroupscope/cgroupscope.go`, `internal/cgroupscope/cgroupscope_test.go`,
  `cmd/agent/cli_daemon_scope.go`, `cmd/agent/cli_daemon_spawn.go`; new —
  `cmd/agent/daemon_scope_note_test.go`, `cmd/agent/daemon_scope_restart_e2e_test.go`.
- **NOT merged** (PA merges after acceptance).
