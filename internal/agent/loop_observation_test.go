package agent

// §6.3 of devdoc/cli-as-eyes-and-hands-feasibility.md: the LOOK-WITHOUT-FIXING
// loop — the one loop shape both existing stuck nets are structurally blind
// to, and the bound this file tests.
//
//   - ① stuckState needs a round with NO tool_use. An observing round always
//     carries one → the counter is reset by the very act of looking.
//   - ② toolStreakState needs a byte-identical fingerprint (tool name +
//     normalized param hash + 50-rune text prefix) AND exempts a polling
//     round whose RESULT changed — a live screen/file/pane changes every
//     round, which that exemption reads as "alive" (correct for bug055's job
//     polls, blind here).
//   - MaxIterations is 0 = unlimited in production, so nothing else caps it.
//
// SEMANTICS CHANGE (task 01a0faac, field task 01a0f8c9) — the bound's
// CRITERION was replaced, and this file carries both the old and the new
// meaning of every assertion:
//
//	OLD: a round was BILLED as a "look" whenever every tool_use in it was an
//	     observer (read/bash/code_search/keyword_search/web_*/git_blame/
//	     vision_analyze/screen_capture). Content was never consulted, so
//	     reading 40 DIFFERENT files was 40 billed looks and the run was
//	     TRUNCATED (01a0f8c9 died that way four times, with zero output).
//	NEW: an all-observer round is only a CANDIDATE. It accumulates when the
//	     round REPEATS — the same call with the same arguments
//	     (toolFingerprint, the ONE comparator, Rule 14) AND a byte-identical
//	     executed RESULT. A different argument (another file, another
//	     keyword) or a result that MOVED (a tailed log, a polled endpoint) is
//	     work and is never billed. This is detector 2's bug055
//	     changing-result exemption, INHERITED — the sibling's semantics are
//	     untouched.
//
// What each test here pins AFTER the change:
//
//	TestObservationBound_EnvOverride            unchanged (one reader, "0"
//	                                            disables, invalid = default)
//	TestObservationBound_StateUnit              per-assertion NEW-vs-OLD notes
//	TestToolClassesCoverTheToolSurface          unchanged (the class test is
//	                                            still the entry condition)
//	TestAgent_ObservationLoop_RedProofUnboundedWithoutBound
//	                                            unchanged arm (bound disabled)
//	TestAgent_ObservationLoop_RedProofA_DistinctLooksSurvive
//	                                            NEW — the LIVE failure, now a
//	                                            survival proof
//	TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips
//	                                            SUPERSEDES the old
//	                                            _BoundTripsAtDefaultAndNotifies:
//	                                            same property (trip at the
//	                                            default + notify), but on the
//	                                            REPEAT stream instead of a
//	                                            distinct-look stream, which the
//	                                            old criterion billed and the new
//	                                            one must not
//	TestAgent_ObservationLoop_ThresholdOverrideTrips
//	                                            same property, repeat stream
//	TestAgent_ObservationLoop_GreenProof_ChangingResultSurvives
//	                                            NEW — the bug055 exemption,
//	                                            inherited by detector 3
//	TestAgent_ObservationLoop_LegitimateJobPollSurvives
//	                                            unchanged semantics (polling is
//	                                            an ACT); error-text assertion
//	                                            updated to the new wording
//	TestAgent_ObservationLoop_PostWorkVerificationSurvives
//	                                            unchanged semantics (ACT +
//	                                            2N exemption)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// observeCaptureNotify points the notify pipeline at a capture file and
// subscribes it to the THREE loop alarms (the new one included). The values
// are TEST-scoped, so t.Setenv is the right tool (Rule 17) — the notify
// command runs as a CHILD process and reads this process's environment at
// exec time, and the suite's notify sandbox is restored when the test ends.
func observeCaptureNotify(t *testing.T, captureFile string) {
	t.Helper()
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+captureFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS",
		notifyEventLoopStuck+","+notifyEventLoopStuckToolful+","+notifyEventStuckObservation+","+notifyEventWallclockExceeded)
	t.Setenv("GOPI_TASK_NOTIFY_ASYNC", "0")
}

