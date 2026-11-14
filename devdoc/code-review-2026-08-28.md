<!-- doc-class: historical -->
# Code Review 2026-08-28 — full-pass line-by-line review

Agent: nick (opponent-branch reviewer). Scope: every non-test source file in the
repo at `master` (HEAD d88f2e4): `cmd/agent`, `cmd/text2img-verify`,
`internal/agent`, `internal/checkpoint`, `internal/config`, `internal/deepseek`,
`internal/gopilog`, `internal/proxy`, `internal/text2img`, `internal/tools`
(~21k lines of Go, 58 source files). Companion reviews in parallel in progress.

**Headline:** the codebase is in exceptionally healthy shape. The 2026-08-03
review's findings are almost all resolved (dead `ContextOverflow` removed; the
5x timeout-wrapper duplication → `withTimeout[T]` generic; tilde drift →
single `expandHome`; env-read repetition → `envInt`/`envFloat`). No new
correctness bugs found. Findings below are mostly cosmetic / maintenance /
defensive gaps, plus one environment state.

---

## CRITICAL — repository is mid-merge and does not build

`git status` shows an **unresolved merge in progress**: `internal/agent/loop.go`
contains a live conflict-marker pair (`<<<<<<< HEAD / ======= / >>>>>>>
feat/memupdate-motivate`) inside the `const` block at ~lines 122–146. As a
result **`go build ./...` fails** (`loop.go:124: syntax error: unexpected <<`).

- The conflict is **semantic** (both sides edit the same prompt-wiring consts):
  - `HEAD` side adds `deadEndTrimMessage` (the context_trim nudge) and keeps a
    longer task-start message.
  - incoming `feat/memupdate-motivate` shortens the task-start message and
    points its tail at `memory_update`.
- Resolution must **merge both concerns**: keep the task-start mentioning
  memory_update AND preserve the dead-end-trim wiring. This is the active merge
  between HEAD and `feat/memupdate-motivate`; it must be resolved and the
  branch landed before the repo is buildable.
- Review of `loop.go`'s non-conflicted body was done against the working-tree
  content; the constants in the conflict block are the only unverifiable part.

## Findings severity-ranked

### Medium

1. **`internal/text2img/render.go` — `parseTTF` table-parser lacks bounds checks
   → process panic on a corrupt/adversarial font blob.**
   `parseTTF` reads `head`/`maxp`/`hhea`/`hmtx`/`loca`/`cmap` directly at
   offsets declared by the font's table directory (`data[h+18:h+20]`, etc.)
   with **no check that `table.to + table.tl ≤ len(data)`**. Only `loca`
   format-1 validates (`lo[1] < 4*n`). A truncated/corrupt font file (any
   third-party font, downloaded font, or a corrupted system font) causes
   `slice bounds out of range` → panic → whole process dies. `renderPage` has a
   `maxRasterDim` sanity guard on glyph size, but the table parse is trusted.
   **Fix:** validate every table's `to+tl ≤ len(data)` in `parseTTF` before use,
   and skip/error instead of panic (matching the existing "CFF unsupported"
   graceful error for NotoSansCJK.ttc).

2. **`internal/tools/edit.go` — non-atomic write + hardcoded `0644`**, diverging
   from `write.go`'s crash-safe `atomicWriteFile` (temp+fsync+rename+preserve
   perms).
   `editImpl` ends with `os.WriteFile(expanded, []byte(newContent), 0644)`:
   - **Not atomic:** an interrupted edit (kill -9 mid-write) can leave a
     truncated/corrupt file at the target path — edit is the ONE mutating tool
     that isn't crash-safe.
   - **Perms reset:** an existing file with `0600` / `0700` / `0755` gets its
     mode clobbered to `0644` after the first edit — a security/permission
     regression (edit of a private key or restricted file quietly makes it
     world-readable).
   **Fix:** reuse `write.go`'s `atomicWriteFile` (temp file in the target's dir
   + chmod to existing mode + fsync + rename). edit and write should share the
   same atomic path.

