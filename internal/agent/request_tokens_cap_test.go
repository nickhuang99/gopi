package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// request_tokens_cap_test.go — the send-time token ceiling's two behaviours and
// the one invariant that ties the gate to the status row.
//
// Every test here drives the REAL send path (chatOnceGated → chatOnce →
// the HTTP client) against a mock endpoint, and counts the requests the
// endpoint actually received. "The request was not sent" is therefore a
// measured fact (the server's own counter), never an inference from an error
// value.

// tokenCapServer is a mock endpoint that answers with a minimal text stream and
// counts how many requests reached it. The count IS the assertion: a gate that
// refuses AFTER sending would pass a test that only inspected the error.
type tokenCapServer struct {
	server *httptest.Server
	calls  *int32
	bodies chan string
}

func newTokenCapServer(t *testing.T) *tokenCapServer {
	t.Helper()
	var n int32
	bodies := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of the mock request body; a short read is acceptable here
		select {
		case bodies <- string(body):
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"answered: the request was accepted by the mock endpoint."}, "end_turn")
	}))
	t.Cleanup(srv.Close)
	return &tokenCapServer{server: srv, calls: &n, bodies: bodies}
}

// tokenCapTestAgent is a minimal Agent wired to the mock endpoint. Ctx is a
// zero-value Context with a ContextWindow set so the shrink path is the REAL
// one (compaction/trim), not a no-op.
func tokenCapTestAgent(t *testing.T, url string, window int) *Agent {
	t.Helper()
	a := &Agent{
		Client: &deepseek.Client{
			BaseURL:    url,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 1,
		MaxTokens:     deepseek.DefaultClaudeMaxTokens,
	}
	a.ContextWindow = window
	return a
}

// fillerText is a deterministic body of ASCII prose of roughly `tokens` BPE
// tokens, CALIBRATED against the real tokenizer rather than guessed from a
// characters-per-token ratio: the first version of this helper assumed a ratio,
// and its "~200K" fixture counted 398,016 tokens — a fixture 2x the size the
// test named, which is exactly the class of error a hard-coded ratio produces.
// The unit is chosen from a measurement, so the fixture's size claim is true.
func fillerText(tokens int) string {
	const word = "tokenized "
	unit := strings.Repeat(word, 100)
	n, err := countRequestTokens(&deepseek.ClaudeRequest{
		Messages: []deepseek.ClaudeMessage{{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: unit}}}},
	})
	if err != nil || n <= 0 {
		// The calibration is a MEASUREMENT; if it cannot be taken the fixture
		// must fail loudly rather than silently produce a differently-sized
		// body (Rule 15).
		panic("fillerText: cannot calibrate the fixture against the tokenizer")
	}
	reps := tokens / n
	if reps < 1 {
		reps = 1
	}
	return strings.Repeat(unit, reps)
}

// TestRequestTokenCapRefusesAnOverCapRequest is the minimal repro of the hole
// this gate closes: a request far over the ceiling reaches the wire today. With
// the gate in place the endpoint's own counter must stay at ZERO and the error
// must carry both numbers.
func TestRequestTokenCapRefusesAnOverCapRequest(t *testing.T) {
	t.Setenv(RequestTokenCapEnv, "100")
	srv := newTokenCapServer(t)
	a := tokenCapTestAgent(t, srv.server.URL, RequestTokensCap)

	// A system block that alone is over the 100-token ceiling: no shrink of the
	// MESSAGE list can bring this request under it, which is what makes the
	// refusal (rather than a retry loop) the expected outcome.
	system := fillerText(500)
	build := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), system, "", nil, 1024, &deepseek.Thinking{Type: "disabled"})
	}

	_, err := a.chatOnceGated(context.Background(), build, 0)
	if err == nil {
		t.Fatal("a request over the cap must not be sent, and must not succeed")
	}
	var over *RequestTokensOverCapError
	if !errors.As(err, &over) {
		t.Fatalf("error must be *RequestTokensOverCapError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrRequestTokensOverCap) {
		t.Fatalf("error must match ErrRequestTokensOverCap with errors.Is: %v", err)
	}
	if over.Counted <= over.Cap {
		t.Fatalf("counted=%d must exceed cap=%d for a refusal", over.Counted, over.Cap)
	}
	if over.Cap != 100 {
		t.Fatalf("cap=%d, want the %s override (100)", over.Cap, RequestTokenCapEnv)
	}
	msg := err.Error()
	for _, want := range []string{fmt.Sprintf("counted=%d", over.Counted), fmt.Sprintf("cap=%d", over.Cap)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal must name %q (Rule 15: a refusal carries its numbers):\n%s", want, msg)
		}
	}
	if got := atomic.LoadInt32(srv.calls); got != 0 {
		t.Fatalf("the endpoint received %d request(s); an over-cap request must not reach the wire", got)
	}
	if last, ok := a.Ctx.LastRequestTokenCount(); !ok || !last.Over {
		t.Fatalf("the gate's last reading must be recorded and over the cap: %+v ok=%v", last, ok)
	}
}

