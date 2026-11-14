<!-- doc-class: historical -->
# Task Report: `gopi tool voice_input` — the bare invocation captures the microphone and transcribes it locally

Task: `01a1137aeabf777386da0c6a523b480f` · branch `task/01a1137a…` · host: peerbox

## Completed

**1. The default shape exists.** `gopi tool voice_input` with **no argument and no
`--client`** now records from this machine's microphone and transcribes it **here, with
the local engine**, printing the transcript on stdout and its device/tier/engine
reporting on stderr. One process is both the capture client and the transcriber, which
is what a machine holding the microphone *and* the weights wants. Implemented in the
new `cmd/agent/cli_voice_local.go` (`runVoiceCaptureLocal`), wired by a `capture` field
on `voiceOptions` that `parseVoiceArgs` sets from what the invocation *names* — no
file, no `--server`, no `--client`, no `--list-devices` — rather than from a flag.

**Nothing about capture was re-implemented.** The device policy (four tiers, each
printed), the recorder argv, the dead-device reporting, the liveness probe and the
"reachable but silent" verdict are the `--client` ones. `--server`/`--client`/
`--server --stdio`/`--listen :PORT` keep their behaviour; the client/server split is
unreached by the new default.

**2. The weights resolve through the gopi PROFILE, not only `$HOME`.** Two rungs,
first-that-holds (`voiceResolveProfilePaths`) — rung 1 `$HOME/work/funasr-models` +
`$HOME/work/runtime/…/llama-funasr-cli` (checked first, so an existing installation is
untouched), rung 2 `<config.GopiDir>/voice` (`$GOPI_HOME/.gopi/voice`, else
`~/.gopi/voice`) with the runtime at `<that dir>/llama-funasr-cli`. Resolved once in
`runVoiceSubcommand`, so the server and file shapes read the same paths. The run prints
which engine and which model directory it used, so a profile-resolved default is never
invisible.

**3. Bounded by a named default.** `voiceLocalDefaultSeconds = 10` (the remote client
keeps its own `voiceClientDefaultSeconds = 5`), rendered into `-h` through
`voiceSecondsPlaceholder` so the number the operator reads and the one the parser
applies come from one constant.

**4. `--listen` implies `--server`.** An address to listen on has no other meaning; the
old behaviour (the file branch took the value and ignored it) was the one way to be
surprised by this verb.

**5. Parse-time refusals — none of them spends a recording.** `--server-url` on a
`--server`/`--list-devices` run; `--device`/`--seconds`/`--record`/`--chunk` on a file
run; `--seconds` while listing devices; a positional on a `--server`/`--list-devices`
run; `--chunk` with the bare capture (it windows a *file*). Each message names the flag
and the shape it was given on. The sharpest one is `--server-url`: left alone it would
have let a LOCAL engine produce the transcript while the operator believed the host
they named had done it.

**6. `--record FILE`** (new) keeps a capture's audio as a 16 kHz mono PCM16 WAV — same
bytes the engine was handed — on the bare shape *and* on `--client`. It is the sibling
of `--out` with the two differences that made a second spelling necessary: it is not
client-only, and an existing file is replaced **on purpose** (re-recording one fixed
utterance name is what it is for) with the byte count announced. `--out`'s
refuse-an-existing-file rule is unchanged.

**7. Registration and docs, in the same commit.** `voiceFlagRecord` in the flag
constants + `voiceFlagMember` + the usage text; the `gopi -h` index row and the
`gopi --help-env` catalog description; `CONFIG.md`'s Voice-input section (the two-rung
table + corrected line references); the devdoc contract row for this report.
CONFIG.md's `:143/:144/:147` references were **already stale** (they point into a
comment block) — re-pointed to the catalog rows' real lines, and the config-doc gate
is green.

**8. Three tests that were RED at HEAD are repaired** (see *Not Done* for the honest
scope of this): `voiceStubArgs` spawned the leaf word `voice_input`, which the
verb-retirement epic has since retired (`gopi voice_input` → "is RETIRED"), so
`TestVoiceHTTPServerEndToEnd` and the two contract-mirror tests failed on a door that
no longer exists. The stub now builds its argv from `toolHome(toolnames.ToolVoiceInput)`
(one rendering, no second `"tool "` literal) and the mirror tests assert the *two live
hops* (`gopi tool` → `toolSubcommandPredicates[voice_input]` → `isVoiceSubcommand`),
comparing the predicate by function pointer rather than by name.

