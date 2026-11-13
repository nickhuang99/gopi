package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syncTestDB prepares an isolated DB (GOPI_MEMORY_DB + GOPI_HOME) and
// returns its path.
func syncTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", dir)
	return dbPath
}

// TestSyncExportLocalNonNode — the export gate: every LOCAL knowledge row
// (brew, hermes-import, empty-source early-dev) travels; only node:*
// imports stay home (no ping-pong). An explicit --source tag still narrows.
func TestSyncExportLocalNonNode(t *testing.T) {
	dbPath := syncTestDB(t)

	// A human/empty-source row — the class the wide gate exists to share
	// (early-dev distilled lessons carry no source tag).
	if _, err := MemoryStore("human topic", "how to test export filter", "human answer", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryStore("brew: exported one", "export filter question A", "answer A", BrewSourceTag, []string{"kwA1", "kwA2", "kwA3"}); err != nil {
		t.Fatal(err)
	}
	// A hermes-import row — non-brew source tag, local knowledge, must travel.
	if _, err := MemoryStore("hermes: skill", "hermes import filter question", "answer hermes", "hermes-skills-import", nil); err != nil {
		t.Fatal(err)
	}
	// An imported row (node:other) must NOT re-export.
	if _, err := MemoryStore("brew: imported row", "imported question C", "answer C", "brew,node:other", nil); err != nil {
		t.Fatal(err)
	}

	patch, stats, err := SyncExport(SyncExportOptions{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Rows) != 3 {
		t.Fatalf("want 3 local rows exported (empty-source + brew + hermes), got %d: %+v", len(patch.Rows), patch.Rows)
	}
	if patch.Node == "" {
		t.Fatal("patch.Node empty — hostname stamping failed")
	}
	if stats.Scanned != 3 {
		t.Errorf("Scanned: want 3 (own local rows; node:* import excluded pre-filter), got %d", stats.Scanned)
	}
	exported := map[string]bool{}
	for _, r := range patch.Rows {
		exported[r.Question] = true
		if r.Topic == "" || r.Question == "" || r.Answer == "" {
			t.Errorf("patch row missing content: %+v", r)
		}
	}
	if !exported["how to test export filter"] || !exported["export filter question A"] || !exported["hermes import filter question"] {
		t.Errorf("wide gate missing a local row class: %v", exported)
	}
	if exported["imported question C"] {
		t.Error("node:* row exported — anti-reflow violated")
	}

	// Narrowing: --source brew restricts the SAME export to brew rows only.
	brewPatch, _, err := SyncExport(SyncExportOptions{DBPath: dbPath, Source: BrewSourceTag})
	if err != nil {
		t.Fatal(err)
	}
	if len(brewPatch.Rows) != 1 || brewPatch.Rows[0].Question != "export filter question A" {
		t.Fatalf("--source brew: want only question A, got %d rows %+v", len(brewPatch.Rows), brewPatch.Rows)
	}
}

// TestSyncImportIdempotent — importing the same patch twice stores rows once;
// the second pass dedups (rows are never duplicated).
func TestSyncImportIdempotent(t *testing.T) {
	dbPath := syncTestDB(t)

	patch := &SyncPatch{
		Version: syncPatchVersion,
		Node:    "hp-test",
		Rows: []SyncRow{
			{Topic: "brew: lesson one", Question: "idempotency question one?", Answer: "answer one", Keywords: []string{"k1", "k2", "k3"}},
			{Topic: "brew: lesson two", Question: "idempotency question two?", Answer: "answer two", Keywords: []string{"k4", "k5", "k6"}},
		},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatal(err)
	}

	s1, err := SyncImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if s1.RowsImported != 2 || s1.RowsDeduped != 0 {
		t.Fatalf("first import: want imported=2 deduped=0, got %+v", s1)
	}
	s2, err := SyncImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if s2.RowsImported != 0 || s2.RowsDeduped != 2 {
		t.Fatalf("second import: want imported=0 deduped=2, got %+v", s2)
	}
	// DB really holds only 2 rows.
	if n := countDistilledRows(t, dbPath); n != 2 {
		t.Fatalf("want 2 rows in DB after double import, got %d", n)
	}
}

// TestSyncImportNodeTag — imported rows carry sources=node:<exporter>, and a
// re-import unions the tag set instead of duplicating it.
func TestSyncImportNodeTag(t *testing.T) {
	dbPath := syncTestDB(t)

	patch := &SyncPatch{Version: syncPatchVersion, Node: "hp-test", Rows: []SyncRow{
		{Topic: "brew: tagged", Question: "node tag question?", Answer: "node tag answer"},
	}}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncImport(patchPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncImport(patchPath); err != nil { // re-import: sources merge
		t.Fatal(err)
	}
	got := querySources(t, dbPath, "node tag question?")
	if !strings.Contains(got, "node:hp-test") {
		t.Fatalf("imported row missing node:hp-test tag, sources=%q", got)
	}
	if strings.Count(got, "node:hp-test") != 1 {
		t.Fatalf("node tag duplicated on re-import: %q", got)
	}
}

// TestSyncImportConflictHumanWins — a human row already in the DB wins:
// the patch row dedups into it (sources merged), content untouched.
func TestSyncImportConflictHumanWins(t *testing.T) {
	dbPath := syncTestDB(t)

	if _, err := MemoryStore("human: conflict", "conflict question identical?", "HUMAN answer stands", "", nil); err != nil {
		t.Fatal(err)
	}
	patch := &SyncPatch{Version: syncPatchVersion, Node: "hp-test", Rows: []SyncRow{
		{Topic: "brew: conflict", Question: "conflict question identical?", Answer: "brew answer loses"},
	}}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatal(err)
	}
	s, err := SyncImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	// The patch row is REFUSED, not merged: same question + different topic
	// would otherwise INSERT a brew shadow beside the human row.
	if s.RowsRefused != 1 || s.RowsImported != 0 || s.RowsDeduped != 0 {
		t.Fatalf("conflict: want refused=1 imported=0 deduped=0, got %+v", s)
	}
	// The answer must still be the human one.
	got := queryAnswer(t, dbPath, "conflict question identical?")
	if got != "HUMAN answer stands" {
		t.Fatalf("human row clobbered: %q", got)
	}
}

// TestMemoryStoreLongQuestionDedup — the exact dedup key is
// question[:200]+topic, but the STORED question is the full text; a
// >200-rune question must still dedup on re-store (m4 acceptance caught the
// original bug: 217-rune brew row duplicated on re-import).
func TestMemoryStoreLongQuestionDedup(t *testing.T) {
	syncTestDB(t)

	longQ := strings.Repeat("跨分支缺文档时怎么定位它在哪个提交与分支？怎么用git历史找回文件？", 6) // 34×6 = 204 runes
	if got := len([]rune(longQ)); got <= dedupQuestionRunes {
		t.Fatalf("test question must exceed %d runes, got %d", dedupQuestionRunes, got)
	}
	topic := "brew: long question dedup"
	if _, err := MemoryStore(topic, longQ, "first answer", BrewSourceTag, nil); err != nil {
		t.Fatal(err)
	}
	res, err := MemoryStore(topic, longQ, "second answer", BrewSourceTag, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "deduped") {
		t.Fatalf("long question re-store did NOT dedup: %q", res)
	}
}

// TestSyncImportGarbageRefused — the garbage gate applies to imported rows
// (no distilled content → refused, counted, no crash).
func TestSyncImportGarbageRefused(t *testing.T) {
	syncTestDB(t)

	patch := &SyncPatch{Version: syncPatchVersion, Node: "hp-test", Rows: []SyncRow{
		{Topic: "brew: garbage", Question: "h", Answer: "i"}, // < minMeaningfulChars
		{Topic: "brew: real", Question: "how does the sync gate reject garbage rows", Answer: "the garbage gate checks the question and answer for distilled knowledge content and refuses rows without any"},
	}}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatal(err)
	}
	s, err := SyncImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if s.RowsRefused != 1 {
		t.Fatalf("want 1 refused (garbage), got %+v", s)
	}
	if s.RowsImported != 1 {
		t.Fatalf("want 1 imported, got %+v", s)
	}
}

// TestSyncPatchVersionGuard — a patch with a wrong version is rejected.
func TestSyncPatchVersionGuard(t *testing.T) {
	syncTestDB(t)
	data := []byte(`{"version":99,"node":"x","rows":[]}`)
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := os.WriteFile(patchPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncImport(patchPath); err == nil {
		t.Fatal("want version error, got nil")
	}

}

// --- helpers -------------------------------------------------------------

func countDistilledRows(t *testing.T, dbPath string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openStoreDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func querySources(t *testing.T, dbPath, question string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openStoreDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var s string
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(sources,'') FROM distilled_qa WHERE question=?", question).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func queryAnswer(t *testing.T, dbPath, question string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openStoreDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var s string
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(answer,'') FROM distilled_qa WHERE question=?", question).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSyncPatchVersionCompatSemantics — the epic's import gate: Version must
// be judged by compatibility semantics, not just "is it exactly mine", and
// every mismatch must REFUSE loudly (Go's json silently drops unknown
// fields — a newer-format patch imported by an older binary would lose data
// with no error).
func TestSyncPatchVersionCompatSemantics(t *testing.T) {
	cases := []struct {
		name    string
		version int
		wantIn  string // substring the error must carry
	}{
		{"newer format refused loudly", 2, "NEWER"},
		{"invalid negative refused", -1, "invalid"},
		{"missing version refused", 0, "missing"},
		{"far-future format refused", 99, "NEWER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := syncCheckPatchVersion("import", tc.version, syncPatchVersion)
			if err == nil {
				t.Fatalf("version %d: want refusal, got nil", tc.version)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("version %d: error %q missing %q", tc.version, err, tc.wantIn)
			}
		})
	}
	// The one happy path: the current format imports cleanly.
	if err := syncCheckPatchVersion("import", 1, syncPatchVersion); err != nil {
		t.Fatalf("current format v1 must pass, got %v", err)
	}
}

// TestSyncImportNewerFormatPatchRefused — E2E shape of the epic: a patch
// written by a FUTURE binary (v2 with fields this build cannot know) is
// rejected explicitly at import, never silently imported with dropped rows.
func TestSyncImportNewerFormatPatchRefused(t *testing.T) {
	dbPath := syncTestDB(t)
	// A v2 brew patch carrying a field v1 code cannot represent — the JSON
	// parses fine (unknown fields ignored), so ONLY the version gate stands
	// between this file and silent field loss.
	data := []byte(`{"version":2,"node":"newer-node","rows":[{"topic":"t","question":"q","answer":"a","future_field":{"x":1}}]}`)
	patchPath := filepath.Join(t.TempDir(), "future.json")
	if err := os.WriteFile(patchPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := SyncImport(patchPath)
	if err == nil {
		t.Fatal("future-format patch must be refused, not silently imported")
	}
	if !strings.Contains(err.Error(), "NEWER") || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("refusal must name the format problem and the fix, got: %v", err)
	}
	// And the DB must be untouched — refusal happens before any row write.
	if n := countDistilledRows(t, dbPath); n != 0 {
		t.Fatalf("refused patch must write nothing, got %d rows", n)
	}
}

// TestSyncImportVersionlessPatchRefused — a hand-written or future patch
// without a version field is refused (v0 == missing, never "assume v1").
func TestSyncImportVersionlessPatchRefused(t *testing.T) {
	dbPath := syncTestDB(t)
	data := []byte(`{"node":"x","rows":[{"topic":"t","question":"q","answer":"a"}]}`)
	patchPath := filepath.Join(t.TempDir(), "noversion.json")
	if err := os.WriteFile(patchPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncImport(patchPath); err == nil {
		t.Fatal("version-less patch must be refused")
	}
	if n := countDistilledRows(t, dbPath); n != 0 {
		t.Fatalf("refused patch must write nothing, got %d rows", n)
	}
}

// TestSyncExportStampsGenerator — the export header carries the producing
// binary's identity (VCS rev) so a refusing import can name what to upgrade.
func TestSyncExportStampsGenerator(t *testing.T) {
	dbPath := syncTestDB(t)
	if _, err := MemoryStore("brew: gen", "generator stamp question", "answer", BrewSourceTag, nil); err != nil {
		t.Fatal(err)
	}
	patch, _, err := SyncExport(SyncExportOptions{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if patch.Generator == "" {
		t.Fatal("export must stamp the generator version")
	}
}