3. **`internal/config` — `--keep-thinking` CLI flag is declared but non-functional**
   (settings/env channel OK; only the CLI flag is broken).
   - `settings.go:75` gives `GOPI_KEEP_THINKING` a `CLIFlag:"keep-thinking"`,
     so `FlagOverrides` accepts `--keep-thinking`.
   - But `isBoolKey("keep_thinking")` is **false** → the flag is misclassified
     as a *scalar* (requires a value) instead of a bool.
   - And `applyFlag` in `flags.go` has **no `case "keep-thinking"`** → any use
     falls through to the default and returns `unhandled flag --keep-thinking`.
   - It is also absent from `FormatUsage`, so the failure is surprising (the
     env-var table advertises a flag that errors).
   - `GOPI_KEEP_THINKING` itself works via settings.json `"keep_thinking"` and
     the env var (`env.go setBool`) — the loop reads it correctly. Only the CLI
     flag channel is dead. **Fix:** add `case "keep-thinking"` to `applyFlag`
     (sets `cfg.KeepThinking`) and add `"keep_thinking"` to `isBoolKey`; or, if
     a CLI switch is intentionally not provided, drop `CLIFlag` from the
     EnvVars row to avoid the trap.

### Low / cosmetic

4. **`internal/proxy/stream.go` — truncated upstream stream ends without a
   termination event; `sc.Err()` never checked.**
   `streamResponse`'s `for sc.Scan()` loop exits on upstream EOF/error but never
   checks `sc.Err()`. If the upstream errors mid-stream **without** emitting a
   `finish_reason` chunk, no `message_delta`/`message_stop` is sent — the client
   sees a dangling SSE stream with no terminator. Related: the upstream request
   is made with `http.DefaultClient.Do` (no context from the inbound request),
   so a client disconnect isn't propagated to upstream. Acceptable for typical
   use; worth hardening.

5. **`internal/proxy/stream.go:141` — `anyToolSeen` field is dead** (written at
   line 249, never read). Leftover state; either use it (e.g. to disambiguate
   interleaved reasoning/tool streams) or remove it.

6. **`internal/agent/compact.go` — `boundSummaryInputMax` has an 8-line dead
   recomputation.** The first `marker/remaining/headChars/tailChars` block uses
   the wrong `omitted = len(content)`, then every value is overwritten by the
   second block (identical logic with the correct `omitted`). Output is correct;
   the first block is pure waste. Delete it.

7. **Rule-1 (named constants) violations** — inline magic numbers where the
   codebase elsewhere demands named constants:
   - `internal/agent/trajectory.go:150` and `internal/agent/resume.go:61`:
     `scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)` (and
     `16*1024*1024` on resume) — inline, while `session.go`/`rawlog.go` use
     named `*SyncThreshold` constants.
   - `internal/agent/compact.go` `serializeForSummary`: inline `6000/4000/
     1500/1200` truncation limits.
   These are intentional buffer/cap values but should be named for the same
   reason the rest of the project names them.

8. **`cmd/agent/cli_checkpoint.go` `truncate()` is byte-unsafe** (`s[:n]` can
   split a UTF-8 rune). The checkpoint-list model column is usually ASCII, so
   low impact, but a CJK model name >16 runes renders mojibake. Use a rune-safe
   truncate.

9. **`internal/tools/gitblame.go` — query tool with a write side-effect**:
   `storeBlameMemory` persists into the knowledge base on every `git_blame`
   call (best-effort, documented). It is a deliberate design, but it makes a
   read-only-reasoning tool mutate persistent state. Flag for a design
   decision, not necessarily a bug.

10. **`internal/tools/memory.go` — two small nits** (low severity, model-only
    callers):
    - `escapeQuery`: a **mixed ASCII+CJK token** (e.g. `GOPI网络-info`) is
      passed bare into the FTS5 `MATCH`; if it contains an FTS operator char
      (`-`, `:`, etc.) the MATCH can break and silently return nothing. Pure-CJK
      tokens are safe (CJK ideographs are never FTS operators).
    - `MemoryStore` exact-dedup path merges only `sources`, **not new
      keywords** — re-storing the same Q+A with better breadcrumbs keeps the
      old `search_kw`. Later-daum keywords are dropped.

### Already-resolved findings (verified fixed since 2026-08-03)

- Dead `ContextOverflow` in `internal/deepseek/types.go` → **removed**;
  `EstimatedTokens`/`TrimToFit` live in `internal/agent/context.go`.
