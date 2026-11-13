package deepseek

// file_upload_test.go — the Files API client's own contract, at the level a
// package test can hold it: what bytes leave, what headers ride along, and how
// every failure is reported. The web carrier's end-to-end arms live in
// cmd/agent/cli_web_files_test.go; these are the client half.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// filesTestCapture records the single upload request the mock endpoint saw.
type filesTestCapture struct {
	path     string
	beta     string
	version  string
	auth     string
	purpose  string
	filename string
	partType string
	bytes    []byte
}

// newFilesTestClient points a Client at a mock Files API endpoint that answers
// with status+body and records the request.
func newFilesTestClient(t *testing.T, status int, body string, cap *filesTestCapture) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.beta = r.Header.Get(FilesAPIBetaHeader)
		cap.version = r.Header.Get(claudeVersionHeader)
		cap.auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		cap.purpose = r.FormValue(FilesAPIPurposeKey)
		f, hdr, err := r.FormFile(FilesAPIFileField)
		if err != nil {
			http.Error(w, "no file part", http.StatusBadRequest)
			return
		}
		defer func() { _ = f.Close() }() //stylegate:ignore closing a test part cannot fail usefully
		cap.filename = hdr.Filename
		cap.partType = hdr.Header.Get("Content-Type")
		cap.bytes, _ = io.ReadAll(f) //stylegate:ignore bounded test read of a small fixture
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body) //stylegate:ignore writing a fixed mock body
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL + claudeAPIPath, APIKey: "test-key", HTTPClient: srv.Client()}
}

// TestUploadFileSendsTheDocumentedMultipartRequest pins the request an upload
// must be: the full "/v1/files" path (a hand-rolled client must spell it — a
// missing "/v1" is a 404), the beta header, the anthropic-version header, the
// bearer credential, `purpose=user_data`, and the picture as the `file` part
// with its own media type.
func TestUploadFileSendsTheDocumentedMultipartRequest(t *testing.T) {
	cap := &filesTestCapture{}
	const resp = `{"id":"file-api-abc","type":"file","size_bytes":75,` +
		`"created_at":"2026-11-06T21:21:11.505657605+00:00",` +
		`"filename":"web-turn.png","mime_type":"image/png"}`
	c := newFilesTestClient(t, http.StatusOK, resp, cap)

	got, err := c.UploadFile(context.Background(), "web-turn.png", "image/png", []byte{0x89, 'P', 'N', 'G'})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if cap.path != claudeAPIPath+claudeFilesPath {
		t.Errorf("upload path = %q, want %q", cap.path, claudeAPIPath+claudeFilesPath)
	}
	if cap.beta != FilesAPIBetaValue {
		t.Errorf("%s = %q, want %q", FilesAPIBetaHeader, cap.beta, FilesAPIBetaValue)
	}
	if cap.version != claudeAPIVersion {
		t.Errorf("%s = %q, want %q", claudeVersionHeader, cap.version, claudeAPIVersion)
	}
	if cap.auth != "Bearer test-key" {
		t.Errorf("Authorization = %q", cap.auth)
	}
	if cap.purpose != FilesAPIPurposeData {
		t.Errorf("purpose = %q, want %q", cap.purpose, FilesAPIPurposeData)
	}
	if cap.filename != "web-turn.png" || cap.partType != "image/png" {
		t.Errorf("file part = %q (%q), want web-turn.png (image/png)", cap.filename, cap.partType)
	}
	if string(cap.bytes) != string([]byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("the part carries %d bytes, want the 4 uploaded", len(cap.bytes))
	}
	// The Anthropic-shaped fields, including the two that differ from the
	// OpenAI-compatible spelling (size_bytes, an RFC 3339 created_at string).
	if got.ID != "file-api-abc" || got.Type != "file" || got.SizeBytes != 75 ||
		got.MIMEType != "image/png" || got.CreatedAt != "2026-11-06T21:21:11.505657605+00:00" {
		t.Errorf("parsed file = %+v", got)
	}
}

// TestUploadFileKeepsTheFilesPathOnAPlainBaseURL covers the other half of
// claudeFilesURL's rule: a base URL that does NOT end in "/anthropic" gets the
// whole prefix, exactly like claudeMessagesURL does.
func TestUploadFileKeepsTheFilesPathOnAPlainBaseURL(t *testing.T) {
	c := &Client{BaseURL: "https://example.test/", APIKey: "k"}
	if got, want := c.claudeFilesURL(), "https://example.test"+claudeAPIPath+claudeFilesPath; got != want {
		t.Errorf("claudeFilesURL() = %q, want %q", got, want)
	}
	c.BaseURL = "https://example.test" + claudeAPIPath
	if got, want := c.claudeFilesURL(), "https://example.test"+claudeAPIPath+claudeFilesPath; got != want {
		t.Errorf("claudeFilesURL() = %q, want %q", got, want)
	}
}

