<!-- doc-class: normative -->
# Voice input — the `--client` leg, end to end on HP (closes devdoc/voice-e2e-peer.md §7.3)

**Task:** prove the real **user** path — `gopi voice_input --client` captures on HP
and the transcript comes back — instead of the `--stdio` manual pipes the earlier
epic used (which silently mis-decode PCM16 as f32, devdoc/voice-e2e-peer.md §6.1).

| | |
|---|---|
| Capture node | `hp` = `hostb-Laptop` (192.0.2.5 / nickname `hp`), mic `alsa_input.pci-0000_05_00.6.analog-stereo` |
| Engine node | `peer1` = 192.0.2.6, Fun-ASR-Nano at `~/work/funasr-models` |
| Repo / branch | GoPi `feat/voice-client-e2e` (worktree `~/workspace/gopi-voice-client-e2e`) |
| Client binary | built on peer1 from this branch, `scp`ed to HP (`~/workspace/gopi-vce2e/bin/gopi`, `CGO_ENABLED=0`) |
| Evidence | `devdoc/assets/voice-client-e2e-peer/*.log` (raw, unedited) + this file |

**Where the epic's deliverables land:** A = §3 (A1/A2/A3/A1http) **+ §5's two
full-length runs**; B = §5 (**CER 3.76 %** aligned `--client`, 6.82 % misaligned,
scored with the same `scripts/asr_score.py` + pinned `ref.txt` as the earlier
`--stdio` evidence); C = §6 (+ §8a for the one fix it forced); D = §4; E = §7.
Everything was run on **2026-09-14**; §5/§5.3 and the F3 fix were added in a
second session that closed the two openings the first session left (the
un-aligned CER rig → root cause + aligned rerun; F3 reported-not-fixed → fixed
with tests).

---

## 1. Why this is not the earlier proof

The previous epic proved the *transport* with hand-built pipes:

```bash
parecord … | sox … -e floating-point -b 32 … | ssh peer1 "gopi voice_input --server --stdio"
```

That harness converts PCM16→f32 **by hand** (§6.1 documents that forgetting the
conversion yields `/sil` or a hallucination with no error). The user path is one
command: `gopi voice_input --client …` — the client owns device choice, the
recorder argv, the conversion and the transport. This report exercises *that*.

## 2. The deterministic microphone (no human, no acoustic path)

A human speaking is not reproducible and HP's builtin speaker→mic chain is a
lossy, saturating transducer pair (§6.2/§6.3 of the earlier report). So the
"microphone" is a Pulse graph device:

```bash
pactl load-module module-null-sink sink_name=gopi_src
pactl load-module module-remap-source master=gopi_src.monitor source_name=gopi_mic \
       source_properties=device.description=gopi_mic      # remap, NOT the raw monitor
paplay --device=gopi_src <fixture.wav>                     # the sound a mic would hear
gopi voice_input --client --device pulse:gopi_mic --seconds N --stdio
```

`gopi_mic` is an **ordinary enumerated capture source** (`--list-devices` lists
it, the policy probes it like any other), so the tier/hotplug logic is exercised
for real, while what it carries is a pinned file. The raw monitor is *not* used
directly: a monitor is a passive loopback with no client stream, and `parecord`
on it blocks (measured: SIGKILL at the 12 s deadline).

**Fixture:** rebuilt with the existing rig, `scripts/voice-e2e-fixture.sh` —
`clip.wav` sha256 `4031d08bf05e587f587c5dccde07b88d…` (219.94 s, 16 kHz mono
PCM16) and `ref.txt` (1011 normalised chars) — i.e. **the same ruler** as the
earlier `--stdio` evidence. The 12 s cut (`CUT_AT=12`, 1:1 with nothing) is used
only for the *short* runs, where its transcript is quoted as a fragment.

## 3. Deliverable A — real `--client` runs

All runs: client on HP, engine on peer1, HP does no LLM work except where noted.
Times are the shell's `real` for the whole command (`time` output verbatim in the
asset logs).

### A1 — HP client → **HP** server (self-loop `--stdio`)

```
$ gopi voice_input --client --device pulse:gopi_mic --seconds 16 --stdio > a1.f32
[voice] recording 16s from pulse:gopi_mic (gopi_mic) [explicit --device]
[voice] captured 512000 bytes (16.0s of 16000Hz mono PCM16)
real 0m37.072s                       ← includes the 1 s liveness probe + 10 s grace

$ gopi voice_input --server --stdio < a1.f32 > a1.txt
[voice] 1024000 bytes in, 9.3s (chunk=15s)
real 0m9.361s
TRANSCRIPT: 大语言模型早已不是工具。the
```

### A2 — HP client → **peer1** server over SSH stdio (the true user path)

```
$ gopi voice_input --client --device pulse:gopi_mic --seconds 16 --stdio > a2.f32
[voice] recording 16s from pulse:gopi_mic (gopi_mic) [explicit --device]
[voice] captured 512000 bytes (16.0s of 16000Hz mono PCM16)
real 0m36.6s

$ ssh peer1 'gopi voice_input --server --stdio' < a2.f32 > a2.txt
[voice] 1024000 bytes in, 5.3s (chunk=15s)
real 0m5.484s
TRANSCRIPT: 大语言模型早已不是工具。the
```

