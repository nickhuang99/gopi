<!-- doc-class: historical -->
# Task Report: Hippocampus core loop (epic child 2/4) — `internal/hippocampus`

Spec: `devdoc/hippocampus.md` (§1.1 N2, §1.2 shape precedent, §6 AGREED INTERFACE).
Worktree: `~/workspace/gopi-hippo-core`, branch `feat/hippocampus-core`.

## Completed

### 1. Structured retrieval exit (`internal/tools/memory.go`)

- **NEW `MemorySearchHits(query string, limit int, intent string) ([]MemoryHit, error)`**
  (spec N2): the multi-route recall (FTS5 exact → ASCII-OR → trigram CJK →
  LIKE 2-char CJK) plus the value-signal rescore now return `[]MemoryHit`
  instead of rendered text. The hippocampus needs the hits themselves — ids
  for the write verbs, `Tier`/`Confidence` for the decision — and parsing the
  rendered text back into structure would be fragile (the spec calls this out
  explicitly).
- **`MemorySearch` is now the rendering wrapper** around it: it keeps the
  input validation, the `bumpRetrievalCounts` re-verification write, the JSON
  marshal → `renderMemorySearchResult` render, and the procedural-workflow
  merge. Both surfaces share every recall route, so they cannot drift.
- **Exported rating constants** `tools.RatingUseful` / `RatingUseless` /
  `RatingUnused` (the unexported `ratingUseful` … now alias them), so an
  in-process caller never types the wire string inline (Rule 1b/13).

### 2. New leaf package `internal/hippocampus/`

| File | Contents |
|---|---|
| `config.go` | All limits/budgets as named constants (`DefaultMaxRounds=3`, `DefaultRetrievalLimit=5`, `DefaultMaxAct=2`, `DefaultTimeout=20s`, `DefaultClientTimeout=10s`), `Config`/`DefaultConfig`, and **`LoadConfig` — the ONE reader** of `GOPI_HIPPOCAMPUS_MAX_ROUNDS/_RETRIEVAL_LIMIT/_MAX_ACT/_TIMEOUT_SEC/_CLIENT_TIMEOUT_SEC` (Rule 2). |
| `types.go` | The five verb constants (`VerbNone/Store/Fetch/Update/Feedback`), `DecisionVerbs`, `SourceTag`, `RetrievalIntent`, the seven `Stop*` reasons, `Request`, `Action`, `Result`. |
| `decision.go` | `Decision` — the Pro-style step's flat JSON contract (one verb + its fields). |
| `engine.go` | The bounded `retrieve → judge → act` cycle: `Client`/`Memory` interfaces, `ToolsMemory` (in-process adapter), `Run`, prompts, `callJSON`/`extractJSONObject`, pure context-digestion helpers. |

**The cycle (bounded by construction):**

1. **Flash-style divergent retrieval** — one `Client.Chat` turns the task
   context into 1–3 retrieval angles (`{"queries":[…]}`); the context tail
   (last `retrievalWords` words) is what the prompt weights highest.
2. **Fan-out** — each angle hits `Memory.Search` = `tools.MemorySearchHits`;
   hits are unioned, deduped by id, capped at `RetrievalLimit`. A failed
   recall is recorded and stops the cycle.
3. **Pro-style decision** — one `Client.Chat` emits EXACTLY ONE JSON action.
   An illegal verb is a recorded failure, not a silent no-op.
4. **Act** — the verb is EXECUTED through the in-process memory functions:
   `none` (no write), `store` → `tools.MemoryStore`, `fetch` → `MemoryGet`,
   `update` → `MemoryUpdate`, `feedback` → `MemoryFeedback`. No tool
   round-trip, no new top-level tool.

**Stop paths** (`Result.StopReason`, one constant each): `none_emitted`,
`round_limit`, `act_budget`, `timeout`, `no_query`, `empty_search`,
`client_error`, `memory_error`. `Run` ALWAYS returns a non-nil `Result`
(partial on timeout) and records every internal failure on `Result.Err`
(Rule 15) — a nil client/memory is the only caller-side error.

## Not Done (honest gaps)

- **No host wiring.** This child is the core loop only. Nothing calls
  `Engine.Run` yet: the delivery levels Lv-0…Lv-3, the round-boundary
  injection point (`loop.go:2729`), the `maybeInjectTaskStartSearch`
  replacement and the session-log event are epic child 3.
- **No real (non-stub) LLM adapter** — that constructor (`deepseek.Client` →
  `hippocampus.Client`, Flash thinking-disabled + GLM effort fallback) is
  host-side work; the interface is deliberately the same shape as
  `tools.brewClient` so the adapter is a copy of `brewFlashClient`.
- **`MaxLv` / queue / Lv-3 quota knobs from spec §5 are NOT implemented** —
  they belong to child 3 (they gate delivery, not recall) and are unused
  infrastructure until then (spec §8.1 makes the same call).
- **`internal/hippocampus` → `internal/tools` import direction is unguarded
  by a test.** The package comment states the rule (tools must never import
  hippocampus); a Go import-cycle would fail the build anyway, so no extra
  gate was added.
- Async/background retrieval (spec §8.1) — deliberately out of scope.
- One pre-existing failure mode worth naming: `Time.now()`-based staleness in
  the memory engine is untouched here, so a `possibly_stale` lead can still
  rank low; the hippocampus does not second-guess the engine's ranking.

