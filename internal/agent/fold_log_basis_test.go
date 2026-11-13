package agent

// fold_log_basis_test.go — the R1 tool-result fold's LOG LINE must print its
// "before → after" pair on ONE basis (the chars/4 heuristic), because the pair
// the row printed was CONSTANT by construction (task 01a0f89d, defect A).
//
// THE DEFECT, as the field read it:
//
//	🧠 context window: folded 12 old tool result(s) into stubs (est 150491 → 150491 tokens, limit 150000)
//
// — a before/after comparison that prints the same number twice, on the one
// path whose entire purpose is to shrink the context. The mechanism is not a
// formatting slip; it is a BASIS error, the same class compact.go's savings
// verdict already fixed (see the comment on its beforeHeuristic): the pair was
// computed with Context.EstimatedTokens(), which uses the API-MEASURED prompt
// count as its base for every message up to PromptTokensIndex and adds the
// chars/4 estimate only for the tail added since. The fold reaches only tool
// results past maxVerbatimToolResults — i.e. OLD ones — and by the time the next
// round folds them they are all INSIDE the measured base (PromptTokens is
// recorded at the end of the previous round, over exactly the messages this
// round starts with). So rewriting their content cannot move the total:
// EstimatedTokens() is invariant across summarizeOldToolResults, and the log
// printed that invariance as if it were a measurement.
//
// THE FIX, and its one boundary: the log's pair is now before/after
// `Context.heuristicTokens()` — the pure chars/4 estimate, the only basis
// available on BOTH sides of the mutation (context.go states the same rule for
// the compaction verdict). The DECISION is deliberately untouched: the trigger
// (`limit`) and the early return stay on EstimatedTokens(), so the trim still
// fires on the measured size and a fold can still correctly decide to hand over
// to the LLM compaction.
//
// WHAT THIS TEST PINS, and why each leg is a claim about the production path
// (Rule 31: the row below is the log trimContext really printed, not a
// re-composition of its parts):
//
//	1. the fold is logged at all (a silent fold is the other failure mode);
//	2. the printed pair is NOT constant, and it SHRINKS — the red leg on the
//	   pre-fix code, where the two numbers are equal by construction;
//	3. the pair is the HEURISTIC pair of this exact fixture, both sides. The
//	   expectation is derived by running the SAME production primitive
//	   (summarizeOldToolResults) over a replica of the messages, so the test
//	   states the basis rather than a second copy of the fold policy;
//	4. the pre-fix pair is shown to be constant ON THIS FIXTURE, by asserting
//	   that EstimatedTokens() before the call is the measured base itself
//	   (every foldable result behind its index) — which is what makes leg 2 a
//	   measurement of the defect rather than of a lucky fixture.

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// foldLogMeasuredPromptTokens is the API-measured prompt count the fixture pins:
// comfortably above the window's trigger (testThresholdWindow ×
// DefaultCompactTriggerRatio), so trimContext takes the over-limit path that
// owns the fold. It stands for the field log's 150491/150000 shape.
const foldLogMeasuredPromptTokens = 5000

// foldLogOldResults / foldLogNewResults / foldLogLongLines describe the fixture's
// foldable zone: more than maxVerbatimToolResults OLD long results (they fold)
// followed by exactly maxVerbatimToolResults short ones (they stay verbatim).
// The counts are written against those production constants rather than typed,
// so a policy change moves the fixture with it (Rule 1/Rule 13).
const (
	foldLogOldResults = 20
	foldLogNewResults = maxVerbatimToolResults
	foldLogLongLines  = maxToolResultLines * 6
)

// foldLineRE matches the fold log's own line and captures the three numbers it
// carries (the fold count, and the pair). Anchored on the message's fixed
// wording so a reworded line fails as "not found" instead of silently matching
// nothing — the same reason the compaction tests match their line by text.
var foldLineRE = regexp.MustCompile(`folded (\d+) old tool result\(s\) into stubs \(est (\d+) → (\d+) tokens, limit (\d+)\)`)

