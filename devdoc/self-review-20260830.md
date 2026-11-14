<!-- doc-class: normative -->
# Self Code Review Report — master 8e1342a (2026-08-30)

Reviewer: GoPi session (feat/self-review worktree @ 8e1342a). Read-only review
per Epic: findings filed as `task_submit` bug tickets (tags=bug+severity), **no
product code changed**. The only file added on this branch is
`internal/tools/repro_review_test.go` — the isolation-proof repro test the
bug-flow contract asks for.

## 1. Scope & statistics

| Area (Epic item) | Files reviewed | LOC |
|---|---|---|
| a) internal/tools memory/brew 三件 | memory_brew.go, memory_brew_llm.go, memory_brew_run.go (+ memory.go dedup/similarity) | 258+330+239 (+relevant parts of 3055) |
| a) internal/tools tasks.go + M4.5 companions | tasks.go (full), taskheartbeat.go, tasknotify.go (core) | 1812 + 167 + 321 |
| b) internal/agent/loop.go | loop.go (full 3693) + context.go (WireMessages / RemoveToolPair / EstimateTokens) | 3693 + ~450 |
| c) cmd/agent/cli_web.go | cli_web.go (full 1077) + commands.go dispatch | 1077 + ~210 |
| d) internal/deepseek/ | types.go, retry.go (full), claude_client.go (wire build, SSE parse, retryDo, stream lifecycle) | 154 + 218 + 1061 |

Deliberately excluded (per Epic): style/format nits, already-fixed findings from
devdoc/code-review-2026-08-28.md / the 08-03 round (edit.go atomic write,
withTimeout consolidation, parseTTF bounds, escapeQuery mixing, etc. — all
confirmed still fixed, not re-reported).

## 2. Findings → bug tickets (6)

| # | Task ID | Sev | One-liner | Root cause anchor |
|---|---|---|---|---|
| 1 | t-1788053263 | **major** | `--skip-tag` head-blocking: daemon sees fake-empty queue | tasks.go:1556-1560 `return Empty` instead of continue |
| 2 | t-1788053263-1 | **major** | legacy CHECK rebuild migration permanently loses FTS sync triggers → migrated rows vanish from tasks_fts/tasks_tri | tasks.go:386-390 RENAME rewrites trigger bodies + :452-458 no 'rebuild' rescue |
| 3 | t-1788053264 | **major** | `gopi web` default binds 0.0.0.0 and prints LAN URLs while docs claim loopback-only (no-approval tool execution exposed) | cli_web.go webDefaultAddrValue + webDisplayURLs |
| 4 | t-1788053264-1 | minor | "exit-check" progressing warning fires every round (comment says exit-only) — stderr spam + misleading wording | loop.go runEngine, no exit-state guard |
| 5 | t-1788053264-2 | minor | isNonZeroExit hard-codes {1,2,128,127,126,255,11,137} — exit=3/42/134/143 not recognized as dead ends | loop.go isNonZeroExit substring list |
| 6 | t-1788053264-3 | minor | retry.go debugLog: init-time GOPI_SNAPSHOT + `!= ""` → "off" counts as on (the exact boolEnvTrue regression, unfixed here) and settings debug=true can never reach it | deepseek/retry.go:86 |

Every ticket carries the 5 mandatory elements (复现 / 现象 / 根因假设定位文件行号 /
隔离证明 / 验收标准).

## 3. Findings investigated and cleared (not bugs)

- **brewGuardBlocked topic-key drift** (memory_brew_run.go:208-236): the guard
  checks exact key as bare `question=?` vs the full stored column while
  MemoryStore dedups on `substr(question,1,200) AND topic` — looked like the
  m4-acceptance bug resurrected. Isolation test
  `TestReproBrewGuardTopicMiss` shows the **similarity screen (trigram
  Jaccard)** catches the twin, so no second row is inserted. Verified-OK;
  noted here so the asymmetry is known (it stays a fragility, not a defect).
- **`task next` removed (M4.5)** — initial repro attempt targeted it; the
  wiring is `task run next` / `task serve` → same `TaskNextFiltered`, ticket 1
  retargeted accordingly.