// TestRequestTokenCapSendsARequestThatFits is the other half — and the half a
// gate that refuses everything would also pass if it were not written: a
// request under the ceiling MUST reach the endpoint.
func TestRequestTokenCapSendsARequestThatFits(t *testing.T) {
	srv := newTokenCapServer(t)
	a := tokenCapTestAgent(t, srv.server.URL, RequestTokensCap)
	a.Ctx.AddUserMessage("what is the token count of this trivial request?")

	build := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), "you are a helpful assistant", "", nil, 1024, &deepseek.Thinking{Type: "disabled"})
	}
	res, err := a.chatOnceGated(context.Background(), build, 0)
	if err != nil {
		t.Fatalf("an under-cap request must be sent, got error: %v", err)
	}
	if res == nil || !strings.Contains(res.content, "mock endpoint") {
		t.Fatalf("the round did not complete through the mock endpoint: %+v", res)
	}
	if got := atomic.LoadInt32(srv.calls); got != 1 {
		t.Fatalf("the endpoint received %d request(s), want exactly 1", got)
	}
	last, ok := a.Ctx.LastRequestTokenCount()
	if !ok || last.Over {
		t.Fatalf("the gate must record a fitting reading: %+v ok=%v", last, ok)
	}
	if last.Cap != RequestTokensCap {
		t.Fatalf("cap=%d, want the default %d when %s is unset", last.Cap, RequestTokensCap, RequestTokenCapEnv)
	}
}

// TestRequestTokenCapBoundaryIsExact pins the comparison itself: the cap is a
// CEILING, so exactly-at-cap is allowed and one token over is refused. An
// off-by-one here would refuse legitimate requests at the model's own window.
func TestRequestTokenCapBoundaryIsExact(t *testing.T) {
	if requestTokenExceeded(RequestTokensCap, RequestTokensCap) {
		t.Fatal("a request of exactly the cap must be allowed (the cap is a ceiling, not a fence)")
	}
	if !requestTokenExceeded(RequestTokensCap+1, RequestTokensCap) {
		t.Fatal("one token over the cap must be refused")
	}
	if requestTokenExceeded(0, RequestTokensCap) {
		t.Fatal("an empty request must not be refused")
	}
}

// TestRequestTokenCapIgnoresAnUnreadableOverride: an env value that is not a
// positive integer must leave the DEFAULT in force — an unreadable ceiling must
// never silently mean "no ceiling" (Rule 15).
func TestRequestTokenCapIgnoresAnUnreadableOverride(t *testing.T) {
	for _, v := range []string{"", "abc", "0", "-5", "1e5"} {
		t.Setenv(RequestTokenCapEnv, v)
		if got := requestTokensCap(); got != RequestTokensCap {
			t.Fatalf("%s=%q gave cap=%d, want the default %d", RequestTokenCapEnv, v, got, RequestTokensCap)
		}
	}
	t.Setenv(RequestTokenCapEnv, "1234")
	if got := requestTokensCap(); got != 1234 {
		t.Fatalf("a positive override must win: got %d, want 1234", got)
	}
}

