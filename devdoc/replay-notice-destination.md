<!-- doc-class: normative -->
# A replay's own diagnostics never land on the screen that draws the recording

`gopi chat --replay <id>` prints `[gopi replay] …` sentences: the recording it
re-runs, the stub the model is bound to, the injection budget, the refusal
catalogue (raw-log open, session mint, slot registration), the closing account.
They are the driver's DIAGNOSTICS — the run's own words about its own work — and
they must be readable afterwards (Rule 15: a run that drops them is a run that
cannot be reviewed).

What they must NOT do is land on the terminal that is drawing the recording. That
screen is a picture of SOMEBODY ELSE'S conversation; a sentence from this run
inside it reads as a line of that conversation.

## The defect (measured 2026-11-04)

The sink existed (`cmd/agent/chat_notice_sink.go`) and had exactly ONE route,
installed inside the FILM branch of the REPL (`beatDriver != nil`). So the
criterion in force was **"this process is a film's producer"**, and a plain

```
gopi chat --replay 004c3578-37f7-4e57-852f-b9c362e5c460 </dev/null >out.txt 2>err.txt
```

was not a film's producer, so every notice kept the default `os.Stderr`:

```
$ grep -c '\[gopi replay\]' err.txt
15
```

`--film` runs were already clean (`grep -c` on their stderr: 0), which is exactly
why the defect survived: the shape that had been measured was the photographed
terminal, and the shape that was broken was the ordinary one.

## The fix, and the criterion it replaces

A film is ONE CONSUMER of a replay. The question the destination actually depends
on is "**is this process re-running a recording**", and `internal/replaymode`
already answers exactly that (`Active()`). So the sink lives beside the answer —
`replaymode.NoticeSinkWriter()` / `SetNoticeSink()` / `Noticef()` — and
`cmd/agent`'s `chatNoticef` is a thin alias over it (Rule 13: one question, one
place).

Two consequences that are the point rather than a side effect:

1. **The writers in `internal/agent` reach it too.** The session-mint refusal
   (`internal/agent/session.go`), the raw-log refusal (`internal/agent/rawlog.go`)
   and the memory-refusal record (`internal/agent/memory_replay_guard.go`) used to
   print straight to `os.Stderr`; they now go through `replaymode.Noticef`, so the
   same sentence goes to the same place no matter which package spoke it. The same
   holds for `cmd/agent`'s slot registration and history-append refusals.
2. **The route is installed before the FIRST notice.** `startReplay` announces
   three of them, so the install sits above that call, not below it.

## Where they GO, and why there are two answers

| the run | destination | why |
|---|---|---|
| a replay WITH a session record | that session's `console.log` | the historic destination: a record beside the session is where a reader already looks |
| a replay with NO session record (a plain `--replay`) | `<scratch>/queue/queue-runtime-ui-session-<pid>/chat-replay-notices*/driver-notices.log` | it mints no session (internal/replaymode refuses the mint by name), so there is nothing to sit beside; this is the chat's OWN scratch class, the same one the replay's screen dump and SGR stream already use |

The second destination adds **no new artifact class and no new dependency**
(Rule 35): `gopi tool scratch-gc` already sweeps `RuntimeClassUISession`
directories, and a replay adding its notices to that stream does not create a life
cycle anything has to own. Nothing is written into the recording — the epic's
read-only contract holds, and a case asserts the path is not under `sessions/`.

A destination that cannot be OPENED is announced and the screen is kept
(Rule 15): `[gopi] chat: this replay's driver notices cannot be recorded (…)`.
That is worse than the fix, and it is STATED — the alternative, a silent fallback,
is indistinguishable from a run that had nothing to report.

## Verification (measured)

```
$ grep -c '\[gopi replay\]' err@pre-fix.txt      # pre-fix binary
15
$ grep -c '\[gopi replay\]' err4.txt             # post-fix, same command, same recording
0
$ grep -c '\[gopi replay\]' out4.txt
0
$ grep -c '\[gopi replay\]' <the route's own file>
15
```

The fifteen lines are MOVED, not deleted: the file holds all of them, including
the two that come from `internal/agent` (`refused to open this run's raw HTTP
log`, `refused to mint this run's session record`) and the one from `cmd/agent`
(`refused to register chat_session_id_*`).

`--film` was ALREADY clean and stays clean — its stderr carries `[gopi film]`
lines (measured: 205) and zero `[gopi replay]` ones, before AND after this change
(before: 0, after: 0):

```
$ grep -c '\[gopi replay\]' ferr.txt    # pre-fix, --film
0
$ grep -c '\[gopi replay\]' ferr3.txt   # post-fix, --film
0
```

What the film case does NOT show is the console.log hand-off: in a run with no
terminal (`</dev/null`), the agent's `SessionLog` was nil and the film's route
answered `""`, so the 13 notices measured in that run are in the scratch file
named above. Where a FILM's notices landed BEFORE this change was not measured
here — they were not on stderr, and the film directory's own
`terminal.log`/`terminal-notices.log` were both empty. Recorded as an open
question, not as a preserved behaviour.

## What was NOT done

- The `--film` producer still writes `[gopi film] …` notices to stderr. That is
  the FILM's channel, not the replay's, and no task asked for it; it is visible in
  the same measurement (`grep -c '\[gopi film\]'` on stderr > 0), and the film
  machinery has its own task surface.
- Whether the pre-fix shape was ALWAYS broken or a REGRESSION is a history
  question this clone cannot answer: the sink file has exactly ONE commit in this
  history (`964b2404`, the source snapshot), and in that commit the only call site
  is already inside `if beatDriver != nil`. Recorded as "always broken as far as
  this history reaches", not as "regressed".
