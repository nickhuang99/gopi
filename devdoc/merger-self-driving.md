<!-- doc-class: normative -->
# The merger drives itself — `gopi merger --every D`, in-process, under one lock

Epic: **merger-self-driving** (2026-09-18), successor of
`merger-pure-cronjob` / `daemon-machinery-removal` (2026-09-17).

Branch: `feat/merger-every-loop` (worktree `~/workspace/gopi-merger-every-loop`).

## CORRECTION (2026-09-19, epic `merger-delete-dead-impl`) — dated, originals kept

This document states the flag classification **as of 2026-09-18**, and it was
overtaken twice inside a day. Read every claim below through this block. The
original text is deliberately left unedited (a landed decision is evidence, and
the re-development needs this reasoning); each correction sits where its claim
is, and this block is the single place that says what is true **today**.

**1. `--stop` / `--restart` were RESTORED the day after this doc was written.**
Epic `merger-loop-restart` (`f234658c`, merged `e241b5ee`, 2026-09-18) gave the
resident loop a real control surface again: `--stop` ends the live loop by the
signal the loop already documents, `--restart` stops it and leaves a successor,
and the period is validated before anything is signalled. Since then
`mergerRetiredFlags` has held only **`--daemon` / `--start` / `--wait` /
`--force`**; `--stop` and `--restart` moved to their own class constant. The
record of that restoration is `devdoc/merger-loop-restart.md`. So the table row
below that calls `--stop` / `--restart` "retired" was already false when the
epic that wrote this doc closed — the row now carries its own dated marker.

**2. The whole resident merger is RETIRED — no row of the table is a live face
today.** Epic `merger-delete-dead-impl` closed the `gopi merger` verb
(`f9ff1a89`, 2026-09-18) and then deleted the implementation (`55c4d6f3`, merged
2026-09-19): the loop, the tick, the take-over launcher, the
`--stop` / `--restart` control face and the `--status` surface are all gone from
`cmd/agent`. Every spelling of the verb — `--every`, `--once`, `--status`,
`--stop`, `--restart`, `--daemon`, `-h`, and a bare `gopi merger` — exits 1 with
one recorded sentence (`mergerRetiredCause`, `cmd/agent/merger_retired.go`),
pinned by `TestRetiredMergerFacesRefuseLoudly` and
`TestRetiredMergerRefusalIsNotTheOldMessage`
(`cmd/agent/merger_retired_face_test.go`). The cause, and what was deliberately
kept, are `devdoc/merger-retired.md`. **What was alive at that moment was the
top-level `gopi merge`** — the one-tick pass this doc spells `gopi merger`; that
is also what the "a cron line still means exactly what it meant" paragraph means
now. *(`gopi merge` itself was later removed by the merge-removal epic,
2026-11-01: the one-tick pass is now the family spelling `gopi task merge`,
`man gopi-merge.md` being the engine's own page. The sentence above is kept as
the record of what was true in 2026-09-19, not as an instruction.)*

**3. The test names in this doc are DEAD — checked against master `90e664e0` on
2026-09-19.** Every test and file name below was resolved by hand against the
tree (`grep -rn "func TestX(" --include=*.go`, plus `git grep` at `f9ff1a89^`):

