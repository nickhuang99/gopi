// Memory tools: memory_search / memory_store — gopi's hippocampus. The
// distilled-knowledge store is EMBEDDED in the gopi binary: pure-Go SQLite
// (modernc.org/sqlite, zero CGO — no Python, no subprocess, no external
// engine). The Go side owns the whole schema and storage: search runs an
// FTS5 MATCH against the external-content index and joins back to the base
// rows for the value signals; store INSERTs and the FTS triggers keep the
// index in sync. "LLM 即海马体": the model decides what to store (distilled
// lessons only) and what to retrieve (value-gated, judged by the returned
// value signals).
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"github.com/nickhuang99/gopi/internal/cjkseg"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TierHot / TierWarm / TierCold are the memory tier values, exported so
// in-process callers outside this package (the hippocampus organ weighs a hit
// by its tier — epic child 4/4) never type the wire string inline
// (Rule 1b/13/14). The unexported tier consts below reference them, so the
// value keeps exactly one definition.
const (
	TierHot  = "hot"
	TierWarm = "warm"
	TierCold = "cold"
)

// ErrGarbageRefused is the store core's garbage-gate sentinel. A per-row
// SEMANTIC verdict: the import counts it as RowsRefused and keeps applying
// the patch (a bad row never poisons its neighbors); everything else out of
// the store core is a DB-level failure that aborts and rolls back the whole
// import.
var ErrGarbageRefused = errors.New("rejected as garbage (no distilled knowledge content)")

const (
	// memoryDefaultLimit is the result cap the tool applies when the model
	// omits the limit argument.
	memoryDefaultLimit = 5

	// memoryDBEnv overrides the shared default DB location
	// (~/.gopi/memory.db) — the independent mode for per-profile /
	// per-project isolation. It is an ALIAS of the single definition in
	// internal/config (Rule 14): the same value is consumed by internal/agent,
	// cmd/agent and internal/testutil, none of which can read an unexported
	// constant here.
	memoryDBEnv = config.MemoryDBEnv

	// memoryDBEnvLegacy is the pre-embedding env var name (it used to be
	// passed through to the hippo CLI as --db). Still honored as a fallback
	// so existing invocations keep working. Alias, as above.
	memoryDBEnvLegacy = config.MemoryDBEnvLegacy

	// memoryDefaultDBFile is the shared knowledge base: every gopi instance
	// accumulates distilled lessons into the same file, so a later task can
	// find what an earlier one learned (跨任务累积). Alias of the single
	// definition (the suite ledger pin names the file too, so it cannot stay
	// private to this package).
	memoryDefaultDBFile = config.MemoryDBFileName

	// dbBusyTimeoutMSec lets concurrent gopi instances (or hermes processes)
	// sharing a DB queue for the write lock instead of failing with
	// SQLITE_BUSY. 15s covers a whole-patch import holding the write lock
	// (single-transaction import) while a peer's import waits its turn:
	// measured in devdoc/memsync-import-atomic.md, the db-sync normal case is
	// two writers interleaving 6-40-row patches at ~0.5s per row (the
	// per-write FTS triggers), so the previous 5s could expire mid-patch.
	dbBusyTimeoutMSec = 15000

	// dbSchemaWaitTries/Interval bound the concurrent-first-open poll
	// (waitForDistilledSchema): the winner's initKnowledgeBase completes in
	// well under a second under WAL, so 20×25ms covers it with margin while
	// keeping a foreign-DB rejection fast (a foreign file never gains the
	// distilled_qa table, so the poll always falls through promptly).
	dbSchemaWaitTries    = 20
	dbSchemaWaitInterval = 25 * time.Millisecond

	// sqliteMasterObjectCountSQL counts the schema objects of a SQLite file —
	// the probe that separates "an EMPTY file" (zero objects: no state to lose,
	// and the only shape a concurrent first-open can be observed in, because
	// the schema batch commits atomically) from "somebody else's database"
	// (zero objects is impossible in a used file). One literal, one meaning
	// (Rule 1b).
	sqliteMasterObjectCountSQL = `SELECT count(*) FROM sqlite_master`

	// schemaDDLBatchBusyRetries is the bounded retry of the whole batch on a
	// locked DB: BEGIN IMMEDIATE is not subject to busy_timeout in every
	// SQLite release (a known engine gap), so the first-open batch retries
	// only on a lock error — a deterministic, bounded, error-triggered loop,
	// never a sleep/timing wait. Any other statement error aborts immediately.
	schemaDDLBatchBusyRetries = 3

	// schemaDDLBatchLockSuffix matches SQLite's lock-contention error text
	// ("database is locked" / "database table is locked") to decide whether a
	// failed batch is retryable.
	schemaDDLBatchLockSuffix = "database is locked"

	// memoryBaseTableDDL is the distilled_qa base table — the first statement
	// of the atomic first-open schema batch (it must land in the SAME
	// transaction as the FTS tables + triggers, never before them).
	memoryBaseTableDDL = `CREATE TABLE IF NOT EXISTS distilled_qa (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL,
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			sources TEXT DEFAULT '',
			evidence TEXT DEFAULT '',
			confidence REAL DEFAULT 0.5,
			distilled_at REAL,
			retrieval_count INTEGER DEFAULT 0,
			last_retrieved REAL,
			tier TEXT DEFAULT 'warm',
			search_kw TEXT DEFAULT '',
			jieba_token TEXT DEFAULT '',
			lesson_type TEXT DEFAULT '',
			when_at TEXT DEFAULT '',
			exam_questions TEXT DEFAULT '',
			kw_what TEXT DEFAULT '',
			kw_why TEXT DEFAULT '',
			kw_when TEXT DEFAULT '',
			kw_where TEXT DEFAULT '',
			kw_who TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			round_index INTEGER DEFAULT 0,
			source_host TEXT DEFAULT ''
		)`

	// defaultConfidence / defaultTier are the store defaults when the model
	// does not provide value signals (same defaults as the reference engine).
	defaultConfidence = 0.85
	defaultTier       = "warm"

	// staleAfterDays is the freshness window for the possibly_stale signal:
	// knowledge older than this AND unverified by retrieval counts as stale.
	staleAfterDays = 30
	staleSeconds   = staleAfterDays * 24 * 3600

	// The when_at calendar spellings parseTimeMarker accepts, in its search
	// order. Named (Rule 1b) because the staleness RULE and its regression
	// fixture must render and read the SAME spelling — a re-typed layout at
	// either site would silently stop matching.
	whenAtDateTimeLayout = "2006-01-02 15:04:05"
	whenAtDateLayout     = "2006-01-02"
	whenAtSlashLayout    = "2006/01/02"

	// secondsPerDay converts a stored-instant difference into the whole-day
	// age the render prints. Named (Rule 1) because the age arithmetic and the
	// fixture that pins it must count the SAME unit — a re-typed 86400 at
	// either site would silently shift every rendered age by a day.
	secondsPerDay = 24 * 60 * 60

	// dedupQuestionRunes is the dedup key width: question[:200] + topic
	// (identical to the reference engine, character-based — not bytes).
	dedupQuestionRunes = 200

	// --- similarity dedup (feature 4: reference INSERT-后 trigram 查重) ---
	// ftsDedupCandidates caps the FTS5 prescreen pool (~the reference
	// DedupPolicy.FTS_DEDUP_CANDIDATES = 5) before the trigram-Jaccard check.
	ftsDedupCandidates = 5
	// trigramJaccardMerge is the Jaccard threshold above which two rows count
	// as near-duplicates (reference DedupPolicy.TRIGRAM_JACCARD_MERGE = 0.5).
	trigramJaccardMerge = 0.5
	// trigramGram is the n-gram width for the Jaccard similarity (3).
	trigramGram = 3

	// lessonJaccardMerge / lessonMinSharedTrigrams are the two conditions of the
	// same-lesson screen — a pair merges only when BOTH hold:
	//
	//	jaccard(whole retrieval surface, both sides) > lessonJaccardMerge
	//	shared trigram COUNT                             >= lessonMinSharedTrigrams
	//
	// Measured on the production store (5063 rows) to fix the values:
	//
	//	genuine twins     #1906/#1908  0.438  shared 1087   → merge
	//	                  #1491/#1494  0.439  shared  678   → merge
	//	                  same shape at test size, measured through this
	//	                  engine: 0.528/339 and 0.470/158    → merge
	//	same topic,
	//	different lesson  #1491/#1496  0.234  shared  396   → no
	//	                  #1494/#1496  0.243  shared  385   → no
	//	random long pairs (n=4000)     max 0.338           → no
	//	random any pairs  (n=6000)     2 above 0.40, both the "git blame <file>
	//	                               lines N-M" TEMPLATE shape with a different
	//	                               file: 0.562 shared 91, 0.429 shared 67 → no
	//
	// The shared-count floor is what makes the rule safe on SHORT rows: Jaccard
	// is length-normalised, so two short rows about one subject outscore two long
	// rows about one lesson (#1491/#1496 at 0.234), and the only short pairs that
	// cross 0.40 at all are template twins that must NOT merge. 120 sits in the
	// measured valley between those (67/91) and the smallest genuine pair
	// (158) — and it is also what keeps the short distinct fixtures inert
	// (0.500/shared 32 and 0.404/shared 23).
	//
	// lessonJaccardMerge is deliberately NOT trigramJaccardMerge, and the two
	// comparisons are not the same string: the old gate compared the incoming
	// QUESTION against the existing ANSWER[:300] (cross-field, asymmetric), which
	// on the production store scores a genuine re-worded twin 0.181-0.219 — never
	// reaching its own 0.5 gate (defect 2). Both sides here are the SAME string
	// shape, so the verdict is symmetric.
	lessonJaccardMerge      = 0.40
	lessonMinSharedTrigrams = 120
	// lessonDedupCandidates caps the same-topic candidate BAND the screen reads
	// (the most recent rows of that topic — the band is what makes the topic
	// twins of #1491/#1494/#1496 reachable at all).
	lessonDedupCandidates = 20
	// legacyCrossFieldAnswerRunes is the existing-answer prefix the LEGACY
	// cross-field screen reads (it stays 300 so that screen's verdict — the one
	// the agent_dedup and brew-guard callers inherit — is unchanged).
	legacyCrossFieldAnswerRunes = 300

	// exactKeySimilarity / noSimilarity render the store confirmation's
	// similarity field for the two paths that do not run a similarity
	// comparison: an exact-key hit IS a 1.0 identity match, a fresh insert
	// matched nothing. Named so the rendered number has a stated meaning
	// (Rule 1b) rather than being a stray literal in a format string.
	exactKeySimilarity = 1.0
	noSimilarity       = 0.0

	// memoryDedupDifferingMarker is the clause storeResult appends when the
	// exact-key screen folds a row whose BODY differs from the row it merged
	// into. The fold itself is unchanged (the existing row is the truth); what
	// this marker makes impossible is the SILENT fold — the caller can now tell
	// "the same lesson arrived again" from "a same-key row arrived carrying a
	// different body that was NOT stored" (Rule 15). It is a named constant
	// because three independent writers (the file import, the live asset sync,
	// brew) parse this confirmation text, and a hand-typed copy would drift.
	memoryDedupDifferingMarker = "differing body"

	// --- garbage / quality gate (feature 3: reference quality.py) ---
	// minQuestionLen / maxQuestionLen are the basic length gate bounds
	// (reference QualityPolicy.MIN/MAX_QUESTION_LEN).
	minQuestionLen = 6
	maxQuestionLen = 300
	// minAnswerLen is the minimum answer length for a knowledge candidate.
	minAnswerLen = 20
	// minQARatio is the minimum answer/question character-length ratio
	// (reference MIN_QA_RATIO = 0.15).
	minQARatio = 0.15
	// minMeaningfulChars is the minimum count of CJK/ASCII alphanumerics in
	// question+answer for a non-garbage candidate (reference MIN_MEANINGFUL_CHARS=4).
	minMeaningfulChars = 4

	// --- FTS sync fallback (feature 5) ---
	// ftsSyncTrigger tables are owned by the FTS5 external-content index; when
	// the hosts DB has no trigger (hermes production copy) we sync manually.
	ftsSyncFTSName = "distilled_qa_fts"

	// memoryFTSIndexTable is the FTS5 shadow table carrying ONE row per
	// actually-indexed base row. It is the per-row index probe: unlike
	// `SELECT count(*) FROM distilled_qa_fts` — which on an external-content
	// table reads the CONTENT table through the vtable and therefore looks
	// healthy for an unindexed row — a row appears here only after a real
	// index write (probe: trigger-inserted 1, unindexed 0, tokenizer
	// independent). A missing index table makes the probe error; callers must
	// read that as "cannot verify", never as "indexed".
	memoryFTSIndexTable = "distilled_qa_fts_docsize"

	// memoryTriIndexTable is the trigram FTS5 companion (CJK ≥3-char recall),
	// named once so the existence probes, the sync path and the rebuild
	// statements can never disagree about which index they mean.
	memoryTriIndexTable = "distilled_qa_tri"

	// --- trigram CJK recall (feature 1) ---
	// trigramMinToken is the minimum CJK run length that the trigram FTS
	// tokenizer indexes (SQLite's trigram needs ≥3 chars; 2-char CJK falls
	// back to a LIKE/title scan).
	trigramMinToken = 3
	// trigramRecallK caps the trigram/LIKE recall pool per route.
	trigramRecallK = 15
	// likeCJKRecallK is route 3's OWN scan depth, and it is deliberately not
	// trigramRecallK. Route 3's WHERE is a broad OR of LIKE predicates: on the
	// real corpus its candidate set runs into the THOUSANDS (median 4,412 rows
	// for the natural-question sample), so a depth of trigramRecallK did not
	// select the best 15 — it cut an unordered scan, and every row beyond the
	// cut was unreachable by the shared rescore no matter how well it answered
	// the query (the rescore re-orders the rows a route RETURNED; it never sees
	// the ones the LIMIT discarded). The depth now spans the relevance key's
	// reach: the key ranks the whole match set, so the route returns the rows
	// that actually answer the query instead of the first ones the planner
	// happened to visit. Measured dose-response in devdoc/memory-recall-pool.md
	// (route-3 depth → arena natural hits): 15 → 0/120, 60 → … , the value
	// below is the smallest depth that saturates the curve.
	likeCJKRecallK = 200

	// --- AND-subset degradation ladder (memsearch-subset-degrade) ---
	// The query side had only two extremes: all-AND (escapeQuery) and all-OR
	// (escapeQueryOR). A natural-language CJK/ASCII query whose full token set
	// matches no single row therefore fell straight to OR, which recalls rows
	// sharing ONE incidental token and ranks those old rows above the records
	// that answer most of the query. The missing middle is the ladder the
	// reference engine runs (`_adaptive_bfs`, hermes agent/fts5_query.py:180):
	// try the full AND, then progressively smaller AND-subsets, and only fall
	// back to OR when nothing qualifies.
	//
	// memsearchMaxMatchTerms bounds the route-1 MATCH expression (Rule 1a: no
	// bare literal). A query field is split into the pieces the tokenizer
	// itself would produce, so an unbounded query produced an unbounded
	// expression — measured 40,000 terms / 160 KB of MATCH text from a
	// 120 KB CJK query. The cap is the declared-safe degradation: the query's
	// first terms are searched, the tail is dropped, and the result is a
	// structured hit list — never a statement no engine will accept.
	// Nominal queries carry a handful of terms; this bounds the pathological
	// shape only.
	memsearchMaxMatchTerms = 256

	// memsearchMaxCJKRouteTerms caps the two CJK substring routes (the trigram
	// MATCH's OR-list and the 2-char LIKE clause set) at the number of distinct
	// terms each may carry. Both routes emit one term per 2- or 3-char gram of
	// the query, so without a cap the term count is LINEAR IN QUERY LENGTH: a
	// 300-char CJK query produced 1,197 terms and took 4.75s on the production
	// store (5 columns × N LIKE clauses), and a 5,000-char one would be two
	// orders of magnitude worse. The cap is the declared-safe degradation —
	// the query's leading grams are searched, the tail is dropped. A query
	// long enough to hit this is a document, not a query.
	memsearchMaxCJKRouteTerms = 32

	// memsearchMinANDTokens is the ladder's floor AND its activation gate: the
	// ladder only runs for queries of at least this many meaningful tokens
	// (below it a subset match is likely to be worse than the OR fallback and
	// the candidate space is not worth the walk). It is NOT a result-count
	// threshold — it never forces a hit into existence.
	memsearchMinANDTokens = 3
	// memsearchMaxANDTokens bounds the query the ladder will walk: the
	// candidate space is every subset of the token set, so an unbounded token
	// count is an unbounded number of COUNT queries (Rule 1a; the bound is
	// asserted by TestMemsearchLadderTokenCap).
	memsearchMaxANDTokens = 10
	// memsearchMaxCombos caps the total number of subset probes per search
	// (2^10-1 = 1023 for a 10-token query). The cap is the hard stop: the
	// ladder stops probing the moment it is reached, so a pathological query
	// can never turn into an unbounded scan.
	memsearchMaxCombos = 1023
	// memsearchMaxSubsetProbes caps how many qualifying subsets are collected
	// before the walk stops early — the best candidate after this many
	// qualifiers is good enough (ranking keeps more-terms-retained first, and
	// qualifiers arrive in exactly that descent order).
	memsearchMaxSubsetProbes = 64
	// memsearchMaxTokensPerSubset is the SQLite variable ceiling that bounds
	// how many rows a single subset may scan: a subset with more terms than
	// this is skipped rather than built into a too-large statement.
	memsearchMaxTokensPerSubset = 8

	// --- M1 roadmap: exam_questions + 5W1H parallel recall columns ---
	// m1ExamQuestions / m1KwCols are the new columns this feature adds to
	// distilled_qa (only added, never dropped — old schemas keep working).
	m1ExamQuestions = "exam_questions"
	// m1FTSCols is the full 12-column set of distilled_qa_fts after M1 — the
	// six legacy columns plus exam_questions and the five 5W1H label columns.
	m1FTSCols = "topic, question, answer, sources, search_kw, jieba_token, " +
		"exam_questions, kw_what, kw_why, kw_when, kw_where, kw_who"
	// m1TriCols is the trigram index column set after M1 (legacy three plus
	// exam_questions — alternate phrasings are whole CJK sentences; indexing
	// them makes an ≥3-char substring of an exam phrasing recall the record
	// even when the phrasing never appears in the body).
	m1TriCols = "question, answer, topic, exam_questions"

	// --- real CJK segmentation of the index column (jieba-token-real-segmentation) ---
	// cjkSegmentSep is the separator between the WORDS a real segmenter returns
	// before they enter the FTS index column. It is a SPACE, and that is the
	// whole contract with unicode61: unicode61 splits a token on whitespace, so
	// a space makes each segmented word its OWN index token, while gluing them
	// (the old comma+space bigram list, or no separator at all) would leave a
	// multi-word run as one un-tokenizable blob.
	//
	// Read the paired constant m1JiebaTextSep with this one: TWO separators,
	// one rule. m1JiebaTextSep joins the SOURCE COLUMNS into the text handed to
	// the segmenter (so 这些 and commit are two fields, never 这些commit), and
	// cjkSegmentSep joins the segmenter's WORDS into the indexed value (so
	// 逐步 and 缩小 are two tokens). Both exist because unicode61 splits on
	// whitespace only: any other joiner fuses what the query side then demands
	// as separate tokens, and the row becomes un-findable by its own phrasing.
	cjkSegmentSep = " "

	// --- the jieba_token source contract (search_kw absorption) ---
	// m1JiebaTextSep separates the composition parts of jieba_token's source
	// text. It is a SPACE, deliberately NOT a comma or no separator at all:
	// the source text is only ever handed to cjkseg.Words, whose own tokens are
	// joined by cjkSegmentSep — the two separators are the pair described
	// there, and this one exists so that two SOURCE COLUMNS can never fuse into
	// one segmentable run (这些…commit must present as 这些 + commit).
	m1JiebaTextSep = cjkSegmentSep
	// m1JiebaTextCols (below, a var — a slice is not a constant) are the
	// BASE-table columns whose text enters jieba_token, in composition order,
	// after topic+question+answer.
	//
	// Why they must enter it (the index-side/query-side tokenization
	// contract): jieba_token is the ONLY FTS-indexed column carrying CJK words
	// at a granularity below a whole run, and cjkseg.Words is the ONE segmenter
	// both sides call — the writer (deriveJiebaToken) and the query side
	// (cjkQuerySegments, route 1's segmented MATCH) — so index and query can
	// never speak at different granularities. Every other indexed CJK column is
	// tokenized by unicode61, which splits on whitespace/punctuation only: a
	// contiguous CJK run of any length is ONE token, so a query for a shorter
	// fragment of it can never match.
	// search_kw is where the LLM writes the phrasings a user will actually
	// search with (避峰 / 挪到夜间 — "the natural wording"), while question is
	// often a declarative title; indexing the phrasing only as a whole token
	// makes the row unreachable by the very phrasing written for retrieval.
	// The contract, the measured symptom and the reusable verification recipe:
	// devdoc/jieba-token-absorbs-search-kw.md; the segmenter swap and its
	// before/after recall measurement: devdoc/jieba-token-real-segmentation.md.

	// --- literal JSON-escape repair at the write funnel ---
	// memoryEscapeCoveragePercent / memoryEscapeCoverageRatio are the
	// payload-vs-prose discriminator for decodeLiteralEscapes: a field is a
	// JSON-escaped payload when the text each `\uXXXX` run CONSUMES covers at
	// least half of the field's non-space runes. Prose that merely discusses an
	// escape occupies a few runes of a long field, so it stays far below; a
	// payload is mostly escape runs. Measured on ~/.gopi/memory.db (2026-09-23):
	// escaped payload cells 0.74–1.00, prose-mention cells 0.00–0.13.
	memoryEscapeCoveragePercent = 0.50
	memoryEscapeCoverageRatio   = 2
	// memoryEscapeHexDigits / memoryEscapeHexBase are the shape of one escape
	// run ("\u" + 4 hex digits) and its decoded value's base.
	memoryEscapeHexDigits = 4
	memoryEscapeHexBase   = 16
	// memoryEscapeRunPrefix opens a literal JSON unicode escape.
	memoryEscapeRunPrefix = `\u`
	// memoryEscapeMinNonSpaceRunes / memoryEscapeMinRunes / memoryEscapeMinHexRuns
	// keep the decode off tiny fields: below these floors the coverage ratio is
	// noise (a single "\u0026" in a 6-rune cell would read as "mostly escaped").
	memoryEscapeMinNonSpaceRunes = 4
	memoryEscapeMinRunes         = 12
	memoryEscapeMinHexRuns       = 2
	// memoryEscapeReplacement is written in place of a decoded control rune
	// (\u0000..\u001f) — a JSON body may carry them, a text column should not.
	memoryEscapeReplacement = '\uFFFD'

	// --- tier semantics (round-3: value-signal ranking + cold maintenance) ---
	// hotTier / warmTier / coldTier are the three supported tier values. warm
	// is the store default; hot boosts; cold DEMOTES: cold items are excluded
	// from the default result set while any warm/hot item matches, and only
	// fill slots that warm/hot cannot — the "no longer 1st tier to return"
	// contract for low-quality rows without deleting them.
	hotTier  = TierHot
	warmTier = TierWarm
	coldTier = TierCold

	// tierWeightHot boosts hot items in the final score (above equal-coverage
	// warm rows). tierWeightCold is the demotion penalty applied to cold items
	// in the filler pass.
	tierWeightHot  = 2.0
	tierWeightCold = -6.0

	// stalePenalty is subtracted from the final score of possibly-stale hits
	// (aged AND never re-verified) — freshness-aware ordering.
	stalePenalty = 1.5

	// tokenCoverageWeight multiplies the query-token coverage count in the
	// final score: one covered query token outweighs a large BM25 gap, so an
	// item answering more of the query's intent ranks above one that merely
	// repeats rare tokens.
	tokenCoverageWeight = 2.0

	// --- feedback (P0: explicit feedback loop, merged design) ---
	// fbBetaPrior / fbGateScale / fbWeight define the Beta-posterior feedback
	// term: mean of the Beta(useful+prior, useless+prior) posterior shifted
	// around 0.5, gated by the effective sample count min(N_eff/4, 1) so
	// fewer than ~4 votes cannot move a record far (IR's "<5 judgments are
	// unreliable" rule). fbWeight keeps the term a small supplement to the
	// token-coverage signal (max |term| = 0.5) — feedback captures semantic
	// usefulness (task-outcome side), tokens capture lexical coverage
	// (query side); they are orthogonal, feedback is not a replacement.
	fbBetaPrior = 1.0
	fbGateScale = 4.0
	fbWeight    = 1.0

	// fbDecaySeconds is the feedback time-decay constant (Ebbinghaus-style):
	// each vote's contribution decays as e^(−Δt/τ) with τ = 90 days, applied
	// on the materialized counts at write/recompute time — old feedback
	// cannot lock a record forever, and a mistaken vote fades on its own.
	fbDecaySeconds = 90 * 24 * 3600

	// fbColdSuggestThreshold is the --suggest-cold gate: a record is a
	// cold-demotion candidate when it accumulated ≥3 (decayed) useless votes
	// and zero useful. Suggestions only — automatic demotion stays off
	// (only-up, never auto-down: a wrong demotion is invisible once the row
	// stops surfacing, a wrong upgrade heals itself via more feedback).
	fbColdSuggestThreshold = 3.0

	// --- M2 roadmap: confidence lifecycle + hot tier closure ---
	// confDecayTauSeconds is the confidence forgetting time constant: stored
	// confidence decays as e^(−Δt/τ) where Δt is the age of the LAST
	// verification (distilled_at, or last_retrieved when the record has been
	// re-verified since). τ is deliberately fbDecaySeconds (90 days): the
	// confidence curve and the feedback curve describe the same fact — "how
	// long unverified" — and two constants would be two magic numbers for
	// one idea.
	confDecayTauSeconds = fbDecaySeconds

	// confDecayFloor is the effective-confidence lower clamp (also the scan
	// floor for undated rows). Zero is unreachable — a record that ranks last
	// still surfaces when nothing better matches, and the value signal must
	// stay legible (conf=0.00 reads as broken, 0.05 reads as "doubtful").
	confDecayFloor = 0.05

	// confReverifySaturate is the retrieval count at which the re-verify
	// reward saturates: each retrieval is an implicit "someone needed this
	// and it was still true" vote, credited at confReverifyRewardSaturate
	// (below) — capped, so a heavily-retrieved old record cannot climb
	// unboundedly above a healthy new one.
	confReverifySaturate = 10

	// confReverifyRewardSaturate is the reward a record receives once it has
	// been retrieved ≥ confReverifySaturate times (pro-rated before that).
	// Value: one full re-verification ≈ undoing ~2 days of decay; enough to
	// keep frequently-used knowledge near its stored confidence, never enough
	// to outrank a fresher equally-used record by itself.
	confReverifyRewardSaturate = 0.15

	// confVoteStepUseful / confVoteStepUseless are the per-vote confidence
	// steps applied inside the MemoryFeedback transaction (M2: the feedback
	// loop now also moves the STORED confidence — the machine's only write
	// to that column). Useless steps down 2× because a contradicted memory
	// that is trusted does active damage (bug-report culture: useless votes
	// require a contradiction or failure, useful is a mere confirmation) —
	// but both steps stay small: a confidence unit weighs 1.0 in the rescore,
	// one token coverage weighs 2.0, so a single vote cannot reorder results;
	// fbColdSuggestThreshold (3.0) decayed useless votes ≈ −0.4 confidence,
	// one tier weight (−6.0), before the manual cold demotion is suggested.
	confVoteStepUseful  = 0.05
	confVoteStepUseless = -0.10

	// hotStaleDays is the re-verification window for hot records: hot and
	// un-retrieved for longer than this renders hot_stale in the meta line —
	// the machine-readable signal of the "no permanent hot" philosophy
	// (stale hot is still returned — it just visibly stops being trusted).
	// Demotion to warm/cold stays a manual memory_update (only-up never
	// auto-down), the hint just makes staleness auditable at a glance.
	hotStaleDays = 7

	// hotResidentInjectLimit / hotResidentMaxChars bound the task-start
	// resident injection: at most this many hot records, at most this many
	// chars of rendered summary — existence hints, not full-text dumps (the
	// LLM that needs the detail searches for it; the budget keeps the
	// injection a rounding error in context terms).
	hotResidentInjectLimit = 3
	hotResidentMaxChars    = 600

	// effConfRenderDelta is the minimum |eff − stored| gap for the meta line
	// to show eff_conf: below one rendered cent (0.01) the two values carry
	// the same information — rendering it would be noise.
	effConfRenderDelta = 0.01

	// The age render's field spellings and the units it prints. Named (Rule 1)
	// because the render, the tool description and the regression test must
	// read the SAME spelling — a re-typed "stored=" at one site would rename
	// the field for the LLM without anything failing.
	memoryNowAnchorPrefix     = "now="
	memoryStoredPrefix        = "stored="
	memoryAgePrefix           = "age="
	memoryAgeUnit             = "d"
	memoryAboutPrefix         = "about="
	memoryStoredUnknownMarker = "stored=unknown"
)

