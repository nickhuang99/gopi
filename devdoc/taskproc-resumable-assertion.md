<!-- doc-class: historical -->
# Task Report: master-red `internal/tools` — inverted resumable assertion

## Summary

`go test ./internal/tools/` was red on master with a self-contradicting failure:

```
--- FAIL: TestTaskStatusRunningForOwnRow (0.03s)
    taskproc_test.go:120: a row with a verified-live holder must NOT be resumable (that is the double-run guard)
```

The **production predicate was right and the test assertion was inverted** —
case (a) of the bug briefing, not case (b). **No production logic was changed.**

## Root cause

`internal/tools/taskproc_test.go:119` read:

```go
if !st.Resumable {
    t.Errorf("a row with a verified-live holder must NOT be resumable (that is the double-run guard)")
}
```

The `!` inverts the guard: the branch is taken exactly when `Resumable` is
**false** — i.e. when production correctly refuses the resume. The error text
("must NOT be resumable") could only be reached by a value that already
satisfies it, so the test could never pass while it existed.

Production, read rather than assumed:

- `taskVerdictResumable(verdict)` (`internal/tools/taskproc.go:448`) returns
  `false` for `TaskProcessStateRunning` and `TaskProcessStateForeign`, `true`
  otherwise — the double-run guard is implemented as intended.
- `taskProcessStatus` (taskproc.go:390) sets `st.Resumable =
  taskVerdictResumable(st.Verdict)`; the test's `Fatalf` at line 115 proves it
  reaches `st.Verdict == TaskProcessStateRunning`, so `Resumable` is genuinely
  `false` for a verified-live holder ⇒ the production double-run guarantee held
  all along and only the assertion direction was wrong.

Introduced by `d7474fb4` ("feat(task): persist a task's process identity
(pid+starttime+cmdline) and verify liveness with all three") — `git_blame` lands
both line 119 and line 120 in that commit; the inversion shipped with the
feature, not with any later merge, and is therefore pre-existing at master tip
`b5064f0a`.

Corroborating evidence — the sibling tests in the same file already used the
**correct** polarity for the same kind of claim:

| test | assertion | meaning |
|---|---|---|
| `TestTaskStatusRunningForOwnRow` (line 119, before) | `if !st.Resumable { Errorf("must NOT be resumable") }` | inverted |
| `TestTaskStatusForeign...` (line 302) | `if st.Resumable { Errorf("must NOT be resumable") }` | correct |
| `TestTaskStatusDeadForReapedChild` (line 143) | `if !st.Resumable { Errorf("must be resumable") }` | correct |
| `TestTaskStatusNoHolderIsReclaimable` (line 272) | `if !st.Resumable { Errorf("must be resumable") }` | correct |
| `TestTaskStatusNonInProgressIsNotApplicable` (line 331) | `if !st.Resumable { Errorf("is resumable") }` | correct |

1 of 5 assertions was inverted; the other 4 pin the same contract correctly, so
relaxing the guard (rather than fixing the polarity) was never an option — that
would have deleted the double-run coverage the epic exists for.

## Fix

Surgical (Rule 4), `internal/tools/taskproc_test.go`:

```diff
-	if !st.Resumable {
-		t.Errorf("a row with a verified-live holder must NOT be resumable (that is the double-run guard)")
+	// The double-run guard, asserted POSITIVELY against the field: the row's
+	// holder is a verified-live process, so it must NOT be resumable. (This
+	// line previously read `if !st.Resumable`, which demanded the row BE
+	// resumable — the exact inverse of both the message and the guard.)
+	if st.Resumable {
+		t.Errorf("resumable=true for a row whose holder is verified live — a resume would double-run it (the double-run guard demands false); note=%s",
+			st.Note)
+	}
+	// Pin the field to the production predicate that computes it, so this
+	// assertion cannot be re-inverted into a pass while the guard is intact,
+	// and so a guard regression cannot hide behind a re-inverted `if`.
+	if taskVerdictResumable(st.Verdict) {
+		t.Errorf("taskVerdictResumable(%q) = true, want false — production must refuse a resume while the holder is live",
+			st.Verdict)
 	}
```

Two hardening choices beyond the one-operator flip, both aimed at the *class* of
defect rather than this instance:

1. The message now interpolates `st.Note` (the evidence chain), so a future
   failure names the pid/start/cmdline that made the verdict "running".
2. The cross-check against `taskVerdictResumable` pins the field to the
   production predicate: re-inverting the `if` alone can no longer produce a
   green run, and a guard regression is reported by a second, independent
   assertion.

The test's doc comment was aligned with the property it pins (`Resumable false`
= the double-run guard, plus the cross-check).

## Verification

### Build

