<!-- doc-class: historical -->
# The streaming voice engine (`doubao-stream`)

2026-10-08. `gopi tool voice_input` grew a THIRD engine arm: the Volcengine v2
real-time streaming channel. This is the record of what the channel actually
does, because three of its properties were learned by measurement and each of
them is invisible from the code.

## What it is, and why it is a separate engine name

`transcribeFile` had two arms — `local` (Fun-ASR-Nano on disk) and `doubao` (the
v3 AUC **batch** channel) — and the new one is `doubao-stream`: the same vendor,
the other protocol.

| | `doubao` (batch) | `doubao-stream` (v2 realtime) |
|---|---|---|
| protocol | HTTP POST + poll, `api/v3/auc/bigmodel` | one WebSocket, `api/v2/asr` |
| cluster | `volc.seedasr.auc` | `volcengine_streaming_common` |
| credential | one API key, `X-Api-Key` | `APPID` + `TOKEN` (KEY=VALUE file) |
| when text exists | after the file is uploaded AND polled | a PARTIAL while you speak |
| measured, 10 s clip | 2.6 s, one transcript | 11.0 s (real time), 30 partials, first at 2.04 s |

Two engine VALUES rather than one engine with a flag, because a caller must be
able to choose: the shapes differ in the bill (batch is priced per audio hour; a
stream is billed against the streaming duration package), in latency, and in what
an interrupted run costs. `doubao-stream` is **never the default** — the
streaming grant is separate on the vendor's side, and the batch engine's own
guarantee ("a machine with nothing deployed still transcribes") has to survive.

## The three properties that are only visible when you run it

### 1. The service's dialogue is response-then-ack, and that dictates the read budget

Every audio packet is answered by a FULL response (message type `0x09`) and then
a bare ACK (`0x0b`, sequence 0, no payload), with the next response arriving
about two packets later.

The first implementation read **one frame per packet**. Consequence: the frame
read for packet N was the ACK for packet N−1, so the transcript was never seen
until the last packet — the run produced a correct final transcript and **zero
partials**, which is precisely the feature being added.

The second implementation read **until a FULL response arrived**. That is a
deadlock, and the service names it: `code 1020: Timeout waiting for 2-th
packet`. The response for packet N and the ACK for packet N are both already in
the socket, so a reader waiting for one more response blocks the writer above
it, and the packet the service is waiting for never goes out.

What works: **drain what has arrived, then return** (`Result.absorb`). Frames are
read while they keep coming and a read that expires ends the drain, so the next
packet goes out within its own slot.

### 2. The pacing is real time, and the packet length is in BYTES

The channel is a real-time recogniser: it expects packet n at n × chunk after the
first. A sender that stops to wait for each answer drifts behind — measured, a
200 ms read per packet turned 10.3 s of audio into a **29.6 s** stream, tripling
the billed duration and pushing the first partial to 3.65 s.

So the send loop waits for each packet's own slot on the audio timeline and
drains from the slack the slot leaves (`minDrainBudget` / `maxDrainBudget`).

The other half of that defect was a units bug: `loadAudio` yields PCM16 **bytes**
and the loop stepped a **sample** count, so every packet carried half the audio it
should have — 104 packets × 200 ms for a 10.3 s clip, with every partial an
audio-second late. The conversion now lives in one named constant
(`bytesPerSample16`), and `TestChunkSizingIsBytesNotSamples` is the regression
guard.

Measured after both fixes: `last packet at 10.288s` for 10.347 s of audio;
first partial 2.06 s; final transcript 11.35 s (the tail is the final drain).

### 3. Code 1013 is a verdict, not a failure

`1013` (`No valid speeches found in input audio`) means the service looked at the
audio and found no speech. That is a correct answer about the audio, and it is
carried as its own error type (`doubaostream.NoSpeechError`) so a caller can say
the true thing instead of "the engine failed" — and so an empty transcript can
never be printed as if it were a recognition result.

Measured on a 4 s synthetic silence clip:

```
gopi tool voice_input --server --stdio --engine doubao-stream < silence-4s.wav
exit=1
gopi tool voice_input: doubao-stream engine (code 1013): the service found no speech in this
audio (No valid speeches found in input audio) (logid …) — this is the service's verdict on
the audio, not a failure of the run
```

