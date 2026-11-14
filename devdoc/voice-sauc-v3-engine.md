<!-- doc-class: normative -->
# The v3 SAUC arm: a third cloud engine for `voice_input`

**Task**: `01a132a973fb747da905337f67396b57` · **Date**: 2026-10-13 ·
**Status**: delivered, live run green.

## Why a third package, when the other two are already there

`voice_input` had two cloud arms and one on-machine arm. The two clouds do
**different jobs**: `recording` is the batch engine (录音文件识别, `volc.seedasr.auc`
— one POST, one polled transcript) and `streaming` is the v2 real-time channel
(`api/v2/asr`, cluster `volcengine_streaming_common` — a JSON envelope inside
binary frames, partials while you speak).

The v3 SAUC endpoint (`api/v3/sauc`, `volc.seedasr.sauc.duration`, 豆包流式语音识别模型
2.0) is **the same job as v2 on a different protocol**, and the difference is not
a flag. Its envelope is a different shape (4-byte header nibbles, big-endian
sequence and length fields, a **negated** sequence number marking end-of-stream)
and its credential is a different **shape** entirely. A shared implementation
would be two implementations behind one name, so the split follows the existing
line: **the transport is shared, the application protocol is not.**

| | shared | not shared |
|---|---|---|
| RFC 6455 handshake, masked frames, message reassembly | `internal/doubaostream` (`WsDial` / `WriteBinary` / `ReadMessage`) | — |
| the WAV decode to 16 kHz mono PCM16 | `doubaostream.DecodeAudio` | — |
| the credential FILE format and location | `doubaostream.LoadCredentials` / `DefaultCredentialPath` | — |
| the request envelope, the packet cadence, the response reading, the credential HEADER shape | — | `internal/doubaosauc` |

## The measurement this channel is built around: the 403 is a SHAPE, not a grant

On this account, against `bigmodel_nostream`, the same resource id, the same clip:

```
X-Api-Key: <UUID>                                    → HTTP 403 requested resource not granted
X-Api-App-Key: <AppID> + X-Api-Access-Key: <Token>   → 101 Switching Protocols
```

The 403 **reads** like "this product line was never enabled for you", and that
reading is what makes it expensive: the operator goes to the vendor's console
looking for a grant to buy. The actual cause is that the single-key shape (the
UUID key the batch engine is configured with) belongs to a **different
application**, so it can never be granted this product line.

CONSEQUENCES IN THE CODE, not just in this document:

- the credential's shape is decided in **one** resolver
  (`doubaosauc.ResolveCredentials`) and refused **by name, before a socket is
  opened** (`voiceSaucDepsReady`), naming both shapes and the file it looked for;
- the `sauc` settings block deliberately has **no `api_key` field** — the block's
  own shape says which credential the channel takes, so an operator cannot write
  a configuration that cannot work;
- the single-key shape stays implemented *and tested*, because the CONTROL is what
  makes the comparison reproducible: a client that can only speak the working
  shape cannot show anyone why the other one fails.

The vendor's 403 is not "wrong" — it is a report about the credential, phrased as
a report about the resource. A client that echoes it without the shape is the
defect that this channel exists to avoid.

## The protocol, in one table

Every message is `4-byte header | optional fields | payload`:

| byte | meaning |
|---|---|
| 0 | protocol version (high nibble) \| header size in 4-byte units (low nibble) — always `0x11` |
| 1 | message type (high) \| flags (low): `0x01` carries a sequence, `0x02` marks the last packet |
| 2 | serialisation (high: `0x01` = JSON) \| compression (low: `0x01` = gzip) |
| 3 | reserved, `0x00` |
| [4..8) | sequence, big endian — the client's parameter frame is **1**, audio starts at **2** |
| [8..12) | payload length, big endian |
| … | the payload (gzipped JSON on the way out, gzipped JSON back) |

Two facts a reader will trip on if they are not written down:

1. **The parameter frame consumes sequence 1.** The server auto-assigns it, and an
   audio packet that repeats it comes back with a message about an
   `autoAssignedSequence` mismatch — which reads like a server bug and is a
   client off-by-one.
2. **The last packet NEGATES the sequence** instead of incrementing it, and sets
   the `0x02` flag. There is no separate end-of-stream message.

## What the live run measured

`gopi tool voice_input <5.616 s, 16 kHz mono, Mandarin> --engine sauc`, against
`bigmodel_nostream`, audio pushed as fast as the socket took it (no pacing):

```
[voice] sauc: 14 chars, 5616 ms audio, 29 packets, first text 961ms, logid 20261013090008047D9020282DE8AFDA4D
[voice] probe.wav in 1.1s (chunk=15s)
[voice] first text at 1151ms (engine doubao-sauc) = engine/channel 1148ms
```

