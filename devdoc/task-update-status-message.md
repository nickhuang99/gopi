<!-- doc-class: historical -->
# Task Report: `gopi task update` status diagnostics (fix/task-update-error-message)

## The report, and what actually happened

Attachment `task_update_error.txt` (verbatim, on master `dc75ea64`):

```
$ gopi task update 01a0b588-… --status cancelled
[task-notify] event "cancelled" for 01a0b588-… not in the events allow-list — not delivered
Task updated: 01a0b588-… (status=cancelled, reason=task cancelled)
$ gopi task update 01a0b588-… --status canceled
gopi task update error: task: invalid status "canceled" (want one of pending|claimed|in_progress|completed|failed|cancelled|verified|reopened)
$ gopi task update 01a0b588-… --status cancelled
Task updated: 01a0b588-… (status=cancelled)
```

Read as "the same command failed, then succeeded" → a claimed bug. **It is not
a bug in the write path**: `canceled` is not a status, so run 2 correctly
refused; runs 1 and 3 both succeeded. Reproduced on a scratch `GOPI_HOME`
(allow-list shaped like the operator's, **the real host settings.json was never
touched**) and the same three exit codes were observed (0, 1, 0), plus the
second cancel writing an empty reason — a same-status no-op does not re-stamp
it, which is the third line's missing `reason=`.

What IS wrong is the TEXT. Three operator-facing surfaces made a correct
refusal read as an unstable command, and one of them actively misdirects:

1. **The remedy list offered `reopened` — a status no write can reach.**
   `taskAllowedTransitions` has no `reopened` key (M4.20-B retired it), so
   following the advice produced a SECOND refusal:
   `illegal status transition cancelled → reopened (allowed from cancelled: )`.
   A remedy that cannot work is worse than no remedy.
2. **An empty allow-list rendered as a blank**: `(allowed from cancelled: )`.
   A terminal status is terminal — the message gave the operator a hole where
   the answer should be.
3. **The notify drop line sat on stderr next to the success without saying what
   it was.** `[task-notify] … not in the events allow-list — not delivered` is
   a delivery *filter* (Rule 15's visibility of a configured-but-unmatched
   subscriber), not a failure — printed directly above `Task updated: …`, it
   reads as the failure half of the reported sequence.

## Completed

- `invalidStatusRefusal(unknown, acceptable)` — ONE invalid-status message for
  both the write (`taskStatusWritable`) and the read-only filter
  (`taskStatusListForCLI`): names a **one-edit near miss** (`did you mean
  "cancelled"?`) and explains the retirement when the token IS a retired status.
- `taskStatusWritable` / `taskStatusListForCLI` — the remedy list is now only
  what `--status` can actually WRITE; the legacy token stays accepted on the
  read-only filter so legacy rows remain queryable.
- `statusBytesNear` — a tiny, dependency-free one-edit distance (insert /
  delete / substitute / transpose) driving the near-miss suggestion.
- `taskTransitionAdvice(from, to)` — a terminal source says `TERMINAL` and names
  the repair path (REISSUE); a non-terminal source keeps the allowed-move list.
  The `reopened` hint now lives in ONE const (`retiredReopenedStatusHint`) used
  by both refusals.
- The notify drop diagnostics (`tasknotify.go`, `taskheartbeat.go`) now say it
  is a **delivery FILTER, not a failure**, that the write succeeded, quote the
  effective allow-list, and name the knob (`GOPI_TASK_NOTIFY_EVENTS` / the
  `notify` section in settings.json) and the `all` value that widens it.
- `memsync export-tasks --status` — the same remedy-list fix (it named the
  retired token too).
- New regression test file `internal/tools/taskupdate_status_message_test.go`
  (6 tests), each tied to one surface, plus three red proofs (below).

## Not Done (honest)

- **No behaviour of the state machine changed** — deliberately. `canceled` was
  and is a refusal; the fix is diagnosis only.
- **`gopi task list --help` still mentions the retired `reopened`** for legacy
  rows — that text is CORRECT (the filter does accept it) and was left alone;
  only the write's remedy list was narrowed.
