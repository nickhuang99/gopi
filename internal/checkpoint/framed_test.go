// framed_test.go — the two readers in this package must serve BOTH raw-log
// framings, and they must agree with their legacy behaviour on the same session.
//
// The write side moved to a length-prefixed framing (a header states the payload's
// byte count). The two readers here answer the two questions the resume path asks —
// "what was the last request?" and "how many requests did this run send?" — and
// both used to answer them by matching a marker LINE. On a framed log that is not a
// near miss, it is a confident wrong answer: LastRequest reported ErrNoRequest (the
// file has no `→ REQUEST` line at all) and CountRequests reported 0 (which a caller
// renders as "this run sent nothing"). So each gets a framing-correct arm, and the
// equivalence is asserted rather than assumed.
package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// framedRec is one record of the fixture session.
type framedRec struct {
	tag     string
	ann     string
	payload string
}

// framedFixtureRecs is the ONE record list both renderings are built from, so a
// drift between the two fixtures cannot pass as a framing difference.
func framedFixtureRecs() []framedRec {
	return []framedRec{
		{rawlogfmt.TagRequest, "", `{"model":"m1","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"first"}]}]}`},
		{rawlogfmt.TagChunk, "", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hel\"}}\n"},
		{rawlogfmt.TagChunk, "", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n"},
		{rawlogfmt.TagRequest, "", `{"model":"m2","max_tokens":200,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"sig"},{"type":"text","text":"hi"}]}]}`},
		{rawlogfmt.TagResponse, "", `{"id":"r1","content":[{"type":"text","text":"ok"}]}`},
	}
}

// writeFramedLog renders recs in the framed framing and returns the path.
func writeFramedLog(t *testing.T, dir string, recs []framedRec) string {
	t.Helper()
	path := filepath.Join(dir, "gopi-raw-framed.log")
	var body []byte
	for _, r := range recs {
		body = append(body, rawlogfmt.EncodeAnnotated(r.tag, r.ann, []byte(r.payload))...)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write framed log: %v", err)
	}
	if format, err := rawlogfmt.Detect(path); err != nil || format != rawlogfmt.FormatFramed {
		t.Fatalf("fixture detected as %v (%v), want FormatFramed", format, err)
	}
	return path
}

// TestLastRequest_FramedFindsTheLastRequest pins the resume point in a framed log:
// the LAST request record, with its payload byte-exact.
func TestLastRequest_FramedFindsTheLastRequest(t *testing.T) {
	path := writeFramedLog(t, t.TempDir(), framedFixtureRecs())
	entry, req, err := LastRequest(path)
	if err != nil {
		t.Fatalf("LastRequest(framed): %v", err)
	}
	if entry.Dir != DirectionRequest {
		t.Errorf("Dir = %q, want %q", entry.Dir, DirectionRequest)
	}
	if req.Model != "m2" || req.MaxTokens != 200 {
		t.Errorf("last request = %s/%d, want m2/200", req.Model, req.MaxTokens)
	}
	if got, want := string(entry.Payload), framedFixtureRecs()[3].payload; got != want {
		t.Errorf("payload not byte-exact:\n got %q\nwant %q", got, want)
	}
	// The offset must be a real position in the file, and decoding from it must
	// reach this very record — that is what makes it usable as a resume marker.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.Seek(entry.Offset, 0); err != nil {
		t.Fatalf("seek to recorded offset %d: %v", entry.Offset, err)
	}
	r := rawlogfmt.NewReaderFrom(f, rawlogfmt.FormatFramed)
	rec, err := r.Next()
	if err != nil {
		t.Fatalf("decode at recorded offset %d: %v", entry.Offset, err)
	}
	if rec.Tag != rawlogfmt.TagRequest {
		t.Fatalf("record at recorded offset has tag %q, want %q", rec.Tag, rawlogfmt.TagRequest)
	}
	if got := trimLogFuncNewline(rec.Payload); string(got) != framedFixtureRecs()[3].payload {
		t.Fatalf("record at recorded offset is not the last request")
	}
}

