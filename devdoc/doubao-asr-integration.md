<!-- doc-class: normative -->
# Doubao (火山/Volcengine) cloud ASR as `voice_input`'s second engine

Task: `01a09d6b-288d-739d-a981-15a21fba00b0` · Branch `feat/doubao-asr-integration` ·
Supersedes the "bundle a local model" route from `devdoc/doubao-asr-eval.md`.

## 1. What shipped

| Deliverable | Where |
|---|---|
| Cloud engine, pure stdlib | `internal/doubaoasr/` (`doubaoasr.go`, `http.go`) |
| Engine dispatch (the seam) | `cmd/agent/cli_voice.go` `transcribeFile` (local arm renamed `transcribeFileLocal`) + `cmd/agent/cli_voice_engine.go` |
| Engine knob (3 rungs: flag > env > settings) | `--engine`, `GOPI_VOICE_ENGINE`, settings `voice_engine` |
| Cloud knobs | `--doubao-key-file/--doubao-resource/--doubao-hotwords/--doubao-submit-url/--doubao-query-url`, `GOPI_DOUBAO_KEY_FILE`, `GOPI_DOUBAO_RESOURCE_ID`, `GOPI_DOUBAO_HOTWORDS`, settings `doubao_key_file/resource_id/hotwords` |
| Zero-dep enforcement | `TestZeroDependencyStdlibOnlyImports` (parses the package's own imports; fails on any non-stdlib first segment) |
| Tests | `internal/doubaoasr/doubaoasr_test.go` (fake service, 1967 lines of behaviour incl. raw-verdict propagation), `cmd/agent/cli_voice_engine_test.go` |

### The two engines

- **doubao** (**DEFAULT** since 2026-10-07, §3): 豆包/Volcengine 录音文件识别 2.0
  (`volc.seedasr.auc`) over `net/http`. No model, no runtime: the audio is
  uploaded, a request id comes back, `POST /query` is polled until the
  transcript is ready. Asynchronous/batch by design — see §4. Every run that
  resolves to it prints the privacy notice on stderr (§3).
- **local** (`--engine local`, unchanged in behaviour): Fun-ASR-Nano via the
  official FunASR llama.cpp runtime. 910 MB of weights (1.7 GB on disk) + a
  6.2 MB runtime binary, and nothing leaves the machine.

## 2. Why the dispatch sits inside `transcribeFile`

Every text-producing shape already funnels through one function — the source
comment calls it "the single core both server shapes call":

```
one-shot     gopi voice_input <audio>            cli_voice.go
HTTP server  gopi voice_input --server           cli_voice_server.go:134
pipe server  gopi voice_input --server --stdio   cli_voice_server.go:291
```

Branching **there** rather than at the three call sites is what makes
`--server`, `--server --stdio` and the one-shot path engine-identical by
construction: there is no way for one shape to grow its own engine logic.

The capture client (`--client`, `--client --stdio`) stays **engine-blind** on
purpose: it records PCM16 and POSTs it to a transcriber, so the engine is
decided on the far side of the hop. A machine with a microphone does not need
the cloud credential, and a server does not need a soundcard. That is the
existing shape — this change does not alter it.

## 3. Why the default is `doubao` — REVERSED 2026-10-07

**This section's conclusion is the OPPOSITE of what it said when the cloud
engine landed.** It was "the default stays local, and silence means local"; on
2026-10-07 the operator instructed the flip (task `01a116b6…`) and the default
is now `doubao`. The superseded argument is kept below, verbatim, because it was
not wrong — it was outweighed — and a later reader deserves to see what was
weighed rather than a conclusion that appeared from nowhere.

### The argument that decided it: the two engines' burdens are not symmetric

A default is not a neutral position; it is **what a machine with nothing
deployed gets**. The two engines do not compete on equal ground there:

| | `local` | `doubao` |
|---|---|---|
| models on disk | 1.7 GB of weights (910 MB faulted in per run) | **0 B** |
| runtime binary | the FunASR llama.cpp runtime (~6 MB) | **0 B** |
| setup before the first transcript | a deployment to discover and perform | an API key, one line |
| per-run first failure if unset | "the weights are not here" — a 1.7 GB correction | "the key is not here" — a one-line correction |

The old default made the LIGHT engine opt-in and the HEAVY one mandatory: a
fresh machine could not transcribe at all until 1.7 GB had been argued for and
placed. The new default trades that for a credential, which is the smaller
thing to discover — and the failure is refused **before** the microphone is
opened (`voiceDepsReady`), naming the missing knob and the way back.

### The two real costs, paid in the open rather than hidden

**1. Privacy — audio now leaves the machine by default, so it is SAID.** This is
the cost the reversal actually creates, and the answer is disclosure, not
silence. Every run that resolves to the cloud engine prints, on stderr, before
any upload:

```
[voice] PRIVACY — engine doubao UPLOADS the audio to
  https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit
  (X-Api-Resource-Id volc.seedasr.auc); the recording leaves this machine
  (the default — no --engine, no $GOPI_VOICE_ENGINE and no "voice_engine"
  settings key chose an engine). to transcribe ON this machine instead:
  --engine local, GOPI_VOICE_ENGINE=local, or "voice_engine": "local" in
  <settings path>
```

It names WHAT leaves (the audio), WHERE it goes (the endpoint that receives it
and the resource it is billed against), WHETHER anything chose it (an explicit
selection reads differently from the default — `voiceExplicitlyChoseDoubao`),
and HOW to take it back (all three channels). It is an **announcement, not a
gate**: no first-run confirmation, no state file, nothing to clean up. A gate
would be a second, invisible default, and the operator's lever is a flag they
already have.

**2. The first-run failure moved from "no weights" to "no key".** Made the most
common first experience, it is answered where it happens: refused at the
pre-flight check with the missing knob named, the exit code non-zero, and the
same three channels back to `local` in the message. **Never a silent
downgrade** — an absent key does not quietly transcribe on-machine, because a
run that silently changed engines would be exactly the confusion the notice
above exists to prevent. `unknown engine "bogus"` is still refused:

```
$ gopi tool voice_input a.wav --engine bogus
gopi tool voice_input: unknown engine "bogus" — want one of local, doubao (default doubao)
```

Precedence is unchanged and is still `--engine` → `$GOPI_VOICE_ENGINE` →
settings `voice_engine` → `config.VoiceEngineDefault` (one named constant, in
`internal/config`; the resolver's two terminal returns both read it, so the two
paths cannot disagree — Rule 13). `local` remains fully reachable by all three
channels and is unchanged in behaviour.

### The superseded argument (kept, not deleted)

1. **Privacy is not a default.** Uploading a recording to a vendor's servers is
   a decision a human must make. `gopi voice_input f.wav` with no flag must not
   silently send audio off the machine — so the cloud engine is reachable only
   by NAME.
2. **An upgrade must not change behaviour.** Existing scripts keep producing
   identical text; the local path's code is byte-identical (only the function
   name gained a `Local` suffix).
