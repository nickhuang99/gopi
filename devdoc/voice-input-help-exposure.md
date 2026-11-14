<!-- doc-class: historical -->
# Task Report: expose the voice-input CLI verb in `gopi -h` (and close the class)

## Epic
`gopi voice_input` (speech-to-text, Fun-ASR-Nano) was fully implemented — dispatcher
`isVoiceSubcommand` wired into `main.go`'s chain since the phase-2 work
(`cmd/agent/cli_voice.go`, design in `devdoc/SPEC-voice-input-phase23.md` +
`devdoc/SPEC-voice-asr-engine-selection.md`) — but `internal/config/help.go`
never mentioned it, so `gopi -h` gave no way to discover or test the feature.
Fix the exposure, and make it provable.

## Completed
- **`internal/config/help.go`** — the voice-input feature is now documented in
  the "Tools & group sub-commands" section, in all three real shapes, plus a
  Use-case example line:
  - `gopi voice_input <audio-file> [--chunk N]` — one-shot transcription, with
    the *why* of `--chunk` (measured: an unchunked long clip is fed as one
    block, the model emits degenerate output and takes 8× longer).
  - `gopi voice_input --server --listen :7788` — HTTP shape (`POST /transcribe`
    multipart field `audio` → JSON `{"text":…,"elapsed_ms":…}`, `GET /health`).
  - `gopi voice_input --server --stdio` — zero-new-port pipe shape for an ssh hop.
  - `--models DIR` / `--runtime PATH`, the engine (Fun-ASR-Nano, CER 1.58%), and
    the rationale *"CLI verb only — no agent tool"* (transcription happens before
    the model sees anything and the user reviews the text first).
  - `gopi voice_input talk.wav` usage example.
  Text mirrors the subcommand's own `-h` consts (`cmd/agent/cli_voice.go`) — one
  surface, so the two can be compared by eye.
- **The 8 siblings the same generalization exposed** — generalizing the voice
  guard into "every predicate wired into `main.go`'s dispatch chain must be
  discoverable" revealed that `gopi -h` was ALSO silent about `chat`, `brew`,
  `memsync`, `proxy`, `screen_capture`, `act` and `scratch-gc`. All are now
  documented from their own `-h` consts (a separate task `01a0959b` owns the
  broader *formatting* epic; this change adds content, it does not reflow).
- **`cmd/agent/usage_test.go`** — two guards:
  - `TestTopLevelUsageShowsVoiceInput` — the instance: the Epic's core strings
    (`gopi voice_input <audio-file>`, `--chunk N`, both server shapes, the
    example, the engine, the HTTP contract, "no agent tool").
  - `TestTopLevelHelpCoversEveryDispatchedSubcommand` — the class: it parses the
    predicate list out of `main.go`, maps each predicate to the `gopi <verb>`
    phrase its help entry must contain (a bare word in prose does not satisfy
    it), and FAILS on an unmapped predicate — so adding a dispatcher now forces
    a deliberate documentation decision instead of shipping an invisible verb.
    It also runs the reverse direction: every verb in `topLevelCommandNames`
    must be explained by `gopi -h`.
- **`cmd/agent/cli_completion.go` / `cli_completion_test.go`** — `voice_input`
  was missing from `topLevelCommandNames` (tab completion never offered it);
  added and the completion-set test updated.

## Not Done
- No reflow/reformat of `gopi -h` (task `01a0959b` "gopi help must be in good
  format" owns that; touching it here would collide).
- `HelpText` for the voice verb is not generated from a single source — help.go
  and `cmd/agent/cli_voice.go`'s `-h` consts are two texts kept in sync by the
  new test's substring contract, not by construction. A "render help from
  subcommand metadata" refactor is a larger epic (see `devdoc/cli-subcommand-groups.md`).
- Known follow-ups filed elsewhere: `voice_input --server` defaults to `:7788`
  (all interfaces, no auth) — task `01a095c7`.

## Verification
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Guards are load-bearing (red-proof, the point of a guard): reverting ONLY
  `internal/config/help.go` to HEAD and re-running `go test ./cmd/agent/`
  fails BOTH new guards —
  `TestTopLevelUsageShowsVoiceInput`: 6 missing strings;
  `TestTopLevelHelpCoversEveryDispatchedSubcommand`: 8 undiscoverable
  predicates (`chat`, `brew`, `memsync`, `proxy`, `screen_capture`, `act`,
  `scratch-gc`, `voice_input`) + completion-verb mismatches. Restored afterwards;
  the restored diff is the committed one.
- Real binary: `./bin/gopi -h` prints the voice entries (lines 436–453 of the
  subcommand section, 519–523 of the use-case block); `./bin/gopi voice_input -h`
  prints the full design text (three shapes, flags, the `--chunk` measurement).
- Live end-to-end (the feature actually runs, not just documented): a
  self-generated 4 s / 16 kHz mono wav (`/tmp/voice-probe.wav`) fed to
  `gopi voice_input /tmp/voice-probe.wav` → the real engine ran and returned
  (`[voice] voice-probe.wav in 54.7s (chunk=15s)`, transcript `/sil` — correct
  for a pure 440 Hz tone: silence/no speech). Model + runtime present at
  `~/work/funasr-models` and `~/work/runtime/.../llama-funasr-cli`.
- Full gate: `go build` → OK; `gofmt -l` → empty; `go vet` → clean;
  `go test ./internal/standards/ ./internal/config/ ./cmd/agent/` → all `ok`
  (cmd/agent 337.6s); `go test ./...` → every package ok EXCEPT a 600s timeout
  in `internal/tools` (`TestTaskStampRun` self-flaked: it recursed into a
  full-suite run of its own). That package is untouched by this branch
  (`git diff --stat 9bfb390e HEAD -- internal/tools` is empty) and the same
  test passes on the same machine against both master (`/tmp/gopi-master-check`,
  `ok 59.3s`) and this worktree in isolation — i.e. a pre-existing
  environment/parallelism flake, not caused by this change.

## Postscript — superseded by task `01a0959b` (help-format epic)

The verbose top-level blocks this report added (and their "use-case" duplicates)
were **replaced** when `feat/help-format` merged: that epic's whole point is that
`gopi -h` is an INDEX and each verb owns its detail behind `gopi <verb> -h`. The
voice_input entry is now one index line —

```
  gopi voice_input <audio>      Speech to text (Fun-ASR-Nano); --server serves HTTP.
                                `gopi voice_input -h` has the usage shapes and the engine contract.
```

— and the full text (three shapes, `--chunk` measurement, `POST /transcribe`,
"no agent tool") lives in `voiceInputUsage` (`cmd/agent/cli_voice.go`), which is
where this report's content already existed. The two guards survive: `gopi -h`
must still carry the verb + engine + the pointer, and `voiceInputUsage` must
carry the detail. Nothing about the voice feature's discoverability is lost —
only its duplication in the top-level page. See `devdoc/help-format.md` and
`tasks/01a0959b-cf65-report.md`.

## Notes for future agents
- The generalization is the reusable part: `dispatchPredicatesInMain(t)` in
  `cmd/agent/cli_contracts_test.go` parses the dispatch chain, so any
  "every surface must also do X" contract can be written against it (this is
  the same helper the CLI-completion guards use).
- `voice_input` is a CLI verb only by design — do NOT add it as an agent tool;
  the help text states the reason, and Rule 3 governs the tool list.