// TestRequestTokenCountPricesTheWholeWire is the coverage assertion the epic
// names: system blocks, message content, a tool_use's arguments, a
// tool_result's payload and the tools array must ALL move the count. Each arm
// adds ONE part to the same base and requires the number to grow — a part the
// counter drops is invisible until a request blows the window on that part
// alone.
func TestRequestTokenCountPricesTheWholeWire(t *testing.T) {
	base := &deepseek.ClaudeRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 1024,
		System:    []deepseek.ClaudeSystemBlock{{Type: "text", Text: "system base"}},
		Messages: []deepseek.ClaudeMessage{{
			Role:    "user",
			Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "hello"}},
		}},
	}
	baseCount, err := countRequestTokens(base)
	if err != nil {
		t.Fatalf("countRequestTokens: %v", err)
	}
	if baseCount <= 0 {
		t.Fatalf("a non-empty request counted %d tokens", baseCount)
	}

	big := fillerText(200)

	arms := []struct {
		name   string
		mutate func(r *deepseek.ClaudeRequest)
	}{
		{"project context (second system block)", func(r *deepseek.ClaudeRequest) {
			r.System = append(r.System, deepseek.ClaudeSystemBlock{Type: "text", Text: big})
		}},
		{"message text", func(r *deepseek.ClaudeRequest) {
			r.Messages[0].Content = append(r.Messages[0].Content, deepseek.ClaudeContentBlock{Type: "text", Text: big})
		}},
		{"tool_use arguments", func(r *deepseek.ClaudeRequest) {
			r.Messages = append(r.Messages, deepseek.ClaudeMessage{
				Role: "assistant",
				Content: []deepseek.ClaudeContentBlock{{
					Type: "tool_use", ID: "toolu_1", Name: "read",
					Input: map[string]interface{}{"path": "/tmp/x", "note": big},
				}},
			})
		}},
		{"tool_result payload", func(r *deepseek.ClaudeRequest) {
			r.Messages = append(r.Messages, deepseek.ClaudeMessage{
				Role: "user",
				Content: []deepseek.ClaudeContentBlock{{
					Type: "tool_result", ToolUseID: "toolu_1", Content: big,
				}},
			})
		}},
		{"tools array", func(r *deepseek.ClaudeRequest) {
			r.Tools = []deepseek.ClaudeToolDef{{
				Name: "read", Description: big,
				InputSchema: map[string]interface{}{"type": "object"},
			}}
		}},
	}
	for _, arm := range arms {
		one := *base
		one.Messages = append([]deepseek.ClaudeMessage(nil), base.Messages...)
		one.System = append([]deepseek.ClaudeSystemBlock(nil), base.System...)
		arm.mutate(&one)
		got, err := countRequestTokens(&one)
		if err != nil {
			t.Fatalf("%s: %v", arm.name, err)
		}
		if got <= baseCount {
			t.Fatalf("%s: count %d did not grow past the base %d — this part is NOT priced", arm.name, got, baseCount)
		}
	}
}

// TestRequestTokenCountIgnoresTheCacheMarker: cache_control is a transport hint
// this client attaches, not content the template renders. Counting it would
// price bytes the model never reads.
func TestRequestTokenCountIgnoresTheCacheMarker(t *testing.T) {
	base := &deepseek.ClaudeRequest{
		Model: "deepseek-v4-pro",
		Tools: []deepseek.ClaudeToolDef{{Name: "read", Description: "reads a file", InputSchema: map[string]interface{}{"type": "object"}}},
	}
	withMarker := *base
	withMarker.Tools = []deepseek.ClaudeToolDef{base.Tools[0]}
	withMarker.Tools[0].CacheControl = &deepseek.ClaudeCacheControl{Type: "ephemeral"}

	plain, err := countRequestTokens(base)
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	marked, err := countRequestTokens(&withMarker)
	if err != nil {
		t.Fatalf("marked: %v", err)
	}
	if plain != marked {
		t.Fatalf("the cache marker moved the count (%d → %d); it must not be priced", plain, marked)
	}
}

