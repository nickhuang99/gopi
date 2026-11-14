<!-- doc-class: historical -->
# GoPi Three-Tier Logging Architecture

Date: 2026-08-05
Source: discussed during terminal chaos + context snapshot design review

## Three Tiers

| Log | Byte-exact | Structured | Size | Purpose |
|-----|-----------|------------|------|---------| 
| `raw/` | YES (REQUEST) / [d] (CHUNK, fixing) | NO (text stream) | large (MBs) | debugging, context snapshot source, clone replay |
| `sessions/` | NO (re-serialized) | YES (JSONL) | medium | queryable analysis, token stats, gopi-log reader |
| `logs/` | NO | NO (formatted text) | small (10-40KB) | quick scan: iteration count, tool calls, cache stats |

Key insight: `raw/` direction markers (-- REQUEST, - CHUNK) are GoPi-added; not part of HTTP wire.
REQUEST body = exact `reqBody []byte` that goes to DeepSeek -- byte identical, cache-hit safe.
CHUNK strips SSE `data:` prefix (to be fixed: preserve for replayability).
Session JSONL tool_use.input is re-marshalled from `map[string]interface{}` -- NOT byte-safe for clone.

`logs/` frozen since Aug 3 when raw logging became always-on. Superseded by `gopi-log --summary`.

## Micro-Compaction Trigger Strategy

v0.20.0 micro-compaction: default false, "breaks provider prompt-cache prefix every pass".

Decision: cherry-pick algorithm but change trigger from `every_n_turns` to `estimated_tokens > threshold` (90% window).
Daily operation: no trigger, prefix stable, cache hit sustained.
At limit: sink cost, cache will break anyway, micro-compaction quality beats simple truncation.
Keep `ephemeral_system_prompt` appended at end (does not break prefix in daily operation).

## Context Snapshot Design

Data already exists in raw log. Every `-- REQUEST` body is a complete byte-exact context checkpoint (system prompt + GOPI.md + full message history + tools + thinking params). Missing: index, extractor, clone bootstrap, detached sub-task mode.

Report: /home/user/work/GoPi/devdoc/context-snapshot-design.md (459 lines, 5-phase roadmap).