// setObserveRounds sets the observation threshold for the test's duration
// through the documented env channel (test-scoped → t.Setenv).
func setObserveRounds(t *testing.T, v string) {
	t.Helper()
	t.Setenv(observationRunEnv, v)
}

// obsLook feeds one full OBSERVATION round — the pre-switch call (observe)
// followed by the post-execution call (observeResults), which is the pair the
// loop itself performs. `args` is the raw argument JSON (its normalized form
// IS the fingerprint's parameter half) and `result` is what the tool
// returned; a round repeats only when BOTH match the previous round.
func obsLook(s *observationState, toolName, args, result string) {
	s.observe([]deepseek.ToolCall{obsCall(toolName, args)}, "", true)
	s.observeResults([]string{result})
}

// obsAct feeds one ACT round (mutator / polling tool / unknown name): the
// pre-switch call decides it, and observeResults must then be a no-op for it
// (an ACT round is not a recorded observation).
func obsAct(s *observationState, toolName, args string) {
	s.observe([]deepseek.ToolCall{obsCall(toolName, args)}, "", true)
	s.observeResults([]string{"act result"})
}

// obsNoTool feeds one no-tool round (detector ①'s signal).
func obsNoTool(s *observationState) {
	s.observe(nil, "", false)
}

// obsCall decodes an args JSON string into a ToolCall the way the streaming
// layer does (Arguments is the decoded map whose json.Marshal is sorted — the
// normalization toolFingerprint relies on). The arguments are test-authored
// literals, so a decode failure means a malformed test, not a product path.
func obsCall(toolName, args string) deepseek.ToolCall {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(args), &m); err != nil { //stylegate:ignore test-authored JSON literal; testify-free t.Fatalf is unavailable in this helper by design (no *testing.T parameter)
		m = map[string]interface{}{"raw": args}
	}
	return deepseek.ToolCall{Name: toolName, Arguments: m}
}

// readNotifyCapture returns the notify capture's content, treating a MISSING
// file as "no alarm was ever delivered" — which is the correct reading for the
// negative tests (the notify command only runs when an alarm fires, so a
// silent run leaves no file at all).
func readNotifyCapture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read the notify capture: %v", err)
	}
	return string(b)
}

// observationLoopServer is the LOOK-WITHOUT-FIXING shape: every round carries
// a tool_use that only LOOKS (read / code_search), and its arguments CHANGE
// every round (a live pane, a moving file, a new search) — so detector ②'s
// fingerprint never repeats AND text differs, while detector ① sees a
// tool_use and resets. Nothing in the tree bounds it.
func observationLoopServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		switch *calls % 3 {
		case 0:
			stuckToolUseStreamArgs(t, w, toolnames.ToolRead, fmt.Sprintf(`{"path":"/var/log/step-%d.log"}`, *calls))
		case 1:
			stuckToolUseStreamArgs(t, w, toolnames.ToolCodeSearch, fmt.Sprintf(`{"symbol":"Step%d"}`, *calls))
		default:
			stuckToolUseStreamArgs(t, w, toolnames.ToolKeywordSearch, fmt.Sprintf(`{"query":"state changed at tick %d"}`, *calls))
		}
	}))
	return server, calls
}

// TestObservationBound_EnvOverride: the documented convention — unset →
// default (maxObservationRounds), "0" → DISABLED, positive → that many
// rounds, invalid → default. observationRounds() is the ONLY reader of the
// env var (Rule 2); this test is the single-reader guard.
func TestObservationBound_EnvOverride(t *testing.T) {
	// The reader treats an EMPTY value exactly as an absent one (both are the
	// documented default arm — see observationRounds), so the "unset" arm is
	// exercised by setting it empty: that is also the only spelling a test may
	// use without leaking process state (Rule 17).
	setObserveRounds(t, "")
	if got := observationRounds(); got != maxObservationRounds {
		t.Fatalf("unset: want default %d, got %d", maxObservationRounds, got)
	}
	if st := newObservationState(); !st.enabled || st.threshold != maxObservationRounds {
		t.Fatalf("unset: detector must be enabled at %d, got enabled=%v threshold=%d",
			maxObservationRounds, st.enabled, st.threshold)
	}
	t.Setenv(observationRunEnv, "0")
	if got := observationRounds(); got != 0 {
		t.Fatalf(`"0": want disabled (0), got %d`, got)
	}
	if st := newObservationState(); st.enabled {
		t.Fatalf(`"0" must DISABLE the detector`)
	}
	t.Setenv(observationRunEnv, "3")
	if got := observationRounds(); got != 3 {
		t.Fatalf(`"3": want 3, got %d`, got)
	}
	t.Setenv(observationRunEnv, "bogus")
	if got := observationRounds(); got != maxObservationRounds {
		t.Fatalf(`"bogus": want default %d (a typo'd net must not disarm itself), got %d`,
			maxObservationRounds, got)
	}
}

