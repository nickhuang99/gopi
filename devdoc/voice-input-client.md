<!-- doc-class: normative -->
# gopi voice_input — the capture client (`--client`)

Scope: the client half of `gopi voice_input` (Phase 3 of
`devdoc/SPEC-voice-input-phase23.md`). The server half — one-shot file
transcription, HTTP `/transcribe`, and the `--stdio` pipe — already shipped in
`cmd/agent/cli_voice.go` / `cli_voice_server.go`. This adds the side that
produces the audio.

Branch: `feat/voice-input-client` (worktree `~/workspace/gopi-voice-client`).

---

## 1. The contract, and why the route is "spawn a recorder"

The two halves are joined by one thing: **16 kHz, mono, signed 16-bit
little-endian**. A 44.1 kHz stereo stream does not produce an error on the far
side — it produces a transcript of noise. So the contract is pinned at the
recorder's argv (the last place before the microphone) rather than repaired
afterwards:

```
arecord  -D <dev> -f S16_LE -r 16000 -c 1 -t raw -q -d <N>
parecord  --device=<dev> --raw --rate=16000 --channels=1 --format=s16le
```

Both recorders are **already installed** on the capture-capable host
(`alsa-utils` / `pulseaudio-utils`), so this route adds **zero runtime
dependency and no cgo**. It also means the contract is *legible*: the four
values that decide whether the stream is decodable are visible in the argv,
not buried behind a C library's default device config.

The alternative from the spec §4 is `miniaudio.h` (4.1 MB header-only) linked
in via cgo — the route the spec recommended before the dependency budget was
weighed. It is **not built here**, and the honest reason is dependency cost:
it forces `CGO_ENABLED=1` for the whole binary (the release build is
`CGO_ENABLED=0`), it is a vendored 4.1 MB header, and it buys nothing the
spawn route cannot do on a host that has either recorder. It stays the
fallback for a host with neither — the one case where this route cannot meet
the contract. See §6 for what would change.

## 2. Device enumeration and stable IDs

`--list-devices` enumerates through two independent sources, because they
answer different questions:

| Source | Command | What it reports |
|---|---|---|
| PulseAudio | `pactl list short sources` + `pactl get-default-source` | what the sound server will actually open |
| ALSA hardware | `arecord -l` | what physical capture hardware exists |

**Pulse comes first, and that ordering is load-bearing.** On a running desktop
the sound server holds the card, so a bare ALSA hardware device is refused:

```
D=plughw:CARD=Generic_1,DEV=0  bytes=0  err=arecord: main:831: audio open error: Device or resource busy
D=default                      bytes=60000  err=none
D=pulse                        bytes=60000  err=none
```

(measured on the HP laptop, 2026-09-13). The Pulse ids are the ones that work
while a desktop session is up; the ALSA ones are for a headless box or when
the sound server is not running.

**Stable IDs.** An ID is `<backend>:<recorder-argument>` —
`pulse:@DEFAULT_SOURCE@`, `alsa:plughw:CARD=Generic_1,DEV=0`. The name half is
passed to the recorder **verbatim**, so there is no translation layer between
what `--list-devices` prints and what gets opened.

Two deliberate choices make an ID survive replugging:

- **ALSA is addressed by `CARD=<card name>`, not `CARD=<index>`.** A card
  index is a boot-order artifact: plug in a USB headset and the codec that was
  `card 1` becomes `card 2`, so `hw:1,0` silently opens the *headset* — a wrong
  device with no error. `CARD=Generic_1` is the card's name and does not move.
- **Pulse's default is offered as the wildcard `@DEFAULT_SOURCE@`**, not as the
  concrete source name that happens to be default today. The wildcard keeps
  working after the user switches their default source.

`plughw` is offered before `hw` because the *hardware* device usually cannot
take the contract natively and refuses it:

```
arecord: set_params:1358: Channels count non available
```

`plughw` is the same device with ALSA's conversion plugins, so it accepts
16 kHz mono PCM16 and converts. Both are listed — `hw` is the right choice
when the device genuinely supports the format.

**Monitor sources are excluded.** `pactl list short sources` also lists
`*.monitor` — the loopback of an *output* sink. Listing it as a microphone
would record whatever is playing (usually silence) and read as a broken mic.
It stays reachable by naming it explicitly, which is what makes it useful as
the human-free test rig (§5).

## 2b. Which device gets chosen — the selection policy

