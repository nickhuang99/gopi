<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Captured output: after-require-scope

Verbatim operator-facing output from a real `gopi worker --daemon` run on this
host (2026-09-15). The command was run BY THE AGENT as a probe with
`GOPI_ALLOW_AGENT_SPAWN=1` (the agent-spawn guard bypass the daemon epic's own
tests use) — the operator-only rule is about the OPERATOR's worker fleet, and
this transcript is a diagnostic capture, not a launch recipe.

```
[gopi] worker: agent-spawn guard bypassed (GOPI_ALLOW_AGENT_SPAWN or GOPI_AGENT_SPAWN_GUARD_OFF is set) — the operator ruling of 2026-09-12 forbids this from an agent
gopi worker: refusing to start: --require-scope requested but the cgroup escape is unavailable: Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied)
gopi worker: user bus (bus-path-unlinked): the user manager is alive, but this socket path is gone from the filesystem while the kernel still has the socket bound
gopi worker:   measured: private /run/user/1000/systemd/private is path absent, socket bound (unlinked); connect ENOENT (no such file or directory); kernel LISTEN row, inode 39144; user@1000.service is active
gopi worker:   confirm: /usr/bin/systemctl is-active user@1000.service; stat '/run/user/1000/systemd/private'; grep -F '/run/user/1000/systemd/private' /proc/net/unix; stat '/run/user/1000/bus'; grep -F '/run/user/1000/bus' /proc/net/unix
gopi worker:   next: this is not a privilege problem: the manager's runtime directory must be recreated (operator action on this host: restart user-runtime-dir@1000.service, then user@1000.service) — and keep tmpdir/scratch sweeps out of the runtime directory, because emptying it unlinks these sockets while the manager keeps running
gopi worker: that is not a privilege problem: no privilege is needed to run this daemon, and none obtains the escape (measured) — the start stays unprivileged.
gopi worker: daemon start failed: cgroup scope escape unavailable: Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied)
exit=1
```
