package tools

// Regression suite for the LATENT provenance harvest
// (internal/tools/memory_provenance_latent.go). The claim under test is
// narrow and easy to fake, so every clause of it is pinned here:
//
//   - the rule FIRES only on a row that declares a file-shaped path as its
//     topic heading AND repeats it in its own question/answer;
//   - it REFUSES a row that merely mentions a path (the fabrication this work
//     exists to prevent) — the negative case is the more important test;
//   - the write is ADDITIVE: sources, source_type, topic, question, answer and
//     search_kw are byte-identical before and after;
//   - the write is IDEMPOTENT: a second pass writes 0 rows;
//   - the honest `none` verdict is REACHABLE and is what an undefendable row
//     gets — never a filled-in guess;
//   - the printed candidate SQL reproduces the Go predicate's population
//     (acceptance criterion a: a re-runnable query that sums to the table).

import (
	"path/filepath"
	"strings"
	"testing"
)

// latentHeadingFixture pins the HEADING predicate: which topics declare a
// file-shaped path, and which must be refused.
func latentHeadingFixture() []struct {
	topic string
	want  string
	note  string
} {
	return []struct {
		topic string
		want  string
		note  string
	}{
		{"hermes_cli/main.py — CLI dispatcher & update/TUI machinery", "hermes_cli/main.py",
			"the measured dominant shape: path, em-dash, description"},
		{"internal/agent/clock.go", "internal/agent/clock.go",
			"a bare heading with no description at all (measured live row 924)"},
		{"cmd/agent/main.go: the CLI entry", "cmd/agent/main.go",
			"colon separator"},
		{"devdoc/standards-gate.md — the six gates", "devdoc/standards-gate.md",
			"a docs path"},
		{"SQLite FTS5 trigram CJK retrieval在 Go memory 引擎", "",
			"CJK prose: no path, nothing to harvest (measured live row 5)"},
		{"报告驱动 memory 子系统修复套路（round-2）", "",
			"pure CJK topic"},
		{"Merge-conflict resolution in a gate allow-list (internal/standards/baseline.go): take it", "",
			"PATH MENTIONED IN PROSE, not as the heading — the measured live row 1825 the loose LIKE mirror misfiled"},
		{"feat/resume-arm-trim (gopi session resume impl)", "",
			"a branch reference is not a file-shaped path (no file extension)"},
		{"https://example.com/docs/page.md", "",
			"a URL is not a filesystem artifact"},
		{"smoke topic", "", "no path at all"},
		{"hermes_cli/plugins.py — plugin manager & loader", "hermes_cli/plugins.py",
			"the briefing's own example row"},
	}
}

// TestLatentHeadingPredicate pins the heading rule, including the refusal cases.
func TestLatentHeadingPredicate(t *testing.T) {
	for _, fx := range latentHeadingFixture() {
		if got := latentHeadingToken(fx.topic); got != fx.want {
			t.Errorf("latentHeadingToken(%.50q) = %q, want %q (%s)", fx.topic, got, fx.want, fx.note)
		}
	}
}

// TestLatentHarvestRequiresRepeat is the ANTI-FABRICATION test. The heading rule
// alone is not the harvest rule: a row must ALSO repeat the path in its own
// question or answer. A row that merely heads with a path it never returns to is
// refused — that is what stops a prose mention from being recorded as an origin.
func TestLatentHarvestRequiresRepeat(t *testing.T) {
	const topic = "hermes_cli/plugins.py — plugin manager & loader"

	// Positive: question repeats the path → harvested.
	if _, ev, ok := latentHarvestRow(topic,
		"how does hermes_cli/plugins.py discover plugins", "it scans the directory"); !ok {
		t.Errorf("a row whose question repeats the heading path must be harvested")
	} else if ev != "hermes_cli/plugins.py" {
		t.Errorf("evidence = %q, want the heading path", ev)
	}
	// Positive: answer repeats the path.
	if _, _, ok := latentHarvestRow(topic, "how are plugins discovered",
		"see hermes_cli/plugins.py for the loader loop"); !ok {
		t.Errorf("a row whose answer repeats the heading path must be harvested")
	}
	// Negative: nothing repeats it → REFUSED (no label, no evidence).
	if _, _, ok := latentHarvestRow(topic, "how are plugins discovered",
		"it scans a directory and imports modules"); ok {
		t.Errorf("a row that never repeats the heading path must NOT be harvested")
	}
	// Negative: the mention is in the ANSWER but the topic does not head with a
	// path — the measured mention-only population.
	if _, _, ok := latentHarvestRow("CJK retrieval pitfalls",
		"how to fix", "already implemented in internal/tools/memory.go"); ok {
		t.Errorf("a path merely MENTIONED in the answer must NOT be harvested")
	}
}