// TestObservationBound_StateUnit: the repeat/reset semantics in one unit test.
//
// SEMANTICS CHANGE (task 01a0faac) — NEW vs OLD, per assertion, so a later
// reader knows which rule each one pins:
//
//	OLD (defective, field task 01a0f8c9): a round was BILLED as a look when
//	  every tool in it was an observer. Four reads of four DIFFERENT files
//	  were one "repeat" — a legitimate evidence-gathering run was truncated
//	  at 40 rounds with zero output, four times.
//	NEW: an all-observer round is only a CANDIDATE. It accumulates only when
//	  its fingerprint (tool name + normalized arguments — toolFingerprint,
//	  the ONE comparator, Rule 14) AND its executed result both repeat. Any
//	  differing argument (another file, another keyword) or a result that
//	  MOVED (a tailed log, live state) is work: streak 0, nothing billed.
//
// Arms NOT changed (each asserted below): ACT resets and re-arms the
// post-work exemption, polling (`job`) is an ACT (detector ② owns it —
// bug055 is load-bearing), a no-tool round ends the streak, an unknown tool
// name is never billed as a look, and the exemption is spent in LOOK rounds
// (so the total a run survives after a mutation is 3N).
func TestObservationBound_StateUnit(t *testing.T) {
	setObserveRounds(t, "4")

	// OLD semantics tripped here at the 4th round. NEW: eight DIFFERENT reads
	// (different path, therefore different result) must never trip.
	s0 := newObservationState()
	for i := 0; i < 8; i++ {
		obsLook(s0, toolnames.ToolRead, fmt.Sprintf("/pkg/f%d.go", i), fmt.Sprintf("content of f%d", i))
		if s0.tripped() {
			t.Fatalf("eight DIFFERENT reads must never trip (old criterion billed the class, new bills the repeat) — look %d", i+1)
		}
	}

	s := newObservationState()
	// Four REPEATED looks — same call, same result — are below a threshold of
	// 4 → not tripped. (The count is a count of REPEATS: the first observation
	// has nothing to compare against, so the 4th repeat lands on the 5th
	// identical round — the bound fires one round later than the class
	// criterion, in the safe direction, documented on observationState.)
	for i := 0; i < 4; i++ {
		obsLook(s, toolnames.ToolRead, "/pkg/same.go", "unchanged content")
		if s.tripped() {
			t.Fatalf("tripped early at repeated look %d", i+1)
		}
	}
	// The 4th repetition (5th identical round) trips.
	obsLook(s, toolnames.ToolRead, "/pkg/same.go", "unchanged content")
	if !s.tripped() {
		t.Fatalf("4 consecutive REPEATED looks (same call AND same result) must trip a threshold of 4 (streak=%d)", s.streak)
	}

	// An ACT (mutator) resets AND arms 2N exempt look rounds: after the
	// edit, 8 looks are free, the 9th starts the streak, and the 12th
	// (8 exempt + 4) trips — 3N looks from the mutation to the termination.
	// (Semantics UNCHANGED by the repeat fix: the exemption is still spent in
	// look rounds; the looks here are identical so they are also repeats,
	// which the exemption must swallow regardless.)
	s2 := newObservationState()
	obsAct(s2, toolnames.ToolEdit, `{"path":"pkg/f.go","oldStr":"a","newStr":"b"}`)
	for i := 0; i < 8; i++ {
		obsLook(s2, toolnames.ToolRead, "/pkg/f.go", "content")
	}
	if s2.tripped() {
		t.Fatalf("the post-work verification burst (2N looks) must never trip (streak=%d)", s2.streak)
	}
	for i := 0; i < 3; i++ {
		obsLook(s2, toolnames.ToolRead, "/pkg/f.go", "content")
		if s2.tripped() {
			t.Fatalf("tripped inside the bounded tail: look %d", i+1)
		}
	}
	obsLook(s2, toolnames.ToolRead, "/pkg/f.go", "content")
	if !s2.tripped() {
		t.Fatalf("a run that stopped acting must still trip (streak=%d)", s2.streak)
	}

	// Polling is an ACT: identical job polls never accumulate here (that
	// signal belongs to detector ②, bug055 — the sibling's semantics are
	// untouched by this fix).
	s3 := newObservationState()
	for i := 0; i < 12; i++ {
		obsAct(s3, toolnames.ToolJob, `{"action":"output","job_id":"J1"}`)
	}
	if s3.tripped() {
		t.Fatalf("job polling must be an ACT (never billed as observation), streak=%d", s3.streak)
	}

	// A no-tool round ENDS the streak (measured: a round spent talking is not
	// a round spent looking).
	s4 := newObservationState()
	obsLook(s4, toolnames.ToolBash, `{"command":"tail -1 log"}`, "line 7")
	obsLook(s4, toolnames.ToolBash, `{"command":"tail -1 log"}`, "line 7")
	if s4.streak != 1 {
		t.Fatalf("a repeated look must accumulate exactly one repeat, got %d", s4.streak)
	}
	obsNoTool(s4)
	if s4.hasLast {
		t.Fatalf("a no-tool round must drop the repeat reference as well as the streak")
	}
	obsLook(s4, toolnames.ToolBash, `{"command":"tail -1 log"}`, "line 7")
	if s4.streak != 0 {
		t.Fatalf("the look after a no-tool round is not a repeat of a pre-narration one, got %d", s4.streak)
	}

	// Unknown tool name = ACT (never billed as a look).
	s5 := newObservationState()
	for i := 0; i < 12; i++ {
		obsLook(s5, "some_future_tool", `{"x":1}`, "result")
	}
	if s5.tripped() {
		t.Fatalf("an unknown tool name must never be billed as observation")
	}

	// A MIXED round (one mutator among observers) is an ACT.
	s6 := newObservationState()
	s6.observe([]deepseek.ToolCall{
		{Name: toolnames.ToolRead, Arguments: map[string]interface{}{"path": "a.go"}},
		{Name: toolnames.ToolRead, Arguments: map[string]interface{}{"path": "b.go"}},
		{Name: toolnames.ToolWrite, Arguments: map[string]interface{}{"path": "c.go"}},
	}, "", true)
	s6.observeResults([]string{"a", "b", "c"})
	if s6.streak != 0 {
		t.Fatalf("a round carrying a mutator is an ACT, got streak=%d", s6.streak)
	}
}

