<!-- doc-class: historical -->
# Bash Efficiency Rules + output soft-cap (race findings → fixes)

Branch: `feat/bash-efficiency` (evaluate → merge).

## Why

The gopi-vs-claude race (2026-08-06, same deepseek-v4-flash on the same
endpoint) measured: gopi 27 min / 61 rounds / 69 tool calls vs claude 7 min
/ 82 rounds / 81 tool calls. The gap was NOT model speed, thinking mode, or
the API — the step-by-step log comparison showed:

1. **Per-round information density**: gopi issued one tiny fact per round
   (`sed -n '24050,24260p' … | grep`); claude issued one dense extraction
   per round (awk ranges + python one-liners). Same ~1 call/round — the
   difference is what each call extracts.
2. **State awareness**: claude ran `git log --all` and found the fix already
   committed (then pivoted to verification); gopi's `git log -15` missed it
   and re-implemented.
3. **Verification strategy**: claude verified with a mock HTTP endpoint
   (~1 min); gopi with nested real runs + polling (~10 min, tripping 30s
   tool timeouts).
4. **Context growth**: gopi's prompts grew 2.4K → 173K tokens (27s/round
   late); claude's stayed bounded by its harness compaction.

## Changes

1. **`bashEfficiencyRules`** — an original system-prompt block (not copied
   from any leaked prompt; generic agent best practices) teaching: one dense
   command per round, never cat whole files, extract many facts per call,
   self-cap output, adjust on error instead of retrying, check `git log
   --all` early, prefer cheap deterministic verification.
2. **Bash soft output cap** — `softOutputSize = 16KB`: outputs above it are
   truncated with the hint "narrow the command with grep/head/sed". The hard
   100KB cap remains. A full 100KB result is ~25K tokens shoved into the
   next request — the measured context-growth driver.

## Verification

- `go build ./...`, `go vet ./...`, `go test ./...` all pass.
- New tests: system prompt contains the rules; bash soft cap + hint.
- Validation run: TBD (gopi with the new prompt on a fresh task — measure
  rounds and per-round density vs baseline).
