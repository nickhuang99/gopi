package agent

// Live organ binding — regression tests (epic: hippocampus host binding).
//
// These tests own the LIVE path: the function production actually calls to
// build the organ behind the host's recall seam. They exist because the live
// binder used to construct the engine with the RAW hippocampus.New, while
// internal/hippocampus/host.go declared the host binding (NewHostEngine +
// ClientAdapter + the two floors) that nothing called — so production ran the
// BATCH defaults (10s per call, no reply-shape retry) and the floors were dead
// code. Every test here is written against the live binder, never against
// NewHostEngine directly (host_test.go owns that side).
//
// Fails before the fix:
//   - TestHippocampus_LiveBinderRetriesJSONLessReply — the raw engine hands the
//     JSON-less reply straight to the parser, so the second attempt never
//     happens (1 HTTP request instead of 2).
//   - TestHippocampus_LivePathConstructsThroughHostBinding — the raw binder's
//     file carries the literal this gate forbids.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/hippocampus"
)

// liveBindClientTimeoutEnv is the organ package's per-call knob
// (hippocampus.envClientTimeout — unexported there, so the value is stated
// once here and used once, the cov_hippocampus_test.go precedent).
const liveBindClientTimeoutEnv = "GOPI_HIPPOCAMPUS_CLIENT_TIMEOUT_SEC"

// liveBindTransport builds the production transport (hipoClaudeClient's real
// wire path) against a stub endpoint — no network, no real key.
func liveBindTransport(t *testing.T, url string) *deepseek.Client {
	t.Helper()
	return &deepseek.Client{BaseURL: url, APIKey: "live-bind-test-key", HTTPClient: &http.Client{}}
}

// ---------------------------------------------------------------------------
// The floors are the LIVE engine's config, not a declaration next door
// ---------------------------------------------------------------------------

// TestHippocampus_LiveBinderMeetsHostFloors: the engine the live binder builds
// runs the host floors declared in internal/hippocampus/host.go — a live host
// LLM call must survive a slow flash endpoint, and the whole cycle must have
// room for several such calls.
func TestHippocampus_LiveBinderMeetsHostFloors(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(liveBindClientTimeoutEnv, "1") // a tiny configured value must NOT win

	engine, ok := newLiveHippocampusEngine(liveBindTransport(t, "http://127.0.0.1:1"), "test-model")
	if !ok || engine == nil {
		t.Fatal("the live binder reported no engine for a non-nil transport")
	}
	cfg := engine.Config()
	if cfg.ClientTimeout < hippocampus.HostEngineClientTimeout {
		t.Errorf("live ClientTimeout = %v, want at least the host floor %v — the live path "+
			"is running the batch default (it must build through hippocampus.NewHostEngine)",
			cfg.ClientTimeout, hippocampus.HostEngineClientTimeout)
	}
	if cfg.Timeout < hippocampus.HostEngineCycleTimeout {
		t.Errorf("live cycle Timeout = %v, want at least the host floor %v",
			cfg.Timeout, hippocampus.HostEngineCycleTimeout)
	}
}

// TestHippocampus_LiveBinderKeepsDeliberateLargerKnobs: the floors RAISE, they
// never lower an operator's deliberate setting (NewHostEngine's own contract —
// asserted here through the live binder, not through the organ's unit test).
func TestHippocampus_LiveBinderKeepsDeliberateLargerKnobs(t *testing.T) {
	newTestAgentEnv(t)
	want := hippocampus.HostEngineClientTimeout + 60*1e9 // +60s, in ns (time.Duration)
	t.Setenv(liveBindClientTimeoutEnv, fmt.Sprintf("%d", int(want/1e9)))

	engine, ok := newLiveHippocampusEngine(liveBindTransport(t, "http://127.0.0.1:1"), "test-model")
	if !ok || engine == nil {
		t.Fatal("the live binder reported no engine for a non-nil transport")
	}
	if got := engine.Config().ClientTimeout; got != want {
		t.Errorf("live ClientTimeout = %v, want the configured %v (floors must never lower it)", got, want)
	}
}

// ---------------------------------------------------------------------------
// The reply-shape retry reaches the live transport
// ---------------------------------------------------------------------------

