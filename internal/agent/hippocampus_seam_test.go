package agent

// hippocampus_seam_test.go — the LIVE recall seam.
//
// newHippocampusRecallFunc is the one function that binds the organ to a real
// agent, so it is the only place where the three deepseek-specific behaviours
// the live path depends on can be exercised at all:
//
//	① thinking MUST be sent as disabled (a flash tier that thinks eats
//	   max_tokens and returns empty content),
//	② a GLM-style always-thinking endpoint REJECTS that (HTTP 400 / 1210) and
//	   must be retried once with reasoning_effort instead,
//	③ a reply with no JSON object is a retryable SHAPE (the organ's
//	   ClientAdapter re-asks), while empty content is a recorded failure —
//	   never a silent "".
//
// The JSON-reply fixtures here parse the shell of a {"verb":…} decision and
// answer with a different raw text per reply, which is what lets one fake
// endpoint drive the retry twice and then succeed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/hippocampus"
	"github.com/nickhuang99/gopi/internal/tools"
)

// Seam-test wire literals (Rule 1: one named place per literal).
const (
	// hippoSeamQueriesReply is a valid divergent-step reply (one query).
	hippoSeamQueriesReply = `{"queries":["seam probe"]}`
	// hippoSeamNoneReply is a valid decision reply: the "none" verb ends the
	// cycle, which keeps the fake endpoint's call sequence short.
	hippoSeamNoneReply = `{"verb":"none","reason":"nothing to do"}`
	// hippoSeamProseNoJSON is a reply carrying NO JSON object — the shape the
	// organ must retry.
	hippoSeamProseNoJSON = "I could not decide, sorry."
	// hippoSeamAlwaysThinkingBody is the GLM always-thinking rejection the
	// adapter's fallback matches (code 1210).
	hippoSeamAlwaysThinkingBody = `{"error":{"code":1210,"message":"该模型不支持关闭思考 (always think)"}`
	// hippoSeamKeyHeader is the header the Claude-compatible endpoint uses.
	hippoSeamKeyHeader = "x-api-key"
)

// hippoSeamRequest is the subset of the request shell this test inspects.
type hippoSeamRequest struct {
	MaxTokens       int                      `json:"max_tokens"`
	Thinking        *deepseek.ClaudeThinking `json:"thinking"`
	ReasoningEffort string                   `json:"reasoning_effort"`
}

// hippoSeamEndpoint is a fake organ endpoint: it records every request it saw
// and replies with the next scripted response. The last scripted entry repeats,
// so a test only lists the replies it cares about.
type hippoSeamEndpoint struct {
	*httptest.Server
	mu       sync.Mutex
	reqs     []hippoSeamRequest
	replies  []hippoSeamReply
	fallback hippoSeamReply
}

// hippoSeamReply is one scripted HTTP answer: a status and a body (body "" ⇒ a
// 200 with an empty-content envelope, i.e. the empty-reply case).
type hippoSeamReply struct {
	status int
	body   string
}

// newHippoSeamEndpoint starts the fake endpoint with the given reply sequence.
func newHippoSeamEndpoint(t *testing.T, replies ...hippoSeamReply) *hippoSeamEndpoint {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("newHippoSeamEndpoint: at least one reply is required")
	}
	e := &hippoSeamEndpoint{replies: replies, fallback: replies[len(replies)-1]}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req hippoSeamRequest
		_ = json.NewDecoder(r.Body).Decode(&req) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		e.mu.Lock()
		idx := len(e.reqs)
		e.reqs = append(e.reqs, req)
		reply := e.fallback
		if idx < len(e.replies) {
			reply = e.replies[idx]
		}
		e.mu.Unlock()
		if reply.status != 0 && reply.status != http.StatusOK {
			w.WriteHeader(reply.status)
		}
		fmt.Fprint(w, reply.body)
	}))
	t.Cleanup(e.Close)
	return e
}

// requests returns a copy of the request shells the endpoint received.
func (e *hippoSeamEndpoint) requests() []hippoSeamRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]hippoSeamRequest(nil), e.reqs...)
}

// count returns how many calls the endpoint served.
func (e *hippoSeamEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.reqs)
}

// client returns the deepseek transport this test drives the real adapter with.
func (e *hippoSeamEndpoint) client() *deepseek.Client {
	return &deepseek.Client{BaseURL: e.URL, APIKey: "test-key", HTTPClient: e.Server.Client()}
}

// claudeJSONBody renders a non-streaming Claude response carrying text.
func claudeJSONBody(text, stopReason string) string {
	payload := map[string]any{
		"id":          "msg_seam",
		"stop_reason": stopReason,
		"content":     []map[string]any{{"type": "text", "text": text}},
		"usage":       map[string]int{"input_tokens": 12, "output_tokens": 4},
	}
	b, _ := json.Marshal(payload) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return string(b)
}

// okReply is a 200 answer whose content block carries text.
func okReply(text string) hippoSeamReply {
	return hippoSeamReply{status: http.StatusOK, body: claudeJSONBody(text, "end_turn")}
}

