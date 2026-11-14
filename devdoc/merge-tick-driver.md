<!-- doc-class: historical -->
# The Resident Merge Tick — what drives the merge pass

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->


> **RETIRED 2026-09-27 — the tick this doc specifies NO LONGER EXISTS (read this
> before copying anything below).** The resident merge tick, its cadence knob
> (`GOPI_MERGE_TICK_MIN` / settings key `merge_tick`), its publisher
> (`config.PublishMergeTickMin`) and the worker-loop call site that spawned
> `gopi merge --fast` as a **blocking child** were **deleted** —
> `devdoc/worker-merge-tick-retired.md` is the report, and the reason is ownership:
> a worker that runs a merge stops running tasks, and two live workers contend for
> the merge flock. What this doc got right and still stands: the merge PASS needed
> driving, the `post-receive` hook only enqueues, and `gopi merger --every D` is
> retired by name. What changed is the answer — **nothing drives unattended merging,
> by design**: `gopi merge` is a foreground command an operator runs, and no
> replacement driver (timer, daemon, `--every`) is to be added (GOPI.md Rule 35).
> The one-shot face, `--fast`, the single-instance lock and the queue are unchanged.
> Setting the retired knob is **reported**, never silently ignored
> (`cmd/agent/merge_tick_retired.go`).

Epic: `merge-tick-driver` (2026-09-24). Branch: `feat/merge-tick-driver`.

## The defect

Nothing drove the merge pass. `gopi merge` (and its git-only `--fast` arm) was
only ever run by a HUMAN:

- `gopi merger --every D` is **retired by name** (`cmd/agent/cli_verbs.go`,
  `CliVerbMerger`), and its lock file header still names it as the withdrawn
  face (`cmd/agent/merge_lock.go`).
- The git `post-receive` hook only **ENQUEUES** a task.
- No cron entry, no systemd unit, no resident loop.

Consequence, measured in the wild: a run completes its work, ends with its own
delivery-point sync commit (preparing its branch to land), and the branch then
sits in `refs/heads` forever. The pile grows until a human sweeps it by hand —
the 188-archive-tag sweep, and `01a0cea7`'s 33 buried branches.

## The choice

Three existing faces were weighed. **The resident `gopi worker` tick was
chosen**; the other two were rejected on what they DRIVE, not on taste.

### ① Rejected: a host scheduler (cron entry / `systemd --user` timer)

This was a real option, not a straw man: on this host `/usr/bin/crontab` exists,
the cron daemon is `active`, a user bus answers, `Linger=yes`, and the repo
already ships a systemd-unit installer for another component
(`scripts/install-scratch-gc-watchdog.sh`, `scripts/systemd/*.timer`). It was
still rejected:

- **It drives blind.** A timer runs one `gopi merge --fast` per interval with no
  knowledge of whether a worker is mid-round, mid-build or draining. The merge
  run holds a root-level lock and rewrites refs; firing it from a scheduler that
  cannot see the machine's state is strictly worse information than firing it
  from a process that *is* the machine's state.
- **It is a second residency model, and this repo has no gopi units.**
  `ls ~/.config/systemd/user/` shows no `gopi*.timer`; `/etc/systemd/system`
  likewise. Adding one means a unit file, an installed copy, an enable step and
  a log — machinery no in-tree code or test can reach.
- **The repo already answered this exact question.** The scratch sweep faced the
  same "periodic maintenance, no scheduler" problem and chose to ride the
  existing tick (`cmd/agent/scratch_gc_tick.go`, whose header argues it at
  length). Consistency with the sibling sweep is itself a reason: one mechanism
  an operator learns once.

### ② Rejected: extending the git `post-receive` hook

Deliberately untouched. It is an enqueue-only face, and a hook that merged would
run inside `git push`'s receive path, blocking the pusher for a full
build + vet + rebuild pass (the merge arm's post-pass verification was one
`go build ./...` + one `go vet ./...` per repo it lands in — measured on this
host at **16.6s** for `go build ./...` alone; that verification was DELETED
2026-11-01 with `internal/tools/merge_pass_verify.go`, and the rebuild pass that
survives is `serveRebuildAfterMerge` → `RebuildAfterMerge`, a repo's DECLARED
build only).

