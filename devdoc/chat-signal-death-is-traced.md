<!-- doc-class: historical -->
# A chat that handled a signal is now traced — `category: signal` and the provenance record that was never written

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Status: shipped. Epic `chat-vanish-trace` (session `ab9e0023`, 2026-10-21).

## The report, and what the artifacts actually said

The operator's report was "chat sessions vanish without a trace": the process ends,
the screen shows only a close-out line, and afterwards nothing explains it. Session
`ab9e0023-69cc-4f98-bc08-7a3a4d0966f7` (ended 2026-10-21 17:16) is the specimen:

| artifact | value |
|---|---|
| `console.log` | `[gopi] hangup received — finalizing chat session…` then `bye.` |
| exit status | `1` |
| `session.dump.json` | `"category": "unknown"`, `"error_body": ""` |
| `signal-provenance.jsonl` | **absent** |
| `session.jsonl` `signal_verdict` events | 0 |

The screen said "orderly". Both durable artifacts said "something unrecognised
happened" — and the one artifact that would have named the signal did not exist.
That is the discrepancy the report describes, and it reproduces on demand.

## The reproduction (before any change)

A real `gopi chat` on a real pty, in a private `GOPI_HOME` sandbox, sent the same
signal a terminal teardown sends:

```
[gopi] hangup received — finalizing chat session…
bye.
exit status: 1
signal-provenance.jsonl: ABSENT
session.dump.json: {"category":"unknown","error_body":""}
```

Harness: the run's own evidence directory, `probe_chat_hup.py` (a pty pair, the
child in its own session exactly as a teardown leaves it, then `kill(pid, SIGHUP)`).

## Root cause — TWO independent defects, one symptom

They are separate bugs that happen to look identical to an operator reading the
session directory afterwards. Each has its own fix and its own red proof.

### 1. The dump's unknown default was the only arm a signal death could reach

`CloseChatSession` collects a failure record whenever the exit code is non-zero —
and a handled signal closes the session through the ordinary close-out with exit 1.
Every arm of `collectErrorRecord` (`internal/agent/crashdump.go`) keys on a **run
error**: a status, a body, transport text, a panic. A signal death has none of
those, so the record fell through to `deepseek.CategoryUnknown` with an empty body.
`unknown` is documented as "failure with neither status, body, nor transport text"
— which is true and useless: the cause was known exactly, by the goroutine that
decoded the signal, and nothing carried it to the collector.

**Fix.** The carrier declares the cause at the moment it knows it, and the
collector reads the declaration:

- `internal/deepseek/lasterr.go` — new `CategorySignal = "signal"`, documented as
  "ended by a catchable signal this process handled and unwound through its own
  close-out", explicitly *not* a failure of the request.
- `internal/agent/loop.go` — `Agent.CloseSignal string`, set by the carrier.
- `internal/agent/crashdump.go` — a signal arm that files `CategorySignal` plus a
  sentence naming the signal and stating that the run did not fail.
- `cmd/agent/cli_chat.go` (chat REPL watcher) and `cmd/agent/main.go` (bare-prompt
  path) — both set `ag.CloseSignal = signalrec.SignalName(sig)` **before**
  `ag.Interrupt()`. The spelling is `signalrec`'s; `internal/agent` never
  re-spells a signal name (Rule 14).

**Precedence, which is not arbitrary.** A panic still wins (a crash is a crash),
and a classified HTTP/vendor failure still wins: a signal often arrives *because*
the run was already failing, and reporting "signal" there would hide the cause
worth fixing. The arm is therefore gated on `CategoryUnknown && runErr == nil`.

### 2. Chat never performed the record-filed handshake

`signalrec.modesThatOwnTheirSignalHandlers` lists `chat: true` — a streaming reply
is in-flight work, so the recorder must not re-raise. The consequence is that the
provenance record is written on the **recorder's** goroutine while the REPL
unwinds, and `main` then calls `os.Exit`, which does not wait. Every other
claiming door pays `settleProvenanceRecord` on the way out — `cli_term.go`,
`cli_term_once.go`, `cli_memory_sync_every.go`, `merge_run.go`, `main.go`'s bare
prompt. **Chat did not**, so the record was routinely outrun and the session
directory kept no trace of the signal at all.

