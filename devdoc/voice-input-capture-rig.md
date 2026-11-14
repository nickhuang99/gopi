<!-- doc-class: historical -->
# `gopi tool voice_input` — the deterministic local-capture rig (null-sink + remap-source)

Task: `01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb` · host: **peerbox** (the host that has PulseAudio)
Branch: `task/01a113e467767eb9a4c33cb1df1cdfbb` · worktree: `/home/user/workspace/gopi-01a113e467767eb9a4c33cb1df1cdfbb`
Product code: **none changed.** master already carries the bare-capture shape
(`457a999b6 feat(voice): bare \`gopi tool voice_input\` records the mic and
transcribes it locally`; `git merge-base --is-ancestor 457a999b6 HEAD` → exit 0,
`git diff --stat master...HEAD` → empty). What this document adds is the **rig**
and the **evidence**, so the run's own capture path can be exercised by anyone,
with no human and no microphone.

## 0. What one run proves

`gopi tool voice_input --seconds 8 --record <wav>`, with **no `--device`** — i.e.
through the device policy's own default choice — under a rig that repoints the
machine's default source at a null sink playing a fixed WAV, produced a recording
that is **8.000 s long and not silence**:

| measure | value |
|---|---|
| requested | `--seconds 8` |
| WAV duration | **8.000 s** (128000 frames @ 16000 Hz, mono, PCM16) |
| file size | 256044 bytes (= 44-byte header + 128000×2) |
| peak sample | **15443** (of 32767) |
| non-zero-sample ratio | **0.8296** |
| RMS | **2374.2** |
| sha256 (peerbox artifact, byte-identical to this run's session copy) | `3ba06d3bbdb356a7d5ac1f0056e8993776bb3f8a8de77af39363cfa2babf902d` |
| transcript (stdout) | `Center, front, center, front, center, front, center, front, center, front, center.` |

The transcript is the fixture's own content (`/usr/share/sounds/alsa/Front_Center.wav`
looped), which is the independent second witness that the audio the engine
decoded is the audio the rig played; **CER scoring is out of scope for this doc**
(the epic's acceptance is numeric on the capture artifact, not on ASR quality).

Measured on peerbox with `./bin/gopi` = `/home/user/work/GoPi/bin/gopi`
(sha256 `09bd0390e1e891dc629269a2449e6c4fa624c60e0c7d6562122ae7749306e7b7`,
`gopi version` → `v0.1.0`, go1.24.5, linux/amd64), tree at `d90950195`,
2026-10-07. That tree also carries `457a999b6`
(`git merge-base --is-ancestor 457a999b6 HEAD` → exit 0 **on peerbox**), so the
binary RUN in §1.3 is the product code the epic says is already on master —
this branch adds no Go file, and the delivered evidence is the rig, not a build.

---

## 1. Verification (装置) — the rig, verbatim

**The order is the rig.** Two of the four traps below are *ordering* traps, so the
numbers here are only reproducible in this sequence.

**记号 (read this before copying any command below).** The commands are verbatim
from the run, but they are shell *fragments*: these two names are set here, once,
and are not defined further down —
`RIG=/home/user/workspace/gopi-voiceinput-rig` (this run's scratch root, off `/`:
device 2052 vs `/`'s 2051, Rule 22), `OUT=$RIG/cap/voiceinput-rig-8s.wav` (the
capture artifact), and `S=/home/user/.gopi/sessions/01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb/artifacts/`
(this run's own session directory — evidence lives there, never in the checkout,
Rule 37). §1.7 re-executes §1.2–§1.5 under its own names and is where the figures
in §1.4 were independently re-measured.

### 1.1 Fixture (build once; recipe in §2)

```bash
RIG=/home/user/workspace/gopi-voiceinput-rig            # scratch, off the source tree (Rule 30/Rule 22)
/usr/share/sounds/alsa/Front_Center.wav                  # source clip, ships with alsa-utils
$RIG/fixture/front-center-20s.wav                        # sha256 1e3f13495cae69c32d84185100ad81abaff67d71b883168b3cac993d4e4c6cd2  1920078 bytes
```

### 1.2 Rig up — three commands, in this order

```bash
SINK_ID=$(pactl load-module module-null-sink sink_name=gopi_rig sink_properties=device.description=gopi_rig)
SRC_ID=$(pactl load-module module-remap-source master=gopi_rig.monitor source_name=gopi_rig_rec)
pactl set-default-source gopi_rig_rec
```

- `module-null-sink` — a **playback sink that goes nowhere** (`gopi_rig`) and,
  with it, its own `.monitor` source. A monitor is not a microphone: the tool's
  device policy deliberately excludes `*.monitor` from the device list
  (`voicePulseMonitorSuffix = ".monitor"`, `cmd/agent/cli_voice_client.go:104`)
  — which is exactly why step 2 exists.
- `module-remap-source master=gopi_rig.monitor source_name=gopi_rig_rec` — turns
  that monitor into a **real, enumerable capture source** (`gopi_rig_rec`), the
  shape the policy accepts and `parecord` opens.
- `set-default-source gopi_rig_rec` — this is what makes the run a **default-device**
  run: the tool is *not* told a device. It selects `pulse:@DEFAULT_SOURCE@`
  ("the one id that keeps working when the user's default source changes",
  `cli_voice_client.go:284-295`), tier `[first enumerated]`, label
  `PulseAudio default source (gopi_rig_rec)`.

Both module ids are captured in variables because teardown needs them by number.

### 1.3 The capture run — the recorder opens BEFORE the fixture plays

```bash
cd /home/user/work/GoPi
( sleep 0.6; exec paplay --device=gopi_rig $RIG/fixture/front-center-20s.wav ) &
./bin/gopi tool voice_input --seconds 8 --record $RIG/cap/voiceinput-rig-8s.wav
```

- `paplay --device=gopi_rig` plays into the **sink**, never into the source.
- The `sleep 0.6` is the whole point (trap B in §3): a recorder's first sample
  arrives a fraction of a second after it is opened, and a monitor source is not
  buffered retroactively — whatever played inside that gap is simply not in the
  recording. Playing *after* the recorder starts is the working order; the fixture
  is 20 s so that the open ramp, the 8 s window and the sink's own warm-up (trap D
  in §3: the first seconds of a monitor stream arrive BELOW real time) all land
  inside it.
