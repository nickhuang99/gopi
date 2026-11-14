package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// ---------------------------------------------------------------------------
// Host tests (epic: hippocampus child 3/4). One test per delivery level, plus
// the hard-stop/quota/default-off contracts. The organ is a stub throughout —
// these tests own the SCHEDULER (the merge-time binding to the real
// internal/hippocampus engine is documented in the devdoc).
// ---------------------------------------------------------------------------

// testHippoCfg builds an enabled config from the shipped constants, with the
// level ceiling overridden — the canonical "defaults + one knob" shape.
func testHippoCfg(maxLv hippocampusLevel) hippocampusConfig {
	cfg := loadHippocampusConfig()
	cfg.Enabled = true
	cfg.MaxLv = maxLv
	return cfg
}

// stubRecall returns a recall func that reports every call and answers with
// the given brief (nil brief = a clean empty answer).
func stubRecall(t *testing.T, calls *int32, brief *hippocampusBrief, err error) hippoRecallFn {
	t.Helper()
	return func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		atomic.AddInt32(calls, 1)
		return brief, err
	}
}

// stubRecallVar answers with a brief derived from the call ordinal — for
// tests that need each cycle to report a DIFFERENT memory state (the
// convergence gate would otherwise suppress the repeat, which is the point of
// TestHippocampus_Convergence*).
func stubRecallVar(t *testing.T, calls *int32, brief func(n int) *hippocampusBrief) hippoRecallFn {
	t.Helper()
	return func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		n := atomic.AddInt32(calls, 1)
		return brief(int(n)), nil
	}
}

// newHippoTestAgent builds the smallest Agent the host touches, with a real
// session log so the Lv-0 archive is readable from disk.
func newHippoTestAgent(t *testing.T) *Agent {
	t.Helper()
	newTestAgentEnv(t)
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	return &Agent{
		Ctx:        &Context{},
		TaskState:  newTodoState(),
		SessionLog: sl,
		Model:      "test-model",
	}
}