**Fix.** `finishChat` (`cmd/agent/cli_chat.go`) calls `settleProvenanceRecord`
with the new `chatProgLabel` ("gopi chat"), right after `CloseChatRow`. It is
silent and ~5 ms on an ordinary exit (no signal reached the recorder → nothing to
wait for) and it never changes the exit code: the process is already leaving, and
the handshake's purpose is that a *loss* gets said out loud (Rule 15).

## Why this is not "make the message prettier"

The defect was never a missing word. It was a **wrong** one: `unknown` made an
orderly terminal teardown indistinguishable from a provider failure, and the
artifact that would have disambiguated them was not written. After the fix the
same reproduction reads:

```
"category": "signal",
"error_body": "the session was ended by SIGHUP, a signal this process handled and unwound through its normal close-out — the run did not fail: nothing was in flight and no provider error was recorded (a terminal teardown or a supervisor's stop reaches a chat this way)"
strictly: signal-provenance.jsonl: {"signal":"SIGHUP","role":"chat","sourced":false,"layout":"session"}
```

## The attached claim, checked rather than assumed

The brief carried a hypothesis: "double run / the worktree belongs to another
repo", pointing at the line

```
own worktree release refused: master ... linked worktree of ANOTHER repo, not /home/user/work/gopi
```

That line is real (`internal/tools/task_reclaim.go:746`) and it belongs to a
**worker** row's cleanup — the provenance trail for the same second shows a
`gopi worker` receiving an unsourced SIGINT at 17:16:45 and its child SIGTERM at
17:16:47. It is a *different process* from the chat, and it explains a refused
worktree reclaim, not a chat exit. **It is not the chat's cause.** The chat's cause
is the two defects above, both of which reproduce without any worker involved.

What the same minute *does* corroborate: the terminal itself went away — the systemd
user journal records `vte-spawn-27aa6f15-….scope: Consumed 1min 41.542s CPU time`
at 17:16:12, a scope that is gone afterwards. A terminal teardown delivers SIGHUP;
that is exactly the signal the run handled, printed, and then failed to record.

## Files

- `internal/deepseek/lasterr.go` — `CategorySignal`
- `internal/agent/loop.go` — `Agent.CloseSignal`
- `internal/agent/crashdump.go` — the signal arm + `closeSignalSentence`
- `cmd/agent/cli_chat.go` — declare the signal; `settleProvenanceRecord` in
  `finishChat`; `chatProgLabel`
- `cmd/agent/main.go` — declare the signal on the bare-prompt path
- `internal/agent/chat_close_signal_category_test.go` — unit red proof (3 arms)
- `testsuite/chat_pty_hangup_traced_test.go` — end-to-end red proof on a real pty

## Verification

Both red proofs were taken by mutating the production file and running the test,
then restoring and confirming the restore against `HEAD` (an index-relative diff
would prove nothing):

- unit: `category = "unknown", want "signal"` with the arm disabled.
  Restore check: `test "$(git hash-object internal/agent/crashdump.go)" =
  "$(git rev-parse HEAD:internal/agent/crashdump.go)"` → differs from HEAD, so the
  fix is present rather than reverted.
- end-to-end: `NO signal-provenance.jsonl … a chat session this process HANDLED a
  hangup for left no trace of it` with the declaration and the handshake disabled.
  Same restore check on `cmd/agent/cli_chat.go`, plus `grep -c` confirming both
  halves are back and no mutation marker remains.

## Not done (honest)

- `gopi cli` (`cli_term.go`) claims its own handlers and settles the record, but it
  does **not** declare `CloseSignal` — its close-out does not collect a dump the way
  chat's does, so the `category` half does not apply to it today. If it ever gains a
  dump, it needs the same declaration.
- The bare-prompt path (`main.go`) got the declaration but no end-to-end test of its
  own: the signal-to-dump leg is pinned where it was measured (chat).
