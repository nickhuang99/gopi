<!-- doc-class: historical -->
# /voice keep-up at five minutes: the offset is structural, not a drift

Task `01a11bcb` (validation of `01a11b81` / commit `8b0254dae`). The report, the six
runs' artifacts and the instrument live under
`~/.gopi/sessions/01a11bcb-de2e-7631-a5f9-15e3eb35db2f/evidence/`; this note is the
part a future reader needs before re-measuring anything in `/voice`.

## The shape of the answer

The fix is validated on the five-minute fixture. Measured over all 62 live 5 s shots, the
fixed binary's lag is **median 1.44 windows (5.9 s), p90 3.09, worst 3.75**; master's is
**median 2.72, p90 6.70, worst 7.86 (32.3 s behind the speaker)**. The decisive number is
the TREND: the fixed median is **1.42 windows in the first half of the live window and 1.47
in the second (flat)**, master's **2.15 → 3.67 (drifting)** — master's growth IS the
not-keeping-up signature. The lag is a STRUCTURAL OFFSET:

```
lag ≈ voiceStagedLookaheadWindows × window_seconds + engine_seconds
```

`voiceStagedLookaheadWindows = 1` and `voiceChatWindowSeconds = 6` are both in the
tree; the engine is 2.4–4.2 s per window on this host. Two independent instruments agree:

- **The landed-text clock** (`latency.py`, all 62 live shots) — fixed median **1.44 windows
  / 5.9 s**, p90 3.09, worst 3.75; master median 2.72 / 11.2 s, p90 6.70, worst 7.86.
- **The anchor method** (`anchors.py`, no fitted rate: each rep's opening marker sits at a
  known audio offset) — fixed **7.4 / 3.4 / 4.7 / 6.1 / 7.4 s**, flat; master 7.4 / 8.8 /
  10.1 / **27.7** / 7.4 s.

Both say the same thing: the fixed build's lag does not grow with the recording's length,
and master's does. At the audio's end both builds owe 5 rounds (the fixed run: four speech
rounds plus the post-audio `/sil`); all speech rounds reach the line inside 8 s of the stop.

## What a future reader must not conclude from the A/B

**Both builds land a similar number of rounds on a PAUSY fixture** (master 80 speech
rounds / 1194 chars, fixed 84 / 1193 — the same five minutes, transcribed). That is not
"the fix does nothing"; the separation is in the LAG (above), not in the count. A window closes on a
pause, so a scripted dictation with a 1.5 s inter-phrase gap gives the SERIAL shape
enough slack to stay roughly abreast — the defect the parent measured lives on the
other shape, where windows must be cut at the 6 s bound and the serial loop spends
each round's engine time reading nobody. Two fixtures, two regimes:

| fixture shape | serial (master) | look-ahead (fixed) |
|---|---|---|
| cut at the 6 s bound (unpaused speech), parent's `long16` × N | ~0.6× real time, backlog grows | keeps up |
| pausy dictation (this fixture), windows close on the pause | roughly abreast | abreast, with a smaller, flat lag |

So **do not use a pausy fixture to argue the fix is unnecessary**, and do not use one
to argue it is dramatic. Use it for what it is good at: a five-minute run that fits a
session budget and shows the offset does not grow.

## The four instrument mistakes (all measured the hard way)

1. **Do not count rounds off the rendered grid.** The console SCROLLS; a 28-row grid
   holds the last rows only, so the count *descends* (20 → 17 → 15 → 14). Count from
   `termgrid.py`'s `<label>.raw.txt` — the cumulative byte stream written to the pty up
   to that shot.
2. **Do not dedupe globally.** The answer band rewrites the row it shares with a
   scrolling transcript, so a row is repainted and one phrase appears 2–4 times.
   Collapse only *consecutive* repeats; a global dedupe folded this fixture's 82 rounds
   (5 identical reps!) down to 28.
3. **Do not read the input line as the measurement.** The line WRAPS and SCROLLS inside
   the band (`… 12 lines above`), so the last `you> ` write carries only the visible
   window — it read 660 characters against a true 1197. Sum the transcript rows instead;
   a row's presence *is* the insertion.
4. **Cut the `you> ` payload at the next paint.** The status row is written after the
   payload with no newline between them (CSI positioning), so a naive regex returns the
   line plus `… PgUp/PgDn scroll · deepseek-flash · …`.

## Runs are SERIAL — this is not a preference

A second rig shares the null sink and the model process and yields EMPTY transcripts
(parent memory #8822/#8417). Every run here waited for the previous one's
`rig all done` line before starting; the queueing lived in the job script, not in a
new dependency.

## The two constants, re-measured

- `voiceStagedLookaheadWindows = 1` — sufficient. +1 second of audio buffered against
  ~2.9 s of engine time absorbed; a deeper queue only holds MORE audio for a leave to
  discard.
- `voiceStagedDrainWait = 8 s` — held at every leave measured (both binaries, including
  one at 97 s of audio). The drain needs `(lookahead + 1) × engine ≈ 5.8 s`. **The
  worst case is not covered**: a leave while the reader is mid-window adds the rest of
  that window's bound (≤ 6 s) on top of an engine call (≤ 4.2 s) → ≈ 10.2 s > 8 s. No run produced
  that case, so this is a **proposal (12 s), not a defect report** — `voiceChatFinishWait`
  (30 s) is the outer bound and is unaffected.

## Reproducing it

```bash
EV=~/.gopi/sessions/01a11bcb-de2e-7631-a5f9-15e3eb35db2f/evidence
ffmpeg -f lavfi -i anullsrc=r=16000:cl=mono -t 1.5 -c:a pcm_s16le gap.wav     # + concat 5 × long16
python3 $EV/rig/termgrid.py $EV/gopi-fixed 110 28 $EV/rig/fine.script         # one shot per 5 s
python3 $EV/analyze.py $EV/run-fine $EV/run-fine/rig.log                      # the curve
python3 $EV/anchors.py $EV/run-fine                                           # the lag, no fitted rate
```