// m1KwCols holds the 5W1H label columns in scan order (an array literal is
// not a Go constant, so it lives in a var next to the const block above).
var m1KwCols = [5]string{"kw_what", "kw_why", "kw_when", "kw_where", "kw_who"}

// m1JiebaTextCols is the ordered column set whose text enters jieba_token
// (see the contract note in the const block above). search_kw comes last
// because it is passed explicitly at both write sites rather than read.
var m1JiebaTextCols = []string{m1ExamQuestions, "kw_what", "kw_why", "kw_when", "kw_where", "kw_who", "search_kw"}

// feedback ratings (memory_feedback tool / CLI). Three-valued: useful
// (adopted/helped), useless (contradicted/failed/misleading — bug-report
// culture: only contradiction or failure, not merely unneeded), unused
// (seen but not used — neutral, recorded but counts toward neither side).
const (
	ratingUseful  = RatingUseful
	ratingUseless = RatingUseless
	ratingUnused  = RatingUnused
)

// RatingUseful / RatingUseless / RatingUnused are the rating values the
// memory_feedback surface accepts, exported so in-process callers (the
// hippocampus core loop) never type the wire string inline (Rule 1b/13).
const (
	RatingUseful  = "useful"
	RatingUseless = "useless"
	RatingUnused  = "unused"
)

// MemoryHit mirrors one search result record of the memory engine contract:
// JSON array of {"id","topic","question","answer","tier","confidence",
// "lesson_type","when_at","sources","possibly_stale","intent_hits"}. The
// value-signal fields (tier/confidence/lesson_type/when_at) are kept in the
// rendered result verbatim so the LLM can judge each hit's worth itself.
type MemoryHit struct {
	ID         int     `json:"id"`
	Topic      string  `json:"topic"`
	Question   string  `json:"question"`
	Answer     string  `json:"answer"`
	Tier       string  `json:"tier"`
	Confidence float64 `json:"confidence"`
	LessonType string  `json:"lesson_type"`
	WhenAt     string  `json:"when_at"`
	// DistilledAt is the write instant the row was distilled at (the
	// distilled_at column, Unix seconds; absent on a legacy schema or a row
	// written before the column existed — omitempty keeps the JSON contract
	// unchanged there). The render prints it as stored= (the ONE fact that
	// says how OLD the row is), while about= carries a date the row's own
	// TEXT states — two different facts that when_at used to conflate into
	// one field, so a row describing a 2026-08-01 incident and written today
	// looked like it had been written on 2026-08-01.
	DistilledAt   float64  `json:"distilled_at,omitempty"`
	Sources       []string `json:"sources"`
	PossiblyStale bool     `json:"possibly_stale"`
	IntentHits    int      `json:"intent_hits"`
	// Tokens is the query-token coverage signal: how many distinct meaningful
	// query tokens appear in the hit's topic+question+answer. It is the
	// primary ranking key (an item covering more of the query's intent ranks
	// above one matching only rare filler tokens) and is rendered into the
	// result meta line so the user can judge each hit's relevance — and spot
	// the low-quality rows worth demoting via memory_update --tier cold.
	Tokens int `json:"tokens,omitempty"`

	// FbUseful / FbUseless are the decayed useful/useless vote masses
	// materialized on the row (zero when the schema has no feedback columns,
	// or the record never received a vote — omitempty keeps the JSON and the
	// rendered meta identical to the pre-feedback contract). Rendered as
	// fb=<posterior mean>(<decayed votes>) so the LLM can see a hit's
	// feedback history and judge it — auditable judging per LLM-as-judge
	// practice.
	FbUseful  float64 `json:"fb_useful,omitempty"`
	FbUseless float64 `json:"fb_useless,omitempty"`

	// M2: retrievalCount feeds the effective-confidence re-verify reward and
	// EffConfidence is the decayed+re-verified confidence the ranking and the
	// rendered meta both consume. Unexported count / exported signal: the
	// JSON contract gains eff_conf (omitempty — legacy schemas without the
	// column render exactly as before) while the raw counter stays internal.
	retrievalCount float64

	EffConfidence float64 `json:"eff_conf,omitempty"`

	// M2: HotStale flags a hot record un-retrieved for longer than
	// hotStaleDays — the "no permanent hot" signal rendered as hot_stale in
	// the meta line.
	HotStale bool `json:"hot_stale,omitempty"`

	// rank is the BM25 rank (fts.rank, negative = better) of the route that
	// recalled this hit — the tiebreak after the value-signal score. It is
	// unexported on purpose: it only feeds the in-process sort and must not
	// leak into the rendered JSON (which round-trips through json.Marshal /
	// json.Unmarshal before rendering).
	rank float64

	// examQuestions / kwLabels carry the M1 parallel recall text (the
	// pre-generated exam phrasings and the 5W1H labels, comma-joined) into
	// the token-coverage scorer. Unexported like rank: they feed scoring
	// only and stay out of the rendered JSON contract.
	examQuestions string
	kwLabels      string

	// Origin is the provenance pointer this row carries back to the session
	// round it was captured from (memory_origin.go): the session id, the
	// session's own 1-based round number and the node the row was written on.
	// A pointer, and omitempty, so a row that recorded nothing — every
	// pre-feature row, an import, a CLI store — renders exactly as before.
	Origin *MemoryOrigin `json:"origin,omitempty"`
}

// MemoryOriginOf returns the hit's provenance pointer as one value; a hit that
// recorded nothing gets the zero MemoryOrigin (memory_origin.go owns the type
// and the completeness rule).
func (h MemoryHit) MemoryOriginOf() MemoryOrigin {
	if h.Origin == nil {
		return MemoryOrigin{}
	}
	return *h.Origin
}

// MemoryOriginWire exposes the hit's provenance pointer in its WIRE shape, the
// value the sync channel carries and the explicit unreachable state is computed
// from (memory_sync_origin.go owns both). It exists so the renderer and the
// transport classify a pointer with ONE implementation: a row read off this
// node's own store and a row that just arrived in a patch must not disagree
// about whether the detail is reachable here.
func (h MemoryHit) MemoryOriginWire() *SyncRowOrigin {
	o := h.MemoryOriginOf()
	if o.None() && strings.TrimSpace(o.Host) == "" {
		return nil
	}
	return &SyncRowOrigin{SessionID: o.SessionID, Round: o.Round, Host: o.Host}
}

// MemoryEmpty mirrors the no-hit contract: {"empty": true, "raw_hint": true}.
type MemoryEmpty struct {
	Empty   bool `json:"empty"`
	RawHint bool `json:"raw_hint"`
}

// memoryEmptyHint is the tool result for a clean no-hit: the topic was never
// distilled, so the model is steered to brew it — store the lesson after the
// task completes (取不到 → memory(action=store)).
const memoryEmptyHint = "No distilled memory found for this query. " +
	"Nothing has been distilled on this topic yet — after you finish the task, " +
	"call memory(action=store) to save what you learned so the next agent finds it."

// ---------------------------------------------------------------------------
// DB location policy.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// jieba_token backfill (the segmenter swap's one-shot).
// ---------------------------------------------------------------------------

// jiebaBackfillBatch bounds how many rows are re-derived inside ONE
// transaction. The work is a bounded UPDATE over the ledger's own table — no
// external resource, no network — but a ledger can be large and SQLite holds
// the write lock for the whole transaction, so the backfill commits in
// batches and a concurrent search is never locked out for the whole scan.
// A constant, not a literal (Rule 1a); the value is a queue depth, not a
// measurement.
const jiebaBackfillBatch = 500

// jiebaBackfillSelect reads every row's jieba_token SOURCES and the stored
// value: the same column list deriveJiebaToken consumes at write time, so the
// backfill cannot derive from a different text than the writer does. Legacy
// rows are the target, so the scan is by (id) with no predicate — MySQL-style
// "WHERE token IS NULL" would miss the rows whose token is the OLD spelling
// but not NULL, which is the whole population this fixes.
const jiebaBackfillSelect = "SELECT id, topic, question, answer, jieba_token FROM distilled_qa ORDER BY id"

// ScanJiebaTokens reports how many rows would be re-derived, writing nothing.
// It is the --dry-run half of BackfillJiebaTokens and shares its derivation
// rule exactly, so the report cannot promise a different set than the write.
func ScanJiebaTokens(ctx context.Context, db *sql.DB) (scanned, wouldChange int, err error) {
	return jiebaBackfill(ctx, db, true)
}

// BackfillJiebaTokens re-derives the jieba_token column for every row whose
// stored value is not what the CURRENT segmenter produces, and reindexes the
// FTS row so the token index agrees. It returns (scanned, changed).
//
// It rewrites exactly ONE column. The row's prose, tier, confidence, usage
// counters and timestamps are read-only inputs to the derivation, never
// outputs — a backfill that re-answered a question would be a data mutation
// wearing an index rebuild's clothes.
//
// Two deliberate refusals:
//   - a row whose text yields NO words keeps its stored token (blanking it
//     would make the row permanently un-findable, which is worse than a stale
//     spelling);
//   - a row already carrying the current derivation is not written at all, so
//     the command is idempotent and safe to re-run after an interruption.
func BackfillJiebaTokens(ctx context.Context, db *sql.DB) (scanned, changed int, err error) {
	return jiebaBackfill(ctx, db, false)
}

// jiebaBackfill is the ONE implementation behind both public entries: dry
// only decides whether the UPDATE is issued, never what counts as a change.
func jiebaBackfill(ctx context.Context, db *sql.DB, dry bool) (scanned, changed int, err error) {
	if db == nil {
		return 0, 0, fmt.Errorf("jieba backfill: no ledger handle")
	}
	rows, err := db.QueryContext(ctx, jiebaBackfillSelect)
	if err != nil {
		return 0, 0, fmt.Errorf("jieba backfill: %w", err)
	}
	var batch []jiebaBackfillRow
	for rows.Next() {
		var (
			id              int64
			topic, question string
			answer          string
			stored          string
		)
		if err := rows.Scan(&id, &topic, &question, &answer, &stored); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return scanned, changed, fmt.Errorf("jieba backfill: %w", err)
		}
		scanned++
		want := deriveJiebaToken(topic, question, answer, "", nil)
		if want == "" || want == stored {
			continue // nothing derivable, or already current (idempotent)
		}
		changed++
		if dry {
			continue
		}
		batch = append(batch, jiebaBackfillRow{id: id, tok: want})
		if len(batch) >= jiebaBackfillBatch {
			if err := applyJiebaTokenBatch(ctx, db, batch); err != nil {
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
				return scanned, changed, err
			}
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
		return scanned, changed, fmt.Errorf("jieba backfill: %w", err)
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if dry || len(batch) == 0 {
		return scanned, changed, nil
	}
	if err := applyJiebaTokenBatch(ctx, db, batch); err != nil {
		return scanned, changed, err
	}
	return scanned, changed, nil
}

// jiebaBackfillRow is one re-derivation to write: the row id and the token
// the current segmenter produces for it.
type jiebaBackfillRow struct {
	id  int64
	tok string
}

// applyJiebaTokenBatch writes one batch of re-derived tokens, each in its own
// transaction: the UPDATE touches the token column and the FTS row is
// refreshed by the table's OWN sync trigger, so no index DDL is needed here
// and an interrupted run is repaired by re-running the command.
func applyJiebaTokenBatch(ctx context.Context, db *sql.DB, batch []jiebaBackfillRow) error {
	for _, p := range batch {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("jieba backfill: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE distilled_qa SET jieba_token=? WHERE id=?", p.tok, p.id); err != nil {
			tx.Rollback() //stylegate:ignore the UPDATE error is returned below and is the one that matters
			return fmt.Errorf("jieba backfill row %d: %w", p.id, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("jieba backfill row %d: %w", p.id, err)
		}
	}
	return nil
}

// MemoryDBPath is the ledger path this process would open (the ONE ranked
// resolver, config.ResolveLedger) — exported so a maintenance verb reports the
// same file the engine actually uses instead of a second guess (Rule 14).
func MemoryDBPath() string { return memoryDBPath() }

// OpenMemoryLedger opens the ledger for a maintenance verb at a path a caller
// resolved. It mirrors initKnowledgeBase's readiness contract (the schema is
// ensured) so a verb that mutates rows operates on the production shape.
//
// THE readOnly ARM IS THE DSN-LEVEL READ-ONLY OPEN, NOT openDB(path, true)
// (measured 2026-10-30, task 01a1892d). This is the opener behind
// `gopi memory converge-keywords --dry-run` and `gopi memory backfill-tokens
// --dry-run`, and on a device-backed COPY of the real ledger one dry run through
// the old shape REWROTE memory.db and DELETED memory.db-wal — a "report what
// would change, write nothing" verb that destroyed the WAL it was reading. The
// same promise, the same remedy as openSearchDB: mode=ro + query_only at the
// DSN, no DDL, and a failed open is REPORTED rather than downgraded (Rule 15).
func OpenMemoryLedger(path string, readOnly bool) (*sql.DB, error) {
	db, err := openKnowledgeBaseDB(path, readOnly)
	if err != nil {
		return nil, err
	}
	if readOnly {
		return db, nil
	}
	if err := ensureSchema(context.Background(), db); err != nil {
		db.Close() //stylegate:ignore handle closed after its last caller finished
		return nil, err
	}
	return db, nil
}

// memoryDBPath resolves the knowledge base through the ONE ranked chain
// (Rule 2/14): `--db` (handed in by the CLI's own flag parser) > GOPI_MEMORY_DB >
// GOPI_HIPPO_DB (legacy alias) > settings.json "memory_db" > <gopi dir>/memory.db.
//
// The ranking itself lives in internal/config (config.ResolveLedger) because
// `gopi config show` must report the SAME answer this process opens — a second
// copy here is exactly how the report and the loader drifted apart. This
// function keeps only what is local to this package: the CWD-relative last
// resort for a host with no resolvable home.
func memoryDBPath() string {
	if p := config.LedgerPath(); p != "" {
		return p
	}
	return memoryDefaultDBFile // last resort: CWD-relative
}

// defaultMemoryDBPath is the shared default location, independent of any
// environment override (used to decide whether a schema-less file is gopi's
// own DB to heal, or a foreign DB to refuse touching).
func defaultMemoryDBPath() string {
	gd, err := config.GopiDir()
	if err != nil {
		return memoryDefaultDBFile // last resort: CWD-relative
	}
	return filepath.Join(gd, memoryDefaultDBFile)
}

// ---------------------------------------------------------------------------
// DB helpers.
// ---------------------------------------------------------------------------

// openDB opens the SQLite database with a single serialized connection and a
// busy timeout.
//
// THE readOnly FLAG IS NOT A READ-ONLY LEDGER OPEN, and must never be used as
// one (measured 2026-10-30, tasks 01a188f7 and 01a1892d). It still uses
// LedgerDSN's `_txlock=immediate`, its first statement is a PRAGMA WRITE
// (busy_timeout), and SQLite checkpoints the WAL away at close — measured on a
// device-backed ledger copy, `openDB(path, true)` rewrote the main file
// (7c08da3c… → 4bbb8467…) and LEFT NO -wal AT ALL. PRAGMA query_only stops SQL
// statements; it does not stop the LOCK, the PRAGMA writes or the checkpoint.
//
// A caller that promised not to write a ledger asks for the DSN-level read-only
// shape instead: openKnowledgeBaseDB (this file, for a memory ledger) or
// openLedgerIndexDBReadOnly (ledger_schema_union.go, for a shared ledger face) —
// both render LedgerReadOnlyDSN (mode=ro + query_only) and SKIP every schema
// DDL. Rule 15: a failed read-only open is REPORTED; it never degrades into this
// writable shape, because that fallback IS the defect.
func openDB(path string, readOnly bool) (*sql.DB, error) {
	// _txlock=immediate: every database/sql BEGIN issues BEGIN IMMEDIATE —
	// the write lock is taken at BEGIN, not deferred to the first write, so
	// a whole-transaction writer (memsync import) queues on busy_timeout
	// up front instead of failing mid-transaction with SQLITE_BUSY. Reads
	// (search) never BEGIN a transaction, so this changes nothing for them.
	sqldb, err := sql.Open("sqlite", path+"?_txlock=immediate")
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1) // SQLite: one writer at a time; serialize access.
	if _, err := sqldb.Exec("PRAGMA busy_timeout = " + strconv.Itoa(dbBusyTimeoutMSec)); err != nil {
		sqldb.Close() //stylegate:ignore extra handle closed after its last caller finished
		return nil, err
	}
	if readOnly {
		if _, err := sqldb.Exec("PRAGMA query_only = ON"); err != nil {
			sqldb.Close() //stylegate:ignore extra handle closed after its last caller finished
			return nil, err
		}
	}
	return sqldb, nil
}

// enableWAL puts the DB in WAL journal mode with synchronous=NORMAL — from
// the very first schema DDL (t-1788361069). A fresh DB's init runs 40+
// DDL statements, each its own commit; under the default rollback journal
// + synchronous=FULL every commit fsyncs (measured ~155ms/DDL on a loaded
// disk) and ONE stalled fsync can block for minutes — the full-suite
// connectionOpener hang. WAL turns the DDL storm into ~0.2ms commits
// (measured 29ms/DDL in WAL, 0.2ms with synchronous=NORMAL); NORMAL under
// WAL still fsyncs at checkpoint, so durability is only relaxed for the
// tail of the WAL on power loss — the same tradeoff ensureSchema's
// end-of-schema best-effort WAL already accepted. Only ever called on
// gopi-owned DBs (initKnowledgeBase/migrate paths) — never on a foreign
// file, because journal_mode is a persistent header property and mutating
// a foreign DB violates the read-only-compat contract.
func enableWAL(ctx context.Context, db *sql.DB) error {
	return enableWALWithSync(ctx, db, ledgerSyncMode)
}

// enableWALWithSync is enableWAL with an explicit synchronous= mode, for the one
// caller whose mode is not the production default (initKnowledgeBase — see
// firstOpenSyncMode).
//
// synchronous IS PER-CONNECTION (unlike journal_mode, which is a persistent
// header property), so the mode scopes to exactly the handle passed in and cannot
// leak into the handles the caller's own openers hand out afterwards: a
// non-production mode here is provably confined to the build it was chosen for.
// The mode is a value from this package's closed constant set (ledgerSyncMode /
// ledgerSyncModeWorkingFS), never caller-supplied text.
func enableWALWithSync(ctx context.Context, db *sql.DB, syncMode string) error {
	// The statement sequence lives in ledgerPragmaStmts (firstopen_pragma_guard.go)
	// rather than inline: it is the ONE place a regression test can reintroduce
	// the pre-fix pragma state to prove the first-open guard's red, and the same
	// list the guard reads its expectations from.
	for _, stmt := range ledgerPragmaStmts(syncMode) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

// firstOpenSyncMode is the synchronous= mode the FIRST-OPEN schema build applies
// to its own connection for the ledger at path.
//
// WHY THIS DECISION EXISTS (the mechanism, measured — see
// internal/tools/firstopen_probe_test.go and devdoc/first-open-durability.md):
// the build is a SEQUENCE of write transactions (the atomic schema batch, the
// feedback table + its two ALTERs, the tasks tables/FTS companions/triggers). In
// WAL with synchronous=FULL/NORMAL each commit syncs the WAL, and on this host
// that syscall parked in ext4's journal-commit wait, /proc/self/task/*/wchan =
// jbd2_log_wait_commit and rq_qos_wait — a SHARED queue, so one commit waits
// behind every other writer's on the device. Measured on the same fresh-DB
// workload: 9-10ms for the whole first open on tmpfs vs 0.5-2.2s PER PHASE on the
// block device, with a single fsync probe at 0.7-1.3s under io pressure. That is
// how a first open reaches minutes.
//
// WHAT IT OWES DEPENDS ON WHERE THE LEDGER LIVES, and that is MEASURED by
// mounted device id (memoryFilesystemLedger → internal/scratch.MemoryFilesystem),
// never assumed from a path spelling (Rule 22):
//
//   - memory filesystem (tmpfs/ramfs): the schema is being built into a fresh
//     file that cannot outlive a reboot anyway, and a process-side crash leaves
//     the pinned scratch tree to be GC'd and rebuilt — the sync protects nothing,
//     so the build runs ledgerSyncModeWorkingFS (OFF);
//   - anything else — the operator's ~/.gopi ledger, any path on a device, any
//     directory whose device cannot be measured: ledgerSyncMode (NORMAL). A
//     ledger that lands on a block device keeps paying for durability, because
//     losing it is real data loss.
func firstOpenSyncMode(path string) string {
	return firstOpenLedgerSyncMode(memoryFilesystemLedger(path))
}

// firstOpenLedgerSyncMode is the decision itself, as a function of the ONE
// measured fact it is made from: whether the ledger's directory is PROVEN to be
// a memory filesystem (memoryFilesystemLedger).
//
// It exists as its own total function because that is what the decision IS — a
// mapping with two inputs and two outputs — and because the alternative way to
// test the mapping is to find a memory filesystem on the host, which a
// block-backed host does not have (524156af retired the scratch home's memory
// backing, so the framework temp dir every suite reaches is on disk). The
// decision table therefore asserts this function directly for the memory arm,
// and asserts the MEASUREMENT on a real path for the device arm; neither half
// can drift from the other, because firstOpenSyncMode above is a one-line call
// into this function.
func firstOpenLedgerSyncMode(memoryBacked bool) string {
	if memoryBacked {
		return ledgerSyncModeWorkingFS
	}
	return ledgerSyncMode
}

// ledgerBuildBatchNeeded reports whether a schema build for the ledger at path
// must go through the atomic batch: it does when the ledger is NOT provably on a
// memory filesystem, i.e. exactly when each of its implicit commits costs a real
// device sync. It is the tasks-schema half of firstOpenSyncMode's decision (same
// measurement, named once so the two halves cannot drift).
func ledgerBuildBatchNeeded(path string) bool {
	return !memoryFilesystemLedger(path)
}

// memoryFilesystemLedger reports whether path's directory is PROVABLY on a memory
// filesystem (tmpfs/ramfs), by the same measured-device classifier Rule 22's
// gates use. An unmeasurable directory is NOT proven memory-backed, so its caller
// keeps the durable mode: the burden of proof is on the cheap path, never on
// durability.
func memoryFilesystemLedger(path string) bool {
	mem, _, err := scratch.MemoryFilesystem(filepath.Dir(path))
	return err == nil && mem
}

// QueryExecer is the minimal SQL surface the store core needs. Both
// *sql.DB and *sql.Tx satisfy it, so one core implementation serves the
// standalone MemoryStore path AND the single-transaction import path
// (memory_sync.go): the import never re-opens a connection per row — it
// hands its transaction in, so the whole patch lands commit-or-rollback.
type QueryExecer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ensureSchema creates the distilled_qa table, its FTS5 external-content
// indexes and the keep-in-sync triggers. Idempotent — safe to run every
// start. journal_mode=WAL is best-effort: readers no longer block the writer
// when several gopi instances share the DB (failure tolerated — busy_timeout
// already serializes writes).
//
// ATOMIC FIRST-OPEN (the t-mem-flake fix): the whole schema — base table,
// both FTS tables and all six keep-in-sync triggers — is published inside ONE
// exclusive transaction instead of a sequence of autocommit DDLs. A
// concurrent store/search connection can therefore never observe distilled_qa
// without its index and triggers: a foreign reader sees the empty pre-schema
// DB until COMMIT and the complete schema afterwards (SQLite flips the schema
// cookie at COMMIT — verified on this engine). Before the fix a store landing
// between the base-table commit and the trigger commits wrote its row through
// the triggerless window, then the installer's triggers landed,
// ftsTriggersOwned saw them and skipped syncFTSRow — the row stayed
// unindexed forever and the search returned the empty hint.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	return execSchemaBatch(ctx, db, func(q QueryExecer) error {
		// 1. Base table first, inside the batch — the FTS DDL below is
		// rendered for the layout this CREATE produces (the column probe,
		// running on the SAME transaction, sees the table it describes).
		if _, err := q.ExecContext(ctx, memoryBaseTableDDL); err != nil {
			return err
		}
		// Test seam only: the hook runs after the base table and BEFORE the
		// index/trigger statements — the exact window the t-mem-flake lived
		// in, where a concurrent connection used to see distilled_qa without
		// its index. Production never sets it.
		if schemaBatchProbeHook != nil {
			schemaBatchProbeHook()
		}
		// 2. Feature 1: the FTS index tables and keep-in-sync triggers are
		// rendered for the CURRENT base-table layout (the M1 columns when
		// present, the legacy sets when not — a seeded legacy schema in tests
		// must keep building), so they are built per call instead of being
		// hard-coded statement literals. The trigram index (feature 1:
		// unicode61 treats a contiguous CJK run as one token, so ≥3-char CJK
		// substring recall runs through distilled_qa_tri; 2-char CJK falls to
		// the LIKE/title scan) is rendered by the same helpers.
		stmts := append(m1TriVirtualTable(ctx, q), m1FTSVirtualTable(ctx, q))
		stmts = append(stmts, m1TriTriggers(ctx, q)...)
		stmts = append(stmts, m1FTSTriggers(ctx, q)...)
		for _, s := range stmts {
			if _, err := q.ExecContext(ctx, s); err != nil {
				return err
			}
		}
		return nil
	})
}

// execSchemaBatch runs the schema DDL build inside ONE exclusive transaction,
// so the DB flips from "no schema" to "complete schema" in a single COMMIT —
// no concurrent connection can ever observe a base table without its index
// and triggers. The batch is retried only on lock contention, bounded by
// schemaDDLBatchBusyRetries; every other failure aborts at once. The
// transaction releases its write lock and its pinned connection on every
// early return, so a partial schema is never published.
func execSchemaBatch(ctx context.Context, db *sql.DB, build func(q QueryExecer) error) error {
	var lastErr error
	for attempt := 0; attempt <= schemaDDLBatchBusyRetries; attempt++ {
		lastErr = execSchemaBatchOnce(ctx, db, build)
		if lastErr == nil {
			return nil
		}
		if !isLockContentionErr(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// execSchemaBatchOnce is one exclusive-transaction attempt of the batch.
// The transaction pins ONE connection (database/sql hands the same handle to
// every statement of the tx), so the whole schema is one atomic unit on the
// wire.
func execSchemaBatchOnce(ctx context.Context, db *sql.DB, build func(q QueryExecer) error) error {
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate (openDB) ⇒ BEGIN IMMEDIATE
	if err != nil {
		return fmt.Errorf("memory schema init: begin: %w", err)
	}
	//stylegate:ignore rollback after a successful commit is a no-op (sql.ErrTxDone); it is what releases the write lock on an early return
	defer func() { _ = tx.Rollback() }()
	if err := build(tx); err != nil {
		return fmt.Errorf("memory schema init: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory schema init: commit: %w", err)
	}
	return nil
}

// isLockContentionErr reports whether err is SQLite lock contention (the one
// class the schema batch retries) rather than a broken statement.
func isLockContentionErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), schemaDDLBatchLockSuffix)
}

// ensureFeedbackSchema creates the append-only memory_feedback event table
// and the EMA materialized columns on distilled_qa (useful_count /
// useless_count / fb_last_at), PRAGMA-probed and ALTERed in when missing —
// the same dynamic-column pattern as the rest of the engine (zero migration
// scripts; old/foreign schemas simply lack the columns and read as zero).
// The event table is the source of truth for audit / rollback / decay
// recompute; the materialized columns keep search at zero join cost (the
// gopi-side EMA performance detail absorbed into the merged design).
func ensureFeedbackSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS memory_feedback (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		memory_id  INTEGER NOT NULL,
		rating     TEXT NOT NULL CHECK (rating IN ('useful','useless','unused')),
		reason     TEXT DEFAULT '',
		task_id    TEXT DEFAULT '',
		created_at REAL NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		"CREATE INDEX IF NOT EXISTS idx_feedback_memory ON memory_feedback(memory_id, created_at)"); err != nil {
		return err
	}
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return err
	}
	// M2: tier/confidence are healed here too — the feedback transaction now
	// writes confidence (vote step), so a legacy schema lacking either column
	// must get it before the first vote touches it (same probe+ALTER pattern;
	// defaults match ensureSchema's CREATE).
	for _, c := range []struct {
		name string
		ddl  string
	}{
		{"useful_count", "REAL DEFAULT 0"},
		{"useless_count", "REAL DEFAULT 0"},
		{"fb_last_at", "REAL DEFAULT 0"},
		{"tier", "TEXT DEFAULT '" + defaultTier + "'"},
		{"confidence", "REAL DEFAULT " + strconv.FormatFloat(defaultConfidence, 'f', -1, 64)},
	} {
		if cols[c.name] {
			continue
		}
		// The PRAGMA probe above has a TOCTOU window under concurrent first
		// use (two gopi processes each opening the shared ~/.gopi/memory.db
		// both see the column missing, both run the ALTER — the loser gets
		// "duplicate column name"). Swallow exactly that error, the same
		// idempotent-ALTER pattern as tasksP2Columns in tasks.go: the column
		// then exists either way, which is all this migration needs.
		if _, err := db.ExecContext(ctx, "ALTER TABLE distilled_qa ADD COLUMN "+c.name+" "+c.ddl); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	return nil
}

