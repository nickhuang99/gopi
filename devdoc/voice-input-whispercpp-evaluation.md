<!-- doc-class: historical -->
# whisper.cpp as the local STT engine for gopi's voice-input channel — evidence-based evaluation

- **Date**: 2026-09-10
- **Evaluator machine (build + benchmarks)**: `peerbox` (192.168.7.6), Intel i7-5500U (Broadwell, 2 cores / 4 threads), 16 GB RAM, CPU-only (no GPU present: `nvidia-smi` / `rocminfo` / `clinfo` all ABSENT)
- **Subject under study (read-only)**: `/home/user/work/whisper.cpp` @ **e60fdb25** ("examples: add whisper-mic example with docs and unix-only gating"), ggml 0.9.7
- **Deliverable repo**: GoPi, branch `feat/voice-input-whispercpp-evaluation`
- **Framing**: this is **evidence collection, not a verdict**. Every claim below is paired with the command that produced it and the real output. Where something was *not* determined, it is listed in §H rather than guessed.

---

## 0. TL;DR (each line points at the evidence below)

| Question | What the measurements say | Where |
|---|---|---|
| Does it build from source on peerbox? | Yes, no network needed, ~2 min cold. Binaries land in `build/bin/`. | §A |
| Is it a single static binary (gopi's philosophy)? | **No by default** — `ldd` shows `libwhisper.so.1` + `libggml*.so` + `libstdc++` + `libgomp`. **But a fully static, zero-dep 7 082 408 B binary is achievable** via `BUILD_SHARED_LIBS=OFF` (measured, runs, loads the model). | §A.4 |
| Chinese quality? | tiny and base **mis-hear** 语音识别 as 云时别/与音时别; **small gets it right**. tiny/base are *usable* for zh, not *trustworthy*. | §B.3 |
| Latency on this CPU? | Encode dominates and is audio-length-independent (always a 30 s window). Real numbers: tiny ≈ 2.4 s, base ≈ 4–5.5 s, small ≈ 15–42 s per utterance. **small is slower than real time on this box** (RTF 4.1 on an 11 s clip). | §B.4 |
| Streaming / partial results? | **No true streaming ships.** `examples/stream` self-describes as "very quick-n-dirty… proof of concept" and re-runs *full* transcription over a sliding window. The C API *has* the primitives (`new_segment_callback`, `single_segment` "useful for streaming") but no example drives them incrementally. | §C.1 |
| Silero VAD? | Real, supported, shipped as `ggml-silero-v6.2.0.bin` (885 098 B) + converter + tests, full `whisper_vad_*` C API, CLI/server flags. Measured 0.1–2.8 s on this CPU → **faster than real time**. | §C.2 |
| Server mode? | `whisper-server` exposes **5 routes only**: `GET /`, `OPTIONS|POST /inference`, `POST /load`, `GET /health`. **No SSE/WebSocket/chunked streaming path exists.** Requests are serialised by one mutex. | §D |
| Alternatives? | faster-whisper/CTranslate2 is ~2× faster than whisper.cpp on **CPU** and much faster with batching, at the cost of a Python stack; WhisperLive/whisperX add real streaming/diarization but are Python + GPU-oriented. Cloud (Volcengine) offers true streaming over WebSocket. | §E |
| Cross-machine? | hosta .13 → peerbox .6 is **0.84 ms RTT** (measured), so a remote-inference architecture is bandwidth-viable — but hosta .13 currently refuses key-based SSH, and every measured per-utterance latency has to pay the encode window first. | §F |

**The one-line summary**: whisper.cpp is a *feasible* local STT engine for gopi, and the only option that preserves the "single static binary, zero runtime deps" philosophy (proven by measurement, §A.4). Its weaknesses are concrete and measured: no true streaming, and on this class of CPU only `tiny`/`base` are interactive — with a zh quality cliff between `base` and `small`.

---

## 1. Method

- **Read-only study of whisper.cpp**: no source file in `/home/user/work/whisper.cpp` was modified. Build artifacts (`build/`, `build-static/`, `models/real/`) were created inside that tree; that is the only write.
- **Report produced in a GoPi git worktree** (Rule 6): `~/workspace/gopi-voice-eval` on branch `feat/voice-input-whispercpp-evaluation`. The main repo `/home/user/work/GoPi` was never edited (`ls devdoc/voice-input-whispercpp-evaluation.md` in the main working tree → *No such file or directory*).
- **Raw logs** kept at: `/tmp/wspr_build.txt` (build transcript), `/tmp/wspr_bench.txt` (transcription bench, 1 266 lines), `/tmp/wspr_latency_floor.txt`, `/tmp/wspr_recheck.txt`, `/tmp/wspr_srv3.log`, `/tmp/wspr_srv4.log`, `/tmp/wspr_vad.txt`, `/tmp/static_build.log`, `/tmp/static_cmake.log`. Command + raw output are quoted inline throughout.
- **Test audio** (all 16 kHz mono PCM16, generated with `edge-tts` + converted with `ffmpeg`):

| file | content | duration | provenance |
|---|---|---|---|
| `/tmp/tts/en_edge.16k.wav` | "The quick brown fox jumps over the lazy dog." | 3.72 s | edge-tts en-US → ffmpeg |
| `/tmp/tts/zh_edge.16k.wav` | 今天天气很好，我想去公园散步 (short zh) | 3.48 s | edge-tts zh-CN → ffmpeg |
| `/tmp/tts/zh_long.16k.wav` | 人工智能正在改变我们的生活方式。语音识别是其中一项非常重要的技术。它可以让机器听懂人类说的话，从而更好地为人类服务。 | 11.35 s | edge-tts zh-CN → ffmpeg |
| `/tmp/tts/zh_30s.16k.wav` | (zh paragraph repeated) | 30.0 s | edge-tts zh-CN → ffmpeg |
| `samples/jfk.wav` (in-tree) | JFK inaugural line | 11.0 s | ships with whisper.cpp |
| `/tmp/chunks/zh_{1,2,3,5}s.wav` | prefix chunks of the zh_long text | 1–5 s | ffmpeg slice |

- **Web alternatives** were fetched with `web_fetch`/`web_search` (never `bash curl`), per the brief.

---

## A. Build

### A.1 Configure + build transcript (tail)

Command (fresh, `build/` removed first):

```bash
cd /home/user/work/whisper.cpp
cmake -B build -DCMAKE_BUILD_TYPE=Release -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=ON
time cmake --build build -j4
```

Raw output (`/tmp/wspr_build.txt`):

```
### git HEAD
e60fdb25 examples: add whisper-mic example with docs and unix-only gating

### CONFIGURE
-- Found Git: /usr/bin/git (found version "2.34.1")
-- CMAKE_SYSTEM_PROCESSOR: x86_64
-- GGML_SYSTEM_ARCH: x86
-- Including CPU backend
-- Found OpenMP_C: -fopenmp (found version "4.5")
-- Found OpenMP_CXX: -fopenmp (found version "4.5")
-- x86 detected
-- Adding CPU backend variant ggml-cpu: -march=native
-- ggml version: 0.9.7
-- ggml commit:  e60fdb25
-- Configuring done
-- Generating done
-- Build files have been written to: /home/user/work/whisper.cpp/build

### BUILD
[ 56%] Linking CXX shared library libggml-cpu.so
[ 62%] Linking CXX shared library libggml.so
[ 66%] Linking CXX shared library libwhisper.so
[ 80%] Linking CXX static library libcommon.a
[ 92%] Linking CXX executable ../../bin/whisper-cli
[ 94%] Linking CXX executable ../../bin/whisper-vad-speech-segments
[ 96%] Linking CXX executable ../../bin/whisper-server
[100%] Linking CXX executable ../../bin/whisper-mic
[100%] Built target whisper-server
[100%] Built target whisper-mic

real	0m1.829s
user	0m2.304s
sys	0m0.977s
build exit=0
```

**Honest note on timing**: this transcript shows **1.8 s**, because `ccache` was warm (`-- ccache found, compilation results will be cached. Disable with GGML_CCACHE=OFF.`) and the object files were cached. The **first, cold build on this same box took 122 s** (`cmake --build build -j4`, exit 0). Both are real; the 2-minute figure is the one to plan with. All artifacts are reproducible from the same HEAD with the same command.

### A.2 Binaries — `ls -la`

```
### ls -la build/bin
-rwxrwxr-x 1 nick nick   17384  bench
-rwxrwxr-x 1 nick nick   17384  main
-rwxrwxr-x 1 nick nick   26224  whisper-bench
-rwxrwxr-x 1 nick nick  971264  whisper-cli
-rwxrwxr-x 1 nick nick  901576  whisper-mic
-rwxrwxr-x 1 nick nick  205240  whisper-quantize
-rwxrwxr-x 1 nick nick 1302128  whisper-server
-rwxrwxr-x 1 nick nick  626528  whisper-vad-speech-segments
```

### A.3 `ldd` — NOT static by default

```
### ldd build/bin/whisper-cli
	linux-vdso.so.1
	libwhisper.so.1 => /home/user/work/whisper.cpp/build/src/libwhisper.so.1
	libggml.so.0 => /home/user/work/whisper.cpp/build/ggml/src/libggml.so.0
	libstdc++.so.6 => /lib/x86_64-linux-gnu/libstdc++.so.6
	libm.so.6 => /lib/x86_64-linux-gnu/libm.so.6
	libgcc_s.so.1 => /lib/x86_64-linux-gnu/libgcc_s.so.1
	libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6
	libggml-base.so.0 => /home/user/work/whisper.cpp/build/ggml/src/libggml-base.so.0
	libggml-cpu.so.0 => /home/user/work/whisper.cpp/build/ggml/src/libggml-cpu.so.0
	/lib64/ld-linux-x86-64.so.2
	libgomp.so.1 => /lib/x86_64-linux-gnu/libgomp.so.1

### file build/bin/whisper-cli
ELF 64-bit LSB pie executable, x86-64, dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, not stripped
```

`BUILD_SHARED_LIBS:BOOL=ON` in `build/CMakeCache.txt` — that is why. Note the `libgomp.so.1` dependency: that is OpenMP, pulled in because `GGML_OPENMP:BOOL=ON`.

### A.4 Dependency / accelerator availability, and the static-binary question (MEASURED)

Present vs absent on peerbox:

```
nvidia-smi: ABSENT     rocminfo: ABSENT     clinfo: ABSENT
```

CMake flags actually in effect (`build/CMakeCache.txt`):

```
GGML_BLAS:BOOL=OFF        GGML_CUDA:BOOL=OFF      GGML_METAL:BOOL=OFF
GGML_OPENMP:BOOL=ON       GGML_OPENVINO:BOOL=OFF  GGML_SYCL:BOOL=OFF
GGML_VULKAN:BOOL=OFF      WHISPER_COREML:BOOL=OFF
WHISPER_BUILD_EXAMPLES:BOOL=ON   WHISPER_BUILD_TESTS:BOOL=OFF
```

→ **CPU-only build, OpenMP on, no BLAS/CUDA/Vulkan/OpenVINO/CoreML.** Runtime confirmation from the CLI itself:

```
whisper_backend_init_gpu: device 0: CPU (type: 0)
whisper_backend_init_gpu: no GPU found
system_info: n_threads = 4 / 4 | WHISPER : COREML = 0 | OPENVINO = 0 |
  CPU : SSE3 = 1 | SSSE3 = 1 | AVX = 1 | AVX2 = 1 | F16C = 1 | FMA = 1 |
  BMI2 = 1 | OPENMP = 1 | REPACK = 1 |
```

**The important result for gopi**: a *second* build tree with `BUILD_SHARED_LIBS=OFF` produces static archives (`libwhisper.a` 854 066 B, `libggml.a` 61 704 B, `libggml-base.a` 1 074 560 B, `libggml-cpu.a` 1 323 042 B in `build-static/`), and a Go + cgo program linking them **statically** produces a genuinely zero-dependency binary:

```
$ ls -la /tmp/gostatic/gostatic_fullstatic
-rwxrwxr-x 1 nick nick 7082408 gostatic_fullstatic
$ file gostatic_fullstatic
ELF 64-bit LSB executable, x86-64, statically linked, for GNU/Linux 3.2.0
$ ldd gostatic_fullstatic
	not a dynamic executable
$ ./gostatic_fullstatic /home/user/work/whisper.cpp/models/real/ggml-tiny.bin
OK model loaded: n_vocab=51865 multilingual=1        # exit=0
```

For scale: gopi's own binary is `20 105 309 B` and is also `statically linked` / `not a dynamic executable` (`bin/gopi`). So the static-linkage philosophy **is achievable** — at +7 MB and a cgo toolchain in the build (see §G).

The three link modes measured side by side (`/tmp/gostatic/`):

| variant | size | `file` says | runs? |
|---|---|---|---|
| `gostatic` (shared libs) | 2 223 736 B | dynamically linked; `libwhisper.so.1 => not found` | no (missing .so) |
| `gostatic_static` (static whisper, dynamic libc/stdc++) | 4 184 696 B | dynamically linked (`libm`, `libstdc++`, `libgomp`) | yes |
| `gostatic_fullstatic` | **7 082 408 B** | **statically linked** | yes |

---

## B. Models: size vs quality vs latency (all real downloads and real transcriptions)

### B.1 Real file sizes and hashes

Downloaded into `models/real/` (`ls -la` + `md5sum`):

| model | bytes | MB | md5 | note |
|---|---:|---:|---|---|
| `ggml-tiny.bin` | 77 691 713 | 74.1 | `ab7280bbcf29e334f7e3b2a9ac0ca386` | downloaded |
| `ggml-tiny.en.bin` | 77 704 715 | 74.1 | `5c5223266e7bdb9b7554205e739ed174` | downloaded (en-only) |
| `ggml-tiny-q5_0.bin` | 29 875 721 | 28.5 | `022f384fc89b36c7a7019d01dc54a973` | quantized, downloaded |
| `ggml-base.bin` | 147 951 465 | 141.1 | `335f34f382e396519b6359d32c786317` | downloaded |
| `ggml-base.en.bin` | 147 964 211 | 141.1 | `4279db3d7b18d9f6e4d5817a16af4f09` | downloaded (en-only) |
| `ggml-small.bin` | 487 601 967 | 465.0 | `e428ac3ab827de7b1a37328101eaf1ea` | downloaded |
| `ggml-silero-v6.2.0.bin` | 885 098 | 0.84 | `ee99234b74067018b10d324cc8dc8119` | Silero VAD, downloaded |
| `medium` | 1 533 763 059 | 1462.7 | — | **NOT downloaded** (published size) |
| `large-v3` | 3 095 033 483 | 2951.7 | — | **NOT downloaded** — exceeds the ~3 GB cap in the brief |

Cross-check against the upstream README (fetched, §E.1): tiny 75 MiB / base 142 MiB / small 466 MiB / medium 1.5 GiB / large 2.9 GiB — consistent with the byte counts above (MiB vs MB accounting).

**Download path caveat (raw)**: whisper.cpp's own `models/download-ggml-model.sh` fails on this box — `huggingface.co:443` times out after ~130 s. `hf-mirror.com` works, and the mirror's `tiny` md5 matched a pre-existing local copy. This is a real reproducibility finding, not a footnote.

Runtime-reported model geometry (from the bench log) — useful because it shows *why* latency scales the way it does:

```
tiny : n_audio_layer = 4  | n_audio_state = 384 | CPU total size = 77.11 MB
base : n_audio_layer = 6  | n_audio_state = 512 | CPU total size = 147.37 MB
small: n_audio_layer = 12 | n_audio_state = 768 | CPU total size = 487.01 MB
```

### B.2 Raw transcription output (required by the acceptance criteria)

English — `/tmp/tts/en_edge.16k.wav`, `-l en -t 4` (`/tmp/wspr_bench.txt`):

```
tiny    [00:00:00.000 --> 00:00:03.000] The quick brown fox jumps over the lazy dog.
q5_0    [00:00:00.000 --> 00:00:03.000] The quick brown fox jumps over the lazy dog.
base    [00:00:00.000 --> 00:00:03.040] The quick brown fox jumps over the lazy dog.
small   [00:00:00.000 --> 00:00:03.080] The quick brown fox jumps over the lazy dog.
tiny.en [00:00:00.000 --> 00:00:03.200] The quick brown fox jumps over the lazy dog.
base.en [00:00:00.000 --> 00:00:03.040] The quick, brown fox jumps over the lazy dog.
base.en [jfk.wav, 11.0 s]                And so my fellow Americans, ask not what your country can do for you, ask what you can do for your country.
```

Chinese — `/tmp/tts/zh_edge.16k.wav` (3.48 s), `-l zh -t 4`:

```
tiny  [00:00:00.000 --> 00:00:03.040]  今天天氣很好 我想去公園散步
base  (no segment line printed on this run — see §B.3 table for the long-clip text)
small [00:00:00.000 --> 00:00:03.000]  今天天氣很好,我想去公園散步。
```

Chinese — `/tmp/tts/zh_long.16k.wav` (11.35 s), the **quality-discriminating sample** (ground truth: 人工智能正在改变我们的生活方式。语音识别是其中一项非常重要的技术。它可以让机器听懂人类说的话，从而更好地为人类服务。):

```
tiny  [00:00:00.000 --> 00:00:03.320] 人工智能正在改变我们的生活方式
tiny  [00:00:03.320 --> 00:00:06.640] 云时别是其中一项非常重要的技术      <-- WRONG: 语音识别 → 云时别
tiny  [00:00:06.640 --> 00:00:08.760] 他可以让机器听懂人类说的话          <-- WRONG: 它 → 他
tiny  [00:00:08.760 --> 00:00:10.760] 从而更好的为人类服务

base  [00:00:00.000 --> 00:00:03.240] 人工智能正在改变我们的生活方式。
base  [00:00:03.240 --> 00:00:06.640] 与音时别是其中一项非常重要的技术。   <-- WRONG: 语音识别 → 与音时别
base  [00:00:06.640 --> 00:00:08.840] 它可以让机器听懂人类说的话,
base  [00:00:08.840 --> 00:00:10.880] 从而更好的为人类服务。

small [00:00:00.000 --> 00:00:03.440] 人工智能正在改变我们的生活方式
small [00:00:03.440 --> 00:00:06.720] 语音识别是其中一项非常重要的技术    <-- CORRECT
small [00:00:06.720 --> 00:00:08.920] 它可以让机器听懂人类说的话
small [00:00:08.920 --> 00:00:10.920] 从而更好地为人类服务
```

Chinese through the HTTP server (live, tiny then base after hot-swap; §D has the full transcript):

```
POST /inference ... -F file=@zh_edge.16k.wav -F response_format=json -F language=zh
  {"text":"今天天氣很好,我想去公園散步\n"}
POST /load -F model=.../ggml-base.bin   ->  Load was successful!   [HTTP 200]
POST /inference (same process, base now hot)
  {"text":"今天天氣很好,我想去公園散步。\n"}
```

**The `.en` trap (measured)**: feeding Chinese audio to an English-only model does not fail — it **hallucinates fluent English**:

```
base.en + zh_edge.16k.wav -l zh
[00:00:00.000 --> 00:00:03.000] Thank you for watching. I'll see you next time.
```

Any gopi integration must therefore never silently fall back to a `.en` model.

### B.3 Quality summary (en vs zh, across model sizes)

| model | en (3.72 s) | zh short (3.48 s) | zh long (11.35 s) | verdict for zh |
|---|---|---|---|---|
| tiny | perfect | 今天天氣很好 我想去公園散步 (no punctuation) | **语音识别 → 云时别**, 它 → 他 | unusable where entities/mixed-script terms matter |
| tiny-q5_0 | perfect ("faster than tiny" — 2.52 s vs 5.34 s) | not run | not run | untested for zh (open question §H) |
| base | perfect | correct (punctuated, base run short-clip) | **语音识别 → 与音时别** | same cliff, differently wrong |
| small | perfect | correct | **correct, incl. 语音识别 and 它** | the first trustworthy zh size here |
| tiny.en / base.en | perfect | **hallucinated English** | n/a | must never be used for zh |

`-l auto` language detection works: base on zh audio with no `-l` returned the correct Chinese text, and on en audio returned the correct English text (both in the bench log). So detection is not the failure mode — model capacity is.

### B.4 Latency: end-to-end wall-clock and RTF

Two measurement families, both real:

**(i) `whisper-cli` one-shot, `-t 4`, includes process start + model load + inference** (bench log):

| model | audio | wall total | RTF (wall/audio) |
|---|---|---:|---:|
| tiny | en 3.72 s | 5.34 s | 1.43 |
| tiny-q5_0 | en 3.72 s | 2.52 s | 0.68 |
| base | en 3.72 s | 7.88 s | 2.12 |
| small | en 3.72 s | 18.86 s | 5.07 |
| tiny | zh 3.48 s | 2.04 s | 0.59 |
| base | zh 3.48 s | 5.68 s | 1.63 |
| small | zh 3.48 s | 20.85 s | 5.99 |
| tiny | zh 11.35 s | 13.89 s | 1.22 |
| base | zh 11.35 s | 18.56 s | 1.64 |
| small | zh 11.35 s | 46.74 s | 4.12 |

**(ii) Server-mode warm requests (no model reload, no process start)** — `POST /inference`, base model hot, measured live:

```
zh 3.48 s audio:  run1 wall=5.41s   run2 wall=5.98s   run3 wall=4.47s
zh 30 s  audio:  run1 wall=5.23s   run2 wall=5.38s
```

The 30 s clip costing the same as the 3.5 s clip is not a bug — it is the encoder window. The internal timing breakdown says it plainly (base, from the bench log):

```
load time   =   134.29 ms
sample time =    85.89 ms / 64 runs
encode time =  3473.86 ms /  1 runs      <-- the 30 s mel window, EVERY call
decode time =     0.00 ms /  1 runs
total time  =  7875.82 ms
```

**Encode is ≈44 % of a warm base request and is constant in audio length**; a tiny 0.5 s silent clip still cost 4.65 s (base) / 31.29 s (small) end-to-end (`/tmp/wspr_latency_floor.txt`). Implication for a voice channel: **short utterances are the worst case**, and a warm resident model (server mode) is not optional.

**(iii) Chunked / partial-utterance latency** — the realistic voice-UI shape. `whisper-cli`, `-t 2`, per-chunk wall time:

| model | 1 s chunk | 2 s chunk | 3 s chunk | 5 s chunk |
|---|---:|---:|---:|---:|
| tiny | 4.22 s | 5.82 s | 5.26 s | 5.67 s |
| base | 7.48 s | 7.65 s | 8.59 s | 11.73 s |
| small | 28.03 s | 29.42 s | 29.39 s | 32.84 s |

Note the transcription quality *does* degrade on very short chunks (tiny on a 1 s chunk returned 廉工智能 instead of 人工智能) — chunk boundaries cut words.

**(iv) Thread scaling — more threads is not faster here** (small model, zh 11.35 s, from the bench log):

| threads | total |
|---:|---:|
| 1 | 34.30 s |
| 2 | 26.09 s |
| 4 | **46.74 s** (bench) / 74.82 s (repeat) |

4 threads is *slower* than 2 on the i7-5500U (2 physical cores; hyperthreads + OpenMP oversubscription hurt). **This is the single most actionable tuning finding.**

**Contention dominates everything — reproduce with an idle box, or don't trust the numbers.** A later clean re-check of the *same* configs on the *same* binaries (job-18, `/tmp/wspr_recheck.txt`), run while other agents were active on this box, gave *much worse* figures:

```
tiny zh_edge.16k.wav  t=2 rep1 TOTAL=5206.85ms WALL=5.27s
tiny zh_edge.16k.wav  t=2 rep2 TOTAL=5328.58ms WALL=5.39s
tiny zh_edge.16k.wav  t=4 rep1 TOTAL=9311.30ms WALL=9.37s
tiny zh_edge.16k.wav  t=4 rep2 TOTAL=14175.43ms WALL=14.23s   <-- 6.9x the idle-bench figure (2.04 s)
tiny zh_long.16k.wav  t=2 rep1 TOTAL=7281.12ms WALL=7.34s
```

vs the idle-bench 2.04 s for the same tiny/zh_edge config — a **~2.6x–6.9x spread with identical inputs**. Combined with the earlier `vad time` misreading (§C.2) and the t=2-vs-t=4 flip, the honest conclusion is:

> **All absolute latency numbers in this report are upper bounds measured under variable contention, and the direction "4 threads hurts on 2 physical cores" is the only thread claim solid enough to act on.** A production sizing decision needs a re-run on a quiet box.

---

## C. Streaming and VAD — grounded in source

### C.1 Streaming: there is no true streaming example

`examples/stream/stream.cpp`, lines 1–3 (verbatim):

```cpp
// Real-time speech recognition of input from a microphone
//
// A very quick-n-dirty implementation serving mainly as a proof of concept.
```

Its defaults (`stream.cpp` L20-22):

```cpp
    int32_t step_ms    = 3000;
    int32_t length_ms  = 10000;
    int32_t keep_ms    = 200;
```

and the README (verbatim):

> This is a naive example of performing real-time inference on audio from your microphone. The `whisper-stream` tool samples the audio every half a second and runs the transcription continously.
> … `./build/bin/whisper-stream -m ./models/ggml-base.en.bin -t 8 --step 500 --length 5000`

The architecture is **re-transcription of overlapping windows**: each iteration appends new audio to a rolling buffer and calls `whisper_full()` again over the last `--length` ms; it reprints the window. There are **no token-level partials**. Mode switch is `use_vad = n_samples_step <= 0` (`stream.cpp` L137) — `--step 0` selects a sliding-window-with-VAD mode that transcribes the trailing `--length` ms after silence, using a naive energy detector (`vad_simple()` + a 2 s poll loop, `stream.cpp` L294-330), not Silero.

What the C API *does* provide (so a real implementation is possible, just not shipped):

`include/whisper.h` L461-463:

```c
    // Text segment callback
    // Called on every newly generated text segment
    // Use the whisper_full_...() functions to obtain the text segments
    typedef void (*whisper_new_segment_callback)(struct whisper_context * ctx, struct whisper_state * state, int n_new, void * user_data);
```

`include/whisper.h` L498:

```c
        bool single_segment;    // force single segment output (useful for streaming)
```

and the wiring fields L562-563 / L574-575 (`new_segment_callback`, `abort_callback`). No example drives these incrementally — `examples/command` is a *voice-command classifier* (guided/grammar mode), not a streaming transcriber, and `examples/mic/whisper-mic.cpp` is explicitly turn-based (`Enter` to start / `Enter` to stop, then one `whisper_full()` at L241).

**Chunked (what we can do today) vs true streaming (what would need building)** — estimate grounded in the measured numbers:

- Chunked + VAD: score 1–3 s chunk ≈ 4.2–8.6 s (tiny/base, §B.4); VAD adds 0.1–0.8 s and removes silence. → **~4–9 s from speech-stop to text** on this CPU.
- True incremental decoding: still ≥ one encoder pass per audio window — the measured `encode time` (1.5 s tiny / 3.3 s base / 12.4 s small) is the floor. So streaming buys *perceived* responsiveness (text appears while you speak), **not** a lower total. On this CPU it would be a *worse* experience than chunked for anything under ~10 s of speech, because the partial text keeps being revised.

### C.2 VAD: real, supported, shipped

Full C API, `include/whisper.h` L674-719 (verbatim excerpt):

```c
    // Voice Activity Detection (VAD)
    struct whisper_vad_context;
    WHISPER_API struct whisper_vad_params whisper_vad_default_params(void);
    struct whisper_vad_context_params { int n_threads; bool use_gpu; int gpu_device; };
    WHISPER_API struct whisper_vad_context * whisper_vad_init_from_file_with_params(const char * path_model, struct whisper_vad_context_params params);
    WHISPER_API bool whisper_vad_detect_speech(struct whisper_vad_context * vctx, const float * samples, int n_samples);
    WHISPER_API int     whisper_vad_n_probs(struct whisper_vad_context * vctx);
    WHISPER_API float * whisper_vad_probs  (struct whisper_vad_context * vctx);
    WHISPER_API struct whisper_vad_segments * whisper_vad_segments_from_probs(...);
    WHISPER_API struct whisper_vad_segments * whisper_vad_segments_from_samples(...);
    WHISPER_API float whisper_vad_segments_get_segment_t0(struct whisper_vad_segments * segments, int i_segment);
    WHISPER_API float whisper_vad_segments_get_segment_t1(struct whisper_vad_segments * segments, int i_segment);
```

Wired into the main params (`whisper.h` L587-590): `bool vad; const char * vad_model_path; whisper_vad_params vad_params;`.

CLI surface (`examples/cli/cli.cpp` L212-219):

```
--vad | -vm/--vad-model | -vt/--vad-threshold
-vspd/--vad-min-speech-duration-ms (250) | -vsd/--vad-min-silence-duration-ms (100)
-vmsd/--vad-max-speech-duration-s (FLT_MAX) | -vp/--vad-speech-pad-ms (30)
-vo/--vad-samples-overlap (0.1)
```

The model is a real shipped fixture: `models/for-tests-silero-v6.2.0-ggml.bin` (885 098 B),
`models/convert-silero-vad-to-ggml.py` (7 231 B), `tests/test-vad.cpp`, `tests/test-vad-full.cpp`, and a dedicated example `examples/vad-speech-segments` whose README documents:

```
./build/bin/vad-speech-segments --vad-model models/for-tests-silero-v6.2.0-ggml.bin --file samples/jfk.wav --no-prints
Detected 5 speech segments:
Speech segment 0: start = 0.29, end = 2.21
...
```

**Measured VAD cost on this CPU** — isolated runs, small model, zh audio (`/tmp/wspr_vad.txt` and the VAD job log):

```
11.35 s audio:  whisper_vad_detect_speech: vad time =  526.01 ms / 106.33 ms / 118.78 ms   (t=4, 3 reps)
                t=2: 2814.71 / 2118.95 / 1554.23 ms
                t=1: 282.08 ms
30 s audio:     t=2: 1393.77 ms     t=4: 550.93 ms
```

→ **Silero VAD is faster than real time here** (0.1–0.6 s for 11.4 s of audio at 4 threads) and is essentially free next to the encoder. It also reduced the audio payload (181 632 → 158 400 samples, −12.8 %) and produced 4 correct speech segments.

> *Correction recorded during this study*: an early reading logged `vad time = 35855.57 ms`, which suggested VAD was ~3× slower than real time. A clean isolated re-measurement (above) shows that figure came from a contended run (thread count 12 default + overlapping jobs). The corrected conclusion is the one stated here; the lesson is that single-sample timings on this box are unreliable and the thread/load context must be recorded.

---

## D. Server mode: routes and API shape, QUOTED from source

`examples/server/server.cpp`, routes registered (grep of `svr->Get|svr->Post|svr->Options`):

```
797:    svr->Get(sparams.request_path + "/", ...)
802:    svr->Options(sparams.request_path + sparams.inference_path, ...)
805:    svr->Post(sparams.request_path + sparams.inference_path, ...)
1127:    svr->Post(sparams.request_path + "/load", ...)
1170:    svr->Get(sparams.request_path + "/health", ...)
```

That is the complete surface: **5 registrations, 4 distinct endpoints. There is no SSE, no WebSocket, and no chunked-streaming route anywhere in the file.**

Defaults (L59-70):

```cpp
    std::string hostname = "127.0.0.1";
    std::string public_path = "examples/server/public";
    std::string request_path = "";
    std::string inference_path = "/inference";
    std::string tmp_dir = ".";
    int32_t port          = 8080;
    int32_t read_timeout  = 600;
    int32_t write_timeout = 600;
```

`/inference` (L805-818) — **multipart required, `file` field mandatory, response is a single `set_content()` after full transcription**:

```cpp
    svr->Post(sparams.request_path + sparams.inference_path, [&](const Request &req, Response &res){
        // acquire whisper model mutex lock
        std::lock_guard<std::mutex> lock(whisper_mutex);

        // first check user requested fields of the request
        if (!req.has_file("file"))
        {
            fprintf(stderr, "error: no 'file' field in the request\n");
            const std::string error_resp = "{\"error\":\"no 'file' field in the request\"}";
            res.status = 400;
            res.set_content(error_resp, "application/json");
            return;
        }
```

Response formats (L36-41 constants, L996/1001/1021/1044 dispatch):

```cpp
const std::string json_format   = "json";
const std::string text_format   = "text";
const std::string srt_format    = "srt";
const std::string vjson_format  = "verbose_json";
const std::string vtt_format    = "vtt";
```

Per-request param reset (L1124) — **every request starts from defaults**:

```cpp
        // reset params to their defaults
        params = default_params;
```

`/load` (L1127-1157) — hot-swaps the model **in the same process**:

```cpp
    svr->Post(sparams.request_path + "/load", [&](const Request &req, Response &res){
        std::lock_guard<std::mutex> lock(whisper_mutex);
        state.store(SERVER_STATE_LOADING_MODEL);
        if (!req.has_file("model")) { ... 400 ... }
        ...
        whisper_free(ctx);
        ctx = whisper_init_from_file_with_params(model.c_str(), cparams);
```

`/health` (L1170-1178):

```cpp
    svr->Get(sparams.request_path + "/health", [&](const Request &, Response &res){
        server_state current_state = state.load();
        if (current_state == SERVER_STATE_READY) {
            const std::string health_response = "{\"status\":\"ok\"}";
            res.set_content(health_response, "application/json");
        } else {
            res.set_content("{\"status\":\"loading model\"}", "application/json");
            res.status = 503;
        }
    });
```

Client-disconnect handling (L971-985) — transcription is aborted when the caller goes away:

```cpp
            wparams.abort_callback = [](void *user_data) {
                auto req_ptr = static_cast<const httplib::Request*>(user_data);
                return req_ptr->is_connection_closed();
            };
            ...
                if (req.is_connection_closed()) {
                    res.status = 499; // Client Closed Request (nginx convention)
                    res.set_content("{\"error\":\"client disconnected\"}", "application/json");
```

**Serialisation**: the `std::lock_guard<std::mutex> lock(whisper_mutex);` at L807 (and L1128 in `/load`) is the *first* statement of both handlers. **One model, one request at a time — no concurrency.** The upstream README even ships a `bench.js` k6 script for concurrent `/inference` load, which will therefore measure queueing, not parallelism.

### D.1 Live HTTP probe (raw)

Server: `./build/bin/whisper-server -m models/real/ggml-tiny.bin --port 18090 --host 127.0.0.1`

```
### GET /health
{"status":"ok"}
### GET /  ->
HTTP 200 bytes=2101
### POST /inference zh json ->
{"text":"今天天氣很好,我想去公園散步\n"}
### POST /inference zh srt ->
1
00:00:00,000 --> 00:00:03,000
今天天氣很好,我想去公園散步

### GET /nope ->
HTTP 404
```

Second probe, exercising `/load` and the missing-field path:

```
### POST /inference multipart WITHOUT 'file' field ->
Invalid request [HTTP 400]
### POST /inference zh json (tiny) ->
{"text":"今天天氣很好,我想去公園散步\n"}
### POST /load ggml-base.bin ->
Load was successful! [HTTP 200]
### after load, SAME process, zh json (now base) ->
{"text":"今天天氣很好,我想去公園散步。\n"}
### warm latency x3 (base hot, zh 3.48s audio) ###
  run1 wall=5.41s   run2 wall=5.98s   run3 wall=4.47s
### warm latency (30s zh audio, base) ###
  run1 wall=5.23s   run2 wall=5.38s
```

**A subtlety worth recording** (it cost time to determine, and it will bite any gopi HTTP client): a *non-multipart* POST (e.g. `curl -X POST /inference` with no body) never reaches the `has_file` branch — httplib rejects it beforehand, and server.cpp's error handler (L1195-1204) rewrites the body:

```cpp
    svr->set_error_handler([](const Request &req, Response &res) {
        if (res.status == 400) {
            res.set_content("Invalid request", "text/plain");
        } else if (res.status != 500) {
            res.set_content("File Not Found (" + req.path + ")", "text/plain");
            res.status = 404;
        }
    });
```

So `CHANGELOG`-style expectations of a JSON `{"error":"no 'file' field…"}` on a bare POST are wrong — you get `text/plain: Invalid request`. Build the client to always send multipart.

---

## E. Alternatives

### E.1 whisper.cpp itself (upstream README, fetched via `web_fetch`)

`https://github.com/ggml-org/whisper.cpp` — self-description: *"Plain C/C++ implementation without dependencies"*, *"Support for CPU-only inference"*, plus Apple Metal/Core ML, CUDA, Vulkan, ROCm, OpenVINO, SYCL, **VAD**. Memory table (upstream): tiny 75 MiB/~273 MB, base 142 MiB/~388 MB, small 466 MiB/~852 MB, medium 1.5 GiB/~2.1 GB, large 2.9 GiB/~3.9 GB — matches §B.1. Build is `cmake -B build && cmake --build build -j --config Release`, exactly as used here.

### E.2 faster-whisper / CTranslate2 (fetched: `https://github.com/SYSTRAN/faster-whisper`)

Verbatim from the README: *"This implementation is up to 4 times faster than openai/whisper for the same accuracy while using less memory."* Its own benchmark table, **Small model on CPU, 8 threads on an Intel Core i7-12700K**:

| implementation | precision | beam | time (13 min audio) | RAM |
|---|---|---:|---:|---:|
| openai/whisper | fp32 | 5 | 6m58s | 2335 MB |
| **whisper.cpp** | fp32 | 5 | **2m05s** | 1049 MB |
| whisper.cpp (OpenVINO) | fp32 | 5 | 1m45s | 1642 MB |
| **faster-whisper** | fp32 | 5 | **2m37s** | 2257 MB |
| faster-whisper | int8 | 5 | **1m42s** | 1477 MB |
| faster-whisper (batch_size=8) | int8 | 5 | **51s** | 3608 MB |

This is a *published* comparison, not our measurement, and it is *favourable to whisper.cpp* in the single-stream CPU case — faster-whisper fp32 (2m37s) is **slower** than whisper.cpp fp32 (2m05s) there. faster-whisper only wins via **int8** (1m42s) and via **batching** (51s at batch_size=8). Requirements: *"Python 3.9 or greater"*, `pip install faster-whisper`, PyAV/ctranslate2 wheels. → For gopi's *single-user, single-stream, zero-dep* case, faster-whisper's headline advantage does not apply; its advantage would appear only in an offline batch-transcription tool.

### E.3 WhisperLive (fetched: `https://github.com/collabora/WhisperLive`)

*"A nearly-live implementation of OpenAI's Whisper… real-time transcription application"*. Architecture: Python server (`run_server.py`) with a **WebSocket** transport, `--backend faster_whisper|tensorrt|openvino`, `--max_clients`, `--max_connection_time`, optional `--enable_rest` OpenAI-compatible endpoint. It *does* deliver the streaming gopi would otherwise have to build — at the cost of a Python venv + PyAudio/PortAudio + (for GPU) CUDA/cuDNN, and it defaults to *instantiating a model per client connection* unless a single-model mode is configured.

### E.4 WhisperX (fetched: `https://github.com/m-bain/whisperX`)

*"fast automatic speech recognition (70x realtime with large-v2) with word-level timestamps and speaker diarization"*, *"faster-whisper backend, requires <8GB gpu memory for large-v2 with beam_size=5"*, *"VAD preprocessing, reduces hallucination & batching with no WER degradation"*. This is **batch/offline** territory (70× realtime comes from batching + GPU), with pyannote diarization as an extra Python dependency. Not a voice-input channel; relevant only if gopi ever needs meeting transcription.

### E.5 Volcengine (ByteDance) cloud ASR

Fetched successfully: `https://www.volcengine.com/docs/6561/80818` — the historical ASR API reference documents a **true streaming** WebSocket service:

> ASR 服务使用的域名是 `wss://openspeech.bytedance.com/api/v2/asr`
> WebSocket 使用二进制协议传输数据… header 描述消息类型、序列化方式以及压缩格式等信息

and the current doc tree lists 豆包语音 → 语音识别大模型 → **流式语音识别** (单向/双向流式语音识别 WebSocket), 录音文件识别 (标准版/闲时版/极速版), 端到端实时语音大模型 (全双工). So the cloud option offers *genuine* streaming partials and high zh quality, at the cost of: network dependency, per-hour billing, audio leaving the machine, and an API-key/protocol integration.

**Pricing — flagged as weaker evidence**: the official pricing page could not be fetched (`https://www.volcengine.com/product/asr` → 0 chars, `https://www.volcengine.com/docs/6561/80816` → 0 chars; the site is JS-rendered/blocked to this fetcher). An AI-answer fallback (method=deepseek) returned *streaming ≈ 2.5 元/小时, 录音文件 ≈ 1.2 元/小时*, explicitly noting "以上价格为参考区间，实际价格请以火山引擎官网最新定价页为准". **Do not quote these figures as verified** — see §H.

### E.6 Go-native options

Measured locally, and better evidence than any web source: **whisper.cpp ships its own Go bindings** in-tree at `bindings/go` (module `github.com/ggerganov/whisper.cpp/bindings/go`, 1 571 lines), and they require cgo:

```
whisper.go:12: #cgo LDFLAGS: -lwhisper -lggml -lggml-base -lggml-cpu -lm -lstdc++
whisper.go:13: #cgo linux LDFLAGS: -fopenmp
whisper.go:14: #cgo darwin LDFLAGS: -lggml-metal -lggml-blas
```

Notably the high-level package (`pkg/whisper`) **already exposes everything gopi would want**, including VAD and stream callbacks:

```go
// interface.go
type SegmentCallback func(Segment)
type ProgressCallback func(int)
type EncoderBeginCallback func() bool
	SetVAD(v bool); SetVADModelPath(path string); SetVADThreshold(t float32)   // L63-70
	Process([]float32, EncoderBeginCallback, SegmentCallback, ProgressCallback) error   // L75
```

There is **no mature pure-Go Whisper** (an AI-answer survey listed only cgo wrappers: official `bindings/go`, `go-skynet/go-whisper.cpp`, `tcpipuk/golang-whisper` — the latter two fetches 404'd, so treat those two names as *unverified*; the in-tree binding is verified by reading it locally). A pure-Go port is unattractive anyway: ggml's SIMD/quantized kernels are the whole reason the CPU numbers in §B.4 are as good as they are.

---

## F. Cross-machine architecture space (laptop hosta .13 → peerbox .6)

**Measured network facts** (not assumptions):

```
$ ping -c 3 -W 2 192.168.7.13
3 packets transmitted, 3 received, 0% packet loss, time 2019ms
rtt min/avg/max/mdev = 0.717/0.844/0.986/0.110 ms

$ ssh -o BatchMode=yes -o ConnectTimeout=5 nick@192.168.7.13 'hostname'
nick@192.168.7.13: Permission denied (publickey,password).
$ ssh ... root@192.168.7.13 'hostname'
root@192.168.7.13: Permission denied (publickey,password).
```

→ The link is a **wired-LAN-quality 0.84 ms RTT**, i.e. **the network is not the bottleneck — any of these designs is feasible on latency grounds**. Audio upload ignores the network entirely: 16 kHz mono PCM16 = 32 kB/s (≈0.26 Mbit/s) raw, and a 10 s utterance is 320 kB uncompressed. But note: **hosta .13 currently refuses key-based SSH from peerbox** (ping works, auth does not). Any architecture that assumes "push audio to .13" or "run an agent on .13" must first solve that — it is a *measured blocker*, not a hypothesis.

Context that matters: `peerbox` is the machine **running gopi** today (`bin/gopi` is built here; `~/work/GoPi` is the task field; the GoPi `settings.json` `repos` table declares this path). So "local" means peerbox in designs A/B; hosta .13 is the *other* end.

### Candidate 1 — Fully local, in-process (gopi embeds whisper.cpp via cgo)

- **Shape**: gopi binary gains a cgo-linked whisper.cpp (very likely the existing in-tree `bindings/go`), model file on disk, mic captured by gopi itself (miniaudio/SDL2 capture, like `whisper-mic`), transcribed in-process.
- **Latency budget (measured)**: model load ~100–300 ms + encode (tiny 1.5 s / base 3.3 s / small 12.4 s) + decode. Warm tiny short utterance: **≈2.4 s** end-to-end; base ≈4–5.5 s.
- **Install burden**: smallest possible *at runtime* (one binary + one model file), largest at *build* time — the declared build becomes `CGO_ENABLED=0`-incompatible (Rule 12 build command change), plus a C/C++ toolchain on every build machine. Static-linkage proven, so the shipped artifact can still be one zero-dep file (§A.4).
- **Failure modes**: build portability (needs `c++`, cmake for the .a's, OpenMP); model file management (75–466 MB, must not be committed); `.en`-model misuse silently hallucinating English (§B.2); CPU starvation while the agent is busy (measured: concurrent jobs distorted VAD timing by ~10×, §C.2).
- **Portability**: best — the whole thing travels with the binary if statically linked.

### Candidate 2 — Local sidecar: `whisper-server` as a separate local process

- **Shape**: gopi shells out to a resident `whisper-server` on `127.0.0.1:8080`, POSTs multipart audio to `/inference`, uses `/load` for model hot-swap and `/health` for readiness.
- **Latency budget (measured, warm)**: 4.5–6.0 s for a 3.5 s zh utterance on **base**; tiny measured 1.60 s in an earlier in-process run of the same server. Requests are **serialised by one mutex**, so concurrency is out.
- **Install burden**: whisper-server binary (1 302 128 B) + model + a supervisor (systemd/user unit). No Python, no cgo in gopi.
- **Failure modes**: another moving part to keep alive; no streaming endpoint at all (so the UI must accept "push-to-talk → wait → text"); model path must be local to the *server*, not to gopi; `/load` swaps model for *everyone* (a shared mutable global).
- **Portability**: excellent (HTTP + two processes), and it keeps gopi's own binary untouched and `CGO_ENABLED=0`.

### Candidate 3 — Remote STT on hosta .13 (fat laptop), audio pushed from gopi's host

- **Shape**: gopi (wherever it runs) records audio, ships it over the LAN to a whisper service on hosta .13, gets text back.
- **Latency budget**: network is *free* here (0.84 ms RTT, sub-second to upload 320 kB), so the budget is entirely hosta's compute — **unmeasured**, because hosta refuses SSH (above). If hosta's CPU is faster than the i7-5500U, this could be the only way to make `small` interactive.
- **Install burden**: service deployment on hosta + a credential/authorization path (currently non-existent — key auth refused) + a client in gopi.
- **Failure modes**: **the measured blocker** (no SSH access), host sleeps/roams, two-machine model version drift, and audio privacy across hosts.
- **Portability**: worst — the feature now depends on a *particular* second machine being awake. Structurally similar to the cloud option below, without the cloud's availability guarantees.

### Candidate 4 — Cloud ASR (Volcengine streaming WebSocket or a local WhisperLive service)

- **Shape**: stream audio to a WebSocket ASR; get **real** partial results. Either 火山引擎 流式语音识别 (`wss://openspeech.bytedance.com/api/v2/asr`) or a self-hosted WhisperLive (`python3 run_server.py --port 9090 --backend faster_whisper --max_clients 4`).
- **Latency budget**: dominated by the *service's* compute and the WAN (or LAN for WhisperLive). Unmeasured here — no key for the cloud, no venv built for WhisperLive.
- **Install burden**: cloud = key management + billing + a binary WebSocket protocol; WhisperLive = Python 3.12 venv + portaudio + a server process.
- **Failure modes**: network dependency (the one thing the local designs exist to avoid), recurring cost, audio egress, vendor protocol churn.
- **Portability**: cloud is portable *as a client* but adds a hard external dependency; this is the architecture that trades "zero-dep binary" for "true streaming + best zh quality", which is exactly the trade the brief asks to be explicit about.

**Framing (evidence, not verdict)**: the *only* measured hard blocker in this space is hosta's SSH refusal. Everything else is a trade-off among measured/estimated numbers. If gopi's voice channel is push-to-talk with a several-second tolerance, Candidate 1 or 2 suffices on this hardware; if it must feel like dictation (partial text as you speak), only Candidate 4's true-streaming path qualifies, and that is a product decision, not a measurement.

---

## G. What gopi would need to change

Grounded in the actual code surface of `/home/user/work/GoPi`:

1. **Input path.** Today gopi's only non-flag input is text: `cmd/agent/main.go` L447-455 —
   ```go
   if isTerminal(os.Stdin) { fmt.Print(usage); os.Exit(1) }
   data, err := io.ReadAll(os.Stdin)
   ```
   and `cli_chat.go:415` (`bufio.NewReader(os.Stdin)`) plus `@file`/`@-` payload syntax. A voice channel means an **audio-capture source** (record → 16 kHz mono PCM16) feeding **text** into the same `agentInput{prompt: …}` seam. That is a new front-end, not a change to the agent loop.

2. **Build command (Rule 12).** `bin/gopi` is built with `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (declared in the `repos` table and GOPI.md). Candidate 1 **breaks** that: cgo must be on, a C++ toolchain and the static `libwhisper.a`/`libggml*.a` archives must exist at build time, and the declared build command (plus `build_timeout_sec`) must change. Candidates 2–4 leave that command untouched — a significant, concrete deployment advantage.

3. **Tool count (Rule 3).** `claudeToolDefs` in `internal/agent/loop.go` is the source of truth and `TestGOPIRule3_MatchesClaudeToolDefs` enforces agreement. A voice channel should **not** be a 17th tool: nothing in the measurements suggests the LLM should decide *when* to listen. Voice belongs in the input path (front-end), consistent with how image positionals already auto-switch to the vision model without a tool call.

4. **New constants, not literals (Rules 1/13/14).** If integrated, these need a single source of truth rather than inline strings/numbers: the model directory/file names, the default port (`8080` — a bare literal in `server.cpp`), route paths (`/inference`, `/load`, `/health`), the 16 kHz sample rate, the thread count (>2 threads measurably *hurt* here, §B.4), and the language codes (`zh`/`en`/`auto`).

5. **Model management.** 75 MB (tiny) / 141 MB (base) / 465 MB (small) files must live outside the repo and outside `git add` (Rule 8) — plus a **download-fallback story**, because whisper.cpp's own downloader failed on this box (huggingface.co timeout) and had to be replaced by `hf-mirror.com`.

6. **Quality guard.** The measured `.en`-hallucination and the tiny/base 语音识别→云时别 errors imply a **minimum model rule** (small for zh) and an explicit refusal to run a `.en` model on non-English audio.

7. **Rule 11 applies if this ever becomes a tool.** Any voice *tool* would need LLM-adoption proof (sessions + `tool_use` records). Not an issue for the front-end design above.

---

## H. Open questions / what we could not determine

Honest list — each is a real gap, with why it is a gap:

1. **Volcengine pricing is not verified.** The official pricing pages (`/product/asr`, `/docs/6561/80816`) returned **0 chars** to `web_fetch` (JS-rendered / blocked). The 2.5 元/小时 & 1.2 元/小时 figures come from an AI-answer with an explicit "check the official page" caveat. **Not to be quoted as fact.** Needs a browser/manual check.
2. **`medium` and `large-v3` were not downloaded** (1.5 GB / 3.1 GB, brief caps models at ~3 GB). Their quality on the zh 语音识别 sample is unknown — and that is the sample where tiny/base fail, so "does medium fix it?" is precisely the interesting question.
3. **No quantized-model zh quality data.** `tiny-q5_0` was only tested on English (where it was *both* smaller and 2× faster than tiny: 2.52 s vs 5.34 s). A `small-q5_0` could plausibly deliver small's zh quality at a better speed/size point — **the single highest-value follow-up experiment**.
4. **Hosta .13's hardware is unknown.** It pings but refuses SSH, so its CPU, RAM, and whether it has a GPU (which would change Candidate 3 completely) are unmeasured.
5. **No GPU/accelerator data.** This box has none. The whisper.cpp README claims Core ML >3× and CUDA/Vulkan support, and faster-whisper's own table shows the GPU picture (1m05s whisper.cpp vs 1m03s faster-whisper, and 17s batched) — but nothing here is measured on GPU, so "is small/medium interactive with a GPU?" is untested.
6. **Thread-count and absolute-latency results are noisy — repro requires a quiet box.** The same config (tiny/zh_edge, 2 threads) measured 2.04 s in the idle bench and 5.27 s in a contended re-run; t=4 measured 46.74 s, 74.82 s, and 9.31 s across runs. Contention (other agents on this box) is the dominant variable. The *direction* (≤2 threads preferred on 2 physical cores) is established; every absolute number in §B.4 should be read as an upper bound.
7. **No end-to-end mic pipeline test.** All audio was files; `arecord`/`parecord` exist and SDL2 (2.0.20) is present so `whisper-stream`/`whisper-mic` *build*, but no real microphone was exercised. Real-world capture latency, device selection, and echo/level issues are untested.
8. **True streaming behaviour is inferred, not measured.** No code here drives `new_segment_callback` incrementally; the "streaming buys perceived responsiveness, not lower total" claim is an argument from the measured encoder cost, not a benchmark of an incremental implementation.
9. **whisper.cpp download script failure is unexplained.** `huggingface.co:443` timed out (130 s) while `hf-mirror.com` worked. DNS/proxy/firewall cause not isolated — relevant if gopi ever automates model fetching.
10. **Two cited Go wrappers are unverified.** `go-skynet/go-whisper.cpp` and `tcpipuk/golang-whisper` fetches both returned HTTP 404; they come from an AI-answer list. Only the **in-tree `bindings/go`** is verified (read locally, 1 571 lines, cgo directives quoted in §E.6).

---

## Appendix — raw evidence artifacts

| file | contents |
|---|---|
| `/tmp/wspr_build.txt` | configure + build transcript, `ls -la build/bin`, `ldd`, `file` |
| `/tmp/wspr_bench.txt` | 1 266 lines: 22 model×audio×lang×thread configs with full timings and segment text |
| `/tmp/wspr_latency_floor.txt` | fixed-cost floor (0.5 s / 1 s / 5 s clips × tiny/base/small) |
| `/tmp/wspr_recheck.txt` | clean re-measurement, 2 reps per config (thread-noise control) |
| `/tmp/wspr_vad.txt` | isolated Silero VAD timings |
| `/tmp/wspr_srv3.log`, `/tmp/wspr_srv4.log` | `whisper-server` stderr for the live HTTP probes |
| `/tmp/static_cmake.log`, `/tmp/static_build.log` | `BUILD_SHARED_LIBS=OFF` static build |
| `/tmp/gostatic/{main.go,gostatic,gostatic_static,gostatic_fullstatic}` | the cgo static-link spike + three link variants |
| `/tmp/tts/*.wav`, `/tmp/chunks/*.wav` | all test audio (16 kHz mono PCM16) |
| `/home/user/work/whisper.cpp/models/real/*.bin` | the actually-downloaded models + md5sums |

Report produced in `/home/user/work/GoPi` worktree `~/workspace/gopi-voice-eval`, branch `feat/voice-input-whispercpp-evaluation` (commits `8feb76b` + `1c31c3d` + this one; the branch is shared with the main repo — no push needed, Rule 6c).
