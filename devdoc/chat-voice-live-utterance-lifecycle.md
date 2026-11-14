<!-- doc-class: historical -->
# Task Report: the live voice arm's utterance lifecycle — two failures, both measured, both fixed

Task `01a1204b-04b7-73c8-8f57-8e030e1f6635` (epic reframed at 2026-10-09).
Worktree `/home/user/workspace/gopi-01a1204b04b773c88f578e030e1f6635`, branch
`task/01a1204b04b773c88f578e030e1f6635`.

## Completed

### Symptom A — `[voice] stream: 0ms of audio sent` + `streaming asr: server: code 101`

**Reproduced, attributed, fixed.**

The failure is NOT in the audio and NOT in the streaming session. It is the mode
opening a SECOND utterance over a capture that has already ended.

The chain, each link observed rather than inferred:

1. `GOPI_CHAT_VOICE_AUDIO=<file>` is how the live arm is driven end to end, and a
   file is FINITE. Its pump closes the hand-off when the samples run out
   (`voiceFileSource.pump`, `cli_voice_audio_source.go`).
2. `Live` selects on the finish key and the feeder's `ended` channel — so a
   capture that runs out ends the utterance *exactly like a keypress does*
   (`chat_voice_live.go`). Before this change the two were indistinguishable to
   the caller.
3. `liveLoop` therefore looped and called `Live` again. `openVoiceStagedStream`
   built a NEW source over the same spent file, so the session got zero packets,
   sent the end-of-stream flag carrying no audio, and the service answered
   `code 101`.
4. The mode reported it through `fail()`:
   `[gopi] voice mode FAILED — doubao-stream engine: streaming asr: server: code 101`
   plus `[voice] stream: 0ms of audio sent` — on a run whose FIRST utterance had
   transcribed correctly.

**The zeros were honest; the failure was a phantom.** The service was right to
refuse an utterance that carried no audio, and the second utterance had no source
behind it.

**The fix** — two clauses that are one idea:
- `voiceLiveReport.CaptureEnded` records *which* of the two endings happened
  (`chat_voice_live.go`).
- `liveLoop` reports the transcript, then leaves the mode through the EXISTING
  `chatVoiceLeftByCaptureEnded` reason when the capture is what ended
  (`chat_voice.go`). It is an exit path the mode already had — no new reason, no
  new state — and its note already carries the right second half
  (`chatVoiceLeftRanOutTail`: "nothing is left to record").

The live arm is driven by a file, so the fix is what makes the file arm usable at
all; on a microphone the predicate can only fire when the device disappears, which
is the same case the pre-existing `chatVoiceLeftByCaptureEnded` sentinel exists
for.

### Symptom B — the finish key has no visible effect

**Quantified, refuted as a dropped key, fixed as a missing row.**

Ctrl+G is claimed and it works, and this was MEASURED before anything was
changed: with the key sent at t=10.139 the transcript landed at t=10.254 — a
115 ms round trip, the same round trip the no-key run pays (t=6.115 → t=10.25).
The key is not dropped and it is not delayed.

