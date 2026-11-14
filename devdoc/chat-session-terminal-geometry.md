<!-- doc-class: historical -->
# Chat Session Terminal Geometry — Recorded, Replayed, and Documented

**Status:** landed (task `01a10351-6fae-7905-8255-7a76b5d2c0b0`, branch
`task/01a103516fae790582557a76b5d2c0b0`).

## The defect this closes

Two facts, both measured, both wrong before this change:

1. **A chat session did not record the terminal it happened on.** `session_start`
   carried session id, model, cwd, pid and task id — no geometry. A replay or a
   film therefore had no way to ask "what size was this session made at?", so the
   film driver fell back to `config.ChatIdealGeometry`'s built-in `117x26` for
   every recording, old or new.
2. **`gopi chat --replay <id> -h` exited 2 with stdout EMPTY.** The `-h` branch
   refuses to print usage when a positional argument is present ("usage only —
   it cannot be combined with the argument X"), and the walk had already eaten
   `<id>` as the value of `--replay` — so the word that reached the positional
   check *was* the flag's own value. The operator asking to read the page of the
   mode they had just named was refused by the very word they named it with.
   Siblings: `--film DIR -h`, `--out F.gif -h`, `--resume <id> -h`,
   `--chat-ideal-geometry 100x30 -h`.

## The recording side

`internal/sessionevent.TerminalGeometry` is one value with two states, written as
a STRUCT rather than as two bare ints:

```json
{"state":"measured","cols":100,"rows":30}
{"state":"unknown","reason":"no standard descriptor is a terminal"}
```

- An **unmeasurable** terminal (the `--once` face, a filer's producer before its
  pty exists, CI) records `unknown` **with the reason** — explicitly NOT `cols:0`,
  because "I could not measure it" and "it is 0 cells wide" are different facts
  and only one of them can be true (Rule 15a: a required non-zero value keeps its
  zero-value case distinguishable).
- `DecodeTerminalGeometry` refuses a `measured` state whose cols/rows are not
  positive, and refuses an unknown state with no `reason` — a half-written field
  is an error to its reader, never silently "no geometry".
- Sessions written **before** this field exist simply have no key; every reader
  (`{ok:false}`) treats that as *no geometry recorded*, distinct from *recorded
  unknown*, and the caller states its fallback out loud.

`internal/terminalgeom` is the ONE measurement (Rule 14 — the session writer is
its consumer here, and the chat verb's own diagnostic path already read the same
descriptors): `stdout → stderr → stdin`, first descriptor answering a positive
cols×rows wins. The order is the surface's own — the chat layout paints on stdout
when stdout is a terminal and on stderr when it is not
(`chatLayoutFile`), with stdin as the fallback for a redirected run that still
has a keyboard. The ioctl itself is NOT re-implemented: `lineedit.TerminalCols/
TerminalRows` remain the single TIOCGWINSZ request sit.

The measurement is taken **once**, in `NewSessionLoggerForRun`, before the first
row is written, and is written to both readers of the run's own facts:
`session_start` (what a replay reads) and `metadata.json` (what a human greps) —
both naming the key through `sessionevent.FieldTerminalGeometry`, so the two
cannot drift. A SIGWINCH mid-session changes the terminal, not the record of
where the session began.

## The replay / film side

`internal/replaydrive.LoadRecordedGeometry` answers "what size was this session
made at?" from the recording's own header, **without loading the recording**
(bounded to 64 leading rows, the header being line 1) — the film driver must
decide its canvas *before* it starts an X server or spawns a producer.

`chatReplayApplyRecordedGeometry` then makes that number the run's canvas:

- **Recorded measured geometry → this run's ideal geometry**, printed to stderr
  together with its source, so a canvas chosen by the recording and one chosen by
  a fallback are never confused.
- **No geometry (pre-field recording) or recorded `unknown` → the built-in ideal**,
  again with a visible notice naming why.
- **An explicit `--chat-ideal-geometry COLSxROWS` wins**, and that is not a
  special case: the flag's own parser publishes it, and the recorded geometry is
  applied only when the operator did not state one.

For the **film** path the same resolution is stated before the shoot:

```
[gopi film] shooting at the recording's own terminal size 100x30 (no --cols/--rows was given on this command line)
```

`chatBeatFlag` gained `colsSet`/`rowsSet` (Rule 15a again): `0` cannot say
"the caller stated this dimension", because every value in that vocabulary is a
positive cell count — a stated size wins over the recording, a defaulted one does
not, and the driver must be able to tell them apart.

## The help side

`gopi chat --replay <id> -h` now prints the verb's page (rc=0) and carries
`chatReplayExamples`: five runnable invocations, including the film one that
writes `/tmp/replay.gif`, plus a size reference — the terminal cell is **6x13 px**
in the built-in font, so `100x30` cells is a `604x394` px frame and the default
`117x26` is `706x342` px. The walk distinguishes a positional from a **flag's
spaced value** (`spacedBesideHelp`), so usage stays refused beside a real
positional (`gopi chat -h some.log` is still an error) while a flag's own value
no longer blocks the page.

The "text is too small" question is **out of scope here** and is recorded as
such: cell size is fixed by the film terminal's font and the replay's own
`--chat-ideal-geometry`; a font-size knob would be a new surface, not a defect in
this one.

## Verification (real runs, not the suite)

Harness: a pty of a KNOWN size (`pty.fork` + `TIOCSWINSZ`), kept under this
session's own evidence directory (Rule 35/37 — not a repo artifact).

| what | command (verbatim) | observed |
|---|---|---|
| recording, TTY | `python3 …/pty_chat_record.py /tmp/gopi-geom-check 100 30 /tmp/geom-pty-100x30.log` | `session_start.terminalGeometry = {"state":"measured","cols":100,"rows":30}`; `metadata.json` identical |
| recording, no TTY | `echo "say hi" \| /tmp/gopi-geom-check chat --once -p "say hi in one line"` | both readers: `{"state":"unknown","reason":"no standard descriptor is a terminal"}` |
| film at recorded size | `gopi chat --replay 6433bf19-… --film /tmp/geom-film --out /tmp/geom.gif` | stderr: *shooting at the recording's own terminal size 100x30*; frames **604x394** px — exactly `6*100+4` × `13*30+4` |
| old recording fallback | `gopi chat --replay <path> --film /tmp/geom-film-old2 --out /tmp/geom-old.gif` (header with the key stripped) | stderr: *the recording carries no terminal geometry; the frames are not the size the session was made on*; frames **706x342** px = the `117x26` ideal |
| helper page | `gopi chat --replay 6433bf19-… -h` | rc=0, **37465 B** on stdout, 0 on stderr, examples and the 6x13 / 604x394 reference present |

## Not done (honest)

- The **interactive** (non-`--once`) chat face was not driven end to end: every
  recording-side check above runs through `--once`, which is the same
  `NewSessionLoggerForRun` writer and therefore the same measurement. No claim is
  made here about interactive-only behaviour.
- Cell size is fixed at **6x13 px** and no font-size knob was added (see above).
- Nothing was added to `go.mod`; no new dependency, script, cron line or
  resident process (Rule 35) — the pty harness is a session-dir file, not a
  deliverable.
