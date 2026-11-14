<!-- doc-class: normative -->
# A film's canvas beyond the recording's is REFUSED before anything starts

**Task**: 01a127c2d9ec76ce9f99cc21f7317c03 · **Branch**: `task/01a127c2d9ec76ce9f99cc21f7317c03`

## The defect, measured

`gopi chat --replay <id> --film DIR --cols N --rows M` with `N`/`M` **above** the
terminal the replayed session was recorded at ended in a **silent** no-film:

```
$ ./bin/gopi chat --replay 62a7079b-3f59-4ce6-a0d5-ad5bef1a948e \
      --film /tmp/filmb-r1 --cols 113 --rows 25
exit status 1
$ ls /tmp/filmb-r1
frames.tsv  terminal-notices.log  terminal.log      # 0 frame-*.png, terminal.log 0 bytes
```

There **is** an error path (`no frame was taken: the filmed command never
delivered a beat`), and it fires. What it cannot do is say **why**, because the
producer's reason lives only in `producer.log`:

```
[gopi replay] this terminal is 112x25 but the beat protocol was asked for 113x25 —
  start `gopi chat --replay` at the geometry the driver films
```

Note the terminal suffix of the driver's own closing report: on a terminal that
keeps no session log, `evidenceHint()` prints *"This terminal captures no
producer output … its terminal's own words in /tmp/filmb-r1/terminal.log are the
whole record"* and `terminal.log` is **empty** — so the sentence points the
reader at a file that holds nothing.

## The mechanism (5-why)

1. **Why 0 frames?** The producer exited before its first beat, so no ledger row
   was ever written.
2. **Why did it exit?** Its own surface found its terminal was `112x25` while the
   beat protocol was asked for `113x25`, and refused by name
   (`startChatReplayBeats`, `chat_replay_beats.go`) rather than film a surface
   drawn at an unrequested canvas.
3. **Why did the driver ask for 113?** `--cols/--rows` is passed straight through
   to the producer's argv (`chatFilmProducerArgv`) and to the terminal's
   geometry — the film paints a 113-cell-wide terminal.
4. **Why is the surface at 112 then?** A replay's canvas IS the recorded size
   (`chatReplayApplyRecordedGeometry` publishes `session_start.terminalGeometry`
   as this run's ideal geometry), so the surface draws at 112 regardless of the
   terminal the film starts. **Two geometries, one picture** — the driver's
   window and the surface's canvas — and only the driver can see both.
5. **Why was that invisible?** The driver never compared them. It had the
   recording's geometry in hand before starting anything
   (`chatFilmRecordedGeometry`) and used it only as a *fallback*, never as a
   *bound*.

## The fix

`chatFilmGeometry` (`cmd/agent/chat_replay_geometry.go`) now reads the recording
**once**, before it decides anything, and refuses a **stated** count above it:

* the check runs **before the display server, the terminal and the producer** —
  the refusal now costs 0.02 s instead of a display server plus a terminal plus a
  producer;
* it is **per-axis**: a width above the recording is not fixed by lowering
  `--rows`, and the sentence names the axis it refused on;
* the value it prints as the remedy is the **recorded** count on that axis, not
  the recording's column count for a row refusal;
* only a **stated** value is checked. The recording's own size and the built-in
  ideal come from the recording or from the designed default and cannot exceed
  it, so neither is a candidate for this refusal;
* a recording with **no usable size** (an older session, an unresolvable target,
  a geometry recorded as unknown) has no boundary to exceed, so the run keeps its
  pre-existing fallback and states the reason.

`chatFilmGeometry` grew a third return value (`error`); `runChatFilmOut` reports
it and returns 1. Nothing else in the film path changed: the legal range
(`cols/rows ≤ recorded`) resolves to exactly the numbers it did before.

## What a reader sees now

```
$ ./bin/gopi chat --replay 62a7079b-… --film DIR --cols 113 --rows 25
[gopi film] --cols asked for 113, but the recording's width was 112 (recorded
geometry 112x25) — a replay draws its canvas at the size the session was
RECORDED at, so the surface refuses the terminal this driver starts and the film
would end with no frame at all. Use --cols 112 (or less), or re-record the
session at that size
$ echo $?
1
```

No directory is created, no display server is started, and the reason is on the
**film driver's own stderr** rather than inside a file the run only points at.

## Deliberately not done

* **`--rows` is NOT clamped to the recording.** Clamping would silently shoot a
  different picture than the one asked for; the whole point of the stated flags
  is that the operator's number is the picture. Refusing is the honest end
  (Rule 15).
* **The check is not applied to `--chat-ideal-geometry` / `GOPI_CHAT_IDEAL_GEOMETRY`.**
  Those state the *surface's* ideal, and a replay deliberately shadows them with
  the recording's size — so they are already harmless, and refusing on them would
  break a legal invocation.
* **The producer's own refusal was left exactly as it is.** It is correct, and it
  is still the guard for a surface that disagrees for a reason the driver cannot
  see.
