<!-- doc-class: normative -->
# The screen-byte recording — one switch, three states, two surfaces, and the run that records nothing

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

The recording answers ONE question: **what bytes did this run write to its own
stream?** It used to be a chat-only flag namespace (`GOPI_CHAT_SGR_STREAM`,
`--sgr-stream`). This page records the change that made it a surface-independent
capability, the precedence it obeys, and the rule that keeps an empty run from
manufacturing evidence.

## 1. Where the switch lives, and why it moved

| | before | after |
|---|---|---|
| the variable | `GOPI_CHAT_SGR_STREAM` (chat namespace) | `GOPI_SCREEN_STREAM` (terminal-agnostic), legacy spelling still READ as an alias |
| the parser | `cmd/agent/chat_sgr_stream.go` (chat only) | `internal/config/screenstream.go` — one parser, one precedence function |
| the surfaces | `gopi chat` | `gopi chat` **and** `gopi worker` |

The move is not cosmetic. The variable's old name said which SURFACE may use it,
and that made the capability unreachable for a process with no command line of its
own: a worker run paints no screen, but the record it writes is a stream a reader
may still want verbatim — the same class of evidence as a chat's paint bytes. A
name that outlives its namespace has to be about the THING (`the screen`), not
about the caller.

## 2. Three states, and why two would be wrong

```go
config.ScreenStreamStateUnset  // this channel said nothing
config.ScreenStreamStateOn     // record at the path the surface resolves
config.ScreenStreamStateOff    // DECLINE — no artifact, and say so
config.ScreenStreamStatePath   // an explicit destination
```

`off` and `unset` are different answers and a parser that collapsed them would
break the one thing an operator uses to decline a default-ON switch:
`GOPI_SCREEN_STREAM=0` would read as "nobody said anything" and the surface
default would silently win. `config.ScreenStreamChoice.Armed()` is the ONE
predicate both recorders read, so "off" cannot acquire a second definition.

**The value vocabulary is not re-spelled here**: `config.IsFalseValue` /
`config.IsTrueValue` (→ `internal/boolvocab`) own the spellings, so
`0/false/off/no/disabled` and `1/true/yes/on` mean the same thing at every channel
of this program.

## 3. The precedence, graded — and it is one function

`config.ScreenStreamResolve` (`internal/config/screenstream.go`) is the ONLY
statement of the order, and every surface calls it, so a surface cannot invent a
fourth rung:

```
1. argv   --screen-stream[=PATH] / --no-screen-stream
            wins outright: this run's own words, typed one second ago.
            Both words on one line → the walk applies them in argv order,
            so the LAST one wins, and the run announces the state it ended in.
2. env    GOPI_SCREEN_STREAM   (GOPI_CHAT_SGR_STREAM only while the current name is unset)
            wins over the surface default, and over the alias.
3. default  the SURFACE's own answer:
              ScreenStreamDefaultChat   = armed    (operator ruling 2026-10-31:
                                       the bytes of a paint sequence cannot be
                                       re-created afterwards)
              ScreenStreamDefaultWorker = declined (a round's progress text is
                                       not a screen; recording it is opt-in)
```

A settings key would sit at 3½ (CONFIG.md §1 places a settings key below the
environment). **There is none today**, and the function's comment says where it
would go, so adding one later is one edit at one rung rather than four surfaces
each deciding for themselves.

`Origin` travels with the resolved choice (`argv` / `env` / `default`) and
`Source` names the SPELLING — that is how the chat's startup notice and its closing
report, and the worker's report line, can say WHERE the decision came from instead
of implying it. The pre-rename variable is an ALIAS, never a silent fallback: the
run that is switched by it says so (`chatSgrEnvAliasWarn`,
`workerScreenStreamLegacyNotice`).

## 4. The worker's half: "equivalent output", stated honestly

`gopi worker` arms the same `uiclient.SgrStream`
(`cmd/agent/worker_screen_stream.go`). What goes in it is the round's **own
stream** — the claim line, the workdir line, the close-out, and
`worktreerelease.ReleaseReporting` — through the round's ONE stdout door
(`workerRoundAnnounce`), because that door is where the round's record is composed
and a second door would be a second, differently-shaped record.

**It does NOT record a terminal's answer, a repaint, or escape sequences the
worker never emitted.** A worker that is not on a tty has no terminal to be
"equivalent to"; claiming one would be exactly the dishonesty this epic forbids.
When stdout IS a tty the tee is invisible by construction: the same bytes reach the
operator and the artifact.

