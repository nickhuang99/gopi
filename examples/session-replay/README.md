# Example: session replay & byte-exact verification

> Every command below was run on a real session. Outputs are verbatim, not mock-ups.
> Source session: `01a20a33-b6db-7cd2-8231-b2787f919587` (a real 61-request worker run).

An agent harness that forgets everything when the context compacts is a chat toy.
gopi writes every run to a **session** you can list, inspect, replay turn by turn, and
verify byte-for-byte. The recording is ground truth, not a story told after the fact.

## 1. List and inspect sessions

```console
$ gopi session list
50 session(s):
01a20a38-...  [run]  running  (pid 1182474, alive, started 2026-11-24 05:08)
01a20a33-...  [run]  ended    (pid 1090657, gone, started 2026-11-24 05:00)

$ gopi session show 01a20a33-b6db-7cd2-8231-b2787f919587
  mode        run
  state       ended
  model       deepseek-flash
  started     2026-11-24 05:00:57
  last write  2026-11-24 05:07:43
  task id     01a20a33-b6db-7cd2-8231-b2787f919587
```

## 2. Replay a run — and understand what a TURN is

```console
$ gopi session replay <id> --turns
session ... source=dump(interpreted, throttled) geometry=80x24 (fallback) turns=1
    1    turn 1/1  (61 request(s), 64 tool call(s))  rounds 1-61  calls=64 rows=4890
```

A **turn** is one complete input to the next: what the person typed, what `gopi notify`
delivered, or what the surface typed for itself. Every round and tool call in between
belongs to that one turn — the numbers inside a label are detail, never a second count.

```console
$ gopi session replay <id> --turn 1     # print one turn in full
$ gopi session context <id> --out ctx.md  # export one turn's context
context written to .../context-turn1.md (215888 bytes, 15 message(s))
```

## 3. Screen replay — byte-exact, with transport controls

`gopi screen-replay` writes a recorded screen's render bytes back out, **unchanged**
(cell grid + truecolour). Step frame by frame, or compare two recordings:

```console
$ gopi screen-replay <id> --step
gopi screen replay: press ENTER for the next frame, q then ENTER to stop

$ gopi screen-replay <id> --verify A B
  A: ...screen.stream — frames=142 bytes=11092
  B: ...screen.stream — frames=43  bytes=1237
differ: frame 4 - payload bytes differ (7 byte(s) each; first differing byte at 5)
  A: frame 4 at byte offset 188 - "\x1b[1;20r"
  B: frame 4 at byte offset 187 - "\x1b[1;22r"
```

`--verify` reports the **first differing frame with the exact payload bytes and
offset**. Not OCR, not a screenshot comparison — byte truth.

## Animated GIF

`session-replay.gif` — 4 frames, 463x492 px, 47 KB, built with `gopi tool text2img` +
`gopi tool gif` (pure Go). See `frames/make.sh` to reproduce (uses a monospace font so
equal columns give equal pixel widths — measure each frame before composing).
