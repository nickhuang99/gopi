<!-- doc-class: normative -->
# voice_input — the local engine's deployment: the gopi profile is rung 1, `$HOME/work` is the legacy rung

Task: `01a1169f-4911-71eb-9941-869336a706b5` · branch `task/01a1169f491171eb9941869336a706b5`

## The defect

`gopi tool voice_input`'s local engine (Fun-ASR-Nano, the bare command's default)
resolved its weights and its runtime to `$HOME/work/funasr-models` and
`$HOME/work/runtime/llama.cpp/build/bin/llama-funasr-cli` — FIRST, and, on a host with
nothing in `$HOME/work`, the only answer that existed. Two properties made that wrong:

1. **The default named a path gopi does not own.** The 914 MB of weights and the ~6 MB
   `llama-funasr-cli` C++ runtime are a deployment gopi neither builds nor ships
   (Rule 35) — it exists because it once happened to be installed in one operator's
   `$HOME/work` (measured: absent in all three pieces on a client host). Meanwhile the
   ONE directory in this story gopi actually maintains is its own profile
   (`config.GopiDir` → `$GOPI_HOME/.gopi`, else `~/.gopi`).
2. **A missing deployment produced a dead end.** The failure was
   `runtime not found: <path>`: a fact, with no statement of the two files, the binary
   or the directory that would fix it, and no mention that `--engine doubao` needs
   none of them.

## The change

**Rung 1 (the default) is the gopi profile: `<gopi dir>/voice`**, with the runtime at
`<gopi dir>/voice/llama-funasr-cli`. **Rung 2 is `$HOME/work`** — still read (a host
mid-migration keeps working), never preferred, and *announced* when it answers.

- One walk, `voiceResolveDeployment` (`cmd/agent/cli_voice_local.go`); the parser's
  default (`voiceDefaultDeploymentPaths`) fills in the same rung 1, so a caller that
  never walks points where a real run would (Rule 2 — one resolution).
- **The migration note.** A path supplied by rung 2 prints rung 2's path, rung 1's
  path and the runtime FILE NAME to move with the weights, on stderr, once per run.
  Silence was the defect: a compatibility rung nobody notices is a deployment nobody
  migrates.
- **The rungs resolve independently** (weights in the profile, binary still in
  `$HOME/work`, or the reverse) — forcing one root on both would refuse a working
  installation.
- **`--models` / `--runtime` are outside the search** (`modelsExplicit` /
  `runtimeExplicit`): named is used exactly as given, no fallback. Re-pointing a path
  the operator stated makes a run read other weights than the ones it reports.
- **Provenance is printed**: every local run says which rung (or that an explicit flag
  answered) supplied its paths (`voiceDeploymentSource`) — engine, model directory,
  runtime path AND the rung label, in one sentence
  (`voiceEngineProvenanceLabel` + `voiceReportEngineProvenance`), printed by the bare
  capture AND by the file run (the first cut of this change printed it only on the bare
  capture, and the file shape — the shape the acceptance runs actually used — stayed
  silent; the round-2 review caught it by running the shape, see Run E). `gopi chat`'s
  `/voice` mode prints the same sentence through `chatVoiceEngineLabel`, which is now an
  alias of that one constant (Rule 1b/14) rather than a second spelling of it. The line
  is **silent on the cloud engine**: `--engine doubao` reads neither weights nor runtime.
- **No deployment anywhere = an install instruction**, not a dead end: the precise
  missing file first (the check's own vocabulary — runtime → encoder → llm, first
  missing wins, unchanged), then both GGUF names and the directory, the runtime's name
  and path, the legacy rung's paths, and `gopi tool voice_input --engine doubao`. The
  long message is deliberate: an install step stated is one round trip; a path stated
  is none.

## What was NOT done (honest)

