<!-- doc-class: historical -->
# Task Report: vision_analyze + Native Image Input — Code Review

## Review Scope

- **CLI subcommand**: `gopi vision_analyze <image_path> [--prompt "text"]` — `cmd/agent/cli_vision.go`
- **Native image input**: positional image file (.png/.jpg/.gif/.webp) attached natively to the vision model — `cmd/agent/main.go` (`readInput`, `imageSources`, `newAgent`)
- **Config plumbing**: `GOPI_VISION_MODEL` / `GOPI_VISION_BASE_URL` / `--vision-model` / `--vision-base-url` — `internal/config/*`, `internal/deepseek/config.go`, `internal/deepseek/claude_client.go`
- **Shared engine**: `internal/tools/vision.go` (`LoadImage`/`IsImagePath`/`VisionAnalyze`), `client.VisionAnalyze`
- Feature history: 3 commits, all authored 2026-08-26 (8a9afac → 6d86b78 → 6fb876f)

## Design Purpose

The vision feature has one clean idea, executed well in the main (top-level
`gopi`) path: **images are first-class input to the model**. A positional that
is an existing image file is attached natively as an Anthropic image block;
the vision model reads the pixels in its own context (image tokens ≈ text
tokens); no OCR round-trip. The `vision_analyze` tool/CLI is the standalone
one-shot form of the same engine (base64 → one lightweight request → text
result only), designed for screenshots/OCR/图表 analysis where the *image
bytes must never enter the main context* (context protection).

Both entry points share one engine (`tools.LoadImage` + `tools.VisionAnalyze`
→ `client.VisionAnalyze`) — the CLI subcommand is literally the same function
path as the agent's `tool_use` dispatch (`loop.go` case `"vision_analyze"` →
`tools.VisionAnalyze`). No drift between tool and CLI.

Verified live (real DeepSeek endpoint):
- `gopi vision_analyze /tmp/vision-ocr-test.png` → correct Chinese OCR
  (「你好，世界！Vision OCR 测试…」), usage reported (271 in / 272 out tokens).
- Missing file / unsupported format / no-arg → clean user-actionable errors.
- Empty `--prompt ""` → falls back to default prompt (works).

## Findings

### P0 — nothing found in this review (feature is young, well-tested).

### P1

#### P1-1. Subcommand ignores `vision_model` from settings.json (two-resolution asymmetry)
The standalone `vision_analyze` CLI/subcommand and the agent's in-loop tool
resolve the model differently from the native-image path:

- `client.VisionAnalyze` (claude_client.go:765): `ResolveVisionModel(nil)` —
  **env-only** (`GOPI_VISION_MODEL`); settings `vision_model` is never read.
  Also `tools.VisionAnalyze`'s result banner re-resolves with `nil`.
- `cmd/agent/main.go` native-image path: `ResolveVisionModel(cfgForVision())`
  — env > settings > default (three sources).

**Verified live**: with `GOPI_VISION_MODEL`/`GOPI_VISION_BASE_URL` unset and a
settings.json containing `"vision_model":"glm-test-vision-X"`:
- `gopi vision_analyze img.png` → wire model **deepseek-v4-flash-vision-exp**
  (settings value ignored).
- `gopi img.png` (native) → stderr `using vision model glm-test-vision-X`
  (settings value honored).

Also, the `vision_analyze` subcommand is dispatched in `main()` **before**
config resolution (it never runs `FromSettingsFile`/`ApplyEnv`), so even a
subcommand-level settings read would need its own plumbing. The in-agent tool
has the same blind spot when the agent runs with a settings-only
`vision_model` (env never injected). In practice `~/.gopi/settings.json` has
both `vision_model` and `GOPI_VISION_MODEL` env set, so this is latent —
but it is a genuine single-source-of-truth violation (Rule 2): "vision model
resolution" has two rules depending on entry point.

**Suggestions**: (a) `tools.VisionAnalyze` should load the config once
(`NewVisionClient` already does) and pass it to a model resolution that
includes settings, or (b) `client.VisionAnalyze` should resolve the model in
the same place `NewVisionClient` resolves base URL + key — one shared
resolution helper. Add a regression test for settings-only `vision_model`.