- **5616 ms of audio → 29 packets** = 194 ms each, i.e. the 200 ms segment the
  constant names, with the tail packet short.
- **961 ms to the first text**, on a file-semantics arm where the first text *is*
  the answer — the service had the whole 5.6 s of audio and answered inside a
  second of it.
- The 5616 ms figure is the **audio's own length**, not a wait: on this endpoint
  the audio is pushed as fast as the socket accepts it, and the transcript arrives
  when the last packet does.

### Reconciling that 1.1 s with the probe's 5.52 s

`devdoc/doubao-sauc-legacy-auth.md` measured the SAME endpoint (4.83 s clip, legacy
pair) at **5.52 s to the final text**, and its own note says why: the probe runs
with `paced=true`, so its sender "本身就耗时 5.06s" (it pushes each packet on the
audio's own timeline, `send_speed 0.95×` real time). This run pushes as fast as the
socket takes it. Same endpoint, same credential shape, correct transcript on both —
**the two figures differ by the sender's pacing, not by the channel's speed**, and
neither is a latency claim about the other. That is the measurement behind
`Options.Paced`'s default being `false` for the file-semantics shape: pacing there
adds the clip's own length to the wall time and buys nothing.

**`Paced` is therefore a knob whose DEFAULT is "as fast as the socket takes it"**,
and the measurement behind that default is the one above: the file-semantics
endpoint bills by audio duration and takes a whole file, so pacing to the clip's
own timeline would add 5.6 s of wall time to buy nothing. A caller pointed at the
**bidirectional** endpoint (`bigmodel_async`, reachable by naming it) should set
`Paced: true`, because there the service expects real-time arrival — and the same
probe measured the arm that way: `bigmodel_async` (paced) delivered its first text
at **2.86 s**, half of what the paced `nostream` arm took.

## Where the two protocol details actually bit

Recorded because the next reader will meet them:

1. **Reader ahead of writer.** The first shape of `Transcribe` wrote the audio and
   read the ack in lockstep. That deadlocks the moment the service sends more than
   the socket buffer holds while the client is still writing: the write blocks
   against unread responses, and the run stalls against itself. The reader now
   runs in its own goroutine into a channel, and the writer drains whatever has
   already arrived between packets (`absorbReady`). The final wait then happens
   once, at the end, with a budget.
2. **A close frame is not an EOF.** `WsConn.ReadMessage` translates the peer's
   close into `WsCloseError{Code, Reason}` — the vendor's own goodbye sentence is
   the most useful line in a mid-stream disconnect, and a bare `io.EOF` throws it
   away. `WsCloseCodeNormal` is exported so "the peer said goodbye" (a result) is
   distinguishable from "the peer vanished" (a failure) without matching prose.

## Rule 35 check: what persistent dependency did this add?

**None.** No cron line, no unit, no script, no state file, no lock, no socket
beside the request's own, no environment variable an operator must set. The two
knobs of the new block live in the settings document the rest of the surface
already uses; the credential is the operator's **existing** file; the only
resident thing this change can start is the same `gopi tool voice_input --server`
that already existed.

## Configuration

`CONFIG.md` carries the rows in the same commit (the doc gate is green):

- `audio_engine` gained the value `sauc`;
- `audio_engine_param.sauc.endpoint` and `.resource_id` are documented **in the
  `audio_engine` row's description** rather than as new §2c rows — the block has
  no environment channel by design (footnote 4 of the table), and a §2c row with
  an empty env cell would be an unjoinable row the flag gate reports.
- the credential paragraph names the shape and the measured 403.

## Not done (honest)

- **The bidirectional endpoint is reachable but not exercised end to end.** It is
  named (`EndpointBidirectional`), it is what `Options.Paced` is for, and no live
  run in this task used it: the acceptance criterion was the file shape, and a
  second live run would have been a second experiment rather than the same one
  repeated. Its partial handling is therefore covered by the frame tests and by
  the endpoint constant, **not** by a measurement.
- **No live run of the chat `/voice sauc` row.** The row is printed through the
  same seam and the same functions the CLI path uses (`voiceSaucEffectiveEndpoint`
  / `voiceSaucEffectiveResourceID` / `voiceSaucCredentialPath`), and the chat
  surface is a REPL — driving it in this task would have been a terminal-driving
  experiment whose result the epic did not ask for. The engine reaches the same
  `transcribeFile` seam, which IS exercised.
- **The recording of the 403 pair above is a measurement from the epic's probe**,
  reproduced here as prose and re-encoded as tests of the HEADER SHAPE. This run
  did not re-issue the 403 request: doing so would have meant deliberately sending
  the wrong credential to a live service for a figure already measured.
