package main

// cli_web_files_test.go — the Files API transport's three non-negotiable
// properties, each asserted on what the WIRE actually carried:
//
//  1. a picture turn references the upload by file_id and ships NO base64;
//  2. that same request carries the `anthropic-beta: files-api-2025-04-14`
//     header (measured live: without it the endpoint answers 400);
//  3. an upload failure FAILS the turn — it never silently becomes an inline
//     turn (Project Rule 15).
//
// A fourth check rides along because it is the only reason the Files API is
// worth using here: the SAME picture is uploaded ONCE and referenced by the
// same id on the next turn.
//
// All four drive the real handler over the real mux; nothing calls the staging
// function directly. A test that called stageImagesAsFiles would prove the
// cache works and say nothing about whether a turn uses it.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// filesMockFileIDPrefix is the stem of every id the mock endpoint hands back;
// the upload ORDINAL is appended. The live endpoint behaves the same way
// (measured: identical bytes re-uploaded after a restart came back as
// 82d27704…, not as the 2fc2bed7… the first run had), and a mock that answered a
// fixed id could not tell a cache hit from a re-upload.
const filesMockFileIDPrefix = "file-mock-"

// filesWireCapture records what the MOCK model endpoint received: the body and
// the two headers this change is about — plus the RECLAMATION leg (which file
// ids were DELETEd, and whether the mock accepted them).
type filesWireCapture struct {
	mu      sync.Mutex
	bodies  [][]byte
	betas   []string
	uploads int
	deletes []string
	// deleteStatus is what the mock answers a DELETE with. Zero means 204 (the
	// endpoint acknowledged it); a non-zero value plants a refusal so the
	// "a failed reclaim never fails the turn" arm can be measured instead of
	// assumed.
	deleteStatus int
}

// record stores one model request.
func (c *filesWireCapture) record(body []byte, beta string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, append([]byte{}, body...))
	c.betas = append(c.betas, beta)
}

// countUpload stores one upload and returns the id the mock will answer with.
// The ordinal is what lets an assertion distinguish "the cache was reused" from
// "the endpoint handed back the same name", which a fixed id could not.
func (c *filesWireCapture) countUpload() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uploads++
	return filesMockFileIDPrefix + strconv.Itoa(c.uploads)
}

// countDelete records one DELETE by its file id and answers the planted status
// (204 when none was planted).
func (c *filesWireCapture) countDelete(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletes = append(c.deletes, id)
	if c.deleteStatus == 0 {
		return http.StatusNoContent
	}
	return c.deleteStatus
}

// snapshot returns a consistent copy of the record.
func (c *filesWireCapture) snapshot() (bodies [][]byte, betas []string, uploads int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bodies = append([][]byte{}, c.bodies...)
	betas = append([]string{}, c.betas...)
	return bodies, betas, c.uploads
}

// deleteSnapshot returns the file ids the mock was asked to DELETE, in order.
func (c *filesWireCapture) deleteSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.deletes...)
}

// filesTestServer builds a webServer whose single client points at a mock that
// answers BOTH legs (upload + messages), and records each separately. It pins
// the profile to a FRESH temp home, so the persistent file_id table
// (cli_web_files_cache.go) can neither read nor write the operator's own
// ~/.gopi/web-files-cache.json.
func filesTestServer(t *testing.T, uploadStatus int) (*webServer, *filesWireCapture) {
	t.Helper()
	return filesTestServerInProfile(t, t.TempDir(), uploadStatus, `{"error":"upload refused by the endpoint"}`)
}

