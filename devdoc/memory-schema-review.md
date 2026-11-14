<!-- doc-class: historical -->
# Task Report: Code review of ensureSchema in internal/tools/memory.go

## Completed
- Located `ensureSchema` and verified its full DDL against source.
- Verified FTS5 table definitions (both use external-content mode) and their
  keep-in-sync triggers.
- Verified call sites and idempotency / migration behavior.

## Findings

### 1. Where is ensureSchema defined?
- `internal/tools/memory.go:285` — `func ensureSchema(ctx context.Context, db *sql.DB) error`
- Call sites: `internal/tools/memory.go:440` (inside `initKnowledgeBase`, first
  creation path) and `internal/tools/memory.go:461` (inside
  `migrateDefaultTriggers`, self-heal path for gopi's own default DB).
- Companion function `ensureFeedbackSchema` (`internal/tools/memory.go:363`)
  creates the append-only `memory_feedback` event table + EMA materialized
  columns via PRAGMA probe + `ALTER TABLE`.

### 2. What schema does ensureSchema create?
Executed in order from the `stmts` slice, all idempotent (`IF NOT EXISTS`),
followed by a best-effort `PRAGMA journal_mode=WAL` (memory.go:351, errors
deliberately ignored — busy_timeout already serializes writes):

| Line | Object | Purpose |
|---|---|---|
| 287 | `distilled_qa` (ordinary table) | base rows: id, topic, question, answer, sources, evidence, confidence, distilled_at, retrieval_count, last_retrieved, tier, search_kw, jieba_token, lesson_type, when_at |
| 304 | `distilled_qa_fts` (FTS5) | BM25 index over topic, question, answer, sources, search_kw, jieba_token — **default unicode61 tokenizer (no tokenize= clause)** |
| 313 | `distilled_qa_tri` (FTS5) | trigram index over question, answer, topic — `tokenize='trigram'`, for CJK substring recall (unicode61 treats a contiguous CJK run as one token) |
| 317/321/327 | triggers `distilled_qa_tri_ai/_au/_ad` | AFTER INSERT/UPDATE/DELETE keep `distilled_qa_tri` in sync ('delete' + re-insert pattern on update) |
| 331/335/341 | triggers `distilled_qa_ai/_au/_ad` | same for `distilled_qa_fts` |

Note: the trigram index was added later; `migrateDefaultTriggers`
(memory.go:449+) re-runs ensureSchema on old default DBs and backfills
`distilled_qa_tri` via `rebuildTrigramIndex` so pre-existing rows gain CJK
recall.

### 3. Is the FTS5 index an external-content table?
**Yes — both of them.** Verified verbatim (memory.go:304-316):

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS distilled_qa_fts USING fts5(
    topic, question, answer, sources, search_kw, jieba_token,
    content='distilled_qa', content_rowid='id'
)
CREATE VIRTUAL TABLE IF NOT EXISTS distilled_qa_tri USING fts5(
    question, answer, topic, content='distilled_qa', content_rowid='id',
    tokenize='trigram'
)
```

`content='distilled_qa'` + `content_rowid='id'` = classic external-content
FTS5 configuration: index text is read from the base table at query time, so
the index stores no duplicate text. Consequences confirmed in code:
- the ai/au/ad triggers are **mandatory** (external-content tables do not
  keep themselves in sync) — all six exist;
- delete goes through the special `INSERT INTO fts(fts, rowid, ...) VALUES
  ('delete', ...)` command — exactly what the au/ad triggers emit;
- the whole package follows this pattern: `tasks_fts`/`tasks_tri`
  (internal/tools/tasks.go:103,107) are external-content over `tasks` too.

Package-wide check: `grep "USING fts5"` over internal/tools/ finds only
external-content definitions (plus test fixtures replicating them), no
contentless (`content=''`) or regular FTS5 tables.

## Function signatures
- `func ensureSchema(ctx context.Context, db *sql.DB) error` — memory.go:285
- `func ensureFeedbackSchema(ctx context.Context, db *sql.DB) error` — memory.go:363
- `func initKnowledgeBase(ctx context.Context, path string) error` — memory.go:432 (calls ensureSchema:440)
- `func migrateDefaultTriggers(ctx context.Context, path string) error` — memory.go:449 (calls ensureSchema:461)

## Verdict
PASS. `ensureSchema` (internal/tools/memory.go:285) creates one base table
(`distilled_qa`), two FTS5 indexes (`distilled_qa_fts` unicode61,
`distilled_qa_tri` trigram) and six keep-in-sync triggers, all idempotent.
Both FTS5 indexes are external-content tables (`content='distilled_qa',
content_rowid='id'`), correctly paired with the mandatory ai/au/ad trigger
set including the ('delete', ...) tombstone pattern. Code is well-commented,
the WAL pragma failure is deliberately tolerated, and the migration path
backfills the trigram index for legacy DBs. No defects found in the schema
DDL itself. Known design gap (not a bug, documented in code comments):
`search_kw`/`jieba_token` are indexed but the memory-search doc contract
still lists them as effectively dead columns — cosmetic.

## Not Done
- Did not review query-side MATCH escaping, dedup, or feedback scoring logic —
  out of scope for this schema-level review (covered by prior task reports).
- No behavior change was needed, hence no Go code change and no test change;
  review-only task.

## Verification
- Build: n/a (review-only, no code change)
- Tests: n/a (review-only; schema confirmed by direct source read)
- Commit: see deliverable declaration (docs-only commit on feat/cs-llm-adoption)