3. **No new failure mode for existing users.** A default cloud engine would
   make every existing invocation depend on: a key file, a network, an account
   quota, and a vendor's availability.

**What each one looks like now.** (1) survives as the disclosure obligation —
the operator does not have to *know* in advance, they have to be *told*, every
time, and be able to withdraw in one flag; the reversibility is what the
reversal was measured against. (2) is accepted deliberately: this change is
MEANT to change what a bare invocation does, and the operator asked for exactly
that. (3) is the standing risk of a cloud default, stated here rather than
argued away: a network outage, an exhausted quota or a vendor incident now
reaches the default path, and the correction is the same one flag.

## 4. The streaming probe: NOT granted (measured, first attempt ever)

The epic's first choice was the **streaming** resource
`volc.seedasr.sauc.duration`. It had never been attempted against this
account (earlier eval evidence covered only `bigasr`/`megatts` resources), so
the probe was run for real, twice, against the SAUC WebSocket endpoint:

```
ERROR: connect: upgrade refused: HTTP 403 logid=202609141803397016D6B16310E9E2EBC5
  body={"error":"[resource_id=volc.seedasr.sauc.duration] requested resource not granted"}
ERROR: connect: upgrade refused: HTTP 403 logid=20260914181717D4E7EF027BB92CDE7FBF
  body={"error":"[resource_id=volc.seedasr.sauc.duration] requested resource not granted"}
```
(raw: `out/probe/seedasr_grant.log`, `out/probe/sauc_seedasr_duration.stderr.txt`)

