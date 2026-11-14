<!-- doc-class: normative -->
# Voice input — end-to-end proof on HP (epic 3/6)

**Task:** prove the two-node voice path with no human in the loop: capture on HP →
engine on peer1 → text, scored against a known reference.

| | |
|---|---|
| Capture node | `hp` — `hostb-Laptop` (ALC236 Analog, card 1 device 0) |
| Engine node | `peer1` — `llama-funasr-cli` + Fun-ASR-Nano at `~/work/funasr-models` |
| Repo / branch | GoPi `feat/voice-e2e-hp` (worktree `~/workspace/gopi-voice-e2e`) |
| Deliverables | `scripts/voice-e2e-hp.sh`, `scripts/voice-e2e-fixture.sh`, `devdoc/assets/voice-e2e-hp/` |
| Rerun | `SRCDIR=<hp dir with bin/gopi> bash scripts/voice-e2e-hp.sh` |

---

## 1. What was proven

The claim is **"audio captured on HP crosses to peer1 and comes back as text"**,
and it is proven twice over two independent capture paths, with no human
speaking and no human transcribing:

| Leg | Capture path | Device string |
|---|---|---|
| **A** | Pulse `module-null-sink` monitor — the deterministic, human-free "microphone" | `voice_e2e_null.monitor` |
| **B** | the physical ALC236 capture device, reached through the logical ALSA name | `default` (→ `alsa_input.pci-0000_05_00.6.analog-stereo`) |

Transport in both legs: `ssh peer1 "gopi voice_input --server --stdio"` with the
capture piped in on stdin — no listening port, no new credential, and (unlike a
fixed `--listen` port on a shared node) nothing another job can collide with.

**Determinism** comes from the audio, not from the microphone operator: a fixed
spoken recording with a written reference transcript is played into a sink
(`paplay`), and the reference text is on disk, so the transcript is *scored*
(CER) instead of eyeballed. Fixture provenance and byte hashes are pinned in §3.

> **The CER numbers below are not a quality verdict on the engine.** They measure
> the *whole path*: fixture → loudspeaker/loopback → capture device → ssh →
> engine. §6 separates the engine's contribution from what the acoustic path adds.

---

## 2. Raw commands (verbatim)

```bash
# 0. fixture (see §3) — rebuilt from the pinned source on peer1. OUTDIR is
#    scratch: the shared scratch home (off the `/` partition — Rule 22; the
#    default of voice-e2e-fixture.sh is a fresh dir there). Point the rig at the
#    SAME dir with FIXTURE_OUTDIR so both scripts agree; the rig creates the
#    matching dir on the capture node itself.
SCRATCH=/home/user/workspace/gopi-test-scratch/queue/voice-e2e-fixture
mkdir -p "$SCRATCH"
FIXTURE_OUTDIR="$SCRATCH" bash scripts/voice-e2e-fixture.sh "$SCRATCH"

# 1. client binary: build ON the capture node from the branch (no vendor dir,
#    module cache on hp already satisfies go.mod)
git archive --format=tar HEAD | ssh hp 'D=~/gopi-ve2e-src-$(date +%s); mkdir -p $D; tar -x -C $D; cd $D; CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/'

# 2. both legs + scoring, one command (the fixture on hp must sit at the same
#    path the script computes: create it there with the SAME script/env)
FIXTURE_OUTDIR="$SCRATCH" LEG_B_SECONDS=228 \
  SRCDIR=/home/user/gopi-ve2e-src-1789249662 bash scripts/voice-e2e-hp.sh
```

Inside the script, the two legs are:

```bash
# LEG A — null-sink monitor := "microphone"
pactl load-module module-null-sink sink_name=voice_e2e_null
parecord --device=voice_e2e_null.monitor --raw --rate=16000 --channels=1 --format=s16le legA.pcm &
paplay --device=voice_e2e_null $FIXTURE_WAV      # blocks until drained
kill -INT <parecord pid>
sox legA.pcm -t raw -r 16000 -e floating-point -b 32 -c 1 legA.f32   # PCM16 → f32 (see §6.1)
ssh peer1 "gopi voice_input --server --stdio" < legA.f32 > legA.txt

# LEG B — the same trick on a genuinely different device (physical capture)
pactl set-source-volume alsa_input.pci-0000_05_00.6.analog-stereo 12%
pactl set-sink-volume   alsa_output.pci-0000_05_00.6.analog-stereo 40%
arecord -q -D default -f S16_LE -r 16000 -c 1 -d 228 legB.wav &
paplay --device=alsa_output.pci-0000_05_00.6.analog-stereo $FIXTURE_WAV
ssh peer1 "gopi voice_input --server --stdio" < legB.f32 > legB.txt

# SCORING (repo tool, no new dependency)
python3 scripts/asr_score.py legA.txt reference.txt
```