`--device` names one device; it does not say what happens when it is absent,
when the user has a preferred mic, or when two mics are attached. That is a
policy, and it is fixed (first match wins):

| # | Tier | Source | If it fails |
|---|---|---|---|
| 1 | `--device ID` | the command line | **hard error** — you named a device, so nothing else is tried |
| 2 | configured default | `$GOPI_VOICE_DEVICE`, else settings.json `"default_device"` | reported, then the search **continues** |
| 3 | system default capture | ALSA's own `default`, considered **only when no sound server answers** | continue |
| 4 | first enumerated | first entry of `--list-devices` | hard error (nothing to record from) |

Two asymmetries are deliberate:

- **Tier 1 is hard, tier 2 is soft.** An explicit `--device` that is not
  available is a user mistake and must be loud; a *configured* default is a
  standing preference that outlives any one session, so a device that is
  currently unplugged should not make the client unusable — it is reported
  (`<id> is not present — falling through to the next rule`) and the search
  continues.
- **Tier 3 is conditional.** With a sound server running, ALSA's `default`
  routes back into that same server, so the tier would silently re-pick tier 4's
  device; it is only consulted when the server is absent (`pactl info` fails).
  This is what makes tier 3 meaningful on a headless box and a no-op on a
  desktop.

Every run prints the winning device **and its tier** (`[explicit --device]`,
`[configured default]`, `[system default capture]`, `[first enumerated]`), so
"which mic did it actually use" is never a guess — it is in the same line as
the recording banner.

**`$GOPI_VOICE_DEVICE` and settings.json are one knob, read in exactly one
place** (`voiceConfigDefaultDevice`, Rule 2): the env var wins over the
settings file, and the file wins over "no configured default". A card *index*
found in either is ignored with a note rather than opened — the configured
default obeys the same by-name rule as `--device` (below).

### Hotplug: the chosen device disappears between list and capture

Enumeration and capture are two separate moments, and the device can go away in
between (unplugged USB mic, or taken by another process). The client does not
assume the listing is still true: **the chosen device is opened for 1s
(`voiceVerifySeconds`) before the real recording**.

There is no cheaper probe. `arecord -d 0` looks like "open and exit
immediately" but actually **hangs** waiting for frames, so a liveness check has
to be a real (short) capture. Still, 1s spent before the user speaks is
strictly better than discovering the device is gone *after* a 5s utterance has
been recorded into nothing.

The failure is reported **by name**, with the fix:

| Where the stale device came from | Behaviour |
|---|---|
| `--device ID` | hard error: `device "<id>" is not reachable: <recorder error> — re-run --list-devices: the device may have been unplugged or taken by another process` |
| configured default | note (`<id> is unreachable …`) then fall through to the next tier |

One device is exempt from the probe: the Pulse wildcard `@DEFAULT_SOURCE@`.
It is a server-side alias resolved at open time, not a concrete source — a
capture through it is itself the liveness check, and a 1s probe would only
record silence while the user has not spoken yet.

A device that opens but yields **zero bytes** is a *warning*, not an error: a
muted microphone is a real, reachable device, and the empty recording is the
honest result.

### Live check with two inputs (HP) — the ordering proved on hardware

The policy is not only a unit-test claim. On `hostb-Laptop` (PulseAudio up)
a **second capture input** was added at runtime
(`pactl load-module module-null-source source_name=gopi_two`) — the laptop's
other HDA card is playback-only, so a null source is the honest way to give the
host two *enumerated and recordable* inputs at once. `--list-devices` then
listed 5 ids, with `pulse:gopi_two` on a line of its own.

| Run | Result | Tier printed |
|---|---|---|
| `--device pulse:gopi_two`, with `GOPI_VOICE_DEVICE=pulse:@DEFAULT_SOURCE@` set | recorded from `gopi_two`, 64000 bytes, exit 0 | `[explicit --device]` — tier 1 beat an *available* tier 2 |
| `GOPI_VOICE_DEVICE=pulse:gopi_two`, no `--device` | recorded from `gopi_two`, 64000 bytes, exit 0 | `[configured default]` — tier 2 beat a *different* tier 4 |
| no `--device`, no knob | recorded from `pulse:@DEFAULT_SOURCE@`, 64000 bytes, exit 0 | `[first enumerated]` |

Each tier therefore won over a **different, working** device — not over an
absent one, which is the only shape that actually proves an order.