## Verification

- Build: `go build ./...` → **OK**
- `go vet ./internal/hippocampus/ ./internal/tools/` → **OK**
- `gofmt -l` on both touched trees → **clean**
- `go test ./internal/hippocampus/ -count=1` → **PASS** (21 tests: 15 engine + 6 config)

Acceptance mapping (all with the stub client, zero network):

| Acceptance | Test |
|---|---|
| (a) retrieval returns leads from a seeded DB | `TestRunRetrievalReturnsSeededLeads` |
| (b) each verb mutates the DB, read back | `TestVerbStoreWritesRecordReadBack`, `TestVerbFetchReadsRecordReadBack`, `TestVerbUpdateMutatesRecordReadBack`, `TestVerbFeedbackRecordsVoteReadBack`, `TestVerbNoneMutationless` |
| (b′) refusals are recorded, never swallowed | `TestIllegalAndRefusedVerbsAreRecorded` (illegal verb / bad rating / incomplete store) |
| (c) stub-only determinism | `TestStubOnlyDeterminism`, `TestClientFailureStopsCycle` (exactly 1 call — no retry loop) |
| (d) bounded: stops on budget/timeout, never loops | `TestRoundBudgetStopsCycle`, `TestActBudgetStopsCycle`, `TestTimeoutReturnsPartialResult` (partial result, no spin), `TestCancelledContextStopsCycle`, `TestEmptyContextStopsWithoutCallingTheModel` |
| Rule 1/2/13 knobs | `TestDefaultsAreTheDocumentedConstants`, `TestLoadConfigEnvOverridesAndFallsBack`, `TestHippocampusEnvHasOneReader` (the env prefix may appear in `config.go` only) |
| Rule 1b protocol | `TestDecisionVerbsAreTheFiveConstants`, `TestExtractJSONObject` (prose-wrapped + size cap), `TestLastWordsDigest` |
| Rule 3 (no new tool) | `claudeToolDefs` untouched; `TestGOPIRule3_MatchesClaudeToolDefs` in the full run below |
| regression | `go test ./internal/tools/ -run 'TestMemory|TestFb|TestFeedback'` green after the `MemorySearch` refactor |

### Full-suite run (branch `feat/hippocampus-core` @ 7ff8a7a)

`go test $(go list ./... | grep -v '/internal/tools$') -count=1` → **all green**:

```
ok  github.com/nick/gopi/cmd/agent        252.129s   (includes TestGOPIRule3_MatchesClaudeToolDefs)
ok  github.com/nick/gopi/internal/agent   174.542s
ok  github.com/nick/gopi/internal/config    0.122s
ok  github.com/nick/gopi/internal/deepseek  2.058s
ok  github.com/nick/gopi/internal/gopilog   2.135s
ok  github.com/nick/gopi/internal/hippocampus 30.408s   ← this change
ok  github.com/nick/gopi/internal/proxy     0.013s
ok  github.com/nick/gopi/internal/shot      1.011s
ok  github.com/nick/gopi/internal/text2img  0.508s
ok  github.com/nick/gopi/testsuite          0.760s
```

`go test ./internal/tools/ -count=1 -timeout 600s -run 'TestGOPIRule3_MatchesClaudeToolDefs|TestMemory|TestFb|TestFeedback'`
→ **ok … 129.020s** (the whole memory/feedback surface that the
`MemorySearch` refactor touches, plus the Rule-3 gate).

**Honest gap — the FULL `./internal/tools/` package run cannot complete on
this repo, for a PRE-EXISTING reason that is not this change.** The package
has a known full-suite hang (`database/sql` connectionOpener deadlock left by
a memory-sync e2e; bug ticket `t-1788361069`, fix epic
`feat/fix-internal-tools-suite-hang`) — it trips even SOLO at
`-timeout 1800s`, with total test CPU ≈15s: blocked, not slow. Observed here
identically on untouched master `aafa7ce` (FAIL 120s inside
`TestMemoryFeedbackRankingIntegration`) and on this branch (FAIL 150s inside
`TestM1KwLabelRecall`) — different tests each time, i.e. the poisoned-suite
symptom, not a deterministic failure introduced by this branch.

This change DOES touch that package (`internal/tools/memory.go`:
`MemorySearchHits` + the `MemorySearch` refactor + exported `Rating*`), so
"zero edits" exoneration does not apply — explicit coverage was run instead:
every memory/feedback test plus the Rule-3 gate, **129.0s, ok** (row above).
The untouched-hang rule stands: the whole-package run is left to a quiet box
or CI, and the ticketed deadlock is the thing to fix, not a reason to block
this child.

## Design notes for child 3

- **`Result` carries no level.** Per ontology §2.2 the delivery level is the
  SCHEDULER's decision — the organ emits raw signals (hits + actions +
  stop reason) and the host frames them (`【海马体备忘】` etc.).
- **`Engine.Run` takes a `Request{Context, TaskID, SessionID, Round}`** — the
  organ never probes agent state itself; the host hands the context in.
  `TaskID`/`SessionID` become keyword breadcrumbs on stored rows and the vote
  provenance, so the audit trail names the origin.
- **Test seam**: `Memory` is an interface (`ToolsMemory` is the production
  adapter), so child 3 can assert the exact call sequence of a host path
  without touching a real DB; `Client` keeps every test network-free.
