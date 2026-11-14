<!-- doc-class: historical -->
# CLI Code Review: `gopi memory_*` Subcommand Family

> Date: 2026-09-02 · Branch: `review/cli-memory` · Method: full source review of
> `cmd/agent/cli_memory.go` + engines in `internal/tools/memory.go` (3107 lines) +
> docs (GOPI.md, reports/archive/2026-08/MEMORY-CLI-REPORT.md, reports/archive/2026-08/MEMORY-UPDATE-TOOL-REPORT.md, FEEDBACK-AUDIT-REPORT.md)
> + git history + live smoke tests against a scratch DB (`GOPI_MEMORY_DB`).
> Scope: the 7 advertised verbs (`memory_search/store/get/update/delete/feedback/feedback_list`).
> Review only — no code changes.

## Executive Summary

The memory CLI family is **architecturally sound**: every verb dispatches to the
same exported engine functions the LLM tool loop uses (`tools.MemorySearch/Store/
Get/Update/Delete/Feedback` + `FeedbackHistory/SuggestCold/Recompute`), so CLI
behavior == tool behavior by construction. `--db PATH` plumbing is consistent
(env-var bridge `GOPI_MEMORY_DB`, same single-source policy as the engine),
error/exit-code discipline is sane (stderr + exit 1, usage + exit 0), constants
are named (Rule 1 mostly respected), and the feedback layer's append-only
design is coherent with the "records are permanent" iron rule.

But the family has **one P0 user-facing defect and a family of stale-ad copy**:
`gopi memory_feedback_list` — advertised in top-level help (`internal/config/help.go:216`)
and guarded by a test (`cmd/agent/usage_test.go:19`) — is **NOT a real subcommand**.
`isMemorySubcommand` only matches `memory_feedback`, so `gopi memory_feedback_list`
falls through to the main agent loop and **starts a live agent run** (verified:
it printed "loaded GOPI.md", spawned a session, and blocked on stdin until the
bash timeout killed it). The audit surface was renamed to
`memory_feedback --list` (commit 682fadc) but the old name was left advertised.

## Per-Item Findings

### 1. `isMemorySubcommand` dispatch table (cli_memory.go:75-88) — P0

**Design**: hand-rolled switch over `memory_search/store/get/update/delete/feedback`,
run before the general `-h` scan in main.go so `gopi memory_X -h` shows the
family usage. Pure parser (`parseMemoryArgs`) + thin dispatcher (`runMemorySubcommand`)
with os.Setenv + os.Exit — unit-testable, matching the family's other CLIs.

**Issues**:
- **P0 `memory_feedback_list` is missing from the dispatch table** while still
  advertised in `help.go:216` and `usage_test.go:19`. `gopi memory_feedback_list`
  → not matched → treated as a task prompt → spawns a live agent run
  (observed: loaded GOPI.md, created `~/.gopi/sessions/…jsonl`, blocked on stdin).
  Worse: it is not even a no-op — it has real side effects (orphan sessions,
  possible memory writes). The intended audit verb is `memory_feedback --list`
  (682fadc rename of the P0-era `memory_feedback_list`).
- Suggestion: add `memory_feedback_list` as an alias for `memory_feedback --list`
  in `isMemorySubcommand` (or remove the dead advert from help.go + usage_test).
  At minimum, make unknown `memory_*` first args print the family usage with a
  "no such subcommand" error instead of falling into the agent loop — a
  hard-coded allow-list of real verbs that errors on anything else is safer than
  silent fallthrough.

### 2. Top-level help (`internal/config/help.go:200-217`) — P0/P1 drift

