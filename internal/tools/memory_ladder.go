package tools

import (
	"context"
	"database/sql"
	"strings"
)

// memory_ladder.go — the FIRST SHARED HALF of memory retrieval: the AND-subset
// degradation ladder (memsearch-subset-degrade), lifted out of memory.go so a
// second consumer can reach it.
//
// WHY IT MOVED, and why that is not a rewrite. There were always two readers of
// this algorithm and only one of them could see it:
//
//   - memory_search walks the ladder inside MemorySearchHits;
//   - the typing-time memory prefetch (cmd/agent/chat_prefetch.go) asked its own
//     question — an all-OR FTS5 MATCH — because the ladder was unexported and
//     lived beside a MIGRATING opener it must not use.
//
// The two therefore disagreed about the SAME query. MEASURED on the operator's
// own ledger (251MB, 2026-10-07), query `254 321 426 cancelled e2e help merger
// 条积压`: all-OR matches 2619 rows, all-AND matches 0 — so the prefetch filled
// its prompt with rows sharing ONE incidental token (the ledger's own ids 254 /
// 321 / 426 are high-frequency fillers) while the ladder's subset
// `"254" AND "321" AND "426" AND "cancelled"` is the conjunct that actually
// means something.
//
// The move is verbatim except for ONE line, and it is a stop condition rather
// than a change of result: the walk now returns what it has when the caller's
// context expires (see the ctx.Err() guard in memsearchSubsetPick). The ladder
// always took a ctx — it simply never read it between probes, which is
// affordable for a foreground memory_search and NOT affordable on a keystroke
// path with a hard 1500ms budget: MEASURED, a full walk of
// `peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太` costs 1723ms and of
// `还没 没修 修复 复的 的问 问题` costs 3203ms on that ledger. The guard is what
// lets the SAME algorithm serve both budgets: memory_search keeps its unbounded
// walk (its ctx has no deadline), the prefetch's walk stops at its own bound.
//
// It is deliberately NOT a new exported surface for the ladder's internals: the
// only new name is LadderMatch, a three-line face over memsearchSubsetPick —
// a second adapter would be a second place for the two consumers to drift.

// memsearchLadderTerms splits q into the injection-safe single-token MATCH
// terms the ladder descends over: each whitespace field, quotes stripped, with
// a CJK field broken into its 2-char grams (the same granularity jieba_token
// is indexed at, and the reason a natural-language CJK query — "怎么降低deepseek
// 开支" → 怎么降低 / 开支 — can descend at all instead of being one un-matchable
// 8-char run). Every term is returned quoted, so a token can never reach FTS5
// as an operator (the same neutralization as escapeQueryOR). Duplicates are
// dropped and the result is capped at memsearchMaxANDTokens, because the
// caller's candidate space is every SUBSET of this slice (Rule 1a: the cap is
// a named constant, asserted by TestMemsearchLadderTokenCap).
//
// ASCII terms are truncated at the first CJK rune ("deepseek开支" → "deepseek",
// "开支" / "开支..."), mirroring how the reference's tokenizer peels an ASCII
// word off a glued run.
func memsearchLadderTerms(q string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(term string) bool {
		term = strings.TrimSpace(strings.ReplaceAll(term, `"`, ""))
		if term == "" {
			return true
		}
		key := strings.ToLower(term)
		if seen[key] {
			return true
		}
		seen[key] = true
		out = append(out, `"`+term+`"`)
		return len(out) < memsearchMaxANDTokens
	}
	for _, field := range strings.Fields(q) {
		if !hasCJK(field) {
			if !add(field) {
				return out
			}
			continue
		}
		for _, part := range splitASCIIRuns(field) {
			if !add(part) {
				return out
			}
		}
		// 2-char CJK grams: a ≥3-char run is the unit escapeQuery leaves
		// bare, but bare means un-matchable against the gram index, so the
		// ladder descends on grams instead.
		if !addCJK2Grams(field, add) {
			return out
		}
	}
	return out
}

// splitASCIIRuns peels the ASCII substrings off a mixed CJK/ASCII field,
// preserving order ("怎么降低deepseek开支" → ["deepseek"]).
func splitASCIIRuns(field string) []string {
	out := []string{}
	run := []rune{}
	flush := func() {
		if len(run) > 0 {
			out = append(out, string(run))
			run = nil
		}
	}
	for _, r := range field {
		if isCJK(r) {
			flush()
			continue
		}
		run = append(run, r)
	}
	flush()
	return out
}