First, unaligned attempt (`a2.f32`, playback started 1 s *before* the recorder
opened) shows the rig — not the client — is what matters: the first ~12 s of the
capture is the tail of the previous play and the rest is silence, so the engine
returned a fragment (`工具。`, engine 3.7 s). The aligned runs above are the
evidence; the misalignment is recorded because it is a rig property (§5.2).

### A3 — HP client → **peer1** HTTP server on a non-default port

```bash
ssh peer1 'setsid nohup gopi voice_input --server --listen :7799 …'
# gopi voice_input: HTTP server on :7799 (engine=funasr-nano chunk=15s)
curl -s http://192.0.2.6:7799/health   # from HP: {"chunk":15,"engine":"funasr-nano","ok":true}

$ gopi voice_input --client --device pulse:gopi_mic --seconds 16 \
      --server-url http://192.0.2.6:7799
[voice] recording 16s from pulse:gopi_mic (gopi_mic) [explicit --device]
[voice] captured 512000 bytes (16.0s of 16000Hz mono PCM16)
[voice] transcribed in 5.1s
real 0m42.199s
TRANSCRIPT: 大语言模型早已不是工具。the
```

The HTTP leg was tested **after** the LAN caveat below was cleared, so it is real
evidence rather than a hopeful assertion:

1. a previous run of this port had been started as `--listen 127.0.0.1:7799` and
   the later `--listen :7799` start **failed** (`bind: address already in use`,
   logged) — the port stayed loopback-only and HP got `connection refused`;
   after freeing the port the LAN bind was confirmed (`ss -ltn` → `*:7799`,
   `/health` 200 from HP) and A3 succeeded. **Honest note:** the client's error
   message for this case is correct and actionable
   (`dial tcp 192.0.2.6:7799: connect: connection refused (start it with
   gopi voice_input --server --listen :7788)`), but it names the *default* port
   while the user passed `--server-url …:7799`; a reader could be sent to the
   wrong port. Cosmetic, reported not fixed.

### A1http — HP client → **HP** HTTP server (loopback)

`--client` POSTs multipart instead of piping; 46.1 s wall, `[voice] transcribed
in 9.0s`, transcript `大语言模型早已不是工具。the`. (HP as *server* is §4.)

## 4. Deliverable D — HP as an engine node (done, not skipped)

HP had no engine assets (`ls ~/work/funasr-models` → No such file). Synced:

```bash
rsync -a peer1:/home/user/work/funasr-models/                 hp:~/work/funasr-models/
rsync -a peer1:…/llama.cpp/build/bin/llama-funasr-cli          hp:~/work/runtime/llama.cpp/build/bin/
# 1.7 G + 6.0 M, 25.3 s + 0.34 s over gigabit; the CLI is a static-ish binary
# (libgomp/libstdc++/libm/libc only) so no build step is needed on HP.
```

**Which model is the default — read from the code, not guessed**:
`cmd/agent/cli_voice.go:80` `voiceDefaultModelRoot = "work/funasr-models"`,
`:81` `voiceDefaultRuntime = "work/runtime/llama.cpp/build/bin/llama-funasr-cli"`,
joined to the home directory at `:343` (`filepath.Join(userHomeDirOrDefault(),
voiceDefaultModelRoot)`) — and `userHomeDirOrDefault` is
`os.UserHomeDir()` (`env HOME`, **not** `GOPI_HOME`) with a "." fallback. Of the
three GGUFs in the directory the engine is handed **`funasr-encoder-f16.gguf`
+ `qwen3-0.6b-q4km.gguf`** (`voiceEncoderName`/`voiceLLMName`); the third
(`qwen3-0.6b-q8_0.gguf`) is never referenced.

**Settings-path resolution (the other half of the question):** settings.json is
resolved through `config.ResolveSettingsPath()` → `internal/config/gopihome.go`
`GopiDir()` — `$GOPI_HOME/.gopi` when `GOPI_HOME` is set (env or `--home`),
otherwise `$HOME/.gopi` via `os.UserHomeDir`; **never** the cwd. HP's own
`~/ .gopi/settings.json` is read for `default_device`; it has none (only the dead
GLM LLM config, which `voice_input` never touches — confirmed by A1/A2/A3 running
with no LLM call).

Result (HP transcription, real run):

```
$ gopi voice_input ~/workspace/vce2e-cut.wav --chunk 15
[voice] vce2e-cut.wav in 8.0s (chunk=15s)
TRANSCRIPT: 2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。大语言模型早已不是工具。
```

i.e. HP is a working standalone engine node at ~0.7× realtime (8.0 s for 12 s of
audio, `user 34.4s` across 8 threads) — no ssh, no peer1.

## 5. Deliverable B — CER on the `--client` path

`scripts/asr_score.py` against the pinned `ref.txt` (the same scorer the
`--stdio` evidence used). The short (16 s) runs capture a 12 s *fragment* of the
fixture, so their transcript is scored the way the earlier report warns is
misleading — against the whole 1011-char reference:

| Evidence | transcript | CER (fragment vs whole ref) |
|---|---|---|
| A1 / A2 / A3 / A1http (16 s capture of the 12 s cut) | `大语言模型早已不是工具。the` | 98.71 % (scorer output in `assets/cer-16s-score.log`) |

That number is **not an ASR quality figure** (a 12 s fragment cannot score against
a 220 s paragraph) — it is reported so the ruler is identical and the reader can
see why the full-length run exists:

