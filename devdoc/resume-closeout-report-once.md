<!-- doc-class: historical -->
# Task Report: one run emits the close-out report once (epic `01a0d9aba1f075fab14229816b085146`)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## Completed

**The defect.** ONE `gopi worker --resume --branch <name>` run printed the entire
completion report **twice** and the close-out refusal line
`<taskid> skipped: worktree in use by live process …` **three times**
(operator evidence: `/tmp/strange_python.txt`, 66 lines, peerbox 2026-09-26).

**The mechanism (measured, not inferred).** One requested close-out grew three
writers in ONE process tree:

| # | writer | where | how it renders |
|---|---|---|---|
| ① | the resumed session's end hook | `internal/agent/taskregister.go` | `[gopi] finish-cleanup: skipped: …` (agent logger → stderr) |
| ② | the resume tail | `cmd/agent/cli_resume.go` `runResumeFinishCleanup` | `<taskid> skipped: …` (stdout, the chain's own renderer) |
| ③ | the resuming parent's close-out | `cmd/agent/cli_worker_resume.go` `runTaskWorkerResumeCleanup` | the same line, a second time |

Reproduced first, on the real binary (`GOPI_BIN=bin/gopi`): the count came out
**①1 + ②1 + ③1 = 3**.

**The fix — one decision per chain, made before the run, by whoever can be
trusted to still be alive.**

- **Owner declared up front, not discovered mid-run.** `gopi resume` now
  declares, BEFORE the session starts
  (`resumeDeclareCleanupOwnership` → `closeouthandoff.Declare`), that its own
  tail will report the chain for that row. **Measured reason for the position**:
  the end hook runs FIRST (it is inside `runEngine`'s defer, which fires when
  `Agent.Resume` returns), so a declaration made after the run would arrive too
  late and ① would already have asked. This is what makes the hook's decision a
  fact rather than a guess.
- **Cross-process half declared through the environment.** A parent cannot see a
  child's memory, so `workerResumeChild` exports
  `GOPI_RESUME_CLOSEOUT_PARENT_REPORTS=1` for the child it spawns — set before
  the child's first instruction, the same deterministic convention
  `GOPI_TASK_WRAPPER_ID` uses. Both of the child's writers then defer, **naming
  the parent**, and the parent makes the one report. This is strictly stronger
  than the in-process latch I first built (it needed no timing assumption and it
  is honest even when the child dies early: the ask stays with the parent).
- **No silent suppression.** Every deferral PRINTS a note naming the covering
  writer and carrying none of the report's own spelling — a suppressed writer
  with no named coverer is a dropped close-out wearing a clean run
  (`devdoc/client-closeout-delivery.md`), which is worse than a duplicate.
- **The tail never reads its own declaration.** It declares for the hook; if it
  also consumed the declaration it would mute the only reporter left on any path
  where the hook did not run (the panic recover, a wrapper-latched engine). Pinned
  by `TestRunResumeFinishCleanupNeverDefersToItsOwnDeclaration`.
- The chain is still asked unconditionally by the parent — the ask covers a child
  that died before its own hook. Only the PRINTING is single-sourced.

**Regression test (the epic's acceptance instrument).**
`cmd/agent/cli_worker_resume_emission_test.go` runs the real binary through
`gopi worker --resume --branch <name>` with a holder process pinning the
worktree (so the chain lands in the very state the operator hit — a correct
refusal) and counts, in the merged capture: the report **exactly once**, each of
the two deferrals **exactly once**, the answer **once on stdout**, and the
deferral spellings disjoint from the report's. It is RED before the fix
(measured 3) and GREEN after (measured 1).

## Not Done (honest — the epic's own scope line)

- **The live stderr answer stream is NOT de-duplicated.** `Agent.OnText` and the
  non-chat path both write the final text, so a caller who merges both streams
  reads the answer twice (measured: the mock's second round replays its script,
  which is why the count read 3 in the test). De-duplicating it means changing
  the streaming contract (`--no-stream`-style), a broader change than the report
  count; **the report's own count is 1, which is what this epic measures.** The
  regression asserts the answer once on **stdout** (the result channel), not once
  in the merged capture.
- **Releasing the worker face's own ground is reported, not performed.**
  `runTaskWorkerResumeCleanupFor` resolves the row through the ledger
  (`tools.TaskResumeGroundFor`) and names it, plus the step that frees it
  (`gopi worktree release`). Running a second chain for that row is part of the
  wider close-out sweep — deliberately not landed here, and stated as such in
  the code.
- **`gopi merge` with no argument refused by a silent ledger read** (found while
  investigating; unrelated to this epic) — not filed as a task in this session.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK;
  `go build ./...` → OK; `go vet ./cmd/agent/ ./internal/agent/
  ./internal/closeouthandoff/` → clean.
- **Tests:** `go test -count=1 -run 'TestWorkerResumeEmitsTheCloseoutReportOnce'
  ./cmd/agent/` → **ok 23.5s** (RED before the fix, GREEN after);
  `-run 'TestRunResumeFinishCleanup|TestResumeTailDeclares|TestDeferralNotes|
  TestResumeRunsFinishCleanup|TestResumeFinishCleanupSkips|
  TestRunResumeFinishCleanupNever' ./cmd/agent/` → **ok**;
  `go test ./internal/closeouthandoff/` → **ok**.
- **Ran:** `GOPI_BIN=$PWD/bin/gopi GOPI_TASK_REPORT_PATH=… go test -count=1 -v
  -timeout 300s -run TestWorkerResumeEmitsTheCloseoutReportOnce ./cmd/agent/` —
  the real CLI verb (`gopi worker --resume --branch feat/worker-resume-e2e`)
  driven against a real ledger + linked worktree, with a live holder process in
  the ground.
- **Result:** before the fix the merged capture carried the refusal line 3×
  (child's hook, child's tail, parent); after it, **exactly 1×, printed by the
  worker face**, with the child's two writers each printing
  `… deferred to the resuming parent (gopi worker --resume) …`. Exit code 0
  (a close-out refusal is not the run's verdict, unchanged).
- **Commit:** (this branch) — see the commit below.

## Evidence (self-filed)

- `grep -o 'skipped: worktree in use' /tmp/strange_python.txt | wc -l` → `3`
  (the operator's capture, the defect as reported).
- Pre-fix run, real binary: `the run's answer must appear exactly once … got 3`
  + `merged capture` carrying `[gopi] finish-cleanup: skipped: …` (hook) and
  `t-worker-resume-e2e skipped: …` twice (tail + parent) — the RED observation.
- Post-fix run: `grep`-visible counts in the captured output —
  1 × `skipped: worktree in use by live process`, 1 ×
  `[gopi] finish-cleanup: deferred to the resuming parent`, 1 ×
  `[gopi] gopi resume: close-out for t-worker-resume-e2e is reported by the
  resuming parent` — the GREEN observation; test exit 0.
- `go test -count=1 ./internal/closeouthandoff/` → `ok … 0.002s` (the latch's
  contract: consume-once, row-scoped, zero value undeclared).