| named in this doc | status today | what holds the claim now |
|---|---|---|
| `TestMergerEveryIsASleepParameterNotAControlFace` | deleted `f9ff1a89` (`merger_loop_test.go`) | nothing — the classification has no live subject |
| `TestMergerRetiredFlagsAreRefusedNotIgnored` | deleted `f9ff1a89` (`merger_retired_flags_test.go`) | `TestRetiredMergerFacesRefuseLoudly`: the whole VERB refuses now, not a flag class |
| `TestMergerBareRunIsTheSupportedSpelling` | deleted `f9ff1a89` (same file) | nothing — a bare `gopi merger` is refused; the bare tick it was pinning was then the top-level `gopi merge`, itself removed 2026-11-01 (the one-tick face today is `gopi task merge`) |
| `TestMergerLoopHoldsTheLockForItsWholeLife` | deleted `f9ff1a89` (`merger_loop_e2e_test.go`) | `merger_work_lock_e2e_test.go`: `TestMergerWorkLock_MergeFacesRefuseWhileAHolderRuns`, `TestMergerWorkLock_RefusesInProcess` |
| `TestMergerLoopKeepsTheLockAcrossASynchronousTick` | deleted `f9ff1a89` (same file) | same two cases as above |
| `TestMergerLoopSharesItsSpellingsWithTheStatusReader` | deleted `f9ff1a89` (`merger_loop_test.go`) | nothing |
| `TestMergerOnceLeavesPendingTaskUntouched` | deleted `55c4d6f3` (its file `serve_worker_split_test.go` went with the surface) | — |
| `cmd/agent/merger_loop.go`, `merger_loop_test.go`, `merger_loop_e2e_test.go`, `merger_tick_no_child_gate_test.go` | all deleted (`f9ff1a89`; the first three also in `55c4d6f3`) | — |

A future reader can re-derive the table with
`git grep -n "func TestX(" <sha> -- cmd/agent` and `git log --oneline <sha> -- <path>`.

## The one decision this epic turns on: flag CLASSIFICATION

The 2026-09-17 epic deleted the merger's whole resident machinery *and*
`--every` with it, on one argument: "a one-tick process has no poll period".
That argument is sound about *control faces* and wrong about *sleep parameters*,
and the two were retired together. Restoring self-driving means restoring
exactly one row of the table:

| flag | class | 2026-09-17 | now (= 2026-09-18) | 2026-09-19 (dated correction) | why |
|---|---|---|---|---|---|
| `--every D` | **sleep parameter** | retired | **admitted** | **retired** with the whole verb (`f9ff1a89` + `55c4d6f3`) | changes how often the SAME process repeats the tick; detaches nothing, stops nothing, acts on no other process |
| `--daemon` | control/detach | retired | retired | retired | creates a resident process nobody can see |
| `--stop` / `--restart` | control | retired | **restored** the NEXT DAY by epic `merger-loop-restart` (`f234658c`) — `mergerRetiredFlags` never held them after that | **retired** with the whole verb | act on a resident process from outside |
| `--start` | control | retired | retired | retired | same class as `--daemon` |
| `--force` / `--wait` | control | retired | retired | retired | same class; `--wait` even waits on one |
| `--once` | execution mode | kept | kept | **retired** — the one-tick form is named in the correction at §3.9 (it was the bare `gopi merge`; that verb is removed today, see the block at the top) | the synchronous seam (§3.9 of the prompt) |
| `--status` | read-only | kept | kept | **retired** — no status surface survives | reads the lock; takes nothing |

The two `now` cells the original table got wrong are marked **in the row itself**,
so a reader who greps for the flag instead of reading the header still sees the
correction (the dated block above carries the full account).

The table is enforced, not described. **Dated correction (2026-09-19): none of
the three guards below exists any more** — they were written for the loop and
went with it (`f9ff1a89`). The names are kept because a reader of this doc must
be able to find out what happened to them: `TestMergerEveryIsASleepParameterNotAControlFace`
and `TestMergerRetiredFlagsAreRefusedNotIgnored` (every retired flag still exits
non-zero naming itself) and `TestMergerBareRunIsTheSupportedSpelling` lived in
`merger_loop_test.go` / `merger_retired_flags_test.go`, both deleted at the
retirement. What enforces the classification NOW is the retirement itself: the
whole verb is closed and `mergerRetiredCause` is the one sentence every spelling
prints (`TestRetiredMergerFacesRefuseLoudly`).