// tableExists reports whether the named table (or virtual table) exists.
// name is an internal constant — never user input — so no quoting is needed.
func tableExists(ctx context.Context, q QueryExecer, name string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx,
		"SELECT 1 FROM sqlite_master WHERE type IN ('table','view') AND name=?", name).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ---------------------------------------------------------------------------
// M1 schema helpers: dynamic FTS column sets.
// ---------------------------------------------------------------------------
//
// The M1 columns (exam_questions + kw_what..who) exist only after the ALTER
// migration runs, but the CREATE statements in ensureSchema must work on
// brand-new DBs (columns in CREATE TABLE — always present) AND on legacy
// seeded DBs in tests (columns missing). Both fts layouts therefore render
// dynamically from a probed column map; m1SyncLegacyFTSSchema repairs the
// mismatch the one way a virtual table allows — full rebuild.

// m1HasM1Cols reports whether the distilled_qa base table already has the
// M1 columns (probed; a missing table reads as false).
func m1HasM1Cols(ctx context.Context, q QueryExecer) bool {
	cols, err := tableColumns(ctx, q, "distilled_qa")
	if err != nil {
		return false
	}
	return cols[m1ExamQuestions]
}

// m1FTSColList renders the fts index column list for the current schema.
func m1FTSColList(ctx context.Context, q QueryExecer) string {
	if m1HasM1Cols(ctx, q) {
		return m1FTSCols
	}
	return "topic, question, answer, sources, search_kw, jieba_token"
}

// m1TriColList renders the trigram index column list for the current schema.
func m1TriColList(ctx context.Context, q QueryExecer) string {
	if m1HasM1Cols(ctx, q) {
		return m1TriCols
	}
	return "question, answer, topic"
}

// ftsIndexPorter is the value that marks an FTS index as built with the
// porter stemmer (English plural/tense folding: `commit` finds `commits`).
// It is an ALTER TABLE argument, hence a bare literal at its one call site —
// named here so the migration's detection and its creation can never spell
// different configs (Rule 1b/13).
const ftsIndexPorter = "tokenize='porter'"

// m1FTSVirtualTable renders the CREATE VIRTUAL TABLE for distilled_qa_fts at
// the column set the CURRENT base table has (legacy six vs M1 twelve). The
// tokenizer is porter, deliberately: unicode61 alone leaves `commits` a
// different token from `commit`, so a query in one number/stem misses the
// other's row entirely (measured: MATCH "commit" = 0 rows against a body
// containing `commits`, and it fuses 这些commit into ONE token).
// porter wraps unicode61's tokenization and only stems the ASCII tokens it
// produces, so CJK tokens are unaffected (measured: MATCH "逐步缩小" = 1, the
// same as unicode61) — the change is strictly additive for CJK.
func m1FTSVirtualTable(ctx context.Context, q QueryExecer) string {
	return "CREATE VIRTUAL TABLE IF NOT EXISTS distilled_qa_fts USING fts5(" +
		m1FTSColList(ctx, q) + ", content='distilled_qa', content_rowid='id', " + ftsIndexPorter + ")"
}

// ftsIsPorter reports whether the distilled_qa_fts index currently in this DB
// was created with the porter tokenizer. The tokenizer is readable from the
// virtual table's own DDL in sqlite_master (the fts5 module stores its
// arguments there), which is the only local evidence that exists: an FTS5
// table created without the option reports no tokenize= at all, i.e. the
// unicode61 default. A missing table reads as false — the CREATE path then
// builds it correctly, and the migration skips.
func ftsIsPorter(ctx context.Context, q QueryExecer) bool {
	var ddl string
	if err := q.QueryRowContext(ctx,
		"SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name=?", ftsSyncFTSName).Scan(&ddl); err != nil {
		return false
	}
	return strings.Contains(ddl, ftsIndexPorter)
}

// m1MigrateFTSTokenizer converts an EXISTING distilled_qa_fts from unicode61
// to porter when, and only when, the index is still the old spelling. It is
// the tokenizer half of the M1 migrate path and is idempotent: a DB already
// on porter returns without touching anything.
//
// The mechanism is FTS5's own ALTER (SQLite 3.46+, present in the modernc.org
// /sqlite build this binary links): `ALTER TABLE ... ADD COLUMN` needs a
// writable connection and cannot run through a query_only handle, which is
// exactly why this function's caller (migrateDefaultTriggers, which already
// holds a write handle) is the right place for it. A failure is returned, not
// swallowed: the index would otherwise stay half-migrated with the caller
// believing the tokenizer changed.
// m1MigrateFTSTokenizer converts an EXISTING distilled_qa_fts from unicode61
// to porter when, and only when, the index is still the old spelling. It is
// the tokenizer half of the M1 migrate path and is idempotent: a DB already
// on porter returns without touching anything.
//
// The mechanism is the rebuild this engine already uses for every other index
// change — DROP + CREATE + 'rebuild' — and not an ALTER, because an FTS5
// virtual table CANNOT be altered at all: measured on the linked modernc
// engine, both `ALTER TABLE fts ADD COLUMN tokenize='porter'` and a plain
// `ADD COLUMN` die with `SQL logic error: virtual tables may not be altered
// (1)`, and the tokenizer is fixed in the CREATE statement that lives in
// sqlite_master. The rebuild also REINDEXES every row, which is exactly what
// the rows need: their stored tokens still carry the old tokenizer's stems.
//
// A failure is returned, never discarded: the caller must not go on believing
// the index changed (Rule 15). The caller's own path (m1MigrateFTSCols, or the
// ensureSchema heal for a foreign DB) recreates the triggers and the index at
// the current layout, so this function only has to drop the table; it does NOT
// recreate it, keeping one owner for the index DDL.
// m1MigrateFTSTokenizer moves the FTS index off the legacy unicode61
// tokenizer onto porter, and it FINISHES that move before it returns.
//
// The tokenizer is fixed in the CREATE statement stored in sqlite_master, and
// an FTS5 virtual table cannot be altered at all (measured: "virtual tables
// may not be altered"). The move is therefore the rebuild the engine already
// uses for every other index change — DROP, then the current CREATE, then
// 'rebuild' — and this function owns the WHOLE of it, not just the DROP: a
// caller that returned early after a bare DROP (the m1MigrateFTSCols column
// early-return shape) would leave the database with NO index at all and every
// existing row unindexed while the triggers made later stores look healthy
// (measured — TestFtsTokenizerMigrationIsIdempotentAndPreservesRows).
//
// Idempotent: an index already on porter returns without touching anything.
func m1MigrateFTSTokenizer(ctx context.Context, db *sql.DB) error {
	if ftsIsPorter(ctx, db) {
		return nil
	}
	exists, err := tableExists(ctx, db, ftsSyncFTSName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	// The base rows hold the data; the index is derived, so dropping and
	// rebuilding it loses nothing — but the rebuild must complete here.
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+ftsSyncFTSName); err != nil {
		return fmt.Errorf("migrate %s to porter: %w", ftsSyncFTSName, err)
	}
	if _, err := db.ExecContext(ctx, m1FTSVirtualTable(ctx, db)); err != nil {
		return fmt.Errorf("migrate %s to porter: %w", ftsSyncFTSName, err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO "+ftsSyncFTSName+"("+ftsSyncFTSName+") VALUES('rebuild')"); err != nil {
		return fmt.Errorf("migrate %s to porter: %w", ftsSyncFTSName, err)
	}
	return nil
}

// m1FTSTriggers renders the three fts keep-in-sync triggers for the column
// set the CURRENT base table has. The INSERT column lists must match the
// virtual table exactly — a trigger written for the wrong set breaks every
// later INSERT with an unknown-column error.
func m1FTSTriggers(ctx context.Context, q QueryExecer) []string {
	cols := m1FTSColList(ctx, q)
	all := strings.Split(cols, ", ")
	oldCol := make([]string, 0, len(all))
	newCol := make([]string, 0, len(all))
	for _, c := range all {
		oldCol = append(oldCol, "old."+c)
		newCol = append(newCol, "new."+c)
	}
	dCol := strings.Join(oldCol, ", ")
	vals := strings.Join(oldCol, ", ")
	valsNew := strings.Join(newCol, ", ")
	return []string{
		`CREATE TRIGGER IF NOT EXISTS distilled_qa_ai AFTER INSERT ON distilled_qa BEGIN
			INSERT INTO distilled_qa_fts(rowid, ` + cols + `)
			VALUES (new.id, ` + valsNew + `);
		END`,
		`CREATE TRIGGER IF NOT EXISTS distilled_qa_au AFTER UPDATE ON distilled_qa BEGIN
			INSERT INTO distilled_qa_fts(distilled_qa_fts, rowid, ` + cols + `)
			VALUES ('delete', old.id, ` + vals + `);
			INSERT INTO distilled_qa_fts(rowid, ` + cols + `)
			VALUES (new.id, ` + valsNew + `);
		END`,
		`CREATE TRIGGER IF NOT EXISTS distilled_qa_ad AFTER DELETE ON distilled_qa BEGIN
			INSERT INTO distilled_qa_fts(distilled_qa_fts, rowid, ` + cols + `)
			VALUES ('delete', old.id, ` + dCol + `);
		END`,
	}
}

// m1TriVirtualTable renders the CREATE VIRTUAL TABLE for the trigram index
// at the column set the CURRENT base table has (question/answer/topic vs
// the M1 four with exam_questions).
func m1TriVirtualTable(ctx context.Context, q QueryExecer) []string {
	cols := m1TriColList(ctx, q)
	return []string{
		"CREATE VIRTUAL TABLE IF NOT EXISTS distilled_qa_tri USING fts5(" +
			cols + ", content='distilled_qa', content_rowid='id', tokenize='trigram')",
	}
}

// m1TriTriggers renders the three trigram keep-in-sync triggers at the
// column set m1TriVirtualTable created (they always run on the same probe,
// so table and triggers can never disagree).
func m1TriTriggers(ctx context.Context, q QueryExecer) []string {
	cols := m1TriColList(ctx, q)
	all := strings.Split(cols, ", ")
	oldCol := make([]string, 0, len(all))
	newCol := make([]string, 0, len(all))
	for _, c := range all {
		oldCol = append(oldCol, "old."+c)
		newCol = append(newCol, "new."+c)
	}
	valsOld := strings.Join(oldCol, ", ")
	valsNew := strings.Join(newCol, ", ")
	return []string{
		`CREATE TRIGGER IF NOT EXISTS distilled_qa_tri_ai AFTER INSERT ON distilled_qa BEGIN
			INSERT INTO distilled_qa_tri(rowid, ` + cols + `)
			VALUES (new.id, ` + valsNew + `);
		END`,
		`CREATE TRIGGER IF NOT EXISTS distilled_qa_tri_au AFTER UPDATE ON distilled_qa BEGIN
			INSERT INTO distilled_qa_tri(distilled_qa_tri, rowid, ` + cols + `)
			VALUES ('delete', old.id, ` + valsOld + `);
			INSERT INTO distilled_qa_tri(rowid, ` + cols + `)
			VALUES (new.id, ` + valsNew + `);
		END`,
		`CREATE TRIGGER IF NOT EXISTS distilled_qa_tri_ad AFTER DELETE ON distilled_qa BEGIN
			INSERT INTO distilled_qa_tri(distilled_qa_tri, rowid, ` + cols + `)
			VALUES ('delete', old.id, ` + valsOld + `);
		END`,
	}
}

// tableColumns returns the actual column names of a table (PRAGMA probing —
// the same dynamic-column pattern as the reference engine's _fetch_extra:
// old schemas may lack columns, and the queries adapt instead of failing).
func tableColumns(ctx context.Context, q QueryExecer, name string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+name+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// tableColumnsInOrder returns a table's (or FTS5 virtual table's) declared
// column names in DECLARED ORDER — the order an INSERT into that table must
// list them in. A virtual table reports its declared columns through the same
// PRAGMA, which is what lets the FTS sync path match a foreign index layout
// exactly.
func tableColumnsInOrder(ctx context.Context, q QueryExecer, name string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+name+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid, notnull, pk int
		var cname, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &cname, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, cname)
	}
	return cols, rows.Err()
}

