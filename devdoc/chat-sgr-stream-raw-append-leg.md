<!-- doc-class: normative -->
# The raw SGR append leg — bytes beside the interpretation

`gopi chat` has written `chat-screen.dump` since 2026-08; that artifact is what
`gopi ui_state --target chat` and the `chat_surface` tool read, and it is the
screen an agent acts on. **It is an INTERPRETATION**, and three properties make it
one:

- it is throttled (`internal/uiclient/dump.go`: `dumpMinInterval`, 100 ms) — a
  burst's interior frames never reach the file;
- it is reduced to the cells `lineedit.Screen` models — attributes the grid folds,
  scrollback past the model's history, and every byte that painted no cell are
  simply absent;
- a frame whose body is identical to the last written one is not written at all.

Each property is correct for its job ("what does the screen show right now") and
wrong for another one: **"did the renderer write the wrong cell, or did our reader
mis-read the right one?"** That question cannot be answered from the dump, because
both halves of the comparison come from the same interpreter.

## What the second artifact is

`internal/uiclient/sgrstream.go` (`SgrStream`) appends every `Write(p)` the sink
observes — verbatim, unthrottled, no frame dropped — beside the dump:

```
#gopi-screen-stream version=1 format=len-prefix terminator=# grid=100x30 revision=<rev> started=<RFC3339>
<decimal payload length>\n<exactly that many raw bytes>
<decimal payload length>\n<exactly that many raw bytes>
…
#gopi-screen-stream-end frames=709 bytes=39450 grid=100x30
```

Nothing is parsed on the way in. The framing is a decimal length and a newline,
which is the only scheme that can survive a payload whose bytes include `0x00`,
`0x1b`, CR, LF and digit runs: the parser is always positioned at a frame boundary
when it reads a length, and a line beginning with `#` is a record (header or
trailer), never a length — `#` is not a digit, so the two grammars cannot collide.

**Two record lines, and the trailer is the important one.** A stream without a
trailer is a run that died mid-frame; a stream with one says the run reached
close-out. The reader (`ReadSgrFrames`) returns the frames it completed plus
`Trailer=false` for the first shape and does **not** error — that is a real state
of the world, and it is the artifact most worth reading. A file with no header, or
with a version this reader does not know, **is** an error: that is not truncation,
it is a different format wearing this one's name (Rule 15).

## The switch, and why OFF costs nothing

**ON by default** (operator ruling, 2026-10-31: 数据的不可再生性高于代码完整性 — the
bytes of a paint sequence cannot be re-created afterwards, so a run that did not
record has nothing to replay, while the cost of recording is one file beside the
dump). The off state is still `DumpConfig.Sgr == nil`: `ScreenDumper.Write` checks
the field before doing anything — one branch, no allocation, no syscall, and no
file. The artifact does not exist at all rather than existing empty, so "declined"
and "armed but broken" cannot be confused.

Because the switch is armed by DEFAULT, the off state is **announced** rather than
left silent: the run prints `chatSgrStreamOffNotice` before the terminal is taken
and the `decision=off` line in its closing report. A default that silently failed to
arm would otherwise be indistinguishable from a deliberate decline (Rule 15).

```
gopi chat --screen-stream[=PATH]     # joined form only (a bare next arg is a session seed)
gopi chat --no-screen-stream         # decline for this run; takes NO value
GOPI_SCREEN_STREAM=1                 # resolve the path
GOPI_SCREEN_STREAM=/path/to/x.stream  # or name it
GOPI_SCREEN_STREAM=0                 # the environment's off switch (any falsey value)
# GOPI_CHAT_SGR_STREAM is the PRE-RENAME spelling: still read, current name wins
```

Both flag spellings may appear on one command line: the **last one wins**, which
keeps "re-arm a run that exported a falsey variable" an ordinary typed command
rather than a second environment. `--screen-stream`/`--no-screen-stream` are the two
directions of ONE switch, so they sit beside each other in the usage page and in
the resume vocabulary, and a recorded decline is restored by `--resume` for the
dump's reason: a resumed run that ignored it would re-arm an artifact the operator
turned off.

Path resolution follows the dump's shape (`cmd/agent/chat_sgr_stream.go`,
`chatResolveSgrPath`): explicit path → this run's own session directory, **named
after the session** (`<session-id>.screen.stream`, so two runs sharing a container
directory stay distinguishable) → the chat's own scratch class (Rule 22, never
`/tmp`). A **replay** is redirected into the scratch class for the dump's own
reason: the recording is what a replay reads, and writing into it would make
`--replay`'s promise false in a way nobody notices until someone hashes the
recording.

On a `--film` command line **both flag spellings** are refused by name, in two
different sentences: the arm flag would write nothing and say nothing, while the
OFF switch would leave the FILMED run recording (the opposite failure, and the one
a caller is likelier to hit now that the default is on). The **variable** works
there, because the producer inherits the environment.

