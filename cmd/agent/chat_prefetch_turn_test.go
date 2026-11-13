package main

// chat_prefetch_turn_test.go — the TURN-BOUNDARY adoption, driven through the
// REAL functions (chatPrefetchAdoptTurn, chatPrefetchTurnTerms, chatSealMessage).
//
// WHAT THESE TESTS ARE FOR. The bug this epic closes is a BLIND SPOT, so the
// tests that matter are the ones that prove the four previously-uncovered input
// shapes now produce a retrieval and a record line — not that a function returns
// a string. Rule 31 governs the real-session evidence (a notify delivery and a
// whole-block paste, driven against the built binary); these tests pin the seams
// those runs exercise, so a regression fails in `go test` rather than only on the
// next live run.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/memoryprefetch"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// The fixture texts. Named (Rule 1) because a test that re-typed them inline
// would let the "the section came from the PAYLOAD, not from the reference line"
// claim drift away from the bytes it is made about.
const (
	// turnPayloadBody is the body a whole-block paste spills to disk. Its words
	// are the retrieval key's words, and they appear NOWHERE in the reference
	// line the editor leaves behind.
	turnPayloadBody = "memory prefetch turn boundary adoption across notify and paste"
	// turnNoticeText is what `gopi notify --session … --message …` delivers.
	turnNoticeText = "the notify delivered retrieval key reaches the turn boundary"
	// turnJobCommand is the command a background job's completion notice names.
	turnJobCommand = "go test ./internal/memoryprefetch -run TurnBoundary"
)

// prefetchFixtureRecordsPerSubject is HOW MANY DISTINCT records the fixture
// ledger carries per subject, and it is DERIVED from the injection floor rather
// than typed: the floor is read through its ONE reader (memoryprefetch.InjectFloor,
// Rule 14), so raising or lowering the framework floor moves the fixture with it
// instead of leaving a fixture that silently retrieves one hit short.
var prefetchFixtureRecordsPerSubject = memoryprefetch.InjectFloor(memoryprefetch.DefaultConfig())

// turnFixtureSubject is one subject the fixture ledger answers, with TWO records
// for it. The second record is not padding: a query must clear the injection
// floor to produce a section at all, and a subject that carries one row can never
// do it — see seedTurnLedger for the measured shape of that failure.
//
// Every record of a subject carries the subject's own retrieval words in its
// QUESTION and its ANSWER, which is where the prefetch's FTS index looks
// (internal/tools indexes topic+question+answer): the fixture's job is to be a
// ledger the tests' lines really reach, and two records reachable by the same
// query is the shape a ledger has after two lessons on one topic.
type turnFixtureSubject struct {
	topic      string
	question   string
	answers    []string
	tier       string
	confidence float64
}

