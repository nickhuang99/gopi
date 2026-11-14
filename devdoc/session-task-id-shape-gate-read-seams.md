<!-- doc-class: normative -->
# Session task-id shape gate — the two READ/attribution seams (bug row 01a12f3c)

**Class:** normative — a standing claim about master: the task-id SHAPE rule is
applied at every seam that joins an id into a path, on both the write side and
the read side, from ONE definition (`internal/sessionid`).

## What this change is, and what it is NOT

Bug row `01a12f3c-d888-720e-b7b5-f458f55e98aa` (2026-10-12) found that a task id
has three spellings in this tree and only one of them is the row id:

| spelling | example | where it comes from |
|---|---|---|
| the ROW id | `01a12dc6-f44d-7347-9b95-2382a4a7d352` | the ledger's key, the session directory's name |
| the branch/worktree spelling | `01a12dc6f44d73479b952382a4a7d352` | `taskGroundIDStem` drops every non-alphanumeric character |
| a rendered prefix | `01a12dc6` | copied out of a listing (`internal/fullid` exists because 2409 rows share prefixes) |

The ledger always refused the wrong ones; the FILESYSTEM did not — the seams
normalized with nothing but `strings.TrimSpace` and `filepath.Join`'d the
operand, so the wrong spelling was not an error, it was a NEW directory
(measured on this host: 11 dash-stripped containers beside the canonical ones;
the case that surfaced it had a caller reading its own evidence out of the
directory gopi never wrote).

**The sibling branch `task/01a12f3c…` fixed the two WRITE seams**
(`internal/agent.sessionDirForID`, `internal/tools.taskSessionDir`) and shipped
the shared rule as `internal/sessionid`. **This branch closes the two REMAINING
read/attribution seams**, which the epic named explicitly:

1. `internal/signalrec/signalrec.go`, `sessionDest()` step 2 — the carried
   ledger id becomes a path element before the record's directory is decided;
2. `internal/tools/tasks.go`, `taskFindSessionJSONLByTask` — the id is joined
   under the sessions root to find a row's run record.

Neither is a writer. **The gate therefore never mints anything**: it refuses,
reports, and returns the caller's own empty answer.

## The rule, and why the two branches carry the same file

`internal/sessionid` is a leaf (stdlib + `internal/fullid`, so the uuid
predicate is not spelled twice) and its verdict is deliberately NARROW: it
refuses exactly what it can PROVE is a mangled uuid — a dashless 32-hex run,
i.e. a uuid with its grouping dropped — and admits everything else (a canonical
uuid, a `t-…` auxiliary/legacy id, and any other id a caller carries). A first
cut on the sibling branch that admitted only "canonical or `t-…`" broke nine
real tests, because this tree genuinely uses task-id-shaped tokens that are not
uuids ("01a11548-tracked", "0000-emptydir"); over-blocking turns a visible
failure into a silent one (Rule 15), which is the worse defect.

**`internal/sessionid/` is byte-identical to the sibling's** (`git show
b61b2b47d:internal/sessionid/sessionid.go` → `git diff` empty), and that is the
point: when the two land, the tree must have ONE copy — not a second spelling of
the rule, and not a second sentence for its refusal (Rules 13/14). Landing the
sibling first or this one first is therefore immaterial; `git merge` of the
second is a no-op on that directory. **The files were NOT hand-transcribed**:
they were taken from the sibling with `git checkout b61b2b47d -- internal/sessionid/`,
which is also why there is nothing to reconcile.

## Seam 1 — `signalrec.sessionDest()` step 2

`sessionid.Refuse(id)`'s first caller-side prefix in this package is
`"signalrec: %w (ledger id %q, mode %s) — refusing to file the record under a
directory this tree did not mint; no public-dir fallback"`: the shared sentence,
this package's prefix, and the two facts an operator needs (which id, which
mode).

**Why a REFUSAL and not the resident fall-through below it.** That fall-through
exists for an id whose container is MISSING (a real row in a profile where no
dir can exist); it files the record under the process key, with a note. An id
this tree does not spell is a different defect — it names no row's container at
all — and filing it under `process-<pid>` would attribute the record to a
directory the run it explains never wrote. The refusal is about the RECORD only
and never blocks the caller's own work.

## Seam 2 — `taskFindSessionJSONLByTask`

The same gate, placed after `taskIDNormalize` and before the join, so the
whitespace seam stays the one seam. Its answer stays `nil` (an empty list) and
the caller's contract is untouched.

**It REPORTS rather than returning quietly**, and the reason is the caller: this
resolver feeds `taskGroundReconcileSessionMinted` (via `task_ground_reconcile.go`
→ `gopi task for-branch` / `for-worktree`), whose "no paths" outcome is
DISPLAYED as a verdict about where the row ran. An inadmissible id would
otherwise read there as evidence that the row ran nowhere — a worse failure than
the one being fixed. So the report goes to stderr with the shared sentence, and
**nothing is minted**.

## Red → green, measured on both seams

The regression tests exist on both seams and were RUN against the gate removed
(`git show HEAD:<file> > <file>`, i.e. the pre-fix production code, the new
tests kept):

- `internal/signalrec/session_dir_shape_test.go` —
  `TestSignalRecRefusesTheBranchSpellingOfTheLedgerID` FAILED pre-fix:
  `Intent accepted the branch/worktree spelling "01a12f3cd888720eb7b5f458f55e98aa" as a ledger id — the shape gate is not on sessionDest's step 2`;
  PASSES post-fix.