// TestRequestTokenCountAgreesWithTheStatusRow measures the gap between the two
// readings the operator can see: the send-time gate (template-exact) and the
// status row (CalibratedTokens). They are computed on different bases by
// design — the row sums per-message BPE costs, the gate renders the request
// through the chat template — so they are not IDENTICAL, and the epic's
// "the row must be shown to be the exact count" is satisfied by the row
// already reading the BPE tokenizer (context_tokens.go). What this test pins is
// that the two cannot drift APART: on the same request they must agree within
// requestTokenRowAgreementPct, or the row has stopped describing what is sent.
func TestRequestTokenCountAgreesWithTheStatusRow(t *testing.T) {
	a := tokenCapTestAgent(t, "http://127.0.0.1:1", RequestTokensCap)
	a.Ctx.SystemPrompt = "you are gopi"
	a.Ctx.ProjectContext = "project rules: be exact"
	a.Ctx.SetTools([]deepseek.ClaudeToolDef{{Name: "read", Description: "reads a file", InputSchema: map[string]interface{}{"type": "object"}}})
	a.Ctx.AddUserMessage(fillerText(30000))
	a.Ctx.AddAssistantMessage("acknowledged, counting.", nil)
	a.Ctx.AddUserMessage(fillerText(30000))

	req := deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), a.Ctx.SystemPrompt, a.Ctx.ProjectContext,
		[]deepseek.ClaudeToolDef{{Name: "read", Description: "reads a file", InputSchema: map[string]interface{}{"type": "object"}}},
		1024, &deepseek.Thinking{Type: "disabled"})

	gated, err := countRequestTokens(req)
	if err != nil {
		t.Fatalf("countRequestTokens: %v", err)
	}
	row := a.Ctx.CalibratedTokens()
	if row <= 0 {
		t.Fatalf("the status row read %d tokens", row)
	}
	delta := gated - row
	if delta < 0 {
		delta = -delta
	}
	t.Logf("gate counted=%d row=%d delta=%d tokens (%.4f%%)", gated, row, delta, float64(delta)/float64(row)*100)
	// The bound is ABSOLUTE, because the gap is a fixed template overhead (the
	// markers and the tools preamble the row's per-message sum does not
	// render), not a proportional error — a percentage bound would pass on a
	// big request while hiding a basis that had stopped agreeing.
	if delta > requestTokenRowOverheadTokens {
		t.Fatalf("the gate (%d) and the status row (%d) differ by %d tokens, over the %d-token template-overhead bound — the row no longer describes what is sent",
			gated, row, delta, requestTokenRowOverheadTokens)
	}
}

// requestTokenRowOverheadTokens is how far the send-time count and the status
// row may diverge on the SAME request. It is not zero because the two bases
// genuinely differ: the gate renders the chat template (BOS, the effort
// header, per-turn markers, the tools preamble, block joins) while the row
// sums per-message BPE costs. The gap is therefore a FIXED overhead, not a
// proportional one — measured on this fixture: 220 tokens on a ~52K request
// (0.43%) and the same 220 on a ~3.2K one (6.8% there). An absolute bound is
// what measures agreement; a percentage on a small request would only measure
// the overhead.
const requestTokenRowOverheadTokens = 600