**Full-length CER run — done** (raw: `assets/vce2e-run-CER.log`, transcript
`assets/vce2e-cer-full.txt`, reference `assets/vce2e-ref.txt` = the HP fixture
`ref.txt`, sha256 `aea95730f5cee904…`, 3192 bytes; score log
`assets/cer-full-score.log`):

```bash
$ $BIN voice_input --client --device pulse:gopi_mic --seconds 228 --stdio > out/cer.f32 &
$ sleep 2; paplay --device=gopi_src ~/workspace/vce2e-clip.wav   # paplay_rc=0 at 18:24:41
[voice] recording 228s from pulse:gopi_mic (gopi_mic) [explicit --device]
[voice] captured 7296000 bytes (228.0s of 16000Hz mono PCM16)     # client_rc=0 at 18:25:06
$ time ssh peer1 'gopi voice_input --server --stdio' < out/cer.f32 > out/cer.txt
real 5m16.399s
[voice] 14592000 bytes in, 315.3s (chunk=15s)                     # server_rc=0
$ python3 scripts/asr_score.py out/cer.txt ref.txt
  chars (norm)      hyp=992  ref=1,011
  edit distance     69
  CER (char error)  6.82%
  accuracy          93.18%
  --- latin/tech terms ---  kept 15/16 (94%)
--- transcript (head) ---
大语言模型早已不是工具，而是嵌入生活机里的硅基器官。每个人自出生起就会被分配一个基础私人大模型…
```

**CER 6.82 %** on the `--client` path, scored by the same
`scripts/asr_score.py` against the same pinned 1011-char reference the earlier
`--stdio` table used — i.e. the two are directly comparable (the earlier epic's
4-minute run was never scored, which is exactly the gap this run closes).

**Root cause found for the missing opening (and the aligned rerun that proves
it).** In the run above, playback started on a guessed `sleep 2` after launch —
*inside* the client's pre-capture window. The client does not record during that
window: it runs a real 1 s liveness probe and then waits `voiceCaptureGraceSec =
10` (`cmd/agent/cli_voice_client.go:110/116`) before spawning the recorder, so
the recipe played the fixture's first ~15 s into a sink that had no capturing
client yet and PulseAudio discarded it (a null-sink carries nothing while
nothing reads it — this is why the 30 s head-probe reads exactly like the full
run: the *content* starts at the same offset in both).

The fix is in the rig, not in the code: play a WAV that carries **20 s of
digital silence in front of the fixture** and start it the moment the pre-record
line is printed (the line is printed before the probe, so the pad covers it
with margin). `assets/vce2e-run-CER4-pad250.log`, transcript
`assets/vce2e-cer4-pad250.txt`, score `assets/cer4-score.log`:

```bash
$ $BIN voice_input --client --device pulse:gopi_mic --seconds 250 --stdio > out/cer4.f32 &
$ # wait for the printed pre-record line, then play the PADDED fixture
[voice] recording 250s from pulse:gopi_mic (gopi_mic) [explicit --device]
$ paplay --device=gopi_src ~/workspace/vce2e-clip-pad.wav      # paplay_rc=0 (19:02:33)
[voice] captured 8000000 bytes (250.0s of 16000Hz mono PCM16)  # client_rc=0 (19:02:52)
$ time ssh peer1 'gopi voice_input --server --stdio' < out/cer4.f32 > out/cer4.txt
real 17m49.511s        # 250 s of audio = 1067.7 s engine on peer1 (4.3x realtime, loaded host)
[voice] 16000000 bytes in, 1067.7s (chunk=15s)                 # server_rc=0
$ python3 scripts/asr_score.py out/cer4.txt ref.txt
  chars (norm)      hyp=1,027  ref=1,011
  edit distance     38
  CER (char error)  3.76%
  accuracy          96.24%
  --- latin/tech terms ---  kept 13/16 (81%), MISSING ['ar','vr']
--- transcript (head 200) ---
/sil两千零八十四无言见人序章裸模型时代二零八四年人类终于驯服了语言却也沦为语言的囚徒大语言模型早已不是工具，而是嵌入生活机里的硅基器官…
```

**The aligned `--client` CER is 3.76 %** (vs 6.82 % misaligned), and the opening
now comes back — `2084无言见人序章裸模型时代` is in the transcript (the `/sil`
prefix is the engine's own marking for the 20 s silent pad, i.e. the rig's
silence is described by the engine in one token, exactly as the pad-probe
predicted). The unaligned 6.82 % run, the aligned 3.76 % run, and the pristine
WAV baseline (`assets/vce2e-run-baseline.log` — the fixture piped straight into
the same `--server --stdio`, `2084无言见人序章裸模型时代，2084年人类终于驯服了语言…`)
are now three points of the same curve and they agree: the engine returns the
reference's opening whenever it is inside the capture window, and only the rig's
playback/capture offset ever kept it out. **The previous subsection's "missing
19 characters" was this offset, not ASR loss and not a transport loss** — the
bound is now an explained measurement rather than an open residual.

### 5.1 The exact commands

