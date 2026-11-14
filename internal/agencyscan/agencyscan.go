// Package agencyscan is the agency-LEVEL-3 improvement-proposal pass: it
// mines gopi's OWN history for recurring patterns whose threshold criteria
// are met and proposes ONE improvement task per candidate — never a fix,
// never a dispatch, never autonomous work.
//
// Design contract (epic agency-proposals, devdoc/agency-proposals.md):
//
//	reuse       — the tasks ledger (memory.db) IS the edge store; nothing new
//	              is persisted, no daemon, no new tool.
//	quiet       — pure ledger reads + (when enabled) a few TaskSubmit INSERTs.
//	              Zero attention cost: nothing is added to the main-loop prompt.
//	off by default — the caller (the task-end hook) gates on an env switch;
//	              disabled = this package is never entered.
//	dedupe      — a candidate is proposed at most ONCE per
//	              RecurrenceKeyFingerprint window, in ANY row status (live or
//	              terminal) — the merge-backlog branch-scan lesson.
//	no invented numbers — every threshold in the const block below is justified
//	              by a stated distribution over the real ledger; see the devdoc.
package agencyscan

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/nickhuang99/gopi/internal/tasksource"
)

// ---------------------------------------------------------------------------
// Thresholds — each one is a named constant with a measured distribution.
// ---------------------------------------------------------------------------

const (
	// minTaskFamilySize is C1's repeat count: a task family must recur at
	// least this many times before it is a PATTERN rather than an incident.
	//
	// Distribution (measured 2026-09-11 over the 969-row ledger): of 433
	// recurrence-key families, 112 reach n>=2, 55 n>=3, 36 n>=4, 26 n>=5.
	// 55 candidate families is too many for a "conservative" pass; n>=5
	// keeps 26 — the size where a family is a standing workflow, not a
	// one-off repeat.
	minTaskFamilySize = 5

	// MinTaskFamilySize is minTaskFamilySize's exported mirror: the end
	// hook resolves the operator knob (GOPI_AGENCY_MIN_FAMILY) AGAINST this
	// value, so the measured constant stays the default and the single place
	// the number is justified.
	MinTaskFamilySize = minTaskFamilySize

	// maxFailureRate is C1's tightness gate: the family's failure share must
	// be below this (a healthy workflow needs no improvement proposal).
	// Families above it are the ones worth a look.
	//
	// Distribution: whole-ledger failure rate is 55/969 = 5.7%; the tightest
	// failing families measured 100% (6/6 and 5/5). A 50% gate therefore
	// catches every observed failing family and lets the healthy 95% through.
	maxFailureRate = 0.5

	// minFailedInFamily is the smallest failing count that can justify a
	// "recurring failure" claim. One failure in a 5-row family is noise
	// (transient network/API), not a pattern.
	minFailedInFamily = 2

	// duplicateGroupMinSize is C2's repeat count: how many near-identical
	// knowledge rows make the store redundant enough to propose a
	// consolidation pass.
	//
	// Distribution: of 1782 distinct normalized questions in the 1814-row
	// distilled_qa table, the size histogram is 1:1766, 2:10, 3:2, 4:2, 5:1,
	// 9:1 — i.e. 6 groups reach n>=3. n>=3 is where the histogram stops
	// being a long tail of coincidences.
	duplicateGroupMinSize = 3

	// RecurrenceKeyFingerprint is the dedupe window: how many leading bytes
	// of the candidate's SHA-256 fingerprint go into its dedupe prefix.
	//
	// This is a collision budget, not a tuned parameter: with a 4-byte (8
	// hex char) prefix, among the ~30 candidates a full corpus scan can
	// produce the birthday collision probability is ~30^2/2^33 ≈ 1e-7 —
	// deliberately generous so a truncated digest can never silently merge
	// two distinct patterns.
	RecurrenceKeyFingerprint = 4
)

// proposalDedupeTagPrefix names the dedupe fence a proposal row's payload
// carries. It must never equal the fingerprint itself, or a row's own tag
// would satisfy its own dedupe predicate on the next pass.
const proposalDedupeTagPrefix = "candidate:"

// RecurrenceKeyFingerprintMarker is the human-readable marker that precedes
// the fence in a rendered payload — exported so the replay harness can assert
// a proposal row really carries the fence it will be deduped by (asserting the
// prefix alone would pass on any unrelated payload text).
const RecurrenceKeyFingerprintMarker = "Dedupe fence (do not edit): "