// filesTestServerInProfile builds the same server against a CALLER-SUPPLIED
// profile directory. Handing the same directory to two constructions is how the
// restart arm proves cross-process reuse: the second server shares nothing with
// the first except the bytes on disk, exactly like a restarted `gopi web`.
func filesTestServerInProfile(t *testing.T, profileDir string, uploadStatus int, refusalBody string) (*webServer, *filesWireCapture) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, profileDir)
	cap := &filesWireCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/files") {
			// The RECLAMATION leg is a different method on the same
			// collection, so the mock must branch on it BEFORE the upload arm:
			// a DELETE answered with an upload body would let a broken client
			// look correct.
			if r.Method == http.MethodDelete {
				id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				status := cap.countDelete(id)
				if status == http.StatusNoContent {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				http.Error(w, `{"error":{"message":"delete refused by the endpoint"}}`, status)
				return
			}
			id := cap.countUpload()
			if uploadStatus != http.StatusOK {
				http.Error(w, refusalBody, uploadStatus)
				return
			}
			// Drain the multipart body: the client wrote it, and an
			// unread request body would keep the connection from being
			// reused.
			_, _ = io.Copy(io.Discard, r.Body) //stylegate:ignore draining a mock request body
			w.Header().Set("Content-Type", "application/json")
			// The id ENCODES the upload ordinal, exactly like the live endpoint
			// (measured: 2fc2bed7… for one upload, 82d27704… for a re-upload of
			// identical bytes). A fixed id would make every reuse assertion
			// vacuously true: the test could not tell a cache HIT from a
			// re-upload that happened to get the same name back.
			io.WriteString(w, `{"id":"`+id+`","type":"file","size_bytes":12,"created_at":"2026-11-06T21:21:11Z","filename":"web-turn.png","mime_type":"image/png"}`) //stylegate:ignore writing a fixed mock body
			return
		}
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body
		cap.record(body, r.Header.Get(deepseek.FilesAPIBetaHeader))
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "the mock answer")
	}))
	t.Cleanup(srv.Close)

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
	}
	ag.Logger = log.New(io.Discard, "", 0)
	ag.PersistChatSession = true
	return &webServer{addr: ":0", agent: ag, startedAt: time.Now(), visionModel: "glm-5.3-flash"}, cap
}

// postImageTurn drives one /api/chat turn carrying pictures and returns the
// SSE body. It exists because the shared postChat helper ships HISTORY, not
// images — the two carriers of a turn are different fields.
func postImageTurn(t *testing.T, ws *webServer, message string, images []webChatImage) string {
	t.Helper()
	body, _ := json.Marshal(webChatRequest{Message: message, Images: images}) //stylegate:ignore marshalling a value built in-process cannot fail
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleChat status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// pngB64For is the test fixture's picture: short, distinctive bytes so a
// search for its base64 in the request body is exact.
func pngB64For(payload string) string {
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

// TestWebChat_ImageTurnSendsAFileIDAndNoInlineData is deliverable 6's first
// arm: the Messages request carries the file reference and NOT the pixels, and
// it carries the beta header that makes the reference legal.
//
// Both halves are asserted on the SAME captured body — a test that checked
// "file_id present" alone would stay green while the bytes rode along beside it.
func TestWebChat_ImageTurnSendsAFileIDAndNoInlineData(t *testing.T) {
	ws, cap := filesTestServer(t, http.StatusOK)
	b64 := pngB64For("this-is-the-picture-bytes")
	postImageTurn(t, ws, "read the picture", []webChatImage{{MediaType: "image/png", DataB64: b64}})

	bodies, betas, uploads := cap.snapshot()
	if uploads != 1 {
		t.Fatalf("uploads = %d, want exactly 1", uploads)
	}
	if len(bodies) != 1 {
		t.Fatalf("model requests = %d, want exactly 1", len(bodies))
	}
	wire := string(bodies[0])
	if !strings.Contains(wire, `"file_id":"`+filesMockFileIDPrefix+`1"`) {
		t.Errorf("the Messages body carries no file_id:\n%s", wire)
	}
	if strings.Contains(wire, b64) {
		t.Error("the Messages body still inlines the picture's base64 — the transport did not switch")
	}
	if strings.Contains(wire, `"type":"base64"`) {
		t.Error(`the Messages body still carries a base64 image source`)
	}
	// THE WIRE SHAPE, asserted explicitly because the briefing's spelling is
	// refused by the live endpoint: the BLOCK stays "image", the reference
	// lives in the SOURCE. Measured 422 "unknown variant `file`" for anything
	// else.
	if !strings.Contains(wire, `"type":"image"`) || !strings.Contains(wire, `"type":"file"`) {
		t.Errorf("the image reference is not spelled as block:image + source:file:\n%s", wire)
	}
	if got := betas[0]; !strings.Contains(got, deepseek.FilesAPIBetaValue) {
		t.Errorf("anthropic-beta = %q, want it to name %q", got, deepseek.FilesAPIBetaValue)
	}
}

// TestWebChat_UploadFailureDoesNotFallBackToBase64 is deliverable 6's second
// arm, and it is the Rule 15 gate: when the upload fails the turn FAILS. It is
// not retried inline, and the model is never asked anything — a silent
// downgrade would leave the operator believing pictures travel as references
// while the 32 MiB inline cap quietly governs the turn.
func TestWebChat_UploadFailureDoesNotFallBackToBase64(t *testing.T) {
	ws, cap := filesTestServer(t, http.StatusInternalServerError)

	rec := httptest.NewRecorder()
	raw := `{"message":"read the picture","images":[{"media_type":"image/png","data_b64":"` + pngB64For("picture-that-will-not-upload") + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(raw))
	ws.handleChat(rec, req)

	if rec.Code != webFilesUploadFailureStatus {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, webFilesUploadFailureStatus, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), webFilesUploadErrPrefix) {
		t.Errorf("the refusal does not name the upload: %s", rec.Body.String())
	}
	bodies, _, uploads := cap.snapshot()
	if uploads != 1 {
		t.Errorf("upload attempts = %d, want exactly 1 (no retry storm)", uploads)
	}
	if len(bodies) != 0 {
		t.Errorf("the model was asked %d time(s) after a failed upload — the turn must fail, never degrade:\n%s",
			len(bodies), string(bodies[0]))
	}
}

