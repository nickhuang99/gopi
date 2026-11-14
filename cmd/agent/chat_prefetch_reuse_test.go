package main

// chat_prefetch_reuse_test.go — ONE SEAL, ONE LOOKUP, counted on the run's own
// record (task 01a115e6, reworked onto master after 8cbddb52a).
//
// THE DEFECT THIS FILE PINS. A line the operator TYPED reaches two machines that
// build the SAME key from it: the typing sampler (chat_prefetch.go's loop over
// internal/memoryprefetch.Session, driven by the published buffer) fetches while
// the operator pauses, and the turn boundary (chat_prefetch_turn.go over
// `p.turnState`, driven by the sealed input) fetches AGAIN at the seal. One
// question, two ledger round-trips, one of them paid for and thrown away (the
// old `discardTypingSection`, recorded as `superseded`).
//
// THE RULING THE MERGED TREE IMPLEMENTS. The two state machines keep SEPARATE
// `Satisfied` sets — merging them is refused, because a turn-boundary retrieval
// would then mark terms satisfied in the machine the operator's NEXT PAUSE is
// judged by, and typing more on the subject the model was just told about would
// fetch nothing (chat_prefetch.go's own comment states it; `State` and `Session`
// are two types for exactly that reason). The redundancy is removed at the
// LOOKUP instead: `takeShared` hands the typing path's pending section to a turn
// boundary whose OWN query is the SAME STRING, and the boundary CONSUMES it
// (Session.Take), so the REPL's fallback cannot append the same block twice.
//
// WHY THE CRITERION IS THE WHOLE KEY, and not "the typing key is a prefix". A
// section fetched for a PREFIX of the line does NOT cover the sealed key, and
// taking it would hand the prompt a section that never carried the turn's own
// words. `Session.Take` refuses it (zero new keywords of the sealed key may be
// missing), the turn then runs its OWN retrieval, and the pause's premium is
// recorded as spent rather than adopted. That is master's pinned behaviour and
// this file does not restate it (`chat_prefetch_shared_test.go:
// TestATypingPauseOnAPartialLineIsPaidForAtThatPause` is its home); what THIS
// file adds is the case that file reaches only as a side effect — ONE pause, at
// which the two keys are EQUAL — measured as the number of LOOKUPS.
//
// WHY THE COUNT IS ON LOOKUP OUTCOMES AND NOT ON ANOTHER KEY. This is the
// episode's own root cause, kept as a test rather than as prose: an earlier
// version of this counter matched `"hits":` — the key `prefetchRecord.Hits`
// serialises — and because adoption and reuse lines carry no lookup of their
// own, counting that string counts EVENTS, not LOOKUPS. A record with one
// lookup and one adoption read as two. The OUTCOME is the event's own name, and
// only four of the vocabulary's members mean "a query ran".
//
// WHAT MAKES THESE REGRESSION TESTS RATHER THAN RESTATEMENTS: they drive the
// production seams (chatPrefetchAdoptTurn, chatPrefetchAdopt,
// chatPrefetchTurnTerms, Session.Claim/fetch) and assert on the RUN'S OWN RECORD
// (memory-prefetch.jsonl), so a tree that fetches twice fails here for the
// artefact's own reason. The pre-fix red is proved in the task's report by
// reverting the subject file and re-running this file unchanged.

import (
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/inputsource"
)

// prefetchLookupOutcomes is the set of record outcomes that mean THIS RUN RAN A
// QUERY — the predicate the count below is built on, named rather than spelled
// inline (Rule 1/13). `shared` is deliberately NOT a member: the event it names
// is the boundary's answer that no query of its own ran, and counting it as a
// lookup is the defect this constant exists to make impossible.
var prefetchLookupOutcomes = []string{
	prefetchOutcomeHits,
	prefetchOutcomeEmpty,
	prefetchOutcomeGaveUp,
	prefetchOutcomeError,
}

// prefetchLookupLineCount counts the LOOKUP lines in a record: the retrievals the
// run actually performed, which is the quantity the ruling is about. Adoption
// (`adopted`), sharing (`shared`) and supersession are events ABOUT a retrieval
// and are deliberately not counted — see the file header for the measured
// failure this predicate replaces.
func prefetchLookupLineCount(rec string) int {
	n := 0
	for _, outcome := range prefetchLookupOutcomes {
		n += strings.Count(rec, `"outcome":"`+outcome+`"`)
	}
	return n
}

