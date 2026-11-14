<!-- doc-class: historical -->
# Task Report: Native Image Input — vision model processes images directly (no OCR round-trip)

Branch: `feat/image-input` (worktree `/tmp/gopi-image-input`)
Base: master `2cfa8e4`

## Goal

The Epic asserts that for DeepSeek the vision model (`deepseek-v4-flash-vision-exp`)
is the same text model with vision tokens treated equally as text tokens — so an
image should be FIRST-CLASS MODEL INPUT, processed natively by the vision model,
not OCR-extracted via the `vision_analyze` tool. Two user commands were broken:

```
gopi --model deepseek-v4-flash-vision-exp "your task is written in image ~/x.png"
gopi --model deepseek-v4-flash-vision-exp --prompt "..." vision_analyze ~/x.png
```

## Theory verification (live, before implementation)

Sent a base64 image as a native Anthropic image block in the main loop to
`deepseek-v4-flash-vision-exp`: the model read the pixels directly —
"Hello from the test image / The answer is 42" — with NO vision_analyze call.
Raw log: 18 requests to `deepseek-v4-flash-vision-exp`, 9 native `"type":"image"`
blocks on the wire. **Theory CONFIRMED.**

## Implemented

1. **Native image content in the main loop** (`internal/deepseek/types.go`,
   `claude_client.go`): `deepseek.Message` gains `Image *ClaudeImageSource`
   (json-serialized so resume checkpoints keep the image). `BuildClaudeRequest`
   emits a user message carrying an image as `{"type":"image","source":{...}}`
   followed by the text instruction. Image-carrying user messages are never
   merged (one image block per message on the wire).

2. **Image auto-detection** (`cmd/agent/main.go`): a positional that is an
   EXISTING file with a supported image extension (.jpg/.jpeg/.png/.gif/.webp)
   is loaded (shared `tools.LoadImage` — same 32 MiB + format validation as the
   vision tool) and attached natively to the first user message. `gopi img.png`
   works with NO prompt needed.

3. **Auto model switch**: when images are present and the user did not pass
   `--model`, the agent's model is switched to the vision model
   (`GOPI_VISION_MODEL`/`--vision-model`/`vision_model`; default
   `deepseek-v4-flash-vision-exp`), with a stderr notice
   "[gopi] image input detected → using vision model …". Explicit `--model`
   always wins.

4. **`--prompt` top-level flag** (`internal/config/flags.go`, `config.go`,
   `env.go`, `settings.go`): `gopi --prompt "describe the chart data" chart.png`
   — prompt is the instruction, positionals are files/images. Also via
   `GOPI_PROMPT` env and settings `prompt` key.

5. **Prompt-vs-file classification** (fixed the broken `gopi "write a README"`):
   a positional that is not a readable file is literal prompt text — even prose
   that mentions a `.png` path. Existing text files are read as the prompt.

6. **Flag-value leak fix** (root cause of Epic cmd 1): `FlagOverrides` now
   consumes flag values (tracked in `cfg.Positionals`), so `--model
   deepseek-v4-flash-vision-exp` can never be mistaken for an input file.

7. **`--model`/`--api-key`/`--base-url` now beat shell-exported env** (root
   cause of Epic cmd 2 silently using the wrong model): the flag marks BOTH
   `ANTHROPIC_*` and `DEEPSEEK_*` env keys in `SetEnvVars`, so a shell-exported
   `ANTHROPIC_MODEL=deepseek-v4-flash` can no longer beat `--model`. Live-verified
   in the raw log.

8. **Help** (`internal/config/help.go`): new "Image input (native vision…)"
   section, `--prompt` documented, auto-switch documented. Usage-test guard
   added.

## Not Done

- The model may still autonomously CHOOSE to call `vision_analyze` mid-task
  (ReAct autonomy) even when the image was attached natively — that is the
  model's tool-use decision, not a pipeline limitation.
- No multi-image content-block merging (each image = own user message) — fine
  for the 1–2 image case.
- GIF animation: only the first frame semantics apply (same as the tool).

## Verification

- Build: `go build -buildvcs=false ./...` OK
- Tests: `go test -buildvcs=false ./...` — all packages PASS
  (cmd/agent incl. 8 new readInput/imageSources tests, internal/config incl.
  6 new flag tests, internal/deepseek incl. image-block wire tests)
- go vet: clean
- Live (raw log verified):
  - `gopi --model deepseek-v4-flash-vision-exp "say MODEL-OK"` → model
    `deepseek-v4-flash-vision-exp` on the wire, no "no such file" error
  - `gopi screenshot.png` → "[gopi] image input detected → using vision model
    deepseek-v4-flash-vision-exp", model reads image natively
  - `gopi --prompt "describe…" chart.png` → native image block + prompt on wire
- Commit: see git log

## Files

```
cmd/agent/main.go               input classification + image attach + model switch
cmd/agent/main_test.go          NEW — readInput/imageSources tests
cmd/agent/usage_test.go         help guards for native image input
internal/agent/loop.go          Run(images...) variadic + runEngine attach
internal/config/config.go       Prompt + Positionals fields
internal/config/flags.go        --prompt, flag-value-aware positionals, dual env-key marks
internal/config/env.go          GOPI_PROMPT propagation
internal/config/settings.go     GOPI_PROMPT catalog entry
internal/config/help.go         image-input documentation
internal/config/config_test.go  flag parsing tests
internal/deepseek/types.go      Message.Image field
internal/deepseek/claude_client.go  image block emission (never merged)
internal/deepseek/claude_client_test.go  wire-format + round-trip tests
internal/tools/vision.go        LoadImage/IsImagePath shared helper (refactor)
```
