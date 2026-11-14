// sessions_rawlog_trim_framed_test.go — the raw-log BOUND on a LENGTH-PREFIXED
// log.
//
// The legacy fixtures in the sibling file are made of marker LINES, so their cut
// points can be aligned to a newline and the file still reads: a legacy record is
// a marker plus whatever follows, and a half-line at the seam is merely ugly. The
// framed framing is not forgiving in the same way — a record STATES its payload's
// byte count, so a cut inside a record leaves a fragment whose stated length the
// file no longer delivers and every later decode fails (rawlogfmt.ErrMalformedRecord).
// "Align to a line" is therefore NOT "align to a record" here, and that difference
// is what this file measures.
//
// The claims, one test each:
//
//	⑥ a framed log over the cap is rewritten at RECORD boundaries, and the whole
//	   result still decodes from its first record to EOF
//	⑦ a framed log WITHIN the cap is byte-identical afterwards
//	⑧ the trimmed file's framing is still detected as framed (the head and the
//	   marker line did not turn it into something else)
package scratch

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// framedRawLogFixture writes a framed raw log of roughly size bytes, opening with
// the same `#` creation header the writer emits, and returns its path.
func framedRawLogFixture(t *testing.T, dir string, size int64) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the fixture container: %v", err)
	}
	path := filepath.Join(dir, "framed"+SessionRawLogFileSuffix)
	var b bytes.Buffer
	b.WriteString("# gopi session raw HTTP log — created " +
		time.Now().UTC().Format(time.RFC3339) + " pid=1 session=fixture log=fixture.raw.log\n")
	round := 0
	for int64(b.Len()) < size {
		round++
		body := `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"round ` +
			strings.Repeat("x", 96) + `"}]}`
		b.Write(rawlogfmt.EncodeAnnotated(rawlogfmt.TagRequest, "", []byte(body)))
		// A MULTI-LINE payload on purpose: a line-aligned cut lands in the middle of
		// this record's payload, which is exactly the shape a framed trim must not
		// produce.
		chunk := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"answer " +
			itoaScratch(round) + "\"}}\n"
		b.Write(rawlogfmt.EncodeAnnotated(rawlogfmt.TagChunk, "", []byte(chunk)))
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write the framed fixture: %v", err)
	}
	return path
}

// itoaScratch is a tiny integer formatter (strconv would do, but the fixture must
// not depend on anything a reader of this file has to look up).
func itoaScratch(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// decodeFramedWhole walks path from its first record to EOF and returns the number
// of records plus the first decode error. It is the assertion the trim exists to
// preserve: a reader can decode the WHOLE file after a rewrite.
func decodeFramedWhole(t *testing.T, path string) (int, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	r := rawlogfmt.NewReaderFrom(f, rawlogfmt.FormatFramed)
	n := 0
	for {
		_, rerr := r.Next()
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return n, nil
			}
			return n, rerr
		}
		n++
	}
}

