<!-- doc-class: historical -->
# The /voice window cut rule — a pause needs a phrase before it, not a window

Task: `01a11b59`. File: `cmd/agent/cli_voice_window_ledger.go`.
Test: `cmd/agent/chat_voice_window_cut_rule_test.go`.

## What was wrong

`/voice` records into ONE continuous capture and cuts it into rounds
(`d53fc6ba8`). Where the cut lands is `voiceWindowPlan.cut`, and its candidate
filter was:

```go
if runStart >= p.minBytes && run > runLen {   // p.minBytes = 1500ms OF AUDIO
```

`p.minBytes` is `voiceWindowMinAudioMS` — the minimum window that is worth
transcribing. It counts the window's whole span, SILENCE INCLUDED, and it was
also being used as "how much speech must precede a pause before that pause may
close the window". Those are two different questions, and the second one is
wrong: a person dictating continuously speaks for about a second between breaths
(measured on the fixture, `silencedetect`: speech runs of 3.35 s / 4.82 s /
7.02 s, pauses of 200–400 ms), so with a 1500 ms floor no pause qualified.

Measured, real fixture bytes (`artifacts/probe-BEFORE-long16.txt`, 67.4 s of
Chinese speech, engine-independent — it drives the PRODUCTION `cut()` over the
recording):

```
window  1:   0.000s..  4.822s  ext=0 run=376ms
window  2:   4.822s.. 12.228s  ext=1 run=378ms
window  3:  12.228s.. 22.120s  ext=1 run=388ms
window  4:  22.120s.. 34.120s  ext=2 run=391ms
window  5:  34.120s.. 46.266s  ext=2 run=345ms
window  6:  46.266s.. 51.795s  ext=0 run=250ms
...
TOTAL extensions=6 splits=0 windows=10
```

Six rounds in ten spent the whole extension budget (12 s windows on a 6 s
bound), and every cut landed on the longest run the window happened to hold at
the moment the budget ran out — not on the pause that ended a phrase.

Where it is visible: the acceptance rig's own run of the pre-fix binary
(`artifacts/rig/runA-before/`, local engine, 18 s fixture + 75 s in `/voice`)
produced three rows for an 18 s recording,

```
[voice] 现在开始连续语音输入测试。
[voice] 请仔细听，今天是十月八日，我们在这台机器上做一次完整的手术验证。
[voice] 第一句语音识别的目标是把连续的说话变成连。
```

and the line those three rows built holds **21 characters of the 98** the same
recording yields in one piece (`gopi tool voice_input`, and the rig's own final
`you>` line): `现在开始…变成连` — 21.5 %, the tail of the sentence missing at the
cut.

## The rule now

```go
best := p.longestPause(pcm, p.minSpeechBytes, p.runBytes)   // speech floor: 1000ms
if best.run < p.runBytes && exhausted {
    best = p.longestPause(pcm, 0, p.runBytes)               // floor yields; the pause LENGTH stands
}
if best.run >= p.runBytes { return cut at best.start }
if !exhausted { return no cut, false }                       // extend the window
return cut at len(pcm), Split: true                          // nothing to land on
```

Three things changed and they are separable:

1. **The speech floor is its own constant** (`voiceWindowMinPauseSpeechMS`,
   1000 ms) instead of the window minimum. It is below the shortest phrase a
   dictating person speaks between breaths, which is the property that makes it
   able to fire at all, and it stays above a consonant's closure — a window with
   budget left still refuses to close on a sub-second gap.
2. **The floor yields when the budget is spent.** A pause taken early in a
   phrase is a better cut than the byte bound (the samples either side of it are
   quiet, so no word straddles it), so lowering the speech floor is preferred to
   cutting inside a word. The floor on the pause's own LENGTH does not yield: a
   gap shorter than `voiceCaptureTrailingSilenceMS` is still a consonant, and a
   cut there is reported as `Split` because it IS inside speech.
3. **A run that reaches the end of the buffer is exempt from the speech floor.**
   The floor exists to stop a window closing on a breath INSIDE a phrase, and
   such a breath is followed by more speech. When the operator's latest audio IS
   the pause there is nothing after it to wait for, so closing there loses
   nothing (the pause carries into the next window) while refusing to close
   spends the budget and ends inside speech. This is the branch the earlier rig
   measured as "window 3, 9 extensions, 58 s of a 12.455 s recording".

The scan is still per sample and still picks the LONGEST qualifying run: the cut
has to land on the pause's FIRST SAMPLE (a block-wise scan quantises the cut to
the block width — measured, 60288 where the pause began at 57600), and "longest"
is what makes the boundary the place the audio breathed most rather than the
first place it paused.