// initKnowledgeBase creates the DB file (and parent dir) with the full
// schema. Idempotent — safe under concurrent first use.
func initKnowledgeBase(ctx context.Context, path string) error {
	if dir := filepath.Dir(path); dir != "." {
		// scratch.MkdirAllWithSuiteRunOwner, not os.MkdirAll: a ledger path can
		// point inside a per-run suite root, and this call is the one that
		// RECREATES that root when the run's pin has already removed it
		// (measured 2026-10-22, task 01a163ae: the 0755 mkdirat 4.6ms after the
		// pin's rmdir is this line). A recreated root with no owner record can
		// never be judged, so every run of the suite left one behind until the
		// 6h age window. The helper claims ownership only for a suite-run-named
		// directory THIS call creates; every other directory behaves exactly as
		// os.MkdirAll did (see internal/scratch/suite_run_resurrect.go).
		if err := scratch.MkdirAllWithSuiteRunOwner(dir); err != nil {
			return err
		}
	}
	db, err := openDB(path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	// WAL before ANY schema DDL: the 40+ CREATE/ALTER statements that follow
	// must not each fsync under rollback journal (t-1788361069 — see
	// enableWAL). The synchronous= mode is chosen by WHERE this ledger lives
	// (firstOpenSyncMode): NORMAL on anything durable, OFF only on a measured
	// memory filesystem, where the build's commits have nothing to survive.
	// Idempotent on re-init of a healed default DB.
	if err := enableWALWithSync(ctx, db, firstOpenSyncMode(path)); err != nil {
		return err
	}
	if err := ensureSchema(ctx, db); err != nil {
		return err
	}
	if err := ensureFeedbackSchema(ctx, db); err != nil {
		return err
	}
	// Feature 2/5 (自动记忆): the provenance pointer columns (session_id /
	// round_index / source_host). They are part of the SAME first-open heal as
	// the feedback columns, so a fresh DB and a migrated one end up with the
	// same column set.
	if err := ensureOriginSchema(ctx, db); err != nil {
		return err
	}
	return ensureTasksSchema(ctx, db)
}

// migrateDefaultTriggers is invoked on gopi's own shared default DB when a
// newer index/trigger is missing (a DB created before the trigram-table
// polish). It is a no-op on a fully-featured schema. Running ensureSchema
// adds the missing distilled_qa_tri index + its keep-in-sync triggers, then
// backfills the trigram index from existing rows so ≥3-char CJK recall works
// on pre-existing content — not just newly stored rows.
func migrateDefaultTriggers(ctx context.Context, path string) error {
	db, err := openDB(path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	// WAL first (t-1788361069): the heal below runs the same 40+ DDL storm
	// as init; it must not fsync per statement. gopi-owned default DB only.
	if err := enableWAL(ctx, db); err != nil {
		return err
	}
	if err := ensureSchema(ctx, db); err != nil {
		return err
	}
	// Heal the feedback schema too (default-DB migration hook): the event
	// table + materialized columns are added here so a pre-feedback default
	// DB gets them on its existing migration path.
	_ = ensureFeedbackSchema(ctx, db) //stylegate:ignore migration heal on an existing DB; the base schema step above already succeeded
	// Heal the provenance-pointer columns on the same migration path, so a
	// pre-feature default DB gains session_id/round_index/source_host when it
	// is next opened for writing (Rule 15: the heal's own failure is reported
	// by the caller, never swallowed here — the base schema step above is what
	// this path's error contract belongs to).
	if err := ensureOriginSchema(ctx, db); err != nil {
		return fmt.Errorf("memory: migrate provenance columns: %w", err)
	}
	// Heal the tasks ledger too: a pre-tasks default DB gains the table on
	// its existing migration path (same pattern as the feedback heal).
	_ = ensureTasksSchema(ctx, db) //stylegate:ignore same migration-heal pattern as the feedback schema step above
	// Heal the M1 columns (exam_questions + 5W1H): a pre-M1 default DB gains
	// them via ALTER + both FTS indexes rebuilt at the wider column set.
	if err := m1MigrateFTSCols(ctx, db); err != nil {
		_ = err //stylegate:ignore best-effort: the base-table heal above already succeeded// best-effort: base-table heal above already succeeded
	}
	hadTri, err := tableExists(ctx, db, "distilled_qa_tri")
	if err != nil || !hadTri {
		// Table was just created: backfill it from the base rows (rowid-aligned
		// external-content table). Failing rows (e.g. a foreign schema quirk)
		// are skipped — data integrity of the base table is paramount.
		_ = rebuildTrigramIndex(ctx, db) //stylegate:ignore optional FTS rebuild of a freshly created table; base rows stay intact
	}
	return nil
}

// rebuildTrigramIndex fills the distilled_qa_tri external-content index from
// the distilled_qa base rows (rowid-aligned) by issuing an FTS 'rebuild'.
// Returns an error only if the index itself is unusable.
func rebuildTrigramIndex(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO distilled_qa_tri(distilled_qa_tri) VALUES('rebuild')`); err != nil {
		return err
	}
	return nil
}

// m1MigrateFTSCols is the M1 heal hook for the shared default DB: when the
// existing distilled_qa lacks the exam_questions / 5W1H columns (a pre-M1
// DB), it ALTERs them in (only added, never dropped), then rebuilds BOTH FTS
// indexes so their column sets pick up the new columns (ALTER on an FTS5
// table is impossible — the external-content tables are dropped, recreated
// at the current 12/4-column layout and refilled from the base rows by the
// FTS 'rebuild' command). The keep-in-sync triggers are recreated at the
// current layout by the ensureSchema call the migration path already runs.
// Best-effort on failure: the base columns exist, so only the index columns
// stay legacy until the next successful heal.
func m1MigrateFTSCols(ctx context.Context, db *sql.DB) error {
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return err
	}
	// The tokenizer heal runs FIRST and independently of the column check: a
	// DB that already has the six M1 columns is otherwise "fully migrated" and
	// would return early with its FTS index still on unicode61 (no porter, no
	// stem folding). The heal completes its own DROP + CREATE + 'rebuild'
	// before returning, so the early return below cannot leave the index
	// half-moved. Both heals are idempotent, so ordering them this way costs
	// one sqlite_master read on a good DB.
	if err := m1MigrateFTSTokenizer(ctx, db); err != nil {
		return err
	}
	if cols[m1ExamQuestions] && cols[m1KwCols[4]] {
		return nil // columns fully migrated (all six M1 columns present)
	}
	for _, c := range append([]string{m1ExamQuestions}, m1KwCols[:]...) {
		if cols[c] {
			continue
		}
		if _, err := db.ExecContext(ctx,
			"ALTER TABLE distilled_qa ADD COLUMN "+c+" TEXT DEFAULT ''"); err != nil {
			return err
		}
	}
	// Stale triggers at the legacy 6/3-column layout would break the next
	// INSERT (unknown column in the trigger body) — drop them before the
	// index rebuild; ensureSchema recreates them at the current layout.
	for _, t := range []string{"distilled_qa_ai", "distilled_qa_au", "distilled_qa_ad",
		"distilled_qa_tri_ai", "distilled_qa_tri_au", "distilled_qa_tri_ad"} {
		_, _ = db.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+t) //stylegate:ignore auxiliary DDL/PRAGMA/FTS statement; the primary statement's result is what is checked
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS distilled_qa_fts"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS distilled_qa_tri"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, m1FTSVirtualTable(ctx, db)); err != nil {
		return err
	}
	for _, t := range m1TriVirtualTable(ctx, db) {
		if _, err := db.ExecContext(ctx, t); err != nil {
			return err
		}
	}
	// The two index DDLs above are unconditional re-creations, so this is the
	// one place the triggers are (re)created — and it runs AFTER the
	// tokenizer decide, which is what makes the fresh triggers match the
	// index layout in both heal shapes.
	for _, t := range append(m1FTSTriggers(ctx, db), m1TriTriggers(ctx, db)...) {
		if _, err := db.ExecContext(ctx, t); err != nil {
			return err
		}
	}
	if err := rebuildTrigramIndex(ctx, db); err != nil {
		return err
	}
	// The FTS index was dropped and recreated at the wider column set above,
	// so it MUST be refilled here: an un-rebuilt index would leave every
	// existing row unindexed while the triggers make later stores look
	// healthy. The failure is returned (never discarded) — the caller's store
	// path then fails loudly via verifyRowIndexed rather than reporting a
	// successful store into an index that cannot see the row.
	if _, err := db.ExecContext(ctx, "INSERT INTO distilled_qa_fts(distilled_qa_fts) VALUES('rebuild')"); err != nil {
		return err
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// dbInitLocks serializes first-open schema initialization per DB path
// (t-1788361069 regression guard). A fresh DB's init is no longer a
// minutes-long rollback-journal fsync storm (WAL at open time fixed that),
// but it is still a multi-statement CREATE sequence — a concurrent opener
// on the SAME fresh path must wait for the initializer instead of reading
// a half-created schema (WAL readers no longer block on the writer, so
// the mid-init transient state became observable). initKnowledgeBase is
// idempotent (every CREATE is IF NOT EXISTS), so the second opener simply
// re-runs it under the lock and gets a complete DB.
var dbInitLocks sync.Map // path → *sync.Mutex

// schemaBatchProbeHook, when non-nil, runs inside ensureSchema's publication
// transaction — after the base table is created and before the index/trigger
// statements, i.e. exactly the window the first-open flake lived in. It
// exists solely so a regression test can observe the DB while the schema is
// in flight; the production path never sets it. Same shape as the other test
// seams (syncSSHExchangeHook, reconRemotePendingSetHook).
var schemaBatchProbeHook func()

// initKnowledgeBaseOnce runs initKnowledgeBase under a per-path lock so two
// goroutines racing the same fresh file serialize instead of one rejecting
// the other's half-built schema.
func initKnowledgeBaseOnce(ctx context.Context, path string) error {
	mu, _ := dbInitLocks.LoadOrStore(path, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	l.Lock()
	defer l.Unlock()
	// Re-check under the lock: the winner may have just created the file,
	// making our init a redundant no-op (still cheap and idempotent).
	return initKnowledgeBase(ctx, path)
}

// ensureKnowledgeBase is the single first-open gate: it initializes a
// missing DB (or heals a schema-less default) under the per-path lock and
// returns the fresh DB opened with the caller's access mode. Callers that
// found a file EXISTS but lacks the distilled_qa table use this instead of
// racing the initializer.
func ensureKnowledgeBase(ctx context.Context, path string, readOnly bool) (*sql.DB, error) {
	if err := initKnowledgeBaseOnce(ctx, path); err != nil {
		return nil, err
	}
	return openKnowledgeBaseDB(path, readOnly)
}

// knowledgeBaseReadOnlyOpenErrPrefix names the ONE read-only open error of a
// memory knowledge base, so the refusal a caller reports says which shape was
// asked for (Rule 1: no bare literal at a call site).
const knowledgeBaseReadOnlyOpenErrPrefix = "knowledge base read-only open"

// openKnowledgeBaseDB is the ONE opener for a memory knowledge base, and the
// readOnly arm is read-only AT THE DSN, not at the pragma.
//
// WHY THE WRITABLE ARM IS A SEPARATE FUNCTION RATHER THAN A FLAG on openDB: the
// two arms do not open the same kind of connection and cannot share one line.
// The writable arm is openDB(path, false) — LedgerDSN, `_txlock=immediate`,
// PRAGMA writes, and (in its callers) schema DDL. The read-only arm has NONE of
// those: mode=ro in the DSN, `_pragma=query_only(true)`, one connection, and no
// statement that a reader is not entitled to run.
//
// THE DEFECT THIS REMOVES (measured 2026-10-30, task 01a1892d). memory_search's
// "read-only" entry opened with openDB(path, true) and left a COPY of a real
// ledger rewritten: memory.db moved and memory.db-wal was GONE (checkpointed
// away at close). Its own header described the flag as "query_only — search
// never writes", which is true about SQL statements and irrelevant to what
// actually wrote the file: the open, its PRAGMA, and the close.
//
// NO SILENT FALLBACK (Rule 15): a read-only arm that cannot open says so. It
// never returns the writable connection, because that connection is exactly the
// thing it was asked to avoid.
func openKnowledgeBaseDB(path string, readOnly bool) (*sql.DB, error) {
	if !readOnly {
		return openDB(path, false)
	}
	return openKnowledgeBaseReadOnly(path)
}

// openKnowledgeBaseReadOnly is the read-only arm itself: LedgerReadOnlyDSN
// (mode=ro + busy_timeout + query_only) over a SINGLE connection, and nothing
// else. The caller is responsible for not running DDL on it — schema work is a
// WRITE and must hop to openKnowledgeBaseDB(path, false) explicitly, which is
// what the default-DB heal branch in openSearchDB does, out loud, through
// migrateDefaultTriggers.
//
// SetMaxOpenConns(1) is not decoration: under mode=ro every connection opens its
// own WAL snapshot, so a multi-connection pool can answer two reads from two
// different states of the file.
func openKnowledgeBaseReadOnly(path string) (*sql.DB, error) {
	if !fileExists(path) {
		// The writable arm would CREATE the file here. A read-only request must
		// not, and must not silently degrade to the shape that writes.
		return nil, fmt.Errorf("%s: %s does not exist (a read-only caller cannot create it)",
			knowledgeBaseReadOnlyOpenErrPrefix, path)
	}
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", knowledgeBaseReadOnlyOpenErrPrefix, path, err)
	}
	db.SetMaxOpenConns(1) // SQLite serializes; mirror openDB's single connection
	if _, err := db.Exec(ledgerReadQueryOnlyPragma); err != nil {
		db.Close() //stylegate:ignore handle closed after its setup failed; the caller gets the error and no handle
		return nil, fmt.Errorf("%s %s: %w", knowledgeBaseReadOnlyOpenErrPrefix, path, err)
	}
	return db, nil
}

// waitForDistilledSchema polls a freshly-created file until the concurrent
// initializer's distilled_qa table appears (t-1788361069 race guard). Two
// goroutines opening the same brand-new non-default path can otherwise race:
// opener A holds the init lock creating the schema while opener B — which
// saw the file appear mid-init — reads a half-built DB and would reject it
// as foreign. The init completes in well under a second with WAL (the old
// rollback-journal path took seconds and masked this under write-lock
// blocking); a foreign DB never acquires the table, so the bounded poll
// falls through to the caller's rejection.
func waitForDistilledSchema(ctx context.Context, db *sql.DB) bool {
	for i := 0; i < dbSchemaWaitTries; i++ {
		ok, err := tableExists(ctx, db, "distilled_qa")
		if err == nil && ok {
			return true
		}
		time.Sleep(dbSchemaWaitInterval)
	}
	return false
}

// hasNoSchemaObjects reports whether the opened file carries ZERO schema
// objects — a fact read from the file, not an inference from elapsed time.
// It is the discriminator between the two shapes an "uninitialized" file can
// have, and their remedies are opposite:
//
//   - TRUE — nobody's data is in it. An empty/0-byte file, or one another
//     process is about to fill (the first open's schema batch is one atomic
//     transaction, so a concurrent initializer is never observable as a
//     half-built schema — it is observable as NOTHING AT ALL), or a file
//     whose bytes were truncated. There is no state to protect, so the
//     opener may JOIN the initialization instead of refusing.
//   - FALSE (the query answered, and the count is non-zero) — a database
//     with somebody's tables in it. That is a foreign DB (hermes'
//     distilled_knowledge.db, state.db, …): refuse exactly as before, because
//     creating our tables inside it is the thing the refusal protects.
//
// A probe ERROR (garbage bytes: "file is not a database") is not "no
// objects" — it is a refusal in its own right, so it reports FALSE and the
// caller refuses.
func hasNoSchemaObjects(ctx context.Context, db *sql.DB) bool {
	var n int
	if err := db.QueryRowContext(ctx, sqliteMasterObjectCountSQL).Scan(&n); err != nil {
		return false
	}
	return n == 0
}

// adoptUninitializedKnowledgeBase is the authoritative answer to the race the
// bounded poll only guesses at (t-mem-firstopen-refusal — the four-concurrent-
// stores refusal).
//
// WHY A POLL CANNOT BE THE GATE, and why this is not "a longer timeout": the
// first open is a SEQUENCE of write transactions (the atomic schema batch,
// then the feedback table and its ALTERs, then the tasks tables and their FTS
// companions). On a block-backed ledger every commit is a device sync; the
// MEASURED build duration is median ~131ms, p90 ~185ms, max 837ms
// (firstopen_probe_test.go) — i.e. the winner's init can legitimately outlast
// ANY short poll budget, and when it does, every real waiter is refused with
// "not a distilled knowledge base" while the file is merely not-yet-built.
// The waist is that the loser's recovery is a wall-clock GUESS about another
// process's progress. This helper replaces the guess with a fact: when the
// file provably holds no schema objects, its own initializer is the only
// candidate owner, so the caller JOINS that initialization (ensureKnowledgeBase:
// the per-path lock plus the idempotent build) and comes back with a complete
// DB. Contention for a shared fresh DB thereby costs latency, never a
// spurious refusal — which is the contract TestMemorySearchNonBlocking pins.
//
// It is therefore checked BEFORE the poll, not after it: for the shape it
// recognizes (an empty file) waiting is pure latency — the answer is already
// known — and the poll survives only for the shapes this probe cannot
// recognize (a file with objects but no distilled_qa).
//
// The bound is the build's own: the initializer either finds the schema
// already published by the peer (a no-op) or waits on the same write lock it
// would have to wait for anyway (busy_timeout + the batch's own bounded
// lock-retry loop). No second winner is created — SQLite serializes the two
// builds on the file, and every statement is IF NOT EXISTS.
func adoptUninitializedKnowledgeBase(ctx context.Context, path string, readOnly bool, db *sql.DB) (*sql.DB, error) {
	if !hasNoSchemaObjects(ctx, db) {
		return nil, nil // somebody's database: not ours to initialize
	}
	db.Close() //stylegate:ignore probe handle closed before its replacement is opened
	return ensureKnowledgeBase(ctx, path, readOnly)
}

// openSearchDB opens the knowledge base read-only for a search session.
// First use auto-creates the schema (init 内嵌 — no manual init step). A
// custom path (GOPI_MEMORY_DB) that exists but is not a distilled knowledge
// base is rejected, never mutated — that is what makes the hermes production
// DB read-only-compatible. gopi's own shared default DB is healed instead:
// a schema-less leftover file there is re-initialized.
//
// READ-ONLY MEANS THE DSN (measured 2026-10-30, task 01a1892d). This function
// used to open with openDB(path, true) — "query_only = ON" — and that is a
// promise about SQL STATEMENTS, not about the file: the open still took the
// immediate transaction lock, its first statement was a PRAGMA WRITE, and close
// checkpointed the WAL away. Measured on a device-backed copy of a real ledger:
// one search rewrote memory.db (7c08da3c… → 4bbb8467…) and LEFT NO -wal.
//
// The shape now is: every read-only handle comes from
// openKnowledgeBaseDB(path, true) → LedgerReadOnlyDSN (mode=ro +
// query_only(true)) + SetMaxOpenConns(1), and it runs no DDL. The one branch
// that genuinely has to write — the default DB's trigram/M1 heal — hops to the
// WRITABLE arm explicitly, through migrateDefaultTriggers, and then RE-OPENS
// read-only. Nothing here falls back to a writable handle silently (Rule 15).
func openSearchDB(ctx context.Context, path string) (*sql.DB, error) {
	if !fileExists(path) {
		return ensureKnowledgeBase(ctx, path, true)
	}
	db, err := openKnowledgeBaseDB(path, true)
	if err != nil {
		return nil, err
	}
	ok, err := tableExists(ctx, db, "distilled_qa")
	if err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, err
	}
	if ok {
		// gopi's own default DB may predate the trigram index or the M1
		// columns (exam_questions + 5W1H). Migrate in place so CJK recall
		// AND the M1 parallel columns work on existing content.
		if isDefaultMemoryDB(path) {
			hasTri, _ := tableExists(ctx, db, memoryTriIndexTable) //stylegate:ignore existence probe; an error is read as 'absent' and the caller heals the table
			hasM1 := m1HasM1Cols(ctx, db)
			if !hasTri || !hasM1 {
				// THE HEAL IS A WRITE, so it leaves the read-only handle
				// deliberately: migrateDefaultTriggers opens the writable arm,
				// runs WAL + ensureSchema + the feedback/origin/tasks heals and
				// closes, and the caller then comes back to the read-only shape.
				db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
				if err := migrateDefaultTriggers(ctx, path); err != nil {
					return nil, fmt.Errorf("memory_search: migrate default index: %w", err)
				}
				return openKnowledgeBaseDB(path, true)
			}
			return db, nil
		}
		// A NON-default ledger is NOT rewritten here: unlike the default DB
		// there is no migrateDefaultTriggers hop, because the pre-fix code did
		// not do one either and this fix changes the OPEN SHAPE, not the policy
		// about whose ledger may be altered behind its owner's back.
		return db, nil
	}
	// Is this file merely NOT BUILT YET, or is it somebody else's database?
	// The file itself answers: zero schema objects = nobody's data, and the
	// first open's schema batch is atomic, so a concurrent initializer is
	// observable as an EMPTY file, never as a half-built schema. Join that
	// initialization instead of refusing a DB that is only not-yet-built
	// (t-mem-firstopen-refusal).
	if db2, err := adoptUninitializedKnowledgeBase(ctx, path, true, db); err != nil || db2 != nil {
		return db2, err
	}
	// The probe did not recognize the file (it carries objects): it may still
	// be a concurrent first-open mid-CREATE (WAL readers no longer block on
	// the writer). Give it a brief window before declaring the file foreign.
	if waitForDistilledSchema(ctx, db) {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return openSearchDB(ctx, path)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	if isDefaultMemoryDB(path) {
		return ensureKnowledgeBase(ctx, path, true)
	}
	return nil, fmt.Errorf("memory_search: %s is not a distilled knowledge base (missing distilled_qa table)", path)
}

// openStoreDB opens the knowledge base read-write for a store session,
// creating the schema on first use. An existing file that is not a distilled
// knowledge base is rejected — never create tables inside a foreign SQLite
// DB (state.db and friends).
func openStoreDB(ctx context.Context, path string) (*sql.DB, error) {
	if !fileExists(path) {
		return ensureKnowledgeBase(ctx, path, false)
	}
	db, err := openDB(path, false)
	if err != nil {
		return nil, err
	}
	ok, err := tableExists(ctx, db, "distilled_qa")
	if err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, err
	}
	if ok {
		// gopi's own default DB may predate the trigram index or the M1
		// columns (exam_questions + 5W1H). Migrate in place so CJK recall
		// AND the M1 parallel columns work on existing content. The
		// provenance-pointer columns (session_id/round_index/source_host,
		// memory_origin.go) are healed on the same path for the same reason.
		if isDefaultMemoryDB(path) {
			hasTri, _ := tableExists(ctx, db, "distilled_qa_tri") //stylegate:ignore existence probe; an error is read as 'absent' and the caller heals the table
			hasM1 := m1HasM1Cols(ctx, db)
			hasOrigin := originHasCols(ctx, db)
			if !hasTri || !hasM1 || !hasOrigin {
				db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
				if err := migrateDefaultTriggers(ctx, path); err != nil {
					return nil, fmt.Errorf("memory_store: migrate default index: %w", err)
				}
				return openDB(path, false)
			}
		} else {
			// A NON-default ledger (a task's pinned --db, a peer's DB) is not
			// rewritten behind its owner's back, but the columns this process
			// is about to write into it are added when missing: without them
			// every store here would silently drop the pointer (Rule 15).
			if !originHasCols(ctx, db) {
				if err := ensureOriginSchema(ctx, db); err != nil {
					db.Close() //stylegate:ignore handle closed after its last caller finished
					return nil, fmt.Errorf("memory_store: add provenance columns: %w", err)
				}
			}
		}
		return db, nil
	}
	// Same fact-not-guess recovery as openSearchDB: an empty file with no
	// distilled_qa is a first open in flight, not a foreign DB.
	if db2, err := adoptUninitializedKnowledgeBase(ctx, path, false, db); err != nil || db2 != nil {
		return db2, err
	}
	// Unrecognized by the probe (it carries objects): it may still be a
	// concurrent first-open mid-CREATE (WAL readers no longer block on the
	// writer). Give it a brief window before declaring the file foreign.
	if waitForDistilledSchema(ctx, db) {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return openStoreDB(ctx, path)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	if isDefaultMemoryDB(path) {
		return ensureKnowledgeBase(ctx, path, false)
	}
	return nil, fmt.Errorf("memory_store: %s is not a distilled knowledge base (missing distilled_qa table)", path)
}

func isDefaultMemoryDB(path string) bool {
	return path == defaultMemoryDBPath()
}

// ---------------------------------------------------------------------------
// search.
// ---------------------------------------------------------------------------

// MemorySearchHits is the STRUCTURED retrieval exit of the memory engine:
// the same multi-route recall + value-signal rescore MemorySearch runs, but
// returning []MemoryHit instead of the rendered text. The hippocampus core
// loop needs the hits themselves (ids for the write verbs, Tier/Confidence
// for the decision step) — parsing the rendered text back into structure
// would be fragile and would violate the single-source-of-truth rule.
// MemorySearch is the rendering wrapper around this function; both share
// every recall route so they can never drift apart.
func MemorySearchHits(query string, limit int, intent string) ([]MemoryHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("memory_search: query must not be empty")
	}
	if limit <= 0 {
		limit = memoryDefaultLimit
	}
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return nil, fmt.Errorf("memory_search: %w", err)
	}
	ftsOK, err := tableExists(ctx, db, "distilled_qa_fts")
	if err != nil {
		return nil, fmt.Errorf("memory_search: %w", err)
	}
	if !ftsOK {
		return nil, fmt.Errorf("memory_search: knowledge base has no FTS index (distilled_qa_fts missing)")
	}

	// Multi-route recall (feature 1): the FTS5 exact-token route ("distilled_qa_fts")
	// covers ASCII tokens / multi-word queries; the trigram route covers CJK
	// ≥3-char substrings; a LIKE/title-scan covers 2-char CJK that trigram's
	// lower bound misses. Results are unioned (deduped by id) and ranked by
	// the FTS rank first, then by confidence.
	hitsByID := map[int]*MemoryHit{}
	var order []int

	// Route 1: exact-token FTS5 (existing behavior). A query with no
	// searchable term at all (quotes/punctuation only) SKIPS the route — the
	// remaining routes plus the empty result are the structured answer;
	// running MATCH "" would be the FTS5 syntax error this fix exists to
	// remove (see escapeQuery).
	//
	// A CJK-bearing query gets a SECOND MATCH at the index column's own
	// granularity (route 1b, immediately below): escapeQuery's bare-CJK idiom
	// can only match a token equal to a whole run, which is why a natural
	// phrasing almost never reached jieba_token through this route.
	now := float64(time.Now().Unix())
	if match, ok := escapeQuery(query); ok {
		rows, err := db.QueryContext(ctx, searchSelect(cols), match)
		if err != nil {
			return nil, fmt.Errorf("memory_search: %w", err)
		}
		for rows.Next() {
			h, perr := scanSearchHit(rows, cols, now, intent)
			if perr != nil {
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
				return nil, fmt.Errorf("memory_search: %w", perr)
			}
			if _, seen := hitsByID[h.ID]; !seen {
				hitsByID[h.ID] = &h
				order = append(order, h.ID)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("memory_search: %w", err)
		}
		rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	}

	// Route 1b: segmented exact-token FTS5 on jieba_token — the query side of
	// the index column's granularity. The index stores the SEGMENTER's words
	// (cjkSegmentation), so the query must ask for the SEGMENTER's words too;
	// escapeQuery above asks for whole runs, which only matches a row whose
	// indexed text happens to contain that run as a single word.
	//
	// The columns are restricted to jieba_token because the other indexed
	// columns hold unsegmentable PROSE: a segmented single-rune word (的, 用)
	// is a legitimate alias inside the compact token column and pure noise in
	// a prose column. The all-words AND is deliberately exact: the recall
	// ladder below owns the degrade-one-word-at-a-time middle, and a second
	// OR-style route here would flood the pool with incidental rows.
	if terms := cjkQuerySegments(query); len(terms) > 0 {
		if match := ftsAndTerms(terms); match != "" && match != segMatchOf(query) {
			rows, err := db.QueryContext(ctx, searchSegSelect(cols), match)
			if err == nil {
				for rows.Next() {
					h, perr := scanSearchHit(rows, cols, now, intent)
					if perr != nil {
						rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
						return nil, fmt.Errorf("memory_search: %w", perr)
					}
					if _, seen := hitsByID[h.ID]; !seen {
						hitsByID[h.ID] = &h
						order = append(order, h.ID)
					}
				}
				rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			}
		}
	}

	// Route 1.5: the recall ladder (the missing middle). Route 1's exact-token
	// AND is the top rung: all tokens must appear in one row. The old code
	// dropped straight to OR, whose pool is every row sharing ANY single
	// token — so a high-DF token (a filler word, a generic noun) floods the
	// capped pool with incidental rows and the query's real subject never
	// enters it (measured: `"deepseek" 降低` matches 0 rows while 427 rows
	// match 怎么 alone). The ladder inserts the middle rung: the biggest
	// AND-subset that still qualifies, ranked so the terms dropped are the
	// ones whose removal costs the least specificity (see
	// memsearchSubsetPick). Its candidates are added FIRST, so a capped pool
	// fills with the subset's evidence; the OR route below still runs
	// UNCHANGED, so no recall is traded away — the ladder adds precision
	// ahead of breadth, it never replaces the fallback.
	// subsetIDs holds the rows the chosen subset matched — the ladder's own
	// evidence, taken from the query result rather than re-derived from the
	// hit's text (see the scoring note). The FTS index is the authority on
	// which rows satisfy the subset: a gram MATCH legitimately fires on rows
	// whose text never exposes the token (jieba_token indexes 2-char grams the
	// unicode61 columns do not), so a lexical re-check could disagree with the
	// very count the ladder selected on.
	subsetIDs := map[int]bool{}
	if exactMiss := len(order) == 0; exactMiss {
		if def, ok := memsearchSubsetPick(ctx, db, query); ok {
			subsetRows, err := db.QueryContext(ctx,
				`SELECT d.id, d.topic, d.question, d.answer`+searchExtraSelect(cols)+m1ExamSelect(cols)+
					`, fts.rank FROM distilled_qa_fts fts JOIN distilled_qa d ON d.id = fts.rowid
					 WHERE distilled_qa_fts MATCH `+"?"+` ORDER BY fts.rank LIMIT ?`,
				def.match, trigramRecallK)
			if err == nil {
				for subsetRows.Next() {
					h, perr := scanSearchHit(subsetRows, cols, now, intent)
					if perr != nil {
						subsetRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
						return nil, fmt.Errorf("memory_search: %w", perr)
					}
					subsetIDs[h.ID] = true
					if _, seen := hitsByID[h.ID]; !seen {
						hitsByID[h.ID] = &h
						order = append(order, h.ID)
					}
				}
				subsetRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
			}
		}
		// ASCII partial-recall fallback, unchanged: the tokens OR-ed so ANY
		// one shared token recalls a candidate (the ASCII analogue of the CJK
		// trigram/LIKE partial routes).
		if asciiOR := escapeQueryOR(query); asciiOR != "" {
			orRows, err := db.QueryContext(ctx,
				`SELECT d.id, d.topic, d.question, d.answer`+searchExtraSelect(cols)+m1ExamSelect(cols)+
					`, fts.rank FROM distilled_qa_fts fts JOIN distilled_qa d ON d.id = fts.rowid
					 WHERE distilled_qa_fts MATCH `+"?"+` ORDER BY fts.rank LIMIT ?`,
				asciiOR, trigramRecallK)
			if err == nil {
				for orRows.Next() {
					h, perr := scanSearchHit(orRows, cols, now, intent)
					if perr != nil {
						orRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
						return nil, fmt.Errorf("memory_search: %w", perr)
					}
					if _, seen := hitsByID[h.ID]; !seen {
						hitsByID[h.ID] = &h
						order = append(order, h.ID)
					}
				}
				orRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
			}
		}
	}

	// Route 2: trigram CJK recall — ≥3-char contiguous CJK runs from the query
	// are OR-ed into a trigram MATCH. Exact-token FTS5 sees a whole CJK run as
	// one token, so a query like "VPN 网络测试环境" wouldn't match "网络" separately;
	// trigram splits the run into overlapping 3-char substrings.
	//stylegate:ignore existence probe; an error is read as 'absent' and the caller heals the table
	if triOK, _ := tableExists(ctx, db, "distilled_qa_tri"); triOK {
		segments := capRouteTerms(cjkRunsGE3(query))
		if len(segments) > 0 {
			matchExpr := make([]string, 0, len(segments))
			for _, s := range segments {
				matchExpr = append(matchExpr, `"`+s+`"`)
			}
			triRows, err := db.QueryContext(ctx,
				`SELECT d.id, d.topic, d.question, d.answer`+searchExtraSelect(cols)+m1ExamSelect(cols)+
					`, t.rank FROM distilled_qa_tri t JOIN distilled_qa d ON d.id = t.rowid
					 WHERE distilled_qa_tri MATCH `+"?"+` ORDER BY t.rank LIMIT ?`,
				strings.Join(matchExpr, " OR "), trigramRecallK)
			if err == nil {
				for triRows.Next() {
					h, perr := scanSearchHit(triRows, cols, now, intent)
					if perr != nil {
						triRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
						return nil, fmt.Errorf("memory_search: %w", perr)
					}
					if _, seen := hitsByID[h.ID]; !seen {
						hitsByID[h.ID] = &h
						order = append(order, h.ID)
					}
				}
				triRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
			}
		}
	}

	// Route 3: LIKE substring scan on the query's SEGMENTED CJK words. It
	// catches the shape the token index structurally cannot carry: a query
	// word that the segmenter does not know, or one that lives INSIDE a
	// longer indexed word rather than on a word boundary. The terms come from
	// cjkQuerySegments — a real segmentation, not the 2-char gram list this
	// route used to walk — so one query field costs one LIKE term instead of
	// one per character pair (the measured 7,999-term blow-up that
	// memsearchMaxCJKRouteTerms caps had a 2000-word ASCII query as its
	// driver; a segmented term list is bounded by the WORD count).
	if terms := cjkQuerySegments(query); len(terms) > 0 {
		likeSQL, qargs := likeCJKQuery(terms, searchExtraSelect(cols)+m1ExamSelect(cols), likeCJKRecallK)
		likeRows, err := db.QueryContext(ctx, likeSQL, qargs...)
		if err == nil {
			for likeRows.Next() {
				h, perr := scanSearchHit(likeRows, cols, now, intent)
				if perr != nil {
					likeRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
					return nil, fmt.Errorf("memory_search: %w", perr)
				}
				if _, seen := hitsByID[h.ID]; !seen {
					hitsByID[h.ID] = &h
					order = append(order, h.ID)
				}
			}
			likeRows.Close() //stylegate:ignore read-only cursor whose iteration already ended
		}
	}

	// Value-signal ranking (round-3): the recall routes union candidates in
	// BM25 order, but BM25 over a six-column index is a poor sole arbiter — a
	// row that merely repeats rare tokens outranks one that answers more of
	// the query (the "deepseek v4 flash price adjustment" example). Every
	// candidate is rescored: query-token coverage (how many distinct
	// meaningful query tokens appear in topic+question+answer) is the primary
	// key, then tier (hot boosts, cold demotes), then the possibly_stale
	// penalty, then confidence; BM25 rank is the tiebreak. Cold items are
	// excluded from tier-1 results and only surface when no warm/hot row
	// matches (the last-resort bucket) — "cold" is the maintenance demotion
	// for low-quality rows (memory_update --tier cold) without deleting them.
	scores := make(map[int]float64, len(order))
	for _, id := range order {
		h := hitsByID[id]
		h.Tokens = queryTokenCoverage(query, h)
		// M2: confidence enters the rescore through its effective (decayed,
		// re-verified) value — an unverified 2024 lesson no longer shouts as
		// loud as yesterday's. Stored confidence keeps feeding the JSON.
		s := float64(h.Tokens)*tokenCoverageWeight + h.EffConfidence - 0.5
		switch h.Tier {
		case hotTier:
			s += tierWeightHot
		case coldTier:
			s += tierWeightCold
		}
		// Feedback term (P0): the Beta posterior vote signal, inserted after
		// the tier weight and before the stale penalty per the merged design.
		// Deliberately small (max |term| = 0.5 ≈ a quarter of one token
		// coverage) and gated by the effective sample count — feedback
		// supplements the token-coverage signal, never replaces it.
		if h.FbUseful+h.FbUseless > 0 {
			s += fbScore(h.FbUseful, h.FbUseless)
		}
		if h.PossiblyStale {
			s -= stalePenalty
		}
		scores[id] = s
	}
	cmp := func(a, b int) int {
		// The ladder's own matches come first: they are the rows that satisfy
		// the chosen AND-subset, i.e. the strongest evidence the query
		// produced, while the OR pool below them contains rows sharing a
		// single (often high-DF) token. This is a SORT key, not a score
		// adjustment — the rendered tokens= signal keeps its exact meaning
		// (how many query tokens the row's own text covers), which the real
		// corpus shows can legitimately be 0 for a row the ladder matched
		// through the gram index.
		if subsetIDs[a] != subsetIDs[b] {
			if subsetIDs[a] {
				return -1
			}
			return 1
		}
		if scores[a] != scores[b] {
			if scores[a] > scores[b] {
				return -1
			}
			return 1
		}
		if hitsByID[a].rank != hitsByID[b].rank {
			if hitsByID[a].rank < hitsByID[b].rank {
				return -1
			}
			return 1 // BM25: more negative = better
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	}
	var warmHot, cold []int
	for _, id := range order {
		if hitsByID[id].Tier == coldTier {
			cold = append(cold, id)
		} else {
			warmHot = append(warmHot, id)
		}
	}
	slices.SortFunc(warmHot, cmp)
	slices.SortFunc(cold, cmp)
	// Tier-1 results = warm/hot only. Cold is the last-resort bucket: it only
	// surfaces when NO warm/hot row matches at all — the "no longer 1st tier
	// to return" contract for maintenance-demoted rows.
	ordered := warmHot
	if len(ordered) == 0 {
		ordered = cold
	}
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	hits := make([]MemoryHit, 0, len(ordered))
	for _, id := range ordered {
		if h, ok := hitsByID[id]; ok {
			hits = append(hits, *h)
		}
	}

	return hits, nil
}

// MemorySearch runs the memory_search tool: FTS5 MATCH against the embedded
// distilled-knowledge store. limit caps the returned records (default
// memoryDefaultLimit); intent is an optional routing hint counted into each
// hit's intent_hits signal. Returns the rendered hits or the no-distillation
// hint. Errors are engine-level failures only (unopenable DB, foreign file,
// malformed schema) — never a panic.
func MemorySearch(query string, limit int, intent string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("memory_search: query must not be empty")
	}
	if limit <= 0 {
		limit = memoryDefaultLimit
	}
	hits, err := MemorySearchHits(query, limit, intent)
	if err != nil {
		return "", err
	}
	// Fix 1 (report §6.2 #4): re-verification signal. Each search bumping the
	// last_retrieved/retrieval_count counters is what makes possibly_stale's
	// "re-verification" half of the AND-condition live (otherwise it
	// degenerates to pure when_at aging and the tool description over-promises
	// a freshness signal nobody writes). The update uses a short separate
	// read-write connection (the search DB itself is read-only), is gated on
	// the counters actually existing in the schema, and tolerates any failure
	// (read-only filesystem, locked DB, foreign read-only copy) — the search
	// result is never degraded by a best-effort counter bump.
	ids := make([]int, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	bumpRetrievalCounts(context.Background(), memoryDBPath(), ids)

	out, err := json.Marshal(hits)
	if err != nil {
		return "", fmt.Errorf("memory_search: %w", err)
	}
	rendered, err := renderMemorySearchResult(string(out))
	if err != nil {
		return "", fmt.Errorf("memory_search: %w", err)
	}
	// Procedural layer merged in (GLM five-layer model, epic M5): workflow
	// rows from the procedural table ride the same search result so one
	// call surfaces both declarative lessons and reusable workflows. A QA
	// empty-hint plus procedural hits is NOT an empty result — the merge
	// lives HERE in the engine so every caller (LLM tool, CLI) sees it.
	// Failure or empty on this side never degrades the QA result.
	if proc, perr := ProceduralSearch(query, limit); perr == nil && proc != "" &&
		!strings.HasPrefix(proc, "No procedural workflow found") {
		if rendered == memoryEmptyHint {
			rendered = "[procedural workflows]\n" + proc
		} else {
			rendered += "\n\n[procedural workflows]\n" + proc
		}
	}
	// The SESSION half (memory_session_hits.go): the same query also searches
	// the round-level note index, so one call answers "has a previous RUN been
	// here?" as well as "has a lesson been distilled about it?".
	//
	// THE TWO HALVES ARE ORDERED SEPARATELY AND RENDERED AS TWO SECTIONS, never
	// merged into one ranking: a distilled row is ranked by the engine's value
	// signals (tier/confidence/feedback/staleness) and a note hit by bm25 over
	// raw note text — one ordering over both would compare a confidence to a
	// bm25 score. The distilled answer keeps its place first (it is this tool's
	// original product, byte-identical to the pre-feature render inside its
	// section), and the session half is best-effort: a failure there is rendered
	// into its own section and never degrades the distilled result.
	return MergeMemorySearchSections(rendered, memorySessionSection(query, limit)), nil
}

// bumpRetrievalCounts is the re-verification write (report §6.2 #4): it
// bumps last_retrieved/retrieval_count for the just-returned hit rows so the
// possibly_stale signal can observe "was re-verified recently". Best-effort:
// it opens a short read-write connection separate from the read-only search
// session, does nothing when the counters are absent, and swallows any error
// (foreign/read-only DB, lock contention) so a failed bump never degrades the
// returned search result. Shared-DB concurrency is safe under WAL (each gopi
// instance queues on busy_timeout for the brief write).
//
// IT IS THE ONLY WRITER ON THE SEARCH PATH (measured 2026-10-30, task
// 01a1892d). With the read-only DSN fix in place, one `gopi memory get` over a
// device-backed copy of the real ledger left memory.db and -wal byte-identical,
// while one `gopi memory search` still moved both: MemorySearchHits' own open is
// now provably read-only, and this call is the deliberate write that follows it.
// It cannot be made read-only — the whole point of the re-verification signal is
// that the counters CHANGE — so it must stay here, visible, rather than riding
// invisibly on the search connection (which is how the defect was hidden).
func bumpRetrievalCounts(ctx context.Context, path string, ids []int) {
	if len(ids) == 0 {
		return
	}
	db, err := openDB(path, false)
	if err != nil {
		return
	}
	defer db.Close()
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil || !cols["retrieval_count"] || !cols["last_retrieved"] {
		return // old/foreign schema without the counters — nothing to bump.
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, float64(time.Now().Unix()))
	for _, id := range ids {
		args = append(args, id)
	}
	_, _ = db.ExecContext(ctx,
		"UPDATE distilled_qa SET retrieval_count=COALESCE(retrieval_count,0)+1, last_retrieved=? WHERE id IN ("+placeholders+")", args...) //stylegate:ignore auxiliary DDL/PRAGMA/FTS statement; the primary statement's result is what is checked
}

// searchSelect builds the hit query with only the value-signal columns the
// actual schema has (PRAGMA probing). tier/confidence/lesson_type/when_at/
// sources/last_retrieved are optional; missing columns read as defaults.
// The FTS5 external-content index only exposes its indexed columns, so the
// base row is joined back for tier/confidence.
func searchSelect(cols map[string]bool) string {
	return `SELECT d.id, d.topic, d.question, d.answer` + searchExtraSelect(cols) +
		m1ExamSelect(cols) +
		`, fts.rank FROM distilled_qa_fts fts JOIN distilled_qa d ON d.id = fts.rowid` +
		` WHERE distilled_qa_fts MATCH ? ORDER BY fts.rank`
}

// searchSegSelect renders route 1b's SELECT: the same projection as
// searchSelect, but joined against the fts index WITHOUT the MATCH — the
// caller's MATCH expression is handed in as the bound parameter. Two
// spellings of one projection, kept adjacent so a column added to the search
// projection cannot silently miss the segmented route.
func searchSegSelect(cols map[string]bool) string {
	return `SELECT d.id, d.topic, d.question, d.answer` + searchExtraSelect(cols) +
		m1ExamSelect(cols) +
		`, fts.rank FROM distilled_qa_fts fts JOIN distilled_qa d ON d.id = fts.rowid` +
		` WHERE distilled_qa_fts MATCH ? ORDER BY fts.rank`
}

// searchExtraSelect renders the optional value-signal columns (tier/
// confidence/lesson_type/when_at/sources/last_retrieved + the feedback
// materialized counts useful_count/useless_count) that the actual schema
// has — shared by the FTS5 and trigram route SELECTs.
func searchExtraSelect(cols map[string]bool) string {
	take := []string{}
	for _, c := range []string{"tier", "confidence", "lesson_type", "when_at", "sources", "last_retrieved", "useful_count", "useless_count", "distilled_at", "retrieval_count"} {
		if cols[c] {
			take = append(take, "d."+c)
		}
	}
	// The provenance pointer (memory_origin.go) rides the SAME projection, so a
	// column added there reaches every search route at once — and scanHit reads
	// it in this exact position.
	for _, c := range originColumns {
		if cols[c] {
			take = append(take, "d."+c)
		}
	}
	if len(take) == 0 {
		return ""
	}
	return ", " + strings.Join(take, ", ")
}

// m1ExamSelect renders the M1 scoring-text columns (exam_questions + the
// five 5W1H label columns) for the schemas that have them — the text feeds
// queryTokenCoverage (an exam phrasing or label matching the query raises
// the hit's coverage score). Rendered right after searchExtraSelect so the
// scan order in scanHit matches: value columns, then exam/kw, then rank.
func m1ExamSelect(cols map[string]bool) string {
	cols2 := []string{m1ExamQuestions}
	cols2 = append(cols2, m1KwCols[:]...)
	take := []string{}
	for _, c := range cols2 {
		if cols[c] {
			take = append(take, "d."+c)
		}
	}
	if len(take) == 0 {
		return ""
	}
	return ", " + strings.Join(take, ", ")
}

// ---------------------------------------------------------------------------
// search route helpers (feature 1: multi-route CJK recall).
// ---------------------------------------------------------------------------

// cjkRunsGE3 extracts all contiguous CJK runs of length ≥3 from s — the
// substrings the trigram FTS tokenizer indexes. Returns only runs of length
// at least trigramMinToken (3 chars), deduped.
// cjkRunsGE3 extracts the query's contiguous CJK runs of at least
// trigramMinToken runes — the terms the trigram MATCH route OR-s together.
func cjkRunsGE3(s string) []string {
	run := []rune{}
	out := []string{}
	seen := map[string]bool{}
	appendRun := func() {
		if len(run) >= trigramMinToken {
			seg := string(run)
			if !seen[seg] {
				seen[seg] = true
				out = append(out, seg)
			}
		}
		run = nil
	}
	for _, r := range s {
		if isCJK(r) {
			run = append(run, r)
		} else {
			appendRun()
		}
	}
	appendRun()
	return out
}

// splitCJKRunsExtract (removed) — the coverage tokenizer uses cjkQuerySegments

// cjkGrams2 extracts 2-char CJK grams from s (the CJK query fragments below
// the trigram tokenizer's lower bound). A contiguous CJK run of length ≥2
// yields its overlapping 2-char grams ("网络测试环境" → "网络","络测","测试","试环",
// "环境"); each is a candidate for the LIKE recall route. Runs shorter than 2
// are ignored.
func cjkGrams2(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	appendGrams := func(run []rune) {
		if len(run) < 2 {
			return
		}
		for i := 0; i+1 < len(run); i++ {
			g := string(run[i : i+2])
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	run := []rune{}
	for _, r := range s {
		if isCJK(r) {
			run = append(run, r)
		} else {
			appendGrams(run)
			run = nil
		}
	}
	appendGrams(run)
	return out
}

// cjkQuerySegments is the query-side twin of cjkSegmentation: it segments a
// user query with the SAME segmenter the index column was written with, so
// route 1's MATCH can demand the very tokens the index stores. It returns
// nil when the segmenter finds nothing (an empty query, or one with no
// segmentable content) and nil on a segmenter failure, which makes the route
// SKIP rather than ask FTS5 for tokens nothing was indexed under.
//
// One granularity, both directions: if this function's tokeniser ever
// diverges from cjkSegmentation's, a stored row becomes un-findable by the
// query that produced it — which is why both are one call into
// internal/cjkseg and neither keeps its own rules.
func cjkQuerySegments(s string) []string {
	words, err := cjkseg.Words(s)
	if err != nil {
		return nil
	}
	return capRouteTerms(words)
}

// capRouteTerms bounds a CJK route's term list, keeping the query's head.
func capRouteTerms(terms []string) []string {
	if len(terms) > memsearchMaxCJKRouteTerms {
		return terms[:memsearchMaxCJKRouteTerms]
	}
	return terms
}

// cjkSegmentation produces the value written into the jieba_token index
// column: the WORDS a real Chinese segmenter finds in s, joined by
// cjkSegmentSep (a space). The segmenter is cjkseg.Words — gse over its
// embedded dictionary — the SAME call the query side makes
// (cjkQuerySegments), which is what keeps index and query at one
// granularity.
//
// Why the column exists at all: FTS5's unicode61 tokenizer runs a
// contiguous CJK run into a SINGLE token, so a row containing 网络测试环境
// is unreachable by 测试 on its own. Writing the segmenter's words into this
// column makes each word an addressable token, so a later query hits the
// record even when it never contains the byte-for-byte phrase.
//
// It replaced a 2-char sliding-window gram list (every overlapping bigram of
// every CJK run, comma-joined). That was a dictionary-free stand-in for
// segmentation: it could not tell 网络测试环境's real words (网络 + 测试环境)
// from its arbitrary bigrams (络测/试环), and it could not peel a Latin word
// off a glued run (这些commit). The measured before/after recall counts are
// in devdoc/jieba-token-real-segmentation.md.
//
// A segmenter failure returns the empty string rather than a partial token
// list: the caller (deriveJiebaToken → the INSERT/UPDATE path) must fail
// loudly on a truly empty required column, and silently writing a
// lower-fidelity token set would reintroduce exactly the defect this
// function exists to remove (Rule 15).
func cjkSegmentation(s string) string {
	words, err := cjkseg.Words(s)
	if err != nil {
		return ""
	}
	return strings.Join(words, cjkSegmentSep)
}

// isCJK reports whether r is a CJK Unified Ideograph (U+4E00–U+9FFF; the
// reference's "一-鿿" class, which matches the CJK range the reference engine
// uses for CJK detection).
func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// memoryEscapeRun matches one literal JSON unicode escape: a backslash, a 'u'
// and memoryEscapeHexDigits hex digits (the form encoding/json emits, in lower
// case — a payload copied through Python's json.dumps carries the same shape).
var memoryEscapeRun = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)

// decodeLiteralEscapes repairs a field whose text is the LITERAL escape form of
// real characters ("\u6210\u672c\u5206\u6790" ⇒ "成本分析"). The JSON-encoded
// bytes reached the column instead of the value they encode — a producer that
// serialized a payload and stored the serialization (python json.dumps without
// ensure_ascii=False; any producer that double-encodes), i.e. a defect fixed
// here because this is the one funnel every producer passes.
//
// CONSERVATIVE BY CONSTRUCTION — text that is not a payload is returned
// byte-identical:
//   - real CJK anywhere in the field means it is PROSE (an answer that
//     discusses "\u003c" HTML escaping, e.g. row 8), never a payload: skipped;
//   - the escape runs must consume at least memoryEscapeCoveragePercent of the
//     value's non-space runes. A field that merely mentions one escape inside a
//     sentence cannot qualify; a fully escaped payload consumes nearly all of
//     it. Threshold derivation (measured on ~/.gopi/memory.db, 2026-09-23):
//     escaped payload cells 0.74–1.00, prose-mention cells 0.00–0.13.
//
// Callers are WRITE funnels only (memoryStoreRow, MemoryUpdate). Reads are
// never rewritten; the historical rows are repaired at the source by the
// repair path, not at every query.
func decodeLiteralEscapes(s string) string {
	if !strings.Contains(s, memoryEscapeRunPrefix) {
		return s
	}
	// Prose guard: a field that already carries real CJK is a discussion of the
	// escape form (or real content that quotes it), never an encoded payload.
	for _, r := range s {
		if isCJK(r) {
			return s
		}
	}
	runs := memoryEscapeRun.FindAllStringSubmatchIndex(s, -1)
	if len(runs) < memoryEscapeMinHexRuns {
		return s
	}
	nonSpace := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			nonSpace++
		}
	}
	if nonSpace < memoryEscapeMinNonSpaceRunes || len(s) < memoryEscapeMinRunes {
		return s
	}
	consumed := len(runs) * len(memoryEscapeRunPrefix+strings.Repeat("0", memoryEscapeHexDigits))
	if consumed*memoryEscapeCoverageRatio < nonSpace {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, m := range runs {
		b.WriteString(s[last:m[0]])
		v, err := strconv.ParseUint(s[m[2]:m[3]], memoryEscapeHexBase, memoryEscapeHexDigits*4)
		if err != nil {
			b.WriteString(s[m[0]:m[1]])
			last = m[1]
			continue
		}
		r := rune(v)
		if unicode.IsControl(r) {
			// A decoded C0/C1 control has no place in a text column (and the
			// garbage gate / FTS would mishandle a NUL) — substitute it so the
			// rest of the payload still decodes.
			r = memoryEscapeReplacement
		}
		b.WriteRune(r)
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// likeCJKAnswerCol is the base table's prose column the substring route used
// to skip. Named once (Rule 1b/14) because it is also the column whose absence
// from likeCJKTargets was a measured blind spot: a query fragment living ONLY
// in an answer was unreachable unless the token index happened to carry it.
const likeCJKAnswerCol = "answer"

// likeCJKNoRelevanceExpr is SQLite's constant tiebreak for a route-3 query
// whose router produced no term worth ranking by (see
// likeCJKRelevanceExpr): every row scores the same and the ordering falls back
// to confidence, which is the pre-relevance-key behaviour.
const likeCJKNoRelevanceExpr = "0"

// likeCJKTargets are the columns the segmented CJK LIKE route scans: the
// legacy question/topic pair plus the M1 exam_questions and what/why label
// columns, and — since the substring route is now the ONLY recovery for a
// phrasing the segmenter does not produce as a word — the answer body. The
// labels are included because short CJK queries most often name the what/why
// of a record, and the answer is included because the row's own prose is where
// a user's phrasing frequently lands (measured: "环境" recalling nothing when
// it exists only in the answer).
var likeCJKTargets = []string{"question", "topic", m1ExamQuestions, "kw_what", "kw_why", likeCJKAnswerCol}

// likeCJKClauses renders N "(d.col LIKE ? OR ...)" clauses — one ? per
// likeCJKTargets column per term (the param order matches appendCJKLikes).
func likeCJKClauses(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		likes := make([]string, 0, len(likeCJKTargets))
		for _, c := range likeCJKTargets {
			likes = append(likes, "d."+c+" LIKE ?")
		}
		out = append(out, "("+strings.Join(likes, " OR ")+")")
	}
	return out
}

// appendCJKLikes builds the %term% parameters for likeCJKClauses (one per
// likeCJKTargets column per term).
func appendCJKLikes(terms []string, n int) []interface{} {
	params := make([]interface{}, 0, n*len(likeCJKTargets))
	for _, t := range terms {
		for range likeCJKTargets {
			params = append(params, "%"+t+"%")
		}
	}
	return params
}

// likeCJKRankMinTermRunes is the shortest term that carries ranking weight in
// the substring route's ORDER BY. The segmenter emits 1-rune CJK function
// words (里, 是, 的) as legitimate tokens — they must stay in the WHERE (they
// are real recall surface) but they are noise as a RELEVANCE signal: a row
// matched by 的 alone is not more relevant than one matched by 数据库.
const likeCJKRankMinTermRunes = 2

// likeCJKQuery renders route 3's SQL and its bound parameters together, so the
// TEXT and the ARGUMENT ORDER cannot drift apart — SQLite numbers parameters in
// TEXT order: the WHERE clause's LIKE terms bind before the ranking key in the
// ORDER BY, and the LIMIT binds last. Rendering one builder for both keeps a
// caller from re-deriving that order (the route and its regression test read
// this one).
func likeCJKQuery(terms []string, selectExtra string, limit int) (string, []interface{}) {
	key := likeCJKRelevanceExpr(terms)
	args := appendCJKLikes(terms, len(terms))
	args = append(args, likeCJKRelevanceArgs(terms)...)
	args = append(args, limit)
	sql := `SELECT d.id, d.topic, d.question, d.answer` + selectExtra +
		`, NULL AS rank FROM distilled_qa d WHERE ` +
		strings.Join(likeCJKClauses(len(terms)), " OR ") +
		` ORDER BY ` + key + ` DESC, d.confidence DESC LIMIT ?`
	return sql, args
}

// likeCJKRelevanceTerms returns the route-3 terms that carry ranking weight,
// paired with that weight. The weight is the term's RUNE length — a longer CJK
// term is a more specific match, which is the specificity signal a LIKE scan
// can compute with no index support (the corpus DF is not available to an
// expression the way FTS5's bm25 is to a MATCH). Terms below
// likeCJKRankMinTermRunes are dropped: they keep recalling rows through the
// WHERE, they just do not rank them.
func likeCJKRelevanceTerms(terms []string) []cjkRankTerm {
	out := make([]cjkRankTerm, 0, len(terms))
	for _, t := range terms {
		if n := utf8.RuneCountInString(t); n >= likeCJKRankMinTermRunes {
			out = append(out, cjkRankTerm{term: t, weight: n})
		}
	}
	return out
}

// cjkRankTerm is one weighted term of the substring route's relevance key.
type cjkRankTerm struct {
	term   string
	weight int
}

// likeCJKRelevanceExpr renders route 3's PRIMARY ordering key: the
// query-relevance score, summed over the weighted terms. Each term contributes
// its weight once per likeCJKTargets column that contains it, so the key
// rewards both answering MORE of the query and answering it out of a MORE
// SPECIFIC term. Confidence follows as a TIEBREAK only — it is a property of
// the row, not of the query, so on its own (the pre-fix key) it could not
// distinguish two rows that matched the query from two that did not, and a
// corpus whose confidences tie returned a constant low-rowid set for every
// natural-language query (measured: natural 0/120 vs control 40/40).
//
// With no weighted term the expression degrades to a constant, which leaves
// the ORDER BY on confidence alone — the pre-fix ordering, but only for a
// query the router produced nothing rankable for.
func likeCJKRelevanceExpr(terms []string) string {
	rank := likeCJKRelevanceTerms(terms)
	if len(rank) == 0 {
		return likeCJKNoRelevanceExpr
	}
	parts := make([]string, 0, len(rank))
	for _, rt := range rank {
		hits := make([]string, 0, len(likeCJKTargets))
		for _, c := range likeCJKTargets {
			// Each predicate is PARENTHESIZED: comparison binds looser than
			// `+` in SQLite, so an unparenthesized list of `instr(..) > 0`
			// joined with ` + ` parses as a chain of comparisons — a constant
			// 0/1, i.e. no ranking key at all (measured: it silently restored
			// the pre-fix ordering).
			hits = append(hits, "(instr(COALESCE(d."+c+",''), ?) > 0)")
		}
		parts = append(parts, fmt.Sprintf("(%s) * %d", strings.Join(hits, " + "), rt.weight))
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

// likeCJKRelevanceArgs are the bound terms of likeCJKRelevanceExpr, one bare
// term per likeCJKTargets column per weighted term (instr, not LIKE — the
// parameter is the term itself). SQLite numbers parameters in TEXT order, so
// the caller binds them where the expression appears — after the WHERE
// clause's LIKE terms when the key is only in the ORDER BY.
func likeCJKRelevanceArgs(terms []string) []interface{} {
	rank := likeCJKRelevanceTerms(terms)
	params := make([]interface{}, 0, len(rank)*len(likeCJKTargets))
	for _, rt := range rank {
		for range likeCJKTargets {
			params = append(params, rt.term)
		}
	}
	return params
}

// rowScanner is the minimal Scan surface shared by *sql.Rows and *sql.Row —
// lets the hit scanner serve both the multi-row routes and single-row lookups.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanSearchHit scans one base-record row into a MemoryHit, filling the
// value-signal columns that exist in the actual schema (cols probing) and
// computing the derived signals. The scan order must match the SELECT column
// order produced by searchSelect/searchExtraSelect (base four always first,
// then tier/confidence/lesson_type/when_at/sources/last_retrieved in that
// order when each column exists, then the trailing rank column).
func scanSearchHit(rows *sql.Rows, cols map[string]bool, now float64, intent string) (MemoryHit, error) {
	return scanHit(rows, cols, now, intent)
}

func scanHit(sc rowScanner, cols map[string]bool, now float64, intent string) (MemoryHit, error) {
	var h MemoryHit
	scans := []interface{}{&h.ID, &h.Topic, &h.Question, &h.Answer}
	var tier, lessonType, whenAt, sources sql.NullString
	var confidence, lastRetrieved, fbUseful, fbUseless, rank sql.NullFloat64
	var distilledAt sql.NullFloat64
	var retrievalCount sql.NullFloat64
	if cols["tier"] {
		scans = append(scans, &tier)
	}
	if cols["confidence"] {
		scans = append(scans, &confidence)
	}
	if cols["lesson_type"] {
		scans = append(scans, &lessonType)
	}
	if cols["when_at"] {
		scans = append(scans, &whenAt)
	}
	if cols["sources"] {
		scans = append(scans, &sources)
	}
	if cols["last_retrieved"] {
		scans = append(scans, &lastRetrieved)
	}
	if cols["useful_count"] {
		scans = append(scans, &fbUseful)
	}
	if cols["useless_count"] {
		scans = append(scans, &fbUseless)
	}
	// M2 lifecycle columns (scan order matches searchExtraSelect's take
	// list: distilled_at, retrieval_count — appended at the end, before the
	// M1 scoring text).
	if cols["distilled_at"] {
		scans = append(scans, &distilledAt)
	}
	if cols["retrieval_count"] {
		scans = append(scans, &retrievalCount)
	}
	// The provenance pointer (memory_origin.go) — read in the position
	// searchExtraSelect appends it, i.e. after the M2 lifecycle columns and
	// before the M1 scoring text.
	var originSession, originHost sql.NullString
	var originRound sql.NullInt64
	if cols[originSessionCol] {
		scans = append(scans, &originSession)
	}
	if cols[originRoundCol] {
		scans = append(scans, &originRound)
	}
	if cols[originHostCol] {
		scans = append(scans, &originHost)
	}
	// M1 scoring text (exam_questions + 5W1H labels), scanned in the same
	// order m1ExamSelect renders them (const order, then the kw_ list).
	var examQ, kw1, kw2, kw3, kw4, kw5 sql.NullString
	if cols[m1ExamQuestions] {
		scans = append(scans, &examQ)
	}
	for i, c := range m1KwCols {
		if !cols[c] {
			break
		}
		switch i {
		case 0:
			scans = append(scans, &kw1)
		case 1:
			scans = append(scans, &kw2)
		case 2:
			scans = append(scans, &kw3)
		case 3:
			scans = append(scans, &kw4)
		case 4:
			scans = append(scans, &kw5)
		}
	}
	// The trailing rank column (fts.rank / t.rank / NULL AS rank) is the
	// BM25 tiebreak for the value-signal sort; NULL scans as 0 (LIKE route).
	scans = append(scans, &rank)
	if err := sc.Scan(scans...); err != nil {
		return h, err
	}
	h.rank = rank.Float64
	h.Tier = tier.String
	if h.Tier == "" {
		h.Tier = defaultTier
	}
	h.Confidence = confidence.Float64
	if h.Confidence < confDecayFloor {
		h.Confidence = confDecayFloor
	}
	// M2: the effective confidence (lazy decay + re-verify reward) feeds both
	// the rescore and the rendered meta — one computation, two consumers.
	h.retrievalCount = retrievalCount.Float64
	h.EffConfidence = m2EffectiveConfidence(h.Confidence, distilledAt, lastRetrieved,
		cols["last_retrieved"], h.retrievalCount, now)
	h.HotStale = h.Tier == hotTier &&
		hotRecordStale(nullableFloatPtr(lastRetrieved), cols["last_retrieved"], whenAt.String, now)
	h.LessonType = lessonType.String
	h.WhenAt = whenAt.String
	// distilled_at is the write instant (absent on a legacy schema): it is the
	// stored= anchor the render uses, and when_at stays exactly the staleness
	// marker possiblyStale/hotRecordStale read — the two are never swapped.
	h.DistilledAt = distilledAt.Float64
	h.Sources = splitSources(sources.String)
	h.PossiblyStale = possiblyStale(whenAt.String, nullableFloatPtr(lastRetrieved), now)
	h.IntentHits = intentHits(intent, h.Question, h.Answer)
	h.FbUseful = fbUseful.Float64
	h.FbUseless = fbUseless.Float64
	// M1: collect the scoring text. The exam column and the kw labels keep
	// their const order; a missing column (legacy schema) just contributes
	// nothing. Labels are comma-joined so queryTokenCoverage's substring
	// match sees every label.
	sb := examQ.String
	for _, k := range []sql.NullString{kw1, kw2, kw3, kw4, kw5} {
		if k.String != "" {
			sb += ", " + k.String
		}
	}
	h.examQuestions = examQ.String
	h.kwLabels = sb
	// The provenance pointer: absent columns (legacy/foreign schema) and empty
	// values both read as the zero MemoryOrigin, so the JSON field is omitted
	// and the renderer prints no origin segment.
	h.Origin = originPointerOf(MemoryOrigin{
		SessionID: strings.TrimSpace(originSession.String),
		Round:     originRoundInt(originRound),
		Host:      strings.TrimSpace(originHost.String),
	})
	return h, nil
}

// originRoundInt reads round_index, which is absent as a column on a legacy
// schema and NULL on a row that never set it — both are
// sessionevent.RoundIndexUnknown ("no round recorded"), never a fabricated
// round.
func originRoundInt(v sql.NullInt64) int {
	if !v.Valid {
		return sessionevent.RoundIndexUnknown
	}
	return int(v.Int64)
}

// escapeQuery builds the route-1 (exact-token AND) FTS5 MATCH expression. It
// returns ok=false for a query that carries NO searchable term (empty, or
// nothing but quotes/punctuation) — the caller must then SKIP the route
// instead of running MATCH "": an empty MATCH expression is an FTS5 SYNTAX
// ERROR, so skipping is what makes "no terms" a clean 0-hit rather than a
// failure (Rule 15: no silent failure — a hostile-input query gets a
// structured result, never an engine error).
//
// SAFETY CONTRACT (memsearch-query-safety): every term emitted here is either
// a QUOTED FTS5 phrase (ASCII) or a BARE contiguous CJK run. Nothing else is
// legal in this expression, because anything else is interpreted by FTS5's
// query grammar rather than matched literally — and the query text is
// LLM/user-controlled, i.e. a hostile input by default.
//
//   - A MATCH term containing '-' is a COLUMN QUALIFIER: `token-free` asks for
//     column 'free' and the statement dies with `SQL logic error: no such
//     column: free` (measured directly on fts5, and through the CLI on any
//     term the OLD code left raw — a CJK-bearing field, e.g. `网络-测试` →
//     `no such column: 测试`, `VPN-网络测试环境` → `no such column:
//     网络测试环境`). Quoting neutralizes it: FTS5 parses `"token-free"` as the
//     phrase token+free and matches it (13 rows on the production store; the
//     pre-fix code already quoted pure-ASCII fields, so this route's recall for
//     them is unchanged — the boundary set in the test file pins that).
//   - quoting is also what neutralizes AND/OR/NOT/NEAR/*/(), ':' column
//     filters and any stray operator the caller smuggles in; '*' inside a
//     phrase is a literal, so no ability is lost (`memory*` → 1245 rows).
//   - A double quote is folded to a SPACE before the split, never deleted: `"`
//     is not a word rune, so the index stores `a"b` as a and b — deleting it
//     would spell the phrase `"ab"` and demand a token that is never stored; a
//     space splits in the same place and keeps an unterminated quote out of the
//     grammar (both halves pinned by TestFTSBoundarySplitMatchesUnicode61).
//   - CJK is deliberately left bare (the pre-existing idiom, unchanged): the
//     unicode61 tokenizer runs a contiguous CJK run into ONE token, so quoting
//     ("网络" "测试") would demand two standalone stored tokens that never
//     exist and the query returns nothing even though 网络测试环境 is present.
//     The trigram (≥3-char) and 2-char LIKE routes own substring recall; the
//     known gap — a query fragment SHORTER than the stored run cannot match
//     the bare term — is filed, not fixed here (see the devdoc).
func escapeQuery(q string) (string, bool) {
	terms := memsearchMatchTerms(q)
	if len(terms) == 0 {
		return "", false
	}
	return strings.Join(terms, " "), true
}

// jiebaTokenCol is the ONE FTS-indexed column carrying the segmenter's words.
// Naming it once (Rule 1b/14) is what lets the segmented route restrict its
// MATCH to the token column without a second, drifting spelling of the name.
const jiebaTokenCol = "jieba_token"

// ftsAndTerms renders a token list as an FTS5 AND expression in the token
// column, each term quoted exactly the way memsearchMatchTerms quotes
// (ftsQuote), so a segmenter word containing '-', '*' or a quote can never
// reach FTS5's query grammar as an operator. Returns "" for an empty list —
// callers SKIP the route rather than run MATCH "" (the FTS5 syntax error
// escapeQuery exists to avoid).
func ftsAndTerms(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		if t == "" {
			continue
		}
		quoted = append(quoted, jiebaTokenCol+":"+ftsQuote(t))
	}
	return strings.Join(quoted, " ")
}

// segMatchOf reports the MATCH expression route 1's escapeQuery would build
// for this query, so route 1b can skip a query it would repeat verbatim
// (escapeQuery's bare-CJK rendering and the segmented one are identical for a
// query that is already a single run of dictionary words).
func segMatchOf(q string) string {
	match, ok := escapeQuery(q)
	if !ok {
		return ""
	}
	return match
}

// memsearchMatchTerms is the ONE place a user query becomes FTS5 MATCH terms
// (escapeQuery's terms; the ladder re-derives its own quoted singles so a
// subset can be counted in isolation). It returns a list of `"phrase"` /
// bare-CJK atoms to be joined with AND (space) or OR.
//
// ASCII fields are split where the INDEX splits them — on every rune
// unicode61 does not keep inside a token (isFTSWordRune) — and each piece is
// quoted separately, because FTS5 parses `"token-free"` as the phrase
// token+free anyway; `"no-"` alone is the degenerate case (a trailing
// separator) and the split spelling is the one that keeps '-' from ever
// reaching the grammar. A CJK field is split into its maximal CJK runs and
// the ASCII/gap runs between them, the CJK runs going in bare (see
// escapeQuery) and the ASCII runs through the same quoting.
//
// The term list is capped at memsearchMaxMatchTerms so a pathological query
// (thousands of tokens) cannot build an unbounded statement or an unbounded
// scan; the drop is deterministic (the query's head wins) and is the
// "declared-safe degradation" of this route — a capped query returns the
// hits its first terms match, never an error. Normal queries are far below
// the cap.
func memsearchMatchTerms(q string) []string {
	out := make([]string, 0, memsearchMaxMatchTerms)
	seen := make(map[string]bool, memsearchMaxMatchTerms)
	// add appends one term's MATCH spelling; false = the cap is reached.
	add := func(term string) bool {
		if term == "" {
			return true
		}
		if len(out) >= memsearchMaxMatchTerms {
			return false
		}
		key := strings.ToLower(term)
		if seen[key] {
			return true
		}
		seen[key] = true
		out = append(out, ftsQuote(term))
		return true
	}
	for _, field := range strings.Fields(q) {
		if !addFieldTerms(field, add) {
			break
		}
	}
	return out
}

// addFieldTerms feeds one whitespace field's terms into add, returning false
// as soon as the cap is reached (the caller stops the whole walk — a partial
// field must not be spliced onto a truncated list).
func addFieldTerms(field string, add func(string) bool) bool {
	// A double quote is folded to a SPACE, never deleted: `"` is not an FTS5
	// word rune, so the index stores `a"b` as the two tokens a and b — deleting
	// the quote would spell the phrase `"ab"` and demand a token the index never
	// stores (a silent recall loss, caught by
	// TestFTSBoundarySplitMatchesUnicode61). A space splits the field in the
	// same place and keeps an unterminated quote out of the grammar (the
	// `unterminated string` failure, measured). A field that is nothing but
	// quotes therefore yields nothing — the "no searchable term" answer.
	field = strings.TrimSpace(strings.ReplaceAll(field, `"`, " "))
	if field == "" {
		return true
	}
	for _, part := range splitCJKRuns(field) {
		if hasCJK(part) {
			// A contiguous CJK run stays ONE bare term — the pre-existing
			// idiom, and what the 2-char grams / trigram routes are built
			// around (quoting it would demand standalone stored tokens).
			if !add(part) {
				return false
			}
			continue
		}
		for _, piece := range splitFTSWords(part) {
			if !add(piece) {
				return false
			}
		}
	}
	return true
}

// splitCJKRuns splits s into its maximal contiguous CJK runs and the
// remaining (ASCII / separator) runs between them, in order. Every part is
// either all-CJK or contains no CJK at all, so hasCJK classifies it exactly.
func splitCJKRuns(s string) []string {
	out := []string{}
	run := []rune{}
	cjk := false
	flush := func() {
		if len(run) > 0 {
			out = append(out, string(run))
			run = nil
		}
	}
	for _, r := range s {
		if isCJK(r) != cjk {
			flush()
			cjk = isCJK(r)
		}
		run = append(run, r)
	}
	flush()
	return out
}

// splitFTSWords splits an ASCII region into the pieces FTS5 itself would
// tokenize it into — the separator being every rune isFTSWordRune rejects.
func splitFTSWords(s string) []string {
	out := []string{}
	run := []rune{}
	for _, r := range s {
		if !isFTSWordRune(r) {
			if len(run) > 0 {
				out = append(out, string(run))
				run = nil
			}
			continue
		}
		run = append(run, r)
	}
	if len(run) > 0 {
		out = append(out, string(run))
	}
	return out
}

// isFTSWordRune reports whether r survives FTS5's unicode61 tokenizer inside
// a token. Letters and digits do; every other character separates tokens.
// The ASCII split above must agree with this predicate — splitting where the
// index does NOT split would demand two query terms the stored text carries
// as a single token, i.e. a query that can never match.
func isFTSWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// hasCJK reports whether the string contains any CJK Unified Ideograph —
// used by escapeQuery to leave CJK tokens unquoted (see escapeQuery).
func hasCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

// asciiTokenMinLen is the minimum rune length for an ASCII token to qualify
// for the partial-recall OR route (shorter fragments match too many unrelated
// rows and add noise — same spirit as the reference MIN_QUERY_TERM_LEN=2).
const asciiTokenMinLen = 2

// escapeQueryOR builds an FTS5 "any token matches" MATCH expression from the
// ASCII tokens of q — the partial-recall fallback for exact-token misses.
// Continuous CJK subtokens are served by the trigram/LIKE routes instead, so
// this helper only ORs ASCII tokens (≥ asciiTokenMinLen runes). Returns an
// empty string when there are fewer than two such tokens (an exact AND on one
// term is not worth downgrading — a single-token query either matches or not,
// and OR/AND on one token are identical). The tokens are individually quoted
// to neutralize FTS operators, same as escapeQuery.
func escapeQueryOR(q string) string {
	fields := strings.Fields(q)
	seen := map[string]bool{}
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(strings.ReplaceAll(f, `"`, ""))
		if f == "" || hasCJK(f) {
			continue
		}
		if len([]rune(f)) < asciiTokenMinLen {
			continue
		}
		key := strings.ToLower(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		tokens = append(tokens, `"`+f+`"`)
	}
	if len(tokens) < 2 {
		return ""
	}
	return strings.Join(tokens, " OR ")
}

// hotRecordStale reports whether a hot record has aged past hotStaleDays
// without re-verification — the machine-readable "no permanent hot" signal
// (the hint renders; demotion stays manual). last_retrieved defaults to 0
// (= never retrieved), so a hot record that was never re-verified goes
// stale on the clock immediately: hot is a temporary boost, not a tenure.
// Undatable input (NULL / column missing) → not flagged (宁缺勿滥; the
// never-re-verified aspect stays possiblyStale's job).
func hotRecordStale(lastRetrieved *float64, lastRetrievedPresent bool, whenAt string, now float64) bool {
	hotStaleSeconds := float64(hotStaleDays * 24 * 3600)
	if lastRetrievedPresent && lastRetrieved != nil && *lastRetrieved > 0 {
		return now-*lastRetrieved > hotStaleSeconds
	}
	// Never re-verified (NULL/missing/0): age the hot tenure from the
	// distilled time instead — a hot record that was never needed again
	// goes stale on the clock.
	anchor := parseTimeMarker(whenAt)
	if anchor == nil {
		return false // undatable → not flagged (宁缺勿滥)
	}
	return now-*anchor > hotStaleSeconds
}

// possiblyStale is the value signal for "knowledge that aged AND was never
// re-verified": unretrieved for staleAfterDays (last_retrieved absent or too
// old) AND when_at older than staleAfterDays. No when_at (undatable) or
// recent retrieval → not stale (宁缺勿滥, same as the reference).
func possiblyStale(whenAt string, lastRetrieved *float64, now float64) bool {
	if lastRetrieved != nil && now-*lastRetrieved <= staleSeconds {
		return false
	}
	when := parseTimeMarker(whenAt)
	if when == nil {
		return false
	}
	return now-*when > staleSeconds
}

// parseTimeMarker parses a when_at value: numeric Unix seconds or one of the
// date formats the reference accepts ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d",
// "%Y/%m/%d"; only the first 19 chars matter). Returns nil when unparseable
// — never an error.
func parseTimeMarker(marker string) *float64 {
	if marker == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(marker), 64); err == nil {
		return &f
	}
	slice := marker
	if len(slice) > 19 {
		slice = slice[:19]
	}
	for _, layout := range []string{whenAtDateTimeLayout, whenAtDateLayout, whenAtSlashLayout} {
		if t, err := time.Parse(layout, slice); err == nil {
			f := float64(t.Unix())
			return &f
		}
	}
	return nil
}

func nullableFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

// ---------------------------------------------------------------------------
// intent_hits signal (ASCII-only approximation of the reference's jieba
// term extraction — same stopword filters, same match rules).
// ---------------------------------------------------------------------------

// asciiStopwords is the reference engine's exact ASCII stopword list —
// intent filler words must not count as hits.
const asciiStopwords = `
a an and are as at be but by can could did do does for from had has have how i
if in into is it its me my no not of on or our shall should so that the their
them then there these they this to us vs was we were what when where which who
why will with would you your don't can't won't we're we've it's
`

// cjkStopwords is the reference engine's exact CJK filler-word list —
// substring match (jieba merges colloquial words into single tokens).
const cjkStopwords = `
今天 明天 昨天 前天 后天 天气 怎么样 怎样 怎么 什么 为什么 为啥 是否 是不是
有没有 可不可以 能不能 可以 吗 呢 吧 啊 呀 哦 嗯 啦 一下 看看 请问 帮忙
好的 对的 是的 对了 另外 然后 这样 那样 这个 那个 这些 那些 哪个 哪些
现在 目前 其实 感觉 觉得 认为 想要 需要 我们 你们 他们 大家 之前 之后
`

var (
	asciiStopwordSet = func() map[string]bool {
		m := map[string]bool{}
		for _, w := range strings.Fields(asciiStopwords) {
			m[w] = true
		}
		return m
	}()
	cjkStopwordList = strings.Fields(cjkStopwords)

	// asciiOnlyTerm matches the reference's _ASCII_ONLY: word-ish ASCII
	// tokens get word-boundary matching, everything else substring.
	asciiOnlyTerm = regexp.MustCompile(`^[A-Za-z0-9 .\-_/]+$`)
)

// intentHits counts how many of the intent's query terms appear in the hit's
// question/answer — the intent-alignment signal (intent_hits). The reference
// extracts terms with jieba for CJK; the embedded engine approximates with
// whitespace-split terms (ASCII word-boundary match, CJK substring match)
// over the same stopword filters, so the signal stays meaningful without the
// Python dependency. Topic is excluded on purpose (conversation fragments
// slip past on generic words — same rationale as the reference).
func intentHits(intent, question, answer string) int {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return 0
	}
	qLow := strings.ToLower(question)
	aLow := strings.ToLower(answer)
	hits := 0
	for _, term := range strings.Fields(intent) {
		if len([]rune(term)) < 2 {
			continue // MIN_QUERY_TERM_LEN
		}
		key := strings.ToLower(term)
		if asciiStopwordSet[key] {
			continue
		}
		ascii := asciiOnlyTerm.MatchString(term)
		if !ascii && containsAny(term, cjkStopwordList) {
			continue
		}
		if ascii {
			pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `\b`)
			if pat.MatchString(qLow) || pat.MatchString(aLow) {
				hits++
			}
		} else if strings.Contains(question, term) || strings.Contains(answer, term) {
			hits++
		}
	}
	return hits
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// meaningfulQueryTokens extracts the query's meaningful tokens for the
// coverage signal: the same filters as intentHits (≥2 runes, ASCII stopwords
// skipped, CJK stopword-substrings skipped) so filler words like "about" or
// "the" never inflate a hit's coverage.
//
// A CJK run is SEGMENTED, not kept whole (the fix measured in
// devdoc/memory-route3-relevance.md). Keeping it whole made coverage
// structurally zero for every natural CJK question: strings.Fields splits on
// whitespace, a natural Chinese question has no whitespace, so the ONE
// "token" was the entire 20-rune question — a string no record's text
// contains, hence tokens=0 for every candidate, hence a rescore in which the
// coverage term was constant and confidence alone decided the order. The
// segmenter (the same one the index column is written with) yields the words
// a natural question is actually made of, so coverage measures what it claims
// to: how much of the ASK the record answers.
func meaningfulQueryTokens(query string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(term string) {
		if len([]rune(term)) < 2 {
			return
		}
		key := strings.ToLower(term)
		if asciiStopwordSet[key] {
			return
		}
		if !asciiOnlyTerm.MatchString(term) && containsAny(term, cjkStopwordList) {
			return
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, term)
	}
	for _, field := range strings.Fields(query) {
		for _, run := range splitCJKRuns(field) {
			if !hasCJK(run) {
				add(run)
				continue
			}
			for _, word := range cjkQuerySegments(run) {
				add(word)
			}
		}
	}
	return out
}

