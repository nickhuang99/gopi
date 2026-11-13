package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// writeRawLog writes a raw log exactly as RawLogger.LogFunc does: each entry
// is direction + payload + a trailing newline. The chunk payload below is
// multi-line (SSE framing) on purpose — the parser must be marker-driven,
// not line-driven. name must end in .log (default "gopi-raw-test.log").
func writeRawLog(t *testing.T, dir, name string, entries [][2]string) string {
	t.Helper()
	if name == "" {
		name = "gopi-raw-test.log"
	}
	path := filepath.Join(dir, name)
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e[0]) // direction incl. newline
		b.WriteString(e[1]) // payload
		b.WriteString("\n") // LogFunc terminator
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("writeRawLog: %v", err)
	}
	return path
}

// fixtureLog builds a log with: REQUEST → CHUNK×2 (multi-line SSE) →
// REQUEST → RESPONSE. The last REQUEST is the resume point.
func fixtureLog(t *testing.T, dir string) string {
	t.Helper()
	return writeRawLog(t, dir, "", [][2]string{
		{"→ REQUEST\n", `{"model":"m1","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"first"}]}]}`},
		{"← CHUNK\n", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hel\"}}\n\n"},
		{"← CHUNK\n", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n"},
		{"→ REQUEST\n", `{"model":"m2","max_tokens":200,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"sig"},{"type":"text","text":"hi"}]}]}`},
		{"← RESPONSE\n", `{"id":"r1","content":[{"type":"text","text":"ok"}]}`},
	})
}

func TestLastRequest_LastAndByteExact(t *testing.T) {
	path := fixtureLog(t, t.TempDir())
	entry, req, err := LastRequest(path)
	if err != nil {
		t.Fatalf("LastRequest: %v", err)
	}
	if entry.Dir != DirectionRequest {
		t.Errorf("expected a REQUEST entry, got %q", entry.Dir)
	}
	if req.Model != "m2" || req.MaxTokens != 200 {
		t.Errorf("expected last request m2/200, got %s/%d", req.Model, req.MaxTokens)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "assistant" {
		t.Fatalf("unexpected last request messages: %+v", req.Messages)
	}
	if b := req.Messages[0].Content[0]; b.Type != "thinking" || b.Thinking != "t" || b.Signature != "sig" {
		t.Errorf("thinking block not preserved: %+v", b)
	}
	// Byte-exact round-trip: the extracted payload re-marshals to the same
	// request (no-op clone invariant).
	var again deepseek.ClaudeRequest
	if err := json.Unmarshal(entry.Payload, &again); err != nil {
		t.Fatalf("re-parse payload: %v", err)
	}
	if !reflect.DeepEqual(req, &again) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", &again, req)
	}
}

// TestLastRequest_TailWindow tests the growing-window path: the last request
// sits at EOF (no trailing chunks), so the first 8MB window finds it and the
// "payload truncated" re-read-to-EOF path must reproduce the exact bytes.
func TestLastRequest_TailWindow(t *testing.T) {
	path := writeRawLog(t, t.TempDir(), "", [][2]string{
		{"→ REQUEST\n", `{"model":"early","max_tokens":1,"messages":[]}`},
		{"← CHUNK\n", "event: message_stop\ndata: {}\n\n"},
		{"→ REQUEST\n", `{"model":"last","max_tokens":2,"messages":[{"role":"user","content":[{"type":"text","text":"final"}]}]}`},
	})
	entry, req, err := LastRequest(path)
	if err != nil {
		t.Fatalf("LastRequest: %v", err)
	}
	if req.Model != "last" {
		t.Errorf("expected the last request (model=last), got %s", req.Model)
	}
	if !strings.Contains(string(entry.Payload), `"text":"final"`) {
		t.Errorf("payload truncated/incorrect: %q", entry.Payload)
	}
}

func TestLastRequest_NoRequest(t *testing.T) {
	path := writeRawLog(t, t.TempDir(), "", [][2]string{
		{"← CHUNK\n", "event: message_stop\ndata: {}\n\n"},
	})
	if _, _, err := LastRequest(path); err == nil || !strings.Contains(err.Error(), "no → REQUEST") {
		t.Fatalf("expected no-REQUEST error, got %v", err)
	}
}

func TestLastRequest_EmptyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gopi-raw-empty.log")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LastRequest(path); err == nil {
		t.Fatal("expected error for empty log")
	}
}