// Candidate kinds. Each kind has its own detection predicate and its own
// rendering of the proposal briefing.
const (
	// KindRecurringFailure is C1: a task family that keeps repeating and
	// whose failure share is above maxFailureRate.
	KindRecurringFailure = "recurring-failure"
	// KindDuplicateKnowledge is C2: one normalized question is answered by
	// duplicateGroupMinSize or more near-identical knowledge rows.
	KindDuplicateKnowledge = "duplicate-knowledge"
)

// Tags. Rule 1b/14: every user-visible string that carries meaning is a
// named constant. These are this package's own vocabulary (the ledger's tag
// column is a free-form CSV), so the constants live here.
const (
	// TagImprovement is the family tag every proposal row carries.
	TagImprovement = "improvement"
	// TagAgencyProposal is the proposal-provenance tag (what dedupe counts).
	TagAgencyProposal = "agency-proposal"
	// TagQuiet marks a row that NO automatic claimer may take: a proposal is
	// addressed to the operator, and the queue's existing skip-tag mechanism
	// is what keeps this pass from becoming an auto-dispatch machine. Its
	// VALUE must match the queue's existing quarantine tag — the worker
	// excludes it by referencing THIS constant (one definition, Rule 14).
	TagQuiet = "quiet"
	// AutoTaggerSource is the source stamp: an automatic mining pass, not a
	// human and not the LLM tool.
	//
	// The VALUE lives in internal/tasksource (Rule 14: one home for the
	// `source` vocabulary), and this package references it rather than
	// re-typing it. It used to be the bare "auto", the retired conflation
	// whose Normalize mapping is `merger` — so every proposal this pass filed
	// reached `task show` narrated as a MERGE-PATH machine enqueue, a role no
	// merge ever played for it. The miner is a different writer with a
	// different meaning, and tasksource.AgencyProposal says which.
	AutoTaggerSource = tasksource.AgencyProposal
)

// candidate is one mined pattern that met its kind's threshold, together
// with the raw evidence the proposal carries.
type candidate struct {
	Kind          string
	RecurrenceKey string
	Family        []string // evidence rows, newest first (display order)
	Count         int
	FailedCount   int
	StatusCounts  map[string]int
	Surplus       int // C2 only: rows beyond the first, i.e. the reclaimable set
}

// ScanResult is the pass's tally — mirrors the ledger sweeps' counts struct
// so the hook can log a one-liner.
type ScanResult struct {
	// Scanned is the number of rows examined (the denominator of every
	// ratio the pass reports).
	Scanned int
	// Candidates is every pattern that met its kind's threshold.
	Candidates []candidate
	// Proposed counts rows actually submitted (0 when a candidate was
	// deduped or its rendered summary was not a usable task headline).
	Proposed int
	// Deduped counts candidates skipped because a prior pass already
	// proposed them.
	Deduped int
	// Dropped counts candidates whose rendering was refused (an over-long
	// or empty headline from a CJK-dense summary). Recorded, not silent —
	// a first pass over a Chinese-heavy corpus drops a measurable share.
	Dropped int
	// FamilySizes is the task-family size histogram (size → number of
	// families), i.e. the raw distribution the size gate is derived from.
	// It ships in every report so an operator can re-check the gate against
	// a moving ledger instead of trusting a constant quoted in a doc.
	FamilySizes map[int]int
}

// Options parameterises a pass. The zero value is the conservative default
// (report-only: every candidate is mined, nothing is written).
type Options struct {
	// DBPath overrides the ledger ("" = memoryDBPath()).
	DBPath string
	// MinFamilySize overrides minTaskFamilySize (0 = the named constant).
	// The end hook passes the operator knob (config.AgencyMinFamilySize)
	// resolved against the constant — the constant stays the default and the
	// single place the number is justified (Rule 1).
	MinFamilySize int
	// Submit true enqueues one proposal row per surviving candidate;
	// false is the report-only replay face (used by the devdoc's evidence
	// pass and by tests). Replay ALWAYS runs with Submit=false.
	Submit bool
}

// ---------------------------------------------------------------------------
// Normalization
// ---------------------------------------------------------------------------