// turnFixtureSubjects is the fixture's vocabulary, one entry per question the
// tests in this package pose. Each pair answers the SAME subject in its own
// words, so a retrieval that returns both is a set of two lessons rather than a
// duplicated row.
var turnFixtureSubjects = []turnFixtureSubject{
	{
		topic:    "notify turn retrieval key",
		question: "how does a notify delivered turn reach the retrieval key?",
		answers: []string{
			"the notify delivered message verbatim is the retrieval key: the turn boundary reads the whole sealed input, and the key reaches the boundary once, with no framing prose",
			"the retrieval key comes from the sealed input rather than from the framed prose: the notify delivered text reaches the turn boundary, and the boundary reads it verbatim",
		},
		tier: "hot", confidence: 0.9,
	},
	{
		topic:    "turn boundary payload",
		question: "does a notify turn boundary pose its own payload retrieval?",
		answers: []string{
			"yes: a notify turn boundary poses its own payload retrieval, because the typing sampler never saw the delivered message and the turn is the only face that can key it — including the text behind a spilled reference",
			"the turn boundary pays its own payload retrieval for a notify delivery; the typing path built no key for a message that never stood on the line, so the words behind a spilled reference are the boundary's own",
		},
		tier: "hot", confidence: 0.85,
	},
	{
		topic:    "whole block paste payload",
		question: "a whole block paste leaves a reference on the line — what is the retrieval key?",
		answers: []string{
			"read the spilled payload behind the reference: a whole block paste leaves a reference on the line, and the payload behind that reference is the retrieval key",
			"a whole block paste is spilled to a file, so the reference on the line is not the retrieval key; read the payload the spilled reference points at",
		},
		tier: "hot", confidence: 0.8,
	},
	{
		topic:    "memory prefetch adoption",
		question: "where does memory prefetch adoption happen across a whole block paste?",
		answers: []string{
			"memory prefetch adoption across a whole block paste is keyed on the payload, not on the reference the editor leaves on the line",
			"memory prefetch adoption across a whole block paste reads the spilled payload; the reference line alone never carries the retrieval key",
		},
		tier: "warm", confidence: 0.75,
	},
	{
		topic:    "prefetch turn boundary adoption",
		question: "what does the memory prefetch turn boundary read when a notify and a paste arrive together?",
		answers: []string{
			"the memory prefetch turn boundary adoption across notify and paste reads the sealed input, so a delivered message reaches the retrieval key",
			"memory prefetch adoption at the turn boundary across a notify delivery: the paste payload and the delivered text both reach the retrieval key",
		},
		tier: "warm", confidence: 0.7,
	},
}

// turnEnabled builds the ON executor over a fixture ledger, with the switch
// resolved through the same three-channel reader the run uses. It returns the
// executor and the SESSION DIRECTORY whose record file the assertions read.
func turnEnabled(t *testing.T) (*chatPrefetch, string) {
	t.Helper()
	return turnEnabledOver(t, seedTurnLedger)
}

