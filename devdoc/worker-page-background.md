<!-- doc-class: historical -->
# The `gopi worker` page — background moved off the page

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class: historical** (no current artifact cites this file, so the generator
classes it a past run's record; the banner above is the machine-checkable
half of the same claim). This is the standing background of the `gopi worker` help
page (`workerUsageHead`, `cmd/agent/main.go`). Slice 3/5 of the "one page = one
level" epic (`01a10bb7`) moved the reasoning off the page and left a pointer.
Nothing was deleted — this file IS the moved prose, verbatim.

## The faces, as the page described them

`--once` stays in the foreground until the queue is empty, then exits 0 (a bare
`gopi worker` is the same drain; `--once` is the unambiguous spelling for a
launcher script). NOT a one-task mode: use `<task-id>` for a single row.

`<task-id>` runs exactly ONE row in the foreground, then exits — the manual /
re-run / jump-the-queue entry. It never touches the rest of the queue. Use it to
re-run a failed row, to start an urgent one now, or as a scripted single-row
runner (exit 0 completed, 1 not).

`--resume` resumes a row that ALREADY has a worktree, and cleans it up. Name the
row (`gopi worker --resume <task-id>`, the default) and its recorded ground is
resolved; or name the ground directly with `--branch`/`--worktree`. It never
creates a worktree — ground that does not exist is a refusal. `--resume` is the
ONLY face that removes the worktree a finished row delivered from, because it is
the only one not standing in it: the resume itself runs in a CHILD (as the
operator would run a resume by hand), and the close-out chain then runs from
here. `--branch B` takes the repo from the ledger rows naming B; `--worktree P`
takes the ground by path — what `pwd` prints inside it.

## The environment knobs, in full

    GOPI_WORKER_MAX_ROUNDS=N    rounds before exit (default 0 = unlimited)
    GOPI_WORKER_IDLE_SEC=N      seconds between re-checks on a drained
                                queue (default 30; --once exits instead)
    GOPI_WORKER_IDLE_EXITS=N    consecutive empty rounds before the worker
                                exits 0 (default 3 ≈ 90s; 0 = never, kill
                                to stop)

## Why the page is the shape it is

The rule this slice applies: **a help page lists what the reader can type, one
line each, and defers the reasoning to a document that is read on purpose.** The
worker page keeps the synopsis, the flag list, the environment, and one line per
exit code — the exit-code paragraph is a CONTRACT (it is read by the
agent-spawn-guard tests), so it stayed on the page with the exact renderings
those tests pin: `Exit code: 2`, `Exit code: 3`, `exits 0`, `1 when it failed`,
and the `-h` anchor the refusal points the reader at. What moved here is the
prose that only matters to someone CHANGING the verb: the faces' relative
purpose, and the reason the output seam is where it is.

Measured on this slice (real binary, `./bin/gopi worker -h | wc -l`, at branch
tip `048ef7a2`): **106 lines before, 97 after** — the two extra lines over the
first count are the pointer block this file is named in. The before figure comes
from the same binary built at that commit without the four page edits
(`git worktree add --detach <path> HEAD` + build), so both numbers are real
renders of the same compiler, not estimates.

## The selector family's MECHANISM (moved here 2026-10-13, task 01a13356)

`b95f24ec1` raised the page's budget 114 → 136 to make room for the worker's
TARGET SELECTOR family. The two usage ROWS were the reason; the mechanism that
came with them — the request-file channel, the resolution chain, the spawn seam,
the close-out chain — was not. The page is a page that ROUTES, so the mechanism
moved to this file and to `man/gopi-worker`, and the budget came back down to
**114**, the page's own measured length (the raise's room for one new knob's
paragraph is `usageBudgetNewFlagMargin`, and nothing here is one). Both
`gopi worker --stop --branch B` and `gopi worker --stop --pid N` are still
advertised on the page: the page must advertise a form before the G6
help-surface gate may type it.

**Every stop is asked, not killed, and the ask is durable.** A `--stop` or
`--restart` writes a REQUEST FILE (`TERM_<pid>`) for EVERY worker, detached or
foreground, and the worker reads it at its claim boundary — so the in-flight task
finishes first, and a worker asked before it started a round still sees the
request. A signal is added only for a detached daemon whose binary we can verify
is this one; the request file is the delivery, and the foreground worker's own
process group is what carries it to its children. The page's
`finish the in-flight round, then exit; the worker is asked, not killed` is this
paragraph in one line.

**A selection is resolved through the LEDGER, and a refusal stops nothing.** The
row that owns the branch, that row's claim stamp, the holder pid in it — the
chain is reported as it is walked. A branch no row owns is refused BY NAME, and
a pid that is alive but not a gopi worker is refused by name too; either refusal
happens BEFORE any request file is written, so a selection that fails has
stopped nothing. A branch selection for the RESUME face is a different question
with the same word (`--branch` is the resume's GROUND); that collision and its
deferral are `devdoc/worker-control-target-selectors.md`.

**Where a run's own text goes is decided by the spawn seam, not by the
launcher.** A task's output lands in `<gopi-dir>/sessions/<task-id>/output.log`,
because the seam that spawns the run computes the path — a launcher's stdout is
not the destination, which is why a detached `--daemon` run (whose stdout is
`<gopi-dir>/logs/worker.log`) still writes the task's output where the task's own
session directory is. The worker's OWN output — the round lines, the claim, the
close-out — stays on the stream it was launched with, so a foreground run is
readable as it happens.

**`--resume` is the one face that runs the close-out chain from a process that
is not standing in the ground:** the resume itself runs in a CHILD (as the
operator would run a resume by hand), and the close-out chain then runs from
here. That is the whole reason it can remove the worktree a finished row
delivered from. The chain protects a dirty worktree and an unfinished row exactly
as it always has — a refusal is reported with its reason and never rewrites the
resume's outcome.
