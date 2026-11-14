<!-- doc-class: normative -->
# Voice capture failure-mode matrix

**Scope**: the failure modes of the gopi voice-input *capture* path — the half
that produces audio (as opposed to the engine half, which transcribes a file).
Feature spec: `devdoc/SPEC-voice-input-phase23.md`. Device inventory that this
builds on: `devdoc/voice-hw-matrix.md` + `scripts/voice-mic-probe.sh`.

**Implementation**: `cmd/agent/cli_voice_capture.go` (client + classifier),
`cmd/agent/cli_voice.go` (flags/usage), `cmd/agent/cli_voice_capture_test.go`
(regression tests), `scripts/voice-capture-failmatrix.sh` (scripted repro of all
five cases).

**Why this exists**: before this change the capture path did not exist in the
binary at all (`grep -rn 'arecord' --include=*.go` → 0 production hits), so every
failure mode below was a live hazard for the client that was being written
elsewhere: a device-less host, a busy device, a device that vanishes, a device
that cannot honour the pinned contract, and a muted input. The last one is the
expensive one — a muted microphone produces a *perfectly valid* 60-second
recording of zeros, which costs a full engine run to transcribe as nothing.

## The pinned contract

16000 Hz, mono, S16_LE, headerless PCM (`voiceCaptureRate/Channels/Format`,
`voiceCaptureBytesPer` in `cli_voice_capture.go`). Every failure mode below is
judged against it: a device that cannot produce this must fail *before* the
engine sees it, because 44.1 kHz stereo is not an error downstream — it is
garbage text (the sibling design doc measured this: `voice-input-design-space.md`).

## Outcome vocabulary (the cross-language contract)

`captureFailureMode` values in `cli_voice_capture.go`; the shell harness and this
document assert on these exact strings, and
`TestVoiceCaptureModeVocabulary` pins them so a rename cannot pass silently.

`ok` · `silent` · `no_device` · `no_soundcards` · `permission` · `busy` ·
`unsupported_params` · `pulse_refused` · `hot_unplug` · `timeout` · `error`

Repro for all five cases on one command line:

```bash
scripts/voice-capture-failmatrix.sh --bin ./bin/gopi          # exit 0 iff every exercised case matched
scripts/voice-capture-failmatrix.sh --case a,b --out /tmp/fm  # a subset
```

The harness prints one `PASS|FAIL|SKIP` line per assertion plus a TSV summary.
It **skips, never passes**, a case whose condition this host cannot stage, and in
that case falls back to asserting the *classifier contract* from the repo against
the recorded real-world stderr (`assert_classifier_contract`).

---

## Case (a) — no capture device present

| | |
|---|---|
| Repro | on the capture-less node: `gopi voice_input --client --device default --seconds 3` ; and `--device gopi_no_such_device_xyz` on any node |
| Red (before) | no capture client existed; the only entry points were file/server modes |
| Green (now) | `mode=no_soundcards` / `mode=no_device`, each with a next command |
| Live evidence (peer1) | `[voice-capture] device="default" mode=no_soundcards bytes=0`; (HP) `device="gopi_no_such_device" mode=no_device bytes=0 want=64000 elapsed_ms=37` |
| Test | `TestVoiceCaptureNoSoundcards`, `TestVoiceCaptureNoDevice`; harness `case_a` |

Two distinct situations share one arecord message, and the client separates them
by asking the inventory (`voiceInventoryProbe`, `arecord -l`):

- **peer1 measured**: `arecord -l` → `no soundcards found`; a capture attempt
  ends `ALSA lib confmisc.c:855:(parse_card) cannot find card '0'` +
  `audio open error: No such file or directory`. The *cause* is no hardware, so
  the answer is `no_soundcards` — "this host has no microphone, capture
  elsewhere" — not "your device name is wrong".
- **HP measured**: the same stderr for a name that does not exist
  (`ALSA lib pcm.c:2664: ... Unknown PCM nosuchdev`) on a host that *does* have
  capture hardware → `no_device`, with `arecord -l` / `pactl list short sources`
  as the next step.

