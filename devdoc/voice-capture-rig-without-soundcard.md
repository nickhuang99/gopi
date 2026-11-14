<!-- doc-class: normative -->
# The voice capture rig: proving the bare capture on a host with no microphone

**Task:** 01a148dc-970b-7623-8e6d-98b53a7b9ccc · **File changed:**
`cmd/agent/voice_module_defects_test.go`

## The defect this closes

`TestVoiceClosingLineNamesTheEngine`'s last arm ("AND ON THE REAL BINARY") drove
`gopi tool voice_input --engine local --runtime <stub>` with no recorder
constraint, so it depended on **this machine having a capture device**. Measured
on hosta (a laptop with no sound card, 2026-10-15):

```
--- FAIL: TestVoiceClosingLineNamesTheEngine (30.85s)
    voice_module_defects_test.go:361: the bare capture exited 1
        gopi tool voice_input: no capture devices found: `arecord -l` lists nothing
        and no PulseAudio sources are visible — on a headless or VM host there is
        usually no sound card at all, so record on the machine with the microphone
```

A permanent red on every headless host is not a signal: it cannot be told apart
from a real regression, and it trains every reader to ignore the case.

## The fix: stub the RECORDER boundary, not the run

`voiceRecorderRig(t)` materialises three tiny scripts in `t.TempDir()` and the
test puts that directory **first** on the child's `PATH`:

| stub | answers | why |
|---|---|---|
| `pactl` | `list short sources` → one source `gopi-voice-rig-source`; `get-default-source` → the same name; `info` → `Server Name: gopi-voice-rig` | the production enumerator; `info` matters because `voicePulseServerUp` gates tier 3 on it |
| `parecord` | streams non-silent PCM16 (10 ms chunks, quarter scale) until stdout closes, appending its own argv to `$GOPI_VOICE_RIG_LOG` first | the recorder production spawns, with `parecord`'s own lifetime rule (no duration flag; the window ends by SIGINT + closed read side) |
| `arecord` | exits 1 with a named message | the ALSA arm is deliberately NOT modelled; the failure is recorded as a policy note and the Pulse arm is the one under test |

An unknown `pactl` argv is a **refusal** (`exit 1` + the argv on stderr), not
silence: a production question the rig does not model must be visible instead of
being answered with an empty listing that reads as "no device".

The tier the run reports is `[first enumerated]` — tier 4 — and that is
**deterministic on every host** precisely because the stub answers `pactl info`
with a server header (so tier 3 is skipped by the production rule) and the rig
pins no `GOPI_VOICE_DEVICE` (so tier 2 is empty).

`--device` was considered and rejected: the wildcard `pulse:@DEFAULT_SOURCE@`
would be announced instead of the rig's name, losing the evidence that the stubbed
enumerator supplied the device.

## Why the case cannot pass vacuously

Two assertions read the rig back:

1. `$GOPI_VOICE_RIG_LOG` is non-empty and carries the production parecord argv
   (`--raw`, `--rate=16000`) — the window really was opened by the script.
2. stderr announces `recording 2s from pulse:` and names `gopi-voice-rig-source` —
   the device came from the stubbed enumerator through the policy.

And the existing closing-line assertions are unchanged.

## Rules honoured

- **Rule 1** — every stub name, flag and env name is a named constant; the
  scripts are assembled from the OWNING constants (`voiceParecordBinary`,
  `voicePactlListFlag`, `voiceParecordRateFormat`, …) so a rig cannot describe an
  argv production does not spawn.
- **Rule 12 / 35** — no dependency: two `sh`/`python3` scripts under `t.TempDir`,
  nothing installed, nothing resident.
- **Rule 17** — the PATH entry and the log path go into the `[]string` handed to
  `exec` (`voiceStubEnv`), never into `t.Setenv`.
- **Rule 22** — `t.TempDir()` (the suite's scratch root, off `/`).
- **Rule 27** — no load is generated, and **nothing is skipped**: the case runs on
  every host.
- **Rule 31** — the case was actually executed before and after (see the report).

## Residual, stated

The rig models the Pulse arm only. The ALSA arm (`alsa:plughw:CARD=…`) has its
own coverage through injected runners in `cli_voice_device_policy_test.go`; a
rig that also modelled `arecord -l` would duplicate that surface rather than
prove the closing line this case is about.