**Verdict: streaming is closed to this account.** The 403 arrives at the
upgrade handshake, before any audio — it is an authorization refusal, not a
request-shape problem. Consequence, stated honestly: **no streaming client was
shipped.** A client for a route that 403s would be dead code that cannot be
tested end to end.

Related measurement (same endpoint, same audio): pointing the *streaming*
endpoint at the AUC resources (`volc.seedasr.auc`, `volc.bigasr.auc`) also
fails — comma-numbered garbage `"2,000。0 80。4。无。"`, then status code
`45000081` / "Timeout waiting next packet" (`out/probe/sauc_auc20_*.txt`).
The streaming endpoint is not a substitute channel for AUC.

**The working cloud channel is therefore `volc.seedasr.auc`** (录音文件识别
2.0) — already proven at CER 0.69% / 16-16 technical terms in the eval, and
re-verified end to end below.

## 5. Real API evidence (working channel)

`internal/doubaoasr.Transcribe` on the 219.94 s evaluation clip, raw log ids
from the service's own response headers:

```
resource=volc.seedasr.auc
reqid=3046a932-1073-24b0-5fef-f88659071762
submit_logid=20260914182316E79694C51D6BCF2FCD6F
query_logid=20260914182339D24972B12898D24CC4FF
status_codes=[20000000 20000001 ... 20000000]   # 20000001 = processing, 20000000 = done
polls=10  duration_ms=219939  total_ms=22592  rtf=0.1027
text_head=2084，无言见人续章裸模型时代。2084年，人类终于驯服了语言，却也沦为语言的囚徒。…
```

`20000000`/`20000001` are the vendor's own `X-Api-Status-Code` values, carried
into `APIError` on failure (Rule 15) so an incident starts from the vendor's
code, not from "it didn't work".

### End-to-end through the product CLI (not a harness)

```
$ gopi voice_input out/audio/audio16k.wav --engine doubao
2084，无言见人续章裸模型时代。… 【full 220 s transcript, correct】 …
[voice] audio16k.wav in 18.4s (chunk=15s)
```

Full chain `--client` → `--server` (cloud engine), real HTTP + real API:

```
$ gopi voice_input --server --engine doubao &          # 127.0.0.1:7788
$ curl -s http://127.0.0.1:7788/health
{"chunk":15,"engine":"doubao","ok":true}
$ gopi voice_input --client --seconds 220 --server-url http://127.0.0.1:7788
… 【the same 220 s transcript, returned by the server】 …
```

Pipe shape (server shape B), raw f32 PCM in — the documented stdin contract —
correct transcript, `[voice] 14076124 bytes in, 20.0s`.

> **Format note (a real trap, measured):** `--server --stdio` reads **f32** PCM
> (`voiceWrapPCMAsWAV`), while the capture client uploads **s16**. Piping the
> s16 bytes I had first produced *hallucinated* text (`他上面写的他也不认识字…`)
> — the engine faithfully transcribed reinterpreted float samples. Root cause
> was the input encoding, not the engine. The HTTP/`--client` path uses s16;
> the stdio path uses f32.

`/health` and `/transcribe` now report the engine that actually ran
(`voiceEngineLabel`); the LOCAL label stays the exact string `"funasr-nano"`
it has always been, asserted by the existing contract tests.

## 6. Zero-dependency proof (mechanical, not prose)

The official Volcengine Go SDK was measured in the eval at **4 direct + 7
indirect modules, +3.65 MB** of binary — and its `ResourceID()` default
(`volc.bigasr.sauc.duration`) is a resource this account is not granted, i.e.
the SDK's own default is the 403. So the client is hand-rolled JSON over
`net/http`.

Four independent checks:

1. **Import gate (in CI):** `TestZeroDependencyStdlibOnlyImports` parses every
   production file of `internal/doubaoasr` **with `go/parser`** and fails on any
   import whose first segment contains a dot (i.e. `example.com/...`). Stdlib
   only: `context`, `encoding/json`, `errors`, `fmt`, `path/filepath`,
   `strings`, `time`, plus (`http.go`) `bytes`, `crypto/rand`,
   `encoding/base64`, `encoding/binary`, `io`, `net/http`, `os`.
   **The gate was red-proved, and the first version FAILED the proof**: it
   scanned source *lines*, so a single-line `import _ "modernc.org/sqlite"` was
   invisible — the gate stayed green while the package blank-imported a real
   third-party module. It now uses `go/parser` (both spellings by
   construction), and two tests lock that in
   (`TestParseImportsSeesEveryImportForm`,
   `TestZeroDependencyGateRejectsASingleLineThirdPartyImport`). Re-running the
   red-proof now fails the gate as it must.
2. **Third-party dep set, branch vs master:**
   `go list -deps ... ./cmd/agent/ | grep -v Standard` → **13 vs 13,
   identical**. No new third-party module (`out/deps.{master,branch}`).
3. **`go.mod` / `go.sum` vs master: byte-identical** (`diff` → rc=0). Nothing
   was added, nothing was tidied.
4. **Binary size delta: 21,988,876 → 22,062,597 = +73,721 B (+0.34 %)**, all
   of it new string literals and code — no new library code.

## 7. Credential hygiene

The key is read from a **file** (`~/.doubao/apikey.txt` by default), never a
config value; `LoadAPIKey` trims whitespace and refuses empty. A whole-tree
scan for the key and its first 8 characters returns **zero hits** in both the
worktree and the main repo (ripgrep, untracked files included):

```
rg -F -l -- "$KEY" .   → no hits        rg -F -l -- "${KEY:0:8}" .   → no hits
rg -F -l -- "${KEY:0:8}" /home/user/work/GoPi → no hits
```

