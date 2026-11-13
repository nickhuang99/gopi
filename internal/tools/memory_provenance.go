package tools

// Rule 16 provenance governance for distilled_qa (2026-09-11).
//
// WHY THIS FILE EXISTS: Rule 16 (devdoc/PRINCIPLE-lifecycle-storage-
// completeness.md) requires every stage's output to be ADDRESSABLE downstream
// — "能被下一个任务检索到", not merely "stored somewhere". A distilled_qa row
// whose origin is a free-text descriptor ("git_blame tool", "smoke-test") is
// stored but NOT addressable: a downstream task cannot resolve it back to the
// artifact/capability that produced it, so it must re-derive what the row
// already knew. That is exactly the DFA-degeneration Rule 16 names.
//
// THE MEASURED DEFECT (live DB ~/.gopi/memory.db, 1831 rows, 2026-09-11):
//
//	the pre-existing `sources` column is free text whose fill is only ~571/1831
//	addressable (a filesystem path). The rest carry either nothing or a human
//	descriptor that names no addressable artifact. `evidence` is EMPTY for all
//	1831 rows — it carries no provenance at all (the briefing's "1426 evidence
//	rows" premise was pre-investigation; the real defect lives in `sources`).
//
// THE FIX (additive, never destructive): a NEW `source_type` column carrying
// the CLASSIFIED origin of each row. The existing free-text `sources` is
// preserved verbatim — the classification is a derived, recomputable view over
// it, so a wrong classification is always repairable without data loss.
//
// Rule 1 note: every literal in this file (the column name, the bucket names,
// the classification ladder) comes from a named constant — the classifier, the
// schema heal, the count query and the tests all read one source of truth. The
// SQL is generated FROM the ladder slice, so a bucket can never be added in one
// place and forgotten in another.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants (Rule 1: no magic numbers / bare string keywords).
// ---------------------------------------------------------------------------

