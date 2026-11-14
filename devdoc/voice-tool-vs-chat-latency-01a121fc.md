<!-- doc-class: historical -->
# The two file entry points to the voice engines, measured against each other

**Task** `01a121fc-eed0-744e-9975-99ad67069342` (epic 01a121fc child 3/5, deliverable C).
**Where** hosta (the only host with the sound card and the local engine), 2026-10-10.
**One fixture for both arms** — `voicectl-11s.wav`, 16 kHz / mono / signed 16-bit LE,
11.23 s, md5 `62902b1bf3cec6b18bb310e2d0e4e108`. A foreign geometry is refused by
name (measured: 44.1 kHz stereo → `is 44100 Hz, 2 channel(s), 16-bit — the capture
contract is 16000 Hz mono signed 16-bit little-endian …`, exit 1), so nothing here
is a resampled comparison.

The question this answers: `gopi tool voice_input --client --audio-source FILE`
and `gopi chat --voice-audio FILE` reach the same engines from the same bytes —
do they give the same numbers, and where do the numbers that differ come from?

## The table

`tool` = `gopi tool voice_input --client --audio-source <fixture>` (the CLI file
entry point, `--client` = capture on this host, transcribe on the local server
`127.0.0.1:7788`, `--engine local`).
`chat` = `gopi chat --voice-audio <fixture>` driven on a real pty, `/voice` typed
then Enter, `GOPI_VOICE_ENGINE=local GOPI_AUDIO_ENGINE=local`.

"first text" for the tool is the `[voice] first text` line's total (instant of the
first text, relative to the tool's own start — `cli_voice_first_text.go`).
For chat it is the console stamp of the first transcript row MINUS the console
stamp of `VOICE MODE ON` — the mode is what the chat path is; the CLI's clock
starts with the process, chat's with the mode. The two are stated as such, not
converted into each other.

### local engine (Fun-ASR-Nano over the two GGUFs), 3 runs each

| arm | end-to-end (wall, incl. surface startup) | end-to-end (from mode/engine start) | first text, from mode/engine start | engine's own duration | engine as a share of the wait |
|---|---|---|---|---|---|
| tool `--client --audio-source` | **1.11 / 1.12 / 1.13 s** | 1.11 / 1.12 / 1.13 s | **no `first text` line** (`--client` prints `transcribed in 1.1s`) | 1.1 s (server-side) | ~99 % |
| chat `/voice` | not meaningful (interactive) | **6.44 / 6.44 / 6.47 / 6.49 / 6.50 / 6.43 s** (6 runs) | **6.43–6.50 s** | 1.33 s (`funasr-nano`, from the probe below) | **~21 %** |