// queryTokenCoverage counts how many distinct meaningful query tokens appear
// in the hit's topic+question+answer — the primary ranking key. ASCII tokens
// match word-boundary, CJK substring (same rules as intentHits, but topic is
// included: a hit whose topic names the subject deserves the coverage).
func queryTokenCoverage(query string, h *MemoryHit) int {
	terms := meaningfulQueryTokens(query)
	if len(terms) == 0 {
		return 0
	}
	// M1: the exam phrasings and 5W1H labels are part of the recall surface —
	// a query word matching an exam question (an alternate phrasing of the
	// record) or a 5W1H label counts as covered intent, which is exactly the
	// "user asks differently than the stored body" case M1 targets.
	hay := strings.ToLower(h.Topic + " " + h.Question + " " + h.Answer + " " +
		h.examQuestions + " " + h.kwLabels)
	n := 0
	for _, term := range terms {
		key := strings.ToLower(term)
		if asciiOnlyTerm.MatchString(term) {
			pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `\b`)
			if pat.MatchString(hay) {
				n++
			}
		} else if strings.Contains(hay, key) {
			n++
		}
	}
	return n
}

// memoryJiebaTextCols renders the jieba_token source columns as a value list
// for a SELECT on the base table — probed against the ACTUAL schema so an
// older DB that lacks a column never breaks the query (it renders ” instead,
// the same "only added, never dropped" adaptation the INSERT path uses).
func memoryJiebaTextCols(cols map[string]bool) []string {
	out := make([]string, 0, len(m1JiebaTextCols))
	for _, c := range m1JiebaTextCols {
		if cols[c] && c != "search_kw" { // search_kw is already in hand at both call sites
			out = append(out, c)
		}
	}
	return out
}

