package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// Env-leak hermeticity regression tests (epic: test-isolation env leak;
// devdoc hippocampus-repeat-memory-env-leak.md, memory #1618).
//
// newTestAgentEnv used to leave the knowledge-base path overrides to whatever
// the developer's shell had exported. memoryDBPath() prefers them over
// GOPI_HOME, so an export left behind by an e2e run
// (GOPI_MEMORY_DB=<real 1560-row db>) was read straight by
// MemoryHotResident() inside the agent under test: the hot-resident digest
// appeared in the session log and
// TestHippocampus_DefaultOffLeavesBehaviourUnchanged failed spuriously
// ("gate-on session differs from gate-off beyond the hippocampus artifacts").
//
// The tests below are that incident, mechanized: plant a POPULATED database at
// the ambient path, run a real scripted session, and require the agent to see
// nothing of it.
//
// They also close the rename hole behaviourally: the fixture is proven
// INJECTABLE through tools.MemoryHotResident() with memoryDBEnv set to the
// ambient path — i.e. this package's constant is asserted to be the name the
// tools package actually reads. Rename the value on either side and the probe
// (not a source-text scan) fails.
// ---------------------------------------------------------------------------

const (
	// leakFixtureTopic is the distilled_qa topic of the sample's documented HOT
	// row; it is the token whose appearance in a session log means the ambient
	// DB leaked. It is a literal HERE and checked against the sample's own
	// definition by assertFixtureTopicMatchesSample below — checked, not
	// trusted, so the sample and this probe cannot drift silently (Rule 13).
	leakFixtureTopic = "sample-hot-1"
	// leakFixtureAnswer is long enough to satisfy the store gate (used by the
	// sandbox test).
	leakFixtureAnswer = "the test helper pins the knowledge-base path, so an ambient export is never read"
)

// clearKnowledgeEnv removes the three env names from the developer's shell so
// a test can plant EXACTLY ONE of them. Without it a test that plants the
// legacy spelling is hijacked by an ambient GOPI_MEMORY_DB, which outranks it
// in memoryDBPath() — the leak that made this very file fail under an ambient
// export (found in integration verification, not by the tests themselves).
func clearKnowledgeEnv(t *testing.T) {
	t.Helper()
	t.Setenv(memoryDBEnv, "")
	t.Setenv(memoryDBEnvLegacy, "")
	t.Setenv(hippocampusEnvEnabled, "")
}

// seedHotDBFixture points the ambient-path probe at the SHARED READ-ONLY SAMPLE
// and returns its path.
//
// WHY THIS IS THE READER SHAPE (and why the sample had to grow an arm for it).
// The old fixture hand-rolled its own knowledge base: it created
// distilled_qa with the schema spelled out inline and INSERTed one hot row, all
// inside a helper that took the path from t.TempDir() and never pinned
// GOPI_MEMORY_DB. The row it seeded is exactly the kind of fixed, documented
// content a shared sample exists to hold, so the fixture no longer builds one:
// testutil.SampleReportAmbientFixtures(t) provisions a private report root whose
// on-disk ambient fixture carries SampleDocs(), and this returns its path.
//
// What that buys, measured in this very test: the ROW DEFINITION now lives in
// one place (SampleDocs), the schema is built ONCE per run rather than per test,
// and the fixture cannot drift from the sample a sibling test reads. The path is
// still PER CALL — the probe must be free to mutate what it points at (the
// leak assertion is that nothing wrote through it) without damaging the shared
// sample; see testutil.SampleReportAmbientFixtures' doc comment for why a
// private copy, not the sealed sample, is the right artifact here.
//
// The fixture no longer opens the database itself either: testutil owns both
// halves (the path and the rows), so the "opens a DB without pinning the env"
// shape is gone from this file rather than baselined.
func seedHotDBFixture(t *testing.T) string {
	t.Helper()
	return testutil.SampleReportAmbientFixtures(t)
}

// assertFixtureTopicMatchesSample is the check that keeps this probe's topic
// literal equal to the sample's documented one: the sample's rows come from ONE
// definition (testutil.SampleDocs), and a rename there must fail HERE rather
// than quietly making the assertion below vacuous.
func assertFixtureTopicMatchesSample(t *testing.T) {
	t.Helper()
	docs := testutil.SampleDocs()
	if len(docs) == 0 {
		t.Fatalf("the shared sample documents no distilled_qa rows — the leak probe has no subject")
	}
	if docs[0].Topic != leakFixtureTopic || docs[0].Tier != "hot" {
		t.Fatalf("the sample's first documented row is (%q, tier %q), but this probe asserts on (%q, hot) — "+
			"the sample and the probe have drifted", docs[0].Topic, docs[0].Tier, leakFixtureTopic)
	}
}

