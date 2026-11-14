<!-- doc-class: normative -->
# /voice reported "no audio within 10s" on a live device — the builder took the source's channel before the source had one

Task: `01a1215cdbf179678b17366d2fa90199` · branch `task/01a1215cdbf179678b17366d2fa90199`
Host: host-b (`host-b`, real sound card, `pulse:@DEFAULT_SOURCE@` live) · 2026-10-10

## Symptom

`gopi chat` → `/voice`: the FIRST round refused with

```
parecord from "pulse:@DEFAULT_SOURCE@" produced no audio within 10s — the source is
reachable but silent: check the capture level is not muted
```

while `gopi tool voice_input` on the SAME device worked, i.e. the device was not
silent and the refusal's own advice was wrong.

## Root cause

`newVoiceStagedStream` (`cmd/agent/cli_voice_audio_source.go`) read the source's
hand-off channel **before** opening the source:

```go
s := &voiceStagedStream{ source: source, chunks: source.Chunks(), done: … }
if err := source.Open(); err != nil { … }
```

`voiceStagedRecorderSource.Chunks()` returns `s.chunks`, and that field is
assigned **inside `Open()`** — `s.chunks = make(chan []byte, voiceStagedQueueChunks)`
— because the channel is the reader goroutine's hand-off and there is no reader
before the recorder process exists. The pre-`Open` read therefore captured the
field's **zero value, `nil`**.

`voiceStagedStream.Next` selects on `s.chunks`. **A `select` on a nil channel never
fires** — that case is simply never ready. So with the bound clock and the 10 s open
deadline as the only other ready cases, `Next` sat until the open deadline expired
and then reported "produced no audio within 10s" about a device whose samples the
reader goroutine had been delivering into the *other* channel the whole time.

### Why no existing test caught it

`voiceFileSource` builds its channel in `newVoiceFileSource`, i.e. **before** its own
`Open`. So for a file run the pre-`Open` read happened to be correct, and the seam's
equivalence case (`TestVoiceAudioSourceFileAndProcessAgreeOnWindows`) feeds **both**
arms through a scripted source that also builds its channel up front. Every test on
the seam was green while the production device path was dead: the case that would
have caught it is one whose source materialises its channel in `Open`.

## The fix (minimal — one statement moved)

```go
if err := source.Open(); err != nil {
	return nil, err
}
// THE ONE MOMENT THE SOURCE HAS A CHANNEL.
s.chunks = source.Chunks()
```

The invariant stated in the code: *a source's channel exists once it is OPEN*, so
that is when the builder takes it. Nothing else changed — no new constant, no new
file, no new dependency (Rule 35).

## Runtime evidence (deliverable 1)

Probe = the **production path** (`openVoiceStagedStream(nil, dev)` with
`dev.ID = "pulse:@DEFAULT_SOURCE@"`), reading the same-package fields directly, on the
real sound card. Logged verbatim at
`~/.gopi/sessions/01a1215c-dbf1-7967-8b17-366d2fa90199/artifacts/probe-postfix-device-run.txt`.

| probe | pre-fix (HEAD `2d9ee72c2`) | post-fix |
|---|---|---|
| P1 `source.Chunks()` before `Open` | `nil = true` | `nil = true` (unchanged — that is the contract) |
| P2 `builder.chunks` nil | **`true`** | `false` |
| P3 builder's channel vs the reader's | `0x0` vs `0xc000218380` → **IDENTICAL = false** | `0xc0001f6460` vs `0xc0001f6460` → **IDENTICAL = true** |
| P4 recorder argv | `parecord` | `parecord` |
| P5 `Next(2)` | after **10s** → **0 bytes**, `err=…produced no audio within 10s`, span `{Start:0 End:0 Run:0 Extended:4 Split:false}` | after 18.02s → **574592 bytes**, `err=<nil>`, span `{Start:0 End:574592 … Extended:9 Split:true}` |
| P6 `source.firstSample` | `23:53:40.908` — **non-zero, i.e. the reader HAD taken the first sample** | `23:56:11.907` |
| P7 `source.readErr` | `<nil>` | `<nil>` |

P6 is the clincher: the source had stamped its first sample while the builder stood
on a nil channel — the capture was live, only the builder could not see it.

`Next(2)` taking 18 s (rather than 2 s) is the pre-existing `Split` path — 2 s never
holds a `voiceWindowMinPauseSpeechMS` (1000 ms) pause after speech, so the bound is
extended up to `voiceWindowMaxExtensions` (8) times and the cut is taken inside
speech. Unchanged by this task.

## The 10 s vs 15 s budget (why they are not the same question)

They were never two budgets for one decision, so nothing needed reconciling:

- **`voiceCaptureGraceSec = 10`** (`cli_voice_client.go:155`) is the **first-sample**
  budget — how long a device may take to produce its *first* sample before it is
  called silent. The message above replays it verbatim
  (`cli_voice_recorder_staged.go:384`). It is shared with the one-shot path on
  purpose and is **spent only while the stream has produced none**
  (`Next` disarms `openDeadline` on the first chunk — that disarming is itself a fix
  of an earlier defect, noted in the code).
- **`voiceDefaultChunkSec = 15`** (`cli_voice.go:50`) is the `gopi tool voice_input`
  one-shot default for `--seconds`. It is an *audio length*, not a deadline, and the
  chat `/voice` mode never reads it — the mode's window bound is
  **`voiceChatWindowSeconds = 6`** (`chat_voice.go:80`).