// addCJK2Grams feeds the field's overlapping 2-char CJK grams through add in
// order, stopping (false) as soon as the caller's cap is reached.
func addCJK2Grams(field string, add func(string) bool) bool {
	run := []rune{}
	flush := func() bool {
		if len(run) >= 2 {
			for i := 0; i+1 < len(run); i++ {
				if !add(string(run[i : i+2])) {
					run = nil
					return false
				}
			}
		}
		run = nil
		return true
	}
	for _, r := range field {
		if isCJK(r) {
			run = append(run, r)
			continue
		}
		if !flush() {
			return false
		}
	}
	return flush()
}

// memsearchLadderCount reports how many rows a term list matches as one AND
// expression (the COUNT the ladder's fast path and every subset probe need).
//
// An error is returned rather than swallowed, and the ladder above READS it:
// the two errors a probe can raise are structurally different facts and the
// ladder's response to each is different (see ladderProbeCount). Reading every
// error as "zero matches" — which is what the descending walk did — is what made
// a ledger held by a writer indistinguishable from a ledger with no match, so
// the walk kept probing a database that was refusing every statement.
func memsearchLadderCount(ctx context.Context, db *sql.DB, terms []string) (int, error) {
	return ladderCountOn(ctx, db, memsearchLadderFTS, terms)
}

// ladderCountOn is memsearchLadderCount over a NAMED FTS5 table. The ladder was
// written for the memory ledger, and it is now the recall shape of a SECOND
// index as well (chat_notes, via ChatNoteSearchOn's ladder tier), so the table
// is a parameter instead of a literal: one ladder, two indexes (Rule 14). The
// name is interpolated rather than bound because SQLite cannot bind an
// identifier — it comes from the caller's own constant, never from a query
// string.
func ladderCountOn(ctx context.Context, db *sql.DB, ftsTable string, terms []string) (int, error) {
	if len(terms) == 0 || len(terms) > memsearchMaxTokensPerSubset {
		return 0, nil
	}
	n := 0
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+ftsTable+` WHERE `+ftsTable+` MATCH ?`,
		strings.Join(terms, " AND ")).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// memsearchDroppedDF sums the document frequency of the terms a subset drops —
// the tiebreak's "did we shed the noise or the signal" mass. It is MAXIMIZED:
// the winner is the subset whose dropped terms are the highest-DF ones, i.e.
// the query's filler. A term matching NOTHING contributes zero on purpose:
// requiring it is what killed the all-AND match, so shedding it is free.
//
// This is a deliberate INVERSION of the reference's tiebreak, on measured
// grounds. The reference minimizes the same sum (penalty = Σ df(dropped)); its
// `_term_df` doc calls low DF "more likely noise", and minimizing therefore
// keeps the query's most COMMON terms. On gopi's real corpus that picks the
// worst subset: for "怎么降低deepseek开支" it kept 怎么 (a filler literally in
// cjkStopwords, DF 434) and shed deepseek, choosing a 136-row conjunct over a
// 1-row conjunct that contains the row the user was asking for. Maximizing
// drops 怎么 and keeps the subject-bearing conjunct. The epic's own wording —
// "drop noise keep signal" — describes this direction.
func memsearchDroppedDF(ctx context.Context, db *sql.DB, dropped []string) int {
	return ladderDroppedDFOn(ctx, db, memsearchLadderFTS, dropped)
}

// ladderDroppedDFOn is memsearchDroppedDF over a NAMED FTS5 table (see
// ladderCountOn).
func ladderDroppedDFOn(ctx context.Context, db *sql.DB, ftsTable string, dropped []string) int {
	total := 0
	for _, term := range dropped {
		if df, ok := ladderTermDFOn(ctx, db, ftsTable, term); ok {
			total += df
		}
	}
	return total
}

// memsearchTermDF returns one term's document frequency. found=false means the
// probe could not answer (the token is below the tokenizer's gram floor, or
// FTS5 refused it) — callers must read that as "cannot be measured", never as
// zero.
func memsearchTermDF(ctx context.Context, db *sql.DB, term string) (int, bool) {
	return ladderTermDFOn(ctx, db, memsearchLadderFTS, term)
}

// ladderTermDFOn is memsearchTermDF over a NAMED FTS5 table (see ladderCountOn).
func ladderTermDFOn(ctx context.Context, db *sql.DB, ftsTable, term string) (int, bool) {
	var n int
	//stylegate:ignore probe DF; an error is read as unmeasurable (found=false)
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+ftsTable+` WHERE `+ftsTable+` MATCH ?`,
		term).Scan(&n); err != nil {
		return 0, false
	}
	return n, true
}