// TestLastRequest_FramedAndLegacyAgree is the equivalence assertion: the same
// session in either framing yields the same resume point.
func TestLastRequest_FramedAndLegacyAgree(t *testing.T) {
	dir := t.TempDir()
	legacyPath := writeRawLog(t, dir, "legacy.log", [][2]string{
		{"→ REQUEST\n", framedFixtureRecs()[0].payload},
		{"← CHUNK\n", framedFixtureRecs()[1].payload + "\n"},
		{"← CHUNK\n", framedFixtureRecs()[2].payload + "\n"},
		{"→ REQUEST\n", framedFixtureRecs()[3].payload},
		{"← RESPONSE\n", framedFixtureRecs()[4].payload},
	})
	framedPath := writeFramedLog(t, dir, framedFixtureRecs())

	le, lreq, err := LastRequest(legacyPath)
	if err != nil {
		t.Fatalf("LastRequest(legacy): %v", err)
	}
	fe, freq, err := LastRequest(framedPath)
	if err != nil {
		t.Fatalf("LastRequest(framed): %v", err)
	}
	if string(le.Payload) != string(fe.Payload) {
		t.Fatalf("payloads differ:\n legacy %q\n framed %q", le.Payload, fe.Payload)
	}
	if le.Dir != fe.Dir {
		t.Fatalf("directions differ: legacy %q, framed %q", le.Dir, fe.Dir)
	}
	if lreq.Model != freq.Model || lreq.MaxTokens != freq.MaxTokens {
		t.Fatalf("decoded requests differ: legacy %s/%d, framed %s/%d",
			lreq.Model, lreq.MaxTokens, freq.Model, freq.MaxTokens)
	}
}

// TestCountRequests_FramedCountsRecords pins the count on a framed log, including
// the case that motivated the framing: a payload quoting the marker vocabulary must
// NOT be counted as requests.
func TestCountRequests_FramedCountsRecords(t *testing.T) {
	dir := t.TempDir()
	recs := []framedRec{
		{rawlogfmt.TagRequest, "", `{"model":"m1","messages":[]}`},
		// A request whose BODY quotes the legacy vocabulary. A line counter sees
		// three `→ REQUEST` lines here; a record counter sees one request.
		{rawlogfmt.TagRequest, "", "{\"model\":\"m2\",\"messages\":[{\"role\":\"user\",\"content\":\"→ REQUEST\\n→ REQUEST\\n→ REQUEST\"}]}"},
		{rawlogfmt.TagResponse, "", `{"content":"ok"}`},
	}
	path := writeFramedLog(t, dir, recs)

	n, truncated, err := CountRequests(path)
	if err != nil {
		t.Fatalf("CountRequests(framed): %v", err)
	}
	if truncated {
		t.Errorf("small framed log must not be truncated")
	}
	if n != 2 {
		t.Errorf("count = %d, want 2 — marker text inside a payload is DATA, not a request", n)
	}

	// And the same session in the legacy framing gives the same answer, so the two
	// arms of CountRequests agree on the population they describe.
	legacyPath := writeRawLog(t, dir, "legacy-count.log", [][2]string{
		{"→ REQUEST\n", recs[0].payload},
		{"← RESPONSE\n", recs[2].payload},
	})
	ln, _, err := CountRequests(legacyPath)
	if err != nil {
		t.Fatalf("CountRequests(legacy): %v", err)
	}
	if ln != 1 {
		t.Errorf("legacy count = %d, want 1", ln)
	}
}

// TestCountRequests_FramedBudgetIsALowerBound mirrors the legacy budget contract:
// a framed file larger than the budget reports truncated=true rather than reading
// the whole file, and a record straddling the cut is not counted (its run has not
// finished). A partially read record is a fact about the SAMPLE, never a decode
// error the caller must handle.
func TestCountRequests_FramedBudgetIsALowerBound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gopi-raw-framed-big.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"m1","messages":[]}`
	const inBudget = 5
	for i := 0; i < inBudget; i++ {
		if _, err := f.Write(rawlogfmt.EncodeAnnotated(rawlogfmt.TagRequest, "", []byte(body))); err != nil {
			t.Fatal(err)
		}
	}
	// One oversized CHUNK record deliberately straddles the budget boundary.
	big := make([]byte, countScanBudgetBytes+1024)
	for i := range big {
		big[i] = 'x'
	}
	if _, err := f.Write(rawlogfmt.EncodeAnnotated(rawlogfmt.TagChunk, "", big)); err != nil {
		t.Fatal(err)
	}
	// A request AFTER the budget must not be counted.
	if _, err := f.Write(rawlogfmt.EncodeAnnotated(rawlogfmt.TagRequest, "", []byte(body))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	n, truncated, err := CountRequests(path)
	if err != nil {
		t.Fatalf("CountRequests(framed, over budget): %v", err)
	}
	if !truncated {
		t.Errorf("file > budget must report truncated=true")
	}
	if n != inBudget {
		t.Errorf("count = %d, want %d (the head sample's complete request records)", n, inBudget)
	}
}