```bash
# on HP, with the rig (gopi_src sink + gopi_mic remap source) up:
$BIN voice_input --client --device pulse:gopi_mic --seconds 250 --stdio > cer4.f32 &
# start playback only after the printed pre-record line, and play a PADDED wav
paplay --device=gopi_src ~/workspace/vce2e-clip-pad.wav
ssh peer1 'gopi voice_input --server --stdio' < cer4.f32 > cer4.txt
python3 scripts/asr_score.py cer4.txt ref.txt
# the misaligned variant (launch-time sleep) is kept as the contrast case:
$BIN voice_input --client --device pulse:gopi_mic --seconds 228 --stdio > cer.f32 &
sleep 2; paplay --device=gopi_src ~/workspace/vce2e-clip.wav
ssh peer1 'gopi voice_input --server --stdio' < cer.f32 > cer.txt
python3 scripts/asr_score.py cer.txt ref.txt          # 6.82 %, opening lost
```

### 5.2 The rig's honest limits (measured, not theorised)

* `paplay` blocks until the playback drains, so the *end* of playback is knowable
  but the *start* is only "2 s after the recorder opened"; a run whose playback
  had not started yet captured the previous play's tail plus silence and the
  engine returned a 2-character fragment. The recorder must be told the full
  length (`--seconds N`), the fixture must be played once — and **playback must
  start only after the client's pre-record line, with the played WAV padded**
  (see §5: the client probes 1 s and waits 10 s before the recorder exists, so
  anything played inside that window is discarded by the null sink, and the
  fixture's opening is lost even though the capture length is nominal).
* The same run also measures the engine's THROUGHPUT, and peer1 is not idle:
  250 s of audio took `1067.7s` (4.3× realtime) in the aligned run vs `315.3s`
  (0.79× realtime) in the 228 s run earlier the same evening — the identical
  binary and model, a ~5.5× spread driven by host load. Any latency budget taken
  from a single run on a shared box is worth ±an order of magnitude; use the
  `--chunk`-stability probe below when the number matters.
* A 16 s capture takes 37 s wall: the policy's liveness probe is a real 1 s
  capture, and `voiceCaptureGraceSec = 10` is added to every capture
  (`cmd/agent/cli_voice_client.go:110`) — so `--seconds N` costs about N + 13 s
  even when the recorder is asked for exactly N.
* The remap source is a *second* graph node behind the monitor; a 12 s capture
  alignment is exact (verified: 16 s capture = one fragment sentence + silence,
  §A2).

### 5.3 Two engine-behaviour probes the rig needed (both real runs, both kept)

**(a) How `--chunk` interacts with the file, and what silence does.** The
server passes the audio file to the engine with `--chunk N` and **no
`--hotword`/VAD flag** (`cmd/agent/cli_voice.go:445-459`), so segmenting is the
engine's own. Two runs, same padded input (15 s digital silence + the first 30 s
of the `--client` capture), engine on peer1 (`assets/vce2e-run-padprobe.log`):

| run | output head | wall |
|---|---|---|
| `ssh peer1 'gopi voice_input --server --stdio'` (default `chunk=15`) | `/sil工具，而是嵌入生活机里的硅基器官。…` | 36.6 s for 45 s |
| same, `--chunk 1` | `thethethet…the工具，而是其。陷入生活积累的。…` (repetition + garbled) | 66.1 s for 45 s |

Two facts worth keeping: (1) the engine **does** account for a leading silence
chunk with the `/sil` marker, so the padded rig is not feeding garbage; (2) a
small `chunk` on a long file is *much* worse (garbling + 2× slower) — `--chunk
15` (the default) is the right value, and the doc's "NOT optional for long
audio" is an under-statement, a small value is actively harmful.

**(b) The pristine fixture through the same server = the baseline row.**
`ssh peer1 'gopi voice_input --server --stdio' < ~/workspace/vce2e-fixture/clip.wav`
(the fixture WAV, 7 038 140 bytes, straight into the same binary) →
`2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。大语言模型早已不是工具…`,
rc=0, `7038140 bytes in, 247.4s (chunk=15s)`, wall 248.4 s
(`assets/vce2e-run-baseline.log`). This is the control that settles §5's
question in one command: the opening IS transcribed when it is inside the input.

**(c) A note for whoever reruns the rig: the FIFO source is the wrong tool
here.** HP also carries a `module-pipe-source` (`gopi_pipe`, `s16le 1ch
16000Hz`), and feeding the fixture into its FIFO with `ffmpeg | dd` looked like
the cleanest alignment rig. It was tried and abandoned, both halves measured
(`assets/vce2e-run-FIFO.log`): HP's `ffmpeg` is broken
(`libopenmpt.so.0: undefined symbol: mpg123_param2`, rc=127) and, independently,
the client's own liveness probe on `pulse:gopi_pipe` failed
(`parecord … signal: killed`) because the FIFO has no writer yet — the probe
kills a source that is silent and unconnected. The null-sink + remap source with
a padded WAV is the rig that works; do not re-derive this one.



## 6. Deliverable C — the six device-policy questions (real commands, real output)

Raw: `devdoc/assets/voice-client-e2e-peer/vce2e-audit.log`.

**C.1 explicit `--device`, success and loud failure.** Success:
`--device pulse:alsa_input.pci-0000_05_00.6.analog-stereo --seconds 2 --stdio` →
`[voice] recording 2s from pulse:alsa_input…(…) [explicit --device]`,
`captured 64000 bytes`, rc=0. Failure:
`--device pulse:no_such_source_xyz` →
`unknown device "pulse:no_such_source_xyz" — available: alsa:hw:…, alsa:plughw:…,
pulse:@DEFAULT_SOURCE@, pulse:alsa_input…`, rc=1. Card-index spellings (`1`,
`hw:1,0`, `plughw:1,0`) are each refused with the by-name remedy, rc=1.

