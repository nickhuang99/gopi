<!-- doc-class: historical -->
# Task Report: `gopi voice_input --client --out FILE` — the client's debug file

## The bug report

> *voice_input should allow client output to file. It is not only convenient, but
> also a mandatory requirement for voice_input at client to output a file for
> debugging purpose. And such command is not recognized:
> `gopi voice_input --client --out /tmp/nick.wav`.*

Reproduced exactly, on master `39a08e20`:

```
$ gopi voice_input --client --out nick.wav
gopi voice_input: unknown flag --out
```

## Root cause (not a missing feature — a broken promise)

`--out` was **advertised in the command's own help text** while the parser
**refused** it. Both halves were true at once:

- `cmd/agent/cli_voice.go` — the `voiceInputUsage` const listed
  `--out FILE       write PCM here (default: a temp file)`, and the constants
  `voiceFlagOut` / `voiceFlagRecorder` plus the struct fields
  `voiceOptions.out` / `.recorder` had been carried over from a previous branch.
- `voiceFlagMember()` returned `false` for `--out`, so `parseVoiceArgs` answered
  `unknown flag --out` — the loud refusal was deliberate (better than a silent
  no-op, Rule 15) but the capability was absent.

How it got that way: the wire-up lived in `cmd/agent/cli_voice_capture.go` on
branch `feat/voice-capture-failmatrix` (commits `7da0ac99`, `94f2438e`). That
file **never reached master**; the merge that landed master's voice work
resolved a whole-feature duplication (two competing clients — `runVoiceClient` /
`voiceCaptureChannels` declared twice) by keeping the branch's client
(`cli_voice_client.go`) and dropping the capture module with the `--out` writer.
The merge note left in `voiceOptions.out` said as much: *"nothing assigns these
two yet"*.

So the defect is precisely the class the class-level guard now closes: **a flag
the usage text promises, that the parser rejects.** The project's own memory of
that merge (`record #2326`) independently confirms the history — merged the
branch's client, dropped master's capture module, kept the constants.

## Completed

- **`--out` is accepted and honoured by `voice_input --client`.**
  `cmd/agent/cli_voice.go`: `voiceFlagOut` added to the `voiceFlagMember()`
  switch and read into `opts.out` in `parseVoiceArgs`.
- **The captured audio is written as a playable WAV, *before* anything is
  forwarded** (`cmd/agent/cli_voice_client.go`, `voiceWriteCaptureFile`). The
  container comes from the same `voicePCM16ToWAV` the HTTP route posts, so the
  debug file is **byte-identical to what the transcriber would have received** —
  a debug copy that differed from the forwarded audio would be worthless as
  evidence. Writing first is the point of the feature: a dead server, a mistyped
  `--server-url` or a refused device no longer consumes the recording.
- **A successful write is reported**: `[voice] wrote 32044 bytes of WAV (1.0s) to nick.wav`.
- **Refusals are loud and named, and happen before the mic is opened** (a
  knowable mistake must not cost a recording):
  - empty path → *"--out requires a file path (e.g. --out nick.wav)"*;
  - a directory → *"--out DIR is a directory — name the file to write"*;
  - an existing file → *"--out f.wav already exists — remove it or name a new
    file (a capture must not overwrite a recording you kept)"*. Also enforced
    atomically with `O_EXCL`, so a file appearing between the parse and the write
    is not truncated either.
  - a **non-client** run (`--server`, a file transcription, `--list-devices`) →
    refused, naming what was asked for: accepting it there would write nothing
    while the operator believed a recording was being kept (Rule 15).
- **Write failures are never swallowed**: a missing parent directory and a
  permission denial get the action that fixes them; a **short write removes the
  partial file** and says so — a truncated WAV looks playable and would be
  debugged as if it were real audio.
- **The usage text now describes what the flag does** (WAV, before forwarding,
  the three refusals), and the stale claim *"write PCM here (default: a temp
  file)"* is gone (there was never a default file). Examples gained
  `gopi voice_input --client --seconds 8 --out nick.wav`.
