<!-- doc-class: historical -->
# voice_input --client: the refusal's ADVICE must name THIS run's target

**Run record** — task `01a10c19-8b69-78a3-b916-5bb7808288c1`, host peerbox,
2026-10-05. (historical: a past run's record, not a standing claim about
master.)

## The report

`gopi voice_input --client --out FILE` "still uses the previous run's
`--server-url`". Evidence (hosta, `/tmp/voice_input_client_out.txt`):

```
$ gopi voice_input --client --out /tmp/8seconds1.wav --seconds 8
[voice] wrote 256044 bytes of WAV (8.0s) to /tmp/8seconds1.wav
gopi voice_input: cannot reach transcriber at http://127.0.0.1:7788/transcribe:
  … dial tcp 127.0.0.1:7788: connect: connection refused
  (start it with `gopi voice_input --server --listen :7788`)
```

## What was measured before anything was changed

The client has **no memory**. The target is resolved per process —
`opts.target` is filled from this run's `--server-url` or from the named default
`voiceDefaultServerURL` (`cmd/agent/cli_voice.go:504` default, `:529` flag),
and there is nothing else to carry one: the only voice settings keys are
`default_device` and `voice_engine`; no env var, temp file, cache or last-used
state mentions a target (searched over all voice code and `internal/config`).

Four runs of the real binary (recorder stubbed on PATH, two stub transcribers
logging every hit) routed correctly every time:

| run | arguments | port hit |
|---|---|---|
| A | `--client --seconds 1` | 7788 |
| B | `--client --seconds 1 --server-url http://127.0.0.1:7999` | **7999** |
| C | `--client --seconds 1 --out c.wav` | 7788 |
| D | `--client --seconds 1 --out d.wav --server-url http://127.0.0.1:7999` | **7999** |

Also exercised: `--server-url=…` (joined), the flag placed before `--out`,
`--stdio` (64 000 f32 bytes on stdout, no POST), a dead port (exit 1, both
shapes), a live port (exit 0). So the transport honours the argument, and `--out`
never touches it.

## The root cause of the *reading*

The refusal message ended in a **literal** hint:

```go
return "", fmt.Errorf("cannot reach transcriber at %s: %w "+
    "(start it with `gopi voice_input --server --listen :7788`)", url, err)
```

printed for every target. Under a `cannot reach … <this run's URL>` line, a hint
naming `:7788` points at an address **this run never used** — which is exactly a
sentence that reads as "it is still using the previous run's server". (In the
reported run there was no `--server-url` at all, so 7788 *was* this run's
documented default; the contradiction between the two halves of the message is
what produced the report.)

The defect class is Rule 15: a message that contradicts the state it reports.

## The fix

`voiceStartServerHint(url)` (`cmd/agent/cli_voice_client.go`) derives the advice
from the URL that failed:

- loopback (`127.0.0.1` / `localhost`) → `--listen <host>:<that port>`; the hint
  is built from constants, not from `net.JoinHostPort`, so the loopback spelling
  never becomes `[::1]` cosmetically;
- any other host → "run `gopi voice_input --server --listen :<port>` **ON that
  host**", instead of pointing the operator at this machine's loopback;
- a target carrying no port → falls back to `voiceDefaultListenPort`, the port
  the server's own default bind uses (Rule 2: one source for that value).

## Guards (both proven red on pre-fix code)

- `TestVoiceForwardRefusalNamesTheFailedPortNotTheDefault` — drives the real
  `voiceForwardHTTP` against a dead port; asserts the error names that port and
  contains no `:7788`. Reverting only the message line fails it with the
  operator's exact sentence.
- `TestVoiceStartServerHintQuotesTheFailedTarget` — five table cases (explicit
  port / LAN peer / `localhost` / no port / shipped default), each with an
  `absent` needle so the default cannot return.
- `scripts/voice-out-e2e-stub.sh` **case E** — the same property end to end on
  the real binary: dead non-default port ⇒ the advice names that port, never
  `:7788`, and the debug WAV survives.

## Not done

- No physical-microphone run (peerbox has no sound card; the fleet's capture host
  is HP). The recorder is stubbed; everything above it is real code.
- A **sticky** default target was deliberately not invented: the default is
  documented (`--server-url` default `http://127.0.0.1:7788`) and a new
  persisted knob would be a dependency (Rule 35) with no observed failure
  demanding it while the flag exists.