// noJSONReply is a 200 answer whose content block carries no JSON object.
func noJSONReply() hippoSeamReply { return okReply(hippoSeamProseNoJSON) }

// driveOneRecall runs the real recall function once against the fake endpoint
// and returns the brief it produced.
func driveOneRecall(t *testing.T, e *hippoSeamEndpoint) (*HippocampusBrief, *sync.Map) {
	t.Helper()
	newTestAgentEnv(t)
	logged := &sync.Map{}
	logger := func(format string, args ...any) {
		logged.Store(fmt.Sprintf(format, args...), true)
	}
	fn := newHippocampusRecallFunc(e.client(), "seam-model", func(format string, args ...any) {
		logger(format, args...)
	})
	brief, _ := fn(context.Background(), HippocampusRecallRequest{
		TaskContext: "how do I isolate a worktree",
		TaskID:      "t-seam",
		SessionID:   "s-seam",
		Round:       1,
	}) //stylegate:ignore invokes the fixture hook the test installed; the recorded request is the assertion
	return brief, logged
}

// TestHippocampusSeam_ThinkingIsDisabledOnEveryCall is behaviour ①: the organ
// requests must carry thinking:disabled (and no reasoning_effort) on the first
// attempt — a flash endpoint that thinks returns empty content.
func TestHippocampusSeam_ThinkingIsDisabledOnEveryCall(t *testing.T) {
	e := newHippoSeamEndpoint(t, okReply(hippoSeamQueriesReply), okReply(hippoSeamNoneReply))
	if _, _ = driveOneRecall(t, e); e.count() < 2 {
		t.Fatalf("cycle made %d calls, want the divergent + decision steps", e.count())
	}
	for i, req := range e.requests() {
		if req.Thinking == nil {
			t.Fatalf("call %d: no thinking field sent — the flash tier would think and return empty content", i)
		}
		if req.Thinking.Type != "disabled" {
			t.Fatalf("call %d: thinking type = %q, want disabled", i, req.Thinking.Type)
		}
		if req.ReasoningEffort != "" {
			t.Fatalf("call %d: reasoning_effort %q sent while thinking is disabled (the endpoints reject that pairing)",
				i, req.ReasoningEffort)
		}
	}
}

// TestHippocampusSeam_AlwaysThinking400FlipsToEffort is behaviour ②: the GLM
// always-thinking endpoint that rejects thinking:disabled for good drives the
// adapter into reasoning_effort mode, and the cycle still completes.
//
// The trigger chain is the real one, measured here rather than assumed: the
// TRANSPORT (internal/deepseek) already absorbs ONE deterministic 4xx by
// re-sending the request's minimal core, so the organ's adapter only sees the
// rejection when that degraded send is ALSO refused — ClaudeChat then returns an
// error whose message embeds the provider body, isHippoAlwaysThinkingError
// matches the marker in it, and the adapter flips to effort mode with the
// effort-mode token budget. A single-400 fixture would therefore never reach the
// fallback (it is what the first reply pair below fixes).
func TestHippocampusSeam_AlwaysThinking400FlipsToEffort(t *testing.T) {
	reject := hippoSeamReply{status: http.StatusBadRequest, body: hippoSeamAlwaysThinkingBody}
	e := newHippoSeamEndpoint(t,
		reject, reject, // original send, then the transport's minimal-core re-send
		okReply(hippoSeamQueriesReply),
		okReply(hippoSeamNoneReply),
	)
	brief, _ := driveOneRecall(t, e)
	if brief == nil {
		t.Fatal("no brief after the effort-mode fallback")
	}
	reqs := e.requests()
	if reqs[0].Thinking == nil || reqs[0].Thinking.Type != "disabled" {
		t.Fatalf("first attempt should still try thinking:disabled, got %+v", reqs[0].Thinking)
	}
	flipped := false
	for _, req := range reqs[1:] {
		if req.ReasoningEffort != hippoReasoningEffortLow {
			continue
		}
		if req.Thinking != nil {
			t.Fatalf("effort-mode request still carries thinking %+v", req.Thinking)
		}
		if req.MaxTokens != hippoClientMaxTokensEffort {
			t.Fatalf("effort-mode max_tokens = %d, want the effort budget %d",
				req.MaxTokens, hippoClientMaxTokensEffort)
		}
		flipped = true
	}
	if !flipped {
		t.Fatalf("no request carried reasoning_effort=%q: %+v", hippoReasoningEffortLow, reqs[1:])
	}
}

// TestHippocampusSeam_JSONLessReplyIsRetried is behaviour ③a: a reply with no
// JSON object must be re-asked (bounded), not handed to the parser to fail —
// and the ONE extra attempt is what the organ's ClientAdapter promises.
func TestHippocampusSeam_JSONLessReplyIsRetried(t *testing.T) {
	e := newHippoSeamEndpoint(t,
		okReply(hippoSeamQueriesReply), // divergent step: fine
		noJSONReply(),                  // decision step, attempt 1: unusable shape
		okReply(hippoSeamNoneReply),    // decision step, retry: usable
	)
	brief, _ := driveOneRecall(t, e)
	if brief == nil {
		t.Fatal("no brief produced")
	}
	// 3 transport calls: the divergent step plus the decision step's two
	// attempts. A fourth would mean the retry was not bounded.
	if got := e.count(); got != 3 {
		t.Fatalf("transport calls = %d, want 3 (divergent + 1 retry)", got)
	}
}

