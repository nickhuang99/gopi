package doubaoasr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ── the zero-dependency gate ────────────────────────────────────────────
//
// WHY THIS TEST EXISTS: the whole reason this package is hand-written is that
// gopi's binary must not grow a dependency (the official SDK adds 4 modules /
// ~3.65 MB). A prose promise ("pure stdlib") rots the first time somebody
// reaches for a websocket or a retry library — so the constraint is CHECKED.
//
// The predicate is deterministic and needs no external tool: every import in
// every production file of this package must be a STANDARD LIBRARY path. A
// stdlib import path has no dot in its first segment; a third-party one always
// has (github.com/…, golang.org/x/…, modernc.org/…). The scan reads the files
// directly (no `go list`, no build cache, no network) and has a read floor, so
// a broken glob cannot pass vacuously.
func TestZeroDependencyStdlibOnlyImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, imp := range parseImports(t, name, string(src)) {
			if strings.Contains(firstSegment(imp), ".") {
				t.Errorf("%s imports %q — this package must stay STANDARD LIBRARY ONLY "+
					"(the zero-dependency constraint is the reason it exists; see the package doc)", name, imp)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("read floor: no production .go files found in this package — the scan proved nothing")
	}
}

// firstSegment returns the first path element of an import path.
func firstSegment(imp string) string {
	if i := strings.IndexByte(imp, '/'); i >= 0 {
		return imp[:i]
	}
	return imp
}