The tool's own local-file shape (no `--client`, the same fixture) reported, in
three runs, `first text at 1335ms / 1337ms / 1350ms … = engine/channel 1332ms/1334ms/1348ms`
— i.e. the total for a local file run IS the engine's inference, 1.33 s, with
`capture window` absent (the line omits the term entirely for a file run:
`voiceFirstTextReport`'s `WindowMS` is 0 because nothing was captured).

### streaming engine (Volcengine v2 real-time), 3 runs each

| arm | first text from the arm's own start | engine's own first-partial latency | the remainder |
|---|---|---|---|
| tool `--audio-source` (bare file run) | **2.8 / 3.3 / 3.6 s** (`first text at … (first partial)`) | **258 / 335 / 313 ms** | 2.5 / 2.9 / 3.3 s of startup + channel handshake, named by the report |
| chat `/voice` | **1.78 / 1.78 / 1.78 s** (3 runs, mode → first draft) | **545 ms** (probe, `FirstPartialMS`) | — |

The tool's identical run without `--client` is the whole comparison for
streaming: it consumes the file as fast as the transport allows (11.2 s of audio
in 2.8–3.6 s), and its `startup + channel handshake` term is that wall — the run
is still being fed WHILE the first partial arrives, and the report counts the
feeding as startup.

## Where the differences come from, pinned to the code

### 1. The file is paced in one shape and burst in the other — `newVoiceFileSource(path, rate bool)`

`cli_voice_audio_source.go:337`'s `rate` flag is the whole difference, and the
call sites are the evidence:

| caller | `rate` | line |
|---|---|---|
| `voiceAudioSourceFor` — the seam both chat arms reach | `true` | `cli_voice_audio_source.go:554` |
| the tool's bare local-file shape | `false` | `cli_voice_local.go:75` |
| the tool's `--client` shape | `false` | `cli_voice_client.go:1127` |
| the tool's streaming file shape | `false` | `cli_voice_first_text.go:300` |

With `rate` false, `pump()` (`cli_voice_audio_source.go:372`) `continue`s past the
sleep and the recording lands at once; with `rate` true it sleeps
`voiceAudioSourceChunkInterval` (100 ms, `voiceAudioSourceChunkMS`) per 100 ms
chunk, and the code states why: *"so the byte stream's SHAPE, not just its
content, is the one a microphone produces."* **The paced shape is the only one
that measures anything about an interactive arm**: the tool's 1.1 s for 5.0 s of
audio and its 2.8 s for 10.0 s of audio are both faster than real time, i.e. they
are throughput figures, not the wait a speaker experiences. Every chat number is
against a clock that ticks once per 100 ms of speech.

### 2. The chat clock starts counting DOWN a whole window before the engine exists — `chat_voice.go` + `chat_voice_recorder_staged.go`

`chat_voice.go:1319` calls `m.recorder.Round(m.options, voiceChatWindowSeconds)`
with `voiceChatWindowSeconds = 6` (`chat_voice.go:80`). `Round`
(`chat_voice_recorder_staged.go:265`) blocks on the hand-off from `readWindows`
(`:347`), which takes windows out of the live stream with `stream.Next(r.bound)`
(`cli_voice_recorder_staged.go:252`) — and `Next` arms `bound := time.After(seconds * time.Second)`
and does not return until a boundary is in reach. So the first transcript is
bounded below by the window length, and the code says it: *"a window boundary is a
decision made AFTER the operator stopped talking, so the transcript can only ever
arrive after the sentence is over"* (`chat_voice_live.go:10-20`, on why the live
arm exists).

Measured by probe over the production recorder, with the real local engine
(`Round` called directly on `chatVoiceRecorder`):

```
select +110µs  device="the file …/voicectl-11s.wav (16000 Hz mono signed 16-bit little-endian — no capture device is opened)"
round 1 wall=6.524s  span=[0,163412)     bytes=163412  text="开饭时间早上九点至下午五点。"
round 2 wall=5.546s  span=[163412,343124) bytes=179712 text="开饭时间早上九点至下午五点"
round 3 wall=0.844s  span=[343124,359424) bytes=16300   text="/sil"
round 4 wall=1.369µs                                            err=the voice capture stream was closed
```

The second row is one of the quiet findings: **`Round 2` costs 5.546 s although its
window closes at the 11.23 s mark**, because the `1 + voiceStagedLookaheadWindows`
windows the hand-off holds are all already full — the device was consumed in real
time while the engine worked, so the round's audio is in hand and its wall clock is
the ENGINE's. Add the same console effect to round 1 (6.52 s) and to the pace of
`Round` itself and the 6.44 s the console shows is exactly round 1 plus the ~30 ms
of line insertion. The 1.33 s the engine actually spends is 21 % of it.

### 3. The tool's `--client` shape pays the same round trip but not the window

`runVoiceClient` (`cli_voice_client.go:1127`) calls `voiceCapturePCM(src, opts.seconds)`
— a single reader (`cli_voice_audio_source.go:434`) that drains the whole source
into one buffer and then does ONE `voiceForwardHTTP` POST. One window, one
request, no boundary to wait for, and `opts.seconds` is the bound: `--seconds`
(default 5, `voiceClientDefaultSeconds`) **truncates the recording**, so the
comparison at equal audio must pass `--seconds` on the chat side too or accept
chat's default of the whole file (which is what this run did). The three
`--client` runs are the same number to 20 ms because there is no clock in them at
all until the engine runs.

### 4. The live arm's 1.78 s and the tool's 2.8 s are measured from DIFFERENT ORIGINS

