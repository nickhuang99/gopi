<!-- doc-class: historical -->
# Devdoc: the take-over's escape, MEASURED on the child (Rule 24 acceptance)

Branch `fix/merger-takeover-cgroup-acceptance` (worktree
`/home/user/workspace/gopi-merger-cgroup-proof`), from `master` @ `4d054ae2`.

## Why this exists — the gap the merged fix left open

The fix for the operator report *"gopi merger cannot start"* landed on master
(`b87c9008`, merged as `4d054ae2`): the take-over's cgroup escape was ONE rung
(the in-process cgroupfs migration — the one rung an unprivileged process can
never use on cgroup v2), and it is now the shared daemon seam's ladder
(① `systemd-run --user --scope`, ② cgroupfs, ③ announced session scope).

What that change pinned was the **decision** (`cmd/agent/merger_takeover_scope_test.go`:
which rung is chosen, what argv the wrap builds) and the launcher's **shape** (the
E2E with a stub child). Its own report said so, honestly:

> **No end-to-end proof that a real take-over survives a session teardown** … The
> rung-① selection plus the E2E child assertions are the evidence.

But the acceptance criterion the bug report is *about* is a fact about a real
process, and Rule 24 names exactly ONE instrument for it:

> the ONLY evidence that a process has left its login session is
> `/proc/<pid>/cgroup` NOT matching `session-*.scope`. PPID=1, PGID==SID and a
> successful `setsid()` are not evidence.

Until that reading is taken on a real child, "detach=systemd-scope" is the
launcher's **claim**, not a measurement — and a mechanism claim the start does
not implement is the defect class this whole epic is about (Rule 15).

## What was added

`cmd/agent/merger_takeover_cgroup_acceptance_test.go` — ONE test,
`TestMergerTakeoverChildIsOutsideTheSessionScope`, which runs the REAL `gopi
merger` tick with a published stub child and:

1. asserts the tick returns and prints its start line;
2. waits for the child's own self-report (pid via the stub — never `pgrep -f`,
   Rule 24's sibling trap);
3. reads `/proc/<child pid>/cgroup` through **the package's own predicates**
   (`cgroupscope.RelOfPID` + `cgroupscope.InSessionScope` — the same reading the
   daemon face's e2e uses, so both faces are judged identically);
4. asserts the measured unit name is a `gopi-takeover-*` transient unit AND that
   the launcher's escape line names *that same* unit.

**Step 4 is the strongest form of the assertion** and it was arrived at by
measurement: the first version recomputed the expected unit with
`takeoverScopeUnitName()` in the test process and FAILED —

```
the take-over child's cgroup "…/app.slice/gopi-takeover-002-518e44ac26a2.scope" does not
name the transient unit "gopi-takeover-suite-gopi-home-c606296cdd9d" the ladder requested
```

The unit is per-PROFILE (`daemonProfileUnitTagNow`), and the test process's
profile identity is not the child's (the child runs under the fixture's
`GopiDir`). Comparing the unit the launcher **announced** against the unit the
child was **measured** in is both rig-independent and strictly stronger: it
cross-checks two independent products of the same run instead of a re-derivation.

### The other world is asserted, not skipped

On a host without a user manager, rung ① is unavailable and the honest outcome
IS the session scope — provided the launcher SAID so. That branch asserts
`take-over detach degraded` is present and returns; a silent fall-through is the
Rule 24 defect. So a green run always means one of the two applicable truths was
measured, never "the interesting path did not run".

## Verification

| check | result |
|---|---|
| `gofmt -l cmd/agent/` | clean |
| `go vet ./cmd/agent/` | clean |
| `go test ./cmd/agent/ -run TestMergerTakeoverChildIsOutsideTheSessionScope -v` | **PASS (2.70s)** — child measured at `…/app.slice/gopi-takeover-002-518e44ac26a2.scope` |
| `go test ./cmd/agent/ -run TestMergerCronTickSpawnsOneAndLeavesNoChild` | **PASS (2.47s)** — the sibling launcher rig still green |
| RED PROOF (rung ① forced unavailable = the pre-fix single-rung ladder) | **FAIL**, and it reproduces the operator's defect verbatim: cgroup = `/user.slice/user-1000.slice/session-18205.scope`, `detach=session-scope` |

The red proof was run by patching `mergerTakeoverEscape` to fail rung ①
(`sysdErr = errors.New(...)`) — a two-line mutation restored immediately after
(`git diff` on the file is empty).

## Not Done (honest)

- **The full `./cmd/agent` suite is RED on master already** (measured in this
  proof worktree, whose ONLY change is the one new test file, so the failures are
  provably pre-existing). This branch does not touch them and does not claim
  them. The focused merger/daemon set (`-run 'Merger|Takeover|takeover|Scope|Daemon'`,
  60.9s) is green.
- **No session teardown is simulated.** Killing the login session to prove the
  child survives would kill this host's own session. The `/proc/<pid>/cgroup`
  reading is Rule 24's criterion *because* it is the proxy that needs no
  teardown: a unit in `app.slice` is not in `session-N.scope` and therefore not
  in the scope systemd tears down.
- **Only the take-over face is covered here.** The daemon face has its own
  cgroup e2e (`daemon_cgroup_e2e_test.go`); this file deliberately does not
  restate it.