- **10 is deliberate and stays.** The fix does not touch the grace: the defect was
  that the builder could never *use* the audio, not that the budget was too small.
  Raising it would have hidden the bug behind a longer wait on exactly the path where
  nothing ever arrives.

## Regression test (deliverable 3, Rule 10.5)

`cmd/agent/cli_voice_stream_channel_test.go` →
`TestVoiceStagedStreamTakesTheChannelOfTheOpenSource`.

It uses `lateChannelSource`, the **smallest** source that exhibits the rule: its
channel is built in `Open`, exactly as the production recorder source's is. The case
first asserts the probe source really is late (`Chunks() == nil` before `Open`), then
that the builder's channel **is** the source's, then that `Next` returns audio within
`voiceCaptureGraceSec` — the same 10 s the operator's refusal names, so the case
cannot be won by waiting.

Red → green, run explicitly (below): against the pre-fix `cli_voice_audio_source.go`
it fails on the nil-channel assertion; against the fix it passes in 0.00 s.

## Not Done / honest boundaries

- **`gopi chat` itself was not driven on a real tty.** Rule 31's requirement is met by
  executing the **changed surface** — `openVoiceStagedStream` + `Next` on
  `@DEFAULT_SOURCE@`, which is exactly what this fix and this test cover — but the
  end-to-end `/voice` keystroke path was not driven, because `gopi chat` needs a real
  tty and the in-chat bash stdin is `/dev/null` (the session's own constraint). The
  same probe on the pre-fix binary does reproduce the operator's literal message on
  the same device, so the chain from "these bytes" to "that refusal" is closed by
  measurement.
- The `Split:true`/18 s window shape on a 2 s bound is pre-existing behaviour, not
  addressed here.
- No other caller reads `Chunks()` before `Open` (checked): `voiceCapturePCM` opens
  first; `chat_voice_live.go` reads `f.stream.Chunks()` at runtime, after the stream
  exists.

## Verification

- **Build**: `go build ./...` → OK (clean, no output)
- **Tests**: `make test-budget PKGS='./cmd/agent'` → see the report's own close-out;
  the focused gates run explicitly below.
- **Ran (changed surface, real device, production path)**:
  ```
  go test -run TestZZProbeStagedStreamChunkIdentity -v ./cmd/agent/
  ```
  with a temporary in-package probe calling `openVoiceStagedStream(nil, &voiceDevice{ID: "pulse:@DEFAULT_SOURCE@", Backend: voiceDeviceBackendPulse, Name: "@DEFAULT_SOURCE@"})` and then `stream.Next(2)`.
- **Result**: pre-fix — `builder.chunks nil = true`, `IDENTICAL = false`, `Next(2) after 10s -> 0 byte(s)`, `err=…produced no audio within 10s`, while `source.firstSample = 23:53:40.908` (non-zero). Post-fix — `builder.chunks 0xc0001f6460 vs reader's source.chunks 0xc0001f6460 — IDENTICAL = true`, `Next(2) after 18.02s -> 574592 byte(s) err=<nil>`.
- **Ran (regression, red)**:
  ```
  git stash cmd/agent/cli_voice_audio_source.go   # pre-fix body
  go test -run TestVoiceStagedStreamTakesTheChannelOfTheOpenSource -v ./cmd/agent/
  ```
  → `cli_voice_stream_channel_test.go:101: the builder holds a nil channel: it read
  source.Chunks() before the source had one — Next will block past every deadline and
  the mode reports "produced no audio within 10s" about a live device`
  → `--- FAIL: TestVoiceStagedStreamTakesTheChannelOfTheOpenSource (0.00s)` → `FAIL`
- **Ran (regression, green)**: with the fix restored →
  `--- PASS: TestVoiceStagedStreamTakesTheChannelOfTheOpenSource (0.00s)` → `ok github.com/nick/gopi/cmd/agent 1.129s`
- **Ran (device enumeration, to name the device the probe used)**:
  `pactl list short sources` → `1 alsa_input.pci-0000_00_1f.3.analog-stereo … SUSPENDED` (the source `@DEFAULT_SOURCE@` resolves to on this host).

## Evidence (self-filed)

- `PROBE-3 builder.chunks 0x0 vs reader's source.chunks 0xc000218380 — IDENTICAL = false`
  (pre-fix, real device) — the reading this task is about.
- `PROBE-5 Next(2) after 10s -> 0 byte(s) … err= from "pulse:@DEFAULT_SOURCE@" produced
  no audio within 10s` (pre-fix) — the operator's message, reproduced by the probe.
- `PROBE-6 source firstSample = 23:53:40.908` (pre-fix) — non-zero, so the reader had
  taken audio; the builder could not see it.
- `PROBE-3 … 0xc0001f6460 vs 0xc0001f6460 — IDENTICAL = true` and
  `PROBE-5 Next(2) after 18.02s -> 574592 byte(s) err=<nil>` (post-fix, real device).
- `--- FAIL: TestVoiceStagedStreamTakesTheChannelOfTheOpenSource (0.00s)` on the
  pre-fix body; `--- PASS … (0.00s)` with the fix — the red-then-green pair.