const (
	// provenanceSourceTypeCol is the additive provenance column on
	// distilled_qa. It is PROVENANCE ONLY — the classification reads `sources`
	// and writes this column, nothing else.
	provenanceSourceTypeCol = "source_type"

	// provenanceTypeUnknown is the source_type of a row whose `sources` is
	// NULL or whitespace only. It is the honest "we cannot say where this came
	// from" bucket — NOT zero-by-construction fraud: it is measured (500 live
	// rows), it is the only truthful label for a row that recorded no origin,
	// and the taxonomy keeps it distinct from provenanceTypeDescriptor.
	provenanceTypeUnknown = "unknown"

	// provenanceTypeLocalPath is a sources value that names a filesystem
	// location (an artifact a downstream task can open). Measured signature: a
	// '/' separator. Chosen over ".md" because ".md" as a bare substring also
	// matches free PROSE ("epic: read GOPI.md + git status …") — a measured
	// false positive; a slash is the only cheap signature that a prose
	// descriptor never carries in this population.
	provenanceTypeLocalPath = "local_path"

	// provenanceTypeGitRef is a sources value that names a GIT REFERENCE (a
	// branch like "feat/chat-remote-dispatch") rather than a file. It shares the
	// '/' signature with local_path — which is why the naive slash rule files it
	// as a path — and the two must not be conflated: a branch is resolved with
	// git, a path is opened with a reader. Measured 2026-09-11: 60 rows are a
	// ref with NO file named anywhere in the row, and the refinement applies
	// only to those (every one of the other 64 slash-bearing non-path rows DOES
	// name a real file — ".md"/".go"/"internal/" — so it is genuinely a path
	// reference and stays local_path).
	provenanceTypeGitRef = "git_ref"

	// provenanceTypeURL is a sources value that is a hyperlink. Kept SEPARATE
	// from local_path because a URL is not a filesystem path — a caller
	// resolving a row's origin must know to fetch rather than open. The '://'
	// signature is checked BEFORE the slash rule (a URL contains slashes and
	// would otherwise be misfiled as a path).
	provenanceTypeURL = "url"

	// provenanceTypeImport is a row whose origin is a bulk import (the
	// distilled.db / hermes-skills migrations). The import CHANNEL is named;
	// the per-row upstream artifact is not — so this is not addressable.
	provenanceTypeImport = "import"

	// provenanceTypeNode is a row that arrived from a PEER NODE via memory
	// sync (the sync layer stamps `node:<alias>` provenance at import time —
	// devdoc/memory-procedural-impl.md §8). The alias makes the origin host
	// addressable.
	provenanceTypeNode = "node"

	// provenanceTypeSession is a row whose origin is an agent session. A
	// session id would be addressable; the measured labels carry only the word
	// "session", so this bucket is honestly NOT addressable.
	provenanceTypeSession = "session"

	// provenanceTypeTask is a row whose origin is a scheduled task (a task id
	// resolves in the task ledger, so this is addressable).
	provenanceTypeTask = "task"

	// provenanceTypeEpic is a row whose origin is an epic goal statement (the
	// epic text lives in the task ledger — addressable).
	provenanceTypeEpic = "epic"

	// provenanceTypeDescriptor is a non-empty human descriptor that names no
	// addressable artifact ("git_blame tool", "smoke-test", "manual"). This is
	// the honest residual of the free-text era: the writer KNEW the origin and
	// the field kept only a label. It is deliberately distinct from `unknown`
	// (which had no label at all) — an absent label and an unresolvable label
	// are different kinds of untraceable, and lumping them would hide that.
	provenanceTypeDescriptor = "descriptor"

	// provenanceImportMarkerEN / provenanceImportMarkerCJK are the two import
	// signatures measured in the live DB: the CJK '导入' of the distilled.db
	// migrations and the ASCII 'import' of the hermes-skills import. Both are
	// kept — a single-language marker would miss half the import population.
	provenanceImportMarkerEN  = "import"
	provenanceImportMarkerCJK = "导入"

	// provenanceNodeMarker is the peer-node provenance marker the sync layer
	// writes (lowercased for the case-insensitive probe).
	provenanceNodeMarker = "node:"

	// provenanceSessionMarker / provenanceEpicMarker are the two descriptor
	// families frequent enough in the measured distribution to deserve their
	// own bucket.
	provenanceSessionMarker = "session"
	provenanceEpicMarker    = "epic"

	// provenanceTaskMarkerPrefix / provenanceTaskMarkerInfix are the two
	// measured shapes of the task-origin family: the bare "task…" prefix
	// ("task: tab-completion epic 2026-09") and the mid-string id form
	// ("task t-1788769211" appearing after other text).
	provenanceTaskMarkerPrefix = "task"
	provenanceTaskMarkerInfix  = "task t-"
)

// provenanceGitRefPrefixes are the git-branch prefixes measured in this repo's
// work (feat/ and fix/ dominate). Probed only at the START of the sources
// value: a branch reference is written as the head of the field, and a bare
// substring match would fire on mid-sentence prose ("… branch feat/x").
var provenanceGitRefPrefixes = []string{"feat/", "fix/", "docs/", "chore/", "test/", "refactor/"}

// provenanceFileShapedMarkers are the "this row really does name a file"
// signals used as the PRE-GUARD of the git_ref rung: a slash-bearing sources
// value carrying any of these is a path reference and must stay local_path.
var provenanceFileShapedMarkers = []string{
	".md", ".go", ".py", ".rs", ".sh", ".json", ".txt",
	"internal/", "cmd/", "devdoc/", "scripts/", "docs/", "src/",
}

// provenanceSourceExpr is the SQL expression the ladder classifies. It reads
// ONLY the pre-existing free-text `sources` column.
const provenanceSourceExpr = "coalesce(sources,'')"

// provenanceSourceToken is the placeholder the ladder's predicates are written
// against; provenanceCaseSQL substitutes provenanceSourceExpr for EVERY
// occurrence. A bare alias ("s") cannot be used because the same rendered SQL
// runs in a SELECT (which could alias) and in an UPDATE (whose expression
// position has no table alias) — the token makes the ladder position-agnostic.
const provenanceSourceToken = "{{src}}"

// ---------------------------------------------------------------------------
// The classification ladder.
// ---------------------------------------------------------------------------

