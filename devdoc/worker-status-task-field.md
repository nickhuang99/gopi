<!-- doc-class: normative -->
# The task field on every `gopi worker --status` line

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **What this page answers**: which task is a given worker pid running, how to
> ask that question as a *value* rather than by reading prose, and why the
> derivation has exactly the three steps it has.

## The measured defect (2026-10-23)

`gopi worker --status` rendered **three** worker shapes and only **one** of them
named a task:

| shape | rendered line (before) | task visible? |
|---|---|---|
| state-record (detached daemon) | `pid N: idle` / `pid N: busy — <task> (session …)` | only *while a round was in flight* — the writer clears `TaskID` at every round end |
| argv-fallback (detached, no record) | `pid N: idle — <raw cmdline> (no state file …)` | only incidentally, inside the command string |
| **foreground** (the common shape here) | `pid N: FOREGROUND (holds no daemon lock) — <raw cmdline>; …` | only incidentally, inside the command string |

Measured on hosta with three live foreground workers (pids 1697081,
1700250, 1701687, all bare `gopi worker …`): an operator could read the row only
by re-parsing `ps` output, and **could not query it at all**.

## The fix, and its one derivation

`cmd/agent/worker_status_task.go` owns a `workerTask` value — the id plus the
honest **source** — and one derivation order, `workerStatusWorker.Task()`:

1. **the state record**, but only while a round is in flight. The writer clears
   `TaskID` in `SetRoundEnd`, so a daemon between rounds must say "not recorded"
   rather than repeat the last row it ran.
2. **the worker's own argv** (`/proc/<pid>/cmdline`), parsed through the verb's
   **own grammar scanner** (`scanWorkerArgs`). This is the step that makes the
   foreground shape work, and the reason a substring search was refused: argv[0]
   is the binary, argv[1] is the verb, and every following word is a flag or a
   flag's operand — the operand of `--repo` is a repository path, of `--db` a
   database file. Only the grammar knows which word is the row.
3. **the not-recorded marker** (`(not recorded)`), rendered and never omitted
   (Rule 15: an empty field is indistinguishable from a renderer that forgot it).

An argv the grammar **refuses** is not guessed at. The process is real; only its
attribution is unknown, so the answer is the marker — not a keyword harvested
from a sentence the scanner did not understand.

## The queryable half

Every worker line carries the stable token `task=<id>`, **second on the line** on
every shape, so one grep answers the question:

```bash
gopi worker --status | grep -o 'pid [0-9]*: [A-Z]* task=[^ ]*'
```

and the machine form answers it as a value:

```bash
gopi worker --status --json | jq -r '.workers[] | "\(.pid)\t\(.task)\t\(.task_recorded)"'
```

The JSON document is built from the **same** enumeration and the **same**
derivation (`workerStatusJSON` is a pure function of the scan), so the two forms
cannot describe the same process differently. `task_recorded` is the boolean a
consumer switches on — comparing against a prose marker would be a coupling to
text — and `task_source` (`state` / `argv` / `session-dir` / `none`) says how
much to trust the value.

## The third source: the round's own working directory (2026-10-28)

The first shape of this field had two sources — the state record and argv — and
that left the COMMON shape on this host saying `task=(not recorded)` for a
worker that was demonstrably running a row:

```
$ gopi worker --status
gopi worker pid 3456940: FOREGROUND task=(not recorded) (holds no daemon lock) — gopi worker; …
gopi worker pid 3687131: FOREGROUND task=01a17ff1-… (holds no daemon lock) — ./bin/gopi worker 01a17ff1-…; …
```

`readlink /proc/3456940/cwd` answered
`/home/user/.gopi/sessions/01a17df9-524a-7362-9ebd-efc24de76275` — the row's own
session container. A FOREGROUND worker writes no state record (it must not: the
file is a daemon's singleton marker), and a bare `gopi worker` carries no row in
argv, so neither source could answer; but `taskWorkdirEnter` moves the round into
`<sessions root>/<task-id>` at its claim boundary and `taskWorkdirRestore` moves
it back at the end (`task_session_cwd.go`), which is exactly the fact an
operator wants.

`workerTaskFromRoundSessionDir` reads it, LAST in the order — argv outranks it
(a NAMED row is what the operator started, authoritative even before the first
claim), and the state record outranks both while a round is in flight. Five
refusals keep it from inventing a row (Rule 15): the cwd link must be readable,
its PARENT must be the sessions root resolved by `config.SessionsRoot` (the one
resolver the writer used), the container must be a DIRECT child, its name must
pass `internal/sessionid`'s shape gate (a dashless uuid is refused), and the
directory must still exist.

`/proc/<pid>/cwd` is the channel, NOT `ps -o cwd`: under this kernel
(procps-ng 3.3.17) `ps -o cwd= -p <pid>` prints `-` even for a process this user
owns (measured 2026-10-28 against pids 3687131 and $$ alike), so the earlier
"userland prober" substitution for the fallback-command-line renderer answers
nothing on this host. The status reader's own answer is a readlink.

## Two routing facts a later change must not undo

1. **`--json` is a modifier of the read, never a read on its own.**
   `workerStatusRequested` opens the status route only when a status spelling is
   *also* present, so a bare `gopi worker --json` still falls through to the loop
   scanner and is refused as an unknown word. Routing it alone would turn a
   misspelled worker launch into a successful status report — one question
   silently substituted for another.
2. **It is classified read-only** (`workerControlRoleOf`), so
   `--status --json --stop` is still refused as READ+ACT. An unclassified
   read-only alias makes that refusal blind, and the act is silently dropped with
   a success exit code (the F1 defect, `devdoc/worker-daemon.md`).

## The status face is still read-only

`workerStatusReport` resolves the state directory through
`tools.ControlDirReadOnly`, enumerates, and — for the JSON form — encodes a value
in memory. Nothing is created, removed or locked, which
`cmd/agent/status_action_conflict_e2e_test.go` pins for a bare `--status`.

## Tests that hold this down

`cmd/agent/worker_status_task_test.go` pins:

- the state-record shape (the field is the record's row, source `state`, and the
  id appears **once** on the line);
- a daemon **between rounds** (`task_recorded=false`, the declared marker);
- the argv-fallback shape (source `argv`);
- **flag operands are not tasks** (`--repo`, `--db`, `--report-to` → not recorded);
- a bare loop worker → not recorded;
- an argv the grammar refuses → not recorded, never a guess;
- the FOREGROUND shape and its empty case;
- an unreadable `/proc` entry → `source=none`, the pid still listed;
- the JSON form's task fields, its round trip, and that its keys are always
  present (Rule 15a);
- `--json`'s routing and role classification, including that a bare `--json`
  does **not** open the status route.
