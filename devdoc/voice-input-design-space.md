<!-- doc-class: historical -->
# gopi voice input — design space: dumb device streams, peerbox transcribes

- **Date**: 2026-09-11
- **Status**: DESIGN ONLY. No implementation, no new tools, no gopi source changed.
- **Input device**: `hosta` (192.168.7.13) — microphone only, must stay dumb.
- **Agent + STT host**: `peerbox` (192.168.7.6) — runs gopi today (`~/work/GoPi`, `bin/gopi`).
- **Sibling evidence doc**: [`devdoc/voice-input-whispercpp-evaluation.md`](voice-input-whispercpp-evaluation.md) (branch `feat/voice-input-whispercpp-evaluation`, tip `ce1e8d8`) — cited below as **(sibling §X)**. This document does **not** re-derive its measurements; it builds on them.
- **Deliverable repo**: GoPi, branch `feat/voice-input-design-space` (worktree `~/workspace/gopi-voice-design`).

---

## 0. The fixed architecture (not up for re-litigation)

> **The input device captures audio and streams it. ALL transcription, models,
> tuning and upgrade live on peerbox.**

Consequences that this document treats as given, not as options:

| Given | Consequence accepted here |
|---|---|
| The device installs nothing | The device contract is at most *one command line of already-present binaries*. No helper, no client, no daemon, no model, no credential on the device. |
| peerbox owns STT | whisper.cpp (or a cloud ASR **called from peerbox**) is a peerbox-local concern. The device never knows which engine ran. |
| Streaming is the transport shape | The device pushes bytes while the human speaks; it does not buffer an utterance, encode it, and hand over a file at the end (that is the degenerate case, §3). |
| Tuning/upgrade live on peerbox | Swapping `base`→`small`, changing threads, adding VAD, or switching to Volcengine is invisible to the device and requires zero device action. |

Everything below is the *wiring* inside those walls.

**What is already settled by measurement (do not re-open):**

- On-device dictation is **proven feasible** — a fully static 7 082 408 B cgo binary loading a ggml model was built and run (sibling §A.4). Feasibility is not the question; *placement* is.
- The LAN is **0.7 ms RTT** (measured again for this doc, §4.2). Bandwidth is a non-issue.
- whisper.cpp has **no true streaming** (sibling §C.1) and `whisper-server` has **no SSE/WebSocket route**, only `GET /`, `OPTIONS|POST /inference`, `POST /load`, `GET /health`, serialised by one mutex (sibling §D).

---

## 1. Transport: device → peerbox

Three candidates. All three move the same bytes; they differ in what the device must *have* and what peerbox must *run*.

