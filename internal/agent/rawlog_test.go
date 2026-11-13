package agent

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nick/gopi/internal/rawlogfmt"
)

// WHY THIS FILE SETS GOPI_RAW_LOG TO "" INSTEAD OF UNSETTING IT (Rule 17).
//
// The raw logger is disabled by the value "OFF" and by nothing else —
// newRawLoggerInDir reads the variable once (`if v := os.Getenv("GOPI_RAW_LOG");
// strings.ToUpper(v) == "OFF"`, rawlog.go) — so an ABSENT variable and an EMPTY
// one are the same input: the default-enable path. The tests below want that
// path (or, in the SameSecond test, a raw log that is definitely enabled) and
// used to say it with a bare os.Unsetenv call.
//
// Rule 17 reports the os.Setenv/os.Unsetenv CALL, not the value it carries: a
// test writing process state directly is the defect, because the undone value
// is the process's, not the test's. `t.Setenv` is the sanctioned spelling, and
// it is a strictly better instrument here — its Cleanup restores the variable's
// PREVIOUS STATE INCLUDING ABSENCE (testing.common.Setenv captures the key with
// os.LookupEnv and unsets it again when it was not set), so a caller that
// exported GOPI_RAW_LOG=OFF before the run gets its value back, where the bare
// os.Unsetenv left the variable set to the empty string.
//
// Safe to use as a test-scoped instrument here: no test in this file (or
// package) calls t.Parallel, which t.Setenv forbids.
func TestNewRawLogger_DefaultEnabled(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() error: %v", err)
	}
	if rl == nil {
		t.Fatal("expected non-nil RawLogger by default")
	}
	defer rl.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "gopi-raw-") || !strings.HasSuffix(entries[0].Name(), ".log") {
		t.Fatalf("unexpected file name: %s", entries[0].Name())
	}
}

func TestNewRawLogger_Disabled(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "OFF")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl != nil {
		rl.Close() //stylegate:ignore test cleanup on an unexpected non-nil logger before t.Fatal; the test is already failing, so a teardown close error adds nothing
		t.Fatal("expected nil RawLogger when GOPI_RAW_LOG=OFF")
	}
}

func TestNewRawLogger_OffCaseInsensitive(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "off")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl != nil {
		rl.Close() //stylegate:ignore test cleanup on an unexpected non-nil logger before t.Fatal in the GOPI_RAW_LOG=off case; the test is already failing
		t.Fatal("expected nil when GOPI_RAW_LOG=off")
	}
}

// TestRawLogger_Write pins what replaced the removed bare-bytes Write path: a
// NEW raw log carries length-prefixed records end to end, and the length field of
// each header is the payload's exact byte count (acceptance criterion 1 at the
// writer). Before this change the same fixture was three bare Write calls that
// left un-framed bytes in the file — the shape that made an in-payload marker
// indistinguishable from a direction.
func TestRawLogger_Write(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() error: %v", err)
	}
	defer rl.Close()

	fn := rl.LogFunc()
	fn(rawlogfmt.TagRequest, "", []byte(`{"messages":[{"role":"user","content":"hello world\nsecond line"}]}`))
	rl.Sync() // R5: buffered writes — flush before reading back

	entries, _ := os.ReadDir(dir)                                 //stylegate:ignore lists a directory the test populated; the entries are the assertion
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name())) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	content := string(data)
	if !strings.Contains(content, "hello world") {
		t.Errorf("missing 'hello world':\n%s", content)
	}
	if !strings.Contains(content, "second line") {
		t.Errorf("missing 'second line':\n%s", content)
	}

	// The declared length is the payload's byte count, checked on the raw header
	// line as well as through the decoder: a header that agreed with itself but not
	// with the file would decode "successfully" while losing bytes.
	records := readRecords(t, data)
	if len(records) != 1 {
		t.Fatalf("decoded %d records, want 1; file:\n%s", len(records), content)
	}
	headerLine, _, _ := strings.Cut(content[strings.Index(content, rawlogfmt.TagRequest):], "\n")
	wantHeader := rawlogfmt.HeaderLine(rawlogfmt.TagRequest, len(records[0].Payload))
	if headerLine+"\n" != wantHeader {
		t.Fatalf("header line = %q, want %q (payload is %d bytes)",
			headerLine, strings.TrimSuffix(wantHeader, "\n"), len(records[0].Payload))
	}
}