The redirect is a process-wide latch, not a parameter threaded through a dozen
round helpers: the round is single-threaded (the same reasoning as
`workerWriteback`), and a caller that forgets a sink cannot silently record
nothing.

## 5. The run that records nothing — the epic's hard rule

An armed round that emitted **zero bytes** would otherwise leave a file with a
header, a trailer, `frames=0`, and no payload. That file is not "an empty
recording"; it is an artifact a reader takes as evidence that a screen existed.
So the close-out:

1. reads `Frames()`/`Bytes()` from the sink;
2. on zero: **removes** the artifact and prints

   ```
   #gopi-worker-screen-stream-report decision=empty path=<p> emitted=0 — 本 run 无屏幕输出: 0 recorded frames, the empty artifact was removed, so no zero-payload stream survives to be mistaken for evidence
   ```

3. on frames > 0: prints `path= bytes= frames= error=` for the real artifact.

The removal is REPORTED with the path it removed, because the `ls` that finds
nothing is the evidence and a quiet `rm` would make that evidence unverifiable. A
removal that FAILS is its own finding, never folded into the empty case: a 0-frame
file that could not be removed is on disk looking exactly like evidence, and
`decision=empty … COULD NOT be removed` says so.

The loop face (an idle or drained queue that ran **no round at all**) opens
nothing — nothing was opened, so there is nothing to remove and nothing to
mistake — and prints the same sentence with `path=(none — no round ran)`.

## 6. The grid a non-tty run declares — measured defect, fixed at the source

The first real worker run recorded a stream nobody could paint. Measured on this
host (sandbox profile, `GOPI_SCREEN_STREAM=1`):

```
worker: screen byte stream → …/01a1b9bc-…/01a1b9bc-….screen.stream (env)
#gopi-worker-screen-stream-report path=… bytes=340 frames=3 error=""

$ gopi session replay 01a1b9bc-… --from stream
gopi session replay: …/01a1b9bc-….screen.stream declares grid=0x0, which cannot
be painted: a screen needs both dimensions        (exit 1)
```

**Root cause, not a parameter.** `terminalGeometry(os.Stdout)` returns `0,0` for a
pipe — and a worker is usually launched by a script, a daemon, or a redirected
shell, so "no terminal" is its NORMAL case. The header therefore carried
`grid=0x0`, and `replayplay.LoadScreenStream` refuses a stream with no dimensions
(`internal/replayplay/stream.go`) — correctly: a zero-size grid is not a picture.
The chat never hit this because a chat without a tty is not a chat.

**The fix.** A non-tty round records at the program's ONE documented default
(`replayplay.FallbackCols`/`FallbackRows`, 80×24 — the same size a replay assumes
when neither artifact states one) and **says so**, because the header's `grid=` is
what a replay reports as a MEASURED size (`replayplay.GeometryFromStream`):

```
worker: screen byte stream grid 80x24 — NOT measured: this process has no
terminal, so the recording declares the program's documented default
(replayplay.FallbackCols/FallbackRows) instead of inventing a size
```

Re-measured after the fix on the same fixture: header `grid=80x24`,
`frames=3 bytes=340`; `gopi session replay <id> --from stream` then **paints** the
recording (`script -qec … ` on a pty, since the player is interactive) — the
artifact is replayable, which is the whole point of recording.

## 7. Verification recipes

```bash
# the parser's three states and the precedence, without touching an environment
go test ./internal/config/ -run 'TestScreenStream' -count=1

# the worker's armed / empty / declined paths, and the live reader on the artifact
go test ./cmd/agent/ -run 'TestWorkerScreenStream' -count=1

# the chat's switch, unchanged in behaviour but now resolved by the shared parser
go test ./cmd/agent/ -run 'TestChatSgr|TestChatResolveSgrPath' -count=1
```

A stream produced by either surface is read back with the SAME reader a replay
uses (`uiclient.ReadSgrFrames`), which is what makes "replayable" a measurement
rather than a claim: `frames` is what a stream holds, `meta.Trailer` is whether the
run closed it (a trailer-less stream is a run that died, not a truncation).

**A note on TTY**: a render-recording run needs a terminal to have bytes at all
(`script -qec "stty cols 100 rows 30; …"`), which is the subject of
`chat-sgr-stream-raw-append-leg.md`. A worker run does not need one — its record is
its own announce lines — which is why the worker's armed path is measurable in a
pipe-free, deterministic fixture.