// foldLogFixture builds the FIELD SHAPE and returns the agent plus a replica of
// the messages for the independent derivation: everything the fold can reach sits
// BEHIND the API-measured base's index, exactly as it does in a live session.
func foldLogFixture(t *testing.T) (*Agent, []deepseek.Message) {
	t.Helper()
	ag, _ := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the fold must be the zero-LLM-cost path — no summarizer call expected")
	}, testThresholdWindow)
	// Compaction OFF: this test is about the fold's log line, and a summarizer
	// round-trip would put another (unrelated) line in the buffer. The fold path
	// is entered before the compaction arm either way.
	ag.CompactionEnabled = false

	fillHistory(ag.Ctx, 3, 40) // a small body of conversation
	long := strings.Repeat("tool-long-content-line\n", foldLogLongLines)
	for i := 0; i < foldLogOldResults; i++ {
		ag.Ctx.AddToolResult("old-call", long)
	}
	for i := 0; i < foldLogNewResults; i++ {
		ag.Ctx.AddToolResult("new-call", "short verbatim result")
	}
	// THE MEASUREMENT, taken LAST: it covers every message above, so the foldable
	// results are all inside its range. This is the state a live session is in
	// when trimContext runs — the previous round's response recorded the count
	// over the very messages this round hands to the folder.
	ag.Ctx.SetPromptTokens(foldLogMeasuredPromptTokens)
	return ag, append([]deepseek.Message(nil), ag.Ctx.Messages...)
}

// foldLogHeuristicPair derives the pair the log SHOULD carry, on the replica,
// through the same production primitive — the fold itself is not re-implemented
// here, only re-run over an identical message set.
func foldLogHeuristicPair(t *testing.T, msgs []deepseek.Message) (before, after int) {
	t.Helper()
	replica := &Context{Messages: append([]deepseek.Message(nil), msgs...)}
	before = replica.heuristicTokens()
	if folded := replica.summarizeOldToolResults(); folded == 0 {
		t.Fatal("the fixture does not fold anything — the case is wrong (check maxVerbatimToolResults / maxToolResultLines against the fixture's sizes)")
	}
	after = replica.heuristicTokens()
	return before, after
}

