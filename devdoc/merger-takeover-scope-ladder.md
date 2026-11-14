<!-- doc-class: historical -->
# Task Report: the take-over's cgroup escape is a LADDER, not one rung

Branch: `fix/merger-takeover-reuse-scope-ladder`

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Record, not current
> truth: the take-over launcher and its cgroup-scope ladder were deleted with the
> resident merger (`f9ff1a89`, `55c4d6f3`). The one-tick merge is the bare
> `gopi merge`. See `devdoc/merger-retired.md` and the dated correction in
> `devdoc/merger-self-driving.md`.
Worktree: `/home/user/workspace/gopi-merger-takeover-seam`
Base: `master` @ `1e5a3f29` ("merge(feat/branch-lifecycle-ruling): land the branch-lifecycle ruling on master")

## The operator-visible symptom

`gopi merger` on this host printed, on every tick:

```
gopi merger: take-over detach degraded: cgroup scope escape unavailable: migrate into
/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/gopi-daemon-merger-nick-<tag>/cgroup.procs:
write …: permission denied — the child stays in this login session scope, keep the tick short-lived
gopi merger: take-over started pid=3083908 detach=session-scope log=/home/user/.gopi/logs/merger-takeover.log
```

i.e. it looked like the merger "cannot start" (or starts something that cannot do its
job), while the tick's own line claimed a child had been started.

## Root cause (measured, not inferred)

`mergerTakeoverDetach` asked exactly **ONE** mechanism — the in-process cgroupfs
migration (`cgroupscope.Enter`) — and, when that one rung failed, reported the whole
**HOST** as unable to escape.

That rung is the one an unprivileged process can never use here: cgroup v2 gates a
`cgroup.procs` write on the **shared ancestor** of source and target, and
`user-1000.slice` is `root:root 0755` (`internal/cgroupscope`, Rule 25 records why no
privilege may be prescribed as the remedy).

Measured on this host (2026-09-17):

| probe | result |
|---|---|
| `systemd-run --user --scope --quiet /bin/true` | **rc=0** ("Running scope as unit: …") |
| `/run/user/1000/bus`, `/run/user/1000/systemd/private` | both present |
| `systemctl --user is-system-running` | `running` |
| `cgroupscope.Enter(uid, …)` from the tick | `EACCES` on `cgroup.procs` |
| child `pid 3083908` `/proc/<pid>/cgroup` | `…/session-18205.scope` (the login session) |

So rung ① **works**, rung ② **cannot** — and the take-over called only rung ②, then
blamed the host. The consequence is Rule 24's exact failure: the take-over lived in
the launching shell's `session-*.scope`, so the session teardown (shell exit, session
manager reap) takes the merge round with it.

## The fix

The shared daemon seam (`cmd/agent/cli_daemon_scope.go`) has had the correct ladder
since 2026-09-13. This change gives the take-over **the same ladder**, and reuses the
seam's own pieces rather than re-implementing them:

| rung | mechanism | shared piece reused |
|---|---|---|
| ① | `systemd-run --user --scope` (transient user scope) | `probeSystemdRun()`, `systemdRunPath`, `systemdRunFlagUser/Scope/Unit/Quiet`, `daemonFlagValueSep` |
| ② | in-process cgroupfs migration | `cgroupscope.Enter`, `scopeDirName(CliVerbMerger)` |
| ③ | neither: setsid only, **announced** (Rule 24) | — |

New file `cmd/agent/merger_takeover_scope.go`:

- `mergerTakeoverEscape(prog, exe) (mode, wrap)` — decides **and applies** the escape,
  returns the mode the child really runs under plus the wrap the launcher applies to
  its child command (rung ① rewrites argv into the systemd-run wrapper; ②/③ return the
  identity wrap, because the escape is then a property of the launcher's cgroup, which
  the child **inherits**).
- `takeoverScopeUnitName()` — `gopi-takeover-<profile tag>` (deliberately **not**
  `gopi-daemon-*`: the take-over is not a daemon, exactly as `mergerTakeoverArgv0` is
  not `gopi-<component>`).
- `takeoverUnitHeld()` — reuses the seam's `daemonScopeUnitLoadState` (including its
  load-bearing `--user` flag), with a bounded wait for a just-released name.
- `mergerTakeoverAlias()` — the rung ① **argv[0] alias** (below).
- `takeoverModeLeftSessionScope` / `takeoverModePhrase` / `takeoverModeStampValue` —
  ONE predicate + rendering over the mode vocabulary (systemd-scope / cgroupfs-scope /
  session-scope), with an **unknown** value reading as NOT escaped (safe direction).