// TestUploadFileRefusesNon200WithTheEndpointsOwnWords: a refusal carries its
// status and the endpoint's body, as a *FilesAPIError so a caller can tell it
// from a transport failure, and NOTHING is returned that could be used as if
// the upload had worked.
func TestUploadFileRefusesNon200WithTheEndpointsOwnWords(t *testing.T) {
	cap := &filesTestCapture{}
	c := newFilesTestClient(t, http.StatusBadRequest, `{"error":"unsupported media type"}`, cap)

	f, err := c.UploadFile(context.Background(), "x.png", "image/png", []byte("data"))
	if err == nil {
		t.Fatalf("UploadFile succeeded on HTTP 400 (file=%+v)", f)
	}
	var apiErr *FilesAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *FilesAPIError: %v", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("FilesAPIError.Status = %d, want 400", apiErr.Status)
	}
	for _, want := range []string{"400", "unsupported media type", "x.png"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestUploadFileRefusesAnIDlessResponse: a 200 whose body is not the documented
// object is a failure, not an empty handle. Without the id the caller has
// nothing to reference, and treating it as success would ship a message block
// with an empty file_id.
func TestUploadFileRefusesAnIDlessResponse(t *testing.T) {
	cases := map[string]string{
		"no id":       `{"type":"file","size_bytes":4}`,
		"not json":    `not-json-at-all`,
		"empty body":  ``,
		"empty idstr": `{"id":"   ","type":"file"}`,
	}
	for name, body := range cases {
		cap := &filesTestCapture{}
		c := newFilesTestClient(t, http.StatusOK, body, cap)
		if _, err := c.UploadFile(context.Background(), "x.png", "image/png", []byte("data")); err == nil {
			t.Errorf("%s: UploadFile succeeded, want a refusal", name)
		}
	}
}

// TestUploadFileRefusesEmptyAndOversizeBodiesBeforeSending: the two client-side
// bounds. Both refuse WITHOUT a network call (an empty picture and a picture
// over the 64 MiB file ceiling cannot be made acceptable by retrying), and the
// oversize message names both ceilings so the reader can tell which one fired.
func TestUploadFileRefusesEmptyAndOversizeBodiesBeforeSending(t *testing.T) {
	cap := &filesTestCapture{}
	c := newFilesTestClient(t, http.StatusOK, `{"id":"x"}`, cap)

	if _, err := c.UploadFile(context.Background(), "x.png", "image/png", nil); err == nil {
		t.Error("an empty body was accepted")
	}
	if cap.path != "" {
		t.Errorf("an empty body still reached the endpoint (%s)", cap.path)
	}

	cap.path = ""
	oversize := make([]byte, filesAPIMaxFileBytes+1)
	_, err := c.UploadFile(context.Background(), "x.png", "image/png", oversize)
	if err == nil {
		t.Fatal("an oversize body was accepted")
	}
	if cap.path != "" {
		t.Errorf("an oversize body still reached the endpoint (%s)", cap.path)
	}
	for _, want := range []string{"67108865", "67108864", "33554432"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the oversize refusal does not name %q: %v", want, err)
		}
	}
}