// turnEnabledOver is the same ON executor over a fixture ledger built by the
// caller's seeder: the LOCK rig needs the rollback journal on the FILE (see
// chat_prefetch_dsn_rig_test.go for the measurement), so it is the rig that
// asks for it, and every other turn test keeps production's contract.
func turnEnabledOver(t *testing.T, seed func(*testing.T) string) (*chatPrefetch, string) {
	t.Helper()
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	path := seed(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	dir := t.TempDir()
	p := chatPrefetchFor(dir, nil, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	p.Start(context.Background())
	t.Cleanup(p.Stop)
	return p, dir
}

// TestTurnTermsNeverReadTheFramedProse is the reason the terms come from the ARM
// and not from the sealed message: the boundary's framing carries constant prose
// on every turn of its shape, and a constant candidate would satisfy the gate by
// itself.
func TestTurnTermsNeverReadTheFramedProse(t *testing.T) {
	ext := &tools.ExternalMessage{ID: "d1", Message: turnNoticeText, Source: inputsource.Notify}
	notices := []tools.JobNotice{{External: ext}}
	terms := chatPrefetchTurnTerms(false, notices, "[system] An external process sent this session a message: "+turnNoticeText)
	if len(terms) != 1 || terms[0] != turnNoticeText {
		t.Fatalf("a notify turn's terms = %q, want exactly the delivered text", terms)
	}
	for _, term := range terms {
		if strings.Contains(term, "external process sent") {
			t.Errorf("the constant framing prose entered the retrieval key: %q", term)
		}
	}

	// A background job's completion notice names the COMMAND it ran: that is
	// what the turn is about, and it is the only text the notice carries.
	jobs := []tools.JobNotice{{JobID: "j1", Command: turnJobCommand}}
	if got := chatPrefetchTurnTerms(false, jobs, "[gopi] opened a turn"); len(got) != 1 || got[0] != turnJobCommand {
		t.Fatalf("a job notice turn's terms = %q, want the job's command", got)
	}

	// A MODEL-typed line is its own input (selfDriven): the arm is the evidence,
	// exactly as it is for the source stamped on a reference.
	if got := chatPrefetchTurnTerms(true, notices, "the model typed this line itself"); len(got) != 1 || got[0] != "the model typed this line itself" {
		t.Fatalf("a self-driven turn's terms = %q, want the line", got)
	}

	// An operator line: the line itself.
	if got := chatPrefetchTurnTerms(false, nil, "what does the prefetch do"); len(got) != 1 || got[0] != "what does the prefetch do" {
		t.Fatalf("an operator turn's terms = %q, want the line", got)
	}
}

// TestTurnInputReadsThePayloadBehindAReference is the PASTE half of the blind
// spot: the editor spills the block and leaves a reference, so the words that
// matter are only in the FILE. The reference line's own "words" (a path, a size)
// must not be the key.
func TestTurnInputReadsThePayloadBehindAReference(t *testing.T) {
	dir := t.TempDir()
	store := lineedit.NewPasteStore(dir)
	if store == nil {
		t.Fatal("the fixture paste store could not be built")
	}
	ref, err := store.Spill([]byte(turnPayloadBody))
	if err != nil {
		t.Fatalf("spill the fixture payload: %v", err)
	}
	line := lineedit.PasteRefFor(ref)
	if !lineedit.HasPasteRef(line) {
		t.Fatalf("the spilled reference is not recognised: %q", line)
	}
	out, err := chatSealMessage(&chatSession{sessionDir: dir}, line, true, inputsource.Human)
	if err != nil {
		t.Fatalf("chatSealMessage on a spilled line: %v", err)
	}
	in, failures := chatPrefetchTurnInput(out, nil)
	if len(failures) != 0 {
		t.Fatalf("a readable payload was reported as a failure: %v", failures)
	}
	if len(in.Payloads) != 1 || !strings.Contains(in.Payloads[0], "turn boundary") {
		t.Fatalf("the payload's words did not reach the retrieval key: %+v", in.Payloads)
	}
	text, degs := memoryprefetch.TurnText(in, memoryprefetch.DefaultConfig())
	if !strings.Contains(text, "turn boundary adoption across notify") {
		t.Fatalf("the assembled key omits the payload's own words: %q", text)
	}
	if len(degs) != 0 {
		t.Errorf("a payload inside the bound recorded a degradation: %+v", degs)
	}
}

// TestTurnAdoptionOffPathIsInertWithTheSwitchOff is the OFF half of the
// acceptance: with the knob off the turn-boundary path is not on the path at all
// — no file read, no section, no record — and the message is byte-identical.
func TestTurnAdoptionOffPathIsInertWithTheSwitchOff(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, "")
	prefetchSettings(t, `{"memory_prefetch": false}`)
	dir := t.TempDir()
	store := lineedit.NewPasteStore(dir)
	ref, err := store.Spill([]byte(turnPayloadBody))
	if err != nil {
		t.Fatalf("spill: %v", err)
	}
	out, err := chatSealMessage(&chatSession{sessionDir: dir}, lineedit.PasteRefFor(ref), true, inputsource.Human)
	if err != nil {
		t.Fatalf("chatSealMessage: %v", err)
	}
	before := out.Message
	got, adopted := chatPrefetchAdoptTurn(out, nil, inputsource.Human, []string{turnPayloadBody})
	if adopted {
		t.Error("the OFF path adopted a turn-boundary section")
	}
	if got.Message != before || got.Display != out.Display {
		t.Errorf("the OFF path changed the message:\n got %q\nwant %q", got.Message, before)
	}
	if _, err := os.Stat(filepath.Join(dir, config.MemoryPrefetchRecordName)); !os.IsNotExist(err) {
		t.Errorf("the OFF path wrote a record (%v)", err)
	}
}

// TestTurnAdoptionRecordsTheSource is the acceptance's record requirement: the
// run's observable must name the FACE the retrieved input came from, so a reader
// can tell a `notify`-delivered turn from the operator's own.
func TestTurnAdoptionRecordsTheSource(t *testing.T) {
	p, dir := turnEnabled(t)
	terms := []string{turnNoticeText}
	out := chatOutbound{Message: "[system] An external process sent this session a message: " + turnNoticeText,
		Display: "external message d1"}
	got, adopted := chatPrefetchAdoptTurn(out, p, inputsource.Notify, terms)
	if !adopted {
		t.Fatalf("the turn-boundary retrieval did not adopt its section (record below)")
	}
	if got.Display != out.Display {
		t.Errorf("Display was modified: %q", got.Display)
	}
	if !strings.Contains(got.Prefetch.Section, "memory-prefetch") {
		t.Errorf("the section did not ride the message:\n%s", got.describe())
	}
	rec := readPrefetchRecord(t, dir)
	if !strings.Contains(rec, `"source":"`+inputsource.Notify+`"`) {
		t.Errorf("the record does not name the face the input came from:\n%s", rec)
	}
	if !strings.Contains(rec, `"outcome":"`+prefetchOutcomeAdopted+`"`) || !strings.Contains(rec, prefetchRecordAdoptedYes) {
		t.Errorf("the adoption event is not recorded:\n%s", rec)
	}
}

// TestTurnAdoptionNeverWaitsLongerThanItsBound is the "轮边界不因检索而阻塞"
// requirement, measured: the seal returns within its own bound even when the
// lookup cannot (a ledger held by a writer), and the lookup still records its real
// outcome afterwards. No load is generated (Rule 27 — the holder is a state, not
// a stressor).
func TestTurnAdoptionNeverWaitsLongerThanItsBound(t *testing.T) {
	p, dir := turnEnabledOver(t, seedTurnLedgerInRollbackJournal)
	path := os.Getenv("GOPI_MEMORY_DB")
	// A writer holding the write lock: rollback journal + BEGIN EXCLUSIVE, the
	// shape this feature's own give-up test uses. The ledger is CREATED in that
	// journal mode rather than flipped into it (a flip needs exclusive access to
	// the file — measured: chat_prefetch_dsn_rig_test.go).
	assertLockRigLedgerIsInRollbackJournal(t, path)
	holder, err := sql.Open(prefetchDriverName, tools.LedgerRollbackJournalDSN(path))
	if err != nil {
		t.Fatalf("open the lock holder: %v", err)
	}
	defer holder.Close() //stylegate:ignore fixture handle closed at the end of the test
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE: %v", err)
	}
	defer func() {
		if _, rerr := holder.Exec("ROLLBACK"); rerr != nil {
			t.Logf("rollback the fixture lock: %v", rerr)
		}
	}()

	started := time.Now()
	out, adopted := chatPrefetchAdoptTurn(chatOutbound{Message: turnNoticeText}, p, inputsource.Notify, []string{turnNoticeText})
	elapsed := time.Since(started)
	if adopted {
		t.Error("a blocked ledger produced an adopted section")
	}
	if out.Message != turnNoticeText {
		t.Errorf("the message was changed by a lookup that had nothing: %q", out.describe())
	}
	// The bound is the wait plus this goroutine's own scheduling; a second of
	// slack is generous and still an order of magnitude below the lookup's 1500ms
	// discipline, which is what "the seal does not block on the retrieval" means.
	if elapsed > time.Duration(prefetchTurnWaitMs+1000)*time.Millisecond {
		t.Fatalf("the seal waited %v — past its own %dms bound plus slack", elapsed, prefetchTurnWaitMs)
	}
	// The lookup's own outcome lands when it finishes, recorded, never silent.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rec := readPrefetchRecordIfAny(dir); strings.Contains(rec, prefetchOutcomeGaveUp) {
			return
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep polls for the prefetch give-up record the assertion is about; the record is not written yet
	}
	t.Errorf("the lookup's give-up was never recorded:\n%s", readPrefetchRecordIfAny(dir))
}