// TestToolClassesCoverTheToolSurface: every tool the model can call is in
// exactly one of this bound's classes (observer / mutator / the two names it
// deliberately defers: `job` to detector ②, `context_trim` which is a
// context operation, not a look at live state). A NEW tool added to
// claudeToolDefs therefore fails this test until its class is decided —
// rather than silently landing in the "unknown = act" bucket where the bound
// would never see it.
func TestToolClassesCoverTheToolSurface(t *testing.T) {
	// context_trim is deliberately absent from both class tables: it does not
	// look at live state and does not change state, so a round carrying only
	// context_trim is neither — it is an ACT by the unknown-name rule, which
	// is the safe direction (documented in toolclass.go).
	deferred := map[string]bool{toolnames.ToolContextTrim: true}

	seen := map[string]bool{}
	for _, def := range claudeToolDefs {
		if seen[def.Name] {
			t.Errorf("claudeToolDefs lists %q twice", def.Name)
		}
		seen[def.Name] = true
		if observerTools[def.Name] && mutatorTools[def.Name] {
			t.Errorf("%q is in BOTH classes", def.Name)
			continue
		}
		if !observerTools[def.Name] && !mutatorTools[def.Name] && !deferred[def.Name] && def.Name != toolnames.ToolJob {
			t.Errorf("tool %q has no class in toolclass.go — decide whether it LOOKS or ACTS "+
				"(Rule 13: the class tables are the single definition of both)", def.Name)
		}
	}
	if len(seen) != len(claudeToolDefs) {
		t.Fatalf("tool surface changed: %d definitions, %d unique names", len(claudeToolDefs), len(seen))
	}
	// Both class tables must only name tools the model can actually call —
	// a stale entry means the bound reasons about a tool that no longer
	// exists (and would silently stop bounding its replacement).
	for name := range observerTools {
		if !seen[name] {
			t.Errorf("observerTools carries %q, which is not in claudeToolDefs", name)
		}
	}
	for name := range mutatorTools {
		if !seen[name] {
			t.Errorf("mutatorTools carries %q, which is not in claudeToolDefs", name)
		}
	}
	// The loudest coverage assertion: at least one tool ACTS, or the bound
	// could never be reset and every run would die at the threshold.
	if len(mutatorTools) == 0 {
		t.Fatalf("no mutator tools declared — the observation bound would terminate every run")
	}
	if len(observerTools) == 0 {
		t.Fatalf("no observer tools declared — the bound could never trip")
	}
}

