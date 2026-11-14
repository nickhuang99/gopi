<!-- doc-class: historical -->
# Task Report: Full Source Read + Code Inventory

## Goal

Read **every non-test Go source file** in the GoPi repository (all packages under
`internal/` and `cmd/`), distil each into the hippocampus knowledge base so future
agents can locate/understand any subsystem without re-reading source, and produce
this code inventory.

## Completed

Read all **54 non-test `.go` files** in full (≈49,000 lines total) and stored a
distilled record per file (or per logical section for the large files) into
memory (record ids **#924–#979**). Packages covered:

| Package | Files | Notes |
|---------|-------|-------|
| `internal/agent` | 10 | loop.go (2776 lines, largest file), context.go, compact.go, resume.go, session.go, rawlog.go, clock.go, heartbeat.go, tasklist.go, trajectory.go |
| `internal/tools` | 12 | memory.go (2376 lines), bash.go, symbol.go, websearch.go, read.go, gitblame.go, search.go, job.go, vision.go, timeout.go, edit.go, write.go, text2img.go |
| `internal/deepseek` | 5 | claude_client.go, client.go, config.go, retry.go, types.go |
| `internal/config` | 5 | config.go, env.go, settings.go, flags.go, help.go |
| `internal/proxy` | 5 | request.go, response.go, server.go, stream.go, types.go |
| `internal/checkpoint` | 2 | index.go, parse.go |
| `internal/gopilog` | 4 | formatter.go (1106 lines), reader.go, rawlog.go, types.go |
| `internal/text2img` | 1 (+test) | render.go (872 lines) — read via bash due to read-tool false "binary" flag |
| `cmd/agent` | 9 | main.go + 8 cli_*.go |
| `cmd/text2img-verify` | 1 | main.go |

## Key Findings / Cross-cutting Faithfulness Notes

1. **loop.go is the beating heart** — the ReAct loop. 2776 lines holds the
   constants (thinking budget harness, task-list guards, tool defs), the Agent
   struct with all execution hooks, `NewAgent` env parsing, the `Run` loop with
   exit logic, tool execution (withTick stamping order), thinking-budget abort,
   and context trimming (fold → compact → trimToFit).
2. **Known duplicates documented in prior redundancy review** (records #918/#921):
   - `flexibleString` + `todoItem` + `resolve()` exist in BOTH
     `internal/gopilog/formatter.go` AND `internal/agent/tasklist.go`
     (brother packages, don't import each other).
   - `tools.EditDetails` (edit.go:93) is a test-only dead exported func.
   - `tools.JobCount()` (job.go) is dead code (never wired).
   - env-int parse blocks repeated ~14× between loop.go and compact.go.
3. **Case-study file**: `internal/text2img/render.go` is the self-built pure-Go
   TrueType rasterizer (zero deps, 4× supersampling, non-zero-winding fill,
   half-open vertex rule) — the flagships of the "pixel-level render truth" and
   "self-built over mirror-swap" habits. Verified by `cmd/text2img-verify`.

## Verification

- Build: `go build ./...` → **OK** (exit 0)
- Tests:  `go test ./...` → **ALL PASS** (internal/agent 25.7s, internal/tools 112.7s incl. SQLite memory tests, others cached)
- Commit: this is a read + doc + memory task — no production code changed, so
  no feature commit. Report committed as the only artifact.

## Not Done

- No production code modified (this was a read-and-distill task).
- `cmd/text2img-verify` has no tests (it is a manual smoke/verify tool, not a
  library — intentionally).
- Memory records are the distilled-by-product; the authoritative persistent
  store is memory.db (query via `memory_search`).

## How To Use This Inventory

Future tasks needing a subsystem's behavior should `memory_search` the record by
topic (e.g. "loop.go ReAct 循环 主引擎", "compact.go 记忆压缩", "memory.go 海马体"),
which returns the distilled constants/design/pitfalls without re-reading source.
The record id range **#924–#979** is the GoPi codebase coverage layer.
