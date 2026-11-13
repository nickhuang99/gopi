---
title: GOPI-SESSION
section: 1
header: GoPi Manual
---

# NAME

gopi-session - inspect and continue sessions

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi session list [--kind KIND] [--mode MODE] [--source WHO] [--limit N]
                    [--all] [--json]
      List EVERY session this machine has, newest first, from TWO sources: the
      chat index (conversations) and the sessions tree (the run logs a task run,
      a one-shot prompt or a resume writes — they register no chat row). One row
      per session: its id, kind, state and, for a run, the pid that wrote it.
      The id is a FULL session uuid, and it is what every sibling verb takes.
      --kind takes ONE of the two species, never a list:
        --kind chat     conversations (a chats-table row)
        --kind run      run logs (a task run, a one-shot prompt, a resume)
      What the view did NOT show is printed as numbers on its own lines:
      [filtered] dropped by the filter, [hidden] a chat row whose log is not
      here, [limit] cut by --limit, [scanned] a directory with no run log.
      "turns" is THE turn count (chats.turn_count, every input face equal) —
      NOT the rounds (session log --rounds) nor the loop passes (Passes). It is
      the CHAT row's number: a run row prints "turns none (no chats row)".
      --mode takes ONE of the two values the chats table holds, never a list:
        --mode chat     the terminal chats (gopi chat, gopi ui)
        --mode web      the browser conversations (gopi web, gopi cli)
        --source WHO    who held the console: human, agent, worker, merger …
        --all           the COMPLETE index, rows whose log is NOT on this
                        machine included ([missing] marker); the default view
                        reports what it hid on a [hidden] line.
      No --mode/--source lists everything; --json prints "turns" and the
      person-only subset "human_turns" (JSON KEYS in man gopi-session);
      "repl_turns" is a DEPRECATED alias, announced on every row. Every row
      also carries "turns_provenance", which NAMES the half it belongs to: a
      run row OMITS the turn keys rather than zeroing them (contract in man
      gopi-session).
  gopi session show <id> [--json] [--round R] [--turn N] [--mapping]
      One session's summary, whichever source holds it: mode, source, state,
      model, turns, human turns, total token counts, times, end reason, workdir,
      branch/repo, parent, log path — plus, for a run, its RUNTIME facts: pid,
      whether that process is still alive, the ledger row it executed for, its
      run id and the raw request log. The id is a chat session id, a run session
      id, or a task/run id naming a session directory.
      --round R / --turn N / --mapping (pass ONE) answer the JOIN between the
      two counters instead of printing the page: which TURN owns round R, which
      ROUNDS turn N covers, or every turn's interval. Same boundaries as the
      turns row — one algorithm, so they cannot disagree (man gopi-session).
  gopi session log <id> [view flags]     → the JSONL session log reader
  gopi session stats <id> [flags]        → session numbers: human turns first,
                                            the request count beside it (the
                                            turn count is session show's)
  gopi session raw <id> [round] [part]   → the verbatim raw HTTP log
  gopi session resume <id> [--task T]    → continue from its checkpoint.
                                            ONE holder per run directory, decided
                                            by its run-scope kernel lock — not by
                                            pid liveness. Criterion and the gaps
                                            left: devdoc/resume-gate-is-the-run-lock.md
  gopi session replay <id> [--terminal N] → PLAY the recording back as the screen it
                                            was (cell grid + its truecolour), with
                                            transport controls; --turns lists the
                                            TURNS and --turn N prints one in full.
                                            NOT the chat verb's --replay flag.
  gopi session context <id> [--out PATH] → EXPORT one turn's context to a FILE
                                            (newest, or --turn N); one line on screen.
  gopi session notes <query> [--limit N]  → SEARCH the round-level [IMPORTANT] note
                                            index: what each round recorded, across
                                            every session, with the machine and the
                                            full path it was recorded on
  gopi session gc [--apply] [--older-than DAYS] [--largest N]
                  [--trim-raw-log SIZE [--apply]]
                                         → RECLAIM: report which session directories
                                            are collectable and why. A DRY RUN by
                                            default — nothing is deleted unless you
                                            pass --apply, and a directory any live
                                            process stands in, or any task ledger
                                            row names, is never collectable.
                                            --largest N sorts by BYTES instead of
                                            age (read-only); --trim-raw-log SIZE
                                            bounds each *.raw.log (dry run unless
                                            --apply). Contract: man gopi-session.
```

<!-- END SYNOPSIS -->

# DESCRIPTION

A **session** is one run of this binary — a chat, a web conversation, a one-shot
prompt, a task run. Its JSONL log is the authoritative record, and this verb is
the door to it: the family inspects a finished run, searches what it recorded,
continues it from its checkpoint, plays it back, and reads the raw HTTP traffic
underneath it.

`gopi session log`, `stats`, `raw` and `resume` are the commands. Their old
top-level spellings (`gopi log`, `gopi stats`, …) were RETIRED in the
2026-10-05 restructure: the bare word is refused by name and points here,
rather than being runnable a second way. One implementation, one spelling
that runs, with the flags belonging to that verb.

`gopi session replay` **plays a recording back** as the screen it was — a cell
grid with its truecolour, with transport controls — while the chat verb's own
replay flag (`--replay`) **re-runs** the recorded session on the live surface.
The two are different operations and neither replaces the other.

**The player's keys, and its two ends.** One step is one TURN. `n`/`→` advance a
turn and `p`/`←` go back, both landing on the new turn's TOP; the vertical arrows
scroll the turn already on screen a row at a time and `PgUp`/`PgDn` a page at a
time. `Home` is the top of the first turn. `End` is the material's LAST ROW —
the last turn, scrolled to that turn's BOTTOM — and `G` (or `g`) is the same
destination spelled the pager way and the vi way. Both ends are stated here
because the distinction is the whole point: a reader reaching for the end key
wants the last row of a long log, not the first row of its last page, and before
this the second was what they got while nothing at all reached the first. `?`
opens the overlay (any key returns) and the overlay, this page's key block in
`-h` and the status line's short hint are all rendered from ONE list
(`replayplay.KeyMapRows`), so they cannot disagree about which keys exist.

**The DATA PLANE: the verbatim recording first, the dump only as a fallback.**
`gopi session replay <id>` draws the run's own screen stream —
`<session-id>.screen.stream`, or the pre-rename `.sgr.stream` — whenever the
session has one. That file is the bytes the renderer WROTE; nothing was parsed
on the way in, so drawing it answers what the screen actually was. Only when a
session recorded none does the verb fall back to `chat-screen.dump` plus
`session.jsonl`, and that fallback is **self-reported**: the header's `source=`
word reads `sgr-stream(verbatim)` or `dump(interpreted, throttled)`, a playback
built the second way prints a `DEGRADED:` paragraph naming both of the dump's
limits, and a warning cell in such a picture cannot be blamed on the renderer.
The choice is the `--from` flag's, and its default (`auto`) is what makes it:
`stream` **refuses** a session that recorded no screen stream rather than quietly
answering a different question, and `dump` forces the interpretation — a
legitimate question ("what did THIS build make of it?"), asked explicitly.

**`--verify OTHER` is the measurement this verb exists for.** It compares THIS
session's recording with `OTHER` (a session id, or the path of a stream file)
frame by frame, by PAYLOAD BYTES, and prints either `first divergence: none` or
the first frame that differs with the byte offset on each side and both payloads
(offsets point at the frame's payload, so `dd bs=1 skip=N` lands on it). A stream
that ends earlier than the other is a divergence, not a prefix match — that is
the shape a regression takes when a run dies before its last paint. `gopi session
log` cannot answer this (it reads events) and the dump cannot either (it is
throttled and reduced), so this is the one check a render-regression question
can rest on. It plays nothing, so it takes no view flag, and an unreadable file
is an ERROR, never `none`.

**`--full` is the DEBUG view, and it lifts BOTH ceilings.** A replay abbreviates
in two independent places: a tool call's recorded INPUT is cut at a rune bound
with a printed `…(+N runes)` note, and a turn's laid-out body is capped by a ROW
CEILING shared between all of its rounds, with each cut round printing
`(N row(s) of this turn's context are not laid out; its tool rows are)`. Both are
correct defaults — the compact view is what fits a terminal, and the tool rows are
never the part that is cut — and both are lifted by `--full`, which is why a
debug run of a large turn prints neither note. Naming only the first (as this
surface once did, so `--full` expanded every input while still reporting 19 rows
not laid out per round) is the defect this flag now does not have; the two are
separate properties of the LAY-OUT rather than one flag, because they bound
different things.

**Scope.** The chat index covers CONVERSATIONS (`gopi chat`, `gopi web`). A task
run or a one-shot prompt has a session log but no chat-index row, so
`gopi session list` does not show it — `gopi session log --last` and
`gopi task show <id>` are the doors to those.

**A synopsis is a promise.** Every `Usage:` line names one command an operator
can run: a `|` belongs in a bracketed group or in prose, never in a word slot
the parser would have to guess at. The `checkpoint` sub-group is reached in
prose for exactly this reason — the shorthand `list|export` once stood where a
command should be, and the dispatcher refused it, because under `Usage:` it read
as a promise.

The top-level verb `gopi replay` is not a command: it was abolished and the word
is refused by name. The playback lives here.

**`show`'s three ADDRESS FLAGS answer the JOIN between the two counters.**
`gopi session show <id> --round R` prints which TURN owns round R and that turn's
interval; `--turn N` prints the ROUNDS turn N covers, against the session's total;
and the third spelling, `--mapping`, prints every turn's interval, one row per
turn, with a trailing count so a truncated listing discloses its own size. Pass ONE
of them — two names two different questions, and the verb refuses rather than
answering one of them. Each prints its answer instead of the page, because the
answer must be greppable:

```
round 5 belongs to turn 1 — turn 1 covers rounds 1–6
turn 2 covers rounds 7–26 (of 31 turns)
```

Every number comes from the SAME boundaries the `show` page's `turns` row and the
`session context` export's TURN address are counted from — one algorithm, indexed
rather than re-derived — so the lookup cannot disagree with either of them. The
two refusals are deliberately different facts and say so: a round **no turn owns**
(a log that recorded no operator row, so nothing opened a turn) is not the same
answer as an ordinal **this session does not have**, and a turn that ran no
request exists in the numbering while covering no round.

ROUND is the debug axis (one HTTP request/response) and appears only where it says
it is not a turn — `session context --round R` titles its page
`ROUND R (debug axis: one HTTP request/response, NOT a turn)`. Every operator-facing
surface names the TURN: the export's screen line and page title, `/context`, and
`show`'s `turns` row.

# RECLAIMING SESSIONS (`gopi session gc`)

The verb answers two DIFFERENT questions, and it is worth knowing which one you
are asking.

**By AGE (the default).** `gopi session gc [--older-than DAYS]` lists every
directory under the sessions root with the verdict `collect` or `keep` and the
reason, then says how many are collectable and why the rest are not. Nothing is
deleted unless you pass `--apply`. Every UNKNOWN is a KEEP: a directory a live
process stands in, a directory whose holder registration cannot be read, a
directory any task-ledger row names, and a root whose process scan stopped at
its bound are all kept — "not looked at" never reads as "nobody there".

**By SIZE (`--largest N`).** Read-only, and it answers the question the age rule
structurally cannot. A big session directory is big *because it is new* — the
`*.raw.log` of a long run grows for the whole life of that run — so the ten
largest directories on a full disk are all inside the freshness window and all
`keep`. `--largest N` (default 10) sorts by bytes instead, biggest first,
keeping the SAME `keep`/`reason` column an apply acts on, so "big AND
collectable" is visible instead of inferred. `--largest` cannot be combined
with `--apply`: it is a reader, and an apply option that silently did nothing
would be worse than no option.

**Bounding a raw log (`--trim-raw-log SIZE`).** `*.raw.log` is the raw HTTP
traffic under a run — the largest single class of file in a sessions tree. A log
over the cap is rewritten to:

1. its **head** (the creation header and the first rounds — what a reader dates
   the run by),
2. ONE **marker** line stating how many bytes were dropped, when, and that they
   are not recoverable,
3. its **tail** (the most recent rounds, on complete lines).

The result is at or under `SIZE` (`64MiB` when the flag is given without a
value). The rewrite is atomic — written beside the target, fsynced, then renamed
over it — and keeps the original mode. Two refusals carry its safety:

- a log **within the cap is never opened for writing**;
- a log a **live process holds open** is refused, and so is a log whose holder
  scan stopped at its bound.

The `--trim-raw-log` arm is a DRY RUN by default: it reports what it would
free. Re-run with `--apply` to perform it. A cap below the floor (four times the
reserved head) is refused by name rather than producing a log with no round left
in it.

Neither arm runs on a schedule and neither creates anything to run later: a
human invokes the verb and reads the report.

# JSON KEYS

`gopi session list --json` and `gopi session show --json` publish one object per
session. The counts that go over the wire are named for the quantity they
measure, and the two turn quantities are deliberately NOT the same word:

| key | quantity |
|---|---|
| `turns` | `chats.turn_count` — THE turn count: the turns the session ran, every input face equal (a line the person typed, a message an external process delivered, a line the model typed for itself). The same number the list row and `session show` print under the same bare word, and the unit `sessionevent.CounterUnitTurn` names. |
| `turns_completed` | the turns that ENDED — the chat row's completed count. Omitted under the same rule as `turns` (below). |
| `turns_provenance` | WHICH PRODUCER fed the turn block, and the key to branch on. `session-log-turn-openings` = the row has a `chats` row, so `turns` is a measurement (`0` included — a chat that ran no turn). `no-chats-row` = the row is a RUN LOG with no chats column behind it: `turns`, `turns_completed` and `deprecated_keys` are **omitted**, never published as `0`. |
| `human_turns` | the PERSON-ONLY SUBSET of `turns` — the number `session log <id> --turns` tabulates. Omitted when unmeasured, with `human_turns_known` saying whether the key is a measurement and `human_turns_note` naming the reason it is absent. |

**One session id, two rows, and the turn keys say which is which.** `gopi
session list` answers one session id TWICE — `kind=chat` (the conversation, whose
turn count exists) and `kind=run` (the run log that produced it, which has none).
Both rows used to publish the turn block under the same bare key, the run row
carrying a `0` that meant "no `chats` column stands behind this" — so a consumer
summing `turns` counted a not-measured `0` as a turn count, and a reader could not
tell which row the number belonged to. The rows are BOTH kept (they are different
facts, not a duplicate); what changed is that the absence is STATED:

- the run row **omits** `turns`, `turns_completed` and `deprecated_keys` instead
  of publishing zeroes (`omitempty` on pointer fields, built by ONE function,
  `sessionJSONStripTurns`, so `session list --json` and `session show --json`
  cannot state it two ways);
- every row carries `turns_provenance`, so summing the column is a matter of
  skipping the rows whose provenance says there is no producer rather than
  guessing from a `0`;
- in the human table the same fact is a WORD: the run row prints
  `turns none (no chats row)` where a chat row spends `turns N`.

**One turn count, one word.** An earlier revision qualified the wire key
(`repl_turns`) and the human table (`turns 66 (repl turns 200)`) to keep two turn
numbers apart. There are not two: an external notify is an input and a
self-driven turn is a turn, so `chats.turn_count` is how many turns a session
ran, and the person-only subset is a DETAIL of that number rather than a rival
reading of the word. The bare key again carries the count; the subset carries its
own qualifier.

**Migration.** `repl_turns` survives until **2026-12-31** as a NUMBER nested
inside a `deprecated_keys` object (`{repl_turns, replaced_by, remove_after,
note}`), which states its own replacement and deadline on every row. It is
deliberately NOT published beside `turns` at the top level — two keys for one
number is a wire shape that depends on which spelling a reader happened to pick:

```
"turns": 200, "human_turns": 66, "human_turns_known": true,
"deprecated_keys": { "repl_turns": 200, "replaced_by": "turns",
                     "remove_after": "2026-12-31", "note": "…" }
```

`operator_turns` — the previous name of the subset key — is NOT republished under
either shape: a key that held a subset and then moves is a key whose meaning a
reader has to guess, and the subset now has one name.

# SEE ALSO

`gopi session <sub> -h` (every sub-verb answers its own page), `gopi -h`,
`man gopi`, `man gopi-chat` (the surface most sessions come from).