#### P1-2. No stderr warning when an explicit `--model <text>` is combined with an image
Prior review (devdoc/image-input-verification-report.md, memory #893) already
flagged this as the biggest UX gap: `gopi --model deepseek-v4-flash img.png`
still attaches the image natively, but the text model cannot see pixels → it
silently degrades to repeated `vision_analyze` OCR calls (26 in one run).
The recommended stderr warning was **never implemented** (only exists in the
devdoc as a code snippet).

**Suggestion**: in `main.go`, when `len(input.images) > 0 &&
overrides.Model != ""` and the chosen model is not the vision model, emit:
`[gopi] warning: --model %q is a text model and cannot see image pixels;
omitting --model auto-switches to the vision model %s`.

#### P1-3. No magic-byte validation — extension-only format check
`LoadImage` validates format by extension (`visionMediaTypes` map lookup)
before reading bytes, then trusts the extension for the wire media_type.
A text file renamed `fake.png` passes local checks and is only rejected by
the **remote API after base64-encoding + sending** (verified live: HTTP 400
"unsupported image" + a full minimal-core degradation cycle + dump written).
Costs a wasted round-trip and confusing error text.

**Suggestion**: sniff the first bytes (PNG `\x89PNG`, JPEG `\xFF\xD8`, GIF
`GIF8`, WebP `RIFF....WEBP`) in `LoadImage` after reading and reject locally
with the same user-actionable error style; map the *sniffed* type to
media_type rather than trusting the extension. Keep the extension check as a
cheap pre-filter (per current doc comment intent).

### P2

#### P2-1. Raw log bloat: full base64 `data` written un-elided
The raw logger writes the full request body verbatim, so every image block's
base64 `data` lands in the raw log — measured 602–948 KB logs for a ~1.3 KB
base64 image in the prior verification (6 copies). For a 32 MiB image this
would be ~42 MB of base64 per copy. The raw log is for replay/debugging of
the conversation, not for re-deriving image bytes.
**Suggestion**: elide/truncate/hash the `data` field of image blocks in the
raw logger (request still carries the bytes).

#### P2-2. `visionMaxFileBytes` (32 MiB cap) is duplicated, and the cap is only checked on the *file* path
- `internal/tools/vision.go` declares `visionMaxFileBytes = 32 << 20`.
- `cmd/agent/cli_web.go` declares a **second** `webMaxChatImageBytes = 32 << 20`
  ("the SAME 32 MiB budget … one validation rule, two entry points" — but it
  is literally two constants, violating the comment's own claim; if the
  ceiling ever changes, the web path and file path silently diverge).
- The 32 MiB ceiling is only enforced against the *file size* on disk;
  there is no check that the *decoded* image (base64 → raw bytes) stays
  under a budget on the file/vision path. Also, per-image **token** cost on
  the native path has no cap: a 32 MiB file could carry thousands of images
  worth of tokens into the main context window (200k default) with no
  interplay check.

**Suggestions**: (a) hoist the cap to one shared exported constant (e.g.
`internal/tools.MaxVisionImageBytes`) and reference it from cli_web.go;
(b) consider checking decoded size on the vision path too.

#### P2-3. Default-prompt literal duplicated in ≥5 places + a dead constant
`"描述这张图片，并识别其中的文字（如有）"` appears as a raw string literal in:
`internal/tools/vision.go:37` (declared but **never referenced** —
`visionDefaultPrompt` is a dead constant), `internal/deepseek/claude_client.go:763`
(the effective default), `cmd/agent/cli_vision.go:26,32` (usage text),
`internal/agent/loop.go:1210,1220` (tool description). Also `visionUsage`'s
help text hard-codes the default model name and endpoint.
**Suggestion**: export the default prompt from one package (e.g. keep
`visionDefaultPrompt` in tools and use it in claude_client, or vice-versa) and
have the usage/tool descriptions reference it — or at least delete the dead
constant and add a cross-check test (like usage_test.go) pinning the help
text to the effective default.

#### P2-4. `vision_analyze` CLI lacks `--vision-model`/`--vision-base-url` flags
Top-level `gopi -h` advertises `--vision-model NAME` / `--vision-base-url` as
flags for image handling, and the subcommand's own usage text says
"Model: GOPI_VISION_MODEL … Endpoint: GOPI_VISION_BASE_URL" — but
`gopi vision_analyze img.png --vision-model X` errors with "unknown flag
--vision-model" (verified live; only `--prompt` is accepted). A user who
wants a one-shot vision call with a different model must export the env var.
Also, the subcommand runs before config resolution, so `--settings` paths /
env-only injection don't apply to it either.
**Suggestion**: accept (or at least document the env-only constraint) —
either plumb `--vision-model`/`--vision-base-url` through to the subcommand's
flag set, or say in the usage text that these must be set via env before the
call.

#### P2-5. Drift between tool description and actual defaults (minor)
- `internal/agent/loop.go:1210` tool description says default prompt
  "描述这张图片并识别文字" — *missing* "（如有）" compared to the actual
  default; minor but it is a visible LLM-facing description.
- The tool description also hard-codes "deepseek-v4-flash-vision-exp" while
  the real default lives in `internal/deepseek` — if the default ever changes,
  the description the model reads will be stale (LLM is the primary consumer
  of that text; Rule 11 territory).

## Strengths (what is right)

- **One shared engine, no tool/CLI drift**: CLI subcommand, in-loop tool
  dispatch, and native-image loading all call `tools.LoadImage` /
  `tools.VisionAnalyze` — single validation rule for format + size.
- **Rule 1 mostly honored**: the 32 MiB cap *is* a named constant
  (`visionMaxFileBytes`) in the primary path; default prompt is a named
  constant (albeit dead); usage text is a const.
- **Clean error handling**: missing file, unsupported extension, oversized
  file all produce user-actionable messages before any network I/O; empty
  positional and extra-arg cases are covered.
- **Auto-switch precedence correct**: explicit `--model` wins over image
  auto-switch (`overrides.Model == ""` guard), resolved *before* agent
  construction so session_start records the true wire model (regression test
  exists). Env-vs-settings precedence (env > settings > default) verified by
  unit tests in internal/deepseek/vision_test.go.
- **`--prompt` semantics documented + implemented**: when set, positionals
  are files/images; literal prompts mentioning `.png` are not misread as
  files (stat-first classification; flag-value leak fixed).
- **Native image attach is first-class**: image blocks never merged, one per
  user message; resume checkpoints keep the image (json-serialized).
- Tests are comprehensive: parse tests (cli_vision_test.go), schema +
  validate + dispatch wire tests (agent/vision_test.go), resolution +
  wire-shape tests (deepseek/vision_test.go), readInput classification tests
  (main_test.go).

## Not Done (deferred / out of scope)

- No code changes shipped (review task = docs only). All P1/P2 items are
  recommendations with code sketches, not patches.
- Full `go test ./...` suite was not run to completion (slow test compile on
  this box); targeted suites (internal/config — PASS; internal/deepseek +
  internal/tools — compile-started, killed by timeout) plus live CLI
  verification were used instead.
- Web chat (R5) image path and `gopi web` were read for the shared-cap
  finding but not deeply reviewed (out of the stated scope).

## Verification

- Build: `go build -o bin/gopi ./cmd/agent/` — OK (main repo).
- Live CLI: `gopi vision_analyze -h` prints full usage; real OCR call
  succeeded on `/tmp/vision-ocr-test.png` (deepseek-v4-flash-vision-exp,
  271/272 tokens).
- Error paths: missing file, `.txt` extension, no-arg, extra-arg,
  `--vision-model` rejected — all produce clean errors.
- Settings asymmetry: settings-only `vision_model` honored on native path,
  ignored on subcommand (both directions live-verified).
- Fake `.png` (text bytes): passes local check, rejected by remote API after
  send — confirms missing magic-byte validation.
- Tests: `go test ./internal/config/` — all PASS (incl. ApplyEnv precedence,
  FromSettingsFile).
- Commit: see git log on branch `review/cli-vision` (docs commit below).

## Prioritized List

| Pri | Item | Where |
|-----|------|-------|
| P0 | (none) | — |
| P1 | Settings-only `vision_model` ignored by vision_analyze subcommand/in-tool (two-resolution asymmetry) | claude_client.go:765, tools/vision.go:130 |
| P1 | No warning when explicit `--model <text>` + image → silent OCR degradation | cmd/agent/main.go (modelOverride block) |
| P1 | Extension-only format check; fake image wastes a round-trip + confusing 400 | tools/vision.go LoadImage |
| P2 | Raw-log base64 bloat (no elision) | internal/agent/rawlog.go |
| P2 | 32 MiB cap duplicated (tools + cli_web); no decoded-size / token-budget check on native path | tools/vision.go:34, cli_web.go:681 |
| P2 | Default-prompt literal ×5 + dead `visionDefaultPrompt` const | tools/vision.go:37, claude_client.go:763, cli_vision.go, loop.go |
| P2 | `vision_analyze` CLI rejects `--vision-model`/`--vision-base-url` despite help advertising them | cli_vision.go visionFlagMember |
| P2 | Tool-description default prompt text missing "（如有）"; hard-coded model name | loop.go:1210 |
