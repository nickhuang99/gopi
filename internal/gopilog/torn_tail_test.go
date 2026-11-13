package gopilog

// torn_tail_test.go — the TORN TAIL: a session record whose last line was
// never terminated (no trailing '\n') and does not parse.
//
// THE DEFECT (measured on this host): three session JSONL files ended
// mid-record — 1,286,144 / 512,000 / 405,504 bytes, every one of them exactly
// 4096-byte aligned, the last byte a printable character from inside a JSON
// string. Every strict reader refused the WHOLE file with the anonymous
//
//	JSON parse error: unexpected end of JSON input
//
// which names neither the file, nor the line, nor the byte, and does not say
// that the failure is a TRUNCATED TAIL rather than mid-file corruption — so the
// hundreds of intact preceding lines became unreadable and unaccounted for.
//
// The fixture below is a VERBATIM 827-byte slice of the third file
// (~/.gopi/sessions/01a12f7d-4389-7506-911f-156a62293903/
// 155941c1-6026-4db3-85b9-f0999da86bf6.jsonl), taken from its last whole line
// boundary to the torn end — see tornTailFixtureDoc for the exact command. It
// is not the whole 396 KiB file because a committed blob above the Rule 39
// 256 KiB ceiling would need a baseline permit for a violation this change
// introduced; the tail carries every property the classification reads (a
// clean line boundary, three readable lines, one unterminated line that fails
// to parse, the final byte inside a JSON string).
//
// The test is written so it FAILS on the pre-fix reader (the anonymous
// sentence is what it would get) and passes on the fixed one (Rule 5).

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// tornTailFixture is a copy of one of the three real torn session records.
const tornTailFixture = "testdata/torn-tail-01a12f7d.jsonl"

// tornTailFixtureDoc is the derivation of that fixture — a COMPLETE, pasteable
// command, not a description of one (a truncated reference is a dead end: the
// reader has to be able to get the bytes back).
//
//	tail -c 827 /home/nick/.gopi/sessions/01a12f7d-4389-7506-911f-156a62293903/155941c1-6026-4db3-85b9-f0999da86bf6.jsonl \
//	    > internal/gopilog/testdata/torn-tail-01a12f7d.jsonl
//
// The original is 405,504 bytes and the slice starts at byte 404,677 (the
// boundary of its last COMPLETE line), so the capture is exactly its last 827
// bytes: three intact lines and the 135-byte torn fragment.
const (
	tornTailFixtureSource = "~/.gopi/sessions/01a12f7d-4389-7506-911f-156a62293903/155941c1-6026-4db3-85b9-f0999da86bf6.jsonl"
	tornTailFixtureBytes  = 827
)

// The measured shape of the fixture, asserted by the test rather than assumed:
// three terminated lines, then a fourth with no terminator.
const (
	tornTailCompleteLines = 3
	tornTailLineNumber    = 4
	tornTailLineOffset    = 692
	tornTailBytes         = 135
)

// copyTornTail copies the committed fixture into this TEST's own scratch
// directory (Rule 32: a test never reads the operator's live profile, and never
// mutates the tree it was built from).
func copyTornTail(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(tornTailFixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", tornTailFixture, err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("copy fixture into scratch: %v", err)
	}
	return path
}

// TestTornTailFixtureHasTheMeasuredShape pins the fixture itself: a fixture
// that quietly grew a trailing newline would make every assertion below pass
// for the wrong reason.
func TestTornTailFixtureHasTheMeasuredShape(t *testing.T) {
	data, err := os.ReadFile(tornTailFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(data) != tornTailFixtureBytes {
		t.Fatalf("fixture is %d bytes, want %d", len(data), tornTailLineOffset+tornTailBytes)
	}
	if data[len(data)-1] == '\n' {
		t.Fatalf("fixture ends with a newline — it is not a TORN tail, so it cannot exercise the classification")
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) != tornTailLineNumber {
		t.Fatalf("fixture splits into %d pieces, want %d (3 complete lines + 1 torn)", len(lines), tornTailLineNumber)
	}
	for i := 0; i < tornTailCompleteLines; i++ {
		var ev RawEvent
		if err := json.Unmarshal([]byte(lines[i]), &ev); err != nil {
			t.Fatalf("line %d of the fixture is not parseable (%v) — the fixture must carry READABLE lines before the torn one", i+1, err)
		}
	}
	last := strings.Join(lines[tornTailCompleteLines:], "\n")
	if err := json.Unmarshal([]byte(last), &RawEvent{}); err == nil {
		t.Fatalf("the torn line parses — a torn tail is defined by a line that CANNOT be read back")
	}
	if len(last) != tornTailBytes {
		t.Fatalf("torn fragment is %d bytes, want %d", len(last), tornTailBytes)
	}
}

// TestParseFileTornTailIsNamedAndPositioned is the acceptance case: reading a
// real torn record must report the TORN TAIL as a distinct, named condition
// carrying the file, the 1-based line number, the byte offset where that line
// starts, its byte count and how many lines were readable — never the
// anonymous "JSON parse error: unexpected end of JSON input".
func TestParseFileTornTailIsNamedAndPositioned(t *testing.T) {
	path := copyTornTail(t)

	sess, err := ParseFile(path)
	if err == nil {
		t.Fatalf("a torn record must still be REFUSED (strict refusal stays the default — a silently dropped torn line is not allowed), got a session with %d iterations", len(sess.Iterations))
	}
	if sess != nil {
		t.Fatalf("ParseFile returned a partial session alongside the error — a strict reader refuses the file, it does not half-read it")
	}

	var torn *TornTailError
	if !errors.As(err, &torn) {
		t.Fatalf("error is not the named TornTailError (got %T): %v", err, err)
	}
	if torn.Path != path {
		t.Errorf("error path = %q, want %q", torn.Path, path)
	}
	if torn.Line != tornTailLineNumber {
		t.Errorf("error line = %d, want %d", torn.Line, tornTailLineNumber)
	}
	if torn.Offset != tornTailLineOffset {
		t.Errorf("error offset = %d, want %d (start of the torn line)", torn.Offset, tornTailLineOffset)
	}
	if torn.TornBytes != tornTailBytes {
		t.Errorf("error torn bytes = %d, want %d", torn.TornBytes, tornTailBytes)
	}
	if torn.ReadableLines != tornTailCompleteLines {
		t.Errorf("error readable lines = %d, want %d (the lines an operator can still read)", torn.ReadableLines, tornTailCompleteLines)
	}
	if !torn.Unterminated {
		t.Errorf("error does not mark the tail as UNTERMINATED — that flag is what separates a torn tail from mid-file corruption")
	}

	// The message itself must be self-describing (the CLI prints it verbatim
	// through `Error parsing %s: %v`): it names the condition, the file, the
	// line, the offset and the readable-line count.
	msg := err.Error()
	for _, want := range []string{
		"TORN TAIL",
		path,
		"line 4",
		"byte 692",
		"135 bytes",
		"3 complete line(s)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message does not contain %q:\n%s", want, msg)
		}
	}
	if strings.HasPrefix(msg, "JSON parse error") {
		t.Errorf("error message is still the ANONYMOUS form:\n%s", msg)
	}
	// It must also say WHY the reader is sure: no terminator at the end.
	if !strings.Contains(msg, "no trailing newline") {
		t.Errorf("error message does not state the missing terminator:\n%s", msg)
	}
}

