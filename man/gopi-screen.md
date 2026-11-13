---
title: GOPI-SCREEN
section: 1
header: GoPi Manual
---

# NAME

gopi-screen - read a recorded screen as the bytes that painted it

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi screen replay <session-id>
        Write the recorded render bytes to stdout, unchanged.
  gopi screen replay <session-id> --verify OTHER
        Compare two recordings frame by frame, and print where they differ.
  gopi screen -h
        This page.
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi screen** is the family that handles a RECORDED SCREEN's artifact — the
`.screen.stream` file a run writes when it is told to record one. Its first
sub-verb is **replay**, and the word is deliberately the same word `gopi session
replay` uses, because the two answer two halves of one question and the halves
are not interchangeable:

`gopi session replay <id>`
: **PLAYS the recording back — a RENDERER.** It chooses a canvas at a geometry,
paces the frames at a rate a human can follow, and falls back to the reduced
dump when the session recorded no stream. The answer it gives is *"what was on
the screen"*.

`gopi screen replay <id>`
: **WRITES the recording — the render bytes exactly as the run wrote them.** No
canvas is built, no frame is paced, no colour is rewritten and no line is
re-wrapped. The answer it gives is *"what bytes did the renderer emit"*, which is
the only answer that can be hashed, diffed, piped into another program or handed
to a terminal that is not throttled.

The distinction is not stylistic. A caller who wants the bytes and receives a
renderer's picture cannot recover the bytes from it: `gopi session replay --from
stream` still walks through the player, so what it emits is the player's
output. That is why the second reader is a surface of its own rather than a flag
on the first.

A session that recorded no screen stream has no bytes to write. This family
**refuses it by name**, naming `gopi session replay <session-id>` — the reader
that can still show the reduced dump. It never substitutes one artifact for the
other: a dump is this program's interpretation of the paint bytes, so printing
one where a caller asked for the bytes would answer a different question in the
same shape.

# SUBCOMMANDS

`replay <session-id> [--verify OTHER]`
: write the recorded render bytes to standard output, or compare two recordings.
See `man gopi-screen`'s OPTIONS below and `gopi screen replay -h`.

# OPTIONS

`--verify OTHER`
: do not write the bytes. Compare THIS session's recording with `OTHER` — a
session id, or the path of a screen-stream file — **frame by frame**, and print
where they first differ: the frame number, the byte offset on each side and both
payloads. Prints `first divergence: none` when the frame sets are identical.

**Frames, not files.** The artifact's header names the build, the grid and the
start stamp, so two recordings of the same picture made by two binaries at two
moments are never byte-identical as FILES, and are frequently identical as
PICTURES. Comparing files answers a question nobody asked; comparing frames
answers the one that matters.

`-h`
: the page you are reading, or — after a sub-verb word — that sub-verb's own
page.

# THE OPERAND

A session id, resolved through the same directory `gopi session log` resolves,
or the PATH of a stream file (a copy, or a file produced on another host, can be
read directly).

The artifact is `<session-id>.screen.stream`, or the pre-rename
`<session-id>.sgr.stream`; **every reader accepts both names**. Record one by
setting `GOPI_SCREEN_STREAM=PATH` (or `GOPI_SCREEN_STREAM=1` for the default
location beside the session).

# OUTPUT STREAMS

**Standard output carries the recording and nothing else** — every frame's
payload, in recorded order. A caller redirecting stdout into a file or another
program receives the recording's own bytes, never a word this command added.

**Every diagnostic goes to standard error**: the frame count, the byte count,
the grid, the revision, and how the recording ENDED. That last one has four
distinct sentences and they are not one boolean:

* **closed** — the close-out record is present. Nothing is printed; a healthy
  recording produces an empty diagnostic stream.
* **whole but unclosed** — every frame ends on a record boundary, but the
  recording run never wrote its close-out record. Nothing is truncated; the
  artifact is simply unclosed.
* **cut inside a frame** — the stream ends inside a frame, and the sentence
  carries the byte offset. What was written above is what reached the terminal
  and no more.
* **contradictory** — the file's own records disagree with each other about how
  much it should hold. That is a fact about the FILE, not about the code that
  reads it.

A fifth sentence exists for the case the scanner itself cannot run, and it is
never folded into "nothing is wrong".

# EXIT STATUS

`0`
: the bytes were written, or the comparison ran — a divergence is a FINDING, not
a failure, and is reported on stdout.

`1`
: no session id, a flag this page does not know, a session that recorded no
stream, an unreadable artifact, or a comparison that could not run.

# SEE ALSO

`gopi screen -h`, `gopi screen replay -h`, `gopi session replay -h`, `gopi -h`,
`man gopi`, `man gopi-session` (the renderer, and the session tree this reads).