// readSessionJSONL parses every event of a session log.
func readSessionJSONL(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	var out []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		rec := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad JSONL line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// findSessionEvent returns the first event of the given type.
func findSessionEvent(events []map[string]interface{}, typ string) map[string]interface{} {
	for _, ev := range events {
		if ev["type"] == typ {
			return ev
		}
	}
	return nil
}

// lastUserMessage returns the content of the newest user message in the ctx.
func lastUserMessage(a *Agent) string {
	if a.Ctx == nil || len(a.Ctx.Messages) == 0 {
		return ""
	}
	return a.Ctx.Messages[len(a.Ctx.Messages)-1].Content
}

// ---------------------------------------------------------------------------
// Lv-0: archive only — the brief reaches the JSONL and NEVER the context.
// ---------------------------------------------------------------------------

func TestHippocampus_Lv0_ArchiveOnly(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	var calls int32
	h := newHippocampusHost(testHippoCfg(hippocampusLv0Archive),
		stubRecall(t, &calls, &hippocampusBrief{Confidence: 0.95, Summary: "found a matching recipe", Keys: []string{"recipe"}}, nil))
	a.Hippocampus = h

	ctx := context.Background()
	h.StartOfRun(ctx, a)
	before := len(a.Ctx.Messages)
	injections := a.roundBoundaryInjections(ctx)

	if len(injections) != 0 {
		t.Fatalf("Lv-0 must not inject; got %d injection(s): %+v", len(injections), injections)
	}
	if got := len(a.Ctx.Messages); got != before {
		t.Fatalf("Lv-0 mutated the context: %d messages, want %d", got, before)
	}
	// atomic.LoadInt32, not a bare read: the counter is written by the recall
	// goroutine and a plain read here is a race in the TEST (the -race run
	// caught exactly that before the host fix was complete).
	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("the organ was never asked to recall at the run start")
	}

	a.SessionLog.Sync()
	ev := findSessionEvent(readSessionJSONL(t, a.SessionLog.path), hippocampusBriefEvent)
	if ev == nil {
		t.Fatal("Lv-0 did not write an archive event to the session log")
	}
	if got := ev[hippocampusLogKeyLevel]; got != hippocampusLvTextArchive {
		t.Fatalf("archive level = %v, want %s", got, hippocampusLvTextArchive)
	}
	if got := ev[hippocampusLogKeySummary]; got != "found a matching recipe" {
		t.Fatalf("archive summary = %v", got)
	}
	if got := ev[hippocampusLogKeyConfidence]; got != 0.95 {
		t.Fatalf("archive confidence = %v, want the organ's raw signal 0.95", got)
	}
	// The two existing consumers must ignore the new event type.
	if _, err := ParseTrajectoryStats(a.SessionLog.path); err != nil {
		t.Fatalf("trajectory parser rejected the new event: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Lv-1: a post-round memo, delivered as a user MESSAGE.
// ---------------------------------------------------------------------------

func TestHippocampus_Lv1_AppendMemoAsMessage(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	var calls int32
	h := newHippocampusHost(testHippoCfg(hippocampusLv1Append),
		stubRecall(t, &calls, &hippocampusBrief{Confidence: 0.9, Summary: "past run hit a deadlock here", Keys: []string{"deadlock"}}, nil))
	a.Hippocampus = h

	ctx := context.Background()
	h.StartOfRun(ctx, a)
	injections := a.roundBoundaryInjections(ctx)

	if len(injections) != 1 {
		t.Fatalf("Lv-1 must deliver exactly one message; got %d", len(injections))
	}
	if injections[0].Level != hippocampusLv1Append {
		t.Fatalf("level = %v, want Lv-1", injections[0].Level)
	}
	if !strings.HasPrefix(injections[0].Text, hippocampusAppendFrame) {
		t.Fatalf("Lv-1 text is not framed as a memo: %q", injections[0].Text)
	}
	if !strings.Contains(injections[0].Text, "past run hit a deadlock here") {
		t.Fatalf("Lv-1 lost the briefing body: %q", injections[0].Text)
	}
	// The delivery is a MESSAGE on the wire (and in context), not a prompt
	// section: it lands in Ctx as a user message and in the JSONL as a user
	// event with the same text.
	if got := lastUserMessage(a); got != injections[0].Text {
		t.Fatalf("injection did not land in the context as the newest user message: %q", got)
	}
	a.SessionLog.Sync()
	if ev := findSessionEvent(readSessionJSONL(t, a.SessionLog.path), "user"); ev == nil {
		t.Fatal("the injected message was not logged (线上有、日志无 must stay distinguishable)")
	} else if msg := ev["message"].(map[string]interface{}); msg["content"] != injections[0].Text {
		t.Fatalf("logged user message = %v, want the injected text", msg["content"])
	}
	// Lv-1 is the memo channel: it must not carry the meta-event frame.
	if strings.Contains(injections[0].Text, hippocampusMetaEventFrame) {
		t.Fatal("Lv-1 leaked the Lv-2 frame")
	}
}

// ---------------------------------------------------------------------------
// Lv-2: the in-context meta event — same physical channel, different framing,
// and provably NOT a prompt section.
// ---------------------------------------------------------------------------

func TestHippocampus_Lv2_MetaEventIsAMessageNotPromptSection(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	var calls int32
	brief := &hippocampusBrief{Confidence: 0.5, Summary: "a prior task solved this with a worktree", Keys: []string{"worktree"}}
	h := newHippocampusHost(testHippoCfg(hippocampusLv2MetaEvent), stubRecall(t, &calls, brief, nil))
	a.Hippocampus = h

	ctx := context.Background()
	h.StartOfRun(ctx, a)
	injections := a.roundBoundaryInjections(ctx)

	if len(injections) != 1 || injections[0].Level != hippocampusLv2MetaEvent {
		t.Fatalf("Lv-2 not delivered: %+v", injections)
	}
	if !strings.HasPrefix(injections[0].Text, hippocampusMetaEventFrame) {
		t.Fatalf("Lv-2 text is not framed as a meta event: %q", injections[0].Text)
	}
	if a.Ctx.Messages[len(a.Ctx.Messages)-1].Role != "user" {
		t.Fatal("Lv-2 did not arrive as a user message")
	}
	// Not a promptSection: the system prompt is a static, cache-stable list and
	// must be byte-identical with the meta event in context.
	sp := assembleSystemPrompt()
	spBefore := assembleSystemPrompt()
	if strings.Contains(sp, hippocampusMetaEventFrame) || strings.Contains(sp, brief.Summary) {
		t.Fatal("Lv-2 output leaked into the system prompt")
	}
	if sp != spBefore {
		t.Fatal("assembling the system prompt is not stable")
	}
	for _, sec := range promptSections {
		if strings.Contains(sec.Text, hippocampusMetaEventFrame) || strings.Contains(sec.Text, brief.Summary) {
			t.Fatalf("a promptSection (%s) carries the meta event", sec.Name)
		}
	}
	// The framing difference from Lv-1 is real and checkable on the wire.
	if strings.HasPrefix(injections[0].Text, hippocampusAppendFrame) {
		t.Fatal("Lv-2 used the Lv-1 frame — the two channels would be indistinguishable")
	}
}

// ---------------------------------------------------------------------------
// Lv-3: quota + the degradation path (over quota ⇒ Lv-1, never a second
// activation, never Interrupt()/ctx-cancel).
// ---------------------------------------------------------------------------

func TestHippocampus_Lv3_QuotaDegradesWithoutSpawningActivations(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	cfg := testHippoCfg(hippocampusLv3SoftInterrupt)
	cfg.Lv3MaxPerSession = 1
	cfg.Lv3MinInterval = hippocampalLv3QuotaWindow
	// The recall budget is spent faster than the boundary count on purpose: the
	// third boundary must have NOTHING in flight (no recall, no injection), so
	// "exceeding the quota degrades" cannot be confused with "the host keeps
	// spawning activations".
	cfg.MaxRecallPerRun = 2

	var calls int32
	// A stub whose state CHANGES per call: each boundary reports one further
	// lead, which is exactly the case where the budget (not convergence) is
	// what must refuse the third activation.
	h := newHippocampusHost(cfg,
		stubRecallVar(t, &calls, func(n int) *hippocampusBrief {
			return &hippocampusBrief{Confidence: 0.95,
				Summary: fmt.Sprintf("the plan contradicts a known pitfall (%d)", n),
				Keys:    []string{"pitfall", fmt.Sprintf("lead%d", n)}}
		}))
	a.Hippocampus = h

	ctx := context.Background()
	a.interruptBase = context.Background() // an Agent with a cancel root, like NewAgent's
	h.setBase(a.interruptRoot())
	h.StartOfRun(ctx, a)
	if got := h.budget.recalls; got != 1 {
		t.Fatalf("start-of-run recalls = %d, want 1", got)
	}

	first := a.roundBoundaryInjections(ctx)
	if len(first) != 1 || first[0].Level != hippocampusLv3SoftInterrupt {
		t.Fatalf("first brief must be a granted Lv-3: %+v", first)
	}
	if !strings.Contains(first[0].Text, hippocampusInterruptInstruction) {
		t.Fatalf("Lv-3 frame missing: %q", first[0].Text)
	}
	// The scheduler launched exactly ONE follow-up recall — a granted Lv-3
	// never spawns a burst.
	if got := h.budget.recalls; got != 2 {
		t.Fatalf("recalls after the granted boundary = %d, want 2", got)
	}

	second := a.roundBoundaryInjections(ctx)
	if len(second) != 1 || second[0].Level != hippocampusLv2MetaEvent {
		t.Fatalf("an over-quota Lv-3 request must DEGRADE one level (Lv-2 here), never escalate: %+v", second)
	}
	if strings.Contains(second[0].Text, hippocampusInterruptInstruction) {
		t.Fatal("the degraded brief still carries the Lv-3 frame")
	}
	if got := h.budget.lv3Count; got != 1 {
		t.Fatalf("lv3 activations = %d, want the quota's 1", got)
	}
	if got := h.budget.recalls; got != 2 {
		t.Fatalf("recalls = %d, want the budget's 2 (one per boundary, no retry storm)", got)
	}

	// Budget exhausted: the host stops recalling entirely — no unbounded
	// activations at the tail of a long run. (Nothing is in flight, so the
	// boundary has nothing to deliver either.)
	third := a.roundBoundaryInjections(ctx)
	if len(third) != 0 {
		t.Fatalf("delivery must stop once the recall budget is spent: %+v", third)
	}
	if got := h.budget.recalls; got != 2 {
		t.Fatalf("recalls grew past the budget: %d", got)
	}

	// Lv-3 NEVER hard-stops the run: it is a scheduler decision, not the user's
	// interrupt. Assert the run's cancel root and the latch are untouched.
	if a.interrupted {
		t.Fatal("Lv-3 used the hard-stop path (interrupt latch set) — the design forbids it")
	}
	if err := a.interruptRoot().Err(); err != nil {
		t.Fatalf("Lv-3 cancelled the run context: %v", err)
	}
}

// TestHippocampus_Lv3_QuotaWindow asserts the OTHER half of the quota: two
// high-confidence requests inside the spacing window ⇒ the second is refused
// (even though the per-session cap still has room) and degrades to Lv-1.
func TestHippocampus_Lv3_QuotaWindow(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	cfg := testHippoCfg(hippocampusLv3SoftInterrupt)
	cfg.Lv3MaxPerSession = 5 // room for both — the WINDOW is what must refuse
	cfg.Lv3MinInterval = hippocampalLv3QuotaWindow

	h := newHippocampusHost(cfg,
		stubRecall(t, new(int32), &hippocampusBrief{Confidence: 0.9, Summary: "urgent lead", Keys: []string{"urgent"}}, nil))
	h.setBase(a.interruptRoot())

	first := h.adjudicate(&hippocampusBrief{Confidence: 0.9, Summary: "urgent lead", Keys: []string{"urgent"}}, 0)
	if first != hippocampusLv3SoftInterrupt {
		t.Fatalf("first adjudication = %v, want Lv-3", first)
	}
	// A brief reporting a DIFFERENT memory state (so the convergence gate is
	// not what refuses it) and round 0 (the spacing gate is silent there): the
	// only remaining reason to refuse Lv-3 is the quota window.
	second := h.adjudicate(&hippocampusBrief{Confidence: 0.9, Summary: "urgent lead + new", Keys: []string{"urgent", "fresh"}}, 0)
	if second != hippocampusLv2MetaEvent {
		t.Fatalf("second adjudication inside the window = %v, want the one-level degradation (Lv-2)", second)
	}
	// Same window, lower ceiling: the degradation bottoms out at Lv-1.
	low := newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	if got := low.adjudicate(&hippocampusBrief{Confidence: 0.9, Summary: "urgent lead", Keys: []string{"urgent"}}, 0); got != hippocampusLv1Append {
		t.Fatalf("ceiling Lv-1 adjudicated %v", got)
	}
	if h.budget.lv3Count != 1 {
		t.Fatalf("lv3 activations = %d, want 1", h.budget.lv3Count)
	}
}

// TestHippocampus_ConfidenceVeto: a low-confidence brief is archived, never
// injected — the scheduler's veto (devdoc §7 "置信度低于阈值被拒").
func TestHippocampus_ConfidenceVetoDegradesToArchive(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	h := newHippocampusHost(testHippoCfg(hippocampusLv3SoftInterrupt),
		stubRecall(t, new(int32), &hippocampusBrief{Confidence: 0.05, Summary: "weak guess", Keys: []string{"weak"}}, nil))
	a.Hippocampus = h

	ctx := context.Background()
	h.StartOfRun(ctx, a)
	if inj := a.roundBoundaryInjections(ctx); len(inj) != 0 {
		t.Fatalf("below-floor confidence must not reach the context: %+v", inj)
	}
	a.SessionLog.Sync()
	ev := findSessionEvent(readSessionJSONL(t, a.SessionLog.path), hippocampusBriefEvent)
	if ev == nil || ev[hippocampusLogKeyLevel] != hippocampusLvTextArchive {
		t.Fatalf("vetoed brief must still be archived at Lv-0: %+v", ev)
	}
	// And a failed recall is recorded, never delivered (Rule 15).
	h2 := newHippocampusHost(testHippoCfg(hippocampusLv3SoftInterrupt),
		stubRecall(t, new(int32), nil, fmt.Errorf("memory db unopenable")))
	h2.setRecall(func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		return nil, fmt.Errorf("memory db unopenable")
	})
	h2.StartOfRun(ctx, a)
	if inj := h2.AtRoundBoundary(ctx, a); len(inj) != 0 {
		t.Fatalf("a failed recall must not be injected: %+v", inj)
	}
}

// ---------------------------------------------------------------------------
// Invocation path: the gate supersedes the passive task-start injection.
// ---------------------------------------------------------------------------

func TestHippocampus_StartOfRunSupersedesTaskStartSearch(t *testing.T) {
	off := normalizeSessionLog(t, runOneScriptedSession(t, false), "", nil)
	on := normalizeSessionLog(t, runOneScriptedSession(t, true), "", nil)
	if !strings.Contains(off, "TASK-START CHECKPOINT") {
		t.Fatal("gate off: the task-start search checkpoint is missing — today's behaviour was dropped")
	}
	if strings.Contains(on, taskStartSearchMessage) {
		t.Fatal("double injection: the forced task-start search runs while the organ is on")
	}
	if !strings.Contains(on, hippocampusAppendFrame) {
		t.Fatal("gate on: the organ delivered nothing at the round boundary")
	}
}

// TestHippocampus_GateIsTheOnlySwitch pins the env gate itself: off ⇒ no host
// (no code path), on ⇒ a host, and the knobs come from the constants.
func TestHippocampus_GateAndKnobs(t *testing.T) {
	newTestAgentEnv(t)
	off, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if off.Hippocampus != nil {
		t.Fatal("GOPI_HIPPOCAMPUS unset must build NO host")
	}

	t.Setenv(hippocampusEnvEnabled, "1")
	on, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if on.Hippocampus == nil {
		t.Fatal("GOPI_HIPPOCAMPUS=1 must build the host")
	}
	if got := on.Hippocampus.maxLv; got != hippocampusDefaultMaxLv {
		t.Fatalf("default max level = %v, want %v", got, hippocampusDefaultMaxLv)
	}
	// NewAgent BINDS the production organ (epic child 4/4): an enabled host
	// whose organ is missing would archive a "retrieval failed" marker every
	// run and could never activate — the exact gap this task closed.
	if on.Hippocampus.recallFn() == nil {
		t.Fatal("NewAgent must bind the production organ when the gate is on")
	}
	// The binding is inert until a boundary asks for it, and the task-start
	// hook now drives the ORGAN (it supersedes the passive injection).
	if got := on.roundBoundaryInjections(context.Background()); len(got) != 0 {
		t.Fatalf("no brief was in flight, yet %+v was delivered", got)
	}
	on.startOfRunFor(context.Background())
	// A BOUND host supersedes the passive injection: the organ's recall IS the
	// run's entry point (devdoc §4.2 禁止双注入), so no forced SEARCH message is
	// added. The unbound fallback is covered by its own test below.
	//
	// The assertion is on the SEARCH message's own text, not on an empty
	// context: "supersedes the passive injection" is a claim about the recall
	// entry point, and the run legitimately carries OTHER one-shot harness
	// messages (the SELF-RECORDING contract, selfrecord.go) that this gate has
	// no business suppressing — an empty-context assertion would have made the
	// two indistinguishable, and it was tightened here rather than relaxing the
	// supersede rule.
	for _, m := range on.Ctx.Messages {
		if strings.HasPrefix(m.Content, taskStartSearchMessage) {
			t.Fatalf("a bound host must supersede the passive injection, got %q", m.Content)
		}
	}
	// The binding is only accepted while the gate is on.
	off.SetHippocampusRecall(func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		t.Fatal("the inert (gate-off) agent must never recall")
		return nil, nil
	})
	if ok := on.SetHippocampusRecall(func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		return nil, nil
	}); !ok {
		t.Fatal("SetHippocampusRecall refused an enabled agent")
	}

	t.Setenv(hippocampusEnvMaxLv, "3")
	t.Setenv(hippocampusEnvMaxRecallPerRun, "0") // non-positive keeps the default
	cfg := loadHippocampusConfig()
	if cfg.MaxLv != hippocampusLv3SoftInterrupt {
		t.Fatalf("GOPI_HIPPOCAMPUS_MAX_LV=3 → %v", cfg.MaxLv)
	}
	if cfg.MaxRecallPerRun != hippocampusDefaultMaxRecallPerRun {
		t.Fatalf("non-positive override changed the recall budget: %d", cfg.MaxRecallPerRun)
	}
	t.Setenv(hippocampusEnvMaxLv, "99") // out of range clamps, never unlocks
	if got := loadHippocampusConfig().MaxLv; got != hippocampusLv3SoftInterrupt {
		t.Fatalf("out-of-range level = %v, want the clamp to Lv-3", got)
	}
	t.Setenv(hippocampusEnvMaxLv, "not-a-number")
	if got := loadHippocampusConfig().MaxLv; got != hippocampusDefaultMaxLv {
		t.Fatalf("unparseable level = %v, want the constant default", got)
	}
}

