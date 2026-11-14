<!-- doc-class: normative -->
# The declared trees had no automatic caller — `/home` filled to 100% while 45 GB sat collectable

Task `01a14592-527f-7ee3-984a-5ef84fb5b3b8`, host `hostb-Laptop` (hosta), 2026-10-17.
Worktree `/home/user/workspace/gopi-01a14592527f7ee3984a5ef84fb5b3b8`,
branch `task/01a14592527f7ee3984a5ef84fb5b3b8`.

## What was reported, and what was actually true

The task arrived as "`cmd/agent` suite scratch hits a 100% full `/home`; merge-union
tests fail with ENOSPC, and 6 prior order/load-dependent reds may be the same cause".
The measurement says the root cause is one level further out than the brief assumed.

## Measurement 1 — the partition, and what is on it

```
df -h /home      → /dev/nvme0n1p7  458G  354G used  81G avail  82%   (/home)
df -i /home      → inodes not exhausted
stat -c '%d %n' / /home /home/user/workspace/gopi-test-scratch /home/user/.cache/go-build
                 → 66307 /   66309 /home  … 66309 /home/user/workspace/gopi-test-scratch
```

`/home` is its own device (66309); the suite's scratch root `gopi-test-scratch` is on
**that device**, so every byte the suite writes counts against the partition that filled.
Rule 22's criterion is satisfied — this is not the `/` partition — but "off `/`" is not
"unbounded", and that is the whole of this defect.

The trees, all on device 66309, measured with `du -xsh`:

| path | size | declared? |
|---|---|---|
| `~/.cache/go-build` | **78 G** | no — agent-side toolchain cache (out of scope by an existing decision, see below) |
| `~/workspace/gopi-build-cache` | **81 G** | **yes — `build-cache/shards`** (45.7 GB of it age-and-holder vetted collectable) |
| `~/.gopi/sessions` | **49 G** | **yes — `raw` / `session-raw` / attachments families** |
| `~/workspace/_tmp` | **11 G** | **yes — `go-tmp`** (1.27 GB of abandoned `go-build*`) |
| `~/workspace/gopi-test-scratch` | 5.8 G | the **home arm** — the one arm that DID run |
| `~/workspace` (total) | 106 G | |

