<!-- doc-class: historical -->
# devdoc — the capture unit's lifetime, owned by the registry instead of the writer

**Epic**: `gopi shot`'s headless chrome runs as a transient systemd USER unit
(`gopi-shot-<pid>-<nanotime>.service`, `systemd-run --user --unit=… --collect`),
whose only teardown is `defer proc.stop()` → `systemctl --user stop <unit>`. A
`SIGKILL`ed shot never reaches its defer, so the unit and its chrome descendants
stay **ACTIVE forever**. Measured 2026-09-23 on peerbox: **13 active units**, the
oldest **4h55m**; the same shape is what a test timeout or a cancelled run
leaves behind. This change gives those units a lifetime that does not depend on
the writer reaching its defer.

## 1. Why the systemd-unit path exists at all (do not "simplify" it away)

`internal/shot/launch.go`'s header is the authority; the short version:

- `bash` children carry scratch's clamp (`ulimit -v 12 GiB -u 4096`), and a hard
  limit is inherited. Chrome reserves **~51 GiB of VIRTUAL** address space per
  process (measured `VmPeak` 5.49e7 kB — the pointer-compression cage), while its
  RSS is ~200 MiB. Under `RLIMIT_AS` it dies at startup before printing a
  DevTools port (V8 `CHECK` failure, `trap int3`).