## Zero dependency, and why the package exists at all

`internal/doubaostream` is **stdlib only**. The official demo for this channel
imports `github.com/gorilla/websocket` and `github.com/satori/go.uuid`; gopi
writes its own: the HTTP/1.1 upgrade, masked client frames, fragmentation
reassembly, ping/pong, and `\x1f\x8b` gzip (no permessage-deflate — the
extension is not negotiated, and the payload compression the protocol's own
header nibble asks for is the gzip this client applies).

The constraint is **checked, not promised**:
`TestZeroDependencyStdlibOnlyImports` parses every production `.go` file in the
package and refuses any import whose first path segment carries a dot (the shape
of every module path, and of no stdlib path). A read floor means a broken glob
cannot pass vacuously, and `TestParseImportsSeesEveryImportForm` proves the
parser sees single-line, block and comment-trailing imports.

`gofmt`/`go.mod`/`go.sum` are untouched by this change: `git status --porcelain
go.mod go.sum` prints nothing.

## The seam: a signature that did NOT change

`transcribeFile(opts, path) (string, time.Duration, error)` is shared by the
`--server`, `--server --stdio` and file shapes. Streaming is "text while you
speak", so the two halves are split rather than the signature widened:

- the **return value** is the final transcript — what every existing caller
  already consumes;
- the **partials** travel on `Options.OnPartial`, an in-arm progress channel.

The local arm is the regression baseline for this whole feature and was not
touched: widening the signature would have reached it through all three call
sites whether or not streaming needs it.

The chat `/voice` window logic is also untouched — a window still captures, then
transcribes, and the engine it transcribes with is the one the resolver answers.
Rewiring the windows onto one continuous stream is its own task; doing it here
would change `/voice`'s behaviour for every engine.

## The credential

One knob, two formats. The streaming arm reads the **existing** key-FILE rungs
(`--doubao-key-file` > `$GOPI_DOUBAO_KEY_FILE` > settings `doubao_key_file`,
default `~/.doubao/v2_voice.env` for this channel) — no second credential
mechanism — and `doubaostream.LoadCredentials` reads the operator's own
`KEY=VALUE` document from whatever file those rungs name.

Discipline, enforced by `TestLoadCredentialsErrorsNameThePathAndTheKeyNeverAValue`:
a message may quote the **path** and a key **NAME**, never a value. A file wider
than `0600` is reported with its mode (`CheckCredentialFileMode`) rather than
silently chmod-ed.

`voiceStreamDepsReady` refuses a missing or unusable credential BEFORE anything is
sent — a credential problem that arrives as a handshake error costs a debugging
session.

## The stale help claim this replaces

The old `-h` engine note said *"The 流式 resource the epic wanted first is NOT
granted to this account (HTTP 403, measured), so the cloud engine is the batch
shape"*. The measurement is real but narrower than the sentence: what is not
granted is the **v3** streaming resource (`sauc`), while **v2** streaming works.
The note and `CONFIG.md` now say exactly that, and
`TestVoiceUsageTextAdvertisesTheStreamingEngine` fails if the old claim or an
unreplaced `{engineStream}` placeholder comes back.

## Contract-message shapes (for the next reader)

Client → server, 4-byte header (protocol/header-size, message-type/flags,
serialization/compression, reserved) then the fields:

| message | header | fields after the header |
|---|---|---|
| full client request | `11 10 11 00` | 4-byte size, gzipped JSON |
| audio, not last | `11 20 11 00` | 4-byte size, gzipped PCM16 |
| audio, LAST | `11 22 11 00` | 4-byte size, gzipped PCM16 (flag `0b0010` = end of stream) |

Server → client: `0x09` full response (4-byte size, gzipped JSON), `0x0b` ack
(4-byte sequence, **no** payload), `0x0f` error (4-byte code, then the size and
payload), `0x0c`… the wire is not trusted for anything it does not carry — a
frame that lies about its own length is refused.

## Verification pointers

- `go test ./internal/doubaostream/` — the frame codec, the WAV decoder, the
  credential reader, the zero-dependency scan.
- `go test ./cmd/agent/ -run VoiceStream` — the dispatch seam, the label, the
  no-speech split, the help contract.
- The live run: `## Verification` in `tasks/<id>-report.md`.