func TestRawLogger_LogFunc(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() error: %v", err)
	}
	defer rl.Close()

	fn := rl.LogFunc()
	fn(rawlogfmt.TagRequest, "", []byte(`{"model":"test"}`))
	fn(rawlogfmt.TagChunk, "", []byte(`{"choices":[{"delta":{"content":"hi"}}]}`))
	fn(rawlogfmt.TagChunk, "", []byte(`{"choices":[{"delta":{"content":" there"}}]}`))
	rl.Sync() // R5: buffered chunks — flush before reading back

	entries, _ := os.ReadDir(dir)                                 //stylegate:ignore lists a directory the test populated; the entries are the assertion
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name())) //stylegate:ignore reads a file the test itself wrote; the content is the assertion

	// THE CONTRACT IS THE FRAMING, not a marker string: a new log is
	// length-prefixed, so each record opens with "<tag> <len>\n" and the payload
	// follows VERBATIM. Asserting the marker text here would pin the format this
	// change removes — and asserting only "the payload is in there" would pass for
	// a log whose length field was wrong.
	records := readRecords(t, data)
	want := []struct{ tag, payload string }{
		{rawlogfmt.TagRequest, `{"model":"test"}`},
		{rawlogfmt.TagChunk, `{"choices":[{"delta":{"content":"hi"}}]}`},
		{rawlogfmt.TagChunk, `{"choices":[{"delta":{"content":" there"}}]}`},
	}
	if len(records) != len(want) {
		t.Fatalf("decoded %d records, want %d; file:\n%s", len(records), len(want), data)
	}
	for i, w := range want {
		if records[i].Tag != w.tag || string(records[i].Payload) != w.payload {
			t.Fatalf("record %d = %q/%q, want %q/%q", i+1,
				records[i].Tag, records[i].Payload, w.tag, w.payload)
		}
		// Acceptance criterion 1, at the writer: the length field the header
		// declares IS the payload's byte count.
		if got := len(records[i].Payload); got != len(w.payload) {
			t.Fatalf("record %d carries %d payload bytes, want %d", i+1, got, len(w.payload))
		}
	}
	if bytes.Contains(data, []byte(rawlogfmt.MarkerRequest)) {
		t.Fatalf("a NEW log still carries the legacy marker text:\n%s", data)
	}
}

func TestRawLogger_ConcurrentWrites(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() error: %v", err)
	}
	defer rl.Close()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			rl.LogFunc()(rawlogfmt.TagChunk, "", []byte{byte('A' + idx%26)})
		}(i)
	}

	wg.Wait()
	rl.Sync() // R5: buffered writes — flush before reading back

	entries, _ := os.ReadDir(dir)                                 //stylegate:ignore lists a directory the test populated; the entries are the assertion
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name())) //stylegate:ignore reads a file the test itself wrote; the content is the assertion

	// No races and no data loss: n records, one byte each. This is a STRONGER
	// assertion than the byte count it replaces, and it is the property the removed
	// bare-bytes Write could not promise — 50 concurrent unframed appends could
	// interleave into bytes no reader could walk, while a framed record is written
	// in one locked call.
	records := readRecords(t, data)
	if len(records) != n {
		t.Fatalf("decoded %d records, want %d", len(records), n)
	}
	var got int
	for _, rec := range records {
		if len(rec.Payload) != 1 {
			t.Fatalf("record payload = %q, want exactly one byte", rec.Payload)
		}
		got += len(rec.Payload)
	}
	if got != n {
		t.Fatalf("payload bytes = %d, want %d", got, n)
	}
}

// readRecords decodes every record in a raw log's BYTES through the ONE decoder
// (rawlogfmt), so a test asserts the format contract instead of a substring of it.
// Bytes before the first record header (the `#` creation header) are skipped by the
// decoder itself.
func readRecords(t *testing.T, data []byte) []rawlogfmt.Record {
	t.Helper()
	r := rawlogfmt.NewReaderFrom(bytes.NewReader(data), rawlogfmt.FormatFramed)
	var out []rawlogfmt.Record
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("decode record %d: %v\nfile:\n%s", len(out)+1, err, data)
		}
		out = append(out, rec)
	}
}

func TestRawLogger_MkdirAutoCreate(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() should auto-create dirs: %v", err)
	}
	defer rl.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("raw log directory was not auto-created")
	}
}

func TestRawLogger_Path(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() error: %v", err)
	}
	defer rl.Close()

	p := rl.Path()
	if !strings.HasPrefix(p, dir) {
		t.Errorf("Path() = %q, expected prefix %q", p, dir)
	}
	if !strings.Contains(p, "gopi-raw-") {
		t.Errorf("Path() = %q, expected 'gopi-raw-' substring", p)
	}
}