// provenanceLadder is THE classification ladder — the single source of truth
// both the rendered SQL and the documented order read. The ORDER is
// load-bearing and justified by the measured distribution:
//
//  1. empty       → unknown     (checked first: no text to classify)
//  2. '://'       → url         (BEFORE the slash rule, else a URL misfiles)
//  3. ref-only    → git_ref     (branch prefix + slash + NO file-shaped marker)
//  4. '/'         → local_path  (measured signature of an addressable artifact)
//  5. import      → import      (导入 | import)
//  6. 'node:'     → node        (sync-layer peer provenance)
//  7. 'session'   → session
//  8. task/…      → task        ('task' prefix or 'task t-' infix)
//  9. 'epic'      → epic
//
// 10. (residual)  → descriptor  (non-empty text naming nothing addressable)
//
// Rung 3 is a REFINEMENT of rung 4, not a competing rule: its NOT-guard over the
// file-shaped markers means it can only fire where rung 4 would otherwise have
// claimed a bare branch name. Rungs 2–9 are mutually exclusive by first-match-
// wins and rung 10 is the total fallback for non-empty text, so the ladder
// PARTITIONS the non-empty population; the empty case is rung 1. Every row
// therefore receives exactly one type and none can reach the SQL's ELSE arm.
func provenanceLadder() []provColumnRule {
	return []provColumnRule{
		{want: provenanceTypeUnknown, sqlTest: "{{src}} IS NULL OR trim({{src}})=''",
			why: "no sources text at all — nothing to classify (honest unknown)"},
		{want: provenanceTypeURL, sqlTest: "{{src}} LIKE '%'||?||'%'",
			why:     "hyperlink — fetch, not open (checked before the '/' path rule)",
			markers: []string{"://"}},
		{want: provenanceTypeGitRef,
			sqlTest: "{{src}} LIKE '%'||?||'%'" +
				" AND ({{src}} LIKE ?||'%'" + strings.Repeat(" OR {{src}} LIKE ?||'%'", len(provenanceGitRefPrefixes)-1) + ")" +
				" AND NOT (" + fileShapedGuardSQL() + ")",
			why: "a GIT REFERENCE (branch feat/x) — ref-only rows only; the NOT guard " +
				"makes this a refinement of local_path, never a reclassification of a real path",
			markers: append([]string{"/"}, append(append([]string{}, provenanceGitRefPrefixes...), provenanceFileShapedMarkers...)...)},
		{want: provenanceTypeLocalPath, sqlTest: "{{src}} LIKE '%'||?||'%'",
			why:     "a '/' separator — the measured signature of an addressable artifact path",
			markers: []string{"/"}},
		{want: provenanceTypeImport,
			sqlTest: "{{src}} LIKE '%'||?||'%' OR lower({{src}}) LIKE '%'||?||'%'",
			why:     "bulk-import origin (distilled.db / hermes-skills migrations)",
			markers: []string{provenanceImportMarkerCJK, provenanceImportMarkerEN}},
		{want: provenanceTypeNode, sqlTest: "lower({{src}}) LIKE '%'||?||'%'",
			why:     "peer-node sync provenance (node:<alias>)",
			markers: []string{provenanceNodeMarker}},
		{want: provenanceTypeSession, sqlTest: "lower({{src}}) LIKE '%'||?||'%'",
			why:     "agent-session origin",
			markers: []string{provenanceSessionMarker}},
		{want: provenanceTypeTask,
			sqlTest: "lower({{src}}) LIKE ?||'%' OR lower({{src}}) LIKE '%'||?||'%'",
			why:     "scheduled-task origin (the task id resolves in the ledger)",
			markers: []string{provenanceTaskMarkerPrefix, provenanceTaskMarkerInfix}},
		{want: provenanceTypeEpic, sqlTest: "lower({{src}}) LIKE '%'||?||'%'",
			why:     "epic-goal origin",
			markers: []string{provenanceEpicMarker}},
		{want: provenanceTypeDescriptor, sqlTest: "1=1",
			why: "NON-EMPTY residual: a human descriptor naming no addressable artifact"},
	}
}

