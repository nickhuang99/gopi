package tools

// Regression suite for Rule 16 provenance governance
// (internal/tools/memory_provenance.go). Each test pins one clause of the
// contract the devdoc claims:
//
//   - the classifier partitions the row population (every row gets exactly one
//     type; no row is left unlabelled);
//   - the backfill is ADDITIVE (topic/question/answer/sources unchanged);
//   - the backfill is IDEMPOTENT (a second run fills 0 rows) — the acceptance
//     criterion, asserted rather than asserted-by-hand;
//   - the honest `unknown` bucket exists and is reachable (never a fabricated
//     label to reach 100% fill);
//   - reclassify is a pure function of `sources` (same input → same labels).

import (
	"github.com/nick/gopi/internal/heavytest"
	"path/filepath"
	"testing"
)

// provFixture is one synthetic row: the free-text `sources` value plus the
// source_type the ladder MUST assign it. Every fixture doubles as the
// classifier's specification — a rule change that reclassifies any of these
// fails the test, which is what keeps the ladder honest.
type provFixture struct {
	sources string
	want    string
	note    string
}

// provFixtures covers every rung of the ladder with the shapes measured in the
// live DB (2026-09-11) plus the two edge cases that broke the naive heuristic:
// a URL (contains slashes → would misfile as local_path) and prose mentioning a
// ".md" filename (→ would misfile under a ".md" heuristic).
func provFixtures() []provFixture {
	return []provFixture{
		{"", provenanceTypeUnknown, "empty sources — no origin recorded"},
		{"   ", provenanceTypeUnknown, "whitespace-only is empty too"},
		{"gopi internal/tools/memory.go", provenanceTypeLocalPath, "the dominant measured path shape"},
		{"~/work/GoPi — internal/agent/loop.go, internal/agent/trajectory.go", provenanceTypeLocalPath, "multi-artifact path list"},
		{"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-flash", provenanceTypeURL, "URL: contains '/' and must NOT be a local_path"},
		{"distilled.db导入-Ollama评估", provenanceTypeImport, "the CJK import marker"},
		{"hermes-skills-import", provenanceTypeImport, "the ASCII import marker"},
		{"node:host-b", provenanceTypeNode, "sync-layer peer provenance"},
		{"GoPi session — reply-only Epic goal", provenanceTypeSession, "session label"},
		{"task t-1788769211", provenanceTypeTask, "bare task prefix"},
		{"GoPi host, 2026-09-11, task scratch-gc-watchdog", provenanceTypeDescriptor,
			"names a task by NAME only — no id, so nothing resolves in the ledger (measured shape)"},
		{"verify task t-1788509224, branch feat/verify-write-task-area", provenanceTypeLocalPath,
			"path + task id: the PATH rung wins — a path is the stronger address (measured shape)"},
		{"verify task t-1788509224", provenanceTypeTask,
			"task id with NO path: the mid-string task rule is the only match (why the infix rule exists)"},
		{"epic: VPN lesson memory task", provenanceTypeEpic, "epic label"},
		{"feat/resume-arm-trim (gopi session resume impl)", provenanceTypeGitRef,
			"a bare branch reference: slash, branch prefix, and NO file named (measured live row)"},
		{"feat/memroadmap-m1 devdoc/memroadmap-m1.md", provenanceTypeLocalPath,
			"branch prefix AND a real file: the file-shaped guard keeps this a path (measured live row)"},
		{"git_blame tool", provenanceTypeDescriptor, "the measured descriptor residual (49 live rows)"},
		{"smoke-test", provenanceTypeDescriptor, "a bare test label names no artifact"},
		{"manual", provenanceTypeDescriptor, "a human label, not an address"},
		{"epic: read GOPI.md + git status of gopi-fix worktree", provenanceTypeEpic, "prose with '.md': epic rung wins, and no '/' so it is not a path"},
	}
}

// TestProvenanceLadderAssignsEveryFixture pins the classification of each
// measured shape — the acceptance evidence's automated half.
func TestProvenanceLadderAssignsEveryFixture(t *testing.T) {
	for _, fx := range provFixtures() {
		got, err := provClassifySource(t, fx.sources)
		if err != nil {
			t.Fatalf("classify %q: %v", fx.sources, err)
		}
		if got != fx.want {
			t.Errorf("sources %q → %q, want %q (%s)", fx.sources, got, fx.want, fx.note)
		}
	}
}