**Issues** (all verified against the real verbs):
- `gopi memory_feedback_list [--memory-id N]` — **dead verb** (see #1).
- `gopi memory_feedback <id> --useful|--useless [--reason "text"]` — **omits
  `--unused`** (a real rating the engine accepts) and the real `--task`/`--db`
  flags; says "one vote per session" but the engine dedups per **record per
  task** (`task_id`), not per session.
- `gopi memory_feedback_list` claims "Audit the feedback ledger (every vote,
  newest first)" — but `FeedbackHistory` (the engine behind `memory_feedback
  --list`) queries only the **materialized `distilled_qa.useful_count/
  useless_count` aggregate columns**, never the append-only `memory_feedback`
  event table, never reasons, never per-vote rows, newest-first. So even the
  renamed verb under-delivers vs. its own help copy ("every vote … newest
  first" is false: it's aggregated per-record, LIMIT 50, no reasons).
- `gopi memory_get <id>` / `memory_delete <id>` omit `[--db PATH]` from the
  top-level advert while the per-verb usage (cli_memory.go memoryUsage) has it —
  minor inconsistency; every verb actually accepts `--db`.
- Top-level `memory_feedback` advert also fails to mention the `--list /
  --suggest-cold / --recompute` audit modes (they live only in the verbose
  per-family usage).

**Verification**: usage_test.go only checks the top-level usage *string contains*
each advertised sub-name — it cannot catch a name that is advertised but not
dispatchable. The test gives false confidence for exactly this bug class.

### 3. `memory_search` — engine faithful, one description over-promise (P1)

**Design**: CLI → `tools.MemorySearch`; `--limit` default 5 (memoryDefaultLimit),
`--intent` routing hint, tokens=/fb= signals in output for low-quality spotting.

**Issues**:
- loop.go:930 tool description says **"cold-tier rows excluded"** — false.
  The engine's cold contract (memory.go:1123-1143) is: cold fills slots only
  when **no** warm/hot row matches (verified live: a cold-only DB returns the
  cold row). "Excluded" overstates; the LLM is told cold rows never surface,
  but they can. The engine itself is correct and matches cli_memory.go's usage
  copy ("no longer 1st tier to return … only fills slots warm/hot rows cannot")
  — the drift is in the LLM-facing description only.
- Multi-route recall (exact FTS5 AND / trigram CJK / LIKE / ASCII OR fallback)
  is well-documented and constant-driven — good.
- `escapeQuery` mixed CJK/ASCII token quoting and the brittle exact-AND recall
  were previously flagged (memory #216, #1022); the current code now has the
  `escapeQueryOR` partial-recall fallback — **fixed since that review**, no
  regression found.

### 4. `memory_store` — dedup + garbage gate faithful (OK)

- CLI enforces the topic/question/answer trio mirroring `validateToolArgs`;
  engine re-validates + garbage-gates (`ErrGarbageRefused`, quality bounds
  minQuestionLen=6 etc. as named constants). Verified live: second identical
  store prints "Memory stored (id=1, deduped into an existing entry).".
- **Rule 2 note**: `memoryDBPath()` single-source chain is
  `GOPI_MEMORY_DB > GOPI_HIPPO_DB (legacy) > ~/.gopi/memory.db` — but the
  settings.json `memory_db` key and `--db` top-level flag also write
  `GOPI_MEMORY_DB` via config/env.go:180, so an env var set at process start
  by config **is** the same channel. CLI `--db` = `os.Setenv` before dispatch =
  the same single policy. **Consistent** — good.
- **Redundant legacy alias**: `GOPI_HIPPO_DB` (memory.go:50) is a second path
  into the same decision, documented as "superseded" in settings.go:148 — a
  mild Rule-2 smell (two env knobs for one concept); acceptable as a compat
  alias, but worth a deprecation date.

### 5. `memory_get` — faithful (OK)

`MemoryGet` scans through the same `scanHit`/`renderMemorySearchResult` path as
search (rank NULL), so output format matches search hits exactly — good
consistency. Verified: exit 0 + record; nonexistent id → stderr + exit 1.
Magic-number check: the memoryUsage default-limit "5" is duplicated in prose
but the code single-sources `memoryDefaultLimit = 5`.

### 6. `memory_update` — faithful, keywords semantics subtle (P2)

- CLI fields → `MemoryUpdateFields`; the `--keywords ""` empty-clear path
  (`fields.Keywords = []string{}` when explicitly provided but empty) is
  handled distinctly from "not provided" (nil) — correct, matches engine.
- Keyword replacement also rewrites the `keywords:` answer tail
  (`stripKeywordsTail`) so stale breadcrumbs never linger — good.
- Verified: `--tier cold` on a live DB; tier demotion works, subsequent search
  reflects the cold bucket.
- P2: `memory_update`'s "nothing to update" check and field-set duplication
  between CLI (cli_memory.go ~330) and engine (memory.go:2381) is double
  maintenance — the CLI cannot fully mirror the engine's nil-vs-empty
  confidence semantics without drift risk. Low priority (tests pin both).

### 7. `memory_delete` — append-only iron rule: **correctly bounded** (OK, P2 note)

**Design**: CLI-only (no LLM tool — loop.go has no memory_delete case),
explicitly a maintenance verb. Engine `MemoryDelete` does a hard
`DELETE FROM distilled_qa WHERE id=?` + explicit FTS/tri cleanup when the DB
lacks keep-in-sync triggers.

**Assessment**: the memory-family "records are permanent" iron rule is about
the **feedback/evolution layer** (votes are append-only events; tier/confidence
changes are manual and reversible). Delete is a separate, deliberate
maintenance surface (removing garbage / accidental private data), gated by
existence-check + exit-1 on missing id, and documented as such in the usage
("Permanently remove one record"). **It does not violate the rule** — but the
rule's boundary should be stated in one place (see #10): the *feedback* layer
is append-only (delete would break the audit trail: FeedbackRecompute replays
events onto `distilled_qa`, and a deleted record's votes would be dropped by
the JOIN — FeedbackHistory never reads the event table, so no cross-check
exists to detect a delete-then-recompute inconsistency). P2: consider soft
delete (tier=cold + deleted_at) for CLI deletes to preserve the event trail,
or document the trade-off.

### 8. `memory_feedback` — vote semantics: **one real drift + doc over-promises** (P1)

**Engine (memory.go:2613-2746)** — verified sound:
- Rating validated against useful/useless/unused; append-only event INSERT +
  EMA materialized-column update + confidence step in ONE transaction;
  90-day decay (`fbDecayFactor`); task-id dedup "one vote per record per task";
  only-up/never-auto-down (cold is suggestion only via `--suggest-cold`);
  FeedbackRecompute replays events (pure function) — coherent.
- The feedback event table is separate from `distilled_qa`; schema is
  PRAGMA-probed ALTER-healed on first vote (idempotent, concurrent-safe fix in
  d1d34b6).

**Issues**:
- **loop.go:1011 tool description drift (P1)**: "each moves the score ≤0.2;
  ≥2 useless votes auto-demote to cold (reversible)". Both claims are stale:
  (a) the score term `fbScore` max is |0.5| (fbWeight=1.0, gate→1, mean 0→1
  gives ±0.5; with prior 1.0 the attainable max is ~0.39 anyway) — never 0.2;
  (b) auto-demotion was **removed in the merged design** (682fadc): 69bdc1d had
  `coldVotesNeeded=2` + `coldScoreThreshold` automatic demotion; the merged P0
  (682fadc) replaced it with suggestion-only `--suggest-cold`. The loop.go
  description still promises the dead automatic behavior. Also "the harness
  then drops that search's tool_use + tool_result pair from context
  immediately" when voting useless — **no such code path exists** (the actual
  mechanism is the whitelist slim, keyed on record-id useful-votes, not on a
  useless tool_use_id; see #9). A model following this description believes it
  is triggering context removal that never happens.
- **CLI-vs-tool rating surface**: cli_memory.go usage lists
  `--useful|--useless|--unused` but top-level help lists only
  `--useful|--useless` — drift within the family's own help (see #2).
- The CLI passes `task=""` to `MemoryFeedback` → bypasses dedup (documented:
  "CLI maintenance channel is always recorded"). Fine — but help.go's "one
  vote per session" wording conflates the tool channel (task-deduped) with the
  CLI channel (always recorded).

### 9. `memory_feedback --list` under-delivers its own copy (P1)

- `FeedbackHistory(memoryID)` renders only aggregated `useful_count/useless_
  count` per record (materialized columns), LIMIT 50, no reasons, no per-vote
  rows, no dates — despite help.go advertising "Audit the feedback ledger
  (every vote, newest first)" and its own comment saying "renders one record's
  full feedback audit trail". The append-only `memory_feedback` event table
  (which has memory_id/rating/reason/task_id/created_at) is never read by any
  CLI verb — the audit surface cannot show the audit trail. P1: either read
  the event table for a true per-vote audit, or fix the copy.
- `--memory-id` filter works (id-exists check) — OK.
- Note: even the LLM tool surface (`memory_feedback`) never exposes `--list`;
  list is CLI-only. No loop.go tool exists for it (correct — it's a
  maintenance surface).

### 10. Docs & cross-surface drift — P1/P2

- **GOPI.md:46 says "18 tools"** — actual `claudeToolDefs` count is **20**
  (read, git_blame, write, edit, bash, code_search, keyword_search, web_search,
  web_fetch, memory_search, memory_store, memory_feedback, memory_update,
  context_trim, job_start, job_output, job_kill, todo, vision_analyze,
  text2img). Same stale claim was flagged in the 2026-08-28 review (#1022) and
  never fixed. GOPI.md:46 is the number a reader uses to sanity-check the tool
  list — wrong by 2.
- **GOPI.md:46 also lists `memory_delete` nowhere** (correct — it is CLI-only),
  but `memory_search/memory_store/memory_feedback/memory_update` are LLM tools
  and the CLI family adds `get/delete` — GOPI.md never mentions the CLI family
  exists. P2 doc gap.
- **Append-only iron rule is not stated in the memory docs**: MEMORY-CLI-REPORT /
  MEMORY-UPDATE-TOOL-REPORT never define whether delete is allowed or bounded;
  the rule appears only inside code comments (task_events "records are
  permanent, no delete (nick's iron rule)" in help.go — which is about the
  **task** ledger). The memory CLI family's own delete boundary should be
  documented next to that rule. P2.
- **`memory_feedback_list` in usage_test.go**: the test that should have caught
  the P0 is itself the carrier of the dead advert. Fix the test list when the
  verb is resolved.
- cli_memory.go's verbose usage is the most current, complete surface doc
  (has `--unused`, `--task`, `--list/--suggest-cold/--recompute`, `--db`
  everywhere). Top-level help.go lags it. Recommend generating the top-level
  subcommand block from the per-verb usage or adding a lint that asserts
  `isMemorySubcommand` covers every advertised name.

### 11. Exit codes / error messages (OK)

Verified live: usage errors → stderr `gopi <verb>: <msg>` + usage + exit 1;
engine errors → stderr + exit 1; success → stdout + exit 0; `-h` → usage +
exit 0. Missing-id / bad-id / unknown-flag / cross-subcommand-flag cases all
covered in cli_memory_test.go (parse-level unit tests) and pass
(`go test ./cmd/agent -run TestParseMemory|TestCLI` — 5.7s green). One nit:
`memory_store` missing-required error capitalizes "Missing required parameter"
while parse errors elsewhere lowercase — cosmetic, matches tool-arg style.

### 12. Dead-verb root cause (git archaeology)

- 69bdc1d (P0 feedback, 2026-08-25): introduced `memory_feedback` + a separate
  `memory_feedback_list` CLI + **automatic** cold demotion (coldVotesNeeded=2,
  reversible coldReversalScore).
- 682fadc (merged P0 design): renamed the audit verb to `memory_feedback
  --list/--suggest-cold/--recompute`, **dropped auto-demotion** for
  suggestion-only. But help.go:216 + usage_test.go:19 kept advertising
  `memory_feedback_list`, and loop.go:1011's description kept promising
  "≥2 useless auto-demote". Rename was done in the code, not in the docs the
  code ships. This is the accumulated-patch / design-reversal signature the
  epic asked to find — exactly the kind of thing a later feature (M2
  confidence lifecycle 6669e41, procedural layer c452d16) never re-checked.

## Prioritized Fix List

### P0
1. **`memory_feedback_list` must not silently start an agent run.** Either
   dispatch it as an alias of `memory_feedback --list` (one line in
   `isMemorySubcommand`) or make unknown `memory_*` args print the family
   usage + error. Currently it advertises (help.go), is test-guarded
   (usage_test.go:19), and falls through to a live task run with real side
   effects (orphan sessions, potential memory writes).

### P1
2. **loop.go:1011 `memory_feedback` description**: remove "each moves the
   score ≤0.2" (actual max ±0.5, typically ≤0.39) and "≥2 useless votes
   auto-demote to cold (reversible)" (auto-demotion removed in 682fadc;
   now suggestion-only) and "the harness then drops that search pair from
   context immediately" (no useless-vote context-drop path exists).
3. **loop.go:930 `memory_search` description**: "cold-tier rows excluded" →
   "cold-tier rows only surface when no warm/hot row matches" (engine cold-fill
   contract).
4. **help.go:200-217 top-level advert**: drop/alias `memory_feedback_list`;
   add `--unused` (and mention `--task`) to the `memory_feedback` line; fix
   "one vote per session" → "one vote per record per task (tool channel)".
5. **`memory_feedback --list` copy vs behavior**: either query the append-only
   `memory_feedback` event table for a true per-vote, reason-bearing,
   newest-first audit (matching its own comment/help), or soften the copy.
6. **usage_test.go:19**: add a guard that every advertised `memory_*` name is
   dispatchable by `isMemorySubcommand` (the current contains-check cannot
   catch an advertised-but-undispatched verb — the exact P0 bug).

### P2
7. **GOPI.md:46 "18 tools" → 20** (and add the CLI family mention); the same
   stale count was flagged 2026-08-28 (#1022) and is still wrong.
8. **Rule-2**: retire or date the `GOPI_HIPPO_DB` legacy alias — two env knobs
   for one path decision.
9. Document the memory-family delete boundary next to the append-only iron
   rule (delete is bounded maintenance; the feedback event trail is the
   permanent layer — a delete-then-FeedbackRecompute drops the deleted row's
   votes from the materialized view without detection).
10. `memory_update`'s field-set/nil-vs-empty validation is duplicated between
    CLI and engine — add a shared validator or a cross-test that pins the two
    stay in lockstep.

## Verification

- Build: `go build ./...` OK; `go vet ./cmd/agent ./internal/tools` clean.
- Tests: `go test ./cmd/agent -run 'TestParseMemory|TestCLI'` — ok (5.7s);
  cmd/agent package suite green.
- Live smoke (scratch DB via `GOPI_MEMORY_DB=/tmp/review-mem.db`):
  store→dedup signal, store distinct→feedback useless (confidence_step=−0.10,
  fb_score=−0.042) → `--list` shows aggregated row → `--tier cold` demotes →
  search returns cold row **only when it is the sole match** (cold-fill
  contract confirmed) → delete removes row, subsequent get errors exit 1.
- P0 repro: `gopi memory_feedback_list` (no args) → NOT the family usage; it
  printed "loaded GOPI.md", created a session file, and blocked on stdin
  (killed after timeout; orphan session/raw logs cleaned up by reviewer).
- Tool count: `grep -c 'Name: "' internal/agent/loop.go` = 20 vs GOPI.md's 18.

## Not Done (scope)

- No code changes (review task). Fix list is advisory; the P0/P1 items are
  one-line to a few-line patches when scheduled.
- Did not run the full agent test suite (internal/agent ~12s + internal/tools
  ~87s incl. real-timeout regression tests) — targeted package tests only,
  since no code was changed.
- Delete-append-only trade-off (P2 #9) left as a design question, not resolved.