// TestRequestTokenCountTimesTheGate measures what the gate costs: one full BPE
// pass over a rendered request. It is the number the report quotes, produced by
// the test rather than typed into prose (Rule 38).
func TestRequestTokenCountTimesTheGate(t *testing.T) {
	if testing.Short() {
		t.Skip("the timing arm builds a ~200K-token request")
	}
	a := tokenCapTestAgent(t, "http://127.0.0.1:1", RequestTokensCap)
	a.Ctx.SystemPrompt = fillerText(20000)
	a.Ctx.ProjectContext = fillerText(20000)
	a.Ctx.AddUserMessage(fillerText(80000))
	a.Ctx.AddAssistantMessage("recorded.", nil)
	a.Ctx.AddUserMessage(fillerText(79000))

	req := deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), a.Ctx.SystemPrompt, a.Ctx.ProjectContext, nil, 1024, &deepseek.Thinking{Type: "disabled"})

	const rounds = 3
	var best time.Duration
	var counted int
	for i := 0; i < rounds; i++ {
		start := time.Now()
		n, err := countRequestTokens(req)
		d := time.Since(start)
		if err != nil {
			t.Fatalf("countRequestTokens: %v", err)
		}
		counted = n
		if i == 0 || d < best {
			best = d
		}
	}
	t.Logf("counted=%d cap=%d  best-of-%d BPE pass = %s", counted, RequestTokensCap, rounds, best)
	if counted < RequestTokensCap/2 {
		t.Fatalf("the timing fixture counted only %d tokens — it is not the ~200K shape this measurement claims to describe", counted)
	}
}

// TestRequestTokenGateSendsATwoHundredThousandTokenRequest is the "放行" half at
// the scale the epic names: a request just UNDER the real 200000 ceiling must go
// out. It is the arm that proves the gate is a ceiling and not a wall.
func TestRequestTokenGateSendsATwoHundredThousandTokenRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a ~200K-token request")
	}
	srv := newTokenCapServer(t)
	a := tokenCapTestAgent(t, srv.server.URL, RequestTokensCap)
	a.Ctx.SystemPrompt = fillerText(40000)
	a.Ctx.AddUserMessage(fillerText(150000))

	build := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), a.Ctx.SystemPrompt, "", nil, 1024, &deepseek.Thinking{Type: "disabled"})
	}
	pre, err := countRequestTokens(build())
	if err != nil {
		t.Fatalf("countRequestTokens: %v", err)
	}
	if pre >= RequestTokensCap {
		t.Fatalf("the fixture counted %d tokens, at or over the %d cap — it cannot demonstrate a send", pre, RequestTokensCap)
	}
	if _, err := a.chatOnceGated(context.Background(), build, 0); err != nil {
		t.Fatalf("an under-cap request must be sent, got: %v", err)
	}
	if got := atomic.LoadInt32(srv.calls); got != 1 {
		t.Fatalf("the endpoint received %d request(s), want 1", got)
	}
	last, ok := a.Ctx.LastRequestTokenCount()
	if !ok || last.Over || last.Counted != pre {
		t.Fatalf("gate reading %+v (ok=%v) must equal the pre-count %d and not be over the cap", last, ok, pre)
	}
	t.Logf("counted=%d cap=%d sent=yes", last.Counted, last.Cap)
}

// TestRequestTokenGateNeverSendsAfterAShrinkItCannotWin pins the retry chain's
// end state: when the shrink path runs but cannot bring the request under the
// ceiling, the send still does not happen and the error still carries numbers.
// Without this arm a regression that sends on the SECOND pass would pass every
// other test in this file.
func TestRequestTokenGateNeverSendsAfterAShrinkItCannotWin(t *testing.T) {
	t.Setenv(RequestTokenCapEnv, "2000")
	srv := newTokenCapServer(t)
	a := tokenCapTestAgent(t, srv.server.URL, RequestTokensCap)

	// A LONG system block (over the 2000-token override) beside a message list
	// the shrink path CAN reduce to its floor: the shrink runs, the request
	// stays over, and nothing may be sent.
	a.Ctx.SystemPrompt = fillerText(4000)
	a.Ctx.AddUserMessage("first message")
	a.Ctx.AddAssistantMessage("first answer", nil)
	a.Ctx.AddUserMessage("second message")

	build := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), a.Ctx.SystemPrompt, "", nil, 1024, &deepseek.Thinking{Type: "disabled"})
	}
	_, err := a.chatOnceGated(context.Background(), build, 0)
	var over *RequestTokensOverCapError
	if !errors.As(err, &over) {
		t.Fatalf("want *RequestTokensOverCapError after an unsuccessful shrink, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(srv.calls); got != 0 {
		t.Fatalf("the endpoint received %d request(s) after an unsuccessful shrink — nothing may be sent", got)
	}
	if over.Counted <= over.Cap {
		t.Fatalf("the reported reading must still be over: %+v", over)
	}
}