// jiebaTokenText composes the text jieba_token is derived from: the row's own
// prose (topic + question + answer) followed by every retrieval-intent column
// the record carries. Passed explicitly (rather than read here) so the caller
// owns the schema probing — see m1JiebaTextCols for why these parts must all
// be present, and memoryJiebaTextSep for the joining rule.
func jiebaTokenText(parts []string) string {
	text := ""
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if text != "" {
			text += m1JiebaTextSep
		}
		text += p
	}
	return text
}

// searchKwValue renders the keyword list the way the search_kw column stores
// it, so the value written into the column and the one absorbed into
// jieba_token can never disagree.
func searchKwValue(kws []string) string {
	return strings.Join(cleanKeywords(kws), ", ")
}

// deriveJiebaToken recomputes jieba_token for a row being written: the
// question+answer body, the topic, the current search_kw value, and every
// OTHER retrieval-intent column value the row carries. Callers pass the
// already-resolved values (a fresh store has empty intent columns; an update
// must pass the STORED ones — see MemoryUpdate).
func deriveJiebaToken(topic, question, answer, searchKw string, intentCols []string) string {
	parts := []string{topic, question, answer, searchKw}
	parts = append(parts, intentCols...)
	return cjkSegmentation(jiebaTokenText(parts))
}

// rowIntentColumns reads the stored value of every jieba_token source column
// beyond search_kw for one row — the read-modify-write half of the
// absorption contract (memory_update must re-derive jieba_token from the
// values the row KEEPS, not from the fields the caller happened to set).
func rowIntentColumns(ctx context.Context, q QueryExecer, id int, cols map[string]bool) ([]string, error) {
	take := memoryJiebaTextCols(cols)
	if len(take) == 0 {
		return nil, nil
	}
	sel := make([]string, 0, len(take))
	for _, c := range take {
		sel = append(sel, "COALESCE("+c+",'')")
	}
	held := make([]sql.NullString, len(take))
	dest := make([]interface{}, 0, len(take))
	for i := range held {
		dest = append(dest, &held[i])
	}
	if err := q.QueryRowContext(ctx,
		"SELECT "+strings.Join(sel, ", ")+" FROM distilled_qa WHERE id=?", id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("read retrieval-intent columns: %w", err)
	}
	out := make([]string, 0, len(take))
	for _, h := range held {
		out = append(out, h.String)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// garbage / quality gate (feature 3: reference core/quality.py).
// ---------------------------------------------------------------------------

// garbagePrefixes are the hermes v1.5 garbage fragments whose presence at the
// start of a question marks it as non-knowledge (reference
// QualityPolicy.GARBAGE_PREFIXES).
var garbagePrefixes = []string{"[ASYNC DELEGATION", "[Background process"}

// metaIngestionQueryPats are the tool-name tokens whose presence in the
// question marks "the memory_search results were copy-pasted as knowledge"
// — the meta-ingestion pollution (reference
// QualityPolicy.META_INGESTION_QUERY_PAT). These are CONTENT patterns over
// LLM-produced prose, deliberately kept as literals (not toolnames
// constants): the human-phrase sibling "memory search" must match in
// tandem, and matching stays tied to the tool's historical name even if
// the tool is renamed (the pollution it detects is about the old name).
var metaIngestionQueryPats = []string{"memory_search", "memory search"}

// metaIngestionResultMarks are the "this is a search result" markers that
// confirm meta-ingestion when combined with a tool token (reference
// QualityPolicy.META_INGESTION_RESULT_MARK).
var metaIngestionResultMarks = []string{"检索结果", "搜索结果", "search results", "检索摘要"}

// meaningfulCharsRe matches a single CJK ideograph or an ASCII alphanumeric —
// the characters that make text information-bearing (reference _MEANINGFUL).
var meaningfulCharsRe = regexp.MustCompile(`[\p{Han}A-Za-z0-9]`)

// isGarbage reports whether a (question, answer) pair is rejected as
// non-knowledge. Any single rule hit → garbage:
//  1. question starts with a known garbage prefix (async-delegation /
//     background-process artifacts).
//  2. meta-ingestion pollution: question mentions memory_search AND the
//     text carries a "search-result" marker.
//  3. no information content: fewer than minMeaningfulChars CJK/ASCII
//     alphanumerics in question + answer[:200].
func isGarbage(question, answer string) bool {
	q := strings.TrimSpace(question)
	if q == "" {
		return true
	}
	for _, p := range garbagePrefixes {
		if strings.HasPrefix(q, p) {
			return true
		}
	}
	if metaIngestion(q, answer) {
		return true
	}
	haystack := q + (answer + " ")[:min(len(answer)+1, 201)]
	if len(meaningfulCharsRe.FindAllStringIndex(haystack, -1)) < minMeaningfulChars {
		return true
	}
	return false
}

// metaIngestion is rule 2 of isGarbage: the question mentions a memory_search
// tool AND either the question or answer carries a "检索/搜索结果" marker —
// that is a retrieved-results transfer, not knowledge.
func metaIngestion(question, answer string) bool {
	qLow := strings.ToLower(question)
	toolHit := false
	for _, p := range metaIngestionQueryPats {
		if strings.Contains(qLow, strings.ToLower(p)) {
			toolHit = true
			break
		}
	}
	if !toolHit {
		return false
	}
	haystack := question + " " + answer
	for _, m := range metaIngestionResultMarks {
		if strings.Contains(haystack, m) {
			return true
		}
	}
	return false
}

// passesBasicGate applies the reference's basic length gate (question/
// answer length + answer/question ratio). Returns (ok, reason).
func passesBasicGate(question, answer string) (bool, string) {
	q := strings.TrimSpace(question)
	a := strings.TrimSpace(answer)
	if len([]rune(q)) < minQuestionLen {
		return false, fmt.Sprintf("question too short (%d < %d)", len([]rune(q)), minQuestionLen)
	}
	if len([]rune(q)) > maxQuestionLen {
		return false, fmt.Sprintf("question too long (%d > %d)", len([]rune(q)), maxQuestionLen)
	}
	if len([]rune(a)) < minAnswerLen {
		return false, fmt.Sprintf("answer too short (%d < %d)", len([]rune(a)), minAnswerLen)
	}
	if float64(len([]rune(a))) < minQARatio*float64(len([]rune(q))) {
		return false, fmt.Sprintf("answer/question ratio too low (%d/%d)", len([]rune(a)), len([]rune(q)))
	}
	return true, ""
}

// ---------------------------------------------------------------------------
// similarity dedup (feature 4: reference INSERT-后 trigram Jaccard).
// ---------------------------------------------------------------------------

// trigramSet builds the set of overlapping n-grams (default trigramGram=3)
// of a string — the reference _trigram_set.
func trigramSet(text string, n int) map[string]bool {
	r := []rune(text)
	set := map[string]bool{}
	for i := 0; i+n <= len(r); i++ {
		set[string(r[i:i+n])] = true
	}
	return set
}

// jaccard returns the Jaccard similarity of two strings' trigram sets —
// the reference _jaccard.
func jaccard(a, b string) float64 {
	sa := trigramSet(a, trigramGram)
	sb := trigramSet(b, trigramGram)
	if len(sa) == 0 || len(sb) == 0 {
		return 0.0
	}
	inter := 0
	for k := range sa {
		if sb[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(sa)+len(sb)-inter)
}

// dedupTokenize is the reference _tokenize (No-jieba fallback): ASCII words
// of ≥3 chars, capped at 8. Used as the FTS5 prescreen query for similarity
// dedup.
func dedupTokenize(text string, capN int) []string {
	words := regexp.MustCompile(`[a-zA-Z]{3,}`).FindAllString(strings.ToLower(text), -1)
	if len(words) > capN {
		words = words[:capN]
	}
	return words
}

// ---------------------------------------------------------------------------
// store.
// ---------------------------------------------------------------------------

// MemoryStore runs the memory_store tool: save a distilled lesson
// (topic/question/answer) into the embedded store. source is optional
// provenance. keywords are the 5W1H retrieval breadcrumbs (检索面包屑) for
// future memory_search queries: the schema has no keyword column, so they are
// merged into the answer tail — persisted with the record, searchable, and
// harmless to older databases. No keywords behaves exactly as before
// (backward compat). Returns the store confirmation including the dedup
// signal — deduped means the topic already had an entry this merged into.
// MemoryStore stores one distilled lesson through the standard write path
// (validation, garbage gate, exact+similarity dedup, FTS sync) on its own
// connection. The row-level core is memoryStoreRow — the single-transaction
// import path (SyncImport) calls the core directly on its own transaction.
func MemoryStore(topic, question, answer, source string, keywords []string) (string, error) {
	return MemoryStoreWithOrigin(topic, question, answer, source, keywords, MemoryOrigin{})
}

// MemoryStoreWithOrigin is MemoryStore plus the provenance pointer (epic 自动记忆
// item 2/5, memory_origin.go): the session the row was captured in, the
// session's own 1-based round number, and the node it was written on. These are
// the columns that keep a self-contained captured body ADDRESSABLE — the body
// (scheme C) is what makes the row survive a cross-host sync, the pointer is
// what lets a reader walk back to the session it came from.
//
// A caller that knows no session (the CLI, a sync import, a test) passes the
// zero MemoryOrigin and behaves exactly like MemoryStore did before. A caller
// that passes HALF a pointer is refused (Rule 15): a round with no session is
// not resolvable, and storing it would be a claim nobody can follow.
func MemoryStoreWithOrigin(topic, question, answer, source string, keywords []string, origin MemoryOrigin) (string, error) {
	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	return memoryStoreRow(ctx, db, topic, question, answer, source, keywords, origin)
}

// memoryStoreRow is the store core shared by MemoryStore (own connection)
// and SyncImport (one transaction for the whole patch). q receives either
// the DB or the transaction. topic/question/answer are trimmed here; the
// empty-field verdict is the caller's contract (MemoryStore wraps it as a
// memory_store error, SyncImport counts it as a refused row).
//
// origin is the provenance pointer; its completeness is checked HERE, before
// any write, so a half pointer can never reach a row (the pointer's own
// constants and refusal text live in memory_origin.go).
func memoryStoreRow(ctx context.Context, q QueryExecer, topic, question, answer, source string, keywords []string, origin MemoryOrigin) (string, error) {
	topic = decodeLiteralEscapes(strings.TrimSpace(topic))
	question = decodeLiteralEscapes(strings.TrimSpace(question))
	answer = decodeLiteralEscapes(strings.TrimSpace(answer))
	if topic == "" || question == "" || answer == "" {
		return "", fmt.Errorf("memory_store: topic, question and answer are all required")
	}
	if !origin.Complete() {
		return "", fmt.Errorf("%s", origin.Refusal())
	}

	kws := cleanKeywords(keywords)
	for i, k := range kws {
		kws[i] = decodeLiteralEscapes(k)
	}
	if len(kws) > 0 {
		answer += "\n\n" + keywordsTailMarker + strings.Join(kws, ", ")
	}

	cols, err := tableColumns(ctx, q, "distilled_qa")
	if err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}

	// Feature 3: garbage gate (reference core/quality.py `_is_garbage`). A
	// rejected candidate is refused before any DB write — never distilled as
	// knowledge. Checked after the DB opens so a foreign-file rejection (above)
	// takes precedence. The contract steers the model to store distilled
	// lessons only; this is the hard engine side of that gate.
	if isGarbage(question, answer) {
		return "", fmt.Errorf("memory_store: %w", ErrGarbageRefused)
	}

	// Exact dedup (the reference contract: question[:200] + topic). On a hit
	// the new source is merged into the existing row and the existing id is
	// returned with deduped=true. The key width is dedupQuestionRunes but
	// the stored question is the FULL text — so the lookup must compare the
	// truncated probe against the truncated column (stray trailing chars
	// beyond 200 runes would otherwise break every re-store of a long
	// question — m4 acceptance caught this: a 217-rune brew row duplicated
	// on re-import because dedup compared 200 runes against 217).
	var existing int64
	var existingAnswer string
	// The exact-key screen folds on IDENTITY (question[:200] + topic), never on
	// the body — so the arrival's answer is read back beside the id purely to
	// be able to SAY when the fold discarded a different body (Rule 15: the
	// merge stays identical, only its silence ends). `cols` decides whether the
	// column exists; a schema without it simply has no verdict to give.
	exactKeyQuery := "SELECT id FROM distilled_qa WHERE substr(question,1,?)=? AND topic=?"
	if cols["answer"] {
		exactKeyQuery = "SELECT id, COALESCE(answer,'') FROM distilled_qa WHERE substr(question,1,?)=? AND topic=?"
	}
	keyRow := q.QueryRowContext(ctx, exactKeyQuery,
		dedupQuestionRunes, truncateRunes(question, dedupQuestionRunes), topic)
	if cols["answer"] {
		err = keyRow.Scan(&existing, &existingAnswer)
	} else {
		err = keyRow.Scan(&existing)
	}
	if err == nil {
		if cols["sources"] {
			if err := mergeSources(ctx, q, existing, source); err != nil {
				return "", fmt.Errorf("memory_store: %w", err)
			}
		}
		// The body verdict compares the ROW's knowledge, not the store's
		// regenerable breadcrumbs: the keywords tail is appended from a
		// separate column on every write, so a re-import that carries the same
		// knowledge re-renders it — counting that as a divergence would put a
		// warning on every idempotent re-import and make the signal worthless.
		bodyDiffering := cols["answer"] &&
			stripKeywordsTail(answer) != stripKeywordsTail(existingAnswer)
		return storeResultBody(int(existing), true, exactKeySimilarity, bodyDiffering), nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("memory_store: dedup check failed: %w", err)
	}

	// Feature 4: similarity dedup (reference INSERT-后 trigram-Jaccard). When
	// the exact key misses, the topic's recent rows are compared as WHOLE
	// retrieval surfaces (see findSameLessonDuplicate for why the reference's
	// question-vs-answer cross-field screen had to be replaced here). Above the
	// threshold the new source merges into the existing entry and the existing
	// id + deduped=true is returned.
	// Cleanup layer: the exact-key screen above owns verbatim re-stores (and is
	// the one that also runs on a DB whose FTS index is unusable); this screen
	// adds the re-worded twin, which no key can see.
	if simID, simScore, err := findSameLessonDuplicate(ctx, q, topic, question, answer); err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	} else if simID > 0 { // else-ok: the error branch is the guard clause; a second `if` would re-run the screen
		if cols["sources"] {
			if err := mergeSources(ctx, q, simID, source); err != nil {
				return "", fmt.Errorf("memory_store: %w", err)
			}
		}
		return storeResult(int(simID), true, simScore), nil
	}

	// New entry: INSERT only the columns the actual schema has (old
	// hermes-schema DBs may lack newer columns — they get defaults and the
	// INSERT adapts instead of failing).
	insCols := []string{"topic", "question", "answer"}
	vals := []interface{}{topic, question, answer}
	for _, c := range append([]string{"sources", "confidence", "tier", "distilled_at", "lesson_type", "when_at", "search_kw", "jieba_token",
		m1ExamQuestions, "kw_what", "kw_why", "kw_when", "kw_where", "kw_who"}, originColumns...) {
		if !cols[c] {
			continue
		}
		insCols = append(insCols, c)
		switch c {
		case originSessionCol:
			vals = append(vals, strings.TrimSpace(origin.SessionID))
		case originRoundCol:
			vals = append(vals, origin.Round)
		case originHostCol:
			// The host is stamped on EVERY new row that has a session — a row
			// with a pointer but no host could not be told apart from one
			// imported from elsewhere. A caller with no pointer at all gets no
			// host either: absence stays absence (Rule 15), never a fabricated
			// host claimed for a row nobody captured.
			if origin.None() {
				vals = append(vals, "")
			} else {
				vals = append(vals, origin.HostOrResolve())
			}
		case "sources":
			vals = append(vals, source)
		case "confidence":
			vals = append(vals, defaultConfidence)
		case "tier":
			vals = append(vals, defaultTier)
		case "distilled_at":
			vals = append(vals, float64(time.Now().Unix()))
		case "search_kw":
			// Fix 2 (report §2.3 + §8 #1): the 5W1H keywords are persisted
			// into their dedicated search_kw column (not just the answer
			// tail) so FTS5's six-column index covers them as standalone
			// comma-separated tokens — a keyword query hits directly even
			// when it never appears verbatim in the answer body.
			vals = append(vals, searchKwValue(kws))
		case "jieba_token":
			// Fix 2 + search_kw absorption: the column carries the words a
			// real Chinese segmenter (internal/cjkseg, gse over its embedded
			// dictionary) finds in the row, joined by cjkSegmentSep — so the
			// FTS index sees the CJK content broken into matchable WORDS
			// rather than one token per run. The source is the row's whole
			// retrieval surface: the topic+question+answer body, search_kw
			// (the phrasings written for retrieval, which the body often does
			// not contain), and every other intent column. This replaced a
			// comma-joined list of 2-char grams — a dictionary-free stand-in
			// whose own history and the before/after measurement are in
			// devdoc/jieba-token-real-segmentation.md; see m1JiebaTextCols for
			// the contract.
			vals = append(vals, deriveJiebaToken(topic, question, answer, searchKwValue(kws), nil))
		case "lesson_type":
			// The value signal the search render promises (`lesson=`), derived
			// from the row's own prose (see deriveLessonType). This column had
			// no writer at all: the INSERT fell to `default:` and wrote "",
			// so the tool description promised a signal no row ever carried
			// (measured on the production store: 5062/5062 rows empty).
			vals = append(vals, deriveLessonType(question, answer))
		case "when_at":
			// The staleness marker possiblyStale() reads (and with it
			// possibly_stale=true — the SIGNAL is unchanged; the render shows
			// it as about= while the row's age comes from distilled_at, so one
			// field no longer carries two meanings). Derived from a date the row's own text
			// states, falling back to the observation time — the row was
			// distilled now, so that IS a when. Like lesson_type this column
			// had no writer, which is why possibly_stale was dead code in
			// production rather than a signal.
			vals = append(vals, deriveWhenAt(question, answer, time.Now()))
		default:
			// The M1 recall columns (exam_questions + kw_what..who) start
			// empty on store: the LLM contract does not provide them, and the
			// backfill (scripts/backfill_exam_5w1h) fills them for existing
			// rows — the columns stay live for FTS.
			vals = append(vals, "")
		}
	}
	placeholders := strings.Repeat("?,", len(vals))
	placeholders = placeholders[:len(placeholders)-1]
	res, err := q.ExecContext(ctx,
		"INSERT INTO distilled_qa ("+strings.Join(insCols, ", ")+") VALUES ("+placeholders+")", vals...)
	if err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}

	// Feature 5: FTS sync fallback. gopi's own schema installs triggers that
	// keep distilled_qa_fts in sync; but a foreign DB (hermes production copy)
	// may lack them. When triggers are absent we explicitly sync the new row
	// into distilled_qa_fts (idempotent external-content-table INSERT) so a
	// search always finds just-stored rows, trigger topology or not. With
	// triggers present, the trigger already wrote the row.
	if !ftsTriggersOwned(ctx, q) {
		if err := syncFTSRow(ctx, q, id); err != nil {
			if verr := verifyRowIndexed(ctx, q, id); verr != nil {
				// Silent failure forbidden: the row was inserted but NOT
				// indexed, and it cannot be repaired — report the index
				// failure (the row stays, a later rebuild heals it) instead
				// of returning success for a row the search will never find.
				syncNote := err.Error()
				return "", fmt.Errorf("memory_store: row %d stored but not indexed: %s (index probe: %w)", id, syncNote, verr)
			}
		}
	}
	// Post-store index guarantee (the t-mem-flake fix): a store must never
	// report success for a row the FTS index cannot see. With the atomic
	// first-open schema this always holds on a gopi-owned DB; the check makes
	// the guarantee explicit rather than implied, and turns a partial-schema
	// store into a loud failure.
	if err := verifyRowIndexed(ctx, q, id); err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}
	return storeResult(int(id), false, noSimilarity), nil
}

