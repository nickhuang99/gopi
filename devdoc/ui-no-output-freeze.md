<!-- doc-class: historical -->
# Task Report: `gopi ui` — "no output, looks stuck" (the turn that was never stopped)

Task: `01a0d2f04e6e763884ad3eb89e329bb1`
Worktree: `/home/user/workspace/gopi-01a0d2f04e6e763884ad3eb89e329bb1`
Branch: `task/01a0d2f04e6e763884ad3eb89e329bb1`
Base: master `99f6bbf3`

## The report

The operator's `gopi ui --tools` (session `cb220845`, pid 22202) stopped producing
output and looked stuck; it was left running for inspection.

## Completed

### What the stuck process actually was (evidence, not a guess)

| probe | reading | meaning |
|---|---|---|
| `/proc/22202/status` + `task/*/wchan` | 11 threads, sleepy, `ep_poll` on the two epoll threads, ~1% CPU | an idle event loop and a live SSE read, not a spin and not a deadlock |
| `/proc/22202/io` `wchar` | grew steadily over every sample | the process was still writing — the transport was alive |
| `select.poll` on `/dev/pts/1` | `POLLOUT`, `TIOCINQ=0` | the terminal was still attached and drained |
| session tree | `cb220845.raw.log` grew continuously (1.9 MB → 4.3 MB) | **the ENGINE was still working the whole time** |
| `session.jsonl` | grew with it | the turn was live, not lost |

So the process was not hung: it was a live engine running a turn that the operator
believed was over, in a surface that had nothing left to say about it. The
reported "no output" was the honest state of a surface that had already stopped
talking about a turn the engine still counted as in flight.

### Root cause

Two facts combine, both in the code:

1. **Ctrl+X cancelled only the client's stream.** `internal/uiclient/model.go`
   `interrupt()` called the *local* cancel of the turn's `context`; the surface
   then said `interrupted (Ctrl+X)` and went idle. The engine never heard about
   it: `cmd/agent/cli_web.go` `handleChat` deliberately ignores client disconnect
   (its `r.Context().Done()` watcher was removed after it panicked "close of closed
   channel"), so the SSE socket closing ends one response body and nothing else.
2. **The engine could not be stopped per turn anyway.** Every run context derived
   from `Agent.interruptRoot()`, whose cancel is `Agent.Interrupt()` — built once
   in `NewAgent`, cancelled once, **never re-armed**. That is right for a signal
   handler on a process about to end, and useless for an interactive "stop this
   turn": after one such call every later turn's context is born cancelled.

The consequence is the reported freeze: the abandoned turn ran to completion
holding the server's single-turn lock (`s.mu`, with `webTurnTimeout()` defaulting
to an unlimited budget), and the operator's next message waited behind it while
the surface showed an unchanged idle-looking line. An unnamed wait on a screen
that never changes is indistinguishable from a dead process — which is exactly
what was reported.