// TestParseFileMidFileCorruptionStaysDistinct proves the classification is a
// DISCRIMINATION and not a rename: the same parse failure on a TERMINATED,
// non-final line is mid-file corruption, carries its own name and position, and
// is never reported as a torn tail.
func TestParseFileMidFileCorruptionStaysDistinct(t *testing.T) {
	path := writeFixture(t, []string{
		`{"type":"session_start","sessionId":"s"}`,
		`{"type":"assistant","mess`, // corrupt, but TERMINATED and NOT last
		`{"type":"assistant","message":{"content":"after"}}`,
	})

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("a corrupt line must be refused")
	}
	var torn *TornTailError
	if errors.As(err, &torn) {
		t.Fatalf("mid-file corruption was reported as a torn tail: %v", err)
	}
	var corrupt *midFileCorruptionError
	if !errors.As(err, &corrupt) {
		t.Fatalf("error is not the named midFileCorruptionError (got %T): %v", err, err)
	}
	if corrupt.Line != 2 || corrupt.Offset != 41 {
		t.Errorf("mid-file error position = line %d byte %d, want line 2 byte 41", corrupt.Line, corrupt.Offset)
	}
	if !strings.Contains(err.Error(), "mid-file corruption") {
		t.Errorf("mid-file error does not name the condition:\n%s", err)
	}
}

// TestParseFileRecordEndingWithoutNewlineButParsingIsAccepted is the control:
// the classifier must not fire on a record whose last line is simply NOT
// terminated. A single-line JSONL without a trailing newline is a legal record
// (the scanner has always accepted it), and turning that into an error here
// would make the fix a regression for every hand-written fixture in this tree.
func TestParseFileRecordEndingWithoutNewlineButParsingIsAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	body := `{"type":"session_start","sessionId":"s"}` + "\n" +
		`{"type":"assistant","message":{"content":"done"}}` // no final newline
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("a complete final line without a trailing newline must be accepted: %v", err)
	}
	if s.ID != "s" {
		t.Fatalf("the record's own session_start was not read: %+v", s)
	}
	if s.StartSegments != 1 {
		t.Fatalf("read %d start segments, want 1 — the unterminated final line must not disturb the header", s.StartSegments)
	}
}

// TestLoadAllSummariesNamesTheRowItSkipped is the TOLERANT reader's half. A
// listing must survive an unreadable row (one damaged record cannot be allowed
// to blind `session log --last` to every other session), but it must not absorb
// the damage in silence: the row it dropped is announced on stderr with the same
// classification the strict reader refuses the file with. Before this, the very
// file `gopi session log <id>` refused with a sentence was listed as an
// ordinary session with no hint that a row had been skipped.
func TestLoadAllSummariesNamesTheRowItSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "torn-session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := os.ReadFile(tornTailFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), data, 0o600); err != nil {
		t.Fatalf("write torn record: %v", err)
	}
	t.Setenv(config.SessionsRootEnv, root)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	summaries, sumErr := LoadAllSummaries(0)
	os.Stderr = prev
	w.Close() //stylegate:ignore closing this pipe's OWN write end is what ends the ReadFrom below; the report read back is the assertion
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	report := buf.String()

	// The listing itself SUCCEEDS: refusing all listings over one damaged record
	// would be the opposite defect.
	if sumErr != nil {
		t.Fatalf("a listing must survive an unreadable row: %v", sumErr)
	}
	if len(summaries) != 0 {
		t.Fatalf("a record whose only activity row is torn produced %d summary row(s); it has no readable activity", len(summaries))
	}
	if report == "" {
		t.Fatal("the skipped row was announced NOWHERE — the reader dropped it silently (Rule 15)")
	}
	for _, want := range []string{"skipped an unreadable row", "TORN TAIL", "session.jsonl", "line 4"} {
		if !strings.Contains(report, want) {
			t.Errorf("announcement does not contain %q:\n%s", want, report)
		}
	}
}