// fileShapedGuardSQL renders the "this value names a FILE" guard as a
// disjunction over provenanceFileShapedMarkers. It is the pre-guard that keeps
// the git_ref rung from stealing a real path: without it, any sources value
// that both starts with a branch prefix and carries a slash would be filed as a
// ref ("feat/memroadmap-m1 devdoc/memroadmap-m1.md" — measured).
func fileShapedGuardSQL() string {
	parts := make([]string, 0, len(provenanceFileShapedMarkers))
	for range provenanceFileShapedMarkers {
		parts = append(parts, "{{src}} LIKE '%'||?||'%'")
	}
	return strings.Join(parts, " OR ")
}

// provColumnRule is one rung of the classification ladder: the source_type it
// assigns, the SQL predicate (written against provenanceSourceToken) that
// selects it, the markers its predicate interpolates in textual order, and the
// measured justification for the rung.
type provColumnRule struct {
	want    string
	sqlTest string
	markers []string
	why     string
}

// provenanceCaseSQL renders the ladder into a SQL CASE expression over the
// `sources` column, returning the expression plus its bound arguments in
// ladder order. Rendering from the ladder (never hand-writing the CASE) is what
// keeps the distribution query, the backfill UPDATE and the tests in lockstep.
func provenanceCaseSQL() (string, []any) {
	var b strings.Builder
	b.WriteString("CASE")
	var args []any
	for _, r := range provenanceLadder() {
		// Markers bind in TEXTUAL order — the predicate is written so its
		// placeholders appear in exactly the order the markers are listed.
		args = append(args, toAnySlice(r.markers)...)
		b.WriteString(" WHEN ")
		b.WriteString(r.sqlTest)
		b.WriteString(" THEN '" + r.want + "'")
	}
	// Unreachable by construction (the last rung is 1=1) — kept so a future
	// rung inserted above the descriptor fallback can never yield NULL.
	b.WriteString(" ELSE '" + provenanceTypeUnknown + "' END")
	return strings.ReplaceAll(b.String(), provenanceSourceToken, provenanceSourceExpr), args
}