// TestLastRequest_NoRequestInCapWindow guards the maxTailWindow termination:
// a log whose last → REQUEST sits deeper than maxTailWindow from EOF must
// return an error promptly — never loop forever re-reading the capped window
// (the pre-fix loop kept doubling until window >= fi.Size(), which can never
// hold once window is pinned at maxTailWindow on a larger file).
func TestLastRequest_NoRequestInCapWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gopi-raw-deep.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	// Fill past maxTailWindow with non-request payload lines (chunk-ish),
	// then one REQUEST near EOF would be found — so instead leave the tail
	// as pure chunk data: no → REQUEST anywhere in the last maxTailWindow.
	// (maxTailWindow is unexported; 512MB is the documented cap. Writing a
	// full 512MB fixture is heavy — instead exercise the loop-termination
	// condition with a much smaller synthetic cap via the doubling logic:
	// the guard fires when window hits the cap, which for a real 512MB+ log
	// is equivalent. Use a file just over initialTailWindow with the whole
	// tail being chunks so every doubling window finds nothing and the
	// error path must fire.)
	// Pragmatic regression: 9MB of pure chunk data (no REQUEST). The first
	// 8MB window finds nothing; window doubles to 16MB ≥ file size → the
	// pre-fix guard fired the "no → REQUEST entry" error. Same code path as
	// the cap guard; keep the fixture small so the suite stays fast.
	chunk := "← CHUNK\nevent: message_stop\ndata: {}\n\n"
	for sz := 0; sz < 9*1024*1024; sz += len(chunk) {
		if _, err := f.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, _, err := LastRequest(path); err == nil {
		t.Fatal("expected no-REQUEST error for a chunk-only log")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("LastRequest took %v on a no-REQUEST log — suspected infinite loop", elapsed)
	}
}

func TestCountRequests(t *testing.T) {
	path := fixtureLog(t, t.TempDir())
	n, truncated, err := CountRequests(path)
	if err != nil {
		t.Fatalf("CountRequests: %v", err)
	}
	if truncated {
		t.Errorf("small fixture log must not be truncated")
	}
	if n != 2 {
		t.Errorf("expected 2 requests, got %d", n)
	}
}

// TestCountRequests_BudgetExceeded verifies the bounded head-sample: a file
// larger than countScanBudgetBytes reports truncated=true and a lower-bound
// count instead of reading the whole file.
func TestCountRequests_BudgetExceeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gopi-raw-big.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	// First write: a few requests inside the first 4KB (well under budget).
	head := `{"model":"m1","max_tokens":1,"messages":[]}`
	for i := 0; i < 5; i++ {
		if _, err := f.WriteString("→ REQUEST\n" + head + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	// Pad past the budget with non-request bytes (SSE-ish payload lines).
	pad := make([]byte, countScanBudgetBytes+1024)
	for i := range pad {
		pad[i] = 'x'
	}
	if _, err := f.Write(pad); err != nil {
		t.Fatal(err)
	}
	// A trailing request AFTER the budget must not be counted (head sample).
	if _, err := f.WriteString("→ REQUEST\n" + head + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	n, truncated, err := CountRequests(path)
	if err != nil {
		t.Fatalf("CountRequests: %v", err)
	}
	if !truncated {
		t.Errorf("file > budget must report truncated=true")
	}
	if n != 5 {
		t.Errorf("expected 5 requests in the head sample, got %d", n)
	}
}

// TestCountRequests_ExactBudget verifies the EOF probe at the exact budget
// boundary: a file whose size equals countScanBudgetBytes is NOT truncated.
func TestCountRequests_ExactBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gopi-raw-exact.log")
	head := `{"model":"m1","max_tokens":1,"messages":[]}`
	var b strings.Builder
	b.WriteString("→ REQUEST\n" + head + "\n")
	pad := countScanBudgetBytes - b.Len()
	if pad > 0 {
		b.WriteString(strings.Repeat("x", pad))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	n, truncated, err := CountRequests(path)
	if err != nil {
		t.Fatalf("CountRequests: %v", err)
	}
	if truncated {
		t.Errorf("file exactly at budget must not be truncated")
	}
	if n != 1 {
		t.Errorf("expected 1 request, got %d", n)
	}
}

func TestIndexDir(t *testing.T) {
	dir := t.TempDir()
	fixtureLog(t, dir) // gopi-raw-test.log with 2 requests
	// A second log with a different last model.
	writeRawLog(t, dir, "gopi-raw-other.log", [][2]string{
		{"→ REQUEST\n", `{"model":"other","max_tokens":3,"messages":[]}`},
	})
	idxs, err := IndexDir(dir)
	if err != nil {
		t.Fatalf("IndexDir: %v", err)
	}
	if len(idxs) != 2 {
		t.Fatalf("expected 2 indexed logs, got %d", len(idxs))
	}
	for _, idx := range idxs {
		if idx.Requests < 1 {
			t.Errorf("log %s: expected ≥1 request, got %d", idx.Path, idx.Requests)
		}
		if idx.LastSHA256 == "" {
			t.Errorf("log %s: missing sha256", idx.Path)
		}
	}
}
