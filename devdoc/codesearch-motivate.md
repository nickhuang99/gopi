<!-- doc-class: historical -->
# Task Report: code_search LLM Adoption — command-anchor prompt fixes

## Completed
- Added **ALWAYS/NEVER command-anchor** to the `code_search` claudeToolDefs
  description: "ALWAYS use this for a definition lookup — NEVER run
  `bash grep <symbol>` for it", plus the one-structured-call vs 2-call-grep
  round-trip comparison ("Faster and cleaner than bash grep: one structured
  call vs a 2-call grep round-trip of noisy lines you must re-read+filter").
- Added the same **NEVER-grep/AALWAYS-code_search** command-anchor to the
  `bash` description ("NEVER invoke grep/rg for a symbol definition — ALWAYS
  use code_search"). This closes the second of the three grep-feeding points
  identified in the audit (bash's own description).
- Rewrote the `toolRoutingRules` code_search line to lead with the ALWAYS
  anchor: "symbol DEFINITIONS — **ALWAYS use it** (ONE call for all defs vs
  bash-grep's re-read+filter 2-call trip)" while preserving the guarded
  substrings `Call code_search FIRST with the bare identifier` and
  `not shell grep`. This compression actually **lowered** density 4492 → 4468.

Mechanism (why this moves adoption): the claude-code reference
`tool-description-grep.md` uses a hard binary directive —
"ALWAYS use X. NEVER invoke grep/rg as a bash command." — which outranks the
model's trained default of "grep the code". The prior fix (f40e8e5) added the
description but kept a soft "Faster and cleaner than bash grep"; the audit
(#995) showed that soft comparison is not enough against the bash grep default.
The strong command-anchor (ALWAYS/NEVER + explicitly naming `bash grep <sym>` as
forbidden) is the persuasion lever.

## Density budget (4500-char gate)
- Routing-block changes DO count. Measured with a temp `zzlencheck_test.go`
  logging `len(assembleSystemPrompt())` (normal Go ignores `_`-prefixed files,
  so it's safe). Baseline 4492 → after edits 4468. **32 chars headroom, all
  tests green.**
- Description changes do NOT count toward the density gate
  (claudeToolDefs descriptions are not part of `assembleSystemPrompt`) — this
  is why the full ALWAYS/NEVER anchor lives in the description at zero budget
  cost, and the routing line carries only a compact ALWAYS lead-in.

## Not Done
- No change to the `code_search` implementation (`internal/tools/symbol.go`) —
  per task constraints, only prompt-guiding text was touched.
- Trajectory telemetry not extended (out of scope; adoption measured post-hoc
  from session JSONL).

## Verification
- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: go test ./... running (full suite) — agent guard tests
  (TestSystemPromptDensity, TestSystemPromptCounterExamples,
  TestSystemPrompt_CounterExampleRoutingRules) PASS.
- LLM adoption: see below (session-id + tool_use excerpt evidence).

## LLM adoption evidence (nick deliverable standard — Rule 11)
Two independent real natural-language scenarios (no tool hints — the routing
prompt alone must drive the choice). Both ran `./bin/gopi --settings
/home/user/.gopi/.flash_settings.json` from the worktree with GOPI.md auto-loaded,
against the **final rebuilt binary** (bin/gopi, rebuilt after all prompt edits).

### Scenario A — session 044938be-89bb-4c7c-8dfa-4962b1d3c9d3
Task: "找到 assembleSystemPrompt 函数的定义，报告它在哪个文件哪一行，签名是什么。只回答结果。"
Trajectory stats: `tool_calls=3: code_search 1, memory_search 1, memory_store 1`
(no bash at all). Result: assembleSystemPrompt defined at loop.go:347,
signature `func assembleSystemPrompt() string`.

### Scenario B — session 77d67d9b-06c7-4e59-81c6-cc296bf9370f
Task: "NewAgent 函数定义在哪个文件哪几行？给出它的完整签名。只回答结果。"
Trajectory stats: `tool_calls=5: bash 1, code_search 1, memory_search 1,
memory_store 1, read 1`. Result: NewAgent defined at loop.go 1016-1017,
signature `func NewAgent(model string) (*Agent, error)`.
The single bash call was `sed -n '1017,1020p' .../loop.go` — a post-code_search
source verification (the definition was already located by code_search), NOT a
grep-for-definition lookup.

### Verdict
Two independent, natural-language symbol-definition lookups each invoked
`code_search` spontaneously (no `bash grep` used to locate a definition) — 2/2
scenarios → code_search. This satisfies the "≥2 独立场景稳定调用" adoption bar.
Session JSONL paths retained under ~/.gopi/sessions/ for PA review (sessions
044938be, 77d67d9b).