// TestParseImportsSeesEveryImportForm is the regression guard for the hole a
// red-proof run found: the first implementation scanned LINES, so a
// single-line `import _ "modernc.org/sqlite"` was invisible — the gate stayed
// green while the package blank-imported a third-party module. Both spellings
// must be seen, and a string that merely LOOKS like an import must not be.
func TestParseImportsSeesEveryImportForm(t *testing.T) {
	const src = `package x

import (
	"context"
	_ "example.com/blocked"
)

import _ "example.com/one"

var s = "import \"example.com/not-an-import\""
`
	got := parseImports(t, "x.go", src)
	want := []string{"context", "example.com/blocked", "example.com/one"}
	if len(got) != len(want) {
		t.Fatalf("parseImports = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseImports[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

// TestZeroDependencyGateRejectsASingleLineThirdPartyImport runs the gate's own
// predicate over a string carrying the single-line form, so the bypass cannot
// come back silently.
func TestZeroDependencyGateRejectsASingleLineThirdPartyImport(t *testing.T) {
	imports := parseImports(t, "x.go", "package x\n\nimport _ \"modernc.org/sqlite\"\n")
	if len(imports) != 1 || imports[0] != "modernc.org/sqlite" {
		t.Fatalf("single-line import not seen: %v", imports)
	}
	if !strings.Contains(firstSegment(imports[0]), ".") {
		t.Fatalf("firstSegment(%q) = %q — a third-party path must carry a dot",
			imports[0], firstSegment(imports[0]))
	}
}

// parseImports extracts the import paths of one Go source file. It parses the
// file with the standard library's go/parser rather than scanning lines: a
// line-based scan sees only the parenthesized form — `import _ "example.com/x"`
// on ONE line is invisible to it, which is exactly the bypass a red-proof run
// caught (the gate stayed green while the package blank-imported a third-party
// module). go/parser sees both forms by construction.
func parseImports(t *testing.T, name, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	out := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s: unquote import %s: %v", name, spec.Path.Value, err)
		}
		out = append(out, p)
	}
	return out
}

// ── key loading ─────────────────────────────────────────────────────────

func TestLoadAPIKeyRejectsMissingAndEmpty(t *testing.T) {
	if _, err := LoadAPIKey(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Error("missing key file should be an error, not an empty key")
	} else if !strings.Contains(err.Error(), msgNoAPIKey) {
		t.Errorf("error %q does not name the missing key", err)
	}

	empty := filepath.Join(t.TempDir(), APIKeyFileName)
	writeTestFile(t, empty, "  \n\t\n")
	if _, err := LoadAPIKey(empty); err == nil {
		t.Error("an empty key file should be an error")
	}
}

func TestLoadAPIKeyTrimsWhitespace(t *testing.T) {
	p := filepath.Join(t.TempDir(), APIKeyFileName)
	writeTestFile(t, p, "\n  secret-token  \n")
	key, err := LoadAPIKey(p)
	if err != nil {
		t.Fatalf("LoadAPIKey: %v", err)
	}
	if key != "secret-token" {
		t.Errorf("key = %q, want the trimmed value", key)
	}
}

func TestDefaultAPIKeyPathUsesDoubaoDotDir(t *testing.T) {
	got := DefaultAPIKeyPath("/home/example")
	want := filepath.Join("/home/example", APIKeyDirName, APIKeyFileName)
	if got != want {
		t.Errorf("DefaultAPIKeyPath = %q, want %q", got, want)
	}
}

// ── request assembly ────────────────────────────────────────────────────

// TestHotwordsAreDoubleEncodedJSON pins the ONE wire shape the service
// accepts: corpus.context is a STRING whose content is a JSON document
// (measured — an object is refused).
func TestHotwordsAreDoubleEncodedJSON(t *testing.T) {
	ctx, err := buildContext([]string{"RAG", " Agent ", ""})
	if err != nil {
		t.Fatalf("buildContext: %v", err)
	}
	var inner struct {
		Hotwords []struct {
			Word string `json:"word"`
		} `json:"hotwords"`
	}
	if err := json.Unmarshal([]byte(ctx), &inner); err != nil {
		t.Fatalf("context is not a JSON string as the API requires: %v (%q)", err, ctx)
	}
	if len(inner.Hotwords) != 2 || inner.Hotwords[0].Word != "RAG" || inner.Hotwords[1].Word != "Agent" {
		t.Errorf("hotwords = %+v, want RAG+Agent with the blank dropped", inner.Hotwords)
	}
	if empty, err := buildContext(nil); err != nil || empty != "" {
		t.Errorf("no hotwords should omit the field entirely, got %q err=%v", empty, err)
	}
}

func TestSniffWAVGeometry(t *testing.T) {
	wav := testWAV([]byte{0, 0, 0, 0}, 16000, 1, 16)
	g, ok := sniffWAVGeometry(wav)
	if !ok || g.rate != 16000 || g.channels != 1 || g.bits != 16 {
		t.Errorf("sniffWAVGeometry = %+v ok=%v, want 16000/1/16", g, ok)
	}
	if _, ok := sniffWAVGeometry([]byte("not a wav at all, definitely not")); ok {
		t.Error("a non-WAV payload must not report geometry")
	}
}

// TestResourceIDOrDefaultNamesTheDefault proves the value the client sends is
// the named constant — the official SDK's implicit default is a 403 on this
// account, which is why nothing here is left implicit.
func TestResourceIDOrDefaultNamesTheDefault(t *testing.T) {
	if got := ResourceIDOrDefault(""); got != ResourceSeedASRAUC20 {
		t.Errorf("default resource = %q, want %q", got, ResourceSeedASRAUC20)
	}
	if got := ResourceIDOrDefault("volc.custom"); got != "volc.custom" {
		t.Errorf("explicit resource was overridden: %q", got)
	}
}

// ── live-call behaviour against a scripted server ────────────────────────

type fakeService struct {
	*httptest.Server
	submitCode  string
	queryCodes  []string // one per query call; the last repeats
	queryBody   string
	submitBody  string
	submitHTTP  int
	queryHTTP   int
	logID       string
	mu          *[]string // captured request bodies
	lastRequest map[string]any
	queryCalls  int
	// onQuery runs on the server's own goroutine when a /query request lands —
	// the deterministic way for a test to act on the client mid-flight (cancel
	// its context, for example) without a wall-clock sleep in the test.
	onQuery func()
}

func newFakeService(t *testing.T) *fakeService {
	t.Helper()
	reqs := &[]string{}
	fs := &fakeService{
		submitCode: StatusOK,
		queryCodes: []string{StatusOK},
		submitHTTP: http.StatusOK,
		queryHTTP:  http.StatusOK,
		logID:      "log-1",
		mu:         reqs,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decodeJSONBody(t, r, &body)
		fs.lastRequest = body
		*fs.mu = append(*fs.mu, r.Header.Get(headerAPIKey))
		w.Header().Set(headerStatusCode, fs.submitCode)
		w.Header().Set(headerLogID, fs.logID)
		w.Header().Set(headerAPIMessage, "submit-message")
		w.WriteHeader(fs.submitHTTP)
		fs.writeBody(w, fs.submitBody)
	})
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		if fs.onQuery != nil {
			fs.onQuery()
		}
		idx := fs.queryCalls
		fs.queryCalls++
		code := fs.queryCodes[len(fs.queryCodes)-1]
		if idx < len(fs.queryCodes) {
			code = fs.queryCodes[idx]
		}
		w.Header().Set(headerStatusCode, code)
		w.Header().Set(headerLogID, fs.logID)
		w.WriteHeader(fs.queryHTTP)
		fs.writeBody(w, fs.queryBody)
	})
	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func (f *fakeService) writeBody(w http.ResponseWriter, body string) {
	if body == "" {
		body = `{}`
	}
	if _, err := w.Write([]byte(body)); err != nil { //stylegate:ignore test server write to a client that may already have hung up
		return
	}
}