## Not Done (honest)

- **`gopi tool voice_input` (bare) was exercised out of a profile-only tree on this
  host (run F below) — but with the weights arriving there by `ln -s`.** The run proves
  the RESOLUTION (rung 1 absent, rung 2 chosen and named `[voice] engine local: models
  <profile>/.gopi/voice`), which is what the branch decides; it does not prove a
  real deployment's file layout, because this host has no such deployment and the
  fixture pointed at the real weights by symlink. The `--seconds 46.1s` transcription
  time in that run is the price of the encoder being read through symlinks on a
  loaded host (load average 9.6) — not a property of the path.
- **`--record` was exercised through the client shape, not the bare shape.** The
  byte-identical file it writes on `--client` was compared against the WAV the HTTP
  server received (existing test path); on the bare shape the write path is the same
  `voiceStoreCapture` call, reached one branch earlier — not separately driven
  end-to-end.
- **The debounce-less `--record` overwrite is deliberate and unguarded by design** (see
  the flag's own comment): an operator who points it at a file they wanted to keep will
  lose it, and the run's stderr line is the only notice.
- **Pre-existing reds remain — a wider set than the two named in the first draft of this
  report, all outside this change.** Measured in this session on the branch tip
  (`make test-budget PKGS='./cmd/agent ./internal/config ./internal/standards'`, 15
  failures; four of them are the ones re-run and characterised here):
  `TestCommandsDocGeneratedBlockMatchesSource` / `TestCommandsDocAnchorsResolve` /
  `TestCommandsDocGateRedProofStaleGeneratedBlock` — `COMMANDS.md` line 10 belongs to
  `gopi checkpoint` and carries a line reference the tree has moved past, and 1 anchor
  points at a BLANK line (`cmd/agent/cli_session.go:209`); the three inputs of that gate
  (`COMMANDS.md`, `cmd/agent/cli_session.go`, `cmd/agent/cli_checkpoint.go`) and the
  gate's own test+census are **byte-identical to master** in this branch
  (`git rev-parse master:<f>` == `git rev-parse HEAD:<f>`, SAME for all five), so the
  commit that must regenerate the block is master's, not this branch's (Rule 26 —
  dispatch, do not patch here). `TestDocManifestCoversEveryDevdocFile`,
  `TestChatBoundaryTenMegabyteInputStaysBounded`, `TestChatStatus_LiveBlockKeepsTheStateWord`,
  `TestBashScriptStructure`, `TestSandboxDirFlagDecidesTheRoot` (`gopi tool sandbox create`
  → `unknown command "create"`), `TestKeywordSearch*`, `TestToolGroupForwardsToTheSameImplementation`,
  `TestAnnotateVerbRunsARealArgvEndToEnd`, `TestTaskVerbVerifyRunsAsAdvertised` — re-run
  **individually on this branch** and each fails on a surface this diff does not touch
  (verb retirements and verb-group routing). A sibling row is already live for this
  family on the ledger: `#01a1137f-90d0-7f10-a4d4-1f9227190ceb`
  `bug(standards): TestScriptsAreBoundedAndOffTmp …` (`in_progress`).
  What IS this branch's: `TestVoice*` — green (28.6 s package run), together with the
  three that were RED at HEAD and are repaired here.

  **Additionally measured after that package run** (a fuller picture than the first
  draft, and one more master-side red):
  `TestUndispatchedToolWordsNameADispatchingCanonical` (`cmd/agent`, re-run alone, FAIL)
  — the table's first row has `word = code_search` and
  `canonical = flatToolAddress(ToolCodeSearch)` = `code-search`, and the guard compares
  `dashTwin(canonical) == dashTwin(word)` → equal → it reports all eight rows as
  "refuses itself" (`code_search`, `keyword-search`, `vision-analyze`,
  `screen-capture`, `ui-state`, `voice-input`, `web-search`, `web-fetch`). The guard's
  own comment says the ADDRESS is what must resolve (`refusalAddressDispatches`,
  `homeProg`), so the dash-twin comparison looks like the invariant that was added after
  the table was re-pointed at full command lines — a decision for that row's owner, not
  for this branch. **The entire `cmd/agent` tree difference between this branch and
  master is the eight voice files** (`git diff --name-only master...HEAD`); every file
  the other failures exercise — `cli_tool.go`, `cli_keyword_search.go`, `cli_sandbox.go`,
  `cli_annotate.go`, `chat_boundary.go`, `cli_completion.go`, and the table plus its test —
  is byte-identical to master (`git rev-parse master:<f>` == `git rev-parse HEAD:<f>`,
  SAME for each). So `cmd/agent` carries these failures on master, and the 11 failing
  test FUNCTIONS listed in the suite's failure block are all in files identical to
  master's. Dispatched as its own row (Rule 26) rather than patched here.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (and `go vet ./cmd/agent/` clean)
- Tests: `CGO_ENABLED=0 go test ./cmd/agent/ -run 'Voice|TopLevelUsage'` → **ok**
  (this includes the repaired `TestVoiceHTTPServerEndToEnd`,
  `TestVoiceInputIsRegisteredInContractMirrors`, `TestVoiceInputIsAContractToolMirror`
  and the new `cli_voice_local_test.go`)
  `CGO_ENABLED=0 go test ./internal/config/` → **ok**
  `CGO_ENABLED=0 go test ./internal/standards/ -run TestConfigDoc` → **ok**
  `make test-budget PKGS='./cmd/agent ./internal/config ./internal/standards'` → see
  the run log under the session directory (whole-package, branch-scoped).
- Ran (the changed surface, for real, against real state — Rule 31):

  **A. The acceptance run — bare invocation, deterministic microphone, local engine.**
  The "microphone" is a PulseAudio null-sink monitor (no human, no hardware dependency),
  fed by the pinned reference recording, with the ALSA default source restored and the
  module unloaded afterwards:

  ```bash
  MOD=$(pactl load-module module-null-sink sink_name=gopi-mic-rig)
  pactl set-default-source gopi-mic-rig.monitor
  ( sleep 1.5; paplay --device=gopi-mic-rig $SD/artifacts/mic-clip10.wav ) &
  ./bin/gopi tool voice_input --seconds 12
  ```

  **Result** — exit 0; stdout (the transcript, and nothing else):

  ```
  2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。
  ```

  stderr (the device, the tier that chose it, the engine and the weights):

  ```
  [voice] engine local: models /home/user/work/funasr-models, runtime /home/user/work/runtime/llama.cpp/build/bin/llama-funasr-cli
  [voice] recording 12s from pulse:@DEFAULT_SOURCE@ (PulseAudio default source (gopi-mic-rig.monitor)) [first enumerated]
  [voice] captured 384000 bytes (12.0s of 16000Hz mono PCM16)
  [voice] transcribed locally in 10.9s (chunk=15s)
  ```

  Scored with the repo's own scorer, `scripts/asr_score.py`, against the reference the
  fixture script pins (sha256 `aea95730…`): the utterance covers the first 10s of a
  220s recording, so the reference is cut proportionally (a conservative floor) —
  **CER 2.63 %** over the 38 characters the speaker actually covered (one homophone,
  `续→序`). Without the 10s ceiling estimate the same comparison reads 21.28 %, which
  measures the window, not the engine.

  **B. File shape unchanged**: `./bin/gopi tool voice_input /usr/share/sounds/alsa/Front_Center.wav`
  → `Front, centre.` exit 0.

  **C. Every refusal, on the real binary** (exit 1, no recording spent):
  `--server --server-url http://x:1`, `--list-devices --server-url http://x:1`,
  `clip.wav --device default`, `clip.wav --seconds 5`, `--server clip.wav`,
  `--list-devices --seconds 5`, `--chunk 15` — each printing the named message.

  **D. `--listen` now implies `--server`**: `./bin/gopi tool voice_input --listen 127.0.0.1:7799`
  → the server's banner, and `GET /health` answered **HTTP 200** while it ran; the port
  stopped answering when the process exited.

  **E. `gopi tool voice_input -h`** renders `10s` from `voiceLocalDefaultSeconds` — the
  literal `{seconds}` appears 0 times (a placeholder the replacer had not carried, which
  the new `TestVoiceUsageRendersTheCaptureBound` now pins).

  **F. The acceptance run RE-DRIVEN in this session, and rung 2 of the weights search
  driven end to end for real.** Both on the branch tip, same binary, same null-sink rig:

  ```bash
  MOD=$(pactl load-module module-null-sink sink_name=gopi-mic-rig2)
  pactl set-default-source gopi-mic-rig2.monitor
  ( sleep 1.5; paplay --device=gopi-mic-rig2 $SD/mic-clip10.wav ) &
  ./bin/gopi tool voice_input --seconds 12           # rung 1 ($HOME/work) answers
  ```

  → exit 0, stdout `无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。`
  (byte-identical to run A's transcript), stderr naming `models /home/user/work/funasr-models`
  and `transcribed locally in 4.8s`. The ALSA default source was restored and the module
  unloaded afterwards (`pactl list short sinks` → only the hardware sink).

  Rung 2 — a home whose `work/` tree does NOT exist, with the weights reachable only
  through `GOPI_HOME/.gopi/voice`:

  ```bash
  env HOME=<rung2>/home GOPI_HOME=<rung2>/profile ./bin/gopi tool voice_input --seconds 12
  ```

  (weights + runtime materialised under `<rung2>/profile/.gopi/voice/` by `ln -s` at the
  real ones — this host has no profile-only deployment, see *Not Done*.) **First attempt
  was RED, and that is the most useful half**: the symlinks had one `..` too few, so they
  resolved to nothing and the run refused with exit 1 —

  ```
  [voice] no model directory under <rung2>/home/work/funasr-models or <rung2>/profile/.gopi/voice,
          and no runtime at <rung2>/home/work/runtime/... or <rung2>/profile/.gopi/voice/llama-funasr-cli
  gopi tool voice_input: runtime not found: <rung2>/home/work/runtime/llama.cpp/build/bin/llama-funasr-cli
  ```

  — which is exactly the intended loud behaviour: BOTH roots named, then the concrete
  missing path. With the symlinks repaired the same command gives exit 0:

  ```
  stdout: 2084无言见人序章裸模型时代，2084年人类终于驯服了语言，却也沦为语言的囚徒。

  [voice] engine local: models /home/user/workspace/gopi-voicerung2/profile/.gopi/voice, runtime /home/user/workspace/gopi-voicerung2/profile/.gopi/voice/llama-funasr-cli
  [voice] recording 12s from pulse:@DEFAULT_SOURCE@ (PulseAudio default source (gopi-mic-rig4.monitor)) [first enumerated]
  [voice] captured 384000 bytes (12.0s of 16000Hz mono PCM16)
  [voice] transcribed locally in 46.1s (chunk=15s)
  ```

  Both stderr lines are the point: rung 1 was absent and rung **2** supplied BOTH the
  weights and the runtime. Scored with `scripts/asr_score.py` against the same pinned
  reference: **CER 21.28 %** — the identical figure run A reports for the same transcript,
  i.e. rung 2 transcribes the same audio with the same accuracy (the figure measures the
  10 s window against the 220 s reference, not the path).

- Commit: the branch tip of `task/01a1137a` — `git log -1 --format=%H` on that branch (`457a999b6` at the time of writing; amending this report moves the hash by one, so the TIP, not this number, is the reference). Subject: `feat(voice): bare `gopi tool voice_input` records the mic and transcribes it locally`.

## Evidence (self-filed)

- Pre-existing-at-HEAD proof for the three repaired tests: `git checkout -- cmd/agent/cli_voice.go` + `rm` the new file (worktree otherwise clean) → `go test ./cmd/agent/ -run 'TestVoiceInputIsRegisteredInContractMirrors|TestVoiceInputIsAContractToolMirror|TestVoiceHTTPServerEndToEnd'` → **3 FAIL**, the first two on a missing token and the third on the stub's stderr `gopi: voice_input is RETIRED — use gopi tool voice_input`. Restored and re-driven to green.
- CONFIG.md staleness: `sed -n '143p' internal/config/settings.go` → a comment line ("json.Unmarshal (no DisallowUnknownFields)…"), i.e. the committed `:143`/`:144`/`:147` refs resolved into prose; the catalog rows are at `:210`–`:214`.
- Top-level page budget: `./bin/gopi -h | wc -l` → 175 lines, under `topLevelUsageMaxLines` (176) — the first draft of my help.go row made it 178 and the gate said so.
- Unit-test evidence for rung 2 of the model search: `TestVoiceResolveProfilePaths` writes weights + runtime under `<GOPI_HOME>/.gopi/voice`, points the defaults at absent trees, and asserts `voiceDepsReady` is then satisfied; `TestVoiceResolveProfilePathsKeepsAWorkingDefault` asserts a complete `$HOME/work` tree is **not** re-pointed.