This is the sharpest one, and it is a definition, not a defect.
`voiceLiveClock` (`chat_voice_live.go:395`) takes `firstPacket` from the **first
packet the feeder SENT** and `firstPartial` from the partial's arrival:
`FirstPartialMS = firstPartial - firstPacket = 545 ms` (probe). The tool's
`voiceFirstTextReport.FirstTextMS` is taken from `voiceRunOrigin()`
(`cli_voice_first_text.go:148`, a stamp taken in `init()`), i.e. from **process
start**. So:

* tool: **2.8 s from invocation**, of which the report itself names `258 ms` as
  the channel and `2.5 s` as "startup + channel handshake" — the credential
  read, the settings load, the WSS handshake, and the fact that the run is still
  FEEDING the file while the first partial comes back (11.23 s of audio sent in
  2.8 s).
* chat: **1.78 s from `VOICE MODE ON`**, of which `545 ms` is packet→partial and
  the rest is the mode's own startup (`Select`, the credential resolution, the
  handshake) — but the mode was armed BEFORE the timer's origin, so chat's figure
  never contains its own startup.

`voiceLiveClock`'s comment states the choice and defends it: *"an origin at the
session's open would fold the handshake and the device's own startup into 'how
fast did the text appear'"*. Both definitions are internally consistent and the
two numbers are NOT subtractable — the console stamp is what makes them
comparable at all, and it puts the live first draft at 1.78 s against the tool's
2.8 s process-inclusive figure.

The live arm's own console output is not evidence of anything yet: its only live
report row is written when the CARRIER implements the draft seam, and the chat
carrier does not (`voiceLiveSinkFor` → `refusingDraftSink`, `chat_voice_live.go:107`),
so `recordLiveReport`'s numbers are computed and then dropped. The `545 ms` above
comes from calling `Live` directly; the pty run shows only the line's text.

### 5. The local engine's own figure comes from a different branch of the dispatch

`transcribeFileReport` (`cli_voice.go:1425`) has three arms and they measure
different things: the streaming arm returns `FinalMS` as the engine duration and
`FirstTextMS` from the first PARTIAL, while the local and batch arms call
`voiceFirstTextFromEngine(elapsed, runClock)` — for them *"the first text IS their
answer"*, so `FirstTextMS` is the clock at the moment the engine returned and
`EngineMS` is the engine's own duration (`cli_voice_local.go:171`'s
`firstTextAt := runClock.since(engineDone)`). Measured consequence: a local file
run's total is 1.33 s and its engine term is 1.33 s, i.e. **the local arm has no
incremental latency to report** — one pass over the whole file, no partials. The
chat side never reaches this function at all: `chatVoiceRecorderStaged.transcribe`
(`chat_voice_recorder_staged.go:295`) calls `r.engineFor()` (the same
`transcribeFile`) but drops the second return value, so the local engine's own
duration is not available to the chat path and the `[voice] transcribed by … in
…s` line the CLI prints has no chat twin.

### 6. One line length, two behaviours (a surprise worth recording)

`voiceChatWindowSeconds = 6` is used both as the window bound (`chat_voice.go:1319`)
and in the operator-facing sentence, and for the streaming engine that sentence is
wrong: the live arm never reads a window (`chat_voice_live.go:24`: *"the window is
not tuned, it is ABSENT"*), yet `chatVoiceEnteredNote` tells the operator
*"transcribing with the streaming engine every 6s"*. The row is a wrong promise on
the live path — reporting defect, not a measurement one.

## What this does NOT claim

* The chat console's first-text instant is the moment a DRAFT was drawn, not the
  moment the text existed: the editor's paint is on the read loop. Measured upper
  bound on that gap: a window-resize (a full repaint, `SIGWINCH`) placed 3 s into a
  streaming run neither released an earlier draft nor changed the arrival stamp.
* Nothing here measures a microphone. Both arms read a file; the pacing in §1 is
  the only thing that makes the file's byte stream microphone-SHAPED, and a real
  device's open latency (`voiceCaptureGraceSec`, first-sample arrival) is out of
  scope for this table.
* The `1.78 s` chat figure is from a draft; its final transcript settles at the
  end of the capture, as the design requires (`FinalText` settles the draft).
