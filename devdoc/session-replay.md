<!-- doc-class: normative -->
# `gopi session replay` — the session group's playback arm

**What this document is.** The record of porting HP's `gopi session replay` into
peerbox's tree, and the standing claims that shape ships with (each backed by a
guard named at the end). It is the document that stops the NEXT agent from
mistaking this arm for a duplicate of the film route and deleting it.

## What it is, and which of the two replay routes it belongs to

There are two things called "replay", and they answer different questions. Mixing
them up is the single easiest way to delete this feature by accident:

| | `gopi chat --replay <id> [--film DIR]` | **`gopi session replay <id>` (this)** |
|---|---|---|
| does | **RE-RUNS** a recorded conversation against a stub, in a real terminal; can film a GIF | **PLAYS the recording back** as the screen it was |
| answers | "what happens when the model runs again?" — the recording is the **INPUT** | "what was on the screen?" — the recording is the **MATERIAL** |
| redaction | **needs it** (`internal/replay/redact.go`; a film is a shareable artifact) | **none, by the operator's ruling** |
| home | a flag on `cli_chat.go` | an arm of the `session` group's switch |

The operator's ruling, verbatim: 「完全不需要脱敏，这个是用户自己看自己的 log，只不过是比
log/raw 的格式好看」「不是真的 replay 制作电影，只是重放 session」. So `session replay`
sits with `session log` / `raw` / `stats` — one more reader of ONE session id,
whose output is for the person who made it. **No redaction step exists on this
path, and none is to be added**: this is not a film, and a length-preserving
redaction (see `#7808`) would be work done for nobody.

The top-level `gopi replay` verb stays **abolished**
(`devdoc/replay-abolished-verb-single-route.md`): the word is a sub-verb of an
already-censused group, so the verb census and `PositionalVerbs` are untouched.

## What was ported, what was rewritten, and why

The source is HP's commit `456c459b4` (`hostb-Laptop`, branch `master`) — reachable
from peerbox as the remote ref **`hp/master`**, so the port took the objects from the
local store rather than over ssh:

```bash
git checkout hp/master -- internal/replayplay cmd/agent/cli_session_replay.go \
    cmd/agent/cli_session_replay_test.go cmd/agent/session_replay_abolition_test.go
```

**Ported essentially verbatim** (2,549 lines + its tests + the 466-line CLI arm +
the 237-line abolition gate): `internal/replayplay/{material,render,loop,help,run}.go`.
The design is HP's and it is good: a cell-exact grid, per-cell truecolour copied
verbatim, scrollback replayed a screenful at a time, transport keys, the terminal
restored on every exit path, and material that is REFUSED loudly rather than
invented when a session has neither artifact.

**Rewritten, because HP's version does not hold on peerbox's tree:**

1. **The dispatch re-home.** HP already moved the word off the top-level verb, so
   the mechanics transferred: `sessionSubcommandNames` gained
   `sessionSubcmdReplay`, `runSessionGroup` gained a `case` arm calling
   `runSessionReplay(rest)` directly (not through the argv-substitution arm the
   four aliases use — there is no top-level spelling to substitute back), and the
   family page gained its synopsis line. `internal/cliverbs.Replay` is **not**
   re-added (Rule 14: one dispatch consumer needs no leaf constant package).
2. **Session resolution — the one real functional gap.** HP's `sessionReplayDir`
   looked for the artifacts in exactly one place: `<GOPI_HOME>/.gopi/sessions/<id>/`,
   with the timeline at `session.jsonl`. peerbox's session tree has a **second
   layout, and it is the majority of it**: a task run's log is
   `<sessions>/<task-id>/<session-id>.jsonl` — the directory named by the TASK, the
   file by the SESSION (measured: 3,931 bare `session.jsonl` files, and every task
   run grew a `<uuid>.jsonl` beside one). Ported as-is, the verb refused **every
   worker session** with "carries no user/assistant turns with text" or "not
   present" — i.e. it failed exactly the case the operator's criterion names
   (「理论上任何 worker 的 session 都可以」), as this run measured before the fix.
   The fix: the arm resolves the log through **internal/gopilog's `FindSession`** —
   the tree's ONE resolver, the same call `gopi session log <id>` makes — and
   `replayplay.LoadSessionAt(dumpDir, jsonlPath)` takes the two paths separately
   (`LoadSession` is now the one-argument wrapper over it). One resolver, so the
   two readers cannot disagree about which file a session id names.