// ---------------------------------------------------------------------------
// Hard stop: Interrupt()/ctx-cancel ends an in-flight recall promptly, with a
// bounded goroutine count.
// ---------------------------------------------------------------------------

func TestHippocampus_HardStopEndsInFlightRecall(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(hippocampusEnvEnabled, "1")
	a, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if a.SessionLog != nil {
		defer a.SessionLog.Close()
	}

	entered := make(chan struct{})
	exited := make(chan struct{})
	a.SetHippocampusRecall(func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		close(entered)
		<-ctx.Done() // a recall that only ends when its context is cancelled
		close(exited)
		return nil, ctx.Err()
	})

	baseline := runtime.NumGoroutine()
	h := a.Hippocampus
	h.StartOfRun(a.interruptRoot(), a)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the recall never started")
	}

	start := time.Now()
	a.Interrupt() // the user's hard stop (signal handler path)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("Agent.Interrupt() did not end the in-flight recall")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("hard stop took %v — an in-flight hippocampal cycle must end promptly", elapsed)
	}
	if !a.interrupted {
		t.Fatal("the interrupt latch did not fire")
	}
	if err := a.interruptRoot().Err(); err == nil {
		t.Fatal("the run's cancel root is not cancelled after Interrupt()")
	}

	// Bounded leak check: the recall goroutine has returned, so the process
	// goroutine count settles back to the baseline.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= baseline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count %d never returned to the baseline %d — leaked recall",
				runtime.NumGoroutine(), baseline)
		}
		time.Sleep(10 * time.Millisecond) //stylegate:allow-sleep distinct timestamps are required to assert hit ordering
	}

	// EndOfRun after a hard stop must not hang on the (already dead) recall.
	done := make(chan struct{})
	go func() { h.EndOfRun(a.interruptRoot(), a); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("EndOfRun blocked after a hard stop")
	}
}