// TestRawLogger_BufferedThenSync verifies the R5 buffering: before Sync() the
// file is empty (writes stay in the buffer); after Sync() the Write and LogFunc
// contents are complete and in order.
func TestRawLogger_BufferedThenSync(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger() error: %v", err)
	}
	defer rl.Close()

	fn := rl.LogFunc()
	fn(rawlogfmt.TagChunk, "", []byte(`{"delta":"a"}`))
	fn(rawlogfmt.TagChunk, "", []byte(`{"delta":"b"}`))

	entries, _ := os.ReadDir(dir) //stylegate:ignore lists a directory the test populated; the entries are the assertion
	path := filepath.Join(dir, entries[0].Name())

	// Before Sync: the buffered writes are not on disk. The file is NOT empty
	// though — the creation header is written straight to the file at
	// NewRawLogger time (that is what makes a zero-byte raw log impossible),
	// so only the header may be present here.
	data, _ := os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if got := string(data); !strings.HasPrefix(got, rawLogHeaderPrefix) {
		t.Fatalf("before Sync the file must hold the creation header and nothing else, got %d bytes: %q", len(data), got)
	}
	if strings.Contains(string(data), `"delta"`) {
		t.Fatalf("buffered content leaked to disk before Sync:\n%s", string(data))
	}

	rl.Sync()

	// After Sync: both records are on disk, in order, and each is a whole framed
	// record — a new log is length-prefixed end to end and carries NO legacy marker.
	data, _ = os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	records := readRecords(t, data)
	var payloads []string
	for _, rec := range records {
		payloads = append(payloads, string(rec.Payload))
	}
	if got, want := strings.Join(payloads, "|"), `{"delta":"a"}|{"delta":"b"}`; got != want {
		t.Fatalf("decoded payloads = %s, want %s; file:\n%s", got, want, data)
	}
	if bytes.Contains(data, []byte(rawlogfmt.MarkerChunk)) {
		t.Fatalf("a NEW log still carries the legacy chunk marker:\n%s", data)
	}
}

// TestRawLogger_NeverStartsEmpty is the regression guard for the session that
// left a 0-byte raw log (2026-09-25): the log is created at session start but
// its first HTTP byte only lands at the first round, so a session that ended
// before its first round produced a file that answered nothing — the operator
// could not tell "raw logging was off", "the session never started" and "the
// run wrote nothing" apart. The creation header is written when the file is
// created, so the log identifies itself even when no round ever follows.
func TestRawLogger_NeverStartsEmpty(t *testing.T) {
	// t.Setenv (Rule 17): an unset GOPI_RAW_LOG and an empty one take the same
	// path in newRawLoggerInDir (only "OFF" disables), and t.Setenv restores the
	// caller's environment when this test ends.
	t.Setenv("GOPI_RAW_LOG", "")
	tempHome(t)

	runUUID := "fb29b78b-8b72-4da7-93d1-706b9696b5f6"
	rl, err := NewRawLoggerForRun("", runUUID)
	if err != nil {
		t.Fatalf("NewRawLoggerForRun: %v", err)
	}
	// Close immediately — the session that never reached a round.
	if err := rl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(rl.Path())
	if err != nil {
		t.Fatalf("read raw log: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("a raw log whose session took no round is 0 bytes — the file must carry its creation header")
	}
	got := string(data)
	if !strings.HasPrefix(got, rawLogHeaderPrefix) {
		t.Errorf("header missing or malformed:\n%s", got)
	}
	if !strings.Contains(got, "session="+runUUID) {
		t.Errorf("header must name the run it belongs to, got:\n%s", got)
	}
	if !strings.Contains(got, "log="+filepath.Base(rl.Path())) {
		t.Errorf("header must name its own file, got:\n%s", got)
	}
}

// clock, so two gopi processes (task worker children, a parallel epic fan-out)
// starting inside the SAME second must not resolve to the same path. When they
// do, both append to one file with O_APPEND and the sessions' HTTP traffic is
// interleaved with no boundary — `gopi session raw <session> 1 request` then returns
// ANOTHER session's first request, i.e. the user's epic goal silently
// disappears from the raw view (observed on real logs: sessions 77d67d9b and
// 044938be shared gopi-raw-20260827-231907.log and each read the other's
// round 1).
func TestNewRawLogger_SameSecondProcessesGetDistinctFiles(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	first, err := NewRawLogger()
	if err != nil {
		t.Fatalf("first NewRawLogger(): %v", err)
	}
	defer first.Close()

	second, err := NewRawLogger()
	if err != nil {
		t.Fatalf("second NewRawLogger(): %v", err)
	}
	defer second.Close()

	if first.Path() == second.Path() {
		t.Fatalf("two agents in the same second share one raw log %s — round numbering is not session-scoped", first.Path())
	}

	// Both files must exist on disk and be independently writable: the second
	// logger's bytes must never land in the first logger's file.
	second.LogFunc()(rawlogfmt.TagChunk, "", []byte("second-only"))
	if err := second.Sync(); err != nil {
		t.Fatalf("second.Sync: %v", err)
	}
	data, err := os.ReadFile(first.Path())
	if err != nil {
		t.Fatalf("read first raw log: %v", err)
	}
	if !strings.HasPrefix(string(data), rawLogHeaderPrefix) {
		t.Fatalf("first raw log must hold its own creation header, got %q", data)
	}
	if strings.Contains(string(data), "second-only") {
		t.Errorf("first raw log received the second logger's bytes: %q", data)
	}
}