- **Regression tests** — `cmd/agent/cli_voice_out_test.go`: flag accepted in both
  spellings (`--out F` / `--out=F`), default is empty, all misuse shapes refused
  with a message that is *not* `unknown flag`, the WAV is valid and identical to
  the HTTP container, an existing file survives, a missing directory creates
  nothing, and the **class-level guard**
  `TestVoiceUsageAdvertisesOnlyAcceptedFlags` — every flag the usage text
  advertises must be a member the parser accepts *and* must have a constant.
- **End-to-end proof on the real binary** — `scripts/voice-out-e2e-stub.sh`
  (see Verification).

## Not Done (honest)

- **No capture-only mode.** `--out` keeps the audio but the run still forwards it
  afterwards, so `--client --out nick.wav` against a stopped server records,
  writes the WAV, and then **exits non-zero** on the forward failure. That is
  deliberate (the primary use is a real client run whose evidence must survive
  its failure) but it is not "record and stop". A `--capture-only` flag is the
  obvious follow-up if someone wants the file as the whole result; it is not in
  this change, so the exit code of a forward failure is unchanged.
- **`--recorder` is still not accepted.** It belonged to the same dropped module
  (`cli_voice_capture.go`). The recorder argv is derived from the chosen device
  (`voiceRecorderCommand`), so an override has to re-decide the backend — that
  is the failure-mode-matrix work, not this task. The flag stays a NON-member:
  `unknown flag --recorder` is louder than accepting a flag that would do
  nothing, and the comment in `voiceOptions.recorder` now says so.