// TestProvenanceLadderIsTotal asserts the ladder has NO gap: every fixture —
// including the empty string and pure whitespace — receives one of the known
// types. A NULL/unlabelled result would be the "row silently unclassified"
// defect this work exists to remove.
func TestProvenanceLadderIsTotal(t *testing.T) {
	known := map[string]bool{}
	for _, label := range []string{
		provenanceTypeUnknown, provenanceTypeLocalPath, provenanceTypeURL,
		provenanceTypeGitRef, provenanceTypeImport, provenanceTypeNode,
		provenanceTypeSession, provenanceTypeTask, provenanceTypeEpic,
		provenanceTypeDescriptor,
	} {
		known[label] = true
	}
	for _, fx := range provFixtures() {
		got, err := provClassifySource(t, fx.sources)
		if err != nil {
			t.Fatalf("classify %q: %v", fx.sources, err)
		}
		if !known[got] {
			t.Errorf("sources %q classified as %q — outside the taxonomy", fx.sources, got)
		}
	}
}

// TestProvenanceTaxonomyIsDocumented: every ladder rung has a documented
// meaning (the taxonomy table's machine-readable half must not drift from the
// classifier). A bucket added without a meaning is an undocumented type.
func TestProvenanceTaxonomyIsDocumented(t *testing.T) {
	meanings := ProvenanceTypeMeanings()
	for _, r := range provenanceLadder() {
		if meanings[r.want] == "" {
			t.Errorf("ladder rung %q has no documented meaning", r.want)
		}
		if r.why == "" {
			t.Errorf("ladder rung %q has no recorded justification", r.want)
		}
	}
}

// TestProvenanceBackfillIsAdditiveAndIdempotent is the core acceptance test:
//
//  1. seed rows whose sources span the ladder;
//  2. backfill → every row labelled, and topic/question/answer/sources are
//     byte-identical to the seeded values (additive-only proof);
//  3. backfill AGAIN → filled=0 (idempotency proof).
func TestProvenanceBackfillIsAdditiveAndIdempotent(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)

	seeds := provFixtures()
	for i, fx := range seeds {
		// Question/answer are distinct per row and long enough for the store
		// quality gate; topic carries the row index so the additive check can
		// match rows back one-to-one.
		topic := "prov fixture " + string(rune('a'+i))
		question := "provenance fixture question number " + string(rune('a'+i))
		answer := "provenance fixture answer body long enough to pass the quality gate " + string(rune('a'+i))
		if _, err := MemoryStore(topic, question, answer, fx.sources, nil); err != nil {
			t.Fatalf("seed %q: %v", fx.sources, err)
		}
	}

	// Snapshot the columns that MUST NOT change.
	db := openRawDB(t, path)
	before := map[int]string{} // id → "topic|question|answer|sources"
	rows, err := db.Query("SELECT id, topic, question, answer, coalesce(sources,'') FROM distilled_qa ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int
		var topic, question, answer, sources string
		if err := rows.Scan(&id, &topic, &question, &answer, &sources); err != nil {
			t.Fatal(err)
		}
		before[id] = topic + "|" + question + "|" + answer + "|" + sources
	}
	// The close error is checked, not discarded: the gate counts a bare
	// Close() as a silent failure, and a fixture read that fails to close
	// cleanly should say so rather than be swallowed. This satisfies the
	// unchecked-close rule by construction (a checked close is never bare),
	// so master's //stylegate:ignore escape is not needed here.
	if err := rows.Close(); err != nil {
		t.Fatalf("before-snapshot close: %v", err)
	}

	first, err := MemoryProvenanceBackfill()
	if err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if first.Filled != len(seeds) {
		t.Errorf("first backfill filled=%d, want %d (one per seeded row)", first.Filled, len(seeds))
	}
	// Exactly two fixtures carry no origin text at all ("" and whitespace) —
	// the honest-unknown population of this seed set, asserted rather than
	// assumed so a future rule change that starts inventing labels is caught.
	const wantUnknown = 2
	if first.Unknown != wantUnknown {
		t.Errorf("first backfill unknown=%d, want %d (the empty + whitespace fixtures)", first.Unknown, wantUnknown)
	}

	// 2. Additive proof: the four protected columns are unchanged.
	rows2, err := db.Query("SELECT id, topic, question, answer, coalesce(sources,'') FROM distilled_qa ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	after := map[int]string{}
	for rows2.Next() {
		var id int
		var topic, question, answer, sources string
		if err := rows2.Scan(&id, &topic, &question, &answer, &sources); err != nil {
			t.Fatal(err)
		}
		after[id] = topic + "|" + question + "|" + answer + "|" + sources
	}
	// Same as the before-snapshot above: the close error is checked rather
	// than discarded, which keeps the unchecked-close rule green without an
	// ignore marker.
	if err := rows2.Close(); err != nil {
		t.Fatalf("after-snapshot close: %v", err)
	}
	for id, v := range before {
		if after[id] != v {
			t.Errorf("row %d changed: %q → %q (backfill must be additive)", id, v, after[id])
		}
	}
	// Every row must now carry a non-empty label.
	var unlabelled int
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa WHERE " + provenanceSourceTypeCol + " IS NULL OR " + provenanceSourceTypeCol + "=''").
		Scan(&unlabelled); err != nil {
		t.Fatal(err)
	}
	if unlabelled != 0 {
		t.Errorf("%d rows left unlabelled after backfill", unlabelled)
	}

	// 3. Idempotency proof: the second pass fills nothing.
	second, err := MemoryProvenanceBackfill()
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if second.Filled != 0 {
		t.Errorf("second backfill filled=%d, want 0 (idempotent)", second.Filled)
	}
	if second.Unchanged != second.Scanned {
		t.Errorf("second backfill unchanged=%d, want scanned=%d", second.Unchanged, second.Scanned)
	}
}

