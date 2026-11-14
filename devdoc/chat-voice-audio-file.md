<!-- doc-class: historical -->
# `/voice` over an audio file — the chat surface's channel onto the audio-source seam

Task `01a11f139919767ea288a557ae095c6e` (child of epic 01a11f13). The parent child
landed the seam (`cmd/agent/cli_voice_audio_source.go`: `voiceAudioSource`,
`voiceFileSource`, geometry 16k/mono/s16le, `voicePCM16ToWAV`, `voiceSourceWAVPCM`).
This one wires the **chat** surface onto it.

## Completed

- **`cmd/agent/chat_voice_audio.go` (new)** — the chat surface's channel:
  - `--voice-audio FILE` (spaced and `=` forms) + the environment channel
    `GOPI_CHAT_VOICE_AUDIO`, resolved by ONE reader, `chatVoiceAudioPath` (flag wins;
    Rule 2). A bare flag is a **usage error**, never a silent fall back to the mic.
  - `chatVoiceRecorder(path)` — the one construction site of the `/voice` recorder.
  - `chatVoiceFileDevice` — the "device" a file run carries: an ID that says what it
    is (`file`), and the entered row's sentence (`the file … — no capture device is
    opened`), built in one place so the window arm and the live arm cannot describe
    one run two ways.
  - `chatVoiceAudioRefusal` — the two command lines that were given a source and have
    no `/voice` mode to read it (a FILM is a driver; `--once` has no loop) are refused
    by name, each naming a route that works.
- **`chat_voice_recorder_staged.go`** — the recorder carries `audioSource`; `Select`
  branches to `selectFileSource` **before** any device enumeration/probe (that is what
  makes the mode work on a host with no sound card); both capture points go through
  `captureOptions()` → the seam's `voiceAudioSourceFor`; `transcribeOptions` puts the
  path on every round's options so the engine dispatch takes the FILE arm, never the
  device arm.
- **`chat_voice_live.go`** — the live arm's `openVoiceStagedStream` takes the same
  `captureOptions()`.
- **`cli_chat.go`** — the flag is in the valued-flag list (`chatResumeValuedFlags`),
  the accepted vocabulary, the usage page and the usage synopsis; the walk consumes it.
- **`cli_voice_audio_source.go`** — `voiceAudioSourceFor` is now **nil-safe** (see
  "defects found" below).

## Defects found and fixed (both measured by this run, not inferred)

1. **`voiceAudioSourceFor(nil, dev)` panicked** (nil pointer dereference on the
   options field). This was live on `master` after the parent commit: the chat
   surface's two capture points passed `nil`, so entering `/voice` on a real machine
   died at Select. Proven by a throwaway probe test that reproduced the panic, then
   fixed (`opts != nil` before reading the field) and pinned by
   `TestVoiceAudioSourceForToleratesNilOptions`.
2. **A finite source ended as a FALSE FAILURE.** `voiceStagedStream.flush` reported
   "produced no audio — the source is reachable but silent" whenever the final window
   was short — which is *every* file run, one window after a window it had already
   transcribed (measured: `[voice] 你好，这是第一句话。` then
   `[gopi] voice mode FAILED — the file … produced no audio`). The stream's own end is
   now reported as it is: `s.source != nil && s.received > 0` → `errVoiceStreamClosed`,
   the sentinel both of the mode's readers already honour (`chat_voice.go`'s loop,
   `recorderFailure`). The genuine "reachable but silent" (a device that delivered
   nothing, or a scripted test stream with no source) keeps the loud refusal.

## Not Done (honest)

- **A film cannot be driven by the FLAG** — a film is a driver whose producer is a
  second `gopi` inside a private terminal, so the flag cannot reach it. The refusal
  names the route that does work (`GOPI_CHAT_VOICE_AUDIO`, which the producer
  inherits) — the flag is refused, the environment is not, exactly as
  `chatFilmDumpRefusal` draws it. Verified both halves by hand (a flag on a film line
  is refused; a film line with the variable armed proceeds past that check and shoots).
- **`--once` cannot run the mode at all** (no REPL, no editor) — refusal, not support.
- The Real-time **pacing** of a file run is exercised in the real run, but the
  equivalence case is not wall-clock-pinned (a paced run's boundary can also be closed
  by the 6 s bound); the fixture's windows are content-closed, which is what makes the
  comparison stable.

## Verification

- Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./cmd/agent/` → clean.
- Tests: `make test-budget PKGS='./cmd/agent'` → see `## Evidence`.
- Ran (the real surface, verbatim — Rule 31):
  - `GOPI_CHAT_VOICE_AUDIO=<fixture13.wav> <termgrid.py> bin/gopi 120 30 script.txt`
    (a real PTY at the chat surface's ideal geometry; the script types `/voice`,
    submits, waits, photographs the grid).
  - `gopi-flag.sh`: `exec ./bin/gopi chat --voice-audio <fixture24.wav>` driven by the
    same harness — **the flag path**, not the env path.
