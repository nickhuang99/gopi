<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# The `gopi worker --status` residue flood: what it is, who it helps, and what changed

Task: *"when `gopi worker --status` is run and the return is a list of NOT worker …
why is garbage info flooded to the user? what is the purpose and what does this info
mean? … the design should be evaluated."*
Branch: `fix/worker-status-residue-output`. Evidence: `/home/user/scratch/wsr-after.txt`
(after), and the operator's own capture (before) reproduced below.

## 1. The answer to "what is this info?"

Every token in the pasted output has one owner:

| Rendered text | Source |
| --- | --- |
| `gopi worker: no workers running` | `workerStatusNoWorkers` (`cmd/agent/cli_worker_status.go`) |
| `121 state file record(s) that are NOT workers (…)` | `workerStatusResidueFmt` |
| `pid 1008456 is gone — SIGKILL residue, no cleanup ran` | `residueReasonDead` |
| `… and 115 more (a worker clears them when it next starts)` | `workerStatusResidueMoreFmt`, sample bound `workerStatusResidueSample = 6` |

The files are `<gopi>/worker/worker-state-<pid>` — a **durable record a worker writes
about itself**, one per worker instance (pid, started, version, idle|busy, current task).
A worker that dies without cleanup (SIGKILL, a leaked test fixture) leaves its file
behind; a later pid number can even be *recycled* onto an unrelated process. Both are
"residue": a file that describes a process which is not a worker any more.

**What it is FOR (the real bug it answered).** `--status` used to report a file as a
worker from its *name + liveness alone*, so a recycled pid became a FABRICATED worker
(measured 2026-09-13: `worker-state-899174` rendered as a live worker while pid 899174
was an unrelated `agent.test`). The fix (commit `6c2ad527`,
`devdoc/worker-status-overreport-20260913.md`) added attestation: a state file is a
worker only if `/proc/<pid>/comm` says `gopi`. Everything the enumeration then *refuses*
had to go somewhere, and that somewhere is the residue line — the reason printed per
entry is the evidence of the refusal, so an operator can tell a SIGKILL corpse from a
recycled pid.

## 2. What it helps the user with — and where the DESIGN failed

Honest split:

* **Genuinely useful**: the exact count (is the pile growing?), the per-entry reason
  (which failure class), and the fact that they are not workers. This is the operator
  debugging the worker machinery.