// seedTurnLedger builds a distilled-qa ledger whose rows answer THIS file's turn
// texts (the shared fixture answers a different vocabulary, and a retrieval that
// legitimately finds nothing would make every adoption assertion below vacuous).
// It uses the WRITER scaffold's path (Rule 32), exactly as the shared fixture does.
//
// TWO RECORDS PER SUBJECT, and the second record is load-bearing rather than
// padding — the same shape (and for the same reason) as seedPrefetchLedger in
// chat_prefetch_test.go. The injection floor (internal/memoryprefetch,
// Config.MinInjectHits / DefaultMinInjectHits) refuses a retrieval that returns
// fewer hits than the floor OUTRIGHT: InjectSection judges `len(hits)` BEFORE it
// renders, so a fixture whose subject carries ONE row can never produce a
// section, and every test that reads one would then be measuring the floor
// instead of the behaviour it names — the record says `hits=1 outcome=below-floor
// adopted=no` and an "adoption" assertion is vacuous while looking green.
//
// The count is DERIVED from the floor (prefetchFixtureRecordsPerSubject, below)
// and both the count and each answer's length are checked against the constants
// the renderer itself judges, so a floor or MinHitRunes change fails HERE, by
// name, instead of turning these tests into floor tests. The second record of a
// subject answers the SAME question in its own words — the shape a ledger really
// has after two lessons on one topic — so the pair is a SET, not a duplicated row.
// seedTurnLedger builds the same ledger on production's contract — the shape
// every turn test that is not the LOCK rig wants.
func seedTurnLedger(t *testing.T) string {
	t.Helper()
	return seedTurnLedgerThrough(t, func(path string) (*sql.DB, error) {
		return sql.Open(prefetchDriverName, tools.LedgerDSN(path))
	})
}

