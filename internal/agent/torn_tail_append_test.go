package agent

// torn_tail_append_test.go — the WRITER-SIDE half of the torn-tail repair: a
// gopi process must never append onto a record whose last byte is not a
// newline.
//
// THE DEFECT IT CLOSES. A session record that ends mid-line is readable as a
// REFUSAL: the reader names the torn line, its offset and the intact lines
// before it (internal/gopilog/torn_tail.go). Append to that file and the
// diagnosis disappears — the dead fragment fuses with the next run's
// session_start, the result is a file whose damage sits in the MIDDLE, and no
// reader can separate it back into the two records it now contains. The
// truncation goes from "legible, repairable by hand" to "permanent corruption",
// which is strictly worse than the condition that caused it.
//
// MEASURED, NOT HYPOTHESISED: the three torn records on this host were produced
// by short writes inside an 8,504-line "No space left on device" window on
// 2026-10-12 (mtimes 18:16:20 / 18:16:42 / 18:16:55), and their runs were
// resumed afterwards — a resumed run appends a second session_start, exactly the
// write this guard refuses.
//
// Rule 32: this file touches no database and no live profile. Every fixture is
// built in the test's own scratch directory (the suite's TestMain pins that tree
// off /), and the bytes appended by the negative control are written by the
// production writer itself, not by the test.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tornRecordBody is a record whose final row was cut off mid-JSON — the shape
// all three real torn files have (a printable byte inside a string, no
// newline).
const tornRecordBody = `{"type":"session_start","sessionId":"torn","model":"m"}` + "\n" +
	`{"type":"user","message":{"content":"the row that was cut of`

// TestOpenResumeSessionRecordRefusesTornTail is the acceptance case for the
// writer side: continuing a session whose record ends mid-line is REFUSED, with
// a named error, and the file is left byte-for-byte untouched.
func TestOpenResumeSessionRecordRefusesTornTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(tornRecordBody), 0o600); err != nil {
		t.Fatalf("write torn fixture: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture back: %v", err)
	}

	sl, err := openResumeSessionRecord(path, "m", "torn", "", "", "resumed-from")
	if err == nil {
		t.Fatalf("appending to a record that ends mid-line must be refused, got a logger for %s", sl.Path())
	}
	if sl != nil {
		t.Fatalf("the refusal returned a logger too — a refused writer must not be usable")
	}

	var torn *UnterminatedRecordTailError
	if !errors.As(err, &torn) {
		t.Fatalf("error is not the named UnterminatedRecordTailError (got %T): %v", err, err)
	}
	if torn.Path != path {
		t.Errorf("error path = %q, want %q", torn.Path, path)
	}
	if want := int64(len(tornRecordBody)); torn.Size != want {
		t.Errorf("error size = %d, want %d", torn.Size, want)
	}
	for _, want := range []string{"REFUSING to append", path, "not a newline", "MIDDLE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not contain %q:\n%s", want, err)
		}
	}

	// THE FILE MUST BE UNTOUCHED. A guard that refused AFTER writing would leave
	// the very fusion it exists to prevent.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture after refusal: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the refused append modified the record:\nbefore=%q\nafter =%q", before, after)
	}
}

// TestOpenResumeSessionRecordAppendsToTerminatedRecord is the negative control:
// the guard must NOT fire on a healthy record — a terminated tail is legal, and
// a guard that refused it would break every resume in the product.
func TestOpenResumeSessionRecordAppendsToTerminatedRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(tornRecordBody[:strings.LastIndex(tornRecordBody, "\n")+1]), 0o600); err != nil {
		t.Fatalf("write healthy fixture: %v", err)
	}

	sl, err := openResumeSessionRecord(path, "m", "healthy", "", "", "resumed-from")
	if err != nil {
		t.Fatalf("a record whose last byte is a newline must be appendable: %v", err)
	}
	if sl == nil {
		t.Fatal("no logger and no error")
	}
	if err := sl.Close(); err != nil {
		t.Fatalf("close the resumed logger: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if n := strings.Count(string(data), `"type":"session_start"`); n != 2 {
		t.Fatalf("the resumed run's own header is not in the record (found %d session_start rows, want 2): the guard did not let a legitimate resume through", n)
	}
	if data[len(data)-1] != '\n' {
		t.Error("the record does not end with a newline after a legitimate resume")
	}
}

// TestOpenResumeSessionRecordAcceptsEmptyOrMissingRecord pins the two shapes the
// guard must treat as "no tail to inspect": a zero-length record, and a record
// that does not exist yet (the open creates it). Reporting either as torn would
// make the guard refuse first runs.
func TestOpenResumeSessionRecordAcceptsEmptyOrMissingRecord(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("create empty record: %v", err)
	}
	if sl, err := openResumeSessionRecord(empty, "m", "empty", "", "", ""); err != nil {
		t.Fatalf("an empty record has no torn tail and must be appendable: %v", err)
	} else if err := sl.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	missing := filepath.Join(dir, "missing.jsonl")
	if sl, err := openResumeSessionRecord(missing, "m", "missing", "", "", ""); err != nil {
		t.Fatalf("a record that does not exist yet must be creatable: %v", err)
	} else if err := sl.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestNewSessionLoggerRefusesTornTailThroughTheMintPath proves the guard sits on
// the ONE seam both constructors pass through, not only on the resume path: a
// mint onto an existing torn record (a run whose directory survived while its
// record was damaged) is refused the same way.
func TestNewSessionLoggerRefusesTornTailThroughTheMintPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(tornRecordBody), 0o600); err != nil {
		t.Fatalf("write torn fixture: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open torn record for the mint path: %v", err)
	}
	sl, err := newSessionLoggerOnFile(f, path, "m", "torn", "", "", "")
	if err == nil {
		t.Fatalf("the mint path appended to a torn record (logger at %s)", sl.Path())
	}
	var torn *UnterminatedRecordTailError
	if !errors.As(err, &torn) {
		t.Fatalf("mint-path error is not the named UnterminatedRecordTailError (got %T): %v", err, err)
	}
}

// TestNewSessionLoggerReportsAHeaderThatNeverLanded is the OTHER half of the
// silent-failure defect. The header write and its flush used to be called for
// their side effects only (`sl.log(start)` / `sl.Sync()`), so a short write —
// ENOSPC, EINTR — produced a record with NO first line and a run that reported
// nothing: the measured torn-tail family lost exactly this way, and the first
// symptom appeared in a LATER reader as an anonymous parse error.
//
// A closed descriptor is a deterministic stand-in for that write failure: the
// write cannot land, and the constructor must say so instead of handing back a
// logger whose record is headless.
func TestNewSessionLoggerReportsAHeaderThatNeverLanded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close before use: %v", err)
	}

	sl, err := newSessionLoggerOnFile(f, path, "m", "headless", "", "", "")
	if err == nil {
		t.Fatalf("a logger whose session_start could not be written must be reported, got none (logger at %s)", sl.Path())
	}
	for _, want := range []string{"session_start", path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not contain %q:\n%s", want, err)
		}
	}
	if fi, statErr := os.Stat(path); statErr == nil && fi.Size() != 0 {
		t.Fatalf("the record holds %d bytes although the header write failed — the write must not have landed", fi.Size())
	}
}