// TestProvenanceReclassifyIsPure locks the Rule 16 "derived data is
// recomputable" claim: reclassify over an unchanged sources column produces
// identical labels (pure function), and it MAY relabel a row whose sources was
// edited — the repair path that a fill-only backfill cannot provide.
func TestProvenanceReclassifyIsPure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	if _, err := MemoryStore("t", "a question for the reclassify test", "an answer body long enough to pass the quality gate here", "smoke-test", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryProvenanceBackfill(); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	var id int
	var label string
	if err := db.QueryRow("SELECT id, "+provenanceSourceTypeCol+" FROM distilled_qa").Scan(&id, &label); err != nil {
		t.Fatal(err)
	}
	if label != provenanceTypeDescriptor {
		t.Fatalf("seeded 'smoke-test' classified %q, want %q", label, provenanceTypeDescriptor)
	}

	// Purity: an unchanged sources column re-derives the same label.
	if _, err := MemoryProvenanceReclassify(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT "+provenanceSourceTypeCol+" FROM distilled_qa WHERE id=?", id).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != provenanceTypeDescriptor {
		t.Errorf("reclassify changed an unchanged row: %q", label)
	}

	// Repair: a sources edit is picked up by the NEXT reclassify. This is the
	// documented limit of the fill-only backfill.
	if _, err := db.Exec("UPDATE distilled_qa SET sources='internal/tools/memory.go' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryProvenanceBackfill(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT "+provenanceSourceTypeCol+" FROM distilled_qa WHERE id=?", id).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != provenanceTypeDescriptor {
		t.Errorf("fill-only backfill rewrote an existing label (%q) — it must only fill empties", label)
	}
	if _, err := MemoryProvenanceReclassify(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT "+provenanceSourceTypeCol+" FROM distilled_qa WHERE id=?", id).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != provenanceTypeLocalPath {
		t.Errorf("reclassify did not pick up the sources edit: got %q, want %q", label, provenanceTypeLocalPath)
	}
}

// TestProvenanceAddressabilitySplit pins WHICH types count as addressable under
// Rule 16 — the definition the report's untraceable number is computed from. A
// seatbelt against silently redefining "untraceable" as "not a path".
func TestProvenanceAddressabilitySplit(t *testing.T) {
	addressable := []string{provenanceTypeLocalPath, provenanceTypeURL, provenanceTypeGitRef,
		provenanceTypeNode, provenanceTypeTask, provenanceTypeEpic}
	notAddressable := []string{provenanceTypeImport, provenanceTypeSession, provenanceTypeDescriptor, provenanceTypeUnknown}
	for _, l := range addressable {
		if !ProvenanceTypeAddressable(l) {
			t.Errorf("%q must count as addressable", l)
		}
	}
	for _, l := range notAddressable {
		if ProvenanceTypeAddressable(l) {
			t.Errorf("%q must NOT count as addressable", l)
		}
	}
}