- 5x duplicated timeout wrappers (`ReadWithTimeout`/`WriteWithTimeout`/
  `EditWithTimeout`/`CodeSearchWithTimeout`/`KeywordSearchWithTimeout`) →
  **unified** into `withTimeout[T]` in `internal/tools/withtimeout.go` (typed
  convenience wrappers remain, harmless).
- `~` expansion drift across read/write/edit → **single shared `expandHome`** in
  `internal/tools/tilde.go`.
- ~14x repeated `Getenv→Atoi→warn→fallback` in the agent → **consolidated**
  into `envInt`/`envFloat` in `internal/agent/envparse.go`.

## Positive notes (high quality worth preserving)

- **`internal/tools/bash.go`** — process-group isolation so timeout SIGKILL
  takes the whole tree (bug011), idle+absolute dual timeouts, cwd whitelist
  with fail-closed hardenings, output soft/hard caps (`softOutputSize=8KB` /
  `maxOutputSize=100KB`, tail-preserving). Exceptionally careful.
- **`internal/tools/memory.go`** — embedded pure-Go SQLite (`modernc.org/
  sqlite`, zero CGO) + FTS5 trigram CJK multi-route recall, Beta-posterior
  feedback decay, garbage gate. Very mature engine.
- **`internal/agent/...`** — reasoning-effort/thinking precedence with an
  unusually explicit PA-ruling rationale; harness-enforced thinking budget
  (abort + re-issue disabled, both on the streaming and non-streaming paths);
  run-loop per-round fsync; job-notice inbox (zero polling); bug022
  consecutive-end_turn exit; native image input; `resume.go` cleanly reverses
  `BuildClaudeRequest`. `NewAgent` resolves precedence as flags > env >
  settings via `config.ApplyEnv` re-export — Rule-2 clean.
- **`internal/proxy`** — anthropic⇄openai protocol translation with careful
  comments; drops client-side thinking on the outbound path deliberately.
- **`internal/config`** — precedence (flags > env > settings > default) is
  coherent end-to-end (CLI → `cfg` → `ApplyEnv` → `os.Getenv` readers).
- Test hygiene: every package reviewed has non-trivial tests; the tools package
  even ships a real bash-timeout regression that makes `go test ./internal/...`
  take several minutes.

## Open state / notes

- **`GOPI.md` tool count (18) is stale** — the actual wired agent tool
  inventory in `loop.go` `executeToolDefault` is **20**: read, git_blame,
  write, edit, bash, code_search, keyword_search, job_start, job_output,
  job_kill, todo, web_search, web_fetch, memory_search, memory_store,
  memory_feedback, memory_update, vision_analyze, text2img, context_trim.
  (`memory_delete` is intentionally CLI-only maintenance, not a tool.)
  Update GOPI.md's tool list/count.
- Marker-constant duplication (harmless, interoperates): `checkpoint/parse.go`
  `dirRequest = "→ REQUEST\n"` vs `loop.go`/`resume.go` `rawMarkerRequest =
  "→ REQUEST"` (same prefix, both work on the raw format).
- The `read` tool (2026-08-28 build) flags `internal/text2img/render.go` and
  `cmd/agent/cli_log.go` as "binary / invalid UTF-8" — both are verified clean
  UTF-8 by `file`/`awk`; it's a false-positive in the tool's charset guard, not
  a repo defect.

## Verification

- `go build` (whole repo): **FAILS** — `internal/agent/loop.go:124` unresolved
  merge conflict (see CRITICAL). This is the in-progress merge, not a new defect.
- Per-package tests (packages not depending on the conflicted agent):
  `go test ./internal/tools ./internal/config ./internal/checkpoint` — **ALL OK**
  (tools took 205.1s because it runs real bash 120s-idle-timeout regression
  tests; config cached-ok; checkpoint 0.011s).
  `go build ./internal/config ./internal/deepseek ./internal/proxy` — **OK**
  (these compile independently of the conflicted agent package).
- Finding #3 (`--keep-thinking`) re-verified against source: `isBoolKey`
  (`settings.go:122`) has no `keep_thinking` case and `applyFlag`
  (`flags.go`) has no `case "keep-thinking"` — confirmed dead CLI flag.
- No code was modified during this review (read-only pass); the repo's
  merge-conflict state was left untouched (`git status` unchanged).