// ---------------------------------------------------------------------------
// Default-off equivalence: with the gate OFF the session reads exactly like
// today's; with it ON, the ONLY delta is the hippocampus' own artifact (the
// archive events + the framed injected messages) — i.e. the enabled host is
// inert where it is not delivering, and the forced task-start search is
// superseded rather than duplicated.
// ---------------------------------------------------------------------------

// normalizedLog is one scripted session reduced to its deterministic content:
// the JSONL with volatile fields dropped and the run's own identifiers
// (session uuid, fixture directory) replaced by placeholders.
type normalizedLog struct {
	Text string
}

// normalizeSessionLog reads a session JSONL and drops the volatile fields
// (plus any event/frame the caller excludes), returning a deterministic
// rendering of the rest.
func normalizeSessionLog(t *testing.T, run *scriptedRun, dropEventType string, dropFrames []string) string {
	t.Helper()
	if run == nil {
		t.Fatal("no run to normalize")
	}
	events := readSessionJSONL(t, run.path)
	var sb strings.Builder
	for _, ev := range events {
		if dropEventType != "" && ev["type"] == dropEventType {
			continue
		}
		if len(dropFrames) > 0 && droppedFrame(ev, dropFrames) {
			continue // the whole event is the hippocampus artifact — drop it
		}
		for _, k := range volatileLogKeys {
			delete(ev, k)
		}
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		sb.WriteString(string(line))
		sb.WriteString("\n")
	}
	return normalizeRunArtifacts(sb.String(), run)
}

// droppedFrame reports whether the event is an injected hippocampus message
// carrying one of the frames (i.e. an artifact the equivalence check removes).
func droppedFrame(ev map[string]interface{}, frames []string) bool {
	msg, ok := ev["message"].(map[string]interface{})
	if !ok {
		return false
	}
	text, _ := msg["content"].(string)
	if text == "" {
		return false
	}
	for _, f := range frames {
		if strings.Contains(text, f) {
			return true
		}
	}
	return false
}