// TestFoldLogPairIsMeasuredOnOneBasis is the regression guard for defect A: the
// fold's log line reports a SHRINK measured on the heuristic basis, on the
// production path, from the fixture that reproduces the field shape.
func TestFoldLogPairIsMeasuredOnOneBasis(t *testing.T) {
	ag, replicaMsgs := foldLogFixture(t)

	// Leg 4's precondition, measured before the call: the estimate IS the
	// measured base (an empty tail), so on the pre-fix code the fold cannot move
	// it and the log's pair is constant. Without this the shrinking pair below
	// could be an accident of the fixture.
	if est := ag.Ctx.EstimatedTokens(); est != foldLogMeasuredPromptTokens {
		t.Fatalf("fixture: EstimatedTokens() = %d, want the pinned measured base %d — every foldable result must sit BEHIND the measurement's index for this case to reproduce the field shape",
			est, foldLogMeasuredPromptTokens)
	}
	limit := expectedLimit(t, ag.ContextWindow)
	if est := ag.Ctx.EstimatedTokens(); est <= limit {
		t.Fatalf("fixture: est %d must exceed the trigger %d so the fold path runs", est, limit)
	}

	// The independently derived expectation, on the replica.
	wantBefore, wantAfter := foldLogHeuristicPair(t, replicaMsgs)
	if wantAfter >= wantBefore {
		t.Fatalf("fixture: the fold does not shrink the heuristic estimate (%d → %d) — the case cannot assert a shrinking pair", wantBefore, wantAfter)
	}

	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.trimContext(context.Background())

	m := foldLineRE.FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("trimContext folded results without logging a fold line — the log is the only record of a zero-LLM-cost mutation\nlog:\n%s", buf.String())
	}
	folded, _ := strconv.Atoi(m[1])        //stylegate:ignore parses the fold report's own printed counts; a parse miss yields 0 and fails the equality below
	printedBefore, _ := strconv.Atoi(m[2]) //stylegate:ignore second printed count of the same fold report line, compared against the measured value
	printedAfter, _ := strconv.Atoi(m[3])  //stylegate:ignore third printed count of the same fold report line, compared against the measured value
	if folded != foldLogOldResults {
		t.Fatalf("the log reports %d folded result(s), want %d (the fixture's foldable zone)", folded, foldLogOldResults)
	}
	// Leg 2 — THE RED LEG: the pair must shrink. On the pre-fix code these two
	// numbers are equal (both sides EstimatedTokens(), which the fold cannot
	// move while the folded results sit behind the measured base).
	if printedAfter >= printedBefore {
		t.Fatalf("the fold's log pair does not shrink: est %d → %d. A constant pair is what the pre-fix code printed — the comparison ran on Code.EstimatedTokens(), whose measured base covers exactly the results the fold rewrites, so the difference is structurally zero and the log says nothing about the mutation it reports",
			printedBefore, printedAfter)
	}
	// Leg 3 — the pair is the heuristic pair of this fixture, both sides.
	if printedBefore != wantBefore || printedAfter != wantAfter {
		t.Fatalf("the fold's log pair is %d → %d, want the heuristic pair %d → %d (the only basis available on BOTH sides of the mutation)",
			printedBefore, printedAfter, wantBefore, wantAfter)
	}
	// The pair the pre-fix code would have printed, for the record: it is the
	// measured base on both sides. Logged (not asserted) so a reader of a green
	// run can see the defect's own numbers without re-deriving them.
	t.Logf("pre-fix pair would have been %d → %d (measured base, invariant); the fix prints %d → %d (heuristic)",
		foldLogMeasuredPromptTokens, foldLogMeasuredPromptTokens, printedBefore, printedAfter)
}

// TestFoldLogMeasuredBaseIsInvariantForTheFoldedRange pins the MECHANISM the fix
// rests on, on the same fixture, without any log: the fold rewrites only messages
// the measurement already covers, so EstimatedTokens() is unchanged while
// heuristicTokens() drops. If a future policy starts folding the RECENT tail
// (whose messages are past PromptTokensIndex) this test is where that changes
// meaning — and the log's basis reasoning would have to be revisited with it,
// rather than kept as a comment nobody can falsify.
func TestFoldLogMeasuredBaseIsInvariantForTheFoldedRange(t *testing.T) {
	ag, _ := foldLogFixture(t)
	estBefore := ag.Ctx.EstimatedTokens()
	heurBefore := ag.Ctx.heuristicTokens()

	if folded := ag.Ctx.summarizeOldToolResults(); folded == 0 {
		t.Fatal("the fixture folds nothing")
	}

	if estAfter := ag.Ctx.EstimatedTokens(); estAfter != estBefore {
		t.Fatalf("EstimatedTokens() moved with the fold (%d → %d): the fixture's foldable results are no longer inside the measured base, so the field shape this guard reproduces has drifted",
			estBefore, estAfter)
	}
	if heurAfter := ag.Ctx.heuristicTokens(); heurAfter >= heurBefore {
		t.Fatalf("heuristicTokens() did not shrink with the fold (%d → %d): on both bases the log's pair would be constant, and the defect would be a measurement failure rather than a basis one",
			heurBefore, heurAfter)
	}
}

