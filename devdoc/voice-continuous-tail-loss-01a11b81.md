<!-- doc-class: historical -->
# Task Report: /voice continuous dictation kept losing the tail — schedule + finish fix

Task 01a11b81 (parent 01a11b59 / commit a646ae573 equipped the cut point). Measurement
fixture: `long16.wav` = 67.4 s of synthetic speech (same fixture the parent task used),
played into a PulseAudio null sink (`q2b`) read back as `q2bsrc`, driven through a real
`gopi chat` under the existing pty rig. Runs were strictly SERIAL — parallel runs share
one null sink and one model process and produce empty transcripts.

## Completed

- **Baseline measured on master** (`gopi-master`, commit 41e59739). One-shot transcription
  of the whole 67.4 s segment = **253 chars** (215 CJK+alnum); the window ledger's ten
  windows transcribed individually = **231 chars** (215 CJK+alnum) — **ratio 0.913 raw /
  1.009 on speech characters**, i.e. *per-window transcription itself loses nothing*.
  So neither "the 6 s bound is too short for human phrases" nor "frames are lost": the
  tiling check (`voiceWindowLedger.verify`) reports the ten windows cover bytes
  0..215680 of the capture with no gap.
- **Root cause (A/B, minimal sample).** On master the mode ran **behind real time**: with
  the mode held open 145 s past the end of the audio, the row curve was
  `t+0s 0 rounds → t+15s 2 → t+90s 16 → t+150s 18` — i.e. 16 of 18 windows had landed
  ~97 s *after* a 67.4 s recording had finished, and the remaining two only landed because
  the mode stayed on. The cause is the schedule, not the recogniser: a round was
  *read window → transcribe → read next*, so the engine's own time (2.4–4.2 s per window,
  ~4 s of audio per window) was spent **not reading the microphone**. The operator's own
  Esc — the event that ends every real session — then closed the stream and **discarded
  the window in hand**: that is the "tail disappears" report.
- **Fix (zero dependency, inside gopi's Go code).**
  - `cmd/agent/chat_voice_recorder_staged.go`: a window-cutting **reader goroutine** runs
    ahead of the engine (`voiceStagedLookaheadWindows = 1`), so engine time is spent on the
    next window's recording; the span travels *with* the audio so the ledger stays in
    stream order.
  - `Drain` / `voiceChatFinisher` / `voiceChatFinishReporter`: the mode's exit path
    transcribes the windows already read **before** the stream is released, and a finish
    that ran out of budget is **reported** (`chatVoiceFinishShortNote`), never a silent
    truncation.
  - `cmd/agent/chat_voice.go`: `shutdown` waits for the loop to finish the capture before
    `recorder.Close()`, and skips that wait when the loop itself is the caller (a failed
    round leaves from inside the loop — waiting there would deadlock).
- **Regression tests** (`cmd/agent/chat_voice_pipeline_test.go`, scripted device at
  wall-clock pace + engine seam): `TestVoiceStagedReaderCutsWhileTheEngineTranscribes`
  (the schedule) and `TestVoiceStagedLeaveTranscribesWhatIsInHand` (the loss).

## Not Done

- **The doubao engine is still broken on master for quiet windows** (a window with no
  speech makes it FAIL). The fix lives only on branch `task/01a11b39` and was deliberately
  **not** merged here — reported for the operator to decide, per the briefing.
- **Behaviour beyond the fixture's length is inferred, not measured**: the 67.4 s fixture
  is the longest used; a multi-minute session was not rigged.
- A finish that runs out of budget still leaves part of the capture unasked (reported, not
  solved): the stream is live, so a reader inside a window whose audio has not paused yet
  takes until that window's bound.

## Verification

- Build: `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o gopi-after3 ./cmd/agent/` → OK
- Tests: `go test ./cmd/agent/ -run 'Voice' -count=1` → `ok ... 54.8s` (all `TestVoice*`,
  `TestChatVoice*`, `TestVoiceWindow*`, `TestVoiceStaged*` green)
- Ran: `bash artifacts/rig/run-rig.sh <bin> <shotdir> artifacts/chat-m0.script artifacts/long16.wav <tag>`
  — a real `gopi chat` in a pty, `/voice` entered, the 67.4 s fixture played into the null
  sink, one terminal dump every 15 s
- Result (same shot spacing, before/after):

  | build | rounds landed | submitted chars | last 18 chars |
  |---|---|---|---|
  | master (runC-M0) | 16 of 18 | 238 | `就结束了。感谢你嘅爱心。` |
  | fixed (runH-FIX-M0) | 18 of 18 | 260 | `/sil/sil我想要一个完整的家。` |

  The two rounds master never landed are the ones the fix's finish transcribes; the
  trailing `/sil` is the local engine's own silence marker for the fixture's tail.
- Commit: 8b0254dae `fix(voice): /voice keeps up with the speaker and finishes the capture on exit`

## Evidence (self-filed)

- Red-then-green, schedule: with the pre-fix `Round` (stream read directly) restored,
  `go test ./cmd/agent/ -run 'TestVoiceStagedReader' -count=1` →
  `FAIL: no window was ever waiting while the engine was transcribing ... (3 engine calls)`;
  with the fix → `ok ... 18.9s`. Saved: `artifacts/RED-pipeline.txt`.
- Red-then-green, loss: with `Drain` closing the stream first (the pre-fix teardown),
  `FAIL: 1 window(s) were in hand at the leave and the finish transcribed none of them`;
  with the fix → PASS. Saved: `artifacts/RED-finish.txt`.
- Tiling (no frames lost): `ab1/` one-shot whole-segment 253 chars vs ten windows 231
  chars (215 vs 215 speech characters, ratio 1.009); ledger verify reports no gap.
- `git status --porcelain` in the worktree: clean after the commit.