### ③ Chosen: the resident `gopi worker` tick

The process is **already alive** on this host (two live `gopi worker`
processes), and its round already carries throttled passes
(`runTaskLifecycleTick`) and the scratch sweep (`scratchGCSweep`). This adds:

- **zero resident processes** — no new daemon, no cron entry, no timer;
- **no new lock, no state file, no pid file** — the child merge run's EXISTING
  single-instance flock (`serve.lock`, `acquireServeLock`) is the guard;
- **the right trigger semantics**: it ticks whether the queue is busy or
  drained, so it is independent of LLM-task traffic, which is exactly what a
  branch-waiting-to-land needs.

## What it runs

`gopi merge --fast` — the deterministic, git-only arm (`mergeFastFlag`). It
prunes a spent ref, fast-forwards a landable branch, and **reports** a branch
that does not fast-forward without touching it. It never hands anything to an
LLM. That is the hard boundary: unattended LLM merges left the main repo
unmerged on 2026-09-23 (`01a0cde3`), and a conflicted branch needs a judgement
the driver is not entitled to make.

`--fast`'s own semantics are **not changed** — the arm is invoked exactly as it
already is. What changed: its release condition (`no branch was landed into a
branch that diverged`, quoted in `merge_run.go`) stops being "an operator
eventually runs it" and becomes a clock.

## Why a child process, not `runMerge([]string{mergeFastFlag})`

`gopi merge` owns its own single-instance guard, and `flock` lives on the open
file description — **process exit is what releases it**, crash or SIGKILL
included (`cmd/agent/merge_lock.go`). Running it as a child keeps that property
exactly as the foreground arm has it, keeps the run's own stdout in the worker's
log stream, and adds no second lock and no cleanup path.

