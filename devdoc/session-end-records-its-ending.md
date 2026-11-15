<!-- doc-class: normative -->
# session_end carries HOW a run ended — not only THAT it ended

Task `01a163f8-5ffa-71fb-be2b-f8507c4adefc`. Baseline `5758f23`.

## The report, and what it actually was

`gopi chat` (session `75d90c11`) vanished after a normal `end_turn`: no signal
or panic in `console.log`, `session.jsonl` ending on a `memory_sync_on_store`
event with no `session_end`. The epic's framing — "no matter which exit path,
there must be a distinguishable cause of death" — was right, but it hinted at
a cause I could not reproduce as stated.

I measured first. Built the worktree's binary and drove the REAL `gopi chat`
through every leg with a parent-side probe (`evidence/probe.py`, raw output in
`evidence/probe-baseline.txt`), each run on a fresh profile with stdin/stdout/
stderr on pipes and its own session:

| leg | action | returncode | session_end |
|---|---|---|---|
| quit | wrote `/quit\n` | 0 | `{"exit_code":0,…}` |
| eof | closed stdin | 0 | `{"exit_code":0,…}` |
| sighup | `killpg SIGHUP` | 1 | exit 1 + `error.category=signal` |
| sigterm | `killpg SIGTERM` | 1 | exit 1 + `error.category=signal` |

Two findings, and the second is the defect:

1. **Every leg that reaches close-out DOES write `session_end`.** Memory #14109's
   claim ("the EOF path and the SIGTERM path both write session_end") is
   REPRODUCED. The suspicion that a path silently omits the record is not what
   this tree does.
2. **`/quit` and a closed stdin are byte-identical**, and the signal legs are
   distinguishable only by reading PROSE inside `error.error_body` or by a
   SECOND event (`signal_verdict`) — `session_end` itself carried no
   machine-readable cause at all.

So the gap is not "one record for three causes" but "no record of the cause".
An operator opening the log afterwards could answer *that* the session ended and
never *why* — which is the epic's own worst outcome, arriving through the
opposite door.

## What changed

`session_end` gains two keys, defined once in `internal/sessionevent` (Rule 14:
the writer is `internal/agent`, the readers are `internal/gopilog` /
`internal/testutil`, so the value needs a leaf package):

- `reason` — ALWAYS written, from a closed vocabulary
  (`sessionevent.SessionEndReason*`). A caller that does not know passes the
  zero `EndCause` and the event records `unknown` — deliberately visible,
  because "ended and cannot say why" should never be silently dressed as a
  known leg.
- `signal` — written ONLY beside `reason=signalled` (the recorder's own spelling,
  `signalrec.SignalName`). One axis, so a record cannot claim `reason=quit`
  beside `signal=SIGHUP`.
- `eof_kind` — written ONLY beside `reason=eof`: `key` (Ctrl+D, a human asked to
  stop) or `source` (the input source ended under the run — a closed stdin, a
  pipe whose writer exited, a terminal torn down). These are different facts
  about the run, and the original report of this bug could not tell them apart.

**A signal WINS over the caller's word** (`agent.agentEndCause`). The carrier's
watcher sets `CloseSignal` on the goroutine that decoded the signal — the only
moment that fact exists — so its presence is more specific than whatever reason
the loop happened to be returning with. Without that precedence a SIGTERM could
be filed as `quit`.

The `eof_kind` needed the input layer to say which end it saw, which it had
collapsed into one `ok=false`: `lineedit.Result` gains `EOFKind`
(`EOFKindKey` / `EOFKindSource`), the cooked reader reports
`chatEndKindCookedEnd` (it has no key stream, so it says "unknown" rather than
borrowing Ctrl+D's word), and `cmd/agent.chatEndCause` is the ONE translation
from that vocabulary to the record's.

Every close-out call site now states its leg: `/quit` and `/exit` → `quit`,
end of input → `eof` (+kind), Ctrl+C at the prompt → `interrupt`, a read error
→ `input_error`, a signal → `signalled` (+name), the panic recover → `panic`,
the replay driver failing → `replay_aborted`, and the faces that observe
neither input event nor signal (one-shot, web, `--once`) → `complete` /
`run_failed` by exit code. Reusing a chat leg's word there would have been a
lie.

## Why the signature changed instead of adding an optional field

`LogSessionEnd` and `CloseChatSession` take the cause as a REQUIRED parameter.
An optional one (a struct field on the agent, a variadic) would have let every
existing caller keep compiling while silently not stating anything — which is
precisely the defect: the close-out HAD a reason list it could write and no
caller supplied one. Making it an argument forced each call site to be visited
and to say what it observed.

## Verification (what was actually run)

Real binary, four legs, fresh profile per run (`evidence/probe-after.txt`):

```
quit    {"elapsed_sec":7.997,"exit_code":0,"reason":"quit",…}
eof     {"elapsed_sec":7.999,"eof_kind":"unknown","exit_code":0,"reason":"eof",…}
sighup  {"reason":"signalled","signal":"SIGHUP","exit_code":1,…}
sigterm {"reason":"signalled","signal":"SIGTERM","exit_code":1,…}
```

The four are now distinguishable from `session_end` alone. The signal legs also
keep their `error.category=signal` block and their `signal_verdict` event, so
nothing that already read those was taken away.

## Honest gaps

- **The cooked path cannot tell Ctrl+D from a closed pipe**, and says
  `eof_kind=unknown` rather than guessing. Under `--no-editor`, in tests, and on
  a piped stdin there is no key stream to ask. On an editor-backed terminal the
  distinction IS made.
- **SIGKILL still leaves nothing**, by definition — it cannot be caught. This
  change does not close that and does not claim to. `75d90c11`'s own death is
  still unexplained: its record predates this field, and none of the legs above
  reproduces it. What the change buys is that the NEXT disappearance of that
  shape is answerable from the record instead of by inference.
- **No reader consumes `reason` yet.** `gopilog` and `rundir` parse
  `session_end` for its exit code and counters; they tolerate the new keys
  (plain JSON object, no `message` key) but do not surface them. Naming the
  cause in `gopi session log` / `task` views is a follow-up, not part of this
  fix.