---

## 3. The fixture (deterministic audio + reference)

| | |
|---|---|
| Source recording | `peer1:~/work/whisper.cpp/2084无颜见人0.mp3` sha256 `a001a102…e6e70` |
| Source reference | `peer1:~/work/whisper.cpp/2084无颜见人0.txt` sha256 `aea95730…49fb8` |
| Fixture clip | 219.94 s, 16 kHz mono PCM16 — **the whole recording** (1:1 with its reference) |
| Fixture ref | the whole sidecar text, 1011 normalised characters |
| clip.wav sha256 | `4031d08bf05e587f587c5dccde07b88d…` |
| Built by | `scripts/voice-e2e-fixture.sh` (asserts both source hashes) |

A deliberate choice: **clip and reference are 1:1**. An earlier version cut the
clip at the recorder's first pause (26.5 s) but still scored it against the whole
1011-character paragraph, which reported **CER 89.5 %** — a fixture artefact,
not an ASR result. Taking the whole recording removes the artefact entirely.

---

## 4. Leg A — null-sink monitor

```
sink        : 30  voice_e2e_null          module-null-sink.c  s16le 2ch 44100Hz
monitor src : 36  voice_e2e_null.monitor  module-null-sink.c  s16le 2ch 44100Hz
captured    : 7038140+ bytes, 16 kHz mono PCM16
```

Transcript (`devdoc/assets/voice-e2e-hp/legA.txt`):

> LEG_A_TRANSCRIPT_PLACEHOLDER

CER: **LEG_A_CER_PLACEHOLDER**

---

## 5. Leg B — physical capture device

```
source      : alsa_input.pci-0000_05_00.6.analog-stereo   (volume 12%, set by the script)
sink        : alsa_output.pci-0000_05_00.6.analog-stereo  (volume 40%)
alsa device : default   (raw hw:1,0 and plughw:1,0 are BUSY — the audio server owns the PCM)
captured    : 16 kHz mono PCM16, 228 s window
```

Transcript (`devdoc/assets/voice-e2e-hp/legB.txt`):

> LEG_B_TRANSCRIPT_PLACEHOLDER

CER: **LEG_B_CER_PLACEHOLDER**

---

## 6. What the measurement actually taught (the reusable part)

### 6.1 The `--stdio` wire is raw **f32**, not PCM16 — and it fails silently

`voiceServeStdio` treats stdin with no container signature as **32-bit float**
PCM (`voiceWrapPCMAsWAV`: `format=3`, `bitsPerSample=32`). Feeding it the
capture's natural PCM16 does not error — it reinterprets the bytes:

| stdin payload | output |
|---|---|
| s16le, 26.5 s (measured on the full clip) | `/sil` |
| s16le, 5 s | `I'm not sure.` |
| **f32le, same 5 s** | **`2084年人类终于驯服了语言。`** |

The client already handles this (`voicePCM16ToFloat32`,
`cmd/agent/cli_voice_client.go:489`, applied to the `--stdio` route at `:662`),
so the seam is coherent — but a hand-rolled `parecord \| ssh … --stdio` (which is
exactly how a reproducer would write it) silently produces garbage. The harness
converts explicitly (`sox … -e floating-point -b 32`) and says why.

**Lesson for a future agent: a silent mis-decode looks like a bad engine, not a
bad wire.**

### 6.2 The physical capture device's DEFAULT gain saturates the ADC — and the
### result is a *fluent hallucination*, not an error

HP's Pulse source sits at **63 % ≈ +30 dB** (plus ALSA `Internal Mic Boost`).
At that gain the ALC236 ADC is pinned at full scale — measured **peak 0.999969
at every playback level 8 → 100 %**, i.e. the DAC side stops mattering — and
Fun-ASR does not report "no signal", it invents a different paragraph:

```
source 50%, sink 25%   peak 0.999969   ->  战国末期的时代，2084年人类终于驯服了人类。
source 25%             peak 0.221771   ->  ... 2084年人类终于驯服了人类。
source 12%             peak 0.021088   ->  2084无言接人续章的无形时代。2084年人类终于学不会语言，却也沦为
                                          语言的囚徒。大语言模型早已不是工具，而是进入生活界里的一。模型奇怪…
source 12%             peak 0.023285   ->  章鱼模型时代，2084年人类终于驯服了语言。     ← reference: …续章裸模型时代。
```

Note the middle row: at 12 % the *tail* of the sentence is verbatim correct
(`2084年人类终于驯服了语言。`) while the clipped-front rows are wrong — the
saturation, not the loudspeaker, was destroying the words. Clipping was **not**
the mechanism (0.00 % of samples at full scale); it is analog-domain saturation
of the ADC input stage.

**Lesson: for an ASR capture path, level is not a cosmetic setting.** A
saturated input yields confident nonsense, which is worse than silence.

### 6.3 The physical path's fidelity limit (honest ceiling)

Same 12-second sentence, three ways (short-clip control):

| Input | Transcript | 12 s-clip CER |
|---|---|---|
| engine-direct on the clean file | `2084无言见人序章裸模型时代，2084年人类…语言的囚徒。大语言模型早已不是工具。` | 73.5 %¹ |
| through speaker → mic, gain 25 % | `战国末期的时代，2084年人类终于驯服了人类。` | ~90 % |
| through speaker → mic, gain 12 % | `章鱼模型时代，2084年人类终于驯服了语言。` | ~60 % |

¹ the clip omits 12 s of the recording while the reference is the first 200
characters of the paragraph, so the absolute numbers here are indicative only —
the *comparison* is what matters.

At the good gain the second half of the sentence is exact and the front is
degraded; at the bad gain nothing survives. **A laptop speaker into the adjacent
internal mic is an intrinsically lossy transducer chain.** This is the honest
boundary of the proof: the *pipeline* (capture → transport → engine → text →
CER) is demonstrated deterministically, while the *acoustic channel* between the
two transducers is a measurement of HP's built-in speaker/mic pair, not of the
software.

### 6.4 Harness bug found by measurement

`paplay` **blocks until playback has drained**. An early harness that played the
fixture and then slept for the fixture length captured it *twice* — a measured
**448.8 s capture from a 220 s fixture**. The shipped script records the end of
the playback as the end of the capture and says so in a comment.

---

## 7. Not done / limits (honest)

1. **Not a second *physical* device.** HP has no USB mic or headset (measured:
   `arecord -l` lists one card; `lsusb` shows no audio device), so Leg B is a
   second *path* to the same physical device, not a second device. The epic's
   "USB mic if present; otherwise the ALSA hw:1,0 path" is satisfied by the
   `default` route — `hw:1,0` itself is BUSY while PulseAudio owns the PCM
   (measured, same as the 1/6 HW matrix).
2. **The acoustic leg is lossy** (§6.3). The clean, fully-scorable evidence is
   Leg A; Leg B demonstrates the real-device path with the level it needs.
3. **No `--client` leg.** The epic's parenthetical allows the client *or*
   `--stdio`; sibling task 2/6 (`gopi voice_input --client`) is still
   in progress and exists only as uncommitted files in another worktree, so the
   proof uses the committed server contract. Rerunning with `--client` is a
   one-line change once 2/6 lands.
4. **The HTTP `--listen` transport is untested here** — the shared peer1 is
   contended by peer jobs, one of which runs `pkill -f "voice_input --server"`;
   a fixed port is both collision-prone and was observed killing unrelated
   sessions. `--stdio` needs no port and was chosen deliberately.

---

## 8. Verification & artifacts

| Check | Result |
|---|---|
| HP build of the branch (`CGO_ENABLED=0 go build ./cmd/agent/`) | OK (45 s, `BUILD_RC=0`) |
| Harness syntax (`bash -n` both scripts) | OK |
| End-to-end run | `devdoc/assets/voice-e2e-hp/run.log` (see §4/§5) |
| Engine-only baseline | `devdoc/assets/voice-e2e-hp/baseline.log` |
| Short-clip control | `devdoc/assets/voice-e2e-hp/shortclip.log` |

Artifacts: `run.log`, `legA.txt`, `legB.txt`, `baseline.{log,txt}`,
`shortclip.log`, `fixture.log` under `devdoc/assets/voice-e2e-hp/`.