func (f *fakeService) options(t *testing.T) Options {
	t.Helper()
	return Options{
		APIKey:          "test-key-not-a-secret",
		AudioFile:       testAudioFile(t),
		SubmitURL:       f.URL + "/submit",
		QueryURL:        f.URL + "/query",
		PollIntervalSec: 1,
		PollTimeoutSec:  30,
		HTTPTimeoutSec:  10,
		EnablePUNC:      true,
		ShowUtterances:  true,
	}
}

func TestTranscribeHappyPathPollsUntilDone(t *testing.T) {
	fs := newFakeService(t)
	fs.queryCodes = []string{StatusQueued, StatusProcessing, StatusOK}
	fs.queryBody = `{"header":{"code":0},"result":{"text":"hello 世界","utterances":[{"definite":true,"text":"hello","start_time":0,"end_time":900,"speaker_id":"1"}]},"audio_info":{"duration":2000}}`

	res, err := Transcribe(context.Background(), fs.options(t))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "hello 世界" {
		t.Errorf("text = %q", res.Text)
	}
	if len(res.Utterances) != 1 || res.Utterances[0].SpeakerID != "1" || res.Utterances[0].EndMS != 900 {
		t.Errorf("utterances = %+v", res.Utterances)
	}
	if res.DurationMS != 2000 {
		t.Errorf("duration = %d, want the service-measured 2000", res.DurationMS)
	}
	if res.Polls != 3 {
		t.Errorf("polls = %d, want 3 (two retry codes then success)", res.Polls)
	}
	if want := []string{StatusOK, StatusQueued, StatusProcessing, StatusOK}; !equalStrings(res.StatusCodes, want) {
		t.Errorf("status codes = %v, want %v (submit then every query)", res.StatusCodes, want)
	}
	if res.RTF <= 0 {
		t.Errorf("rtf = %v, want a positive service-derived ratio", res.RTF)
	}
	if res.ResourceID != ResourceSeedASRAUC20 {
		t.Errorf("resource = %q, want the named default", res.ResourceID)
	}
	if res.SubmitLogID == "" || res.QueryLogID == "" {
		t.Errorf("log ids must be captured for evidence: %+v", res)
	}
}