- A process spawned by the systemd USER MANAGER starts **outside** those limits.
  `systemd-run --user --unit=… --collect -q -- <chrome argv>` is a transient
  SERVICE unit under `app.slice`. (`--scope` does NOT work: a transient scope
  inherits the caller's limits — four controlled probes, only `LimitAS` mattered.)
- The clamp is not simply dropped: the unit carries
  `MemoryMax=512M MemorySwapMax=0`, which bounds the thing that actually
  threatens the host (resident memory) instead of a reservation chrome never
  uses.

**The structural consequence, and the reason this epic exists**: chrome's parent
is the user MANAGER, not the shot process. The unit therefore outlives its
creator *by design*, and nothing in the unit's own configuration can tie the two
together.

## 2. Shape A vs Shape B — decided, and Shape B is documented as a supplement

Both shapes were on the table. Only one is implemented.

**Shape A (implemented): an external, mechanical reclamation.** The creator
registers the unit (its own pid + kernel start ticks + command line, the unit
name, the timestamp) the instant it holds the name; the EXISTING one-shot
sweeper stops the registered units whose creator is **measurably** gone. No new
daemon, no new tick, no new schedule, and the unit keeps the `--collect` /
`MemoryMax` / clamp-bypass behaviour untouched.

**Shape B (`PR_SET_PDEATHSIG`), NOT implemented — and it cannot be, on this
path.** The signal is delivered to a process when its *parent thread* exits, and
the flag is **cleared on `exec`**:

- chrome cannot be made to set it: systemd `exec`s chrome directly, and the
  flag has to be armed *after* the exec by the process itself (chrome is not
  ours to modify);
- arming it on the `systemd-run` **client** is meaningless: that client is a
  short-lived process that always exits normally — the case that leaks is
  precisely the one where the *shot* process dies, not the client;
- chrome's parent is `systemd --user`, which never dies, so even a correctly
  armed `PDEATHSIG` would never fire.

Shape B would **help the attached launch path** (`launchChromeAttached`, where
chrome is a real child and `CommandContext` already kills its process group on
cancel — a case that is already reaped, and does not leak). It is therefore
recorded here as a *supplement to a different path*, deliberately **not
half-built**: a `PDEATHSIG` on a path that cannot leak would be a change nobody
can measure the effect of, protecting the case that was never broken.

## 3. The design

```
gopi shot                       gopi scratch-gc  (CLI one-shot  +  resident worker/merger tick)
─────────                       ──────────────────────────────────────────────────────────────
systemd-run --user --unit=U     scratch.SweepHome(...)          ← unchanged
   │  unit exists
   ├─ scratch.RegisterShotUnit(U, pid, ticks, cmd)
   │     → <scratch home>/shot-units/<U>/unit.json
   │
   ├─ waitDevToolsPortInFile(...)      ← a kill in THIS window is registered too
   │
   └─ defer stop() → systemctl --user stop U
                     scratch.DeregisterShotUnit(U)
                                     scratch.ReclaimShotUnits(proc, dir, stop)
                                       ├─ verdict := procname.RunnerIdentity(record)
                                       ├─ gone | recycled  → stop(U) → deregister
                                       └─ live | unmeasurable | unreadable → KEEP (named reason)
```

Delivery points, each with the file that owns it:

| piece | where | why there |
|---|---|---|
| registry write + deregister | `internal/shot/launch.go` (`registerShotUnit`), `internal/scratch/units_registry.go` | the writer owns the moment the name is held |
| the verdict + the conservative keep | `internal/scratch/units_registry.go` (`ShotUnitCreatorVerdict`, `shotUnitVerdictKeeps`) | ONE policy, both faces |
| the liveness ladder | `internal/procname` (`RunnerIdentity`) | the SAME implementation the task ledger uses (`taskproc.go`'s `identityLiveness`, `finishCleanupProcessHolderLive`) — **no second probe** |
| the teardown argv | `internal/shot/launch.go` (`stopUserUnitErr`) | ONE definition; the normal defer and the sweep call the same function |
| the CLI face | `cmd/agent/cli_scratch_gc.go` (`reclaimShotUnits`, dry-run plan) | rides `gopi scratch-gc`, which the host watchdog already runs |
| the resident face | `cmd/agent/scratch_gc_tick.go` → `internal/shot.ReclaimOrphanedUnitsReporting` | rides the worker/merger tick that already sweeps |

### Decisions worth recording

- **Registry = a directory of records, not a line in one file.** The writer is
  concurrent (several shots at once) and must never corrupt another run's
  registration; one atomically written directory per unit gives that with no
  read-modify-write, and one `RemoveAll` is the whole deregistration.
- **Registration happens BEFORE the DevTools port is awaited.** A kill during
  the startup window (up to `chromeStartTimeout` = 15s) must not leave an
  unregistered unit — a record written after the port appeared would leave the
  leak open for the whole window. The refusal path (`waitDevToolsPortInFile`
  fails) also deregisters, so a failed launch leaves no residue.
- **A creator is identified by pid + kernel start ticks**, with the recorded
  command line only as the diagnostic spelling and as the fallback factor for a
  legacy record without ticks. `RunnerLive`'s doc is explicit that a runner's
  cmdline is a hint and not an identity axis — the incarnation is the strong
  factor, and passing the cmdline alongside it constrains nothing.
- **`unmeasurable` and `unreadable` are their own verdicts.** Both KEEP the
  unit. This is the direction that cannot destroy a live capture: a second
  reader of the same facts that read "could not measure" as "gone" would stop
  the chrome of a running `gopi shot`.
- **A failed stop keeps the registration**, so the next sweep retries; the
  sweep reports it on stderr rather than silently counting it as reclaimed.
- **The registry is not a scratch entry.** `shot-units` carries no namespace
  marker and is not a queue child, so `gc.go`'s `Collectable` refuses it at any
  age (`TestShotUnitRegistrySurvivesTheHomeSweep` pins this with a genuinely
  collectable sibling in the same home, so the test cannot pass vacuously).
- **Arm ordering on the tick**: the capture-unit arm runs BEFORE the base and
  sandbox arms, because each of those can `return` early on its own failure and
  an orphaned chrome must not depend on an unrelated tree being sweepable.
- **The `gopi shot` usage text was deliberately NOT changed**, even though it
  documents the transient unit: `cmd/agent/g6-tools-maintenance-help-surface.sh`
  and the CLI contract suites exact-match the verb help surface, and this
  branch's mandate is the reclamation, not a help-text rollout. A sentence
  belongs in that text ("a kill -9'd shot's unit is reclaimed by `gopi
  scratch-gc`") and is filed under *Not Done*.

## 4. Residual gaps (honest)

1. **The sweep is periodic, not instant.** An orphan lives until the next
   `gopi scratch-gc` or the next resident tick (default cadence 60 min,
   `scratchGCDefaultIntervalMin`). The unit's lifetime is now BOUNDED, not
   immediate. Closing this would need an event, i.e. new machinery — outside
   this epic's "no new daemon / no new tick" constraint.
2. **The host that leaks most may have no sweeper.** A host watchdog is
   installed explicitly (`scripts/install-scratch-gc-watchdog.sh`); this host has
   the stable copy at `~/.local/bin/gopi-scratch-gc-watchdog.sh`. On a host with
   neither that nor a resident gopi, an orphaned unit still waits for the next
   manual `gopi scratch-gc`. (That is the SAME precondition the scratch home
   sweep has always had — no regression, no new gap.)
3. **The pre-existing units are not reclaimed by this change.** They carry no
   registration (they predate it) and the sweep deliberately refuses any unit it
   did not register: acting on the systemd unit list would mean deciding about
   units this system cannot attribute to a creator, which is exactly the "no LLM
   judgement / mechanical lifecycle only" line the epic drew. They are cleaned up
   once, by hand or by a separate census — **dispatched as task
   `01a0cf3b-0921-72ea-8b4c-3581a7106979`, not left as a note** (Rule 26) — and
   cannot recur. Census at filing time (2026-09-24): 14 units, every creator
   provably dead, ~130 chrome processes retained.
4. **A registry entry that no sweep ever reads is unbounded in count.** A host
   that runs `gopi shot` without ever sweeping accumulates one small directory
   per capture. Bounding that by age would be a policy for *crashed-vs-finished*
   registration, and a registration for a live capture must never be aged out —
   so it is left to the sweep that already runs, rather than guessed at here.
5. **Shape B is not built** (§2), on purpose, and the attached path's own
   reaping is unchanged.

## 5. Evidence

RED and GREEN are REAL executions on this host (real `gopi shot --url`, real
`kill -9`, real `gopi scratch-gc`, real `systemctl --user`), with the observed
output quoted in `tasks/shot-unit-orphan-reclaim-report.md`. Scripts:
`~/.local/share` is not used — they are kept beside the binaries in the evidence
directory quoted by the report.