- **This run pays no device probe**, and that is by design, not luck: the selected
  id is the wildcard `pulse:@DEFAULT_SOURCE@`, and `voiceVerifyDevice` returns
  early for exactly that id (`cmd/agent/cli_voice_device_policy.go:260-263`) — a
  wildcard cannot be opened before it is resolved. A run that names a real source
  (`--device pulse:gopi_rig_rec`) DOES pay the 1 s liveness probe
  (`capture(dev, voiceVerifySeconds)`), which is why the rig below plays the
  fixture *after* the capture starts rather than before either shape.

**Real output of that invocation (verbatim, `$RIG/cap/stderr.txt` + stdout):**

```
[voice] engine local: models /home/user/work/funasr-models, runtime /home/user/work/runtime/llama.cpp/build/bin/llama-funasr-cli
[voice] recording 8s from pulse:@DEFAULT_SOURCE@ (PulseAudio default source (gopi_rig_rec)) [first enumerated]
[voice] captured 256000 bytes (8.0s of 16000Hz mono PCM16)
[voice] kept 256044 bytes of WAV (8.0s) in /home/user/workspace/gopi-voiceinput-rig/cap/voiceinput-rig-8s.wav
[voice] transcribed locally in 69.2s (chunk=15s)
Center, front, center, front, center, front, center, front, center, front, center.
```

exit code **0**; wall time 90.0 s (≈69 s of it is the local Fun-ASR-Nano decode,
not capture — `--seconds 8` bounds the *microphone*, not the run).

