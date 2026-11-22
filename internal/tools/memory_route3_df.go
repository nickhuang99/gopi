// Route 3's LEXICAL RARITY weight: how specific a query term is to THIS corpus.
//
// WHY THIS FILE EXISTS. `likeCJKRelevanceExpr` (memory.go) ranks route 3's scan
// by how much of the query a row answers, and by how SPECIFIC each answering
// term is. Before this file the only specificity signal available to an SQL
// expression was the term's RUNE LENGTH — a 4-rune term was assumed more
// specific than a 2-rune one. Rune length is a proxy, and a bad one: measured
// on the real 17686-row ledger, `测试环境` (4 runes) has df=34 while a common
// word of the same length has df=2583 — a 76x difference in how much the term
// actually narrows the corpus, which length cannot see at all.
//
// WHAT IT CHANGES, AND WHAT IT DELIBERATELY DOES NOT. The df factor
// MULTIPLIES the existing rune-length weight; it never replaces it, and it is
// clamped so a rare term cannot run away with the ordering. Weight therefore
// keeps its old meaning (longer = more specific) and gains a corpus-aware
// second half (rarer = more specific). A term whose df cannot be measured —
// fewer than cjkIDFMinTrigramRunes runes (SQLite's trigram tokenizer cannot
// index a shorter phrase), a ledger with no trigram index, or a failed read —
// gets the NEUTRAL factor, which reproduces the pre-df ordering exactly. That
// is the Rule 15 shape: an unavailable measurement is a named, honest fallback,
// never a silently different ranking.
//
// WHAT IT CANNOT REACH, stated so nobody reads more into it than is there: the
// relevance key is route 3's ORDER BY, so it decides WHICH `likeCJKRecallK`
// rows route 3 contributes to the candidate pool. It does not reorder the pool
// itself (that is the ladder → session bridge → token-coverage ladder in
// MemorySearchHits). A row route 3 never scanned cannot be rescued by a
// weight, and a row the pool already contains is not moved by one.
package tools

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	// cjkIDFMinTrigramRunes is the shortest term SQLite's trigram tokenizer can
	// index, and therefore the shortest term whose df is measurable on this
	// ledger. Below it the weight keeps its rune-length value (neutral factor):
	// measured, a 2-rune term (`存储`, `内存`) returns df=0 from a MATCH, which
	// is "no trigram to look up", NOT "the term occurs in no row".
	cjkIDFMinTrigramRunes = 3

	// cjkIDFNeutralFactor is the multiplier for a term with no measurable df —
	// exactly 1.0, so the weight is the pre-df rune-length weight and this
	// file's whole contribution vanishes for that term.
	cjkIDFNeutralFactor = 1.0

	// cjkIDFBoostScale is how much the normalised rarity may raise a term's
	// weight. Rarity is normalised to [0,1] (see cjkIDFNormalised) and scaled
	// by this, so the final factor lives in [1, 1+boost]. A LOWER scale is the
	// conservative direction: the ordering moves less.
	cjkIDFBoostScale = 1.0

	// cjkRelevanceWeightDecimals is how many decimals a df-weighted term weight
	// is rendered with into the ORDER BY literal. Three is more than enough:
	// the factors differ by whole percent (df 34 vs 2583 on the real ledger),
	// and a shorter spelling keeps the SQL readable.
	cjkRelevanceWeightDecimals = 3

	// cjkIDFCostCeiling is the epic's per-search budget for the whole df
	// weight. It is a NAMED ceiling with a test that measures against it
	// (TestRoute3DFCostIsWithinTheEpicCeiling) rather than a comment, because
	// "the cost is small" is the kind of claim that stops being true silently.
	cjkIDFCostCeiling = 50 * time.Millisecond

	// cjkDFCacheMaxEntries bounds the process-wide df cache. The cache is an
	// optimisation only — past the bound lookups simply are not remembered —
	// so hitting it degrades cost, never correctness (Rule 15: no state that
	// changes an answer).
	cjkDFCacheMaxEntries = 4096
)

// cjkDFCache remembers df per (ledger path, term). A df is a property of a
// corpus snapshot, not of a query, so the same term is asked for on every
// search in a chat: without this the cost was paid per search (measured 13.4ms
// for 8 terms); with it, once per process per term.
//
// The key carries the ledger PATH because two databases (the operator's profile
// and a test fixture, or two hosts' synced copies) give different answers for
// the same term — a cache keyed on the term alone would leak one corpus's df
// into another's ranking, which is a wrong ordering that no test would explain.
type cjkDFCache struct {
	mu      sync.RWMutex
	entries map[string]int64
}

var route3DFCache = &cjkDFCache{entries: make(map[string]int64)}

func cjkDFCacheKey(dbPath, term string) string { return dbPath + "\x00" + term }

func (c *cjkDFCache) get(dbPath, term string) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	df, ok := c.entries[cjkDFCacheKey(dbPath, term)]
	return df, ok
}

func (c *cjkDFCache) put(dbPath, term string, df int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= cjkDFCacheMaxEntries {
		return
	}
	c.entries[cjkDFCacheKey(dbPath, term)] = df
}