// seedTurnLedgerThrough is the ONE definition of this fixture's rows: the rows
// live here, the connection contract belongs to the caller's opener (which is
// where the DSN helper call is written, so the suite's contract gate reads it).
func seedTurnLedgerThrough(t *testing.T, open func(string) (*sql.DB, error)) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	db, err := open(path)
	if err != nil {
		t.Fatalf("open fixture ledger: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("close fixture ledger: %v", cerr)
		}
	}()
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE distilled_qa (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL,
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			tier TEXT DEFAULT 'warm',
			confidence REAL DEFAULT 0.5,
			possibly_stale INTEGER DEFAULT 0)`,
		`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, content='distilled_qa', content_rowid='id')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed fixture (%s): %v", stmt, err)
		}
	}
	for _, subject := range turnFixtureSubjects {
		if len(subject.answers) < prefetchFixtureRecordsPerSubject {
			t.Fatalf("the fixture subject %q carries %d record(s) but the injection floor is %d: a retrieval below the floor injects nothing at all, so every test reading a section from this subject would be measuring the floor rather than the adoption it names",
				subject.topic, len(subject.answers), prefetchFixtureRecordsPerSubject)
		}
		for _, answer := range subject.answers {
			// The renderer's own per-hit bound, checked rather than trusted: a
			// body below MinHitRunes is TRIMMED AWAY inside the segment, so a
			// fixture that satisfied the floor with slivers would produce a
			// segment with no hits — the second way to record a refusal that
			// looks like a passing fixture.
			if n := len([]rune(answer)); n < memoryprefetch.DefaultMinHitRunes {
				t.Fatalf("the fixture answer %q is %d rune(s), below the renderer's MinHitRunes=%d (internal/memoryprefetch): a hit this short is dropped inside the budget, so the section would carry a header and nothing else",
					answer, n, memoryprefetch.DefaultMinHitRunes)
			}
			if _, err := db.Exec(
				`INSERT INTO distilled_qa (topic, question, answer, tier, confidence) VALUES (?, ?, ?, ?, ?)`,
				subject.topic, subject.question, answer, subject.tier, subject.confidence); err != nil {
				t.Fatalf("seed fixture subject %q: %v", subject.topic, err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO distilled_qa_fts (rowid, topic, question, answer) SELECT id, topic, question, answer FROM distilled_qa`); err != nil {
		t.Fatalf("index the fixture: %v", err)
	}
	return path
}

// readPrefetchRecordIfAny reads the run's record without failing when it does not
// exist yet: the bound test polls for a line a lookup writes from ITS OWN
// goroutine, so "not there yet" is a state to wait out rather than a defect.
func readPrefetchRecordIfAny(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, config.MemoryPrefetchRecordName))
	if err != nil {
		return ""
	}
	return string(b)
}