### 1.4 Measurement — stdlib only, no OCR, no "exit 0"

```bash
python3 - "$RIG/cap/voiceinput-rig-8s.wav" 8 <<'PY'
import sys, wave, struct, math
path, want = sys.argv[1], int(sys.argv[2])
w = wave.open(path,'rb'); n = w.getnframes(); sr = w.getframerate()
d = w.readframes(n); a = struct.unpack('<%dh' % (len(d)//2), d[:len(d)//2*2])
nz = sum(1 for x in a if x != 0)
pk = max(abs(x) for x in a); rms = math.sqrt(sum(x*x for x in a)/len(a))
print("bytes=%d frames=%d rate=%d sampwidth=%d" % (len(open(path,'rb').read()), n, sr, w.getsampwidth()))
print("duration=%.3fs requested=%ds delta=%.3fs" % (n/sr, want, abs(n/sr-want)))
print("peak=%d nonzero_samples=%d nonzero_ratio=%.4f rms=%.1f" % (pk, nz, nz/len(a), rms))
PY
```

```
bytes=256044 frames=128000 rate=16000 sampwidth=2
duration=8.000s requested=8s delta=0.000s
peak=15443 nonzero_samples=106195 nonzero_ratio=0.8296 rms=2374.2
```

Re-measured **on peerbox against its own copy of the artifact**, so the numbers are
not a transfer artifact (`sha256 3ba06d3bbdb356a7…` on both sides):

```
PEERBOX-REMEMBERED: frames=128000 rate=16000 peak=15443 nz=106195 ratio=0.8296 rms=2374.2
```

### 1.5 Teardown — modules out, default source back, nothing left

```bash
pactl unload-module $SRC_ID          # 34 — the remap-source
pactl unload-module $SINK_ID         # 33 — the null sink
pactl set-default-source alsa_input.pci-0000_00_1b.0.analog-stereo   # the value read BEFORE the rig was up
pactl list short modules | sort -k2 > $RIG/modules-after.txt
diff $RIG/modules-before.txt $RIG/modules-after.txt
```

```
default_source_before=alsa_input.pci-0000_00_1b.0.analog-stereo
default_source_after=alsa_input.pci-0000_00_1b.0.analog-stereo      # restored, re-queried
rig_sources_left=0                                                   # `pactl list short sources | grep -c gopi_rig`
modules_before=24
```

**One honest line about that `diff`:** it was NOT empty — modules **35/36**, a
`gopi-mic-rig2` sink + remap-source, appeared between the "before" and "after"
snapshots. Those are **not this rig's** (this rig's were 33/34) and not this
run's writing: another process on peerbox was building its own mic rig in the same
minute (the "before" snapshot also carries its predecessor `gopi-mic-rig`, module
30). Every module *this* run loaded is gone, the default source is the original,
and `grep -c gopi_rig` on both sources and modules is 0. The diff is recorded
here rather than hidden because a reader who re-runs §1.5 will see a non-empty
diff too, and must not read it as this rig leaking. **Re-verify with the resource
names, not with a table equality**: `pactl list short modules | grep gopi_rig`
must print nothing.

### 1.6 The rig scripts (evidence, non-branch)

`$RIG/rig-run.sh` (the run above), `$RIG/trap-run.sh` (traps A–C),
`$RIG/trap2-rate.sh` (trap D) — one-shot, invoked by hand, no cron/unit/service
(Rule 35). Copies with the logs and the artifacts:
`/home/user/.gopi/sessions/01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb/artifacts/`
(`rig-run.log`, `trap-run.log`, `trap2-rate.log`, `voiceinput-rig-8s.wav`,
`front-center-20s.wav`, `stdout.txt`, `stderr.txt`).

### 1.7 Re-verification on peerbox (a second, independent execution)

Why: the numbers in §1.4 were produced by the run that also wrote this document.
They were then re-measured from the raw archived bytes, and finally reproduced by a
**second live run** under fresh resource names — so nothing here rests on one
execution's say-so.

