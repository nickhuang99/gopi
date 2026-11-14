<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Devdoc: the user-bus diagnosis (three host states, one blended message)

**Epic**: user-bus diagnosis, 2026-09-15. **Branch**: `feat/daemon-userbus-diagnosis`.

## The defect

When BOTH escape rungs of the daemon ladder fail, `cmd/agent/cli_daemon_scope.go`
rendered ONE blended reason:

```
gopi worker: cgroup escape: setsid-only (Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: …)
```

That sentence covers **three distinguishable host states**, and the repairs
differ:

| # | state | how it is measured | repair |
|---|-------|--------------------|--------|
| ① | **no user manager** | `user@<uid>.service` not `active` (asked on the SYSTEM bus) | create a session / `loginctl enable-linger` |
| ② | **bus path unlinked** — manager alive, socket path ABSENT in the VFS, kernel still holds the socket | path `stat` → ENOENT **and** a `/proc/net/unix` row (LISTEN, inode), connect → ENOENT | operator: restart `user-runtime-dir@<uid>.service`, then `user@<uid>.service`; keep sweeps out of the runtime dir |
| ③ | **stale socket node** — a node exists, nothing accepts | path present, connect → ECONNREFUSED | something else holds the path, or restart `user@<uid>.service` |

State ② is **this host's state** and it is the silent rung-① disabler: measured
2026-09-15, uid 1000 — `/run/user/1000/bus` and `/run/user/1000/systemd/private`
both absent from the VFS (`stat` ENOENT), both still **bound** in
`/proc/net/unix` (LISTEN, inodes 40136 / 39144), `connect()` → ENOENT, while
`user@1000.service` is `active` on the system bus and its cgroup
(`/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service`) is present. The
manager was never dead; the path was.

The kernel side of that claim was reproduced independently (a control
experiment, not an inference): bind a unix socket, `unlink` its path while the
listener stays open → the `/proc/net/unix` row survives **with the same inode**
and `connect()` returns **ENOENT**. A socket whose path is gone cannot be
reached by name even while the listening process holds it.

## What was built

`cmd/agent/daemon_userbus_probe.go` — a zero-dep (stdlib) unprivileged probe and
a PURE classifier:

* `observeUserBus(uid, XDG_RUNTIME_DIR)` takes the measurement — one liveness
  question plus one observation per socket path:
  * **liveness** = `/usr/bin/systemctl is-active user@<uid>.service`. It is asked
    on the **system** bus: the user manager's own bus is the thing under
    diagnosis, so asking it would be circular (it would answer "no bus" whenever
    the bus is the fault — the exact blending this epic removes). `systemctl`'s
    exit status is the answer (0 active, 3 inactive, 4 unknown unit); a question
    that could not be asked at all is carried as an error, **never** read as
    "no manager".
  * **path presence** = `stat(2)` on `<runtime>/systemd/private` and
    `<runtime>/bus`, in that order (the private socket is the one
    `systemd-run --user` dials, i.e. the decisive one for rung ①).
  * **kernel binding** = the path's row in `/proc/net/unix` (state column for
    LISTEN, inode column for the holder).
  * **client verdict** = a REAL `net.DialTimeout` with a bounded budget; the
    errno is the discriminator (ENOENT = unresolvable path, ECONNREFUSED =
    present node refusing).
* `classifyUserBus(obs)` is a pure function of that observation, in the
  discriminator's order: unaskable liveness → `unknown`; manager inactive →
  `no-user-manager`; **absent-and-bound** → `bus-path-unlinked`; present-and-
  refused → `bus-node-stale`; all paths accepting → `bus-ready`; anything else →
  `unknown`. Each verdict carries the reason code, the operator sentence, the
  evidence line, the confirming commands (`systemctl`, `stat`, `grep -F … /proc/net/unix`)
  and the next step.

**Wiring** (`cli_daemon_scope.go`, `cli_daemon_spawn.go`): `daemonScopeDecide`
takes the diagnosis ONCE, from rung ①'s own error, and only when that error
mentions the bus (`userBusFailureMentionsBus`) — a systemd-run failure about
something else must not collect a user-bus verdict it did not earn, and a
healthy start pays nothing. The diagnosis travels in `daemonScopeDecision.userBus`
and is rendered by `daemonUserBusReport` at the two exits where an operator
needs it: the **chosen-mechanism** line of a degraded start (immediately after
the blended sentence, which is kept — it names the failing invocation) and the
**`--require-scope` refusal** (plus the pre-existing no-privilege correction).

## Decisions and traps

* **The block is NOT inside `daemonScopeMeasureLines`.** That function's output
  is PINNED to exactly two lines by `daemon_scope_no_privilege_test.go` (the
  measured note + the correction), and the degraded note is asserted to be one
  line by `daemon_scope_note_test.go`. Adding a diagnosis there would have
  broken both. It is printed at the seam, before the fork, where the decision —
  not the child's cgroup — is the subject.
* **`omitempty` is not the issue here, but "zero value must print nothing" is
  the same family (Rule 15):** `userBusDiagnosis{}` (probe not taken) renders no
  line, so a start whose rung ① failed for another reason stays silent instead of
  printing an uninvented state.
* **Rule 25 (no privilege as the remedy):** the repair that needs an operator
  (restarting the manager) is named as an OPERATOR action; gopi performs no host
  repair and asks for no privilege. The correction fragment
  `not a privilege problem` is kept verbatim inside the unlinked state's next
  step, so the no-privilege gate's presence half still holds and the state the
  operator is actually in carries the answer.
* **`/proc/net/unix` column indices** are named constants
  (`procNetUnixPathFieldIdx0` …) rather than inline numbers — a parser whose
  field index drifts by one silently stops detecting the state (Rule 1/13).
* **The suite's own scratch root** (`testutil.SuiteTempRoot()`) is used for the
  positive-control socket, never a hardcoded `/tmp` (Rule 22).

## Verification

* `go build ./...` — OK.
* `go test ./...` — green (see the task report for the exact tail).
* Targeted: `go test ./cmd/agent/ -run TestUserBus` — the three synthetic states,
  the red proof (② never classifies as ①, and the same measurement yields
  different reasons under different liveness), the positive control (a real
  unlinked-yet-bound socket is detected), the live both-worlds case, the seam
  provenance cases (degraded start + `--require-scope` refusal).
* Rule 11 (real binary, this host): `gopi worker --daemon` before/after, and
  `--require-scope` before/after — the AFTER output carries
  `user bus (bus-path-unlinked)` with the measurement, the confirming commands
  and the next step. Transcripts: `devdoc/userbus-diagnosis-probes/` (markdown
  files, each carrying the worker-ownership pointer the doc gate requires —
  the first commit used `.txt` captures, which the gate also audits, and that
  was corrected on `fix/userbus-doc-worker-ownership`).