// TestWebChat_SameImageIsUploadedOnceAndReused is deliverable 3: the second
// turn that carries the same picture sends the SAME file_id and uploads
// nothing. Both halves matter — a cache that still uploaded would waste an
// account slot (10 000-file ceiling, no reclamation in this change), and one
// that produced a NEW id per turn would make the cache a lie.
func TestWebChat_SameImageIsUploadedOnceAndReused(t *testing.T) {
	ws, cap := filesTestServer(t, http.StatusOK)
	img := []webChatImage{{MediaType: "image/png", DataB64: pngB64For("the-same-picture-twice")}}

	postImageTurn(t, ws, "first look", img)
	postImageTurn(t, ws, "look again", img)

	_, _, uploads := cap.snapshot()
	if uploads != 1 {
		t.Errorf("uploads = %d for one picture over two turns, want exactly 1", uploads)
	}
	bodies, betas, _ := cap.snapshot()
	if len(bodies) != 2 {
		t.Fatalf("model requests = %d, want 2", len(bodies))
	}
	for i, b := range bodies {
		if !strings.Contains(string(b), `"file_id":"`+filesMockFileIDPrefix+`1"`) {
			t.Errorf("turn %d did not reference the cached file_id:\n%s", i+1, string(b))
		}
		if !strings.Contains(betas[i], deepseek.FilesAPIBetaValue) {
			t.Errorf("turn %d anthropic-beta = %q, want %q", i+1, betas[i], deepseek.FilesAPIBetaValue)
		}
	}
}

// TestWebChat_TextTurnCarriesNoBetaHeader is the negative half of the header
// rule: the beta header is CONDITIONAL. Sending it on a text-only turn would
// put every ordinary turn's wire under a beta gate it does not need (and, on a
// provider that dislikes unknown betas, would break turns that used to work).
func TestWebChat_TextTurnCarriesNoBetaHeader(t *testing.T) {
	ws, cap := filesTestServer(t, http.StatusOK)
	postChat(t, ws, "plain question", nil)
	_, betas, uploads := cap.snapshot()
	if uploads != 0 {
		t.Errorf("a text-only turn uploaded %d file(s)", uploads)
	}
	if len(betas) != 1 {
		t.Fatalf("model requests = %d, want 1", len(betas))
	}
	if betas[0] != "" {
		t.Errorf("text-only turn anthropic-beta = %q, want the header absent", betas[0])
	}
}

// TestWebChat_FileIDSurvivesARestart is the arm this change exists for, and it
// is a RESTART, not a second turn: two `webServer` values built one after the
// other over the SAME profile directory, sharing nothing but the bytes on disk —
// which is exactly what two successive `gopi web` processes share.
//
// Before cli_web_files_cache.go the second server uploaded the same PNG again
// and the live account ended up holding two objects with identical bytes
// (measured: 2fc2bed7… then 82d27704…). The mock answers a DIFFERENT id per
// upload, so "the same file_id in both turns" cannot be an accident of naming.
func TestWebChat_FileIDSurvivesARestart(t *testing.T) {
	profile := t.TempDir()
	img := []webChatImage{{MediaType: "image/png", DataB64: pngB64For("the-picture-across-a-restart")}}

	first, firstCap := filesTestServerInProfile(t, profile, http.StatusOK, "")
	postImageTurn(t, first, "before the restart", img)
	bodiesA, _, uploadsA := firstCap.snapshot()
	if uploadsA != 1 {
		t.Fatalf("the first process uploaded %d time(s), want exactly 1", uploadsA)
	}
	idA := filesMockFileIDPrefix + "1"
	if !strings.Contains(string(bodiesA[0]), idA) {
		t.Fatalf("the first process did not reference %s:\n%s", idA, string(bodiesA[0]))
	}

	// The second process. A fresh cache value, a fresh constructor, a fresh
	// mock counter — the ONLY thing carried over is <profile>/.gopi.
	second, secondCap := filesTestServerInProfile(t, profile, http.StatusOK, "")
	postImageTurn(t, second, "after the restart", img)

	_, _, uploadsB := secondCap.snapshot()
	if uploadsB != 0 {
		t.Errorf("the restarted process uploaded the same picture %d more time(s), want 0 — the id was not reused across processes", uploadsB)
	}
	bodiesB, _, _ := secondCap.snapshot()
	if len(bodiesB) != 1 {
		t.Fatalf("the restarted process made %d model request(s), want 1", len(bodiesB))
	}
	if !strings.Contains(string(bodiesB[0]), `"file_id":"`+idA+`"`) {
		t.Errorf("the restarted process did not reference %s:\n%s", idA, string(bodiesB[0]))
	}
}

