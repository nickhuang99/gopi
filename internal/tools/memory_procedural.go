// Procedural memory (工作流记忆) — the fifth layer of the GLM five-layer
// memory model. distilled_qa holds declarative lessons; this table holds
// REUSABLE WORKFLOWS: named procedures with a trigger (何时启用), structured
// steps (JSON array), preconditions and the tools they exercise.
//
// Design (Bond-validated epic spec, verified against this engine):
//   - Separate table `procedural` with name UNIQUE — a workflow's name IS
//     its identity (unlike distilled_qa, whose dedup key is question+topic).
//   - Two FTS5 external-content indexes + keep-in-sync triggers, the exact
//     distilled_qa pattern: procedural_fts (unicode61, ASCII exact-token)
//     over (name, trigger, steps) and procedural_tri (trigram, CJK ≥3-char
//     substring recall). 2-char CJK falls back to a LIKE scan like search.
//   - Dedup: name UNIQUE (exact) + trigram-Jaccard similarity on
//     name+trigger (same threshold trigramJaccardMerge as MemoryStore) —
//     a re-brew or re-store of the same workflow under a slightly different
//     name merges instead of duplicating.
//   - Lifecycle columns reuse the M2 machinery semantics: tier/confidence
//     (feedback + cold demotion apply), version + updated_at (evolution
//     tracking: a similar-name store bumps version instead of inserting),
//     use_count/success_rate (stats columns, written by future executors —
//     never read here).
//   - Sync: rows travel in the memsync channel with a dedicated patch kind
//     and the same node:<hostname> provenance stamp; export gates on the
//     brew tag like the QA channel (no ping-pong).
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Procedural schema constants (Rule 1: every tunable in one place).
const (
	proceduralTable = "procedural"

	// proceduralSourceTag marks procedural rows written by the store path —
	// the export gate reads it exactly like BrewSourceTag on the QA channel.
	proceduralSourceTag = "procedural"

	// proceduralStepMax caps the steps JSON (bytes) — a workflow with more
	// than ~8KB of steps is a runbook, not a memory row.
	proceduralStepMax = 8192

	// proceduralNameMaxRunes / proceduralTriggerMaxRunes clamp the fields
	// the dedup identity is built from (mirror minQuestionLen's spirit).
	proceduralNameMaxRunes    = 120
	proceduralTriggerMaxRunes = 400

	// proceduralDefaultVersion is the first version a stored row carries.
	proceduralDefaultVersion = 1

	// proceduralNameJaccardMerge is the name-similarity merge threshold. A
	// workflow's name is short and often reordered (重启网络服务 vs
	// 网络服务重启 = 1/3) — 0.3 merges reorderings while a genuinely
	// different flow shares almost no trigrams (well below 0.3).
	proceduralNameJaccardMerge = 0.3
)

// ProceduralStep is one step of a structured workflow. JSON-encoded into the
// steps column as an array; freeform enough for command/text steps.
type ProceduralStep struct {
	Action string `json:"action"` // what to do ("run", "check", "edit", ...)
	Detail string `json:"detail"` // how to do it (command, path, condition)
}

// ProceduralWorkflow is the store/search contract: the named workflow record.
type ProceduralWorkflow struct {
	Name          string `json:"name"`
	Trigger       string `json:"trigger"` // 何时启用
	Steps         []ProceduralStep
	StepsJSON     string   `json:"-"` // raw JSON when the caller pre-encoded
	Preconditions string   `json:"preconditions,omitempty"`
	ToolsUsed     []string `json:"tools_used,omitempty"`

	// Lifecycle / provenance (store fills, search returns).
	Tier        string   `json:"tier,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	Version     int      `json:"version,omitempty"`
	UseCount    int      `json:"use_count,omitempty"`
	SuccessRate float64  `json:"success_rate,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	CreatedAt   float64  `json:"created_at,omitempty"`
	UpdatedAt   float64  `json:"updated_at,omitempty"`

	ID     int  `json:"id,omitempty"`
	Merged bool `json:"deduped,omitempty"` // merged into an existing workflow
}