**Fig. 1 — archived artifacts, re-measured from the bytes.** Each PCM below is the
raw `parecord` output the trap scripts kept (not a re-recording), measured with a
44-line stdlib-only Python reader (session artifacts, `measure_wav.py`):

| artifact | bytes | duration | peak | non-zero ratio | RMS |
|---|---|---|---|---|---|
| `cap/voiceinput-rig-8s.wav` (the capture of §1.3) | 256044 | 8.000 s | 15443 | 0.8296 | 2374.2 |
| `fixture/front-center-20s.wav` | 1920078 | 20.000 s | 15487 | 0.8400 | 2426.4 |
| `cap/trapA-order-after.pcm` | 28880 | 0.902 s | 15229 | 0.9785 | 2140.6 |
| `cap/trapB-played-first.pcm` | 64010 | 2.000 s | **0** | **0.0000** | **0.0** |
| `cap/trapC-idle.pcm` | 64038 | 2.001 s | **0** | **0.0000** | **0.0** |
| `cap/rate-4s.pcm` | 33974 | 1.062 s | 15174 | 0.9813 | 2755.4 |
| `cap/rate-10s.pcm` | 251014 | 7.844 s | 15479 | 0.8506 | 2381.5 |

Every row equals its figure in §1.4/§3, on peerbox and on the session copy alike —
which is also what settles §1.4's "re-measured on peerbox" line: the capture
artifact's sha256 is `3ba06d3bbdb356a7…` on both hosts, byte-identical.

**Fig. 2 — a second live run on peerbox, fresh names.** §1.2–§1.5 re-executed with
`sink_name=gopi_rig_v2` / `source_name=gopi_rig_v2_rec` (disjoint from §1's names
and from the other rig then on the host), the fixture re-derived through §2's own
recipe, and the recorder's 0.6 s delay kept:

```
host=peerbox  tree=d90950195  ancestor_457a999b6_exit=0  binary_sha256=09bd0390e1e891dc…e7b7
default_before=alsa_input.pci-0000_00_1b.0.analog-stereo
sha256(fixture re-derived by §2) = 1e3f13495cae69c3…c6cd2   ← §2 is byte-reproducible
sink_module=45 remap_module=46   default_rigged=gopi_rig_v2_rec
voice_exit=0  wall_seconds=47.809673533
[voice] recording 8s from pulse:@DEFAULT_SOURCE@ (PulseAudio default source (gopi_rig_v2_rec)) [first enumerated]
[voice] captured 256000 bytes (8.0s of 16000Hz mono PCM16)
[voice] kept 256044 bytes of WAV (8.0s) in …/verify-rerun/voiceinput-verify-8s.wav
[voice] transcribed locally in 29.6s (chunk=15s)
stdout: Center, front, center, front, center, front, center, front, center, front, center, front.
unload_src_exit=0 unload_sink_exit=0    default_after=alsa_input.pci-0000_00_1b.0.analog-stereo
rig_modules_left=0 rig_sources_left=0 gopi_listeners=0
```

```
file=…/verify-rerun/voiceinput-verify-8s.wav  sha256=f08868343720c19b…6fd14
container=wav bytes=256044 frames=128000 rate=16000 sampwidth=2 channels=1
duration=8.000s requested=8.000s delta=0.000s
peak=15475 nonzero_samples=109273 nonzero_ratio=0.8537 rms=2356.5
```