// verifyRowIndexed asserts that the freshly stored row is actually present in
// the FTS index, so a store can never silently commit an unsearchable row
// (Rule 15). It reads the FTS5 docsize shadow table, which carries one row
// per INDEXED base row — the only probe that distinguishes an indexed row
// from a base row merely reflected through the external-content vtable.
// A missing index table (a not-yet-migrated legacy/foreign DB) is returned as
// an error, and the store turns it into a loud failure rather than a silent
// success.
func verifyRowIndexed(ctx context.Context, q QueryExecer, id int64) error {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM "+memoryFTSIndexTable+" WHERE id=?", id).Scan(&n)
	if err != nil {
		return fmt.Errorf("FTS index probe failed (index %s unusable): %w", ftsSyncFTSName, err)
	}
	if n == 0 {
		return fmt.Errorf("row %d is not in the FTS index (%s) — the DB schema is partial", id, ftsSyncFTSName)
	}
	return nil
}

// findSimilarDuplicate implements the reference's _find_duplicate fuzzy step:
// tokenize question, FTS5-prescreen up to ftsDedupCandidates similar rows, and
// return the first whose answer[:300] trigram-Jaccard against
// question+topic exceeds trigramJaccardMerge. Returns (id, score) with
// id=0 when no similar duplicate is found.
//
// This is the LEGACY cross-field screen (question vs answer). It is kept as-is
// for the callers that inherit its verdict — the brew guard's pre-screen and
// the agent_dedup path — and because a THRESHOLD is a contract: changing what
// this function compares would silently retune them. The store's own dedup no
// longer relies on it (see findSameLessonDuplicate): measured on the production
// store it scores a re-worded twin at 0.181-0.219, below its own 0.5 gate, so
// it can never merge the rows defect 2 is about.
func findSimilarDuplicate(ctx context.Context, q QueryExecer, question, topic string) (int64, float64) {
	// Match the exact-dedup key width: dedup decides on question[:200] +
	// topic, so the FTS probe uses the same truncated probe. (The stored
	// column keeps the full text — probing FTS with the full 217-rune
	// question still pre-screens, but the Jaccard verdict then compares a
	// text the exact key never sees; keeping both paths on the same probe
	// makes dedup deterministic. m4 acceptance: long-question re-import.)
	question = truncateRunes(question, dedupQuestionRunes)
	words := dedupTokenize(question, 8)
	if len(words) == 0 {
		return 0, 0.0
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + w + `"`
	}
	ftsQ := strings.Join(quoted, " OR ")
	rows, err := q.QueryContext(ctx,
		`SELECT id, answer FROM distilled_qa WHERE id IN
		 (SELECT rowid FROM distilled_qa_fts WHERE distilled_qa_fts MATCH ?)
		 ORDER BY id DESC LIMIT ?`, ftsQ, ftsDedupCandidates)
	if err != nil {
		return 0, 0.0 // FTS unavailable → degrade to insert-only
	}
	defer rows.Close()
	merged := question + " " + topic
	for rows.Next() {
		var id int64
		var ans sql.NullString
		if err := rows.Scan(&id, &ans); err != nil {
			return 0, 0.0
		}
		cAns := ans.String
		r := []rune(cAns)
		if len(r) > legacyCrossFieldAnswerRunes {
			r = r[:legacyCrossFieldAnswerRunes]
		}
		cAns = string(r)
		if s := jaccard(merged, cAns); s > trigramJaccardMerge {
			return id, s
		}
	}
	return 0, 0.0
}

// findSameLessonDuplicate merges a row that states the SAME LESSON as an
// existing one, even when the wording differs (defect 2).
//
// The old screen could not: it prescreened candidates from QUESTION ASCII
// tokens alone and then compared jaccard(question, answer[:300]) — a
// cross-field, asymmetric comparison whose score for a genuine re-worded twin
// (measured on the production store: 0.181/0.219) never reaches the 0.5 merge
// gate. Two shapes of twin were therefore missed, both live in the store:
//
//	#1906/#1908, #1491/#1494 — the same lesson re-worded (question AND answer
//	both differ): qq=0.30/0.32, aa=0.43/0.43, but question-vs-answer only 0.18.
//	#1491/#1494/#1496 — one topic, three rows (#1491 and #1496 share a topic and
//	less than 0.24 of their text): the incoming row's question-token prescreen
//	could not even REACH them, because it never considers the topic.
//
// What this screen does instead:
//   - the candidate band is the topic's most recent lessonDedupCandidates rows,
//     which IS how the topic twins of #1491/#1494/#1496 become reachable;
//   - both sides are the row's whole retrieval surface (topic+question+answer,
//     keywords tail excluded) — the SAME string shape on both sides, so the
//     verdict is symmetric and a re-wording on either field is averaged over
//     the row rather than being the whole comparison;
//   - the gate is lessonJaccardMerge plus the shared-trigram floor (both
//     conditions must hold; the constant block carries the measured populations).
//
// The surface is deliberately NOT truncated: capping the answer at 600 runes
// dropped a genuine twin from 0.438 to 0.283 while leaving the distinct-pair
// score at 0.181-0.243 — it moved the twins toward the noise, not away from it.
//
// Returns (id, score); id=0 means no same-lesson row. A SQL failure is
// reported as an error (Rule 15) — never silently read as "no duplicate", or
// a broken DB would start duplicating every row.
func findSameLessonDuplicate(ctx context.Context, q QueryExecer, topic, question, answer string) (int64, float64, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, topic, question, answer FROM distilled_qa
		 WHERE topic=? ORDER BY id DESC LIMIT ?`, topic, lessonDedupCandidates)
	if err != nil {
		return 0, 0.0, fmt.Errorf("same-lesson screen: %w", err)
	}
	defer rows.Close()

	mine := lessonSurface(topic, question, answer)
	var bestSurface float64
	for rows.Next() {
		var id int64
		var rTopic, rQuestion, rAnswer sql.NullString
		if err := rows.Scan(&id, &rTopic, &rQuestion, &rAnswer); err != nil {
			return 0, 0.0, fmt.Errorf("same-lesson screen: %w", err)
		}
		surface, shared := compareLessonSurfaces(mine, lessonSurface(rTopic.String, rQuestion.String, rAnswer.String))
		// Both conditions must hold: a high Jaccard alone is reachable by two
		// short template rows about DIFFERENT subjects (lessonMinSharedTrigrams
		// documents the measured cases).
		if surface > lessonJaccardMerge && shared >= lessonMinSharedTrigrams {
			return id, surface, nil
		}
		if surface > bestSurface {
			bestSurface = surface
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0.0, fmt.Errorf("same-lesson screen: %w", err)
	}
	return 0, bestSurface, nil
}

// compareLessonSurfaces returns the trigram Jaccard of two retrieval surfaces
// AND the count of trigrams they share. The count is the length-independent
// half of the verdict: Jaccard alone cannot tell two long rows about one lesson
// (0.438, shared 1087) from two long rows on one topic stating different lessons
// (0.234, shared 396) — the margin is thin — while on SHORT rows it inverts
// (two short distinct rows about one product reach 0.500 on 32 shared trigrams).
func compareLessonSurfaces(a, b string) (float64, int) {
	sa := trigramSet(a, trigramGram)
	sb := trigramSet(b, trigramGram)
	if len(sa) == 0 || len(sb) == 0 {
		return 0.0, 0
	}
	shared := 0
	for k := range sa {
		if sb[k] {
			shared++
		}
	}
	return float64(shared) / float64(len(sa)+len(sb)-shared), shared
}

// lessonSurface renders the ONE string a same-lesson comparison is made on:
// the row's whole retrieval surface — topic, question and the FULL answer body
// (the keywords tail excluded, the same exclusion the store's dedup and the
// reranker apply). It is deliberately NOT truncated: capping the answer at
// 600 runes (a limit an earlier revision carried) drops a re-worded twin from
// 0.438 to 0.283 while leaving the distinct-pair score at 0.181-0.243, i.e. it
// moves the twins toward the noise rather than away from it.
func lessonSurface(topic, question, answer string) string {
	return strings.Join([]string{topic, question, stripKeywordsTail(answer)}, " ")
}

// ---------------------------------------------------------------------------
// value-signal derivation (lesson_type / when_at).
//
// Both columns exist in the schema, are read back by every search (the render
// prints lesson=, and possibly_stale is computed from when_at — which the
// render shows as about= beside stored=/age=, see renderAgeSegment) and had NO
// writer: memoryStoreRow's INSERT fell to `default:` and wrote "" for each, so
// in production both stayed empty on every row ever stored (measured 2026-09-23:
// 5062/5062 rows empty in both) and possibly_stale was dead code. These are the
// writers — DERIVED from the row's own text, because the store's contract has no
// field for them (an LLM-supplied lesson_type would be a better source and is
// the follow-up; until then a derived value is strictly more signal than "").
// ---------------------------------------------------------------------------

// lessonTypeVocab is the closed vocabulary written into lesson_type: the values
// the tool description already advertises ("success paths, pitfalls, lessons")
// plus the two shapes the store's own callers use. Nothing else is written, and
// every entry is named here so a test can enumerate them (Rule 1b/13).
var lessonTypeVocab = []string{
	lessonTypeRecipe,
	lessonTypePitfall,
	lessonTypeFailure,
	lessonTypeLesson,
}

// The vocabulary entries, plus the marker spellings the classifier keys on —
// both named, because the classifier and its test must agree on the exact
// spelling (a re-typed literal at either site would silently never match).
const (
	lessonTypeRecipe  = "recipe"
	lessonTypePitfall = "pitfall"
	lessonTypeFailure = "failure"
	lessonTypeLesson  = "lesson"

	lessonMarkerHowPrefix = "how "
	lessonMarkerHowTo     = "how to"
	lessonMarkerSteps     = "step"
	lessonMarkerRootCause = "root cause"
)

// lessonPromptMarkers are the QUESTION-side markers each type is recognised by,
// in classification order (the most specific prompt shape wins — a "how do I
// avoid the pitfall" question is a pitfall lesson, not a recipe). Classifying on
// the QUESTION is deliberate: the question states what kind of knowledge the row
// is, while the answer body of almost any lesson contains the word "error"
// somewhere, which would classify the whole store as one type. Markers are
// matched case-insensitively against the raw question; the CJK entries are the
// equivalent phrasings in the language most of the store is written in
// (踩坑/坑 = pitfall, 如何/怎么/怎么 = how-to, 根因 = root cause).
var lessonPromptMarkers = map[string][]string{
	lessonTypePitfall: {lessonTypePitfall, "gotcha", "caveat", "beware", "trap", "踩坑", "坑", "陷阱", "注意", "别"},
	lessonTypeRecipe:  {lessonTypeRecipe, lessonMarkerHowTo, lessonMarkerHowPrefix, "recipe", "workflow", "步骤", "如何", "怎么", "方法"},
	lessonTypeFailure: {lessonMarkerRootCause, lessonTypeFailure, "fail", "bug", "broke", "crash", "panic", "regress", "wrong", "missing", "lost", "失败", "报错", "根因", "丢失"},
}

// deriveLessonType classifies one row into lessonTypeVocab from its own prose.
// Deterministic and total: every row gets a member of the vocabulary (the
// fallback is lessonTypeLesson), so the column can never go back to being empty
// and the search render's `lesson=` field always carries a value.
func deriveLessonType(question, answer string) string {
	hay := strings.ToLower(question)
	// Pitfall then recipe then failure: the order is the specificity order —
	// "why does X fail (踩坑记录)" is a pitfall row, and a "how to …" question is a
	// recipe even when its answer narrates a failure.
	for _, t := range []string{lessonTypePitfall, lessonTypeRecipe, lessonTypeFailure} {
		for _, m := range lessonPromptMarkers[t] {
			if strings.Contains(hay, m) {
				return t
			}
		}
	}
	return lessonTypeLesson
}

// whenAtDateRe matches the calendar dates a row's own text states (the shape
// the store's distilled rows carry: an ISO date, or the same with slashes).
// Named because the marker, its parse (parseTimeMarker) and the regression test
// must read the SAME shape — a re-typed pattern at either site would silently
// stop matching.
var whenAtDateRe = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}`)

// deriveWhenAt produces the when_at marker for a row being stored: the date the
// row's own text states, falling back to now (the row IS being observed now, so
// the fallback is the one date the store can vouch for). A stated date that lies
// in the FUTURE is ignored — a planned date or a version string is not an
// observation, and letting one through would make possibly_stale permanently
// false for that row. The returned spelling is whenAtDateTimeLayout, the layout
// parseTimeMarker reads back.
func deriveWhenAt(question, answer string, now time.Time) string {
	// The matcher and the future-date guard are statedDateIn's — the SAME pair
	// the render's about= half uses, so the date a row is stored with and the
	// date it is rendered as about can never disagree (Rule 14).
	if m := statedDateIn("", question, answer, now); m != "" {
		if ts := parseTimeMarker(m); ts != nil {
			return time.Unix(int64(*ts), 0).UTC().Format(whenAtDateTimeLayout)
		}
	}
	return now.UTC().Format(whenAtDateTimeLayout)
}

