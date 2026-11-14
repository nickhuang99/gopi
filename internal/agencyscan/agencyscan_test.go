package agencyscan

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Fixture: a MINIMAL ledger (only the columns the miner reads). Rule 5:
// the regression the dedupe logic guards against is pinned here, on the
// smallest possible corpus.
// ---------------------------------------------------------------------------

const testLedgerSchema = `
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL UNIQUE,
    summary TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    tags TEXT DEFAULT '',
    payload TEXT DEFAULT ''
);
CREATE TABLE distilled_qa (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    question TEXT NOT NULL,
    retrieval_count INTEGER DEFAULT 0
);`

// newTestLedger creates a task-only ledger file and returns its path.
func newTestLedger(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open test ledger: %v", err)
	}
	if _, err := db.Exec(testLedgerSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() { db.Close() }) //stylegate:ignore test fixture handle
	return path, db
}

// seedTask inserts one ledger row.
func seedTask(t *testing.T, db *sql.DB, id, summary, status, tags string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO tasks (task_id, summary, status, tags, payload) VALUES (?,?,?,?,'')`,
		id, summary, status, tags); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// seedKnowledge inserts one knowledge row.
func seedKnowledge(t *testing.T, db *sql.DB, question string, rc int) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO distilled_qa (question, retrieval_count) VALUES (?,?)`, question, rc); err != nil {
		t.Fatalf("seed knowledge: %v", err)
	}
}

// recordSubmissions swaps the submit seam for a recorder for the duration of
// one test and returns the captured slice pointer.
func recordSubmissions(t *testing.T) *[]SubmitRequest {
	t.Helper()
	prev := SubmitFn
	var got []SubmitRequest
	SetSubmitFunc(func(req SubmitRequest) (string, error) {
		got = append(got, req)
		return fmt.Sprintf("proposal-%d", len(got)), nil
	})
	t.Cleanup(func() { SubmitFn = prev })
	return &got
}

// ---------------------------------------------------------------------------
// Pure-logic tests
// ---------------------------------------------------------------------------

// TestNormalizeAndKey_FoldVolatileTokens: two rows that differ only in an id,
// a hash or a number must fold to ONE recurrence key — that fold IS the
// pattern detector (an id-sensitive key would make every row a singleton).
func TestNormalizeAndKey_FoldVolatileTokens(t *testing.T) {
	a := "[auto] Fix the flaky merge test 01a08e41-a458-75c8-b329-33b02dd19003 run 3"
	b := "[auto] Fix the flaky merge test 01a08e42-b458-75c8-b329-33b02dd19004 run 7"
	if ka, kb := recurrenceKeyOf(a, "bug"), recurrenceKeyOf(b, "bug"); ka != kb {
		t.Fatalf("volatile tokens did not fold:\n %q\n %q", ka, kb)
	}
	// Provenance tags are runner identity, not pattern identity.
	if k1, k2 := recurrenceKeyOf(a, "bug,worker:zpeer"), recurrenceKeyOf(b, "bug,from-hp"); k1 != k2 {
		t.Fatalf("provenance tags leaked into the key: %q vs %q", k1, k2)
	}
	// Automation-owned rows are the dispatcher's own doings, never a pattern.
	if k1, k2 := recurrenceKeyOf(a, "bug,merge-conflict"), recurrenceKeyOf(b, "bug,auto-dispatch"); k1 == k2 {
		t.Fatalf("automation tags must be stripped before comparing: %q == %q", k1, k2)
	}
}

// TestDedupeFence_IsNotTheFingerprint: the fence must not contain the full
// fingerprint. If it did, a proposal row's own payload would satisfy its own
// dedupe probe and the pattern could never be re-proposed after the operator
// cancelled it — a silent permanent suppression.
func TestDedupeFence_IsNotTheFingerprint(t *testing.T) {
	fp := fingerprint("bug|fix the flaky merge test")
	fence := dedupeFence(fp)
	if strings.Contains(fence, fp) {
		t.Fatalf("fence %q contains the whole fingerprint %q — self-matching", fence, fp)
	}
	// Stability: the fence is a pure function of the key (byte-identical
	// across processes — a per-run salt would defeat dedupe entirely).
	if dedupeFence(fingerprint("k")) != dedupeFence(fingerprint("k")) {
		t.Fatal("fence is not deterministic")
	}
	// Distinct patterns must not collide at the chosen prefix length.
	seen := map[string]string{}
	for i := 0; i < 4096; i++ {
		key := fmt.Sprintf("bug|family number %d", i)
		f := dedupeFence(fingerprint(key))
		if prev, ok := seen[f]; ok {
			t.Fatalf("fingerprint prefix collision: %q shared by %q and %q", f, prev, key)
		}
		seen[f] = key
	}
}