// TestLatentHarvestIsAdditiveAndIdempotent is the core acceptance test: the
// protected columns are byte-identical after the harvest, and a second pass
// writes 0 rows.
func TestLatentHarvestIsAdditiveAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)

	seeds := []struct{ topic, question, answer string }{
		{"hermes_cli/main.py — CLI dispatcher", "what does hermes_cli/main.py provide",
			"an argparse dispatcher with many subcommands for the hermes CLI entry"},
		{"internal/agent/clock.go", "what does the session clock expose",
			"internal/agent/clock.go holds SessionClock with NewSessionClock and Elapsed"},
		{"CJK retrieval pitfalls", "how to fix CJK substring search",
			"the fix landed in internal/tools/memory.go and uses a trigram index"},
		{"smoke topic", "a smoke question naming nothing",
			"a smoke answer naming no artifact anywhere in this body at all"},
	}
	for _, s := range seeds {
		if _, err := MemoryStore(s.topic, s.question, s.answer, "", nil); err != nil {
			t.Fatalf("seed %q: %v", s.topic, err)
		}
	}

	db := openRawDB(t, path)
	// fingerprint = every column the harvest must NOT touch.
	fingerprint := func() map[int]string {
		out := map[int]string{}
		rows, err := db.Query("SELECT id, topic, question, answer, coalesce(sources,''), coalesce(search_kw,''), coalesce(" +
			provenanceSourceTypeCol + ",'') FROM distilled_qa ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var topic, question, answer, sources, kw, st string
			if err := rows.Scan(&id, &topic, &question, &answer, &sources, &kw, &st); err != nil {
				t.Fatal(err)
			}
			out[id] = strings.Join([]string{topic, question, answer, sources, kw, st}, "\x00")
		}
		return out
	}
	// A recorded-origin row must be restated, never re-labelled. It is seeded
	// and classified BEFORE the baseline fingerprint: the recorded-source
	// column is part of the protected set the harvest must not move, so the
	// baseline has to be taken once it exists.
	if _, err := MemoryStore("recorded row", "a question with a recorded origin here",
		"an answer body long enough for the quality gate to accept the row", "internal/tools/memory.go", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryProvenanceBackfill(); err != nil {
		t.Fatal(err)
	}
	before := fingerprint()

	first, err := MemoryProvenanceLatentHarvest()
	if err != nil {
		t.Fatalf("first harvest: %v", err)
	}
	if first.Harvested != 2 {
		t.Errorf("harvested=%d, want 2 (the two rows declaring+repeating a path)", first.Harvested)
	}
	if first.Harvestable != 2 {
		t.Errorf("harvestable=%d, want 2 (the rule's defensible population)", first.Harvestable)
	}
	if first.Unharvestable != 2 {
		t.Errorf("unharvestable=%d, want 2 (CJK pitfalls + smoke topic)", first.Unharvestable)
	}
	if first.Written != first.Total {
		t.Errorf("first pass written=%d, want every row (%d) — including the honest 'none' verdicts",
			first.Written, first.Total)
	}

	// ADDITIVE: not one protected byte moved.
	after := fingerprint()
	for id, v := range before {
		if after[id] != v {
			t.Errorf("row %d protected columns changed — the harvest must be additive:\n  before %q\n  after  %q",
				id, v, after[id])
		}
	}
	// The undefendable rows carry the honest `none`, with an EMPTY latent type.
	var noneType, noneCount int
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa WHERE "+latentProvenanceCol+"=? AND "+latentSourceTypeCol+"=''",
		latentProvNone).Scan(&noneCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa WHERE "+latentSourceTypeCol+"=?",
		provenanceTypeLocalPath).Scan(&noneType); err != nil {
		t.Fatal(err)
	}
	if noneCount != 2 {
		t.Errorf("provenance='none' with an empty type = %d, want 2 (never a guessed label)", noneCount)
	}
	if noneType != 3 { // 2 harvested + the 1 recorded row restated from source_type
		t.Errorf("local_path latent rows = %d, want 3 (2 harvested + 1 recorded restated)", noneType)
	}

	// IDEMPOTENT: the second pass writes NOTHING and harvests nothing new.
	second, err := MemoryProvenanceLatentHarvest()
	if err != nil {
		t.Fatalf("second harvest: %v", err)
	}
	if second.Written != 0 {
		t.Errorf("second pass wrote %d rows, want 0 (idempotent)", second.Written)
	}
	if second.Harvested != 0 {
		t.Errorf("second pass harvested=%d, want 0", second.Harvested)
	}
	if second.Untouched != second.Total {
		t.Errorf("second pass untouched=%d, want total=%d", second.Untouched, second.Total)
	}
	if second.Harvestable != first.Harvestable {
		t.Errorf("the defensible population changed between passes: %d → %d", first.Harvestable, second.Harvestable)
	}
}

