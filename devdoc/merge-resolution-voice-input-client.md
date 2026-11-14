<!-- doc-class: normative -->
# Merge resolution: feat/voice-input-client → master

Merge commit on `feat/voice-input-client` (parents `f5b2fd38` branch tip +
`4dd78fe9` master tip); the resolution and this devdoc are in that one commit.

## The conflict (not what the branch report predicted)

The branch's own handoff note (`tasks/voice-input-client-report.md`) predicted a
single usage-text hunk. Reproducing `git merge master --no-commit --no-ff` gave
**one conflicted file, 10 hunks — `cmd/agent/cli_voice.go`** — because master
had shipped a *competing* capture client in the meantime, so the two sides
disagree about the client's flags, defaults and help text, not just wording:

| | this branch (HEAD) | master |
|---|---|---|
| client impl | `cli_voice_client.go` + `cli_voice_device_policy.go` | `cli_voice_capture.go` |
| device ids | stable by-name ids, tier ladder (explicit > configured > system default > first) | device string passthrough |
| failures | explicit per-mode errors | named failure-mode matrix (silence detection, wall-clock bound) |
| flags | `--device`, `--seconds`, `--server-url`, `--stdio` | `--device`, `--seconds`, `--out`, `--recorder` |
| device listing | `--client --list-devices` | top-level `--list-devices` |

`master`'s `--listen` loopback fix and its wider engine-dependency guard are the
other two hunks; the rest is help text and the flag switch.

## Resolution

**Kept this branch's client.** The branch is the unit being merged and its
client carries the live-hardware proof (device-selection tiers, hotplug
pre-open, HTTP/stdio transports). The two implementations cannot coexist:
`runVoiceClient` and `voiceCaptureChannels` are each declared in both.
`cmd/agent/cli_voice_capture.go` + `cli_voice_capture_test.go` are therefore
removed in the merge commit.

**Kept master's unique values, wired to the kept client:**

- the top-level `--list-devices` verb (usable without `--client`;
  `--client --list-devices` still works and prints the same table);
- the wider engine guard `!client && !listDevices` (master's intent: a capture
  host needs no 910 MB of weights);
- master's loopback `--listen` text and the valueless-flag guard (the parser
  would swallow the word after `--client`);
- this branch's constants discipline (Rule 1b/13) over master's bare-string
  switch; master's two extra flags are named as constants
  (`voiceFlagOut`/`voiceFlagRecorder`) and deliberately **not** accepted — an
  unknown-flag error is louder than a silently ignored flag (Rule 15).

Usage text is the union: master's `--listen`/`--stdio` wording plus the
branch's device-selection order. Master's stale `--out`/recorder paragraphs are
dropped: they would advertise an implementation that is not the one shipped.

## Verification (bounded)

- `grep -E '^(<<<<<<<|=======|>>>>>>>)'` on the resolved file → 0 hits; no
  markers anywhere in `cmd/` or `internal/`.
- `gofmt -l cmd/agent/cli_voice.go` → clean.
- `go build ./cmd/agent/` after the duplicate removal → the **only** remaining
  errors are `merger_self_restart.go: undefined: mergerDaemon*` — and a pristine
  `git archive master` extract fails with **exactly the same** errors, so that
  break is pre-existing at master's tip, not introduced here.
- The voice parse/flag surface was checked against both sides' test
  expectations (`cli_voice_test.go`, `cli_voice_review_test.go`,
  `cli_voice_capture_test.go`): every flag both suites exercise is still
  accepted, `--out`/`--recorder` (master-only) now error as unknown.

## Follow-up owed (not this resolver's scope)

1. **Port master's failure-mode matrix** into `cli_voice_client.go` (named
   modes, silence detection, wall-clock bound) — the only real work lost by
   dropping `cli_voice_capture.go`, whose tests are the red-proofs for it.
   Restore `--out`/`--recorder` wiring at the same time (`voiceOptions.out` /
   `voiceOptions.recorder` and their constants are already in place).
2. **master's tip does not compile**: `cmd/agent/merger_self_restart.go` calls
   `mergerDaemonLogDirMode` / `mergerDaemonLogFileMode` / `mergerDaemonReadyEnv`
   / `mergerDaemonReadyChildFD` / `mergerDaemonAwaitReady` /
   `mergerDaemonAbandonChild` / `mergerSetDaemonStart` / `mergerRecordVerifiedRebuild`
   / `versionDirtySuffix`, none of which exist at `4dd78fe9`. Reproduce with
   `git archive master | tar -x -C /tmp/m && (cd /tmp/m && go build ./cmd/agent/)`.