// ---------------------------------------------------------------------------
// RED PROOF: with the bound disabled, the look-without-fixing loop runs
// unbounded — neither existing detector stops it, and the only ceiling left
// is the iteration budget (0 = unlimited in production). Its OWN run
// MaxIterations is the test's stand-in for "the run never ends".
// ---------------------------------------------------------------------------

func TestAgent_ObservationLoop_RedProofUnboundedWithoutBound(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	observeCaptureNotify(t, captureFile)
	setObserveRounds(t, "0") // the bound is DISABLED — this is the pre-change tree's behaviour

	server, calls := observationLoopServer(t)
	defer server.Close()

	const rounds = 40
	ag := newStuckTestAgent(server.URL, rounds)
	ag.executeToolFn = func(tc deepseek.ToolCall) string {
		// Every look returns something NEW — the model learns a little each
		// round, which is exactly why the loop is plausible and why the
		// planted detectors miss it.
		return fmt.Sprintf("live state at tick %d: everything still fine", *calls)
	}
	_, err := ag.Run("keep watching the deployment until it settles")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("without the bound the observation loop must run to the iteration budget, got: %v", err)
	}
	if *calls != rounds {
		t.Fatalf("expected all %d look rounds to run unbounded, got %d", rounds, *calls)
	}
	b := readNotifyCapture(t, captureFile)
	// No loop-stuck-family alarm at all: ①① (no tool_use) and ②② (identical
	// fingerprint / frozen poll result) are structurally blind here — every
	// round has a tool_use, every round's tool+args+text and result differ.
	for _, ev := range []string{notifyEventLoopStuck, notifyEventLoopStuckToolful, notifyEventStuckObservation} {
		if strings.Contains(b, `"event":"`+ev+`"`) {
			t.Fatalf("detector %q fired for the observation loop — the red proof needs it UNBOUNDED: %s", ev, b)
		}
	}
}

