<!-- doc-class: normative -->
# Ctrl+D mid-turn: the console that stopped reading the terminal

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

**Task:** `01a1650f-e0a2-700c-b9db-44a6e0554a51`
**Measured:** 2026-10-23, host `host-b`, `bin/gopi` built from `c9c0534`.

## The report

The operator pressed Ctrl+D in a `gopi chat` session and the process did not
exit; the SSH connection stayed up and **their Ctrl+C did get them out**. The
same session had just taken a 306.8 s `memory-sync-on-store` round and reported
an over-budget context (152403 tokens, compression assertion false).

**That last detail is where this report and my own measurement disagree, and
the disagreement is the most useful thing in the file.** On a pty, with the turn
held open by a peer that never answers, Ctrl+D at t+5.4 s left the process alive
at t+35.4 s and the Ctrl+C sent there ALSO did nothing — alive at t+65.4 s,
SIGQUIT needed (`repro3-inflight-ctrld.log`). So the console was not merely
slower to exit: from the instant of the Ctrl+D it had stopped reading the
terminal. The operator's Ctrl+C worked; mine did not. The difference is not the
key, it is what the turn was doing at the time — their turn was a real
`deepseek` round trip that WOULD eventually answer (so the turn ended, the
queued marker was reached, and the exit they saw may have been the marker rather
than the key), while the probe's turn was a black hole by construction. **Both
observations are true and neither is the whole story**, which is exactly why the
fix has to claim the turn instead of relying on the turn ending by itself.

> **Retired mechanism (memory-store sync decouple, `5e3ce2e`, 2026-10-27).** The
> store-triggered sync leg named below was DELETED, not disabled:
> `internal/tools/memory_store_trigger.go` (1033 lines) is gone, and with it
> `memoryStoreSyncRound` / `MemoryStoreSyncAfterStore` (both: zero definitions
> at HEAD). The sentence "a sync round is not the thing that blocked the chat
> loop" therefore cost nothing to keep true — but the round it is about no
> longer starts at all, so this page's reasoning now describes a mechanism the
> tree has not got. The page stays `normative` because its ACTIONABLE half is
> live: the defect was the line-editor turn seam, and
> `internal/lineedit/editor.go` (`InterruptTurn`, `OriginEOF`, `OriginCtrlC`)
> plus `testsuite/chat_pty_ctrl_d_midturn_test.go` are the code and the guard
> that still hold. Read the sync-round paragraph as the record of what the
> screenshot made look like the blocker; current state of that leg is
> `devdoc/memory-store-sync-decouple.md`.

The screenshot suggested the sync round was the blocker. **It was not.** The
sync round already runs on its own goroutine
(`internal/tools/memory_store_trigger.go:409`, `go memoryStoreSyncRound(...)`),
and `MemoryStoreSyncAfterStore` returns as soon as the decision is made.
The line number is a landing-time record taken at the revision this page
declares at the top (`c9c0534`, where `:409` reads
`go memoryStoreSyncRound(direction, peers, round)`); the file itself was removed
on 2026-10-27 in `5e3ce2e`, so no line in it — that one included — describes
anything at HEAD. Nothing resolves this anchor today; it is quoted so a reader
can see what the 2026-10-23 run was looking at.

## The reproduction, on a real pty

`~/.gopi/sessions/01a1650f-…/evidence/pty_ctrld_probe.py` drives the built
binary on a pty, waits for the prompt, optionally types a prompt, presses a key,
and — if the process is still alive — sends `SIGQUIT` and captures the Go
goroutine dump off the same pty.

A listener that accepts and never answers (`hangserver.py`) keeps a turn
genuinely in flight, which is the state the operator was in.

| run | state | key | outcome |
|---|---|---|---|
| `repro1-ctrld-idle.log` | at the prompt | Ctrl+D | exited in **0.766 s**, rc=0 |
| `repro3-inflight-ctrld.log` | turn in flight | Ctrl+D | **alive at 35 s**; Ctrl+C then sent → **still alive at 65 s**; SIGQUIT needed |
| `after-fix-inflight-ctrld.log` | turn in flight | Ctrl+D | exited in **0.019 s**, rc=0 |

Two harness mistakes on the way, both worth recording because they look like
results: a pasted `hello\r` never submits (the editor suppresses a pasted CR),
and a burst `b"\x04"` sent alongside other bytes is treated as paste content
rather than a keystroke. Every probe step now separates its bytes.

## The root cause

Two layers disagreed about who owns the end-of-input key mid-turn.

1. `internal/lineedit/editor.go`, `case KeyEOF` with an empty buffer, returned
   `Result{EOF: true}` **without consulting the turn seam** — while its sibling
   `case KeyInterrupt` did consult it (`e.InterruptTurn(OriginCtrlC)`).
2. The REPL's feeder goroutine (`cmd/agent/cli_chat.go`) queues the EOF and then
   **returns**: `lineCh <- lineRead{eof: true}; if !ok { return }`.
3. The main loop cannot read that queue entry while it is inside `ag.Chat` — the
   turn's HTTP round trip against a peer that never answers. The goroutine dump
   shows `main.runChatWithInput` parked on the select at `cli_chat.go:3187`,
   *waiting for a turn that never returns*, and no goroutine reading the pty at
   all.

So the failure is not "the exit is slow". It is: **after Ctrl+D the console
stops reading its terminal altogether**, which is why the Ctrl+C that would
normally interrupt the turn did nothing either. That is the defect the fix
addresses.

## The fix
- `KeyEOF`'s empty-buffer arm now calls `e.InterruptTurn(OriginEOF)` **before**
  returning the marker. The turn is cancelled; the queued marker is then read at
  the boundary the cancel creates, and the session ends for the reason it
  already recorded (`eof`).