// ftsTriggersOwned reports whether the host DB installs the FTS keep-in-sync
// triggers (gopi's own schema does; a hermes production copy generally does
// not). Used by syncFTSRow to decide whether manual FTS sync is needed.
func ftsTriggersOwned(ctx context.Context, q QueryExecer) bool {
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type='trigger' AND tbl_name='distilled_qa' LIMIT 1`).Scan(&one)
	return err == nil
}

// syncFTSRow explicitly syncs one base row into the distilled_qa_fts
// external-content table (rowid-aligned). Safe to call on a trigger-owned DB
// too: the trigger's au has already written the row, and an external-content
// INSERT of an already-indexed row is an idempotent upsert (no duplicate).
// The INSERT is built from the INDEX's own declared columns (PRAGMA-probed),
// so a foreign index with a narrower column set than the base table is still
// filled correctly; a DB with no index table fails loudly (the caller turns
// that into a refused store rather than a silently unsearchable row).
func syncFTSRow(ctx context.Context, q QueryExecer, id int64) error {
	// The column set is probed from the VIRTUAL TABLE ITSELF (PRAGMA
	// table_info on an FTS5 vtable lists its declared columns). A foreign DB
	// may have either the legacy six/three columns or the M1 twelve/four, and
	// the INSERT must match the index that is actually there — a column the
	// vtable does not declare would fail the INSERT and silently leave the row
	// unindexed (the t-mem-flake root cause, seen in the legacy-schema test).
	ftsCols, err := tableColumnsInOrder(ctx, q, ftsSyncFTSName)
	if err != nil {
		return fmt.Errorf("syncFTSRow: read %s layout: %w", ftsSyncFTSName, err)
	}
	if len(ftsCols) == 0 {
		return fmt.Errorf("syncFTSRow: %s has no columns (index missing)", ftsSyncFTSName)
	}
	if err := insertFTSRow(ctx, q, ftsSyncFTSName, ftsCols, id); err != nil {
		return err
	}
	// Also sync the trigram FTS index (CJK recall). The table may not exist on
	// older foreign DBs — tolerantly skip.
	if triOK, err := tableExists(ctx, q, memoryTriIndexTable); err == nil && triOK {
		triCols, err := tableColumnsInOrder(ctx, q, memoryTriIndexTable)
		if err != nil {
			return fmt.Errorf("syncFTSRow: read %s layout: %w", memoryTriIndexTable, err)
		}
		if len(triCols) > 0 {
			if err := insertFTSRow(ctx, q, memoryTriIndexTable, triCols, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// insertFTSRow writes one base row into an external-content FTS index at
// exactly the given column list (an idempotent upsert by rowid). Values are
// read through the index's own column list, so the index is filled even when
// the base table carries MORE columns than the index declares.
func insertFTSRow(ctx context.Context, q QueryExecer, ftsTable string, ftsCols []string, id int64) error {
	vals := make([]interface{}, 0, len(ftsCols)+1)
	vals = append(vals, id)
	for _, c := range ftsCols {
		var v sql.NullString
		if err := q.QueryRowContext(ctx,
			"SELECT "+c+" FROM distilled_qa WHERE id=?", id).Scan(&v); err != nil {
			return fmt.Errorf("syncFTSRow: read column %s: %w", c, err)
		}
		vals = append(vals, v.String)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",")
	if _, err := q.ExecContext(ctx,
		"INSERT INTO "+ftsTable+"(rowid, "+strings.Join(ftsCols, ", ")+") VALUES ("+ph+")",
		vals...); err != nil {
		return err
	}
	return nil
}

// cleanKeywords trims each 5W1H breadcrumb and drops empties, so a sloppy
// keyword list cannot degrade the stored answer.
func cleanKeywords(keywords []string) []string {
	out := make([]string, 0, len(keywords))
	for _, k := range keywords {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// truncateRunes caps s at max runes — byte-slicing could split a UTF-8
// character, and the reference dedup key is character-based (question[:200]).
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// splitSources splits a comma-separated sources column into trimmed entries
// (the reference's SOURCE_SEPARATOR format).
func splitSources(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// mergeSources appends new sources to the row's comma-separated list,
// de-duplicating per entry while preserving order (reference _merge_sources).
func mergeSources(ctx context.Context, q QueryExecer, id int64, source string) error {
	if source == "" {
		return nil
	}
	var existing sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT sources FROM distilled_qa WHERE id=?", id).Scan(&existing); err != nil {
		return err
	}
	parts := splitSources(existing.String)
	for _, s := range splitSources(source) {
		if !slices.Contains(parts, s) {
			parts = append(parts, s)
		}
	}
	_, err := q.ExecContext(ctx, "UPDATE distilled_qa SET sources=? WHERE id=?", strings.Join(parts, ","), id)
	return err
}

// storeResult renders the store confirmation the LLM sees (dedup signal
// surfaced so the model knows the entry merged into an existing one).
func storeResult(id int, deduped bool, simScore float64) string {
	return storeResultBody(id, deduped, simScore, false)
}

// storeResultBody is storeResult plus the exact-key body verdict. A caller that
// has no verdict (the similarity screen, a fresh insert) passes false and the
// confirmation reads exactly as before — absent stays absent.
func storeResultBody(id int, deduped bool, simScore float64, bodyDiffering bool) string {
	if deduped {
		// The similarity score rides the confirmation so the caller can see WHY
		// a row merged (a weak merge and an exact-key merge are very different
		// events) — rounded to three decimals, the precision the store's own
		// threshold is argued at.
		//
		// bodyDiffering is the exact-key screen's finding that the arrival's
		// body is NOT the stored body. It is added to the SAME sentence rather
		// than a second one so every existing parser of "deduped into an
		// existing entry" keeps working, and so a caller that only greps the
		// dedup signal cannot miss the divergence by reading half a line.
		line := fmt.Sprintf("Memory stored (id=%d, deduped into an existing entry, similarity=%.3f", id, simScore)
		if bodyDiffering {
			line += ", " + memoryDedupDifferingMarker
		}
		return line + ")."
	}
	return fmt.Sprintf("Memory stored (id=%d).", id)
}

// syncStoreBodyDiffering reports whether a store confirmation says the exact-key
// fold DISCARDED a differing body. One predicate for every channel that counts
// the class (Rule 14): the file import, the live asset sync and brew all read
// the same sentence.
func syncStoreBodyDiffering(storeResult string) bool {
	return strings.Contains(storeResult, memoryDedupDifferingMarker)
}

// ---------------------------------------------------------------------------
// maintenance: memory_get / memory_update / memory_delete (round-3 CLI).
// These are CLI-only (not exposed as LLM tools): the maintenance loop is
// "search → spot a low-quality row → inspect it (memory_get) → demote it
// (memory_update --tier cold) or remove it (memory_delete)". cold demotion
// makes the row stop being 1st tier to return WITHOUT deleting the knowledge.
// ---------------------------------------------------------------------------

// MemoryGet returns one record's full stored content rendered exactly like a
// search hit — the inspection step of the maintenance loop. It opens the DB
// read-only and never mutates.
func MemoryGet(id int) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("memory_get: invalid id %d", id)
	}
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return "", fmt.Errorf("memory_get: %w", err)
	}
	h, err := scanHitByID(ctx, db, cols, id)
	if err != nil {
		return "", fmt.Errorf("memory_get: %w", err)
	}
	out, err := json.Marshal([]MemoryHit{h})
	if err != nil {
		return "", fmt.Errorf("memory_get: %w", err)
	}
	return renderMemorySearchResult(string(out))
}

// scanHitByID loads one row by id through the same scan path as the search
// routes (rank column reads NULL — no FTS rank for a direct lookup).
func scanHitByID(ctx context.Context, db *sql.DB, cols map[string]bool, id int) (MemoryHit, error) {
	now := float64(time.Now().Unix())
	row := db.QueryRowContext(ctx,
		`SELECT d.id, d.topic, d.question, d.answer`+searchExtraSelect(cols)+m1ExamSelect(cols)+
			`, NULL AS rank FROM distilled_qa d WHERE d.id=?`, id)
	return scanHit(row, cols, now, "")
}

// MemoryUpdateFields is the optional-field set for memory_update: empty
// string / nil means "leave unchanged". Tier, when set, must be one of
// hot|warm|cold — the maintenance demotion path for low-quality rows.
type MemoryUpdateFields struct {
	Topic      string
	Question   string
	Answer     string
	Source     string
	Tier       string
	Confidence *float64
	Keywords   []string // non-nil replaces search_kw AND the "keywords:" answer tail
}

// MemoryUpdate edits an existing record (memory_update CLI). At least one
// field must be set. Tier validation is strict: only hot|warm|cold (cold =
// demoted, no longer 1st tier to return). Keywords, when provided, replace
// both the search_kw column and the "keywords: ..." tail of the answer.
func MemoryUpdate(id int, f MemoryUpdateFields) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("memory_update: invalid id %d", id)
	}
	if f.Tier != "" && f.Tier != hotTier && f.Tier != warmTier && f.Tier != coldTier {
		return "", fmt.Errorf("memory_update: invalid tier %q (want hot|warm|cold)", f.Tier)
	}
	if f.Confidence != nil && (*f.Confidence < 0 || *f.Confidence > 1) {
		return "", fmt.Errorf("memory_update: confidence must be in [0,1], got %v", *f.Confidence)
	}
	if f.Topic == "" && f.Question == "" && f.Answer == "" && f.Source == "" &&
		f.Tier == "" && f.Confidence == nil && f.Keywords == nil {
		return "", fmt.Errorf("memory_update: nothing to update (set at least one field)")
	}

	// The same conservative repair as memoryStoreRow: a client that passes the
	// literal escape form of real text (see decodeLiteralEscapes for the shape
	// and the measured payload/prose boundary) gets it decoded before it is
	// written, so the update funnel cannot reintroduce the defect a store
	// would have repaired.
	f.Topic = decodeLiteralEscapes(f.Topic)
	f.Question = decodeLiteralEscapes(f.Question)
	f.Answer = decodeLiteralEscapes(f.Answer)
	for i, k := range f.Keywords {
		f.Keywords[i] = decodeLiteralEscapes(k)
	}

	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	var one int
	err = db.QueryRowContext(ctx, "SELECT 1 FROM distilled_qa WHERE id=?", id).Scan(&one)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("memory_update: no record with id %d", id)
	}
	if err != nil {
		return "", fmt.Errorf("memory_update: %w", err)
	}
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return "", fmt.Errorf("memory_update: %w", err)
	}

	sets := []string{}
	args := []interface{}{}
	if f.Topic != "" {
		sets = append(sets, "topic=?")
		args = append(args, f.Topic)
	}
	if f.Question != "" {
		sets = append(sets, "question=?")
		args = append(args, f.Question)
	}
	if f.Answer != "" || f.Keywords != nil {
		newAnswer := f.Answer
		if newAnswer == "" {
			var cur sql.NullString
			if err := db.QueryRowContext(ctx, "SELECT answer FROM distilled_qa WHERE id=?", id).Scan(&cur); err != nil {
				return "", fmt.Errorf("memory_update: %w", err)
			}
			newAnswer = cur.String
		}
		newAnswer = stripKeywordsTail(newAnswer)
		if kws := cleanKeywords(f.Keywords); len(kws) > 0 {
			newAnswer += "\n\n" + keywordsTailMarker + strings.Join(kws, ", ")
		}
		sets = append(sets, "answer=?")
		args = append(args, newAnswer)
	}
	if f.Source != "" {
		sets = append(sets, "sources=?")
		args = append(args, f.Source)
	}
	if f.Tier != "" {
		sets = append(sets, "tier=?")
		args = append(args, f.Tier)
	}
	if f.Confidence != nil {
		sets = append(sets, "confidence=?")
		args = append(args, *f.Confidence)
	}
	if f.Keywords != nil && cols["search_kw"] {
		sets = append(sets, "search_kw=?")
		args = append(args, searchKwValue(f.Keywords))
	}
	// jieba_token is DERIVED from the retrieval surface, so every update that
	// can change that surface must re-derive it — a stale jieba_token keeps
	// the pre-edit phrasing inscribed in the index (the row would still be
	// recalled by wording the record no longer carries, and would NOT be
	// recalled by newly-written wording: measured 2026-09-22, the seed row's
	// "夜间" was unreachable after an update that added the phrase). The
	// re-derivation always reads the STORED values, so a partial update keeps
	// the columns it does not touch.
	if cols["jieba_token"] && (f.Question != "" || f.Answer != "" || f.Keywords != nil) {
		curTopic, curQuestion, curAnswer, curKw := "", "", "", ""
		var t, q, a, k sql.NullString
		if err := db.QueryRowContext(ctx,
			"SELECT COALESCE(topic,''), COALESCE(question,''), COALESCE(answer,''), COALESCE(search_kw,'') FROM distilled_qa WHERE id=?",
			id).Scan(&t, &q, &a, &k); err != nil {
			return "", fmt.Errorf("memory_update: %w", err)
		}
		curTopic, curQuestion, curAnswer, curKw = t.String, q.String, a.String, k.String
		// Apply the pending field replacements before deriving.
		if f.Topic != "" {
			curTopic = f.Topic
		}
		if f.Question != "" {
			curQuestion = f.Question
		}
		if f.Answer != "" || f.Keywords != nil {
			curAnswer = stripKeywordsTail(curAnswer)
			if kws := cleanKeywords(f.Keywords); len(kws) > 0 {
				curAnswer += "\n\n" + keywordsTailMarker + strings.Join(kws, ", ")
			}
		}
		if f.Keywords != nil {
			curKw = searchKwValue(f.Keywords)
		}
		intent, err := rowIntentColumns(ctx, db, id, cols)
		if err != nil {
			return "", fmt.Errorf("memory_update: %w", err)
		}
		sets = append(sets, "jieba_token=?")
		args = append(args, deriveJiebaToken(curTopic, curQuestion, curAnswer, curKw, intent))
	}
	args = append(args, id)
	if _, err := db.ExecContext(ctx,
		"UPDATE distilled_qa SET "+strings.Join(sets, ", ")+" WHERE id=?", args...); err != nil {
		return "", fmt.Errorf("memory_update: %w", err)
	}

	// FTS sync fallback for foreign DBs without keep-in-sync triggers: the
	// updated row must be re-indexed (external-content INSERT by rowid is an
	// idempotent upsert). gopi's own schema's AFTER UPDATE triggers already
	// did this — skip to avoid double-write edge.
	if !ftsTriggersOwned(ctx, db) {
		if err := syncFTSRow(ctx, db, int64(id)); err != nil {
			_ = err //stylegate:ignore best-effort; the base-row update stands// best-effort; the base-row update stands
		}
	}
	return fmt.Sprintf("Memory updated (id=%d).", id), nil
}

// keywordsTailRe matches the "keywords: ..." tail MemoryStore appends to the
// answer — stripped on keyword replacement so stale breadcrumbs never linger.
var keywordsTailRe = regexp.MustCompile(`(?s)\n\nkeywords: .*$`)

// keywordsTailMarker is the label keywordsTailRe finds — the tail the store
// appends (`answer += "\n\n" + keywordsTailMarker + joined`) and the prefix this
// file strips when it needs the breadcrumbs back. One definition, or the writer
// and the readers drift apart silently (Rule 1b/13).
const keywordsTailMarker = "keywords: "

func stripKeywordsTail(answer string) string {
	return keywordsTailRe.ReplaceAllString(answer, "")
}

// MemoryDelete removes one record and its FTS index entries (memory_delete
// CLI). gopi's own schema's AFTER DELETE triggers clean distilled_qa_fts /
// distilled_qa_tri; a foreign DB without triggers is cleaned explicitly.
func MemoryDelete(id int) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("memory_delete: invalid id %d", id)
	}
	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()

	var one int
	err = db.QueryRowContext(ctx, "SELECT 1 FROM distilled_qa WHERE id=?", id).Scan(&one)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("memory_delete: no record with id %d", id)
	}
	if err != nil {
		return "", fmt.Errorf("memory_delete: %w", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM distilled_qa WHERE id=?", id); err != nil {
		return "", fmt.Errorf("memory_delete: %w", err)
	}
	if !ftsTriggersOwned(ctx, db) {
		_, _ = db.ExecContext(ctx, "DELETE FROM distilled_qa_fts WHERE rowid=?", id) //stylegate:ignore auxiliary DDL/PRAGMA/FTS statement; the primary statement's result is what is checked
		_, _ = db.ExecContext(ctx, "DELETE FROM distilled_qa_tri WHERE rowid=?", id) //stylegate:ignore auxiliary DDL/PRAGMA/FTS statement; the primary statement's result is what is checked
	}
	return fmt.Sprintf("Memory deleted (id=%d).", id), nil
}

// ---------------------------------------------------------------------------
// feedback: memory_feedback (P0 — the explicit feedback loop). The LLM (or
// the CLI maintenance channel) votes useful / useless / unused on a search
// hit; the vote is appended to the memory_feedback event table (audit /
// rollback — never an in-place rewrite of tier/confidence, which stay
// manual CLI decisions) and the materialized Beta counts are updated with
// the 90-day decay. The derived score feeds the search rescore (fbScore
// above) — memory evolution as ranking bias, not truth maintenance (mem0's
// decay lesson).
// ---------------------------------------------------------------------------

// fbMean returns the Beta(useful+prior, useless+prior) posterior mean —
// the small-sample-shrunk useful ratio (0 feedback → 0.5, 1 useful →
// 0.67, 1 useless → 0.33, 10/0 → 0.95).
func fbMean(useful, useless float64) float64 {
	alpha := useful + fbBetaPrior
	beta := useless + fbBetaPrior
	return alpha / (alpha + beta)
}

// fbScore is the feedback ranking term: the posterior mean shifted around
// 0.5, gated by the effective sample count (min(N_eff/4, 1) — fewer than
// ~4 votes cannot move a record far) and scaled by fbWeight (small by
// design). Contradictory votes (A useful + B useless) land back near 0:
// the Beta accumulates both directions, mean → 0.5, and the growing N_eff
// makes the record's *dispute* visible rather than its direction.
func fbScore(useful, useless float64) float64 {
	neff := useful + useless
	if neff <= 0 {
		return 0
	}
	gate := neff / fbGateScale
	if gate > 1 {
		gate = 1
	}
	return (fbMean(useful, useless) - 0.5) * gate * fbWeight
}

// fbDecayFactor is the e^(−Δt/τ) multiplier for the materialized counts —
// time decay (Ebbinghaus-style) so old feedback cannot lock a record.
// Clamped to ≤1 so clock skew can never inflate counts, and sub-second
// gaps are treated as instantaneous (no decay): votes in one burst stack
// exactly, so threshold comparisons (e.g. --suggest-cold's useless ≥ 3)
// are not derailed by float drift.
func fbDecayFactor(elapsed float64) float64 {
	if elapsed <= 1 {
		return 1
	}
	f := math.Exp(-elapsed / fbDecaySeconds)
	if f > 1 {
		return 1
	}
	return f
}

// m2EffectiveConfidence is the M2 lazy confidence lifecycle (no decay job —
// search runs on a query_only connection, so the decay is pure read-side
// math over columns already on the row; deterministic, so re-computing per
// query always agrees with itself):
//
//	eff = clamp( base × e^(−Δt/τ) + reward, confDecayFloor, 1.0 )
//
// Δt ages from the LAST verification instant — max(distilled_at,
// last_retrieved): a retrieval is a re-verification (someone needed this and
// it was still true), so it resets the decay clock. The re-verify reward
// (pro-rated by retrieval_count up to confReverifySaturate) credits
// frequently-re-verified rows, capped so an old workhorse cannot climb
// unboundedly above a healthy new record. An undated row (no distilled_at)
// cannot be aged — no decay without evidence (宁缺勿滥, same policy as
// possiblyStale's unparseable when_at): it keeps its stored confidence.
func m2EffectiveConfidence(baseConfidence float64, distilledAt, lastRetrieved sql.NullFloat64, lastRetrievedPresent bool, retrievalCount float64, now float64) float64 {
	if baseConfidence <= 0 {
		baseConfidence = confDecayFloor
	}
	// The verification instant: last_retrieved only counts when the column
	// exists AND carries a timestamp (0 = never retrieved).
	lastVerified := distilledAt
	if lastRetrievedPresent && lastRetrieved.Valid && lastRetrieved.Float64 > 0 {
		if !lastVerified.Valid || lastRetrieved.Float64 > lastVerified.Float64 {
			lastVerified = lastRetrieved
		}
	}
	eff := baseConfidence
	if lastVerified.Valid && lastVerified.Float64 > 0 && now > lastVerified.Float64 {
		eff = baseConfidence * fbDecayFactor(now-lastVerified.Float64)
	}
	if retrievalCount > 0 {
		reward := confReverifyRewardSaturate * math.Min(1, retrievalCount/confReverifySaturate)
		eff += reward
	}
	if eff < confDecayFloor {
		eff = confDecayFloor
	}
	if eff > 1 {
		eff = 1
	}
	return eff
}

// MemoryFeedback records one explicit feedback vote (memory_feedback tool /
// CLI). rating is one of useful|useless|unused; reason is free text kept
// for the audit trail (not parsed in P0); taskID, when non-empty, is the
// session dedup key — one vote per record per task (the anti-sycophancy /
// anti-stacking gate; the CLI maintenance channel passes "" and is always
// recorded). The write is append-only plus an incremental EMA update of the
// materialized columns in one transaction; nothing here mutates tier or
// confidence (machine only ever moves the soft feedback layer).
func MemoryFeedback(id int, rating, reason, taskID string) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("memory_feedback: invalid id %d", id)
	}
	switch rating {
	case ratingUseful, ratingUseless, ratingUnused:
	default:
		return "", fmt.Errorf("memory_feedback: invalid rating %q (want useful|useless|unused)", rating)
	}
	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	// Heals a pre-feedback DB at its first vote (idempotent; the columns are
	// PRAGMA-probed, missing ones are ALTERed in).
	if err := ensureFeedbackSchema(ctx, db); err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	var one int
	err = db.QueryRowContext(ctx, "SELECT 1 FROM distilled_qa WHERE id=?", id).Scan(&one)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("memory_feedback: no record with id %d", id)
	}
	if err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	if taskID != "" {
		var existing int
		err = db.QueryRowContext(ctx,
			"SELECT 1 FROM memory_feedback WHERE memory_id=? AND task_id=? LIMIT 1", id, taskID).Scan(&existing)
		if err == nil {
			return fmt.Sprintf("memory_feedback: record %d already voted %s in this task — one vote per record per task (ignored)", id, rating), nil
		}
		if err != sql.ErrNoRows {
			return "", fmt.Errorf("memory_feedback: %w", err)
		}
	}
	now := float64(time.Now().Unix())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO memory_feedback (memory_id, rating, reason, task_id, created_at) VALUES (?,?,?,?,?)",
		id, rating, reason, taskID, now); err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	// EMA update with time decay: old masses decay by e^(−Δt/τ), the new
	// vote adds 1 to its side (unused is recorded but neutral).
	var useful, useless, lastAt sql.NullFloat64
	if err := tx.QueryRowContext(ctx,
		"SELECT useful_count, useless_count, fb_last_at FROM distilled_qa WHERE id=?", id).
		Scan(&useful, &useless, &lastAt); err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	factor := 1.0
	if lastAt.Valid && lastAt.Float64 > 0 {
		factor = fbDecayFactor(now - lastAt.Float64)
	}
	u, b := useful.Float64*factor, useless.Float64*factor
	if rating == ratingUseful {
		u += 1
	} else if rating == ratingUseless {
		b += 1
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE distilled_qa SET useful_count=?, useless_count=?, fb_last_at=? WHERE id=?",
		u, b, now, id); err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	// M2 feedback closure: the vote also moves the STORED confidence — the
	// machine's only write to that column (memory_update stays the human
	// channel; FeedbackRecompute intentionally does NOT replay these steps,
	// they are one-shot state transitions, not events). Useless steps 2×
	// down (a trusted-but-contradicted record does active damage); both
	// steps are small enough that one vote cannot reorder search results
	// (confidence weighs 1.0 in the rescore, one token coverage 2.0).
	step := 0.0
	switch rating {
	case ratingUseful:
		step = confVoteStepUseful
	case ratingUseless:
		step = confVoteStepUseless
	}
	coldSuggest := false
	if step != 0 {
		var base sql.NullFloat64
		if err := tx.QueryRowContext(ctx,
			"SELECT confidence FROM distilled_qa WHERE id=?", id).Scan(&base); err != nil {
			return "", fmt.Errorf("memory_feedback: %w", err)
		}
		conf := base.Float64 + step
		if conf < confDecayFloor {
			conf = confDecayFloor
		}
		if conf > 1 {
			conf = 1
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE distilled_qa SET confidence=? WHERE id=?", conf, id); err != nil {
			return "", fmt.Errorf("memory_feedback: %w", err)
		}
		// Cooldown gate: after this vote, enough decayed useless mass with
		// zero useful mass means the record belongs in cold — suggest the
		// manual demotion right in the vote's result text (same only-up
		// contract as FeedbackSuggestCold: the machine proposes, the human
		// or the LLM's next memory_update executes).
		if rating == ratingUseless && u == 0 && b >= fbColdSuggestThreshold {
			coldSuggest = true
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("memory_feedback: %w", err)
	}
	label := ""
	if reason != "" {
		label = " reason=" + strconv.Quote(reason)
	}
	out := fmt.Sprintf("Memory feedback recorded (id=%d, rating=%s%s): useful=%.2f useless=%.2f fb_score=%+.3f",
		id, rating, label, u, b, fbScore(u, b))
	if step != 0 {
		out += fmt.Sprintf(" confidence_step=%+.2f", step)
	}
	if coldSuggest {
		out += fmt.Sprintf(" — COOLDOWN: ≥%.0f decayed useless votes and zero useful; consider `memory_update %d %s` (suggestion only)",
			fbColdSuggestThreshold, id, ColdDemotionHint)
	}
	return out, nil
}

// FeedbackRecompute rebuilds the materialized useful/useless columns from
// the append-only event table — the feedback layer is a pure function of
// its events, so decay/prior tuning or a corrupted materialization is a
// one-command rebuild (rollback of the machine-moved layer, always).
func FeedbackRecompute() (string, error) {
	ctx := context.Background()
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := ensureFeedbackSchema(ctx, db); err != nil {
		return "", fmt.Errorf("memory_feedback --recompute: %w", err)
	}
	// Zero every materialized row first (a recompute must converge to the
	// event history, whatever the columns held before).
	if _, err := db.ExecContext(ctx,
		"UPDATE distilled_qa SET useful_count=0, useless_count=0, fb_last_at=0"); err != nil {
		return "", fmt.Errorf("memory_feedback --recompute: %w", err)
	}
	rows, err := db.QueryContext(ctx,
		"SELECT memory_id, rating, created_at FROM memory_feedback ORDER BY memory_id, created_at")
	if err != nil {
		return "", fmt.Errorf("memory_feedback --recompute: %w", err)
	}
	type agg struct{ u, b, last float64 }
	groups := map[int]*agg{}
	for rows.Next() {
		var mid int
		var rating string
		var at float64
		if err := rows.Scan(&mid, &rating, &at); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return "", fmt.Errorf("memory_feedback --recompute: %w", err)
		}
		g, ok := groups[mid]
		if !ok {
			g = &agg{}
			groups[mid] = g
		}
		if g.last > 0 {
			f := fbDecayFactor(at - g.last)
			g.u, g.b = g.u*f, g.b*f
		}
		switch rating {
		case ratingUseful:
			g.u += 1
		case ratingUseless:
			g.b += 1
		}
		g.last = at
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("memory_feedback --recompute: %w", err)
	}
	for mid, g := range groups {
		if _, err := db.ExecContext(ctx,
			"UPDATE distilled_qa SET useful_count=?, useless_count=?, fb_last_at=? WHERE id=?",
			g.u, g.b, g.last, mid); err != nil {
			return "", fmt.Errorf("memory_feedback --recompute: %w", err)
		}
	}
	return fmt.Sprintf("Memory feedback recomputed from %d records' event history.", len(groups)), nil
}

// FeedbackHistory renders one record's full feedback audit trail + the
// derived Beta stats — the memory_feedback --list inspection step.
func FeedbackHistory(memoryID int) (string, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return "", fmt.Errorf("memory_feedback --list: %w", err)
	}
	if !cols["useful_count"] {
		return "No feedback recorded (schema has no feedback columns).", nil
	}
	if memoryID > 0 {
		var one int
		err = db.QueryRowContext(ctx, "SELECT 1 FROM distilled_qa WHERE id=?", memoryID).Scan(&one)
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("memory_feedback --list: no record with id %d", memoryID)
		}
	}
	query := "SELECT d.id, d.topic, d.useful_count, d.useless_count FROM distilled_qa d"
	var args []interface{}
	if memoryID > 0 {
		query += " WHERE d.id=?"
		args = append(args, memoryID)
	} else {
		query += " WHERE d.useful_count+d.useless_count>0 ORDER BY d.useless_count+d.useful_count DESC LIMIT 50"
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("memory_feedback --list: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id int
		var topic string
		var u, b float64
		if err := rows.Scan(&id, &topic, &u, &b); err != nil {
			return "", fmt.Errorf("memory_feedback --list: %w", err)
		}
		out = append(out, fmt.Sprintf("#%d %s useful=%.2f useless=%.2f fb=%.2f(%d)",
			id, topic, u, b, fbMean(u, b), int(b+u+0.5)))
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("memory_feedback --list: %w", err)
	}
	if len(out) == 0 {
		return "No feedback recorded yet.", nil
	}
	return strings.Join(out, "\n"), nil
}

// FeedbackSuggestCold lists the cold-demotion candidates — the "only-up,
// never auto-down" enforcement of the merged design: the machine never
// demotes a record itself (a wrong demotion is invisible once the row stops
// surfacing; a wrong upgrade heals via more feedback). The gate is strict:
// ≥3 (decayed) useless votes AND zero useful — and the output is a
// suggestion, execution stays a manual `memory_update <id> --tier cold`.
func FeedbackSuggestCold() (string, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return "", fmt.Errorf("memory_feedback --suggest-cold: %w", err)
	}
	if !cols["useless_count"] {
		return "No feedback recorded (schema has no feedback columns).", nil
	}
	rows, err := db.QueryContext(ctx, `SELECT d.id, d.topic, d.question, d.useful_count, d.useless_count,
		(SELECT f.reason FROM memory_feedback f WHERE f.memory_id = d.id AND f.rating = 'useless'
		 ORDER BY f.created_at DESC LIMIT 1) AS last_reason
		FROM distilled_qa d
		WHERE d.useful_count = 0 AND d.useless_count >= ? AND d.tier != 'cold'
		ORDER BY d.useless_count DESC`, fbColdSuggestThreshold)
	if err != nil {
		return "", fmt.Errorf("memory_feedback --suggest-cold: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id int
		var topic, question string
		var u, b float64
		var reason sql.NullString
		if err := rows.Scan(&id, &topic, &question, &u, &b, &reason); err != nil {
			return "", fmt.Errorf("memory_feedback --suggest-cold: %w", err)
		}
		q := question
		r := []rune(q)
		if len(r) > 60 {
			q = string(r[:60]) + "…"
		}
		rline := ""
		if reason.Valid && reason.String != "" {
			rline = "  reason: " + strconv.Quote(reason.String)
		}
		out = append(out, fmt.Sprintf("#%d %s useless=%.1f useful=%.1f  Q: %s%s\n  → gopi memory_update %d %s",
			id, topic, b, u, q, rline, id, ColdDemotionHint))
	}
	if len(out) == 0 {
		return "No cold-demotion candidates (need useless ≥ 3 and useful = 0).", nil
	}
	return strings.Join(out, "\n"), nil
}

// ---------------------------------------------------------------------------
// result rendering (contract unchanged from the CLI era).
// ---------------------------------------------------------------------------

// renderMemorySearchResult turns the engine's JSON into the tool result the
// LLM sees; the result's FIRST line is the now= anchor every age= on it is
// measured against (renderMemorySearchResultAt is the same renderer with the
// clock injected — the seam a test pins the arithmetic on). Valid outputs per
// the contract: a JSON array of MemoryHit
// (rendered as compact records with the value signals intact) or
// {"empty": true, "raw_hint": true} (rendered as the no-distillation hint).
// Single-line and multi-line JSON are both accepted — parsing, not string
// inspection. Anything else is an error: the contract is JSON.
func renderMemorySearchResult(out string) (string, error) {
	return renderMemorySearchResultAt(out, time.Now())
}

// renderMemorySearchResultAt is the renderer with an INJECTED clock: the
// result's first line is the now= anchor and every hit's age is measured
// against it, so a test can pin the anchor without a wall clock (and a caller
// can render an archived hit at the instant it was retrieved).
func renderMemorySearchResultAt(out string, now time.Time) (string, error) {
	if out == "" {
		return "", fmt.Errorf("memory_search: empty output from memory engine")
	}

	var empty MemoryEmpty
	if err := json.Unmarshal([]byte(out), &empty); err == nil && empty.Empty {
		return memoryEmptyHint, nil
	}

	var hits []MemoryHit
	if err := json.Unmarshal([]byte(out), &hits); err != nil {
		return "", fmt.Errorf("memory_search: unparseable memory engine output: %w", err)
	}
	if len(hits) == 0 {
		return memoryEmptyHint, nil
	}

	rendered := make([]string, 0, len(hits))
	for _, h := range hits {
		meta := fmt.Sprintf("#%d %s tier=%s conf=%.2f lesson=%s %s",
			h.ID, h.Topic, h.Tier, h.Confidence, h.LessonType, renderAgeSegment(h, now))
		// M2: the effective confidence (after decay + re-verify reward) is
		// what the ranking actually used — shown when it diverges from the
		// stored value, so the LLM sees the trust the score reflects.
		if h.EffConfidence > 0 && math.Abs(h.EffConfidence-h.Confidence) >= effConfRenderDelta {
			meta += fmt.Sprintf(" eff_conf=%.2f", h.EffConfidence)
		}
		if h.HotStale {
			meta += " hot_stale=true"
		}
		if len(h.Sources) > 0 {
			meta += " sources=" + strings.Join(h.Sources, ",")
		}
		if h.PossiblyStale {
			meta += " possibly_stale=true"
		}
		if h.IntentHits > 0 {
			meta += fmt.Sprintf(" intent_hits=%d", h.IntentHits)
		}
		if h.Tokens > 0 {
			meta += fmt.Sprintf(" tokens=%d", h.Tokens)
		}
		if h.FbUseful+h.FbUseless > 0 {
			// fb=<Beta posterior mean>(<decayed votes>) — the hit's feedback
			// state, rendered so the LLM can judge it transparently.
			meta += fmt.Sprintf(" fb=%.2f(%d)", fbMean(h.FbUseful, h.FbUseless), int(h.FbUseful+h.FbUseless+0.5))
		}
		// The provenance pointer, when the row recorded one (memory_origin.go).
		// Omitted entirely for a row that recorded nothing, so the pre-feature
		// render contract is byte-identical there.
		if seg := originRenderSegment(h.MemoryOriginOf()); seg != "" {
			meta += " " + seg
		}
		rendered = append(rendered, meta+"\nQ: "+h.Question+"\nA: "+h.Answer+originLocalOnlyRender(h.MemoryOriginWire(), OriginHostName()))
	}
	// The now= anchor: without it an age is a number the reader cannot check,
	// and "14d old" reads identically whether the row was written in a week of
	// this run's work or a year ago. First line, RFC3339, the renderer's OWN
	// clock — the same instant every stored=/age= pair below was measured
	// against, so the arithmetic in one result is internally consistent.
	return memoryNowAnchorPrefix + now.Format(time.RFC3339) + "\n" + strings.Join(rendered, "\n---\n"), nil
}

// renderAgeSegment renders the two INDEPENDENT time facts a hit carries, the
// split the single when= field used to conflate:
//
//	stored=<local time> age=<Nd>   when the row was WRITTEN (distilled_at,
//	                               falling back to when_at on a legacy row
//	                               that predates the column — the same
//	                               instant the old when= showed)
//	about=<YYYY-MM-DD>             a calendar date the row's own TEXT states
//	                               (omitted when it states none)
//
// Both are printed when both are known: a row describing a 2026-08-01 incident
// and written today is exactly the case the old field could not express.
func renderAgeSegment(h MemoryHit, now time.Time) string {
	stored, ok := storedInstantOf(h)
	if !ok {
		// Nothing to anchor: no distilled_at and an unparseable/empty when_at.
		// The absence is stated rather than a fabricated date (Rule 15).
		seg := memoryStoredUnknownMarker
		if about := statedDateIn(h.Topic, h.Question, h.Answer, now); about != "" {
			seg += " " + memoryAboutPrefix + about
		}
		return seg
	}
	age := now.Sub(stored)
	if age < 0 {
		// A stored instant in the future is not an age: clamp to 0d rather than
		// print a negative day count (the store's own future-date guard keeps
		// this from happening; the renderer must not invent a number if it does).
		age = 0
	}
	seg := fmt.Sprintf("%s%s %s%d%s", memoryStoredPrefix, stored.Local().Format(whenAtDateTimeLayout),
		memoryAgePrefix, int(age/(secondsPerDay*time.Second)), memoryAgeUnit)
	if about := statedDateIn(h.Topic, h.Question, h.Answer, now); about != "" {
		seg += " " + memoryAboutPrefix + about
	}
	return seg
}

// storedInstantOf resolves the WRITE instant of a hit: distilled_at when the
// row carries it (every row written since the column existed), else the
// when_at marker (the legacy fallback — the old when= value, unchanged for
// those rows). ok=false when neither parses.
func storedInstantOf(h MemoryHit) (time.Time, bool) {
	if h.DistilledAt > 0 {
		return time.Unix(int64(h.DistilledAt), 0), true
	}
	if ts := parseTimeMarker(h.WhenAt); ts != nil {
		return time.Unix(int64(*ts), 0), true
	}
	return time.Time{}, false
}

// statedDateIn is the ONE matcher both readers share (Rule 14): deriveWhenAt
// applies it to a row being stored, renderAgeSegment applies it to a row being
// rendered. clock injected, so neither caller depends on the wall clock.
func statedDateIn(topic, question, answer string, now time.Time) string {
	hay := question + " " + answer
	if topic != "" {
		hay = topic + " " + hay
	}
	m := whenAtDateRe.FindString(hay)
	if m == "" {
		return ""
	}
	norm := strings.ReplaceAll(m, "/", "-")
	if ts := parseTimeMarker(norm); ts == nil || *ts > float64(now.Unix()) {
		return ""
	}
	return norm
}

// MemoryHotResident renders the compact hot-tier digest injected at task
// start — the M2 "hot = 常驻候选" semantics: hot records are existence
// hints the LLM sees without searching (the search itself stays value-gated
// and cold-excluding as before). Read-only, best-effort: any error returns
// "" so the injection can never block a task start. At most
// hotResidentInjectLimit rows (recent re-verification first, then
// confidence), truncated to hotResidentMaxChars.
func MemoryHotResident() string {
	ctx, cancel := context.WithTimeout(context.Background(), hotResidentTimeout)
	defer cancel()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return ""
	}
	defer db.Close()
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return ""
	}
	if !cols["tier"] {
		return ""
	}
	query := `SELECT id, topic, question, tier, confidence, last_retrieved
		FROM distilled_qa WHERE tier='` + hotTier + `'
		ORDER BY (last_retrieved IS NULL), last_retrieved DESC, confidence DESC
		LIMIT ` + strconv.Itoa(hotResidentInjectLimit)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return ""
	}
	defer rows.Close()
	now := float64(time.Now().Unix())
	var lines []string
	used := 0
	for rows.Next() {
		var id int
		var topic, question, tier string
		var conf, lastRet sql.NullFloat64
		if err := rows.Scan(&id, &topic, &question, &tier, &conf, &lastRet); err != nil {
			return ""
		}
		stale := hotRecordStale(nullableFloatPtr(lastRet), cols["last_retrieved"], "", now)
		q := question
		if r := []rune(q); len(r) > hotResidentQuestionRunes {
			q = string(r[:hotResidentQuestionRunes]) + "…"
		}
		suffix := ""
		if stale {
			suffix = fmt.Sprintf(" | hot_stale (unverified >%dd — consider memory_update --tier warm/cold)", hotStaleDays)
		}
		line := fmt.Sprintf("#%d %s | conf=%.2f | %s%s", id, topic, conf.Float64, q, suffix)
		if used+len(line)+1 > hotResidentMaxChars {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	if err := rows.Err(); err != nil {
		return ""
	}
	return strings.Join(lines, "\n")
}

// hotResidentTimeout bounds the resident-digest read; hotResidentQuestionRunes
// truncates each question so the digest stays a hint, not a dump.
const (
	hotResidentTimeout       = 2 * time.Second
	hotResidentQuestionRunes = 80
)

// MemoryListRecent renders a compact list of the N most recent distilled
// records (id / tier / topic) — the shared engine behind `gopi memory list`,
// reusable by any surface. Read-only: no
// ranking, no feedback mutation, plain recent-rows query. The rendered
// table mirrors renderMemorySearchResult's meta style so the two surfaces
// read alike.
func MemoryListRecent(n int) (string, error) {
	if n <= 0 {
		n = 20
	}
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT id, topic, tier, question FROM distilled_qa ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return "", fmt.Errorf("memory_list: %w", err)
	}
	defer rows.Close()
	var b strings.Builder
	count := 0
	for rows.Next() {
		var id int
		var topic, tier, question string
		if err := rows.Scan(&id, &topic, &tier, &question); err != nil {
			return "", fmt.Errorf("memory_list: %w", err)
		}
		fmt.Fprintf(&b, "#%d [%s] %s\n    Q: %s\n", id, tier, topic, oneLineShort(question, 90))
		count++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("memory_list: %w", err)
	}
	if count == 0 {
		return "knowledge base is empty", nil
	}
	return fmt.Sprintf("%d most recent record(s):\n%s(details: memory_get <id>)", count, b.String()), nil
}

// oneLineShort collapses s to a single short line (list rendering helper).
func oneLineShort(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// ColdDemotionHint is the flag spelling the engine's own suggestions name as
// the executor of a cold demotion (`memory_update <id> --tier cold`, rendered
// by both the vote result and --suggest-cold). It is a shared constant, not a
// literal, so the model-facing text and the drift gate read the SAME spelling
// the engine prints (Rule 13).
const ColdDemotionHint = "--tier cold"