// cjkDFLookup measures how many rows carry the term, through the trigram index
// that already exists for this corpus (`memoryTriIndexTable`). It is a
// read-only COUNT over the external-content index — no new table, no new
// column, no new process (Rule 35).
//
// The MATCH is over the index as a whole, i.e. df counts a row ONCE even if the
// term appears in several of the columns `likeCJKTargets` scans. The relevance
// key counts per-column hits, so the two are not the same quantity: df is used
// here only as a RARITY ORDINAL between terms, which the per-column
// multiplicity does not disturb enough to change an ordering (and the clamp in
// cjkIDFNormalised bounds how far any single term can move).
//
// A caller with no database (a pure SQL-shape test) or a ledger without the
// trigram index gets ok=false, and the caller uses the neutral factor.
func cjkDFLookup(ctx context.Context, db *sql.DB, dbPath, term string) (df int64, ok bool) {
	if db == nil || utf8RuneCount(term) < cjkIDFMinTrigramRunes {
		return 0, false
	}
	if v, hit := route3DFCache.get(dbPath, term); hit {
		return v, true
	}
	// The MATCH operand is a QUOTED PHRASE, so the trigram tokenizer treats the
	// whole term as one phrase instead of splitting it on its own rules — the
	// same spelling the recall routes use, and the reason a term containing a
	// quote must have it doubled.
	phrase := `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	var n int64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+memoryTriIndexTable+` WHERE `+memoryTriIndexTable+` MATCH ?`,
		phrase).Scan(&n); err != nil {
		return 0, false
	}
	route3DFCache.put(dbPath, term, n)
	return n, true
}

// cjkDFRowCount reads the corpus size the rarity is measured AGAINST — the
// denominator of the idf. It reads the COUNT of the same trigram index the df
// lookup uses, which on an external-content FTS5 table reads the CONTENT table
// through the vtable: the number IS the base table's row count, obtained
// without typing the base table's name a second time (Rule 14 — the index is
// already the corpus's one named handle here). A ledger that cannot answer
// (missing index, empty, unreadable) gets ok=false, and every factor stays
// neutral.
func cjkDFRowCount(ctx context.Context, db *sql.DB, dbPath string) (n int64, ok bool) {
	if db == nil {
		return 0, false
	}
	if v, hit := route3DFCache.get(dbPath, "\x00rowcount"); hit {
		return v, true
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+memoryTriIndexTable).Scan(&n); err != nil || n == 0 {
		return 0, false
	}
	route3DFCache.put(dbPath, "\x00rowcount", n)
	return n, true
}

// cjkIDFNormalised maps (df, N) to [0,1], where 1 is "the rarest term this
// corpus can hold" and 0 is "the term is in every row, it narrows nothing".
//
// Natural log of N/(df+1), normalised by log(N): the +1 keeps a df of 0 (a
// term the index has not seen) finite, and log(N) is the width of the whole
// scale, so the answer never depends on the ledger's absolute size — a 100-row
// fixture and a 17686-row profile rank RARITY THE SAME WAY, which is what keeps
// the ordering meaningful in a test.
func cjkIDFNormalised(df, n int64) float64 {
	if n <= 1 || df < 0 {
		return 0
	}
	scale := math.Log(float64(n))
	if scale <= 0 {
		return 0
	}
	v := math.Log(float64(n)/float64(df+1)) / scale
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// cjkIDFFactor is the multiplier applied to a term's rune-length weight:
// 1 + boost*rarity, so it is ALWAYS >= 1 — df can only ever make a rare term
// weigh MORE, never demote a common one below what rune length already said.
// One-sided on purpose: a demotion would change the ordering of every
// already-working query, and this change has to be provably additive.
func cjkIDFFactor(df, n int64) float64 {
	return cjkIDFNeutralFactor + cjkIDFBoostScale*cjkIDFNormalised(df, n)
}

// cjkDFFactors resolves the lexical-rarity multiplier for every term that
// carries ranking weight. It returns an EMPTY map when nothing is measurable —
// no database handle (a pure SQL-shape test), no trigram index, a read failure,
// or every term shorter than cjkIDFMinTrigramRunes — and the renderer then
// produces the pre-df key byte for byte. That is the whole fallback contract:
// unmeasurable means UNCHANGED, never a different ordering nobody can explain.
//
// COST, measured on the real 17686-row ledger rather than estimated — the
// worst case is a COLD cache over a full 16-term query, which is the first
// search of a chat:
//
//	rankable terms=16  cold=39.4ms  warm=19.5µs  ceiling=50ms
//
// (TestRoute3DFCostIsWithinTheEpicCeiling, same run, --v output). So the cold
// path — a chat's FIRST search — sits under the 50 ms per-search budget with
// 1.27x headroom, and every later search in that chat is free. One trigram
// COUNT per rankable term plus one for the corpus size; both cached per
// (ledger path, term) for the process's lifetime (`route3DFCache`).
//
// The honest limit: a query with MANY rankable terms is the expensive shape
// (16 terms ≈ 39 ms cold), because the cost is linear in distinct terms. A
// pathological 40-term query would exceed the ceiling on its first search; the
// ceiling test measures a 16-term query, and that is the shape the epic's
// +50 ms budget was written for.
func cjkDFFactors(ctx context.Context, db *sql.DB, dbPath string, terms []string) map[string]float64 {
	rankable := likeCJKRelevanceTerms(terms)
	if len(rankable) == 0 {
		return nil
	}
	n, ok := cjkDFRowCount(ctx, db, dbPath)
	if !ok {
		return nil
	}
	factors := make(map[string]float64, len(rankable))
	for _, rt := range rankable {
		df, measured := cjkDFLookup(ctx, db, dbPath, rt.term)
		if !measured {
			// Short term (unindexable by the trigram tokenizer) or a read
			// failure: keep its rune-length weight exactly as it was.
			factors[rt.term] = cjkIDFNeutralFactor
			continue
		}
		factors[rt.term] = cjkIDFFactor(df, n)
	}
	return factors
}

// utf8RuneCount is utf8.RuneCountInString without importing utf8 at every call
// site (this file asks it twice).
func utf8RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