`--doubao-submit-url`/`--doubao-query-url` exist so the endpoints are explicit
and testable (the package's own tests point them at a local fake); they are
also the seam that makes a private deployment a **named choice** rather than an
inference.

## 8. Model-zero-burden: the reason the cloud engine exists

Measured on this host (`du`, `ls -la`, `stat`):

| | local Fun-ASR-Nano | cloud (doubao 2.0) |
|---|---|---|
| Weights on disk | **1.7 GB** (`~/work/funasr-models`: encoder 469 MB + qwen3-0.6b q4km 484 MB + q8_0 805 MB); the runtime loads the **910 MB** encoder+LM pair | **0 B** |
| Runtime binary | **6.2 MB** (`llama-funasr-cli`) + its build tree | **0 B** |
| Setup on a new machine | download 2 files, build/pin a runtime, keep `--models`/`--runtime` paths honest | put a key in a file |
| First-run latency | model load before the first word | none |
| Accuracy (same clip, same scorer) | CER 1.58 %, technical terms 100 % | CER **0.69 %**, technical terms 100 % |
| Speed (220 s clip) | 71 s (0.32× RTF) | **18.4 s (0.10× RTF)** |
| Privacy | audio never leaves the machine | audio leaves the machine |
| Cost | 0 marginal | 0.8 元/小时, free tier 20 h |

**The selection core is the burden asymmetry, not accuracy alone**: the cloud
engine is the only way to have speech-to-text on a machine where a 1.7 GB
model + a separate runtime are unacceptable — a container, a fresh box, a
laptop. The accuracy gap (0.69 % vs 1.58 %) is a bonus; the *zero model
burden* is the reason the option must exist.

## 9. Latency framing — correction of a wrong conclusion

`devdoc/doubao-asr-eval.md` concluded "云端实时流式 220 s 音频要 221 s（1.00×）
→ 在'实时'这件事上云端比本地慢 3 倍". **That conclusion was wrong** and has
been corrected in place. The error: it read the **client's send pacing** as the
**service's processing speed**. Measured on the same endpoint, with paced=false:

```
[sauc_auc20_nopaced] log_id=2026091418140802084FFF55CFABD1D86B
  dial=13ms tls=17ms upgrade=126ms first_char=459ms last_send=468ms
  final=189064ms rtf=0.860 packets=1100
```

The client pushed the entire 220 s clip in **468 ms** and the service kept
emitting — the service is nowhere near 1.00×. `RTF≈1.00` appears only where
the *client* is deliberately throttled: the live-recording shape (you cannot
send faster than the microphone) and the official SDK's `/nostream` submit
pacing. Corrected comparison, same "submit and know" shape:
**cloud AUC ≈ 0.10× RTF, cloud `/nostream` 0.097×, local 0.32×** — i.e. the
cloud is ~3× *faster* on file transcription, the opposite of the old claim.

What survives: **the streaming resource is not granted to this account** (403,
log ids in §4), so cloud low-latency streaming could not be measured at all —
we have no first-hand number for it, and local remains the only *verified*
live (边录边出) path. That choice is about **availability**, not speed.

## 10. Not Done (honest)

1. **Live microphone capture was NOT exercised end to end on this host — there
   is no soundcard.** `arecord -l` prints "no soundcards found" and PulseAudio
   refuses the connection, so the real recorder cannot produce audio. The
   `--client` arm was exercised with a **stand-in `arecord`** (`out/shim/`,
   uncommitted) that answers enumeration and emits the bytes the real one
   would; the client's job — record PCM16, POST multipart, print the returned
   text — ran for real, and the server side of that hop is the production
   cloud path. **A real microphone end-to-end run (and therefore any claim
   about live mic → cloud latency) is untested here.**
2. **No streaming client is shipped** (`volc.seedasr.sauc.duration` → 403).
   If the resource is ever granted, the paced/non-paced SAUC client exists
   only as the evaluation code in `devdoc/doubao-asr-eval/minclient/`; it is
   deliberately NOT lifted into the product package, because the product must
   not carry a route that cannot be run.
3. **Hotword uplift is still unproven.** The chain is wired
   (`request.corpus.context` inline JSON, double-encoded, unit-tested) and the
   service accepts it, but the eval's A/B found only a case change
   (`rag`→`RAG`). No "hotwords improve accuracy" evidence exists yet.
4. **Speaker separation and `enable_ddc` are plumbed but unverified against
   real multi-speaker audio** — the request fields are sent and the response
   parser reads `utterances`, but no multi-speaker clip was scored.
5. **No rate-limit / retry policy.** A transient 5xx or a throttled response is
   reported, not retried. The eval's own rate-limit probe was inconclusive
   (59/60 empty response headers), so a retry policy would be guesswork.
6. **The AUC batch shape waits for the whole file** — for a long recording the
   first word arrives only after the last one is uploaded. That is the nature
   of the granted channel, not a defect of this integration.

## 11. Verification (commands, all green)

```
go build ./...                                        BUILD_OK
go vet ./internal/doubaoasr/ ./cmd/agent/             clean
go test ./internal/doubaoasr/ ./internal/config/ ./internal/standards/ -count=1
  ok internal/doubaoasr  7.8s · ok internal/config 1.8s · ok internal/standards 11.0s
go test ./cmd/agent/ -run 'TestVoice|TestTopLevelUsageShowsVoiceInput|TestUsage' -count=1
  ok cmd/agent 4.5s
go list -deps ... ./cmd/agent/  vs master             13 = 13 (no new module)
diff go.mod master:go.mod ; diff go.sum master:go.sum byte-identical
binary: master 21,988,876 B → branch 22,062,597 B     +73,721 B (+0.34 %)
key scan (key + first 8 chars), worktree + main repo  0 hits
```

Known unrelated red, pre-existing on clean master and NOT caused by this
branch: `TestWorkerDaemon_StatusReportsTheLiveDaemon` (reproduced on master at
`1335daed` with this worktree absent).

## 12. Deliverable declaration

- **Branch:** `feat/doubao-asr-integration` (worktree
  `~/workspace/gopi-doubao-integ`, shares the main repo's object store — the
  commit is already visible in `/home/user/work/GoPi`).
- **Recovery:** `git -C /home/user/work/GoPi show feat/doubao-asr-integration:devdoc/doubao-asr-integration.md`
  (and `…:internal/doubaoasr/doubaoasr.go`, `…:cmd/agent/cli_voice_engine.go`).
- **Never merged** (Rule 6a/9) — delivery branch only; merge is the PA's step.
