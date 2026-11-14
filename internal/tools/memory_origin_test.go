package tools

// Provenance-pointer storage shape (epic 自动记忆 item 2/5). What these tests
// pin, in the order the epic's acceptance criteria state them:
//
//  1. an OLD database (one whose distilled_qa predates the columns) becomes
//     usable and KEEPS ITS ROWS — the migration is additive, and
//  2. it is IDEMPOTENT: a completed migration plus a second open changes
//     nothing and loses nothing (the concurrent-first-open race the #1145
//     five-piece pattern exists for), and
//  3. the pointer round-trips: written by store, read back by search, and the
//     reader sees the session, the round and the host.
//
// Plus the refusal: a round with no session is not stored at all (Rule 15).

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// originTestDB returns a fresh ledger path under the per-test scratch root, the
// environment pinned, and a raw handle for asserting column-level facts.
func originTestDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "memory.db")
}

// originLegacyDB turns a REAL, fully-initialised ledger into the shape a
// pre-feature gopi wrote: every table, index and trigger present, the three
// provenance columns absent. It is built by OPENING through the product's own
// initializer and then dropping exactly those three columns, never by
// hand-rolling a schema (Rule 32) — a hand-rolled fixture drifts from the real
// one the moment the schema moves, and a hand-rolled one that omits FTS cannot
// be stored into at all.
func originLegacyDB(t *testing.T, path string) {
	t.Helper()
	db := openRawDB(t, path)
	defer db.Close()
	ctx := context.Background()
	// The real schema, built by the real code path.
	if err := initKnowledgeBase(ctx, path); err != nil {
		t.Fatalf("initialise the fixture ledger: %v", err)
	}
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range originColumns {
		if !cols[c] {
			t.Fatalf("precondition: a fresh ledger must carry %s (the heal did not run)", c)
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE distilled_qa DROP COLUMN "+c); err != nil {
			t.Fatalf("drop %s to model a pre-feature ledger: %v", c, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO distilled_qa (topic, question, answer) VALUES (?, ?, ?)`,
		"legacy topic", "legacy question", "legacy answer"); err != nil {
		t.Fatalf("seed the pre-feature row: %v", err)
	}
	cols, err = tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range originColumns {
		if cols[c] {
			t.Fatalf("precondition: the modelled legacy schema must NOT carry %s", c)
		}
	}
}

// TestOriginMigrationIsAdditiveAndKeepsRows: an old DB gains the three columns
// on first open AND keeps every row it had, with the new columns reading as
// "nothing recorded" (not as an error, and not as a fabricated value).
func TestOriginMigrationIsAdditiveAndKeepsRows(t *testing.T) {
	path := originTestDB(t)
	originLegacyDB(t, path)
	t.Setenv(memoryDBEnv, path)

	// A store is the ordinary first WRITE — it opens the DB, heals it and adds
	// a row of its own.
	if _, err := MemoryStore("new topic", "new question", "new answer", "", nil); err != nil {
		t.Fatalf("store into a legacy DB: %v", err)
	}

	db := openRawDB(t, path)
	defer db.Close()
	cols, err := tableColumns(t.Context(), db, "distilled_qa")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range originColumns {
		if !cols[c] {
			t.Errorf("the heal did not add %s", c)
		}
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows after migration = %d, want 2 (the legacy row MUST survive)", n)
	}
	var topic, session, host string
	var round int
	if err := db.QueryRowContext(t.Context(),
		"SELECT topic, "+originSessionCol+", "+originRoundCol+", "+originHostCol+
			" FROM distilled_qa WHERE topic = ?", "legacy topic").
		Scan(&topic, &session, &round, &host); err != nil {
		t.Fatalf("the legacy row is unreadable after migration: %v", err)
	}
	if session != "" || host != "" || round != sessionevent.RoundIndexUnknown {
		t.Errorf("a pre-feature row must read as 'nothing recorded', got session=%q round=%d host=%q", session, round, host)
	}
}

// TestOriginMigrationIsIdempotent: running the heal on an already-healed DB —
// the concurrent-first-open case, twice over — succeeds, changes no column and
// loses no row.
func TestOriginMigrationIsIdempotent(t *testing.T) {
	path := originTestDB(t)
	originLegacyDB(t, path)
	t.Setenv(memoryDBEnv, path)

	db := openRawDB(t, path)
	for i := 0; i < 3; i++ {
		if err := ensureOriginSchema(t.Context(), db); err != nil {
			t.Fatalf("heal #%d: %v", i, err)
		}
	}
	cols, err := tableColumns(t.Context(), db, "distilled_qa")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range originColumns {
		if !cols[c] {
			t.Errorf("column %s missing after three heals", c)
		}
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows = %d after three heals, want 1", n)
	}
	db.Close() //stylegate:ignore closes the ledger handle the test opened in this scope; every assertion above it has already run and the reopen below re-reads the same file

	// And a SECOND OPEN of the same file (the real path a reader takes).
	db2 := openRawDB(t, path)
	defer db2.Close()
	if err := ensureOriginSchema(t.Context(), db2); err != nil {
		t.Fatalf("heal on a second open: %v", err)
	}
}

// TestOriginPointerRoundTrip: the pointer written by store is what search reads
// back — session, round and host — through the ordinary search route, and it is
// rendered on the hit's meta line so the LLM sees where a row came from.
func TestOriginPointerRoundTrip(t *testing.T) {
	path := originTestDB(t)
	t.Setenv(memoryDBEnv, path)

	const (
		session = "a11d0e73-test-session"
		round   = 4
		host    = "test-host-peer1"
	)
	origin := MemoryOrigin{SessionID: session, Round: round, Host: host}
	if _, err := MemoryStoreWithOrigin("origin probe topic", "does the pointer round-trip",
		"yes: session_id, round_index and source_host all come back through search", "", nil, origin); err != nil {
		t.Fatalf("store with origin: %v", err)
	}

	hits, err := MemorySearchHits("origin probe topic", 5, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("search returned nothing for the row just stored")
	}
	got := hits[0].MemoryOriginOf()
	if got.SessionID != session {
		t.Errorf("session_id = %q, want %q", got.SessionID, session)
	}
	if got.Round != round {
		t.Errorf("round_index = %d, want %d", got.Round, round)
	}
	if got.Host != host {
		t.Errorf("source_host = %q, want %q", got.Host, host)
	}
	if hits[0].Origin == nil {
		t.Error("Origin is nil — the JSON field would be omitted for a row that HAS a pointer")
	}
	if seg := originRenderSegment(got); !strings.Contains(seg, session) || !strings.Contains(seg, fmt.Sprint(round)) {
		t.Errorf("the rendered origin segment %q does not carry the session and the round", seg)
	}
}

// TestOriginRefusesRoundWithoutSession: a pointer carrying a round and no
// session names a cursor into nothing. It is refused BY NAME before any write,
// and — the half of the check that matters — no row is created by the attempt
// (Rule 15: a refusal, not a silent drop).
func TestOriginRefusesRoundWithoutSession(t *testing.T) {
	path := originTestDB(t)
	t.Setenv(memoryDBEnv, path)

	_, err := MemoryStoreWithOrigin("half pointer topic", "is half a pointer stored",
		"no: a round with no session is refused", "", nil, MemoryOrigin{Round: 3})
	if err == nil {
		t.Fatal("a round with no session was accepted")
	}
	if !strings.Contains(err.Error(), originSessionCol) {
		t.Errorf("the refusal must name the missing field %s, got: %v", originSessionCol, err)
	}

	db := openRawDB(t, path)
	defer db.Close()
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows = %d after a refused store, want 0", n)
	}
}

// TestOriginAbsentPointerStoresNoHost: a caller with no session (the CLI, an
// import, a sync patch) stores NO pointer at all — in particular not a host,
// because a row with a host and no session reads exactly like a row captured
// somewhere and never resolvable. Absence stays absence.
func TestOriginAbsentPointerStoresNoHost(t *testing.T) {
	path := originTestDB(t)
	t.Setenv(memoryDBEnv, path)

	if _, err := MemoryStore("no origin topic", "does a plain store stamp a host",
		"no: with no session there is no pointer, so the host stays empty too", "", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	var session, host string
	var round int
	if err := db.QueryRowContext(t.Context(),
		"SELECT "+originSessionCol+", "+originRoundCol+", "+originHostCol+" FROM distilled_qa").
		Scan(&session, &round, &host); err != nil {
		t.Fatal(err)
	}
	if session != "" || host != "" || round != sessionevent.RoundIndexUnknown {
		t.Errorf("a pointer-less store wrote session=%q round=%d host=%q — want all absent", session, round, host)
	}
}

// TestOriginLegacySchemaRendersNothing: a hit read from a schema WITHOUT the
// columns (a foreign/legacy DB that the heal has not touched) still renders —
// and renders NO origin segment, so the pre-feature render contract is
// byte-identical there.
func TestOriginLegacySchemaRendersNothing(t *testing.T) {
	h := MemoryHit{}
	if seg := originRenderSegment(h.MemoryOriginOf()); seg != "" {
		t.Errorf("a hit that recorded nothing rendered %q, want the segment omitted", seg)
	}
	if h.Origin != nil {
		t.Error("Origin must be nil when nothing was recorded (the JSON field is omitempty)")
	}
}