var (
	uuidRE   = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	hexRE    = regexp.MustCompile(`\b[0-9a-f]{7,}\b`)
	taskIDRE = regexp.MustCompile(`t-\d+`)
	digitsRE = regexp.MustCompile(`\d+`)
	runRE    = regexp.MustCompile(`\s+`)
)

// normalize folds one headline into its recurrence form: lowercase, volatile
// tokens (uuids, hashes, task ids, numbers, punctuation) replaced by stable
// placeholders. Two rows that differ only in a timestamp or an id fold to
// the same key — that is exactly the recurrence the pass is looking for.
func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "[auto]", " ")
	s = strings.ReplaceAll(s, "[code-review]", " ")
	s = uuidRE.ReplaceAllString(s, "<id>")
	s = taskIDRE.ReplaceAllString(s, "<tid>")
	s = hexRE.ReplaceAllString(s, "<h>")
	s = digitsRE.ReplaceAllString(s, "<n>")
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '<' || r == '>' {
			return r
		}
		return ' '
	}, s)
	return strings.TrimSpace(runRE.ReplaceAllString(s, " "))
}

// recurrenceKey is the pattern identity of one ledger row: its tag signature
// (provenance tags stripped — they identify the RUNNER, never the pattern)
// plus the first recurrenceKeyTokens normalized headline tokens.
//
// The distribution that justifies the shape is in the devdoc: over the real
// ledger a 3-token key yields 433 families (321 singletons — the long tail of
// one-off briefings), while a 1-token key collapses distinct workflows into a
// single bucket.
const recurrenceKeyTokens = 3

// recurrenceKeyOf renders the key for one row.
func recurrenceKeyOf(summary, tags string) string {
	toks := strings.Fields(normalize(summary))
	if len(toks) > recurrenceKeyTokens {
		toks = toks[:recurrenceKeyTokens]
	}
	return strings.Join(tagSignature(tags), ",") + "|" + strings.Join(toks, " ")
}

// fingerprint is the candidate's stable identity across passes.
func fingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// dedupeFence is the payload marker a proposal row carries so the NEXT pass
// can recognise it. It is deliberately NOT the raw fingerprint: a row whose
// payload contained its own fingerprint would satisfy its own dedupe probe
// and mask a real re-proposal forever.
func dedupeFence(fp string) string {
	n := RecurrenceKeyFingerprint * 2
	if n > len(fp) {
		n = len(fp)
	}
	return proposalDedupeTagPrefix + fp[:n]
}