3. **The timeline reader's row shape — the port's own defect, found by running
   it.** `messageText` assumed `message` was a bare string or a bare block list
   (the Anthropic WIRE shape). The writer emits an **envelope**:

   ```
   {"role":"assistant","content":[{"type":"text","text":"…"},{"type":"tool_use",…}]}
   ```

   (measured over this run's own log: 103 assistant rows with a block list, 5 user
   rows with a plain string, every one an envelope carrying `role`+`content`). The
   first real invocation therefore produced **zero frames** while every ported unit
   test was green — because the ported fixture was written to the reader's
   assumption. `messageText` now unwraps `content` first and still accepts the bare
   shapes, and `internal/replayplay/jsonl_writer_shape_test.go` is the regression
   guard that drives the writer's real shape.
4. **The usage page.** Rewritten to this tree's address
   (`sessionReplaySynopsis` composes `CliVerbSession` + `sessionSubcmdReplay`) and
   to the family's own shape. It keeps HP's key map spliced from the package that
   owns the bindings and the terminal rows spliced from `internal/shot`'s registry,
   so the page cannot advertise a binding or an emulator the code disagrees with.

**Not changed:** `gopi chat --replay` / `--film` (the film route) — untouched.

## The `internal/gopilog` API question (the briefing's flagged risk)

The risk was that the port would need HP's `internal/gopilog` (HP's tree is 284
commits behind peerbox's on that package: `git diff --stat master hp/master --
internal/gopilog` = 23 files, +1,102/−73) — a port with an API mismatch nobody
could see until the build ran.

**Measured answer: the risk did not exist.** `internal/replayplay` imports
**nothing** from `internal/gopilog` — its non-test imports are
`internal/lineedit`, `internal/sgr`, `internal/uiclient`, `mattn/go-runewidth`
and the stdlib. The two packages HP shipped alongside the port
(`internal/gopilog/screen.go`, `cmd/agent/cli_log_screen.go`) are a **different**
feature (the light log renderer) and are **not** part of this port. The single
place this tree needed gopilog is the CLI arm, and there it calls only two
long-standing exported functions (`ResolveSessionDir`, `FindSession`) that exist in
both trees. So no API diff had to be reconciled: the port compiles against
peerbox's gopilog as it stands.

## Family page and the gate it must satisfy

`gopi session -h` carries this line (verbatim from `sessionUsage`, printed by the
built binary):

```
  gopi session replay <id> [--terminal N] → PLAY the recording back as the screen it
                                            was (cell grid + its truecolour), with
                                            transport controls; --frames/--frame are
                                            the static views. NOT the chat verb's
                                            --replay flag, which RE-RUNS the session