// volatileLogKeys are the fields that legitimately differ between two runs of
// the same input: wall-clock timestamps, per-run identifiers, and the token
// figures MEASURED AGAINST a prompt whose size changes with them.
//
// task_id is the run's OWN ledger row id (a per-run uuid minted by the start
// hook), carried by the task_row_closed event the end hook writes at its
// terminal ledger write — a per-run identifier like sessionId, and the reason
// it belongs here. The event itself is NOT dropped: two runs of the same input
// must still agree on the close-out it records (status, reason).
//
// The token-reconciliation figures are the second half of that reasoning, and
// they are named from their ONE definition site (token_reconciliation.go's
// reconField* constants, Rule 1/13/14) rather than typed again here. A
// token_reconciliation row records how many tokens the request's prompt
// MEASURED — a derived quantity of a text, not a fact about the run's
// behaviour. This comparison removes the hippocampus' frames from the message
// events (droppedFrame) and the superseded task-start checkpoint
// (stripFramedUserMessage), so the prompt each arm measured is intentionally a
// DIFFERENT text, and a measurement of it cannot be equal: measured on this
// tree, the gate-off prompt (carrying the long TASK-START CHECKPOINT) predicts
// 51669 tokens where the gate-on one (checkpoint replaced by the shorter
// hippocampus brief) predicts 51430 — a stable 239-token gap that has nothing
// to do with behaviour. The row itself still stays in the comparison and still
// asserts what it exists to assert: that a reconciliation row was written, for
// which frame, and whether its prediction was anchored.
var volatileLogKeys = []string{
	"timestamp", "sessionId", "session_id", "task_id", "promptId", "elapsed_sec", "duration_ms",
	reconFieldPredictedTokens, reconFieldDeltaTokens, reconFieldDeltaPctBasisPoints,
}

// The placeholders a normalized log carries in place of the run's own ids.
const (
	sessionIDPlaceholder     = "<SESSION-ID>"
	fixtureDirPlaceholder    = "<FIXTURE-DIR>"
	tickPlaceholder          = "[t+Ns]"
	wallclockPlaceholder     = "<WALLCLOCK>"
	tickGapPlaceholder       = "+<GAP>"
	budgetElapsedPlaceholder = "<ELAPSED>"
	budgetPercentPlaceholder = "<PERCENT>"
)

// sessionTickRe matches the session clock's tick label ("[t+12s]", and the
// folded "[t+~31m]"/"[t+~2h]" forms), which the harness embeds in tool results
// and requests — an elapsed reading, so two runs of the same input legitimately
// differ. Normalizing it is what makes the gate-off comparison a byte
// comparison at all. It is the package's own tickRe (one definition of the
// clock's spelling, Rule 14): a regexp that knew only the seconds form would
// silently stop normalizing the moment the clock passed a minute, and the
// comparison would fail for a reason that has nothing to do with the run.
var sessionTickRe = tickRe

// durationUnitClass is the character class of the ONE letter durationTier paints
// (clock.go: "%ds"/"%dm"/"%dh"/"%dd"). Named rather than inlined into each
// pattern below so a fourth tier added there is a one-line change here — the
// same agreement sessionTickRe already relies on for the tick.
const durationUnitClass = `[smhd]`

// The BUDGET and the turn stamp's GAP are ELAPSED READINGS, like the tick: both
// are computed from the session clock's wallclock, so two runs of the same input
// legitimately differ in them and a byte comparison must not read their digits.
// They are masked by the LINE's own shape rather than by a value pattern,
// because the elapsed is a Go time.Duration rendered with %v ("0s", "1m5s",
// "2h0m3s") — a pattern that knew only "Ns" would stop masking the moment a run
// crossed a minute, which is the failure mode sessionTickRe's comment already
// records for the tick. Every pattern is anchored on a PRODUCTION constant
// (sessionBudgetPrefix, turnStampGapSuffix), so a reworded renderer breaks the
// mask loudly here instead of silently leaving the comparison failing for a
// reason that has nothing to do with the runs it compares (Rules 1/13/14).
var (
	// "session budget: 1m5s elapsed of 1h0m0s (0% used)." — the plain report.
	sessionBudgetElapsedRe = regexp.MustCompile(sessionBudgetPrefix + `[0-9][0-9.hms]* elapsed`)
	// "…, 57m remaining of …" — the escalation report's second reading.
	sessionBudgetRemainingRe = regexp.MustCompile(`[0-9][0-9.hms]* remaining\b`)
	// "(0% used" — derived from elapsed, so the percentage moves with it.
	sessionBudgetPercentRe = regexp.MustCompile(`\(\d+% used\b`)
	// "+0s since your previous request." — the stamp's gap half, the same
	// clock reading as the tick and rendered by the same durationTier.
	turnStampGapRe = regexp.MustCompile(`\+[0-9]+` + durationUnitClass + ` ` + regexp.QuoteMeta(turnStampGapSuffix))
)

// normalizeRunArtifacts replaces every per-run identifier and wall-clock
// reading a session embeds with a stable placeholder.
func normalizeRunArtifacts(s string, run *scriptedRun) string {
	s = strings.ReplaceAll(s, run.sessionID, sessionIDPlaceholder)
	s = strings.ReplaceAll(s, run.dir, fixtureDirPlaceholder)
	// The per-request stamp carries an ABSOLUTE wallclock (clock.go
	// TurnStamp.Render), which two runs of the same input legitimately differ in
	// for the same reason they differ in their tick — it is a reading of WHEN,
	// not of WHAT. It is normalized through the pattern the stamp's own test pins
	// to the production layout (turnstamp_wallclock_test.go), so a re-spelled
	// wallclock cannot leave this comparison failing for a reason unrelated to
	// the runs it compares.
	s = stampWallclockRe.ReplaceAllString(s, wallclockPlaceholder)
	// The budget report's two elapsed readings and the percentage derived from
	// them. This is the SAME class of reading as the tick — a measurement of
	// the run's own wallclock — and the reason the baseline determinism check
	// used to fail once per second boundary ("1s elapsed" vs "0s elapsed").
	s = sessionBudgetElapsedRe.ReplaceAllString(s, budgetElapsedPlaceholder+" elapsed")
	s = sessionBudgetRemainingRe.ReplaceAllString(s, budgetElapsedPlaceholder+" remaining")
	s = sessionBudgetPercentRe.ReplaceAllString(s, "("+budgetPercentPlaceholder+" used")
	// The stamp's gap half, likewise an elapsed reading of the same clock.
	s = turnStampGapRe.ReplaceAllString(s, tickGapPlaceholder+" "+turnStampGapSuffix)
	return sessionTickRe.ReplaceAllString(s, tickPlaceholder)
}