// TestTranscribeDeclaresExplicitResourceAndGeometry asserts the wire body:
// explicit resource header, explicit geometry, corpus.context as a string.
func TestTranscribeDeclaresExplicitResourceAndGeometry(t *testing.T) {
	fs := newFakeService(t)
	opts := fs.options(t)
	opts.ResourceID = ResourceBigASRAUC10
	opts.Hotwords = []string{"RAG"}
	opts.EnableSpeakerInfo = true
	opts.EnableLID = true
	opts.EnableITN = true

	if _, err := Transcribe(context.Background(), opts); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	req := fs.lastRequest
	audio, ok := req["audio"].(map[string]any)
	if !ok {
		t.Fatalf("audio block missing: %v", req["audio"])
	}
	if audio["rate"] != float64(16000) || audio["channel"] != float64(1) || audio["bits"] != float64(16) {
		t.Errorf("geometry = %v, want the sniffed 16000/1/16", audio)
	}
	if _, ok := audio["data"].(string); !ok {
		t.Errorf("audio.data must carry the inline base64 payload: %v", audio["data"])
	}
	request, _ := req["request"].(map[string]any)
	if request["model_name"] != ModelName {
		t.Errorf("model_name = %v, want %q", request["model_name"], ModelName)
	}
	corpus, _ := request["corpus"].(map[string]any)
	ctxStr, ok := corpus["context"].(string)
	if !ok || !strings.Contains(ctxStr, "hotwords") {
		t.Errorf("corpus.context = %v, want a JSON STRING carrying hotwords", corpus["context"])
	}
	for _, f := range []string{"enable_itn", "enable_punc", "enable_speaker_info", "enable_lid", "show_utterances"} {
		if v, ok := request[f].(bool); !ok || !v {
			t.Errorf("request.%s = %v, want true", f, request[f])
		}
	}
	// The key travels in the header ONLY — never in the body.
	if strings.Contains(fmt.Sprint(req), opts.APIKey) {
		t.Error("the API key leaked into the request body")
	}
}

