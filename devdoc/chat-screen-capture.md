<!-- doc-class: normative -->
# `/screen`: the capture is a file, the turn carries its path

`gopi chat` already records every byte its renderer paints into a
length-prefixed screen stream (`--screen-stream`, `internal/uiclient.SgrStream`). That
artifact is the right TRUTH and the wrong thing to hand a model: it is a stream,
it grows without bound, and its content is escape sequences. `/screen` closes the
gap by putting ONE TURN's screen on disk and letting the next turn name it.

## The shape

```
/screen [N] [--out PATH]
```

1. **Copy ONE TURN, WHOLE.** With no `N`, the newest turn the recording holds —
   the screen as it stands. `/screen 58` is turn 58. The span begins at the frame
   that drew that turn's header and ends at the frame before the next turn's
   header; nothing is dropped, and **there is no way to ask for less**, because a
   screen the model only half saw is a screen it will describe wrongly. The byte
   budget this command used to apply (64 KiB of payload / 512 render writes) and
   the `--full` flag that removed it are both GONE, and the flag is refused BY
   NAME with the reason if anyone types it.
2. **Park a ONE-SHOT reference** on the session (`chatSession.screenPending`):
   absolute path, byte count, frame count, WHICH TURN the file holds, and how to
   read it in pages. It carries no SGR bytes — the artifact is one `read` away.
3. **The next turn's message carries that note**, and the slot is CLEARED by the
   read. One capture describes exactly one turn.

Nothing is sent when the command runs: `/screen` costs no model turn.

## Where a turn begins — the anchor, and the two criteria it took to get right

A stream is a flat sequence of frames: nothing in the format says where a turn
starts. The anchor is the renderer's OWN mark — it paints
`──── turn N · <prompt>` above every answer — and it is found by
`internal/uiclient.LocateSgrTurns` (`sgr_turn.go`), not by a byte offset chosen
here.

**The stem is DERIVED from the renderer, never written down**: the common prefix
of `lineedit.TurnHeader(Index:0)` and `(Index:1)` is everything the header draws
before the number, and it is then VERIFIED by requiring `TurnHeader(12345)` to be
that stem followed by `12345`. A header shape the derivation cannot describe is
refused with a reason rather than silently matching nothing.

**The stem alone is not the criterion** — and the honest state of the evidence is
worth writing down, because it changed while I was measuring it.

On `e68fddf5` at `size=9543867 mtime=2026-11-07T07:17:59` (60090 frames — quote the
size and mtime WITH any frame count; a live `gopi` still holds this file at fd 7w, so
the count moves) a plain stem matcher finds **14** occurrences and there are **14**
turns: on THIS file the stem alone happens to be
enough, so this recording does **not** demonstrate the defect. The two false-positive
shapes are measured elsewhere and by someone else — a sibling run
(`01a1b2fc`, whose own streams carried them) reported:

- a frame that QUOTES an old screen, drawing a header mid-line (its offset 147071);
- a frame that prints the ESCAPE'S OWN SPELLING in front of a header, after a
  genuine cursor move (the `\x1b` there is ten printable bytes, not an escape).

An earlier reading of this page said "17 sites where there are 12 turns" on this
recording. **I cannot reproduce that and it is withdrawn**: 17 and 12 were readings of
the same file at two different moments while a live process still held it open, which
is the very error this page warns about two paragraphs down. Each shape still has its
own gate (`TestSgrTurnLocatorRejectsAHeaderDrawnMidLine`,
`TestSgrTurnLocatorRejectsACopyThatCarriesTheEscapeText`) built from the sibling's
measurement — the gate is the artifact, not the anecdote.

The rule is therefore an **absence**: between the header and the most recent
cursor-position sequence there must be **nothing but complete control sequences**.
Printable text in that gap refuses the boundary. The walk skips sequences rather
than demanding a cursor move immediately before the stem, because the renderer
positions the cursor and sets a colour first, and the shape is NOT fixed. Measured
over the 14 header frames of that recording, grouped by the bytes in front of the
stem:

```
count=13  ESC[22;1H ESC[38;2;255;248;220m
count=1   ESC[17;1H ESC[38;2;255;248;220m
```

Two segments here; an erase-line would be a third, and the ANY-COUNT skip below
passes it while a fixed-length rule would fail. **The row varies (22 and 17 in one
recording)** — the third independent demonstration that the cursor row is a screen
coordinate, not a format fact. Demanding the cursor move ADJACENT to the stem
located **0 of 12** turns, which is how that stricter rule was rejected instead of
shipped.

That paragraph was wrong in an earlier revision of this page, and the correction
is worth stating: I had described the prefix as CUP + erase-line + SGR, having read
the `ESC[2K` **across a frame boundary**. The predicate skips arbitrary sequences
and never depended on the count — but the stated MEASUREMENT did, and a wrong
measurement written into a document is a claim the next reader acts on.