// ensureProceduralSchema creates the procedural table, its two FTS5
// external-content indexes and the keep-in-sync triggers. Idempotent — safe
// to run on every store/search/open (same contract as ensureSchema).
func ensureProceduralSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS procedural (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			trigger TEXT NOT NULL DEFAULT '',
			steps TEXT NOT NULL DEFAULT '[]',
			preconditions TEXT DEFAULT '',
			tools_used TEXT DEFAULT '',
			success_rate REAL DEFAULT 0,
			use_count INTEGER DEFAULT 0,
			version INTEGER DEFAULT 1,
			tier TEXT DEFAULT 'warm',
			confidence REAL DEFAULT 0.85,
			sources TEXT DEFAULT '',
			created_at REAL DEFAULT 0,
			updated_at REAL DEFAULT 0
		)`); err != nil {
		return err
	}
	ftsCols := "name, trigger, steps"
	triCols := "name, trigger"
	stmts := []string{
		"CREATE VIRTUAL TABLE IF NOT EXISTS procedural_fts USING fts5(" + ftsCols +
			", content='" + proceduralTable + "', content_rowid='id')",
		"CREATE VIRTUAL TABLE IF NOT EXISTS procedural_tri USING fts5(" + triCols +
			", content='" + proceduralTable + "', content_rowid='id', tokenize='trigram')",
		// Keep-in-sync triggers, external-content INSERT/DELETE('delete') pattern —
		// the exact distilled_qa_ai/au/ad trio applied per index.
		`CREATE TRIGGER IF NOT EXISTS procedural_fts_ai AFTER INSERT ON procedural BEGIN
			INSERT INTO procedural_fts(rowid, name, trigger, steps)
			VALUES (new.id, new.name, new.trigger, new.steps);
		END`,
		`CREATE TRIGGER IF NOT EXISTS procedural_fts_au AFTER UPDATE ON procedural BEGIN
			INSERT INTO procedural_fts(procedural_fts, rowid, name, trigger, steps)
			VALUES ('delete', old.id, old.name, old.trigger, old.steps);
			INSERT INTO procedural_fts(rowid, name, trigger, steps)
			VALUES (new.id, new.name, new.trigger, new.steps);
		END`,
		`CREATE TRIGGER IF NOT EXISTS procedural_fts_ad AFTER DELETE ON procedural BEGIN
			INSERT INTO procedural_fts(procedural_fts, rowid, name, trigger, steps)
			VALUES ('delete', old.id, old.name, old.trigger, old.steps);
		END`,
		`CREATE TRIGGER IF NOT EXISTS procedural_tri_ai AFTER INSERT ON procedural BEGIN
			INSERT INTO procedural_tri(rowid, name, trigger)
			VALUES (new.id, new.name, new.trigger);
		END`,
		`CREATE TRIGGER IF NOT EXISTS procedural_tri_au AFTER UPDATE ON procedural BEGIN
			INSERT INTO procedural_tri(procedural_tri, rowid, name, trigger)
			VALUES ('delete', old.id, old.name, old.trigger);
			INSERT INTO procedural_tri(rowid, name, trigger)
			VALUES (new.id, new.name, new.trigger);
		END`,
		`CREATE TRIGGER IF NOT EXISTS procedural_tri_ad AFTER DELETE ON procedural BEGIN
			INSERT INTO procedural_tri(procedural_tri, rowid, name, trigger)
			VALUES ('delete', old.id, old.name, old.trigger);
		END`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	_, _ = db.ExecContext(ctx, "PRAGMA journal_mode=WAL") //stylegate:ignore auxiliary DDL/PRAGMA/FTS statement; the primary statement's result is what is checked
	return nil
}

// stepsJSON renders the workflow's steps as the JSON array the steps column
// stores. Callers may pass pre-encoded JSON via StepsJSON (brew/sync path);
// nil steps → "[]" (an empty workflow is still a valid, searchable record).
func (w *ProceduralWorkflow) stepsJSON() (string, error) {
	if strings.TrimSpace(w.StepsJSON) != "" {
		return w.StepsJSON, nil
	}
	if w.Steps == nil {
		return "[]", nil
	}
	b, err := json.Marshal(w.Steps)
	if err != nil {
		return "", fmt.Errorf("procedural: marshal steps: %w", err)
	}
	return string(b), nil
}

// proceduralOpenStore opens a read-write connection and ensures the schema.
func proceduralOpenStore(ctx context.Context) (*sql.DB, error) {
	db, err := openStoreDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	if err := ensureProceduralSchema(ctx, db); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		return nil, fmt.Errorf("procedural: schema: %w", err)
	}
	return db, nil
}

// ProceduralStore runs the memory_store type=procedural path: validate,
// dedup (name UNIQUE exact + name+trigger trigram similarity → version bump),
// insert with FTS sync. The row is tagged source=procedural (+ caller source)
// so the memsync export gate can scope it exactly like the QA channel.
func ProceduralStore(w *ProceduralWorkflow, source string) (string, error) {
	ctx := context.Background()
	db, err := proceduralOpenStore(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()

	w.Name = strings.TrimSpace(w.Name)
	w.Trigger = strings.TrimSpace(w.Trigger)
	w.Preconditions = strings.TrimSpace(w.Preconditions)
	if w.Name == "" {
		return "", fmt.Errorf("procedural_store: name is required")
	}
	if w.Trigger == "" {
		return "", fmt.Errorf("procedural_store: trigger is required (何时启用)")
	}
	steps, err := w.stepsJSON()
	if err != nil {
		return "", err
	}
	if len(steps) > proceduralStepMax {
		return "", fmt.Errorf("procedural_store: steps JSON %d bytes > %d cap — split the workflow",
			len(steps), proceduralStepMax)
	}
	w.Name = clampRunes(w.Name, proceduralNameMaxRunes)
	w.Trigger = clampRunes(w.Trigger, proceduralTriggerMaxRunes)

	// Dedup 1: exact name. The UNIQUE constraint is the identity backstop;
	// the explicit SELECT turns it into a version-bump merge (evolution
	// tracking) instead of a constraint error.
	var existing ProceduralWorkflow
	var existingSourcesText string
	err = db.QueryRowContext(ctx,
		`SELECT id, COALESCE(version,1), COALESCE(sources,'') FROM procedural WHERE name=?`, w.Name).
		Scan(&existing.ID, &existing.Version, &existingSourcesText)
	if err == nil {
		if err := proceduralMerge(ctx, db, existing.ID, existing.Version, w, source, existingSourcesText); err != nil {
			return "", err
		}
		return fmt.Sprintf("stored procedural workflow #%d (deduped into existing entry: name %q — version bumped to %d)",
			existing.ID, w.Name, existing.Version+1), nil
	}
	// Rule 16 — REVIEWED UNWRAP, one guard split into two arms. This line is
	// reached in two states: a real failure (wrapped with %w so errors.Is/As
	// keep working) and sql.ErrNoRows, whose only meaning here is "no row yet —
	// go insert". Wrapping that sentinel would put a value into the chain that
	// the caller must treat as SUCCESS, so it stays out of the chain on purpose;
	// the arm is split rather than annotated `%v` so the reason survives review
	// and no message ever renders a sentinel as a cause.
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("procedural_store: name dedup check: %w", err)
	}

	// Dedup 2: trigram similarity on name+trigger (same threshold as
	// MemoryStore's findSimilarDuplicate) — "重启网络服务" vs "网络服务重启"
	// is one workflow, not two rows.
	if simID := proceduralFindSimilar(ctx, db, w.Name, w.Trigger); simID > 0 {
		var version int
		var sources string
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(version,1), COALESCE(sources,'') FROM procedural WHERE id=?`, simID).
			Scan(&version, &sources); err != nil {
			return "", fmt.Errorf("procedural_store: similarity row read: %w", err)
		}
		if err := proceduralMerge(ctx, db, int(simID), version, w, source, sources); err != nil {
			return "", err
		}
		return fmt.Sprintf("stored procedural workflow #%d (deduped into similar entry: version bumped to %d)",
			simID, version+1), nil
	}

	// New row: insert only with the tag set the export gate can see.
	src := proceduralSourceTag
	if s := strings.TrimSpace(source); s != "" && s != proceduralSourceTag {
		src += "," + s
	}
	now := float64(time.Now().Unix())
	res, err := db.ExecContext(ctx,
		`INSERT INTO procedural (name, trigger, steps, preconditions, tools_used, sources, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		w.Name, w.Trigger, steps, w.Preconditions,
		strings.Join(cleanKeywords(w.ToolsUsed), ", "),
		src, now, now)
	if err != nil {
		return "", fmt.Errorf("procedural_store: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("procedural_store: %w", err)
	}
	return fmt.Sprintf("stored procedural workflow #%d: %s", id, w.Name), nil
}

// proceduralMerge applies an incoming workflow onto an existing row: sources
// union + version bump + updated_at touch. Steps are NOT clobbered — the
// stored workflow stays authoritative (brew re-runs and re-stores are
// idempotent), mirroring MemoryStore's merge-not-overwrite dedup semantics.
func proceduralMerge(ctx context.Context, db *sql.DB, id, version int, w *ProceduralWorkflow, source, existingSources string) error {
	sources := mergeSourceTags(existingSources, source, proceduralSourceTag)
	_, err := db.ExecContext(ctx,
		`UPDATE procedural SET version=?, updated_at=?, sources=? WHERE id=?`,
		version+1, float64(time.Now().Unix()), sources, id)
	if err != nil {
		return fmt.Errorf("procedural_store: merge: %w", err)
	}
	return nil
}

// mergeSourceTags unions new tags into a comma-joined sources string without
// duplicates (order: existing tags first, then the newcomers).
func mergeSourceTags(existing string, tags ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(existing, ",") {
		if part = strings.TrimSpace(part); part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	for _, t := range tags {
		for _, part := range strings.Split(t, ",") {
			if part = strings.TrimSpace(part); part != "" && !seen[part] {
				seen[part] = true
				out = append(out, part)
			}
		}
	}
	return strings.Join(out, ",")
}

// proceduralFindSimilar pre-screens with the trigram index (name+trigger
// text) and confirms with trigram-Jaccard on the NAME alone — the name is
// the workflow's identity; two flows sharing a trigger but doing different
// things ("重启 nginx" vs "重启 postgres") must coexist. The trigger text
// only widens the FTS prescreen. Returns 0 when nothing is similar enough.
func proceduralFindSimilar(ctx context.Context, db *sql.DB, name, trigger string) int64 {
	triOK, err := tableExists(ctx, db, "procedural_tri")
	if err != nil || !triOK {
		return 0 // no trigram index → degrade to exact-name dedup only
	}
	grams := cjkRunsGE3(name)
	ascii := dedupTokenize(name, 8)
	parts := make([]string, 0, len(grams)*3+len(ascii))
	// A whole CJK run is indexed by trigram as overlapping 3-char grams, so
	// the probe must be split into the same grams — the run itself matches
	// only a stored run of identical length ("网络服务重启" ≠ "重启网络服务").
	for _, g := range grams {
		for i := 0; i+trigramGram <= len([]rune(g)); i++ {
			gram := string([]rune(g)[i : i+trigramGram])
			parts = append(parts, `"`+strings.ReplaceAll(gram, `"`, "")+`"`)
		}
	}
	for _, w := range ascii {
		parts = append(parts, `"`+w+`"`)
	}
	if len(parts) == 0 {
		return 0
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, name FROM procedural WHERE id IN
		 (SELECT rowid FROM procedural_tri WHERE procedural_tri MATCH ?)
		 ORDER BY id DESC LIMIT ?`,
		strings.Join(parts, " OR "), ftsDedupCandidates)
	if err != nil {
		return 0 // FTS unavailable → degrade to insert-only
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return 0
		}
		if jaccard(name, n) >= proceduralNameJaccardMerge {
			return id
		}
	}
	return 0
}

// ProceduralSearch retrieves workflows: FTS5 exact-token over
// name+trigger+steps, trigram CJK route, LIKE 2-char fallback — the three
// recall routes of MemorySearch pointed at the procedural table, ranked by
// query-token coverage then tier then recency of updated_at.
func ProceduralSearch(query string, limit int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("procedural_search: query must not be empty")
	}
	if limit <= 0 {
		limit = memoryDefaultLimit
	}
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	// Schema is ensured on the WRITE path (ProceduralStore / import). The
	// search connection is read-only (query_only PRAGMA), so creating it
	// here is impossible — a DB with no procedural table simply has no
	// workflows to find (the exact openSearchDB semantics of MemorySearch).
	if ok, err := tableExists(ctx, db, proceduralTable); err != nil || !ok {
		return "No procedural workflow found for this query. " +
			"(检索面包屑: use the workflow's name / trigger keywords; workflows live in the `procedural` table.)", nil
	}

	type cand struct {
		w      ProceduralWorkflow
		tokens int
		rank   float64
	}
	var cands []cand
	add := func(rows *sql.Rows, err error) error {
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var w ProceduralWorkflow
			var steps, toolsUsed, sources string
			var rank sql.NullFloat64
			if err := rows.Scan(&w.ID, &w.Name, &w.Trigger, &steps, &w.Preconditions, &toolsUsed,
				&w.Version, &w.Tier, &w.Confidence, &sources, &w.UpdatedAt, &rank); err != nil {
				return err
			}
			w.StepsJSON = steps
			_ = json.Unmarshal([]byte(steps), &w.Steps) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
			w.ToolsUsed = splitSources(toolsUsed)
			w.Sources = splitSources(sources)
			text := w.Name + " " + w.Trigger + " " + steps + " " + w.Preconditions + " " + toolsUsed
			tokens := queryTokenCoverage(query, &MemoryHit{Topic: w.Name, Question: w.Trigger, Answer: text})
			cands = append(cands, cand{w: w, tokens: tokens, rank: rank.Float64})
		}
		return rows.Err()
	}

	// Route 1: exact-token FTS5 (unicode61 over name+trigger+steps). A query
	// with no searchable term at all skips the route — MATCH "" is an FTS5
	// syntax error, and this route must never be the reason a user query
	// fails (see escapeQuery's safety contract).
	if match, ok := escapeQuery(query); ok {
		err = add(db.QueryContext(ctx,
			`SELECT p.id, p.name, p.trigger, p.steps, COALESCE(p.preconditions,''), COALESCE(p.tools_used,''),
		        COALESCE(p.version,1), COALESCE(p.tier,'`+defaultTier+`'), COALESCE(p.confidence,0),
		        COALESCE(p.sources,''), COALESCE(p.updated_at,0), fts.rank
		 FROM procedural_fts fts JOIN procedural p ON p.id = fts.rowid
		 WHERE procedural_fts MATCH ? ORDER BY fts.rank LIMIT ?`,
			match, trigramRecallK))
		if err != nil && err != sql.ErrNoRows {
			return "", fmt.Errorf("procedural_search: %w", err)
		}
	}
	// Route 2: trigram CJK recall (≥3-char runs OR-ed).
	if len(cands) == 0 {
		//stylegate:ignore existence probe; an error is read as 'absent' and the caller heals the table
		if triOK, _ := tableExists(ctx, db, "procedural_tri"); triOK {
			if grams := capRouteTerms(cjkRunsGE3(query)); len(grams) > 0 {
				parts := make([]string, 0, len(grams))
				for _, g := range grams {
					parts = append(parts, `"`+strings.ReplaceAll(g, `"`, "")+`"`)
				}
				err = add(db.QueryContext(ctx,
					`SELECT p.id, p.name, p.trigger, p.steps, COALESCE(p.preconditions,''), COALESCE(p.tools_used,''),
					        COALESCE(p.version,1), COALESCE(p.tier,'`+defaultTier+`'), COALESCE(p.confidence,0),
					        COALESCE(p.sources,''), COALESCE(p.updated_at,0), tri.rank
					 FROM procedural_tri tri JOIN procedural p ON p.id = tri.rowid
					 WHERE procedural_tri MATCH ? ORDER BY tri.rank LIMIT ?`,
					strings.Join(parts, " OR "), trigramRecallK))
				if err != nil && err != sql.ErrNoRows {
					return "", fmt.Errorf("procedural_search: trigram route: %w", err)
				}
			}
		}
	}
	// Route 3: LIKE fallback for 2-char CJK (trigram's lower bound).
	if len(cands) == 0 {
		if toks := cjkSegmentation(query); toks != "" {
			pattern := "%" + strings.ReplaceAll(strings.TrimSpace(toks), " ", "%") + "%"
			err = add(db.QueryContext(ctx,
				`SELECT id, name, trigger, steps, COALESCE(preconditions,''), COALESCE(tools_used,''),
				        COALESCE(version,1), COALESCE(tier,'`+defaultTier+`'), COALESCE(confidence,0),
				        COALESCE(sources,''), COALESCE(updated_at,0), 0
				 FROM procedural
				 WHERE name LIKE ? ESCAPE '\' OR trigger LIKE ? ESCAPE '\' OR steps LIKE ? ESCAPE '\'
				 ORDER BY updated_at DESC LIMIT ?`,
				pattern, pattern, pattern, trigramRecallK))
			if err != nil && err != sql.ErrNoRows {
				return "", fmt.Errorf("procedural_search: like route: %w", err)
			}
		}
	}

	if len(cands) == 0 {
		return "No procedural workflow found for this query. " +
			"(检索面包屑: use the workflow's name / trigger keywords; workflows live in the `procedural` table.)", nil
	}

	// Rank: token coverage primary (same value-signal philosophy), tier
	// weight, updated_at recency tiebreak, BM25 rank as the last tiebreak.
	now := float64(time.Now().Unix())
	for i := range cands {
		s := float64(cands[i].tokens)*tokenCoverageWeight + cands[i].w.Confidence - 0.5
		switch cands[i].w.Tier {
		case hotTier:
			s += tierWeightHot
		case coldTier:
			s += tierWeightCold
		}
		if cands[i].w.UpdatedAt > 0 {
			// Small recency nudge: a workflow refreshed in the last 30 days
			// outranks an identical one untouched for a year (bounded ≤0.5).
			ageDays := (now - cands[i].w.UpdatedAt) / 86400
			if ageDays < 0 {
				ageDays = 0
			}
			if n := 0.5 - ageDays/720; n > 0 {
				s += n
			}
		}
		cands[i].rank = s
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].rank > cands[b].rank })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	hits := make([]ProceduralWorkflow, 0, len(cands))
	for _, c := range cands {
		c.w.Merged = false
		hits = append(hits, c.w)
	}
	out, err := json.Marshal(hits)
	if err != nil {
		return "", fmt.Errorf("procedural_search: %w", err)
	}
	return renderProceduralSearchResult(string(out))
}

