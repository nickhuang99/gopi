<!-- doc-class: normative -->
# `voice_input`: when the FIRST text arrived, measured from the tool's start

Task `01a120a7-2dd9-746e-9949-57c980188d9f` · branch `task/01a120a72dd9746e994957c980188d9f`

## The gap this closes

`gopi tool voice_input` printed `[voice] transcribed by doubao in 2.8s` — and 2.8 s
is the **engine's own** duration. It says nothing about what the operator waited
through, and the bare capture puts a whole fixed recording window in front of that
call. So the one figure the operator had was the one figure that was not the wait.

## The shape of the new line

```
[voice] first text at <T> (engine <E>) = <terms, joined with " + ">[: final transcript at <T2>]
```

- `<T>` is measured from the **process's own start** (`voiceProcessStart`, stamped in
  `init()`), not from the moment a function happened to run — a clock started at the
  top of the capture path would measure strictly less than the wait.
- The terms are the **capture window** (measured, not the requested `--seconds`), the
  **engine/channel**, and — only when a real remainder is left over — `X of startup +
  channel handshake`. The terms close the sum; a sub-floor remainder (100 ms) is
  noise and is not printed, so the sentence keeps meaning something.
- A run that measured **nothing** prints the honest sentence instead:
  `[voice] first text: none from engine doubao — <why> (the run waited 8.8s anyway: …)`.
  A duration is never printed as if a first text had existed.

## What "first text" means, and it differs by channel

| engine | the first-text instant |
|---|---|
| `recording` (vendor batch) | the engine's return — one answer for the whole recording |
| `streaming` (vendor v2 realtime) | the **first partial** — text while the audio is still going out |
| `local` (Fun-ASR) | the engine's return |

That is why the line labels the streaming figure `(first partial)` and prints the
final transcript's own instant beside it: "the first word came at 1.2 s and the
sentence closed at 3.4 s" is two facts.

## The measured comparison (same 12 s audio file, real vendor credentials)

Command shape (verbatim, `$A` = this run's session artifacts dir):

```bash
./bin/gopi tool voice_input --engine recording $A/fixtures/poem12s.wav
./bin/gopi tool voice_input --engine streaming $A/fixtures/poem12s.wav
./bin/gopi tool voice_input --engine recording --seconds 12 --device pulse:@DEFAULT_SOURCE@   # with poem12s.wav playing
./bin/gopi tool voice_input --engine streaming --seconds 12 --device pulse:@DEFAULT_SOURCE@   # with poem12s.wav playing
```

| run | first text | capture window | engine/channel | startup + handshake | final transcript |
|---|---|---|---|---|---|
| **file**, `recording` | 2.6 s | — | 2.6 s | — | = first text |
| **file**, `streaming` | **1240 ms** | — | **182 ms** | 1058 ms | 3.4 s |
| **mic 12 s**, `recording` | 14.6 s | 12.0 s | 2.5 s | — | = first text |
| **mic 12 s**, `streaming` | 14.7 s | 12.1 s | **558 ms** | 2.1 s | — |

### What the numbers say

1. **On a FILE, streaming wins by ~2.1×: 1240 ms vs 2.6 s.** The batch channel
   uploads the whole recording and polls for one answer; the real-time channel emits
   a partial while the audio is still going out (182 ms of channel latency).
2. **On the BARE capture the two modes are a wash (14.6 s vs 14.7 s)** — and that is
   the operator's question answered: the wait is dominated by the **fixed recording
   window**, not by the channel. The channel's own share is 2.5 s (batch) vs 0.56 s
   (streaming), i.e. ~2 s saved out of ~14.6 s.
3. **The third term is real and was invisible before.** On a file the split left
   ~1.1 s unaccounted for at first ("first text at 2.6 s: engine/channel 152 ms"), which
   is process startup plus the vendor channel's **handshake** — paid once per session
   and invisible in the per-packet latency the channel reports. It is named rather
   than quietly dropped, because a split that does not close its sum is arithmetic
   pretending to be a measurement.

## Where the code lives

- `cmd/agent/cli_voice_first_text.go` — the clock, the report struct, the ONE line
  renderer, the streaming-over-a-file pass.
- `cmd/agent/cli_voice.go` — `transcribeFileReport` is `transcribeFile` plus the
  report; the original signature stays and delegates (three callers, one of which
  wants the figure).
- `cmd/agent/cli_voice_local.go` — the bare capture starts the run clock before the
  device is looked for, measures the capture window, and fills the window term.
- `cmd/agent/chat_voice_live.go` — `firstPartialWall()` and `sendFailure()`: two
  accessors so the file-shaped streaming pass reads the production clock's stamps
  rather than re-deriving them.

## Known gap (filed as its own task)

The bare capture with `--engine streaming` still **records the full window and then
streams it** (`runVoiceCaptureLocal` → `voiceRecord` → `transcribeFileReport`). The
live arm that feeds the microphone packet-by-packet to `doubaostream` exists —
`chat_voice_live.go`'s live arm, over `openVoiceStagedStream` — but the tool's bare
shape does not route through it. So the 14.7 s figure is a *recording* + *streaming*
latency, not the *streaming* latency a live capture would show. Filed separately;
the fix is a route, not a new client (the seam and the feeder already exist).

## Red-first evidence

`cmd/agent/cli_voice_first_text_test.go` — 9 tests. The red path was demonstrated by
disabling the absence branch (`if false && (!r.measured() …)`) and re-running:
`TestVoiceFirstTextAbsenceIsNeverAZeroDuration` failed with
`report = "[voice] first text at 0ms (engine doubao) = engine/channel 0ms"` — the
exact Rule 15 defect the branch exists to prevent. Restoring it went green.