// toAnySlice widens a marker slice for database/sql's variadic argument list.
func toAnySlice(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------------------
// Schema heal.
// ---------------------------------------------------------------------------

// ensureProvenanceSchema adds the additive source_type column when missing —
// the same PRAGMA-probe + ALTER pattern as ensureFeedbackSchema (zero migration
// scripts; an old/foreign schema simply lacks the column and reads as empty).
// NEVER drops, NEVER rewrites `sources`: the classification is derivable from
// `sources` at any time, so no information is destroyed.
func ensureProvenanceSchema(ctx context.Context, db *sql.DB) error {
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return err
	}
	if cols[provenanceSourceTypeCol] {
		return nil
	}
	// The PRAGMA probe has a TOCTOU window under concurrent first use — the
	// loser's ALTER reports "duplicate column name", which means the column
	// exists, which is all this heal needs (ensureFeedbackSchema precedent).
	if _, err := db.ExecContext(ctx, "ALTER TABLE distilled_qa ADD COLUMN "+
		provenanceSourceTypeCol+" TEXT DEFAULT ''"); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Distribution report (deliverable 1 — data-driven, re-runnable).
// ---------------------------------------------------------------------------

// ProvenanceShape is one bucket of the measured shape distribution.
type ProvenanceShape struct {
	SourceType string `json:"source_type"`
	Count      int    `json:"count"`
	Meaning    string `json:"meaning"`
}

// ProvenanceShapeReport is the full distribution plus the population it
// accounts for. Total is the row count the buckets were measured over;
// Addressable and Untraceable are surfaced explicitly so the untraceable split
// is never buried in the bucket list.
type ProvenanceShapeReport struct {
	Total       int               `json:"total"`
	Shapes      []ProvenanceShape `json:"shapes"`
	Addressable int               `json:"addressable"`
	Untraceable int               `json:"untraceable"`
}

// ProvenanceTypeMeanings documents each bucket for the report — the
// machine-readable half of the taxonomy (devdoc/distilled-provenance-
// governance.md is the prose half).
func ProvenanceTypeMeanings() map[string]string {
	return map[string]string{
		provenanceTypeLocalPath:  "sources names a filesystem artifact — a downstream task opens it directly",
		provenanceTypeURL:        "sources is a hyperlink — a downstream task fetches it",
		provenanceTypeGitRef:     "sources is a git reference (branch) — a downstream task resolves it with git, not a reader",
		provenanceTypeImport:     "bulk import (distilled.db / hermes-skills); the channel is named, the upstream row is not",
		provenanceTypeNode:       "arrived from peer node <alias> via memory sync — the origin host is addressable",
		provenanceTypeSession:    "agent-session origin (a session id would make it fully addressable; the label alone does not)",
		provenanceTypeTask:       "scheduled-task origin — the task id resolves in the task ledger",
		provenanceTypeEpic:       "epic-goal origin (the epic text lives in the task ledger)",
		provenanceTypeDescriptor: "free-text label naming no addressable artifact (the free-text era's residual)",
		provenanceTypeUnknown:    "sources is empty — no origin recorded at all (honest unknown, never fabricated)",
	}
}

// ProvenanceTypeAddressable reports whether a label counts as ADDRESSABLE under
// Rule 16 — can a downstream task resolve the origin without re-doing the work?
// local_path/url/node/task/epic = yes; import/session/descriptor/unknown = no
// (import: channel only; session: a label, not an id; descriptor: nothing
// resolvable; unknown: nothing at all). This split — NOT "everything that is not
// a path" — is what the report's untraceable number is measured from, so a
// peer-node origin is not unfairly counted as untraceable.
func ProvenanceTypeAddressable(label string) bool {
	switch label {
	case provenanceTypeLocalPath, provenanceTypeURL, provenanceTypeGitRef,
		provenanceTypeNode, provenanceTypeTask, provenanceTypeEpic:
		return true
	}
	return false
}

// MemoryProvenanceShape measures the CURRENT shape of the knowledge base's row
// origins by classifying the live `sources` free text with the ladder — no
// writes, no assumptions. Buckets are ordered by count (descending) so the
// dominant shape is the first line of the report.
func MemoryProvenanceShape() (*ProvenanceShapeReport, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	caseSQL, args := provenanceCaseSQL()
	q := "SELECT " + caseSQL + " AS st, count(*) FROM distilled_qa GROUP BY st ORDER BY count(*) DESC"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("provenance shape: %w", err)
	}
	defer rows.Close()

	rep := &ProvenanceShapeReport{}
	meanings := ProvenanceTypeMeanings()
	for rows.Next() {
		var s ProvenanceShape
		if err := rows.Scan(&s.SourceType, &s.Count); err != nil {
			return nil, fmt.Errorf("provenance shape: %w", err)
		}
		s.Meaning = meanings[s.SourceType]
		rep.Total += s.Count
		if ProvenanceTypeAddressable(s.SourceType) {
			rep.Addressable += s.Count
		} else {
			rep.Untraceable += s.Count
		}
		rep.Shapes = append(rep.Shapes, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("provenance shape: %w", err)
	}
	return rep, nil
}

// ---------------------------------------------------------------------------
// Backfill (deliverable 3 — idempotent, transactional, additive).
// ---------------------------------------------------------------------------

// ProvenanceBackfillResult reports one backfill pass. Filled counts rows whose
// (previously empty) source_type was set by THIS pass; Unchanged counts rows
// already carrying a value (the idempotency witness — a second run reports
// Filled=0). Unknown is the honest-unknown tally AFTER the pass.
type ProvenanceBackfillResult struct {
	Scanned   int `json:"scanned"`
	Filled    int `json:"filled"`
	Unchanged int `json:"unchanged"`
	Unknown   int `json:"unknown"`
}

// MemoryProvenanceBackfill classifies every distilled_qa row's `sources` free
// text and writes the result into the additive source_type column.
//
// Contract:
//   - additive ONLY: the single UPDATE touches source_type; topic, question,
//     answer, search_kw, sources and every other column are untouched (the
//     WHERE clause anchors on source_type=” so an already-classified row is
//     never even re-written).
//   - idempotent: a second run finds no row with an empty source_type and
//     reports Filled=0 (proven by running it twice in the test).
//   - transactional: the whole pass runs inside one BEGIN IMMEDIATE
//     transaction, so a crash mid-pass leaves the table unchanged rather than
//     half-classified.
//   - non-destructive: a blank/whitespace sources row classifies to the honest
//     `unknown`; nothing is deleted and `sources` is never rewritten.
//
// A row whose `sources` CHANGED after a backfill keeps its old label until
// explicitly reclassified (see MemoryProvenanceReclassify) — an unattended pass
// never overwrites a label a human may have corrected.
func MemoryProvenanceBackfill() (*ProvenanceBackfillResult, error) {
	return provenanceBackfill(false)
}

// MemoryProvenanceReclassify re-derives source_type for EVERY row from the
// current `sources` text — the repair path after a `sources` edit, and the proof
// that the classification is a pure function of `sources` (Rule 16: derived data
// is recomputable, never a second source of truth). Running it twice yields the
// same labels (idempotent), which the test asserts explicitly.
func MemoryProvenanceReclassify() (*ProvenanceBackfillResult, error) {
	return provenanceBackfill(true)
}

// provenanceBackfill is the shared engine for the two entry points above.
// force=false seeds only empty labels; force=true re-derives every label.
func provenanceBackfill(force bool) (*ProvenanceBackfillResult, error) {
	ctx := context.Background()
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureProvenanceSchema(ctx, db); err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}
	// merge-note: both sides fixed the SAME stylegate ignored-error finding on
	// this defer — the branch added the inline escape here (commit 8e6c72ad,
	// "make the latent harvest add zero new gate findings"), master's
	// fix/stylegate-ceiling-audit landed the audited superset (prose rationale
	// + the marker on the defer line, the form the gate prefers). Master's
	// block is kept; the branch's intent — a documented inline escape, control
	// flow untouched — is preserved, not dropped.
	// The deferred Rollback runs after the Commit below on the success path,
	// where database/sql returns ErrTxDone — a documented no-op, not a failure
	// to report. Restructuring the control flow to satisfy the gate would be
	// the real defect (never change semantics for a gate). The error path is
	// covered because every failure between BeginTx and Commit returns its own
	// wrapped error.
	defer func() { _ = tx.Rollback() }() //stylegate:ignore defer Rollback after a successful Commit returns ErrTxDone (a no-op); the error path already returns its own wrapped failure, so there is nothing this error could add

	rep := &ProvenanceBackfillResult{}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM distilled_qa").Scan(&rep.Scanned); err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}

	caseSQL, args := provenanceCaseSQL()
	where := provenanceSourceTypeCol + " IS NULL OR " + provenanceSourceTypeCol + "=''"
	if force {
		where = "1=1"
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE distilled_qa SET "+provenanceSourceTypeCol+"=("+caseSQL+") WHERE "+where, args...)
	if err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}
	rep.Filled = int(n)
	if force {
		rep.Filled = 0 // a forced pass re-derives rather than fills; report it as such
		rep.Unchanged = int(n)
	} else {
		rep.Unchanged = rep.Scanned - rep.Filled
	}

	// Honest-unknown tally over the post-pass table (the number the devdoc must
	// state plainly: how many rows have no recorded origin).
	if err := tx.QueryRowContext(ctx,
		"SELECT count(*) FROM distilled_qa WHERE "+provenanceSourceTypeCol+"=?",
		provenanceTypeUnknown).Scan(&rep.Unknown); err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("provenance backfill: %w", err)
	}
	return rep, nil
}