// scriptedRun is what one scripted session leaves behind for comparison.
type scriptedRun struct {
	path      string
	dir       string
	sessionID string
}

// runOneScriptedSession drives the real agent loop (3 rounds: tool call,
// end_turn, confirmation) against a mock LLM, with the gate in the requested
// state, and returns the run's identifying parts.
func runOneScriptedSession(t *testing.T, hippocampusOn bool) *scriptedRun {
	t.Helper()
	newTestAgentEnv(t)
	t.Setenv("GOPI_RAW_LOG", "OFF")
	if hippocampusOn {
		t.Setenv(hippocampusEnvEnabled, "1")
	}
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello from gopi test file\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	argsBytes, _ := json.Marshal(map[string]interface{}{"path": testFile}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONStr, _ := json.Marshal(string(argsBytes))                      //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"read\",\"input\":{}}}")
			writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsJSONStr))
			writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
			writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
			writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
		default:
			// Every later round ends the turn: the loop's continuation probe
			// produces the fixed 3-round shape the off-run comparison uses.
			claudeTextStream(w, "All tasks completed.", "end_turn")
		}
	}))
	defer srv.Close()

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	ag.Client = &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: &http.Client{}}
	ag.MaxIterations = 5
	if hippocampusOn {
		if ag.Hippocampus == nil {
			t.Fatal("gate on but no host was built")
		}
		// A deterministic organ: no network, no DB — fixed signal, fixed body.
		ag.SetHippocampusRecall(func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
			return &hippocampusBrief{Confidence: 0.9, MemoryWeight: 0.5, Memories: 2,
				DurationMS: 3, Summary: "past session did exactly this"}, nil
		})
	}
	if _, err := ag.Run("What is in the test file?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ag.SessionLog == nil {
		t.Fatal("no session log")
	}
	run := &scriptedRun{path: ag.SessionLog.path, dir: dir, sessionID: ag.SessionLog.SessionID()}
	ag.SessionLog.Close() //stylegate:ignore test closes the session log after the run so the JSONL is flushed to disk before the off/on comparison reads it; a flush error appears as missing content in that comparison
	return run
}

// TestHippocampus_DefaultOffLeavesBehaviourUnchanged is 口径 1's machine proof
// (N9): the gate off runs exactly the rounds it ran before (the forced
// task-start search is still there), and the gate on byte-identical to it once
// the hippocampus' own artifacts are removed.
// firstLineDiff renders the first differing line pair of two logs (a
// determinism failure is unreadable without it).
func firstLineDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n  A: %s\n  B: %s", i+1, x, y)
		}
	}
	return "(no differing line)"
}

func TestHippocampus_DefaultOffLeavesBehaviourUnchanged(t *testing.T) {
	offRun := runOneScriptedSession(t, false)
	upRun := runOneScriptedSession(t, false)
	baseline := normalizeSessionLog(t, offRun, "", nil)
	if again := normalizeSessionLog(t, upRun, "", nil); again != baseline {
		t.Fatalf("the harness itself is not deterministic — the equivalence claim needs a stable baseline:\n%s",
			firstLineDiff(baseline, again))
	}
	if !strings.Contains(baseline, "TASK-START CHECKPOINT") {
		t.Fatal("gate off lost today's task-start checkpoint")
	}

	onRun := runOneScriptedSession(t, true)
	on := normalizeSessionLog(t, onRun, "", nil)
	if on == baseline {
		t.Fatal("gate on produced no hippocampus artifact — the wiring never fired in the real loop")
	}
	if strings.Contains(on, taskStartSearchMessage) {
		t.Fatal("double injection: the forced task-start search is still present with the organ on")
	}
	stripped := normalizeSessionLog(t, onRun, hippocampusBriefEvent,
		[]string{hippocampusAppendFrame, hippocampusMetaEventFrame, hippocampusInterruptInstruction})
	// The superseded checkpoint is the ONE intended removal, so restore its
	// line to the baseline side before comparing the remainder.
	baselineWithoutCheckpoint := stripFramedUserMessage(t, offRun, taskStartSearchMessage, 1)
	if stripped != baselineWithoutCheckpoint {
		t.Fatalf("gate-on session differs from gate-off beyond the hippocampus artifacts:\n--- want ---\n%s\n--- got ---\n%s",
			baselineWithoutCheckpoint, stripped)
	}
}

// TestNormalizeRunArtifacts_MasksEveryElapsedReading is the regression guard for
// the baseline determinism check above: the equivalence claim rests on the
// normalizer, so the normalizer's own scope is pinned here rather than inferred
// from a green run (a mask that keyed on "Ns" alone passed every comparison until
// the day a run crossed a minute — memory #6770's failure mode).
//
// It is deliberately NON-VACUOUS in both directions: the elapsed readings must
// normalize AWAY (the wallclock readings two runs legitimately differ in), and
// the CONTENT of the line must survive (a normalizer that swallowed the whole
// line would make the comparison above pass by comparing nothing).
func TestNormalizeRunArtifacts_MasksEveryElapsedReading(t *testing.T) {
	run := &scriptedRun{sessionID: "session-uuid", dir: "/fixture/dir"}
	render := func(elapsed, gap, percent string) string {
		return "[system] [t+0s] @ 2026-11-07T07:28:30+08:00 — " + gap +
			" since your previous request. session budget: " + elapsed + " elapsed of 1h0m0s (" + percent + "% used)."
	}
	// Two runs of the SAME input, one second apart — the exact pair that made the
	// baseline check red (measured: "1s elapsed" against "0s elapsed").
	slow := normalizeRunArtifacts(render("1s", "+0s", "0"), run)
	fast := normalizeRunArtifacts(render("0s", "+0s", "0"), run)
	if slow != fast {
		t.Fatalf("two elapsed readings one second apart did not normalize equal:\n  A: %s\n  B: %s", slow, fast)
	}
	// Every tiered spelling of the same reading, including the forms Go's
	// time.Duration %v paints once a run is past a minute or an hour.
	for _, elapsed := range []string{"59s", "1m5s", "1h0m0s", "2h3m4s"} {
		if got := normalizeRunArtifacts(render(elapsed, "+9m", "17"), run); got != slow {
			t.Fatalf("elapsed %q did not normalize to the same text:\n  want: %s\n  got:  %s", elapsed, slow, got)
		}
	}
	// The CONTENT must survive: a different budget, or a different clause, is a
	// real difference and normalization may not hide it.
	if same := normalizeRunArtifacts(render("0s", "+0s", "0"), run); same == "" {
		t.Fatal("normalization produced an empty string — the comparison would be vacuous")
	}
	if strings.Contains(slow, "1s elapsed") || strings.Contains(slow, "(0% used") {
		t.Fatalf("a wallclock reading survived normalization: %s", slow)
	}
	for _, want := range []string{budgetElapsedPlaceholder + " elapsed of 1h0m0s", "(" + budgetPercentPlaceholder + " used", turnStampGapSuffix} {
		if !strings.Contains(slow, want) {
			t.Fatalf("normalized text lost %q: %s", want, slow)
		}
	}
	other := normalizeRunArtifacts(render("0s", "+0s", "0")+" a different clause", run)
	if other == slow {
		t.Fatal("normalization hid a real content difference — the equivalence check would be vacuous")
	}
}

