<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: `gopi worker --status --stop` — a read+act argv is REFUSED, not silently reduced to the read

Fixes **F1** of `devdoc/worker-daemon.md` (the daemon operating-model decision record,
commit 055924af). Branch `fix/worker-status-stop-conflict`.

## The defect (measured on master 7358ee18, and on 7c24ea06 where it was filed)

```
$ gopi worker --status --stop      # prints the live-worker lines, exit 0, NOTHING signalled
$ gopi merger --status --stop      # exit 0, NOTHING signalled
$ gopi task merger --status --stop # same
```

Two different mechanisms produced the same silent success code:

- **worker face** — `isWorkerSubcommand` (cmd/agent/main.go) routed the READ-ONLY
  status action FIRST: `if workerStatusRequested(args[1:]) { os.Exit(workerStatusReport()) }`.
  That `os.Exit` returned before `workerControlCommand` could call
  `workerControlConflicts` — so the function whose entire job is to refuse "pick ONE
  action" was **unreachable** whenever `--status` was present. The comment above
  `workerControlConflicts` ("Multiple DISTINCT actions are left for the caller to
  refuse — this function never picks silently") described a contract that could not fire.
- **merger face** — a *different* shape with the same outcome: `--status` is not routed
  at all on this face, it is a PARSED option (`opts.statusOnly`) that the dispatch
  consults *after* `mergerControlCommand` has already stopped/restarted the resident
  service. So `--status --stop` was not a no-op here — it **actually performed the
  action** while the operator asked to look. This is the argv shape that produced the
  F4 self-inflicted incident recorded in `devdoc/worker-daemon.md` (a live resident
  merger SIGTERMed mid-pass).

## Why a refusal and not an ordering rule

A read plus an act in one argv is ambiguous **by construction**: does the operator want
the state *before* the stop, *after* it, or both in one command? Every ordering rule
silently answers a question only the operator can answer, and the failure mode of the
wrong answer is what the bug already was — a monitor that believes the service stopped.
The refusal names BOTH flags, exits non-zero, and leaves the choice to the operator.

## The fix

One shared predicate, in `cmd/agent/cli_worker_control.go` next to its ACTION-class
sibling `workerControlConflicts`:

```go
type controlActionRole int
const ( controlActionNone controlActionRole = iota; controlActionReadOnly; controlActionActs )
func controlActionRoleOf(word string) controlActionRole   // --status = read-only; the rest act
func controlReadAndActRefusal(args []string) string       // "" when the argv is unambiguous
```

It is spelled on the ROLE classes (read-only vs acting), not on one component's flag
list, because the worker and the merger share the control flags as *wire values* but are
different services and a future fifth action must be classified rather than silently
routed. Each face keeps its own message spelling + exit code:

| face | guard site | exit code |
|---|---|---|
| `gopi worker` | `isWorkerSubcommand`, **before** the status route | `workerUnexpectedArgExitCode` (2, usage class) |
| `gopi merger` | `isMergerSubcommand`, before `mergerControlCommand` | `mergerStopExitFailed` (1) |
| `gopi task merger/serve` (deprecated alias) | `cli_tasks.go` dispatch, before `mergerControlCommand` | `mergerStopExitFailed` (1) |
| `gopi task merger` via `mergerControlCommand` | `mergerControlRefuseReadAndAct` at the top of the command | `mergerStopExitFailed` (1) |

Four call sites on purpose: the merger's `--status` is reachable on two faces AND
through the control command itself, so a guard on only one route leaves the other three.
The refusal is deliberately **not** added to `mergerControlConflicts`: that function
lists ACTIONS, `--status` is not one, and widening it there would make the status flag
look like an action.

## Verification

| check | result |
|---|---|
| `go build ./...` | OK |
| New e2e (`TestStatusWithActingAction*`) — 3 faces × 4 acting verbs | **13 subtests PASS** |
| RED proof: revert the 4 guards, rebuild, rerun | **13 subtests FAIL** (exit 0 / no refusal text) — the test is not a shape formality |
| `go test ./internal/standards/` | OK (baseline re-keyed, see below) |
| `go test ./testsuite/...` | OK |
| `go test ./internal/tools/` | OK |
| `go test ./cmd/agent/` (full) | one pre-existing failure, **identical on master** (see boundary) |
| `gofmt -l cmd/agent/` | clean |

### The e2e (`cmd/agent/status_action_conflict_e2e_test.go`, real binary)

- ① every face × every acting verb (`--stop`/`--restart`/`--start`/`--force`) exits
  non-zero and the message names BOTH flags plus "pick ONE action";
- ② the profile is untouched — asserted by a **walk of the whole private GOPI_HOME**,
  which is strictly stronger than "no request file": the action path creates
  `~/.gopi/worker` via `tools.ControlDir` *before* it writes anything, so any trace at
  all proves the action path was entered;
- ②′ the sharpest form: a **seeded live-looking state file for our own pid** is placed
  first, so a stop path would have a target and really write `TERM_<pid>`; after the
  refusal that request file must not exist. Without this the empty-profile assertion
  could pass merely because an empty profile gives the action nothing to do;
- ③ a bare `--status` still exits 0 and still creates nothing (the documented read-only
  guarantee — the half a careless "refuse when another flag is present" fix would break);
- ④ `--stop` alone still takes the ACTION path (the honest "no daemon is running" line,
  exit 0) — proving the refusal did not overshoot into a blanket ban.

### Two test-harness traps worth recording

1. **`sscFaceArgv` must render the VERB.** The first draft built the argv from the
   face's prefix only, so the worker/merger faces degenerated into
   `gopi --status --stop` — the *agent* face, which refuses `--status` through the
   unrelated unknown-flag path and exits **1**. The refusal assertions still "passed" the
   exit-code check for the merger (1 == 1!) and the message check is what caught it. The
   verb is now rendered from one place (`sscFaceVerb`) so a face can never degenerate.
2. `f.prefix[1:]` panics on a zero-length prefix — guard with `len(f.prefix) > 1` when
   the first element is the verb.

### Boundary — a PRE-EXISTING master failure, not this change

`go test ./cmd/agent/` fails on `TestRunTaskWorker_OnceSweepsZombieFirst`:

```
FAIL: inherited worker lock fd 3 is not a usable file: stat worker.lock:
bad file descriptor: handshake write failed: write GOPI_DAEMON_READY_FD: bad file descriptor
```

**Reproduced identically on master at the same tip** (`git -C ~/work/GoPi` clean tree,
`go test ./cmd/agent/ -run TestRunTaskWorker_OnceSweepsZombieFirst`) — same message, same
`FAIL`. The full-package failure *count* also fluctuates run to run on master (3, then 0,
then 1 across four runs), so the package total is not a stable baseline; the per-test
identity is. Charged to master, not to this branch. (The three
`TestAgentSpawnGuard*` names that appeared in one master run are the same fd-3 cause.)

## Addendum (2026-09-13, second pass): `--daemon` is an ACT on its own route

The route audit for this fix found a **third** silent read+act, on the merger's daemon
route — a different mechanism again, so it is recorded here rather than left to the
table above:

```
$ gopi merger --daemon --status    # measured: DETACHED a resident merger, printed
                                   # "merger daemon started: pid …", exit 0
```

`--daemon` is not a member of the `--stop/--restart/--start/--force` family, so
`mergerControlRequested` never saw it: the argv fell through to
`mergerDaemonRequested`, which spawned the daemon and never looked at `--status`. The
operator asked to look and got a service — the same F1 shape, one route over.

- `--daemon` (`daemonFlagLong` **and** its `--daemon=…` spelling) is now classified as
  `controlActionActs` in `controlActionRoleOf`, so *any* route guarded by
  `controlReadAndActRefusal`/`mergerControlRefuseReadAndAct` covers it without the route
  having to remember it. The guard was added to the `mergerDaemonRequested` branch in
  `isMergerSubcommand`.
- `mergerControlConflicts` now lists `--daemon` too, so `gopi merger --daemon --stop` is
  refused as two acts ("--daemon and --stop — pick ONE action") instead of silently
  detaching while an action was also requested. The worker face needs no such entry: its
  argv scanner already refuses `--daemon` in combination.
- The deprecated alias reaches `--daemon` by yet another path (its flag table has no
  `--daemon`, so the parser refuses the whole argv with the "top-level verb only" hint) —
  already loud, already non-zero, now pinned by `TestAliasDaemonStatusIsRefused` so it
  cannot regress into a silent no-op.

Verified after the addendum (real binary, private `GOPI_HOME`):

| argv | exit | answer |
|---|---|---|
| `merger --daemon --status` | 1 | `--status and --daemon cannot be combined — pick ONE action` |
| `merger --daemon --stop` | 1 | `--daemon and --stop — pick ONE action` |
| `task merger --daemon --status` | 1 | `unknown flag --daemon … top-level verb only` |
| `merger --daemon` (alone) | 0 | still detaches; `merger --status` then shows it alive |

## Not Done

- The two files still carry legal-but-inconsistent siblings of the old shape:
  `workerDedupeActions`/`mergerControlParse` report the *first* of >1 distinct action
  instead of the full list, which is why the peer sweep prints "--daemon and --stop"
  while the worker prints "--status and --stop". Both paths refuse correctly; only the
  message's phrasing differs. Filed as a follow-up, not bundled here.
- The other findings of `devdoc/worker-daemon.md` (F2 launcher/DISPLAY ownership,
  F3 the stale `workerDaemonSpec` comment) are out of scope here — this task is F1 only.
- No `--status=false`-style negation or subcommand spelling was introduced: the refusal
  is the whole change, per the briefing's "prefer the refusal".

## Files

- `cmd/agent/cli_worker_control.go` — `controlActionRole` / `controlActionRoleOf` /
  `controlReadAndActRefusal` + the rationale.
- `cmd/agent/main.go` — worker guard (before the status route) + merger guard.
- `cmd/agent/cli_merger_stop.go` — `mergerControlRefuseReadAndAct` + call at the top of
  `mergerControlCommand`.
- `cmd/agent/cli_tasks.go` — the deprecated `gopi task merger/serve` alias guard.
- `cmd/agent/status_action_conflict_e2e_test.go` — the real-binary e2e (new).
- `internal/standards/baseline.go` — **re-key** `cmd/agent/main.go:940 → :955` with its
  reason: the two guard blocks added lines above the baselined Rule-16 `%v` false-positive
  site. The site is unchanged; only its line-key moved (the gate fails on a stale permit
  exactly as on a new violation, so it is re-keyed, never relaxed).
