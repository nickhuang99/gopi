<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
# The resident merger daemon is RETIRED (2026-09-19)

## The report

```
nick@peer1:~/work/GoPi$ gopi merger --stop
[merger] gopi merger: stop requested — pid 552530, terminated
(answered at the tick boundary; the in-flight tick is not interrupted)
```

The operator's verdict: *buggy and useless, it cannot be interrupted — retire
it/disable it and re-develop it.* That message is not a bug report about a
message; it is the whole diagnosis. `--stop` is **answered at a tick
boundary**, and a tick can be a full merge pass — it takes the single-instance
flock, repairs a predecessor's mid-merge repo, and launches a take-over gopi
with its prompt on stdin. An operator who types `--stop` while that is in flight
gets a sentence that says the stop did not happen where they needed it to.

A service the operator cannot stop is not patched in place. It is retired, and
the ground is cleared for the re-development.

## What was retired

The **resident merger daemon** — the `merger` CLI face and its in-process
repeat:

- `gopi merger` (top-level verb) and the **deprecated aliases**
  `gopi task merger` / `gopi task serve` (same runner);
- its tick (`runTaskMerger`), its self-driving loop (`--every D`, the
  single-instance flock held for the loop's whole life), the take-over launcher
  and the take-over progress face;
- the control faces that acted on it (`--stop` / `--restart`), its read-only
  status report (`--status`), and the whole `merger_*.go` implementation family
  they are built on.

## What is NOT retired — and must not be touched by the re-development

`gopi merge` was a **different feature** and was untouched by this retirement. It
was the one-shot merge command (`cmd/agent/merge_run.go`, the `merge_*.go`
build/ceiling/contract/revert/union gates, `task_merge*.go`, `merge_prompt.md`),
and it was where a merge pass happened then.

*(Correction added by the merge-removal epic: the argv entry point this
paragraph listed has since been deleted — the standalone `gopi merge` SURFACE
was removed. The engine itself is still in the tree and still dispatches: the
family spelling `gopi task merge` runs it (measured 2026-10-31 — it landed and
deleted a probe branch), and `man gopi-merge.md` documents it. The history is
not rewritten: this paragraph records what was true when the merger was retired,
and the removal came later.)*

The shared symbols the one-shot face consumed **stay where they are** (the
standalone verb's own file is gone as of 2026-11-01; its readers were not):

- `tools.MergerForcedSafety`, `tools.IntegrationBranchName`
  (`internal/tools/merger_graceful.go`) — the list below named `merge_run.go` and
  `merge_ceiling_rework.go` as readers; BOTH files are gone (measured 2026-11-01:
  they went with the top-level merge verb). `IntegrationBranchName` still has
  dozens of readers (`task_merge.go`, `worktree_sweep.go`,
  `finish_cleanup_survivors.go`, `task_merge_executor.go`, `git_peer_fetch.go`;
  the full list is `grep -rln IntegrationBranchName --include=*.go internal/ cmd/`);
  `MergerForcedSafety` now has none outside its own file — a dead symbol kept
  because deleting it is a separate decision, not because something calls it. The
  live readers of the pair are `task_staleness_guard.go` and `tasks.go`;
- the lock-probe family `tools.LockProbe` / `OrphanLockHolder` /
  `ReadStampedPID` / `LockHolderNote` / `KernelFlockRecords` /
  `InodeOfFileInfo` (`internal/tools/merger_status.go`) — used by the **worker**
  control path (`cmd/agent/cli_worker_control.go`) and `cli_tasks.go`;
- the merge work lock (`merger_work_lock.go`, `mergeFaceProg`) — taken by
  `gopi task merge` and `gopi task adopt` too.

A delete-the-files retirement would have ripped those out from under the worker
and the merge face. This is why the retirement is an **entry-point closure**,
not a file removal (see the next section) — and why a future agent must not
"finish the job" by deleting the kept symbols.

## How the retirement is implemented (and how to revive it)

`cmd/agent/merger_retired.go` owns the ONE statement of cause and the ONE
pointer to what remains live (`retireMergerFace(invoked)`), and is called at
both live entry points:

1. `isMergerSubcommand` (`cmd/agent/main.go`) — the top-level `gopi merger` verb;
2. the `taskverbs.Serve, taskverbs.Merger` case (`cmd/agent/cli_tasks.go`) — the
   deprecated aliases.

The call is the **first statement at each entry**, before the take-over
sub-face, the help scan and the argument parser, so:

- every spelling lands on the same sentence, including `-h` and `--status`
  (none of them reaches the parser — and the argument parser itself is
  untouched, because its grammar is kept for re-development);
- nothing takes a lock, spawns a child, or writes state on the way out;
- the dispatch that used to run is left **verbatim and unreachable** in the
  tree — it is what the re-development starts from.

**To revive the feature: delete the `retireMergerFace(...)` call at each entry
point.** Nothing else is required.

Exit code is 1, and the message names the cause plus the face that still merges,
so nobody concludes that merging itself is gone (Rule 15: a retirement is a loud
refusal, never a silent no-op). *(When this was written that face was `gopi
merge`; measured 2026-11-01 the refusals say `gopi task merge`, because the
top-level verb was removed by the merge-removal epic — a refusal that pointed at
it would now point at another refusal.)*

## Re-development notes (what the next attempt inherits)

- The **uninterruptible tick** is the defect to design against. The retired
  design answered `--stop` only at a tick boundary; the re-development must give
  the operator a stop that takes effect *inside* a pass, or must not run long
  work in a process the operator cannot interrupt. Anything that keeps a
  boundary-answered stop is the same bug with new names.
- The merge JUDGEMENT is already factored out into the take-over **prompt**
  (`merger_takeover_prompt.md`) rather than into the daemon's heuristics, and
  that separation is worth keeping.
- `devdoc/merger-tick-grid.md`, `merger-pure-cronjob.md`,
  `merger-stop-takeover-contract.md`, `merger-loop-restart` (see
  `cmd/agent/merger_restart.go`'s header) and `merger-stop-*` record the
  history — three prior attempts at the stop/take-over contract. Read them
  before designing the fourth.

## Deferred to the re-development — NOW DONE (2026-09-19)

The retirement's first stage deliberately left the retired implementation **in
the tree** (unreachable, still compiling, still exercised by its own tests):

```
merger_loop.go  merger_tick.go  merger_restart.go  merger_takeover*.go
merger_work_lock.go
internal/tools/merger_{status*,schedule*,takeover*}.go
```

Stage 2 — epic `merger-delete-dead-impl`, task
`01a0b48b-000e-7070-8efc-7900358a9561` — **deleted it**, and re-homed every
symbol that had a live caller. `git show` of this file's own branch at the
stage-1 commit, plus the `git rm` list below, is the record.

### Why "unreachable but present" was not good enough

Unreachable code that still compiles is not a re-development head start. It is
a face nobody can reach, kept alive by nothing, which the **next** reader has to
prove is dead all over again — at the cost of the same call-graph analysis, done
by hand, with worse information. The design's record is this document and the
deleted files' own commits; the tree keeps the refusal, not the corpse.

### Deleted (28 files)

`cmd/agent`: `merger_loop.go`, `merger_restart.go`, `merger_takeover.go`,
`merger_takeover_gate.go`, `merger_takeover_lock.go`,
`merger_takeover_progress.go`, `merger_takeover_scope.go`, `merger_tick.go`,
`merger_takeover_prompt.md`, plus the tests of those subjects
(`merger_takeover_{gate,lock,progress,scope,prompt_gate}_test.go`,
`merger_tick_{no_child_gate,scope}_test.go`,
`merger_stop_arm_order_gate_test.go`).

`internal/tools`: `merger_ancestors.go`, `merger_schedule.go`,
`merger_takeover_live.go`, `merger_takeover_progress.go`,
`merger_status_candidate.go`, plus the tests of those subjects.

Two test files were **restored after deletion** — the lesson of this stage:
`cmd/agent/merger_test_rig_test.go` (a process-fixture rig three surviving
suites depend on; its name is historical, its job is not the merger) and
`internal/tools/task_deliverable_{claim_reader,inplace_branch}_test.go` (they
assert on the CLAIM STAMP, and merely reference two status-render constants
that `merger_status_candidate.go` restores). A test file's subject — not its
filename — decides whether it dies with the implementation.

### Re-homed (live callers kept working)

| symbol | new home | live caller |
|---|---|---|
| `mergerWorkLock`, `mergerWorkLockInherited`, `mergerWorkChildMarked` | `cmd/agent/merger_work_lock.go` (rewritten) | `task merge`, `task adopt`, `task merged-cleanup`, `task merge-execute`, `task finish-cleanup`, `task sweep-worktrees` |
| `mergerLockHeldByAncestor`, `inheritedLockHandleForExemptFace`, `mergerLockOwnerEnv` | same file | the ONE acquisition funnel (`acquireServeLock`) |
| `TakeoverLockOwnerEnv` | `internal/tools/lock_owner.go` (new) | the marker's stamper and its kernel-verified reader |
| `AncestorPIDs` / `SelfAncestorPIDs` | `internal/tools/merger_ancestors.go` (**kept**, not deleted) | `LockEvidences`'s ancestor walk |
| `mergerProgTopLevel` | `cmd/agent/merger_retired.go` | the work lock's admission messages |
| `mergerSerialFaceLegs`, `mergerPassOnce`, `mergerSweepRepo`, `mergerResumeDeadHolderLeg`, `mergeDeadHolderReclaimLeg` | `cmd/agent/cli_tasks.go` (**kept**) | the Rule-12 close loop + the dead-holder ladder |

**The last row is the one worth reading twice.** `mergerPassOnce` /
`mergerSweepRepo` are `merger`-NAMED but not `merger`-owned: they carry the
pass-level rebuild invariant (Rule 12's "after a merge, rebuild and verify the
binary follows master") and the dead-holder reclaim ladder, and three
red-proof gate suites assert that wiring by reading those exact declarations
(`cli_tasks_rebuild_{gate,pass,pending}_test.go`). Deleting them because of the
prefix would have removed the only reader of the declared-rebuild marks. A
`merger_` prefix is a **history**, not an ownership claim — check the callers.

### Kept entry points (unchanged behaviour, one gap closed)

Both aliases were already routed to `retireMergerFace`; the `-h` half was.
The final dispatch switch did **not** have a case for `taskverbs.Serve` /
`taskverbs.Merger` — so `gopi task merger --stop` and `gopi task serve`
printed nothing and **exited 0**, the silent no-op the retirement exists to
prevent. Measured before the fix:

```
$ gopi task merger --stop   # exit=0, no output
$ gopi task serve           # exit=0, no output
$ gopi task merger -h       # exit=1, the retirement sentence (help scan)
```

A `case` now refuses both spellings by name (exit 1). This is the regression
that a "the entry points are already closed" reading would have shipped.

## The retired `-h` page — preserved verbatim (resolution B, 2026-09-19)

`gopi merger -h` has **no live help page** any more: the `-h` spelling reaches
the retirement sentence (`merger_retired_face_test.go` pins the exit code). The
page it used to print was the constant `mergerUsageHead` in `cmd/agent/main.go`
(a `var` rendered from `mergerUsageHeadFormat`, whose three shared values came
from `tools.MergerIntervalFlag` / `tools.MergerIntervalDefault` /
`mergerControlFlagSummary()`).

**Why the page is not kept in the tree.** Unreachable code that still compiles
is not a head start — it is a face nobody can reach, kept alive by nothing,
which the next reader has to prove dead all over again (this document's own
"why unreachable but present was not good enough", above). Stage 2
(`93009688`, epic `merger-delete-dead-impl`) therefore **deleted the constant**,
and the five gates that pinned its text were re-aligned in the same change.

**What was NOT done, and is done here.** That deletion preserved the page
nowhere: `git grep 'one tick, or the same tick repeated'` returned zero hits
tree-wide, so the re-development lost the one artifact that recorded exactly
which invocations, flags and Notes the retired face presented. Resolution **(B)**
— *delete it and move its text into this document as the re-development spec* —
was half-applied. The page is reproduced **verbatim** below, from
`93009688^:cmd/agent/main.go`, with the rendered `%s` verbs left in place (the
values they interpolated are named above, so the page can be reconstructed
exactly).

**Read it as a spec, never as documentation of current behaviour.** Every claim
in it is about the RETIRED daemon. In particular: `--once`, `--every D`,
`--status`, `--stop` and `--restart` are **not startable at any spelling** —
`retireMergerFace` refuses before any of them is parsed — and the take-over it
launches no longer exists (deleted with the 28 files listed above). The one
thing worth carrying forward is the shape of the page itself: the invocations
first, options, then a `Notes:` tail, which is what
`cmd/agent/help_capability_first_test.go` now measures on the SURVIVING pages
(`workerUsageHead`, `mergeUsageHead`).

```
GoPi merger — one tick, or the same tick repeated. Guard master, then hand the merge to a take-over gopi.

  gopi merger              ONE tick, then exit 0  (the cron job's whole shape)
  gopi merger --every 5m   repeat that tick every 5m IN THIS PROCESS, until a
                           signal (Ctrl-C / SIGTERM) ends it — self-driving,
                           with the lock held for the loop's WHOLE life
  gopi merger --once       the SYNCHRONOUS tick: the whole merge pass, in-process
  gopi merger --status     read-only: the lock holder, and for a live loop its
                           interval and next tick
  gopi merger --stop       end the live loop, or deliver a fresh one with --every
  gopi merger --restart    stop the live loop, then start a fresh one
  gopi merger -h|--help    this help (starts nothing)

  ONE TICK LAUNCHES AND LEAVES. It takes the single-instance flock, repairs any
  repo a dead predecessor left mid-merge (master safety), then starts ONE
  short-lived gopi with the take-over prompt on its stdin and EXITS — holding no
  child, waiting on nothing, deciding nothing. All merge JUDGEMENT (is a peer
  working? is it dead? is this worktree orphaned? can this branch fast-forward?
  what if no branch merges?) lives in that prompt, not in this process: this
  repo has measured three times that an external "alive or stuck?" heuristic is
  the wrong instrument. The take-over's report goes to
  <gopi-dir>/logs/merger-takeover.log, and the path is printed when it starts.

  %s MAKES THAT TICK REPEAT (self-driving, no cron line needed): the loop is
  tick → sleep → tick, and the flock is taken ONCE at start-up and held for the
  loop's whole life — so "lock held" means exactly one MERGING PROCESS, not
  merely one tick in flight. What comes back is a sleep parameter and nothing
  else: it detaches nothing, stops nothing and acts on no other process, and the
  looped tick is the very tick described above (no child, no watchdog, no retry,
  no peer judgement — only a signal ends it). Its interval has exactly ONE
  source, this flag; omitted, the compiled-in default is %s. With NO %s
  at all — the default — a bare 'gopi merger' runs ONE tick: unchanged, and
  still what a cron line is expected to say.

  A --once tick does the merge pass ITSELF, synchronously, under the flock:
  per configured repo, land every fast-forwardable branch, REPORT the branches
  that do not fast-forward (they are left completely alone: the take-over path
  hands their diff over as a self-contained rework task), refresh the CLI
  contract/test-token maps, clean up already-merged branches, then rebuild
  every repo whose tick landed work. Conflict RESOLUTION is the worker path's
  job, not this process's.

Options:
  --every D          repeat the tick every D — a Go duration (30s, 5m, 1h0m0s);
                     omit it for ONE tick (the cron-shaped default)
  --once             single SYNCHRONOUS tick (the whole pass, in this process)
  --status           read-only: is the lock held; for a live loop, its interval
                     and its next tick
  --stop             END the live loop — SIGTERM to its pid, answered at the
                     tick boundary (the in-flight tick is not interrupted), then
                     exit as soon as the lock is free. With --every D it also
                     DELIVERS a fresh loop, which is what --restart spells.
  --restart          STOP the live loop, then START a fresh one from the current
                     on-disk binary in THIS directory; with no loop running it is
                     a plain start. The fresh loop holds the same flock, so a
                     predecessor that does not leave in time is REFUSED, never
                     raced.
  --db PATH          task ledger override (sweep-backlog rows)

Single-tick atoms (the same work, once, on demand — all still dispatched as
'gopi task <verb>' too; their contract has exactly one home, this page):

  gopi task merge [<branch>...] [--all] [--repo PATH] [--submit]
      Merge the named branches (or every unmerged local branch with --all).
      git is the arbiter: already-merged → branch -d, clean → auto-commit →
      branch -d, hp-unique-* archive-ledger branches exempt, a real conflict →
      merge --abort (master untouched) and, with --submit, ONE merge-conflict
      task enqueued for an agent's semantic judgment.
  gopi task merge-execute <task-id> [--db PATH]
      Run the guarded merge for ONE pending "merge <branch> into master" row:
      clean-tree guard (dirty = skip, row stays pending) → safe-merge guard
      (archive tag / loser prefix / worktree occupancy = failed with reason) →
      git merge --no-commit --no-ff → commit + branch -d + completed writeback.
  gopi task merged-cleanup
      ONE merged-branch cleanup pass: reap branches already integrated but
      still checked out in a linked worktree (guard chain: dirty / live-CWD /
      live-ledger / not-at-tip / ownership) and delete the ORPHAN REFS worktree
      removal leaves behind (ref-only, -d, under the same guards). Prints one
      line per branch.
  gopi task audit-merges [--repo PATH] [--submit] [--db PATH]
      Report the unmerged-branch backlog as a table (BRANCH / AHEAD / TIP /
      DATE); --submit enqueues ONE merge-backlog task per unmerged branch
      (deduped per live row).
  gopi task adopt [--repo PATH] [--master BRANCH] [--submit] [--db PATH]
      Branch-adoption sweep, report-only by default: discover branches on both
      arrival channels (refs/heads and refs/remotes/origin/*), probe-merge each
      into master without committing, then commit + delete the staged-clean
      ones; a conflict aborts and (--submit) dispatches ONE merge-conflict
      task. --repo overrides the swept repo (default cwd), --master the target
      branch (default master). The tick's adoption pass always submits.

Cross-node sync (a peer protocol, not a merge): 'gopi task sync -h' — the
sync/diff/harvest/push verbs and their flags, with 'gopi memsync' for the
patch-file channel.

Notes:
  One tick, or that tick on a period: the host's scheduler decides WHEN a
  one-tick merger runs; with %s the process itself decides how often it repeats
  — the CRON SHAPED DEFAULT is still a single tick, so an existing cron line
  keeps meaning exactly what it meant.
  No flag detaches anything. The loop is the operator's OWN foreground
  process (or a launcher of the operator's own, if it must outlive the
  shell), it holds the single-instance flock, it is reported by --status,
  and it is ended by the signal it documents — so there is nothing hidden
  for a flag to reach. The two control flags (%s) are
  therefore names for the gesture an operator would otherwise perform by pid
  hunt: a stop ends the loop, and a restart replaces it with one running the
  CURRENT on-disk binary.
  A RETIRED detach flag (%s) is
  REFUSED with a named non-zero exit, never silently ignored. %s is NOT among
  them: it is the sleep parameter of the repetition above, not a control face.
  Single instance: a second merger on the same data root is refused while one
  holds the lock — for a LOOP's whole life, not only while a tick happens to be
  running. The AUTHORITY is the flock on "<data root>/serve.lock"; the pid
  stamped in that file is a diagnostic record written by the holder at
  start-up (a stamp can name a dead pid — the lock, never the number, decides
  who is running). The lock dies with the process, so a killed merger blocks
  nothing.
  Log: <gopi data root>/logs/merger.log (default $HOME/.gopi/logs/merger.log).
  Master consistency rests on the per-tick safety sweep, which repairs a repo
  left mid-merge by a predecessor that died — not on anyone waiting: a tick
  that is killed leaves the flock free and the next tick (or the next loop
  iteration) repairs the tree.
  Peer service: 'gopi worker' runs the queued tasks — the merger only merges
  the branches they produced.
  Exit codes: 0 = the tick ran (whatever it found); 1 = it could not run (the
  lock is held by a live merger, or the argv was refused).
  Privileges: the merger needs NO privilege — it is an ordinary process that
  reads and writes git refs it already owns; running it as root obtains
  nothing more than running it as yourself. Detaching is not a privilege
  problem either: within this repo the merger never detaches at all (there is
  no resident daemon face to start), and the other services' honest levers are
  their own --require-scope (refuse the start instead of degrading) or a host
  service unit the operator installs deliberately.
  Sweep targets: the settings.json "repos" table keys (an entry with an
  empty build is a valid pure merge target) — no --repo flag, no cwd
  assumption.
  Aliases: 'gopi task merger …' and 'gopi task serve …' still work, and are
  the same tick (and the same loop, given %s).
```