// TestRenderSubmission_RefusesDegenerateHeadlines (Rule 15): a candidate whose
// excerpt cannot survive the headline budget must be REFUSED (ok=false), never
// silently submitted as a meaningless one-liner.
func TestRenderSubmission_RefusesDegenerateHeadlines(t *testing.T) {
	ok := candidate{Kind: KindRecurringFailure, RecurrenceKey: "|", Count: 5, FailedCount: 5}
	if _, _, good := renderSubmission(ok); good {
		t.Fatal("a candidate with no excerpt must be refused")
	}
	good := candidate{Kind: KindRecurringFailure, RecurrenceKey: "bug|fix flaky merge test", Count: 6, FailedCount: 6}
	s, p, good2 := renderSubmission(good)
	if !good2 {
		t.Fatal("a healthy candidate must render")
	}
	if !strings.Contains(s, "fix flaky merge test") || !strings.Contains(s, "n=6") {
		t.Fatalf("headline lost the evidence: %q", s)
	}
	if len(s) > maxProposalSummaryRunes {
		t.Fatalf("headline over budget (%d > %d): %q", len(s), maxProposalSummaryRunes, s)
	}
	if !strings.Contains(p, dedupeFence(fingerprint(good.RecurrenceKey))) {
		t.Fatal("payload must carry the dedupe fence")
	}
}

// TestMineTaskFamilies_ThresholdMatrix pins the four conservative gates.
func TestMineTaskFamilies_ThresholdMatrix(t *testing.T) {
	cases := []struct {
		name    string
		summary string
		status  string
		tags    string
		want    bool
	}{
		{"healthy family", "[auto] routine sync job", "completed", "", false},
		{"below size floor", "[auto] rare job", "failed", "", false},
		// 5 failures of one family: the positive case.
		{"recurring failure", "[auto] recurring broken job", "failed", "bug", true},
	}
	path, db := newTestLedger(t)
	// 4 healthy rows of the routine family (below the size floor).
	for i := 0; i < 4; i++ {
		seedTask(t, db, fmt.Sprintf("h%d", i), "[auto] routine sync job", "completed", "")
	}
	for i := 0; i < 3; i++ {
		seedTask(t, db, fmt.Sprintf("r%d", i), "[auto] rare job", "failed", "")
	}
	for i := 0; i < 5; i++ {
		seedTask(t, db, fmt.Sprintf("f%d", i), "[auto] recurring broken job", "failed", "bug")
	}
	// An in-flight member must disqualify an otherwise qualifying family.
	for i := 0; i < 5; i++ {
		seedTask(t, db, fmt.Sprintf("live%d", i), "[auto] half-done family", "failed", "")
	}
	seedTask(t, db, "live-x", "[auto] half-done family", "in_progress", "")
	// Harness self-reports recur by construction and are not briefings.
	for i := 0; i < 9; i++ {
		seedTask(t, db, fmt.Sprintf("d%d", i),
			fmt.Sprintf("[auto] t%d \u2014 elapsed=%d.0s iter=3 tokens=100", i, i), "completed", "")
	}

	got := map[string]bool{}
	res, err := Scan(context.Background(), Options{DBPath: path})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, c := range res.Candidates {
		got[c.RecurrenceKey] = true
	}
	for _, tc := range cases {
		key := recurrenceKeyOf(tc.summary, tc.tags)
		if got[key] != tc.want {
			t.Errorf("%s: kind=%q wanted candidate=%v, got %v", tc.name, key, tc.want, got[key])
		}
	}
	if got[recurrenceKeyOf("[auto] half-done family", "")] {
		t.Error("a family with an in-flight member must not be proposed")
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("report-only pass must expose exactly the qualifying family, got %d: %+v",
			len(res.Candidates), res.Candidates[0].RecurrenceKey)
	}
	if res.Proposed != 0 {
		t.Fatalf("report-only pass must not submit (proposed=%d)", res.Proposed)
	}
}

// ---------------------------------------------------------------------------
// Idempotence — THE acceptance test
// ---------------------------------------------------------------------------

