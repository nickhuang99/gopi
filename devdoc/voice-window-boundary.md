<!-- doc-class: historical -->
# Task Report: `/voice` window mode — the round boundary no longer drops speech (01a1188c)

## Completed

**The defect.** `gopi chat`'s `/voice` mode recorded ONE window per round with a
process PER round: it spawned `parecord`, read its bound (default 6 s), stopped the
recorder, transcribed, and only then spawned the next one. Everything spoken while
the engine worked (2-3 s per round) belonged to no window. The operator's own
recording lost 「测试，我声音完全流逝，」 exactly that way (parent task 01a11853).

**The fix, in two halves.**

1. **ONE CONTINUOUS CAPTURE** — `cli_voice_recorder_staged.go` (new). The recorder
   for window mode is opened once, when the mode is entered, and a reader goroutine
   consumes its stdout for as long as the mode is on. A round is the NEXT WINDOW OF
   THAT STREAM (`voiceStagedStream.Next`), so the microphone is never unread between
   rounds. The audio after a window's cut is **carried, never dropped**.

2. **THE CUT IS A BOOKKEEPING DECISION** — `cli_voice_window_ledger.go` (new). A
   window ends at the **longest pause it holds** (`voiceWindowPlan.cut`), not at a
   byte bound: real speech pauses ~0.24 s between phrases, so a rule keyed on "the
   first run longer than N" either never fires (every window cut mid-word — measured
   on the fixture) or fires inside a word. The run is scanned **per sample** (a
   block-wise scan quantised the cut 128 ms past the pause). A window with no
   qualifying pause is **extended** (bounded by `voiceWindowMaxExtensions`) rather
   than cut, and when the budget is spent the cut IS taken and the split is
   **reported** to the operator (`chatVoiceWindowSplitNote`).

**What changed at the seam.** `voiceChatRecorder` gained `Close()` (every exit path
releases the stream — `chatVoiceMode.shutdown`) and an optional
`voiceWindowReporter` for the split report. `cli_chat.go` now builds the mode over
`&voiceChatRecorderStaged{}`. The one-shot path (`voice_input --seconds N`) is
**deliberately unchanged**: an early close there would make "record 6 seconds"
quietly mean "record until you pause", and the measurement that closed 01a11853
(recording→captured = 6.05 s) would stop meaning anything.

**Care taken that the constant could fire on real audio.** The first version used a
700 ms silence run; the fixture's own longest pause is 0.38 s, so **no boundary
existed anywhere in that recording** and every window ran to its bound and was cut
inside a word — the defect itself. It is now 200 ms.

## Not Done

- **The `gopi chat` pty rig** (`~/.gopi/sessions/…/rig/voice-window-rig.py`) never
  drove `/voice` to completion: a pty is not a terminal emulator, so gopi's own
  terminal queries (`ESC]11;?`, `ESC[6n`, `ESC[c`) block it; answering them got the
  mode ON, but the run was abandoned in favour of the Go integration rig below,
  which exercises the same production path (`openVoiceStagedStream` + `Next` +
  the real engine) with a real card and real time. **The interactive `/voice` loop
  was not driven end-to-end on a pty in this run** — the mode's wiring is covered by
  the existing `chat_voice_pty_test.go` cases (which now also assert Close).
- **The transcript-vs-transcript comparison the parent task proposed** is reported,
  not asserted: the rounds' concatenation does not contain 「我声音完全流逝，」 in the
  runs above because the **local engine** transcribed that phrase as 「测试火山引擎的
  文相流式识别…」 — an ASR accuracy difference, not a boundary loss (the same engine
  reads the whole-utterance file the same way except for 「完全流逝」→「文相流式」).
  The acceptance measure is therefore **audio-level**, where this task made its
  change and where it can be exact.
- The rig does not assert a **third** window: the fixture's 12.455 s fits two
  windows at the 6 s bound. Three-window behaviour is covered by the unit cases'
  synthetic audio.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./cmd/agent/` → exit 0; `go vet ./cmd/agent/` → exit 0.
- **Tests:** `go test ./cmd/agent/ -run 'TestVoiceWindowCut|TestVoiceWindowLedgerVerify|TestVoiceStagedStream' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 1.005s`. `make test-budget PKGS='./cmd/agent'` → see Evidence.
- **Ran (Rule 31, the surface changed):**
  - The acceptance rig, real card + real time + real engine:
    `GOPI_VOICE_RIG=1 GOPI_VOICE_RIG_FIXTURE=…/rig/fixture-12.455.wav GOPI_VOICE_RIG_FIXTURE_PCM=…/rig/fixture.pcm GOPI_VOICE_RIG_DEVICE=pulse:q3rsrc GOPI_VOICE_RIG_SINK=q3r go test ./cmd/agent/ -run TestVoiceWindowBoundaryRig -v -timeout 200s`
  - The one-shot path, unchanged: `bin-gopi-staged tool voice_input --engine local --seconds 6` (real mic).
- **Result:**
  - Rig (rig-run-9.txt): `--- PASS: TestVoiceWindowBoundaryRig (19.53s)`. Round 1 = bytes
    0..168536 (5.267 s, ended on the speaker's own pause); round 2 = 168536..383200
    (5.267→11.975 s, same). `the windows TILE the capture exactly: 2 windows, no gap,
    no overlap, last byte 383200`. `AUDIO-LEVEL TRUTH: the rounds carry 93 of the
    fixture's 94 speech blocks (98.9%)`.
  - `voice_input --seconds 6`: `[voice] captured 192000 bytes (6.0s of 16000Hz mono PCM16)`.
- **Commit:** see the worktree's tip (`task/01a1188cbd4b784fa38d3b770ef52dba`).

## Evidence (self-filed)

- `make test-budget PKGS='./cmd/agent'` → the scoped package **PASSED**, measured from the
  run's own stream (`…/rig/test-budget.txt` quotes it): stream
  `…/queue/test-budget/run-1791416971-2519720.json`, 12570 events, **zero
  `Action=="fail"` events**, `{'github.com/nick/gopi/cmd/agent': 'pass'}`.
- Rig green: `…/rig/rig-run-9.txt` — the PASS line and the two windows quoted above.
  Earlier runs are kept as the trail: rig-run-3/6/7 (failures the fix then closed —
  the carry's offset, the tile check, the post-playback round).
- One-shot path measured: `[voice] captured 192000 bytes (6.0s of 16000Hz mono PCM16)`.
- Fixture: `rig/fixture-12.455.wav`, sha256
  `1271188d911262257076a6b25ff7e6fe66c2a6efa9448b1906342b9d2b52f466` (recorded beside it
  in `rig/fixture.sha256`).
- Audio-alignment note: PulseAudio resamples 44.1 kHz stereo → 16 kHz mono, so the
  captured samples are NOT byte-equal to the file (measured: sample 0 differs); the
  check is the speech envelope, which resampling preserves.
