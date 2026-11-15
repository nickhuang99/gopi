<!-- doc-class: normative -->
# A web session's idle wait point: the session inbox becomes a turn with no POST

**The gap, measured.** `gopi notify --session <id> --message <text>` appends one
line to a session's inbox from ANOTHER process. Nothing in the receiving process
observes that write — no signal, no pipe, no in-process edge. So the only thing
that could turn a delivery into a model call was a READER: `gopi chat` has one
(it parks on `select` over stdin and, since epic 01a16dab, over
`agent.SessionInboxIdleWake()`), and `gopi web` had none.

Before this change, in this tree:

```bash
$ grep -c "chatInboxTurnText" cmd/agent/cli_chat.go
2
$ grep -c "chatInboxTurnText" cmd/agent/cli_web.go
0
$ grep -c "SessionInboxIdle" cmd/agent/cli_web.go
0
```

A web server's only inbox read was `loadSessionInbox` at a ROUND BOUNDARY, and a
boundary is reachable only from inside a turn — and a web turn only ever started
when the page POSTed one. A page that POSTs nothing sees nothing, so a delivery
sat in the file for as long as the page stayed open. The note `gopi notify`
itself prints ("an IDLE console wakes for it and opens the next turn by itself")
was true of the console and false of the browser.

## What was added, and what deliberately was not

`cmd/agent/cli_web_notice.go` is the carrier's half. It owns **two** things and
nothing else:

1. **one goroutine per server** (`startInboxNoticeLoop` / `inboxNoticeLoop`) —
   it calls `agent.SessionInboxIdleLoad()` once (so a message delivered while no
   server was running mints a turn at once) and then
   `agent.SessionInboxIdlePoll(stop)` for the cadence, and `select`s on
   `agent.SessionInboxIdleWake()`;
2. **a fan-out hub** for the frames of a turn the SERVER started.

It does **not** parse an inbox, add a queue, or add a take point. The take is
still the single `tools.TakeJobNotices` at the turn's own round boundary, which
injects the notice "BEFORE the request is built"
(`internal/agent/chat_ops.go`). The opening line is `agent.ChatJobNoticeOpenLine`
— the same function the console uses. That is what makes a web notice turn and a
console notice turn the same statement of the same fact, rather than two
implementations free to drift (Rules 13/14).

### Why the frames need a standing connection

A turn the server started has no request to answer. Its SSE frames have nowhere
to go unless something is already listening — that is exactly the shape the
console gets for free from its parked `select`. So:

- `GET /api/events` is a standing `text/event-stream` (registered on the same
  mux, so `webTokenGate` covers it identically);
- the page connects with `EventSource` (`connectEvents()`), which **reconnects by
  itself** — the reason it is this primitive and not a fetch loop;
- the frames are the same four names a POSTed turn already emits, plus
  `notice` (the opening line, so a reader can see WHY the page answered by
  itself), and reuse `writeSSEFrame` unchanged.

An idle connection writes an SSE **comment** (`: keepalive\n\n`) every
`webEventsKeepAliveInterval`. A comment is ignored by `EventSource` and by the
page's parser; it exists so a proxy does not reap a legitimately quiet stream.

### The busy case takes the console's own trade

`runInboxNoticeTurn` takes `s.mu.TryLock()`. If the lock is held, the wake
returns **without touching the agent**: the notice stays in the queue where it
has always waited, the in-flight turn's own boundary injects it, and nothing
about that turn changes. The console documents the identical trade
(`cmd/agent/chat_notice_turn.go`), and here there is a harder reason for it than
politeness — `gopi web` holds ONE persistent agent across every turn, so a
concurrent second turn would race `Ctx.Messages` and the cost counters. `s.mu`
exists to serialize exactly that.

Four cases return without a turn, each deliberate:

| case | why returning is correct |
|---|---|
| shutting down | the session is closing; the finalizer will take `s.mu` |
| the LEVEL is false (`tools.JobNoticePending()`) | a stale token: another boundary drained the queue first, and minting would open a turn for a notice that no longer exists |
| `TryLock` failed | a turn is in flight; the notice waits for ITS boundary |
| the queue emptied between the LEVEL and the peek | same conclusion as the LEVEL case |

The LEVEL is re-checked **under** the lock, so a turn minted by a wake that lost
the race is impossible.

### Shutdown order is part of the mechanism

A notice turn holds `s.mu` for its whole model round-trip. `finalizeChatSession`
takes that same lock, so joining the goroutine AFTER the finalizer would wait for
a peer that may never answer — the deadlock `abandonInFlightTurn` exists to
prevent (measured in epic 01a16dab). Both shutdown paths therefore:

```
shuttingDown = true  ->  abandonInFlightTurn()  ->  stopInboxNoticeLoop()  ->  finalizeChatSession(normalHalt)
```

`stopInboxNoticeLoop` closes the stop channel **once** (`sync.Once`) and
**joins** the goroutine through `done`, so a ticker is never left running and the
suite's goroutine sweep sees nothing parked.

## Verification (Rule 31 — the surface was executed, not just compiled)

`cmd/agent/cli_web_notice_test.go` drives the real surfaces and asserts three
facts that none of the others implies: the delivery is CONSUMED (`kind:delivered`
in the inbox), it reached the MODEL (a mock endpoint's recorded request body
carries the delivered text), and a live `/api/events` subscriber receives the
`notice` frame. Plus the hub contract (fan-out, release idempotence) and the
page/server constant pair (Rule 1b).

**Red proof.** Removing only the mux registration:

```
--- FAIL: TestWebInboxNoticeTurnConsumesAndStreams (0.00s)
    cli_web_notice_test.go:189: GET /api/events status = 404, want 200
                               (the standing stream is not registered)
```

The restore was verified against a revision, not against my own copy:
`git diff --stat HEAD -- cmd/agent/cli_web.go` → `107 insertions(+)`,
registration present at line 1103.

**A real run**, outside the suite (the whole point is a second PROCESS):

```bash
SB=$(gopi tool sandbox create --label webnotice --task-id <task> | tail -1)
python3 /tmp/stubmodel.py &                      # a stub model endpoint :18098
GOPI_HOME="$SB" ANTHROPIC_BASE_URL=http://127.0.0.1:18098 \
  GOPI_WEB_ADDR=127.0.0.1:18077 gopi web &
curl -sN --max-time 12 http://127.0.0.1:18077/api/events > /tmp/events-capture.txt &
GOPI_HOME="$SB" gopi notify --session 172938de-eaa3-4f42-aff1-06265b11e0fc \
  --message "REAL-RUN-PROBE-7c41: hello from another process"
```

The stream, verbatim:

```
event: notice
data: {"label":"external message 9461d04f713d326d","text":"[system] An external process sent this session a message: REAL-RUN-PROBE-7c41: hello from another process"}

event: delta
data: {"text":"NOTICE-ACK from the stub"}

event: done
data: {"result":"NOTICE-ACK from the stub"}
```

and the two halves that prove it was a real turn: `grep -c REAL-RUN-PROBE-7c41
/tmp/stubmodel-requests.log` → `1`, and the inbox ends with
`{"kind":"delivered","id":"9461d04f713d326d"}`.

## What this does NOT do

- **It does not interrupt a turn.** Same trade as the console (see above).
- **It adds no second poller interval, inbox path or take point** — the cadence
  and the merge are `internal/agent/session_inbox.go`'s, so a future third
  carrier inherits them without restating either (Rules 2/13/14).
- **The page's rendering of a server-driven turn is display-only**: `notice`
  renders as its own row (`.msg.notice`, dashed border), deltas accumulate into
  one assistant bubble, `tool` frames become the existing collapsible cards,
  `done` closes it. No new page state machine.