// TestScan_IdempotentSecondPassHasZeroProposals: running the pass twice over
// the SAME ledger must propose N the first time and ZERO the second. This is
// the epic's stated acceptance criterion and the exact failure the
// merge-backlog branch-scan shipped (~600 duplicate rows) — so it is asserted
// on the DB, not on a mock.
func TestScan_IdempotentSecondPassHasZeroProposals(t *testing.T) {
	path, db := newTestLedger(t)
	for i := 0; i < 6; i++ {
		seedTask(t, db, fmt.Sprintf("f%d", i), "[auto] recurring broken job", "failed", "bug")
	}
	for i := 0; i < 3; i++ {
		seedKnowledge(t, db, "how do we verify a merge", 100+i)
	}
	// The recorder writes the row back into the ledger like TaskSubmit does
	// (a proposal IS a ledger row) — otherwise the second pass would have
	// nothing to dedupe against and the test would prove nothing.
	prev := SubmitFn
	SetSubmitFunc(func(req SubmitRequest) (string, error) {
		id := fmt.Sprintf("proposal-%s", shortHash(req.Summary))
		if _, err := db.Exec(
			`INSERT INTO tasks (task_id, summary, status, tags, payload) VALUES (?,?,?,?,?)`,
			id, req.Summary, "pending", req.Tags, req.Payload); err != nil {
			return "", err
		}
		return id, nil
	})
	defer func() { SubmitFn = prev }()

	first, err := Scan(context.Background(), Options{DBPath: path, Submit: true})
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if first.Proposed < 2 {
		t.Fatalf("first pass must propose both mined patterns, got %d (candidates=%d)",
			first.Proposed, len(first.Candidates))
	}

	second, err := Scan(context.Background(), Options{DBPath: path, Submit: true})
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Proposed != 0 {
		t.Fatalf("IDEMPOTENCE VIOLATED: second pass proposed %d row(s) over unchanged data", second.Proposed)
	}
	if second.Deduped != first.Proposed {
		t.Fatalf("second pass must dedupe exactly the first pass's proposals: deduped=%d proposed=%d",
			second.Deduped, first.Proposed)
	}

	// Third pass over the same data — still zero (no oscillation).
	third, err := Scan(context.Background(), Options{DBPath: path, Submit: true})
	if err != nil {
		t.Fatalf("third pass: %v", err)
	}
	if third.Proposed != 0 {
		t.Fatalf("third pass proposed %d row(s) — dedupe is not stable", third.Proposed)
	}
}

// TestProposalExists_TerminalRowStillSuppresses is the merge-backlog
// regression, transplanted: the branch-scan's coverage mask counted only LIVE
// rows, so a decision row that reached a TERMINAL state re-armed the branch
// and every tick minted a byte-identical row (~600 duplicates in the legacy
// ledger). A PROPOSAL row must suppress in every status — cancelled and
// completed included: the operator's "no" is permanent until the pattern
// itself changes.
func TestProposalExists_TerminalRowStillSuppresses(t *testing.T) {
	// The mined key is the recurrence-key form of the seeded headline (the
	// same function the miner uses — a hand-copied literal would drift).
	const seededSummary = "[auto] fix flaky merge test"
	key := recurrenceKeyOf(seededSummary, "bug")
	c := candidate{Kind: KindRecurringFailure, RecurrenceKey: key, Count: 6, FailedCount: 6}
	for _, status := range []string{"pending", "claimed", "in_progress", "completed", "failed", "cancelled", "verified"} {
		t.Run(status, func(t *testing.T) {
			path, db := newTestLedger(t)
			for i := 0; i < 6; i++ {
				seedTask(t, db, fmt.Sprintf("f%d", i), seededSummary, "failed", "bug")
			}
			// The prior proposal row, in a TERMINAL state.
			if _, err := db.Exec(
				`INSERT INTO tasks (task_id, summary, status, tags, payload) VALUES (?,?,?,?,?)`,
				"prior", "[auto] recurring failure: fix flaky merge test", status, "improvement,agency-proposal",
				"briefing\nDedupe fence (do not edit): "+dedupeFence(fingerprint(c.RecurrenceKey))+"\n"); err != nil {
				t.Fatalf("seed prior proposal: %v", err)
			}
			got := recordSubmissions(t)
			res, err := Scan(context.Background(), Options{DBPath: path, Submit: true})
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if len(*got) != 0 || res.Proposed != 0 {
				t.Fatalf("a %s proposal row must suppress re-proposal (proposed=%d)", status, res.Proposed)
			}
			if res.Deduped != 1 {
				t.Fatalf("the suppression must be COUNTED as deduped, got %d", res.Deduped)
			}
		})
	}
}

// TestScan_DisabledMeansNoWrite asserts the switch's contract at the package
// boundary: a pass without Submit performs zero writes and zero submits —
// disabled = zero behaviour change.
func TestScan_DisabledMeansNoWrite(t *testing.T) {
	path, db := newTestLedger(t)
	for i := 0; i < 6; i++ {
		seedTask(t, db, fmt.Sprintf("f%d", i), "[auto] recurring broken job", "failed", "bug")
	}
	got := recordSubmissions(t)
	before := countTasks(t, db)
	if _, err := Scan(context.Background(), Options{DBPath: path}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("report-only mode submitted %d row(s)", len(*got))
	}
	if after := countTasks(t, db); after != before {
		t.Fatalf("report-only mode changed the ledger: %d -> %d rows", before, after)
	}
}

// TestScan_MissingLedgerIsSoftFailure: the hook's fail-soft contract — a
// fresh home with no ledger must return an error, never panic.
func TestScan_MissingLedgerIsSoftFailure(t *testing.T) {
	if _, err := Scan(context.Background(), Options{DBPath: filepath.Join(t.TempDir(), "nope.db")}); err == nil {
		t.Fatal("a missing ledger must be reported as an error")
	}
}

func countTasks(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// shortHash is a test-local id generator (the fixture has no uuid mint).
func shortHash(s string) string {
	fp := fingerprint(s)
	return fp[:8]
}
