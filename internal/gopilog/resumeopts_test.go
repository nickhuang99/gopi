package gopilog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// The record's WIRE SHAPE is the contract between two packages that cannot
// share a definition: internal/agent WRITES it (SessionLogger.LogResumeOptions)
// and this package READS it (ResumeOptionsFromFile). This test drives both
// halves through the real event name and the real field constants, so a rename
// on one side cannot pass.
func TestResumeOptionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	body := strings.Join([]string{
		`{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"s1","timestamp":"2026-10-01T00:00:00Z"}`,
		`{"type":"` + sessionevent.EventTypeResumeOptions + `","` + sessionevent.ResumeOptionsFieldFlags + `":["--tools","-v"],"` +
			sessionevent.ResumeOptionsFieldSessionID + `":"s1","` + sessionevent.ResumeOptionsFieldWorkdir + `":"/home/x/proj"}`,
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	opts, found, err := ResumeOptionsFromFile(p)
	if err != nil {
		t.Fatalf("ResumeOptionsFromFile: %v", err)
	}
	if !found {
		t.Fatal("the record was not found — the reader and the writer disagree about the event name or a field key")
	}
	if len(opts.Flags) != 2 || opts.Flags[0] != "--tools" || opts.Flags[1] != "-v" {
		t.Errorf("flags = %v, want [--tools -v] in the recorded order", opts.Flags)
	}
	if opts.SessionID != "s1" || opts.Workdir != "/home/x/proj" {
		t.Errorf("record = %+v, want sessionId s1 and the recorded workdir", opts)
	}
	if opts.Path != p {
		t.Errorf("Path = %q, want the file it was read from (%q)", opts.Path, p)
	}
}

// A session that recorded NOTHING (every session written before this event
// existed, and every run with no options) must answer found=false with no
// error: the absence is a fact the caller reports, not a failure. A reader that
// returned an error here would make every pre-existing session unresumable.
func TestResumeOptionsAbsentIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	body := `{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"old","timestamp":"2026-09-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, found, err := ResumeOptionsFromFile(p)
	if err != nil {
		t.Fatalf("a session with no record must not be an error: %v", err)
	}
	if found {
		t.Error("found=true for a session that recorded no options — a caller would restore a command line that was never written")
	}
}

// TOLERANCE: a session file is a stream written by many binaries, and the file
// an operator opens after a CRASH is the one most likely to be torn mid-line.
// A torn line must be skipped, and the record beside it still read — the
// alternative is a recovery path that fails exactly when it is needed.
func TestResumeOptionsSkipsTornLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"s2","timestamp":"2026-10-01T00:00:00Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"crashed mid-`,
		`{"type":"` + sessionevent.EventTypeResumeOptions + `","` + sessionevent.ResumeOptionsFieldFlags + `":["--no-color"]}`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	opts, found, err := ResumeOptionsFromFile(p)
	if err != nil {
		t.Fatalf("a torn line failed the whole file: %v", err)
	}
	if !found || len(opts.Flags) != 1 || opts.Flags[0] != "--no-color" {
		t.Errorf("record after a torn line = %+v (found=%v), want the [--no-color] record", opts, found)
	}
}

// The LAST record wins: a resumed run writes its own record into the file it
// continues, so the newest statement on the session is the newest fact.
func TestResumeOptionsLastRecordWins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"` + sessionevent.EventTypeResumeOptions + `","` + sessionevent.ResumeOptionsFieldFlags + `":["--tools"]}`,
		`{"type":"` + sessionevent.EventTypeResumeOptions + `","` + sessionevent.ResumeOptionsFieldFlags + `":["--no-editor","--no-color"]}`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	opts, found, err := ResumeOptionsFromFile(p)
	if err != nil || !found {
		t.Fatalf("record not found: found=%v err=%v", found, err)
	}
	if len(opts.Flags) != 2 || opts.Flags[0] != "--no-editor" {
		t.Errorf("flags = %v, want the SECOND (newest) record", opts.Flags)
	}
}