**A cron line still means exactly what it meant.** `gopi merger` with no
`--every` is ONE tick — the compiled-in default, stated in the usage head, and
pinned by `TestMergerBareRunIsTheSupportedSpelling` plus the two pre-existing
E2E cases that drive a bare tick. **Dated correction (2026-09-19): the spelling
moved, the meaning did not** — the one-tick form is now the bare `gopi merge`,
and `gopi merger` answers with the retirement sentence. The pinned test names
are dead (see the block at the top); the live one-tick face is
`cmd/agent/merge_run.go`. *(Re-dated 2026-11-01: BOTH spellings named above are
now gone — `gopi merger` refused from 2026-09-19, and the top-level `gopi merge`
was removed by the merge-removal epic, so `merge_run.go` is itself deleted. The
one-tick form a reader can run today is `gopi task merge`.)*

## Shape: the tick is unchanged; the loop is a `for`

> **Dated correction (2026-09-19): this section describes code that is DELETED.**
> `cmd/agent/merger_loop.go`, `merger_tick.go` and the take-over launcher no
> longer exist on master (`55c4d6f3`); the merged loop is history, kept here as
> the design record the re-development is meant to start from. The lock, the
> one-tick face (then `gopi merge`, removed 2026-11-01 — today `gopi task merge`)
> and the `--status` reader's package
> (`internal/tools/merger_status.go`) survive.

`cmd/agent/merger_loop.go` is the whole new feature. One iteration:

```
① [merger] tick start HH:MM:SS          ← the status surface's boundary
② runTaskMergerWithLock(opts, lock)     ← THE tick, byte-identical to the cron form
③ stop-signal check                     ← non-blocking, between work and sleep
④ [merger] sleeping 5m0s from HH:MM:SS  ← the idle edge (measured anchor)
⑤ sleep(interval) — or a stop signal
```

There is **no child handle, no watchdog, no retry, no peer-liveness judgement,
no state file, no stop-request file**. The loop contributes the word `for`, two
log lines and the signal ladder. Merge *judgement* still lives entirely in
`merger_takeover_prompt.md`.

A stop signal is **not** a control face. SIGINT (the terminal this loop runs in)
and SIGTERM (`kill`, a service unit's ExecStop) are answered because a
foreground loop must be interruptible; there is no request file, no watching
goroutine, no per-tick flag check, no `--stop` verb, and no state a status query
reads. The retired `SIGUSR1/SIGUSR2` pair is deliberately NOT handled.

## The measured lock window — the §3 hole, closed and quantified

**The hole.** `merger_takeover_prompt.md` §3.9 told the take-over: *"the
launcher released its lock when it exited, so you do not hold it"* — and
prescribed `gopi merger --once` as the way to serialise its git writes. With a
resident loop, that sentence became FALSE: the loop holds the flock for its
whole life, so the child's `--once` is refused every time. Left alone, every
take-over would have taken the "somebody else is mid-write — skip" branch for a
lock its own launcher never releases.

**The measurement** (this host, `measure.sh`, stub take-over child,
`~/workspace/_tmp/merger-lock-measure`):

```
$ gopi merger --every 20s &            # the loop
loop pid=3460561 ppid=3460555 pgid=3460554 sid=2034244
cgroup: 0::/user.slice/user-1000.slice/session-18257.scope
stub pid=3460579 argv0=gopi-merger-takeover      # the child the tick started

$ cat home/.gopi/serve.lock            # stamped pid
3460561                                 # = the LOOP's pid, not a tick's

$ flock -n home/.gopi/serve.lock -c 'echo FREE'
FLOCK BUSY — the lock IS held by a live process (the loop, pid 3460561)

$ kill -TERM 3460561 ; sleep 3
loop exited on SIGTERM
$ flock -n home/.gopi/serve.lock -c 'echo FREE'
   FLOCK FREE — the lock died with the process
```

Two facts, both load-bearing:

1. **Held across the loop's whole life, including the sleep window** — the probe
   was fired *after* the loop's own `[merger] sleeping …` line, i.e. when no tick
   was in flight. A per-tick lock would have been free exactly then. Pinned as a
   test: `TestMergerLoopHoldsTheLockForItsWholeLife` waits for the sleep edge,
   *then* fires the second merger and requires the refusal to name the loop's own
   pid.
2. **The kernel's `/proc/locks` prints no FLOCK record for this file** (it is on
   an overlay fs; the same absence was already observed for the daemon). The lock
   is nonetheless real — the independent `flock -n` probe above failed while the
   loop lived and succeeded the instant it died. **The authority is the flock;
   the stamped pid is a diagnostic.** This is why `--status` reads both and why
   the refusal text names the pid rather than trying to prove the lock from
   `/proc`.