// tagSignature returns the sorted, provenance-stripped tag list of one row.
// worker:<host> / from-<peer> / wrapper:<task> are RUNNER provenance (the
// same row scanned from two nodes must fold to one pattern), while
// auto-spawn / merge-conflict / merge-backlog are automation-owned rows whose
// "recurrence" is the dispatcher's own doing, not a behaviour pattern.
func tagSignature(tags string) []string {
	var out []string
	for _, t := range strings.Split(tags, ",") {
		t = strings.TrimSpace(t)
		if t == "" || isProvenanceTag(t) || isAutomationTag(t) {
			continue
		}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func isProvenanceTag(t string) bool {
	switch {
	case strings.HasPrefix(t, "worker:"), strings.HasPrefix(t, "from-"), strings.HasPrefix(t, "wrapper:"):
		return true
	case strings.HasPrefix(t, "node:"): // memory-sync origin tag (SyncNodeTagPrefix)
		return true
	}
	return false
}

func isAutomationTag(t string) bool {
	switch t {
	case "auto-spawn", "auto-dispatch", "merge-backlog", "sync-yielded", "sync-resolved":
		return true
	}
	return false
}

// isDigestSelfReport reports whether a summary is the task-end hook's own
// stats digest rather than a real briefing. Rule 13: the marker is a named
// constant (mirror of internal/agent's autoTaskStatsFormat, which this leaf
// package must not import).
const digestSelfReportMarker = "\u2014 elapsed="

func isDigestSelfReport(summary string) bool {
	return strings.Contains(summary, digestSelfReportMarker)
}

// ---------------------------------------------------------------------------
// Mining
// ---------------------------------------------------------------------------

// terminalStatuses are the ledger states a settled row can hold. A candidate
// family must be fully settled before it can be judged a pattern: rows still
// in flight are evidence that the family is being worked on RIGHT NOW.
//
// MIRROR, and it is a deliberate one (Rule 14 tension, stated): this leaf
// package imports nothing from internal/tools (that import would drag the
// ledger layer into the mining pass for one string), so the vocabulary is
// spelled here and the mirror test below pins it against drift.
// `superseded` joined 2026-10-07 with the parent-release status: the row's
// work is DONE ELSEWHERE, which is settled, not in flight.
var terminalStatuses = map[string]bool{
	"completed": true, "failed": true, "cancelled": true, "verified": true,
	"superseded": true,
}

// Scan mines the ledger and (when opts.Submit) proposes the surviving
// candidates. It is the pass's single entry point. A missing/foreign ledger
// degrades to an empty result — the hook's fail-soft contract.
func Scan(ctx context.Context, opts Options) (ScanResult, error) {
	res := ScanResult{FamilySizes: map[int]int{}}
	dbPath := opts.DBPath
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	db, err := openLedgerReadOnlyContext(ctx, dbPath)
	if err != nil {
		return res, fmt.Errorf("agencyscan: open ledger: %w", err)
	}
	defer db.Close() //stylegate:ignore read-only handle closed at pass end; the pass is bounded

	minSize := opts.MinFamilySize
	if minSize <= 0 {
		minSize = minTaskFamilySize
	}
	taskCands, err := mineTaskFamilies(ctx, db, &res, minSize)
	if err != nil {
		return res, err
	}
	knowCands, err := mineDuplicateKnowledge(ctx, db, &res)
	if err != nil {
		return res, err
	}
	res.Candidates = append(taskCands, knowCands...)
	sort.Slice(res.Candidates, func(i, j int) bool {
		if res.Candidates[i].Kind != res.Candidates[j].Kind {
			return res.Candidates[i].Kind < res.Candidates[j].Kind
		}
		return res.Candidates[i].RecurrenceKey < res.Candidates[j].RecurrenceKey
	})
	if !opts.Submit {
		return res, nil
	}
	for _, c := range res.Candidates {
		summary, payload, ok := renderSubmission(c)
		if !ok {
			res.Dropped++
			continue
		}
		taken, err := proposalExists(ctx, db, c)
		if err != nil {
			return res, fmt.Errorf("agencyscan: dedupe probe: %w", err)
		}
		if taken {
			res.Deduped++
			continue
		}
		if err := submitProposal(summary, payload, c); err != nil {
			// A single refused submit must not abort the pass: the remaining
			// candidates still deserve their chance (fail-soft, recorded).
			fmt.Fprintf(scanStderr, "[agency] proposal for %s refused: %v\n", c.Kind, err)
			res.Dropped++
			continue
		}
		res.Proposed++
	}
	return res, nil
}

// ledgerRow is one task row reduced to the fields the miner needs.
type ledgerRow struct {
	summary string
	tags    string
	status  string
}

// mineTaskFamilies is C1: recurring task families whose failure share is
// above maxFailureRate.
func mineTaskFamilies(ctx context.Context, db *sql.DB, res *ScanResult, minSize int) ([]candidate, error) {
	rows, err := db.QueryContext(ctx, `SELECT summary, tags, status FROM tasks`)
	if err != nil {
		return nil, fmt.Errorf("agencyscan: query tasks: %w", err)
	}
	defer rows.Close() //stylegate:ignore result set closed at scan end; the pass is bounded

	families := map[string][]ledgerRow{}
	for rows.Next() {
		var summary, tags, status sql.NullString
		if err := rows.Scan(&summary, &tags, &status); err != nil {
			return nil, fmt.Errorf("agencyscan: scan task row: %w", err)
		}
		res.Scanned++
		// Harness self-reports ([auto] … — elapsed=… tokens=…) are the
		// task-end hook's own bookkeeping rows, not briefings. They recur by
		// construction and would drown every real pattern (measured: 239 of
		// 969 rows).
		if isDigestSelfReport(summary.String) {
			continue
		}
		key := recurrenceKeyOf(summary.String, tags.String)
		if key == "|" || strings.HasSuffix(key, "|") {
			continue // no usable headline tokens — not a pattern identity
		}
		families[key] = append(families[key], ledgerRow{
			summary: summary.String, tags: tags.String, status: status.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agencyscan: iterate tasks: %w", err)
	}

	var out []candidate
	for key, fam := range families {
		res.FamilySizes[len(fam)]++
		if len(fam) < minSize {
			continue
		}
		counts := map[string]int{}
		settled := true
		var evidence []string
		for _, r := range fam {
			counts[r.status]++
			if !terminalStatuses[r.status] {
				settled = false
			}
			evidence = append(evidence, fmt.Sprintf("%s [%s]", r.status, strings.TrimSpace(r.summary)))
		}
		// An in-flight membership means the family is being worked on right
		// now — proposing an improvement for it would fight the live work.
		if !settled {
			continue
		}
		failed := counts["failed"]
		if failed < minFailedInFamily {
			continue
		}
		if float64(failed)/float64(len(fam)) <= maxFailureRate {
			continue
		}
		sort.Strings(evidence)
		out = append(out, candidate{
			Kind:          KindRecurringFailure,
			RecurrenceKey: key,
			Count:         len(fam),
			FailedCount:   failed,
			StatusCounts:  counts,
			Family:        evidence,
		})
	}
	return out, nil
}

// mineDuplicateKnowledge is C2: one normalized question answered by
// duplicateGroupMinSize or more knowledge rows.
func mineDuplicateKnowledge(ctx context.Context, db *sql.DB, res *ScanResult) ([]candidate, error) {
	ok, err := tablePresent(ctx, db, "distilled_qa")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // a task-only ledger is legitimate (tests, fresh homes)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, question, retrieval_count FROM distilled_qa`)
	if err != nil {
		return nil, fmt.Errorf("agencyscan: query knowledge rows: %w", err)
	}
	defer rows.Close() //stylegate:ignore result set closed at scan end; the pass is bounded

	type knowRow struct {
		id      int64
		asked   string
		queried int
	}
	groups := map[string][]knowRow{}
	for rows.Next() {
		var id int64
		var question sql.NullString
		var rc sql.NullInt64
		if err := rows.Scan(&id, &question, &rc); err != nil {
			return nil, fmt.Errorf("agencyscan: scan knowledge row: %w", err)
		}
		res.Scanned++
		key := normalize(question.String)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], knowRow{id: id, asked: question.String, queried: int(rc.Int64)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agencyscan: iterate knowledge: %w", err)
	}

	var out []candidate
	for key, g := range groups {
		if len(g) < duplicateGroupMinSize {
			continue
		}
		// Evidence order: most-retrieved first — the canonical row the other
		// copies should fold into.
		sort.Slice(g, func(i, j int) bool { return g[i].queried > g[j].queried })
		var evidence []string
		for _, r := range g {
			evidence = append(evidence, fmt.Sprintf("#%d(rc=%d) %s", r.id, r.queried, oneLine(r.asked)))
		}
		out = append(out, candidate{
			Kind:          KindDuplicateKnowledge,
			RecurrenceKey: "knowledge|" + key,
			Count:         len(g),
			Surplus:       len(g) - 1,
			Family:        evidence,
		})
	}
	return out, nil
}

// tablePresent reports whether the ledger carries a named table (a
// task-only fixture DB has no knowledge base).
func tablePresent(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("agencyscan: probe table %s: %w", name, err)
	}
	return n > 0, nil
}

// proposalExists is the dedupe gate: has ANY row (in any status — live or
// terminal) already been proposed for this candidate's fingerprint?
//
// This is deliberately the OPPOSITE of the merge-backlog scan's original
// bug: that scan's coverage mask counted only LIVE rows, so a decision row
// that reached a terminal state re-armed the branch and every tick minted a
// byte-identical row (~600 duplicates). A proposal row that is cancelled or
// completed without action must still suppress re-proposing.
func proposalExists(ctx context.Context, db *sql.DB, c candidate) (bool, error) {
	fence := dedupeFence(fingerprint(c.RecurrenceKey))
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE payload LIKE ?`, "%"+fence+"%").Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// roundingEpsilon guards the float comparison in rateString against binary
// representation noise (a 1-in-3 rate printed as 33.333333%).
const roundingEpsilon = 0.05

// rateString renders a share as a whole percentage for the briefing.
func rateString(part, whole int) string {
	if whole == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", int(math.Floor(float64(part)/float64(whole)*100+roundingEpsilon)))
}

// oneLine collapses a multi-line headline into a bounded single line.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const maxRunes = 160
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return s
}
