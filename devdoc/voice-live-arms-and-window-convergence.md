<!-- doc-class: historical -->
# Task Report: the live arms and the local window read converge — task 01a121fc (deliverable A)

## Completed

**1. ONE runner for a doubao-stream LIVE utterance — `cmd/agent/voice_stream_runtime.go` (new).**

Three shapes fed the v2 real-time channel and each carried its own transcription
of the same sequence — open the session, build the clock BEFORE it (the partials
start arriving on the reader goroutine the moment it is open), feed, wait for the
end, take the audio still in hand, send it with the end-of-stream flag, prefer the
feeder's send failure over the finish failure, report:

| shape | file | ends on |
|---|---|---|
| chat `/voice` live dictation | `chat_voice_live.go` (`Live`) | the finish key, or the capture running out |
| tool, file-fed first text | `cli_voice_first_text.go` (`voiceStreamFirstText`) | the file running out, or a budget |
| tool, microphone first text | `cli_voice_stream_mic.go` (`voiceMicStreamFirstText`) | the capture ending, or a budget |

They now all call `runVoiceLiveSession(stream, cfg)` — the session's open, the
clock, the feeder's lifetime, the end-of-utterance wait, the in-hand flush,
`Finish`, and the send-before-finish failure precedence, in ONE place. What stays
with the surface is supplied as a callback (`OnPartial`: draw the draft, or print
it) and as the stop/budget pair, because WHAT A PARTIAL MEANS is a property of the
surface and not of the channel.

The three copies had already drifted, and the drift is removed with them: the file
arm built its staged stream by hand — `&voiceStagedStream{source: src, chunks:
src.Chunks()}` — the one stream in the package with no source origin, no device and
no source verdict, and it opened the file source directly instead of through the
seam's own chooser. It now goes through `newVoiceStagedStream` (one construction of
the type) and `voiceFileSourceFor` (one source decision).

The session is an interface (`voiceLiveSession`: `Say`, `Finish`, `PartialCount`,
`FinishTimedOut`) for the same reason `voicePacketSayer` already was: the control
flow is the part with an obligation — nothing spoken may be dropped, the first
failure must be the one named — and an obligation with a network and a credential
in front of it is an obligation nobody tests. Production's implementation is
`*doubaostream.Session` and nothing else.

**2. The batch arm is NOT merged, with a mechanical reason (not a preference).**

`transcribeFileStream` (the batch arm of the streaming engine) walks the whole
audio buffer against a **CLIENT-OWNED clock** — `doubaostream.Transcribe`'s send
loop paces each packet with `time.Until(audioStart + n*chunkDur)` — because a file
is audio the service has not "heard" yet, and sending it early bills a stream
longer than the audio. A LIVE session has no pacing loop at all (`session.go`'s own
header: "a LIVE microphone IS the clock"). They are the two sides of the pacing
split, and everything this runner owns — waiting for a human to stop speaking, the
in-hand flush, the end-of-stream flag — has no meaning in a bounded call that
already holds the audio file. What the two genuinely share is the packet geometry
(`PacketAssembler`) and the protocol, and both had that before this change.

**3. ONE window schedule for the local engine — `cmd/agent/voice_window_schedule.go` (new).**

Under the local engine the console's `/voice` and the tool's `voice_input` read the
same microphone/file seam, and each had its own reader: the console read
`voiceStagedStream.Next` (a window plan, a carry, a span ledger, a `Drain`), the
tool drained the source into one buffer with its own byte bound and no ledger at
all (`voiceCapturePCM`). Now there is ONE schedule (`voiceStagedWindows`) and two
read shapes over it:

- `Next()` — one window; the console reads it round by round
  (`voiceChatRecorderStaged.readWindows`).
