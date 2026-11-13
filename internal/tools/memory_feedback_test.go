package tools

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Beta posterior math (pure).
// ---------------------------------------------------------------------------

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestFbScoreBetaMath: the Beta-posterior feedback term — small-sample
// shrinkage (1 useful → +0.042), the min(N_eff/4,1) confidence gate
// (needs ~4 votes for full weight), contradiction → 0, and the ±0.5 cap.
func TestFbScoreBetaMath(t *testing.T) {
	cases := []struct {
		name                string
		useful, useless     float64
		wantScore, wantMean float64
	}{
		{"no feedback", 0, 0, 0, 0.5},
		{"one useful (gated)", 1, 0, 1.0 / 24.0, 2.0 / 3.0},
		{"one useless (gated)", 0, 1, -1.0 / 24.0, 1.0 / 3.0},
		{"three useful (still gated)", 3, 0, 0.225, 0.8},
		{"four useful (full gate)", 4, 0, 1.0 / 3.0, 5.0 / 6.0},
		{"ten useful (capped)", 10, 0, 5.0 / 12.0, 11.0 / 12.0},
		{"contradiction 1/1", 1, 1, 0, 0.5},
		{"contradiction 5/5", 5, 5, 0, 0.5},
		{"contradiction 3/1 (mean toward the prior, not 0.6)", 3, 1, 1.0 / 6.0, 2.0 / 3.0},
		{"old contested record 3 useful 1 useless", 3, 1, 1.0 / 6.0, 2.0 / 3.0},
	}
	for _, c := range cases {
		if got := fbScore(c.useful, c.useless); !approx(got, c.wantScore) {
			t.Errorf("%s: fbScore(%v,%v) = %.6f, want %.6f", c.name, c.useful, c.useless, got, c.wantScore)
		}
		if got := fbMean(c.useful, c.useless); !approx(got, c.wantMean) {
			t.Errorf("%s: fbMean(%v,%v) = %.6f, want %.6f", c.name, c.useful, c.useless, got, c.wantMean)
		}
	}
}

// ---------------------------------------------------------------------------
// vote persistence / event table.
// ---------------------------------------------------------------------------

