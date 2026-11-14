<!-- doc-class: historical -->
# Task Report: the bare capture's streaming engine is fed by the microphone, not by a finished recording

Task `01a12161-de40-75a4-8777-4f0042b63d6a`, host hosta, branch
`task/01a12161de4075a487774f0042b63d6a`, worktree
`/home/user/workspace/gopi-01a12161de4075a487774f0042b63d6a`.

## Why (the measured defect)

`gopi tool voice_input --engine streaming --seconds 12` on the BARE shape (no file
positional, no `--audio-source`) recorded the whole window first and streamed the
finished file:

```
[voice] first text at 14.7s (first partial) (engine doubao-stream) = capture window 12.1s + engine/channel 558ms + 2.1s of startup + channel handshake
```

(prerequisite task 01a120a7's `ev4-bare-streaming-mic.err`). The channel was the
FASTEST of the three engines — 558 ms — and the operator still waited 14.7 s
because 12.1 s of it was a recording in front of the channel. The
recording-vs-streaming comparison the parent epic asked for compared nothing on
that surface: both modes paid the same window.

## Baseline, stated first (Rule 6a)

The prerequisite task 01a120a7's branch had **not** been merged into my base
`2d9ee72c2` — `cmd/agent/cli_voice_first_text.go`, `devdoc/voice-first-text-latency.md`
and the process-start clock did not exist there. I fetched its branch from origin
and **ff-merged it into this branch as the first step** (`b0bd44d34`, plus
`1c6c5f7ab` which was on the same ref), because committing the report's own
extensions would otherwise collide by name. Every measurement in this report is
against the ff-merged tree; the diff against the base therefore contains both
tasks' work, and the part that is MINE is the four files listed under `## Files`.

## Completed

- **The routing** (`cmd/agent/cli_voice_local.go`): on the bare capture with the
  resolved engine `config.AudioEngineStreaming`, the run now takes the live arm
  BEFORE any recording is made (the choice IS "record a block, then transcribe it"
  versus "feed the live capture", so it is taken before `voiceRecord`). Every other
  case falls through to the recording path byte for byte: `--engine recording`,
  `--engine local`, a named `--audio-source`, and the file / `--client` /
  `--server` shapes.
- **The live arm** (`cmd/agent/cli_voice_stream_mic.go`): `voiceMicStreamFirstText`
  is the prerequisite's file-fed pass with the source swapped — the same
  `doubaostream.Open`, the same `voiceReportStreamStart`/`voiceReportStreamPartial`/
  `voiceReportStreamSummary`, the same **production feeder** `newVoiceLiveFeeder`
  and the same `voiceLiveClock` from `chat_voice_live.go`, the same end-of-stream
  tail discipline. Nothing was re-implemented: no second client (Rule 3), no second
  capture path (Rule 14), no second packet loop.
- **The one thing that had to be added is a route INTO the existing seam**: the
  staged builder takes the source's chunks channel ONCE, at construction, so a
  source swapped in afterwards leaves the feeder reading the recorder's channel and
  the watcher watching a channel nobody drains. The arm therefore composes the two
  calls `openVoiceStagedStream` is made of — `voiceAudioSourceFor` (the seam's own
  chooser, so the recorder argv and the device policy are the production ones) and
  `newVoiceStagedStream` — with `voiceMicStreamSink` between them.
- **The sink** (`voiceMicStreamSink`, same file): passes every byte of the capture
  through untouched, counts it, keeps it when `--record` asked for it, and **ends
  the capture at the operator's own `--seconds` bound** — which is what expresses
  the flag on a path that has no file: the recorder is stopped (through the stream's
  own `Close` → `stopVoiceRecorder`, so "we closed its window" stays classified in
  one place) and the feeder's end-of-stream path finishes the utterance with the
  audio in hand. No new timer, no new flag.
- **The report carries no capture-window term, as a measurement**: `report.WindowMS`
  stays at its zero value because no part of this total is a recording, and the
  existing renderer already names the window only when `WindowMS > 0` — so the
  shape is stated by the run's own arithmetic rather than by a second renderer.
- **A real end-to-end defect found while taking the evidence**: a zero-byte
  capture. `newVoiceStagedStream` read `source.Chunks()` before `source.Open()`,
  which is wrong for the recorder source (it builds its channel inside `Open`), and
  that is `1c6c5f7ab` on master — the sibling task 01a1215c's fix, which my
  worktree file predated. Fixed here too, together with the same ordering for
  `sourceOrigin` (`Origin()` before `Open` printed ` from "pulse:…"` — a refusal
  naming an empty binary; identity is taken after `Open` now, once, so no caller
  has to know when to ask).
- **Named constants (Rule 1b/13/14)**: the split's two term labels became
  `voiceFirstTextWindowTermLabel` / `voiceFirstTextEngineTermLabel` (+ their
  rendered `…Format` forms), because the streaming arm's property IS the absence of
  the first of them — the test reads the constant, not a string.

## Not Done (honest)

- **THE OPERATOR'S OWN SOURCE, BUT A FIXTURE FOR A VOICE.** This round's runs used
  the acceptance command's own device (`--device pulse:@DEFAULT_SOURCE@`, the
  ALC256 physical microphone) and played `poem12s.wav` into the default sink, so
  what the microphone captured was a speaker and not a person. The shape under test
  — a device opened through `pulse:`, fed to the live channel as it arrives — is the
  same either way; what is NOT covered is a human speaking to the beat of
  `--seconds` in a quiet room. The earlier session's runs used a virtual source
  (`pulse:gopi_pipe_rec`); both are recorded, and neither is a person.