// TestRequestTokenCapStaysQuietWhenTheRequestFits pins the NOISE fix: the gate
// runs on every round of every chat, and a per-round line saying "this request
// fits" buried the rounds where something actually happened. A fitting reading
// must print NOTHING — and must still be RECORDED on the Context (Rule 15: the
// number is not lost, it is just not shouted), which the assertion below
// measures through the same public reader the status row uses.
func TestRequestTokenCapStaysQuietWhenTheRequestFits(t *testing.T) {
	srv := newTokenCapServer(t)
	a := tokenCapTestAgent(t, srv.server.URL, RequestTokensCap)
	a.Ctx.AddUserMessage("what is the token count of this trivial request?")

	// A real logger, capturing what the round prints: an Agent with a nil
	// Logger would pass this test without any code change at all.
	var buf bytes.Buffer
	a.Logger = log.New(&buf, "", 0)

	build := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), "you are a helpful assistant", "", nil, 1024, &deepseek.Thinking{Type: "disabled"})
	}
	if _, err := a.chatOnceGated(context.Background(), build, 0); err != nil {
		t.Fatalf("an under-cap request must be sent, got error: %v", err)
	}
	if got := atomic.LoadInt32(srv.calls); got != 1 {
		t.Fatalf("the endpoint received %d request(s), want exactly 1", got)
	}
	for _, quiet := range []string{"🔢", "request tokens:", "counted="} {
		if strings.Contains(buf.String(), quiet) {
			t.Fatalf("a fitting round must print nothing carrying %q — the gate's own reading is noise there:\n%s", quiet, buf.String())
		}
	}
	last, ok := a.Ctx.LastRequestTokenCount()
	if !ok {
		t.Fatal("the fitting reading must still be recorded on the Context even though it is not printed")
	}
	if last.Over || last.Counted <= 0 || last.Cap != RequestTokensCap {
		t.Fatalf("recorded reading %+v is not the fitting one the round measured", last)
	}
}

// TestRequestTokenOverCapStillPrintsItsRefusal is the other half of the same
// change: silencing the fitting line must not silence the OVER path. It drives
// a genuinely over-cap round and asserts the ⛔ line REACHES the log, so a fix
// that removed the Logger block outright would fail here.
func TestRequestTokenOverCapStillPrintsItsRefusal(t *testing.T) {
	t.Setenv(RequestTokenCapEnv, "100")
	srv := newTokenCapServer(t)
	a := tokenCapTestAgent(t, srv.server.URL, RequestTokensCap)
	a.Ctx.SystemPrompt = fillerText(500)
	var buf bytes.Buffer
	a.Logger = log.New(&buf, "", 0)

	build := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(a.Model, a.requestMessages(false), a.Ctx.SystemPrompt, "", nil, 1024, &deepseek.Thinking{Type: "disabled"})
	}
	_, err := a.chatOnceGated(context.Background(), build, 0)
	var over *RequestTokensOverCapError
	if !errors.As(err, &over) {
		t.Fatalf("want *RequestTokensOverCapError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(srv.calls); got != 0 {
		t.Fatalf("the endpoint received %d request(s); an over-cap request must not reach the wire", got)
	}
	if !strings.Contains(buf.String(), "⛔ request over the send-time cap") {
		t.Fatalf("the refusal must stay observable on the log (Rule 15):\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), fmt.Sprintf("counted=%d cap=%d", over.Counted, over.Cap)) {
		t.Fatalf("the ⛔ line must carry the numbers:\n%s", buf.String())
	}
}