What was missing is the ACKNOWLEDGEMENT. The round's own numbers — `Nms of audio
sent`, the logid — are printed by the CLI's ONE-SHOT arm
(`cli_voice_stream.go`), never by the chat mode; `report.AudioMS` is recorded into
the mode's rows and read by nothing. So a working press and a dropped keystroke
produced the SAME picture on a real terminal until the transcript appeared. The
mode already had a row for the SECOND press (`chatVoiceLiveFinishingNote`, written
for exactly this ambiguity) and nothing for the first.

**The fix**: `chatVoiceLiveFinishSentNote`, written at the moment the gate TAKES
the key, naming the key through the existing `voiceLiveFinishKeyLabel` constant
(Rule 14 — one spelling, and its own test asserts the usage row and this note
agree).

## Not Done

- **The 29-minute delay, the duplicated text and the Ctrl+G byte landing mid-word
  seen in the driver** are ALL artifacts of the evidence harness, not of gopi, and
  no gopi change is claimed for them. `drive-live-chat.py` holds `/quit` until
  `t_total`, and the capture keeps running until then, so `/quit` typed at t=30.6
  was appended to a transcript still being produced — which is why the run shows
  `…meritoring in enemy` and a turn whose message body literally contains
  `…/quit`. The driver's own timeline (`t_sent_finish_key=10.139`,
  `t_sent_quit=26.744` in the reference transcript) is what showed the 29-minute
  figure to be a wall-clock mix-up between two scripts. Nothing in this report
  rests on it.
- **A REAL microphone was not used.** All runs drive the file source
  (`GOPI_CHAT_VOICE_AUDIO`). The file source is the arm's own supported input and
  its timing is paced to real time (`voiceFileSource.rate`), so the mechanics
  under test are the same ones — but "spoken into a real mic" is not claimed.
- **`FinishTimedOut` and the draft-latency numbers were not exercised.** They are
  untouched by the diff.
- **`cmd/voiceliveprobe`** — the standalone probe that first separated "the
  service refuses this audio" from "the client never sends it" — is NOT committed.
  It is evidence, kept with the evidence at
  `~/.gopi/sessions/01a1204b-…/artifacts/probe/main.go`. It exists to answer one
  question once; a permanent command in the tree for that would be a dependency
  (Rule 35).
- **A pty test in the Go suite was not added.** The two pty-driven runs live as
  raw output in the session's artifacts and are quoted below; encoding them as a
  `go test` would mean committing a driver that spawns a real `gopi chat` against
  a real network service, which is not a test — it is a run.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o …/artifacts/gopi-fixed2 ./cmd/agent/` → `BUILD_OK`
