---
title: GOPI-WORKER
section: 1
header: GoPi Manual
---

# NAME

gopi-worker - run the queued tasks, then exit

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi worker                 run tasks until the queue drains, exit 0 by itself
  gopi worker --once          same drain; exits as soon as the queue is empty
  gopi worker [<task-id>]     run THAT row and nothing else, then exit
                              (exit 1 if it failed)
  gopi worker --resume <task-id>       finish THAT row's own ground (a CLONE when
                              dev_repo is declared) and PUBLISH it in the close-out
  gopi worker --resume --branch B    the same, when you hold the BRANCH
  gopi worker --resume --worktree P  the same, with P naming the ground
  gopi worker --daemon        same loop, detached (returns at once; logs to
                              <gopi-dir>/logs/worker.log)
  gopi worker --status        what is running, and WHICH TASK each one runs
                              (read-only; a bare gopi worker is a FOREGROUND
                              worker — listed by pid)
  gopi worker --status --json  the same read as one document (see Options)
  gopi worker list            the same read, spelled as a question — the alias
                              to reach for when you want to know which workers
                              are running (identical output to --status)
  gopi worker --stop          finish the in-flight round, then exit; the worker
                              is asked, not killed (see Options)
  gopi worker --stop --branch B   stop the worker that holds branch B (the row
                              owning B → its claim stamp → that holder's pid)
  gopi worker --stop --pid N  stop that one worker, as --status listed it
  gopi worker --restart       --stop, then --start (FOREGROUND workers are
                              reported, not signalled: a restart replaces the
                              daemon and never claims to stop them)
  gopi worker --start         start a detached worker (refused if one runs)
  gopi worker -h|--help       this help (claims nothing)
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi worker** is the ONLY command that runs a task row. `gopi task` manages the
ledger — submit, list, show, verify, reopen — and runs nothing; this is the
executor. There is no daemon to babysit: the command drains the queue and exits
by itself.

One round claims one pending task, runs it, records the result, syncs with the
other nodes, then claims again. A lost claim race yields to the next round, so
two workers never run the same row.

**Only the queue faces loop.** A bare `gopi worker` and `gopi worker --once` are
the same drain; a NAMED row is one round and then exit — it never claims a second
row. Rows tagged `pa-queue` are never claimed by the queue (they are for human or
other-dispatcher handling); naming such a row as the argument selects it
explicitly, which is also the way to run a row the queue would never hand out.

**A drained queue is success.** The loop exits 0 whatever the rows it ran did —
read the rows for their outcome. Naming a row reports that row: 0 when it
completed, 1 when it failed or could not be run.

**Privileges.** A worker needs no privilege, `--daemon` included: running it as
root obtains no more than running it as yourself.

**Resume** is the only face that REMOVES the worktree a finished row delivered
from, because it is the only one not standing in it — the resume itself runs in
a child, and the close-out chain then runs from here. The row is the default
selector (its recorded worktree and branch are the ground); `--branch` /
`--worktree` name the ground directly. The chain protects a dirty worktree and
an unfinished row exactly as it always has, and a refusal is reported with its
reason.

**A launch that is refused never runs anything**, and reports itself with its own
exit code rather than a generic failure.

# OPTIONS

`--once`
: stay in the foreground until the queue is empty, then exit 0. The
unambiguous spelling for a launcher script.

`<task-id>`
: the row to run, as a bare ARGUMENT — run exactly this one row in the
foreground, then exit.

`--takeover`, `--as HOLDER`, `--db PATH`, `--report-to ALIAS`, `--repo TARGET`
: the named-row companions: run a row a live process already holds, claim and
report under a given holder identity, choose the ledger, name the ssh alias a
dispatched run reports back through, and name the git push-back target for the
finished branch.

`--resume`, `--branch B`, `--worktree P`
: resume a row that already has a ground, and close it out — naming the row, or
naming the ground by branch or by path. The ground is whatever the row's first
run worked in: with `dev_repo` declared it is a CLONE and the close-out
PUBLISHES from it (a push that does not land leaves the row OPEN with the
reason); without one it is a worktree, where the commit is already on the branch
and there is nothing to push.

`--stop`, `--restart`, `--status` with `--branch B` or `--pid N`
: address ONE worker instead of every worker of the profile. `--branch B` is
resolved through the ledger: the row that owns B, its claim stamp, that holder's
pid — a branch no row owns is refused BY NAME, and never falls back to "every
worker". `--pid N` addresses the process `--status` listed; an alive process that
is not a gopi worker is refused by name and never signalled. A selector that
resolves nothing is refused BEFORE any request file is written, so a selection
that fails has stopped nothing.

**`--status` names WHICH TASK each worker is running.** Every line carries a
stable `task=<id>` field, second on the line for both worker shapes, so one grep
answers "which task is pid N running". The value has ONE derivation order, and
the source says how much to trust it: the daemon's own STATE RECORD first, but
only while a round is in flight (the writer clears the task id at every round
end); then the worker's OWN `/proc/<pid>/cmdline`, parsed through this verb's
own grammar scanner — so a flag operand (`--repo <path>`, `--db <file>`,
`--report-to <alias>`) is consumed by the flag that owns it and a repo path can
never be reported as a running task; then the ROUND'S OWN WORKING DIRECTORY
(`/proc/<pid>/cwd`) when it is a task's session container directly under this
profile's sessions root and its name passes the task-id shape gate — the source
a plain loop worker has instead of a state record, because the round moves into
that container at its claim boundary and back out at its end; and finally the
declared marker `task=(not recorded)`, which is rendered and never omitted,
because a blank field is indistinguishable from a renderer that forgot it. A
loop worker between rounds is honestly "not recorded" — it is running no row.

`--json`
: with `--status`: the SAME read as ONE machine-queryable document instead of the
human lines, so the task is a VALUE rather than a field to parse:

      gopi worker --status --json | jq -r '.workers[] | "\(.pid)\t\(.task)"'

Every worker carries `pid`, `shape`, `state`, `task`, `task_source` and
`task_recorded`; `task_recorded` is the boolean a consumer switches on, because
comparing against a prose marker would be a coupling to text, and `task_source`
(`state` / `argv` / `none`) says how much to trust the value. The document is
emitted by the same read-only enumeration the text form uses — nothing is
created, removed or signalled — and `--json` is a MODIFIER of the read, never a
read of its own: a bare `gopi worker --json` is an unknown word (a misspelled
worker launch must not become a successful status report), and
`--status --json --stop` is refused as a read+act combination.

`--daemon`, `--start`, `--restart`, `--stop`, `--status`, `--status-verbose`
: the detached faces. At most ONE detached worker per profile; `--daemon` returns
only once the worker is really up, and with `--require-scope` a start that could
not survive your login session FAILS instead of detaching a worker that quietly
exits with your shell.

A FOREGROUND worker is not a detached one: a bare `gopi worker` takes no lock and
writes no daemon state file, so `--status` finds it in the process table (listed
by pid) and `--stop` reaches it with the same graceful `SIGTERM` the detached
worker gets. The signals are sent only to a process whose executable is provably
this same binary; when it is not, the pid is named with the command that stops
it instead. `--restart` reports the foreground workers it found and does NOT
signal them — replacing the daemon is not a request for them to leave.

**Every stop is asked, not killed, and the ask is durable.** A `--stop` or
`--restart` writes a REQUEST FILE (`TERM_<pid>`) for EVERY worker, detached or
foreground, and the worker reads it at its claim boundary — so the in-flight task
finishes first, and a worker asked before it started a round still sees the
request. A signal is added only for a detached daemon whose binary we can verify
is this one; the request file is the delivery, and the foreground worker's own
process group is what carries it to its children.

**Output goes where the task's own seam decides, not where the launcher is.** A
run's text lands in `<gopi-dir>/sessions/<task-id>/output.log`, while the worker's
own output — the round lines, the claim, the close-out — stays on the stream it
was launched with, so a foreground run is readable as it happens.

`--force`, `--wait DURATION`
: with `--stop` / `--restart`: interrupt the round now, and bound the graceful
wait (default 25 minutes; 0 = no wait).

# ENVIRONMENT

`GOPI_WORKER_MAX_ROUNDS`
: rounds before exit (default 0 = unlimited).

`GOPI_WORKER_IDLE_SEC`
: seconds between re-checks on a drained queue (default 30; `--once` exits
instead).

`GOPI_WORKER_IDLE_EXITS`
: consecutive empty rounds before the worker exits 0 (default 3, about 90
seconds; 0 = never, kill to stop).

# EXIT STATUS

`0`
: the queue drained, or a named row completed — and the read-only faces
(`--status`, `-h`, `--stop`, `--restart`) answer and return without a lock.

`1`
: a named row failed or could not be run.

`2`
: an argument this verb does not accept.

`3`
: the agent-spawn guard refused the launch — an agent session must never start
this loop. The refusal says so and names both operator escapes.

`4`
: the PRE-claim API reachability preflight found that this node cannot reach
the LLM endpoint, so **nothing was claimed**: the queue is untouched and every
row is still `pending`. The measurement — the endpoint, each bounded attempt's
duration and error, and the remedy — is printed on the stream above this code.

The preflight is three CONSECUTIVE bounded TCP connects to the configured
endpoint (`apiPreflightAttempts` × `apiPreflightConnectTimeout`, spaced
`apiPreflightRetryBackoff`); a single success anywhere in the ladder is proof of
reachability and ends it, so one dropped SYN never parks a node. Only three
consecutive failures produce `unreachable`. A measurement that cannot be taken
at all (preflight switched off, endpoint not resolvable) is `unknown`: it is
reported with its reason and does NOT refuse, because an unperformed probe must
never be able to park a healthy node. It asks one question — can this node reach
the API host — and never judges the work: a failure that happens DURING a run is
`internal/tools/task_transport_failure.go`'s business, and its successor is
minted by `internal/tools/task_reroute.go`.

# SEE ALSO

`gopi worker -h`, `gopi -h`, `man gopi`, `man gopi-task` (the ledger this command
runs rows from), `man gopi-merge` (which files conflicted branches as rows).