**Verdict on my own first hypothesis.** I initially suspected the operator had
been driving the surface through a stray automation session on the fixed default
port (the session's turns at 18:37 came from nowhere in sight). A process listing
refuted it: no driver process existed. The trail led instead to the surface's own
interrupt path — recorded here because the refutation is part of the finding.

### The fix

**1. A run-scoped interrupt exists** (`internal/agent/loop.go`):

- `runScopedRoot()` returns the interrupt root with a **cancel scoped to this
  run**, published on the agent under `runMu`; the caller's existing `defer
  cancel()` releases it, so it cannot outlive the turn.
- `InterruptRun() bool` cancels **that** context and reports truthfully whether
  there was a run to cancel (a false "interrupted" is the one lie the surface must
  not tell). The signal path `Interrupt()` is untouched in behaviour.
- All three run-ctx sites (`chat_ops.go` `ChatOps`; `loop.go` `RunStream` and the
  one-shot path) now derive from `runScopedRoot()`.

**2. A route that stops the turn** (`cmd/agent/cli_web.go`):
`webChatInterruptPath = "/api/chat/interrupt"`, `handleChatInterrupt` POST-only,
refused while shutting down (the same interlock the reset route uses), answering a
JSON ack built from what actually happened. **It deliberately does not take
`s.mu`** — a handler queueing behind the very turn it was asked to abandon is the
opposite of the request.

**3. The surface tells the engine** (`internal/uiclient/{wire,stream,run,model}.go`
+ `cmd/agent/cli_ui.go`):

- `uiclient.InterruptPath` + `Client.Interrupt()` (bounded 10 s POST; an error
  names what went wrong — a surface that says "interrupted" while the engine runs
  on is lying about the one thing the key was pressed for).
- `Config.OnInterrupt` carries the hook into the model; `interrupt()` calls it next
  to the local cancel. `gopi ui` builds it as **in process** (the agent this
  carrier started — `InterruptRun`, no HTTP, no token) or **over the wire**
  (`Client.Interrupt`) when the engine belongs to somebody else (`--server`). Both
  failure modes print through the engine log, because the display owns the
  terminal (`silenceEngineConsole` redirects the process's stderr).

**4. A wait is named, never silent** (`internal/uiclient/model.go`): a `queued`
frame now records when the wait began, the status line reads
`queued… <seconds>`, and a repaint chain (`queuedWaitTick = 1s`) keeps the elapsed
time moving so a long wait is visibly alive instead of a frozen line. The chain is
scheduled once per wait and dies with the turn.

### Not Done (honest)

- **The decisive A/B did not discriminate** — see "The one verification this
  report does NOT have" below. The fix is a real improvement (the surface's
  Ctrl+X now reaches the engine instead of cancelling only its own stream), but
  the operator's exact freeze is NOT demonstrated fixed, and the two candidate
  reasons are both unfixed: the hook may not fire in the `ui --tools --addr`
  shape, or the engine's run context may not be the one the tool actually waits
  on. This needs a follow-up (log at the hook + at `InterruptRun`, driven by
  `ab7.py`), not a claim.
- Two deeper features were found and NOT implemented, recorded for the follow-up:
  the bash tool never kills its child (`internal/tools/bash.go` uses
  `exec.CommandContext` without `WaitDelay`/process-group kill), and a Ctrl+X
  that arrives while a tool executes produces an unbalanced `tool_use` in the next
  request when the tool's result is not appended (mitigated upstream by
  `repairToolUsePairs`, so a property of the window, not a known break).
- **The live session was not re-driven.** pid 22202 predates this build and was
  left running on purpose; the fix is verified on a freshly built binary (below),
  not by repairing that process.
- `cmd/agent/cli_web.go`'s route table is long-lived (`http.ServeMux`), so the new
  route is registered where every other route is; no separate route-inventory gate
  was added for it.
- The `--server` (attach to somebody else's engine) interrupt path is exercised
  through the same `Client.Interrupt()` the local path uses and by the handler
  contract test; it was not driven against a second host.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → clean (exit 0); `go vet
  ./internal/uiclient/ ./internal/agent/ ./cmd/agent/` → clean.
- **Tests:** branch-scoped to the touched packages, listed in `## Evidence`.
- **Ran:** (Rule 31 — the changed surface, with real arguments)
  - pty run of the surface end to end (`/home/user/workspace/uidbg/pty_sched.py`
    against `/tmp/gopi-new`): the status line advances
    `streaming… → idle · turns 1`, i.e. the turn completes and the surface keeps
    painting.
  - The repo's own pty driver `scripts/ui/drive_ui.py` against the new binary:
    `steps completed: 7/7` with `COUNTERS render_frames=50 render_bytes=10951
    turns=4 turn_frames=[3 10 7 13]`.
  - The new route, POSTed for real against a live engine: `POST
    /api/chat/interrupt` → `200 {"interrupted":false,"ok":false}` (no turn in
    flight — the ack is honest), `GET` → `405`.
- **Result:** see Evidence. **The decisive leg did NOT discriminate** — read the
  box below before trusting the fix for the operator's exact symptom.
- **Commit:** see the commit hash below.

### The one verification this report does NOT have

The A/B that was supposed to prove the fix end to end
(`/home/user/workspace/uidbg/ab7.py`, four probe designs, ports 8201–8252) could
not tell the two binaries apart, and the earlier probe designs were not evidence
at all:

| probe | what it measured | why it did not settle the question |
|---|---|---|
| `ab2.py` | engine artifact growth across Ctrl+X | globbed `$SB/sessions/*/*` while the engine writes `$SB/.gopi/sessions/*/*` — **it measured 0 B on every window on both binaries** (an instrument reading zero, not a result) |
| `ab2.py` (fixed glob) | same | a turn that FINISHES also stops growing: `STOPPED` is what a normal completion looks like, so the reading cannot separate "abandoned" from "finished" |
| `ab3/4/5.py` | whether the NEXT message is answered | answered in ~1 s on BOTH binaries; `ab4.py` additionally proved nothing because the first message had not reached the engine yet (the surface still said `nothing to interrupt`) — caught by `ab5.py`'s liveness gate |
| `ab6.py` | the tool's own child (`sleep 50`) after Ctrl+X | alive on both — the tool's child living on is a property of `exec.CommandContext` + the bash tool, not of the interrupt path |
| `ab7.py` | engine growth AFTER the tool would have ended | 57 483 B (old) vs 56 993 B (new): **both engines kept working after the key** |

So the honest state is: the route exists and answers truthfully, the surface's
Ctrl+X now *reaches* the engine, and the engine keeps working in this harness
anyway. Either the hook does not fire in the `ui --tools --addr` shape (in which
case Ctrl+X still only detaches the display), or the engine's run context is not
the one the tool actually waits on. **Both are defects of the same size as the
one this branch set out to fix, and the operator's freeze is NOT demonstrated
fixed.** The next step is a log line at the hook and at `InterruptRun`, driven by
`ab7.py` — the driver is sound and reusable (it discriminates a live turn from an
idle surface, which the original probes did not).

## Evidence (self-filed)

```
# the engine's own view of the freeze (the stuck live session, master build)
$ stat -c '%y %s' ~/.gopi/sessions/cb220845-.../*.raw.log   # 18:09 → 18:13, 1.9 MB
$ stat -c '%y %s' ~/.gopi/sessions/cb220845-.../*.raw.log   # 18:37, 4.3 MB — growing while the operator saw "idle"
$ for t in /proc/22202/task/*; do cat $t/wchan; done        # ep_poll ×2, futex ×9 (idle loop, live socket)
$ awk '/^wchar/{print $2}' /proc/22202/io (over 10 s)       # +2096 B — still writing
$ poll(/dev/pts/1)                                          # POLLOUT, TIOCINQ=0 — terminal alive, drained

# the new regression tests (before the fix these fail: no hook, no route, no wait state)
$ go test ./internal/agent/ -run 'TestInterruptRun|TestInterruptStillReaches' -v
--- PASS: TestInterruptRunCancelsOnlyTheRun
--- PASS: TestInterruptRunWithoutARunIsFalse
--- PASS: TestInterruptStillReachesARunDerivedFromTheRoot

# the changed surface, POSTed for real against the OLD binary (pre-fix master build)
$ curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8094/api/chat/interrupt
404          # the route does not exist — there was no way to stop a turn
```

## Files

- `internal/agent/loop.go` — `runScopedRoot`, `InterruptRun`, run-scoped fields
- `internal/agent/chat_ops.go` — the ChatOps run ctx derives from the run scope
- `internal/agent/run_interrupt_test.go` — new
- `internal/uiclient/wire.go` — `InterruptPath`
- `internal/uiclient/stream.go` — `Client.Interrupt`
- `internal/uiclient/run.go` — `Config.OnInterrupt`
- `internal/uiclient/model.go` — engine hook, `queued…` wait state, wait repaint
- `internal/uiclient/client_test.go` — two new regression tests
- `cmd/agent/cli_web.go` — the interrupt route + handler
- `cmd/agent/cli_ui.go` — wiring (in-process vs over-the-wire hook)
- `cmd/agent/cli_ui_wire_guard_test.go` — route/const mirror + handler contract