// memsearchLadderPick is the ladder's outcome: the MATCH expression to run and
// the winning subset's terms (quoted, token order). The terms carry the
// subset's score weight — a hit that the subset matched earns one coverage
// point per demanded term, the ladder's evidence added ahead of the OR pool's
// incidental single-token rows.
type memsearchLadderPick struct {
	match string
	terms []string
}

// memsearchSubsetPick is the ladder: fast-path all-AND when the full token set
// already satisfies the threshold, else walk size=len(tokens)-1 down to 1
// trying every combination, keep the qualifiers ranked by (more terms
// retained, then larger summed dropped-token DF = shed filler keep signal —
// see memsearchDroppedDF for why the reference's direction is inverted here),
// and report no qualifier so
// the caller can fall back to OR. It returns ok=false when the query is
// outside the ladder's bounds (below memsearchMinANDTokens / above
// memsearchMaxANDTokens) — the ladder is an addition to recall, never a
// replacement for the routes that already work.
func memsearchSubsetPick(ctx context.Context, db *sql.DB, query string) (memsearchLadderPick, bool) {
	return memsearchSubsetPickOn(ctx, db, memsearchLadderFTS, query)
}

// memsearchSubsetPickOn is the ladder over a NAMED FTS5 table — the SAME walk,
// the same thresholds, the same probe budget, one implementation. Its second
// caller is the chat-note search's ladder tier (chat_notes.go), which needs
// exactly this recall shape against another index rather than a re-implementation
// of the descent (Rule 8: reuse the wheel; a second copy of this walk would drift
// from this one on the first tuning change).
func memsearchSubsetPickOn(ctx context.Context, db *sql.DB, ftsTable, query string) (memsearchLadderPick, bool) {
	terms := memsearchLadderTerms(query)
	if len(terms) < memsearchMinANDTokens || len(terms) > memsearchMaxANDTokens {
		return memsearchLadderPick{}, false
	}
	// Fast path: the full AND already satisfies the threshold — never
	// downgrade a query the exact route can answer.
	//
	// The threshold is a SPECIFICITY floor, deliberately not the result-count
	// knob the reference engine uses (min_results): lowering the count to make
	// a miss "hit" would trade precision for a number, and gopi's contract is
	// that the recall routes own candidate generation while ranking owns
	// order. The floor says "this subset is specific enough to be evidence",
	// and one is the strictest value that still admits a single well-aimed
	// row.
	fastCount, fastErr := ladderCountOn(ctx, db, ftsTable, terms)
	if fastErr != nil {
		// The FULL conjunct could not be counted. A locked ledger ends the
		// walk here with no pick, so the caller's fallback meets the lock
		// itself (and reports it as the refusal it is) instead of the ladder
		// spending its budget re-asking an engine that is refusing.
		if LedgerBusyErr(fastErr) {
			return memsearchLadderPick{}, false
		}
	} else if fastCount >= memsearchMinSubsetCount {
		return memsearchLadderPick{match: strings.Join(terms, " AND "), terms: terms}, true
	}
	// Degrade: walk sizes downward and keep every qualifying subset. The walk
	// order IS the primary ranking key (more terms retained first), so the
	// first size that yields qualifiers holds the winner; later sizes are only
	// collected while the probe budget lasts, never to outrank it. Within one
	// size the dropped-DF mass decides (memsearchDroppedDF).
	best := memsearchLadderPick{}
	bestSize := 0
	bestDroppedDF := 0
	probes := 0
	qualifiers := 0
	for size := len(terms) - 1; size >= 1; size-- {
		for _, subset := range memsearchSubsets(len(terms), size) {
			// The caller's own deadline ENDS the walk with what it has. The
			// ladder has always taken a ctx and never read it here: affordable
			// for a foreground search, not for a keystroke path whose whole
			// budget is prefetchGoroutineTimeoutMs — MEASURED 1723ms and
			// 3203ms walks on the operator's 251MB ledger, i.e. two queries
			// that would burn the prefetch's entire budget and still give up.
			// A partial result is strictly better than none, and it is the
			// SAME ranking (the walk descends from the biggest subset, so what
			// it has when it stops is what it would have returned anyway).
			if ctx.Err() != nil {
				return best, best.match != ""
			}
			if probes >= memsearchMaxCombos {
				return best, best.match != ""
			}
			probes++
			kept := make([]string, 0, size)
			dropped := make([]string, 0, len(terms)-size)
			idx := 0
			for i, term := range terms {
				if idx < len(subset) && subset[idx] == i {
					kept = append(kept, term)
					idx++
					continue
				}
				dropped = append(dropped, term)
			}
			keptCount, keptErr := ladderCountOn(ctx, db, ftsTable, kept)
			if keptErr != nil {
				if LedgerBusyErr(keptErr) {
					// A LOCKED ledger is not evidence about this subset, and it is
					// not going to become evidence by asking 1000 more times: the
					// walk ends and the caller's OR fallback meets the lock once,
					// which is the 250ms refusal the keystroke path was built for
					// (measured: without this, a prefetch on a locked ledger
					// reported a 1500ms deadline expiry instead of SQLITE_BUSY).
					return memsearchLadderPick{}, false
				}
				continue
			}
			if keptCount < memsearchMinSubsetCount {
				continue
			}
			droppedDF := ladderDroppedDFOn(ctx, db, ftsTable, dropped)
			if best.match == "" || size > bestSize || (size == bestSize && droppedDF > bestDroppedDF) {
				best = memsearchLadderPick{match: strings.Join(kept, " AND "), terms: kept}
				bestSize = size
				bestDroppedDF = droppedDF
			}
			qualifiers++
			if qualifiers >= memsearchMaxSubsetProbes {
				return best, true
			}
		}
	}
	return best, best.match != ""
}