* **The design failure**: the answer was rendered as a *data structure*, not as an
  answer to the question that was asked. `gopi worker --status` is a yes/no question
  ("is a worker running?"); the reply was **2 lines, 962 bytes, of which ONE 929-character
  line was 121 entries comma-joined after a colon**. Three concrete defects:

  1. **Form contradicts content.** A comma-joined list of pids under a colon reads like a
     worker *list* — the vocabulary of the line ("state file record(s) that are NOT
     workers") is correct and the shape still lied. The count was right and the *look*
     was wrong.
  2. **Jargon without education.** "SIGKILL residue", "attested", "recycled pid" are our
     internal vocabulary; nothing said what the files *are*, and the trailing clause
     named the *symptom* ("a worker clears them when it next starts") instead of the cure.
  3. **No next action.** The output said "you have 121 corpses" and stopped. The operator
     could not even tell *which profile* the answer came from (the dir was implicit), so
     the obvious cleanup could not be typed by hand. A dead end.

  The volume itself is not the whole story: the residue is real, bounded information —
  the defect is that it was printed at the wrong **altitude** for the question asked, on
  the same line as the answer, with no way to ask for more.

## 3. The design change (implemented here)

Default output now answers in the order the operator asks (measured after: 121 residue
files → **16 lines, 1373 bytes, longest row 138 runes**, exit 0, nothing written):

```
gopi worker: no workers running
  why: no detached `gopi worker --daemon` holds the lock, and no `gopi worker` process is in the process table
  lock: <gopi>/worker.lock
   (a FOREGROUND worker takes no lock — the process-table half of this scan is what finds it)
  start one: `gopi worker --daemon` (detached) — or `gopi worker` in the foreground to watch it run
gopi worker: 121 stale state file record(s) found — NONE of these is a worker:
  (what these are: state files of a worker pid that has EXITED — a SIGKILL, or a leaked test fixture, leaves one behind.)
  (a "recycled pid" row means the number now names an UNRELATED process. Deleting these files is safe; the next worker start clears them.)
  worker-state-900001: pid 900001 is gone — SIGKILL residue, no cleanup ran
  … (6 rows) …
  … and 115 more (not listed — rerun with --status-verbose)
  to clear them now: rm -f <gopi>/worker/worker-state-*   (121 file(s))
```

Decisions, each with the reason it is not the other option:

1. **The empty answer is educated, not bare.** "why / lock / start one" answers the
   question actually asked ("why no worker?") with two MEASURED facts (the process-table
   scan found nothing; `tools.LockProbe` names no holder). The lock note exists because
   "no lock holder" is *not* "no worker" — a foreground worker takes no lock.
2. **Residue is a labelled block, one row per entry**, basename first, reason second.
   The basename is the identity; the 40-character directory prefix repeated 6× was noise
   (it is stated once in the remedy command, and the full path is preserved in verbose).
3. **The remedy closes the dead end**: the command is built from the dir the enumeration
   *actually read* and the same `workerStateFilePrefix` the reader matches, so the advice
   cannot name a different set of files than the count counted.
4. **`--status-verbose` is the second altitude**, not a second feature: the same
   read-only read with every path listed. It is routed by `workerStatusRequested` and is
   classified READ-ONLY by the worker face's parse, so it can never be mistaken for an
   action by the read+act refusal.
4b. **The alias needed TWO fixes that the first cut got wrong** (both measured on the
   shipped binary 2026-09-14, both now pinned by tests). This is the trap of adding a
   read-only *modifier* to a gate built for *actions*:
   - **The printed remedy was refused.** The block tells the operator "rerun with
     `--status-verbose`", but only `--status` opened the control route
     (`workerControlRequested` → `workerControlParse`, which carries no action for the
     bare alias), so the word fell through to the loop scanner:
     `unexpected argument "--status-verbose" — refusing to start the worker loop`,
     exit 2. The report's own advice was the one command the binary refused, and the
     file list was unreachable. Fix: the route now opens on the read-only spellings too
     (`workerControlRequested` asks `workerStatusRequested` as its second half).
   - **The alias was invisible to the read+act refusal.** `controlActionRoleOf` returns
     `controlActionNone` for it (it is not a Merger-face control flag), so
     `gopi worker --status-verbose --stop` printed the status report and exited **0**
     with the `--stop` silently DROPPED — the F1 defect class this codebase already
     fixed once for `--status --stop` (a monitor concludes the service was stopped; it
     was not). Fix: the WORKER parse classifies the alias read-only beside
     `controlActionRoleOf`, where the face owns it.
   The lesson for the constant's doc comment, now recorded: a read-only spelling that
   is an *alias* of an existing read must be classified wherever the read is — routing
   and role classification are two different gates, and passing one does not pass the
   other.
5. **The bound is pinned by a test, not by taste**: no rendered row may exceed
   `workerStatusResidueMaxRowRunes` (200) — the old 929-rune row is the regression.
6. **A correctness fix found while measuring**: the enumeration emitted the *fallback
   (process-table) half after* the record half, so a live worker scored by both could be
   listed **twice**. Measured on a real host with a live daemon (pid 2527977). The
   process-table half now runs FIRST and the record half skips what it saw — dedupe true
   by construction. This was the same over-reporting class as the original defect, one
   layer down.
7. **DEAD CODE removed**: `workerControlStatusCommand` (`cli_worker_control.go`) had no
   caller and still rendered the OBSOLETE `"N stale state file(s): pid …"` shape — a
   second, unmaintained `--status` face that would have resurrected the old over-report
   if ever wired up. Deleted (with `workerLockPathReadOnly` kept: the live report uses
   it for the lock line). The stale shape survives only in `devdoc/`, as history.

## 4. Verification

* `go build ./...` — OK.
* `go test ./cmd/agent/ -run TestWorkerStatus` — PASS (`TestWorkerStatusResidueBlockIsBoundedAndReadable`
  now pins: exact count, meaning, remedy command, ≤6 rows, omitted remainder stated,
  every row ≤200 runes, verbose lists all 121, no residue → nothing rendered;
  `TestWorkerStatusCLIVerboseListsEveryResiduePath` drives the BARE alias the block
  prints — the spelling that used to exit 2 — against the shipped binary, including
  read-only; `TestWorkerStatusCLIRefusesTheVerboseAliasBesideAnAct` pins the read+act
  refusal for the alias at CLI level; `TestWorkerStatusPrintedRemedyIsExecutable` pins
  the routing/role contract for every spelling the report prints).
* `go test ./...` — see the task report's run (whole-tree gate).
* Before/after on the SAME fixture (121 dead-pid records, private `GOPI_HOME`,
  `GOPI_PROC_ROOT` pointed at an empty dir): before 2 lines / 962 bytes / longest row
  929; after 16 lines / 1373 bytes / longest row 138. Exit code 0 and **all 121 files
  still on disk** in both runs (`--status` is read-only by contract).

## 5. Not done (honest)

* The **self-healing** half is unchanged: `--status` deliberately never deletes
  (`status_action_conflict_e2e_test.go` pins it); the residue is cleared by
  `pruneWorkerStateResidue` on the next worker START, or by the printed command.
  `gopi worker --status` remains a *report*; making it sweep would break that contract.
* The residue is **still one file per worker start** by design; nothing here reduces how
  many accumulate (a worker that exits normally does remove its own file).
* `workerProgTopLevel` is a bare `"gopi worker"` spelling; it is pre-existing and used by
  every worker message, so it was not centralised into a constants package in this change
  (Rule 14 cleanup, out of scope here).
