package tools

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// defect 1 — lesson_type / when_at must be WRITTEN (they were dead columns).
//
// Browser: the store's INSERT switch had no case for either column, so both
// fell to `default:` and every row in production carried "" (measured
// 2026-09-23: 5062/5062 rows empty). The search render prints lesson= and when=
// on every hit, and possiblyStale() reads when_at, so the promise was on the
// tool description while nothing on earth wrote the values.
// ---------------------------------------------------------------------------

// TestMemoryStoreDerivesLessonType: every stored row leaves with a member of
// lessonTypeVocab (never ""), classified from its own question.
func TestMemoryStoreDerivesLessonType(t *testing.T) {
	setMemoryDB(t)
	cases := []struct {
		question string
		want     string
	}{
		{"how do I rebuild the CLI binary", lessonTypeRecipe},
		{"why does the memory search silently miss 踩坑 rows?", lessonTypePitfall},
		{"root cause: the sweeper reaped a live worktree", lessonTypeFailure},
		{"the ledger row keeps its claim until it is released", lessonTypeLesson},
	}
	for i, c := range cases {
		if _, err := MemoryStore(fmt.Sprintf("derived-topic-%d", i), c.question,
			"an answer body long enough to clear the store's garbage gate", "src", nil); err != nil {
			t.Fatalf("store %q: %v", c.question, err)
		}
		got := memoryScalar(t, "SELECT lesson_type FROM distilled_qa WHERE question=?", c.question)
		if got != c.want {
			t.Errorf("lesson_type for %q = %q, want %q", c.question, got, c.want)
		}
		if !slicesContains(lessonTypeVocab, got) {
			t.Errorf("lesson_type %q is outside the closed vocabulary %v", got, lessonTypeVocab)
		}
	}
}

