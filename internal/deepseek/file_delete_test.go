package deepseek

// file_delete_test.go — the reclamation leg's wire contract, asserted on what
// the endpoint actually received.
//
// Three properties, and each one is a failure that would be invisible in
// production:
//
//  1. the DELETE goes to the Files API object URL with the beta header (a
//     missing header is a 400 the carrier would only ever see as "reclaim
//     failed", with no hint that the request itself was malformed);
//  2. a 200 and a 204 both mean "gone", and anything else is a refusal that
//     names the status and the endpoint's own words (Rule 15);
//  3. the id is PATH-ESCAPED — an id carrying a separator must not be able to
//     address a different endpoint.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDeleteFileSendsTheDocumentedDelete pins the request shape.
func TestDeleteFileSendsTheDocumentedDelete(t *testing.T) {
	type seen struct {
		method string
		path   string
		beta   string
		auth   string
	}
	got := make(chan seen, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seen{method: r.Method, path: r.URL.Path,
			beta: r.Header.Get(FilesAPIBetaHeader), auth: r.Header.Get("Authorization")}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k-del", HTTPClient: srv.Client()}
	if err := c.DeleteFile(context.Background(), "file-abc123"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	s := <-got
	if s.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", s.method)
	}
	// The base has no /anthropic suffix here, so the documented prefix applies
	// (claudeFilesURL's own rule, the one UploadFile already follows).
	if want := "/anthropic/v1/files/file-abc123"; s.path != want {
		t.Errorf("path = %q, want %q", s.path, want)
	}
	if s.beta != FilesAPIBetaValue {
		t.Errorf("%s = %q, want %q", FilesAPIBetaHeader, s.beta, FilesAPIBetaValue)
	}
	if s.auth != "Bearer k-del" {
		t.Errorf("Authorization = %q, want the bearer credential", s.auth)
	}
}

// TestDeleteFileKeepsTheFilesPathOnAnAnthropicBaseURL is the other half of the
// URL rule: DeepSeek's own default base already ENDS in /anthropic, and
// appending the prefix again would address /anthropic/anthropic/v1/files.
func TestDeleteFileKeepsTheFilesPathOnAnAnthropicBaseURL(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/anthropic", APIKey: "k", HTTPClient: srv.Client()}
	if err := c.DeleteFile(context.Background(), "file-x"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if want := "/anthropic/v1/files/file-x"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// TestDeleteFileRefusesNon2xxWithTheEndpointsOwnWords is Rule 15 on the delete
// leg: the caller must be able to report WHY a slot was not freed.
func TestDeleteFileRefusesNon2xxWithTheEndpointsOwnWords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"file not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	err := c.DeleteFile(context.Background(), "file-gone")
	if err == nil {
		t.Fatal("DeleteFile succeeded on HTTP 404")
	}
	var delErr *FilesDeleteError
	if !errors.As(err, &delErr) {
		t.Fatalf("error type = %T, want *FilesDeleteError", err)
	}
	if delErr.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", delErr.Status)
	}
	for _, want := range []string{"file-gone", "404", "file not found"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// TestDeleteFileEscapesTheID closes the injection door: an id is opaque text
// that came back from the endpoint (or from the profile's table), and one
// carrying a path separator must address THIS object, never a path of its
// choosing.
func TestDeleteFileEscapesTheID(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	if err := c.DeleteFile(context.Background(), "a/../b"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if strings.Contains(path, "/../") {
		t.Errorf("the id was not escaped, the path is %q", path)
	}
}

// TestDeleteFileRefusesAnEmptyID: nothing to delete is a programming error, and
// silently issuing DELETE /files/ would target the COLLECTION.
func TestDeleteFileRefusesAnEmptyID(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", APIKey: "k"}
	if err := c.DeleteFile(context.Background(), "  "); err == nil {
		t.Fatal("DeleteFile accepted an empty id")
	}
}