// TestWebChat_QuotaRefusalPointsAtTheConsole is deliverable 2: a refusal whose
// cause is the ACCOUNT'S CAPACITY must not reach the operator as the endpoint's
// raw 502. It must say what filled up, what gopi can never do about it (delete
// an upload), and where the freeing happens.
func TestWebChat_QuotaRefusalPointsAtTheConsole(t *testing.T) {
	ws, _ := filesTestServerInProfile(t, t.TempDir(), http.StatusForbidden,
		`{"error":{"message":"insufficient storage quota for this account","type":"insufficient_quota"}}`)

	rec := httptest.NewRecorder()
	raw := `{"message":"read the picture","images":[{"media_type":"image/png","data_b64":"` + pngB64For("picture-when-full") + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(raw))
	ws.handleChat(rec, req)

	if rec.Code != webFilesUploadFailureStatus {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, webFilesUploadFailureStatus, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		webFilesQuotaRemedyConsole,
		"FULL OR THE FILE COUNT LIMIT",
		"Nothing in this carrier deletes an uploaded file",
		webFilesCacheFileName,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the quota refusal does not carry %q:\n%s", want, body)
		}
	}
}

// TestWebChat_NonQuotaRefusalStillNamesTheConsole is the honest half: an
// endpoint refusal that names NO capacity cause must not be reported as a full
// account (Rule 41 — an inference is never dressed as a measurement), and it
// must still leave the operator a place to look.
func TestWebChat_NonQuotaRefusalStillNamesTheConsole(t *testing.T) {
	ws, _ := filesTestServerInProfile(t, t.TempDir(), http.StatusBadRequest,
		`{"error":{"message":"unsupported media type"}}`)

	rec := httptest.NewRecorder()
	raw := `{"message":"read the picture","images":[{"media_type":"image/png","data_b64":"` + pngB64For("picture-rejected") + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(raw))
	ws.handleChat(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "FULL OR THE FILE COUNT LIMIT") {
		t.Errorf("a non-capacity refusal was reported as a full account:\n%s", body)
	}
	if !strings.Contains(body, webFilesQuotaRemedyConsole) || !strings.Contains(body, "unsupported media type") {
		t.Errorf("a non-capacity refusal must quote the endpoint and name the console:\n%s", body)
	}
}

// TestWebFilesCache_CorruptTableIsReportedAndIgnored is Rule 15 on the disk
// table: a table this build cannot read is a CACHE MISS (one extra upload), and
// it must never fail the operator's turn. The repair — delete the file — is
// what the warning says, so it is asserted too.
func TestWebFilesCache_CorruptTableIsReportedAndIgnored(t *testing.T) {
	profile := t.TempDir()
	path := filepath.Join(profile, config.ProfileDirName, webFilesCacheFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the profile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatalf("plant the corrupt table: %v", err)
	}

	ws, cap := filesTestServerInProfile(t, profile, http.StatusOK, "")
	postImageTurn(t, ws, "with a corrupt table on disk",
		[]webChatImage{{MediaType: "image/png", DataB64: pngB64For("bytes-with-a-corrupt-table")}})

	if _, _, uploads := cap.snapshot(); uploads != 1 {
		t.Fatalf("uploads = %d, want 1 — a corrupt table is a cache MISS, never a refused turn", uploads)
	}
	// The turn still uploaded, so the repair path ran: a fresh table replaced
	// the unreadable one, and the NEW table is what a later process reads.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the corrupt table was not replaced: %v", err)
	}
	var doc webFilesCacheDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the table on disk is still not the documented JSON: %v\n%s", err, raw)
	}
	if doc.Version != webFilesCacheFileVersion || len(doc.Entries) != 1 {
		t.Errorf("the replaced table = version %d with %d entries, want version %d with 1", doc.Version, len(doc.Entries), webFilesCacheFileVersion)
	}
}

