<!-- doc-class: normative -->
# A Replay Longer Than the Model's Key Budget Hung Forever — the Replay's Own Injection Budget

Task `01a103375f0175bea1288e243719f7c2`. Status: implemented, verified with a real
end-to-end run. Tier: Rule 7 devdoc (a measured deadlock plus a design decision
that a later reader would otherwise "fix" back).

## The symptom

```
gopi chat --replay <session-id> --film <outdir> --out <outdir>/replay.gif
```

on a recording of **20 turns / 150 beats** never returned. Measured 2026-10-04:
the driver ran **7h06m** and was killed by hand; the frames and the GIF *were*
written (the film driver's own frame timeout ended the film), so the artifact
existed while the process that made it did not stop.

Evidence (this run's own session directory, `evidence/red/`):

- `stdout/stderr`: the driver announced the display, the terminal and the producer,
  then nothing more.
- `film/producer.log` — the filmed terminal's own capture of the inner `gopi chat`:

  ```
  [gopi replay]  injecting "/quit" was refused: this period's key ceiling is
  reached (64 of 64 keys injected) — it does not raise itself, and only a line
  the OPERATOR submits opens a fresh period
  you>
  ```

  …followed by the prompt's repaint loop, forever.
- `ps` at t+150s: the outer driver, the `Xvfb :63` and the `xterm` all alive, the
  driver's main goroutine parked in `futex_wait_queue_me`, and — after the outer
  process was killed — the inner `gopi chat` **re-parented to init (PPID 1)** and
  still printing status rows at its prompt.

## The root cause (5-why)

1. **Why did the process not exit?** The inner `gopi chat` never returned, so the
   completion marker (`done.txt`) the film driver waits on was never written.
2. **Why did the inner `gopi chat` not return?** Its REPL was parked in its
   blocking read (`lineCh`) waiting for a line; in a replay the only writer of
   lines is the replay driver, and the driver had already stopped.
3. **Why did the driver stop?** Its last injection — the closing `/quit` — was
   REFUSED, `injectLine` returned that refusal as an error, and `run` set `r.err`
   and returned.
4. **Why was the injection refused?** `injectLine` charged every unit against
   `agent.ChatSurfaceKeyCeiling()` (**64**), the ceiling built for the **model's
   self-driven loop** (`chat_surface` action=key). The recording's script needs
   20 turns' chunks + 20 Returns + the quit line = **65 units**, one over.
5. **Why did the refusal not end the run?** Because "the driver stopped" and "the
   session ends" were two different events with nothing between them. `run`'s
   `defer close(r.done)` said *the driver is finished*; the REPL's loop read only
   `lineCh` and `interruptCh`, and every exit path in the process went through
   that loop. The driver's `done` channel was joined by `finishChat` — which is
   reached **from** the loop, i.e. only after the loop has already decided to
   leave. Nobody was left to type, and nobody was told.

The norm it violated: **a deterministic replay is not a self-driven loop.**
`chatSurfaceKeyCeiling` exists to bound how many keystrokes *the model* may spend
in one period, precisely so a bounded party cannot renew its own budget
(`internal/lineedit/inject.go`: *"a bound the bounded party can lift is not a
bound"*). A recording's length is a property of a FILE — 64 was never a bound on
it, it was the wrong quantity applied to the wrong party.

## The fix — three parts, each with its own reason

### 1. The replay's own budget channel (`cmd/agent/chat_replay_budget.go`, new)

- `chatReplayInjectPlan(rec)` counts the units the recording's script will spend —
  each turn's `chatReplayChunks` plus one Return, plus the exit line — through the
  **same chunker the injection uses**, so the plan and the typing cannot disagree
  about what one unit is.
- `chatReplayInjectBoundFor(rec)` turns that into a ceiling
  (`units × chatReplayInjectPerUnit + chatReplayInjectSlack`, floored at the
  loop's declared ceiling — this channel may only move the number **up**, and only
  for a script that needs it).
- `recordChatReplayInjectBound(rec)` **records** it, because two readers must
  agree: the injector is *constructed* with the ceiling and the driver *charges*
  its injections against it.

  **NOT a `sync.Once`, and the difference was measured.** The first cut used one,
  and it reproduced the very defect it was written to remove: a `sync.Once` is
  taken by whichever reader asks FIRST, so any injector built — or any caller that
  merely *read* the ceiling — before `startReplay` recorded the recording froze
  `agent.ChatSurfaceKeyCeiling()` (64) for the whole process and the replay's
  budget was silently never applied. A new guard case caught it (it read the
  ceiling, then got 64, then reproduced the refusal on unit 65).
  The record is now: **zero means unset** (an unrecorded ceiling answers with the
  loop's declared one and stores nothing), and recording only ever **raises** it.
  Direction matters — the floor is the loop's ceiling, so the channel cannot
  shrink the model's bound — and a reader that stored what it resolved would have
  turned "unset" into "frozen".
- `chatReplayInjectCeiling()` is the ONE reader. Its unlatched default **is**
  `agent.ChatSurfaceKeyCeiling()`, so a run with no `--replay` behaves exactly as
  before — which is why the self-driven loop's bound and its tests are untouched.
- `applyChatReplayInjectBudget(inj)` opens the replay's own key period
  (`Injector.ResetKeys`) at that ceiling, from `run`, before the first turn.

**Why not raise the 64** (the rejected alternative): that number protects the
model. Raising it would have traded this deadlock for a larger model keystroke
budget toward the same unbounded end. Two quantities, two definitions.

### 2. Stopping is not ending — the abort channel (`chat_replay.go`, `cli_chat.go`)

- `chatReplay` gains `abort chan struct{}` + `abortOnce`, closed in ONE deferred
  decision when — and only when — `r.err != nil`. The success path (a driver that
  typed `/quit`) must **not** signal: the REPL is already leaving through its own
  closeout.
- The REPL's loop carries `replayAbort` (nil for every non-replay run and for a
  successful replay) into both blocking waits of its read select, and the arm
  calls `replayAbortExit`, which prints `chatReplayAbortNotice` and leaves through
  the SAME `leaveChat` closeout as `/quit`/Ctrl+C — with `chatExitFailed`.

**Why a channel and not a shared flag**: the loop is parked inside `select`; only
a channel can wake it.

### 3. The sentence that claimed a close it did not perform (`joinChatReplay`)

`joinChatReplay`'s timeout arm said the session *"is being closed anyway"*. That
was not true of anything this function can do — it is reached FROM the closeout,
so the REPL has already decided to leave, and the driver it gave up on still holds
the editor, the injector and the session's goroutines. The sentence now says what
is true (*"this closeout is not waiting for it any longer"*), and the doc comment
names the observable consequence (a process that does not exit — the 7h06m run).

**Not done, stated honestly**: the abort channel covers the REPL's **read** waits.
A driver that dies *while a turn is in flight* is not covered by it, because the
REPL is then inside the turn, not in the read select — Ctrl+C remains the
operator's way out, exactly as the chat surface's own contract says. The measured
defect was in the read wait, and the abort is exactly as wide as that defect.

## Regression guards

`cmd/agent/chat_replay_budget_test.go`:

| case | what it pins |
|---|---|
| `TestChatReplayBudgetCoversTheWholeScript` | the plan counts the exit line; the derived budget exceeds the loop's ceiling for an over-long script; the latch agrees with the derivation |
| `TestChatReplayBudgetIsReadBeforeTheInjectorIsBuilt` | the number the injector is BUILT with (one reader) is the number derived from the recording |
| `TestChatReplayScriptFitsItsBudget` | the minimal sample run forward: every unit of the script — the exit line last — is accepted by a REAL injector (production input layer on an allocated pty) under the derived budget |
| `TestChatReplayRefusalStillPossibleUnderTheDerivedBudget` | the budget is a NUMBER, not an exemption: the key past a spent period is still refused with its sentence |

`cmd/agent/chat_inject_pty_test.go`'s "a caller's larger bound cannot raise it"
operand now derives from `chatReplayInjectCeiling()` instead of
`agent.ChatSurfaceKeyCeiling()` — for every run that is not a replay the two are
the same number, and the case is about the RATIO (asking for more than your own
ceiling gets nothing), which holds for either.

## Files

- `cmd/agent/chat_replay_budget.go` (new) — the budget channel.
- `cmd/agent/chat_replay_budget_test.go` (new) — the guards.
- `cmd/agent/chat_replay.go` — latch at `startReplay`, the abort channel, the
  injector reset, both injection call sites read `r.injectBound`, the honest
  join sentence.
- `cmd/agent/cli_chat.go` — the `replayAbort` arm in both blocking waits,
  `replayAbortExit`.
- `cmd/agent/lineedit_wire.go` — the three `NewInjector` construction sites read
  the ONE reader.

## Verification recipe (reusable)

```bash
# the recorded session that reproduces it (20 turns, 65 injection units):
python3 - <<'EOF'
import json
units=0
for line in open('$HOME/.gopi/sessions/<id>/session.jsonl'):
    e=json.loads(line)
    if e.get('type')!='user': continue
    c=(e.get('message') or {}).get('content') or ''
    units += -(-len(c)//16) + 1
print("injection units:", units+1, "(+1 for the exit line)")
EOF
# RED:  the served unit count decides it — >64 units means the pre-fix binary hangs
bin/gopi chat --replay <id> --film <outdir> --out <outdir>/replay.gif
# GREEN: the same command returns, and `ps` shows no Xvfb / xterm / inner chat
```

The unit count is the whole trigger: it is a property of the recording, computable
**before** the run, so a recording that will hang the old binary is identifiable
without waiting out a timeout.

## The GREEN re-run (2026-10-04, branch `task/01a10370ce097bcd9cae8f7cd99bb675`)

The fixture recording needs **65** injection units, one past the loop's 64 — the
RED run's `producer.log` is the proof that the OLD binary was the thing that
stopped:

```
injecting "/quit" was refused: this period's key ceiling is reached (64 of 64 keys injected)
— it does not raise itself, and only a line the OPERATOR submits opens a fresh period
```

The same recording, the same command, on a binary built from this branch — run
twice, once on the current default film terminal and once on `xterm` (which keeps
the `-l/-lf` session-log channel, i.e. the shape the RED evidence was captured in):

```
# xfce4-terminal (the default)
$ timeout 240 ./bin/gopi chat --replay 0cd5d1b7-… --film <ev>/green --out <ev>/green/replay.gif
exit=0 elapsed=151s     151 frames, replay.gif 3,702,945 bytes

# xterm — producer.log is available here
$ GOPI_FILM_TERMINAL=xterm timeout 240 ./bin/gopi chat --replay 0cd5d1b7-… \
        --film <ev>/green-xterm --out <ev>/green-xterm/replay.gif
exit=0 elapsed=109s     151 frames, replay.gif 917,375 bytes
```

and the `producer.log` of the xterm run carries the sentence this fix exists to
produce — the run pricing its OWN script instead of charging it against the
model's 64:

```
[gopi replay] injection budget: 66 key unit(s) over 20 recorded turn(s) plus the
exit line, derived from 666 byte(s) of recorded text — this period's key ceiling is 196
```

`66` is the plan (20 turns' chunks + 20 Returns + the `/quit` pair; the plan
counts rune-safe chunks, so it may exceed a byte-length estimate) and `196` is the
derived ceiling — `66 × 2 + 64`, floored at the loop's 64. The old binary ran the
same script against `64`, refused the 65th unit, and never returned.

- **151 frames / 151 beats**, the last beat being the closing `/quit`
  (`beats.tsv` row 151, `text_b64` = `L3F1aXQ=`) — i.e. the unit the old binary
  refused was injected and SERVED, and the surface then left through its own
  `/quit` closeout (`bye.` on the surface; `[gopi] chat table: … ended (normal)`
  in the inner session's log).
- **No orphan**: `ps` snapshots were taken before and after; the run's own process
  tree (the `timeout`, the driver, the private `Xvfb`, the terminal, the inner
  `gopi chat --replay`) is absent from the after-snapshot — the inner session
  re-parented to nothing because it EXITED, which is exactly what the RED run did
  not do (there an inner `gopi chat` had PPID 1 and kept printing status rows
  after its driver was killed).
  *Honest note on the after-snapshot*: the machine is shared, and other agents'
  film runs (main-repo `/home/user/work/GoPi/bin/gopi`, different recordings and
  displays) appear in it. The claim is about MY run's own processes — identified by
  its scratch dir and display — and none of those survive.

Why this run is a check of the FIX and not of the environment: the only term that
changed between the two runs is the binary. The recording, the machine and the
`--film/--out` shape are identical, and the differing number (65 units against a
ceiling of 64) is computable before either run starts — the recipe above prints
`injection units: 65 (+1 for the exit line)` for this recording.

### One shape note, and it is a property of the runner, not of the budget

Under the CURRENT default film terminal (`xfce4-terminal`) the run **announces**
that the filmed command's own log cannot be captured:

```
[gopi film] xfce4-terminal offers no session-log switch, so the filmed command's own
output cannot be captured to …/producer.log — it reaches that terminal's pty only
```

So a green film run on the default terminal produces frames and a GIF but no
`producer.log` to grep for the budget sentence in. The driver's notices for such a
run land in the INNER session's own `console.log`
(`~/.gopi/sessions/<inner-id>/console.log`, the destination
`chat_notice_sink.go` routes them to precisely because they must not become film
pixels) — that is where `[gopi replay] turn N/20` and the run's `done:` line are
read. Filming in `xterm` (`GOPI_FILM_TERMINAL=xterm`) keeps the classic
`-l/-lf` channel and DOES give a `producer.log`, which is the shape the RED run
was captured in.