**C.2 configured default honoured, and stale-reported-then-continue.**
`GOPI_VOICE_DEVICE=pulse:gopi_mic` → `[configured default]` on gopi_mic.
`GOPI_VOICE_DEVICE=pulse:gone_mic` →
`[voice] configured default device "pulse:gone_mic" (GOPI_VOICE_DEVICE) is not
present — falling through to the next rule` then
`[first enumerated]`, rc=0 (no abort). Explicit beats configured in the same run.

**C.3 tier 3 vs tier 4 with PulseAudio UP.** `pactl info` answers on HP
(`Server Name` present) → tier 3 is skipped by construction, and a run with no
`--device`/no configured default prints
`[voice] recording 3s from pulse:@DEFAULT_SOURCE@ (…) [first enumerated]`
— i.e. **tier 4, not tier 3**, which is the policy's stated goal. Tier 3's
negative arm was then driven for real by pointing Pulse at a dead socket
(`PULSE_SERVER=unix:/nonexistent-pulse-sock`, so `pactl info` fails):
`[voice] pulse unavailable (pactl: exit status 1 — Connection refused …)` and the
policy consulted `alsa:default`. **Finding:** on HP that consultation failed
(`arecord … Device or resource busy`) and the failure was **swallowed** — the run
fell through to tier 4 and printed only tier 4's own busy error, with nothing
saying tier 3 had been tried. Failure mode for that run: rc=1 with the reader
blaming `plughw:CARD=Generic_1,DEV=0`. Fixed (§7/§8, F2).

Re-measured this session on HP with the fixed binary (`PULSE_SERVER` pointed at a
dead socket, no `--device`, no configured default) — the tier-3 failure is now
reported before tier 4 speaks, and the run still ends rc=1 with the *right*
diagnosis:

```
$ PULSE_SERVER=unix:/nonexistent-pulse-sock ./bin/gopi2 voice_input --client --seconds 2 --stdio
[voice] pulse unavailable (pactl: exit status 1 — Connection failure: Connection refused | pa_context_connect() failed: Connection refused)
[voice] system default capture "alsa:default" is not reachable — falling through to the next rule: device alsa:default is not reachable: arecord failed on "alsa:default": exit status 1 — ALSA lib pcm_dsnoop.c:601:(snd_pcm_dsnoop_open) unable to open slave | arecord: main:831: audio open error: No such file or directory …
gopi voice_input: device alsa:plughw:CARD=Generic_1,DEV=0 is not reachable: arecord failed on …: Device or resource busy (the device is held by another process — on a desktop that is the sound server, so pick a pulse: device from --list-devices) …
rc=1
```

Note what the middle line says about the *rig*, not the policy: with PulseAudio
pointed at a dead socket, ALSA's `default` on HP resolves through
`pcm_dsnoop` (the desktop sound-server passthrough plugin) and fails with
`No such file or directory` — i.e. on THIS host `default` is not a
server-independent route. That is a real-host fact for question E.2 (a pure-ALSA
host has no `dsnoop` shim to break), and it is why tier 3's failure being
*visible* matters: the old output blamed `plughw`, which was never the tier that
failed.

**C.4 `--list-devices`.** `[voice] 4 capture device(s)` and, in order:
`pulse:@DEFAULT_SOURCE@` (label names the concrete default
`alsa_input.pci-0000_05_00.6.analog-stereo`), `pulse:alsa_input.…`,
`alsa:plughw:CARD=Generic_1,DEV=0`, `alsa:hw:CARD=Generic_1,DEV=0`. With the rig
up, `pulse:gopi_mic`/`pulse:gopi_pipe` appear too (6 devices) — the list is live
enumeration, not a static table. Monitors are deliberately absent (§7).

**C.5 hotplug.** The selected device was made to vanish in the window between
enumeration and capture (`( sleep 0.05; pactl unload-module <remap> ) &` before
the run): the policy's pre-capture open fails and the run fails **by name with
the re-list advice**, in 0.09 s (no hang, no silent fallback):

```
gopi voice_input: device pulse:gopi_mic is not reachable: parecord failed on
"pulse:gopi_mic": exit status 1 — Stream error: No such entity (no capture device
by that name — it may have been unplugged or taken: run `gopi voice_input
--list-devices` to see what is there now) — it may have been removed or taken
since it was listed; run `gopi voice_input --list-devices` again to see what is
there now
```

**C.6 missing vs busier — the classification.**

| Device named | class | evidence |
|---|---|---|
| `alsa:plughw:CARD=Generic_1,DEV=0` (present but Pulse owns the PCM) | **BUSY, classified distinctly** | `… audio open error: Device or resource busy (the device is held by another process — on a desktop that is the sound server, so pick a pulse: device from --list-devices)` |
| `alsa:plughw:CARD=Generic_1,DEV=5` (card named correctly, device absent) | missing | `unknown device … — available: …` (never opened) |
| `pulse:gone_mic` | missing | `configured default device … is not present` |
| `pulse:gopi_mic` after unload | missing *after being enumerated* | `No such entity … may have been unplugged`, plus the same "removed or taken since it was listed" tail as BUSY |

The two paths share the trailing "may have been removed or taken since it was
listed" sentence and add the cause-specific hint before it, so BUSY is **not**
reported as a dead mic — the requirement is met.

## 7. Deliverable E — the two operator questions