// The bound at a SMALL threshold, on the REPEAT stream: proves the threshold
// is honoured (not hardcoded at 24 or 40) and that firing is threshold-driven.
// Semantics OLD vs NEW: the old test ran this at threshold 3 on a stream of
// DISTINCT looks with a CHANGING result — under the fix that stream must
// survive, so the stream itself is the repeat shape (same call, same bytes)
// and only the threshold moves. (The default-threshold trip + notify proof is
// TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips below.)
func TestAgent_ObservationLoop_ThresholdOverrideTrips(t *testing.T) {
	observeCaptureNotify(t, t.TempDir()+"/notify.jsonl")
	setObserveRounds(t, "3")

	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		proveTextToolStream(w, fmt.Sprintf("round %d: still the same", round),
			toolnames.ToolBash, `{"command":"stat -c %Y /var/tmp/state"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 30)
	ag.executeToolFn = func(tc deepseek.ToolCall) string { return "1700000000" } // frozen
	_, err := ag.Run("watch")
	if err == nil || !strings.Contains(err.Error(), notifyEventStuckObservation) {
		t.Fatalf("threshold 3 must terminate the repeated-look loop, got: %v", err)
	}
	if round != 3+1 { // 3 REPEATS = the 4th identical round (see observationState's OFFSET note)
		t.Fatalf("threshold 3 fired at %d rounds", round)
	}
}

// ---------------------------------------------------------------------------
// REGRESSION: the two loops that must SURVIVE the new bound.
// ---------------------------------------------------------------------------

// A legitimate wait on a RUNNING job — job(action=output) with identical
// args, result changing every round — is NOT killed, even at a threshold of
// 1: polling is detector ②'s class (bug055's exemption is load-bearing), and
// this bound must never bill it as observation. This is the "do not narrow
// toolStreakState's exemption" guard at the third detector.
func TestAgent_ObservationLoop_LegitimateJobPollSurvives(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	observeCaptureNotify(t, captureFile)
	setObserveRounds(t, "1") // the most aggressive possible bound

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stuckToolUseStreamArgs(t, w, toolnames.ToolJob, `{"action":"output","job_id":"J1"}`)
	}))
	defer server.Close()

	const rounds = 20
	ag := newStuckTestAgent(server.URL, rounds)
	ag.executeToolFn = func(tc deepseek.ToolCall) string {
		return fmt.Sprintf("job J1 status=running elapsed=%.1fs\n(no output yet)", float64(calls)*0.7)
	}
	_, err := ag.Run("wait for the build job to finish")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("a waiting Bond must run to its budget, got: %v", err)
	}
	if strings.Contains(err.Error(), "repeating the same observation") || strings.Contains(err.Error(), notifyEventStuckObservation) {
		t.Fatalf("the observation bound killed a legitimate job poll: %v", err)
	}
	b := readNotifyCapture(t, captureFile)
	if strings.Contains(b, `"event":"`+notifyEventStuckObservation+`"`) {
		t.Fatalf("observation alarm fired for a legitimate job poll: %s", string(b))
	}
}

// The productive cadence: an ACT, then the verification burst that follows it
// (build → test → read → read → …) must not be billed as a look-without-
// fixing loop. Each act funds 2N more looks, so a long alternating
// edit/verify run survives indefinitely while a run that STOPS acting trips.
//
// Semantics OLD vs NEW: UNCHANGED arm (mutator → streak 0 + 2N exemption).
// Each look here carries a different argument, so under the repeat criterion it
// is additionally a non-repeat; the exemption is asserted by the count of looks
// the run survives, which is longer than either rule alone would give.
func TestAgent_ObservationLoop_PostWorkVerificationSurvives(t *testing.T) {
	observeCaptureNotify(t, t.TempDir()+"/notify.jsonl")
	setObserveRounds(t, "3") // 3 acts × (1 act + 6 looks) — well past 3 looks each

	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		// act, then 6 looks (twice the 2N exemption), then act again…
		if round%7 == 1 {
			stuckToolUseStreamArgs(t, w, toolnames.ToolEdit, fmt.Sprintf(`{"path":"pkg/f%d.go","oldStr":"a%d","newStr":"b%d"}`, round, round, round))
			return
		}
		stuckToolUseStreamArgs(t, w, toolnames.ToolRead, fmt.Sprintf(`{"path":"pkg/f%d.go","offset":%d}`, round, round))
	}))
	defer server.Close()

	const rounds = 28
	ag := newStuckTestAgent(server.URL, rounds)
	ag.executeToolFn = func(tc deepseek.ToolCall) string { return "ok" }
	_, err := ag.Run("edit, verify, edit, verify")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("edit/verify cadence must not trip the observation bound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The three detectors must stay distinguishable: the error text and the wire
// name of each are asserted separately, and the new one's name is a
// superstring-free sibling (a bare Contains on "loop-stuck" would be fooled —
// the bug055 lesson, restated here for the new event).
// ---------------------------------------------------------------------------

func TestObservationEventNameIsDistinct(t *testing.T) {
	names := map[string]string{
		"no-tool":     notifyEventLoopStuck,
		"toolful":     notifyEventLoopStuckToolful,
		"observation": notifyEventStuckObservation,
	}
	seen := map[string]string{}
	for kind, n := range names {
		if n == "" {
			t.Fatalf("%s has an empty wire name", kind)
		}
		if prev, dup := seen[n]; dup {
			t.Fatalf("%s and %s share the wire name %q", prev, kind, n)
		}
		seen[n] = kind
	}
	if !strings.HasPrefix(notifyEventStuckObservation, notifyEventLoopStuck) {
		t.Fatalf("expected the new event to be a member of the loop-stuck family, got %q", notifyEventStuckObservation)
	}
	// The "all" subscription must see it: the registry is the enumeration
	// (the tools-side gate TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth
	// proves the registry half; this asserts the agent-side alias resolves to
	// the SAME constant the tools package declares and registers).
	if notifyEventStuckObservation != tools.NotifyEventStuckObservation {
		t.Fatalf("the agent alias %q and the tools declaration %q disagree",
			notifyEventStuckObservation, tools.NotifyEventStuckObservation)
	}
}

// ---------------------------------------------------------------------------
// TASK 01a0faac — THE REPEAT CRITERION: the three proofs the fix must carry.
//
// Every round in all three streams carries prose that DIFFERS from the
// previous round's (so detector ② 's fingerprint — which includes the 50-rune
// text prefix — never repeats) and a tool_use (so detector ① 's no-tool
// counter stays reset). That isolation is deliberate: whatever these tests
// observe is detector ③ 's judgment alone.
// ---------------------------------------------------------------------------

// proveTextToolStream writes one assistant message carrying prose AND a
// tool_use — the shape that separates the repeat criterion from its two
// siblings: detector ① needs a tool-free round, detector ② needs an identical
// text prefix, and neither is true here.
func proveTextToolStream(w http.ResponseWriter, text, name, argsJSON string) {
	nameJSON, _ := json.Marshal(name)      //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONB, _ := json.Marshal(argsJSON) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	textJSON, _ := json.Marshal(text)      //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	w.Header().Set("Content-Type", "text/event-stream")
	writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
	writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, textJSON))
	writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_b","name":%s,"input":{}}}`, nameJSON))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":%s}}`, argsJSONB))
	writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
	writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// RED PROOF A — the LIVE failure (field task 01a0f8c9): a run that gathers