// TestSubmitRefusalCarriesRawServiceVerdict is the anti-silent-failure test:
// a non-20000000 submit must surface HTTP status, X-Api-Status-Code, the
// message and the log id — the four fields a support ticket needs.
func TestSubmitRefusalCarriesRawServiceVerdict(t *testing.T) {
	fs := newFakeService(t)
	fs.submitCode = "45000030"
	fs.submitBody = `{"error":"requested resource not granted"}`

	res, err := Transcribe(context.Background(), fs.options(t))
	if err == nil {
		t.Fatal("a refused submit must be an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError — the raw verdict is not inspectable", err)
	}
	if apiErr.Op != opSubmit || apiErr.StatusCode != "45000030" || apiErr.HTTPStatus != http.StatusOK {
		t.Errorf("APIError = %+v, want op=submit status=45000030 http=200", apiErr)
	}
	if apiErr.LogID == "" || !strings.Contains(apiErr.Error(), apiErr.LogID) {
		t.Errorf("log id missing from %q — evidence must survive", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "45000030") {
		t.Errorf("error text %q hides the service status code", apiErr)
	}
	if res == nil || len(res.StatusCodes) == 0 {
		t.Errorf("the refused call must still return its status trail: %+v", res)
	}
	if opts := fs.options(t); strings.Contains(apiErr.Error(), opts.APIKey) {
		t.Error("the API key leaked into an error message")
	}
}

// TestQueryHTTPStatusIsReported covers a transport-level failure on the poll
// call (an HTTP error with a body but no success status).
func TestQueryHTTPStatusIsReported(t *testing.T) {
	fs := newFakeService(t)
	fs.queryHTTP = http.StatusUnauthorized
	fs.queryBody = `{"header":{"code":40000001,"message":"invalid authentication"}}`

	_, err := Transcribe(context.Background(), fs.options(t))
	if err == nil {
		t.Fatal("an HTTP failure on query must be an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.Op != opQuery || apiErr.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("APIError = %+v, want op=query http=401", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "401") {
		t.Errorf("error %q hides the HTTP status", apiErr)
	}
}

func TestTranscribeRejectsEmptyAudioAndMissingKey(t *testing.T) {
	if _, err := Transcribe(context.Background(), Options{APIKey: "k"}); !errors.Is(err, ErrEmptyAudio) {
		t.Errorf("no audio: err = %v, want ErrEmptyAudio", err)
	}
	if _, err := Transcribe(context.Background(), Options{AudioFile: testAudioFile(t)}); err == nil ||
		!strings.Contains(err.Error(), msgNoAPIKey) {
		t.Errorf("no key: err = %v, want a named missing-key error", err)
	}
	both := Options{APIKey: "k", AudioFile: testAudioFile(t), AudioURL: "https://example/x.wav"}
	if _, err := Transcribe(context.Background(), both); err == nil {
		t.Error("file+URL together must be refused")
	}
}

// TestTranscribeHonoursContextCancellation: a cancelled context stops the
// poll loop promptly instead of running to the poll timeout.
//
// The cancellation is triggered DETERMINISTICALLY: the fake service cancels the
// context from its own handler goroutine when the first /query lands, i.e.
// while the client is sitting in the poll wait. A wall-clock time.Sleep would
// only make the race likely — slow CI would let the poll query before the
// cancel, and the test would flake (or silently stop proving cancellation).
func TestTranscribeHonoursContextCancellation(t *testing.T) {
	fs := newFakeService(t)
	fs.queryCodes = []string{StatusProcessing}

	ctx, cancel := context.WithCancel(context.Background())
	fs.onQuery = cancel

	start := time.Now()
	_, err := Transcribe(ctx, fs.options(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v — the poll loop ignored the context", elapsed)
	}
}

// TestTranscribePollTimeoutNamesTheLastStatus: when the job never finishes the
// error must say how long we waited and what the service last said.
func TestTranscribePollTimeoutNamesTheLastStatus(t *testing.T) {
	fs := newFakeService(t)
	fs.queryCodes = []string{StatusProcessing}
	opts := fs.options(t)
	opts.PollTimeoutSec = 1
	opts.PollIntervalSec = 1

	_, err := Transcribe(context.Background(), opts)
	if err == nil {
		t.Fatal("a job that never finishes must time out")
	}
	if !strings.Contains(err.Error(), StatusProcessing) {
		t.Errorf("timeout error %q does not name the last status code", err)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func testAudioFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "clip.wav")
	writeTestFile(t, p, string(testWAV(make([]byte, 32), 16000, 1, 16)))
	return p
}

// testWAV builds a canonical 44-byte-header WAV around pcm.
func testWAV(pcm []byte, rate, channels, bits int) []byte {
	blockAlign := channels * bits / 8
	header := make([]byte, 0, 44)
	le32 := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	le16 := func(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }
	header = append(header, []byte(wavRIFFMagic)...)
	header = append(header, le32(uint32(36+len(pcm)))...)
	header = append(header, []byte(wavWAVEMagic)...)
	header = append(header, []byte(wavFmtChunkID)...)
	header = append(header, le32(16)...)
	header = append(header, le16(1)...)
	header = append(header, le16(uint16(channels))...)
	header = append(header, le32(uint32(rate))...)
	header = append(header, le32(uint32(rate*blockAlign))...)
	header = append(header, le16(uint16(blockAlign))...)
	header = append(header, le16(uint16(bits))...)
	header = append(header, []byte("data")...)
	header = append(header, le32(uint32(len(pcm)))...)
	return append(header, pcm...)
}

func decodeJSONBody(t *testing.T, r *http.Request, into any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		t.Errorf("decode request body: %v", err)
	}
}
