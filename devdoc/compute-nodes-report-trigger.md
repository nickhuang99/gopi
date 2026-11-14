<!-- doc-class: historical -->
# compute_nodes "NO CONSUMER" report (Rule 21)

> **SUPERSEDED 2026-10-22 (task `01a160a2`)**: the `compute_nodes` knob, its
> resolver, its three sources and `cmd/agent/dispatch.go` were DELETED — see
> `devdoc/compute-nodes-knob-deleted-20261022.md`. This page is the record of the
> state it was written in (a knob marked, not wired); it describes no live code.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

RECORD (2026-10-07, task `01a11548`): where the Rule 21 advisory for the
`compute_nodes` whitelist speaks. The finding itself is unchanged — the knob
still has no consumer. The report existed (task `01a0ed9f`); this record is
about correcting WHERE it speaks and pinning that wiring with tests.

## The defect (measured 2026-10-07, task `01a11548`)

`reportComputeNodesWithoutConsumer()` stood in `cmd/agent/tmpdir_inject.go`'s
`tmpDirBootstrapFromArgs` — the **pre-subcommand-chain** bootstrap in `main()`.
That function's own argument for the placement was "every verb passes through
here", and the argument is exactly what made the line noise: the bootstrap runs
*before* the dispatch chain knows which verb was asked for, so on a host whose
`settings.json` declares `compute_nodes` (peerbox) the line landed in front of

| command | `compute_nodes:` lines on stderr |
|---|---|
| `gopi task show <id>` | 1 |
| `gopi notify --peer nosuchpeer --session <id> --message x` | 1 |
| `gopi worker --once` (the face the knob concerns) | 1 |

— and, by construction, in front of `gopi task submit` and every bare one-shot
prompt run as well. `gopi notify` and `gopi task show` cannot read a dispatch
layer at all: the advisory was addressed to nobody. A line every command prints
is a line every reader learns to skip, so the report reached the operator who
would act on it with **less** force, not more.

## The fix

- `tmpdir_inject.go` no longer calls the report; the comment left in its place
  states why the site was wrong (so the next reader cannot re-add it on the same
  "every verb passes through here" argument).
- `isWorkerSubcommand` (`cmd/agent/main.go`) calls it, **after** the `-h`/`--help`
  scan, gated by the new ONE predicate `isWorkerFaceArgv`
  (`cmd/agent/compute_nodes_unwired.go`) — the predicate lives beside the
  sentence it gates, not in the dispatcher.
- Unchanged: `config.ResolveComputeNodes` (Rule 2 — one resolver), the early
  return on an empty declaration, and the one shared reason sentence
  `config.ComputeNodesNoConsumer` (Rule 1b/14 — no forked short copy).
- It still never refuses (Rule 15: report, do not refuse). A stale declaration is
  not a reason to take a worker down.

## Why the worker face, and why not the others

`dispatch.go`'s `runDispatchOnce` — the knob's only consumer — has no caller; its
one historical entry point was the resident serve daemon. The face that would
have carried it is therefore the **worker**, and the operator starting a worker
is exactly the reader who needs to know the whitelist reaches nothing.

Rejected candidate, worth recording because it looks right and is not:
**`gopi merger` / `gopi task serve` / `gopi task merger`**. They are
worker/serve-shaped by verb, but every argv of them exits inside
`retireMergerFace` (`-h` exits 0 with a one-line pointer; every other argument
list exits 1 with the retirement cause). A capture point placed after the
retirement is **unreachable**, so putting the report there would have been a
report that can never print — a silent loss of the detection, dressed as a
narrowing. Rule 31's habit (run the surface, do not reason about it) is what
catches this: the verb is reachable, the line behind the `os.Exit` is not.

`gopi worker -h` is silent **on purpose** (P0-1: a usage query answers whatever
the state of the world and spawns nothing); the report sits after the `-h` scan.

## How the wiring is pinned (the test half)

`cmd/agent/compute_nodes_unwired_test.go`:

- `TestComputeNodesReportIsReachedOnTheStartPath` — both directions: `main.go`
  must call it and must gate the call by `isWorkerFaceArgv`; `tmpdir_inject.go`
  must **not** call it. The negative half is the defect this task closes, and no
  unit test over the reporting function can see it.
- `TestComputeNodesReportHasExactlyOneCallSite` — exhaustive: every non-test
  `.go` file in `cmd/agent` is read and the call counted; exactly one site is
  allowed, and it must be `main.go`. A read-floor (`scanned < 20`) makes a blind
  scan fail instead of passing vacuously.
- `TestComputeNodesTriggerIsTheWorkerFace` — the trigger table driven through the
  real predicate: loop, `--once`, a named row, `--resume`, `--daemon` and
  `--status` fire; `-h` / `--help` do not.

Red proof: re-adding the old pre-chain call makes both wiring tests fail by name
(`tmpdir_inject.go calls reportComputeNodesWithoutConsumer again …`,
`has 2 call site(s) outside its own definition: [main.go tmpdir_inject.go]`).

Pitfall met while doing it (recorded because it silently un-fixes the change):
`git checkout cmd/agent/tmpdir_inject.go` to undo the *red-proof* re-add also
reverted the **uncommitted** real fix in that same file. Copy the file aside
before the red proof, or re-apply from the diff, before rebuilding — otherwise
the binary under test predates the fix and every "green" is meaningless.

## Still open (honest)

The knob is still unwired: `runDispatchOnce` has no caller and no remote-dispatch
layer works. This change only controls *where the truth is told*. Re-wiring is a
separate epic (Rule 26).