// TestOneSealCostsExactlyOneLookup is the defect itself, at the one sequence the
// equality criterion is exactly for: the operator TYPES the whole line, pauses
// long enough for the typing path to fetch (Observe's debounce), and presses
// Enter without typing another character — so the key the pause fetched and the
// key the seal builds are the SAME STRING.
//
// THREE ASSERTIONS, and the third is the one that makes the first two a REUSE
// rather than a deletion: exactly one lookup ran; the sharing is recorded under
// an outcome of its own (never a silent absence, Rule 15); and the section still
// reached the prompt.
func TestOneSealCostsExactlyOneLookup(t *testing.T) {
	p, dir := turnEnabled(t)

	// 1. The typing path, as the executor's own loop runs it: the pause observed,
	//    the claim made, the lookup run, the section left waiting in the session.
	//    ONE lookup so far — asserted before the seal, so a failure names which
	//    half spent it.
	typeAndPause(t, p, sharedSealedLine, time.Now())
	if n := prefetchLookupLineCount(readPrefetchRecord(t, dir)); n != 1 {
		t.Fatalf("after the pause the record carries %d lookup line(s), want exactly 1:\n%s",
			n, readPrefetchRecord(t, dir))
	}

	// 2. The turn boundary, at the seal, for the SAME line. It must NOT retrieve:
	//    the keywords are already bought and the section it would need is the one
	//    the typing path is holding.
	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedSealedLine, Display: sharedSealedLine},
		p, inputsource.Human, chatPrefetchTurnTerms(false, nil, sharedSealedLine))
	if !adopted {
		t.Fatalf("the turn carried no section — the shared one was not handed over:\n%s",
			readPrefetchRecord(t, dir))
	}
	recs := decodePrefetchRecords(t, dir)
	if n := len(queries(recs)); n != 1 {
		t.Fatalf("the same seal performed %d lookup(s) — the turn boundary fetched for keywords the typing path already had:\n%s",
			n, readPrefetchRecord(t, dir))
	}
	if got := countOutcome(recs, prefetchOutcomeShared); got != 1 {
		t.Errorf("recorded %d outcome=%s line(s), want 1 — the sharing is an event with a name of its own, not a silent absence:\n%s",
			got, prefetchOutcomeShared, readPrefetchRecord(t, dir))
	}
	if got := countOutcome(recs, prefetchOutcomeSuperseded); got != 0 {
		t.Errorf("recorded %d outcome=%s line(s) — nothing should have been fetched and then thrown away:\n%s",
			got, prefetchOutcomeSuperseded, readPrefetchRecord(t, dir))
	}
	if out.Display != sharedSealedLine {
		t.Errorf("Display was modified (it is the operator's own line): %q", out.Display)
	}

	// 3. The half that makes the reuse a REUSE: the turn's own adoption CONSUMES
	//    the typing section, so the REPL's fallback — the branch taken when the
	//    boundary poses no lookup — finds nothing and cannot append the block a
	//    second time. A probe that declined and left the section in place would
	//    pass assertions 1 and 2 and fail here.
	//
	//    THE MEASUREMENT IS `ok` AND A FIELD-BY-FIELD COMPARISON of the turn, and
	//    it is worth saying why the second half is not spelled
	//    `again.Prefetch.Section != ""` (the shape this line had until the fixture
	//    reached the injection floor). Ok=false means chatPrefetchAdopt returned
	//    the caller's own value untouched, so what a reader must be able to see is
	//    that the turn it returns is the turn it was handed — Message, Display and
	//    the seed all unchanged. Comparing the COPY's Prefetch.Section against ""
	//    measured the opposite of its intent: a declined adoption returns `out`
	//    unchanged, so a non-empty seed (the only state a retrieval above the
	//    floor can produce) was reported as "changed the turn", while the empty
	//    seed this fixture used to produce made the clause pass vacuously.
	again, ok := chatPrefetchAdopt(out, p, sharedSealedLine)
	if ok {
		t.Error("the typing section was adopted a SECOND time after the turn consumed it")
	}
	if again.Message != out.Message || again.Display != out.Display || again.Prefetch != out.Prefetch {
		t.Errorf("a second adoption changed the turn:\n%q\n%q", out.describe(), again.describe())
	}
	if n := prefetchLookupLineCount(readPrefetchRecord(t, dir)); n != 1 {
		t.Fatalf("the seal ended with %d lookup line(s), want exactly 1:\n%s",
			n, readPrefetchRecord(t, dir))
	}
}

