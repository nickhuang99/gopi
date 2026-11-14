<!-- doc-class: historical -->
# Daemon spawn profile guard

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: `01a1076a` (2026-10-04/05). One half of a two-root-cause incident: the
**production side** — the half that was still live.

## The incident, in one paragraph

A `go test -run TestUnboundDaemonSurvivesItsSpawner ./cmd/agent/` left a REAL
resident `gopi worker --daemon` (pid 639670, `ppid=1`) holding the operator's own
`~/.gopi/worker.lock` — the file's content is STILL `639670` (measured
2026-10-05). Its environment was the operator's `GOPI_SETTINGS`/`GOPI_HOME`, and
on a host with no systemd user bus the detach degrades to **setsid-only**, so
nothing binds the daemon's lifetime to a live process. It was an orphan with
nobody's name on it.

## Two root causes — only one is addressed here

**A. TEST SIDE (upstream, already fixed).** The test spawner seam inherited
`os.Environ()` verbatim; a child with no explicit `GOPI_HOME`/`GOPI_SETTINGS`
fell through to `HOME=/home/user` ⇒ `config.GopiDir() = ~/.gopi`. Every spawn
site now pins a private profile (`testutil.PinSuiteGopiProfile`, asserted by
`TestSuiteGopiProfileIsSandboxed` in `cmd/agent/testmain_test.go`). For a TEST's
own environment the right fix is pinning, not a guard — a test may legitimately
name whatever profile it likes.

**B. PRODUCTION SIDE (this file — the fix here).** The daemon face took its
flock on **whatever profile it resolved**, with no question asked. The flock is
acquired on the production start path in `daemonSpawn` step ③, BEFORE any
agent-spawn guard, and a daemon that started under a test then had **no owner to
die with**. That is the defect: a spawn whose profile nobody named can inherit
the operator's, take their lock, and outlive everything.

## The fix

`cli_daemon_profile_guard.go` + one call in `cli_daemon_spawn.go`.

`daemonSpawn` runs `guardDaemonProfile(spec)` between step ① (argv validation)
and step ③ (the control flock) — **before anything is opened, locked or
detached**. A refused spawn therefore creates no lock, no directory, no process.

### The predicate — and why it is NOT an environment-shape test

The guard asks ONE question the incident turned on:

> **Was the profile NAMED, or did it fall through to the operator's real one?**

Concretely: if `GOPI_HOME` is set (non-empty), the profile was named → **allow**.
If it is absent, the daemon would resolve `<HOME>/.gopi` → **refuse**.

This is deliberately NOT "reject GOPI_HOME shaped like a sandbox" or "reject
when GOPI_HOME is absent in general". Both of those would be wrong here, and the
repository already warns about the second: `cmd/agent/testmain_test.go` says in
as many words that refusing on an env signature would break the whole suite —
every legitimate agent session and every sandbox carries `GOPI_HOME`/
`GOPI_MEMORY_DB` by construction, and those MUST keep working. The guard refuses
exactly the one shape that has no owner: **an inherited environment, not a
decision.**

### Remedy (Rule 15 — a refusal names its correction)

The refusal prints the two ways out: name the profile explicitly
(`GOPI_HOME=<dir> gopi worker --daemon`), or start it in a sandbox
(`gopi sandbox run --label build --task-id <id> -- gopi worker --daemon`). It
carries its own exit code, **3** (`daemonProfileRefusalExitCode`) — the same
"refusal is its own outcome" convention `config.AgentGuardRefusalExitCode`
established — so a caller can tell "the guard said no" from "the start broke".
`daemonFail` honours it via an `ExitCode() int` interface; every other failure
keeps the generic 1.

### The test-only bypass (why it is not an escape hatch)

`GOPI_DAEMON_PROFILE_GUARD_OFF=1` disables the guard. It exists for the two
legitimate reasons `GOPI_AGENT_SPAWN_GUARD_OFF` documents (a subprocess harness
driving the real binary from an inherited environment; a bootstrap whose
environment it cannot control) **and** for the regression test's red proof. It
is announced loudly when set — never a silent downgrade. It is not a route for
a real daemon to skip the question.

## What this deliberately does NOT do

- It does **not** change the FOREGROUND `gopi worker` (no `--daemon`): that face
  takes no lock and holds no daemon lifespan, and must stay usable for debugging.
- It does **not** claim to tell a sandbox from a real profile by path (it
  cannot: a sandbox at `<home>/.gopi` would be indistinguishable).
- It does **not** touch the running binary's semantics for a STARTED daemon: an
  explicitly named profile behaves exactly as before
  (`TestDaemonSpawnRefusalAllowsANamedProfile`).
- It does **not** remove the 639670 residue (the stale `~/.gopi/worker.lock`):
  that is a separate, operator-owned cleanup.

## Honest limits (Rule 26 — an incomplete fix says so)

- The guard closes the **untended-spawn** door. It does not re-validate a
  daemon's profile AFTER the fork (root cause B's part (1)): a daemon that
  started legally under a profile keeps running under it. That re-validation is
  a separate change and is not in this branch.
- The setsid-only degradation on a host without a systemd user bus (root cause
  B's part (2)) is untouched — the daemon's lifetime is still not bound to a
  live process there. The guard prevents the *untended* daemon from starting;
  it does not give a *legitimately started* one a lifetime owner.
- 639670 is dead (measured: `/proc/639670` absent, `ps -p 639670` empty), so no
  live `/proc/<pid>/environ` + fd capture of it exists; the surviving evidence
  is the lock file's content (`639670`) and the incident's own report.

## Verification

See `## Verification` in the task report and
`devdoc/` sibling evidence under the session directory. Build: `go build
./cmd/agent/` OK. Tests: `go test ./cmd/agent/ -run
'TestSuiteGopiProfile|SpawnRefusal'` all PASS. Ran: both shapes of
`gopi worker --daemon` against a freshly built binary in an isolated HOME (red:
guard off spawns a real daemon holding a lock; green: guard on refuses with exit
3 and creates nothing).