The peer1's ALSA inventory is itself misleading (two cards, an advertised
ALC3228 capture PCM, every `/dev/snd` node mode 660 root:audio and the user not
in `audio`) — which is why the classification is a *probe*, not a guess from
stderr alone.

## Case (b) — device busy (a second capture holds the PCM)

| | |
|---|---|
| Repro | hold a raw capture device, then capture the same device from a second process: `arecord -D plughw:1,0 -t raw -f S16_LE -r 16000 -c 1 -d 8 hold.pcm &` then `gopi voice_input --client --device plughw:1,0 --seconds 3` |
| Red (before) | no client; and the naive client shape (`exec` + `Wait`) blocks for the FULL requested duration on a device that will never open — with `--seconds 60` that is a one-minute hang for a failure that is known in milliseconds |
| Green (now) | `mode=busy` with an actionable next step, returned in well under the wall-clock bound |
| Live evidence (HP) | `[voice-capture] device="hw:1,0" mode=busy bytes=0 want=64000 elapsed_ms=17` |
| Test | `TestVoiceCaptureBusyIsNamedAndNeverHangs`; harness `case_b` (+ its `<=HANG_BUDGET` assertion) |

Measured on the HP: with a holder on `plughw:1,0`, a second capture fails
deterministically 3/3 with `arecord: main:831: audio open error: Device or
resource busy`.

The anti-hang guarantee is structural, not a timer bolted on afterwards: every
capture runs under `voiceCapturePlan.wallClockBound()`
(`--seconds + open grace + drain grace`) through `exec.CommandContext` with
`WaitDelay` as the kill grace, so a device that opens and then never delivers is
killed and reported as `timeout` (red-proof:
`TestVoiceCaptureRunnerDoesNotHang` — reverting the bound makes the test hang,
which is the failure it prevents; verified RED by mutation).

## Case (c) — hot-unplug mid-capture

| | |
|---|---|
| Repro | end the client's own recorder mid-capture, exactly as a yanked device would: `gopi voice_input --client --seconds 6 --out /tmp/hu.pcm &` then `sleep 3; pkill -f "arecord.*/tmp/hu.pcm"` (the harness does this; the output path makes the match unambiguous) |
| Red (before) | no client; the naive shape reports success (arecord can exit **0** on a vanished stream) and hands a truncated recording to the engine as if it were complete |
| Green (now) | `mode=hot_unplug`, stating how much arrived, out of how much, and where the partial file is |
| Test | `TestVoiceCaptureHotUnplugPartialPolicy`, `TestVoiceCaptureShortCaptureIsHotUnplugNotOK`; harness `case_c` |

A PulseAudio null-sink monitor is NOT a usable rig for this case (measured): its
monitor keeps delivering after `pactl unload-module`, and tearing down a real
input source mid-capture proved unreliable too. Interrupting the recorder is the
deterministic repro, and it exercises the same client path a yanked device does
(recorder dies → short buffer → classification).

### Partial-audio handling policy (explicit, not implied)

1. **Never report a truncated capture as complete.** Completeness is a
   *measurement*, not the exit code: `voiceCompleteRatio` (0.9) of the expected
   PCM. Exit 0 with 2 s of a 10 s request is `hot_unplug`, not `ok`
   — `TestVoiceCaptureShortCaptureIsHotUnplugNotOK` is the regression guard for
   exactly that trap.
2. **Keep what arrived.** Bytes ≥ `voiceCapturePartialKeepBytes` stay on disk
   (HP run: 160000 bytes retained) and the path is named in the message, because
   a partial recording is often still usable if the user spoke before the device
   went away.
3. **Exit non-zero**, so a script branches on the failure rather than on prose.