- `ReadBounded(want)` — the tool's one-block read, built on the SAME `Next`: the
  windows are cut by the same plan and the same carry, and only the stopping
  condition differs (the caller's byte bound). A non-positive bound means the whole
  source, so the tail is handed over by the stream's own flush rather than cut at a
  byte count — the tool gets the console's END behaviour too.

`A CUT IS NOT A CONSUMPTION`: the reader may run ahead of the engine (that is the
console's look-ahead, which is what stops a long window's engine call from making
the capture drift behind real time), so the schedule keeps the cut SEQUENCE and the
LEDGER separately — `Mark(span)` is the caller saying the engine has this audio, and
it is the only ledger writer, so "did the rounds cover the capture" is one list fed
by both shapes.

**4. The pacing decision has ONE home.** `voiceSourcePacing` (`voiceSourcePaced` /
`voiceSourceImmediate`) with the reason written beside it, and `voiceFileSourceFor`
as the named constructor; the four call sites no longer pass a bare `true`/`false`.
The console reads its file through the seam's own chooser (`voiceAudioSourceFor`),
so the two console arms and the tool cannot take different source kinds. The reason
the two shapes pace differently is mechanical: a shape whose bound is a WALL CLOCK
(the console's cut reacts to whether the audio arrived with a pause in it) needs the
capture's own rhythm, and a shape whose bound is a BYTE COUNT has no schedule to
protect — pacing a 30-second file would cost 30 seconds for no property at all.

**5. The regression case — `cmd/agent/voice_convergence_test.go` (new).**

`TestVoiceConvergenceToolAndChatReadTheSameWindows`: on one fixture, the console's
schedule and the tool's own entry point (`voiceCapturePCM`) must agree — same
audio byte for byte, the bounded read the head of the same sequence, the windows
tiling the recording, each window carrying the recording's own bytes for its own
span, a staged WAV each, and the schedule's ledger tiling the capture.
`TestVoiceConvergenceProcessAndFileSourcesAgreeThroughTheSchedule`: the process
source and the file source, driven through the schedule production now uses, cut
the same windows and hand the engine the same bytes.

**6. The structural evidence that there is now ONE cut.** `voiceStagedStream.Next`
— the raw window cut — is called from EXACTLY one place in production:
`voiceStagedWindows.Next` (`grep -rn '\.Next(' cmd/agent/*.go`, non-test). Every
reader in the package reaches the cut through the schedule, so "two devices cutting
the same capture differently" is not a thing the tree can express any more.

## Not Done

- **The batch arm's LIVE-side convergence** (above): stated with its mechanism, not
  merged. Whoever can run a real byte-counting peer against the v2 endpoint can
  re-open it; the case is about a client-owned clock, so it is decidable from
  `internal/doubaostream/client.go` alone.
- **`transcribeFileStream` still builds its own `doubaostream.Options`** and its own
  start/summary messages. Those are its surface's words, and moving them is a
  separate convergence with no measured defect behind it.
- **The console's live arm and the window arm still choose their own `captureOptions`**
  shape in `Live` (a device-only path); the file-fed live arm does not exist for chat
  (the file source reaches `/voice` through the window arm). Unifying the two chat arms'
  session construction is deliverable D's neighbourhood, not this one.
- **The `--seconds` bound of the tool reads the AUDIO, not a wall clock**, which it
  did before too (the old drain loop broke on the byte count); this report does not
  claim a behaviour change there, only that one reader now produces it.

## Verification

- Build: `go build ./...` — OK (also `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`).
- Tests: `make test-budget PKGS='./cmd/agent ./internal/doubaostream'` — the branch-scoped
  run for exactly the packages the diff touches. Its own line:
  `packages: 2 finished, 1 with a failure` / `stamp: 2026-10-10T05:19:37+08:00 host=hosta
  pkg=./cmd/agent ./internal/doubaostream budget=25m elapsed=845s … exit=1 outcome=fail
  stream=run-1791580777-2562333.json`. In that stream `cmd/agent` records 1214 pass and 25
  fail, `internal/doubaostream` 64 pass and 0 fail. **Every one of the 25 names fails
  identically at the base commit `926a71342` in a clean detached worktree**, checked by
  running the same `-run` regex there: 17 in the chat-prefetch/cooked/search family, 5 in
  the daemon/user-bus/merge family, 1 engine-precedence (see below). `comm -23` between the
  two name sets leaves only the three subtests of `TestSearchWrongGroundExitCodeOnTheRealBinary`,
  whose parent is red at base as well. The causes are the environment's (`ctags`/`rg` absent,
  no user session bus for `systemd-run --user`) or another surface's — none of those names
  appears in `git show --stat 469fb49d6`.
- **`TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings` is red because of THIS SHELL, not
  this change**: `GOPI_AUDIO_ENGINE=streaming` is exported in it, so the env rung outranks the
  settings file the test writes (the resolver's stated order is flag > env > settings), and
  the case fails at base `926a71342` the same way. `env -u GOPI_AUDIO_ENGINE go test -count=1
  -run 'TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings' ./cmd/agent/` → `ok … 0.997s`.
- Tests (the surface): `go test -count=1 -v -run 'TestVoiceConvergence' ./cmd/agent/` →
  `--- PASS: TestVoiceConvergenceToolAndChatReadTheSameWindows (0.01s)`,
  `--- PASS: TestVoiceConvergenceProcessAndFileSourcesAgreeThroughTheSchedule (0.00s)`,
  `ok github.com/nick/gopi/cmd/agent 1.111s`. Every `TestVoice*`, `TestChatVoice*`,
  `TestVoiceLive*`, `TestVoiceStaged*`, `TestVoiceWindow*` and `TestStatusVoice*` case in the
  branch-scoped run reports `pass` there too.
- Ran: `env GOPI_VOICE_ENGINE=local GOPI_AUDIO_ENGINE=local ./bin/gopi tool voice_input --audio-source
  /home/user/.gopi/sessions/01a121fc-eea7-71f2-a58e-db02637ffcca/artifacts/zh.pcm --seconds 6`
- Result: `[voice] captured 179712 bytes (5.6s of 16000Hz mono PCM16)` /
  `[voice] first text at 1166ms (engine funasr-nano) = capture window 15ms + engine/channel 1149ms` /
  `开放时间早上九点至下午五点。` — exit 0, with the audio read through the new window
  schedule.
- Ran (the two live shapes, real peers): `env GOPI_AUDIO_ENGINE=streaming ./bin/gopi tool voice_input --seconds 4`
  → `[voice] captured 128000 bytes (4.0s …) from parecord …` and the service's own
  verdict on silent audio (`code 1013 … no speech in this audio`, logid recorded),
  i.e. the runner's session opened, the feeder fed the live capture and `Finish`
  reported the peer's answer; and the real-pty `/voice` run below.
- Ran (chat `/voice` over a file, real pty): `python3 <session>/artifacts/drv.py
  <session>/artifacts/chat-file.raw <session>/artifacts/voice-file.script -- ./bin/gopi chat --voice-audio
  /home/user/.gopi/sessions/01a121fc-eea7-71f2-a58e-db02637ffcca/artifacts/zh.pcm`
  (script: `sleep 1.5`, `type /voice`, `submit`, `sleep 12`, `key esc`, `sleep 2`, `type /quit`, `submit`).
- Result: `VOICE MODE ON — the file … (16000 Hz mono signed 16-bit little-endian — no
  capture device is opened)` → `[voice] 开饭时间早上九点至下午五点。` on the stderr row
  and `you> 开饭时间早上九点至下午五点。` **on the input line** → `[voice] no text in the
  last 6s window … still listening.` → `voice mode OFF (the capture ended) — nothing
  is left to record; the text already on the line is kept.` The raw pty bytes are
  kept at the session's own artifacts path.
- Commit: this report's commit (the branch tip is `task/01a121fceee3717e821ac5cf10eed65d`
  — Rule 6d: `git log -1 --format=%H task/01a121fceee3717e821ac5cf10eed65d`).

## Evidence (self-filed)

- **Regression green**: `go test ./cmd/agent/ -run 'VoiceConvergence' -count=1` →
  `ok github.com/nick/gopi/cmd/agent 0.980s`.
- **RED PROOF (the file source one-shot burst)** — the shape change, not an import
  error: in `voiceFileSource.pump` the chunk geometry was replaced by
  `off += len(s.pcm)` / `end := off + len(s.pcm)` (one chunk = the whole recording),
  the tree rebuilt green, and the case run:
  `--- FAIL: TestVoiceConvergenceProcessAndFileSourcesAgreeThroughTheSchedule (0.00s)`
  `voice_convergence_test.go:215: the file source cut 2 window(s) and the process source 3 — the source changed the boundary`
  → `FAIL github.com/nick/gopi/cmd/agent 1.042s`. The proof patch was then reverted
  (from a copy kept at the session's artifacts dir) and the same command ran green
  again: `ok github.com/nick/gopi/cmd/agent 0.980s`. The claim "a boundary is decided
  against the ARRIVAL of the audio, so a source that changes shape changes the
  windows" is therefore measured, not asserted.
- **The ledger's semantics were caught by an existing case, not by this report**:
  recording every CUT in the ledger made
  `TestVoiceStagedReaderCutsWhileTheEngineTranscribes` red with
  `the ledger holds 4 windows for 3 rounds` — the look-ahead cut one window ahead of
  the rounds. That is the `Mark` design above, and the case is green after it.
- **The tool's own reading, byte for byte**: the run quoted in `## Verification`
  reported `captured 179712 bytes` for a 179712-byte fixture (`ls -l` on
  `zh.pcm` is 179712) — the bounded read neither lost nor invented audio.