- The `--result`/`--reason` duplicate-`nothing to update` strings (three copies
  in `tasks.go`) were not consolidated — outside this report's scope.

## Verification

- Build: `go build ./...` → OK (exit 0); `go vet ./internal/tools/` → OK.
- Tests: `go test -count=1 -timeout 300s -run '<the six new tests>'
  ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 18.111s`
  (exit 0); `go test -c -o /dev/null ./internal/tools/` → `TEST_PKG_COMPILE_OK`.
- **Branch scope, stated honestly**: a BROAD selection
  (`-run 'Task|Status|Notify|Transition'`) is unusable on this host in this
  window — it timed out at 600s with a parked
  `insertTaskRow → ExecContext` goroutine (`tasks.go:1436`, reached from
  `TestTaskShowSessionLogPath`). This diff has no hunk near `insertTaskRow`
  (`git diff dc75ea64 -- internal/tools/tasks.go`), the six target tests pass
  in 18s on the same tree, and the cause was measured on the host: load average
  **15.62** with **eight concurrent `tools.test` binaries** from other agents'
  worktrees, against a scratch root `devdoc/firstopen-wallclock-budget-test.md`
  measures at 0.6–4.0s per fresh ledger open (p50 1.29s). No load was
  manufactured (Rule 27). A green whole-package run is NOT claimed here — it
  belongs to the off-peak heavy path. Reproduce the six:
  `go test -count=1 -timeout 300s -run
  'TestInvalidStatusRefusalOffersOnlyWritableStatuses|TestTaskListFilterKeepsAcceptingLegacyStatus|TestIllegalTransitionFromTerminalNamesTerminality|TestIllegalTransitionStillListsAllowedMoves|TestNotifyAllowListDropReadsAsAFilterNotAFailure|TestStatusBytesNear'
  ./internal/tools/`
- Commit: HEAD of branch `fix/task-update-error-message` (worktree
  `~/workspace/gopi-taskupdate-err`, base `dc75ea64`).

## Evidence (self-filed)

**Reproduction of the reported sequence** (scratch `GOPI_HOME`, host
`settings.json` untouched):

```
cancelled            → exit 0   "Task updated: … (status=cancelled, reason=task cancelled)"
canceled             → exit 1   'task: invalid status "canceled" (want one of pending|…|reopened)'
cancelled (2nd)      → exit 0   "Task updated: … (status=cancelled)"   ← no reason= : same-status no-op
```

**Red-then-green, one revert per surface** (each revert applied to the fixed
tree, test run, then restored — all three FAILED on the reverted code):

1. remedy list re-admits `reopened` →
   `FAIL: taskStatusWritable contains the retired "reopened"` /
   `want taskStatusSet minus the retired token`.
2. terminal advice reverted to the old render →
   `FAIL: refusal must say the status is terminal: "…cancelled → completed (allowed from cancelled: )"`
   (the empty-allow-list render reproduced byte-for-byte).
3. drop line reverted to the bare form →
   `FAIL: the drop line must separate a delivery filter from a failure: "[task-notify] event \"cancelled\" for t-statusmsg not in the events allow-list — not delivered"`.

**New messages actually rendered** (from the green run):

```
task: invalid status "canceled" (want one of pending|claimed|in_progress|completed|failed|cancelled|verified)
  — did you mean "cancelled"?
task: illegal status transition cancelled → completed — cancelled is TERMINAL: no status write
  leaves it (a rejected/unfinished row is repaired by REISSUE: submit a NEW task and leave this row as history)
[task-notify] event "cancelled" for <id> not in the events allow-list — not delivered
  (this is a delivery FILTER, not a failure; the write succeeded — GOPI_TASK_NOTIFY_EVENTS
  (env) / the "notify" section in settings.json="claimed,in_progress,completed,failed";
  add "cancelled" to it, or set it to "all" for every event)
```

`TaskList(taskStatusReopened)` still returns without error (legacy filter kept),
and the real host `~/.gopi/settings.json` was never written: every run used a
`t.TempDir()` ledger plus `GOPI_SETTINGS` pointed at a non-existent file.