// TestLatentCandidateSQLMatchesTheRule is acceptance criterion (a) as a test:
// the printed candidate query — run against the SAME database the Go predicate
// scanned — must return exactly the number of rows the rule defends. If the
// printed SQL drifts from the rule (the loose-LIKE mistake this file's history
// records), this fails rather than shipping a query that reproduces a different
// population than the harvest.
func TestLatentCandidateSQLMatchesTheRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	seeds := []struct{ topic, question, answer string }{
		{"hermes_cli/main.py — CLI dispatcher", "what does hermes_cli/main.py provide",
			"an argparse dispatcher with many subcommands for the hermes CLI entry"},
		{"internal/agent/clock.go", "what does the session clock expose",
			"internal/agent/clock.go holds SessionClock with NewSessionClock and Elapsed"},
		{"Merge conflict in a gate allow-list (internal/standards/baseline.go): take it",
			"how do I resolve this conflict", "the resolver case re-points internal/standards/baseline.go"},
		{"smoke topic", "a smoke question naming nothing",
			"a smoke answer naming no artifact anywhere in this body at all"},
	}
	for _, s := range seeds {
		if _, err := MemoryStore(s.topic, s.question, s.answer, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := MemoryProvenanceLatentCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Harvestable != 2 {
		t.Errorf("Go rule harvestable=%d, want 2", rep.Harvestable)
	}

	// Run the PRINTED SQL through the same DB.
	db := openRawDB(t, path)
	var sqlCount int
	if err := db.QueryRow(LatentCandidateSQL()).Scan(&sqlCount); err != nil {
		t.Fatalf("printed candidate SQL failed to run: %v\n%s", err, LatentCandidateSQL())
	}
	if sqlCount != rep.Harvestable {
		t.Errorf("printed SQL counts %d candidates, the Go rule defends %d — the mirror has drifted\n%s",
			sqlCount, rep.Harvestable, LatentCandidateSQL())
	}
	// The query must also account for the whole table through its own filter.
	var emptySources int
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa WHERE trim(coalesce(sources,''))=''").Scan(&emptySources); err != nil {
		t.Fatal(err)
	}
	if rep.EmptySources != emptySources {
		t.Errorf("candidate report empty_sources=%d, table says %d", rep.EmptySources, emptySources)
	}
	if rep.Harvestable+rep.Unharvestable != rep.EmptySources {
		t.Errorf("harvestable(%d)+unharvestable(%d) != empty_sources(%d) — the populations must sum",
			rep.Harvestable, rep.Unharvestable, rep.EmptySources)
	}
	if rep.Total != rep.Recorded+rep.EmptySources {
		t.Errorf("recorded(%d)+empty(%d) != total(%d)", rep.Recorded, rep.EmptySources, rep.Total)
	}
}

// TestLatentReadModeNeverWrites: the candidate report is a READ. On a corpus
// that has never been harvested it must still answer (reporting the population)
// without creating the columns — a read must never ALTER the production DB.
func TestLatentReadModeNeverWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	if _, err := MemoryStore("hermes_cli/main.py — entry", "what does hermes_cli/main.py do",
		"it dispatches subcommands for the hermes CLI entry point at startup", "", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	cols, err := tableColumns(t.Context(), db, "distilled_qa")
	if err != nil {
		t.Fatal(err)
	}
	if cols[latentProvenanceCol] {
		t.Fatal("precondition: the latent column must not exist yet")
	}
	rep, err := MemoryProvenanceLatentCandidates()
	if err != nil {
		t.Fatalf("read-only candidate report failed on an unharvested DB: %v", err)
	}
	if rep.Harvestable != 1 {
		t.Errorf("harvestable=%d, want 1", rep.Harvestable)
	}
	cols2, err := tableColumns(t.Context(), db, "distilled_qa")
	if err != nil {
		t.Fatal(err)
	}
	if cols2[latentProvenanceCol] {
		t.Error("the read-only report CREATED the latent column — a report must never ALTER the DB")
	}
}