// TestAddBetaHeaderAppendsAndNeverClobbers: the coexistence rule. Another
// feature's beta must survive, duplicates must not accumulate, and a value
// arriving as a comma-separated list must be split rather than nested.
func TestAddBetaHeaderAppendsAndNeverClobbers(t *testing.T) {
	cases := []struct {
		name    string
		initial []string
		want    string
	}{
		{"absent", nil, filesAPIBetaFilesAPI},
		{"another feature", []string{"some-other-beta"}, "some-other-beta," + filesAPIBetaFilesAPI},
		{"already listed", []string{filesAPIBetaFilesAPI}, filesAPIBetaFilesAPI},
		{"inside a list", []string{"a," + filesAPIBetaFilesAPI + ",b"}, "a," + filesAPIBetaFilesAPI + ",b"},
		{"two headers", []string{"a", "b"}, "a,b," + filesAPIBetaFilesAPI},
	}
	for _, tc := range cases {
		h := http.Header{}
		for _, v := range tc.initial {
			h.Add(filesAPIBetaHeader, v)
		}
		addBetaHeader(h, filesAPIBetaFilesAPI)
		if got := h.Get(filesAPIBetaHeader); got != tc.want {
			t.Errorf("%s: header = %q, want %q", tc.name, got, tc.want)
		}
		// Adding it twice must be idempotent — the send paths re-derive it per
		// attempt.
		addBetaHeader(h, filesAPIBetaFilesAPI)
		if got := h.Get(filesAPIBetaHeader); got != tc.want {
			t.Errorf("%s: header after the second add = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestHasFileBlocksSeesOnlyFileSources: the ONE predicate behind the beta
// header. It must fire for a file source and stay quiet for base64, text and an
// empty request — a false positive puts an unnecessary beta gate on an ordinary
// turn, a false negative gets the reference refused with 400.
func TestHasFileBlocksSeesOnlyFileSources(t *testing.T) {
	if HasFileBlocks(nil) {
		t.Error("nil request reported a file block")
	}
	base64Only := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{
		{Type: claudeImageBlockType, Source: &ClaudeImageSource{Type: claudeSourceTypeBase64, MediaType: "image/png", Data: "aGk="}},
		{Type: "text", Text: "hi"},
	}}}}
	if HasFileBlocks(base64Only) {
		t.Error("a base64 image reported a file block")
	}
	fileRef := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{
		{Type: claudeImageBlockType, Source: &ClaudeImageSource{Type: ClaudeSourceTypeFileAPI, FileID: "file-api-1"}},
	}}}}
	if !HasFileBlocks(fileRef) {
		t.Error("a file reference was not seen")
	}
}

// TestClaudeHeadersCarryTheBetaOnlyForFileRequests drives the header through the
// REAL send path (ClaudeChat) rather than calling setClaudeHeaders directly: the
// question is whether a request that references a file reaches the wire with the
// header, and only the send path answers it.
func TestClaudeHeadersCarryTheBetaOnlyForFileRequests(t *testing.T) {
	cases := []struct {
		name     string
		source   *ClaudeImageSource
		wantBeta string
	}{
		{"file reference", &ClaudeImageSource{Type: ClaudeSourceTypeFileAPI, FileID: "file-api-1"}, FilesAPIBetaValue},
		{"inline base64", &ClaudeImageSource{Type: claudeSourceTypeBase64, MediaType: "image/png", Data: "aGk="}, ""},
	}
	for _, tc := range cases {
		var gotBeta string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBeta = r.Header.Get(FilesAPIBetaHeader)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`) //stylegate:ignore writing a fixed mock body
		}))
		c := &Client{BaseURL: srv.URL + claudeAPIPath, APIKey: "k", HTTPClient: srv.Client()}
		msgs := []Message{{Role: "user", Content: "read this", Images: []ClaudeImageSource{*tc.source}}}
		req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)
		if _, err := c.ClaudeChat(context.Background(), req); err != nil {
			srv.Close()
			t.Fatalf("%s: ClaudeChat: %v", tc.name, err)
		}
		srv.Close()
		if gotBeta != tc.wantBeta {
			t.Errorf("%s: anthropic-beta = %q, want %q", tc.name, gotBeta, tc.wantBeta)
		}
	}
}

// TestBuildClaudeRequestEmitsTheMeasuredImageShape pins the wire shape the live
// endpoint accepts: the BLOCK is "image" and the reference is the SOURCE's
// "file". The briefing's {"type":"file","file_id":…} is refused with HTTP 422
// "unknown variant `file`" (measured 2026-11-06), so this test is what stops
// that spelling from coming back.
func TestBuildClaudeRequestEmitsTheMeasuredImageShape(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "what colour", Images: []ClaudeImageSource{{Type: ClaudeSourceTypeFileAPI, FileID: "file-api-9"}}},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(raw)
	// Asserted field by field, not as one substring: the block legitimately
	// carries "input":null (every non-tool_use block does — the API tolerates
	// it, see ClaudeContentBlock), so a single-literal match would fail on a
	// field this change does not own.
	for _, want := range []string{
		`"type":"image"`,
		`"source":{"type":"file","file_id":"file-api-9"}`,
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("the image block is missing %s:\n%s", want, wire)
		}
	}
	if strings.Contains(wire, `"type":"file","file_id":"file-api-9"}}`) &&
		!strings.Contains(wire, `"source":{"type":"file"`) {
		t.Errorf("the reference is not inside a source — the refused shape:\n%s", wire)
	}
	if strings.Contains(wire, `"data"`) || strings.Contains(wire, `"media_type"`) {
		t.Errorf("a file reference emitted inline-source fields:\n%s", wire)
	}
}
