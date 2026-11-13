package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestErrorDisplayCap_TruncatesToNamedConstant pins the display truncation to
// maxErrorDisplayBytes (PROJECT.md Rule 1/13): the shown body is exactly that
// many bytes plus an ellipsis — one constant, so the shown length cannot drift
// from the classified length (non200BodyProbeMax).
func TestErrorDisplayCap_TruncatesToNamedConstant(t *testing.T) {
	const bodyLen = 2000 // > maxErrorDisplayBytes, so the truncation branch runs

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("x", bodyLen))) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("a 400 response must be returned as an error")
	}
	msg := err.Error()
	if !strings.HasSuffix(msg, strings.Repeat("x", maxErrorDisplayBytes)+"...") {
		t.Fatalf("error message must end with exactly %d body bytes + \"...\":\n%s",
			maxErrorDisplayBytes, msg)
	}
	if strings.Count(msg, "x") != maxErrorDisplayBytes {
		t.Fatalf("error message carries %d body bytes, want %d (maxErrorDisplayBytes)",
			strings.Count(msg, "x"), maxErrorDisplayBytes)
	}
}

// TestErrorDisplayCap_ShortBodyUntouched is the negative probe: a body shorter
// than the cap is shown whole, with no ellipsis appended.
func TestErrorDisplayCap_ShortBodyUntouched(t *testing.T) {
	const short = "short provider error"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(short)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer srv.Close()

	c := newMatrixTestClient(t, srv)
	_, err := c.ClaudeChat(context.Background(), baseReq())
	if err == nil {
		t.Fatal("a 400 response must be returned as an error")
	}
	if !strings.Contains(err.Error(), short) || strings.Contains(err.Error(), "...") {
		t.Fatalf("a body under the cap must be shown whole and unellipsised, got:\n%s", err.Error())
	}
}