- **`--seconds` still bounds the utterance by a clock** (the capture is ended at
  that much audio). A live operator therefore still has to say their sentence
  inside the window; there is no silence-detection end. That is the invocation's
  existing bound, and inventing a second one is a Rule 35 question this task did
  not open.
- **~4.2 s of the fixed total is startup + the one-per-session WebSocket
  handshake** (measured: `engine/channel 146ms` vs total `4.3s`). That is the
  prerequisite's accounting of what the channel costs; this task only removed the
  recording in front of it.
- The branch was **not merged** and its worktree was not removed — that is the
  merger's step, not mine (Rule 6e).

## Files

Mine (the four, in this branch; the rest of the diff is the ff-merged
prerequisite):

- `cmd/agent/cli_voice_local.go` — the routing point in `runVoiceCaptureLocal`
- `cmd/agent/cli_voice_stream_mic.go` — the live arm, the routing predicate, the sink
- `cmd/agent/cli_voice_stream_mic_test.go` — the branch-scoped contract tests
- `devdoc/bare-streaming-mic-live-arm.md` — this feature's own devdoc

Touched by the fix found during the run:

- `cmd/agent/cli_voice_audio_source.go` — the source's channel and identity are
  read AFTER `Open`
- `cmd/agent/cli_voice_first_text.go` — the split's term labels became constants