- **cli_web XSS**: the page renders every dynamic string via
  `textContent`/`createElement` only (`div.textContent`, `pre.textContent`,
  tool-card bodies); deltas are JSON-framed SSE. `innerHTML` appears only with
  `=''` clears. No XSS sink found.
- **slash commands**: registry dispatch (no `os/exec`); unknown command →
  error frame. Clean.
- **SSE stream lifecycle (claude_client.go:911)**: unbuffered `ch` +
  context-aware sends (`select { case ch <- …; case <-ctx.Done() }`); chatOnce
  drains events before returning. No stuck-goroutine hazard found on the
  abort path.
- **omitempty traps (d)**: the three known traps are all explicitly guarded
  and documented as live-verified — `input` map default (`:608-615`), empty
  thinking block drop (`:572-580`), thinking-mode `" "` placeholder
  (`:586-597`). types.go `ToolCall` RawArgs fragment tolerance correct.
- **thinking strip boundary (b)**: wire-only strip in `WireMessages`
  (context.go:254-267) keeps ReasoningContent in memory; tool-use rounds get
  the placeholder block only when thinking is enabled on the current request
  (`:589`). Budget-abort re-issue path (chatOnce) resets streaks correctly.
- **taskheartbeat touch fallback**: second UPDATE's `task_id=?` predicate
  makes it a strict subset of the first UPDATE — dead branch, not a
  correctness bug (harmless), mentioned in the report only.

## 4. Verification

- Build: `go build ./...` → **OK** (exit 0, no output)
- Tests: `go test ./internal/tools/ -run 'TestReproSkipTagHeadBlocking|TestReproBrewGuardTopicMiss'`
  → head-blocking repro **FAIL (bug reproduced)**, brew-guard **PASS**
  (finding cleared) — exactly as reported in tickets 1 and §3.
- Package sanity: `go vet ./internal/tools/` → clean.
- Branch: `feat/self-review` @ (this commit), based on 8e1342a. Deliverable =
  report + repro test + 6 ledger tickets (t-1788053263, -1; t-1788053264,
  -1, -2, -3). No product code modified (`git diff 8e1342a --stat` =
  devdoc + repro test only).

## 5. code_search adoption self-report (Epic ②)

Environment reality: the repo's pre-indexed ctags database is broken in this
worktree (ctags exits 1 — "Unknown language Rust/TypeScript, --map-JavaScript"
at DB build), and `rg` is absent from PATH, so keyword_search also cannot run.
Both failures are logged via context_trim in-session.

| Channel | Calls this session |
|---|---|
| code_search (definition lookups) | **4** — MemoryStore ×2, findSimilarDuplicate ×2 (2 calls errored → tool-internal ripgrep fallback; 2 would have returned) |
| keyword_search (full-text) | 1 call attempted → errored (rg missing) |
| bash grep (fallback, counted) | **2** — `grep MemoryStore/findSimilarDuplicate/dedupQuestionRunes` (after both search tools errored), `grep TaskNextFiltered/skip-tag wiring` |
| keyword_search-style full-text via bash | ~10 additional `grep -n` line-lookups inside already-identified files (line-window reads, not symbol-definition searches) |

Adoption posture: every symbol-definition lookup was first attempted via
code_search (the Epic's workflow anchor held); bash grep was used only as the
documented fallback after the tool errored, and for in-file line-window
navigation where code_search is not the right instrument. PA can verify from
the session JSONL (tool_use sequence: code_search errors → context_trim →
bash).

## 6. Not Done / deferred

- No fixes of any filed bug (Epic: report-only; fixes go through the queue).
- tasknotify.go reviewed core-only (config loader + payload); delivery
  (webhook/cmd) internals skimmed, not line-audited.
- taskingest.go not audited (Epic named heartbeat/notify as the M4.5 hot
  trio; ingest is CLI-side and unchanged this window — flagged here honestly).
- The FTS-migration bug (ticket 2) root cause is experiment+code-trace proven
  on sqlite 3.37.2; a Go-level regression test is specified in the ticket's
  acceptance criteria but not written (no-fix mandate).
