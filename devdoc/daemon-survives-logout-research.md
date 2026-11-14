<!-- doc-class: normative -->
# Research: running a user daemon that survives logout (headless Linux + systemd), and the absent user bus on this host

**Status**: research only — **no implementation, no fix landed** (a separate task consumes this finding).
**Branch**: `research/daemon-survives-logout` · **Worktree**: `~/workspace/gopi-daemon-research`
**Host**: `peerbox`, Ubuntu 22.04.4, `systemd 249 (249.11-0ubuntu3.16)`, kernel `5.15.0-163-generic`, cgroup v2 unified, uid 1000 (`nick`).
**Evidence**: every host claim below is backed by a recorded command + verbatim output. The 14 read-only probe scripts and their full outputs are committed beside this file in `devdoc/daemon-survives-logout-probes/` (`p1…p14`). Citations are upstream man pages / specifications; secondary sources are marked.

---

## 1. Recommended solution (one paragraph)

Run the daemon as a **`systemd --user` unit** (`~/.config/systemd/user/<name>.service`, pulled in by
`WantedBy=default.target`) with **lingering enabled** for the user (`loginctl enable-linger nick`;
already `yes` on this host — measured). Lingering keeps `user@1000.service` alive with no session, so
the daemon's process lives in `user@1000.service/…` — **outside every login-session scope** — and is
therefore not a member of the cgroup that `loginctl terminate-session`, an ssh disconnect, or
`KillUserProcesses=yes` tears down with SIGTERM. Start/stop it with `systemctl --user` and verify
placement with `systemd-run --user --scope`. On this host the *control path* for that mechanism
(`systemctl --user`, `systemd-run --user`) is currently broken because the user bus socket path
`$XDG_RUNTIME_DIR/bus` (and `systemd/private`) **was unlinked while the manager kept it bound** — a
state the XDG specification explicitly permits, and which is healed only by recreating the runtime
directory/manager. So the durable recommendation is threefold: **(a)** keep the daemon in
`user@1000.service` so logout survival is a *cgroup membership* property, not a signal-handling
trick; **(b)** never park socket-bearing files in `$XDG_RUNTIME_DIR` — the spec warns that its
contents MAY be periodically cleaned up, and this host demonstrates the failure; **(c)** restore the
user bus by an operator action that recreates `/run/user/1000`
(`systemctl restart user-runtime-dir@1000.service` / `user@1000.service`, or a reboot — see §3.4),
after which rung ① of gopi's ladder works and rung ② is no longer needed.
Sources: [`loginctl(1)`](https://man.archlinux.org/man/loginctl.1.en) (`enable-linger`),
[`user@.service(5)`](https://man.archlinux.org/man/user@.service.5.en),
[`systemd.special(7)`](https://man.archlinux.org/man/systemd.special.7.en),
[`logind.conf(5)`](https://man.archlinux.org/man/logind.conf.5.en) (`KillUserProcesses=`),
[XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/).

---

## 2. Canonical mechanisms, what each guarantees, and upstream URLs

### 2.1 `Linger=` / `loginctl enable-linger`

> *"If enabled for a specific user, a user manager is spawned for the user at boot and kept around
> after logouts. This allows users who are not logged in to run long-running services."*
> — [`loginctl(1)`](https://man.archlinux.org/man/loginctl.1.en), `enable-linger`

**Guarantee**: the user manager (`user@UID.service`) exists and stays alive **with no session**.
Everything started as a user unit therefore has a lifetime independent of any login. This is the
foundation of every other mechanism here.
**Measured on this host**: `loginctl show-user nick` → `Linger=yes`; the linger state file
`/var/lib/systemd/linger/nick` exists (0 bytes, 2026-09-13 12:16, p10); `user@1000.service` has been
`active (running) since Wed 2026-07-15 21:44:22 CST; 2 months 0 days ago` — it has **never
restarted** since boot (p6/p10).

### 2.2 `systemd --user` units, `WantedBy=default.target`

> *"Systemd offers the ability to manage services under the user's control with a per-user systemd
> instance … When a systemd user instance starts, it brings up the per user target
> `default.target`."* — [ArchWiki: systemd/User](https://wiki.archlinux.org/title/Systemd/User)
> (secondary, but the mechanism it describes is the documented unit-search behaviour of
> `systemd.unit(5)`).

> *"`user@UID.service` is accompanied by the system unit `user-runtime-dir@UID.service`, which
> creates the user's runtime directory `/run/user/UID` when started, and removes it when it is
> stopped."* … *"Options that control resources available to logged-in users … Both
> `user@UID.service` and the scope units are collected under the `user-UID.slice`."*
> — [`user@.service(5)`](https://man.archlinux.org/man/user@.service.5.en)

**Guarantee**: a unit started by the user manager is a **child of `user@UID.service`**, i.e. its
cgroup is `user-UID.slice/user@UID.service/...`, not `user-UID.slice/session-N.scope`. Session
teardown cannot reach it. This is the property gopi needs.
**Measured on this host**: `/lib/systemd/system/user@.service` (verbatim, p10) carries
`Delegate=pids memory`, `Requires=user-runtime-dir@%i.service`, `After=… user-runtime-dir@%i.service
dbus.service`, `KillMode=mixed`; the manager's subtree exposes exactly `memory pids`
(`cgroup.controllers` / `cgroup.subtree_control`, p10). `user-runtime-dir@1000.service` is
`ActiveState=active` / `SubState=exited` / `Result=success` (oneshot, `RemainAfterExit=yes`), i.e. it
**ran once at boot and has not been re-run** (p7). Its unit file (verbatim, p7) is
`ExecStart=/lib/systemd/systemd-user-runtime-dir start %i` / `StopWhenUnneeded=yes`.

### 2.3 Transient scopes: `systemd-run --user --scope` (and `--system --scope`)

> *"If a command is run as transient scope unit, it will be executed by `systemd-run` itself as
> parent process and will thus inherit the execution environment of the caller. However, the
> processes of the command are managed by the service manager similarly to normal services … This
> mode is enabled via the `--scope` switch."* — [`systemd-run(1)`](https://man.archlinux.org/man/systemd-run.1.en)

> *"Scope units are not configured via unit configuration files, but are only created
> programmatically using the bus interfaces of systemd."* … *"The lifecycle of the scope unit is thus
> not bound to the lifetime of one specific process, but to the existence of at least one process in
> the scope."* — [`systemd.scope(5)`](https://man.archlinux.org/man/systemd.scope.5.en)

**Guarantee**: `systemd-run --scope` creates a real unit; with `--user` the scope is created by the
**user manager**, so the processes sit under `user@UID.service` and survive logout. With `--system`
it is created by PID 1 and survives logout too — but as an unprivileged user that path is
polkit-gated.
**Measured on this host** (p3, re-run in p10):
```
rung1 systemd-run --user --scope : Failed to connect to bus: No such file or directory
rung1b explicit DBUS_SESSION_BUS_ADDRESS : Failed to connect to bus: No such file or directory
rung  systemd-run --scope (SYSTEM bus)    : Failed to start transient scope unit: Interactive authentication required.
```

### 2.4 `KillUserProcesses=` and session-scope abandonment

> *"Configures whether the processes of a user should be killed when the user logs out. If `true`, the
> scope unit corresponding to the session and all processes inside that scope will be terminated. If
> `false`, the scope is "abandoned", see `systemd.scope(5)`, and processes are not killed. Defaults to
> "no".* … *"Note that setting `KillUserProcesses=yes` will break tools like `screen(1)` and
> `tmux(1)`, unless they are moved out of the session scope."*
> — [`logind.conf(5)`](https://man.archlinux.org/man/logind.conf.5.en)

**Guarantee**: with `no` (the compiled default, and this host's effective value: `/etc/systemd/logind.conf`
contains only `[Login]`, no drop-ins — p10), a session's scope is *abandoned* at logout rather than
purged, so ordinary processes can linger. This is a **weak** guarantee: it preserves processes,
not their session-scoped dependencies, and the upstream advice for any session-bound tool is still
"move it out of the session scope".

### 2.5 `KillMode=` / `Delegate=` semantics (why the cgroup decision matters)

> *"`KillMode=` … If set to `control-group`, all remaining processes in the control group of this unit
> will be killed on unit stop."* — [`systemd.kill(5)`](https://man.archlinux.org/man/systemd.kill.5.en)

> *"The root control group may only be written to by systemd (PID 1). Services that create and
> manipulate control groups in the top level cgroup are in direct conflict with the kernel's
> requirement that each control group should have a single-writer only. Services must set
> `Delegate=yes` for the units they intend to manage subcgroups of. If they create and manipulate
> cgroups outside of units that have `Delegate=yes` set, they violate the access contract for control
> groups."* — [systemd.io: New Control Group Interfaces](https://systemd.io/CONTROL_GROUP_INTERFACE/)

**Guarantee / constraint**: cgroup membership is the *actual* kill boundary — not the session, not
the terminal. This is why gopi's ladder exists, and it is also why the cgroupfs route (§4.2) is an
**access-contract violation** unless the subtree is delegated (upstream delegates `Delegate=yes`
generically for user managers; on this host only `pids memory` are delegated — measured, p10).
Kernel-side background: [Control Group v2](https://docs.kernel.org/admin-guide/cgroup-v2.html).

### 2.6 `$XDG_RUNTIME_DIR` — and the rule this host violated

> *"`$XDG_RUNTIME_DIR` defines the base directory relative to which user-specific non-essential
> runtime files and other file objects (such as sockets, named pipes, ...) should be stored. The
> directory MUST be owned by the user … Its Unix access mode MUST be 0700."* …
> *"The lifetime of the directory MUST be bound to the user being logged in. It MUST be created when
> the user first logs in and if the user fully logs out the directory MUST be removed."* …
> *"**Files in this directory MAY be subjected to periodic clean-up.** To ensure that your files are
> not removed, they should have their access time timestamp modified at least once every 6 hours of
> monotonic time or the 'sticky' bit should be set on the file."*
> — [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/)

**Guarantee**: none against deletion — by design. The runtime dir is a *cleaning-permitted*
namespace. A long-lived daemon must therefore not depend on a file it did not itself re-create,
and a manager's socket is not "safe" merely because the manager is alive.

---

## 3. For this host

### 3.1 The observable symptom (verbatim)

`p3-mechanisms.sh` / `p7-rootcause-pin.sh`:
```
=== systemd-run --user --scope (the declarative escape) ===
Failed to connect to bus: No such file or directory
exit=1

=== systemctl --user status ===
Failed to connect to bus: No such file or directory
exit=0

=== busctl --user list ===
Failed to connect to bus: No such file or directory
exit=0
```
with `DBUS_SESSION_BUS_ADDRESS` unset in the agent shell (`p1`), while `dbus-send --session` *does*
work (p2: `method return time=… sender=org.freedesktop.DBus`) — because the client uses **X11
autolaunch** (X `DISPLAY=:1` + Xauthority is available), not `/run/user/1000/bus`. Two different
buses, one of them a private X-autolaunched session.

### 3.2 What it is NOT (each excluded by measurement)

| Hypothesis | Probe | Result |
|---|---|---|
| `KillUserProcesses` / logind misconfig | `/etc/systemd/logind.conf` (p10) | only `[Login]`, no drop-ins → compiled default `no`; `EnableWallMessages`/`KillUserProcesses` unset |
| linger missing (the 2026-09-11 devdoc reading) | `loginctl show-user nick` (p1/p10) | `Linger=yes`, `/var/lib/systemd/linger/nick` present |
| manager not running | `systemctl status user@1000.service` (p6) | `active (running) … 2 months … Main PID: 2321` |
| wrong runtime dir / permissions | `stat /run/user/1000`, `mountinfo` (p5) | tmpfs `mode=700,uid=1000,gid=1000`, owned by `nick` — spec-correct |
| a private mount namespace on the manager | `readlink /proc/{2321,2342,self}/ns/mnt` (p5) | all three = `mnt:[4026531841]` — **same** namespace |
| the gvfs tmpfiles rule removing it | `/usr/lib/tmpfiles.d/*` (p10/p6) | only `x /run/user/*/gvfs` and `d /run/user 0755 root root -`; no age rule touching sockets |
| the operator's GC script removing it | `gopi-home-gc.sh` (p8), `grep` for `rm` on the runtime dir | one `rm -f -- "$target"` on a *relocation* target; no runtime-dir sweep |

### 3.3 Root cause (measured): the socket **paths were unlinked** while the manager kept them bound

`p7-rootcause-pin.sh`:
```
=== 1. inode/ctime/links of the runtime dir and systemd/ ===
ino=1      links=53 ctime=2026-09-14 22:39:10.643058632 +0800 mtime=… /run/user/1000
ino=107320 links=3  ctime=2026-09-15 07:26:18.870262117 +0800 mtime=… /run/user/1000/systemd

=== 2. the two sockets: kernel state vs VFS ===
0000000000000000: 00000002 00000000 00010000 0001 01 39144 /run/user/1000/systemd/private
0000000000000000: 00000002 00000000 00010000 0001 01 40136 /run/user/1000/bus
stat: cannot statx '/run/user/1000/bus': No such file or directory
stat: cannot statx '/run/user/1000/systemd/private': No such file or directory

=== G. holders ===
lrwx------ 1 nick nick 64 … 19 -> socket:[39144]     # user manager 2321
lrwx------ 1 nick nick 64 … 27 -> socket:[40136]     # user manager 2321
lrwx------ 1 nick nick 64 Sep 12 04:15 3 -> socket:[40136]   # dbus-daemon 2342

=== sanity: does ANY process's filesystem view carry the path? ===
visible_in_any_process=0
```
`p12-final-facts.sh` adds the second face of the same truth — the *listeners* are alive and the
directory holds nothing:
```
bound paths in /proc/net/unix : 57          (incl. 40136 = /run/user/1000/bus,
                                             39144 = /run/user/1000/systemd/private,
                                             + 8 connected entries on the bus)
entries ON DISK at /run/user/1000/systemd : inaccessible
VFS stat of $XDG_RUNTIME_DIR/bus : No such file or directory
```
and `dbus-daemon 2342` still advertises the dead path in its own environment:
`DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus` (p9). Consequently
`connect()` → `ENOENT` for both paths (p2), while the *endpoints* exist in the kernel.

**Control user** — the same host, a different uid, untouched (p12):
```
uid 117 (lightdm): /run/user/117/systemd/notify, /run/user/117/systemd/private,
                   /run/user/117/bus (+ gnupg/pipewire/pulse/at-spi sockets) — all bound
uid 1000:          only systemd/private and bus in the kernel; nothing on disk
```
⇒ uid 1000's runtime dir was **specifically cleaned**, not a host-wide policy. (The VFS half of the
uid-117 check needs a privileged read — see Not Done §8.3.)

**Actor**: **narrowed, not proven.** The two candidate moments are bracketed by mtimes
(`/run/user/1000` 2026-09-14 22:39:10; `/run/user/1000/systemd` 2026-09-15 07:26:18) but both
journal windows are empty of any cleaning unit (p6/p11), no tmpfiles rule with an age touches
`/run/user` (`/usr/lib/tmpfiles.d/systemd.conf` has only `d /run/user 0755 root root -`, and the
gvfs rule is `x /run/user/*/gvfs` — p13), no such timer/cron entry exists, and
`/etc/tmpfiles.d/gopi-residue.conf` — which **did** exist when it was invoked as root on 2026-09-13
09:57 (`sudo … systemd-tmpfiles --clean /etc/tmpfiles.d/gopi-residue.conf`) — is **absent now**
(p6/p10/p13), so that instrument cannot be re-read. No process holds a cwd/fd directly under
`/run/user/1000` (p11, p14).

What the evidence DOES establish:
- **gopi's own root lists never include the runtime dir** (measured, p13/p14): `worktreeroots.ShmDir`
  = `/dev/shm`, `AmbientTempRoot` = `/tmp`, and neither the scratch GC nor the legacy-residue sweep
  (namespace: `gopi-*` + `suite-tmp-*`, **directories only**) can name `bus` or `systemd/private`.
  So no *default* gopi sweep explains it.
- **A privileged blank-sudo probe family exists and runs constantly**: 36 journal lines of
  `sudo[nnn]: nick : PWD=/home/user/workspace/gopi-<task>/internal/tools ; USER=root ;
  COMMAND=/usr/bin/true` on 2026-09-14 (p14) — these are gopi's own sudo-availability *tests*
  (the command is `/usr/bin/true`), i.e. **evidence, not the actor**: they change nothing.
- **The XDG-copy pattern is demonstrably in use on this host** (p14): a live abandoned scope
  (`session-18661`) holds `gvfsd-fuse /tmp/tabpoc/rt/gvfs` and
  `fusermount3 … /tmp/tabpoc/rt/doc` — i.e. a run with `XDG_RUNTIME_DIR=/tmp/tabpoc/rt`, and the
  login shell's own runtime-dir listing still carries that run's leftovers (`mv-run*.sh`).

⇒ The failure mode and the fix are *certain*; the identity of the sweep that took the two paths is
*not*. The dispatchable leads, in order: (1) `GOPI_TEST_TMPDIR`/`GOPI_TEST_SCRATCH_HOME`/`TMPDIR`
pointed at `/run/user/1000` by any run, whose own root cleanup (`os.RemoveAll(root)` in
`internal/testutil/scratchroot.go`) would remove directories there; (2) a renamed/unknown-namespace
sweep over the memory base. Both are §7 checklist item 6 — not a blocker for the daemon decision.

### 3.4 The identifying diagnostic (works on any host) and the exact fix

**Diagnostic — "absent path" vs "unlinked socket" vs "no manager"** (all unprivileged):
```sh
# 1. is the manager there at all?
systemctl is-active user@$(id -u).service
# 2. does the path exist in the VFS?
stat -c '%F' "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus" 2>&1
# 3. does the KERNEL have it bound? (a bound AF_UNIX path is listed even after unlink)
grep -F "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}" /proc/net/unix
# 4. can a client reach it?
python3 - <<'PY'
import socket,os,errno
p=f"/run/user/{os.getuid()}/bus"
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
try: s.connect(p); print("connect OK")
except OSError as e: print("connect",errno.errorcode.get(e.errno),e.strerror)
PY
```
Interpretation: (2) missing + (3) present + `ENOENT` ⇒ **unlinked socket, manager alive** (this
host). (2) missing + (3) missing + manager inactive ⇒ no manager (enable linger / log in).
(2) present + `ECONNREFUSED` ⇒ stale *file* node, no listener.

**Fix (operator/root-gated; the `stop` removes `/run/user/UID`, the `start` recreates it and the
manager re-binds both sockets — per `user@.service(5)`: *"creates the user's runtime directory
`/run/user/UID` when started, and removes it when it is stopped"*):**
```sh
# OPERATOR-REQUIRED (root). Cost measured on this host: the live user@1000.service tree holds the
# desktop session (XFCE on :1, 216 tasks) — a restart terminates the runtime graph.
sudo systemctl restart user-runtime-dir@1000.service user@1000.service   # or: sudo reboot
# then, unprivileged, verify:
systemctl --user is-system-running && ls -l "${XDG_RUNTIME_DIR}/bus" "${XDG_RUNTIME_DIR}/systemd/private"
```
Measured on this host: the restart is **not** available to uid 1000 without authentication —
`pkcheck --action-id org.freedesktop.systemd1.manage-units --process 2321` → *"Authorization requires
authentication and -u wasn't passed"* exit 2 (p12); `systemd-run --scope` on the system bus →
*"Interactive authentication required"* (p3). (Conversely, `org.freedesktop.login1.set-self-linger`
**is** granted to this caller — exit 0, p12 — so linger could be toggled without root; it is already
enabled.) Reported as a **privilege finding**, not as a remedy: no privilege is what makes the daemon
run; a privilege is what a *host repair* costs, and that boundary is stated plainly (Rule 25).
A restart-free alternative exists in principle (a client that re-`bind()`s the address — measured to
succeed, p4) but it would create a *third* socket that the running manager does not own; it is
recorded as an experiment, **not** a recommendation.

---

## 4. Mechanism → guarantee → needs root? → works here

`works here` is **measured** on this host (Verbatim probe refs in parentheses); `?` means untestable
in this session. Hover-free legend: **Y** = measured to work, **N** = measured to fail,
**N (harmful)** = it runs but protects nothing.

| # | Mechanism | Guarantee it actually provides | Needs root? | Works here | Evidence |
|---|---|---|---|---|---|
| 1 | **linger + `user@UID.service` unit** (`WantedBy=default.target`) | lifetime = manager's lifetime; cgroup `user-UID.slice/user@UID.service/...`; survives logout by *membership* | No (linger toggle: polkit `set-self-linger` granted to this caller; unit placement is a user file) | **?** — linger ✔ (`Linger=yes`), manager ✔ (active 2 months), but the unit cannot be enabled/started *now* without the bus | p1, p10, p9 |
| 2 | **`systemd-run --user --scope`** | transient scope created by the user manager; leaves the session scope | No | **N** — `Failed to connect to bus: No such file or directory` | p3, p10 |
| 3 | **`systemd-run --scope` (system bus, transient system scope)** | scope created by PID 1; survives logout; outside `user-UID.slice` entirely | **Yes** — polkit `manage-units` | **N** for uid 1000 — `Interactive authentication required`; re-checked: pkcheck exit 2 | p3, p12 |
| 4 | **cgroupfs self-migration** (`internal/cgroupscope`: `mkdir` + write own pid) | moves the process out of the session scope **iff** the subtree is delegated and the shared ancestor is writable | No uid change needed — but *may not work unprivileged at all* | **N** — `access(W_OK)` on `user-1000.slice` = False; `echo pid > cgroup.procs` → `I/O error`; fresh `/proc/self/cgroup` **unchanged** (`session-18257.scope`) | p3 |
| 5 | **`setsid` only** (gopi's fallback) | new session, no controlling terminal, SIGHUP cleared | No | **N (harmful)** — cgroup unchanged: `setsid child: sid=2863247 cgroup=0::/user.slice/user-1000.slice/session-18257.scope` | p3 |
| 6 | **`nohup` / `disown` / `&`** | SIGHUP disposition / job-table only | No | **N (harmful)** — no cgroup or session effect at all (no mechanism to probe: they only change SIGHUP/hupone) | p3 |
| 7 | **`screen` / `tmux`** | a detachable terminal; **not** a cgroup escape — upstream says so explicitly | No | **N (harmful)** — still in the session scope; `logind.conf(5)`: broken by `KillUserProcesses=yes` "unless they are moved out of the session scope" | citation §2.4 |
| 8 | **writing `cgroup.procs` upward** | (attempted) cgroup move | No | **N** — kernel gates on the shared ancestor (`user-1000.slice` = `root:root 0755`): `Permission denied`; upstream: violates the single-writer contract unless `Delegate=yes` | p3, §2.5 |
| 9 | **`daemon(3)`** | fork + `setsid` + chdir("/") + redirect stdio | No | **N (harmful)** — identical to (5): no cgroup effect | §2.5 |
| 10 | **`KillUserProcesses=no`** (host policy) | a logout *abandons* the session scope instead of killing it | Yes to *change*; already the effective default here | **Y (weak)** — effective value `no`; that is exactly why 78 processes survive in 5 scopes with no session (see below) | p10, p12 |
| 11 | **`loginctl terminate-session` is what kills** | the SIGTERM source is the scope teardown, not the tty | No | n/a (deliberately NOT run — it would kill the operator's sessions) | p3 |

**Logout-survival evidence (indirect but strong)** — `p9`/`p12`: `loginctl list-sessions` reports
`18200 18205 18252 18257 18855 18893 19166 19205 c1`, while the slice contains live processes in
five scopes that are **not** sessions any more:
```
session-15158=2  session-15164=3  session-15425=3  session-17674=6  session-18199=44  session-18661=20
```
= **78 live processes in six abandoned session scopes** ⇒ on this host a process in a session scope can
outlive its session (`KillUserProcesses=no`), which is why `setsid`-only appears to "work" in
practice while being unprotected the moment someone runs `loginctl terminate-session <ID>` or the
policy changes.

---

## 5. Verification recipe — proving logout survival

Run as the user, from the shell whose session will end (values as constants; `PID` is the daemon's):

```sh
# 0. preconditions (all must answer; the recipe is meaningless otherwise)
systemctl is-active  "user@$(id -u).service"        # active
loginctl show-user "$(id -un)" -p Linger            # Linger=yes
test -S "${XDG_RUNTIME_DIR}/bus"                    # the user bus must exist (§3.4 diagnostic)

# 1. start the daemon as a USER UNIT (the only mechanism that yields membership, not signals)
systemctl --user daemon-reload
systemctl --user enable --now mydaemon.service
PID="$(systemctl --user show -p MainPID --value mydaemon.service)"

# 2. ASSERT cgroup membership — the decisive check (NOT PPID, NOT setsid, PROJECT.md Rule 24)
CGROUP="$(cat "/proc/$PID/cgroup")"
echo "$CGROUP"
case "$CGROUP" in *"user@$(id -u).service"*) echo "PASS: under the user manager";;
  *session-*.scope*) echo "FAIL: still in a login session scope"; exit 1;; esac
grep -q 'session-.*\.scope' "/proc/$PID/cgroup" && { echo "FAIL (session scope)"; exit 1; } || echo "PASS (no session scope)"

# 3. prove the daemon is reachable AFTER logout: close EVERY session of this user
loginctl list-sessions --no-legend | awk -v u="$(id -un)" '$3==u {print $1}'   # inspect first
#    (from another host/console, or after reconnecting:)
#    loginctl terminate-session <ID>      # kills scopes, not units -- the point of the test

# 4. second logout / second reconnect cycle — survival must be repeatable, not a one-off
#    log in again, then:
test -d "/proc/$PID" && echo "PASS: alive after logout #1"
cat "/proc/$PID/cgroup"                       # still user@UID.service, still not a session scope

# 5. the unit's own view (the declarative record an operator reads)
systemctl --user status mydaemon.service --no-pager
systemd-cgls "/user.slice/user-$(id -u).slice/user@$(id -u).service" | head

# 6. clean stop (and the negative control: the same binary under setsid-only)
systemctl --user stop mydaemon.service
setsid -f sleep 300; sleep 0.2; P2=$(pgrep -x sleep | head -1)
cat "/proc/$P2/cgroup"                        # NEGATIVE CONTROL: session-N.scope remains == unprotected
```
Why each step: (2) is the **only** accepted evidence of independence — measuring the cgroup, never
PPID/PGID/setsid (Rule 24, `devdoc/detach-judgment-rule.md`); (3)/(4) exercise the actual teardown and
require it to be repeatable; (6) is the negative control that fails if the mechanism silently
degrades (Rule 15: no silent downgrade).

---

## 6. Anti-patterns — what each fails to protect

| Anti-pattern | What it looks like | What it actually protects | What it does **not** protect |
|---|---|---|---|
| `setsid` alone | `setsid -f cmd` / `SysProcAttr{Setsid:true}` | a new session + no controlling terminal; SIGHUP disposition cleared at `setsid` | **cgroup membership** — the process stays in `session-N.scope` (measured, §4 row 5); dies with `loginctl terminate-session`/`KillUserProcesses=yes` |
| `nohup` | `nohup cmd &` | SIGHUP is ignored | everything else — no session, no cgroup, no reparenting guarantee |
| `disown` / `&` | shell job control | the shell's job table | the shell's own lifetime; a login shell's exit still takes the process's *scope* down |
| `screen` / `tmux` | interactive detach | terminal reuse | cgroup escape — upstream: broken by `KillUserProcesses=yes` "unless they are moved out of the session scope" |
| writing `cgroup.procs` | `echo $pid > …/cgroup.procs` | nothing by itself | the kernel gates on the shared ancestor's write permission; gated `EACCES` on this host; upstream: a single-writer contract violation outside a `Delegate=yes` unit |
| `daemon(3)` | classic double-fork | fork + setsid + chdir("/") + stdio redirect | identical to `setsid` — in cgroup terms it changes nothing |
| `at`/`cron` as a launcher | schedule the daemon | a different parent | not a lifetime mechanism; the job's cgroup is the launcher's, and a `@reboot` cron entry is a *different* contract than a manager-owned unit |
| `supervisor`/`runit`/`s6` as user processes | a supervising tree | restart-on-exit | the supervisor itself is in the session scope unless *it* is a user unit — supervision is not detachment |

---

## 7. Critique of gopi's existing 3-rung ladder

Files: `cmd/agent/cli_daemon_scope.go` (rungs and reporting), `internal/cgroupscope/cgroupscope.go`
(the in-process escape). The ladder is **①** `systemd-run --user --scope` → **②** cgroupfs
self-migration → **③** `setsid`-only with an announced degradation.

**Is it a workaround for a host misconfiguration?** **Yes — rung ② is, and the code itself says so**
(`cgroupscope.go`: *"`systemd-run --user --scope` would be the declarative equivalent but needs that
bus (absent on this host); this is the zero-dependency path"*). The measurement in this research goes
further than the code's premise: on this host rung ② **does not work either** —
`access(W_OK)` on the shared ancestor `user-1000.slice` is `False`, the write fails with `I/O error`,
and a fresh `/proc/self/cgroup` still names `session-18257.scope` (p3). So on this host the ladder
reduces to rung ③, honestly announced. Three consequences:

1. **The ladder is correct about the boundary it protects** (`cgroup membership`, measured not
   assumed) and correct about the honest degradation (rung ③ prints a note; `--require-scope` turns
   convenience into a hard failure). Those should be **kept**.
2. **Rung ②'s mechanism is upstream-discouraged on principle**: `cgroupscope` writes into
   `user@UID.service`'s subtree. Upstream: *"Services must set `Delegate=yes` for the units they
   intend to manage subcgroups of. If they create and manipulate cgroups outside of units that have
   `Delegate=yes` set, they violate the access contract for control groups"*
   ([systemd.io](https://systemd.io/CONTROL_GROUP_INTERFACE/)). On this host the delegation is
   `memory pids` only (measured), which is consistent with the `EACCES` measured above. Rung ② is
   therefore best regarded as a **host-workaround with an upstream access-contract caveat**, valid
   only where a delegated subtree is actually writable.
3. **Nothing should be *deleted* on the strength of this research** — the rungs are the honest
   instrumentation of a broken host, and rung ② may work on a host whose delegation differs. What
   *should* change is the framing (pending the follow-up task): the ladder is a **repair-ordering**
   (fix the host → rung ① returns), not a permanent design; the `no-privilege` note (Rule 25,
   `devdoc/no-privilege-daemon.md`) is right and should stay; and the absent user bus should be
   surfaced as a **host defect with a named diagnostic** (§3.4) rather than as a permanent property
   of the machine. Deletion candidates, if any, are only ever the *redundant* paths after the host
   is repaired — the decision belongs to the implementation task, with the ladder's own tests as
   the guard.

---

## 8. Follow-up implementation checklist (a separate task; nothing here is landed)

1. **Host repair (operator-required, root)**: `sudo systemctl restart user-runtime-dir@1000.service user@1000.service`
   (or a reboot); then re-run the §3.4 diagnostic and confirm `/run/user/1000/bus` + `systemd/private`
   exist, `systemctl --user is-system-running` answers, and `systemd-run --user --scope --quiet /bin/true` exits 0.
2. **Turn rung ① back on for real**: with the bus restored, prove the gopi daemon starts under
   `systemd-run --user --scope` and that `/proc/<pid>/cgroup` names the scope (the existing
   `cmd/agent/daemon_cgroup_e2e_test.go` is the guard) — no code change needed if it passes.
3. **Make the degradation diagnostic actionable**: when *both* rungs fail, report the §3.4 three-way
   distinction (no manager / unlinked socket / stale node) instead of one blended reason, so the
   operator is sent to the host repair rather than to a privilege (Rule 25 boundary stays intact).
4. **Consider a user unit as the first-class deployment**: a `mydaemon.service` template +
   `enable-linger` becomes the documented install path; transient scopes stay for ad-hoc runs.
5. **Keep the runtime dir out of every scratch/tmp root**: assert (test) that no harness sets
   `TMPDIR`/`GOPI_TEST_TMPDIR`/`GOPI_TEST_SCRATCH_HOME` to a path under `$XDG_RUNTIME_DIR`, and that
   no sweep's root resolves there (complements Rule 22/§2.6).
6. **Name the unlinker** (research gap §3.3): instrument the runtime dir (inotify watcher or a
   sticky-bit + atime discipline per the XDG rule) and/or bisect the two candidate mechanisms
   (`legacy_residue` sweep namespace; a test/operator run with the runtime dir as its root).
7. **Regression test for the host defect class**: a diagnostic test that *distinguishes* the three
   bus failure modes and fails loudly on "path absent but socket bound" (the state that silently
   disables rung ①).
8. **Re-verify after the host repair** that the ladder's rung ordering is still the intended one and
   that `--require-scope` behaves (hard failure, not degradation).

---

## 9. Citations (upstream / authoritative)

1. `loginctl(1)` — https://man.archlinux.org/man/loginctl.1.en (upstream `enable-linger`: "a user
   manager is spawned for the user at boot and kept around after logouts")
2. `user@.service(5)` — https://man.archlinux.org/man/user@.service.5.en (`user-runtime-dir@UID.service`
   creates/removes `/run/user/UID`; user manager hierarchy under `user-UID.slice`)
3. `logind.conf(5)` — https://man.archlinux.org/man/logind.conf.5.en (`KillUserProcesses=`, screen/tmux caveat)
4. `systemd-run(1)` — https://man.archlinux.org/man/systemd-run.1.en (`--scope`, `--user`, transient units)
5. `systemd.scope(5)` — https://man.archlinux.org/man/systemd.scope.5.en (scopes are bus-created; lifecycle bound to members)
6. `systemd.kill(5)` — https://man.archlinux.org/man/systemd.kill.5.en (`KillMode=control-group` kills the whole cgroup)
7. `systemd.special(7)` — https://man.archlinux.org/man/systemd.special.7.en (`default.target`, `user.slice`)
8. systemd.io — New Control Group Interfaces — https://systemd.io/CONTROL_GROUP_INTERFACE/ (single-writer contract, `Delegate=yes`)
9. Linux kernel — Control Group v2 — https://docs.kernel.org/admin-guide/cgroup-v2.html (migration/delegation semantics)
10. XDG Base Directory Specification — https://specifications.freedesktop.org/basedir-spec/latest/
    (`$XDG_RUNTIME_DIR` MUST be 0700, bound to login, **"files … MAY be subjected to periodic clean-up"**)
11. D-Bus — `dbus-daemon(1)` — https://dbus.freedesktop.org/doc/dbus-daemon.1.html (the per-login-session bus and its address)
12. *Secondary*: ArchWiki — systemd/User — https://wiki.archlinux.org/title/Systemd/User
    (practical `WantedBy=default.target` / `~/.config/systemd/user` layout; marked secondary)

Host files quoted as primary evidence (not external sources): `/lib/systemd/system/user@.service`,
`/lib/systemd/system/user-runtime-dir@.service`, `/usr/lib/systemd/user/dbus.socket`,
`/etc/systemd/logind.conf` — all quoted verbatim in `devdoc/daemon-survives-logout-probes/p{7,10}.out`.

---

## 10. Verbatim probe transcripts

Full outputs (and the scripts that produced them) are committed in
`devdoc/daemon-survives-logout-probes/`:

| Probe | Script | Covers |
|---|---|---|
| p1 | `p1-identity-logind.sh` | host identity, `loginctl show-user` (`Linger=yes`), sessions, initial devdoc readings |
| p2 | `p2-userbus.sh` | the absent paths, `ss -xl`, `/proc/net/unix`, manager view, `connect()` → ENOENT, dbus clients |
| p3 | `p3-mechanisms.sh` | every rung's exact failure + the cgroupfs probes (mkdir OK / write EACCES) + `setsid` baseline + sudo presence |
| p4 | `p4-forensics.sh` | unlinked-socket ownership, mtimes, tmpfiles search, live gopi cgroups |
| p5 | `p5-namespace-rootcause.sh` | the manager's own view, shared mount namespace, unit files, runtime-dir listing |
| p6 | `p6-deletion-forensics.sh` | mtime stability samples, the earlier probe scripts' own text, harness scratch scripts, manager status |
| p7 | `p7-rootcause-pin.sh` | inode/ctime/links, kernel-vs-VFS pairing, `systemd/` subtree, runtime-dir unit file |
| p8 | `p8-gc-candidate.sh` | the GC script's `rm` sites, scratch-root resolution, residue inventory, cron entry |
| p9 | `p9-repair-feasibility.sh` | dbus unit states, dbus-daemon identity, pkcheck (restart gating), session-scope inventory |
| p10 | `p10-mechanism-baseline.sh` | `user@.service` verbatim, delegation, linger file, `logind.conf`, every rung re-run |
| p11 | `p11-discriminators.sh` | **uid 117 control** (sockets bound), empty journal windows, no unlinker process |
| p12 | `p12-final-facts.sh` | per-user properties, polkit verdicts, listener/path pairing, abandoned-scope process counts |
| p13 | `p13-tmpfiles-and-citations.sh` | tmpfiles inventory + `gopi-residue.conf` absence, host quotes re-read, abandoned-scope arithmetic (78), **citation HTTP status for all 12 URLs** |
| p14 | `p14-residue-reap-actor.sh` | the actor lead: `sudo … /usr/bin/true` probe family, the two unlink windows, the `XDG_RUNTIME_DIR=/tmp/tabpoc/rt` evidence, per-scope process lists |

Key excerpts are quoted inline above (§3.1–§3.3, §4). Every quoted line is copied from the committed
`.out` file for its probe. All 12 citation URLs answered HTTP 200 (`p13.out`, section D).

---

## 11. Not Done / limits (honest)

1. **No fix landed, no code changed** — by task constraint (research only). Every item in §8 is a
   proposal for a separate task; the ladder's files are untouched.
2. **The exact unlinker of the two socket paths is not identified.** The *state* is proven
   (kernel-bound, VFS-absent, same mount namespace, uid-1000-only); the *actor* is narrowed but not
   proven. Ruled out by measurement: tmpfiles (no age rule touching `/run/user`; the residue conf is
   gone), cron/timers, mount namespaces, and gopi's default root lists (`/dev/shm`, `/tmp`, scratch
   home — none is the runtime dir). The evidence points at a run whose **root was the runtime dir**
   (the same-host `/tmp/tabpoc/rt` pattern proves the practice exists), and at gopi's own
   `os.RemoveAll(root)` cleanup when `GOPI_TEST_TMPDIR` names such a root. §7 item 6 is the follow-up
   (instrument the directory, or bisect the two mechanisms).
3. **The privileged control read was not run.** The decisive VFS check for the control user
   (`sudo -n ls -la /run/user/117/systemd/`, `/run/user/117` is `0700 lightdm:lightdm`) is blocked by
   the harness (a `sudo` token in a bash command is refused). The uid-117 conclusion therefore rests
   on `/proc/net/unix` (kernel state, unprivileged — it lists `systemd/private`, `bus`, `notify`
   bound) plus the uid-1000 `sudo`-free evidence. Operator command if the VFS half matters:
   `sudo ls -la /run/user/117/systemd/`.
4. **Logout survival was not exercised end-to-end** (a real logout would terminate the operator's
   sessions; `loginctl terminate-user nick` / `terminate-session` were deliberately not run). The §5
   recipe is written to be run on a host/console where that is acceptable; the indirect evidence
   (78 live processes in 6 abandoned scopes; `KillUserProcesses` effective `no`) is recorded and
   labelled as evidence, not as the live test.
5. **Mechanism 1 (linger + user unit) is marked `?`, not `Y`, on purpose**: linger and the manager
   are measured, but the unit cannot be *enabled/started now* without the user bus, and installing a
   long-lived unit was out of scope for a read-only research task. Its boot-path behaviour is
   upstream-documented (`user@.service(5)`, `systemd.special(7)`), not measured here.
6. **Host repair was not applied** (would kill the operator's desktop/session processes: the
   `user@1000.service` tree holds the XFCE session on `:1`, 216 tasks measured). Its gating
   (`pkcheck` → "requires authentication") and its mechanism are recorded, not exercised.
7. **Cgroupfs rung ② was probed with a transient `mkdir`/write and cleaned up immediately** — no
   lasting cgroup node was left behind (verified by `rmdir` exit 0 in `p3`).
8. **Web-search coverage is partial**: a `google` method search was unavailable (egress CAPTCHA) and
   a GitHub search returned nothing useful, so the citation list leans on the man pages and
   specifications I could retrieve directly; the two secondary sources are marked as such.

---

## 12. Deliverable location / recovery

- Branch: **`research/daemon-survives-logout`** (shared repo — the worktree already commits to it; no push needed)
- This document: `git show research/daemon-survives-logout:devdoc/daemon-survives-logout-research.md`
- Probes: `git show research/daemon-survives-logout:devdoc/daemon-survives-logout-probes/p12.out`
- Task report: `devdoc/daemon-survives-logout-research-report.md` on the same branch
- Build check: `go build ./...` → exit 0 (doc-only change; no production code touched)