### The transition, measured

`artifacts/probe-BEFORE-*.txt` vs `artifacts/probe-FINAL-*.txt`, same bytes, same
production `cut()`:

| fixture | before | after |
|---|---|---|
| 18 s (rig recording) | ext=1, window 3 ended at the byte bound | ext=1, window 3 closes on a 0 ms run at 18.000 s |
| 18 s + 54 s of silence | 4 rounds, the last with **ext=8** (a 54 s round) | 1 extension total; the quiet tail cuts every 6 s |
| 1.3 s head + short pauses | — | boundaries at 1.300 s / 4.500 s / 7.700 s, each on a 200 ms pause |

`TILING OK` in every case: the rounds still cover the capture with no gap and no
overlap (`voiceWindowLedger.verify`), which is where a wrong cut would show first.

### What was deliberately NOT changed

**A window with no speech in it is not closed at its bound.** The first version
of this change did that (a "speechless window has no boundary to wait for, so the
round should not be spent"), and the rig reversed it with a measurement: on the
local engine the mode then appends a hallucinated sentence to the line every
6 s while the room is simply quiet —

```
[voice] 我想要一个完整的家。   × 5, in a row
```

— and on doubao it is a wasted request per bound. So the extension budget keeps
the old behaviour for that case, and a window that is finally taken from pure
silence is taken as a `Split`, which the notice reports. `voiceWindowCutKeepsASpeechlessWindowOutOfTheEngine`
pins it.

**The split notice stays.** `chatVoiceWindowSplitNote` ("window boundary cut
inside speech after %d extension(s)") is unchanged and still fires — the change
makes it rare, it does not remove it (`voiceWindowCutReportsASplitWhenThereIsNothingToLandOn`).

## Measured after the change: what it does NOT fix

Two real `/voice` runs on the operator's 67.4 s Chinese recording, serial (one
null sink, one engine), same script, same fixtures, pre-fix vs post-fix binary
(`artifacts/rig/runH-before-long16s`, `artifacts/rig/runI-after-long16s`):

| | rounds | line on submit | last 20 chars submitted |
|---|---|---|---|
| BEFORE | 11 | 231 chars | `十九。二十最后一句。测试到这里就结束了。` |
| AFTER  | 4 | 178 chars | `一次输字是在什么时候。现在从一数到二十。` |

**The loss moved; it did not go away.** Before, the middle of the recording was
mangled (`第二句：如果网购的边贴贴在此中间…` — that is the engine reading the
wrong audio, not a lost word) while the tail survived; after, the tail is the
part that is gone. Fewer, cleaner rounds (4 vs 11, boundaries on phrase ends)
and a *shorter* line is not a win, and this document does not claim one.

The reason is now measurable in the same table: the recording's speech runs are
3–7 s long, and the window bound is 6 s — so the bound expires MID-PHRASE in
almost every round, and no rule that only looks for pauses can land a 6 s bound
on a phrase end. The speech floor (1000 ms) then makes candidates *rarer* than
the old 1500 ms-of-audio rule did, which is why the post-fix rig takes FEWER
rounds (longer windows, `runI`) and why the earlier proposal to raise the floor
made boundaries worse rather than better.

**What follows**: landing the boundary on a phrase end for this shape is a
change to the BOUND or to when the window is fed — not to the pause criterion —
and it is filed as its own task rather than smuggled into this one.

## Not covered here (honest)

- **The rig's real `/voice` transcript only got one round.** The synthetic
  speaker plays 18 s and the mode records 6 s per round; the transcript the
  engine returns is short, so the run is a real end-to-end path but not a
  continuous-dictation one. The cut rule itself is measured over the operator's
  67.4 s recording instead (engine-free, the production `cut()` on real bytes).
- **The local Fun-ASR engine hallucinates on pure digital silence** — the same
  sentence, deterministically, for an all-zero PCM (`我想要一个完整的家。`), so
  "the engine transcribes silence as nothing" cannot be assumed anywhere.
- **`/voice` with the doubao engine dies on the first quiet window.**
  `20000003 no valid speech in audio` is surfaced as `voice mode FAILED`, which
  is the defect task `01a11b39` fixed — on its own branch
  (`task/01a11b39753c70babc91e0886bdb048c`) and **not on master**: the same rig
  run on this branch's binary reproduces it verbatim
  (`artifacts/rig/runC-before-doubao/07-off.grid.txt`). Any doubao acceptance run
  of this change is blocked on that merge, which is a separate decision.
