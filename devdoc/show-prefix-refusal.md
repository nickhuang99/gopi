<!-- doc-class: normative -->
# The ledger show seam refuses a truncated task id BY SHAPE

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a132ed-8752-7013-9902-1c427d289e19` (epic `full-id-no-prefix`), commit
`ec2fa309b`. Filed as a Rule 41 anomaly: *the id `gopi task list` prints is not
an id `gopi task show` accepts.*

## The reported premise was half false — and measuring it was the first work

The anomaly's prose said the listing renders "12 hex chars" under a "SHORT id"
heading. Measured on this host, in the sandbox profile the artifact itself names
(`sandbox-fixture-gate-c1cf907a`):

```
$ GOPI_HOME=$SB $SB/gopi task list --all
#01a132d6-5ac1-7b41-aa26-ee1a0c9a178f  [pending]  SEEDED FIXTURE — …
$ GOPI_HOME=$SB $SB/gopi task list --status pending
#01a132d6-5ac1-7b41-aa26-ee1a0c9a178f  [pending]  …  [queue-skip]
```

Both views render the FULL 36-char id. `taskHashDisplay`
(`internal/tools/tasks.go`) returns the id verbatim, which is what epic
`full-id-no-prefix` landed in `c7de4ec8` — and the artifact's own verbatim trace
agrees with the code while its parenthetical disagrees with its own trace. So
**the display half was already fixed and was left alone**; a "fix" there would
have been a regression against a working rule.

This is the shape worth remembering: an anomaly report can carry a true trace
and a false summary of it. Re-run the trace before acting on the summary.

## The real residue

When the operand IS a truncated canonical uuid — the first dash group, the
intuitive thing to copy — `show` printed the generic sentence:

```
$ gopi task show 01a132d6-5ac1
gopi task show error: task: no task with task_id="01a132d6-5ac1"
```

Literally true. And the SAME sentence it prints for a typo, for a row that lives
on a peer's ledger, and for a near-miss spelling — so the operator cannot tell
"you gave me a fragment" from "this ledger holds no such row".

## The rule already existed next door

`internal/fullid` is the project's ONE source of truth for "an id is always used
in FULL; a prefix is never resolved, it is refused, and the refusal names every
full id it could have meant". `fullid.Refusal` / `fullid.CanBePrefix` /
`fullid.KindTask` were already reached by:

- the session-resume seam (`taskResumedSessionArtifact`), and
- the row send/move leg (`syncRowResolveTaskID` — the sibling of this fix, and
  memory record #7650 describes that one),

but **not** by the ledger show seam. This change is the third door.

## The change

```go
// internal/tools/tasks.go — the seam
if t == nil {
    return "", fmt.Errorf("task: no task with task_id=%q%s%s",
        taskID, taskIDNearMissSuffix(ctx, db, taskID), taskIDPrefixRefusal(ctx, db, taskID))
}
```

`taskIDPrefixRefusal` is a **MESSAGE, never a resolution path**: nothing looks
the prefix up and returns a row.

- Gated on `fullid.CanBePrefix` — the ONE shape predicate. No second uuid
  comparison is spelled (Rule 13/14).
- Phrased entirely by `fullid.Refusal(fullid.KindTask, …)`. No new sentence.
- The candidate scan is `taskIDPrefixCandidates`, the scan
  `syncRowIDPrefixCandidates` already was, promoted to its shared name (its one
  caller, `syncRowResolveTaskID`, was updated). Both ledger seams now name the
  SAME candidate set for the same operand — one scan, two faces (Rule 14).
- `taskIDPrefixCandidateCap` is a named constant (Rule 1).

### Order at the seam: exactly one clause per operand

The near-miss clause (`taskIDNearMissSuffix`, the dash-moved spelling) keeps
firing and is NOT displaced, and a truncated prefix gets **no invented spelling
advice**. The two clauses are provably disjoint: a dash-moved operand is
full-length, so it fails `fullid.CanBePrefix` (`fullid.TruncatedUUID` requires
`ShortPrefixLen ≤ len < UUIDLen`). One operand, one refusal.

A `t-<epoch>` legacy id, a name, and any ordinary typo also keep the seam's
ordinary answer, because they are not the uuid shape cut short. That is the
intended bound, not an oversight: the two verdicts are about different things —
the OPERAND is not an id, versus the ROW is not in this ledger.

## The defect in the first cut that the red proof caught

`fullid.Refusal`'s multi-candidate arm renders "N identities share that prefix"
from **the length of the list it is GIVEN**. The first version sliced the
candidates to the cap and handed that slice to `Refusal` — so with 20 rows
sharing a prefix the message said "12 identities share that prefix", a number
that **understates the set** and reads as the whole truth (Rule 15).

Fixed by passing the capped slice and carrying the real total in its own clause:
`— 12 of 20 identities sharing that prefix are named above, in claim order`.

Unit review had not caught this; the red proof surfaced the design question
because writing the test forced me to state what the message must say.

## Test/data traps, both measured

- **A test that pins UNCHANGED behaviour passes on the pre-fix tree — that is
  correct.** The near-miss test and the ordinary-answer test are green in the
  red proof, and must be; only the three new-behaviour tests fail there.
- **A red-proof test file may not reference a constant the fix introduces.** The
  first test spelled the cap as `taskIDPrefixCandidateCap`, so the pre-fix tree
  failed to BUILD (`undefined: taskIDPrefixCandidateCap`) and proved nothing.
  The bound is now a literal in the test, with the reason in a comment.

## Red proof recipe (no stash, no worktree of the branch touched)

```bash
R=$(mktemp -d -p /home/user/workspace gopi-redproof-XXXXXX)   # off the / partition
git archive HEAD | tar -x -C "$R"            # at the commit BEFORE the fix
cp internal/tools/task_show_prefix_refusal_test.go "$R/internal/tools/"
cd "$R" && go test ./internal/tools/ -run 'TestTaskShow…' -count=1
```

The failure text quotes the anomaly's own sentence
(`= task: no task with task_id="01a132d6-5ac1", want a refusal whose text names
the operand as not a full id`), which is what makes it evidence. The branch's
worktree is never written by this, so there is nothing to restore.

## Not done (honest)

The other generic `no task with task_id` seams were not swept — `show` was the
observed surface: `internal/tools/taskproc.go:449,596`, `taskrunguard.go:240,402`,
`tasks.go:221` (`task status`), and `TaskSessionID`. Each is a one-line reach for
the same helper.

## Closed by the sibling epic (task `01a1336d`, 2026-10-13)

Those six seams now reach the same helper — the list above is the TODO this epic
executed, so read it as a record, not as an open gap:

| seam | file:line | trailing clause | CLI door |
|---|---|---|---|
| `TaskProcessStatusFor` | `internal/tools/taskproc.go:449` | `%s` | `gopi task status <id>` |
| `TaskProcessLiveness` | `internal/tools/taskproc.go:596` | `%s` | *(no CLI verb — Go caller)* |
| `TaskRunGuardFor` | `internal/tools/taskrunguard.go:240` | `%s` | `gopi worker <id>` |
| `taskRunGuardResolvable` | `internal/tools/taskrunguard.go:403` | `%s` before `— refusing to run a ghost id` | `gopi worker <id> --takeover` |
| `TaskStatusOf` | `internal/tools/tasks.go:222` | `%s` | `gopi worker --resume <id>` |
| `TaskSessionID` | `internal/tools/tasks.go:3498` | `%s` | `gopi worker <id>` (session publication) |

Two details worth carrying forward, both measured:

1. **The clause is a suffix, not a sentence.** Each seam keeps its own prefix
   (`task: run guard: no task with task_id=…`) and appends
   `taskIDPrefixRefusal(ctx, db, operand)`, which returns `""` for a non-prefix
   operand — so the ordinary answer is byte-identical, and the seam-specific
   tail (the ghost-id sentence) still reads as the seam's own words.
2. **`taskRunGuardResolvable` needed one line to be reachable at all**: it passed
   `context.Background()` inline to `taskByID`, so the helper had no ctx to take;
   hoisting it to a local is the whole change there.

The remaining `no task with task_id=%q` sites (`tasks.go` 4477, 4658, 5420, 5604,
7491, 7521, 7925, 7950, 8251; `task_run_retry.go:93`; `task_repo_repair.go:162`)
are still NOT swept, and for a structural reason rather than an oversight: they sit
inside `taskByIDTx` transactions or in paths handed a `QueryExecer`/`*sql.Tx`, and
the helper's signature is `(ctx, *sql.DB, string)`. Reaching them means widening
the helper to an interface or opening a second connection mid-transaction (the
ledger's single-connection openers deadlock) — a design decision, not a mechanical
append.