**The one measured thing the re-run does NOT reproduce, and why.** The *verdicts*
reproduce exactly — duration 8.000 s on the nose, peak > 0, ratio ≫ 0, the same
word sequence in the transcript. The **file sha256 does not** (`f0886834…` vs
`3ba06d3b…`). That is the honest boundary of this rig, and it is a property of the
rig rather than a defect: the sink is not sample-locked to `parecord`'s first
sample, so each run captures a slightly different *window* (and phase, inside the
looped fixture) while the duration, the level and the transcript land on the same
values. A byte-identical WAV would need a locked stream (e.g. `module-loopback`
with a fixed latency), which this rig does not claim to be — and the epic's
acceptance is numeric on the artifact (duration ≈ requested, non-silent), not a
fixed digest. So read §1.4's sha256 as *the artifact of that run*, reproducible
from the archived bytes, never as a constant the rig promises.

Wall time also differs between the two runs (90.0 s vs 47.8 s; the local decode
69.2 s vs 29.6 s) — the engine's own cold/warm variance, not capture (§1.3).

**Fig. 3 — close-out re-measurement of the session copies.** Re-run a second time
over the **session directory's own copies** (`…/sessions/01a113e4-…/artifacts/`),
i.e. the bytes that survive `$RIG` being deleted, with the same all-equal result:

```
3ba06d3bbdb356a7d5ac1f0056e8993776bb3f8a8de77af39363cfa2babf902d  artifacts/voiceinput-rig-8s.wav
1e3f13495cae69c32d84185100ad81abaff67d71b883168b3cac993d4e4c6cd2  artifacts/front-center-20s.wav
voiceinput-rig-8s.wav bytes=256044 frames=128000 rate=16000 sw=2 ch=1 dur=8.000s peak=15443 nz=106195 ratio=0.8296 rms=2374.2
front-center-20s.wav  bytes=1920078 frames=960000 rate=48000 sw=2 ch=1 dur=20.000s peak=15487 nz=806402 ratio=0.8400 rms=2426.4
```

and the host state that must be true for §1.5's teardown to have held, re-queried
at close-out:

```
default_source=alsa_input.pci-0000_00_1b.0.analog-stereo   # the original, still
rig_modules=0    rig_sources=0    cron gopi_rig=0    paplay=0    parecord=0
```

so the rig is gone by resource name, the default source is the pre-rig value, and
no playback/recording process was left behind (the `gopi_listeners` count is the
one figure that needs the attribution §5 gives it — it reads 1 for an unrelated
`gopi web` test-budget process).

---

## 2. Fixture recipe (so the rig is reproducible)

```bash
ffmpeg -hide_banner -loglevel error -y \
  -stream_loop 14 -i /usr/share/sounds/alsa/Front_Center.wav \
  -t 20 -ac 1 -ar 48000 -c:a pcm_s16le \
  $RIG/fixture/front-center-20s.wav
```

- source: `/usr/share/sounds/alsa/Front_Center.wav`, 1.428 s, 48 kHz mono, a
  human voice saying "Front, center." — sha256 `0d61518bcd3f13b0…`; it is part
  of alsa-utils, so the recipe needs no asset that is not already on the host.
- `-stream_loop 14` + `-t 20` — the clip is short and the **opening second is
  discarded by the device probe**, so the fixture must be much longer than the
  recording: 20 s covers 1 s probe + 8 s capture with room to spare.
- result: 20.000 s, 48000 Hz, mono, 16-bit; 1920078 bytes; sha256
  `1e3f13495cae69c32d84185100ad81abaff67d71b883168b3cac993d4e4c6cd2`.
- `-c:a pcm_s16le` keeps it uncompressed: no decoder in the path, nothing to
  drift.
- **If you re-derive the fixture differently the *numbers* stay valid only in
  kind, not in value** — the acceptance is "non-silent, duration ≈ requested";
  a different clip gives a different peak/RMS but the same verdicts.

---

## 3. Reproduction points and traps (all measured, not asserted)

Each trap was measured with the **tool's own recorder argv**
(`voiceRecorderCommand`, pulse arm: `parecord --device=<name> --raw --rate=16000
--channels=1 --format=s16le`, `cmd/agent/cli_voice_client.go:421-430`), so what
follows is the same mechanism the tool runs, minus the transcription wait.