Measured evidence (HP, live) — a real mic capture cut off at 2.9 s of a 6 s request:
```
[voice-capture] device="default" mode=hot_unplug bytes=92000 want=192000 elapsed_ms=3127
gopi voice_input: device disappeared mid-capture — got 2.9s of the requested 6 s
  (partial audio kept at /tmp/hu3.pcm; it is usable only if you spoke before the device went away)
```
4. **A mid-stream death outranks the open-time wording.** Measured: a removed
   Pulse source makes parecord print `Stream error: No such entity` and exit 0
   with 0 bytes; the same phrase at open time means "no such source". The
   classifier checks *bytes arrived, then stopped early* first
   (`classifyCaptureOutcome`, `gotBytes > 0 && short → hot_unplug`), so the same
   error text is named for what it actually was.

## Case (d) — unsupported rate/channel request

| | |
|---|---|
| Repro | ask a raw device for the pinned 16k/mono contract when it only accepts 2 channels at 44100–192000 Hz: `arecord -D hw:1,0 -t raw -f S16_LE -r 16000 -c 1 -d 1 x.pcm` |
| Red (before) | no client |
| Green (now) | `mode=unsupported_params` (or `busy` when a sound server holds the PCM), pointing at a plug-capable device string |
| Live evidence (HP) | `mode=unsupported_params bytes=0 want=64000` when the raw PCM is free; `mode=busy` while PulseAudio owned it — both correct, and the classifier prefers the parameter verdict when both texts appear |
| Test | `TestVoiceCaptureUnsupportedParamsBeatsBusy`, `TestVoiceCaptureUnsupportedVariants`; harness `case_d` |

Measured on the HP, `hw:1,0` (`--dump-hw-params`): `CHANNELS: 2`,
`RATE: [44100 192000]` — it *cannot* honour the contract, and says so with
`arecord: set_params:1358: Channels count non available`.

Two things this case pins down:

- **Parameter complaints outrank "Device or resource busy".** When an audio
  server owns the raw PCM, arecord's stderr contains *both*; the parameter text
  is the deterministic one (this ordering was first measured by the sibling probe
  script and is preserved here). The harness's HP run shows the state dependence
  honestly: on the same day `hw:1,0` reported `busy` because PulseAudio held it —
  both are correct refusals, and the classifier picks parameters when both appear.
- **The client refuses impossible requests before touching hardware**: `--seconds 0`
  and `--seconds` above `voiceMaxSeconds` are rejected up front
  (`buildVoiceCapturePlan`), asserted by the harness as `reject --seconds 0`.

## Case (e) — muted / zero-gain input

| | |
|---|---|
| Repro | mute a real source and capture it: `pactl set-source-mute @DEFAULT_SOURCE@ 1; gopi voice_input --client --seconds 3`; or the hardware-free equivalent: capture a freshly loaded null-sink's monitor (nothing playing) |
| Red (before) | no client — the naive shape returns `ok` with a **full, valid** 60 s of zeros, which is then either transcribed as nothing (wasted engine run) or sent over the transport (wasted 1.9 MB) |
| Green (now) | `mode=silent`, naming peak / dBFS / non-zero-sample count and the likely cause |
| Live evidence (HP) | `[voice-capture] device="default" mode=silent bytes=64000 want=64000 elapsed_ms=2053` — a muted real source, `peak 0, -120.0 dBFS, 0/32000` |
| Test | `TestVoiceCaptureSilenceIsDetected`, `TestVoiceCaptureDitherIsSilent`, `TestVoiceCaptureAmbientIsNotSilent`, `TestVoiceCaptureEmptyBufferIsSilent`, `TestVoiceCaptureRunnerSurfacesSilence`; harness `case_e` |

Measured on the HP:

| condition | samples | non-zero | peak | outcome |
|---|---|---|---|---|
| `pactl set-source-mute <id> 1`, then a 2 s pulse capture | 32000 | **0** | 0 | silent |
| null-sink monitor, nothing playing | 36006 | 19 | 32000 | silent |
| real microphone, **not** muted (ambient room) | 48000 | 100 % | 32768 | ok |