// TestTheTwoKeysAreOneQuestionHere is the CRITERION, measured rather than
// described: the sharing is decided on the query strings, so this case is only
// the equality case if the pause's key and the turn's key really are equal. The
// relation is asserted between the two strings the run itself reported, not
// through a new exported helper (production surface bought for a test).
//
// It also pins the direction the criterion is CONSERVATIVE in: the comparison is
// the whole string, so the sibling case — a key that merely OVERLAPS — is a
// different question and still pays its own lookup. That refusal lives in
// chat_prefetch_shared_test.go (TestASharedKeyNeverSuppressesADifferentQuestion)
// and is not restated here.
func TestTheTwoKeysAreOneQuestionHere(t *testing.T) {
	p, dir := turnEnabled(t)
	typeAndPause(t, p, sharedSealedLine, time.Now())

	pending := p.session.PendingQuery()
	if pending == "" {
		t.Fatal("no pending query after the pause — the fixture no longer reaches the typing path")
	}
	_, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: sharedSealedLine, Display: sharedSealedLine},
		p, inputsource.Human, chatPrefetchTurnTerms(false, nil, sharedSealedLine))
	if !adopted {
		t.Fatalf("the turn was not served from the pause's section:\n%s", readPrefetchRecord(t, dir))
	}
	got := queries(decodePrefetchRecords(t, dir))
	if len(got) != 1 || got[0] != pending {
		t.Fatalf("the record's one retrieval key %q is not the pause's own %q — the seal asked a second, different question:\n%s",
			got, pending, readPrefetchRecord(t, dir))
	}
}

// TestANotifySealWithNoTypingSectionStillPaysItsOwnLookup is the constraint the
// ruling must not break, and it is the reason the boundary exists at all: a turn
// whose input the typing sampler structurally cannot see — a `notify` delivery,
// a whole-block paste, a `/queue`d prompt, a model-typed line — has no typed line
// and no pending section, so it retrieves for itself and records `shared` never.
func TestANotifySealWithNoTypingSectionStillPaysItsOwnLookup(t *testing.T) {
	p, dir := turnEnabled(t)
	notice := turnNoticeText
	out, adopted := chatPrefetchAdoptTurn(
		chatOutbound{Message: "[system] An external process sent this session a message: " + notice,
			Display: "external message d1"},
		p, inputsource.Notify, []string{notice})
	if !adopted {
		t.Fatalf("a notify turn posed no retrieval and adopted nothing:\n%s", readPrefetchRecordIfAny(dir))
	}
	if !strings.Contains(out.Prefetch.Section, "memory-prefetch") {
		t.Errorf("the notify turn's section did not ride the message:\n%s", out.describe())
	}
	recs := decodePrefetchRecords(t, dir)
	if n := len(queries(recs)); n != 1 {
		t.Fatalf("a notify turn performed %d lookup(s), want exactly 1:\n%s", n, readPrefetchRecord(t, dir))
	}
	if got := countOutcome(recs, prefetchOutcomeShared); got != 0 {
		t.Errorf("a turn with no typed line was served from a typing section it cannot share (%d shared line(s)):\n%s",
			got, readPrefetchRecord(t, dir))
	}
}

// TestTheLookupCountCountsLookupsNotEvents is the counter's own red proof, and it
// is a test rather than a comment because the failure it names was SHIPPED once:
// a counter that matched the `"hits":` key reported two lookups for a record
// carrying one lookup and one adoption, because the adoption line carries no
// lookup of its own. The predicate reads OUTCOMES, so an event ABOUT a retrieval
// contributes nothing — asserted here on a literal record, independent of any
// producer.
func TestTheLookupCountCountsLookupsNotEvents(t *testing.T) {
	// The shape a fixed run writes for one seal: one lookup, one sharing, one
	// adoption. Written as the wire bytes the record actually carries, so the
	// assertion is about the text a reader counts and not about a struct.
	rec := strings.Join([]string{
		`{"query":"notify delivered retrieval key","hits":1,"outcome":"hits","adopted":"no"}`,
		`{"query":"notify delivered retrieval key","outcome":"shared","adopted":"yes","section_bytes":167}`,
		`{"outcome":"superseded","adopted":"no"}`,
	}, "\n")
	if got := prefetchLookupLineCount(rec); got != 1 {
		t.Fatalf("counted %d lookup(s) in a record carrying exactly one, want 1 — an event about a retrieval was counted as a retrieval:\n%s",
			got, rec)
	}
}