The two stale paths were then proved on the same live host:

```
# the device was ENUMERATED, then unloaded between listing and capture:
$ gopi voice_input --client --device pulse:gopi_race --seconds 2 --stdio
gopi voice_input: device pulse:gopi_race is not reachable: parecord failed on "pulse:gopi_race":
  exit status 1 — Stream error: No such entity (no capture device by that name — it may have been
  removed since it was listed) — it may have been removed or taken since it was listed;
  run `gopi voice_input --list-devices` again to see what is there now     exit=1   (hard, tier 1)

$ GOPI_VOICE_DEVICE=pulse:gopi_two …        # after the same source was unloaded
[voice] configured default device "pulse:gopi_two" (GOPI_VOICE_DEVICE) is not present — falling
through to the next rule
[voice] recording 2s from pulse:@DEFAULT_SOURCE@ … [first enumerated]      exit=0   (soft, tier 2)

$ GOPI_VOICE_DEVICE=alsa:plughw:CARD=Generic_1,DEV=0 …   # enumerated, but the server owns the card
[voice] configured default device "alsa:plughw:CARD=Generic_1,DEV=0" (GOPI_VOICE_DEVICE) is
unreachable — falling through to the next rule: … audio open error: Device or resource busy …
[voice] recording 2s from pulse:@DEFAULT_SOURCE@ … [first enumerated]      exit=0   ("unreachable" ≠ "not present")
```

The first block is the exact race the 1s pre-open probe exists for: the device
was in the listing, and gone by the time the recorder opened it — reported by
name, before any speech was lost. The card-index refusal was re-confirmed live
for `1`, `hw:1,0` and `plughw:1,0` (all exit 1 with the boot-order
explanation), and the host was left as found (module unloaded, 3 sources).

## 3. Failure modes, each with its own message

Every failure a user can hit is named rather than surfacing as a recorder
stack trace:

| Situation | Message |
|---|---|
| No capture hardware at all (a headless/VM host) | `no capture devices found: \`arecord -l\` lists nothing and no PulseAudio sources are visible… record on the machine with the microphone` |
| `--device` names something absent | `unknown device "x" — available: <every valid id>` |
| Device held by the sound server | the recorder's own error **plus** `(the device is held by another process … pick a pulse: device from --list-devices)` |
| Device reachable but silent (muted) | `produced no audio from <id> (1.9s) — the device is reachable but silent: check the capture level is not muted` |
| Transcriber not running | `cannot reach transcriber at <url> … (start it with \`gopi voice_input --server --listen :7788\`)` |
| URL with no scheme | `server URL must start with http:// or https://, got "192.168.7.6:7788"` |
| Body is not JSON (an HTML proxy error) | `transcriber at <url> returned no JSON (HTTP 502): <first 200 chars>` |

The "reachable but silent" and "held by another process" cases are separated on
purpose: they look identical from the outside (zero bytes) and have opposite
fixes.

## 4. Two transports, one capture

`--client` captures once and sends the same audio two ways:

- **`--client [--server-url URL]`** — build a WAV container around the PCM16
  and `POST` it as multipart field `audio` to `/transcribe`. The server keeps
  the uploaded extension (`.wav`) and the engine's decoder sniffs the format,
  so a real container is what it wants.
- **`--client --stdio`** — write raw **f32** PCM on stdout, because that is
  what the server's `--stdio` shape reads (`voiceServeStdio`). The two halves
  are the ends of one hop:

  ```
  gopi voice_input --client --seconds 5 --stdio \
    | ssh host gopi voice_input --server --stdio
  ```

  (int16 → float32 divides by full scale to map onto [-1,1) — a wrong divisor
  is a gain error that survives into the transcript, which is why it is a
  constant and covered by a test.)

`--server-url` is spelled out rather than reusing `--server` because
`--server` is already a **mode** (run as a server). `--client --server` is
rejected with an explanation: one process is one side of the hop.

## 5. Test rig, and how this was verified