// fixtureRowCount counts the rows of the fixture DB — the sandbox test's proof
// that nothing wrote into the ambient database.
func fixtureRowCount(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("open fixture for count: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM distilled_qa`).Scan(&n); err != nil {
		t.Fatalf("count fixture rows: %v", err)
	}
	return n
}

// TestTestEnvIsolation_AmbientMemoryDBDoesNotLeak is the incident's regression
// guard: with a POPULATED database exported as GOPI_MEMORY_DB, a session run
// through newTestAgentEnv must still see an isolated, empty knowledge base —
// no hot-resident digest in its log.
//
// Pre-fix this test fails (the fixture's hot row is read and injected);
// post-fix it passes because the helper pins the path into its temp home.
func TestTestEnvIsolation_AmbientMemoryDBDoesNotLeak(t *testing.T) {
	clearKnowledgeEnv(t)
	assertFixtureTopicMatchesSample(t)
	ambient := seedHotDBFixture(t)

	// The ambient export the incident describes, planted BEFORE the helper
	// runs (t.Setenv is inherited by helper and subtest alike).
	t.Setenv(memoryDBEnv, ambient)

	// Fixture is injectable: with the ambient path in force, the tools layer
	// renders the hot row. Without this the absence assertion below could
	// pass vacuously (an unreadable fixture injects nothing anywhere).
	digest := tools.MemoryHotResident()
	if !strings.Contains(digest, leakFixtureTopic) {
		t.Fatalf("fixture not injectable — the assertion would be vacuous (digest=%q)", digest)
	}

	run := runOneScriptedSession(t, false)
	log := normalizeSessionLog(t, run, "", nil)
	if strings.Contains(log, hotResidentMessagePrefix) {
		t.Fatalf("ambient %s leaked into the agent under test — a hot-resident digest was injected:\n%s",
			memoryDBEnv, log)
	}
	if strings.Contains(log, leakFixtureTopic) {
		t.Fatalf("ambient %s leaked into the agent under test — the fixture topic reached the session log:\n%s",
			memoryDBEnv, log)
	}
	if got := os.Getenv(memoryDBEnv); got == ambient {
		t.Fatalf("newTestAgentEnv left the ambient %s in place (%q)", memoryDBEnv, got)
	}
	if home := os.Getenv("GOPI_HOME"); home != "" && !strings.HasPrefix(os.Getenv(memoryDBEnv), home) {
		t.Fatalf("%s=%q is not inside the isolated home %q", memoryDBEnv, os.Getenv(memoryDBEnv), home)
	}
}

// TestTestEnvIsolation_LegacyDBEnvDoesNotLeak covers the second name of the
// same override (GOPI_HIPPO_DB, the legacy alias memoryDBPath() falls back to):
// a leftover of the OLD spelling must not leak either.
func TestTestEnvIsolation_LegacyDBEnvDoesNotLeak(t *testing.T) {
	clearKnowledgeEnv(t)
	assertFixtureTopicMatchesSample(t)
	ambient := seedHotDBFixture(t)
	t.Setenv(memoryDBEnvLegacy, ambient)

	if digest := tools.MemoryHotResident(); !strings.Contains(digest, leakFixtureTopic) {
		t.Fatalf("fixture not injectable under %s — assertion would be vacuous (digest=%q)", memoryDBEnvLegacy, digest)
	}

	run := runOneScriptedSession(t, false)
	if log := normalizeSessionLog(t, run, "", nil); strings.Contains(log, leakFixtureTopic) {
		t.Fatalf("ambient %s leaked into the agent under test (fixture topic injected):\n%s", memoryDBEnvLegacy, log)
	}
	if got := os.Getenv(memoryDBEnvLegacy); got != "" {
		t.Fatalf("newTestAgentEnv left %s set to %q", memoryDBEnvLegacy, got)
	}
}

// TestTestEnvIsolation_ResolvedPathStaysInSandbox pins the write side: after
// newTestAgentEnv, a store lands inside the isolated home and the ambient
// database is left byte-for-byte untouched — the sandbox holds for writes,
// not only for the task-start read.
func TestTestEnvIsolation_ResolvedPathStaysInSandbox(t *testing.T) {
	clearKnowledgeEnv(t)
	ambient := seedHotDBFixture(t)
	before := fixtureRowCount(t, ambient)
	t.Setenv(memoryDBEnv, ambient)

	newTestAgentEnv(t)
	home := os.Getenv("GOPI_HOME")
	if home == "" {
		t.Fatal("newTestAgentEnv did not pin GOPI_HOME")
	}
	if _, err := tools.MemoryStore(leakFixtureTopic, "where does a store land after isolation?", leakFixtureAnswer, "test", nil); err != nil {
		t.Fatalf("store into the isolated base: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "memory.db")); err != nil {
		t.Fatalf("the isolated base was not created under the temp home: %v", err)
	}
	if after := fixtureRowCount(t, ambient); after != before {
		t.Fatalf("the ambient database was written to (%d → %d rows) — the sandbox leaked", before, after)
	}
}
