package tools

// M2 roadmap tests: confidence lifecycle (lazy decay + re-verify reward),
// feedback→confidence closure (useful up / useless down + cooldown suggest),
// hot-stale signal, hot resident digest, and FTS sync across the feedback
// UPDATE (the au trigger must survive the machine's confidence write).
// Every threshold is asserted against the named constants (no magic numbers
// in the expectations) and each case seeds the smallest possible DB.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// m2SeedDB opens a scratch DB with the current schema and inserts one record
// with caller-chosen lifecycle columns. Returns the DB and the row id.
func m2SeedDB(t *testing.T, confidence float64, distilledAt float64, retrievalCount int, lastRetrieved *float64, tier string) (*sql.DB, string, int) {
	t.Helper()
	path := t.TempDir() + "/m2.db"
	db, err := openStoreDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open store db: %v", err)
	}
	if err := ensureSchema(context.Background(), db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := ensureFeedbackSchema(context.Background(), db); err != nil {
		t.Fatalf("ensure feedback schema: %v", err)
	}
	when := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.Exec(`INSERT INTO distilled_qa
		(topic, question, answer, confidence, distilled_at, retrieval_count, last_retrieved, tier, when_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		"m2 test", "m2 lifecycle probe question?", "answer body long enough for the gate", confidence,
		distilledAt, retrievalCount, lastRetrieved, tier, when)
	if err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	id, _ := res.LastInsertId() //stylegate:ignore the id only feeds an ack string; 0 is non-fatal there
	return db, path, int(id)
}

func fptr(f float64) *float64 { return &f }

// m2SearchHit runs MemorySearch and unmarshals the engine JSON (pre-render).
// m2SearchText runs MemorySearch and returns the tool-facing RENDERED text
// (renderMemorySearchResult output: "#id ... eff_conf=... hot_stale=..." meta
// blocks) — the contract the LLM actually sees.
func m2SearchText(t *testing.T, query string) string {
	t.Helper()
	out, err := MemorySearch(query, 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	return out
}

func TestM2EffConfidenceDecaysWithAge(t *testing.T) {
	now := float64(time.Now().Unix())
	cases := []struct {
		name        string
		conf        float64
		distilled   float64
		lastRetr    *float64
		retrCount   int
		wantFloorOK bool
		wantAtLeast float64
		wantAtMost  float64
	}{
		{
			name:        "fresh undecayed",
			conf:        defaultConfidence,
			distilled:   now - 3600,
			wantAtLeast: defaultConfidence - 0.01,
			wantAtMost:  1.0,
		},
		{
			name:        "half-life-ish: 90d old ≈ conf/e",
			conf:        0.85,
			distilled:   now - fbDecaySeconds,
			wantAtLeast: 0.85/math.E - 0.01,
			wantAtMost:  0.85/math.E + 0.01,
		},
		{
			name:        "re-verified yesterday resets the clock",
			conf:        0.85,
			distilled:   now - fbDecaySeconds,
			lastRetr:    fptr(now - 3600),
			wantAtLeast: 0.84,
			wantAtMost:  1.0,
		},
		{
			name:        "saturated re-verify reward",
			conf:        0.85,
			distilled:   now - fbDecaySeconds*2,
			retrCount:   confReverifySaturate,
			wantAtLeast: 0.85/math.E/math.E + confReverifyRewardSaturate - 0.01,
			wantAtMost:  0.85/math.E/math.E + confReverifyRewardSaturate + 0.01,
		},
		{
			name:        "ancient unverified hits the floor",
			conf:        0.85,
			distilled:   now - fbDecaySeconds*20,
			wantFloorOK: true,
			wantAtLeast: confDecayFloor,
			wantAtMost:  confDecayFloor + 0.001,
		},
		{
			name:        "undated row keeps stored confidence (no evidence, no decay)",
			conf:        0.85,
			distilled:   0,
			wantAtLeast: 0.85,
			wantAtMost:  0.85,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lastRet sql.NullFloat64
			if tc.lastRetr != nil {
				lastRet = sql.NullFloat64{Float64: *tc.lastRetr, Valid: true}
			}
			got := m2EffectiveConfidence(tc.conf, sql.NullFloat64{Float64: tc.distilled, Valid: true},
				lastRet, tc.lastRetr != nil, float64(tc.retrCount), now)
			if got < tc.wantAtLeast-1e-9 || got > tc.wantAtMost+1e-9 {
				t.Fatalf("eff=%v want in [%v, %v]", got, tc.wantAtLeast, tc.wantAtMost)
			}
			if tc.wantFloorOK && got < confDecayFloor {
				t.Fatalf("eff=%v below floor %v", got, confDecayFloor)
			}
			if got > 1 {
				t.Fatalf("eff=%v exceeds 1", got)
			}
		})
	}
}

// The decayed confidence must actually reorder results: an old unverified
// row loses to an otherwise identical fresh row.
func TestM2DecayReordersSearchResults(t *testing.T) {
	path := t.TempDir() + "/m2.db"
	db, err := openStoreDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := ensureSchema(context.Background(), db); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if err := ensureFeedbackSchema(context.Background(), db); err != nil {
		t.Fatalf("feedback schema: %v", err)
	}
	now := float64(time.Now().Unix())
	for i, age := range []float64{24 * 3600.0, fbDecaySeconds * 20} { // fresh vs 20τ old
		when := time.Unix(int64(now-age), 0).UTC().Format("2006-01-02 15:04:05")
		if _, err := db.Exec(`INSERT INTO distilled_qa
			(topic, question, answer, confidence, distilled_at, retrieval_count, last_retrieved, when_at)
			VALUES (?,?,?,?,?,0,NULL,?)`,
			fmt.Sprintf("same topic %d", i), "unique m2 reorder widget probe?", "answer body long enough", 0.85, now-age, when); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	t.Setenv(memoryDBEnv, path)
	out := m2SearchText(t, "unique m2 reorder widget probe?")
	iFresh, iOld := strings.Index(out, "#1 "), strings.Index(out, "#2 ")
	if iFresh < 0 || iOld < 0 {
		t.Fatalf("both rows must be recalled:\n%s", out)
	}
	if iFresh > iOld {
		t.Fatalf("fresh row must outrank the 20τ-unverified row:\n%s", out)
	}
	if strings.Contains(out[:iOld], "eff_conf=") {
		t.Fatalf("fresh row (eff≈conf) must not render eff_conf:\n%s", out[:iOld])
	}
	if !strings.Contains(out[iOld:], "eff_conf=0.05") {
		t.Fatalf("ancient unverified row must decay to the floor eff_conf=%.2f:\n%s", confDecayFloor, out[iOld:])
	}
}

func TestM2FeedbackMovesConfidenceBothWays(t *testing.T) {
	for _, tc := range []struct {
		rating string
		want   float64 // expected delta direction/magnitude vs start 0.5
	}{
		{ratingUseful, 0.5 + confVoteStepUseful},
		{ratingUseless, 0.5 + confVoteStepUseless},
		{ratingUnused, 0.5},
	} {
		t.Run(tc.rating, func(t *testing.T) {
			db, dbPath, id := m2SeedDB(t, 0.5, float64(time.Now().Unix()), 0, nil, warmTier)
			t.Setenv(memoryDBEnv, dbPath)
			if _, err := MemoryFeedback(id, tc.rating, "m2 test vote", "task-m2-1"); err != nil {
				t.Fatalf("MemoryFeedback: %v", err)
			}
			var conf float64
			if err := db.QueryRow("SELECT confidence FROM distilled_qa WHERE id=?", id).Scan(&conf); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if math.Abs(conf-tc.want) > 1e-9 {
				t.Fatalf("confidence=%v want %v (%s)", conf, tc.want, tc.rating)
			}
		})
	}
}

func TestM2FeedbackCooldownSuggestsCold(t *testing.T) {
	db, dbPath, id := m2SeedDB(t, 0.5, float64(time.Now().Unix()), 0, nil, warmTier)
	t.Setenv(memoryDBEnv, dbPath)
	// Seed the materialized state just under the gate with back-to-back
	// votes (sub-second gaps → no decay drift, exactly like the real burst).
	var out string
	var err error
	for i := 0; i < int(fbColdSuggestThreshold)-1; i++ {
		out, err = MemoryFeedback(id, ratingUseless, "wrong", fmt.Sprintf("task-cool-%d", i))
		if err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}
	if strings.Contains(out, "COOLDOWN") {
		t.Fatalf("cooled off before the threshold: %s", out)
	}
	out, err = MemoryFeedback(id, ratingUseless, "still wrong", "task-cool-2")
	if err != nil {
		t.Fatalf("vote: %v", err)
	}
	if !strings.Contains(out, "COOLDOWN") || !strings.Contains(out, fmt.Sprintf("memory_update %d --tier cold", id)) {
		t.Fatalf("cooldown suggestion missing:\n%s", out)
	}
	// A useful vote breaks the gate (u>0 → no suggestion).
	out, err = MemoryFeedback(id, ratingUseful, "actually right", "task-cool-3")
	if err != nil {
		t.Fatalf("useful vote: %v", err)
	}
	if strings.Contains(out, "COOLDOWN") {
		t.Fatalf("useful vote should break the cooldown gate:\n%s", out)
	}
	// The stored confidence walked down 4×confVoteStepUseless then up once,
	// clamped at the floor.
	var conf float64
	if err := db.QueryRow("SELECT confidence FROM distilled_qa WHERE id=?", id).Scan(&conf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// 3 decayed useless votes (2 loop + 1 crossing the threshold) then 1
	// useful: 0.5 + 3×(−0.10) + 0.05.
	want := math.Max(confDecayFloor, 0.5+float64(int(fbColdSuggestThreshold))*confVoteStepUseless+confVoteStepUseful)
	if math.Abs(conf-want) > 1e-9 {
		t.Fatalf("confidence=%v want %v", conf, want)
	}
}

func TestM2FeedbackUpdateKeepsFTSInSync(t *testing.T) {
	_, dbPath, id := m2SeedDB(t, 0.85, float64(time.Now().Unix()), 0, nil, warmTier)
	t.Setenv(memoryDBEnv, dbPath)
	if _, err := MemoryFeedback(id, ratingUseless, "m2 fts probe", "task-fts"); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	// The feedback UPDATE must have routed through the au trigger: FTS5
	// content still matches the row (a desynced external-content index
	// returns nothing here).
	out := m2SearchText(t, "m2 lifecycle probe question?")
	if !strings.Contains(out, fmt.Sprintf("#%d ", id)) {
		t.Fatalf("row %d lost from FTS after feedback UPDATE (trigger sync broken):\n%s", id, out)
	}
}

func TestM2HotStaleSignal(t *testing.T) {
	now := float64(time.Now().Unix())
	day := 24 * 3600.0
	// hot + verified 30d ago → hot_stale (hotStaleDays=7).
	_, dbPath, _ := m2SeedDB(t, 0.85, now-30*day, 3, fptr(now-30*day), hotTier)
	t.Setenv(memoryDBEnv, dbPath)
	out := m2SearchText(t, "m2 lifecycle probe question?")
	if !strings.Contains(out, "hot_stale=true") {
		t.Fatalf("hot record unverified 30d (>%d) must render hot_stale:\n%s", hotStaleDays, out)
	}
	// hot + verified 1d ago → not stale (fresh DB per scenario).
	_, dbPath2, _ := m2SeedDB(t, 0.85, now-day, 1, fptr(now-day), hotTier)
	t.Setenv(memoryDBEnv, dbPath2)
	out2 := m2SearchText(t, "m2 lifecycle probe question?")
	if strings.Contains(out2, "hot_stale") {
		t.Fatalf("freshly verified hot record must not be hot_stale:\n%s", out2)
	}
}

func TestM2HotResidentDigest(t *testing.T) {
	db, dbPath, hotID := m2SeedDB(t, 0.85, float64(time.Now().Unix()), 2, fptr(float64(time.Now().Unix()-3600)), hotTier)
	if _, err := db.Exec(`INSERT INTO distilled_qa
		(topic, question, answer, confidence, distilled_at, retrieval_count, last_retrieved, tier, when_at)
		VALUES ('m2 warm', 'm2 warm question?', 'answer body long enough', 0.85, ?, 0, NULL, 'warm', ?)`,
		float64(time.Now().Unix()), time.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("seed warm: %v", err)
	}
	t.Setenv(memoryDBEnv, dbPath)
	digest := MemoryHotResident()
	if !strings.Contains(digest, fmt.Sprintf("#%d", hotID)) {
		t.Fatalf("digest missing hot row:\n%s", digest)
	}
	if strings.Contains(digest, "m2 warm") {
		t.Fatalf("digest must contain ONLY hot rows:\n%s", digest)
	}
	// Best-effort: an empty DB yields "" (never an error).
	db2 := t.TempDir() + "/empty.db"
	d2, err := openStoreDB(context.Background(), db2)
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	defer d2.Close()
	if err := ensureSchema(context.Background(), d2); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Setenv(memoryDBEnv, db2)
	if got := MemoryHotResident(); got != "" {
		t.Fatalf("empty DB must render empty digest, got:\n%s", got)
	}
}

func TestM2EnsureFeedbackSchemaHealsLegacyTierConfidence(t *testing.T) {
	// A legacy DB with the base table but neither tier/confidence nor the
	// feedback columns: the heal must add all five, then a vote must succeed.
	path := t.TempDir() + "/legacy.db"
	db, err := openStoreDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE IF EXISTS distilled_qa`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE distilled_qa (
		id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT, question TEXT, answer TEXT)`); err != nil {
		t.Fatalf("legacy create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO distilled_qa (topic, question, answer) VALUES ('t','q m2 legacy?','a body')`); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}
	t.Setenv(memoryDBEnv, path)
	if _, err := MemoryFeedback(1, ratingUseful, "heal probe", "task-legacy"); err != nil {
		t.Fatalf("feedback on healed legacy schema: %v", err)
	}
	var tier string
	var conf float64
	if err := db.QueryRow("SELECT tier, confidence FROM distilled_qa WHERE id=1").Scan(&tier, &conf); err != nil {
		t.Fatalf("read healed columns: %v", err)
	}
	if tier != defaultTier {
		t.Fatalf("healed tier=%q want %q", tier, defaultTier)
	}
	if math.Abs(conf-(defaultConfidence+confVoteStepUseful)) > 1e-9 {
		t.Fatalf("healed confidence=%v want %v", conf, defaultConfidence+confVoteStepUseful)
	}
}