**E.1 Capture on host-b / RPi5, transcribe on peer1 — feasibility, latency,
dependencies.**

*Reachability measured now:* `ssh host-b` → *could not resolve hostname*;
`ssh rpi5` → `192.0.2.14 … No route to host`. **Neither is reachable from
peer1 at this moment**, so nothing here was run on them — the answer is a
requirements analysis from the measured HP leg, not a claim of having executed on
those hosts.

*What the remote client needs on the capture host* (all measured present on HP):
`arecord` (alsa-utils), `parecord`+`pactl` (pulseaudio-utils), `ssh` (client
side), and a `gopi` binary — the same static `CGO_ENABLED=0` build used here
(`scp` + `chmod +x`; building on the target is unnecessary). Zero models, zero
Python, zero LLM credentials: `--client` only records and forwards.
*A sound server matters:* with PulseAudio up the ALSA `plughw/hw` spellings are
busy and the working ids are `pulse:…`; on a pure-ALSA host the ids are
`alsa:plughw:CARD=…,DEV=0` and tier 3 (`alsa:default`) is the intended route.

*Two transports, both measured from HP:*
| Transport | HP→peer1 measured | Ports | Needs |
|---|---|---|---|
| `--client --stdio \| ssh peer1 'gopi voice_input --server --stdio'` | engine 5.3 s for 16 s audio; client 36.6 s | **none** (ssh only) | ssh reachability + host key trusted |
| `--client --server-url http://192.0.2.6:7799` | client 42.2 s, server `transcribed in 5.1s` | one TCP port, must be `--listen :PORT` (not loopback) | LAN reachability; the endpoint is unauthenticated and accepts up to 512 MiB/request |

*Latency budget* (HP measured): `--seconds N` costs ≈ N + 13 s wall (1 s liveness
probe + 10 s `voiceCaptureGraceSec` + startup), the engine ≈ 0.33–0.7× realtime
on the 8-thread HP / faster on peer1, and the transport is a pipe (no measurable
overhead: 1 MB of f32 crossed in the same second the engine start was logged).
A host-b/RPi5 client would add only its own mic-open time plus the network.

*ssh precondition (this bit me, so it is called out):* on a **fresh** client node
the first `ssh peer1 …` fails with `Host key verification failed`
(measured with `StrictHostKeyChecking=yes`: *No ED25519 host key is known for
192.0.2.6*), and — measured — **`ssh-keyscan` cannot be used before that**,
because `peer1` is not in DNS/`/etc/hosts` on HP (`getaddrinfo peer1: Name or
service not known`); scan the **address** (`ssh-keyscan -t ed25519 192.0.2.6 >
~/.ssh/known_hosts`), which then also satisfies the alias defined by
`~/.ssh/config`'s `Host peer1 → HostName 192.0.2.6` (verified: strict-mode ssh
by IP **and** by alias both succeed afterwards).

**E.2 Can RPi5's USB mic be attached (a pure-ALSA host)?**

Yes in principle, and the policy is built for exactly that host, but two
prerequisites and one honest caveat:

1. **A sound server is not required** — that is the point of tier 3: with no
   `pactl info` answer the policy offers ALSA's `default` (the plug chain that
   accepts 16 kHz mono; a raw `hw:` spelling cannot, measured in the 1/6 matrix)
   and falls back to the first enumerated `alsa:plughw:CARD=…,DEV=0`.
2. **`ssh-keyscan` is the prerequisite** in the sense above: the *host-b/RPi5 →
   peer1* hop is a client-side ssh, so the RPi5 needs peer1's host key trusted
   (`ssh-keyscan -t ed25519 192.0.2.6 >> ~/.ssh/known_hosts`) and a key-based
   login (BatchMode must succeed — no password prompt: the client has nowhere to
   prompt).
3. **Caveat found here:** a *named monitor* is not resolvable by `--device` on a
   pure-ALSA host — monitors are Pulse-only, and the ALSA plugin `default` and
   the enumeration-suppressed monitors are the two names the policy does not
   accept today (ALSA `default` is only *synthesised*, never *resolved*; §7).
   On a pure-ALSA RPi5 the working spelling is `alsa:plughw:CARD=<card name>,DEV=0`
   from `--list-devices`. `alsa-utils` (`arecord`) must be installed — it is the
   recorder the ALSA backend spawns.

## 8. Failures found and what was done (first-class deliverable)

