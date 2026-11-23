# Example: two agents, one repository

![](replay.gif)

Every command and output below was **actually run on this machine**. The inbox
counts are from the two real sessions that wrote these examples.

The animation above is **not** a drawing of the idea — it is a recording. It was
made by re-running one of the two real sessions (`gopi chat --replay`) inside a
private X display and photographing the live chat surface once per drawn beat;
the model's words and every tool result come out of the recording, and no tool was
executed to make it. Reproduce it with the command in
[Reproducing the recording](#reproducing-the-recording).

---

## The problem this answers

Two agents working on one repository need to exchange findings — "I measured this",
"I think you're wrong", "that number changed". Giving them a chat room means one of
them holds the conversation in its context and the other does not. Giving them shared
memory means one's mistake becomes the other's premise, silently.

GoPi gives each session a **mailbox keyed on a session id**. No shared context, no
shared memory — one file per session, and a receipt written by the reader.

## 1. The address is a session id, not a username

```console
$ gopi notify --session 4d973459-6a04-4435-8015-8ea18fb097e5 --message "$(cat /tmp/msg.md)"

session dir: ~/.gopi/sessions/4d973459-6a04-4435-8015-8ea18fb097e5 (resolved as its session directory name)
the session takes it at its next round boundary (an IDLE console wakes for it and
opens the next turn by itself; a turn already in flight is never interrupted)
```

Two sentences there are load-bearing:

- **"at its next round boundary"** — a message is never injected mid-thought. A round
  is one model call, so a running agent picks it up within minutes, but never between
  a tool call and its result.
- **"an IDLE console wakes for it"** — a session that has gone quiet is not dead; the
  message opens its next turn by itself.

## 2. The message is durable, and the receipt is the reader's

```console
$ python3 -c "count kinds in ~/.gopi/sessions/<id>/messages/inbox.jsonl"
{'message': 38, 'delivered': 38}
```

38 messages this pair exchanged, and 38 `delivered` lines. The second number is not
a courtesy the sender prints — **the `delivered` line is written by the receiving
session when it actually takes the message**. That is the difference between "I sent
it" and "it was read", and it is the difference between a log and a receipt.

The inbox is a file, so it survives a restart. A session that is not running today
still receives what you left for it.

## 3. What the two sessions actually did

```
   session A (this one)          session B (hy4-flash)
   ────────────────────          ────────────────────
   measured, drafted             measured, drafted
        │                              │
        ├── notify ──────────────────► │
        │                              │
        │ ◄────────────────── notify ──┤
        │                              │
   changed its mind twice         changed its mind twice
```

Neither side deferred to the other, and neither was the "senior" one. The corrections
that landed went both directions:

- **B overturned one of A's claims** — A reported three rows of stale bigram residue;
  B pulled the raw data and showed the criterion was a false positive. The count was
  zero. A withdrew it.
- **A corrected B's drafts three times** — a private path written out in full in a
  public repository, a missing exit-code level in a status table, and an index entry
  pointing at a directory that did not exist yet.

Both of those classes of error are invisible from inside one session: the author of a
false positive cannot see that the criterion is wrong, and the author of a missing
table row is looking at their own table.

## 4. Why a message beats a re-dispatch

A worker that has gone the wrong way can be stopped and a fresh row dispatched. That
works — and it costs one full context: measured on this machine, a three-hour worker
run that was re-dispatched burned **310 million tokens** to re-derive what the first
run already had.

A notify costs a few lines and **keeps the run**. Evidence arrives inside the same
row, one iteration at a time, instead of in three parallel rows that each measured a
different thing.

The one thing a message must carry, and it is not obvious: **the sender's own return
address.** A session that receives a correction with no way to answer can only
comply or ignore — it cannot disagree, and disagreement is where the value is.

---

## Honest boundaries

- **There is no shared memory.** Each session's distilled lessons are its own. What
  crosses over is what somebody typed into the mailbox.
- **A message can be missed.** It is delivered at a round boundary; a session that
  ends before reaching one never reads it. The `delivered` line exists precisely so
  that "not read" is visible rather than assumed.
- **Agreement is not convergence.** Two agents can agree on a wrong number. Every
  count in this example was re-derived by the side that did not produce it — that is
  the practice, not a property of the mailbox.

---

## Reproducing the recording

The animation is a real replay, filmed with gopi's own film driver — no ffmpeg, no
ImageMagick, no screenshot loop written by hand. The recording session is
`4d973459-6a04-4435-8015-8ea18fb097e5` (this side of the two-agent exchange), its
peer is `1c314185-f1d9-451b-91cb-035e8248b7a4`.

```console
$ gopi chat --replay 4d973459-6a04-4435-8015-8ea18fb097e5 \
      --film /tmp/film --out /tmp/replay.gif \
      --cols 48 --rows 23 --from-turn 74 --max-beats 12
[gopi film] display :64 (server pid 4066928), terminal xfce4-terminal … at 48x23 cells on it
[gopi film] the filmed terminal's face is 10x19 px per cell + 14x27 px border …
[gopi film] terminal window 2097155 ("gopi film film4") area 494x464+0+0, expected 494x464px …
[gopi film] the producer was released at the canvas this run decided (… chat --replay … --cols 48 --rows 23)
[gopi film] the producer wrote no new beat for 2m0s — the film ends here with the beats that were delivered (last beat 200)
[gopi film] film written to /tmp/film: 200 frame(s) from 200 ledger row(s), 200 frame(s) reached the settled criterion, 0 did not
[gopi film] animation /tmp/replay.gif: 200 frame(s) at 25cs (4.0 frame(s)/s), 4497036 byte(s)
```

Four facts, each of which cost a failed run to learn:

- **The geometry is the RECORDING's, not yours.** This session was recorded at
  `110x23`; `--rows 24` is refused outright (`the recording's height was 23`), and a
  width above the recorded one is refused too. The 500 px phone ceiling lands at
  `--cols 48` — the terminal face is 10 px per cell plus a 14 px border, so 48
  columns is 494 px.
- **A refused canvas exits 0 and writes nothing.** Verify the product, not the exit
  status: `ls -l` on the `--out` path, then read the header back.
- **200 beats is ONE turn.** The driver photographs every drawn beat, and one
  recorded turn holds ~200 of them — so the raw film of this session is 200 frames /
  4.5 MB, and its first frame is the session's *first* line, not the turn named on
  the command line.
- **`--from-turn` / `--max-beats` do not reach the film's producer.** The driver
  starts the producer with `--replay … --scratch … --cols … --rows …` only
  (`cmd/agent/chat_film_producer.go`), so those two flags are accepted, printed on
  no line, and silently ignored: the film always begins at turn 1 and runs to the
  200-beat cap. Measured, with the driver's own argv quoted, in the run's evidence
  file `from-turn-dropped.md`; filed as a bug row.

The animation on this page is therefore an **18-frame subsequence of the session's own
first turn** — the operator introducing the two agents, this side measuring, and the
peer's message arriving with `我接受你的修正 — 我的判据是假阳性` on screen. It is
composed from the frames that run wrote, with `gopi tool gif`, choosing beats
1, 20, 45, 80, 97, 105, 118, 122, 125, 126, 128, 133, 140, 145, 150, 160, 180, 200:

```console
$ gopi tool gif --frames "<beats 1,20,45,…,200 of /tmp/film/frame-*.png>" \
      --out examples/session-collaboration/replay.gif --delay 170
✅ 18 帧 → replay.gif  494x464 (每帧 494x464 + 文字带 0 行) delay=170cs loop=forever(0) … 字节=403547
```

`replay.gif` is **494x464, 403547 B, 18 frames, GIF89a** — header read back from the
file, not from the tool's own summary.

