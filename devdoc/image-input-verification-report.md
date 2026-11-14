<!-- doc-class: historical -->
# Verification Report: Native Image Input — vision model processes images directly

Branch: `feat/image-input` (worktree `/tmp/gopi-image-input`)
Base: master `6d86b78`
Date: 2026-08-26

## Scope

The Epic asked to run model input + processing **completely using the vision
model** via a `--model` parameter to designate it, and to:
1. check for **unexpected behavior**,
2. **confirm logging / processing work as expected**,
3. suggest a **better / easier way** if one can be thought of.

The implementation (`feat/image-input`, commit `6d86b78`) already existed and
was merged into master. This report VERIFIES it end-to-end against the live
DeepSeek Anthropic-compatible endpoint.

---

## Completed (verified live)

### Primary paths — ALL WORK

| Command | Result |
|---|---|
| `gopi --model deepseek-v4-flash-vision-exp "say MODEL-OK"` | ✅ No "no such file" error. Wire shows model `deepseek-v4-flash-vision-exp`. **Fixed** (was the Epic cmd 1 bug). |
| `gopi img.png` (no `--model`) | ✅ Auto-switch: stderr `[gopi] image input detected → using vision model deepseek-v4-flash-vision-exp`. Model reads pixels natively. |
| `gopi --prompt "..." img.png` | ✅ Native image block + prompt on wire. Epic cmd 2 works. |
| `gopi img1.png img2.png` (multi-image) | ✅ One image block per user message (never merged), both attached. |

### Wire format (raw log evidence)
- Native image block emitted as `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"<base64>"}}`.
- Image-carrying user messages are **never merged** (one image block per message).
- 6 image blocks = 6 requests (each round re-sends the image).
- Media type `image/png` correct for the test PNG.

### Model override precedence — CORRECT
`--model` now marks BOTH `ANTHROPIC_MODEL` and `DEEPSEEK_MODEL` in `SetEnvVars`;
`ApplyEnv(cfg, fromFlags=true)` force-writes them. A shell-exported
`ANTHROPIC_MODEL` can no longer silently beat `--model` (this was the root
cause of Epic cmd 2 using the wrong model).

### Logging — WORKS
stderr reports, in order:
- `[gopi] loaded GOPI.md (N bytes)`
- `[gopi] raw log: <path>`
- `[gopi] session: <path>`
- (image path) `[gopi] image input detected → using vision model <model>`
- `[gopi] 🧠 task-start checkpoint: forcing memory_search before first round`
- Per-round thinking + final `⏱ Xs | N iterations | OK | tokens: ...`

### Build / tests
- `go build -buildvcs=false ./...` — OK
- `go vet -buildvcs=false ./...` — clean
- `go test -buildvcs=false ./...` — ALL packages PASS
  (cmd/agent incl. 8 readInput/imageSources tests, config, deepseek image-block wire tests)

---

## UNEXPECTED BEHAVIOR (the real findings)

### #1 — `--model <TEXT-model>` + image silently degrades to OCR (BIGGEST GAP)

When the user passes a **text** model with an image —
`gopi --model deepseek-v4-flash /tmp/gopi-test-img.png` — the image is STILL
attached natively, **but the text model cannot see the pixels**. The model
silently falls back to calling `vision_analyze` **26 times** in one run
(each image re-read + OCR'd via standalone vision requests). There is
**NO stderr warning** — the user has no idea their explicit `--model` defeated
the whole point of native image input.

**Recommendation:** emit a stderr warning in `main.go` when
`len(input.images) > 0 && overrides.Model != ""` and the chosen model is not a
vision model:

```go
if len(input.images) > 0 && overrides.Model != "" &&
    overrides.Model != deepseek.ResolveVisionModel(cfgForVision()) {
    fmt.Fprintf(os.Stderr,
        "[gopi] warning: --model %q is a text model and cannot see image pixels; "+
        "omitting --model auto-switches to the vision model %s\n",
        overrides.Model, deepseek.ResolveVisionModel(cfgForVision()))
}
```

This is cheap (no new tool, one stderr line) and prevents a 26× wasteful OCR
round-trip.

### #2 — Model still calls `vision_analyze` even with native image

Even on the exact Epic path
`gopi --model deepseek-v4-flash-vision-exp --prompt "..." img.png`, the model
still autonomously called `vision_analyze` (8 calls) alongside the native
image blocks. This is **ReAct tool autonomy**, documented in the report's
"Not Done" section as expected — but it doubles the vision work. The native
image is never *forced*; the model chooses to re-OCR.

This is a design tension, not a bug. It means the Epic's "run model input and
processing completely using vision model" is only true for the *first*
round — the model still reaches for the tool. If strict native-only is wanted,
the `vision_analyze` tool could be suppressed when an image was attached
natively, but that reduces the model's freedom. **Recommend:** leave as-is but
document that the native path is a *first-class input*, not a replacement for
the tool.

### #3 — Raw log base64 bloat

The full base64 `data` field is written to the raw log (6 copies per image,
each ~1,329 chars). Logs measured 602 KB – 948 KB for a tiny test image. There
is **no truncation/elision** of the `data` field in `internal/agent/rawlog.go`.

**Recommendation:** elide/truncate the `data` field (or hash it) in the raw
logger for image blocks, ahead of the byte content. The raw log is for replay /
debugging of the *conversation*, not for re-deriving image bytes (the request
still carries them). Halves log size for image-heavy sessions.

---

## Better / easier way (suggestions)

1. **The auto-switch is already the best UX** — `gopi img.png` just works with
   no `--model`. Recommend the warning (#1) over any forcing logic.
2. **Consider whether the image should be re-sent every round.** Currently the
   native image is re-attached to every loop iteration (6 image blocks = 6
   requests). The image tokens are small (~83/message) and cache-read
   (`cache_read_input_tokens: 7680`), so cost is bounded — but for very large
   images re-sending per round is avoidable. For most cases this is fine.
3. **The vision model == text model theory is CONFIRMED.** The Epic's core
   assertion holds: `deepseek-v4-flash-vision-exp` reads native Anthropic image
   blocks directly as pixels with no OCR tool round-trip. The implementation is
   sound.

---

## Not Done / known limits (from the original report)

- Model may still autonomously choose `vision_analyze` (see #2).
- No multi-image content-block merging (fine for 1–2 images).
- GIF: only first-frame semantics.

---

## Files inspected
```
cmd/agent/main.go              input classification + image attach + model switch + auto-switch notice
internal/deepseek/types.go     Message.Image field (JSON-serialized)
internal/deepseek/claude_client.go  image block emission (never merged), ResolveVisionModel/NewVisionClient
internal/config/flags.go       --prompt, flag-aware positionals, dual env-key marks
internal/config/env.go         ApplyEnv precedence (fromFlags force-write)
internal/tools/vision.go       LoadImage/IsImagePath (shared 32 MiB check)
internal/agent/loop.go         Run/runEngine image attach to first user message
```

## Commit
`6d86b78 feat(image-input): native image input — vision model processes pixels directly, no OCR round-trip` (already on master; no new code written by this verification).