// TestMemoryStoreWhenAtIsWrittenAndRendered: when_at is never empty, a date the
// row's own text states wins, a FUTURE date is refused, and both value signals
// come back out through a real search render (the acceptance the epic states:
// a hit whose header carries lesson= and when=).
func TestMemoryStoreWhenAtIsWrittenAndRendered(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("dated topic", "the worktree vanished on 2026-08-01 during the sweep",
		"an answer body long enough to clear the store's garbage gate", "src", nil); err != nil {
		t.Fatal(err)
	}
	if got := memoryScalar(t, "SELECT when_at FROM distilled_qa WHERE topic='dated topic'"); got != "2026-08-01 00:00:00" {
		t.Errorf("a stated past date must be the when_at marker, got %q", got)
	}

	// A future date is a plan or a version string, not an observation: it must
	// not become the marker (a future when_at would make possiblyStale false
	// forever — the very signal this column exists for).
	if _, err := MemoryStore("future topic", "the rollout is planned for 2099-01-01",
		"an answer body long enough to clear the store's garbage gate", "src", nil); err != nil {
		t.Fatal(err)
	}
	got := memoryScalar(t, "SELECT when_at FROM distilled_qa WHERE topic='future topic'")
	if strings.HasPrefix(got, "2099") {
		t.Errorf("a future date must be ignored, got %q", got)
	}
	ts := parseTimeMarker(got)
	if ts == nil || *ts > float64(time.Now().Unix()) {
		t.Errorf("the fallback marker must be the observation time, got %q", got)
	}

	// The signals reach the rendered search hit.
	out, err := MemorySearch("the worktree vanished during the sweep", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lesson=lesson") {
		t.Errorf("the rendered hit must carry the derived lesson=:\n%s", out)
	}
	// The stated date reaches the render as about=; the write instant is a
	// SEPARATE field (stored=) taken from distilled_at — the split this epic
	// exists for, since when= used to carry whichever of the two came first.
	if !strings.Contains(out, "about=2026-08-01") {
		t.Errorf("the rendered hit must carry the row's stated date as about=:\n%s", out)
	}
	if !strings.Contains(out, "stored=") || strings.Contains(out, "when=") {
		t.Errorf("the rendered hit must carry stored= (distilled_at) and no when=:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// defect 2 — the store must merge a re-worded twin, and must NOT merge two
// different lessons that happen to share a topic.
// ---------------------------------------------------------------------------

// TestMemoryStoreMergesReWordedTwinLesson is the epic's live shape (#1906/#1908,
// #1491/#1494): the same lesson stored twice, re-worded on BOTH the question and
// the answer, filed under overlapping 5W1H breadcrumbs. It produced two ids
// before this fix — the old screen prescreened candidates from question ASCII
// tokens and then compared question against answer[:300], a cross-field score
// that measured 0.181/0.219 for exactly these pairs (below its own 0.5 gate).
func TestMemoryStoreMergesReWordedTwinLesson(t *testing.T) {
	setMemoryDB(t)
	const topic = "worktree sweep reaped a live run"
	first, err := MemoryStore(topic,
		"a freshly created worktree and branch vanished minutes after creation with a live agent inside",
		"MECHANISM: the cleanup listed candidate branches by asking git whether the branch is an ancestor of the integration tip, and a branch created at that tip with zero commits is trivially an ancestor of it. Any sweep that discovers work to delete through that listing therefore reaps a brand-new run: the worktree directory, the ref, the reflog entry and the administrative record under the repository all disappear together, with no archive tag and no error. The damage is silent because the run that owned the worktree is still alive and only notices when its next command cannot resolve the repository. FIX: discover candidates by the branch's own commits and by the age of the worktree, never by ancestry alone, and refuse to collect any worktree whose owner process is still alive.",
		"src-a", []string{"worktree sweep", "merged branch cleanup", "zero-commit branch", "reaped live run"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := MemoryStore(topic,
		"a live session's brand-new linked worktree disappears with no archive and no reflog entry",
		"SYMPTOM: a worktree created at the integration tip with zero commits is gone minutes after it was made, and the directory, the branch ref and the reflog entry are all absent, with nothing in the archive and no error anywhere. CAUSE: the cleanup asked git which branches are ancestors of the integration tip in order to decide what counts as finished work, and a branch made at that tip with zero commits answers yes trivially, so a run that had not committed anything yet looked exactly like work that was already merged and safe to delete. The reaping is silent because the session that created it is still running and only finds out when a later command cannot resolve the repository it is standing in. FIX: decide what to collect by the branch's own commits and by the age of the worktree instead of by ancestry, and never collect a worktree whose owning process is alive.",
		"src-b", []string{"merged branch cleanup", "zero-commit branch", "worktree disappears", "ancestor of master"})
	if err != nil {
		t.Fatal(err)
	}
	id1, id2 := memoryStoredID(t, first), memoryStoredID(t, second)
	if !strings.Contains(second, "deduped") {
		t.Errorf("the re-worded twin must merge into the existing lesson, got %q", second)
	}
	if id1 != id2 {
		t.Errorf("the twin must reuse id %d, got %d", id1, id2)
	}
	if strings.Contains(second, "similarity=0.000") {
		t.Errorf("the merge must report the similarity it decided on, got %q", second)
	}
	if n := memoryRowCount(t); n != 1 {
		t.Errorf("a merged store must leave ONE row, got %d", n)
	}
}

// TestMemoryStoreKeepsDistinctLessonsAboutOneProductSeparate is the regression
// for the OPPOSITE failure — over-merging. Both pairs below are short rows about
// one subject that state DIFFERENT lessons; a whole-surface-only gate merged
// them (measured 0.404 and 0.500 against a 0.40 gate) while the epic's genuine
// twins scored 0.438/0.439. The breadcrumb facet is what tells them apart.
func TestMemoryStoreKeepsDistinctLessonsAboutOneProductSeparate(t *testing.T) {
	setMemoryDB(t)
	a := storeID(t, "deepseek v4 flash", "deepseek v4 flash config?", "notes about deepseek v4 flash configuration")
	b := storeID(t, "deepseek v4 flash", "deepseek v4 flash price?", "notes about deepseek v4 flash pricing and cost")
	if a == b {
		t.Errorf("a config lesson and a price lesson are different lessons, both got id=%d", a)
	}
	c := storeID(t, "vpn", "vpn mtu drops?", "raise the mtu and disable offload")
	d := storeID(t, "vpn", "vpn drops under load?", "raise the mtu")
	if c == d {
		t.Errorf("two different vpn lessons both got id=%d", c)
	}
	if n := memoryRowCount(t); n != 4 {
		t.Errorf("four distinct lessons must leave four rows, got %d", n)
	}
}

// TestFindSameLessonDuplicateReportsABrokenDatabase: the screen's SQL failure is
// an ERROR on the store path (Rule 15) — a screen that read a broken DB as "no
// duplicate" would start duplicating every row silently.
func TestFindSameLessonDuplicateReportsABrokenDatabase(t *testing.T) {
	path := setMemoryDB(t)
	db := openRawDB(t, path)
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor// a closed handle fails every later query
	if _, _, err := findSameLessonDuplicate(t.Context(), db, "topic", "question", "answer"); err == nil {
		t.Error("a failing candidate query must be reported, not read as 'no duplicate'")
	}
	if _, err := MemoryStore("closed", "a question long enough", "an answer body long enough to clear the gate", "src", nil); err != nil {
		t.Fatalf("store on a fresh DB: %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// memoryScalar reads one scalar out of the test's memory DB.
func memoryScalar(t *testing.T, query string, args ...interface{}) string {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(memoryDBPath()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var out sql.NullString
	if err := db.QueryRow(query, args...).Scan(&out); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return out.String
}

// memoryRowCount counts the rows in the test's store.
func memoryRowCount(t *testing.T) int {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(memoryDBPath()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// memoryStoredID parses the id out of a store confirmation.
func memoryStoredID(t *testing.T, out string) int {
	t.Helper()
	var id int
	if _, err := fmt.Sscanf(out, "Memory stored (id=%d", &id); err != nil {
		t.Fatalf("parse store id from %q: %v", out, err)
	}
	return id
}

func slicesContains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