// TestRawLogTrimFramedKeepsTheFileDecodable is claim ⑥ — the load-bearing one. A
// trimmed framed log must decode from its first record to EOF: the cut is on a
// RECORD boundary, the head ends on one, and the trim marker (a `#` line, which both
// framings tolerate) sits between them.
func TestRawLogTrimFramedKeepsTheFileDecodable(t *testing.T) {
	root := sessionsRoot(t)
	path := framedRawLogFixture(t, filepath.Join(root, sessGCOldUUID), 1<<20)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}
	capBytes := int64(128 << 10)
	if before.Size() <= capBytes {
		t.Fatalf("the fixture (%d bytes) does not exceed the cap (%d) — the test would prove nothing",
			before.Size(), capBytes)
	}
	// Precondition: the fixture decodes BEFORE the rewrite, so a post-rewrite
	// failure is the trim's doing and not the fixture's.
	if _, derr := decodeFramedWhole(t, path); derr != nil {
		t.Fatalf("the framed fixture does not decode before trimming: %v", derr)
	}

	out, err := TrimRawLog(path, capBytes)
	if err != nil {
		t.Fatalf("TrimRawLog(framed): %v", err)
	}
	if !out.Trimmed {
		t.Fatalf("a %d-byte framed file over a %d-byte cap reports Trimmed=false", before.Size(), capBytes)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the rewritten file: %v", err)
	}
	if after.Size() > capBytes {
		t.Errorf("the rewritten file is %d bytes, above the %d-byte cap", after.Size(), capBytes)
	}
	if after.Size() != out.After {
		t.Errorf("the outcome reports After=%d but the file is %d bytes", out.After, after.Size())
	}

	// THE ASSERTION: every remaining record decodes, to the very end.
	records, derr := decodeFramedWhole(t, path)
	if derr != nil {
		t.Fatalf("the rewritten framed log does not decode: %v (this is what a cut inside a record produces)", derr)
	}
	if records == 0 {
		t.Fatalf("the rewritten file carries no decodable record — the tail was lost")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the rewritten file: %v", err)
	}
	// The creation header still opens it, and exactly one trim marker states the loss.
	lines := strings.Split(string(data), "\n")
	if !strings.HasPrefix(lines[0], "# gopi session raw HTTP log — created ") {
		t.Errorf("the rewritten file no longer opens with the creation header; first line = %q", lines[0])
	}
	markers := 0
	for _, ln := range lines {
		if !strings.HasPrefix(ln, SessionsRawLogTrimMarkerPrefix) {
			continue
		}
		markers++
		if !strings.Contains(ln, "dropped ") {
			t.Errorf("the trim marker does not state how many bytes were dropped: %q", ln)
		}
	}
	if markers != 1 {
		t.Fatalf("the rewritten file carries %d trim markers, want exactly 1", markers)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Errorf("the rewritten file does not end with a newline — its last line is incomplete")
	}
}

// TestRawLogTrimFramedWithinCapIsUntouched is claim ⑦.
func TestRawLogTrimFramedWithinCapIsUntouched(t *testing.T) {
	root := sessionsRoot(t)
	path := framedRawLogFixture(t, filepath.Join(root, sessGCOldUUID), 64<<10)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	capBytes := int64(len(before)) + 1024
	if err := checkRawLogCap(capBytes); err != nil {
		t.Fatalf("the fixture's cap is below this arm's floor: %v", err)
	}
	out, err := TrimRawLog(path, capBytes)
	if err != nil {
		t.Fatalf("TrimRawLog on a within-cap framed file: %v", err)
	}
	if out.Trimmed {
		t.Fatalf("a %d-byte file under a %d-byte cap reports Trimmed=true", out.Before, capBytes)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture after the call: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a within-cap framed file CHANGED on disk (%d -> %d bytes)", len(before), len(after))
	}
}

// TestRawLogTrimFramedKeepsItsFraming is claim ⑧: the rewritten file is still a
// FRAMED log — the head alignment and the inserted marker did not change what the
// file is, so no reader has to guess.
func TestRawLogTrimFramedKeepsItsFraming(t *testing.T) {
	root := sessionsRoot(t)
	path := framedRawLogFixture(t, filepath.Join(root, sessGCOldUUID), 1<<20)
	if _, err := TrimRawLog(path, 128<<10); err != nil {
		t.Fatalf("TrimRawLog(framed): %v", err)
	}
	format, decided, err := rawlogfmt.DetectExact(path)
	if err != nil {
		t.Fatalf("DetectExact on the rewritten file: %v", err)
	}
	if !decided {
		t.Fatalf("the rewritten framed log carries no record in its head — the head alignment lost the first record")
	}
	if format != rawlogfmt.FormatFramed {
		t.Fatalf("the rewritten framed log is detected as %v, want FormatFramed", format)
	}
	// And the trim is still recognised by the marker reader, so a checkpoint caller
	// keeps getting the honest "this log was trimmed" refusal instead of a mystery.
	trimmed, err := RawLogCarriesTrimMarker(path)
	if err != nil {
		t.Fatalf("RawLogCarriesTrimMarker: %v", err)
	}
	if !trimmed {
		t.Fatalf("the rewritten file does not report its trim marker")
	}
}