// TestMemoryFeedbackVotePersists: a vote appends to the event table (audit)
// AND updates the materialized EMA columns on distilled_qa (zero-join
// search), in one transaction.
func TestMemoryFeedbackVotePersists(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("go build cache", "why is my go build slow?", "Use the build cache: -p and -parallel. Cache reuse cuts rebuild time dramatically.", "repo-a", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	res, err := MemoryFeedback(1, ratingUseful, "adopted the -p flag advice", "task-42")
	if err != nil {
		t.Fatalf("MemoryFeedback: %v", err)
	}
	if !strings.Contains(res, "rating=useful") || !strings.Contains(res, "fb_score=+0.042") {
		t.Errorf("vote result unexpected: %q", res)
	}

	db := openRawDB(t, path)
	var rating, reason, taskID string
	var memoryID int
	if err := db.QueryRow("SELECT memory_id, rating, reason, task_id FROM memory_feedback").Scan(&memoryID, &rating, &reason, &taskID); err != nil {
		t.Fatalf("event row: %v", err)
	}
	if memoryID != 1 || rating != "useful" || reason != "adopted the -p flag advice" || taskID != "task-42" {
		t.Errorf("event row = (%d, %s, %q, %q)", memoryID, rating, reason, taskID)
	}
	var u, b float64
	if err := db.QueryRow("SELECT useful_count, useless_count FROM distilled_qa WHERE id=1").Scan(&u, &b); err != nil {
		t.Fatalf("materialized columns: %v", err)
	}
	if u != 1 || b != 0 {
		t.Errorf("materialized = (%.2f, %.2f), want (1, 0)", u, b)
	}
}

// TestMemoryFeedbackHistoryByID: the audit list filters to one record
// (--list --memory-id) and shows the derived Beta stats.
func TestMemoryFeedbackHistoryByID(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("h topic", "h question here", "h answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := MemoryStore("i topic", "i question here", "i answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := MemoryFeedback(1, ratingUseful, "", ""); err != nil {
		t.Fatalf("vote: %v", err)
	}
	out, err := FeedbackHistory(1)
	if err != nil {
		t.Fatalf("FeedbackHistory(1): %v", err)
	}
	if !strings.Contains(out, "#1") || strings.Contains(out, "#2") || !strings.Contains(out, "fb=0.67(1)") {
		t.Errorf("history by id wrong:\n%s", out)
	}
	out, err = FeedbackHistory(0)
	if err != nil {
		t.Fatalf("FeedbackHistory(0): %v", err)
	}
	// The unfiltered audit lists only records that received votes — #2
	// (never voted) stays out.
	if !strings.Contains(out, "#1") || strings.Contains(out, "#2") {
		t.Errorf("full history must list only voted records:\n%s", out)
	}
}

// TestMemoryFeedbackErrors: invalid rating / id / missing record are
// rejected without touching the DB.
func TestMemoryFeedbackErrors(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("x topic", "x question here", "x answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := MemoryFeedback(1, "great", "", ""); err == nil || !strings.Contains(err.Error(), "invalid rating") {
		t.Errorf("bad rating: err = %v", err)
	}
	if _, err := MemoryFeedback(0, ratingUseful, "", ""); err == nil {
		t.Error("id 0 accepted")
	}
	if _, err := MemoryFeedback(99, ratingUseful, "", ""); err == nil || !strings.Contains(err.Error(), "no record with id 99") {
		t.Errorf("missing record: err = %v", err)
	}
}

// TestMemoryFeedbackUnusedNeutral: unused is recorded (audit) but adds to
// neither side — the "seen but not used ≠ useless" exemption.
func TestMemoryFeedbackUnusedNeutral(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("y topic", "y question here", "y answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := MemoryFeedback(1, ratingUnused, "saw it, went another way", ""); err != nil {
		t.Fatalf("MemoryFeedback: %v", err)
	}
	db := openRawDB(t, path)
	var u, b float64
	if err := db.QueryRow("SELECT useful_count, useless_count FROM distilled_qa WHERE id=1").Scan(&u, &b); err != nil {
		t.Fatalf("materialized: %v", err)
	}
	if u != 0 || b != 0 {
		t.Errorf("unused must be neutral, materialized = (%.2f, %.2f)", u, b)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM memory_feedback").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("unused vote not recorded in event table (%d rows)", n)
	}
}

// ---------------------------------------------------------------------------
// ranking integration.
// ---------------------------------------------------------------------------

// hitIDs extracts the #id sequence from a rendered search result.
func hitIDs(t *testing.T, out string) []int {
	t.Helper()
	var ids []int
	for _, m := range regexp.MustCompile(`#(\d+) `).FindAllStringSubmatch(out, -1) {
		if len(m) > 1 {
			if id, err := strconv.Atoi(m[1]); err == nil {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// TestMemoryFeedbackRankingIntegration: two records with IDENTICAL token
// coverage for a query — the tiebreak order flips when the second-ranked
// record gets a useful vote and the first a useless one (feedback works
// where tokens cannot distinguish).
func TestMemoryFeedbackRankingIntegration(t *testing.T) {
	setMemoryDB(t)
	mk := func(topic, answer string) int {
		t.Helper()
		res, err := MemoryStore(topic, "alpha beta gamma framework question", answer, "s", nil)
		if err != nil {
			t.Fatalf("store %s: %v", topic, err)
		}
		m := regexp.MustCompile(`id=(\d+)`).FindStringSubmatch(res)
		if m == nil {
			t.Fatalf("no id in %q", res)
		}
		id, _ := strconv.Atoi(m[1]) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
		return id
	}
	// Same question+answer token surface (both cover alpha/beta/gamma/
	// framework), different bodies so trigram dedup does not merge them.
	idA := mk("alpha", "alpha beta gamma framework: use the build cache and parallel builds for speed")
	idB := mk("beta", "alpha beta gamma framework: avoid goroutine leaks and set timeouts on every request")
	if idA == idB {
		t.Fatal("records deduped into one — adjust seed text")
	}

	baseline, err := MemorySearch("alpha beta gamma framework", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	base := hitIDs(t, baseline)
	if len(base) < 2 {
		t.Fatalf("expected ≥2 hits, got %v", base)
	}
	if strings.Contains(baseline, "fb=") {
		t.Errorf("baseline must not render fb= :\n%s", baseline)
	}

	// Vote the second-ranked record useful, the first useless → order must
	// flip (fb is the only signal that changed).
	if _, err := MemoryFeedback(base[1], ratingUseful, "this one actually helped", ""); err != nil {
		t.Fatalf("useful vote: %v", err)
	}
	if _, err := MemoryFeedback(base[0], ratingUseless, "misleading", ""); err != nil {
		t.Fatalf("useless vote: %v", err)
	}
	after, err := MemorySearch("alpha beta gamma framework", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	got := hitIDs(t, after)
	if len(got) < 2 || got[0] != base[1] || got[1] != base[0] {
		t.Errorf("feedback did not flip the tie: before %v, after %v", base, got)
	}
	if !strings.Contains(after, "fb=0.67(1)") || !strings.Contains(after, "fb=0.33(1)") {
		t.Errorf("render must show the vote history (fb=mean(votes)):\n%s", after)
	}
}

// TestMemoryFeedbackContradiction: useful then useless on the same record —
// the Beta accumulates both directions and the mean returns to ~0.5 (the
// dispute is visible via the growing vote count, not a direction).
func TestMemoryFeedbackContradiction(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("z topic", "z question here", "z answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := MemoryFeedback(1, ratingUseful, "helped once", ""); err != nil {
		t.Fatalf("useful: %v", err)
	}
	if _, err := MemoryFeedback(1, ratingUseless, "contradicted later", ""); err != nil {
		t.Fatalf("useless: %v", err)
	}
	out, err := MemorySearch("z topic question", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if !strings.Contains(out, "fb=0.50(2)") {
		t.Errorf("contradiction must render mean back at 0.50 with 2 votes:\n%s", out)
	}
	if strings.Contains(out, "fb=0.67") || strings.Contains(out, "fb=0.33") {
		t.Errorf("contradiction must not keep a direction:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// session dedup + regression.
// ---------------------------------------------------------------------------

// TestMemoryFeedbackSessionDedup: one vote per (task_id, memory_id) — the
// anti-stacking gate. A repeat vote in the same task is ignored; a vote
// without a task (CLI maintenance channel) is always recorded.
func TestMemoryFeedbackSessionDedup(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("w topic", "w question here", "w answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := MemoryFeedback(1, ratingUseful, "", "task-1"); err != nil {
		t.Fatalf("first vote: %v", err)
	}
	res, err := MemoryFeedback(1, ratingUseless, "", "task-1")
	if err != nil {
		t.Fatalf("dup vote: %v", err)
	}
	if !strings.Contains(res, "already voted") {
		t.Errorf("dup vote not rejected: %q", res)
	}
	// CLI channel (no task): recorded.
	if _, err := MemoryFeedback(1, ratingUseless, "", ""); err != nil {
		t.Fatalf("untagged vote: %v", err)
	}
	db := openRawDB(t, path)
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM memory_feedback").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("event rows = %d, want 2 (task vote + CLI vote)", n)
	}
	var u, b float64
	if err := db.QueryRow("SELECT useful_count, useless_count FROM distilled_qa WHERE id=1").Scan(&u, &b); err != nil {
		t.Fatalf("materialized: %v", err)
	}
	if !approx(u, 1) || !approx(b, 1) {
		t.Errorf("materialized = (%.4f, %.4f), want (1, 1)", u, b)
	}
}

// TestMemoryFeedbackNoFeedbackRegression: a DB that never received a vote
// renders exactly like the pre-feedback contract (no fb= anywhere) — the
// feedback term adds nothing to scores when there is no feedback.
func TestMemoryFeedbackNoFeedbackRegression(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("a topic", "a question here", "a answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	out, err := MemorySearch("a topic question", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if strings.Contains(out, "fb=") {
		t.Errorf("no-feedback DB must not render fb=:\n%s", out)
	}
	for _, want := range []string{"#1", "tier=warm", "conf=0.85"} {
		if !strings.Contains(out, want) {
			t.Errorf("regression: missing %q:\n%s", want, out)
		}
	}
}

// TestMemoryFeedbackLegacySchemaNoColumns: a pre-feedback schema (no
// materialized columns, no event table) still searches fine — the PRAGMA
// probe skips the fb term and the search never writes to the foreign DB.
func TestMemoryFeedbackLegacySchemaNoColumns(t *testing.T) {
	path := setMemoryDB(t)
	db := openRawDB(t, path)
	seed(t, db,
		`CREATE TABLE distilled_qa (
			id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT, question TEXT, answer TEXT,
			sources TEXT DEFAULT '', confidence REAL DEFAULT 0.85, tier TEXT DEFAULT 'warm')`,
		`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, content='distilled_qa', content_rowid='id')`,
		`INSERT INTO distilled_qa (topic, question, answer) VALUES ('legacy topic', 'legacy question here', 'legacy answer text that is long enough')`,
		`INSERT INTO distilled_qa_fts (rowid, topic, question, answer) VALUES (1, 'legacy topic', 'legacy question here', 'legacy answer text that is long enough')`)
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	out, err := MemorySearch("legacy topic question", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch on legacy schema: %v", err)
	}
	if !strings.Contains(out, "#1") || strings.Contains(out, "fb=") {
		t.Errorf("legacy schema render unexpected:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// recompute / decay / suggest-cold.
// ---------------------------------------------------------------------------

// TestMemoryFeedbackRecompute: the materialized layer is a pure function of
// the event history — recompute repairs a corrupted materialization and
// applies the time decay (90-day half-life) to old votes.
func TestMemoryFeedbackRecompute(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("r topic", "r question here", "r answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	now := float64(1_800_000_000) // fixed epoch for the time-travel seeds
	db := openRawDB(t, path)
	seed(t, db,
		// t-180d: useful; t-90d: useless — between the two events exactly one
		// decay window of 90 days elapses → useful decays to e^-1.
		`INSERT INTO memory_feedback (memory_id, rating, reason, task_id, created_at) VALUES (1, 'useful', 'old', '', `+strconv.FormatFloat(now-180*86400, 'f', 0, 64)+`)`,
		`INSERT INTO memory_feedback (memory_id, rating, reason, task_id, created_at) VALUES (1, 'useless', 'newer', '', `+strconv.FormatFloat(now-90*86400, 'f', 0, 64)+`)`,
		// Corrupt the materialized columns — recompute must overwrite them.
		`UPDATE distilled_qa SET useful_count=99, useless_count=99, fb_last_at=0 WHERE id=1`)
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	res, err := FeedbackRecompute()
	if err != nil {
		t.Fatalf("FeedbackRecompute: %v", err)
	}
	if !strings.Contains(res, "recomputed from 1 records") {
		t.Errorf("recompute result: %q", res)
	}
	db = openRawDB(t, path)
	defer db.Close()
	var u, b, lastAt float64
	if err := db.QueryRow("SELECT useful_count, useless_count, fb_last_at FROM distilled_qa WHERE id=1").Scan(&u, &b, &lastAt); err != nil {
		t.Fatalf("recomputed columns: %v", err)
	}
	// useful: 1 at t-180d, decayed e^(-90/90) = 1/e by t-90d; useless: 1 at
	// t-90d (no window after it). fb_last_at = the last event time.
	wantU := math.Exp(-1)
	if !approx(u, wantU) || !approx(b, 1) {
		t.Errorf("recomputed = (%.4f, %.4f), want (%.4f, 1.0)", u, b, wantU)
	}
	if !approx(lastAt, now-90*86400) {
		t.Errorf("fb_last_at = %.0f, want %.0f", lastAt, now-90*86400)
	}
	if got := fbMean(u, b); got >= 0.5 {
		t.Errorf("decayed old useful must lose to fresh useless: mean = %.4f, want < 0.5", got)
	}
}

// TestMemoryFeedbackRecomputeRecentNoDecay: recent votes recompute with no
// decay (two useful votes in the same instant → 2).
func TestMemoryFeedbackRecomputeRecentNoDecay(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("s topic", "s question here", "s answer text that is long enough to pass the gate", "s", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	db := openRawDB(t, path)
	seed(t, db,
		`INSERT INTO memory_feedback (memory_id, rating, reason, task_id, created_at) VALUES (1, 'useful', '', '', 1000)`,
		`INSERT INTO memory_feedback (memory_id, rating, reason, task_id, created_at) VALUES (1, 'useful', '', '', 1001)`)
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	if _, err := FeedbackRecompute(); err != nil {
		t.Fatalf("FeedbackRecompute: %v", err)
	}
	db = openRawDB(t, path)
	defer db.Close()
	var u float64
	if err := db.QueryRow("SELECT useful_count FROM distilled_qa WHERE id=1").Scan(&u); err != nil {
		t.Fatalf("columns: %v", err)
	}
	if !approx(u, 2) {
		t.Errorf("recent votes must not decay: useful = %.4f, want 2", u)
	}
}

// TestMemoryFeedbackSuggestCold: the --suggest-cold gate — useless ≥ 3 AND
// useful = 0 AND not already cold. Output is a suggestion, never a tier
// change (only-up, never auto-down).
func TestMemoryFeedbackSuggestCold(t *testing.T) {
	setMemoryDB(t)
	store := func(topic string) int {
		t.Helper()
		res, err := MemoryStore(topic, topic+" question here", topic+" answer text that is long enough to pass the gate", "s", nil)
		if err != nil {
			t.Fatalf("store: %v", err)
		}
		m := regexp.MustCompile(`id=(\d+)`).FindStringSubmatch(res)
		id, _ := strconv.Atoi(m[1]) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
		return id
	}
	vote := func(id int, rating string) {
		t.Helper()
		if _, err := MemoryFeedback(id, rating, "test reason", ""); err != nil {
			t.Fatalf("vote %d %s: %v", id, rating, err)
		}
	}
	bad3 := store("candidate one") // 3 useless, 0 useful → candidate
	bad2 := store("candidate two") // 2 useless → not enough
	disputed := store("candidate three")
	coldAlready := store("candidate four")
	for _, id := range []int{bad3, disputed, coldAlready} {
		vote(id, ratingUseless)
		vote(id, ratingUseless)
		vote(id, ratingUseless)
	}
	vote(bad2, ratingUseless)
	vote(bad2, ratingUseless)
	vote(disputed, ratingUseful) // has useful → never a candidate
	if _, err := MemoryUpdate(coldAlready, MemoryUpdateFields{Tier: coldTier}); err != nil {
		t.Fatalf("pre-cold: %v", err)
	}

	out, err := FeedbackSuggestCold()
	if err != nil {
		t.Fatalf("FeedbackSuggestCold: %v", err)
	}
	if !strings.Contains(out, "#"+strconv.Itoa(bad3)) {
		t.Errorf("candidate #%d missing:\n%s", bad3, out)
	}
	for _, id := range []int{bad2, disputed, coldAlready} {
		if strings.Contains(out, "#"+strconv.Itoa(id)) {
			t.Errorf("#%d must NOT be suggested:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "memory_update "+strconv.Itoa(bad3)+" --tier cold") {
		t.Errorf("suggestion must name the manual demotion command:\n%s", out)
	}
	// The suggestion must not have mutated the tier: the candidate is still
	// warm and searchable (only-up, never auto-down).
	got, err := MemoryGet(bad3)
	if err != nil {
		t.Fatalf("MemoryGet: %v", err)
	}
	if !strings.Contains(got, "tier=warm") {
		t.Errorf("suggest-cold must not demote: %s", got)
	}
}