// TestFoldLogBasisGuardKeepsTheDecisionOnTheMeasuredSize pins the ONE boundary of
// the fix (the epic's "do not change the criterion" clause): the fold's log pair
// moved to the heuristic, but the TRIGGER did not. A context whose heuristic sits
// under the limit while its API-measured count is over it must still be folded —
// the trim fires on the measured size, which is the calibration context.go's
// EstimatedTokens note exists for.
//
// This is the counterweight to the fix: without it, "use the heuristic for the
// pair" reads as licence to move the trigger too, and that would re-open the hole
// the measured-size calibration closed.
func TestFoldLogBasisGuardKeepsTheDecisionOnTheMeasuredSize(t *testing.T) {
	ag, _ := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("this case asserts the fold path; no summarizer call expected")
	}, testThresholdWindow)
	ag.CompactionEnabled = false
	// A fixture built for THIS clause: the foldable content is long in LINES
	// (the fold's own predicate) but tiny in characters, so the heuristic stays
	// far under the trigger while the pinned measurement puts the context over
	// it — the two bases disagree by construction, which is the state that
	// distinguishes "the decision is on the measurement" from "on the pair".
	fillHistory(ag.Ctx, 2, 20)
	tiny := strings.Repeat("x\n", maxToolResultLines+1) // folds: > maxToolResultLines, tiny
	for i := 0; i < maxVerbatimToolResults+1; i++ {
		ag.Ctx.AddToolResult("old-call", tiny)
	}
	for i := 0; i < maxVerbatimToolResults; i++ {
		ag.Ctx.AddToolResult("new-call", "short verbatim result")
	}
	limit := expectedLimit(t, ag.ContextWindow)
	ag.Ctx.SetPromptTokens(foldLogMeasuredPromptTokens)
	ag.Ctx.PromptTokensIndex = len(ag.Ctx.Messages)
	if h := ag.Ctx.heuristicTokens(); h > limit {
		t.Fatalf("fixture: heuristic %d must sit under the trigger %d for this case to mean anything", h, limit)
	}
	if est := ag.Ctx.EstimatedTokens(); est <= limit {
		t.Fatalf("fixture: measured estimate %d must exceed the trigger %d", est, limit)
	}

	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.trimContext(context.Background())

	if m := foldLineRE.FindStringSubmatch(buf.String()); m == nil {
		t.Fatalf("the fold did not run for a context the MEASUREMENT puts over the trigger — the criterion moved to the heuristic, which the fix must not do\nlog:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "tokens, limit") {
		t.Fatalf("the fold log lost the limit it is measured against\nlog:\n%s", buf.String())
	}
}

// compactionLineRE matches the compaction log's own line and captures the
// measured trigger count, the heuristic pair, and the target ratio. Anchored on
// the wording so a reword fails as "not found" rather than matching nothing.
var compactionLineRE = regexp.MustCompile(`est (\d+) tokens \(measured (\d+)\) > \d+ \([0-9.]+% of \d+\) — compacted: heuristic (\d+) → (\d+) tokens`)