**A repaint is not a boundary**: the walk keeps a running maximum, so a frame
naming a turn at or below the highest already seen is chrome (a scroll, a resize,
the band rewriting itself). That rule only HIDES the two shapes above by luck —
both copies happened to be painted after the turn they quote — which is why it
does not replace the absence rule.

## Why it is REPL-native and not a `slashCommands` entry

`cmd/agent/commands.go`'s table receives **nothing but the words typed**. This
command's entire subject is per-process state the table cannot see:

- the LIVE `*uiclient.SgrStream` of this run — the handle the renderer is
  appending to as the command runs (a second resolution of the path could
  describe a different file than the one being written);
- the NEXT user turn of THIS REPL — the injection rides the message boundary
  (`chatOutbound`), which exists only on the chat path.

So the word, the argument parser, the renderer and the usage text live in
`cmd/agent/chat_screen.go`; the word joins `chatSessionWords` (so the completion
menu learns it from one place) and `chatNativeUsage` answers `/help screen` from
the same `chatScreenUsage` constant the command prints. The cost of the choice,
stated so it is not mistaken for an oversight: `/screen` is not available on the
`--once` or web faces, because there is no "next turn" there to describe it to.

## The injection point, and why it is there

`cmd/agent/cli_chat.go`, in the turn loop, AFTER the seal succeeded and BEFORE
the prefetch adoption:

- **after the seal** — a REFUSED turn must not eat the operator's capture; the
  capture is a one-shot, so consuming it on a turn that never leaves the process
  would destroy it;
- **before the prefetch** — the retrieval must see the message the model will
  actually receive, not the one it would have seen without the note.

`chatScreenInject` appends the note to the operator's own line, keeping that line
byte-identical at its START (a reader's search for what they typed still finds
it), and sends the reference alone when the line is empty.

## The readers (`internal/uiclient/sgrstream_range.go`, `sgr_turn.go`)

Two passes over an `io.ReaderAt`:

1. **index** — walk the record headers, record each frame's position and payload
   length, copy no payload (`walkSgrMarks`, shared by the locator and the copier
   so a second spelling of "where does frame N start" cannot drift one frame);
2. **copy** — write the selected frames through the SAME
   `sgrHeaderLine`/`sgrTrailerLine` composers the recording itself uses, so a
   range artifact and a recording cannot drift in shape, and a copy restates the
   moment the SOURCE began (`SgrStreamMeta.Started`) rather than the moment the
   copy was made.

The walk tolerates a cut mid-frame by design — a live stream is being appended
while the command runs — and keeps the frames it completed. That is the right
behaviour for a snapshot: what had been painted at the instant it was taken.

## The defect this feature found, and the lesson it carries

The first working version of `/screen` reported **"nothing has been painted
yet"** over a stream holding 256 frames. Root cause was in the range reader, not
in the capture: `readSgrRecordAt` appended the chunk prefix its per-byte loop had
already appended, so every record line came back DOUBLED — a 111-byte header read
as 222, each frame prefix `16` read as `1616` — and the walk then rejected its own
input, parsing a full recording as **zero frames with no error**.

Two things are worth keeping from it:

- **A parser that reads its own output must be checked against raw bytes.** The
  regression test (`sgrstream_range_test.go`) asserts on the FILE — one header
  prefix, not two; a first line no longer than the format allows — not on the
  reader that was wrong. A test written from the reader's point of view would
  have agreed with the bug.
- **`frames=0` and `err=nil` is a silent failure** (Rule 15). The honest forms are
  two: a parse error naming the byte offset, or a count with the file it counted.
  `/screen` could only say "nothing has been painted" because the reader gave it
  no way to tell that apart from "the recording is empty".

## What to run

```bash
go test ./internal/uiclient/ -run 'TestSgrTurn' -count=1

# and the same locator over a REAL recording (the only evidence that the
# renderer's repaints, escapes and Chinese text do not defeat the anchor):
GOPI_REPLAY_SGR_TURN=/path/to/<sid>.sgr.stream \
  go test ./internal/uiclient/ -run TestSgrTurnLocatorOnARealRecording -count=1 -v
# measured 2026-11-07T07:17:59 on session e68fddf5 at size=9543867:
#   located 14 turn(s) 59..72 in 60090 frames
# (an earlier reading of 12 turn(s) 59..70 / 55377 found the SAME file before it
#  grew — a live gopi still holds it at fd 7w. A reading is (value, size, mtime),
#  never value alone: two counts of one growing file look like a contradiction and
#  are just two moments.)

go test ./cmd/agent/ -run 'Screen|Slash' -count=1
go test ./internal/uiclient/ -run 'SgrRange' -count=1
```

**And a mock REPL is not the real one.** A sibling run (`01a1b2fc`) drove a live
`gopi chat` through a pty against this very binary and produced the artifacts
`screen-turn1.sgr.stream` (274 frames) and `screen-turn2.sgr.stream` (293 frames) in
session `f26aee98`; read with an INDEPENDENT probe, each holds **exactly one** turn
header, at frame 0, numbered to match its own name. That is evidence the synthetic
e2e below cannot give: the real run OPENS its recording with `O_TRUNC`, so what the
fixture appends and what the renderer actually painted are two different histories,
and only a live run shows the second one.