- `internal/tools/task_find_session_by_task_test.go` —
  `TestTaskFindSessionJSONLByTaskRefusesTheBranchSpelling` FAILED pre-fix with
  the silent join visible in the failure itself:
  `returned [.../01a12f3cd888720eb7b5f458f55e98aa/01a12f3c-d888-720e-b7b5-f458f55e98aa.jsonl] for the branch/worktree spelling …`;
  PASSES post-fix.

Each seam also carries the OTHER half, so the gate cannot quietly widen:

- `TestSignalRecAcceptanceIsUnchangedForAdmissibleIDs` — a canonical id and a
  `t-…` id with a usable container are still filed as `LayoutTaskRun`, and the
  sessions root still holds exactly one directory;
- `TestSignalRecUnknownAdmissibleIDKeepsTheMissingDirRefusal` — an admissible id
  with no container keeps the PRE-EXISTING missing-directory refusal, and that
  refusal must NOT contain "task id shape";
- `TestTaskFindSessionJSONLByTaskAcceptsAdmissibleIDs` /
  `…UnknownIDStaysQuiet` — the same two halves on the read seam, including "a
  read seam must not materialise anything" (`ls` of the root is unchanged).

## Real-surface evidence (Rule 31)

Both seams were exercised for real, with real arguments and real state. The
signalrec one is the stronger of the two because it needs no test harness — a
real binary, a real env, a real signal, and the filesystem as the measurement
(the only place this defect was ever observable).

**Seam 2, real face (`gopi task for-branch`, the displayed outcome):**

```
$ ./bin/gopi task for-branch task/01a12f4b34bb7c28bd9a61d569db30f7
1 owner(s) of branch task/01a12f4b34bb7c28bd9a61d569db30f7
  task 01a12f4b-34bb-7c28-bd9a-61d569db30f7  status=in_progress  (in flight, claimed by host-b:3904777)
    session  18500e4e-cb3c-401a-bcad-5b0e8a3731fa
    worktree /home/user/workspace/gopi-01a12f4b34bb7c28bd9a61d569db30f7
```

The ledger renderer resolves a dashless branch name (`task/01a12f3cd888720eb7b5f458f55e98aa`)
back to its canonical row, which is why the admissible path must keep working.

**Seam 1, real surface, scenario A — the carried id whose container does NOT
exist** (a real one-shot, `GOPI_TASK_RUN_ID` = the branch/worktree spelling, the
handoff env unset so step 1 cannot answer; SIGUSR1, so the record's resolution
happens while the process is alive). The victim's own stderr, verbatim:

- **pre-fix binary** — a refusal about the wrong thing (a MISSING DIRECTORY),
  which is what made the defect invisible:
  `signal provenance NOT FILED (no public-dir fallback): signalrec: session dir "…/sessions/01a12f3cd888720eb7b5f458f55e98aa" for task 01a12f3cd888720eb7b5f458f55e98aa is missing or unusable — refusing to write …`
- **fixed binary**:
  `signal provenance NOT FILED (no public-dir fallback): signalrec: task id shape: "01a12f3cd888720eb7b5f458f55e98aa" is a dashless 32-hex run — a uuid with its grouping dropped; it is the branch/worktree spelling (taskGroundIDStem strips every non-alphanumeric character), and joining it mints a SECOND session directory beside the row's own — did you mean "01a12f3c-d888-720e-b7b5-f458f55e98aa", the ROW id? (ledger id "01a12f3cd888720eb7b5f458f55e98aa", mode memory) — refusing to file the record under a directory this tree did not mint; no public-dir fallback`

**Seam 1, scenario B — the PRE-MINTED mangled container** (the litter the
ungated writers made; `metadata.json` written, so `isSessionDir` accepts it and
only the SHAPE rule can stop the record). This is the defect's own shape, and
the measurement is the filesystem:

| binary | outcome | measurement |
|---|---|---|
| pre-fix | record filed INSIDE the mangled container | `1 line(s) in …/sessions/01a12f3cd888720eb7b5f458f55e98aa/signal-provenance.jsonl` |
| fixed | refused by `internal/sessionid` | `NOT FILED (the container is still only [metadata.json ])` |

Both scenarios ran in a real sandbox profile (`gopi tool sandbox create
--label signalrec-probe`, Rule 34) whose `sessions/` started EMPTY, and every
signal went to a process this run started — its cmdline verified against the
binary under test (`/proc/<pid>/cmdline`) before signalling, so no unrelated
process was ever touched.

## Not Done (honest)

- **The gate is NARROW by design, and this branch keeps it narrow.** A rendered
  uuid PREFIX (`01a12dc6`) is still admitted — the tree uses such tokens as ids
  (`internal/scratch` names a gocache residue generation `01a0a392`), and the
  boundary test above pins that an unknown id keeps its pre-existing behaviour.
  So "an id I typed wrong in a way that is a valid uuid prefix" is still not
  caught here; it was not caught before either, and widening it is a separate
  decision with its own evidence.
- **`taskGroundReconcileSessionMinted` (the caller) reports nothing of its own**
  when the id is refused: the report is the one this change prints, and the
  caller's "no paths" path is unchanged. Making the DISPLAYED verdict carry the
  reason (rather than stderr) would change that verb's answers and is out of
  scope here.
- A one-shot that carries the mangled id and is killed by SIGTERM **can** lose
  the race against its own exit (measured) — the record's resolution runs on the
  recorder's goroutine. That is pre-existing behaviour of the recorder, not
  something this change introduced, and it is why the probe above uses SIGUSR1.