**The fleet constraint (measured 2026-09-13, recorded in memory #2187):** the
peerbox (this development host) has **no capture device at all** —
`arecord -l` reports only HDMI playback PCMs, PulseAudio refuses connections.
The HP laptop is the only capture-capable node (`card 1: Generic_1 … ALC236
Analog`, PulseAudio up). So:

- **Unit tests** (`cmd/agent/cli_voice_client_test.go`) cover everything that
  does not need a microphone, and they run anywhere — including this host.
  Device parsing is tested against the **verbatim** `arecord -l` and `pactl
  list short sources` output captured from the HP, kept as fixtures, because
  the parser exists to handle that real formatting.
- **`--list-devices` is verified on the HP**, the only machine where the
  question has a non-empty answer.
- **A real recording is verified on the HP** by exercising the recorder argv
  the tests pin.

**Human-free capture rig** (for testing the mic *path* without a person
speaking, including in CI-like conditions): a Pulse null sink plus its monitor
source is a deterministic audio source —

```
pactl load-module module-null-sink sink_name=gopitest
parecord -d gopitest.monitor --raw --rate=16000 --channels=1 --format=s16le   # captures what is PLAYED
# play a known WAV into gopitest, then score the transcript with the CER script
```

This is why monitor sources are excluded from `--list-devices` but not
unreachable by name.

## 6. What would move this to the cgo route

The spawn route cannot meet the contract only when **neither `arecord` nor
`parecord` exists** and installing them is not permitted (a locked-down
appliance image). The change is localized by design: `voiceRecorderCommand` is
the single function that produces argv, and `voiceRecord` the single function
that produces PCM16 bytes. A cgo capture backend replaces `voiceRecord`'s body
and returns the same `[]byte` of PCM16 at `voiceCaptureSampleRate` — the
enumeration, resolution, forwarding and conversion layers do not change.
Building that in now would cost `CGO_ENABLED=1` for every user to serve a host
this fleet does not have.

## 7. Rule 1 / 13 notes

Every rate, channel count, format string, recorder flag, backend name, device
wildcard, header field, size cap and timeout is a named constant in
`cli_voice_client.go` (the capture half), `cli_voice.go`/`cli_voice_server.go`
(the shared halves). Two of them are cross-file contracts:

- `voiceCaptureSampleRate` / `voiceCaptureChannels` / `voiceCaptureBitsPerSample`
  are referenced by the **recorder argv** *and* by the **WAV header builder**
  (`voiceBuildWAV` in `cli_voice_server.go`), so a rate change cannot land in
  one and not the other.
- `voiceDefaultServerURL` is assembled from `voiceDefaultListenPort`, which is
  the port half of the server's `voiceDefaultListen` — the client's default URL
  and the server's default bind address cannot drift apart. A test
  (`TestVoiceClientDefaultTargetPointsAtServerDefault`) asserts it.

`TestVoiceClientNoBareDeviceLiterals` is the Rule 13 gate: it scans every
production `cli_voice*.go` file and fails if a device string or the sample rate
appears as a bare literal outside its own definition site.

`voiceWrapPCMAsWAV` was refactored onto a shared `voiceBuildWAV(pcm, format,
bits)`: the server's stdio wrapper (f32) and the client's upload wrapper (PCM16)
now share one header implementation, differing only in the two values that are
genuinely different. Before this change the server's builder hardcoded
`channels=1, sampleRate=16000` as function-local literals — the exact drift
Rule 13 describes, fixed here because the client needs the same header.

---

## 8. Two bugs the verification caught (both worth remembering)

**A valueless flag ate the next argument.** The shared parser
(`memoryFlagSet` in `cli_memory.go`, used by both `memory` and `voice_input`)
treated *every* `--flag` as taking a value: if the next token did not start
with `--`, it was consumed as the flag's value. `--client clip.wav` therefore
recorded from the device and never saw the path — and the invocation was
**accepted**, which is the dangerous shape. Fix: `boolean(name)` declares
valueless flags and `parse` never claims the token after them. Undeclared flags
keep the old behaviour (every existing caller), pinned by a test.

**`/transcribe` was joined twice.** `voiceTranscribeURL` was called once in the
pre-flight check (whose result was discarded) and again inside the forwarder,
so the endpoint became `/transcribe/transcribe` and the POST failed with a
"invalid port" parse error — *after* a full recording had been spent. The unit
tests passed because they tested each layer in isolation; only running the real
binary against a stub transcriber exposed the composition. Fix: resolve the URL
once, treat the result as final (documented at both ends), and parse the whole
URL up front so a bad port is caught before the microphone opens.

The lesson generalizes: **layer-local tests cannot see a contract mismatch
between two layers.** The pre-flight check and the forwarder each looked right.
Full flow is documented in §5 and `tasks/voice-input-client-report.md`.