// stripFramedUserMessage re-renders a session log with ONE matching user
// message removed (the superseded checkpoint the enabled gate replaces).
func stripFramedUserMessage(t *testing.T, run *scriptedRun, text string, n int) string {
	t.Helper()
	events := readSessionJSONL(t, run.path)
	var sb strings.Builder
	removed := 0
	for _, ev := range events {
		if removed < n && ev["type"] == "user" {
			if msg, ok := ev["message"].(map[string]interface{}); ok && msg["content"] == text {
				removed++
				continue
			}
		}
		for _, k := range volatileLogKeys {
			delete(ev, k)
		}
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		sb.WriteString(string(line))
		sb.WriteString("\n")
	}
	// The hot-resident digest rides with the checkpoint (MemoryHotResident is
	// best-effort and empty in the isolated test home) — assert the shape held,
	// so a future digest does not silently invalidate the comparison.
	if removed != n {
		t.Fatalf("expected to strip %d checkpoint message(s), removed %d", n, removed)
	}
	return normalizeRunArtifacts(sb.String(), run)
}

// TestHippocampus_ChatPathClampsToLv1 pins the §4.1 rule: a path with no task
// flow to soft-interrupt can never receive Lv-3, and the clamp never RAISES a
// configured ceiling.
func TestHippocampus_ChatPathClampsToLv1(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(hippocampusEnvEnabled, "1")
	t.Setenv(hippocampusEnvMaxLv, "3")
	a, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if got := a.Hippocampus.maxLv; got != hippocampusLv3SoftInterrupt {
		t.Fatalf("configured ceiling = %v, want Lv-3", got)
	}
	a.Hippocampus.clampMaxLv(hippocampusLv1Append)
	if got := a.Hippocampus.maxLv; got != hippocampusLv1Append {
		t.Fatalf("chat clamp = %v, want Lv-1", got)
	}
	a.Hippocampus.clampMaxLv(hippocampusLv3SoftInterrupt) // may not raise
	if got := a.Hippocampus.maxLv; got != hippocampusLv1Append {
		t.Fatalf("clamp raised the ceiling to %v", got)
	}
	if b := newHippocampusHost(testHippoCfg(hippocampusLv3SoftInterrupt), nil); b != nil {
		b.clampMaxLv(hippocampusLv1Append)
		if lv := b.adjudicate(&hippocampusBrief{Confidence: 1, Summary: "x", Keys: []string{"x"}}, 0); lv != hippocampusLv1Append {
			t.Fatalf("clamped host adjudicated %v, want Lv-1", lv)
		}
	}
}

// ---------------------------------------------------------------------------
// Rule 13: the config/audit values exist ONCE. The gate is the only
// anti-regression guard against a future re-inline (a bare "GOPI_HIPPOCAMPUS_*"
// read or a bare level token bypassing the constant).
// ---------------------------------------------------------------------------

// TestHippocampus_LiteralGate pins every value of this feature to its single
// definition site: the env namespace lives only in hippocampus_config.go (the
// ONE env reader, Rule 2) and the audit tokens/event type only in
// hippocampus.go (whose Lv-0 archive writers are the only producers).
func TestHippocampus_LiteralGate(t *testing.T) {
	cases := []struct {
		holder string   // the ONE production file allowed to carry the literals
		lits   []string // the literals themselves
	}{
		{holder: "hippocampus_config.go", lits: []string{
			hippocampusEnvEnabled, hippocampusEnvMaxLv, hippocampusEnvMaxRecallPerRun,
			hippocampusEnvTimeoutSec, hippocampusEnvLv3MinIntervalSec, hippocampusEnvLv3MaxPerSession,
		}},
		// The level tokens, the event type and the distinctive signal keys. The
		// GENERIC JSON field names ("summary" / "confidence" / "round") are
		// deliberately NOT gated: they are field spellings shared with unrelated
		// events all over the package (session.go logs every event the same
		// way), so a package-wide literal scan would be a false positive, not a
		// drift guard. The distinctive values below are the ones a copy-paste
		// could silently mismatch.
		{holder: "hippocampus.go", lits: []string{
			hippocampusBriefEvent,
			hippocampusLvTextArchive, hippocampusLvTextAppend, hippocampusLvTextMetaEvent, hippocampusLvTextSoftInterrupt,
			hippocampusLogKeyMemoryWeight, hippocampusLogKeyDurationMS,
		}},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, c := range cases {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") || e.Name() == c.holder {
				continue
			}
			b, err := os.ReadFile(e.Name())
			if err != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), err)
			}
			for _, lit := range c.lits {
				if strings.Contains(string(b), `"`+lit+`"`) {
					t.Errorf("%s carries the literal %q — reference the constant (defined in %s)", e.Name(), lit, c.holder)
				}
			}
		}
	}
}