// TestLatentVerdictsAreDocumented: every provenance marker the harvest can write
// has a constant and the CLI renders it — a marker added without wiring would
// print as an unlabelled blank.
func TestLatentVerdictsAreDocumented(t *testing.T) {
	for _, m := range []string{latentProvHarvested, latentProvRecorded, latentProvNone} {
		if m == "" {
			t.Error("a provenance marker constant is empty")
		}
	}
	if latentHarvestRule().Provenance != latentProvHarvested {
		t.Errorf("the harvest rule's marker (%q) must be the one the scan writes",
			latentHarvestRule().Provenance)
	}
	if latentHarvestRule().Why == "" {
		t.Error("the harvest rule carries no recorded justification")
	}
}

// TestLatentResidualBasesSum is the guard the hand-typed residual lacked: the
// four refusal bases the report prints MUST partition the unharvestable rows
// (sum to Unharvestable) and MUST each land on the fixture they describe. A
// report whose residual numbers do not add up is how a report starts quietly
// disagreeing with its own rule — the exact defect this test closes (the
// devdoc's first cut claimed 401 mention-only rows where the rule's own
// predicate finds 200).
func TestLatentResidualBasesSum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)

	seeds := []struct{ topic, question, answer string }{
		// harvested (NOT a residual base): heading declared + repeated.
		{"hermes_cli/main.py — CLI dispatcher", "what does hermes_cli/main.py provide",
			"an argparse dispatcher dispatching many subcommands for the CLI"},
		// mention-only: a file-shaped path exists, but mid-sentence.
		{"SQLite CJK retrieval", "how to fix CJK substring search in the memory engine",
			"the fix landed in internal/tools/memory.go and uses a trigram index"},
		// token-not-file-shaped: path-shaped tokens, no file extension.
		{"env helper design", "how are env helpers named",
			"env_int/env_float/env_bool are the readers and mode/owner the selector values"},
		// no path token at all.
		{"smoke topic", "a smoke question naming nothing",
			"a smoke answer naming no artifact anywhere in this body at all"},
		// declared but not repeated: clause 4 refuses it.
		{"internal/agent/clock.go", "what does the session clock expose",
			"a wall-clock abstraction with several session-scoped accessors"},
		// bare filename declared AND repeated: refused as non-addressable.
		{"hermes_time.py — timezone-aware clock", "what does hermes_time.py expose",
			"hermes_time.py wraps a timezone-aware session clock"},
	}
	for _, s := range seeds {
		if _, err := MemoryStore(s.topic, s.question, s.answer, "", nil); err != nil {
			t.Fatalf("seed %q: %v", s.topic, err)
		}
	}

	rep, err := MemoryProvenanceLatentHarvest()
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if rep.Harvested != 1 {
		t.Errorf("harvested=%d, want 1 (only the declared-and-repeated row)", rep.Harvested)
	}
	if rep.Unharvestable != 5 {
		t.Errorf("unharvestable=%d, want 5", rep.Unharvestable)
	}
	// Each fixture must land in ITS base — a partition is not enough, the
	// attribution has to be right or the report misleads in a new way.
	if rep.ResidualMentionOnly != 1 {
		t.Errorf("mention_only=%d, want 1 (the CJK row naming memory.go mid-sentence)", rep.ResidualMentionOnly)
	}
	if rep.ResidualTokenNotFileShape != 1 {
		t.Errorf("token_not_file_shaped=%d, want 1 (the env_int/env_float row)", rep.ResidualTokenNotFileShape)
	}
	if rep.ResidualNoPathToken != 1 {
		t.Errorf("no_path_token=%d, want 1 (the smoke row)", rep.ResidualNoPathToken)
	}
	if rep.ResidualDeclaredNoRepeat != 1 {
		t.Errorf("declared_not_repeated=%d, want 1 (clock.go declared, never repeated)", rep.ResidualDeclaredNoRepeat)
	}
	if rep.ResidualBareFilename != 1 {
		t.Errorf("bare_filename=%d, want 1 (hermes_time.py declared and repeated, but not addressable)",
			rep.ResidualBareFilename)
	}
	sum := rep.ResidualNoPathToken + rep.ResidualTokenNotFileShape +
		rep.ResidualMentionOnly + rep.ResidualDeclaredNoRepeat + rep.ResidualBareFilename
	if sum != rep.Unharvestable {
		t.Errorf("residual bases sum to %d but unharvestable is %d — the report's split must partition its own total",
			sum, rep.Unharvestable)
	}
}