// filesSeedTable writes a table holding `n` entries into the profile, with
// recency ticks 1..n, and returns the keys in EVICTION order (least recently
// used first) together with their planted file ids.
//
// A table is seeded on disk rather than filled through 256 real turns because
// the defect is about the TABLE being at its bound, not about how it got there:
// a 256-turn fixture would measure the mock's speed and hide the assertion
// behind a minute of SSE. The seeding goes through the documented doc shape
// (webFilesCacheDoc), so the loader below is exercised for real.
func filesSeedTable(t *testing.T, profileDir string, n int) (keys []string, ids []string) {
	t.Helper()
	path := filepath.Join(profileDir, config.ProfileDirName, webFilesCacheFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the profile dir: %v", err)
	}
	doc := webFilesCacheDoc{Version: webFilesCacheFileVersion, Clock: int64(n),
		Entries: make(map[string]webFilesCacheEntryDoc, n)}
	keys = make([]string, 0, n)
	ids = make([]string, 0, n)
	for i := 1; i <= n; i++ {
		key := "seed-key-" + strconv.Itoa(i)
		id := "file-seed-" + strconv.Itoa(i)
		doc.Entries[key] = webFilesCacheEntryDoc{ID: id, Used: int64(i)}
		keys = append(keys, key)
		ids = append(ids, id)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal the seeded table: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write the seeded table: %v", err)
	}
	return keys, ids
}

// captureStderr runs fn with os.Stderr redirected into a pipe and returns what
// the process wrote to it. The cache's every report goes through
// webFilesCacheWarnf, which formats os.Stderr at call time, so this is the
// channel the operator would read.
func filesCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	fn()
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatalf("close the stderr pipe: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close the read end: %v", err)
	}
	return string(data)
}

// TestWebChat_FullCacheStillCachesANewPictureAndUploadsItOnce is THE regression
// arm for the defect this change exists to fix.
//
// The old behaviour, measured: once the table reached webFilesCacheMax, put()
// returned without recording anything, so the picture in hand was uploaded
// again on EVERY turn that carried it — the same new picture sent three times
// produced three uploads, and three permanent account objects (nothing ever
// deletes them). An OLD picture, by contrast, still hit the cache and produced
// zero uploads: the table was not broken, it had stopped admitting.
//
// The assertion is therefore framed as the brief frames it — feed the cache to
// its bound, send a NEW picture three times, and require ONE upload — and it is
// paired with the reclamation half, because an eviction that leaves the remote
// object behind is worse than no cache at all.
func TestWebChat_FullCacheStillCachesANewPictureAndUploadsItOnce(t *testing.T) {
	profile := t.TempDir()
	keys, ids := filesSeedTable(t, profile, webFilesCacheMax)
	ws, cap := filesTestServerInProfile(t, profile, http.StatusOK, "")

	img := []webChatImage{{MediaType: "image/png", DataB64: pngB64For("a-new-picture-when-the-table-is-full")}}
	var stderr string
	stderr = filesCaptureStderr(t, func() {
		postImageTurn(t, ws, "first look at the new picture", img)
		postImageTurn(t, ws, "second look", img)
		postImageTurn(t, ws, "third look", img)
	})

	_, _, uploads := cap.snapshot()
	if uploads != 1 {
		t.Fatalf("uploads = %d for ONE new picture over three turns with a full table, want exactly 1 — the cache stopped admitting (the defect)", uploads)
	}

	// EVERY turn references the SAME id: the new picture was admitted, not
	// re-uploaded. (A cache that admitted but forgot would show a different id
	// per turn and uploads == 3, which the count above already rules out; this
	// arm pins that the MESSAGES agree with it.)
	bodies, _, _ := cap.snapshot()
	if len(bodies) != 3 {
		t.Fatalf("model requests = %d, want 3", len(bodies))
	}
	wantRef := `"file_id":"` + filesMockFileIDPrefix + "1" + `"`
	for i, b := range bodies {
		if !strings.Contains(string(b), `"file_id":"`+filesMockFileIDPrefix+"1"+`"`) {
			t.Errorf("turn %d does not reference the newly admitted id %s:\n%s", i+1, wantRef, string(b))
		}
	}

	// THE RECLAMATION LEG: exactly one entry was evicted — the LEAST RECENTLY
	// USED one, i.e. the seed whose tick is smallest — and its remote object was
	// deleted, exactly once. An eviction without a DELETE leaks the slot and
	// makes the re-upload on a later turn a SECOND object holding the same
	// bytes.
	deletes := cap.deleteSnapshot()
	if len(deletes) != 1 {
		t.Fatalf("DELETE requests = %d, want exactly 1 (one eviction, one reclaim): %v", len(deletes), deletes)
	}
	if deletes[0] != ids[0] {
		t.Errorf("the reclamation deleted %q, want the least recently used entry %q (key %q)", deletes[0], ids[0], keys[0])
	}

	st := ws.files.stats()
	if st.evicted != 1 {
		t.Errorf("evicted counter = %d, want 1", st.evicted)
	}
	if st.reclaimed != 1 || st.reclaimFails != 0 {
		t.Errorf("reclaim counters = %d ok / %d failed, want 1 / 0", st.reclaimed, st.reclaimFails)
	}
	if st.entries != webFilesCacheMax {
		t.Errorf("table size after the eviction = %d, want the bound %d (one out, one in)", st.entries, webFilesCacheMax)
	}

	// RULE 15: the eviction is VISIBLE, naming the object and the cause. A
	// silent eviction is the defect's other half.
	for _, want := range []string{ids[0], webFilesCacheEvictReason, "evicted", "reclaimed file_id " + ids[0]} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not report %q:\n%s", want, stderr)
		}
	}

	// The table ON DISK carries the new picture and NOT the evicted one: a
	// restart must inherit the same decision (an in-memory-only eviction would
	// re-admit the evicted bytes on the next start).
	raw, err := os.ReadFile(filepath.Join(profile, config.ProfileDirName, webFilesCacheFileName))
	if err != nil {
		t.Fatalf("read the table after the eviction: %v", err)
	}
	var doc webFilesCacheDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the table is not the documented JSON: %v\n%s", err, raw)
	}
	if _, stillThere := doc.Entries[keys[0]]; stillThere {
		t.Errorf("the evicted key %q is still in the table on disk", keys[0])
	}
	if len(doc.Entries) != webFilesCacheMax {
		t.Errorf("the table on disk holds %d entries, want %d", len(doc.Entries), webFilesCacheMax)
	}
}