// TestCompactionLogChangePairIsMeasuredOnOneBasis is the SECOND instance of
// defect A's class, found by the same scan and fixed in the same commit: the
// compaction log line printed `before` (the MEASURED estimate that tripped the
// trigger) beside `after` (necessarily heuristic — compactContext resets
// PromptTokens, so EstimatedTokens() falls back to chars/4 on that side). Two
// bases in one "before → after" pair is the phantom-savings shape compact.go's
// own savings verdict was corrected for; here it is corrected in the LOG, while
// the trigger statement keeps the measured count (a real-usage judgement).
//
// The fixture makes the two bases DISAGREE by construction (the pinned
// measurement is a multiple of the heuristic), which is what makes this case red
// on the pre-fix code: there the printed "before" is the measured count.
func TestCompactionLogChangePairIsMeasuredOnOneBasis(t *testing.T) {
	ag, _ := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, compactionLogWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.CompactKeepRecent = 0
	fillHistory(ag.Ctx, 6, 500)
	ag.Ctx.AddUserMessage("final task")
	ag.Ctx.AddAssistantMessage("final answer", nil)
	// The measurement is recorded LAST and inflated (the field log's own shape:
	// a real session's measured prompt count sits well above chars/4 — CJK and
	// JSON are under-estimated by the rule of thumb).
	ag.Ctx.SetPromptTokens(heuristicOf(ag.Ctx) * compactionLogBasisFactor)

	heuristicBefore := heuristicOf(ag.Ctx)
	measuredBefore := ag.Ctx.EstimatedTokens()
	if measuredBefore == heuristicBefore {
		t.Fatal("fixture: the two bases must disagree for this case to distinguish them")
	}

	ag.trimContext(context.Background())

	m := compactionLineRE.FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("no compaction change line in the log\nlog:\n%s", buf.String())
	}
	printedBefore, _ := strconv.Atoi(m[3]) //stylegate:ignore parses the second case's printed count; the measured value is what the assertion pins
	printedAfter, _ := strconv.Atoi(m[4])  //stylegate:ignore parses the third count of that case for the same comparison
	// Both sides are the heuristic: the "before" side is the pre-compaction
	// heuristic (derived independently over the same message set), the "after"
	// side is the context's own post-compaction heuristic — the arm returns
	// immediately after logging, so the live context IS the logged state.
	if printedBefore != heuristicBefore {
		t.Fatalf("the compaction log's change pair starts at %d, want the heuristic %d — the pre-fix line printed the MEASURED count (%d) there, i.e. a pair whose two sides are different bases",
			printedBefore, heuristicBefore, measuredBefore)
	}
	if got := ag.Ctx.heuristicTokens(); printedAfter != got {
		t.Fatalf("the compaction log's change pair ends at %d, want the context's own post-compaction heuristic %d", printedAfter, got)
	}
	if printedAfter >= printedBefore {
		t.Fatalf("the compaction log's change pair does not shrink: heuristic %d → %d", printedBefore, printedAfter)
	}
	// The TRIGGER STATEMENT stays measured — the fix must not move the criterion
	// off the basis a compaction is justified on.
	if got, _ := strconv.Atoi(m[1]); got != measuredBefore { //stylegate:ignore parses the printed count inside the condition; an unparseable report fails the condition, not passes it
		t.Fatalf("the compaction log's trigger statement reads %d, want the measured estimate %d (the trigger fires on the measured size)", got, measuredBefore)
	}
	if got, _ := strconv.Atoi(m[2]); got != measuredBefore { //stylegate:ignore parses the second printed count inside the condition, same shape as the line above
		// The field is the measurement the trigger JUDGED, not the value the
		// field happens to hold after the compaction reset it. Reading it off
		// the live context (m[2] == ag.Ctx.PromptTokens) asserted the post-reset
		// 0 — that is the reported defect, not a contract, and it is why every
		// compacted line in the field reads "(measured 0)" for a judgment made
		// on a real count (task 01a10e36: 95/95 lines, while session 8981132f's
		// raw log holds 144 non-zero prompt-side frames).
		t.Fatalf("the compaction log reports measured %d, want the measurement the trigger judged (%d); the live field is %d because the compaction reset it",
			got, measuredBefore, ag.Ctx.PromptTokens)
	}
	if ag.Ctx.PromptTokens != 0 {
		t.Fatalf("fixture premise: the compaction must have reset the live measurement, got %d — without the reset this case proves nothing", ag.Ctx.PromptTokens)
	}
	t.Logf("compaction log pair: heuristic %d → %d (measured trigger %d)", printedBefore, printedAfter, measuredBefore)
}

// compactionLogWindow / compactionLogBasisFactor describe the compaction-log
// case's fixture: a small window (so a few hundred characters trip the trigger)
// and the factor by which the pinned measurement exceeds the heuristic — the
// field shape, and what makes the mixed basis visible.
const (
	compactionLogWindow      = 2000
	compactionLogBasisFactor = 3
)