| # | setup | bytes | audio | peak | non-zero | RMS |
|---|---|---|---|---|---|---|
| A | **capture first, fixture 0.6 s later** | 28880 | 0.90 s | 15229 | **0.9785** | 2140.6 |
| B | fixture played **before** the capture opens | 64010 | 2.00 s | **0** | **0.0000** | 0.0 |
| C | idle sink, nothing ever played | 64038 | 2.00 s | **0** | **0.0000** | 0.0 |

**Trap B — "played first" is silence, not a quiet recording.** Playing the 1.4 s
clip before the recorder opens looks harmless and yields a **fully zero** buffer
(peak 0, ratio 0.0000). The monitor stream starts when the recorder opens, not
before; audio that finished earlier is simply not there. This is the trap the
0.6 s delay in §1.3 exists to avoid, and it is also why the fixture is looped to
20 s: a *short* clip can finish during the tool's 1 s liveness probe even when
you did start playback first.

**Trap C — an idle null sink is all-zero, and the tool is right to refuse it.**
With nothing playing, the same recorder returns 2 s of zeros. Against the tool
this is not a subtle failure: `voiceRecord` treats "no audio at all" as
`produced no audio from %q — the device is reachable but silent` (exit 1). A rig
built without playback therefore looks like a broken device, not like a silent
one — check the fixture is actually playing before blaming the tool.

**Trap D — an idle monitor does NOT deliver at real-time rate.** Measured with
continuous playback and the tool's recorder argv, `timeout -s INT W parecord …`:

| requested | wall | audio delivered | delivered rate |
|---|---|---|---|
| 4 s | 4.01 s | 1.06 s | 0.26× |
| 10 s | 10.00 s | 7.84 s | 0.78× |

`parecord` has no `-d`; the tool kills it at `--seconds + voiceCaptureGraceSec`
(10 s, `cli_voice_client.go:116-120`) and trims the result to `--seconds` — and
this table is the measured reason that grace exists: **reading `elapsed_ms` (or
wall time) as "audio captured" is wrong**, only the PCM byte count is. These two
figures are the **cold shape** (a sink created seconds earlier); in the real §1.3
run the same window returned the full 8.000 s with 0 % loss, measured — so the
cold ramp is not a constant, and the honest reading is "the first seconds of a
fresh monitor stream can arrive at a fraction of real time, which is why the tool
budgets a grace and why you should not size a fixture to the recording window
exactly".

**Trap E — record the *default* device, not a name.** If the run is given
`--device`, tier 1 wins and the rig proves nothing about default-device
resolution. §1.3 deliberately passes no `--device`.

---

## 4. Why this host, and what it had already

- peerbox is the host with PulseAudio (`/run/user/1000/pulse/native`,
  `alsa_input.pci-0000_00_1b.0.analog-stereo` as the original default source) and
  the local engine deployed in the two places the bare shape searches:
  `$HOME/work/funasr-models` (encoder + Qwen3 LLM) and
  `$HOME/work/runtime/llama.cpp/build/bin/llama-funasr-cli`. That is why the run
  in §1.3 needed **no** `--runtime`/models flags.
- **A second rig was live on the host during this run** (`gopi-mic-rig`, module
  30, then `gopi-mic-rig2`, modules 35/36) — i.e. another agent is exercising the
  same mechanism in parallel. This rig used disjoint names (`gopi_rig`,
  `gopi_rig_rec`) so the two cannot be confused; the default source was restored
  at the end of every one of this run's four invocations (§1.5, and each trap
  script restores its own).
- Reproducing §1 while that other rig is up is safe, but **re-read the default
  source before you start**: the "before" value is whatever it is at that moment,
  and the restore in §1.5 must name *that* value, not a remembered one.

## 5. Cleanup and dependency surface