It is also what makes "two drivers firing close together do not double-merge"
true **by construction rather than by a new rule**: the second child loses the
flock, refuses at its own lock site
(`mergeRefusedNoWorkLine`: *"this run decided NOTHING: no repo was walked, no ref
was written, no branch was deleted, no build ran"*) and exits cleanly.

## Two contract properties (not tuning)

1. **The clock starts with the process.** The first tick of a worker's life
   stamps the clock; the first opportunity to merge is one *full interval*
   later. A driver that fired on its first tick would merge seconds after every
   worker start — and a unit/E2E suite spawns workers, so a suite would mutate
   real repos. `mergeTickLastRun`'s zero value means "just started", and
   `mergeTickSend` stamps instead of firing.
2. **It is skipped on the `--once` face**, riding the same `if !once` guard the
   scratch sweep uses: `--once` is the unit/E2E seam and a test must not merge
   real repos.

It is **fail-open**, like the sweep beside it: the merge pile surviving one more
tick is the worst acceptable outcome, and a driver must never be able to fail
the host task. Every failure is *reported* (Rule 15), never swallowed.

## The knob

| settings key | env | flag |
|---|---|---|
| `merge_tick` | `GOPI_MERGE_TICK_MIN` | *(none)* |

Default **15 minutes**, `0` disables. The precedence is the scratch sweep's
(env wins over settings; the settings layer publishes only into an *unset*
variable), through one publisher (`config.PublishMergeTickMin`) and one reader
(`mergeTickInterval`). A malformed value is *reported* and falls back to the
default — it is explicitly **not** an off switch, because an operator who
mistypes a cadence must not silently lose the driver.

15 is argued against two clocks: a completed row's branch waits at most one
interval (bounding the pile by ~15 minutes of finished work instead of "until a
human notices" — the whole defect), and a run that lands anything costs one
build + vet (16.6s measured here) plus the declared rebuild, so a tick is never
still running when the next is due — and if a tick ever were, the child's flock
makes the overlap a clean refusal.

## Why this file has no flag face

A cadence flag (`--scratch-gc N`) only exists for a verb whose OWN argv reaches
`applyFlag`; the pre-subcommand hoist that runs before the verb chain does not
parse daemon verbs' flags (`config.FlagOverrides` would also fold a positional
verb name into `cfg.Positionals`). Inventing a flag face would either be wired
to nothing or need a new parse path for one knob. The env + settings channels
are complete, so there is no flag. This is stated rather than left as an
apparent omission: a reader who greps for `--merge-tick` finds this paragraph.

## Operator-facing lines

> **Retired mechanism (merge verb removal, 2026-11-01).** The transcript below is
> the RECORD of a past run, kept verbatim — including the `gopi merge --fast`
> invocations and the `gopi merge:` lines it printed. The top-level verb was
> removed afterwards: `./bin/gopi merge` answers **exit 1** today, and `gopi task
> merge` runs the same engine. The driver described here is what was measured then.

The driver's own lines name the driver, the cadence, the exact command and the
log file; the child's lines carry their own `gopi merge:` prefix one line below:

```
[worker] [merge-tick] due (every 15m0s) — running `gopi merge --fast` (git-only unattended arm; its lines follow, and are mirrored to <path>)
gopi merge: holding the single-instance lock; 1 repo(s), ceiling … / … conflicted files per branch
gopi merge --fast: deterministic-only run: land and prune what a fast-forward carries, hand nothing to an LLM, file nothing
… <per-branch lines: landed / pruned / DIVERGED, with the reason>
[worker] [merge-tick] `gopi merge --fast` exited 0
[worker] [merge-tick] any branch the run did not land was reported above by name and left untouched (a branch that does not fast-forward is a judgement call: run `gopi merge` for it)
```

**The diverged-branch answer is the child's own line**, by name and with the
reason (`mergeDetailFastLeft`: *"does not fast-forward — needs judgement; left
untouched for a full `gopi merge`"*). The driver deliberately does **not**
paraphrase branch outcomes: it reports the exit status verbatim and states the
arm's own left-alone property. A driver that re-worded the child's verdicts
would be a second, silently-diverging account of the same run (Rule 2) — and it
would upgrade an exit status into a claim about branches.

## What the operator must still do by hand

1. **A diverged branch needs a human (or an LLM) decision.** The unattended path
   lands only what git alone can land. Run `gopi merge` (no `--fast`) for a
   branch that does not fast-forward; it measures the branch, and either hands
   it to a short-lived LLM run inside a ceiling, or files it as a branch-rework
   task with its diff attached.
2. **`--max-context` / `--max-conflicted-files` are refused with `--fast`** (the
   fast arm measures nothing), so the ceiling knobs are the full arm's.
3. **Nothing else.** The rows keep executing; the merge of finished work no
   longer needs a human.

## Files touched

- `cmd/agent/merge_tick.go` — the driver (throttle, arm, spawn, log lines).
- `cmd/agent/merge_tick_test.go` — the gate: cadence readings, the
  process-start clock, the throttle, the arm (no hand-over flags), the off
  switch, the status + left-alone lines, and the wiring into the worker round.
- `cmd/agent/cli_tasks_worker.go` — the `①″` tick call site, under the existing
  `if !once` guard.
- `cmd/agent/tmpdir_inject.go` — publishes the cadence pre-chain (the group
  verbs return at the subcommand chain and never reach the agent path's config
  block), beside the sibling `PublishScratchGCStaleMin` call.
- `internal/config/config.go` — `MergeTickIntervalEnvName`,
  `MergeTickSettingsKey`, `PublishMergeTickMin`, and the `merge_tick` settings
  field.
- `internal/config/settings.go` — the catalogue row (`gopi --help-env` renders
  from it).
- `CONFIG.md` — the knob's table row, in the same commit
  (`internal/standards/config_doc_gate_test.go` is the gate).

## Not done / honest limits

- **No timer was installed**, by choice — see ① above. If an operator later
  prefers one, the command to schedule is exactly `gopi merge --fast`, and it is
  safe to fire concurrently with the tick because of the shared flock.
- **The driver is not "instant".** It is bounded by the cadence (15 min default;
  the first opportunity after a worker starts is one full interval later).
- **A worker that is not running merges nothing** — the driver lives in the
  worker process, so this is a property to state, not a bug: on this host two
  workers are resident.