The single largest consumer, the Go build cache, is a *different* tree with a *different,
already-taken* decision attached to it — `internal/scratch/toolchain_cache.go` states it
in as many words ("the finder of a defect proposes the reclaim, the operator decides when
it happens") and its face requires an explicit `--trim`. That decision is left exactly as
it was; conflating the two would have been the tempting wrong fix, and it would have
re-decided somebody else's question in a task about a different one.

## Measurement 2 — every one of those bytes was already authorised for collection

```
$ gopi tool scratch-gc trees --dry-run
scratch-gc role=cli-trees home= removed=3 stale=6h0m0s  disabled=false tree=budget-runs present=true entries=53 bytes=70869348 holding=811133895 holding_n=53
would-remove …/queue/test-budget/run-1792168955-2815153-tested-bin tree=budget-runs age=6h2m45s bytes=67743298
would-remove …/queue/test-budget/run-1792169214-2836961-tested-bin tree=budget-runs age=6h0m34s bytes=1563025
would-remove …/queue/test-budget/run-1792169245-2843402-tested-bin tree=budget-runs age=6h0m15s bytes=1563025
tree-summary tree=budget-runs examined=53 not_ours=7529 collectable=3 kept=50 truncated=false
tree-summary tree=build-cache/suite-sandbox examined=483 not_ours=82 collectable=344 kept=139 truncated=false
```

`collectable` — not "needs a human". The predicates (own namespace, own age window, no
live holder, never the tree's root, a sibling for a shared family) had all already
decided. The verb simply has to be run.

## Measurement 3 — nothing runs it

The declared trees had exactly ONE caller in the whole tree — the CLI verb's own face:

```
$ grep -rn "SweepDeclaredTree" --include=*.go cmd/ internal/ | grep -v _test
cmd/agent/cli_scratch_gc.go:1115:  removed, bytes, err := scratch.SweepDeclaredTree(tree, treeAge)
```

and the resident tick (`cmd/agent/scratch_gc_tick.go`) swept the home, the memory base,
the sandboxes, the profile root and the capture units — **never the declared set**.

The design intent was the opposite, and it is written in the code: `declared_budget_runs.go`
justifies its own row with "it was never in the declared set, so the resident tick could
not walk it either" — i.e. the row exists *so that the tick can walk it*. The wiring was
never done.

The host-level watchdog that was written to drive the verb is **not installed here**:

```
$ ls ~/.local/bin/scratch-gc-watchdog.sh ~/bin/scratch-gc-watchdog.sh   → No such file or directory
$ systemctl --user list-timers --all                                    → only gopi-home-gc.timer
$ crontab -l | grep -i 'scratch\|gopi'                                  → (nothing)
```

`scripts/scratch-gc-watchdog.sh` and its installer exist in the repo
(`devdoc/scratch-gc-declared-trees.md` records a run of it on 2026-09-20); they are not
installed on this host. So on this host the declared set was bounded by **nothing** —
not by the tick (never wired) and not by the watchdog (never installed). A tree bounded
only by a command nobody runs is unbounded, and `/home` at 100% with 45 GB of
`collectable` on it is what that looks like.

## The fix

Wiring, not a new policy and not a new dependency (Rule 35 — no new process, no schedule,
no state file; it rides the tick that already exists).

- `internal/scratch/declared_trees_automatic.go` — `SweepDeclaredTreesAutomatic()`:
  one pass over `DeclaredTrees()`, each row swept at **its own** `StaleAge` through the
  **same** `SweepDeclaredTree` the CLI arm calls. Every guard stays where it was
  (namespace predicate, root refusal, live-holder veto). A row that cannot be resolved or
  walked is returned in its own result with its error, and never folded into a zero
  (Rule 15: "could not look" is never "nothing there").
- `cmd/agent/scratch_gc_tick.go` — the call site, placed **before** the sandbox arm
  (the arms after it can return early on their own refusal, and a declared tree must not
  be left unbounded because an unrelated sandbox root was misconfigured); it rides the
  same cadence and the same off switch, is counted into the tick's return value, prints
  one line per tree that gave bytes back, and reports an unwalkable row once on stderr
  without failing the host task (fail-open, like every other arm).
- `cmd/agent/cli_scratch_gc.go` — `scratchGCDeclaredTreesLogFmt`, composed from
  `scratchGCLogFmt` (Rules 13/14) so a field added to the shared line reaches both faces.

**The toolchain cache is deliberately NOT in this face**, and the distinction is the one
that matters: `gopi tool scratch-gc trees` deletes on invocation with no `--yes` and no
vetting step — its entire safety argument is in the predicates, and that argument does not
weaken when the caller is the tick. The build cache's face *does* carry an explicit
operator step, so making it automatic would be re-deciding a question this task was not
asked. (A follow-up for the 78 GB cache is filed separately — see `## Not Done`.)

## The regression proof

`cmd/agent/scratch_gc_tick_declared_trees_test.go` plants ONE aged, unheld `go-build*`
entry in the declared go-tmp tree (every resolver rank pinned inside the fixture, both
sandbox arms pinned empty exactly as `TestScratchGCTick_ThrottleAndFailOpen` does),
asserts the tree resolves to where the fixture wrote (so a green cannot be vacuous), runs
**one real tick**, and requires the entry to be gone and the tree's root to survive. A
second test pins the other half — a *fresh* entry inside its window survives — because an
automatic caller that ignored the age window would be worse than no caller.

Red proof: the wiring was mutated back to `[]scratch.DeclaredTreeSweepResult(nil)` in
`cmd/agent/scratch_gc_tick.go` (HEAD blob `e40d286e6141ded0119f4052b73578e59e2646c3`),
the test failed with `one tick removed 0 entr(ies), want 1 …`, and the mutation was then
reverted and the revert verified against the explicit revision — see the report's
`## Evidence (self-filed)` for the commands and outputs.

## The 6 order/load-dependent reds

Not this cause, and not re-litigated here. The ENOSPC explanation was a *hypothesis about*
`01a1458b-3644-7be4-bb39-2b3dc56fcb0b` (6 of 9 package-wide reds, green in isolation,
`workerTakeOverAttempt` returning `""` only inside the full-package run). This run's
`cmd/agent` package-wide run is recorded in the evidence directory; the honest reading and
what remains unproven is stated in the report's `## Not Done`.
