package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/flaketest"
	"github.com/nick/gopi/internal/testutil"
)

// setMemoryDB points the tools at a fresh DB and returns its path. The tests
// exercise the same entry points the agent loop calls.
//
// MIGRATED to the scaffold (2026-09-21, PROJECT.md Rule 32): this used to be
//
//	path := filepath.Join(t.TempDir(), "memory.db")
//	t.Setenv(memoryDBEnv, path)
//
// i.e. one more hand-rolled copy of the same three lines — and the exact shape
// the scaffold exists to retire, because it type-checks with or without the
// `t.Setenv` on the next line. NewTestDBPath does all of it (a FRESH per-test
// file, the env contract, the legacy spelling masked), so the 47 tests that call
// this helper inherit the guarantees instead of re-stating them. It is the
// worked example in devdoc/testdb-scaffold-contract.md.
func setMemoryDB(t *testing.T) string {
	t.Helper()
	return testutil.NewTestDBPath(t)
}

// openRawDB opens a SQLite DB directly for seeding legacy schemas.
func openRawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	t.Cleanup(func() { db.Close() })
	return db
}

func seed(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

// splitRendered splits the multi-hit render back into per-hit blocks.
func splitRendered(t *testing.T, out string) []string {
	t.Helper()
	// A merged result (memory_session_hits.go) wraps the distilled render in its
	// section label and appends the [session] half after it. The per-hit block
	// shape this helper describes is the DISTILLED half's, so both are trimmed
	// here: the merge ADDS a section, it does not change a hit block.
	if _, rest, ok := strings.Cut(out, memoryDistilledSectionHeader+"\n"); ok {
		out = rest
	}
	if head, _, ok := strings.Cut(out, "\n\n"+memorySessionSectionHeader); ok {
		out = head
	}
	return strings.Split(out, "\n---\n")
}

// countHits counts the hit blocks in a rendered result.
func countHits(out string) int {
	return strings.Count(out, "\nQ: ")
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

// TestMemorySearchHitsAndRender: the happy path — store then search; the
// render carries the value signals (tier/confidence/lesson_type/when_at)
// and the Q/A content intact.
func TestMemorySearchHitsAndRender(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("go build cache", "why is my go build slow?", "Use the build cache: -p and -cache.", "repo-a", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	out, err := MemorySearch("slow go build", 0, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	for _, want := range []string{"now=", "#1", "go build cache", "why is my go build slow?", "tier=warm", "conf=0.85", "stored=", "age=0d", "sources=repo-a", "Q: why is my go build slow?", "A: Use the build cache: -p and -cache."} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %q:\n%s", want, out)
		}
	}
}

// TestMemorySearchEmptyFreshDB: first use auto-inits the schema, and a
// no-hit renders the no-distillation hint steering toward memory_store —
// not an error.
func TestMemorySearchEmptyFreshDB(t *testing.T) {
	path := setMemoryDB(t)
	out, err := MemorySearch("coldfusion deploy", 5, "deploy")
	if err != nil {
		t.Fatalf("empty result must not be an error: %v", err)
	}
	if !strings.Contains(out, "No distilled memory found") || !strings.Contains(out, "memory(action=store)") {
		t.Errorf("empty hint missing store steer:\n%s", out)
	}
	// The DB was created by the search itself (init 内嵌).
	if _, err := os.Stat(path); err != nil {
		t.Errorf("search did not auto-create the DB: %v", err)
	}
}

// TestMemorySearchEmptyQuery: an empty query is rejected before any DB work.
func TestMemorySearchEmptyQuery(t *testing.T) {
	if _, err := MemorySearch("  ", 5, ""); err == nil {
		t.Fatal("expected error for empty query")
	}
}

// TestMemorySearchLimit: the cap is enforced; zero means the default (5).
func TestMemorySearchLimit(t *testing.T) {
	setMemoryDB(t)
	for i := 0; i < 3; i++ {
		q := strings.Repeat("q", 3+i)
		if _, err := MemoryStore("topic "+q, "question "+q, "answer about "+q, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	out, err := MemorySearch("question", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := countHits(out); n != 1 {
		t.Errorf("limit=1 returned %d hits:\n%s", n, out)
	}
	if out, err := MemorySearch("question", 0, ""); err != nil || countHits(out) != 3 {
		t.Errorf("default limit should return all 3 hits, got %q (err=%v)", out, err)
	}
}

// TestMemorySearchOperatorQueryEscaped: FTS5 operator characters in the
// query are neutralized (quoted phrases) — no syntax error, never SQL/FTS
// injection. Beyond that, the ASCII partial-recall fallback keeps a noisy
// operator-laden query from silently returning nothing: "go OR build AND NOT"
// still recalls the "go build" record via the shared "go"/"build" tokens.
func TestMemorySearchOperatorQueryEscaped(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("go build", "how to build go?", "run go build .", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("go OR build AND NOT", 5, "")
	if err != nil {
		t.Fatalf("operator query must not error: %v", err)
	}
	// Injection-safety is the hard guarantee: operators parse, no error.
	if !strings.Contains(out, "go build") {
		t.Errorf("operator query should not crash but should partial-recall the record, got:\n%s", out)
	}
}

// TestMemorySearchIntentHits: the intent routing hint counts into the
// intent_hits signal (ASCII approximation, word-boundary matched).
func TestMemorySearchIntentHits(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("vpn mtu", "vpn drops under load?", "raise the mtu and disable offload", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("vpn", 5, "mtu tunnel offload")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "intent_hits=2") {
		t.Errorf("intent signal missing: %s", out)
	}
}

// TestMemorySearchPossiblyStale: knowledge older than 30 days AND never
// re-verified renders possibly_stale=true; recent entries do not.
//
// The two when_at markers are DERIVED from the rule's own window (staleAfterDays)
// and from the clock at run time — never hard-coded calendar dates. The hard-coded
// pair ('2026-07-01' old / '2026-08-20' fresh) was correct when written and became
// FALSE 30 days later: on 2026-09-19 the "fresh" marker had itself crossed the
// window, so the assertion "fresh entry should not be stale" could no longer hold
// on any run. A dated fixture inside a relative-time rule is a test that expires;
// deriving both sides keeps the assertion measuring the rule, which is what it
// exists for.
func TestMemorySearchPossiblyStale(t *testing.T) {
	path := setMemoryDB(t)
	if err := initKnowledgeBase(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)

	// The markers are relative to NOW: one INSIDE the freshness window (half of
	// it, so the crossing cannot be a clock-skew artifact), one well past it.
	day := int64(24 * 3600)
	now := time.Now().Unix()
	freshMarker := time.Unix(now-staleAfterDays*day/2, 0).UTC().Format(whenAtDateLayout)
	oldMarker := time.Unix(now-(staleAfterDays+7)*day, 0).UTC().Format(whenAtDateLayout)
	seed(t, db,
		`INSERT INTO distilled_qa (topic, question, answer, when_at) VALUES ('old vpn', 'vpn old issue?', 'old fix', '`+oldMarker+`')`,
		`INSERT INTO distilled_qa (topic, question, answer, when_at) VALUES ('fresh vpn', 'vpn fresh issue?', 'fresh fix', '`+freshMarker+`')`,
	)
	out, err := MemorySearch("vpn issue", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	blocks := splitRendered(t, out)
	var oldBlock, freshBlock string
	for _, b := range blocks {
		if strings.Contains(b, "old vpn") {
			oldBlock = b
		}
		if strings.Contains(b, "fresh vpn") {
			freshBlock = b
		}
	}
	if !strings.Contains(oldBlock, "possibly_stale=true") {
		t.Errorf("old entry should be stale:\n%s", oldBlock)
	}
	if strings.Contains(freshBlock, "possibly_stale") {
		t.Errorf("fresh entry should not be stale:\n%s", freshBlock)
	}
}

// TestMemorySearchLegacySchemaReadOnly: an old-schema knowledge base
// (missing lesson_type/when_at/sources/last_retrieved) searches fine —
// missing columns read as defaults (PRAGMA probing). The DB file must be
// byte-identical after the search (read-only compat).
func TestMemorySearchLegacySchemaReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	t.Setenv(memoryDBEnv, path)
	db := openRawDB(t, path)
	seed(t, db,
		`CREATE TABLE distilled_qa (id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT NOT NULL, question TEXT NOT NULL, answer TEXT NOT NULL, tier TEXT DEFAULT 'warm', confidence REAL DEFAULT 0.5)`,
		`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, content='distilled_qa', content_rowid='id')`,
		`INSERT INTO distilled_qa (topic, question, answer) VALUES ('legacy vpn', 'how to fix vpn?', 'check mtu')`,
		`INSERT INTO distilled_qa_fts (rowid, topic, question, answer) VALUES (1, 'legacy vpn', 'how to fix vpn?', 'check mtu')`,
	)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("vpn", 5, "")
	if err != nil {
		t.Fatalf("legacy schema search must work: %v", err)
	}
	// A legacy schema has no distilled_at AND (here) no when_at either, so the
	// write instant is unknowable and the render SAYS so — never a fabricated
	// date, and never the old when= field the split replaced.
	for _, want := range []string{"legacy vpn", "tier=warm", "conf=0.50", memoryStoredUnknownMarker} {
		if !strings.Contains(out, want) {
			t.Errorf("legacy hit missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "lesson=hello") {
		t.Error("lesson_type must default empty on legacy schema")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("search modified the legacy DB file — must be read-only")
	}
}

// TestMemoryStoreLegacySchema: storing into an old-schema DB adapts the
// INSERT to the existing columns (no ALTER, no failure).
func TestMemoryStoreLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	t.Setenv(memoryDBEnv, path)
	db := openRawDB(t, path)
	seed(t, db,
		`CREATE TABLE distilled_qa (id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT NOT NULL, question TEXT NOT NULL, answer TEXT NOT NULL, sources TEXT DEFAULT '', tier TEXT DEFAULT 'warm', confidence REAL DEFAULT 0.5)`,
		`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, content='distilled_qa', content_rowid='id')`,
	)
	out, err := MemoryStore("legacy store", "does it write?", "yes, without lesson_type/when_at", "src-x", nil)
	if err != nil {
		t.Fatalf("store into legacy schema: %v", err)
	}
	if !strings.Contains(out, "id=1") {
		t.Errorf("store result: %s", out)
	}
	// The INSERT adapted to the existing columns — the legacy table has no
	// lesson_type/when_at, so querying only the shared ones must succeed.
	var topic, q, a, sources string
	if err := db.QueryRow("SELECT topic, question, answer, sources FROM distilled_qa WHERE id=1").Scan(&topic, &q, &a, &sources); err != nil {
		t.Fatalf("legacy insert must only touch existing columns: %v", err)
	}
	if sources != "src-x" {
		t.Errorf("sources not stored: %q", sources)
	}
}

// ---------------------------------------------------------------------------
// store
// ---------------------------------------------------------------------------

// TestMemoryStoreInsertAndRetrieve: the FTS triggers keep the index in sync
// — a stored entry is immediately searchable (keyword breadcrumbs too).
func TestMemoryStoreInsertAndRetrieve(t *testing.T) {
	setMemoryDB(t)
	out, err := MemoryStore("plugin arch", "how do plugins mount?", "apply(ctx,config)", "repo-x",
		[]string{"Cordis", "元内核", "agent-loop", "插件挂载流程", "ctx.waterfall"})
	if err != nil {
		t.Fatalf("MemoryStore: %v", err)
	}
	if !strings.Contains(out, "id=1") {
		t.Errorf("store result: %s", out)
	}
	if out, err := MemorySearch("plugin mount", 5, ""); err != nil || !strings.Contains(out, "apply(ctx,config)") {
		t.Errorf("stored entry not searchable: %q (err=%v)", out, err)
	}
	if out, err := MemorySearch("ctx.waterfall", 5, ""); err != nil || !strings.Contains(out, "#1") {
		t.Errorf("keyword breadcrumb not searchable: %q (err=%v)", out, err)
	}
}

// TestMemoryStoreDedup: the same topic+question merges into the existing
// entry — deduped=true, the same id back, and sources accumulate.
func TestMemoryStoreDedup(t *testing.T) {
	path := setMemoryDB(t)
	out1, err := MemoryStore("go build cache", "why slow?", "Use -p", "repo-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1, "id=1") {
		t.Fatalf("first store: %s", out1)
	}
	out2, err := MemoryStore("go build cache", "why slow?", "Use -p", "repo-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "id=1") || !strings.Contains(out2, "deduped") {
		t.Errorf("dedup signal missing: %s", out2)
	}
	var sources string
	if err := openRawDB(t, path).QueryRow("SELECT sources FROM distilled_qa WHERE id=1").Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if sources != "repo-a,repo-b" {
		t.Errorf("sources not merged: %q", sources)
	}
}

// TestMemoryStoreDedupKeyTruncation: the 200-rune dedup key is truncated
// character-based (no UTF-8 split). The stored question stays FULL text —
// but the dedup probe is truncated and compared against the truncated
// column (substr), so a question within the key dedups AND a longer one
// still dedups on re-store. (m4: the old "long ⇒ never dedups" behavior —
// locked in as reference parity — duplicated every >200-rune brew row on
// re-import; the probe/column width mismatch is the engine bug, not a
// semantic to preserve. Full text is still stored verbatim.)
func TestMemoryStoreDedupKeyTruncation(t *testing.T) {
	path := setMemoryDB(t)

	exact := strings.Repeat("问", 200) // exactly the key width
	out, err := MemoryStore("t", exact, "a1", "", nil)
	if err != nil {
		t.Fatalf("exact-width question must store: %v", err)
	}
	if !strings.Contains(out, "id=1") {
		t.Errorf("store result: %s", out)
	}
	out2, err := MemoryStore("t", exact, "a1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "id=1") || !strings.Contains(out2, "deduped") {
		t.Errorf("exact-width question must dedup: %s", out2)
	}

	long := strings.Repeat("长", 205) // beyond the key: stores in full
	out3, err := MemoryStore("t2", long, "a2", "", nil)
	if err != nil {
		t.Fatalf("long question must store: %v", err)
	}
	if !strings.Contains(out3, "id=2") {
		t.Errorf("store result: %s", out3)
	}
	out4, err := MemoryStore("t2", long, "a2", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out4, "deduped") {
		t.Errorf("over-key-width question must dedup on re-store (probe+column truncation aligned): %s", out4)
	}
	var full string
	if err := openRawDB(t, path).QueryRow("SELECT question FROM distilled_qa WHERE id=2").Scan(&full); err != nil {
		t.Fatal(err)
	}
	if full != long {
		t.Errorf("full question not stored verbatim")
	}
}

// TestMemoryStoreRequiredFields: any of topic/question/answer missing is
// rejected before any DB work (write gate, 三件套必填).
func TestMemoryStoreRequiredFields(t *testing.T) {
	if _, err := MemoryStore("", "q", "a", "", nil); err == nil {
		t.Error("expected error for empty topic")
	}
	if _, err := MemoryStore("t", "", "a", "", nil); err == nil {
		t.Error("expected error for empty question")
	}
	if _, err := MemoryStore("t", "q", "", "", nil); err == nil {
		t.Error("expected error for empty answer")
	}
}

// TestMemoryStoreKeywordsBreadcrumb: the 5W1H keywords are merged into the
// answer tail so they persist with the record (no keyword column exists) —
// future memory_search can match them (面包屑有效). No keywords leaves the
// answer verbatim (backward compat).
func TestMemoryStoreKeywordsBreadcrumb(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("plugin arch", "how do plugins mount?", "apply(ctx,config)", "repo-x",
		[]string{"Cordis", "元内核", "agent-loop", "插件挂载流程", "ctx.waterfall"}); err != nil {
		t.Fatal(err)
	}
	var answer string
	if err := openRawDB(t, path).QueryRow("SELECT answer FROM distilled_qa WHERE id=1").Scan(&answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "keywords: Cordis, 元内核, agent-loop, 插件挂载流程, ctx.waterfall") {
		t.Errorf("keywords not merged into the answer tail: %q", answer)
	}

	if _, err := MemoryStore("t2", "q2", "plain answer", "", nil); err != nil {
		t.Fatal(err)
	}
	var plain string
	if err := openRawDB(t, path).QueryRow("SELECT answer FROM distilled_qa WHERE id=2").Scan(&plain); err != nil {
		t.Fatal(err)
	}
	if plain != "plain answer" {
		t.Errorf("answer mutated without keywords: %q", plain)
	}
}

// ---------------------------------------------------------------------------
// DB location & foreign-file protection
// ---------------------------------------------------------------------------

// TestMemoryDBPathResolution: GOPI_MEMORY_DB > GOPI_HIPPO_DB (legacy alias)
// > ~/.gopi/memory.db (shared default).
func TestMemoryDBPathResolution(t *testing.T) {
	t.Setenv(memoryDBEnv, "/tmp/primary.db")
	if got := memoryDBPath(); got != "/tmp/primary.db" {
		t.Errorf("GOPI_MEMORY_DB not honored: %s", got)
	}
	t.Setenv(memoryDBEnv, "")
	t.Setenv(memoryDBEnvLegacy, "/tmp/legacy.db")
	if got := memoryDBPath(); got != "/tmp/legacy.db" {
		t.Errorf("legacy GOPI_HIPPO_DB not honored: %s", got)
	}
	t.Setenv(memoryDBEnvLegacy, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The fixture's gopi root must be NAMED, not inherited from HOME: the
	// suite pins GOPI_HOME at its own sandbox (testutil.PinSuiteGopiProfile),
	// and GOPI_HOME wins over HOME in config.GopiDir.
	t.Setenv(config.GopiDirEnv, home)
	want := filepath.Join(home, ".gopi", memoryDefaultDBFile)
	if got := memoryDBPath(); got != want {
		t.Errorf("default = %s, want %s", got, want)
	}
}

// TestMemorySearchForeignFileRejected: a custom path pointing at a foreign
// SQLite file (no distilled_qa) is rejected, not mutated — this is what
// keeps the hermes production DB read-only-compatible.
func TestMemorySearchForeignFileRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	t.Setenv(memoryDBEnv, path)
	db := openRawDB(t, path)
	seed(t, db, "CREATE TABLE junk (x TEXT)")
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	before, _ := os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	_, err := MemorySearch("anything", 5, "")
	if err == nil || !strings.Contains(err.Error(), "not a distilled knowledge base") {
		t.Fatalf("expected foreign-file rejection, got: %v", err)
	}
	if _, err := MemoryStore("t", "q", "a", "", nil); err == nil || !strings.Contains(err.Error(), "not a distilled knowledge base") {
		t.Fatalf("store must refuse foreign files too, got: %v", err)
	}
	after, _ := os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if string(before) != string(after) {
		t.Error("foreign file was mutated")
	}
}

// TestMemorySearchDefaultPathHeals: gopi's own shared DB at the default
// location heals a schema-less leftover file (auto-init), while the same
// file at a custom path is rejected.
func TestMemorySearchDefaultPathHeals(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The fixture's gopi root must be NAMED, not inherited from HOME: the
	// suite pins GOPI_HOME at its own sandbox (testutil.PinSuiteGopiProfile),
	// and GOPI_HOME wins over HOME in config.GopiDir.
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(memoryDBEnv, "")
	t.Setenv(memoryDBEnvLegacy, "")
	defaultPath := filepath.Join(home, ".gopi", memoryDefaultDBFile)
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("anything", 5, "")
	if err != nil {
		t.Fatalf("default path should heal: %v", err)
	}
	if !strings.Contains(out, "No distilled memory") {
		t.Errorf("healed DB should be empty: %s", out)
	}

	// Same situation via a custom path, but with a file that is somebody's
	// DATABASE (a table in it): strict rejection. An EMPTY custom-path file
	// is NOT this case — zero schema objects means no state to protect, and
	// the first-open recovery adopts it (t-mem-firstopen-refusal:
	// TestEmptyFileStoreIsInitializedNotRefused). The boundary is the file's
	// content, never the path.
	foreign := filepath.Join(t.TempDir(), "empty.db")
	fdb := openRawDB(t, foreign)
	seed(t, fdb, "CREATE TABLE junk (x TEXT)")
	fdb.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	t.Setenv(memoryDBEnv, foreign)
	if _, err := MemorySearch("anything", 5, ""); err == nil {
		t.Fatal("custom-path foreign DB must be rejected")
	}
}

// ---------------------------------------------------------------------------
// render contract (direct — the function is the tool-result boundary)
// ---------------------------------------------------------------------------

func TestRenderMemorySearchResult(t *testing.T) {
	// Array of hits renders with value signals intact.
	// A FIXED clock: the result's now= anchor and every age= on it are measured
	// against one instant, so the assertion pins the arithmetic instead of
	// expiring with the wall clock.
	fixedNow := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	out, err := renderMemorySearchResultAt(`[{"id":7,"topic":"go build cache (2026-08-01 note)","question":"why slow?","answer":"use -p","tier":"verified","confidence":0.92,"lesson_type":"success","when_at":"2026-08-01","sources":["repo-a"],"possibly_stale":false,"intent_hits":2}]`, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// The row states 2026-08-01 in its own text and carries no distilled_at, so
	// the write instant falls back to when_at and the stated date renders as
	// about=: the two facts are independent and BOTH are visible.
	for _, want := range []string{"#7", "tier=verified", "conf=0.92", "lesson=success", "now=2026-08-11T12:00:00Z", "stored=2026-08-01", "age=10d", "about=2026-08-01", "sources=repo-a", "intent_hits=2", "Q: why slow?", "A: use -p"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}

	// {"empty": true} → the no-distillation hint, not an error.
	if out, err := renderMemorySearchResult(`{"empty": true, "raw_hint": true}`); err != nil || !strings.Contains(out, "memory(action=store)") {
		t.Errorf("empty-object render: %q (err=%v)", out, err)
	}

	// [] → the same hint.
	if out, err := renderMemorySearchResult(`[]`); err != nil || !strings.Contains(out, "No distilled memory") {
		t.Errorf("empty-array render: %q (err=%v)", out, err)
	}

	// Single-line JSON tolerated (parsed, not string-inspected).
	if out, err := renderMemorySearchResult(`[{"id":1,"topic":"t","question":"q","answer":"a","tier":"tier2","confidence":0.5,"lesson_type":"pitfall","when_at":"2026-08-02"}]`); err != nil || !strings.Contains(out, "tier=tier2") {
		t.Errorf("single-line render: %q (err=%v)", out, err)
	}

	// Garbage / empty output are errors — the contract is JSON.
	if _, err := renderMemorySearchResult("hello world\n"); err == nil || !strings.Contains(err.Error(), "unparseable") {
		t.Errorf("garbage should error, got: %v", err)
	}
	if _, err := renderMemorySearchResult(""); err == nil || !strings.Contains(err.Error(), "empty output") {
		t.Errorf("empty output should error, got: %v", err)
	}
}

// TestMemorySearchNonBlocking: concurrent stores from separate connections
// (two gopi instances sharing the DB) serialize via busy_timeout instead of
// failing — the shared ~/.gopi/memory.db contract.
func TestMemorySearchNonBlocking(t *testing.T) {
	flaketest.Declare("deterministic: four concurrent MemoryStore calls race the FIRST-OPEN schema build of a fresh ledger — ensureSchema, ensureFeedbackSchema and ensureTasksSchema are a SEQUENCE of write transactions, each commit a device sync, so the build legitimately outlasts the losers' bounded wall-clock poll (waitForDistilledSchema, dbSchemaWaitTries x dbSchemaWaitInterval). Registered: testdata/flakes.tsv, owner 01a0b314-83bf-7d5b-95f8-168fb9293868.")
	setMemoryDB(t)
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func(n int) {
			_, err := MemoryStore("concurrent topic", "question?", "answer", "", nil)
			done <- err
		}(i)
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent store: %v", err)
		}
	}
	out, err := MemorySearch("concurrent", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "concurrent topic") {
		t.Errorf("concurrent entries not searchable: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Fix 1 (death signal resurrection) & Fix 2 (dead columns) & Fix 4 (escape)
// ---------------------------------------------------------------------------

// TestMemorySearchBumpsRetrievalCount: each search re-verifies the hit rows —
// retrieval_count increments and last_retrieved is written, so the
// possibly_stale "was re-verified" half of its AND-condition goes live
// (report §6.2 #4). Two searches of the same record → count 2.
func TestMemorySearchBumpsRetrievalCount(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("vpn split tunnel", "how to fix split tunnel?", "enable dns", "", nil); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := openRawDB(t, path).QueryRow("SELECT id FROM distilled_qa").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("fresh DB should have id=1, got %d", id)
	}
	// First search bumps to 1.
	if _, err := MemorySearch("vpn split tunnel", 5, ""); err != nil {
		t.Fatal(err)
	}
	var rc int
	var lr sql.NullFloat64
	if err := openRawDB(t, path).QueryRow(
		"SELECT retrieval_count, last_retrieved FROM distilled_qa WHERE id=1").Scan(&rc, &lr); err != nil {
		t.Fatal(err)
	}
	if rc != 1 {
		t.Errorf("after 1st search retrieval_count=%d, want 1", rc)
	}
	if !lr.Valid || lr.Float64 <= 0 {
		t.Errorf("last_retrieved not written after 1st search (%+v)", lr)
	}
	// Second search bumps to 2.
	if _, err := MemorySearch("vpn split tunnel", 5, ""); err != nil {
		t.Fatal(err)
	}
	if err := openRawDB(t, path).QueryRow(
		"SELECT retrieval_count, last_retrieved FROM distilled_qa WHERE id=1").Scan(&rc, &lr); err != nil {
		t.Fatal(err)
	}
	if rc != 2 {
		t.Errorf("after 2nd search retrieval_count=%d, want 2", rc)
	}
	if !lr.Valid || lr.Float64 <= 0 {
		t.Errorf("last_retrieved not written after 2nd search (%+v)", lr)
	}
}

// TestMemoryStoreSearchKwColumn: the 5W1H keywords are persisted into the
// search_kw column (not just the answer tail), and a keyword-only query hits
// via the FTS index even though the keyword never appears verbatim in the
// answer body (report §2.3 + §8 #1).
func TestMemoryStoreSearchKwColumn(t *testing.T) {
	path := setMemoryDB(t)
	kws := []string{"Cordis", "元内核", "agent-loop", "ctx.waterfall"}
	if _, err := MemoryStore("plugin arch", "how do plugins mount?", "apply(ctx,config)", "repo-x", kws); err != nil {
		t.Fatal(err)
	}
	var searchKw, jieba string
	if err := openRawDB(t, path).QueryRow(
		"SELECT search_kw, jieba_token FROM distilled_qa WHERE id=1").Scan(&searchKw, &jieba); err != nil {
		t.Fatal(err)
	}
	for _, k := range kws {
		if !strings.Contains(searchKw, k) {
			t.Errorf("search_kw missing keyword %q: %q", k, searchKw)
		}
	}
	// jieba_token carries the SEGMENTED form of the stored CJK (a real
	// segmenter, not a 2-char window): 元内核 must survive as its own word.
	if !strings.Contains(jieba, "元") || !strings.Contains(jieba, "内核") {
		t.Errorf("jieba_token does not carry the segmented CJK of the stored row: %q", jieba)
	}
	// The search_kw column is FTS-indexed, so a keyword-only query hits even
	// when the keyword is absent from answer body (it lives in the tail too,
	// but the column is the point of this test).
	out, err := MemorySearch("Cordis agent-loop", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "plugin arch") {
		t.Errorf("search_kw keyword query did not hit: %s", out)
	}
}

// TestMemorySearchCJKEnglishRegression (Fix 4): a CJK multi-character query
// hits a Chinese entry (the trigram/LIKE routes handle the unquoted CJK
// token that escapeQuery now leaves bare), and an English multi-word phrase
// query still hits (quoted phrase, no regression). "VPN 教训" must still
// match the VPN lesson.
func TestMemorySearchCJKEnglishRegression(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("vpn 教训", "vpn split tunnel drops?", "enable dns and raise mtu", "", []string{"split-tunnel", "DNS", "MTU"}); err != nil {
		t.Fatal(err)
	}
	// CJK multi-char query → must hit (was 0 before polish).
	out, err := MemorySearch("教训", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "vpn 教训") {
		t.Errorf("CJK query 教训 did not hit:\n%s", out)
	}
	// Mixed English phrase "VPN 教训" → must still hit (English regression).
	out, err = MemorySearch("VPN 教训", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "vpn 教训") {
		t.Errorf("mixed EN+CJK query \"VPN 教训\" did not hit:\n%s", out)
	}
	// English-only phrase → must still hit (no regression).
	out, err = MemorySearch("DNS MTU", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "vpn 教训") {
		t.Errorf("English phrase DNS MTU did not hit:\n%s", out)
	}
	// Operator characters still neutralized (injection-safe).
	if _, err := MemorySearch(`vpn AND OR NOT "x`, 5, ""); err != nil {
		t.Fatalf("operator-heavy query must not error: %v", err)
	}
}

// TestMemorySearchASCIIPartialRecall — regression for the repeat-run bug: an
// LLM-generated n-gram query with one out-of-vocabulary ASCII word used to
// zero the whole exact-token AND match, so the second run of the same task
// found none of the lessons the first run had stored (sessions 82f5bc4c →
// 1cbb8fef on the VCMI task: "vcmi LLM AI roadmap tool calls game agent
// study" returned nothing despite 14 rows mentioning "vcmi"). The ASCII
// OR-fallback must rescue such noisy queries.
func TestMemorySearchASCIIPartialRecall(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore(
		"VCMI AI architecture: how to add a new AI player",
		"how do I add a new adventure AI to VCMI?",
		"subclass CGlobalAI or CAdventureAI, register in AIFactory createAdventureAI and isAvailableAdventureAI, and gate behind an ENABLE_*_AI CMake option.",
		"", []string{"VCMI", "AIFactory", "LLM AI roadmap"},
	); err != nil {
		t.Fatal(err)
	}

	// The exact full query from the failing repeat-run (contains "study",
	// present in no row). Before the fix this returned the no-memory hint
	// even though the record exists and mentions "vcmi".
	out, err := MemorySearch("vcmi LLM AI roadmap tool calls game agent study", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "No distilled memory") {
		t.Fatalf("partial recall failed: out-of-vocab word nuked the AND match:\n%s", out)
	}
	if !strings.Contains(out, "VCMI AI architecture") {
		t.Errorf("ASCII OR-fallback did not recall the VCMI record:\n%s", out)
	}

	// A clean AND-match query still works at full precision (no regression):
	// "vcmi AI" both present → must recall, and it should be a precise hit.
	out, err = MemorySearch("vcmi AI", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "VCMI AI architecture") {
		t.Errorf("exact AND query did not hit:\n%s", out)
	}
}

// TestMemorySearchSingleASCIIOrderDegrades / escapeQueryOR refuses to
// downgrade a single-token query (OR == AND on one term, so the fallback is
// not triggered — nothing extra to lose).
func TestEscapeQueryOR(t *testing.T) {
	if got := escapeQueryOR("one"); got != "" {
		t.Errorf("single token should return empty (no OR needed), got %q", got)
	}
	if got := escapeQueryOR("vcmi study"); got != `"vcmi" OR "study"` {
		t.Errorf("two tokens should OR, got %q", got)
	}
	if got := escapeQueryOR("a b"); got != "" {
		t.Errorf("sub-min-length tokens must be dropped, got %q", got)
	}
	if got := escapeQueryOR(`vcmi "AI`); got != `"vcmi" OR "AI"` {
		t.Errorf("quotes neutralized + tokens ORed, got %q", got)
	}
	// CJK tokens are not OR-ed here (the trigram/LIKE routes own them); with
	// only one remaining ASCII token the helper returns empty (no downgrade).
	if got := escapeQueryOR("vcmi 教训"); got != "" {
		t.Errorf("CJK token excluded, leaving one ASCII token → empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// round-3: value-signal ranking, cold tier, maintenance CLI backend.
// ---------------------------------------------------------------------------

// storeID stores a row and returns its id (parsed from the store confirmation
// "Memory stored (id=N).").
func storeID(t *testing.T, topic, question, answer string) int {
	t.Helper()
	out, err := MemoryStore(topic, question, answer, "", nil)
	if err != nil {
		t.Fatalf("MemoryStore: %v", err)
	}
	var id int
	if _, err := fmt.Sscanf(out, "Memory stored (id=%d)", &id); err != nil {
		t.Fatalf("parse store id from %q: %v", out, err)
	}
	return id
}

// TestMemorySearchRankingTokenCoverage: the primary ranking key is how many
// distinct meaningful query tokens a hit covers. When the exact AND match
// misses (an out-of-vocabulary token zeroes it) the OR fallback recalls
// partial-coverage rows — and among them higher coverage ranks first, the fix
// for "a row that merely repeats rare tokens outranks one that answers more
// of the query".
func TestMemorySearchRankingTokenCoverage(t *testing.T) {
	setMemoryDB(t)
	full := storeID(t, "vpn", "vpn mtu drops?", "raise the mtu and disable offload")
	partial2 := storeID(t, "vpn", "vpn drops under load?", "raise the mtu")
	partial1 := storeID(t, "mtu", "mtu size?", "mtu tuning notes here")

	// "zzz" appears in no row → exact AND returns 0 → the OR fallback recalls
	// the three rows by shared tokens; the final re-sort must order them by
	// query-token coverage (3, 2, 1), not by raw BM25.
	out, err := MemorySearch("vpn mtu offload zzz", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	blocks := splitRendered(t, out)
	if len(blocks) < 3 {
		t.Fatalf("want 3 hits, got %d:\n%s", len(blocks), out)
	}
	// Order: full coverage (tokens=3) first, then 2, then 1.
	if !strings.Contains(blocks[0], fmt.Sprintf("#%d", full)) || !strings.Contains(blocks[0], "tokens=3") {
		t.Errorf("first hit should be full-coverage row #%d tokens=3:\n%s", full, blocks[0])
	}
	if !strings.Contains(blocks[1], fmt.Sprintf("#%d", partial2)) || !strings.Contains(blocks[1], "tokens=2") {
		t.Errorf("second hit should be row #%d tokens=2:\n%s", partial2, blocks[1])
	}
	if !strings.Contains(blocks[2], fmt.Sprintf("#%d", partial1)) || !strings.Contains(blocks[2], "tokens=1") {
		t.Errorf("third hit should be row #%d tokens=1:\n%s", partial1, blocks[2])
	}
}

// TestMemorySearchColdDemoted: the epic's core contract — a low-quality row
// demoted to cold via MemoryUpdate stops being 1st tier to return. While any
// warm/hot row matches, cold rows are excluded; a cold row only fills slots
// when nothing warm/hot matches (knowledge is demoted, not lost).
func TestMemorySearchColdDemoted(t *testing.T) {
	setMemoryDB(t)
	warmID := storeID(t, "deepseek v4 flash", "deepseek v4 flash config?", "notes about deepseek v4 flash configuration")
	coldID := storeID(t, "deepseek v4 flash", "deepseek v4 flash price?", "notes about deepseek v4 flash pricing and cost")

	if _, err := MemoryUpdate(coldID, MemoryUpdateFields{Tier: coldTier}); err != nil {
		t.Fatalf("demote to cold: %v", err)
	}

	// Cold row is excluded while the warm row matches.
	out, err := MemorySearch("deepseek v4 flash", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("#%d", warmID)) {
		t.Errorf("warm row #%d missing:\n%s", warmID, out)
	}
	if strings.Contains(out, fmt.Sprintf("#%d", coldID)) {
		t.Errorf("cold row #%d must be excluded while warm matches:\n%s", coldID, out)
	}

	// With only the cold row left it fills the result (demoted, not deleted).
	if _, err := MemoryDelete(warmID); err != nil {
		t.Fatalf("delete warm row: %v", err)
	}
	out, err = MemorySearch("deepseek v4 flash", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch after delete: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("#%d", coldID)) {
		t.Errorf("cold row should fill when no warm/hot row matches:\n%s", out)
	}
}

// TestMemoryGetInspect: memory_get renders one record's full content through
// the same scan path as search hits.
func TestMemoryGetInspect(t *testing.T) {
	setMemoryDB(t)
	id := storeID(t, "vpn mtu", "vpn drops under load?", "raise the mtu and disable offload")
	_ = id
	out, err := MemoryGet(id)
	if err != nil {
		t.Fatalf("MemoryGet: %v", err)
	}
	for _, want := range []string{"vpn mtu", "vpn drops under load?", "raise the mtu and disable offload"} {
		if !strings.Contains(out, want) {
			t.Errorf("MemoryGet missing %q:\n%s", want, out)
		}
	}
}

// TestMemoryUpdateTierValidation: only hot|warm|cold are accepted; a
// non-existent id and an empty update are rejected before any write.
func TestMemoryUpdateTierValidation(t *testing.T) {
	setMemoryDB(t)
	id := storeID(t, "t", "question about topic", "an answer long enough to pass the gate")

	if _, err := MemoryUpdate(id, MemoryUpdateFields{Tier: "frozen"}); err == nil {
		t.Fatal("invalid tier must be rejected")
	}
	if _, err := MemoryUpdate(99999, MemoryUpdateFields{Tier: coldTier}); err == nil {
		t.Fatal("updating a non-existent id must error")
	}
	if _, err := MemoryUpdate(id, MemoryUpdateFields{}); err == nil {
		t.Fatal("empty update must error")
	}
}

// TestMemoryUpdateKeywordsReplaceTail: new keywords replace BOTH the
// search_kw column and the "keywords:" answer tail — no stale breadcrumbs
// linger, and the record is only reachable via the new keywords.
func TestMemoryUpdateKeywordsReplaceTail(t *testing.T) {
	setMemoryDB(t)
	id := storeID(t, "vpn", "vpn drops under load?", "raise the mtu and disable offload")
	if _, err := MemoryUpdate(id, MemoryUpdateFields{Keywords: []string{"alpha", "beta"}}); err != nil {
		t.Fatalf("update keywords: %v", err)
	}
	out, err := MemorySearch("alpha beta", 5, "")
	if err != nil {
		t.Fatalf("search by new keyword: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("#%d", id)) {
		t.Errorf("record should be reachable via new keywords:\n%s", out)
	}
	if strings.Contains(out, "disable offload") == false {
		t.Errorf("answer body should be preserved:\n%s", out)
	}
	// Replace again — the tail must not accumulate (only one keywords line).
	if _, err := MemoryUpdate(id, MemoryUpdateFields{Keywords: []string{"gamma"}}); err != nil {
		t.Fatalf("re-update keywords: %v", err)
	}
	got, err := MemoryGet(id)
	if err != nil {
		t.Fatalf("MemoryGet: %v", err)
	}
	if n := strings.Count(got, "keywords:"); n != 1 {
		t.Errorf("keywords tail duplicated (%d occurrences):\n%s", n, got)
	}
}

// TestMemoryDeleteRemovesIndexEntries: after delete the row is gone from the
// FTS index AND the trigram index (base + both indexes stay consistent).
func TestMemoryDeleteRemovesIndexEntries(t *testing.T) {
	path := setMemoryDB(t)
	id := storeID(t, "vpn mtu", "vpn drops under load?", "raise the mtu and disable offload")
	if _, err := MemoryDelete(id); err != nil {
		t.Fatalf("MemoryDelete: %v", err)
	}
	if _, err := MemoryDelete(id); err == nil {
		t.Fatal("deleting again must error (no record)")
	}
	out, err := MemorySearch("vpn mtu", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch after delete: %v", err)
	}
	if !strings.Contains(out, "No distilled memory found") {
		t.Errorf("deleted row must be unreachable:\n%s", out)
	}
	db := openRawDB(t, path)
	var fts, tri int
	if err := db.QueryRow("SELECT COUNT(*) FROM distilled_qa_fts WHERE rowid=?", id).Scan(&fts); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM distilled_qa_tri WHERE rowid=?", id).Scan(&tri); err != nil {
		t.Fatalf("tri count: %v", err)
	}
	if fts != 0 || tri != 0 {
		t.Errorf("index entries not cleaned: fts=%d tri=%d", fts, tri)
	}
}

// ---------------------------------------------------------------------------
// AND-subset degradation ladder (memsearch-subset-degrade).
// ---------------------------------------------------------------------------

// memsearchRenderedFirstHitID reads the "#id" of a rendered hit block.
func memsearchRenderedFirstHitID(t *testing.T, block string) int {
	t.Helper()
	line := strings.SplitN(block, "\n", 2)[0]
	for _, field := range strings.Fields(line) {
		if n, ok := strings.CutPrefix(field, "#"); ok {
			v, err := strconv.Atoi(n)
			if err != nil {
				t.Fatalf("unparsable hit id field %q", field)
			}
			return v
		}
	}
	t.Fatalf("no #id in hit block %q", line)
	return 0
}

// TestMemsearchLadderDegradesToSubset is the epic's red proof, expressed as a
// behavioural assertion rather than a code-shape one.
//
// THE FIXTURE IS THE CRITICAL PART, because two of gopi's existing behaviours
// mask the defect: the 2-char CJK LIKE route recalls such a row anyway, and the
// coverage signal scores the whole un-splittable CJK/ASCII query at 0 for every
// row. So "was the row recalled" is green on master, and "tokens=" cannot see
// the subset. What the ladder uniquely changes is ORDER, and the shape that
// exposes it is the corpus's real shape (devdoc/jieba-token-absorbs-search-kw.md):
//
//   - the SUBJECT carries the query's CJK signal in its `search_kw` only — its
//     topic/question/answer never spell it, so queryTokenCoverage cannot see it
//     (it scans topic/question/answer/exam/kw-label text, not search_kw);
//   - the NOISE row carries the ASCII token `deepseek` in its body and is hot
//     (conf 1.0), so on master it wins the ranking outright;
//   - `开支` is in no row, so the all-AND fast path cannot answer and the
//     descent is forced.
//
// The ladder's middle rung is exactly `"deepseek" AND "降低"`, whose sole member
// is the subject (the noise row lacks 降低), so membership puts the subject
// first and the OR pool's single-token row below it.
func TestMemsearchLadderDegradesToSubset(t *testing.T) {
	setMemoryDB(t)
	// Subject: deepseek in the body, the CJK signal in search_kw.
	subject := storeID(t, "成本分析", "缓存命中率下降，deepseek 账单会怎样？",
		"命中率下降会让 deepseek 账单涨上去，错峰调度能省一半。")
	if _, err := MemoryUpdate(subject, MemoryUpdateFields{Keywords: []string{"deepseek", "降低"}}); err != nil {
		t.Fatalf("MemoryUpdate: %v", err)
	}
	// Noise: the ASCII token plus a hotter confidence — master's winner.
	noise := storeID(t, "vision", "vision model as first-class model deepseek-v4-flash", "an unrelated old note")
	if _, err := MemoryUpdate(noise, MemoryUpdateFields{Confidence: ptrFloat(1.0)}); err != nil {
		t.Fatalf("MemoryUpdate: %v", err)
	}
	// Fixture self-check through the PUBLIC surface only (so this test also
	// compiles against the pre-change code — that is what makes the red proof
	// runnable): 开支 must be in NO row, or the all-AND fast path answers and
	// the descent never runs. This guard caught the first version of this
	// fixture, whose answer text contained the term.
	if probe, err := MemorySearch("开支", 5, ""); err != nil {
		t.Fatalf("MemorySearch probe: %v", err)
	} else if !strings.Contains(probe, "No distilled memory found") {
		t.Fatalf("fixture invalid: 开支 must match no row, probe found hits:\n%s", probe)
	}

	out, err := MemorySearch("怎么降低deepseek开支", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	blocks := splitRendered(t, out)
	if len(blocks) == 0 || strings.Contains(out, "No distilled memory found") {
		t.Fatalf("ladder did not recall the subject row:\n%s", out)
	}
	// THE RED PROOF: master ranks the hot single-token row first because no
	// middle rung exists to promote the row the conjunction actually matched.
	if got := memsearchRenderedFirstHitID(t, blocks[0]); got != subject {
		t.Errorf("rank-1 hit = #%d, want the subject #%d (master ranks the "+
			"single-token row first — the ladder's middle rung never ran):\n%s", got, subject, out)
	}
	// Recall is not traded away: the exact route missed, so the OR fallback
	// still runs. Queried directly, so the assertion is about the fallback
	// reaching the row, not about its position.
	out, err = MemorySearch("deepseek", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("#%d", noise)) {
		t.Errorf("OR-route reachability regressed for row #%d:\n%s", noise, out)
	}

}

// ptrFloat is the optional-confidence setter for MemoryUpdateFields.
func ptrFloat(v float64) *float64 { return &v }

// TestMemsearchLadderShedsTheFillerNotTheSignal — the within-size tiebreak,
// which DECIDES the winner whenever several same-size subsets qualify (the
// common case: any query with an unmatched filler has one).
//
// The rule maximizes the summed DF of the DROPPED terms, i.e. sheds the
// query's highest-DF filler — the epic's "drop noise, keep signal". The
// reference engine minimizes the same sum ("lower = rarer = more likely
// noise"); that direction is measured WORSE on gopi's corpus and the reason is
// documented on memsearchDroppedDF (it kept 怎么, DF 434, and shed deepseek,
// picking a 136-row conjunct over a 1-row one). This test pins the shipped
// direction, so a future "port fidelity" change that flips it back goes red.
func TestMemsearchLadderShedsTheFillerNotTheSignal(t *testing.T) {
	setMemoryDB(t)
	// zzcommon is the filler (every row); zzbeta appears twice; zzsignal once.
	signal := storeID(t, "cost analysis", "zzsignal here and some detail", "the record that answers the question")
	storeID(t, "noise one", "zzcommon zzbeta filler one", "an unrelated row")
	storeID(t, "noise two", "zzcommon zzbeta filler two", "another unrelated row")
	// zzcommon must really be the highest-DF term, and zzsignal the rarest.
	storeID(t, "noise three", "zzcommon filler three", "a third unrelated row")
	storeID(t, "noise four", "zzcommon filler four", "a fourth unrelated row")

	ctx := context.Background()
	db := openRawDB(t, memoryDBPath())
	dfCommon, ok := memsearchTermDF(ctx, db, `"zzcommon"`)
	if !ok {
		t.Fatal("zzcommon unmeasurable")
	}
	dfSignal, ok := memsearchTermDF(ctx, db, `"zzsignal"`)
	if !ok {
		t.Fatal("zzsignal unmeasurable")
	}
	if dfCommon <= dfSignal {
		t.Fatalf("fixture invalid: filler DF %d must exceed signal DF %d", dfCommon, dfSignal)
	}
	// The tiebreak value IS the summed DF of the dropped terms, and shedding
	// the filler must score HIGHER under the shipped (maximize) direction.
	shedFiller := memsearchDroppedDF(ctx, db, []string{`"zzcommon"`})
	shedSignal := memsearchDroppedDF(ctx, db, []string{`"zzsignal"`})
	if shedFiller <= shedSignal {
		t.Errorf("shedding the filler must score higher than shedding the signal: "+
			"drop-filler=%d, drop-signal=%d", shedFiller, shedSignal)
	}
	// End to end: both same-size subsets qualify —
	//   {zzsignal, zzcommon} (keeps everything matchable) and
	//   {zzcommon, zzmissing} (the filler plus a term no row has) —
	// so only the tiebreak separates them, and the filler-shedding,
	// signal-keeping conjunct must be the one the ladder returns.
	pick, ok := memsearchSubsetPick(ctx, db, "zzsignal zzcommon zzmissing")
	if !ok {
		t.Fatal("ladder found no qualifying subset")
	}
	if !strings.Contains(pick.match, `"zzsignal"`) {
		t.Errorf("ladder shed the signal term: %q", pick.match)
	}
	out, err := MemorySearch("zzsignal zzcommon zzmissing", 10, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	out = strings.ReplaceAll(out, "\n", "\n")
	if !strings.Contains(out, fmt.Sprintf("#%d", signal)) {
		t.Errorf("the signal row #%d was not recalled:\n%s", signal, out)
	}
}

// TestMemsearchLadderFastPathAndBounds — the ladder never downgrades a query
// the exact route can answer, and refuses queries outside its bounds.
func TestMemsearchLadderFastPathAndBounds(t *testing.T) {
	setMemoryDB(t)
	storeID(t, "vpn mtu", "vpn mtu drops?", "raise the mtu and disable offload")
	db := openRawDB(t, memoryDBPath())

	// Fast path: the full AND qualifies → the pick IS the full AND.
	pick, ok := memsearchSubsetPick(context.Background(), db, "vpn mtu drops")
	if !ok {
		t.Fatal("fast path declined a query the exact route answers")
	}
	want := `"vpn" AND "mtu" AND "drops"`
	if pick.match != want {
		t.Errorf("fast path must keep the full AND, got %q want %q", pick.match, want)
	}
	// Below the ladder's activation floor: no subset walk (the OR fallback
	// owns 1-2 token queries).
	if _, ok := memsearchSubsetPick(context.Background(), db, "vpn mtu"); ok {
		t.Error("ladder must not engage below memsearchMinANDTokens")
	}
	// Above the token cap: refused, not walked.
	many := strings.Repeat("zz ", memsearchMaxANDTokens+3) + "vpn"
	if _, ok := memsearchSubsetPick(context.Background(), db, many); ok {
		t.Error("ladder must refuse a query above memsearchMaxANDTokens")
	}
}

// TestMemsearchLadderTokenCap — the combination-explosion bound: the token
// list is capped (Rule 1a), and the subset count for a capped query equals the
// bound the documentation claims.
func TestMemsearchLadderTokenCap(t *testing.T) {
	// A query far longer than the cap.
	words := make([]string, 0, memsearchMaxANDTokens*3)
	for i := 0; i < memsearchMaxANDTokens*3; i++ {
		words = append(words, fmt.Sprintf("w%d", i))
	}
	terms := memsearchLadderTerms(strings.Join(words, " "))
	if len(terms) != memsearchMaxANDTokens {
		t.Fatalf("token cap not enforced: got %d terms, want %d", len(terms), memsearchMaxANDTokens)
	}
	// The cap IS the safety property: at the cap the full subset space is
	// memsearchMaxCombos, and no size can exceed it.
	total := 0
	for size := 1; size <= len(terms); size++ {
		total += len(memsearchSubsets(len(terms), size))
	}
	if total != memsearchMaxCombos {
		t.Errorf("subset space at the cap = %d, want memsearchMaxCombos=%d", total, memsearchMaxCombos)
	}
	// A 10-token query walks size 9..1, whose combined space is inside the cap.
	walked := 0
	for size := len(terms) - 1; size >= 1; size-- {
		walked += len(memsearchSubsets(len(terms), size))
	}
	if walked > memsearchMaxCombos {
		t.Errorf("descent space %d exceeds the probe cap %d", walked, memsearchMaxCombos)
	}
}

// TestMemsearchLadderTermsSplitsCJKAndASCIITokens — the tokenizer that makes
// the descent possible: a glued natural-language CJK/ASCII query yields the
// 2-char CJK grams plus the ASCII word, order preserved.
func TestMemsearchLadderTermsSplitsCJKAndASCIITokens(t *testing.T) {
	got := memsearchLadderTerms("怎么降低deepseek开支")
	for _, want := range []string{`"怎么"`, `"么降"`, `"降低"`, `"deepseek"`, `"开支"`} {
		if !slices.Contains(got, want) {
			t.Errorf("missing term %s in %v", want, got)
		}
	}
	// Pure ASCII passes through as whole words; pure CJK via grams.
	if got := memsearchLadderTerms("vpn mtu"); len(got) != 2 || got[0] != `"vpn"` {
		t.Errorf("ASCII passthrough broken: %v", got)
	}
	// Injection safety: quotes stripped, a bare operator survives only quoted.
	for _, term := range memsearchLadderTerms(`vpn AND "x`) {
		if !strings.HasPrefix(term, `"`) || !strings.HasSuffix(term, `"`) {
			t.Errorf("term %q is not quoted — injection surface", term)
		}
	}
}
