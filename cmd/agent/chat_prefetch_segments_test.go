package main

// chat_prefetch_segments_test.go — the regression guard for the defect this
// task diagnoses: a prefetch that GAVE UP reported one opaque number (`ms` ≈
// 1500) and left the reader to guess whether the dial, the lock or the search
// had eaten the budget.
//
// Two properties are pinned here, and they are different claims:
//
//  1. the record CARRIES the segments, and their sum is the measured ms — so
//     "the 1500ms expired; which phase spent it?" is answerable from the record
//     alone (acceptance a);
//  2. the phase a failure is ATTRIBUTED to is the phase that really failed —
//     proven by driving two failure shapes that can only come from one phase
//     each (an unopenable ledger, and a deadline that is already expired).
//
// A test that only read the fields back would pass on a record that always
// wrote `handle: fresh, open_ms: 0`; the attribution cases are what stop that.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/memoryprefetch"
	"github.com/nick/gopi/internal/tools"
	_ "modernc.org/sqlite"
)

// prefetchRecordLine is the record's wire shape, read back as a test-side value.
// It is a SECOND definition of the schema, deliberately: a test that decoded
// through prefetchRecord itself would not notice a renamed or dropped field,
// which is exactly the drift the record's consumers would suffer.
type prefetchRecordLine struct {
	Query         string               `json:"query"`
	Hits          int                  `json:"hits"`
	Ms            int64                `json:"ms"`
	Outcome       string               `json:"outcome"`
	Adopted       string               `json:"adopted"`
	Err           string               `json:"err,omitempty"`
	SectionBytes  int                  `json:"section_bytes,omitempty"`
	Segment       string               `json:"segment,omitempty"`
	Segments      *prefetchSegments    `json:"segments,omitempty"`
	Notes         *prefetchNotesRecord `json:"notes,omitempty"`
	MemoryOutcome string               `json:"memory_outcome,omitempty"`
}

// readPrefetchSegments decodes the run's record into its lines.
func readPrefetchSegments(t *testing.T, dir string) []prefetchRecordLine {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, config.MemoryPrefetchRecordName))
	if err != nil {
		t.Fatalf("read the prefetch record: %v", err)
	}
	var out []prefetchRecordLine
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var r prefetchRecordLine
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			t.Fatalf("the record line %q is not decodable JSON: %v", ln, err)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		t.Fatal("the record carried no lines")
	}
	return out
}