```

`TestSessionFamilyUsageAdvertisesEverySubVerb` derives the family page from
`sessionSubcommandNames`, so adding the word without the line fails the build —
that is the gate `devdoc/session-verb-family.md` said would demand the line, and
it does. The family page's stated compatibility holds: `gopi session list` prints
full session uuids, and those ids feed `replay` unchanged.

## Not ported, and why (honest)

| left behind | why |
|---|---|
| `internal/gopilog/screen.go` (622) + `cmd/agent/cli_log_screen.go` (183) | a different feature (a light log renderer), not the playback; HP's copy of gopilog cannot be lifted onto this tree's 23-file-newer version |
| the other ~99 HP commits (`delivery-point sync` ×4, `fix(demo)` ×5, `fix(docindex)` ×3, `chore(standards)` baseline recount, `docs(lineedit)` ×12 …) | catch-up noise: this tree already has the correct versions |
| `internal/cliverbs.Replay` (the leaf verb constant) | Rule 14 — a word with one dispatch consumer does not need a leaf constant to drift from; the printed name is `replayplay.PlaybackCommand`, owned by the package that prints it |
| redaction | operator's ruling (above) |
| `-` (nothing else) | — |

## A flake the port brought with it, and the one-write fix

`TestSessionReplayKeyboard_DrivesTheTransportOverPty` failed **once, then passed
with no source change** — a flake, which is worse than a red. The failure was
`the left arrow did not step back`, and the mechanism was in the test's own
driver: `send` wrote a key sequence **byte by byte**, one `write(2)` per byte, so
a slow schedule could interleave `ESC`, `[`, `D` and the player could resolve a
lone ESC as a key of its own instead of the arrow. A terminal emulator sends a
multi-byte key as ONE unit; the driver now does too (one write per sequence, with
the EAGAIN retry kept). Measured after the fix: **4 consecutive runs, 4 PASS**.

A second red visible in the same suite is NOT this branch's:
`TestCLIContractInventoryMatchesDispatch` (`main.go has 20 … contract suite
tracks 18`) is red on master, has its own in-progress task
(`#01a10f84`), and this branch changes neither `cmd/agent/main.go` nor that predicate list.

## Verified shape (guards this ships with)

| guard | file | fails on |
|---|---|---|
| `TestSessionFamilyUsageAdvertisesEverySubVerb` | `cmd/agent/cli_session_test.go` | a sub-verb in the vocabulary with no line on the family page (this change's own hard requirement) |
| `TestReplayVerbStaysAbolished` | `cmd/agent/session_replay_abolition_test.go` | the top-level word coming back: guard vocabulary, census, the BUILT binary (`gopi replay -h` must print the top-level help) and the source tree (`CliVerbReplay` nowhere), each with a non-vacuity floor |
| `TestSessionReplayIsTheDoorTheValueCameBackThrough` | same | the positive half — `gopi session replay -h` IS the player's page, so "came back through the right door" is distinguishable from "was dropped" |
| `TestSessionReplay*` (static, pty transport, refusals) | `cmd/agent/cli_session_replay_test.go` | the material, the transport, the restore path and the refusals, driven over a real pty |
| `TestLoadJSONLReadsTheWritersMessageEnvelope` | `internal/replayplay/jsonl_writer_shape_test.go` | the envelope regression above |
| `cliContractInventory` row `session replay` | `cmd/agent/cli_contracts_test.go` | the built binary's `-h` page drifting from `sessionReplayUsageHead` |

## The key map: one key, one responsibility (2026-10-10)

**What was wrong.** `↑` and `↓` were bound as ALIASES of the step pair
(`finalUp, finalRight → KeyStepForward`, `finalDown, finalLeft → KeyStepBack`),
justified in a comment as "down the timeline, so Up is forward". The cost was
structural, not cosmetic: a round is ONE step and a round can be taller than the
screen, so the step keys SKIPPED the rest of a long round — the exact content the
in-round scroll exists to reach — and the arrows were two spellings of one
decision instead of two keys.

**The split now in `loop.go`'s `parseEscape`.** One `case` per arrow, one job per
arrow:

| key | what it decodes to | what it moves |
|---|---|---|
| `↑` / `↓` (CSI `ESC [ A/B` and SS3 `ESC O A/B`) | `KeyScrollUp` / `KeyScrollDown` | the VIEW, one ROW |
| `PgUp` / `PgDn` (tilde form) | `KeyPageUp` / `KeyPageDown` | the VIEW, one PAGE |
| `←` / `→` | `KeyStepBack` / `KeyStepForward` | the ROUND (unchanged) |

`KeyPageUp`/`KeyPageDown` are NEW keys, and that is the honest shape of a
requirement that says "one key one responsibility": the arrows and the page keys
move the same thing (the `Scroll` offset, hence the shared field and the shared
`scroll()` helper, which is where the two clamped ends keep their notices) but by
different AMOUNTS, and an amount is a decision the key carries. A single key
whose effect depended on which sequence produced it is what the change removes.
`scrollRowDelta` names the arrows' one-row amount (Rule 1).

**The texts that had to move with it** (a key map is one value read by several
surfaces — `replayplay.KeyMapRows` reaches the `?` overlay, the verb's `-h` page
and both rendering tests by construction):

- `help.go` `KeyMapRows`: `↑ / ↓` — "scroll a row up / down inside the round";
  `PgUp / PgDn` — "scroll a page up / down inside the round"; the two step rows
  now say "from the top", because a step resets the in-round scroll.
- `help.go` `HelpLines`: the long-round sentence names both granularities and
  states the rule (`←/→` BETWEEN rounds, `↑/↓` WITHIN one).
- `render.go` `statusKeys` (the status line's hint) gained `[↑/↓]`.
- `cli_session_replay.go`'s usage page: the long-round paragraph now says the
  arrows read a row at a time and PgUp/PgDn a page at a time.
- The `KeyScrollUp/Down` declaration comment, `finalUp/Down` vs `finalRight/Left`,
  and the `Scroll` field's comment all state which pair is which.

**Observation channel, kept explicit.** `KeyScrollUp`/`KeyScrollDown` had no
notice-clearing arm and this change does not add one: a flow of arrow presses
inside a round leaves `Notice` exactly as it was. The one visible consequence is
that a notice raised by a CLAMPED edge (the `↑` at the top that says "(top of
this round)") survives a later `↑` that is clamped again — the only reading that
would show it to a viewer. `Notice` on an unclamped scroll is the empty string
either way, so "does this scroll clobber the state?" has one honest answer for
each of the two cases, and it is written down here rather than left to a reader.

**Verification of the change, in the tree and out of it** — the out-of-tree half
is what a unit test cannot see:

- `internal/replayplay/loop_test.go`:
  `TestPlayer_TheArrowsAndThePageKeysMoveTheViewInsideTheRound` (the round number
  does not move, `Scroll` moves by ONE row per arrow and by a PAGE per page key,
  and a following `→` really does change the round); the old contract assertion
  in `TestParseKey_EveryBindingIsTheByteTheHelpPageNames` is inverted, and its
  SS3 rows now cover all four arrows (`ESC O A/B/C/D`) — SS3 is the form a
  terminal in application-cursor mode sends, so a replay that decoded only CSI
  would silently ignore the arrows in half the terminals on this host.
- `cmd/agent/cli_session_replay_test.go` (real pty, real keys): `↓` makes the
  status line's scroll marker appear while the position stays `1/`, `↑` takes it
  away, `→` moves the position.
- **Driven by hand on this host** (the evidence the acceptance asks for, session
  `01a12652-311b-7461-81fa-6abecfcff03c`): a synthetic session whose round 1
  carries 60 numbered lines, replayed in a real 64x12 `xterm` with `xdotool`
  typing the keys and `import` taking the frames. `round 1/2` at the top →
  twelve `Down` presses → `↓4`; ninety `Down` presses → `↓51 … (bottom of this
  round)` with `PROBE-LINE-50 … 60` on screen (rows 1-9 had shown PROBE-LINE-01 …
  09); one `PgUp` → `↓40` with `PROBE-LINE-39 … 49`. Two lessons from that run,
  both recorded because they cost time here: `xdotool key --window` sends a
  synthetic event xterm largely IGNORES (86 of 90 presses were lost — use the
  XTEST path, i.e. no `--window`, with the window focused), and the player's
  burst rule makes `ESC [ B`×30 in ONE write ONE key press, so a harness must
  press one key per write.

## Measurements (this port's own run)

```
$ gopi session replay 0135e96e-7b46-4d6d-89a5-c112cd0ff9f3 --frames
session 0135e96e-…  source=chat-screen.dump.json  geometry=103x40 (dump)  frames=2
   1  scrollback scrollback 1/1     rows=23  first=""
   2  screen     screen             rows=40  first="╭── gopi v0.1.0 ───…"
$ gopi session replay 14b20cca-5956-412e-b5c2-7f015dde9cc9 --frames
session 14b20cca-…  source=session.jsonl  geometry=80x24 (fallback)  frames=20
   1  event      turn 1/11          rows=24  first="user: [EPIC GOAL]"
   …
$ gopi session replay <id>            # over a real pty
gopi session replay 0135e96e  screen 2/2  PAUSED  1x  (end of recording — space to replay, p to step back)
```

The first is a chat session (dump material, the recording's own cell grid); the
second is a task-run session (timeline material, resolved through the
`<task-id>/<uuid>.jsonl` layout this port had to learn). Both are the operator's
criterion exercised for real.

A figure here is re-derivable with the command beside it (Rule 38); the counts of
the two layouts are narrative measurements of ONE tree at ONE time, not census
figures.
