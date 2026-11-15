<!-- doc-class: normative -->
# The audio configuration surface: `audio_engine` + `audio_engine_param`

**Task**: 01a120bd-53bb-7e44-a6e5-4e3a05f5f5a5 (revision of the cancelled row
01a120b6). **Date**: 2026-10-09.

This document carries the three decisions the brief requires: the naming ruling
(and its explicit disagreement with the operator's one-liner), the per-engine
block tradeoff and why the rejected design lost, and the migration policy with
its cost.

---

## 1. `audio_engine`, not `voice_engine` — a ruling recorded as CONTRARY to the operator's one-liner

### The operator's sentence

The task's own closing line says, verbatim:

> 把 doubao_* 改成 audio_*，顶层键选用 `voice_engine`

That is: keep the top-level engine selector spelled `voice_engine`, and rename
the vendor-specific parameter keys to `audio_*`.

### The ruling: the top-level key is `audio_engine`, and this DEPARTS from that sentence

**This section is the explicit mark the brief asks for: the ruling below is
CONTRARY to the operator's sentence.** It is not a restatement of it and it is
not a reading that makes the sentence say this — it is a disagreement, recorded
where a later reader will find it without having to reconstruct it from a diff.

The three reasons, in the order they decided it:

1. **A `voice_engine` selector above an `audio_*` parameter surface is one
   vocabulary with two nouns.** The rename's whole purpose is that the keys name
   the DOMAIN (audio) rather than one implementation of it (the voice
   subcommand). Leaving the selector behind keeps exactly the split the rename
   was for: an operator reads `voice_engine` and `audio_engine_param` in one
   document and has to work out whether they belong to the same mechanism. The
   surface is the audio→text engine; the selector is the head of that surface.
2. **The knobs under the new selector are not all voice-shaped.** A recording
   engine takes an audio FILE and returns text — no microphone, no voice, no
   capture anywhere in the path. `gopi tool voice_input <file.wav>` is the same
   engine reached from a file the operator already had. Naming the selector
   after the subcommand would tie the configuration to the surface that happens
   to call it today, which is the coupling the vendor-neutral rename exists to
   break.
3. **The two spellings would diverge on the first non-voice caller.** The
   engine is a function of an audio path (that is `internal/doubaoasr` and
   `internal/doubaostream`'s own contract). Any second caller — a task
   attachment transcribed on ingest, say — would have to read a key called
   `voice_engine` to pick an engine it does not reach through a voice surface.
   The name would then be wrong in the document, not only in conversation.

### What the departure COSTS, stated honestly

* The operator's sentence is not followed, so an operator looking for
  `voice_engine` in a new settings document finds nothing by that name. The
  mitigation is the migration policy (§3): the old key is **still read** and the
  run says so out loud, so the cost is a notice rather than a breakage.
* This is a second rename on a surface that was already renamed once
  (`voice_engine` ← the pre-2026-08 spelling). Each rename spends operator
  attention; the doc records this one so the next rename is a decision rather
  than a habit.
* `--engine` (the flag) is deliberately NOT renamed: the flag belongs to
  `gopi tool voice_input`, which is still spelled that way, and renaming a
  subcommand's flag without renaming the subcommand would be a third vocabulary.

---

## 2. One param block per engine, not provider + mode

### The design that was chosen

```json
"audio_engine": "recording",
"audio_engine_param": {
  "recording": { "api_key": "...", "key_file": "...", "api_base_url": "...",
                 "resource_id": "...", "hotwords": "..." },
  "streaming": { "app_id": "...", "token": "...", "key_file": "...",
                 "endpoint": "wss://...", "cluster": "..." },
  "local":     { "models_dir": "/path/to/weights", "runtime_bin": "/path/to/bin" }
}
```

Three separate Go types (`AudioRecordingParams` / `AudioStreamingParams` /
`AudioLocalParams`), one field each on `AudioEngineParam`.

### The rejected design: `provider` + `mode` at the top level

The obvious alternative is orthogonal decomposition — a vendor axis and a
transport axis:

```json
"audio_provider": "volcengine",
"audio_mode": "batch" | "stream",
"audio_param": { "api_key": "...", "app_id": "...", "endpoint": "..." }
```

It is the more elegant schema on paper: two independent dimensions, and a third
vendor adds a provider without a new block. It loses on the MEASUREMENT.

### Why it lost: the two cloud engines' parameter surfaces do not unify

The brief names the differences; the repository's own source is where they were
measured, and they are four independent ones, not one:

| dimension | `recording` (batch) | `streaming` (v2 real-time) | source |
|---|---|---|---|
| credential | ONE key | an APPID **pair** with a TOKEN — neither usable alone | `internal/doubaoasr/doubaoasr.go:87` (`X-Api-Resource-Id`), `internal/doubaostream/client.go:147` (`Options.AppID`/`Token`) |
| transport | **two** HTTP URLs, submit then poll | **one** `wss://` socket, no polling | `doubaoasr.SubmitURLFor`/`QueryURLFor`; `doubaostream.DefaultEndpoint` |
| payload | JSON, audio inlined base64 | a custom BINARY frame (`0x11` full request, `0x2`/`0x1` audio) | `internal/doubaostream/client.go:41-140` |
| authorisation | an `X-Api-Resource-Id` header | `Authorization: Bearer;<token>` (semicolon) plus the pair repeated in the frame body | `doubaoasr` headers; `doubaostream` dial |

The `provider` + `mode` schema has to answer one question the measurement makes
unanswerable: **under `volcengine`/`batch`, is `token` a legal key?** The honest
answer is "no — it belongs to the other mode", and a schema whose fields are
each legal only under some combination of two independent axes cannot say that
in the type. The three forms available to it are all worse than a block:

* **One flat param struct with the union of both surfaces.** Then
  `api_key` and `app_id` sit side by side in a document where one of them is
  meaningless; nothing rejects `{"provider":"volcengine","mode":"batch",
  "token":"..."}`, and the engine that received it would either ignore the token
  silently (Rule 15) or refuse a document the schema called legal.
* **Per-(provider, mode) validation in code.** The knowledge then lives in a
  function, not in the shape, and the settings document — which is what the
  operator reads — does not show it. The brief's own ruling asks for a
  STRUCTURED shape, and a structure whose legality is enforced elsewhere is a
  flat map with a comment.
* **Prefixing the union** (`batch_api_key` / `stream_api_key`). That is the
  flat `doubao_*` surface again with the vendor's name filed off, which is the
  defect the rename was dispatched to fix.

### What the chosen shape gives up

* A third VENDOR answering the *same* shape (`recording`) needs a block-internal
  discriminator — today's answer is `api_base_url` + `resource_id`, which is
  how a second deployment of the same protocol is reached. A genuinely
  different vendor behind `recording` will need a second block name, and that is
  a deliberate trade: the shape that is wrong is a shape that hides
  incompatibility, not one that requires a new name.
* The engine is the outer key, so a knob shared by two engines (`key_file`) is
  written twice when both are configured. Accepted: a shared knob under an
  engine key is honest about the fact that each engine reads a different FORMAT
  out of that one file.

### The engine VALUES are functional, not vendor names — same reasoning

`recording` / `streaming` / `local` name the SHAPE of the work: a whole audio
file uploaded and transcribed, a live stream transcribed while the audio is
still being spoken, or an engine on this machine. A vendor-named value would
have to change (and every operator's settings file with it) the day a second
vendor answers the same shape — which is exactly the defect this task was
dispatched to fix, one level up.

---

## 3. Migration policy, and what it costs

### The policy, in one sentence

**Every pre-rename channel is still READ; none is silently honoured.**

Three classes, each handled at the same rung as its successor (Rule 2):

| class | old spelling | still read from | replacement |
|---|---|---|---|
| engine selector | `voice_engine` / `GOPI_VOICE_ENGINE` | settings, env, at the flag's rung | `audio_engine` / `GOPI_AUDIO_ENGINE` |
| engine NAMES | `doubao`, `doubao-stream` | wherever a name appears (flag, env, settings) | `recording`, `streaming` |
| parameters | `doubao_api_key`, `doubao_key_file`, `doubao_base_url`, `doubao_resource_id`, `doubao_hotwords`, `doubao_app_id`, `doubao_token`, `doubao_stream_endpoint` + their `GOPI_DOUBAO_*` env names | settings (folded into the nested block), env (same rung as the new name) | `audio_engine_param.<block>.<knob>` + the `GOPI_AUDIO_*` names |

### The mechanics, and the one place each lives

* **The engine name** is translated by `config.AudioEngineLegacyVendorName`
  (`internal/config/settings.go`) — ONE lookup, called by the resolver, the
  explicit-choice predicate, the validator and the settings reader. There is no
  second table of "what doubao means".
* **The settings keys** are folded into the nested block by
  `Config.normalizeAudioParams` (`internal/config/audio_engine_param.go`), which
  runs at the END of the load path: `nested block > legacy top-level key >
  built-in default`, applied field by field and only where the new field is
  empty. An operator mid-migration therefore gets exactly the knobs they moved,
  not "the new surface wins wholesale".
* **The env names** are read by the block resolvers themselves
  (`config.ResolveAudioRecording` / `ResolveAudioStreaming` / `ResolveAudioLocal`)
  — the CURRENT name first, the pre-rename name at the same rung. This is also
  the fix for a Rule 2 debt the old surface carried: cmd/agent used to read
  `GOPI_DOUBAO_*` inline in the credential resolver AND in the settings resolver.
* **The notice** is printed once per run by
  `cmd/agent/cli_voice_deprecation.go`, under the `needsEngine` guard (the
  process that will do the work is the process that speaks), from sentences
  composed in `internal/config` so the CLI, the chat path and `gopi config show`
  cannot describe the migration three ways.

### What the policy costs

1. **A second reading path exists and must be maintained.** The legacy fields
   are on `Config` with their old JSON tags, folded at load time. They are never
   written back (gopi only reads settings), so the cost is reading, not drift.
2. **The notice is a permanent line in the output of an unmigrated host.** It
   disappears on migration and is silent otherwise; a host that never migrates
   pays one line per run until it does. That is the intended trade: the
   alternative is a configuration that stopped taking effect with no message.
3. **The deprecation cannot expire on its own.** There is no date after which the
   old keys are dropped. Removing them is a separate, deliberate task with the
   notice list as its inventory — and the doc says so rather than promising a
   deadline nothing enforces.
4. **`gopi config show` renders one row per nested leaf, keyed by the dotted
   path.** The cost is a longer settings section; the benefit is that a
   credential inside a block can be redacted at all (a whole-block JSON cell has
   no leaf name to redact by — see §4).

---

## 4. What was PRESERVED (the acceptance the brief named)

* **The two-stage deployment search** with `modelsExplicit` / `runtimeExplicit`
  (`cmd/agent/cli_voice_local.go`): an explicitly answered path is never
  overridden. The configured local block enters at the SAME rung as the flag
  (`opts.modelsExplicit = true` in `parseVoiceArgs`), so a path written in
  settings.json is a declaration of where the deployment lives, not a hint the
  search may improve on. `modelsFromConfig` / `runtimeFromConfig` record that
  the path came from configuration rather than the flag, purely so the
  provenance line names the right channel.
* **The single credential-precedence resolver**: `voiceDoubaoKeySource` still
  owns the order (`flag > env > settings > legacy fallback`), and the asymmetric
  rule (an explicitly named FILE outranks an inline value) is intact — the
  settings and env rungs of it now come from `config.ResolveAudioRecording`
  rather than being read inline twice.
* **The 0600 settings-permission guard**: `EnsureSettingsPrivateForCredential`
  is called at the same point, and `SettingsDocumentCarriesCredential` learned
  the NESTED surface (`audio_engine_param.<block>.api_key|app_id|token`) — a
  guard that only looked at the top level would have left a document holding a
  live nested key world-readable.
* **`gopi config show` redaction inside the nested structure**: the renderer
  flattens the block into one row per leaf, and the redaction question is asked
  per row (`config.IsCredentialSettingsKey` for a top-level key,
  `config.IsAudioCredentialParamName` for a dotted path). A row whose value is a
  credential renders `<set: N chars>` with N the DECODED length, exactly as
  before.
