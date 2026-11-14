<!-- doc-class: historical -->
# Task Report: chat-once/bare-prompt SIGTERM → session_end zombie fix (with regression guard)

## Context

BUG (2026-09-05 evidence, t-1788514086/1788514159/1788514437): chat-once /
bare-prompt processes (`gopi --settings X "Reply..."`) killed by SIGTERM died
by the Go runtime's default signal disposition — runEngine's deferred
`session_end` write + auto-ledger row close never ran. Task text completed,
but the session JSONL had no `session_end` and the auto ledger row stayed
`in_progress` forever (manual cancel to clean).

Root cause: the session finalization lives in a defer inside runEngine, which
only runs on normal return. Uncaught SIGTERM/SIGHUP terminates the process
immediately, skipping all defers. `task run <id>` had a wrapper waitpid
reconciliation fallback; bare-prompt/chat-once had none. Precedent fix:
cli_web.go bug003 (signal.Notify SIGINT/SIGTERM → graceful shutdown), not yet
ported to the chat-once path.

## Completed

### 1. Agent core — cancellable interrupt root (commit 4e2840c)

- `Agent` now owns a cancellable base context (`interruptBase`/`interruptCancel`
  created in `NewAgent`), with an `interrupted` latch and an exported
  `Interrupt()` + `interruptRoot()`/`interruptedBySignal()` accessors
  (internal/agent/loop.go).
- All 4 run-ctx sites (`runEngine`, `Chat`, `ChatOps` × deadline/cancel) now
  derive from `a.interruptRoot()` instead of `context.Background()`, so an
  external `Interrupt()` reaches the in-flight run.
- runEngine's round-boundary `ctx.Done()` check and the in-flight-chatOnce
  error map both route through `interruptedBySignal()` → return
  `interruptErr` → the normal end-of-run defer runs: `session_end`
  (exit_code=1) + `finishSessionTaskRecorded` (auto row failed). `interruptErr`
  declared top-of-file with signal semantics comments.

### 2. CLI wiring — signal handlers call Interrupt() (commit efb74e3)

- **main.go bare-prompt path**: `signal.Notify(sigCh, os.Interrupt,
  syscall.SIGTERM)` → goroutine prints `[gopi] <sig> received — finalizing
  session…` and calls `ag.Interrupt()` → runEngine unwinds through its
  end-of-run defer (session_end exit=1 + auto row failed).
- **cli_chat.go runChat REPL**: `signal.Notify` + `interruptCh` closed on the
  first signal; the REPL loop `select`s on `interruptCh` before/at each prompt
  read and breaks to `finishChat(ag, ..., 1)` (session_end + CloseChatRow);
  an in-flight `Chat`/`ChatOps` returns `interruptErr` → runErr path →
  `finishChat(1)`.
- SIGKILL remains uncatchable — the sweep-zombies / waitpid reconciliation
  stays as the fallback (documented, acceptable).

### 3. Merge discipline (this session's merge-gap find)

The branch carried TWO commits (core 4e2840c + wiring efb74e3). Master's
merge at 05:14 landed only the FIRST commit (the merge raced ahead of the
second commit at 05:19). **Master had the interrupt plumbing with NO caller —
the fix was inert.** Detected by grepping master's merged files for the
caller (`signal.Notify` absent from main.go/cli_chat.go). Landed the missing
commit via safe-merge (worktree removed first → gate exit 0) + clean merge
(582d315). Lesson stored in memory (#1431).

### 4. Regression test + harness support (commit dc0e307)

- `internal/testutil/mockllm.go`: added `Resp.Hold` — the mock accepts the
  request, writes SSE headers, flushes, then blocks until `ReleaseHold(n)`
  (or the client aborts via `r.Context().Done()`). `HeldCount()` reports
  in-flight holds; `Close()` releases any stragglers (httptest.Close
  deadlock guard).
- `internal/testutil/run.go`: added `Proc.Signal(sig)` for SIGTERM delivery;
  fixed a latent `Stop()`-after-`Wait()` deadlock — `done` is 1-buffered, so
  once `Wait()` consumed it, `Running()` reported true forever and a deferred
  `Stop()` blocked on `<-done`. Added a `waited` flag: `Wait()` sets it,
  `Running()`/`Stop()` respect it.
- `testsuite/task_test.go`: `TestTask_BarePrompt_SIGTERM_WritesSessionEnd`
  — the mock holds the first request in-flight, the test SIGTERMs the real
  binary, and asserts: exit code 1 (NOT 143/137 default-disposition death),
  exactly one session_end with exit_code=1, and the `finalizing session`
  stderr notice. **Passes in 0.72s, keyless, deterministic** (vs the web
  bug003 test which needs a real API key).

### 5. Regression found & fixed in the SIGTERM fix itself (commit dc0e307)

The feeder goroutine introduced by efb74e3 (hoisting the blocking
ReadString so the REPL could select on a signal) used a **drop-on-full
channel send** (`select { case ch <- x: default: }`). Piped input lines —
and critically the EOF marker — arriving while the loop was mid-turn were
silently discarded; the loop then blocked forever on `lineCh`
(TestChat_HappyPath_TwoTurnsSameSession hung 180s, TestChat_ToolsToggle
hung). Fixed to a **blocking send** (`lineCh <- lineRead{line, err}`): the
loop is the sole consumer, a full channel only means mid-turn and will
drain; a blocking send cannot deadlock, and a signal-driven return kills the
goroutine with the process. Lesson stored in memory (#1433). The two chat
REPL tests are the regression guard (they failed with the drop, pass now).

## Not Done

- SIGKILL (uncatchable) still leaves the sweep-zombies reconciliation as the
  fallback — documented in code, accepted (aligns with waitpid semantics).
- Deployed-plugin drift `TestWebFetchContract_DeployedScriptSync`
  (arxiv_search.py repo vs ~/.gopi deployment) — pre-existing, unrelated to
  this fix (my diff doesn't touch plugins); left untouched per isolated-work
  rules.
- Live kill-SIGTERM verification against a REAL model (the mock-backed test
  is deterministic and keyless; a live bare-prompt `kill -TERM` check is
  optional follow-up for the operator).

## Verification

- Build: `go build ./...` OK; `go vet ./cmd/agent/ ./internal/testutil/
  ./testsuite/` clean; gofmt clean on all touched files; `bin/gopi`
  rebuilt (CGO_ENABLED=0 go build) — `./bin/gopi version` = 582d315+ (tracks
  master tip).
- Tests:
  - `TestTask_BarePrompt_SIGTERM_WritesSessionEnd`: PASS (0.72s)
  - `testsuite` full (mock-backed three-mode): PASS except the pre-existing
    unrelated plugin-sync drift test
  - `TestChat_*` (Three REPL tests — the feeder regression guard): 3/3 PASS
  - `internal/agent`: PASS (44s)
  - `cmd/agent` (incl. Web/Chat/Task + bug003 signal tests): PASS (108s)
- Commit: dc0e307 (fix(chat): blocking feeder send + SIGTERM closeout
  regression test), on master. Fix halves merged: 4e2840c (agent core) +
  efb74e3 (CLI wiring) via merge 582d315.

## Acceptance mapping

- 裸 prompt 中途 `kill -TERM` → session JSONL 有 session_end exit_code=1 ✓
  (mock-held in-flight request, SIGTERM, asserted session_end=1 exit=1)
- auto 行 completed/failed 非 in_progress ✓ (runEngine defer → finishSessionTaskRecorded failed)
- 正常完成无回归 ✓ (full agent/task/chat/web suites green)