// TestHippocampusSeam_EmptyContentIsRecordedNotSilent is behaviour ③b (Rule
// 15): an empty reply ends the cycle with a recorded failure — the host logs the
// degraded cycle and archives a bodyless brief, so nothing is silently
// swallowed and nothing empty is delivered to the model.
func TestHippocampusSeam_EmptyContentIsRecordedNotSilent(t *testing.T) {
	e := newHippoSeamEndpoint(t, hippoSeamReply{status: http.StatusOK, body: claudeJSONBody("", "end_turn")})
	brief, logged := driveOneRecall(t, e)
	if brief == nil {
		t.Fatal("a degraded cycle must still produce a brief (the host archives its marker)")
	}
	if brief.Summary != "" {
		t.Fatalf("an empty reply produced a body: %q", brief.Summary)
	}
	if brief.Confidence >= hippocampusAppendMinConfidence {
		t.Fatalf("empty cycle confidence = %v, want below the %v append floor",
			brief.Confidence, hippocampusAppendMinConfidence)
	}
	found := false
	logged.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok && strings.Contains(s, "hippocampus cycle degraded") {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("the degraded cycle was not recorded in the host log (Rule 15)")
	}
}

// TestHippocampusRecallFunc_DelegatesToTheOrganArithmetic pins the ONE
// derivation at the live seam: the numbers the agent publishes about a recall
// are the organ's, and no host-side arithmetic is left to drift from them.
func TestHippocampusRecallFunc_DelegatesToTheOrganArithmetic(t *testing.T) {
	res := &hippocampus.Result{
		Query:      "seam probe",
		StopReason: hippocampus.StopNoneEmitted,
		Hits: []tools.MemoryHit{{
			ID: 871, Tier: tools.TierHot, Confidence: 0.8, Tokens: 2, Topic: "worktree 隔离",
		}},
	}
	got := hippoBriefFromResult(res)
	if want := hippocampus.ClampedBriefConfidence(res); got.Confidence != want {
		t.Fatalf("confidence = %v, want the organ's %v", got.Confidence, want)
	}
	if want := hippocampus.ClampedBriefMemoryWeight(res); got.MemoryWeight != want {
		t.Fatalf("memory weight = %v, want the organ's %v", got.MemoryWeight, want)
	}
	if got.Memories != 1 || got.Summary == "" || len(got.Keys) == 0 {
		t.Fatalf("brief incomplete: %+v", got)
	}
}

// TestEmptyRecallStaysUnderTheAppendFloor pins the invariant the host's
// scheduler depends on, at the seam where both constants are visible: the
// organ's empty-recall reading must stay STRICTLY below the host's append floor.
//
// It matters because unifying the derivation moved that reading: the host used
// to publish 0 for a hitless recall, and now publishes the organ's
// briefConfidenceNoHits (0.1). Both are under 0.2, so no delivery level changes
// — but the two numbers MUST NOT cross, or every hitless cycle would start
// escalating to Lv-1. If a future edit raises either constant, this fails.
func TestEmptyRecallStaysUnderTheAppendFloor(t *testing.T) {
	empty := hippocampus.ClampedBriefConfidence(nil)
	if empty >= hippocampusAppendMinConfidence {
		t.Fatalf("empty-recall confidence %v is not under the append floor %v — hitless cycles would be delivered",
			empty, hippocampusAppendMinConfidence)
	}
	if got := hippoBriefFromResult(&hippocampus.Result{}); got.Confidence != empty {
		t.Fatalf("the brief publishes %v for a hitless recall, want the organ's %v", got.Confidence, empty)
	}
	// The unified number must survive the host's own adjudication as Lv-0.
	if got := newHippocampusHost(testHippoCfg(hippocampusLv2MetaEvent), nil).adjudicate(
		&hippocampusBrief{Confidence: empty, Summary: "hitless"}, 1); got != hippocampusLv0Archive {
		t.Fatalf("a hitless brief landed at %v, want Lv-0 archive", got)
	}
}

// TestHostSideArithmeticRetiredGate is the Rule-13 gate for this resolution: the
// host must never grow a SECOND tier table, clamp or hit-confidence reader — the
// duplication this round removed. The scan covers the package's production
// sources (this test file is exempt: it must be free to NAME the retired
// helpers to assert their absence).
func TestHostSideArithmeticRetiredGate(t *testing.T) {
	retired := []string{
		"hippoTierWeightHot", "hippoTierWeightWarm", "hippoTierWeightCold",
		"hippoTierWeight", "hippoHitConfidence", "clampHippoUnit",
	}
	for name, src := range prodGoSources(t, ".") {
		for _, token := range retired {
			if strings.Contains(string(src), token) {
				t.Errorf("%s references %s — the brief's arithmetic lives in internal/hippocampus (one derivation)", name, token)
			}
		}
	}
}