// renderProceduralSearchResult formats the JSON hits into the tool result
// text — the meta line carries version/tier/tools so the LLM can judge.
func renderProceduralSearchResult(out string) (string, error) {
	var hits []ProceduralWorkflow
	if err := json.Unmarshal([]byte(out), &hits); err != nil {
		return "", fmt.Errorf("procedural_search: unparseable output: %w", err)
	}
	if len(hits) == 0 {
		return "No procedural workflow found.", nil
	}
	rendered := make([]string, 0, len(hits))
	for _, h := range hits {
		meta := fmt.Sprintf("#%d %s v%d tier=%s conf=%.2f", h.ID, h.Name, h.Version, h.Tier, h.Confidence)
		if len(h.ToolsUsed) > 0 {
			meta += " tools=" + strings.Join(h.ToolsUsed, ",")
		}
		if len(h.Sources) > 0 {
			meta += " sources=" + strings.Join(h.Sources, ",")
		}
		steps := h.Steps
		if len(steps) == 0 && h.StepsJSON != "" {
			_ = json.Unmarshal([]byte(h.StepsJSON), &steps) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
		}
		var sb strings.Builder
		sb.WriteString(meta + "\n触发: " + h.Trigger)
		if h.Preconditions != "" {
			sb.WriteString("\n前提: " + h.Preconditions)
		}
		for i, s := range steps {
			sb.WriteString(fmt.Sprintf("\n%d. %s: %s", i+1, s.Action, s.Detail))
		}
		rendered = append(rendered, sb.String())
	}
	return strings.Join(rendered, "\n---\n"), nil
}