// TestWebChat_FailedReclaimIsCountedAndDoesNotFailTheTurn is the contract half:
// a DELETE that the endpoint refuses must NOT fail the operator's turn (the
// picture in hand uploaded fine), and it must not be silent either — the slot
// is gone for good and only the operator can free it.
func TestWebChat_FailedReclaimIsCountedAndDoesNotFailTheTurn(t *testing.T) {
	profile := t.TempDir()
	_, ids := filesSeedTable(t, profile, webFilesCacheMax)
	ws, cap := filesTestServerInProfile(t, profile, http.StatusOK, "")
	cap.deleteStatus = http.StatusInternalServerError

	img := []webChatImage{{MediaType: "image/png", DataB64: pngB64For("a-picture-whose-predecessor-cannot-be-reclaimed")}}
	var stderr string
	stderr = filesCaptureStderr(t, func() {
		postImageTurn(t, ws, "the turn must still succeed", img)
	})

	if _, _, uploads := cap.snapshot(); uploads != 1 {
		t.Errorf("uploads = %d, want 1", uploads)
	}
	deletes := cap.deleteSnapshot()
	if len(deletes) != 1 || deletes[0] != ids[0] {
		t.Fatalf("DELETE requests = %v, want exactly the least recently used id %q", deletes, ids[0])
	}
	st := ws.files.stats()
	if st.reclaimed != 0 || st.reclaimFails != 1 {
		t.Errorf("reclaim counters = %d ok / %d failed, want 0 / 1", st.reclaimed, st.reclaimFails)
	}
	if st.evicted != 1 {
		t.Errorf("evicted = %d, want 1 (the entry left the table even though the object stayed)", st.evicted)
	}
	// Rule 15: the failure names the object, the cause, and where the operator
	// frees it. It must ALSO be unmistakable that the operator's own upload
	// succeeded — an eviction runs in the middle of an admission, so a sentence
	// that only says "could not …" is read as "my picture failed".
	for _, want := range []string{"could not free OLD file_id " + ids[0], "refused with HTTP 500", webFilesQuotaRemedyConsole, "Your picture was stored and this turn is fine"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not report %q:\n%s", want, stderr)
		}
	}
	for _, forbidden := range []string{"upload failed", "could not upload", "your upload failed"} {
		if strings.Contains(strings.ToLower(stderr), forbidden) {
			t.Errorf("the reclaim failure reads as an upload failure (%q):\n%s", forbidden, stderr)
		}
	}
}