// TestHippocampus_LiveBinderRetriesJSONLessReply: a real cycle over the LIVE
// binder where the model first answers with prose and NO JSON object. The bound
// ClientAdapter retries once, so the stub sees a SECOND request and the cycle
// completes instead of dying on "no JSON object in reply" — the exact
// production failure the raw New left unfixed.
func TestHippocampus_LiveBinderRetriesJSONLessReply(t *testing.T) {
	newTestAgentEnv(t)

	var requests int32
	proseReply := `Sure! Let me think about what queries would help here. ` +
		`I would look for lessons about the memory organ and its timeouts.`
	queriesReply := `{"queries":["live bind retry contract"]}`
	decideReply := `{"verb":"none","reason":"nothing new to store"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Errorf("request %d is not a Claude Messages call (missing %s header)", n, "anthropic-version")
		}
		text := decideReply
		switch n {
		case 1:
			text = proseReply // shape the adapter must reject-and-retry
		case 2:
			text = queriesReply
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"m%d","type":"message","role":"assistant",`+
			`"content":[{"type":"text","text":%q}],"stop_reason":"end_turn",`+
			`"usage":{"input_tokens":10,"output_tokens":5}}`, n, text)
	}))
	defer srv.Close()

	recall := newHippocampusRecallFunc(liveBindTransport(t, srv.URL), "test-model", nil)
	if recall == nil {
		t.Fatal("the live binder returned no recall function for a bound transport")
	}
	brief, err := recall(context.Background(), HippocampusRecallRequest{
		TaskContext: "why does the memory organ retry a JSON-less reply?",
		TaskID:      "live-bind-test",
	})
	if err != nil {
		t.Fatalf("live recall returned a caller-side error: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got < 2 {
		t.Fatalf("the JSON-less reply earned %d HTTP request(s) — the reply-shape retry is "+
			"NOT on the live path (want at least 2: the rejected prose + the retry)", got)
	}
	if brief == nil {
		t.Fatal("live recall returned no brief")
	}
}

// ---------------------------------------------------------------------------
// Rule 15: no transport ⇒ an explicitly unbound organ, never a silent stub
// ---------------------------------------------------------------------------

// TestHippocampus_LiveBinderWithoutTransportStaysUnbound: NewHostEngine's
// ok=false contract is honoured on the live path — a transport-less agent ends
// up with an UNBOUND host (no fake organ that answers nothing), and the refusal
// is logged, not swallowed.
func TestHippocampus_LiveBinderWithoutTransportStaysUnbound(t *testing.T) {
	a := newHippoTestAgent(t)
	defer a.SessionLog.Close()
	a.Client = nil
	a.Hippocampus = newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)

	var logged []string
	bindHippocampusRecall(a, func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})

	if fn := a.Hippocampus.recallFn(); fn != nil {
		t.Fatal("a transport-less agent got a bound organ — it would archive a failure marker every run")
	}
	if len(logged) == 0 {
		t.Fatal("the unbound outcome was not recorded (Rule 15: no silent failure)")
	}
	if _, ok := newLiveHippocampusEngine(nil, "test-model"); ok {
		t.Error("newLiveHippocampusEngine(nil) reported a bound engine")
	}
}

// ---------------------------------------------------------------------------
// Rule 13: the live path may construct the organ in exactly ONE way
// ---------------------------------------------------------------------------

// TestHippocampus_LivePathConstructsThroughHostBinding is the anti-regression
// gate for the defect this task fixed: a production file in this package must
// never build the organ with the raw hippocampus.New(...) — that bypasses the
// host floors and the reply-shape retry and silently reintroduces the batch
// defaults. The only legal live construction is hippocampus.NewHostEngine
// (wrapped in newLiveHippocampusEngine).
func TestHippocampus_LivePathConstructsThroughHostBinding(t *testing.T) {
	const rawConstructor = "hippocampus.New(" // the bypassing call, not NewHostEngine
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", name, err)
		}
		scanned++
		if strings.Contains(string(b), rawConstructor) {
			t.Errorf("%s builds the organ with %q — the live path must go through "+
				"newLiveHippocampusEngine (hippocampus.NewHostEngine), or the host floors "+
				"and the reply-shape retry silently stop applying", name, rawConstructor)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no production file — the gate is not actually covering the package")
	}
}
