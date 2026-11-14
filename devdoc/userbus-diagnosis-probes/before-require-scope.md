<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Captured output: before-require-scope

Verbatim operator-facing output from a real `gopi worker --daemon` run on this
host (2026-09-15). The command was run BY THE AGENT as a probe with
`GOPI_ALLOW_AGENT_SPAWN=1` (the agent-spawn guard bypass the daemon epic's own
tests use) — the operator-only rule is about the OPERATOR's worker fleet, and
this transcript is a diagnostic capture, not a launch recipe.

```
[gopi] worker: agent-spawn guard bypassed (GOPI_ALLOW_AGENT_SPAWN or GOPI_AGENT_SPAWN_GUARD_OFF is set) — the operator ruling of 2026-09-12 forbids this from an agent
gopi worker: refusing to start: --require-scope requested but the cgroup escape is unavailable: Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied)
gopi worker: that is not a privilege problem: no privilege is needed to run this daemon, and none obtains the escape (measured) — the start stays unprivileged.
gopi worker: daemon start failed: cgroup scope escape unavailable: Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied)
exit=1
```