// evidence by reading DIFFERENT files / searching DIFFERENT keywords for far
// more rounds than the threshold is doing investigation, not looping.
//
// Under the OLD criterion (bill every all-observer round) this stream trips at
// maxObservationRounds and truncates the run; under the repeat criterion every
// round is a NEW observation, so it must survive to the iteration budget.
func TestAgent_ObservationLoop_RedProofA_DistinctLooksSurvive(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	observeCaptureNotify(t, captureFile)
	t.Setenv(observationRunEnv, "") // the documented default (maxObservationRounds)

	const rounds = 60 // > maxObservationRounds: the old criterion would cut it at 40
	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		text := fmt.Sprintf("round %d: reading the next file to gather evidence", round)
		switch round % 3 {
		case 0:
			proveTextToolStream(w, text, toolnames.ToolRead, fmt.Sprintf(`{"path":"/pkg/step-%d.go"}`, round))
		case 1:
			proveTextToolStream(w, text, toolnames.ToolCodeSearch, fmt.Sprintf(`{"symbol":"Handler%d"}`, round))
		default:
			proveTextToolStream(w, text, toolnames.ToolKeywordSearch, fmt.Sprintf(`{"query":"distinct keyword %d"}`, round))
		}
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, rounds)
	ag.executeToolFn = func(tc deepseek.ToolCall) string {
		// A different file/keyword returns a different answer — real evidence.
		return fmt.Sprintf("evidence %d: %v", round, tc.Arguments)
	}
	_, err := ag.Run("read every file you need to understand the defect, then report")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("RED PROOF A: a run reading DIFFERENT files every round must survive the observation bound "+
			"(it is investigation, not repetition); got: %v", err)
	}
	if round != rounds {
		t.Fatalf("RED PROOF A: expected all %d distinct looks to run, got %d", rounds, round)
	}
	if b := readNotifyCapture(t, captureFile); strings.Contains(b, `"event":"`+notifyEventStuckObservation+`"`) {
		t.Fatalf("RED PROOF A: the observation alarm fired on distinct looks: %s", b)
	}
}