**WHAT THAT WALK DOES NOT PROVE — stated here because a walk read as "everything was
verified" is dearer than no walk at all.** It was a **two-turn** conversation. So it
establishes, end to end on a real console:

- `/screen` captures the current turn and `/screen 1` captures turn 1, each artifact
  holding exactly the turn its name claims (name = content, the operator's defect);
- an out-of-range request is REFUSED with the range that would have worked —
  measured on that console, verbatim: `/screen 58: this recording carries turns 1..2;
  turn 58 is not among them.`, i.e. `RangeWords()` ran on a real path and not only
  under a unit test;
- the copy begins at the header frame, so `ReadSgrTurnFile`'s span start is written
  first — a fact no assertion about byte counts would catch.

It does **NOT** establish that `/screen 58` finds the 58th turn of a long session.

## What the LOCATOR layer proves — a third claim, made separately on purpose

The RANGE layer has its own evidence, and it is about `Span(n)` + `ReadSgrTurnFile`,
NOT about the REPL that calls them. Measured on the real recording (60090 frames at
size=9543867), and reproduced independently by two runs before it was written down:

```
RANGEWORDS="59..72" STREAMFRAMES=60090 SPANS=14
PROBE 59  span[353,3744)    frames=3391  own=1 other=0
PROBE 65  span[27598,34066) frames=6468  own=1 other=0
PROBE 70  span[51496,55387) frames=3891  own=1 other=0
PROBE 72  span[58268,60090) frames=1822  own=1 other=0
PROBE 73  ABSENT(refused)
PROBE 58  ABSENT(refused)
```

`own`/`other` are counted in the COPIED bytes after the read — occurrences of
`turn <n> ` for the requested turn and for every other turn — not read out of the
span's metadata, so the assertion is about the artifact rather than about the index.

Three things this establishes, each a contract this page states elsewhere:

- a far turn number RESOLVES and the copy holds exactly that turn (four samples,
  including the first span and the last);
- **the last span's `EndFrame` equals the stream's own frame count** (58268+1822 =
  60090) — the "spans tile the stream" claim, on real data rather than in a fixture;
- an out-of-range turn is REFUSED, not clamped to the edge: 58 (below) and 73
  (above) are both absent, so `Span(n)` cannot silently return a neighbour.

## The REPL layer's FAR turn — now closed, by a recording rather than a script

The gap above was real for one revision: the console path had only turns 1 and 2, so
`/screen 60-something` had no end-to-end evidence. It is closed by
`cmd/agent/chat_screen_e2e_real_test.go`, which feeds a REAL recording's span into
`runChatWithInput` — the same entry the interactive and `--once` faces call — types
`/screen <turn>`, and asserts on both the artifact and the request after it:

```
$ GOPI_REPLAY_SGR_TURN=<real .sgr.stream> go test ./cmd/agent/ \
    -run TestChatScreenE2EFarTurnFromARealRecording -v
    chat_screen_e2e_real_test.go:103: real recording: 14 turn(s) 1..14 in 28991
      frames; /screen 13 -> screen-turn13.sgr.stream holds 13
--- PASS

    chat_screen_e2e_real_test.go:103: real recording: 19 turn(s) 1..19 in 37893
      frames; /screen 18 -> screen-turn18.sgr.stream holds 18
--- PASS
```

Those two readings are quoted because the recordings they were taken from are
**stable on disk** (written once, closed). An earlier reading was taken from a
stream that a live session still held open and subsequently rewrote with
`O_TRUNC`; that number is deliberately not repeated here — a figure about a file
something is still writing is true only of the instant it was read, which is the
same lesson `devdoc/` records about content-identity readings.

The turn it asks for is the **second-to-last**, and that choice is the assertion's
design: the newest would be satisfied by a bare `/screen` (proving nothing about the
argued number) and the first could look like a hit if anything clamped to a boundary.
It also asserts the artifact does NOT paint the newest turn's header — the "one turn
too long" failure, which a byte count cannot see — and that the following request
names the path while carrying no painted byte.

**What this still does not prove**: that a 60-turn CONVERSATION behaves this way. The
frames are copied from a recording into a fresh run's stream; the run itself had two
turns. It is the REPL layer's far-number path, not a long session's.

Three claims, three paragraphs, deliberately not merged.

`cmd/agent/chat_screen_e2e_test.go` drives the real REPL: it appends frames from
inside the first model request's handler (the only moment a live run's stream has
content — the run OPENS its recording with `O_TRUNC`, so anything a fixture writes
before that is wiped, measured), then types `/screen` followed by a question, and
asserts the LAST request body carries the path and byte count, that NO `\x1b` byte
is in it, and that exactly ONE message carries the reference across the following
turns (a note that is not cleared appends a second copy every turn — "consumed
once" is a statement about MESSAGES, not about requests, because every turn sends
the whole conversation).
