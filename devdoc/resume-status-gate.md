<!-- doc-class: historical -->
# The resume gate's missing half: a row's STATUS, not only its holder

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Measured** 2026-10-16 on branch `task/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd`.

## The defect

`TaskRunChildResumable` (`internal/tools/task_run_retry.go`) is the gate both
`gopi worker --resume` (through the wrapper's abnormal-child retry arm) and any
future resume caller ask before a resume is admitted. It read **only** the
process-holder verdict:

```go
switch st.Verdict {
case TaskProcessStateRunning: ... // live holder
case TaskProcessStateForeign, TaskProcessStateUnknown: return false, st.Verdict
}
return true, st.Verdict   // ← every row with a free holder, whatever its status
```

Every **terminal** row has a free holder — the terminal transition clears the
identity/claim stamp — so a `failed` row fell through to `return true`. The
consequence: `gopi worker --resume <failed-id>` was admitted and replayed the
row's **judged** raw log as if it were an interrupted run.

The pre-existing posture test (`cmd/agent/cli_resume_failed_row_test.go`,
`TestResumeGateProceedsOnTerminalRow`) asserted exactly that behaviour and its
own header said it was the one to flip if the product decision changed. This
change is that flip.

## The fix — one decision, two surfaces

```go
// internal/tools/task_run_retry.go
func TaskRunResumeStatusGate(taskID, status string) (bool, string)
```

* **Admitted:** `in_progress`, `cancelled`, `pending`, `claimed` — every status
  whose work is still **unjudged**. `in_progress` is the crash shape; `cancelled`
  was stopped externally; `pending`/`claimed` carry no run at all, so nothing can
  be replayed. Admitting the last two is deliberate: a dispatch surface reading a
  pending row as "unresumable" would tell an operator to re-enter a row that was
  never entered.
* **Refused:** `failed` (a verdict), `completed`/`verified`/`superseded`
  (finished work), and any status the vocabulary does not know (the refusal then
  states the rule rather than guessing which side it is on).

The **holder arms are byte-for-byte unchanged** (running-self admits,
running-other refuses, foreign/unknown refuse); the status gate runs *after*
them, so it can only add a refusal, never remove one.

The same function is asked by `taskProcessStatus` (the `gopi task status` view)
through a new `StatusRefusal` field, so the verb an operator reads and the
mechanism that takes the decision cannot disagree.

## The remedies are measured, not assumed

The briefing's suggested wording — `gopi task update <id> --status pending` —
**is refused by the ledger**:

```
$ gopi task update 01a15ca0-…-beef --status pending --reason "…"
gopi task update error: task: illegal status transition failed → pending
                        (allowed from failed: verified|completed)
```

So the printed remedy is the verb that exists for this:
`gopi task reopen <id> --reason "…"` (re-enqueues the work as a fresh pending
`-r1` row, keeping the judged row's verdict — the ledger is append-only), or a
fresh `gopi task submit`.

## A bug this change introduced, and caught before commit

The status axis first dropped `st.Resumable = true` from the early-return arm
(rows with no holder to judge). A pending row then rendered `resumable: no`
with no refusal. Caught by running the real surface, not by the tests:

```
$ gopi task status 01a15ca0-…-beef-r1   # pending
resumable:   no            ← wrong, and no status_refusal to explain it
```

Fixed by restoring the holder axis's own answer before the status axis runs.
The invariant is now stated and pinned: **each axis may only make the answer
stricter**. `TestTaskProcessStatusUnjudgedRowsKeepResumable` guards it.

## `gopi task status` exit codes

`5` = the row's status forbids a resume (holder and ground both free). `4` was
unavailable — it is `tools.TaskForBranchExitNoOwner`, already meaning "the
ledger was read and no row names this selector"; collapsing the two would make a
selector miss read as a resume refusal.

## Verification (what was actually run)

* Red proof: neutralizing the gate (`!admitted && false`) makes
  `TestTaskRunChildResumableRefusesFailedRow` fail with
  *"a failed row with no live holder was ADMITTED"*; restoring the file makes it
  green again (`git hash-object` identical).
* `go test ./internal/tools/ -run 'TaskRunChildResumable|TaskRunGuard|TaskProcessStatus|GroundInUse|TaskStatus|Resume|Identity'` → ok
* `go test ./cmd/agent/ -run 'ChildRetry|RetryCounter|RetryRefuses|JudgedChildFailure|Resume|Status|Singleton|TaskVerb'` → ok
* `go test ./internal/standards/ -run 'Gate1..Gate6|NamedLimits|WorktreeRoot|Scratch'` → ok
* Real surface, in a sandbox profile (`gopi tool sandbox create`), on a row driven
  `pending → in_progress → failed` through the CLI: `gopi task status` prints
  `resumable: no`, names the status, prints both ways forward, exit **5**; the
  re-opened `-r1` row prints `resumable: yes`, exit **0**.