- **The local engine is still the default engine.** The epic's part B ("no silent
  local default — only an explicit choice goes local") is landed in the form the
  budget and the previous task's design allow: the default is no longer *silent* (the
  engine and its deployment rung are printed on every transcribing run, and every
  missing deployment produces an install instruction naming the zero-dependency
  alternative). Inverting the resolution — an unset `voice_engine` refusing to run —
  would break `gopi tool voice_input f.wav`, whose unchanged output is a standing
  contract of this verb (`cli_voice_engine.go`: "an upgrade must not change existing
  behaviour"), and would make the bare capture unusable on any host that has the
  weights. Only the operator can decide that trade; it is not taken here.
- **No fetch, no build, no new dependency** (Rule 35): the guidance tells the operator
  where to put files; gopi does not download them, and nothing was added that
  outlives the process.
- **No CGO / embedded engine**: unchanged (would break `CGO_ENABLED=0` and Rule 12).

## Verification (Rule 31 — real runs, verbatim)

Binary: `CGO_ENABLED=0 go build -o /tmp/gopi-voice-rungs ./cmd/agent/` (BUILD_OK).

**Run A — only rung 2 exists** (`$HOME=/home/user`, weights really in `~/work`):

```
$ /tmp/gopi-voice-rungs tool voice_input /tmp/nonexistent.wav
[voice] the local engine came from the legacy deployment /home/user/work/funasr-models
  (runtime /home/user/work/runtime/llama.cpp/build/bin/llama-funasr-cli) — gopi's own
  location for it is the gopi profile: put the weights and llama-funasr-cli in
  /home/user/.gopi/voice (runtime /home/user/.gopi/voice/llama-funasr-cli) and this
  note goes away
gopi tool voice_input: engine failed: exit status 1 — audio: failed to open/decode
  /tmp/nonexistent.wav (supported: wav/mp3/flac) | failed to read audio
```

(exit 1 — the engine's own refusal of a missing file, reached through the legacy rung.)

**Run B — no deployment anywhere** (`gopi tool sandbox create`, empty `HOME`):

```
$ HOME=$SB GOPI_HOME=$SB /tmp/gopi-voice-rungs tool voice_input /tmp/x.wav
gopi tool voice_input: runtime not found: $SB/.gopi/voice/llama-funasr-cli
  the local engine (local) reads 2 weight files and a runtime binary that gopi neither
  builds nor downloads (Rule 35 …)
  1. weights → both files in the model directory:
       $SB/.gopi/voice/funasr-encoder-f16.gguf   funasr-encoder-f16.gguf
       $SB/.gopi/voice/qwen3-0.6b-q4km.gguf   qwen3-0.6b-q4km.gguf
     (this is rung 1 — the gopi profile: config.GopiDir + "voice")
  2. runtime → the llama-funasr-cli binary at:
       $SB/.gopi/voice/llama-funasr-cli
  3. no local deployment at all? the cloud engine needs neither, and is used by naming it:
       gopi tool voice_input --engine doubao
  gopi tool voice_input --models DIR / --runtime PATH override BOTH rungs …
```

exit 1, instruction printed, nothing loaded.

**Run C — rung 1 answers, `$HOME` deliberately empty** (profile holds the deployment,
`HOME` set to an empty directory so rung 2 cannot exist):

```
$ HOME=$SB/empty-home GOPI_HOME=$SB /tmp/gopi-voice-rungs tool voice_input /tmp/voice-rung-probe.wav --chunk 12
2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。大语言模型早已不是工具。
[voice] voice-rung-probe.wav in 33.0s (chunk=12s)
```

exit 0 — a real Fun-ASR-Nano transcript with the weights supplied by the PROFILE
(symlinks in `$SB/.gopi/voice/`). No migration note: rung 1 answered.
(Note the engine is `--enc`/`-m` over symlinked paths; the audio was cut from
`~/work/whisper.cpp/2084无颜见人0.mp3` with ffmpeg to 12 s of 16 kHz mono PCM16.)

**Run D — only rung 2 exists, and it is a real transcription** (legacy symlinked into
a sandbox's `work/`, profile empty):

```
$ HOME=$SB2 GOPI_HOME=$SB2 /tmp/gopi-voice-rungs tool voice_input /tmp/voice-rung-probe.wav --chunk 12
[voice] the local engine came from the legacy deployment $SB2/work/funasr-models
  (runtime $SB2/work/runtime/llama.cpp/build/bin/llama-funasr-cli) — gopi's own location
  for it is the gopi profile: put the weights and llama-funasr-cli in $SB2/.gopi/voice
  (runtime $SB2/.gopi/voice/llama-funasr-cli) and this note goes away
2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。
[voice] voice-rung-probe.wav in 12.1s (chunk=12s)
```

exit 0 — legacy rung still works, and the migration note is what the operator sees.

**Run E — the FILE shape, both rungs** (round 2: this is the shape the earlier runs used
for transcription while the provenance line was being claimed for "every local run" —
so it was run explicitly, and it is the observation that FOUND the gap):

```
$ HOME=$SB GOPI_HOME=$SB /tmp/gopi-voice-rungs2 tool voice_input /tmp/voice-rung-probe.wav   # profile rung
[voice] engine local: models $SB/.gopi/voice, runtime $SB/.gopi/voice/llama-funasr-cli (rung 1: the gopi profile)
[voice] voice-rung-probe.wav in 75.0s (chunk=15s)
2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。大语言模型早已不是工具。

$ HOME=$SB2 GOPI_HOME=$SB2 /tmp/gopi-voice-rungs2 tool voice_input /tmp/voice-rung-probe.wav  # legacy rung only
[voice] the local engine came from the legacy deployment $SB2/work/funasr-models (runtime
  $SB2/work/runtime/llama.cpp/build/bin/llama-funasr-cli) — gopi's own location for it is the
  gopi profile: put the weights and llama-funasr-cli in $SB2/.gopi/voice (runtime
  $SB2/.gopi/voice/llama-funasr-cli) and this note goes away
[voice] engine local: models $SB2/work/funasr-models, runtime $SB2/work/runtime/llama.cpp/build/bin/llama-funasr-cli (rung 2: the legacy $HOME/work deployment)
[voice] voice-rung-probe.wav in 56.8s (chunk=15s)
```

Both exit 0 with a real transcript; the second prints the migration note AND the
provenance line, which is the pair an operator mid-migration should see.

**Red proof** (the guard, measured): patching `voiceResolveDeployment` back to the OLD
order (rung 2 preferred, and its presence short-circuiting rung 1) went red in three
tests with the exact wrong path, then the file was restored from a copy:

```
--- FAIL: TestVoiceDeploymentPrefersTheProfileRung
    models = ".../002/work/funasr-models", want rung 1 (the profile) ".../001/.gopi/voice"
--- FAIL: TestVoiceDeploymentLegacyRungAnswersWithAMigrationNote
--- FAIL: TestVoiceDeploymentRungsResolveIndependently
```

**Gates**: `go build ./...` OK · `go test ./cmd/agent/ -run TestVoice -count=1` ok ·
`go test ./internal/standards/ -run TestConfigDoc -count=1` ok (CONFIG.md line refs
resolve) · `make test-budget PKGS='./cmd/agent ./internal/config'` — see the task report.

## Files

- `cmd/agent/cli_voice.go` — `voiceLegacyModelRoot` / `voiceLegacyRuntime` (the old
  spelling `voiceDefault*` renamed: it no longer names the default), `modelsExplicit`
  `runtimeExplicit`, `voiceDefaultDeploymentPaths`, the install guidance
  (`voiceLocalDeploymentMissing` + its two note helpers), usage text.
- `cmd/agent/cli_voice_local.go` — `voiceResolveDeployment` (+ `voiceProfileVoicePaths`,
  `voiceLegacyModelPaths`, `voiceModelsComplete`, `voiceFilePresent`,
  `voiceDeploymentSource`, the migration notes).
- `cmd/agent/chat_voice.go` — the `gopi chat` voice mode calls the same walk.
- `cmd/agent/cli_voice_local_test.go` — the four deployment-search tests + two
  guidance tests; `cli_voice_parse_test.go` / `cli_voice_format_test.go` updated.
- `CONFIG.md` — the rung table and the `voice_engine` row.

## Gotcha worth keeping

`os.Stat` on a directory **succeeds**. The first draft tested the rungs with
`os.Stat(path) == nil`, so an empty `<profile>/voice/` DIRECTORY looked like an
installed runtime — Run B printed `runtime not found: ` with an EMPTY path and no
instructions. `voiceFilePresent` (exists AND not a directory) is the fix, and the
install guidance now names rung 1 rather than an empty string when the profile itself
does not resolve.
