<!-- doc-class: historical -->
# Devdoc: "did it escape the login session scope?" — the criterion is the OWNING unit, not the depth

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Branch:** `task/01a12c1a4aa77b3aa55fd4d0ddb64aad`
**Date:** 2026-10-11
**Epic:** the `GOPI_CGROUP_ESCAPE_OFF` acceptance case of epic daemon-cgroup-detach
(`cmd/agent/daemon_cgroup_e2e_test.go:389`,
`TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation`)

---

## The symptom

With the escape disarmed the daemon degrades exactly as documented
(`setsid-only`, mode stamp written, announcement printed) and stays where its
launcher lives. The acceptance case then failed on its own premise:

```
daemon_cgroup_e2e_test.go:390: with the escape disarmed the daemon must stay in its
session scope, got "/user.slice/user-1000.slice/user@1000.service/app.slice/
app-org.gnome.Terminal.slice/vte-spawn-b4078f3c-7005-4309-80b9-9ed8ea10f798.scope"
```

The daemon behaved correctly; the VERDICT about it was wrong.

## Root cause (measured, 5-why)

1. **Symptom** — `cgroupscope.InSessionScope(rel)` returned `false` for a cgroup
   the daemon had genuinely not escaped.
2. **Mechanism** — the predicate accepted a `*.scope` **directly under the
   per-uid slice** and nothing else:

   ```go
   dir := filepath.Base(filepath.Clean(rel))          // vte-spawn-….scope
   parent := filepath.Base(filepath.Dir(...))          // app-org.gnome.Terminal.slice
   return strings.HasSuffix(dir, ".scope") &&
          strings.HasPrefix(parent, "user-") && strings.HasSuffix(parent, ".slice")
   ```

3. **Why the shape did not match** — the operand is this host's real shell
   cgroup, and a DESKTOP session is not shaped like an ssh login:

   ```
   $ cat /proc/self/cgroup
   0::/user.slice/user-1000.slice/user@1000.service/app.slice/
      app-org.gnome.Terminal.slice/vte-spawn-b4078f3c-….scope
   ```

   An ssh login lands in `…/user-1000.slice/session-N.scope` — a *sibling of the
   user manager*, four levels shorter. The desktop path is a scope under
   `app.slice` under the MANAGER, so `parent` is a vendor `.slice` and the
   predicate answered "not a session".
4. **Why the predicate was written that way** — it encoded the shape of the two
   2026-09-13 incident reports (both ssh logins) rather than a criterion. A
   test that answers "in a session scope?" by matching a DEPTH is a model of one
   host's layout, not of cgroup v2's membership rule.
5. **The real rule (cgroup v2)** — a cgroup's processes are members of the
   nearest ANCESTOR unit that carries processes of its own. The terminal scope
   above carries processes; the agent shell's process is therefore a member of
   *that* scope — a scope the GNOME session spawned, whose teardown is the login
   session's teardown. The classification must ask what the nearest owning
   ancestor IS, which is what the fix does.

## The fix — one criterion, one walk

`cgroupscope.InSessionScope` now climbs from the process's own cgroup and asks
one question per level, stopping at the first answer:

| level is … | verdict |
|---|---|
| the per-uid slice (`user-<uid>.slice`) | **not in a session scope** — the boundary every session hangs under |
| a `*.scope` hanging *directly* off the user manager (`user@<uid>.service/init.scope`) | step over it (it is the manager's OWN scope) |
| a `*.scope` named `gopi-daemon-*` (`EscapeTargetPrefix`) | **not in a session scope** — a successful escape's end state |
| any other `*.scope` | **in a session scope** |
| anything else (`*.slice`, `*.service`, the root) | transparent — keep climbing |

`EscapeTargetPrefix` is **exported from `internal/cgroupscope` and consumed by
`cmd/agent`** (`daemonScopeUnitPrefix = cgroupscope.EscapeTargetPrefix`), so the
name that PRODUCES the escape target and the name that RECOGNISES it in a cgroup
path cannot drift (Rules 13/14). The prefix is unguessable enough
(`gopi-daemon-`) that a foreign host with an unrelated `gopi-daemon-*.scope` is
not a realistic collision; a false "escaped" there would be a wrong verdict, and
the alternative (a name table) is what this fix removes.

**Deliberately NOT name-matched:** which scopes are sessions. systemd's spellings
differ per vendor and per launch path (`session-N.scope`, `vte-spawn-<uuid>.scope`,
`app-gnome-*.scope`, a container's `<machine>-scope-<n>.scope`), and a table of
them is a second model to keep true — the walk needs the OWNING unit's kind,
which the hierarchy itself states.

## What was rejected, and why (measured)

- **"Any `*.scope` ancestor ⇒ in a session."** Red-proved backwards: the escape
  target itself is a scope (`…/app.slice/gopi-daemon-worker-<profile>.scope`),
  so this reading reports an ESCAPED daemon as "still in the session scope" —
  `TestWorkerDaemon_LeavesTheLoginSessionScope` fails with the daemon measured in
  its own transient unit.
- **Stopping at the user manager AS WELL as at the per-uid slice.** Kept
  initially, then removed: removing it turned **no** test red (measured), because
  the per-uid slice is an ancestor of everything the manager contains. An
  untestable second boundary is exactly what a later reader deletes wrongly, so
  the predicate has one (Rule 13 in spirit).

## Red proofs (the predicate has teeth)

Each mutation was applied to the production line, run, and restored
(`git diff` back to the fix):

| mutation | observed failure |
|---|---|
| the pre-fix predicate restored verbatim | unit: `TestInSessionScope_RecognizesASessionScope` + `…_StopsAtTheLoginMachinery` FAIL; e2e: `daemon_cgroup_e2e_test.go:390 … must stay in its session scope, got "…/vte-spawn-….scope"` |
| escape-target carve-out removed (`return true`) | unit checks stay green, **e2e** `TestWorkerDaemon_LeavesTheLoginSessionScope` FAIL: `worker daemon pid … is still in the login session scope "…/gopi-daemon-worker-001-9b0117a163b0.scope"` |
| manager stop removed | nothing turned red → the doubling was deleted rather than carried |

## Verification (this branch)

```
gofmt -l internal/cgroupscope/ cmd/agent/cli_daemon_scope.go   → clean
go build ./...                                                 → OK
go test ./internal/cgroupscope/ -run TestInSessionScope        → ok
go test ./cmd/agent/ -run 'TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation|
         TestWorkerDaemon_LeavesTheLoginSessionScope|TestWorkerDaemon_DetachesAndKeepsRunning|
         TestDaemonScopeNote|TestWorkerDaemon_RequireScopeIsHardWhenUnavailable' → ok (4.3s)
```

Real surfaces, with the measured cgroup of each (Rule 31):

```
$ cat /proc/self/cgroup
0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-<uuid>.scope

$ GOPI_CGROUP_ESCAPE_OFF=1 ./bin/gopi worker --daemon        # escape DISARMED
gopi worker: cgroup escape: setsid-only (GOPI_CGROUP_ESCAPE_OFF is set)
worker daemon started: pid 2959374, …
$ cat /proc/2959374/cgroup
0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-<uuid>.scope   ← arm-off: UNAVOIDABLE
   … and yet the old predicate called this "escaped".

$ ./bin/gopi worker --daemon                                 # escape ARMED (user bus present)
gopi worker: cgroup escape: systemd-scope (/usr/bin/systemd-run)
$ cat /proc/2959550/cgroup
0::/user.slice/user-1000.slice/user@1000.service/app.slice/gopi-daemon-worker-.gopi-4a4f944e264d.scope   ← real escape
$ systemctl --user stop gopi-daemon-worker-.gopi-4a4f944e264d.scope && kill -0 2959550
gone after scope stop
```

So the two worlds are now told apart by measurement, on the host shape that
produced the report: disarmed ⇒ the launcher's own terminal scope (in a
session), armed ⇒ gopi's own transient unit (out of it).

## Honest residuals

- An ssh login scope (`session-N.scope`) is not expressible on this desktop
  host, so the ssh shape is covered by the fixture table, not by a live run.
- The boundary test is named `TestInSessionScope_StopsAtTheLoginMachinery`: the
  walk enforces that property through the per-uid slice, and the user manager is
  below it (see the rejected alternative above), so the name says what the walk
  actually stops at.
- `TestAvailable_AgreesWithEnter` in `internal/cgroupscope` was **already red on
  pristine `master`** on this host (measured: `Available` reads the per-uid slice
  as unwritable while `Enter` succeeds into the manager's delegated subtree). It
  is pre-existing, unrelated to this criterion, and untouched here — it deserves
  its own task.
- The retired `gopi-takeover-*` unit name (the merger's deleted take-over face)
  is NOT recognised as an escape target by the new predicate. That face no longer
  exists (epic daemon-machinery-removal); a process still sitting in such a
  scope would now read as "in a session", which is the honest answer for a unit
  nothing starts any more.