- **No listener — and the one truth `grep -c gopi` cannot tell you.** The bare
  capture shape "never opens a port" by construction
  (`cmd/agent/cli_voice_local.go:30-42`), but the *measurement* of that claim needs
  care: `grep -c gopi` matches the **process name** `gopi` on ANY listening socket,
  and peerbox's test-budget runner was hosting
  `…/test-budget/run-…/tested-bin/gopi web --addr=127.0.0.1:0` (pid 1277479,
  started 11:56:23) while this was written — so a bare `grep -c gopi` reads **1 on
  an idle host**, as a later re-check of this document's own session measured:
  `gopi_listeners=1`, and `ss -ltnp | grep gopi` resolved to that `gopi web`
  process, not to anything this rig started. The claim is therefore stated as what
  was actually observed: the capture path opens **no socket at all** (it is local
  `parecord` + a local decode), this doc started no server, and any listener seen
  after a re-run must be attributed by its pid/argv before it is read as yours —
  an unattributed listener is not evidence about the rig either way. No server was
  started for this doc, and
  no `pactl` module loaded by it outlived §1.5 (re-queried: both `grep -c` on the
  resource names → 0).
- **No persistent dependency** (Rule 35): the rig is three `pactl` commands plus
  one `paplay`, run by hand inside one session. No cron line, no systemd unit, no
  daemon, no lock file, no new config key, no shell profile edit, no new binary.
  The three helper scripts under `$RIG/` are one-shot evidence kept OUTSIDE the
  checkout and outside `$HOME`'s config surface; nothing invokes them
  automatically, and deleting `$RIG` leaves no trace beyond the logs already
  copied into the session's artifact directory.
- **Nothing was written into a shared tree.** The scratch root is
  `/home/user/workspace/gopi-voiceinput-rig` (device `2052`, off `/`'s `2051`,
  Rule 22); the checkout of this run holds only the doc and its manifest row.

## Not Done (honest)

- **No product-code change is delivered** — by design: the epic's premise was
  that master already carries the bare-capture shape, and it does (see §0's
  header block: `457a999b6` is an ancestor of HEAD). This document is rig +
  evidence only.
- **ASR quality (CER) is not measured here.** The transcript is quoted as a
  second witness that the captured audio is the fixture; the epic's acceptance is
  numeric on the *capture artifact*, and the repo already owns the CER recipe
  (`scripts/asr_score.py`, `scripts/voice-e2e-fixture.sh`, devdoc
  `voice-client-e2e-hp.md`). A CER run was not executed, so no CER is claimed.
- **The deliverable was produced against `/home/user/work/GoPi/bin/gopi` on
  peerbox**, not against a binary built from this worktree: this branch changes no
  Go file (`git diff --stat master...HEAD` is empty), so the binary and this
  branch's tree are the same product code — stated here rather than left
  implicit.
- Trap numbers (B/C) were measured with the tool's recorder argv rather than by
  invoking `gopi tool voice_input` twice more; the reason is wall time (each
  invocation pays ~70 s of local decode) and the mechanism under test is the
  recorder open/playback ordering, which the argv-equivalent measurement shows
  exactly. The **end-to-end** number (§1.3/§1.4) is a real `gopi tool voice_input`
  invocation — and §1.7 adds a **second** real one, so the end-to-end claim rests
  on two executions, not one.
- **The capture is not byte-deterministic** across runs (§1.7): the verdicts are,
  the sha256 is not. Stated where a re-runner will meet it rather than only here.
- **`internal/standards` is RED on this base, for a reason that is not this
  branch.** `go test ./internal/standards/ -run TestDocClassBannerMatchesManifest`
  fails on `devdoc/cli-help-user-audit.md` (banner `historical`, manifest row
  `normative`) — and it fails **identically in `/home/user/work/GoPi` on master
  `3255973cc`**, so it is a pre-existing upstream condition, not something this
  document introduced. This document's own classification is green:
  `gopi tool doccheck` → *"the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row"* (exit 0). The upstream mismatch is left alone
  here on purpose: repairing another writer's row is a separate change with its
  own review (Rule 36), not a side effect of this one.