// TestWebFilesCache_EvictionIsLRUNotFIFO pins the POLICY, and it is a unit arm
// on purpose: the difference between LRU and FIFO is invisible in a fixture
// where nothing is ever re-referenced, so it has to be measured by touching an
// old entry and then filling the table.
//
// Why LRU: `s.history` keeps a picture's file reference for the whole
// conversation, so a picture still under discussion is LOOKED UP on every
// following turn. FIFO would evict it anyway and force a re-upload of the one
// picture the operator is actually talking about.
func TestWebFilesCache_EvictionIsLRUNotFIFO(t *testing.T) {
	profile := t.TempDir()
	keys, ids := filesSeedTable(t, profile, webFilesCacheMax)
	t.Setenv(config.GopiDirEnv, profile)

	var cache webFilesCache
	// A hit on the OLDEST entry makes it the most recently used one.
	if got, ok := cache.get(keys[0]); !ok || got != ids[0] {
		t.Fatalf("get(%q) = %q, %v; want %q, true", keys[0], got, ok, ids[0])
	}
	evicted := cache.put("a-brand-new-key", "file-new")
	if len(evicted) != 1 {
		t.Fatalf("evictions = %d, want exactly 1", len(evicted))
	}
	if evicted[0].key != keys[1] {
		t.Errorf("evicted key = %q, want %q (the second-oldest: the oldest was just USED)", evicted[0].key, keys[1])
	}
	if got, ok := cache.get(keys[0]); !ok || got != ids[0] {
		t.Errorf("the just-used entry was evicted: get(%q) = %q, %v", keys[0], got, ok)
	}
	if got, ok := cache.get("a-brand-new-key"); !ok || got != "file-new" {
		t.Errorf("the new entry was not admitted: get = %q, %v", got, ok)
	}
}

// TestWebFilesCache_OversizeTableKeepsTheMostRecentDeterministically covers the
// SECOND half of the same defect, which lived in the LOADER: it filled the map
// by ranging over a Go map and breaking at the bound, and map iteration order
// is deliberately randomised — so an oversize table lost a DIFFERENT arbitrary
// subset on every start, and a picture that had been cached could silently
// vanish and be re-uploaded (one account slot per occurrence).
//
// Two arms: the current version keeps the most recently used entries, and a
// version-1 (unordered) table is upgraded deterministically by key. Both are
// asserted to report the drop, because a silently shrunk cache is a silent
// re-upload later.
func TestWebFilesCache_OversizeTableKeepsTheMostRecentDeterministically(t *testing.T) {
	const oversize = webFilesCacheMax + 44
	for _, version := range []int{webFilesCacheFileVersion, webFilesCacheFileVersionUnordered} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			profile := t.TempDir()
			path := filepath.Join(profile, config.ProfileDirName, webFilesCacheFileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatalf("create the profile dir: %v", err)
			}
			var raw []byte
			var err error
			if version == webFilesCacheFileVersion {
				doc := webFilesCacheDoc{Version: version, Clock: oversize,
					Entries: make(map[string]webFilesCacheEntryDoc, oversize)}
				for i := 1; i <= oversize; i++ {
					doc.Entries["k-"+strconv.Itoa(i)] = webFilesCacheEntryDoc{ID: "id-" + strconv.Itoa(i), Used: int64(i)}
				}
				raw, err = json.Marshal(doc)
			} else {
				doc := webFilesCacheLegacyDoc{Version: version, Entries: make(map[string]string, oversize)}
				for i := 1; i <= oversize; i++ {
					doc.Entries["k-"+strconv.Itoa(i)] = "id-" + strconv.Itoa(i)
				}
				raw, err = json.Marshal(doc)
			}
			if err != nil {
				t.Fatalf("marshal the oversize table: %v", err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatalf("write the oversize table: %v", err)
			}
			t.Setenv(config.GopiDirEnv, profile)

			cache := &webFilesCache{loaded: true, ids: map[string]string{}, used: map[string]int64{}}
			var stderr string
			stderr = filesCaptureStderr(t, cache.loadFromDisk)

			if len(cache.ids) != webFilesCacheMax {
				t.Fatalf("loaded %d entries, want the bound %d", len(cache.ids), webFilesCacheMax)
			}
			if !strings.Contains(stderr, "DROPPED 44") {
				t.Errorf("the trim was not reported:\n%s", stderr)
			}
			// The survivors are the SAME set on every start, and they are the
			// most recently used ones: id-1 was the oldest and must be gone,
			// id-<oversize> the newest and must be present.
			if _, ok := cache.ids["k-1"]; ok {
				t.Error("the OLDEST entry survived the trim — the loader is not keeping by recency")
			}
			newest := "k-" + strconv.Itoa(oversize)
			if _, ok := cache.ids[newest]; !ok {
				t.Errorf("the newest entry %q did not survive the trim", newest)
			}
			// Determinism: a SECOND load of the same bytes keeps the same set.
			again := &webFilesCache{loaded: true, ids: map[string]string{}, used: map[string]int64{}}
			filesCaptureStderr(t, again.loadFromDisk)
			if len(again.ids) != len(cache.ids) {
				t.Fatalf("two loads kept %d and %d entries of the same document", len(cache.ids), len(again.ids))
			}
			for key := range cache.ids {
				if _, ok := again.ids[key]; !ok {
					t.Errorf("load 1 kept %q and load 2 did not — the trim is not deterministic", key)
				}
			}
		})
	}
}