// TestProvenanceCountsSumToPopulation: the verification query's buckets must
// account for every row — the acceptance criterion that the distribution sums
// to the population rather than to whatever the query happened to match.
func TestProvenanceCountsSumToPopulation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	for i, fx := range provFixtures() {
		// The index keeps question+topic unique — MemoryStore dedups identical
		// question+topic pairs, and repeated `want` values would otherwise
		// collapse two fixtures into one row.
		suffix := string(rune('a' + i))
		if _, err := MemoryStore("t "+suffix, "question for the population sum test "+suffix,
			"answer body long enough for the quality gate to accept this row "+suffix, fx.sources, nil); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := MemoryProvenanceShape()
	if err != nil {
		t.Fatal(err)
	}
	var total int
	if err := openRawDB(t, path).QueryRow("SELECT count(*) FROM distilled_qa").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if rep.Total != total {
		t.Errorf("shape total=%d, table count=%d — the distribution does not account for every row", rep.Total, total)
	}
	if rep.Addressable+rep.Untraceable != rep.Total {
		t.Errorf("addressable(%d)+untraceable(%d) != total(%d)", rep.Addressable, rep.Untraceable, rep.Total)
	}
}

// provClassifySource runs the PRODUCTION classifier (the same ladder-rendered
// SQL the backfill's UPDATE uses) against one candidate sources string, through
// an in-memory SQLite so no shared DB is touched. Testing the rendered SQL —
// rather than a Go re-implementation of the ladder — is what makes the fixture
// table evidence about the code that actually runs.
func provClassifySource(t *testing.T, sources string) (string, error) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "classify.db"), false)
	if err != nil {
		return "", err
	}
	defer db.Close()
	caseSQL, args := provenanceCaseSQL()
	// The ladder is written against the `sources` column; a one-row subquery
	// exposes the candidate under that name. SQLite binds placeholders in TEXT
	// order, and the subquery sits AFTER the ladder's markers — so the
	// candidate value is appended last, not prepended.
	q := "SELECT " + caseSQL + " FROM (SELECT ? AS sources)"
	args = append(args, sources)
	var got string
	if err := db.QueryRow(q, args...).Scan(&got); err != nil {
		return "", err
	}
	return got, nil
}

// TestProvenanceLatentSplit pins the honest-residual measurement: an unknown
// row whose own text names an artifact is LATENT (recoverable), not lost. The
// split must be measured on the SAME signature the ladder uses for a path, so
// a looser probe cannot inflate the recoverable count.
func TestProvenanceLatentSplit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	// Row A: no sources, but its topic names a file → latent.
	if _, err := MemoryStore("hermes_cli/plugins.py — plugin manager",
		"how does the plugin manager load plugins from disk",
		"it walks the plugin directory and imports each module found there in order", "", nil); err != nil {
		t.Fatal(err)
	}
	// Row B: no sources and no path anywhere → strongly untraceable.
	if _, err := MemoryStore("smoke topic",
		"a smoke question with no artifact reference at all",
		"a smoke answer that names nothing on the filesystem whatsoever here", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryProvenanceBackfill(); err != nil {
		t.Fatal(err)
	}
	rep, err := MemoryProvenanceLatent()
	if err != nil {
		t.Fatal(err)
	}
	if rep.UnknownRows != 2 {
		t.Errorf("unknown rows = %d, want 2", rep.UnknownRows)
	}
	if rep.LatentRecoverable != 1 {
		t.Errorf("latent recoverable = %d, want 1 (the row naming a file in its topic)", rep.LatentRecoverable)
	}
	if rep.LatentNone != 1 {
		t.Errorf("latent none = %d, want 1 (the row naming nothing)", rep.LatentNone)
	}
	if rep.LatentRecoverable+rep.LatentNone != rep.UnknownRows {
		t.Errorf("latent split does not sum to the unknown population")
	}
}