`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.

### Targeted test (was red, now green)

```
go test -run TestTaskStatusRunningForOwnRow -count=1 -v ./internal/tools/
--- PASS: TestTaskStatusRunningForOwnRow (0.03s)
ok  	github.com/nick/gopi/internal/tools	0.048s
```

### Package gate (Rule 5) — was FAIL, now ok

```
go test -count=1 ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	90.620s
```

### Consumer of the same contract (e2e half)

```
go test -run TestResumeGateRefusesLiveHolder -count=1 ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	0.067s
```

`cmd/agent/cli_resume_gate_test.go` drives the real refusal path, so the
end-to-end double-run guard is exercised independently of this field assertion.

### Negative controls (the assertion is not vacuous, and the defect was purely directional)

**A — production guard broken** (drop `TaskProcessStateRunning` from the refusing
case of `taskVerdictResumable`, so a live holder becomes resumable):

```
--- FAIL: TestTaskStatusRunningForOwnRow (0.04s)
    taskproc_test.go:127: resumable=true for a row whose holder is verified live — ... note=pid 2617894 is alive and matches the recorded identity (start=2026-09-13T13:46:22+08:00, cmdline identical)
    taskproc_test.go:134: taskVerdictResumable("running") = true, want false — production must refuse a resume while the holder is live
FAIL	github.com/nick/gopi/internal/tools	0.073s
```

**B — assertion direction re-inverted only** (production untouched, the original
bug shape):

```
--- FAIL: TestTaskStatusRunningForOwnRow (0.05s)
    taskproc_test.go:127: resumable=true for a row whose holder is verified live — ...
FAIL	github.com/nick/gopi/internal/tools	0.088s
```

B reproduces the reported symptom (red while production is correct), proving the
defect was directional; A proves the corrected assertion discriminates against a
real guard regression. Both production and test files were restored afterwards
(`git status` clean; targeted test `ok` again).

## Files

- `internal/tools/taskproc_test.go` (+18/-3)
- `devdoc/taskproc-resumable-assertion.md`

## Not Done

- No production-logic change (none was warranted — the predicate is correct).
- The full `go test ./...` differential over the whole tree was not run as the
  gate; `internal/tools` is the package this task touches and its gate is green.
- The sweep for inverted assertions is limited to the `Resumable` assertions of
  this epic (5 sites, 1 defective); a repo-wide audit of assertion polarity is a
  separate concern, not part of this bug task.
- Merge / archive cleanup is the PA's step (Rule 9); not done here.

## Commit

- `93fb9cd7` fix(tools): correct inverted resumable assertion in TestTaskStatusRunningForOwnRow

## Branch / recovery

- Branch: `fix/taskproc-resumable-assertion` (worktree
  `~/workspace/gopi-taskproc-assertion`, base master `b5064f0a`)
- Recover: `git show fix/taskproc-resumable-assertion:internal/tools/taskproc_test.go`

## Note on concurrent work in this worktree

Two sessions worked this same branch/worktree within a minute of each other:
`93fb9cd7` (test fix) + `e6fc76e2` (first devdoc) at 13:45:54/13:46:09, then
`8bd6ca64` at 13:46:49, which re-wrote the devdoc and thereby superseded
`e6fc76e2`'s file. This document is the union of both: `e6fc76e2`'s blame
attribution (`d7474fb4`, re-verified) and mutation red-proof, plus `8bd6ca64`'s
production-predicate cross-check and the A/B negative controls. The test file at
HEAD is byte-identical to `93fb9cd7`'s version plus that cross-check.

## Sibling tray: the `cmd/agent` guard red seen during the sweep is ENVIRONMENTAL

The confirmation sweep on this branch also reported `cmd/agent`
`TestAgentSpawnGuard{AllowsOperatorStyleLaunch,WorkerTaskRunChainIsAllowed,AllowsOperatorTerminalInsideAgentSession}`
failing with `inherited worker lock fd 3 is not a usable file: … write GOPI_DAEMON_READY_FD`.
Classified on ONE unchanged master worktree (`/tmp/probe-master` @ `b5064f0a`) with the
SAME built binary:

| run | env | result |
|---|---|---|
| `go test -run TestAgentSpawnGuard ./cmd/agent/` (no `bin/gopi` yet) | inherited | `ok 0.026s` (binary cases skipped by `testutil.GopiBin`) |
| same, `bin/gopi` built | inherited (`GOPI_DAEMON_READY_FD=3`, `GOPI_WORKER_DAEMON_CHILD`) | **FAIL ×3** |
| same, `bin/gopi` built | `env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD` | **ok 5.490s** |

`guardTestEnv` (agent_spawn_guard_test.go) strips only the three guard knobs, so a
worker-daemon-child session leaks the daemon handshake variable into the "operator-style"
child; `cli_worker_daemon.go:263-266` then correctly refuses an inherited fd 3 that is an
ordinary file. Test-isolation defect, not a guard regression — filed as its own bug task
(`01a09950-bd30-7f2c-820d-364bcd3f73d1`) with the fix direction (strip the daemon-handshake
constants in `guardTestEnv`, referencing named constants). Lesson for future triage: an
`cmd/agent` guard red is only real if it survives `env -u GOPI_DAEMON_READY_FD
-u GOPI_WORKER_DAEMON_CHILD`, and a test file predicated on `testutil.GopiBin` silently
self-skips its real assertions until the binary exists.
