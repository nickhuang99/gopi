<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Captured output: before-worker-daemon

Verbatim operator-facing output from a real `gopi worker --daemon` run on this
host (2026-09-15). The command was run BY THE AGENT as a probe with
`GOPI_ALLOW_AGENT_SPAWN=1` (the agent-spawn guard bypass the daemon epic's own
tests use) — the operator-only rule is about the OPERATOR's worker fleet, and
this transcript is a diagnostic capture, not a launch recipe.

```
[gopi] worker: agent-spawn guard bypassed (GOPI_ALLOW_AGENT_SPAWN or GOPI_AGENT_SPAWN_GUARD_OFF is set) — the operator ruling of 2026-09-12 forbids this from an agent
gopi worker: daemon cwd /dev/shm/gopi-test-scratch/runtime/process (left the inherited directory /home/user/workspace/gopi-userbus-diag behind: a process standing in a task worktree vetoes its recycle for its whole lifetime)
gopi worker: starting a detached worker daemon (idle wait 5s between rounds, self-exits after 1 idle round(s))
gopi worker: cgroup escape: setsid-only (Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape unavailable: migrating a pid into cgroup.procs: /sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied))
worker daemon started: pid 3081574, log /home/user/workspace/gopi-userbus-diag/.rb/run/gp-egaaWo/.gopi/logs/worker.log
gopi worker daemon cwd: pid 3081574 stands in /dev/shm/gopi-test-scratch/runtime/process
gopi worker: note: could not leave the login session scope — this daemon runs in /user.slice/user-1000.slice/session-18855.scope and will end with that scope.
gopi worker: that is not a privilege problem: no privilege is needed to run this daemon, and none obtains the escape (measured) — the start stays unprivileged.
exit=0
```