**The repair** is in the artifact, not in Go: the briefing frame
(`mergerTakeoverBriefingHeaderFormat`) and §3.9 now tell the child to MEASURE
which form started it (`gopi merger --status`) instead of assuming the lock is
free, and state explicitly that the loop's refusal is **a skip, not a deadlock,
and never a reason to kill the holder**. Nothing in the merge machinery changed —
which is the point: the judgement stayed in the prompt, and the prompt was wrong.

## `--status` reports a LIVE loop

> **Dated correction (2026-09-19): the loop half is gone and so is the CLI
> `--status` face.** `internal/tools/merger_status.go` and its readers survive in
> the package, but no verb prints them; `tools.MergerIntervalFlag`,
> `merger_schedule.go` and the guard below (`TestMergerLoopSharesItsSpellingsWithTheStatusReader`)
> went with the deleted loop.

`internal/tools/merger_schedule.go` already read a live holder's argv for
`--every` and its log tail for the tick/sleep boundaries (`MergerTickMarker`,
`MergerSleepLineFormat`, `MergerClockLayout` — shared constants, Rules 13/14);
the CLI already rendered interval + next tick. The loop now actually writes
those lines, so the surface works end to end — asserted in the E2E: while the
loop is live, `--status` prints the interval (`6s`) and a `next tick:` line, and
does **not** print `merger:      not running`.

`tools.MergerIntervalFlag` was added as the EXPORTED alias of the reader's
`mergerIntervalFlag`, because `cmd/agent` is now a second consumer of the same
token — the flag spelling lives in exactly one place, and
`TestMergerLoopSharesItsSpellingsWithTheStatusReader` fails if either side
drifts.

## Ownership details that had to move with the loop

> **Dated correction (2026-09-19): the merger-side subjects of all three bullets
> are deleted.** `merger_loop.go` (`mergerResidentLoop`), the merger's
> `runTaskMerger`/`mergerEveryInterval` reader and the `--status` reader's
> `merger_schedule.go` are gone. The helpers named here survive for the WORKER
> face (`attachHostLifecycleLog`, `ensureServeLockIdentity` in
> `cmd/agent/cli_tasks.go`), and the merger's lock file lives on in
> `cmd/agent/merger_work_lock.go`. The design reasoning is kept because the
> re-development will hit the same three ownership questions.

- **The declared lifecycle record belongs to the longest-lived face.**
  `attachHostLifecycleLog` / `closeHostLifecycleLog` moved from the tick into the
  loop, and the tick attaches only when nobody has (`hostLifecycleLogAttached()`).
  Without this, the tick's own `defer closeHostLifecycleLog()` closed the file
  when the tick returned, so the lines the loop prints *between* ticks — the
  boundary and the sleep edge, i.e. exactly what `--status` reads — never reached
  `<gopi-dir>/logs/merger.log`.
- **The lock-identity guard runs once, for the loop's life.**
  `ensureServeLockIdentity` (unchanged, not weakened) is called by the loop
  before the first tick; a lock file replaced underneath the loop is repaired or
  the process stops rather than merging unguarded.