// TestPrefetchRecordCarriesSegmentTiming is acceptance (a): a lookup's record
// answers WHERE its time went, and the segments ADD UP to the ms it reports.
// The sum is the checkable property — a record that reports a segment it did not
// measure, or omits one it did, breaks it.
func TestPrefetchRecordCarriesSegmentTiming(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)

	dir := t.TempDir()
	line := "where should a read-only prefetch attach to the editor"
	p := chatPrefetchFor(dir, func() string { return line }, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	query, ok := p.session.Claim(line, time.Now(), p.cfg)
	if !ok {
		t.Fatal("the gate refused a line it should accept")
	}
	p.fetch(query)

	lines := readPrefetchSegments(t, dir)
	var lookup *prefetchRecordLine
	for i := range lines {
		if lines[i].Outcome != prefetchOutcomeAdopted {
			lookup = &lines[i]
		}
	}
	if lookup == nil {
		t.Fatalf("the record carried no lookup line: %+v", lines)
	}
	if lookup.Outcome != prefetchOutcomeHits {
		t.Fatalf("outcome = %q, want %q (err=%s)", lookup.Outcome, prefetchOutcomeHits, lookup.Err)
	}
	if lookup.Segments == nil {
		t.Fatal("the record carries no segments — the 1500ms question is unanswerable from it")
	}
	seg := lookup.Segments
	if seg.JournalMode == "" {
		t.Error("the record does not report the connection's journal_mode — the WAL fact a reader needs")
	}
	if seg.QueryOnly != 1 {
		t.Errorf("query_only = %d, want 1: the read-only contract must be OBSERVED on the connection, not assumed from the DSN", seg.QueryOnly)
	}
	if seg.BusyTimeout != prefetchLedgerBusyTimeoutMs {
		t.Errorf("busy_timeout_ms = %d, want %d (the DSN's own pragma, read back)", seg.BusyTimeout, prefetchLedgerBusyTimeoutMs)
	}
	if seg.Handle != prefetchHandleSession {
		t.Errorf("handle = %q, want %q: an executor lookup runs on the session's warmed handle", seg.Handle, prefetchHandleSession)
	}
	if seg.OpenMs != prefetchSegNoOpenMs {
		t.Errorf("seg_open_ms = %d, want %d: a session lookup performs no dial, so reporting one would be inventing a measurement", seg.OpenMs, prefetchSegNoOpenMs)
	}
	// THE IDENTITY, NOT A TOLERANCE. The lookup runs on ONE clock: its segments
	// are spans of it and seg_query_ms is its remainder, so their sum accounts
	// for `ms` EXACTLY — no rounding unit either side. A tolerance here is what
	// let the real defect hide: the pre-fix shape took `ms` from the CALLER's
	// clock and the segments from its own three, and the residual that opened up
	// under load was read as "measurement noise" instead of as the record failing
	// to account for its own time (measured 2026-10-07, load ~15: residual up to
	// +2, ~6% of rounds on the fresh shape). The assertion is exact because the
	// code now is.
	sum := seg.StatMs + seg.OpenMs + seg.QueryMs
	if sum != lookup.Ms {
		t.Errorf("ms = %d but seg_stat_ms+seg_open_ms+seg_query_ms = %d (%d, %d, %d): the segments must account for the measured time exactly — they are spans of one clock, and seg_query_ms is its remainder",
			lookup.Ms, sum, seg.StatMs, seg.OpenMs, seg.QueryMs)
	}
	if seg.Attempts < 1 {
		t.Errorf("attempts = %d, want >= 1", seg.Attempts)
	}
}

// TestPrefetchSessionHandlePaysNoDial is the property the fix rests on: the
// SECOND lookup on one handle does no dial and no pragma round-trip, while the
// first (fresh) lookup does. It is measured, not asserted from the code shape —
// the two records are compared field by field.
func TestPrefetchSessionHandlePaysNoDial(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	cfg := memoryprefetch.DefaultConfig()
	q := "read-only ledger DSN busy timeout"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := prefetchOpenHandle(ctx, path)
	if err != nil {
		t.Fatalf("warm the read-only handle: %v", err)
	}
	defer h.close()
	if h.journalMode == "" {
		t.Fatal("the warmed handle reports no journal_mode: the state was not read back")
	}
	if h.openMs < 0 {
		t.Fatalf("handle warm ms = %d, want >= 0", h.openMs)
	}

	first := prefetchLookupDetailed(context.Background(), q, cfg, h, nil)
	second := prefetchLookupDetailed(context.Background(), q, cfg, h, nil)
	for i, r := range []prefetchResult{first, second} {
		if r.Outcome != prefetchOutcomeHits {
			t.Fatalf("lookup %d: outcome = %q (err=%v)", i+1, r.Outcome, r.Err)
		}
		if r.Seg.Handle != prefetchHandleSession {
			t.Errorf("lookup %d: handle = %q, want %q", i+1, r.Seg.Handle, prefetchHandleSession)
		}
		if r.Seg.OpenMs != prefetchSegNoOpenMs {
			t.Errorf("lookup %d: seg_open_ms = %d on a reused handle", i+1, r.Seg.OpenMs)
		}
		if r.Seg.HandleWarmMs != h.openMs {
			t.Errorf("lookup %d: handle_warm_ms = %d, want the handle's own %d (the cost is reported once, as itself)",
				i+1, r.Seg.HandleWarmMs, h.openMs)
		}
		if r.Seg.QueryOnly != 1 {
			t.Errorf("lookup %d: query_only = %d, want 1", i+1, r.Seg.QueryOnly)
		}
	}

	// The FRESH shape — the pre-fix lifetime — pays the dial itself. That is the
	// difference this test exists to pin: same query, same ledger, one shape
	// dials inside its budget and the other does not.
	fresh := prefetchLookupDetailed(context.Background(), q, cfg, nil, nil)
	if fresh.Outcome != prefetchOutcomeHits {
		t.Fatalf("fresh lookup: outcome = %q (err=%v)", fresh.Outcome, fresh.Err)
	}
	if fresh.Seg.Handle != prefetchHandleFresh {
		t.Errorf("fresh lookup: handle = %q, want %q", fresh.Seg.Handle, prefetchHandleFresh)
	}
	if fresh.Seg.HandleWarmMs != 0 {
		t.Errorf("fresh lookup: handle_warm_ms = %d, want 0 — no session handle was warmed", fresh.Seg.HandleWarmMs)
	}
}

