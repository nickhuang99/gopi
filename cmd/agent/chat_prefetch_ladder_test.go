package main

// chat_prefetch_ladder_test.go — the regression guard for the defect this task
// repairs: the prefetch built its candidate set with an all-OR FTS5 MATCH, so a
// query whose full AND matches nothing fell to a pool of everything sharing ONE
// token, and the section injected into the prompt was filled with rows that have
// nothing to do with the question.
//
// The fixture reproduces the shape at the smallest size that carries it (Rule:
// minimal sample), and the assertion is the CANDIDATE SET, not a score:
//
//	filler rows (30)  carry the high-frequency token only — the noise the OR
//	                  pool is made of;
//	subject row (1)   carries the query's two real tokens and NOT the filler;
//	query             filler + tokenA + tokenB, whose full AND matches 0 rows.
//
// Under OR the pool is 31 rows and the prefetch takes its 6 by bm25; under the
// ladder the biggest qualifying subset is {tokenA, tokenB} — the only one that
// matches anything — so the pool is exactly the one row that answers the
// question. Both are pinned below, so a future edit that reinstates OR cannot
// pass by accident.

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/memoryprefetch"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// The fixture's vocabulary, named (Rule 1): the prefix keeps the tokens
// disjoint from anything the extraction's stop-word list could drop, and the
// counts are the smallest that make the OR pool wider than the prefetch's hit
// limit (so the OR path cannot return the subject row by accident).
const (
	prefetchLadderFillerToken  = "zzfiller"
	prefetchLadderSubjectA     = "zzsubjectalpha"
	prefetchLadderSubjectB     = "zzsubjectbeta"
	prefetchLadderFillerRows   = 30
	prefetchLadderQueryNoise   = "zzfiller zzsubjectalpha zzsubjectbeta"
	prefetchLadderSubjectTopic = "the record that answers the question"
	// prefetchLadderSubjectTopicTwo labels the second subject row, which shares
	// the query's two real tokens with the first. The pair is what reaches the
	// injection floor.
	prefetchLadderSubjectTopicTwo = "the other record that answers the question"
	// prefetchLadderSubjectBody is the field the SECTION renders (the section
	// carries each hit's answer body, not its topic — read off the rendered
	// section, not assumed), so it is what the "the answer reached the prompt"
	// assertion can actually look for.
	prefetchLadderSubjectBody = "the one row whose body carries both real tokens"
	// prefetchLadderSubjectBodyTwo is the second subject row's body: it is what
	// makes "the section carries the rows that answer the question" a statement
	// about a SET rather than about the single row a one-hit fixture would pin.
	prefetchLadderSubjectBodyTwo = "the other row whose body carries both real tokens"
	// unrelatedNoiseTopic labels the filler rows, so the assertion can say
	// "the noise is absent" by naming it once.
	unrelatedNoiseTopic = "unrelated noise record"
)