- **`--every` is validated before the lock is taken**, by its one reader
  (`mergerEveryInterval`). `--every 5` is refused by name with exit 1 — a bare
  number is 5ns to Go's parser and 5s to a human, and either guess is a merger
  that spins or sleeps for a year.

## Gates updated (deliberately, with reasons in the diff)

> **Dated correction (2026-09-19): both gate edits were superseded.** Neither
> file survives: `merger_tick_no_child_gate_test.go` was deleted with the tick
> (`f9ff1a89`), and `cli_tasks_advert_gate_test.go`'s flag list now carries
> `--daemon --every --force --wait` (and no merger verb to advertise them) after
> the retirement. What the loop's own gates enforced is now enforced by the
> retirement face's two cases.

- `merger_tick_no_child_gate_test.go`: `merger_loop.go` joins the tick file set
  with `mtgForbiddenOwnFace` (no `.Wait()`, no registry, no in-process LLM), plus
  presence markers (`mergerResidentLoop`, the repeated
  `runTaskMergerWithLock(opts, lock)`, `mergerStopSignals`, `mergerEveryInterval`)
  so "delete the loop again" cannot pass the absence half for free.
- `cli_tasks_advert_gate_test.go`: `--every` returns to
  `documentableServiceFlags` — the list's own rule is "a flag some face
  accepts", and `gopi merger --every D` accepts it again.

## A real defect the loop surfaced: `--every D --once` released the loop's lock

Writing the loop's E2E found a genuine bug in the *existing* tick code — one
that only becomes a bug once a tick can run more than once in a process.
`runTaskMergerWithLock`'s `--once` branch ended with `defer lock.Close()`, which
is correct for a process about to exit and **wrong under a loop**: each tick
released THE LOOP's flock on return, then the loop slept and re-merged with the
single-instance guard down — `--status` would report a lock nobody held, and a
second merger would be accepted mid-loop.

Reproduced first, as a test
(`TestMergerLoopKeepsTheLockAcrossASynchronousTick`): drive
`gopi merger --every 6s --once` in a hermetic home, wait for the loop's sleep
edge (i.e. the tick has returned), then fire a second merger. Before the fix:

```
--- FAIL: TestMergerLoopKeepsTheLockAcrossASynchronousTick
    a second merger was ACCEPTED while the `--once` loop slept — the tick released the loop's lock
```