// TestPrefetchSegmentNamesThePhaseThatFailed is the attribution guard (acceptance
// a's second half). Each sub-case can only fail in ONE phase, so the segment the
// record names is either right or the record is lying:
//
//   - a ledger path that exists but is not a database cannot be opened at all;
//   - an already-expired deadline cannot be the search's own doing.
func TestPrefetchSegmentNamesThePhaseThatFailed(t *testing.T) {
	cfg := memoryprefetch.DefaultConfig()

	t.Run("unopenable ledger is attributed to open", func(t *testing.T) {
		dir := t.TempDir()
		// A real file that is NOT a SQLite database: the lookup's stat succeeds
		// (so the ledger LOOKS present) and the dial fails — the only phase that
		// can be responsible.
		path := filepath.Join(dir, "not-a-ledger.db")
		if err := os.WriteFile(path, []byte("this file is not a sqlite database"), 0o600); err != nil {
			t.Fatalf("write the decoy ledger: %v", err)
		}
		t.Setenv("GOPI_MEMORY_DB", path)

		r := prefetchLookupDetailed(context.Background(), "anything at all", cfg, nil, nil)
		if r.Err == nil {
			t.Fatal("a decoy ledger produced no error")
		}
		if r.Outcome != prefetchOutcomeError {
			t.Errorf("outcome = %q, want %q (err=%v)", r.Outcome, prefetchOutcomeError, r.Err)
		}
		if r.Segment != prefetchSegOpen {
			t.Errorf("segment = %q, want %q: the phase that failed was the dial (err=%v)", r.Segment, prefetchSegOpen, r.Err)
		}
	})

	t.Run("expired deadline is attributed to query", func(t *testing.T) {
		path := seedPrefetchLedger(t)
		t.Setenv("GOPI_MEMORY_DB", path)
		cfg := memoryprefetch.DefaultConfig()

		// The SESSION shape is where this question matters: the handle is
		// already warmed, so the lookup's work is the query and nothing else —
		// which is exactly why a deadline that expired cannot be blamed on a
		// dial.
		h, err := prefetchOpenHandle(context.Background(), path)
		if err != nil {
			t.Fatalf("warm the read-only handle: %v", err)
		}
		defer h.close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already expired: the search cannot possibly have run
		r := prefetchLookupDetailed(ctx, "read-only ledger DSN", cfg, h, nil)
		if r.Outcome != prefetchOutcomeGaveUp {
			t.Fatalf("outcome = %q, want %q (err=%v)", r.Outcome, prefetchOutcomeGaveUp, r.Err)
		}
		if r.Segment != prefetchSegQuery {
			t.Errorf("segment = %q, want %q (err=%v)", r.Segment, prefetchSegQuery, r.Err)
		}
		// The give-up must NAME the segment: a bare "context deadline exceeded"
		// is the shape this task was filed about.
		if r.Err == nil || !strings.Contains(r.Err.Error(), prefetchSegQuery) {
			t.Errorf("the give-up error does not name its segment: %v", r.Err)
		}
	})
}

