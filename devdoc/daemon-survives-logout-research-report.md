<!-- doc-class: historical -->
# Task Report — Research: the upstream-documented way to run a long-lived user daemon that survives logout

**Epic**: research only — no implementation, no fix landed (a separate task consumes the finding).
**Branch**: `research/daemon-survives-logout` (worktree `~/workspace/gopi-daemon-research`).
**Deliverable**: `devdoc/daemon-survives-logout-research.md` (+ verbatim probe transcripts in `devdoc/daemon-survives-logout-probes/`).

---

## Completed

### 1. The question and the finding

Researched the standard, upstream-documented mechanisms for a logout-surviving user daemon on a
headless Linux host (`systemd 249`, Ubuntu 22.04, cgroup v2 unified), where
`$XDG_RUNTIME_DIR/bus` and `$XDG_RUNTIME_DIR/systemd/private` are **absent** even though
`user@1000.service` is active with `Linger=yes` — so `systemd-run --user --scope` fails and gopi's
daemon degrades to `setsid`-only inside `session-N.scope`.

**Recommended solution (one paragraph, carried in the devdoc):** run the daemon as a **user unit**
(`systemd --user`, `WantedBy=default.target`) with **lingering enabled**
(`loginctl enable-linger nick` — already `yes` here), which puts the process in
`user@1000.service/…` **outside every login session scope** and therefore outside the SIGTERM that
a session teardown (`loginctl terminate-session`, ssh close, `KillUserProcesses=yes`) delivers;
drive it with `systemctl --user` and verify with `systemd-run --user --scope`, both of which need
the user bus. On this host that bus vanished because the **socket paths under
`/run/user/1000` were unlinked while the manager kept them bound** — a state the XDG
specification explicitly permits (`$XDG_RUNTIME_DIR` contents MAY be periodically cleaned up) and
which only a manager restart recreates. So the *durable* recommendation is (a) do NOT park
socket-bearing files in `$XDG_RUNTIME_DIR`, (b) restart `user@1000.service` (operator/root-gated)
to restore the bus, and (c) keep the daemon in `user@1000.service` so it survives logout by
cgroup membership rather than by signal handling.

### 2. Canonical mechanisms with upstream URLs

`enable-linger`/`Linger=`, `user@.service` + `user-runtime-dir@.service`, `systemd --user` +
`WantedBy=default.target`, transient scopes via `systemd-run --scope`, `KillUserProcesses=no` and
session-scope abandonment, `systemd.scope(5)` semantics, `KillMode=`, the cgroup-v2 single-writer
contract, and the XDG runtime-dir cleanliness rule — **12 citations** (11 upstream man pages /
specifications plus one marked-secondary wiki), all HTTP 200 verified (`p13.out` section D).

### 3. Root cause of the absent user bus (with verbatim evidence)

**Not** a `logind.conf`/linger/PAM misconfiguration (all stock/`yes`/`no` as documented), **not** a
separate mount namespace (manager, dbus-daemon and this shell all share `mnt:[4026531841]`), **not**
a permission artifact (`sudo`-level listing of the manager's own view shows only `inaccessible/`).
The sockets are **bound in the kernel and unlinked from the filesystem**:

```
0000000000000000: 00000002 00000000 00010000 0001 01 39144 /run/user/1000/systemd/private
0000000000000000: 00000000 00000000 00010000 0001 01 40136 /run/user/1000/bus   # plus 8 connected entries
stat: cannot statx '/run/user/1000/bus': No such file or directory
```

with the holders still alive (manager pid 2321 fd 19 → `socket:[39144]`, fd 27 → `socket:[40136]`;
dbus-daemon pid 2342 fd 3 → `socket:[40136]`; `dbus-daemon`'s own
`DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus`), `connect()` → `ENOENT`, and no process
on the host whose filesystem view carries the path (`visible_in_any_process=0`). The uid-117
control user (lightdm, `Linger=no`) still has `/run/user/117/{bus,systemd/private,systemd/notify}`
bound ⇒ the loss is **uid-1000-specific**, not a host-wide policy. Two mtime brackets exist
(`/run/user/1000` 2026-09-14 22:39:10; `/run/user/1000/systemd` 2026-09-15 07:26:18) but neither
the journal (empty windows) nor any scheduled job names the actor, so the **exact actor is recorded
as unresolved** with the two candidate mechanisms that could match a sweep's namespace
(`internal/testutil.legacy_residue`'s `gopi-*suite-tmp-*` prefix rule, and `GOPI_TEST_TMPDIR`
being pointed at the runtime dir by any test/operator run) — an honest, dispatchable gap.

### 4. Comparison table, verification recipe, anti-patterns, ladder critique, checklist

All present in the devdoc as required (mechanism → guarantee → needs root? → measured works here
Y/N/?; a full logout-survival recipe with cgroup assertion and a second-logout repeat; setsid /
nohup / disown / `&` / screen / tmux / `cgroup.procs` / `daemon(3)` each with what it fails to
protect; an explicit critique of `cli_daemon_scope.go` + `internal/cgroupscope`; a follow-up
implementation checklist).

### 5. Build/test unaffected (doc-only change)

```
go build ./...   → exit 0
```

## Not Done (honest)

1. **No fix landed** — by constraint (research only). The follow-up checklist names the separate task.
2. **The exact unlinker is not identified**, only narrowed: ruled out by measurement are tmpfiles,
   cron/timers, mount namespaces and gopi's default root lists; the evidence points at a run whose
   root was the runtime dir (the same-host `XDG_RUNTIME_DIR=/tmp/tabpoc/rt` pattern) and at
   `os.RemoveAll(root)` cleanup under a runtime-dir pin. Recorded as a dispatchable follow-up.
3. **The privileged control read was NOT run**: the decisive VFS check for uid 117
   (`sudo -n ls -la /run/user/117/systemd/`) was refused by the harness (a `sudo` token in a bash
   command is blocked), so the per-user conclusion rests on `/proc/net/unix` (kernel state, readable
   without privilege) plus the uid-1000 `sudo`-free evidence. Exact operator command recorded in the
   devdoc's Not Done section.
4. **Logout survival was not performed live** (a second logout/login would terminate the operator's
   sessions). Survival is evidenced *indirectly and strongly*: 6 session scopes
   (`15158/15164/15425/17674/18199/18661`) hold **78 live processes** (recounted in `p13.out`) while
   `loginctl list-sessions` names none of them — those are abandoned scopes whose processes outlived their login. The devdoc
   distinguishes "measured" from "inferred" in the table.
5. **The manager-restart repair was not exercised** (it would kill the operator's desktop session
   processes: the manager tree holds the XFCE/desktop session). Its cost and gating are documented
   from measurement (`pkcheck` says the action needs authentication).

## Verification

- Build: `go build ./...` → OK (exit 0), doc-only change
- Repro tests: none required (research task; no production behavior changed)
- Probes: 14 read-only probe scripts + verbatim outputs committed under
  `devdoc/daemon-survives-logout-probes/` (p1…p14, scripts and `.out`)
- Deliverable renders: `git show research/daemon-survives-logout:devdoc/daemon-survives-logout-research.md`

## Commit

see the branch tip (`research/daemon-survives-logout`).