## How it is verified

Two instruments, and the second one is independent of this program:

```bash
go test ./internal/uiclient/ -run 'SgrStream' -count=1
go test ./testsuite/ -run 'TestChatSgrStreamPTY' -count=1 -v
```

The unit cases pin: unarmed = nil stream + no artifact + an interpreted dump
byte-identical to the pre-capability shape; one frame byte-for-byte identical
including `0x00`/`0x1b`; a truncated stream still reads; recorded bytes replay
through a fresh `lineedit.Screen` into the sink's own grid.

The pty case is the acceptance reading, and its judge is **the kernel's pty**, not
this package's writer: every recorded frame must appear in the byte stream the
harness itself received, in order (an in-order subsequence walk — other writers
interleave, but a frame cannot be invented, reordered or altered by a byte); the
frames replay into the dump's grid row for row; and an answer longer than the
screen leaves its **first** word in the recording while the final screen has
scrolled past it — the gap that proves this leg is not a second copy of the grid.

## The reader: `gopi session replay`, verbatim first

The recording is only worth its bytes if the reader draws THEM. `gopi session
replay <id>` now resolves its data plane through `internal/replayplay/stream.go`:

| order | artifact | header `source=` word |
|---|---|---|
| 1 | `<session-id>.screen.stream` (or the pre-rename `.sgr.stream`) | `sgr-stream(verbatim)` |
| 2 | `chat-screen.dump` + `session.jsonl` | `dump(interpreted, throttled)` |

The second row is a **self-reported** degrade: the header's `source=` word says
which one a reader is looking at, a playback built from the dump prints a
`DEGRADED:` paragraph naming both of that artifact's limits (`dumpMinInterval`,
grid-cell reduction), and the geometry provenance word is `screen-stream` when the
recording's own header supplied the grid — a run that resized wrote both artifacts
at different sizes, and the replay must say which one it is painting at.

`--from auto|stream|dump` chooses explicitly. `--from stream` is a
**requirement**: a session that recorded no screen stream is REFUSED, naming the
way to get the dump — measured on this tree (2026-11-08) it used to fall through
to the dump, so a caller who demanded the renderer's own bytes was silently handed
the interpretation, and the output looked correct.

**Both suffixes are read, through one list** (`uiclient.SgrStreamSuffixes`): the
sessions this host recorded before the rename are real data, and a resolver that
only asked for the new spelling would hide them. A directory holding both resolves
to the CURRENT one.

## The measurement: `--verify`, frame by frame

```bash
gopi session replay <id> --verify <other-id-or-stream-path>
```

It compares the two recordings **by payload bytes** and prints either
`first divergence: none` or the first frame that differs, with the byte offset on
each side and both payloads. The offsets point at the frame's PAYLOAD, so
`dd bs=1 skip=N count=464` reads that frame's bytes out of the file — an offset
that named the length prefix would be off by its digits, which is why the test
seeks the file and compares the bytes back.

A stream that ends earlier is a **divergence, not a prefix match** (the shape a
regression takes when a run dies before its last paint), an unreadable file is an
ERROR rather than `none` (the measurement must never fail open), and it takes no
view flag because it plays nothing.

Measured on this host (2026-11-08), two real recordings:

```
$ gopi session replay e68fddf5-32a9-4e6d-b06f-13a101295883 --verify e68fddf5-32a9-4e6d-b06f-13a101295883
  A: frames=635322 bytes=27886779
  B: frames=635322 bytes=27886779
first divergence: none

$ gopi session replay e68fddf5-32a9-4e6d-b06f-13a101295883 --verify 2d3d0c42-e5e1-4ea0-b2b3-5166b4721674
  A: frames=635322 bytes=27886779
  B: frames=35664 bytes=1335669
first divergence: frame 44 — payload bytes differ (464 byte(s) each; first differing byte at 326)
  A: frame 44 at byte offset 6130 — "\x1b[22;1H\x1b[38;2;204;155;31m│\x1b[0m…"
```

This is the check `gopi session log` cannot answer (it reads events) and the dump
cannot either (it is throttled and reduced). It is why the data plane moved.

Verification for this half:

```bash
go test ./internal/uiclient/ -run 'TestSgrStreamCompare|TestSgrStreamSuffixes|TestSgrStreamReaderAccepts' -count=1
go test ./internal/replayplay/ -run 'TestScreenStreamPath|TestLoadScreenStream|TestLoadStreamLeg|TestParseStreamFrom|TestLoadSessionAtFromStreamIsARequirement' -count=1
go test ./cmd/agent/ -run 'TestSessionReplayFromFlagGrammar|TestSessionReplayVerifyFlagGrammar|TestSessionReplayUsageDocumentsBothNewFlags' -count=1
```