- **Tests**: red-then-green, same command both sides, only the fix toggled.
  - RED (the fix's two clauses neutered, tests in place):
    `go test ./cmd/agent/ -run 'TestVoiceLiveLoop_|TestVoiceFinishKey_ThePressIsAnswered' -count=1 -v`
    → 3/3 FAIL, each naming the defect:
    `chat_voice_live_test.go:252: the press was consumed and nothing named the key …`
    `chat_voice_live_test.go:432: the live loop did not return within the case's bound — it kept asking the recorder for another utterance after the capture had ended`
    `chat_voice_live_test.go:476: … (same, second case)`
  - GREEN (fix restored, the file byte-identical to the committed one apart from
    the comment lines the neuter touched):
    `go test ./cmd/agent/ -run 'TestVoiceLiveLoop_|TestVoiceFinishKey_|TestVoiceLiveDispatch_|TestVoiceLiveUsageNote_|TestVoiceLiveSink_' -count=1 -v`
    → `ok github.com/nick/gopi/cmd/agent 0.549s`, 8/8 PASS, including the three
    pre-existing finish-key cases unchanged.
  - `go vet ./cmd/agent/` → exit 0.
  - **Branch-scoped smoke** (the packages this diff touched — the merge/verify
    shape, never the whole tree):
    `go test ./cmd/agent/ -run 'Voice|voice' -count=1` → `ok github.com/nick/gopi/cmd/agent 211.774s`
    `go test ./internal/doubaostream/ -count=1` → `ok github.com/nick/gopi/internal/doubaostream 36.886s`
- **Ran** (verbatim; the binary under test is named by `GOPI_UNDER_TEST`, the
  driver by path, the audio by `GOPI_CHAT_VOICE_AUDIO`):

  ```
  export GOPI_CHAT_VOICE_AUDIO=/home/user/.gopi/sessions/01a11f13-9a70-7d0c-8609-049a90dce3c4/artifacts/fixture.wav
  export GOPI_UNDER_TEST=<…>/artifacts/gopi        # BEFORE, base binary
  python3 drive-live-chat.py run-no-key.out       none 8.0 30.0
  python3 drive-live-chat.py run-mid-audio-key.out 07  4.0 30.0

  export GOPI_UNDER_TEST=<…>/artifacts/gopi-fixed2 # AFTER, this diff's binary
  python3 drive-live-chat.py AFTER-no-key.out        none 8.0 30.0
  python3 drive-live-chat.py AFTER-mid-audio-key.out 07  4.0 32.0
  ```

  `drive-live-chat.py` drives the REAL `gopi chat` through a pty (via `script`),
  pacing every byte so the line editor does not classify the input as a paste,
  and records the arrival TIME of every read.
- **Result**: the same two invocations, before and after.

  BEFORE — key pressed at t=10.139, capture still producing audio (fixture is
  7.2 s and the file source re-seeks the arm's own boundary):

  ```
  t=6.059  [gopi] VOICE MODE ON — …
  t=10.139 t_sent_finish_key
  t=10.254 [voice] 你好你好！            ← the key DID work: 115 ms
  t=14.653 [voice] stream: no partial arrived before the final transcript
  t=14.653 [voice] stream: 0ms of audio sent, logid …, reqid …
  t=14.653 [gopi] voice mode FAILED — doubao-stream engine: streaming asr: server: code 101
  t=14.654 [gopi] voice mode OFF (a failed round) — the recorder is stopping; …
  ```

  BEFORE, capturing the mode's own `/voice status` row at t=6.060 and its exit at
  t=7.606 — the reference the harness's mix-up is measured against.

  AFTER — key pressed at t=10.162:

  ```
  t=6.082  [gopi] VOICE MODE ON — …
  t=10.162 [gopi] Ctrl+G — the audio in hand is being sent to the engine; the sentence appears on the line when it answers.   ← NEW ROW
  t=10.250 [voice] 你好你好！                                                       ← 88 ms after the key
  t=17.891 [gopi] voice mode OFF (the capture ended) — nothing is left to record; the text …
  ```

  AFTER, no key pressed at all — the same exit, reached by the source running out:

  ```
  t=6.147  [gopi] VOICE MODE ON — …
  t=13.492 [voice] 你好你好！                              ← the transcript IS still reported
  t=13.494 [gopi] voice mode OFF (the capture ended) — nothing is left to record; the text …
  ```

  Counted over both AFTER runs, in the cleaned text:
  `code 101` = **0**, `0ms of audio sent` = **0**, `voice mode FAILED` = **0**,
  `the capture ended` = 1 per run. The same counts over the BEFORE run with the
  key: `code 101` = 1, `0ms of audio sent` = 1, `FAILED` = 2 (the row and the
  duplicate render), `the capture ended` = 0.

- **Commit**: `3f539ca7a` — `fix(chat-voice): the live arm keeps the
  transcript and stops when its capture is spent`

## Evidence (self-filed)

- Reproduced A on the base binary, real service:
  `t=14.653 [gopi] voice mode FAILED — doubao-stream engine: streaming asr: server: code 101`
  beside `t=14.653 [voice] stream: 0ms of audio sent, logid …` — in
  `.../artifacts/run-with-key.out` (cleaned text also in
  `.../artifacts/run-mid-audio-key.out`).
- Isolated it from the audio: the probe sent the SAME 7227 ms of audio twice,
  opening its own session per round, and both rounds transcribed —
  `ROUND 1: packets=36 audioMS=7227 partials=26 text="你好你好！" err=<nil>` /
  `ROUND 2: … err=<nil>`. So the service does not refuse this audio, and the
  service does not refuse a second session: it refuses an EMPTY one.
- Quantified B before touching anything: key at `t=10.139` → transcript at
  `t=10.254` (115 ms), against a no-key run whose transcript lands at `t=13.419`.
- Red proof, observed: 3 named failures, quoted in `## Verification`.
- Green proof, observed: `ok github.com/nick/gopi/cmd/agent 0.549s`, 8/8 PASS.
- After proof, observed: `code 101` count **0** in both AFTER runs, and the new
  row `[gopi] Ctrl+G — the audio in hand is being sent…` at `t=10.162` with the
  transcript at `t=10.250`.
- The neutered red proof was a TEMPORARY edit (`if false && report.CaptureEnded`
  and the removed notice) made only to run the red side while keeping the tree
  compiling; it is fully reverted — `grep -c 'RED-PROOF NEUTER'` = 0.