// TestPrefetchHandleRefusesToWriteTheLedger is contract ① measured rather than
// trusted: the read-only contract in a DSN is a REQUEST, so this test asks the
// warmed handle to write and requires the refusal. `query_only=1` in the record
// is the same fact observed on the connection; this is the fact observed by
// trying.
func TestPrefetchHandleRefusesToWriteTheLedger(t *testing.T) {
	path := seedPrefetchLedger(t)
	h, err := prefetchOpenHandle(context.Background(), path)
	if err != nil {
		t.Fatalf("warm the read-only handle: %v", err)
	}
	defer h.close()

	for _, stmt := range []string{
		"CREATE TABLE prefetch_must_not_write (id INTEGER)",
		"INSERT INTO distilled_qa (topic, question, answer) VALUES ('x','y','z')",
		"DELETE FROM distilled_qa",
	} {
		if _, err := h.db.Exec(stmt); err == nil {
			t.Fatalf("the prefetch handle EXECUTED a write (%q) — contract ① is broken", stmt)
		}
	}
	// The ledger must be intact: the fixture's own rows are still there.
	var n int
	if err := h.db.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatalf("read the ledger back: %v", err)
	}
	if n == 0 {
		t.Fatal("the fixture's rows are gone after the refused writes")
	}
}

// prefetchSealNoWaitBound is how long the adoption path may take while a lookup
// is in flight against a LOCKED ledger. It is a bound on a mutex-guarded
// in-memory decision, not on I/O, so it is deliberately tiny — an adoption point
// that waited for the lookup would exceed it by three orders of magnitude (the
// lookup cannot finish before its own budget expires).
const prefetchSealNoWaitBound = 250 * time.Millisecond

// TestPrefetchSealDoesNotWaitForAnInFlightLookup is contract ③ measured: the
// adoption point a sealed turn goes through must not wait for the lookup. The
// ledger is held EXCLUSIVE, so a lookup started here cannot finish inside this
// test's window — and the adoption must still return promptly, because the
// alternative is the operator's Enter parked behind a busy ledger.
func TestPrefetchSealDoesNotWaitForAnInFlightLookup(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)

	dir := t.TempDir()
	line := "seal the turn even while the ledger is locked"
	p := chatPrefetchFor(dir, func() string { return line }, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	holder, err := sql.Open(prefetchDriverName, tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("open the lock holder: %v", err)
	}
	defer holder.Close()
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE: %v", err)
	}
	defer func() {
		if _, err := holder.Exec("ROLLBACK"); err != nil {
			t.Logf("rollback the fixture lock: %v", err)
		}
	}()

	query, ok := p.session.Claim(line, time.Now(), p.cfg)
	if !ok {
		t.Fatal("the gate refused a line it should accept")
	}
	// A lookup that cannot finish: it will retry against the held lock for the
	// whole budget.
	go p.fetch(query)

	started := time.Now()
	in := chatOutbound{Message: line, Display: line}
	out, _ := chatPrefetchAdopt(in, p, line)
	elapsed := time.Since(started)

	// Two facts, both required: the seal returned fast, and it returned the
	// operator's own message untouched (Display is never edited, and the
	// Message is not appended to when there is no section in hand).
	if elapsed > prefetchSealNoWaitBound {
		t.Errorf("the adoption took %s against an in-flight lookup — the seal is waiting for the ledger", elapsed)
	}
	if out.Display != line {
		t.Errorf("the adoption edited the operator's line: %q", out.Display)
	}
	if out.Message != line {
		t.Errorf("the adoption appended to the message with no section in hand: %q", out.Message)
	}
}