// TestHippocampus_GateAndKnobs asserts the knob defaults: the gate's OFF state
// is a nil host (not a disabled-but-present one) and the shipped ceiling is
// Lv-1 — stage 1 of the epic (devdoc/hippocampus.md §5).
func TestHippocampus_DefaultGateIsOffAtLv1(t *testing.T) {
	newTestAgentEnv(t)
	if cfg := loadHippocampusConfig(); cfg.Enabled {
		t.Fatal("the gate is ON by default — the default-off contract is broken")
	} else if cfg.MaxLv != hippocampusLv1Append {
		t.Fatalf("shipped ceiling = %v, want Lv-1", cfg.MaxLv)
	}
}

// ---------------------------------------------------------------------------
// Noise gates (epic child 4/4). Measured in a REAL session: with the naive
// rule the host delivered one memo per round — the same five leads, the same
// saturation confidence, eight rounds running (see
// tasks/01a08919-9399-report.md). These two tests are the repro.
// ---------------------------------------------------------------------------

// TestHippocampus_ConvergenceGateSuppressesRepeatedState: a brief that reports
// memory state the model has already been shown is archived (Lv-0), never
// injected again — the fix for "one identical memo per round".
func TestHippocampus_ConvergenceGateSuppressesRepeatedState(t *testing.T) {
	h := newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	brief := func() *hippocampusBrief {
		return &hippocampusBrief{Confidence: 0.9, Summary: "same 5 leads as last round",
			Keys: []string{"none#0", "#871", "#1330"}}
	}
	if got := h.adjudicate(brief(), 1); got != hippocampusLv1Append {
		t.Fatalf("the run's first brief = %v, want Lv-1 (a first brief is never suppressed)", got)
	}
	// Round 5 is past the spacing interval, so convergence is the ONLY reason
	// this can be refused.
	if got := h.adjudicate(brief(), 5); got != hippocampusLv0Archive {
		t.Fatalf("a repeated memory state = %v, want Lv-0 (archive only)", got)
	}
	// One new key = a changed state ⇒ deliverable again (past the interval).
	changed := brief()
	changed.Keys = append(changed.Keys, "#1600")
	if got := h.adjudicate(changed, 8); got != hippocampusLv1Append {
		t.Fatalf("a brief with fresh state = %v, want Lv-1", got)
	}
}

// TestHippocampus_MinInjectIntervalSpacing: even a genuinely new brief waits
// for the interval — at most one delivery per few rounds.
func TestHippocampus_MinInjectIntervalSpacing(t *testing.T) {
	h := newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	if got := h.adjudicate(&hippocampusBrief{Confidence: 0.9, Summary: "lead one", Keys: []string{"a"}}, 1); got != hippocampusLv1Append {
		t.Fatalf("first delivery = %v, want Lv-1", got)
	}
	near := func(round int) hippocampusLevel {
		return h.adjudicate(&hippocampusBrief{Confidence: 0.9,
			Summary: fmt.Sprintf("lead %d", round), Keys: []string{"a", fmt.Sprintf("r%d", round)}}, round)
	}
	for _, r := range []int{2, 3} {
		if got := near(r); got != hippocampusLv0Archive {
			t.Fatalf("round %d is inside the %d-round spacing window: %v, want Lv-0",
				r, hippocampusMinInjectIntervalRounds, got)
		}
	}
	if got := near(1 + hippocampusMinInjectIntervalRounds); got != hippocampusLv1Append {
		t.Fatalf("round %d is past the window: %v, want Lv-1", 1+hippocampusMinInjectIntervalRounds, got)
	}
}

// TestHippocampus_UnboundHostFallsBackToPassiveInjection: an enabled host with
// NO organ (a hand-built Agent, or a build where the binding is missing) must
// still get the run's entry point — the fallback is what keeps an
// enabled-but-unbound gate from silently dropping the task-start search.
func TestHippocampus_UnboundHostFallsBackToPassiveInjection(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()
	a.Hippocampus = newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)

	a.startOfRunFor(context.Background())
	if len(a.Ctx.Messages) == 0 || a.Ctx.Messages[0].Content != taskStartSearchMessage {
		t.Fatal("an unbound host dropped the task-start search checkpoint")
	}
}

// ---------------------------------------------------------------------------
// Race repro (bug: pre-existing DATA RACE in the host, fixed 2026-09-10).
// ---------------------------------------------------------------------------

// TestHippocampus_Race_RecallHandoffUnderConcurrentDrain is the minimal repro
// of the unlocked in-flight hand-off: the recall goroutine writes the shared
// in-flight fields from its OWN goroutine (enqueue) while the run goroutine
// reads them in AtRoundBoundary/EndOfRun. Before the fix
//
//	go test ./internal/agent/ -race -run TestHippocampus_Race_RecallHandoffUnderConcurrentDrain
//
// reported "WARNING: DATA RACE — read of *hippoScheduler.inflight at
// AtRoundBoundary (the run goroutine) vs previous write at enqueue (the recall
// goroutine)"; after the fix the same command prints ok.
//
// The organ parks (`release`) until the run goroutine has the slot in hand, so
// both accesses provably happen — and because NEITHER is ordered before the
// other by a lock (the slot's done-channel close orders the wake-up, not the
// fields), the detector reports the race regardless of how the scheduler
// interleaves them. That is the "the channel is not the lock" trap.
func TestHippocampus_Race_RecallHandoffUnderConcurrentDrain(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()

	wrote := make(chan struct{})   // organ → test: the brief exists, the hand-off follows
	release := make(chan struct{}) // test → organ: write the shared fields back now

	var calls int32
	h := newHippocampusHost(testHippoCfg(hippocampusLv0Archive),
		func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				close(wrote)
				<-release
			}
			return &hippocampusBrief{Confidence: 0.9, Summary: "handoff", Keys: []string{"k"}}, nil
		})
	a.Hippocampus = h

	ctx := context.Background()
	h.StartOfRun(ctx, a)
	<-wrote // the first recall is now in flight and parked on the hand-off

	drained := make(chan struct{})
	go func() {
		a.roundBoundaryInjections(ctx)
		close(drained)
	}()
	close(release) // the organ writes the shared fields from ITS goroutine
	<-drained

	// EndOfRun races the same fields on the way out.
	h.EndOfRun(ctx, a)
	a.SessionLog.Sync()
	if ev := findSessionEvent(readSessionJSONL(t, a.SessionLog.path), hippocampusBriefEvent); ev == nil {
		t.Fatal("the handoff brief was never archived — the drain lost it")
	}
}