// ProvenanceCounts returns the per-source_type row counts currently persisted in
// the DB, ordered by count (descending). This is the VERIFICATION query of the
// acceptance criteria: the labels sum to the row population and the
// filled/unknown split is read straight off it.
func ProvenanceCounts() (map[string]int, error) {
	ctx := context.Background()
	// Heal first (additive column only): on a DB that predates the backfill the
	// column is absent, and a verification query must report "all empty"
	// honestly rather than fail — the heal adds the column, it never fills it.
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureProvenanceSchema(ctx, db); err != nil {
		return nil, fmt.Errorf("provenance counts: %w", err)
	}
	rows, err := db.QueryContext(ctx,
		"SELECT "+provenanceSourceTypeCol+", count(*) FROM distilled_qa GROUP BY "+
			provenanceSourceTypeCol+" ORDER BY count(*) DESC")
	if err != nil {
		return nil, fmt.Errorf("provenance counts: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var label string
		var n int
		if err := rows.Scan(&label, &n); err != nil {
			return nil, fmt.Errorf("provenance counts: %w", err)
		}
		out[label] = n
	}
	return out, rows.Err()
}

// ProvenanceLatentReport is the HONEST-RESIDUAL measurement: of the rows whose
// `sources` field records no origin (source_type = unknown), how many carry a
// path-shaped token somewhere in their OWN text (topic/question/answer)?
//
// WHY THIS EXISTS: `unknown` is accurate about the FIELD ("no origin recorded
// here") but it must never be over-claimed as "the origin is lost" — measured
// 2026-09-11, 495 of the 500 unknown rows name a file in their topic
// ("hermes_cli/plugins.py — plugin manager"), so their provenance is LATENT:
// recoverable by a future pass that harvests it, without redoing the work. Only
// LatentNone rows are untraceable in the strong sense. Reporting the two
// separately is what keeps `unknown` honest rather than a bucket that hides a
// recoverable population.
type ProvenanceLatentReport struct {
	// UnknownRows is the population measured (source_type = unknown).
	UnknownRows int `json:"unknown_rows"`
	// LatentRecoverable counts unknown rows carrying a path-shaped token in
	// their own text — provenance exists, it was just never copied into
	// `sources`.
	LatentRecoverable int `json:"latent_recoverable"`
	// LatentNone counts unknown rows with no path-shaped token anywhere: the
	// strong-sense untraceable remainder.
	LatentNone int `json:"latent_none"`
}

// provenancePathToken is the read-only "does this row's text name an artifact"
// probe. It is deliberately the SAME signature the ladder's local_path rung
// uses (a '/' separator), so "latent provenance" and "recorded provenance"
// are measured on one definition — a second, looser signature here would
// inflate the recoverable count against the classifier's own standard.
const provenancePathToken = "/"

// MemoryProvenanceLatent measures the latent-vs-lost split of the rows whose
// `sources` field records no origin. Read-only: it classifies text, it writes
// nothing.
func MemoryProvenanceLatent() (*ProvenanceLatentReport, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureProvenanceSchema(ctx, db); err != nil {
		return nil, err
	}
	rep := &ProvenanceLatentReport{}
	const textHasPath = "topic LIKE '%'||?||'%' OR question LIKE '%'||?||'%' OR answer LIKE '%'||?||'%'"
	// Unknown population = the persisted label (the field-level verdict).
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM distilled_qa WHERE "+provenanceSourceTypeCol+"=?",
		provenanceTypeUnknown).Scan(&rep.UnknownRows); err != nil {
		return nil, fmt.Errorf("provenance latent: %w", err)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM distilled_qa WHERE "+provenanceSourceTypeCol+"=? AND ("+textHasPath+")",
		provenanceTypeUnknown, provenancePathToken, provenancePathToken, provenancePathToken).
		Scan(&rep.LatentRecoverable); err != nil {
		return nil, fmt.Errorf("provenance latent: %w", err)
	}
	rep.LatentNone = rep.UnknownRows - rep.LatentRecoverable
	return rep, nil
}

// ProvenanceShapeLines renders the distribution as stable, human-readable lines
// (the CLI report body), preserving the report's count-descending order.
func ProvenanceShapeLines(rep *ProvenanceShapeReport) []string {
	lines := []string{
		fmt.Sprintf("total rows: %d", rep.Total),
		fmt.Sprintf("addressable: %d   untraceable: %d", rep.Addressable, rep.Untraceable),
	}
	for _, s := range rep.Shapes {
		lines = append(lines, fmt.Sprintf("  %-12s %5d  %s", s.SourceType, s.Count, s.Meaning))
	}
	return lines
}