| # | Finding | class | action |
|---|---|---|---|
| F1 | A named Pulse **monitor** is unresolvable: monitors are excluded from `--list-devices` (deliberate) **and** rejected by `--device` — while `voiceResolveMonitorSource`'s sibling comment documents the opposite ("It stays reachable by naming it explicitly") and the E2E capture rig's source is a monitor. Measured: `--device pulse:gopi_src.monitor` → `unknown device` while the rim's audio was demonstrably present. | **code defect** | fixed: a monitor named explicitly resolves if the server still lists it, labelled `(monitor — captures what is PLAYING, not a microphone)`; `TestVoiceResolveMonitorSourceByName` (incl. a "no longer listed" arm and a non-monitor arm that must NOT consult the server) |
| F2 | A considered **tier 3 that fails is silent**: with `pactl info` failing and `alsa:default` busy, the run falls through to tier 4 and prints only tier 4's error — the reader blames the wrong device. | **code defect** (Rule 15b) | fixed: the tier appends `system default capture "alsa:default" is not reachable — falling through to the next rule: …`; `TestVoiceSelectionSystemDefaultFailureIsReported`. Re-measured on HP this session with the fixed binary — full output in `assets/vce2e-run-F2fix-tier3.log`, quoted in §6 C.3. |
| F3 | `--device alsa:default` / `--device default` → `unknown device`, although `-h` says `--device D … (default "default" — the plug chain …)`. | **code defect** (help/behaviour contradiction) | **fixed** (§8a): both spellings now resolve to the card-less ALSA plug device and are then PROVEN by the liveness probe, so an absent/busy `default` fails by its own name instead of "unknown device". Measured on HP after the fix (same binary, same host, PulseAudio up): `--device default` → `[voice] recording 2s from alsa:default (default (ALSA's configured capture device)) [explicit --device]`, `captured 64000 bytes`, rc=0 — for **both** spellings; `--device pulse:typo` still `unknown device …` (rc=1). |
| F4 | The HTTP "connection refused" error names the **default** port while the user passed `--server-url …:7799`. | cosmetic, NOT fixed | reported here; message is otherwise correct and actionable |
| F5 | `--seconds N` costs N + ~13 s even when the recorder is asked for exactly N (probe + grace). | intended (documented in code), reported | no change; it explains the wall-clock column of every run above and the earlier epic's "228 s took longer" |
| F6 | HP's `~/work/funasr-models` was absent → HP could not serve. | environment | fixed by rsync (§4); a documented one-liner for any future engine node |

**Red proof for both code fixes** (Rule 10.4): with the two hunks reverted
(monitor lookup removed, tier-3 note removed) the two new tests fail —

```
--- FAIL: TestVoiceResolveMonitorSourceByName/a_listed_monitor_resolves_by_name
    a monitor named explicitly must resolve: unknown device "pulse:gopi_src.monitor" — available: pulse:@DEFAULT_SOURCE@
--- FAIL: TestVoiceSelectionSystemDefaultFailureIsReported
    notes "" must report that tier 3 ("default") was tried and failed
```

— and both pass on the fixed tree. The reverted binary was also scanned by hand
against HP: `--device pulse:gopi_src.monitor` rc=1 (old) → rc=0 (new, with the
monitor label printed).

**Red proof for the F3 fix** (added this session; same method): the resolution
hunk was removed from `voiceResolveDevice` and the two new tests fail —

```
--- FAIL: TestVoiceSelectionSystemDefaultNamedExplicitlyPlaysOut (0.00s)
    error "unknown device \"default\" — available: pulse:@DEFAULT_SOURCE@, …" must name "alsa:default"
    error … must name "Device or resource busy"
    the system default is a real device — the error must not call it unknown
FAIL	github.com/the operator/gopi/cmd/agent
```

— then restored, and the whole `TestVoice*` set is green. On the real host the
same before/after is in `assets/vce2e-run-F3fix.log`: the OLD binary answered
`unknown device` for both `default` and `alsa:default`; the NEW binary records
from `alsa:default` (rc=0, `captured 64000 bytes`) while `--device pulse:typo`
still fails as `unknown device`.

### 8a. The F3 fix, in code terms

`voiceResolveDevice` (`cmd/agent/cli_voice_client.go`) resolves against the
**enumerated** set, and ALSA's `default` can never be in it — `arecord -l`
enumerates CARDS, while `default` is a card-less plugin the ALSA library
synthesises. Tier 3 already knows that (`voiceSystemDefaultDevice()`,
`cli_voice_device_policy.go`), so the two spellings now resolve to that same
entry (one definition, Rule 1b/13) and then go through the normal
`voiceVerifyDevice` liveness probe. Nothing is accepted silently: a `default`
that is absent or busy fails **by its own name and its own reason**, which is
exactly the classification the epic asks about (C.6). Two tests pin it:
`TestVoiceResolveALSADefaultSpelling` (both spellings, and a near-miss that must
NOT resolve) and `TestVoiceSelectionSystemDefaultNamedExplicitlyPlaysOut` (the
HP shape: named explicitly, busy, error names `alsa:default` + `Device or
resource busy` + the re-list flag, never "unknown device").

Scope note: this is CLI behaviour, not a tool wire name, description or prompt
routing change — Rule 11's LLM-adoption gate does not apply (no new/changed tool
the model must be persuaded to call). The change is verified by unit tests plus
two real runs on HP.

## 9. Verification

| Check | Result |
|---|---|
| `go build ./...` (worktree, final tree) | OK |
| `go test ./cmd/agent/ -run TestVoice -count=1` | **ok** — includes the 2 tests added by this session's F3 fix |
| `go test ./internal/config/ -count=1` | **ok** |
| `gofmt -l cmd/agent/cli_voice_client.go cmd/agent/cli_voice_client_test.go` | clean |
| F3 red proof | with the F3 hunk removed the two new tests FAIL (`unknown device "default"` instead of the busy-device error) and pass with it — quoted in §8 |
| Client binary built from this branch, run on HP | all §3/§4/§5/§6 runs use it (`~/workspace/gopi-vce2e/bin/gopi`, CGO_ENABLED=0) |
| A1/A2/A3/A1http | 4/4 rc=0 with transcripts printed above |
| `--client` CER (the deliverable) | **3.76 %** aligned (`cer4`), 6.82 % misaligned (`cer`) — both scored with `scripts/asr_score.py` against the pinned `ref.txt` |
| Control | pristine fixture → same `--server --stdio` transcribes the opening (baseline log) |
| HP as engine node | real transcript, 8.0 s for 12 s audio |
| Six policy questions | all answered with command + output (§6) |
| Raw evidence | `devdoc/assets/voice-client-e2e-peer/` (25 logs/transcripts, unedited) |