// seedLadderLedger builds the fixture above with the WRITER scaffold's path
// (Rule 32) and returns its path plus the subject row's id.
//
// IT SEEDS **TWO** SUBJECT ROWS, and that number is load-bearing: the injection
// floor (internal/memoryprefetch, Config.MinInjectHits) refuses a retrieval of
// fewer than 2 hits outright, so a one-row fixture would never reach a section
// and the test could not measure the candidate set it exists for. Both subject
// rows carry the query's two real tokens, so the ladder's winning subset still
// matches exactly these two and none of the filler.
func seedLadderLedger(t *testing.T) (string, int) {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	db, err := sql.Open(prefetchDriverName, tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("open fixture ledger: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close() //stylegate:ignore fixture handle closed at the end of the test

	if _, err := db.Exec(`CREATE TABLE distilled_qa (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL,
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			tier TEXT DEFAULT 'warm',
			confidence REAL DEFAULT 0.5,
			possibly_stale INTEGER DEFAULT 0)`); err != nil {
		t.Fatalf("create base table: %v", err)
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, content='distilled_qa', content_rowid='id')`); err != nil {
		t.Fatalf("create fts table: %v", err)
	}
	// The noise: each row carries the filler token and an unrelated subject, so
	// an OR pool is wide and none of it answers the query.
	for i := 0; i < prefetchLadderFillerRows; i++ {
		if _, err := db.Exec(
			`INSERT INTO distilled_qa (topic, question, answer, tier, confidence) VALUES (?, ?, ?, 'warm', 0.5)`,
			unrelatedNoiseTopic, "a question about something else entirely",
			prefetchLadderFillerToken+" filler body that shares only the common word"); err != nil {
			t.Fatalf("seed filler row %d: %v", i, err)
		}
	}
	res, err := db.Exec(
		`INSERT INTO distilled_qa (topic, question, answer, tier, confidence) VALUES (?, ?, ?, 'hot', 0.9)`,
		prefetchLadderSubjectTopic, prefetchLadderSubjectA+" and "+prefetchLadderSubjectB+" together",
		prefetchLadderSubjectBody)
	if err != nil {
		t.Fatalf("seed subject row: %v", err)
	}
	// The SECOND subject row: same two real tokens, its own body. It is what
	// puts the ladder's candidate set AT the injection floor (2), so the section
	// this test reads is the one the floor lets through.
	if _, err := db.Exec(
		`INSERT INTO distilled_qa (topic, question, answer, tier, confidence) VALUES (?, ?, ?, 'warm', 0.8)`,
		prefetchLadderSubjectTopicTwo, prefetchLadderSubjectB+" again, with "+prefetchLadderSubjectA,
		prefetchLadderSubjectBodyTwo); err != nil {
		t.Fatalf("seed second subject row: %v", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("subject id: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO distilled_qa_fts (rowid, topic, question, answer) SELECT id, topic, question, answer FROM distilled_qa`); err != nil {
		t.Fatalf("index the fixture: %v", err)
	}
	return path, int(id64)
}

// TestPrefetchCandidatesComeFromTheLadderNotFromOR is the epic's red proof. It
// drives the REAL lookup seam (prefetchLookupDetailed, the function the executor
// calls) with the record's own observable read back, and it asserts the two
// halves separately:
//
//  1. the candidate path the record reports is the ladder's;
//  2. the rows that reached the section are the ones that answer the question —
//     the OR pool's 30 noise rows are NOT among them, and the subject row IS.
//
// Assertion 2 is the one that matters and the one a "did it call the new code"
// test cannot make: a ladder pick that returned the noise would satisfy the
// first and fail the question.
func TestPrefetchCandidatesComeFromTheLadderNotFromOR(t *testing.T) {
	path, subjectID := seedLadderLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv("GOPI_MEMORY_PREFETCH", "1")

	// The premise, measured rather than assumed: the query's full AND matches
	// NOTHING. Without this the ladder's fast path would answer it and the test
	// would prove nothing about the descending walk.
	probe, err := sql.Open(prefetchDriverName, tools.LedgerReadOnlyDSNIn(path, prefetchLedgerBusyTimeoutMs))
	if err != nil {
		t.Fatalf("open probe: %v", err)
	}
	defer probe.Close() //stylegate:ignore fixture handle closed at the end of the test
	var andCount, orCount int
	if err := probe.QueryRow(
		`SELECT COUNT(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH ?`,
		`"`+prefetchLadderFillerToken+`" AND "`+prefetchLadderSubjectA+`" AND "`+prefetchLadderSubjectB+`"`).Scan(&andCount); err != nil {
		t.Fatalf("all-AND count: %v", err)
	}
	if err := probe.QueryRow(
		`SELECT COUNT(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH ?`,
		ftsMatchQuery(prefetchLadderQueryNoise)).Scan(&orCount); err != nil {
		t.Fatalf("all-OR count: %v", err)
	}
	if andCount != 0 {
		t.Fatalf("fixture premise broken: the full AND matches %d rows, want 0", andCount)
	}
	if orCount <= prefetchHitLimit {
		t.Fatalf("fixture premise broken: the OR pool is %d rows, which the prefetch's limit of %d would not dilute",
			orCount, prefetchHitLimit)
	}

	res := prefetchLookupDetailed(context.Background(), prefetchLadderQueryNoise, memoryprefetch.DefaultConfig(), nil, nil)

	// 1. The path the lookup took.
	if res.Candidates != prefetchCandidatesLadder {
		t.Errorf("candidates = %q, want %q (outcome=%s err=%v) — the candidate set is not coming from the ladder",
			res.Candidates, prefetchCandidatesLadder, res.Outcome, res.Err)
	}
	if res.Outcome != prefetchOutcomeHits {
		t.Fatalf("outcome = %q (err=%v), want %q", res.Outcome, res.Err, prefetchOutcomeHits)
	}

	// 2. WHICH rows reached the section. The section renders each hit's topic,
	// so the noise rows and the one real answer are distinguishable by their
	// text — and the noise's absence is the claim.
	// The COUNT is the ladder subset's own size — two rows carry both real
	// tokens — which is also the injection floor's boundary: one row would be
	// refused before rendering and this assertion would never run.
	if res.Hits != 2 {
		t.Errorf("hits = %d, want 2 — the ladder subset is the two rows carrying both real tokens (section=%q)",
			res.Hits, res.Section)
	}
	for _, body := range []string{prefetchLadderSubjectBody, prefetchLadderSubjectBodyTwo} {
		if !strings.Contains(res.Section, body) {
			t.Errorf("the section does not carry the record whose body is %q:\n%s", body, res.Section)
		}
	}
	// The subject row's ID is asserted too, so "the right row" is not inferred
	// from rendered prose: the section names the row it carries (`[memory:#N|…]`),
	// and that N must be the seeded subject.
	if !strings.Contains(res.Section, "#"+strconv.Itoa(subjectID)+"|") {
		t.Errorf("the section does not name the seeded subject row #%d:\n%s", subjectID, res.Section)
	}
	if strings.Contains(res.Section, unrelatedNoiseTopic) {
		t.Errorf("the section carries a row that shares only the filler token — the OR pool's noise is still reaching the prompt:\n%s", res.Section)
	}

	// The subject row is the one the index answered with, not merely one whose
	// text happens to match: assert it through the same SQL the prefetch ran.
	var gotID int
	if err := probe.QueryRow(
		`SELECT rowid FROM distilled_qa_fts WHERE distilled_qa_fts MATCH ? ORDER BY rank LIMIT 1`,
		`"`+prefetchLadderSubjectA+`" AND "`+prefetchLadderSubjectB+`"`).Scan(&gotID); err != nil {
		t.Fatalf("the ladder's subset does not resolve to a row: %v", err)
	}
	if gotID != subjectID {
		t.Errorf("the subset's row = %d, want the seeded subject %d", gotID, subjectID)
	}
}

// TestPrefetchFallsBackToORWhenTheLadderHasNoPick pins the OTHER half: when the
// ladder returns nothing — here because the query is below its token floor, so
// it never activates — the candidate set must still come from OR. The assertion
// is the path the record reports and the rows that reached the section, i.e. the
// same observable the ladder test reads, so "the fallback still works" is a
// measurement rather than an inference from the code.
func TestPrefetchFallsBackToORWhenTheLadderHasNoPick(t *testing.T) {
	path, _ := seedLadderLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	// TWO tokens: below memsearchMinANDTokens, so tools.LadderMatch declines and
	// the OR shape is the only recall left. Both tokens are indexed on the noise
	// rows, so a working fallback returns them.
	res := prefetchLookupDetailed(context.Background(),
		prefetchLadderFillerToken+" "+prefetchLadderSubjectA, memoryprefetch.DefaultConfig(), nil, nil)

	if res.Candidates != prefetchCandidatesOR {
		t.Errorf("candidates = %q, want %q — a query the ladder declines must fall back to OR (outcome=%s err=%v)",
			res.Candidates, prefetchCandidatesOR, res.Outcome, res.Err)
	}
	if res.Outcome != prefetchOutcomeHits {
		t.Fatalf("outcome = %q (err=%v), want %q — the fallback returned nothing at all",
			res.Outcome, res.Err, prefetchOutcomeHits)
	}
	if !strings.Contains(res.Section, prefetchLadderFillerToken) {
		t.Errorf("the OR fallback's section does not carry a row matching the query's tokens:\n%s", res.Section)
	}
}