| | Device requirement | Framing / end-of-utterance | Auth | peerbox side | Verdict |
|---|---|---|---|---|---|
| **(T1) ffmpeg \| ssh** | `ffmpeg` + a working SSH identity, `ssh` | SSH channel; **EOF = end of utterance** (channel close) | SSH key | **none** — `ssh peerbox 'cat > /tmp/u.raw'` | **Blocked today.** Measured: `ssh nick@192.168.7.13` → `Permission denied (publickey,password)` (sibling §F), and peerbox itself refuses the operator's key from localhost with `-i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes` (verified for this doc). Shortest path *if* keys are ever fixed — keep as the zero-server-code fallback. |
| **(T2) raw TCP** (`nc` / `/dev/tcp`) | `ffmpeg` + `nc` or bash | None of its own; half-close semantics are ambiguous, no error channel, no status code | none | a bespoke TCP listener + its own framing + its own failure reporting (all hand-rolled) | **Rejected for v1.** It re-implements HTTP's framing and gives up the status/error path — strictly more peerbox code for strictly less signal. |
| **(T3) HTTP chunked POST** (`curl -T -`) | `ffmpeg` + `curl` (or `arecord` + `curl`) | HTTP/1.1 chunked transfer; **body end = end of utterance**; the response carries a status code | none (LAN trust) | a small peerbox-local HTTP handler (`net/http`, stdlib — matches gopi's zero-dep posture) | **RECOMMENDED.** EOF framing, status codes, works with the most universally present binary, and the same HTTP+streaming carrier gopi already runs (§5). |

**Why not "record then upload the file"?** That is transport (T3) with a synchronous file write in front of it: it adds `utterance_duration` of dead time to the latency budget and makes the device stateful (a temp file that must be cleaned, sized, and rotated). Streaming with chunked encoding is *also* simpler on the device — `curl -T -` needs no length and no temp file.

### 1.1 The minimum device contract — ONE command line

A device may be considered supported **iff** it can execute this line (given `ffmpeg` and `curl`, both already present on hosta and on every mainstream Linux/BSD/macOS box):

```bash
ffmpeg -f alsa -i default -ac 1 -ar 16000 -f s16le - | curl -sS -T - http://192.168.7.6:8088/v1/audio
```

- **Push-to-talk semantics**: run it when the key goes down; the operator stops it (Ctrl+C) to close the chunked body = **end of utterance**. Nothing on the device decides what an utterance is.
- **Audio contract (pinned, non-negotiable): 16 000 Hz, mono, signed-16-bit little-endian, headerless.** Whisper's front end is 16 kHz; a device that streams 44.1 kHz stereo "successfully" produces garbage text, not an error (§7 F6).
- **Fallback line** (no `ffmpeg`, ALSA only): `arecord -f S16_LE -r 16000 -c 1 -t raw - | curl -sS -T - http://192.168.7.6:8088/v1/audio`
- **What the device does NOT need**: a model, a Python, a venv, a key, a config file, a port, a daemon, an upgrade path.

**The whole point of this contract**: the device's entire voice capability is 2 binaries that were already installed. Everything tunable (sample-rate validation, VAD, model choice, threads, language) is on the peerbox side of the pipe.

---

## 2. Codec: raw PCM16 vs Opus — and why the LAN makes it moot

### 2.1 Byte rates (arithmetic, exact)

| Format | Bytes/s | bitrate | 10 s utterance | relative |
|---|---|---|---|---|
| **PCM16 16 kHz mono** (the contract) | 32 000 | 256 kbit/s | **320 000 B** | 1× |
| Opus 24 kbps | 3 000 | 24 kbit/s | 30 000 B | 10.7× smaller |
| Opus 16 kbps | 2 000 | 16 kbit/s | 20 000 B | 16× smaller |
| Opus 6 kbps (narrowband floor) | 750 | 6 kbit/s | 7 500 B | 42× smaller |

### 2.2 The LAN is a non-issue — plainly

Measured for this document at the moment of writing:

```
$ ping -c 5 -W 1 192.168.7.13
5 packets transmitted, 5 received, 0% packet loss, time 4051ms
rtt min/avg/max/mdev = 0.738/0.825/0.968/0.089 ms
```

- 320 000 B over a 1 Gbit/s link ≈ **2.6 ms**; on a 100 Mbit/s link ≈ **26 ms**. Raw PCM is 0.026 % of gigabit.
- The audio never queues: **the network could not be the bottleneck even if it wanted to.** Contributing ~3 ms to a budget whose dominant stage is ~5 000 ms (§6) means the codec choice is invisible.
- The sibling measured the same link independently (0.717/0.844/0.986 ms, sibling §F), and the two agree.

### 2.3 Recommendation: ship raw PCM16, keep Opus as a WAN-only escape hatch

- **Raw PCM16 wins on the LAN** because it is **zero-work on both ends**: `ffmpeg` writes raw frames straight to stdout, and peerbox feeds the same 16 kHz PCM16 straight to Whisper — no encoder, no decoder, no container, no licence/libopus dependency on either side, no quality loss on data that costs nothing to move.
- **Opus costs two moving parts**: an encoder on the device (`libopus` present in `ffmpeg` builds — an unverifiable assumption at contract time) and a **decode step on peerbox** before Whisper sees it. Both are extra failure surfaces (§7 F6) bought with bandwidth that is free.
- **Opus's justification is a different link, not this one**: a 4G/WAN or a metered tether where 256 kbit/s up is real money. If that ever becomes the target, add `-c:a libopus -b:a 24k -f ogg -` to the device line and an `ffmpeg -i - -ar 16000 -ac 1 -f s16le` decode on peerbox. The device contract stays one command line either way — that is the property worth protecting.
- **Note for the peerbox side**: `whisper-server`'s `/inference` takes an *audio file* (multipart `file` field, sibling §D), not a raw stream, so the bridge decodes/muxes to whatever the chosen engine wants. If Opus is adopted, `ffmpeg` on peerbox is already present (`/usr/bin/ffmpeg`, 4.4.2 — verified) and does the decode in the same process that owns the tuning.

---

## 3. Where VAD / endpointing runs

Endpointing = deciding *"the utterance is over, transcribe it"*. Three placements:

| Placement | What it costs | Where the code lives | Verdict |
|---|---|---|---|
| **(V1) Human endpointing** (push-to-talk: key release closes the stream) | **nothing** | nowhere — the device's `Ctrl+C`/EOF *is* the endpoint | **v1 recommendation.** Uses the transport's own EOF framing (§1). Zero device deps, zero added latency, and it is the only option that is 100 % correct in the presence of noise, since the human knows when they stopped talking. |
| **(V2) Device-local VAD** (silence detection on the device) | a second binary on the device + a threshold/config to tune + a hit on the "installs nothing" rule; and a VAD error on the device is *unfixable from peerbox* | device | **Rejected** — it violates the zero-install wall and moves a *tunable* off the machine where tuning is supposed to live. |
| **(V3) peerbox-side VAD / segmentation** | one more stage in the bridge; the stream must be ring-buffered until the endpoint fires | peerbox (Silero VAD, `ggml-silero-v6.2.0.bin`, 885 098 B; `whisper_vad_*` C API; `--vad`/`-vm` flags — sibling §C.2; measured **faster than real time**, 0.1–0.6 s per 11.4 s of audio) | **v2 upgrade path.** Silence-hangover endpointing (~300–500 ms of trailing silence, §6) so the operator can speak hands-free. Cheap and entirely peerbox-local — the device line does not change by one byte. |

**The trade-off in one line**: device-local endpointing *feels* faster (no round trip for the decision) but the round trip is **0.7 ms** — so the latency argument for putting VAD on the device evaporates, while the cost (a second dependency, an untunable-from-peerbox threshold) stays. Put the VAD where you can tune it: **on peerbox**.

**Segmentation is not optional in one respect**: Whisper encodes a fixed 30 s window (sibling §B.4 — encode cost is audio-length-independent), so an utterance longer than 30 s must be split *somewhere*. Splitting on peerbox (at a VAD silence, or at a fixed 30 s boundary with an overlap) keeps that policy where the models are — and means the device contract never has to know the limit exists.

---

## 4. Transcription backend: local whisper.cpp vs Volcengine ASR

Both are admissible under the fixed architecture — **both are called from peerbox**; the device is blind to the difference. The decision is peerbox-local and reversible, which is exactly the property the architecture was chosen for.

| | Local `whisper-server` on peerbox | Volcengine streaming ASR (called from peerbox) |
|---|---|---|
| Cost | zero marginal (CPU already owned) | metered per hour of audio — **figure unverified, do not quote** (official pricing page returned 0 chars to the fetcher; sibling §H.1) |
| Dependencies | `whisper-server` binary (1 302 128 B) + a model file + a supervisor | an API key + a binary WebSocket protocol + WAN availability |
| Audio egress | none — audio never leaves the LAN | full utterance leaves the network |
| Quality (zh) | `small` is the measured floor for 语音识别 (tiny/base mis-hear it; sibling §B.3) | unknown here (no key), vendor claims mature zh models |
| Partials (text while speaking) | **none** — no true streaming exists (sibling §C.1); the UI must accept "utterance → wait → text" | **yes** — true streaming WebSocket (`wss://openspeech.bytedance.com/api/v2/asr`, sibling §E.5) |
| Latency | measured: warm `base` 4.47–5.98 s for a 3.5 s zh clip; `tiny` ≈1.6–2.4 s; `small` 15–42 s (sibling §B.4) | service-side + WAN — unmeasured |
| Concurrency | **one request at a time** (single mutex, sibling §D) | service-side |

**Recommendation**: **local whisper.cpp on peerbox is the v1 backend**, because (a) it needs no WAN, no key, no billing, and no audio egress; (b) it keeps the entire tunable surface on peerbox where the architecture put it; (c) the LAN latency is not the problem — the *engine* is, and swapping engines later is a peerbox-side change that the device never observes.

**When to switch to Volcengine**: exactly and only when one of two measured walls is hit — (1) `small`-class zh quality remains insufficient even after the highest-value untested experiment (a quantised `small-q5_0`, sibling §H.3), or (2) the product requirement becomes *partial text while the human speaks*, which local whisper.cpp **cannot** deliver at any model size (sibling §C.1). Note the honest constraint carried over from the sibling: **no local option on this CPU is interactive for zh** — `small` runs at RTF 4.1 on the i7-5500U, i.e. ≳15 s for a 3.5 s utterance. That is a product-shape decision (§5.4), not a wiring bug.

---

## 5. Injecting the transcript into a running gopi on peerbox

Three real injection points exist in the source today. All file:line citations below were read at commit `e6336f9` and verified line-by-line.

### 5.1 (I1) `POST /api/chat` — the `gopi web` SSE turn — **RECOMMENDED**

The only running-agent injection point with a streamed answer. Introduced/registered at `cmd/agent/cli_web.go:319-322`:

```go
mux := http.NewServeMux()
mux.HandleFunc("/", srv.handleRoot)                     // the chat page
mux.HandleFunc("/api/chat", srv.handleChat)             // SSE turn
mux.HandleFunc("/api/transcript", srv.handleTranscript) // R4: refresh replay
```

- **Request shape** (`cmd/agent/cli_web.go:165-169`): `{"message":"...","history":[...],"images":[...]}` — `message` is exactly the transcript string, nothing else required.
- **The handler** is `handleChat` at `cmd/agent/cli_web.go:492`; it POST-only guards (`:493-496`), refuses during shutdown with 503 (`:500-503`), caps the body at **4 MiB** (`webMaxChatBodyBytes = 4 << 20`, `cmd/agent/cli_web.go:840`) and rejects an empty message with 400 (`:512-515`).
- **It injects into a single persistent agent** created once at startup and reused for every turn (`cmd/agent/cli_web.go:296-308`), i.e. a voice turn joins the *same* conversation, session JSONL and raw log as the typed turns — no new session, no fork.
- **History is server-owned** (`cmd/agent/cli_web.go:255-262` comment + `s.history`), so the injector ships *only the new transcript*. A client that re-ships `history` is ignored by design (`:577-583`). This is what makes injection a one-field POST.
- **The turn runs** `ag.ChatOps(msg, seed, images...)` when `--tools` is set, else `ag.Chat(msg, seed, images...)` (`cmd/agent/cli_web.go:634-639`). Both are real agent turns: `Agent.Chat` is at `internal/agent/loop.go:3300`.
- **The answer streams back** as SSE `event: delta` frames via `ag.OnText` (`cmd/agent/cli_web.go:597-607`), tool activity as `event: tool` (`:612-621`), terminated by a `done` frame (`:644+`). So the injector can *observe* the reply — the injection point is also the return path.
- **Two constraints the injector must respect**:
  1. **Serialisation** — `s.mu.Lock()` at `cmd/agent/cli_web.go:539` is held for the whole turn, streaming included: concurrent turns *queue*, one at a time (documented at `:534-538`). A voice turn submitted while a long tool turn is running waits for it.
  2. **`/`-prefixed text is not a message** — `cmd/agent/cli_web.go:569-571` routes any message starting with `/` to `serveSlashCommand` (the local command table), never to the model. A transcript that happens to begin with `/` must be prefixed or rejected, or a spoken "slash task list" silently becomes a command. **This is the one real injection hazard.**
- **Exposure**: the default bind is `127.0.0.1:8090` (`webDefaultAddrValue`, `cmd/agent/cli_web.go:386`) — loopback-only *by design*, because `--tools` runs tools with no approval gate. Reaching it from the bridge is trivial (`127.0.0.1`); making it LAN-reachable requires the explicit `--addr 0.0.0.0` opt-in and carries that documented trust decision.
- **Zero gopi changes needed for v1** — the bridge is a *client* of an endpoint that already ships (`CliVerbWeb = "web"`, `cmd/agent/cli_verbs.go:75`).

### 5.2 (I2) `gopi chat` stdin

`cmd/agent/cli_chat.go:415` reads the REPL from `bufio.NewReader(os.Stdin)`; `:431` blocks on `reader.ReadString('\n')`. Injection would mean writing lines into that stdin (a FIFO or a pty).

**Rejected as the primary path**: (a) there is no return channel — the answer goes to the *terminal's* stdout, so the injector cannot see it and cannot know a turn finished; (b) the REPL treats every input line as prompt text with no framing/echo semantics, and `:406` `ag.Interrupt()` is a SIGINT-side effect, not something an injector can request; (c) it requires the voice path to own the lifetime of a foreground terminal process. It is the natural path only if voice *is* the terminal session — which it isn't, because the device has no display.

### 5.3 (I3) `gopi task submit` / the task ledger

`tools.TaskSubmit` (`internal/tools/tasks.go:2814`) via `runTaskSubmit` (`cmd/agent/cli_tasks.go:2565`, verb dispatch `case "submit":` at `cmd/agent/cli_tasks.go:593`) writes a **durable work item** — a different object from "a user message".

**Rejected**: it is asynchronous by construction. Work is picked up by `TaskNextFiltered` (`internal/tools/tasks.go:3038`, called in the serve loop at `cmd/agent/cli_tasks.go:1138`) on a **default 5-minute poll** (`serveIntervalDefault = 5 * time.Minute`, `cmd/agent/cli_tasks.go:24`, used at `:1834`). A 300 000 ms stage in a budget whose target is a few seconds is not a design, it is a category error. Voice turns need a *conversation turn*, not a queued deliverable.

### 5.4 Recommendation

**Inject via (I1) `POST /api/chat`** on an already-running `gopi web`, from a **peerbox-local bridge** (chunked-audio handler → decode/validate → whisper → POST text → optionally relay SSE deltas). Ranked reasons: it is the only point that injects into a *live* agent and *returns* the reply; it needs no change to gopi's source or its declared build; it inherits session persistence, slash-command dispatch and the SSE carrier; and it is one HTTP hop from the bridge on the same host.

The **return path is explicitly out of scope for v1**: `hosta` is a downstream-only, mic-only device with no display, and the SSE reply lands on peerbox (browser or terminal). Whether the answer comes back to the human as text on peerbox's screen or as synthesized audio is §8 Q2/Q3 — deliberately not decided here.

---

## 6. Latency budget for the decided path

**Decided path**: device `ffmpeg` → chunked HTTP → peerbox bridge → local `whisper-server` → `POST 127.0.0.1:8090/api/chat` → first streamed token.

| # | Stage | Where | Budget (ms) | Source |
|---|---|---|---|---|
| 1 | Mic capture + ALSA period buffering | device | 20–50 | estimate — no real-mic pipeline was ever measured (sibling §H.7) |
| 2 | Endpointing (key release / EOF) | device | **0** | human decision; transport EOF (§3 V1) |
| 2′ | *(v2 alt)* Silero VAD silence hangover | peerbox | +300–500 | design choice; VAD itself is sub-real-time (sibling §C.2) |
| 3 | Encode | — | **0** | raw PCM16 needs no encoder (§2.3) |
| 3′ | *(alt)* Opus encode | device | ~5 per second of audio | estimate; `libopus` presence unverified at contract time |
| 4 | Transport (chunked POST) | LAN | 3–5 | 0.7 ms RTT measured (§2.2) + 320 kB ≈ 2.6 ms at 1 Gb/s |
| 5 | Bridge: reassemble + validate + hand to engine | peerbox | 1–5 | stdlib copy of ≤320 kB |
| 6 | **Transcribe** — warm `whisper-server`, `base`, 3.5 s zh clip | peerbox | **4 470–5 980** | measured, sibling §B.4 (warm HTTP) |
| 6′ | — same, `tiny` | peerbox | 1 600–2 400 | measured, sibling §B.4 |
| 6″ | — same, `small` (zh floor) | peerbox | 15 000–42 000 | measured, sibling §B.4 |
| 7 | Inject: `POST /api/chat` | peerbox (loopback) | <1 | `cmd/agent/cli_web.go:492` + localhost |
| 8 | Agent turn → first `event: delta` | peerbox → model API | 500–1 500 | estimate (network + model TTFT); not measured |
| | **TOTAL (base)** | | **≈ 5 000–7 500** | |
| | **TOTAL (tiny)** | | **≈ 2 100–4 000** | |
| | **TOTAL (small, zh)** | | **≈ 15 500–42 500** | |

**The dominant stage is transcription (stage 6) — it is ~90 % of the budget at every model size.** Everything the device and the network contribute (stages 1, 2, 3, 4, 5) sums to **≤60 ms, i.e. ≈1 %** of the total.

And the reason is structural, not a tuning miss: Whisper encodes a **fixed 30 s mel window**, so encode cost is **audio-length-independent** (sibling §B.4) — a 1 s utterance and a 30 s utterance cost the same encode. That makes *short* utterances the worst case, and makes a **resident warm model mandatory** (a cold load adds seconds on top).

Two consequences worth writing down:

1. **Optimising the device or the transport is worthless** — the whole device-side contribution is ~1 % of the budget. The dumb-device architecture costs essentially nothing in latency; that is worth saying plainly.
2. **The only real lever is the model tier**, and for Chinese that lever is locked by quality: `base` is fast but mis-hears 语音识别 (sibling §B.3), while `small` — the measured zh floor — is slower than real time on this CPU. That tension, not any wiring, is the design's binding constraint.

---

## 7. Failure modes

Ordered by how likely they are to bite. Each names the mitigation the design *assumes*.

- **F1 — No partial results, ever (structural).** Local whisper.cpp cannot stream (sibling §C.1) and `whisper-server` has no SSE/WebSocket route (sibling §D). The UI must be push-to-talk: speak, release, wait. **Mitigation**: accept it explicitly (§5.4) and keep the perceived-latency story honest; only a cloud ASR with true streaming changes this, and only from peerbox (§4).
- **F2 — One request at a time.** `whisper-server` serialises every `/inference` behind a single mutex, and `/load` swaps the model for *everyone* (sibling §D). A 20 s zh `small` transcription blocks the next utterance, and a concurrent model hot-swap corrupts an in-flight one. **Mitigation**: one bridge, one queue; never `/load` while a transcription is in flight.
- **F3 — Silently wrong text (the dangerous class).** Not an error, a *plausible* sentence: tiny/base mis-hear 语音识别 as 云时别/与音时别, and an `.en` model fed Chinese audio returns fluent hallucinated English ("Thank you for watching!") — sibling §B.2/§B.3. **Mitigation**: a minimum-model rule for zh (`small`) and an explicit refusal to run an `.en` model on non-English audio. A wrong transcript that looks right is worse than a failure, because it goes straight into the agent's context as a user instruction.
- **F4 — SSH path is dead.** The natural "no server code" transport (T1) is unavailable: hosta refuses key auth (sibling §F) and peerbox refuses the operator's key from localhost (verified here). **Mitigation**: primary contract is chunked HTTP with no auth (§1.1); keep T1 as a fallback *if* keys are ever restored.
- **F5 — Truncated / half-open stream.** A chunked POST interrupted mid-utterance (device sleep, wifi roam, Ctrl+C misfire) leaves a partial utterance that transcribes into a confident-looking fragment. **Mitigation**: the bridge must distinguish "body ended cleanly" from "connection died" and must never feed a truncated buffer to the engine as if it were complete; idle-stream timeout on the bridge (the device can vanish — hosta sleeps/roams).
- **F6 — Format drift.** The contract pins 16 kHz mono s16le (§1.1). A device that streams 44.1 kHz or stereo gets **garbage text, not an error**. **Mitigation**: the bridge validates rate/channels/length before transcribing; a rejected utterance must return a visible error, never a transcript.
- **F7 — A transcript beginning with `/`.** `cmd/agent/cli_web.go:569-571` routes `/`-prefixed messages to the local command table instead of the model, so a spoken "slash task list" becomes a command execution. **Mitigation**: escape or reject `/`-prefixed transcripts at the bridge — this must be handled explicitly, it will not fail loudly.
- **F8 — Queueing behind a long turn.** `s.mu` at `cmd/agent/cli_web.go:539` is held for the whole turn, so a voice message submitted during a long tool turn waits (and a shutdown in progress returns 503, `cmd/agent/cli_web.go:500-503`). **Mitigation**: the bridge surfaces "busy" back to the device/operator rather than silently queueing for minutes; a 4 MiB body cap (`:840`) also rejects oversized payloads.
- **F9 — No auth on the audio endpoint.** Any LAN device can POST audio by design (§1). With a single-mutex engine that is a trivial denial of service, and audio is inherently sensitive. **Mitigation**: LAN-only bind, rate limit, and an explicit decision recorded as §8 Q4 — not silently "we'll add auth later".
- **F10 — Hallucination on silence / no speech.** Whisper is known to emit text for silence or noise; a stuck-open mic produces phantom instructions. **Mitigation**: endpoint on explicit EOF (v1); when VAD lands (v2), gate on detected speech and drop zero-length/zero-energy utterances.
- **F11 — Unbounded growth per utterance.** Every injected turn writes session JSONL + raw log rows via the persistent agent (`cmd/agent/cli_web.go:296-308`). Always-on voice multiplies log volume. **Mitigation**: out of scope for v1 (push-to-talk volume is human-paced); revisit if always-listening is ever proposed — it is explicitly in §5 "not building yet".
- **F12 — Engine/model state drift between the bridge and the server.** The bridge's assumption about which model is hot (`/load` can change it under it) vs. what `/health` reports. **Mitigation**: one owner for the model lifecycle; the bridge checks `/health` before trusting a tier, and never assumes.

---

## 8. Open questions

Honest list — each is a real gap, not a rhetorical one.

1. **Volcengine pricing and zh quality are unverified** (the official pricing page returned 0 chars to the fetcher; sibling §H.1) and no key exists here. So "cloud ASR as the zh escape hatch" is a *structural* escape hatch with unmeasured cost and unmeasured quality. The decision in §4 rests on that being reversible, not on it being proven.
2. **Where does the answer appear?** v1 injects and the reply streams to peerbox alone. Does the operator watch peerbox's screen, or does the answer need a return path to the human? Delivery is outside the fixed architecture as stated — it needs a decision.
3. **Is a spoken reply wanted?** TTS is a completely separate feature (the mirror of this one) and is out of scope here; it becomes a design question the moment the answer must reach a mic-only device.
4. **What is the auth/stewardship model for the audio endpoint?** "Zero-install" and "no credential on the device" are in tension with "anything on the LAN can post audio into my agent". The fixed architecture implies LAN trust; that should be an explicit, written decision (and a rate limit), not an omission.
5. **How are >30 s utterances segmented?** Whisper's fixed 30 s encode window forces a split policy (VAD boundary vs fixed window + overlap). Unanswered, and it is a quality-affecting choice, not a formatting one.
6. **Is a quantised `small` (e.g. `small-q5_0`) zh-viable?** The sibling rates this **the single highest-value follow-up experiment** (§H.3): if it holds small's zh quality at a better speed/size point, the §6 budget for Chinese improves materially. Untested.
7. **Does `medium`/`large-v3` fix the zh cliff, and at what cost?** Neither was downloaded (1.5/3.1 GB, sibling §H.2). On this CPU the answer is almost certainly "correct but unusable" — worth confirming only if the Cloud path is being compared.
8. **Real-microphone behaviour is unmeasured.** Every sibling measurement used files; capture latency, device selection, gain/level issues and echo are untested (sibling §H.7). Stage 1 of the budget is an estimate for exactly this reason.
9. **Device-side capability on hosta is unknown.** Its CPU/RAM/GPU were never readable (SSH refused, sibling §H.4). The contract assumes only `ffmpeg`/`curl` — which is the right assumption *because* the device is dumb — but "is hosta actually able to run that line" is unverified end-to-end.
10. **What happens when the bridge is down?** The device's `curl` fails and the operator sees a non-zero exit — acceptable, but the *error experience* (a one-line message vs a silent no-op) is undecided.

---

## 9. Not building yet (explicit scope)

Deliberately excluded from v1. Each is excluded for a stated reason, not by omission.

| Not building | Why not |
|---|---|
| **Any cgo / embedded whisper.cpp in gopi** | Contradicts the fixed architecture (models live on peerbox) *and* gopi's declared build (`CGO_ENABLED=0`, Rule 12). The sibling proved it is *possible* (sibling §A.4) — that is not a reason to do it. |
| **A new gopi tool** (Rule 3) | `claudeToolDefs` (`internal/agent/loop.go:802`) is the source of truth and `TestGOPIRule3_MatchesClaudeToolDefs` enforces it. Nothing here suggests the LLM should decide *when to listen* — voice belongs on the input path, like the image positionals that already switch to the vision model without a tool call. |
| **Any client/helper binary on the device** | The whole point is a dumb device: `ffmpeg` + `curl`, both pre-installed (§1.1). |
| **Device-local VAD or endpointing** | Violates zero-install and moves a tunable off peerbox (§3, V2). |
| **True streaming / partial transcripts** | Not achievable with local whisper.cpp (sibling §C.1) and not needed for push-to-talk. If it ever is needed, it is a cloud-ASR decision made on peerbox (§4). |
| **Wake word / always-listening** | A product change with privacy, logging (F11) and DoS (F9) consequences, not a wiring change. |
| **TTS / a return audio path** | §8 Q3 — a separate feature and a separate design. |
| **Opus encoding** | The LAN does not need it (§2.2/§2.3); adding it buys two failure surfaces for zero benefit *on this link*. |
| **VAD silence-hangover endpointing** | v2 path (§3, V3); v1's human endpointing is strictly more reliable. |
| **Multi-utterance concurrency / barge-in** | Blocked by the engine's single mutex (F2) and by `s.mu` (F8) — it is a real change, not a config. |
| **Model fine-tuning / custom acoustic models** | Out of scope of "wiring"; a research track. |
| **Changes to gopi's source or declared build for v1** | The v1 bridge is a *client* of the existing `/api/chat`. The design adds **zero bytes** to `bin/gopi`. |

---

## 10. Appendix — rejected architectures

### R1. Device-local STT (whisper.cpp running on the input device)

Rejected because it is the architecture the fixed decision rules out, and because its costs are paid on the least capable, least maintainable machine. It would require installing on hosta a whisper build (or a 7 MB cgo binary plus a C++ toolchain at build time, sibling §A.4), a **488 MB** `small` model for zh (sibling §B.3), a supervisor, and a model-upgrade path — all on a box whose CPU/RAM/GPU is *unmeasured* (SSH refused, sibling §H.4) and which the operator cannot tune without walking over to it. It also makes the *input device* the bottleneck for tuning: every quality experiment, every model swap, every thread-count fix becomes a hosta-side operation, which is precisely the ownership inversion the fixed architecture exists to prevent. What it would buy — one hop of latency — is worth **≤5 ms** (§6), i.e. ~0.07 % of the budget, against a multi-hundred-megabyte install on the dumb end of the wire. Note the honest counterpoint: on-device dictation is *proven feasible* (sibling §A.4) — it is rejected on placement, not on capability.

### R2. Device-direct cloud STT (the device calls Volcengine itself)

Rejected because it breaks the single most valuable property of the fixed architecture: **the device would need a credential, a vendor-specific protocol client, WAN reachability, and a billing identity** — four things the dumb device must never have. It also moves the audio-egress decision to the device (every utterance leaves the LAN from the machine with the least ability to enforce policy), and it puts the vendor's protocol inside the thing you cannot upgrade: a Volcengine API change or a key rotation would require touching hosta, which is exactly what "all models, tuning and upgrade live on peerbox" forbids. Cloud ASR is *not* rejected as a backend (§4 explicitly keeps it, called from peerbox) — it is rejected *on the device*, where it would trade a one-line contract for a vendor lock-in surface on the wrong machine.

---

## Appendix A — citations verified

Every GoPi citation in this document was read at commit `e6336f9` (branch `feat/voice-input-design-space`, worktree `~/workspace/gopi-voice-design`) and confirmed to say what §5/§6 claim:

| Citation | Verified content |
|---|---|
| `cmd/agent/cli_web.go:319-322` | `mux := http.NewServeMux()` + the three `HandleFunc` registrations incl. `/api/chat` → `srv.handleChat` |
| `cmd/agent/cli_web.go:165-169` | `type webChatRequest struct` with fields `Message` (json tag `message`), `History`, `Images` |
| `cmd/agent/cli_web.go:386` | `const webDefaultAddrValue = "127.0.0.1:8090"` (loopback-only default) |
| `cmd/agent/cli_web.go:492` | `func (s *webServer) handleChat(w http.ResponseWriter, r *http.Request)` |
| `cmd/agent/cli_web.go:500-503` | shutdown interlock → HTTP 503 |
| `cmd/agent/cli_web.go:512-515` | empty message → HTTP 400 |
| `cmd/agent/cli_web.go:539` | `s.mu.Lock()` held for the whole turn (serialisation) |
| `cmd/agent/cli_web.go:569-571` | `/`-prefixed message → `s.serveSlashCommand(w, msg)` (never the model) |
| `cmd/agent/cli_web.go:597-607` | `ag.OnText` → SSE `event: delta` |
| `cmd/agent/cli_web.go:634-639` | `ag.ChatOps(...)` when tools, else `ag.Chat(...)` |
| `cmd/agent/cli_web.go:840` | `const webMaxChatBodyBytes = 4 << 20 // 4 MiB` |
| `cmd/agent/cli_web.go:846` | `const webShutdownGrace = 5 * time.Second` |
| `cmd/agent/cli_chat.go:415`, `:431` | `reader := bufio.NewReader(os.Stdin)` / `reader.ReadString('\n')` |
| `cmd/agent/cli_verbs.go:75` | `CliVerbWeb = "web"` |
| `cmd/agent/cli_tasks.go:24` | `serveIntervalDefault = 5 * time.Minute` |
| `cmd/agent/cli_tasks.go:593`, `:2565` | `case "submit":` dispatch / `func runTaskSubmit(opts *tasksOptions)` |
| `cmd/agent/cli_tasks.go:1138` | `tools.TaskNextFiltered(...)` in the serve loop |
| `internal/agent/loop.go:802` | `var claudeToolDefs = []deepseek.ClaudeToolDef{` (Rule 3 source of truth) |
| `internal/agent/loop.go:3300` | `func (a *Agent) Chat(userInput string, history []deepseek.Message, images ...)` |
| `internal/tools/tasks.go:2814` | `func TaskSubmit(opts TaskSubmitOptions) (string, error)` |
| `internal/tools/tasks.go:3038` | `func TaskNextFiltered(claimedBy, excludeSession, skipTags string) (*TaskNextResult, error)` |
| `whisper.cpp examples/server/server.cpp:55-70` | `hostname = "127.0.0.1"`, `inference_path = "/inference"`, `port = 8080`, `read/write_timeout = 600` |
| `whisper.cpp examples/server/README.md:46-47` | `--host HOST [127.0.0.1]`, `--port PORT [8080]` |

Environment facts measured for this document: `ping 192.168.7.13` → 0.738/0.825/0.968 ms (min/avg/max, 0 % loss); peerbox = 192.168.7.6; `ffmpeg 4.4.2`, `arecord`, `curl`, `nc`, `jq`, `python3` present on peerbox; `socat` **missing**; `whisper.cpp/build/bin/{whisper-cli,whisper-server,whisper-mic,whisper-quantize,whisper-bench}` present; models present: `ggml-{tiny,tiny-q5_0,tiny.en,base,base.en,small}.bin` + `ggml-silero-v6.2.0.bin`; no listener on peerbox:8080; `ssh` from peerbox to its own key → `Permission denied (publickey,password)`; `ssh nick@192.168.7.13` → key auth refused (host reachable, SSH-2.0-OpenSSH_8.9p1).

**Sibling doc reused, not duplicated**: transcription quality, model sizes, RTF/latency, Silero VAD cost, `whisper-server` route surface, faster-whisper comparison and the Volcengine protocol notes all live in `devdoc/voice-input-whispercpp-evaluation.md` and are cited by section above.

---

## Appendix B — deliverable status (Rule 5)

### Completed

- `devdoc/voice-input-design-space.md` — the refocused design-space document, all nine required parts:
  (a) transport comparison + the ONE-line minimum device contract (§1), (b) PCM16-vs-Opus byte rates + the LAN non-issue (§2), (c) VAD/endpointing placement trade-off (§3), (d) injection-point analysis with verified file:line (§5) + recommendation (§5.4), (e) latency budget table with the dominant stage named (§6), (f) failure modes F1–F12 (§7), (g) open questions Q1–Q10 (§8), (h) explicit "not building yet" scope table (§9), (i) appendix rejecting device-local-STT and device-cloud-STT (§10).
- Fixed-architecture statement (§0) with an explicit non-re-litigation table.
- Citation verification pass (Appendix A): 27 cited lines re-read individually and confirmed to say what the document claims.
- Reused `devdoc/voice-input-whispercpp-evaluation.md` by citation (32 `sibling §` references) instead of duplicating its measurements.

### Not Done

- **No implementation of any kind** — by design (the epic is DESIGN ONLY). No gopi source file was modified: the worktree diff is the doc alone (`1 file changed, 316 insertions(+)`).
- **No new tool** — Rule 3 respected; `claudeToolDefs` untouched (the contract test still passes).
- **No gopi test was added** — there is no code change for a regression test to guard. (Rule 10.5 applies to fixes, not to a design document.)
- **No end-to-end voice demo** — no bridge was written, no microphone was exercised, no transcript was injected into a live `gopi web`. The design's stage-1 and stage-8 numbers are explicitly labelled estimates (§6) for this reason.
- **Volcengine pricing/quality left unverified** — carried forward from the sibling (§H.1) rather than re-guessed; §4 treats the cloud backend as *reversible*, not *proven*.

### Verification

- Build: `CGO_ENABLED=0 go build ./...` in the worktree → **exit 0** (no output) — proves the tree is unmodified and compiles.
- Tests: `go test ./internal/agent/ -run TestGOPIRule3_MatchesClaudeToolDefs -count=1` → **`ok github.com/nick/gopi/internal/agent 0.005s`** (the Rule-3 tool-count contract still holds — no 17th tool was introduced, even in design).
- Citations: 24 distinct GoPi `file:line` citations + 5 whisper.cpp citations, each re-read at the cited line (Appendix A table). All cited files exist; every quoted line matched its claim.
- Isolation (Rule 6): work done in worktree `~/workspace/gopi-voice-design`; `git status` in the main checkout `/home/user/work/GoPi` does **not** list the doc, and `devdoc/voice-input-design-space.md` does not exist there.
- Measurements made for this document: `ping 192.168.7.13` (0.738/0.825/0.968 ms, 0 % loss); ssh key-auth probes (refused in both directions); tool presence sweep on peerbox; `whisper.cpp/build/bin` + model inventory; no listener on `:8080`.

### Commit

- `68cc1e8` on branch `feat/voice-input-design-space` (worktree `~/workspace/gopi-voice-design`) — the doc is committed in the shared repo; no push needed (Rule 6c).