The detector (`voiceAudioStats.silent`) is deliberately two-factor — peak
(`voiceSilencePeakThreshold`, ≈ −72 dBFS) *and* non-zero ratio
(`voiceSilenceNonZeroRatio`) — because either alone misfires: pure peak would
call a dithered near-silence loud, pure non-zero-count would call the
single-LSB-dithered case above (19 non-zero of 36006) loud. Both directions have
a test, including the false-positive guard
(`TestVoiceCaptureAmbientIsNotSilent`, built from the measured ambient numbers)
— a detector that discarded real recordings would be worse than no detector.

---

## Red-proofs: mutation testing

Rule 10.5 requires each fix to ship a regression test. "The test passes" is not
evidence that it *would fail without the fix*, so each was verified by mutating
the implementation and watching the test go red:

| Mutation (the fix, reverted) | Test that catches it | Result |
|---|---|---|
| silence promotion removed from `runVoiceCapture` | `TestVoiceCaptureRunnerSurfacesSilence`, `TestVoiceCaptureSilenceIsDetected` | RED |
| completeness check removed (`short := false`) | `TestVoiceCaptureShortCaptureIsHotUnplugNotOK`, `TestVoiceCaptureHotUnplugPartialPolicy` | RED |
| parameter-before-busy ordering dropped | `TestVoiceCaptureUnsupportedParamsBeatsBusy`, `TestVoiceCaptureUnsupportedVariants` | RED |
| wall-clock bound removed (`WithCancel` for `WithTimeout`) | `TestVoiceCaptureRunnerDoesNotHang` | RED (hangs, then fails) |
| `silent()` forced to `false` | `TestVoiceCaptureSilenceIsDetected`, `TestVoiceCaptureDitherIsSilent` | RED |

## What the harness can and cannot stage (honesty note)

`scripts/voice-capture-failmatrix.sh` runs on any node and **SKIPs** what the
host cannot reproduce, asserting that case's classifier contract from the repo
instead. Observed coverage:

- **HP laptop (real capture hardware + PulseAudio)** — live: `no_device`,
  `unsupported_params`/`busy` on `hw:1,0`, `hot_unplug` (capture cut off at
  3 s of a 6 s request; 100000 partial bytes retained), `silent` twice (muted
  real source; zero-gain null-sink), the two front-end refusals. Not staggable:
  a genuine second-reader BUSY — PulseAudio *mixes* two readers of the same
  source and the null-sink unbinds its monitor slot instead of refusing, both
  confirmed by measurement, so the case is SKIPped rather than mis-asserted
  (its classifier contract is asserted from the repo instead).
- **peer1 (no capture hardware)** — live: `no_soundcards` for both a default
  capture and a nonsense device name. Not staggable: anything needing a device.

Every SKIP is recorded as SKIP in the TSV; the run exits 0 only when no *PASS*
assertion failed.

A `vocab` row is always checked last: it asserts that all eleven mode strings this
script matches on are the strings `captureFailureMode` in
`cmd/agent/cli_voice_capture.go` actually produces, so the two halves of this
contract cannot drift apart. With `--repo <checkout>` the classifier tests also
run on the node, so a remote run is a real regression gate and not just a
hardware probe — that flag is how the same script produced the "all PASS" run
below from the HP.

## Known limits / not done

- **No `--client | engine` pipeline yet.** The capture path writes PCM and
  reports its outcome; forwarding the PCM to `--server` (HTTP or `--stdio`) is
  the deliverable of the sibling capture-client task, and this layer was kept
  self-contained so the two do not collide.
- **PortAudio/USB hot-plug detection** is not implemented: a device that
  disappears is detected *by the capture ending short*, not by a udev watch.
  That is sufficient for the pinned contract (the client never claims success on
  a short capture) but it cannot warn before a capture starts.
- **`permission` and `pulse_refused`** are classified from stderr but have no
  live repro in the harness: they need a node whose user lacks the `audio` group
  (peer1's situation, already measured by the probe script) and a dead Pulse
  socket respectively. Their patterns are covered by the classifier tests'
  vocabulary pin; a live repro is deferred.