**Fix — the acquirer owns the lifetime.** `ensureServeLockIdentity` is still
called by the tick (unchanged guard), but the `defer lock.Close()` moved out to
the two faces that ACQUIRE: `runTaskMerger` (`defer lock.Close()`, one tick) and
`mergerResidentLoop` (`defer lock.Close()`, the loop's whole life). The reasoning
is written at the call site so the next reader does not re-add it.

## What the FULL suite caught that the targeted runs did not

Four failures, all mine, all fixed at the cause (no ceiling raised, no permit
added) — recorded here because three of them are the kind a targeted run
structurally cannot see:

1. **A help TEMPLATE measured as text.** `mergerUsageHead` became a rendered var,
   and the width sweep rendered it with *shotUsage's* arguments — so it measured
   `%!s(int=2)` (8 chars, `int=2`) instead of "5m0s". Fixed by making the render
   step data-driven: `helpTemplateArgs` is keyed by constant name and a template
   with no entry is a FATAL failure. The lesson generalizes: a test that RENDERS
   a format string must be given the real arguments, or its green is empty.
   The two over-budget lines are rewrapped; the page's one-line title carries a
   reviewed `help-width-ok` escape like the other pages' headers.
2. **A gate that was RIGHT.** The ledger family usage restated `--every`. That
   contract's one advertised home is `gopi merger -h`, so the family text now
   points there instead of naming the flag. The gate is unchanged.
3. **A spawner that skipped the repo-context sandbox.** Both new loop E2E cases
   started a REAL merger whose child inherited the LIVE repository as its cwd —
   its own-worktree-release pass could have enumerated and freed the developer's
   worktrees. `SandboxRepoContext(t)` in both. This is the hazard the guard
   exists for, and a targeted run of my own new tests would never have shown it.
4. **A re-keyed baseline entry.** Lines added above `main()`'s `@` payload site
   shifted it; the baseline key went STALE (which the gate reports as an error on
   purpose — a stale permit is a silent permission). Re-keyed with the reason.

## Not done (honest)

> **Dated correction (2026-09-19): every subject below is deleted, so the three
> `Not done` items are moot (kept as the record).** `TestMergerOnceLeavesPendingTaskUntouched`
> and its file went with the surface (`55c4d6f3`); `internal/tools.LockEvidences`
> survives but nothing reads it through a CLI face; the take-over child and its
> prompt (`merger_takeover_prompt.md`) are gone. What remains actionable for the
> re-development is the first item's LESSON, not its fix: a field that graduates
> from inert filler to meaningful must be grepped across the suite.

- **A pre-existing test the loop turned into a hang** — found by the FULL suite
  after the targeted runs were green, fixed here: `every: "1s"` in
  `TestMergerOnceLeavesPendingTaskUntouched` was inert daemon-era filler and
  became an infinite 1-second loop (22m16s, killed by the package timeout). See
  the report's `## Not Done` and the commit `fix(test): a stale daemon-era
  literal turned an acceptance test into a 22-minute hang`.
- `/proc/locks` stays silent about this lock on this host's filesystem (measured
  above). `internal/tools.LockEvidences` therefore cannot show a kernel FLOCK
  row for a merger loop here; the status surface says so rather than inventing
  one. Not fixed, not fixable in gopi: it is a property of the lock's filesystem.
- The take-over child does not itself re-probe the lock before §3.9; it is
  *instructed* to (`gopi merger --status` first). Making it mechanical would put
  a lock probe into the agent's judgement path — the thing this design keeps out
  of Go.

## Verification

> **Dated correction (2026-09-19): the `Verification` block below records the
> 2026-09-18 run, and it cannot be re-run — the tests it names are deleted with
> the loop.** It is kept as the record of what that epic proved.

- `go build ./...`, `go vet ./cmd/agent/ ./internal/tools/`: clean.
- `go test ./cmd/agent/ ./internal/tools/ ./internal/standards/ ./internal/stylegate/ ./internal/config/`:
  see the suite log pasted in the report (`## Verification`).
- New tests: `merger_loop_test.go` (grammar, shared spellings, classification),
  `merger_loop_e2e_test.go` (real loop: refusal in the sleep window, lock kept
  across a synchronous tick, `--status` on a live loop, SIGTERM ends it, no stale
  lock afterwards).
- Red proofs run: the tick gate FAILS on an injected `.Wait()` in
  `merger_loop.go` (reported `merger_loop.go:283 carries ".Wait()"`), green once
  removed; `TestMergerLoopKeepsTheLockAcrossASynchronousTick` FAILED before the
  lock-ownership fix and passes after.

## Reproducing the lock measurement

The measurement is a script, not a memory — `~/workspace/_tmp/merger-lock-measure/measure.sh`
(kept OUTSIDE the worktree, PROJECT.md Rule 30). It:

1. builds a stub "take-over" binary that records its pid and sleeps;
2. builds a hermetic `GOPI_HOME` + settings.json whose `repos` table names one
   throwaway git repo, and points `GOPI_BINARY` at the stub;
3. starts the real `gopi merger --every 20s` under it;
4. prints the loop's pid/cgroup, the stub's pid, the stamped lock pid, the
   kernel's `/proc/locks` row (absent here — that is a finding, not a failure),
   and probes the lock twice: `flock -n` while the loop lives (BUSY) and after
   SIGTERM (FREE);
5. prints the tick/sleep line counts, which is how "the loop is turning" is
   established without trusting a single line.