// memsearchMinSubsetCount is the qualifying floor for a subset: a subset is
// evidence when it matches at least this many rows (see memsearchSubsetPick
// for why this is not the reference's min_results result-count knob).
const memsearchMinSubsetCount = 1

// memsearchLadderFTS is the table the ladder was written for: the memory
// ledger's own full-text index. It is a named constant because the ladder now
// runs over more than one index and a caller must be able to say WHICH (Rule 1:
// no bare table-name literal at a MATCH site).
const memsearchLadderFTS = "distilled_qa_fts"

// memsearchSubsets enumerates every size-element index combination over n
// terms in lexicographic order — the combinations() of the reference engine,
// rendered as an iterator so the combination-explosion bound is explicit and
// countable at the call site (callback form; returning false stops the walk).
// Indices ascending per subset means the ORIGINAL token order is preserved in
// every reconstructed MATCH expression.
func memsearchSubsets(n, size int) [][]int {
	out := [][]int{}
	if size <= 0 || size > n {
		return out
	}
	idx := make([]int, size)
	for i := range idx {
		idx[i] = i
	}
	for {
		cur := make([]int, size)
		copy(cur, idx)
		out = append(out, cur)
		i := size - 1
		for i >= 0 && idx[i] == i+n-size {
			i--
		}
		if i < 0 {
			return out
		}
		idx[i]++
		for j := i + 1; j < size; j++ {
			idx[j] = idx[j-1] + 1
		}
	}
}

// LadderMatch is the ladder's ONE exported face: the MATCH expression its walk
// selected for query, or ok=false when it has no pick — the query is outside the
// ladder's token bounds, no subset qualified, the probe budget ran out, or ctx
// expired before a subset was found. Callers read ok=false as "use your own
// fallback"; the ladder never returns an expression it did not verify against
// the index (memsearchLadderCount is the authority), so ok=true must not be
// second-guessed by the caller's own lexical test.
//
// The db must be the caller's OWN handle — this function opens nothing, so it
// inherits the caller's read-only contract, its busy timeout and its deadline.
// That is the whole point of the shared layer: the prefetch keeps its provably
// read-only connection (tools.LedgerReadOnlyDSNIn, never openSearchDB's
// migrating opener) while running the same algorithm memory_search runs.
func LadderMatch(ctx context.Context, db *sql.DB, query string) (string, bool) {
	return LadderMatchOn(ctx, db, memsearchLadderFTS, query)
}

// LadderMatchOn is LadderMatch over a NAMED FTS5 table, for a caller whose index
// is not the memory ledger's.
//
// THE NAME IS THE CALLER'S OWN CONSTANT, never user input: SQLite cannot bind an
// identifier, so this parameter is the one place a table name is interpolated
// into SQL in this file, and every caller passes a package constant.
//
// The MATCH expression it returns is written in the TARGET table's token space,
// which is the caller's responsibility to satisfy: the ladder's terms are built
// by memsearchLadderTerms (ASCII words plus 2-char CJK grams, each quoted), and
// that shape is what both indexes use — the memory ledger's distilled_qa_fts and
// chat_notes_fts are both fts5 with tokenize='unicode61'.
func LadderMatchOn(ctx context.Context, db *sql.DB, ftsTable, query string) (string, bool) {
	pick, ok := memsearchSubsetPickOn(ctx, db, ftsTable, query)
	if !ok || pick.match == "" {
		return "", false
	}
	return pick.match, true
}