- `lineedit.OriginEOF` (`"ctrl-d"`) joins `KeyOriginSet`, and
  `agent.InterruptReasonEOF` (`"ctrl-d"`) joins `InterruptReasonSet`, so the
  session JSONL names the key the operator actually pressed instead of borrowing
  Ctrl+C's word (`chatInterruptReason`, one mapping site).

End-to-end proof, from the fixed binary's own session log:

```json
{"partial_chars": 0, "reason": "ctrl-d", "source": "operator", "type": "turn_interrupted"}
{"elapsed_sec": 5.34, "eof_kind": "key", "exit_code": 0, "reason": "eof", "type": "session_end"}
```

## The tests, and the red proof that they catch THIS defect

Two levels, because they answer different questions.

**Unit — `internal/lineedit/eof_turn_test.go`** (3 cases). Red with the fix
removed: `the claim reported origins [], want exactly ["ctrl-d"]`. It states the
rule ("the key claims the turn it interrupts") but it cannot say what the
operator sees.

**Acceptance — `testsuite/chat_pty_ctrl_d_midturn_test.go`.** A real pty, the
built binary, a turn made deterministic with `testutil.Resp{Hold: true}` — the
same technique `chat_queue_pty_test.go` and `chat_pty_escape_interrupt_test.go`
use. It asserts four things the operator actually needs: the process exits 0; the
JSONL records `turn_interrupted` with `reason=ctrl-d` and `source=operator`; the
session ends with `reason=eof`, `eof_kind=key`, `exit_code=0`; and the turn was a
LIVE one (the mock served exactly one request, and the question is in the
transcript).

**The red proof is the same test with only the BINARY swapped** — the fix
reverted in `internal/lineedit/editor.go`, `GOPI_BIN=<binary built from the
pre-fix tree>`, everything else identical:

| binary | result |
|---|---|
| pre-fix | **FAIL** `chat did not exit within 1m0s` (78.00 s) |
| fixed | **PASS** (30.81 s) |

That failure line is the operator's own hang, as a test result. The reverted file
was restored and verified by CONTENT against a revision
(`git hash-object` vs `git rev-parse HEAD:…` → identical), not by an
index-relative diff — Rule 5's criterion, which an index-seeded restore quietly
satisfies while writing back the wrong bytes.

One deliberate constraint on the acceptance file: it spells the reason values as
**literals** and imports nothing from `internal/agent`. On the pre-fix tree
`agent.InterruptReasonEOF` does not exist, so a version that referenced it would
fail to COMPILE there — and a build error proves nothing about behaviour. The
values are pinned at their definitions and cross-checked by `cmd/agent`'s
exhaustive origin→reason test.

## What was NOT fixed, and why it is a separate defect

`/quit` typed **mid-turn** still does not exit until the turn ends
(`after-fix-inflight-quit-gapped.log`: alive at 20 s, SIGQUIT needed). It does
not make the console deaf — a following Ctrl+C *does* land
(`after-fix-quit-then-ctrlc-gapped.log`: exited rc=0, `bye.`,
`turn interrupted by ctrl-c`), because the feeder goroutine keeps reading after
a line. `/quit` is an ordinary line: it is queued and answered by the loop's
slash switch (`cli_chat.go:3377`), which is only reachable at a turn boundary.

That is the design of every REPL-native command and is NOT the same bug: the
session is repairable by Ctrl+C. Making `/quit` mid-turn cancel the turn would
change what every slash command means, so it is left as a separate decision.

## The four questions this task was opened with

1. **Does the sync round block the caller?** No. `MemoryStoreSyncAfterStore`
   returns at the decision; the round runs on `go memoryStoreSyncRound` (line
   409). Its 216.8 s / 306.8 s are the round's own ssh leg, reported through the
   hook — not the chat loop waiting.
2. **When are Ctrl+D and `/quit` checked?** Ctrl+D is a keystroke the editor
   classifies (`KeyEOF`) — it acted immediately, and its *consequence* (the queue
   entry) could not be read. `/quit` is a **line**, so it is only ever checked by
   the main loop at a turn boundary.
3. **Are two `--serve-assets` processes normal?** Yes. The store trigger spawns
   one child per round (`memoryStoreSyncRunChild` → argv containing
   `memory sync --push`), and the peer side runs `gopi memory sync
   --serve-assets` for the duration of that exchange
   (`memory_sync_assets.go:642`). Two peers (or two rounds across two machines)
   produce exactly the observed pair. It is not a leak.
4. **What does `pull{skipped=9376} (0.0s, bound=0s)` mean?** It is the
   `--push` line: `PullSkippedRows` is the number of rows the peer offered that a
   push deliberately does NOT apply (`memory_sync_assets.go:194`, `:423`), and
   `bound=0s` is `TimeoutSec` reading the **leg's own** stats object — which
   `initiatorExchangeDefault` returns freshly built, so the caller's
   `stats.TimeoutSec = bound.Seconds()` (line 375) is stamped on a DIFFERENT
   object than the one printed. The bound is really the 15-minute default
   (`memoryAssetSyncSSHTimeout`); printing `0s` is a reporting defect, not a
   missing bound.

## Rules this touches

- Rule 31: the surface was executed — a pty, the real binary, real keys.
- Rule 10.5: the fix ships a regression test (`internal/lineedit/eof_turn_test.go`),
  shown red with the fix removed and green with it in.
- Rule 15: the new origin is mapped at the ONE site that maps origins; an unknown
  one still reports on stderr rather than being swallowed.
