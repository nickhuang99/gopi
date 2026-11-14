<!-- doc-class: historical -->
# Code Review 2026-08-28 — Buddy Cross-Review (GO developer, second pass)

Agent: GO developer buddy reviewer. Scope: every non-test source file, same
repo pass as `code-review-2026-08-28.md` (started at HEAD d88f2e4, finished at
merge 4c71f74 — the loop.go conflict was resolved and landed DURING this
review). Method: memory_search for prior findings first, then line-by-line
verification of the source per Epic protocol.

## Verification status

- At review start: `go build ./...` FAILED (loop.go:124 unresolved conflict,
  UU state) — matched the companion report's CRITICAL.
- At review end: conflict resolved in 4c71f74, resolution merges BOTH
  semantics correctly (task-start tail now points contradictions at
  memory_update AND keeps the dead-end context_trim wiring + deadEndTrimMessage).
- Final: `go build ./...` OK, `go vet` OK (all packages), `go test ./...`
  ALL PASS (agent 29.3s, tools ~205s incl. real 120s bash-timeout regressions).

## New findings not in the companion report

1. **[low] cmd/agent/cli_proxy.go — parseProxyArgs double-dash error text.**
   `fmt.Errorf("unknown flag --%s", unknown[0])` prints `--` + the flag token
   that ALREADY contains its dashes: `gopi proxy --foo` errors as
   `unknown flag ---foo`. Cosmetic; use `%s` or strip leading dashes.

2. **[low] cmd/agent/cli_checkpoint.go — checkpointUsage resume section is
   stale vs cli_resume.go.** Usage doc says resume reads the continuation from
   stdin/positional join ("no [EPIC GOAL] reframing"), but cli_resume.go takes
   `resume <log> [continuation-file]` (file contents) and DOES warn on model
   drift; checkpoint/resume dispatches to the cli_resume implementation and
   errors by design. The two help texts advertise different resume semantics —
   future agents will trust the wrong one.

3. **[info] internal/proxy — companion report's file attribution correction.**
   streamResponse lives in **server.go:138** (stream.go holds only the SSE
   translator). The no-`sc.Err()` / no-client-ctx / `http.DefaultClient.Do`
   findings themselves were verified and stand.

4. **[info] internal/text2img render.go — partial correction to the 08-27
   review (#1016).** The format-4 cmap gidArr access at render.go:311-312 IS
   bounds-guarded: `idx >= 0 && idx < len(gidArr)/2` makes
   `be16(gidArr[2*idx:])` safe even for odd-length gidArr (max read index
   2*(len/2-1)+1 ≤ len-1). The parseTTF unguarded-table claim stands and
   remains the real panic vector.

## Companion-report findings independently re-verified (confirmed)

- `--keep-thinking` CLI flag dead (no applyFlag case, isBoolKey omits
  keep_thinking, absent from FormatUsage) — env/settings channel works.
- edit.go `os.WriteFile(..., 0644)` non-atomic + perm reset vs write.go's
  atomicWriteFile.
- compact.go boundSummaryInputMax 8-line dead first block (output correct).
- cli_checkpoint truncate() byte-cut (s[:n] not rune-safe).
- proxy anyToolSeen dead field (stream.go:141 written :249, never read).
- proxy dangling-SSE (no sc.Err()/ctx propagation) — real, minor.
- deepseek thinkingBudget stacked duplicate doc comment (claude_client.go:434).
- deepseek scanSSEEvents returns nil after flushing buffered events even on
  real I/O errors (documented design trade-off).
- Rule-1 nits: trajectory.go:150 / resume.go:61 scanner buffer magic numbers,
  compact serializeForSummary inline 6000/4000/1500/1200.
- memory.go escapeQuery mixed ASCII+CJK token not quoted (FTS operator risk),
  MemoryStore exact-dedup drops NEW keywords (merges sources only).
- gitblame.go storeBlameMemory write side-effect in a read-only tool.
- GOPI.md says 18 tools; loop.go wires 20 (git_blame, vision_analyze, text2img,
  context_trim added; memory_delete intentionally CLI-only).
- read tool false "binary/invalid UTF-8" on cmd/agent/cli_log.go +
  internal/text2img/render.go — both verified clean UTF-8 (`file`/`iconv`).

## Prior-review fix verification (2026-08-03 baseline)

- ContextOverflow dead code: REMOVED (types.go clean; EstimatedTokens/
  TrimToFit live in agent/context.go).
- Timeout wrapper duplication: unified via withTimeout[T] generic.
- Tilde drift: single shared expandHome in tools/tilde.go.
- env read repetition: envInt/envFloat in agent/envparse.go.
- Missing edit_test.go: exists now (104 lines).

## Design docs read before review (Epic requirement)

GOPI.md (11 rules incl. Rule-11 LLM-adoption standard), roadmap.md, 
gopi_design_concern.txt (pi-mono comparison; P0/P1 status), 
code-review-2026-08-03.md (5 findings — all since fixed),
code-inventory-2026-08-27.md (54-file read layer, memory #924–#979).