// TestPrefetchTopNShapeKeepsProductionRanking guards the SQL change: the
// TOP-N form must return the SAME rows in the SAME order as the flat
// `JOIN ... ORDER BY bm25(...) LIMIT ?` production used, on a fixture the FTS
// planner has a real choice about.
func TestPrefetchTopNShapeKeepsProductionRanking(t *testing.T) {
	path := seedPrefetchLedger(t)
	db, err := sql.Open(prefetchDriverName, tools.LedgerReadOnlyDSNIn(path, prefetchLedgerBusyTimeoutMs))
	if err != nil {
		t.Fatalf("open the fixture read-only: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// The pre-fix SQL, kept here as the comparison datum — it is what "the same
	// answer" is measured against.
	const flatShapeSQL = `SELECT d.id, d.topic, d.question, d.answer, COALESCE(d.tier,''), COALESCE(d.confidence,0)
FROM distilled_qa_fts f
JOIN distilled_qa d ON d.id = f.rowid
WHERE distilled_qa_fts MATCH ?
ORDER BY bm25(distilled_qa_fts), d.confidence DESC
LIMIT ?`

	match := ftsMatchQuery("read-only ledger DSN busy timeout prefetch")
	read := func(sqlText string) []int64 {
		rows, err := db.Query(sqlText, match, prefetchHitLimit)
		if err != nil {
			t.Fatalf("run the shape: %v", err)
		}
		defer rows.Close() //stylegate:ignore closed at the end of its own iteration
		var ids []int64
		for rows.Next() {
			var id int64
			var topic, q, a, tier string
			var conf float64
			if err := rows.Scan(&id, &topic, &q, &a, &tier, &conf); err != nil {
				t.Fatalf("scan: %v", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		return ids
	}
	flat := read(flatShapeSQL)
	topN := read(prefetchSearchSQL)
	if len(flat) == 0 {
		t.Fatal("the fixture produced no hits: the comparison would be vacuous")
	}
	if len(flat) != len(topN) {
		t.Fatalf("row counts differ: flat=%d topN=%d", len(flat), len(topN))
	}
	for i := range flat {
		if flat[i] != topN[i] {
			t.Fatalf("the top-N shape reordered the answer: flat=%v topN=%v", flat, topN)
		}
	}
}

// TestPrefetchBusyIsRetriedWithinTheSameBudget pins the fix for the SECOND defect
// the task names: a read-only connection reporting SQLITE_BUSY. The lock is a
// state, not a stressor (Rule 27 — no load is created). Two shapes are measured
// under the SAME held lock, because they answer different questions:
//
//   - the SESSION shape (a handle warmed BEFORE the lock was taken) meets the
//     lock in its QUERY, and the retry loop must let it through or give up
//     within the budget;
//   - the FRESH shape meets it at the DIAL, which is where its own work begins,
//     and the record must SAY that rather than reporting a generic expiry.
//
// The assertion that matters is the wall clock: a retry loop that ignored the
// goroutine's deadline would show up as a duration above the budget.
func TestPrefetchBusyIsRetriedWithinTheSameBudget(t *testing.T) {
	path := openPrefetchLockRigLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	cfg := memoryprefetch.DefaultConfig()

	// The ledger is built in the mode this test MEASURES, from its first
	// connection: under WAL a read-only reader is never refused (it takes a
	// consistent snapshot) and the give-up below would be unobservable.
	//
	// It is NOT put there by a post-hoc `PRAGMA journal_mode = DELETE` on the
	// holder any more. That flip needs EXCLUSIVE access to the file, and it is
	// refused with SQLITE_BUSY while any other connection is open on it — this
	// rig's own seed is still pooled at that instant, which made the flip a race
	// this test lost in a full-package run. Measured, and fixed by asking for the
	// mode in the DSN instead: chat_prefetch_dsn_rig_test.go.
	assertLockRigLedgerIsInRollbackJournal(t, path)
	holder, err := sql.Open(prefetchDriverName, tools.LedgerRollbackJournalDSN(path))
	if err != nil {
		t.Fatalf("open the lock holder: %v", err)
	}
	defer holder.Close()
	holder.SetMaxOpenConns(1)

	// Warm the session handle AFTER the journal is back in rollback mode, and
	// BEFORE the lock is taken: the lookup below must meet the lock in its
	// query, not in its dial.
	h, err := prefetchOpenHandle(context.Background(), path)
	if err != nil {
		t.Fatalf("warm the read-only handle: %v", err)
	}
	defer h.close()

	// A writer holding the ledger EXCLUSIVE. rollback journal + BEGIN EXCLUSIVE
	// is the shape the seam measurement (devdoc/reports/memory-prefetch-seams.md
	// §5) used to prove a read-only connection returns SQLITE_BUSY instead of
	// blocking forever.
	if _, err := holder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE: %v", err)
	}
	defer func() {
		if _, err := holder.Exec("ROLLBACK"); err != nil {
			t.Logf("rollback the fixture lock: %v", err)
		}
	}()

	budget := time.Duration(prefetchGoroutineTimeoutMs) * time.Millisecond

	started := time.Now()
	warm := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", cfg, h, nil)
	warmElapsed := time.Since(started)
	if warm.Outcome != prefetchOutcomeGaveUp {
		t.Fatalf("session shape: outcome = %q, want %q (err=%v)", warm.Outcome, prefetchOutcomeGaveUp, warm.Err)
	}
	if !isBusyErr(warm.Err) {
		t.Fatalf("session shape: the give-up is not a busy refusal: %v", warm.Err)
	}
	if warm.Segment != prefetchSegQuery {
		t.Errorf("session shape: segment = %q, want %q — the handle was already warmed, so the lock was met by the query (err=%v)",
			warm.Segment, prefetchSegQuery, warm.Err)
	}
	if warm.Seg.Attempts != prefetchBusyRetryMax+1 {
		t.Errorf("session shape: attempts = %d, want %d — a locked ledger must be retried, not refused on the first refusal",
			warm.Seg.Attempts, prefetchBusyRetryMax+1)
	}
	if warmElapsed > budget {
		t.Errorf("session shape took %s, above its own %s budget: the retries did not respect the deadline", warmElapsed, budget)
	}
	// A read-only connection must never have been able to write its way out.
	if warm.Seg.QueryOnly != 1 {
		t.Errorf("session shape: query_only = %d, want 1", warm.Seg.QueryOnly)
	}

	// The FRESH shape under the same lock: its dial is blocked, and the record
	// has to say so — this is precisely the misdiagnosis the task was filed on
	// ("context deadline exceeded" for a ledger that was simply locked).
	freshStarted := time.Now()
	fresh := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", cfg, nil, nil)
	freshElapsed := time.Since(freshStarted)
	if fresh.Outcome != prefetchOutcomeGaveUp {
		t.Errorf("fresh shape: outcome = %q, want %q (err=%v)", fresh.Outcome, prefetchOutcomeGaveUp, fresh.Err)
	}
	if fresh.Segment != prefetchSegOpen {
		t.Errorf("fresh shape: segment = %q, want %q (err=%v)", fresh.Segment, prefetchSegOpen, fresh.Err)
	}
	if fresh.Seg.Attempts < 1 {
		t.Errorf("fresh shape: attempts = %d, want >= 1 (the dial must be retried too, or a session that loses the race dials fresh forever)", fresh.Seg.Attempts)
	}
	if freshElapsed > 2*budget {
		t.Errorf("fresh shape took %s against a locked ledger — it must give up within its own bound, not the ledger's 15s busy timeout", freshElapsed)
	}
}

// prefetchBusyArmTries is how many independent takes the arm-order guard below
// performs against a ledger a writer holds EXCLUSIVE. The overlap it exploits is
// a RACE (whether the refusal or the expired deadline is the LAST thing the loop
// sees), so one take can pass on the passing arm by luck: the defect was measured
// at 5 red rounds in 6, i.e. a single take is green about once in six and a
// twelve-take run misses it about once in three billion. The budget is set at
// DOUBLE that measured number because the rate is a property of the host's load,
// not of the code — under a quiet machine the racy arm is met less often, and the
// takes are independent (each its own lookup and its own clock, no shared state),
// so the rate stands on its own rather than being a repeat of one schedule.
//
// The cost is the reason it is not larger: a locked session lookup spends its
// whole retry allowance, so a take costs roughly a second and the guard must stay
// a seconds-long test rather than a minutes-long one.
const prefetchBusyArmTries = 24

// TestPrefetchBusyGiveUpIsReportedAsBusy never lets the deadline arm win over a
// live lock refusal. It is the regression guard for the arm ORDER measured
// 2026-10-07 (task 01a115b6…, whose intent this task reworks onto current
// master): the query error switch tested `runCtx.Err() != nil` BEFORE
// `isBusyErr(err)`, and the two are true for the SAME error — the retries spend
// the DSN's own busy timeout (prefetchLedgerBusyTimeoutMs, deliberately not
// production's 15s), so the last attempt is refused inside its own wait while
// the lookup's own deadline expires in parallel. The old order therefore printed
// `prefetch gave up after 1500ms (query): context deadline exceeded` for a
// ledger that was LOCKED — a reader sent after "slow" when the truth was "held".
//
// The fixture is a HARD lock that outlives the lookup's whole budget, so the two
// conditions necessarily overlap and the ORDER is what decides the report. That
// the refusal is what the loop actually returns is measured HERE rather than
// assumed: the loop takes a retry after each refusal, so the last error is the
// last REFUSAL — unless the deadline cuts a retry short first, which is exactly
// the flake this pins.
func TestPrefetchBusyGiveUpIsReportedAsBusy(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	path := openPrefetchLockRigLedger(t)
	cfg := memoryprefetch.DefaultConfig()

	// The ledger is built in the rollback journal from its first connection: under
	// WAL a read-only reader is never refused, and the refusal this test measures
	// would be unobservable. No post-hoc flip is used — a journal_mode flip needs
	// exclusive access to the file and is refused while the fixture's own seed is
	// still pooled (measured: chat_prefetch_dsn_rig_test.go).
	assertLockRigLedgerIsInRollbackJournal(t, path)
	holder, err := sql.Open(prefetchDriverName, tools.LedgerRollbackJournalDSN(path))
	if err != nil {
		t.Fatalf("open the lock holder: %v", err)
	}
	defer holder.Close()
	holder.SetMaxOpenConns(1)

	h, err := prefetchOpenHandle(context.Background(), path)
	if err != nil {
		t.Fatalf("warm the read-only handle: %v", err)
	}
	defer h.close()

	if _, err := holder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE: %v", err)
	}
	defer func() {
		if _, err := holder.Exec("ROLLBACK"); err != nil {
			t.Logf("rollback the fixture lock: %v", err)
		}
	}()

	refusals, notBusy := 0, 0
	for i := 0; i < prefetchBusyArmTries; i++ {
		res := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", cfg, h, nil)
		if res.Outcome != prefetchOutcomeGaveUp {
			t.Fatalf("try %d: outcome = %q, want %q (err=%v)", i, res.Outcome, prefetchOutcomeGaveUp, res.Err)
		}
		if res.Segment != prefetchSegQuery {
			t.Fatalf("try %d: segment = %q, want %q — the handle was warmed, so the lock is met by the query (err=%v)",
				i, res.Segment, prefetchSegQuery, res.Err)
		}
		if !isBusyErr(res.Err) {
			notBusy++
			if notBusy == 1 {
				t.Errorf("try %d: a LOCKED ledger was reported as something else — the busy arm must be judged BEFORE the deadline arm, since both are true for the same error: %v",
					i, res.Err)
			}
			continue
		}
		refusals++
	}
	t.Logf("%d takes against a held EXCLUSIVE lock: %d reported the refusal, %d did not", prefetchBusyArmTries, refusals, notBusy)
}

// prefetchRetryGuardTries is the retry-loop guard's own budget: the loop under
// test retries with prefetchBusyRetryDelayMs between attempts and the deadline
// that ends it is prefetchLedgerBusyTimeoutMs long, so a handful of takes is
// enough to reach every arm — the assertion is about WHICH error comes back,
// never about how long the loop may run, and a long run would be a cost with no
// additional fact.
const prefetchRetryGuardTries = 8

// errPrefetchBusySentinel is a lock refusal with no database behind it. It is
// spelled exactly as the driver spells one ("database is locked (5)
// (SQLITE_BUSY)") because that is the vocabulary tools.LedgerBusyErr reads, and
// driving the loop directly keeps this guard from inheriting a fixture's timing:
// what is under test is the loop's bookkeeping, not SQLite's.
var errPrefetchBusySentinel = errors.New("database is locked (5) (SQLITE_BUSY)")

// TestPrefetchRetrySpendsItsCapAndStopsOnTheRefusal pins the retry loop's two
// contracts, neither of which is a wall-clock assertion.
//
// It reworks the surviving half of the attached branch's
// TestPrefetchRetryRespectsItsCapAndKeepsTheReason (task 01a115b6…): the loop
// must spend its full cap — the bound IS the cap, so refusing a retry "for want
// of room" would not shorten the lookup, it would only stop the attempts
// counter, and the lock contract requires prefetchBusyRetryMax+1 attempts —
// while every attempt stays bounded by the CALLER's deadline.
//
// The attached guard's other half — that a lock met on an earlier attempt
// outlives the deadline as the reported reason — is deliberately NOT restated.
// Master measured that a retry can be refused by the context before it reaches
// SQLite, and deliberately removed the loop's memo so the caller reports the
// LAST error it saw; the arm order pinned above is what keeps a refusal from
// being read as a deadline. Restoring the memo here would be re-applying the
// stale diff rather than re-authoring against master.
func TestPrefetchRetrySpendsItsCapAndStopsOnTheRefusal(t *testing.T) {
	// A roomy deadline must not shorten the loop: a "budget" fix that simply
	// stopped retrying would pass a give-up assertion and fail here.
	for i := 0; i < prefetchRetryGuardTries; i++ {
		roomy, cancelRoomy := context.WithTimeout(context.Background(), time.Duration(prefetchGoroutineTimeoutMs)*time.Millisecond)
		attempts, err := prefetchRetryOnBusy(roomy, func() error {
			return errPrefetchBusySentinel
		})
		cancelRoomy()
		if err == nil {
			t.Fatalf("try %d: a retry loop against a refused ledger returned no error — a refusal may never be reported as success", i)
		}
		if !isBusyErr(err) {
			t.Fatalf("try %d: the give-up error is %v — with the whole budget available the refusal it met IS the last error, and the caller's arm order depends on that", i, err)
		}
		if attempts != prefetchBusyRetryMax+1 {
			t.Fatalf("try %d: attempts = %d, want %d — the bound is the RETRY CAP; refusing on the first refusal is not a retry", i, attempts, prefetchBusyRetryMax+1)
		}
	}

	// A deadline that expires INSIDE the loop's own wait must still end it —
	// the bound is the caller's deadline, not the ledger's 15s busy timeout —
	// and the loop still reports a failure rather than a fabricated success.
	tightening, cancelTightening := context.WithTimeout(context.Background(), time.Duration(prefetchLedgerBusyTimeoutMs/2)*time.Millisecond)
	defer cancelTightening()
	attempts, err := prefetchRetryOnBusy(tightening, func() error {
		return errPrefetchBusySentinel
	})
	if err == nil {
		t.Fatal("a retry loop whose deadline expired mid-wait returned no error — a refusal may never be reported as success")
	}
	if attempts < 1 {
		t.Errorf("attempts = %d, want at least 1", attempts)
	}
}