// TestWebFilesCache_LoadsARealVersionOneFileFromDisk is the durable answer to
// "is compatibility tested against a REAL old file, not a struct this build
// re-marshalled?".
//
// The fixture cmd/agent/testdata/web-files-cache-v1-real.json is the table the
// PRE-FIX binary (059eec3) actually wrote to disk during this defect's own
// real-entry proof: 8731 bytes, top-level keys exactly {version, entries}, 256
// entries, every value a bare string. It was copied off disk, not produced by
// any type in this build — so a v1 shape this build cannot reconstruct
// (different key set, different value type, formatting) is exactly what the
// test carries.
//
// WHAT IT PINS: the upgrade happens on LOAD (not on the first put), every one of
// the 256 entries survives a table that is exactly at the bound, the order the
// upgrade invents is the SAME on every start, and the file is not rewritten by a
// load alone.
func TestWebFilesCache_LoadsARealVersionOneFileFromDisk(t *testing.T) {
	const fixture = "testdata/web-files-cache-v1-real.json"
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read the real version-1 fixture: %v", err)
	}
	// The fixture's own shape is the premise: if a future edit "regenerates" it
	// from this build's structs, this arm fails rather than silently testing
	// nothing real.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("the fixture is not a JSON object: %v", err)
	}
	if len(top) != 2 || top["version"] == nil || top["entries"] == nil {
		t.Fatalf("the fixture's top-level keys are %v, want exactly [version entries]", mapKeys(top))
	}
	if strings.Contains(string(raw), `"clock"`) {
		t.Fatal("the fixture carries a clock field — it was written by THIS build, so it cannot stand in for a real version-1 file")
	}

	profile := t.TempDir()
	path := filepath.Join(profile, config.ProfileDirName, webFilesCacheFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the profile dir: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("install the real version-1 file: %v", err)
	}
	t.Setenv(config.GopiDirEnv, profile)

	cache := &webFilesCache{loaded: true, ids: map[string]string{}, used: map[string]int64{}}
	stderr := filesCaptureStderr(t, cache.loadFromDisk)

	// A table EXACTLY at the bound loses nothing — the defect was that it did.
	if len(cache.ids) != webFilesCacheMax {
		t.Fatalf("loaded %d entries from a real version-1 file holding %d, want all %d", len(cache.ids), webFilesCacheMax, webFilesCacheMax)
	}
	if strings.Contains(stderr, "DROPPED") {
		t.Errorf("a table at the bound reported a drop:\n%s", stderr)
	}
	// The oldest seed in that file is the FIRST entry the old binary wrote, and
	// it must have been given the SMALLEST tick by the upgrade.
	if id, ok := cache.ids["probe-seed-001"]; !ok || id != "file-old-256" {
		t.Errorf("probe-seed-001 = %q (present=%v), want file-old-256", id, ok)
	}
	if got := cache.used["probe-seed-001"]; got != 1 {
		t.Errorf("probe-seed-001 got tick %d, want 1 (the upgrade must order deterministically)", got)
	}
	// Loading must not rewrite the file: a read-only consumer leaves it alone.
	if after, err := os.ReadFile(path); err != nil {
		t.Fatalf("re-read the file: %v", err)
	} else if string(after) != string(raw) {
		t.Error("loadFromDisk rewrote the file — a load must not be a write")
	}

	// Determinism across processes: a second load of the same bytes agrees.
	again := &webFilesCache{loaded: true, ids: map[string]string{}, used: map[string]int64{}}
	filesCaptureStderr(t, again.loadFromDisk)
	if len(again.ids) != len(cache.ids) {
		t.Fatalf("two loads of the real file kept %d and %d entries", len(cache.ids), len(again.ids))
	}
	for key, id := range cache.ids {
		if again.ids[key] != id {
			t.Errorf("load 1 had %q=%q, load 2 had %q — the upgrade is not deterministic", key, id, again.ids[key])
		}
		if again.used[key] != cache.used[key] {
			t.Errorf("%q got tick %d on one load and %d on the next", key, cache.used[key], again.used[key])
		}
	}
}

func mapKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