- Result: `/voice` entered with no microphone and no sound card, and the mode's own
  row says so:

  ```
  [gopi] VOICE MODE ON — the file /…/fixture13.wav (16000 Hz mono signed 16-bit
  little-endian — no capture device is opened); transcribing with the doubao engine
  every 6s; each transcript is APPENDED at the caret …
  [voice] 你好，这是第一句话。
  [voice] 第二句。
  [voice] 导航去第一句话。
  [voice] no text in the last 6s window (silence, or the speech fell outside it) — still listening.
  [gopi] voice mode OFF (the capture ended) — the recorder is stopping; the text already on the line is kept.
  you> 你好，这是第一句话。第二句。导航去第一句话。
  ```

  The last row is the acceptance: **the transcripts are on the input line**. On the
  `/voice stop` arm the same run prints `[gopi] voice mode OFF (stop) — …`.
- Commit: the run's own branch, `task/01a11f139919767ea288a557ae095c6e` (the ledger
  address — Rule 6a names the task id as the branch, so `gopi task for-branch` can
  read the row back off the ref).

## Evidence (self-filed)

Persisted under the session directory (`$GOPI_TASK_REPORT_PATH`'s own directory),
`artifacts/`:

- `voice-entered.grid.txt`, `voice-live.grid.txt`, `voice-stopped.grid.txt` — the env
  channel run (termgrid shots, one per `shot` label).
- `voice-on-clean.grid.txt`, `voice-stopped-clean.grid.txt` — the mode ON row and the
  `OFF (stop)` row, from an otherwise fresh session.
- `flag-armed.grid.txt`, `flag-stopped.grid.txt` — the same, driven by
  `--voice-audio` (spaced form) instead of the environment.
- `fixture13.wav` — the fixture itself (16 kHz mono s16le, espeak + ffmpeg, three
  phrases separated by ~1.2 s pauses), so the run is reproducible.
- `budget-run.log` — the `make test-budget` stream.

## The de-dashed session id — ROOT-CAUSED 2026-10-10 (task 01a1222d)

A follow-up run reported an "anomalous, reproducible observation": a wrapper
script spelling the same wav path with the session id DE-DASHED
(`01a121fceebc73918785db2d149f931e`, the task-id spelling the ledger uses) got
`no such file or directory` for a file that `ls -l` showed. **It is not
anomalous and not a gopi defect.** On a real pty, two byte-identical wrapper
scripts differing only in that id segment:

```
--voice-audio /home/user/.gopi/sessions/01a121fc-eebc-7391-8785-db2d149f931e/artifacts/voicectl-11s.wav
    → [gopi] VOICE MODE ON — the file … (16000 Hz mono signed 16-bit little-endian, no capture device is opened)
--voice-audio /home/user/.gopi/sessions/01a121fceebc73918785db2d149f931e/artifacts/voicectl-11s.wav
    → `/voice` cannot start: --audio-source …: the DIRECTORY in that path does not exist (…) — did you mean …?
```

`stat` settles it: the DASHED directory exists, the DE-DASHED one does not, and
the only de-dashed directory under `~/.gopi/sessions` belongs to a different task
and holds a hand-made `artifacts/` alone. **Spell the session directory with its
dashes** — a session directory name and a task id are two different spellings of
the same 128 bits. The wrapper-script hypothesis is falsified; do not repeat it.

Refusals observed by hand (each verbatim on the command line, not from a test):

```
$ ./bin/gopi chat --voice-audio
gopi chat: --voice-audio needs a file — write --voice-audio FILE (16kHz mono s16le, or a WAV of the same geometry)
$ ./bin/gopi chat --once --voice-audio=/tmp/a.wav
gopi chat: --voice-audio /tmp/a.wav names the audio `/voice` reads, and this run is one-shot (`--once`): the voice mode is a REPL command, so it exists only in an interactive chat
$ ./bin/gopi chat --replay <sid> --film /tmp/f1 --voice-audio=/tmp/a.wav
gopi chat: --voice-audio names the audio `/voice` reads, and --film films a session: … set GOPI_CHAT_VOICE_AUDIO=FILE in the environment (the producer inherits it); or drop --voice-audio
```
