<!-- doc-class: historical -->
# DeepSeek V4 Thinking Mode

Source: ~/work/claude/nick/deepseek_v4_thinking_mode_talk.txt (2026-07-30, doubao analysis)

## Key Facts

Both V4-Flash and V4-Pro support native thinking mode via API:
```
thinking: {"type": "enabled"}
reasoning_effort: high | max
```

**reasoning_effort levels:**
- `max` — unrestricted thinking budget; model can self-verify, backtrack, replan. Best for agent/planning tasks.
- `high` — medium depth, balanced speed-quality. Good for coding without extreme complexity.

**Thinking mode is NOT just "show CoT"** — it activates a specialized decoding path where the model explicitly generates `reasoning_content` before the final answer. This is GRPO-trained behavior, not prompt engineering.

**Disabled thinking ≠ no reasoning** — the model can still infer via prompt-based CoT, but stability/self-correction drops.

## Flash vs Pro

| | Flash (13B active) | Pro (49B active) |
|---|---|---|
| thinking mode | full high/max | full high/max |
| agent/coding | close to Pro, sometimes better | higher ceiling |
| world knowledge | weaker | stronger |
| cost | much lower | 3x more |
| **rule of thumb** | Flash-max ≈ Pro-high | Pro-max for truly hard problems |

## GoPi Config

Default: `reasoning_effort: max`, `thinking: enabled` for all models.
Override: `GOPI_REASONING_EFFORT=high` or `GOPI_THINKING=disabled`.

Commit: GoPi internal/agent/loop.go — reasoningEffort default changed from "" (DeepSeek model default) to "max".
