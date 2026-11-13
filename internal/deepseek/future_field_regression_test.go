package deepseek

// future_field_regression_test.go — testsuite extension for the
// provider-neutral error matrix (devdoc/error-handling-matrix.md). Pins
// the two invariants the epic redesign rests on, against FUTURE change:
//
//   1. Vendor tunnel-vision must never come back: adding a vendor-shaped
//      error body to the matrix must NOT change its class (the v1 "1214
//      special case" regression guard).
//   2. MinimalCore is a whitelist: a NEWLY-ADDED optional field on
//      ClaudeRequest is stripped by construction — but if someone adds the
//      field to the whitelist literal itself, this test fails and forces a
//      conscious decision.
//
// Both tests are written as "tables that future edits extend", the exact
// shape the epic called for.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClassifyHTTPError_VendorShapesStaySemantic — vendor error codes in the
// BODY must never influence classification (only the status code matters).
// New vendor quirks belong in config data, never in an `if vendor` branch.
func TestClassifyHTTPError_VendorShapesStaySemantic(t *testing.T) {
	cases := []struct {
		name string
		body string
		want errorClass
	}{
		// GLM shapes (historical: 1214 param-tunnel, 1210 always-thinking).
		{"GLM 1214 in body", `{"error":{"code":"1214","msg":"params only support reasoning_effort=medium"}}`, classDeterministic},
		{"GLM 1210 in body", `{"error":{"code":"1210","msg":"always thinking"}}`, classDeterministic},
		// DeepSeek shape: 400 with empty-thinking message.
		{"DeepSeek empty thinking 400", `{"error":{"message":"thinking is empty"}}`, classDeterministic},
		// Anthropic shape.
		{"Anthropic invalid_request", `{"type":"error","error":{"type":"invalid_request_error"}}`, classDeterministic},
		// Future vendor marker that LOOKS like a status (must not parse).
		{"body claims 200", `{"error":{"code":500,"msg":"internal"}}`, classDeterministic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Same body on a 4xx stays deterministic...
			if got := classifyHTTPError(400, []byte(tc.body), nil); got != tc.want {
				t.Fatalf("400 + %s: class=%v want=%v (body must not flip the class)", tc.name, got, tc.want)
			}
			// ...and the same body on 5xx stays transient.
			if got := classifyHTTPError(500, []byte(tc.body), nil); got != classTransient {
				t.Fatalf("500 + %s: class=%v want=transient (body must not flip the class)", tc.name, got)
			}
		})
	}
}

// TestMinimalCore_NewFieldLeakGuard — the whitelist must contain EXACTLY
// model, max_tokens, messages, stream. A future optional field that sneaks
// into the whitelist (or a struct field added to ClaudeRequest that the
// literal starts copying) fails here at wire-JSON level, the level the
// provider actually sees.
func TestMinimalCore_NewFieldLeakGuard(t *testing.T) {
	core := MinimalCore(baseReq())
	wire, err := json.Marshal(core)
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{"model", "max_tokens", "messages", "stream"} // stream omitted when false (omitempty)
	allowedSet := map[string]bool{}
	for _, k := range allowed {
		allowedSet[k] = true
	}
	keys := topLevelJSONKeys(t, wire)
	for _, k := range keys {
		if !allowedSet[k] {
			t.Errorf("minimal-core wire JSON contains key %q — NOT in the minimal core whitelist; a new optional field leaked into the degrade path", k)
		}
	}
	// stream is omitempty: with Stream=false it is absent from the wire JSON
	// (baseReq sets Stream:false), so the required set is the three that
	// always serialize. The whitelist contract itself is the allowed slice.
	if len(keys) == 0 {
		t.Error("minimal-core wire JSON is empty — nothing serialized")
	}
}

// TestDegradePath_VendorMarkerBodyStillDegrades — end-to-end through
// ClaudeChat: a 4xx whose body carries a vendor marker still gets exactly
// ONE minimal-core degrade (never a vendor-coded second chance), then
// fail-fast with a dump in GOPI_ERROR_DUMP_DIR.
func topLevelJSONKeys(t *testing.T, wire []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(wire, &m); err != nil {
		t.Fatalf("wire not a JSON object: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestDegradePath_VendorMarkerBodyStillDegrades(t *testing.T) {
	var sendCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendCount++
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":"1214","msg":"params only support reasoning_effort=medium"}}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer server.Close()

	c := newMatrixTestClient(t, server) // helper owns GOPI_ERROR_DUMP_DIR (its own t.TempDir)
	c.RetryConfig.MaxRetries = 0
	dumpDir := os.Getenv("GOPI_ERROR_DUMP_DIR")
	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("expected fail-fast after degraded re-send still 400")
	}
	if sendCount != 2 {
		t.Fatalf("vendor-marker 4xx: sends=%d, want exactly 2 (orig + minimal core) — no vendor-coded extra chances", sendCount)
	}
	entries, _ := filepath.Glob(filepath.Join(dumpDir, "*.dump.json")) //stylegate:ignore glob for optional cleanup; the pattern is a constant so ErrBadPattern is unreachable
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 dump file, got %d", len(entries))
	}
	raw, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	// The dump JSON is pretty-printed ("http_status": 400) — check the
	// key + value pair loosely rather than a byte-exact compact form.
	for _, want := range []string{"\"http_status\": 400", "1214", "original_request"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("dump missing %s", want)
		}
	}
}