// RED PROOF B — the shape the bound EXISTS for: the same command with the same
// arguments returning the same bytes, round after round, while the model's
// prose moves (so detector ② — text prefix in the fingerprint — stays blind
// and detector ① sees a tool_use). This must trip at the documented default
// threshold; the criterion is the repeat, and this round IS the repeat.
func TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	observeCaptureNotify(t, captureFile)
	t.Setenv(observationRunEnv, "") // default threshold: 40

	frozen := "drwxr-xr-x 2 nick nick 4096 .\n-rw-r--r-- 1 nick nick 12 stale.tmp"
	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		proveTextToolStream(w, fmt.Sprintf("round %d: checking whether the state has moved yet", round),
			toolnames.ToolBash, `{"command":"ls -la /var/tmp/state"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 4*maxObservationRounds)
	ag.executeToolFn = func(tc deepseek.ToolCall) string { return frozen } // byte-identical every round
	_, err := ag.Run("watch /var/tmp/state until it settles")
	if err == nil {
		t.Fatalf("RED PROOF B: the same call with the same unchanged result must trip the bound")
	}
	if !strings.Contains(err.Error(), notifyEventStuckObservation) {
		t.Fatalf("RED PROOF B: expected the error to cite %q, got: %v", notifyEventStuckObservation, err)
	}
	// The OFFSET (observationState's doc): streak counts REPEATS, so the 40th
	// repeat arrives on the 41st identical round. Asserting the exact round
	// pins that the bound fires on repetition, not on the category.
	if round != maxObservationRounds+1 {
		t.Fatalf("RED PROOF B: expected the trip on the round that produces the %dth repeat (%d), got %d rounds",
			maxObservationRounds, maxObservationRounds+1, round)
	}
	b := readNotifyCapture(t, captureFile)
	if !strings.Contains(b, `"event":"`+notifyEventStuckObservation+`"`) {
		t.Fatalf("RED PROOF B: alarm not delivered: %q", b)
	}
	if !strings.Contains(b, "byte-identical result") {
		t.Fatalf("RED PROOF B: the payload must name the criterion (same call + byte-identical result): %s", b)
	}
}

// GREEN PROOF — the same command AND the same arguments, but a result that
// MOVES: this is a run watching live state (a tailed log, a polled endpoint),
// and it must survive the bound at the documented default threshold. This is
// detector ② 's bug055 exemption — the semantics the epic requires detector ③
// to INHERIT rather than re-derive.
func TestAgent_ObservationLoop_GreenProof_ChangingResultSurvives(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	observeCaptureNotify(t, captureFile)
	t.Setenv(observationRunEnv, "") // default threshold

	const rounds = 60 // > maxObservationRounds
	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		proveTextToolStream(w, fmt.Sprintf("round %d: the log is still growing, keep watching", round),
			toolnames.ToolBash, `{"command":"tail -1 /var/log/build.log"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, rounds)
	ag.executeToolFn = func(tc deepseek.ToolCall) string {
		return fmt.Sprintf("build step %d: compiling...", round) // the output MOVES every round
	}
	_, err := ag.Run("tail the build log until the build finishes")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("GREEN PROOF: the same command with a CHANGING result is live state and must survive; got: %v", err)
	}
	if round != rounds {
		t.Fatalf("GREEN PROOF: expected all %d rounds, got %d", rounds, round)
	}
	if b := readNotifyCapture(t, captureFile); strings.Contains(b, `"event":"`+notifyEventStuckObservation+`"`) {
		t.Fatalf("GREEN PROOF: the bound killed a moving-result watch: %s", b)
	}
}