- **No real-microphone run.** This host has no sound card at all
  (`arecord -l` → *"no soundcards found"*; `pactl get-info` → *"Connection
  refused"*). The last mile against real hardware is HP-only
  (`devdoc/voice-client-e2e-hp.md`, `scripts/voice-e2e-hp.sh`) and was not run
  here; the recorder boundary is stubbed instead (Verification, case A–D).
- **Two older devdocs still show `--out` in its dropped-module, raw-PCM form**
  (`devdoc/voice-capture-failure-matrix.md` line 106:
  `--client --seconds 6 --out /tmp/hu.pcm`; `devdoc/voice-node-coverage.md`
  line 232 likewise). They are left untouched on purpose: they document
  `feat/voice-capture-failmatrix`, which never merged, so rewriting them here
  would describe a flag shape the tree does not have. Anything that revives that
  branch must reconcile the two.
- **No `CmdMode`/`_test.go`-level unit test of `runVoiceClient`'s ordering** —
  the ordering claim ("written before forwarded") is proven end-to-end by case C
  (forward to a dead port, file still valid), which is stronger evidence than a
  test double asserting call order.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./cmd/agent/` → OK; `go vet ./cmd/agent/` → clean.
- **Tests:** `go test ./cmd/agent/ -run TestVoice -count=1` → `ok … 1.949s`
  (the new file adds 8 tests). **Whole package, final gate:**
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` then
  `go test ./cmd/agent/ -count=1` → `ok github.com/nick/gopi/cmd/agent 192.830s`,
  0 `--- FAIL` lines. Note for anyone re-running it: the four environmental
  failures seen earlier were (a) three guard tests that need `../../bin/gopi` to
  exist — build it first (a missing binary fails loudly by design, it does not
  skip) and (b) `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound`'s
  `SQLITE_BUSY` when a concurrent gopi holds the db. Also: this linked worktree
  was removed twice mid-run by the concurrent worktree-residue sweeper, which
  produces a mass of unrelated "missing tests/fixtures/…" failures; re-create it
  (`git worktree add <path> <branch>`) and keep a live holder process inside it.
  The branch tip was never at risk — commits live in the shared repo refs.
- **RED-PROOF of the class guard** (Rule 10.4 — a guard that passes on the
  unfixed tree is vacuous). Reverting *only* the membership line, keeping every
  other fix in place:
  ```
  --- FAIL: TestVoiceParseAcceptsOutFlag
      parseVoiceArgs(["--client" "--out" "…/a.wav"]): unexpected error: unknown flag --out
  --- FAIL: TestVoiceUsageAdvertisesOnlyAcceptedFlags
      usage advertises --out but voiceFlagMember refuses it — an advertised flag the
      parser rejects is a broken promise (the --out defect)
  ```
  i.e. the guard reproduces the user's exact error string on the pre-fix tree;
  restoring the one line turns it green.
- **End-to-end on the real binary** (`scripts/voice-out-e2e-stub.sh`, PASS) —
  `arecord`/`pactl` stubbed on PATH (deterministic 440 Hz PCM16), everything above
  the recorder is the real code path. Workspace under `/dev/shm` (measured off the
  `/` partition, Rule 22 criterion), removed on exit.
  - **A** `--client --seconds 1 --out nick.wav --stdio` → exit 0, **no "unknown
    flag"**, and the 32044-byte WAV holds exactly `44 + 32000` bytes: RIFF/WAVE,
    format tag 1 (integer PCM), 1 channel, 16 kHz, 16-bit, `data` size == bytes
    carried, `len(pcm) == 32000` (the 1.0 s recording, not the 0.1 s liveness
    probe), `max>10000`, `min<-10000`, >100 distinct samples (**not silence**);
    stdout carried the matching 64000 bytes of f32 with `max|x|>0.3`.
  - **B** the same run **without** `--out` → the working directory stays empty and
    stdout is byte-count identical (64000), so the default behaviour is unchanged
    and the flag is what asks for the file.
  - **C — the bug-report scenario:** `--out nick.wav --server-url http://127.0.0.1:1`
    (nothing listening) → the forward **fails**, exit non-zero, **and the
    32044-byte playable WAV is still there**. Before this change that audio did
    not exist anywhere.
  - **D** `--out` onto an existing file → refused with *"already exists"*, exit
    non-zero, and the kept recording's bytes are unchanged.
- **Commit:** see the branch tip; `git show feat/voice-input-out:devdoc/voice-input-out-file.md`.

## Files

| File | Change |
| --- | --- |
| `cmd/agent/cli_voice.go` | `--out` accepted (`voiceFlagMember`), read + validated (`voiceValidateOutPath`), refused outside `--client`, usage text corrected, `--recorder` note updated, `voiceRunShape` |
| `cmd/agent/cli_voice_client.go` | `voiceWriteCaptureFile` + `voiceOutWriteHint` + create-semantics consts; the write is wired in before `--stdio`/HTTP forwarding |
| `cmd/agent/cli_voice_out_test.go` | new — 8 tests incl. the class-level advertised-vs-accepted guard |
| `scripts/voice-out-e2e-stub.sh` | new — zero-dep capture proof rig (4 cases) for a host with no sound card |
| `devdoc/voice-input-out-file.md` | this report |

## Design decisions worth knowing

- **WAV, not raw PCM.** The old help text said "write PCM here". A raw `.pcm`
  cannot be listened to, and "debugging a client" starts with *hearing* what the
  mic actually captured. Since the bytes are the same PCM16 the engine consumes,
  a WAV container loses nothing and makes the file inspectable/playable. The file
  is therefore the *exact* container the HTTP route sends — the debug artefact and
  the sent artefact are one object, so a difference between them can never be a
  red herring.
- **Refuse to overwrite, do not offer `--force`.** A capture that silently
  replaces yesterday's recording is a data-loss bug. The refusal costs one flag
  value; `rm` is right there. (Atomic `O_EXCL` backs the parse-time check.)
- **The flag is a `--client` flag only.** On the file-transcription, `--server`
  and `--list-devices` shapes there is no captured audio to keep, so accepting it
  would be the silent no-op Rule 15 forbids.