`mergerTakeoverDetach` is deleted; `merger_takeover.go` keeps the launcher's shape
(① log, ② escape, staged-prompt-on-stdin, ③ cwd escape, ③b **wrap applied last**,
④ `Start` then return).

### The argv[0] alias is load-bearing, not cosmetic

The take-over identifies itself by `filepath.Base(os.Args[0]) == mergerTakeoverArgv0`
(`mergerTakeoverInjectedBriefing`). `systemd-run --scope <path>` execs `<path>` with
`argv[0] = <path>`, so a naive rung ① would start a child that reads
`argv[0] = ".../bin/gopi"`, concludes it is **not** a take-over, and runs as an
unattended agent with **no briefing at all** — strictly worse than the degraded detach
being fixed. The rung therefore execs an atomically-published scratch **symlink** whose
basename IS the marker. `TestMergerTakeoverSystemdWrapKeepsTheMarker` pins it, and the
existing E2E (`TestMergerCronTickSpawnsOneAndLeavesNoChild`) passes under rung ① —
including its stub-image, argv[0]-marker and stdin-byte assertions.

## Files

- `cmd/agent/merger_takeover_scope.go` (new) — the ladder.
- `cmd/agent/merger_takeover_scope_test.go` (new) — the regression tests.
- `cmd/agent/merger_takeover.go` — `mergerTakeoverDetach` removed; spawn applies the
  returned wrap last; mode vocabulary + briefing frame updated.
- `cmd/agent/merger_takeover_launcher_test.go` — the briefing test uses the new mode
  constant.
- `cmd/agent/merger_tick_no_child_gate_test.go` — the no-child gate gains the ladder's
  file (per-file forbid list + a ladder presence list), because that file is now the
  one piece of the take-over that starts processes of its own (the probe, systemd-run).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK.
- Vet: `go vet ./cmd/agent/` → clean.
- New tests: `go test ./cmd/agent/ -run 'TestMergerTakeoverEscape|TestMergerTakeoverSystemdWrap|TestTakeoverMode|TestMergerTakeoverBriefing|TestMergerTakeoverAlias'` → **ok**.
- **Red proof** (Rule 10.5 / Rule 11): with rung ① patched out (the old single-rung
  shape), `TestMergerTakeoverEscapeSelectsRungOne` FAILS and reproduces the operator's
  log line verbatim —
  `the ladder chose mode "session-scope", want "systemd-scope" … rung ② … permission denied`.
  Restoring the ladder turns it green.
- No-child gate: `TestMergerTickSpawnsButNeverWaits` → ok.
- E2E launcher rig: `TestMergerCronTickSpawnsOneAndLeavesNoChild` → **PASS (3.34s)** under
  rung ① (real `systemd-run` wrap, stub child): the child still reports the marker
  argv[0], the stub image, and the full prompt on stdin.
- Full package sweep: see "Not Done" for the run that was still in flight at
  write-up time; the package set `go test ./cmd/agent/ ./internal/tools/
  ./internal/standards/ ./internal/cgroupscope/ ./internal/scratch/` is the gate.

## Not Done (honest)

- **`merger --daemon`'s own path is untouched.** The daemon face has had this ladder
  since 2026-09-13; this change brings the take-over to parity with it, and deliberately
  does not edit the seam itself.
- **The unit name is per-profile, not per-run.** Two ticks of the same profile overlap
  only if one tick's take-over is still running; that case falls through to rung ② with
  the reason announced, rather than stealing the name (systemd would refuse anyway).
- **Rung ② remains unusable on this host** — correctly. It is kept because the seam
  keeps it (a host without a user manager needs it), and its failure is now one rung's
  reason instead of the whole verdict.
- **No end-to-end proof that a real take-over survives a session teardown** (killing a
  login session to prove it would kill the test host's own session). The rung-①
  selection plus the E2E child assertions are the evidence; the cgroup measurement
  itself is what the daemon seam's own e2e (`daemon_cgroup_e2e_test.go`) covers on the
  daemon face.
- **A sibling session was working the same defect concurrently**
  (`~/workspace/gopi-merger-takeover-detach`, branch `fix/merger-takeover-scope-escape`,
  uncommitted at the time). This branch was opened FRESH from master per Rule 23/29 —
  the sibling's worktree was only *read* (it informed the alias decision, which is the
  subtle part), never developed in. Two branches answering one defect is the ordinary
  Rule 26 overlap; the operator can pick either.