**Two red trees that are NOT this change** (checked, because a green claim needs
the comparison):

* `go test ./internal/standards/` fails on `internal/shot/x11.go:129`
  (`x11UnixSocketDir = "/tmp/.X11-unix"` flagged by the Rule 22 literal gate).
  Reproduced on a **clean checkout of this branch's HEAD** in a separate
  worktree (`~/workspace/gopi-vce2e-baseline`, `git worktree add … HEAD`) —
  identical failure, file untouched by this task.
* `go test ./cmd/agent/ -count=1` (whole package, ~600 s under heavy host load)
  exceeded Go's 10-minute default test timeout on the contended box. The voice
  subset used for this task's verification is green, and the file the change
  touches is exercised by that subset; the timeout was not attributed further
  because the package's own long-running worker/e2e tests are the likely
  consumer of the budget on a host running 17 concurrent `go test` processes.

## 10. Not done (honest)

1. **host-b and RPi5 were not testable**: neither resolves/routes from peer1
   (measured above), so D/E's answers there are requirements analysis, not runs.
2. **No physical-mic run**: every run uses the deterministic remap rig, by
   design (reproducibility); the earlier report already proved the physical
   `default` path for the `--stdio` shape, and doing it again through the speaker
   would add the acoustic channel's saturation (§6.2 there), not information
   about this leg.
3. **F4 not fixed** (reported, with reason); **F3 is now fixed** — see §8.
4. **HP's settings.json still points at the dead GLM config** — irrelevant to
   `voice_input` (no LLM call in any run above); not touched, so HP's operator
   state is unchanged.
5. **The HTTP server on peer1 `:7799` and HP's loopback `:7799` were left STOPPED**
   after evidence collection (verified `ss -ltn`), and the rig modules
   (`gopi_src`, `gopi_mic`, `gopi_pipe`) were unloaded except where §6 re-created
   them for a run; `pactl list short sources` returns to its original count.
   `~/workspace/vce2e-*` (HP and peer1) hold the fixture, binary and raw captures
   for rerun — all under `$HOME/workspace`, never `/tmp` (Rule 22: verified with
   `gopi_assert_off_root_fs` / device-id on both hosts; the rig script itself
   uses the repo's `scripts/lib/tmp-ban.sh` guard).

## 11. Rerun

```bash
cd ~/workspace/gopi-voice-client-e2e                        # branch feat/voice-client-e2e
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
scp bin/gopi hp:~/workspace/gopi-vce2e/bin/gopi && ssh hp 'chmod +x ~/workspace/gopi-vce2e/bin/gopi'

# rig (on hp; off-/ scratch paths only, Rule 22)
ssh hp 'pactl load-module module-null-sink sink_name=gopi_src'
ssh hp 'pactl load-module module-remap-source master=gopi_src.monitor source_name=gopi_mic'

# the client leg, both transports (off-`/` paths; never /tmp — Rule 22)
bash scripts/voice-e2e-fixture.sh ~/workspace/vce2e-fixture   # clip.wav + ref.txt, hashes asserted
ssh hp '~/workspace/gopi-vce2e/bin/gopi voice_input --client --device pulse:gopi_mic \
        --seconds 16 --stdio > ~/workspace/gopi-vce2e/out/x.f32'   # then:
ssh hp 'ssh peer1 "gopi voice_input --server --stdio" < ~/workspace/gopi-vce2e/out/x.f32' > x.txt
python3 scripts/asr_score.py x.txt ~/workspace/vce2e-fixture/ref.txt

# a 1:1 (whole-fixture) run — the padded rig of §5. NOTE: the fixture ref.txt
# from voice-e2e-fixture.sh is 3192 bytes while HP's /home/user/workspace/vce2e-ref.txt
# is the same file (sha256 aea95730…) — keep them in sync or score against one path.
python3 - <<'PY'   # build the padded playback wav (20s silence + fixture)
import wave
s=wave.open('/home/user/workspace/vce2e-fixture/clip.wav','rb')
o=wave.open('/home/user/workspace/vce2e-clip-pad.wav','wb'); o.setnchannels(1); o.setsampwidth(2); o.setframerate(16000)
o.writeframes(b'\x00'*(20*16000*2)); o.writeframes(s.readframes(s.getnframes())); o.close()
PY
scp ~/workspace/vce2e-clip-pad.wav hp:~/workspace/vce2e-clip-pad.wav
bash ~/workspace/vce2e-cer4.sh    # shipped to hp; prints its own timeline, ends "CER4_DONE"
```

Two traps the rerun must respect (both measured, both in §5.2/§5.3): play the
**padded** wav and only after the client's pre-record line, and never
`pkill -f "<the whole pattern>"` on the remote box — it matches the ssh shell
carrying it (use a bracket, e.g. `pkill -f 'voice_input --server --stdi[o]'`).

Recovery (Rule 6d): branch **`feat/voice-client-e2e`** in the shared repo —
`git show feat/voice-client-e2e:devdoc/voice-client-e2e-peer.md`, worktree
`~/workspace/gopi-voice-client-e2e` (never merged by this task).