Evidence (this run's session dir, never in the worktree — Rule 37/30):

- `~/.gopi/sessions/01a12161-de40-75a4-8777-4f0042b63d6a/artifacts/evidence-bare-streaming-mic.md`
- `…/artifacts/ev-s-live.{out,err}` (streaming), `ev-r-live.{out,err}` (recording),
  `ev-s-record.{out,err}` + `recmic.wav` (`--record`), `red-proof.txt`

## Verification

**Rule 31 clause: what was actually RUN this session.** The report's own
acceptance line was re-run LITERALLY, on the operator's own source —
`pulse:@DEFAULT_SOURCE@`, which on hosta resolves to
`alsa_input.pci-0000_00_1f.3.analog-stereo` (the ALC256 physical microphone) — with
the 12-second fixture played into the default SINK so the microphone hears it
instead of the silent room (a speaker whose output the microphone picks up: no new
PulseAudio module, no virtual device, nothing resident after the run — the two
`pactl load-module` calls the earlier session used are NOT part of this tree or of
this round's run):

- `/home/user/workspace/gopi-01a12161de4075a487774f0042b63d6a` — this run's worktree
  (`git worktree list` → `89231fcea [task/01a12161de4075a487774f0042b63d6a]`); every
  command below ran with that worktree as CWD against the binary built from it.
- Session: this round recorded under
  `~/.gopi/sessions/01a12161de4075a487774f0042b63d6a/artifacts/` (Rule 37 — evidence
  outside the worktree). The earlier session's files live under the hyphenated
  `01a12161-de40-…` directory, and both are named where they are cited.

- **Build**: `CGO_ENABLED=0 go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests**: `make test-budget PKGS='./cmd/agent'` → see `## Evidence` for the observed
  tail; the branch-scoped new tests alone → `ok github.com/nick/gopi/cmd/agent 1.406s`.
- **Ran** (verbatim, from the worktree, `./bin/gopi` built from this tree):
  - **the acceptance command itself, on the operator's own source** (this round):
    - `./bin/gopi tool voice_input --engine streaming --seconds 12 --device pulse:@DEFAULT_SOURCE@`
    - `./bin/gopi tool voice_input --engine streaming --seconds 12 --device pulse:@DEFAULT_SOURCE@ --record /tmp/gopi-01a12161-rec.wav`
    - `./bin/gopi tool voice_input --engine recording --seconds 12 --device pulse:@DEFAULT_SOURCE@`
    - the fixture is played into the DEFAULT SINK 0.3 s before launch, so the
      microphone hears it through the speaker: `paplay <fixture>/poem12s.wav`
  - the earlier session's runs, on a virtual source:
    - `./bin/gopi tool voice_input --engine streaming --seconds 12 --device pulse:gopi_pipe_rec`
    - `… --record $A/recmic.wav`, `… --engine recording …` (same device)
    - `paplay --device=gopi_pipe <fixture>/poem12s.wav`, 2 s after launch
- **Result** (each exit 0, stdout the transcript alone):
  - **`pulse:@DEFAULT_SOURCE@`, streaming**: `[voice] first text at 1548ms (first partial) (engine doubao-stream) = engine/channel 467ms + 1081ms of startup + channel handshake; final transcript at 12.0s`
    — **no capture-window term**, 35 partials, capture `384000 bytes (12.0s …) from
    parecord from "pulse:@DEFAULT_SOURCE@"`, 12000 ms of audio sent. Repeated with
    `--record`: `first text at 1326ms … = engine/channel 240ms + 1086ms of startup +
    channel handshake`, `[voice] kept 384000 bytes of WAV (12.0s)` and the file on
    disk at 384 044 bytes, stdout still the transcript. Repeating the same two
    invocations gave 1.33 s and 1.55 s — the spread is the startup term, not the
    channel's.
  - **`pulse:@DEFAULT_SOURCE@`, recording**: `[voice] first text at 14.6s (engine doubao) = capture window 12.0s + engine/channel 2.5s` — the same renderer, the
    window still MEASURED and still reported there (12.0 s because the sink-fed
    capture starts filling immediately). On the virtual source the same command
    reported `17.8s = capture window 15.1s + engine/channel 2.6s`; the window is the
    source's own measured 15.1 s, not a constant.
  - streaming (virtual source): `[voice] first text at 4.3s (first partial) (engine doubao-stream) = engine/channel 146ms + 4.2s of startup + channel handshake; final transcript at 15.2s`
    — **no capture-window term**, 37 partials whose first arrived 1.05 s into the
    audio (while it was still being spoken);
  - red proof, three rungs (this round): base `2d9ee72c2` + these files →
    `undefined: voiceRunClock` … `FAIL … [build failed]`; `b0bd44d34` (prerequisite
    merged, routing not) + these files → `undefined: voiceFirstTextWindowTermLabel`
    … `[build failed]`; **controlled revert** of the dispatch →
    `--- FAIL: TestBareCaptureStreamingTakesTheLiveStream … it records the whole --seconds window first`,
    restored → `ok github.com/nick/gopi/cmd/agent 1.091s`. A build failure is not a
    red test, which is why the third rung is the one that counts.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → `BUILD-ALL-OK` (exit 0).
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0; `./bin/gopi version` → `v0.1.0 / go1.24.0 / linux/amd64`.
- `make test-budget PKGS='./cmd/agent'` → tail in this session's job-4 output (observed below).
- **This round, the acceptance device (`pulse:@DEFAULT_SOURCE@`):**
  - `./bin/gopi tool voice_input --engine streaming --seconds 12 --device pulse:@DEFAULT_SOURCE@` →
    `EXIT=0`; stderr ended `[voice] captured 384000 bytes (12.0s of 16000Hz mono PCM16) from parecord from "pulse:@DEFAULT_SOURCE@"` then
    `[voice] first text at 1548ms (first partial) (engine doubao-stream) = engine/channel 467ms + 1081ms of startup + channel handshake; final transcript at 12.0s`;
    stdout was the transcript alone.
  - `… --record /tmp/gopi-01a12161-rec.wav` → `EXIT=0`; `[voice] kept 384000 bytes of WAV (12.0s)`;
    `ls -la` → `-rw-r--r-- 1 nick nick 384044 Oct 10 00:28 /tmp/gopi-01a12161-rec.wav`; stdout still the transcript.
  - `… --engine recording …` → `EXIT=0`; `[voice] first text at 14.6s (engine doubao) = capture window 12.0s + engine/channel 2.5s`.
  - Artefacts: `~/.gopi/sessions/01a12161de4075a487774f0042b63d6a/artifacts/{evA-bare-streaming-mic,evB-bare-recording-mic,evC-bare-streaming-record}.{out,err}`,
    red proofs `red-proof-routing-revert.txt`, `red-proof-baseline-tree.txt`, `red-proof-prereq-only.txt`.
- The earlier session's lines (virtual source `pulse:gopi_pipe_rec`), kept because they
  are the same properties on a second capture path:
  - `… --engine streaming --seconds 12 --device pulse:gopi_pipe_rec` → `EXIT=0`,
    `[voice] first text at 4.3s (first partial) (engine doubao-stream) = engine/channel 146ms + 4.2s of startup + channel handshake; final transcript at 15.2s`; stdout was the transcript.
  - `… --record …/recmic.wav` → `EXIT=0`, `[voice] kept 384000 bytes of WAV (12.0s) in …/recmic.wav`,
    `ls -la` → `-rw-r--r-- 1 nick nick 384044 Oct 10 00:21 …/recmic.wav`.
  - `… --engine recording …` → `EXIT=0`, `[voice] first text at 17.8s (engine doubao) = capture window 15.1s + engine/channel 2.6s`.
- `git status --porcelain` in the worktree → printed nothing at commit time (the
  close-out measurement carries the final reading).
- `./bin/gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every
  file under devdoc/ has a well-formed row` (exit 0; the new devdoc's row landed in
  the same commit — Rule 36).
- The muted-device refusal (Rule 15, regression included so the FIFO cannot hide
  address changes) is covered by the branch-scoped test file; see its `## Verification`.

## Commit

The deliverable is the BRANCH `task/01a12161de4075a487774f0042b63d6a`, not a push
(Rule 6c: a worktree shares the main repo's refs, so no push is needed). Its
commits, by subject — `git log --oneline <branch>` is the authority:

1. `feat(voice): report when the FIRST text arrived, measured from the tool's own start`
   — the ff-merged prerequisite task 01a120a7 (Rule 6a), read from origin, not mine.
2. `feat(voice): the bare capture feeds the streaming channel as it is spoken` — the
   deliverable: the routing, the live arm and the sink.
3. `docs(voice): state what the red proof actually showed, and record the bare-mic run`
4. `docs(voice): name the branch tip, the recovery command and the ref's descent (Rule 6d)`
5. `docs(voice): cite no tip hash in the report — the commit that lands a hash invalidates it`

**Declaration (Rule 6d), so the work survives a cleaned-up worktree:**

- **After** the documentation commits land, the count is checked the way a reader
  can re-check it — the number is NOT written here, because the commit that lands
  this sentence changes it. Run it:

  ```bash
  git rev-list --count master..task/01a12161de4075a487774f0042b63d6a
  git merge-base --is-ancestor master task/01a12161de4075a487774f0042b63d6a  # exit 0 = descends from master
  ```

  (Measured while writing this sentence: `count` → 5, `is-ancestor` → exit 0, with
  `master` at `2d9ee72c2` — the base this branch was created from.)
- **No tip hash is named here on purpose**: any hash written into this file is
  invalidated by the commit that lands the correction, and writing one invites a
  reader to check a ref that must already be gone. `git log --oneline
  task/01a12161de4075a487774f0042b63d6a` is the authority, and the earlier draft's
  `a5d7f49ea` names a commit that exists only in this branch's reflog. The ledger
  owns the branch (`gopi task for-branch …` → `task
  01a12161-de40-75a4-8777-4f0042b63d6a status=in_progress`, worktree and branch named).
- recover this report at any time with
  `git show task/01a12161de4075a487774f0042b63d6a:devdoc/bare-streaming-mic-live-arm.md`
  (from the main repo `/home/user/work/GoPi` — no checkout needed, Rule 29).
